# CAMS TPS Monitoring System

[![Build and Publish Docker Image](https://github.com/khensin166/CAMS-TPS-Agregator/actions/workflows/docker-build.yml/badge.svg)](https://github.com/khensin166/CAMS-TPS-Agregator/actions/workflows/docker-build.yml)
[![Go Version](https://img.shields.io/badge/go-1.21-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

Production-ready TPS (Transactions Per Second) Monitoring System built in Go. Collects transaction aggregations from two independent Elasticsearch clusters (On-Premise & Cloud), calculates metrics at 1-second granularity, persists historical records and periodic summaries into Microsoft SQL Server, and exposes the metrics to Prometheus and Grafana.

---

## Key Features

- **Dual Independent Workers**: Worker A (On-Premise) and Worker B (Cloud) operate completely isolated with separate Elasticsearch clients, polling cycles, checkpoints, and backoff states.
- **Accurate 1-Second TPS Aggregations**: Uses bounded `date_histogram` queries with `fixed_interval: "1s"`, distinguishing verified zero-transaction seconds from missing or failed queries.
- **Atomic SQL Server Persistence**: Metric records and checkpoint advancement are committed atomically inside a single SQL transaction (`BEGIN TRANSACTION ... MERGE ... UPDATE checkpoints ... COMMIT`).
- **Watermark Delay & Overlap Reprocessing**: Incorporates a 60-second watermark delay to handle indexing lag, plus a 120-second overlap window to reprocess late-arriving events without duplicates.
- **Calendar-Based Summaries**: Automatically rolls up Daily, Weekly, and Monthly TPS summaries calculated against `Asia/Jakarta` calendar boundaries and stored in UTC.
- **Data-Loss Risk Monitoring**: Detects when prolonged downtime causes watermark lag to exceed Elasticsearch retention (e.g. 7 days) and triggers alerts.
- **Hardened Multi-Stage Docker Container**: Standalone, non-root user (`UID 10001`), with built-in health checks and minimal resource footprint (< 30MB RAM, < 0.8% CPU at 1,000 TPS).
- **Automated CI/CD**: Automatic test execution, container image building, and publishing to GitHub Container Registry (`ghcr.io/khensin166/cams-tps-aggregator`).

---

## Architecture Overview

```
Elasticsearch On-Premise (172.18.65.100:9200) ──> Go Worker A ──┐
                                                                 ├──> Microsoft SQL Server (172.24.80.169:1433)
Elasticsearch Cloud      (10.212.224.172:9200) ──> Go Worker B ──┘           │
                                                                             ├──> dbo.tps_metrics (1s granularity)
                                                                             ├──> dbo.tps_checkpoints (Watermarks)
                                                                             └──> dbo.tps_summary (Daily/Weekly/Monthly)
                                                                                     ▲
                                                                                     │ Reads via MSSQL Datasource
                                                                           Grafana (172.24.81.99:3000)
```

---

## Project Structure

```
├── cmd/
│   └── aggregator/
│       └── main.go                         # Application entrypoint
├── internal/
│   ├── config/                             # Environment variable configuration
│   ├── elasticsearch/                      # ES HTTP client and local mock server
│   ├── aggregator/                         # Dual independent workers and bucket processors
│   ├── repository/                         # Repository interfaces
│   │   └── sqlserver/                      # SQL Server implementation with MERGE & transactions
│   ├── summary/                            # Calendar-based daily/weekly/monthly rollup calculator
│   ├── metrics/                            # Prometheus telemetry collectors
│   ├── logger/                             # slog structured logging (text & JSON)
│   └── health/                             # /health, /ready, /metrics endpoints
├── migrations/                             # SQL Server migration scripts
│   ├── 001_create_checkpoints.sql
│   ├── 002_create_metrics.sql
│   ├── 003_create_summary.sql
│   └── 004_create_worker_logs.sql
├── deployments/
│   ├── Dockerfile                          # Multi-stage production Dockerfile
│   ├── docker-compose.local.yml            # Local SQL Server for development
│   └── docker-compose.prod.yml             # Production compose for 172.24.81.99
├── grafana/
│   └── dashboards/
│       └── tps_monitoring_dashboard.json   # Ready-to-import Grafana dashboard
├── scripts/
│   ├── es-discover.sh                      # Cluster inspection script for 172.24.81.99
│   └── load-simulate.go                    # 1,000 TPS load simulation tool
├── tests/
│   └── recovery_test.go                    # Recovery & late-arrival tests
├── docs/
│   ├── DEPLOYMENT_GUIDE.md                 # Production deployment manual
│   └── OPERATIONAL_RUNBOOK.md              # SRE runbook & alert response guide
├── go.mod
└── README.md
```

---

## Local Development & Testing

### 1. Run All Tests
```bash
go test -v ./...
```

### 2. Run 1,000 TPS Load Simulation
```bash
go run ./scripts/load-simulate.go
```

### 3. Build Static Binary
```bash
go build -o bin/aggregator ./cmd/aggregator
```

---

## Production Deployment

Detailed production deployment instructions are documented in:  
👉 [DEPLOYMENT_GUIDE.md](file:///d:/eksplor/CAMS-TPS-Agregator/docs/DEPLOYMENT_GUIDE.md)

For SRE alert responses and operational procedures, see:  
👉 [OPERATIONAL_RUNBOOK.md](file:///d:/eksplor/CAMS-TPS-Agregator/docs/OPERATIONAL_RUNBOOK.md)
