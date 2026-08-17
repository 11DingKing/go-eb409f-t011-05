// Package cabin models the grid-forming energy-storage cabin zones
// (构网型储能舱区) and the work-lock discipline that guarantees at most one
// crew operates on a given cabin at any time.
package cabin

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrCabinNotFound = errors.New("cabin not found")
	ErrLockHeld      = errors.New("cabin work lock held by another team")
	ErrNotLockHolder = errors.New("caller does not hold the cabin work lock")
)

// Cabin is a single energy-storage cabin-zone ledger entry.
type Cabin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CapacityKWh float64   `json:"capacity_kwh"`
	Powered     bool      `json:"powered"`
	LockHolder  string    `json:"lock_holder"`
	LockPurpose string    `json:"lock_purpose"`
	LockedAt    time.Time `json:"locked_at"`
}

// Repository persists cabin ledger entries.
type Repository interface {
	Save(c Cabin) error
	Get(id string) (Cabin, error)
	All() ([]Cabin, error)
}

// LockManager enforces the "one team per cabin" work-lock rule. All access is
// serialised through a single mutex so concurrent drill, inspection and repair
// requests targeting the same cabin cannot interleave.
type LockManager struct {
	mu   sync.Mutex
	repo Repository
}

// NewLockManager returns a lock manager backed by the given repository.
func NewLockManager(repo Repository) *LockManager {
	return &LockManager{repo: repo}
}

// Acquire takes the work lock for cabinID on behalf of teamID. It is idempotent
// for the current holder: re-acquiring by the same team is a no-op.
func (m *LockManager) Acquire(cabinID, teamID, purpose string) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.repo.Get(cabinID)
	if err != nil {
		return Cabin{}, err
	}
	if c.LockHolder != "" && c.LockHolder != teamID {
		return c, fmt.Errorf("%w: cabin %s held by team %s", ErrLockHeld, cabinID, c.LockHolder)
	}
	if c.LockHolder == teamID {
		return c, nil
	}
	c.LockHolder = teamID
	c.LockPurpose = purpose
	c.LockedAt = time.Now()
	if err := m.repo.Save(c); err != nil {
		return Cabin{}, err
	}
	return c, nil
}

// Release frees the work lock, but only when teamID is the current holder.
func (m *LockManager) Release(cabinID, teamID string) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.repo.Get(cabinID)
	if err != nil {
		return Cabin{}, err
	}
	if c.LockHolder != teamID {
		return c, ErrNotLockHolder
	}
	c.LockHolder = ""
	c.LockPurpose = ""
	c.LockedAt = time.Time{}
	if err := m.repo.Save(c); err != nil {
		return Cabin{}, err
	}
	return c, nil
}

// SetPowered toggles the cabin power state. A power loss marks the cabin
// unpowered until an operator restarts cabin control.
func (m *LockManager) SetPowered(cabinID string, powered bool) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.repo.Get(cabinID)
	if err != nil {
		return Cabin{}, err
	}
	c.Powered = powered
	if err := m.repo.Save(c); err != nil {
		return Cabin{}, err
	}
	return c, nil
}

// Holder returns the team currently holding the lock and whether it is held.
func (m *LockManager) Holder(cabinID string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := m.repo.Get(cabinID)
	if err != nil {
		return "", false
	}
	return c.LockHolder, c.LockHolder != ""
}

// Get returns a snapshot of the cabin.
func (m *LockManager) Get(cabinID string) (Cabin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.repo.Get(cabinID)
}
