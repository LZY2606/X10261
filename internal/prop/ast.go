// Package prop defines the small sequence property language (AST,
// parser). The AST is preserved verbatim as part of the archive
// baseline together with the semantics version it was parsed under.
package prop

import (
	"fmt"
	"sort"
	"strings"

	"signalcourt/internal/logic4"
)

// BoolExpr is a boolean expression over sampled signal values.
type BoolExpr interface{ boolExpr() }

// Cmp compares a signal against a four-state literal.
// Op "==" is logical equality (X/Z propagate to unknown);
// Op "===" is case equality (exact match, never unknown).
type Cmp struct {
	Sig string
	Op  string
	Val logic4.V
}

func (Cmp) boolExpr() {}

// Not negates a boolean expression (three-state).
type Not struct{ E BoolExpr }

func (Not) boolExpr() {}

// And conjoins two boolean expressions (three-state).
type And struct{ L, R BoolExpr }

func (And) boolExpr() {}

// Node is a sequence-expression AST node.
type Node interface{ node() }

// Atom matches one sample where Expr holds.
type Atom struct{ Expr BoolExpr }

func (Atom) node() {}

// Empty matches zero samples (arises from [*0]).
type Empty struct{}

func (Empty) node() {}

// Delay between concatenated subsequences: ##Min or ##[Min:Max].
type Delay struct{ Min, Max int }

// Concat is Left ##Delay Right.
type Concat struct {
	Left  Node
	Delay Delay
	Right Node
}

func (Concat) node() {}

// Or matches either alternative (forks threads).
type Or struct{ L, R Node }

func (Or) node() {}

// Repeat is Sub [*Count]; Count 0 yields an empty match.
type Repeat struct {
	Sub   Node
	Count int
}

func (Repeat) node() {}

// Impl is Ant |-> Cons (Overlapped) or Ant |=> Cons (non-overlapped).
type Impl struct {
	Ant        Node
	Cons       Node
	Overlapped bool
}

func (Impl) node() {}

// Property is a named assertion with clock, disable iff and body.
type Property struct {
	Name    string
	Clock   string
	Edge    string // "posedge" | "negedge"
	Disable BoolExpr
	Body    Node
}

// File is a parsed property source.
type File struct {
	Properties []*Property
}

// Signals returns the sorted set of signal names referenced anywhere
// in the property (used for dependency-scoped invalidation).
func (p *Property) Signals() []string {
	set := map[string]bool{}
	var walkB func(b BoolExpr)
	walkB = func(b BoolExpr) {
		switch e := b.(type) {
		case Cmp:
			set[e.Sig] = true
		case Not:
			walkB(e.E)
		case And:
			walkB(e.L)
			walkB(e.R)
		}
	}
	var walkN func(n Node)
	walkN = func(n Node) {
		switch e := n.(type) {
		case Atom:
			walkB(e.Expr)
		case Concat:
			walkN(e.Left)
			walkN(e.Right)
		case Or:
			walkN(e.L)
			walkN(e.R)
		case Repeat:
			walkN(e.Sub)
		case Impl:
			walkN(e.Ant)
			walkN(e.Cons)
		}
	}
	if p.Disable != nil {
		walkB(p.Disable)
	}
	walkN(p.Body)
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// String renders a node back to source form (stable identity for AST).
func String(n Node) string {
	switch e := n.(type) {
	case Atom:
		return BoolString(e.Expr)
	case Empty:
		return "[*0]"
	case Concat:
		d := fmt.Sprintf("##%d", e.Delay.Min)
		if e.Delay.Min != e.Delay.Max {
			d = fmt.Sprintf("##[%d:%d]", e.Delay.Min, e.Delay.Max)
		}
		return String(e.Left) + " " + d + " " + String(e.Right)
	case Or:
		return String(e.L) + " or " + String(e.R)
	case Repeat:
		return String(e.Sub) + fmt.Sprintf("[*%d]", e.Count)
	case Impl:
		op := "|->"
		if !e.Overlapped {
			op = "|=>"
		}
		return String(e.Ant) + " " + op + " " + String(e.Cons)
	}
	return "?"
}

// BoolString renders a boolean expression.
func BoolString(b BoolExpr) string {
	switch e := b.(type) {
	case Cmp:
		return fmt.Sprintf("%s%s%s", e.Sig, e.Op, e.Val)
	case Not:
		return "!(" + BoolString(e.E) + ")"
	case And:
		return BoolString(e.L) + " && " + BoolString(e.R)
	}
	return "?"
}

// Normalize expands repeats into concatenations so the evaluator only
// handles Atom/Empty/Concat/Or/Impl.
func Normalize(n Node) Node {
	switch e := n.(type) {
	case Repeat:
		if e.Count <= 0 {
			return Empty{}
		}
		sub := Normalize(e.Sub)
		var out Node = sub
		for i := 1; i < e.Count; i++ {
			out = Concat{Left: out, Delay: Delay{Min: 1, Max: 1}, Right: sub}
		}
		return out
	case Concat:
		return Concat{Left: Normalize(e.Left), Delay: e.Delay, Right: Normalize(e.Right)}
	case Or:
		return Or{L: Normalize(e.L), R: Normalize(e.R)}
	case Impl:
		return Impl{Ant: Normalize(e.Ant), Cons: Normalize(e.Cons), Overlapped: e.Overlapped}
	}
	return n
}

// Key is a canonical serialization used to deduplicate residuals.
func Key(n Node) string { return String(n) }

var _ = strings.TrimSpace
