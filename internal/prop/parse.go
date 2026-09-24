package prop

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// parser is a recursive-descent parser for the small SVA-like fragment:
//
//	property ::= ["disable" "iff" "(" expr ")" ] implication
//	implication ::= seq ("|->" | "|=>") seq
//	seq ::= concat
//	concat ::= ("##" number)* factor (("##" number)? factor)*
//	factor ::= atom rep? | "(" seq ")" rep? | "*" | "1"
//	atom ::= expr
//	expr ::= unary (("&&" | "||") unary)*
//	unary ::= "!" unary | primary
//	primary ::= ident [ "==" number ] | "(" expr ")"
//	rep ::= "[*]" | "[+]" | "[" number "]"
type parser struct {
	toks []token
	pos  int
	src  string
	ids  map[string]int
	sigs map[string]struct{}
}

// Parse parses one property/sequence text into a Spec.
func Parse(text string) (*Spec, error) {
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, src: text, ids: map[string]int{}, sigs: map[string]struct{}{}}
	spec := &Spec{Text: text}
	if p.peek().kind == tIdent && p.peek().text == "disable" {
		p.next()
		if p.peek().kind != tIdent || p.peek().text != "iff" {
			return nil, p.err("expected 'iff' after 'disable'")
		}
		p.next()
		if p.peek().kind != tLParen {
			return nil, p.err("expected '(' after 'disable iff'")
		}
		p.next()
		cond := p.parseExpr()
		if p.peek().kind != tRParen {
			return nil, p.err("missing ')' for disable iff")
		}
		p.next()
		spec.Disable = &cond
	}
	root, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, p.err(fmt.Sprintf("unexpected token %q", p.peek().text))
	}
	spec.Root = root
	for s := range p.sigs {
		spec.Signals = append(spec.Signals, s)
	}
	sort.Strings(spec.Signals)
	return spec, nil
}

func (p *parser) peek() token  { return p.toks[p.pos] }
func (p *parser) next() token  { t := p.toks[p.pos]; p.pos++; return t }
func (p *parser) err(msg string) error {
	t := p.peek()
	return fmt.Errorf("parse at offset %d near %q: %s", t.pos, t.text, msg)
}

func (p *parser) parseSeq() (Node, error) {
	left, err := p.parseConcat()
	if err != nil {
		return Node{}, err
	}
	if p.peek().kind == tImplO || p.peek().kind == tImplNO {
		t := p.next()
		right, err := p.parseConcat()
		if err != nil {
			return Node{}, err
		}
		return p.node("implication", t.pos, opFor(t.kind), left, right), nil
	}
	return left, nil
}

func opFor(k tokenKind) string {
	if k == tImplNO {
		return "|=>"
	}
	return "|->"
}

// parseConcat parses S1 ##d S2. A leading ##d is an initial delay relative to
// the attempt origin. A missing delay between factors means ##1.
func (p *parser) parseConcat() (Node, error) {
	var first Node
	firstDelay := -1
	if p.peek().kind == tHash {
		p.next()
		t := p.next()
		if t.kind != tNumber {
			return Node{}, p.err("expected number after ##")
		}
		firstDelay, _ = strconv.Atoi(t.text)
	}
	f, err := p.parseFactor()
	if err != nil {
		return Node{}, err
	}
	if firstDelay >= 0 {
		d := firstDelay
		first = p.node("concat", p.toks[0].pos, "", emptyNode(p), f)
		first.Delay = &d
	} else {
		first = f
	}
	node := first
	for {
		if p.peek().kind != tHash {
			break
		}
		hashPos := p.peek().pos
		p.next()
		delay := 1
		if p.peek().kind == tNumber {
			delay, _ = strconv.Atoi(p.next().text)
		}
		rhs, err := p.parseFactor()
		if err != nil {
			return Node{}, err
		}
		n := p.node("concat", hashPos, "", node, rhs)
		n.Delay = &delay
		node = n
	}
	return node, nil
}

func (p *parser) parseFactor() (Node, error) {
	if p.peek().kind == tStar {
		t := p.next()
		return p.node("empty", t.pos, "*"), nil
	}
	if p.peek().kind == tNumber && p.peek().text == "1" {
		// bare 1 is an empty-match sequence (SVA "1")
		t := p.next()
		return p.node("empty", t.pos, "1"), nil
	}
	if p.peek().kind == tLParen {
		t := p.next()
		inner, err := p.parseSeq()
		if err != nil {
			return Node{}, err
		}
		if p.peek().kind != tRParen {
			return Node{}, p.err("missing ')'")
		}
		p.next()
		if r, ok, err := p.maybeRep(); err != nil {
			return Node{}, err
		} else if ok {
			inner.Rep = r
		}
		return inner, nil
	}
	atom, err := p.parseExpr()
	if err != nil {
		return Node{}, err
	}
	if r, ok, err := p.maybeRep(); err != nil {
		return Node{}, err
	} else if ok {
		atom.Rep = r
	}
	return atom, nil
}

func (p *parser) maybeRep() (*Rep, bool, error) {
	if p.peek().kind != tLBrack {
		return nil, false, nil
	}
	p.next()
	r := &Rep{}
	switch p.peek().kind {
	case tStar:
		r.Text = "[*]"; r.Min, r.Max = 0, -1; p.next()
	case tPlus:
		r.Text = "[+]"; r.Min, r.Max = 1, -1; p.next()
	case tNumber:
		n, _ := strconv.Atoi(p.next().text)
		r.Text = "[" + strconv.Itoa(n) + "]"; r.Min, r.Max = n, n
	default:
		return nil, false, p.err("malformed repetition")
	}
	if p.peek().kind != tRBrack {
		return nil, false, p.err("missing ']' in repetition")
	}
	p.next()
	return r, true, nil
}

func (p *parser) parseExpr() Node {
	left := p.parseUnary()
	for p.peek().kind == tAmpAmp || p.peek().kind == tPipePipe {
		t := p.next()
		right := p.parseUnary()
		op := "&&"
		if t.kind == tPipePipe {
			op = "||"
		}
		left = p.node("binop", t.pos, op, left, right)
	}
	return left
}

func (p *parser) parseUnary() Node {
	if p.peek().kind == tBang {
		t := p.next()
		return p.node("not", t.pos, "!", p.parseUnary())
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() Node {
	if p.peek().kind == tLParen {
		p.next()
		inner := p.parseExpr()
		if p.peek().kind == tRParen {
			p.next()
		}
		return inner
	}
	t := p.next()
	n := p.node("signal", t.pos, "")
	n.Name = t.text
	p.sigs[t.text] = struct{}{}
	if p.peek().kind == tEqEq || p.peek().kind == tNotEq {
		op := p.next()
		v := p.next()
		n.Op = "=="
		n.Value = v.text
		if op.kind == tNotEq {
			n.Op = "!="
		}
	}
	return n
}

func emptyNode(p *parser) Node { return p.node("empty", 0, "0") }

func (p *parser) node(kind string, pos int, op string, children ...Node) Node {
	n := Node{Kind: kind, Op: op, Children: children}
	if pos >= 0 && pos < len(p.src) {
		n.Src = sliceSrc(p.src, pos)
	}
	key := stableKey(&n)
	seq := p.ids[key] + 1
	p.ids[key] = seq
	n.ID = shortHash(key) + fmt.Sprintf("-%d", seq)
	return n
}

func sliceSrc(src string, pos int) string {
	end := pos + 16
	if end > len(src) {
		end = len(src)
	}
	return strings.TrimSpace(src[pos:end])
}

func stableKey(n *Node) string {
	var b strings.Builder
	b.WriteString(n.Kind)
	b.WriteString("|")
	b.WriteString(n.Name)
	b.WriteString("|")
	b.WriteString(n.Op)
	b.WriteString("|")
	b.WriteString(n.Value)
	if n.Rep != nil {
		b.WriteString("|rep")
		b.WriteString(n.Rep.Text)
	}
	if n.Delay != nil {
		b.WriteString("|d")
		b.WriteString(strconv.Itoa(*n.Delay))
	}
	for i := range n.Children {
		b.WriteString("|c")
		b.WriteString(n.Children[i].ID)
	}
	return b.String()
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return "n" + hex.EncodeToString(sum[:])[:8]
}
