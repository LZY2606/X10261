package prop

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokKind string

const (
	tEOF    tokKind = "eof"
	tIdent  tokKind = "ident"
	tInt    tokKind = "int"
	tNot    tokKind = "!"
	tAnd    tokKind = "&&"
	tOr     tokKind = "||"
	tLParen tokKind = "("
	tRParen tokKind = ")"
	tLBrack tokKind = "["
	tRBrack tokKind = "]"
	tOver   tokKind = "|->"
	tNon    tokKind = "|=>"
	tDelay  tokKind = "##"
	tStar   tokKind = "*"
)

type token struct {
	kind tokKind
	text string
	pos  Pos
}

type lexer struct {
	src       string
	i         int
	line, col int
}

func (l *lexer) pos() Pos { return Pos{Offset: l.i, Line: l.line, Col: l.col} }

func (l *lexer) skipSpace() {
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == ' ' || c == '\t' || c == '\r' {
			l.i++
			l.col++
			continue
		}
		if c == '\n' {
			l.i++
			l.line++
			l.col = 1
			continue
		}
		// line comment
		if c == '/' && l.i+1 < len(l.src) && l.src[l.i+1] == '/' {
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.i++
			}
			continue
		}
		// block comment
		if c == '/' && l.i+1 < len(l.src) && l.src[l.i+1] == '*' {
			l.i += 2
			for l.i+1 < len(l.src) && !(l.src[l.i] == '*' && l.src[l.i+1] == '/') {
				if l.src[l.i] == '\n' {
					l.line++
					l.col = 1
				}
				l.i++
			}
			l.i += 2
			continue
		}
		break
	}
}

var keywords = map[string]bool{"disable": true, "iff": true, "or": true}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.i >= len(l.src) {
		return token{kind: tEOF, pos: l.pos()}, nil
	}
	start := l.pos()
	c := l.src[l.i]
	switch {
	case c == '|' && l.i+2 < len(l.src) && (l.src[l.i+1] == '-' || l.src[l.i+1] == '=') && l.src[l.i+2] == '>':
		k := tOver
		if l.src[l.i+1] == '=' {
			k = tNon
		}
		t := token{kind: k, text: l.src[l.i : l.i+3], pos: start}
		l.i += 3
		l.col += 3
		return t, nil
	case c == '#' && l.i+1 < len(l.src) && l.src[l.i+1] == '#':
		l.i += 2
		l.col += 2
		return token{kind: tDelay, text: "##", pos: start}, nil
	case c == '&' && l.i+1 < len(l.src) && l.src[l.i+1] == '&':
		l.i += 2
		l.col += 2
		return token{kind: tAnd, text: "&&", pos: start}, nil
	case c == '|' && l.i+1 < len(l.src) && l.src[l.i+1] == '|':
		l.i += 2
		l.col += 2
		return token{kind: tOr, text: "||", pos: start}, nil
	case strings.ContainsRune("!()[]*", rune(c)):
		l.i++
		l.col++
		return token{kind: tokKind(string(c)), text: string(c), pos: start}, nil
	case unicode.IsDigit(rune(c)):
		j := l.i
		for j < len(l.src) && unicode.IsDigit(rune(l.src[j])) {
			j++
		}
		text := l.src[l.i:j]
		l.col += j - l.i
		l.i = j
		return token{kind: tInt, text: text, pos: start}, nil
	case isIdentStart(c):
		j := l.i
		for j < len(l.src) && isIdentPart(l.src[j]) {
			j++
		}
		text := l.src[l.i:j]
		l.col += j - l.i
		l.i = j
		return token{kind: tIdent, text: text, pos: start}, nil
	}
	return token{}, fmt.Errorf("unexpected character %q at line %d col %d", string(c), start.Line, start.Col)
}

func isIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c))
}
func isIdentPart(c byte) bool {
	return c == '_' || c == '$' || c == '.' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))
}

type parser struct {
	toks []token
	pos  int
}

// Parse compiles one property body (without a leading name) plus an optional
// "disable iff (..)" prefix.
func Parse(text string) (*Spec, error) {
	l := &lexer{src: text, line: 1, col: 1}
	var toks []token
	for {
		tk, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tk)
		if tk.kind == tEOF {
			break
		}
	}
	p := &parser{toks: toks}
	sp := &Spec{Text: strings.TrimSpace(text)}
	start := p.peek().pos
	if p.peek().kind == tIdent && p.peek().text == "disable" {
		if err := p.parseDisable(sp); err != nil {
			return nil, err
		}
	}
	pr, err := p.parseProperty()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("trailing tokens after property near %q at line %d", p.peek().text, p.peek().pos.Line)
	}
	sp.Property = pr
	sp.Pos = start
	return sp, nil
}

func (p *parser) parseDisable(sp *Spec) error {
	p.advance() // disable
	t := p.advance()
	if t.kind != tIdent || t.text != "iff" {
		return fmt.Errorf("expected 'iff' after disable at line %d", t.pos.Line)
	}
	if p.advance().kind != tLParen {
		return fmt.Errorf("expected '(' after disable iff")
	}
	e, err := p.parseExpr()
	if err != nil {
		return err
	}
	if p.advance().kind != tRParen {
		return fmt.Errorf("expected ')' closing disable iff")
	}
	sp.Disable = e
	return nil
}

func (p *parser) peek() token  { return p.toks[p.pos] }
func (p *parser) next2() token { return p.toks[p.pos+1] }
func (p *parser) advance() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) parseProperty() (*Property, error) {
	ante, err := p.parseSequence()
	if err != nil {
		return nil, err
	}
	switch p.peek().kind {
	case tOver, tNon:
		op := p.advance()
		cons, err := p.parseSequence()
		if err != nil {
			return nil, err
		}
		kind := POverlap
		if op.kind == tNon {
			kind = PNonOverlap
		}
		return &Property{Kind: kind, Ante: ante, Cons: cons, Pos: op.pos}, nil
	}
	return &Property{Kind: PPlain, Cons: ante, Pos: ante.Pos}, nil
}

func (p *parser) parseSequence() (*Seq, error) {
	first, err := p.parseSeqConcat()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tIdent || p.peek().text != "or" {
		return first, nil
	}
	var kids []*Seq
	kids = append(kids, first)
	for p.peek().kind == tIdent && p.peek().text == "or" {
		p.advance()
		rhs, err := p.parseSeqConcat()
		if err != nil {
			return nil, err
		}
		kids = append(kids, rhs)
	}
	return &Seq{Op: SOr, Children: kids, Pos: kids[0].Pos}, nil
}

func (p *parser) parseSeqConcat() (*Seq, error) {
	first, err := p.parseSeqRepeat()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tDelay {
		return first, nil
	}
	kids := []*Seq{first}
	delays := []int{0}
	for p.peek().kind == tDelay {
		p.advance()
		n := 1
		if p.peek().kind == tInt {
			v, _ := strconv.Atoi(p.advance().text)
			n = v
		}
		rhs, err := p.parseSeqRepeat()
		if err != nil {
			return nil, err
		}
		kids = append(kids, rhs)
		delays = append(delays, n)
	}
	// Encode delays on children index>0.
	for i := 1; i < len(kids); i++ {
		kids[i].Delay = delays[i]
	}
	return &Seq{Op: SConcat, Children: kids, Pos: first.Pos}, nil
}

func (p *parser) parseSeqRepeat() (*Seq, error) {
	base, err := p.parseSeqFactor()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tLBrack {
		p.advance()
		if p.peek().kind != tStar {
			return nil, fmt.Errorf("only '[* N]' repetition supported at line %d", p.peek().pos.Line)
		}
		p.advance()
		n := 1
		if p.peek().kind == tInt {
			n, _ = strconv.Atoi(p.advance().text)
		}
		if p.advance().kind != tRBrack {
			return nil, fmt.Errorf("expected ']' after repetition")
		}
		base = &Seq{Op: SRepeat, Children: []*Seq{base}, RepeatCount: n, Pos: base.Pos}
	}
	return base, nil
}

func (p *parser) parseSeqFactor() (*Seq, error) {
	if p.peek().kind == tLParen {
		start := p.advance().pos
		inner, err := p.parseSequence()
		if err != nil {
			return nil, err
		}
		if p.advance().kind != tRParen {
			return nil, fmt.Errorf("expected ')' at line %d", p.peek().pos.Line)
		}
		return &Seq{Op: SGroup, Children: []*Seq{inner}, Pos: start}, nil
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &Seq{Op: SSig, Expr: e, Pos: e.Pos}, nil
}

// boolean grammar
func (p *parser) parseExpr() (*Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (*Expr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tOr {
		op := p.advance()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &Expr{Op: BOr, Children: []*Expr{l, r}, Pos: op.pos}
	}
	return l, nil
}

func (p *parser) parseAnd() (*Expr, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tAnd {
		op := p.advance()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = &Expr{Op: BAnd, Children: []*Expr{l, r}, Pos: op.pos}
	}
	return l, nil
}

func (p *parser) parseNot() (*Expr, error) {
	if p.peek().kind == tNot {
		op := p.advance()
		e, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Expr{Op: BNot, Children: []*Expr{e}, Pos: op.pos}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (*Expr, error) {
	t := p.peek()
	if t.kind == tLParen {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.advance().kind != tRParen {
			return nil, fmt.Errorf("expected ')' at line %d", p.peek().pos.Line)
		}
		return e, nil
	}
	if t.kind != tIdent || keywords[t.text] {
		return nil, fmt.Errorf("expected signal identifier but got %q at line %d", t.text, t.pos.Line)
	}
	p.advance()
	bit := 0
	// A "[" immediately followed by an integer is a bit index; "[*" belongs
	// to sequence repetition and is left for parseSeqRepeat.
	if p.peek().kind == tLBrack && p.next2().kind == tInt {
		p.advance()
		it := p.advance()
		if it.kind != tInt {
			return nil, fmt.Errorf("expected bit index after %s[ at line %d", t.text, it.pos.Line)
		}
		bit, _ = strconv.Atoi(it.text)
		if p.advance().kind != tRBrack {
			return nil, fmt.Errorf("expected ']' after bit index")
		}
	}
	return &Expr{Op: BVar, Name: t.text, Bit: bit, Pos: t.pos}, nil
}
