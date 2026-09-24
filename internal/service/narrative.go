package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"gsb/internal/cases"
	"gsb/internal/store"
)

// NarrativeState tracks guided-tour progress for one case.
type NarrativeState struct {
	CaseID   string                `json:"case_id"`
	Step     int                   `json:"step"`
	Branches map[string]string     `json:"branches"` // label -> branch id
	LastAdj  string                `json:"last_adj"`
	Conflict *store.ConflictDetail `json:"conflict,omitempty"`
}

// EnsureCase imports the embedded case pack if absent and returns its id.
func (s *Service) EnsureCase() (string, error) {
	return s.DB.ImportCase("信号言词庭案例包", cases.WaveVCD, cases.ClocksJSON, cases.PropsSVA)
}

// RunStep executes one narrative action. It is idempotent per branch label so
// refreshing the page never duplicates branches.
func (s *Service) RunStep(st *NarrativeState, action string) (*NarrativeState, error) {
	if st.Branches == nil {
		st.Branches = map[string]string{}
	}
	base, err := s.baseBranch(st.CaseID)
	if err != nil {
		return nil, err
	}
	switch action {
	case "import":
		id, err := s.EnsureCase()
		if err != nil {
			return nil, err
		}
		st.CaseID = id
	case "replay":
		if _, err := s.RunReplay(st.CaseID, base, "narrative"); err != nil {
			return nil, err
		}
	case "adjudicate":
		verdicts, err := s.DB.LoadVerdicts(st.CaseID, base)
		if err != nil {
			return nil, err
		}
		target := ""
		for _, v := range verdicts {
			if v.Result.String() == "false" {
				target = v.ID
				break
			}
		}
		if target == "" {
			return nil, errors.New("no failing verdict to adjudicate")
		}
		ver, _ := s.DB.BranchVersion(st.CaseID, base)
		adj, conflict, err := s.DB.Adjudicate(st.CaseID, base, target, "accept",
			"基线失败候选经复核确认", "reviewer-a", ver)
		if err != nil {
			return nil, err
		}
		_ = conflict
		st.LastAdj = adj.ID
	case "revise-semver":
		if id, ok := st.Branches["semver"]; ok {
			_ = id
			break
		}
		id, err := s.ReviseSemVer(st.CaseID, base, "1.1", "narrative")
		if err != nil {
			return nil, err
		}
		st.Branches["semver"] = id
	case "replay-branch":
		br, ok := st.Branches["semver"]
		if !ok {
			return nil, errors.New("semver branch missing; run revise-semver first")
		}
		if _, err := s.RunReplay(st.CaseID, br, "narrative"); err != nil {
			return nil, err
		}
	case "source-change":
		if _, ok := st.Branches["source"]; ok {
			break
		}
		id, err := s.SourceChange(st.CaseID, base, "vcd", cases.Wave2VCD, "narrative",
			"ack 脉冲移动；时标 100ps→1ps")
		if err != nil {
			return nil, err
		}
		st.Branches["source"] = id
	case "replay-source":
		br, ok := st.Branches["source"]
		if !ok {
			return nil, errors.New("source branch missing; run source-change first")
		}
		if _, err := s.RunReplay(st.CaseID, br, "narrative"); err != nil {
			return nil, err
		}
	case "conflict":
		verdicts, err := s.DB.LoadVerdicts(st.CaseID, base)
		if err != nil {
			return nil, err
		}
		if len(verdicts) == 0 {
			return nil, errors.New("no verdicts; run replay first")
		}
		target := verdicts[0].ID
		ver, _ := s.DB.BranchVersion(st.CaseID, base)
		// Window 1 commits at version ver.
		if _, _, err := s.DB.Adjudicate(st.CaseID, base, target, "reject",
			"窗口一：否定该候选", "reviewer-b", ver); err != nil {
			return nil, err
		}
		// Window 2 submits against the same stale version -> conflict.
		_, conflict, err := s.DB.Adjudicate(st.CaseID, base, target, "accept",
			"窗口二：接受该候选", "reviewer-c", ver)
		if !errors.Is(err, store.ErrConflict) {
			return nil, fmt.Errorf("expected conflict, got %v", err)
		}
		st.Conflict = conflict
	case "undo":
		adjs, err := s.DB.ListAdjudications(st.CaseID, base)
		if err != nil {
			return nil, err
		}
		var last string
		for _, a := range adjs {
			if !a.Undone {
				last = a.ID
			}
		}
		if last == "" {
			return nil, errors.New("nothing to undo")
		}
		if _, err := s.DB.UndoAdjudication(st.CaseID, base, last, "reviewer-a"); err != nil {
			return nil, err
		}
	case "export":
		if _, err := s.DB.ExportBundle(st.CaseID); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown narrative action %q", action)
	}
	st.Step++
	return st, nil
}

func (s *Service) baseBranch(caseID string) (string, error) {
	branches, err := s.DB.ListBranches(caseID)
	if err != nil {
		return "", err
	}
	for _, b := range branches {
		if b.Name == "baseline" {
			return b.ID, nil
		}
	}
	if len(branches) > 0 {
		return branches[0].ID, nil
	}
	return "", errors.New("no branches")
}

// ExportJSON returns the bundle as indented JSON for download.
func (s *Service) ExportJSON(caseID string) ([]byte, error) {
	b, err := s.DB.ExportBundle(caseID)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(b, "", "  ")
}
