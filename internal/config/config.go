// Package config reads settings from flags, falling back to environment
// variables, falling back to demo defaults.
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
}

func Load() Config {
	var c Config
	flag.StringVar(&c.Addr, "addr", env("ADDR", ":8080"), "HTTP listen address")
	flag.StringVar(&c.StoresFile, "stores", env("STORES_FILE", "data/stores.json"), "store registry")
	flag.StringVar(&c.DevicesDir, "devices", env("DEVICES_DIR", "data/devices"), "simulated controller files")
	flag.StringVar(&c.LogDir, "logs", env("LOG_DIR", "logs"), "log-file persistence directory")
	flag.BoolVar(&c.SimulatorEnabled, "sim", envBool("SIMULATOR", true), "run the file-backed device simulator")
	flag.DurationVar(&c.OfflineTimeout, "offline-timeout", envDur("OFFLINE_TIMEOUT", 30*time.Second), "no telemetry for this long => GATEWAY_OFFLINE")
	flag.DurationVar(&c.VerifyTimeout, "verify-timeout", envDur("VERIFY_TIMEOUT", 3*time.Second), "wait for self-test answer")
	flag.DurationVar(&c.TelemetryRetention, "retention", envDur("TELEMETRY_RETENTION", 6*time.Hour), "in-memory telemetry window for dashboards")
	flag.IntVar(&c.AvoidedTripCostEUR, "trip-cost", envInt("AVOIDED_TRIP_COST_EUR", 2000), "cost of one avoided site visit (EUR)")
	flag.StringVar(&c.LLM.Provider, "llm", env("LLM_PROVIDER", "template"), "template | openai | anthropic")
	flag.DurationVar(&c.LLMTimeout, "llm-timeout", envDur("LLM_TIMEOUT", 15*time.Second), "LLM call timeout")
	flag.Parse()
	c.LLM.APIKey = os.Getenv("LLM_API_KEY")
	c.LLM.Model = os.Getenv("LLM_MODEL")
	c.LLM.BaseURL = os.Getenv("LLM_BASE_URL")
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
