#!/usr/bin/env bash
# ============================================================================
# ONOBILL — Backup Database Otomatis
# Menyimpan salinan onobill.db (SQLite) secara aman (tidak corrupt saat live),
# dengan rotasi otomatis. Cocok dijalankan via cron.
#
# Pemakaian:
#   bash backup.sh                 # backup sekali
#   ONOBILL_DIR=/opt/onobill bash backup.sh
#
# Cron harian jam 03:00:
#   0 3 * * * /opt/onobill/backup.sh >> /var/log/onobill-backup.log 2>&1
# ============================================================================
set -euo pipefail

ONOBILL_DIR="${ONOBILL_DIR:-/opt/onobill}"
DB_PATH="${DB_PATH:-${ONOBILL_DIR}/onobill.db}"
BACKUP_DIR="${BACKUP_DIR:-${ONOBILL_DIR}/backups}"
KEEP="${BACKUP_KEEP:-14}"   # simpan N backup terakhir

log(){ echo -e "\033[1;32m[backup]\033[0m $*"; }
err(){ echo -e "\033[1;31m[backup]\033[0m $*" >&2; }

[ -f "${DB_PATH}" ] || { err "DB tidak ditemukan: ${DB_PATH}"; exit 1; }
mkdir -p "${BACKUP_DIR}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="${BACKUP_DIR}/onobill-${TS}.db"

# Backup konsisten untuk SQLite live DB (online backup API via .backup).
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 "${DB_PATH}" ".backup '${OUT}'"
else
  # Fallback: checkpoint WAL lalu copy
  cp -f "${DB_PATH}" "${OUT}"
  [ -f "${DB_PATH}-wal" ] && cp -f "${DB_PATH}-wal" "${OUT}-wal" || true
  [ -f "${DB_PATH}-shm" ] && cp -f "${DB_PATH}-shm" "${OUT}-shm" || true
fi

# Kompres agar hemat ruang
gzip -f "${OUT}"
log "Backup dibuat: ${OUT}.gz ($(du -h "${OUT}.gz" | cut -f1))"

# Rotasi: hapus yang lebih tua dari KEEP terakhir
ls -1t "${BACKUP_DIR}"/onobill-*.db.gz 2>/dev/null | tail -n +$((KEEP+1)) | while read -r old; do
  rm -f "$old" && log "Rotasi: hapus $(basename "$old")"
done

log "Selesai. Total backup tersimpan: $(ls -1 "${BACKUP_DIR}"/onobill-*.db.gz 2>/dev/null | wc -l)"
