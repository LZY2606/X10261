package prop

import "fmt"

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tLParen
	tRParen
	tLBrack
	tRBrack
	tStar
	tPlus
	tMinus
	tBang
	tAmpAmp
	tPipePipe
	tEqEq
	tNotEq
	tImplO   // |->
	tImplNO  // |=>
	tHash    // ##
	tDisable // disable iff
	tProp
	tSeq
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "(", i}); i++
		case c == ')':
			toks = append(toks, token{tRParen, ")", i}); i++
		case c == '[':
			toks = append(toks, token{tLBrack, "[", i}); i++
		case c == ']':
			toks = append(toks, token{tRBrack, "]", i}); i++
		case c == '*':
			toks = append(toks, token{tStar, "*", i}); i++
		case c == '+':
			toks = append(toks, token{tPlus, "+", i}); i++
		case c == '!':
			if i+1 < len(src) && src[i+1] == '=' {
				toks = append(toks, token{tNotEq, "!=", i}); i += 2
			} else {
				toks = append(toks, token{tBang, "!", i}); i++
			}
		case c == '-':
			toks = append(toks, token{tMinus, "-", i}); i++
		case c == '&' && i+1 < len(src) && src[i+1] == '&':
			toks = append(toks, token{tAmpAmp, "&&", i}); i += 2
		case c == '|':
			if i+1 < len(src) && src[i+1] == '-' {
				toks = append(toks, token{tImplO, "|->", i}); i += 3
			} else if i+1 < len(src) && src[i+1] == '=' {
				toks = append(toks, token{tImplNO, "|=>", i}); i += 3
			} else if i+1 < len(src) && src[i+1] == '|' {
				toks = append(toks, token{tPipePipe, "||", i}); i += 2
			} else {
				return nil, fmt.Errorf("lex: stray '|' at offset %d", i)
			}
		case c == '=' && i+1 < len(src) && src[i+1] == '=':
			toks = append(toks, token{tEqEq, "==", i}); i += 2
		case c == '#' && i+1 < len(src) && src[i+1] == '#':
			toks = append(toks, token{tHash, "##", i}); i += 2
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			toks = append(toks, token{tNumber, src[i:j], i}); i = j
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			toks = append(toks, token{tIdent, src[i:j], i}); i = j
		default:
			return nil, fmt.Errorf("lex: unexpected character %q at offset %d", string(c), i)
		}
	}
	toks = append(toks, token{tEOF, "", i})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
