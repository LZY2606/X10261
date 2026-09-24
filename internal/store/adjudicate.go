package store

import (
	"database/sql"
	"errors"
	"fmt"

	"gsb/internal/domain"
)

// Adjudicate applies accept/reject to a candidate or verdict. It never mutates
// source data: only a new adjudication row and a new event are appended.
// expectedVersion implements optimistic concurrency: a second review window
// saving against a stale version receives ErrConflict plus both-side details.
func (d *DB) Adjudicate(caseID, branchID, targetID, decision, reason, actor string, expectedVersion int64) (*domain.Adjudication, *ConflictDetail, error) {
	if decision != "accept" && decision != "reject" {
		return nil, nil, fmt.Errorf("decision must be accept or reject")
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var currentVer int64
	var head string
	if err := tx.QueryRow(`SELECT version,head_event FROM branches WHERE id=? AND case_id=?`,
		branchID, caseID).Scan(&currentVer, &head); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("branch not found")
		}
		return nil, nil, err
	}
	if expectedVersion != 0 && expectedVersion != currentVer {
		theirs, _ := d.lastDecision(caseID, branchID)
		yours := fmt.Sprintf("%s %s (v%d)", decision, targetID, expectedVersion)
		conflict := &ConflictDetail{
			BranchID: branchID, CurrentVersion: currentVer, YourVersion: expectedVersion,
			TheirLast: theirs, YourAttempt: yours,
		}
		evID := domain.StableID("conf", branchID, targetID, fmt.Sprint(expectedVersion), actor)
		_ = insertEvent(tx, EventInput{
			ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvConflict),
			Parent: head, Refs: []string{targetID},
			Detail: map[string]string{
				"actor": actor, "expected_version": fmt.Sprint(expectedVersion),
				"current_version": fmt.Sprint(currentVer), "their_last": theirs, "your_attempt": yours,
			},
			Version: currentVer, Actor: actor,
		})
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return nil, conflict, ErrConflict
	}

	// A later decision supersedes the previous decision on the same target.
	var supersedes string
	tx.QueryRow(`SELECT id FROM adjudications WHERE case_id=? AND branch_id=? AND target_id=? AND undone=0
		ORDER BY rowid DESC LIMIT 1`, caseID, branchID, targetID).Scan(&supersedes)
	if supersedes != "" {
		if _, err := tx.Exec(`UPDATE adjudications SET undone=1 WHERE id=?`, supersedes); err != nil {
			return nil, nil, err
		}
	}
	adjID := domain.StableID("adj", caseID, branchID, targetID, decision, fmt.Sprint(currentVer+1))
	newVer := currentVer + 1
	evID := domain.StableID("ev", adjID)
	adj := &domain.Adjudication{
		ID: adjID, BranchID: branchID, TargetID: targetID, Decision: decision,
		Reason: reason, Supersedes: supersedes, EventID: evID, Version: newVer,
	}
	if _, err := tx.Exec(`INSERT INTO adjudications(id,case_id,branch_id,target_id,decision,reason,undone,supersedes,event_id,version)
		VALUES(?,?,?,?,?, ?,0,?,?,?)`,
		adjID, caseID, branchID, targetID, decision, reason, supersedes, evID, newVer); err != nil {
		return nil, nil, err
	}
	if err := d.AddDeps(tx, caseID, targetID, []string{adjID}); err != nil {
		return nil, nil, err
	}
	if _, err := AppendEvent(tx, EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvAdjudicate),
		Parent: head, Refs: []string{targetID, adjID}, Deps: []string{targetID},
		Detail: map[string]string{
			"decision": decision, "reason": reason, "actor": actor,
			"supersedes": supersedes, "target": targetID,
		},
		Version: newVer, Actor: actor,
	}); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return adj, nil, nil
}

// ConflictDetail carries both sides of a two-window save conflict.
type ConflictDetail struct {
	BranchID       string `json:"branch_id"`
	CurrentVersion int64  `json:"current_version"`
	YourVersion    int64  `json:"your_version"`
	TheirLast      string `json:"their_last"`
	YourAttempt    string `json:"your_attempt"`
}

func (d *DB) lastDecision(caseID, branchID string) (string, error) {
	var decision, target string
	err := d.conn.QueryRow(`SELECT decision,target_id FROM adjudications
		WHERE case_id=? AND branch_id=? AND undone=0 ORDER BY rowid DESC LIMIT 1`,
		caseID, branchID).Scan(&decision, &target)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return decision + " " + target, nil
}

// UndoAdjudication restores the previous branch state by marking the decision
// undone and appending an undo event. Source data is never rewritten.
func (d *DB) UndoAdjudication(caseID, branchID, adjID, actor string) (int64, error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var target string
	if err := tx.QueryRow(`SELECT target_id FROM adjudications WHERE id=? AND case_id=? AND branch_id=?`,
		adjID, caseID, branchID).Scan(&target); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE adjudications SET undone=1 WHERE id=?`, adjID); err != nil {
		return 0, err
	}
	// Restore the prior decision (if any) to active.
	var prior string
	tx.QueryRow(`SELECT id FROM adjudications WHERE target_id=? AND id<>? AND undone=1
		ORDER BY rowid DESC LIMIT 1`, target, adjID).Scan(&prior)
	if prior != "" {
		if _, err := tx.Exec(`UPDATE adjudications SET undone=0 WHERE id=?`, prior); err != nil {
			return 0, err
		}
	}
	var ver int64
	var head string
	tx.QueryRow(`SELECT version,head_event FROM branches WHERE id=?`, branchID).Scan(&ver, &head)
	ver++
	evID := domain.StableID("ev", "undo", adjID, fmt.Sprint(ver))
	if _, err := AppendEvent(tx, EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvUndo),
		Parent: head, Refs: []string{adjID, prior}, Deps: []string{target},
		Detail:  map[string]string{"undone": adjID, "restored": prior, "actor": actor},
		Version: ver, Actor: actor,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return ver, nil
}

func (d *DB) ListAdjudications(caseID, branchID string) ([]domain.Adjudication, error) {
	rows, err := d.conn.Query(`SELECT id,target_id,decision,reason,undone,supersedes,event_id,version
		FROM adjudications WHERE case_id=? AND branch_id=? ORDER BY rowid ASC`, caseID, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Adjudication
	for rows.Next() {
		var a domain.Adjudication
		var undone int
		if err := rows.Scan(&a.ID, &a.TargetID, &a.Decision, &a.Reason, &undone, &a.Supersedes, &a.EventID, &a.Version); err != nil {
			return nil, err
		}
		a.Undone = undone == 1
		a.BranchID = branchID
		out = append(out, a)
	}
	return out, rows.Err()
}
