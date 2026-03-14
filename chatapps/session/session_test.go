package session

import (
	"testing"

	"github.com/google/uuid"
)

// ========================================
// SessionManager Interface Tests
// ========================================

func TestDefaultSessionManager_Interface(t *testing.T) {
	// Verify DefaultSessionManager implements SessionManager
	var _ SessionManager = (*DefaultSessionManager)(nil)
}

// ========================================
// NewSessionManager Tests
// ========================================

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager("test-namespace")
	if sm == nil {
		t.Error("NewSessionManager should not return nil")
		return
	}
	if sm.namespace != "test-namespace" {
		t.Errorf("namespace = %s, want test-namespace", sm.namespace)
	}
}

func TestNewSessionManager_EmptyNamespace(t *testing.T) {
	sm := NewSessionManager("")
	if sm.namespace != "" {
		t.Errorf("namespace = %s, want empty", sm.namespace)
	}
}

// ========================================
// GetChatSessionID Tests
// ========================================

func TestGetChatSessionID(t *testing.T) {
	sm := NewSessionManager("test")
	
	// Same inputs should produce same ID
	id1 := sm.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	id2 := sm.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	
	if id1 != id2 {
		t.Errorf("Same inputs should produce same ID: %s != %s", id1, id2)
	}
	
	// Different inputs should produce different IDs
	id3 := sm.GetChatSessionID("slack", "U123", "Bot456", "C789", "T222")
	if id1 == id3 {
		t.Error("Different thread IDs should produce different session IDs")
	}
	
	// Different users should produce different IDs
	id4 := sm.GetChatSessionID("slack", "U999", "Bot456", "C789", "T111")
	if id1 == id4 {
		t.Error("Different user IDs should produce different session IDs")
	}
	
	// Different platforms should produce different IDs
	id5 := sm.GetChatSessionID("discord", "U123", "Bot456", "C789", "T111")
	if id1 == id5 {
		t.Error("Different platforms should produce different session IDs")
	}
}

func TestGetChatSessionID_Format(t *testing.T) {
	sm := NewSessionManager("test")
	
	id := sm.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	
	// Should be a valid UUID (SHA1 based)
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Errorf("GetChatSessionID should return valid UUID: %v", err)
	}
	
	// Should be SHA1 variant (version 5)
	if parsed.Version() != 5 {
		t.Errorf("UUID version = %v, want 5 (SHA1)", parsed.Version())
	}
}

// ========================================
// GenerateEngineSessionID Tests
// ========================================

func TestGenerateEngineSessionID(t *testing.T) {
	sm := NewSessionManager("test")
	
	id1 := sm.GenerateEngineSessionID()
	id2 := sm.GenerateEngineSessionID()
	
	// Each call should produce unique ID
	if id1 == id2 {
		t.Error("GenerateEngineSessionID should produce unique IDs")
	}
	
	// Should be valid UUID (version 4 = random)
	if id1.Version() != 4 {
		t.Errorf("EngineSessionID should be V4 (random), got %v", id1.Version())
	}
}

// ========================================
// GenerateProviderSessionID Tests
// ========================================

func TestGenerateProviderSessionID(t *testing.T) {
	sm := NewSessionManager("test")
	
	engineID := uuid.New()
	
	id1 := sm.GenerateProviderSessionID(engineID, "claude")
	id2 := sm.GenerateProviderSessionID(engineID, "claude")
	
	// Same inputs should produce same ID
	if id1 != id2 {
		t.Errorf("Same inputs should produce same ID: %s != %s", id1, id2)
	}
	
	// Different provider types should produce different IDs
	id3 := sm.GenerateProviderSessionID(engineID, "openai")
	if id1 == id3 {
		t.Error("Different provider types should produce different IDs")
	}
	
	// Different engine IDs should produce different IDs
	engineID2 := uuid.New()
	id4 := sm.GenerateProviderSessionID(engineID2, "claude")
	if id1 == id4 {
		t.Error("Different engine IDs should produce different IDs")
	}
}

func TestGenerateProviderSessionID_Format(t *testing.T) {
	sm := NewSessionManager("test")
	
	engineID := uuid.New()
	id := sm.GenerateProviderSessionID(engineID, "claude")
	
	// Should be string, not UUID (different format)
	if len(id) == 0 {
		t.Error("ProviderSessionID should not be empty")
	}
	
	// Should contain namespace
	if len(id) < len("test-namespace") {
		t.Error("ProviderSessionID seems too short")
	}
}

// ========================================
// CreateSessionContext Tests
// ========================================

func TestCreateSessionContext(t *testing.T) {
	sm := NewSessionManager("test")
	
	ctx := sm.CreateSessionContext(
		"slack",
		"U123",
		"Bot456",
		"C789",
		"T111",
		"claude",
	)
	
	// Verify all fields are populated
	if ctx.ChatSessionID == "" {
		t.Error("ChatSessionID should not be empty")
	}
	if ctx.ChatPlatform != "slack" {
		t.Errorf("ChatPlatform = %s, want slack", ctx.ChatPlatform)
	}
	if ctx.ChatUserID != "U123" {
		t.Errorf("ChatUserID = %s, want U123", ctx.ChatUserID)
	}
	if ctx.ChatBotUserID != "Bot456" {
		t.Errorf("ChatBotUserID = %s, want Bot456", ctx.ChatBotUserID)
	}
	if ctx.ChatChannelID != "C789" {
		t.Errorf("ChatChannelID = %s, want C789", ctx.ChatChannelID)
	}
	if ctx.ChatThreadID != "T111" {
		t.Errorf("ChatThreadID = %s, want T111", ctx.ChatThreadID)
	}
	if ctx.EngineSessionID == uuid.Nil {
		t.Error("EngineSessionID should not be nil")
	}
	if ctx.EngineNamespace != "test" {
		t.Errorf("EngineNamespace = %s, want test", ctx.EngineNamespace)
	}
	if ctx.ProviderSessionID == "" {
		t.Error("ProviderSessionID should not be empty")
	}
	if ctx.ProviderType != "claude" {
		t.Errorf("ProviderType = %s, want claude", ctx.ProviderType)
	}
}

func TestCreateSessionContext_Consistency(t *testing.T) {
	sm := NewSessionManager("test")
	
	// Create two contexts with same inputs
	ctx1 := sm.CreateSessionContext("slack", "U123", "Bot456", "C789", "T111", "claude")
	ctx2 := sm.CreateSessionContext("slack", "U123", "Bot456", "C789", "T111", "claude")
	
	// ChatSessionID should be deterministic (same inputs)
	if ctx1.ChatSessionID != ctx2.ChatSessionID {
		t.Errorf("ChatSessionID should be deterministic: %s != %s", ctx1.ChatSessionID, ctx2.ChatSessionID)
	}
	
	// But EngineSessionID should be unique each time
	if ctx1.EngineSessionID == ctx2.EngineSessionID {
		t.Error("EngineSessionID should be unique per call")
	}
	
	// ProviderSessionID depends on EngineSessionID, so should also be unique
	if ctx1.ProviderSessionID == ctx2.ProviderSessionID {
		t.Error("ProviderSessionID should be unique per call")
	}
}

func TestCreateSessionContext_EmptyInputs(t *testing.T) {
	sm := NewSessionManager("test")
	
	// Empty inputs should still work (might produce same IDs)
	ctx := sm.CreateSessionContext("", "", "", "", "", "")
	
	if ctx.ChatSessionID == "" {
		t.Error("ChatSessionID should be generated even with empty inputs")
	}
}

// ========================================
// SessionContext Tests
// ========================================

func TestSessionContext_Fields(t *testing.T) {
	sm := NewSessionManager("test")
	ctx := sm.CreateSessionContext("slack", "U123", "Bot456", "C789", "T111", "claude")
	
	// Verify all expected fields exist and are valid
	_ = ctx.ChatSessionID
	_ = ctx.ChatPlatform
	_ = ctx.ChatUserID
	_ = ctx.ChatBotUserID
	_ = ctx.ChatChannelID
	_ = ctx.ChatThreadID
	_ = ctx.EngineSessionID
	_ = ctx.EngineNamespace
	_ = ctx.ProviderSessionID
	_ = ctx.ProviderType
}

// ========================================
// Namespace Tests
// ========================================

func TestDifferentNamespaces(t *testing.T) {
	sm1 := NewSessionManager("namespace1")
	sm2 := NewSessionManager("namespace2")
	
	id1 := sm1.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	id2 := sm2.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	
	// Different namespaces should produce different IDs
	if id1 == id2 {
		t.Error("Different namespaces should produce different session IDs")
	}
}

func TestEmptyNamespace(t *testing.T) {
	sm := NewSessionManager("")
	
	id := sm.GetChatSessionID("slack", "U123", "Bot456", "C789", "T111")
	
	if id == "" {
		t.Error("Empty namespace should still produce valid ID")
	}
}
