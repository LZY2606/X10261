// Package prop defines the small sequence-property language: boolean
// expressions, ## delays, [*] repetition (including empty match [*0]),
// or, overlapped (|->) and non-overlapped (|=>) implication, and
// disable iff. The AST is preserved verbatim in the archive.
package prop

import (
	"fmt"
	"strings"

	"signaltribunal/internal/logic"
)

// BoolExpr is a sampled boolean expression.
type BoolExpr struct {
	Op    string    `json:"op"` // "sig" | "eq" | "not" | "and"
	Sig   string    `json:"sig,omitempty"`
	Const string    `json:"const,omitempty"` // "0","1","x","z" for eq
	Left  *BoolExpr `json:"left,omitempty"`
	Right *BoolExpr `json:"right,omitempty"`
}

func (b *BoolExpr) String() string {
	switch b.Op {
	case "sig":
		return b.Sig
	case "eq":
		return fmt.Sprintf("%s == 1'b%s", b.Sig, b.Const)
	case "not":
		return "!" + b.Left.String()
	case "and":
		return b.Left.String() + " && " + b.Right.String()
	}
	return "?"
}

// Seq is a sequence-expression AST node.
type Seq struct {
	Kind  string    `json:"kind"` // "bool" | "delay" | "rep" | "or"
	Bool  *BoolExpr `json:"bool,omitempty"`
	Left  *Seq      `json:"left,omitempty"`
	Right *Seq      `json:"right,omitempty"`
	Min   int       `json:"min,omitempty"`
	Max   int       `json:"max,omitempty"` // -1 means unbounded ($)
}

func (s *Seq) String() string {
	switch s.Kind {
	case "bool":
		return s.Bool.String()
	case "delay":
		r := fmt.Sprintf("##%d", s.Min)
		if s.Max != s.Min {
			if s.Max < 0 {
				r = fmt.Sprintf("##[%d:$]", s.Min)
			} else {
				r = fmt.Sprintf("##[%d:%d]", s.Min, s.Max)
			}
		}
		return s.Left.String() + " " + r + " " + s.Right.String()
	case "rep":
		if s.Max == s.Min {
			return fmt.Sprintf("%s[*%d]", s.Left.String(), s.Min)
		}
		if s.Max < 0 {
			return fmt.Sprintf("%s[*%d:$]", s.Left.String(), s.Min)
		}
		return fmt.Sprintf("%s[*%d:%d]", s.Left.String(), s.Min, s.Max)
	case "or":
		return "(" + s.Left.String() + " or " + s.Right.String() + ")"
	}
	return "?"
}

// Property is a full property declaration.
type Property struct {
	Name    string    `json:"name"`
	Disable *BoolExpr `json:"disable,omitempty"`
	Ante    *Seq      `json:"ante"`
	Op      string    `json:"op,omitempty"` // "|->" overlapped, "|=>" non-overlapped, "" plain
	Cons    *Seq      `json:"cons,omitempty"`
	Source  string    `json:"source"` // original text, preserved
}

func (p *Property) String() string {
	var b strings.Builder
	b.WriteString("property " + p.Name + ": ")
	if p.Disable != nil {
		b.WriteString("disable iff (" + p.Disable.String() + ") ")
	}
	b.WriteString(p.Ante.String())
	if p.Op != "" {
		b.WriteString(" " + p.Op + " " + p.Cons.String())
	}
	b.WriteString(";")
	return b.String()
}

// Automaton is a display-oriented state machine derived from the AST.
type Automaton struct {
	States []string    `json:"states"`
	Edges  [][3]string `json:"edges"` // from, label, to
}

// Automaton flattens the property into a simple labeled graph for the
// review UI. It is a view only; evaluation uses the AST directly.
func (p *Property) Automaton() Automaton {
	a := Automaton{}
	n := 0
	next := func() string { s := fmt.Sprintf("S%d", n); n++; return s }
	start := next()
	a.States = append(a.States, start)
	var build func(s *Seq, from string) []string
	build = func(s *Seq, from string) []string {
		switch s.Kind {
		case "bool":
			to := next()
			a.States = append(a.States, to)
			a.Edges = append(a.Edges, [3]string{from, s.Bool.String(), to})
			return []string{to}
		case "delay":
			label := fmt.Sprintf("##%d", s.Min)
			if s.Max != s.Min {
				if s.Max < 0 {
					label = fmt.Sprintf("##[%d:$]", s.Min)
				} else {
					label = fmt.Sprintf("##[%d:%d]", s.Min, s.Max)
				}
			}
			var mids []string
			for _, lo := range build(s.Left, from) {
				to := next()
				a.States = append(a.States, to)
				a.Edges = append(a.Edges, [3]string{lo, label, to})
				mids = append(mids, to)
			}
			var outs []string
			for _, m := range mids {
				outs = append(outs, build(s.Right, m)...)
			}
			return outs
		case "rep":
			loop := next()
			a.States = append(a.States, loop)
			a.Edges = append(a.Edges, [3]string{from, fmt.Sprintf("[*%d:%d]", s.Min, s.Max), loop})
			outs := build(s.Left, loop)
			for _, o := range outs {
				a.Edges = append(a.Edges, [3]string{o, "iterate", loop})
			}
			exit := next()
			a.States = append(a.States, exit)
			a.Edges = append(a.Edges, [3]string{loop, "exit", exit})
			return []string{exit}
		case "or":
			var outs []string
			outs = append(outs, build(s.Left, from)...)
			outs = append(outs, build(s.Right, from)...)
			return outs
		}
		return []string{from}
	}
	ends := build(p.Ante, start)
	if p.Cons != nil {
		for _, e := range ends {
			var outs []string
			if p.Op == "|=>" {
				gap := next()
				a.States = append(a.States, gap)
				a.Edges = append(a.Edges, [3]string{e, "|=> (1 tick)", gap})
				outs = build(p.Cons, gap)
			} else {
				outs = build(p.Cons, e)
			}
			for _, o := range outs {
				a.Edges = append(a.Edges, [3]string{o, "accept", "ACCEPT"})
			}
		}
	} else {
		for _, e := range ends {
			a.Edges = append(a.Edges, [3]string{e, "accept", "ACCEPT"})
		}
	}
	a.States = append(a.States, "ACCEPT")
	return a
}

// ConstV4 parses a constant literal to a four-state value.
func ConstV4(s string) (logic.V4, bool) { return logic.ParseV4(s[0]) }
