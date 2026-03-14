package persistence

import (
	"os"
	"path/filepath"
	"testing"
)

// ========================================
// FileMarkerStore Tests
// ========================================

func TestFileMarkerStore_NewFileMarkerStore(t *testing.T) {
	tmpDir := t.TempDir()
	
	store, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create FileMarkerStore: %v", err)
	}
	
	if store.Dir() != tmpDir {
		t.Errorf("Expected dir '%s', got '%s'", tmpDir, store.Dir())
	}
}

func TestFileMarkerStore_NewFileMarkerStore_EmptyDir(t *testing.T) {
	_, err := NewFileMarkerStore("")
	if err == nil {
		t.Error("Expected error for empty directory")
	}
}

func TestFileMarkerStore_CreateAndExists(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create FileMarkerStore: %v", err)
	}
	
	sessionID := "test-session-123"
	
	// Initially should not exist
	if store.Exists(sessionID) {
		t.Error("Session should not exist initially")
	}
	
	// Create marker
	err = store.Create(sessionID)
	if err != nil {
		t.Fatalf("Failed to create marker: %v", err)
	}
	
	// Now should exist
	if !store.Exists(sessionID) {
		t.Error("Session should exist after Create")
	}
	
	// Verify file exists on disk
	markerPath := filepath.Join(tmpDir, sessionID+".lock")
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Error("Marker file should exist on disk")
	}
}

func TestFileMarkerStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create FileMarkerStore: %v", err)
	}
	
	sessionID := "test-session-delete"
	
	// Create marker
	err = store.Create(sessionID)
	if err != nil {
		t.Fatalf("Failed to create marker: %v", err)
	}
	
	// Delete marker
	err = store.Delete(sessionID)
	if err != nil {
		t.Fatalf("Failed to delete marker: %v", err)
	}
	
	// Should not exist
	if store.Exists(sessionID) {
		t.Error("Session should not exist after Delete")
	}
}

func TestFileMarkerStore_Delete_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create FileMarkerStore: %v", err)
	}
	
	// Deleting non-existent should not error
	err = store.Delete("non-existent-session")
	if err != nil {
		t.Errorf("Delete should not error for non-existent session: %v", err)
	}
}

func TestFileMarkerStore_Dir(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewFileMarkerStore(tmpDir)
	
	if store.Dir() != tmpDir {
		t.Errorf("Expected '%s', got '%s'", tmpDir, store.Dir())
	}
}

func TestFileMarkerStore_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create FileMarkerStore: %v", err)
	}
	
	// Concurrent create
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			err := store.Create("session-" + string(rune('0'+id)))
			if err != nil {
				t.Errorf("Concurrent Create failed: %v", err)
			}
			done <- true
		}(i)
	}
	
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Verify all exist
	for i := 0; i < 10; i++ {
		if !store.Exists("session-" + string(rune('0'+i))) {
			t.Errorf("Session %d should exist", i)
		}
	}
}

// ========================================
// InMemoryMarkerStore Tests
// ========================================

func TestInMemoryMarkerStore(t *testing.T) {
	store := NewInMemoryMarkerStore()
	
	sessionID := "test-session"
	
	// Initially should not exist
	if store.Exists(sessionID) {
		t.Error("Session should not exist initially")
	}
	
	// Create marker
	err := store.Create(sessionID)
	if err != nil {
		t.Fatalf("Failed to create marker: %v", err)
	}
	
	// Now should exist
	if !store.Exists(sessionID) {
		t.Error("Session should exist after Create")
	}
	
	// Delete marker
	err = store.Delete(sessionID)
	if err != nil {
		t.Fatalf("Failed to delete marker: %v", err)
	}
	
	// Should not exist
	if store.Exists(sessionID) {
		t.Error("Session should not exist after Delete")
	}
}

func TestInMemoryMarkerStore_Dir(t *testing.T) {
	store := NewInMemoryMarkerStore()
	
	if store.Dir() != "" {
		t.Errorf("InMemoryMarkerStore.Dir() should return empty string, got '%s'", store.Dir())
	}
}

func TestInMemoryMarkerStore_Concurrent(t *testing.T) {
	store := NewInMemoryMarkerStore()
	
	// Concurrent access
	done := make(chan bool, 20)
	for i := 0; i < 10; i++ {
		go func(id int) {
			_ = store.Create("session-" + string(rune('0'+id)))
			_, _ = store.Exists("session-" + string(rune('0'+id)))
			done <- true
		}(i)
		go func(id int) {
			_ = store.Delete("session-" + string(rune('0'+id)))
			done <- true
		}(i)
	}
	
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ========================================
// NewDefaultFileMarkerStore Tests
// ========================================

func TestNewDefaultFileMarkerStore(t *testing.T) {
	store := NewDefaultFileMarkerStore()
	
	if store == nil {
		t.Fatal("NewDefaultFileMarkerStore should not return nil")
	}
	
	// Should have a valid directory
	dir := store.Dir()
	if dir == "" {
		t.Error("Default marker store should have a directory")
	}
	
	// Should be able to create and delete markers
	err := store.Create("test-default")
	if err != nil {
		t.Fatalf("Failed to create marker: %v", err)
	}
	
	if !store.Exists("test-default") {
		t.Error("Marker should exist after Create")
	}
	
	err = store.Delete("test-default")
	if err != nil {
		t.Fatalf("Failed to delete marker: %v", err)
	}
}

func TestNewDefaultFileMarkerStore_Context(t *testing.T) {
	// Multiple calls should work (may share temp dir)
	store1 := NewDefaultFileMarkerStore()
	store2 := NewDefaultFileMarkerStore()
	
	// Both should be functional
	store1.Create("test-1")
	store2.Create("test-2")
	
	if !store1.Exists("test-1") || !store2.Exists("test-2") {
		t.Error("Both stores should function independently")
	}
}

// ========================================
// Integration Tests
// ========================================

func TestFileMarkerStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create and populate store
	store1, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	
	err = store1.Create("session-1")
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	err = store1.Create("session-2")
	if err != nil {
		t.Fatalf("Failed to create: %v", err)
	}
	
	// Create new store with same directory
	store2, err := NewFileMarkerStore(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create second store: %v", err)
	}
	
	// Should see existing markers
	if !store2.Exists("session-1") {
		t.Error("session-1 should persist across store instances")
	}
	if !store2.Exists("session-2") {
		t.Error("session-2 should persist across store instances")
	}
}

func TestInterfaceCompliance(t *testing.T) {
	// Compile-time interface verification
	var _ SessionMarkerStore = (*FileMarkerStore)(nil)
	var _ SessionMarkerStore = (*InMemoryMarkerStore)(nil)
	
	// Verify both implement all methods
	_ = func(s SessionMarkerStore) error {
		_ = s.Exists("test")
		_ = s.Create("test")
		_ = s.Delete("test")
		_ = s.Dir()
		return nil
	}
}
