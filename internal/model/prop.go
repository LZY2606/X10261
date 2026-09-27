package model

// NodeKind enumerates the small SVA-style sequence/property language.
type NodeKind string

const (
	NProposition NodeKind = "proposition" // signal-level boolean (4-state)
	NEmpty       NodeKind = "empty"       // explicitly zero-length matching window
	NConcat      NodeKind = "concat"      // A ##[l:h] B
	NRepeat      NodeKind = "repeat"      // A[*l:h]
	NNot         NodeKind = "not"
	NAnd         NodeKind = "and"
	NOr          NodeKind = "or"
	NImplication NodeKind = "implication" // |-> / |=>
	NDisable     NodeKind = "disable_iff"  // disable iff (sig) expr
)

// PropNode is one AST node. Stable NodeID survives re-parse of same bytes.
type PropNode struct {
	ID     string   `json:"id"`
	Kind   NodeKind `json:"kind"`
	Text   string   `json:"text"`
	Signal string   `json:"signal,omitempty"` // proposition subject (raw token)
	Children []string `json:"children,omitempty"`
	Lo, Hi  int      `json:"lo,omitempty"` // repetition / concat window
	Overlap bool     `json:"overlap,omitempty"` // implication overlaps?
}

// Property is one named assertion with semantics version.
type Property struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Text     string              `json:"text"`
	DomainID string              `json:"domain_id"`
	RootID   string              `json:"root_id"`
	SemVer   string              `json:"semantics_version"`
	Nodes    map[string]*PropNode `json:"nodes"`
}

// PropSource is an imported property file: raw bytes + AST, version retained.
type PropSource struct {
	ID      string     `json:"id"`
	Path    string     `json:"path"`
	ByteHash string    `json:"byte_hash"`
	Bytes   []byte     `json:"-"`
	SemVer  string     `json:"semantics_version"`
	Properties []*Property `json:"properties"`
}
