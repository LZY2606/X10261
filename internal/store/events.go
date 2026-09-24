package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"gsb/internal/domain"
)

// EventInput is the exported shape used when appending provenance events.
type EventInput struct {
	ID        string
	CaseID    string
	BranchID  string
	Kind      string
	Parent    string
	Refs      []string
	Deps      []string
	Detail    map[string]string
	Version   int64
	OriginRef string
	Actor     string
}

type eventRow = EventInput

func insertEvent(tx *sql.Tx, r eventRow) error {
	_, err := tx.Exec(`INSERT INTO events
		(id,case_id,branch_id,kind,parent_event,refs_json,deps_json,detail_json,version,origin_ref,actor_origin)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.CaseID, r.BranchID, r.Kind, r.Parent,
		mustJSON(r.Refs), mustJSON(r.Deps), mustJSON(r.Detail), r.Version, r.OriginRef, r.Actor)
	return err
}

// AppendEvent appends a provenance event and moves the branch head inside the
// given transaction.
func AppendEvent(tx *sql.Tx, r eventRow) (int64, error) {
	if err := insertEvent(tx, r); err != nil {
		return 0, err
	}
	var seq int64
	if err := tx.QueryRow(`SELECT seq FROM events WHERE id=?`, r.ID).Scan(&seq); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE branches SET head_event=?, version=? WHERE id=?`,
		r.ID, r.Version, r.BranchID); err != nil {
		return 0, err
	}
	return seq, nil
}

// BranchVersion returns the current optimistic-lock version of a branch.
func (d *DB) BranchVersion(caseID, branchID string) (int64, error) {
	var v int64
	err := d.conn.QueryRow(`SELECT version FROM branches WHERE id=? AND case_id=?`,
		branchID, caseID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("branch not found: %s", branchID)
	}
	return v, err
}

type StoredEvent struct {
	domain.Event
	CaseID    string `json:"case_id"`
	OriginRef string `json:"origin_ref"`
}

func (d *DB) Events(caseID, branchID string) ([]StoredEvent, error) {
	rows, err := d.conn.Query(`SELECT seq,id,branch_id,kind,parent_event,refs_json,deps_json,detail_json,version,actor_origin,origin_ref
		FROM events WHERE case_id=? AND branch_id=? ORDER BY seq ASC`, caseID, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		var ev StoredEvent
		var kind, refs, deps, detail string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.BranchID, &kind, &ev.ParentEvent,
			&refs, &deps, &detail, &ev.Version, &ev.ActorOrigin, &ev.OriginRef); err != nil {
			return nil, err
		}
		ev.Kind = domain.EventKind(kind)
		ev.Refs = parseJSON[[]string](refs)
		ev.DependsOn = parseJSON[[]string](deps)
		ev.Detail = parseJSON[map[string]string](detail)
		ev.CaseID = caseID
		out = append(out, ev)
	}
	return out, rows.Err()
}

// CreateBranch forks an interpretation. The old branch, its adjudications and
// the raw bytes they reference remain readable forever.
func (d *DB) CreateBranch(caseID, parentID, name, createdBy string) (string, int64, error) {
	var parentHead string
	var parentVer int64
	row := d.conn.QueryRow(`SELECT head_event, version FROM branches WHERE id=? AND case_id=?`,
		parentID, caseID)
	if err := row.Scan(&parentHead, &parentVer); err != nil {
		return "", 0, err
	}
	branchID := domain.StableID("br", caseID, parentID, name)
	exists, err := d.CaseExists(caseID)
	if err != nil || !exists {
		return "", 0, fmt.Errorf("case not found")
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE branches SET is_current=0 WHERE case_id=? AND is_current=1`, caseID); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(`INSERT INTO branches(id,case_id,name,parent_id,created_by,version,head_event,is_current)
		VALUES(?,?,?,?,?,?,?,1)`,
		branchID, caseID, name, parentID, createdBy, 1, parentHead); err != nil {
		return "", 0, err
	}
	// Properties are copied to the new branch at the chosen semantic version
	// by the caller (replay service); here we only record the fork event.
	evID := domain.StableID("ev", branchID, name, fmt.Sprint(parentVer))
	if _, err := AppendEvent(tx, EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvReinterpret),
		Parent: parentHead, Refs: []string{parentID}, Deps: []string{parentID},
		Detail: map[string]string{"name": name, "created_by": createdBy}, Version: 1, Actor: createdBy,
	}); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return branchID, 1, nil
}

func (d *DB) ListBranches(caseID string) ([]domain.Branch, error) {
	rows, err := d.conn.Query(`SELECT id,name,parent_id,created_by,version,head_event,is_current
		FROM branches WHERE case_id=? ORDER BY rowid ASC`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Branch
	for rows.Next() {
		var b domain.Branch
		var cur int
		if err := rows.Scan(&b.ID, &b.Name, &b.ParentID, &b.CreatedBy, &b.Version, &b.HeadEvent, &cur); err != nil {
			return nil, err
		}
		b.CaseID = caseID
		b.Current = cur == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

// ensureJSON helper re-exported for service use.
var _ = json.Marshal
