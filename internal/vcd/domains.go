package vcd

import (
	"bufio"
	"strings"

	"signalcourt/internal/model"
)

// ParseDomains parses the small clock/reset declaration language, one domain
// per line:
//
//	domain tb (clock=clk, edge=posedge, reset=rst, reset_polar=high)
//
// Raw lines are retained by the caller. Unknown fields are an error so the
// import can roll back rather than silently guessing a sampling base.
func ParseDomains(src model.Source) (model.DomainsDoc, error) {
	doc := model.DomainsDoc{SourceID: src.ID}
	sc := bufio.NewScanner(strings.NewReader(string(src.Bytes)))
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d, err := parseDomainLine(line)
		if err != nil {
			return model.DomainsDoc{}, errLine(lineNo, err)
		}
		doc.Domains = append(doc.Domains, d)
	}
	if err := sc.Err(); err != nil {
		return model.DomainsDoc{}, err
	}
	if len(doc.Domains) == 0 {
		return model.DomainsDoc{}, errLine(0, errEmpty("no clock domains declared"))
	}
	return doc, nil
}

type parseErr struct{ line int; msg string }

func (e parseErr) Error() string { return "domains line " + itoa(e.line) + ": " + e.msg }

func errLine(n int, err error) error {
	if e, ok := err.(parseErr); ok {
		return e
	}
	return parseErr{line: n, msg: err.Error()}
}

type emptyErr string

func (e emptyErr) Error() string { return string(e) }
func errEmpty(s string) error    { return emptyErr(s) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func parseDomainLine(line string) (model.ClockDomain, error) {
	if !strings.HasPrefix(line, "domain ") {
		return model.ClockDomain{}, parseErr{msg: "expected 'domain NAME (clock=..., edge=..., reset=..., reset_polar=...)'"}
	}
	open := strings.IndexByte(line, '(')
	closeP := strings.LastIndexByte(line, ')')
	if open < 0 || closeP < 0 || closeP < open {
		return model.ClockDomain{}, parseErr{msg: "domain body must be parenthesized"}
	}
	name := strings.TrimSpace(line[len("domain "):open])
	if name == "" {
		return model.ClockDomain{}, parseErr{msg: "missing domain name"}
	}
	d := model.ClockDomain{Name: name}
	fields := strings.Split(line[open+1:closeP], ",")
	seen := map[string]bool{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			return model.ClockDomain{}, parseErr{msg: "bad field " + f}
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if seen[key] {
			return model.ClockDomain{}, parseErr{msg: "duplicate field " + key}
		}
		seen[key] = true
		switch key {
		case "clock":
			d.ClockSig = val
		case "edge":
			if val != "posedge" && val != "negedge" {
				return model.ClockDomain{}, parseErr{msg: "edge must be posedge or negedge, got " + val}
			}
			d.Edge = val
		case "reset":
			d.ResetSig = val
		case "reset_polar":
			if val != "high" && val != "low" {
				return model.ClockDomain{}, parseErr{msg: "reset_polar must be high or low, got " + val}
			}
			d.ResetPolar = val
		default:
			return model.ClockDomain{}, parseErr{msg: "unknown field " + key}
		}
	}
	if d.ClockSig == "" || d.Edge == "" {
		return model.ClockDomain{}, parseErr{msg: "clock and edge are required"}
	}
	if d.ResetSig == "" {
		d.ResetSig = "" // reset is optional; empty means no disable intervals
	}
	return d, nil
}
