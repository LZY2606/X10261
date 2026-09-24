package property

// WalkSignals collects every signal identifier referenced by a property.
func WalkSignals(p *Prop) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	var walkExpr func(e *Expr)
	walkExpr = func(e *Expr) {
		if e == nil {
			return
		}
		switch e.Kind {
		case ExSignal:
			add(e.Name)
		default:
			for _, c := range e.Children {
				walkExpr(c)
			}
		}
	}
	var walkSeq func(s *Seq)
	walkSeq = func(s *Seq) {
		if s == nil {
			return
		}
		if s.Atom != nil {
			walkExpr(s.Atom)
		}
		walkSeq(s.Left)
		walkSeq(s.Right)
	}
	var walkProp func(pr *Prop)
	walkProp = func(pr *Prop) {
		if pr == nil {
			return
		}
		walkExpr(pr.Cond)
		walkSeq(pr.Seq)
		walkSeq(pr.Ante)
		if pr.Cons != nil {
			walkProp(pr.Cons)
		}
		for _, c := range pr.Children {
			walkProp(c)
		}
	}
	walkProp(p)
	return out
}

// Print returns a canonical-ish source form of the property, used only for
// display; the raw source remains the authoritative material.
func Print(p *Prop) string {
	if p == nil {
		return ""
	}
	switch p.Kind {
	case PropSeq:
		return printSeq(p.Seq)
	case PropNot:
		return "not " + Print(p.Children[0])
	case PropAnd:
		return "(" + Print(p.Children[0]) + " && " + Print(p.Children[1]) + ")"
	case PropOr:
		return "(" + Print(p.Children[0]) + " || " + Print(p.Children[1]) + ")"
	case PropImply, PropImplyNo:
		op := "|->"
		if p.Kind == PropImplyNo {
			op = "|=>"
		}
		return printSeq(p.Ante) + " " + op + " " + Print(p.Cons)
	case PropDisableIff:
		return "disable iff (" + printExpr(p.Cond) + ") " + Print(p.Children[0])
	}
	return ""
}

func printSeq(s *Seq) string {
	switch s.Kind {
	case SeqAtom:
		return printExpr(s.Atom)
	case SeqDelay:
		rng := "##[" + itoa(s.Lo) + ":"
		if s.Hi == Unbounded {
			rng += "$"
		} else {
			rng += itoa(s.Hi)
		}
		rng += "] " + printSeq(s.Right)
		return rng
	case SeqOr:
		return "(" + printSeq(s.Left) + " or " + printSeq(s.Right) + ")"
	}
	return ""
}

func printExpr(e *Expr) string {
	switch e.Kind {
	case ExSignal:
		return e.Name
	case ExConst:
		if e.Const {
			return "true"
		}
		return "false"
	case ExNot:
		return "!" + printExpr(e.Children[0])
	case ExAnd:
		return "(" + printExpr(e.Children[0]) + " && " + printExpr(e.Children[1]) + ")"
	case ExOr:
		return "(" + printExpr(e.Children[0]) + " || " + printExpr(e.Children[1]) + ")"
	case ExXor:
		return "(" + printExpr(e.Children[0]) + " ^ " + printExpr(e.Children[1]) + ")"
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
