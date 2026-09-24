package service

import (
	"database/sql"
	"encoding/json"
	"strconv"

	"gsb/internal/domain"
	"gsb/internal/store"
)

// ReviseSemVer creates a re-interpretation branch at a different semantic
// version. Old branch, adjudications and raw bytes remain readable; only
// verdicts depending on the old semantic version are marked stale (localized
// invalidation, never a global replacement).
func (s *Service) ReviseSemVer(caseID, parentID, newSemVer, actor string) (string, error) {
	branchID, _, err := s.DB.CreateBranch(caseID, parentID, "semver-"+newSemVer, actor)
	if err != nil {
		return "", err
	}
	if err := s.copyPropertiesAtSemVer(caseID, parentID, branchID, newSemVer); err != nil {
		return "", err
	}
	tx, err := s.DB.Raw().Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	head, ver, err := store.HeadInfo(tx, branchID)
	if err != nil {
		return "", err
	}
	ver++
	evID := domain.StableID("ev", "param", branchID, newSemVer)
	if _, err := store.AppendEventRow(tx, store.EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvParamRevise),
		Parent: head, Refs: []string{"semver:" + newSemVer}, Deps: []string{"semver:" + oldSemVer(tx, caseID, parentID)},
		Detail:  map[string]string{"new_semver": newSemVer, "parent_branch": parentID, "actor": actor},
		Version: ver, Actor: actor,
	}); err != nil {
		return "", err
	}
	// Mark old verdicts depending on semantic version stale (localized).
	affected, err := s.DB.MarkStale(tx, caseID, []string{"semver:1.0", "semver:1.1"})
	if err != nil {
		return "", err
	}
	if len(affected) > 0 {
		ver++
		invID := domain.StableID("ev", "inv", branchID, strconv.Itoa(len(affected)))
		if _, err := store.AppendEventRow(tx, store.EventInput{
			ID: invID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvInvalidate),
			Parent: evID, Refs: affected, Deps: []string{"semver:" + newSemVer},
			Detail: map[string]string{
				"scope": "semver_dependent", "affected_count": strconv.Itoa(len(affected)),
				"affected": joinRefs(affected),
			}, Version: ver, Actor: actor,
		}); err != nil {
			return "", err
		}
	}
	// Difference event between semantic versions.
	ver++
	diffID := domain.StableID("ev", "semdiff", branchID)
	if _, err := store.AppendEventRow(tx, store.EventInput{
		ID: diffID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvSemverDiff),
		Parent: evID, Refs: []string{branchID, parentID},
		Deps: []string{"semver:" + newSemVer},
		Detail: map[string]string{"new": newSemVer, "old": "1.0",
			"difference": "X/Z inside disable iff is recorded as explicit unknown; vacuous-match marking preserved"},
		Version: ver, Actor: actor,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return branchID, nil
}

// SourceChange applies a localized change to one raw source kind. The source
// bytes of the parent are immutable; the new branch gets an override row and
// only verdicts that depend on that source kind are invalidated.
func (s *Service) SourceChange(caseID, parentID, kind string, newRaw []byte, actor, note string) (string, error) {
	branchID, _, err := s.DB.CreateBranch(caseID, parentID, "source-"+kind, actor)
	if err != nil {
		return "", err
	}
	// Copy property interpretation to the new branch at its parent's semver.
	data, err := s.DB.LoadCaseData(caseID, parentID)
	if err != nil {
		return "", err
	}
	sem := "1.0"
	if len(data.Properties) > 0 {
		sem = data.Properties[0].SemVer
	}
	if err := s.copyPropertiesAtSemVer(caseID, parentID, branchID, sem); err != nil {
		return "", err
	}
	tx, err := s.DB.Raw().Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := s.DB.SetBranchSource(tx, caseID, branchID, kind, newRaw); err != nil {
		return "", err
	}
	head, ver, err := store.HeadInfo(tx, branchID)
	if err != nil {
		return "", err
	}
	ver++
	evID := domain.StableID("ev", "src", branchID, kind, strconv.FormatInt(ver, 10))
	if _, err := store.AppendEventRow(tx, store.EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvSourceChange),
		Parent: head, Refs: []string{"src:" + kind}, Deps: []string{"src:" + kind},
		Detail:  map[string]string{"kind": kind, "note": note, "actor": actor},
		Version: ver, Actor: actor,
	}); err != nil {
		return "", err
	}
	// Localized invalidation: only verdicts depending on this source kind.
	affected, err := s.DB.MarkStale(tx, caseID, []string{"src:" + kind})
	if err != nil {
		return "", err
	}
	if len(affected) > 0 {
		ver++
		invID := domain.StableID("ev", "inv", branchID, kind, strconv.Itoa(len(affected)))
		if _, err := store.AppendEventRow(tx, store.EventInput{
			ID: invID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvInvalidate),
			Parent: evID, Refs: affected, Deps: []string{"src:" + kind},
			Detail: map[string]string{
				"scope": "source:" + kind, "affected_count": strconv.Itoa(len(affected)),
				"affected": joinRefs(affected),
			}, Version: ver, Actor: actor,
		}); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return branchID, nil
}

func (s *Service) copyPropertiesAtSemVer(caseID, parentID, branchID, semVer string) error {
	data, err := s.DB.LoadCaseData(caseID, parentID)
	if err != nil {
		return err
	}
	tx, err := s.DB.Raw().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range data.Properties {
		p.SemVer = semVer
		p.ID = domain.StableID("prop", p.Name, p.ClockDomain, semVer)
		ast, _ := json.Marshal(p.AST)
		if _, err := tx.Exec(`INSERT INTO properties(id,case_id,branch_id,name,raw_text,ast_json,clock_domain,disable_signal,disable_level,sem_ver)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			p.ID, caseID, branchID, p.Name, p.RawText, string(ast), p.ClockDomain,
			p.DisableSignal, int(p.DisableLevel), p.SemVer); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func oldSemVer(tx *sql.Tx, caseID, branchID string) string {
	var v string
	err := tx.QueryRow(`SELECT sem_ver FROM properties WHERE case_id=? AND branch_id=? LIMIT 1`,
		caseID, branchID).Scan(&v)
	if err != nil {
		return "1.0"
	}
	return v
}

func joinRefs(rs []string) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}
