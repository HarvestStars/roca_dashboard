// Command rocadash runs the Rocateq remote-diagnostics demo in one process:
// device simulator -> in-memory broker -> diagnostics engine -> ERP -> REST API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/harveststars/roca_dashboard/internal/api"
	"github.com/harveststars/roca_dashboard/internal/broker"
	"github.com/harveststars/roca_dashboard/internal/config"
	"github.com/harveststars/roca_dashboard/internal/diagnostics"
	"github.com/harveststars/roca_dashboard/internal/erp"
	"github.com/harveststars/roca_dashboard/internal/llm"
	"github.com/harveststars/roca_dashboard/internal/model"
	"github.com/harveststars/roca_dashboard/internal/simulator"
	"github.com/harveststars/roca_dashboard/internal/storage"
	"github.com/harveststars/roca_dashboard/internal/tunnel"
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
	stores, err := loadJSON[[]model.Store](cfg.StoresFile)
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

	// Requirement 2: ERP (trips, timesheets, receipts)
	engineers, err := loadJSON[[]erp.Engineer](cfg.EngineersFile)
	if err != nil {
		return err
	}
	extractor := llm.NewReceiptExtractor(cfg.LLM)
	erpSvc, err := erp.New(erp.Config{
		UploadDir:      cfg.UploadDir,
		PublicBaseURL:  cfg.PublicBaseURL,
		ReminderDelay:  cfg.ReminderDelay,
		ExtractTimeout: cfg.LLMTimeout,
	}, engineers, engine, extractor, cfg.LogDir, log.With("mod", "erp"))
	if err != nil {
		return err
	}
	defer erpSvc.Close()

	srv := &http.Server{Addr: cfg.Addr, ReadHeaderTimeout: 5 * time.Second, Handler: api.New(api.Deps{
		Engine: engine, Guides: guides, Telemetry: tel, ERP: erpSvc, SampleReceiptsDir: cfg.SampleReceiptsDir,
		Web: web.FS(), Log: log.With("mod", "api"),
	})}
	if cfg.Tunnel {
		port, err := listenPort(cfg.Addr)
		if err != nil {
			return err
		}
		start("tunnel", func(ctx context.Context) error {
			return tunnel.Run(ctx, tunnel.Config{
				Server: cfg.TunnelServer, RemotePort: cfg.TunnelRemotePort, LocalPort: port,
				PublicURL: cfg.PublicBaseURL, AuthUser: cfg.DemoAuthUser, AuthPass: cfg.DemoAuthPassword,
			}, log.With("mod", "tunnel"))
		})
	}

	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("rocadash listening", "addr", cfg.Addr, "public_url", cfg.PublicBaseURL,
		"ui", "/store/ /engineer/ /backoffice/", "llm_guides", guides.ProviderName(), "llm_receipts", extractor.Name(), "logs", cfg.LogDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	wg.Wait()
	return nil
}

// listenPort extracts the port from an address like ":8080" or "0.0.0.0:8080".
func listenPort(addr string) (int, error) {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("addr %q: %w", addr, err)
	}
	return strconv.Atoi(p)
}

func loadJSON[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(b, &v)
}
