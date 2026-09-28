package prop

import (
	"fmt"
	"strconv"
	"strings"
)

// File is a property source tagged with its semantic version.
type File struct {
	SemVer    string
	Raw       string
	Properties []*Property
}

type parser struct {
	tokens []string
	pos    int
}

// Parse parses:
//
//	semver = "1.0.0"
//	property name: disable iff (rst) ( ant |-> ##[lo:hi] cons )
func Parse(src string) (*File, error) {
	p := &parser{tokens: tokenize(src)}
	f := &File{Raw: src}
	for {
		tok, ok := p.peek()
		if !ok {
			break
		}
		switch tok {
		case "semver":
			p.next()
			if t, ok := p.next(); !ok || t != "=" {
				return nil, fmt.Errorf("expected '=' after semver")
			}
			v, ok := p.next()
			if !ok {
				return nil, fmt.Errorf("missing semver literal")
			}
			v = strings.Trim(v, `"'`)
			if f.SemVer != "" && f.SemVer != v {
				return nil, fmt.Errorf("conflicting semver tags: %s vs %s", f.SemVer, v)
			}
			f.SemVer = v
		case "property":
			pr, err := p.parseProperty()
			if err != nil {
				return nil, err
			}
			if f.SemVer == "" {
				return nil, fmt.Errorf("property %q: semver must be declared first", pr.Name)
			}
			pr.SemVer = f.SemVer
			f.Properties = append(f.Properties, pr)
		default:
			return nil, fmt.Errorf("unexpected token %q", tok)
		}
	}
	if f.SemVer == "" {
		return nil, fmt.Errorf("property file has no semver tag")
	}
	return f, nil
}

func (p *parser) parseProperty() (*Property, error) {
	p.next() // property
	name, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("property without name")
	}
	pr := &Property{Name: strings.TrimSuffix(name, ":")}
	if t, _ := p.peek(); t == ":" {
		p.next()
	}
	if t, _ := p.peek(); t == "disable" {
		p.next()
		if t, ok := p.next(); !ok || t != "iff" {
			return nil, fmt.Errorf("expected iff after disable")
		}
		e, err := p.parseParenExpr()
		if err != nil {
			return nil, err
		}
		pr.Disable = e
	}
	body, err := p.parseParenBody()
	if err != nil {
		return nil, err
	}
	pp := &parser{tokens: body}
	ant, err := pp.parseOr()
	if err != nil {
		return nil, err
	}
	pr.Ante = ant
	op, ok := pp.next()
	if !ok || (op != "|->" && op != "|=>") {
		return nil, fmt.Errorf("property %s: expected implication", pr.Name)
	}
	pr.Overlap = op == "|->"
	seq := &Sequence{}
	if t, _ := pp.peek(); t == "##[" {
		lo, hi, err := pp.parseDelay()
		if err != nil {
			return nil, err
		}
		seq.Delay = Event{Lo: lo, Hi: hi}
	} else {
		seq.Delay = Event{Lo: 0, Hi: 0}
	}
	end, err := pp.parseOr()
	if err != nil {
		return nil, err
	}
	seq.End = end
	if _, ok := pp.next(); ok {
		return nil, fmt.Errorf("property %s: trailing tokens", pr.Name)
	}
	pr.Cons = seq
	return pr, nil
}

func (p *parser) parseParenBody() ([]string, error) {
	tok, ok := p.next()
	if !ok || tok != "(" {
		return nil, fmt.Errorf("expected '(' property body, got %q", tok)
	}
	depth := 1
	var out []string
	for {
		t, ok := p.next()
		if !ok {
			return nil, fmt.Errorf("unterminated property body")
		}
		if t == "(" {
			depth++
		}
		if t == ")" {
			depth--
			if depth == 0 {
				return out, nil
			}
		}
		out = append(out, t)
	}
}

func (p *parser) parseDelay() (int, int, error) {
	p.next() // ##[
	loS, ok := p.next()
	if !ok {
		return 0, 0, fmt.Errorf("bad delay range")
	}
	lo, err := strconv.Atoi(loS)
	if err != nil {
		return 0, 0, err
	}
	sep, _ := p.next()
	if sep != ":" && sep != "]" {
		return 0, 0, fmt.Errorf("bad delay range near %q", sep)
	}
	if sep == "]" {
		return lo, lo, nil
	}
	hiS, ok := p.next()
	if !ok {
		return 0, 0, fmt.Errorf("bad delay hi")
	}
	close, _ := p.next()
	if close != "]" {
		return 0, 0, fmt.Errorf("delay range missing ]")
	}
	if hiS == "$" {
		return lo, -1, nil
	}
	hi, err := strconv.Atoi(hiS)
	if err != nil {
		return 0, 0, err
	}
	return lo, hi, nil
}

func (p *parser) parseParenExpr() (*Expr, error) {
	tok, ok := p.next()
	if !ok || tok != "(" {
		return nil, fmt.Errorf("expected '(' expression, got %q", tok)
	}
	save := p.pos
	depth := 1
	var inner []string
	for {
		t, ok := p.next()
		if !ok {
			return nil, fmt.Errorf("unterminated expression")
		}
		if t == "(" {
			depth++
		}
		if t == ")" {
			depth--
			if depth == 0 {
				break
			}
		}
		inner = append(inner, t)
	}
	sub := &parser{tokens: inner, pos: 0}
	e, err := sub.parseOr()
	if err != nil {
		p.pos = save
		return nil, err
	}
	return e, nil
}

func (p *parser) parseOr() (*Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t != "||" {
			return left, nil
		}
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: KOr, Args: []*Expr{left, right}}
	}
}

func (p *parser) parseAnd() (*Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t != "&&" {
			return left, nil
		}
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: KAnd, Args: []*Expr{left, right}}
	}
}

func (p *parser) parseNot() (*Expr, error) {
	t, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("expected operand")
	}
	if t == "!" {
		p.next()
		e, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: KNot, Args: []*Expr{e}}, nil
	}
	if t == "(" {
		return p.parseParenExpr()
	}
	p.next()
	if isConst(t) {
		return &Expr{Kind: KConst, Name: strings.ToUpper(t)}, nil
	}
	if !isIdent(t) {
		return nil, fmt.Errorf("bad identifier %q", t)
	}
	return &Expr{Kind: KIdent, Name: t}, nil
}

func isConst(t string) bool {
	switch strings.ToUpper(t) {
	case "0", "1", "X", "Z":
		return true
	}
	return false
}

func isIdent(t string) bool {
	if t == "" {
		return false
	}
	for i, r := range t {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (p *parser) peek() (string, bool) {
	if p.pos >= len(p.tokens) {
		return "", false
	}
	return p.tokens[p.pos], true
}

func (p *parser) next() (string, bool) {
	if p.pos >= len(p.tokens) {
		return "", false
	}
	t := p.tokens[p.pos]
	p.pos++
	return t, true
}

var twoChar = map[string]bool{
	"|->": true, "|=>": true, "##[": true, "&&": true, "||": true, "==": true, ":=": true,
}

func tokenize(src string) []string {
	var toks []string
	i := 0
	for i < len(src) {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		// line comments
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// quoted literal
		if c == '"' || c == '\'' {
			q := c
			j := i + 1
			for j < len(src) && src[j] != q {
				j++
			}
			toks = append(toks, strings.Trim(src[i:j+1], `"'`))
			i = j + 1
			continue
		}
		if i+3 <= len(src) && twoChar[src[i:i+3]] {
			toks = append(toks, src[i:i+3])
			i += 3
			continue
		}
		if i+2 <= len(src) && twoChar[src[i:i+2]] {
			toks = append(toks, src[i:i+2])
			i += 2
			continue
		}
		if c == '(' || c == ')' || c == '[' || c == ']' || c == ':' || c == ',' || c == '=' || c == '!' {
			toks = append(toks, string(c))
			i++
			continue
		}
		j := i
		for j < len(src) && isWordByte(src[j]) {
			j++
		}
		if j == i {
			i++
			continue
		}
		toks = append(toks, src[i:j])
		i = j
	}
	return toks
}

func isWordByte(c byte) bool {
	switch {
	case c == '_', c == '.', c == '$', c == '#', c == '|', c == '-', c == '>':
		return true
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return false
}
