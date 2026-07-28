package domain

import "time"

// FeedingRecipe is a nutrient schedule: an ordered set of products (nutrient
// lines) dosed across an ordered set of phases, each phase spanning one or more
// weeks. It is deliberately flexible so it can model any brand's chart (e.g.
// BioBizz): phases are free-form and only optionally linked to a species stage.
//
// Built-in recipe templates are defined as YAML under species/<id>/feedings.yaml
// and are read-only (Source == "builtin"); user recipes are created in-app and
// stored as YAML on disk (Source == "user"). Both share this shape and one API
// surface.
type FeedingRecipe struct {
	ID          string `json:"id" yaml:"id"`
	Species     string `json:"species" yaml:"-"`
	Name        string `json:"name" yaml:"name"`
	Brand       string `json:"brand" yaml:"brand"`
	Description string `json:"description" yaml:"description"`
	// Source is "builtin" (YAML, read-only) or "user" (DB, editable). It is
	// derived at load time, never persisted in the preset body.
	Source string `json:"source" yaml:"-"`
	// Unit is the default dose unit (e.g. "ml/L"); a product may override it.
	Unit      string           `json:"unit" yaml:"unit"`
	Products  []FeedingProduct `json:"products" yaml:"products"`
	Phases    []FeedingPhase   `json:"phases" yaml:"phases"`
	CreatedAt time.Time        `json:"createdAt" yaml:"-"`
}

// FeedingProduct is one nutrient line in a schedule. Key identifies its dose in
// each week's Doses map; Unit overrides the preset default when set.
type FeedingProduct struct {
	Key   string `json:"key" yaml:"key"`
	Label string `json:"label" yaml:"label"`
	Unit  string `json:"unit,omitempty" yaml:"unit,omitempty"`
	// Basis is what a dose is measured against. Empty means BasisSolution —
	// the original and still overwhelmingly common case, where the dose is per
	// litre of water mixed. Dry amendments (tablets, mycorrhiza powder) are
	// instead dosed per plant, per pot, or per litre of substrate.
	Basis DoseBasis `json:"basis,omitempty" yaml:"basis,omitempty"`
}

// DoseBasis is the quantity a product's dose is measured against.
type DoseBasis string

const (
	// BasisSolution doses per litre of nutrient solution (the zero value).
	BasisSolution DoseBasis = ""
	// BasisPlant doses per individual plant (e.g. 2 tablets per plant).
	BasisPlant DoseBasis = "plant"
	// BasisPot doses per pot, regardless of how many plants are in it.
	BasisPot DoseBasis = "pot"
	// BasisSubstrate doses per litre of growing medium.
	BasisSubstrate DoseBasis = "substrate"
)

// ValidBasis reports whether b is a known dose basis.
func ValidBasis(b DoseBasis) bool {
	switch b {
	case BasisSolution, BasisPlant, BasisPot, BasisSubstrate:
		return true
	}
	return false
}

// PhaseTrigger is the event a one-shot phase is applied at, as opposed to the
// default week-by-week cadence.
type PhaseTrigger string

const (
	// TriggerWeekly is the zero value: the phase runs week by week via Weeks.
	TriggerWeekly PhaseTrigger = ""
	// TriggerPlanting applies once when the plant goes into its substrate.
	TriggerPlanting PhaseTrigger = "planting"
	// TriggerTransplant applies once at each pot-up.
	TriggerTransplant PhaseTrigger = "transplant"
	// TriggerTopdress applies once when top-dressing an established plant.
	TriggerTopdress PhaseTrigger = "topdress"
)

// ValidTrigger reports whether t is a known phase trigger.
func ValidTrigger(t PhaseTrigger) bool {
	switch t {
	case TriggerWeekly, TriggerPlanting, TriggerTransplant, TriggerTopdress:
		return true
	}
	return false
}

// FeedingPhase is a named span of the schedule (e.g. "Vegetative", "Flowering
// wk1-8"). Stage optionally links it to one of the species' cultivation stages.
//
// A phase is either weekly or event-triggered, never both:
//
//   - Trigger empty (the default): Weeks holds the per-week doses in order.
//   - Trigger set: the phase is a single application performed at that event —
//     a soil amendment buried at planting, say — and Doses holds the one-shot
//     amounts. Weeks is empty. Such a phase has no position on the week
//     timeline, so it must not be rendered as "week 1".
type FeedingPhase struct {
	Name    string       `json:"name" yaml:"name"`
	Stage   string       `json:"stage,omitempty" yaml:"stage,omitempty"`
	Trigger PhaseTrigger `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	// Doses is the one-shot dose map for a triggered phase, keyed by product
	// key exactly like FeedingWeek.Doses. Empty for weekly phases.
	Doses map[string]float64 `json:"doses,omitempty" yaml:"doses,omitempty"`
	Weeks []FeedingWeek      `json:"weeks" yaml:"weeks"`
}

// OneShot reports whether the phase is a single event-triggered application
// rather than a run of weeks.
func (p FeedingPhase) OneShot() bool { return p.Trigger != TriggerWeekly }

// FeedingWeek is one week of dosing: product key -> dose amount in the product's
// (or preset's) unit. A missing/zero key means that product isn't fed that week.
type FeedingWeek struct {
	Doses map[string]float64 `json:"doses" yaml:"doses"`
}
