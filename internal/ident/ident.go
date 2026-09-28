// Package ident builds stable, content-derived identifiers. Identifiers never
// depend on wall-clock time or database row ids, so an export/delete/import
// round trip preserves every identity, ordering and provenance relation.
package ident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash returns the first 16 bytes (32 hex chars) of SHA-256 over the UTF-8
// bytes of data. Callers feed canonical concatenations (parts separated by 0x1f)
// or CanonicalJSON so that equal logical content always hashes equally.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// Parts hashes a deterministic unit-separator join of parts.
func Parts(parts ...string) string {
	buf := make([]byte, 0, 256)
	for i, p := range parts {
		if i > 0 {
			buf = append(buf, 0x1f)
		}
		buf = append(buf, p...)
	}
	return Hash(buf)
}

// CanonicalJSON marshals v without HTML escaping and with sorted map keys.
// Only map[string]any needs sorting; encoding/json already sorts such maps.
func CanonicalJSON(v any) []byte {
	buf := make([]byte, 0, 256)
	enc := json.NewEncoder(canonicalWriter{&buf})
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return buf
}

// Canonical hashes the canonical JSON encoding of v.
func Canonical(v any) string {
	return Hash(CanonicalJSON(v))
}

type canonicalWriter struct{ b *[]byte }

func (w canonicalWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}
