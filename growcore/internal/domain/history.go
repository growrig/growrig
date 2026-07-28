package domain

import "time"

// History is the human-readable story of one collection — a grow, a plant, an
// environment — assembled from every record that touches it: care events,
// photos, stage changes, placements, pots and activity entries.
//
// It is deliberately not the activity log. The log is the raw system-wide
// stream; History is the merged, chronological account of what happened, paged
// server-side so a client never has to hold every source in memory just to show
// the most recent page (which is what merging client-side would require).
//
// Entries carry structured data, never rendered sentences. Wording, ordering of
// clauses and pluralisation belong to the UI: baking English into the API is
// the mistake Activity.Message already makes, which is why an activity entry
// can't be linked to the plant it mentions.

// HistoryKind is the sort of record an entry came from.
type HistoryKind string

const (
	HistoryCare      HistoryKind = "care"
	HistoryPhoto     HistoryKind = "photo"
	HistoryMilestone HistoryKind = "milestone"
	HistoryPlacement HistoryKind = "placement"
	HistoryPot       HistoryKind = "pot"
	HistorySystem    HistoryKind = "system"
)

// ValidHistoryKind reports whether k is a known entry kind.
func ValidHistoryKind(k HistoryKind) bool {
	switch k {
	case HistoryCare, HistoryPhoto, HistoryMilestone, HistoryPlacement, HistoryPot, HistorySystem:
		return true
	}
	return false
}

// HistoryRef is a thing an entry points at, resolved to a display label so the
// client can render it as a link without a second lookup.
type HistoryRef struct {
	Kind  string `json:"kind"` // plant | grow | environment | device
	ID    string `json:"id"`
	Label string `json:"label"`
}

// HistoryEntry is one thing that happened, at one moment.
//
// Exactly one of the payload pointers is set, matching Kind. They carry the
// full source record so the client can render both the summary line and the
// detail view without refetching.
type HistoryEntry struct {
	ID   string      `json:"id"`
	Kind HistoryKind `json:"kind"`
	At   time.Time   `json:"at"`
	// Automation marks routine control-loop chatter (a fan nudged a few
	// percent). Real history, but it buries everything a person did, so clients
	// hide it by default and the query can exclude it outright.
	Automation bool `json:"automation,omitempty"`
	// Refs are the entities this entry involves, for inline links.
	Refs []HistoryRef `json:"refs,omitempty"`

	Care      *CareEvent      `json:"care,omitempty"`
	Photo     *GrowPhoto      `json:"photo,omitempty"`
	Stage     string          `json:"stage,omitempty"`
	Placement *PlantPlacement `json:"placement,omitempty"`
	Pot       *PlantPot       `json:"pot,omitempty"`
	Activity  *Activity       `json:"activity,omitempty"`
}

// HistoryQuery scopes and pages a history request. Exactly one of GrowID,
// PlantID and EnvironmentID identifies the collection.
type HistoryQuery struct {
	GrowID        string
	PlantID       string
	EnvironmentID string
	// Kinds, when non-empty, restricts to those entry kinds.
	Kinds []HistoryKind
	// Automation includes routine control-loop entries; false omits them.
	Automation bool
	Limit      int
	Offset     int
}

// HistoryPage is one page of entries plus the total available, so a client can
// show "N more" without walking to the end.
type HistoryPage struct {
	Items []HistoryEntry `json:"items"`
	Total int            `json:"total"`
}
