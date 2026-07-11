// Package store persists Grow Core configuration and history in SQLite.
//
// It uses the pure-Go modernc.org/sqlite driver so Grow Core builds and runs
// without CGO, which keeps cross-compilation for the Grow Hub simple.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

type Store struct {
	db        *sql.DB
	configDir string
	syncing   bool
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use ":memory:" for ephemeral runs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc/sqlite is safest single-writer
	s := &Store{db: db, configDir: filepath.Join(filepath.Dir(path), "environments")}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.syncYAMLConfig(); err != nil {
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
    kind            TEXT NOT NULL DEFAULT 'tent',
    air_source      TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL DEFAULT '',
    size            TEXT NOT NULL DEFAULT '', -- legacy freeform; superseded by width/depth/height
    width_cm        REAL NOT NULL DEFAULT 0,
    depth_cm        REAL NOT NULL DEFAULT 0,
    height_cm       REAL NOT NULL DEFAULT 0,
    target_temp     REAL NOT NULL DEFAULT 24,
    target_humidity REAL NOT NULL DEFAULT 55,
    target_co2      REAL NOT NULL DEFAULT 0,
    emergency_temp  REAL NOT NULL DEFAULT 35,
    leaf_temp_offset REAL NOT NULL DEFAULT -2
);
CREATE TABLE IF NOT EXISTS locations (
    id      TEXT PRIMARY KEY,
    name    TEXT NOT NULL,
    lat     REAL NOT NULL DEFAULT 0,
    lon     REAL NOT NULL DEFAULT 0,
    address TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS cycles (
    environment_id TEXT PRIMARY KEY,
    strain         TEXT NOT NULL DEFAULT '',
    started_at     INTEGER NOT NULL DEFAULT 0,
    phase          TEXT NOT NULL DEFAULT '',
    phase_started  INTEGER NOT NULL DEFAULT 0,
    notes          TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS light_schedules (
    environment_id TEXT PRIMARY KEY,
    mode           TEXT NOT NULL DEFAULT 'off',
    lights_on_at   TEXT NOT NULL DEFAULT '06:00',
    on_hours       REAL NOT NULL DEFAULT 18,
    phase_on_hours TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS bindings (
    id             TEXT PRIMARY KEY,
	device_id      TEXT NOT NULL,
	device_name    TEXT NOT NULL,
	power_controller_id TEXT NOT NULL DEFAULT '',
	controller_channel_id TEXT NOT NULL DEFAULT '',
    environment_id TEXT NOT NULL,
    kind           TEXT NOT NULL,
    name           TEXT NOT NULL,
    entity         TEXT NOT NULL,
    measurement    TEXT NOT NULL DEFAULT '',
    role           TEXT NOT NULL DEFAULT '',
    rpm_entity     TEXT NOT NULL DEFAULT '',
    wattage        REAL NOT NULL DEFAULT 0,
    is_primary     INTEGER NOT NULL DEFAULT 0,
	max_rpm        INTEGER NOT NULL DEFAULT 0,
	size_mm        INTEGER NOT NULL DEFAULT 0,
	airflow_cfm    REAL NOT NULL DEFAULT 0,
	static_pressure REAL NOT NULL DEFAULT 0,
	starting_voltage REAL NOT NULL DEFAULT 0,
    created        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_bindings_env ON bindings (environment_id);
CREATE TABLE IF NOT EXISTS readings (
    environment_id TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    temp           REAL NOT NULL,
    humidity       REAL NOT NULL,
    co2            REAL NOT NULL DEFAULT 0,
    vpd            REAL NOT NULL DEFAULT 0,
    exhaust_speed  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_readings_env_ts ON readings (environment_id, ts);
CREATE TABLE IF NOT EXISTS device_readings (
    binding_id     TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    metric         TEXT NOT NULL,
    value          REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_devreadings ON device_readings (binding_id, ts);
CREATE INDEX IF NOT EXISTS idx_devreadings_env_ts ON device_readings (environment_id, ts);
CREATE TABLE IF NOT EXISTS sensor_readings (
    binding_id     TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    ts             INTEGER NOT NULL,
    measurement    TEXT NOT NULL,
    value          REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sensreadings ON sensor_readings (binding_id, ts);
CREATE INDEX IF NOT EXISTS idx_sensreadings_env_ts ON sensor_readings (environment_id, ts);
CREATE TABLE IF NOT EXISTS weather_readings (
    location_id TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    temp        REAL NOT NULL,
    humidity    REAL NOT NULL,
    pressure    REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (location_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_weatherreadings ON weather_readings (location_id, ts);
CREATE TABLE IF NOT EXISTS activity_log (
    id             TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL DEFAULT '',
    device_id      TEXT NOT NULL DEFAULT '',
    ts             INTEGER NOT NULL,
    level          TEXT NOT NULL,
    type           TEXT NOT NULL,
    message        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activity_ts ON activity_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_activity_env_ts ON activity_log (environment_id, ts DESC);
-- Superseded by the bindings model.
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS devices;
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// The pre-device schema represented every entity as a device. It is
	// intentionally not migrated: entity rows cannot reliably tell us which
	// physical device they belong to.
	var hasDeviceID int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('bindings') WHERE name='device_id'`).Scan(&hasDeviceID); err != nil {
		return err
	}
	if hasDeviceID == 0 {
		if _, err := s.db.Exec(`DROP TABLE bindings; CREATE TABLE bindings (
			id TEXT PRIMARY KEY, device_id TEXT NOT NULL, device_name TEXT NOT NULL,
			environment_id TEXT NOT NULL, kind TEXT NOT NULL, name TEXT NOT NULL,
			entity TEXT NOT NULL, measurement TEXT NOT NULL DEFAULT '', role TEXT NOT NULL DEFAULT '',
			rpm_entity TEXT NOT NULL DEFAULT '', wattage REAL NOT NULL DEFAULT 0,
			is_primary INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL DEFAULT 0
		); CREATE INDEX idx_bindings_env ON bindings (environment_id); CREATE INDEX idx_bindings_device ON bindings (device_id)`); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_bindings_device ON bindings (device_id)`); err != nil {
		return err
	}
	// Additive migrations for databases created before these columns existed.
	for _, m := range []struct{ table, column, def string }{
		{"environments", "model", "TEXT NOT NULL DEFAULT ''"},
		{"environments", "size", "TEXT NOT NULL DEFAULT ''"},
		{"environments", "width_cm", "REAL NOT NULL DEFAULT 0"},
		{"environments", "depth_cm", "REAL NOT NULL DEFAULT 0"},
		{"environments", "height_cm", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "wattage", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "is_primary", "INTEGER NOT NULL DEFAULT 0"},
		{"bindings", "power_controller_id", "TEXT NOT NULL DEFAULT ''"},
		{"bindings", "controller_channel_id", "TEXT NOT NULL DEFAULT ''"},
		{"bindings", "max_rpm", "INTEGER NOT NULL DEFAULT 0"},
		{"bindings", "size_mm", "INTEGER NOT NULL DEFAULT 0"},
		{"bindings", "airflow_cfm", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "static_pressure", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "starting_voltage", "REAL NOT NULL DEFAULT 0"},
		{"environments", "location_id", "TEXT NOT NULL DEFAULT ''"},
		{"environments", "leaf_temp_offset", "REAL NOT NULL DEFAULT -2"},
	} {
		if err := s.ensureColumn(m.table, m.column, m.def); err != nil {
			return err
		}
	}
	// Before controller channels were first-class, controllable HA fan entities
	// were stored as physical fans. Reclassify those capabilities; entityless
	// fan rows remain physical airflow devices.
	if _, err := s.db.Exec(`UPDATE bindings SET kind='controller'
		WHERE kind='fan' AND entity<>'' AND controller_channel_id=''`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE bindings SET device_name='DIY ESP32 controller'
		WHERE device_name='DIY ESP32 dual PC-fan controller'`); err != nil {
		return err
	}
	return nil
}

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
	if e.Kind == "" {
		e.Kind = domain.KindTent
	}
	_, err := s.db.Exec(
		`INSERT INTO environments
		   (id, name, kind, air_source, location_id, model, width_cm, depth_cm, height_cm, target_temp, target_humidity, target_co2, emergency_temp, leaf_temp_offset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, kind=excluded.kind, air_source=excluded.air_source,
		   location_id=excluded.location_id, model=excluded.model,
		   width_cm=excluded.width_cm, depth_cm=excluded.depth_cm, height_cm=excluded.height_cm,
		   target_temp=excluded.target_temp, target_humidity=excluded.target_humidity,
		   target_co2=excluded.target_co2, emergency_temp=excluded.emergency_temp,
		   leaf_temp_offset=excluded.leaf_temp_offset`,
		e.ID, e.Name, string(e.Kind), e.AirSourceID, e.LocationID, e.Model,
		e.WidthCm, e.DepthCm, e.HeightCm,
		e.TargetTempC, e.TargetHumidity, e.TargetCO2, e.EmergencyTempC, e.LeafTempOffsetC,
	)
	if err != nil {
		return err
	}
	return s.writeEnvironmentConfig(e.ID)
}

func (s *Store) UpdateTargets(id string, targetTemp, targetHumidity float64) error {
	res, err := s.db.Exec(
		`UPDATE environments SET target_temp=?, target_humidity=? WHERE id=?`,
		targetTemp, targetHumidity, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("environment %q not found", id)
	}
	return s.writeEnvironmentConfig(id)
}

func (s *Store) Environments() ([]domain.Environment, error) {
	rows, err := s.db.Query(
		`SELECT id, name, kind, air_source, location_id, model, width_cm, depth_cm, height_cm, target_temp, target_humidity, target_co2, emergency_temp, leaf_temp_offset
		 FROM environments ORDER BY kind DESC, name`) // tents before rooms
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Environment
	for rows.Next() {
		var e domain.Environment
		var kind string
		if err := rows.Scan(&e.ID, &e.Name, &kind, &e.AirSourceID, &e.LocationID, &e.Model, &e.WidthCm, &e.DepthCm, &e.HeightCm,
			&e.TargetTempC, &e.TargetHumidity, &e.TargetCO2, &e.EmergencyTempC, &e.LeafTempOffsetC); err != nil {
			return nil, err
		}
		e.Kind = domain.EnvironmentKind(kind)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Locations ---

func (s *Store) SaveLocation(l domain.Location) error {
	_, err := s.db.Exec(
		`INSERT INTO locations (id, name, lat, lon, address) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, lat=excluded.lat, lon=excluded.lon, address=excluded.address`,
		l.ID, l.Name, l.Lat, l.Lon, l.Address)
	return err
}

func (s *Store) Locations() ([]domain.Location, error) {
	rows, err := s.db.Query(`SELECT id, name, lat, lon, address FROM locations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Location
	for rows.Next() {
		var l domain.Location
		if err := rows.Scan(&l.ID, &l.Name, &l.Lat, &l.Lon, &l.Address); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) Location(id string) (domain.Location, bool, error) {
	var l domain.Location
	err := s.db.QueryRow(`SELECT id, name, lat, lon, address FROM locations WHERE id=?`, id).
		Scan(&l.ID, &l.Name, &l.Lat, &l.Lon, &l.Address)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Location{}, false, nil
	}
	if err != nil {
		return domain.Location{}, false, err
	}
	return l, true, nil
}

// DeleteLocation removes a location and clears it from any environments sited
// there (those environments simply become unlocated).
func (s *Store) DeleteLocation(id string) error {
	if _, err := s.db.Exec(`UPDATE environments SET location_id='' WHERE location_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM locations WHERE id=?`, id)
	return err
}

// --- Cycles ---

func (s *Store) SaveCycle(c domain.Cycle) error {
	_, err := s.db.Exec(
		`INSERT INTO cycles (environment_id, strain, started_at, phase, phase_started, notes)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(environment_id) DO UPDATE SET
		   strain=excluded.strain, started_at=excluded.started_at,
		   phase=excluded.phase, phase_started=excluded.phase_started, notes=excluded.notes`,
		c.EnvironmentID, c.Strain, c.StartedAt.UnixMilli(), string(c.Phase), c.PhaseStarted.UnixMilli(), c.Notes,
	)
	return err
}

func (s *Store) DeleteCycle(envID string) error {
	_, err := s.db.Exec(`DELETE FROM cycles WHERE environment_id=?`, envID)
	return err
}

func (s *Store) Cycles() ([]domain.Cycle, error) {
	rows, err := s.db.Query(
		`SELECT environment_id, strain, started_at, phase, phase_started, notes FROM cycles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Cycle
	for rows.Next() {
		var c domain.Cycle
		var started, phaseStarted int64
		var phase string
		if err := rows.Scan(&c.EnvironmentID, &c.Strain, &started, &phase, &phaseStarted, &c.Notes); err != nil {
			return nil, err
		}
		c.StartedAt = time.UnixMilli(started)
		c.PhaseStarted = time.UnixMilli(phaseStarted)
		c.Phase = domain.Phase(phase)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Light schedules ---

func (s *Store) SaveLightSchedule(sched domain.LightSchedule) error {
	phases, err := json.Marshal(sched.PhaseOnHours)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO light_schedules (environment_id, mode, lights_on_at, on_hours, phase_on_hours)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(environment_id) DO UPDATE SET
		   mode=excluded.mode, lights_on_at=excluded.lights_on_at,
		   on_hours=excluded.on_hours, phase_on_hours=excluded.phase_on_hours`,
		sched.EnvironmentID, string(sched.Mode), sched.LightsOnAt, sched.OnHours, string(phases),
	)
	return err
}

// LightSchedule returns the saved schedule for an environment, or a default
// (manual) schedule with found=false when none is stored.
func (s *Store) LightSchedule(envID string) (sched domain.LightSchedule, found bool, err error) {
	row := s.db.QueryRow(
		`SELECT mode, lights_on_at, on_hours, phase_on_hours FROM light_schedules WHERE environment_id=?`, envID)
	sched, err = scanLightSchedule(envID, row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DefaultLightSchedule(envID), false, nil
	}
	if err != nil {
		return domain.LightSchedule{}, false, err
	}
	return sched, true, nil
}

func (s *Store) LightSchedules() ([]domain.LightSchedule, error) {
	rows, err := s.db.Query(`SELECT environment_id, mode, lights_on_at, on_hours, phase_on_hours FROM light_schedules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LightSchedule
	for rows.Next() {
		var envID string
		sched, err := scanLightSchedule("", func(dst ...any) error {
			return rows.Scan(append([]any{&envID}, dst...)...)
		})
		if err != nil {
			return nil, err
		}
		sched.EnvironmentID = envID
		out = append(out, sched)
	}
	return out, rows.Err()
}

func (s *Store) DeleteLightSchedule(envID string) error {
	_, err := s.db.Exec(`DELETE FROM light_schedules WHERE environment_id=?`, envID)
	return err
}

// scanLightSchedule decodes the mode/on-at/hours/phase-overrides columns via
// the given scan function into a schedule (EnvironmentID left to the caller).
func scanLightSchedule(envID string, scan func(dst ...any) error) (domain.LightSchedule, error) {
	var mode, onAt, phasesJSON string
	var onHours float64
	if err := scan(&mode, &onAt, &onHours, &phasesJSON); err != nil {
		return domain.LightSchedule{}, err
	}
	phaseOn := map[domain.Phase]float64{}
	if phasesJSON != "" {
		_ = json.Unmarshal([]byte(phasesJSON), &phaseOn)
	}
	return domain.LightSchedule{
		EnvironmentID: envID,
		Mode:          domain.LightScheduleMode(mode),
		LightsOnAt:    onAt,
		OnHours:       onHours,
		PhaseOnHours:  phaseOn,
	}, nil
}

// DeleteEnvironment removes an environment. It fails if bindings still
// reference it, or if another environment uses it as an air source.
func (s *Store) DeleteEnvironment(id string) error {
	var bindings, refs int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bindings WHERE environment_id=?`, id).Scan(&bindings); err != nil {
		return err
	}
	if bindings > 0 {
		return fmt.Errorf("environment %q still has %d binding(s)", id, bindings)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM environments WHERE air_source=?`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("environment %q is used as an air source by %d tent(s)", id, refs)
	}
	res, err := s.db.Exec(`DELETE FROM environments WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("environment %q not found", id)
	}
	_, _ = s.db.Exec(`DELETE FROM light_schedules WHERE environment_id=?`, id)
	return s.removeEnvironmentConfig(id)
}

// --- Bindings ---

func (s *Store) SaveBinding(b domain.Binding) error {
	_, err := s.db.Exec(
		`INSERT INTO bindings (id, device_id, device_name, power_controller_id, controller_channel_id, environment_id, kind, name, entity, measurement, role, rpm_entity, wattage, is_primary, size_mm, max_rpm, airflow_cfm, static_pressure, starting_voltage, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   device_id=excluded.device_id, device_name=excluded.device_name,
		   power_controller_id=excluded.power_controller_id,
		   controller_channel_id=excluded.controller_channel_id,
		   environment_id=excluded.environment_id, kind=excluded.kind, name=excluded.name,
		   entity=excluded.entity, measurement=excluded.measurement, role=excluded.role,
		   rpm_entity=excluded.rpm_entity, wattage=excluded.wattage, is_primary=excluded.is_primary,
		   size_mm=excluded.size_mm, max_rpm=excluded.max_rpm, airflow_cfm=excluded.airflow_cfm,
		   static_pressure=excluded.static_pressure, starting_voltage=excluded.starting_voltage`,
		b.ID, b.DeviceID, b.DeviceName, b.PowerControllerID, b.ControllerChannelID, b.EnvironmentID, string(b.Kind), b.Name, b.Entity,
		string(b.Measurement), string(b.Role), b.RPMEntity, b.Wattage, boolToInt(b.Primary), b.SizeMM, b.MaxRPM, b.AirflowCFM, b.StaticPressureMMH2O, b.StartingVoltage, time.Now().UnixNano(),
	)
	if err != nil {
		return err
	}
	return s.writeEnvironmentConfig(b.EnvironmentID)
}

func (s *Store) DeleteBinding(id string) error {
	var envID string
	_ = s.db.QueryRow(`SELECT environment_id FROM bindings WHERE id=?`, id).Scan(&envID)
	// Removing a power controller disconnects any fixtures assigned to it.
	if _, err := s.db.Exec(`UPDATE bindings SET power_controller_id=''
		WHERE power_controller_id=(SELECT device_id FROM bindings WHERE id=?)`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM bindings WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("binding %q not found", id)
	}
	return s.writeEnvironmentConfig(envID)
}

// SetPrimaryLight makes bindingID the sole primary light in its environment.
func (s *Store) SetPrimaryLight(envID, bindingID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(
		`UPDATE bindings SET is_primary=0 WHERE environment_id=? AND kind='light'`, envID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE bindings SET is_primary=1 WHERE id=? AND kind='light'`, bindingID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.writeEnvironmentConfig(envID)
}

// EnsurePrimaryLight guarantees that, if an environment has any lights, exactly
// one is primary — promoting the oldest light when none is currently marked.
func (s *Store) EnsurePrimaryLight(envID string) error {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM bindings WHERE environment_id=? AND kind='light' AND is_primary=1`,
		envID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil // a primary already exists
	}
	var oldest string
	err := s.db.QueryRow(
		`SELECT id FROM bindings WHERE environment_id=? AND kind='light' ORDER BY created LIMIT 1`,
		envID).Scan(&oldest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no lights at all
	}
	if err != nil {
		return err
	}
	return s.SetPrimaryLight(envID, oldest)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) Bindings() ([]domain.Binding, error) {
	rows, err := s.db.Query(
		`SELECT id, device_id, device_name, power_controller_id, controller_channel_id, environment_id, kind, name, entity, measurement, role, rpm_entity, wattage, is_primary, size_mm, max_rpm, airflow_cfm, static_pressure, starting_voltage
		 FROM bindings ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Binding
	for rows.Next() {
		var b domain.Binding
		var kind, measurement, role string
		var isPrimary int
		if err := rows.Scan(&b.ID, &b.DeviceID, &b.DeviceName, &b.PowerControllerID, &b.ControllerChannelID, &b.EnvironmentID, &kind, &b.Name, &b.Entity, &measurement, &role, &b.RPMEntity, &b.Wattage, &isPrimary, &b.SizeMM, &b.MaxRPM, &b.AirflowCFM, &b.StaticPressureMMH2O, &b.StartingVoltage); err != nil {
			return nil, err
		}
		b.Kind = domain.BindingKind(kind)
		b.Measurement = domain.Measurement(measurement)
		b.Role = domain.Role(role)
		b.Primary = isPrimary != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

// --- Readings history ---

func (s *Store) InsertReading(r domain.Reading) error {
	_, err := s.db.Exec(
		`INSERT INTO readings (environment_id, ts, temp, humidity, co2, vpd, exhaust_speed)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.EnvironmentID, r.Time.UnixMilli(), r.TempC, r.Humidity, r.CO2, r.VPD, r.ExhaustSpeed,
	)
	return err
}

// RecentReadings returns up to limit most-recent readings, oldest first.
func (s *Store) RecentReadings(envID string, limit int) ([]domain.Reading, error) {
	rows, err := s.db.Query(
		`SELECT ts, temp, humidity, co2, vpd, exhaust_speed FROM readings
		 WHERE environment_id=? ORDER BY ts DESC LIMIT ?`, envID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Reading
	for rows.Next() {
		r := domain.Reading{EnvironmentID: envID}
		var ts int64
		if err := rows.Scan(&ts, &r.TempC, &r.Humidity, &r.CO2, &r.VPD, &r.ExhaustSpeed); err != nil {
			return nil, err
		}
		r.Time = time.UnixMilli(ts)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ReadingsSince returns readings newer than since, averaged into at most
// buckets evenly-spaced time buckets (oldest first). Downsampling keeps a
// multi-day window to a chart-friendly number of points.
func (s *Store) ReadingsSince(envID string, since time.Time, buckets int) ([]domain.Reading, error) {
	if buckets < 1 {
		buckets = 1
	}
	sinceMs := since.UnixMilli()
	windowMs := time.Now().UnixMilli() - sinceMs
	bucketMs := windowMs / int64(buckets)
	if bucketMs < 1 {
		bucketMs = 1
	}
	rows, err := s.db.Query(
		`SELECT CAST(AVG(ts) AS INTEGER) AS ts, AVG(temp), AVG(humidity), AVG(co2), AVG(vpd), CAST(AVG(exhaust_speed) AS INTEGER)
		 FROM readings
		 WHERE environment_id=? AND ts>=?
		 GROUP BY (ts - ?) / ?
		 ORDER BY ts`, envID, sinceMs, sinceMs, bucketMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Reading
	for rows.Next() {
		r := domain.Reading{EnvironmentID: envID}
		var ts int64
		if err := rows.Scan(&ts, &r.TempC, &r.Humidity, &r.CO2, &r.VPD, &r.ExhaustSpeed); err != nil {
			return nil, err
		}
		r.Time = time.UnixMilli(ts)
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertDeviceReadings persists a batch of per-device samples in one transaction.
func (s *Store) InsertDeviceReadings(rs []domain.DeviceReading) error {
	if len(rs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO device_readings (binding_id, environment_id, ts, metric, value) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rs {
		if _, err := stmt.Exec(r.BindingID, r.EnvironmentID, r.Time.UnixMilli(), r.Metric, r.Value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// DeviceReadingsSince returns per-device series newer than since, each averaged
// into at most buckets time buckets (oldest first).
func (s *Store) DeviceReadingsSince(envID string, since time.Time, buckets int) ([]domain.DeviceSeries, error) {
	if buckets < 1 {
		buckets = 1
	}
	sinceMs := since.UnixMilli()
	bucketMs := (time.Now().UnixMilli() - sinceMs) / int64(buckets)
	if bucketMs < 1 {
		bucketMs = 1
	}
	rows, err := s.db.Query(
		`SELECT binding_id, metric, CAST(AVG(ts) AS INTEGER) AS ts, AVG(value)
		 FROM device_readings
		 WHERE environment_id=? AND ts>=?
		 GROUP BY binding_id, metric, (ts - ?) / ?
		 ORDER BY binding_id, metric, ts`, envID, sinceMs, sinceMs, bucketMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]*domain.DeviceSeries{}
	var order []*domain.DeviceSeries
	for rows.Next() {
		var bindingID, metric string
		var ts int64
		var value float64
		if err := rows.Scan(&bindingID, &metric, &ts, &value); err != nil {
			return nil, err
		}
		key := bindingID + "\x00" + metric
		ser, ok := byKey[key]
		if !ok {
			ser = &domain.DeviceSeries{BindingID: bindingID, Metric: metric}
			byKey[key] = ser
			order = append(order, ser)
		}
		ser.Points = append(ser.Points, domain.SeriesPoint{Time: time.UnixMilli(ts), Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.DeviceSeries, 0, len(order))
	for _, ser := range order {
		out = append(out, *ser)
	}
	return out, nil
}

// InsertSensorReadings persists a batch of per-sensor samples in one transaction.
func (s *Store) InsertSensorReadings(rs []domain.SensorSample) error {
	if len(rs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO sensor_readings (binding_id, environment_id, ts, measurement, value) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rs {
		if _, err := stmt.Exec(r.BindingID, r.EnvironmentID, r.Time.UnixMilli(), string(r.Measurement), r.Value); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// SensorReadingsSince returns per-sensor series newer than since, each averaged
// into at most buckets time buckets (oldest first). Names and entities are
// joined from the current bindings so the series can label itself; a sensor
// that has since been unbound falls back to its id.
func (s *Store) SensorReadingsSince(envID string, since time.Time, buckets int) ([]domain.SensorSeries, error) {
	if buckets < 1 {
		buckets = 1
	}
	sinceMs := since.UnixMilli()
	bucketMs := (time.Now().UnixMilli() - sinceMs) / int64(buckets)
	if bucketMs < 1 {
		bucketMs = 1
	}
	rows, err := s.db.Query(
		`SELECT sr.binding_id, sr.measurement, COALESCE(b.name, sr.binding_id), COALESCE(b.entity, ''),
		        CAST(AVG(sr.ts) AS INTEGER) AS ts, AVG(sr.value)
		 FROM sensor_readings sr
		 LEFT JOIN bindings b ON b.id = sr.binding_id
		 WHERE sr.environment_id=? AND sr.ts>=?
		 GROUP BY sr.binding_id, sr.measurement, (sr.ts - ?) / ?
		 ORDER BY sr.binding_id, sr.ts`, envID, sinceMs, sinceMs, bucketMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]*domain.SensorSeries{}
	var order []*domain.SensorSeries
	for rows.Next() {
		var bindingID, measurement, name, entity string
		var ts int64
		var value float64
		if err := rows.Scan(&bindingID, &measurement, &name, &entity, &ts, &value); err != nil {
			return nil, err
		}
		ser, ok := byKey[bindingID]
		if !ok {
			ser = &domain.SensorSeries{BindingID: bindingID, Name: name, Entity: entity, Measurement: domain.Measurement(measurement)}
			byKey[bindingID] = ser
			order = append(order, ser)
		}
		ser.Points = append(ser.Points, domain.SeriesPoint{Time: time.UnixMilli(ts), Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.SensorSeries, 0, len(order))
	for _, ser := range order {
		out = append(out, *ser)
	}
	return out, nil
}

// SaveWeatherReadings upserts outdoor observations, keyed by (location, ts) so
// overlapping polls (Open-Meteo returns several past days each call) dedupe.
func (s *Store) SaveWeatherReadings(rs []domain.WeatherSample) error {
	if len(rs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO weather_readings (location_id, ts, temp, humidity, pressure) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rs {
		if _, err := stmt.Exec(r.LocationID, r.Time.UnixMilli(), r.Temp, r.Humidity, r.Pressure); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// WeatherReadingsSince returns a location's outdoor history newer than since,
// averaged into at most buckets time buckets (oldest first).
func (s *Store) WeatherReadingsSince(locationID string, since time.Time, buckets int) (domain.WeatherHistory, error) {
	var out domain.WeatherHistory
	if buckets < 1 {
		buckets = 1
	}
	sinceMs := since.UnixMilli()
	bucketMs := (time.Now().UnixMilli() - sinceMs) / int64(buckets)
	if bucketMs < 1 {
		bucketMs = 1
	}
	rows, err := s.db.Query(
		`SELECT CAST(AVG(ts) AS INTEGER) AS ts, AVG(temp), AVG(humidity), AVG(pressure)
		 FROM weather_readings
		 WHERE location_id=? AND ts>=?
		 GROUP BY (ts - ?) / ?
		 ORDER BY ts`, locationID, sinceMs, sinceMs, bucketMs)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		var temp, hum, pres float64
		if err := rows.Scan(&ts, &temp, &hum, &pres); err != nil {
			return out, err
		}
		t := time.UnixMilli(ts)
		out.Temp = append(out.Temp, domain.SeriesPoint{Time: t, Value: temp})
		out.Humidity = append(out.Humidity, domain.SeriesPoint{Time: t, Value: hum})
		out.Pressure = append(out.Pressure, domain.SeriesPoint{Time: t, Value: pres})
	}
	return out, rows.Err()
}

func (s *Store) AddActivity(a domain.Activity) error {
	if a.Time.IsZero() {
		a.Time = time.Now()
	}
	if a.ID == "" {
		a.ID = fmt.Sprintf("activity-%d", a.Time.UnixNano())
	}
	_, err := s.db.Exec(`INSERT INTO activity_log (id, environment_id, device_id, ts, level, type, message)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, a.ID, a.EnvironmentID, a.DeviceID, a.Time.UnixMilli(), a.Level, a.Type, a.Message)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM activity_log WHERE id IN (SELECT id FROM activity_log ORDER BY ts DESC LIMIT -1 OFFSET 10000)`)
	return nil
}

func (s *Store) Activities(envID string, limit int) ([]domain.Activity, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, environment_id, device_id, ts, level, type, message FROM activity_log`
	args := []any{}
	if envID != "" {
		query += ` WHERE environment_id=?`
		args = append(args, envID)
	}
	query += ` ORDER BY ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Activity
	for rows.Next() {
		var a domain.Activity
		var ts int64
		if err := rows.Scan(&a.ID, &a.EnvironmentID, &a.DeviceID, &ts, &a.Level, &a.Type, &a.Message); err != nil {
			return nil, err
		}
		a.Time = time.UnixMilli(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}
