package fourstate

// Tri is the explicit three-state verdict domain used by every derived
// construct in the assertion semantics:
//
//	Denies (-1): the construct is deterministically false,
//	Inconclusive (0): it cannot be decided (X/Z propagation, open match,
//	                  disable-iff active, vacuity under a semantics version),
//	Holds (1): it is deterministically true.
//
// The four listed families of constructs (X/Z propagation, overlapped and
// non-overlapped implication, disable iff, and empty/vacuous matches) must
// each yield one of these three explicit results - never a missing answer.
type Tri int8

const (
	Denies       Tri = -1
	Inconclusive Tri = 0
	Holds        Tri = 1
)

func (t Tri) String() string {
	switch t {
	case Holds:
		return "holds"
	case Denies:
		return "denies"
	default:
		return "inconclusive"
	}
}

// TriFromValue maps a four-state value onto the three-state verdict domain.
// X and Z both become Inconclusive with the originating four-state value
// preserved separately by the caller (cause metadata), so propagation is
// explicit rather than folded into true or false.
func TriFromValue(v Value) Tri {
	switch v {
	case One:
		return Holds
	case Zero:
		return Denies
	default:
		return Inconclusive
	}
}

// TriAnd is three-state conjunction on the verdict domain.
func TriAnd(a, b Tri) Tri {
	if a == Denies || b == Denies {
		return Denies
	}
	if a == Holds && b == Holds {
		return Holds
	}
	return Inconclusive
}

// TriOr is three-state disjunction on the verdict domain.
func TriOr(a, b Tri) Tri {
	if a == Holds || b == Holds {
		return Holds
	}
	if a == Denies && b == Denies {
		return Denies
	}
	return Inconclusive
}

// TriNot negates a three-state verdict; inconclusive stays inconclusive.
func TriNot(a Tri) Tri {
	switch a {
	case Holds:
		return Denies
	case Denies:
		return Holds
	default:
		return Inconclusive
	}
}
