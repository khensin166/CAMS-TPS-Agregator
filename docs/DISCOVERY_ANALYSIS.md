# CAMS TPS Monitoring — Elasticsearch Discovery & Index Analysis

Captured on: `2026-11-13`  
Source On-Premise: `172.18.65.100:9200` (Cluster: `cao-cluster`, ES v7.7.0)  
Source Cloud: `10.212.224.172:9200`  

---

## 1. Analisis Perbandingan Indeks On-Premise vs Cloud

| Kategori Index | Pola Index | On-Premise Volume / Hari | Cloud Volume / Hari | Status & Rekomendasi |
|---|---|---|---|---|
| **CAMS Service** | `cams-service-YYYY.MM.DD` | **~3 - 6 Juta docs** (13 - 19 GB) | **~800k - 1.6 Juta docs** (3 - 6 GB) | ⭐ **Kandidat Utama Terkuat** (Aktif di On-Prem & Cloud) |
| **CAMS Apps** | `cams-apps-YYYY.MM.DD` | ~7 Juta docs (25 - 30 GB) | Tidak aktif (~200 docs di Okt) | Opsional (Hanya On-Prem aktif) |
| **CAMS Load Balancer** | `cams-lb-YYYY.MM.DD` | ~6.5 - 7.5 Juta docs (8 - 10 GB) | Tidak ada | Layer gateway / network On-Prem |
| **Credit Card Way4** | `cc_way4_transaction_flat-*` | ~150k - 250k docs (1.5 - 3.5 GB) | Tidak ada | Khusus log kartu kredit / Way4 |
| **CDC Core/Mobile Banking**| `cdc_logdat_mblog-*` | > 170 Juta docs (400+ GB) | Tidak ada | Terlalu masif (Database CDC) |

---

## 2. Temuan Kunci Arsitektur

### 2.1 Keselarasan Indeks: `cams-service-*`
Indeks **`cams-service-YYYY.MM.DD`** adalah satu-satunya indeks transaksi inti yang **secara konsisten aktif setiap hari di KEDUA lingkungan**:
- **On-Premise**: `cams-service-2026.11.13` (2.902.187 dokumen, 13.5 GB).
- **Cloud**: `cams-service-2026.11.13` (835.428 dokumen, 3.0 GB).

Ini sangat cocok dengan arsitektur **Dual Independent Workers**:
- **Worker A (On-Premise)**: `ONPREM_INDEX_PATTERN=cams-service-*`
- **Worker B (Cloud)**: `CLOUD_INDEX_PATTERN=cams-service-*`

### 2.2 Verifikasi Masa Retensi Indeks (ILM)
- **On-Premise**: Indeks tertua adalah `2026.11.03` (~10 hari retensi). Ini mengonfirmasi bahwa On-Premise menerapkan kebijakan retensi ketat (~7–10 hari).
- **Cloud**: Indeks tersimpan mulai `2026.10.05` (~40 hari retensi).

---

## 3. Langkah Berikutnya: Menentukan Mapping Field

Untuk memastikan field waktu dan kode sukses/gagal pada `cams-service-*`, kita perlu mengambil **1 contoh dokumen** dari On-Premise dan Cloud:

### Perintah Cek Contoh Dokumen On-Premise:
```bash
curl --noproxy "*" -u "elastic:P@ssw0rd2020" "http://172.18.65.100:9200/cams-service-2026.11.13/_search?size=1&pretty"
```

### Perintah Cek Contoh Dokumen Cloud:
```bash
curl --noproxy "*" -u "elastic:P@ssw0rd2026" "http://10.212.224.172:9200/cams-service-2026.11.13/_search?size=1&pretty"
```
