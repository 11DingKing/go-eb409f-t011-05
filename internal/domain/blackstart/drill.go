package blackstart

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrDrillNotFound     = errors.New("drill not found")
	ErrInvalidTransition = errors.New("invalid drill state transition")
	ErrNoBackupPath      = errors.New("plan has no backup path")
)

// Status enumerates the lifecycle states of a black-start drill.
type Status string

const (
	StatusRequested       Status = "requested"
	StatusApproved        Status = "approved"
	StatusExecuting       Status = "executing"
	StatusFallback        Status = "fallback"
	StatusAwaitingRestart Status = "awaiting_restart"
	StatusCompleted       Status = "completed"
	StatusCancelled       Status = "cancelled"
)

// Drill is a black-start drill execution ledger entry.
type Drill struct {
	ID             string    `json:"id"`
	PlanID         string    `json:"plan_id"`
	CabinID        string    `json:"cabin_id"`
	TeamID         string    `json:"team_id"`
	SupervisorID   string    `json:"supervisor_id"`
	Status         Status    `json:"status"`
	Path           string    `json:"path"`
	CurrentStep    int       `json:"current_step"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
	FallbackReason string    `json:"fallback_reason"`
	RestartReason  string    `json:"restart_reason"`
}

// Approve moves a requested drill to approved. The cabin work lock is acquired
// by the application layer after this transition succeeds.
func (d *Drill) Approve(supervisorID string) error {
	if d.Status != StatusRequested {
		return fmt.Errorf("%w: cannot approve from %s", ErrInvalidTransition, d.Status)
	}
	d.Status = StatusApproved
	d.SupervisorID = supervisorID
	return nil
}

// Start begins execution along the primary path.
func (d *Drill) Start() error {
	if d.Status != StatusApproved {
		return fmt.Errorf("%w: cannot start from %s", ErrInvalidTransition, d.Status)
	}
	d.Status = StatusExecuting
	d.Path = "primary"
	d.CurrentStep = 0
	d.StartedAt = time.Now()
	return nil
}

// Current returns the step at the current index for the active path. The second
// result is false when the path is exhausted.
func (d *Drill) Current(plan Plan) (Step, bool) {
	steps := plan.Path(d.Path)
	if d.CurrentStep < 0 || d.CurrentStep >= len(steps) {
		return Step{}, false
	}
	return steps[d.CurrentStep], true
}

// Advance moves the cursor to the next step and reports whether the active path
// is now exhausted.
func (d *Drill) Advance(plan Plan) (bool, error) {
	steps := plan.Path(d.Path)
	if d.CurrentStep < 0 || d.CurrentStep >= len(steps) {
		return true, nil
	}
	d.CurrentStep++
	return d.CurrentStep >= len(steps), nil
}

// Complete finalises a drill that has exhausted its active path.
func (d *Drill) Complete() error {
	if d.Status != StatusExecuting && d.Status != StatusFallback {
		return fmt.Errorf("%w: cannot complete from %s", ErrInvalidTransition, d.Status)
	}
	d.Status = StatusCompleted
	d.CompletedAt = time.Now()
	return nil
}

// Cancel aborts a drill from any active state. It is idempotent for terminal
// states.
func (d *Drill) Cancel() error {
	switch d.Status {
	case StatusCompleted, StatusCancelled:
		return nil
	case StatusRequested, StatusApproved, StatusExecuting,
		StatusFallback, StatusAwaitingRestart:
		d.Status = StatusCancelled
		d.CompletedAt = time.Now()
		return nil
	}
	return fmt.Errorf("%w: cannot cancel from %s", ErrInvalidTransition, d.Status)
}

// TriggerFallback handles a main-power recovery that causes a synchronising
// rush-close: it cancels the remaining primary steps and switches the drill to
// the plan's backup path.
func (d *Drill) TriggerFallback(plan Plan, reason string) error {
	if d.Status != StatusExecuting && d.Status != StatusFallback {
		return fmt.Errorf("%w: cannot fall back from %s", ErrInvalidTransition, d.Status)
	}
	if len(plan.BackupPath) == 0 {
		return ErrNoBackupPath
	}
	d.Status = StatusFallback
	d.Path = "backup"
	d.CurrentStep = 0
	d.FallbackReason = reason
	return nil
}

// AwaitRestart pauses the drill because the storage cabin lost power.
func (d *Drill) AwaitRestart(reason string) error {
	if d.Status != StatusExecuting && d.Status != StatusFallback {
		return fmt.Errorf("%w: cannot await restart from %s", ErrInvalidTransition, d.Status)
	}
	d.Status = StatusAwaitingRestart
	d.RestartReason = reason
	return nil
}

// ResumeFromRestart resumes execution after cabin control has been restarted.
func (d *Drill) ResumeFromRestart() error {
	if d.Status != StatusAwaitingRestart {
		return fmt.Errorf("%w: cannot resume from %s", ErrInvalidTransition, d.Status)
	}
	if d.Path == "backup" {
		d.Status = StatusFallback
	} else {
		d.Status = StatusExecuting
	}
	d.RestartReason = ""
	return nil
}

// IsActive reports whether the drill still holds runtime resources.
func (d *Drill) IsActive() bool {
	switch d.Status {
	case StatusApproved, StatusExecuting, StatusFallback, StatusAwaitingRestart:
		return true
	}
	return false
}

// DrillRepository persists black-start drills.
type DrillRepository interface {
	Save(d Drill) error
	Get(id string) (Drill, error)
	All() ([]Drill, error)
}
