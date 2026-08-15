#!/usr/bin/env bash
#
# donezo installer
#
# Installs or upgrades donezod (the donezo server) on a systemd Linux host
# and optionally configures Caddy as a reverse proxy. Safe to re-run: a
# re-run with the same settings changes nothing except restarting the
# service, and a re-run against a newer release is the upgrade path (the
# data directory is backed up first).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/install.sh | sudo bash
#
# Configuration (environment variables, all optional unless noted):
#   DONEZO_DOMAIN       Site address for Caddy. An FQDN gets automatic
#                       HTTPS; "localhost", an IP, or a dotless LAN hostname
#                       gets a plain-HTTP site that works without public
#                       DNS. Prompted for interactively when unset;
#                       required in unattended mode unless DONEZO_NO_CADDY=1.
#   DONEZO_VERSION      "vX.Y.Z" = that release tag; a 7-40 char hex commit
#                       hash = build from source (requires git, go, npm);
#                       unset or "latest" = newest GitHub release.
#   DONEZO_PORT         donezod listen port (default: 8787).
#   DONEZO_DATA_DIR     Data directory (default: /var/lib/donezo).
#   DONEZO_BACKUP_DIR   Backup directory (default: /var/backups/donezo).
#   DONEZO_UNATTENDED   1 = never prompt; missing required values error out.
#   DONEZO_NO_CADDY     1 = skip reverse-proxy setup entirely.
#   DONEZO_LOCAL_ASSET  Path to a local release tarball. Skips download and
#                       checksum verification (testing seam).

set -euo pipefail

REPO="bgrewell/donezo"
BIN_PATH="/usr/local/bin/donezod"
UNIT_PATH="/etc/systemd/system/donezod.service"
ENV_DIR="/etc/donezo"
ENV_FILE="/etc/donezo/donezod.env"
CADDYFILE="/etc/caddy/Caddyfile"
CADDY_SITE="/etc/caddy/conf.d/donezo.caddy"
CADDY_IMPORT_LINE='import /etc/caddy/conf.d/*.caddy'

PORT="${DONEZO_PORT:-8787}"
DATA_DIR="${DONEZO_DATA_DIR:-/var/lib/donezo}"
BACKUP_DIR="${DONEZO_BACKUP_DIR:-/var/backups/donezo}"
UNATTENDED="${DONEZO_UNATTENDED:-0}"
NO_CADDY="${DONEZO_NO_CADDY:-0}"
LOCAL_ASSET="${DONEZO_LOCAL_ASSET:-}"
DOMAIN="${DONEZO_DOMAIN:-}"
VERSION="${DONEZO_VERSION:-}"

WORKDIR=""
ARCH=""
UPGRADE=0
CADDY_ACTIVE=0

log() { printf '[donezo-install] %s\n' "$*"; }
die() { printf '[donezo-install] error: %s\n' "$*" >&2; exit 1; }

cleanup() { [ -n "$WORKDIR" ] && rm -rf "$WORKDIR"; }

require_root() {
    [ "$(id -u)" -eq 0 ] && return 0
    # Interactive terminal and a real script file on disk: re-exec via sudo.
    if [ "$UNATTENDED" != "1" ] && [ -t 0 ] && [ -f "$0" ]; then
        log "re-executing with sudo"
        exec sudo --preserve-env=DONEZO_DOMAIN,DONEZO_VERSION,DONEZO_PORT,DONEZO_DATA_DIR,DONEZO_BACKUP_DIR,DONEZO_UNATTENDED,DONEZO_NO_CADDY,DONEZO_LOCAL_ASSET bash "$0" "$@"
    fi
    die "must run as root (use: curl -fsSL .../install.sh | sudo bash)"
}

require_tools() {
    command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) is required"
    for tool in curl tar sha256sum install sed; do
        command -v "$tool" >/dev/null 2>&1 || die "required tool missing: $tool"
    done
    [[ "$PORT" =~ ^[0-9]+$ ]] || die "DONEZO_PORT must be numeric, got: $PORT"
    case "$DATA_DIR" in /*) ;; *) die "DONEZO_DATA_DIR must be an absolute path" ;; esac
    case "$BACKUP_DIR" in /*) ;; *) die "DONEZO_BACKUP_DIR must be an absolute path" ;; esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64)  ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        *) die "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
    esac
    log "architecture: $ARCH"
}

resolve_domain() {
    [ "$NO_CADDY" = "1" ] && return 0
    [ -n "$DOMAIN" ] && return 0
    if [ "$UNATTENDED" = "1" ]; then
        die "DONEZO_DOMAIN is required in unattended mode (or set DONEZO_NO_CADDY=1)"
    fi
    if ! { true </dev/tty; } 2>/dev/null; then
        die "cannot prompt for domain (no terminal); set DONEZO_DOMAIN or DONEZO_UNATTENDED=1"
    fi
    local suggested
    suggested="$(hostname -f 2>/dev/null || hostname)"
    printf 'Domain for donezo (FQDN for HTTPS, or "localhost" / a LAN name for HTTP-only) [%s]: ' "$suggested" >/dev/tty
    read -r DOMAIN </dev/tty || true
    DOMAIN="${DOMAIN:-$suggested}"
    log "domain: $DOMAIN"
}

# is_local_domain: names Caddy cannot get a public certificate for.
is_local_domain() {
    case "$1" in
        localhost|*.localhost) return 0 ;;
    esac
    [[ "$1" != *.* ]] && return 0                              # dotless LAN hostname
    [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && return 0 # IPv4 literal
    [[ "$1" == *:* ]] && return 0                              # IPv6 literal
    return 1
}

resolve_version() {
    [ -n "$LOCAL_ASSET" ] && { VERSION="local"; log "using local asset: $LOCAL_ASSET"; return 0; }
    if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
        log "resolving latest release tag"
        VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
            | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
        [ -n "$VERSION" ] || die "could not resolve the latest release tag from the GitHub API"
    fi
    log "version: $VERSION"
}

# acquire_release populates $WORKDIR/pkg with the release payload:
# pkg/donezod and (when packaged) pkg/deploy/donezod.service.example.
acquire_release() {
    mkdir -p "$WORKDIR/pkg"
    if [ -n "$LOCAL_ASSET" ]; then
        [ -f "$LOCAL_ASSET" ] || die "DONEZO_LOCAL_ASSET not found: $LOCAL_ASSET"
        tar -xzf "$LOCAL_ASSET" -C "$WORKDIR/pkg"
    elif [[ "$VERSION" == v* ]]; then
        download_release
    elif [[ "$VERSION" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
        build_from_source "$VERSION"
    else
        die "DONEZO_VERSION must be a vX.Y.Z tag or a 7-40 char commit hash, got: $VERSION"
    fi
    [ -f "$WORKDIR/pkg/donezod" ] || die "release payload has no donezod binary"
}

download_release() {
    local asset="donezo_${VERSION}_linux_${ARCH}.tar.gz"
    local base="https://github.com/${REPO}/releases/download/${VERSION}"
    log "downloading $asset"
    curl -fsSL -o "$WORKDIR/$asset" "$base/$asset" \
        || die "download failed: $base/$asset"
    curl -fsSL -o "$WORKDIR/checksums.txt" "$base/checksums.txt" \
        || die "download failed: $base/checksums.txt"
    local expected actual
    expected="$(awk -v a="$asset" '$2 == a {print $1}' "$WORKDIR/checksums.txt")"
    [ -n "$expected" ] || die "checksums.txt has no entry for $asset"
    actual="$(sha256sum "$WORKDIR/$asset" | awk '{print $1}')"
    [ "$expected" = "$actual" ] || die "sha256 mismatch for $asset (expected $expected, got $actual)"
    log "sha256 verified"
    tar -xzf "$WORKDIR/$asset" -C "$WORKDIR/pkg"
}

# build_from_source: DONEZO_VERSION was a commit hash. Clones the repo (and
# the design-system sibling that web/ depends on), checks out the commit,
# and runs make release-build. Requires git, go, and npm on the host.
build_from_source() {
    local commit="$1" tool
    for tool in git go npm; do
        command -v "$tool" >/dev/null 2>&1 || die "source build requires $tool"
    done
    log "building from source at commit $commit"
    git clone --quiet "https://github.com/${REPO}" "$WORKDIR/src/donezo"
    git -C "$WORKDIR/src/donezo" checkout --quiet --detach "$commit"
    # web/package.json declares @grewelltech/console as file:../../design-system.
    git clone --quiet https://github.com/grewelltech/design-system "$WORKDIR/src/design-system"
    npm ci --prefix "$WORKDIR/src/donezo/web" --no-audit --no-fund
    make -C "$WORKDIR/src/donezo" release-build "VERSION=$commit"
    install -m 0755 "$WORKDIR/src/donezo/bin/donezod" "$WORKDIR/pkg/donezod"
    if [ -f "$WORKDIR/src/donezo/deploy/donezod.service.example" ]; then
        mkdir -p "$WORKDIR/pkg/deploy"
        cp "$WORKDIR/src/donezo/deploy/donezod.service.example" "$WORKDIR/pkg/deploy/"
    fi
}

detect_existing_install() {
    if [ -x "$BIN_PATH" ] || [ -f "$UNIT_PATH" ]; then
        UPGRADE=1
        log "existing install detected: upgrading in place"
    fi
}

# stop_and_backup runs only on the upgrade path, before anything is
# replaced: stop the service so the SQLite files are quiescent, then
# snapshot the data directory.
stop_and_backup() {
    [ "$UPGRADE" = "1" ] || return 0
    if systemctl is-active --quiet donezod 2>/dev/null; then
        log "stopping donezod"
        systemctl stop donezod
    fi
    if [ -d "$DATA_DIR" ] && [ -n "$(ls -A "$DATA_DIR" 2>/dev/null)" ]; then
        install -d -m 0750 "$BACKUP_DIR"
        local ts backup
        ts="$(date +%Y%m%d-%H%M%S)"
        backup="$BACKUP_DIR/donezo-data-${ts}.tar.gz"
        log "backing up $DATA_DIR to $backup"
        tar -czf "$backup" -C "$(dirname "$DATA_DIR")" "$(basename "$DATA_DIR")"
        prune_backups
    fi
}

# prune_backups keeps the 10 newest donezo-data-*.tar.gz files.
prune_backups() {
    find "$BACKUP_DIR" -maxdepth 1 -name 'donezo-data-*.tar.gz' -printf '%T@ %p\n' 2>/dev/null \
        | sort -rn | awk 'NR > 10 { sub(/^[^ ]+ /, ""); print }' \
        | while IFS= read -r old; do
            rm -f -- "$old"
            log "pruned old backup: $old"
        done
}

ensure_user() {
    if ! getent passwd donezo >/dev/null; then
        log "creating system user donezo"
        useradd --system --user-group --no-create-home \
            --home-dir "$DATA_DIR" --shell /usr/sbin/nologin donezo
    fi
}

ensure_data_dir() {
    if [ ! -d "$DATA_DIR" ]; then
        log "creating data dir $DATA_DIR"
    fi
    install -d -m 0750 -o donezo -g donezo "$DATA_DIR"
}

install_binary() {
    log "installing donezod to $BIN_PATH"
    install -m 0755 "$WORKDIR/pkg/donezod" "${BIN_PATH}.new"
    mv -f "${BIN_PATH}.new" "$BIN_PATH"
}

# write_env_file creates /etc/donezo/donezod.env on first install only.
# Operator edits are preserved on upgrade; the unit's ExecStart flags are
# kept in sync with the same values by install_unit.
write_env_file() {
    install -d -m 0755 "$ENV_DIR"
    if [ -f "$ENV_FILE" ]; then
        log "keeping existing $ENV_FILE"
        return 0
    fi
    log "writing $ENV_FILE"
    cat >"$ENV_FILE" <<EOF
# donezod environment (systemd EnvironmentFile).
# Written once by install.sh; re-running the installer preserves this file.
# Note: flags on the unit's ExecStart line take precedence over these.
DONEZOD_PORT=$PORT
DONEZOD_DATA_DIR=$DATA_DIR
EOF
    chmod 0640 "$ENV_FILE"
}

# default_unit is the fallback when the release tarball does not carry
# deploy/donezod.service.example. Kept in sync with that template.
default_unit() {
    cat <<'EOF'
[Unit]
Description=donezod - donezo server
After=network.target

[Service]
Type=simple
User=donezo
Group=donezo
EnvironmentFile=-/etc/donezo/donezod.env
ExecStart=/usr/local/bin/donezod --data-dir /var/lib/donezo --port 8787 --trust-proxy
Restart=on-failure
RestartSec=2
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
}

install_unit() {
    local template rendered
    if [ -f "$WORKDIR/pkg/deploy/donezod.service.example" ]; then
        template="$(cat "$WORKDIR/pkg/deploy/donezod.service.example")"
    else
        log "packaged unit example missing; using built-in template"
        template="$(default_unit)"
    fi
    # Render port and data dir into the template. The data-dir replacement
    # is plain string substitution so arbitrary paths are safe.
    rendered="${template//\/var\/lib\/donezo/$DATA_DIR}"
    rendered="$(sed -E "s/--port [0-9]+/--port $PORT/" <<<"$rendered")"
    if [ "$NO_CADDY" = "1" ]; then
        # No reverse proxy in front: donezod must accept direct connections,
        # so it must not run --trust-proxy. That flag both binds donezod to
        # loopback (making it unreachable here) and trusts forwarded headers
        # that no proxy is setting. Without it, donezod binds all interfaces
        # and ignores the headers, which is correct for direct access.
        rendered="$(sed -E "s/ --trust-proxy//" <<<"$rendered")"
    fi
    if [ -f "$UNIT_PATH" ] && [ "$(cat "$UNIT_PATH")" = "$rendered" ]; then
        log "unit unchanged: $UNIT_PATH"
        return 0
    fi
    log "writing $UNIT_PATH"
    printf '%s\n' "$rendered" >"${UNIT_PATH}.new"
    mv -f "${UNIT_PATH}.new" "$UNIT_PATH"
    systemctl daemon-reload
}

start_service() {
    log "enabling and starting donezod"
    systemctl enable --now donezod >/dev/null 2>&1 || systemctl enable --now donezod
    # A rerun over a running service leaves the old binary in memory;
    # restart so the installed binary is the one serving.
    systemctl restart donezod
}

health_check() {
    local i
    log "waiting for http://127.0.0.1:${PORT}/api/healthz"
    for i in $(seq 1 40); do
        if curl -fsS -o /dev/null "http://127.0.0.1:${PORT}/api/healthz" 2>/dev/null; then
            log "donezod is healthy (attempt $i)"
            return 0
        fi
        sleep 0.5
    done
    die "donezod did not become healthy; check: journalctl -u donezod -n 50"
}

install_caddy_if_missing() {
    command -v caddy >/dev/null 2>&1 && return 0
    local id_like=""
    [ -r /etc/os-release ] && id_like="$(. /etc/os-release; printf '%s %s' "${ID:-}" "${ID_LIKE:-}")"
    case " $id_like " in
        *debian*|*ubuntu*) ;;
        *)
            log "caddy is not installed and this is not a Debian/Ubuntu system."
            log "install caddy manually (https://caddyserver.com/docs/install), then add:"
            log "  $DOMAIN { reverse_proxy 127.0.0.1:$PORT }"
            log "continuing without a reverse proxy; donezod is on http://127.0.0.1:$PORT"
            return 1
            ;;
    esac
    log "installing caddy from the official apt repository"
    apt-get update -qq
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl gnupg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
        | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
        -o /etc/apt/sources.list.d/caddy-stable.list
    apt-get update -qq
    apt-get install -y -qq caddy
}

setup_caddy() {
    if [ "$NO_CADDY" = "1" ]; then
        log "skipping reverse proxy setup (DONEZO_NO_CADDY=1)"
        return 0
    fi
    install_caddy_if_missing || return 0

    # Ensure the Caddyfile pulls in conf.d site files (append exactly once).
    install -d -m 0755 /etc/caddy /etc/caddy/conf.d
    [ -f "$CADDYFILE" ] || : >"$CADDYFILE"
    if ! grep -qF "$CADDY_IMPORT_LINE" "$CADDYFILE"; then
        log "adding conf.d import to $CADDYFILE"
        printf '\n# Added by donezo install.sh: load per-site configs.\n%s\n' \
            "$CADDY_IMPORT_LINE" >>"$CADDYFILE"
    fi

    local site
    if is_local_domain "$DOMAIN"; then
        # No public DNS: serve plain HTTP so no certificate is needed.
        site="# Managed by donezo install.sh - local/LAN mode (plain HTTP).
http://${DOMAIN} {
	reverse_proxy 127.0.0.1:${PORT}
}"
    else
        site="# Managed by donezo install.sh - automatic HTTPS.
${DOMAIN} {
	reverse_proxy 127.0.0.1:${PORT}
}"
    fi
    if [ ! -f "$CADDY_SITE" ] || [ "$(cat "$CADDY_SITE")" != "$site" ]; then
        log "writing $CADDY_SITE"
        printf '%s\n' "$site" >"$CADDY_SITE"
    else
        log "caddy site unchanged: $CADDY_SITE"
    fi

    if systemctl is-active --quiet caddy; then
        log "reloading caddy"
        systemctl reload caddy
    else
        log "enabling and starting caddy"
        systemctl enable --now caddy
    fi
    CADDY_ACTIVE=1
}

summary() {
    local url
    if [ "$CADDY_ACTIVE" = "1" ]; then
        if is_local_domain "$DOMAIN"; then url="http://${DOMAIN}"; else url="https://${DOMAIN}"; fi
    else
        url="http://$(hostname):${PORT}"
    fi
    cat <<EOF

------------------------------------------------------------------
donezo ${VERSION} installed

  URL:        $url
  Binary:     $BIN_PATH
  Service:    donezod (systemctl status donezod)
  Data dir:   $DATA_DIR
  Backups:    $BACKUP_DIR (taken before each upgrade, last 10 kept)
  Env file:   $ENV_FILE (preserved across upgrades)

  Upgrade:    re-run this installer (same command)
  Uninstall:  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/uninstall.sh | sudo bash
------------------------------------------------------------------
EOF
}

main() {
    require_root "$@"
    require_tools
    detect_arch
    resolve_domain
    resolve_version

    WORKDIR="$(mktemp -d)"
    trap cleanup EXIT

    acquire_release
    detect_existing_install
    stop_and_backup
    ensure_user
    ensure_data_dir
    install_binary
    write_env_file
    install_unit
    start_service
    health_check
    setup_caddy
    summary
}

main "$@"
