package property

import (
	"fmt"
	"unicode"
)

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tKeyword
	tOp
	tColon
	tSemi
	tLParen
	tRParen
	tLBrack
	tRBrack
	tComma
	tStar
	tDot
)

type lexTok struct {
	kind tokKind
	text string
	line int
	col  int
}

var keywords = map[string]bool{
	"version": true, "clock": true, "reset": true, "posedge": true,
	"negedge": true, "edge": true, "async_low": true, "async_high": true,
	"sync_low": true, "sync_high": true, "property": true, "or": true,
	"and": true, "not": true, "disable": true, "iff": true,
	"strong": true, "weak": true, "true": true, "false": true,
}

func lex(src string) ([]lexTok, error) {
	var out []lexTok
	line, col := 1, 1
	add := func(k tokKind, text string, l, c int) {
		out = append(out, lexTok{k, text, l, c})
	}
	runes := []rune(src)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '\n':
			line++
			col = 1
			i++
		case unicode.IsSpace(r):
			i++
			col++
		case r == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				if runes[i] == '\n' {
					line++
					col = 1
				}
				i++
			}
			i += 2
		case r == '(':
			add(tLParen, "(", line, col)
			i++
			col++
		case r == ')':
			add(tRParen, ")", line, col)
			i++
			col++
		case r == '[':
			add(tLBrack, "[", line, col)
			i++
			col++
		case r == ']':
			add(tRBrack, "]", line, col)
			i++
			col++
		case r == ',':
			add(tComma, ",", line, col)
			i++
			col++
		case r == ':':
			add(tColon, ":", line, col)
			i++
			col++
		case r == ';':
			add(tSemi, ";", line, col)
			i++
			col++
		case r == '.':
			add(tDot, ".", line, col)
			i++
			col++
		case r == '*':
			add(tStar, "*", line, col)
			i++
			col++
		case r == '#':
			if i+1 < len(runes) && runes[i+1] == '#' {
				add(tOp, "##", line, col)
				i += 2
				col += 2
				continue
			}
			return nil, fmt.Errorf("property: stray '#' at %d:%d", line, col)
		case r == '|':
			if i+2 < len(runes) && runes[i+1] == '-' && runes[i+2] == '>' {
				add(tOp, "|->", line, col)
				i += 3
				col += 3
				continue
			}
			if i+2 < len(runes) && runes[i+1] == '=' && runes[i+2] == '>' {
				add(tOp, "|=>", line, col)
				i += 3
				col += 3
				continue
			}
			return nil, fmt.Errorf("property: stray '|' at %d:%d", line, col)
		case r == '&' && i+1 < len(runes) && runes[i+1] == '&':
			add(tOp, "&&", line, col)
			i += 2
			col += 2
		case r == '|' && i+1 < len(runes) && runes[i+1] == '|':
			add(tOp, "||", line, col)
			i += 2
			col += 2
		case r == '=' && i+1 < len(runes) && runes[i+1] == '=':
			add(tOp, "==", line, col)
			i += 2
			col += 2
		case r == '!' && i+1 < len(runes) && runes[i+1] == '=':
			add(tOp, "!=", line, col)
			i += 2
			col += 2
		case r == '!':
			add(tOp, "!", line, col)
			i++
			col++
		case r == '-' && i+1 < len(runes) && runes[i+1] == '>':
			add(tOp, "->", line, col)
			i += 2
			col += 2
		case unicode.IsLetter(r) || r == '_':
			l, c := line, col
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
				col++
			}
			word := string(runes[start:i])
			if keywords[word] {
				add(tKeyword, word, l, c)
			} else {
				add(tIdent, word, l, c)
			}
		case unicode.IsDigit(r):
			l, c := line, col
			start := i
			for i < len(runes) && unicode.IsDigit(runes[i]) {
				i++
				col++
			}
			add(tNum, string(runes[start:i]), l, c)
		default:
			return nil, fmt.Errorf("property: unexpected character %q at %d:%d", r, line, col)
		}
	}
	add(tEOF, "", line, col)
	return out, nil
}
