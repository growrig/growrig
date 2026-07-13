// Package integrations discovers integration bundles and runs configured
// instances. It is intentionally separate from catalog/control: integrations
// connect external services, never physical grow devices.
package integrations

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Bundle struct {
	ID string `json:"id" yaml:"id"`
	// Source is the id of the custom catalog source that contributed this
	// bundle; empty for the built-in catalog.
	Source        string        `json:"source,omitempty" yaml:"-"`
	Name          string        `json:"name" yaml:"name"`
	Version       string        `json:"version" yaml:"version"`
	Category      string        `json:"category" yaml:"category"`
	Description   string        `json:"description" yaml:"description"`
	Capabilities  []string      `json:"capabilities" yaml:"capabilities"`
	Config        []ConfigField `json:"config" yaml:"config"`
	Runtime       RuntimeSpec   `json:"-" yaml:"runtime"`
	Icon          string        `json:"icon,omitempty" yaml:"-"`
	Documentation string        `json:"documentation,omitempty" yaml:"documentation"`
	dir           string
	assetFS       fs.FS
	assetRoot     string
}

// data is populated from the catalog submodule's integrations/ tree by
// `make build`. The placeholder keeps plain development builds valid; those
// load bundles directly from disk instead.
//
//go:embed all:data
var data embed.FS

type ConfigField struct {
	Key         string   `json:"key" yaml:"key"`
	Label       string   `json:"label" yaml:"label"`
	Type        string   `json:"type" yaml:"type"`
	Required    bool     `json:"required" yaml:"required"`
	Secret      bool     `json:"secret" yaml:"secret"`
	Default     string   `json:"default,omitempty" yaml:"default"`
	Placeholder string   `json:"placeholder,omitempty" yaml:"placeholder"`
	Help        string   `json:"help,omitempty" yaml:"help"`
	Options     []string `json:"options,omitempty" yaml:"options"`
}

func loadEmbeddedBundles() ([]Bundle, error) {
	var out []Bundle
	err := fs.WalkDir(data, "data", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "integration.yaml" {
			return nil
		}
		raw, err := fs.ReadFile(data, path)
		if err != nil {
			return err
		}
		var b Bundle
		if err := yaml.Unmarshal(raw, &b); err != nil {
			return fmt.Errorf("parse embedded %s: %w", path, err)
		}
		if b.ID == "" || b.Name == "" || b.Category == "" || len(b.Capabilities) == 0 {
			return fmt.Errorf("embedded %s: invalid bundle", path)
		}
		b.assetFS, b.assetRoot = data, filepath.Dir(path)
		if _, err := fs.Stat(data, b.assetRoot+"/icon.svg"); err == nil {
			b.Icon = "/api/integration-bundles/" + b.ID + "/icon?v=" + b.Version
		}
		out = append(out, b)
		return nil
	})
	return out, err
}

type RuntimeSpec struct {
	Type       string                 `yaml:"type"`
	Handler    string                 `yaml:"handler"`
	Test       *HTTPRequest           `yaml:"test"`
	Operations map[string]HTTPRequest `yaml:"operations"`
}

type HTTPRequest struct {
	URLField string            `yaml:"urlField"`
	Method   string            `yaml:"method"`
	Headers  map[string]string `yaml:"headers"`
	Body     any               `yaml:"body"`
}

func LoadBundles(root string) ([]Bundle, error) {
	var out []Bundle
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "integration.yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var b Bundle
		if err := yaml.Unmarshal(raw, &b); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if b.ID == "" || b.Name == "" || b.Category == "" || len(b.Capabilities) == 0 {
			return fmt.Errorf("%s: id, name, category and capabilities are required", path)
		}
		b.dir = filepath.Dir(path)
		if _, err := os.Stat(filepath.Join(b.dir, "icon.svg")); err == nil {
			b.Icon = "/api/integration-bundles/" + b.ID + "/icon?v=" + b.Version
		}
		if b.Documentation == "" {
			if _, err := os.Stat(filepath.Join(b.dir, "README.md")); err == nil {
				b.Documentation = "README.md"
			}
		}
		out = append(out, b)
		return nil
	})
	if os.IsNotExist(err) {
		return []Bundle{}, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category == out[j].Category {
			return out[i].Name < out[j].Name
		}
		return out[i].Category < out[j].Category
	})
	seen := map[string]bool{}
	for _, b := range out {
		if seen[b.ID] {
			return nil, fmt.Errorf("duplicate integration bundle %q", b.ID)
		}
		seen[b.ID] = true
	}
	return out, nil
}

func FindBundleRoot() string {
	if root := os.Getenv("GROWCORE_INTEGRATIONS_DIR"); root != "" {
		return root
	}
	dir, _ := os.Getwd()
	for i := 0; i < 8; i++ {
		for _, candidate := range []string{filepath.Join(dir, "catalog", "integrations"), filepath.Join(dir, "integrations")} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "integrations"
}

func (b Bundle) hasCapability(cap string) bool {
	for _, c := range b.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
func (b Bundle) field(key string) *ConfigField {
	for i := range b.Config {
		if b.Config[i].Key == key {
			return &b.Config[i]
		}
	}
	return nil
}
func normalizedMethod(method string) string {
	method = strings.ToUpper(method)
	if method == "" {
		return "POST"
	}
	return method
}
