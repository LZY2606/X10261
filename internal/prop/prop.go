// Package prop defines the small sequence-property language. The AST is
// preserved verbatim alongside its raw source text so that later
// reinterpretation under a different semantics version never loses the
// original form.
package prop

import (
	"fmt"
	"strings"
)

// SemanticsVersion identifies the replay semantics. Versions differ in
// how empty matches are resolved; both remain available forever.
const (
	SemV10 = "1.0" // empty match => vacuous true
	SemV11 = "1.1" // empty match => explicit unknown
)

// BoolExpr is a boolean expression over sampled four-state signals.
type BoolExpr struct {
	Op    string // "sig" | "not" | "and" | "or"
	Name  string // for Op=="sig"
	Left  *BoolExpr
	Right *BoolExpr
}

func Sig(name string) *BoolExpr { return &BoolExpr{Op: "sig", Name: name} }

// SeqElem is one element of a sequence: a boolean expression preceded by
// a delay from the previous element.
type SeqElem struct {
	// DelayMin/DelayMax in sampled clock cycles relative to previous
	// element. DelayMin==DelayMax==0 on the first element means "now".
	DelayMin int
	DelayMax int
	Expr     *BoolExpr
}

// Sequence is an ordered list of elements.
type Sequence struct {
	Elems []SeqElem
}

// CanMatchEmpty reports whether the sequence can match zero cycles.
func (s Sequence) CanMatchEmpty() bool {
	if len(s.Elems) == 0 {
		return true
	}
	for _, e := range s.Elems {
		if e.DelayMin > 0 {
			return false
		}
	}
	// All delays may be zero; an empty match is only possible for the
	// degenerate zero-element sequence. Kept for clarity.
	return len(s.Elems) == 0
}

// Property is one named assertion.
type Property struct {
	Name       string
	Raw        string // original source text, preserved
	Clock      string
	DisableIff string // "" if none
	Antecedent Sequence
	Overlapped bool // |-> overlapped, |=> non-overlapped
	Consequent Sequence
}

// File is a parsed property file.
type File struct {
	Raw        string
	Properties []Property
}

// Parse parses a property file:
//
//	property <name> @(posedge <clk>) [disable iff (<sig>)]
//	  <seq> (|-> | |=>) <seq> ;
//
// A sequence element is [##N | ##[A:B]] <boolexpr>; a boolexpr is
// name, !name, a & b, a | b, or (a).
func Parse(src string) (*File, error) {
	f := &File{Raw: src}
	p := &parser{src: src}
	for {
		p.skipSpace()
		if p.eof() {
			break
		}
		prop, err := p.parseProperty()
		if err != nil {
			return nil, err
		}
		f.Properties = append(f.Properties, *prop)
	}
	if len(f.Properties) == 0 {
		return nil, fmt.Errorf("no properties found")
	}
	return f, nil
}

type parser struct {
	src string
	pos int
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) skipSpace() {
	for !p.eof() {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			p.pos++
			continue
		}
		if c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/' {
			for !p.eof() && p.src[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		break
	}
}

func (p *parser) expect(s string) error {
	p.skipSpace()
	if !strings.HasPrefix(p.src[p.pos:], s) {
		return fmt.Errorf("expected %q at offset %d", s, p.pos)
	}
	p.pos += len(s)
	return nil
}

func (p *parser) ident() (string, error) {
	p.skipSpace()
	start := p.pos
	for !p.eof() {
		c := p.src[p.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", fmt.Errorf("expected identifier at offset %d", start)
	}
	return p.src[start:p.pos], nil
}

func (p *parser) number() (int, error) {
	p.skipSpace()
	start := p.pos
	for !p.eof() && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, fmt.Errorf("expected number at offset %d", start)
	}
	var n int
	fmt.Sscanf(p.src[start:p.pos], "%d", &n)
	return n, nil
}

func (p *parser) parseProperty() (*Property, error) {
	start := p.pos
	if err := p.expect("property"); err != nil {
		return nil, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("@"); err != nil {
		return nil, err
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	if err := p.expect("posedge"); err != nil {
		return nil, err
	}
	clock, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	pr := &Property{Name: name, Clock: clock, Overlapped: true}
	p.skipSpace()
	if strings.HasPrefix(p.src[p.pos:], "disable") {
		p.pos += len("disable")
		if err := p.expect("iff"); err != nil {
			return nil, err
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		sig, err := p.ident()
		if err != nil {
			return nil, err
		}
		pr.DisableIff = sig
		if err := p.expect(")"); err != nil {
			return nil, err
		}
	}
	ant, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	pr.Antecedent = *ant
	p.skipSpace()
	switch {
	case strings.HasPrefix(p.src[p.pos:], "|=>"):
		pr.Overlapped = false
		p.pos += 3
	case strings.HasPrefix(p.src[p.pos:], "|->"):
		pr.Overlapped = true
		p.pos += 3
	default:
		return nil, fmt.Errorf("expected |-> or |=> at offset %d", p.pos)
	}
	con, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	pr.Consequent = *con
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	pr.Raw = strings.TrimSpace(p.src[start:p.pos])
	return pr, nil
}

func (p *parser) parseSeq() (*Sequence, error) {
	s := &Sequence{}
	for {
		p.skipSpace()
		if len(s.Elems) == 0 && strings.HasPrefix(p.src[p.pos:], "empty") {
			// Explicit empty sequence: matches zero cycles. Its
			// three-valued result is fixed by the semantics version.
			p.pos += len("empty")
			return s, nil
		}
		min, max := 0, 0
		if strings.HasPrefix(p.src[p.pos:], "##") {
			p.pos += 2
			p.skipSpace()
			if strings.HasPrefix(p.src[p.pos:], "[") {
				p.pos++
				a, err := p.number()
				if err != nil {
					return nil, err
				}
				if err := p.expect(":"); err != nil {
					return nil, err
				}
				b, err := p.number()
				if err != nil {
					return nil, err
				}
				if err := p.expect("]"); err != nil {
					return nil, err
				}
				min, max = a, b
			} else {
				n, err := p.number()
				if err != nil {
					return nil, err
				}
				min, max = n, n
			}
		}
		expr, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		s.Elems = append(s.Elems, SeqElem{DelayMin: min, DelayMax: max, Expr: expr})
		p.skipSpace()
		// Sequence continues only if a delay or another element follows
		// that is not the implication operator or terminator.
		if strings.HasPrefix(p.src[p.pos:], "##") {
			continue
		}
		break
	}
	return s, nil
}

func (p *parser) parseBool() (*BoolExpr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (*BoolExpr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.eof() || p.src[p.pos] != '|' || strings.HasPrefix(p.src[p.pos:], "|->") || strings.HasPrefix(p.src[p.pos:], "|=>") {
			return l, nil
		}
		p.pos++
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &BoolExpr{Op: "or", Left: l, Right: r}
	}
}

func (p *parser) parseAnd() (*BoolExpr, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if p.eof() || p.src[p.pos] != '&' {
			return l, nil
		}
		p.pos++
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = &BoolExpr{Op: "and", Left: l, Right: r}
	}
}

func (p *parser) parseUnary() (*BoolExpr, error) {
	p.skipSpace()
	if p.eof() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	if p.src[p.pos] == '!' {
		p.pos++
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &BoolExpr{Op: "not", Left: e}, nil
	}
	if p.src[p.pos] == '(' {
		p.pos++
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	return Sig(name), nil
}
