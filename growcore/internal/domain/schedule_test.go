package domain

import (
	"testing"
	"time"
)

func at(hhmm string) time.Time {
	t, _ := time.ParseInLocation("15:04", hhmm, time.Local)
	// Anchor to a fixed date; only the clock time matters.
	return time.Date(2026, 7, 11, t.Hour(), t.Minute(), 0, 0, time.Local)
}

func TestDesiredOn_PhaseMode(t *testing.T) {
	s := LightSchedule{Mode: LightSchedulePhase, LightsOnAt: "06:00", Anchor: AnchorStart, StageOnHours: map[string]float64{}}

	// Vegetative default is 18/6: on 06:00–00:00.
	cases := []struct {
		now   string
		stage string
		want  bool
	}{
		{"06:00", "vegetative", true},
		{"23:59", "vegetative", true},
		{"00:00", "vegetative", false}, // exactly off boundary (06:00+18h)
		{"03:00", "vegetative", false},
		{"05:59", "vegetative", false},
		// Flowering default is 12/12: on 06:00–18:00.
		{"12:00", "flowering", true},
		{"18:00", "flowering", false},
		{"19:00", "flowering", false},
		// Drying default is 0h: always dark.
		{"12:00", "drying", false},
		// An unknown stage falls back to 18h (like vegetative).
		{"12:00", "fruiting", true},
	}
	for _, c := range cases {
		got, ok := s.DesiredOn(c.stage, at(c.now))
		if !ok {
			t.Fatalf("phase mode should drive the light")
		}
		if got != c.want {
			t.Errorf("DesiredOn(%s, %s) = %v, want %v", c.stage, c.now, got, c.want)
		}
	}
}

func TestDesiredOn_WrapMidnight(t *testing.T) {
	// Lights on at 20:00 for 12h -> on 20:00–08:00, wrapping midnight.
	s := LightSchedule{Mode: LightScheduleCustom, LightsOnAt: "20:00", Anchor: AnchorStart, OnHours: 12}
	on := []string{"20:00", "23:30", "00:30", "07:59"}
	off := []string{"08:00", "12:00", "19:59"}
	for _, hhmm := range on {
		if got, _ := s.DesiredOn("vegetative", at(hhmm)); !got {
			t.Errorf("expected light ON at %s", hhmm)
		}
	}
	for _, hhmm := range off {
		if got, _ := s.DesiredOn("vegetative", at(hhmm)); got {
			t.Errorf("expected light OFF at %s", hhmm)
		}
	}
}

func TestDesiredOn_OffMode(t *testing.T) {
	s := LightSchedule{Mode: LightScheduleOff, LightsOnAt: "06:00", OnHours: 18}
	if _, ok := s.DesiredOn("vegetative", at("12:00")); ok {
		t.Errorf("off mode must not drive the light")
	}
}

func TestNextTransition(t *testing.T) {
	// 12/12 from 06:00 -> transitions at 06:00 and 18:00.
	s := LightSchedule{Mode: LightScheduleCustom, LightsOnAt: "06:00", Anchor: AnchorStart, OnHours: 12}
	if got := s.NextTransition("vegetative", at("10:00")); got.Hour() != 18 {
		t.Errorf("next transition after 10:00 should be 18:00, got %v", got)
	}
	if got := s.NextTransition("vegetative", at("20:00")); got.Hour() != 6 {
		t.Errorf("next transition after 20:00 should be next 06:00, got %v", got)
	}
	// Always-on / always-off schedules have no boundary.
	dark := LightSchedule{Mode: LightScheduleCustom, LightsOnAt: "06:00", OnHours: 0}
	if got := dark.NextTransition("vegetative", at("10:00")); !got.IsZero() {
		t.Errorf("always-off schedule should have no transition, got %v", got)
	}
}

func TestEffectiveOnHours_Override(t *testing.T) {
	s := LightSchedule{Mode: LightSchedulePhase, StageOnHours: map[string]float64{"flowering": 11}}
	if h := s.EffectiveOnHours("flowering"); h != 11 {
		t.Errorf("override should win: got %v", h)
	}
	if h := s.EffectiveOnHours("vegetative"); h != 18 {
		t.Errorf("veg should fall back to default 18: got %v", h)
	}
}

func TestWindow_Anchors(t *testing.T) {
	// The grower's day: 18/6 runs 06:00–00:00 under every anchor below, and
	// each anchor moves a 12/12 stage differently.
	cases := []struct {
		name          string
		anchor        LightAnchor
		anchorAt      string
		wantVegOn     int
		wantVegOff    int
		wantFlowerOn  int
		wantFlowerOff int
	}{
		{"start", AnchorStart, "06:00", 6 * 60, 0, 6 * 60, 18 * 60},
		{"center", AnchorCenter, "15:00", 6 * 60, 0, 9 * 60, 21 * 60},
		{"end", AnchorEnd, "00:00", 6 * 60, 0, 12 * 60, 0},
	}
	for _, c := range cases {
		s := LightSchedule{Mode: LightSchedulePhase, LightsOnAt: c.anchorAt, Anchor: c.anchor}
		on, off, ok := s.Window("vegetative")
		if !ok || on != c.wantVegOn || off != c.wantVegOff {
			t.Errorf("%s: vegetative window = %d–%d (ok=%v), want %d–%d", c.name, on, off, ok, c.wantVegOn, c.wantVegOff)
		}
		on, off, ok = s.Window("flowering")
		if !ok || on != c.wantFlowerOn || off != c.wantFlowerOff {
			t.Errorf("%s: flowering window = %d–%d (ok=%v), want %d–%d", c.name, on, off, ok, c.wantFlowerOn, c.wantFlowerOff)
		}
	}
}

func TestDesiredOn_CenterAnchor(t *testing.T) {
	// 12/12 centred on 15:00 is on 09:00–21:00.
	s := LightSchedule{Mode: LightSchedulePhase, LightsOnAt: "15:00", Anchor: AnchorCenter}
	on := []string{"09:00", "15:00", "20:59"}
	off := []string{"08:59", "21:00", "03:00"}
	for _, hhmm := range on {
		if got, _ := s.DesiredOn("flowering", at(hhmm)); !got {
			t.Errorf("expected light ON at %s", hhmm)
		}
	}
	for _, hhmm := range off {
		if got, _ := s.DesiredOn("flowering", at(hhmm)); got {
			t.Errorf("expected light OFF at %s", hhmm)
		}
	}
	// The transition times follow the anchored window, not the anchor time.
	if got := s.NextTransition("flowering", at("10:00")); got.Hour() != 21 {
		t.Errorf("next transition after 10:00 should be 21:00, got %v", got)
	}
}

func TestWindow_CenterWrapsMidnight(t *testing.T) {
	// A night-time photoperiod: 12h centred on 03:00 runs 21:00–09:00.
	s := LightSchedule{Mode: LightScheduleCustom, LightsOnAt: "03:00", Anchor: AnchorCenter, OnHours: 12}
	on, off, ok := s.Window("flowering")
	if !ok || on != 21*60 || off != 9*60 {
		t.Fatalf("window = %d–%d (ok=%v), want 1260–540", on, off, ok)
	}
	if got, _ := s.DesiredOn("flowering", at("23:00")); !got {
		t.Errorf("expected light ON at 23:00")
	}
	if got, _ := s.DesiredOn("flowering", at("12:00")); got {
		t.Errorf("expected light OFF at 12:00")
	}
}

func TestNextTransitionAcrossDaylightSaving(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatal(err)
	}
	s := LightSchedule{Mode: LightSchedulePhase, LightsOnAt: "15:00", Anchor: AnchorCenter}
	for _, tc := range []struct{ now, next string }{
		{"2026-10-24 22:00", "2026-10-25 09:00"},
		{"2026-10-25 08:00", "2026-10-25 09:00"},
		{"2026-10-25 10:00", "2026-10-25 21:00"},
		{"2026-03-28 22:00", "2026-03-29 09:00"},
		{"2026-03-29 08:00", "2026-03-29 09:00"},
		{"2026-03-29 10:00", "2026-03-29 21:00"},
	} {
		t.Run(tc.now, func(t *testing.T) {
			now, err := time.ParseInLocation("2006-01-02 15:04", tc.now, loc)
			if err != nil {
				t.Fatal(err)
			}
			want, err := time.ParseInLocation("2006-01-02 15:04", tc.next, loc)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.NextTransition("flowering", now); !got.Equal(want) {
				t.Fatalf("next transition = %v, want %v", got, want)
			}
		})
	}
}
