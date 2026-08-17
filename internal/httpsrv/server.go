// Package httpsrv exposes the dispatch service over HTTP using only the
// standard library router. It maps domain errors to HTTP status codes so that
// lock conflicts, illegal transitions and missing entities are reported
// precisely.
package httpsrv

import (
	"encoding/json"
	"errors"
	"net/http"

	"microgrid-dispatch/internal/app"
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/domain/cabin"
	"microgrid-dispatch/internal/domain/workorder"
)

// Server is the HTTP front-end for the dispatch service.
type Server struct {
	svc *app.Service
	mux *http.ServeMux
}

// New builds a server with all routes registered.
func New(svc *app.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	m.HandleFunc("GET /api/cabins", s.listCabins)
	m.HandleFunc("POST /api/cabins", s.createCabin)
	m.HandleFunc("GET /api/cabins/{id}", s.getCabin)
	m.HandleFunc("POST /api/cabins/{id}/restart", s.restartCabin)
	m.HandleFunc("GET /api/plans", s.listPlans)
	m.HandleFunc("POST /api/plans", s.createPlan)
	m.HandleFunc("POST /api/drills", s.applyDrill)
	m.HandleFunc("GET /api/drills/{id}", s.getDrill)
	m.HandleFunc("POST /api/drills/{id}/approve", s.approveDrill)
	m.HandleFunc("POST /api/drills/{id}/start", s.startDrill)
	m.HandleFunc("POST /api/drills/{id}/execute", s.executeStep)
	m.HandleFunc("POST /api/drills/{id}/complete", s.completeDrill)
	m.HandleFunc("POST /api/drills/{id}/cancel", s.cancelDrill)
	m.HandleFunc("POST /api/drills/{id}/power-recovery", s.powerRecovery)
	m.HandleFunc("POST /api/drills/{id}/cabin-powerloss", s.cabinPowerLoss)
	m.HandleFunc("POST /api/drills/{id}/resume", s.resumeDrill)
	m.HandleFunc("POST /api/inspections", s.createInspection)
	m.HandleFunc("POST /api/inspections/{id}/complete", s.completeInspection)
	m.HandleFunc("POST /api/anomalies", s.reportAnomaly)
	m.HandleFunc("POST /api/anomalies/{id}/isolate", s.isolateAnomaly)
	m.HandleFunc("POST /api/anomalies/{id}/repair", s.generateRepair)
	m.HandleFunc("POST /api/repairs/{id}/dispatch", s.dispatchRepair)
	m.HandleFunc("POST /api/repairs/{id}/complete", s.completeRepair)
	m.HandleFunc("POST /api/repairs/{id}/retest", s.retestRepair)
	m.HandleFunc("POST /api/repairs/{id}/confirm", s.confirmClose)
	m.HandleFunc("GET /api/orders/{id}", s.getOrder)
}

// ServeHTTP dispatches to the registered routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Listen binds the HTTP listener.
func (s *Server) Listen(addr string) error {
	return http.ListenAndServe(addr, s.mux)
}

type cabinReq struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CapacityKWh float64 `json:"capacity_kwh"`
}

func (s *Server) listCabins(w http.ResponseWriter, r *http.Request) {
	cs, err := s.svc.ListCabins()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) getCabin(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.GetCabin(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) createCabin(w http.ResponseWriter, r *http.Request) {
	var req cabinReq
	if err := decode(r, &req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if req.ID == "" || req.Name == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("id and name required"))
		return
	}
	c, err := s.svc.RegisterCabin(req.ID, req.Name, req.CapacityKWh)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) restartCabin(w http.ResponseWriter, r *http.Request) {
	c, err := s.svc.RestartCabinControl(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	ps, err := s.svc.ListPlans()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	var p blackstart.Plan
	if err := decode(r, &p); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if p.Name == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("plan name required"))
		return
	}
	out, err := s.svc.RegisterPlan(p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) applyDrill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID  string `json:"plan_id"`
		CabinID string `json:"cabin_id"`
		TeamID  string `json:"team_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if req.PlanID == "" || req.CabinID == "" || req.TeamID == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("plan_id, cabin_id and team_id required"))
		return
	}
	d, err := s.svc.ApplyDrill(req.PlanID, req.CabinID, req.TeamID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) getDrill(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.GetDrill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) approveDrill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SupervisorID string `json:"supervisor_id"`
	}
	_ = decode(r, &req)
	if req.SupervisorID == "" {
		req.SupervisorID = "dispatcher"
	}
	d, err := s.svc.ApproveDrill(r.PathValue("id"), req.SupervisorID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) startDrill(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.StartDrill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) executeStep(w http.ResponseWriter, r *http.Request) {
	step, done, err := s.svc.ExecuteStep(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"step": step, "done": done})
}

func (s *Server) completeDrill(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.CompleteDrill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) cancelDrill(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.CancelDrill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) powerRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &req)
	if req.Reason == "" {
		req.Reason = "main power recovery"
	}
	d, err := s.svc.HandlePowerRecovery(r.PathValue("id"), req.Reason)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) cabinPowerLoss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &req)
	if req.Reason == "" {
		req.Reason = "cabin power loss"
	}
	d, err := s.svc.HandleCabinPowerLoss(r.PathValue("id"), req.Reason)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) resumeDrill(w http.ResponseWriter, r *http.Request) {
	d, err := s.svc.ResumeDrill(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) createInspection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CabinID     string `json:"cabin_id"`
		Description string `json:"description"`
	}
	if err := decode(r, &req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if req.CabinID == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("cabin_id required"))
		return
	}
	o, err := s.svc.CreateInspection(req.CabinID, req.Description)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) completeInspection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DefectFound bool   `json:"defect_found"`
		DefectDesc  string `json:"defect_desc"`
	}
	_ = decode(r, &req)
	o, err := s.svc.CompleteInspection(r.PathValue("id"), req.DefectFound, req.DefectDesc)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) reportAnomaly(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CabinID     string `json:"cabin_id"`
		Description string `json:"description"`
	}
	if err := decode(r, &req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if req.CabinID == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("cabin_id required"))
		return
	}
	o, err := s.svc.ReportAnomaly(req.CabinID, req.Description)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) isolateAnomaly(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.IsolateAnomaly(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) generateRepair(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.GenerateRepairFromAnomaly(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) dispatchRepair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TeamID string `json:"team_id"`
	}
	if err := decode(r, &req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err)
		return
	}
	if req.TeamID == "" {
		writeErrCode(w, http.StatusBadRequest, errors.New("team_id required"))
		return
	}
	o, err := s.svc.DispatchRepair(r.PathValue("id"), req.TeamID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) completeRepair(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.CompleteRepair(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) retestRepair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passed bool `json:"passed"`
	}
	_ = decode(r, &req)
	o, err := s.svc.RetestRepair(r.PathValue("id"), req.Passed)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) confirmClose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SupervisorID string `json:"supervisor_id"`
	}
	_ = decode(r, &req)
	if req.SupervisorID == "" {
		req.SupervisorID = "dispatcher"
	}
	o, err := s.svc.ConfirmClose(r.PathValue("id"), req.SupervisorID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.GetOrder(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps a domain error to an HTTP status code and writes the response.
func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, statusFor(err), map[string]string{"error": err.Error()})
}

// writeErrCode writes an error with an explicit status code.
func writeErrCode(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// statusFor maps a domain error to an HTTP status code.
func statusFor(err error) int {
	switch {
	case errors.Is(err, cabin.ErrCabinNotFound),
		errors.Is(err, blackstart.ErrPlanNotFound),
		errors.Is(err, blackstart.ErrDrillNotFound),
		errors.Is(err, workorder.ErrOrderNotFound):
		return http.StatusNotFound
	case errors.Is(err, cabin.ErrLockHeld),
		errors.Is(err, cabin.ErrNotLockHolder),
		errors.Is(err, blackstart.ErrInvalidTransition),
		errors.Is(err, blackstart.ErrNoBackupPath),
		errors.Is(err, workorder.ErrInvalidTransition),
		errors.Is(err, workorder.ErrNotIsolated),
		errors.Is(err, workorder.ErrRetestRequired):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
