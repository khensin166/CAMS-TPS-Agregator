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

Berdasarkan sampel dokumen asli dari index `cams-service-2026.11.13`:

### 2.1 Kesimpulan Kritis:
1. **Kardinalitas Dokumen (1 Doc = 1 Transaksi Penuh)**:
   - Satu dokumen Elasticsearch memuat **lengkap** baik request (`json_req`) maupun response (`json_resp`).
   - Tidak ada dokumen request dan response yang terpisah.
   - **Artinya: Setiap 1 dokumen = tepat 1 transaksi CAMS**. Perhitungan TPS aman dari risiko dobel hitungan.

2. **Field Timestamp**:
   - Field waktu adalah: **`@timestamp`** (Format ISO 8601 UTC: `2026-11-13T02:03:16.419Z`).
   - Sesuai dengan konfigurasi default: `ONPREM_TIMESTAMP_FIELD=@timestamp`.

3. **Field Penentu Sukses vs Gagal**:
   - Field respon utama: **`json_resp.ResponseCode`**.
   - Transaksi Sukses: `json_resp.ResponseCode == "00"` (dengan `json_resp.ResponseDesc == "SUKSES"`).
   - Transaksi Gagal: `json_resp.ResponseCode != "00"`.

4. **Metrik Tambahan yang Tersedia**:
   - Channel: `json_req.ChannelID` (contoh: `"ESB"`).
   - Action / Fitur: `json_req.Action` (contoh: `"InquiryByNoKartu"`).
   - Server Host: `host.name` (contoh: `"prd-gti-vm-cams-camap-bed-02"`).

---

## 3. Catatan Autentikasi Kluster

| Kluster | Host & Port | Akun Terverifikasi | Status |
|---|---|---|---|
| **On-Premise** | `http://172.18.65.100:9200` | `elastic` / `P@ssw0rd2020` | ✅ Terverifikasi & Sampel Dokumen Berhasil Diambil |
| **Cloud** | `http://10.212.224.172:9200` | `elastic` / `P@ssw0rd2026` | ⚠️ Saat pengujian sebelumnya salah ketik menggunakan password On-Premise (`P@ssw0rd2020`). Gunakan `P@ssw0rd2026`. |

---

## 4. Konfigurasi Produksi Rekomendasi (`.env`)

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
