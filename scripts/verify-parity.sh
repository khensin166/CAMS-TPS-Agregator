#!/usr/bin/env bash
# ==============================================================================
# Elasticsearch Aggregator - Hit Parity & Data Reconciliation Test
# Compares Direct ES `_count` vs Aggregator 1-Second Date Histogram Sum
# Zero Delta = 100% Mathematical Parity (No missing & no duplicate transactions)
# ==============================================================================

set -euo pipefail

# 1. Force bypass corporate proxies for internal cluster IP addresses
unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY
export no_proxy="*"
export NO_PROXY="*"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="${SCRIPT_DIR}/.."

# Load environment configuration if available
if [ -f "${PROJECT_ROOT}/.env" ]; then
    echo "[i] Sourcing configuration from ${PROJECT_ROOT}/.env"
    set -a
    # shellcheck disable=SC1091
    source "${PROJECT_ROOT}/.env"
    set +a
elif [ -f "./.env" ]; then
    echo "[i] Sourcing configuration from ./.env"
    set -a
    # shellcheck disable=SC1091
    source "./.env"
    set +a
fi

# Configurable Targets
TARGET="${1:-onprem}" # "onprem" or "cloud"

if [ "${TARGET}" = "cloud" ]; then
    ES_URL="${CLOUD_ES_URL:-http://10.212.224.172:9200}"
    ES_USER="${CLOUD_ES_USER:-elastic}"
    ES_PASS="${CLOUD_ES_PASSWORD:-P@ssw0rd2026}"
    INDEX_PATTERN="${CLOUD_INDEX_PATTERN:-cams-service-*,cams-apps-*}"
    TIMESTAMP_FIELD="${CLOUD_TIMESTAMP_FIELD:-@timestamp}"
    SOURCE_LABEL="Cloud Cluster"
else
    ES_URL="${ONPREM_ES_URL:-http://172.18.65.100:9200}"
    ES_USER="${ONPREM_ES_USER:-elastic}"
    ES_PASS="${ONPREM_ES_PASSWORD:-P@ssw0rd2020}"
    INDEX_PATTERN="${ONPREM_INDEX_PATTERN:-cams-service-*,cams-apps-*}"
    TIMESTAMP_FIELD="${ONPREM_TIMESTAMP_FIELD:-@timestamp}"
    SOURCE_LABEL="On-Premise Cluster"
fi

# Determine 60-second fixed test window ending 2 minutes ago
TIMESTAMPS=$(python3 -c "
from datetime import datetime, timezone, timedelta
now = datetime.now(timezone.utc)
end_time = (now - timedelta(minutes=2)).replace(microsecond=0)
start_time = end_time - timedelta(seconds=60)
print(start_time.strftime('%Y-%m-%dT%H:%M:%SZ'), end_time.strftime('%Y-%m-%dT%H:%M:%SZ'))
")

START_ISO=$(echo "${TIMESTAMPS}" | awk '{print $1}')
END_ISO=$(echo "${TIMESTAMPS}" | awk '{print $2}')

echo "=================================================================="
echo " Starting Elasticsearch Hit Parity & Reconciliation Test"
echo "=================================================================="
echo " Target:          ${SOURCE_LABEL} (${ES_URL})"
echo " Index Pattern:   ${INDEX_PATTERN}"
echo " Timestamp Field: ${TIMESTAMP_FIELD}"
echo " Test Window:     ${START_ISO} to ${END_ISO} (60 Seconds)"
echo "=================================================================="

# Check cluster reachability
PING_STATUS=$(curl -s -o /dev/null -w "%{http_code}" --noproxy "*" -u "${ES_USER}:${ES_PASS}" "${ES_URL}/" || echo "000")
if [ "${PING_STATUS}" != "200" ]; then
    echo "[-] ERROR: Cannot reach Elasticsearch at ${ES_URL} (HTTP Status: ${PING_STATUS})"
    exit 1
fi
echo "[+] Cluster connection OK"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

# --- Step 1: Direct Count Query (All Transactions) ---
echo "[+] Querying Direct ES _count..."
curl -s --noproxy "*" -u "${ES_USER}:${ES_PASS}" \
    "${ES_URL}/${INDEX_PATTERN}/_count" \
    -H "Content-Type: application/json" \
    -d '{
      "query": {
        "range": {
          "'"${TIMESTAMP_FIELD}"'": {
            "gte": "'"${START_ISO}"'",
            "lt": "'"${END_ISO}"'"
          }
        }
      }
    }' > "${TMP_DIR}/direct_total.json"

# --- Step 2: Direct Success Count Query ---
echo "[+] Querying Direct ES Success _count..."
curl -s --noproxy "*" -u "${ES_USER}:${ES_PASS}" \
    "${ES_URL}/${INDEX_PATTERN}/_count" \
    -H "Content-Type: application/json" \
    -d '{
      "query": {
        "bool": {
          "filter": [
            {
              "range": {
                "'"${TIMESTAMP_FIELD}"'": {
                  "gte": "'"${START_ISO}"'",
                  "lt": "'"${END_ISO}"'"
                }
              }
            }
          ],
          "should": [
            { "term": { "json_resp.ResponseCode.keyword": "00" } },
            { "term": { "json_resp.ResponseCode": "00" } },
            { "term": { "json_resp.responseCode.keyword": "00" } },
            { "term": { "json_resp.responseCode": "00" } },
            { "term": { "json_resp.status.keyword": "200" } },
            { "term": { "json_resp.status": "200" } },
            { "term": { "response_code.keyword": "00" } },
            { "term": { "response_code": "00" } },
            { "term": { "status": "SUCCESS" } },
            { "term": { "status": "00" } }
          ],
          "minimum_should_match": 1
        }
      }
    }' > "${TMP_DIR}/direct_success.json"

# --- Step 3: Aggregator 1-Second Histogram Query ---
echo "[+] Executing Aggregator 1-Second Date Histogram Query..."
curl -s --noproxy "*" -u "${ES_USER}:${ES_PASS}" \
    "${ES_URL}/${INDEX_PATTERN}/_search" \
    -H "Content-Type: application/json" \
    -d '{
      "size": 0,
      "query": {
        "bool": {
          "filter": [
            {
              "range": {
                "'"${TIMESTAMP_FIELD}"'": {
                  "gte": "'"${START_ISO}"'",
                  "lt": "'"${END_ISO}"'"
                }
              }
            }
          ]
        }
      },
      "aggs": {
        "by_second": {
          "date_histogram": {
            "field": "'"${TIMESTAMP_FIELD}"'",
            "fixed_interval": "1s",
            "min_doc_count": 0,
            "extended_bounds": {
              "min": "'"${START_ISO}"'",
              "max": "'"${END_ISO}"'"
            }
          },
          "aggs": {
            "success": {
              "filter": {
                "bool": {
                  "should": [
                    { "term": { "json_resp.ResponseCode.keyword": "00" } },
                    { "term": { "json_resp.ResponseCode": "00" } },
                    { "term": { "json_resp.responseCode.keyword": "00" } },
                    { "term": { "json_resp.responseCode": "00" } },
                    { "term": { "json_resp.status.keyword": "200" } },
                    { "term": { "json_resp.status": "200" } },
                    { "term": { "response_code.keyword": "00" } },
                    { "term": { "response_code": "00" } },
                    { "term": { "status": "SUCCESS" } },
                    { "term": { "status": "00" } }
                  ],
                  "minimum_should_match": 1
                }
              }
            }
          }
        }
      }
    }' > "${TMP_DIR}/agg_response.json"

# --- Step 4: Parse & Reconcile using Python ---
python3 -c "
import json, sys

with open('${TMP_DIR}/direct_total.json') as f:
    dir_total = json.load(f).get('count', 0)

with open('${TMP_DIR}/direct_success.json') as f:
    dir_success = json.load(f).get('count', 0)

with open('${TMP_DIR}/agg_response.json') as f:
    agg_data = json.load(f)

buckets = agg_data.get('aggregations', {}).get('by_second', {}).get('buckets', [])
hist_total = sum(b.get('doc_count', 0) for b in buckets)
hist_success = sum(b.get('success', {}).get('doc_count', 0) for b in buckets)
hist_failed = hist_total - hist_success
dir_failed = dir_total - dir_success

delta_total = hist_total - dir_total
delta_success = hist_success - dir_success

print('')
print('========================================================================================')
print('  RECONCILIATION & PARITY RESULTS')
print('========================================================================================')
print(f'Buckets Processed:      {len(buckets)} one-second intervals')
print(f'Average Window TPS:     {hist_total / 60.0:.2f} transactions/sec')
print(f'Peak One-Second TPS:    {max((b.get(\"doc_count\", 0) for b in buckets), default=0)} TPS')
print('----------------------------------------------------------------------------------------')
print(f'Metric                  Direct ES Count     Histogram Sum       Delta     Status')
print('----------------------------------------------------------------------------------------')

def status_str(delta):
    return '100% PARITY MATCH (PASS)' if delta == 0 else f'MISMATCH ({delta})'

print(f'Total Transactions      {dir_total:<20} {hist_total:<20} {delta_total:<10} {status_str(delta_total)}')
print(f'Success Transactions    {dir_success:<20} {hist_success:<20} {delta_success:<10} {status_str(delta_success)}')
print(f'Failed Transactions     {dir_failed:<20} {hist_failed:<20} {delta_total-delta_success:<10} {status_str(delta_total-delta_success)}')
print('========================================================================================')

if delta_total == 0 and delta_success == 0:
    print('VERDICT: SUCCESS -> Aggregator exactly matches Elasticsearch. Zero missing data.')
    sys.exit(0)
else:
    print('VERDICT: WARNING -> Parity discrepancy detected.')
    sys.exit(1)
"
