package webui

import (
	"sort"

	"gsb/internal/domain"
	"gsb/internal/store"
)

// HistorySnapshot reconstructs the visible conclusions and dependency versions
// as of a given event sequence, by replaying the event log — never by
// overwriting history with the latest computation.
type HistorySnapshot struct {
	Seq           int64                 `json:"seq"`
	Events        []store.StoredEvent   `json:"events"`
	Adjudications []domain.Adjudication `json:"adjudications"`
	Verdicts      []VerdictView         `json:"verdicts"`
	StaleRefs     []string              `json:"stale_refs"`
}

type VerdictView struct {
	ID         string `json:"id"`
	PropertyID string `json:"property_id"`
	Result     string `json:"result"`
	SemVer     string `json:"sem_ver"`
	Stale      bool   `json:"stale"`
	EventID    string `json:"event_id"`
}

// SnapshotAt rebuilds branch state visible at event sequence seq.
func SnapshotAt(db *store.DB, caseID, branchID string, seq int64) (*HistorySnapshot, error) {
	events, err := db.Events(caseID, branchID)
	if err != nil {
		return nil, err
	}
	snap := &HistorySnapshot{Seq: seq}
	eventSeq := map[string]int64{}
	for _, ev := range events {
		eventSeq[ev.ID] = ev.Seq
		if ev.Seq <= seq {
			snap.Events = append(snap.Events, ev)
		}
	}
	// Invalidation scope visible at seq.
	stale := map[string]bool{}
	for _, ev := range snap.Events {
		if ev.Kind == domain.EvInvalidate {
			for _, r := range ev.Refs {
				stale[r] = true
			}
		}
	}
	for r := range stale {
		snap.StaleRefs = append(snap.StaleRefs, r)
	}
	sort.Strings(snap.StaleRefs)

	// Adjudications: apply decision/undo events in order up to seq.
	adjs, err := db.ListAdjudications(caseID, branchID)
	if err != nil {
		return nil, err
	}
	undone := map[string]bool{}
	for _, ev := range snap.Events {
		if ev.Kind == domain.EvUndo && len(ev.Refs) > 0 {
			undone[ev.Refs[0]] = true
			if len(ev.Refs) > 1 && ev.Refs[1] != "" {
				delete(undone, ev.Refs[1])
			}
		}
	}
	for _, a := range adjs {
		if eventSeq[a.EventID] == 0 || eventSeq[a.EventID] > seq {
			continue
		}
		a.Undone = undone[a.ID]
		snap.Adjudications = append(snap.Adjudications, a)
	}

	// Verdicts visible at seq: created by an event <= seq; stale if an
	// invalidation <= seq covered them.
	verdicts, err := db.LoadVerdicts(caseID, branchID)
	if err != nil {
		return nil, err
	}
	for _, v := range verdicts {
		created := eventSeq[v.EventID]
		if created == 0 || created > seq {
			continue
		}
		snap.Verdicts = append(snap.Verdicts, VerdictView{
			ID: v.ID, PropertyID: v.PropertyID, Result: v.Result.String(),
			SemVer: v.SemVer, Stale: stale[v.ID], EventID: v.EventID,
		})
	}
	return snap, nil
}
