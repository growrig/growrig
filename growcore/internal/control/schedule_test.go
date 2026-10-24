package control

import (
	"testing"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
)

func TestFloweringScheduleUsesInstanceTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatal(err)
	}
	e := New(nil, nil, nil)
	e.SetScheduleLocation(loc)
	s := domain.LightSchedule{Mode: domain.LightSchedulePhase, Anchor: domain.AnchorCenter, LightsOnAt: "15:00"}
	for _, date := range []string{"2026-10-22", "2026-10-25", "2026-12-01", "2026-03-29"} {
		for _, tc := range []struct {
			clock string
			on    bool
		}{
			{"08:59", false}, {"09:00", true}, {"20:59", true}, {"21:00", false},
		} {
			t.Run(date+"/"+tc.clock, func(t *testing.T) {
				local, err := time.ParseInLocation("2006-01-02 15:04", date+" "+tc.clock, loc)
				if err != nil {
					t.Fatal(err)
				}
				// Simulate the UTC server clock, including both DST transitions.
				now := e.scheduleTime(local.UTC())
				if on, ok := s.DesiredOn("flowering", now); !ok || on != tc.on {
					t.Fatalf("at %v: on=%v, active=%v, want on=%v", now, on, ok, tc.on)
				}
			})
		}
	}
	// A preference change must take effect without restarting the engine.
	e.SetScheduleLocation(time.UTC)
	instant := time.Date(2026, 10, 22, 7, 0, 0, 0, time.UTC)
	if on, _ := s.DesiredOn("flowering", e.scheduleTime(instant)); on {
		t.Fatal("UTC preference must keep light off at 07:00 UTC")
	}
}

func TestManualLightOverrideUsesInstanceTimezone(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// Use a fixed zone independent of the host and choose an on boundary one
	// hour ahead so a UTC calculation would expire the override elsewhere.
	loc := time.FixedZone("instance", 5*60*60)
	now := time.Now().In(loc)
	s := domain.LightSchedule{EnvironmentID: "b1", Mode: domain.LightScheduleCustom,
		Anchor: domain.AnchorStart, LightsOnAt: now.Add(time.Hour).Format("15:04"), OnHours: 12}
	if err := st.SaveLightSchedule(s); err != nil {
		t.Fatal(err)
	}
	e := New(st, nil, nil)
	e.SetScheduleLocation(loc)
	e.NoteManualLightSwitch("b1")
	want := s.NextTransition("", now)
	if got := e.lightOverrides["b1"]; !got.Equal(want) {
		t.Fatalf("override expires at %v, want %v", got, want)
	}
}
