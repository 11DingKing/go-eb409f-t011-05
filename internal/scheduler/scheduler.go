// Package scheduler runs the background housekeeping for the dispatch service:
// enforcing the 24-hour inspection-defect dispatch SLA and reactivating orders
// once an off-grid window is released.
package scheduler

import (
	"context"
	"time"

	"microgrid-dispatch/internal/app"
)

// Scheduler periodically sweeps the work-order pool.
type Scheduler struct {
	svc      *app.Service
	interval time.Duration
}

// New returns a scheduler that ticks at the given interval.
func New(svc *app.Service, interval time.Duration) *Scheduler {
	return &Scheduler{svc: svc, interval: interval}
}

// Tick performs one housekeeping pass at the supplied wall-clock time.
func (s *Scheduler) Tick(now time.Time) {
	s.svc.CheckDefectDispatchDeadlines(now)
	s.svc.ReactivateRescheduledOrders()
}

// Run blocks until ctx is cancelled, ticking at the configured interval.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.Tick(now)
		}
	}
}
