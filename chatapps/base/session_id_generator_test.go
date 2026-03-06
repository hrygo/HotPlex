package base

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestGetOrCreateSession_CreatesNewSession(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	sessionID := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	if sessionID == "" {
		t.Error("Expected non-empty SessionID")
	}

	// Verify UUID5 format (36 chars with hyphens)
	if len(sessionID) != 36 {
		t.Errorf("Expected UUID5 format (36 chars), got %d", len(sessionID))
	}

	// Verify session is stored in the adapter's sessions map
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()

	session, ok := adapter.sessions["slack:U001:bot123:C12345:"]
	if !ok {
		t.Error("Expected session to be stored in adapter.sessions map")
	}

	// Verify session has correct fields
	if session.SessionID != sessionID {
		t.Errorf("Expected SessionID %s, got %s", sessionID, session.SessionID)
	}
	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
	if session.Platform != "slack" {
		t.Errorf("Expected Platform slack, got %s", session.Platform)
	}
}

func TestGetOrCreateSession_ReturnsExistingSession(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create initial session
	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	// Wait a tiny bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Get existing session
	sessionID2 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	if sessionID1 != sessionID2 {
		t.Errorf("Expected same SessionID, got %s vs %s", sessionID1, sessionID2)
	}

	// Verify LastActive was updated
	adapter.mu.RLock()
	session, ok := adapter.sessions["slack:U001:bot123:C12345:"]
	adapter.mu.RUnlock()

	if !ok {
		t.Fatal("Session not found")
	}

	// LastActive should be recent (within last second)
	if time.Since(session.LastActive) > time.Second {
		t.Errorf("Expected LastActive to be recently updated, got %v", session.LastActive)
	}
}

func TestGetOrCreateSession_Deterministic(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Call multiple times with same inputs
	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	sessionID2 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	sessionID3 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	if sessionID1 != sessionID2 || sessionID2 != sessionID3 {
		t.Errorf("Expected deterministic SessionIDs, got %s, %s, %s", sessionID1, sessionID2, sessionID3)
	}

	// Verify UUID5 format
	if len(sessionID1) != 36 {
		t.Errorf("Expected UUID5 format (36 chars), got %d", len(sessionID1))
	}
}

func TestGetOrCreateSession_EmptyBotUserID(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create session with empty botUserID (DM scenario)
	sessionID := adapter.GetOrCreateSession("U001", "", "", "")

	if sessionID == "" {
		t.Error("Expected non-empty SessionID for DM")
	}

	// Verify session is stored
	adapter.mu.RLock()
	_, ok := adapter.sessions["slack:U001:::"]
	adapter.mu.RUnlock()

	if !ok {
		t.Error("Expected session to be stored in adapter.sessions map")
	}
}

func TestGetOrCreateSession_EmptyChannelID(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("telegram", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create session with empty channelID (DM scenario)
	sessionID := adapter.GetOrCreateSession("user123", "bot456", "", "")

	if sessionID == "" {
		t.Error("Expected non-empty SessionID for DM")
	}

	// Verify session is stored
	adapter.mu.RLock()
	_, ok := adapter.sessions["telegram:user123:bot456::"]
	adapter.mu.RUnlock()

	if !ok {
		t.Error("Expected session to be stored in adapter.sessions map")
	}

	// Verify different from channel session
	sessionIDWithChannel := adapter.GetOrCreateSession("user123", "bot456", "chat789", "")
	if sessionID == sessionIDWithChannel {
		t.Error("Expected different SessionID for DM vs channel")
	}
}

func TestGetOrCreateSession_DifferentUsers(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	sessionID2 := adapter.GetOrCreateSession("U002", "bot123", "C12345", "")

	if sessionID1 == sessionID2 {
		t.Errorf("Expected different SessionIDs for different users, got same: %s", sessionID1)
	}
}

func TestGetOrCreateSession_DifferentBots(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	sessionID2 := adapter.GetOrCreateSession("U001", "bot456", "C12345", "")

	if sessionID1 == sessionID2 {
		t.Errorf("Expected different SessionIDs for different bots, got same: %s", sessionID1)
	}
}

func TestGetOrCreateSession_DifferentChannels(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	sessionID2 := adapter.GetOrCreateSession("U001", "bot123", "C67890", "")

	if sessionID1 == sessionID2 {
		t.Errorf("Expected different SessionIDs for different channels, got same: %s", sessionID1)
	}
}

func TestGetOrCreateSession_DifferentPlatforms(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	sessionID1 := adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	// Different platform - need a different adapter
	adapter2 := NewAdapter("telegram", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sessionID2 := adapter2.GetOrCreateSession("U001", "bot123", "C12345", "")

	if sessionID1 == sessionID2 {
		t.Errorf("Expected different SessionIDs for different platforms, got same: %s", sessionID1)
	}
}

func TestGetOrCreateSession_SessionRetrieval(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("discord", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create a session
	expectedSessionID := adapter.GetOrCreateSession("user789", "bot321", "channel123", "")

	// Retrieve the session using GetSession
	session, found := adapter.GetSession("discord:user789:bot321:channel123:")

	if !found {
		t.Error("Expected to find session via GetSession")
	}

	if session.SessionID != expectedSessionID {
		t.Errorf("Expected SessionID %s, got %s", expectedSessionID, session.SessionID)
	}

	if session.UserID != "user789" {
		t.Errorf("Expected UserID user789, got %s", session.UserID)
	}

	if session.Platform != "discord" {
		t.Errorf("Expected Platform discord, got %s", session.Platform)
	}
}

// Table-driven tests for edge cases
func TestGetOrCreateSession_EdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		platform  string
		userID    string
		botUserID string
		channelID string
	}{
		{"empty_userID", "slack", "", "bot123", "C12345"},
		{"empty_all", "slack", "", "", ""},
		{"special_chars_user", "slack", "U$#@001", "bot123", "C12345"},
		{"special_chars_channel", "slack", "U001", "bot123", "C-$#@"},
		{"very_long_ids", "slack", "U" + string(make([]byte, 100)), "bot123", "C12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewAdapter(tt.platform, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

			sessionID := adapter.GetOrCreateSession(tt.userID, tt.botUserID, tt.channelID, "")

			if sessionID == "" {
				t.Error("Expected non-empty SessionID")
			}

			// Verify session is stored
			key := fmt.Sprintf("%s:%s:%s:%s:", tt.platform, tt.userID, tt.botUserID, tt.channelID)
			adapter.mu.RLock()
			_, ok := adapter.sessions[key]
			adapter.mu.RUnlock()

			if !ok {
				t.Error("Expected session to be stored in adapter.sessions map")
			}
		})
	}
}

func TestGetOrCreateSession_MultipleSessions(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create multiple different sessions
	sessions := []struct {
		userID    string
		botUserID string
		channelID string
	}{
		{"U001", "bot123", "C12345"},
		{"U002", "bot123", "C12345"},
		{"U001", "bot456", "C12345"},
		{"U001", "bot123", "C67890"},
		{"U003", "", ""}, // DM
	}

	var sessionIDs []string
	for _, s := range sessions {
		sessionID := adapter.GetOrCreateSession(s.userID, s.botUserID, s.channelID, "")
		sessionIDs = append(sessionIDs, sessionID)
	}

	// All session IDs should be unique
	seen := make(map[string]bool)
	for _, id := range sessionIDs {
		if seen[id] {
			t.Errorf("Expected unique SessionID, got duplicate: %s", id)
		}
		seen[id] = true
	}

	// Verify all sessions are stored
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()

	if len(adapter.sessions) != len(sessions) {
		t.Errorf("Expected %d sessions, got %d", len(sessions), len(adapter.sessions))
	}
}

func TestUUID5Generator_Deterministic(t *testing.T) {
	t.Parallel()
	gen := NewUUID5Generator("hotplex")

	// Same inputs should produce same outputs
	id1 := gen.Generate("slack", "U001", "bot123", "C12345", "")
	id2 := gen.Generate("slack", "U001", "bot123", "C12345", "")

	if id1 != id2 {
		t.Errorf("Expected same ID, got %s vs %s", id1, id2)
	}
}

func TestUUID5Generator_DifferentInputs(t *testing.T) {
	t.Parallel()
	gen := NewUUID5Generator("hotplex")

	// Different user should produce different ID
	id1 := gen.Generate("slack", "U001", "bot123", "C12345", "")
	id2 := gen.Generate("slack", "U002", "bot123", "C12345", "")

	if id1 == id2 {
		t.Errorf("Expected different IDs for different users, got same: %s", id1)
	}

	// Different bot should produce different ID
	id3 := gen.Generate("slack", "U001", "bot456", "C12345", "")
	if id1 == id3 {
		t.Errorf("Expected different IDs for different bots, got same: %s", id1)
	}

	// Different channel should produce different ID
	id4 := gen.Generate("slack", "U001", "bot123", "C67890", "")
	if id1 == id4 {
		t.Errorf("Expected different IDs for different channels, got same: %s", id1)
	}

	// Different platform should produce different ID
	id5 := gen.Generate("telegram", "U001", "bot123", "C12345", "")
	if id1 == id5 {
		t.Errorf("Expected different IDs for different platforms, got same: %s", id1)
	}
}

func TestUUID5Generator_DMChannel(t *testing.T) {
	t.Parallel()
	gen := NewUUID5Generator("hotplex")

	// DM (empty channel) should work
	id1 := gen.Generate("slack", "U001", "bot123", "", "")
	id2 := gen.Generate("slack", "U001", "bot123", "", "")

	if id1 != id2 {
		t.Errorf("Expected same ID for DM, got %s vs %s", id1, id2)
	}

	// DM should be different from channel message
	id3 := gen.Generate("slack", "U001", "bot123", "C12345", "")
	if id1 == id3 {
		t.Errorf("Expected different IDs for DM vs channel, got same: %s", id1)
	}
}

func TestSimpleKeyGenerator(t *testing.T) {
	t.Parallel()
	gen := NewSimpleKeyGenerator()

	id := gen.Generate("slack", "U001", "bot123", "C12345", "")
	expected := "slack:U001:bot123:C12345:"

	if id != expected {
		t.Errorf("Expected %s, got %s", expected, id)
	}
}

func TestSimpleKeyGenerator_DMChannel(t *testing.T) {
	t.Parallel()
	gen := NewSimpleKeyGenerator()

	id := gen.Generate("slack", "U001", "bot123", "", "")
	expected := "slack:U001:bot123::"

	if id != expected {
		t.Errorf("Expected %s, got %s", expected, id)
	}
}

// =============================================================================
// FindSessionByUserAndChannel Tests
// =============================================================================

func TestFindSessionByUserAndChannel_FindsExisting(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create a session
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	// Find it
	session := adapter.FindSessionByUserAndChannel("U001", "C12345")

	if session == nil {
		t.Fatal("Expected to find session, got nil")
	}

	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
}

func TestFindSessionByUserAndChannel_NotFound(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create a session
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")

	// Try to find non-existent session
	session := adapter.FindSessionByUserAndChannel("U999", "C99999")

	if session != nil {
		t.Error("Expected nil for non-existent session")
	}
}

func TestFindSessionByUserAndChannel_MultipleSessions(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create sessions for different users in same channel
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U002", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U003", "bot123", "C12345", "")

	// Find U002's session - should return U002's session, not U001's
	session := adapter.FindSessionByUserAndChannel("U002", "C12345")

	if session == nil {
		t.Fatal("Expected to find session for U002, got nil")
	}

	if session.UserID != "U002" {
		t.Errorf("Expected UserID U002, got %s", session.UserID)
	}

	// Verify we don't get wrong user's session
	wrongSession := adapter.FindSessionByUserAndChannel("U001", "C12345")
	if wrongSession.UserID == "U002" {
		t.Error("Should not return wrong user's session")
	}
}

func TestFindSessionByUserAndChannel_DifferentChannels(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create sessions for same user in different channels
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U001", "bot123", "C67890", "")
	adapter.GetOrCreateSession("U001", "bot123", "C11111", "")

	// Find session in C67890
	session := adapter.FindSessionByUserAndChannel("U001", "C67890")

	if session == nil {
		t.Fatal("Expected to find session in C67890, got nil")
	}

	// Verify we get different sessions for different channels
	session1 := adapter.FindSessionByUserAndChannel("U001", "C12345")
	session2 := adapter.FindSessionByUserAndChannel("U001", "C67890")
	session3 := adapter.FindSessionByUserAndChannel("U001", "C11111")

	if session1 == nil || session2 == nil || session3 == nil {
		t.Fatal("Expected to find all sessions")
	}

	// All sessions belong to same user but different channels
	if session1.SessionID == session2.SessionID || session2.SessionID == session3.SessionID {
		t.Error("Expected different session IDs for different channels")
	}
}

func TestFindSessionByUserAndChannel_EmptyBotID(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create session with empty botUserID (DM scenario)
	// Key format: "platform:user:bot:channel" -> "slack:U001::C12345"
	adapter.GetOrCreateSession("U001", "", "C12345", "")

	// Find it - FindSessionByUserAndChannel matches userID and channelID
	session := adapter.FindSessionByUserAndChannel("U001", "C12345")

	if session == nil {
		t.Fatal("Expected to find session with empty botUserID, got nil")
	}

	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
}

func TestFindSessionByUserAndChannel_EmptyChannelID(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create session with empty channelID (DM scenario)
	adapter.GetOrCreateSession("U001", "bot123", "", "")

	// Find it
	session := adapter.FindSessionByUserAndChannel("U001", "")

	if session == nil {
		t.Fatal("Expected to find session with empty channelID, got nil")
	}

	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
}

func TestFindSessionByUserAndChannel_BothEmptyBotAndChannel(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create session with both empty botUserID and channelID
	adapter.GetOrCreateSession("U001", "", "", "")

	// Find it
	session := adapter.FindSessionByUserAndChannel("U001", "")

	if session == nil {
		t.Fatal("Expected to find session with empty bot and channel, got nil")
	}

	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
}

func TestFindSessionByUserAndChannel_DifferentBots(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create sessions for same user with different bots in same channel
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U001", "bot456", "C12345", "")

	// Find session - should return first match
	session := adapter.FindSessionByUserAndChannel("U001", "C12345")

	if session == nil {
		t.Fatal("Expected to find session, got nil")
	}

	// The method returns first match, which could be either bot
	// Verify it's one of the two sessions
	if session.UserID != "U001" {
		t.Errorf("Expected UserID U001, got %s", session.UserID)
	}
}

func TestFindSessionByUserAndChannel_EmptySessions(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Try to find session in empty adapter
	session := adapter.FindSessionByUserAndChannel("U001", "C12345")

	if session != nil {
		t.Error("Expected nil for empty adapter")
	}
}

// Table-driven tests for FindSessionByUserAndChannel
func TestFindSessionByUserAndChannel_TableDriven(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		userID       string
		channelID    string
		expectFound  bool
		expectedUser string
	}{
		{"find_user1_channel1", "U001", "C12345", true, "U001"},
		{"find_user2_channel1", "U002", "C12345", true, "U002"},
		{"find_user1_channel2", "U001", "C67890", true, "U001"},
		{"not_found_wrong_user", "U999", "C12345", false, ""},
		{"not_found_wrong_channel", "U001", "C99999", false, ""},
		{"not_found_both_wrong", "U999", "C99999", false, ""},
		{"empty_channel", "U001", "", true, "U001"},
	}

	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Setup sessions
	adapter.GetOrCreateSession("U001", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U002", "bot123", "C12345", "")
	adapter.GetOrCreateSession("U001", "bot123", "C67890", "")
	adapter.GetOrCreateSession("U001", "bot123", "", "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := adapter.FindSessionByUserAndChannel(tt.userID, tt.channelID)

			if tt.expectFound {
				if session == nil {
					t.Fatalf("Expected to find session for %s/%s, got nil", tt.userID, tt.channelID)
				}
				if session.UserID != tt.expectedUser {
					t.Errorf("Expected UserID %s, got %s", tt.expectedUser, session.UserID)
				}
			} else {
				if session != nil {
					t.Errorf("Expected nil for %s/%s, got session %s", tt.userID, tt.channelID, session.UserID)
				}
			}
		})
	}
}

func TestFindSessionByUserAndChannel_ThreadSafety(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Create multiple sessions
	for i := 0; i < 100; i++ {
		adapter.GetOrCreateSession(fmt.Sprintf("U%03d", i), "bot123", "C12345", "")
	}

	// Concurrent read operations
	var wg sync.WaitGroup
	errChan := make(chan error, 200)

	// Readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Search for random users
			for j := 0; j < 10; j++ {
				userID := fmt.Sprintf("U%03d", (idx+j)%100)
				session := adapter.FindSessionByUserAndChannel(userID, "C12345")
				if session == nil {
					errChan <- fmt.Errorf("expected to find session for %s", userID)
					return
				}
				if session.UserID != userID {
					errChan <- fmt.Errorf("expected UserID %s, got %s", userID, session.UserID)
					return
				}
			}
		}(i)
	}

	// Writers - also do some creates while reading
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Create new sessions while reading
			adapter.GetOrCreateSession(fmt.Sprintf("W%03d", idx), "bot123", "C54321", "")
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Error(err.Error())
	}
}

func TestFindSessionByUserAndChannel_RaceDetector(t *testing.T) {
	t.Parallel()
	// This test is specifically designed to be run with -race flag
	// It creates race conditions between reads and writes
	if testing.Short() {
		t.Skip("Skipping race detector test in short mode")
	}

	adapter := NewAdapter("slack", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Continuously create sessions
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				adapter.GetOrCreateSession(fmt.Sprintf("U%05d", i), "bot123", fmt.Sprintf("C%05d", i%10), "")
				i++
			}
		}
	}()

	// Continuously search sessions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-stop:
				return
			default:
				adapter.FindSessionByUserAndChannel(fmt.Sprintf("U%05d", i%100), fmt.Sprintf("C%05d", i%10))
			}
		}
	}()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
