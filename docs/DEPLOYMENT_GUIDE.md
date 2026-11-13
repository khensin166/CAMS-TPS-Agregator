# CAMS TPS Monitoring System — Production Deployment Guide

**Target Production Server**: `172.24.81.99` (Hostname: `prd-gti-vm-grafana-gfnsvrcao-bed-01`)  
**Operating System**: Ubuntu Linux (Docker Engine installed, Grafana container active)

---

## 1. Pre-Deployment Infrastructure Verification

Before deploying the container, execute the discovery and connectivity probe directly from `172.24.81.99`.

### 1.1 Network Connectivity Verification
Verify TCP connectivity to all target services:
```bash
# Test Elasticsearch On-Premise
nc -zv 172.18.65.100 9200

# Test Elasticsearch Cloud
nc -zv 10.212.224.172 9200

# Test SQL Server
nc -zv 172.24.80.169 1433
```

### 1.2 Elasticsearch Mapping Discovery
Execute the discovery script on the server to inspect actual index names, mappings, and sample documents:
```bash
# Set credentials if required
export ES_USER="your_es_user"
export ES_PASSWORD="your_es_password"

chmod +x scripts/es-discover.sh
./scripts/es-discover.sh
```
Review the outputs generated in `./discovery_results/` to confirm:
- Timestamp field name (typically `@timestamp`)
- Transaction index naming pattern (e.g. `cams-transactions-YYYY.MM.DD` or alias)
- Status field values (`SUCCESS`, `00`, etc.)

---

## 2. Docker Image Preparation & Distribution

### Option A: Via GitHub Container Registry (Recommended)
1. Push your changes to GitHub:
   ```bash
   git push origin main
   ```
2. GitHub Actions will automatically test, build, and publish the image:
   `ghcr.io/khensin166/cams-tps-aggregator:latest`
3. On the production server `172.24.81.99`, pull the image:
   ```bash
   docker pull ghcr.io/khensin166/cams-tps-aggregator:latest
   ```

### Option B: Offline Docker Image Transfer (Air-gapped)
If the production server cannot reach external registries:
1. On your local machine:
   ```bash
   docker build -t cams-tps-aggregator:1.0.0 .
   docker save cams-tps-aggregator:1.0.0 | gzip > cams-tps-aggregator-1.0.0.tar.gz
   scp cams-tps-aggregator-1.0.0.tar.gz user@172.24.81.99:/tmp/
   ```
2. On the production server:
   ```bash
   docker load < /tmp/cams-tps-aggregator-1.0.0.tar.gz
   ```

---

## 3. Server Deployment Setup

### 3.1 Create Deployment Directory
On `172.24.81.99`:
```bash
mkdir -p /opt/cams-tps-aggregator
cd /opt/cams-tps-aggregator
```

### 3.2 Prepare Production Environment File (`.env`)
Create `/opt/cams-tps-aggregator/.env` with strict permissions (`chmod 600 .env`):
```bash
# Application
APP_NAME=CAMS-TPS-Agregator
APP_ENV=production
APP_PORT=8080
APP_TIMEZONE=Asia/Jakarta

# SQL Server (172.24.80.169)
DB_HOST=172.24.80.169
DB_PORT=1433
DB_NAME=CAMS_Monitoring
DB_USER=svc_tps_monitor
DB_PASSWORD=SecurePasswordHere
DB_MAX_OPEN_CONNS=25
DB_MAX_IDLE_CONNS=10
DB_CONN_MAX_LIFETIME=5m
DB_COMMAND_TIMEOUT=30s

# Worker A: On-Premise (172.18.65.100)
ONPREM_ENABLED=true
ONPREM_SOURCE_NAME=onprem
ONPREM_ES_URL=http://172.18.65.100:9200
ONPREM_ES_USER=es_user_onprem
ONPREM_ES_PASSWORD=es_pass_onprem
ONPREM_INDEX_PATTERN=cams-transactions-*
ONPREM_TIMESTAMP_FIELD=@timestamp
ONPREM_POLL_INTERVAL=10s
ONPREM_WATERMARK_DELAY=60s
ONPREM_OVERLAP_WINDOW=120s
ONPREM_RETENTION_PERIOD=168h

# Worker B: Cloud (10.212.224.172)
CLOUD_ENABLED=true
CLOUD_SOURCE_NAME=cloud
CLOUD_ES_URL=http://10.212.224.172:9200
CLOUD_ES_USER=es_user_cloud
CLOUD_ES_PASSWORD=es_pass_cloud
CLOUD_INDEX_PATTERN=cams-transactions-*
CLOUD_TIMESTAMP_FIELD=@timestamp
CLOUD_POLL_INTERVAL=10s
CLOUD_WATERMARK_DELAY=60s
CLOUD_OVERLAP_WINDOW=120s
CLOUD_RETENTION_PERIOD=168h
```

### 3.3 Deploy Container
Copy `deployments/docker-compose.prod.yml` to `/opt/cams-tps-aggregator/docker-compose.yml` and run:
```bash
docker compose up -d
```

---

## 4. Post-Deployment Verification Checklist

- [ ] **Container Status**: `docker ps | grep cams-tps-aggregator` shows `Up (healthy)`.
- [ ] **Existing Grafana Status**: Verify existing Grafana container is untouched and still running normally.
- [ ] **Liveness Endpoint**: `curl -s http://localhost:8080/health` returns `{"status":"UP", ...}`.
- [ ] **Readiness Endpoint**: `curl -s http://localhost:8080/ready` returns `{"status":"UP","database":"UP","sources":{...}}`.
- [ ] **Prometheus Metrics**: `curl -s http://localhost:8080/metrics | grep tps_aggregator_up` returns `1`.
- [ ] **Database Checkpoints**: Query SQL Server to verify checkpoints are advancing:
  ```sql
  SELECT * FROM dbo.tps_checkpoints;
  ```
- [ ] **Metrics Ingestion**: Query SQL Server to verify 1-second records are being written:
  ```sql
  SELECT TOP 10 * FROM dbo.tps_metrics ORDER BY metric_time DESC;
  ```
- [ ] **Grafana Dashboard**: Import `grafana/dashboards/tps_monitoring_dashboard.json` into Grafana and connect to `CAMS_Monitoring` SQL Server datasource.
