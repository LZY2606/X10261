// Package prop defines the small sequence property language, its parser and
// the fixed-sampling-semantics replay evaluator with three-valued results.
package prop

import "fmt"

// Bool is a boolean expression over sampled four-state signal values.
type Bool struct {
	Op   string // "sig","const","and","or","not","eq","ne","lt","le","gt","ge"
	Name string // for "sig"
	Val  string // for "const" (four-state string)
	L, R *Bool
}

// Seq is a sequence expression.
type Seq struct {
	Kind string // "bool","delay","and","or","impl"(overlapped),"impln"(non-overlapped),"rep"
	B    *Bool  // for "bool"
	N    int    // for "delay" cycles, "rep" count
	L, R *Seq
}

// Property is a parsed property with clocking and disable condition.
type Property struct {
	Name    string
	Clock   string
	Edge    string // "posedge" | "negedge"
	Disable *Bool  // disable iff expression, nil if absent
	Body    *Seq
	Source  string // raw source text, preserved
}

// SemVersion selects the replay semantics revision.
type SemVersion string

const (
	SemV1 SemVersion = "v1"
	SemV2 SemVersion = "v2"
)

// Semantic differences between versions (documented, machine-checkable):
//   - v1: unknown (X/Z) antecedent of an implication -> attempt result UNKNOWN.
//     empty repetition [*0] matches empty -> contributes TRUE (vacuous).
//   - v2: unknown antecedent -> attempt is vacuous TRUE.
//     empty repetition [*0] -> UNKNOWN (refuses to silently accept zero-width).
//
// Both versions: X/Z in disable iff -> thread result UNKNOWN (never silently
// treated as inactive); disable iff TRUE -> thread DISABLED.

func (b *Bool) String() string {
	switch b.Op {
	case "sig":
		return b.Name
	case "const":
		return "'" + b.Val
	case "not":
		return "!(" + b.L.String() + ")"
	case "and":
		return "(" + b.L.String() + " && " + b.R.String() + ")"
	case "or":
		return "(" + b.L.String() + " || " + b.R.String() + ")"
	default:
		sym := map[string]string{"eq": "==", "ne": "!=", "lt": "<", "le": "<=", "gt": ">", "ge": ">="}[b.Op]
		return "(" + b.L.String() + " " + sym + " " + b.R.String() + ")"
	}
}

func (s *Seq) String() string {
	switch s.Kind {
	case "bool":
		return s.B.String()
	case "delay":
		return fmt.Sprintf("%s ##%d %s", s.L.String(), s.N, s.R.String())
	case "and":
		return "(" + s.L.String() + " and " + s.R.String() + ")"
	case "or":
		return "(" + s.L.String() + " or " + s.R.String() + ")"
	case "impl":
		return "(" + s.L.String() + " |-> " + s.R.String() + ")"
	case "impln":
		return "(" + s.L.String() + " |=> " + s.R.String() + ")"
	case "rep":
		return fmt.Sprintf("%s [*%d]", s.L.String(), s.N)
	}
	return "?"
}

// Signals returns the set of signal names referenced by the property.
func (p *Property) Signals() map[string]bool {
	out := map[string]bool{}
	var walkB func(b *Bool)
	walkB = func(b *Bool) {
		if b == nil {
			return
		}
		if b.Op == "sig" {
			out[b.Name] = true
		}
		walkB(b.L)
		walkB(b.R)
	}
	var walkS func(s *Seq)
	walkS = func(s *Seq) {
		if s == nil {
			return
		}
		if s.Kind == "bool" {
			walkB(s.B)
		}
		walkS(s.L)
		walkS(s.R)
	}
	walkB(p.Disable)
	walkS(p.Body)
	out[p.Clock] = true
	return out
}
