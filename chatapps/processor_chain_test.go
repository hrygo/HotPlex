package chatapps

import (
	"context"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
)

func TestProcessorChain_SortProcessors(t *testing.T) {
	formatConv := NewFormatConversionProcessor(nil)
	rateLimit := NewRateLimitProcessor(nil, RateLimitProcessorOptions{})
	richContent := NewRichContentProcessor(nil)
	aggregator := NewMessageAggregatorProcessor(context.Background(), nil, MessageAggregatorProcessorOptions{})

	chain := NewProcessorChain(formatConv, rateLimit, richContent, aggregator)

	expectedOrder := []int{
		int(OrderRateLimit),
		int(OrderAggregation),
		int(OrderRichContent),
		int(OrderFormatConversion),
	}

	for i, processor := range chain.processors {
		if processor.Order() != expectedOrder[i] {
			t.Errorf("Processor %d has order %d, expected %d", i, processor.Order(), expectedOrder[i])
		}
	}
}

func TestProcessorChain_Process(t *testing.T) {
	chain := NewDefaultProcessorChain(context.Background(), nil)

	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "Hello **world**",
		Metadata: map[string]any{
			"stream":   true,
			"is_final": true,
		},
		RichContent: &base.RichContent{
			ParseMode: base.ParseModeMarkdown,
		},
	}

	processed, err := chain.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if processed == nil {
		t.Fatal("Process returned nil")
	}

	if processed.Content != "Hello *world*" {
		t.Errorf("Content not converted: got %q, expected %q", processed.Content, "Hello *world*")
	}
}

func TestProcessorChain_ProcessNilMessage(t *testing.T) {
	chain := NewDefaultProcessorChain(context.Background(), nil)

	processed, err := chain.Process(context.Background(), nil)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if processed != nil {
		t.Errorf("Expected nil for nil input, got %v", processed)
	}
}

func TestDefaultProcessorChain_Creation(t *testing.T) {
	chain := NewDefaultProcessorChain(context.Background(), nil)

	if chain == nil {
		t.Fatal("NewDefaultProcessorChain returned nil")
	}

	// Now we have 8 processors: filter, rateLimit, zoneOrder, thread, aggregator, richContent, formatConv, chunk
	if len(chain.processors) != 8 {
		t.Errorf("Expected 8 processors, got %d", len(chain.processors))
	}

	expectedOrders := []ProcessorOrder{
		OrderFilter,
		OrderRateLimit,
		OrderZoneOrder,
		OrderThread,
		OrderAggregation,
		OrderRichContent,
		OrderFormatConversion,
		OrderChunk,
	}

	for i, processor := range chain.processors {
		if i >= len(expectedOrders) {
			break
		}
		if processor.Order() != int(expectedOrders[i]) {
			t.Errorf("Processor %d order mismatch: got %d, expected %d",
				i, processor.Order(), int(expectedOrders[i]))
		}
	}
}

func TestProcessorChain_AddProcessor(t *testing.T) {
	chain := NewProcessorChain()

	chain.AddProcessor(NewFormatConversionProcessor(nil))
	chain.AddProcessor(NewRateLimitProcessor(nil, RateLimitProcessorOptions{}))

	if len(chain.processors) != 2 {
		t.Errorf("Expected 2 processors, got %d", len(chain.processors))
	}

	if chain.processors[0].Order() > chain.processors[1].Order() {
		t.Error("Processors not sorted after AddProcessor")
	}
}

// TestFormatConversionProcessor_CodeBlockProtection tests that code blocks are preserved
func TestFormatConversionProcessor_CodeBlockProtection(t *testing.T) {
	processor := NewFormatConversionProcessor(nil)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bold outside code block",
			input:    "Hello **world**",
			expected: "Hello *world*",
		},
		{
			name:     "bold inside code block should not convert",
			input:    "Text ```**not bold**``` more",
			expected: "Text ```**not bold**``` more",
		},
		{
			name:     "mixed content",
			input:    "**bold** ```code **not bold**``` **more bold**",
			expected: "*bold* ```code **not bold**``` *more bold*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &base.ChatMessage{
				Platform:  "slack",
				SessionID: "test",
				Content:   tt.input,
				RichContent: &base.RichContent{
					ParseMode: base.ParseModeMarkdown,
				},
			}

			result, err := processor.Process(context.Background(), msg)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}

			// Unescape for comparison since we escape & < >
			if result.Content != tt.expected {
				t.Errorf("got %q, expected %q", result.Content, tt.expected)
			}
		})
	}
}

// TestRateLimitProcessor_Basic tests basic rate limiting functionality
func TestRateLimitProcessor_Basic(t *testing.T) {
	processor := NewRateLimitProcessor(nil, RateLimitProcessorOptions{
		MinInterval: 50 * 1e6, // 50ms in nanoseconds
	})

	ctx := context.Background()
	msg := &base.ChatMessage{
		Platform:  "slack",
		SessionID: "test-session",
		Content:   "test",
	}

	// First message should pass immediately
	_, err := processor.Process(ctx, msg)
	if err != nil {
		t.Fatalf("First process failed: %v", err)
	}

	// Cleanup
	processor.Cleanup()
}
