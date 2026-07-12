package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// Hub fans out live snapshots to all connected WebSocket clients.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

type client struct {
	send chan domain.Snapshot
	pong chan int64
}

func NewHub() *Hub { return &Hub{clients: map[*client]struct{}{}} }

// Broadcast delivers a snapshot to every connected client, dropping it for any
// client that is too slow to keep up rather than blocking the control loop.
func (h *Hub) Broadcast(snap domain.Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- snap:
		default: // slow consumer; skip this frame
		}
	}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// serveWS upgrades the connection and streams snapshots until the client
// disconnects. It sends the provided initial snapshot immediately. Each frame
// is filtered to the environments the connected user may view (all=true for
// admins, streaming everything).
func (h *Hub) serveWS(c *websocket.Conn, initial domain.Snapshot, allowed map[string]bool, all bool) {
	ctx := context.Background()
	cl := &client{send: make(chan domain.Snapshot, 4), pong: make(chan int64, 2)}
	h.add(cl)
	defer h.remove(cl)

	// Detect client-side close so we can stop writing.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			_, raw, err := c.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				ID   int64  `json:"id"`
			}
			if json.Unmarshal(raw, &msg) == nil && msg.Type == "ping" {
				select {
				case cl.pong <- msg.ID:
				default:
				}
			}
		}
	}()

	if err := writeSnap(ctx, c, initial); err != nil {
		return
	}
	for {
		select {
		case <-closed:
			return
		case snap := <-cl.send:
			if err := writeSnap(ctx, c, filterSnapshot(snap, allowed, all)); err != nil {
				return
			}
		case id := <-cl.pong:
			if err := writeMessage(ctx, c, map[string]any{"type": "pong", "id": id}); err != nil {
				return
			}
		}
	}
}

func writeMessage(ctx context.Context, c *websocket.Conn, message any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, message)
}

func writeSnap(ctx context.Context, c *websocket.Conn, snap domain.Snapshot) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, snap)
}
