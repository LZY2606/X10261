// Package ssp defines the "small sequence/property" assertion language used by
// the digital verification team. It is a deliberately small SVA-like grammar:
//
//	property pname ;
//	  [ disable iff ( signal ) ]
//	  sequence (|-> | =>) sequence ;
//	endproperty
//
//	sequence ::= bexpr
//	         |   sequence ##[lo:hi] sequence
//	         |   sequence ## sequence
//	         |   sequence or sequence
//	         |   ( sequence )
//
//	bexpr    ::= signal | ! bexpr | bexpr && bexpr | bexpr || bexpr
//
// Every AST node gets a content-derived ID, so the same text under the same
// semantic version always yields the same automaton identities.
package ssp

// NodeKind enumerates AST node types.
type NodeKind string

const (
	NBool     NodeKind = "bool"
	NSeqDelay NodeKind = "seq_delay"
	NSeqOr    NodeKind = "seq_or"
	NProp     NodeKind = "property"
)

// BoolOp enumerates boolean operators on four-state signals.
type BoolOp string

const (
	BVar      BoolOp = "var"
	BNot      BoolOp = "not"
	BAnd      BoolOp = "and"
	BOr       BoolOp = "or"
)

// BoolNode is a four-state boolean expression node.
type BoolNode struct {
	ID     string    `json:"id"`
	Op     BoolOp    `json:"op"`
	Name   string    `json:"name,omitempty"`   // for BVar
	Left   *BoolNode `json:"left,omitempty"`
	Right  *BoolNode `json:"right,omitempty"`
	Span   [2]int    `json:"span"`
}

// SeqNode is a sequence expression node. Delay ranges are [Lo:Hi] tick delays
// placed BETWEEN left and right (SVA ## semantics). Lo may be 0; under
// ssp-1.0.0 a zero-width sequence used directly as a property is rejected as a
// compile verdict, while ssp-1.1.0 admits it and the engine reports vacuous
// matches explicitly.
type SeqNode struct {
	ID    string   `json:"id"`
	Kind  NodeKind `json:"kind"`
	Bool  *BoolNode `json:"bool,omitempty"`
	Left  *SeqNode  `json:"left,omitempty"`
	Right *SeqNode  `json:"right,omitempty"`
	Lo    int       `json:"lo,omitempty"`
	Hi    int       `json:"hi,omitempty"`
	Span  [2]int    `json:"span"`
}

// Property is a named assertion with optional disable condition.
type Property struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Disable   *BoolNode `json:"disable,omitempty"`
	Ante      *SeqNode  `json:"antecedent"`
	Cons      *SeqNode  `json:"consequent"`
	Overlap   bool      `json:"overlap"` // |-> true, |=> false
	SourceID  string    `json:"source_id"`
	Semver    string    `json:"semver"`  // recorded at parse time
	Span      [2]int    `json:"span"`
	// Bare is true when the property body is a bare sequence (no implication).
	Bare      bool      `json:"bare"`
	BareSeq   *SeqNode  `json:"bare_seq,omitempty"`
}

// CanMatchZero reports whether the sequence admits a zero-length match. It is
// purely structural and version-independent: only a single delay atom
// ##[0:0] glued between two booleans can collapse to empty.
func (n *SeqNode) CanMatchZero() bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case NBool:
		return false
	case NSeqOr:
		return n.Left.CanMatchZero() || n.Right.CanMatchZero()
	case NSeqDelay:
		return n.Lo == 0 && n.Hi == 0 && n.Left.CanMatchZeroish() && n.Right.CanMatchZeroish()
	}
	return false
}

// CanMatchZeroish treats a bool guarded by ##[0:0] as contributing an empty
// branch at the SVA syntactic level (antecedent `(a ##[0:0] b)`). The
// non-vacuity of a *bool consumed at the same tick* is resolved by the engine;
// version divergence is about whether the empty alternative is legal.
func (n *SeqNode) CanMatchZeroish() bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case NBool:
		return true
	case NSeqOr:
		return n.Left.CanMatchZeroish() || n.Right.CanMatchZeroish()
	case NSeqDelay:
		return n.Lo == 0 && n.Hi == 0 && n.Left.CanMatchZeroish() && n.Right.CanMatchZeroish()
	}
	return false
}
