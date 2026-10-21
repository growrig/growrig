package domain

import "time"

// Care is the grow's manual-action journal: watering, feeding, inspecting,
// training, trimming, and every other hands-on action performed on plants. A
// single care action is a session — one CareEvent targeting one or more plants
// — so "mixed 5 L and fed all four plants" is recorded once while still noting
// what each plant received. The set of available actions and their form fields
// comes from the grow's species (see package species, CareActionsFor).
//
// Feeding is watering with nutrients: both are ordinary CareEvents (types
// "water" / "feed") and differ only in which optional solution fields are set.

// CareSource distinguishes a hand-logged action from one recorded by automation
// (irrigation/dosing equipment). Automation-sourced care lands in Phase 5; the
// field exists now so the journal can badge entries without a later migration.
type CareSource string

const (
	CareManual     CareSource = "manual"
	CareAutomation CareSource = "automation"
)

// CareEvent is one care action performed at a moment in time against a grow's
// plants. Solution fields (Recipe/PH/EC/Runoff) apply to watering and feeding
// and are zero for other action types.
type CareEvent struct {
	ID         string     `json:"id"`
	GrowID     string     `json:"growId"`
	Type       string     `json:"type"` // a species CareAction key (water, feed, …)
	OccurredAt time.Time  `json:"occurredAt"`
	Source     CareSource `json:"source"`
	Notes      string     `json:"notes,omitempty"`

	// Feeding/watering solution. RecipeID references a FeedingRecipe.
	RecipeID string  `json:"recipeId,omitempty"`
	PH       float64 `json:"ph,omitempty"`
	EC       float64 `json:"ec,omitempty"`
	RunoffML float64 `json:"runoffMl,omitempty"`
	RunoffPH float64 `json:"runoffPh,omitempty"`

	// Doses is what each plant received of a recipe's non-liquid products, keyed
	// by FeedingProduct.Key, in that product's unit — "2 tablets and 1 g of
	// mycorrhiza per plant". It records amendments, which are dosed per plant or
	// per pot rather than per litre of solution, so they cannot be expressed by
	// CareApplication.AmountML. The amount is per plant, not per event: the
	// event's Applications say which plants got it.
	Doses map[string]float64 `json:"doses,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	// Applications record what each targeted plant received in this event.
	Applications []CareApplication `json:"applications"`
}

// CareApplication is what a single plant unit received in a care event: the
// per-plant amount and an optional per-plant note (override or remark).
type CareApplication struct {
	ID          string  `json:"id"`
	CareEventID string  `json:"careEventId"`
	PlantUnitID string  `json:"plantUnitId"`
	AmountML    float64 `json:"amountMl,omitempty"`
	Note        string  `json:"note,omitempty"`
	// PlantLabel is a display-only convenience filled by the API so clients can
	// name the plant without a second lookup. It is never persisted.
	PlantLabel string `json:"plantLabel,omitempty"`
}

// GrowCareConfig customizes a grow's care actions on top of its species
// defaults: enable/disable, reorder, rename, quick-action flags, and any
// user-added custom actions. An absent config means "use the species defaults
// as-is". When present, it is the authoritative ordered action list; species
// actions it omits are treated as newly added and appended, enabled.
type GrowCareConfig struct {
	Actions []GrowCareActionConfig `json:"actions"`
}

// GrowCareActionConfig is one action's per-grow customization. Key references a
// species action key, or names a custom action (Custom == true). Label, when
// set, overrides the species label. Fields is used only for custom actions
// (built-in actions keep their species-defined fields).
type GrowCareActionConfig struct {
	Key     string   `json:"key"`
	Label   string   `json:"label,omitempty"`
	Enabled bool     `json:"enabled"`
	Quick   bool     `json:"quick"`
	Custom  bool     `json:"custom,omitempty"`
	Fields  []string `json:"fields,omitempty"`
}

// TotalML sums the amount applied across all plants in the event, e.g. for a
// "3.2 L total" journal summary.
func (e CareEvent) TotalML() float64 {
	var total float64
	for _, a := range e.Applications {
		total += a.AmountML
	}
	return total
}
