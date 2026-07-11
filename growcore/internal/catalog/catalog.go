// Package catalog is Grow Core's built-in database of supported devices.
//
// The setup wizard uses it so growers pick a product ("Xiaomi LYWSD03MMC",
// "VIPARSPECTRA via Tapo P110") instead of hand-crafting entity bindings. Each
// product declares the bindings it contributes and hints (entity domain,
// device class) the wizard uses to match Home Assistant entities.
//
// Devices are defined as YAML files under the repo-root devices/ tree, one per
// device, grouped by category:
//
//	devices/<category>/<device-id>/device.yaml
//
// The category is the parent directory; the device id is the device directory
// name. At runtime the loader prefers that on-disk tree (so edits are live in
// development), and falls back to the copy embedded into the binary — synced
// from devices/ by `make build` — so the add-on still ships as one file. See
// [Products].
package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"gopkg.in/yaml.v3"
)

type Category string

const (
	CatTent       Category = "tent"
	CatFan        Category = "fan"
	CatController Category = "controller"
	CatLight      Category = "light"
	CatSensor     Category = "sensor"
	CatCamera     Category = "camera"
	CatPlug       Category = "plug"
	CatCombo      Category = "combo" // provides several bindings
)

// categoryOrder is the display order of categories in the catalog listing.
var categoryOrder = []Category{CatTent, CatController, CatFan, CatSensor, CatLight, CatPlug, CatCamera, CatCombo}

// BindingTemplate describes one binding a product contributes.
type BindingTemplate struct {
	Label       string             `json:"label" yaml:"label"`
	Kind        domain.BindingKind `json:"kind" yaml:"kind"`
	Measurement domain.Measurement `json:"measurement,omitempty" yaml:"measurement,omitempty"`
	Role        domain.Role        `json:"role,omitempty" yaml:"role,omitempty"`
	// EntityDomain is the Home Assistant entity domain to look for
	// (sensor, fan, light, switch, camera).
	EntityDomain string `json:"entityDomain" yaml:"entityDomain"`
	// DeviceClass narrows sensor discovery (temperature, humidity, carbon_dioxide).
	DeviceClass string `json:"deviceClass,omitempty" yaml:"deviceClass,omitempty"`
	// Wattage is a light's rated power in watts; 0 means the grower specifies it
	// (e.g. a generic grow light).
	Wattage float64 `json:"wattage,omitempty" yaml:"wattage,omitempty"`
	// RPMEntityDomain requests a separate tachometer entity for this controller channel.
	RPMEntityDomain string `json:"rpmEntityDomain,omitempty" yaml:"rpmEntityDomain,omitempty"`
}

// FanPreset is an optional known set of physical specifications for a generic fan.
type FanPreset struct {
	ID                  string  `json:"id" yaml:"id"`
	Label               string  `json:"label" yaml:"label"`
	SizeMM              int     `json:"sizeMm,omitempty" yaml:"sizeMm,omitempty"`
	MaxRPM              int     `json:"maxRpm,omitempty" yaml:"maxRpm,omitempty"`
	AirflowCFM          float64 `json:"airflowCfm,omitempty" yaml:"airflowCfm,omitempty"`
	StaticPressureMMH2O float64 `json:"staticPressureMmH2O,omitempty" yaml:"staticPressureMmH2O,omitempty"`
	StartingVoltage     float64 `json:"startingVoltage,omitempty" yaml:"startingVoltage,omitempty"`
}

// Product is a catalog entry.
type Product struct {
	ID            string            `json:"id"`
	Brand         string            `json:"brand"`
	Model         string            `json:"model"`
	Category      Category          `json:"category"`
	Connection    string            `json:"connection"`
	Description   string            `json:"description"`
	Version       string            `json:"version"`
	Author        string            `json:"author"`
	HAIntegration string            `json:"haIntegration,omitempty"`
	Documentation string            `json:"documentation,omitempty"`
	Provides      []BindingTemplate `json:"provides,omitempty"`
	MaxChannels   int               `json:"maxChannels,omitempty"`
	FanPresets    []FanPreset       `json:"fanPresets,omitempty"`
}

// deviceFile is the on-disk YAML schema for a single device. Category and id
// come from the directory path, not the file.
type deviceFile struct {
	Brand         string            `yaml:"brand"`
	Model         string            `yaml:"model"`
	Connection    string            `yaml:"connection"`
	Description   string            `yaml:"description"`
	Version       string            `yaml:"version"`
	Author        string            `yaml:"author"`
	HAIntegration string            `yaml:"haIntegration"`
	Documentation string            `yaml:"documentation"`
	Provides      []BindingTemplate `yaml:"provides"`
	MaxChannels   int               `yaml:"maxChannels"`
	FanPresets    []FanPreset       `yaml:"fanPresets"`
}

// data holds the catalog tree synced from repo-root devices/ by `make build`.
// Only a .gitkeep placeholder is committed; a plain `go build` embeds nothing,
// and the loader falls back to the on-disk tree (dev) or returns an empty
// catalog (API-only), mirroring the webui embed.
//
//go:embed all:data
var data embed.FS

var (
	once     sync.Once
	products []Product
)

// Products returns the device catalog, loaded once. It prefers the on-disk
// devices/ tree (found by searching up from the working directory, or via
// GROWCORE_CATALOG_DIR) so edits are live in development, and otherwise reads
// the tree embedded into the binary.
func Products() []Product {
	once.Do(load)
	return products
}

func load() {
	if dir := diskDir(); dir != "" {
		if ps, err := loadTree(os.DirFS(dir)); err != nil {
			log.Printf("catalog: reading %s: %v", dir, err)
		} else if len(ps) > 0 {
			products = ps
			return
		}
	}

	sub, err := fs.Sub(data, "data")
	if err == nil {
		if ps, err := loadTree(sub); err != nil {
			log.Printf("catalog: reading embedded tree: %v", err)
		} else {
			products = ps
			return
		}
	}
	products = []Product{}
}

// diskDir locates the repo-root devices/ directory, or "" if not found.
func diskDir() string {
	if d := os.Getenv("GROWCORE_CATALOG_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	// Walk up from the working directory looking for a devices/ dir that holds
	// category subdirectories.
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "devices")
		if isCatalogDir(cand) {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// isCatalogDir reports whether p looks like the devices/ tree (contains at
// least one known category subdirectory).
func isCatalogDir(p string) bool {
	for _, c := range categoryOrder {
		if fi, err := os.Stat(filepath.Join(p, string(c))); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

func validCategory(c Category) bool {
	for _, k := range categoryOrder {
		if k == c {
			return true
		}
	}
	return false
}

func categoryRank(c Category) int {
	for i, k := range categoryOrder {
		if k == c {
			return i
		}
	}
	return len(categoryOrder)
}

// loadTree reads every category/<device>/device.yaml under fsys.
func loadTree(fsys fs.FS) ([]Product, error) {
	cats, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []Product
	for _, cat := range cats {
		if !cat.IsDir() {
			continue
		}
		category := Category(cat.Name())
		if !validCategory(category) {
			continue
		}
		devs, err := fs.ReadDir(fsys, cat.Name())
		if err != nil {
			return nil, err
		}
		for _, dev := range devs {
			if !dev.IsDir() {
				continue
			}
			path := cat.Name() + "/" + dev.Name() + "/device.yaml"
			raw, err := fs.ReadFile(fsys, path)
			if err != nil {
				log.Printf("catalog: %s: %v", path, err)
				continue
			}
			var df deviceFile
			if err := yaml.Unmarshal(raw, &df); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			out = append(out, Product{
				ID:            dev.Name(),
				Brand:         df.Brand,
				Model:         df.Model,
				Category:      category,
				Connection:    df.Connection,
				Description:   df.Description,
				Version:       defaultString(df.Version, "1.0.0"),
				Author:        defaultString(df.Author, "GrowRig"),
				HAIntegration: df.HAIntegration,
				Documentation: df.Documentation,
				Provides:      df.Provides,
				MaxChannels:   df.MaxChannels,
				FanPresets:    df.FanPresets,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := categoryRank(out[i].Category), categoryRank(out[j].Category); ri != rj {
			return ri < rj
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
