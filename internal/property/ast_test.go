package property

import "testing"

func TestParsePrintRoundtrip(t *testing.T) {
	cases := []string{
		"req",
		"!(req && gnt)",
		"a ##1 b |-> c ##[1:2] d",
		"empty |=> x",
		"disable iff (rst) req |-> gnt[*2]",
		"(a ##1 b)[*2] |-> c",
	}
	for _, src := range cases {
		p, err := Parse(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		if got := Print(p); !equiv(got, src) {
			t.Errorf("roundtrip %q -> %q", src, got)
		}
		if len(Idents(p)) == 0 {
			t.Errorf("%q reports no idents", src)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{"", "a |->", "disable iff a b", "a ## b"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("expected error parsing %q", src)
		}
	}
}

func TestSemDiffLocalized(t *testing.T) {
	d := SemV1().Diff(SemV2())
	if d.All || len(d.Features) != 1 || d.Features[0] != "disable-iff-x" {
		t.Fatalf("v1->v2 must invalidate only disable-iff-x, got %+v", d)
	}
	if d.Affects(func(f string) bool { return f == "z-literal" }) {
		t.Fatal("z-literal property must not be invalidated by v1->v2")
	}
	if !d.Affects(func(f string) bool { return f == "disable-iff-x" }) {
		t.Fatal("disable-iff property must be invalidated")
	}
}

// equiv compares ignoring spaces.
func equiv(a, b string) bool {
	return squash(a) == squash(b)
}
func squash(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
