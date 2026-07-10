// Package store persists Grow Core configuration and history in SQLite.
//
// It uses the pure-Go modernc.org/sqlite driver so Grow Core builds and runs
// without CGO, which keeps cross-compilation for the Grow Hub simple.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use ":memory:" for ephemeral runs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// modernc/sqlite is safest with a single writer connection.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS environments (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    target_temp     REAL NOT NULL,
    target_humidity REAL NOT NULL,
    emergency_temp  REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    environment_id  TEXT NOT NULL,
    adapter         TEXT NOT NULL,
    temp_entity     TEXT NOT NULL DEFAULT '',
    humidity_entity TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS channels (
    device_id  TEXT NOT NULL,
    id         TEXT NOT NULL,
    name       TEXT NOT NULL,
    role       TEXT NOT NULL,
    entity     TEXT NOT NULL DEFAULT '',
    rpm_entity TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL,
    PRIMARY KEY (device_id, id)
);
CREATE TABLE IF NOT EXISTS readings (
    environment_id TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    temp           REAL NOT NULL,
    humidity       REAL NOT NULL,
    exhaust_speed  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_readings_env_ts ON readings (environment_id, ts);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Additive migrations so databases created by earlier versions gain the
	// entity-binding columns.
	migrations := []struct{ table, column, def string }{
		{"devices", "temp_entity", "TEXT NOT NULL DEFAULT ''"},
		{"devices", "humidity_entity", "TEXT NOT NULL DEFAULT ''"},
		{"channels", "entity", "TEXT NOT NULL DEFAULT ''"},
		{"channels", "rpm_entity", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		if err := s.ensureColumn(m.table, m.column, m.def); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn adds a column if it does not already exist.
func (s *Store) ensureColumn(table, column, def string) error {
	rows, err := s.db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + def)
	return err
}

// --- Environments ---

func (s *Store) SaveEnvironment(e domain.Environment) error {
	_, err := s.db.Exec(
		`INSERT INTO environments (id, name, target_temp, target_humidity, emergency_temp)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   target_temp=excluded.target_temp,
		   target_humidity=excluded.target_humidity,
		   emergency_temp=excluded.emergency_temp`,
		e.ID, e.Name, e.TargetTempC, e.TargetHumidity, e.EmergencyTempC,
	)
	return err
}

func (s *Store) UpdateTargets(id string, targetTemp, targetHumidity float64) error {
	res, err := s.db.Exec(
		`UPDATE environments SET target_temp=?, target_humidity=? WHERE id=?`,
		targetTemp, targetHumidity, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("environment %q not found", id)
	}
	return nil
}

func (s *Store) Environments() ([]domain.Environment, error) {
	rows, err := s.db.Query(
		`SELECT id, name, target_temp, target_humidity, emergency_temp
		 FROM environments ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Environment
	for rows.Next() {
		var e domain.Environment
		if err := rows.Scan(&e.ID, &e.Name, &e.TargetTempC, &e.TargetHumidity, &e.EmergencyTempC); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Devices & channels ---

// SaveDevice upserts a device and replaces its channel configuration.
func (s *Store) SaveDevice(d domain.Device) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO devices (id, name, environment_id, adapter, temp_entity, humidity_entity)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   environment_id=excluded.environment_id,
		   adapter=excluded.adapter,
		   temp_entity=excluded.temp_entity,
		   humidity_entity=excluded.humidity_entity`,
		d.ID, d.Name, d.EnvironmentID, d.Adapter, d.TempEntity, d.HumidityEntity,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM channels WHERE device_id=?`, d.ID); err != nil {
		return err
	}
	for i, c := range d.Channels {
		if _, err := tx.Exec(
			`INSERT INTO channels (device_id, id, name, role, entity, rpm_entity, position)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			d.ID, c.ID, c.Name, string(c.Role), c.Entity, c.RPMEntity, i,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteDevice removes a device and its channels.
func (s *Store) DeleteDevice(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM channels WHERE device_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM devices WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("device %q not found", id)
	}
	return tx.Commit()
}

// DeleteEnvironment removes an environment. It fails if devices still
// reference it.
func (s *Store) DeleteEnvironment(id string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM devices WHERE environment_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("environment %q still has %d device(s)", id, count)
	}
	res, err := s.db.Exec(`DELETE FROM environments WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("environment %q not found", id)
	}
	return nil
}

// UpdateChannelRole persists a role assignment for a single channel.
func (s *Store) UpdateChannelRole(deviceID, channelID string, role domain.Role) error {
	res, err := s.db.Exec(
		`UPDATE channels SET role=? WHERE device_id=? AND id=?`,
		string(role), deviceID, channelID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("channel %q on device %q not found", channelID, deviceID)
	}
	return nil
}

// Devices returns persisted device config (channels included). Live values
// such as temperature, RPM and health are supplied at runtime by adapters.
func (s *Store) Devices() ([]domain.Device, error) {
	rows, err := s.db.Query(
		`SELECT id, name, environment_id, adapter, temp_entity, humidity_entity
		 FROM devices ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Device
	for rows.Next() {
		var d domain.Device
		if err := rows.Scan(&d.ID, &d.Name, &d.EnvironmentID, &d.Adapter, &d.TempEntity, &d.HumidityEntity); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ch, err := s.channels(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Channels = ch
	}
	return out, nil
}

func (s *Store) channels(deviceID string) ([]domain.Channel, error) {
	rows, err := s.db.Query(
		`SELECT id, name, role, entity, rpm_entity FROM channels WHERE device_id=? ORDER BY position`,
		deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Channel
	for rows.Next() {
		var c domain.Channel
		var role string
		if err := rows.Scan(&c.ID, &c.Name, &role, &c.Entity, &c.RPMEntity); err != nil {
			return nil, err
		}
		c.Role = domain.Role(role)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Readings history ---

func (s *Store) InsertReading(r domain.Reading) error {
	_, err := s.db.Exec(
		`INSERT INTO readings (environment_id, ts, temp, humidity, exhaust_speed)
		 VALUES (?, ?, ?, ?, ?)`,
		r.EnvironmentID, r.Time.UnixMilli(), r.TempC, r.Humidity, r.ExhaustSpeed,
	)
	return err
}

// RecentReadings returns up to limit most-recent readings for an environment,
// oldest first (chart-friendly order).
func (s *Store) RecentReadings(envID string, limit int) ([]domain.Reading, error) {
	rows, err := s.db.Query(
		`SELECT ts, temp, humidity, exhaust_speed FROM readings
		 WHERE environment_id=? ORDER BY ts DESC LIMIT ?`,
		envID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Reading
	for rows.Next() {
		r := domain.Reading{EnvironmentID: envID}
		var ts int64
		if err := rows.Scan(&ts, &r.TempC, &r.Humidity, &r.ExhaustSpeed); err != nil {
			return nil, err
		}
		r.Time = time.UnixMilli(ts)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
