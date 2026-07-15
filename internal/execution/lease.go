package execution

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/observability"
)

type LeaseConfig struct {
	RenewInterval   time.Duration
	RecoverInterval time.Duration
	ShutdownTimeout time.Duration
}

func DefaultLeaseConfig() LeaseConfig {
	return LeaseConfig{
		RenewInterval:   20 * time.Second,
		RecoverInterval: 20 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
}

type LeaseManager struct {
	store   Store
	ownerID string
	cfg     LeaseConfig
	log     *slog.Logger

	stopCh chan struct{}
	wg     sync.WaitGroup
	start  sync.Once
	stop   sync.Once
	closed atomic.Bool

	exclusions LeaseExclusionTracker
}

type LeaseExclusionTracker interface {
	AbandonedExecutionIDs() []string
	ClearAbandonedExecutionIDs([]string)
}

func NewLeaseManager(store Store, ownerID string, cfg LeaseConfig, log *slog.Logger, exclusionTrackers ...LeaseExclusionTracker) *LeaseManager {
	if log == nil {
		log = slog.Default()
	}
	if cfg.RenewInterval == 0 || cfg.RecoverInterval == 0 || cfg.ShutdownTimeout == 0 {
		cfg = DefaultLeaseConfig()
	}
	m := &LeaseManager{
		store:   store,
		ownerID: ownerID,
		cfg:     cfg,
		log:     log.With("component", "lease_manager", "owner", ownerID),
		stopCh:  make(chan struct{}),
	}
	if len(exclusionTrackers) > 0 {
		m.exclusions = exclusionTrackers[0]
	}
	return m
}

func (m *LeaseManager) Start(ctx context.Context) {
	m.start.Do(func() {
		m.wg.Add(2)
		go m.renewLoop(ctx)
		go m.recoverLoop(ctx)
	})
}

func (m *LeaseManager) renewLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.cfg.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			var excluded []string
			if m.exclusions != nil {
				excluded = m.exclusions.AbandonedExecutionIDs()
			}
			renewed, err := m.store.RenewLeases(ctx, m.ownerID, int64(LeaseTTL), excluded)
			if err != nil {
				observability.LeaseRenewFailure().Add(ctx, 1)
				m.log.Warn("lease renew failed", "error", err)
				continue
			}
			if renewed > 0 {
				m.log.Debug("lease renew succeeded", "renewed", renewed)
			}
		}
	}
}

func (m *LeaseManager) recoverLoop(ctx context.Context) {
	defer m.wg.Done()

	// Startup sweep: recover expired leases immediately instead of waiting for
	// the first ticker, so a restarting gateway unblocks sessions whose leases
	// expired during the outage without the extra RecoverInterval of latency.
	m.recoverOnce(ctx)

	ticker := time.NewTicker(m.cfg.RecoverInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.recoverOnce(ctx)
		}
	}
}

func (m *LeaseManager) recoverOnce(ctx context.Context) {
	var tracked []string
	if m.exclusions != nil {
		tracked = m.exclusions.AbandonedExecutionIDs()
	}
	result, err := m.store.RecoverExpiredLeases(ctx, tracked)
	if err != nil {
		m.log.Warn("expired lease recovery failed", "error", err)
		return
	}
	if m.exclusions != nil && len(result.ConvergedExecutionIDs) > 0 {
		m.exclusions.ClearAbandonedExecutionIDs(result.ConvergedExecutionIDs)
	}
	if result.Recovered > 0 {
		observability.LeaseExpiredRecovery().Add(ctx, result.Recovered)
		m.log.Warn("recovered expired leases", "recovered", result.Recovered)
	}
}

func (m *LeaseManager) Shutdown(ctx context.Context) error {
	m.stop.Do(func() {
		close(m.stopCh)
	})
	if m.closed.Load() {
		return nil
	}

	terminated, err := m.store.TerminateOwnerLeases(ctx, m.ownerID, "GATEWAY_SHUTDOWN")
	if err != nil {
		m.log.Error("failed to terminate owner leases on shutdown", "error", err)
	} else if terminated > 0 {
		m.log.Info("terminated active leases on shutdown", "terminated", terminated)
	}

	select {
	case <-m.waitDone():
	case <-time.After(m.cfg.ShutdownTimeout):
		m.log.Warn("lease manager shutdown timed out", "timeout", m.cfg.ShutdownTimeout)
	}
	m.closed.Store(true)
	return nil
}

func (m *LeaseManager) waitDone() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(ch)
	}()
	return ch
}
