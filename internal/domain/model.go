package domain

// Timescale keeps the raw VCD timescale representation plus its integer
// timebase in femtoseconds. When the unit/number cannot be converted,
// BaseFS is 0 and Convertible is false; the raw text is never discarded.
type Timescale struct {
	Raw         string `json:"raw"`
	Number      int    `json:"number"`
	Unit        string `json:"unit"`
	BaseFS      int64  `json:"base_fs"`
	Convertible bool   `json:"convertible"`
}

// Change is one raw value-change record in source order.
type Change struct {
	ID       string  `json:"id"`
	Code     string  `json:"code"`
	SignalID string  `json:"signal_id"`
	Index    int     `json:"index"`
	AtTick   int64   `json:"at_tick"`
	RawValue string  `json:"raw_value"`
	Width    int     `json:"width"`
	Bits     []Logic `json:"bits,omitempty"`
	NonCanon bool    `json:"non_canonical,omitempty"`
}

// Signal is one wire/reg identifier from the VCD header.
type Signal struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Width   int    `json:"width"`
	RawName string `json:"raw_name"`
}

// ClockDomain declares a clock edge source and optional reset signal.
type ClockDomain struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ClockSignal string `json:"clock_signal"`
	Edge        string `json:"edge"` // posedge | negedge
	ResetSignal string `json:"reset_signal,omitempty"`
	ResetLevel  Logic  `json:"reset_level"`
	RawText     string `json:"raw_text"`
}

// AST node kinds for the small sequence/property language.
const (
	ASTBool      = "bool"
	ASTNot       = "not"
	ASTAnd       = "and"
	ASTOr        = "or"
	ASTSeq       = "seq"      // a ##n b
	ASTOverlap   = "|->"      // overlapped implication
	ASTNoOverlap = "|=>"      // non-overlapped implication
	ASTProp      = "property" // name, disable iff, clock, expr
)

type Node struct {
	Kind     string  `json:"kind"`
	Name     string  `json:"name,omitempty"`  // signal name for bool nodes
	Cycle    int     `json:"cycle,omitempty"` // delay lower bound
	Hi       int     `json:"hi,omitempty"`    // delay upper bound; -1 unbounded
	Empty    bool    `json:"empty,omitempty"` // empty-match-capable marker
	Children []*Node `json:"children,omitempty"`
}

// Property keeps the raw source text, the parsed AST and the semantic
// version under which it is interpreted.
type Property struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RawText       string `json:"raw_text"`
	AST           *Node  `json:"ast"`
	ClockDomain   string `json:"clock_domain"`
	DisableSignal string `json:"disable_signal,omitempty"`
	DisableLevel  Logic  `json:"disable_level"`
	SemVer        string `json:"sem_ver"`
}

// Sample marks one sampling point in fixed (preponed-region) semantics.
type Sample struct {
	Index      int    `json:"index"`
	AtTick     int64  `json:"at_tick"`
	AtFS       int64  `json:"at_fs"`
	Region     string `json:"region"` // always "preponed"
	Edge       string `json:"edge"`
	ClockValue Logic  `json:"clock_value"`
}

// ThreadState is the state of one live automaton thread.
type ThreadState struct {
	ID        string `json:"id"`
	BranchOf  string `json:"branch_of,omitempty"`
	Property  string `json:"property"`
	StartedAt int    `json:"started_at"`
	Sample    int    `json:"sample"`
	NodePath  string `json:"node_path"`
	Status    string `json:"status"` // active | matched | failed | disabled | empty
	Result    Tri    `json:"result"`
	Note      string `json:"note,omitempty"`
}

// Candidate is one legal verdict candidate kept for multi-candidate review.
type Candidate struct {
	ID         string `json:"id"`
	Result     Tri    `json:"result"`
	Accepted   bool   `json:"accepted"`
	Reason     string `json:"reason"`
	BasisEvent string `json:"basis_event"`
}

// Verdict is one property replay outcome on a branch.
type Verdict struct {
	ID         string        `json:"id"`
	PropertyID string        `json:"property_id"`
	BranchID   string        `json:"branch_id"`
	Result     Tri           `json:"result"`
	EmptyMatch bool          `json:"empty_match"`
	Candidates []Candidate   `json:"candidates"`
	Threads    []ThreadState `json:"threads,omitempty"`
	SemVer     string        `json:"sem_ver"`
}

// Counterex is a reduced failure trace. OriginSample never moves; only
// intervals unrelated to the failure may be removed.
type Counterex struct {
	ID           string     `json:"id"`
	CaseID       string     `json:"case_id,omitempty"`
	BranchID     string     `json:"branch_id,omitempty"`
	VerdictID    string     `json:"verdict_id"`
	OriginSample int        `json:"origin_sample"`
	OriginTick   int64      `json:"origin_tick"`
	Kept         [][2]int   `json:"kept_intervals"`
	Removed      [][2]int   `json:"removed_intervals"`
	Minimal      bool       `json:"minimal"`
	Alternatives [][][2]int `json:"alternatives,omitempty"`
}

// EventKind enumerates the machine-readable provenance event types.
type EventKind string

const (
	EvImport        EventKind = "import"
	EvRollback      EventKind = "import_rollback"
	EvReinterpret   EventKind = "reinterpret_branch"
	EvParamRevise   EventKind = "param_revision"
	EvSourceChange  EventKind = "source_change"
	EvReplay        EventKind = "replay"
	EvAdjudicate    EventKind = "adjudicate"
	EvConflict      EventKind = "version_conflict"
	EvReduction     EventKind = "counterex_reduction"
	EvInvalidate    EventKind = "dependency_invalidation"
	EvXZPropagate   EventKind = "xz_propagation"
	EvEmptyMatch    EventKind = "empty_match"
	EvResetBoundary EventKind = "reset_boundary"
	EvSemverDiff    EventKind = "semver_difference"
	EvTimescaleDiff EventKind = "timescale_difference"
	EvMultithread   EventKind = "multithread_coexist"
	EvDiagnostic    EventKind = "unconvertible_value"
	EvUndo          EventKind = "adjudication_undo"
)

func (k EventKind) String() string { return string(k) }

// Event is a machine-readable provenance/judgement record.
type Event struct {
	Seq         int64             `json:"seq"`
	ID          string            `json:"id"`
	Kind        EventKind         `json:"kind"`
	BranchID    string            `json:"branch_id"`
	ParentEvent string            `json:"parent_event,omitempty"`
	Refs        []string          `json:"refs"`
	DependsOn   []string          `json:"depends_on"`
	Detail      map[string]string `json:"detail"`
	ActorOrigin string            `json:"actor_origin,omitempty"`
	Version     int64             `json:"version"`
}

// Adjudication is a human decision that never rewrites source data.
type Adjudication struct {
	ID         string `json:"id"`
	BranchID   string `json:"branch_id"`
	TargetID   string `json:"target_id"` // candidate or verdict id
	Decision   string `json:"decision"`  // accept | reject
	Reason     string `json:"reason"`
	Undone     bool   `json:"undone"`
	Supersedes string `json:"supersedes,omitempty"`
	EventID    string `json:"event_id"`
	Version    int64  `json:"version"`
}

// Branch groups one (possibly re-)interpretation of the same source case.
type Branch struct {
	ID        string `json:"id"`
	CaseID    string `json:"case_id"`
	Name      string `json:"name"`
	ParentID  string `json:"parent_id,omitempty"`
	CreatedBy string `json:"created_by"`
	Version   int64  `json:"version"`
	HeadEvent string `json:"head_event"`
	Current   bool   `json:"current"`
}

// Diagnostic is attached to a value that could not be normalized.
type Diagnostic struct {
	ID       string `json:"id"`
	RefID    string `json:"ref_id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	RawValue string `json:"raw_value"`
}
