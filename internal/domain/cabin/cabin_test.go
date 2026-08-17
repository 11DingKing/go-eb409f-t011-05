package cabin

import (
	"errors"
	"sync"
	"testing"
)

// memRepo is an in-memory cabin.Repository used to exercise the lock manager
// without touching the disk-backed store.
type memRepo struct {
	mu     sync.Mutex
	cabins map[string]Cabin
}

func newMemRepo() *memRepo { return &memRepo{cabins: make(map[string]Cabin)} }

func (r *memRepo) Save(c Cabin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cabins[c.ID] = c
	return nil
}

func (r *memRepo) Get(id string) (Cabin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cabins[id]
	if !ok {
		return Cabin{}, ErrCabinNotFound
	}
	return c, nil
}

func (r *memRepo) All() ([]Cabin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Cabin, 0, len(r.cabins))
	for _, c := range r.cabins {
		out = append(out, c)
	}
	return out, nil
}

func seededManager() (*LockManager, *memRepo) {
	r := newMemRepo()
	r.cabins["c1"] = Cabin{ID: "c1", Name: "cabin-1", Powered: true}
	return NewLockManager(r), r
}

func TestAcquireLockSuccess(t *testing.T) {
	m, _ := seededManager()
	c, err := m.Acquire("c1", "teamA", "drill")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if c.LockHolder != "teamA" {
		t.Fatalf("holder = %q want teamA", c.LockHolder)
	}
	if holder, held := m.Holder("c1"); !held || holder != "teamA" {
		t.Fatalf("Holder = %q %v want teamA true", holder, held)
	}
}

func TestAcquireLockBlockedByOtherTeam(t *testing.T) {
	m, _ := seededManager()
	if _, err := m.Acquire("c1", "teamA", "drill"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	_, err := m.Acquire("c1", "teamB", "repair")
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("second acquire err = %v want ErrLockHeld", err)
	}
}

func TestAcquireLockIdempotentSameTeam(t *testing.T) {
	m, _ := seededManager()
	if _, err := m.Acquire("c1", "teamA", "drill"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := m.Acquire("c1", "teamA", "drill"); err != nil {
		t.Fatalf("re-acquire should be idempotent, got %v", err)
	}
}

func TestReleaseLockOnlyByHolder(t *testing.T) {
	m, _ := seededManager()
	if _, err := m.Acquire("c1", "teamA", "drill"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := m.Release("c1", "teamB"); !errors.Is(err, ErrNotLockHolder) {
		t.Fatalf("release by non-holder err = %v want ErrNotLockHolder", err)
	}
	if _, err := m.Release("c1", "teamA"); err != nil {
		t.Fatalf("release by holder: %v", err)
	}
	if _, held := m.Holder("c1"); held {
		t.Fatal("lock should be free after release")
	}
}

func TestSetPoweredTogglesCabinState(t *testing.T) {
	m, _ := seededManager()
	if _, err := m.SetPowered("c1", false); err != nil {
		t.Fatalf("set powered false: %v", err)
	}
	c, _ := m.Get("c1")
	if c.Powered {
		t.Fatal("cabin should be unpowered")
	}
	if _, err := m.SetPowered("c1", true); err != nil {
		t.Fatalf("set powered true: %v", err)
	}
	c, _ = m.Get("c1")
	if !c.Powered {
		t.Fatal("cabin should be powered after restart")
	}
}
