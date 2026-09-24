package property

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func contentID(kind string, v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(append([]byte("sc:"+kind+":"), b...))
	return kind + "_" + hex.EncodeToString(sum[:10])
}

// AssignIDs fills NodeID fields deterministically from node contents.
func (m *Module) AssignIDs() {
	for _, d := range m.Decls {
		d.NodeID = contentID("decl", []string{d.Name, d.Property.assignIDs()})
	}
	key := []any{m.Name, m.ClockEdge, m.ClockName, m.ResetName}
	ids := make([]string, len(m.Decls))
	for i, d := range m.Decls {
		ids[i] = d.NodeID
	}
	key = append(key, ids)
	m.NodeID = contentID("mod", key)
}

func (p *Property) assignIDs() string {
	if p == nil {
		return ""
	}
	switch p.Kind {
	case PropSeq:
		p.NodeID = contentID("pseq", []any{p.Strength, p.Seq.assignIDs()})
	case PropImp:
		p.NodeID = contentID("pimp", []any{p.Ante.assignIDs(), p.Overlapped, p.Cons.assignIDs()})
	case PropDisable:
		id := p.Inner.assignIDs()
		cid := boolID(p.DisableCond)
		p.NodeID = contentID("pdis", []any{cid, id})
	}
	return p.NodeID
}

func (s *Sequence) assignIDs() string {
	if s == nil {
		return ""
	}
	switch s.Kind {
	case SeqLit:
		s.NodeID = contentID("slit", []any{boolID(s.Cond), s.HasRep, s.RepMin, s.RepMax})
	case SeqEmpty:
		s.NodeID = contentID("semp", nil)
	case SeqDelay:
		s.NodeID = contentID("sdly", []any{s.DelayLo, s.DelayHi, s.Left.assignIDs(), s.Right.assignIDs()})
	case SeqOr, SeqAnd:
		s.NodeID = contentID(string(s.Kind), []any{s.Left.assignIDs(), s.Right.assignIDs()})
	}
	return s.NodeID
}

func boolID(e *BoolExpr) string {
	if e == nil {
		return ""
	}
	key := []any{string(e.Op), e.Name, e.Const}
	for _, a := range e.Args {
		key = append(key, boolID(a))
	}
	e.NodeID = contentID("bool", key)
	return e.NodeID
}
