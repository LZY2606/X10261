package model

// Sample is the fixed-sampling readback of one signal at one clock tick.
type Sample struct {
	ID       string `json:"id"`
	Tick     int    `json:"tick"`
	SignalID string `json:"signal_id"`
	Quad     Quad   `json:"quad"`
	RawValue string `json:"raw_value"`
	Region   string `json:"region"` // "post_edge" fixed sampling region label
	EdgeID   string `json:"edge_id"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Thread is one concurrent evaluation attempt ("sequence thread") of a
// property started at a tick. Multiple threads coexist and are tracked
// independently with stable ids.
type Thread struct {
	ID         string    `json:"id"`
	PropertyID string    `json:"property_id"`
	StartTick  int       `json:"start_tick"`
	Alive      bool      `json:"alive"`
	Tri        Tri       `json:"tri"`
	Tag        VerdictTag `json:"tag"`
	Verdicts   []NodeVerdict `json:"verdicts"`
	PathTicks  []int     `json:"path_ticks"` // ticks this thread matched across
	Note       string    `json:"note,omitempty"`
}

// Attempt is a per-tick launch record for a property, with its spawned threads.
type Attempt struct {
	ID         string   `json:"id"`
	PropertyID string   `json:"property_id"`
	Tick       int      `json:"tick"`
	ThreadIDs  []string `json:"thread_ids"`
}

// Candidate is one legally distinct reduced counterexample. When the minimum
// is not unique, ALL legal candidates are stored and adjudicated explicitly.
type Candidate struct {
	ID         string `json:"id"`
	PropertyID string `json:"property_id"`
	ThreadID   string `json:"thread_id"`
	OriginTick int    `json:"origin_tick"` // time origin, immutable
	FailTick   int    `json:"fail_tick"`
	KeptTicks  []int  `json:"kept_ticks"` // original tick indices retained
	RemovedTicks []int `json:"removed_ticks"` // failure-irrelevant only
	Reason     string `json:"reason"`
	Preferred  bool   `json:"preferred"`
}

// Replay bundles the derived interpretation for one branch: sampled regions,
// edges used, attempts/threads, per-node tri verdicts and reduction candidates.
type Replay struct {
	ID          string                `json:"id"`
	BranchID    string                `json:"branch_id"`
	EventID     string                `json:"event_id"`
	SemVer      string                `json:"semver"`
	DomainID    string                `json:"domain_id"`
	Samples     []Sample              `json:"samples"`
	Attempts    []Attempt             `json:"attempts"`
	Threads     []Thread              `json:"threads"`
	Candidates  []Candidate           `json:"candidates"`
	Deps        []DepEdge             `json:"deps"`
	Diagnostics []Diagnostic          `json:"diagnostics"`
}

// Outcome is the adopted/non-adopted adjudication of a candidate or property.
type Outcome string

const (
	OutPending  Outcome = "pending"
	OutAccepted Outcome = "accepted"
	OutRejected Outcome = "rejected"
)

// Decision is a reversible human adjudication recorded as a ledger event.
type Decision struct {
	ID         string  `json:"id"`
	BranchID   string  `json:"branch_id"`
	EventID    string  `json:"event_id"`
	TargetID   string  `json:"target_id"` // candidate or property id
	Outcome    Outcome `json:"outcome"`
	Rationale  string  `json:"rationale"`
	Reviewer   string  `json:"reviewer"`
	Supersedes string  `json:"supersedes,omitempty"` // previous decision id
	Reverted   bool    `json:"reverted,omitempty"`
}
