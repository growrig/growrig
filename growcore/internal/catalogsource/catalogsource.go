// Package catalogsource manages user-added catalog repositories.
//
// GrowRig ships a default catalog (the catalog/ submodule, see
// github.com/growrig/growrig-catalog), but growers can register additional
// public repositories that follow the same layout to add their own devices or
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
// fetched through predefined provider source-archive endpoints — no git binary
// required — extracted under the storage directory (catalog-cache/<id>/), and
// recorded in catalog-sources.yaml beside the database so they survive
// restarts. After any change the manager fires its Apply hook so the in-memory
// catalogs reload.
package catalogsource

import (
	"fmt"
	"net/url"
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
var Kinds = []string{"devices", "integrations", "species", "inventory", "vendors", "mediums"}

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

// Source is one registered catalog package.
type Source struct {
	ID          string    `json:"id" yaml:"id"`
	Repository  string    `json:"repository" yaml:"repository"`
	Provider    string    `json:"provider" yaml:"provider"`
	Ref         string    `json:"ref,omitempty" yaml:"ref,omitempty"`
	ArchiveURL  string    `json:"-" yaml:"archiveUrl"`
	Repo        string    `json:"-" yaml:"repo,omitempty"` // legacy GitHub owner/name
	URL         string    `json:"-" yaml:"url,omitempty"`  // legacy direct archive URL
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
	for i := range f.Sources {
		source := &f.Sources[i]
		if source.Repository == "" && source.Repo != "" {
			repository := source.Repo
			if !strings.Contains(repository, "://") {
				repository = "https://github.com/" + strings.Trim(repository, "/")
			}
			spec, err := resolveRepository(repository, source.Ref)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", m.file, err)
			}
			source.Repository = spec.Repository
			source.Provider = spec.Provider
			source.ArchiveURL = spec.ArchiveURL
			source.Repo = ""
		} else if source.Repository == "" && source.URL != "" {
			// Transitional builds accepted direct archive URLs. Keep those saved
			// sources refreshable, while all newly-added sources use repositories.
			source.Repository = source.URL
			source.Provider = "archive"
			source.ArchiveURL = source.URL
			source.URL = ""
		} else if source.Provider != "archive" {
			spec, err := resolveRepository(source.Repository, source.Ref)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", m.file, err)
			}
			source.Repository = spec.Repository
			source.Provider = spec.Provider
			source.ArchiveURL = spec.ArchiveURL
		}
		if err := validateSource(*source); err != nil {
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
	if source.Provider == "archive" {
		if _, err := normalizeArchiveURL(source.ArchiveURL); err != nil {
			return err
		}
	} else if _, err := resolveRepository(source.Repository, source.Ref); err != nil {
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

// Add resolves a supported public repository URL to its provider's source
// archive endpoint, validates the catalog manifest and registers it.
func (m *Manager) Add(repository, ref string) (Source, error) {
	spec, err := resolveRepository(repository, ref)
	if err != nil {
		return Source{}, err
	}
	man, dir, err := m.fetch(spec.ArchiveURL)
	if err != nil {
		return Source{}, err
	}
	m.mu.Lock()
	for _, s := range m.sources {
		if s.ID == man.ID {
			m.mu.Unlock()
			_ = os.RemoveAll(dir)
			return Source{}, fmt.Errorf("a catalog with id %q is already registered (from %s)", man.ID, s.Repository)
		}
	}
	if err := m.install(man.ID, dir); err != nil {
		m.mu.Unlock()
		_ = os.RemoveAll(dir)
		return Source{}, err
	}
	now := time.Now().UTC()
	src := Source{
		ID:          man.ID,
		Repository:  spec.Repository,
		Provider:    spec.Provider,
		Ref:         strings.TrimSpace(ref),
		ArchiveURL:  spec.ArchiveURL,
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

	archiveURL := src.ArchiveURL
	if src.Provider != "archive" {
		spec, err := resolveRepository(src.Repository, src.Ref)
		if err != nil {
			return Source{}, err
		}
		archiveURL = spec.ArchiveURL
	}
	man, dir, err := m.fetch(archiveURL)
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
		_ = os.RemoveAll(dir)
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

func normalizeArchiveURL(in string) (string, error) {
	raw := strings.TrimSpace(in)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("archive URL %q: expected a public HTTP(S) URL", in)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("archive URL must not contain credentials")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("archive URL must not contain a fragment")
	}
	return parsed.String(), nil
}

type repositorySpec struct {
	Repository string
	Provider   string
	ArchiveURL string
}

// resolveRepository recognizes hosted providers with stable source-archive
// endpoints. Other owner/repository URLs use the compatible public API shared
// by Forgejo and Gitea; download errors identify incompatible hosts. Git is
// never invoked.
func resolveRepository(in, ref string) (repositorySpec, error) {
	raw := strings.TrimSpace(in)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return repositorySpec{}, fmt.Errorf("repository %q: expected a public HTTP(S) repository URL", in)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return repositorySpec{}, fmt.Errorf("repository URL must not contain credentials, query parameters, or a fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return repositorySpec{}, fmt.Errorf("repository URL has an invalid path")
		}
	}
	if len(parts) < 2 {
		return repositorySpec{}, fmt.Errorf("repository URL must include an owner and repository")
	}
	cleanRef := strings.TrimSpace(ref)
	repository := parsed.Scheme + "://" + parsed.Host + "/" + path
	archiveRef := cleanRef
	if archiveRef == "" {
		archiveRef = "HEAD"
	}

	var provider, archiveURL string
	switch host {
	case "github.com":
		if len(parts) != 2 {
			return repositorySpec{}, fmt.Errorf("GitHub repository URL must be https://github.com/owner/repository")
		}
		provider = "github"
		archiveURL = fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", parts[0], parts[1], url.PathEscape(archiveRef))
	case "gitlab.com":
		provider = "gitlab"
		project := url.PathEscape(strings.Join(parts, "/"))
		archiveURL = parsed.Scheme + "://" + parsed.Host + "/api/v4/projects/" + project + "/repository/archive.tar.gz"
		if cleanRef != "" {
			archiveURL += "?sha=" + url.QueryEscape(cleanRef)
		}
	case "bitbucket.org":
		if len(parts) != 2 {
			return repositorySpec{}, fmt.Errorf("Bitbucket repository URL must be https://bitbucket.org/workspace/repository")
		}
		provider = "bitbucket"
		archiveURL = fmt.Sprintf("https://bitbucket.org/%s/%s/get/%s.gz", parts[0], parts[1], url.PathEscape(archiveRef))
	case "codeberg.org", "gitea.com":
		if len(parts) != 2 {
			return repositorySpec{}, fmt.Errorf("%s repository URL must include one owner and repository", parsed.Host)
		}
		provider = "gitea"
		if host == "codeberg.org" {
			provider = "codeberg"
		}
		archiveURL = fmt.Sprintf("%s://%s/api/v1/repos/%s/%s/archive/%s.tar.gz", parsed.Scheme, parsed.Host, parts[0], parts[1], url.PathEscape(archiveRef))
	default:
		// Forgejo and Gitea instances are self-hosted on arbitrary domains and
		// expose the same public repository archive endpoint, so a normal
		// host/owner/repository URL is enough to derive it without cloning.
		if len(parts) != 2 {
			return repositorySpec{}, fmt.Errorf("self-hosted Forgejo or Gitea repository URL must include one owner and repository")
		}
		provider = "forgejo"
		archiveURL = fmt.Sprintf("%s://%s/api/v1/repos/%s/%s/archive/%s.tar.gz", parsed.Scheme, parsed.Host, parts[0], parts[1], url.PathEscape(archiveRef))
	}
	return repositorySpec{Repository: repository, Provider: provider, ArchiveURL: archiveURL}, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
