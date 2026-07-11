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
}

func (s *Server) activity(envID, deviceID, level, eventType, message string) {
	_ = s.store.AddActivity(domain.Activity{EnvironmentID: envID, DeviceID: deviceID, Level: level, Type: eventType, Message: message})
}

func NewServer(st *store.Store, eng *control.Engine, adapter control.Adapter, hub *Hub, adapterType string, static http.Handler) *Server {
	return &Server{store: st, engine: eng, adapter: adapter, hub: hub, adapterType: adapterType, static: static}
}

// Handler builds the HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.getInfo)
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("GET /api/roles", s.getRoles)
	mux.HandleFunc("GET /api/phases", s.getPhases)
	mux.HandleFunc("GET /api/catalog", s.getCatalog)
	mux.HandleFunc("GET /api/discovery", s.getDiscovery)
	mux.HandleFunc("POST /api/demo", s.postDemo)
	mux.HandleFunc("GET /api/activity", s.getActivity)

	mux.HandleFunc("GET /api/environments", s.getEnvironments)
	mux.HandleFunc("POST /api/environments", s.createEnvironment)
	mux.HandleFunc("PUT /api/environments/{id}", s.updateEnvironment)
	mux.HandleFunc("DELETE /api/environments/{id}", s.deleteEnvironment)
	mux.HandleFunc("GET /api/environments/{id}/config", s.getEnvironmentConfig)
	mux.HandleFunc("PUT /api/environments/{id}/config", s.putEnvironmentConfig)
	mux.HandleFunc("PUT /api/environments/{id}/targets", s.putTargets)
	mux.HandleFunc("GET /api/environments/{id}/history", s.getHistory)
	mux.HandleFunc("GET /api/environments/{id}/device-history", s.getDeviceHistory)
	mux.HandleFunc("GET /api/environments/{id}/sensor-history", s.getSensorHistory)
	mux.HandleFunc("GET /api/environments/{id}/weather-history", s.getWeatherHistory)
	mux.HandleFunc("PUT /api/environments/{id}/cycle", s.putCycle)
	mux.HandleFunc("DELETE /api/environments/{id}/cycle", s.deleteCycle)
	mux.HandleFunc("GET /api/environments/{id}/schedule", s.getSchedule)
	mux.HandleFunc("PUT /api/environments/{id}/schedule", s.putSchedule)
	mux.HandleFunc("GET /api/lighting/defaults", s.getLightingDefaults)

	mux.HandleFunc("GET /api/locations", s.getLocations)
	mux.HandleFunc("POST /api/locations", s.createLocation)
	mux.HandleFunc("PUT /api/locations/{id}", s.updateLocation)
	mux.HandleFunc("DELETE /api/locations/{id}", s.deleteLocation)
	mux.HandleFunc("GET /api/geocode", s.geocode)
	mux.HandleFunc("GET /api/weather", s.getWeather)

	mux.HandleFunc("GET /api/bindings", s.getBindings)
	mux.HandleFunc("POST /api/bindings", s.createBinding)
	mux.HandleFunc("PUT /api/bindings/{id}", s.updateBinding)
	mux.HandleFunc("DELETE /api/bindings/{id}", s.deleteBinding)
	mux.HandleFunc("PUT /api/bindings/{id}/switch", s.putSwitch)

	mux.HandleFunc("GET /api/ws", s.ws)

	if s.static != nil {
		mux.Handle("/", s.static)
	}
	return withCORS(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"adapter": s.adapterType})
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Latest())
}

func (s *Server) getActivity(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	events, err := s.store.Activities(r.URL.Query().Get("environmentId"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
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
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	s.hub.serveWS(c, s.engine.Latest())
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
