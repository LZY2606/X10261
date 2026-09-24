package proplang

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNum
	tBinLit
	tPunct
	tAt
)

type token struct {
	kind tokenKind
	text string
	pos  int
	end  int
}

var keywords = map[string]bool{
	"property": true, "endproperty": true, "@": false,
	"posedge": true, "negedge": true, "or": true, "and": true,
	"not": true, "disable": true, "iff": true,
	"overlapped": true, "nonoverlapped": true, "true": true, "false": true,
}

type lexer struct {
	src string
	pos int
}

func (l *lexer) skip() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			l.pos++
		} else if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		} else if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			l.pos += 2
			for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
				l.pos++
			}
			l.pos += 2
		} else {
			return
		}
	}
}

func (l *lexer) next() (token, error) {
	l.skip()
	if l.pos >= len(l.src) {
		return token{kind: tEOF, pos: l.pos, end: l.pos}, nil
	}
	start := l.pos
	c := l.src[l.pos]
	if c == '@' {
		l.pos++
		return token{kind: tAt, text: "@", pos: start, end: l.pos}, nil
	}
	if c == '\'' && l.pos+1 < len(l.src) {
		// 'b01xz / 'h / 'd literal
		l.pos++
		s := l.pos
		for l.pos < len(l.src) && (isWordByte(l.src[l.pos]) || unicode.IsDigit(rune(l.src[l.pos]))) {
			l.pos++
		}
		return token{kind: tBinLit, text: "'" + l.src[s:l.pos], pos: start, end: l.pos}, nil
	}
	if isIdentStart(c) {
		for l.pos < len(l.src) && (isWordByte(l.src[l.pos]) || l.src[l.pos] == '.' || l.src[l.pos] == '[') {
			if l.src[l.pos] == '[' {
				for l.pos < len(l.src) && l.src[l.pos] != ']' {
					l.pos++
				}
				if l.pos < len(l.src) {
					l.pos++
				}
				continue
			}
			l.pos++
		}
		return token{kind: tIdent, text: l.src[start:l.pos], pos: start, end: l.pos}, nil
	}
	if unicode.IsDigit(rune(c)) {
		for l.pos < len(l.src) && unicode.IsDigit(rune(l.src[l.pos])) {
			l.pos++
		}
		return token{kind: tNum, text: l.src[start:l.pos], pos: start, end: l.pos}, nil
	}
	// two char punctuation
	two := ""
	if l.pos+2 <= len(l.src) {
		two = l.src[l.pos : l.pos+2]
	}
	switch two {
	case "|->", "|=>", "==", "!=":
		l.pos += 2
		return token{kind: tPunct, text: two, pos: start, end: l.pos}, nil
	}
	switch c {
	case '(', ')', '[', ']', '*', '#', '&', '|', '!', ';', ',', ':', '.':
		l.pos++
		return token{kind: tPunct, text: string(c), pos: start, end: l.pos}, nil
	}
	return token{}, fmt.Errorf("lex: unexpected character %q at %d", c, start)
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("01xXzZ", rune(c))
}

func lexAll(src string) ([]token, error) {
	l := &lexer{src: src}
	var toks []token
	for {
		tk, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tk)
		if tk.kind == tEOF {
			return toks, nil
		}
	}
}
