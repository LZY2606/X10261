// Package logic4 implements Verilog four-state (0/1/x/z) scalar operations and
// the explicit three-state reduction used by sequence/property evaluation.
package logic4

import "signaltribunal/internal/model"

// BoolContext maps a quad into a boolean context. Per Verilog, high impedance
// is coerced to unknown there: 0->false, 1->true, x/z->unknown.
func BoolContext(q model.Quad) (model.Tri, bool) {
	switch q {
	case model.Q0:
		return model.TriFALSE, false
	case model.Q1:
		return model.TriTRUE, false
	case model.QX:
		return model.TriUNKNOWN, true
	case model.QZ:
		return model.TriUNKNOWN, true
	}
	return model.TriUNKNOWN, true
}

// Not is four-state logical negation (x/z stay unknown).
func Not(q model.Quad) model.Quad {
	switch q {
	case model.Q0:
		return model.Q1
	case model.Q1:
		return model.Q0
	default:
		return model.QX
	}
}

// And is the Verilog four-state AND truth table.
func And(a, b model.Quad) model.Quad {
	if a == model.Q0 || b == model.Q0 {
		return model.Q0
	}
	if a == model.Q1 && b == model.Q1 {
		return model.Q1
	}
	return model.QX
}

// Or is the Verilog four-state OR truth table.
func Or(a, b model.Quad) model.Quad {
	if a == model.Q1 || b == model.Q1 {
		return model.Q1
	}
	if a == model.Q0 && b == model.Q0 {
		return model.Q0
	}
	return model.QX
}

// TriAnd / TriOr operate on the explicit tri results of child nodes, applying
// Kleene strong three-valued logic so that X/Z propagation is observable.
func TriAnd(a, b model.Tri) model.Tri {
	if a == model.TriFALSE || b == model.TriFALSE {
		return model.TriFALSE
	}
	if a == model.TriTRUE && b == model.TriTRUE {
		return model.TriTRUE
	}
	return model.TriUNKNOWN
}

func TriOr(a, b model.Tri) model.Tri {
	if a == model.TriTRUE || b == model.TriTRUE {
		return model.TriTRUE
	}
	if a == model.TriFALSE && b == model.TriFALSE {
		return model.TriFALSE
	}
	return model.TriUNKNOWN
}

func TriNot(a model.Tri) model.Tri {
	switch a {
	case model.TriTRUE:
		return model.TriFALSE
	case model.TriFALSE:
		return model.TriTRUE
	}
	return model.TriUNKNOWN
}

// HasUnknown reports whether any quads are x or z.
func HasUnknown(qs ...model.Quad) bool {
	for _, q := range qs {
		if q == model.QX || q == model.QZ {
			return true
		}
	}
	return false
}
