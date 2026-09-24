package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// HashID builds a deterministic, content-derived identifier. No timestamps or
// randomness are used, so re-importing identical bytes yields identical
// identities and order.
func HashID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(h[:])[:18]
}

// SeqID builds deterministic branch/event sequence ids inside a lineage.
func SeqID(prefix, parent string, seq int) string {
	return HashID(prefix, parent, strconv.Itoa(seq))
}
