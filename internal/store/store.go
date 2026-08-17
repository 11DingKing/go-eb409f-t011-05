// Package store provides a self-contained, file-backed in-memory persistence
// layer. State is held in memory for speed and atomically snapshotted to a JSON
// file for durability across restarts. The package exposes repository adapters
// that satisfy the domain repository interfaces.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
	"microgrid-dispatch/internal/domain/workorder"
)

// ErrNotFound is a generic not-found sentinel kept for callers that do not
// depend on a specific domain error type.
var ErrNotFound = errors.New("entity not found")

type snapshot struct {
	Cabins []cabin.Cabin
	Plans  []blackstart.Plan
	Drills []blackstart.Drill
	Orders []workorder.WorkOrder
}

// Store is the in-memory persistence root. All repository adapters share a
// single read/write mutex so cross-ledger snapshots are consistent.
type Store struct {
	mu   sync.RWMutex
	path string
	data snapshot
}

// New returns a store that snapshots to path. An empty path disables disk
// persistence (useful for tests).
func New(path string) *Store {
	return &Store{path: path}
}

// Load restores state from the snapshot file, if present.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.data)
}

// Snapshot atomically writes the in-memory state to disk.
func (s *Store) Snapshot() error {
	s.mu.RLock()
	data := s.data
	s.mu.RUnlock()
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// CabinRepo returns the cabin ledger repository adapter.
func (s *Store) CabinRepo() *CabinRepo { return &CabinRepo{S: s} }

// PlanRepo returns the black-start plan repository adapter.
func (s *Store) PlanRepo() *PlanRepo { return &PlanRepo{S: s} }

// DrillRepo returns the black-start drill repository adapter.
func (s *Store) DrillRepo() *DrillRepo { return &DrillRepo{S: s} }

// OrderRepo returns the work-order repository adapter.
func (s *Store) OrderRepo() *OrderRepo { return &OrderRepo{S: s} }
