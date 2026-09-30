#!/usr/bin/env bash
# ============================================================================
# ONOBILL Auto-Installer
# Sistem Billing ISP & Manajemen MikroTik Multi-Tenant
#
# Pemakaian (Ubuntu/Debian, sebagai root atau dengan sudo):
#   curl -sSL https://raw.githubusercontent.com/DWISSNET/onobill/main/install.sh | sudo bash
#
# atau setelah clone:
#   sudo bash install.sh
# ============================================================================
set -euo pipefail

# ---- Konfigurasi yang bisa dioverride via env -----------------------------
REPO_URL="${ONOBILL_REPO:-https://github.com/DWISSNET/onobill.git}"
INSTALL_DIR="${ONOBILL_DIR:-/opt/onobill}"
APP_PORT="${APP_PORT:-8080}"
GO_VERSION="${GO_VERSION:-1.22.5}"
SERVICE_NAME="onobill"
ADMIN_EMAIL="${ONOBILL_ADMIN_EMAIL:-admin@onobill.local}"
ADMIN_PASS="${ONOBILL_ADMIN_PASS:-admin123}"

log()  { echo -e "\033[1;32m[onobill]\033[0m $*"; }
warn() { echo -e "\033[1;33m[onobill]\033[0m $*"; }
err()  { echo -e "\033[1;31m[onobill]\033[0m $*" >&2; }

[ "$(id -u)" -eq 0 ] || { err "Jalankan sebagai root (atau dengan sudo)."; exit 1; }

# ---- 1. Deteksi OS ---------------------------------------------------------
if [ -f /etc/os-release ]; then . /etc/os-release; else err "OS tidak dikenali"; exit 1; fi
log "OS: ${NAME} ${VERSION_ID}"
case "${ID}" in
  ubuntu|debian) PKG="apt-get" ;;
  *) err "Installer ini mendukung Ubuntu/Debian. OS lain: build manual (lihat README)."; exit 1 ;;
esac

# ---- 2. Install dependensi dasar ------------------------------------------
log "Menginstall dependensi (git, build tools, sqlite)..."
export DEBIAN_FRONTEND=noninteractive
$PKG update -y >/dev/null
$PKG install -y git curl ca-certificates build-essential gcc sqlite3 >/dev/null

# ---- 3. Install Go bila belum ada -----------------------------------------
need_go=1
if command -v go >/dev/null 2>&1; then
  cur="$(go version | awk '{print $3}' | sed 's/go//')"
  log "Go terdeteksi: ${cur}"
  need_go=0
fi
if [ "${need_go}" = "1" ]; then
  log "Menginstall Go ${GO_VERSION}..."
  arch="$(uname -m)"; case "$arch" in
    x86_64) goarch=amd64 ;; aarch64) goarch=arm64 ;; armv7l) goarch=armv6l ;;
    *) err "Arsitektur $arch tidak didukung auto-install Go"; exit 1 ;;
  esac
  tmp="$(mktemp -d)"
  curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-${goarch}.tar.gz" -o "${tmp}/go.tgz"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tmp}/go.tgz"
  rm -rf "${tmp}"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  log "Go terinstall: $(go version)"
fi
export PATH=$PATH:/usr/local/go/bin

# ---- 4. Ambil source code ---------------------------------------------------
if [ -d "${INSTALL_DIR}/.git" ]; then
  log "Repo sudah ada di ${INSTALL_DIR} — menarik update terbaru..."
  git -C "${INSTALL_DIR}" pull --ff-only || warn "git pull gagal, lanjut pakai versi lokal"
else
  log "Meng-clone ${REPO_URL} ke ${INSTALL_DIR}..."
  git clone --depth 1 "${REPO_URL}" "${INSTALL_DIR}"
fi
cd "${INSTALL_DIR}"

# ---- 5. Build binary --------------------------------------------------------
log "Build ONOBILL (CGO untuk SQLite)..."
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o onobill-server ./cmd/onobilling
log "Build selesai: ${INSTALL_DIR}/onobill-server"

# ---- 6. File .env -----------------------------------------------------------
if [ ! -f .env ]; then
  log "Membuat .env default..."
  cat > .env <<EOF
# ONOBILL environment
APP_PORT=${APP_PORT}
BASE_URL=http://localhost:${APP_PORT}
DB_DRIVER=sqlite
DB_PATH=${INSTALL_DIR}/onobill.db
APP_ENV=production
JWT_SECRET=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')

# Payment gateway (opsional — bisa diatur juga dari menu Superadmin > Payment)
# DUITKU_MERCHANT=
# DUITKU_API_KEY=
# DUITKU_BASE_URL=

# Login awal (GANTI setelah login pertama!)
# Admin default: ${ADMIN_EMAIL} / ${ADMIN_PASS}
EOF
else
  log ".env sudah ada — tidak ditimpa."
fi

# ---- 7. systemd service ------------------------------------------------------
log "Membuat systemd service '${SERVICE_NAME}'..."
cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=ONOBILL - Sistem Billing ISP Multi-Tenant
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${INSTALL_DIR}/onobill-server
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable ${SERVICE_NAME} >/dev/null 2>&1 || true
systemctl restart ${SERVICE_NAME}

# ---- 8. Firewall (ufw bila aktif) -------------------------------------------
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
  log "Membuka port ${APP_PORT} di ufw..."
  ufw allow "${APP_PORT}/tcp" >/dev/null || true
fi

# ---- 9. Selesai --------------------------------------------------------------
sleep 2
if systemctl is-active --quiet ${SERVICE_NAME}; then
  IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
  echo
  log "======================================================"
  log " ✅ ONOBILL BERHASIL TERINSTALL & BERJALAN"
  log "======================================================"
  log " URL      : http://${IP:-localhost}:${APP_PORT}"
  log " Login    : ${ADMIN_EMAIL}"
  log " Password : ${ADMIN_PASS}   (GANTI segera!)"
  log " Service  : systemctl status ${SERVICE_NAME}"
  log " Logs     : journalctl -u ${SERVICE_NAME} -f"
  log " Dir      : ${INSTALL_DIR}"
  log "======================================================"
else
  err "Service gagal start. Cek: journalctl -u ${SERVICE_NAME} -n 50"
  exit 1
fi
