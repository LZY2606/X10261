package property

import (
	"fmt"
	"strconv"
)

// Unbounded is used as the inclusive upper bound of an open-ended ##[lo:$].
const Unbounded = -1

type parser struct {
	src  string
	toks []token
	pos  int
}

type parseError struct {
	off int
	msg string
}

func (e *parseError) Error() string { return fmt.Sprintf("property parse at offset %d: %s", e.off, e.msg) }

// Parse parses one property document. The raw source is returned unchanged
// alongside the derived AST; parsing never edits or canonicalizes the input.
func Parse(name, source string) (*Document, error) {
	toks, err := lex(source)
	if err != nil {
		return nil, err
	}
	p := &parser{src: source, toks: toks}
	root, err := p.parseProp()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, &parseError{off: p.peek().off, msg: "trailing tokens after property"}
	}
	return &Document{Name: name, Source: source, Root: root, ParseRules: "property-grammar@1"}, nil
}

func (p *parser) peek() token  { return p.toks[p.pos] }
func (p *parser) next() token  { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) errf(off int, f string, a ...any) error {
	return &parseError{off: off, msg: fmt.Sprintf(f, a...)}
}
func (p *parser) accept(k tokKind) (token, bool) {
	if p.peek().kind == k {
		return p.next(), true
	}
	return token{}, false
}
func (p *parser) expect(k tokKind) (token, error) {
	if p.peek().kind != k {
		return token{}, p.errf(p.peek().off, "expected %s, got %q", k, p.peek().text)
	}
	return p.next(), nil
}

// prop = disable? orProp
func (p *parser) parseProp() (*Prop, error) {
	start := p.peek().off
	if p.peek().kind == tDisable {
		kw := p.next()
		if _, err := p.expect(tIff); err != nil {
			return nil, err
		}
		if _, err := p.expect(tLParen); err != nil {
			return nil, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen); err != nil {
			return nil, err
		}
		body, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return &Prop{Kind: PropDisableIff, Cond: cond, Children: []*Prop{body}, SourceRef: [2]int{kw.off, endOf(p.src)}}, nil
	}
	prop, err := p.parseOrProp()
	if err != nil {
		return nil, err
	}
	prop.SourceRef = [2]int{start, endOf(p.src)}
	return prop, nil
}

func endOf(src string) int { return len(src) }

func (p *parser) parseOrProp() (*Prop, error) {
	left, err := p.parseAndProp()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr || p.peek().kind == tSeqOr {
		op := p.next()
		right, err := p.parseAndProp()
		if err != nil {
			return nil, err
		}
		left = mergeProp(PropOr, left, right, op.off)
	}
	return left, nil
}

func mergeProp(kind PropKind, left, right *Prop, off int) *Prop {
	if left.Kind == kind {
		left.Children = append(left.Children, right)
		left.SourceRef = [2]int{left.SourceRef[0], right.SourceRef[1]}
		return left
	}
	return &Prop{Kind: kind, Children: []*Prop{left, right}, SourceRef: [2]int{off, right.SourceRef[1]}}
}

func (p *parser) parseAndProp() (*Prop, error) {
	left, err := p.parseNotProp()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		op := p.next()
		right, err := p.parseNotProp()
		if err != nil {
			return nil, err
		}
		left = mergeProp(PropAnd, left, right, op.off)
	}
	return left, nil
}

func (p *parser) parseNotProp() (*Prop, error) {
	if p.peek().kind == tNot {
		op := p.next()
		body, err := p.parseNotProp()
		if err != nil {
			return nil, err
		}
		return &Prop{Kind: PropNot, Children: []*Prop{body}, SourceRef: [2]int{op.off, body.SourceRef[1]}}, nil
	}
	return p.parseImplication()
}

func (p *parser) parseImplication() (*Prop, error) {
	if p.peek().kind == tLParen {
		open := p.next()
		inner, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen); err != nil {
			return nil, err
		}
		inner.SourceRef = [2]int{open.off, p.pos + 0}
		return p.maybeImplication(inner, true)
	}
	seq, start, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	prop := &Prop{Kind: PropSeq, Seq: seq, SourceRef: [2]int{start, p.peek().off}}
	return p.maybeImplication(prop, false)
}

// maybeImplication parses an optional antecedent `|->`/`|=>` consequent.
// When the head is a parenthesised property it can only act as an
// antecedent if it is a pure sequence property; otherwise the tokens stop.
func (p *parser) maybeImplication(head *Prop, parenthesized bool) (*Prop, error) {
	if p.peek().kind != tImplOv && p.peek().kind != tImplNo {
		return head, nil
	}
	op := p.next()
	ante := head.Seq
	if ante == nil {
		return nil, p.errf(op.off, "implication antecedent must be a sequence")
	}
	cons, err := p.parseProp()
	if err != nil {
		return nil, err
	}
	return &Prop{
		Kind:      map[bool]PropKind{true: PropImply, false: PropImplyNo}[op.kind == tImplOv],
		Overlap:   op.kind == tImplOv,
		Ante:      ante,
		Cons:      cons,
		SourceRef: [2]int{head.SourceRef[0], cons.SourceRef[1]},
	}, nil
}

// seq = seqOr
func (p *parser) parseSeq() (*Seq, int, error) {
	return p.parseSeqOr()
}

func (p *parser) parseSeqOr() (*Seq, int, error) {
	left, start, err := p.parseDelayed()
	if err != nil {
		return nil, 0, err
	}
	for p.peek().kind == tSeqOr {
		p.next()
		right, _, err := p.parseDelayed()
		if err != nil {
			return nil, 0, err
		}
		left = &Seq{Kind: SeqOr, Left: left, Right: right}
	}
	return left, start, nil
}

// delayed = (##[lo:hi] | ##N | ##[lo:$])* seqAtom
func (p *parser) parseDelayed() (*Seq, int, error) {
	start := p.peek().off
	var wrapped *Seq
	for p.peek().kind == tHash {
		hash := p.next()
		if _, err := p.expect(tLBrack); err != nil {
			return nil, 0, err
		}
		loTok, err := p.expect(tNum)
		if err != nil {
			return nil, 0, err
		}
		lo, _ := strconv.Atoi(loTok.text)
		hi := lo
		if _, ok := p.accept(tColon); ok {
			if p.peek().kind == tDollar {
				p.next()
				hi = Unbounded
			} else {
				hiTok, err := p.expect(tNum)
				if err != nil {
					return nil, 0, err
				}
				hi, _ = strconv.Atoi(hiTok.text)
			}
		}
		if _, err := p.expect(tRBrack); err != nil {
			return nil, 0, err
		}
		if hi != Unbounded && hi < lo {
			return nil, 0, &parseError{off: hash.off, msg: "delay range upper bound below lower bound"}
		}
		wrapped = &Seq{Kind: SeqDelay, Lo: lo, Hi: hi, Right: wrapped}
	}
	atom, err := p.parseSeqAtom()
	if err != nil {
		return nil, 0, err
	}
	if wrapped == nil {
		return atom, start, nil
	}
	// nested delays were built innermost-first in Right; attach the atom
	cur := wrapped
	for cur.Right != nil {
		cur = cur.Right
	}
	cur.Right = atom
	return wrapped, start, nil
}

func (p *parser) parseSeqAtom() (*Seq, error) {
	if p.peek().kind == tLParen {
		p.next()
		inner, _, err := p.parseSeqOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen); err != nil {
			return nil, err
		}
		return inner, nil
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &Seq{Kind: SeqAtom, Atom: expr}, nil
}

// expr = orE
func (p *parser) parseExpr() (*Expr, error) { return p.parseExprOr() }

func (p *parser) parseExprOr() (*Expr, error) {
	left, err := p.parseExprAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		p.next()
		right, err := p.parseExprAnd()
		if err != nil {
			return nil, err
		}
		left = mergeExpr(ExOr, left, right)
	}
	return left, nil
}

func mergeExpr(kind ExprKind, left, right *Expr) *Expr {
	if left.Kind == kind {
			left.Children = append(left.Children, right)
			return left
	}
	return &Expr{Kind: kind, Children: []*Expr{left, right}}
}

func (p *parser) parseExprAnd() (*Expr, error) {
	left, err := p.parseExprXor()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		p.next()
		right, err := p.parseExprXor()
		if err != nil {
			return nil, err
		}
		left = mergeExpr(ExAnd, left, right)
	}
	return left, nil
}

func (p *parser) parseExprXor() (*Expr, error) {
	left, err := p.parseExprUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tXor {
		p.next()
		right, err := p.parseExprUnary()
		if err != nil {
			return nil, err
		}
		left = mergeExpr(ExXor, left, right)
	}
	return left, nil
}

func (p *parser) parseExprUnary() (*Expr, error) {
	if p.peek().kind == tNot {
		p.next()
		body, err := p.parseExprUnary()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: ExNot, Children: []*Expr{body}}, nil
	}
	return p.parseExprPrimary()
}

func (p *parser) parseExprPrimary() (*Expr, error) {
	t := p.peek()
	switch t.kind {
	case tIdent:
		p.next()
		return &Expr{Kind: ExSignal, Name: t.text}, nil
	case tTrue:
		p.next()
		return &Expr{Kind: ExConst, Const: true}, nil
	case tFalse:
		p.next()
		return &Expr{Kind: ExConst, Const: false}, nil
	case tLParen:
		p.next()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tRParen); err != nil {
			return nil, err
		}
		return inner, nil
	default:
		return nil, p.errf(t.off, "expected signal expression, got %q", t.text)
	}
}
