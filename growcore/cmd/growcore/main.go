// Command growcore runs the Grow Core control engine and API server.
//
// Configuration is YAML (see -config) and covers infrastructure only: listen
// address, storage, control interval, and the adapter used to reach devices.
// The grow-domain model (environments, devices, roles, entity bindings) is
// owned by Grow Core and lives in per-environment YAML, edited through the
// API/UI or manually between restarts. SQLite stores runtime/history data.
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
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // Keep IANA timezones available in minimal add-on images.

	"github.com/growrig/growrig/growcore/internal/api"
	"github.com/growrig/growrig/growcore/internal/camera"
	"github.com/growrig/growrig/growcore/internal/catalog"
	"github.com/growrig/growrig/growcore/internal/catalogsource"
	"github.com/growrig/growrig/growcore/internal/config"
	"github.com/growrig/growrig/growcore/internal/control"
	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/ha"
	"github.com/growrig/growrig/growcore/internal/integrations"
	"github.com/growrig/growrig/growcore/internal/sim"
	"github.com/growrig/growrig/growcore/internal/store"
	"github.com/growrig/growrig/growcore/internal/tailscale"
	"github.com/growrig/growrig/growcore/internal/webui"
)

func main() {
	configPath := flag.String("config", "growcore.yaml", "path to YAML config (falls back to simulator defaults if absent)")
	addr := flag.String("addr", "", "override server.addr from config")
	flag.Parse()

	cfg := loadConfig(*configPath)
	if *addr != "" {
		cfg.Server.Addr = *addr
	}
	if err := cfg.ApplyWorkDir(); err != nil {
		log.Fatalf("working directory: %v", err)
	}

	st, err := store.Open(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

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

	// A single, clean lifecycle marker per boot. The engine stays quiet for its
	// settle window after start (see control.settleWindow), so a restart shows
	// only this pair — "started" now, "stopped" on shutdown — instead of a burst
	// of availability churn.
	_ = st.AddActivity(domain.Activity{Level: "info", Type: "system", Message: "Grow Core started"})

	var static http.Handler
	if h, ok := webui.Handler(); ok {
		static = h
		log.Print("web UI embedded; serving at /")
	}

	dataDir := cfg.DataDir()
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	cameraRecorder := camera.New(st, cfg.EnvironmentsDir(), cfg.Storage.Path)
	go cameraRecorder.Run(ctx)
	integrationManager, err := integrations.NewManager(st, integrations.FindBundleRoot(), filepath.Join(dataDir, ".integration-secret-key"))
	if err != nil {
		log.Fatalf("load integrations: %v", err)
	}
	catalogSources, err := catalogsource.New(dataDir)
	if err != nil {
		log.Fatalf("load catalog sources: %v", err)
	}
	applyCatalogSources := func() {
		deviceDirs := catalogSources.Dirs("devices")
		catalogDirs := make([]catalog.ExtraDir, len(deviceDirs))
		for i, dir := range deviceDirs {
			catalogDirs[i] = catalog.ExtraDir{SourceID: dir.SourceID, Dir: dir.Dir}
		}
		catalog.SetExtraDirs(catalogDirs)

		integrationDirs := catalogSources.Dirs("integrations")
		integrationRoots := make([]integrations.ExtraRoot, len(integrationDirs))
		for i, dir := range integrationDirs {
			integrationRoots[i] = integrations.ExtraRoot{SourceID: dir.SourceID, Dir: dir.Dir}
		}
		if err := integrationManager.SetExtraRoots(integrationRoots); err != nil {
			log.Printf("reload catalog integrations: %v", err)
		}
	}
	catalogSources.Apply = applyCatalogSources
	applyCatalogSources()

	apiServer := api.NewServer(st, engine, adapter, hub, string(cfg.Adapter.Type), static, cameraRecorder, filepath.Join(dataDir, "preferences.yaml"), cfg.GrowsDir(), integrationManager, catalogSources)
	if err := apiServer.ApplyPreferences(); err != nil {
		log.Fatalf("load instance preferences: %v", err)
	}
	go engine.Run(ctx, cfg.Control.Interval.Std())
	go apiServer.PollWeather(ctx)

	handler := apiServer.Handler()

	// Optional private remote access: the same handler served over a tailnet-only
	// HTTPS listener, alongside the LAN server below. Off unless the user enables
	// it in Settings; a start error is surfaced in the UI, never fatal.
	tsMgr, err := tailscale.New(filepath.Join(dataDir, "tsnet"), filepath.Join(dataDir, "tailscale.yaml"), handler)
	if err != nil {
		log.Fatalf("tailscale: %v", err)
	}
	apiServer.SetTailscale(tsMgr)
	tsMgr.Start(ctx)
	defer tsMgr.Close()

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
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
	_ = st.AddActivity(domain.Activity{Level: "info", Type: "system", Message: "Grow Core stopped"})
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
