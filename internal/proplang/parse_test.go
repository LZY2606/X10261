package proplang

import "testing"

const src = `// @semver 1.1
property p_handshake;
  @(posedge clk) disable iff (rst)
  req ##[2] ack |-> gnt
endproperty

property p_empty;
  @(posedge clk)
  req |-> ack
endproperty
`

func TestRepeat(t *testing.T) {
	f, err := ParseText("property pr; @(posedge clk) en[*3] |-> gnt; endproperty")
	if err != nil {
		t.Fatal(err)
	}
	r := f.Properties[0].Root.Children[0]
	if r.Kind != "repeat" || r.Repeat != 3 {
		t.Fatalf("repeat %+v", r)
	}
}

func TestNonOverlap(t *testing.T) {
	f, _ := ParseText("property pn; req |=> ack; endproperty")
	if f.Properties[0].Root.Kind != "nonimpl" {
		t.Fatal("non-overlapped implication")
	}
}

func TestError(t *testing.T) {
	if _, err := ParseText("property ; endproperty"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := ParseText(""); err == nil {
		t.Fatal("empty must error")
	}
}
