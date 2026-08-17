package httpsrv_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"microgrid-dispatch/internal/app"
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/httpsrv"
	"microgrid-dispatch/internal/store"
)

func newServer(t *testing.T) (*httpsrv.Server, *app.Service) {
	t.Helper()
	st := store.New("")
	svc := app.NewService(st.CabinRepo(), st.PlanRepo(), st.DrillRepo(), st.OrderRepo(), st)
	return httpsrv.New(svc), svc
}

func do(t *testing.T, srv *httpsrv.Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func setupCabinAndPlan(t *testing.T, srv *httpsrv.Server) {
	t.Helper()
	if code, _ := do(t, srv, "POST", "/api/cabins", map[string]any{"id": "cabin-1", "name": "储能舱", "capacity_kwh": 1250}); code != http.StatusCreated {
		t.Fatalf("create cabin: %d", code)
	}
	plan := blackstart.Plan{
		ID:          "plan-1",
		Name:        "test",
		TargetCabin: "cabin-1",
		PrimaryPath: []blackstart.Step{{ID: "p1", Name: "离网确认"}, {ID: "p2", Name: "同期并网"}},
		BackupPath:  []blackstart.Step{{ID: "b1", Name: "备用同期"}},
	}
	if code, _ := do(t, srv, "POST", "/api/plans", plan); code != http.StatusCreated {
		t.Fatalf("create plan: %d", code)
	}
}

func TestHTTPDrillFlow(t *testing.T) {
	srv, _ := newServer(t)
	setupCabinAndPlan(t, srv)

	code, body := do(t, srv, "POST", "/api/drills", map[string]any{"plan_id": "plan-1", "cabin_id": "cabin-1", "team_id": "crew-A"})
	if code != http.StatusCreated {
		t.Fatalf("apply drill: %d %v", code, body)
	}
	id := body["id"].(string)

	if code, _ := do(t, srv, "POST", "/api/drills/"+id+"/approve", map[string]any{"supervisor_id": "sup"}); code != http.StatusOK {
		t.Fatalf("approve: %d", code)
	}
	if code, _ := do(t, srv, "POST", "/api/drills/"+id+"/start", nil); code != http.StatusOK {
		t.Fatalf("start: %d", code)
	}
	for {
		code, body := do(t, srv, "POST", "/api/drills/"+id+"/execute", nil)
		if code != http.StatusOK {
			t.Fatalf("execute: %d %v", code, body)
		}
		if body["done"].(bool) {
			break
		}
	}
	_, body = do(t, srv, "GET", "/api/drills/"+id, nil)
	if body["status"] != "completed" {
		t.Fatalf("status = %v want completed", body["status"])
	}
	// Health check works too.
	if code, _ := do(t, srv, "GET", "/healthz", nil); code != http.StatusOK {
		t.Fatalf("healthz: %d", code)
	}
}

func TestHTTPLockConflictReturns409(t *testing.T) {
	srv, _ := newServer(t)
	setupCabinAndPlan(t, srv)

	_, b1 := do(t, srv, "POST", "/api/drills", map[string]any{"plan_id": "plan-1", "cabin_id": "cabin-1", "team_id": "crew-A"})
	_, b2 := do(t, srv, "POST", "/api/drills", map[string]any{"plan_id": "plan-1", "cabin_id": "cabin-1", "team_id": "crew-B"})

	if code, _ := do(t, srv, "POST", "/api/drills/"+b1["id"].(string)+"/approve", nil); code != http.StatusOK {
		t.Fatalf("first approve: %d", code)
	}
	code, _ := do(t, srv, "POST", "/api/drills/"+b2["id"].(string)+"/approve", nil)
	if code != http.StatusConflict {
		t.Fatalf("second approve code = %d want 409", code)
	}
}

func TestHTTPAnomalyRepairFlow(t *testing.T) {
	srv, _ := newServer(t)
	setupCabinAndPlan(t, srv)

	// Report an anomaly.
	code, body := do(t, srv, "POST", "/api/anomalies", map[string]any{"cabin_id": "cabin-1", "description": "inverter fault"})
	if code != http.StatusCreated {
		t.Fatalf("report anomaly: %d %v", code, body)
	}
	anomID := body["id"].(string)

	// Generate repair without isolation must fail with 409.
	if code, _ := do(t, srv, "POST", "/api/anomalies/"+anomID+"/repair", nil); code != http.StatusConflict {
		t.Fatalf("repair before isolation code = %d want 409", code)
	}

	// Isolate, then generate repair.
	if code, _ := do(t, srv, "POST", "/api/anomalies/"+anomID+"/isolate", nil); code != http.StatusOK {
		t.Fatalf("isolate: %d", code)
	}
	code, body = do(t, srv, "POST", "/api/anomalies/"+anomID+"/repair", nil)
	if code != http.StatusCreated {
		t.Fatalf("generate repair: %d %v", code, body)
	}
	repairID := body["id"].(string)

	// Dispatch, complete, retest (pass), confirm close.
	if code, _ := do(t, srv, "POST", "/api/repairs/"+repairID+"/dispatch", map[string]any{"team_id": "crew-R"}); code != http.StatusOK {
		t.Fatalf("dispatch: %d", code)
	}
	if code, _ := do(t, srv, "POST", "/api/repairs/"+repairID+"/complete", nil); code != http.StatusOK {
		t.Fatalf("complete repair: %d", code)
	}
	if code, _ := do(t, srv, "POST", "/api/repairs/"+repairID+"/retest", map[string]any{"passed": true}); code != http.StatusOK {
		t.Fatalf("retest: %d", code)
	}
	// Confirm without supervisor id still works (defaults to dispatcher).
	if code, _ := do(t, srv, "POST", "/api/repairs/"+repairID+"/confirm", nil); code != http.StatusOK {
		t.Fatalf("confirm: %d", code)
	}
	_, body = do(t, srv, "GET", "/api/orders/"+repairID, nil)
	if body["status"] != "closed" {
		t.Fatalf("repair status = %v want closed", body["status"])
	}
}
