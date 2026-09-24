// Package prop defines the small sequence property language, its parser and
// the fixed-sampling-semantics evaluator.
package prop

import "sigtribunal/internal/fourval"

// ImpOp selects overlapped (|->) or non-overlapped (|=>) implication.
type ImpOp int

const (
	Overlapped ImpOp = iota
	NonOverlapped
)

func (o ImpOp) String() string {
	if o == NonOverlapped {
		return "|=>"
	}
	return "|->"
}

// Delay is a cycle delay range between sequence items.
type Delay struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Item is one sequence element: a boolean expression, an optional exact
// repetition count (0 = empty match allowed), and the delay to the next item.
type Item struct {
	Expr  BExpr `json:"-"`
	Rep   int   `json:"rep"`
	Delay Delay `json:"delay"`
}

// Seq is a sequence with an optional leading delay (Pre).
type Seq struct {
	Pre   Delay  `json:"pre"`
	Items []Item `json:"-"`
}

// BExpr is a boolean expression node.
type BExpr interface{ bexpr() }

type Signal struct{ Name string }
type Const struct{ Val fourval.V }
type Not struct{ E BExpr }
type And struct{ L, R BExpr }
type Or struct{ L, R BExpr }
type Cmp struct {
	Op  string // "==" or "!="
	L, R BExpr
}

func (Signal) bexpr() {}
func (Const) bexpr()  {}
func (Not) bexpr()    {}
func (And) bexpr()    {}
func (Or) bexpr()     {}
func (Cmp) bexpr()    {}

// Property is a parsed assertion. Raw keeps the original source text.
type Property struct {
	Name       string `json:"name"`
	Clock      string `json:"clock"`
	DisableIff BExpr  `json:"-"`
	Antecedent Seq    `json:"-"`
	Op         ImpOp  `json:"op"`
	Consequent Seq    `json:"-"`
	Raw        string `json:"raw"`
}

// ReferencedSignals returns the sorted set of signal names used by p.
func (p *Property) ReferencedSignals() []string {
	set := map[string]bool{}
	var walk func(e BExpr)
	walk = func(e BExpr) {
		switch n := e.(type) {
		case Signal:
			set[n.Name] = true
		case Not:
			walk(n.E)
		case And:
			walk(n.L)
			walk(n.R)
		case Or:
			walk(n.L)
			walk(n.R)
		case Cmp:
			walk(n.L)
			walk(n.R)
		}
	}
	var walkSeq func(s Seq)
	walkSeq = func(s Seq) {
		for _, it := range s.Items {
			walk(it.Expr)
		}
	}
	if p.DisableIff != nil {
		walk(p.DisableIff)
	}
	walkSeq(p.Antecedent)
	walkSeq(p.Consequent)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
