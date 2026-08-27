#!/bin/sh
# Postinstall lamund (.deb/.rpm). Dijalankan saat install DAN upgrade — harus
# idempoten. Argumen berbeda antar-distro; kita deteksi "sudah pernah ada" dari
# keberadaan user, bukan dari argumen paket.
set -e

DATA_DIR="/var/lib/lamund"

# 1) user & grup sistem (buat sekali; aman diulang).
if command -v systemd-sysusers >/dev/null 2>&1 && [ -f /usr/lib/sysusers.d/lamund.conf ]; then
  systemd-sysusers >/dev/null 2>&1 || true
fi
if ! id lamund >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin lamund 2>/dev/null \
    || useradd --system --home-dir "$DATA_DIR" --shell /bin/false lamund 2>/dev/null || true
fi

# 2) direktori data (mode ketat) — via tmpfiles bila ada, plus jaminan manual.
if command -v systemd-tmpfiles >/dev/null 2>&1 && [ -f /usr/lib/tmpfiles.d/lamund.conf ]; then
  systemd-tmpfiles --create /usr/lib/tmpfiles.d/lamund.conf >/dev/null 2>&1 || true
fi
mkdir -p "$DATA_DIR/acme" "$DATA_DIR/logs" "$DATA_DIR/sites"
chown -R lamund:lamund "$DATA_DIR" 2>/dev/null || true
chmod 700 "$DATA_DIR"

# 3) muat ulang unit systemd.
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  # Upgrade: restart layanan yang memang sedang berjalan (nol konfigurasi ulang).
  for unit in lamund.service lamund-panel.service; do
    if systemctl is-active --quiet "$unit" 2>/dev/null; then
      systemctl restart "$unit" >/dev/null 2>&1 || true
    fi
  done
fi

# 4) pesan hanya saat install pertama (belum ada DB).
if [ ! -f "$DATA_DIR/lamund.db" ]; then
  cat <<'EOF'

Lamund terpasang. Langkah berikutnya:
  1) Set email ACME (untuk HTTPS) — edit /etc/systemd/system/lamund.service
     atau override: systemctl edit lamund.service  (ganti admin@example.com).
  2) Buat admin awal:
       sudo -u lamund lamund user create --username admin --admin
  3) Nyalakan layanan:
       sudo systemctl enable --now lamund.service lamund-panel.service
  Panel di http://127.0.0.1:8080 (akses via SSH tunnel).

EOF
fi

exit 0
