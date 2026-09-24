package property

import "fmt"

type tokKind string

const (
	tEOF       tokKind = "eof"
	tIdent     tokKind = "ident"
	tNum       tokKind = "num"
	tNot       tokKind = "!"
	tAnd       tokKind = "&&"
	tOr        tokKind = "||"
	tXor       tokKind = "^"
	tLParen    tokKind = "("
	tRParen    tokKind = ")"
	tLBrack    tokKind = "["
	tRBrack    tokKind = "]"
	tColon     tokKind = ":"
	tImplOv    tokKind = "|->"
	tImplNo    tokKind = "|=>"
	tDisable   tokKind = "disable"
	tIff       tokKind = "iff"
	tSeqOr     tokKind = "or"
	tTrue      tokKind = "true"
	tFalse     tokKind = "false"
	tHash      tokKind = "#"
	tDollar    tokKind = "$"
)

type token struct {
	kind tokKind
	text string
	off  int
}

type lexError struct{ off int; msg string }

func (e *lexError) Error() string { return fmt.Sprintf("lex offset %d: %s", e.off, e.msg) }

func lex(src string) ([]token, error) {
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
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 >= len(src) {
				return nil, &lexError{off: i, msg: "unterminated block comment"}
			}
			i += 2
		case isIdentStart(c):
			start := i
			for i < len(src) && isIdentPart(src[i]) {
				i++
			}
			text := src[start:i]
			switch text {
			case "disable":
				toks = append(toks, token{tDisable, text, start})
			case "iff":
				toks = append(toks, token{tIff, text, start})
			case "or":
				toks = append(toks, token{tSeqOr, text, start})
			case "true":
				toks = append(toks, token{tTrue, text, start})
			case "false":
				toks = append(toks, token{tFalse, text, start})
			default:
				toks = append(toks, token{tIdent, text, start})
			}
		case c >= '0' && c <= '9':
			start := i
			for i < len(src) && src[i] >= '0' && src[i] <= '9' {
				i++
			}
			toks = append(toks, token{tNum, src[start:i], start})
		case c == '|' && i+2 < len(src) && src[i+1] == '-' && src[i+2] == '>':
			toks = append(toks, token{tImplOv, "|->", i})
			i += 3
		case c == '|' && i+2 < len(src) && src[i+1] == '=' && src[i+2] == '>':
			toks = append(toks, token{tImplNo, "|=>", i})
			i += 3
		case c == '|' && i+1 < len(src) && src[i+1] == '|':
			toks = append(toks, token{tOr, "||", i})
			i += 2
		case c == '&' && i+1 < len(src) && src[i+1] == '&':
			toks = append(toks, token{tAnd, "&&", i})
			i += 2
		default:
			switch c {
			case '!':
				toks = append(toks, token{tNot, "!", i})
			case '^':
				toks = append(toks, token{tXor, "^", i})
			case '(':
				toks = append(toks, token{tLParen, "(", i})
			case ')':
				toks = append(toks, token{tRParen, ")", i})
			case '[':
				toks = append(toks, token{tLBrack, "[", i})
			case ']':
				toks = append(toks, token{tRBrack, "]", i})
			case ':':
				toks = append(toks, token{tColon, ":", i})
			case '#':
				toks = append(toks, token{tHash, "#", i})
			case '$':
				toks = append(toks, token{tDollar, "$", i})
			default:
				return nil, &lexError{off: i, msg: "unexpected character " + string(c)}
			}
			i++
		}
	}
	toks = append(toks, token{tEOF, "", len(src)})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.'
}
