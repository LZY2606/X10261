// Package identity provides content-derived stable identifiers used across
// materials, derived objects, events and exported archives. IDs are pure
// functions of their canonical preimages, so deleting the local database and
// importing an exported archive reproduces every identity, order relation and
// source link.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Derive returns prefix_<20 hex chars of sha256(canonical preimage)>.
func Derive(prefix string, parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0x1f})
		}
		h.Write([]byte(p))
	}
	sum := h.Sum(nil)
	return prefix + "_" + hex.EncodeToString(sum[:10])
}

// Hash returns a standalone content hash string.
func Hash(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0x1f})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// Join is the canonical separator for composite preimages.
func Join(parts ...string) string { return strings.Join(parts, "|") }
