package memorystore

import (
	"context"
	"fmt"
	"sync"

	historyv1 "github.com/funinthecloud/protosource/history/v1"
	recordv1 "github.com/funinthecloud/protosource/record/v1"
)

// MemoryStore is an in-memory implementation for managing and storing histories.
// It uses a map to associate aggregate IDs with their corresponding histories,
// and a mutex to ensure thread-safe operations.
type MemoryStore struct {
	mu               sync.RWMutex                    // Read-Write mutex for protecting the maps.
	events           map[string]*historyv1.History   // Stores histories indexed by aggregate IDs.
	snapshotInterval int32                           // Configurable snapshot interval value.
}

// New initializes and returns a new instance of MemoryStore.
// Pass the aggregate's snapshot interval (0 to disable snapshots).
func New(snapshotInterval int32) *MemoryStore {
	return &MemoryStore{
		events:           make(map[string]*historyv1.History),
		snapshotInterval: snapshotInterval,
	}
}

// SnapshotInterval returns the snapshot interval (0 means disabled).
func (m *MemoryStore) SnapshotInterval() int32 {
	return m.snapshotInterval
}

// Save stores a list of records for a given aggregate ID.
// If the context is canceled or timed out, it returns an error.
// The function uses write locks for thread-safe access to the `events` map.
func (m *MemoryStore) Save(ctx context.Context, aggregateId string, records ...*recordv1.Record) error {
	// Validate the context before proceeding.
	if err := validateContext(ctx); err != nil {
		return fmt.Errorf("save failed: %w", err)
	}

	m.mu.Lock()         // Lock the mutex for exclusive write access.
	defer m.mu.Unlock() // Unlock when finished.

	// If no history exists for the given aggregateId, create one.
	history, exists := m.events[aggregateId]
	if !exists {
		history = &historyv1.History{}
		m.events[aggregateId] = history
	}

	// Add the provided records to the identified history.
	history.Records = append(history.Records, records...)
	return nil
}

// Load retrieves the history for a given aggregate ID.
// If the context is canceled or timed out, it returns an error.
// The function uses read locks for thread-safe read access to the `events` map.
func (m *MemoryStore) Load(ctx context.Context, aggregateId string) (*historyv1.History, error) {
	// Validate the context before proceeding.
	if err := validateContext(ctx); err != nil {
		return nil, fmt.Errorf("load failed: %w", err)
	}

	m.mu.RLock()         // Lock the mutex for shared read access.
	defer m.mu.RUnlock() // Unlock when finished.

	// Retrieve the history if it exists or return a new empty history.
	if history, exists := m.events[aggregateId]; exists {
		return history, nil
	}
	return &historyv1.History{}, nil
}

// validateContext checks if a context has been canceled or exceeded its deadline.
// It returns an error if the context is invalid.
func validateContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context error: %w", err)
	}
	return nil
}
