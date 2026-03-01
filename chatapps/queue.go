package chatapps

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type QueuedMessage struct {
	Platform  string
	SessionID string
	Message   *ChatMessage
	Retries   int
	CreatedAt time.Time
}

type MessageQueue struct {
	mu      sync.RWMutex
	queue   []*QueuedMessage
	dlq     []*QueuedMessage // Dead Letter Queue for failed messages
	logger  *slog.Logger
	maxSize int
	dlqSize int
	workers int
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewMessageQueue(logger *slog.Logger, maxSize, dlqSize, workers int) *MessageQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &MessageQueue{
		logger:  logger,
		maxSize: maxSize,
		dlqSize: dlqSize,
		workers: workers,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// AddToDLQ stores a failed message to the Dead Letter Queue
func (q *MessageQueue) AddToDLQ(msg *QueuedMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// If DLQ is full, remove oldest
	if q.dlqSize > 0 && len(q.dlq) >= q.dlqSize {
		q.dlq = q.dlq[1:]
	}
	q.dlq = append(q.dlq, msg)
	q.logger.Warn("Message moved to DLQ", "platform", msg.Platform, "retries", msg.Retries)
}

// GetDLQ returns all messages in the Dead Letter Queue
func (q *MessageQueue) GetDLQ() []*QueuedMessage {
	q.mu.RLock()
	defer q.mu.RUnlock()
	result := make([]*QueuedMessage, len(q.dlq))
	copy(result, q.dlq)
	return result
}

// DLQLen returns the number of messages in the Dead Letter Queue
func (q *MessageQueue) DLQLen() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.dlq)
}

func (q *MessageQueue) Enqueue(platform, sessionID string, msg *ChatMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) >= q.maxSize {
		return ErrQueueFull
	}

	queued := &QueuedMessage{
		Platform:  platform,
		SessionID: sessionID,
		Message:   msg,
		Retries:   0,
		CreatedAt: time.Now(),
	}
	q.queue = append(q.queue, queued)
	return nil
}

func (q *MessageQueue) Dequeue() (*QueuedMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.queue) == 0 {
		return nil, false
	}

	msg := q.queue[0]
	q.queue = q.queue[1:]
	return msg, true
}

func (q *MessageQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.queue)
}

func (q *MessageQueue) Start(adapterGetter func(string) (ChatAdapter, bool), sendFn func(context.Context, string, string, *ChatMessage) error) {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i, adapterGetter, sendFn)
	}
}

func (q *MessageQueue) worker(_ int, _ func(string) (ChatAdapter, bool), sendFn func(context.Context, string, string, *ChatMessage) error) {
	defer q.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-ticker.C:
			msg, ok := q.Dequeue()
			if !ok {
				continue
			}
			if err := sendFn(q.ctx, msg.Platform, msg.SessionID, msg.Message); err != nil {
				msg.Retries++
				if msg.Retries < 3 {
					if err := q.Requeue(msg); err != nil {
						q.logger.Error("Requeue failed, moving to DLQ", "error", err, "platform", msg.Platform)
						q.AddToDLQ(msg)
					} else {
						q.logger.Warn("Message retry", "platform", msg.Platform, "retries", msg.Retries)
					}
				} else {
					q.AddToDLQ(msg)
				}
			}
		}
	}
}

// Requeue adds a message back to the queue for retry
func (q *MessageQueue) Requeue(msg *QueuedMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check size limit before re-queuing
	if q.maxSize > 0 && len(q.queue) >= q.maxSize {
		return ErrQueueFull
	}
	q.queue = append(q.queue, msg)
	return nil
}

func (q *MessageQueue) Stop() {
	q.cancel()
	q.wg.Wait()
}

var ErrQueueFull = &QueueError{Message: "queue is full"}

type QueueError struct {
	Message string
}

func (e *QueueError) Error() string {
	return e.Message
}
