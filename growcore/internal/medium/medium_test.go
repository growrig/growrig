package medium

import "testing"

// Every shipped recipe must describe a whole mix. A builtin whose components
// don't total 100% is a transcription slip, and it would surface in the app as
// a permanent "doesn't add up" warning the user can't fix (builtins are
// read-only), so catch it here instead.
func TestBuiltinComponentsTotal100(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no builtin medium recipes loaded")
	}
	for _, r := range all {
		if total := r.TotalPercent(); total != 100 {
			t.Errorf("%s: components total %.1f%%, want 100", r.ID, total)
		}
		if r.Source != "builtin" {
			t.Errorf("%s: source = %q, want builtin", r.ID, r.Source)
		}
	}
}

func TestGetByID(t *testing.T) {
	r, ok := Get("coco-perlite-70-30")
	if !ok {
		t.Fatal("coco-perlite-70-30 not found")
	}
	if len(r.Components) != 2 || r.Base != "coco" {
		t.Fatalf("unexpected recipe: %+v", r)
	}
	if _, ok := Get("nope"); ok {
		t.Error("Get returned a recipe for an unknown id")
	}
}
