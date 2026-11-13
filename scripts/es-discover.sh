#!/usr/bin/env bash
# ==============================================================================
# TPS Monitoring System - Elasticsearch Discovery Script
# MUST BE EXECUTED FROM AUTHORIZED PRODUCTION SERVER: 172.24.81.99
# ==============================================================================

set -euo pipefail

# 1. Automatically load credentials from .discovery.env or .env if present
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/.discovery.env" ]; then
    echo "[i] Loading credentials from ${SCRIPT_DIR}/.discovery.env"
    # shellcheck disable=SC1091
    source "${SCRIPT_DIR}/.discovery.env"
elif [ -f "./.discovery.env" ]; then
    echo "[i] Loading credentials from ./.discovery.env"
    # shellcheck disable=SC1091
    source "./.discovery.env"
elif [ -f "./.env" ]; then
    echo "[i] Loading credentials from ./.env"
    # shellcheck disable=SC1091
    source "./.env"
fi

# 2. Endpoints
ES_ONPREM_HOST="${ES_ONPREM_HOST:-http://172.18.65.100:9200}"
ES_CLOUD_HOST="${ES_CLOUD_HOST:-http://10.212.224.172:9200}"

# 3. Independent Credentials for On-Premise and Cloud
ES_ONPREM_USER="${ES_ONPREM_USER:-${ONPREM_ES_USER:-}}"
ES_ONPREM_PASSWORD="${ES_ONPREM_PASSWORD:-${ONPREM_ES_PASSWORD:-}}"

ES_CLOUD_USER="${ES_CLOUD_USER:-${CLOUD_ES_USER:-}}"
ES_CLOUD_PASSWORD="${ES_CLOUD_PASSWORD:-${CLOUD_ES_PASSWORD:-}}"

OUTPUT_DIR="./discovery_results"
mkdir -p "${OUTPUT_DIR}"

echo "=========================================================="
echo " Starting Elasticsearch Discovery from $(hostname -I 2>/dev/null || hostname)"
echo " Target 1 (On-Premise): ${ES_ONPREM_HOST}"
echo " Target 2 (Cloud):      ${ES_CLOUD_HOST}"
echo " Output Directory:      ${OUTPUT_DIR}"
echo "=========================================================="

discover_cluster() {
    local name="$1"
    local endpoint="$2"
    local user="$3"
    local pass="$4"
    local dir="${OUTPUT_DIR}/${name}"
    mkdir -p "${dir}"

    local auth_header=()
    if [ -n "${user}" ] && [ -n "${pass}" ]; then
        auth_header=(-u "${user}:${pass}")
        echo ""
        echo "[+] Probing ${name} (${endpoint}) with Basic Auth user: ${user}..."
    else
        echo ""
        echo "[!] No credentials provided for ${name} (${endpoint}). Attempting anonymous request..."
    fi

    # 1. Cluster info & Version
    echo "  -> [1/6] Fetching cluster root info..."
    curl -s -m 10 "${auth_header[@]}" "${endpoint}/" > "${dir}/01_cluster_info.json" || echo "FAILED"
    
    # 2. Cluster Health
    echo "  -> [2/6] Fetching cluster health..."
    curl -s -m 10 "${auth_header[@]}" "${endpoint}/_cluster/health?pretty" > "${dir}/02_cluster_health.json" || echo "FAILED"

    # 3. List Indices
    echo "  -> [3/6] Fetching indices list..."
    curl -s -m 10 "${auth_header[@]}" "${endpoint}/_cat/indices?v&s=index" > "${dir}/03_cat_indices.txt" || echo "FAILED"

    # 4. Search for transaction indices specifically
    echo "  -> [4/6] Looking for transaction-related indices..."
    grep -iE "trx|trans|cams|log|api|event" "${dir}/03_cat_indices.txt" > "${dir}/04_candidate_indices.txt" || true

    # Extract top candidate index
    local candidate_index
    candidate_index=$(head -n 1 "${dir}/04_candidate_indices.txt" | awk '{print $3}' || true)

    if [ -n "${candidate_index}" ]; then
        echo "  -> Sample candidate index found: ${candidate_index}"
        
        # 5. Fetch Mapping
        echo "  -> [5/6] Fetching mapping for ${candidate_index}..."
        curl -s -m 10 "${auth_header[@]}" "${endpoint}/${candidate_index}/_mapping?pretty" > "${dir}/05_mapping_${candidate_index}.json" || echo "FAILED"

        # 6. Fetch 1 Sample Document
        echo "  -> [6/6] Fetching sample document from ${candidate_index}..."
        curl -s -m 10 "${auth_header[@]}" "${endpoint}/${candidate_index}/_search?size=1&pretty" > "${dir}/06_sample_doc.json" || echo "FAILED"
    else
        echo "  [!] No obvious transaction candidate index identified in _cat/indices. Please inspect 03_cat_indices.txt manually."
    fi

    echo "[✓] Finished probing ${name}. Results saved to ${dir}/"
}

# Run discovery for On-Premise with its own credentials
discover_cluster "onprem" "${ES_ONPREM_HOST}" "${ES_ONPREM_USER}" "${ES_ONPREM_PASSWORD}"

# Run discovery for Cloud with its own credentials
discover_cluster "cloud" "${ES_CLOUD_HOST}" "${ES_CLOUD_USER}" "${ES_CLOUD_PASSWORD}"

echo ""
echo "=========================================================="
echo " Discovery completed. Outputs saved to ${OUTPUT_DIR}/"
echo " Review the files in:"
echo "   - ${OUTPUT_DIR}/onprem/"
echo "   - ${OUTPUT_DIR}/cloud/"
echo "=========================================================="
