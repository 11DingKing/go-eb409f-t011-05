package scheduler_test

import (
	"context"
	"testing"
	"time"

	"microgrid-dispatch/internal/app"
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/workorder"
	"microgrid-dispatch/internal/scheduler"
	"microgrid-dispatch/internal/store"
)

func newService() *app.Service {
	st := store.New("")
	return app.NewService(st.CabinRepo(), st.PlanRepo(), st.DrillRepo(), st.OrderRepo(), st)
}

func seed(t *testing.T, s *app.Service) (string, string) {
	t.Helper()
	s.RegisterCabin("cabin-1", "一号储能舱", 1250)
	s.RegisterPlan(blackstart.Plan{
		ID:          "plan-1",
		TargetCabin: "cabin-1",
		PrimaryPath: []blackstart.Step{{ID: "p1"}, {ID: "p2"}},
		BackupPath:  []blackstart.Step{{ID: "b1"}},
	})
	return "cabin-1", "plan-1"
}

func TestSchedulerTickMarksOverdue(t *testing.T) {
	s := newService()
	cabinID, _ := seed(t, s)

	insp, _ := s.CreateInspection(cabinID, "check")
	insp, _ = s.CompleteInspection(insp.ID, true, "cell degraded")
	repair, _ := s.GetOrder(insp.RepairOrderID)

	sch := scheduler.New(s, time.Hour)
	// Before the SLA: nothing overdue.
	sch.Tick(time.Now())
	if got, _ := s.GetOrder(repair.ID); got.Status != workorder.StatusOpen {
		t.Fatalf("status = %s want open before deadline", got.Status)
	}
	// After the SLA: the scheduler flags it overdue.
	sch.Tick(time.Now().Add(25 * time.Hour))
	if got, _ := s.GetOrder(repair.ID); got.Status != workorder.StatusOverdue {
		t.Fatalf("status = %s want overdue after deadline", got.Status)
	}
}

func TestSchedulerReactivationRespectsWindow(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	insp, _ := s.CreateInspection(cabinID, "check")
	d, _ := s.ApplyDrill(planID, cabinID, "crew-A")
	s.ApproveDrill(d.ID, "sup")
	// Inspection is rescheduled because the window is occupied.
	if got, _ := s.GetOrder(insp.ID); got.Status != workorder.StatusRescheduled {
		t.Fatalf("status = %s want rescheduled", got.Status)
	}
	// The scheduler must not reactivate while the window is still occupied.
	if n := s.ReactivateRescheduledOrders(); n != 0 {
		t.Fatalf("expected 0 reactivations while occupied, got %d", n)
	}
	if got, _ := s.GetOrder(insp.ID); got.Status != workorder.StatusRescheduled {
		t.Fatalf("status = %s want rescheduled (scheduler should not reactivate)", got.Status)
	}
	// Once the drill completes the window frees and the inspection reactivates.
	s.StartDrill(d.ID)
	for {
		_, done, err := s.ExecuteStep(d.ID)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if done {
			break
		}
	}
	if got, _ := s.GetOrder(insp.ID); got.Status != workorder.StatusOpen {
		t.Fatalf("status = %s want open after window freed", got.Status)
	}
	// A subsequent sweep is a no-op.
	if n := s.ReactivateRescheduledOrders(); n != 0 {
		t.Fatalf("expected 0 reactivations in steady state, got %d", n)
	}
}

func TestSchedulerRunStopsOnContextCancel(t *testing.T) {
	s := newService()
	seed(t, s)
	sch := scheduler.New(s, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		sch.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after context cancel")
	}
}
