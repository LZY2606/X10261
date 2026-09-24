package property

import (
	"fmt"
	"strings"
)

// Parse parses one property declaration. Errors carry source offsets so the
// UI can mark the exact fragment.
func Parse(src string) (Prop, error) {
	p := &parser{tokens: tokenize(src)}
	prop, err := p.parseProp()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, p.errf("trailing tokens after property: %q", p.peek().text)
	}
	return prop, nil
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNot
	tokAnd
	tokOr
	tokLParen
	tokRParen
	tokOverlap    // |->
	tokNonOverlap // |=>
	tokDisable
	tokIff
	tokEmpty
	tokDelay  // ##N or ##[lo:hi]
	tokRepeat // [*N]
)

type token struct {
	kind tokenKind
	text string
	lo   int // delay lo
	hi   int
	pos  int
}

func tokenize(src string) []token {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '!':
			toks = append(toks, token{kind: tokNot, text: "!", pos: i})
			i++
		case c == '(':
			toks = append(toks, token{kind: tokLParen, text: "(", pos: i})
			i++
		case c == ')':
			toks = append(toks, token{kind: tokRParen, text: ")", pos: i})
			i++
		case strings.HasPrefix(src[i:], "|->"):
			toks = append(toks, token{kind: tokOverlap, text: "|->", pos: i})
			i += 3
		case strings.HasPrefix(src[i:], "|=>"):
			toks = append(toks, token{kind: tokNonOverlap, text: "|=>", pos: i})
			i += 3
		case strings.HasPrefix(src[i:], "&&"):
			toks = append(toks, token{kind: tokAnd, text: "&&", pos: i})
			i += 2
		case strings.HasPrefix(src[i:], "||"):
			toks = append(toks, token{kind: tokOr, text: "||", pos: i})
			i += 2
		case strings.HasPrefix(src[i:], "##"):
			j := i + 2
			lo, hi, consumed, ok := readDelay(src, j)
			if !ok {
				toks = append(toks, token{kind: tokIdent, text: "##", pos: i})
				i += 2
				continue
			}
			toks = append(toks, token{kind: tokDelay, text: src[i : j+consumed], lo: lo, hi: hi, pos: i})
			i = j + consumed
		case c == '[':
			if strings.HasPrefix(src[i:], "[*") {
				j := i + 2
				n, k := readInt(src, j)
				end := strings.Index(src[j+k:], "]")
				if end < 0 {
					toks = append(toks, token{kind: tokIdent, text: "[", pos: i})
					i++
					continue
				}
				toks = append(toks, token{kind: tokRepeat, text: src[i : j+k+end+1], lo: n, hi: n, pos: i})
				i = j + k + end + 1
			} else {
				toks = append(toks, token{kind: tokIdent, text: "[", pos: i})
				i++
			}
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			switch word {
			case "disable":
				toks = append(toks, token{kind: tokDisable, text: word, pos: i})
			case "iff":
				toks = append(toks, token{kind: tokIff, text: word, pos: i})
			case "empty":
				toks = append(toks, token{kind: tokEmpty, text: word, pos: i})
			default:
				toks = append(toks, token{kind: tokIdent, text: word, pos: i})
			}
			i = j
		default:
			toks = append(toks, token{kind: tokIdent, text: string(c), pos: i})
			i++
		}
	}
	toks = append(toks, token{kind: tokEOF, pos: len(src)})
	return toks
}

func readDelay(src string, j int) (lo, hi, consumed int, ok bool) {
	if j < len(src) && src[j] == '[' {
		k := j + 1
		a, n1 := readInt(src, k)
		k += n1
		for k < len(src) && (src[k] == ' ' || src[k] == ':') {
			if src[k] == ':' {
				k++
				break
			}
			k++
		}
		b, n2 := readInt(src, k)
		k += n2
		for k < len(src) && src[k] == ' ' {
			k++
		}
		if k >= len(src) || src[k] != ']' {
			return 0, 0, 0, false
		}
		return a, b, k + 1 - j, true
	}
	n, used := readInt(src, j)
	return n, n, used, used > 0
}

func readInt(src string, j int) (int, int) {
	k := j
	for k < len(src) && src[k] == ' ' {
		k++
	}
	start := k
	for k < len(src) && src[k] >= '0' && src[k] <= '9' {
		k++
	}
	if start == k {
		return 0, 0
	}
	n := 0
	for _, c := range src[start:k] {
		n = n*10 + int(c-'0')
	}
	return n, k - j
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '\\'
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token { return p.tokens[p.pos] }
func (p *parser) next() token { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) errf(f string, a ...any) error {
	t := p.peek()
	return fmt.Errorf("col %d: %s", t.pos+1, fmt.Sprintf(f, a...))
}

func (p *parser) accept(k tokenKind) (token, bool) {
	if p.peek().kind == k {
		return p.next(), true
	}
	return token{}, false
}

func (p *parser) parseProp() (Prop, error) {
	if _, ok := p.accept(tokDisable); ok {
		if _, ok := p.accept(tokIff); !ok {
			return nil, p.errf("expected iff after disable")
		}
		if _, ok := p.accept(tokLParen); !ok {
			return nil, p.errf("expected ( after disable iff")
		}
		cond, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, ok := p.accept(tokRParen); !ok {
			return nil, p.errf("expected ) to close disable iff")
		}
		body, err := p.parseProp()
		if err != nil {
			return nil, err
		}
		return &DisableProp{Cond: cond, Body: body}, nil
	}
	seq, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind == tokOverlap || t.kind == tokNonOverlap {
		op := p.next()
		rhs, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		return &ImplyProp{
			Antecedent: seq,
			Op:         op.text,
			Consequent: rhs,
		}, nil
	}
	return &SeqProp{Seq: seq}, nil
}

// parseSeq parses sequences with ## and [*n] (left associative).
func (p *parser) parseSeq() (Seq, error) {
	left, err := p.parseSeqAtom()
	if err != nil {
		return nil, err
	}
	for {
		if t := p.peek(); t.kind == tokDelay {
			d := p.next()
			right, err := p.parseSeqAtom()
			if err != nil {
				return nil, err
			}
			left = &DelaySeq{Prefix: left, Lo: d.lo, Hi: d.hi, Suffix: right, Src: d.text}
			continue
		}
		if t := p.peek(); t.kind == tokRepeat {
			r := p.next()
			left = &RepeatSeq{Inner: left, Count: r.lo, Src: r.text}
			continue
		}
		break
	}
	return left, nil
}

func (p *parser) parseSeqAtom() (Seq, error) {
	if _, ok := p.accept(tokEmpty); ok {
		return &EmptySeq{Src: "empty"}, nil
	}
	if _, ok := p.accept(tokLParen); ok {
		// Could be a parenthesized expression or sequence; try expression
		// first (properties have no tuple syntax, so ) disambiguates).
		save := p.pos
		e, err := p.parseExpr()
		if err == nil {
			if _, ok := p.accept(tokRParen); ok {
				return &ExprSeq{Expr: e}, nil
			}
		}
		p.pos = save
		s, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		if _, ok := p.accept(tokRParen); !ok {
			return nil, p.errf("expected ) to close sequence")
		}
		return s, nil
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	return &ExprSeq{Expr: e}, nil
}

// parseExpr handles || < && < unary ! (no precedence surprise: ! binds first).
func (p *parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (Expr, error) {
	lhs, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.accept(tokOr); !ok {
			return lhs, nil
		}
		rhs, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		lhs = &BinExpr{Op: "||", Lhs: lhs, Rhs: rhs}
	}
}

func (p *parser) parseAnd() (Expr, error) {
	lhs, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.accept(tokAnd); !ok {
			return lhs, nil
		}
		rhs, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		lhs = &BinExpr{Op: "&&", Lhs: lhs, Rhs: rhs}
	}
}

func (p *parser) parseUnary() (Expr, error) {
	if _, ok := p.accept(tokNot); ok {
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &NotExpr{Inner: inner}, nil
	}
	if _, ok := p.accept(tokLParen); ok {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, ok := p.accept(tokRParen); !ok {
			return nil, p.errf("expected ) in expression")
		}
		return e, nil
	}
	t, ok := p.accept(tokIdent)
	if !ok {
		return nil, p.errf("expected signal name")
	}
	return &IdentExpr{Name: t.text, Src: t.text}, nil
}
