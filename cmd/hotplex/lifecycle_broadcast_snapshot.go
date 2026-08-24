package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

const (
	lifecycleSnapshotVersion       = 1
	lifecycleSnapshotMaxBytes      = 64 << 10
	lifecycleSnapshotTTL           = 24 * time.Hour
	lifecycleSnapshotFutureSkew    = 5 * time.Minute
	lifecycleSnapshotClaimedSuffix = ".claimed"
	lifecycleSnapshotFilename      = "gateway.lifecycle-broadcast.json"
)

type lifecycleSnapshot struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SessionIDs []string  `json:"session_ids"`
}

type lifecycleSnapshotStore struct {
	path       string
	maxTargets int
	now        func() time.Time
}

func newLifecycleSnapshotStore(path string, maxTargets int, now func() time.Time) *lifecycleSnapshotStore {
	if now == nil {
		now = time.Now
	}
	return &lifecycleSnapshotStore{path: path, maxTargets: maxTargets, now: now}
}

func (s *lifecycleSnapshotStore) Save(sessionIDs []string) error {
	if len(sessionIDs) == 0 {
		for _, path := range []string{s.path, s.claimedPath()} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove empty lifecycle snapshot: %w", err)
			}
		}
		return nil
	}
	snapshot := &lifecycleSnapshot{
		Version:    lifecycleSnapshotVersion,
		CreatedAt:  s.now().UTC(),
		SessionIDs: append([]string(nil), sessionIDs...),
	}
	if err := s.validate(snapshot); err != nil {
		return err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal lifecycle snapshot: %w", err)
	}
	if len(data) > lifecycleSnapshotMaxBytes {
		return fmt.Errorf("lifecycle snapshot exceeds %d bytes", lifecycleSnapshotMaxBytes)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create lifecycle snapshot directory: %w", err)
	}
	if err := os.Remove(s.claimedPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale lifecycle claim: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create lifecycle snapshot temp file: %w", err)
	}
	tmpPath := tmp.Name()
	keepTemp := true
	defer func() {
		_ = tmp.Close()
		if keepTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod lifecycle snapshot: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write lifecycle snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync lifecycle snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close lifecycle snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("promote lifecycle snapshot: %w", err)
	}
	keepTemp = false
	return nil
}

func (s *lifecycleSnapshotStore) Claim() (*lifecycleSnapshot, string, error) {
	claimedPath := s.claimedPath()
	if _, err := os.Stat(claimedPath); err == nil {
		return nil, "", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("inspect lifecycle claim: %w", err)
	}
	if err := os.Rename(s.path, claimedPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("claim lifecycle snapshot: %w", err)
	}
	snapshot, err := s.read(claimedPath)
	if err != nil {
		_ = os.Remove(claimedPath)
		return nil, "", err
	}
	return snapshot, claimedPath, nil
}

func (s *lifecycleSnapshotStore) CompleteClaim(claimedPath string) error {
	if claimedPath == "" {
		return nil
	}
	if err := os.Remove(claimedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove lifecycle claim: %w", err)
	}
	return nil
}

func (s *lifecycleSnapshotStore) claimedPath() string {
	return s.path + lifecycleSnapshotClaimedSuffix
}

func (s *lifecycleSnapshotStore) read(path string) (*lifecycleSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open lifecycle snapshot: %w", err)
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, lifecycleSnapshotMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read lifecycle snapshot: %w", err)
	}
	if len(raw) > lifecycleSnapshotMaxBytes {
		return nil, fmt.Errorf("lifecycle snapshot exceeds %d bytes", lifecycleSnapshotMaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot lifecycleSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode lifecycle snapshot: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("decode lifecycle snapshot: trailing data")
	}
	if err := s.validate(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *lifecycleSnapshotStore) validate(snapshot *lifecycleSnapshot) error {
	if snapshot == nil || snapshot.Version != lifecycleSnapshotVersion {
		return fmt.Errorf("invalid lifecycle snapshot version")
	}
	if s.maxTargets <= 0 || len(snapshot.SessionIDs) == 0 || len(snapshot.SessionIDs) > s.maxTargets {
		return fmt.Errorf("invalid lifecycle snapshot target count")
	}
	now := s.now().UTC()
	if snapshot.CreatedAt.IsZero() || now.Sub(snapshot.CreatedAt) > lifecycleSnapshotTTL || snapshot.CreatedAt.Sub(now) > lifecycleSnapshotFutureSkew {
		return fmt.Errorf("invalid lifecycle snapshot timestamp")
	}
	seen := make(map[string]struct{}, len(snapshot.SessionIDs))
	for _, id := range snapshot.SessionIDs {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid lifecycle snapshot session id")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate lifecycle snapshot session id")
		}
		seen[id] = struct{}{}
	}
	return nil
}
