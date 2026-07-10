// Package api exposes Grow Core over HTTP: a REST surface for configuration
// plus a WebSocket that streams the live system snapshot.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/coder/websocket"

	"github.com/growrig/growrig-platform/growcore/internal/control"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/store"
)

type Server struct {
	store   *store.Store
	engine  *control.Engine
	hub     *Hub
	adapter string       // adapter type, for /api/info
	static  http.Handler // optional embedded web UI, served at "/"
}

func NewServer(st *store.Store, eng *control.Engine, hub *Hub, adapter string, static http.Handler) *Server {
	return &Server{store: st, engine: eng, hub: hub, adapter: adapter, static: static}
}

// Handler builds the HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/info", s.getInfo)
	mux.HandleFunc("GET /api/state", s.getState)
	mux.HandleFunc("GET /api/roles", s.getRoles)

	mux.HandleFunc("GET /api/environments", s.getEnvironments)
	mux.HandleFunc("POST /api/environments", s.createEnvironment)
	mux.HandleFunc("PUT /api/environments/{id}", s.updateEnvironment)
	mux.HandleFunc("DELETE /api/environments/{id}", s.deleteEnvironment)
	mux.HandleFunc("PUT /api/environments/{id}/targets", s.putTargets)
	mux.HandleFunc("GET /api/environments/{id}/history", s.getHistory)

	mux.HandleFunc("GET /api/devices", s.getDevices)
	mux.HandleFunc("POST /api/devices", s.createDevice)
	mux.HandleFunc("PUT /api/devices/{id}", s.updateDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", s.deleteDevice)
	mux.HandleFunc("PUT /api/devices/{id}/channels/{ch}/role", s.putChannelRole)

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
	writeJSON(w, http.StatusOK, map[string]string{"adapter": s.adapter})
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.engine.Latest())
}

func (s *Server) getRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, domain.AllFanRoles)
}

func (s *Server) getEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targetTempC must be between 5 and 45"})
		return
	}
	if body.TargetHumidity < 10 || body.TargetHumidity > 95 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "targetHumidity must be between 10 and 95"})
		return
	}
	if err := s.store.UpdateTargets(r.PathValue("id"), body.TargetTempC, body.TargetHumidity); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	limit := 120
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}
	readings, err := s.store.RecentReadings(r.PathValue("id"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if readings == nil {
		readings = []domain.Reading{}
	}
	writeJSON(w, http.StatusOK, readings)
}

func (s *Server) getDevices(w http.ResponseWriter, r *http.Request) {
	// Serve devices from the store so configuration changes are reflected
	// immediately (live temp/RPM/health come via the WebSocket snapshot).
	devs, err := s.store.Devices()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if devs == nil {
		devs = []domain.Device{}
	}
	writeJSON(w, http.StatusOK, devs)
}

func (s *Server) putChannelRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role domain.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !validRole(body.Role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown role"})
		return
	}
	if err := s.store.UpdateChannelRole(r.PathValue("id"), r.PathValue("ch"), body.Role); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Dev convenience: the SvelteKit dev server is a different origin.
		InsecureSkipVerify: true,
	})
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
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
