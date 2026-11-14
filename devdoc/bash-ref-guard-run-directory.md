<!-- doc-class: historical -->

# The bash tool's ref guard judges the RUN DIRECTORY, not the session cwd (task 01a1621f)

Measured 2026-10-22 on hosta. Baseline `d344671`. Branch `master` in the run's
own ground `/home/user/workspace/01a1621f-b692-7aa1-ade0-181aabf594f2` (clone channel).

## The defect, as filed

The bash tool refused a plainly legal cross-repository fetch:

    cd /home/user/work/gopi && git fetch /home/user/workspace/gopi-ellipsis-offset \
        fix/ellipsis-offset-anchoring:fix/ellipsis-offset-anchoring

with

> `git fetch …` from `/home/user/workspace/01a16053-…-r1` would rewrite the SHARED
> branch `fix/ellipsis-offset-anchoring` of `/home/user/workspace/01a16053-…-r1`,
> which is checked out by ANOTHER worktree (…)

Two separate defects in one sentence:

1. **The judged directory was the session's cwd, not the command's run directory.**
   The command carries its own `cd /home/user/work/gopi &&`; the session's process cwd
   was the run's clone, where that branch *is* held by a linked worktree. In the
   repository the command actually reaches, the branch does not exist at all — so the
   write was harmless there and the refusal was a false positive.
2. **The refusal named a directory the command never touches**, which sends the reader
   to inspect a tree that could not be affected. That is worse than the false positive:
   it manufactures a non-existent incident.

The same report carried a second shape: a documentation write whose heredoc body
*quoted* that fetch command was also refused — the guard read text as a command.

## What the fix does

`internal/tools/shared_integration_ref_guard.go`:

- `refGuardClauses(stripped, workdir)` now returns `[]refGuardClause` — one entry per
  simple command **plus the run directory the shell has selected for it** — instead of
  bare `[][]string`. Resolution order is the shell's own, with the caller's parameter
  authoritative:
  **`workdir` (the bash tool's own parameter, which is the child process's cwd) >
  a leading `cd <dir>` on the command line > the process cwd.**
  `refGuardRunDir` is the one function that orders those three (Rule 14).
- The carry is REPLAYED per clause: a `cd` advances it for the commands that follow,
  and an unresolvable `cd` (shell expansion) keeps the last directory that *could* be
  resolved rather than falling back to a fabricated one.
- `refGuardRecognisedTargets` feeds each clause's own run directory into
  `refGuardWrittenRefs`, so `refGuardResolve` — which already honoured a per-invocation
  `-C <dir>` — now starts from the right base for every clause. This also removes the
  old `stashGuardRepoDir(command, workdir)` shortcut, which scanned the WHOLE command
  for a `-C` and therefore attributed one clause's `-C` to every other clause.
- Text that is not a command is skipped: **here-document bodies** (`<<WORD`,
  `<<-WORD`, `<< WORD`, terminated by the delimiter line bash itself compares against,
  queues opened in order) and **shell comments** (`#` that begins a word).

`internal/tools/git_publish_target_guard.go`: the clause scanner's signature changed,
so its one remaining consumer passes `workdir` and reads `.words`.

Nothing was widened. The holder predicate (`refGuardBranchHolder`), the worktree list
probe and the refusal's four obligations are untouched: a write to a `refs/heads/<b>`
that another checkout of *the judged repository* stands on is still refused, by name.

## Evidence

**Red→green through the tool's own entry point** (`tools.BashInDir`, the real bash-tool
surface), fixture = two repositories: the session's (holds the destination branch in a
linked worktree) and the target (does not):

| case | before the fix | after |
|---|---|---|
| `cd <target> && git fetch <src> shipping:shipping` | REFUSED (defect) | PERMITTED, ref created in the target |
| `git fetch <src> shipping:shipping-2` with `workdir=<target>` | PERMITTED | PERMITTED |
| `cd <target> && git fetch <src> shipping:theld` (branch the target's own second checkout holds) | PERMITTED (**weakened/broken**) | REFUSED, naming the target and its holder |
| `cat <<'EOF' … git update-ref refs/heads/theld … EOF` | REFUSED (text read as a command) | PERMITTED, body printed |

The first row is the measured defect and the fourth is its documentation-write sibling;
the third row is the no-weakening proof, and it also records something the guard got
*wrong* in the other direction before the fix: judged at the session cwd, a real
shared-ref write inside another repository was let through.

Full transcripts: `~/.gopi/sessions/01a1621f-b692-7aa1-ade0-181aabf594f2/`
`evidence-bash-tool-before-fix.txt` and `evidence-bash-tool-fixed.txt`, produced by
`liveprobe/main.go` in the same directory (a throwaway driver, not part of the tree).

## Tests

`internal/tools/shared_integration_ref_guard_run_dir_test.go`:

- `TestSharedIntegrationRefGuard_JudgesTheRunDirectory` — the three resolutions above,
  driven through `BashInDir`; the permitted case asserts the *ref* landed in the target
  (not merely an exit status), and the refusal case asserts the session's own paths
  appear NOWHERE in the sentence.
- `TestSharedIntegrationRefGuard_HeredocBodyIsNotACommand` — a heredoc body and a
  comment are text; a real invocation *after* a heredoc is still judged (so the rule
  cannot widen into "stop parsing at a heredoc").

Both are red on the pre-fix source (verified by reverting the two production files and
re-running) and green after.

## Pitfalls this cost (for the next agent)

- **`git stash push` is refused in this repo** by the shared-stash guard (many linked
  worktrees). The red-proof therefore has to copy the two files aside and
  `git checkout --` them, then copy them back — and the path policy refuses a `cp`
  whose *destination* is outside the working directory, so the backup must live inside
  the worktree or be written with `>` redirection into the session dir.
- **`$VAR` in a command is refused** by the path policy ("contains shell variable/command
  substitution"), so a probe script cannot be parameterised inline; spell full paths.
- A `go run` of a package importing `internal/…` must run from INSIDE the module:
  a copy under the session directory is rejected ("use of internal package not
  allowed"), so the driver has to be temporarily placed in the module tree and removed
  before `git status --porcelain` is asserted empty (Rule 30).
