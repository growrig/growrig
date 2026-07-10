package control

import (
	"context"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

// Adapter is the boundary between the control engine and the physical world.
// The simulator and Home Assistant adapters both implement it, so the engine
// and control law are identical regardless of how devices are reached.
//
// Methods receive the domain device/channel so adapters can read their entity
// bindings (populated from the database), keeping the adapters stateless with
// respect to topology — devices can be added or re-bound at runtime.
type Adapter interface {
	// Start establishes the connection (or initialises the simulator). It
	// should return once initial state is available or fail fast on a fatal
	// misconfiguration.
	Start(ctx context.Context) error

	// Tick advances internal state for one control cycle of duration dt. For
	// the simulator this steps the physical model; for Home Assistant it is a
	// no-op (state arrives asynchronously over the WebSocket).
	Tick(dt time.Duration)

	// Climate returns the latest temperature (°C) and relative humidity (%)
	// for a device, or ok=false if not yet available.
	Climate(dev domain.Device) (tempC, humidity float64, ok bool)

	// SetSpeed commands a channel to a PWM speed in the range 0-100.
	SetSpeed(dev domain.Device, ch domain.Channel, speed int) error

	// FanRPM returns the measured tachometer RPM for a channel, if available.
	FanRPM(dev domain.Device, ch domain.Channel) (rpm int, ok bool)

	// Health reports controller/connection health for a device.
	Health(dev domain.Device) domain.ControllerHealth

	// Close releases resources.
	Close() error
}
