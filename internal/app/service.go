// Package app orchestrates the four Ejina microgrid dispatch workflows across
// the three ledgers (storage cabins, black-start plans, work-order pool). It is
// the only layer allowed to coordinate concurrency boundaries, idempotency and
// failure recovery between domains.
package app

import (
	"fmt"
	"sync"
	"sync/atomic"

	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
	"microgrid-dispatch/internal/domain/workorder"
)

// Snapshooter is satisfied by the store and lets the service durably persist
// state after mutating operations.
type Snapshooter interface {
	Snapshot() error
}

// Service wires the repositories, cabin lock manager and off-grid window manager
// into a single application surface.
type Service struct {
	cabins      cabin.Repository
	plans       blackstart.PlanRepository
	drills      blackstart.DrillRepository
	orders      workorder.Repository
	locks       *cabin.LockManager
	windows     *WindowManager
	snapshooter Snapshooter
	idSeq       atomic.Uint64
}

// NewService builds a service from the supplied repositories and snapshooter.
func NewService(
	cabins cabin.Repository,
	plans blackstart.PlanRepository,
	drills blackstart.DrillRepository,
	orders workorder.Repository,
	snap Snapshooter,
) *Service {
	return &Service{
		cabins:      cabins,
		plans:       plans,
		drills:      drills,
		orders:      orders,
		locks:       cabin.NewLockManager(cabins),
		windows:     NewWindowManager(),
		snapshooter: snap,
	}
}

func (s *Service) newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, s.idSeq.Add(1))
}

func (s *Service) persist() {
	if s.snapshooter != nil {
		_ = s.snapshooter.Snapshot()
	}
}

// WindowManager tracks off-grid window occupation per cabin. A cabin's off-grid
// window can host at most one drill at a time; while occupied, pending
// inspections and repairs are auto-rescheduled.
type WindowManager struct {
	mu       sync.Mutex
	occupied map[string]string // cabinID -> drillID
}

// NewWindowManager returns an empty window manager.
func NewWindowManager() *WindowManager {
	return &WindowManager{occupied: make(map[string]string)}
}

// Occupy reserves the off-grid window for a drill. It fails if another drill
// already holds the window for the same cabin.
func (w *WindowManager) Occupy(cabinID, drillID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cur, ok := w.occupied[cabinID]; ok && cur != drillID {
		return fmt.Errorf("off-grid window for cabin %s occupied by drill %s", cabinID, cur)
	}
	w.occupied[cabinID] = drillID
	return nil
}

// Release frees the window, but only if drillID is the current occupant.
func (w *WindowManager) Release(cabinID, drillID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if cur := w.occupied[cabinID]; cur == drillID {
		delete(w.occupied, cabinID)
	}
}

// IsOccupied reports the drill currently occupying a cabin's window.
func (w *WindowManager) IsOccupied(cabinID string) (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d, ok := w.occupied[cabinID]
	return d, ok
}

// RegisterCabin adds a powered cabin to the ledger.
func (s *Service) RegisterCabin(id, name string, capacityKWh float64) (cabin.Cabin, error) {
	c := cabin.Cabin{ID: id, Name: name, CapacityKWh: capacityKWh, Powered: true}
	if err := s.cabins.Save(c); err != nil {
		return cabin.Cabin{}, err
	}
	s.persist()
	return c, nil
}

// RegisterPlan adds a black-start plan to the library, generating an id when
// none is supplied.
func (s *Service) RegisterPlan(p blackstart.Plan) (blackstart.Plan, error) {
	if p.ID == "" {
		p.ID = s.newID("plan")
	}
	if err := s.plans.Save(p); err != nil {
		return blackstart.Plan{}, err
	}
	s.persist()
	return p, nil
}

// ListCabins returns the cabin ledger.
func (s *Service) ListCabins() ([]cabin.Cabin, error) { return s.cabins.All() }

// ListPlans returns the plan library.
func (s *Service) ListPlans() ([]blackstart.Plan, error) { return s.plans.All() }

// GetCabin returns a cabin by id.
func (s *Service) GetCabin(id string) (cabin.Cabin, error) { return s.cabins.Get(id) }

// GetDrill returns a drill by id.
func (s *Service) GetDrill(id string) (blackstart.Drill, error) { return s.drills.Get(id) }

// GetOrder returns a work order by id.
func (s *Service) GetOrder(id string) (workorder.WorkOrder, error) { return s.orders.Get(id) }

// IsWindowOccupied exposes the window occupancy for a cabin.
func (s *Service) IsWindowOccupied(cabinID string) (string, bool) {
	return s.windows.IsOccupied(cabinID)
}
