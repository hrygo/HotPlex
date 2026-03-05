package chatapps

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// MessageProcessor defines the interface for processing messages before sending
type MessageProcessor interface {
	// Process processes a message and returns the processed message
	// Can return the same message pointer if no modification needed
	// Can return a new message pointer if modification needed
	// Can return an error to stop processing
	Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error)

	// Name returns the processor name for logging and debugging
	Name() string

	// Order returns the processor order in the chain (lower = earlier)
	Order() int
}

// ProcessorChain executes a chain of message processors
type ProcessorChain struct {
	processors []MessageProcessor
	mu         sync.RWMutex
}

// NewProcessorChain creates a new processor chain with the given processors
// Processors are automatically sorted by Order()
func NewProcessorChain(processors ...MessageProcessor) *ProcessorChain {
	chain := &ProcessorChain{
		processors: make([]MessageProcessor, len(processors)),
	}
	copy(chain.processors, processors)
	chain.sortProcessors()
	return chain
}

// AddProcessor adds a processor to the chain and re-sorts
// Note: This method is thread-safe but should preferably be called during initialization
func (c *ProcessorChain) AddProcessor(processor MessageProcessor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processors = append(c.processors, processor)
	c.sortProcessorsLocked()
}

// Process executes all processors in order
func (c *ProcessorChain) Process(ctx context.Context, msg *base.ChatMessage) (*base.ChatMessage, error) {
	if msg == nil {
		return nil, nil
	}

	processors := func() []MessageProcessor {
		c.mu.RLock()
		defer c.mu.RUnlock()
		res := make([]MessageProcessor, len(c.processors))
		copy(res, c.processors)
		return res
	}()

	current := msg
	for _, processor := range processors {
		var err error
		current, err = processor.Process(ctx, current)
		if err != nil {
			return nil, err
		}
		if current == nil {
			// Processor decided to drop the message
			return nil, nil
		}
	}
	return current, nil
}

// ResetSession propagate session reset to all processors that support it
func (c *ProcessorChain) ResetSession(platform, sessionID string) {
	processors := func() []MessageProcessor {
		c.mu.RLock()
		defer c.mu.RUnlock()
		res := make([]MessageProcessor, len(c.processors))
		copy(res, c.processors)
		return res
	}()

	for _, p := range processors {
		if r, ok := p.(interface{ ResetSession(string, string) }); ok {
			r.ResetSession(platform, sessionID)
		}
	}
}

// sortProcessors sorts processors by Order() - not thread-safe, caller must hold lock
func (c *ProcessorChain) sortProcessors() {
	c.sortProcessorsLocked()
}

// sortProcessorsLocked sorts processors by Order() - must be called with lock held
func (c *ProcessorChain) sortProcessorsLocked() {
	sort.Slice(c.processors, func(i, j int) bool {
		return c.processors[i].Order() < c.processors[j].Order()
	})
}

// ProcessorOrder defines standard processor ordering
type ProcessorOrder int

const (
	// OrderFilter drops noise events before anything else
	OrderFilter ProcessorOrder = 10
	// OrderThread manages thread_ts caching for message chunking
	OrderThread ProcessorOrder = 15
	// OrderFormatConversion converts markdown to platform-specific format
	OrderFormatConversion ProcessorOrder = 40
	// OrderChunk splits long messages for Slack API limits
	OrderChunk ProcessorOrder = 50
)

// NewDefaultProcessorChain creates a default processor chain with all standard processors
func NewDefaultProcessorChain(ctx context.Context, logger *slog.Logger) *ProcessorChain {
	filter := NewMessageFilterProcessor(logger)

	thread := NewThreadProcessor(logger, ThreadProcessorOptions{
		TTL: 30 * time.Minute,
	})

	formatConv := NewFormatConversionProcessor(logger)

	chunk := NewChunkProcessor(logger, ChunkProcessorOptions{
		MaxChars: 4000,
	})

	return NewProcessorChain(
		filter,
		thread,
		formatConv,
		chunk,
	)
}

// Name returns the processor chain name
func (c *ProcessorChain) Name() string {
	return "chain"
}

// Order returns the processor chain order
func (c *ProcessorChain) Order() int {
	return 0 // Chain doesn't have a specific order, it contains processors with orders
}

// Close stops all processors that have background goroutines
func (c *ProcessorChain) Close() {
	c.mu.RLock()
	processors := make([]MessageProcessor, len(c.processors))
	copy(processors, c.processors)
	c.mu.RUnlock()

	for _, p := range processors {
		if stoppable, ok := p.(interface{ Stop() }); ok {
			func() {
				defer func() {
					if r := recover(); r != nil {
						processorName := "unknown"
						if namer, ok := p.(interface{ Name() string }); ok {
							processorName = namer.Name()
						}
						slog.Default().Error("Processor Stop() panic recovered",
							"processor", processorName,
							"panic", r)
					}
				}()
				stoppable.Stop()
			}()
		}
	}
}

// Verify ProcessorChain implements MessageProcessor at compile time
var _ MessageProcessor = (*ProcessorChain)(nil)
