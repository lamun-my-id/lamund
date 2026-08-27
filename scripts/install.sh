#!/bin/sh
# Lamund installer — satu perintah.
#   curl -fsSL https://raw.githubusercontent.com/lamun-my-id/lamund/main/scripts/install.sh | sh
#
# Idempoten: aman dijalankan ulang untuk memutakhirkan binary.
set -eu

REPO="lamund/lamund"
BIN_DIR="/usr/local/bin"
DATA_DIR="/var/lib/lamund"
VERSION="${LAMUND_VERSION:-latest}"

log()  { printf '\033[1;35m›\033[0m %s\n' "$1"; }
err()  { printf '\033[1;31m✗\033[0m %s\n' "$1" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || err "jalankan sebagai root (mis. dengan sudo)."
command -v systemctl >/dev/null 2>&1 || err "butuh systemd (systemctl tidak ditemukan)."

# --- deteksi arch ---
case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) err "arsitektur tidak didukung: $(uname -m)" ;;
esac
log "Arsitektur: linux/${ARCH}"

# --- resolusi versi rilis ---
if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)
  [ -n "$VERSION" ] || err "gagal mendapatkan versi rilis terbaru."
fi
log "Versi: ${VERSION}"

TARBALL="lamund_${VERSION#v}_linux_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

# --- unduh & pasang binary ---
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
log "Mengunduh ${URL}"
curl -fsSL "$URL" -o "$TMP/lamund.tar.gz" || err "unduh gagal."
tar -xzf "$TMP/lamund.tar.gz" -C "$TMP"
install -m 0755 "$TMP/lamund" "$BIN_DIR/lamund"
log "Binary terpasang di ${BIN_DIR}/lamund"

# --- user sistem + direktori data ---
if ! id lamund >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --shell /usr/sbin/nologin lamund 2>/dev/null \
    || useradd --system --home-dir "$DATA_DIR" --shell /bin/false lamund
  log "User sistem 'lamund' dibuat"
fi
mkdir -p "$DATA_DIR/acme"
chown -R lamund:lamund "$DATA_DIR"
chmod 700 "$DATA_DIR"

# --- unit systemd ---
BASE="https://raw.githubusercontent.com/${REPO}/${VERSION}/packaging"
for unit in lamund.service lamund-panel.service; do
  curl -fsSL "${BASE}/${unit}" -o "/etc/systemd/system/${unit}" || err "gagal unduh ${unit}."
done
systemctl daemon-reload
systemctl enable --now lamund.service lamund-panel.service
log "Layanan diaktifkan (data plane :80/:443, panel 127.0.0.1:8080)"

# --- bootstrap admin bila belum ada user ---
if [ "$(sudo -u lamund "$BIN_DIR/lamund" user list --db "$DATA_DIR/lamund.db" 2>/dev/null | grep -c .)" = "0" ]; then
  PW=$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')
  sudo -u lamund "$BIN_DIR/lamund" user create --db "$DATA_DIR/lamund.db" \
    --username admin --password "$PW" --admin >/dev/null
  printf '\n\033[1;32m✔ Lamund siap.\033[0m\n'
  printf '  Panel : http://127.0.0.1:8080  (buka via SSH tunnel: ssh -L 8080:127.0.0.1:8080 <server>)\n'
  printf '  Admin : admin\n'
  printf '  Sandi : %s\n\n' "$PW"
  printf '  Ganti sandi setelah masuk pertama kali.\n'
else
  printf '\n\033[1;32m✔ Lamund dimutakhirkan ke %s.\033[0m\n' "$VERSION"
fi
