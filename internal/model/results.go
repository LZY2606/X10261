package model

import "time"

// Branch is an interpretation lineage. Re-interpreting the same material
// always creates a new branch; the old branch, its decisions and the raw
// bytes it references stay readable forever.
type Branch struct {
	ID              ID
	ProjectID       ID
	ParentID        ID // empty for the baseline branch
	Name            string
	Reason          string
	CreatedAt       JsonTime
	AnchoredEventID ID // event the branch was forked from (history anchor)
}

// Run is one fixed-sampling replay of a property on a branch.
type Run struct {
	ID            ID
	BranchID      ID
	PropertyID    ID
	MaterialID    ID // VCD material replayed
	SemVer        string
	Timescale     string // native timescale used for coordinates
	Verdict       Verdict
	StartedAt     JsonTime
	Stale         bool // invalidated by a dependency change; never silently replaced
	StaleReason   string
	AncestorRunID ID // prior run this run supersedes on its branch (optional)
}

// Thread is one concurrently alive automaton instance created by an
// overlapping implication (each antecedent match spawns a consequent).
type Thread struct {
	ID        ID
	RunID     ID
	Index     int
	BirthStep int    // step at which the antecedent completed
	DeathStep int    // -1 while alive
	Label     string // human-readable, e.g. "T2@edge#7"
}

// Step is one per-edge evaluation record. Every displayed value carries
// the region it was sampled in and the inputs that produced it.
type Step struct {
	ID          ID
	RunID       ID
	ThreadID    ID
	EdgeSeq     int
	Tick        int64
	Picoseconds int64
	Region      Region
	Expr        string // expression evaluated at this step
	Value       string // four-state token or "T/F/U" verdict token
	Verdict     Verdict
	Note        string
}

// Counterexample is a reduced failing trajectory. Reduction removes only
// intervals proven irrelevant to the failure and never moves the time
// origin: all kept coordinates are native ticks of the original trace.
type Counterexample struct {
	ID          ID
	RunID       ID
	FailureStep int
	OriginTick  int64 // always equal to the earliest kept original tick
	EndTick     int64
	Timescale   string
	Verdict     Verdict
	Selected    bool // candidate chosen by adjudication (multiple can coexist)
	Cardinality int  // number of kept change groups
	Minimal     bool // no further interval can be removed without losing failure
	Unique      bool // whether reduction yielded a single legal minimum
	Diagnostics string
}

// KeptInterval is a surviving [Start,End] tick window inside a
// counterexample, referencing the original change groups by Seq range.
type KeptInterval struct {
	ID               ID
	CounterexampleID ID
	StartTick        int64
	EndTick          int64
	StartSeq         int64
	EndSeq           int64
	Removed          bool // true for provenance-recorded removed sibling windows
	Reason           string
}

// Decision is an adjudication (accept/reject/parameter-change). Decisions
// are reversible: reviving restores the prior branch state.
type Decision struct {
	ID               ID
	BranchID         ID
	RunID            ID
	CounterexampleID ID
	Kind             string // "accept_fail" | "reject_fail" | "param_change" | "revoke"
	Outcome          Verdict
	Reason           string
	BaseVersion      int64 // optimistic concurrency base
	CreatedAt        JsonTime
	RevokedAt        JsonTime
	Active           bool
}

// Event is a machine-readable append-only provenance record. Events are
// the audit backbone: reverting the view to an old event restores the
// dependency versions and conclusions visible then.
type Event struct {
	ID        ID
	ProjectID ID
	BranchID  ID
	Seq       int64
	Type      string
	Payload   string // JSON payload (see engine event kinds)
	CreatedAt JsonTime
}

// JsonTime is a time value serialized without an external dependency.
type JsonTime = time.Time

// Dependency rows link a derived object to the exact source/rule versions
// that produced it, enabling scoped invalidation instead of global
// replacement.
type Dependency struct {
	ID            ID
	DerivedType   string // "run" | "counterexample"
	DerivedID     ID
	SourceType    string // "material" | "property" | "semver" | "decision"
	SourceID      ID
	SourceVersion int64
}
