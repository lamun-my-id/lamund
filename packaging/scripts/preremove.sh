#!/bin/sh
# Preremove lamund. Saat UPGRADE, package manager memanggil ini juga — jangan
# disable di situ. Kita hanya hentikan+disable saat benar-benar dihapus.
set -e

# Deteksi upgrade vs remove lintas-distro:
#   deb  : arg1 = "upgrade" saat upgrade, "remove" saat hapus.
#   rpm  : arg1 = "1" saat upgrade, "0" saat hapus.
case "${1:-}" in
  upgrade|1) exit 0 ;; # upgrade: biarkan layanan; postinstall yang restart.
esac

if command -v systemctl >/dev/null 2>&1; then
  for unit in lamund.service lamund-panel.service; do
    systemctl disable --now "$unit" >/dev/null 2>&1 || true
  done
fi
exit 0
