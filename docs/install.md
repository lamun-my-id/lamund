# Install

## Paket .deb / .rpm (idempoten — install & upgrade)

Setiap rilis menyediakan paket `.deb` (Debian/Ubuntu) dan `.rpm` (Fedora/RHEL).
Cara ini paling rapi: **package manager** yang mengurus "sudah terpasang →
lewati / upgrade ke versi baru", membuat user & direktori, dan memuat unit.

```sh
# Debian/Ubuntu
curl -fsSLO https://github.com/lamun-my-id/lamund/releases/latest/download/lamund_<versi>_linux_amd64.deb
sudo apt install ./lamund_<versi>_linux_amd64.deb   # atau: sudo dpkg -i …

# upgrade nanti = install paket versi baru; apt otomatis mengganti yang lama.
```

Setelah pasang: set email ACME (di unit), buat admin
(`sudo -u lamund lamund user create --username admin --admin`), lalu
`sudo systemctl enable --now lamund.service lamund-panel.service`.

> Runtime bahasa (Node, Python, dst.) **tidak** ikut terpasang — hanya perlu
> jika kamu menjalankan aplikasi di bahasa itu, dan dikelola per-aplikasi.

## One-line installer (alternatif)

On a fresh Ubuntu/Debian VPS with systemd, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/lamun-my-id/lamund/main/scripts/install.sh | sh
```

The script:

1. Detects your CPU architecture (`amd64` or `arm64`).
2. Downloads the latest release binary to `/usr/local/bin/lamund`.
3. Creates the `lamund` system user and `/var/lib/lamund` (mode `0700`).
4. Installs and enables two systemd units.
5. Bootstraps an admin account and prints a one-time password.

Pin a version with `LAMUND_VERSION=v0.1.0` in front of the command. Re-run
the installer any time to upgrade — it is idempotent and preserves your
data and admin account.

## What gets installed

| Path | Purpose |
|------|---------|
| `/usr/local/bin/lamund` | the binary |
| `/var/lib/lamund/` | data directory (DB, JWT secret, ACME certs) — mode `0700` |
| `/etc/systemd/system/lamund.service` | data plane (`:80`/`:443`) |
| `/etc/systemd/system/lamund-panel.service` | panel + REST (loopback `:8080`) |

## Running unprivileged

Binding `:80`/`:443` normally needs root. Lamund instead runs as the
`lamund` user with `AmbientCapabilities=CAP_NET_BIND_SERVICE`, so it can
bind low ports without any other privilege. The units also enable systemd
hardening (`ProtectSystem=strict`, `NoNewPrivileges`, a restricted
`ReadWritePaths`).

## Reaching the panel

The panel listens on `127.0.0.1:8080` only. Open it through an SSH tunnel:

```sh
ssh -L 8080:127.0.0.1:8080 you@your-server
# open http://127.0.0.1:8080
```

To expose it publicly, add a Lamund proxy site in front of it (for example
`panel.example.com` → `http://127.0.0.1:8080`) so it gets HTTPS too.

## Authoritative DNS (opsional)

Lamund bisa menjadi **authoritative DNS** untuk domain yang kamu delegasikan
ke server ini. Fitur ini **mati secara default** dan hanya menyala bila kamu
memberi flag `--dns`. Ada dua cara menaruh domain di server:

**Cara 1 — cukup tunjuk A record (tanpa fitur DNS).** Di penyedia DNS domainmu
sekarang (registrar / Cloudflare), tambahkan `sub.domainmu.com A <IP server>`.
Reverse-proxy + auto-HTTPS Lamund langsung melayaninya. Tidak butuh apa pun di
bawah ini.

**Cara 2 — delegasikan domain ke Lamund.** Set **nameserver** domainmu ke
nameserver instance ini (mis. `ns1.lamund.my.id`, `ns2.lamund.my.id`). Lamund
lalu authoritative → kelola semua record di panel `/domain`. Perlu langkah
setup sekali di bawah.

### 1. Aktifkan listener DNS

Tambahkan `--dns` (bind ke IP publik spesifik, bukan `:53`, agar tak bentrok
`systemd-resolved`) ke unit `lamund.service`, lalu buka port 53:

```ini
# /etc/systemd/system/lamund.service — pada ExecStart data plane:
ExecStart=/usr/local/bin/lamund serve --http :80 --https :443 \
  --public-ip <IP-PUBLIK> --dns <IP-PUBLIK>:53
# CAP_NET_BIND_SERVICE sudah aktif (lihat "Running unprivileged").
```

```sh
# buka UDP+TCP 53 di firewall host
ufw allow 53/udp && ufw allow 53/tcp
# di cloud (mis. Oracle) tambahkan juga ingress 53 udp+tcp di security list.
```

Bila kamu butuh bind `0.0.0.0:53`, matikan stub listener resolved dulu:
set `DNSStubListener=no` di `/etc/systemd/resolved.conf` lalu
`systemctl restart systemd-resolved`.

Beri flag `--public-ip <IP>` yang sama ke `lamund-panel.service` juga, agar
panel menampilkan IP glue nameserver dengan benar.

### 2. Setel nameserver instance (sekali)

Di panel: **Pengaturan → Nameserver DNS** (superadmin) isi `ns1`, `ns2`
(mis. `ns1.lamund.my.id`, `ns2.lamund.my.id`). Lalu di registrar **domain
infrastruktur** kamu (yang memiliki `lamund.my.id`):

1. Buat glue/host record: `ns1.lamund.my.id → <IP>`, `ns2.lamund.my.id → <IP>`.
2. Delegasikan domain infrastruktur itu ke nameserver-nya sendiri.

Satu domain infrastruktur melayani tak-terbatas domain — tidak perlu domain
kedua per pengguna.

### 3. Delegasikan domain pengguna

Di registrar domain yang mau dilayani, ganti **Nameserver** menjadi
`ns1.lamund.my.id` & `ns2.lamund.my.id`. Setelah propagasi, buat zona-nya di
panel `/domain` dan kelola record di sana.

### Wildcard cert (DNS-01)

Saat `--dns` aktif dan sebuah domain **didelegasikan** ke Lamund (zona ada di
panel `/domain`), Lamund menerbitkan sertifikat **wildcard `*.zona`** secara
otomatis lewat ACME **DNS-01** — Lamund memasang TXT `_acme-challenge` sendiri
di zonanya, jadi tak perlu tantangan HTTP per-subdomain. Sekali terbit, semua
subdomain baru (`a.zona`, `b.zona`, …) langsung ber-HTTPS tanpa terbit ulang.

Domain yang **hanya menunjuk A record** (tak didelegasikan) tetap memakai
HTTP-01 seperti biasa. Tak ada konfigurasi tambahan; syaratnya cuma zona sudah
**propagate** (ACME memverifikasi TXT lewat nameserver publik).

## Service management

```sh
systemctl status lamund lamund-panel
journalctl -u lamund -f          # data plane logs
systemctl restart lamund-panel   # after config changes
```

## Uninstall

```sh
systemctl disable --now lamund lamund-panel
rm /etc/systemd/system/lamund.service /etc/systemd/system/lamund-panel.service
rm /usr/local/bin/lamund
# remove data (irreversible): rm -rf /var/lib/lamund && userdel lamund
```
