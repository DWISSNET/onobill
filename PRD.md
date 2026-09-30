# 📘 PRD — ONOBILL

**Product Requirements Document**
**Produk:** ONOBILL — Sistem Billing ISP & Manajemen MikroTik Multi-Tenant
**Versi Dokumen:** 1.0
**Status:** Aktif / Dalam Pengembangan
**Stack:** Go (Golang) + GORM + SQLite/MySQL + RouterOS API + HTML Templates

---

## 1. Ringkasan Produk

**ONOBILL** adalah platform billing ISP berbasis web (SaaS multi-tenant) yang mengintegrasikan
manajemen pelanggan, tagihan, dan kontrol router MikroTik dalam satu sistem. Dirancang untuk
pemilik usaha internet (RT/RW-net, WISP kecil-menengah) agar bisa menjalankan bisnis ISP
tanpa keahlian teknis jaringan yang mendalam.

### Masalah yang Diselesaikan
- Penagihan pelanggan ISP masih manual (catatan/Excel/chat).
- Isolir pelanggan nunggak harus login ke MikroTik satu per satu.
- Tidak ada sistem voucher hotspot & PPPoE yang terintegrasi dengan pembayaran.
- Pemilik multi-router/multi-lokasi sulit memantau semuanya dari satu tempat.
- Pembayaran pelanggan tidak otomatis mengaktifkan layanan.

### Solusi
Satu dashboard web untuk: kelola pelanggan → terbitkan invoice otomatis → terima pembayaran
otomatis via payment gateway → pelanggan aktif otomatis → nunggak → isolir otomatis di MikroTik.

---

## 2. Tujuan & Sasaran

| Tujuan | Ukuran Keberhasilan |
|---|---|
| Otomasi billing end-to-end | 90% invoice & isolir tanpa sentuhan admin |
| Multi-tenant SaaS | 1 server melayani banyak ISP (tenant) terisolasi |
| Kontrol MikroTik terpusat | Semua router terkelola dari 1 panel |
| Pembayaran cashless | Pelanggan bayar QRIS/VA, layanan aktif otomatis |
| Skalabilitas router | Onboarding router < 5 menit (copas 1 script) |

---

## 3. Peran Pengguna (Persona)

### 3.1 Superadmin (Pemilik Platform / "CEO")
- Mengelola tenant (ISP pelanggan ONOBILL) & langganan mereka.
- Mengelola server CHR (MikroTik CHR sebagai gateway VPN pusat).
- Konfigurasi notifikasi platform (WA/Email/Telegram) & payment gateway.
- Broadcast pengumuman ke semua tenant.
- Menerima pendapatan dari langganan tenant.

### 3.2 Admin Tenant (Pemilik ISP)
- Mengelola pelanggan, paket, tagihan, router, voucher, VPN, port forward miliknya.
- Menerima pembayaran dari pelanggannya.
- Data tenant **terisolasi penuh** dari tenant lain.

### 3.3 Pelanggan Akhir (End Customer)
- Menerima invoice & notifikasi (WA/email).
- Membayar via QRIS/VA/e-wallet (Duitku/KlikQRIS).
- Layanan aktif otomatis setelah bayar; terisolir otomatis bila nunggak.

---

## 4. Ruang Lingkup Fitur

### 4.1 Modul Dashboard
- Statistik: jumlah pelanggan aktif/terisolir, pendapatan, invoice jatuh tempo.
- Pembayaran terbaru (5 terakhir).
- Status router & tunnel VPN.

### 4.2 Modul Pelanggan
- CRUD pelanggan (nama, telepon, alamat, tipe layanan PPPoE/Hotspot).
- Kredensial PPPoE/Hotspot (username/password).
- Penugasan router & paket.
- Ubah status manual: **aktif / isolir** (langsung dieksekusi ke MikroTik).
- Sinkronisasi secret PPPoE/hotspot user ke router.

### 4.3 Modul Billing
- Invoice otomatis berulang per pelanggan sesuai paket & siklus.
- Status invoice: `unpaid / paid / overdue`.
- Pembayaran manual (admin) & otomatis (payment gateway webhook).
- Reminder H-3 jatuh tempo & notifikasi overdue (WA/email).
- **Auto-isolir**: invoice overdue → isolir pelanggan otomatis.
- **Auto-aktif**: pembayaran sukses → invoice lunas → pelanggan aktif lagi.

### 4.4 Modul Paket
- CRUD paket layanan (harga, kecepatan, siklus billing).
- Sinkronisasi PPPoE profile ke router MikroTik.

### 4.5 Modul Router MikroTik
- Tambah router via **provisioning script** (copas 1 script ke terminal MikroTik):
  - Script membuat user API, konfigurasi dasar, dan **check-in otomatis** ke ONOBILL (`/api/onboard/{token}`).
  - Check-in idempotent (script bisa dipaste ulang).
- Auto-detect IP VPN router dari CHR saat onboard lewat tunnel.
- Test koneksi API (host:port, dengan normalisasi alamat `RouterAddr`).
- Status aktif & *last seen*.
- Arsitektur akses router (beda jaringan): **via CHR gateway** — dst-nat port API per router di CHR.

### 4.6 Modul VPN (L2TP)
- CHR sebagai L2TP server pusat; router tenant sebagai L2TP client (overlay `10.99.0.0/24`).
- Pembuatan secret L2TP per router di CHR.
- Monitoring jumlah tunnel aktif dari CHR.

### 4.7 Modul Voucher Hotspot
- Generate voucher massal (kode, durasi, harga, profil).
- Push hotspot user ke router MikroTik.
- Status voucher (tersedia/terpakai).

### 4.8 Modul Port Forward
- CRUD dst-nat rule di CHR: `IP_CHR:public_port → IP_router:port_tujuan`.
- Validasi port bentrok di firewall CHR.
- Dipakai juga untuk **jalur API router** (router yang tidak bisa dijangkau langsung).

### 4.9 Area CEO (Superadmin)
- **Tenant**: CRUD tenant & admin tenant; status langganan; suspend otomatis bila nunggak langganan.
- **Superadmin**: manajemen akun superadmin.
- **CHR**: CRUD server CHR; probe status (online, uptime, versi, jumlah tunnel); toggle aktif; cari IP client L2TP.
- **Notifikasi**: engine WA gateway + SMTP email + Telegram milik platform; test kirim; broadcast ke semua tenant.
- **Payment Gateway**: konfigurasi Duitku & KlikQRIS (merchant, API key, sandbox/produksi), tersimpan di DB, berlaku **live tanpa restart**.

### 4.10 Payment Gateway
- Interface `Gateway` pluggable (Duitku, KlikQRIS; mudah ditambah).
- Buat transaksi (QRIS default `SQ`, VA, e-wallet) dengan signature md5 Duitku.
- Webhook `POST /webhooks/payment/{gateway}`: verifikasi signature → invoice lunas → aktifkan pelanggan → notifikasi.
- Konfigurasi dari env var **dan/atau** DB (UI Superadmin menimpa env).

### 4.11 Notifikasi
- Multi-channel: WhatsApp gateway, SMTP email, Telegram.
- Event: invoice baru, pembayaran diterima, reminder H-3, overdue/terisolir.
- Log notifikasi per tenant (sukses/gagal + error).

### 4.12 Akun & Keamanan
- Login session-based; role `superadmin / admin`.
- Middleware `RequireAuth` + `RequireSuperadmin`.
- Tenant suspension gate (tenant nunggak langganan → akses dibatasi).
- Ganti password.
- Onboarding router tanpa sesi — divalidasi **token onboarding** tenant.

### 4.13 Background Engine
- **mikrosync**: auto-sync PPPoE profile/secret, hotspot user, voucher ke router secara periodik + event-driven.
- **auto-isolir**: job isolir invoice overdue.
- **subscription**: penagihan langganan tenant → suspend otomatis.

---

## 5. Arsitektur Teknis

```
┌─────────────────────────────────────────────────────────────┐
│                     ONOBILL Server (Go)                     │
│  HTTP handlers → services → repository (GORM) → SQLite/MySQL│
│                                                             │
│  Payment: Duitku / KlikQRIS  ── webhook ──► auto-lunas      │
│  Notify:  WA / SMTP / Telegram                              │
└──────────┬──────────────────────────────┬───────────────────┘
           │ RouterOS API (8728)           │ RouterOS API
           ▼                               ▼
   ┌───────────────┐   L2TP overlay   ┌──────────────┐
   │  CHR Server   │◄════════════════►│ Router Tenant│
   │ (103.x publik)│   10.99.0.0/24   │ (MikroTik)   │
   └──────┬────────┘                  └──────────────┘
          │ dst-nat IP_CHR:port → 10.99.0.x:8728 (jalur API)
          ▼
   Port Forward publik pelanggan
```

### Keputusan Desain Penting
- **Akses router beda jaringan** → lewat CHR gateway (dst-nat), bukan langsung ke IP VPN.
- **Normalisasi alamat router** terpusat (`domain.RouterAddr`) — host boleh berisi port.
- **Onboarding token-based** tanpa login — aman & mudah (copas script).
- **Idempotency** di semua operasi router (script/aksi boleh diulang).
- **Konfigurasi payment di DB** → superadmin ubah dari UI tanpa restart.

---

## 6. Model Data Utama

| Entitas | Kolom Kunci |
|---|---|
| Tenant | id, name, slug, status |
| User | id, tenant_id, email, password, role |
| Customer | id, tenant_id, name, phone, service_type, username, password, router_id, package_id, status |
| Package | id, tenant_id, name, price, speed, cycle |
| Invoice | id, tenant_id, customer_id, amount, due_date, status |
| Payment | id, invoice_id, amount, method, gateway, paid_at |
| Router | id, tenant_id, name, host, port, username, password, vpn_ip, is_active, last_seen |
| CHRServer | id, name, host, api_port, username, password, l2tp_range, is_active |
| Voucher | id, tenant_id, code, profile, price, status |
| PortForward | id, router_id, public_port, dest_ip, dest_port, purpose |
| NotifSetting | tenant_id, WA/SMTP/Telegram config, event toggles |
| PaymentGatewaySetting | tenant_id, Duitku (merchant/apikey/sandbox), KlikQRIS |
| AuditLog / NotifLog / Job | jejak audit & antrian |

---

## 7. API & Endpoint Kunci

| Endpoint | Fungsi |
|---|---|
| `POST /login` | Login admin/superadmin |
| `GET /api/onboard/{token}` | Check-in router (tanpa auth, token tenant) |
| `POST /routers/{id}/test` | Test koneksi API router |
| `POST /customers/{id}/status` | Isolir/aktifkan pelanggan (eksekusi ke MikroTik) |
| `POST /webhooks/payment/{gateway}` | Callback pembayaran → invoice lunas |
| `GET/POST /superadmin/payment` | Konfigurasi payment gateway |
| `GET/POST /superadmin/notify*` | Notifikasi platform + broadcast |
| `GET/POST /superadmin/chr*` | Kelola CHR |
| `GET/POST /portfwd` | Port forward di CHR |

---

## 8. Non-Fungsional

- **Bahasa UI:** Bahasa Indonesia.
- **Timeout API router:** 6–10 detik; kegagalan router tidak memblokir status DB (retry-able).
- **TLS opsional** untuk API-SSL (8729) dengan dukungan self-signed.
- **DB:** SQLite (default, zero-config) atau MySQL via env.
- **Deploy:** 1 binary Go + folder `web/templates`; auto-migrate saat boot.

---

## 9. Instalasi (Auto-Install)

Instalasi satu perintah di Ubuntu/Debian (mesin baru):

```bash
curl -sSL https://raw.githubusercontent.com/<org>/onobill/main/install.sh | sudo bash
```

Installer melakukan: cek/install Go bila perlu → clone/download release → build →
buat file `.env` → buat systemd service `onobill` → start → tampilkan URL & kredensial awal.
(Lihat `install.sh` dan `README.md` di repo.)

---

## 10. Roadmap

**Selesai (v1):** dashboard, pelanggan, billing, paket, router provisioning, VPN/CHR,
voucher, port forward, notifikasi, isolir otomatis, payment gateway Duitku/KlikQRIS,
multi-tenant + area superadmin, CHR gateway untuk akses router.

**Berikutnya:**
- [ ] Halaman pelanggan self-service (cek tagihan & bayar sendiri).
- [ ] Laporan keuangan & export (PDF/Excel).
- [ ] Payment gateway tambahan (Midtrans/Xendit).
- [ ] Monitoring traffic router (grafik) via RouterOS API.
- [ ] Mode L2TP client langsung di server ONOBILL (alternatif CHR gateway).
- [ ] Docker image & docker-compose.
- [ ] Auto-backup database terjadwal.

---

## 11. Risiko & Mitigasi

| Risiko | Mitigasi |
|---|---|
| Server beda jaringan dengan router | CHR gateway (dst-nat) — sudah diimplementasi |
| CHR down → semua router tak terjangkau | Multi-CHR (roadmap), probe status CHR |
| Kredensial gateway bocor | Simpan di DB, field password masked, tidak ditampilkan ulang |
| Webhook palsu | Verifikasi signature md5 per gateway |
| Script onboard disalahgunakan | Token unik per tenant, bisa di-rotate |

---

*Dokumen ini hidup — diperbarui seiring perkembangan fitur ONOBILL.*
