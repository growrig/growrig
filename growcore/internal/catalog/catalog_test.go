package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProducts loads the catalog submodule's devices/ tree and checks the
// directory-derived invariants hold.
func TestProducts(t *testing.T) {
	products := Products()
	if len(products) == 0 {
		t.Fatal("no products loaded; expected the repo-root devices/ tree")
	}

	seen := map[string]bool{}
	for _, p := range products {
		if p.ID == "" {
			t.Errorf("product with empty id: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate product id %q", p.ID)
		}
		seen[p.ID] = true

		if !validCategory(p.Category) {
			t.Errorf("product %q has invalid category %q", p.ID, p.Category)
		}
		if p.Brand == "" || p.Model == "" {
			t.Errorf("product %q missing brand/model", p.ID)
		}
	}

	// Categories must be emitted in categoryOrder.
	last := -1
	for _, p := range products {
		if r := categoryRank(p.Category); r < last {
			t.Errorf("products not ordered by category: %q (rank %d) after rank %d", p.ID, r, last)
		} else {
			last = r
		}
	}

	// Spot-check a multi-binding device survived the round-trip.
	xiaomi := find(products, "xiaomi-lywsd03mmc")
	if xiaomi == nil {
		t.Fatal("expected xiaomi-lywsd03mmc in catalog")
	}
	if xiaomi.Category != CatSensor {
		t.Errorf("xiaomi category = %q, want sensor", xiaomi.Category)
	}
	if len(xiaomi.Provides) != 2 {
		t.Fatalf("xiaomi provides %d bindings, want 2", len(xiaomi.Provides))
	}
	if xiaomi.Provides[0].Measurement != "temperature" || xiaomi.Provides[1].Measurement != "humidity" {
		t.Errorf("xiaomi provides = %+v, want temperature+humidity", xiaomi.Provides)
	}
}

func TestVendors(t *testing.T) {
	if len(Vendors()) == 0 {
		t.Fatal("no vendors loaded")
	}
	cloudline := find(Products(), "ac-infinity-cloudline")
	if cloudline == nil || cloudline.Vendor != "ac-infinity" {
		t.Fatalf("cloudline vendor = %#v", cloudline)
	}
	if Vendors()[0].Color == "" || Vendors()[0].Background == "" {
		t.Fatal("expected vendor fallback colors")
	}
}

func TestExtraCatalogOverridesBuiltInProductAndAssets(t *testing.T) {
	dir := t.TempDir()
	deviceDir := filepath.Join(dir, "sensor", "xiaomi-lywsd03mmc")
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `brand: Community
model: Replacement sensor
connection: home-assistant
version: "2"
author: Test
image: replacement.png
provides:
  - label: Temperature
    kind: sensor
    measurement: temperature
    entityDomain: sensor
`
	if err := os.WriteFile(filepath.Join(deviceDir, "device.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deviceDir, "replacement.png"), []byte("custom-image"), 0o644); err != nil {
		t.Fatal(err)
	}

	SetExtraDirs([]ExtraDir{{SourceID: "community", Dir: dir}})
	t.Cleanup(func() { SetExtraDirs(nil) })
	product := find(Products(), "xiaomi-lywsd03mmc")
	if product == nil || product.Source != "community" || product.Brand != "Community" {
		t.Fatalf("overridden product = %#v", product)
	}
	raw, err := DeviceAsset("sensor", "xiaomi-lywsd03mmc", "replacement.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "custom-image" {
		t.Fatalf("asset = %q", raw)
	}
}

func find(products []Product, id string) *Product {
	for i := range products {
		if products[i].ID == id {
			return &products[i]
		}
	}
	return nil
}
