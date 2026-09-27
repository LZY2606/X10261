package model

import (
	"encoding/json"
	"time"
)

// EventKind enumerates the machine-readable ledger events.
type EventKind string

const (
	EImport      EventKind = "source_imported"
	EReplay      EventKind = "replay_recorded"
	EParamRevise EventKind = "parameter_revised"
	ESourcePatch EventKind = "source_patched"
	EDecision    EventKind = "decision_recorded"
)

// Branch is an immutable interpretation line. Re-interpretation creates a
// new branch; old branches, decisions and referenced raw bytes stay readable.
type Branch struct {
	ID          string `json:"id"`
	CaseID      string `json:"case_id"`
	ParentID    string `json:"parent_id,omitempty"`
	Label       string `json:"label"`
	RootEventID string `json:"root_event_id"`
	HeadEventID string `json:"head_event_id"`
	Basis       string `json:"basis"` // human description of what changed
}

// Event is one append-only ledger entry, content addressed for stable IDs.
type Event struct {
	ID        string          `json:"id"`
	CaseID    string          `json:"case_id"`
	BranchID  string          `json:"branch_id"`
	Seq       int             `json:"seq"`
	Kind      EventKind       `json:"kind"`
	ParentID  string          `json:"parent_id,omitempty"`
	Timestamp time.Time       `json:"ts"`
	Actor     string          `json:"actor"` // "engine" | "reviewer:<id>"
	Summary   string          `json:"summary"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// DepEdge marks that an output depends on a parameter/source reference.
type DepEdge struct {
	FromResult string `json:"from_result"` // replay/candidate id
	DepKind    string `json:"dep_kind"`    // "semver"|"source"|"prop"|"clock"
	DepRef     string `json:"dep_ref"`     // versioned reference id
}

// Diagnostic attaches an unconvertible/unusable condition to its original item.
type Diagnostic struct {
	ID       string `json:"id"`
	Where    string `json:"where"`    // stable id of the original item
	Severity string `json:"severity"` // "warn"|"error"
	Code     string `json:"code"`
	Message  string `json:"message"`
	KeptRaw  string `json:"kept_raw"` // value left in place, never dropped
}
