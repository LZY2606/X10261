package prop

import (
	"fmt"
	"strings"

	"sigtribunal/internal/fourval"
)

// Grammar:
//   file     := property*
//   property := "property" ident ";" "@(" "posedge" ident ")"
//               [ "disable" "iff" "(" bexpr ")" ] seq ("|->"|"|=>") seq "endproperty"
//   seq      := [ "##" delay ] item { "##" delay item }
//   item     := bexpr [ "[*" n "]" ]
//   delay    := n | "[" n ":" n "]"
//   bexpr    := orExpr
//   orExpr   := andExpr { "||" andExpr }
//   andExpr  := unary { "&&" unary }
//   unary    := "!" unary | primary
//   primary  := "(" bexpr ")" | operand [ ("=="|"!=") operand ]
//   operand  := ident | const        (const: 0|1|1'b0|1'b1|1'bx|1'bz)

type lexer struct {
	toks []string
}

func lex(src string) ([]string, error) {
	var toks []string
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
			toks = append(toks, "##")
			i += 2
		case c == '[' && i+1 < len(src) && src[i+1] == '*':
			toks = append(toks, "[*")
			i += 2
		case c == '|' && i+2 < len(src) && src[i+1] == '-' && src[i+2] == '>':
			toks = append(toks, "|->")
			i += 3
		case c == '|' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '>':
			toks = append(toks, "|=>")
			i += 3
		case c == '=' && i+1 < len(src) && src[i+1] == '=':
			toks = append(toks, "==")
			i += 2
		case c == '!' && i+1 < len(src) && src[i+1] == '=':
			toks = append(toks, "!=")
			i += 2
		case c == '&' && i+1 < len(src) && src[i+1] == '&':
			toks = append(toks, "&&")
			i += 2
		case c == '|' && i+1 < len(src) && src[i+1] == '|':
			toks = append(toks, "||")
			i += 2
		case strings.ContainsRune("@();:![]", rune(c)):
			toks = append(toks, string(c))
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '\'' || src[j] == 'b' || src[j] == 'x' || src[j] == 'z' || src[j] == 'X' || src[j] == 'Z') {
				j++
			}
			toks = append(toks, src[i:j])
			i = j
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			toks = append(toks, src[i:j])
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q at offset %d", string(c), i)
		}
	}
	return toks, nil
}

type parser struct {
	toks []string
	pos  int
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *parser) expect(want string) error {
	if got := p.next(); got != want {
		return fmt.Errorf("expected %q, got %q", want, got)
	}
	return nil
}

// ParseFile parses all properties in src, keeping each property's raw text.
func ParseFile(src string) ([]*Property, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	var out []*Property
	for p.peek() == "property" {
		start := p.pos
		pr, err := p.parseProperty()
		if err != nil {
			return nil, err
		}
		pr.Raw = strings.Join(p.toks[start:p.pos], " ")
		out = append(out, pr)
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("trailing tokens after last property: %q", p.peek())
	}
	return out, nil
}

func (p *parser) parseProperty() (*Property, error) {
	if err := p.expect("property"); err != nil {
		return nil, err
	}
	pr := &Property{}
	pr.Name = p.next()
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	if err := p.expect("@"); err != nil {
		return nil, err
	}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	if err := p.expect("posedge"); err != nil {
		return nil, err
	}
	pr.Clock = p.next()
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	if p.peek() == "disable" {
		p.next()
		if err := p.expect("iff"); err != nil {
			return nil, err
		}
		if err := p.expect("("); err != nil {
			return nil, err
		}
		e, err := p.parseBExpr()
		if err != nil {
			return nil, err
		}
		pr.DisableIff = e
		if err := p.expect(")"); err != nil {
			return nil, err
		}
	}
	ant, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	pr.Antecedent = ant
	switch p.next() {
	case "|->":
		pr.Op = Overlapped
	case "|=>":
		pr.Op = NonOverlapped
	default:
		return nil, fmt.Errorf("expected implication operator, got %q", p.toks[p.pos-1])
	}
	con, err := p.parseSeq()
	if err != nil {
		return nil, err
	}
	pr.Consequent = con
	if p.peek() == ";" {
		p.next()
	}
	if err := p.expect("endproperty"); err != nil {
		return nil, err
	}
	return pr, nil
}

func (p *parser) parseSeq() (Seq, error) {
	var s Seq
	if p.peek() == "##" {
		p.next()
		d, err := p.parseDelay()
		if err != nil {
			return s, err
		}
		s.Pre = d
	}
	it, err := p.parseItem()
	if err != nil {
		return s, err
	}
	s.Items = append(s.Items, it)
	for p.peek() == "##" {
		p.next()
		d, err := p.parseDelay()
		if err != nil {
			return s, err
		}
		s.Items[len(s.Items)-1].Delay = d
		it, err := p.parseItem()
		if err != nil {
			return s, err
		}
		s.Items = append(s.Items, it)
	}
	return s, nil
}

func (p *parser) parseItem() (Item, error) {
	var it Item
	it.Rep = 1
	e, err := p.parseBExpr()
	if err != nil {
		return it, err
	}
	it.Expr = e
	if p.peek() == "[*" {
		p.next()
		n := 0
		if _, err := fmt.Sscanf(p.next(), "%d", &n); err != nil {
			return it, fmt.Errorf("bad repetition count: %w", err)
		}
		if err := p.expect("]"); err != nil {
			return it, err
		}
		it.Rep = n
	}
	return it, nil
}

func (p *parser) parseDelay() (Delay, error) {
	var d Delay
	if p.peek() == "[" {
		p.next()
		if _, err := fmt.Sscanf(p.next(), "%d", &d.Min); err != nil {
			return d, err
		}
		if err := p.expect(":"); err != nil {
			return d, err
		}
		if _, err := fmt.Sscanf(p.next(), "%d", &d.Max); err != nil {
			return d, err
		}
		if err := p.expect("]"); err != nil {
			return d, err
		}
		return d, nil
	}
	n := 0
	if _, err := fmt.Sscanf(p.next(), "%d", &n); err != nil {
		return d, fmt.Errorf("bad delay: %w", err)
	}
	d.Min, d.Max = n, n
	return d, nil
}

func (p *parser) parseBExpr() (BExpr, error) { return p.parseOr() }

func (p *parser) parseOr() (BExpr, error) {
	l, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "||" {
		p.next()
		r, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l = Or{L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseAnd() (BExpr, error) {
	l, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek() == "&&" {
		p.next()
		r, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		l = And{L: l, R: r}
	}
	return l, nil
}

func (p *parser) parseUnary() (BExpr, error) {
	if p.peek() == "!" {
		p.next()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return Not{E: e}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (BExpr, error) {
	if p.peek() == "(" {
		p.next()
		e, err := p.parseBExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	l, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	if op := p.peek(); op == "==" || op == "!=" {
		p.next()
		r, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return Cmp{Op: op, L: l, R: r}, nil
	}
	return l, nil
}

func (p *parser) parseOperand() (BExpr, error) {
	t := p.next()
	if t == "" {
		return nil, fmt.Errorf("unexpected end of input")
	}
	if t == "0" {
		return Const{Val: fourval.Zero}, nil
	}
	if t == "1" {
		return Const{Val: fourval.One}, nil
	}
	if strings.HasPrefix(t, "1'b") && len(t) == 4 {
		v, ok := fourval.Parse(t[3])
		if !ok {
			return nil, fmt.Errorf("bad constant %q", t)
		}
		return Const{Val: v}, nil
	}
	if t[0] >= '0' && t[0] <= '9' {
		return nil, fmt.Errorf("unsupported constant %q", t)
	}
	return Signal{Name: t}, nil
}
