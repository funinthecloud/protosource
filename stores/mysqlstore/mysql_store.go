package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
)

// MySQLStore is a database-backed implementation of the Store interface.
// It uses a single MySQL table to persist and manage aggregate records.
type MySQLStore struct {
	db               *sql.DB
	snapshotInterval int64
}

// NewMySQLStore initializes and returns a new instance of MySQLStore.
// It receives an open `sql.DB` connection and an optional snapshot interval.
func NewMySQLStore(db *sql.DB, snapshotInterval int64) *MySQLStore {
	return &MySQLStore{
		db:               db,
		snapshotInterval: snapshotInterval,
	}
}

// SnapshotInterval returns the configured snapshot interval.
func (m *MySQLStore) SnapshotInterval() int64 {
	return m.snapshotInterval
}

// Save persists a list of records for a given aggregate ID in the database.
// If any record conflicts (e.g., duplicate aggregate ID and version), an error is returned.
func (m *MySQLStore) Save(ctx context.Context, aggregateId string, records ...*recordv1.Record) error {
	if len(records) == 0 {
		return errors.New("mysqlstore.Save: no records to save")
	}

	tx, err := m.db.BeginTx(ctx, nil) // Start a transaction.
	if err != nil {
		return fmt.Errorf("mysqlstore.Save: failed to begin transaction: %w", err)
	}

	// Prepare the record insertion statement
	query := `INSERT INTO records (aggregate_id, version, payload, timestamp) VALUES (?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("mysqlstore.Save: failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert each record
	for _, record := range records {
		if _, err := stmt.ExecContext(ctx, aggregateId, record.Version, record.Data); err != nil {
			tx.Rollback()
			return fmt.Errorf("mysqlstore.Save: failed to insert record: %w", err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mysqlstore.Save: failed to commit transaction: %w", err)
	}

	return nil
}

// Load retrieves the history for a given aggregate ID from the database.
// If no history exists for the specified ID, an empty History object is returned.
func (m *MySQLStore) Load(ctx context.Context, aggregateId string) (*historyv1.History, error) {
	query := `SELECT version, payload FROM records WHERE aggregate_id = ? ORDER BY version ASC`
	rows, err := m.db.QueryContext(ctx, query, aggregateId)
	if err != nil {
		return nil, fmt.Errorf("mysqlstore.Load: failed to query records: %w", err)
	}
	defer rows.Close()

	// Build history from the retrieved records
	history := &historyv1.History{}
	for rows.Next() {
		var record recordv1.Record
		if err := rows.Scan(&record.Version, &record.Data); err != nil {
			return nil, fmt.Errorf("mysqlstore.Load: failed to scan record: %w", err)
		}
		history.Records = append(history.Records, &record)
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysqlstore.Load: row iteration error: %w", err)
	}

	return history, nil
}
