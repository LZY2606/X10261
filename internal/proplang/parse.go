package proplang

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// File is a parsed property text: semantic pragma plus properties.
type File struct {
	Semver     string     `json:"semver"`
	PragmaRaw  string     `json:"pragmaRaw"`
	PragmaSpan [2]int     `json:"-"`
	Properties []Property `json:"properties"`
}

var semverLine = regexp.MustCompile(`(?m)^[ \t]*//[ \t]*@semver[ \t]+([0-9]+\.[0-9]+)[^\n]*`)

// ParseText parses a small property file. Pragma syntax:
//
//	// @semver 1.1
//
// Default is 1.0. Pragma applies to all properties until changed.
func ParseText(src string) (*File, error) {
	f := &File{Semver: "1.0"}
	if m := semverLine.FindStringSubmatchIndex(src); m != nil {
		f.Semver = src[m[2]:m[3]]
		f.PragmaRaw = strings.TrimSpace(src[m[0]:m[1]])
		f.PragmaSpan = [2]int{m[0], m[1]}
	}
	toks, err := lexAll(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: src}
	for {
		tk := p.peek()
		if tk.kind == tEOF {
			break
		}
		if tk.text != "property" {
			return nil, p.errorf(tk, "expected 'property', got %q", tk.text)
		}
		prop, err := p.parseProperty(f.Semver)
		if err != nil {
			return nil, err
		}
		f.Properties = append(f.Properties, prop)
	}
	if len(f.Properties) == 0 {
		return nil, fmt.Errorf("proplang: no property definitions")
	}
	assignIDs(f)
	return f, nil
}

func assignIDs(f *File) {
	n := 0
	var walk func(*Node)
	walk = func(nd *Node) {
		if nd == nil {
			return
		}
		nd.ID = n
		n++
		for _, c := range nd.Children {
			walk(c)
		}
	}
	for i := range f.Properties {
		n = 0
		walk(f.Properties[i].Disable)
		walk(f.Properties[i].Root)
	}
}

type parser struct {
	toks []token
	pos  int
	src  string
}

func (p *parser) peek() token        { return p.toks[p.pos] }
func (p *parser) next() token        { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) accept(s string) bool {
	if p.peek().text == s {
		p.pos++
		return true
	}
	return false
}
func (p *parser) want(s string) (token, error) {
	if p.peek().text != s {
		return token{}, p.errorf(p.peek(), "expected %q got %q", s, p.peek().text)
	}
	return p.next(), nil
}
func (p *parser) errorf(tk token, format string, args ...any) error {
	return fmt.Errorf("proplang: %d:%d: %s", lineCol(p.src, tk.pos), 0, fmt.Sprintf(format, args...))
}

func lineCol(src string, off int) int {
	return strings.Count(src[:off], "\n") + 1
}

func (p *parser) parseProperty(semver string) (Property, error) {
	start := p.next() // property
	name := p.next()
	if name.kind != tIdent {
		return Property{}, p.errorf(name, "property name must be an identifier")
	}
	if _, err := p.want(";"); err != nil {
		return Property{}, err
	}
	prop := Property{Name: name.text, Text: "", SemVer: semver}
	// @(posedge clk) or @(clk)
	if p.peek().kind == tAt {
		at := p.next()
		_ = at
		if _, err := p.want("("); err != nil {
			return Property{}, err
		}
		var edge string
		if p.peek().text == "posedge" || p.peek().text == "negedge" {
			edge = p.next().text
		}
		clk := p.next()
		if clk.kind != tIdent {
			return Property{}, p.errorf(clk, "expected clock signal")
		}
		if _, err := p.want(")"); err != nil {
			return Property{}, err
		}
		if edge == "" {
			edge = "posedge"
		}
		prop.Clock = clk.text
		_ = edge
	}
	// disable iff (expr)
	if p.peek().text == "disable" {
		ds := p.next()
		if p.peek().text != "iff" {
			return Property{}, p.errorf(p.peek(), "expected 'iff' after disable")
		}
		p.next()
		if _, err := p.want("("); err != nil {
			return Property{}, err
		}
		expr, err := p.parseExpr(0)
		if err != nil {
			return Property{}, err
		}
		if _, err := p.want(")"); err != nil {
			return Property{}, err
		}
		dnode := &Node{Kind: "disable", Span: [2]int{ds.pos, expr.Span[1]}, Text: p.src[ds.pos:expr.Span[1]], Children: []*Node{expr}}
		prop.Disable = dnode
		prop.DisableExpr = p.src[expr.Span[0]:expr.Span[1]]
	}
	seq, err := p.parseSeq()
	if err != nil {
		return Property{}, err
	}
	// optional implication
	for {
		tk := p.peek()
		if tk.text != "|->" && tk.text != "|=>" {
			break
		}
		op := p.next()
		rhs, err := p.parseSeq()
		if err != nil {
			return Property{}, err
		}
		kind := "implies"
		if op.text == "|=>" {
			kind = "nonimpl"
		}
		seq = &Node{Kind: kind, Operator: op.text, Span: [2]int{seq.Span[0], rhs.Span[1]}, Text: p.src[seq.Span[0]:rhs.Span[1]], Children: []*Node{seq, rhs}}
	}
	if _, err := p.want("endproperty"); err != nil {
		return Property{}, err
	}
	_ = p.accept(";")
	prop.Root = seq
	prop.Text = p.src[start.pos : p.peekEnd(seq)]
	return prop, nil
}

func (p *parser) peekEnd(_ *Node) int {
	// endproperty already consumed; use previous token end
	return p.toks[p.pos-1].end
}

// parseSeq parses a sequence: atim ##[N] atim [;] atim ...
// Consecutive concatenations are wrapped in a "concat" node.
func (p *parser) parseSeq() (*Node, error) {
	first, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	parts := []*Node{first}
	for {
		tk := p.peek()
		if tk.text != "##" && tk.text != "#" {
			break
		}
		op := p.next()
		delay := 1
		if p.accept("[") {
			nt := p.next()
			if nt.kind != tNum {
				return nil, p.errorf(nt, "expected delay number")
			}
			delay, _ = strconv.Atoi(nt.text)
			if _, err := p.want("]"); err != nil {
				return nil, err
			}
		} else if p.peek().kind == tNum {
			nt := p.next()
			delay, _ = strconv.Atoi(nt.text)
		}
		nxt, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		dnode := &Node{Kind: "delay", Value: strconv.Itoa(delay), Span: [2]int{op.pos, nxt.Span[1]}, Text: p.src[op.pos:nxt.Span[1]], Children: []*Node{nxt}}
		parts = append(parts, dnode)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return &Node{Kind: "concat", Span: [2]int{parts[0].Span[0], parts[len(parts)-1].Span[1]}, Text: p.src[parts[0].Span[0]:parts[len(parts)-1].Span[1]], Children: parts}, nil
}

func (p *parser) parseAtom() (*Node, error) {
	tk := p.peek()
	switch {
	case tk.text == "(":
		open := p.next()
		// Could be a parenthesized expr or sequence.
		if isSeqStartAfterParen(p) {
			seq, err := p.parseSeq()
			if err != nil {
				return nil, err
			}
			close, err := p.want(")")
			if err != nil {
				return nil, err
			}
			node := &Node{Kind: "group", Span: [2]int{open.pos, close.end}, Text: p.src[open.pos:close.end], Children: []*Node{seq}}
			return p.maybeRepeat(node)
		}
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		close, err := p.want(")")
		if err != nil {
			return nil, err
		}
		node := &Node{Kind: "group", Span: [2]int{open.pos, close.end}, Text: p.src[open.pos:close.end], Children: []*Node{expr}}
		return p.maybeRepeat(node)
	case tk.kind == tIdent || tk.kind == tBinLit || tk.kind == tNum:
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		return p.maybeRepeat(expr)
	}
	return nil, p.errorf(tk, "unexpected token %q in sequence", tk.text)
}

func isSeqStartAfterParen(p *parser) bool {
	// heuristic: if the content contains ## or ends with ) [*n] it is a seq;
	// simplest: scan to matching ) for ##
	depth := 1
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return false
			}
		case "##", "#":
			return true
		}
	}
	return false
}

func (p *parser) maybeRepeat(node *Node) (*Node, error) {
	if !p.accept("[") {
		return node, nil
	}
	star := p.next()
	if star.text != "*" {
		return nil, p.errorf(star, "expected '*' in repetition")
	}
	rep := 1
	if p.peek().kind == tNum {
		rep, _ = strconv.Atoi(p.next().text)
	}
	close, err := p.want("]")
	if err != nil {
		return nil, err
	}
	return &Node{Kind: "repeat", Value: strconv.Itoa(rep), Repeat: rep, Span: [2]int{node.Span[0], close.end}, Text: p.src[node.Span[0]:close.end], Children: []*Node{node}}, nil
}

// precedence climbing for boolean expressions
var binop = map[string]int{
	"||": 1, "or": 1,
	"&&": 2, "and": 2,
	"|": 2, "&": 2,
	"==": 3, "!=": 3,
}

func (p *parser) parseExpr(minPrec int) (*Node, error) {
	tk := p.peek()
	var left *Node
	if tk.text == "!" || tk.text == "not" {
		op := p.next()
		child, err := p.parseExpr(10)
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: "unary", Operator: "!", Span: [2]int{op.pos, child.Span[1]}, Text: p.src[op.pos:child.Span[1]], Children: []*Node{child}}
	} else {
		n, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = n
	}
	for {
		tk := p.peek()
		prec, ok := binop[tk.text]
		if !ok || prec < minPrec {
			break
		}
		op := p.next()
		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		opText := op.text
		if opText == "and" {
			opText = "&&"
		}
		if opText == "or" {
			opText = "||"
		}
		left = &Node{Kind: "binary", Operator: opText, Span: [2]int{left.Span[0], right.Span[1]}, Text: p.src[left.Span[0]:right.Span[1]], Children: []*Node{left, right}}
	}
	return left, nil
}

func (p *parser) parsePrimary() (*Node, error) {
	tk := p.next()
	switch tk.kind {
	case tIdent:
		switch tk.text {
		case "true":
			return &Node{Kind: "lit", Value: "1", Span: tkSpan(tk), Text: tk.text}, nil
		case "false":
			return &Node{Kind: "lit", Value: "0", Span: tkSpan(tk), Text: tk.text}, nil
		}
		return &Node{Kind: "ident", Name: tk.text, Span: tkSpan(tk), Text: tk.text}, nil
	case tBinLit:
		return &Node{Kind: "lit", Value: strings.ToLower(strings.TrimPrefix(tk.text, "'")), Span: tkSpan(tk), Text: tk.text}, nil
	case tNum:
		return &Node{Kind: "lit", Value: tk.text, Span: tkSpan(tk), Text: tk.text}, nil
	case tPunct:
		if tk.text == "(" {
			e, err := p.parseExpr(0)
			if err != nil {
				return nil, err
			}
			if _, err := p.want(")"); err != nil {
				return nil, err
			}
			return e, nil
		}
	}
	return nil, p.errorf(tk, "unexpected expression token %q", tk.text)
}

func tkSpan(tk token) [2]int { return [2]int{tk.pos, tk.end} }
