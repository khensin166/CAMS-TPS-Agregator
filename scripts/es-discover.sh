#!/usr/bin/env bash
# ==============================================================================
# TPS Monitoring System - Elasticsearch Discovery Script
# MUST BE EXECUTED FROM AUTHORIZED PRODUCTION SERVER: 172.24.81.99
# ==============================================================================

set -euo pipefail

ES_ONPREM_HOST="${ES_ONPREM_HOST:-http://172.18.65.100:9200}"
ES_CLOUD_HOST="${ES_CLOUD_HOST:-http://10.212.224.172:9200}"

# Optional credentials (passed as env vars or arguments)
ES_USER="${ES_USER:-}"
ES_PASSWORD="${ES_PASSWORD:-}"

OUTPUT_DIR="./discovery_results"
mkdir -p "${OUTPUT_DIR}"

AUTH_HEADER=()
if [ -n "${ES_USER}" ] && [ -n "${ES_PASSWORD}" ]; then
    AUTH_HEADER=(-u "${ES_USER}:${ES_PASSWORD}")
    echo "[!] Using Basic Authentication with user: ${ES_USER}"
else
    echo "[!] No credentials provided in ES_USER/ES_PASSWORD. Attempting anonymous request..."
fi

echo "=========================================================="
echo " Starting Elasticsearch Discovery from $(hostname -I 2>/dev/null || hostname)"
echo " Target 1 (On-Premise): ${ES_ONPREM_HOST}"
echo " Target 2 (Cloud):      ${ES_CLOUD_HOST}"
echo " Output Directory:      ${OUTPUT_DIR}"
echo "=========================================================="

discover_cluster() {
    local name="$1"
    local endpoint="$2"
    local dir="${OUTPUT_DIR}/${name}"
    mkdir -p "${dir}"

    echo ""
    echo "[+] Probing ${name} (${endpoint})..."

    # 1. Cluster info & Version
    echo "  -> Fetching cluster root info..."
    curl -s -m 10 "${AUTH_HEADER[@]}" "${endpoint}/" > "${dir}/01_cluster_info.json" || echo "FAILED"
    
    # 2. Cluster Health
    echo "  -> Fetching cluster health..."
    curl -s -m 10 "${AUTH_HEADER[@]}" "${endpoint}/_cluster/health?pretty" > "${dir}/02_cluster_health.json" || echo "FAILED"

    # 3. List Indices
    echo "  -> Fetching indices list..."
    curl -s -m 10 "${AUTH_HEADER[@]}" "${endpoint}/_cat/indices?v&s=index" > "${dir}/03_cat_indices.txt" || echo "FAILED"

    # 4. Search for transaction indices specifically
    echo "  -> Looking for transaction-related indices..."
    grep -iE "trx|trans|cams|log|api|event" "${dir}/03_cat_indices.txt" > "${dir}/04_candidate_indices.txt" || true

    # Extract top candidate index
    local candidate_index
    candidate_index=$(head -n 1 "${dir}/04_candidate_indices.txt" | awk '{print $3}' || true)

    if [ -n "${candidate_index}" ]; then
        echo "  -> Sample candidate index found: ${candidate_index}"
        
        # 5. Fetch Mapping
        echo "  -> Fetching mapping for ${candidate_index}..."
        curl -s -m 10 "${AUTH_HEADER[@]}" "${endpoint}/${candidate_index}/_mapping?pretty" > "${dir}/05_mapping_${candidate_index}.json" || echo "FAILED"

        # 6. Fetch 1 Sample Document
        echo "  -> Fetching sample document from ${candidate_index}..."
        curl -s -m 10 "${AUTH_HEADER[@]}" "${endpoint}/${candidate_index}/_search?size=1&pretty" > "${dir}/06_sample_doc.json" || echo "FAILED"
    else
        echo "  [!] No obvious transaction candidate index identified in _cat/indices. Please inspect 03_cat_indices.txt manually."
    fi

    echo "[✓] Finished probing ${name}. Results saved to ${dir}/"
}

# Run discovery for both sources
discover_cluster "onprem" "${ES_ONPREM_HOST}"
discover_cluster "cloud" "${ES_CLOUD_HOST}"

echo ""
echo "=========================================================="
echo " Discovery completed. Please review JSON outputs in ${OUTPUT_DIR}/"
echo "=========================================================="
