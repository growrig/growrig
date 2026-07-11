// Package ha is a Home Assistant adapter for Grow Core.
//
// It reads sensors and commands fans/lights through Home Assistant, and
// discovers bindable entities from HA so users can add devices by picking from
// what they already have. It implements control.Adapter.
//
// State (and entity metadata) arrives over the Home Assistant WebSocket API
// (authenticate, then subscribe to state_changed). Commands are issued over the
// REST API, which keeps writes simple and independent of the event stream.
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/growrig/growrig-platform/growcore/internal/config"
	"github.com/growrig/growrig-platform/growcore/internal/control"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

const staleAfter = 90 * time.Second

type entityMeta struct {
	name           string
	deviceClass    string
	deviceID       string
	deviceName     string
	integration    string
	entityCategory string
	manufacturer   string
	model          string
	unit           string
}

type Adapter struct {
	restBase string
	wsURL    string
	token    string
	client   *http.Client

	mu        sync.RWMutex
	values    map[string]float64    // numeric states
	states    map[string]string     // raw states (for on/off)
	meta      map[string]entityMeta // for discovery
	connected bool
	lastState time.Time
}

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
		states:   map[string]string{},
		meta:     map[string]entityMeta{},
	}, nil
}

func (a *Adapter) Start(ctx context.Context) error {
	go a.manage(ctx)
	return nil
}

func (a *Adapter) Close() error       { return nil }
func (a *Adapter) Tick(time.Duration) {} // event-driven

func (a *Adapter) Value(entity string) (float64, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.values[entity]
	return v, ok
}

func (a *Adapter) SwitchState(entity string) (bool, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.states[entity]
	if !ok {
		return false, false
	}
	return s == "on", true
}

func (a *Adapter) Health() domain.ControllerHealth {
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

// SetFan commands a fan entity via fan.set_percentage.
func (a *Adapter) SetFan(entity string, speed int) error {
	if entity == "" {
		return nil
	}
	return a.callService("fan", "set_percentage",
		map[string]any{"entity_id": entity, "percentage": speed})
}

// SetSwitch turns a switchable entity on or off using its own domain's service.
func (a *Adapter) SetSwitch(entity string, on bool) error {
	if entity == "" {
		return nil
	}
	service := "turn_off"
	if on {
		service = "turn_on"
	}
	return a.callService(serviceDomain(entity), service, map[string]any{"entity_id": entity})
}

func (a *Adapter) callService(domainName, service string, body map[string]any) error {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost,
		a.restBase+"/services/"+domainName+"/"+service, strings.NewReader(string(payload)))
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
		return fmt.Errorf("%s.%s: HTTP %d", domainName, service, resp.StatusCode)
	}
	return nil
}

// Discover classifies cached entities into bindable candidates.
func (a *Adapter) Discover() []control.DiscoveredEntity {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []control.DiscoveredEntity
	for entity, m := range a.meta {
		d := control.DiscoveredEntity{Entity: entity, Name: m.name, HADeviceID: m.deviceID, DeviceName: m.deviceName, Integration: m.integration, EntityCategory: m.entityCategory, Manufacturer: m.manufacturer, Model: m.model, Unit: m.unit}
		if d.Name == "" {
			d.Name = entity
		}
		switch serviceDomain(entity) {
		case "sensor":
			switch m.deviceClass {
			case "temperature":
				d.Kind, d.Measurement = domain.KindSensor, domain.MeasureTemperature
			case "humidity":
				d.Kind, d.Measurement = domain.KindSensor, domain.MeasureHumidity
			case "carbon_dioxide":
				d.Kind, d.Measurement = domain.KindSensor, domain.MeasureCO2
			case "power":
				d.Kind, d.Measurement = domain.KindSensor, domain.MeasurePower
			default:
				if strings.EqualFold(strings.TrimSpace(m.unit), "rpm") {
					d.Kind = domain.KindSensor
				} else {
					continue
				}
			}
		case "fan":
			d.Kind = domain.KindController
		case "light":
			d.Kind = domain.KindLight
		case "switch":
			d.Kind = domain.KindPower
		case "camera":
			d.Kind = domain.KindCamera
		default:
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
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

func (a *Adapter) session(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, _, err := websocket.Dial(dialCtx, a.wsURL, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)

	if err := a.authenticate(ctx, conn); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 1, "type": "get_states"}); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{
		"id": 2, "type": "subscribe_events", "event_type": "state_changed",
	}); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 3, "type": "config/entity_registry/list"}); err != nil {
		return err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 4, "type": "config/device_registry/list"}); err != nil {
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
		switch msg.ID {
		case 1:
			var states []haState
			if json.Unmarshal(msg.Result, &states) == nil {
				for _, st := range states {
					a.storeState(st)
				}
			}
		case 3:
			a.storeEntityRegistry(msg.Result)
		case 4:
			a.storeDeviceRegistry(msg.Result)
		}
	case "event":
		var ev struct {
			Data struct {
				NewState *haState `json:"new_state"`
			} `json:"data"`
		}
		if json.Unmarshal(msg.Event, &ev) == nil && ev.Data.NewState != nil {
			a.storeState(*ev.Data.NewState)
		}
	}
}

func (a *Adapter) storeState(st haState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.states[st.EntityID] = st.State
	m := a.meta[st.EntityID]
	m.name, m.deviceClass, m.unit = st.Attributes.FriendlyName, st.Attributes.DeviceClass, st.Attributes.Unit
	a.meta[st.EntityID] = m
	if v, err := strconv.ParseFloat(st.State, 64); err == nil {
		a.values[st.EntityID] = v
	}
	a.lastState = time.Now()
}

func (a *Adapter) storeEntityRegistry(raw json.RawMessage) {
	// HA uses snake_case on the wire.
	var wire []struct {
		EntityID       string `json:"entity_id"`
		DeviceID       string `json:"device_id"`
		Platform       string `json:"platform"`
		EntityCategory string `json:"entity_category"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range wire {
		m := a.meta[e.EntityID]
		m.deviceID, m.integration, m.entityCategory = e.DeviceID, e.Platform, e.EntityCategory
		a.meta[e.EntityID] = m
	}
}

func (a *Adapter) storeDeviceRegistry(raw json.RawMessage) {
	var devices []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		NameByUser   string `json:"name_by_user"`
		Manufacturer string `json:"manufacturer"`
		Model        string `json:"model"`
	}
	if json.Unmarshal(raw, &devices) != nil {
		return
	}
	type deviceInfo struct{ name, manufacturer, model string }
	infos := map[string]deviceInfo{}
	for _, d := range devices {
		name := d.Name
		if d.NameByUser != "" {
			name = d.NameByUser
		}
		infos[d.ID] = deviceInfo{name, d.Manufacturer, d.Model}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, m := range a.meta {
		if info, ok := infos[m.deviceID]; ok {
			m.deviceName, m.manufacturer, m.model = info.name, info.manufacturer, info.model
			a.meta[id] = m
		}
	}
}

func (a *Adapter) setConnected(v bool) {
	a.mu.Lock()
	a.connected = v
	a.mu.Unlock()
}

// --- helpers & wire types ---

func serviceDomain(entity string) string {
	if i := strings.IndexByte(entity, '.'); i > 0 {
		return entity[:i]
	}
	return "homeassistant"
}

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
	EntityID   string `json:"entity_id"`
	State      string `json:"state"`
	Attributes struct {
		FriendlyName string `json:"friendly_name"`
		DeviceClass  string `json:"device_class"`
		Unit         string `json:"unit_of_measurement"`
	} `json:"attributes"`
}
