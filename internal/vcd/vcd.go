// Package vcd parses VCD waveforms while preserving the raw representation
// of every quantity. Normalization (to femtoseconds) happens only in the
// derived layer; values that cannot be converted stay raw and carry a
// diagnostic instead of being dropped.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"sigcourt/internal/fourstate"
)

// Quantity is a domain quantity that always carries its unit and the
// coordinate base it was expressed in, plus the original raw text.
type Quantity struct {
	Raw        string `json:"raw"`        // original representation, never rewritten
	Value      int64  `json:"value"`      // magnitude as written
	Unit       string `json:"unit"`       // "fs","ps","ns","us","ms","s" or "" for unitless
	Base       string `json:"base"`       // coordinate base, e.g. "sim-time" or "wall-clock"
	NormOK     bool   `json:"norm_ok"`    // false when normalization was impossible
	NormFS     int64  `json:"norm_fs"`    // normalized femtoseconds (derived layer only)
	Diagnostic string `json:"diagnostic"` // why normalization failed, if it did
}

var unitToFS = map[string]int64{
	"fs": 1,
	"ps": 1_000,
	"ns": 1_000_000,
	"us": 1_000_000_000,
	"ms": 1_000_000_000_000,
	"s":  1_000_000_000_000_000,
}

// Normalize converts the quantity to femtoseconds. Unconvertible values
// keep NormOK=false and stay in place with a diagnostic.
func (q *Quantity) Normalize() {
	mul, ok := unitToFS[q.Unit]
	if !ok {
		q.NormOK = false
		q.Diagnostic = fmt.Sprintf("cannot normalize unit %q to fs; raw value kept in place", q.Unit)
		return
	}
	q.NormOK = true
	q.NormFS = q.Value * mul
}

// Signal describes one VCD signal.
type Signal struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Change is one raw value change.
type Change struct {
	Time Quantity        `json:"time"`
	Sig  string          `json:"sig"`
	Val  fourstate.Value `json:"val"`
	Raw  string          `json:"raw"` // original line
}

// Waveform is the parsed, provenance-preserving waveform.
type Waveform struct {
	Raw       []byte   `json:"-"`
	RawHash   string   `json:"raw_hash"`
	Timescale Quantity `json:"timescale"`
	Signals   []Signal `json:"signals"`
	Changes   []Change `json:"changes"`
}

// Parse reads a VCD file. It never discards input: the raw bytes are kept
// on the Waveform and every change keeps its source line.
func Parse(name string, r io.Reader) (*Waveform, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	w := &Waveform{Raw: data, RawHash: hashBytes(data)}
	id2name := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	inHeader := true
	var curTime int64
	lineNo := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		lineNo++
		if line == "" {
			continue
		}
		if inHeader {
			switch {
			case strings.HasPrefix(line, "$timescale"):
				rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "$timescale"), "$end"))
				for rest == "" && sc.Scan() {
					rest = strings.TrimSpace(strings.TrimSuffix(sc.Text(), "$end"))
				}
				q, perr := parseTimescale(rest)
				if perr != nil {
					return nil, fmt.Errorf("vcd %s line %d: %w", name, lineNo, perr)
				}
				w.Timescale = q
			case strings.HasPrefix(line, "$var"):
				f := strings.Fields(line)
				if len(f) >= 5 {
					id := f[4]
					nm := f[5-1]
					// $var wire 1 <id> <name> $end -> fields: $var wire 1 id name $end
					id = f[3]
					nm = f[4]
					id2name[id] = nm
					w.Signals = append(w.Signals, Signal{ID: id, Name: nm})
				}
			case strings.HasPrefix(line, "$enddefinitions"):
				inHeader = false
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "#"):
			var t int64
			if _, err := fmt.Sscanf(line[1:], "%d", &t); err != nil {
				return nil, fmt.Errorf("vcd %s line %d: bad timestamp %q", name, lineNo, line)
			}
			curTime = t
		case len(line) >= 2 && (line[0] == '0' || line[0] == '1' || line[0] == 'x' || line[0] == 'X' || line[0] == 'z' || line[0] == 'Z'):
			v, err := fourstate.Parse(line[0])
			if err != nil {
				return nil, fmt.Errorf("vcd %s line %d: %w", name, lineNo, err)
			}
			id := strings.TrimSpace(line[1:])
			nm, ok := id2name[id]
			if !ok {
				return nil, fmt.Errorf("vcd %s line %d: unknown id %q", name, lineNo, id)
			}
			q := Quantity{Raw: fmt.Sprintf("%d", curTime), Value: curTime, Unit: w.Timescale.Unit, Base: "sim-time"}
			q.Normalize()
			w.Changes = append(w.Changes, Change{Time: q, Sig: nm, Val: v, Raw: line})
		default:
			// Unparseable lines are kept as diagnostics on a synthetic change
			// rather than dropped: nothing is silently cleaned at import.
			q := Quantity{Raw: fmt.Sprintf("%d", curTime), Value: curTime, Unit: w.Timescale.Unit, Base: "sim-time"}
			q.Normalize()
			w.Changes = append(w.Changes, Change{Time: q, Sig: "?unparsed", Val: fourstate.VX, Raw: line})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(w.Signals, func(i, j int) bool { return w.Signals[i].Name < w.Signals[j].Name })
	return w, nil
}

func parseTimescale(s string) (Quantity, error) {
	s = strings.TrimSpace(s)
	var mag int64
	var unit string
	if _, err := fmt.Sscanf(s, "%d%s", &mag, &unit); err != nil {
		// try "1 ps" style
		f := strings.Fields(s)
		if len(f) != 2 {
			return Quantity{Raw: s, Base: "sim-time", Diagnostic: "unparseable timescale, kept raw"}, nil
		}
		if _, err := fmt.Sscanf(f[0], "%d", &mag); err != nil {
			return Quantity{Raw: s, Base: "sim-time", Diagnostic: "unparseable timescale, kept raw"}, nil
		}
		unit = f[1]
	}
	q := Quantity{Raw: s, Value: mag, Unit: unit, Base: "sim-time"}
	q.Normalize()
	return q, nil
}

// ValueAt returns the value of sig at normalized time tfs (last change <= tfs),
// and whether any change exists at or before tfs.
func (w *Waveform) ValueAt(sig string, tfs int64) (fourstate.Value, bool) {
	var v fourstate.Value
	found := false
	for _, c := range w.Changes {
		if c.Sig != sig {
			continue
		}
		if c.Time.NormOK && c.Time.NormFS <= tfs {
			v = c.Val
			found = true
		}
	}
	return v, found
}

// Edges returns normalized times of rising/falling edges of a clock signal.
func (w *Waveform) Edges(sig string, posedge bool) []Quantity {
	var out []Quantity
	prev := fourstate.V0
	first := true
	for _, c := range w.Changes {
		if c.Sig != sig || !c.Time.NormOK {
			continue
		}
		if first {
			prev = c.Val
			first = false
			continue
		}
		rise := prev == fourstate.V0 && c.Val == fourstate.V1
		fall := prev == fourstate.V1 && c.Val == fourstate.V0
		if (posedge && rise) || (!posedge && fall) {
			out = append(out, c.Time)
		}
		prev = c.Val
	}
	return out
}

func hashBytes(b []byte) string {
	// FNV-1a, stable across runs, good enough for identity.
	var h uint64 = 1469598103934665603
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return fmt.Sprintf("%016x", h)
}
