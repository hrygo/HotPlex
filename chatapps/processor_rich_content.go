package chatapps

import (
	"context"
	"log/slog"

	"github.com/hrygo/hotplex/chatapps/base"
)

// RichContentProcessor processes RichContent (reactions, attachments, blocks)
// and converts them to platform-specific formats
type RichContentProcessor struct {
	logger *slog.Logger
}

// NewRichContentProcessor creates a new RichContentProcessor
func NewRichContentProcessor(logger *slog.Logger) *RichContentProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &RichContentProcessor{
		logger: logger,
	}
}

// Name returns the processor name
func (p *RichContentProcessor) Name() string {
	return "RichContentProcessor"
}

// Order returns the processor order
func (p *RichContentProcessor) Order() int {
	return int(OrderRichContent)
}

// Process processes the message's RichContent
func (p *RichContentProcessor) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg.RichContent == nil {
		return msg, nil
	}

	rc := msg.RichContent

	// Process attachments based on platform
	if len(rc.Attachments) > 0 {
		p.processAttachments(msg)
	}

	// Process reactions - ensure they have required metadata

	// Process embeds for platforms that support them (Discord)
	if len(rc.Embeds) > 0 {
		p.processEmbeds(msg)
	}

	return msg, nil
}

// processAttachments processes attachments for platform-specific format
func (p *RichContentProcessor) processAttachments(msg *base.ChatMessage) {
	// Attachments are already in base.Attachment format
	// Platform-specific adapters will handle the actual conversion
	p.logger.Debug("Processing attachments",
		"platform", msg.Platform,
		"count", len(msg.RichContent.Attachments))
}

// processEmbeds processes embeds for platforms like Discord
func (p *RichContentProcessor) processEmbeds(msg *base.ChatMessage) {
	// Embeds are platform-agnostic
	// Discord adapter will convert to Discord Embed format
	p.logger.Debug("Processing embeds",
		"platform", msg.Platform,
		"count", len(msg.RichContent.Embeds))
}
