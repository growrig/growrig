// Package ha is a Home Assistant adapter for Grow Core.
//
// It reads climate sensors and commands fans through Home Assistant, so the
// same Grow Core binary drives real ESPHome controllers (via the HA native API
// path) using only configuration for the connection and per-device entity
// bindings stored in the database. It implements control.Adapter.
//
// State arrives over the Home Assistant WebSocket API (authenticated, then a
// subscription to state_changed events). Commands are issued over the REST API
// (fan.set_percentage), which keeps writes simple and independent of the event
// stream.
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/growrig/growrig-platform/growcore/internal/config"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// staleAfter marks a device offline if no state has arrived for this long.
const staleAfter = 90 * time.Second

type Adapter struct {
	restBase string
	wsURL    string
	token    string
	client   *http.Client

	mu        sync.RWMutex
	values    map[string]float64 // entity_id -> numeric state
	connected bool
	lastState time.Time
}

// New builds the adapter from the connection configuration. Entity bindings
// come from each device/channel at call time, not from config.
func New(cfg *config.Config) (*Adapter, error) {
	wsURL, err := websocketURL(cfg.HomeAssistant.URL)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		restBase: strings.TrimRight(cfg.HomeAssistant.URL, "/") + "/api",
		wsURL:    wsURL,
		token:    cfg.HomeAssistant.Token,
		client:   &http.Client{Timeout: 10 * time.Second},
		values:   map[string]float64{},
	}, nil
}

// Start launches the WebSocket manager. It does not block on connectivity:
// Home Assistant may still be starting (e.g. during a HAOS boot), so the
// adapter reports offline health until the first connection succeeds.
func (a *Adapter) Start(ctx context.Context) error {
	go a.manage(ctx)
	return nil
}

func (a *Adapter) Close() error { return nil }

func (a *Adapter) Tick(time.Duration) {} // state is event-driven

func (a *Adapter) Climate(dev domain.Device) (float64, float64, bool) {
	if dev.TempEntity == "" {
		return 0, 0, false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	temp, ok := a.values[dev.TempEntity]
	if !ok {
		return 0, 0, false
	}
	humidity := a.values[dev.HumidityEntity] // 0 if unbound/absent
	return temp, humidity, true
}

func (a *Adapter) FanRPM(_ domain.Device, ch domain.Channel) (int, bool) {
	if ch.RPMEntity == "" {
		return 0, false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.values[ch.RPMEntity]
	return int(v), ok
}

func (a *Adapter) Health(domain.Device) domain.ControllerHealth {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.connected {
		return domain.HealthOffline
	}
	if !a.lastState.IsZero() && time.Since(a.lastState) > staleAfter {
		return domain.HealthStale
	}
	return domain.HealthOnline
}

// SetSpeed commands a fan channel via the fan.set_percentage service.
func (a *Adapter) SetSpeed(_ domain.Device, ch domain.Channel, speed int) error {
	if ch.Entity == "" {
		return nil // unbound channel (e.g. unassigned role)
	}
	body, _ := json.Marshal(map[string]any{"entity_id": ch.Entity, "percentage": speed})
	req, err := http.NewRequest(http.MethodPost, a.restBase+"/services/fan/set_percentage", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("set_percentage %s: HTTP %d", ch.Entity, resp.StatusCode)
	}
	return nil
}

// --- WebSocket manager ---

func (a *Adapter) manage(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := a.session(ctx); err != nil && ctx.Err() == nil {
			log.Printf("ha: session ended: %v (retrying in %s)", err, backoff)
		}
		a.setConnected(false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

// session runs one full connect → auth → subscribe → consume cycle.
func (a *Adapter) session(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, a.wsURL, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20) // state dumps can be large

	if err := a.authenticate(ctx, conn); err != nil {
		return err
	}

	// Prime the cache with current states, then subscribe to changes.
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 1, "type": "get_states"}); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{
		"id": 2, "type": "subscribe_events", "event_type": "state_changed",
	}); err != nil {
		return err
	}
	a.setConnected(true)
	log.Printf("ha: connected to %s", a.wsURL)

	for ctx.Err() == nil {
		var msg wsMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return err
		}
		a.handle(&msg)
	}
	return ctx.Err()
}

func (a *Adapter) authenticate(ctx context.Context, conn *websocket.Conn) error {
	var hello wsMessage
	if err := wsjson.Read(ctx, conn, &hello); err != nil {
		return fmt.Errorf("read auth_required: %w", err)
	}
	if hello.Type != "auth_required" {
		return fmt.Errorf("unexpected first message %q", hello.Type)
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "auth", "access_token": a.token}); err != nil {
		return err
	}
	var result wsMessage
	if err := wsjson.Read(ctx, conn, &result); err != nil {
		return fmt.Errorf("read auth result: %w", err)
	}
	if result.Type != "auth_ok" {
		return fmt.Errorf("authentication failed: %s", result.Type)
	}
	return nil
}

func (a *Adapter) handle(msg *wsMessage) {
	switch msg.Type {
	case "result":
		// Response to get_states: an array of state objects.
		var states []haState
		if len(msg.Result) > 0 && json.Unmarshal(msg.Result, &states) == nil {
			for _, s := range states {
				a.storeState(s.EntityID, s.State)
			}
		}
	case "event":
		var ev struct {
			EventType string `json:"event_type"`
			Data      struct {
				EntityID string   `json:"entity_id"`
				NewState *haState `json:"new_state"`
			} `json:"data"`
		}
		if json.Unmarshal(msg.Event, &ev) == nil && ev.Data.NewState != nil {
			a.storeState(ev.Data.EntityID, ev.Data.NewState.State)
		}
	}
}

// storeState caches an entity's numeric state, ignoring non-numeric values
// (e.g. "unavailable", or a fan's on/off state, which Grow Core commands).
func (a *Adapter) storeState(entityID, state string) {
	v, err := strconv.ParseFloat(state, 64)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.values[entityID] = v
	a.lastState = time.Now()
	a.mu.Unlock()
}

func (a *Adapter) setConnected(v bool) {
	a.mu.Lock()
	a.connected = v
	a.mu.Unlock()
}

// --- helpers & wire types ---

// websocketURL derives the HA WebSocket endpoint from the base HTTP URL.
func websocketURL(base string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid homeassistant.url %q: %w", base, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported scheme %q in homeassistant.url", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/websocket"
	return u.String(), nil
}

type wsMessage struct {
	ID     int             `json:"id"`
	Type   string          `json:"type"`
	Result json.RawMessage `json:"result"`
	Event  json.RawMessage `json:"event"`
}

type haState struct {
	EntityID string `json:"entity_id"`
	State    string `json:"state"`
}
