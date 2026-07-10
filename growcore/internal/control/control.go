// Package control is Grow Core's reconciliation engine.
//
// Given actual state, desired targets and safety constraints, it computes the
// desired state of each device channel. The control law is expressed as pure
// functions so it can be reasoned about and unit-tested independently of the
// adapters and storage.
package control

import "github.com/growrig/growrig-platform/growcore/internal/domain"

const (
	minExhaust   = 15  // always keep some air exchange
	exhaustGain  = 18  // % per °C over target
	baseCirc     = 35  // steady circulation airflow
	circBoost    = 8   // extra circulation per °C over target
	emergencyPWM = 100 // full airflow in an emergency
)

// ChannelSpeed computes the desired PWM speed (0-100) for a channel given its
// role and the current vs. target climate of its environment.
func ChannelSpeed(role domain.Role, env domain.Environment, tempC float64) int {
	// Safety first: an over-temperature environment drives every fan to max.
	if env.EmergencyTempC > 0 && tempC >= env.EmergencyTempC {
		if role == domain.RoleUnassigned {
			return 0
		}
		return emergencyPWM
	}

	over := tempC - env.TargetTempC // positive when too hot

	switch role {
	case domain.RoleExhaust, domain.RoleIntake:
		if over <= 0 {
			return minExhaust
		}
		return clamp(minExhaust+int(over*exhaustGain), minExhaust, 100)
	case domain.RoleCirculation:
		if over <= 0 {
			return baseCirc
		}
		return clamp(baseCirc+int(over*circBoost), baseCirc, 100)
	default: // unassigned
		return 0
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
