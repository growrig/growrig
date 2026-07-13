// Package species is Grow Core's built-in database of crop families.
//
// A species defines its ordered cultivation stages (with per-stage default
// light hours) and the schema of species-specific attributes a cultivar of
// that species carries (e.g. cannabis genetics / THC / flowering weeks). The
// grow form derives its stage sequence from the chosen species, and the
// cultivar form renders attribute inputs dynamically from the species schema.
//
// Species are defined as YAML files under the catalog submodule's species/
// tree (repo-root catalog/species/), one per species:
//
//	catalog/species/<species-id>/species.yaml
//
// The species id is the directory name. At runtime the loader prefers that
// on-disk tree (so edits are live in development), and falls back to the copy
// embedded into the binary — synced from species/ by `make build` — so the
// add-on still ships as one file. This mirrors internal/catalog.
package species

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AttrType is the input kind of a cultivar attribute.
type AttrType string

const (
	AttrText    AttrType = "text"
	AttrNumber  AttrType = "number"
	AttrPercent AttrType = "percent"
	AttrEnum    AttrType = "enum"
)

// Attribute declares one species-specific cultivar field.
type Attribute struct {
	Key     string   `json:"key" yaml:"key"`
	Label   string   `json:"label" yaml:"label"`
	Type    AttrType `json:"type" yaml:"type"`
	Options []string `json:"options,omitempty" yaml:"options,omitempty"`
	Unit    string   `json:"unit,omitempty" yaml:"unit,omitempty"`
}

// Stage is one cultivation phase with its default daily light hours.
type Stage struct {
	Name       string  `json:"name" yaml:"name"`
	LightHours float64 `json:"lightHours" yaml:"lightHours"`
}

// CareField is a form input a care action may show when logging it. The web
// client renders only the fields a given action declares.
type CareField string

const (
	FieldAmount    CareField = "amount"    // volume per plant (ml)
	FieldRunoff    CareField = "runoff"    // runoff volume / pH
	FieldRecipe    CareField = "recipe"    // nutrient recipe (FeedingRecipe)
	FieldPH        CareField = "ph"        // solution pH
	FieldEC        CareField = "ec"        // solution EC
	FieldNote      CareField = "note"      // free-text note
	FieldPhotos    CareField = "photos"    // photo attachments (UI only for now)
	FieldPotSize   CareField = "potSize"   // new pot size on transplant
	FieldProduct   CareField = "product"   // treatment product
	FieldTrainType CareField = "trainType" // training method
)

// CareAction declares one manual action a grower can log against a grow's
// plants. The set of actions comes from the grow's species (or DefaultCareActions
// when the species declares none), so the log-care menu is crop-appropriate.
type CareAction struct {
	Key    string      `json:"key" yaml:"key"`
	Label  string      `json:"label" yaml:"label"`
	Icon   string      `json:"icon,omitempty" yaml:"icon,omitempty"`
	Fields []CareField `json:"fields" yaml:"fields"`
	Quick  bool        `json:"quick,omitempty" yaml:"quick,omitempty"`
}

// DefaultCareActions is the crop-neutral fallback set used when a species does
// not declare its own careActions. It defines which actions and fields are
// relevant; exact amounts, doses and schedules stay per-grow/per-event.
var DefaultCareActions = []CareAction{
	{Key: "water", Label: "Water", Icon: "droplet", Fields: []CareField{FieldAmount, FieldRunoff, FieldNote}, Quick: true},
	{Key: "feed", Label: "Feed", Icon: "flask-conical", Fields: []CareField{FieldRecipe, FieldAmount, FieldPH, FieldEC, FieldRunoff, FieldNote}, Quick: true},
	{Key: "inspect", Label: "Inspect", Icon: "search", Fields: []CareField{FieldNote, FieldPhotos}, Quick: true},
	{Key: "train", Label: "Train", Icon: "spline", Fields: []CareField{FieldTrainType, FieldNote, FieldPhotos}},
	{Key: "trim", Label: "Trim / prune", Icon: "scissors", Fields: []CareField{FieldNote, FieldPhotos}},
	{Key: "transplant", Label: "Transplant", Icon: "shovel", Fields: []CareField{FieldPotSize, FieldNote}},
	{Key: "treat", Label: "Spray / treat", Icon: "spray-can", Fields: []CareField{FieldProduct, FieldNote}},
	{Key: "flush", Label: "Flush", Icon: "waves", Fields: []CareField{FieldAmount, FieldRunoff, FieldNote}},
	{Key: "harvest", Label: "Harvest", Icon: "sprout", Fields: []CareField{FieldNote}},
	{Key: "custom", Label: "Custom", Icon: "list-plus", Fields: []CareField{FieldNote, FieldPhotos}},
}

// Species is a crop family: an ordered stage sequence plus the cultivar
// attribute schema. ID is the directory name; Label is the display name.
type Species struct {
	ID                 string       `json:"id"`
	Label              string       `json:"label" yaml:"label"`
	Stages             []Stage      `json:"stages" yaml:"stages"`
	CultivarAttributes []Attribute  `json:"cultivarAttributes,omitempty" yaml:"cultivarAttributes"`
	CareActions        []CareAction `json:"careActions,omitempty" yaml:"careActions,omitempty"`
}

// StageNames returns the ordered stage names.
func (s Species) StageNames() []string {
	names := make([]string, len(s.Stages))
	for i, st := range s.Stages {
		names[i] = st.Name
	}
	return names
}

// LightHours maps each stage name to its default daily light hours.
func (s Species) LightHours() map[string]float64 {
	m := make(map[string]float64, len(s.Stages))
	for _, st := range s.Stages {
		m[st.Name] = st.LightHours
	}
	return m
}

//go:embed all:data
var data embed.FS

var (
	once   sync.Once
	loaded []Species
	byID   map[string]Species
)

// All returns the species catalog, loaded once, sorted by id.
func All() []Species {
	once.Do(load)
	return loaded
}

// SourceFS returns the file system the species catalog is read from: the
// repo-root species/ directory when present (so edits are live in development),
// otherwise the tree embedded into the binary. Sibling loaders (e.g. feeding
// schedules under species/<id>/feedings.yaml) reuse this so they resolve the
// same tree without duplicating the embed or the disk-discovery logic.
func SourceFS() fs.FS {
	if dir := diskDir(); dir != "" {
		return os.DirFS(dir)
	}
	if sub, err := fs.Sub(data, "data"); err == nil {
		return sub
	}
	return nil
}

// Get returns the species with the given id (case-insensitive).
func Get(id string) (Species, bool) {
	once.Do(load)
	s, ok := byID[strings.ToLower(strings.TrimSpace(id))]
	return s, ok
}

// StageNames returns a species' ordered stage sequence.
func StageNames(id string) ([]string, bool) {
	s, ok := Get(id)
	if !ok {
		return nil, false
	}
	return s.StageNames(), true
}

// StagePresets returns the legacy id -> stage-names map (GET /api/stage-presets).
func StagePresets() map[string][]string {
	out := map[string][]string{}
	for _, s := range All() {
		out[s.ID] = s.StageNames()
	}
	return out
}

func load() {
	byID = map[string]Species{}
	if dir := diskDir(); dir != "" {
		if sp, err := loadTree(os.DirFS(dir)); err != nil {
			log.Printf("species: reading %s: %v", dir, err)
		} else if len(sp) > 0 {
			set(sp)
			return
		}
	}
	if sub, err := fs.Sub(data, "data"); err == nil {
		if sp, err := loadTree(sub); err != nil {
			log.Printf("species: reading embedded tree: %v", err)
		} else {
			set(sp)
			return
		}
	}
	loaded = []Species{}
}

func set(sp []Species) {
	for i := range sp {
		// A species that declares no care actions inherits the crop-neutral
		// defaults, so every species carries a usable action set to the client.
		if len(sp[i].CareActions) == 0 {
			sp[i].CareActions = DefaultCareActions
		}
	}
	loaded = sp
	for _, s := range sp {
		byID[s.ID] = s
	}
}

// CareActionsFor returns the care actions available for a species (its own, or
// the defaults), and whether the species is known.
func CareActionsFor(id string) ([]CareAction, bool) {
	s, ok := Get(id)
	if !ok {
		return DefaultCareActions, false
	}
	if len(s.CareActions) == 0 {
		return DefaultCareActions, true
	}
	return s.CareActions, true
}

// CareAction returns the definition of one action key for a species.
func CareActionFor(speciesID, key string) (CareAction, bool) {
	actions, _ := CareActionsFor(speciesID)
	for _, a := range actions {
		if a.Key == key {
			return a, true
		}
	}
	return CareAction{}, false
}

// diskDir locates the repo-root species/ directory, or "" if not found.
func diskDir() string {
	if d := os.Getenv("GROWCORE_SPECIES_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		for _, cand := range []string{filepath.Join(dir, "catalog", "species"), filepath.Join(dir, "species")} {
			if isSpeciesDir(cand) {
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

// isSpeciesDir reports whether p holds at least one <id>/species.yaml.
func isSpeciesDir(p string) bool {
	entries, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(p, e.Name(), "species.yaml")); err == nil {
				return true
			}
		}
	}
	return false
}

func loadTree(fsys fs.FS) ([]Species, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Species
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := e.Name() + "/species.yaml"
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			continue // directory without a species.yaml is skipped
		}
		var sp Species
		if err := yaml.Unmarshal(raw, &sp); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		sp.ID = e.Name()
		if sp.Label == "" {
			sp.Label = strings.Title(e.Name())
		}
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
