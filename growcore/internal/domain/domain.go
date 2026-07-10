// Package domain defines the grow-domain model for Grow Core.
//
// These types are deliberately semantic: they describe environments, roles,
// and capabilities rather than mirroring Home Assistant entities or vendor
// hardware. See ../../../growrig/docs/architecture.md.
package domain

import "time"

// Role is the grow purpose assigned to a device capability/channel.
type Role string

const (
	RoleUnassigned  Role = "unassigned"
	RoleExhaust     Role = "exhaust"
	RoleIntake      Role = "intake"
	RoleCirculation Role = "circulation"
)

// AllFanRoles lists the roles a fan channel may be assigned in the MVP.
var AllFanRoles = []Role{RoleUnassigned, RoleExhaust, RoleIntake, RoleCirculation}

// ControllerHealth describes the liveness of a controller as seen by Grow Core.
type ControllerHealth string

const (
	HealthOnline  ControllerHealth = "online"
	HealthStale   ControllerHealth = "stale"
	HealthOffline ControllerHealth = "offline"
)

// Environment is a controlled physical space (grow box, tent, room) with
// target climate conditions that the control engine tries to maintain.
type Environment struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	TargetTempC    float64 `json:"targetTempC"`
	TargetHumidity float64 `json:"targetHumidity"` // relative humidity %
	// EmergencyTempC triggers max exhaust regardless of target.
	EmergencyTempC float64 `json:"emergencyTempC"`
}

// Channel is a controllable output on a Controller (e.g. a PWM fan).
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role Role   `json:"role"`
	// Entity is the Home Assistant fan entity this channel commands
	// (e.g. "fan.growbox_exhaust"). Empty for the simulator.
	Entity string `json:"entity"`
	// RPMEntity is an optional HA tachometer sensor entity.
	RPMEntity string `json:"rpmEntity"`
	// DesiredSpeed is the intent computed by the control engine (0-100).
	DesiredSpeed int `json:"desiredSpeed"`
	// RPM is the last measured tachometer reading.
	RPM int `json:"rpm"`
}

// Device is a Grow Controller and the channels/sensors it exposes.
type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	EnvironmentID string `json:"environmentId"`
	Adapter       string `json:"adapter"` // e.g. "simulator", "homeassistant"
	// TempEntity / HumidityEntity are the Home Assistant sensor entities that
	// provide this device's climate readings. Empty for the simulator.
	TempEntity     string           `json:"tempEntity"`
	HumidityEntity string           `json:"humidityEntity"`
	Health         ControllerHealth `json:"health"`
	Channels       []Channel        `json:"channels"`
	TempC          float64          `json:"tempC"`
	Humidity       float64          `json:"humidity"`
	LastSeen       time.Time        `json:"lastSeen"`
}

// Reading is a single historical sample persisted for an environment.
type Reading struct {
	EnvironmentID string    `json:"environmentId"`
	Time          time.Time `json:"time"`
	TempC         float64   `json:"tempC"`
	Humidity      float64   `json:"humidity"`
	ExhaustSpeed  int       `json:"exhaustSpeed"`
}

// Snapshot is the full live system state broadcast to clients.
type Snapshot struct {
	Time         time.Time     `json:"time"`
	Environments []Environment `json:"environments"`
	Devices      []Device      `json:"devices"`
}
