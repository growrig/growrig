// Package store persists Grow Core configuration and history in SQLite.
//
// It uses the pure-Go modernc.org/sqlite driver so Grow Core builds and runs
// without CGO, which keeps cross-compilation for the Grow Hub simple.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/growrig/growrig/growcore/internal/domain"
)

type Store struct {
	db         *sql.DB
	configDir  string
	feedingDir string
	syncing    bool
}

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. Use ":memory:" for ephemeral runs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc/sqlite is safest single-writer
	s := &Store{
		db:         db,
		configDir:  filepath.Join(filepath.Dir(path), "environments"),
		feedingDir: filepath.Join(filepath.Dir(path), "feedings"),
	}
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

// inClause builds a "?,?,…" placeholder list and the matching args slice for a
// SQL IN (…) predicate over the given ids.
func inClause(ids []string) (string, []any) {
	args := make([]any, len(ids))
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
		args[i] = id
	}
	return b.String(), args
}

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
    phase_on_hours TEXT NOT NULL DEFAULT '{}' -- JSON map keyed by stage name
);
CREATE TABLE IF NOT EXISTS grows (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    slug          TEXT NOT NULL DEFAULT '', -- URL-friendly form of name, derived on save
    species       TEXT NOT NULL DEFAULT '',
    stage         TEXT NOT NULL DEFAULT '',
    stages        TEXT NOT NULL DEFAULT '[]', -- JSON array of stage names
    started_at    INTEGER NOT NULL DEFAULT 0,
    stage_started INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'active',
    notes         TEXT NOT NULL DEFAULT '',
    care_config   TEXT NOT NULL DEFAULT '' -- JSON GrowCareConfig; '' = species defaults
);
CREATE TABLE IF NOT EXISTS plant_units (
    id         TEXT PRIMARY KEY,
    grow_id    TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    slug       TEXT NOT NULL DEFAULT '', -- URL-friendly form of label, derived on save
    cultivar   TEXT NOT NULL DEFAULT '',
    tracking   TEXT NOT NULL DEFAULT 'group',
    quantity   INTEGER NOT NULL DEFAULT 1,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_plant_units_grow ON plant_units (grow_id);
CREATE TABLE IF NOT EXISTS plant_placements (
    id             TEXT PRIMARY KEY,
    plant_unit_id  TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    started_at     INTEGER NOT NULL DEFAULT 0,
    ended_at       INTEGER, -- NULL = current placement
    position       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_placements_unit ON plant_placements (plant_unit_id);
CREATE INDEX IF NOT EXISTS idx_placements_env_open ON plant_placements (environment_id, ended_at);
CREATE TABLE IF NOT EXISTS plant_pots (
    id             TEXT PRIMARY KEY,
    plant_unit_id  TEXT NOT NULL,
    size           REAL NOT NULL DEFAULT 0,
    unit           TEXT NOT NULL DEFAULT '',   -- 'L' or 'gal'
    type           TEXT NOT NULL DEFAULT '',   -- pot material/kind
    started_at     INTEGER NOT NULL DEFAULT 0,
    ended_at       INTEGER                     -- NULL = current pot
);
CREATE INDEX IF NOT EXISTS idx_pots_unit ON plant_pots (plant_unit_id);
CREATE TABLE IF NOT EXISTS cultivars (
    id          TEXT PRIMARY KEY,
    species     TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    creator     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    attributes  TEXT NOT NULL DEFAULT '{}', -- JSON map keyed by the species' attribute keys
    image_data  BLOB,
    image_type  TEXT NOT NULL DEFAULT '',   -- '' = no image
    created_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_cultivars_species ON cultivars (species);
CREATE TABLE IF NOT EXISTS inventory_items (
    id           TEXT PRIMARY KEY,
    category     TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    quantity     REAL NOT NULL DEFAULT 0,
    unit         TEXT NOT NULL DEFAULT '',
    variant      TEXT NOT NULL DEFAULT '',   -- pack size (e.g. "1 L") for product variants
    location     TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active',
    low_stock_at REAL NOT NULL DEFAULT 0,
    notes        TEXT NOT NULL DEFAULT '',
    attributes   TEXT NOT NULL DEFAULT '{}', -- JSON map keyed by the category's column keys
    variants     TEXT NOT NULL DEFAULT '[]', -- JSON array of {size, quantity, lowStockAt} stock lines
    product_id   TEXT NOT NULL DEFAULT '',   -- bound built-in product template, "<category>/<id>"
    image_data   BLOB,
    image_type   TEXT NOT NULL DEFAULT '',   -- '' = no user-uploaded image
    created_at   INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_inventory_category ON inventory_items (category);
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
	duct_size_inches REAL NOT NULL DEFAULT 0,
	noise_dba       REAL NOT NULL DEFAULT 0,
	fan_type        TEXT NOT NULL DEFAULT '',
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
    grow_id        TEXT NOT NULL DEFAULT '',
    device_id      TEXT NOT NULL DEFAULT '',
    ts             INTEGER NOT NULL,
    level          TEXT NOT NULL,
    type           TEXT NOT NULL,
    message        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activity_ts ON activity_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_activity_env_ts ON activity_log (environment_id, ts DESC);
CREATE TABLE IF NOT EXISTS users (
    id             TEXT PRIMARY KEY,
    username       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash  TEXT NOT NULL,
    password_salt  TEXT NOT NULL,
    role           TEXT NOT NULL DEFAULT 'user',
    disabled       INTEGER NOT NULL DEFAULT 0,
    created        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS env_access (
    user_id        TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    access         TEXT NOT NULL DEFAULT 'read',
    PRIMARY KEY (user_id, environment_id)
);
CREATE INDEX IF NOT EXISTS idx_env_access_user ON env_access (user_id);
CREATE TABLE IF NOT EXISTS sessions (
    token_hash     TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL,
    created        INTEGER NOT NULL DEFAULT 0,
    expires        INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id      TEXT PRIMARY KEY,           -- base64url(credential id)
    user_id TEXT NOT NULL,
    name    TEXT NOT NULL DEFAULT '',
    created INTEGER NOT NULL DEFAULT 0,
    data    TEXT NOT NULL               -- JSON-encoded webauthn.Credential record
);
CREATE INDEX IF NOT EXISTS idx_webauthn_user ON webauthn_credentials (user_id);
CREATE TABLE IF NOT EXISTS care_events (
    id          TEXT PRIMARY KEY,
    grow_id     TEXT NOT NULL,
    type        TEXT NOT NULL,
    occurred_at INTEGER NOT NULL,
    source      TEXT NOT NULL DEFAULT 'manual',
    notes       TEXT NOT NULL DEFAULT '',
    recipe_id   TEXT NOT NULL DEFAULT '',
    ph          REAL NOT NULL DEFAULT 0,
    ec          REAL NOT NULL DEFAULT 0,
    runoff_ml   REAL NOT NULL DEFAULT 0,
    runoff_ph   REAL NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_care_grow_ts ON care_events (grow_id, occurred_at DESC);
CREATE TABLE IF NOT EXISTS care_applications (
    id            TEXT PRIMARY KEY,
    care_event_id TEXT NOT NULL,
    plant_unit_id TEXT NOT NULL,
    amount_ml     REAL NOT NULL DEFAULT 0,
    note          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_care_app_event ON care_applications (care_event_id);
CREATE INDEX IF NOT EXISTS idx_care_app_plant ON care_applications (plant_unit_id);
CREATE TABLE IF NOT EXISTS integration_instances (
    id             TEXT PRIMARY KEY,
    bundle_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    config         TEXT NOT NULL DEFAULT '{}',
    secrets        TEXT NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1,
    status         TEXT NOT NULL DEFAULT 'unknown',
    status_message TEXT NOT NULL DEFAULT '',
    last_checked   INTEGER NOT NULL DEFAULT 0,
    created        INTEGER NOT NULL,
    updated        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_integration_bundle ON integration_instances (bundle_id);
CREATE TABLE IF NOT EXISTS integration_bindings (
    id            TEXT PRIMARY KEY,
    feature       TEXT NOT NULL,
    grow_id       TEXT NOT NULL DEFAULT '',
    environment_id TEXT NOT NULL DEFAULT '',
    capability    TEXT NOT NULL,
    instance_id   TEXT NOT NULL,
    created       INTEGER NOT NULL,
    updated       INTEGER NOT NULL,
    UNIQUE(feature, grow_id, environment_id, capability)
);
CREATE INDEX IF NOT EXISTS idx_integration_binding_instance ON integration_bindings (instance_id);
CREATE TABLE IF NOT EXISTS ai_chats (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    grow_id     TEXT NOT NULL,
    environment_id TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL DEFAULT '',
    archived    INTEGER NOT NULL DEFAULT 0,
    created     INTEGER NOT NULL,
    updated     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_chats_user_updated ON ai_chats (user_id, archived, updated DESC);
CREATE INDEX IF NOT EXISTS idx_ai_chats_grow ON ai_chats (user_id, grow_id, updated DESC);
CREATE TABLE IF NOT EXISTS ai_chat_messages (
    id      TEXT PRIMARY KEY,
    chat_id TEXT NOT NULL,
    role    TEXT NOT NULL,
    content TEXT NOT NULL,
    created INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_chat_messages_chat ON ai_chat_messages (chat_id, created, id);
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
	// Environment-scoped integration bindings expanded the original global/grow
	// uniqueness key. Rebuild older tables because SQLite cannot drop the old
	// UNIQUE(feature, grow_id, capability) constraint in place.
	var hasIntegrationEnvironment int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('integration_bindings') WHERE name='environment_id'`).Scan(&hasIntegrationEnvironment); err != nil {
		return err
	}
	if hasIntegrationEnvironment == 0 {
		if _, err := s.db.Exec(`
			DROP INDEX IF EXISTS idx_integration_binding_instance;
			ALTER TABLE integration_bindings RENAME TO integration_bindings_legacy;
			CREATE TABLE integration_bindings (
				id TEXT PRIMARY KEY, feature TEXT NOT NULL, grow_id TEXT NOT NULL DEFAULT '',
				environment_id TEXT NOT NULL DEFAULT '', capability TEXT NOT NULL,
				instance_id TEXT NOT NULL, created INTEGER NOT NULL, updated INTEGER NOT NULL,
				UNIQUE(feature, grow_id, environment_id, capability));
			INSERT INTO integration_bindings (id, feature, grow_id, environment_id, capability, instance_id, created, updated)
				SELECT id, feature, grow_id, '', capability, instance_id, created, updated FROM integration_bindings_legacy;
			DROP TABLE integration_bindings_legacy;
			CREATE INDEX idx_integration_binding_instance ON integration_bindings (instance_id);`); err != nil {
			return err
		}
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
		{"bindings", "duct_size_inches", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "noise_dba", "REAL NOT NULL DEFAULT 0"},
		{"bindings", "fan_type", "TEXT NOT NULL DEFAULT ''"},
		{"environments", "location_id", "TEXT NOT NULL DEFAULT ''"},
		{"environments", "leaf_temp_offset", "REAL NOT NULL DEFAULT -2"},
		{"environments", "control_grow_id", "TEXT NOT NULL DEFAULT ''"},
		{"plant_units", "cultivar", "TEXT NOT NULL DEFAULT ''"},
		{"bindings", "stream_url", "TEXT NOT NULL DEFAULT ''"},
		{"bindings", "camera_type", "TEXT NOT NULL DEFAULT ''"},
		{"bindings", "camera_capture_interval", "INTEGER NOT NULL DEFAULT 60"},
		{"bindings", "camera_retention_days", "INTEGER NOT NULL DEFAULT 7"},
		{"bindings", "camera_storage_mb", "INTEGER NOT NULL DEFAULT 5120"},
		{"activity_log", "grow_id", "TEXT NOT NULL DEFAULT ''"},
		{"inventory_items", "variants", "TEXT NOT NULL DEFAULT '[]'"},
		{"inventory_items", "product_id", "TEXT NOT NULL DEFAULT ''"},
		{"inventory_items", "image_data", "BLOB"},
		{"inventory_items", "image_type", "TEXT NOT NULL DEFAULT ''"},
		{"grows", "care_config", "TEXT NOT NULL DEFAULT ''"},
		{"ai_chats", "environment_id", "TEXT NOT NULL DEFAULT ''"},
		{"grows", "slug", "TEXT NOT NULL DEFAULT ''"},
		{"plant_units", "slug", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(m.table, m.column, m.def); err != nil {
			return err
		}
	}
	// Backfill slugs for grows and plant units created before the slug columns
	// existed (or persisted with an empty slug).
	if err := s.backfillSlugs(); err != nil {
		return err
	}
	// Index activity by grow; created here (not in the schema DDL) so it runs
	// after the additive grow_id column exists on pre-existing databases.
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_activity_grow_ts ON activity_log (grow_id, ts DESC)`); err != nil {
		return err
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
	if err := s.migrateCyclesToGrows(); err != nil {
		return err
	}
	return nil
}

// migrateCyclesToGrows converts pre-Grows cannabis cycles into crop-neutral
// grows the first time the new schema is applied. Each cycle becomes an active
// grow (species "cannabis", cultivar = strain, current stage = the cycle's
// phase) whose environment is nominated as the control grow, plus a single
// group plant unit placed in that environment. It is a no-op once any grow
// exists, and silently skips if the legacy cycles table is absent.
func (s *Store) migrateCyclesToGrows() error {
	var hasCycles int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cycles'`).Scan(&hasCycles); err != nil {
		return err
	}
	if hasCycles == 0 {
		return nil
	}
	var grows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM grows`).Scan(&grows); err != nil {
		return err
	}
	if grows > 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT environment_id, strain, started_at, phase, phase_started FROM cycles`)
	if err != nil {
		return err
	}
	type legacy struct {
		envID, strain, phase string
		started, phaseStart  int64
	}
	var cycles []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.envID, &l.strain, &l.started, &l.phase, &l.phaseStart); err != nil {
			rows.Close()
			return err
		}
		cycles = append(cycles, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range cycles {
		stage := l.phase
		if stage == "" {
			stage = "vegetative"
		}
		grow := domain.Grow{
			ID:           newID("grow"),
			Name:         l.strain,
			Species:      "cannabis",
			Stage:        stage,
			Stages:       domain.StagePresets["cannabis"],
			StartedAt:    time.UnixMilli(l.started),
			StageStarted: time.UnixMilli(l.phaseStart),
			Status:       domain.GrowActive,
		}
		if grow.Name == "" {
			grow.Name = "Grow"
		}
		if err := s.SaveGrow(grow); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE environments SET control_grow_id=? WHERE id=?`, grow.ID, l.envID); err != nil {
			return err
		}
		unit := domain.PlantUnit{
			ID: newID("plant"), GrowID: grow.ID, Label: "Plants", Cultivar: l.strain,
			Tracking: domain.TrackGroup, Quantity: 1, Status: domain.PlantActive,
			CreatedAt: grow.StartedAt,
		}
		if err := s.SavePlantUnit(unit); err != nil {
			return err
		}
		if _, err := s.db.Exec(
			`INSERT INTO plant_placements (id, plant_unit_id, environment_id, started_at, ended_at, position)
			 VALUES (?, ?, ?, ?, NULL, '')`,
			newID("place"), unit.ID, l.envID, grow.StartedAt.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

// backfillSlugs derives slugs for grow and plant-unit rows whose slug is still
// empty — rows written before the slug columns existed. Slugs are computed in Go
// (SQLite has no equivalent slugify) from the current name/label.
func (s *Store) backfillSlugs() error {
	for _, t := range []struct{ table, source string }{
		{"grows", "name"},
		{"plant_units", "label"},
	} {
		rows, err := s.db.Query("SELECT id, " + t.source + " FROM " + t.table + " WHERE slug=''")
		if err != nil {
			return err
		}
		type row struct{ id, source string }
		var pending []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.source); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, r := range pending {
			slug := domain.Slugify(r.source)
			if slug == "" {
				continue // nothing alphanumeric to slugify; leave empty
			}
			if _, err := s.db.Exec("UPDATE "+t.table+" SET slug=? WHERE id=?", slug, r.id); err != nil {
				return err
			}
		}
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
		   (id, name, kind, air_source, location_id, control_grow_id, model, width_cm, depth_cm, height_cm, target_temp, target_humidity, target_co2, emergency_temp, leaf_temp_offset)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, kind=excluded.kind, air_source=excluded.air_source,
		   location_id=excluded.location_id, control_grow_id=excluded.control_grow_id, model=excluded.model,
		   width_cm=excluded.width_cm, depth_cm=excluded.depth_cm, height_cm=excluded.height_cm,
		   target_temp=excluded.target_temp, target_humidity=excluded.target_humidity,
		   target_co2=excluded.target_co2, emergency_temp=excluded.emergency_temp,
		   leaf_temp_offset=excluded.leaf_temp_offset`,
		e.ID, e.Name, string(e.Kind), e.AirSourceID, e.LocationID, e.ControlGrowID, e.Model,
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
		`SELECT id, name, kind, air_source, location_id, control_grow_id, model, width_cm, depth_cm, height_cm, target_temp, target_humidity, target_co2, emergency_temp, leaf_temp_offset
		 FROM environments ORDER BY kind DESC, name`) // tents before rooms
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Environment
	for rows.Next() {
		var e domain.Environment
		var kind string
		if err := rows.Scan(&e.ID, &e.Name, &kind, &e.AirSourceID, &e.LocationID, &e.ControlGrowID, &e.Model, &e.WidthCm, &e.DepthCm, &e.HeightCm,
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

// newID returns a store-generated identifier with the given prefix, used for
// entities created inside the store (bulk plant units, placements).
func newID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// --- Grows ---

func (s *Store) SaveGrow(g domain.Grow) error {
	stages, err := json.Marshal(g.Stages)
	if err != nil {
		return err
	}
	if g.Status == "" {
		g.Status = domain.GrowActive
	}
	g.Slug = domain.Slugify(g.Name) // always derived from the current name
	_, err = s.db.Exec(
		`INSERT INTO grows (id, name, slug, species, stage, stages, started_at, stage_started, status, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, slug=excluded.slug, species=excluded.species,
		   stage=excluded.stage, stages=excluded.stages, started_at=excluded.started_at,
		   stage_started=excluded.stage_started, status=excluded.status, notes=excluded.notes`,
		g.ID, g.Name, g.Slug, g.Species, g.Stage, string(stages),
		g.StartedAt.UnixMilli(), g.StageStarted.UnixMilli(), string(g.Status), g.Notes,
	)
	return err
}

func scanGrow(scan func(dst ...any) error) (domain.Grow, error) {
	var g domain.Grow
	var stagesJSON, status string
	var started, stageStarted int64
	if err := scan(&g.ID, &g.Name, &g.Slug, &g.Species, &g.Stage, &stagesJSON, &started, &stageStarted, &status, &g.Notes); err != nil {
		return domain.Grow{}, err
	}
	if stagesJSON != "" {
		_ = json.Unmarshal([]byte(stagesJSON), &g.Stages)
	}
	g.StartedAt = time.UnixMilli(started)
	g.StageStarted = time.UnixMilli(stageStarted)
	g.Status = domain.GrowStatus(status)
	return g, nil
}

const growCols = `id, name, slug, species, stage, stages, started_at, stage_started, status, notes`

func (s *Store) Grows() ([]domain.Grow, error) {
	rows, err := s.db.Query(`SELECT ` + growCols + ` FROM grows ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Grow
	for rows.Next() {
		g, err := scanGrow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) Grow(id string) (domain.Grow, bool, error) {
	g, err := scanGrow(s.db.QueryRow(`SELECT `+growCols+` FROM grows WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Grow{}, false, nil
	}
	if err != nil {
		return domain.Grow{}, false, err
	}
	return g, true, nil
}

// DeleteGrow removes a grow together with its plant units and placements, and
// clears it from any environment that used it as the control grow.
func (s *Store) DeleteGrow(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM plant_placements WHERE plant_unit_id IN (SELECT id FROM plant_units WHERE grow_id=?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM plant_units WHERE grow_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM care_applications WHERE care_event_id IN (SELECT id FROM care_events WHERE grow_id=?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM care_events WHERE grow_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE environments SET control_grow_id='' WHERE control_grow_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM grows WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Plant units ---

func (s *Store) SavePlantUnit(u domain.PlantUnit) error {
	if u.Status == "" {
		u.Status = domain.PlantActive
	}
	if u.Quantity <= 0 {
		u.Quantity = 1
	}
	u.Slug = domain.Slugify(u.Label) // always derived from the current label
	_, err := s.db.Exec(
		`INSERT INTO plant_units (id, grow_id, label, slug, cultivar, tracking, quantity, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   grow_id=excluded.grow_id, label=excluded.label, slug=excluded.slug, cultivar=excluded.cultivar,
		   tracking=excluded.tracking, quantity=excluded.quantity, status=excluded.status,
		   created_at=excluded.created_at`,
		u.ID, u.GrowID, u.Label, u.Slug, u.Cultivar, string(u.Tracking), u.Quantity, string(u.Status), u.CreatedAt.UnixMilli(),
	)
	return err
}

func scanPlantUnit(scan func(dst ...any) error) (domain.PlantUnit, error) {
	var u domain.PlantUnit
	var tracking, status string
	var created int64
	if err := scan(&u.ID, &u.GrowID, &u.Label, &u.Slug, &u.Cultivar, &tracking, &u.Quantity, &status, &created); err != nil {
		return domain.PlantUnit{}, err
	}
	u.Tracking = domain.TrackingMode(tracking)
	u.Status = domain.PlantStatus(status)
	u.CreatedAt = time.UnixMilli(created)
	return u, nil
}

const unitCols = `id, grow_id, label, slug, cultivar, tracking, quantity, status, created_at`

// PlantUnits returns the units belonging to a grow, oldest first.
func (s *Store) PlantUnits(growID string) ([]domain.PlantUnit, error) {
	rows, err := s.db.Query(`SELECT `+unitCols+` FROM plant_units WHERE grow_id=? ORDER BY created_at`, growID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantUnit
	for rows.Next() {
		u, err := scanPlantUnit(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// PlantUnitsAll returns every plant unit (used by the control engine to build
// per-grow plant counts).
func (s *Store) PlantUnitsAll() ([]domain.PlantUnit, error) {
	rows, err := s.db.Query(`SELECT ` + unitCols + ` FROM plant_units`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantUnit
	for rows.Next() {
		u, err := scanPlantUnit(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) PlantUnit(id string) (domain.PlantUnit, bool, error) {
	u, err := scanPlantUnit(s.db.QueryRow(`SELECT `+unitCols+` FROM plant_units WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.PlantUnit{}, false, nil
	}
	if err != nil {
		return domain.PlantUnit{}, false, err
	}
	return u, true, nil
}

// --- Plant placements ---

func scanPlacement(scan func(dst ...any) error) (domain.PlantPlacement, error) {
	var p domain.PlantPlacement
	var started int64
	var ended sql.NullInt64
	if err := scan(&p.ID, &p.PlantUnitID, &p.EnvironmentID, &started, &ended, &p.Position); err != nil {
		return domain.PlantPlacement{}, err
	}
	p.StartedAt = time.UnixMilli(started)
	if ended.Valid {
		t := time.UnixMilli(ended.Int64)
		p.EndedAt = &t
	}
	return p, nil
}

const placementCols = `id, plant_unit_id, environment_id, started_at, ended_at, position`

// PlacementsForUnit returns a unit's full placement history, newest first.
func (s *Store) PlacementsForUnit(unitID string) ([]domain.PlantPlacement, error) {
	rows, err := s.db.Query(`SELECT `+placementCols+` FROM plant_placements WHERE plant_unit_id=? ORDER BY started_at DESC`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantPlacement
	for rows.Next() {
		p, err := scanPlacement(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlacementsForUnits loads the placements for many units in one query, grouped
// by unit id (each group newest first, matching PlacementsForUnit), so a grow's
// plants can be hydrated without a query per unit.
func (s *Store) PlacementsForUnits(unitIDs []string) (map[string][]domain.PlantPlacement, error) {
	out := map[string][]domain.PlantPlacement{}
	if len(unitIDs) == 0 {
		return out, nil
	}
	in, args := inClause(unitIDs)
	rows, err := s.db.Query(`SELECT `+placementCols+` FROM plant_placements WHERE plant_unit_id IN (`+in+`) ORDER BY started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPlacement(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.PlantUnitID] = append(out[p.PlantUnitID], p)
	}
	return out, rows.Err()
}

// CurrentPlacements returns every open placement (ended_at IS NULL): where each
// plant unit lives right now.
func (s *Store) CurrentPlacements() ([]domain.PlantPlacement, error) {
	rows, err := s.db.Query(`SELECT ` + placementCols + ` FROM plant_placements WHERE ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantPlacement
	for rows.Next() {
		p, err := scanPlacement(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlantsInEnvironment returns the plant units currently placed in an
// environment (units with an open placement there).
func (s *Store) PlantsInEnvironment(envID string) ([]domain.PlantUnit, error) {
	rows, err := s.db.Query(
		`SELECT u.id, u.grow_id, u.label, u.slug, u.cultivar, u.tracking, u.quantity, u.status, u.created_at
		 FROM plant_units u
		 JOIN plant_placements p ON p.plant_unit_id=u.id
		 WHERE p.environment_id=? AND p.ended_at IS NULL
		 ORDER BY u.grow_id, u.created_at`, envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantUnit
	for rows.Next() {
		u, err := scanPlantUnit(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// BulkCreatePlants creates n plant units for a grow in one transaction, each
// with an opening placement in the given environment. quantityPer sets the
// group size of each unit (1 for individually-tracked plants). It returns the
// created units.
func (s *Store) BulkCreatePlants(growID string, n int, tracking domain.TrackingMode, quantityPer int, labelPrefix, cultivar, envID string, at time.Time) ([]domain.PlantUnit, error) {
	if n <= 0 {
		return nil, fmt.Errorf("count must be positive")
	}
	if quantityPer <= 0 {
		quantityPer = 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	units := make([]domain.PlantUnit, 0, n)
	for i := 0; i < n; i++ {
		label := labelPrefix
		if n > 1 {
			label = fmt.Sprintf("%s %d", labelPrefix, i+1)
		}
		u := domain.PlantUnit{
			ID: newID("plant"), GrowID: growID, Label: label, Slug: domain.Slugify(label), Cultivar: cultivar,
			Tracking: tracking, Quantity: quantityPer, Status: domain.PlantActive, CreatedAt: at,
		}
		if _, err := tx.Exec(
			`INSERT INTO plant_units (id, grow_id, label, slug, cultivar, tracking, quantity, status, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.GrowID, u.Label, u.Slug, u.Cultivar, string(u.Tracking), u.Quantity, string(u.Status), u.CreatedAt.UnixMilli()); err != nil {
			return nil, err
		}
		if envID != "" {
			if _, err := tx.Exec(
				`INSERT INTO plant_placements (id, plant_unit_id, environment_id, started_at, ended_at, position)
				 VALUES (?, ?, ?, ?, NULL, '')`,
				newID("place"), u.ID, envID, at.UnixMilli()); err != nil {
				return nil, err
			}
		}
		units = append(units, u)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return units, nil
}

// MovePlant transactionally closes a unit's current placement and opens a new
// one in toEnvID at time at. It is a no-op error if the unit does not exist.
// The returned bool reports whether an existing placement was closed.
func (s *Store) MovePlant(unitID, toEnvID string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE plant_placements SET ended_at=? WHERE plant_unit_id=? AND ended_at IS NULL`,
		at.UnixMilli(), unitID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO plant_placements (id, plant_unit_id, environment_id, started_at, ended_at, position)
		 VALUES (?, ?, ?, ?, NULL, '')`,
		newID("place"), unitID, toEnvID, at.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Plant pots (repot history, mirroring placements) ---

const potCols = `id, plant_unit_id, size, unit, type, started_at, ended_at`

func scanPot(scan func(dst ...any) error) (domain.PlantPot, error) {
	var p domain.PlantPot
	var started int64
	var ended sql.NullInt64
	if err := scan(&p.ID, &p.PlantUnitID, &p.Size, &p.Unit, &p.Type, &started, &ended); err != nil {
		return domain.PlantPot{}, err
	}
	p.StartedAt = time.UnixMilli(started)
	if ended.Valid {
		t := time.UnixMilli(ended.Int64)
		p.EndedAt = &t
	}
	return p, nil
}

// PotsForUnit returns a unit's full repot history, newest first.
func (s *Store) PotsForUnit(unitID string) ([]domain.PlantPot, error) {
	rows, err := s.db.Query(`SELECT `+potCols+` FROM plant_pots WHERE plant_unit_id=? ORDER BY started_at DESC`, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlantPot
	for rows.Next() {
		p, err := scanPot(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PotsForUnits loads the pot histories for many units in one query, grouped by
// unit id (each group newest first, matching PotsForUnit), so a grow's plants
// can be hydrated without a query per unit.
func (s *Store) PotsForUnits(unitIDs []string) (map[string][]domain.PlantPot, error) {
	out := map[string][]domain.PlantPot{}
	if len(unitIDs) == 0 {
		return out, nil
	}
	in, args := inClause(unitIDs)
	rows, err := s.db.Query(`SELECT `+potCols+` FROM plant_pots WHERE plant_unit_id IN (`+in+`) ORDER BY started_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPot(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[p.PlantUnitID] = append(out[p.PlantUnitID], p)
	}
	return out, rows.Err()
}

// Repot transactionally closes a unit's current pot (if any) and opens a new one
// of the given size/unit/type at time at — recording a repot in the history.
func (s *Store) Repot(unitID string, size float64, unit, potType string, at time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE plant_pots SET ended_at=? WHERE plant_unit_id=? AND ended_at IS NULL`,
		at.UnixMilli(), unitID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO plant_pots (id, plant_unit_id, size, unit, type, started_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		newID("pot"), unitID, size, unit, potType, at.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Light schedules ---

func (s *Store) SaveLightSchedule(sched domain.LightSchedule) error {
	phases, err := json.Marshal(sched.StageOnHours)
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
	var mode, onAt, stagesJSON string
	var onHours float64
	if err := scan(&mode, &onAt, &onHours, &stagesJSON); err != nil {
		return domain.LightSchedule{}, err
	}
	stageOn := map[string]float64{}
	if stagesJSON != "" {
		_ = json.Unmarshal([]byte(stagesJSON), &stageOn)
	}
	return domain.LightSchedule{
		EnvironmentID: envID,
		Mode:          domain.LightScheduleMode(mode),
		LightsOnAt:    onAt,
		OnHours:       onHours,
		StageOnHours:  stageOn,
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
	_, _ = s.db.Exec(`DELETE FROM env_access WHERE environment_id=?`, id)
	return s.removeEnvironmentConfig(id)
}

// --- Bindings ---

func (s *Store) SaveBinding(b domain.Binding) error {
	_, err := s.db.Exec(
		`INSERT INTO bindings (id, device_id, device_name, power_controller_id, controller_channel_id, environment_id, kind, name, entity, measurement, role, rpm_entity, wattage, is_primary, fan_type, size_mm, max_rpm, airflow_cfm, static_pressure, starting_voltage, duct_size_inches, noise_dba, stream_url, camera_type, camera_capture_interval, camera_retention_days, camera_storage_mb, created)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   device_id=excluded.device_id, device_name=excluded.device_name,
		   power_controller_id=excluded.power_controller_id,
		   controller_channel_id=excluded.controller_channel_id,
		   environment_id=excluded.environment_id, kind=excluded.kind, name=excluded.name,
		   entity=excluded.entity, measurement=excluded.measurement, role=excluded.role,
		   rpm_entity=excluded.rpm_entity, wattage=excluded.wattage, is_primary=excluded.is_primary,
		   fan_type=excluded.fan_type, size_mm=excluded.size_mm, max_rpm=excluded.max_rpm, airflow_cfm=excluded.airflow_cfm,
		   static_pressure=excluded.static_pressure, starting_voltage=excluded.starting_voltage,
		   duct_size_inches=excluded.duct_size_inches, noise_dba=excluded.noise_dba,
		   stream_url=excluded.stream_url, camera_type=excluded.camera_type,
		   camera_capture_interval=excluded.camera_capture_interval, camera_retention_days=excluded.camera_retention_days,
		   camera_storage_mb=excluded.camera_storage_mb`,
		b.ID, b.DeviceID, b.DeviceName, b.PowerControllerID, b.ControllerChannelID, b.EnvironmentID, string(b.Kind), b.Name, b.Entity,
		string(b.Measurement), string(b.Role), b.RPMEntity, b.Wattage, boolToInt(b.Primary), b.FanType, b.SizeMM, b.MaxRPM, b.AirflowCFM, b.StaticPressureMMH2O, b.StartingVoltage, b.DuctSizeInches, b.NoiseDBA, b.StreamURL, string(b.CameraType), b.CameraCaptureInterval, b.CameraRetentionDays, b.CameraStorageMB, time.Now().UnixNano(),
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
		`SELECT id, device_id, device_name, power_controller_id, controller_channel_id, environment_id, kind, name, entity, measurement, role, rpm_entity, wattage, is_primary, fan_type, size_mm, max_rpm, airflow_cfm, static_pressure, starting_voltage, duct_size_inches, noise_dba, stream_url, camera_type, camera_capture_interval, camera_retention_days, camera_storage_mb
		 FROM bindings ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Binding
	for rows.Next() {
		var b domain.Binding
		var kind, measurement, role, cameraType string
		var isPrimary int
		if err := rows.Scan(&b.ID, &b.DeviceID, &b.DeviceName, &b.PowerControllerID, &b.ControllerChannelID, &b.EnvironmentID, &kind, &b.Name, &b.Entity, &measurement, &role, &b.RPMEntity, &b.Wattage, &isPrimary, &b.FanType, &b.SizeMM, &b.MaxRPM, &b.AirflowCFM, &b.StaticPressureMMH2O, &b.StartingVoltage, &b.DuctSizeInches, &b.NoiseDBA, &b.StreamURL, &cameraType, &b.CameraCaptureInterval, &b.CameraRetentionDays, &b.CameraStorageMB); err != nil {
			return nil, err
		}
		b.Kind = domain.BindingKind(kind)
		b.Measurement = domain.Measurement(measurement)
		b.Role = domain.Role(role)
		b.CameraType = domain.CameraType(cameraType)
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
	_, err := s.db.Exec(`INSERT INTO activity_log (id, environment_id, grow_id, device_id, ts, level, type, message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, a.EnvironmentID, a.GrowID, a.DeviceID, a.Time.UnixMilli(), a.Level, a.Type, a.Message)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`DELETE FROM activity_log WHERE id IN (SELECT id FROM activity_log ORDER BY ts DESC LIMIT -1 OFFSET 10000)`)
	return nil
}

// activityWhere builds the shared WHERE clause and its args for the activity
// filters, so Activities and CountActivities stay in sync.
func activityWhere(envID, growID string, levels, types []string) (string, []any) {
	where := []string{}
	args := []any{}
	if envID != "" {
		where = append(where, "environment_id=?")
		args = append(args, envID)
	}
	if growID != "" {
		where = append(where, "grow_id=?")
		args = append(args, growID)
	}
	if len(levels) > 0 {
		placeholders := make([]string, len(levels))
		for i, lvl := range levels {
			placeholders[i] = "?"
			args = append(args, lvl)
		}
		where = append(where, "level IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		where = append(where, "type IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// Activities returns recent activity, newest first. envID and growID are
// optional filters; levels, when non-empty, restricts to those severity levels
// (e.g. "warning", "error") so callers can hide routine control/notice noise.
// offset skips that many rows for pagination.
func (s *Store) Activities(envID, growID string, levels, types []string, limit, offset int) ([]domain.Activity, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	clause, args := activityWhere(envID, growID, levels, types)
	query := `SELECT id, environment_id, grow_id, device_id, ts, level, type, message FROM activity_log` + clause + ` ORDER BY ts DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Activity
	for rows.Next() {
		var a domain.Activity
		var ts int64
		if err := rows.Scan(&a.ID, &a.EnvironmentID, &a.GrowID, &a.DeviceID, &ts, &a.Level, &a.Type, &a.Message); err != nil {
			return nil, err
		}
		a.Time = time.UnixMilli(ts)
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountActivities returns the total number of activity rows matching the same
// filters Activities uses, for pagination.
func (s *Store) CountActivities(envID, growID string, levels, types []string) (int, error) {
	clause, args := activityWhere(envID, growID, levels, types)
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM activity_log`+clause, args...).Scan(&n)
	return n, err
}

// ClearActivities removes every activity-log entry.
func (s *Store) ClearActivities() error {
	_, err := s.db.Exec(`DELETE FROM activity_log`)
	return err
}
