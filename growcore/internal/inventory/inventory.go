// Package inventory is Grow Core's catalog of stock categories.
//
// A category groups the things a grower owns (consumables, plant material,
// equipment, growing supplies, harvest & storage) and declares the schema of
// extra property columns an item in that category carries — the same
// data-driven idea as a species declaring its cultivar attributes (see
// internal/species). The inventory item form renders its extra inputs
// dynamically from the chosen category's columns, and the API drops any column
// key a category does not declare.
//
// Categories are defined as YAML files under the catalog submodule's inventory/
// tree, one per category:
//
//	catalog/inventory/<category-id>/inventory.yaml
//
// The category id is the directory name. At runtime the loader prefers that
// on-disk tree (so edits are live in development), and falls back to the copy
// embedded into the binary — synced from catalog/inventory/ by `make build` —
// so the add-on still ships as one file. This mirrors internal/species.
package inventory

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

// ColumnType is the input kind of an extra item property.
type ColumnType string

const (
	ColText   ColumnType = "text"
	ColNumber ColumnType = "number"
	ColEnum   ColumnType = "enum"
	ColDate   ColumnType = "date"
)

// Column declares one category-specific item field.
type Column struct {
	Key     string     `json:"key" yaml:"key"`
	Label   string     `json:"label" yaml:"label"`
	Type    ColumnType `json:"type" yaml:"type"`
	Options []string   `json:"options,omitempty" yaml:"options,omitempty"`
	Unit    string     `json:"unit,omitempty" yaml:"unit,omitempty"`
}

// Category is a group of stock with a shared set of extra columns. ID is the
// directory name; Label is the display name. Icon is an optional lucide icon
// name the UI may render. Units are suggested measurement units offered in the
// item form's unit picker.
type Category struct {
	ID          string   `json:"id"`
	Label       string   `json:"label" yaml:"label"`
	Description string   `json:"description,omitempty" yaml:"description"`
	Icon        string   `json:"icon,omitempty" yaml:"icon"`
	Order       int      `json:"order" yaml:"order"`
	Units       []string `json:"units,omitempty" yaml:"units"`
	Columns     []Column `json:"columns,omitempty" yaml:"columns"`
}

//go:embed all:data
var data embed.FS

var (
	once   sync.Once
	loaded []Category
	byID   map[string]Category
)

// All returns the category catalog, loaded once, sorted by order then id.
func All() []Category {
	once.Do(load)
	return loaded
}

// Get returns the category with the given id (case-insensitive).
func Get(id string) (Category, bool) {
	once.Do(load)
	c, ok := byID[strings.ToLower(strings.TrimSpace(id))]
	return c, ok
}

// SourceFS returns the file system the catalog is read from: the repo-root
// inventory/ directory when present (so edits are live in development),
// otherwise the tree embedded into the binary. The product-template loader
// (inventory/<category>/products.yaml and its images) reuses this so it
// resolves the same tree without duplicating the embed or discovery logic.
// This mirrors species.SourceFS.
func SourceFS() fs.FS {
	if dir := diskDir(); dir != "" {
		return os.DirFS(dir)
	}
	if sub, err := fs.Sub(data, "data"); err == nil {
		return sub
	}
	return nil
}

func load() {
	byID = map[string]Category{}
	if dir := diskDir(); dir != "" {
		if cats, err := loadTree(os.DirFS(dir)); err != nil {
			log.Printf("inventory: reading %s: %v", dir, err)
		} else if len(cats) > 0 {
			set(cats)
			return
		}
	}
	if sub, err := fs.Sub(data, "data"); err == nil {
		if cats, err := loadTree(sub); err != nil {
			log.Printf("inventory: reading embedded tree: %v", err)
		} else {
			set(cats)
			return
		}
	}
	loaded = []Category{}
}

func set(cats []Category) {
	loaded = cats
	for _, c := range cats {
		byID[c.ID] = c
	}
}

// diskDir locates the catalog submodule's inventory/ directory, or "" if not
// found.
func diskDir() string {
	if d := os.Getenv("GROWCORE_INVENTORY_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		for _, cand := range []string{filepath.Join(dir, "catalog", "inventory"), filepath.Join(dir, "inventory")} {
			if isInventoryDir(cand) {
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

// isInventoryDir reports whether p holds at least one <id>/inventory.yaml.
func isInventoryDir(p string) bool {
	entries, err := os.ReadDir(p)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(p, e.Name(), "inventory.yaml")); err == nil {
				return true
			}
		}
	}
	return false
}

func loadTree(fsys fs.FS) ([]Category, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Category
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := e.Name() + "/inventory.yaml"
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			continue // directory without an inventory.yaml is skipped
		}
		var c Category
		if err := yaml.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		c.ID = e.Name()
		if c.Label == "" {
			c.Label = strings.Title(e.Name())
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
