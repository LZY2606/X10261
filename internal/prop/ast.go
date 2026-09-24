// Package prop 定义序列属性语言的 AST 与解析器。
// 语言特性：四态布尔表达式、seq 连接、[*m..n] 重复（[*0] 空匹配）、
// overlapped(|->)/non-overlapped(|=>) implication、disable iff。
package prop

// Node 是属性 AST 节点。
type Node interface{ node() }

// Bool 是四态布尔表达式（原文保留，求值在派生层进行）。
type Bool struct{ Expr string }

// Delay 是 ##N 拍延迟。
type Delay struct{ N int }

// Seq 是拍级连接。
type Seq struct{ Items []Node }

// Rep 是 [*Min..Max] 重复；Min=0 时允许空匹配。
type Rep struct {
	Inner    Node
	Min, Max int
}

// Impl 是 implication；Overlapped 区分 |-> 与 |=>。
type Impl struct {
	Ant, Cons  Node
	Overlapped bool
}

// Disable 是 disable iff 包裹。
type Disable struct {
	Cond  string
	Inner Node
}

func (Bool) node()    {}
func (Delay) node()   {}
func (Seq) node()     {}
func (Rep) node()     {}
func (Impl) node()    {}
func (Disable) node() {}

// Prop 是一条命名属性，绑定采样时钟。
type Prop struct {
	Name  string `json:"name"`
	Clock string `json:"clock"`
	Root  Node   `json:"-"`
	Text  string `json:"text"` // 原始文本，导入时保留
}

// File 是解析出的属性文件，AST 与语义版本一起保存。
type File struct {
	Props []*Prop
}

// Automaton 是从 AST 派生的可视自动机（节点/边），仅供展示。
type Automaton struct {
	Nodes []ANode `json:"nodes"`
	Edges []AEdge `json:"edges"`
}

type ANode struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // start|bool|rep|accept|impl
}

type AEdge struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label"`
}

// BuildAutomaton 把 AST 展平为可视自动机。
func BuildAutomaton(p *Prop) Automaton {
	a := Automaton{}
	next := 0
	add := func(label, kind string) int {
		id := next
		next++
		a.Nodes = append(a.Nodes, ANode{ID: id, Label: label, Kind: kind})
		return id
	}
	start := add("start:"+p.Name+" @"+p.Clock, "start")
	accept := add("accept", "accept")
	var walk func(n Node, from int) []int
	walk = func(n Node, from int) []int {
		switch v := n.(type) {
		case Bool:
			id := add(v.Expr, "bool")
			a.Edges = append(a.Edges, AEdge{From: from, To: id, Label: "sample"})
			return []int{id}
		case Delay:
			return []int{from} // 延迟不改变状态结构，标注在边上从略
		case Seq:
			cur := []int{from}
			for _, it := range v.Items {
				var nxt []int
				for _, c := range cur {
					nxt = append(nxt, walk(it, c)...)
				}
				cur = nxt
			}
			return cur
		case Rep:
			id := add("rep[*"+itoa(v.Min)+".."+itoa(v.Max)+"]", "rep")
			a.Edges = append(a.Edges, AEdge{From: from, To: id, Label: "enter"})
			outs := walk(v.Inner, id)
			for _, o := range outs {
				a.Edges = append(a.Edges, AEdge{From: o, To: id, Label: "loop"})
			}
			if v.Min == 0 {
				// 空匹配：可直通
				return append(outs, from)
			}
			return outs
		case Impl:
			op := "|->"
			if !v.Overlapped {
				op = "|=>"
			}
			ants := walk(v.Ant, from)
			var outs []int
			for _, an := range ants {
				outs = append(outs, walk(v.Cons, an)...)
			}
			for _, an := range ants {
				for _, o := range outs {
					_ = an
					_ = o
				}
			}
			// 在 ant 结束边上标注算子
			for i := range a.Edges {
				for _, an := range ants {
					if a.Edges[i].To == an && a.Edges[i].Label == "sample" {
						a.Edges[i].Label = "sample " + op
					}
				}
			}
			return outs
		case Disable:
			id := add("disable iff ("+v.Cond+")", "impl")
			a.Edges = append(a.Edges, AEdge{From: from, To: id, Label: "guard"})
			return walk(v.Inner, id)
		}
		return []int{from}
	}
	for _, o := range walk(p.Root, start) {
		a.Edges = append(a.Edges, AEdge{From: o, To: accept, Label: "done"})
	}
	return a
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
