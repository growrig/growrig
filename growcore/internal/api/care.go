package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/species"
)

// --- Care action configuration (per-grow customization of species actions) ---

// careActionDef is one resolved care action for a grow: the effective action
// (species default overlaid with the grow's customization) plus whether it is
// enabled and whether it is a user-added custom action. It is the shape the log
// dialog and the care-settings editor both consume.
type careActionDef struct {
	species.CareAction
	Enabled bool `json:"enabled"`
	Custom  bool `json:"custom"`
}

// effectiveCareActions resolves a grow's care actions: its species defaults
// overlaid with any per-grow config (order, enable/disable, rename, quick,
// custom actions). Species actions the config omits are appended, enabled, so
// actions added to a species later still surface.
func (s *Server) effectiveCareActions(grow domain.Grow) ([]careActionDef, error) {
	speciesActions, _ := species.CareActionsFor(grow.Species)
	byKey := make(map[string]species.CareAction, len(speciesActions))
	for _, a := range speciesActions {
		byKey[a.Key] = a
	}
	cfg, ok, err := s.store.GrowCareConfig(grow.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		// No customization: species defaults, all enabled, in species order.
		out := make([]careActionDef, 0, len(speciesActions))
		for _, a := range speciesActions {
			out = append(out, careActionDef{CareAction: a, Enabled: true})
		}
		return out, nil
	}
	out := make([]careActionDef, 0, len(cfg.Actions)+len(speciesActions))
	seen := map[string]bool{}
	for _, item := range cfg.Actions {
		seen[item.Key] = true
		if item.Custom {
			fields := make([]species.CareField, 0, len(item.Fields))
			for _, f := range item.Fields {
				fields = append(fields, species.CareField(f))
			}
			out = append(out, careActionDef{
				CareAction: species.CareAction{Key: item.Key, Label: item.Label, Icon: "list-plus", Fields: fields, Quick: item.Quick},
				Enabled:    item.Enabled, Custom: true,
			})
			continue
		}
		base, known := byKey[item.Key]
		if !known {
			continue // species dropped this action; drop it from the grow too
		}
		if item.Label != "" {
			base.Label = item.Label
		}
		base.Quick = item.Quick
		out = append(out, careActionDef{CareAction: base, Enabled: item.Enabled})
	}
	for _, a := range speciesActions {
		if !seen[a.Key] {
			out = append(out, careActionDef{CareAction: a, Enabled: true})
		}
	}
	return out, nil
}

// careActionForGrow returns an enabled action available for logging on this
// grow, resolving per-grow customization and custom actions.
func (s *Server) careActionForGrow(grow domain.Grow, key string) (species.CareAction, bool, error) {
	defs, err := s.effectiveCareActions(grow)
	if err != nil {
		return species.CareAction{}, false, err
	}
	for _, d := range defs {
		if d.Key == key && d.Enabled {
			return d.CareAction, true, nil
		}
	}
	return species.CareAction{}, false, nil
}

func (s *Server) getCareConfig(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	defs, err := s.effectiveCareActions(grow)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": defs})
}

type careConfigBody struct {
	Actions []domain.GrowCareActionConfig `json:"actions"`
}

var validCareFields = map[string]bool{
	"amount": true, "runoff": true, "recipe": true, "ph": true, "ec": true,
	"note": true, "photos": true, "potSize": true, "product": true, "trainType": true,
	"doses": true,
}

func (s *Server) putCareConfig(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	var b careConfigBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	speciesActions, _ := species.CareActionsFor(grow.Species)
	speciesKeys := map[string]bool{}
	for _, a := range speciesActions {
		speciesKeys[a.Key] = true
	}
	seen := map[string]bool{}
	for i := range b.Actions {
		a := &b.Actions[i]
		a.Key = strings.TrimSpace(a.Key)
		if a.Key == "" {
			writeJSON(w, http.StatusBadRequest, errBody("every care action needs a key"))
			return
		}
		if seen[a.Key] {
			writeJSON(w, http.StatusBadRequest, errBody("duplicate care action: "+a.Key))
			return
		}
		seen[a.Key] = true
		if a.Custom {
			if speciesKeys[a.Key] {
				writeJSON(w, http.StatusBadRequest, errBody("custom action collides with a built-in: "+a.Key))
				return
			}
			if strings.TrimSpace(a.Label) == "" {
				writeJSON(w, http.StatusBadRequest, errBody("custom action needs a label: "+a.Key))
				return
			}
			for _, f := range a.Fields {
				if !validCareFields[f] {
					writeJSON(w, http.StatusBadRequest, errBody("unknown field on custom action: "+f))
					return
				}
			}
		} else if !speciesKeys[a.Key] {
			writeJSON(w, http.StatusBadRequest, errBody("unknown built-in care action: "+a.Key))
			return
		}
	}
	if err := s.store.SaveGrowCareConfig(grow.ID, domain.GrowCareConfig{Actions: b.Actions}); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defs, err := s.effectiveCareActions(grow)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": defs})
}

// --- Care: the grow's manual-action journal ---

// careEventView is a care event with resolved plant labels and recipe name.
type careEventView struct {
	domain.CareEvent
	Applications []domain.CareApplication `json:"applications"`
	RecipeName   string                   `json:"recipeName,omitempty"`
	// DosesLabel is Doses rendered for display ("2 tabs · 1 g Mycotrex"), so
	// clients don't each re-resolve product keys against the recipe.
	DosesLabel string `json:"dosesLabel,omitempty"`
}

// careSkip flags a plant left out of the grow's most recent care action.
type careSkip struct {
	PlantUnitID string     `json:"plantUnitId"`
	PlantLabel  string     `json:"plantLabel"`
	LastCareAt  *time.Time `json:"lastCareAt,omitempty"`
}

type careSummary struct {
	LastByType map[string]careEventView `json:"lastByType"`
	Skipped    []careSkip               `json:"skipped"`
}

type careHistory struct {
	Summary careSummary     `json:"summary"`
	Events  []careEventView `json:"events"`
}

// plantLabels returns a plant-unit-id -> display label map for a grow's units,
// plus the active units, so care handlers resolve labels and default targets.
func (s *Server) growPlants(growID string) (map[string]domain.PlantUnit, error) {
	units, err := s.store.PlantUnits(growID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.PlantUnit, len(units))
	for _, u := range units {
		byID[u.ID] = u
	}
	return byID, nil
}

// recipeName resolves a feeding recipe id to its display name, or "" if unknown.
func (s *Server) recipeName(id string) string {
	if id == "" {
		return ""
	}
	if p, ok, _ := s.store.FeedingRecipe(id); ok {
		return p.Name
	}
	return ""
}

// doseSummary renders an amendment's per-plant doses for display, e.g.
// "2 tabs · 1 g Mycotrex" — resolving each product key against its recipe for
// the label and unit. Keys the recipe no longer knows are shown bare rather
// than dropped, so a renamed product doesn't silently erase history.
func (s *Server) doseSummary(recipeID string, doses map[string]float64) string {
	if len(doses) == 0 {
		return ""
	}
	labels := map[string]domain.FeedingProduct{}
	unit := ""
	if r, ok, _ := s.store.FeedingRecipe(recipeID); ok {
		unit = r.Unit
		for _, p := range r.Products {
			labels[p.Key] = p
		}
	}
	// Map order is random; sort by key so the same event reads the same twice.
	keys := make([]string, 0, len(doses))
	for k := range doses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		p, known := labels[k]
		u := unit
		if p.Unit != "" {
			u = p.Unit
		}
		label := k
		if known && p.Label != "" {
			label = p.Label
		}
		parts = append(parts, strings.TrimSpace(fmt.Sprintf("%s %s %s", trimFloat(doses[k]), u, label)))
	}
	return strings.Join(parts, " · ")
}

// trimFloat prints a dose without trailing zeros: 2 not 2.00, 1.5 not 1.50.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func (s *Server) toCareEventView(e domain.CareEvent, plants map[string]domain.PlantUnit) careEventView {
	v := careEventView{
		CareEvent:  e,
		RecipeName: s.recipeName(e.RecipeID),
		DosesLabel: s.doseSummary(e.RecipeID, e.Doses),
	}
	v.Applications = make([]domain.CareApplication, 0, len(e.Applications))
	for _, a := range e.Applications {
		a.PlantLabel = resolvePlantLabel(plants, a.PlantUnitID)
		v.Applications = append(v.Applications, a)
	}
	// Nil the embedded slice so JSON serialises the augmented one, not both.
	v.CareEvent.Applications = nil
	return v
}

func (s *Server) getCare(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	plants, err := s.growPlants(grow.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	events, err := s.store.CareEvents(grow.ID, 100, 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	lastByType, err := s.store.LastCareByType(grow.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	lastPerPlant, err := s.store.LastCarePerPlant(grow.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	out := careHistory{
		Summary: careSummary{LastByType: map[string]careEventView{}, Skipped: []careSkip{}},
		Events:  make([]careEventView, 0, len(events)),
	}
	for _, e := range events {
		out.Events = append(out.Events, s.toCareEventView(e, plants))
	}
	for t, e := range lastByType {
		out.Summary.LastByType[t] = s.toCareEventView(e, plants)
	}
	// A plant is "skipped" when its most recent care predates the grow's latest
	// care action (or it has never been cared for) — i.e. it was left out of the
	// last batch. With no care logged yet, nothing is flagged.
	if len(events) > 0 {
		latest := events[0].OccurredAt
		for _, u := range plants {
			if u.Status != domain.PlantActive {
				continue
			}
			last, seen := lastPerPlant[u.ID]
			if !seen || last.Before(latest) {
				skip := careSkip{PlantUnitID: u.ID, PlantLabel: plantLabel(u)}
				if seen {
					lt := last
					skip.LastCareAt = &lt
				}
				out.Summary.Skipped = append(out.Summary.Skipped, skip)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// careApplicationInput is one plant's line in a log-care request.
type careApplicationInput struct {
	PlantUnitID string  `json:"plantUnitId"`
	AmountML    float64 `json:"amountMl"`
	Note        string  `json:"note"`
}

// logCareBody logs one care action. Provide either explicit per-plant
// Applications, or PlantUnitIDs + AmountML to apply the same amount to each
// (empty PlantUnitIDs targets every active plant in the grow).
type logCareBody struct {
	Type         string                 `json:"type"`
	OccurredAt   string                 `json:"occurredAt"` // RFC3339 or YYYY-MM-DD; empty = now
	Source       string                 `json:"source"`
	Notes        string                 `json:"notes"`
	RecipeID     string                 `json:"recipeId"`
	PH           float64                `json:"ph"`
	EC           float64                `json:"ec"`
	RunoffML     float64                `json:"runoffMl"`
	RunoffPH     float64                `json:"runoffPh"`
	Doses        map[string]float64     `json:"doses"`
	AmountML     float64                `json:"amountMl"`
	PlantUnitIDs []string               `json:"plantUnitIds"`
	Applications []careApplicationInput `json:"applications"`
	// The new container, for actions carrying the potSize field (transplant).
	// Recording one actually repots every targeted plant rather than only
	// noting the size in the journal.
	PotSize float64 `json:"potSize"`
	PotUnit string  `json:"potUnit"`
	PotType string  `json:"potType"`
}

func (s *Server) logCare(w http.ResponseWriter, r *http.Request) {
	grow, ok, err := s.store.Grow(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("grow not found"))
		return
	}
	var b logCareBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	action, known, err := s.careActionForGrow(grow, strings.TrimSpace(b.Type))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !known {
		writeJSON(w, http.StatusBadRequest, errBody("care action is not enabled for this grow"))
		return
	}
	plants, err := s.growPlants(grow.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Resolve the per-plant applications: explicit list, else broadcast AmountML
	// to the named plants (or every active plant when none are named).
	var inputs []careApplicationInput
	if len(b.Applications) > 0 {
		inputs = b.Applications
	} else {
		ids := b.PlantUnitIDs
		if len(ids) == 0 {
			for _, u := range plants {
				if u.Status == domain.PlantActive {
					ids = append(ids, u.ID)
				}
			}
		}
		for _, id := range ids {
			inputs = append(inputs, careApplicationInput{PlantUnitID: id, AmountML: b.AmountML})
		}
	}
	if len(inputs) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("no plants selected for this care action"))
		return
	}

	event := domain.CareEvent{
		ID:         id(grow.Name+"-"+action.Key, "care"),
		GrowID:     grow.ID,
		Type:       action.Key,
		OccurredAt: parseDate(b.OccurredAt),
		Source:     careSource(b.Source),
		Notes:      strings.TrimSpace(b.Notes),
		RecipeID:   strings.TrimSpace(b.RecipeID),
		PH:         b.PH,
		EC:         b.EC,
		RunoffML:   b.RunoffML,
		RunoffPH:   b.RunoffPH,
		Doses:      positiveDoses(b.Doses),
		CreatedAt:  time.Now(),
	}
	for _, in := range inputs {
		if _, ok := plants[in.PlantUnitID]; !ok {
			writeJSON(w, http.StatusBadRequest, errBody("plant "+in.PlantUnitID+" is not part of this grow"))
			return
		}
		event.Applications = append(event.Applications, domain.CareApplication{
			ID:          id(action.Key, "careapp"),
			CareEventID: event.ID,
			PlantUnitID: in.PlantUnitID,
			AmountML:    in.AmountML,
			Note:        strings.TrimSpace(in.Note),
		})
	}
	// A transplant is a pot change, not just a note about one: move every
	// targeted plant into the new container so the plant's current pot and its
	// pot history stay true.
	repot := b.PotSize > 0 && actionHasField(action, species.FieldPotSize)
	if b.PotSize < 0 {
		writeJSON(w, http.StatusBadRequest, errBody("pot size cannot be negative"))
		return
	}
	if err := s.store.SaveCareEvent(event); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if repot {
		for _, app := range event.Applications {
			if err := s.store.Repot(app.PlantUnitID, b.PotSize, potUnit(b.PotUnit), strings.TrimSpace(b.PotType), event.OccurredAt); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	s.growActivity(grow.ID, "", "info", "care",
		careJournalMessage(action, event, s.recipeName(event.RecipeID), s.doseSummary(event.RecipeID, event.Doses)))
	writeJSON(w, http.StatusOK, s.toCareEventView(event, plants))
}

// updateCare corrects an already-logged care event: when it happened, the note,
// the solution readings and the per-plant amounts. The action type is fixed —
// a mis-picked action is a delete plus a fresh log, not an edit.
func (s *Server) updateCare(w http.ResponseWriter, r *http.Request) {
	existing, ok, err := s.store.CareEvent(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("care event not found"))
		return
	}
	var b logCareBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	plants, err := s.growPlants(existing.GrowID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	updated := existing
	updated.OccurredAt = parseDate(b.OccurredAt)
	if b.OccurredAt == "" {
		updated.OccurredAt = existing.OccurredAt
	}
	updated.Notes = strings.TrimSpace(b.Notes)
	updated.RecipeID = strings.TrimSpace(b.RecipeID)
	updated.PH, updated.EC = b.PH, b.EC
	updated.RunoffML, updated.RunoffPH = b.RunoffML, b.RunoffPH
	updated.Doses = positiveDoses(b.Doses)

	// Applications are replaced wholesale when given, so a plant can be dropped
	// from an event that recorded it by mistake.
	if len(b.Applications) > 0 || len(b.PlantUnitIDs) > 0 {
		inputs := b.Applications
		if len(inputs) == 0 {
			for _, unitID := range b.PlantUnitIDs {
				inputs = append(inputs, careApplicationInput{PlantUnitID: unitID, AmountML: b.AmountML})
			}
		}
		updated.Applications = nil
		for _, in := range inputs {
			if _, ok := plants[in.PlantUnitID]; !ok {
				writeJSON(w, http.StatusBadRequest, errBody("plant "+in.PlantUnitID+" is not part of this grow"))
				return
			}
			updated.Applications = append(updated.Applications, domain.CareApplication{
				ID:          id(updated.Type, "careapp"),
				CareEventID: updated.ID,
				PlantUnitID: in.PlantUnitID,
				AmountML:    in.AmountML,
				Note:        strings.TrimSpace(in.Note),
			})
		}
		if len(updated.Applications) == 0 {
			writeJSON(w, http.StatusBadRequest, errBody("a care event needs at least one plant"))
			return
		}
	}

	if err := s.store.UpdateCareEvent(updated); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toCareEventView(updated, plants))
}

func (s *Server) deleteCare(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCareEvent(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// positiveDoses drops blank and non-positive entries so an untouched dose input
// doesn't persist as "0 tablets per plant". Returns nil when nothing remains.
func positiveDoses(in map[string]float64) map[string]float64 {
	var out map[string]float64
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" || v <= 0 {
			continue
		}
		if out == nil {
			out = map[string]float64{}
		}
		out[key] = v
	}
	return out
}

func careSource(v string) domain.CareSource {
	if strings.EqualFold(strings.TrimSpace(v), string(domain.CareAutomation)) {
		return domain.CareAutomation
	}
	return domain.CareManual
}

// careJournalMessage renders the grouped, human-readable activity-log line for a
// care event, e.g. "Watered 4 plants · 3.2 L total" or "Fed all plants · Veg
// Base · EC 1.4".
func careJournalMessage(action species.CareAction, e domain.CareEvent, recipe, doses string) string {
	n := len(e.Applications)
	verb := careVerb(action)
	msg := fmt.Sprintf("%s %s", verb, plantsPhrase(n))
	var parts []string
	if total := e.TotalML(); total > 0 {
		parts = append(parts, formatVolume(total))
	}
	if recipe != "" {
		parts = append(parts, recipe)
	}
	if doses != "" {
		// Amendments are per plant, so say so — "2 tabs" alone is ambiguous.
		parts = append(parts, doses+" per plant")
	}
	if e.EC > 0 {
		parts = append(parts, fmt.Sprintf("EC %.1f", e.EC))
	}
	if len(parts) > 0 {
		msg += " · " + strings.Join(parts, " · ")
	}
	return msg
}

// careVerb turns an action into a past-tense journal verb, falling back to the
// action label for custom/unknown actions.
func careVerb(a species.CareAction) string {
	switch a.Key {
	case "water":
		return "Watered"
	case "feed":
		return "Fed"
	case "inspect":
		return "Inspected"
	case "train":
		return "Trained"
	case "trim", "prune":
		return "Trimmed"
	case "transplant":
		return "Transplanted"
	case "treat":
		return "Treated"
	case "flush":
		return "Flushed"
	case "harvest":
		return "Harvested"
	case "stake":
		return "Staked"
	case "pollinate":
		return "Pollinated"
	default:
		return a.Label
	}
}

func plantsPhrase(n int) string {
	if n == 1 {
		return "1 plant"
	}
	return fmt.Sprintf("%d plants", n)
}

// formatVolume renders millilitres as a compact "900 ml" or "3.2 L" string.
func formatVolume(ml float64) string {
	if ml >= 1000 {
		return fmt.Sprintf("%.1f L", ml/1000)
	}
	return fmt.Sprintf("%.0f ml", ml)
}

// resolvePlantLabel names a plant for display, falling back to its id when the
// unit is gone (a deleted plant still has its care history).
func resolvePlantLabel(plants map[string]domain.PlantUnit, unitID string) string {
	if u, ok := plants[unitID]; ok {
		return plantLabel(u)
	}
	return unitID
}

// actionHasField reports whether a care action collects the given field.
func actionHasField(a species.CareAction, f species.CareField) bool {
	for _, have := range a.Fields {
		if have == f {
			return true
		}
	}
	return false
}
