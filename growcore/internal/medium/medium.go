// Package medium is Grow Core's built-in database of growing-medium recipes:
// what a substrate is mixed from, as bulk percentages plus per-litre
// amendments.
//
// Built-in recipes live in the catalog as a single file:
//
//	mediums/mediums.yaml
//
// holding a `recipes:` list. Unlike feeding schedules, medium recipes are not
// filed under a species — a 70/30 coco-perlite mix serves any crop — so they
// sit at the catalog root and carry an optional `species:` tag instead.
//
// These recipes are read-only; users create their own editable copies in-app
// (see store/medium.go). At runtime the loader prefers the on-disk catalog tree
// so edits are live in development, and falls back to the copy embedded into
// the binary, mirroring internal/inventory and internal/species.
package medium

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// file is the on-disk shape of mediums/mediums.yaml.
type file struct {
	Recipes []domain.MediumRecipe `yaml:"recipes"`
}

//go:embed all:data
var data embed.FS

var (
	once   sync.Once
	loaded []domain.MediumRecipe
	byID   map[string]domain.MediumRecipe
)

// All returns every built-in recipe, loaded once, sorted by name.
func All() []domain.MediumRecipe {
	once.Do(load)
	return loaded
}

// Get returns the built-in recipe with the given id, or false if there is none.
func Get(id string) (domain.MediumRecipe, bool) {
	once.Do(load)
	r, ok := byID[strings.TrimSpace(id)]
	return r, ok
}

func load() {
	byID = map[string]domain.MediumRecipe{}
	loaded = []domain.MediumRecipe{}
	raw, err := readSource()
	if err != nil {
		return // no catalog is fine: the user's own recipes still work
	}
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		log.Printf("medium: mediums.yaml: %v", err)
		return
	}
	for _, r := range f.Recipes {
		r.Source = "builtin"
		r.Species = strings.ToLower(strings.TrimSpace(r.Species))
		loaded = append(loaded, r)
		byID[r.ID] = r
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].Name < loaded[j].Name })
}

// readSource reads mediums.yaml from the on-disk catalog when present, else
// from the embedded copy.
func readSource() ([]byte, error) {
	if dir := diskDir(); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, "mediums.yaml")); err == nil {
			return raw, nil
		}
	}
	sub, err := fs.Sub(data, "data")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(sub, "mediums.yaml")
}

// diskDir locates the catalog submodule's mediums/ directory, or "" if absent.
func diskDir() string {
	if d := os.Getenv("GROWCORE_MEDIUMS_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		for _, cand := range []string{filepath.Join(dir, "catalog", "mediums"), filepath.Join(dir, "mediums")} {
			if _, err := os.Stat(filepath.Join(cand, "mediums.yaml")); err == nil {
				return cand
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
