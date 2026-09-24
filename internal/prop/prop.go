// Package prop defines the small sequence-property language (an SVA
// subset) with its AST, lexer and parser. The AST is stored verbatim so
// that every replay can be traced back to the exact property text and
// semantics version that produced it.
package prop

import (
	"fmt"
	"strings"
)

// ---- AST ----

type Node interface{ node() }

type Ident struct{ Name string }
type Not struct{ X Node }
type And struct{ L, R Node }
type Or struct{ L, R Node }

// Seq is a concatenation: First, then each Rest step after Delay cycles.
type Seq struct {
	First Node
	Rest  []SeqStep
}
type SeqStep struct {
	Delay int
	Expr  Node
}

type ImpKind int

const (
	Overlapped    ImpKind = iota // |->
	NonOverlapped                // |=>
)

// Implies is (disable-iff-free) implication with a sequence antecedent.
type Implies struct {
	Kind    ImpKind
	Ante    *Seq
	Conseq  Node
}

// DisableIff aborts evaluation of Body while Cond holds.
type DisableIff struct {
	Cond Node
	Body Node
}

func (Ident) node()      {}
func (Not) node()        {}
func (And) node()        {}
func (Or) node()         {}
func (Seq) node()        {}
func (Implies) node()    {}
func (DisableIff) node() {}

// String renders the node in a canonical, parseable form.
func String(n Node) string {
	switch t := n.(type) {
	case Ident:
		return t.Name
	case Not:
		return "!(" + String(t.X) + ")"
	case And:
		return "(" + String(t.L) + " & " + String(t.R) + ")"
	case Or:
		return "(" + String(t.L) + " | " + String(t.R) + ")"
	case Seq:
		var b strings.Builder
		b.WriteString(String(t.First))
		for _, s := range t.Rest {
			fmt.Fprintf(&b, " ##%d %s", s.Delay, String(s.Expr))
		}
		return b.String()
	case *Seq:
		return String(*t)
	case Implies:
		op := "|->"
		if t.Kind == NonOverlapped {
			op = "|=>"
		}
		return String(t.Ante) + " " + op + " " + String(t.Conseq)
	case DisableIff:
		return "disable iff (" + String(t.Cond) + ") " + String(t.Body)
	}
	return "?"
}

// ---- Declarations ----

type ClockDecl struct {
	Name   string // clock domain name
	Signal string // clock signal in the waveform
	Edge   string // "posedge" (fixed sampling semantics)
	Period int64  // sampling period in the waveform's time units
	Unit   string // raw unit text, kept verbatim
}

type ResetDecl struct {
	Signal string
	From   int64
	To     int64
}

type PropertyDecl struct {
	Name string
	Clock string // clock domain name; empty means "first clock"
	Body Node
}

type File struct {
	Clocks     []ClockDecl
	Resets     []ResetDecl
	Properties []PropertyDecl
}

// ---- Lexer ----

type tok struct {
	kind string // "ident","num","sym","kw"
	text string
	pos  int
}

func lex(src string) ([]tok, error) {
	var toks []tok
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_':
			j := i
			for j < len(src) && (src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9' || src[j] == '_') {
				j++
			}
			toks = append(toks, tok{kind: "ident", text: src[i:j], pos: i})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			toks = append(toks, tok{kind: "num", text: src[i:j], pos: i})
			i = j
		case c == '|' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '>':
			toks = append(toks, tok{kind: "sym", text: "|=>", pos: i})
			i += 3
		case c == '|' && i+2 < len(src) && src[i+1] == '-' && src[i+2] == '>':
			toks = append(toks, tok{kind: "sym", text: "|->", pos: i})
			i += 3
		case c == '#' && i+1 < len(src) && src[i+1] == '#':
			toks = append(toks, tok{kind: "sym", text: "##", pos: i})
			i += 2
		case c == '(' || c == ')' || c == '{' || c == '}' || c == ';' || c == '!' || c == '&' || c == '|':
			toks = append(toks, tok{kind: "sym", text: string(c), pos: i})
			i++
		default:
			return nil, fmt.Errorf("prop: unexpected character %q at %d", string(c), i)
		}
	}
	return toks, nil
}

// ---- Parser ----

type parser struct {
	toks []tok
	i    int
}

func (p *parser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i].text
	}
	return ""
}
func (p *parser) next() tok {
	t := p.toks[p.i]
	p.i++
	return t
}
func (p *parser) expect(s string) error {
	if p.peek() != s {
		return fmt.Errorf("prop: expected %q, got %q", s, p.peek())
	}
	p.i++
	return nil
}

// Parse parses a property file (clock/reset/property declarations).
func Parse(src string) (*File, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	f := &File{}
	for p.i < len(p.toks) {
		switch p.peek() {
		case "clock":
			p.next()
			name := p.next().text
			if err := p.expect("posedge"); err != nil {
				return nil, err
			}
			sig := p.next().text
			if err := p.expect("every"); err != nil {
				return nil, err
			}
			period := p.next()
			unit := p.next()
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			var n int64
			fmt.Sscanf(period.text, "%d", &n)
			f.Clocks = append(f.Clocks, ClockDecl{Name: name, Signal: sig, Edge: "posedge", Period: n, Unit: unit.text})
		case "reset":
			p.next()
			sig := p.next().text
			if err := p.expect("from"); err != nil {
				return nil, err
			}
			from := p.next()
			if err := p.expect("to"); err != nil {
				return nil, err
			}
			to := p.next()
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			var a, b int64
			fmt.Sscanf(from.text, "%d", &a)
			fmt.Sscanf(to.text, "%d", &b)
			f.Resets = append(f.Resets, ResetDecl{Signal: sig, From: a, To: b})
		case "property":
			p.next()
			name := p.next().text
			clock := ""
			if p.peek() == "on" {
				p.next()
				clock = p.next().text
			}
			if err := p.expect("{"); err != nil {
				return nil, err
			}
			body, err := p.parseProp()
			if err != nil {
				return nil, err
			}
			if err := p.expect("}"); err != nil {
				return nil, err
			}
			f.Properties = append(f.Properties, PropertyDecl{Name: name, Clock: clock, Body: body})
		default:
			return nil, fmt.Errorf("prop: unexpected token %q", p.peek())
		}
	}
	return f, nil
}

func (p *parser) parseProp() (Node, error) {
	if p.peek() == "disable" {
		p.next()
		if err := p.expect("iff"); err != nil {
			return nil, err
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		body, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return DisableIff{Cond: cond, Body: body}, nil
	}
	s, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	switch p.peek() {
	case "|->":
		p.next()
		c, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return Implies{Kind: Overlapped, Ante: s, Conseq: c}, nil
	case "|=>":
		p.next()
		c, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return Implies{Kind: NonOverlapped, Ante: s, Conseq: c}, nil
	}
	return s, nil
}

func (p *parser) parseSeq() (*Seq, error) {
	first, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	seq := &Seq{First: first}
	for p.peek() == "##" {
		p.next()
		n := p.next()
		var d int64
		fmt.Sscanf(n.text, "%d", &d)
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		seq.Rest = append(seq.Rest, SeqStep{Delay: int(d), Expr: e})
	}
	return seq, nil
}

func (p *parser) parseExpr() (Node, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Node, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "|" {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = Or{L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseAnd() (Node, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek() == "&" {
		p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = And{L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseUnary() (Node, error) {
	if p.peek() == "!" {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return Not{X: x}, nil
	}
	if p.peek() == "(" {
		p.next()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	t := p.next()
	if t.kind != "ident" {
		return nil, fmt.Errorf("prop: expected identifier, got %q", t.text)
	}
	return Ident{Name: t.text}, nil
}
