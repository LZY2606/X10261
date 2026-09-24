// Package model defines the stable, stored identities shared across import,
// replay, adjudication and the review UI. Every value displayed on a page
// carries an ID from this package so export/delete/re-import cannot reorder or
// re-parent it.
package model

import "time"

// SourceKind discriminates imported materials.
type SourceKind string

const (
	SrcVCD     SourceKind = "vcd"
	SrcDomains SourceKind = "domains"
	SrcProps   SourceKind = "props"
)

// Source is one imported material blob with its byte content addressable hash
// and origin provenance. Bytes are never rewritten by later interpretation.
type Source struct {
	ID        string     `json:"id"`         // "src_"+sha256(content)[:16]
	Kind      SourceKind `json:"kind"`
	Name      string     `json:"name"`
	SHA256    string     `json:"sha256"`
	Bytes     []byte     `json:"-"`
	ByteLen   int        `json:"byte_len"`
	ImportedAt time.Time `json:"imported_at"`
	ImportSeq int64      `json:"import_seq"`
}

// Signal is a declared VCD variable.
type Signal struct {
	ID       string `json:"id"` // "sig_"+hash(sourceID|idCode|name)
	SourceID string `json:"source_id"`
	IDCode   string `json:"id_code"` // VCD identifier code (raw)
	Name     string `json:"name"`
	Width    int    `json:"width"`
	VType    string `json:"vtype"` // raw var type (wire/reg/...)
}

// Change is one raw value-change record exactly as present in the VCD.
type Change struct {
	ID       string `json:"id"` // "chg_"+hash(sourceID|idCode|rawMark|index)
	SourceID string `json:"source_id"`
	SignalID string `json:"signal_id"`
	IDCode   string `json:"id_code"`
	RawMark  int64  `json:"raw_mark"` // integer from #mark, units of source timescale
	ValueTok string `json:"value_tok"` // exact token, e.g. "x", "z", "1", "b10x"
	Order    int    `json:"order"`     // stable input order within the mark
	ByteOff  int    `json:"byte_off"`  // offset into raw bytes
}

// Diagnostic keeps an unconvertible value at its original position. Nothing is
// dropped to make the picture drawable.
type Diagnostic struct {
	ID       string `json:"id"`
	SourceID string `json:"source_id"`
	ByteOff  int    `json:"byte_off"`
	Raw      string `json:"raw"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ClockDomain fixes the sampling coordinate base for a replay.
type ClockDomain struct {
	Name       string `json:"name"`
	ClockSig   string `json:"clock_sig"`
	Edge       string `json:"edge"`        // posedge / negedge
	ResetSig   string `json:"reset_sig"`
	ResetPolar string `json:"reset_polar"` // high / low
}

// DomainsDoc is the parsed clock/domain declaration file (raw text retained).
type DomainsDoc struct {
	SourceID string        `json:"source_id"`
	Domains  []ClockDomain `json:"domains"`
}

// Semver identifies the rule version used for an interpretation. Changing it
// never replaces old conclusions; both are readable and diffable.
type Semver struct {
	Name    string `json:"name"`    // "ssp"
	Major   int    `json:"major"`
	Minor   int    `json:"minor"`
	Patch   int    `json:"patch"`
}

func (s Semver) String() string {
	return s.Name + "-" + itoa(s.Major) + "." + itoa(s.Minor) + "." + itoa(s.Patch)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ParseSemver accepts names like "ssp-1.0.0".
func ParseSemver(v string) (Semver, bool) {
	var s Semver
	i := 0
	for i < len(v) && v[i] != '-' {
		i++
	}
	if i == 0 || i >= len(v)-1 {
		return s, false
	}
	s.Name = v[:i]
	n, ok := parseVersions(v[i+1:])
	if !ok {
		return s, false
	}
	s.Major, s.Minor, s.Patch = n[0], n[1], n[2]
	return s, true
}

func parseVersions(rest string) ([3]int, bool) {
	var out [3]int
	idx := 0
	cur := -1
	for i := 0; i <= len(rest); i++ {
		if i == len(rest) || rest[i] == '.' {
			if cur < 0 || idx > 2 {
				return out, false
			}
			out[idx] = cur
			idx++
			cur = -1
			continue
		}
		if rest[i] < '0' || rest[i] > '9' {
			return out, false
		}
		if cur < 0 {
			cur = 0
		}
		cur = cur*10 + int(rest[i]-'0')
	}
	return out, idx == 3
}
