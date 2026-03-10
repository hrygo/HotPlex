package apphome

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapability_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cap     Capability
		wantErr bool
	}{
		{
			name: "valid capability",
			cap: Capability{
				ID:             "test_cap",
				Name:           "Test Capability",
				Icon:           ":test:",
				Description:    "A test capability",
				Category:       "test",
				PromptTemplate: "Test prompt: {{.input}}",
				Enabled:        true,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			cap: Capability{
				Name:           "Test",
				PromptTemplate: "Test",
			},
			wantErr: true,
		},
		{
			name: "missing name",
			cap: Capability{
				ID:             "test",
				PromptTemplate: "Test",
			},
			wantErr: true,
		},
		{
			name: "missing prompt template",
			cap: Capability{
				ID:   "test",
				Name: "Test",
			},
			wantErr: true,
		},
		{
			name: "invalid parameter type",
			cap: Capability{
				ID:             "test",
				Name:           "Test",
				PromptTemplate: "Test",
				Parameters: []Parameter{
					{ID: "p1", Label: "P1", Type: "invalid"},
				},
			},
			wantErr: true,
		},
		{
			name: "select without options",
			cap: Capability{
				ID:             "test",
				Name:           "Test",
				PromptTemplate: "Test",
				Parameters: []Parameter{
					{ID: "p1", Label: "P1", Type: "select"},
				},
			},
			wantErr: true,
		},
		{
			name: "select with options",
			cap: Capability{
				ID:             "test",
				Name:           "Test",
				PromptTemplate: "Test: {{.p1}}",
				Parameters: []Parameter{
					{ID: "p1", Label: "P1", Type: "select", Options: []string{"a", "b"}},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cap.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegistry_LoadFromBytes(t *testing.T) {
	yamlData := `
capabilities:
  - id: test_cap
    name: Test Capability
    icon: ":test:"
    description: A test
    category: test
    enabled: true
    prompt_template: "Test: {{.input}}"
    parameters:
      - id: input
        label: Input
        type: text
        required: true
  - id: disabled_cap
    name: Disabled
    icon: ":x:"
    description: Disabled
    category: test
    enabled: false
    prompt_template: "Disabled"
`

	registry := NewRegistry()
	err := registry.LoadFromBytes([]byte(yamlData))
	require.NoError(t, err)

	// Should have 1 capability (disabled one skipped)
	assert.Equal(t, 1, registry.Count())

	cap, ok := registry.Get("test_cap")
	require.True(t, ok)
	assert.Equal(t, "Test Capability", cap.Name)
	assert.Len(t, cap.Parameters, 1)
}

func TestRegistry_GetByCategory(t *testing.T) {
	registry := NewRegistry()

	// Register test capabilities
	require.NoError(t, registry.Register(Capability{
		ID:             "code1",
		Name:           "Code 1",
		Category:       "code",
		PromptTemplate: "Test",
	}))
	require.NoError(t, registry.Register(Capability{
		ID:             "code2",
		Name:           "Code 2",
		Category:       "code",
		PromptTemplate: "Test",
	}))
	require.NoError(t, registry.Register(Capability{
		ID:             "debug1",
		Name:           "Debug 1",
		Category:       "debug",
		PromptTemplate: "Test",
	}))

	codeCaps := registry.GetByCategory("code")
	assert.Len(t, codeCaps, 2)

	debugCaps := registry.GetByCategory("debug")
	assert.Len(t, debugCaps, 1)

	gitCaps := registry.GetByCategory("git")
	assert.Len(t, gitCaps, 0)
}

func TestFormBuilder_BuildModal(t *testing.T) {
	fb := NewFormBuilder()
	cap := Capability{
		ID:             "test",
		Name:           "Test Capability",
		Description:    "Test description",
		PromptTemplate: "Test: {{.input}}",
		Parameters: []Parameter{
			{
				ID:          "input",
				Label:       "Input",
				Type:        "text",
				Required:    true,
				Placeholder: "Enter input",
			},
			{
				ID:          "select_field",
				Label:       "Select",
				Type:        "select",
				Options:     []string{"a", "b", "c"},
				Placeholder: "Choose",
			},
		},
	}

	modal := fb.BuildModal(cap)
	require.NotNil(t, modal)
	assert.Equal(t, slack.VTModal, modal.Type)
	assert.Equal(t, "test", modal.PrivateMetadata)
	assert.NotEmpty(t, modal.Blocks.BlockSet)
}

func TestFormBuilder_ValidateParams(t *testing.T) {
	fb := NewFormBuilder()
	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Test",
		Parameters: []Parameter{
			{ID: "required_field", Label: "Required", Type: "text", Required: true},
			{ID: "optional_field", Label: "Optional", Type: "text", Required: false},
		},
	}

	tests := []struct {
		name   string
		params map[string]string
		errors int
	}{
		{
			name:   "all required provided",
			params: map[string]string{"required_field": "value"},
			errors: 0,
		},
		{
			name:   "missing required",
			params: map[string]string{},
			errors: 1,
		},
		{
			name:   "all provided",
			params: map[string]string{"required_field": "value", "optional_field": "opt"},
			errors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := fb.ValidateParams(cap, tt.params)
			assert.Len(t, errors, tt.errors)
		})
	}
}

func TestBuilder_BuildFullHomeView(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "code1",
		Name:           "Code Review",
		Icon:           ":mag:",
		Description:    "Review code",
		Category:       "code",
		PromptTemplate: "Review: {{.code}}",
	}))

	builder := NewBuilder(registry)
	view := builder.BuildFullHomeView()

	require.NotNil(t, view)
	assert.Equal(t, slack.VTHomeTab, view.Type)
	assert.NotEmpty(t, view.Blocks.BlockSet)
}

func TestIsCapabilityAction(t *testing.T) {
	tests := []struct {
		name     string
		actionID string
		want     bool
	}{
		{
			name:     "capability action",
			actionID: "cap_click:test_cap",
			want:     true,
		},
		{
			name:     "non-capability action",
			actionID: "other_action",
			want:     false,
		},
		{
			name:     "empty action",
			actionID: "",
			want:     false,
		},
		{
			name:     "prefix only",
			actionID: ActionIDPrefix,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCapabilityAction(tt.actionID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractCapabilityID(t *testing.T) {
	tests := []struct {
		name     string
		actionID string
		want     string
	}{
		{
			name:     "extract capability ID",
			actionID: "cap_click:code_review",
			want:     "code_review",
		},
		{
			name:     "no prefix",
			actionID: "other",
			want:     "other",
		},
		{
			name:     "empty",
			actionID: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCapabilityID(tt.actionID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExecutor_RenderPrompt(t *testing.T) {
	executor := NewExecutor()

	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Hello {{.name}}, your score is {{.score}}",
	}

	prompt, err := executor.renderPrompt(cap, map[string]string{
		"name":  "Alice",
		"score": "100",
	})

	require.NoError(t, err)
	assert.Equal(t, "Hello Alice, your score is 100", prompt)
}

func TestExecutor_RenderPrompt_InvalidTemplate(t *testing.T) {
	executor := NewExecutor()

	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Hello {{.name}",
	}

	_, err := executor.renderPrompt(cap, map[string]string{"name": "Alice"})
	assert.Error(t, err)
}

func TestExecutor_RenderPrompt_MissingParam(t *testing.T) {
	executor := NewExecutor()

	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Hello {{.name}}",
	}

	prompt, err := executor.renderPrompt(cap, map[string]string{})
	require.NoError(t, err)
	assert.Contains(t, prompt, "Hello")
}

func TestBrainIntegration_PreparePrompt_NoBrain(t *testing.T) {
	bi := &BrainIntegration{brain: nil}

	prompt, err := bi.PreparePrompt(context.Background(), Capability{}, map[string]string{}, "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "test prompt", prompt)
}

func TestBrainIntegration_CompressContext_NoBrain(t *testing.T) {
	bi := &BrainIntegration{brain: nil}

	compressed, err := bi.compressContext(context.Background(), "long prompt")
	require.NoError(t, err)
	assert.Equal(t, "long prompt", compressed)
}

func TestBrainIntegration_EnhancePrompt_NoBrain(t *testing.T) {
	bi := &BrainIntegration{brain: nil}

	enhanced, err := bi.EnhancePrompt(context.Background(), "original", "context")
	require.NoError(t, err)
	assert.Equal(t, "original", enhanced)
}

func TestBrainIntegration_ConfirmIntent_NoBrain(t *testing.T) {
	bi := &BrainIntegration{brain: nil}

	confirmed, err := bi.confirmIntent(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.True(t, confirmed)
}

func TestLoadDefaultCapabilities(t *testing.T) {
	registry := NewRegistry()
	err := LoadDefaultCapabilities(registry)
	require.NoError(t, err)

	assert.Equal(t, 1, registry.Count())

	cap, ok := registry.Get("code_review")
	require.True(t, ok)
	assert.Equal(t, "代码审查", cap.Name)
	assert.Equal(t, "code", cap.Category)
	assert.Len(t, cap.Parameters, 1)
}

func TestSetup_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler, registry, executor := Setup(nil, nil, Config{Enabled: false}, logger)
	assert.Nil(t, handler)
	assert.Nil(t, registry)
	assert.Nil(t, executor)
}

func TestDefaultCategories(t *testing.T) {
	categories := DefaultCategories()
	assert.Len(t, categories, 6)

	// Check some expected categories
	catMap := make(map[string]CategoryInfo)
	for _, c := range categories {
		catMap[c.ID] = c
	}

	assert.Equal(t, "代码", catMap["code"].Name)
	assert.Equal(t, ":computer:", catMap["code"].Icon)
	assert.Equal(t, "调试", catMap["debug"].Name)
	assert.Equal(t, ":bug:", catMap["debug"].Icon)
}

func TestCapability_BrainOptions(t *testing.T) {
	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Test",
		BrainOpts: BrainOptions{
			IntentConfirm:   true,
			CompressContext: true,
			PreferredModel:  "claude-3",
		},
	}

	err := cap.Validate()
	assert.NoError(t, err)
	assert.True(t, cap.BrainOpts.IntentConfirm)
	assert.True(t, cap.BrainOpts.CompressContext)
	assert.Equal(t, "claude-3", cap.BrainOpts.PreferredModel)
}
