package httpsrv_test

import (
	"net/http"
	"testing"

	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/httpsrv"
)

// setupPlanWithoutBackup registers a cabin and a plan that only has a primary
// path, i.e. no backup route was filed for it.
func setupPlanWithoutBackup(t *testing.T, srv *httpsrv.Server) {
	t.Helper()
	if code, body := do(t, srv, "POST", "/api/cabins", map[string]any{
		"id": "cabin-1", "name": "储能舱", "capacity_kwh": 1250,
	}); code != http.StatusCreated {
		t.Fatalf("create cabin: %d %v", code, body)
	}
	plan := blackstart.Plan{
		ID:          "plan-no-backup",
		Name:        "仅主路径预案",
		TargetCabin: "cabin-1",
		PrimaryPath: []blackstart.Step{
			{ID: "p1", Name: "离网确认"},
			{ID: "p2", Name: "储能舱自启"},
			{ID: "p3", Name: "同期并网"},
		},
	}
	if code, body := do(t, srv, "POST", "/api/plans", plan); code != http.StatusCreated {
		t.Fatalf("create plan: %d %v", code, body)
	}
}

// startDrillOn applies, approves and starts a drill on the given plan, then runs
// one step so the drill is mid-execution.
func startDrillOn(t *testing.T, srv *httpsrv.Server, planID string) string {
	t.Helper()
	code, body := do(t, srv, "POST", "/api/drills", map[string]any{
		"plan_id": planID, "cabin_id": "cabin-1", "team_id": "crew-A",
	})
	if code != http.StatusCreated {
		t.Fatalf("apply drill: %d %v", code, body)
	}
	id := body["id"].(string)
	if code, body := do(t, srv, "POST", "/api/drills/"+id+"/approve", map[string]any{
		"supervisor_id": "sup",
	}); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, body)
	}
	if code, body := do(t, srv, "POST", "/api/drills/"+id+"/start", nil); code != http.StatusOK {
		t.Fatalf("start: %d %v", code, body)
	}
	if code, body := do(t, srv, "POST", "/api/drills/"+id+"/execute", nil); code != http.StatusOK {
		t.Fatalf("execute first step: %d %v", code, body)
	}
	return id
}

func drillState(t *testing.T, srv *httpsrv.Server, id string) map[string]any {
	t.Helper()
	code, body := do(t, srv, "GET", "/api/drills/"+id, nil)
	if code != http.StatusOK {
		t.Fatalf("get drill: %d %v", code, body)
	}
	return body
}

// TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath covers a main-power
// recovery on a drill whose plan has no backup route filed: the drill cannot be
// switched to a route that does not exist, so the request must be refused and
// the drill must stay on its primary route.
func TestHTTPPowerRecoveryRejectedWhenPlanHasNoBackupPath(t *testing.T) {
	srv, _ := newServer(t)
	setupPlanWithoutBackup(t, srv)
	drillID := startDrillOn(t, srv, "plan-no-backup")

	code, body := do(t, srv, "POST", "/api/drills/"+drillID+"/power-recovery", map[string]any{
		"reason": "rush close",
	})
	if code != http.StatusConflict {
		t.Fatalf("power recovery on a plan without a backup route: code = %d want %d (body %v)",
			code, http.StatusConflict, body)
	}

	d := drillState(t, srv, drillID)
	if d["status"] != "executing" {
		t.Fatalf("drill status = %v, want executing (the drill must stay on its primary route)", d["status"])
	}
	if d["path"] != "primary" {
		t.Fatalf("drill path = %v, want primary", d["path"])
	}

	// The drill still owns its resources, so the cabin stays locked.
	code, c := do(t, srv, "GET", "/api/cabins/cabin-1", nil)
	if code != http.StatusOK {
		t.Fatalf("get cabin: %d %v", code, c)
	}
	if c["lock_holder"] != "crew-A" {
		t.Fatalf("cabin lock_holder = %v, want crew-A", c["lock_holder"])
	}
}

// TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute covers the recovery
// after such a refusal: the remaining primary steps must still be executable and
// the drill only reaches completed after the whole primary route has run.
func TestHTTPDrillWithoutBackupPathStillFinishesItsPrimaryRoute(t *testing.T) {
	srv, _ := newServer(t)
	setupPlanWithoutBackup(t, srv)
	drillID := startDrillOn(t, srv, "plan-no-backup")

	if code, _ := do(t, srv, "POST", "/api/drills/"+drillID+"/power-recovery", nil); code != http.StatusConflict {
		t.Fatalf("power recovery: code = %d want 409", code)
	}

	// Two primary steps remain after the one executed during setup.
	executed := 0
	for i := 0; i < 10; i++ {
		code, body := do(t, srv, "POST", "/api/drills/"+drillID+"/execute", nil)
		if code != http.StatusOK {
			t.Fatalf("execute: %d %v", code, body)
		}
		executed++
		if done, _ := body["done"].(bool); done {
			break
		}
	}
	if executed != 2 {
		t.Fatalf("executed %d further steps, want 2 remaining primary steps", executed)
	}
	if d := drillState(t, srv, drillID); d["status"] != "completed" {
		t.Fatalf("drill status = %v, want completed", d["status"])
	}
}

// TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne pins the positive path: a
// plan that does have a backup route falls back and runs it.
func TestHTTPPowerRecoverySwitchesToBackupWhenPlanHasOne(t *testing.T) {
	srv, _ := newServer(t)
	setupCabinAndPlan(t, srv)
	drillID := startDrillOn(t, srv, "plan-1")

	if code, body := do(t, srv, "POST", "/api/drills/"+drillID+"/power-recovery", map[string]any{
		"reason": "rush close",
	}); code != http.StatusOK {
		t.Fatalf("power recovery on a plan with a backup route: %d %v", code, body)
	}
	d := drillState(t, srv, drillID)
	if d["status"] != "fallback" || d["path"] != "backup" {
		t.Fatalf("drill state = %v, want fallback on the backup route", d)
	}

	code, body := do(t, srv, "POST", "/api/drills/"+drillID+"/execute", nil)
	if code != http.StatusOK {
		t.Fatalf("execute backup step: %d %v", code, body)
	}
	if done, _ := body["done"].(bool); !done {
		t.Fatalf("backup route has one step, expected done=true, got %v", body)
	}
	if d := drillState(t, srv, drillID); d["status"] != "completed" {
		t.Fatalf("drill status = %v, want completed", d["status"])
	}
}
