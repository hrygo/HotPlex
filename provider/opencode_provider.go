package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// OpenCodeProvider implements the Provider interface for OpenCode CLI.
// OpenCode has a different architecture from Claude Code:
// - Supports both CLI mode and HTTP API mode
// - Uses Part-based output format instead of stream-json
// - Has Plan/Build dual modes
// - Supports multiple LLM providers
type OpenCodeProvider struct {
	ProviderBase
	opts        ProviderConfig
	opencodeCfg *OpenCodeConfig
}

// OpenCode Part types (from research results)
const (
	OpenCodePartText       = "text"
	OpenCodePartReasoning  = "reasoning"
	OpenCodePartTool       = "tool"
	OpenCodePartStepStart  = "step-start"
	OpenCodePartStepFinish = "step-finish"
)

// OpenCodeMessage represents the output message structure from OpenCode.
type OpenCodeMessage struct {
	ID      string         `json:"id,omitempty"`
	Role    string         `json:"role,omitempty"`
	Parts   []OpenCodePart `json:"parts,omitempty"`
	Content string         `json:"content,omitempty"`
	Status  string         `json:"status,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// OpenCodePart represents a single part in an OpenCode message.
type OpenCodePart struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Name    string         `json:"name,omitempty"`
	ID      string         `json:"id,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
	Content string         `json:"content,omitempty"`
	Status  string         `json:"status,omitempty"`
	Error   string         `json:"error,omitempty"`

	// Step tracking
	StepNumber int `json:"step_number,omitempty"`
	TotalSteps int `json:"total_steps,omitempty"`

	// Token usage (in metadata)
	Usage *OpenCodeUsage `json:"usage,omitempty"`
}

// OpenCodeUsage represents token usage information.
type OpenCodeUsage struct {
	InputTokens  int32 `json:"input_tokens,omitempty"`
	OutputTokens int32 `json:"output_tokens,omitempty"`
}

// NewOpenCodeProvider creates a new OpenCode provider instance.
func NewOpenCodeProvider(cfg ProviderConfig, logger *slog.Logger) (*OpenCodeProvider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	meta := ProviderMeta{
		Type:        ProviderTypeOpenCode,
		DisplayName: "OpenCode",
		BinaryName:  "opencode",
		Features: ProviderFeatures{
			SupportsResume:             false, // OpenCode doesn't have explicit resume
			SupportsStreamJSON:         false, // Uses different format
			SupportsSSE:                true,  // HTTP API mode uses SSE
			SupportsHTTPAPI:            true,  // Has HTTP API mode
			SupportsSessionID:          false, // Uses different session model
			SupportsPermissions:        true,  // Plan/Build modes
			MultiTurnReady:             true,
			RequiresInitialPromptAsArg: true,
		},
	}

	// Determine binary path
	binaryPath := cfg.BinaryPath
	if binaryPath == "" {
		if path, err := exec.LookPath(meta.BinaryName); err == nil {
			binaryPath = path
		}
	}

	// Extract OpenCode-specific config
	var opencodeCfg *OpenCodeConfig
	if cfg.OpenCode != nil {
		opencodeCfg = cfg.OpenCode
	} else {
		opencodeCfg = &OpenCodeConfig{}
	}

	return &OpenCodeProvider{
		ProviderBase: ProviderBase{
			meta:       meta,
			binaryPath: binaryPath,
			logger:     logger.With("provider", "opencode"),
		},
		opts:        cfg,
		opencodeCfg: opencodeCfg,
	}, nil
}

// BuildCLIArgs constructs OpenCode CLI arguments.
func (p *OpenCodeProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
	args := []string{
		"run",
	}

	// Use --session if we have a provider-level session ID
	if providerSessionID != "" {
		// OpenCode v1.2.x requires session IDs to start with 'ses'
		fullSessionID := providerSessionID
		if !strings.HasPrefix(fullSessionID, "ses_") {
			fullSessionID = "ses_" + fullSessionID
		}
		args = append(args, "--session", fullSessionID)
	}

	// Format comes before prompt and options
	args = append(args, "--format", "json")

	// Provider and model
	provider := p.opencodeCfg.Provider
	model := opts.Model
	if model == "" {
		model = p.opencodeCfg.Model
	}
	if model == "" {
		model = p.opts.DefaultModel
	}

	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Extra arguments from config
	if len(p.opts.ExtraArgs) > 0 {
		args = append(args, p.opts.ExtraArgs...)
	}

	// Use --command for the initial prompt (CRITICAL for OpenCode session startup #17)
	if opts.InitialPrompt != "" {
		finalPrompt := opts.InitialPrompt
		if opts.TaskInstructions != "" {
			finalPrompt = fmt.Sprintf("<context>\n<![CDATA[\n%s\n]]>\n</context>\n\n<user_query>\n<![CDATA[\n%s\n]]>\n</user_query>",
				opts.TaskInstructions, opts.InitialPrompt)
		}
		args = append(args, "--command", finalPrompt)
	}

	return args
}

// BuildEnvVars constructs environment variables for OpenCode.
func (p *OpenCodeProvider) BuildEnvVars(opts *ProviderSessionOptions) []string {
	env := []string{}

	// Add extra environment variables from config
	for k, v := range p.opts.ExtraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// OpenCode-specific environment variables
	// OPENCODE_PROVIDER, OPENCODE_MODEL can be used as alternatives

	return env
}

// BuildInputMessage constructs the input for OpenCode.
// Note: OpenCode typically takes the prompt as a CLI argument, not stdin.
// For multi-turn sessions, this method may be used differently.
func (p *OpenCodeProvider) BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error) {
	// Inject task-level constraints using XML tags and CDATA.
	finalPrompt := prompt
	if taskInstructions != "" {
		finalPrompt = fmt.Sprintf("<context>\n<![CDATA[\n%s\n]]>\n</context>\n\n<user_query>\n<![CDATA[\n%s\n]]>\n</user_query>",
			taskInstructions, prompt)
	}

	// OpenCode CLI takes prompt as argument, but we structure this
	// for potential future stdin support or HTTP API mode
	return map[string]any{
		"prompt": finalPrompt,
	}, nil
}

// ParseEvent parses an OpenCode JSON output line into a ProviderEvent.
func (p *OpenCodeProvider) ParseEvent(line string) (*ProviderEvent, error) {
	var msg OpenCodeMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not valid JSON, return as raw content
		return &ProviderEvent{
			Type:    EventTypeRaw,
			RawType: "raw",
			Content: line,
			RawLine: line,
		}, nil
	}

	// If message has a global error
	if msg.Error != "" {
		return &ProviderEvent{
			Type:    EventTypeError,
			Content: msg.Error,
			RawLine: line,
		}, nil
	}

	// Process parts - OpenCode outputs multiple parts per message
	// We return the first significant part as the event
	for _, part := range msg.Parts {
		event := p.parsePart(part, line)
		if event != nil {
			return event, nil
		}
	}

	// Fallback: use content field
	if msg.Content != "" {
		return &ProviderEvent{
			Type:    EventTypeAnswer,
			RawType: "content",
			Content: msg.Content,
			Status:  msg.Status,
			RawLine: line,
		}, nil
	}

	// Empty or system message
	return &ProviderEvent{
		Type:    EventTypeSystem,
		RawType: "empty",
		RawLine: line,
	}, nil
}

// parsePart converts an OpenCode Part to a ProviderEvent.
func (p *OpenCodeProvider) parsePart(part OpenCodePart, rawLine string) *ProviderEvent {
	switch part.Type {
	case OpenCodePartText:
		return &ProviderEvent{
			Type:    EventTypeAnswer,
			RawType: part.Type,
			Content: part.Text,
			Status:  part.Status,
			RawLine: rawLine,
		}

	case OpenCodePartReasoning:
		return &ProviderEvent{
			Type:    EventTypeThinking,
			RawType: part.Type,
			Content: part.Text,
			Status:  "running",
			RawLine: rawLine,
		}

	case OpenCodePartTool:
		// Tool can be tool_use or tool_result depending on context
		if part.Output != "" || part.Content != "" {
			// This is a tool result
			status := "success"
			if part.Error != "" || part.Status == "error" {
				status = "error"
			}
			return &ProviderEvent{
				Type:     EventTypeToolResult,
				RawType:  part.Type,
				ToolName: part.Name,
				ToolID:   part.ID,
				Content:  part.Output + part.Content,
				Status:   status,
				Error:    part.Error,
				IsError:  part.Error != "",
				RawLine:  rawLine,
			}
		}
		// This is a tool use
		return &ProviderEvent{
			Type:      EventTypeToolUse,
			RawType:   part.Type,
			ToolName:  part.Name,
			ToolID:    part.ID,
			ToolInput: part.Input,
			Status:    "running",
			RawLine:   rawLine,
		}

	case OpenCodePartStepStart:
		return &ProviderEvent{
			Type:    EventTypeStepStart,
			RawType: part.Type,
			Status:  "running",
			Metadata: &ProviderEventMeta{
				CurrentStep: int32(part.StepNumber),
				TotalSteps:  int32(part.TotalSteps),
			},
			RawLine: rawLine,
		}

	case OpenCodePartStepFinish:
		return &ProviderEvent{
			Type:    EventTypeStepFinish,
			RawType: part.Type,
			Status:  "success",
			Metadata: &ProviderEventMeta{
				CurrentStep: int32(part.StepNumber),
				TotalSteps:  int32(part.TotalSteps),
			},
			RawLine: rawLine,
		}

	default:
		// Unknown part type, try to extract text
		text := part.Text
		if text == "" {
			text = part.Content
		}
		if text != "" {
			return &ProviderEvent{
				Type:    EventTypeAnswer,
				RawType: part.Type,
				Content: text,
				RawLine: rawLine,
			}
		}
		return nil
	}
}

// DetectTurnEnd checks if the event signals turn completion.
func (p *OpenCodeProvider) DetectTurnEnd(event *ProviderEvent) bool {
	if event == nil {
		return false
	}

	if event.Type == EventTypeError || event.Type == EventTypeResult {
		return true
	}

	// OpenCode signals completion via step-finish (last step) or explicit completion.
	if event.Type == EventTypeStepFinish && event.Metadata != nil {
		// If current step matches total steps, it's the end
		if event.Metadata.CurrentStep > 0 && event.Metadata.TotalSteps > 0 &&
			event.Metadata.CurrentStep >= event.Metadata.TotalSteps {
			return true
		}
	}

	// Some events explicitly mark the finish
	if event.RawType == "result" || event.RawType == "complete" || event.RawType == "finish" {
		return true
	}

	return false
}

// ExtractSessionID extracts session ID from OpenCode events.
func (p *OpenCodeProvider) ExtractSessionID(event *ProviderEvent) string {
	// If the event has a session ID
	if event.SessionID != "" {
		return event.SessionID
	}
	return ""
}

// ValidateBinary checks if the OpenCode CLI is available.
func (p *OpenCodeProvider) ValidateBinary() (string, error) {
	if p.binaryPath != "" {
		return p.binaryPath, nil
	}
	path, err := exec.LookPath("opencode")
	if err != nil {
		return "", fmt.Errorf("opencode CLI not found: %w", err)
	}
	return path, nil
}

// SupportsHTTPAPI returns true if HTTP API mode should be used.
func (p *OpenCodeProvider) SupportsHTTPAPI() bool {
	return p.opencodeCfg != nil && p.opencodeCfg.UseHTTPAPI
}

// GetHTTPAPIPort returns the configured HTTP API port.
func (p *OpenCodeProvider) GetHTTPAPIPort() int {
	if p.opencodeCfg != nil && p.opencodeCfg.Port > 0 {
		return p.opencodeCfg.Port
	}
	return 4096 // Default OpenCode port
}

// BuildHTTPCommand constructs the prompt as a command string for CLI mode.
// Unlike Claude Code, OpenCode takes the prompt as a CLI argument.
func (p *OpenCodeProvider) BuildHTTPCommand(prompt string, taskInstructions string) string {
	finalPrompt := prompt
	if taskInstructions != "" {
		finalPrompt = fmt.Sprintf("<context>\n<![CDATA[\n%s\n]]>\n</context>\n\n<user_query>\n<![CDATA[\n%s\n]]>\n</user_query>",
			taskInstructions, prompt)
	}
	// Escape quotes for shell
	return strings.ReplaceAll(finalPrompt, "\"", "\\\"")
}
