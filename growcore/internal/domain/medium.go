package domain

import "time"

// MediumRecipe is a growing-medium composition: what a substrate is mixed from.
// Where GrowingSetup.Medium records the coarse category a grow uses ("coco"),
// a recipe records the actual mix — "70% coco, 30% perlite, 5 g/L dolomite
// lime" — so it can be reproduced, compared between grows, and shopped for.
//
// A recipe has two kinds of ingredient, because real mixes are written with
// both and they do not share a unit:
//
//   - Components are bulk ingredients measured as a share of total volume.
//     Their percentages describe the whole mix and so should total 100.
//   - Amendments are dosed against the finished volume instead (5 g per litre
//     of mix), and are deliberately excluded from that total.
//
// Built-in recipes are defined as YAML under mediums/mediums.yaml in the
// catalog and are read-only (Source == "builtin"); user recipes are created
// in-app and stored as YAML on disk (Source == "user"). Both share this shape
// and one API surface, mirroring FeedingRecipe.
type MediumRecipe struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Source is "builtin" (YAML, read-only) or "user" (editable). Derived at
	// load time, never persisted in the recipe body.
	Source string `json:"source" yaml:"-"`
	// Species optionally tags a recipe as crop-specific. Recipes are global —
	// a coco/perlite mix serves any crop — so an empty value means "any", and
	// the tag only sorts and filters the list.
	Species string `json:"species,omitempty" yaml:"species,omitempty"`
	// Base is the coarse medium category this mix produces (soil, coco, …), so
	// picking a recipe can fill in the grow's medium.
	Base       string            `json:"base,omitempty" yaml:"base,omitempty"`
	Components []MediumComponent `json:"components" yaml:"components"`
	Amendments []MediumAmendment `json:"amendments,omitempty" yaml:"amendments,omitempty"`
	CreatedAt  time.Time         `json:"createdAt" yaml:"-"`
}

// MediumComponent is one bulk ingredient as a share of total mix volume.
//
// An ingredient is normally a catalog product (ProductID, e.g.
// "consumables/biobizz-light-mix") — that is where its brand, image and
// description live, which is why the recipe itself carries no brand. Loose
// materials with no catalog entry (perlite, a local compost) are recorded by
// Label alone. Label is always set: for a product it snapshots the name at the
// time it was chosen, so a recipe still reads correctly if the catalog changes.
type MediumComponent struct {
	ProductID string  `json:"productId,omitempty" yaml:"productId,omitempty"`
	Label     string  `json:"label" yaml:"label"`
	Percent   float64 `json:"percent" yaml:"percent"`
	Note      string  `json:"note,omitempty" yaml:"note,omitempty"`
}

// MediumAmendment is an ingredient dosed against the finished mix rather than
// taking up a share of it, e.g. 5 g of dolomite lime per litre. Unit is the
// amount's unit per litre ("g" means g/L).
type MediumAmendment struct {
	ProductID string  `json:"productId,omitempty" yaml:"productId,omitempty"`
	Label     string  `json:"label" yaml:"label"`
	Amount    float64 `json:"amount" yaml:"amount"`
	Unit      string  `json:"unit,omitempty" yaml:"unit,omitempty"`
	Note      string  `json:"note,omitempty" yaml:"note,omitempty"`
}

// TotalPercent sums the bulk components. A well-formed recipe totals 100;
// callers surface the difference rather than silently normalising, since a mix
// that doesn't add up is a mistake the grower should see.
func (r MediumRecipe) TotalPercent() float64 {
	var total float64
	for _, c := range r.Components {
		total += c.Percent
	}
	return total
}
