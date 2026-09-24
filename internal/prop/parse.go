// Package prop implements the small SVA-style sequence/property language:
//
//	property name @clock (disable iff (sig == level)) boolean ##n boolean;
//	boolean        := primary {&&,||} primary, !primary, parentheses
//	sequence       := boolean {##[min:max|n|*|num] boolean}
//	implication    := sequence (|-> | |=>) sequence
//	prefix "empty:" marks a sequence allowed to match vacuously.
package prop

import (
	"fmt"
	"strconv"
	"strings"

	"gsb/internal/domain"
)

type File struct {
	Properties []*domain.Property
}

type lexer struct {
	src  string
	pos  int
	line int
}

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNum
	tAnd
	tOr
	tNot
	tImplO
	tImplN
	tDelay
	tLParen
	tRParen
	tSemi
	tAt
	tEq
	tComma
	tColon
)

type token struct {
	kind tokenKind
	text string
	line int
}

func (l *lexer) skip() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			if c == '\n' {
				l.line++
			}
			l.pos++
			continue
		}
		if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			l.pos += 2
			for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
				if l.src[l.pos] == '\n' {
					l.line++
				}
				l.pos++
			}
			l.pos += 2
			continue
		}
		return
	}
}

func (l *lexer) next() token {
	l.skip()
	if l.pos >= len(l.src) {
		return token{kind: tEOF, line: l.line}
	}
	c := l.src[l.pos]
	line := l.line
	switch {
	case c == '&' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '&':
		l.pos += 2
		return token{kind: tAnd, text: "&&", line: line}
	case c == '|' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '-':
		l.pos += 2
		return token{kind: tImplO, text: "|->", line: line}
	case c == '|' && l.pos+2 < len(l.src) && l.src[l.pos+1] == '=' && l.src[l.pos+2] == '>':
		l.pos += 3
		return token{kind: tImplN, text: "|=>", line: line}
	case c == '|' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '|':
		l.pos += 2
		return token{kind: tOr, text: "||", line: line}
	case c == '#':
		start := l.pos
		l.pos++
		for l.pos < len(l.src) && l.src[l.pos] != ' ' && l.src[l.pos] != '\t' &&
			l.src[l.pos] != '\n' && l.src[l.pos] != '\r' && l.src[l.pos] != '(' {
			l.pos++
		}
		return token{kind: tDelay, text: l.src[start:l.pos], line: line}
	case c == '(':
		l.pos++
		return token{kind: tLParen, text: "(", line: line}
	case c == ')':
		l.pos++
		return token{kind: tRParen, text: ")", line: line}
	case c == ';':
		l.pos++
		return token{kind: tSemi, text: ";", line: line}
	case c == '@':
		l.pos++
		return token{kind: tAt, text: "@", line: line}
	case c == ',':
		l.pos++
		return token{kind: tComma, text: ",", line: line}
	case c == ':':
		l.pos++
		return token{kind: tColon, text: ":", line: line}
	case c == '=':
		l.pos++
		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.pos++
			return token{kind: tEq, text: "==", line: line}
		}
		return token{kind: tEq, text: "=", line: line}
	case c == '!':
		l.pos++
		return token{kind: tNot, text: "!", line: line}
	case c >= '0' && c <= '9':
		start := l.pos
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		return token{kind: tNum, text: l.src[start:l.pos], line: line}
	case isIdentStart(c):
		start := l.pos
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		return token{kind: tIdent, text: l.src[start:l.pos], line: line}
	}
	return token{kind: tEOF, text: string(c), line: line}
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

type parser struct {
	src string
	lx  *lexer
	tok token
}

// ParseFile parses one or more property declarations.
func ParseFile(src string) (*File, error) {
	p := &parser{src: src, lx: &lexer{src: src, line: 1}}
	p.advance()
	f := &File{}
	for p.tok.kind != tEOF {
		pr, err := p.parseProperty()
		if err != nil {
			return nil, err
		}
		f.Properties = append(f.Properties, pr)
	}
	return f, nil
}

func (p *parser) advance() { p.tok = p.lx.next() }

func (p *parser) parseProperty() (*domain.Property, error) {
	if p.tok.kind != tIdent || p.tok.text != "property" {
		return nil, p.err("expected 'property'")
	}
	p.advance()
	if p.tok.kind != tIdent {
		return nil, p.err("expected property name")
	}
	pr := &domain.Property{RawText: strings.TrimSpace(p.src), Name: p.tok.text}
	p.advance()
	if p.tok.kind != tAt {
		return nil, p.err("expected '@clock' after property name")
	}
	p.advance()
	if p.tok.kind != tIdent {
		return nil, p.err("expected clock domain name after @")
	}
	pr.ClockDomain = p.tok.text
	p.advance()
	if p.tok.kind == tIdent && p.tok.text == "disable" {
		if err := p.parseDisable(pr); err != nil {
			return nil, err
		}
	}
	expr, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tSemi {
		return nil, p.err("expected ';' at end of property")
	}
	p.advance()
	pr.AST = expr
	pr.ID = domain.StableID("prop", pr.Name, pr.ClockDomain, pr.SemVer)
	return pr, nil
}

func (p *parser) parseDisable(pr *domain.Property) error {
	p.advance() // disable
	if p.tok.kind != tIdent || p.tok.text != "iff" {
		return p.err("expected 'iff' after 'disable'")
	}
	p.advance()
	if p.tok.kind != tLParen {
		return p.err("expected '(' after 'disable iff'")
	}
	p.advance()
	if p.tok.kind != tIdent {
		return p.err("expected disable signal name")
	}
	pr.DisableSignal = p.tok.text
	p.advance()
	if p.tok.kind != tEq {
		return p.err("expected '==' in disable iff")
	}
	p.advance()
	lvl, err := p.parseLevel()
	if err != nil {
		return err
	}
	pr.DisableLevel = lvl
	if p.tok.kind != tRParen {
		return p.err("expected ')' after disable iff condition")
	}
	p.advance()
	return nil
}

func (p *parser) parseLevel() (domain.Logic, error) {
	if p.tok.kind == tNum && (p.tok.text == "0" || p.tok.text == "1") {
		l, _ := domain.ParseLogic(p.tok.text[0])
		p.advance()
		return l, nil
	}
	if p.tok.kind == tIdent {
		switch strings.ToLower(p.tok.text) {
		case "high":
			p.advance()
			return domain.L1, nil
		case "low":
			p.advance()
			return domain.L0, nil
		}
	}
	return domain.LX, p.err("disable level must be 0/1/high/low")
}

// parseExpr parses implication > or-sequence > and-sequence > delay chain > or.
func (p *parser) parseExpr() (*domain.Node, error) {
	left, err := p.parseOrSeq()
	if err != nil {
		return nil, err
	}
	if p.tok.kind == tImplO || p.tok.kind == tImplN {
		kind := domain.ASTOverlap
		if p.tok.kind == tImplN {
			kind = domain.ASTNoOverlap
		}
		p.advance()
		right, err := p.parseOrSeq()
		if err != nil {
			return nil, err
		}
		return &domain.Node{Kind: kind, Children: []*domain.Node{left, right}}, nil
	}
	return left, nil
}

func (p *parser) parseOrSeq() (*domain.Node, error) {
	left, err := p.parseAndSeq()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tOr {
		p.advance()
		right, err := p.parseAndSeq()
		if err != nil {
			return nil, err
		}
		left = &domain.Node{Kind: domain.ASTOr, Children: []*domain.Node{left, right}}
	}
	return left, nil
}

func (p *parser) parseAndSeq() (*domain.Node, error) {
	left, err := p.parseDelayChain()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tAnd {
		p.advance()
		right, err := p.parseDelayChain()
		if err != nil {
			return nil, err
		}
		left = &domain.Node{Kind: domain.ASTAnd, Children: []*domain.Node{left, right}}
	}
	return left, nil
}

func (p *parser) parseDelayChain() (*domain.Node, error) {
	left, err := p.parseBool()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tDelay {
		spec := p.tok.text // e.g. ##1 ##[2:3] ##[*] ##[+]
		p.advance()
		lo, hi, err := parseDelaySpec(spec)
		if err != nil {
			return nil, p.err(err.Error())
		}
		right, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		node := &domain.Node{Kind: domain.ASTSeq, Cycle: lo, Hi: hi, Children: []*domain.Node{left, right}}
		left = node
	}
	return left, nil
}

// parseDelaySpec parses ##n, ##[lo:hi], ##[*] (0:unbounded) and ##[+] (1:unbounded).
func parseDelaySpec(s string) (lo, hi int, err error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "##") {
		return 0, 0, fmt.Errorf("bad delay spec %q", s)
	}
	body := s[2:]
	if body == "" || body == "[*]" {
		return 0, -1, nil
	}
	if body == "[+]" {
		return 1, -1, nil
	}
	if strings.HasPrefix(body, "[") && strings.HasSuffix(body, "]") {
		inner := body[1 : len(body)-1]
		parts := strings.Split(inner, ":")
		if len(parts) == 1 {
			n, err := strconv.Atoi(parts[0])
			if err != nil {
				return 0, 0, fmt.Errorf("bad delay spec %q", s)
			}
			return n, n, nil
		}
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("bad delay range %q", s)
		}
		lv, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		var rv int
		var err2 error
		if strings.TrimSpace(parts[1]) == "$" {
			rv = -1
		} else {
			rv, err2 = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
		if err1 != nil || err2 != nil {
			return 0, 0, fmt.Errorf("bad delay range %q", s)
		}
		return lv, rv, nil
	}
	n, err := strconv.Atoi(body)
	if err != nil {
		return 0, 0, fmt.Errorf("bad delay spec %q", s)
	}
	return n, n, nil
}

func (p *parser) parseBool() (*domain.Node, error) {
	switch p.tok.kind {
	case tNot:
		p.advance()
		child, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		return &domain.Node{Kind: domain.ASTNot, Children: []*domain.Node{child}}, nil
	case tLParen:
		p.advance()
		// Optional "empty:" marker enabling vacuous matching.
		empty := false
		if p.tok.kind == tIdent && p.tok.text == "empty" {
			p.advance()
			if p.tok.kind != tColon {
				return nil, p.err("expected ':' after 'empty'")
			}
			p.advance()
			empty = true
		}
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tRParen {
			return nil, p.err("expected ')'")
		}
		p.advance()
		if empty {
			inner.Empty = true
		}
		return inner, nil
	case tIdent:
		name := p.tok.text
		p.advance()
		return &domain.Node{Kind: domain.ASTBool, Name: name}, nil
	}
	return nil, p.err("expected boolean expression")
}

func (p *parser) err(msg string) error {
	return fmt.Errorf("prop:%d: %s (got %q)", p.tok.line, msg, p.tok.text)
}
