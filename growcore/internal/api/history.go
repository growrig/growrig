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
	if err := s.labelCareApplications(page.Items); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// labelCareApplications names the plants inside care entries, so a history row
// can say "Cheese #1" instead of a raw unit id. Units are looked up once per
// grow: a page of history is usually one or two grows deep.
func (s *Server) labelCareApplications(entries []domain.HistoryEntry) error {
	byGrow := map[string]map[string]domain.PlantUnit{}
	for _, entry := range entries {
		if entry.Care == nil {
			continue
		}
		plants, ok := byGrow[entry.Care.GrowID]
		if !ok {
			var err error
			if plants, err = s.growPlants(entry.Care.GrowID); err != nil {
				return err
			}
			byGrow[entry.Care.GrowID] = plants
		}
		for i := range entry.Care.Applications {
			app := &entry.Care.Applications[i]
			app.PlantLabel = resolvePlantLabel(plants, app.PlantUnitID)
		}
	}
	return nil
}
