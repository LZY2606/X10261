package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// StableID builds a deterministic, content-addressed identifier. The same
// inputs always produce the same id, so export/delete/re-import and
// re-computation never change identity or provenance relations.
func StableID(prefix string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}
