#!/usr/bin/env bash
# ==============================================================================
# CAMS TPS Aggregator - Single Service On-Premise Parity Test (GMT+7)
# Target: cams-service-* on http://172.18.65.100:9200
# Compares Direct ES _count vs 1-Second Date Histogram Sum for exact parity
# ==============================================================================

set -euo pipefail

# Bypass corporate proxy
unset http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY
export no_proxy="*"
export NO_PROXY="*"

ES_URL="http://172.18.65.100:9200"
ES_USER="elastic"
ES_PASS="P@ssw0rd2020"
INDEX_PATTERN="cams-service-*"
TIMESTAMP_FIELD="@timestamp"

# Waktu Default: 10:47:00 s/d 10:48:00 GMT+7 pada hari ini (atau argumen baris perintah)
# Format argumen opsional: ./verify-service-onprem.sh "2026-11-13T10:47:00" "2026-11-13T10:48:00"
START_LOCAL="${1:-2026-11-13T10:47:00}"
END_LOCAL="${2:-2026-11-13T10:48:00}"

# Pastikan ada suffix +07:00
if [[ ! "${START_LOCAL}" =~ \+ ]]; then
    START_TZ="${START_LOCAL}+07:00"
else
    START_TZ="${START_LOCAL}"
fi

if [[ ! "${END_LOCAL}" =~ \+ ]]; then
    END_TZ="${END_LOCAL}+07:00"
else
    END_TZ="${END_LOCAL}"
fi

echo "=================================================================="
echo " Pengujian Akurasi Hit: On-Premise CAMS Service (WIB / GMT+7)"
echo "=================================================================="
echo " Target Host:     ${ES_URL}"
echo " Index Pattern:   ${INDEX_PATTERN}"
echo " Waktu Mulai:     ${START_TZ}"
echo " Waktu Selesai:   ${END_TZ}"
echo " Rentang Waktu:   60 Detik (1 Menit)"
echo "=================================================================="

TMP_DIR=$(mktemp -d)
trap 'rm -rf "${TMP_DIR}"' EXIT

# --- Step 1: Query Direct ES _count (Total Transaksi Mentah di ES) ---
echo "[1/3] Menjalankan Direct ES _count (Total Dokumen)..."
curl -s --noproxy "*" -u "${ES_USER}:${ES_PASS}" \
    "${ES_URL}/${INDEX_PATTERN}/_count" \
    -H "Content-Type: application/json" \
    -d '{
      "query": {
        "range": {
          "'"${TIMESTAMP_FIELD}"'": {
            "gte": "'"${START_TZ}"'",
            "lt": "'"${END_TZ}"'"
          }
        }
      }
    }' > "${TMP_DIR}/direct_total.json"

# --- Step 2: Query Direct ES _count (Hanya Transaksi Sukses 00) ---
echo "[2/3] Menjalankan Direct ES _count (Hanya Sukses ResponseCode 00)..."
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
                  "gte": "'"${START_TZ}"'",
                  "lt": "'"${END_TZ}"'"
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
            { "term": { "status": "SUCCESS" } },
            { "term": { "status": "00" } }
          ],
          "minimum_should_match": 1
        }
      }
    }' > "${TMP_DIR}/direct_success.json"

# --- Step 3: Query Aggregator 1-Second Date Histogram ---
echo "[3/3] Menjalankan Query 1-Second Date Histogram Aggregator..."
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
                  "gte": "'"${START_TZ}"'",
                  "lt": "'"${END_TZ}"'"
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
            "time_zone": "+07:00",
            "min_doc_count": 0,
            "extended_bounds": {
              "min": "'"${START_TZ}"'",
              "max": "'"${END_TZ}"'"
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

# --- Step 4: Rekonsiliasi & Output Hasil ---
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

dir_failed = dir_total - dir_success
hist_failed = hist_total - hist_success

delta_total = hist_total - dir_total
delta_success = hist_success - dir_success

print('')
print('========================================================================================')
print('  HASIL REKONSILIASI AKURASI DATA (PARITY RESULTS)')
print('========================================================================================')
print(f'Rentang Uji (GMT+7):    ${START_TZ} s/d ${END_TZ}')
print(f'Total Detik Dihitung:   {len(buckets)} detik')
if len(buckets) > 0:
    print(f'Rata-rata TPS Menit ini:{hist_total / float(len(buckets)):.2f} trx/detik')
    peak_bucket = max(buckets, key=lambda b: b.get('doc_count', 0))
    print(f'Peak 1-Detik Tertinggi: {peak_bucket.get(\"doc_count\", 0)} TPS (pada {peak_bucket.get(\"key_as_string\", \"-\")})')
print('----------------------------------------------------------------------------------------')
print(f'Metrik Transaksi        Direct ES Count     Histogram Sum       Selisih   Status')
print('----------------------------------------------------------------------------------------')

def status_str(delta):
    return '100% MATCH (PASS)' if delta == 0 else f'MISMATCH ({delta})'

print(f'Total Transaksi         {dir_total:<20} {hist_total:<20} {delta_total:<10} {status_str(delta_total)}')
print(f'Transaksi Sukses (00)   {dir_success:<20} {hist_success:<20} {delta_success:<10} {status_str(delta_success)}')
print(f'Transaksi Gagal         {dir_failed:<20} {hist_failed:<20} {delta_total-delta_success:<10} {status_str(delta_total-delta_success)}')
print('========================================================================================')

if delta_total == 0 and delta_success == 0:
    print('KESIMPULAN: BERHASIL! Query agregasi 1-detik 100% identik dengan data mentah ES.')
    print('            Nol transaksi tercecer, nol transaksi dobel.')
    sys.exit(0)
else:
    print('KESIMPULAN: Terdapat selisih antara Direct Count dan Histogram Sum.')
    sys.exit(1)
"
