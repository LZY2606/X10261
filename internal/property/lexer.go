package property

import (
	"fmt"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNumber
	tPunct
)

type token struct {
	kind tokKind
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	var toks []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '|' && i+2 < len(src) && (src[i+1] == '-' && src[i+2] == '>' || src[i+1] == '=' && src[i+2] == '>'):
			toks = append(toks, token{tPunct, src[i : i+3], i})
			i += 3
		case c == '|' && i+1 < len(src) && src[i+1] == '|':
			toks = append(toks, token{tPunct, "||", i})
			i += 2
		case strings.ContainsRune("()[]:;.,*!&@#", rune(c)):
			if c == '&' && i+1 < len(src) && src[i+1] == '&' {
				toks = append(toks, token{tPunct, "&&", i})
				i += 2
				continue
			}
			toks = append(toks, token{tPunct, string(c), i})
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			toks = append(toks, token{tNumber, src[i:j], i})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			toks = append(toks, token{tIdent, src[i:j], i})
			i = j
		default:
			return nil, fmt.Errorf("illegal character %q at offset %d", string(c), i)
		}
	}
	toks = append(toks, token{tEOF, "", len(src)})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '\\' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) next() token {
	t := p.toks[p.pos]
	p.pos++
	return t
}
func (p *parser) acceptPunct(s string) bool {
	if t := p.peek(); t.kind == tPunct && t.text == s {
		p.pos++
		return true
	}
	return false
}
func (p *parser) acceptWord(s string) bool {
	if t := p.peek(); t.kind == tIdent && t.text == s {
		p.pos++
		return true
	}
	return false
}
func (p *parser) expectPunct(s string) error {
	if !p.acceptPunct(s) {
		t := p.peek()
		return fmt.Errorf("expected %q but got %q at offset %d", s, t.text, t.pos)
	}
	return nil
}
func (p *parser) expectWord(s string) error {
	if !p.acceptWord(s) {
		t := p.peek()
		return fmt.Errorf("expected keyword %q but got %q at offset %d", s, t.text, t.pos)
	}
	return nil
}
