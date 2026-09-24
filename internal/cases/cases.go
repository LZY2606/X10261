// Package cases embeds the offline domain case pack ("信号言词庭") so the
// repository ships real HDL/assertion material rather than generic tables.
package cases

import (
	_ "embed"
	"encoding/json"
)

//go:embed pack/wave.vcd
var WaveVCD []byte

//go:embed pack/wave2.vcd
var Wave2VCD []byte

//go:embed pack/clocks.json
var ClocksJSON []byte

//go:embed pack/props.sva
var PropsSVA []byte

//go:embed pack/narrative.json
var NarrativeJSON []byte

type Step struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

type Narrative struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Steps   []Step `json:"steps"`
}

func LoadNarrative() (*Narrative, error) {
	var n Narrative
	if err := json.Unmarshal(NarrativeJSON, &n); err != nil {
		return nil, err
	}
	return &n, nil
}
