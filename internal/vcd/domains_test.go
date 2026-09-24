package vcd

import (
	"testing"

	"signalcourt/internal/model"
)

func TestParseDomains(t *testing.T) {
	src := model.Source{ID: "src_d", Bytes: []byte(`# clock base
domain tb (clock=clk, edge=posedge, reset=rst, reset_polar=high)
domain slow (clock=slowclk, edge=negedge, reset=none, reset_polar=low)
`)}
	doc, err := ParseDomains(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Domains) != 2 {
		t.Fatalf("domains = %d", len(doc.Domains))
	}
	if doc.Domains[1].ClockSig != "slowclk" || doc.Domains[1].Edge != "negedge" {
		t.Fatalf("second domain = %+v", doc.Domains[1])
	}
}

func TestParseDomainsRejectsBad(t *testing.T) {
	for _, bad := range []string{
		"domain tb (clock=clk, edge=both)",
		"tb clock=clk",
		"domain (clock=clk, edge=posedge)",
	} {
		if _, err := ParseDomains(model.Source{ID: "x", Bytes: []byte(bad)}); err == nil {
			t.Fatalf("accepted bad domain: %q", bad)
		}
	}
}
