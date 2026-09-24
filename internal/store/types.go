package store

import (
	"encoding/json"

	"gsb/internal/domain"
)

// CaseBundle is the unit of import/export. Sources carry raw bytes; the
// derived tables are rebuilt on import so provenance survives export/delete.
type CaseBundle struct {
	Schema   int                  `json:"schema"`
	CaseID   string               `json:"case_id"`
	Name     string               `json:"name"`
	Order    []string             `json:"source_order"` // import order stability
	Sources  map[string]RawSource `json:"sources"`
	Timeline []domain.Event       `json:"timeline"` // human decisions + parameter history
}

type RawSource struct {
	Kind string `json:"kind"`
	Raw  []byte `json:"raw"`
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func parseJSON[T any](s string) T {
	var v T
	if s == "" {
		return v
	}
	_ = json.Unmarshal([]byte(s), &v)
	return v
}
