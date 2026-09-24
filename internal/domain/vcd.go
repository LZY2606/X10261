package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func StableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(sum[:])[:16]
}

type vcdSignal struct {
	sig Signal
	width int
}

func ParseVCD(fileName string, raw []byte, materialID string) (*VCDData, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s is empty; import transaction will roll back", fileName)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	data := &VCDData{
		ID:              StableID("vcd", materialID, string(raw)),
		FileName:        fileName,
		RawBytes:        append([]byte(nil), raw...),
		RawSHA256:       sha256Hex(raw),
		TimeUnit:        "ps",
		CoordinateBasis: "picoseconds-from-vcd-time-zero",
	}
	byVCDID := map[string]*vcdSignal{}
	order := 0
	orderChanged := func(id string) { order++ }
	_ = orderChanged

	currentTime := int64(0)
	for lineNo, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "$timescale"):
			block := line
			if !strings.Contains(line, "$end") {
				for j := lineNo + 1; j < len(lines); j++ {
					block += " " + strings.TrimSpace(lines[j])
					if strings.Contains(lines[j], "$end") {
						break
					}
				}
			}
			text := cleanDirective(block)
			scale, err := parseTimescale(text)
			if err != nil {
				return nil, err
			}
			data.TimescaleText = text
			data.TimescalePS = scale
		case strings.HasPrefix(line, "$var"):
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return nil, fmt.Errorf("%s:%d malformed $var", fileName, lineNo+1)
			}
			width, err := strconv.Atoi(fields[2])
			if err != nil || width <= 0 {
				return nil, fmt.Errorf("%s:%d invalid width", fileName, lineNo+1)
			}
			vcdID := fields[3]
			name := fields[4]
			sig := Signal{
				ID:       StableID("sig", materialID, vcdID, name),
				Name:     name,
				Width:    width,
				VCDID:    vcdID,
				Material: materialID,
				Kind:     fields[1],
			}
			data.Signals = append(data.Signals, sig)
			byVCDID[vcdID] = &vcdSignal{sig: sig, width: width}
		case strings.HasPrefix(line, "$"):
			continue
		case strings.HasPrefix(line, "#"):
			rawTime, err := strconv.ParseInt(strings.TrimPrefix(line, "#"), 10, 64)
			if err != nil || rawTime < 0 {
				return nil, fmt.Errorf("%s:%d invalid time", fileName, lineNo+1)
			}
			currentTime = rawTime
		default:
			val, vcdID, ok := parseChange(line)
			if !ok {
				return nil, fmt.Errorf("%s:%d malformed value change %q", fileName, lineNo+1, line)
			}
			meta := byVCDID[vcdID]
			if meta == nil {
				return nil, fmt.Errorf("%s:%d change references undeclared identifier %s", fileName, lineNo+1, vcdID)
			}
			order++
			change := RawChange{
				ID:       StableID("change", materialID, vcdID, strconv.FormatInt(currentTime, 10), strconv.Itoa(order)),
				SignalID: meta.sig.ID,
				Order:    order,
				RawTime:  currentTime,
				RawValue: val,
			}
			ps := currentTime * data.TimescalePS
			change.TimePS = &ps
			normalized, diag := normalizeValue(val, meta.width)
			change.NormValue = normalized
			change.Diagnostic = diag
			if diag != "" {
				data.Diagnostics = append(data.Diagnostics, diag+" at raw time "+strconv.FormatInt(currentTime, 10)+" for "+meta.sig.Name)
			}
			data.Changes = append(data.Changes, change)
		}
	}
	if data.TimescalePS == 0 {
		return nil, fmt.Errorf("%s has no $timescale", fileName)
	}
	sort.Slice(data.Signals, func(i, j int) bool { return data.Signals[i].Name < data.Signals[j].Name })
	sort.SliceStable(data.Changes, func(i, j int) bool {
		if data.Changes[i].RawTime == data.Changes[j].RawTime {
			return data.Changes[i].Order < data.Changes[j].Order
		}
		return data.Changes[i].RawTime < data.Changes[j].RawTime
	})
	return data, nil
}

func cleanDirective(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "$timescale")
	line = strings.TrimSuffix(line, "$end")
	return strings.Fields(line)[0]
}

func parseTimescale(text string) (int64, error) {
	if len(text) < 2 {
		return 0, fmt.Errorf("invalid timescale %q", text)
	}
	unit := text[len(text)-2:]
	amount, err := strconv.ParseInt(text[:len(text)-2], 10, 64)
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("invalid timescale %q", text)
	}
	switch unit {
	case "fs":
		if 1_000_000_000%amount != 0 {
			return 0, fmt.Errorf("timescale %q cannot be converted exactly to ps", text)
		}
		return 1_000_000_000 / amount, nil
	case "ps":
		return amount, nil
	case "ns":
		return amount * 1000, nil
	case "us":
		return amount * 1_000_000, nil
	case "ms":
		return amount * 1_000_000_000, nil
	default:
	return 0, fmt.Errorf("unsupported timescale %q", text)
	}
}

func parseChange(line string) (string, string, bool) {
	if strings.HasPrefix(line, "b") || strings.HasPrefix(line, "B") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", "", false
		}
		return fields[0][1:], fields[1], true
	}
	if len(line) < 2 {
		return "", "", false
	}
	return string(line[0]), line[1:], true
}

func normalizeValue(raw string, width int) (string, string) {
	if width == 1 {
		value := strings.ToLower(raw)
		if _, err := ParseLogic(value); err != nil {
			return "x", err.Error()
		}
		return value, ""
	}
	value := strings.ToLower(strings.TrimLeft(raw, "0"))
	if value == "" {
		value = "0"
	}
	for _, r := range value {
		if r != '0' && r != '1' && r != 'x' && r != 'z' {
			return strings.Repeat("x", width), fmt.Sprintf("value %q contains a character outside 0/1/x/z", raw)
		}
	}
	return value, ""
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ParseClockDomains(raw []byte, materialID string, vcdByMaterial map[string]*VCDData) ([]ClockDomain, error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	var out []ClockDomain
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("clock domain line %d must be name,clock,posedge|negedge", i+1)
		}
		if fields[2] != "posedge" && fields[2] != "negedge" {
			return nil, fmt.Errorf("clock domain line %d edge must be posedge or negedge", i+1)
		}
		material := materialID
		if len(out) > 0 {
			material = fields[1]
			parts := strings.SplitN(fields[1], ".", 2)
			if len(parts) == 2 {
				material = parts[0]
				fields[1] = parts[1]
			}
		} else if idx := strings.IndexByte(fields[1], '.'); idx >= 0 {
			material = fields[1][:idx]
			fields[1] = fields[1][idx+1:]
		}
		if vcdByMaterial[material] == nil {
			return nil, fmt.Errorf("clock domain %s references unknown material %s", fields[0], material)
		}
		out = append(out, ClockDomain{
			Name: fields[0], ClockSignal: fields[1], MaterialID: material, Edge: fields[2], TimeUnit: "ps",
		})
}
	return out, nil
}
