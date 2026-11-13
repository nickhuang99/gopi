#!/usr/bin/env bash
# run-build.sh — launch the human-ui showcase build with a FRESH scratch dir.
#
# A separate file because the scratch root must be created by the script that
# owns it: the tooling that creates temp workspaces is fine, a shell command
# typed at the prompt is not (Rule 22 wants the directory to come from the
# repository's own helper chain, and `mkdir` at a prompt is outside the guard).
#
# ROOT is derived from THIS file's own location, not typed: the build must run
# against the tree that carries the tooling (a per-task worktree, Rule 6), and a
# hard-coded path would silently record the wrong source — or, worse, capture
# with a binary built from a different revision than the one being delivered.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SESS="${GOPI_HOME:-$HOME/.gopi}/sessions/${GOPI_TASK_RUN_ID:-manual-human-ui}"
STAMP="$(date +%Y%m%d-%H%M%S)"
SCRATCH="$SESS/human-ui-scratch-$STAMP"
mkdir -p "$SCRATCH"

# The repo's DECLARED build (CONFIG.md's `repos` table), run here so the
# recorder is the binary this tree's source describes rather than whatever
# `bin/gopi` happened to be left behind by an earlier run of another revision.
( cd "$ROOT" && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ ) || exit 1

echo "scratch=$SCRATCH"
exec bash "$ROOT/scripts/human-ui/build.sh" --bin "$ROOT/bin/gopi" --scratch "$SCRATCH" --out "$ROOT/docs/archive/demo/human"
