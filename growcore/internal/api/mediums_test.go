package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
)

func newMediumServer(t *testing.T) *Server {
	t.Helper()
	// A file-backed store: user medium recipes persist as YAML next to the db,
	// so :memory: would have nowhere to write them.
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Server{store: st}
}

func createMediumReq(t *testing.T, s *Server, body string) (domain.MediumRecipe, int) {
	t.Helper()
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.createMediumRecipe(rec, req)
	var r domain.MediumRecipe
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
	}
	return r, rec.Code
}

func TestCreateMediumRecipeRoundTrips(t *testing.T) {
	s := newMediumServer(t)
	created, code := createMediumReq(t, s, `{
		"name": "  My coco mix  ",
		"base": "coco",
		"components": [
			{"label": " Coco coir ", "percent": 70},
			{"label": "Perlite", "percent": 30}
		],
		"amendments": [{"label": "Dolomite lime", "amount": 5, "unit": "g"}]
	}`)
	if code != 200 {
		t.Fatalf("create: code=%d", code)
	}
	if created.Name != "My coco mix" {
		t.Errorf("name not trimmed: %q", created.Name)
	}
	if created.Source != "user" {
		t.Errorf("source = %q, want user", created.Source)
	}
	if len(created.Components) != 2 || created.Components[0].Label != "Coco coir" {
		t.Fatalf("components wrong: %+v", created.Components)
	}
	if total := created.TotalPercent(); total != 100 {
		t.Errorf("total = %v, want 100", total)
	}
	if len(created.Amendments) != 1 || created.Amendments[0].Unit != "g" {
		t.Fatalf("amendments wrong: %+v", created.Amendments)
	}

	// It must come back from the store as saved.
	got, ok, err := s.store.MediumRecipe(created.ID)
	if err != nil || !ok {
		t.Fatalf("reload: ok=%v err=%v", ok, err)
	}
	if len(got.Components) != 2 || got.Base != "coco" {
		t.Fatalf("reloaded recipe wrong: %+v", got)
	}
}

// Percentages that don't add up are the grower's to fix: the server must store
// what was entered rather than quietly rescaling it to 100.
func TestCreateMediumRecipeKeepsUnbalancedPercentages(t *testing.T) {
	s := newMediumServer(t)
	created, code := createMediumReq(t, s, `{
		"name": "Half a mix",
		"components": [{"label": "Coco", "percent": 60}]
	}`)
	if code != 200 {
		t.Fatalf("create: code=%d", code)
	}
	if total := created.TotalPercent(); total != 60 {
		t.Errorf("total = %v, want the 60 that was entered", total)
	}
}

func TestCreateMediumRecipeRejectsEmptyMix(t *testing.T) {
	s := newMediumServer(t)
	// Blank labels and non-positive percentages are dropped, which leaves no
	// components at all — that is a bad request, not an empty recipe.
	if _, code := createMediumReq(t, s, `{
		"name": "Nothing",
		"components": [{"label": "  ", "percent": 50}, {"label": "Perlite", "percent": 0}]
	}`); code != 400 {
		t.Errorf("code = %d, want 400", code)
	}
	if _, code := createMediumReq(t, s, `{"name": "", "components": [{"label": "Coco", "percent": 100}]}`); code != 400 {
		t.Errorf("nameless recipe: code = %d, want 400", code)
	}
}

func TestCreateMediumRecipeRejectsUnknownSpecies(t *testing.T) {
	s := newMediumServer(t)
	if _, code := createMediumReq(t, s, `{
		"name": "Tagged",
		"species": "not-a-crop",
		"components": [{"label": "Coco", "percent": 100}]
	}`); code != 400 {
		t.Errorf("code = %d, want 400", code)
	}
	// An empty tag means "any crop" and must be accepted.
	if _, code := createMediumReq(t, s, `{
		"name": "Untagged",
		"components": [{"label": "Coco", "percent": 100}]
	}`); code != 200 {
		t.Errorf("untagged recipe: code = %d, want 200", code)
	}
}
