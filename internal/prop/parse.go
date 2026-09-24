package prop

import (
	"fmt"
	"strconv"
	"strings"
)

type tok struct {
	kind string // ident, num, const, sym, eof
	text string
}

func lex(src string) ([]tok, error) {
	var toks []tok
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] == '.' ||
				(src[j] >= 'a' && src[j] <= 'z') || (src[j] >= 'A' && src[j] <= 'Z') ||
				(src[j] >= '0' && src[j] <= '9')) {
				j++
			}
			toks = append(toks, tok{"ident", src[i:j]})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			// constant literal like 1'b1 / 1'bx
			if j < len(src) && src[j] == '\'' {
				k := j + 1
				for k < len(src) && strings.ContainsRune("bB01xXzZ?", rune(src[k])) {
					k++
				}
				toks = append(toks, tok{"const", src[i:k]})
				i = k
			} else {
				toks = append(toks, tok{"num", src[i:j]})
				i = j
			}
		default:
			// multi-char symbols
			for _, sym := range []string{"|->", "|=>", "##", "[*", "==", "&&", "[", "]", ":", "$", "(", ")", ";", "!"} {
				if strings.HasPrefix(src[i:], sym) {
					toks = append(toks, tok{"sym", sym})
					i += len(sym)
					goto next
				}
			}
			return nil, fmt.Errorf("unexpected character %q at offset %d", string(c), i)
		}
	next:
	}
	toks = append(toks, tok{"eof", ""})
	return toks, nil
}

type parser struct {
	toks []tok
	pos  int
}

func (p *parser) peek() tok { return p.toks[p.pos] }
func (p *parser) next() tok { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) expect(sym string) error {
	t := p.next()
	if t.text != sym {
		return fmt.Errorf("expected %q, got %q", sym, t.text)
	}
	return nil
}

// ParseFile parses all property declarations in a source text.
func ParseFile(src string) ([]*Property, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var props []*Property
	for p.peek().kind != "eof" {
		t := p.next()
		if t.kind == "ident" && t.text == "property" {
			prop, err := p.parseProperty(src)
			if err != nil {
				return nil, err
			}
			props = append(props, prop)
		} else {
			return nil, fmt.Errorf("expected 'property', got %q", t.text)
		}
	}
	return props, nil
}

func (p *parser) parseProperty(src string) (*Property, error) {
	name := p.next()
	if name.kind != "ident" {
		return nil, fmt.Errorf("expected property name, got %q", name.text)
	}
	if err := p.expect(":"); err != nil {
		return nil, err
	}
	pr := &Property{Name: name.text, Source: src}
	if p.peek().text == "disable" {
		p.next()
		if err := p.expect("iff"); err != nil {
			return nil, err
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		b, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		pr.Disable = b
	}
	ante, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	pr.Ante = ante
	if p.peek().text == "|->" || p.peek().text == "|=>" {
		pr.Op = p.next().text
		cons, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		pr.Cons = cons
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	return pr, nil
}

// parseSeq parses: orSeq := andSeq ("or" andSeq)*  with ## as binary op.
func (p *parser) parseSeq() (*Seq, error) {
	left, err := p.parseSeqAtom()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		switch t.text {
		case "##":
			p.next()
			min, max, err := p.parseDelayRange()
			if err != nil {
				return nil, err
			}
			right, err := p.parseSeqAtom()
			if err != nil {
				return nil, err
			}
			left = &Seq{Kind: "delay", Left: left, Right: right, Min: min, Max: max}
		case "[*":
			p.next()
			min := p.next()
			if min.kind != "num" {
				return nil, fmt.Errorf("expected repetition count, got %q", min.text)
			}
			lo, _ := strconv.Atoi(min.text)
			hi := lo
			if p.peek().text == ":" {
				p.next()
				if p.peek().text == "$" {
					p.next()
					hi = -1
				} else {
					hn := p.next()
					if hn.kind != "num" {
						return nil, fmt.Errorf("expected repetition max, got %q", hn.text)
					}
					hi, _ = strconv.Atoi(hn.text)
				}
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			left = &Seq{Kind: "rep", Left: left, Min: lo, Max: hi}
		case "or":
			p.next()
			right, err := p.parseSeqAtom()
			if err != nil {
				return nil, err
			}
			left = &Seq{Kind: "or", Left: left, Right: right}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseDelayRange() (int, int, error) {
	t := p.next()
	if t.kind == "num" {
		n, _ := strconv.Atoi(t.text)
		return n, n, nil
	}
	if t.text != "[" {
		return 0, 0, fmt.Errorf("expected delay number or range, got %q", t.text)
	}
	lo := p.next()
	if lo.kind != "num" {
		return 0, 0, fmt.Errorf("expected delay min, got %q", lo.text)
	}
	loN, _ := strconv.Atoi(lo.text)
	if err := p.expect(":"); err != nil {
		return 0, 0, err
	}
	hi := loN
	if p.peek().text == "$" {
		p.next()
		hi = -1
	} else {
		hn := p.next()
		if hn.kind != "num" {
			return 0, 0, fmt.Errorf("expected delay max, got %q", hn.text)
		}
		hi, _ = strconv.Atoi(hn.text)
	}
	if err := p.expect("]"); err != nil {
		return 0, 0, err
	}
	return loN, hi, nil
}

func (p *parser) parseSeqAtom() (*Seq, error) {
	t := p.peek()
	if t.text == "(" {
		p.next()
		s, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return s, nil
	}
	b, err := p.parseBool()
	if err != nil {
		return nil, err
	}
	return &Seq{Kind: "bool", Bool: b}, nil
}

func (p *parser) parseBool() (*BoolExpr, error) {
	t := p.next()
	switch {
	case t.text == "!":
		inner, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		return &BoolExpr{Op: "not", Left: inner}, nil
	case t.text == "(":
		b, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		if p.peek().text == "&&" {
			p.next()
			r, err := p.parseBool()
			if err != nil {
				return nil, err
			}
			return &BoolExpr{Op: "and", Left: b, Right: r}, nil
		}
		return b, nil
	case t.kind == "ident":
		if p.peek().text == "==" {
			p.next()
			c := p.next()
			if c.kind != "const" {
				return nil, fmt.Errorf("expected 1'bX constant, got %q", c.text)
			}
			val := constValue(c.text)
			if val == "" {
				return nil, fmt.Errorf("unsupported constant %q", c.text)
			}
			return &BoolExpr{Op: "eq", Sig: t.text, Const: val}, nil
		}
		if p.peek().text == "&&" {
			p.next()
			r, err := p.parseBool()
			if err != nil {
				return nil, err
			}
			return &BoolExpr{Op: "and", Left: &BoolExpr{Op: "sig", Sig: t.text}, Right: r}, nil
		}
		return &BoolExpr{Op: "sig", Sig: t.text}, nil
	}
	return nil, fmt.Errorf("unexpected token %q in boolean expression", t.text)
}

// constValue extracts the bit character from a literal like 1'b1.
func constValue(lit string) string {
	i := strings.Index(lit, "'")
	if i < 0 || i+2 >= len(lit) {
		return ""
	}
	v := strings.ToLower(string(lit[i+2]))
	if v == "?" {
		v = "z"
	}
	switch v {
	case "0", "1", "x", "z":
		return v
	}
	return ""
}
