package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPiProvider(t *testing.T) {
	tests := []struct {
		name    string
	 config  ProviderConfig
	 wantErr bool
    }{
        {
            name: "default config",
            config: ProviderConfig{
                Type:    ProviderTypePi,
                Enabled: true,
            },
            wantErr: false,
        },
        {
            name: "with pi config",
            config: ProviderConfig{
                Type:    ProviderTypePi,
                Enabled: true,
                Pi: &PiConfig{
                    Provider: "anthropic",
                    Model:    "claude-sonnet-4-20250514",
                    Thinking: "high",
                },
            },
            wantErr: false,
        },
        {
            name: "with custom binary path",
            config: ProviderConfig{
                Type:       ProviderTypePi,
                Enabled:    true,
                BinaryPath: "/usr/local/bin/pi",
            },
            wantErr: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            provider, err := NewPiProvider(tt.config, nil)
            if tt.wantErr {
                assert.Error(t, err)
                assert.Nil(t, provider)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, provider)
                assert.Equal(t, ProviderTypePi, provider.Metadata().Type)
                assert.Equal(t, "pi", provider.Metadata().BinaryName)
            }
        })
    }
}

func TestPiProvider_Metadata(t *testing.T) {
    provider, err := NewPiProvider(ProviderConfig{
        Type:    ProviderTypePi,
        Enabled: true,
    }, nil)
    require.NoError(t, err)

    meta := provider.Metadata()
    assert.Equal(t, ProviderTypePi, meta.Type)
    assert.Equal(t, "Pi (pi-coding-agent)", meta.DisplayName)
    assert.Equal(t, "pi", meta.BinaryName)

    // Verify features
    assert.True(t, meta.Features.SupportsResume)
    assert.True(t, meta.Features.SupportsStreamJSON)
    assert.True(t, meta.Features.MultiTurnReady)
    assert.True(t, meta.Features.RequiresInitialPromptAsArg)
    assert.False(t, meta.Features.SupportsSSE)
    assert.False(t, meta.Features.SupportsHTTPAPI)
}
