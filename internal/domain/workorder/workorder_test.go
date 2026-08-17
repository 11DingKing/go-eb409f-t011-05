package workorder_test

import (
	"errors"
	"testing"

	"microgrid-dispatch/internal/domain/workorder"
)

func newAnomaly() workorder.WorkOrder {
	return workorder.WorkOrder{ID: "a1", Type: workorder.TypeAnomaly, Status: workorder.StatusOpen}
}

func TestAnomalyIsolateBeforeRepair(t *testing.T) {
	a := newAnomaly()
	// Cannot generate a repair before isolation (checked at the service layer,
	// but the order tracks isolation state).
	if a.Isolated {
		t.Fatal("anomaly should start un-isolated")
	}
	if err := a.Isolate(); err != nil {
		t.Fatalf("isolate: %v", err)
	}
	if !a.Isolated || a.IsolatedAt.IsZero() {
		t.Fatalf("anomaly not isolated: %+v", a)
	}
	if a.Status != workorder.StatusInProgress {
		t.Fatalf("status = %s want in_progress", a.Status)
	}
	// Isolation is idempotent.
	if err := a.Isolate(); err != nil {
		t.Fatalf("re-isolate: %v", err)
	}
}

func TestRepairRetestAndConfirmClose(t *testing.T) {
	r := workorder.WorkOrder{ID: "r1", Type: workorder.TypeRepair, Status: workorder.StatusOpen}
	if err := r.Assign("crew-1"); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := r.Complete(); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := r.Retest(true); err != nil {
		t.Fatalf("retest: %v", err)
	}
	if !r.RetestPassed {
		t.Fatal("retest should pass")
	}
	if err := r.ConfirmClose("sup"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if r.Status != workorder.StatusClosed || r.ConfirmedBy != "sup" {
		t.Fatalf("after close: %+v", r)
	}
	// Confirming again is idempotent.
	if err := r.ConfirmClose("sup"); err != nil {
		t.Fatalf("second confirm: %v", err)
	}
}

func TestRepairConfirmWithoutRetestFails(t *testing.T) {
	r := workorder.WorkOrder{ID: "r2", Type: workorder.TypeRepair, Status: workorder.StatusOpen}
	r.Assign("crew")
	r.Complete()
	err := r.ConfirmClose("sup")
	if !errors.Is(err, workorder.ErrRetestRequired) {
		t.Fatalf("confirm err = %v want ErrRetestRequired", err)
	}
	// A failed retest also blocks closing.
	if err := r.Retest(false); err != nil {
		t.Fatalf("retest: %v", err)
	}
	if err := r.ConfirmClose("sup"); !errors.Is(err, workorder.ErrRetestRequired) {
		t.Fatalf("confirm after failed retest err = %v want ErrRetestRequired", err)
	}
}

func TestOrderRescheduleReactivate(t *testing.T) {
	o := workorder.WorkOrder{ID: "i1", Type: workorder.TypeInspection, Status: workorder.StatusOpen}
	if err := o.Reschedule("window occupied"); err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if o.Status != workorder.StatusRescheduled || o.RescheduleReason != "window occupied" {
		t.Fatalf("after reschedule: %+v", o)
	}
	// Cannot complete a rescheduled order directly.
	if err := o.Complete(); !errors.Is(err, workorder.ErrInvalidTransition) {
		t.Fatalf("complete err = %v want ErrInvalidTransition", err)
	}
	if err := o.Reactivate(); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if o.Status != workorder.StatusOpen {
		t.Fatalf("status = %s want open", o.Status)
	}
}

func TestRepairOverdueFromLateDispatch(t *testing.T) {
	r := workorder.WorkOrder{ID: "r3", Type: workorder.TypeRepair, Status: workorder.StatusOpen}
	if err := r.MarkOverdue(); err != nil {
		t.Fatalf("mark overdue: %v", err)
	}
	if r.Status != workorder.StatusOverdue {
		t.Fatalf("status = %s want overdue", r.Status)
	}
	// A late dispatch recovers the order back to assigned.
	if err := r.Assign("crew-late"); err != nil {
		t.Fatalf("assign overdue: %v", err)
	}
	if r.Status != workorder.StatusAssigned || r.TeamID != "crew-late" {
		t.Fatalf("after late assign: %+v", r)
	}
}
