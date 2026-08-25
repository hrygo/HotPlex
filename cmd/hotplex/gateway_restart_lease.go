package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/worker/proc"
)

const (
	restartLeaseSchemaVersion = 2
	restartLeaseReclaimAfter  = 5 * time.Minute
	restartLeaseMaxBytes      = 16 << 10
)

type restartLeasePhase string

const (
	restartLeasePrepared        restartLeasePhase = "prepared"
	restartLeaseHelperStarted   restartLeasePhase = "helper_started"
	restartLeaseWaitingForReady restartLeasePhase = "waiting_for_ready"
)

var (
	errRestartLeaseTicketMismatch = errors.New("restart lease ticket mismatch")
	errRestartLeaseInProgress     = errors.New("gateway restart already in progress")
)

type restartLease struct {
	SchemaVersion int               `json:"schema_version"`
	RequestID     string            `json:"request_id"`
	Phase         restartLeasePhase `json:"phase"`
	OwnerPID      int               `json:"owner_pid"`
	HelperPID     int               `json:"helper_pid"`
	CreatedAt     time.Time         `json:"created_at"`
}

type restartLeaseConflictError struct {
	RequestID string
}

func (e *restartLeaseConflictError) Error() string {
	if e.RequestID == "" {
		return errRestartLeaseInProgress.Error()
	}
	return fmt.Sprintf("%s (request_id=%s)", errRestartLeaseInProgress, e.RequestID)
}

func (e *restartLeaseConflictError) Unwrap() error {
	return errRestartLeaseInProgress
}

type restartLeaseStore struct {
	path         string
	now          func() time.Time
	processAlive func(int) bool
	mu           sync.Mutex
}

func newRestartLeaseStore(path string, now func() time.Time, processAlive func(int) bool) *restartLeaseStore {
	if now == nil {
		now = time.Now
	}
	if processAlive == nil {
		processAlive = func(pid int) bool {
			if pid <= 0 {
				return false
			}
			return !proc.IsProcessNotExist(proc.IsProcessAlive(pid))
		}
	}
	return &restartLeaseStore{
		path:         path,
		now:          now,
		processAlive: processAlive,
	}
}

func (s *restartLeaseStore) Acquire(ownerPID int) (*restartLease, error) {
	if ownerPID <= 0 {
		return nil, fmt.Errorf("acquire restart lease: invalid owner PID %d", ownerPID)
	}
	var acquired *restartLease
	err := s.withExclusiveLock(func() error {
		for {
			current, err := s.readUnlocked()
			if err == nil {
				if !s.stale(current) {
					return &restartLeaseConflictError{RequestID: current.RequestID}
				}
				if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("reclaim stale restart lease: %w", err)
				}
				continue
			}
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}

			requestID, err := newRestartRequestID()
			if err != nil {
				return err
			}
			lease := &restartLease{
				SchemaVersion: restartLeaseSchemaVersion,
				RequestID:     requestID,
				Phase:         restartLeasePrepared,
				OwnerPID:      ownerPID,
				CreatedAt:     s.now().UTC(),
			}
			if err := s.create(lease); err != nil {
				if errors.Is(err, os.ErrExist) {
					continue
				}
				return err
			}
			acquired = lease
			return nil
		}
	})
	return acquired, err
}

func (s *restartLeaseStore) Read() (*restartLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readUnlocked()
}

func (s *restartLeaseStore) readUnlocked() (*restartLease, error) {
	data, err := readBoundedFile(s.path, restartLeaseMaxBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read restart lease: %w", err)
	}

	var envelope struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode restart lease: %w", err)
	}
	if envelope.SchemaVersion == nil {
		var legacy restartMarker
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("decode legacy restart marker: %w", err)
		}
		if legacy.HelperPID <= 0 || legacy.CreatedAt.IsZero() {
			return nil, errors.New("invalid legacy restart marker")
		}
		return &restartLease{
			SchemaVersion: 1,
			Phase:         restartLeaseHelperStarted,
			HelperPID:     legacy.HelperPID,
			CreatedAt:     legacy.CreatedAt,
		}, nil
	}
	if *envelope.SchemaVersion != restartLeaseSchemaVersion {
		return nil, fmt.Errorf("unsupported restart lease schema_version %d", *envelope.SchemaVersion)
	}

	var lease restartLease
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lease); err != nil {
		return nil, fmt.Errorf("decode restart lease: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode restart lease: trailing JSON")
		}
		return nil, fmt.Errorf("decode restart lease: %w", err)
	}
	if err := validateRestartLease(&lease); err != nil {
		return nil, err
	}
	return &lease, nil
}

func (s *restartLeaseStore) Update(requestID string, mutate func(*restartLease) error) error {
	if mutate == nil {
		return errors.New("update restart lease: nil mutation")
	}
	return s.withExclusiveLock(func() error {
		current, err := s.readUnlocked()
		if err != nil {
			return err
		}
		if current.RequestID != requestID || requestID == "" {
			return errRestartLeaseTicketMismatch
		}
		if err := mutate(current); err != nil {
			return err
		}
		if err := validateRestartLease(current); err != nil {
			return err
		}
		return s.writeAtomic(current)
	})
}

func (s *restartLeaseStore) Release(requestID string) error {
	return s.withExclusiveLock(func() error {
		current, err := s.readUnlocked()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.RequestID != requestID || requestID == "" {
			return errRestartLeaseTicketMismatch
		}
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("release restart lease: %w", err)
		}
		return nil
	})
}

func (s *restartLeaseStore) withExclusiveLock(action func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create restart lease directory: %w", err)
	}
	release, err := acquireRestartLeaseFileLock(s.path + ".lock")
	if err != nil {
		return fmt.Errorf("acquire restart lease lock: %w", err)
	}
	actionErr := action()
	if releaseErr := release(); releaseErr != nil {
		return errors.Join(actionErr, fmt.Errorf("release restart lease lock: %w", releaseErr))
	}
	return actionErr
}

func (s *restartLeaseStore) create(lease *restartLease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return fmt.Errorf("encode restart lease: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = os.Remove(s.path)
		return fmt.Errorf("set restart lease permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = os.Remove(s.path)
		return fmt.Errorf("write restart lease: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(s.path)
		return fmt.Errorf("sync restart lease: %w", err)
	}
	return nil
}

func (s *restartLeaseStore) writeAtomic(lease *restartLease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return fmt.Errorf("encode restart lease: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".gateway.restart.*")
	if err != nil {
		return fmt.Errorf("create restart lease temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("set restart lease temporary permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write restart lease temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync restart lease temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close restart lease temporary file: %w", err)
	}
	if err := replaceRestartFile(tempPath, s.path); err != nil {
		return fmt.Errorf("replace restart lease: %w", err)
	}
	return nil
}

func (s *restartLeaseStore) stale(lease *restartLease) bool {
	if lease.Phase == restartLeasePrepared {
		return !s.processAlive(lease.OwnerPID)
	}
	if s.now().Sub(lease.CreatedAt) < restartLeaseReclaimAfter {
		return false
	}
	return !s.processAlive(lease.OwnerPID) && !s.processAlive(lease.HelperPID)
}

func validateRestartLease(lease *restartLease) error {
	if lease.SchemaVersion != restartLeaseSchemaVersion {
		return fmt.Errorf("unsupported restart lease schema_version %d", lease.SchemaVersion)
	}
	if !validRestartRequestID(lease.RequestID) {
		return errors.New("invalid restart lease request_id")
	}
	switch lease.Phase {
	case restartLeasePrepared, restartLeaseHelperStarted, restartLeaseWaitingForReady:
	default:
		return fmt.Errorf("invalid restart lease phase %q", lease.Phase)
	}
	if lease.OwnerPID <= 0 || lease.CreatedAt.IsZero() {
		return errors.New("invalid restart lease owner or timestamp")
	}
	if lease.HelperPID < 0 {
		return errors.New("invalid restart lease helper PID")
	}
	return nil
}

func newRestartRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate restart request ID: %w", err)
	}
	return "req_" + hex.EncodeToString(raw[:]), nil
}

func validRestartRequestID(requestID string) bool {
	if len(requestID) != len("req_")+32 || !strings.HasPrefix(requestID, "req_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(requestID, "req_"))
	return err == nil
}

func readBoundedFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file %s exceeds %d bytes", path, maxBytes)
	}
	return data, nil
}
