package apphome

import (
	"fmt"

	"github.com/slack-go/slack"
)

const (
	// BlockIDPrefix is the prefix for input block IDs.
	BlockIDPrefix = "input_"

	// ActionIDInputSuffix is the suffix for input action IDs.
	ActionIDInputSuffix = "_value"
)

// FormBuilder constructs dynamic Modal forms based on Capability definitions.
type FormBuilder struct{}

// NewFormBuilder creates a new form builder.
func NewFormBuilder() *FormBuilder {
	return &FormBuilder{}
}

// BuildModal creates a Modal view for a capability.
func (f *FormBuilder) BuildModal(cap Capability) *slack.ModalViewRequest {
	blocks := f.BuildBlocks(cap)

	return &slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           slack.NewTextBlockObject(slack.PlainTextType, truncate(cap.Name, 24), false, false),
		Submit:          slack.NewTextBlockObject(slack.PlainTextType, "执行", false, false),
		Close:           slack.NewTextBlockObject(slack.PlainTextType, "取消", false, false),
		Blocks:          slack.Blocks{BlockSet: blocks},
		PrivateMetadata: cap.ID,
	}
}

// BuildBlocks creates input blocks for each parameter.
func (f *FormBuilder) BuildBlocks(cap Capability) []slack.Block {
	var blocks []slack.Block

	// Description header
	if cap.Description != "" {
		descText := slack.NewTextBlockObject(
			slack.PlainTextType,
			cap.Description,
			false, false,
		)
		blocks = append(blocks, slack.NewHeaderBlock(descText))
		blocks = append(blocks, slack.NewDividerBlock())
	}

	// Build input for each parameter
	for _, param := range cap.Parameters {
		block := f.buildInputBlock(param)
		if block != nil {
			blocks = append(blocks, block)
		}
	}

	return blocks
}

// buildInputBlock creates an input block based on parameter type.
func (f *FormBuilder) buildInputBlock(param Parameter) slack.Block {
	blockID := BlockIDPrefix + param.ID
	actionID := param.ID + ActionIDInputSuffix

	switch param.Type {
	case "text":
		return f.buildTextInput(param, blockID, actionID)
	case "multiline":
		return f.buildTextarea(param, blockID, actionID)
	case "select":
		return f.buildSelect(param, blockID, actionID)
	default:
		// Default to text input
		return f.buildTextInput(param, blockID, actionID)
	}
}

// buildTextInput creates a plain text input block.
func (f *FormBuilder) buildTextInput(param Parameter, blockID, actionID string) slack.Block {
	placeholder := slack.NewTextBlockObject(slack.PlainTextType, param.Placeholder, false, false)
	label := slack.NewTextBlockObject(slack.PlainTextType, buildLabel(param), false, false)

	textInput := slack.NewPlainTextInputBlockElement(
		placeholder,
		actionID,
	)

	return slack.NewInputBlock(
		blockID,
		label,
		nil,
		textInput,
	)
}

// buildTextarea creates a multiline text input block.
func (f *FormBuilder) buildTextarea(param Parameter, blockID, actionID string) slack.Block {
	placeholder := slack.NewTextBlockObject(slack.PlainTextType, param.Placeholder, false, false)
	label := slack.NewTextBlockObject(slack.PlainTextType, buildLabel(param), false, false)

	textInput := slack.NewPlainTextInputBlockElement(
		placeholder,
		actionID,
	)
	// Enable multiline
	textInput.Multiline = true

	return slack.NewInputBlock(
		blockID,
		label,
		nil,
		textInput,
	)
}

// buildSelect creates a static select input block.
func (f *FormBuilder) buildSelect(param Parameter, blockID, actionID string) slack.Block {
	label := slack.NewTextBlockObject(slack.PlainTextType, buildLabel(param), false, false)

	// Build options
	var options []*slack.OptionBlockObject
	for _, opt := range param.Options {
		text := slack.NewTextBlockObject(slack.PlainTextType, opt, false, false)
		value := opt
		options = append(options, slack.NewOptionBlockObject(value, text, nil))
	}

	// Placeholder for select
	placeholder := slack.NewTextBlockObject(slack.PlainTextType, param.Placeholder, false, false)

	selectInput := slack.NewOptionsSelectBlockElement(
		slack.OptTypeStatic,
		placeholder,
		actionID,
		options...,
	)

	return slack.NewInputBlock(
		blockID,
		label,
		nil,
		selectInput,
	)
}

// ExtractParams extracts parameter values from a Modal submission.
func (f *FormBuilder) ExtractParams(state *slack.ViewState, cap Capability) map[string]string {
	params := make(map[string]string)

	if state == nil || state.Values == nil {
		return params
	}

	for _, param := range cap.Parameters {
		blockID := BlockIDPrefix + param.ID
		actionID := param.ID + ActionIDInputSuffix

		if blockValues, ok := state.Values[blockID]; ok {
			if actionValue, ok := blockValues[actionID]; ok {
				// Extract value based on type
				switch param.Type {
				case "select":
					// SelectedOption is a value type, check if Value is not empty
					if actionValue.SelectedOption.Value != "" {
						params[param.ID] = actionValue.SelectedOption.Value
					}
				default:
					params[param.ID] = actionValue.Value
				}
			}
		}

		// Apply default if empty
		if params[param.ID] == "" && param.Default != "" {
			params[param.ID] = param.Default
		}
	}

	return params
}

// ValidateParams validates extracted parameters against capability definition.
func (f *FormBuilder) ValidateParams(cap Capability, params map[string]string) map[string]string {
	errors := make(map[string]string)

	for _, param := range cap.Parameters {
		value := params[param.ID]

		if param.Required && value == "" {
			errors[param.ID] = fmt.Sprintf("%s 是必填项", param.Label)
		}
	}

	return errors
}

// buildLabel creates a label text with required marker.
func buildLabel(param Parameter) string {
	if param.Required {
		return param.Label + " *"
	}
	return param.Label
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
