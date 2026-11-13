# shellcheck shell=bash
# worktree-fresh-exempt: this FILE is the guard's own shell half, so it cannot
# source itself; its `git worktree add` appears only in this rationale. Every
# CREATOR calls gopi_assert_worktree_path_fresh (see the acceptance/fixture
# scripts), and this file is the citation that keeps the exemption visible.
#
# worktree-fresh.sh — THE SHELL HALF OF "A WORKTREE PATH MUST NOT EXIST BEFORE
# WE CREATE IT" (GOPI.md Rule 6a; the Go half is
# internal/tools/worktree_fresh_path.go, and this file is its DELEGATION, never
# a second rule).
#
# WHY IT EXISTS (measured). `git worktree add <path>` refuses a NON-EMPTY target
# ("fatal: '<path>' already exists") and SILENTLY REUSES an existing EMPTY one
# (rc=0, "Updating files: 100%", HEAD moved). A worktree's identity IS its path,
# so a reused path is a SHARED path: the motivating incident found
# ~/workspace/gopi-tasksync-switch carrying three different tasks' work. A
# fixture script that leaves an empty directory behind (a re-run after an
# aborted setup, a GC that emptied but did not remove) therefore hands the next
# run's checkout into the previous run's directory.
#
# Usage (source, do not execute):
#     . "$(dirname "$0")/lib/worktree-fresh.sh"
#     gopi_assert_worktree_path_fresh "$WT" || exit 2
#     git worktree add -q "$WT" "$BR"
#
# `-e` (not `-d`) and it does NOT follow symlinks meaningfully: a DANGLING
# symlink at the path is also an occupant, and `test -e` reports it as absent
# because it resolves the link. So the check probes `-e` OR `-L`, which together
# cover every occupant including a link whose target does not exist.
#
# NEVER removes anything: the whole point is that the operator inspects what is
# standing there. `mkdir -p … && rmdir …` (a "prove it is empty by creating and
# dropping it" probe) is exactly the shape this file bans — it DELETES a
# pre-existing empty directory, which is the shared path being inherited.

# gopi_worktree_path_exists DIR — 0 when anything occupies DIR, 1 when it is
# genuinely absent. Both spellings are needed: `-e` follows a symlink (a
# dangling link reads as absent) and `-L` catches it.
gopi_worktree_path_exists() {
    local dir="$1"
    [ -e "$dir" ] || [ -L "$dir" ]
}

# gopi_assert_worktree_path_fresh DIR — return 0 only when DIR does not exist at
# all. Any occupant (file, directory, symlink, dangling symlink) is REFUSED by
# name, with the remedy printed, and nothing is deleted.
gopi_assert_worktree_path_fresh() {
    local dir="${1:?gopi_assert_worktree_path_fresh needs a path}"
    if ! gopi_worktree_path_exists "$dir"; then
        return 0
    fi
    local what="an existing entry"
    if [ -L "$dir" ]; then
        what="a symbolic link"
    elif [ -d "$dir" ]; then
        what="a directory"
    elif [ -f "$dir" ]; then
        what="a regular file"
    fi
    echo "worktree-fresh: refuse to create a worktree at $dir — $what already exists there." >&2
    echo "  git worktree add silently REUSES an existing EMPTY directory (a worktree's identity is its path," >&2
    echo "  so a reused path is a shared path). Inspect it — it may be another run's live work — and remove" >&2
    echo "  it by hand, or choose another path. This guard never deletes it for you." >&2
    return 1
}
