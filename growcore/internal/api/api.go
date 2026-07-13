// Package api exposes Grow Core over HTTP: a REST surface for configuration and
// discovery, plus a WebSocket that streams the live system snapshot.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/growrig/growrig-platform/growcore/internal/camera"
	"github.com/growrig/growrig-platform/growcore/internal/catalog"
	"github.com/growrig/growrig-platform/growcore/internal/catalogsource"
	"github.com/growrig/growrig-platform/growcore/internal/control"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/integrations"
	"github.com/growrig/growrig-platform/growcore/internal/store"
)

type Server struct {
	store           *store.Store
	engine          *control.Engine
	adapter         control.Adapter
	hub             *Hub
	adapterType     string
	static          http.Handler
	passkeys        *ceremonyStore
	cameras         *camera.Recorder
	preferencesPath string
	integrations    *integrations.Manager
	catalogSources  *catalogsource.Manager
}

func (s *Server) activity(envID, deviceID, level, eventType, message string) {
	_ = s.store.AddActivity(domain.Activity{EnvironmentID: envID, DeviceID: deviceID, Level: level, Type: eventType, Message: message})
}

// growActivity records an activity event scoped to a grow (and optionally the
// environment its plants sit in), so it surfaces on that grow's activity log.
func (s *Server) growActivity(growID, envID, level, eventType, message string) {
	_ = s.store.AddActivity(domain.Activity{GrowID: growID, EnvironmentID: envID, Level: level, Type: eventType, Message: message})
}

func NewServer(st *store.Store, eng *control.Engine, adapter control.Adapter, hub *Hub, adapterType string, static http.Handler, cameras *camera.Recorder, preferencesPath string, integrationManager *integrations.Manager, catalogSources *catalogsource.Manager) *Server {
	return &Server{store: st, engine: eng, adapter: adapter, hub: hub, adapterType: adapterType, static: static, passkeys: newCeremonyStore(), cameras: cameras, preferencesPath: preferencesPath, integrations: integrationManager, catalogSources: catalogSources}
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
	mux.HandleFunc("GET /api/preferences", s.requireAuth(s.getPreferences))
	mux.HandleFunc("GET /api/state", s.requireAuth(s.getState))
	mux.HandleFunc("GET /api/roles", s.requireAuth(s.getRoles))
	mux.HandleFunc("GET /api/stage-presets", s.requireAuth(s.getStagePresets))
	mux.HandleFunc("GET /api/activity", s.requireAuth(s.getActivity))
	mux.HandleFunc("GET /api/environments", s.requireAuth(s.getEnvironments))
	mux.HandleFunc("GET /api/bindings", s.requireAuth(s.getBindings))
	mux.HandleFunc("GET /api/bindings/{id}/camera", s.requireEnvReadForBinding(s.getCameraImage))
	mux.HandleFunc("GET /api/bindings/{id}/camera/live", s.requireEnvReadForBinding(s.getCameraLive))
	mux.HandleFunc("GET /api/bindings/{id}/camera/archive", s.requireEnvReadForBinding(s.getCameraArchive))
	mux.HandleFunc("GET /api/bindings/{id}/camera/archive/{snapshot}", s.requireEnvReadForBinding(s.getCameraArchiveImage))
	mux.HandleFunc("GET /api/bindings/{id}/camera/stats", s.requireEnvReadForBinding(s.getCameraStats))
	mux.HandleFunc("GET /api/lighting/defaults", s.requireAuth(s.getLightingDefaults))
	mux.HandleFunc("GET /api/locations", s.requireAuth(s.getLocations))
	mux.HandleFunc("GET /api/weather", s.requireAuth(s.getWeather))
	mux.HandleFunc("GET /api/grows", s.requireAuth(s.getGrows))
	mux.HandleFunc("GET /api/grows/{id}", s.requireAuth(s.getGrow))
	mux.HandleFunc("GET /api/grows/{id}/ai", s.requireAuth(s.getGrowAIStatus))
	mux.HandleFunc("POST /api/grows/{id}/ai/chat", s.requireAuth(s.chatWithGrowAI))
	mux.HandleFunc("GET /api/ai/status", s.requireAuth(s.getAIStatus))
	mux.HandleFunc("POST /api/ai/chat", s.requireAuth(s.chatWithGrowAI))
	mux.HandleFunc("GET /api/ai/chats", s.requireAuth(s.getAIChats))
	mux.HandleFunc("GET /api/ai/chats/{id}", s.requireAuth(s.getAIChat))
	mux.HandleFunc("PUT /api/ai/chats/{id}", s.requireAuth(s.updateAIChat))
	mux.HandleFunc("GET /api/plants/{id}", s.requireAuth(s.getPlant))
	mux.HandleFunc("GET /api/species", s.requireAuth(s.getSpecies))
	mux.HandleFunc("GET /api/cultivars", s.requireAuth(s.getCultivars))
	mux.HandleFunc("GET /api/cultivars/{id}", s.requireAuth(s.getCultivar))
	mux.HandleFunc("GET /api/cultivars/{id}/image", s.requireAuth(s.getCultivarImage))
	mux.HandleFunc("GET /api/inventory/categories", s.requireAuth(s.getInventoryCategories))
	mux.HandleFunc("GET /api/inventory/products", s.requireAuth(s.getInventoryProducts))
	mux.HandleFunc("GET /api/inventory/products/{category}/{id}/image", s.requireAuth(s.getInventoryProductImage))
	mux.HandleFunc("GET /api/inventory/items", s.requireAuth(s.getInventoryItems))
	mux.HandleFunc("GET /api/inventory/items/{id}", s.requireAuth(s.getInventoryItem))
	mux.HandleFunc("GET /api/inventory/items/{id}/image", s.requireAuth(s.getInventoryItemImage))
	mux.HandleFunc("GET /api/recipes", s.requireAuth(s.getRecipes))
	// Built-in recipe templates, offered only to seed a new user recipe.
	mux.HandleFunc("GET /api/recipe-templates", s.requireAuth(s.getRecipeTemplates))
	mux.HandleFunc("GET /api/recipes/{id}", s.requireAuth(s.getRecipe))

	// Per-environment read.
	mux.HandleFunc("GET /api/environments/{id}/history", s.requireEnvRead(s.getHistory))
	mux.HandleFunc("GET /api/environments/{id}/device-history", s.requireEnvRead(s.getDeviceHistory))
	mux.HandleFunc("GET /api/environments/{id}/sensor-history", s.requireEnvRead(s.getSensorHistory))
	mux.HandleFunc("GET /api/environments/{id}/weather-history", s.requireEnvRead(s.getWeatherHistory))
	mux.HandleFunc("GET /api/environments/{id}/schedule", s.requireEnvRead(s.getSchedule))
	mux.HandleFunc("GET /api/environments/{id}/plants", s.requireEnvRead(s.getEnvironmentPlants))

	// Per-environment write (operate the grow).
	mux.HandleFunc("PUT /api/environments/{id}/targets", s.requireEnvWrite(s.putTargets))
	mux.HandleFunc("PUT /api/environments/{id}/schedule", s.requireEnvWrite(s.putSchedule))
	mux.HandleFunc("PUT /api/environments/{id}/control-grow", s.requireEnvWrite(s.putControlGrow))
	mux.HandleFunc("PUT /api/bindings/{id}/switch", s.requireEnvWriteForBinding(s.putSwitch))

	// Grows & plants (admin-managed cultivation layer).
	mux.HandleFunc("POST /api/grows", s.requireAdmin(s.createGrow))
	mux.HandleFunc("PUT /api/grows/{id}", s.requireAdmin(s.updateGrow))
	mux.HandleFunc("DELETE /api/grows/{id}", s.requireAdmin(s.deleteGrow))
	mux.HandleFunc("POST /api/grows/{id}/stage", s.requireAdmin(s.changeStage))
	mux.HandleFunc("POST /api/grows/{id}/complete", s.requireAdmin(s.completeGrow))
	mux.HandleFunc("POST /api/grows/{id}/plants", s.requireAdmin(s.createPlants))
	mux.HandleFunc("GET /api/grows/{id}/care", s.requireAuth(s.getCare))
	mux.HandleFunc("POST /api/grows/{id}/care", s.requireAdmin(s.logCare))
	mux.HandleFunc("GET /api/grows/{id}/care-config", s.requireAuth(s.getCareConfig))
	mux.HandleFunc("PUT /api/grows/{id}/care-config", s.requireAdmin(s.putCareConfig))
	mux.HandleFunc("DELETE /api/care/{id}", s.requireAdmin(s.deleteCare))
	mux.HandleFunc("PUT /api/plants/{id}", s.requireAdmin(s.updatePlant))
	mux.HandleFunc("POST /api/plants/{id}/move", s.requireAdmin(s.movePlant))
	mux.HandleFunc("POST /api/plants/{id}/repot", s.requireAdmin(s.repotPlant))
	mux.HandleFunc("POST /api/plants/{id}/harvest", s.requireAdmin(s.setPlantStatus(domain.PlantHarvested, "Harvested")))
	mux.HandleFunc("POST /api/plants/{id}/remove", s.requireAdmin(s.setPlantStatus(domain.PlantRemoved, "Removed")))
	mux.HandleFunc("POST /api/cultivars", s.requireAdmin(s.createCultivar))
	mux.HandleFunc("PUT /api/cultivars/{id}", s.requireAdmin(s.updateCultivar))
	mux.HandleFunc("DELETE /api/cultivars/{id}", s.requireAdmin(s.deleteCultivar))
	mux.HandleFunc("POST /api/inventory/items", s.requireAdmin(s.createInventoryItem))
	mux.HandleFunc("PUT /api/inventory/items/{id}", s.requireAdmin(s.updateInventoryItem))
	mux.HandleFunc("DELETE /api/inventory/items/{id}", s.requireAdmin(s.deleteInventoryItem))
	mux.HandleFunc("POST /api/recipes", s.requireAdmin(s.createRecipe))
	mux.HandleFunc("PUT /api/recipes/{id}", s.requireAdmin(s.updateRecipe))
	mux.HandleFunc("DELETE /api/recipes/{id}", s.requireAdmin(s.deleteRecipe))

	// Admin only (configuration & user management).
	mux.HandleFunc("GET /api/catalog", s.requireAdmin(s.getCatalog))
	mux.HandleFunc("GET /api/catalog/assets/{category}/{device}/{name}", s.requireAdmin(s.getCatalogAsset))
	mux.HandleFunc("GET /api/vendors", s.requireAdmin(s.getVendors))
	mux.HandleFunc("GET /api/vendors/{vendor}/{name}", s.requireAdmin(s.getVendorAsset))
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
	mux.HandleFunc("PUT /api/preferences", s.requireAdmin(s.putPreferences))
	mux.HandleFunc("POST /api/admin/restart", s.requireAdmin(s.restart))
	mux.HandleFunc("GET /api/catalog-sources", s.requireAdmin(s.getCatalogSources))
	mux.HandleFunc("POST /api/catalog-sources", s.requireAdmin(s.createCatalogSource))
	mux.HandleFunc("POST /api/catalog-sources/{id}/refresh", s.requireAdmin(s.refreshCatalogSource))
	mux.HandleFunc("DELETE /api/catalog-sources/{id}", s.requireAdmin(s.deleteCatalogSource))
	mux.HandleFunc("GET /api/admin/database/tables", s.requireAdmin(s.getDatabaseTables))
	mux.HandleFunc("DELETE /api/activity", s.requireAdmin(s.clearActivity))
	mux.HandleFunc("GET /api/admin/homeassistant", s.requireAdmin(s.getHomeAssistant))
	mux.HandleFunc("POST /api/admin/homeassistant/reload", s.requireAdmin(s.reloadHomeAssistant))
	mux.HandleFunc("POST /api/admin/homeassistant/update", s.requireAdmin(s.updateHomeAssistant))
	mux.HandleFunc("GET /api/integration-bundles", s.requireAdmin(s.getIntegrationBundles))
	mux.HandleFunc("GET /api/integration-bundles/{id}/icon", s.requireAdmin(s.getIntegrationIcon))
	mux.HandleFunc("GET /api/integration-instances", s.requireAdmin(s.getIntegrationInstances))
	mux.HandleFunc("POST /api/integration-instances", s.requireAdmin(s.createIntegrationInstance))
	mux.HandleFunc("PUT /api/integration-instances/{id}", s.requireAdmin(s.updateIntegrationInstance))
	mux.HandleFunc("DELETE /api/integration-instances/{id}", s.requireAdmin(s.deleteIntegrationInstance))
	mux.HandleFunc("POST /api/integration-instances/{id}/test", s.requireAdmin(s.testIntegrationInstance))
	mux.HandleFunc("POST /api/integration-instances/{id}/invoke/{capability}", s.requireAuth(s.invokeIntegration))
	mux.HandleFunc("GET /api/integration-bindings", s.requireAdmin(s.getIntegrationBindings))
	mux.HandleFunc("POST /api/integration-bindings", s.requireAdmin(s.saveIntegrationBinding))
	mux.HandleFunc("DELETE /api/integration-bindings/{id}", s.requireAdmin(s.deleteIntegrationBinding))
	mux.HandleFunc("GET /api/integration-bindings/resolve", s.requireAuth(s.resolveIntegrationBinding))

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

func (s *Server) getVendors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Vendors())
}

func (s *Server) getCatalogAsset(w http.ResponseWriter, r *http.Request) {
	raw, err := catalog.DeviceAsset(r.PathValue("category"), r.PathValue("device"), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/"+imageSubtype(r.PathValue("name")))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(raw)
}

func (s *Server) getVendorAsset(w http.ResponseWriter, r *http.Request) {
	raw, err := catalog.VendorAsset(r.PathValue("vendor"), r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/"+imageSubtype(r.PathValue("name")))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(raw)
}

func imageSubtype(name string) string {
	if len(name) >= 4 && name[len(name)-4:] == ".svg" {
		return "svg+xml"
	}
	if len(name) >= 5 && name[len(name)-5:] == ".webp" {
		return "webp"
	}
	if len(name) >= 4 && name[len(name)-4:] == ".png" {
		return "png"
	}
	return "jpeg"
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
	offset := 0
	if value := r.URL.Query().Get("offset"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			offset = parsed
		}
	}
	envParam := r.URL.Query().Get("environmentId")
	growParam := r.URL.Query().Get("growId")
	var levels []string
	if lv := strings.TrimSpace(r.URL.Query().Get("levels")); lv != "" {
		for _, l := range strings.Split(lv, ",") {
			if l = strings.TrimSpace(l); l != "" {
				levels = append(levels, l)
			}
		}
	}
	var types []string
	if tp := strings.TrimSpace(r.URL.Query().Get("types")); tp != "" {
		for _, t := range strings.Split(tp, ",") {
			if t = strings.TrimSpace(t); t != "" {
				types = append(types, t)
			}
		}
	}
	u, _ := currentUser(r)
	allowed, all := s.accessibleEnvIDs(u)
	// A non-admin asking for a specific environment must be able to see it.
	if !all && envParam != "" && !allowed[envParam] {
		writeJSON(w, http.StatusForbidden, errBody("you do not have access to this environment"))
		return
	}

	// Without an environment or grow filter, non-admins only see events for the
	// environments they can access (env-less config events stay admin-only). A
	// grow filter is not environment-scoped, so it isn't narrowed here. Because
	// this filtering is post-query, paginate the accessible subset in memory.
	if !all && envParam == "" && growParam == "" {
		batch, err := s.store.Activities(envParam, growParam, levels, types, 500, 0)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		filtered := make([]domain.Activity, 0, len(batch))
		for _, e := range batch {
			if e.EnvironmentID != "" && allowed[e.EnvironmentID] {
				filtered = append(filtered, e)
			}
		}
		writeJSON(w, http.StatusOK, paginate(filtered, offset, limit))
		return
	}

	total, err := s.store.CountActivities(envParam, growParam, levels, types)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	events, err := s.store.Activities(envParam, growParam, levels, types, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if events == nil {
		events = []domain.Activity{}
	}
	writeJSON(w, http.StatusOK, activityPage{Items: events, Total: total})
}

// activityPage is a page of activity plus the total matching count, so clients
// can render pagination.
type activityPage struct {
	Items []domain.Activity `json:"items"`
	Total int               `json:"total"`
}

// paginate slices an already-filtered, in-memory activity list into a page.
func paginate(items []domain.Activity, offset, limit int) activityPage {
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := items[offset:end]
	if page == nil {
		page = []domain.Activity{}
	}
	return activityPage{Items: page, Total: total}
}

func (s *Server) getRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, domain.AllFanRoles)
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

type cameraImageAdapter interface {
	CameraImage(context.Context, string) ([]byte, string, error)
}

func (s *Server) getCameraImage(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.store.Bindings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var camera domain.Binding
	for _, binding := range bindings {
		if binding.ID == r.PathValue("id") && binding.Kind == domain.KindCamera {
			camera = binding
			break
		}
	}
	if camera.ID == "" {
		writeJSON(w, http.StatusNotFound, errBody("camera binding not found"))
		return
	}
	if camera.CameraType == domain.CameraRTSP && camera.StreamURL != "" {
		image, err := os.ReadFile(s.cameras.Latest(camera.EnvironmentID, camera.ID))
		if err != nil {
			log.Printf("camera %s: latest snapshot unavailable: %v", camera.ID, err)
			writeJSON(w, http.StatusServiceUnavailable, errBody("camera is connecting; no snapshot is available yet"))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(image)
		return
	}
	if camera.Entity == "" {
		log.Printf("camera %s: proxy request has no RTSP source or Home Assistant entity (type=%q stream=%t)", camera.ID, camera.CameraType, camera.StreamURL != "")
		writeJSON(w, http.StatusBadRequest, errBody("camera does not use a proxied source"))
		return
	}
	adapter, ok := s.adapter.(cameraImageAdapter)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errBody("camera proxy is unavailable"))
		return
	}
	image, contentType, err := adapter.CameraImage(r.Context(), camera.Entity)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(image)
}

func (s *Server) getCameraLive(w http.ResponseWriter, r *http.Request) {
	bindings, err := s.store.Bindings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var camera domain.Binding
	for _, binding := range bindings {
		if binding.ID == r.PathValue("id") && binding.Kind == domain.KindCamera {
			camera = binding
			break
		}
	}
	if camera.ID == "" {
		writeJSON(w, http.StatusNotFound, errBody("camera binding not found"))
		return
	}
	if camera.CameraType != domain.CameraRTSP {
		log.Printf("camera %s: rejected live request because camera type is %q", camera.ID, camera.CameraType)
		writeJSON(w, http.StatusBadRequest, errBody("live view is only available for RTSP cameras"))
		return
	}
	frames, unsubscribe := s.cameras.Subscribe(camera.ID)
	defer unsubscribe()
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=growrigframe")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case frame := <-frames:
			if _, err := fmt.Fprintf(w, "--growrigframe\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (s *Server) getCameraArchive(w http.ResponseWriter, r *http.Request) {
	binding, ok := s.cameraBinding(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("camera binding not found"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	snapshots, err := s.cameras.Snapshots(binding.EnvironmentID, binding.ID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if snapshots == nil {
		snapshots = []camera.Snapshot{}
	}
	writeJSON(w, http.StatusOK, snapshots)
}

func (s *Server) getCameraArchiveImage(w http.ResponseWriter, r *http.Request) {
	binding, ok := s.cameraBinding(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("camera binding not found"))
		return
	}
	path, err := s.cameras.SnapshotPath(binding.EnvironmentID, binding.ID, r.PathValue("snapshot"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody("snapshot not found"))
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func (s *Server) getCameraStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.cameraBinding(r.PathValue("id")); !ok {
		writeJSON(w, http.StatusNotFound, errBody("camera binding not found"))
		return
	}
	writeJSON(w, http.StatusOK, s.cameras.StreamStats(r.PathValue("id")))
}

func (s *Server) cameraBinding(id string) (domain.Binding, bool) {
	bindings, err := s.store.Bindings()
	if err != nil {
		return domain.Binding{}, false
	}
	for _, binding := range bindings {
		if binding.ID == id && binding.Kind == domain.KindCamera {
			return binding, true
		}
	}
	return domain.Binding{}, false
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
