// Package workorder models the inspection/anomaly/repair work-order pool and
// the state transitions that connect the four dispatch workflows.
package workorder

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrOrderNotFound     = errors.New("work order not found")
	ErrInvalidTransition = errors.New("invalid work-order state transition")
	ErrNotIsolated       = errors.New("anomaly not yet electrically isolated")
	ErrRetestRequired    = errors.New("synchronisation retest not passed")
)

// Type enumerates work-order kinds.
type Type string

const (
	TypeInspection Type = "inspection"
	TypeAnomaly    Type = "anomaly"
	TypeRepair     Type = "repair"
)

// Status enumerates work-order lifecycle states.
type Status string

const (
	StatusOpen        Status = "open"
	StatusAssigned    Status = "assigned"
	StatusInProgress  Status = "in_progress"
	StatusCompleted   Status = "completed"
	StatusClosed      Status = "closed"
	StatusRescheduled Status = "rescheduled"
	StatusOverdue     Status = "overdue"
)

// WorkOrder is an entry in the inspection/anomaly/repair work-order pool.
type WorkOrder struct {
	ID               string    `json:"id"`
	Type             Type      `json:"type"`
	CabinID          string    `json:"cabin_id"`
	Status           Status    `json:"status"`
	TeamID           string    `json:"team_id"`
	Description      string    `json:"description"`
	CreatedAt        time.Time `json:"created_at"`
	DispatchDeadline time.Time `json:"dispatch_deadline"`
	Isolated         bool      `json:"isolated"`
	IsolatedAt       time.Time `json:"isolated_at"`
	RetestPassed     bool      `json:"retest_passed"`
	RetestedAt       time.Time `json:"retested_at"`
	ConfirmedBy      string    `json:"confirmed_by"`
	ClosedAt         time.Time `json:"closed_at"`
	SourceOrderID    string    `json:"source_order_id"`
	RepairOrderID    string    `json:"repair_order_id"`
	RescheduleReason string    `json:"reschedule_reason"`
	DefectFound      bool      `json:"defect_found"`
}

// Assign dispatches the order to a crew. Orders may be (re-)assigned from open,
// rescheduled or overdue states so a late dispatch can still recover an order.
func (o *WorkOrder) Assign(teamID string) error {
	if o.Status != StatusOpen && o.Status != StatusRescheduled && o.Status != StatusOverdue {
		return fmt.Errorf("%w: cannot assign from %s", ErrInvalidTransition, o.Status)
	}
	o.Status = StatusAssigned
	o.TeamID = teamID
	return nil
}

// Start moves an assigned order into progress.
func (o *WorkOrder) Start() error {
	if o.Status != StatusAssigned {
		return fmt.Errorf("%w: cannot start from %s", ErrInvalidTransition, o.Status)
	}
	o.Status = StatusInProgress
	return nil
}

// Complete marks an in-progress (or assigned) order finished.
func (o *WorkOrder) Complete() error {
	if o.Status != StatusAssigned && o.Status != StatusInProgress {
		// Daily inspections are self-contained tasks and may be completed
		// directly from open; repairs must be dispatched first.
		if !(o.Type == TypeInspection && o.Status == StatusOpen) {
			return fmt.Errorf("%w: cannot complete from %s", ErrInvalidTransition, o.Status)
		}
	}
	o.Status = StatusCompleted
	return nil
}

// Isolate records electrical isolation for an anomaly. Idempotent.
func (o *WorkOrder) Isolate() error {
	if o.Type != TypeAnomaly {
		return fmt.Errorf("%w: isolation only applies to anomalies", ErrInvalidTransition)
	}
	if o.Status == StatusClosed {
		return fmt.Errorf("%w: cannot isolate a closed order", ErrInvalidTransition)
	}
	o.Isolated = true
	o.IsolatedAt = time.Now()
	if o.Status == StatusOpen {
		o.Status = StatusInProgress
	}
	return nil
}

// Retest records the grid-synchronisation retest result for a completed repair.
func (o *WorkOrder) Retest(passed bool) error {
	if o.Type != TypeRepair {
		return fmt.Errorf("%w: retest only applies to repairs", ErrInvalidTransition)
	}
	if o.Status != StatusCompleted {
		return fmt.Errorf("%w: retest requires a completed repair, got %s", ErrInvalidTransition, o.Status)
	}
	o.RetestPassed = passed
	o.RetestedAt = time.Now()
	return nil
}

// ConfirmClose closes a repair after a passing retest and supervisor confirm.
func (o *WorkOrder) ConfirmClose(supervisorID string) error {
	if o.Type != TypeRepair {
		return fmt.Errorf("%w: confirm only applies to repairs", ErrInvalidTransition)
	}
	if o.Status == StatusClosed {
		return nil
	}
	if o.Status != StatusCompleted {
		return fmt.Errorf("%w: confirm requires a completed repair, got %s", ErrInvalidTransition, o.Status)
	}
	if !o.RetestPassed {
		return ErrRetestRequired
	}
	o.ConfirmedBy = supervisorID
	o.Status = StatusClosed
	o.ClosedAt = time.Now()
	return nil
}

// Reschedule defers a pending order because the off-grid window is occupied.
func (o *WorkOrder) Reschedule(reason string) error {
	if o.Status != StatusOpen && o.Status != StatusAssigned {
		return fmt.Errorf("%w: cannot reschedule from %s", ErrInvalidTransition, o.Status)
	}
	o.Status = StatusRescheduled
	o.RescheduleReason = reason
	return nil
}

// Reactivate moves a rescheduled order back to open.
func (o *WorkOrder) Reactivate() error {
	if o.Status != StatusRescheduled {
		return fmt.Errorf("%w: cannot reactivate from %s", ErrInvalidTransition, o.Status)
	}
	o.Status = StatusOpen
	o.RescheduleReason = ""
	return nil
}

// MarkOverdue flags a repair that missed its dispatch deadline.
func (o *WorkOrder) MarkOverdue() error {
	if o.Type != TypeRepair {
		return fmt.Errorf("%w: overdue only applies to repairs", ErrInvalidTransition)
	}
	if o.Status != StatusOpen && o.Status != StatusRescheduled {
		return fmt.Errorf("%w: cannot mark overdue from %s", ErrInvalidTransition, o.Status)
	}
	o.Status = StatusOverdue
	return nil
}

// Repository persists work orders.
type Repository interface {
	Save(o WorkOrder) error
	Get(id string) (WorkOrder, error)
	All() ([]WorkOrder, error)
	ByCabin(cabinID string) ([]WorkOrder, error)
}
