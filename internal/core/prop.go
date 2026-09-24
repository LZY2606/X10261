package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Expr 是四态布尔表达式 AST。AST 与语义版本一起被持久化，
// 重新解释时新建分支而不是改写旧 AST。
type Expr struct {
	Kind string `json:"kind"` // sig | not | and | or
	Name string `json:"name,omitempty"`
	A    *Expr  `json:"a,omitempty"`
	B    *Expr  `json:"b,omitempty"`
}

// SeqItem 是序列的一项：相对前一项结束点的延迟、布尔表达式、
// 以及是否为空匹配（[*0]，消耗零个时钟，不读任何采样）。
type SeqItem struct {
	Delay int   `json:"delay"`
	Expr  *Expr `json:"expr,omitempty"`
	Rep0  bool  `json:"rep0,omitempty"`
}

// Seq 是序列：items 依次匹配，Delay 为相对前一项的时钟数。
type Seq struct {
	Items []SeqItem `json:"items"`
}

// Property 是一条序列属性。
type Property struct {
	Name       string `json:"name"`
	DisableIff *Expr  `json:"disable_iff,omitempty"`
	Ant        Seq    `json:"ant"`
	Op         string `json:"op"` // "|->" overlapped | "|=>" non-overlapped
	Cons       Seq    `json:"cons"`
}

type token struct {
	kind string
	text string
}

func lexProp(src string) ([]token, error) {
	var toks []token
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
		case c == '#' && i+1 < len(src) && src[i+1] == '#':
			toks = append(toks, token{"##", "##"})
			i += 2
		case c == '|' && i+2 < len(src) && src[i+1] == '-' && src[i+2] == '>':
			toks = append(toks, token{"op", "|->"})
			i += 3
		case c == '|' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '>':
			toks = append(toks, token{"op", "|=>"})
			i += 3
		case c == '&' && i+1 < len(src) && src[i+1] == '&':
			toks = append(toks, token{"&&", "&&"})
			i += 2
		case c == '|' && i+1 < len(src) && src[i+1] == '|':
			toks = append(toks, token{"||", "||"})
			i += 2
		case c == '[':
			j := strings.IndexByte(src[i:], ']')
			if j < 0 {
				return nil, fmt.Errorf("unclosed '['")
			}
			toks = append(toks, token{"rep", src[i : i+j+1]})
			i += j + 1
		case c == '(' || c == ')' || c == ';' || c == '!':
			toks = append(toks, token{string(c), string(c)})
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			toks = append(toks, token{"num", src[i:j]})
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			toks = append(toks, token{"ident", src[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", string(c))
		}
	}
	return toks, nil
}

type propParser struct {
	toks []token
	pos  int
}

// ParseProperties 解析属性源文件（可含多条 property）。
func ParseProperties(src string) ([]*Property, error) {
	toks, err := lexProp(src)
	if err != nil {
		return nil, err
	}
	p := &propParser{toks: toks}
	var props []*Property
	for p.pos < len(p.toks) {
		prop, err := p.parseProperty()
		if err != nil {
			return nil, err
		}
		props = append(props, prop)
	}
	return props, nil
}

func (p *propParser) atEnd() bool { return p.pos >= len(p.toks) }

func (p *propParser) peek() token {
	if p.atEnd() {
		return token{}
	}
	return p.toks[p.pos]
}

func (p *propParser) next() token {
	t := p.peek()
	p.pos++
	return t
}

func (p *propParser) accept(kind string) bool {
	if !p.atEnd() && p.toks[p.pos].kind == kind {
		p.pos++
		return true
	}
	return false
}

func (p *propParser) expect(kind string) error {
	if p.accept(kind) {
		return nil
	}
	return fmt.Errorf("expected %q, got %q", kind, p.peek().text)
}

func (p *propParser) expectIdent(word string) error {
	if !p.atEnd() && p.toks[p.pos].kind == "ident" && p.toks[p.pos].text == word {
		p.pos++
		return nil
	}
	return fmt.Errorf("expected %q, got %q", word, p.peek().text)
}

func (p *propParser) peekIdent(word string) bool {
	return !p.atEnd() && p.toks[p.pos].kind == "ident" && p.toks[p.pos].text == word
}

func (p *propParser) parseProperty() (*Property, error) {
	if err := p.expectIdent("property"); err != nil {
		return nil, err
	}
	name := p.next()
	if name.kind != "ident" {
		return nil, fmt.Errorf("expected property name, got %q", name.text)
	}
	prop := &Property{Name: name.text}
	p.accept(";")
	if p.peekIdent("disable") {
		p.next()
		if err := p.expectIdent("iff"); err != nil {
			return nil, err
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		prop.DisableIff = e
		if err := p.expect(")"); err != nil {
			return nil, err
		}
	}
	ant, err := p.parseSeq()
	if err != nil {
		return nil, fmt.Errorf("property %s antecedent: %v", prop.Name, err)
	}
	prop.Ant = *ant
	tok := p.next()
	if tok.kind != "op" {
		return nil, fmt.Errorf("property %s: expected |-> or |=>, got %q", prop.Name, tok.text)
	}
	prop.Op = tok.text
	cons, err := p.parseSeq()
	if err != nil {
		return nil, fmt.Errorf("property %s consequent: %v", prop.Name, err)
	}
	prop.Cons = *cons
	p.accept(";")
	if err := p.expectIdent("endproperty"); err != nil {
		return nil, err
	}
	p.accept(";")
	return prop, nil
}

func (p *propParser) parseSeq() (*Seq, error) {
	seq := &Seq{}
	for {
		delay := 0
		if p.accept("##") {
			t := p.next()
			if t.kind != "num" {
				return nil, fmt.Errorf("expected number after ##, got %q", t.text)
			}
			delay, _ = strconv.Atoi(t.text)
		}
		if p.atEnd() || p.peek().kind == "op" || p.peekIdent("endproperty") || p.peek().kind == ";" {
			return nil, fmt.Errorf("sequence item expected")
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		item := SeqItem{Delay: delay, Expr: e}
		if p.peek().kind == "rep" {
			rep := p.next().text
			if rep != "[*0]" {
				return nil, fmt.Errorf("only [*0] repetition (empty match) is supported, got %q", rep)
			}
			item.Rep0 = true
		}
		seq.Items = append(seq.Items, item)
		if p.peek().kind != "##" {
			break
		}
	}
	return seq, nil
}

func (p *propParser) parseExpr() (*Expr, error) { return p.parseOr() }

func (p *propParser) parseOr() (*Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.accept("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: "or", A: left, B: right}
	}
	return left, nil
}

func (p *propParser) parseAnd() (*Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.accept("&&") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Expr{Kind: "and", A: left, B: right}
	}
	return left, nil
}

func (p *propParser) parseUnary() (*Expr, error) {
	if p.accept("!") {
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Expr{Kind: "not", A: e}, nil
	}
	if p.accept("(") {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	t := p.next()
	if t.kind != "ident" {
		return nil, fmt.Errorf("expected signal name, got %q", t.text)
	}
	return &Expr{Kind: "sig", Name: t.text}, nil
}

// ExprString 渲染表达式（用于自动机图与页面展示）。
func ExprString(e *Expr) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case "sig":
		return e.Name
	case "not":
		return "!" + ExprString(e.A)
	case "and":
		return "(" + ExprString(e.A) + " && " + ExprString(e.B) + ")"
	case "or":
		return "(" + ExprString(e.A) + " || " + ExprString(e.B) + ")"
	}
	return "?"
}

// SeqString 渲染序列。
func SeqString(s *Seq) string {
	var parts []string
	for _, it := range s.Items {
		label := ""
		if it.Delay > 0 {
			label = fmt.Sprintf("##%d ", it.Delay)
		}
		label += ExprString(it.Expr)
		if it.Rep0 {
			label += "[*0]"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}
