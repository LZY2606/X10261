package ssp

import "signalcourt/internal/model"

// assignIDs gives every node a content-derived identity that is stable across
// imports and branches. The property ID also binds the semantic version, so the
// same text under two versions yields two readable interpretations rather than
// one silently overwriting the other.
func assignIDs(p *Property, sourceID string) {
	var bID func(b *BoolNode) string
	bID = func(b *BoolNode) string {
		if b == nil {
			return ""
		}
		l := bID(b.Left)
		r := bID(b.Right)
		b.ID = model.HashID("astb", sourceID, string(b.Op), b.Name, l, r)
		return b.ID
	}
	var sID func(s *SeqNode) string
	sID = func(s *SeqNode) string {
		if s == nil {
			return ""
		}
		b := bID(s.Bool)
		l := sID(s.Left)
		r := sID(s.Right)
		s.ID = model.HashID("asts", sourceID, string(s.Kind), b, l, r,
			itoa(s.Lo), itoa(s.Hi))
		return s.ID
	}
	bID(p.Disable)
	if p.Bare {
		sID(p.BareSeq)
	} else {
		sID(p.Ante)
		sID(p.Cons)
	}
	ante := ""
	if p.Ante != nil {
		ante = p.Ante.ID
	}
	cons := ""
	if p.Cons != nil {
		cons = p.Cons.ID
	}
	disable := bID(p.Disable)
	bare := sID(p.BareSeq)
	kind := "impl"
	if p.Bare {
		kind = "bare"
	}
	ov := "overlap"
	if !p.Overlap && !p.Bare {
		ov = "nonoverlap"
	}
	p.ID = model.HashID("astp", sourceID, p.Semver, kind, ov, p.Name, disable, ante, cons, bare)
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

// Signals returns the set of signal names referenced by a property.
func (p *Property) Signals() []string {
	seen := map[string]bool{}
	var out []string
	var add func(b *BoolNode)
	add = func(b *BoolNode) {
		if b == nil {
			return
		}
		if b.Op == BVar && !seen[b.Name] {
			seen[b.Name] = true
			out = append(out, b.Name)
		}
		add(b.Left)
		add(b.Right)
	}
	add(p.Disable)
	var walkSeq func(s *SeqNode)
	walkSeq = func(s *SeqNode) {
		if s == nil {
			return
		}
		add(s.Bool)
		walkSeq(s.Left)
		walkSeq(s.Right)
	}
	if p.Bare {
		walkSeq(p.BareSeq)
	} else {
		walkSeq(p.Ante)
		walkSeq(p.Cons)
	}
	return out
}
