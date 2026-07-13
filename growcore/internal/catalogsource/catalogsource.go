// Package catalogsource manages user-added catalog repositories.
//
// GrowRig ships a default catalog (the catalog/ submodule, see
// github.com/growrig/growrig-catalog), but growers can register additional
// GitHub repositories that follow the same layout to add their own devices or
// integrations without forking the platform. A catalog repository is
// identified by a catalog.yaml manifest at its root:
//
//	manifest: 1
//	id: my-catalog
//	name: My Catalog
//	provides: [devices, integrations]
//
// Each entry in provides names a top-level directory using the standard
// catalog layout (devices/<category>/<id>/device.yaml, …). Sources are
// fetched as GitHub tarballs — no git binary required — extracted under the
// storage directory (catalog-cache/<id>/), and recorded in
// catalog-sources.yaml beside the database so they survive restarts. After
// any change the manager fires its Apply hook so the in-memory catalogs
// reload.
package catalogsource

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Kinds are the content directories a catalog manifest may provide. Devices
// and integrations are merged into the running catalogs today; the remaining
// kinds are accepted (and fetched) so manifests stay forward-compatible.
var Kinds = []string{"devices", "integrations", "species", "inventory", "vendors"}

// MergedKinds are the kinds growcore currently merges from custom sources.
var MergedKinds = []string{"devices", "integrations"}

// Manifest is the catalog.yaml every catalog repository carries at its root.
type Manifest struct {
	Manifest    int      `json:"manifest" yaml:"manifest"`
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description"`
	Maintainer  string   `json:"maintainer,omitempty" yaml:"maintainer"`
	Homepage    string   `json:"homepage,omitempty" yaml:"homepage"`
	Provides    []string `json:"provides" yaml:"provides"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (m Manifest) validate() error {
	if m.Manifest != 1 {
		return fmt.Errorf("unsupported manifest version %d (expected 1)", m.Manifest)
	}
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("manifest id %q must be lowercase letters, digits and hyphens", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest name is required")
	}
	if len(m.Provides) == 0 {
		return fmt.Errorf("manifest provides at least one content kind (%s)", strings.Join(Kinds, ", "))
	}
	for _, p := range m.Provides {
		if !isKind(p) {
			return fmt.Errorf("manifest provides unknown kind %q (known: %s)", p, strings.Join(Kinds, ", "))
		}
	}
	return nil
}

func isKind(k string) bool {
	for _, known := range Kinds {
		if known == k {
			return true
		}
	}
	return false
}

// Source is one registered catalog repository.
type Source struct {
	ID          string    `json:"id" yaml:"id"`
	Repo        string    `json:"repo" yaml:"repo"` // owner/name on GitHub
	Ref         string    `json:"ref,omitempty" yaml:"ref,omitempty"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description,omitempty" yaml:"description,omitempty"`
	Maintainer  string    `json:"maintainer,omitempty" yaml:"maintainer,omitempty"`
	Homepage    string    `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Provides    []string  `json:"provides" yaml:"provides"`
	AddedAt     time.Time `json:"addedAt" yaml:"addedAt"`
	FetchedAt   time.Time `json:"fetchedAt" yaml:"fetchedAt"`
}

// ExtraDir is one content root a source contributes for a given kind.
type ExtraDir struct {
	SourceID string
	Dir      string
}

// Manager owns the registered sources, their on-disk caches and persistence.
type Manager struct {
	mu       sync.Mutex
	file     string // catalog-sources.yaml
	cacheDir string // catalog-cache/
	sources  []Source

	// Apply is invoked (without the manager lock held) after every mutation
	// so the process can reload the affected catalogs. Set once at startup.
	Apply func()
}

type sourcesFile struct {
	Sources []Source `yaml:"sources"`
}

// New loads the persisted source list from storageDir. Caches are used as-is;
// nothing is fetched at startup, so growcore boots offline.
func New(storageDir string) (*Manager, error) {
	m := &Manager{
		file:     filepath.Join(storageDir, "catalog-sources.yaml"),
		cacheDir: filepath.Join(storageDir, "catalog-cache"),
	}
	raw, err := os.ReadFile(m.file)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	var f sourcesFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", m.file, err)
	}
	seen := make(map[string]bool, len(f.Sources))
	for _, source := range f.Sources {
		if err := validateSource(source); err != nil {
			return nil, fmt.Errorf("parse %s: %w", m.file, err)
		}
		if seen[source.ID] {
			return nil, fmt.Errorf("parse %s: duplicate source id %q", m.file, source.ID)
		}
		seen[source.ID] = true
	}
	m.sources = f.Sources
	return m, nil
}

func validateSource(source Source) error {
	if !idPattern.MatchString(source.ID) {
		return fmt.Errorf("source id %q must be lowercase letters, digits and hyphens", source.ID)
	}
	if _, _, err := parseRepo(source.Repo); err != nil {
		return err
	}
	if strings.TrimSpace(source.Name) == "" {
		return fmt.Errorf("source %q has no name", source.ID)
	}
	if len(source.Provides) == 0 {
		return fmt.Errorf("source %q provides no content", source.ID)
	}
	for _, kind := range source.Provides {
		if !isKind(kind) {
			return fmt.Errorf("source %q provides unknown kind %q", source.ID, kind)
		}
	}
	return nil
}

// List returns the registered sources sorted by name.
func (m *Manager) List() []Source {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Source, len(m.sources))
	copy(out, m.sources)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Dirs returns the content roots registered sources contribute for kind
// (e.g. "devices"), in registration order, limited to sources whose manifest
// provides that kind and whose cache actually holds the directory.
func (m *Manager) Dirs(kind string) []ExtraDir {
	if !isKind(kind) {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ExtraDir
	for _, s := range m.sources {
		if !contains(s.Provides, kind) {
			continue
		}
		dir := filepath.Join(m.cacheDir, s.ID, kind)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			out = append(out, ExtraDir{SourceID: s.ID, Dir: dir})
		}
	}
	return out
}

// Add fetches a GitHub repository, validates its manifest and registers it.
// repo accepts "owner/name", "github.com/owner/name" or a full GitHub URL;
// ref is a branch, tag or commit (empty means the default branch).
func (m *Manager) Add(repo, ref string) (Source, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return Source{}, err
	}
	man, dir, err := m.fetch(owner, name, ref)
	if err != nil {
		return Source{}, err
	}
	m.mu.Lock()
	for _, s := range m.sources {
		if s.ID == man.ID {
			m.mu.Unlock()
			_ = os.RemoveAll(dir)
			return Source{}, fmt.Errorf("a catalog with id %q is already registered (from %s)", man.ID, s.Repo)
		}
	}
	if err := m.install(man.ID, dir); err != nil {
		m.mu.Unlock()
		return Source{}, err
	}
	now := time.Now().UTC()
	src := Source{
		ID:          man.ID,
		Repo:        owner + "/" + name,
		Ref:         ref,
		Name:        man.Name,
		Description: man.Description,
		Maintainer:  man.Maintainer,
		Homepage:    man.Homepage,
		Provides:    man.Provides,
		AddedAt:     now,
		FetchedAt:   now,
	}
	m.sources = append(m.sources, src)
	err = m.save()
	m.mu.Unlock()
	if err != nil {
		return Source{}, err
	}
	m.apply()
	return src, nil
}

// Refresh re-fetches an existing source. The manifest id must not change.
func (m *Manager) Refresh(id string) (Source, error) {
	m.mu.Lock()
	idx := m.index(id)
	if idx < 0 {
		m.mu.Unlock()
		return Source{}, ErrNotFound
	}
	src := m.sources[idx]
	m.mu.Unlock()

	owner, name, err := parseRepo(src.Repo)
	if err != nil {
		return Source{}, err
	}
	man, dir, err := m.fetch(owner, name, src.Ref)
	if err != nil {
		return Source{}, err
	}
	if man.ID != id {
		_ = os.RemoveAll(dir)
		return Source{}, fmt.Errorf("manifest id changed from %q to %q; remove and re-add the source", id, man.ID)
	}

	m.mu.Lock()
	if idx = m.index(id); idx < 0 { // removed while fetching
		m.mu.Unlock()
		_ = os.RemoveAll(dir)
		return Source{}, ErrNotFound
	}
	if err := m.install(id, dir); err != nil {
		m.mu.Unlock()
		return Source{}, err
	}
	m.sources[idx].Name = man.Name
	m.sources[idx].Description = man.Description
	m.sources[idx].Maintainer = man.Maintainer
	m.sources[idx].Homepage = man.Homepage
	m.sources[idx].Provides = man.Provides
	m.sources[idx].FetchedAt = time.Now().UTC()
	src = m.sources[idx]
	err = m.save()
	m.mu.Unlock()
	if err != nil {
		return Source{}, err
	}
	m.apply()
	return src, nil
}

// Remove unregisters a source and deletes its cache.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	idx := m.index(id)
	if idx < 0 {
		m.mu.Unlock()
		return ErrNotFound
	}
	m.sources = append(m.sources[:idx], m.sources[idx+1:]...)
	err := m.save()
	if err == nil {
		err = os.RemoveAll(filepath.Join(m.cacheDir, id))
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.apply()
	return nil
}

// ErrNotFound is returned for operations on an unknown source id.
var ErrNotFound = fmt.Errorf("catalog source not found")

func (m *Manager) index(id string) int {
	for i, s := range m.sources {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// install moves a freshly extracted tree into the cache slot for id,
// replacing any previous fetch. Caller holds m.mu.
func (m *Manager) install(id, dir string) error {
	dst := filepath.Join(m.cacheDir, id)
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(dir, dst)
}

func (m *Manager) save() error {
	raw, err := yaml.Marshal(sourcesFile{Sources: m.sources})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.file, raw, 0o644)
}

func (m *Manager) apply() {
	if m.Apply != nil {
		m.Apply()
	}
}

// parseRepo normalizes "owner/name", "github.com/owner/name" or a full
// GitHub URL into its owner and name.
func parseRepo(in string) (owner, name string, err error) {
	s := strings.TrimSpace(in)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "git@github.com:")
	s = strings.TrimPrefix(s, "github.com/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository %q: expected owner/name or a GitHub URL", in)
	}
	for _, p := range parts {
		if strings.ContainsAny(p, " \t?#&") {
			return "", "", fmt.Errorf("repository %q: expected owner/name or a GitHub URL", in)
		}
	}
	return parts[0], parts[1], nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
