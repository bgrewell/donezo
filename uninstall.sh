#!/usr/bin/env bash
#
# donezo uninstaller
#
# Removes the donezod service, binary, Caddy site config, and /etc/donezo.
# The data directory and existing backups are PRESERVED unless
# DONEZO_PURGE=1, in which case a final backup is taken first and the data
# directory is then removed. Caddy itself is never uninstalled.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bgrewell/donezo/main/uninstall.sh | sudo bash
#
# Configuration (environment variables):
#   DONEZO_DATA_DIR     Data directory (default: /var/lib/donezo).
#   DONEZO_BACKUP_DIR   Backup directory (default: /var/backups/donezo).
#   DONEZO_PURGE        1 = remove the data directory (after a final backup).
#   DONEZO_UNATTENDED   1 = no confirmation prompt.

set -euo pipefail

BIN_PATH="/usr/local/bin/donezod"
UNIT_PATH="/etc/systemd/system/donezod.service"
ENV_DIR="/etc/donezo"
CADDY_SITE="/etc/caddy/conf.d/donezo.caddy"

DATA_DIR="${DONEZO_DATA_DIR:-/var/lib/donezo}"
BACKUP_DIR="${DONEZO_BACKUP_DIR:-/var/backups/donezo}"
PURGE="${DONEZO_PURGE:-0}"
UNATTENDED="${DONEZO_UNATTENDED:-0}"

log() { printf '[donezo-uninstall] %s\n' "$*"; }
die() { printf '[donezo-uninstall] error: %s\n' "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] && return 0
    if [ "$UNATTENDED" != "1" ] && [ -t 0 ] && [ -f "$0" ]; then
        log "re-executing with sudo"
        exec sudo --preserve-env=DONEZO_DATA_DIR,DONEZO_BACKUP_DIR,DONEZO_PURGE,DONEZO_UNATTENDED bash "$0" "$@"
    fi
    die "must run as root (use: curl -fsSL .../uninstall.sh | sudo bash)"
}

confirm() {
    [ "$UNATTENDED" = "1" ] && return 0
    if ! { true </dev/tty; } 2>/dev/null; then
        die "cannot prompt for confirmation (no terminal); set DONEZO_UNATTENDED=1 to proceed"
    fi
    local what="service, binary, and config"
    [ "$PURGE" = "1" ] && what="service, binary, config, AND the data directory ($DATA_DIR)"
    printf 'This removes the donezod %s. Continue? [y/N]: ' "$what" >/dev/tty
    local answer=""
    read -r answer </dev/tty || true
    case "$answer" in
        y|Y|yes|YES) return 0 ;;
        *) log "aborted"; exit 0 ;;
    esac
}

remove_service() {
    if systemctl is-active --quiet donezod 2>/dev/null \
        || systemctl is-enabled --quiet donezod 2>/dev/null; then
        log "stopping and disabling donezod"
        systemctl disable --now donezod >/dev/null 2>&1 || true
    fi
    if [ -f "$UNIT_PATH" ]; then
        log "removing $UNIT_PATH"
        rm -f "$UNIT_PATH"
        systemctl daemon-reload
    fi
}

remove_binary() {
    if [ -f "$BIN_PATH" ]; then
        log "removing $BIN_PATH"
        rm -f "$BIN_PATH"
    fi
}

remove_caddy_site() {
    if [ -f "$CADDY_SITE" ]; then
        log "removing $CADDY_SITE"
        rm -f "$CADDY_SITE"
        if systemctl is-active --quiet caddy 2>/dev/null; then
            log "reloading caddy"
            systemctl reload caddy || true
        fi
    fi
}

remove_env() {
    if [ -d "$ENV_DIR" ]; then
        log "removing $ENV_DIR"
        rm -rf "$ENV_DIR"
    fi
}

purge_data() {
    [ "$PURGE" = "1" ] || return 0
    if [ -d "$DATA_DIR" ] && [ -n "$(ls -A "$DATA_DIR" 2>/dev/null)" ]; then
        install -d -m 0750 "$BACKUP_DIR"
        local ts backup
        ts="$(date +%Y%m%d-%H%M%S)"
        backup="$BACKUP_DIR/donezo-data-${ts}-final.tar.gz"
        log "final backup: $backup"
        tar -czf "$backup" -C "$(dirname "$DATA_DIR")" "$(basename "$DATA_DIR")"
    fi
    if [ -d "$DATA_DIR" ]; then
        log "removing data dir $DATA_DIR (DONEZO_PURGE=1)"
        rm -rf "$DATA_DIR"
    fi
}

summary() {
    printf '\n'
    log "donezod removed."
    if [ "$PURGE" = "1" ]; then
        log "kept: backups in $BACKUP_DIR (including the final pre-purge backup)"
    else
        log "kept: data dir $DATA_DIR (set DONEZO_PURGE=1 to remove it)"
        if [ -d "$BACKUP_DIR" ]; then
            log "kept: backups in $BACKUP_DIR"
        fi
    fi
    log "kept: the caddy package and /etc/caddy/Caddyfile (only the donezo site file was removed)"
    log "kept: system user 'donezo' (remove manually with: userdel donezo)"
}

main() {
    require_root "$@"
    command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) is required"
    confirm
    remove_service
    remove_binary
    remove_caddy_site
    remove_env
    purge_data
    summary
}

main "$@"
