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

func TestFormBuilder_ExtractParams(t *testing.T) {
	fb := NewFormBuilder()
	cap := Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Test",
		Parameters: []Parameter{
			{ID: "text_field", Label: "Text", Type: "text"},
			{ID: "select_field", Label: "Select", Type: "select", Options: []string{"a", "b"}},
			{ID: "with_default", Label: "Default", Type: "text", Default: "default_value"},
		},
	}

	tests := []struct {
		name     string
		state    *slack.ViewState
		expected map[string]string
	}{
		{
			name:     "nil state returns empty map",
			state:    nil,
			expected: map[string]string{},
		},
		{
			name:     "nil values returns empty map",
			state:    &slack.ViewState{Values: nil},
			expected: map[string]string{},
		},
		{
			name: "extract text value",
			state: &slack.ViewState{
				Values: map[string]map[string]slack.BlockAction{
					"input_text_field": {"text_field_value": {Value: "hello"}},
				},
			},
			expected: map[string]string{
				"text_field":   "hello",
				"with_default": "default_value",
			},
		},
		{
			name: "extract select value",
			state: &slack.ViewState{
				Values: map[string]map[string]slack.BlockAction{
					"input_select_field": {"select_field_value": {SelectedOption: slack.OptionBlockObject{Value: "option_a"}}},
				},
			},
			expected: map[string]string{
				"select_field": "option_a",
				"with_default": "default_value",
			},
		},
		{
			name: "empty select falls back to default",
			state: &slack.ViewState{
				Values: map[string]map[string]slack.BlockAction{
					"input_select_field": {"select_field_value": {SelectedOption: slack.OptionBlockObject{Value: ""}}},
				},
			},
			expected: map[string]string{"with_default": "default_value"},
		},
		{
			name: "value overrides default",
			state: &slack.ViewState{
				Values: map[string]map[string]slack.BlockAction{
					"input_with_default": {"with_default_value": {Value: "user_value"}},
				},
			},
			expected: map[string]string{"with_default": "user_value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fb.ExtractParams(tt.state, cap)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormBuilder_BuildBlocks(t *testing.T) {
	fb := NewFormBuilder()

	t.Run("with description adds header and divider", func(t *testing.T) {
		cap := Capability{
			ID:             "test",
			Name:           "Test",
			Description:    "Test description",
			PromptTemplate: "Test",
			Parameters:     []Parameter{{ID: "p1", Label: "P1", Type: "text"}},
		}
		blocks := fb.BuildBlocks(cap)
		require.GreaterOrEqual(t, len(blocks), 3) // header + divider + input
	})

	t.Run("without description skips header", func(t *testing.T) {
		cap := Capability{
			ID:             "test",
			Name:           "Test",
			PromptTemplate: "Test",
			Parameters:     []Parameter{{ID: "p1", Label: "P1", Type: "text"}},
		}
		blocks := fb.BuildBlocks(cap)
		assert.Equal(t, 1, len(blocks)) // just input
	})
}

func TestFormBuilder_BuildInputBlock_Types(t *testing.T) {
	fb := NewFormBuilder()

	t.Run("multiline type creates textarea", func(t *testing.T) {
		param := Parameter{ID: "desc", Label: "Description", Type: "multiline"}
		block := fb.buildInputBlock(param)
		require.NotNil(t, block)
		inputBlock, ok := block.(*slack.InputBlock)
		require.True(t, ok)
		_, isTextarea := inputBlock.Element.(*slack.PlainTextInputBlockElement)
		assert.True(t, isTextarea)
	})

	t.Run("select type creates select element", func(t *testing.T) {
		param := Parameter{ID: "choice", Label: "Choice", Type: "select", Options: []string{"a", "b"}}
		block := fb.buildInputBlock(param)
		require.NotNil(t, block)
		inputBlock, ok := block.(*slack.InputBlock)
		require.True(t, ok)
		_, isSelect := inputBlock.Element.(*slack.SelectBlockElement)
		assert.True(t, isSelect)
	})

	t.Run("unknown type defaults to text input", func(t *testing.T) {
		param := Parameter{ID: "custom", Label: "Custom", Type: "unknown_type"}
		block := fb.buildInputBlock(param)
		require.NotNil(t, block)
		inputBlock, ok := block.(*slack.InputBlock)
		require.True(t, ok)
		_, isText := inputBlock.Element.(*slack.PlainTextInputBlockElement)
		assert.True(t, isText)
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 8, "hello..."},
		{"empty string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandler_HandleHomeOpened_NoClient(t *testing.T) {
	registry := NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(registry, WithHandlerLogger(logger))

	err := handler.HandleHomeOpened(context.Background(), &HomeOpenedEvent{User: "U123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slack client not configured")
}

func TestHandler_HandleCapabilityClick_NoClient(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "test_cap",
		Name:           "Test Capability",
		PromptTemplate: "Test",
	}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(registry, WithHandlerLogger(logger))

	err := handler.HandleCapabilityClick(context.Background(), &slack.InteractionCallback{}, "test_cap")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slack client not configured")
}

func TestHandler_HandleCapabilityClick_UnknownCapability(t *testing.T) {
	registry := NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create a client with invalid token (won't be called for error case)
	client := slack.New("xoxb-test", slack.OptionAPIURL("http://localhost:0/"))
	handler := NewHandler(registry,
		WithSlackClient(client),
		WithHandlerLogger(logger))

	err := handler.HandleCapabilityClick(context.Background(), &slack.InteractionCallback{TriggerID: "trig123"}, "unknown_cap")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "capability not found")
}

func TestHandler_HandleViewSubmission_NoClient(t *testing.T) {
	registry := NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(registry, WithHandlerLogger(logger))

	callback := &slack.InteractionCallback{
		View: slack.View{PrivateMetadata: "test_cap"},
	}
	resp, err := handler.HandleViewSubmission(context.Background(), callback)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestHandler_HandleViewSubmission_UnknownCapability(t *testing.T) {
	registry := NewRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := slack.New("xoxb-test", slack.OptionAPIURL("http://localhost:0/"))
	handler := NewHandler(registry,
		WithSlackClient(client),
		WithHandlerLogger(logger))

	callback := &slack.InteractionCallback{
		View: slack.View{PrivateMetadata: "unknown_cap"},
	}
	resp, err := handler.HandleViewSubmission(context.Background(), callback)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestHandler_HandleViewSubmission_ValidationFailure(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "test_cap",
		Name:           "Test Capability",
		PromptTemplate: "Test: {{.input}}",
		Parameters: []Parameter{
			{ID: "input", Label: "Input", Type: "text", Required: true},
		},
	}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := slack.New("xoxb-test", slack.OptionAPIURL("http://localhost:0/"))
	handler := NewHandler(registry,
		WithSlackClient(client),
		WithHandlerLogger(logger))

	callback := &slack.InteractionCallback{
		User: slack.User{ID: "U123"},
		View: slack.View{
			PrivateMetadata: "test_cap",
			State:           &slack.ViewState{Values: map[string]map[string]slack.BlockAction{}},
		},
	}
	resp, err := handler.HandleViewSubmission(context.Background(), callback)
	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Errors)
}

func TestHandler_HandleViewSubmission_SuccessNoExecutor(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "test_cap",
		Name:           "Test Capability",
		PromptTemplate: "Test: {{.input}}",
		Parameters: []Parameter{
			{ID: "input", Label: "Input", Type: "text", Required: true},
		},
	}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := slack.New("xoxb-test", slack.OptionAPIURL("http://localhost:0/"))
	handler := NewHandler(registry,
		WithSlackClient(client),
		WithHandlerLogger(logger))

	callback := &slack.InteractionCallback{
		User: slack.User{ID: "U123"},
		View: slack.View{
			PrivateMetadata: "test_cap",
			State: &slack.ViewState{
				Values: map[string]map[string]slack.BlockAction{
					"input_input": {"input_value": {Value: "test value"}},
				},
			},
		},
	}
	resp, err := handler.HandleViewSubmission(context.Background(), callback)
	assert.NoError(t, err)
	assert.Nil(t, resp)
}

func TestHandler_SetClient(t *testing.T) {
	registry := NewRegistry()
	handler := NewHandler(registry)

	assert.Nil(t, handler.client)
	handler.SetClient(&slack.Client{})
	assert.NotNil(t, handler.client)
}

func TestHandler_SetExecutor(t *testing.T) {
	registry := NewRegistry()
	handler := NewHandler(registry)

	assert.Nil(t, handler.executor)
	handler.SetExecutor(NewExecutor())
	assert.NotNil(t, handler.executor)
}

func TestBuilder_BuildHomeTab(t *testing.T) {
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
	view := builder.BuildHomeTab()

	require.NotNil(t, view)
	assert.Equal(t, slack.VTHomeTab, view.Type)
	assert.NotEmpty(t, view.Blocks.BlockSet)
}

func TestBuilder_BuildBlocks(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "code1",
		Name:           "Code Review",
		Icon:           ":mag:",
		Description:    "Review code",
		Category:       "code",
		PromptTemplate: "Review: {{.code}}",
	}))
	require.NoError(t, registry.Register(Capability{
		ID:             "debug1",
		Name:           "Debug",
		Icon:           ":bug:",
		Description:    "Debug issues",
		Category:       "debug",
		PromptTemplate: "Debug: {{.issue}}",
	}))

	builder := NewBuilder(registry)
	blocks := builder.BuildBlocks()

	assert.NotEmpty(t, blocks)
	// Should have header + category header + capability row + divider + footer
	assert.GreaterOrEqual(t, len(blocks), 5)
}

func TestBuilder_BuildCapabilityRow(t *testing.T) {
	builder := NewBuilder(NewRegistry())

	t.Run("empty caps returns nil", func(t *testing.T) {
		block := builder.buildCapabilityRow([]Capability{})
		assert.Nil(t, block)
	})

	t.Run("single capability", func(t *testing.T) {
		caps := []Capability{
			{ID: "test", Name: "Test", Icon: ":test:", Description: "Test cap"},
		}
		block := builder.buildCapabilityRow(caps)
		require.NotNil(t, block)
		section, ok := block.(*slack.SectionBlock)
		require.True(t, ok)
		assert.Len(t, section.Fields, 1)
	})

	t.Run("multiple capabilities", func(t *testing.T) {
		caps := []Capability{
			{ID: "test1", Name: "Test1", Icon: ":t1:", Description: "Test 1"},
			{ID: "test2", Name: "Test2", Icon: ":t2:", Description: "Test 2"},
			{ID: "test3", Name: "Test3", Icon: ":t3:", Description: "Test 3"},
		}
		block := builder.buildCapabilityRow(caps)
		require.NotNil(t, block)
		section, ok := block.(*slack.SectionBlock)
		require.True(t, ok)
		assert.Len(t, section.Fields, 3)
	})
}

func TestBuilder_BuildCapabilitySection(t *testing.T) {
	builder := NewBuilder(NewRegistry())
	cap := Capability{
		ID:          "test",
		Name:        "Test Capability",
		Icon:        ":test:",
		Description: "Test description",
	}

	block := builder.BuildCapabilitySection(cap)
	require.NotNil(t, block)

	section, ok := block.(*slack.SectionBlock)
	require.True(t, ok)
	assert.NotNil(t, section.Text)
	assert.NotNil(t, section.Accessory)
}

func TestRegistry_Unregister(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{
		ID:             "test",
		Name:           "Test",
		PromptTemplate: "Test",
	}))

	assert.Equal(t, 1, registry.Count())
	registry.Unregister("test")
	assert.Equal(t, 0, registry.Count())

	_, exists := registry.Get("test")
	assert.False(t, exists)
}

func TestRegistry_ConfigPath(t *testing.T) {
	t.Run("default empty path", func(t *testing.T) {
		registry := NewRegistry()
		assert.Equal(t, "", registry.ConfigPath())
	})

	t.Run("custom path", func(t *testing.T) {
		registry := NewRegistry(WithConfigPath("/custom/path.yaml"))
		assert.Equal(t, "/custom/path.yaml", registry.ConfigPath())
	})
}

func TestRegistry_GetAll(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(Capability{ID: "a", Name: "A", PromptTemplate: "A", Category: "code"}))
	require.NoError(t, registry.Register(Capability{ID: "b", Name: "B", PromptTemplate: "B", Category: "debug"}))

	all := registry.GetAll()
	assert.Len(t, all, 2)

	// Verify it's a copy (modifying shouldn't affect registry)
	all[0].Name = "Modified"
	original, _ := registry.Get("a")
	assert.Equal(t, "A", original.Name)
}

func TestRegistry_Reload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(WithLogger(logger), WithConfigPath("nonexistent.yaml"))

	err := registry.Reload()
	assert.Error(t, err) // Should fail because file doesn't exist
}

func TestExecutor_Options(t *testing.T) {
	t.Run("WithExecutorClient", func(t *testing.T) {
		client := slack.New("xoxb-test")
		executor := NewExecutor(WithExecutorClient(client))
		assert.NotNil(t, executor.client)
	})

	t.Run("WithMessageHandler", func(t *testing.T) {
		handler := func(ctx context.Context, userID, channelID, message string) error { return nil }
		executor := NewExecutor(WithMessageHandler(handler))
		assert.NotNil(t, executor.MessageHandler)
	})

	t.Run("WithExecutorLogger", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		executor := NewExecutor(WithExecutorLogger(logger))
		assert.NotNil(t, executor.logger)
	})
}

func TestExecutor_SetClient(t *testing.T) {
	executor := NewExecutor()
	assert.Nil(t, executor.client)
	executor.SetClient(&slack.Client{})
	assert.NotNil(t, executor.client)
}

func TestNewBrainIntegration(t *testing.T) {
	t.Run("nil brain", func(t *testing.T) {
		bi := NewBrainIntegration(nil)
		assert.Nil(t, bi.brain)
	})
}

func TestBrainIntegration_SetLogger(t *testing.T) {
	bi := &BrainIntegration{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bi.SetLogger(logger)
	assert.NotNil(t, bi.logger)
}
