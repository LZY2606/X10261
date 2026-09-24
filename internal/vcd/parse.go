// Package vcd imports Value Change Dump waveforms WITHOUT irreversible
// cleaning. Every change keeps its raw token, raw tick number and source
// byte range; values that cannot be converted stay in place with a
// diagnostic record.
package vcd

import (
	"fmt"
	"strconv"
	"strings"

	"signalcourt/internal/domain"
)

// Signal is one declared wire/reg with its VCD identifier and full
// hierarchical name.
type Signal struct {
	ID      string `json:"id"`
	Code    string `json:"code"` // VCD short id, e.g. "!"
	Name    string `json:"name"` // full hierarchical name, e.g. "tb.dut.req"
	Short   string `json:"short"`
	Width   int    `json:"width"`
	Kind    string `json:"kind"`
	RawDecl string `json:"rawDecl"`
}

// Change is one value-change event. Byte range references the ORIGINAL
// bytes so any later display value can always be traced back.
type Change struct {
	ID       string `json:"id"`
	SignalID string `json:"signalId"`
	Code     string `json:"code"`
	Tick     int64  `json:"tick"`      // raw VCD time number (timescale units)
	RawTime  string `json:"rawTime"`   // "#40" verbatim
	TimeFs   int64  `json:"timeFs"`    // derived normalized coordinate
	Token    string `json:"token"`     // raw value token e.g. "1", "10xz", "r3.14"
	WordRaw  string `json:"wordRaw"`   // four-state token for b[01xz]+ changes
	IsReal   bool   `json:"isReal"`
	RealVal  string `json:"realVal,omitempty"` // kept verbatim when unparseable
	Begin    int    `json:"begin"`
	End      int    `json:"end"`
	Diag     string `json:"diag,omitempty"`
}

// Diagnostic records material that could not be converted without loss.
type Diagnostic struct {
	ID     string `json:"id"`
	Where  string `json:"where"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
	Begin  int    `json:"begin"`
	End    int    `json:"end"`
	Source string `json:"sourceRef"`
}

// ParseResult is the full source-preserving import result.
type ParseResult struct {
	Timescale  domain.Quantity `json:"timescale"`
	TsRaw      string          `json:"timescaleRaw"`
	TsValid    bool            `json:"timescaleValid"`
	Signals    []Signal        `json:"signals"`
	Changes    []Change        `json:"changes"`
	Diagnostic []Diagnostic    `json:"diagnostics"`
	EndTimeFs  int64           `json:"endTimeFs"`
	EndTick    int64           `json:"endTick"`
	RawLen     int             `json:"rawLen"`
}

// Parse reads a VCD fragment. ingID labels quantities with their
// coordinate basis (the ingest identity).
func Parse(raw []byte, ingID string) (*ParseResult, error) {
	res := &ParseResult{RawLen: len(raw)}
	codeIndex := map[string]int{}
	var curTick int64
	curRaw := "#0"
	lines := splitLines(raw)
	off := 0
	inHeader := true
	for li, line := range lines {
		lb, le := off, off+len(line)
		trimmed := strings.TrimSpace(line)
		off += len(line)
		if off < len(raw) && raw[off] == '\n' {
			off++
		}
		if trimmed == "" {
			continue
		}
		// Header section declarations.
		if inHeader {
			if strings.HasPrefix(trimmed, "$timescale") {
				tsText, _ := sectionText(trimmed)
				res.TsRaw = tsText
				q := domain.ParseTime(tsText, "vcd:"+ingID)
				res.Timescale = q
				res.TsValid = q.Valid
				if !q.Valid {
					res.Diagnostic = append(res.Diagnostic, Diagnostic{Where: "timescale", Line: li + 1, Kind: "unconvertible-timescale", Text: tsText + " :: " + q.Diag, Begin: lb, End: le})
				}
				continue
			}
			if strings.HasPrefix(trimmed, "$var") {
				sig, ok := parseVar(trimmed)
				if ok {
					sig.ID = sigIdentity(ingID, sig.Code)
					codeIndex[sig.Code] = len(res.Signals)
					res.Signals = append(res.Signals, sig)
				}
				continue
			}
			if strings.HasPrefix(trimmed, "$enddefinitions") {
				inHeader = false
				continue
			}
			// Unknown header directives are harmless; keep $dumpvars transition.
			if strings.HasPrefix(trimmed, "$dumpvars") || strings.HasPrefix(trimmed, "$end") ||
				strings.HasPrefix(trimmed, "$comment") || strings.HasPrefix(trimmed, "$date") ||
				strings.HasPrefix(trimmed, "$version") || strings.HasPrefix(trimmed, "$scope") ||
				strings.HasPrefix(trimmed, "$upscope") {
				continue
			}
			// time marker can technically begin directly
		}
		if strings.HasPrefix(trimmed, "#") {
			n, err := strconv.ParseInt(strings.TrimPrefix(trimmed, "#"), 10, 64)
			if err != nil {
				res.Diagnostic = append(res.Diagnostic, Diagnostic{Where: "time", Line: li + 1, Kind: "bad-time", Text: trimmed, Begin: lb, End: le})
				continue
			}
			curTick, curRaw = n, trimmed
			if n > res.EndTick {
				res.EndTick = n
			}
			continue
		}
		if strings.HasPrefix(trimmed, "$") {
			continue // dumpvars/end markers around value rows
		}
		ch, ok := parseChange(trimmed, curTick, curRaw, lb, le, li+1, ingID)
		if !ok {
			res.Diagnostic = append(res.Diagnostic, Diagnostic{Where: "change", Line: li + 1, Kind: "unparseable-change", Text: trimmed, Begin: lb, End: le})
			continue
		}
		ch.ID = ChangeID(ingID, len(res.Changes))
		if ch.Diag != "" {
			res.Diagnostic = append(res.Diagnostic, Diagnostic{Where: "value", Line: li + 1, Kind: "unconvertible-value", Text: trimmed + " :: " + ch.Diag, Begin: lb, End: le})
		}
		if idx, known := codeIndex[ch.Code]; known {
			ch.SignalID = res.Signals[idx].ID
		} else {
			ch.Diag = "undeclared identifier"
			res.Diagnostic = append(res.Diagnostic, Diagnostic{Where: "change", Line: li + 1, Kind: "undeclared-identifier", Text: trimmed, Begin: lb, End: le})
		}
		res.Changes = append(res.Changes, ch)
	}
	if res.TsValid {
		res.EndTimeFs = res.EndTick * res.Timescale.Fs
		for i := range res.Changes {
			res.Changes[i].TimeFs = res.Changes[i].Tick * res.Timescale.Fs
		}
	}
	if len(res.Signals) == 0 {
		return nil, fmt.Errorf("vcd: no $var declarations found")
	}
	return res, nil
}

func sigIdentity(ingID, code string) string {
	return "sig_" + ingID + "_" + code
}

// ChangeID is deterministic from ingest, code, tick and raw token.
func ChangeID(ingID string, seq int) string {
	return fmt.Sprintf("ch_%s_%06d", ingID, seq)
}

func parseVar(s string) (Signal, bool) {
	f := strings.Fields(s)
	// $var wire 1 ! req [3:0] $end
	if len(f) < 5 || f[0] != "$var" {
		return Signal{}, false
	}
	width, _ := strconv.Atoi(f[2])
	if width == 0 {
		width = 1
	}
	code := f[3]
	name := f[4]
	short := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		short = name[i+1:]
	}
	return Signal{Code: code, Name: name, Short: short, Width: width, Kind: f[1], RawDecl: s}, true
}

// begin/end on Signal are unused beyond changes; zero is fine.
func parseChange(s string, tick int64, rawTime string, begin, end, line int, ingID string) (Change, bool) {
	ch := Change{Tick: tick, RawTime: rawTime, Begin: begin, End: end}
	if strings.HasPrefix(s, "b") || strings.HasPrefix(s, "B") {
		f := strings.Fields(s)
		if len(f) != 2 {
			return ch, false
		}
		token := f[1]
		ch.Token, ch.Code, ch.WordRaw = f[0]+f[1], token, f[0][1:]
		for _, c := range ch.WordRaw {
			if c != '0' && c != '1' && c != 'x' && c != 'X' && c != 'z' && c != 'Z' {
				ch.Diag = "vector token contains non four-state character"
				break
			}
		}
		return ch, true
	}
	if strings.HasPrefix(s, "r") || strings.HasPrefix(s, "R") {
		f := strings.Fields(s)
		if len(f) != 2 {
			return ch, false
		}
		ch.Token, ch.Code, ch.IsReal = s, f[1], true
		ch.RealVal = f[0][1:]
		if _, err := strconv.ParseFloat(ch.RealVal, 64); err != nil {
			ch.Diag = "real value not parseable: " + err.Error()
		}
		return ch, true
	}
	// scalar: value immediately followed by identifier ("1!", "x#3"? code is rest)
	if len(s) < 2 {
		return ch, false
	}
	if _, err := parseScalar(s[0]); err != nil {
		return ch, false
	}
	ch.Token = string(s[0])
	ch.Code = s[1:]
	ch.WordRaw = ch.Token
	return ch, true
}

func parseScalar(b byte) (byte, error) {
	switch b {
	case '0', '1', 'x', 'X', 'z', 'Z':
		return b, nil
	}
	return 0, fmt.Errorf("not scalar")
}

func sectionText(s string) (string, string) {
	s = strings.TrimPrefix(s, "$timescale")
	if i := strings.Index(s, "$end"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), ""
}

func splitLines(raw []byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			out = append(out, string(raw[start:i]))
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, string(raw[start:]))
	}
	return out
}
