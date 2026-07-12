// Package api exposes Grow Core over HTTP: a REST surface for configuration and
// discovery, plus a WebSocket that streams the live system snapshot.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/growrig/growrig-platform/growcore/internal/catalog"
	"github.com/growrig/growrig-platform/growcore/internal/control"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/store"
)

type Server struct {
	store       *store.Store
	engine      *control.Engine
	adapter     control.Adapter
	hub         *Hub
	adapterType string
	static      http.Handler
	passkeys    *ceremonyStore
}

func (s *Server) activity(envID, deviceID, level, eventType, message string) {
	_ = s.store.AddActivity(domain.Activity{EnvironmentID: envID, DeviceID: deviceID, Level: level, Type: eventType, Message: message})
}

func NewServer(st *store.Store, eng *control.Engine, adapter control.Adapter, hub *Hub, adapterType string, static http.Handler) *Server {
	return &Server{store: st, engine: eng, adapter: adapter, hub: hub, adapterType: adapterType, static: static, passkeys: newCeremonyStore()}
}

// Handler builds the HTTP router.
//
// Access control: withAuth resolves the caller into the request context; each
// protected route is wrapped by a require* guard. Public routes (health and the
// unauthenticated auth endpoints) are registered raw so first-run setup and
// login work before anyone is signed in.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth/status", s.getAuthStatus)
	mux.HandleFunc("POST /api/auth/bootstrap", s.bootstrap)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/passkey/login/begin", s.passkeyLoginBegin)
	mux.HandleFunc("POST /api/auth/passkey/login/finish", s.passkeyLoginFinish)

	// Authenticated (any signed-in user); list responses are filtered per-user.
	mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.logout))
	mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.me))
	mux.HandleFunc("POST /api/auth/passkey/register/begin", s.requireAuth(s.passkeyRegisterBegin))
	mux.HandleFunc("POST /api/auth/passkey/register/finish", s.requireAuth(s.passkeyRegisterFinish))
	mux.HandleFunc("GET /api/auth/passkeys", s.requireAuth(s.listPasskeys))
	mux.HandleFunc("DELETE /api/auth/passkeys/{id}", s.requireAuth(s.deletePasskey))
	mux.HandleFunc("GET /api/info", s.requireAuth(s.getInfo))
	mux.HandleFunc("GET /api/state", s.requireAuth(s.getState))
	mux.HandleFunc("GET /api/roles", s.requireAuth(s.getRoles))
	mux.HandleFunc("GET /api/phases", s.requireAuth(s.getPhases))
	mux.HandleFunc("GET /api/activity", s.requireAuth(s.getActivity))
	mux.HandleFunc("GET /api/environments", s.requireAuth(s.getEnvironments))
	mux.HandleFunc("GET /api/bindings", s.requireAuth(s.getBindings))
	mux.HandleFunc("GET /api/lighting/defaults", s.requireAuth(s.getLightingDefaults))
	mux.HandleFunc("GET /api/locations", s.requireAuth(s.getLocations))
	mux.HandleFunc("GET /api/weather", s.requireAuth(s.getWeather))

	// Per-environment read.
	mux.HandleFunc("GET /api/environments/{id}/history", s.requireEnvRead(s.getHistory))
	mux.HandleFunc("GET /api/environments/{id}/device-history", s.requireEnvRead(s.getDeviceHistory))
	mux.HandleFunc("GET /api/environments/{id}/sensor-history", s.requireEnvRead(s.getSensorHistory))
	mux.HandleFunc("GET /api/environments/{id}/weather-history", s.requireEnvRead(s.getWeatherHistory))
	mux.HandleFunc("GET /api/environments/{id}/schedule", s.requireEnvRead(s.getSchedule))

	// Per-environment write (operate the grow).
	mux.HandleFunc("PUT /api/environments/{id}/targets", s.requireEnvWrite(s.putTargets))
	mux.HandleFunc("PUT /api/environments/{id}/cycle", s.requireEnvWrite(s.putCycle))
	mux.HandleFunc("DELETE /api/environments/{id}/cycle", s.requireEnvWrite(s.deleteCycle))
	mux.HandleFunc("PUT /api/environments/{id}/schedule", s.requireEnvWrite(s.putSchedule))
	mux.HandleFunc("PUT /api/bindings/{id}/switch", s.requireEnvWriteForBinding(s.putSwitch))

	// Admin only (configuration & user management).
	mux.HandleFunc("GET /api/catalog", s.requireAdmin(s.getCatalog))
	mux.HandleFunc("GET /api/discovery", s.requireAdmin(s.getDiscovery))
	mux.HandleFunc("POST /api/demo", s.requireAdmin(s.postDemo))
	mux.HandleFunc("GET /api/geocode", s.requireAdmin(s.geocode))
	mux.HandleFunc("POST /api/environments", s.requireAdmin(s.createEnvironment))
	mux.HandleFunc("PUT /api/environments/{id}", s.requireAdmin(s.updateEnvironment))
	mux.HandleFunc("DELETE /api/environments/{id}", s.requireAdmin(s.deleteEnvironment))
	mux.HandleFunc("GET /api/environments/{id}/config", s.requireAdmin(s.getEnvironmentConfig))
	mux.HandleFunc("PUT /api/environments/{id}/config", s.requireAdmin(s.putEnvironmentConfig))
	mux.HandleFunc("POST /api/locations", s.requireAdmin(s.createLocation))
	mux.HandleFunc("PUT /api/locations/{id}", s.requireAdmin(s.updateLocation))
	mux.HandleFunc("DELETE /api/locations/{id}", s.requireAdmin(s.deleteLocation))
	mux.HandleFunc("POST /api/bindings", s.requireAdmin(s.createBinding))
	mux.HandleFunc("PUT /api/bindings/{id}", s.requireAdmin(s.updateBinding))
	mux.HandleFunc("DELETE /api/bindings/{id}", s.requireAdmin(s.deleteBinding))
	mux.HandleFunc("GET /api/users", s.requireAdmin(s.getUsers))
	mux.HandleFunc("POST /api/users", s.requireAdmin(s.createUser))
	mux.HandleFunc("PUT /api/users/{id}", s.requireAdmin(s.updateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.requireAdmin(s.deleteUser))
	mux.HandleFunc("GET /api/settings/signup", s.requireAdmin(s.getSignupSetting))
	mux.HandleFunc("PUT /api/settings/signup", s.requireAdmin(s.setSignupSetting))
	mux.HandleFunc("GET /api/admin/homeassistant", s.requireAdmin(s.getHomeAssistant))
	mux.HandleFunc("POST /api/admin/homeassistant/reload", s.requireAdmin(s.reloadHomeAssistant))
	mux.HandleFunc("POST /api/admin/homeassistant/update", s.requireAdmin(s.updateHomeAssistant))

	// The WebSocket authenticates from a ?token= query param (browsers cannot
	// set headers on a WebSocket handshake).
	mux.HandleFunc("GET /api/ws", s.ws)

	if s.static != nil {
		mux.Handle("/", s.static)
	}
	return withCORS(s.withAuth(mux))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"adapter": s.adapterType})
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	u, _ := currentUser(r)
	allowed, all := s.accessibleEnvIDs(u)
	writeJSON(w, http.StatusOK, filterSnapshot(s.engine.Latest(), allowed, all))
}

func (s *Server) getActivity(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	envParam := r.URL.Query().Get("environmentId")
	u, _ := currentUser(r)
	allowed, all := s.accessibleEnvIDs(u)
	// A non-admin asking for a specific environment must be able to see it.
	if !all && envParam != "" && !allowed[envParam] {
		writeJSON(w, http.StatusForbidden, errBody("you do not have access to this environment"))
		return
	}
	events, err := s.store.Activities(envParam, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Without an environment filter, non-admins only see events for the
	// environments they can access (env-less config events stay admin-only).
	if !all && envParam == "" {
		filtered := make([]domain.Activity, 0, len(events))
		for _, e := range events {
			if e.EnvironmentID != "" && allowed[e.EnvironmentID] {
				filtered = append(filtered, e)
			}
		}
		events = filtered
	}
	if events == nil {
		events = []domain.Activity{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) getRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, domain.AllFanRoles)
}

func (s *Server) getPhases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, domain.AllPhases)
}

func (s *Server) getCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Products())
}

func (s *Server) getDiscovery(w http.ResponseWriter, r *http.Request) {
	found := s.adapter.Discover()
	if found == nil {
		found = []control.DiscoveredEntity{}
	}
	writeJSON(w, http.StatusOK, found)
}

func (s *Server) getEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	u, _ := currentUser(r)
	if allowed, all := s.accessibleEnvIDs(u); !all {
		filtered := make([]domain.Environment, 0, len(envs))
		for _, e := range envs {
			if allowed[e.ID] {
				filtered = append(filtered, e)
			}
		}
		envs = filtered
	}
	if envs == nil {
		envs = []domain.Environment{}
	}
	writeJSON(w, http.StatusOK, envs)
}

func (s *Server) putTargets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetTempC    float64 `json:"targetTempC"`
		TargetHumidity float64 `json:"targetHumidity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.TargetTempC < 5 || body.TargetTempC > 45 {
		writeJSON(w, http.StatusBadRequest, errBody("targetTempC must be between 5 and 45"))
		return
	}
	if body.TargetHumidity < 10 || body.TargetHumidity > 95 {
		writeJSON(w, http.StatusBadRequest, errBody("targetHumidity must be between 10 and 95"))
		return
	}
	if err := s.store.UpdateTargets(r.PathValue("id"), body.TargetTempC, body.TargetHumidity); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	var readings []domain.Reading
	var err error
	// ?hours=N returns a downsampled window (for the timeline); otherwise the
	// legacy ?limit=N most-recent readings (for sparklines).
	if v := q.Get("hours"); v != "" {
		hours := 72.0
		if n, e := strconv.ParseFloat(v, 64); e == nil && n > 0 && n <= 24*30 {
			hours = n
		}
		buckets := 500
		if n, e := strconv.Atoi(q.Get("buckets")); e == nil && n > 0 && n <= 2000 {
			buckets = n
		}
		since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
		readings, err = s.store.ReadingsSince(id, since, buckets)
	} else {
		limit := 120
		if v := q.Get("limit"); v != "" {
			if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 5000 {
				limit = n
			}
		}
		readings, err = s.store.RecentReadings(id, limit)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if readings == nil {
		readings = []domain.Reading{}
	}
	writeJSON(w, http.StatusOK, readings)
}

// getDeviceHistory returns downsampled per-device series (fan rpm, light power)
// over the last ?hours, for the timeline's optional per-device lines.
func (s *Server) getDeviceHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours := 72.0
	if n, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && n > 0 && n <= 24*30 {
		hours = n
	}
	buckets := 500
	if n, e := strconv.Atoi(q.Get("buckets")); e == nil && n > 0 && n <= 2000 {
		buckets = n
	}
	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	series, err := s.store.DeviceReadingsSince(r.PathValue("id"), since, buckets)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if series == nil {
		series = []domain.DeviceSeries{}
	}
	writeJSON(w, http.StatusOK, series)
}

// getSensorHistory returns downsampled per-sensor series (each bound sensor's
// own readings) over the last ?hours, for the metric-detail modal.
func (s *Server) getSensorHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours := 72.0
	if n, e := strconv.ParseFloat(q.Get("hours"), 64); e == nil && n > 0 && n <= 24*30 {
		hours = n
	}
	buckets := 500
	if n, e := strconv.Atoi(q.Get("buckets")); e == nil && n > 0 && n <= 2000 {
		buckets = n
	}
	since := time.Now().Add(-time.Duration(hours * float64(time.Hour)))
	series, err := s.store.SensorReadingsSince(r.PathValue("id"), since, buckets)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if series == nil {
		series = []domain.SensorSeries{}
	}
	writeJSON(w, http.StatusOK, series)
}

func (s *Server) getBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.store.Bindings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	u, _ := currentUser(r)
	if allowed, all := s.accessibleEnvIDs(u); !all {
		filtered := make([]domain.Binding, 0, len(bindings))
		for _, b := range bindings {
			if allowed[b.EnvironmentID] {
				filtered = append(filtered, b)
			}
		}
		bindings = filtered
	}
	if bindings == nil {
		bindings = []domain.Binding{}
	}
	writeJSON(w, http.StatusOK, bindings)
}

func (s *Server) putSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	bindings, err := s.store.Bindings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	id := r.PathValue("id")
	for _, b := range bindings {
		if b.ID == id {
			entity := b.Entity
			if b.Kind == domain.KindLight {
				entity = ""
				for _, candidate := range bindings {
					if candidate.DeviceID == b.PowerControllerID && candidate.Kind == domain.KindPower {
						entity = candidate.Entity
						break
					}
				}
			}
			if entity == "" {
				writeJSON(w, http.StatusConflict, errBody("no power controller assigned"))
				return
			}
			if err := s.adapter.SetSwitch(entity, body.On); err != nil {
				_ = s.store.AddActivity(domain.Activity{EnvironmentID: b.EnvironmentID, DeviceID: b.DeviceID, Level: "error", Type: "control", Message: "Failed to switch " + b.Name})
				writeErr(w, http.StatusBadGateway, err)
				return
			}
			state := "off"
			if body.On {
				state = "on"
			}
			_ = s.store.AddActivity(domain.Activity{EnvironmentID: b.EnvironmentID, DeviceID: b.DeviceID, Level: "info", Type: "control", Message: "Manually switched " + b.Name + " " + state})
			// A hand toggle of the scheduled primary light holds until the next
			// scheduled transition, so the schedule doesn't immediately revert it.
			if b.Kind == domain.KindLight && b.Primary {
				s.engine.NoteManualLightSwitch(b.EnvironmentID)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, errBody("binding not found"))
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	// The WebSocket authenticates from ?token= (set by the client) since the
	// browser cannot attach an Authorization header to the handshake.
	u := s.userFromToken(bearerToken(r))
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, errBody("authentication required"))
		return
	}
	allowed, all := s.accessibleEnvIDs(u)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	s.hub.serveWS(c, filterSnapshot(s.engine.Latest(), allowed, all), allowed, all)
}

func validRole(role domain.Role) bool {
	for _, r := range domain.AllFanRoles {
		if r == role {
			return true
		}
	}
	return false
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errBody(err.Error()))
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
