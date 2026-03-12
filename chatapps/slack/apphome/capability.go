// Package apphome provides Slack App Home capability center functionality.
// It enables users to trigger predefined, parameterizable task templates
// through the App Home tab interface.
package apphome

import (
	"fmt"
)

// Capability represents a predefined, parameterizable task template.
type Capability struct {
	// ID is the unique identifier for the capability.
	ID string `yaml:"id" json:"id"`

	// Name is the display name shown in the UI.
	Name string `yaml:"name" json:"name"`

	// Icon is the emoji icon for the capability (e.g., ":mag:").
	Icon string `yaml:"icon" json:"icon"`

	// Description is a brief description of what the capability does.
	Description string `yaml:"description" json:"description"`

	// Category is the grouping category (e.g., "code", "debug", "git").
	Category string `yaml:"category" json:"category"`

	// Parameters are the input parameters for the capability.
	Parameters []Parameter `yaml:"parameters" json:"parameters"`

	// PromptTemplate is the Go template for rendering the final prompt.
	// Uses standard text/template syntax with {{.param_id}} placeholders.
	PromptTemplate string `yaml:"prompt_template" json:"prompt_template"`

	// BrainOpts configures Native Brain integration options.
	BrainOpts BrainOptions `yaml:"brain_opts" json:"brain_opts"`

	// Enabled indicates whether the capability is active.
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// Parameter represents an input parameter for a capability.
type Parameter struct {
	// ID is the unique identifier for the parameter.
	ID string `yaml:"id" json:"id"`

	// Label is the display label shown in the UI.
	Label string `yaml:"label" json:"label"`

	// Type is the input type: "text", "select", or "multiline".
	Type string `yaml:"type" json:"type"`

	// Required indicates whether the parameter must be filled.
	Required bool `yaml:"required" json:"required"`

	// Default is the default value for the parameter.
	Default string `yaml:"default" json:"default"`

	// Options are the available options for "select" type parameters.
	Options []string `yaml:"options" json:"options"`

	// Placeholder is the placeholder text shown in the input field.
	Placeholder string `yaml:"placeholder" json:"placeholder"`
}

// BrainOptions configures Native Brain integration for a capability.
type BrainOptions struct {
	// IntentConfirm enables intent confirmation before execution.
	IntentConfirm bool `yaml:"intent_confirm" json:"intent_confirm"`

	// CompressContext enables context compression to save tokens.
	CompressContext bool `yaml:"compress_context" json:"compress_context"`

	// PreferredModel is the preferred model for this capability.
	PreferredModel string `yaml:"preferred_model" json:"preferred_model"`
}

// Validate validates the capability configuration.
func (c *Capability) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("capability ID is required")
	}
	if c.Name == "" {
		return fmt.Errorf("capability name is required")
	}
	if c.PromptTemplate == "" {
		return fmt.Errorf("prompt_template is required for capability %s", c.ID)
	}

	// Validate parameters
	for i, p := range c.Parameters {
		if p.ID == "" {
			return fmt.Errorf("parameter %d: ID is required", i)
		}
		if p.Label == "" {
			return fmt.Errorf("parameter %s: label is required", p.ID)
		}
		if p.Type != "text" && p.Type != "select" && p.Type != "multiline" {
			return fmt.Errorf("parameter %s: invalid type %q, must be text, select, or multiline", p.ID, p.Type)
		}
		if p.Type == "select" && len(p.Options) == 0 {
			return fmt.Errorf("parameter %s: select type requires options", p.ID)
		}
	}

	return nil
}

// CategoryInfo contains display information for a capability category.
type CategoryInfo struct {
	ID          string
	Name        string
	Icon        string
	Description string
}

// DefaultCategories returns the default capability categories.
func DefaultCategories() []CategoryInfo {
	return []CategoryInfo{
		{ID: "code", Name: "代码", Icon: ":computer:", Description: "代码相关能力"},
		{ID: "debug", Name: "调试", Icon: ":bug:", Description: "调试和错误诊断"},
		{ID: "git", Name: "Git", Icon: ":twisted_rightwards_arrows:", Description: "Git 和版本控制"},
		{ID: "docs", Name: "文档", Icon: ":book:", Description: "文档生成"},
		{ID: "db", Name: "数据库", Icon: ":card_file_box:", Description: "数据库相关"},
		{ID: "design", Name: "设计", Icon: ":art:", Description: "API 和系统设计"},
	}
}
