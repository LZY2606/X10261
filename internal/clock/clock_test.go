package clock

import (
	"testing"

	"signalcourt/internal/vcd"
)

const clkVCD = `$timescale 1 ns $end
$scope module d $end
$var wire 1 ! clk $end
$var wire 1 # d $end
$upscope $end
$enddefinitions $end
#0
$dumpvars
0!
0#
$end
#5
1!
1#
#10
0!
#15
1!
#15
0#
#20
0!
#25
1!
`

func buildT(t *testing.T) *Domain {
	w, err := vcd.Parse("c.vcd", []byte(clkVCD))
	if err != nil {
		t.Fatal(err)
	}
	d, err := Build("aclk", "clk", Posedge, w)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEdgeGrid(t *testing.T) {
	d := buildT(t)
	ticks := d.Ticks()
	if len(ticks) != 3 || ticks[0] != 5 || ticks[1] != 15 || ticks[2] != 25 {
		t.Fatalf("edges: %v", ticks)
	}
}

func TestSamplingVersionBoundary(t *testing.T) {
	d := buildT(t)
	// d changes 0->1 exactly on the first edge at tick 5.
	v10, _ := d.Sample("d", "v1.0.0", 0)
	v11, _ := d.Sample("d", "v1.1.0", 0)
	if v10.String() != "1" {
		t.Fatalf("v1.0.0 region (prev,edge] must read at-edge change: %s", v10)
	}
	if v11.String() != "0" {
		t.Fatalf("v1.1.0 strict prepone must defer at-edge change: %s", v11)
	}
	// at edge 15 d falls at the edge itself: v1.1 sees old 1.
	v10b, _ := d.Sample("d", "v1.0.0", 1)
	v11b, _ := d.Sample("d", "v1.1.0", 1)
	if v10b.String() != "0" || v11b.String() != "1" {
		t.Fatalf("boundary at edge 15: %s/%s", v10b, v11b)
	}
}

func TestRegionCoordinates(t *testing.T) {
	d := buildT(t)
	r := d.RegionAt(1, "v1.0.0")
	if r.StartTick != 5 || r.EndTick != 15 || r.EdgeFS != 15_000_000 || r.Unit != "ns" || r.Basis != "fs" {
		t.Fatalf("region coordinates lost: %+v", r)
	}
}
