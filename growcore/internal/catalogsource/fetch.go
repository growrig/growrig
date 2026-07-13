package catalogsource

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Fetch limits. Catalogs are YAML plus a few product images; anything near
// these caps is not a catalog repository.
const (
	maxTarballBytes = 200 << 20 // compressed download
	maxFileBytes    = 20 << 20  // single extracted file
	maxTotalBytes   = 500 << 20 // extracted tree
	fetchTimeout    = 60 * time.Second
)

var httpClient = &http.Client{Timeout: fetchTimeout}

// fetch downloads owner/name at ref (empty = default branch) as a GitHub
// tarball, extracts it into a temporary directory under the cache root and
// returns the validated manifest with that directory. The caller installs or
// removes the directory.
func (m *Manager) fetch(owner, name, ref string) (Manifest, string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, name, ref)
	resp, err := httpClient.Get(url)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("download %s/%s: %w", owner, name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, "", fmt.Errorf("download %s/%s@%s: GitHub returned %s (is the repository public and the ref correct?)", owner, name, ref, resp.Status)
	}

	if err := os.MkdirAll(m.cacheDir, 0o755); err != nil {
		return Manifest{}, "", err
	}
	dir, err := os.MkdirTemp(m.cacheDir, ".fetch-*")
	if err != nil {
		return Manifest{}, "", err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	if err := extractTarball(io.LimitReader(resp.Body, maxTarballBytes), dir); err != nil {
		cleanup()
		return Manifest{}, "", fmt.Errorf("extract %s/%s: %w", owner, name, err)
	}
	man, err := readManifest(dir)
	if err != nil {
		cleanup()
		return Manifest{}, "", err
	}
	return man, dir, nil
}

func readManifest(dir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, fmt.Errorf("not a GrowRig catalog: no catalog.yaml at the repository root")
		}
		return Manifest{}, err
	}
	var man Manifest
	if err := yaml.Unmarshal(raw, &man); err != nil {
		return Manifest{}, fmt.Errorf("parse catalog.yaml: %w", err)
	}
	if err := man.validate(); err != nil {
		return Manifest{}, fmt.Errorf("invalid catalog.yaml: %w", err)
	}
	for _, kind := range man.Provides {
		if fi, err := os.Stat(filepath.Join(dir, kind)); err != nil || !fi.IsDir() {
			return Manifest{}, fmt.Errorf("invalid catalog: manifest provides %q but the repository has no %s/ directory", kind, kind)
		}
	}
	return man, nil
}

// extractTarball unpacks a GitHub .tar.gz stream into dst, stripping the
// top-level "<repo>-<ref>/" component GitHub adds. Only regular files and
// directories are written; symlinks and other entry types are skipped, and
// any path escaping dst is rejected.
func extractTarball(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rel := stripRoot(hdr.Name)
		if rel == "" {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("tarball entry %q escapes the extraction root", hdr.Name)
		}
		path := filepath.Join(dst, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > maxFileBytes {
				return fmt.Errorf("file %s exceeds the %d MB limit", rel, maxFileBytes>>20)
			}
			total += hdr.Size
			if total > maxTotalBytes {
				return fmt.Errorf("catalog exceeds the %d MB extracted-size limit", maxTotalBytes>>20)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFileBytes)); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		default:
			// Symlinks, hardlinks and devices have no place in a catalog.
		}
	}
}

// stripRoot removes the "<repo>-<ref>/" prefix of a GitHub tarball entry.
func stripRoot(name string) string {
	name = strings.TrimPrefix(name, "./")
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return ""
}
