package supportchat

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/platform/db"
)

// Event is a live signal that something changed. It carries ids only: a
// stream that receives it reads the change from the database, through the
// same authorisation as an ordinary request, so a signal can never leak
// content (or an internal note) to the wrong listener.
type Event struct {
	Kind         string `json:"k"` // message, read, status, typing
	Conversation string `json:"c"`
	Seq          int64  `json:"s,omitempty"`
	Actor        string `json:"a,omitempty"` // customer, guest, agent
}

// notify sends an event on commit of the transaction q belongs to (or at
// once on a pool). PostgreSQL delivers it to every API instance listening.
func notify(ctx context.Context, q db.Querier, e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, string(b))
	return err
}

// Hub fans events from one LISTEN connection out to the streams of this API
// instance. It connects when the first stream subscribes and disconnects
// when the last one leaves.
type Hub struct {
	pool *pgxpool.Pool

	mu     sync.Mutex
	subs   map[*Sub]struct{}
	cancel context.CancelFunc
	ready  chan struct{}
}

func NewHub(pool *pgxpool.Pool) *Hub { return &Hub{pool: pool, subs: map[*Sub]struct{}{}} }

// Sub is one stream's subscription. C receives a value whenever a matching
// event arrives; events are coalesced when the stream is busy (the stream
// re-reads the database, so nothing is lost by coalescing).
type Sub struct {
	C      chan Event
	filter func(Event) bool
}

// Subscribe registers a stream and waits (briefly) for the listener to be
// connected, so an event committed after Subscribe returns is not missed.
func (h *Hub) Subscribe(filter func(Event) bool) *Sub {
	s := &Sub{C: make(chan Event, 16), filter: filter}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel, h.ready = cancel, make(chan struct{})
		go h.run(ctx, h.ready)
	}
	ready := h.ready
	h.mu.Unlock()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		// Streams also poll, so a slow listener only delays delivery.
	}
	return s
}

func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, s)
	if len(h.subs) == 0 && h.cancel != nil {
		h.cancel()
		h.cancel, h.ready = nil, nil
	}
}

func (h *Hub) publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if s.filter != nil && !s.filter(e) {
			continue
		}
		select {
		case s.C <- e:
		default: // full: the stream will catch up from the database
		}
	}
}

// run keeps one dedicated connection (outside the pool, so closing the pool
// is never blocked by it) listening until ctx ends, reconnecting on errors.
func (h *Hub) run(ctx context.Context, ready chan struct{}) {
	signalled := false
	for ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(ctx, h.pool.Config().ConnConfig.Copy())
		if err == nil {
			if _, err = conn.Exec(ctx, `LISTEN `+Channel); err == nil {
				if !signalled {
					close(ready)
					signalled = true
				}
				for {
					n, werr := conn.WaitForNotification(ctx)
					if werr != nil {
						err = werr
						break
					}
					var e Event
					if json.Unmarshal([]byte(n.Payload), &e) == nil {
						h.publish(e)
					}
				}
			}
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = conn.Close(closeCtx)
			cancel()
		}
		if ctx.Err() != nil {
			break
		}
		slog.Warn("support chat listener lost its connection; reconnecting", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	if !signalled {
		close(ready)
	}
}

// throttle lets one key through at most once per interval (typing).
type throttle struct {
	mu    sync.Mutex
	every time.Duration
	last  map[string]time.Time
}

func newThrottle(every time.Duration) *throttle {
	return &throttle{every: every, last: map[string]time.Time{}}
}

func (t *throttle) allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if prev, ok := t.last[key]; ok && now.Sub(prev) < t.every {
		return false
	}
	if len(t.last) > 10000 {
		for k, v := range t.last {
			if now.Sub(v) > t.every {
				delete(t.last, k)
			}
		}
	}
	t.last[key] = now
	return true
}
