package property

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseFile parses a property file. The raw text is preserved on each
// property so reinterpretation never needs the original file again.
//
// Grammar (line oriented):
//
//	sem sem/2
//	property <name> @<sig>:posedge|negedge
//	  disable iff <boolexpr>
//	  <seq> |-> <seq>      (or |=>)
//	end
//
// Sequence item: [##N|##[A:B]] <boolexpr> [[*N]|[*A:B]]
// Boolexpr: sig | !sig | ( a & b ) | ( a | b )
func ParseFile(src string) ([]*Property, error) {
	lines := strings.Split(src, "\n")
	sem := SemV2
	var props []*Property
	var cur *Property
	var body []string
	flush := func() error {
		if cur == nil {
			return nil
		}
		if err := parseBody(cur, body); err != nil {
			return err
		}
		cur.Raw = strings.Join(body, "\n")
		props = append(props, cur)
		cur = nil
		body = nil
		return nil
	}
	for ln, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "sem ") {
			sem = strings.TrimSpace(strings.TrimPrefix(line, "sem "))
			if sem != SemV1 && sem != SemV2 {
				return nil, fmt.Errorf("line %d: unknown sem version %q", ln+1, sem)
			}
			continue
		}
		if strings.HasPrefix(line, "property ") {
			if err := flush(); err != nil {
				return nil, err
			}
			f := strings.Fields(line)
			if len(f) < 3 {
				return nil, fmt.Errorf("line %d: bad property header", ln+1)
			}
			cur = &Property{Name: f[1], SemVersion: sem, Posedge: true}
			clock := f[2]
			if strings.HasPrefix(clock, "@") {
				clock = clock[1:]
			}
			if i := strings.Index(clock, ":"); i >= 0 {
				cur.Clock = clock[:i]
				cur.Posedge = clock[i+1:] != "negedge"
			} else {
				cur.Clock = clock
			}
			continue
		}
		if line == "end" {
			if err := flush(); err != nil {
				return nil, fmt.Errorf("line %d: %w", ln+1, err)
			}
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("line %d: statement outside property: %q", ln+1, line)
		}
		body = append(body, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return props, nil
}

func parseBody(p *Property, body []string) error {
	var main string
	for _, l := range body {
		if strings.HasPrefix(l, "disable iff ") {
			e, err := parseBool(strings.TrimSpace(strings.TrimPrefix(l, "disable iff ")))
			if err != nil {
				return err
			}
			p.DisableIff = e
			continue
		}
		main = l
	}
	if main == "" {
		return fmt.Errorf("property %s: no implication", p.Name)
	}
	op := OpOverlapped
	idx := strings.Index(main, OpOverlapped)
	if i := strings.Index(main, OpNonOverlapped); i >= 0 && (idx < 0 || i < idx) {
		op = OpNonOverlapped
		idx = i
	}
	if idx < 0 {
		return fmt.Errorf("property %s: missing |-> or |=>", p.Name)
	}
	p.Op = op
	ant, err := parseSeq(strings.TrimSpace(main[:idx]))
	if err != nil {
		return fmt.Errorf("property %s antecedent: %w", p.Name, err)
	}
	con, err := parseSeq(strings.TrimSpace(main[idx+len(op):]))
	if err != nil {
		return fmt.Errorf("property %s consequent: %w", p.Name, err)
	}
	p.Ant, p.Con = ant, con
	return nil
}

func parseSeq(s string) (Seq, error) {
	toks := tokenizeSeq(s)
	var seq Seq
	pending := Delay{}
	i := 0
	for i < len(toks) {
		t := toks[i]
		if strings.HasPrefix(t, "##") {
			d, err := parseRange(t[2:])
			if err != nil {
				return seq, err
			}
			pending = d
			i++
			continue
		}
		// collect boolean expr tokens until next ## or [*
		var exprToks []string
		for i < len(toks) && !strings.HasPrefix(toks[i], "##") && !strings.HasPrefix(toks[i], "[*") {
			exprToks = append(exprToks, toks[i])
			i++
		}
		e, err := parseBoolTokens(exprToks)
		if err != nil {
			return seq, err
		}
		item := SeqItem{Delay: pending, Expr: e}
		pending = Delay{}
		if i < len(toks) && strings.HasPrefix(toks[i], "[*") {
			r, err := parseRange(strings.TrimSuffix(strings.TrimPrefix(toks[i], "[*"), "]"))
			if err != nil {
				return seq, err
			}
			item.Repeat = &Repeat{Min: r.Min, Max: r.Max}
			i++
		}
		seq.Items = append(seq.Items, item)
	}
	if len(seq.Items) == 0 {
		return seq, fmt.Errorf("empty sequence")
	}
	return seq, nil
}

func tokenizeSeq(s string) []string {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '#':
			flush()
			j := i
			for j < len(s) && (s[j] == '#' || s[j] == '[' || s[j] == ']' || s[j] == ':' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j - 1
		case c == '[' && i+1 < len(s) && s[i+1] == '*':
			flush()
			j := strings.Index(s[i:], "]")
			if j < 0 {
				toks = append(toks, s[i:])
				i = len(s)
			} else {
				toks = append(toks, s[i:i+j+1])
				i += j
			}
		case c == '(' || c == ')' || c == '&' || c == '|' || c == '!':
			flush()
			toks = append(toks, string(c))
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return toks
}

func parseRange(s string) (Delay, error) {
	s = strings.Trim(s, "[]")
	if i := strings.Index(s, ":"); i >= 0 {
		a, err1 := strconv.Atoi(strings.TrimSpace(s[:i]))
		b, err2 := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if err1 != nil || err2 != nil || a < 0 || b < a {
			return Delay{}, fmt.Errorf("bad range %q", s)
		}
		return Delay{Min: a, Max: b}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return Delay{}, fmt.Errorf("bad delay %q", s)
	}
	return Delay{Min: n, Max: n}, nil
}

func parseBool(s string) (*BoolExpr, error) {
	return parseBoolTokens(tokenizeSeq(s))
}

// parseBoolTokens parses !, &, |, () with precedence ! > & > |.
func parseBoolTokens(toks []string) (*BoolExpr, error) {
	pos := 0
	var parseOr func() (*BoolExpr, error)
	var parseAnd func() (*BoolExpr, error)
	var parseAtom func() (*BoolExpr, error)
	parseOr = func() (*BoolExpr, error) {
		l, err := parseAnd()
		if err != nil {
			return nil, err
		}
		for pos < len(toks) && toks[pos] == "|" {
			pos++
			r, err := parseAnd()
			if err != nil {
				return nil, err
			}
			l = Or(l, r)
		}
		return l, nil
	}
	parseAnd = func() (*BoolExpr, error) {
		l, err := parseAtom()
		if err != nil {
			return nil, err
		}
		for pos < len(toks) && toks[pos] == "&" {
			pos++
			r, err := parseAtom()
			if err != nil {
				return nil, err
			}
			l = And(l, r)
		}
		return l, nil
	}
	parseAtom = func() (*BoolExpr, error) {
		if pos >= len(toks) {
			return nil, fmt.Errorf("unexpected end of expression")
		}
		t := toks[pos]
		switch t {
		case "!":
			pos++
			a, err := parseAtom()
			if err != nil {
				return nil, err
			}
			return Not(a), nil
		case "(":
			pos++
			e, err := parseOr()
			if err != nil {
				return nil, err
			}
			if pos >= len(toks) || toks[pos] != ")" {
				return nil, fmt.Errorf("missing )")
			}
			pos++
			return e, nil
		case ")", "&", "|":
			return nil, fmt.Errorf("unexpected %q", t)
		default:
			pos++
			return Sig(t), nil
		}
	}
	e, err := parseOr()
	if err != nil {
		return nil, err
	}
	if pos != len(toks) {
		return nil, fmt.Errorf("trailing tokens in expression")
	}
	return e, nil
}
