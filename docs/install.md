# Installing donezo

`install.sh` installs or upgrades **donezod** (the donezo server) as a
systemd service and, by default, configures [Caddy](https://caddyserver.com)
as a reverse proxy in front of it. It targets Linux hosts with systemd on
amd64 or arm64; automatic Caddy installation is Debian/Ubuntu only (other
distros: see [Unsupported distros](#unsupported-distros)).

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/install.sh | sudo bash
```

The installer prompts once for a domain (defaulting to the machine
hostname); everything else uses silent defaults. Fully unattended:

```sh
curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/install.sh \
  | sudo DONEZO_UNATTENDED=1 DONEZO_DOMAIN=todo.example.com bash
```

## Configuration

All configuration is via environment variables set on the `bash` that runs
the script (as shown above — with `curl | sudo bash`, variables must be
passed through `sudo`).

| Variable            | Default              | Meaning                                                                                                                                              |
| ------------------- | -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DONEZO_DOMAIN`     | prompted (hostname)  | Caddy site address. An FQDN gets automatic HTTPS via Let's Encrypt. `localhost`, an IP, or a dotless LAN hostname gets a plain-HTTP site (no public DNS needed). |
| `DONEZO_VERSION`    | latest release       | `vX.Y.Z` installs that release tag. A 7–40 char hex commit hash builds from source (see below). Unset or `latest` resolves the newest GitHub release. |
| `DONEZO_PORT`       | `8787`               | donezod listen port (Caddy proxies to `127.0.0.1:<port>`).                                                                                            |
| `DONEZO_DATA_DIR`   | `/var/lib/donezo`    | SQLite data directory, owned `donezo:donezo`, mode `0750`.                                                                                            |
| `DONEZO_BACKUP_DIR` | `/var/backups/donezo`| Where pre-upgrade data backups are written.                                                                                                            |
| `DONEZO_UNATTENDED` | unset                | `1` = never prompt. Missing required values (the domain, unless `DONEZO_NO_CADDY=1`) become errors.                                                    |
| `DONEZO_NO_CADDY`   | unset                | `1` = skip all reverse-proxy setup; donezod serves plain HTTP on its port.                                                                             |
| `DONEZO_LOCAL_ASSET`| unset                | Path to a local release tarball; skips download and checksum verification. Intended for testing.                                                       |

## What the installer does

Each step is idempotent — re-running the script with the same settings is
safe and changes nothing (apart from a service restart):

1. Detects the architecture (`amd64`/`arm64`) and resolves the version.
2. Downloads `donezo_<tag>_linux_<arch>.tar.gz` and `checksums.txt` from
   the GitHub release and **verifies the sha256** before installing.
3. Creates the `donezo` system user (no login shell) and the data dir.
4. If an existing install is detected, stops the service and **backs up
   the data directory first** (see [Upgrades](#upgrades-and-backups)).
5. Installs the binary atomically to `/usr/local/bin/donezod`.
6. Writes `/etc/donezo/donezod.env` (`DONEZOD_PORT`, `DONEZOD_DATA_DIR`) —
   **only if absent**, so operator edits survive upgrades.
7. Installs `/etc/systemd/system/donezod.service` from the packaged
   template with the chosen port/data dir rendered in (rewritten only when
   the content actually differs), then `systemctl enable --now donezod`
   and waits for `GET /api/healthz` to answer.
8. Unless `DONEZO_NO_CADDY=1`: installs Caddy from its official apt repo
   if missing, ensures `/etc/caddy/Caddyfile` imports
   `/etc/caddy/conf.d/*.caddy` (added once), writes
   `/etc/caddy/conf.d/donezo.caddy`, and reloads Caddy.

The service runs as `User=donezo` with `--trust-proxy` (safe because only
Caddy talks to it on 127.0.0.1). With `DONEZO_NO_CADDY=1` the packaged
unit still passes `--trust-proxy`; remove that flag from the unit if
clients will reach donezod directly, since proxy headers are then
attacker-controlled.

## First run

Open the address you configured (`DONEZO_DOMAIN`, or `http://<host>:8787`
with `DONEZO_NO_CADDY=1`). The first thing you'll see is a setup screen —
pick a username and password to create the **owner** account. There's no
separate admin panel to find first; the first account created is always
the owner.

To let someone else in, open the avatar menu → **Invites** and generate a
code. Codes are single-use and expire (7 days by default); hand one to
whoever you're inviting, and they register with it from the login
screen. Each person gets their own account and their own private
starting space — nobody sees anyone else's data by default. The technical
detail on roles, invite codes, and the registration flow is in
[`docs/api.md`](api.md#roles--invites).

## Upgrades and backups

**To upgrade, re-run the same install command.** When the installer finds
an existing install it:

1. Stops `donezod` (so the SQLite files are quiescent),
2. writes `<backup-dir>/donezo-data-<timestamp>.tar.gz` from the data dir,
3. prunes backups to the newest 10,
4. swaps the binary and restarts the service.

To restore a backup: stop the service, extract the tarball over the data
dir's parent, fix ownership, start the service:

```sh
sudo systemctl stop donezod
sudo tar -xzf /var/backups/donezo/donezo-data-<ts>.tar.gz -C /var/lib
sudo chown -R donezo:donezo /var/lib/donezo
sudo systemctl start donezod
```

## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/uninstall.sh | sudo bash
```

This stops and disables `donezod` and removes the unit, the binary,
`/etc/donezo`, and the Caddy site file (`/etc/caddy/conf.d/donezo.caddy`,
reloading Caddy — the Caddy package itself is never touched). The **data
directory and backups are preserved** by default. To also remove the data
(a final backup is taken first):

```sh
curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/uninstall.sh \
  | sudo DONEZO_UNATTENDED=1 DONEZO_PURGE=1 bash
```

`uninstall.sh` honors the same `DONEZO_DATA_DIR` / `DONEZO_BACKUP_DIR`
variables if you installed to non-default paths. Without
`DONEZO_UNATTENDED=1` it asks for confirmation.

## Building from a commit (source build)

Setting `DONEZO_VERSION` to a commit hash (7–40 hex chars) switches to a
source build: the installer clones `bgrewell/donezo` at that commit (plus
the sibling [`grewelltech/design-system`](https://github.com/grewelltech/design-system)
repo that `web/` depends on), runs `npm ci` and `make release-build`, and
installs the resulting binary. This path requires `git`, `go`, and `npm`
on the host. Everything after the build (user, dirs, unit, Caddy) is
identical to a release install.

## Unsupported distros

The service install works on any systemd distro; only the automatic Caddy
installation is Debian/Ubuntu-specific. On other distros the installer
prints manual instructions and finishes successfully without a proxy —
donezod will be listening on `http://127.0.0.1:<port>`. Install Caddy via
your package manager (see the [official docs](https://caddyserver.com/docs/install)),
then re-run the installer: with `caddy` present on `PATH` it completes the
proxy setup on any distro.

## Verifying downloads manually

Every release ships a `checksums.txt`:

```sh
tag=v0.1.0; arch=amd64
curl -fsSLO "https://github.com/bgrewell/donezo/releases/download/${tag}/donezo_${tag}_linux_${arch}.tar.gz"
curl -fsSLO "https://github.com/bgrewell/donezo/releases/download/${tag}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
```
