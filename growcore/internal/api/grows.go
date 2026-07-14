package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/species"
)

// --- Grows ---

func (s *Server) getGrows(w http.ResponseWriter, r *http.Request) {
	grows, err := s.store.Grows()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if grows == nil {
		grows = []domain.Grow{}
	}
	writeJSON(w, http.StatusOK, grows)
}

func (s *Server) getStagePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, species.StagePresets())
}

type growBody struct {
	Name      string `json:"name"`
	Species   string `json:"species"`
	StartedAt string `json:"startedAt"` // RFC3339 or YYYY-MM-DD; empty = now
	Notes     string `json:"notes"`
}

// speciesStages validates a species against the predefined presets and returns
// its (auto-derived) stage sequence. Species is the single source of truth for
// stages; a grow cannot use an unknown crop family.
func speciesStages(name string) (key string, stages []string, ok bool) {
	sp, found := species.Get(name)
	if !found {
		return strings.ToLower(strings.TrimSpace(name)), nil, false
	}
	return sp.ID, sp.StageNames(), true
}

func (s *Server) createGrow(w http.ResponseWriter, r *http.Request) {
	var b growBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	species, stages, ok := speciesStages(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	now := time.Now()
	name := strings.TrimSpace(b.Name)
	grow := domain.Grow{
		ID:           id(name, "grow"),
		Name:         name,
		Slug:         domain.Slugify(name),
		Species:      species,
		Stage:        stages[0],
		Stages:       stages,
		StartedAt:    parseDate(b.StartedAt),
		StageStarted: now,
		Status:       domain.GrowActive,
		Notes:        b.Notes,
	}
	if err := s.store.SaveGrow(grow); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(grow.ID, "", "info", "configuration", "Created grow "+grow.Name)
	writeJSON(w, http.StatusOK, grow)
}

func (s *Server) updateGrow(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	var b growBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) != "" {
		grow.Name = strings.TrimSpace(b.Name)
		grow.Slug = domain.Slugify(grow.Name)
	}
	species, stages, ok := speciesStages(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	grow.Species = species
	grow.Stages = stages
	if b.StartedAt != "" {
		grow.StartedAt = parseDate(b.StartedAt)
	}
	grow.Notes = b.Notes
	// Species (hence the stage sequence) may have changed; keep the current stage
	// valid against the derived sequence.
	if !contains(grow.Stages, grow.Stage) {
		grow.Stage = grow.Stages[0]
	}
	if err := s.store.SaveGrow(grow); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, grow)
}

func (s *Server) deleteGrow(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGrow(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type stageBody struct {
	Stage string `json:"stage"`
}

func (s *Server) changeStage(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	var b stageBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	stage := strings.TrimSpace(b.Stage)
	if !contains(grow.Stages, stage) {
		writeJSON(w, http.StatusBadRequest, errBody("stage is not part of this grow's sequence"))
		return
	}
	grow.Stage = stage
	grow.StageStarted = time.Now()
	if err := s.store.SaveGrow(grow); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(grow.ID, "", "info", "configuration", grow.Name+" advanced to "+stage)
	writeJSON(w, http.StatusOK, grow)
}

func (s *Server) completeGrow(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	grow.Status = domain.GrowCompleted
	if err := s.store.SaveGrow(grow); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(grow.ID, "", "info", "configuration", "Completed grow "+grow.Name)
	writeJSON(w, http.StatusOK, grow)
}

// --- Grow detail (grow + plants + placements) ---

type placementView struct {
	domain.PlantPlacement
	EnvironmentName string `json:"environmentName"`
}

type plantDetail struct {
	domain.PlantUnit
	CurrentEnvironmentID   string            `json:"currentEnvironmentId"`
	CurrentEnvironmentName string            `json:"currentEnvironmentName"`
	Placements             []placementView   `json:"placements"`
	CurrentPot             *domain.PlantPot  `json:"currentPot,omitempty"`
	Pots                   []domain.PlantPot `json:"pots"`
}

type growDetail struct {
	domain.Grow
	StageDays  int           `json:"stageDays"`
	TotalDays  int           `json:"totalDays"`
	PlantCount int           `json:"plantCount"`
	Plants     []plantDetail `json:"plants"`
}

func (s *Server) getGrow(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	envName, err := s.environmentNames()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	units, err := s.store.PlantUnits(grow.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Hydrate placements and pots for all of the grow's units in two queries
	// (rather than a pair per unit), then index into the results below.
	unitIDs := make([]string, len(units))
	for i, u := range units {
		unitIDs[i] = u.ID
	}
	placementsByUnit, err := s.store.PlacementsForUnits(unitIDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	potsByUnit, err := s.store.PotsForUnits(unitIDs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now()
	detail := growDetail{
		Grow:      grow,
		StageDays: domain.DaysSince(grow.StageStarted, now),
		TotalDays: domain.DaysSince(grow.StartedAt, now),
		Plants:    []plantDetail{},
	}
	for _, u := range units {
		if u.Status == domain.PlantActive {
			detail.PlantCount += u.Quantity
		}
		pd := plantDetail{PlantUnit: u, Placements: []placementView{}}
		for _, p := range placementsByUnit[u.ID] {
			pd.Placements = append(pd.Placements, placementView{PlantPlacement: p, EnvironmentName: envName[p.EnvironmentID]})
			if p.EndedAt == nil {
				pd.CurrentEnvironmentID = p.EnvironmentID
				pd.CurrentEnvironmentName = envName[p.EnvironmentID]
			}
		}
		pd.Pots, pd.CurrentPot = normalizePots(potsByUnit[u.ID])
		detail.Plants = append(detail.Plants, pd)
	}
	writeJSON(w, http.StatusOK, detail)
}

// --- Plants ---

type plantView struct {
	domain.PlantUnit
	GrowName               string            `json:"growName"`
	CurrentEnvironmentID   string            `json:"currentEnvironmentId"`
	CurrentEnvironmentName string            `json:"currentEnvironmentName"`
	Placements             []placementView   `json:"placements"`
	CurrentPot             *domain.PlantPot  `json:"currentPot,omitempty"`
	Pots                   []domain.PlantPot `json:"pots"`
}

// potsFor returns a unit's repot history (newest first) and its current (open)
// pot, if any.
func (s *Server) potsFor(unitID string) ([]domain.PlantPot, *domain.PlantPot, error) {
	pots, err := s.store.PotsForUnit(unitID)
	if err != nil {
		return nil, nil, err
	}
	list, current := normalizePots(pots)
	return list, current, nil
}

// normalizePots returns a non-nil pot list (newest first) and the current (open)
// pot, if any. pots is expected already ordered newest first.
func normalizePots(pots []domain.PlantPot) ([]domain.PlantPot, *domain.PlantPot) {
	if pots == nil {
		pots = []domain.PlantPot{}
	}
	var current *domain.PlantPot
	for i := range pots {
		if pots[i].EndedAt == nil {
			current = &pots[i]
			break
		}
	}
	return pots, current
}

func (s *Server) getPlant(w http.ResponseWriter, r *http.Request) {
	unit, ok, err := s.store.PlantUnit(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("plant not found"))
		return
	}
	envName, err := s.environmentNames()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	placements, err := s.store.PlacementsForUnit(unit.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	pv := plantView{PlantUnit: unit, Placements: []placementView{}}
	if grow, ok, _ := s.store.Grow(unit.GrowID); ok {
		pv.GrowName = grow.Name
	}
	for _, p := range placements {
		pv.Placements = append(pv.Placements, placementView{PlantPlacement: p, EnvironmentName: envName[p.EnvironmentID]})
		if p.EndedAt == nil {
			pv.CurrentEnvironmentID = p.EnvironmentID
			pv.CurrentEnvironmentName = envName[p.EnvironmentID]
		}
	}
	if pv.Pots, pv.CurrentPot, err = s.potsFor(unit.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, pv)
}

// plantBody creates one plant record: either a single individual plant, or one
// group (tray/bed/batch) whose Quantity is how many plants it holds. Each call
// makes exactly one unit with its own id and placement history.
type plantBody struct {
	Tracking      domain.TrackingMode `json:"tracking"`
	Quantity      int                 `json:"quantity"` // plants in the group; ignored for individuals
	Label         string              `json:"label"`
	Cultivar      string              `json:"cultivar"`
	EnvironmentID string              `json:"environmentId"`
	// Optional initial pot; PotSize > 0 opens the plant's first pot record.
	PotSize float64 `json:"potSize"`
	PotUnit string  `json:"potUnit"`
	PotType string  `json:"potType"`
}

// potUnit normalizes a pot volume unit to "L" or "gal" (default "L").
func potUnit(u string) string {
	if strings.EqualFold(strings.TrimSpace(u), "gal") {
		return "gal"
	}
	return "L"
}

func (s *Server) createPlants(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	var b plantBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tracking := b.Tracking
	if tracking != domain.TrackIndividual && tracking != domain.TrackGroup {
		tracking = domain.TrackIndividual
	}
	// Individuals are always a single plant; a group holds its Quantity of plants.
	quantity := 1
	if tracking == domain.TrackGroup {
		quantity = b.Quantity
		if quantity < 1 {
			quantity = 1
		}
	}
	if b.EnvironmentID != "" {
		envs, err := s.store.Environments()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !containsEnv(envs, b.EnvironmentID) {
			writeJSON(w, http.StatusBadRequest, errBody("environment not found"))
			return
		}
	}
	label := strings.TrimSpace(b.Label)
	if label == "" {
		if tracking == domain.TrackGroup {
			label = "Group"
		} else {
			label = "Plant"
		}
	}
	// One record per call: each plant (individual or group) gets its own id and
	// placement history.
	now := time.Now()
	units, err := s.store.BulkCreatePlants(grow.ID, 1, tracking, quantity, label, strings.TrimSpace(b.Cultivar), b.EnvironmentID, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Optional starting pot, opening the plant's repot history.
	if b.PotSize > 0 {
		if err := s.store.Repot(units[0].ID, b.PotSize, potUnit(b.PotUnit), strings.TrimSpace(b.PotType), now); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.growActivity(grow.ID, b.EnvironmentID, "info", "configuration", "Added a plant to "+grow.Name)
	writeJSON(w, http.StatusOK, units[0])
}

type updatePlantBody struct {
	Label    string              `json:"label"`
	Cultivar string              `json:"cultivar"`
	Tracking domain.TrackingMode `json:"tracking"` // omitted/invalid leaves it unchanged
	Quantity *int                `json:"quantity"` // pointer: omitted leaves quantity unchanged
}

// updatePlant edits a plant unit's per-unit attributes (label, cultivar, and —
// for groups — quantity). Placement, grow and status are changed via their own
// endpoints.
func (s *Server) updatePlant(w http.ResponseWriter, r *http.Request) {
	unit, ok, err := s.store.PlantUnit(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("plant not found"))
		return
	}
	var b updatePlantBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	unit.Label = strings.TrimSpace(b.Label)
	unit.Slug = domain.Slugify(unit.Label)
	unit.Cultivar = strings.TrimSpace(b.Cultivar)
	if b.Tracking == domain.TrackIndividual || b.Tracking == domain.TrackGroup {
		unit.Tracking = b.Tracking
	}
	// Individuals are always a single plant; groups carry their quantity.
	if unit.Tracking == domain.TrackIndividual {
		unit.Quantity = 1
	} else if b.Quantity != nil && *b.Quantity > 0 {
		unit.Quantity = *b.Quantity
	}
	if err := s.store.SavePlantUnit(unit); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(unit.GrowID, "", "info", "configuration", "Updated "+plantLabel(unit))
	writeJSON(w, http.StatusOK, unit)
}

type moveBody struct {
	EnvironmentID string `json:"environmentId"`
}

func (s *Server) movePlant(w http.ResponseWriter, r *http.Request) {
	unit, ok, err := s.store.PlantUnit(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("plant not found"))
		return
	}
	var b moveBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !containsEnv(envs, b.EnvironmentID) {
		writeJSON(w, http.StatusBadRequest, errBody("environment not found"))
		return
	}
	if err := s.store.MovePlant(unit.ID, b.EnvironmentID, time.Now()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(unit.GrowID, b.EnvironmentID, "info", "configuration", "Moved "+plantLabel(unit)+" here")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type repotBody struct {
	Size float64 `json:"size"`
	Unit string  `json:"unit"`
	Type string  `json:"type"`
}

// repotPlant records a repot: it closes the plant's current pot and opens a new
// one, keeping the size history (mirrors movePlant).
func (s *Server) repotPlant(w http.ResponseWriter, r *http.Request) {
	unit, ok, err := s.store.PlantUnit(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("plant not found"))
		return
	}
	var b repotBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if b.Size <= 0 {
		writeJSON(w, http.StatusBadRequest, errBody("pot size must be positive"))
		return
	}
	if err := s.store.Repot(unit.ID, b.Size, potUnit(b.Unit), strings.TrimSpace(b.Type), time.Now()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.growActivity(unit.GrowID, "", "info", "configuration", "Repotted "+plantLabel(unit))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) setPlantStatus(status domain.PlantStatus, verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unit, ok, err := s.store.PlantUnit(r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, errBody("plant not found"))
			return
		}
		unit.Status = status
		if err := s.store.SavePlantUnit(unit); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		s.growActivity(unit.GrowID, "", "info", "configuration", verb+" "+plantLabel(unit))
		writeJSON(w, http.StatusOK, unit)
	}
}

func plantLabel(u domain.PlantUnit) string {
	if u.Label != "" {
		return u.Label
	}
	return "plant"
}

// --- Environment occupancy & control grow ---

type envPlantsGroup struct {
	Grow  domain.Grow        `json:"grow"`
	Units []domain.PlantUnit `json:"units"`
}

func (s *Server) getEnvironmentPlants(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	units, err := s.store.PlantsInEnvironment(envID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	byGrow := map[string][]domain.PlantUnit{}
	order := []string{}
	for _, u := range units {
		if _, seen := byGrow[u.GrowID]; !seen {
			order = append(order, u.GrowID)
		}
		byGrow[u.GrowID] = append(byGrow[u.GrowID], u)
	}
	groups := []envPlantsGroup{}
	for _, growID := range order {
		grow, ok, err := s.store.Grow(growID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if !ok {
			continue
		}
		groups = append(groups, envPlantsGroup{Grow: grow, Units: byGrow[growID]})
	}
	writeJSON(w, http.StatusOK, groups)
}

type controlGrowBody struct {
	GrowID string `json:"growId"`
}

func (s *Server) putControlGrow(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	envs, err := s.store.Environments()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	env, ok := findEnvByID(envs, envID)
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("environment not found"))
		return
	}
	var b controlGrowBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if b.GrowID != "" {
		if _, exists, err := s.store.Grow(b.GrowID); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		} else if !exists {
			writeJSON(w, http.StatusBadRequest, errBody("grow not found"))
			return
		}
	}
	env.ControlGrowID = b.GrowID
	if err := s.store.SaveEnvironment(env); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if b.GrowID == "" {
		s.activity(envID, "", "info", "configuration", "Cleared control grow")
	} else {
		s.activity(envID, "", "info", "configuration", "Set control grow")
	}
	writeJSON(w, http.StatusOK, env)
}

// environmentNames returns a map of environment id -> display name.
func (s *Server) environmentNames() (map[string]string, error) {
	envs, err := s.store.Environments()
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(envs))
	for _, e := range envs {
		m[e.ID] = e.Name
	}
	return m, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
