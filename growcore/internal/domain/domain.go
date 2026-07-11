// Package domain defines the grow-domain model for Grow Core.
//
// The model is entity-oriented and semantic: an Environment (a grow tent or a
// monitored room such as a lung room) owns a set of typed Bindings that attach
// Home Assistant entities as sensors, fans, lights or cameras. This mirrors how
// Home Assistant exposes things while keeping Grow Core's own roles and targets
// independent. See ../../../growrig/docs/architecture.md.
package domain

import (
	"fmt"
	"time"
)

// EnvironmentKind distinguishes a controlled grow space from a monitored room.
type EnvironmentKind string

const (
	// KindTent is a controlled grow space with targets and control.
	KindTent EnvironmentKind = "tent"
	// KindRoom is a monitored space (e.g. a lung room feeding a tent's intake).
	KindRoom EnvironmentKind = "room"
)

// Role is the grow purpose assigned to a fan channel.
type Role string

const (
	RoleUnassigned  Role = "unassigned"
	RoleExhaust     Role = "exhaust"
	RoleIntake      Role = "intake"
	RoleCirculation Role = "circulation"
)

// AllFanRoles lists the roles a fan may be assigned.
var AllFanRoles = []Role{RoleUnassigned, RoleExhaust, RoleIntake, RoleCirculation}

// BindingKind is the category of thing an entity binding represents.
type BindingKind string

const (
	KindSensor     BindingKind = "sensor"
	KindFan        BindingKind = "fan"
	KindController BindingKind = "controller"
	KindLight      BindingKind = "light"
	KindPower      BindingKind = "power"
	KindCamera     BindingKind = "camera"
)

// Measurement is what a sensor binding measures.
type Measurement string

const (
	MeasureTemperature Measurement = "temperature"
	MeasureHumidity    Measurement = "humidity"
	MeasureCO2         Measurement = "co2"
	MeasurePower       Measurement = "power"
)

// ControllerHealth describes connection/adapter liveness.
type ControllerHealth string

const (
	HealthOnline  ControllerHealth = "online"
	HealthStale   ControllerHealth = "stale"
	HealthOffline ControllerHealth = "offline"
)

// Location is a physical place (a home, a greenhouse site) with geographic
// coordinates, shared by the environments sited there. Coordinates drive local
// weather lookups; the name groups environments on the dashboard.
type Location struct {
	ID      string  `json:"id" yaml:"id"`
	Name    string  `json:"name" yaml:"name"`
	Lat     float64 `json:"lat" yaml:"lat"`
	Lon     float64 `json:"lon" yaml:"lon"`
	Address string  `json:"address" yaml:"address,omitempty"` // geocoder display name
}

// Environment is a controlled tent or a monitored room.
type Environment struct {
	ID   string          `json:"id" yaml:"id"`
	Name string          `json:"name" yaml:"name"`
	Kind EnvironmentKind `json:"kind" yaml:"type"`
	// AirSourceID optionally references the room (lung room) that supplies this
	// tent's intake air. Empty for rooms or tents without a linked source.
	AirSourceID string `json:"airSourceId" yaml:"airSourceId,omitempty"`
	// LocationID optionally sites this environment at a Location (for weather
	// and dashboard grouping).
	LocationID string `json:"locationId" yaml:"locationId,omitempty"`

	// Model is an optional descriptive field (e.g. the grow-tent product)
	// captured by the setup wizard.
	Model string `json:"model" yaml:"tentModel,omitempty"`

	// Tent dimensions in centimetres; 0 = unset. VolumeM3 derives from these.
	WidthCm  float64 `json:"widthCm" yaml:"widthCm,omitempty"`
	DepthCm  float64 `json:"depthCm" yaml:"depthCm,omitempty"`
	HeightCm float64 `json:"heightCm" yaml:"heightCm,omitempty"`

	TargetTempC    float64 `json:"targetTempC" yaml:"targetTempC"`
	TargetHumidity float64 `json:"targetHumidity" yaml:"targetHumidity"`
	TargetCO2      float64 `json:"targetCO2" yaml:"targetCO2,omitempty"` // ppm; 0 = unset
	EmergencyTempC float64 `json:"emergencyTempC" yaml:"emergencyTempC"`
	// LeafTempOffsetC estimates leaf temperature from air temperature for VPD.
	// For example, -2 means leaves are assumed to be 2°C cooler than the air.
	LeafTempOffsetC float64 `json:"leafTempOffsetC" yaml:"leafTempOffsetC,omitempty"`
}

// VolumeM3 returns the tent's air volume in cubic metres, or 0 if any
// dimension is unset.
func (e Environment) VolumeM3() float64 {
	if e.WidthCm <= 0 || e.DepthCm <= 0 || e.HeightCm <= 0 {
		return 0
	}
	return e.WidthCm * e.DepthCm * e.HeightCm / 1_000_000
}

// Phase is a stage of a grow cycle.
type Phase string

const (
	PhaseSeedling   Phase = "seedling"
	PhaseVegetative Phase = "vegetative"
	PhaseFlowering  Phase = "flowering"
	PhaseFlush      Phase = "flush"
	PhaseDrying     Phase = "drying"
	PhaseCure       Phase = "cure"
)

// AllPhases lists grow phases in chronological order.
var AllPhases = []Phase{PhaseSeedling, PhaseVegetative, PhaseFlowering, PhaseFlush, PhaseDrying, PhaseCure}

// Cycle is a running grow in a tent: a strain, a start date, and the current
// phase. One active cycle per environment in this MVP.
type Cycle struct {
	EnvironmentID string    `json:"environmentId"`
	Strain        string    `json:"strain"`
	StartedAt     time.Time `json:"startedAt"`
	Phase         Phase     `json:"phase"`
	PhaseStarted  time.Time `json:"phaseStarted"`
	Notes         string    `json:"notes"`
}

// --- Automations ---
//
// An automation drives an actuator over time rather than in reaction to a
// sensor reading. The light photoperiod schedule is the first (and today only)
// automation type; future types (interval circulation, threshold dehumidify)
// are siblings of LightSchedule, not extensions of it.

// LightScheduleMode selects how a tent's primary grow light is driven.
type LightScheduleMode string

const (
	// LightScheduleOff leaves the light under manual control only.
	LightScheduleOff LightScheduleMode = "off"
	// LightSchedulePhase follows the recommended photoperiod for the cycle's
	// current phase, with optional per-phase overrides.
	LightSchedulePhase LightScheduleMode = "phase"
	// LightScheduleCustom uses a fixed on-time and duration, ignoring the phase.
	LightScheduleCustom LightScheduleMode = "custom"
)

// AllLightScheduleModes lists the selectable schedule modes.
var AllLightScheduleModes = []LightScheduleMode{LightScheduleOff, LightSchedulePhase, LightScheduleCustom}

// PhotoperiodDefaults maps each grow phase to its recommended daily hours of
// light. These are the presets a user starts from and can override per phase.
var PhotoperiodDefaults = map[Phase]float64{
	PhaseSeedling:   18,
	PhaseVegetative: 18,
	PhaseFlowering:  12,
	PhaseFlush:      12,
	PhaseDrying:     0,
	PhaseCure:       0,
}

// LightSchedule is the photoperiod automation for an environment's primary
// grow light. The light turns on at LightsOnAt (a local "HH:MM" wall-clock
// time) and stays on for the effective number of hours; the remainder of the
// 24h day is dark. Anchoring on wall-clock time (rather than counting from an
// arbitrary cycle start) keeps the dark period aligned to a real daily window.
type LightSchedule struct {
	EnvironmentID string            `json:"environmentId"`
	Mode          LightScheduleMode `json:"mode"`
	// LightsOnAt is the local time the light comes on, "HH:MM" (24h).
	LightsOnAt string `json:"lightsOnAt"`
	// OnHours is the on-duration for custom mode.
	OnHours float64 `json:"onHours"`
	// PhaseOnHours holds per-phase overrides for phase mode. A phase absent
	// from the map falls back to PhotoperiodDefaults.
	PhaseOnHours map[Phase]float64 `json:"phaseOnHours"`
}

// DefaultLightSchedule is the schedule for an environment that has none saved:
// manual control, with sensible on-time/duration seeds for the editor.
func DefaultLightSchedule(envID string) LightSchedule {
	return LightSchedule{
		EnvironmentID: envID,
		Mode:          LightScheduleOff,
		LightsOnAt:    "06:00",
		OnHours:       18,
		PhaseOnHours:  map[Phase]float64{},
	}
}

// EffectiveOnHours resolves the on-duration for the given phase: the custom
// duration in custom mode, otherwise the per-phase override or the default.
func (s LightSchedule) EffectiveOnHours(phase Phase) float64 {
	if s.Mode == LightScheduleCustom {
		return clampHours(s.OnHours)
	}
	if h, ok := s.PhaseOnHours[phase]; ok {
		return clampHours(h)
	}
	if h, ok := PhotoperiodDefaults[phase]; ok {
		return h
	}
	return 18
}

// DesiredOn reports whether the light should be on at time now for the given
// phase. ok is false when the schedule is not driving the light (mode off).
func (s LightSchedule) DesiredOn(phase Phase, now time.Time) (on bool, ok bool) {
	if s.Mode == LightScheduleOff {
		return false, false
	}
	hours := s.EffectiveOnHours(phase)
	if hours <= 0 {
		return false, true
	}
	if hours >= 24 {
		return true, true
	}
	onAt, valid := parseHHMM(s.LightsOnAt)
	if !valid {
		return false, false
	}
	mins := now.Hour()*60 + now.Minute()
	span := int(hours * 60)
	return inWindow(mins, onAt, span), true
}

// NextTransition returns the next wall-clock instant at/after now when the
// scheduled light state flips. Used to hold a manual override until the next
// scheduled boundary. It returns the zero time when the schedule never flips
// (mode off, or an always-on / always-off duration).
func (s LightSchedule) NextTransition(phase Phase, now time.Time) time.Time {
	if s.Mode == LightScheduleOff {
		return time.Time{}
	}
	hours := s.EffectiveOnHours(phase)
	if hours <= 0 || hours >= 24 {
		return time.Time{}
	}
	onAt, valid := parseHHMM(s.LightsOnAt)
	if !valid {
		return time.Time{}
	}
	offAt := (onAt + int(hours*60)) % 1440
	return nextClockTime(now, onAt, offAt)
}

func clampHours(h float64) float64 {
	if h < 0 {
		return 0
	}
	if h > 24 {
		return 24
	}
	return h
}

// parseHHMM parses "HH:MM" into minutes-since-midnight.
func parseHHMM(s string) (mins int, ok bool) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// inWindow reports whether minute-of-day t falls in the on-window that starts
// at start and lasts span minutes, wrapping past midnight.
func inWindow(t, start, span int) bool {
	end := start + span
	if end <= 1440 {
		return t >= start && t < end
	}
	// Window wraps midnight: on from start..2400 and 0..(end-1440).
	return t >= start || t < end-1440
}

// nextClockTime returns the soonest instant strictly after now that lands on
// one of the given minute-of-day marks.
func nextClockTime(now time.Time, marks ...int) time.Time {
	var best time.Time
	nowMins := now.Hour()*60 + now.Minute()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, mark := range marks {
		cand := midnight.Add(time.Duration(mark) * time.Minute)
		if mark <= nowMins {
			cand = cand.Add(24 * time.Hour)
		}
		if best.IsZero() || cand.Before(best) {
			best = cand
		}
	}
	return best
}

// Binding attaches a Home Assistant entity (or simulator entity id) to an
// environment with a semantic category.
type Binding struct {
	ID         string `json:"id"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	// PowerControllerID links an entityless light fixture to a separately
	// configured power device.
	PowerControllerID   string      `json:"powerControllerId,omitempty"`
	ControllerChannelID string      `json:"controllerChannelId,omitempty"`
	EnvironmentID       string      `json:"environmentId"`
	Kind                BindingKind `json:"kind"`
	Name                string      `json:"name"`
	Entity              string      `json:"entity"`

	// Sensor only:
	Measurement Measurement `json:"measurement,omitempty"`
	// Fan/controller only:
	Role                Role    `json:"role,omitempty"`
	RPMEntity           string  `json:"rpmEntity,omitempty"`
	SizeMM              int     `json:"sizeMm,omitempty"`
	MaxRPM              int     `json:"maxRpm,omitempty"`
	AirflowCFM          float64 `json:"airflowCfm,omitempty"`
	StaticPressureMMH2O float64 `json:"staticPressureMmH2O,omitempty"`
	StartingVoltage     float64 `json:"startingVoltage,omitempty"`
	// Light only:
	Wattage float64 `json:"wattage,omitempty"` // rated power in watts; 0 = unknown
	Primary bool    `json:"primary,omitempty"` // the box's main grow light (one per env)
}

// Reading is a single historical sample persisted for an environment.
type Reading struct {
	EnvironmentID string    `json:"environmentId"`
	Time          time.Time `json:"time"`
	TempC         float64   `json:"tempC"`
	Humidity      float64   `json:"humidity"`
	CO2           float64   `json:"co2"`
	VPD           float64   `json:"vpd"`
	ExhaustSpeed  int       `json:"exhaustSpeed"`
}

// DeviceReading is a single per-device sample (fan RPM, light power, …)
// persisted for the timeline. Kept separate from Reading, which aggregates
// per-environment climate.
type DeviceReading struct {
	BindingID     string    `json:"bindingId"`
	EnvironmentID string    `json:"environmentId"`
	Time          time.Time `json:"time"`
	Metric        string    `json:"metric"` // "rpm" | "power"
	Value         float64   `json:"value"`
}

// SeriesPoint is one downsampled point in a device series.
type SeriesPoint struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// DeviceSeries is a device's downsampled history for one metric.
type DeviceSeries struct {
	BindingID string        `json:"bindingId"`
	Metric    string        `json:"metric"`
	Points    []SeriesPoint `json:"points"`
}

// SensorSample is a single per-sensor reading persisted for the timeline. Kept
// separate from Reading, which aggregates all sensors of a kind into one
// per-environment climate value.
type SensorSample struct {
	BindingID     string      `json:"bindingId"`
	EnvironmentID string      `json:"environmentId"`
	Time          time.Time   `json:"time"`
	Measurement   Measurement `json:"measurement"`
	Value         float64     `json:"value"`
}

// SensorSeries is one sensor's downsampled history, with enough identity to
// label it in the metric-detail modal.
type SensorSeries struct {
	BindingID   string        `json:"bindingId"`
	Name        string        `json:"name"`
	Entity      string        `json:"entity"`
	Measurement Measurement   `json:"measurement"`
	Points      []SeriesPoint `json:"points"`
}

// WeatherSample is a single persisted outdoor observation for a location.
type WeatherSample struct {
	LocationID string
	Time       time.Time
	Temp       float64
	Humidity   float64
	Pressure   float64
}

// WeatherHistory is a location's downsampled outdoor history, used to overlay
// outdoor conditions on the metric-detail modal for comparison.
type WeatherHistory struct {
	Temp     []SeriesPoint `json:"temp"`
	Humidity []SeriesPoint `json:"humidity"`
	Pressure []SeriesPoint `json:"pressure"`
}

// Activity records a human-readable system action, warning or notice.
type Activity struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	DeviceID      string    `json:"deviceId,omitempty"`
	Time          time.Time `json:"time"`
	Level         string    `json:"level"` // info, warning, error
	Type          string    `json:"type"`  // control, warning, notice, configuration
	Message       string    `json:"message"`
}

// --- Live view types (built each control tick, sent to clients) ---

// SensorReading is a sensor binding with its current value.
type SensorReading struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Measurement Measurement `json:"measurement"`
	Entity      string      `json:"entity"`
	Value       float64     `json:"value"`
	OK          bool        `json:"ok"`
}

// ControlState is a fan or light binding with its current state.
type ControlState struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Kind         BindingKind `json:"kind"`
	Role         Role        `json:"role,omitempty"`
	Entity       string      `json:"entity"`
	DesiredSpeed int         `json:"desiredSpeed"`      // fans
	RPM          int         `json:"rpm"`               // fans
	MaxRPM       int         `json:"maxRpm,omitempty"`  // fans: configured full-speed RPM
	On           bool        `json:"on"`                // lights
	Wattage      float64     `json:"wattage,omitempty"` // lights: rated power (W)
	Power        float64     `json:"power,omitempty"`   // lights: actual measured power (W) from the plug meter, else rated while on
	Primary      bool        `json:"primary,omitempty"` // lights: the box's main grow light
}

// CameraRef is a camera binding (no stream in this MVP).
type CameraRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Entity string `json:"entity"`
}

// AirSourceView summarises a linked lung room on a tent's dashboard.
type AirSourceView struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	TempC    float64 `json:"tempC"`
	Humidity float64 `json:"humidity"`
	VPD      float64 `json:"vpd"`
	OK       bool    `json:"ok"`
}

// EnvironmentView is the full live state of one environment.
type EnvironmentView struct {
	Environment
	Health     ControllerHealth `json:"health"`
	HasClimate bool             `json:"hasClimate"`
	HasTemp    bool             `json:"hasTemp"`
	HasHum     bool             `json:"hasHum"`
	TempC      float64          `json:"tempC"`
	Humidity   float64          `json:"humidity"`
	CO2        float64          `json:"co2"`
	HasCO2     bool             `json:"hasCO2"`
	VPD        float64          `json:"vpd"`
	Sensors    []SensorReading  `json:"sensors"`
	Controls   []ControlState   `json:"controls"`
	Cameras    []CameraRef      `json:"cameras"`
	AirSource  *AirSourceView   `json:"airSource,omitempty"`
	Cycle      *Cycle           `json:"cycle,omitempty"`
	Schedule   *LightSchedule   `json:"schedule,omitempty"`
}

// Snapshot is the full live system state broadcast to clients.
type Snapshot struct {
	Time         time.Time         `json:"time"`
	Environments []EnvironmentView `json:"environments"`
}
