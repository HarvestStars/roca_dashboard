# Rocateq Remote Diagnostics — Dashboard Demo

A runnable demo of the remote-diagnostics platform described in [docs/technical_architecture_spec.md](docs/technical_architecture_spec.md) §2.
Design notes for reviewers: [docs/dashboard_demo_design.md](docs/dashboard_demo_design.md).

```bash
go run ./cmd/rocadash      # backend + device simulator  -> http://localhost:8080/store/
docker compose up -d       # Grafana ops dashboard       -> http://localhost:3000
go test ./...
```

- Simulated controllers: `data/devices/*.json` — edit a file to change the "hardware" state.
- Persistence: append-only JSONL under `logs/` (delete it to reset history).
- LLM: `LLM_PROVIDER=template` (default, offline). `openai` / `anthropic` are stubbed in `internal/llm/remote.go`.
