// Command dispatch is the entry point for the Ejina microgrid dispatch service.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"microgrid-dispatch/internal/app"
	"microgrid-dispatch/internal/domain/blackstart"
	"microgrid-dispatch/internal/httpsrv"
	"microgrid-dispatch/internal/scheduler"
	"microgrid-dispatch/internal/store"
)

type config struct {
	ListenAddr        string `json:"listen_addr"`
	DataPath          string `json:"data_path"`
	SchedulerInterval string `json:"scheduler_interval"`
}

func loadConfig(path string) config {
	c := config{
		ListenAddr:        ":48302",
		DataPath:          "/tmp/microgrid-dispatch-state.json",
		SchedulerInterval: "30s",
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("DATA_PATH"); v != "" {
		c.DataPath = v
	}
	return c
}

func main() {
	cfg := loadConfig("config.json")

	st := store.New(cfg.DataPath)
	if err := st.Load(); err != nil {
		log.Fatalf("load state: %v", err)
	}
	svc := app.NewService(st.CabinRepo(), st.PlanRepo(), st.DrillRepo(), st.OrderRepo(), st)
	seed(svc)

	interval, err := time.ParseDuration(cfg.SchedulerInterval)
	if err != nil || interval <= 0 {
		interval = 30 * time.Second
	}
	sch := scheduler.New(svc, interval)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go sch.Run(ctx)

	srv := httpsrv.New(svc)
	log.Printf("microgrid dispatch service listening on %s", cfg.ListenAddr)
	if err := srv.Listen(cfg.ListenAddr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// seed bootstraps a default cabin and plan when the ledgers are empty so the
// service is usable immediately after a fresh start.
func seed(svc *app.Service) {
	if cabins, _ := svc.ListCabins(); len(cabins) == 0 {
		svc.RegisterCabin("cabin-01", "1号构网型储能舱", 1250)
		svc.RegisterCabin("cabin-02", "2号构网型储能舱", 2000)
	}
	if plans, _ := svc.ListPlans(); len(plans) == 0 {
		svc.RegisterPlan(blackstart.Plan{
			ID:          "plan-default",
			Name:        "默认黑启动预案",
			TargetCabin: "cabin-01",
			PrimaryPath: []blackstart.Step{
				{ID: "p1", Name: "离网确认", Action: "island_mode_confirm"},
				{ID: "p2", Name: "储能舱自启", Action: "storage_self_start"},
				{ID: "p3", Name: "同期并网", Action: "sync_grid_close"},
			},
			BackupPath: []blackstart.Step{
				{ID: "b1", Name: "备用电源切入", Action: "backup_source_switch"},
				{ID: "b2", Name: "同期并网", Action: "sync_grid_close"},
			},
		})
	}
}
