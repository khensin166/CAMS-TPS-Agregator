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

## 4. Analisis Terkait `cams-apps-*` vs `cams-service-*`

Dari hasil inspeksi sampel dokumen nyata `cams-apps-2026.11.13` (On-Premise) dan `cams-apps-2026.10.29` (Cloud), ditemukan arsitektur yang sangat jelas:

### 4.1 Perbandingan Karakteristik Dua Kanal CAMS:

| Parameter | `cams-apps-*` (Kanal ISO 8583) | `cams-service-*` (Kanal REST/JSON API) |
|---|---|---|
| **Fungsi / Peran** | **ISO Router Engine** untuk ATM, EDC, BRILink | **Backend Service API** untuk Mobile Banking, Web, ESB |
| **Log Source / Binary** | `/u/bricam/cam/apps/APR/isorouter.*` | `/u/bricam/cam/service/RESP/*` |
| **Field Timestamp** | `@timestamp` (`2026-11-13T01:17:10.070Z`) | `@timestamp` (`2026-11-13T02:03:16.419Z`) |
| **Field Respon Sukses** | **`response_code == "00"`** (Root level) | **`json_resp.ResponseCode == "00"`** (On-Prem) / **`responseCode == "00"`** (Cloud) |
| **Identitas Transaksi** | `iso: "200"`, `termid`, `proccode`, `cardno` | `ChannelID: "ESB"`, `Action: "InquiryByNoKartu"` |
| **Status di Cloud** | Identik (`/u/bricam/cam/apps/APR/isorouter.*`), saat ini sedang di-stop/standby | Aktif melayani transaksi harian |

### 4.2 Kesimpulan:
1. `cams-apps` dan `cams-service` **BUKAN duplikat**, melainkan **dua kanal transaksi yang saling melengkapi**:
   - `cams-apps` melayani transaksi berbasis ISO 8583 (ATM, EDC, Merchant).
   - `cams-service` melayani transaksi berbasis JSON/REST API (ESB, Inquiry, Digital).
2. Di Cloud, path log aplikasi juga mengarah ke `isorouter` (`/u/bricam/cam/apps/APR/isorouter.1865140`). Artinya begitu CAMS Apps di Cloud diaktifkan kembali, formatnya akan **persis sama 100%** dengan On-Premise.
3. **Pilihan Konfigurasi Pattern Index**:
   - **Opsi Rekomendasi (Semua Kanal Tergabung Total TPS)**:
     ```ini
     ONPREM_INDEX_PATTERN=cams-service-*,cams-apps-*
     CLOUD_INDEX_PATTERN=cams-service-*,cams-apps-*
     ```
     Dengan pattern ini, TPS On-Premise akan mencakup total transaksi ISO ATM/EDC (~7M/hari) + API ESB (~3M/hari) = ~10M transaksi/hari. Dan ketika Cloud Apps hidup, Cloud TPS akan otomatis langsung membaca log ISO tanpa perlu restart atau perubahan konfigurasi lagi!
   - **Opsi Alternatif (Hanya Service API)**:
     ```ini
     ONPREM_INDEX_PATTERN=cams-service-*
     CLOUD_INDEX_PATTERN=cams-service-*
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
