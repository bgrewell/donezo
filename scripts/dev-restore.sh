#!/usr/bin/env bash
# Restore a donezo dev snapshot taken by dev-snapshot.sh.
#
# Usage:
#   dev-restore.sh              list available snapshots
#   dev-restore.sh <timestamp>  stop donezod-dev, restore, start again
#
# Note: the restart itself triggers ExecStartPre, so restoring also
# snapshots the state you are replacing — restores are always undoable.
set -euo pipefail

DATA_DIR="${DONEZO_DATA_DIR:-$HOME/.local/share/donezo-dev}"
SNAP_ROOT="${DONEZO_SNAP_DIR:-$HOME/.local/share/donezo-dev-snapshots}"

if [ $# -eq 0 ]; then
  ls -1 "$SNAP_ROOT" 2>/dev/null || echo "no snapshots yet"
  exit 0
fi

src="$SNAP_ROOT/$1"
[ -d "$src" ] || { echo "no such snapshot: $1" >&2; exit 1; }

systemctl --user stop donezod-dev
rm -rf "$DATA_DIR"
mkdir -p "$DATA_DIR"
cp -a "$src/." "$DATA_DIR/"
systemctl --user start donezod-dev
echo "restored snapshot $1"
