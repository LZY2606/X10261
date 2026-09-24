package property

import (
	"fmt"
	"strconv"
)

// Parse parses a full module text and assigns stable node identities.
func Parse(src string) (*Module, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	m := &Module{RawText: src}
	if !p.acceptWord("module") {
		return nil, fmt.Errorf("property text must start with 'module' at offset %d", p.peek().pos)
	}
	t := p.next()
	if t.kind != tIdent {
		return nil, fmt.Errorf("module name expected at offset %d", t.pos)
	}
	m.Name = t.text
	if err := p.expectPunct(";"); err != nil {
		return nil, err
	}
	// clock line: clock <sig> @ posedge <clk> ;
	if err := p.expectWord("clock"); err != nil {
		return nil, err
	}
	if err := p.expectPunct("@"); err != nil {
		return nil, err
	}
	et := p.next()
	if et.text != "posedge" && et.text != "negedge" {
		return nil, fmt.Errorf("expected posedge/negedge at %d", et.pos)
	}
	m.ClockEdge = et.text
	ct := p.next()
	if ct.kind != tIdent {
		return nil, fmt.Errorf("clock name expected at %d", ct.pos)
	}
	m.ClockName = ct.text
	if err := p.expectPunct(";"); err != nil {
		return nil, err
	}
	if p.acceptWord("reset") {
		rt := p.next()
		if rt.kind != tIdent {
			return nil, fmt.Errorf("reset signal name expected at %d", rt.pos)
		}
		m.ResetName = rt.text
		if err := p.expectPunct(";"); err != nil {
			return nil, err
		}
	}
	for p.peek().kind != tEOF {
		d, err := p.parseDecl()
		if err != nil {
			return nil, err
		}
		m.Decls = append(m.Decls, d)
	}
	m.AssignIDs()
	return m, nil
}

func (p *parser) parseDecl() (*Decl, error) {
	name := p.next()
	if name.kind != tIdent {
		return nil, fmt.Errorf("property name expected at offset %d", name.pos)
	}
	if err := p.expectPunct(":"); err != nil {
		return nil, err
	}
	pr, err := p.parseProperty()
	if err != nil {
		return nil, err
	}
	if err := p.expectPunct(";"); err != nil {
		return nil, err
	}
	d := &Decl{Name: name.text, Property: pr}
	d.NodeID = contentID("decl", []string{d.Name, pr.assignIDs()})
	return d, nil
}

func (p *parser) parseProperty() (*Property, error) {
	var disable *BoolExpr
	if p.acceptWord("disable") {
		if err := p.expectWord("iff"); err != nil {
			return nil, err
		}
		if err := p.expectPunct("("); err != nil {
			return nil, err
		}
		c, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		disable = c
	}
	strength := ""
	if p.acceptWord("strong") {
		strength = "strong"
	} else if p.acceptWord("weak") {
		strength = "weak"
	}
	ante, err := p.parseSequence()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tPunct && (p.peek().text == "|->" || p.peek().text == "|=>") {
		op := p.next().text
		cons, err := p.parseProperty()
		if err != nil {
			return nil, err
		}
		pr := &Property{Kind: PropImp, Ante: ante, Cons: cons, Overlapped: op == "|->"}
		pr.NodeID = contentID("pimp", []any{ante.assignIDs(), pr.Overlapped, cons.assignIDs()})
		return wrapDisable(disable, pr), nil
	}
	if strength != "" && p.peek().kind == tPunct && (p.peek().text == "|->" || p.peek().text == "|=>") {
		// strength on the antecedent of an implication: strength applies to
		// the antecedent sequence itself, implication still proceeds.
	}
	pr := &Property{Kind: PropSeq, Seq: ante, Strength: strength}
	pr.NodeID = contentID("pseq", []any{strength, ante.assignIDs()})
	return wrapDisable(disable, pr), nil
}

func wrapDisable(cond *BoolExpr, inner *Property) *Property {
	if cond == nil {
		return inner
	}
	p := &Property{Kind: PropDisable, DisableCond: cond, Inner: inner}
	p.NodeID = contentID("pdis", []any{boolID(cond), inner.assignIDs()})
	return p
}

func (p *parser) parseSequence() (*Sequence, error) {
	left, err := p.parseSeqTerm()
	if err != nil {
		return nil, err
	}
	for {
		if p.peek().kind == tPunct && p.peek().text == "##" {
			// handled below as punct "#" "#" tokens from lexer: lexer emits
			// '#' single puncts. Detect double-hash explicitly.
		}
		if p.peek().kind == tPunct && p.peek().text == "#" && p.pos+1 < len(p.toks) && p.toks[p.pos+1].text == "#" {
			p.next()
			p.next()
			lo, hi, err := p.parseRangeBracket()
			if err != nil {
				return nil, err
			}
			right, err := p.parseSeqTerm()
			if err != nil {
				return nil, err
			}
			next, err := p.parseSequenceTail(right, lo, hi)
			if err != nil {
				return nil, err
			}
			left = &Sequence{Kind: SeqDelay, DelayLo: lo, DelayHi: hi, Left: left, Right: right}
			left.NodeID = contentID("sdly", []any{lo, hi, left.Left.assignIDs(), right.assignIDs()})
			_ = next
			continue
		}
		if p.acceptWord("or") {
			right, err := p.parseSeqTerm()
			if err != nil {
				return nil, err
			}
			left = &Sequence{Kind: SeqOr, Left: left, Right: right}
			left.NodeID = contentID("sor", []any{left.Left.assignIDs(), right.assignIDs()})
			continue
		}
		if p.acceptWord("and") {
			right, err := p.parseSeqTerm()
			if err != nil {
				return nil, err
			}
			left = &Sequence{Kind: SeqAnd, Left: left, Right: right}
			left.NodeID = contentID("sand", []any{left.Left.assignIDs(), right.assignIDs()})
			continue
		}
		break
	}
	return left, nil
}

func (p *parser) parseSequenceTail(right *Sequence, lo, hi int) (*Sequence, error) {
	return right, nil
}

func (p *parser) parseRangeBracket() (int, int, error) {
	if err := p.expectPunct("["); err != nil {
		return 0, 0, err
	}
	if p.acceptPunct("*") {
		// [*0:n] inside repetition would have consumed '*' before; here '##[*..]'
		// is invalid.
		return 0, 0, fmt.Errorf("delay range must be [n:m], not [*...] at %d", p.peek().pos)
	}
	lo, err := p.parseNumber()
	if err != nil {
		return 0, 0, err
	}
	hi := lo
	if p.acceptPunct(":") {
		hi, err = p.parseNumber()
		if err != nil {
			return 0, 0, err
		}
	}
	if p.acceptPunct("$") {
		hi = 1 << 30
	}
	if err := p.expectPunct("]"); err != nil {
		return 0, 0, err
	}
	if hi < lo {
		return 0, 0, fmt.Errorf("range end %d below start %d", hi, lo)
	}
	return lo, hi, nil
}

func (p *parser) parseNumber() (int, error) {
	t := p.next()
	if t.kind != tNumber {
		return 0, fmt.Errorf("number expected at offset %d (got %q)", t.pos, t.text)
	}
	n, err := strconv.Atoi(t.text)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (p *parser) parseSeqTerm() (*Sequence, error) {
	if p.acceptWord("empty") {
		return &Sequence{Kind: SeqEmpty, NodeID: contentID("semp", nil)}, nil
	}
	cond, err := p.parseBool()
	if err != nil {
		return nil, err
	}
	s := &Sequence{Kind: SeqLit, Cond: cond}
	if p.peek().kind == tPunct && p.peek().text == "[" {
		p.next()
		if err := p.expectPunct("*"); err != nil {
			return nil, err
		}
		lo, err := p.parseNumber()
		if err != nil {
			return nil, err
		}
		hi := lo
		if p.acceptPunct(":") {
			if p.acceptPunct("$") {
				hi = 1 << 30
			} else {
				hi, err = p.parseNumber()
				if err != nil {
					return nil, err
				}
			}
		}
		if err := p.expectPunct("]"); err != nil {
			return nil, err
		}
		s.HasRep, s.RepMin, s.RepMax = true, lo, hi
	}
	s.NodeID = contentID("slit", []any{boolID(s.Cond), s.HasRep, s.RepMin, s.RepMax})
	return s, nil
}

func (p *parser) parseBool() (*BoolExpr, error) { return p.parseOr() }

func (p *parser) parseOr() (*BoolExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.acceptPunct("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BoolExpr{Op: BOr, Args: []*BoolExpr{left, right}}
	}
	left.NodeID = boolID(left)
	return left, nil
}

func (p *parser) parseAnd() (*BoolExpr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.acceptPunct("&&") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BoolExpr{Op: BAnd, Args: []*BoolExpr{left, right}}
	}
	left.NodeID = boolID(left)
	return left, nil
}

func (p *parser) parseNot() (*BoolExpr, error) {
	if p.acceptPunct("!") {
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		e := &BoolExpr{Op: BNot, Args: []*BoolExpr{inner}}
		e.NodeID = boolID(e)
		return e, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (*BoolExpr, error) {
	t := p.next()
	switch {
	case t.kind == tPunct && t.text == "(":
		inner, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return inner, nil
	case t.kind == tNumber && (t.text == "0" || t.text == "1"):
		e := &BoolExpr{Op: BConst, Const: t.text}
		e.NodeID = boolID(e)
		return e, nil
	case t.kind == tIdent && t.text == "x":
		e := &BoolExpr{Op: BConst, Const: "x"}
		e.NodeID = boolID(e)
		return e, nil
	case t.kind == tIdent:
		e := &BoolExpr{Op: BVar, Name: t.text}
		e.NodeID = boolID(e)
		return e, nil
	}
	return nil, fmt.Errorf("boolean atom expected at offset %d (got %q)", t.pos, t.text)
}
