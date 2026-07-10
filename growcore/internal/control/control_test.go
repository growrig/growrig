package control

import (
	"testing"

	"github.com/growrig/growrig-platform/growcore/internal/domain"
)

func env() domain.Environment {
	return domain.Environment{TargetTempC: 24, TargetHumidity: 55, EmergencyTempC: 35}
}

func TestExhaustAtTargetRunsAtMinimum(t *testing.T) {
	if got := ChannelSpeed(domain.RoleExhaust, env(), 24); got != minExhaust {
		t.Fatalf("exhaust at target = %d, want %d", got, minExhaust)
	}
}

func TestExhaustRampsWithHeat(t *testing.T) {
	cool := ChannelSpeed(domain.RoleExhaust, env(), 24)
	hot := ChannelSpeed(domain.RoleExhaust, env(), 27)
	if hot <= cool {
		t.Fatalf("exhaust should increase with heat: cool=%d hot=%d", cool, hot)
	}
	if hot > 100 {
		t.Fatalf("exhaust exceeded 100: %d", hot)
	}
}

func TestEmergencyForcesMaxOnFans(t *testing.T) {
	if got := ChannelSpeed(domain.RoleExhaust, env(), 36); got != emergencyPWM {
		t.Fatalf("emergency exhaust = %d, want %d", got, emergencyPWM)
	}
	if got := ChannelSpeed(domain.RoleCirculation, env(), 36); got != emergencyPWM {
		t.Fatalf("emergency circulation = %d, want %d", got, emergencyPWM)
	}
	if got := ChannelSpeed(domain.RoleUnassigned, env(), 36); got != 0 {
		t.Fatalf("emergency unassigned = %d, want 0", got)
	}
}

func TestUnassignedStaysOff(t *testing.T) {
	if got := ChannelSpeed(domain.RoleUnassigned, env(), 30); got != 0 {
		t.Fatalf("unassigned = %d, want 0", got)
	}
}

func TestCirculationHasBaseline(t *testing.T) {
	if got := ChannelSpeed(domain.RoleCirculation, env(), 20); got != baseCirc {
		t.Fatalf("circulation baseline = %d, want %d", got, baseCirc)
	}
}
