package proplang

import (
	"strings"

	"signalcourt/internal/domain"
	"signalcourt/internal/logic"
)

// Env resolves signal names to four-state words for one sampling step.
type Env interface {
	Word(name string) (domain.Word, bool)
}

// Eval evaluates a boolean expression under a sampled environment and
// returns an explicit three-state verdict. X/Z propagate: any
// contended operand makes the result unknown.
func Eval(n *Node, env Env) logic.Tri {
	if n == nil {
		return logic.TUnknown
	}
	switch n.Kind {
	case "ident":
		w, ok := env.Word(n.Name)
		if !ok {
			return logic.TUnknown
		}
		return logic.FromL(bitOf(w))
	case "lit":
		return evalLit(n.Value)
	case "unary":
		return logic.TNot(Eval(n.Children[0], env))
	case "binary":
		l := Eval(n.Children[0], env)
		r := Eval(n.Children[1], env)
		switch n.Operator {
		case "&&", "&":
			return logic.TAnd(l, r)
		case "||", "|":
			return logic.TOr(l, r)
		case "==":
			return evalEquality(n.Children[0], n.Children[1], env, true)
		case "!=":
			return evalEquality(n.Children[0], n.Children[1], env, false)
		}
	case "group":
		return Eval(n.Children[0], env)
	}
	return logic.TUnknown
}

// bitOf reduces a word to scalar with Z preserved then lifted via
// FromL (Z => unknown).
func bitOf(w domain.Word) logic.L {
	switch w.Scalar() {
	case domain.B0:
		return logic.Zero
	case domain.B1:
		return logic.One
	case domain.BZ:
		return logic.Z
	default:
		return logic.X
	}
}

func evalLit(v string) logic.Tri {
	v = strings.ToLower(v)
	switch {
	case strings.HasPrefix(v, "b"):
		for _, c := range v[1:] {
			if c == 'x' || c == 'z' {
				return logic.TUnknown
			}
		}
		if strings.Contains(v[1:], "1") {
			return logic.TTrue
		}
		return logic.TFalse
	}
	if v == "1" {
		return logic.TTrue
	}
	if v == "0" {
		return logic.TFalse
	}
	return logic.TUnknown
}

// evalEquality supports scalar ==/!= and ident == 'b10xz vector compare.
func evalEquality(a, b *Node, env Env, equal bool) logic.Tri {
	wa, oka := wordOf(a, env)
	wb, okb := wordOf(b, env)
	if !oka || !okb {
		return logic.TUnknown
	}
	if wa.Width() == 0 || wb.Width() == 0 {
		return logic.TUnknown
	}
	if wa.Width() == 1 && wb.Width() == 1 {
		t := logic.FromL(logic.Eq(bitOf(wa), bitOf(wb)))
		if !equal {
			t = logic.TNot(t)
		}
		return t
	}
	// vector: compare common width, right aligned; contended => unknown
	ai, bi := wa.Width()-1, wb.Width()-1
	res := logic.One
	for ai >= 0 && bi >= 0 {
		va := scalarL(wa.Bits[ai])
		vb := scalarL(wb.Bits[bi])
		res = logic.And(res, logic.Eq(va, vb))
		ai--
		bi--
	}
	t := logic.FromL(res)
	if !equal {
		t = logic.TNot(t)
	}
	return t
}

func wordOf(n *Node, env Env) (domain.Word, bool) {
	if n.Kind == "ident" {
		return env.Word(n.Name)
	}
	if n.Kind == "lit" {
		v := strings.TrimPrefix(strings.ToLower(n.Value), "b")
		return domain.NewWord(v), true
	}
	return domain.Word{}, false
}

func scalarL(b uint8) logic.L {
	switch b {
	case domain.B0:
		return logic.Zero
	case domain.B1:
		return logic.One
	case domain.BZ:
		return logic.Z
	default:
		return logic.X
	}
}
