// Package property defines the small SVA-style sequence/property language
// used by signalcourt: a parser, a versioned AST and semantic-version
// metadata. The evaluator lives in internal/replay; this package only
// preserves structure so different semantic versions can interpret the same
// stored AST differently.
package property

// SourceRef points back at the exact source text an AST node came from.
type SourceRef struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

// BoolOp is a four-state boolean operator inside a sequence element.
type BoolOp string

const (
	BAnd  BoolOp = "&&"
	BOr   BoolOp = "||"
	BImp  BoolOp = "->"
	BNand BoolOp = "!&" // not used by source; reserved
)

// Expr is a boolean element expression.
type Expr struct {
	Ref   SourceRef `json:"ref"`
	Kind  string    `json:"kind"` // sig,const,not,and,or,impl,eq
	Sig   string    `json:"sig,omitempty"`
	Value string    `json:"value,omitempty"` // 0/1/x/z for eq and const
	Op    BoolOp    `json:"op,omitempty"`
	Left  *Expr     `json:"left,omitempty"`
	Right *Expr     `json:"right,omitempty"`
}

// SeqKind enumerates sequence node kinds.
type SeqKind string

const (
	SElement SeqKind = "element" // bool expr, single cycle
	SConcat  SeqKind = "concat"  // a ##[l:r] b
	SDelay   SeqKind = "delay"   // ##[l:r] suffix (prefix is true)
	SRepeat  SeqKind = "repeat"  // s [* l:r]
	SOr      SeqKind = "or"      // s1 or s2
	SAnd     SeqKind = "and"     // s1 and s2
	SNot     SeqKind = "not"     // not (s) over a bounded window
	SEmpty   SeqKind = "empty"   // [*0]
	SPort    SeqKind = "port"    // parenthesized group
)

// Seq is a sequence AST node.
type Seq struct {
	Ref  SourceRef `json:"ref"`
	Kind SeqKind   `json:"kind"`
	Expr *Expr     `json:"expr,omitempty"`
	// concat/delay
	Left        *Seq `json:"left,omitempty"`
	Right       *Seq `json:"right,omitempty"`
	Lo, Hi      int  `json:"lo,omitempty"`
	HiUnbounded bool `json:"hi_unbounded,omitempty"`
	// repeat
	Base *Seq `json:"base,omitempty"`
}

// PropKind enumerates property node kinds.
type PropKind string

const (
	PSequence   PropKind = "sequence" // strong(s)
	PNot        PropKind = "not"
	PDisable    PropKind = "disable_iff"
	PImpliesOv  PropKind = "implies_ov"  // s |-> p
	PImpliesNOv PropKind = "implies_nov" // s |=> p
)

// Prop is a property AST node.
type Prop struct {
	Ref         SourceRef `json:"ref"`
	Kind        PropKind  `json:"kind"`
	Seq         *Seq      `json:"seq,omitempty"`
	Inner       *Prop     `json:"inner,omitempty"`
	Ante        *Seq      `json:"ante,omitempty"`
	Cons        *Prop     `json:"cons,omitempty"`
	Disable     *Expr     `json:"disable,omitempty"`
	Lo, Hi      int       `json:"lo,omitempty"`
	HiUnbounded bool      `json:"hi_unbounded,omitempty"`
}

// File is a parsed .ssp source.
type File struct {
	Path      string      `json:"path"`
	Version   string      `json:"version"` // declared semantic version
	ClockEdge EdgeSpec    `json:"clock_edge"`
	ResetExpr *Expr       `json:"reset_expr,omitempty"`
	ResetKind string      `json:"reset_kind,omitempty"` // async_low/async_high/sync_low/sync_high
	Props     []NamedProp `json:"props"`
	RawText   string      `json:"-"`
}

// EdgeSpec names the sampling edge, e.g. posedge clk.
type EdgeSpec struct {
	Edge string `json:"edge"` // posedge|negedge|edge
	Clk  string `json:"clk"`
}

// NamedProp binds a property to its declared name and source order.
type NamedProp struct {
	Order int    `json:"order"`
	Name  string `json:"name"`
	Prop  Prop   `json:"prop"`
}
