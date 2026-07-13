package api

import (
	"errors"
	"net/http"

	"github.com/growrig/growrig-platform/growcore/internal/catalogsource"
)

// Custom catalog sources: GitHub repositories with a catalog.yaml manifest
// that extend the built-in device/integration catalogs. Managed by admins
// under Control panel → Catalog.

func (s *Server) getCatalogSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sources":     s.catalogSources.List(),
		"mergedKinds": catalogsource.MergedKinds,
	})
}

func (s *Server) createCatalogSource(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Repo string `json:"repo"`
		Ref  string `json:"ref"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	src, err := s.catalogSources.Add(in.Repo, in.Ref)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.activity("", "", "info", "catalog", "Catalog source added: "+src.Name+" ("+src.Repo+")")
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) refreshCatalogSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.catalogSources.Refresh(r.PathValue("id"))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, catalogsource.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.activity("", "", "info", "catalog", "Catalog source refreshed: "+src.Name+" ("+src.Repo+")")
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) deleteCatalogSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.catalogSources.Remove(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, catalogsource.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err)
		return
	}
	s.activity("", "", "info", "catalog", "Catalog source removed: "+id)
	w.WriteHeader(http.StatusNoContent)
}
