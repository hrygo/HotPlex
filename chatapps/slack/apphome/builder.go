package apphome

import (
	"fmt"

	"github.com/slack-go/slack"
)

const (
	// ActionIDPrefix is the prefix for capability button action IDs.
	ActionIDPrefix = "cap_click:"

	// HomeTitle is the title displayed in the App Home.
	HomeTitle = "🔥 HotPlex 能力中心"

	// MaxCapabilitiesPerRow is the maximum number of capability cards per row.
	MaxCapabilitiesPerRow = 3
)

// Builder constructs Slack App Home Tab views.
type Builder struct {
	registry *Registry
}

// NewBuilder creates a new App Home builder.
func NewBuilder(registry *Registry) *Builder {
	return &Builder{
		registry: registry,
	}
}

// BuildHomeTab constructs the complete Home Tab view.
func (b *Builder) BuildHomeTab() *slack.HomeTabViewRequest {
	blocks := b.BuildBlocks()
	return &slack.HomeTabViewRequest{
		Type:   slack.VTHomeTab,
		Blocks: slack.Blocks{BlockSet: blocks},
	}
}

// BuildBlocks constructs the block set for the Home Tab.
func (b *Builder) BuildBlocks() []slack.Block {
	var blocks []slack.Block

	// Header
	blocks = append(blocks, b.buildHeader())

	// Group capabilities by category
	categories := b.registry.GetCategories()
	capabilities := b.registry.GetAll()

	// Build capability map by category
	capByCategory := make(map[string][]Capability)
	for _, cap := range capabilities {
		capByCategory[cap.Category] = append(capByCategory[cap.Category], cap)
	}

	// Build each category section
	for _, cat := range categories {
		caps, ok := capByCategory[cat.ID]
		if !ok || len(caps) == 0 {
			continue
		}

		// Category header
		blocks = append(blocks, b.buildCategoryHeader(cat))

		// Capability cards (grouped in rows)
		for i := 0; i < len(caps); i += MaxCapabilitiesPerRow {
			end := i + MaxCapabilitiesPerRow
			if end > len(caps) {
				end = len(caps)
			}
			row := caps[i:end]
			blocks = append(blocks, b.buildCapabilityRow(row))
		}

		// Spacer between categories
		blocks = append(blocks, slack.NewDividerBlock())
	}

	// Footer with help text
	blocks = append(blocks, b.buildFooter())

	return blocks
}

// buildHeader creates the main header block.
func (b *Builder) buildHeader() slack.Block {
	headerText := slack.NewTextBlockObject(slack.PlainTextType, HomeTitle, false, false)
	return slack.NewHeaderBlock(headerText)
}

// buildCategoryHeader creates a category section header.
func (b *Builder) buildCategoryHeader(cat CategoryInfo) slack.Block {
	text := fmt.Sprintf("%s *%s*", cat.Icon, cat.Name)
	headerText := slack.NewTextBlockObject(slack.MarkdownType, text, false, false)
	return slack.NewSectionBlock(headerText, nil, nil)
}

// buildCapabilityRow creates a row of capability cards.
// Note: This returns a section with fields. Individual capabilities should use BuildCapabilitySection.
func (b *Builder) buildCapabilityRow(caps []Capability) slack.Block {
	if len(caps) == 0 {
		return nil
	}

	// For Slack, we use section blocks with fields for multi-column layout
	var fields []*slack.TextBlockObject
	for _, cap := range caps {
		text := fmt.Sprintf("%s *%s*\n_%s_", cap.Icon, cap.Name, cap.Description)
		fields = append(fields, slack.NewTextBlockObject(slack.MarkdownType, text, false, false))
	}

	// Use fields for multi-column layout
	return slack.NewSectionBlock(nil, fields, nil)
}

// BuildCapabilitySection creates a section block for a single capability with a button.
func (b *Builder) BuildCapabilitySection(cap Capability) slack.Block {
	// Main text
	text := fmt.Sprintf("%s *%s*\n_%s_", cap.Icon, cap.Name, cap.Description)
	mainText := slack.NewTextBlockObject(slack.MarkdownType, text, false, false)

	// Execute button
	btn := slack.NewButtonBlockElement(
		ActionIDPrefix+cap.ID,
		cap.ID,
		slack.NewTextBlockObject(slack.PlainTextType, "执行", false, false),
	)

	return slack.NewSectionBlock(mainText, nil, slack.NewAccessory(btn))
}

// buildFooter creates a footer with help text.
func (b *Builder) buildFooter() slack.Block {
	helpText := slack.NewTextBlockObject(
		slack.MarkdownType,
		"_点击能力卡片上的「执行」按钮开始使用。_\n_能力中心由 Native Brain 智能驱动。_",
		false, false,
	)
	return slack.NewContextBlock("", helpText)
}

// BuildCapabilityBlocks builds all capability blocks organized by category.
func (b *Builder) BuildCapabilityBlocks() []slack.Block {
	var blocks []slack.Block

	categories := b.registry.GetCategories()
	capabilities := b.registry.GetAll()

	// Build capability map by category
	capByCategory := make(map[string][]Capability)
	for _, cap := range capabilities {
		capByCategory[cap.Category] = append(capByCategory[cap.Category], cap)
	}

	// Build each category section
	for _, cat := range categories {
		caps, ok := capByCategory[cat.ID]
		if !ok || len(caps) == 0 {
			continue
		}

		// Category header
		blocks = append(blocks, b.buildCategoryHeader(cat))

		// Each capability as a section with button
		for _, cap := range caps {
			blocks = append(blocks, b.BuildCapabilitySection(cap))
		}
	}

	return blocks
}

// BuildFullHomeView builds the complete Home Tab with proper block structure.
func (b *Builder) BuildFullHomeView() *slack.HomeTabViewRequest {
	var blocks []slack.Block

	// Header
	blocks = append(blocks, b.buildHeader())

	// Introduction
	introText := slack.NewTextBlockObject(
		slack.MarkdownType,
		"欢迎使用 HotPlex 能力中心！选择一个能力开始工作。",
		false, false,
	)
	blocks = append(blocks, slack.NewSectionBlock(introText, nil, nil))
	blocks = append(blocks, slack.NewDividerBlock())

	// Capabilities by category
	blocks = append(blocks, b.BuildCapabilityBlocks()...)

	// Footer
	blocks = append(blocks, slack.NewDividerBlock())
	blocks = append(blocks, b.buildFooter())

	return &slack.HomeTabViewRequest{
		Type:   slack.VTHomeTab,
		Blocks: slack.Blocks{BlockSet: blocks},
	}
}
