// Package property defines the small gate-sequence property language:
// AST, stable content identity, lexer and recursive-descent parser.
//
// Grammar:
//
//	module     ::= IDENT
//	clockdecl  ::= "clock" IDENT "@" ("posedge"|"negedge") IDENT
//	resetdecl  ::= "reset" IDENT
//	prop       ::= IDENT ":" property ";"
//	property   ::= ("disable iff" "(" boolexpr ")")? implication
//	implication ::= antecedent ("|->"|"|=>") property | boolproperty
//	boolproperty ::= "strong" sequence | "weak" sequence | sequence
//	sequence   ::= seqterm ("or" seqterm | "and" seqterm | "##["INT":"INT"]" seqterm)*
//	seqterm    ::= boolexpr ("[*" range "]")? | "empty"
//	range      ::= INT | INT ":" INT
//	boolexpr   ::= boolor
//	boolor     ::= booland ("||" booland)*
//	booland    ::= boolnot ("&&" boolnot)*
//	boolnot    ::= "!" boolnot | boolatom
//	boolatom   ::= IDENT | "0" | "1" | "x" | "(" boolexpr ")"
package property

// BOp is a boolean operator.
type BOp string

const (
	BVar   BOp = "var"
	BConst BOp = "const"
	BNot   BOp = "not"
	BAnd   BOp = "and"
	BOr    BOp = "or"
)

// BoolExpr is a boolean expression node.
type BoolExpr struct {
	Op     BOp       `json:"op"`
	Name   string    `json:"name,omitempty"`  // for var
	Const  string    `json:"const,omitempty"` // 0/1/x
	Args   []*BoolExpr `json:"args,omitempty"`
	NodeID string    `json:"node_id"`
}

// SeqKind distinguishes sequence node shapes.
type SeqKind string

const (
	SeqLit   SeqKind = "lit"
	SeqEmpty SeqKind = "empty"
	SeqDelay SeqKind = "delay"
	SeqOr    SeqKind = "or"
	SeqAnd   SeqKind = "and"
)

// Sequence is a sequence expression node.
type Sequence struct {
	Kind    SeqKind     `json:"kind"`
	Cond    *BoolExpr   `json:"cond,omitempty"`
	RepMin  int         `json:"rep_min"`
	RepMax  int         `json:"rep_max"`
	HasRep  bool        `json:"has_rep"`
	DelayLo int         `json:"delay_lo"`
	DelayHi int         `json:"delay_hi"`
	Left    *Sequence   `json:"left,omitempty"`
	Right   *Sequence   `json:"right,omitempty"`
	NodeID  string      `json:"node_id"`
}

// PropKind distinguishes property node shapes.
type PropKind string

const (
	PropSeq     PropKind = "seq"
	PropImp     PropKind = "implies"
	PropDisable PropKind = "disable"
)

// Property is a property node.
type Property struct {
	Kind        PropKind   `json:"kind"`
	Strength    string     `json:"strength,omitempty"` // strong/weak on seq
	Seq         *Sequence  `json:"seq,omitempty"`
	Ante        *Sequence  `json:"ante,omitempty"`
	Cons        *Property  `json:"cons,omitempty"`
	Overlapped  bool       `json:"overlapped"`
	DisableCond *BoolExpr  `json:"disable_cond,omitempty"`
	Inner       *Property  `json:"inner,omitempty"`
	NodeID      string     `json:"node_id"`
}

// Decl is one named property declaration.
type Decl struct {
	Name     string    `json:"name"`
	Property *Property `json:"property"`
	NodeID   string    `json:"node_id"`
}

// Module is a parsed source text.
type Module struct {
	Name       string   `json:"name"`
	ClockEdge  string   `json:"clock_edge"`  // posedge/negedge
	ClockName  string   `json:"clock_name"`
	ResetName  string   `json:"reset_name"`
	Decls      []*Decl  `json:"decls"`
	RawText    string   `json:"raw_text"`
	NodeID     string   `json:"node_id"`
}
