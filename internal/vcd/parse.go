// Package vcd parses a disciplined subset of Value Change Dump files. It never
// discards information: original bytes, raw values, width and timescale are
// retained; only the derived layer normalizes timestamps to picoseconds.
package vcd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"signaltribunal/internal/model"
)

var unitPS = map[string]int64{
	"s": 1_000_000_000_000,
	"ms": 1_000_000_000,
	"us": 1_000_000,
	"ns": 1_000,
	"ps": 1,
}

// Parse reads a VCD from data. path is used only for provenance/display.
func Parse(path string, data []byte) (*model.WaveSource, []model.Diagnostic, error) {
	hash := sha256.Sum256(data)
	src := &model.WaveSource{
		ID:       "src_vcd_" + hex.EncodeToString(hash[:])[:16],
		Path:     path,
		ByteHash: "sha256:" + hex.EncodeToString(hash[:]),
		Bytes:    append([]byte(nil), data...),
	}
	var diags []model.Diagnostic
	byID := map[string]*model.Signal{}
	var scaleVal int64 = 1
	var scaleUnit = "ns"

	lines := strings.Split(string(data), "\n")
	var curTime int64
	changeSeq := map[string]int{}

	emit := func(d model.Diagnostic) {
		d.ID = fmt.Sprintf("diag_%x", len(diags)+1)
		diags = append(diags, d)
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "$timescale"):
			if v, u, ok := parseTimescale(line); ok {
				scaleVal, scaleUnit = v, u
			}
		case strings.HasPrefix(line, "$var"):
			if sig, ok := parseVar(line); ok {
				sig.ID = model.StableID("sig", src.ID, sig.VCDID)
				byID[sig.VCDID] = sig
				src.Signals = append(src.Signals, *sig)
			}
		case line[0] == '#':
			t, err := strconv.ParseInt(line[1:], 10, 64)
			if err == nil {
				curTime = t
			}
		case isChange(line):
			val, id := splitChange(line)
			sig, ok := byID[id]
			if !ok {
				emit(model.Diagnostic{Where: line, Severity: "warn", Code: "unknown_id",
					Message: "value change for undeclared identifier", KeptRaw: line})
				continue
			}
			ch := model.Change{
				SignalID: sig.ID,
				At: model.TimePoint{
					Raw: curTime, OriginID: src.ID,
					Unit: model.UnitSpec{Raw: fmt.Sprintf("%d%s", scaleVal, scaleUnit),
						Kind: "time", Scale: scaleVal * unitPS[scaleUnit]},
					PS: curTime * scaleVal * unitPS[scaleUnit],
				},
				RawValue: val,
				Width:    sig.Width,
			}
			q, err := reduceQuad(val, sig.Width)
			if err != nil {
				ch.Diagnostic = err.Error()
				emit(model.Diagnostic{Where: sig.ID, Severity: "warn", Code: "unconvertible_value",
					Message: err.Error() + " at raw time " + strconv.FormatInt(curTime, 10),
					KeptRaw: val})
			} else {
				ch.Quad = q
			}
			changeSeq[id]++
			ch.ID = model.StableID("chg", src.ID, id, strconv.Itoa(changeSeq[id]))
			src.Changes = append(src.Changes, ch)
		}
	}

	src.Timescale = model.UnitSpec{
		Raw:   fmt.Sprintf("%d%s", scaleVal, scaleUnit),
		Kind:  "time",
		Scale: scaleVal * unitPS[scaleUnit],
	}
	if len(src.Signals) == 0 {
		return nil, nil, fmt.Errorf("vcd %s: no signals parsed", path)
	}
	return src, diags, nil
}

var tsRe = regexp.MustCompile(`\$timescale\s+(\d+)\s*(s|ms|us|ns|ps)\s*\$end`)

func parseTimescale(line string) (int64, string, bool) {
	m := tsRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	v, _ := strconv.ParseInt(m[1], 10, 64)
	return v, m[2], true
}

var varRe = regexp.MustCompile(`\$var\s+\w+\s+(\d+)\s+(\S+)\s+(\S+)(?:\s+\[[^\]]+\])?\s+\$end`)

func parseVar(line string) (*model.Signal, bool) {
	m := varRe.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	w, _ := strconv.Atoi(m[1])
	return &model.Signal{VCDID: m[2], Name: m[3], Width: w, Original: line}, true
}

func isChange(line string) bool {
	switch line[0] {
	case '0', '1', 'x', 'X', 'z', 'Z', 'b', 'B', 'r', 'R':
		return true
	}
	return false
}

func splitChange(line string) (string, string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return "", ""
	}
	return line[:idx], strings.TrimSpace(line[idx+1:])
}

// reduceQuad derives a single quad for a scalar or vector change.
// Real-valued changes are not convertible and stay raw with a diagnostic.
func reduceQuad(val string, width int) (model.Quad, error) {
	v := strings.ToLower(val)
	switch {
	case strings.HasPrefix(v, "r"):
		return "", fmt.Errorf("real value %q has no four-state representation", val)
	case strings.HasPrefix(v, "b"):
		bits := v[1:]
		all0, all1 := true, true
		for _, c := range bits {
			if c == 'x' || c == 'z' {
				return model.QX, nil // unknown/high-impedance anywhere -> unknown read
			}
			if c != '0' {
				all0 = false
			}
			if c != '1' {
			all1 = false
			}
		}
		if bits == "" {
			return "", fmt.Errorf("empty vector %q", val)
		}
		switch {
		case all0:
			return model.Q0, nil
		case all1:
			return model.Q1, nil
		default:
			return model.QX, nil
		}
	default:
		if len(v) != 1 {
			return "", fmt.Errorf("malformed scalar %q", val)
		}
		q := model.Quad(v)
		if !q.Valid() {
			return "", fmt.Errorf("not a four-state scalar %q", val)
		}
		return q, nil
	}
}
