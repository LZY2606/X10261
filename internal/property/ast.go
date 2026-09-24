// Package property defines the small gate/sequence property language used
// by the verification team. An AST is derived material: it is produced from
// an immutable source text under an explicit semantic version, and both the
// raw source and the derived AST are retained.
package property

// ExprKind enumerates Boolean/gate expression nodes.
type ExprKind string

const (
	ExSignal ExprKind = "sig"
	ExConst  ExprKind = "const"
	ExNot    ExprKind = "not"
	ExAnd    ExprKind = "and"
	ExOr     ExprKind = "or"
	ExXor    ExprKind = "xor"
)

// Expr is a Boolean expression over sampled signals.
type Expr struct {
	Kind     ExprKind `json:"kind"`
	Name     string   `json:"name,omitempty"`
	Const    bool     `json:"const,omitempty"`
	Children []*Expr  `json:"children,omitempty"`
}

// SeqKind enumerates sequence operators.
type SeqKind string

const (
	SeqAtom  SeqKind = "atom"
	SeqDelay SeqKind = "delay" // ##[lo:hi] seq ; ##N is ##[N:N]
	SeqOr    SeqKind = "or"
)

// Seq is a sequence term.
type Seq struct {
	Kind  SeqKind `json:"kind"`
	Atom  *Expr   `json:"atom,omitempty"`
	Lo    int     `json:"lo,omitempty"`
	Hi    int     `json:"hi,omitempty"`
	Left  *Seq    `json:"left,omitempty"`
	Right *Seq    `json:"right,omitempty"`
}

// PropKind enumerates property operators.
type PropKind string

const (
	PropSeq        PropKind = "seq"         // sequence must match
	PropNot        PropKind = "not"
	PropAnd        PropKind = "and"
	PropOr         PropKind = "or"
	PropImply      PropKind = "imply"       // |-> overlapped
	PropImplyNo    PropKind = "imply_nonov" // |=> non-overlapped
	PropDisableIff PropKind = "disable_iff"
)

// Prop is the property AST root.
type Prop struct {
	Kind      PropKind `json:"kind"`
	Seq       *Seq     `json:"seq,omitempty"`
	Overlap   bool     `json:"overlap,omitempty"`
	Cond      *Expr    `json:"cond,omitempty"`
	Children  []*Prop  `json:"children,omitempty"`
	Ante      *Seq     `json:"ante,omitempty"`
	Cons      *Prop    `json:"cons,omitempty"`
	SourceRef [2]int   `json:"source_ref"` // byte offsets into the raw source
}

// Document is a parsed property file: raw source plus derived root and
// the semantic version declared for the parse (recorded, not enforced).
type Document struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Root       *Prop  `json:"root"`
	ParseRules string `json:"parse_rules"` // e.g. "property-grammar@1"
}
