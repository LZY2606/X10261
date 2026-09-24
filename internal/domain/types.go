package domain

type RawChange struct {
	ID         string `json:"id"`
	SignalID   string `json:"signal_id"`
	Order      int    `json:"order"`
	RawTime    int64  `json:"raw_time"`
	RawValue   string `json:"raw_value"`
	TimePS     *int64 `json:"time_ps,omitempty"`
	NormValue  string `json:"normalized_value,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type Signal struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Width    int    `json:"width"`
	VCDID    string `json:"vcd_id"`
	Material string `json:"material_id"`
	Kind     string `json:"kind"`
}

type VCDData struct {
	ID             string      `json:"id"`
	FileName       string      `json:"file_name"`
	RawBytes       []byte      `json:"-"`
	RawSHA256      string      `json:"raw_sha256"`
	TimescaleText  string      `json:"timescale_text"`
	TimescalePS    int64       `json:"timescale_ps"`
	TimeUnit       string      `json:"time_unit"`
	CoordinateBasis string      `json:"coordinate_basis"`
	Signals        []Signal    `json:"signals"`
	Changes        []RawChange `json:"changes"`
	Diagnostics    []string    `json:"diagnostics"`
}

type ClockDomain struct {
	Name        string `json:"name"`
	ClockSignal string `json:"clock_signal"`
	MaterialID  string `json:"material_id"`
	Edge        string `json:"edge"`
	TimeUnit    string `json:"time_unit"`
}

type ClockEdge struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	Cycle      int    `json:"cycle"`
	TimePS     int64  `json:"time_ps"`
	RawTime    int64  `json:"raw_time"`
	MaterialID string `json:"material_id"`
	RegionStartPS int64 `json:"region_start_ps"`
}

type ResetInterval struct {
	ID         string `json:"id"`
	Signal     string `json:"signal"`
	StartPS    int64  `json:"start_ps"`
	EndPS      int64  `json:"end_ps"`
	MaterialID string `json:"material_id"`
	Kind       string `json:"kind"`
}

type PropertyFile struct {
	ID              string     `json:"id"`
	FileName        string     `json:"file_name"`
	RawBytes        []byte     `json:"-"`
	RawSHA256       string     `json:"raw_sha256"`
	SemanticVersion string     `json:"semantic_version"`
	Properties      []Property `json:"properties"`
}

type Property struct {
	Name            string   `json:"name"`
	Domain          string   `json:"domain"`
	SourceText      string   `json:"source_text"`
	AST             string   `json:"ast"`
	ASTHash         string   `json:"ast_hash"`
	Reset           BoolExpr `json:"reset,omitempty"`
	Antecedent      SeqExpr  `json:"antecedent"`
	Consequent      *SeqExpr `json:"consequent,omitempty"`
	Overlapped      bool     `json:"overlapped"`
	SemanticVersion string   `json:"semantic_version"`
}

type BoolExpr interface{ boolNode() }
type SeqExpr interface{ seqNode() }

type BoolSignal struct{ Name string }
type BoolLiteral struct{ Value string }
type BoolNot struct{ Inner BoolExpr }
type BoolAnd struct{ Items []BoolExpr }
type BoolOr struct{ Items []BoolExpr }
type BoolEq struct{ Left, Right BoolExpr }

func (BoolSignal) boolNode()  {}
func (BoolLiteral) boolNode() {}
func (BoolNot) boolNode()      {}
func (BoolAnd) boolNode()      {}
func (BoolOr) boolNode()       {}
func (BoolEq) boolNode()       {}

type SeqBool struct{ Expr BoolExpr }
type SeqDelay struct {
	Left  SeqExpr
	Right SeqExpr
	Min   int
	Max   int
	MaxParameter string `json:"max_parameter,omitempty"`
}

func (SeqBool) boolNode()  {}
func (SeqBool) seqNode()   {}
func (SeqDelay) seqNode()  {}

type Verdict string

const (
	VerdictPass          Verdict = "pass"
	VerdictFail          Verdict = "fail"
	VerdictUnknown       Verdict = "unknown"
	VerdictDisabled      Verdict = "disabled"
	VerdictVacuousPass   Verdict = "vacuous_pass"
)

type Trilean string

const (
	TriTrue    Trilean = "true"
	TriFalse   Trilean = "false"
	TriUnknown Trilean = "unknown"
)

type Candidate struct {
	ID             string   `json:"id"`
	Property       string   `json:"property"`
	AnchorCycle    int      `json:"anchor_cycle"`
	AnchorTimePS   int64    `json:"anchor_time_ps"`
	Verdict        Verdict  `json:"verdict"`
	Vacuity        Trilean  `json:"vacuity"`
	StartCycle     int      `json:"start_cycle"`
	EndCycle       int      `json:"end_cycle"`
	StartPS        int64    `json:"start_ps"`
	EndPS          int64    `json:"end_ps"`
	ThreadTrace    []Step   `json:"thread_trace"`
	RuleEvents     []RuleEvent `json:"rule_events"`
	ReducedWindows []Window `json:"reduced_windows,omitempty"`
	RemovedWindows []Window `json:"removed_windows,omitempty"`
	Basis          []string `json:"basis"`
	Decision       string   `json:"decision,omitempty"`
}

type Step struct {
	ID             string `json:"id"`
	ThreadID       string `json:"thread_id"`
	Cycle          int    `json:"cycle"`
	TimePS         int64  `json:"time_ps"`
	RegionStartPS  int64  `json:"region_start_ps"`
	Domain         string `json:"domain"`
	AutomatonState string `json:"automaton_state"`
	Symbol         string `json:"symbol"`
	SampledReset   string `json:"sampled_reset"`
}

type Window struct {
	ID      string `json:"id"`
	StartPS int64  `json:"start_ps"`
	EndPS   int64  `json:"end_ps"`
	Reason  string `json:"reason"`
}

type RuleEvent struct {
	ID       string            `json:"id"`
	Rule     string            `json:"rule"`
	Outcome  Trilean           `json:"outcome"`
	Cycle    int               `json:"cycle"`
	TimePS   int64             `json:"time_ps"`
	Evidence map[string]string `json:"evidence"`
}

type ReplayInput struct {
	Branch          string
	SemanticVersion string
	Parameters      map[string]int
	VCDs            map[string]*VCDData
	Domains         []ClockDomain
	Properties      []Property
}

type ReplayResult struct {
	ID              string                 `json:"id"`
	Branch          string                 `json:"branch"`
	ParentResultID  string                 `json:"parent_result_id,omitempty"`
	SemanticVersion string                 `json:"semantic_version"`
	TimeOriginPS    int64                  `json:"time_origin_ps"`
	Parameters      map[string]int         `json:"parameters"`
	Edges           []ClockEdge            `json:"edges"`
	ResetIntervals  []ResetInterval        `json:"reset_intervals"`
	Candidates      []Candidate            `json:"candidates"`
	RuleEvents      []RuleEvent            `json:"rule_events"`
	Dependencies    []Dependency           `json:"dependencies"`
	Diagnostics     []string               `json:"diagnostics"`
	Changed         []string               `json:"changed_properties"`
	Invalidated     []string               `json:"invalidated_properties"`
	Metadata        map[string]interface{} `json:"metadata"`
}

type Dependency struct {
	Property   string   `json:"property"`
	Materials  []string `json:"materials"`
	ASTHash    string   `json:"ast_hash"`
	Semantics  string   `json:"semantics"`
	Parameters []string `json:"parameters"`
	Domain     string   `json:"domain"`
}
