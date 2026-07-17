package opencodeserver

import "sync"

// ringBuffer is a fixed-capacity, thread-safe ring buffer of stderr lines.
// It retains the most recent N lines so startup failures can surface a bounded,
// redacted tail instead of forcing operators to grep the gateway log at the
// exact crash timestamp.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	cap  int
	head int // next write position
	n    int // item count (0..cap)
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity < 0 {
		capacity = 0
	}
	return &ringBuffer{cap: capacity, buf: make([]string, capacity)}
}

func (r *ringBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cap == 0 {
		return
	}
	r.buf[r.head] = line
	r.head = (r.head + 1) % r.cap
	if r.n < r.cap {
		r.n++
	}
}

// Lines returns a snapshot of buffered lines in oldest→newest order, or nil if
// empty.
func (r *ringBuffer) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n == 0 {
		return nil
	}
	out := make([]string, 0, r.n)
	start := (r.head - r.n + r.cap) % r.cap // oldest entry
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(start+i)%r.cap])
	}
	return out
}
