package fourval

import "testing"

func TestThreeStateAlgebra(t *testing.T) {
	if And(True, Unknown) != Unknown || And(False, Unknown) != False {
		t.Fatal("and table wrong")
	}
	if Or(False, Unknown) != Unknown || Or(True, Unknown) != True {
		t.Fatal("or table wrong")
	}
	if Not(Unknown) != Unknown {
		t.Fatal("not table wrong")
	}
	if Eq(X, X) != Unknown || Eq(One, One) != True || Eq(One, Z) != Unknown {
		t.Fatal("logical equality must propagate X/Z as unknown")
	}
	if Bit(Z) != Unknown || Bit(One) != True {
		t.Fatal("bit lift wrong")
	}
}
