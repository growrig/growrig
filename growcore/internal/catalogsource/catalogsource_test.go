package catalogsource

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "devices"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `manifest: 1
id: test-catalog
name: Test Catalog
provides: [devices]
`
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "test-catalog" || len(got.Provides) != 1 || got.Provides[0] != "devices" {
		t.Fatalf("manifest = %#v", got)
	}

	bad := `manifest: 1
id: ../escape
name: Bad
provides: [devices]
`
	if err := os.WriteFile(filepath.Join(dir, "catalog.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(dir); err == nil {
		t.Fatal("expected invalid manifest id to be rejected")
	}
}

func TestExtractTarballStripsRootAndRejectsTraversal(t *testing.T) {
	archive := tarball(t, []tarEntry{
		{name: "repo-main/catalog.yaml", body: "manifest: 1\n"},
		{name: "repo-main/devices/fan/example/device.yaml", body: "brand: Example\n"},
	})
	dir := t.TempDir()
	if err := extractTarball(bytes.NewReader(archive), dir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "devices", "fan", "example", "device.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "brand: Example\n" {
		t.Fatalf("extracted content = %q", raw)
	}

	escape := tarball(t, []tarEntry{{name: "repo-main/../../outside", body: "nope"}})
	if err := extractTarball(bytes.NewReader(escape), t.TempDir()); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestNewLoadsValidatedSourcesAndDirs(t *testing.T) {
	dir := t.TempDir()
	source := Source{
		ID:        "community",
		Repo:      "growrig/community-catalog",
		Name:      "Community",
		Provides:  []string{"devices", "species"},
		AddedAt:   time.Now().UTC(),
		FetchedAt: time.Now().UTC(),
	}
	m := &Manager{
		file:     filepath.Join(dir, "catalog-sources.yaml"),
		cacheDir: filepath.Join(dir, "catalog-cache"),
		sources:  []Source{source},
	}
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	deviceDir := filepath.Join(m.cacheDir, source.ID, "devices")
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	loaded, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	dirs := loaded.Dirs("devices")
	if len(dirs) != 1 || dirs[0].SourceID != source.ID || dirs[0].Dir != deviceDir {
		t.Fatalf("device dirs = %#v", dirs)
	}
	if dirs := loaded.Dirs("../devices"); dirs != nil {
		t.Fatalf("invalid kind returned dirs: %#v", dirs)
	}

	loaded.Apply = func() { t.Fatal("apply called for missing source") }
	if err := loaded.Remove("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove(missing) = %v", err)
	}
}

func TestAddPersistsAppliesAndRemovesSource(t *testing.T) {
	archive := tarball(t, []tarEntry{
		{name: "community-main/catalog.yaml", body: "manifest: 1\nid: community\nname: Community Catalog\nprovides: [devices]\n"},
		{name: "community-main/devices/sensor/example/device.yaml", body: "brand: Example\n"},
	})
	previousClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://codeload.github.com/growrig/community/tar.gz/main" {
			t.Fatalf("download URL = %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(archive)),
			Request:    request,
		}, nil
	})}
	t.Cleanup(func() { httpClient = previousClient })

	dir := t.TempDir()
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	applied := 0
	m.Apply = func() { applied++ }
	source, err := m.Add("https://github.com/growrig/community.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	if source.ID != "community" || source.Repo != "growrig/community" || applied != 1 {
		t.Fatalf("source = %#v, applied = %d", source, applied)
	}
	if len(m.Dirs("devices")) != 1 {
		t.Fatalf("device dirs = %#v", m.Dirs("devices"))
	}
	if _, err := New(dir); err != nil {
		t.Fatalf("reload persisted sources: %v", err)
	}
	if err := m.Remove(source.ID); err != nil {
		t.Fatal(err)
	}
	if applied != 2 || len(m.List()) != 0 || len(m.Dirs("devices")) != 0 {
		t.Fatalf("source was not fully removed: applied=%d list=%#v dirs=%#v", applied, m.List(), m.Dirs("devices"))
	}
}

type tarEntry struct {
	name string
	body string
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func tarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
