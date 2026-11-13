# shellcheck shell=bash
# baseline-verify.sh — the shared contract for a BASE/BASELINE worktree whose
# COUNTERS we intend to trust.
#
# WHY THIS FILE EXISTS (measured 2026-09-19, task 01a0ba1f): a "pristine base"
# worktree was created for a doc-gate counter diff and its counters (1970
# anchors / 413 normative / 108 new / 30 stale) were read as master rot and
# recorded as fact in a bug row and a report. The base tree was never ASSERTED
# — only assumed to be clean and to be at the revision the comparison named.
# Re-measured afterwards on a verified-clean checkout at the exact HEAD, those
# SAME counters reproduce, and so does the gate that reports them: the phantom
# was not the counters, it was the confidence. A counter diff is evidence only
# about the tree it was measured in, so this file turns the assumption into the
# contract every baseline comparison must pass FIRST:
#
#   1. `git status --porcelain` in the base tree prints NOTHING (a dirty base
#      carries the very edits whose effect the diff is supposed to isolate);
#   2. HEAD equals the revision the comparison CLAIMS (a base created from a
#      branch, a stale tip or a shadowed directory measures a different tree —
#      the failure mode that produced the phantom);
#   3. the base lives off `/` (GOPI.md Rule 22 — reuse the /tmp ban's measured
#      device check rather than inventing a path).
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/baseline-verify.sh"
#     BASE="$(gopi_baseline_worktree_add ~/workspace/gopi-base-<task> "$(git rev-parse master)")" || exit 2
#     # ... run the gates in "$BASE" and compare counters ...
#     gopi_assert_baseline_clean "$BASE" "$(git rev-parse master)" || exit 2
#
# A FAILURE HERE IS LOUD AND FATAL TO THE COMPARISON, never a warning: the whole
# value of the check is that an unverified base cannot be read as a result.
# NOTE: sourcing deliberately does not change the caller's shell options
# (set -e/-u/pipefail stay the caller's business), matching tmp-ban.sh.

# gopi_assert_baseline_clean can find the /tmp ban's device check without the
# caller having to source two files in the right order.
BASELINE_VERIFY_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The fresh-path guard is sourced HERE, not left to the caller: the base helper
# below calls it, and a contract that works only when the caller happened to
# source a second file is the "assume the precondition" failure this whole file
# exists to remove. Same reason as the /tmp ban above — one source line, and the
# helper is self-contained.
# shellcheck source=worktree-fresh.sh
. "$BASELINE_VERIFY_SRC/worktree-fresh.sh"

# gopi_baseline_off_root_fs DIR — Rule 22's measured half, delegated to the
# /tmp ban when it is sourced and inlined only as a fallback (never a silent
# skip): a base tree on the same device as `/` is refused.
gopi_baseline_off_root_fs() {
    local dir="$1"
    if command -v gopi_assert_off_root_fs >/dev/null 2>&1; then
        gopi_assert_off_root_fs "$dir"
        return $?
    fi
    if [ -f "$BASELINE_VERIFY_SRC/tmp-ban.sh" ]; then
        # shellcheck source=tmp-ban.sh
        . "$BASELINE_VERIFY_SRC/tmp-ban.sh"
        gopi_assert_off_root_fs "$dir"
        return $?
    fi
    echo "baseline-verify: tmp-ban.sh not found next to $BASELINE_VERIFY_SRC — cannot verify $dir is off /" >&2
    return 1
}

# gopi_assert_baseline_clean DIR [EXPECTED_REV] — the gate. Returns 0 only
# when DIR is a git worktree, its `git status --porcelain` is empty, its HEAD
# equals EXPECTED_REV (when given) and it is off `/`. On any failure it names
# DIR, the offending porcelain entries (capped, with the total) or the expected
# vs actual HEAD, and returns non-zero — callers must treat that as "the
# comparison did not happen", never as "no difference found".
gopi_assert_baseline_clean() {
    local dir="${1:?gopi_assert_baseline_clean needs a directory}"
    local expect="${2:-}"
    local bad=0

    if [ ! -d "$dir" ]; then
        echo "baseline-verify: $dir does not exist — refusing to read counters from a base tree that is not there" >&2
        return 1
    fi
    if ! git -C "$dir" rev-parse --git-dir >/dev/null 2>&1; then
        echo "baseline-verify: $dir is not a git worktree — a base tree must be a checkout of the revision under test" >&2
        return 1
    fi

    gopi_baseline_off_root_fs "$dir" || bad=1

    local porcelain total shown
    porcelain="$(git -C "$dir" status --porcelain 2>&1)"
    if [ -n "$porcelain" ]; then
        total="$(printf '%s\n' "$porcelain" | wc -l | tr -d ' ')"
        shown="$(printf '%s\n' "$porcelain" | head -20 | sed 's/^/  /')"
        echo "baseline-verify: $dir is NOT clean (git status --porcelain: $total entr(y/ies))." >&2
        printf '%s\n' "$shown" >&2
        if [ "$total" -gt 20 ]; then
            echo "  ... and $((total - 20)) more" >&2
        fi
        echo "  A dirty base tree carries edits of its own, so every counter diff against it is" >&2
        echo "  unattributable. Commit or clean it (untracked evidence belongs outside the tree)," >&2
        echo "  or create a fresh base at the revision you mean to compare against." >&2
        bad=1
    fi

    if [ -n "$expect" ]; then
        local actual
        actual="$(git -C "$dir" rev-parse HEAD 2>/dev/null)"
        if [ "$actual" != "$expect" ]; then
            echo "baseline-verify: $dir is at HEAD $actual, NOT the expected revision $expect." >&2
            echo "  A base at another revision measures another tree — the diff is not about $expect." >&2
            bad=1
        fi
    fi

    if [ "$bad" != 0 ]; then
        return 1
    fi

}

# gopi_baseline_worktree_add DIR REV — create the detached base worktree at REV
# and verify it in one step, so a caller cannot create a base and forget the
# check. An existing non-empty DIR is refused (a nested/again-added worktree is
# exactly how a "base" ends up measuring something other than REV). Prints DIR
# on success; returns non-zero with the reason on stderr.
gopi_baseline_worktree_add() {
    local dir="${1:?gopi_baseline_worktree_add needs a directory}"
    local rev="${2:?gopi_baseline_worktree_add needs a revision}"

    # THE PATH MUST BE FREE (scripts/lib/worktree-fresh.sh — the shell half of
    # internal/tools/worktree_fresh_path.go). This helper used to probe with
    # `mkdir -p "$dir"; rmdir "$dir"`, which is a DESTRUCTIVE shape wearing a
    # probe's clothes: on a pre-existing EMPTY directory the rmdir SUCCEEDS and
    # deletes it, and the add then recreates it — so the one case this whole
    # guard exists for (git silently reusing an existing empty directory) was
    # reached by first DELETING somebody else's directory. Refuse by name
    # instead; removing an occupant is a human's call.
    if ! gopi_assert_worktree_path_fresh "$dir"; then
        return 1
    fi

    if ! git worktree add --detach "$dir" "$rev" >/dev/null; then
        echo "baseline-verify: git worktree add --detach $dir $rev failed" >&2
        return 1
    fi
    if ! gopi_assert_baseline_clean "$dir" "$rev"; then
        echo "baseline-verify: the freshly created base $dir did not verify — do NOT trust a counter diff from it" >&2
        return 1
    fi
    printf '%s\n' "$dir"
    return 0
}
