package blackstart_test

import (
	"errors"
	"testing"

	"microgrid-dispatch/internal/domain/blackstart"
)

func samplePlan() blackstart.Plan {
	return blackstart.Plan{
		ID:          "plan-1",
		PrimaryPath: []blackstart.Step{{ID: "p1"}, {ID: "p2"}},
		BackupPath:  []blackstart.Step{{ID: "b1"}},
	}
}

func TestDrillApproveStartComplete(t *testing.T) {
	p := samplePlan()
	var d blackstart.Drill
	d.Status = blackstart.StatusRequested
	if err := d.Approve("sup-1"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if d.Status != blackstart.StatusApproved || d.SupervisorID != "sup-1" {
		t.Fatalf("after approve: %+v", d)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if d.Path != "primary" || d.CurrentStep != 0 {
		t.Fatalf("after start: path=%q step=%d", d.Path, d.CurrentStep)
	}
	// Execute both primary steps.
	for i := 0; i < len(p.PrimaryPath); i++ {
		st, ok := d.Current(p)
		if !ok {
			t.Fatalf("step %d: no current step", i)
		}
		done, err := d.Advance(p)
		if err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		_ = st
		if i == len(p.PrimaryPath)-1 && !done {
			t.Fatalf("expected path complete on step %d", i)
		}
	}
	if err := d.Complete(); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if d.Status != blackstart.StatusCompleted {
		t.Fatalf("status = %s want completed", d.Status)
	}
}

func TestDrillFallbackSwitchesToBackup(t *testing.T) {
	p := samplePlan()
	var d blackstart.Drill
	d.Status = blackstart.StatusRequested
	d.Approve("sup")
	d.Start()
	// Run one primary step, then main power recovers.
	if _, err := d.Advance(p); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if err := d.TriggerFallback(p, "rush close"); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if d.Status != blackstart.StatusFallback || d.Path != "backup" || d.CurrentStep != 0 {
		t.Fatalf("after fallback: %+v", d)
	}
	if d.FallbackReason != "rush close" {
		t.Fatalf("reason = %q", d.FallbackReason)
	}
	// Backup path should now be runnable.
	if _, ok := d.Current(p); !ok {
		t.Fatal("backup path has no current step")
	}
}

func TestDrillFallbackWithoutBackupPathFails(t *testing.T) {
	p := blackstart.Plan{ID: "p", PrimaryPath: []blackstart.Step{{ID: "p1"}}}
	var d blackstart.Drill
	d.Status = blackstart.StatusRequested
	d.Approve("sup")
	d.Start()
	err := d.TriggerFallback(p, "rush close")
	if !errors.Is(err, blackstart.ErrNoBackupPath) {
		t.Fatalf("err = %v want ErrNoBackupPath", err)
	}
}

func TestDrillCabinPowerLossAwaitRestartResume(t *testing.T) {
	var d blackstart.Drill
	d.Status = blackstart.StatusRequested
	d.Approve("sup")
	d.Start()
	if err := d.AwaitRestart("cabin blackout"); err != nil {
		t.Fatalf("await restart: %v", err)
	}
	if d.Status != blackstart.StatusAwaitingRestart || d.RestartReason != "cabin blackout" {
		t.Fatalf("after await: %+v", d)
	}
	if err := d.ResumeFromRestart(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if d.Status != blackstart.StatusExecuting {
		t.Fatalf("status = %s want executing", d.Status)
	}
}

func TestDrillCancelIdempotent(t *testing.T) {
	var d blackstart.Drill
	d.Status = blackstart.StatusRequested
	if err := d.Cancel(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if d.Status != blackstart.StatusCancelled {
		t.Fatalf("status = %s want cancelled", d.Status)
	}
	// Cancelling again is a no-op.
	if err := d.Cancel(); err != nil {
		t.Fatalf("second cancel: %v", err)
	}
	if err := d.Start(); !errors.Is(err, blackstart.ErrInvalidTransition) {
		t.Fatalf("start after cancel err = %v want ErrInvalidTransition", err)
	}
}
