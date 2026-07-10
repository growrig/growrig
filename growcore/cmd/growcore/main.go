// Command growcore runs the Grow Core control engine and API server.
//
// Configuration is YAML (see -config) and covers infrastructure only: listen
// address, storage, control interval, and the adapter used to reach devices.
// The grow-domain model (environments, devices, roles, entity bindings) is
// owned by Grow Core and lives in the database, edited through the API/UI.
//
// The same binary runs either as a Home Assistant OS add-on (talking to HA
// through the Supervisor proxy) or against a remote Home Assistant during local
// development — the difference is entirely in the config file. With no config
// file it falls back to a built-in simulator so it runs with no hardware.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/growrig/growrig-platform/growcore/internal/api"
	"github.com/growrig/growrig-platform/growcore/internal/config"
	"github.com/growrig/growrig-platform/growcore/internal/control"
	"github.com/growrig/growrig-platform/growcore/internal/domain"
	"github.com/growrig/growrig-platform/growcore/internal/ha"
	"github.com/growrig/growrig-platform/growcore/internal/sim"
	"github.com/growrig/growrig-platform/growcore/internal/store"
	"github.com/growrig/growrig-platform/growcore/internal/webui"
)

func main() {
	configPath := flag.String("config", "growcore.yaml", "path to YAML config (falls back to simulator defaults if absent)")
	addr := flag.String("addr", "", "override server.addr from config")
	flag.Parse()

	cfg := loadConfig(*configPath)
	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if err := seedDefaults(st, cfg.Adapter.Type); err != nil {
		log.Fatalf("seed: %v", err)
	}

	adapter, err := buildAdapter(cfg)
	if err != nil {
		log.Fatalf("adapter: %v", err)
	}
	defer adapter.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := adapter.Start(ctx); err != nil {
		log.Fatalf("start adapter: %v", err)
	}

	hub := api.NewHub()
	engine := control.New(st, adapter, hub.Broadcast)
	go engine.Run(ctx, cfg.Control.Interval.Std())

	var static http.Handler
	if h, ok := webui.Handler(); ok {
		static = h
		log.Print("web UI embedded; serving at /")
	}

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           api.NewServer(st, engine, hub, string(cfg.Adapter.Type), static).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("Grow Core listening on %s (adapter=%s, db=%s)", cfg.Server.Addr, cfg.Adapter.Type, cfg.Storage.Path)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	os.Exit(0)
}

// loadConfig reads the config file, or uses simulator defaults if the default
// path is absent (so `growcore` runs out of the box).
func loadConfig(path string) *config.Config {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg
	}
	if errors.Is(err, os.ErrNotExist) && path == "growcore.yaml" {
		log.Printf("no %s found; using built-in simulator defaults", path)
		return config.Default()
	}
	log.Fatalf("load config %s: %v", path, err)
	return nil
}

func buildAdapter(cfg *config.Config) (control.Adapter, error) {
	switch cfg.Adapter.Type {
	case config.AdapterHomeAssistant:
		return ha.New(cfg)
	default: // simulator
		return sim.New(), nil
	}
}

// seedDefaults gives a fresh database a usable starting point: one environment,
// and — in simulator mode — the virtual controller so the platform works out of
// the box. Real devices are added through the API/UI. Existing data is left
// untouched so user edits persist across restarts.
func seedDefaults(st *store.Store, adapter config.AdapterType) error {
	envs, err := st.Environments()
	if err != nil {
		return err
	}
	if len(envs) == 0 {
		env := domain.Environment{
			ID: "env-main", Name: "Main Grow Box",
			TargetTempC: 24, TargetHumidity: 55, EmergencyTempC: 35,
		}
		if err := st.SaveEnvironment(env); err != nil {
			return err
		}
		envs = []domain.Environment{env}
	}
	if adapter != config.AdapterSimulator {
		return nil
	}
	devs, err := st.Devices()
	if err != nil {
		return err
	}
	for _, d := range devs {
		if d.ID == sim.DeviceID {
			return nil // already provisioned
		}
	}
	envID := "env-main"
	if !containsEnv(envs, envID) {
		envID = envs[0].ID
	}
	return st.SaveDevice(domain.Device{
		ID: sim.DeviceID, Name: "Breadboard Fan Controller",
		EnvironmentID: envID, Adapter: "simulator",
		Channels: []domain.Channel{
			{ID: "fan1", Name: "Fan 1", Role: domain.RoleExhaust},
			{ID: "fan2", Name: "Fan 2", Role: domain.RoleCirculation},
		},
	})
}

func containsEnv(envs []domain.Environment, id string) bool {
	for _, e := range envs {
		if e.ID == id {
			return true
		}
	}
	return false
}
