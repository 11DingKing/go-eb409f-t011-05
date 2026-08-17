// Package blackstart models the black-start contingency plan library and the
// drill state machine that executes a plan along its primary or backup path.
package blackstart

import "errors"

// ErrPlanNotFound is returned when a plan id does not exist.
var ErrPlanNotFound = errors.New("black-start plan not found")

// Step is a single action within a black-start path.
type Step struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

// Plan is a black-start contingency plan with a primary and a backup path.
type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TargetCabin string `json:"target_cabin"`
	PrimaryPath []Step `json:"primary_path"`
	BackupPath  []Step `json:"backup_path"`
}

// Path returns the steps for the named path ("primary" or "backup").
func (p Plan) Path(name string) []Step {
	if name == "backup" {
		return p.BackupPath
	}
	return p.PrimaryPath
}

// PlanRepository persists black-start plans.
type PlanRepository interface {
	Save(p Plan) error
	Get(id string) (Plan, error)
	All() ([]Plan, error)
}
