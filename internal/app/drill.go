package app

import (
	"fmt"

	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
)

// ApplyDrill creates a black-start drill request for the given plan, cabin and
// crew. The cabin work lock is not taken until the drill is approved.
func (s *Service) ApplyDrill(planID, cabinID, teamID string) (blackstart.Drill, error) {
	if _, err := s.plans.Get(planID); err != nil {
		return blackstart.Drill{}, err
	}
	if _, err := s.cabins.Get(cabinID); err != nil {
		return blackstart.Drill{}, err
	}
	d := blackstart.Drill{
		ID:      s.newID("drill"),
		PlanID:  planID,
		CabinID: cabinID,
		TeamID:  teamID,
		Status:  blackstart.StatusRequested,
	}
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// ApproveDrill approves a drill, acquires the cabin work lock and reserves the
// off-grid window. On any failure the partially acquired resources are rolled
// back so the drill remains requestable. Pending inspection/repair orders for
// the cabin are auto-rescheduled.
func (s *Service) ApproveDrill(drillID, supervisorID string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if err := d.Approve(supervisorID); err != nil {
		return blackstart.Drill{}, err
	}
	if _, err := s.locks.Acquire(d.CabinID, d.TeamID, "drill"); err != nil {
		return blackstart.Drill{}, fmt.Errorf("acquire work lock: %w", err)
	}
	if err := s.windows.Occupy(d.CabinID, d.ID); err != nil {
		s.locks.Release(d.CabinID, d.TeamID)
		return blackstart.Drill{}, fmt.Errorf("occupy off-grid window: %w", err)
	}
	s.reschedulePendingOrders(d.CabinID, "off-grid window occupied by drill "+d.ID)
	if err := s.drills.Save(d); err != nil {
		s.releaseDrillResources(&d)
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// StartDrill begins execution of an approved drill along the primary path.
func (s *Service) StartDrill(drillID string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if err := d.Start(); err != nil {
		return blackstart.Drill{}, err
	}
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// ExecuteStep runs the current step of an executing drill and advances the
// cursor. When the active path is exhausted the drill is completed and its
// resources released. The returned step is the one just executed; done reports
// whether the path completed on this call.
func (s *Service) ExecuteStep(drillID string) (step blackstart.Step, done bool, err error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Step{}, false, err
	}
	if d.Status != blackstart.StatusExecuting && d.Status != blackstart.StatusFallback {
		return blackstart.Step{}, false, fmt.Errorf("%w: drill %s not executing (status %s)",
			blackstart.ErrInvalidTransition, drillID, d.Status)
	}
	plan, err := s.plans.Get(d.PlanID)
	if err != nil {
		return blackstart.Step{}, false, err
	}
	cur, ok := d.Current(plan)
	if !ok {
		if err := s.completeDrill(&d); err != nil {
			return blackstart.Step{}, false, err
		}
		return blackstart.Step{}, true, nil
	}
	step = cur
	done, err = d.Advance(plan)
	if err != nil {
		return blackstart.Step{}, false, err
	}
	if done {
		if err := s.completeDrill(&d); err != nil {
			return blackstart.Step{}, false, err
		}
	} else if err := s.drills.Save(d); err != nil {
		return blackstart.Step{}, false, err
	}
	s.persist()
	return step, done, nil
}

// CompleteDrill explicitly completes a drill, releasing its resources. It is
// idempotent for an already-completed drill.
func (s *Service) CompleteDrill(drillID string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if d.Status == blackstart.StatusCompleted {
		return d, nil
	}
	if d.Status != blackstart.StatusExecuting && d.Status != blackstart.StatusFallback {
		return blackstart.Drill{}, fmt.Errorf("%w: cannot complete drill in status %s",
			blackstart.ErrInvalidTransition, d.Status)
	}
	if err := s.completeDrill(&d); err != nil {
		return blackstart.Drill{}, err
	}
	return d, nil
}

// CancelDrill aborts a drill from any active state and releases its resources.
func (s *Service) CancelDrill(drillID string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if d.Status == blackstart.StatusCompleted || d.Status == blackstart.StatusCancelled {
		return d, nil
	}
	if err := d.Cancel(); err != nil {
		return blackstart.Drill{}, err
	}
	s.releaseDrillResources(&d)
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// HandlePowerRecovery models a main-power recovery that causes a synchronising
// rush-close: the remaining primary steps are cancelled and the drill falls
// back to the plan's backup path.
func (s *Service) HandlePowerRecovery(drillID, reason string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	plan, err := s.plans.Get(d.PlanID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if err := d.TriggerFallback(plan, reason); err != nil {
		return blackstart.Drill{}, err
	}
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// HandleCabinPowerLoss marks the cabin unpowered and pauses the drill until
// cabin control is restarted.
func (s *Service) HandleCabinPowerLoss(drillID, reason string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if _, err := s.locks.SetPowered(d.CabinID, false); err != nil {
		return blackstart.Drill{}, err
	}
	if err := d.AwaitRestart(reason); err != nil {
		return blackstart.Drill{}, err
	}
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// RestartCabinControl restarts a cabin's control after a power loss.
func (s *Service) RestartCabinControl(cabinID string) (cabin.Cabin, error) {
	c, err := s.locks.SetPowered(cabinID, true)
	if err != nil {
		return cabin.Cabin{}, err
	}
	s.persist()
	return c, nil
}

// ResumeDrill resumes a drill paused by cabin power loss and re-runs the
// current synchronisation step.
func (s *Service) ResumeDrill(drillID string) (blackstart.Drill, error) {
	d, err := s.drills.Get(drillID)
	if err != nil {
		return blackstart.Drill{}, err
	}
	if err := d.ResumeFromRestart(); err != nil {
		return blackstart.Drill{}, err
	}
	if err := s.drills.Save(d); err != nil {
		return blackstart.Drill{}, err
	}
	s.persist()
	return d, nil
}

// completeDrill finalises an active drill: marks it completed, releases the work
// lock and off-grid window, and reactivates rescheduled orders for the cabin.
func (s *Service) completeDrill(d *blackstart.Drill) error {
	if d.Status == blackstart.StatusExecuting || d.Status == blackstart.StatusFallback {
		if err := d.Complete(); err != nil {
			return err
		}
	}
	s.releaseDrillResources(d)
	if err := s.drills.Save(*d); err != nil {
		return err
	}
	s.persist()
	return nil
}

// releaseDrillResources releases the cabin work lock, the off-grid window and
// reactivates any orders that were rescheduled while the window was occupied.
func (s *Service) releaseDrillResources(d *blackstart.Drill) {
	s.locks.Release(d.CabinID, d.TeamID)
	s.windows.Release(d.CabinID, d.ID)
	s.reactivateOrders(d.CabinID)
}
