package api

import (
	"context"
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
	cl := &client{send: make(chan domain.Snapshot, 4)}
	h.add(cl)
	defer h.remove(cl)

	// Detect client-side close so we can stop writing.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
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
		}
	}
}

func writeSnap(ctx context.Context, c *websocket.Conn, snap domain.Snapshot) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, snap)
}
