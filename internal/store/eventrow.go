package store

import (
	"database/sql"
)

// EventRow is the exported constructor shape for appending provenance events.
type EventRow = EventInput

// AppendEventRow inserts the event and advances the branch head/version.
func AppendEventRow(tx *sql.Tx, r eventRow) (int64, error) {
	return AppendEvent(tx, r)
}

// HeadInfo returns the branch head event id and optimistic-lock version.
func HeadInfo(tx *sql.Tx, branchID string) (string, int64, error) {
	var head string
	var ver int64
	if err := tx.QueryRow(`SELECT head_event, version FROM branches WHERE id=?`, branchID).
		Scan(&head, &ver); err != nil {
		return "", 0, err
	}
	return head, ver, nil
}
