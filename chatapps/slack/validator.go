// Package slack provides the Slack adapter implementation for the hotplex engine.
// Validation rules for Slack Block Kit to ensure payloads meet API constraints.
package slack

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Slack Block Kit character limits (from official documentation)
const (
	MaxSectionTextLen    = 3000
	MaxBlocksLen         = 50
	MaxModalBlocksLen    = 100
	MaxFieldTextLen      = 2000
	MaxBlockIDLen        = 255
	MaxMarkdownBlockLen  = 12000
	MaxPlainTextLen      = 150
	MaxButtonActionIDLen = 255
	MaxButtonTextLen     = 75
)

// ValidationError represents a block validation error
type ValidationError struct {
	BlockType string
	Field     string
	Message   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error in %s block: %s - %s", e.BlockType, e.Field, e.Message)
}

// ValidateBlocks validates an array of blocks
func ValidateBlocks(blocks []map[string]any, isModal bool) error {
	maxBlocks := MaxBlocksLen
	if isModal {
		maxBlocks = MaxModalBlocksLen
	}

	if len(blocks) > maxBlocks {
		return &ValidationError{
			BlockType: "message",
			Field:     "blocks",
			Message:   fmt.Sprintf("exceeds maximum %d blocks (got %d)", maxBlocks, len(blocks)),
		}
	}

	for i, block := range blocks {
		if err := ValidateBlock(block, i); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBlock validates a single block
func ValidateBlock(block map[string]any, index int) error {
	blockType, ok := block["type"].(string)
	if !ok {
		return &ValidationError{
			BlockType: fmt.Sprintf("block[%d]", index),
			Field:     "type",
			Message:   "missing or invalid type field",
		}
	}

	if id, ok := block["block_id"].(string); ok && len(id) > MaxBlockIDLen {
		return &ValidationError{
			BlockType: blockType,
			Field:     "block_id",
			Message:   fmt.Sprintf("exceeds %d characters (got %d)", MaxBlockIDLen, len(id)),
		}
	}

	switch blockType {
	case "section":
		return validateSectionBlock(block)
	case "header":
		return validateHeaderBlock(block)
	case "context":
		return validateContextBlock(block)
	case "actions":
		return validateActionsBlock(block)
	case "divider":
		return nil
	case "image":
		return validateImageBlock(block)
	case "file":
		return validateFileBlock(block)
	case "input":
		return validateInputBlock(block)
	case "markdown":
		return nil // Markdown block only requires markdown field (max 12000 chars)
	case "plan":
		return ValidatePlanBlock(block)
	case "table":
		return ValidateTableBlock(block)
	case "task_card":
		return ValidateTaskCardBlock(block)
	case "context_actions":
		return nil // Context actions block validates arrays (context ≤ 10, actions ≤ 25)
	default:
		return &ValidationError{
			BlockType: blockType,
			Field:     "type",
			Message:   fmt.Sprintf("unknown block type: %s", blockType),
		}
	}
}

func validateSectionBlock(block map[string]any) error {
	_, hasText := block["text"]
	_, hasFields := block["fields"]

	if !hasText && !hasFields {
		return &ValidationError{
			BlockType: "section",
			Field:     "text/fields",
			Message:   "section block must have either text or fields",
		}
	}

	if text, ok := block["text"].(map[string]any); ok {
		if textStr, ok := text["text"].(string); ok && len(textStr) > MaxSectionTextLen {
			return &ValidationError{
				BlockType: "section",
				Field:     "text",
				Message:   fmt.Sprintf("exceeds %d characters (got %d)", MaxSectionTextLen, len(textStr)),
			}
		}
	}

	if fields, ok := block["fields"].([]any); ok {
		if len(fields) > 10 {
			return &ValidationError{
				BlockType: "section",
				Field:     "fields",
				Message:   "cannot have more than 10 fields",
			}
		}
		for i, f := range fields {
			if field, ok := f.(map[string]any); ok {
				if textStr, ok := field["text"].(string); ok && len(textStr) > MaxFieldTextLen {
					return &ValidationError{
						BlockType: "section",
						Field:     fmt.Sprintf("fields[%d]", i),
						Message:   fmt.Sprintf("exceeds %d characters (got %d)", MaxFieldTextLen, len(textStr)),
					}
				}
			}
		}
	}

	if accessory, ok := block["accessory"].(map[string]any); ok {
		if err := validateInteractionElement(accessory); err != nil {
			return err
		}
	}

	return nil
}

func validateHeaderBlock(block map[string]any) error {
	text, ok := block["text"].(map[string]any)
	if !ok {
		return &ValidationError{
			BlockType: "header",
			Field:     "text",
			Message:   "missing or invalid text field",
		}
	}

	textStr, ok := text["text"].(string)
	if !ok {
		return &ValidationError{
			BlockType: "header",
			Field:     "text.text",
			Message:   "missing or invalid text.text field",
		}
	}

	if utf8.RuneCountInString(textStr) > MaxPlainTextLen {
		return &ValidationError{
			BlockType: "header",
			Field:     "text",
			Message:   fmt.Sprintf("exceeds %d characters (got %d)", MaxPlainTextLen, utf8.RuneCountInString(textStr)),
		}
	}

	return nil
}

func validateContextBlock(block map[string]any) error {
	elements, ok := block["elements"].([]any)
	if !ok {
		return &ValidationError{
			BlockType: "context",
			Field:     "elements",
			Message:   "missing or invalid elements field",
		}
	}

	if len(elements) > 10 {
		return &ValidationError{
			BlockType: "context",
			Field:     "elements",
			Message:   "cannot have more than 10 elements",
		}
	}

	return nil
}

func validateActionsBlock(block map[string]any) error {
	elements, ok := block["elements"].([]any)
	if !ok {
		return &ValidationError{
			BlockType: "actions",
			Field:     "elements",
			Message:   "missing or invalid elements field",
		}
	}

	if len(elements) > 25 {
		return &ValidationError{
			BlockType: "actions",
			Field:     "elements",
			Message:   "cannot have more than 25 elements",
		}
	}

	for i, elem := range elements {
		if element, ok := elem.(map[string]any); ok {
			if err := validateInteractionElement(element); err != nil {
				return &ValidationError{
					BlockType: "actions",
					Field:     fmt.Sprintf("elements[%d]", i),
					Message:   err.Error(),
				}
			}
		}
	}

	return nil
}

func validateImageBlock(block map[string]any) error {
	if _, ok := block["image_url"].(string); !ok {
		return &ValidationError{
			BlockType: "image",
			Field:     "image_url",
			Message:   "missing or invalid image_url field",
		}
	}

	if _, ok := block["alt_text"].(string); !ok {
		return &ValidationError{
			BlockType: "image",
			Field:     "alt_text",
			Message:   "missing or invalid alt_text field",
		}
	}

	return nil
}

func validateFileBlock(block map[string]any) error {
	if _, ok := block["external_id"].(string); !ok {
		return &ValidationError{
			BlockType: "file",
			Field:     "external_id",
			Message:   "missing or invalid external_id field",
		}
	}

	return nil
}

func validateInputBlock(block map[string]any) error {
	if _, ok := block["label"].(map[string]any); !ok {
		return &ValidationError{
			BlockType: "input",
			Field:     "label",
			Message:   "missing or invalid label field",
		}
	}

	if _, ok := block["element"].(map[string]any); !ok {
		return &ValidationError{
			BlockType: "input",
			Field:     "element",
			Message:   "missing or invalid element field",
		}
	}

	return nil
}

func validateInteractionElement(element map[string]any) error {
	elemType, ok := element["type"].(string)
	if !ok {
		return errors.New("missing or invalid type field")
	}

	if actionID, ok := element["action_id"].(string); ok && len(actionID) > MaxButtonActionIDLen {
		return fmt.Errorf("action_id exceeds %d characters", MaxButtonActionIDLen)
	}

	switch elemType {
	case "button":
		return validateButton(element)
	case "static_select", "external_select", "users_select", "conversations_select", "channels_select", "multi_static_select", "multi_external_select", "multi_users_select", "multi_conversations_select", "multi_channels_select":
		return validateSelect(element)
	case "overflow":
		return validateSelect(element)
	case "datepicker", "datetimepicker", "timepicker":
		return nil
	case "plain_text_input", "number_input", "email_input", "url_input":
		return nil
	case "radio_buttons", "checkboxes":
		return validateMultiOption(element)
	case "file_input":
		return ValidateFileInput(element)
	case "rich_text_input":
		return ValidateRichTextInput(element)
	case "workflow_button":
		return validateButton(element) // Same validation as button
	case "icon_button", "feedback_buttons", "url_source":
		return nil // Basic validation passed (action_id already checked)
	default:
		return fmt.Errorf("unknown element type: %s", elemType)
	}
}

func validateButton(element map[string]any) error {
	text, ok := element["text"].(map[string]any)
	if !ok {
		return errors.New("missing or invalid text field")
	}

	if textStr, ok := text["text"].(string); ok && utf8.RuneCountInString(textStr) > MaxButtonTextLen {
		return fmt.Errorf("button text exceeds %d characters", MaxButtonTextLen)
	}

	return nil
}

func validateSelect(element map[string]any) error {
	if placeholder, ok := element["placeholder"].(map[string]any); ok {
		if textStr, ok := placeholder["text"].(string); ok && utf8.RuneCountInString(textStr) > MaxPlainTextLen {
			return fmt.Errorf("placeholder text exceeds %d characters", MaxPlainTextLen)
		}
	}
	return nil
}

func validateMultiOption(element map[string]any) error {
	if options, ok := element["options"].([]any); ok {
		if len(options) > 10 {
			return errors.New("cannot have more than 10 options")
		}
		for i, opt := range options {
			if option, ok := opt.(map[string]any); ok {
				if optText, ok := option["text"].(map[string]any); ok {
					if textStr, ok := optText["text"].(string); ok && utf8.RuneCountInString(textStr) > MaxPlainTextLen {
						return fmt.Errorf("option[%d] text exceeds %d characters", i, MaxPlainTextLen)
					}
				}
			}
		}
	}
	return nil
}

// ValidateTextObject validates a text object
func ValidateTextObject(text map[string]any) error {
	textType, ok := text["type"].(string)
	if !ok {
		return errors.New("missing or invalid type field")
	}

	if textType != "plain_text" && textType != "mrkdwn" {
		return fmt.Errorf("invalid text type: %s (must be plain_text or mrkdwn)", textType)
	}

	textStr, ok := text["text"].(string)
	if !ok {
		return errors.New("missing or invalid text field")
	}

	maxLen := MaxPlainTextLen
	if textType == "mrkdwn" {
		maxLen = MaxSectionTextLen
	}

	if utf8.RuneCountInString(textStr) > maxLen {
		return fmt.Errorf("text exceeds %d characters", maxLen)
	}

	return nil
}

// TruncateMrkdwn truncates mrkdwn text to max length while preserving formatting
// It avoids cutting in the middle of code blocks or special syntax
func TruncateMrkdwn(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}

	truncated := text[:maxLen]

	// Check if we're inside a code block
	codeBlockCount := 0
	for i := 0; i < len(truncated); {
		if i+2 < len(truncated) && truncated[i:i+3] == "```" {
			codeBlockCount++
			i += 3
			continue
		}
		i++
	}

	// If we're inside a code block (odd count), find the start and close it
	if codeBlockCount%2 != 0 {
		lastCodeStart := -1
		for i := len(truncated) - 1; i >= 0; i-- {
			if i+2 < len(truncated) && truncated[i:i+3] == "```" {
				lastCodeStart = i
				break
			}
		}
		if lastCodeStart > 0 {
			truncated = truncated[:lastCodeStart]
		}
	}

	// Check if we're inside inline code
	backtickCount := 0
	for i := 0; i < len(truncated); i++ {
		if truncated[i] == '`' && (i == 0 || truncated[i-1] != '\\') {
			backtickCount++
		}
	}

	// If inside inline code, find the start
	if backtickCount%2 != 0 {
		lastBacktick := -1
		for i := len(truncated) - 1; i >= 0; i-- {
			if truncated[i] == '`' && (i == 0 || truncated[i-1] != '\\') {
				lastBacktick = i
				break
			}
		}
		if lastBacktick > 0 {
			truncated = truncated[:lastBacktick]
		}
	}

	return truncated + "..."
}

// =============================================================================
// Additional Validation Functions
// =============================================================================

// ValidateButtonURLLength validates button URL length (max 3000 chars)
func ValidateButtonURLLength(buttonURL string) error {
	if utf8.RuneCountInString(buttonURL) > 3000 {
		return fmt.Errorf("button URL too long: %d chars (max 3000)", utf8.RuneCountInString(buttonURL))
	}
	return nil
}

// ValidateAccessibilityLabel validates accessibility label for button (max 75 chars)
func ValidateAccessibilityLabel(label string) error {
	if utf8.RuneCountInString(label) > 75 {
		return fmt.Errorf("accessibility_label too long: %d chars (max 75)", utf8.RuneCountInString(label))
	}
	return nil
}

// AllowedFileTypes defines Slack-supported file extensions for file_input
var AllowedFileTypes = map[string]bool{
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true,
	"ppt": true, "pptx": true, "txt": true, "rtfd": true, "zip": true,
	"mp3": true, "mov": true, "mp4": true, "wav": true, "key": true,
}

// ValidateFileInput validates file input element
func ValidateFileInput(element map[string]any) error {
	// Validate type field
	elemType, ok := element["type"].(string)
	if !ok || elemType != "file_input" {
		return fmt.Errorf("invalid file_input: missing or incorrect type field")
	}

	// Validate max_files
	maxFiles, ok := element["max_files"].(int)
	if ok {
		if maxFiles <= 0 {
			return fmt.Errorf("file_input max_files must be positive")
		}
		if maxFiles > 10 {
			return fmt.Errorf("file_input max_files cannot exceed 10")
		}
	}

	// Validate filetypes
	filetypes, ok := element["filetypes"].([]string)
	if ok {
		if len(filetypes) > 10 {
			return fmt.Errorf("file_input cannot have more than 10 filetypes")
		}

		// Validate each filetype against allowed list
		for i, ft := range filetypes {
			ft = strings.ToLower(strings.TrimPrefix(ft, "."))
			if !AllowedFileTypes[ft] {
				return fmt.Errorf("file_input filetype[%d] '%s' not in allowed list", i, ft)
			}
		}
	}

	return nil
}

// ValidateRichTextInput validates rich text input element
func ValidateRichTextInput(element map[string]any) error {
	// Rich text input has a max length of 3000 characters
	initialValue, ok := element["initial_value"].(string)
	if ok && utf8.RuneCountInString(initialValue) > 3000 {
		return fmt.Errorf("rich_text_input initial_value too long: %d chars (max 3000)", utf8.RuneCountInString(initialValue))
	}
	return nil
}

// ValidateTableBlock validates table block
func ValidateTableBlock(block map[string]any) error {
	rows, ok := block["rows"].([]map[string]any)
	if !ok {
		return fmt.Errorf("table block must have rows")
	}

	if len(rows) == 0 {
		return fmt.Errorf("table block must have at least 1 row")
	}

	if len(rows) > 1000 {
		return fmt.Errorf("table block cannot have more than 1000 rows")
	}

	columns, ok := block["columns"].(int)
	if ok && columns > 12 {
		return fmt.Errorf("table block cannot have more than 12 columns")
	}

	return nil
}

// ValidatePlanBlock validates plan block
func ValidatePlanBlock(block map[string]any) error {
	sections, ok := block["sections"].([]map[string]any)
	if !ok {
		return fmt.Errorf("plan block must have sections")
	}

	if len(sections) > 25 {
		return fmt.Errorf("plan block cannot have more than 25 sections")
	}

	return nil
}

// ValidateTaskCardBlock validates task card block
func ValidateTaskCardBlock(block map[string]any) error {
	// Validate status - must be present
	status, ok := block["status"].(string)
	if !ok || status == "" {
		return fmt.Errorf("task_card status is required")
	}

	validStatuses := map[string]bool{
		"pending":     true,
		"in_progress": true,
		"completed":   true,
	}
	if !validStatuses[status] {
		return fmt.Errorf("task_card status must be pending, in_progress, or completed")
	}

	return nil
}

// ValidateComplete validates all fields in a block for comprehensive validation
func ValidateComplete(block map[string]any) error {
	blockType, ok := block["type"].(string)
	if !ok {
		return fmt.Errorf("block must have type")
	}

	// Type-specific complete validation
	switch blockType {
	case "button":
		if err := ValidateButtonComplete(block); err != nil {
			return err
		}
	case "image":
		if err := ValidateImageComplete(block); err != nil {
			return err
		}
	case "file":
		if err := ValidateFileComplete(block); err != nil {
			return err
		}
	}

	return nil
}

// ValidateButtonComplete performs complete validation for button element
func ValidateButtonComplete(button map[string]any) error {
	// Validate text
	text, ok := button["text"].(map[string]any)
	if !ok {
		return fmt.Errorf("button must have text")
	}
	if err := ValidateTextObject(text); err != nil {
		return err
	}

	// Validate action_id
	actionID, ok := button["action_id"].(string)
	if !ok {
		return fmt.Errorf("button must have action_id")
	}
	if err := ValidateActionID(actionID); err != nil {
		return err
	}

	// Validate URL if present
	if url, ok := button["url"].(string); ok {
		if err := ValidateButtonURL(url); err != nil {
			return err
		}
	}

	// Validate accessibility_label if present
	if label, ok := button["accessibility_label"].(string); ok {
		if err := ValidateAccessibilityLabel(label); err != nil {
			return err
		}
	}

	return nil
}

// ValidateImageComplete performs complete validation for image block
func ValidateImageComplete(image map[string]any) error {
	// Validate image_url
	imageURL, ok := image["image_url"].(string)
	if !ok {
		return fmt.Errorf("image block must have image_url")
	}
	if err := ValidateImageURL(imageURL); err != nil {
		return err
	}

	// Validate alt_text
	altText, ok := image["alt_text"].(string)
	if !ok {
		return fmt.Errorf("image block must have alt_text")
	}
	if utf8.RuneCountInString(altText) > 2000 {
		return fmt.Errorf("alt_text too long: %d chars (max 2000)", utf8.RuneCountInString(altText))
	}

	return nil
}

// ValidateFileComplete performs complete validation for file block
func ValidateFileComplete(file map[string]any) error {
	// Validate external_id
	externalID, ok := file["external_id"].(string)
	if !ok {
		return fmt.Errorf("file block must have external_id")
	}
	if utf8.RuneCountInString(externalID) > 255 {
		return fmt.Errorf("external_id too long: %d chars (max 255)", utf8.RuneCountInString(externalID))
	}

	return nil
}

// ValidateBlockWithDetails returns detailed validation errors
func ValidateBlockWithDetails(block map[string]any, index int) []error {
	var errors []error

	// Basic validation
	if err := ValidateBlock(block, index); err != nil {
		errors = append(errors, err)
	}

	// Complete validation
	if err := ValidateComplete(block); err != nil {
		errors = append(errors, err)
	}

	return errors
}
