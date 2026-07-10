// Package webui serves the Grow App Web single-page app embedded into the
// Grow Core binary.
//
// The built SvelteKit output (adapter-static) is copied into ./dist by the
// build (see the repo Makefile) and embedded here, so the Home Assistant OS
// add-on ships as a single binary that serves both the UI and the API on one
// port. When no build is embedded (a plain `go build` with only the .gitkeep
// placeholder), Handler reports ok=false and Grow Core runs API-only.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler returns an http.Handler serving the embedded SPA, and ok=false if no
// build is present. Unknown paths fall back to index.html so client-side routes
// (e.g. /setup) resolve.
func Handler() (http.Handler, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false // only the placeholder is embedded
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// SPA fallback: serve the app shell for unknown routes.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}), true
}
