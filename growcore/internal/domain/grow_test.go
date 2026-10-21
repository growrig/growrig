package domain

import (
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Blue Dream", "blue-dream"},
		{"OG Kush #2", "og-kush-2"},
		{"  Trailing/Leading  ", "trailing-leading"},
		{"Genovese Basil (Batch 3)", "genovese-basil-batch-3"},
		{"already-slug", "already-slug"},
		{"multi   spaces", "multi-spaces"},
		{"!!!", ""}, // no alphanumeric content
		{"", ""},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveControlGrow(t *testing.T) {
	grows := map[string]Grow{
		"a":    {ID: "a", Stage: "flowering", Status: GrowActive},
		"b":    {ID: "b", Stage: "vegetative", Status: GrowActive},
		"done": {ID: "done", Stage: "cure", Status: GrowCompleted},
	}
	units := map[string]PlantUnit{
		"ua":    {ID: "ua", GrowID: "a", Status: PlantActive},
		"ua2":   {ID: "ua2", GrowID: "a", Status: PlantActive},
		"ub":    {ID: "ub", GrowID: "b", Status: PlantActive},
		"udone": {ID: "udone", GrowID: "done", Status: PlantActive},
	}
	ended := time.Now()
	env := Environment{ID: "tent"}

	// No nomination: the only grow growing here drives the schedule.
	sole := []PlantPlacement{
		{PlantUnitID: "ua", EnvironmentID: "tent"},
		{PlantUnitID: "ua2", EnvironmentID: "tent"},
		{PlantUnitID: "ub", EnvironmentID: "other"},
		{PlantUnitID: "ub", EnvironmentID: "tent", EndedAt: &ended},
		{PlantUnitID: "udone", EnvironmentID: "tent"},
	}
	if g, ok := ResolveControlGrow(env, grows, units, sole); !ok || g.ID != "a" {
		t.Fatalf("sole grow: got %q ok=%v, want a", g.ID, ok)
	}

	// Several grows share the tent: ambiguous without a nomination.
	shared := append(sole, PlantPlacement{PlantUnitID: "ub", EnvironmentID: "tent"})
	if g, ok := ResolveControlGrow(env, grows, units, shared); ok {
		t.Fatalf("shared tent: got %q, want none", g.ID)
	}

	// A nomination always wins.
	env.ControlGrowID = "b"
	if g, ok := ResolveControlGrow(env, grows, units, shared); !ok || g.ID != "b" {
		t.Fatalf("nominated: got %q ok=%v, want b", g.ID, ok)
	}

	if _, ok := ResolveControlGrow(Environment{ID: "empty"}, grows, units, sole); ok {
		t.Fatal("empty tent: want none")
	}
}
