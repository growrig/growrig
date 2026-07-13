// Package catalog is Grow Core's built-in database of supported devices.
//
// The setup wizard uses it so growers pick a product ("Xiaomi LYWSD03MMC",
// "VIPARSPECTRA via Tapo P110") instead of hand-crafting entity bindings. Each
// product declares the bindings it contributes and hints (entity domain,
// device class) the wizard uses to match Home Assistant entities.
//
// Devices are defined as YAML files under the catalog submodule's devices/
// tree (repo-root catalog/devices/), one per device, grouped by category:
//
//	catalog/devices/<category>/<device-id>/device.yaml
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

// Variant is a concrete product supported by a driver (a device.yaml).
// Selecting one during setup pre-fills its physical specs. Specs is a free-form
// numeric map so it works across categories — e.g. sizeMm / maxRpm / airflowCfm
// for fans, or widthCm / depthCm / heightCm for tents. A driver with no variants
// is itself the single product.
type Variant struct {
	ID          string             `json:"id" yaml:"id"`
	Brand       string             `json:"brand,omitempty" yaml:"brand,omitempty"`
	Vendor      string             `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	Group       string             `json:"group,omitempty" yaml:"group,omitempty"`
	Model       string             `json:"model,omitempty" yaml:"model,omitempty"`
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	Image       string             `json:"image,omitempty" yaml:"-"`
	Images      []ProductImage     `json:"images,omitempty" yaml:"images,omitempty"`
	Specs       map[string]float64 `json:"specs,omitempty" yaml:"specs,omitempty"`
	Models      []Variant          `json:"models,omitempty" yaml:"models,omitempty"`
}

type ProductImage struct {
	Src   string `json:"src" yaml:"src"`
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
}

// Product is a catalog entry — a driver that GrowRig can bind to. It may list
// the concrete Products (variants) it supports.
type Product struct {
	ID string `json:"id"`
	// Source is the id of the custom catalog source that contributed this
	// product; empty for the built-in catalog.
	Source        string            `json:"source,omitempty"`
	Brand         string            `json:"brand"`
	Vendor        string            `json:"vendor,omitempty"`
	Model         string            `json:"model"`
	Image         string            `json:"image,omitempty"`
	Category      Category          `json:"category"`
	Connection    string            `json:"connection"`
	Description   string            `json:"description"`
	Version       string            `json:"version"`
	Author        string            `json:"author"`
	HAIntegration string            `json:"haIntegration,omitempty"`
	Documentation string            `json:"documentation,omitempty"`
	Provides      []BindingTemplate `json:"provides,omitempty"`
	MaxChannels   int               `json:"maxChannels,omitempty"`
	Products      []Variant         `json:"products,omitempty"`
	FanType       string            `json:"fanType,omitempty"`
}

// deviceFile is the on-disk YAML schema for a single device. Category and id
// come from the directory path, not the file.
type deviceFile struct {
	Brand         string            `yaml:"brand"`
	Vendor        string            `yaml:"vendor"`
	Model         string            `yaml:"model"`
	Connection    string            `yaml:"connection"`
	Description   string            `yaml:"description"`
	Version       string            `yaml:"version"`
	Author        string            `yaml:"author"`
	HAIntegration string            `yaml:"haIntegration"`
	Documentation string            `yaml:"documentation"`
	Provides      []BindingTemplate `yaml:"provides"`
	MaxChannels   int               `yaml:"maxChannels"`
	Products      []Variant         `yaml:"products"`
	FanType       string            `yaml:"fanType"`
}

// data holds the catalog tree synced from repo-root devices/ by `make build`.
// Only a .gitkeep placeholder is committed; a plain `go build` embeds nothing,
// and the loader falls back to the on-disk tree (dev) or returns an empty
// catalog (API-only), mirroring the webui embed.
//
//go:embed all:data
var data embed.FS

// ExtraDir is an additional devices tree contributed by a custom catalog
// source (see internal/catalogsource).
type ExtraDir struct {
	SourceID string
	Dir      string
}

var (
	mu        sync.Mutex
	loaded    bool
	products  []Product
	extraDirs []ExtraDir
)

// Products returns the device catalog, loaded on first use. It prefers the
// on-disk catalog/devices/ tree (found by searching up from the working
// directory, or via GROWCORE_CATALOG_DIR) so edits are live in development,
// and otherwise reads the tree embedded into the binary. Devices from
// registered custom catalog sources are merged on top.
func Products() []Product {
	mu.Lock()
	defer mu.Unlock()
	if !loaded {
		load()
		loaded = true
	}
	return products
}

// SetExtraDirs registers devices trees from custom catalog sources and
// reloads the catalog. A product with the same category and id as an earlier
// one overrides it (built-in catalog first, then sources in order).
func SetExtraDirs(dirs []ExtraDir) {
	mu.Lock()
	defer mu.Unlock()
	extraDirs = dirs
	load()
	loaded = true
}

func load() {
	var base []Product
	if dir := diskDir(); dir != "" {
		if ps, err := loadTree(os.DirFS(dir), ""); err != nil {
			log.Printf("catalog: reading %s: %v", dir, err)
		} else if len(ps) > 0 {
			base = ps
		}
	}
	if base == nil {
		if sub, err := fs.Sub(data, "data"); err == nil {
			if ps, err := loadTree(sub, ""); err != nil {
				log.Printf("catalog: reading embedded tree: %v", err)
			} else {
				base = ps
			}
		}
	}

	index := map[string]int{}
	for i, p := range base {
		index[string(p.Category)+"/"+p.ID] = i
	}
	for _, extra := range extraDirs {
		ps, err := loadTree(os.DirFS(extra.Dir), extra.SourceID)
		if err != nil {
			log.Printf("catalog: reading source %s (%s): %v", extra.SourceID, extra.Dir, err)
			continue
		}
		for _, p := range ps {
			key := string(p.Category) + "/" + p.ID
			if at, ok := index[key]; ok {
				base[at] = p
			} else {
				index[key] = len(base)
				base = append(base, p)
			}
		}
	}
	sort.Slice(base, func(i, j int) bool {
		if ri, rj := categoryRank(base[i].Category), categoryRank(base[j].Category); ri != rj {
			return ri < rj
		}
		return base[i].ID < base[j].ID
	})
	if base == nil {
		base = []Product{}
	}
	products = base
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
	// Walk up from the working directory looking for the catalog submodule's
	// devices/ dir (or a bare devices/ tree) holding category subdirectories.
	for i := 0; i < 8; i++ {
		for _, cand := range []string{filepath.Join(dir, "catalog", "devices"), filepath.Join(dir, "devices")} {
			if isCatalogDir(cand) {
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

// loadTree reads every category/<device>/device.yaml under fsys, tagging
// each product with the custom-source id (empty for the built-in catalog).
func loadTree(fsys fs.FS, source string) ([]Product, error) {
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
			assetURL := func(name string) string {
				if name == "" {
					return ""
				}
				return "/api/catalog/assets/" + cat.Name() + "/" + dev.Name() + "/" + filepath.Base(name)
			}
			var concrete []Variant
			for _, item := range df.Products {
				if len(item.Models) > 0 {
					for _, model := range item.Models {
						if model.Brand == "" {
							model.Brand = item.Brand
						}
						if model.Vendor == "" {
							model.Vendor = item.Vendor
						}
						for _, image := range item.Images {
							if model.Image == "" && (image.Model == "" || image.Model == model.ID) {
								model.Image = image.Src
							}
						}
						concrete = append(concrete, model)
					}
				} else {
					concrete = append(concrete, item)
				}
			}
			for i := range concrete {
				for _, image := range concrete[i].Images {
					if concrete[i].Image == "" && (image.Model == "" || image.Model == concrete[i].ID) {
						concrete[i].Image = image.Src
					}
				}
				concrete[i].Image = assetURL(concrete[i].Image)
				for j := range concrete[i].Images {
					concrete[i].Images[j].Src = assetURL(concrete[i].Images[j].Src)
				}
			}
			out = append(out, Product{
				ID:            dev.Name(),
				Source:        source,
				Brand:         df.Brand,
				Vendor:        df.Vendor,
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
				Products:      concrete,
				FanType:       df.FanType,
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

// DeviceAsset reads a catalogue image from a custom source's tree, the
// on-disk built-in tree or the embedded device tree. Custom sources are
// searched newest-registered first so an overriding device also serves its
// own images.
func DeviceAsset(category, device, name string) ([]byte, error) {
	if filepath.Base(category) != category || filepath.Base(device) != device || filepath.Base(name) != name {
		return nil, fs.ErrNotExist
	}
	path := category + "/" + device + "/" + name
	mu.Lock()
	extras := make([]ExtraDir, len(extraDirs))
	copy(extras, extraDirs)
	mu.Unlock()
	for i := len(extras) - 1; i >= 0; i-- {
		if raw, err := os.ReadFile(filepath.Join(extras[i].Dir, path)); err == nil {
			return raw, nil
		}
	}
	if dir := diskDir(); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, path)); err == nil {
			return raw, nil
		}
	}
	sub, err := fs.Sub(data, "data")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(sub, path)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
