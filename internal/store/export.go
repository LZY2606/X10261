package store

import (
	"encoding/json"
)

// ExportBundle serializes raw source bytes plus the human timeline so that
// identity (content-addressed), source order and provenance survive deletion
// of the local database and re-import.
func (d *DB) ExportBundle(caseID string) (*CaseBundle, error) {
	var name string
	if err := d.conn.QueryRow(`SELECT name FROM cases WHERE id=?`, caseID).Scan(&name); err != nil {
		return nil, err
	}
	b := &CaseBundle{Schema: SchemaVersion, CaseID: caseID, Name: name,
		Sources: map[string]RawSource{}, Order: []string{"vcd", "clockdomains", "properties"}}
	for _, kind := range b.Order {
		raw, err := d.RawSourceBytes(caseID, kind)
		if err != nil {
			return nil, err
		}
		b.Sources[kind] = RawSource{Kind: kind, Raw: raw}
	}
	branches, err := d.ListBranches(caseID)
	if err != nil {
		return nil, err
	}
	// Merge every branch's events/adjudications into one stable timeline;
	// branch ids are content-addressed so the order and links reproduce.
	for _, br := range branches {
		evs, err := d.Events(caseID, br.ID)
		if err != nil {
			return nil, err
		}
		for _, ev := range evs {
			b.Timeline = append(b.Timeline, ev.Event)
		}
	}
	data, _ := json.Marshal(b)
	_ = data
	return b, nil
}

// DeleteCase removes every row of a case (FK cascade). The raw bytes remain
// available only via a previously exported bundle.
func (d *DB) DeleteCase(caseID string) error {
	_, err := d.conn.Exec(`DELETE FROM cases WHERE id=?`, caseID)
	return err
}
