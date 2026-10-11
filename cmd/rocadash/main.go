// Command rocadash runs the Rocateq remote-diagnostics demo in one process:
// device simulator -> in-memory broker -> diagnostics engine -> REST API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/harveststars/roca_dashboard/internal/api"
	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/config"
	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
	"github.com/harveststars/roca_dashboard/internal/simulator"
	"github.com/harveststars/roca_dashboard/internal/storage"
	"github.com/harveststars/roca_dashboard/web"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(config.Load(), log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	stores, err := loadStores(cfg.StoresFile)
	if err != nil {
		return err
	}

	// Persistence (log files)
	tel, err := storage.NewTelemetryLog(cfg.LogDir, cfg.TelemetryRetention)
	if err != nil {
		return err
	}
	defer tel.Close()
	journal := storage.NewFileJournal(cfg.LogDir)
	defer journal.Close()

	// Ingestion
	bus := broker.NewMemory(log.With("mod", "broker"))
	defer bus.Close()

	// Intelligence
	guides := llm.NewGuideService(llm.NewProvider(cfg.LLM), cfg.LLMTimeout, log.With("mod", "llm"))

	// Core
	engine, err := diagnostics.New(diagnostics.Config{
		OfflineTimeout:     cfg.OfflineTimeout,
		VerifyTimeout:      cfg.VerifyTimeout,
		WatchdogInterval:   time.Second,
		AvoidedTripCostEUR: cfg.AvoidedTripCostEUR,
	}, stores, bus, tel, journal, log.With("mod", "engine"))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	start := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx); err != nil {
				log.Error(name+" stopped", "err", err)
				stop()
			}
		}()
	}
	start("engine", engine.Run)
	if cfg.SimulatorEnabled {
		// Give the engine a moment to subscribe before the first heartbeat burst.
		time.Sleep(100 * time.Millisecond)
		start("simulator", simulator.New(cfg.DevicesDir, bus, log.With("mod", "sim")).Run)
	}

	srv := &http.Server{Addr: cfg.Addr, Handler: api.New(engine, guides, tel, web.Store(), log.With("mod", "api")),
		ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("rocadash listening", "addr", cfg.Addr, "store_ui", "http://localhost"+cfg.Addr+"/store/",
		"llm", guides.ProviderName(), "logs", cfg.LogDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	wg.Wait()
	return nil
}

func loadStores(path string) ([]model.Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stores []model.Store
	return stores, json.Unmarshal(b, &stores)
}
