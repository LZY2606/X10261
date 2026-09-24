package domain

import "fmt"

type Logic rune

const (
	Logic0 Logic = '0'
	Logic1 Logic = '1'
	LogicX Logic = 'x'
	LogicZ Logic = 'z'
)

func ParseLogic(raw string) (Logic, error) {
	if len(raw) != 1 {
		return LogicX, fmt.Errorf("scalar value %q is not one of 0/1/x/z", raw)
	}
	switch Logic(raw[0]) {
	case Logic0, Logic1, LogicX, LogicZ:
		return Logic(raw[0]), nil
	default:
		return LogicX, fmt.Errorf("value %q cannot be normalized to four-state logic", raw)
	}
}

func Not(a Logic) Logic {
	switch a {
	case Logic0:
		return Logic1
	case Logic1:
		return Logic0
	default:
		return LogicX
	}
}

func And(a, b Logic) Logic {
	if a == Logic0 || b == Logic0 {
		return Logic0
	}
	if a == Logic1 && b == Logic1 {
		return Logic1
	}
	return LogicX
}

func Or(a, b Logic) Logic {
	if a == Logic1 || b == Logic1 {
		return Logic1
	}
	if a == Logic0 && b == Logic0 {
		return Logic0
	}
	return LogicX
}

func Equal(a, b Logic) Logic {
	if a == LogicX || a == LogicZ || b == LogicX || b == LogicZ {
		return LogicX
	}
	if a == b {
		return Logic1
	}
	return Logic0
}
