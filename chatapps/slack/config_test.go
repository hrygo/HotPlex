package slack

import (
	"fmt"
	"testing"
)

func TestConfig_IsUserAllowed(t *testing.T) {
	tests := []struct {
		name          string
		allowedUsers  []string
		blockedUsers  []string
		testUserID    string
		expectedAllow bool
		description   string
	}{
		{
			name:          "empty_lists_all_allowed",
			testUserID:    "U123456",
			expectedAllow: true,
			description:   "No lists configured - all users allowed",
		},
		{
			name:          "user_not_in_empty_blocked",
			blockedUsers:  []string{},
			testUserID:    "U123456",
			expectedAllow: true,
			description:   "Empty blocked list - user allowed",
		},
		{
			name:          "user_in_blocked_list",
			blockedUsers:  []string{"U123456", "U789012"},
			testUserID:    "U123456",
			expectedAllow: false,
			description:   "User in blocked list - denied",
		},
		{
			name:          "user_not_in_blocked_list",
			blockedUsers:  []string{"U789012", "U345678"},
			testUserID:    "U123456",
			expectedAllow: true,
			description:   "User not in blocked list - allowed",
		},
		{
			name:          "blocked_takes_priority",
			allowedUsers:  []string{"U123456", "U789012"},
			blockedUsers:  []string{"U123456"},
			testUserID:    "U123456",
			expectedAllow: false,
			description:   "User in both lists - blocked takes priority",
		},
		{
			name:          "user_in_allowed_list",
			allowedUsers:  []string{"U123456", "U789012"},
			testUserID:    "U123456",
			expectedAllow: true,
			description:   "User in allowed list - permitted",
		},
		{
			name:          "user_not_in_allowed_list",
			allowedUsers:  []string{"U123456", "U789012"},
			testUserID:    "U999999",
			expectedAllow: false,
			description:   "User not in allowed list - denied",
		},
		{
			name:          "allowed_list_with_no_blocked",
			allowedUsers:  []string{"U123456"},
			testUserID:    "U123456",
			expectedAllow: true,
			description:   "Only allowed list configured - user in list",
		},
		{
			name:          "allowed_list_blocks_others",
			allowedUsers:  []string{"U123456"},
			testUserID:    "U999999",
			expectedAllow: false,
			description:   "Only allowed list configured - user not in list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AllowedUsers: tt.allowedUsers,
				BlockedUsers: tt.blockedUsers,
			}

			result := cfg.IsUserAllowed(tt.testUserID)

			if result != tt.expectedAllow {
				t.Errorf("IsUserAllowed(%q) = %v, want %v\nScenario: %s",
					tt.testUserID, result, tt.expectedAllow, tt.description)
			}
		})
	}
}

func TestConfig_ShouldProcessChannel(t *testing.T) {
	tests := []struct {
		name            string
		dmPolicy        string
		groupPolicy     string
		channelType     string
		channelID       string
		expectedProcess bool
		description     string
	}{
		{
			name:            "dm_allow_policy",
			dmPolicy:        "allow",
			channelType:     "dm",
			expectedProcess: true,
			description:     "DM policy 'allow' - process message",
		},
		{
			name:            "dm_block_policy",
			dmPolicy:        "block",
			channelType:     "dm",
			expectedProcess: false,
			description:     "DM policy 'block' - reject message",
		},
		{
			name:            "dm_pairing_policy",
			dmPolicy:        "pairing",
			channelType:     "dm",
			expectedProcess: false, // Unpaired users are rejected
			description:     "DM policy 'pairing' - reject unpaired user (now implemented)",
		},
		{
			name:            "dm_default_policy",
			dmPolicy:        "",
			channelType:     "dm",
			expectedProcess: true,
			description:     "DM policy empty (default) - process message",
		},
		{
			name:            "channel_allow_policy",
			groupPolicy:     "allow",
			channelType:     "channel",
			expectedProcess: true,
			description:     "Group policy 'allow' - process message",
		},
		{
			name:            "channel_block_policy",
			groupPolicy:     "block",
			channelType:     "channel",
			expectedProcess: false,
			description:     "Group policy 'block' - reject message",
		},
		{
			name:            "channel_mention_policy",
			groupPolicy:     "mention",
			channelType:     "channel",
			expectedProcess: true,
			description:     "Group policy 'mention' - process (TODO: implement mention check)",
		},
		{
			name:            "group_allow_policy",
			groupPolicy:     "allow",
			channelType:     "group",
			expectedProcess: true,
			description:     "Group policy 'allow' for private groups - process message",
		},
		{
			name:            "group_block_policy",
			groupPolicy:     "block",
			channelType:     "group",
			expectedProcess: false,
			description:     "Group policy 'block' for private groups - reject message",
		},
		{
			name:            "group_default_policy",
			groupPolicy:     "",
			channelType:     "group",
			expectedProcess: true,
			description:     "Group policy empty (default) - process message",
		},
		{
			name:            "unknown_channel_type",
			channelType:     "unknown",
			expectedProcess: true,
			description:     "Unknown channel type - default to allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				DMPolicy:    tt.dmPolicy,
				GroupPolicy: tt.groupPolicy,
			}

			result := cfg.ShouldProcessChannel(tt.channelType, tt.channelID)

			if result != tt.expectedProcess {
				t.Errorf("ShouldProcessChannel(%q, %q) = %v, want %v\nScenario: %s",
					tt.channelType, tt.channelID, result, tt.expectedProcess, tt.description)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_http_mode",
			config: Config{
				BotToken:      "xoxb-0-0-testtoken",
				SigningSecret: "testsigningsecret32characterslong",
				Mode:          "http",
			},
			expectError: false,
		},
		{
			name: "valid_socket_mode",
			config: Config{
				BotToken: "xoxb-0-0-testtoken",
				AppToken: "xapp-0-0-testtoken",
				Mode:     "socket",
			},
			expectError: false,
		},
		{
			name: "valid_socket_mode_new_format",
			config: Config{
				// New 4-part format: xapp-{num}-{alnum}-{num}-{alnum}
				BotToken: "xoxb-0-0-testtoken",
				AppToken: "xapp-0-test-0-token",
				Mode:     "socket",
			},
			expectError: false,
		},
		{
			name: "valid_default_mode",
			config: Config{
				BotToken:      "xoxb-0-0-testtoken",
				SigningSecret: "testsigningsecret32characterslong",
				Mode:          "",
			},
			expectError: false,
		},
		{
			name: "missing_bot_token",
			config: Config{
				SigningSecret: "testsigningsecret32characterslong",
			},
			expectError: true,
			errorMsg:    "bot token is required",
		},
		{
			name: "invalid_bot_token_format",
			config: Config{
				BotToken:      "invalid-token",
				SigningSecret: "testsigningsecret32characterslong",
			},
			expectError: true,
			errorMsg:    "invalid bot token format",
		},
		{
			name: "http_mode_missing_signing_secret",
			config: Config{
				BotToken: "xoxb-0-0-testtoken",
				Mode:     "http",
			},
			expectError: true,
			errorMsg:    "signing secret is required for HTTP mode",
		},
		{
			name: "http_mode_short_signing_secret",
			config: Config{
				BotToken:      "xoxb-0-0-testtoken",
				SigningSecret: "short",
				Mode:          "http",
			},
			expectError: true,
			errorMsg:    "signing secret too short",
		},
		{
			name: "socket_mode_missing_app_token",
			config: Config{
				BotToken: "xoxb-0-0-testtoken",
				Mode:     "socket",
			},
			expectError: true,
			errorMsg:    "app token is required for Socket mode",
		},
		{
			name: "invalid_mode",
			config: Config{
				BotToken: "xoxb-0-0-testtoken",
				Mode:     "invalid",
			},
			expectError: true,
			errorMsg:    "invalid mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError && err == nil {
				t.Errorf("Validate() expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}

			if tt.expectError && err != nil && tt.errorMsg != "" {
				if err.Error() != tt.errorMsg && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Validate() error = %q, want message containing %q", err.Error(), tt.errorMsg)
				}
			}
		})
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfig_MarkPaired(t *testing.T) {
	cfg := &Config{}

	// Test marking a user as paired
	cfg.MarkPaired("U123456")

	// Verify the user is paired
	if !cfg.isPaired("U123456") {
		t.Error("Expected user U123456 to be paired")
	}

	// Verify other users are not paired
	if cfg.isPaired("U999999") {
		t.Error("Expected user U999999 to not be paired")
	}
}

func TestConfig_isPaired_UnpairedUser(t *testing.T) {
	cfg := &Config{}

	// Test with nil pairedUsers map
	if cfg.isPaired("U123456") {
		t.Error("Expected unpaired user to return false")
	}
}

func TestConfig_ContainsBotMention(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		botID    string
		expected bool
	}{
		{"exact_match", "Hello <@U1234567890>", "U1234567890", true},
		{"mention_at_start", "<@U1234567890> help me", "U1234567890", true},
		{"multiple_mentions", "<@U1234567890> can you <@U1234567890> help?", "U1234567890", true},
		{"similar_but_different_id", "Hello <@U12345678901>", "U1234567890", false},
		{"bot_id_in_text_not_mention", "The bot ID is U1234567890", "U1234567890", false},
		{"no_mention", "Hello bot", "U1234567890", false},
		{"empty_bot_id", "<@U1234567890>", "", false},
		{"mention_with_exclamation", "<!here> and <@U1234567890>", "U1234567890", true},
		{"partial_id_no_match", "<@U123456>", "U1234567890", false},
		{"longer_id_no_match", "<@U123456789012345>", "U1234567890", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{BotUserID: tt.botID}
			result := cfg.ContainsBotMention(tt.text)
			if result != tt.expected {
				t.Errorf("ContainsBotMention(%q) with botID=%q = %v, want %v",
					tt.text, tt.botID, result, tt.expected)
			}
		})
	}
}

func TestConfig_MarkPaired_Concurrent(t *testing.T) {
	cfg := &Config{pairing: &pairingState{users: make(map[string]bool)}}
	done := make(chan bool)
	userCount := 100

	for i := 0; i < userCount; i++ {
		go func(id int) {
			cfg.MarkPaired(fmt.Sprintf("U%d", id))
			done <- true
		}(i)
	}

	for i := 0; i < userCount; i++ {
		<-done
	}

	for i := 0; i < userCount; i++ {
		if !cfg.isPaired(fmt.Sprintf("U%d", i)) {
			t.Errorf("Expected user U%d to be paired", i)
		}
	}
}

func TestConfig_ConcurrentAccess(t *testing.T) {
	cfg := &Config{
		AllowedUsers: []string{"U1", "U2", "U3"},
		BlockedUsers: []string{"U4", "U5"},
		pairing:      &pairingState{users: make(map[string]bool)},
	}
	done := make(chan bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		go func(id int) {
			_ = cfg.IsUserAllowed(fmt.Sprintf("U%d", id%10))
			done <- true
		}(i)
		go func(id int) {
			cfg.MarkPaired(fmt.Sprintf("PairedU%d", id))
			done <- true
		}(i)
	}

	for i := 0; i < iterations*2; i++ {
		<-done
	}
}

func TestExtractMentionedUsers(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "no mentions",
			text:     "hello world",
			expected: nil,
		},
		{
			name:     "single mention",
			text:     "<@U1234567890> hello",
			expected: []string{"U1234567890"},
		},
		{
			name:     "multiple mentions",
			text:     "<@U1111111111> <@U2222222222> hi",
			expected: []string{"U1111111111", "U2222222222"},
		},
		{
			name:     "mention with bang prefix",
			text:     "<@!U1234567890> hello",
			expected: []string{"U1234567890"},
		},
		{
			name:     "mixed mentions",
			text:     "<@U1111111111> <@!U2222222222> hi",
			expected: []string{"U1111111111", "U2222222222"},
		},
		{
			name:     "duplicate mentions",
			text:     "<@U1234567890> <@U1234567890> hi",
			expected: []string{"U1234567890", "U1234567890"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractMentionedUsers(tt.text)
			if !equalSlices(result, tt.expected) {
				t.Errorf("ExtractMentionedUsers() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShouldRespondInMultibotMode(t *testing.T) {
	cfg := &Config{BotUserID: "U9999999999"}

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "no mentions - broadcast",
			text:     "hello world",
			expected: true,
		},
		{
			name:     "mentioned self",
			text:     "<@U9999999999> help me",
			expected: true,
		},
		{
			name:     "mentioned self with bang",
			text:     "<@!U9999999999> help me",
			expected: true,
		},
		{
			name:     "mentioned other bot",
			text:     "<@U8888888888> help me",
			expected: false,
		},
		{
			name:     "mentioned multiple including self",
			text:     "<@U8888888888> <@U9999999999> help",
			expected: true,
		},
		{
			name:     "mentioned multiple excluding self",
			text:     "<@U7777777777> <@U8888888888> help",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.ShouldRespondInMultibotMode(tt.text)
			if result != tt.expected {
				t.Errorf("ShouldRespondInMultibotMode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsBroadcastMessage(t *testing.T) {
	cfg := &Config{BotUserID: "U9999999999"}

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "no mentions - broadcast",
			text:     "hello world",
			expected: true,
		},
		{
			name:     "has mention - not broadcast",
			text:     "<@U9999999999> hello",
			expected: false,
		},
		{
			name:     "multiple mentions - not broadcast",
			text:     "<@U1111111111> <@U2222222222> hi",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.IsBroadcastMessage(tt.text)
			if result != tt.expected {
				t.Errorf("IsBroadcastMessage() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestConfig_CanRespond(t *testing.T) {
	tests := []struct {
		name     string
		owner    *OwnerConfig
		userID   string
		expected bool
	}{
		{
			name:     "no_owner_config_defaults_to_public",
			owner:    nil,
			userID:   "U1",
			expected: true,
		},
		{
			name: "primary_owner_always_allowed",
			owner: &OwnerConfig{
				Primary: "U1",
				Policy:  "owner_only",
			},
			userID:   "U1",
			expected: true,
		},
		{
			name: "trusted_user_allowed_in_trusted_policy",
			owner: &OwnerConfig{
				Primary: "U1",
				Trusted: []string{"U2", "U3"},
				Policy:  "trusted",
			},
			userID:   "U2",
			expected: true,
		},
		{
			name: "non_trusted_user_blocked_in_trusted_policy",
			owner: &OwnerConfig{
				Primary: "U1",
				Trusted: []string{"U2"},
				Policy:  "trusted",
			},
			userID:   "U3",
			expected: false,
		},
		{
			name: "public_policy_allows_all",
			owner: &OwnerConfig{
				Primary: "U1",
				Policy:  "public",
			},
			userID:   "U99",
			expected: true,
		},
		{
			name: "owner_only_blocks_others",
			owner: &OwnerConfig{
				Primary: "U1",
				Trusted: []string{"U2"}, // Even if in trusted list, policy is owner_only
				Policy:  "owner_only",
			},
			userID:   "U2",
			expected: false,
		},
		{
			name: "unknown_policy_fails_secure",
			owner: &OwnerConfig{
				Primary: "U1",
				Policy:  "something_else",
			},
			userID:   "U1",
			expected: true, // Owner still allowed
		},
		{
			name: "unknown_policy_fails_secure_for_others",
			owner: &OwnerConfig{
				Primary: "U1",
				Policy:  "something_else",
			},
			userID:   "U2",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Owner: tt.owner}
			result := cfg.CanRespond(tt.userID)
			if result != tt.expected {
				t.Errorf("CanRespond(%q) = %v, want %v", tt.userID, result, tt.expected)
			}
		})
	}
}
