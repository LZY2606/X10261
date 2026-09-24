package ssp

import (
	"fmt"
	"strconv"
	"strings"

	"signalcourt/internal/model"
)

// File is a parsed property source.
type File struct {
	SourceID   string
	Properties []*Property
}

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tInt
	tPunct
	tKeyword
)

type token struct {
	kind tokenKind
	val  string
	span [2]int
}

type lexer struct {
	src string
	pos int
}

var keywords = map[string]bool{
	"property": true, "endproperty": true, "disable": true, "iff": true,
	"or": true,
}

func (l *lexer) lex() ([]token, error) {
	var toks []token
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			l.pos++
			continue
		}
		if c == ';' {
			toks = append(toks, token{tPunct, ";", [2]int{l.pos, l.pos + 1}})
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
			end := strings.Index(l.src[l.pos+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("ssp: unterminated block comment at %d", l.pos)
			}
			l.pos += 2 + end + 2
			continue
		}
		start := l.pos
		if isIdentStart(c) {
			for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
				l.pos++
			}
			val := l.src[start:l.pos]
			k := tIdent
			if keywords[val] {
				k = tKeyword
			}
			toks = append(toks, token{k, val, [2]int{start, l.pos}})
			continue
		}
		if c >= '0' && c <= '9' {
			for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
				l.pos++
			}
			toks = append(toks, token{tInt, l.src[start:l.pos], [2]int{start, l.pos}})
			continue
		}
		// multi-char punctuation
		for _, op := range []string{"|->", "|=>", "##", "&&", "||", "=="} {
			if strings.HasPrefix(l.src[l.pos:], op) {
				toks = append(toks, token{tPunct, op, [2]int{l.pos, l.pos + len(op)}})
				l.pos += len(op)
				start = -1
				break
			}
		}
		if start == -1 {
			continue
		}
		toks = append(toks, token{tPunct, string(c), [2]int{l.pos, l.pos + 1}})
		l.pos++
	}
	toks = append(toks, token{tEOF, "", [2]int{len(l.src), len(l.src)}})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '\\'
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '$'
}

type parser struct {
	toks []token
	pos  int
	src  string
	sid  string
}

func (p *parser) peek() token       { return p.toks[p.pos] }
func (p *parser) next() token       { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) isPunct(v string) bool {
	t := p.peek()
	return t.kind == tPunct && t.val == v
}
func (p *parser) eatPunct(v string) (token, error) {
	t := p.peek()
	if t.kind != tPunct || t.val != v {
		return t, fmt.Errorf("ssp: expected %q at byte %d, got %q", v, t.span[0], t.val)
	}
	p.pos++
	return t, nil
}

// Parse parses a property source. semver is the interpretation version (e.g.
// "ssp-1.0.0"); it is recorded on every property. A zero-width sequence used
// directly as a property is a parse-level version error under 1.0 so the
// difference is explicit and never silently rewritten.
func Parse(src model.Source, semver string) (*File, error) {
	if _, ok := model.ParseSemver(semver); !ok {
		return nil, fmt.Errorf("ssp: bad semantic version %q", semver)
	}
	toks, err := (&lexer{src: string(src.Bytes)}).lex()
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: string(src.Bytes), sid: src.ID}
	f := &File{SourceID: src.ID}
	for p.peek().kind != tEOF {
		prop, err := p.parseProperty(semver)
		if err != nil {
			return nil, err
		}
		f.Properties = append(f.Properties, prop)
	}
	if len(f.Properties) == 0 {
		return nil, fmt.Errorf("ssp: no properties in source")
	}
	return f, nil
}

func (p *parser) parseProperty(semver string) (*Property, error) {
	if t := p.peek(); t.kind != tKeyword || t.val != "property" {
		return nil, fmt.Errorf("ssp: expected 'property' at byte %d", t.span[0])
	}
	kw := p.next()
	nameT := p.next()
	if nameT.kind != tIdent {
		return nil, fmt.Errorf("ssp: property name expected at byte %d", nameT.span[0])
	}
	if _, err := p.eatPunct(";"); err != nil {
		return nil, err
	}
	prop := &Property{
		Name:     nameT.val,
		SourceID: p.sid,
		Semver:   semver,
	}
	if t := p.peek(); t.kind == tKeyword && t.val == "disable" {
		_ = p.next()
		if t := p.next(); t.kind != tKeyword || t.val != "iff" {
			return nil, fmt.Errorf("ssp: expected 'iff' at byte %d", t.span[0])
		}
		if _, err := p.eatPunct("("); err != nil {
			return nil, err
		}
		b, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		prop.Disable = b
		if _, err := p.eatPunct(")"); err != nil {
			return nil, err
		}
	}
	first, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	if p.isPunct("|->") || p.isPunct("|=>") {
		op := p.next()
		prop.Ante = first
		prop.Overlap = op.val == "|->"
		prop.Cons, err = p.parseSeq()
		if err != nil {
			return nil, err
		}
	} else {
		prop.Bare = true
		prop.BareSeq = first
	}
	if p.isPunct(";") {
		_, _ = p.eatPunct(";")
	}
	if t := p.next(); t.kind != tKeyword || t.val != "endproperty" {
		return nil, fmt.Errorf("ssp: expected 'endproperty' at byte %d, got %q", t.span[0], t.val)
	}
	if _, err := p.eatPunct(";"); err != nil {
		// ; optional after endproperty
		if p.peek().kind != tEOF && !(p.peek().kind == tKeyword && p.peek().val == "property") {
			return nil, err
		}
	}
	prop.Span = [2]int{kw.span[0], p.peek().span[0]}
	assignIDs(prop, p.sid)
	if prop.Bare && prop.BareSeq.CanMatchZero() {
		if rejectsEmptySeq(semver) {
			return nil, fmt.Errorf("ssp %s: property %s uses a zero-width sequence; ssp-1.0.0 rejects empty sequence properties (use ssp-1.1.0)", semver, prop.Name)
		}
	}
	return prop, nil
}

// rejectsEmptySeq encodes the semantic version divergence.
func rejectsEmptySeq(v string) bool {
	s, _ := model.ParseSemver(v)
	return s.Major == 1 && s.Minor == 0
}

func (p *parser) atSeqStop() bool {
	t := p.peek()
	return t.kind == tPunct && (t.val == ";" || t.val == ")" || t.val == "|->" || t.val == "|=>")
}

func (p *parser) parseSeq() (*SeqNode, error) {
	left, err := p.parseSeqDelay()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind == tKeyword && t.val == "or" {
			_ = p.next()
			right, err := p.parseSeqDelay()
			if err != nil {
				return nil, err
			}
			left = &SeqNode{Kind: NSeqOr, Left: left, Right: right, Span: [2]int{left.Span[0], right.Span[1]}}
			continue
		}
		break
	}
	return left, nil
}

func (p *parser) parseSeqDelay() (*SeqNode, error) {
	left, err := p.parseSeqAtom()
	if err != nil {
		return nil, err
	}
	for p.isPunct("##") && !p.atSeqStop() {
		_ = p.next()
		lo, hi, span, err := p.parseRange()
		if err != nil {
			return nil, err
		}
		right, err := p.parseSeqAtom()
		if err != nil {
			return nil, err
		}
		left = &SeqNode{Kind: NSeqDelay, Left: left, Right: right, Lo: lo, Hi: hi,
			Span: [2]int{left.Span[0], span[1]}}
	}
	return left, nil
}

func (p *parser) parseRange() (lo, hi int, span [2]int, err error) {
	if t := p.peek(); t.kind == tInt {
		nt := p.next()
		n, _ := strconv.Atoi(nt.val)
		return n, n, nt.span, nil
	}
	if p.isPunct("[") {
		open := p.next()
		loT := p.next()
		if loT.kind != tInt {
			return 0, 0, span, fmt.Errorf("ssp: expected range integer at %d", loT.span[0])
		}
		lo, _ = strconv.Atoi(loT.val)
		hi = lo
		if p.isPunct(":") {
			_ = p.next()
			hiT := p.next()
			if hiT.kind != tInt {
				return 0, 0, span, fmt.Errorf("ssp: expected range hi at %d", hiT.span[0])
			}
			hi, _ = strconv.Atoi(hiT.val)
		}
		closeT, cerr := p.eatPunct("]")
		if cerr != nil {
			return 0, 0, span, cerr
		}
		if hi < lo {
			return 0, 0, span, fmt.Errorf("ssp: bad range [%d:%d]", lo, hi)
		}
		return lo, hi, [2]int{open.span[0], closeT.span[1]}, nil
	}
	// bare ## means ##[1:1]
	t := p.peek()
	return 1, 1, t.span, nil
}

func (p *parser) parseSeqAtom() (*SeqNode, error) {
	if p.isPunct("(") {
		open := p.next()
		s, err := p.parseSeq()
		if err != nil {
			return nil, err
		}
		close, err := p.eatPunct(")")
		if err != nil {
			return nil, err
		}
		s.Span = [2]int{open.span[0], close.span[1]}
		return s, nil
	}
	b, err := p.parseBool()
	if err != nil {
		return nil, err
	}
	return &SeqNode{Kind: NBool, Bool: b, Span: b.Span}, nil
}

// Boolean grammar (precedence: || < && < !):
func (p *parser) parseBool() (*BoolNode, error) { return p.parseOr() }

func (p *parser) parseOr() (*BoolNode, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isPunct("||") {
		op := p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = &BoolNode{Op: BOr, Left: l, Right: r, Span: [2]int{l.Span[0], r.Span[1]}}
		_ = op
	}
	return l, nil
}

func (p *parser) parseAnd() (*BoolNode, error) {
	l, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isPunct("&&") {
		_ = p.next()
		r, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l = &BoolNode{Op: BAnd, Left: l, Right: r, Span: [2]int{l.Span[0], r.Span[1]}}
	}
	return l, nil
}

func (p *parser) parseNot() (*BoolNode, error) {
	if p.isPunct("!") {
		op := p.next()
		b, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &BoolNode{Op: BNot, Left: b, Span: [2]int{op.span[0], b.Span[1]}}, nil
	}
	return p.parseBoolAtom()
}

func (p *parser) parseBoolAtom() (*BoolNode, error) {
	if p.isPunct("(") {
		open := p.next()
		b, err := p.parseBool()
		if err != nil {
			return nil, err
		}
		close, err := p.eatPunct(")")
		if err != nil {
			return nil, err
		}
		b.Span = [2]int{open.span[0], close.span[1]}
		return b, nil
	}
	t := p.next()
	if t.kind != tIdent {
		return nil, fmt.Errorf("ssp: expected signal name at byte %d, got %q", t.span[0], t.val)
	}
	return &BoolNode{Op: BVar, Name: t.val, Span: t.span}, nil
}
