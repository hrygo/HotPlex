package security

import (
	"testing"

	"github.com/hrygo/hotplex/internal/config"
)

func TestKeyRateLimiter_DisabledAllowsEverything(t *testing.T) {
	l := NewKeyRateLimiter(config.RateLimitConfig{Enabled: false})
	for i := 0; i < 100; i++ {
		if !l.Allow("k") {
			t.Fatalf("disabled limiter rejected request %d", i)
		}
	}
}

func TestKeyRateLimiter_BurstThenReject(t *testing.T) {
	l := NewKeyRateLimiter(config.RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 1,
		Burst:          3,
	})
	// A fresh key starts with a full burst.
	for i := 0; i < 3; i++ {
		if !l.Allow("key-a") {
			t.Fatalf("request %d within burst should be allowed", i)
		}
	}
	// Bucket is now empty; the next request is throttled.
	if l.Allow("key-a") {
		t.Fatal("request beyond burst should be rejected")
	}
	// A different key has its own independent bucket.
	if !l.Allow("key-b") {
		t.Fatal("independent key should start with a full burst")
	}
}

func TestKeyRateLimiter_AdminKeyBypasses(t *testing.T) {
	l := NewKeyRateLimiter(config.RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 1,
		Burst:          1,
		AdminKey:       "ops-probe",
	})
	for i := 0; i < 50; i++ {
		if !l.Allow("ops-probe") {
			t.Fatalf("admin key should always bypass (request %d)", i)
		}
	}
}
