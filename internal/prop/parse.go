// Package prop parses the small SVA-style sequence/property language and the
// clock-domain/reset specification that accompanies a VCD.
//
// Property grammar (whitespace tolerant):
//
//	<prop>   := <expr>
//	<expr>   := <or>
//	<or>     := <and> ("or" <and>)*
//	<and>    := <unary> ("and" <unary>)*
//	<unary>  := "not" <unary> | <imp>
//	<imp>    := <seq> (("|->" | "|=>") <seq>)?
//	<seq>    := <atom> ("##[" <num> ":" <num> "]" <atom>)* | <atom>
//	<atom>   := <sig> | "(" <expr> ")" | "empty" | <atom> "[*" <num> ":" <num> "]"
package prop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"signaltribunal/internal/model"
)

// DomainSpec is a parsed clock/reset declaration.
type DomainSpec struct {
	Name       string
	Clock      string
	Polarity   model.EdgeKind
	Reset      string
	ResetLevel model.Quad
}

// File is a parsed property specification.
type File struct {
	Source     *model.PropSource
	Domains    []DomainSpec
	Properties []*model.Property
}

type lexer struct {
	tokens []string
	pos    int
}

// Parse parses a property specification file.
func Parse(path string, data []byte) (*File, error) {
	hash := sha256.Sum256(data)
	srcID := "src_prop_" + hex.EncodeToString(hash[:])[:16]
	f := &File{Source: &model.PropSource{
		ID: srcID, Path: path, ByteHash: "sha256:" + hex.EncodeToString(hash[:]),
		Bytes: append([]byte(nil), data...),
	}}
	semVer := "sva2012"
	var orderedNames []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "semantics":
			if len(fields) >= 2 {
				semVer = fields[1]
			}
		case "clock":
			// clock <name> on <sig> posedge|negedge reset <sig> level <0|1>
			ds := DomainSpec{Polarity: model.EdgeRising, ResetLevel: model.Q1}
			for i := 1; i < len(fields); i++ {
				switch fields[i] {
				case "on":
					i++
					ds.Clock = fields[i]
				case "name":
					i++
					ds.Name = fields[i]
				case "posedge":
					ds.Polarity = model.EdgeRising
				case "negedge":
					ds.Polarity = model.EdgeFalling
				case "reset":
					i++
					ds.Reset = fields[i]
				case "level":
					i++
					ds.ResetLevel = model.Quad(fields[i])
				}
			}
			if ds.Name == "" {
				ds.Name = ds.Clock
			}
			f.Domains = append(f.Domains, ds)
		case "assert":
			// assert <id> ":" <expression...>
			rest := strings.TrimSpace(strings.TrimPrefix(line, "assert"))
			ci := strings.Index(rest, ":")
			if ci < 0 {
				return nil, fmt.Errorf("assert missing ':' : %q", line)
			}
			name := strings.TrimSpace(rest[:ci])
			expr := strings.TrimSpace(rest[ci+1:])
			orderedNames = append(orderedNames, name)
			_ = expr
			p, err := parseProperty(srcID, name, expr, semVer)
			if err != nil {
				return nil, fmt.Errorf("assert %s: %w", name, err)
			}
			f.Properties = append(f.Properties, p)
		default:
			return nil, fmt.Errorf("unknown directive %q", line)
		}
	}
	f.Source.SemVer = semVer
	for _, p := range f.Properties {
		p.DomainID = f.Domains[0].Name
	}
	if len(f.Domains) == 0 {
		return nil, fmt.Errorf("property file %s: no clock domain declared", path)
	}
	_ = orderedNames
	return f, nil
}

func parseProperty(srcID, name, text, semVer string) (*model.Property, error) {
	toks := tokenize(text)
	lx := &lexer{tokens: toks}
	p := &model.Property{
		Name:   name,
		Text:   text,
		SemVer: semVer,
		Nodes:  map[string]*model.PropNode{},
	}
	p.ID = model.StableID("prop", srcID, name)
	root, err := lx.parseExpr(p)
	if err != nil {
		return nil, err
	}
	if lx.pos != len(lx.tokens) {
		return nil, fmt.Errorf("unexpected token %q", lx.tokens[lx.pos])
	}
	p.RootID = root
	return p, nil
}

func (l *lexer) add(p *model.Property, n *model.PropNode) string {
	n.ID = model.StableID("node", p.ID, string(n.Kind), n.Text, n.Signal,
		strconv.Itoa(n.Lo), strconv.Itoa(n.Hi), strconv.FormatBool(n.Overlap),
		strings.Join(n.Children, ","))
	p.Nodes[n.ID] = n
	return n.ID
}

func tokenize(s string) []string {
	repl := strings.NewReplacer(
		"|->", " |-> ", "|=>", " |=> ",
		"##[", " ##[ ", "[*", " [* ",
		"(", " ( ", ")", " ) ",
		":", " : ", "]", " ] ",
	)
	s = repl.Replace(s)
	return strings.Fields(s)
}

func (l *lexer) peek() string {
	if l.pos < len(l.tokens) {
		return l.tokens[l.pos]
	}
	return ""
}
func (l *lexer) next() string {
	t := l.peek()
	l.pos++
	return t
}
func (l *lexer) accept(t string) bool {
	if l.peek() == t {
		l.pos++
		return true
	}
	return false
}
func (l *lexer) expectNum() (int, error) {
	t := l.next()
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0, fmt.Errorf("expected number, got %q", t)
	}
	return n, nil
}

func (l *lexer) parseExpr(p *model.Property) (string, error) {
	left, err := l.parseOr(p)
	return left, err
}

func (l *lexer) parseOr(p *model.Property) (string, error) {
	left, err := l.parseAnd(p)
	if err != nil {
		return "", err
	}
	for l.accept("or") {
		right, err := l.parseAnd(p)
		if err != nil {
			return "", err
		}
		left = l.add(p, &model.PropNode{Kind: model.NOr, Text: "or", Children: []string{left, right}})
	}
	return left, nil
}

func (l *lexer) parseAnd(p *model.Property) (string, error) {
	left, err := l.parseUnary(p)
	if err != nil {
		return "", err
	}
	for l.accept("and") {
		right, err := l.parseUnary(p)
		if err != nil {
			return "", err
		}
		left = l.add(p, &model.PropNode{Kind: model.NAnd, Text: "and", Children: []string{left, right}})
	}
	return left, nil
}

func (l *lexer) parseUnary(p *model.Property) (string, error) {
	if l.accept("not") {
		child, err := l.parseUnary(p)
		if err != nil {
			return "", err
		}
		return l.add(p, &model.PropNode{Kind: model.NNot, Text: "not", Children: []string{child}}), nil
	}
	if l.accept("disable") {
		if !l.accept("iff") {
			return "", fmt.Errorf("expected 'iff' after 'disable'")
		}
		if !l.accept("(") {
			return "", fmt.Errorf("expected '(' after 'disable iff'")
		}
		sig := l.next()
		if !isSig(sig) {
			return "", fmt.Errorf("disable iff expects a signal, got %q", sig)
		}
		if !l.accept(")") {
			return "", fmt.Errorf("expected ')' after disable condition")
		}
		child, err := l.parseUnary(p)
		if err != nil {
			return "", err
		}
		return l.add(p, &model.PropNode{
			Kind: model.NDisable, Text: "disable iff", Signal: sig,
			Children: []string{child},
		}), nil
	}
	return l.parseImp(p)
}

func (l *lexer) parseImp(p *model.Property) (string, error) {
	ante, err := l.parseSeq(p)
	if err != nil {
		return "", err
	}
	if t := l.peek(); t == "|->" || t == "|=>" {
		op := l.next()
		cons, err := l.parseSeq(p)
		if err != nil {
			return "", err
		}
		return l.add(p, &model.PropNode{
			Kind: model.NImplication, Text: op, Overlap: op == "|->",
			Children: []string{ante, cons},
		}), nil
	}
	return ante, nil
}

func (l *lexer) parseSeq(p *model.Property) (string, error) {
	left, err := l.parseAtom(p)
	if err != nil {
		return "", err
	}
	for l.accept("##[") {
		lo, err := l.expectNum()
		if err != nil {
			return "", err
		}
		if !l.accept(":") {
			return "", fmt.Errorf("expected ':' in delay window")
		}
		hi, err := l.expectNum()
		if err != nil {
			return "", err
		}
		if !l.accept("]") {
			return "", fmt.Errorf("expected ']' after delay window")
		}
		right, err := l.parseAtom(p)
		if err != nil {
			return "", err
		}
		left = l.add(p, &model.PropNode{
			Kind: model.NConcat, Text: fmt.Sprintf("##[%d:%d]", lo, hi),
			Lo: lo, Hi: hi, Children: []string{left, right},
		})
	}
	return left, nil
}

func (l *lexer) parseAtom(p *model.Property) (string, error) {
	var id string
	var err error
	switch l.peek() {
	case "":
		return "", fmt.Errorf("unexpected end of expression")
	case "(":
		l.next()
		id, err = l.parseExpr(p)
		if err != nil {
			return "", err
		}
		if !l.accept(")") {
			return "", fmt.Errorf("expected ')'")
		}
	case "empty":
		l.next()
		id = l.add(p, &model.PropNode{Kind: model.NEmpty, Text: "empty"})
	default:
		sig := l.next()
		if !isSig(sig) {
			return "", fmt.Errorf("expected signal, got %q", sig)
		}
		id = l.add(p, &model.PropNode{Kind: model.NProposition, Text: sig, Signal: sig})
	}
	for l.accept("[*") {
		lo, err := l.expectNum()
		if err != nil {
			return "", err
		}
		if !l.accept(":") {
			return "", fmt.Errorf("expected ':' in repeat window")
		}
		hi, err := l.expectNum()
		if err != nil {
			return "", err
		}
		if !l.accept("]") {
			return "", fmt.Errorf("expected ']' after repeat window")
		}
		id = l.add(p, &model.PropNode{
			Kind: model.NRepeat, Text: fmt.Sprintf("[*%d:%d]", lo, hi),
			Lo: lo, Hi: hi, Children: []string{id},
		})
	}
	return id, nil
}

func isSig(t string) bool {
	if t == "" {
		return false
	}
	for _, c := range t {
		if !(c == '_' || c == '.' ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}
