package store

import (
	"testing"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
)

// seedHistory builds a grow with a plant and one of every kind of record,
// spaced a minute apart so their order is unambiguous.
func seedHistory(t *testing.T) (*Store, time.Time) {
	t.Helper()
	st := open(t)
	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)

	if err := st.SaveEnvironment(domain.Environment{ID: "env-1", Name: "Tent A", Kind: domain.KindTent}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveGrow(domain.Grow{ID: "grow-1", Name: "G", Stages: domain.DefaultStages, Status: domain.GrowActive}); err != nil {
		t.Fatal(err)
	}
	if err := st.SavePlantUnit(domain.PlantUnit{
		ID: "plant-1", GrowID: "grow-1", Label: "Cheese #1", Quantity: 1, CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddStageEvent("grow-1", "vegetative", base.Add(1*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCareEvent(domain.CareEvent{
		ID: "care-1", GrowID: "grow-1", Type: "water", OccurredAt: base.Add(2 * time.Minute),
		Applications: []domain.CareApplication{
			{ID: "app-1", CareEventID: "care-1", PlantUnitID: "plant-1", AmountML: 200},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddGrowPhoto(domain.GrowPhoto{
		ID: "photo-1", GrowID: "grow-1", PlantUnitID: "plant-1", TakenAt: base.Add(3 * time.Minute), File: "a.jpg",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddActivity(domain.Activity{
		ID: "act-1", GrowID: "grow-1", Time: base.Add(4 * time.Minute),
		Level: "info", Type: "configuration", Message: "Edited stage dates",
	}); err != nil {
		t.Fatal(err)
	}
	return st, base
}

func TestHistoryMergesSourcesNewestFirst(t *testing.T) {
	st, _ := seedHistory(t)
	page, err := st.History(domain.HistoryQuery{GrowID: "grow-1"})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []domain.HistoryKind
	for _, e := range page.Items {
		kinds = append(kinds, e.Kind)
	}
	want := []domain.HistoryKind{
		domain.HistorySystem, domain.HistoryPhoto, domain.HistoryCare, domain.HistoryMilestone,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if page.Total != 4 {
		t.Errorf("total = %d, want 4", page.Total)
	}
	// The care entry must arrive hydrated, with its plant resolved for linking.
	for _, e := range page.Items {
		if e.Kind != domain.HistoryCare {
			continue
		}
		if e.Care == nil || len(e.Care.Applications) != 1 {
			t.Fatalf("care entry not hydrated: %+v", e.Care)
		}
		if len(e.Refs) != 1 || e.Refs[0].Label != "Cheese #1" {
			t.Fatalf("care refs = %+v, want the plant label", e.Refs)
		}
	}
}

// Paging must walk the merged stream, not one source at a time.
func TestHistoryPagesAcrossSources(t *testing.T) {
	st, _ := seedHistory(t)
	first, err := st.History(domain.HistoryQuery{GrowID: "grow-1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.History(domain.HistoryQuery{GrowID: "grow-1", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || len(second.Items) != 2 {
		t.Fatalf("page sizes = %d and %d, want 2 and 2", len(first.Items), len(second.Items))
	}
	if first.Total != 4 || second.Total != 4 {
		t.Errorf("totals = %d and %d, want 4 — total is of the whole scope", first.Total, second.Total)
	}
	// Pages must not overlap.
	for _, a := range first.Items {
		for _, b := range second.Items {
			if a.ID == b.ID {
				t.Fatalf("entry %q appears on both pages", a.ID)
			}
		}
	}
	if !first.Items[1].At.After(second.Items[0].At) {
		t.Error("pages are not in descending time order across the boundary")
	}
}

// Care is written twice — as a CareEvent and as an activity echo — so History
// must never surface the echo, or every watering would appear twice.
func TestHistoryExcludesCareEchoes(t *testing.T) {
	st, base := seedHistory(t)
	if err := st.AddActivity(domain.Activity{
		ID: "act-echo", GrowID: "grow-1", Time: base.Add(2 * time.Minute),
		Level: "info", Type: "care", Message: "Watered 1 plant · 200 ml",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := st.History(domain.HistoryQuery{GrowID: "grow-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if e.Activity != nil && e.Activity.ID == "act-echo" {
			t.Fatal("the care echo leaked into History")
		}
	}
	if page.Total != 4 {
		t.Errorf("total = %d, want 4 (the echo must not be counted either)", page.Total)
	}
}

// Routine control chatter is excluded unless asked for; control *failures* are
// never treated as noise.
func TestHistoryAutomationFilter(t *testing.T) {
	st, base := seedHistory(t)
	if err := st.AddActivity(domain.Activity{
		ID: "act-fan", EnvironmentID: "env-1", Time: base.Add(5 * time.Minute),
		Level: "info", Type: "control", Message: "Set Fan to 79%",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddActivity(domain.Activity{
		ID: "act-fail", EnvironmentID: "env-1", Time: base.Add(6 * time.Minute),
		Level: "error", Type: "control", Message: "Failed to set Fan speed",
	}); err != nil {
		t.Fatal(err)
	}

	quiet, err := st.History(domain.HistoryQuery{EnvironmentID: "env-1"})
	if err != nil {
		t.Fatal(err)
	}
	if quiet.Total != 1 {
		t.Fatalf("total = %d, want 1 — only the failure", quiet.Total)
	}
	if quiet.Items[0].Activity.ID != "act-fail" {
		t.Errorf("kept %q, want the control failure", quiet.Items[0].Activity.ID)
	}

	loud, err := st.History(domain.HistoryQuery{EnvironmentID: "env-1", Automation: true})
	if err != nil {
		t.Fatal(err)
	}
	if loud.Total != 2 {
		t.Fatalf("total = %d, want 2 with automation included", loud.Total)
	}
	for _, e := range loud.Items {
		if e.Activity.ID == "act-fan" && !e.Automation {
			t.Error("the fan entry should be flagged as automation")
		}
	}
}

// A plant's history is the events that touched it, not its whole grow's.
func TestHistoryScopedToPlant(t *testing.T) {
	st, base := seedHistory(t)
	if err := st.SavePlantUnit(domain.PlantUnit{
		ID: "plant-2", GrowID: "grow-1", Label: "Cheese #2", Quantity: 1, CreatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCareEvent(domain.CareEvent{
		ID: "care-2", GrowID: "grow-1", Type: "feed", OccurredAt: base.Add(7 * time.Minute),
		Applications: []domain.CareApplication{
			{ID: "app-2", CareEventID: "care-2", PlantUnitID: "plant-2"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	page, err := st.History(domain.HistoryQuery{PlantID: "plant-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Items {
		if e.Care != nil && e.Care.ID == "care-2" {
			t.Fatal("another plant's care event leaked into this plant's history")
		}
	}
	// Its own care and photo, and nothing from the grow at large.
	if page.Total != 2 {
		t.Errorf("total = %d, want 2 (its care event and its photo)", page.Total)
	}
}

func TestHistoryKindFilter(t *testing.T) {
	st, _ := seedHistory(t)
	page, err := st.History(domain.HistoryQuery{
		GrowID: "grow-1", Kinds: []domain.HistoryKind{domain.HistoryCare},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Kind != domain.HistoryCare {
		t.Fatalf("kind filter returned %d items: %+v", len(page.Items), page.Items)
	}
}
