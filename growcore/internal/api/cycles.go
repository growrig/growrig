package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/sim"
)

// postDemo seeds the simulator's demo tent + lung room into an empty database,
// so users can explore the app instantly. Only valid in simulator mode.
func (s *Server) postDemo(w http.ResponseWriter, r *http.Request) {
	if s.adapterType != "simulator" {
		writeJSON(w, http.StatusConflict, errBody("demo data is only available with the simulator adapter"))
		return
	}
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(envs) > 0 {
		writeJSON(w, http.StatusConflict, errBody("environments already exist"))
		return
	}
	demoEnvs, bindings := sim.SeedTopology()
	for _, env := range demoEnvs {
		if err := s.store.SaveEnvironment(env); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	for _, b := range bindings {
		if err := s.store.SaveBinding(b); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	// A sample grow with a few plants placed in the tent, nominated as its
	// control grow, so the demo shows the full cultivation picture.
	grow := domain.Grow{
		ID: id("Demo Kush", "grow"), Name: "Demo Kush", Species: "cannabis",
		Stage: "vegetative", Stages: domain.StagePresets["cannabis"],
		StartedAt: time.Now().AddDate(0, 0, -21), StageStarted: time.Now().AddDate(0, 0, -7),
		Status: domain.GrowActive,
	}
	if err := s.store.SaveGrow(grow); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := s.store.BulkCreatePlants(grow.ID, 4, domain.TrackIndividual, 1, "Plant", "Demo Kush", sim.TentID, grow.StartedAt); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if env, ok := findEnvByID(demoEnvs, sim.TentID); ok {
		env.ControlGrowID = grow.ID
		_ = s.store.SaveEnvironment(env)
	}
	w.WriteHeader(http.StatusCreated)
}

func findEnvByID(envs []domain.Environment, id string) (domain.Environment, bool) {
	for _, e := range envs {
		if e.ID == id {
			return e, true
		}
	}
	return domain.Environment{}, false
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	sched, _, err := s.store.LightSchedule(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, sched)
}

type scheduleBody struct {
	Mode         domain.LightScheduleMode `json:"mode"`
	LightsOnAt   string                   `json:"lightsOnAt"`
	Anchor       domain.LightAnchor       `json:"anchor"`
	OnHours      float64                  `json:"onHours"`
	StageOnHours map[string]float64       `json:"stageOnHours"`
}

func (s *Server) putSchedule(w http.ResponseWriter, r *http.Request) {
	var b scheduleBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !validScheduleMode(b.Mode) {
		writeJSON(w, http.StatusBadRequest, errBody("unknown schedule mode"))
		return
	}
	if b.LightsOnAt != "" && !validHHMM(b.LightsOnAt) {
		writeJSON(w, http.StatusBadRequest, errBody("lightsOnAt must be HH:MM"))
		return
	}
	if b.Anchor == "" {
		b.Anchor = domain.AnchorCenter
	}
	if !validAnchor(b.Anchor) {
		writeJSON(w, http.StatusBadRequest, errBody("anchor must be center, start or end"))
		return
	}
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	id := r.PathValue("id")
	if !containsEnv(envs, id) {
		writeJSON(w, http.StatusNotFound, errBody("environment not found"))
		return
	}
	stageOn := map[string]float64{}
	for stage, h := range b.StageOnHours {
		if strings.TrimSpace(stage) != "" {
			stageOn[stage] = h
		}
	}
	onAt := strings.TrimSpace(b.LightsOnAt)
	if onAt == "" {
		// An unset anchor time means "the usual": a day centred on 15:00, or
		// the equivalent 06:00 lights-on under a start anchor.
		onAt = "15:00"
		if b.Anchor == domain.AnchorStart {
			onAt = "06:00"
		}
	}
	sched := domain.LightSchedule{
		EnvironmentID: id,
		Mode:          b.Mode,
		LightsOnAt:    onAt,
		Anchor:        b.Anchor,
		OnHours:       b.OnHours,
		StageOnHours:  stageOn,
	}
	if err := s.store.SaveLightSchedule(sched); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity(id, "", "info", "configuration", "Updated light schedule")
	writeJSON(w, http.StatusOK, sched)
}

// putControl updates an environment's per-category automation policy: the
// auto/manual mode for lighting, air exchange and irrigation, plus the manual
// fan setpoints. Every field is optional so the "Full automatic" master toggle
// can set them all at once, or a single panel can flip one category. Lighting is
// kept in lockstep with the light schedule (manual ⇔ the schedule is off) so the
// two never drift.
func (s *Server) putControl(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Lighting    *string `json:"lighting"`
		Air         *string `json:"air"`
		Exhaust     *int    `json:"exhaust"`
		Circulation *int    `json:"circulation"`
		Irrigation  *string `json:"irrigation"`
	}
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	parseMode := func(v string) (domain.ControlMode, bool) {
		switch domain.ControlMode(v) {
		case domain.ControlAuto, domain.ControlManual:
			return domain.ControlMode(v), true
		default:
			return "", false
		}
	}
	id := r.PathValue("id")
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var env *domain.Environment
	for i := range envs {
		if envs[i].ID == id {
			env = &envs[i]
			break
		}
	}
	if env == nil {
		writeJSON(w, http.StatusNotFound, errBody("environment not found"))
		return
	}
	ctrl := env.Control

	if b.Air != nil {
		mode, ok := parseMode(*b.Air)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errBody("air mode must be auto or manual"))
			return
		}
		ctrl.AirExchange.Mode = mode
	}
	if b.Exhaust != nil {
		if *b.Exhaust < 0 || *b.Exhaust > 100 {
			writeJSON(w, http.StatusBadRequest, errBody("exhaust must be between 0 and 100"))
			return
		}
		ctrl.AirExchange.Exhaust = *b.Exhaust
	}
	if b.Circulation != nil {
		if *b.Circulation < 0 || *b.Circulation > 100 {
			writeJSON(w, http.StatusBadRequest, errBody("circulation must be between 0 and 100"))
			return
		}
		ctrl.AirExchange.Circulation = *b.Circulation
	}
	if b.Irrigation != nil {
		mode, ok := parseMode(*b.Irrigation)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errBody("irrigation mode must be auto or manual"))
			return
		}
		ctrl.Irrigation.Mode = mode
	}
	if b.Lighting != nil {
		mode, ok := parseMode(*b.Lighting)
		if !ok {
			writeJSON(w, http.StatusBadRequest, errBody("lighting mode must be auto or manual"))
			return
		}
		ctrl.Lighting.Mode = mode
		// Keep the photoperiod schedule consistent: manual turns it off; auto
		// resumes a stage-following schedule if none is currently active.
		sched, _, err := s.store.LightSchedule(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		sched.EnvironmentID = id
		if mode == domain.ControlManual {
			sched.Mode = domain.LightScheduleOff
		} else if sched.Mode == domain.LightScheduleOff || sched.Mode == "" {
			sched.Mode = domain.LightSchedulePhase
		}
		if err := s.store.SaveLightSchedule(sched); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	env.Control = ctrl
	if err := s.store.SaveEnvironment(*env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity(id, "", "info", "configuration", "Updated control settings")
	writeJSON(w, http.StatusOK, ctrl)
}

// getLightingDefaults returns the recommended photoperiod (hours of light) for
// known growth stages, used to seed the schedule editor.
func (s *Server) getLightingDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, domain.DefaultStageLightHours)
}

func validScheduleMode(m domain.LightScheduleMode) bool {
	for _, x := range domain.AllLightScheduleModes {
		if x == m {
			return true
		}
	}
	return false
}

func validAnchor(a domain.LightAnchor) bool {
	for _, x := range domain.AllLightAnchors {
		if x == a {
			return true
		}
	}
	return false
}

func validHHMM(s string) bool {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 {
		return false
	}
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}

// parseDate accepts RFC3339 or YYYY-MM-DD, defaulting to now.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Now()
}
