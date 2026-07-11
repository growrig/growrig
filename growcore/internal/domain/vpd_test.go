package domain

import (
	"math"
	"testing"
)

func TestVPD(t *testing.T) {
	// At 25°C / 50% RH, air VPD is ~1.58 kPa (a well-known reference point).
	got := VPD(25, 50)
	if math.Abs(got-1.58) > 0.05 {
		t.Errorf("VPD(25,50) = %.3f, want ~1.58", got)
	}
	// Saturated air has zero deficit.
	if got := VPD(25, 100); got != 0 {
		t.Errorf("VPD at 100%% RH = %.3f, want 0", got)
	}
	// Drier air => larger deficit.
	if VPD(25, 30) <= VPD(25, 60) {
		t.Error("VPD should increase as humidity drops")
	}
}

func TestLeafVPDWithCoolerLeaf(t *testing.T) {
	// The common chart example: 25°C air, 50% RH and a leaf 2°C cooler.
	got := LeafVPD(25, 50, -2)
	if got < 1.22 || got > 1.24 {
		t.Errorf("LeafVPD(25, 50, -2) = %.3f, want ~1.23", got)
	}
	if got >= VPD(25, 50) {
		t.Error("a cooler leaf should produce lower VPD than air VPD")
	}
}
