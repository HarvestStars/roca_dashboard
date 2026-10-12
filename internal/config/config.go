// Package config reads settings from flags, falling back to environment
// variables (optionally loaded from a local .env file), falling back to demo
// defaults.
package config

import (
	"flag"
	"os"
	"strconv"
	"time"

	"github.com/harveststars/roca_dashboard/internal/llm"
)

type Config struct {
	Addr               string
	PublicBaseURL      string // base URL used in e-mail links (phone demo behind HTTPS)
	StoresFile         string
	DevicesDir         string
	LogDir             string
	SimulatorEnabled   bool
	OfflineTimeout     time.Duration
	VerifyTimeout      time.Duration
	TelemetryRetention time.Duration
	AvoidedTripCostEUR int
	LLM                llm.Config
	LLMTimeout         time.Duration

	// ERP (requirement 2)
	EngineersFile     string
	UploadDir         string
	SampleReceiptsDir string
	ReminderDelay     time.Duration

	// Phone demo: SSH reverse tunnel to the HTTPS entry server
	Tunnel           bool
	TunnelServer     string
	TunnelRemotePort int
	DemoAuthUser     string
	DemoAuthPassword string
}

func Load() Config {
	loadDotEnv(".env")

	var c Config
	flag.StringVar(&c.Addr, "addr", env("ADDR", ":8080"), "HTTP listen address")
	flag.StringVar(&c.PublicBaseURL, "public-url", env("PUBLIC_BASE_URL", "http://localhost:8080"), "public base URL for links in e-mails")
	flag.StringVar(&c.StoresFile, "stores", env("STORES_FILE", "data/stores.json"), "store registry")
	flag.StringVar(&c.DevicesDir, "devices", env("DEVICES_DIR", "data/devices"), "simulated controller files")
	flag.StringVar(&c.LogDir, "logs", env("LOG_DIR", "logs"), "log-file persistence directory")
	flag.BoolVar(&c.SimulatorEnabled, "sim", envBool("SIMULATOR", true), "run the file-backed device simulator")
	flag.DurationVar(&c.OfflineTimeout, "offline-timeout", envDur("OFFLINE_TIMEOUT", 30*time.Second), "no telemetry for this long => GATEWAY_OFFLINE")
	flag.DurationVar(&c.VerifyTimeout, "verify-timeout", envDur("VERIFY_TIMEOUT", 3*time.Second), "wait for self-test answer")
	flag.DurationVar(&c.TelemetryRetention, "retention", envDur("TELEMETRY_RETENTION", 6*time.Hour), "in-memory telemetry window for dashboards")
	flag.IntVar(&c.AvoidedTripCostEUR, "trip-cost", envInt("AVOIDED_TRIP_COST_EUR", 2000), "cost of one avoided site visit (EUR)")
	flag.StringVar(&c.LLM.Provider, "llm", env("LLM_PROVIDER", "auto"), "auto | template | openai | anthropic (auto = openai when a key is set)")
	flag.StringVar(&c.LLM.Model, "llm-model", env("LLM_MODEL", "gpt-5.4-mini"), "LLM model id")
	flag.DurationVar(&c.LLMTimeout, "llm-timeout", envDur("LLM_TIMEOUT", 60*time.Second), "LLM call timeout")
	flag.StringVar(&c.EngineersFile, "engineers", env("ENGINEERS_FILE", "data/engineers.json"), "field engineer registry")
	flag.StringVar(&c.UploadDir, "uploads", env("UPLOAD_DIR", "uploads"), "receipt photo storage")
	flag.StringVar(&c.SampleReceiptsDir, "samples", env("SAMPLE_RECEIPTS_DIR", "data/sample_receipts"), "demo receipt images")
	flag.DurationVar(&c.ReminderDelay, "reminder-delay", envDur("REMINDER_DELAY", 20*time.Second), "delay after check-out before the receipt reminder e-mail (production: evening cron)")
	flag.BoolVar(&c.Tunnel, "tunnel", envBool("TUNNEL", false), "open the SSH reverse tunnel so the demo is reachable at PUBLIC_BASE_URL")
	flag.StringVar(&c.TunnelServer, "tunnel-server", env("TUNNEL_SERVER", "2.28.105.58"), "ssh destination of the HTTPS entry server")
	flag.IntVar(&c.TunnelRemotePort, "tunnel-port", envInt("TUNNEL_REMOTE_PORT", 18080), "port on the server's 127.0.0.1 that Caddy proxies to")
	flag.Parse()

	c.LLM.APIKey = env("LLM_API_KEY", os.Getenv("OPENAI_API_KEY"))
	c.LLM.BaseURL = os.Getenv("LLM_BASE_URL")
	c.DemoAuthUser = os.Getenv("DEMO_BASIC_AUTH_USER")
	c.DemoAuthPassword = os.Getenv("DEMO_BASIC_AUTH_PASSWORD")
	if c.LLM.Provider == "auto" {
		c.LLM.Provider = "template"
		if c.LLM.APIKey != "" {
			c.LLM.Provider = "openai"
		}
	}
	return c
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}
