#!/bin/sh
# Postremove lamund. Reload systemd; JANGAN hapus /var/lib/lamund (berisi DB,
# sertifikat, secret) — data user tak boleh hilang saat uninstall biasa.
set -e

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi

case "${1:-}" in
  purge|0) : ;; # bahkan saat purge kita tetap simpan data; hapus manual bila mau.
esac
exit 0
