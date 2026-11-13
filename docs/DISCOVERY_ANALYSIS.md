# CAMS TPS Monitoring — Elasticsearch Discovery & Schema Verification

Captured on: `2026-11-13`  
Source On-Premise: `172.18.65.100:9200` (Cluster: `cao-cluster`, ES v7.7.0)  
Source Cloud: `10.212.224.172:9200`  

---

## 1. Analisis Perbandingan Indeks On-Premise vs Cloud

| Kategori Index | Pola Index | On-Premise Volume / Hari | Cloud Volume / Hari | Status & Rekomendasi |
|---|---|---|---|---|
| **CAMS Service** | **`cams-service-*`** | **~3 - 6 Juta docs** (13 - 19 GB) | **~800k - 1.6 Juta docs** (3 - 6 GB) | ⭐ **Kandidat Utama Terkuat** (Aktif simetris di On-Prem & Cloud) |
| **CAMS Apps** | `cams-apps-*` | ~7 Juta docs (25 - 30 GB) | Tidak aktif (~200 docs di Okt) | Opsional (Hanya On-Prem aktif) |
| **CAMS Load Balancer** | `cams-lb-*` | ~6.5 - 7.5 Juta docs (8 - 10 GB) | Tidak ada | Layer gateway / network On-Prem |
| **Credit Card Way4** | `cc_way4_transaction_flat-*` | ~150k - 250k docs (1.5 - 3.5 GB) | Tidak ada | Khusus log kartu kredit / Way4 |
| **CDC Core/Mobile Banking**| `cdc_logdat_mblog-*` | > 170 Juta docs (400+ GB) | Tidak ada | Terlalu masif (Database CDC) |

---

## 2. Struktur Dokumen Transaksi Nyata (`cams-service-*`)

Berdasarkan sampel dokumen asli dari index `cams-service-2026.11.13` di kedua cluster:

### 2.1 Perbandingan Skema: On-Premise (v2) vs Cloud (v3)

| Atribut / Field | On-Premise (`172.18.65.100`) | Cloud (`10.212.224.172`) | Penanganan di Go Aggregator |
|---|---|---|---|
| **Timestamp** | `@timestamp` (`2026-11-13T02:03:16.419Z`) | `@timestamp` (`2026-11-13T03:13:03.605Z`) | Identik, menggunakan field `@timestamp` |
| **Versi CAMS** | `version`: `"v2"` | `version`: `"v3"` | Otomatis terdeteksi |
| **Kode Respon Sukses** | `json_resp.ResponseCode`: `"00"` (PascalCase) | `json_resp.responseCode`: `"00"` (camelCase) | Aggregator query mendukung kedua varian case (`ResponseCode` & `responseCode`) |
| **Status HTTP Respon** | - | `json_resp.status`: `"200"` | Aggregator query mendukung `status == "200"` |
| **Keterangan Respon** | `json_resp.ResponseDesc`: `"SUKSES"` | `json_resp.responseDesc`: `"SUKSES"` | - |
| **Kardinalitas** | 1 Doc = 1 Request + 1 Response | 1 Doc = 1 Request + 1 Response | Aman, **tidak ada risiko double counting** |
| **IP Sumber / Host** | `host.name`, `ip` | `ip` | - |

### 2.2 Cuplikan Sampel Dokumen Asli

#### On-Premise (`discovery_results/onprem/06_sample_doc.json`):
```json
{
  "_index": "cams-service-2026.11.13",
  "_id": "6Uak0qEB_sEmQAgu4j11",
  "_source": {
    "@timestamp": "2026-11-13T02:03:16.419Z",
    "version": "v2",
    "ip": "172.27.27.150",
    "json_req": {
      "ChannelID": "ESB",
      "Action": "InquiryByNoKartu",
      "CardNo": "6013013657940263"
    },
    "json_resp": {
      "ResponseCode": "00",
      "ResponseDesc": "SUKSES"
    }
  }
}
```

#### Cloud (`discovery_results/cloud/06_sample_doc.json`):
```json
{
  "_index": "cams-service-2026.11.13",
  "_id": "ORXl0qEBjyq2Lyq-FIw8",
  "_source": {
    "@timestamp": "2026-11-13T03:13:03.605Z",
    "version": "v3",
    "ip": "10.212.224.166",
    "uri": "/000000000000000",
    "json_resp": {
      "status": "200",
      "responseCode": "00",
      "responseDesc": "SUKSES"
    }
  }
}
```

---

## 3. Catatan Autentikasi Kluster

| Kluster | Host & Port | Akun Terverifikasi | Status Koneksi & Query |
|---|---|---|---|
| **On-Premise** | `http://172.18.65.100:9200` | `elastic` / `P@ssw0rd2020` | ✅ **SUKSES** (Cluster: `cao-cluster`, v7.7.0, dokumen berhasil diambil) |
| **Cloud** | `http://10.212.224.172:9200` | `elastic` / `P@ssw0rd2026` | ✅ **SUKSES** (Dokumen v3 berhasil diambil setelah koreksi password) |

---

## 4. Analisis Terkait `cams-apps-*`

Berdasarkan daftar index yang aktif:
- **`cams-apps-*`**:
  - Di On-Premise memiliki volume tinggi (~7 juta docs/hari).
  - Di Cloud **tidak aktif** (hanya ~200 dokumen di bulan Oktober).
- **`cams-service-*`**:
  - Aktif di **kedua cluster** (On-Premise: 2.9 juta/hari, Cloud: 835 ribu/hari).
  - Merupakan core backend execution layer dari transaksi CAMS (memuat info kartu, inquiry, debit/credit, dsb).

**Rekomendasi**:
1. Gunakan **`cams-service-*`** sebagai index utama di Worker A (On-Premise) dan Worker B (Cloud). Ini menjamin keseragaman metrik TPS antara On-Premise dan Cloud.
2. Jika ingin memverifikasi apa isi dari `cams-apps-*` On-Premise sebelum finalisasi, jalankan perintah berikut di server `172.24.81.99`:
   ```bash
   curl --noproxy "*" -u "elastic:P@ssw0rd2020" "http://172.18.65.100:9200/cams-apps-2026.11.13/_search?size=1&pretty"
   ```

---

## 5. Konfigurasi Produksi Rekomendasi (`.env`)

```ini
# Worker A: On-Premise
ONPREM_ENABLED=true
ONPREM_SOURCE_NAME=onprem
ONPREM_ES_URL=http://172.18.65.100:9200
ONPREM_ES_USER=elastic
ONPREM_ES_PASSWORD=P@ssw0rd2020
ONPREM_INDEX_PATTERN=cams-service-*
ONPREM_TIMESTAMP_FIELD=@timestamp

# Worker B: Cloud
CLOUD_ENABLED=true
CLOUD_SOURCE_NAME=cloud
CLOUD_ES_URL=http://10.212.224.172:9200
CLOUD_ES_USER=elastic
CLOUD_ES_PASSWORD=P@ssw0rd2026
CLOUD_INDEX_PATTERN=cams-service-*
CLOUD_TIMESTAMP_FIELD=@timestamp
```
