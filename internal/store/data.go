package store

import (
	"database/sql"
	"encoding/json"
	"strings"

	"gsb/internal/domain"
	"gsb/internal/prop"
	"gsb/internal/vcd"
)

type CaseData struct {
	CaseID      string
	Name        string
	Timescale   domain.Timescale
	Signals     []domain.Signal
	Changes     []domain.Change
	Domains     []domain.ClockDomain
	Properties  []*domain.Property
	BranchID    string
	Diagnostics []domain.Diagnostic
}

func (d *DB) ListCases() ([]domain.Branch, []struct{ ID, Name string }, error) {
	rows, err := d.conn.Query(`SELECT id,name FROM cases ORDER BY created_at ASC`)
	if err != nil {
		return nil, nil, err
	}
	type cn = struct{ ID, Name string }
	var cs []cn
	for rows.Next() {
		var c cn
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		cs = append(cs, c)
	}
	rows.Close()
	var all []domain.Branch
	for _, c := range cs {
		bs, err := d.ListBranches(c.ID)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, bs...)
	}
	type cc = struct{ ID, Name string }
	out := make([]cc, len(cs))
	for i, c := range cs {
		out[i] = cc(c)
	}
	return all, out, nil
}

// LoadCaseData reads sources-as-derived rows for a branch. Property records
// are the branch-local interpretation; raw bytes are fetched separately.
// Branch-local source overrides (localized source changes) take precedence
// over the baseline rows and are re-parsed on read.
func (d *DB) LoadCaseData(caseID, branchID string) (*CaseData, error) {
	cd := &CaseData{CaseID: caseID, BranchID: branchID}
	if err := d.conn.QueryRow(`SELECT name FROM cases WHERE id=?`, caseID).Scan(&cd.Name); err != nil {
		return nil, err
	}

	vcdRaw, err := d.BranchSourceBytes(caseID, branchID, "vcd")
	if err != nil {
		return nil, err
	}
	parsed, err := vcd.Parse(vcdRaw)
	if err != nil {
		return nil, err
	}
	cd.Timescale = parsed.Timescale
	cd.Signals = parsed.Signals
	cd.Changes = parsed.Changes
	cd.Diagnostics = parsed.Diagnostics

	rows, err := d.conn.Query(`SELECT id,name,clock_signal,edge,reset_signal,reset_level,raw_text
		FROM clock_domains WHERE case_id=? ORDER BY name ASC`, caseID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c domain.ClockDomain
		var lvl int
		if err := rows.Scan(&c.ID, &c.Name, &c.ClockSignal, &c.Edge, &c.ResetSignal, &lvl, &c.RawText); err != nil {
			rows.Close()
			return nil, err
		}
		c.ResetLevel = domain.Logic(lvl)
		cd.Domains = append(cd.Domains, c)
	}
	rows.Close()

	rows, err = d.conn.Query(`SELECT id,name,raw_text,ast_json,clock_domain,disable_signal,disable_level,sem_ver
		FROM properties WHERE case_id=? AND branch_id=? ORDER BY name ASC`, caseID, branchID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		p := &domain.Property{}
		var ast, dis string
		var lvl int
		if err := rows.Scan(&p.ID, &p.Name, &p.RawText, &ast, &p.ClockDomain, &dis, &lvl, &p.SemVer); err != nil {
			rows.Close()
			return nil, err
		}
		p.DisableSignal = dis
		p.DisableLevel = domain.Logic(lvl)
		_ = json.Unmarshal([]byte(ast), &p.AST)
		cd.Properties = append(cd.Properties, p)
	}
	rows.Close()

	// A properties override replaces the branch interpretation entirely.
	if propRaw, err := d.BranchSourceBytes(caseID, branchID, "properties"); err == nil {
		if f, perr := prop.ParseFile(string(propRaw)); perr == nil {
			sem := "1.0"
			if len(cd.Properties) > 0 {
				sem = cd.Properties[0].SemVer
			}
			cd.Properties = nil
			for _, pr := range f.Properties {
				pr.SemVer = sem
				pr.ID = domain.StableID("prop", pr.Name, pr.ClockDomain, sem)
				cd.Properties = append(cd.Properties, pr)
			}
		}
	}
	return cd, nil
}

// RawSourceBytes returns the original imported bytes for one source kind.
func (d *DB) RawSourceBytes(caseID, kind string) ([]byte, error) {
	var raw []byte
	if err := d.conn.QueryRow(`SELECT raw FROM sources WHERE case_id=? AND kind=?`,
		caseID, kind).Scan(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// BranchSourceBytes returns the branch-local override if present, else the
// original source bytes.
func (d *DB) BranchSourceBytes(caseID, branchID, kind string) ([]byte, error) {
	var raw []byte
	err := d.conn.QueryRow(`SELECT raw FROM branch_sources WHERE case_id=? AND branch_id=? AND kind=?`,
		caseID, branchID, kind).Scan(&raw)
	if err == nil {
		return raw, nil
	}
	return d.RawSourceBytes(caseID, kind)
}

// SetBranchSource stores a branch-local source override.
func (d *DB) SetBranchSource(tx *sql.Tx, caseID, branchID, kind string, raw []byte) error {
	_, err := tx.Exec(`INSERT INTO branch_sources(case_id,branch_id,kind,raw,sha256) VALUES(?,?,?,?,?)
		ON CONFLICT(case_id,branch_id,kind) DO UPDATE SET raw=excluded.raw, sha256=excluded.sha256`,
		caseID, branchID, kind, raw, sha(raw))
	return err
}

// SaveVerdict upserts a branch-local verdict.
func (d *DB) SaveVerdict(tx *sql.Tx, caseID string, v *domain.Verdict, eventID string) error {
	_, err := tx.Exec(`INSERT INTO verdicts
		(id,case_id,branch_id,property_id,sem_ver,result,empty_match,candidates_json,threads_json,stale,event_id)
		VALUES(?,?,?,?,?,?,?,?,?,0,?)
		ON CONFLICT(id,branch_id) DO UPDATE SET
			sem_ver=excluded.sem_ver, result=excluded.result, empty_match=excluded.empty_match,
			candidates_json=excluded.candidates_json, threads_json=excluded.threads_json,
			stale=0, event_id=excluded.event_id`,
		v.ID, caseID, v.BranchID, v.PropertyID, v.SemVer, v.Result.String(),
		boolInt(v.EmptyMatch), mustJSON(v.Candidates), mustJSON(v.Threads), eventID)
	return err
}

type StoredVerdict struct {
	domain.Verdict
	CaseID  string
	Stale   bool
	EventID string
}

func (d *DB) LoadVerdicts(caseID, branchID string) ([]StoredVerdict, error) {
	rows, err := d.conn.Query(`SELECT id,property_id,sem_ver,result,empty_match,candidates_json,threads_json,stale,event_id
		FROM verdicts WHERE case_id=? AND branch_id=? ORDER BY rowid ASC`, caseID, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredVerdict
	for rows.Next() {
		var sv StoredVerdict
		var res, cands, threads string
		var empty, stale int
		if err := rows.Scan(&sv.ID, &sv.PropertyID, &sv.SemVer, &res, &empty, &cands, &threads, &stale, &sv.EventID); err != nil {
			return nil, err
		}
		sv.CaseID = caseID
		sv.BranchID = branchID
		sv.EmptyMatch = empty == 1
		sv.Stale = stale == 1
		switch res {
		case "true":
			sv.Result = domain.TriTrue
		case "false":
			sv.Result = domain.TriFalse
		default:
			sResult := domain.TriUnknown
			sv.Result = sResult
		}
		sv.Candidates = parseJSON[[]domain.Candidate](cands)
		sv.Threads = parseJSON[[]domain.ThreadState](threads)
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (d *DB) SaveCounterex(tx *sql.Tx, caseID string, cx *domain.Counterex) error {
	_, err := tx.Exec(`INSERT INTO counterexamples
		(id,case_id,branch_id,verdict_id,origin_sample,origin_tick,kept_json,removed_json,minimal,alternatives_json,stale)
		VALUES(?,?,?,?,?,?,?,?,?,?,0)
		ON CONFLICT(id) DO UPDATE SET
			kept_json=excluded.kept_json, removed_json=excluded.removed_json,
			minimal=excluded.minimal, alternatives_json=excluded.alternatives_json, stale=0`,
		cx.ID, caseID, cx.BranchID, cx.VerdictID, cx.OriginSample, cx.OriginTick,
		mustJSON(cx.Kept), mustJSON(cx.Removed), boolInt(cx.Minimal), mustJSON(cx.Alternatives))
	return err
}

type StoredCounterex struct {
	domain.Counterex
	Stale bool
}

func (d *DB) LoadCounterex(caseID, branchID, verdictID string) (*StoredCounterex, error) {
	var cx StoredCounterex
	var kept, removed, alts string
	var minimal, stale int
	err := d.conn.QueryRow(`SELECT id,origin_sample,origin_tick,kept_json,removed_json,minimal,alternatives_json,stale
		FROM counterexamples WHERE case_id=? AND branch_id=? AND verdict_id=?`,
		caseID, branchID, verdictID).Scan(&cx.ID, &cx.OriginSample, &cx.OriginTick,
		&kept, &removed, &minimal, &alts, &stale)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cx.VerdictID = verdictID
	cx.BranchID = branchID
	cx.Minimal = minimal == 1
	cx.Stale = stale == 1
	cx.Kept = parseJSON[[][2]int](kept)
	cx.Removed = parseJSON[[][2]int](removed)
	cx.Alternatives = parseJSON[[][][2]int](alts)
	return &cx, nil
}

// MarkStale marks derived objects that depend on changed refs as stale. Old
// conclusions are never globally replaced; they remain readable with the
// stale flag and an invalidation event links them to the new dependency state.
func (d *DB) MarkStale(tx *sql.Tx, caseID string, changedRefs []string) ([]string, error) {
	if len(changedRefs) == 0 {
		return nil, nil
	}
	var affected []string
	rows, err := tx.Query(`SELECT DISTINCT from_ref FROM deps WHERE case_id=? AND to_ref IN (`+placeholders(len(changedRefs))+`)`,
		append([]any{caseID}, stringsAny(changedRefs)...)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return nil, err
		}
		affected = append(affected, ref)
	}
	rows.Close()
	for _, ref := range affected {
		if _, err := tx.Exec(`UPDATE verdicts SET stale=1 WHERE case_id=? AND id=?`, caseID, ref); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE counterexamples SET stale=1 WHERE case_id=? AND id=?`, caseID, ref); err != nil {
			return nil, err
		}
	}
	return affected, nil
}

func (d *DB) AddDeps(tx *sql.Tx, caseID, fromRef string, toRefs []string) error {
	for _, to := range toRefs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO deps(from_ref,to_ref,case_id) VALUES(?,?,?)`,
			fromRef, to, caseID); err != nil {
			return err
		}
	}
	return nil
}

func placeholders(n int) string {
	return strings.Repeat("?,", n-1) + "?"
}

func stringsAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// SetMeta stores a small JSON blob (e.g. narrative progress).
func (d *DB) SetMeta(key, value string) error {
	_, err := d.conn.Exec(`INSERT INTO meta(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMeta reads a meta blob; missing keys yield "".
func (d *DB) GetMeta(key string) (string, error) {
	var v string
	err := d.conn.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
