# CAMS TPS Monitoring System — SRE Operational Runbook

## 1. System Architecture Summary

The CAMS TPS Monitoring System ingests 1-second transaction aggregations from two independent Elasticsearch clusters (On-Premise and Cloud) into SQL Server (`172.24.80.169`, database: `CAMS_Monitoring`) and exposes operational telemetry to Prometheus and Grafana on server `172.24.81.99`.

---

## 2. Common Alert Responses & Troubleshooting

### Alert 1: `Worker Degraded` or `tps_aggregator_up == 0`
**Symptoms**: Health endpoint `/ready` returns status `DEGRADED` for one source; `tps_aggregator_query_errors_total` increases.
**Cause**: Network interruption or authentication timeout to Elasticsearch.
**Action**:
1. Check container logs:
   ```bash
   docker logs --tail 100 -f cams-tps-aggregator
   ```
2. Check connectivity from server:
   ```bash
   nc -zv 172.18.65.100 9200   # On-Premise
   nc -zv 10.212.224.172 9200  # Cloud
   ```
3. Verify worker isolation: Ensure the other source worker is still operating and advancing checkpoints normally.
4. The affected worker will apply exponential backoff and resume automatically once Elasticsearch recovers.

### Alert 2: `tps_aggregator_processing_lag_seconds > 300`
**Symptoms**: Checkpoint lag exceeds 5 minutes.
**Cause**: The application was temporarily stopped, or Elasticsearch had prolonged indexing lag.
**Action**:
1. Check if the worker is in recovery mode:
   `curl -s http://localhost:8080/metrics | grep tps_aggregator_recovery_mode`
2. In recovery mode, the worker automatically fetches larger historical windows up to the target watermark.
3. Check SQL Server query performance and lock waits:
   ```sql
   SELECT r.session_id, r.status, r.wait_type, r.wait_time, r.blocking_session_id
   FROM sys.dm_exec_requests r
   WHERE r.database_id = DB_ID('CAMS_Monitoring');
   ```

### Alert 3: `tps_aggregator_data_loss_risk == 1`
**Symptoms**: Watermark lag is greater than Elasticsearch retention window (7 days / 168 hours).
**Cause**: Extended downtime exceeding cluster index retention.
**Action**:
1. Inspect the last checkpoint:
   ```sql
   SELECT * FROM dbo.tps_checkpoints;
   ```
2. Check available indices in Elasticsearch using `./scripts/es-discover.sh`.
3. If older indices have been deleted by Elasticsearch Index Lifecycle Management (ILM), historical data prior to the oldest available index cannot be recovered.
4. Manually advance the checkpoint to the timestamp of the oldest available index to allow the worker to resume processing current transactions:
   ```sql
   UPDATE dbo.tps_checkpoints
   SET last_processed_until = '2026-11-06 00:00:00.000',
       updated_at = SYSUTCDATETIME()
   WHERE source_name = 'onprem';
   ```

---

## 3. Routine Maintenance & Operations

### 3.1 Container Lifecycle Commands
```bash
# View live container logs
docker logs -f cams-tps-aggregator

# Graceful restart (Sends SIGTERM; waits up to 10s for clean checkpoint commit)
docker compose restart

# Stop container
docker compose down

# Start container
docker compose up -d
```

### 3.2 SQL Server Maintenance Queries
```sql
-- Check latest TPS records
SELECT TOP 20 application_name, source_name, metric_time, total_transactions, success_transactions, failed_transactions
FROM dbo.tps_metrics
ORDER BY metric_time DESC;

-- Check summary rollups
SELECT * FROM dbo.tps_summary
ORDER BY period_start DESC;

-- Check operational worker error logs
SELECT TOP 50 * FROM dbo.tps_worker_logs
ORDER BY created_at DESC;
```

---

## 4. Key PromQL Monitoring Queries

| Purpose | PromQL Expression | Warning Threshold |
|---|---|---|
| Worker Status | `tps_aggregator_up` | `< 1` |
| Current 1s TPS | `tps_aggregator_current_tps` | N/A |
| Processing Lag | `tps_aggregator_processing_lag_seconds` | `> 180s` |
| Query Error Rate | `rate(tps_aggregator_query_errors_total[5m])` | `> 0` |
| Database Error Rate | `rate(tps_aggregator_database_errors_total[5m])` | `> 0` |
| Data-Loss Risk | `tps_aggregator_data_loss_risk` | `> 0` |
