// Package prop defines the small gate/sequence property language, its lexer,
// parser and canonical serialization.
//
// Language (SVA-inspired), boolean precedence high -> low:
//
//	spec      := [disable iff "(" expr ")"] property ;
//	property  := sequence ("|->" | "|=>") sequence
//	          |  sequence
//	sequence  := seq ("or" seq)*
//	seq       := factor ("##" cycle factor)*
//	factor    := expr
//	          |  "(" sequence ")"
//	          |  factor "[" "*" cycles "]"
//	expr      := or
//	or        := and ("||" and)*
//	and       := not ("&&" not)*
//	not       := "!" not | atom
//	atom      := ident ["[" INT "]"] | "(" expr ")"
//
// An empty antecedent (a sequence that can match zero cycles) combined with an
// implication is what drives vacuous/non-vacuous semantic differences.
package prop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Pos is a 1-based source position in the property text.
type Pos struct {
	Offset int `json:"o"`
	Line   int `json:"l"`
	Col    int `json:"c"`
}

// BoolOp enumerates boolean connectives.
type BoolOp string

const (
	BVar  BoolOp = "var"
	BNot  BoolOp = "not"
	BAnd  BoolOp = "and"
	BOr   BoolOp = "or"
)

// Expr is a boolean expression over sampled signal bits.
type Expr struct {
	Op       BoolOp `json:"op"`
	Name     string `json:"name,omitempty"`
	Bit      int    `json:"bit,omitempty"`
	Children []*Expr `json:"children,omitempty"`
	Pos      Pos    `json:"pos"`
}

// SeqOp enumerates sequence constructors.
type SeqOp string

const (
	SSig   SeqOp = "sig"   // boolean factor (one-cycle match)
	SGroup SeqOp = "group" // parenthesized sequence
	SConcat SeqOp = "concat"
	SOr    SeqOp = "or"
	SRepeat SeqOp = "repeat"
)

// Seq is a sequence expression. Delay/RepeatCount attach to concat/repeat.
type Seq struct {
	Op          SeqOp `json:"op"`
	Expr        *Expr `json:"expr,omitempty"`
	Children    []*Seq `json:"children,omitempty"`
	Delay       int   `json:"delay,omitempty"`       // for concat children[1]
	RepeatCount int   `json:"repeat_count,omitempty"`
	Pos         Pos   `json:"pos"`
}

// PropKind is implication or plain sequence.
type PropKind string

const (
	PPlain     PropKind = "plain"
	POverlap   PropKind = "overlap_implies"   // |->
	PNonOverlap PropKind = "nonoverlap_implies" // |=>
)

// Property is the assertion body.
type Property struct {
	Kind     PropKind `json:"kind"`
	Ante     *Seq     `json:"ante,omitempty"`
	Cons     *Seq     `json:"cons"`
	Disable  *Expr    `json:"disable_iff,omitempty"`
	Pos      Pos      `json:"pos"`
}

// Spec is a named assertion with its exact source text.
type Spec struct {
	Name     string    `json:"name"`
	Text     string    `json:"text"`
	Disable  *Expr     `json:"disable_iff,omitempty"`
	Property *Property `json:"property"`
	Pos      Pos       `json:"pos"`
}

// CanonicalJSON renders a value with deterministic key order (Go struct order)
// so that identical ASTs hash identically.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ASTHash is a stable content hash of the property AST (without its name/text,
// which are provenance, not semantics).
func ASTHash(p *Property) string {
	sum := sha256.Sum256(CanonicalJSON(p))
	return "ast_" + hex.EncodeToString(sum[:])[:16]
}

// Signals collects every referenced identifier (with its sampled bit).
func (sp *Spec) Signals() []SignalRef {
	seen := map[SignalRef]bool{}
	var out []SignalRef
	var walkE func(e *Expr)
	walkE = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Op == BVar {
			r := SignalRef{Name: e.Name, Bit: e.Bit}
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
		for _, c := range e.Children {
			walkE(c)
		}
	}
	var walkS func(s *Seq)
	walkS = func(s *Seq) {
		if s == nil {
			return
		}
		walkE(s.Expr)
		for _, c := range s.Children {
			walkS(c)
		}
	}
	if sp.Disable != nil {
		walkE(sp.Disable)
	}
	if sp.Property != nil {
		walkS(sp.Property.Ante)
		walkS(sp.Property.Cons)
	}
	return out
}

// SignalRef names one sampled bit of a signal.
type SignalRef struct {
	Name string `json:"name"`
	Bit  int    `json:"bit"`
}
