package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// SaveCareEvent persists a care event together with its per-plant applications
// in one transaction. The event and each application must already carry ids.
func (s *Store) SaveCareEvent(e domain.CareEvent) error {
	if e.Source == "" {
		e.Source = domain.CareManual
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Doses are a small open-ended map (product key → per-plant amount), so they
	// ride as JSON rather than earning a table of their own. Empty stays "".
	var doses string
	if len(e.Doses) > 0 {
		raw, err := json.Marshal(e.Doses)
		if err != nil {
			return err
		}
		doses = string(raw)
	}
	if _, err := tx.Exec(
		`INSERT INTO care_events (id, grow_id, type, occurred_at, source, notes, recipe_id, ph, ec, runoff_ml, runoff_ph, doses, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.GrowID, e.Type, e.OccurredAt.UnixMilli(), string(e.Source), e.Notes,
		e.RecipeID, e.PH, e.EC, e.RunoffML, e.RunoffPH, doses, e.CreatedAt.UnixMilli(),
	); err != nil {
		return err
	}
	for _, a := range e.Applications {
		if _, err := tx.Exec(
			`INSERT INTO care_applications (id, care_event_id, plant_unit_id, amount_ml, note)
			 VALUES (?, ?, ?, ?, ?)`,
			a.ID, e.ID, a.PlantUnitID, a.AmountML, a.Note,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const careCols = `id, grow_id, type, occurred_at, source, notes, recipe_id, ph, ec, runoff_ml, runoff_ph, doses, created_at`

func scanCareEvent(scan func(dst ...any) error) (domain.CareEvent, error) {
	var e domain.CareEvent
	var occurred, created int64
	var source, doses string
	if err := scan(&e.ID, &e.GrowID, &e.Type, &occurred, &source, &e.Notes,
		&e.RecipeID, &e.PH, &e.EC, &e.RunoffML, &e.RunoffPH, &doses, &created); err != nil {
		return domain.CareEvent{}, err
	}
	if doses != "" {
		// A malformed blob shouldn't sink the whole journal entry.
		if err := json.Unmarshal([]byte(doses), &e.Doses); err != nil {
			e.Doses = nil
		}
	}
	e.OccurredAt = time.UnixMilli(occurred)
	e.CreatedAt = time.UnixMilli(created)
	e.Source = domain.CareSource(source)
	return e, nil
}

// applicationsByEvent loads the applications for the given event ids, grouped by
// event id, so a page of events is hydrated with one query instead of N.
func (s *Store) applicationsByEvent(eventIDs []string) (map[string][]domain.CareApplication, error) {
	out := map[string][]domain.CareApplication{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	q := `SELECT id, care_event_id, plant_unit_id, amount_ml, note FROM care_applications WHERE care_event_id IN (`
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		if i > 0 {
			q += ","
		}
		q += "?"
		args[i] = id
	}
	q += `)`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a domain.CareApplication
		if err := rows.Scan(&a.ID, &a.CareEventID, &a.PlantUnitID, &a.AmountML, &a.Note); err != nil {
			return nil, err
		}
		out[a.CareEventID] = append(out[a.CareEventID], a)
	}
	return out, rows.Err()
}

// CareEvents returns a grow's care events, newest first, each with its
// applications attached.
func (s *Store) CareEvents(growID string, limit, offset int) ([]domain.CareEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(
		`SELECT `+careCols+` FROM care_events WHERE grow_id=? ORDER BY occurred_at DESC LIMIT ? OFFSET ?`,
		growID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.CareEvent
	var ids []string
	for rows.Next() {
		e, err := scanCareEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps, err := s.applicationsByEvent(ids)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Applications = apps[events[i].ID]
		if events[i].Applications == nil {
			events[i].Applications = []domain.CareApplication{}
		}
	}
	return events, nil
}

// AllCareEvents returns care events across every grow whose OccurredAt falls in
// [from, to], newest first, each with its applications attached. It backs the
// calendar's cross-grow view. A zero from or to leaves that bound open.
func (s *Store) AllCareEvents(from, to time.Time, limit int) ([]domain.CareEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 1000
	}
	q := `SELECT ` + careCols + ` FROM care_events WHERE 1=1`
	var args []any
	if !from.IsZero() {
		q += ` AND occurred_at >= ?`
		args = append(args, from.UnixMilli())
	}
	if !to.IsZero() {
		q += ` AND occurred_at <= ?`
		args = append(args, to.UnixMilli())
	}
	q += ` ORDER BY occurred_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.CareEvent
	var ids []string
	for rows.Next() {
		e, err := scanCareEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps, err := s.applicationsByEvent(ids)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Applications = apps[events[i].ID]
		if events[i].Applications == nil {
			events[i].Applications = []domain.CareApplication{}
		}
	}
	return events, nil
}

// CareEvent returns a single care event with its applications.
func (s *Store) CareEvent(id string) (domain.CareEvent, bool, error) {
	e, err := scanCareEvent(s.db.QueryRow(`SELECT `+careCols+` FROM care_events WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CareEvent{}, false, nil
	}
	if err != nil {
		return domain.CareEvent{}, false, err
	}
	apps, err := s.applicationsByEvent([]string{id})
	if err != nil {
		return domain.CareEvent{}, false, err
	}
	e.Applications = apps[id]
	if e.Applications == nil {
		e.Applications = []domain.CareApplication{}
	}
	return e, true, nil
}

// LastCareByType returns the most recent care event per action type for a grow,
// keyed by type (with applications attached). It drives the "last watered / last
// fed" summary on the grow page.
func (s *Store) LastCareByType(growID string) (map[string]domain.CareEvent, error) {
	rows, err := s.db.Query(
		`SELECT `+careCols+` FROM care_events e
		 WHERE grow_id=? AND occurred_at = (
		     SELECT MAX(occurred_at) FROM care_events WHERE grow_id=e.grow_id AND type=e.type
		 ) GROUP BY type`, growID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.CareEvent{}
	var ids []string
	for rows.Next() {
		e, err := scanCareEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[e.Type] = e
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps, err := s.applicationsByEvent(ids)
	if err != nil {
		return nil, err
	}
	for t, e := range out {
		e.Applications = apps[e.ID]
		if e.Applications == nil {
			e.Applications = []domain.CareApplication{}
		}
		out[t] = e
	}
	return out, nil
}

// LastCarePerPlant returns the last time each plant unit of a grow received any
// care application, keyed by plant unit id. Plants absent from the map have
// never been cared for. It backs skipped-plant detection on the grow page.
func (s *Store) LastCarePerPlant(growID string) (map[string]time.Time, error) {
	rows, err := s.db.Query(
		`SELECT a.plant_unit_id, MAX(e.occurred_at)
		 FROM care_applications a JOIN care_events e ON e.id = a.care_event_id
		 WHERE e.grow_id=? GROUP BY a.plant_unit_id`, growID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var unitID string
		var ts int64
		if err := rows.Scan(&unitID, &ts); err != nil {
			return nil, err
		}
		out[unitID] = time.UnixMilli(ts)
	}
	return out, rows.Err()
}

// GrowCareConfig returns a grow's per-grow care customization. ok is false when
// the grow has no saved config (so callers fall back to species defaults) or the
// grow does not exist.
func (s *Store) GrowCareConfig(growID string) (domain.GrowCareConfig, bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT care_config FROM grows WHERE id=?`, growID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GrowCareConfig{}, false, nil
	}
	if err != nil {
		return domain.GrowCareConfig{}, false, err
	}
	if raw == "" {
		return domain.GrowCareConfig{}, false, nil
	}
	var cfg domain.GrowCareConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return domain.GrowCareConfig{}, false, err
	}
	return cfg, true, nil
}

// SaveGrowCareConfig persists a grow's care customization. An empty action list
// clears the config, reverting the grow to its species defaults.
func (s *Store) SaveGrowCareConfig(growID string, cfg domain.GrowCareConfig) error {
	var raw string
	if len(cfg.Actions) > 0 {
		b, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	_, err := s.db.Exec(`UPDATE grows SET care_config=? WHERE id=?`, raw, growID)
	return err
}

// DeleteCareEvent removes a care event and its applications.
func (s *Store) DeleteCareEvent(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM care_applications WHERE care_event_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM care_events WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateCareEvent rewrites an existing event's editable fields and replaces its
// applications. Type and GrowID are fixed at creation — changing either makes it
// a different entry, so that kind of correction is a delete plus a fresh log.
func (s *Store) UpdateCareEvent(e domain.CareEvent) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	var doses string
	if len(e.Doses) > 0 {
		raw, err := json.Marshal(e.Doses)
		if err != nil {
			return err
		}
		doses = string(raw)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`UPDATE care_events SET occurred_at=?, notes=?, recipe_id=?, ph=?, ec=?, runoff_ml=?, runoff_ph=?, doses=?
		 WHERE id=?`,
		e.OccurredAt.UnixMilli(), e.Notes, e.RecipeID, e.PH, e.EC, e.RunoffML, e.RunoffPH, doses, e.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("care event not found")
	}
	if _, err := tx.Exec(`DELETE FROM care_applications WHERE care_event_id=?`, e.ID); err != nil {
		return err
	}
	for _, a := range e.Applications {
		if _, err := tx.Exec(
			`INSERT INTO care_applications (id, care_event_id, plant_unit_id, amount_ml, note)
			 VALUES (?, ?, ?, ?, ?)`,
			a.ID, e.ID, a.PlantUnitID, a.AmountML, a.Note,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
