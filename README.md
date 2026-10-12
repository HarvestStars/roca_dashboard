# Rocateq Field Operations — Demo

Runnable demo of both requirements in [docs/technical_architecture_spec.md](docs/technical_architecture_spec.md):

| | Requirement | UI | Design notes |
| :-- | :-- | :-- | :-- |
| 1 | Remote diagnostics & store self-help | `/store/` + Grafana | [docs/dashboard_demo_design.md](docs/dashboard_demo_design.md) |
| 2 | Trips, timesheets & AI receipt parsing | `/engineer/` (phone) + `/backoffice/` + Grafana | [docs/erp_demo_design.md](docs/erp_demo_design.md) |

```bash
cp .env.example .env       # put OPENAI_API_KEY in .env (never committed)
go run ./cmd/rocadash      # backend + device simulator  -> http://localhost:8080
docker compose up -d       # Grafana ops dashboard       -> http://localhost:3000
go test ./...
```

- Simulated controllers: `data/devices/*.json` — edit a file to change the "hardware" state.
- Sample receipts: `data/sample_receipts/` (regenerate: `python tools/receipts/gen_receipts.py`).
- Persistence: append-only JSONL under `logs/`, receipt photos under `uploads/`.
- Phone demo over HTTPS: `go run ./cmd/rocadash -tunnel` (or `TUNNEL=true` in .env; see docs/erp_demo_design.md §13).
- Reset: `powershell -ExecutionPolicy Bypass -File tools/reset_demo.ps1` (add `-ErpOnly` to keep diagnostics tickets); bash: `sh tools/reset_demo.sh [--erp-only]`.
- LLM: OpenAI when `OPENAI_API_KEY` is set (store guides + receipt vision); otherwise offline templates / manual entry.
- Grafana dashboard source: `python tools/grafana/gen_dashboard.py deploy/grafana/dashboards/rocateq-ops.json`.
