// Package service orchestrates the archive: it replays properties, records
// verdicts/counterexamples with dependency edges, reinterprets on branches and
// computes invalidation scope without globally replacing old conclusions.
package service

import (
	"database/sql"
	"sort"
	"strconv"
	"time"

	"gsb/internal/domain"
	"gsb/internal/minimize"
	"gsb/internal/replay"
	"gsb/internal/store"
)

type Service struct{ DB *store.DB }

func New(db *store.DB) *Service { return &Service{DB: db} }

type ReplaySummary struct {
	Verdict   *domain.Verdict
	Result    *replay.Result
	Counterex *store.StoredCounterex
}

// RunReplay replays every property on a branch, persists verdicts, dependency
// edges and a counterexample for each failing property.
func (s *Service) RunReplay(caseID, branchID, actor string) ([]ReplaySummary, error) {
	data, err := s.DB.LoadCaseData(caseID, branchID)
	if err != nil {
		return nil, err
	}
	semVer := semverOf(data)
	eng := replay.NewEngine(semVer, data.Signals, data.Changes, data.Timescale, data.Domains)

	tx, err := s.DB.Raw().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	head, ver, err := store.HeadInfo(tx, branchID)
	if err != nil {
		return nil, err
	}

	// sampleOf maps a change to the preponed sample it feeds (the first edge
	// tick strictly after it).
	sampleOf := buildSampleOf(data, semVer)

	var out []ReplaySummary
	for _, pr := range data.Properties {
		res := eng.Run(pr)
		v := &domain.Verdict{
			ID: domain.StableID("v", pr.ID, branchID, res.SemVer,
				strconv.Itoa(res.FailureAt), res.Verdict.String()),
			PropertyID: pr.ID, BranchID: branchID, Result: res.Verdict,
			EmptyMatch: res.Vacuous, SemVer: res.SemVer,
		}
		for _, c := range res.Candidates {
			v.Candidates = append(v.Candidates, c)
		}
		for _, m := range res.Matches {
			v.Threads = append(v.Threads, domain.ThreadState{
				ID: m.ThreadID, Property: pr.Name, StartedAt: m.Start, Sample: m.End,
				NodePath: pathShort(m.Node), Status: statusOf(m), Result: m.Result, Note: m.Note,
			})
		}
		ver++
		evID := domain.StableID("ev", "replay", v.ID, strconv.FormatInt(ver, 10))
		deps := []string{"src:vcd", "src:properties", "src:clockdomains", "semver:" + res.SemVer, pr.ID, branchID}
		if err := s.DB.AddDeps(tx, caseID, v.ID, deps); err != nil {
			return nil, err
		}
		if err := s.DB.SaveVerdict(tx, caseID, v, evID); err != nil {
			return nil, err
		}
		if _, err := store.AppendEventRow(tx, store.EventRow{
			ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvReplay),
			Parent: head, Refs: []string{v.ID, pr.ID}, Deps: deps,
			Detail: map[string]string{
				"result": res.Verdict.String(), "vacuous": strconv.FormatBool(res.Vacuous),
				"failure_at": strconv.Itoa(res.FailureAt), "semver": res.SemVer,
				"property": pr.Name,
			}, Version: ver, Actor: actor,
		}); err != nil {
			return nil, err
		}
		head = evID
		summary := ReplaySummary{Verdict: v, Result: res}

		if res.Verdict == domain.TriFalse {
			cx, err := s.reduce(tx, data, pr, res, v, caseID, branchID, &head, &ver, actor, eng, sampleOf)
			if err != nil {
				return nil, err
			}
			summary.Counterex = cx
		}
		out = append(out, summary)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Verdict.Result == domain.TriFalse {
			cx, _ := s.DB.LoadCounterex(caseID, branchID, out[i].Verdict.ID)
			out[i].Counterex = cx
		}
	}
	return out, nil
}

func (s *Service) reduce(tx *sql.Tx, data *store.CaseData, pr *domain.Property, baseline *replay.Result,
	v *domain.Verdict, caseID, branchID string, head *string, ver *int64, actor string,
	baseEng *replay.Engine, sampleOf func(domain.Change) int) (*store.StoredCounterex, error) {

	replayer := func(changes []domain.Change) *replay.Result {
		eng := replay.NewEngine(pr.SemVer, data.Signals, changes, data.Timescale, data.Domains)
		return eng.Run(pr)
	}
	rep := minimize.Run(baseline, data.Changes, sampleOf, replayer)
	cx := &domain.Counterex{
		ID: domain.StableID("cex", v.ID, strconv.Itoa(rep.OriginSample),
			strconv.FormatInt(rep.OriginTick, 10)),
		CaseID: caseID, BranchID: branchID, VerdictID: v.ID,
		OriginSample: rep.OriginSample, OriginTick: rep.OriginTick,
		Kept: rep.Kept, Removed: rep.Removed, Minimal: rep.Minimal,
	}
	for _, alt := range rep.Alternatives {
		cx.Alternatives = append(cx.Alternatives, alt)
	}
	if err := s.DB.SaveCounterex(tx, caseID, cx); err != nil {
		return nil, err
	}
	if err := s.DB.AddDeps(tx, caseID, cx.ID, append([]string{v.ID}, rep.Support...)); err != nil {
		return nil, err
	}
	*ver++
	evID := domain.StableID("ev", "red", cx.ID, strconv.FormatInt(*ver, 10))
	removed := 0
	for _, iv := range rep.Removed {
		removed += iv[1] - iv[0] + 1
	}
	if _, err := store.AppendEventRow(tx, store.EventRow{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvReduction),
		Parent: *head, Refs: []string{cx.ID, v.ID}, Deps: []string{v.ID},
		Detail: map[string]string{
			"origin_sample":     strconv.Itoa(rep.OriginSample),
			"origin_tick":       strconv.FormatInt(rep.OriginTick, 10),
			"removed_regions":   strconv.Itoa(removed),
			"minimal":           strconv.FormatBool(rep.Minimal),
			"kept_intervals":    fmtIntervals(rep.Kept),
			"removed_intervals": fmtIntervals(rep.Removed),
		}, Version: *ver, Actor: actor,
	}); err != nil {
		return nil, err
	}
	*head = evID
	return &store.StoredCounterex{Counterex: *cx}, nil
}

func buildSampleOf(data *store.CaseData, semVer string) func(domain.Change) int {
	// Build edge ticks per clock domain and map each change to the earliest
	// sample that reads it (smallest edge tick strictly greater than change).
	type edge struct {
		tick int64
	}
	_ = edge{}
	// Use the replay engine indirectly: build sampled tables is not exported
	// per domain map here, so reconstruct from a throwaway engine run.
	eng := replay.NewEngine(semVer, data.Signals, data.Changes, data.Timescale, data.Domains)
	ticksByDomain := eng.SampleTicks()
	var allTicks []int64
	for _, ts := range ticksByDomain {
		allTicks = append(allTicks, ts...)
	}
	sort.Slice(allTicks, func(i, j int) bool { return allTicks[i] < allTicks[j] })
	// Dedup.
	uniq := allTicks[:0]
	for i, t := range allTicks {
		if i == 0 || t != allTicks[i-1] {
			uniq = append(uniq, t)
		}
	}
	return func(ch domain.Change) int {
		idx := sort.Search(len(uniq), func(i int) bool { return uniq[i] > ch.AtTick })
		if idx >= len(uniq) {
			return len(uniq) - 1
		}
		return idx
	}
}

func fmtIntervals(ivs [][2]int) string {
	out := ""
	for i, iv := range ivs {
		if i > 0 {
			out += ","
		}
		out += strconv.Itoa(iv[0]) + "-" + strconv.Itoa(iv[1])
	}
	return out
}

func semverOf(data *store.CaseData) string {
	for _, p := range data.Properties {
		if p.SemVer != "" {
			return p.SemVer
		}
	}
	return "1.0"
}

func pathShort(n *domain.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == domain.ASTBool {
		return n.Name
	}
	return n.Kind
}

func statusOf(m replay.Match) string {
	switch {
	case m.Result == domain.TriTrue && m.Vacuous:
		return "empty"
	case m.Result == domain.TriTrue:
		return "matched"
	case m.Result == domain.TriFalse:
		return "failed"
	default:
		return "active"
	}
}

var _ = time.Now
var _ = sql.ErrNoRows
