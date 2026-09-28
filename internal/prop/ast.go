// Package prop defines the small SVA-style gating-sequence property language.
// Every property is parsed once per semantic version: the AST plus the
// semantic-version tag are retained together so replaying under a different
// rule version never silently rewrites old conclusions.
package prop

import "fmt"

// Node kinds for the property AST.
type Kind string

const (
	KIdent Kind = "ident"
	KConst Kind = "const"
	KNot   Kind = "not"
	KAnd   Kind = "and"
	KOr    Kind = "or"
	KSeq   Kind = "sequence"
	KImplO Kind = "impl_overlap"
	KImplN Kind = "impl_nonoverlap"
	KProp  Kind = "property"
	KDisIf Kind = "disable_iff"
)

// Expr is a Boolean expression node.
type Expr struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name,omitempty"` // identifier text or 0/1/X/Z literal
	Args []*Expr `json:"args,omitempty"`
}

// Event is ##[lo:hi] cycle range (positive integer bounds).
type Event struct {
	Lo int `json:"lo"`
	Hi int `json:"hi"` // -1 denotes open-ended $ (in this pack, always bounded)
}

// Sequence is one RHS sequence: Boolean guard, delay, final Boolean.
type Sequence struct {
	Guard *Expr  `json:"guard,omitempty"` // nil means no SERE-style guard
	Delay Event  `json:"delay"`
	End   *Expr  `json:"end"`
}

// Property is a top-level assertion with optional disable condition.
type Property struct {
	Name     string     `json:"name"`
	Ante     *Expr      `json:"antecedent"`
	Cons     *Sequence  `json:"consequent"`
	Overlap  bool       `json:"overlap"` // true: |->, false: |=>
	Disable  *Expr      `json:"disable_iff,omitempty"`
	SemVer   string     `json:"semver"`
}

// Walk applies fn to every expression in depth-first order.
func (e *Expr) Walk(fn func(*Expr)) {
	if e == nil {
		return
	}
	fn(e)
	for _, a := range e.Args {
		a.Walk(fn)
	}
}

// SignalNames returns the identifiers referenced (used for dependency scopes).
func (p *Property) SignalNames() []string {
	seen := map[string]bool{}
	var out []string
	add := func(e *Expr) {
		e.Walk(func(x *Expr) {
			if x.Kind == KIdent && !seen[x.Name] {
				seen[x.Name] = true
				out = append(out, x.Name)
			}
		})
	}
	add(p.Ante)
	if p.Disable != nil {
		add(p.Disable)
	}
	if p.Cons.Guard != nil {
		add(p.Cons.Guard)
	}
	add(p.Cons.End)
	return out
}

func (e *Expr) String() string {
	switch e.Kind {
	case KIdent, KConst:
		return e.Name
	case KNot:
		return "!(" + e.Args[0].String() + ")"
	case KAnd, KOr:
		op := " && "
		if e.Kind == KOr {
			op = " || "
		}
		return "(" + e.Args[0].String() + op + e.Args[1].String() + ")"
	default:
		return fmt.Sprintf("<%s>", e.Kind)
	}
}
