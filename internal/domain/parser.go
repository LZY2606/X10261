package domain

import (
	"fmt"
	"strconv"
	"strings"
)

type token struct {
	text string
	pos  int
}

type lexer struct {
	tokens []token
	pos    int
}

func ParseProperties(fileName string, raw []byte, defaultSemantic string) (*PropertyFile, error) {
	semantics := defaultSemantic
	var props []Property
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			if rest, ok := strings.CutPrefix(trimmed, "// semantics:"); ok {
				semantics = strings.TrimSpace(rest)
			}
			continue
		}
		trimmed = strings.TrimSuffix(strings.TrimSpace(trimmed), ";")
		fields := strings.Fields(trimmed)
		if len(fields) < 3 || fields[0] != "property" {
			return nil, fmt.Errorf("%s: expected property name @domain body", fileName)
		}
		name := fields[1]
		domain := ""
		body := strings.Join(fields[2:], " ")
		if at := strings.Index(body, "@"); at >= 0 {
			rest := strings.Fields(body[at+1:])
			domain = rest[0]
			body = strings.TrimSpace(body[:at]) + " " + strings.TrimPrefix(rest[0]+" ", "")
			body = strings.TrimSpace(strings.Join(fields[2:], " ")[:strings.Index(strings.Join(fields[2:], " "), "@")])
		}
		lex := newLexer(body)
		prop := Property{Name: name, Domain: domain, SourceText: trimmed, SemanticVersion: semantics}
		var err error
		if strings.HasPrefix(strings.ToLower(body), "disable iff") {
			prop.Reset, lex, err = parseDisable(body)
			if err != nil {
				return nil, fmt.Errorf("%s:%s %w", fileName, name, err)
			}
		}
		prop.Antecedent, prop.Consequent, prop.Overlapped, err = parsePropertySeq(lex)
		if err != nil {
			return nil, fmt.Errorf("%s:%s %w", fileName, name, err)
		}
		prop.AST = renderAST(prop)
		prop.ASTHash = StableID("ast", fileName, name, prop.AST, semantics)[4:]
		props = append(props, prop)
	}
	return &PropertyFile{
		ID: StableID("props", fileName, string(raw), semantics), FileName: fileName, RawBytes: append([]byte(nil), raw...),
		RawSHA256: sha256Hex(raw), SemanticVersion: semantics, Properties: props,
	}, nil
}

func newLexer(input string) *lexer {
	replacer := strings.NewReplacer("(", " ( ", ")", " ) ", "|->", " |-> ", "|=>", " |=> ", "##[", " ##[ ", ",", " , ", "]", " ] ", "&&", " && ", "||", " || ", "==", " == ", "!", " ! ")
	fields := strings.Fields(replacer.Replace(input))
	tokens := make([]token, 0, len(fields))
	for _, item := range fields {
		tokens = append(tokens, token{text: item, pos: 0})
	}
	return &lexer{tokens: tokens}
}

func parseDisable(body string) (BoolExpr, *lexer, error) {
	open := strings.Index(body, "(")
	closeAt := strings.Index(body[open:], ")")
	if open < 0 || closeAt < 0 {
		return nil, nil, fmt.Errorf("disable iff must contain parentheses")
	}
	closeAt += open
	expr, err := parseBool(newLexer(body[open+1 : closeAt]))
	if err != nil {
		return nil, nil, err
	}
	return expr, newLexer(body[closeAt+1:]), nil
}

func parsePropertySeq(lex *lexer) (SeqExpr, *SeqExpr, bool, error) {
	left, err := parseSeq(lex)
	if err != nil {
		return nil, nil, false, err
}
	if lex.peek() != "|->" && lex.peek() != "|=>" {
		return left, nil, true, nil
	}
	op := lex.next()
	right, err := parseSeq(lex)
	if err != nil {
		return nil, nil, false, err
	}
	return left, &right, op == "|->", nil
}

func parseSeq(lex *lexer) (SeqExpr, error) {
	first, err := parseSeqAtom(lex)
	if err != nil {
		return nil, err
	}
	if lex.peek() != "##[" {
		return first, nil
	}
	lex.next()
	minText := lex.next()
	if minText.text == "" {
		return nil, fmt.Errorf("expected delay minimum")
	}
	minValue, err := strconv.Atoi(minText.text)
	if err != nil {
		return nil, fmt.Errorf("delay minimum must be an integer")
	}
		maxValue := minValue
		if lex.peek() == "," {
			lex.next()
			maxToken := lex.next()
			if maxToken.text == "$" {
				maxValue = 64
			} else if isIdentifier(maxToken.text) {
				right, err := parseSeqAtom(lex)
				if err != nil {
					return nil, err
				}
				return SeqDelay{Left: first, Right: right, Min: minValue, Max: 64, MaxParameter: maxToken.text}, nil
			} else {
			maxValue, err = strconv.Atoi(maxToken.text)
			if err != nil {
				return nil, fmt.Errorf("delay maximum must be an integer or $")
			}
		}
	}
	if lex.next().text != "]" {
		return nil, fmt.Errorf("delay must end with ]")
	}
	right, err := parseSeqAtom(lex)
	if err != nil {
		return nil, err
	}
	return SeqDelay{Left: first, Right: right, Min: minValue, Max: maxValue}, nil
}

func parseSeqAtom(lex *lexer) (SeqExpr, error) {
	if lex.peek() == "(" {
		lex.next()
		expr, err := parseBool(lex)
		if err != nil {
			return nil, err
		}
		if lex.next().text != ")" {
			return nil, fmt.Errorf("missing )")
		}
		return SeqBool{Expr: expr}, nil
	}
	expr, err := parseBool(lex)
	if err != nil {
		return nil, err
	}
	return SeqBool{Expr: expr}, nil
}

func (l *lexer) peek() string {
	if l.pos >= len(l.tokens) {
		return ""
	}
	return l.tokens[l.pos].text
}

func (l *lexer) next() token {
	if l.pos >= len(l.tokens) {
		return token{}
	}
	t := l.tokens[l.pos]
	l.pos++
	return t
}

func parseBool(lex *lexer) (BoolExpr, error) { return parseOr(lex) }

func parseOr(lex *lexer) (BoolExpr, error) {
	left, err := parseAnd(lex)
	if err != nil {
		return nil, err
	}
	var items []BoolExpr
	for lex.peek() == "||" {
		lex.next()
		right, err := parseAnd(lex)
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	if items == nil {
		return left, nil
	}
	return BoolOr{Items: append([]BoolExpr{left}, items...)}, nil
}

func parseAnd(lex *lexer) (BoolExpr, error) {
	left, err := parseEq(lex)
	if err != nil {
		return nil, err
	}
	var items []BoolExpr
	for lex.peek() == "&&" {
		lex.next()
		right, err := parseEq(lex)
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	if items == nil {
		return left, nil
	}
	return BoolAnd{Items: append([]BoolExpr{left}, items...)}, nil
}

func parseEq(lex *lexer) (BoolExpr, error) {
	left, err := parseUnary(lex)
	if err != nil {
		return nil, err
	}
	if lex.peek() != "==" {
		return left, nil
	}
	lex.next()
	right, err := parseUnary(lex)
	if err != nil {
		return nil, err
	}
	return BoolEq{Left: left, Right: right}, nil
}

func parseUnary(lex *lexer) (BoolExpr, error) {
	if lex.peek() == "!" {
		lex.next()
		inner, err := parseUnary(lex)
		if err != nil {
			return nil, err
		}
		return BoolNot{Inner: inner}, nil
	}
	if lex.peek() == "(" {
		lex.next()
		inner, err := parseOr(lex)
		if err != nil {
			return nil, err
		}
		if lex.next().text != ")" {
			return nil, fmt.Errorf("missing )")
		}
		return inner, nil
}
	tok := lex.next()
	if tok.text == "" {
		return nil, fmt.Errorf("expected boolean expression")
	}
	switch tok.text {
	case "1'b0", "1'b1":
		return BoolLiteral{Value: string(tok.text[3])}, nil
	case "0", "1":
		return BoolLiteral{Value: tok.text}, nil
	default:
		if isIdentifier(tok.text) {
			return BoolSignal{Name: tok.text}, nil
		}
		return nil, fmt.Errorf("invalid boolean token %q", tok.text)
	}
}

func isIdentifier(text string) bool {
	for i, r := range text {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return text != ""
}

func renderAST(p Property) string {
	parts := []string{"property", p.Name, "@"+p.Domain}
	if p.Reset != nil {
		parts = append(parts, "disable_iff("+renderBool(p.Reset)+")")
	}
	parts = append(parts, renderSeq(p.Antecedent))
	if p.Consequent != nil {
		op := "|->"
		if !p.Overlapped {
			op = "|=>"
		}
		parts = append(parts, op, renderSeq(*p.Consequent))
	}
	return strings.Join(parts, " ")
}

func renderSeq(e SeqExpr) string {
	switch n := e.(type) {
	case SeqBool:
		return "(" + renderBool(n.Expr) + ")"
	case SeqDelay:
		maxText := strconv.Itoa(n.Max)
		if n.MaxParameter != "" {
			maxText = n.MaxParameter
		}
		return renderSeq(n.Left) + " ##[" + strconv.Itoa(n.Min) + ":" + maxText + "] " + renderSeq(n.Right)
	}
	return ""
}

func renderBool(e BoolExpr) string {
	switch n := e.(type) {
	case BoolSignal:
		return n.Name
	case BoolLiteral:
		return "1'b" + n.Value
	case BoolNot:
		return "!(" + renderBool(n.Inner) + ")"
	case BoolAnd:
		out := make([]string, len(n.Items))
		for i, item := range n.Items {
			out[i] = renderBool(item)
		}
		return "(" + strings.Join(append([]string{renderBool(n.Items[0])}, out...), " && ") + ")"
	case BoolOr:
		out := make([]string, len(n.Items))
		for i, item := range n.Items {
			out[i] = renderBool(item)
		}
		return "(" + strings.Join(append([]string{renderBool(n.Items[0])}, out...), " || ") + ")"
	case BoolEq:
		return "(" + renderBool(n.Left) + " == " + renderBool(n.Right) + ")"
	}
	return ""
}
