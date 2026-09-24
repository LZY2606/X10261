package prop

// Node is an AST node. IDs are stable within one parse of one source text;
// the store assigns globally stable content hashes separately.
type Node struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Src      string `json:"src"`
	Name     string `json:"name,omitempty"`
	Op       string `json:"op,omitempty"`
	Value    string `json:"value,omitempty"`
	Rep      *Rep   `json:"rep,omitempty"`
	Delay    *int   `json:"delay,omitempty"`
	Children []Node `json:"children,omitempty"`
}

// Rep is a sequence repetition [n], [*] (zero-or-more) or [+] (one-or-more).
type Rep struct {
	Text string `json:"text"`
	Min  int    `json:"min"`
	Max  int    `json:"max"` // -1 means unbounded
}

// Spec is a parsed property: an optional disable iff clause plus a property
// expression.
type Spec struct {
	Text      string `json:"text"`
	Disable   *Node  `json:"disable,omitempty"`
	Root      Node   `json:"root"`
	Signals   []string `json:"signals"`
}

// Property source kinds used by the store.
const (
	KindProperty = "property"
	KindSequence = "sequence"
)
