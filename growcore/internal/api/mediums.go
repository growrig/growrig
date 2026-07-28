package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/medium"
	"github.com/growrig/growrig/growcore/internal/species"
)

// Medium recipes are substrate compositions: what a growing medium is mixed
// from. User recipes are stored as YAML on disk (see store/medium.go); built-in
// recipes are a separate read-only catalog exposed as *templates* to prefill
// the create form, exactly as feeding recipes work.
//
// Unlike feeding recipes these are not scoped to a species — a coco/perlite mix
// serves any crop — so `species` is an optional tag rather than a requirement.

// getMediumRecipes lists the user's medium recipes.
func (s *Server) getMediumRecipes(w http.ResponseWriter, r *http.Request) {
	recipes, err := s.store.MediumRecipes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if recipes == nil {
		recipes = []domain.MediumRecipe{}
	}
	writeJSON(w, http.StatusOK, recipes)
}

// getMediumTemplates lists the built-in recipes used to seed a new one.
func (s *Server) getMediumTemplates(w http.ResponseWriter, r *http.Request) {
	templates := medium.All()
	if templates == nil {
		templates = []domain.MediumRecipe{}
	}
	writeJSON(w, http.StatusOK, templates)
}

// getMediumRecipe resolves a user recipe, falling back to a built-in so a grow
// referencing a template still renders.
func (s *Server) getMediumRecipe(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok, err := s.store.MediumRecipe(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		if rec, ok = medium.Get(id); !ok {
			writeJSON(w, http.StatusNotFound, errBody("medium recipe not found"))
			return
		}
	}
	writeJSON(w, http.StatusOK, rec)
}

type mediumBody struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Species     string                   `json:"species"`
	Base        string                   `json:"base"`
	Components  []domain.MediumComponent `json:"components"`
	Amendments  []domain.MediumAmendment `json:"amendments"`
}

// sanitizeMedium trims text and drops unlabelled or non-positive entries so a
// stored recipe stays clean regardless of editor churn.
//
// It deliberately does NOT normalise the percentages to 100: a mix that doesn't
// add up is a mistake the grower needs to see and correct, not one the server
// should paper over by silently rescaling their numbers.
func sanitizeMedium(b mediumBody) (components []domain.MediumComponent, amendments []domain.MediumAmendment) {
	for _, c := range b.Components {
		label := strings.TrimSpace(c.Label)
		if label == "" || c.Percent <= 0 {
			continue
		}
		components = append(components, domain.MediumComponent{
			ProductID: strings.TrimSpace(c.ProductID),
			Label:     label,
			Percent:   c.Percent,
			Note:      strings.TrimSpace(c.Note),
		})
	}
	for _, a := range b.Amendments {
		label := strings.TrimSpace(a.Label)
		if label == "" || a.Amount <= 0 {
			continue
		}
		amendments = append(amendments, domain.MediumAmendment{
			ProductID: strings.TrimSpace(a.ProductID),
			Label:     label,
			Amount:    a.Amount,
			Unit:      strings.TrimSpace(a.Unit),
			Note:      strings.TrimSpace(a.Note),
		})
	}
	return components, amendments
}

// mediumSpecies resolves the optional species tag: empty means "any crop", and
// an unknown id is rejected rather than stored as a dangling tag.
func mediumSpecies(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", true
	}
	sp, ok := species.Get(v)
	if !ok {
		return "", false
	}
	return sp.ID, true
}

func (s *Server) createMediumRecipe(w http.ResponseWriter, r *http.Request) {
	var b mediumBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	sp, ok := mediumSpecies(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	components, amendments := sanitizeMedium(b)
	if len(components) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("a medium recipe needs at least one component"))
		return
	}
	rec := domain.MediumRecipe{
		ID:          id(b.Name, "medium"),
		Name:        strings.TrimSpace(b.Name),
		Description: strings.TrimSpace(b.Description),
		Source:      "user",
		Species:     sp,
		Base:        strings.TrimSpace(b.Base),
		Components:  components,
		Amendments:  amendments,
		CreatedAt:   time.Now(),
	}
	if err := s.store.SaveMediumRecipe(rec); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.activity("", "", "info", "configuration", "Created medium recipe "+rec.Name)
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) updateMediumRecipe(w http.ResponseWriter, r *http.Request) {
	rec, ok, err := s.store.MediumRecipe(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, errBody("medium recipe not found"))
		return
	}
	var b mediumBody
	if err := decode(r, &b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(b.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errBody("name is required"))
		return
	}
	sp, ok := mediumSpecies(b.Species)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errBody("species must be one of the predefined crop families"))
		return
	}
	components, amendments := sanitizeMedium(b)
	if len(components) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("a medium recipe needs at least one component"))
		return
	}
	rec.Name = strings.TrimSpace(b.Name)
	rec.Description = strings.TrimSpace(b.Description)
	rec.Species = sp
	rec.Base = strings.TrimSpace(b.Base)
	rec.Components = components
	rec.Amendments = amendments
	if err := s.store.SaveMediumRecipe(rec); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) deleteMediumRecipe(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMediumRecipe(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
