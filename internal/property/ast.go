// Package property defines the small sequence/property language used by the
// signal-court case pack, its parser, and parameterized semantic versions.
//
// The grammar is intentionally tiny but covers the judgement-rule matrix:
//
//	expr     := id | "!" expr | expr ("&&"|"||") expr
//	sequence := expr
//	         |  "(" sequence ")"
//	         |  sequence "##" int sequence
//	         |  sequence "##[" int ":" int "]" sequence
//	         |  sequence "[*" int "]"
//	         |  "empty"            // the empty sequence, explicit vacuity source
//	property := sequence
//	         |  sequence "|->" sequence            // overlapped implication
//	         |  sequence "|=>" sequence            // non-overlapped
//	         |  "disable iff " "(" expr ")" property
package property

// Node is any AST node. Src is the exact source span for provenance.
type Node interface{ nodeKind() string }

// Expr nodes.
type (
	IdentExpr struct {
		Name string
		Src  string
	}
	NotExpr struct {
		Inner Expr
		Src   string
	}
	BinExpr struct {
		Op       string // "&&","||"
		Lhs, Rhs Expr
		Src      string
	}
)

// Expr is a boolean (four-state evaluated) expression.
type Expr interface {
	Node
	exprNode()
}

func (*IdentExpr) nodeKind() string { return "ident" }
func (*NotExpr) nodeKind() string   { return "not" }
func (*BinExpr) nodeKind() string   { return "binop" }
func (*IdentExpr) exprNode()        {}
func (*NotExpr) exprNode()          {}
func (*BinExpr) exprNode()          {}

// Seq nodes.
type (
	ExprSeq struct {
		Expr Expr
		Src  string
	}
	EmptySeq struct {
		Src string // "empty": matches with length zero on purpose
	}
	DelaySeq struct {
		Prefix Seq
		Lo, Hi int // ##[lo:hi]; fixed ##N => Lo==Hi==N
		Suffix Seq
		Src    string
	}
	RepeatSeq struct {
		Inner Seq
		Count int
		Src   string
	}
)

// Seq is a sequence.
type Seq interface {
	Node
	seqNode()
}

func (*ExprSeq) nodeKind() string   { return "expr-seq" }
func (*EmptySeq) nodeKind() string  { return "empty-seq" }
func (*DelaySeq) nodeKind() string  { return "delay-seq" }
func (*RepeatSeq) nodeKind() string { return "repeat-seq" }
func (*ExprSeq) seqNode()           {}
func (*EmptySeq) seqNode()          {}
func (*DelaySeq) seqNode()          {}
func (*RepeatSeq) seqNode()         {}

// Property nodes.
type (
	SeqProp struct {
		Seq Seq
		Src string
	}
	ImplyProp struct {
		Antecedent Seq
		Op         string // "|->" overlapped, "|=>" non-overlapped
		Consequent Seq
		Src        string
	}
	DisableProp struct {
		Cond Expr
		Body Prop
		Src  string
	}
)

// Prop is a top-level assertion property.
type Prop interface {
	Node
	propNode()
}

func (*SeqProp) nodeKind() string     { return "seq-prop" }
func (*ImplyProp) nodeKind() string   { return "imply-prop" }
func (*DisableProp) nodeKind() string { return "disable-prop" }
func (*SeqProp) propNode()            {}
func (*ImplyProp) propNode()          {}
func (*DisableProp) propNode()        {}

// Walk traverses an AST.
func Walk(n Node, f func(Node) bool) {
	if n == nil || !f(n) {
		return
	}
	switch t := n.(type) {
	case *NotExpr:
		Walk(t.Inner, f)
	case *BinExpr:
		Walk(t.Lhs, f)
		Walk(t.Rhs, f)
	case *ExprSeq:
		Walk(t.Expr, f)
	case *DelaySeq:
		Walk(t.Prefix, f)
		Walk(t.Suffix, f)
	case *RepeatSeq:
		Walk(t.Inner, f)
	case *SeqProp:
		Walk(t.Seq, f)
	case *ImplyProp:
		Walk(t.Antecedent, f)
		Walk(t.Consequent, f)
	case *DisableProp:
		Walk(t.Cond, f)
		Walk(t.Body, f)
	}
}

// Idents collects referenced signal names in first-visit order.
func Idents(n Node) []string {
	var out []string
	seen := map[string]bool{}
	Walk(n, func(x Node) bool {
		if id, ok := x.(*IdentExpr); ok && !seen[id.Name] {
			seen[id.Name] = true
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// Print renders the AST back to canonical source text.
func Print(n Node) string {
	switch t := n.(type) {
	case Expr:
		return printExpr(t)
	case Seq:
		return printSeq(t)
	case Prop:
		return printProp(t)
	}
	return ""
}

func printExpr(e Expr) string {
	switch t := e.(type) {
	case *IdentExpr:
		return t.Name
	case *NotExpr:
		// BinExpr already renders its own parentheses.
		if _, ok := t.Inner.(*BinExpr); ok {
			return "!" + printExpr(t.Inner)
		}
		return "!(" + printExpr(t.Inner) + ")"
	case *BinExpr:
		return "(" + printExpr(t.Lhs) + " " + t.Op + " " + printExpr(t.Rhs) + ")"
	}
	return ""
}

func printSeq(s Seq) string {
	switch t := s.(type) {
	case *ExprSeq:
		return printExpr(t.Expr)
	case *EmptySeq:
		return "empty"
	case *DelaySeq:
		if t.Lo == t.Hi {
			return printSeq(t.Prefix) + " ##" + itoa(t.Lo) + " " + printSeq(t.Suffix)
		}
		return printSeq(t.Prefix) + " ##[" + itoa(t.Lo) + ":" + itoa(t.Hi) + "] " + printSeq(t.Suffix)
	case *RepeatSeq:
		inner := printSeq(t.Inner)
		if _, ok := t.Inner.(*ExprSeq); !ok {
			inner = "(" + inner + ")"
		}
		return inner + "[*" + itoa(t.Count) + "]"
	}
	return ""
}

func printProp(p Prop) string {
	switch t := p.(type) {
	case *SeqProp:
		return printSeq(t.Seq)
	case *ImplyProp:
		return printSeq(t.Antecedent) + " " + t.Op + " " + printSeq(t.Consequent)
	case *DisableProp:
		return "disable iff (" + printExpr(t.Cond) + ") " + printProp(t.Body)
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
