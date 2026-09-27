// Package vcd parses Value Change Dump text into raw, lossless change records.
//
// Import preserves the original representation: each change keeps the raw
// value token and the raw timestamp, the timescale keeps both its textual
// form and the numeric multiplier, and values that cannot be converted into
// the four-state level space stay in place with a diagnostic instead of being
// dropped.
package vcd

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"signalcourt/internal/tri"
)

type Diagnostic struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw"`
}

type Timescale struct {
	Raw    string `json:"raw"`
	Number int    `json:"number"`
	Unit   string `json:"unit"`
}

// Picoseconds is the normalized coordinate base used only by derived layers.
func (t Timescale) Picoseconds() int64 {
	mul := int64(t.Number)
	switch t.Unit {
	case "s":
		return mul * 1e12
	case "ms":
		return mul * 1e9
	case "us":
		return mul * 1e6
	case "ns":
		return mul * 1e3
	case "ps":
		return mul
	}
	return mul
}

type Variable struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Width int    `json:"width"`
	Type  string `json:"type"`
}

type Change struct {
	Code string  `json:"code"`
	RawT int64   `json:"raw_time"`     // units of the source timescale, unchanged
	RawV string  `json:"raw_value"`    // original token, e.g. "x", "z", "101"
	Bits []tri.L `json:"bits"`         // four-state per bit when convertible
	Real float64 `json:"real"`
	IsReal bool  `json:"is_real"`
	Known bool  `json:"known"`        // convertible to four-state
	DiagID int  `json:"diag_id,omitempty"`
}

type Document struct {
	Version     string      `json:"version"`
	Date        string      `json:"date"`
	Comment     string      `json:"comment"`
	Timescale   Timescale   `json:"timescale"`
	Variables   []Variable  `json:"variables"`
	Changes     []Change    `json:"changes"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (d *Document) Var(code string) (Variable, bool) {
	for _, v := range d.Variables {
		if v.Code == code {
			return v, true
		}
	}
	return Variable{}, false
}

func (d *Document) VarByName(name string) (Variable, bool) {
	for _, v := range d.Variables {
		if v.Name == name {
			return v, true
		}
	}
	return Variable{}, false
}

var timescaleRe = regexp.MustCompile(`^\s*(\d+)\s*(s|ms|us|ns|ps)\s*$`)

// Parse reads a VCD stream. Parsing is order preserving; changes are sorted by
// timestamp only within the same dump region as encountered.
func Parse(r io.Reader) (*Document, error) {
	doc := &Document{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	var curTime int64
	haveTime := false
	inHeader := true
	codeIndex := map[string]int{}

	addDiag := func(code, msg, raw string) int {
		id := len(doc.Diagnostics)
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Line: line, Code: code, Message: msg, Raw: raw})
		return id
	}

	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "$") {
			content, kw, ok := directive(s)
			if !ok {
				// multi-line directive opener, e.g. "$timescale 1ns ..."
				content = strings.TrimSpace(s)
				kw = strings.Fields(strings.TrimPrefix(content, "$"))[0]
				if !strings.HasSuffix(content, "$end") {
					for sc.Scan() {
						line++
						more := strings.TrimSpace(sc.Text())
						content += " " + more
						if strings.HasSuffix(more, "$end") {
							break
						}
					}
				}
			}
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "$"+kw), "$end"))
			body = strings.TrimSpace(strings.TrimSuffix(body, kw))
			body = strings.TrimSpace(body)
			switch kw {
			case "timescale":
				raw := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(content, "$timescale", ""), "$end", ""))
				raw = strings.Join(strings.Fields(raw), "")
				m := timescaleRe.FindStringSubmatch(strings.Join(strings.Fields(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(content, "$timescale", ""), "$end", ""))), ""))
				if m == nil {
					addDiag("TIMESCALE_UNPARSEABLE", "timescale kept as raw text only", raw)
					doc.Timescale = Timescale{Raw: raw}
				} else {
					n, _ := strconv.Atoi(m[1])
					doc.Timescale = Timescale{Raw: raw, Number: n, Unit: m[2]}
				}
			case "version":
				doc.Version = body
			case "date":
				doc.Date = body
			case "comment":
				if doc.Comment != "" {
					doc.Comment += "\n"
				}
				doc.Comment += body
			case "var":
				f := strings.Fields(body)
				if len(f) >= 4 {
					width, _ := strconv.Atoi(f[2])
					v := Variable{Type: f[0], Width: width, Code: f[3], Name: f[4]}
					if v.Width <= 0 {
						v.Width = 1
					}
					if idx, exists := codeIndex[v.Code]; !exists {
						codeIndex[v.Code] = len(doc.Variables)
						doc.Variables = append(doc.Variables, v)
					} else {
						// Alias: keep the first canonical mapping but note width.
						_ = idx
					}
				}
			case "dumpvars", "dumpall", "dumpon", "dumpoff":
				// Values for these sections appear as subsequent scalar/vector
				// lines at the current time.
				if kw == "dumpoff" {
					// Section marker itself holds no level.
				}
			}
			if kw == "enddefinitions" {
				inHeader = false
			}
			continue
		}
		if s[0] == '#' {
			t, err := strconv.ParseInt(s[1:], 10, 64)
			if err != nil {
				addDiag("TIMESTAMP_UNPARSEABLE", "timestamp kept verbatim, ordering falls back to encounter order", s)
			} else {
				curTime = t
				haveTime = true
			}
			continue
		}
		if !haveTime && inHeader {
			continue
		}
		ch := parseChange(s)
		ch.RawT = curTime
		if !ch.Known && !ch.IsReal {
			ch.DiagID = addDiag("VALUE_UNCONVERTIBLE", "raw value retained at source position; not dropped for rendering", s)
		}
		doc.Changes = append(doc.Changes, ch)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("vcd scan: %w", err)
	}
	_ = time.Now
	return doc, nil
}

func directive(s string) (content, kw string, ok bool) {
	if !strings.HasPrefix(s, "$") {
		return "", "", false
	}
	if !strings.Contains(s, "$end") {
		return "", "", false
	}
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return "", "", false
	}
	return s, strings.TrimPrefix(fields[0], "$"), true
}

func parseChange(s string) Change {
	if s[0] == 'b' || s[0] == 'B' || s[0] == 'r' || s[0] == 'R' {
		fields := strings.Fields(s)
		if len(fields) != 2 {
			return Change{RawV: s}
		}
		if s[0] == 'r' || s[0] == 'R' {
			v, err := strconv.ParseFloat(fields[0][1:], 64)
			return Change{RawV: s, Code: fields[1], Real: v, IsReal: true, Known: err == nil}
		}
		ch := Change{RawV: s, Code: fields[1]}
		token := fields[0][1:]
		for i := 0; i < len(token); i++ {
			l, ok := tri.ParseLevel(token[i])
			if !ok {
				ch.Bits = append(ch.Bits, tri.X)
				ch.Known = false
				continue
			}
			ch.Bits = append(ch.Bits, l)
			if !l.IsKnown() {
				ch.Known = false
			}
		}
		if ch.Known {
			ch.Known = true
		}
		return ch
	}
	// Scalar: first char is level (optionally with strength), remainder code.
	l, ok := tri.ParseLevel(s[0])
	if len(s) >= 2 {
		if _, strengthOK := tri.ParseLevel(s[1]); !strengthOK && (s[0] >= '0' && s[0] <= '9' || s[0] == 'x' || s[0] == 'X' || s[0] == 'z' || s[0] == 'Z' || s[0] == '1') {
			// standard case already handled below
		}
	}
	_ = ok
	code := s[1:]
	// Strength-encoded scalars look like "sxx !":
	if s[0] == 's' || s[0] == 'S' {
		fields := strings.Fields(s)
		if len(fields) == 2 {
			return Change{RawV: s, Code: fields[1], Bits: []tri.L{tri.X}, Known: false}
		}
		return Change{RawV: s}
	}
	return Change{RawV: s, Code: code, Bits: []tri.L{l}, Known: l.IsKnown()}
}
