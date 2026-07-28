package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// History assembles a collection's chronological story across every table that
// records something about it.
//
// The work is done in two steps rather than by loading each source and merging
// in memory:
//
//  1. A UNION ALL over the sources produces a lightweight (kind, id, ts) index,
//     ordered and paged in SQL. Only the ids on the requested page survive.
//  2. Those ids are hydrated per kind, one query each.
//
// The reason is correctness as much as speed: to show "the newest 50 things"
// a merge has to consider every source in full, because the next-oldest entry
// could come from any of them. Paging in SQL over the union is the only way to
// bound that without silently truncating history.

// historyRow is one line of the union index: enough to sort and page, no more.
type historyRow struct {
	kind domain.HistoryKind
	id   string
	ts   int64
}

// historySources builds the per-source SELECTs for a scope, each yielding
// (kind, id, ts).
//
// Every branch aliases its columns: a UNION takes its column names from the
// first SELECT, and which branch comes first depends on the scope and on the
// kind filter, so relying on one of them to name the set silently breaks the
// others. Sources that cannot apply to the scope are omitted.
func historySources(q domain.HistoryQuery) (selects []string, args []any) {
	want := map[domain.HistoryKind]bool{}
	for _, k := range q.Kinds {
		want[k] = true
	}
	include := func(k domain.HistoryKind) bool { return len(want) == 0 || want[k] }

	add := func(sql string, a ...any) {
		selects = append(selects, sql)
		args = append(args, a...)
	}

	switch {
	case q.GrowID != "":
		if include(domain.HistoryCare) {
			add(`SELECT 'care' AS kind, id AS id, occurred_at AS ts FROM care_events WHERE grow_id=?`, q.GrowID)
		}
		if include(domain.HistoryPhoto) {
			add(`SELECT 'photo' AS kind, id AS id, taken_at AS ts FROM grow_photos WHERE grow_id=?`, q.GrowID)
		}
		if include(domain.HistoryMilestone) {
			add(`SELECT 'milestone' AS kind, id AS id, entered_at AS ts FROM stage_events WHERE grow_id=?`, q.GrowID)
		}
		if include(domain.HistorySystem) {
			add(`SELECT 'system' AS kind, id AS id, ts AS ts FROM activity_log WHERE grow_id=?`+historyActivityFilter(q), q.GrowID)
		}

	case q.PlantID != "":
		if include(domain.HistoryCare) {
			// A care event belongs to a plant's history when it applied to it.
			add(`SELECT 'care' AS kind, e.id AS id, e.occurred_at AS ts FROM care_events e
			     JOIN care_applications a ON a.care_event_id = e.id
			     WHERE a.plant_unit_id=?`, q.PlantID)
		}
		if include(domain.HistoryPhoto) {
			add(`SELECT 'photo' AS kind, id AS id, taken_at AS ts FROM grow_photos WHERE plant_unit_id=?`, q.PlantID)
		}
		if include(domain.HistoryPlacement) {
			add(`SELECT 'placement' AS kind, id AS id, started_at AS ts FROM plant_placements WHERE plant_unit_id=?`, q.PlantID)
		}
		if include(domain.HistoryPot) {
			add(`SELECT 'pot' AS kind, id AS id, started_at AS ts FROM plant_pots WHERE plant_unit_id=?`, q.PlantID)
		}

	case q.EnvironmentID != "":
		if include(domain.HistorySystem) {
			add(`SELECT 'system' AS kind, id AS id, ts AS ts FROM activity_log WHERE environment_id=?`+historyActivityFilter(q), q.EnvironmentID)
		}
		if include(domain.HistoryPlacement) {
			// Plants arriving in this environment are part of its story.
			add(`SELECT 'placement' AS kind, id AS id, started_at AS ts FROM plant_placements WHERE environment_id=?`, q.EnvironmentID)
		}
	}
	return selects, args
}

// historyActivityFilter excludes the noise History never wants, and optionally
// the routine control-loop entries.
//
// Care entries are dropped always: logging care writes both a CareEvent and a
// text echo into the activity log, so including them would show every watering
// twice. Notices are internal chatter with no reader value.
func historyActivityFilter(q domain.HistoryQuery) string {
	f := ` AND type NOT IN ('care','notice')`
	if !q.Automation {
		// Only info-level control entries are routine; a control failure is
		// exactly what History exists to surface.
		f += ` AND NOT (type='control' AND level='info')`
	}
	return f
}

// History returns one page of a collection's history, newest first, plus the
// total number of entries available in that scope.
func (s *Store) History(q domain.HistoryQuery) (domain.HistoryPage, error) {
	page := domain.HistoryPage{Items: []domain.HistoryEntry{}}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	selects, args := historySources(q)
	if len(selects) == 0 {
		return page, nil
	}
	union := strings.Join(selects, "\nUNION ALL\n")

	// Total first, so the client can render "N more" without paging to the end.
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM (`+union+`)`, args...).Scan(&page.Total); err != nil {
		return page, fmt.Errorf("history count: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT kind, id, ts FROM (`+union+`) ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), q.Limit, q.Offset)...)
	if err != nil {
		return page, fmt.Errorf("history index: %w", err)
	}
	var index []historyRow
	for rows.Next() {
		var r historyRow
		var kind string
		if err := rows.Scan(&kind, &r.id, &r.ts); err != nil {
			rows.Close()
			return page, err
		}
		r.kind = domain.HistoryKind(kind)
		index = append(index, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return page, err
	}

	entries, err := s.hydrateHistory(index, q)
	if err != nil {
		return page, err
	}
	page.Items = entries
	return page, nil
}

// hydrateHistory loads the full record behind each indexed id, one query per
// kind, and returns entries in the index's order.
func (s *Store) hydrateHistory(index []historyRow, q domain.HistoryQuery) ([]domain.HistoryEntry, error) {
	byKind := map[domain.HistoryKind][]string{}
	for _, r := range index {
		byKind[r.kind] = append(byKind[r.kind], r.id)
	}

	care := map[string]domain.CareEvent{}
	if ids := byKind[domain.HistoryCare]; len(ids) > 0 {
		events, err := s.careEventsByID(ids)
		if err != nil {
			return nil, err
		}
		care = events
	}
	photos := map[string]domain.GrowPhoto{}
	if ids := byKind[domain.HistoryPhoto]; len(ids) > 0 {
		got, err := s.photosByID(ids)
		if err != nil {
			return nil, err
		}
		photos = got
	}
	stages := map[string]string{}
	if ids := byKind[domain.HistoryMilestone]; len(ids) > 0 {
		got, err := s.stageEventsByID(ids)
		if err != nil {
			return nil, err
		}
		stages = got
	}
	placements := map[string]domain.PlantPlacement{}
	if ids := byKind[domain.HistoryPlacement]; len(ids) > 0 {
		got, err := s.placementsByID(ids)
		if err != nil {
			return nil, err
		}
		placements = got
	}
	pots := map[string]domain.PlantPot{}
	if ids := byKind[domain.HistoryPot]; len(ids) > 0 {
		got, err := s.potsByID(ids)
		if err != nil {
			return nil, err
		}
		pots = got
	}
	activity := map[string]domain.Activity{}
	if ids := byKind[domain.HistorySystem]; len(ids) > 0 {
		got, err := s.activityByID(ids)
		if err != nil {
			return nil, err
		}
		activity = got
	}

	// Labels for the refs an entry links to, resolved in bulk.
	labels, err := s.historyLabels(care, placements, activity)
	if err != nil {
		return nil, err
	}

	out := make([]domain.HistoryEntry, 0, len(index))
	for _, r := range index {
		e := domain.HistoryEntry{ID: r.id, Kind: r.kind, At: time.UnixMilli(r.ts)}
		switch r.kind {
		case domain.HistoryCare:
			c, ok := care[r.id]
			if !ok {
				continue // deleted between index and hydrate
			}
			e.Care = &c
			for _, a := range c.Applications {
				e.Refs = append(e.Refs, domain.HistoryRef{
					Kind: "plant", ID: a.PlantUnitID, Label: labels[a.PlantUnitID],
				})
			}
		case domain.HistoryPhoto:
			p, ok := photos[r.id]
			if !ok {
				continue
			}
			e.Photo = &p
		case domain.HistoryMilestone:
			stage, ok := stages[r.id]
			if !ok {
				continue
			}
			e.Stage = stage
		case domain.HistoryPlacement:
			p, ok := placements[r.id]
			if !ok {
				continue
			}
			e.Placement = &p
			e.Refs = append(e.Refs,
				domain.HistoryRef{Kind: "environment", ID: p.EnvironmentID, Label: labels[p.EnvironmentID]},
				domain.HistoryRef{Kind: "plant", ID: p.PlantUnitID, Label: labels[p.PlantUnitID]},
			)
		case domain.HistoryPot:
			p, ok := pots[r.id]
			if !ok {
				continue
			}
			e.Pot = &p
		case domain.HistorySystem:
			a, ok := activity[r.id]
			if !ok {
				continue
			}
			e.Activity = &a
			e.Automation = a.Type == "control" && a.Level == "info"
			if a.GrowID != "" {
				e.Refs = append(e.Refs, domain.HistoryRef{Kind: "grow", ID: a.GrowID, Label: labels[a.GrowID]})
			}
			if a.EnvironmentID != "" {
				e.Refs = append(e.Refs, domain.HistoryRef{
					Kind: "environment", ID: a.EnvironmentID, Label: labels[a.EnvironmentID],
				})
			}
		}
		// Scope self-references add nothing: on a plant's page, every entry
		// links back to that plant.
		e.Refs = dropSelfRefs(e.Refs, q)
		out = append(out, e)
	}
	return out, nil
}

// dropSelfRefs removes references to the collection being viewed.
func dropSelfRefs(refs []domain.HistoryRef, q domain.HistoryQuery) []domain.HistoryRef {
	if len(refs) == 0 {
		return nil
	}
	out := refs[:0]
	for _, r := range refs {
		if r.ID == q.PlantID || r.ID == q.GrowID || r.ID == q.EnvironmentID {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// historyLabels resolves display names for every entity the page's entries
// reference, in one query per entity type.
func (s *Store) historyLabels(
	care map[string]domain.CareEvent,
	placements map[string]domain.PlantPlacement,
	activity map[string]domain.Activity,
) (map[string]string, error) {
	plantIDs := map[string]bool{}
	envIDs := map[string]bool{}
	growIDs := map[string]bool{}
	for _, c := range care {
		for _, a := range c.Applications {
			plantIDs[a.PlantUnitID] = true
		}
	}
	for _, p := range placements {
		plantIDs[p.PlantUnitID] = true
		envIDs[p.EnvironmentID] = true
	}
	for _, a := range activity {
		if a.GrowID != "" {
			growIDs[a.GrowID] = true
		}
		if a.EnvironmentID != "" {
			envIDs[a.EnvironmentID] = true
		}
	}

	labels := map[string]string{}
	load := func(ids map[string]bool, query string) error {
		if len(ids) == 0 {
			return nil
		}
		list := make([]any, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		rows, err := s.db.Query(query+" ("+placeholders(len(list))+")", list...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, label string
			if err := rows.Scan(&id, &label); err != nil {
				return err
			}
			labels[id] = label
		}
		return rows.Err()
	}
	if err := load(plantIDs, `SELECT id, label FROM plant_units WHERE id IN`); err != nil {
		return nil, err
	}
	if err := load(envIDs, `SELECT id, name FROM environments WHERE id IN`); err != nil {
		return nil, err
	}
	if err := load(growIDs, `SELECT id, name FROM grows WHERE id IN`); err != nil {
		return nil, err
	}
	return labels, nil
}

// placeholders renders "?,?,?" for an IN clause of n values.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func idArgs(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// --- per-kind bulk loaders ---

func (s *Store) careEventsByID(ids []string) (map[string]domain.CareEvent, error) {
	rows, err := s.db.Query(
		`SELECT `+careCols+` FROM care_events WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.CareEvent{}
	var order []string
	for rows.Next() {
		e, err := scanCareEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[e.ID] = e
		order = append(order, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps, err := s.applicationsByEvent(order)
	if err != nil {
		return nil, err
	}
	for id, list := range apps {
		e := out[id]
		e.Applications = list
		out[id] = e
	}
	return out, nil
}

func (s *Store) photosByID(ids []string) (map[string]domain.GrowPhoto, error) {
	rows, err := s.db.Query(
		`SELECT `+photoCols+` FROM grow_photos WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.GrowPhoto{}
	for rows.Next() {
		p, err := scanGrowPhoto(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func (s *Store) stageEventsByID(ids []string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT id, stage FROM stage_events WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, stage string
		if err := rows.Scan(&id, &stage); err != nil {
			return nil, err
		}
		out[id] = stage
	}
	return out, rows.Err()
}

func (s *Store) placementsByID(ids []string) (map[string]domain.PlantPlacement, error) {
	rows, err := s.db.Query(
		`SELECT id, plant_unit_id, environment_id, started_at, ended_at, position
		 FROM plant_placements WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.PlantPlacement{}
	for rows.Next() {
		var p domain.PlantPlacement
		var started int64
		var ended *int64
		if err := rows.Scan(&p.ID, &p.PlantUnitID, &p.EnvironmentID, &started, &ended, &p.Position); err != nil {
			return nil, err
		}
		p.StartedAt = time.UnixMilli(started)
		if ended != nil {
			t := time.UnixMilli(*ended)
			p.EndedAt = &t
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func (s *Store) potsByID(ids []string) (map[string]domain.PlantPot, error) {
	rows, err := s.db.Query(
		`SELECT `+potCols+` FROM plant_pots WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.PlantPot{}
	for rows.Next() {
		p, err := scanPot(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

func (s *Store) activityByID(ids []string) (map[string]domain.Activity, error) {
	rows, err := s.db.Query(
		`SELECT id, environment_id, grow_id, device_id, ts, level, type, message
		 FROM activity_log WHERE id IN (`+placeholders(len(ids))+`)`, idArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.Activity{}
	for rows.Next() {
		var a domain.Activity
		var ts int64
		if err := rows.Scan(&a.ID, &a.EnvironmentID, &a.GrowID, &a.DeviceID, &ts, &a.Level, &a.Type, &a.Message); err != nil {
			return nil, err
		}
		a.Time = time.UnixMilli(ts)
		out[a.ID] = a
	}
	return out, rows.Err()
}
