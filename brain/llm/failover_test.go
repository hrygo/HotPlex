package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFailoverManager_BasicFailover(t *testing.T) {
	config := DefaultFailoverConfig()
	config.EnableAutoFailover = true
	config.EnableFailback = false
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	// Manually failover to backup
	err := fm.ManualFailover("backup")
	assert.NoError(t, err)
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)
	
	stats := fm.GetStats()
	assert.Equal(t, int32(1), stats.FailoverCount)
}

func TestFailoverManager_ManualFailover(t *testing.T) {
	config := DefaultFailoverConfig()
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	// Manual failover
	err := fm.ManualFailover("backup")
	assert.NoError(t, err)
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)
}

func TestFailoverManager_Failback(t *testing.T) {
	config := DefaultFailoverConfig()
	config.EnableAutoFailover = true
	config.EnableFailback = true
	config.FailbackCooldown = 50 * time.Millisecond
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	
	// Force failover to backup
	fm.ManualFailover("backup")
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)

	// Wait for cooldown
	time.Sleep(100 * time.Millisecond)

	// Execute successfully with backup - should trigger failback
	err := fm.ExecuteWithFailover(context.Background(), func(p *ProviderConfig) error {
		return nil
	})
	assert.NoError(t, err)

	// Should have failed back to primary
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)
}

func TestFailoverManager_Stats(t *testing.T) {
	config := DefaultFailoverConfig()
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)

	// Manual failover
	fm.ManualFailover("backup")

	stats := fm.GetStats()
	assert.True(t, stats.IsActive)
	assert.Equal(t, "backup", stats.CurrentProvider)
	assert.Equal(t, int32(1), stats.FailoverCount)
	assert.Len(t, stats.RecentFailovers, 1)
}

func TestFailoverManager_Reset(t *testing.T) {
	config := DefaultFailoverConfig()
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	fm.ManualFailover("backup")
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)

	// Reset
	fm.Reset()
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)
	
	stats := fm.GetStats()
	assert.Equal(t, int32(0), stats.FailoverCount)
	assert.False(t, stats.IsActive)
}

func TestFailoverManager_NoHealthyProviders(t *testing.T) {
	config := DefaultFailoverConfig()
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
	}

	fm := NewFailoverManager(config)

	// Force circuit breaker open
	fm.circuitBreakers["primary"].ForceOpen()

	// Should fail with no healthy providers
	err := fm.ExecuteWithFailover(context.Background(), func(p *ProviderConfig) error {
		return nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no healthy providers")
}

func TestFailoverHistory(t *testing.T) {
	history := NewFailoverHistory(5)

	// Add 7 records
	for i := 0; i < 7; i++ {
		history.Add(FailoverRecord{
			Timestamp:    time.Now(),
			FromProvider: "primary",
			ToProvider:   "backup",
			Reason:       "error",
		})
	}

	// Should only keep last 5
	recent := history.GetRecent(10)
	assert.Len(t, recent, 5)
}
