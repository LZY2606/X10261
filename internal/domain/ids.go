package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// StableID derives a deterministic, content-addressed identity of the form
// "<prefix>_<12 hex chars>". The same material always yields the same id,
// which is what keeps identity, order and provenance stable across export,
// database deletion and re-import.
func StableID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:12]
}
