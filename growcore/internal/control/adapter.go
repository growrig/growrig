package control

import (
	"context"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// DiscoveredEntity is a candidate device/sensor the adapter has found and that
// the user can bind to an environment.
type DiscoveredEntity struct {
	Entity         string             `json:"entity"`
	Name           string             `json:"name"`
	Kind           domain.BindingKind `json:"kind"`
	Measurement    domain.Measurement `json:"measurement,omitempty"`
	HADeviceID     string             `json:"haDeviceId,omitempty"`
	DeviceName     string             `json:"deviceName,omitempty"`
	Integration    string             `json:"integration,omitempty"`
	EntityCategory string             `json:"entityCategory,omitempty"`
	Manufacturer   string             `json:"manufacturer,omitempty"`
	Model          string             `json:"model,omitempty"`
	Unit           string             `json:"unit,omitempty"`
}

// Adapter is the boundary between the control engine and the physical world.
// It is entity-oriented: the engine reads and writes Home Assistant (or
// simulator) entities by id, exactly as they are bound to environments in the
// database. The simulator and Home Assistant adapters both implement it.
type Adapter interface {
	// Start establishes the connection (or initialises the simulator).
	Start(ctx context.Context) error

	// Tick advances internal state for one control cycle of duration dt. For
	// the simulator this steps the physical model; for Home Assistant it is a
	// no-op (state arrives asynchronously over the WebSocket).
	Tick(dt time.Duration)

	// Value returns the latest numeric value of an entity (sensor reading,
	// tachometer RPM, …), or ok=false if unavailable.
	Value(entity string) (value float64, ok bool)

	// SetFan commands a fan entity to a PWM speed in the range 0-100.
	SetFan(entity string, speed int) error

	// SetSwitch turns a switchable entity (e.g. a light) on or off.
	SetSwitch(entity string, on bool) error

	// SwitchState returns the on/off state of a switchable entity, if known.
	SwitchState(entity string) (on bool, ok bool)

	// Health reports overall adapter/connection health.
	Health() domain.ControllerHealth

	// Discover lists candidate entities that can be bound to environments.
	Discover() []DiscoveredEntity

	// Close releases resources.
	Close() error
}
