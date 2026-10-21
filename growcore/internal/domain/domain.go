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
	KindIrrigation BindingKind = "irrigation"
)

// IrrigationType is the kind of irrigation setup a binding represents. AutoPot
// is the first supported type; the rest name common alternatives so the model
// doesn't need widening later.
type IrrigationType string

const (
	IrrigationAutoPot IrrigationType = "autopot"  // gravity-fed AQUAvalve trays
	IrrigationDrip    IrrigationType = "drip"     // dripper lines / emitters
	IrrigationWick    IrrigationType = "wick"     // passive wick from a reservoir
	IrrigationEbbFlow IrrigationType = "ebb_flow" // flood-and-drain tables
	IrrigationHand    IrrigationType = "hand"     // hand-watering, tracked as equipment
)

// IrrigationMode is how an irrigation binding is driven. A passive setup (an
// AutoPot tray today) is a physical arrangement with no live telemetry or
// control, so it carries no Home Assistant entity. A controlled setup drives a
// pump/valve entity — reserved for a future phase.
type IrrigationMode string

const (
	IrrigationPassive    IrrigationMode = "passive"    // physical setup, no entity
	IrrigationControlled IrrigationMode = "controlled" // pump/valve driven via HA
)

// CameraType is how a generic (non-Home-Assistant) camera stream is rendered.
// MJPEG and snapshots are browser-playable directly. RTSP unicast is relayed by
// Grow Core as MJPEG because browsers cannot render RTSP themselves.
type CameraType string

const (
	CameraMJPEG    CameraType = "mjpeg"
	CameraSnapshot CameraType = "snapshot"
	CameraRTSP     CameraType = "rtsp"
)

// RetentionPolicy decides what happens once an archive passes its configured
// age or size limit. Recorded footage is the user's data, so the default is to
// raise an alert and keep everything; deletion is strictly opt-in.
type RetentionPolicy string

const (
	RetentionAlert  RetentionPolicy = "alert"
	RetentionDelete RetentionPolicy = "delete"
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

	// ControlGrowID optionally nominates the Grow whose current stage supplies
	// this environment's automation presets (photoperiod). Empty means the only
	// active grow with plants here drives it (see ResolveControlGrow); it must
	// be set explicitly when several grows share one environment, otherwise the
	// light schedule's phase mode falls back to defaults.
	ControlGrowID string `json:"controlGrowId" yaml:"controlGrowId,omitempty"`

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

	// Optional display target ranges (0 = unset). These do not change control
	// behavior — the single Target* values above remain the engine setpoints —
	// but drive the "ok band" and status shown on climate cards and the timeline.
	TargetTempMinC    float64 `json:"targetTempMinC" yaml:"targetTempMinC,omitempty"`
	TargetTempMaxC    float64 `json:"targetTempMaxC" yaml:"targetTempMaxC,omitempty"`
	TargetHumidityMin float64 `json:"targetHumidityMin" yaml:"targetHumidityMin,omitempty"`
	TargetHumidityMax float64 `json:"targetHumidityMax" yaml:"targetHumidityMax,omitempty"`
	TargetVPDMin      float64 `json:"targetVpdMin" yaml:"targetVpdMin,omitempty"`
	TargetVPDMax      float64 `json:"targetVpdMax" yaml:"targetVpdMax,omitempty"`
	TargetCO2Min      float64 `json:"targetCo2Min" yaml:"targetCo2Min,omitempty"`
	TargetCO2Max      float64 `json:"targetCo2Max" yaml:"targetCo2Max,omitempty"`

	EmergencyTempC float64 `json:"emergencyTempC" yaml:"emergencyTempC"`
	// LeafTempOffsetC estimates leaf temperature from air temperature for VPD.
	// For example, -2 means leaves are assumed to be 2°C cooler than the air.
	LeafTempOffsetC float64 `json:"leafTempOffsetC" yaml:"leafTempOffsetC,omitempty"`

	// Control is the per-category automation configuration (which categories run
	// automatically vs. by hand, and manual setpoints). It is the environment's
	// live control policy, persisted in the on-disk config file so it round-trips
	// with the rest of the environment. Zero values resolve to sensible defaults
	// via Control.Resolve.
	Control ControlConfig `json:"control" yaml:"control"`
}

// ControlMode selects whether a controllable category is driven by automation
// or by hand.
type ControlMode string

const (
	ControlAuto   ControlMode = "auto"
	ControlManual ControlMode = "manual"
)

// ControlConfig is an environment's per-category automation policy. Each
// controllable category carries an auto/manual mode; air exchange also carries
// the fixed speeds used while it is in manual mode. New categories (CO₂,
// dehumidify, …) are added here as sibling fields.
type ControlConfig struct {
	Lighting    LightingControl    `json:"lighting" yaml:"lighting"`
	AirExchange AirExchangeControl `json:"airExchange" yaml:"airExchange"`
	Irrigation  IrrigationControl  `json:"irrigation" yaml:"irrigation"`
}

// LightingControl gates the photoperiod: auto lets the light schedule drive the
// primary light, manual leaves it under the grower's switch.
type LightingControl struct {
	Mode ControlMode `json:"mode" yaml:"mode"`
}

// AirExchangeControl gates the fans: auto drives them from the climate targets,
// manual holds the fixed exhaust/circulation speeds below (an emergency
// over-temperature still forces every fan to full).
type AirExchangeControl struct {
	Mode        ControlMode `json:"mode" yaml:"mode"`
	Exhaust     int         `json:"exhaust" yaml:"exhaust"`         // manual speed %
	Circulation int         `json:"circulation" yaml:"circulation"` // manual speed %
}

// IrrigationControl gates watering: auto means installed irrigation equipment
// handles it (the grow is auto-watered), manual means the grower hand-waters.
type IrrigationControl struct {
	Mode ControlMode `json:"mode" yaml:"mode"`
}

// Resolve fills any unset per-category mode with its default: lighting follows
// the schedule (manual when the schedule is not driving), air exchange and
// irrigation default to automatic. Manual air setpoints are left as stored — a
// manual 0 % is a legitimate "off", so it is never overridden.
func (c ControlConfig) Resolve(scheduleActive bool) ControlConfig {
	if c.Lighting.Mode == "" {
		if scheduleActive {
			c.Lighting.Mode = ControlAuto
		} else {
			c.Lighting.Mode = ControlManual
		}
	}
	if c.AirExchange.Mode == "" {
		c.AirExchange.Mode = ControlAuto
	}
	if c.Irrigation.Mode == "" {
		c.Irrigation.Mode = ControlAuto
	}
	return c
}

// VolumeM3 returns the tent's air volume in cubic metres, or 0 if any
// dimension is unset.
func (e Environment) VolumeM3() float64 {
	if e.WidthCm <= 0 || e.DepthCm <= 0 || e.HeightCm <= 0 {
		return 0
	}
	return e.WidthCm * e.DepthCm * e.HeightCm / 1_000_000
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
	// LightSchedulePhase follows the recommended photoperiod for the control
	// grow's current stage, with optional per-stage overrides.
	LightSchedulePhase LightScheduleMode = "phase"
	// LightScheduleCustom uses a fixed on-time and duration, ignoring the stage.
	LightScheduleCustom LightScheduleMode = "custom"
)

// AllLightScheduleModes lists the selectable schedule modes.
var AllLightScheduleModes = []LightScheduleMode{LightScheduleOff, LightSchedulePhase, LightScheduleCustom}

// LightAnchor says which edge of the light window AnchorAt pins. It only
// matters when the on-duration changes (a stage change in phase mode, or an
// edited duration): the anchored edge stays put and the rest of the window
// moves around it.
//
//	center: 18/6 centred on 15:00 → 06:00–00:00; 12/12 → 09:00–21:00 (default)
//	start:  18/6 on at 06:00 → 06:00–00:00; 12/12 → 06:00–18:00
//	end:    18/6 off at 00:00 → 06:00–00:00; 12/12 → 12:00–00:00
type LightAnchor string

const (
	// AnchorCenter pins the middle of the light period ("solar noon"), so a
	// shorter photoperiod stays centred on the same part of the day. It is the
	// default, and the zero value's meaning.
	AnchorCenter LightAnchor = "center"
	// AnchorStart pins the on-time: a shorter photoperiod ends earlier.
	AnchorStart LightAnchor = "start"
	// AnchorEnd pins the off-time: a shorter photoperiod starts later.
	AnchorEnd LightAnchor = "end"
)

// AllLightAnchors lists the selectable anchors.
var AllLightAnchors = []LightAnchor{AnchorCenter, AnchorStart, AnchorEnd}

// LightSchedule is the photoperiod automation for an environment's primary
// grow light. The light is on for the effective number of hours each day,
// positioned against the wall clock by AnchorAt and Anchor; the remainder of
// the 24h day is dark. Anchoring on wall-clock time (rather than counting from
// an arbitrary start) keeps the dark period aligned to a real daily window.
//
// In "phase" mode the on-duration follows the control grow's current stage.
type LightSchedule struct {
	EnvironmentID string            `json:"environmentId"`
	Mode          LightScheduleMode `json:"mode"`
	// LightsOnAt is the anchor wall-clock time, "HH:MM" (24h) local. What it
	// pins depends on Anchor: the middle of the light period (center, the
	// default), the on-time (start), or the off-time (end).
	LightsOnAt string `json:"lightsOnAt"`
	// Anchor selects which edge of the light window LightsOnAt pins. Empty
	// means AnchorCenter, the default for new schedules.
	Anchor LightAnchor `json:"anchor"`
	// OnHours is the on-duration for custom mode.
	OnHours float64 `json:"onHours"`
	// StageOnHours holds per-stage overrides for phase mode, keyed by stage
	// name. A stage absent from the map falls back to DefaultStageLightHours.
	StageOnHours map[string]float64 `json:"stageOnHours"`
}

// DefaultLightSchedule is the schedule for an environment that has none saved:
// manual control, with sensible seeds for the editor. New schedules default to
// a day centred on 15:00, so 18/6 runs 06:00–00:00 and a switch to 12/12 keeps
// the same midday rather than cutting the evening short.
func DefaultLightSchedule(envID string) LightSchedule {
	return LightSchedule{
		EnvironmentID: envID,
		Mode:          LightScheduleOff,
		LightsOnAt:    "15:00",
		Anchor:        AnchorCenter,
		OnHours:       18,
		StageOnHours:  map[string]float64{},
	}
}

// EffectiveOnHours resolves the on-duration for the given stage: the custom
// duration in custom mode, otherwise the per-stage override or the default.
func (s LightSchedule) EffectiveOnHours(stage string) float64 {
	if s.Mode == LightScheduleCustom {
		return clampHours(s.OnHours)
	}
	if h, ok := s.StageOnHours[stage]; ok {
		return clampHours(h)
	}
	if h, ok := DefaultStageLightHours[stage]; ok {
		return h
	}
	return 18
}

// ReferenceHours is the schedule's full day: the custom duration, or the
// longest photoperiod across the stages it can drive. It is the day the grower
// set up, so it is what anchors are converted against — re-anchoring on a
// shortened stage would only re-describe the window that stage already has.
func (s LightSchedule) ReferenceHours() float64 {
	if s.Mode == LightScheduleCustom {
		return clampHours(s.OnHours)
	}
	longest := 0.0
	consider := func(h float64) {
		if h := clampHours(h); h > longest {
			longest = h
		}
	}
	for stage, h := range DefaultStageLightHours {
		if override, ok := s.StageOnHours[stage]; ok {
			h = override
		}
		consider(h)
	}
	// Stages outside the known set (custom stage names) count too.
	for stage, h := range s.StageOnHours {
		if _, known := DefaultStageLightHours[stage]; !known {
			consider(h)
		}
	}
	return longest
}

// AnchorTimeFor returns the "HH:MM" anchor time that keeps a window whose light
// comes on at onMin (minutes since midnight) and runs for hours in place under
// the given anchor. It is the inverse of the offset Window applies.
func AnchorTimeFor(a LightAnchor, onMin int, hours float64) string {
	span := int(clampHours(hours) * 60)
	switch a {
	case AnchorStart:
		// The anchor time is the on-time itself.
	case AnchorEnd:
		onMin += span
	default: // AnchorCenter
		onMin += span / 2
	}
	onMin = ((onMin % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", onMin/60, onMin%60)
}

// Window resolves the light-on window for the given stage as minutes since
// local midnight: onMin is when the light comes on, offMin when it goes off
// (offMin may be <= onMin when the window wraps past midnight). ok is false
// when the anchor time is unparseable, or the window is degenerate (always on
// or always off).
func (s LightSchedule) Window(stage string) (onMin, offMin int, ok bool) {
	hours := s.EffectiveOnHours(stage)
	if hours <= 0 || hours >= 24 {
		return 0, 0, false
	}
	anchorAt, valid := ParseHHMM(s.LightsOnAt)
	if !valid {
		return 0, 0, false
	}
	span := int(hours * 60)
	switch s.Anchor {
	case AnchorStart:
		// The stored time is the on-time; nothing to shift.
	case AnchorEnd:
		anchorAt -= span
	default: // AnchorCenter, the default for schedules that name no anchor.
		anchorAt -= span / 2
	}
	onMin = ((anchorAt % 1440) + 1440) % 1440
	return onMin, (onMin + span) % 1440, true
}

// DesiredOn reports whether the light should be on at time now for the given
// stage. ok is false when the schedule is not driving the light (mode off).
func (s LightSchedule) DesiredOn(stage string, now time.Time) (on bool, ok bool) {
	if s.Mode == LightScheduleOff {
		return false, false
	}
	hours := s.EffectiveOnHours(stage)
	if hours <= 0 {
		return false, true
	}
	if hours >= 24 {
		return true, true
	}
	onAt, _, valid := s.Window(stage)
	if !valid {
		return false, false
	}
	mins := now.Hour()*60 + now.Minute()
	return inWindow(mins, onAt, int(hours*60)), true
}

// NextTransition returns the next wall-clock instant at/after now when the
// scheduled light state flips. Used to hold a manual override until the next
// scheduled boundary. It returns the zero time when the schedule never flips
// (mode off, or an always-on / always-off duration).
func (s LightSchedule) NextTransition(stage string, now time.Time) time.Time {
	if s.Mode == LightScheduleOff {
		return time.Time{}
	}
	onAt, offAt, ok := s.Window(stage)
	if !ok {
		return time.Time{}
	}
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

// ParseHHMM parses "HH:MM" into minutes-since-midnight.
func ParseHHMM(s string) (mins int, ok bool) {
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
	// ProductID links the physical device to the catalog product it was
	// installed from ("<category>/<id>"), so the UI can show its image and
	// datasheet. Empty for manually-added devices. Shared by all of a device's
	// capabilities.
	ProductID string `json:"productId,omitempty"`
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
	FanType             string  `json:"fanType,omitempty"`
	SizeMM              int     `json:"sizeMm,omitempty"`
	MaxRPM              int     `json:"maxRpm,omitempty"`
	AirflowCFM          float64 `json:"airflowCfm,omitempty"`
	StaticPressureMMH2O float64 `json:"staticPressureMmH2O,omitempty"`
	StartingVoltage     float64 `json:"startingVoltage,omitempty"`
	DuctSizeInches      float64 `json:"ductSizeInches,omitempty"`
	NoiseDBA            float64 `json:"noiseDba,omitempty"`
	// Light only:
	Wattage float64 `json:"wattage,omitempty"` // rated power in watts; 0 = unknown
	Primary bool    `json:"primary,omitempty"` // the box's main grow light (one per env)
	// Camera only: a generic camera streams from a URL instead of a Home Assistant
	// entity. StreamURL is empty for HA-entity cameras.
	StreamURL             string     `json:"streamUrl,omitempty"`
	CameraType            CameraType `json:"cameraType,omitempty"`
	CameraCaptureInterval int        `json:"cameraCaptureInterval,omitempty"` // seconds
	// Archive limits. Both are 0 = unlimited. What happens when a limit is
	// exceeded is decided by CameraRetentionPolicy, which defaults to raising
	// an alert rather than deleting anything.
	CameraRetentionDays   int             `json:"cameraRetentionDays,omitempty"`
	CameraStorageMB       int             `json:"cameraStorageMb,omitempty"`
	CameraRetentionPolicy RetentionPolicy `json:"cameraRetentionPolicy,omitempty"`
	// Irrigation only: a passive setup (AutoPot today) has no entity; ReservoirL
	// is the feed reservoir volume and ValveCount the number of trays/valves.
	IrrigationType IrrigationType `json:"irrigationType,omitempty"`
	IrrigationMode IrrigationMode `json:"irrigationMode,omitempty"`
	ReservoirL     float64        `json:"reservoirL,omitempty"`
	ValveCount     int            `json:"valveCount,omitempty"`
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
	GrowID        string    `json:"growId,omitempty"`
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

// CameraRef is a camera binding for the live view. HA-entity cameras carry an
// Entity; generic cameras carry a StreamURL + CameraType the client renders.
type CameraRef struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Entity                string     `json:"entity,omitempty"`
	StreamURL             string     `json:"streamUrl,omitempty"`
	CameraType            CameraType `json:"cameraType,omitempty"`
	CameraCaptureInterval int        `json:"cameraCaptureInterval,omitempty"`
}

// IrrigationRef is an irrigation binding for the live view. A passive setup
// carries no live state, so this is a static description (type, mode, reservoir
// and valve count) the client renders in Equipment and Operating now.
type IrrigationRef struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Type       IrrigationType `json:"type"`
	Mode       IrrigationMode `json:"mode"`
	ReservoirL float64        `json:"reservoirL,omitempty"`
	ValveCount int            `json:"valveCount,omitempty"`
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
	Irrigation []IrrigationRef  `json:"irrigation"`
	AirSource  *AirSourceView   `json:"airSource,omitempty"`
	Grow       *GrowSummary     `json:"grow,omitempty"`
	Schedule   *LightSchedule   `json:"schedule,omitempty"`
}

// Snapshot is the full live system state broadcast to clients.
type Snapshot struct {
	Time         time.Time         `json:"time"`
	Environments []EnvironmentView `json:"environments"`
	Grows        []GrowView        `json:"grows"`
}

// --- Users & access control ---

// UserRole is an account's global role. Note this is distinct from Role, which
// is the fan/airflow role enum; a user account is either an administrator or a
// normal user whose reach is scoped by per-environment access grants.
type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

// AccessLevel is a user's granted access to a single environment.
type AccessLevel string

const (
	// AccessRead lets a user view an environment; AccessWrite additionally lets
	// them operate it (toggle devices, adjust targets, edit cycle & schedule).
	AccessRead  AccessLevel = "read"
	AccessWrite AccessLevel = "write"
)

// User is an account. The password hash and salt live only in the store and are
// never serialised to the API (there are no JSON tags for them).
type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Role     UserRole  `json:"role"`
	Disabled bool      `json:"disabled"`
	Created  time.Time `json:"created"`

	PasswordHash string `json:"-"`
	PasswordSalt string `json:"-"`
}

// EnvAccess is a single per-environment grant for a user.
type EnvAccess struct {
	EnvironmentID string      `json:"environmentId"`
	Access        AccessLevel `json:"access"`
}

// UserView is the API representation of a user: the account plus its grants,
// and never any secret material.
type UserView struct {
	User
	Access []EnvAccess `json:"access"`
}

// AllowsWrite reports whether an access level permits operating an environment.
func (a AccessLevel) AllowsWrite() bool { return a == AccessWrite }

// StoredCredential is a persisted WebAuthn passkey. Data is the opaque
// JSON-encoded credential record owned by the api layer (the store treats it as
// a blob so it needn't depend on the WebAuthn library).
type StoredCredential struct {
	ID      string    `json:"id"`
	UserID  string    `json:"-"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Data    []byte    `json:"-"`
}
