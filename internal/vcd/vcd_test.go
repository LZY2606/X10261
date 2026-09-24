package vcd

import (
	"testing"

	"signalcourt/internal/model"
)

const sample = `$timescale 10ps $end
$scope module tb $end
$var wire 1 ! clk $end
$var wire 1 " rst $end
$var wire 1 # data $end
$var wire 4 $ bus $end
$upscope $end
$enddefinitions $end
#0
0!
1"
x#
b10xz $
#5
1!
0"
1#
#10
0!
b0011 $
`

func mustParse(t *testing.T, src string) *Parsed {
	t.Helper()
	s := model.Source{ID: "src_test", Kind: model.SrcVCD, Name: "a.vcd", Bytes: []byte(src)}
	p, err := Parse(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func TestParseBasics(t *testing.T) {
	p := mustParse(t, sample)
	if !p.TimescaleOK || p.Timescale.Unit != "ps" || p.Timescale.Number != "10" {
		t.Fatalf("timescale = %+v", p.Timescale)
	}
	if len(p.Signals) != 4 {
		t.Fatalf("signals = %d", len(p.Signals))
	}
	bus := p.Signals[3]
	if bus.Width != 4 || bus.Name != "bus" {
		t.Fatalf("bus = %+v", bus)
	}
	if p.EndMark != 10 {
		t.Fatalf("endmark = %d", p.EndMark)
	}
	// Raw tokens preserved, including four-state digits in vector.
	var bus0 string
	var dataX string
	for _, c := range p.Changes {
		if c.SignalID == bus.ID && c.RawMark == 0 {
			bus0 = c.ValueTok
		}
		if c.RawMark == 0 && c.ValueTok == "x" {
			dataX = c.SignalID
		}
	}
	if bus0 != "b10xz" {
		t.Fatalf("bus raw value = %q", bus0)
	}
	if dataX == "" {
		t.Fatal("x scalar change not retained")
	}
}

func TestBadTimescaleIsDiagnosticNotFatal(t *testing.T) {
	src := `$timescale 3 fortnights $end
$var wire 1 ! clk $end
$enddefinitions $end
#0 1!
`
	p := mustParse(t, src)
	if p.TimescaleOK {
		t.Fatal("timescale must not normalize")
	}
	if len(p.Diagnostics) == 0 || p.Diagnostics[0].Code != "BAD_TIMESCALE" {
		t.Fatalf("want BAD_TIMESCALE diagnostic, got %+v", p.Diagnostics)
	}
}

func TestMalformedRollsBack(t *testing.T) {
	src := "$var wire 1 ! clk $end\n#0\n1!\n#oops\n"
	s := model.Source{ID: "src_test", Bytes: []byte(src)}
	if _, err := Parse(s); err == nil {
		t.Fatal("malformed time mark must return an error so the importer can roll back")
	}
}

func TestUnknownIDCodeDiagnostic(t *testing.T) {
	p := mustParse(t, "#0 1?\n#5 0!\n")
	if len(p.Changes) != 0 || len(p.Diagnostics) == 0 {
		t.Fatalf("undeclared id must stay diagnostic, got changes=%d diag=%d", len(p.Changes), len(p.Diagnostics))
	}
}
