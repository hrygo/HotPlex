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
}

func NewLeaseManager(store Store, ownerID string, cfg LeaseConfig, log *slog.Logger) *LeaseManager {
	if log == nil {
		log = slog.Default()
	}
	if cfg.RenewInterval == 0 || cfg.RecoverInterval == 0 || cfg.ShutdownTimeout == 0 {
		cfg = DefaultLeaseConfig()
	}
	return &LeaseManager{
		store:   store,
		ownerID: ownerID,
		cfg:     cfg,
		log:     log.With("component", "lease_manager", "owner", ownerID),
		stopCh:  make(chan struct{}),
	}
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
			renewed, err := m.store.RenewLeases(ctx, m.ownerID, int64(LeaseTTL))
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
	recovered, err := m.store.RecoverExpiredLeases(ctx, time.Now().UnixMilli())
	if err != nil {
		m.log.Warn("expired lease recovery failed", "error", err)
		return
	}
	if recovered > 0 {
		observability.LeaseExpiredRecovery().Add(ctx, recovered)
		m.log.Warn("recovered expired leases", "recovered", recovered)
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
