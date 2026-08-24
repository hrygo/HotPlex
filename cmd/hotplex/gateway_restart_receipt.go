package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hrygo/hotplex/internal/config"
)

const (
	gatewayRestartReceiptSchemaVersion = 1
	gatewayRestartReceiptMaxBytes      = 64 << 10
)

var (
	errRestartReceiptExists         = errors.New("gateway restart receipt already exists")
	errRestartReceiptTicketMismatch = errors.New("gateway restart receipt ticket mismatch")
)

type gatewayRestartReceipt struct {
	SchemaVersion int               `json:"schema_version"`
	RequestID     string            `json:"request_id"`
	Platform      string            `json:"platform"`
	BotName       string            `json:"bot_name,omitempty"`
	PlatformKey   map[string]string `json:"platform_key,omitempty"`
	RequestedAt   time.Time         `json:"requested_at"`
	OldVersion    string            `json:"old_version"`
	OldPID        int               `json:"old_pid"`
}

type restartReceiptStore struct {
	path string
}

func gatewayRestartReceiptPath() string {
	return filepath.Join(config.HotplexHome(), ".pids", "gateway.restart.receipt.json")
}

func newRestartReceiptStore(path string) *restartReceiptStore {
	return &restartReceiptStore{path: path}
}

func (s *restartReceiptStore) Write(receipt *gatewayRestartReceipt) error {
	if receipt == nil {
		return errors.New("write gateway restart receipt: nil receipt")
	}
	if _, err := os.Stat(s.path); err == nil {
		return errRestartReceiptExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check gateway restart receipt: %w", err)
	}

	candidate := *receipt
	if candidate.SchemaVersion == 0 {
		candidate.SchemaVersion = gatewayRestartReceiptSchemaVersion
	}
	if err := validateGatewayRestartReceipt(&candidate); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create gateway restart receipt directory: %w", err)
	}

	data, err := json.Marshal(&candidate)
	if err != nil {
		return fmt.Errorf("encode gateway restart receipt: %w", err)
	}
	if len(data) > gatewayRestartReceiptMaxBytes {
		return fmt.Errorf("gateway restart receipt exceeds %d bytes", gatewayRestartReceiptMaxBytes)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".gateway.restart.receipt.*")
	if err != nil {
		return fmt.Errorf("create gateway restart receipt temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("set gateway restart receipt permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write gateway restart receipt: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync gateway restart receipt: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close gateway restart receipt: %w", err)
	}
	if err := replaceRestartFile(tempPath, s.path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errRestartReceiptExists
		}
		return fmt.Errorf("publish gateway restart receipt: %w", err)
	}
	return nil
}

func (s *restartReceiptStore) Read() (*gatewayRestartReceipt, error) {
	data, err := readBoundedFile(s.path, gatewayRestartReceiptMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read gateway restart receipt: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt gatewayRestartReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return nil, fmt.Errorf("decode gateway restart receipt: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode gateway restart receipt: trailing JSON")
		}
		return nil, fmt.Errorf("decode gateway restart receipt: %w", err)
	}
	if err := validateGatewayRestartReceipt(&receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (s *restartReceiptStore) Complete(requestID string) error {
	receipt, err := s.Read()
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil
	}
	if receipt.RequestID != requestID || requestID == "" {
		return errRestartReceiptTicketMismatch
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("complete gateway restart receipt: %w", err)
	}
	return nil
}

func (s *restartReceiptStore) Quarantine() (string, error) {
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("inspect gateway restart receipt: %w", err)
	}
	quarantined := s.path + ".corrupt." + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.Rename(s.path, quarantined); err != nil {
		return "", fmt.Errorf("quarantine gateway restart receipt: %w", err)
	}
	_ = os.Chmod(quarantined, 0o600)
	return quarantined, nil
}

func validateGatewayRestartReceipt(receipt *gatewayRestartReceipt) error {
	if receipt.SchemaVersion != gatewayRestartReceiptSchemaVersion {
		return fmt.Errorf("unsupported gateway restart receipt schema_version %d", receipt.SchemaVersion)
	}
	if !validRestartRequestID(receipt.RequestID) {
		return errors.New("invalid gateway restart receipt request_id")
	}
	if receipt.Platform == "" || receipt.RequestedAt.IsZero() {
		return errors.New("invalid gateway restart receipt platform or timestamp")
	}
	if receipt.OldPID <= 0 {
		return errors.New("invalid gateway restart receipt old PID")
	}
	return nil
}
