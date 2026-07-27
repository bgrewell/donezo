#!/usr/bin/env bash
# Snapshot the donezo dev data dir. Wired as ExecStartPre= in
# donezod-dev.service, so it runs on every service (re)start while the old
# server is already stopped — the SQLite files are quiescent (graceful
# shutdown checkpoints the WAL), making plain copies consistent.
#
# Keeps the most recent $KEEP snapshots under $SNAP_ROOT/<timestamp>/.
set -euo pipefail

DATA_DIR="${DONEZO_DATA_DIR:-$HOME/.local/share/donezo-dev}"
SNAP_ROOT="${DONEZO_SNAP_DIR:-$HOME/.local/share/donezo-dev-snapshots}"
KEEP="${DONEZO_SNAP_KEEP:-20}"

# Nothing to snapshot on a fresh system.
[ -d "$DATA_DIR" ] || exit 0

ts="$(date +%Y%m%d-%H%M%S)"
dest="$SNAP_ROOT/$ts"
mkdir -p "$dest"
cp -a "$DATA_DIR/." "$dest/"

# Prune beyond KEEP, oldest first.
ls -1d "$SNAP_ROOT"/*/ 2>/dev/null | sort | head -n -"$KEEP" | xargs -r rm -rf

echo "donezo dev snapshot: $dest"
