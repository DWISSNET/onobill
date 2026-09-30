#!/usr/bin/env bash
# ============================================================================
# ONOBILL — Restore Database dari Backup
#
# Pemakaian:
#   bash restore.sh backups/onobill-20240930-030000.db.gz
#
# PERHATIAN: menimpa database aktif. Service akan di-stop sementara.
# ============================================================================
set -euo pipefail

ONOBILL_DIR="${ONOBILL_DIR:-/opt/onobill}"
DB_PATH="${DB_PATH:-${ONOBILL_DIR}/onobill.db}"
SERVICE_NAME="${SERVICE_NAME:-onobill}"

log(){ echo -e "\033[1;32m[restore]\033[0m $*"; }
err(){ echo -e "\033[1;31m[restore]\033[0m $*" >&2; }

[ $# -ge 1 ] || { err "Usage: bash restore.sh <file-backup.db.gz>"; exit 1; }
SRC="$1"
[ -f "${SRC}" ] || { err "File backup tidak ada: ${SRC}"; exit 1; }

# Stop service bila systemd tersedia
if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files | grep -q "${SERVICE_NAME}.service"; then
  log "Menghentikan service ${SERVICE_NAME}..."
  systemctl stop "${SERVICE_NAME}" || true
  STOPPED=1
fi

# Backup DB saat ini dulu (pengaman)
if [ -f "${DB_PATH}" ]; then
  cp -f "${DB_PATH}" "${DB_PATH}.pre-restore-$(date +%Y%m%d-%H%M%S)"
  log "DB aktif diamankan sebagai *.pre-restore-*"
fi

TMP="$(mktemp)"
case "${SRC}" in
  *.gz) gunzip -c "${SRC}" > "${TMP}" ;;
  *)    cp -f "${SRC}" "${TMP}" ;;
esac

# Validasi file sqlite
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 "${TMP}" "PRAGMA integrity_check;" | grep -q "ok" || { err "File backup corrupt / bukan SQLite valid."; rm -f "${TMP}"; exit 1; }
fi

mv -f "${TMP}" "${DB_PATH}"
rm -f "${DB_PATH}-wal" "${DB_PATH}-shm"
chmod 600 "${DB_PATH}" || true
log "Database dipulihkan dari $(basename "${SRC}")"

if [ "${STOPPED:-0}" = "1" ]; then
  systemctl start "${SERVICE_NAME}"
  log "Service ${SERVICE_NAME} dinyalakan kembali."
fi
log "✅ Restore selesai."
