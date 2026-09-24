package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gsb/internal/domain"
	"gsb/internal/prop"
	"gsb/internal/vcd"
)

// ErrConflict is returned when an optimistic-lock check fails: two review
// windows adjudicated the same branch and the later writer must reconcile.
var ErrConflict = errors.New("store: version conflict; branch changed by another writer")

// ErrRolledBack records that an import failed and its transaction was rolled
// back, so no partial records remain.
type RollbackError struct{ Reason string }

func (e *RollbackError) Error() string { return "import rolled back: " + e.Reason }

// ClockDoc is the JSON representation of clock domain declarations.
type ClockDoc struct {
	Domains []struct {
		Name       string `json:"name"`
		Clock      string `json:"clock"`
		Edge       string `json:"edge"`
		Reset      string `json:"reset,omitempty"`
		ResetLevel string `json:"reset_level,omitempty"` // "0" | "1"
	} `json:"domains"`
}

// ImportCase validates and stores a complete case inside a single
// transaction. Any failure rolls the whole import back (no half records).
// Re-importing the same bytes is idempotent and returns the existing case id.
func (d *DB) ImportCase(name string, vcdBytes, clockBytes, propBytes []byte) (string, error) {
	// Parse before opening the transaction so parser errors leave no trace.
	parsed, err := vcd.Parse(vcdBytes)
	if err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	if len(parsed.Signals) == 0 {
		return "", &RollbackError{Reason: "no signals declared in VCD"}
	}
	var clocks ClockDoc
	if err := json.Unmarshal(clockBytes, &clocks); err != nil {
		return "", &RollbackError{Reason: "clock domains invalid JSON: " + err.Error()}
	}
	if len(clocks.Domains) == 0 {
		return "", &RollbackError{Reason: "at least one clock domain required"}
	}
	file, err := prop.ParseFile(string(propBytes))
	if err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	names := map[string]bool{}
	for _, sg := range parsed.Signals {
		names[sg.Name] = true
	}
	var cds []domain.ClockDomain
	for _, c := range clocks.Domains {
		if !names[c.Clock] {
			return "", &RollbackError{Reason: "clock signal not present in VCD: " + c.Clock}
		}
		if c.Edge != "posedge" && c.Edge != "negedge" {
			return "", &RollbackError{Reason: "edge must be posedge or negedge: " + c.Name}
		}
		if c.Reset != "" && !names[c.Reset] {
			return "", &RollbackError{Reason: "reset signal not present in VCD: " + c.Reset}
		}
		lvl := domain.L0
		if c.ResetLevel == "1" {
			lvl = domain.L1
		}
		cd := domain.ClockDomain{
			Name: c.Name, ClockSignal: c.Clock, Edge: c.Edge,
			ResetSignal: c.Reset, ResetLevel: lvl, RawText: rawCD(c),
		}
		cd.ID = domain.StableID("cd", cd.RawText)
		cds = append(cds, cd)
	}
	clockNames := map[string]bool{}
	for _, cd := range cds {
		clockNames[cd.Name] = true
	}
	for _, p := range file.Properties {
		if !clockNames[p.ClockDomain] {
			return "", &RollbackError{Reason: "property references unknown clock domain: " + p.ClockDomain}
		}
		if p.DisableSignal != "" && !names[p.DisableSignal] {
			return "", &RollbackError{Reason: "disable iff signal not present: " + p.DisableSignal}
		}
	}

	caseID := domain.StableID("case", sha(vcdBytes), sha(clockBytes), sha(propBytes))
	if existing, err := d.CaseExists(caseID); err != nil {
		return "", err
	} else if existing {
		return caseID, nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`INSERT INTO cases(id,name,created_at) VALUES(?,?,?)`,
		caseID, name, time.Now().UnixNano()); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	for _, kind := range []string{"vcd", "clockdomains", "properties"} {
		var raw []byte
		switch kind {
		case "vcd":
			raw = vcdBytes
		case "clockdomains":
			raw = clockBytes
		case "properties":
			raw = propBytes
		}
		if _, err := tx.Exec(`INSERT INTO sources(case_id,kind,raw,sha256) VALUES(?,?,?,?)`,
			caseID, kind, raw, sha(raw)); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}
	if _, err := tx.Exec(`INSERT INTO timescale(case_id,raw,number,unit,base_fs,convertible) VALUES(?,?,?,?,?,?)`,
		caseID, parsed.Timescale.Raw, parsed.Timescale.Number, parsed.Timescale.Unit,
		parsed.Timescale.BaseFS, boolInt(parsed.Timescale.Convertible)); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	for _, sg := range parsed.Signals {
		if _, err := tx.Exec(`INSERT INTO signals(id,case_id,code,name,raw_name,width) VALUES(?,?,?,?,?,?)`,
			sg.ID, caseID, sg.Code, sg.Name, sg.RawName, sg.Width); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}
	for _, ch := range parsed.Changes {
		bits := make([]byte, len(ch.Bits))
		for i, b := range ch.Bits {
			bits[i] = b.Char()
		}
		if _, err := tx.Exec(`INSERT INTO changes(id,case_id,signal_id,seq,at_tick,raw_value,bits,width,non_canonical)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			ch.ID, caseID, ch.SignalID, ch.Index, ch.AtTick, ch.RawValue,
			string(bits), ch.Width, boolInt(ch.NonCanon)); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}
	for _, cd := range cds {
		if _, err := tx.Exec(`INSERT INTO clock_domains(id,case_id,name,clock_signal,edge,reset_signal,reset_level,raw_text)
			VALUES(?,?,?,?,?,?,?,?)`,
			cd.ID, caseID, cd.Name, cd.ClockSignal, cd.Edge, cd.ResetSignal, int(cd.ResetLevel), cd.RawText); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}
	for _, dg := range parsed.Diagnostics {
		if _, err := tx.Exec(`INSERT INTO diagnostics(id,case_id,ref_id,code,message,raw_value) VALUES(?,?,?,?,?,?)`,
			dg.ID, caseID, dg.RefID, dg.Code, dg.Message, dg.RawValue); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}

	branchID := domain.StableID("br", caseID, "baseline")
	if _, err := tx.Exec(`INSERT INTO branches(id,case_id,name,parent_id,created_by,version,head_event,is_current)
		VALUES(?,?,?,?,?,?,?,1)`,
		branchID, caseID, "baseline", "", "import", 1, ""); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	for _, p := range file.Properties {
		p.SemVer = "1.0"
		p.ID = domain.StableID("prop", p.Name, p.ClockDomain, p.SemVer)
		astJSON, _ := json.Marshal(p.AST)
		if _, err := tx.Exec(`INSERT INTO properties(id,case_id,branch_id,name,raw_text,ast_json,clock_domain,disable_signal,disable_level,sem_ver)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			p.ID, caseID, branchID, p.Name, p.RawText, string(astJSON),
			p.ClockDomain, p.DisableSignal, int(p.DisableLevel), p.SemVer); err != nil {
			return "", &RollbackError{Reason: err.Error()}
		}
	}
	evID := domain.StableID("ev", caseID, "import", fmt.Sprint(time.Now().UnixNano()))
	if err := insertEvent(tx, EventInput{
		ID: evID, CaseID: caseID, BranchID: branchID, Kind: string(domain.EvImport),
		Parent: "", Refs: []string{caseID}, Deps: []string{sha(vcdBytes), sha(clockBytes), sha(propBytes)},
		Detail: map[string]string{
			"timescale_raw":         parsed.Timescale.Raw,
			"timescale_convertible": fmt.Sprint(parsed.Timescale.Convertible),
			"changes":               fmt.Sprint(len(parsed.Changes)),
			"signals":               fmt.Sprint(len(parsed.Signals)),
			"properties":            fmt.Sprint(len(file.Properties)),
		},
		Version: 1,
	}); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	if _, err := tx.Exec(`UPDATE branches SET head_event=? WHERE id=?`, evID, branchID); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	if err := tx.Commit(); err != nil {
		return "", &RollbackError{Reason: err.Error()}
	}
	committed = true
	return caseID, nil
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func rawCD(c struct {
	Name       string `json:"name"`
	Clock      string `json:"clock"`
	Edge       string `json:"edge"`
	Reset      string `json:"reset,omitempty"`
	ResetLevel string `json:"reset_level,omitempty"`
}) string {
	b, _ := json.Marshal(c)
	return string(b)
}

func (d *DB) CaseExists(id string) (bool, error) {
	var n int
	if err := d.conn.QueryRow(`SELECT COUNT(*) FROM cases WHERE id=?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
