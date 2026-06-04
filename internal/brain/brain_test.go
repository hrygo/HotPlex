package brain

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBrainInterface_Compatibility(t *testing.T) {
	var _ Brain = (*enhancedBrainWrapper)(nil)
}

func TestConfig_LoadFromEnv(t *testing.T) {
	_ = os.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key")
	_ = os.Setenv("HOTPLEX_BRAIN_PROVIDER", "openai")
	_ = os.Setenv("HOTPLEX_BRAIN_MODEL", "gpt-4o")
	_ = os.Setenv("HOTPLEX_BRAIN_TIMEOUT_S", "30")
	_ = os.Setenv("HOTPLEX_BRAIN_CACHE_SIZE", "500")
	_ = os.Setenv("HOTPLEX_BRAIN_MAX_RETRIES", "5")
	_ = os.Setenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", "200")
	_ = os.Setenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", "3000")

	defer func() {
		_ = os.Unsetenv("HOTPLEX_BRAIN_API_KEY")
		_ = os.Unsetenv("HOTPLEX_BRAIN_PROVIDER")
		_ = os.Unsetenv("HOTPLEX_BRAIN_MODEL")
		_ = os.Unsetenv("HOTPLEX_BRAIN_TIMEOUT_S")
		_ = os.Unsetenv("HOTPLEX_BRAIN_CACHE_SIZE")
		_ = os.Unsetenv("HOTPLEX_BRAIN_MAX_RETRIES")
		_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS")
		_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS")
	}()

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "gpt-4o", config.Model.Model)
	assert.Equal(t, 30, config.Model.TimeoutS)
	assert.Equal(t, 500, config.Cache.Size)
	assert.Equal(t, 5, config.Retry.MaxAttempts)
	assert.Equal(t, 200, config.Retry.MinWaitMs)
	assert.Equal(t, 3000, config.Retry.MaxWaitMs)
}

func TestConfig_DefaultValues(t *testing.T) {
	_ = os.Unsetenv("HOTPLEX_BRAIN_API_KEY")
	_ = os.Unsetenv("HOTPLEX_BRAIN_PROVIDER")
	_ = os.Unsetenv("HOTPLEX_BRAIN_MODEL")
	_ = os.Unsetenv("HOTPLEX_BRAIN_TIMEOUT_S")
	_ = os.Unsetenv("HOTPLEX_BRAIN_CACHE_SIZE")
	_ = os.Unsetenv("HOTPLEX_BRAIN_MAX_RETRIES")
	_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS")
	_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("DEEPSEEK_API_KEY")
	_ = os.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	_ = os.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key")
	_ = os.Setenv("HOTPLEX_BRAIN_PROVIDER", "openai")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "gpt-4o", config.Model.Model)
	assert.Equal(t, 30, config.Model.TimeoutS)
	assert.Equal(t, 1000, config.Cache.Size)
	assert.Equal(t, 3, config.Retry.MaxAttempts)
	assert.Equal(t, 100, config.Retry.MinWaitMs)
	assert.Equal(t, 5000, config.Retry.MaxWaitMs)
}

func TestGlobalBrain_Access(t *testing.T) {
	assert.Nil(t, Global(), "global brain should be nil initially")

	mockBrain := &mockBrain{}
	SetGlobal(mockBrain)

	assert.Equal(t, mockBrain, Global(), "global brain should be set")
}

type mockBrain struct{}

func (m *mockBrain) Chat(ctx context.Context, prompt string) (string, error) {
	return "mock response", nil
}

func (m *mockBrain) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	return m.Chat(ctx, prompt)
}

func TestTimeoutApplication(t *testing.T) {
	mockBrain := &slowMockBrain{}
	SetGlobal(mockBrain)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Global().Chat(ctx, "test")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "should timeout quickly")
}

type slowMockBrain struct{}

func (m *slowMockBrain) Chat(ctx context.Context, prompt string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(1 * time.Second):
		return "response", nil
	}
}

func (m *slowMockBrain) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	return m.Chat(ctx, prompt)
}
