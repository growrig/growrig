package catalogsource

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// schemaData holds the catalog JSON Schemas synced from the repo-root
// schema/catalog/ tree by `make schema-embed`, so a shipped binary can validate
// catalogs offline. Only a .gitkeep is committed; a plain `go build` embeds
// nothing and the loader falls back to the on-disk schema/catalog/ tree (found
// by walking up from the working directory), which covers development and tests.
//
//go:embed schema
var schemaData embed.FS

// schemaNames are the logical schemas, each backing one catalog file kind.
var schemaNames = []string{
	"manifest", "device", "integration", "species", "feedings", "inventory", "products", "vendor",
	"mediums",
}

type schemaSet struct {
	compiled map[string]*jsonschema.Schema
	err      error
}

var (
	schemasOnce sync.Once
	schemas     *schemaSet
)

// loadSchemas compiles the catalog schemas once. A non-nil err means no schemas
// were available (e.g. an API-only build without `make schema-embed`); callers
// treat that as "validation unavailable" rather than a hard failure.
func loadSchemas() *schemaSet {
	schemasOnce.Do(func() {
		s := &schemaSet{compiled: map[string]*jsonschema.Schema{}}
		fsys, err := schemaFS()
		if err != nil {
			s.err = err
			schemas = s
			return
		}
		c := jsonschema.NewCompiler()
		for _, name := range schemaNames {
			raw, err := fs.ReadFile(fsys, name+".schema.yaml")
			if err != nil {
				s.err = fmt.Errorf("read schema %q: %w", name, err)
				schemas = s
				return
			}
			doc, err := yamlToJSON(raw)
			if err != nil {
				s.err = fmt.Errorf("parse schema %q: %w", name, err)
				schemas = s
				return
			}
			if err := c.AddResource(schemaURL(name), doc); err != nil {
				s.err = fmt.Errorf("add schema %q: %w", name, err)
				schemas = s
				return
			}
		}
		for _, name := range schemaNames {
			sch, err := c.Compile(schemaURL(name))
			if err != nil {
				s.err = fmt.Errorf("compile schema %q: %w", name, err)
				schemas = s
				return
			}
			s.compiled[name] = sch
		}
		schemas = s
	})
	return schemas
}

func schemaURL(name string) string {
	return "https://growrig.dev/schema/catalog/" + name + ".json"
}

// schemaFS resolves the catalog schema directory, preferring the on-disk
// repo-root schema/catalog/ tree (so edits are live in development and tests
// work without a build step) and falling back to the embedded copy.
func schemaFS() (fs.FS, error) {
	if dir := schemaDiskDir(); dir != "" {
		return os.DirFS(dir), nil
	}
	sub, err := fs.Sub(schemaData, "schema")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "manifest.schema.yaml"); err != nil {
		return nil, fmt.Errorf("no catalog schemas available (run `make schema-embed`)")
	}
	return sub, nil
}

func schemaDiskDir() string {
	if d := os.Getenv("GROWCORE_SCHEMA_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "schema", "catalog")
		if _, err := os.Stat(filepath.Join(cand, "manifest.schema.yaml")); err == nil {
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

// validateCatalogContent validates a staged catalog's manifest and every
// content file it provides against the catalog schemas. It returns the first
// validation error, identifying the offending file by its path within the
// package. When schemas are unavailable it logs and skips (no hard failure).
func validateCatalogContent(root string, provides []string) error {
	set := loadSchemas()
	if set.err != nil {
		log.Printf("catalogsource: schema validation skipped: %v", set.err)
		return nil
	}

	if err := validateFile(set, "manifest", root, filepath.Join(root, "catalog.yaml")); err != nil {
		return err
	}

	for _, kind := range provides {
		var err error
		switch kind {
		case "devices":
			err = validateGlob(set, "device", root, "devices", "*", "*", "device.yaml")
		case "integrations":
			err = validateGlob(set, "integration", root, "integrations", "*", "*", "integration.yaml")
		case "species":
			if err = validateGlob(set, "species", root, "species", "*", "species.yaml"); err == nil {
				err = validateGlob(set, "feedings", root, "species", "*", "feedings.yaml")
			}
		case "inventory":
			if err = validateGlob(set, "inventory", root, "inventory", "*", "inventory.yaml"); err == nil {
				err = validateGlob(set, "products", root, "inventory", "*", "products.yaml")
			}
		case "vendors":
			err = validateGlob(set, "vendor", root, "vendors", "*", "vendor.yaml")
		case "mediums":
			err = validateGlob(set, "mediums", root, "mediums", "mediums.yaml")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// validateGlob validates every file matching root/<parts...> against a schema.
// Optional files (feedings.yaml, products.yaml) simply produce no matches.
func validateGlob(set *schemaSet, schemaName, root string, parts ...string) error {
	matches, err := filepath.Glob(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := validateFile(set, schemaName, root, path); err != nil {
			return err
		}
	}
	return nil
}

func validateFile(set *schemaSet, schemaName, root, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	inst, err := yamlToJSON(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", relPath(root, path), err)
	}
	if err := set.compiled[schemaName].Validate(inst); err != nil {
		return fmt.Errorf("%s does not match the %s schema: %w", relPath(root, path), schemaName, err)
	}
	return nil
}

func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return filepath.Base(path)
}

// yamlToJSON decodes YAML and normalizes it to JSON-native types (map[string]any,
// []any, float64, string, bool, nil) so the JSON Schema validator sees the same
// value shapes it would for JSON input.
func yamlToJSON(raw []byte) (any, error) {
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}
