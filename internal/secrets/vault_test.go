package secrets

import (
	"context"
	"testing"
)

func TestVaultProvider_NotImplemented(t *testing.T) {
	ctx := context.Background()
	p := NewVaultProvider(
		WithVaultAddress("http://localhost:8200"),
		WithVaultToken("test-token"),
	)

	// Test Get (should fail with not implemented)
	_, err := p.Get(ctx, "test-key")
	if err == nil {
		t.Error("Get() expected error for not implemented")
	}

	// Test Set (should fail with not implemented)
	err = p.Set(ctx, "test-key", "test-value")
	if err == nil {
		t.Error("Set() expected error for not implemented")
	}

	// Test Delete (should fail with not implemented)
	err = p.Delete(ctx, "test-key")
	if err == nil {
		t.Error("Delete() expected error for not implemented")
	}
}

func TestVaultProvider_NoConfig(t *testing.T) {
	ctx := context.Background()
	p := NewVaultProvider()

	// Test Get without config
	_, err := p.Get(ctx, "test-key")
	if err == nil {
		t.Error("Get() expected error for no config")
	}

	// Test Set without config
	err = p.Set(ctx, "test-key", "test-value")
	if err == nil {
		t.Error("Set() expected error for no config")
	}
}
