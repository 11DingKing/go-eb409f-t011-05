package app_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"microgrid-dispatch/internal/app"
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
	"microgrid-dispatch/internal/domain/workorder"
	"microgrid-dispatch/internal/store"
)

func newService() *app.Service {
	st := store.New("")
	return app.NewService(st.CabinRepo(), st.PlanRepo(), st.DrillRepo(), st.OrderRepo(), st)
}

func seed(t *testing.T, s *app.Service) (string, string) {
	t.Helper()
	if _, err := s.RegisterCabin("cabin-1", "一号储能舱", 1250); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterCabin("cabin-2", "二号储能舱", 2000); err != nil {
		t.Fatal(err)
	}
	_, err := s.RegisterPlan(blackstart.Plan{
		ID:          "plan-1",
		Name:        "test plan",
		TargetCabin: "cabin-1",
		PrimaryPath: []blackstart.Step{{ID: "p1", Name: "离网确认"}, {ID: "p2", Name: "同期并网"}},
		BackupPath:  []blackstart.Step{{ID: "b1", Name: "备用电源同期"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "cabin-1", "plan-1"
}

func TestFullDrillFlowWithLockAndWindow(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	d, err := s.ApplyDrill(planID, cabinID, "crew-A")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if d.Status != blackstart.StatusRequested {
		t.Fatalf("status = %s want requested", d.Status)
	}
	if _, err := s.ApproveDrill(d.ID, "sup"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// The cabin work lock and off-grid window should now be held.
	if holder, held := s.IsWindowOccupied(cabinID); !held || holder != d.ID {
		t.Fatalf("window not occupied by drill: %q %v", holder, held)
	}
	c, _ := s.GetCabin(cabinID)
	if c.LockHolder != "crew-A" {
		t.Fatalf("lock holder = %q want crew-A", c.LockHolder)
	}
	if _, err := s.StartDrill(d.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	for {
		_, done, err := s.ExecuteStep(d.ID)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if done {
			break
		}
	}
	got, _ := s.GetDrill(d.ID)
	if got.Status != blackstart.StatusCompleted {
		t.Fatalf("status = %s want completed", got.Status)
	}
	// Completion must release the lock and window.
	if _, held := s.IsWindowOccupied(cabinID); held {
		t.Fatal("window should be free after completion")
	}
	c, _ = s.GetCabin(cabinID)
	if c.LockHolder != "" {
		t.Fatalf("lock should be released, holder=%q", c.LockHolder)
	}
}

func TestDrillReschedulesInspectionAndRepair(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	// A pending inspection exists before the drill is approved.
	insp, err := s.CreateInspection(cabinID, "daily check")
	if err != nil {
		t.Fatal(err)
	}
	if insp.Status != workorder.StatusOpen {
		t.Fatalf("inspection status = %s want open", insp.Status)
	}
	d, _ := s.ApplyDrill(planID, cabinID, "crew-A")
	if _, err := s.ApproveDrill(d.ID, "sup"); err != nil {
		t.Fatal(err)
	}
	// The pending inspection must have been auto-rescheduled.
	insp, _ = s.GetOrder(insp.ID)
	if insp.Status != workorder.StatusRescheduled {
		t.Fatalf("inspection status = %s want rescheduled", insp.Status)
	}
	// A new inspection created while the window is occupied is also rescheduled.
	insp2, _ := s.CreateInspection(cabinID, "second check")
	if insp2.Status != workorder.StatusRescheduled {
		t.Fatalf("inspection2 status = %s want rescheduled", insp2.Status)
	}
	// Completing the drill reactivates the rescheduled orders.
	if _, err := s.StartDrill(d.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	for {
		_, done, err := s.ExecuteStep(d.ID)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if done {
			break
		}
	}
	insp, _ = s.GetOrder(insp.ID)
	if insp.Status != workorder.StatusOpen {
		t.Fatalf("inspection status = %s want open after reactivation", insp.Status)
	}
	insp2, _ = s.GetOrder(insp2.ID)
	if insp2.Status != workorder.StatusOpen {
		t.Fatalf("inspection2 status = %s want open after reactivation", insp2.Status)
	}
}

func TestDefectDispatchDeadlineOverdue(t *testing.T) {
	s := newService()
	cabinID, _ := seed(t, s)

	insp, _ := s.CreateInspection(cabinID, "daily check")
	insp, err := s.CompleteInspection(insp.ID, true, "battery cell degraded")
	if err != nil {
		t.Fatalf("complete inspection: %v", err)
	}
	repair, _ := s.GetOrder(insp.RepairOrderID)
	if repair.Status != workorder.StatusOpen {
		t.Fatalf("repair status = %s want open", repair.Status)
	}
	if repair.DispatchDeadline.IsZero() {
		t.Fatal("repair should have a 24h dispatch deadline")
	}
	// Before the deadline: nothing is overdue.
	if n := s.CheckDefectDispatchDeadlines(time.Now()); n != 0 {
		t.Fatalf("expected 0 overdue, got %d", n)
	}
	// After the deadline: the repair becomes overdue.
	if n := s.CheckDefectDispatchDeadlines(time.Now().Add(25 * time.Hour)); n != 1 {
		t.Fatalf("expected 1 overdue, got %d", n)
	}
	repair, _ = s.GetOrder(repair.ID)
	if repair.Status != workorder.StatusOverdue {
		t.Fatalf("repair status = %s want overdue", repair.Status)
	}
	// A late dispatch recovers it.
	if _, err := s.DispatchRepair(repair.ID, "crew-late"); err != nil {
		t.Fatalf("late dispatch: %v", err)
	}
}

func TestConcurrentLockAcquisition(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	d1, _ := s.ApplyDrill(planID, cabinID, "crew-A")
	d2, _ := s.ApplyDrill(planID, cabinID, "crew-B")

	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok, fail int
	approve := func(id string) {
		defer wg.Done()
		_, err := s.ApproveDrill(id, "sup")
		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	wg.Add(2)
	go approve(d1.ID)
	go approve(d2.ID)
	wg.Wait()

	if ok != 1 || fail != 1 {
		t.Fatalf("expected exactly one approval success, got ok=%d fail=%d", ok, fail)
	}
	// The losing drill must still be in the requested state (rollback).
	a, _ := s.GetDrill(d1.ID)
	b, _ := s.GetDrill(d2.ID)
	approved := 0
	for _, st := range []blackstart.Status{a.Status, b.Status} {
		if st == blackstart.StatusApproved {
			approved++
		}
	}
	if approved != 1 {
		t.Fatalf("expected 1 approved drill, got %d (%s, %s)", approved, a.Status, b.Status)
	}
}

func TestPowerRecoveryFallbackPath(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	d, _ := s.ApplyDrill(planID, cabinID, "crew-A")
	s.ApproveDrill(d.ID, "sup")
	s.StartDrill(d.ID)
	// Execute one primary step then simulate main-power recovery.
	if _, _, err := s.ExecuteStep(d.ID); err != nil {
		t.Fatalf("execute primary: %v", err)
	}
	if _, err := s.HandlePowerRecovery(d.ID, "rush close"); err != nil {
		t.Fatalf("power recovery: %v", err)
	}
	d, _ = s.GetDrill(d.ID)
	if d.Status != blackstart.StatusFallback || d.Path != "backup" {
		t.Fatalf("after fallback: %+v", d)
	}
	// Run the backup path to completion.
	for {
		_, done, err := s.ExecuteStep(d.ID)
		if err != nil {
			t.Fatalf("execute backup: %v", err)
		}
		if done {
			break
		}
	}
	d, _ = s.GetDrill(d.ID)
	if d.Status != blackstart.StatusCompleted {
		t.Fatalf("status = %s want completed", d.Status)
	}
	if _, held := s.IsWindowOccupied(cabinID); held {
		t.Fatal("window should be free after fallback completion")
	}
}

func TestCabinPowerLossRestartAndResume(t *testing.T) {
	s := newService()
	cabinID, planID := seed(t, s)

	d, _ := s.ApplyDrill(planID, cabinID, "crew-A")
	s.ApproveDrill(d.ID, "sup")
	s.StartDrill(d.ID)
	if _, _, err := s.ExecuteStep(d.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := s.HandleCabinPowerLoss(d.ID, "cabin blackout"); err != nil {
		t.Fatalf("power loss: %v", err)
	}
	d, _ = s.GetDrill(d.ID)
	if d.Status != blackstart.StatusAwaitingRestart {
		t.Fatalf("status = %s want awaiting_restart", d.Status)
	}
	c, _ := s.GetCabin(cabinID)
	if c.Powered {
		t.Fatal("cabin should be unpowered after loss")
	}
	// Restart cabin control and resume; the remaining step must still run.
	if _, err := s.RestartCabinControl(cabinID); err != nil {
		t.Fatalf("restart cabin: %v", err)
	}
	if _, err := s.ResumeDrill(d.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	d, _ = s.GetDrill(d.ID)
	if d.Status != blackstart.StatusExecuting {
		t.Fatalf("status = %s want executing", d.Status)
	}
	for {
		_, done, err := s.ExecuteStep(d.ID)
		if err != nil {
			t.Fatalf("execute after resume: %v", err)
		}
		if done {
			break
		}
	}
	d, _ = s.GetDrill(d.ID)
	if d.Status != blackstart.StatusCompleted {
		t.Fatalf("status = %s want completed", d.Status)
	}
	// Sanity: a missing cabin returns a typed error.
	_, err := s.GetCabin("nope")
	if !errors.Is(err, cabin.ErrCabinNotFound) {
		t.Fatalf("err = %v want ErrCabinNotFound", err)
	}
}
