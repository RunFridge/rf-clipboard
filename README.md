# rf-clipboard

[![ci](https://github.com/RunFridge/rf-clipboard/actions/workflows/ci.yml/badge.svg)](https://github.com/RunFridge/rf-clipboard/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/RunFridge/rf-clipboard)](https://github.com/RunFridge/rf-clipboard/releases/latest)
[![go](https://img.shields.io/github/go-mod/go-version/RunFridge/rf-clipboard)](go.mod)
[![license](https://img.shields.io/github/license/RunFridge/rf-clipboard)](LICENSE)

English | [한국어](docs/README_ko.md)

A shared clipboard for the CLI, synced across your UNIX-like devices through a
self-hosted server. Contents are encrypted on the client with a key the server
never sees — the server stores only ciphertext under an anonymous account ID.

```sh
# on machine A
echo 'hello world' | rf-clip

# on machine B
rf-paste > file.txt
```

## Install (client)

Any of:

```sh
# installer script (downloads the latest GitHub release)
curl -fsSL https://raw.githubusercontent.com/RunFridge/rf-clipboard/main/scripts/install.sh | sh

# go install (then symlink rf-copy/rf-paste, or use `rf-clip paste`)
go install github.com/RunFridge/rf-clipboard/cmd/rf-clip@latest
ln -s "$(command -v rf-clip)" ~/.local/bin/rf-copy
ln -s "$(command -v rf-clip)" ~/.local/bin/rf-paste

# or grab a binary from the releases page
```

The installer creates both symlinks: `rf-copy` is an alias of `rf-clip`
(they both copy stdin), `rf-paste` pastes.

## Setup

```sh
rf-clip init
```

This uses the hosted server at `https://clip.runfridge.dev`. Self-hosting?
Point init at your own server instead:

```sh
SERVER_URL=https://clip.example.com rf-clip init
```

`init` generates a random 256-bit secret and writes
`$XDG_CONFIG_HOME/rf-clipboard.conf` (default `~/.config/rf-clipboard.conf`,
mode 0600). Copy that file to your other devices — sharing the secret is what
shares the clipboard. `init` refuses to overwrite an existing config unless you
pass `-f`, because losing the secret orphans the server-side data.

### System clipboard

Set `system_clipboard=true` in the config to make `rf-copy`/`rf-clip` also
copy the plaintext to the local system clipboard, alongside the encrypted
upload. It uses the first tool found among `termux-clipboard-set`, `pbcopy`
(macOS), `wl-copy` (Wayland), `xclip`/`xsel` (X11), and `clip.exe` (WSL) —
install one for your platform. Best-effort: if the tool is missing or fails,
you get a warning on stderr but the upload still decides the exit code.
Default `false`.

## How the crypto works

One secret, two derived values (HKDF-SHA256):

- **account ID** = `HKDF(secret, "rf-clipboard/account-id")` — sent to the
  server as a bearer token. It's a hash: the server learns nothing from it and
  needs no registration; any well-formed ID is an account.
- **encryption key** = `HKDF(secret, "rf-clipboard/encryption-key")` — used
  for AES-256-GCM on the client. Never leaves your machine.

A compromised server yields ciphertext and opaque IDs, nothing else.

The account ID is also the *write* capability: anyone who holds it — including
the server operator — can overwrite or delete your clipboard, or replay an
older ciphertext of yours. They can never read contents or forge new ones
(GCM authentication fails on anything not encrypted with your key).
Confidentiality and content integrity are guaranteed; availability is not
part of the security model.

## Self-hosting the server

### Docker (recommended)

```sh
docker run -d --name rf-clipd --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v rf-clipd-data:/data \
  -e RF_CLIPD_PERSIST=/data/snap.gob \
  ghcr.io/runfridge/rf-clipd:latest
```

Images are published to `ghcr.io/runfridge/rf-clipd` (linux amd64/arm64) on
every release, tagged `latest` and the release version.

Or use the sample [docker-compose.yml](docker-compose.yml):

```sh
docker compose up -d
```

Then put a TLS-terminating reverse proxy in front, e.g. Caddy:

```
clip.example.com {
    reverse_proxy localhost:8080
}
```

Finally, point each client at it:

```sh
SERVER_URL=https://clip.example.com rf-clip init
```

### Bare binary

Download `rf-clipd_<os>_<arch>` from the releases page (or `make server` from
source) and run it under your process manager:

```sh
rf-clipd -addr :8080 -ttl 24h -max-size 1048576 -max-entries 1000 -persist /var/lib/rf-clipd/snap.gob
```

Every flag also reads an env var: `RF_CLIPD_ADDR`, `RF_CLIPD_TTL`,
`RF_CLIPD_MAX_SIZE`, `RF_CLIPD_MAX_ENTRIES`, `RF_CLIPD_PERSIST`,
`RF_CLIPD_HERO` (flags win).

### Notes

- Storage is an in-memory map. Entries unused (no copy *or* paste) for `-ttl`
  are evicted by a periodic sweep.
- `-persist` (optional) snapshots the map to a file on shutdown and on each
  sweep tick, and loads it on start — survives reboots, loses at most one
  sweep interval on a crash. The file contains only ciphertext.
- When the store hits `max-entries`, *new* accounts are rejected (507) rather
  than evicting existing entries — deliberate: ID-spraying spam can lock out
  new accounts until TTL clears it, but it can never push out your data.
  Watch for "store full" log lines; the remedy is raising `-max-entries`.
- **Sizing:** worst-case memory ≈ `max-entries × max-size` (defaults: ~1 GB).
  Set the caps so that product fits your host's RAM; typical personal use is a
  few KB total, so any small VPS works.
- The server serves a landing page at `/` (plus `/privacy` and the `/ko`
  variants). `-hero=false` (or `RF_CLIPD_HERO=false`) turns them off for an
  API-only server — those paths then return 404.
- The server speaks plain HTTP. Run it behind a TLS-terminating reverse proxy
  (Caddy, nginx, Traefik) — the account token travels in a header, and clip
  contents are already client-side encrypted.

### API

| Method | Path       | Auth                       | Response                          |
| ------ | ---------- | -------------------------- | --------------------------------- |
| PUT    | `/v1/clip` | `Bearer <64-hex account>`  | 204, 413 too large, 507 full      |
| GET    | `/v1/clip` | `Bearer <64-hex account>`  | 200 ciphertext, 404 empty         |
