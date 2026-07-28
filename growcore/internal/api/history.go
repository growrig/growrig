package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// getCollectionHistory serves a collection's merged, chronological story, paged server-side.
//
//	GET /api/history?growId=…|plantId=…|environmentId=…
//	                &limit=50&offset=0&kinds=care,photo&automation=true
//
// Exactly one scope parameter is required: history is always *of* something.
// Entries carry structured records, not rendered sentences — the client builds
// the wording, so the API stays free of English and its links stay resolvable.
func (s *Server) getCollectionHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := domain.HistoryQuery{
		GrowID:        strings.TrimSpace(q.Get("growId")),
		PlantID:       strings.TrimSpace(q.Get("plantId")),
		EnvironmentID: strings.TrimSpace(q.Get("environmentId")),
		Automation:    q.Get("automation") == "true",
		Limit:         50,
	}
	scopes := 0
	for _, v := range []string{query.GrowID, query.PlantID, query.EnvironmentID} {
		if v != "" {
			scopes++
		}
	}
	if scopes != 1 {
		writeJSON(w, http.StatusBadRequest,
			errBody("exactly one of growId, plantId or environmentId is required"))
		return
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			query.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			query.Offset = n
		}
	}
	if raw := strings.TrimSpace(q.Get("kinds")); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			kind := domain.HistoryKind(strings.TrimSpace(k))
			if kind == "" {
				continue
			}
			if !domain.ValidHistoryKind(kind) {
				writeJSON(w, http.StatusBadRequest, errBody("unknown history kind: "+string(kind)))
				return
			}
			query.Kinds = append(query.Kinds, kind)
		}
	}

	page, err := s.store.History(query)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
