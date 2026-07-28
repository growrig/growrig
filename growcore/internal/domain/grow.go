package domain

import (
	"regexp"
	"strings"
	"time"
)

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns a display name into a URL-friendly slug: lowercased, with each
// run of non-alphanumeric characters collapsed to a single hyphen and the
// leading/trailing hyphens trimmed. It returns "" for names with no
// alphanumeric content, letting callers supply their own fallback.
func Slugify(name string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// The cultivation layer sits beside the physical environment layer. A Grow is a
// crop-neutral cultivation run (species, cultivar, a configurable stage
// sequence); PlantUnits are the individually- or group-tracked plants that
// belong to a grow; and PlantPlacements record where each unit lives over time,
// forming a movement history. An Environment may nominate one Grow as its
// control grow, whose current stage supplies the automation presets (photoperiod).

// GrowStatus is the lifecycle state of a cultivation run.
type GrowStatus string

const (
	GrowActive    GrowStatus = "active"
	GrowCompleted GrowStatus = "completed"
	GrowArchived  GrowStatus = "archived"
)

// AllGrowStatuses lists the selectable grow statuses.
var AllGrowStatuses = []GrowStatus{GrowActive, GrowCompleted, GrowArchived}

// TrackingMode distinguishes an individually-tracked plant from a group (a
// tray, bed or batch) counted by quantity.
type TrackingMode string

const (
	TrackIndividual TrackingMode = "individual"
	TrackGroup      TrackingMode = "group"
)

// PlantStatus is the lifecycle state of a plant unit.
type PlantStatus string

const (
	PlantActive    PlantStatus = "active"
	PlantHarvested PlantStatus = "harvested"
	PlantRemoved   PlantStatus = "removed"
	PlantArchived  PlantStatus = "archived"
)

// StagePresets are the built-in, editable stage sequences per crop family. They
// are defaults a user starts from, not fixed phases: any grow can define its own
// ordered Stages.
var StagePresets = map[string][]string{
	"cannabis": {"seedling", "vegetative", "flowering", "flush", "drying", "cure"},
	"tomato":   {"seedling", "growth", "flowering", "fruiting"},
	"basil":    {"seedling", "growth", "harvest"},
}

// DefaultStages is the fallback stage sequence for a grow that specifies none.
var DefaultStages = StagePresets["cannabis"]

// DefaultStageLightHours gives the recommended daily hours of light for known
// stage names, seeding the photoperiod editor. Unknown stages fall back to 18h
// (see LightSchedule.EffectiveOnHours). These are editable defaults.
var DefaultStageLightHours = map[string]float64{
	"seedling":   18,
	"vegetative": 18,
	"growth":     18,
	"flowering":  12,
	"fruiting":   12,
	"flush":      12,
	"harvest":    12,
	"drying":     0,
	"cure":       0,
}

// GrowingSetup captures how a grow is cultivated: the growing medium and its
// composition, the nutrient method, and a default container that seeds each new
// plant's pot. It is stored per-grow (a whole grow usually shares one setup); a
// per-plant override lives on PlantPot. Irrigation is deliberately absent — it
// is a property of the physical environment (see Binding.IrrigationType), which
// the grow references rather than duplicates.
type GrowingSetup struct {
	Medium string `json:"medium,omitempty"` // soil | coco | soilless | hydroponic | aeroponic
	// MediumRecipeID references a MediumRecipe describing what the substrate is
	// actually mixed from. It is the grow's default: a plant with its own
	// MediumRecipeID overrides it, and an empty value on the plant means
	// "inherit this one". MediumDetails remains the free-text escape hatch for
	// a mix that hasn't been written up as a recipe.
	MediumRecipeID string  `json:"mediumRecipeId,omitempty"`
	MediumDetails  string  `json:"mediumDetails,omitempty"`  // free text: product / composition
	NutrientMethod string  `json:"nutrientMethod,omitempty"` // organic | mineral | living-soil
	PotSize        float64 `json:"potSize,omitempty"`        // default container volume
	PotUnit        string  `json:"potUnit,omitempty"`        // "L" or "gal"
	PotType        string  `json:"potType,omitempty"`        // fabric | plastic | terracotta | air-pot | other
}

// Grow is a cultivation run: a named crop with a species (a predefined crop
// family) and a stage sequence derived from that species. It is not bound to a
// single environment; its plants' whereabouts live in placements. Cultivar is
// tracked per PlantUnit, since one grow can mix cultivars.
type Grow struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Slug         string       `json:"slug"` // URL-friendly form of Name, derived on save
	Species      string       `json:"species"`
	Stage        string       `json:"stage"`  // current stage name (one of Stages)
	Stages       []string     `json:"stages"` // ordered sequence, derived from Species
	StartedAt    time.Time    `json:"startedAt"`
	StageStarted time.Time    `json:"stageStarted"`
	Status       GrowStatus   `json:"status"`
	Notes        string       `json:"notes"`
	Setup        GrowingSetup `json:"setup"`
}

// PlantUnit is one tracked plant or a group (tray/bed/batch) within a grow. Its
// cultivar is set per unit so a single grow can carry several cultivars.
type PlantUnit struct {
	ID       string `json:"id"`
	GrowID   string `json:"growId"`
	Label    string `json:"label"`
	Slug     string `json:"slug"` // URL-friendly form of Label, derived on save
	Cultivar string `json:"cultivar"`
	// MediumRecipeID overrides the grow's medium recipe for this plant; empty
	// means it inherits GrowingSetup.MediumRecipeID.
	MediumRecipeID string       `json:"mediumRecipeId,omitempty"`
	Tracking       TrackingMode `json:"tracking"`
	Quantity       int          `json:"quantity"` // >1 for groups; 1 for individuals
	Status         PlantStatus  `json:"status"`
	CreatedAt      time.Time    `json:"createdAt"`
}

// PlantPlacement records that a plant unit occupied an environment for a span of
// time. The current placement has a nil EndedAt; closing it and opening a new
// one records a move.
type PlantPlacement struct {
	ID            string     `json:"id"`
	PlantUnitID   string     `json:"plantUnitId"`
	EnvironmentID string     `json:"environmentId"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`  // nil = current
	Position      string     `json:"position,omitempty"` // optional, e.g. "A1"
}

// PlantPot records that a plant lived in a pot of a given size for a span of
// time, mirroring PlantPlacement: the current pot has a nil EndedAt, and
// repotting closes it and opens a new one — so a plant carries a repot history.
// Size is the volume in Unit ("L" or "gal"); Type is the pot material/kind.
type PlantPot struct {
	ID          string     `json:"id"`
	PlantUnitID string     `json:"plantUnitId"`
	Size        float64    `json:"size"`
	Unit        string     `json:"unit"` // "L" or "gal"
	Type        string     `json:"type,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt,omitempty"` // nil = current
}

// --- Live/view types for the dashboard and environment views ---

// GrowEnvRef names one environment a grow currently occupies.
type GrowEnvRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GrowCultivarRef is the count of active plants of one cultivar within a grow,
// used to render per-cultivar plant thumbnails on the grow card. Cultivar is the
// cultivar name (may be empty for plants with no cultivar set).
type GrowCultivarRef struct {
	Cultivar string `json:"cultivar"`
	Count    int    `json:"count"`
}

// GrowSummary is the compact live view of an environment's control grow.
type GrowSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Species    string `json:"species"`
	Stage      string `json:"stage"`
	StageDays  int    `json:"stageDays"`
	TotalDays  int    `json:"totalDays"`
	PlantCount int    `json:"plantCount"`
}

// GrowView is the dashboard "Active Grows" view: a grow plus derived duration,
// plant count and the environments it currently occupies.
type GrowView struct {
	Grow
	StageDays    int               `json:"stageDays"`
	TotalDays    int               `json:"totalDays"`
	PlantCount   int               `json:"plantCount"`
	Environments []GrowEnvRef      `json:"environments"`
	Cultivars    []GrowCultivarRef `json:"cultivars"`
}

// DaysSince returns whole days between then and now, floored at 0.
func DaysSince(then, now time.Time) int {
	if then.IsZero() {
		return 0
	}
	d := int(now.Sub(then).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}
