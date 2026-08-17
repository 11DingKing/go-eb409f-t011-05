package app

import (
	"fmt"
	"time"

	"microgrid-dispatch/internal/domain/workorder"
)

// defectDispatchSLA is the maximum time allowed to dispatch a repair raised
// from an inspection defect.
const defectDispatchSLA = 24 * time.Hour

// CreateInspection opens a daily inspection order for a cabin. If the cabin's
// off-grid window is occupied by a drill the inspection is auto-rescheduled.
func (s *Service) CreateInspection(cabinID, description string) (workorder.WorkOrder, error) {
	if _, err := s.cabins.Get(cabinID); err != nil {
		return workorder.WorkOrder{}, err
	}
	o := workorder.WorkOrder{
		ID:          s.newID("insp"),
		Type:        workorder.TypeInspection,
		CabinID:     cabinID,
		Status:      workorder.StatusOpen,
		Description: description,
		CreatedAt:   time.Now(),
	}
	if _, occ := s.windows.IsOccupied(cabinID); occ {
		if err := o.Reschedule("off-grid window occupied"); err != nil {
			return workorder.WorkOrder{}, err
		}
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// CompleteInspection finishes an inspection. When a defect is found a repair
// order is created with a 24-hour dispatch deadline and linked back to the
// inspection.
func (s *Service) CompleteInspection(orderID string, defectFound bool, defectDesc string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeInspection {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not an inspection", orderID)
	}
	if err := o.Complete(); err != nil {
		return workorder.WorkOrder{}, err
	}
	o.DefectFound = defectFound
	if defectFound {
		repair := workorder.WorkOrder{
			ID:               s.newID("repair"),
			Type:             workorder.TypeRepair,
			CabinID:          o.CabinID,
			Status:           workorder.StatusOpen,
			Description:      defectDesc,
			CreatedAt:        time.Now(),
			DispatchDeadline: time.Now().Add(defectDispatchSLA),
			SourceOrderID:    o.ID,
		}
		if _, occ := s.windows.IsOccupied(o.CabinID); occ {
			if err := repair.Reschedule("off-grid window occupied"); err != nil {
				return workorder.WorkOrder{}, err
			}
		}
		o.RepairOrderID = repair.ID
		if err := s.orders.Save(repair); err != nil {
			return workorder.WorkOrder{}, err
		}
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// ReportAnomaly opens an equipment anomaly report. Anomalies are never
// rescheduled: electrical isolation must proceed regardless of window state.
func (s *Service) ReportAnomaly(cabinID, description string) (workorder.WorkOrder, error) {
	if _, err := s.cabins.Get(cabinID); err != nil {
		return workorder.WorkOrder{}, err
	}
	o := workorder.WorkOrder{
		ID:          s.newID("anom"),
		Type:        workorder.TypeAnomaly,
		CabinID:     cabinID,
		Status:      workorder.StatusOpen,
		Description: description,
		CreatedAt:   time.Now(),
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// IsolateAnomaly records electrical isolation for an anomaly. Isolation must be
// completed before a repair order can be generated.
func (s *Service) IsolateAnomaly(orderID string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := o.Isolate(); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// GenerateRepairFromAnomaly creates a repair order from an isolated anomaly.
// It is idempotent: calling it twice returns the same repair order.
func (s *Service) GenerateRepairFromAnomaly(orderID string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeAnomaly {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not an anomaly", orderID)
	}
	if !o.Isolated {
		return workorder.WorkOrder{}, workorder.ErrNotIsolated
	}
	if o.RepairOrderID != "" {
		return s.orders.Get(o.RepairOrderID)
	}
	repair := workorder.WorkOrder{
		ID:            s.newID("repair"),
		Type:          workorder.TypeRepair,
		CabinID:       o.CabinID,
		Status:        workorder.StatusOpen,
		Description:   "repair raised from anomaly " + o.ID,
		CreatedAt:     time.Now(),
		SourceOrderID: o.ID,
	}
	if _, occ := s.windows.IsOccupied(o.CabinID); occ {
		if err := repair.Reschedule("off-grid window occupied"); err != nil {
			return workorder.WorkOrder{}, err
		}
	}
	o.RepairOrderID = repair.ID
	if err := s.orders.Save(repair); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return repair, nil
}

// DispatchRepair assigns a repair to a crew. Late dispatch from an overdue
// state is permitted so an order can still recover.
func (s *Service) DispatchRepair(orderID, teamID string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeRepair {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not a repair", orderID)
	}
	if err := o.Assign(teamID); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// CompleteRepair marks a repair finished and ready for retest.
func (s *Service) CompleteRepair(orderID string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeRepair {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not a repair", orderID)
	}
	if err := o.Complete(); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// RetestRepair records the grid-synchronisation retest result for a completed
// repair.
func (s *Service) RetestRepair(orderID string, passed bool) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeRepair {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not a repair", orderID)
	}
	if err := o.Retest(passed); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// ConfirmClose closes a repair after a passing retest and supervisor confirm.
func (s *Service) ConfirmClose(orderID, supervisorID string) (workorder.WorkOrder, error) {
	o, err := s.orders.Get(orderID)
	if err != nil {
		return workorder.WorkOrder{}, err
	}
	if o.Type != workorder.TypeRepair {
		return workorder.WorkOrder{}, fmt.Errorf("order %s is not a repair", orderID)
	}
	if err := o.ConfirmClose(supervisorID); err != nil {
		return workorder.WorkOrder{}, err
	}
	if err := s.orders.Save(o); err != nil {
		return workorder.WorkOrder{}, err
	}
	s.persist()
	return o, nil
}

// reschedulePendingOrders defers all open/assigned inspection and repair orders
// for a cabin whose off-grid window has just been occupied.
func (s *Service) reschedulePendingOrders(cabinID, reason string) int {
	orders, err := s.orders.ByCabin(cabinID)
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range orders {
		if o.Type != workorder.TypeInspection && o.Type != workorder.TypeRepair {
			continue
		}
		if o.Status != workorder.StatusOpen && o.Status != workorder.StatusAssigned {
			continue
		}
		if err := o.Reschedule(reason); err == nil {
			s.orders.Save(o)
			n++
		}
	}
	if n > 0 {
		s.persist()
	}
	return n
}

// reactivateOrders moves rescheduled orders for a cabin back to open once the
// off-grid window is free again.
func (s *Service) reactivateOrders(cabinID string) int {
	orders, err := s.orders.ByCabin(cabinID)
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range orders {
		if o.Status != workorder.StatusRescheduled {
			continue
		}
		if _, occ := s.windows.IsOccupied(o.CabinID); occ {
			continue
		}
		if err := o.Reactivate(); err == nil {
			s.orders.Save(o)
			n++
		}
	}
	if n > 0 {
		s.persist()
	}
	return n
}

// CheckDefectDispatchDeadlines flags inspection-derived repairs that have not
// been dispatched within their 24-hour SLA. Returns the number marked overdue.
func (s *Service) CheckDefectDispatchDeadlines(now time.Time) int {
	orders, err := s.orders.All()
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range orders {
		if o.Type != workorder.TypeRepair || o.SourceOrderID == "" {
			continue
		}
		if o.DispatchDeadline.IsZero() {
			continue
		}
		if o.Status != workorder.StatusOpen && o.Status != workorder.StatusRescheduled {
			continue
		}
		if now.After(o.DispatchDeadline) {
			if err := o.MarkOverdue(); err == nil {
				s.orders.Save(o)
				n++
			}
		}
	}
	if n > 0 {
		s.persist()
	}
	return n
}

// ReactivateRescheduledOrders reactivates any rescheduled order whose cabin
// window is no longer occupied. Returns the number reactivated.
func (s *Service) ReactivateRescheduledOrders() int {
	orders, err := s.orders.All()
	if err != nil {
		return 0
	}
	n := 0
	for _, o := range orders {
		if o.Status != workorder.StatusRescheduled {
			continue
		}
		if _, occ := s.windows.IsOccupied(o.CabinID); occ {
			continue
		}
		if err := o.Reactivate(); err == nil {
			s.orders.Save(o)
			n++
		}
	}
	if n > 0 {
		s.persist()
	}
	return n
}
