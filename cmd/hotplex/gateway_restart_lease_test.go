package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRestartLease_ConcurrentAcquireOnlyOneWinner(t *testing.T) {
	t.Parallel()

	store := newRestartLeaseStore(filepath.Join(t.TempDir(), "gateway.restart"), time.Now, func(pid int) bool {
		return pid == 1234
	})

	const attempts = 50
	var winners atomic.Int32
	var winner *restartLease
	var winnerMu sync.Mutex
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := store.Acquire(1234)
			if err == nil {
				winners.Add(1)
				winnerMu.Lock()
				winner = lease
				winnerMu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), winners.Load())
	require.NotNil(t, winner)
	require.Len(t, winner.RequestID, len("req_")+32)
	require.Equal(t, restartLeaseSchemaVersion, winner.SchemaVersion)
	require.Equal(t, restartLeasePrepared, winner.Phase)
}

func TestRestartLease_StaleReclaimSerializesIndependentStores(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart")
	stale := &restartLease{
		SchemaVersion: restartLeaseSchemaVersion,
		RequestID:     "req_0123456789abcdef0123456789abcdef",
		Phase:         restartLeasePrepared,
		OwnerPID:      9001,
		CreatedAt:     time.Now().Add(-restartLeaseReclaimAfter),
	}
	data, err := json.Marshal(stale)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	staleCheckStarted := make(chan struct{})
	releaseStaleCheck := make(chan struct{})
	blockedStore := newRestartLeaseStore(path, time.Now, func(pid int) bool {
		if pid == stale.OwnerPID {
			close(staleCheckStarted)
			<-releaseStaleCheck
			return false
		}
		return pid == 1001
	})
	competingStore := newRestartLeaseStore(path, time.Now, func(pid int) bool {
		return pid == 1001
	})

	type acquireResult struct {
		lease *restartLease
		err   error
	}
	blockedResult := make(chan acquireResult, 1)
	go func() {
		lease, acquireErr := blockedStore.Acquire(1001)
		blockedResult <- acquireResult{lease: lease, err: acquireErr}
	}()
	<-staleCheckStarted

	competingResult := make(chan acquireResult, 1)
	go func() {
		lease, acquireErr := competingStore.Acquire(2002)
		competingResult <- acquireResult{lease: lease, err: acquireErr}
	}()

	var earlyCompeting *acquireResult
	select {
	case result := <-competingResult:
		earlyCompeting = &result
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseStaleCheck)

	blocked := <-blockedResult
	var competing acquireResult
	if earlyCompeting != nil {
		competing = *earlyCompeting
	} else {
		competing = <-competingResult
	}

	winners := 0
	for _, result := range []acquireResult{blocked, competing} {
		if result.err == nil {
			winners++
			require.NotNil(t, result.lease)
			continue
		}
		require.ErrorIs(t, result.err, errRestartLeaseInProgress)
	}
	require.Equal(t, 1, winners)
}

func TestRestartLease_PermissionsAndTicketFencing(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart")
	store := newRestartLeaseStore(path, time.Now, func(int) bool { return true })
	lease, err := store.Acquire(1234)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	lockInfo, err := os.Stat(path + ".lock")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), lockInfo.Mode().Perm())

	err = store.Update("req_wrong", func(current *restartLease) error {
		current.Phase = restartLeaseHelperStarted
		return nil
	})
	require.ErrorIs(t, err, errRestartLeaseTicketMismatch)
	require.ErrorIs(t, store.Release("req_wrong"), errRestartLeaseTicketMismatch)

	current, err := store.Read()
	require.NoError(t, err)
	require.Equal(t, lease.RequestID, current.RequestID)
	require.Equal(t, restartLeasePrepared, current.Phase)
}

func TestRestartLease_ReclaimsDeadLegacyMarkerOnlyAfterGracePeriod(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart")
	createdAt := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	legacy := restartMarker{HelperPID: 9001, CreatedAt: createdAt}
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	store := newRestartLeaseStore(path, func() time.Time {
		return createdAt.Add(restartLeaseReclaimAfter + time.Second)
	}, func(int) bool { return false })
	lease, err := store.Acquire(1234)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Equal(t, restartLeaseSchemaVersion, lease.SchemaVersion)
}

func TestRestartLease_UnknownSchemaFailsClosed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.restart")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600))

	store := newRestartLeaseStore(path, time.Now, func(int) bool { return false })
	_, err := store.Acquire(1234)
	require.Error(t, err)
	require.False(t, errors.Is(err, os.ErrNotExist))
}
