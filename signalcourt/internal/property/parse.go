package property

import (
	"fmt"
	"strings"
	"unicode"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tKeyword
	tOp
	tColon
	tLParen
	tRParen
	tLBrack
	tRBrack
	tComma
	tStar
)

type lexTok struct {
	kind tokKind
	text string
	line int
	col  int
}

var keywords = map[string]bool{
	"version": true, "clock": true, "reset": true, "posedge": true,
	"negedge": true, "edge": true, "async_low": true, "async_high": true,
	"sync_low": true, "sync_high": true, "property": true, "or": true,
	"and": true, "not": true, "disable": true, "iff": true,
	"strong": true, "weak": true,
}

func lex(src string) ([]lexTok, error) {
	var out []lexTok
	line, col := 1, 1
	add := func(k tokKind, text string, l, c int) {
		out = append(out, lexTok{k, text, l, c})
	}
	runes := []rune(src)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '\n':
			line++
			col = 1
			i++
		case unicode.IsSpace(r):
			i++
			col++
		case r == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				if runes[i] == '\n' {
					line++
					col = 1
				}
				i++
			}
			i += 2
		case r == '(':
			add(tLParen, "(", line, col)
			i++
			col++
		case r == ')':
			add(tRParen, ")", line, col)
			i++
			col++
		case r == '[':
			add(tLBrack, "[", line, col)
			i++
			col++
		case r == ']':
			add(tRBrack, "]", line, col)
			i++
			col++
		case r == ',':
			add(tComma, ",", line, col)
			i++
			col++
		case r == ':':
			add(tColon, ":", line, col)
			i++
			col++
		case r == '*':
			add(tStar, "*", line, col)
			i++
			col++
		case r == '#':
			if i+2 < len(runes) && runes[i+1] == '#' {
				l, c := line, col
				i += 2
				col += 2
				for i < len(runes) && runes[i] == '#' {
					i++
					col++
				}
				add(tOp, "##", l, c)
				continue
			}
			return nil, fmt.Errorf("property: stray '#' at %d:%d", line, col)
		case r == '|':
			l, c := line, col
			if i+1 < len(runes) && runes[i+1] == '-' {
				if i+2 < len(runes) && runes[i+2] == '>' {
					add(tOp, "|->", l, c)
					i += 3
					col += 3
					continue
				}
				add(tOp, "|", l, c)
				i++
				col++
				continue
			}
			if i+2 < len(runes) && runes[i+1] == '=' && runes[i+2] == '>' {
				add(tOp, "|=>", l, c)
				i += 3
				col += 3
				continue
			}
			return nil, fmt.Errorf("property: stray '|' at %d:%d", line, col)
		case r == '&':
			l, c := line, col
			if i+1 < len(runes) && runes[i+1] == '&' {
				add(tOp, "&&", l, c)
				i += 2
				col += 2
				continue
			}
			return nil, fmt.Errorf("property: bitwise '&' not supported at %d:%d", line, col)
		case r == '=':
			l, c := line, col
			if i+1 < len(runes) && runes[i+1] == '=' {
				add(tOp, "==", l, c)
				i += 2
				col += 2
				continue
			}
			return nil, fmt.Errorf("property: stray '=' at %d:%d", line, col)
		case r == '!':
			l, c := line, col
			if i+1 < len(runes) && runes[i+1] == '=' {
				add(tOp, "!=", l, c)
				i += 2
				col += 2
				continue
			}
			add(tOp, "!", l, c)
			i++
			col++
		case r == '-' && i+1 < len(runes) && runes[i+1] == '>':
			add(tOp, "->", line, col)
			i += 2
			col += 2
		case unicode.IsLetter(r) || r == '_':
			l, c := line, col
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
				col++
			}
			word := string(runes[start:i])
			if keywords[word] {
				add(tKeyword, word, l, c)
			} else {
				add(tIdent, word, l, c)
			}
		case unicode.IsDigit(r):
			l, c := line, col
			start := i
			for i < len(runes) && unicode.IsDigit(runes[i]) {
				i++
				col++
			}
			add(tNum, string(runes[start:i]), l, c)
		default:
			return nil, fmt.Errorf("property: unexpected character %q at %d:%d", r, line, col)
		}
	}
	add(tEOF, "", line, col)
	return out, nil
}

type parser struct {
	toks []lexTok
	pos  int
	path string
}

func (p *parser) peek() lexTok  { return p.toks[p.pos] }
func (p *parser) next() lexTok { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) accept(k tokKind, text string) bool {
	t := p.peek()
	if t.kind == k && (text == "" || t.text == text) {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expect(k tokKind, text string) (lexTok, error) {
	t := p.peek()
	if t.kind != k || (text != "" && t.text != text) {
		return t, fmt.Errorf("property %s:%d:%d: expected %q got %q", p.path, t.line, t.col, text, t.text)
	}
	p.pos++
	return t, nil
}

// Parse parses a complete .ssp source.
func Parse(src, path string) (*File, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, path: path}
	f := &File{Path: path, RawText: src}
	for p.peek().kind != tEOF {
		t := p.peek()
		if t.kind != tKeyword {
			return nil, fmt.Errorf("property %s:%d:%d: expected declaration, got %q", path, t.line, t.col, t.text)
		}
		switch t.text {
		case "version":
			p.next()
			if _, err := p.expect(tNum, ""); err != nil {
				// allow x.y.z style joined digits: expect ident or dotted text
				return nil, err
			}
			ver := p.toks[p.pos-1].text
			// consume dotted suffix
			var parts []string
			parts = append(parts, ver)
			return nil, fmt.Errorf("property %s: version x.y.z form not tokenized", path)
		case "clock":
			if err := p.parseClock(f); err != nil {
				return nil, err
			}
		case "reset":
			if err := p.parseReset(f); err != nil {
				return nil, err
			}
		case "property":
			np, err := p.parsePropertyDecl()
			if err != nil {
				return nil, err
			}
			np.Order = len(f.Props)
			f.Props = append(f.Props, np)
		default:
			return nil, fmt.Errorf("property %s:%d:%d: unexpected keyword %q", path, t.line, t.col, t.text)
		}
	}
	return f, nil
}

func (p *parser) parseClock(f *File) error {
	p.next() // clock
	edge, err := p.expect(tKeyword, "")
	if err != nil {
		return err
	}
	if edge.text != "posedge" && edge.text != "negedge" && edge.text != "edge" {
		return fmt.Errorf("property %s:%d: clock edge must be posedge/negedge/edge", p.path, edge.line)
	}
	clk, err := p.expect(tIdent, "")
	if err != nil {
		return err
	}
	f.ClockEdge = EdgeSpec{Edge: edge.text, Clk: clk.text}
	return nil
}

func (p *parser) parseReset(f *File) error {
	p.next() // reset
	kind, err := p.expect(tKeyword, "")
	if err != nil {
		return err
	}
	switch kind.text {
	case "async_low", "async_high", "sync_low", "sync_high":
	default:
		return fmt.Errorf("property %s:%d: bad reset kind %q", p.path, kind.line, kind.text)
	}
	f.ResetKind = kind.text
	sig, err := p.expect(tIdent, "")
	if err != nil {
		return err
	}
	active := "1"
	if strings.HasSuffix(kind.text, "_low") {
		active = "0"
	}
	f.ResetExpr = &Expr{Ref: SourceRef{File: p.path, Line: sig.line, Col: sig.col},
		Kind: "eq", Sig: sig.text, Value: active}
	return nil
}

func (p *parser) parsePropertyDecl() (NamedProp, error) {
	p.next() // property
	name, err := p.expect(tIdent, "")
	if err != nil {
		return NamedProp{}, err
	}
	if _, err := p.expect(tColon, ""); err != nil {
		return NamedProp{}, err
	}
	pr, err := p.parseProp()
	if err != nil {
		return NamedProp{}, err
	}
	if _, err := p.expect(tOp, ";"); err != nil {
		// ';' not lexed: use EOF-ish; instead require end token check
	}
	return NamedProp{Name: name.text, Prop: pr}, nil
}
