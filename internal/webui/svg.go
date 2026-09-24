package webui

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"gsb/internal/domain"
	"gsb/internal/replay"
	"gsb/internal/store"
)

const (
	rowH   = 26
	leftW  = 90
	topPad = 30
)

func (s *Server) loadReplayData(caseID, branchID string) (*store.CaseData, *replay.Engine, error) {
	data, err := s.db.LoadCaseData(caseID, branchID)
	if err != nil {
		return nil, nil, err
	}
	sem := "1.0"
	if len(data.Properties) > 0 {
		sem = data.Properties[0].SemVer
	}
	eng := replay.NewEngine(sem, data.Signals, data.Changes, data.Timescale, data.Domains)
	return data, eng, nil
}

// waveSVG renders raw value changes per signal, the preponed sampling region
// of every sample, reset intervals and stable change identities.
func (s *Server) waveSVG(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, eng, err := s.loadReplayData(q.Get("case"), q.Get("branch"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	domains := eng.Domains()
	var smp *replay.Sampled
	for _, d := range domains {
		smp = d
		break
	}
	if smp == nil || len(smp.Samples) == 0 {
		http.Error(w, "no samples", 404)
		return
	}
	endTick := smp.Samples[len(smp.Samples)-1].AtTick + 20
	width := leftW + int(endTick)/2 + 60
	height := topPad + rowH*(len(data.Signals)+1) + 40

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" font-family="monospace" font-size="10">`, width, height)
	b.WriteString(`<rect width="100%" height="100%" fill="#0d1117"/>`)

	x := func(t int64) int { return leftW + int(t)/2 }

	// Reset intervals shading.
	for _, ri := range smp.ResetInts {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#5a1f1f" opacity="0.5"/>`,
			x(ri.StartTick), topPad, x(ri.EndTick)-x(ri.StartTick), rowH*(len(data.Signals)+1))
	}
	// Sampling regions: preponed window of each sample.
	for _, sm := range smp.Samples {
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#2f81f7" stroke-dasharray="2 2"/>`,
			x(sm.AtTick), topPad-4, x(sm.AtTick), topPad+rowH*(len(data.Signals)+1))
		if sm.Index%2 == 0 {
			fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#2f81f7" font-size="8">s%d</text>`,
				x(sm.AtTick)+1, topPad-6, sm.Index)
		}
	}
	// Signal rows with raw four-state changes.
	for i, sig := range data.Signals {
		y := topPad + rowH*(i+1)
		fmt.Fprintf(&b, `<text x="4" y="%d" fill="#c9d1d9">%s</text>`, y+12, sig.Name)
		fmt.Fprintf(&b, `<text x="4" y="%d" fill="#8b949e" font-size="7">%s</text>`, y+20, sig.ID)
		chgs := changesOf(data.Changes, sig.ID)
		prevY := y
		prevX := x(0)
		prevColor := "#8b949e"
		first := true
		for _, ch := range chgs {
			cx := x(ch.AtTick)
			v := domain.LX
			if len(ch.Bits) > 0 {
				v = ch.Bits[0]
			}
			color := logicColor(v)
			if !first {
				fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2"/>`,
					prevX, prevY, cx, prevY, prevColor)
				fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2"/>`,
					cx, prevY, cx, yOffset(y, v), color)
			}
			prevY = yOffset(y, v)
			prevX = cx
			prevColor = color
			first = false
			fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="2" fill="%s"><title>%s @%d raw=%q</title></circle>`,
				cx, prevY, color, ch.ID, ch.AtTick, ch.RawValue)
		}
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2"/>`,
			prevX, prevY, x(endTick), prevY, prevColor)
	}
	// Axis: raw ticks and normalized femtoseconds.
	for _, sm := range smp.Samples {
		if sm.Index%5 != 0 {
			continue
		}
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#8b949e" font-size="8">t=%d (%dfs)</text>`,
			x(sm.AtTick)-8, height-8, sm.AtTick, sm.AtFS)
	}
	fmt.Fprintf(&b, `<text x="4" y="14" fill="#8b949e">timescale raw: %s (base %d fs, convertible=%v) — preponed sampling regions marked</text>`,
		data.Timescale.Raw, data.Timescale.BaseFS, data.Timescale.Convertible)
	b.WriteString(`</svg>`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(b.String()))
}

func yOffset(y int, v domain.Logic) int {
	switch v {
	case domain.L1:
		return y
	case domain.L0:
		return y + 14
	case domain.LX:
		return y + 7
	default:
		return y + 10
	}
}

func logicColor(v domain.Logic) string {
	switch v {
	case domain.L1:
		return "#3fb950"
	case domain.L0:
		return "#8b949e"
	case domain.LX:
		return "#f85149"
	default:
		return "#a371f7"
	}
}

func changesOf(changes []domain.Change, sigID string) []domain.Change {
	var out []domain.Change
	for _, ch := range changes {
		if ch.SignalID == sigID {
			out = append(out, ch)
		}
	}
	return out
}

// automatonSVG renders the property AST and the active thread states per
// sample — the "property automaton + active threads" view.
func (s *Server) automatonSVG(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, eng, err := s.loadReplayData(q.Get("case"), q.Get("branch"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	propID := q.Get("property")
	var pr *domain.Property
	for _, p := range data.Properties {
		if p.ID == propID || p.Name == propID {
			pr = p
		}
	}
	if pr == nil && len(data.Properties) > 0 {
		pr = data.Properties[0]
	}
	if pr == nil {
		http.Error(w, "no property", 404)
		return
	}
	res := eng.Run(pr)
	n := len(res.Steps)
	cellW := 46
	width := 220 + n*cellW + 40
	height := 120 + 6*22 + 60
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" font-family="monospace" font-size="10">`, width, height)
	b.WriteString(`<rect width="100%" height="100%" fill="#0d1117"/>`)
	fmt.Fprintf(&b, `<text x="8" y="16" fill="#c9d1d9">%s — %s (semver %s, verdict %s)</text>`,
		pr.Name, pr.RawText, res.SemVer, res.Verdict.String())
	fmt.Fprintf(&b, `<text x="8" y="30" fill="#8b949e">property id %s — every step is a preponed sampling region</text>`, pr.ID)

	// AST boxes.
	astY := 48
	renderAST(&b, pr.AST, 8, astY)

	// Thread grid: rows = thread start, cols = sample.
	gridY := astY + 64
	fmt.Fprintf(&b, `<text x="8" y="%d" fill="#8b949e">active automaton threads per sample</text>`, gridY-6)
	for i, st := range res.Steps {
		x := 220 + i*cellW
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#2f81f7" font-size="8">s%d@%d</text>`, x, gridY+10, st.Sample.Index, st.Sample.AtTick)
		if st.InReset {
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="#5a1f1f"/>`,
				x-2, gridY+14, cellW, 5*22)
		}
		row := 0
		for _, th := range st.Active {
			color := "#8b949e"
			switch th.Status {
			case "matched":
				color = "#3fb950"
			case "failed":
				color = "#f85149"
			case "empty":
				color = "#a371f7"
			case "active":
				color = "#d29922"
			}
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="16" fill="none" stroke="%s"/>`,
				x, gridY+18+row*22, cellW-6, color)
			fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s" font-size="8"><title>%s start=s%d %s</title>%s</text>`,
				x+2, gridY+30+row*22, color, th.ID, th.StartedAt, th.Note, th.Status)
			row++
			if row >= 5 {
				break
			}
		}
	}
	b.WriteString(`</svg>`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(b.String()))
}

func renderAST(b *strings.Builder, n *domain.Node, x, y int) {
	if n == nil {
		return
	}
	label := n.Kind
	if n.Kind == domain.ASTBool {
		label = n.Name
	}
	if n.Kind == domain.ASTSeq {
		label = fmt.Sprintf("##%d..%d", n.Cycle, n.Hi)
	}
	if n.Empty {
		label += " (empty)"
	}
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="18" fill="#161b22" stroke="#30363d"/>`,
		x, y, 12*len(label)+12)
	fmt.Fprintf(b, `<text x="%d" y="%d" fill="#c9d1d9">%s</text>`, x+6, y+13, label)
	cx := x
	for _, c := range n.Children {
		renderAST(b, c, cx, y+24)
		cx += 130
	}
}

// counterexSVG renders the minimal counterexample: kept/removed sampling
// regions with the unmoved time origin.
func (s *Server) counterexSVG(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	caseID, branchID, verdictID := q.Get("case"), q.Get("branch"), q.Get("verdict")
	cx, err := s.db.LoadCounterex(caseID, branchID, verdictID)
	if err != nil || cx == nil {
		http.Error(w, "no counterexample", 404)
		return
	}
	verdicts, _ := s.db.LoadVerdicts(caseID, branchID)
	var total int
	for _, v := range verdicts {
		if v.ID == verdictID {
			for _, th := range v.Threads {
				if th.Sample > total {
					total = th.Sample
				}
			}
		}
	}
	if total == 0 {
		total = cx.OriginSample + 1
	}
	cellW := 18
	width := 160 + (total+2)*cellW
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="150" font-family="monospace" font-size="10">`, width)
	b.WriteString(`<rect width="100%" height="100%" fill="#0d1117"/>`)
	fmt.Fprintf(&b, `<text x="8" y="16" fill="#c9d1d9">minimal counterexample %s — origin fixed at s%d (tick %d)</text>`,
		cx.ID, cx.OriginSample, cx.OriginTick)
	removed := map[int]bool{}
	for _, iv := range cx.Removed {
		for i := iv[0]; i <= iv[1]; i++ {
			removed[i] = true
		}
	}
	for i := 0; i <= total; i++ {
		x := 160 + i*cellW
		fill := "#1f6feb"
		label := "kept"
		if removed[i] {
			fill = "#30363d"
			label = "removed"
		}
		if i == cx.OriginSample {
			fill = "#f85149"
			label = "origin"
		}
		fmt.Fprintf(&b, `<rect x="%d" y="40" width="%d" height="30" fill="%s"><title>region s%d: %s</title></rect>`,
			x, cellW-2, fill, i, label)
		if i%5 == 0 || i == cx.OriginSample {
			fmt.Fprintf(&b, `<text x="%d" y="84" fill="#8b949e" font-size="8">s%d</text>`, x, i)
		}
	}
	fmt.Fprintf(&b, `<text x="8" y="120" fill="#8b949e">kept %s | removed %s | minimal=%v | reduction only removed failure-unrelated regions; time origin unchanged</text>`,
		fmtIntervals(cx.Kept), fmtIntervals(cx.Removed), cx.Minimal)
	b.WriteString(`</svg>`)
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write([]byte(b.String()))
}

func fmtIntervals(ivs [][2]int) string {
	parts := []string{}
	for _, iv := range ivs {
		parts = append(parts, strconv.Itoa(iv[0])+"-"+strconv.Itoa(iv[1]))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

var _ = sort.Strings
