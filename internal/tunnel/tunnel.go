// Package tunnel keeps an SSH reverse tunnel open from this machine to the
// public HTTPS entry server (Caddy), so the locally running demo is reachable
// at PublicURL. It shells out to the system `ssh`, so it uses the user's
// ~/.ssh keys, config and known_hosts; no credentials pass through this code.
//
//	phone --HTTPS--> Caddy :443 --> server 127.0.0.1:RemotePort ==ssh -R==> localhost:LocalPort
package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Config struct {
	Server     string // ssh destination, e.g. "root@2.28.105.58" or a Host alias from ~/.ssh/config
	RemotePort int    // bound to 127.0.0.1 on the server; Caddy proxies to it
	LocalPort  int    // where rocadash listens
	PublicURL  string // used for the reachability check and the log line
	AuthUser   string // basic auth for the reachability check (optional)
	AuthPass   string
}

// Run keeps the tunnel up until ctx is cancelled, reconnecting with backoff.
// It never returns an error: a broken tunnel must not take the demo down.
func Run(ctx context.Context, cfg Config, log *slog.Logger) error {
	if _, err := exec.LookPath("ssh"); err != nil {
		log.Error("tunnel disabled: ssh client not found in PATH")
		return nil
	}
	forward := fmt.Sprintf("127.0.0.1:%d:localhost:%d", cfg.RemotePort, cfg.LocalPort)
	backoff := 3 * time.Second
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, "ssh", "-N",
			"-o", "BatchMode=yes", // never hang on a password/host-key prompt
			"-o", "ExitOnForwardFailure=yes", // remote port taken => fail fast and retry
			"-o", "ServerAliveInterval=20", "-o", "ServerAliveCountMax=3",
			"-R", forward, cfg.Server)
		var stderr strings.Builder
		cmd.Stderr = &stderr

		start := time.Now()
		if err := cmd.Start(); err != nil {
			log.Error("tunnel: cannot start ssh", "err", err)
		} else {
			log.Info("tunnel connecting", "server", cfg.Server, "forward", forward)
			go checkReachable(ctx, cfg, log)
			err := cmd.Wait()
			if ctx.Err() != nil {
				break
			}
			msg := strings.TrimSpace(stderr.String())
			if strings.Contains(msg, "remote port forwarding failed") {
				log.Warn("tunnel: server port already in use — another tunnel is still connected "+
					"(another machine, tools/tunnel.ps1, or a leftover ssh from a killed server; tools/reset_demo.ps1 closes the local one). Retrying...",
					"port", cfg.RemotePort)
			} else {
				log.Warn("tunnel closed, reconnecting", "after", time.Since(start).Round(time.Second), "err", err, "ssh", msg)
			}
			if time.Since(start) > time.Minute {
				backoff = 3 * time.Second // it was healthy for a while: retry quickly
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
	log.Info("tunnel stopped")
	return nil
}

// checkReachable probes PublicURL/healthz through Caddy and the tunnel and logs
// the outcome once, so the presenter knows the phone link works.
func checkReachable(ctx context.Context, cfg Config, log *slog.Logger) {
	if cfg.PublicURL == "" || !strings.HasPrefix(cfg.PublicURL, "https://") {
		return
	}
	url := strings.TrimRight(cfg.PublicURL, "/") + "/healthz"
	client := &http.Client{Timeout: 5 * time.Second}
	var last string
	for range 8 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if cfg.AuthUser != "" {
			req.SetBasicAuth(cfg.AuthUser, cfg.AuthPass)
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err.Error()
			continue
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			log.Info("tunnel up — demo reachable", "url", strings.TrimRight(cfg.PublicURL, "/")+"/engineer/")
			return
		case http.StatusUnauthorized:
			log.Info("tunnel up — demo reachable (password required; check DEMO_BASIC_AUTH_* in .env)", "url", cfg.PublicURL)
			return
		default:
			last = resp.Status // 503 = Caddy offline page: tunnel not connected yet
		}
	}
	log.Warn("tunnel: public URL not reachable yet", "url", url, "last", last)
}
