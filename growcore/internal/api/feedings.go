package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/feeding"
	"github.com/growrig/growrig/growcore/internal/species"
)

// Feeding recipes are user-owned nutrient schedules stored as YAML on disk (see
// store/feeding.go). Built-in recipe templates (the BioBizz charts) are a
// separate, read-only catalog exposed only as *templates* to prefill the create
// form — they never appear in the user's own list.

// getRecipes lists the user's recipes (the ones shown in the Knowledge library).
func (s *Server) getRecipes(w http.ResponseWriter, r *http.Request) {
	recipes, err := s.store.FeedingRecipes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if recipes == nil {
		recipes = []domain.FeedingRecipe{}
	}
	writeJSON(w, http.StatusOK, recipes)
}

// getRecipeTemplates lists the built-in recipe templates used to seed a new
// recipe, optionally filtered to one species.
func (s *Server) getRecipeTemplates(w http.ResponseWriter, r *http.Request) {
	sp := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("species")))
	var templates []domain.FeedingRecipe
	if sp != "" {
		templates = feeding.BySpecies(sp)
	} else {
		templates = feeding.All()
	}
	if templates == nil {
		templates = []domain.FeedingRecipe{}
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) getRecipe(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.store.FeedingRecipe(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("recipe not found"))
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type recipeBody struct {
	Species     string                  `json:"species"`
	Name        string                  `json:"name"`
	Brand       string                  `json:"brand"`
	Description string                  `json:"description"`
	Unit        string                  `json:"unit"`
	Products    []domain.FeedingProduct `json:"products"`
	Phases      []domain.FeedingPhase   `json:"phases"`
}

// sanitizeRecipe trims text and drops empty product/phase/week entries so the
// stored preset stays clean regardless of editor churn.
func sanitizeRecipe(b recipeBody) (products []domain.FeedingProduct, phases []domain.FeedingPhase) {
	valid := map[string]bool{}
	for _, p := range b.Products {
		key := strings.TrimSpace(p.Key)
		label := strings.TrimSpace(p.Label)
		if key == "" || label == "" {
			continue
		}
		valid[key] = true
		basis := domain.DoseBasis(strings.TrimSpace(string(p.Basis)))
		if !domain.ValidBasis(basis) {
			basis = domain.BasisSolution
		}
		products = append(products, domain.FeedingProduct{
			Key:   key,
			Label: label,
			Unit:  strings.TrimSpace(p.Unit),
			Basis: basis,
		})
	}
	// Keep only doses naming a surviving product with a positive amount.
	clean := func(in map[string]float64) map[string]float64 {
		out := map[string]float64{}
		for k, v := range in {
			if valid[k] && v > 0 {
				out[k] = v
			}
		}
		return out
	}
	for _, ph := range b.Phases {
		name := strings.TrimSpace(ph.Name)
		if name == "" {
			continue
		}
		trigger := domain.PhaseTrigger(strings.TrimSpace(string(ph.Trigger)))
		if !domain.ValidTrigger(trigger) {
			trigger = domain.TriggerWeekly
		}
		phase := domain.FeedingPhase{
			Name:    name,
			Stage:   strings.TrimSpace(ph.Stage),
			Trigger: trigger,
		}
		// A phase is weekly or one-shot, never both: drop whichever half does
		// not belong so a phase switched in the editor leaves nothing behind.
		if phase.OneShot() {
			phase.Doses = clean(ph.Doses)
		} else {
			for _, wk := range ph.Weeks {
				phase.Weeks = append(phase.Weeks, domain.FeedingWeek{Doses: clean(wk.Doses)})
			}
		}
		phases = append(phases, phase)
	}
	return products, phases
}

func (s *Server) createRecipe(w http.ResponseWriter, r *http.Request) {
	var b recipeBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	sp, ok := species.Get(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	products, phases := sanitizeRecipe(b)
	p := domain.FeedingRecipe{
		ID:          id(b.Name, "recipe"),
		Species:     sp.ID,
		Name:        strings.TrimSpace(b.Name),
		Brand:       strings.TrimSpace(b.Brand),
		Description: strings.TrimSpace(b.Description),
		Source:      "user",
		Unit:        strings.TrimSpace(b.Unit),
		Products:    products,
		Phases:      phases,
		CreatedAt:   time.Now(),
	}
	if err := s.store.SaveFeedingRecipe(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", "Created feeding recipe "+p.Name)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateRecipe(w http.ResponseWriter, r *http.Request) {
	p, ok, err := s.store.FeedingRecipe(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("recipe not found"))
		return
	}
	var b recipeBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	sp, ok := species.Get(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	products, phases := sanitizeRecipe(b)
	p.Species = sp.ID
	p.Name = strings.TrimSpace(b.Name)
	p.Brand = strings.TrimSpace(b.Brand)
	p.Description = strings.TrimSpace(b.Description)
	p.Unit = strings.TrimSpace(b.Unit)
	p.Products = products
	p.Phases = phases
	if err := s.store.SaveFeedingRecipe(p); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteRecipe(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFeedingRecipe(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
