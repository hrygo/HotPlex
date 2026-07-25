package gateway

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// maxPendingPerSession caps buffered supplements per session to bound memory.
const maxPendingPerSession = 20

type pendingEntry struct {
	content    string
	envelope   *events.Envelope
	receivedAt int64
}

// PendingBuffer holds user supplements that arrived while a session was busy,
// keyed by sessionID. Only used for workers that do NOT implement
// MidTurnInjector (acp/ocs fallback); mid-turn-capable workers inject directly.
type PendingBuffer struct {
	mu    sync.Mutex
	items map[string][]pendingEntry
}

func NewPendingBuffer() *PendingBuffer {
	return &PendingBuffer{items: make(map[string][]pendingEntry)}
}

// Append adds a supplement. Adjacent identical content is deduped; over the
// per-session cap the oldest are dropped to keep the newest.
func (p *PendingBuffer) Append(sessionID, content string, env *events.Envelope) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[sessionID]
	if n := len(items); n > 0 && items[n-1].content == content {
		return
	}
	items = append(items, pendingEntry{content: content, envelope: events.Clone(env), receivedAt: time.Now().UnixMilli()})
	if len(items) > maxPendingPerSession {
		items = items[len(items)-maxPendingPerSession:]
	}
	p.items[sessionID] = items
}

// DrainAndMerge atomically removes and returns the merged supplement for a
// session. A single entry is returned verbatim; multiple are joined with a
// header and 1-based numbering. repr is the last entry's envelope (template
// for replay). ok is false if nothing was buffered.
func (p *PendingBuffer) DrainAndMerge(sessionID string) (string, *events.Envelope, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[sessionID]
	if len(items) == 0 {
		delete(p.items, sessionID)
		return "", nil, false
	}
	delete(p.items, sessionID)
	repr := items[len(items)-1].envelope
	if len(items) == 1 {
		return items[0].content, repr, true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "（以下是上一轮执行期间追加的 %d 条消息，请一并处理）\n", len(items))
	for i, it := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(it.content))
	}
	return b.String(), repr, true
}

func (p *PendingBuffer) Clear(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, sessionID)
}

func (p *PendingBuffer) ClearAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = make(map[string][]pendingEntry)
}

// cloneForReplay builds a fresh Input envelope for replay: new id + new
// client_message_id (avoids UNIQUE(session_id, client_message_id) dedup),
// replaced content, reused session/owner/metadata, seq=0 (re-assigned by hub).
// Lives in gateway (not pkg/events) because it needs aep.NewID and pkg/events
// cannot import pkg/aep (cycle).
func cloneForReplay(env *events.Envelope, content string) *events.Envelope {
	c := events.Clone(env) // deep-copies Event.Data map + Metadata
	c.ID = aep.NewID()
	c.Seq = 0
	if data, ok := c.Event.Data.(map[string]any); ok {
		data["content"] = content
		data["client_message_id"] = aep.NewID()
	}
	return c
}
