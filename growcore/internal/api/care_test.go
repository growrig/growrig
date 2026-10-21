package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
)

// careGrow sets up a one-plant cannabis grow whose plant starts in a small pot.
func careGrow(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SaveGrow(domain.Grow{
		ID: "g1", Name: "G", Species: "cannabis", Stage: "vegetative",
		Stages: []string{"seedling", "vegetative"}, Status: domain.GrowActive, StageStarted: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SavePlantUnit(domain.PlantUnit{
		ID: "p1", GrowID: "g1", Label: "Plant", Tracking: domain.TrackIndividual,
		Quantity: 1, Status: domain.PlantActive, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Repot("p1", 7.6, "L", "fabric", time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return &Server{store: st}, st, "g1"
}

func logCareReq(t *testing.T, s *Server, growID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.SetPathValue("id", growID)
	rec := httptest.NewRecorder()
	s.logCare(rec, req)
	return rec
}

func currentPot(t *testing.T, st *store.Store, unitID string) domain.PlantPot {
	t.Helper()
	pots, err := st.PotsForUnit(unitID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pots {
		if p.EndedAt == nil {
			return p
		}
	}
	t.Fatalf("plant %s has no current pot (%d in history)", unitID, len(pots))
	return domain.PlantPot{}
}

// Logging a transplant must actually move the plant into the new container —
// not just mention the size in the journal note.
func TestLogCareTransplantRepotsThePlant(t *testing.T) {
	s, st, growID := careGrow(t)

	rec := logCareReq(t, s, growID, `{"type":"transplant","plantUnitIds":["p1"],"potSize":20,"potUnit":"L","potType":"air-pot"}`)
	if rec.Code != 200 {
		t.Fatalf("logCare: code=%d body=%s", rec.Code, rec.Body.String())
	}

	pot := currentPot(t, st, "p1")
	if pot.Size != 20 || pot.Unit != "L" || pot.Type != "air-pot" {
		t.Errorf("current pot = %v %s %q, want 20 L air-pot", pot.Size, pot.Unit, pot.Type)
	}
	pots, err := st.PotsForUnit("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pots) != 2 {
		t.Fatalf("expected the 7.6 L pot to be kept in history, got %d pot(s)", len(pots))
	}
}

// Care actions that don't collect a pot size must never repot, even if a stray
// potSize rides along in the request.
func TestLogCareWaterIgnoresPotSize(t *testing.T) {
	s, st, growID := careGrow(t)

	rec := logCareReq(t, s, growID, `{"type":"water","plantUnitIds":["p1"],"amountMl":500,"potSize":20}`)
	if rec.Code != 200 {
		t.Fatalf("logCare: code=%d body=%s", rec.Code, rec.Body.String())
	}

	if pot := currentPot(t, st, "p1"); pot.Size != 7.6 {
		t.Errorf("watering changed the pot to %v, want it left at 7.6", pot.Size)
	}
}

// A transplant with no size given is a valid journal entry (e.g. moved to open
// ground) and must leave the pot history alone rather than opening a 0 L pot.
func TestLogCareTransplantWithoutSizeKeepsPot(t *testing.T) {
	s, st, growID := careGrow(t)

	rec := logCareReq(t, s, growID, `{"type":"transplant","plantUnitIds":["p1"],"notes":"into the bed"}`)
	if rec.Code != 200 {
		t.Fatalf("logCare: code=%d body=%s", rec.Code, rec.Body.String())
	}

	pots, err := st.PotsForUnit("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pots) != 1 || pots[0].Size != 7.6 {
		t.Errorf("expected the original pot untouched, got %d pot(s): %+v", len(pots), pots)
	}
}

// Every targeted plant is repotted, not only the first.
func TestLogCareTransplantRepotsEveryTargetedPlant(t *testing.T) {
	s, st, growID := careGrow(t)
	if err := st.SavePlantUnit(domain.PlantUnit{
		ID: "p2", GrowID: growID, Label: "Plant", Tracking: domain.TrackIndividual,
		Quantity: 1, Status: domain.PlantActive, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	rec := logCareReq(t, s, growID, `{"type":"transplant","potSize":11,"potUnit":"L"}`)
	if rec.Code != 200 {
		t.Fatalf("logCare: code=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, unit := range []string{"p1", "p2"} {
		if pot := currentPot(t, st, unit); pot.Size != 11 {
			t.Errorf("plant %s pot = %v, want 11", unit, pot.Size)
		}
	}
}

// Editing corrects when it happened, the note and the amount, without touching
// the action type.
func TestUpdateCareCorrectsAnEntry(t *testing.T) {
	s, st, growID := careGrow(t)
	rec := logCareReq(t, s, growID, `{"type":"water","plantUnitIds":["p1"],"amountMl":500,"notes":"tap"}`)
	if rec.Code != 200 {
		t.Fatalf("logCare: code=%d body=%s", rec.Code, rec.Body.String())
	}
	events, err := st.CareEvents(growID, 10, 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("expected 1 event, got %d (%v)", len(events), err)
	}
	eventID := events[0].ID

	req := httptest.NewRequest("PUT", "/", strings.NewReader(
		`{"occurredAt":"2026-07-01T08:30:00Z","notes":"rainwater","plantUnitIds":["p1"],"amountMl":750,"ph":6.2}`))
	req.SetPathValue("id", eventID)
	put := httptest.NewRecorder()
	s.updateCare(put, req)
	if put.Code != 200 {
		t.Fatalf("updateCare: code=%d body=%s", put.Code, put.Body.String())
	}

	got, ok, err := st.CareEvent(eventID)
	if err != nil || !ok {
		t.Fatalf("reload: %v (found=%v)", err, ok)
	}
	if got.Type != "water" {
		t.Errorf("type changed to %q", got.Type)
	}
	if got.Notes != "rainwater" || got.PH != 6.2 {
		t.Errorf("notes=%q ph=%v, want rainwater / 6.2", got.Notes, got.PH)
	}
	if !got.OccurredAt.UTC().Equal(time.Date(2026, 7, 1, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("occurredAt = %s, want 2026-07-01T08:30:00Z", got.OccurredAt)
	}
	if len(got.Applications) != 1 || got.Applications[0].AmountML != 750 {
		t.Errorf("applications = %+v, want one of 750 ml", got.Applications)
	}
}

// Editing must not silently duplicate the per-plant rows it replaces.
func TestUpdateCareReplacesApplicationsWithoutDuplicating(t *testing.T) {
	s, st, growID := careGrow(t)
	if err := st.SavePlantUnit(domain.PlantUnit{
		ID: "p2", GrowID: growID, Label: "Plant", Tracking: domain.TrackIndividual,
		Quantity: 1, Status: domain.PlantActive, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	logCareReq(t, s, growID, `{"type":"water","amountMl":100}`)
	events, _ := st.CareEvents(growID, 10, 0)
	eventID := events[0].ID

	// Drop p2 from the event: only p1 was actually watered.
	req := httptest.NewRequest("PUT", "/", strings.NewReader(`{"plantUnitIds":["p1"],"amountMl":100}`))
	req.SetPathValue("id", eventID)
	put := httptest.NewRecorder()
	s.updateCare(put, req)
	if put.Code != 200 {
		t.Fatalf("updateCare: code=%d body=%s", put.Code, put.Body.String())
	}

	got, _, _ := st.CareEvent(eventID)
	if len(got.Applications) != 1 || got.Applications[0].PlantUnitID != "p1" {
		t.Errorf("applications = %+v, want exactly p1", got.Applications)
	}
}

// An edit naming a plant outside the grow is rejected rather than silently dropped.
func TestUpdateCareRejectsUnknownPlant(t *testing.T) {
	s, st, growID := careGrow(t)
	logCareReq(t, s, growID, `{"type":"water","plantUnitIds":["p1"],"amountMl":100}`)
	events, _ := st.CareEvents(growID, 10, 0)

	req := httptest.NewRequest("PUT", "/", strings.NewReader(`{"plantUnitIds":["nope"]}`))
	req.SetPathValue("id", events[0].ID)
	put := httptest.NewRecorder()
	s.updateCare(put, req)

	if put.Code != 400 {
		t.Fatalf("expected 400 for a plant outside the grow, got %d", put.Code)
	}
}
