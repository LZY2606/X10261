package ssp

import (
	"strings"
	"testing"

	"signalcourt/internal/model"
)

func src(text string) model.Source {
	return model.Source{ID: "src_props", Kind: model.SrcProps, Name: "p.ssp", Bytes: []byte(text)}
}

const good = `
property p_multithread;
  a ##[1:3] b |-> c;
endproperty
property p_nonoverlap;
  a ##1 b |=> c;
endproperty
property p_disabled;
  disable iff (rst) a |-> b;
endproperty
`

func TestParseGood(t *testing.T) {
	f, err := Parse(src(good), "ssp-1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Properties) != 3 {
		t.Fatalf("properties = %d", len(f.Properties))
	}
	mt := f.Properties[0]
	if mt.Bare || !mt.Overlap {
		t.Fatal("first property must be overlapping implication")
	}
	if mt.Ante.Lo != 1 || mt.Ante.Hi != 3 {
		t.Fatalf("range = [%d:%d]", mt.Ante.Lo, mt.Ante.Hi)
	}
	if f.Properties[1].Overlap {
		t.Fatal("second property must be non-overlapping")
	}
	if f.Properties[2].Disable == nil || f.Properties[2].Disable.Name != "rst" {
		t.Fatal("disable iff not parsed")
	}
	for _, p := range f.Properties {
		if !strings.HasPrefix(p.ID, "astp_") {
			t.Fatalf("property %s has no stable id", p.Name)
		}
	}
	sigs := mt.Signals()
	if len(sigs) != 3 {
		t.Fatalf("signals = %v", sigs)
	}
}

func TestIDsStableAcrossParses(t *testing.T) {
	a, _ := Parse(src(good), "ssp-1.0.0")
	b, _ := Parse(src(good), "ssp-1.0.0")
	for i := range a.Properties {
		if a.Properties[i].ID != b.Properties[i].ID {
			t.Fatalf("property id not stable: %s vs %s", a.Properties[i].ID, b.Properties[i].ID)
		}
	}
	c, _ := Parse(src(good), "ssp-1.1.0")
	if a.Properties[0].ID == c.Properties[0].ID {
		t.Fatal("semantic version must participate in property identity")
	}
}

func TestZeroWidthVersionDivergence(t *testing.T) {
	text := "property p_empty; a ##[0:0] b; endproperty"
	if _, err := Parse(src(text), "ssp-1.0.0"); err == nil {
		t.Fatal("ssp-1.0.0 must reject empty sequence properties explicitly")
	}
	f, err := Parse(src(text), "ssp-1.1.0")
	if err != nil {
		t.Fatalf("ssp-1.1.0 must accept zero-width sequence: %v", err)
	}
	if !f.Properties[0].BareSeq.CanMatchZero() {
		t.Fatal("zero-width sequence must be structurally detectable")
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{
		"property ; endproperty",
		"property p; a |-> ; endproperty",
		"property p; a ##[3:1] b; endproperty",
		"property p; a |-> b",
	} {
		if _, err := Parse(src(bad), "ssp-1.0.0"); err == nil {
			t.Fatalf("accepted bad property: %q", bad)
		}
	}
}
