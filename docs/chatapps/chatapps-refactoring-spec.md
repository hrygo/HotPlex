# ChatApps Package DRY SOLID Architecture Refactoring Specification

## Architecture Design Goals

The refactored architecture should achieve the following layered design:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Application Layer                            │
│  (User code, CLI commands, web applications)                   │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                  ChatApps Integration Layer                     │
│  - EngineHolder (interface-based)                               │
│  - AdapterManager (platform-agnostic)                           │
│  - Message routing and session management                       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                Platform Adapters Layer                          │
│  - Base adapter with common functionality                       │
│  - Platform-specific implementations (Slack, Telegram, etc.)    │
│  - Interface-contract based operations                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Engine Abstraction Layer                     │
│  - Engine interface (not concrete implementation)               │
│  - Session management abstraction                              │
│  - Event callback abstraction                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Provider/Engine Layer                        │
│  - Concrete engine implementations (ClaudeCode, OpenCode, etc.) │
│  - Session pool management                                     │
│  - Security and execution logic                                │
└─────────────────────────────────────────────────────────────────┘
```

### Key Principles

1. **Dependency Inversion**: High-level modules should not depend on low-level modules; both should depend on abstractions
2. **Interface Segregation**: Create client-specific interfaces rather than one general-purpose interface
3. **DRY**: Eliminate duplicate code and logic across platform adapters
4. **SOLID Compliance**: Each component should have a single responsibility and be open for extension but closed for modification
5. **Platform Agnosticism**: Core chatapps logic should not contain platform-specific code or type assertions

## SPEC Task List

### SPEC-001: Extract Engine Interface Abstraction

**Description**: Create an interface that abstracts the engine functionality currently hardcoded as `*engine.Engine` in the chatapps package.

**Acceptance Criteria**:
- [ ] `Engine` interface defined in `chatapps/types.go`
- [ ] All references to `*engine.Engine` in `chatapps/` package replaced with `Engine` interface
- [ ] `EngineHolder` struct uses `Engine` interface instead of concrete `*engine.Engine`
- [ ] `EngineMessageHandler` uses `Engine` interface for execution
- [ ] Tests pass without modification to engine functionality

**Dependencies**: None

**Implementation**:
```go
// Define Engine interface in chatapps/types.go
type Engine interface {
    Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error
    GetSession(sessionID string) (Session, bool)
    Close() error
    // Add other necessary methods
}

// Update EngineHolder to use interface
type EngineHolder struct {
    engine           Engine  // Changed from *engine.Engine
    logger           *slog.Logger
    adapters         *AdapterManager
    defaultWorkDir   string
    defaultTaskInstr string
}
```

### SPEC-002: Define Platform-Specific Operations Interface

**Description**: Create interfaces for platform-specific operations that are currently accessed via Slack type assertions.

**Acceptance Criteria**:
- [ ] `MessageOperations` interface defined with `DeleteMessage`, `AddReaction`, `UpdateMessage` methods
- [ ] `SessionOperations` interface defined with `GetSession`, `FindSessionByUserAndChannel` methods  
- [ ] All Slack-specific type assertions in `engine_handler.go` replaced with interface calls
- [ ] Other adapters (Telegram, Discord, etc.) implement these interfaces where applicable
- [ ] No platform-specific code remains in `engine_handler.go`

**Dependencies**: SPEC-001

**Implementation**:
```go
// Define interfaces in chatapps/base/types.go
type MessageOperations interface {
    DeleteMessage(ctx context.Context, channelID, messageTS string) error
    AddReaction(ctx context.Context, reaction base.Reaction) error
    UpdateMessage(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error
}

type SessionOperations interface {
    GetSession(key string) (*base.Session, bool)
    FindSessionByUserAndChannel(userID, channelID string) *base.Session
}

// Update StreamCallback to accept these interfaces
type StreamCallback struct {
    ctx              context.Context
    sessionID        string
    platform         string
    adapters         *AdapterManager
    logger           *slog.Logger
    // ... other fields
    messageOps       MessageOperations  // Injected dependency
    sessionOps       SessionOperations  // Injected dependency
}
```

### SPEC-003: Implement Interface Compliance in All Adapters

**Description**: Ensure all platform adapters implement the required interfaces for platform-specific operations.

**Acceptance Criteria**:
- [ ] Slack adapter implements `MessageOperations` and `SessionOperations` interfaces
- [ ] Telegram adapter implements supported interfaces (with appropriate no-op or error implementations)
- [ ] Discord adapter implements supported interfaces
- [ ] WhatsApp adapter implements supported interfaces  
- [ ] DingTalk adapter implements supported interfaces
- [ ] All adapters compile without errors
- [ ] Interface compliance verified at compile time

**Dependencies**: SPEC-002

**Implementation**:
```go
// In slack/adapter.go
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
    return a.DeleteMessageSDK(ctx, channelID, messageTS)
}

func (a *Adapter) AddReaction(ctx context.Context, reaction base.Reaction) error {
    return a.AddReactionSDK(ctx, reaction)
}

// In telegram/adapter.go (example for unsupported operation)
func (a *Adapter) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
    // Telegram may not support message deletion, return appropriate error or no-op
    return nil // or return fmt.Errorf("delete message not supported")
}
```

### SPEC-004: Refactor StreamCallback to Use Dependency Injection

**Description**: Modify `StreamCallback` to accept platform-specific operations as injected dependencies rather than performing type assertions.

**Acceptance Criteria**:
- [ ] `NewStreamCallback` function accepts `MessageOperations` and `SessionOperations` parameters
- [ ] All hard-coded Slack type assertions removed from `setReaction`, `scheduleDeleteStartingMessage`, `enforceSlidingWindow`, `scheduleDeleteActionMessages`, and `handleAnswer` functions
- [ ] Functions call interface methods instead of type-asserted concrete methods
- [ ] `engine_handler.go` compiles without errors
- [ ] All existing functionality preserved

**Dependencies**: SPEC-002, SPEC-003

**Implementation**:
```go
// Update NewStreamCallback signature
func NewStreamCallback(
    ctx context.Context, 
    sessionID, platform string, 
    adapters *AdapterManager, 
    logger *slog.Logger, 
    metadata map[string]any,
    messageOps MessageOperations,      // Added parameter
    sessionOps SessionOperations,     // Added parameter
) *StreamCallback {
    cb := &StreamCallback{
        ctx:          ctx,
        sessionID:    sessionID,
        platform:     platform,
        adapters:     adapters,
        logger:       logger,
        metadata:     metadata,
        messageOps:   messageOps,       // Store dependency
        sessionOps:   sessionOps,       // Store dependency
        processor:    NewDefaultProcessorChain(logger),
    }
    // ... rest of initialization
    return cb
}

// Update setReaction to use interface
func (c *StreamCallback) setReaction(emoji string) {
    if c.reactionChannelID == "" || c.reactionMessageTS == "" {
        return
    }
    
    // Remove type assertion, use injected interface
    if c.messageOps == nil {
        return
    }
    
    // Remove previous reaction
    if c.currentReaction != "" && c.currentReaction != emoji {
        prevReaction := base.Reaction{
            Name:      c.currentReaction,
            Channel:   c.reactionChannelID,
            Timestamp: c.reactionMessageTS,
        }
        _ = c.messageOps.RemoveReaction(c.ctx, prevReaction)
    }
    
    // Add new reaction
    newReaction := base.Reaction{
        Name:      emoji,
        Channel:   c.reactionChannelID,
        Timestamp: c.reactionMessageTS,
    }
    if err := c.messageOps.AddReaction(c.ctx, newReaction); err == nil {
        c.currentReaction = emoji
    } else {
        c.logger.Warn("Failed to set reaction", "emoji", emoji, "error", err)
    }
}
```

### SPEC-005: Update AdapterManager to Provide Interface Access

**Description**: Modify `AdapterManager` to provide access to platform-specific operations interfaces.

**Acceptance Criteria**:
- [ ] `AdapterManager` provides methods to get `MessageOperations` and `SessionOperations` for a platform
- [ ] Methods return appropriate interfaces or nil if not supported
- [ ] Safe interface type assertions performed only in `AdapterManager`
- [ ] `engine_handler.go` uses `AdapterManager` methods to get required interfaces
- [ ] No direct type assertions remain in application code

**Dependencies**: SPEC-002, SPEC-003, SPEC-004

**Implementation**:
```go
// Add methods to AdapterManager
func (m *AdapterManager) GetMessageOperations(platform string) MessageOperations {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    adapter, ok := m.adapters[platform]
    if !ok {
        return nil
    }
    
    // Safe type assertion here only
    if ops, ok := adapter.(MessageOperations); ok {
        return ops
    }
    return nil
}

func (m *AdapterManager) GetSessionOperations(platform string) SessionOperations {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    adapter, ok := m.adapters[platform]
    if !ok {
        return nil
    }
    
    if ops, ok := adapter.(SessionOperations); ok {
        return ops
    }
    return nil
}

// Update EngineMessageHandler to get interfaces from AdapterManager
func (h *EngineMessageHandler) Handle(ctx context.Context, msg *ChatMessage) error {
    // ... existing code ...
    
    // Get platform-specific operations
    messageOps := h.adapters.GetMessageOperations(msg.Platform)
    sessionOps := h.adapters.GetSessionOperations(msg.Platform)
    
    // Create stream callback with injected dependencies
    callback := NewStreamCallback(
        ctx, msg.SessionID, msg.Platform, h.adapters, h.logger, msg.Metadata,
        messageOps, sessionOps,  // Inject dependencies
    )
    // ... rest of method
}
```

### SPEC-006: Consolidate Message Type Definitions

**Description**: Standardize message type handling and eliminate redundant type definitions across adapters.

**Acceptance Criteria**:
- [ ] Single source of truth for `MessageType` enum in `base/types.go`
- [ ] All adapters use the same `MessageType` definitions
- [ ] Platform-specific message builders handle type conversion internally
- [ ] No duplicate message type definitions in individual adapters
- [ ] Message type handling is consistent across all platforms

**Dependencies**: None (can be done independently)

**Implementation**:
```go
// Keep MessageType in base/types.go as the single source of truth
// Remove any duplicate definitions from platform-specific files

// Update platform-specific message builders to use base.MessageType
// Example in slack/builder.go:
func (b *MessageBuilder) Build(msg *base.ChatMessage) []slack.Block {
    switch msg.Type {
    case base.MessageTypeThinking:
        return b.BuildThinkingMessage(msg)
    case base.MessageTypeToolUse:
        return b.BuildToolUseMessage(msg)
    // ... all cases use base.MessageType
    }
}
```

### SPEC-007: Implement Graceful Fallback for Unsupported Operations

**Description**: Ensure that when a platform doesn't support certain operations, the system gracefully handles it without errors.

**Acceptance Criteria**:
- [ ] All interface methods have clear contracts for unsupported operations
- [ ] `StreamCallback` handles nil interface implementations gracefully
- [ ] Log appropriate warnings when operations are not supported
- [ ] System continues to function even when some operations are not available
- [ ] User experience degrades gracefully (no crashes or missing core functionality)

**Dependencies**: SPEC-004, SPEC-005

**Implementation**:
```go
// In StreamCallback methods, add nil checks
func (c *StreamCallback) setReaction(emoji string) {
    if c.reactionChannelID == "" || c.reactionMessageTS == "" {
        return
    }
    
    // Handle case where platform doesn't support reactions
    if c.messageOps == nil {
        c.logger.Debug("Reactions not supported on this platform", "platform", c.platform)
        return
    }
    
    // Proceed with reaction logic...
}

// In AdapterManager, return nil for unsupported interfaces
func (m *AdapterManager) GetMessageOperations(platform string) MessageOperations {
    // ... type assertion logic ...
    // If adapter doesn't implement MessageOperations, return nil
    return nil
}
```

### SPEC-008: Update Setup and Initialization Logic

**Description**: Refactor the setup and initialization logic to use the new interface-based architecture.

**Acceptance Criteria**:
- [ ] `setup.go` no longer contains Slack-specific type assertions
- [ ] Engine registration works through interfaces rather than concrete types
- [ ] Platform initialization is consistent across all adapters
- [ ] No direct coupling between setup logic and specific adapter implementations
- [ ] All platforms initialize correctly with the new architecture

**Dependencies**: SPEC-001, SPEC-005

**Implementation**:
```go
// Remove Slack-specific SetEngine call from setup.go
// Instead, if engine integration is needed, use interface-based approach

// Example: Instead of this (current code):
// if slackAdapter, ok := adapter.(*slack.Adapter); ok {
//     slackAdapter.SetEngine(eng)
// }

// Use interface-based engine interaction through callbacks
// The engine interaction should happen through the message handler callback chain
```

### SPEC-009: Comprehensive Testing Strategy

**Description**: Implement comprehensive tests to ensure the refactored architecture works correctly across all platforms.

**Acceptance Criteria**:
- [ ] Unit tests for all new interfaces
- [ ] Integration tests for `StreamCallback` with mocked platform operations
- [ ] Tests verify that type assertions are eliminated
- [ ] Cross-platform compatibility tests
- [ ] Backward compatibility tests (existing functionality unchanged)
- [ ] All tests pass successfully

**Dependencies**: All previous SPECs

**Implementation**:
```go
// Create mock implementations for testing
type MockMessageOperations struct {
    DeleteMessageFunc func(ctx context.Context, channelID, messageTS string) error
    AddReactionFunc   func(ctx context.Context, reaction base.Reaction) error
    UpdateMessageFunc func(ctx context.Context, channelID, messageTS string, msg *base.ChatMessage) error
}

func (m *MockMessageOperations) DeleteMessage(ctx context.Context, channelID, messageTS string) error {
    if m.DeleteMessageFunc != nil {
        return m.DeleteMessageFunc(ctx, channelID, messageTS)
    }
    return nil
}

// Test StreamCallback with mocks
func TestStreamCallback_SetReaction(t *testing.T) {
    mockOps := &MockMessageOperations{
        AddReactionFunc: func(ctx context.Context, reaction base.Reaction) error {
            // Verify correct reaction parameters
            assert.Equal(t, "brain", reaction.Name)
            return nil
        },
    }
    
    callback := NewStreamCallback(ctx, "session1", "slack", adapters, logger, metadata, mockOps, nil)
    callback.setReaction("brain")
    // Verify mock was called correctly
}
```

### SPEC-010: Documentation and Migration Guide

**Description**: Create comprehensive documentation for the new architecture and migration guide for existing code.

**Acceptance Criteria**:
- [ ] Architecture diagram showing the new layered design
- [ ] Interface definitions documented with examples
- [ ] Migration guide for existing integrations
- [ ] Best practices for implementing new platform adapters
- [ ] Troubleshooting guide for common issues
- [ ] All documentation is accurate and up-to-date

**Dependencies**: All previous SPECs

**Implementation**:
- Create architecture diagrams using Mermaid or similar
- Document each interface with usage examples
- Provide before/after code examples for migration
- Include adapter implementation checklist

## SPEC Dependency Graph

```
SPEC-001 ──┐
           ├─→ SPEC-005 ──→ SPEC-008
SPEC-002 ──┼─→ SPEC-004 ──→ SPEC-007
           ├─→ SPEC-003 ──┘
           └─→ SPEC-006

SPEC-009 ←─ All previous SPECs
SPEC-010 ←─ All previous SPECs
```

## Execution Order

1. **Phase 1: Foundation** (SPEC-001, SPEC-002, SPEC-006)
   - Extract interfaces and define contracts
   - Consolidate message type definitions
   
2. **Phase 2: Implementation** (SPEC-003, SPEC-004, SPEC-005, SPEC-008)
   - Implement interface compliance in all adapters
   - Refactor StreamCallback and AdapterManager
   - Update setup logic
   
3. **Phase 3: Robustness** (SPEC-007)
   - Implement graceful fallbacks for unsupported operations
   
4. **Phase 4: Validation** (SPEC-009, SPEC-010)
   - Comprehensive testing and documentation

## Acceptance Criteria Verification

Each SPEC includes specific, testable acceptance criteria that can be verified through:

1. **Static Analysis**: Compile-time interface compliance checks
2. **Code Review**: Manual verification that type assertions are eliminated
3. **Unit Tests**: Automated tests for individual components
4. **Integration Tests**: End-to-end tests across platforms
5. **Manual Testing**: Verification of user-facing functionality

The refactored architecture will be considered complete when:
- All P0 critical issues (hard-coded Slack type assertions) are resolved
- All P1 high issues (Engine coupling) are resolved  
- Clean architecture principles are consistently applied
- All platforms work correctly with the new interface-based design
- No breaking changes to existing public APIs