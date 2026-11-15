#!/usr/bin/env bash
# E2E reproduction for bug 01a08e8f — a freshly created linked worktree whose
# branch sits AT the integration tip with ZERO commits of its own is reaped
# (worktree directory + worktree admin entry + branch ref, no archive tag, no
# signal).
#
#   usage: bash devdoc/wip-worktree-guard-e2e.sh <pre-fix-gopi-bin> <fixed-gopi-bin>
#
# FACE NOTE (honest comparison — read before trusting phase 1): the
# `gopi task merged-cleanup` verb is ADDED BY THIS FIX, so a binary built from
# the unfixed tree cannot run it (it prints the family usage and exits). The
# pre-fix side therefore goes through the OTHER face of the same pass — the
# merger tick (`gopi task merger --once`), which exists on both trees and runs
# the identical MergedCleanupSweep. The fixed side is then shown on BOTH faces.
# (devdoc/wip-worktree-tick-e2e.sh, an earlier draft of this script, has been
# folded in here — the tick face IS phase 1/2 below, and keeping one script
# avoids two copies of the same fixtures drifting apart.)
set -u

# Scratch-home contract: this script creates fixture trees under $HOME/workspace,
# and the shared helper is what asserts the path is off the / filesystem and
# registers trap-time cleanup (see scripts/lib/tmp-ban.sh, devdoc/tmp-ban-scratch-root.md).
# shellcheck source=scripts/lib/tmp-ban.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib" && pwd)/tmp-ban.sh"
# THE FRESH-PATH GUARD (scripts/lib/worktree-fresh.sh — the shell half of
# internal/tools/worktree_fresh_path.go). The fixture below creates a linked
# worktree by hand, so this script is a creation site too.
# shellcheck source=scripts/lib/worktree-fresh.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/../scripts/lib" && pwd)/worktree-fresh.sh" \
  || { echo "wip-worktree-guard-e2e: scripts/lib/worktree-fresh.sh missing" >&2; exit 2; }

OLD_BIN="${1:?usage: wip-worktree-guard-e2e.sh <pre-fix-bin> <fixed-bin>}"
NEW_BIN="${2:?usage: wip-worktree-guard-e2e.sh <pre-fix-bin> <fixed-bin>}"

pass() { printf '\n===== %s =====\n' "$*"; }

# make_fixture <tag> <branch> → echoes the fixture root (repo + sandbox settings)
make_fixture() {
  local tag="$1" branch="$2" root
  root="$(gopi_scratch_mkdir "e2e-${tag}")"
  gopi_assert_off_root_fs "$root"
  git init -q "$root"
  git -C "$root" config user.email e2e@e2e
  git -C "$root" config user.name e2e
  git -C "$root" checkout -q -b master
  echo base >"$root/base.txt"
  git -C "$root" add base.txt
  git -C "$root" commit -qm base
  # THE VICTIM SHAPE: worktree + branch at the tip, zero commits.
  # THE FRESH-PATH GUARD (scripts/lib/worktree-fresh.sh): this fixture is itself
  # a worktree creation site. `gopi_scratch_mkdir` hands back a FRESH directory,
  # so `$root/wt` is fresh by construction — the guard is here because every
  # creator carries it and because a scratch class that ever returned a
  # non-empty directory must be refused rather than checked out into.
  gopi_assert_worktree_path_fresh "$root/wt" || exit 2
  git -C "$root" worktree add -q "$root/wt" -b "$branch"
  mkdir -p "$root/ghome"
  printf '{"repos": {"%s": {}}}\n' "$root" >"$root/settings.json"
  echo "$root"
}

# report <root> <branch>
report() {
  local root="$1" branch="$2"
  printf 'worktree dir:         %s\n' "$([ -d "$root/wt" ] && echo SURVIVES || echo GONE)"
  printf 'worktree ADMIN entry: %s\n' "$(git -C "$root" worktree list --porcelain | grep -c "^worktree $root/wt$" | sed 's/^1$/SURVIVES/;s/^0$/GONE/')"
  printf 'branch ref:           %s\n' "$(git -C "$root" rev-parse --verify --quiet "$branch" >/dev/null && echo SURVIVES || echo GONE)"
  printf 'archive tag:          %s\n' "$(git -C "$root" tag -l "archive/$branch*" | wc -l | sed 's/^0$/none/')"
}

tick() { # tick <bin> <root> <label>
  local bin="$1" root="$2" label="$3"
  printf -- '--- %s: gopi task merger --once (the pass exists on BOTH trees) ---\n' "$label"
  ( cd "$root" && HOME="$root/ghome" GOPI_HOME="$root/ghome" \
      GOPI_SETTINGS="$root/settings.json" GOPI_MEMORY_DB="$root/ghome/memory.db" \
      GOPI_COMPUTE_NODES= GOPI_SKIP_CLAIM_BROADCAST=1 \
      "$bin" task merger --once ) 2>&1 | grep -iE "merger|merged|delete|cleanup|reap" | sed 's/^/    /'
}

verb() { # verb <bin> <root> <label> — the fix-side one-pass face
  local bin="$1" root="$2" label="$3"
  printf -- '--- %s: gopi task merged-cleanup (verb added by the fix) ---\n' "$label"
  ( cd "$root" && HOME="$root/ghome" GOPI_HOME="$root/ghome" \
      GOPI_SETTINGS="$root/settings.json" GOPI_MEMORY_DB="$root/ghome/memory.db" \
      "$bin" task merged-cleanup ) 2>&1 | sed 's/^/    /'
}

pass "PHASE 1 — PRE-FIX binary, fresh WIP worktree at the tip (tick face)"
R1="$(make_fixture prefix e2e/wip-prefix)"
printf 'fixture: %s\n' "$R1"
printf 'branch is trivially an ancestor of master: %s\n' "$(git -C "$R1" merge-base --is-ancestor e2e/wip-prefix master && echo YES || echo no)"
printf 'reflog entries on the brand-new branch: %s\n' "$(git -C "$R1" reflog show --format=%H e2e/wip-prefix | wc -l)"
printf 'pre-fix binary has the merged-cleanup verb? %s\n' "$(cd "$R1" && "$OLD_BIN" task merged-cleanup 2>&1 | grep -q 'unknown' && echo 'NO — hence the tick face below' || echo yes)"
tick "$OLD_BIN" "$R1" PRE-FIX
report "$R1" e2e/wip-prefix

pass "PHASE 2 — FIXED binary, identical shape (tick face)"
R2="$(make_fixture fixed e2e/wip-fixed)"
printf 'fixture: %s\n' "$R2"
tick "$NEW_BIN" "$R2" FIXED
report "$R2" e2e/wip-fixed

pass "PHASE 3 — FIXED binary, the same fixture on the new one-pass verb"
verb "$NEW_BIN" "$R2" FIXED
report "$R2" e2e/wip-fixed

pass "PHASE 4 — FIXED binary, master ADVANCES after creation (the window the first draft missed)"
R3="$(make_fixture advance e2e/wip-advance)"
printf 'fixture: %s\n' "$R3"
( cd "$R3" && echo concurrent >>base.txt && git commit -qam "concurrent advance" )
printf 'master advanced to: %s (worktree created at its previous tip)\n' "$(git -C "$R3" rev-parse --short HEAD)"
verb "$NEW_BIN" "$R3" FIXED
report "$R3" e2e/wip-advance

pass "PHASE 5 — FIXED binary, a genuine MERGED LEFTOVER is still reaped (recycle lane alive)"
R4="$(make_fixture recycle e2e/merged-leftover)"
printf 'fixture: %s\n' "$R4"
( cd "$R4/wt" && echo work >feature.txt && git add feature.txt && git commit -qm "feature work" )
( cd "$R4" && git merge -q --no-edit e2e/merged-leftover )
printf 'branch merged into master; worktree holds a commit of its own\n'
verb "$NEW_BIN" "$R4" FIXED
report "$R4" e2e/merged-leftover

printf '\nfixtures left in place for inspection: %s %s %s %s\n' "$R1" "$R2" "$R3" "$R4"
