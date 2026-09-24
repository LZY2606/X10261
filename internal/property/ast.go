// Package property implements a small sequence property language with
// fixed sampling semantics. Every judgement produced during evaluation
// carries an explicit three-valued result and is emitted as a
// machine-readable event.
package property

import (
	"fmt"
	"strings"

	"signalcourt/internal/fourstate"
)

// ---- boolean expressions ----

type Expr interface{ isExpr() }

type SignalRef struct{ Name string }
type Const struct{ V fourstate.Value }
type Not struct{ E Expr }
type And struct{ L, R Expr }
type Or struct{ L, R Expr }

func (SignalRef) isExpr() {}
func (Const) isExpr()     {}
func (Not) isExpr()       {}
func (And) isExpr()       {}
func (Or) isExpr()        {}

func ExprString(e Expr) string {
	switch e := e.(type) {
	case SignalRef:
		return e.Name
	case Const:
		return "1'b" + string(e.V.Char())
	case Not:
		return "!" + ExprString(e.E)
	case And:
		return "(" + ExprString(e.L) + " && " + ExprString(e.R) + ")"
	case Or:
		return "(" + ExprString(e.L) + " || " + ExprString(e.R) + ")"
	}
	return "?"
}

// ---- sequences ----

type SeqElem interface{ isSeqElem() }

// BoolItem matches when the expression holds in the current cycle.
type BoolItem struct{ E Expr }

// Delay advances N cycles (##N).
type Delay struct{ N int }

// Repeat matches the expression between Min and Max times; Min==0 makes
// an empty match possible, which must still yield an explicit result.
type Repeat struct {
	E          Expr
	Min, Max   int
}

func (BoolItem) isSeqElem() {}
func (Delay) isSeqElem()    {}
func (Repeat) isSeqElem()   {}

type Seq []SeqElem

func SeqString(s Seq) string {
	parts := []string{}
	for _, el := range s {
		switch el := el.(type) {
		case BoolItem:
			parts = append(parts, ExprString(el.E))
		case Delay:
			parts = append(parts, fmt.Sprintf("##%d", el.N))
		case Repeat:
			if el.Min == el.Max {
				parts = append(parts, fmt.Sprintf("%s[*%d]", ExprString(el.E), el.Min))
			} else {
				parts = append(parts, fmt.Sprintf("%s[*%d:%d]", ExprString(el.E), el.Min, el.Max))
			}
		}
	}
	return strings.Join(parts, " ")
}

// ---- property ----

const (
	Overlapped    = "|->" // consequent starts in the same cycle
	NonOverlapped = "|=>" // consequent starts one cycle later
)

type Property struct {
	Name        string
	ClockEdge   string // "posedge" or "negedge"
	ClockSignal string
	DisableIff  Expr // may be nil
	Antecedent  Seq
	Op          string
	Consequent  Seq
}
