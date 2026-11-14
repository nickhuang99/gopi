#!/usr/bin/env bash
# Real ground-truth demo of the worktree-cwd release defect and its fix.
#
#   bash devdoc/worktree-cwd-release-coverage/run.sh [<repo>]      # default ~/workspace/gopi-cwd-release
#
# CASE RED   a process that stands in a worktree and NEVER releases its cwd:
#            `git worktree remove` unregisters and deletes the directory, but the
#            holder still reports "<path> (deleted)" as its cwd — the removed
#            tree's inode is pinned, which is exactly what finish-cleanup guard 3
#            ("no live process with cwd inside the tree") refuses on. A long-lived
#            process (a worker/merger daemon) in that state withholds its branch
#            forever: `git worktree list` keeps naming the directory and
#            scripts/safe-merge.sh gate [c] refuses it.
# CASE GREEN the same holder shape, but the holder calls THE PRODUCTION ACTION
#            (internal/worktreerelease.Release — the one implementation) before
#            the removal: cwd is the process root, nothing is pinned.
#
# Scratch lives under $HOME/workspace (NEVER the / partition — GOPI.md Rule 22);
# the script prints both device ids so the claim is measured, not asserted.
set -u
# THE FRESH-PATH GUARD (scripts/lib/worktree-fresh.sh — the shell half of
# internal/tools/worktree_fresh_path.go). `mk_world` creates a linked worktree
# by hand, so this demo is a creation site: git silently reuses an existing
# EMPTY directory, and a demo run twice must not inherit the previous run's.
# shellcheck source=../../scripts/lib/worktree-fresh.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib" && pwd)/worktree-fresh.sh" \
  || { echo "run.sh: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }
REPO="${1:-$HOME/workspace/gopi-cwd-release}"
ROOT="$HOME/workspace/cwd-demo/scratch"
HOLDER_BIN="$HOME/workspace/cwd-demo/release-holder"
rm -rf "$ROOT"; mkdir -p "$ROOT"
echo "scratch root device=$(stat -c %d "$ROOT")   / device=$(stat -c %d /)"

# The demo holder calls the PRODUCTION release (no second chdir spelling). It is
# built inside the module tree because internal/ packages are import-restricted
# to their own module, then removed again.
build_holder() {
  local d="$REPO/.cwdholder-demo"
  mkdir -p "$d"
  cat > "$d/main.go" <<'GO'
// Command cwdholder stands in the directory it was started in, performs THE
// PRODUCTION RELEASE (internal/worktreerelease.Release — the one
// implementation), and holds, so the demo can remove the worktree under it.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nick/gopi/internal/worktreerelease"
)

func main() {
	wd, _ := os.Getwd()
	fmt.Println("holder start cwd:", wd)
	// THE PRODUCTION SEAM (what every close-out path calls): ReleaseReporting
	// skips only inside a `go test` binary — this holder is built as an ordinary
	// executable, so it takes the real path, exactly as bin/gopi does.
	if ok := worktreerelease.ReleaseReporting(os.Stdout, "gopi demo holder"); !ok {
		os.Exit(1)
	}
	now, _ := os.Getwd()
	fmt.Println("released-by-holder: cwd is now", now)
	time.Sleep(120 * time.Second)
}
GO
  ( cd "$REPO" && go build -o "$HOLDER_BIN" ./.cwdholder-demo ) || { echo "holder build failed"; exit 1; }
  rm -rf "$d"
}

mk_world() { # $1 = case dir, $2 = branch suffix
  local C="$1"
  mkdir -p "$C/repo"
  git -C "$C/repo" init -q -b master .
  git -C "$C/repo" config user.email d@d
  git -C "$C/repo" config user.name d
  echo base > "$C/repo/b.txt"
  git -C "$C/repo" add b.txt
  git -C "$C/repo" commit -qm base
  # THE FRESH-PATH GUARD (scripts/lib/worktree-fresh.sh): a hand-created
  # worktree is a creation site, and git silently reuses an existing EMPTY
  # directory. `mk_world`'s caller removes $C first, so this path is fresh by
  # construction — the guard is here so the demo cannot inherit a leftover.
  gopi_assert_worktree_path_fresh "$C/wt-demo" || exit 2
  git -C "$C/repo" worktree add -q -b "feat/$2" "$C/wt-demo" master
  echo delivery > "$C/wt-demo/deliverable.txt"
  git -C "$C/wt-demo" add deliverable.txt
  git -C "$C/wt-demo" commit -qm "the deliverable"
}

echo
echo "=============== CASE RED: a holder that NEVER releases its cwd ==============="
C="$ROOT/red"; mk_world "$C" demo
( cd "$C/wt-demo" && exec sleep 120 ) &          # the unfixed process shape
HOLDER=$!
for _ in $(seq 1 50); do [ "$(readlink "/proc/$HOLDER/cwd" 2>/dev/null)" = "$C/wt-demo" ] && break; sleep 0.1; done
echo "holder pid         : $HOLDER"
echo "cwd before removal : $(readlink "/proc/$HOLDER/cwd")"
git -C "$C/repo" worktree remove "$C/wt-demo"; echo "git worktree remove exit=$?"
echo "git worktree list  : $(git -C "$C/repo" worktree list | wc -l) entry/-ies (the registration is gone)"
echo "worktree on disk   : $(test -e "$C/wt-demo" && echo present || echo removed)"
echo "cwd  after removal : $(readlink "/proc/$HOLDER/cwd")"
echo "=> still PINNED (the ' (deleted)' suffix) — finish-cleanup guard 3 keeps refusing this branch, the merger never merges it"
kill "$HOLDER" 2>/dev/null; wait "$HOLDER" 2>/dev/null

echo
echo "=============== CASE GREEN: the holder calls the PRODUCTION release ==============="
build_holder
C="$ROOT/green"; mk_world "$C" demo2
( cd "$C/wt-demo" && exec "$HOLDER_BIN" ) > "$ROOT/green-holder.log" 2>&1 &
HOLDER=$!
for _ in $(seq 1 50); do grep -q 'released-by-holder' "$ROOT/green-holder.log" 2>/dev/null && break; sleep 0.1; done
cat "$ROOT/green-holder.log"
echo "holder pid         : $HOLDER"
echo "cwd before removal : $(readlink "/proc/$HOLDER/cwd")"
git -C "$C/repo" worktree remove "$C/wt-demo"; echo "git worktree remove exit=$?"
echo "worktree on disk   : $(test -e "$C/wt-demo" && echo present || echo removed)"
echo "cwd  after removal : $(readlink "/proc/$HOLDER/cwd")"
echo "=> PINNED? no — the released tree's inode is free while the holder is STILL ALIVE"
echo "deliverable safe   : $(git -C "$C/repo" log --oneline -1 feat/demo2)"
kill "$HOLDER" 2>/dev/null; wait "$HOLDER" 2>/dev/null

echo
echo "git worktree list — no demo registration survives in either repo:"
git -C "$ROOT/red/repo" worktree list
git -C "$ROOT/green/repo" worktree list
