package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// User medium recipes are stored as one YAML file per recipe under
// <dataDir>/mediums/<id>.yaml — portable, hand-editable and versionable, the
// same way user feeding recipes persist (see feeding.go). Built-in recipes are
// a separate read-only catalog (see internal/medium) used as templates.

// mediumDoc is the on-disk shape of a user recipe file.
type mediumDoc struct {
	Version     int                      `yaml:"version"`
	ID          string                   `yaml:"id"`
	Name        string                   `yaml:"name"`
	Description string                   `yaml:"description,omitempty"`
	Species     string                   `yaml:"species,omitempty"`
	Base        string                   `yaml:"base,omitempty"`
	CreatedAt   time.Time                `yaml:"createdAt"`
	Components  []domain.MediumComponent `yaml:"components"`
	Amendments  []domain.MediumAmendment `yaml:"amendments,omitempty"`
}

func (d mediumDoc) toRecipe() domain.MediumRecipe {
	return domain.MediumRecipe{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Source:      "user",
		Species:     d.Species,
		Base:        d.Base,
		Components:  d.Components,
		Amendments:  d.Amendments,
		CreatedAt:   d.CreatedAt,
	}
}

func (s *Store) mediumPath(id string) string {
	return filepath.Join(s.mediumDir, id+".yaml")
}

// MediumRecipes returns all user medium recipes, newest first.
func (s *Store) MediumRecipes() ([]domain.MediumRecipe, error) {
	paths, err := filepath.Glob(filepath.Join(s.mediumDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	out := make([]domain.MediumRecipe, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc mediumDoc
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			continue // skip a corrupt file rather than fail the whole list
		}
		if doc.ID == "" {
			// Fall back to the filename stem so hand-created files still load.
			doc.ID = filepath.Base(p[:len(p)-len(".yaml")])
		}
		out = append(out, doc.toRecipe())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// MediumRecipe returns one user recipe by id.
func (s *Store) MediumRecipe(id string) (domain.MediumRecipe, bool, error) {
	raw, err := os.ReadFile(s.mediumPath(id))
	if os.IsNotExist(err) {
		return domain.MediumRecipe{}, false, nil
	}
	if err != nil {
		return domain.MediumRecipe{}, false, err
	}
	var doc mediumDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return domain.MediumRecipe{}, false, fmt.Errorf("invalid medium recipe YAML: %w", err)
	}
	if doc.ID == "" {
		doc.ID = id
	}
	return doc.toRecipe(), true, nil
}

// SaveMediumRecipe writes a user recipe atomically to its YAML file.
func (s *Store) SaveMediumRecipe(r domain.MediumRecipe) error {
	if r.ID == "" {
		return fmt.Errorf("recipe id is required")
	}
	doc := mediumDoc{
		Version:     1,
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Species:     r.Species,
		Base:        r.Base,
		CreatedAt:   r.CreatedAt,
		Components:  r.Components,
		Amendments:  r.Amendments,
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.mediumDir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(s.mediumDir, "."+r.ID+".yaml.tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.mediumPath(r.ID))
}

// DeleteMediumRecipe removes a user recipe file.
func (s *Store) DeleteMediumRecipe(id string) error {
	err := os.Remove(s.mediumPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
