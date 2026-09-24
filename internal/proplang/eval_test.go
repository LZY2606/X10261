package proplang

import (
	"testing"

	"signalcourt/internal/domain"
	"signalcourt/internal/logic"
)

type stubEnv map[string]domain.Word

func (s stubEnv) Word(name string) (domain.Word, bool) {
	w, ok := s[name]
	return w, ok
}

func env(pairs ...string) stubEnv {
	m := stubEnv{}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i]] = domain.NewWord(pairs[i+1])
	}
	return m
}

func expr(t *testing.T, text string) *Node {
	t.Helper()
	f, err := ParseText("property px; " + text + " ; endproperty")
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return f.Properties[0].Root
}

func TestEvalXZPropagation(t *testing.T) {
	cases := []struct {
		text string
		e    stubEnv
		want logic.Tri
	}{
		{"req", env("req", "1"), logic.TTrue},
		{"req", env("req", "x"), logic.TUnknown},
		{"req", env("req", "z"), logic.TUnknown},
		{"req && ack", env("req", "1", "ack", "0"), logic.TFalse},
		{"req && ack", env("req", "1", "ack", "x"), logic.TUnknown},
		{"req && ack", env("req", "0", "ack", "z"), logic.TFalse},
		{"req || ack", env("req", "1", "ack", "z"), logic.TTrue},
		{"req || ack", env("req", "0", "ack", "z"), logic.TUnknown},
		{"!req", env("req", "z"), logic.TUnknown},
		{"data == 'b10xz", env("data", "10xz"), logic.TUnknown},
		{"data == 'b1010", env("data", "1010"), logic.TTrue},
		{"data == 'b1010", env("data", "1000"), logic.TFalse},
		{"data != 'b1010", env("data", "1010"), logic.TFalse},
		{"missing", env(), logic.TUnknown},
	}
	for _, c := range cases {
		got := Eval(expr(t, c.text), c.e)
		if got != c.want {
			t.Fatalf("%s = %s want %s", c.text, got, c.want)
		}
	}
}

func TestParseBasic(t *testing.T) {
	f, err := ParseText(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Semver != "1.1" || len(f.Properties) != 2 {
		t.Fatal("semver/properties")
	}
	p0 := f.Properties[0]
	if p0.Clock != "clk" || p0.DisableExpr != "rst" || p0.Root.Kind != "implies" {
		t.Fatalf("property %+v", p0)
	}
}
