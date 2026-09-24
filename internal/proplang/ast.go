// Package proplang defines the small SVA-flavoured property language
// used by the verification team. Properties are parsed to an AST that
// keeps source spans and a numeric node identity; evaluation yields an
// explicit three-state verdict (never a silent binary collapse).
package proplang

// Node is one AST node. ID is a stable depth-first pre-order index
// within the property; Span is a byte range in the source text.
type Node struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	Span   [2]int `json:"span"`
	Text   string `json:"text"`
	Children []*Node `json:"-"`
	// leaf payloads
	Name     string `json:"name,omitempty"`     // ident
	Operator string `json:"operator,omitempty"` // unary/binary/property operators
	Value    string `json:"value,omitempty"`    // literal token
	Repeat   int    `json:"repeat,omitempty"`
}

// Property is one parsed assertion with its semantic version and
// optional sampled clock / disable guard.
type Property struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Clock    string `json:"clock,omitempty"`
	Disable  *Node  `json:"disable,omitempty"`
	DisableExpr string `json:"disableExpr,omitempty"`
	Root     *Node  `json:"root"`
	Text     string `json:"text"`
	SemVer   string `json:"semver"`
	PragmaAt int    `json:"-"`
}

// SemVersion is a reference semantics version.
type SemVersion struct {
	Major int
	Minor int
	Raw   string
}

// All nodes carry Kind values understood by the automaton renderer:
// ident lit not and or implies nonimpl repeat seq-expr disable prop
