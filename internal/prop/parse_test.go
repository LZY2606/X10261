package prop

import "testing"

const src1 = `
semver = "1.0.0"
property q_req_ack: (req |-> ##[1:2] gnt)
property q_rst: disable iff (rst) (sig_x |-> ##[1:1] sig_x)
property q_non: (ante |=> ##[1:1] cons)
property q_z: (za || zb |-> ##[1:1] gnt)
`

func TestParseProperties(t *testing.T) {
	f, err := Parse(src1)
	if err != nil {
		t.Fatal(err)
	}
	if f.SemVer != "1.0.0" || len(f.Properties) != 4 {
		t.Fatalf("got %+v n=%d", f.SemVer, len(f.Properties))
	}
	q := f.Properties[0]
	if q.Name != "q_req_ack" || !q.Overlap || q.Cons.Delay.Lo != 1 || q.Cons.Delay.Hi != 2 {
		t.Fatalf("q1 parse wrong: %+v", q)
	}
	rst := f.Properties[1]
	if rst.Disable == nil || rst.Disable.Kind != KIdent || rst.Disable.Name != "rst" {
		t.Fatalf("disable iff missing: %+v", rst.Disable)
	}
	non := f.Properties[2]
	if non.Overlap {
		t.Fatal("|=> must be non-overlapping")
	}
	z := f.Properties[3]
	if z.Ante.Kind != KOr {
		t.Fatalf("or expr wrong: %+v", z.Ante)
	}
	names := map[string]bool{}
	for _, n := range rst.SignalNames() {
		names[n] = true
	}
	if !names["rst"] || !names["sig_x"] {
		t.Fatalf("deps = %v", names)
	}
}

func TestParseRequiresSemverFirst(t *testing.T) {
	if _, err := Parse("property p: (a |-> b)"); err == nil {
		t.Fatal("must require a semver tag")
	}
}
