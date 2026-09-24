// Package property defines the AST of the small sequence property language
// and its parser. The AST is stored verbatim (with the semantics version it
// was written against) so that later reinterpretation can build new branches
// without touching the original.
package property

import "fmt"

// Semantics versions. Differences are explicit and replay-visible:
//
//	v1: X/Z in a boolean expression makes the attempt vacuous (skipped).
//	v2: X/Z propagates and yields an explicit UNKNOWN verdict.
//	v1: empty match ([*0]) of the consequent of |-> is rejected.
//	v2: empty match is legal and reported as an empty-match event.
const (
	SemV1 = "sem/1"
	SemV2 = "sem/2"
)

// BoolExpr is a boolean expression over four-state signals.
type BoolExpr struct {
	Op   string      `json:"op"` // "sig" | "not" | "and" | "or"
	Sig  string      `json:"sig,omitempty"`
	Args []*BoolExpr `json:"args,omitempty"`
}

func Sig(name string) *BoolExpr    { return &BoolExpr{Op: "sig", Sig: name} }
func Not(a *BoolExpr) *BoolExpr    { return &BoolExpr{Op: "not", Args: []*BoolExpr{a}} }
func And(a, b *BoolExpr) *BoolExpr { return &BoolExpr{Op: "and", Args: []*BoolExpr{a, b}} }
func Or(a, b *BoolExpr) *BoolExpr  { return &BoolExpr{Op: "or", Args: []*BoolExpr{a, b}} }

func (e *BoolExpr) String() string {
	switch e.Op {
	case "sig":
		return e.Sig
	case "not":
		return "!" + e.Args[0].String()
	case "and":
		return "(" + e.Args[0].String() + " & " + e.Args[1].String() + ")"
	case "or":
		return "(" + e.Args[0].String() + " | " + e.Args[1].String() + ")"
	}
	return "?"
}

// Delay is a cycle delay ##N or ##[A:B]. Max=-1 means unbounded.
type Delay struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Repeat is a repetition [*N] or [*A:B]. Min=Max=0 is the empty match.
type Repeat struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SeqItem is one boolean expression with an optional delay before it and an
// optional repetition.
type SeqItem struct {
	Delay  Delay     `json:"delay"`
	Expr   *BoolExpr `json:"expr"`
	Repeat *Repeat   `json:"repeat,omitempty"` // nil means exactly one
}

// Seq is a concatenation of items.
type Seq struct {
	Items []SeqItem `json:"items"`
}

func (s Seq) String() string {
	out := ""
	for i, it := range s.Items {
		if i > 0 || it.Delay.Min != 0 || it.Delay.Max != 0 {
			if it.Delay.Max == it.Delay.Min {
				out += fmt.Sprintf("##%d ", it.Delay.Min)
			} else {
				out += fmt.Sprintf("##[%d:%d] ", it.Delay.Min, it.Delay.Max)
			}
		}
		out += it.Expr.String()
		if it.Repeat != nil {
			if it.Repeat.Min == it.Repeat.Max {
				out += fmt.Sprintf("[*%d]", it.Repeat.Min)
			} else {
				out += fmt.Sprintf("[*%d:%d]", it.Repeat.Min, it.Repeat.Max)
			}
		}
		if i < len(s.Items)-1 {
			out += " "
		}
	}
	return out
}

// Implication operators.
const (
	OpOverlapped    = "|->" // consequent starts in the same cycle
	OpNonOverlapped = "|=>" // consequent starts one cycle later
)

// Property is a full property definition.
type Property struct {
	Name       string    `json:"name"`
	Clock      string    `json:"clock"`
	Posedge    bool      `json:"posedge"`
	SemVersion string    `json:"sem_version"`
	DisableIff *BoolExpr `json:"disable_iff,omitempty"`
	Ant        Seq       `json:"ant"`
	Op         string    `json:"op"`
	Con        Seq       `json:"con"`
	Raw        string    `json:"raw"` // original source text, preserved
}

// ResetInterval marks a reset region [Start,End] in normalized fs.
type ResetInterval struct {
	Signal string `json:"signal"`
	Start  int64  `json:"start_fs"`
	End    int64  `json:"end_fs"`
}
