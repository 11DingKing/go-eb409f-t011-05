package store

import (
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
	"microgrid-dispatch/internal/domain/workorder"
)

// CabinRepo adapts the store to cabin.Repository.
type CabinRepo struct{ S *Store }

func (r *CabinRepo) Save(c cabin.Cabin) error {
	r.S.mu.Lock()
	defer r.S.mu.Unlock()
	for i := range r.S.data.Cabins {
		if r.S.data.Cabins[i].ID == c.ID {
			r.S.data.Cabins[i] = c
			return nil
		}
	}
	r.S.data.Cabins = append(r.S.data.Cabins, c)
	return nil
}

func (r *CabinRepo) Get(id string) (cabin.Cabin, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	for _, c := range r.S.data.Cabins {
		if c.ID == id {
			return c, nil
		}
	}
	return cabin.Cabin{}, cabin.ErrCabinNotFound
}

func (r *CabinRepo) All() ([]cabin.Cabin, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	out := make([]cabin.Cabin, len(r.S.data.Cabins))
	copy(out, r.S.data.Cabins)
	return out, nil
}

// PlanRepo adapts the store to blackstart.PlanRepository.
type PlanRepo struct{ S *Store }

func (r *PlanRepo) Save(p blackstart.Plan) error {
	r.S.mu.Lock()
	defer r.S.mu.Unlock()
	for i := range r.S.data.Plans {
		if r.S.data.Plans[i].ID == p.ID {
			r.S.data.Plans[i] = p
			return nil
		}
	}
	r.S.data.Plans = append(r.S.data.Plans, p)
	return nil
}

func (r *PlanRepo) Get(id string) (blackstart.Plan, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	for _, p := range r.S.data.Plans {
		if p.ID == id {
			return p, nil
		}
	}
	return blackstart.Plan{}, blackstart.ErrPlanNotFound
}

func (r *PlanRepo) All() ([]blackstart.Plan, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	out := make([]blackstart.Plan, len(r.S.data.Plans))
	copy(out, r.S.data.Plans)
	return out, nil
}

// DrillRepo adapts the store to blackstart.DrillRepository.
type DrillRepo struct{ S *Store }

func (r *DrillRepo) Save(d blackstart.Drill) error {
	r.S.mu.Lock()
	defer r.S.mu.Unlock()
	for i := range r.S.data.Drills {
		if r.S.data.Drills[i].ID == d.ID {
			r.S.data.Drills[i] = d
			return nil
		}
	}
	r.S.data.Drills = append(r.S.data.Drills, d)
	return nil
}

func (r *DrillRepo) Get(id string) (blackstart.Drill, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	for _, d := range r.S.data.Drills {
		if d.ID == id {
			return d, nil
		}
	}
	return blackstart.Drill{}, blackstart.ErrDrillNotFound
}

func (r *DrillRepo) All() ([]blackstart.Drill, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	out := make([]blackstart.Drill, len(r.S.data.Drills))
	copy(out, r.S.data.Drills)
	return out, nil
}

// OrderRepo adapts the store to workorder.Repository.
type OrderRepo struct{ S *Store }

func (r *OrderRepo) Save(o workorder.WorkOrder) error {
	r.S.mu.Lock()
	defer r.S.mu.Unlock()
	for i := range r.S.data.Orders {
		if r.S.data.Orders[i].ID == o.ID {
			r.S.data.Orders[i] = o
			return nil
		}
	}
	r.S.data.Orders = append(r.S.data.Orders, o)
	return nil
}

func (r *OrderRepo) Get(id string) (workorder.WorkOrder, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	for _, o := range r.S.data.Orders {
		if o.ID == id {
			return o, nil
		}
	}
	return workorder.WorkOrder{}, workorder.ErrOrderNotFound
}

func (r *OrderRepo) All() ([]workorder.WorkOrder, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	out := make([]workorder.WorkOrder, len(r.S.data.Orders))
	copy(out, r.S.data.Orders)
	return out, nil
}

func (r *OrderRepo) ByCabin(cabinID string) ([]workorder.WorkOrder, error) {
	r.S.mu.RLock()
	defer r.S.mu.RUnlock()
	var out []workorder.WorkOrder
	for _, o := range r.S.data.Orders {
		if o.CabinID == cabinID {
			out = append(out, o)
		}
	}
	return out, nil
}
