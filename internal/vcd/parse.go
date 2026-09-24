package vcd

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
)

var unitFS = map[string]int64{
	"s":  1_000_000_000_000_000,
	"ms": 1_000_000_000_000,
	"us": 1_000_000_000,
	"ns": 1_000_000,
	"ps": 1_000,
	"fs": 1,
}

// Parse turns raw VCD bytes into a File. It never discards a value token:
// unconvertible entries are flagged and retained.
func Parse(path string, raw []byte) *File {
	f := &File{Path: path, ByteSize: int64(len(raw)), RawSHA256: HashBytes(raw)}
	known := map[string]bool{}
	section := ""
	var curTimeRaw string
	var step int

	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	addDiag := func(msg, rawLine string) {
		f.Diagnostics = append(f.Diagnostics, Diagnostic{Line: lineNo, Section: section, Message: msg, Raw: rawLine})
	}

	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "$timescale"):
			body := strings.TrimSuffix(line, "$end")
			body = strings.TrimSpace(strings.TrimPrefix(body, "$timescale"))
			parseTimescale(body, f, addDiag, line)
		case strings.HasPrefix(line, "$var"):
			parseVar(line, f, known, addDiag)
		case strings.HasPrefix(line, "#"):
			curTimeRaw = line[1:]
			step = 0
		case strings.HasPrefix(line, "$scope"), strings.HasPrefix(line, "$upscope"),
			strings.HasPrefix(line, "$version"), strings.HasPrefix(line, "$comment"),
			strings.HasPrefix(line, "$date"), strings.HasPrefix(line, "$dumpall"),
			strings.HasPrefix(line, "$dumpoff"), strings.HasPrefix(line, "$dumpon"),
			strings.HasPrefix(line, "$dumpvars"), strings.HasPrefix(line, "$enddefinitions"),
			strings.HasPrefix(line, "$endtime"):
			switch {
			case strings.HasPrefix(line, "$dumpvars"):
				section = "dumpvars"
			case strings.HasPrefix(line, "$dumpoff"):
				section = "dumpoff"
			case strings.HasPrefix(line, "$dumpon"):
				section = "dumpon"
			}
			if strings.HasPrefix(line, "$version") && !strings.Contains(line, "$end") {
				section = "version"
			}
			if strings.HasPrefix(line, "$comment") && !strings.Contains(line, "$end") {
				section = "comment"
			}
			if strings.HasPrefix(line, "$enddefinitions") {
				section = ""
			}
		case section == "version" || section == "comment":
			if line == "$end" {
				section = ""
			}
		case strings.HasPrefix(line, "#"):
		case line == "$end":
			if section == "dumpvars" || section == "dumpoff" || section == "dumpon" {
				section = ""
			}
		default:
			if ch, ok := parseChange(line, curTimeRaw, step, f.FSPerTick, addDiag); ok {
				f.Changes = append(f.Changes, ch)
				step++
			}
		}
	}
	if err := sc.Err(); err != nil {
		addDiag("scanner error: "+err.Error(), "")
	}
	return f
}

func parseTimescale(body string, f *File, addDiag func(string, string), raw string) {
	f.TimescaleRaw = strings.TrimSpace(strings.TrimSpace(body))
	fields := strings.Fields(f.TimescaleRaw)
	if len(fields) == 0 {
		addDiag("timescale without unit", raw)
		return
	}
	numField := fields[0]
	unitField := ""
	if len(fields) > 1 {
		unitField = fields[1]
	} else {
		// compact form like "100ps"
		i := 0
		for i < len(numField) && (numField[i] == '.' || numField[i] >= '0' && numField[i] <= '9') {
			i++
		}
		unitField = numField[i:]
		numField = numField[:i]
	}
	num, err := strconv.ParseFloat(numField, 64)
	if err != nil {
		addDiag("unconvertible timescale number: "+err.Error(), raw)
		return
	}
	mult, ok := unitFS[unitField]
	if !ok {
		addDiag("unknown timescale unit: "+unitField, raw)
		return
	}
	fs := int64(num * float64(mult))
	if num != float64(int64(num)) {
		// fractional units resolve at fs granularity; non-integral residue is a diagnostic.
		if float64(fs) != num*float64(mult) {
			addDiag("timescale loses sub-femtosecond residue", raw)
		}
	}
	f.TimescaleNum, f.TimescaleUnit, f.FSPerTick = int(num), unitField, fs
}

func parseVar(line string, f *File, known map[string]bool, addDiag func(string, string)) {
	fields := strings.Fields(line)
	// $var type width id name [dims] $end
	if len(fields) < 6 {
		addDiag("malformed $var", line)
		return
	}
	width, err := strconv.Atoi(fields[2])
	if err != nil || width < 1 {
		addDiag("unconvertible $var width", line)
		width = 0
	}
	id, name := fields[3], fields[4]
	dims := ""
	tail := fields[5:]
	for i, fl := range tail {
		if fl == "$end" {
			tail = tail[:i]
			break
		}
	}
	if len(tail) > 0 {
		dims = strings.Join(tail, " ")
	}
	key := id + "\x00" + name
	if known[key] {
		return
	}
	known[key] = true
	if width == 0 {
		width = 1
	}
	f.Signals = append(f.Signals, Signal{ID: id, Name: name, Type: fields[1], Width: width, DimsRaw: dims})
}

func parseChange(line, timeRaw string, step int, fsPerTick int64, addDiag func(string, string)) (Change, bool) {
	ch := Change{StepIndex: step, TimeRaw: timeRaw, TimeFS: -1}
	t := timeRaw
	if n, err := strconv.ParseInt(t, 10, 64); err == nil {
		ch.TimeFS = n * fsPerTick
	} else if t != "" {
		addDiag("unconvertible simulation time: #"+t, line)
	}
	switch {
	case line[0] == 'b' || line[0] == 'B':
		parts := strings.Fields(line)
		if len(parts) != 2 {
			ch.Kind = KindInvalid
			ch.RawValue = line
			ch.Problem = "binary change needs token and id"
			addDiag(ch.Problem, line)
			return ch, true
		}
		ch.RawValue, ch.SignalID = parts[0], parts[1]
		ch.Kind = KindVector
		bits, prob := decodeBits(parts[0][1:])
		ch.Bits, ch.Problem = bits, prob
		if prob != "" {
			addDiag(prob, line)
		}
	case line[0] == 'r' || line[0] == 'R':
		parts := strings.Fields(line)
		if len(parts) != 2 {
			ch.Kind = KindInvalid
			ch.RawValue = line
			ch.Problem = "real change needs token and id"
			addDiag(ch.Problem, line)
			return ch, true
		}
		ch.RawValue, ch.SignalID = parts[0], parts[1]
		ch.Kind = KindReal
		if _, err := strconv.ParseFloat(strings.TrimLeft(parts[0], "rR"), 64); err != nil {
			ch.Problem = "unconvertible real value"
			addDiag(ch.Problem, line)
		}
	case strings.HasPrefix(line, "#"):
		return Change{}, false
	default:
		// scalar: first char value, rest id (allow multi-char ids)
		if len(line) < 2 {
			ch.Kind = KindInvalid
			ch.RawValue = line
			ch.Problem = "bare value token"
			addDiag(ch.Problem, line)
			return ch, true
		}
		v := line[0]
		id := strings.TrimSpace(line[1:])
		if _, ok := parseBit(v); !ok {
			ch.Kind = KindInvalid
			ch.RawValue = line
			ch.SignalID = id
			ch.Problem = "unconvertible scalar value"
			addDiag(ch.Problem, line)
			return ch, true
		}
		ch.Kind = KindScalar
		ch.RawValue = string(v)
		ch.SignalID = id
		ch.Bits = []byte{bitOf(v)}
	}
	return ch, true
}

func parseBit(c byte) (byte, bool) {
	switch c {
	case '0', '1', 'x', 'X', 'z', 'Z', '?':
		return bitOf(c), true
	default:
		return 0, false
	}
}

func bitOf(c byte) byte {
	switch c {
	case 'X':
		return 'x'
	case 'Z':
		return 'z'
	case '?':
		return '?'
	default:
		return c
	}
}

func decodeBits(s string) ([]byte, string) {
	if s == "" {
		return nil, "empty binary value"
	}
	bits := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		b, ok := parseBit(s[i])
		if !ok {
			return nil, "unconvertible bit in vector: " + s
		}
		bits[i] = b
	}
	return bits, ""
}
