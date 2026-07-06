package audit

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// SpillRecord is one audit event written to the spill WAL.
type SpillRecord struct {
	TsMs int64         `json:"ts_ms"`
	UA   *UserActivity `json:"ua"`
}

// SpillFile is a crash-safe append-only WAL for audit events that overflowed
// the in-memory channel. Uses O_SYNC to ensure durability before returning.
type SpillFile struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// OpenSpill opens (or creates) a spill file at path with O_APPEND|O_WRONLY|O_SYNC.
func OpenSpill(path string) (*SpillFile, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_SYNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open spill: %w", err)
	}
	return &SpillFile{f: f, path: path}, nil
}

// Write appends one record: 4-byte big-endian length + JSON payload.
// O_SYNC ensures the data is on disk before returning.
func (s *SpillFile) Write(rec SpillRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit: spill marshal: %w", err)
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := s.f.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("audit: spill write len: %w", err)
	}
	if _, err := s.f.Write(payload); err != nil {
		return fmt.Errorf("audit: spill write payload: %w", err)
	}
	return nil
}

// maxSpillRecordSize is the sanity limit for a single record (64 MiB).
const maxSpillRecordSize = 64 * 1024 * 1024

// ReadAll reads all complete records from the spill file. Truncated trailing
// records (from a crash mid-write) are silently skipped — never returned.
func (s *SpillFile) ReadAll() ([]SpillRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Open a separate read handle to avoid interfering with the write handle.
	rf, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("audit: spill open for read: %w", err)
	}
	defer func() { _ = rf.Close() }()

	var records []SpillRecord
	for {
		var lenBuf [4]byte
		_, err := io.ReadFull(rf, lenBuf[:])
		if err != nil {
			// EOF or truncated length prefix — stop here.
			break
		}
		length := binary.BigEndian.Uint32(lenBuf[:])
		if length == 0 || length > maxSpillRecordSize {
			break // sanity limit or corrupt
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(rf, payload); err != nil {
			// Truncated payload (mid-record crash) — stop.
			break
		}
		var rec SpillRecord
		if err := json.Unmarshal(payload, &rec); err != nil {
			// Corrupt JSON — stop.
			break
		}
		records = append(records, rec)
	}
	return records, nil
}

// Truncate clears the spill file (after successful replay).
func (s *SpillFile) Truncate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Truncate(0); err != nil {
		return fmt.Errorf("audit: spill truncate: %w", err)
	}
	// Seek back to 0 so subsequent writes start at the beginning.
	if _, err := s.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("audit: spill seek after truncate: %w", err)
	}
	return nil
}

// Close closes the spill file.
func (s *SpillFile) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.f.Close()
}
