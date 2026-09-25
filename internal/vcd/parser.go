// Package vcd parses Value Change Dump files while preserving the raw
// representation. Every value change keeps its original token; tokens that
// cannot be mapped into four-state logic stay in place with a diagnostic
// instead of being dropped to make a prettier picture.
package vcd

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/verify/waveassert/internal/fourstate"
	"github.com/verify/waveassert/internal/model"
)

// ParseResult is the immutable explanation of one VCD material.
type ParseResult struct {
	Material    model.Material
	Signals     []model.Signal
	Changes     []model.Change
	Diagnostics []string
}

var timescaleRe = regexp.MustCompile(`^(\d+)\s*(fs|ps|ns|us|µs|ms|s)$`)

// scaleToPicoseconds returns the picosecond span of one native tick for a
// timescale of (number, unit). fs resolution cannot be losslessly expressed
// in the integer ps coordinate base, so it reports ok=false and the caller
// keeps raw ticks with a diagnostic.
func scaleToPicoseconds(number int64, unit string) (int64, bool) {
	switch unit {
	case "fs":
		return 0, false
	case "ps":
		return number, true
	case "ns":
		return number * 1_000, true
	case "us", "µs":
		return number * 1_000_000, true
	case "ms":
		return number * 1_000_000_000, true
	case "s":
		return number * 1_000_000_000_000, true
	default:
		return 0, false
	}
}

// TimescaleToPs returns the picosecond span of one native tick.
func TimescaleToPs(ts string) (int64, bool) {
	m := timescaleRe.FindStringSubmatch(strings.TrimSpace(ts))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return scaleToPicoseconds(n, m[2])
}

// Parse reads VCD text. Fatal structural errors are returned so the caller
// can roll the whole import back; per-token problems are collected and the
// raw tokens retained.
func Parse(projectID model.ID, name string, raw []byte, origin string) (*ParseResult, error) {
	if len(raw) == 0 {
		return nil, errors.New("vcd: empty input")
	}
	sum := sha256.Sum256(raw)
	mat := model.Material{
		ID:        model.ID("id_mat_" + uuid.NewString()),
		ProjectID: projectID,
		Kind:      model.KindVCD,
		Name:      name,
		RawBytes:  append([]byte(nil), raw...),
		RawSHA256: hex.EncodeToString(sum[:]),
		MediaType: "application/x-vcd",
		Origin:    origin,
	}
	res := &ParseResult{Material: mat}

	// Normalize directives: join "$timescale 1ns $end" or block form into a
	// single logical line while preserving value-change lines untouched.
	logical := normalizeDirectives(string(raw))

	codeToSignal := map[string]*model.Signal{}
	order := 0
	var tick, seq int64
	currentTime := false
	tsPs, tsOK := int64(0), false

	emit := func(tok, code string) error {
		sig, known := codeToSignal[code]
		if !known {
			return fmt.Errorf("vcd: change for undeclared id %q at #%d", code, tick)
		}
		seq++
		ch := model.Change{
			ID:          model.ID("id_chg_" + uuid.NewString()),
			MaterialID:  mat.ID,
			SignalID:    sig.ID,
			Seq:         seq,
			Tick:        tick,
			Timescale:   mat.Timescale,
			Picoseconds: -1,
			RawValue:    tok,
			Kind:        model.ChangeValue,
		}
		if currentTime {
			ch.Region = model.RegionObserved
		} else {
			ch.Region = model.RegionUntimed
		}
		val, diag := classifyToken(tok, sig.Width)
		if diag != "" {
			ch.Kind = model.ChangeUnconvert
			ch.Diagnostics = diag
			res.Diagnostics = append(res.Diagnostics, diag)
		} else {
			ch.Value = val
		}
		if tsOK && ch.Kind == model.ChangeValue {
			ch.Picoseconds = tick * tsPs
		}
		res.Changes = append(res.Changes, ch)
		return nil
	}

	for _, line := range logical {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "$timescale"):
			body := directiveBody(t)
			fields := strings.Fields(body)
			if len(fields) == 0 {
				return nil, errors.New("vcd: empty $timescale")
			}
			mat.Timescale = fields[0] + fields[1]
			if p, ok := TimescaleToPs(mat.Timescale); ok {
				tsPs, tsOK = p, true
			} else {
				res.Diagnostics = append(res.Diagnostics,
					fmt.Sprintf("vcd: timescale %q cannot convert to ps; raw retained", mat.Timescale))
			}
		case strings.HasPrefix(t, "$var"):
			f := strings.Fields(t)
			// $var wire 1 ! sig $end
			if len(f) < 5 {
				return nil, fmt.Errorf("vcd: malformed $var: %q", t)
			}
			width := 1
			if w, err := strconv.Atoi(f[2]); err == nil {
				width = w
			}
			sig := &model.Signal{
				ID:         model.ID("id_sig_" + uuid.NewString()),
				MaterialID: mat.ID,
				VCDCode:    f[3],
				RawName:    f[4],
				Width:      width,
				Index:      order,
			}
			order++
			res.Signals = append(res.Signals, *sig)
			codeToSignal[sig.VCDCode] = sig
		case strings.HasPrefix(t, "#"):
			n, err := strconv.ParseInt(strings.TrimPrefix(t, "#"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("vcd: malformed time marker %q", t)
			}
			tick = n
			currentTime = true
		case strings.HasPrefix(t, "b") || strings.HasPrefix(t, "B") ||
			strings.HasPrefix(t, "r") || strings.HasPrefix(t, "R"):
			f := strings.Fields(t)
			if len(f) != 2 {
				return nil, fmt.Errorf("vcd: malformed vector change %q", t)
			}
			if err := emit(f[0], f[1]); err != nil {
				return nil, err
			}
		case isScalarStart(t[0]):
			if err := emit(t[:1], t[1:]); err != nil {
				return nil, err
			}
		case strings.HasPrefix(t, "$"):
			// Other directives/comments: ignore, never reorder.
		default:
			// A bare "<token><id>" line that is not a recognized directive,
			// time marker or four-state scalar is still a value change whose
			// token we cannot convert. Retain it in place with a diagnostic.
			i := 0
			for i < len(t) && t[i] != ' ' && t[i] != '\t' {
				i++
			}
			tok, code := t, ""
			if i < len(t) {
				tok, code = t[:i], strings.TrimSpace(t[i:])
			}
			if sp := strings.IndexAny(t, " \t"); sp >= 0 {
				tok, code = t[:sp], strings.TrimSpace(t[sp:])
			}
			if code != "" {
				if err := emit(tok, code); err != nil {
					return nil, err
				}
			}
		}
	}

	if len(res.Signals) == 0 {
		return nil, errors.New("vcd: no signals declared")
	}
	res.Material = mat
	return res, nil
}

// classifyToken maps a raw change token to a canonical four-state scalar.
// Multi-bit buses with mixed known bits are not silently collapsed: the raw
// representation is retained with a diagnostic.
func classifyToken(tok string, width int) (string, string) {
	switch {
	case strings.HasPrefix(tok, "b") || strings.HasPrefix(tok, "B"):
		bits := tok[1:]
		if strings.ContainsAny(bits, "xX") {
			return "x", ""
		}
		if strings.ContainsAny(bits, "zZ") {
			return "z", ""
		}
		for _, c := range bits {
			if c != '0' && c != '1' {
				v, ok := fourstate.Parse(string(c))
				if !ok {
					return "", fmt.Sprintf("token %q is not four-state; raw value retained", tok)
				}
				return v.String(), ""
			}
		}
		if width == 1 || len(bits) == 1 {
			return string(bits[len(bits)-1]), ""
		}
		allSame := true
		for i := 1; i < len(bits); i++ {
			if bits[i] != bits[0] {
				allSame = false
			}
		}
		if allSame {
			return string(bits[0]), ""
		}
		return "", fmt.Sprintf("mixed multi-bit vector %q needs explicit bus decode; raw retained", tok)
	case len(tok) == 1:
		v, ok := fourstate.Parse(tok)
		if !ok {
			return "", fmt.Sprintf("token %q is not four-state; raw value retained", tok)
		}
		return v.String(), ""
	default:
		return "", fmt.Sprintf("token %q is not four-state; raw value retained", tok)
	}
}

func isScalarStart(b byte) bool {
	return b == '0' || b == '1' || b == 'x' || b == 'X' || b == 'z' || b == 'Z'
}

// directiveBody strips a leading "$name" and trailing "$end".
func directiveBody(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.Index(t, " "); i >= 0 {
		t = t[i+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(t, "$end"))
}

// normalizeDirectives joins multi-line directive blocks onto one logical
// line and leaves all other lines intact.
func normalizeDirectives(s string) []string {
	var out []string
	var block strings.Builder
	inBlock := false
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if !inBlock && strings.HasPrefix(trim, "$") &&
			!strings.Contains(trim, "$end") {
			inBlock = true
			block.Reset()
			block.WriteString(line)
			block.WriteString(" ")
			continue
		}
		if inBlock {
			block.WriteString(line)
			block.WriteString(" ")
			if strings.Contains(trim, "$end") {
				out = append(out, block.String())
				inBlock = false
			}
			continue
		}
		out = append(out, line)
	}
	if inBlock {
		out = append(out, block.String())
	}
	return out
}
