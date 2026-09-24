package timescale

import "testing"

func TestKnownScales(t *testing.T) {
	cases := map[string]int64{
		"100 ps": 100_000,
		"1ns":    1_000_000,
		"1 fs":   1,
		"10 us":  10_000_000_000,
	}
	for body, fs := range cases {
		s := Parse(body)
		if !s.OK || s.FSPerTick != fs {
			t.Fatalf("%s => ok=%v fs=%d want %d", body, s.OK, s.FSPerTick, fs)
		}
		q := s.At(7)
		if q.OK && q.FS != 7*fs {
			t.Fatalf("At(7)=%d", q.FS)
		}
	}
}

func TestUnknownUnitRetained(t *testing.T) {
	s := Parse("3 fortnights")
	if s.OK {
		t.Fatal("expected non-normalizable scale")
	}
	q := s.At(9)
	if q.OK || q.Unit != "fortnights" || q.Raw != "9" || q.Diagnostic == "" {
		t.Fatalf("raw value must stay in place with diagnostic: %+v", q)
	}
}

func TestAtRaw(t *testing.T) {
	s := Parse("garbage body")
	q := s.At(2)
	if q.OK || q.Number != "2" || q.Diagnostic == "" {
		t.Fatalf("expected retained raw tick: %+v", q)
	}
}
