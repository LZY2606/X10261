// Package identity computes deterministic, stable identifiers for all
// persisted objects. Identities are content-derived so that exporting a
// database, deleting it and re-importing produces identical ids, ordering
// and provenance relations.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Of returns sha256(kind + 0 + canonical-json(parts)) truncated to 16 hex
// characters, prefixed with kind. Deterministic JSON keeps ids stable
// across Go versions and machines.
func Of(kind string, parts any) string {
	b, err := json.Marshal(parts)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(append([]byte(kind+"\x00"), b...))
	return kind + "_" + hex.EncodeToString(sum[:])[:16]
}

// Hash returns the full 64-hex sha256 of canonical JSON, used for raw
// byte content hashes.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SortedKeys returns map keys in stable order; helpers for callers that
// build canonical structures.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
