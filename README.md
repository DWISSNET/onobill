# ONOBILL — Sistem Billing ISP & Manajemen MikroTik Multi-Tenant

Platform billing ISP berbasis web (SaaS multi-tenant) yang mengintegrasikan manajemen
pelanggan, tagihan, pembayaran otomatis, dan kontrol router MikroTik dalam satu sistem.
Dibangun dengan **Go** — satu binary, tanpa dependency berat.

> 📘 Lihat [PRD.md](PRD.md) untuk dokumen produk lengkap.

---

## ✨ Fitur

- 📊 **Dashboard** — Statistik pelanggan, pendapatan, invoice jatuh tempo
- 👥 **Pelanggan** — CRUD, PPPoE & Hotspot, isolir/aktifkan langsung ke MikroTik
- 💰 **Billing** — Invoice otomatis berulang, reminder H-3, auto-isolir saat overdue, auto-aktif setelah bayar
- 📦 **Paket** — Profil layanan + sinkronisasi PPPoE profile ke router
- 📡 **Router MikroTik** — Onboarding copas 1 script (auto check-in), test koneksi, status & last-seen
- 🔒 **VPN / CHR** — L2TP overlay (`10.99.0.0/24`) via MikroTik CHR, monitoring tunnel
- 🎫 **Voucher Hotspot** — Generate massal & push ke router
- 🔀 **Port Forward** — dst-nat di CHR (`IP_CHR:port → router:port`), juga untuk jalur API router beda jaringan
- 💳 **Payment Gateway** — Duitku & KlikQRIS (QRIS/VA/e-wallet), webhook → invoice lunas otomatis, konfigurasi dari UI tanpa restart
- 📣 **Notifikasi** — WhatsApp gateway, SMTP email, Telegram; broadcast ke semua tenant
- 🏢 **Multi-Tenant** — Satu server untuk banyak ISP, data terisolasi penuh
- 👑 **Area CEO (Superadmin)** — Kelola tenant, CHR, notifikasi platform, payment gateway

## 🧰 Tech Stack

- **Backend**: Go 1.22+ (stdlib `net/http`, routing pattern Go 1.22)
- **Database**: SQLite (default, zero-config) / MySQL — via GORM
- **MikroTik**: RouterOS API (`go-routeros/v3`)
- **Frontend**: Server-side rendered HTML (`html/template`)
- **Auth**: Session cookie + role-based access (superadmin/admin)

## 🚀 Instalasi Cepat (Auto-Install)

Satu perintah di **Ubuntu/Debian** (mesin baru, sebagai root):

```bash
curl -sSL https://raw.githubusercontent.com/DWISSNET/onobill/main/install.sh | sudo bash
```

Installer otomatis: install Go bila perlu → clone → build → buat `.env` →
buat systemd service `onobill` → start → tampilkan URL & kredensial awal.

Override lewat env bila perlu:
```bash
APP_PORT=9000 ONOBILL_DIR=/srv/onobill \
  curl -sSL https://raw.githubusercontent.com/DWISSNET/onobill/main/install.sh | sudo -E bash
```

## 🛠️ Build Manual

```bash
git clone https://github.com/DWISSNET/onobill.git
cd onobill
go mod download
CGO_ENABLED=1 go build -o onobill-server ./cmd/onobilling/
./onobill-server
```

Buka **http://localhost:8080** — login default:
- **Email**: `admin@onobill.local`  •  **Password**: `admin123`

> ⚠️ **Ganti password default segera setelah login pertama!**

## ⚙️ Konfigurasi (env)

| Variabel | Default | Keterangan |
|---|---|---|
| `APP_PORT` | `8080` | Port HTTP |
| `BASE_URL` | `http://localhost:8080` | URL publik (callback payment & onboard router) |
| `JWT_SECRET` | random | Secret sesi — **set di production!** |
| `DB_DRIVER` | `sqlite` | `sqlite` atau `mysql` |
| `DB_PATH` | `./onobill.db` | Path SQLite |
| `DB_HOST/PORT/USER/PASSWORD/NAME` | — | Untuk MySQL |
| `DUITKU_MERCHANT/API_KEY/BASE_URL` | — | Duitku (atau atur via UI Superadmin) |

## 🔌 Menghubungkan Router MikroTik

1. Menu **Router → + Tambah Router** → salin script provisioning.
2. Paste script di terminal MikroTik → router otomatis terdaftar (check-in).
3. Bila server beda jaringan dengan router: gunakan **CHR gateway** —
   ONOBILL membuat dst-nat di CHR (`IP_CHR:port → IP_VPN_router:8728`) via menu **Port Forward**,
   lalu arahkan host router ke `IP_CHR:port` tersebut.

## 💳 Payment Gateway (Duitku)

Menu **Superadmin → Payment**: isi Merchant Code + API Key, pilih Sandbox/Produksi,
aktifkan. Daftarkan webhook `{{BASE_URL}}/webhooks/payment/duitku` di dashboard Duitku.
Perubahan berlaku **langsung tanpa restart**.

## 📁 Struktur Proyek

```
onobill/
├── cmd/onobilling/          # Entry point
├── internal/
│   ├── config/              # Config, DB, seed, migrate
│   ├── domain/              # Model GORM
│   ├── repository/          # Akses data
│   ├── service/             # billing, customer, router, chr, payment,
│   │                        # isolation, mikrosync, notify, vpn, subscription...
│   └── transport/http/      # handlers, routes, render
├── web/templates/           # HTML (layout + pages)
├── PRD.md                   # Dokumen produk
└── install.sh               # Auto-installer
```

## 💾 Backup & Restore Database

**Auto-backup harian** terpasang otomatis oleh `install.sh` (cron jam 03:00) ke
`backups/` dengan rotasi 14 hari. Backup manual kapan saja:

```bash
bash backup.sh                          # backup sekali → backups/onobill-<tgl>.db.gz
```

Pulihkan dari backup:

```bash
bash restore.sh backups/onobill-20240930-030000.db.gz   # stop service, restore, start
```

`restore.sh` otomatis mengamankan DB aktif sebagai `*.pre-restore-*` sebelum menimpa.

## 🔐 Catatan Keamanan (Production)

1. Set `JWT_SECRET` kuat  2. Ganti password admin default  3. HTTPS via reverse proxy
4. Backup DB berkala  5. Simpan API key payment hanya via UI (tidak di-commit)

## 🗺️ Roadmap

- [ ] Halaman self-service pelanggan (cek & bayar tagihan)
- [ ] Laporan keuangan & export (PDF/Excel)
- [ ] Payment gateway tambahan (Midtrans/Xendit)
- [ ] Grafik traffic router via RouterOS API
- [ ] Docker image & docker-compose
- [ ] Auto-backup database terjadwal

## Lisensi

MIT
