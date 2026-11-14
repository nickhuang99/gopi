<!-- doc-class: historical -->
# Task Report: a worktree path must not exist before we create it

Task `01a0c53c-b770-7b66-a5d4-39d9e4e7cfc4` — BUG: a worktree path is not proven
fresh; git silently REUSES an existing EMPTY directory (non-empty is refused,
empty is not).

Branch: `fix/worktree-path-must-not-exist`
Worktree: `/home/user/workspace/gopi-wtfreshpath`

## Completed

### The defect, measured in the report itself

```
$ mkdir -p <path> && touch <path>/EXISTING ; git worktree add <path> -b probe/a
fatal: '<path>' already exists                       # REFUSED   (non-empty)
$ mkdir -p <path2>                            ; git worktree add <path2> -b probe/b
Preparing worktree (new branch 'probe/b')
HEAD is now at cd42df7 fixture                       # rc=0      (EMPTY → SILENTLY REUSED)
```

A worktree's identity IS its path, so a reused path is a SHARED path. The
motivating incident found `~/workspace/gopi-tasksync-switch` carrying three
different tasks' work.

### 1. The predicate — `internal/tools/worktree_fresh_path.go` (new)

`worktreeFreshPath(path) error` / exported `CheckWorktreePathFresh` (a
delegation, not a second implementation — Rules 13/14):

- `os.Lstat`, **never** `os.Stat`: a DANGLING SYMLINK must be refused too, or
  the checkout is written *through* the link.
- Any occupant — directory, regular file, symlink, dangling symlink — is refused
  by a NAMED error: `ErrWorktreePathExhausted` carrying the marker
  `worktree-path-not-fresh`, the path, the KIND of occupant, and ONE remedy
  sentence.
- Absent → `nil`. Empty/unresolvable path → refused (fail-closed).
- **It never deletes anything.** Removing an occupant is a human's call (the
  remedy sentence says so).

It adds exactly ONE predicate: *existence before creation*. The pre-existing
guards are all POST-existence and are cited, not duplicated or weakened:

| already owned by | asks |
|---|---|
| `task_reclaim*.go` (live-holder veto), `task_finish_cleanup.go` (`finishCleanupRemovalStateReason`, `taskLiveClaims`) | is an EXISTING directory held by a live run? |
| `worktree_sweep.go` (registration/lock/lock-age/orphan) | is an EXISTING directory locked, registered, orphaned? |
| `path_policy.go` `judgeTarget` + `worktreeroots` | may a worktree live at this path at all? |

No new config knob, env var, or flag: the guard is not optional.

### 2. Wiring — every creation site

Go call sites (all via the ONE predicate):

| file:line | site |
|---|---|
| `internal/tools/task_run_ground.go:389` | `taskGroundCreate` — the producer ground |
| `internal/tools/tasks.go:6642` | `taskVerifyGround` private ground (fresh by construction; §Exceptions) |
| `internal/tools/merge_build_gate.go:545` | build-gate checkout (`rev`-derived name) |
| `internal/tools/rebuild_vet_mergeset.go:369` | mergeset measurement checkout |
| `internal/tools/rebuild_vet_mergeset.go:419` | mergeset BASE arm (refuses the base arm only — conservative) |
| `internal/tools/rebuild_vet_revert.go:248` | baseline-at-rev checkout |
| `internal/tools/task_finish_premerge.go:472` | merged-state checks checkout |
| `internal/tools/path_policy.go:250` | the **bash-tool** spelling of `git worktree add` |
| `cmd/agent/merge_run.go:1794` | merge probe worktree |
| `cmd/agent/merge_union.go:413` | union-resolution probe worktree |

`path_policy.go` needed a new `worktreeTarget.create` field so the predicate
applies to `worktree ADD` only — a `git worktree remove <path>` names a
directory that is SUPPOSED to exist, and checking it would refuse every
legitimate removal. `judgeTarget` (the root judgement) is untouched.

Shell half — `scripts/lib/worktree-fresh.sh` (new): `gopi_worktree_path_exists`
(probes `-e` **or** `-L`) and `gopi_assert_worktree_path_fresh`. Sourced by
`scripts/acceptance-self-release.sh`, `acceptance-self-release-stop.sh`,
`evidence-dirty-closeout.sh`, `nonff-able-cohort-after.sh`,
`repro-live-holder-worktree-veto.sh`, `scripts/lib/baseline-verify.sh`, plus the
three `devdoc/` demos below.

**Wiring audit is a test, not a promise**:
`TestWorktreeFreshShellIsSourcedByEveryCreator` walks `scripts/` **and**
`devdoc/` and fails any script running `git worktree add` without the guard.
Measured output: `read 145 script(s) across [scripts devdoc] and found 10
worktree creator(s)`.

### 3. Two defects found ON THE WAY (fixed, not deferred)

- **`scripts/lib/baseline-verify.sh` was DESTRUCTIVE.** Its probe was
  `mkdir -p "$dir"; rmdir "$dir"` — on a pre-existing EMPTY directory the
  `rmdir` SUCCEEDS and deletes it. So the one case the guard exists for was
  reached by first DELETING somebody else's directory. Replaced with the
  name-and-refuse helper.
- **The audit's own scope was partial.** The first version walked `scripts/`
  only; the very next read found THREE more creators under `devdoc/`
  (`demo-park-worktree-release.sh:59`, `wip-worktree-guard-e2e.sh:44`,
  `worktree-cwd-release-coverage/run.sh:74`), all hand-rolling
  `git worktree add`. All three now carry the guard, and the audit's root list
  is the two directories (a scoped audit is how a guard stays partial).

### 4. Exceptions — REPORTED, never silently skipped

| site | why it deliberately does not carry the guard |
|---|---|
| `scripts/lib/worktree-fresh.sh` | it IS the guard's shell half; sourcing itself is impossible. Carries an inline `worktree-fresh-exempt:` note, and the audit PRINTS it as a reviewed exemption. |
| `internal/tools/tasks.go:6642` (`taskVerifyGround`) | `wtDir` sits under a directory this run just created (`scratch.MkdirTemp`), so it is fresh by construction and the check can only pass. It carries the predicate anyway — every creation site does, and if it ever fires, the scratch class handed out a non-empty directory, which is itself worth refusing on. |
| the `rm -rf "$D"` in the three `devdoc/` demos | each removes a path IT named and owns before the guard runs; the guard sits immediately before the `add`. Stated in each script. |

No site deliberately reuses a path.

## Not Done

- **The three `devdoc/` demo scripts were not re-executed end to end.** They
  need a pre-fix and a post-fix binary plus a scratch world; `bash -n` passes on
  all three and each guard call is the same sourced helper executed for real in
  `TestWorktreeFreshShell*`. An honest gap, named rather than claimed green.
- **No `os.RemoveAll`-based "make room" path was added** — deliberate: a human
  decides what an occupant was.
- **The corpus of `git worktree add` invocations in `devdoc/*.sh`/`testsuite`
  is gated only for `scripts/` + `devdoc/`.** A Python- or Go-written creator
  outside those roots is not covered by the shell audit (the Go sites are
  gated by the wiring table above, by review and by the tests, not by a
  predicate over `exec.Command("git", …)`); a general Go-side audit is not in
  this diff.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0
- **Vet:** `go vet ./internal/tools/ ./cmd/agent/ ./internal/testutil/ ./testsuite/` → exit 0
- **Tests:** `go test ./internal/tools/ ./internal/testutil/ ./internal/standards/ -count=1` → see `## Evidence`
- **Ran (Rule 31 — the real surface, real arguments, real state):**

```
=== 0) RAW GIT CONTROL — the case git silently reuses ===
Preparing worktree (new branch 'feat/control')
HEAD is now at cd42df7 fixture
raw git rc=0   <-- 0 = REUSED, no complaint
the 'empty' dir now holds: 1 entries

=== 1) SHELL GUARD (scripts/lib/worktree-fresh.sh) — existing EMPTY dir ===
worktree-fresh: refuse to create a worktree at .../declared-root/gopi-collide — a directory already exists there.
  git worktree add silently REUSES an existing EMPTY directory (a worktree's identity is its path,
  so a reused path is a shared path). Inspect it — it may be another run's live work — and remove
  it by hand, or choose another path. This guard never deletes it for you.
guard rc=1  (1 = refused)

=== 2) SHELL GUARD — FRESH (absent) path ===
guard rc=0  (0 = free)

=== 3) GO PREDICATE via the exported entry the merge faces call ===
existing empty dir  -> worktree path .../gopi-collide already exists (a directory): worktree-path-not-fresh — choose another path, or inspect the existing entry (it may be another run's live work) and remove it by hand before retrying — this guard never deletes it for you
errors.Is(named)    -> true
absent path         -> <nil>
```

- **Ran (Rule 31 — the real binary, through the real bash tool):**
  `go test ./testsuite/ -run 'TestE2E_BashToolRefusesWorktreeAddIntoExistingEmptyDirectory|TestE2E_BashToolAllowsWorktreeAddOnAFreshPath' -count=1 -v`
  → both PASS. The refusal the MODEL would read, quoted from the session record:

```
Bash error: dangerous command rejected: worktree path .../declared-root/gopi-collide already exists
(a directory): worktree-path-not-fresh — choose another path, or inspect the existing entry
(it may be another run's live work) and remove it by hand before retrying — this guard never
deletes it for you
```
  and the green half on the same invocation at an absent path:
```
exit=0
Preparing worktree (new branch 'feat/fresh')
HEAD is now at 7e400a7 fixture commit
```

- **Commit:** see the branch tip (this report is committed with the work).

## Files

| path | what |
|---|---|
| `internal/tools/worktree_fresh_path.go` | NEW — the predicate, its named error, the remedy sentence |
| `internal/tools/worktree_fresh_path_test.go` | NEW — unit tests (absent/empty/non-empty/file/dangling link/empty path) + the two bash-tool entry-point tests |
| `scripts/lib/worktree-fresh.sh` | NEW — the shell half (`gopi_worktree_path_exists`, `gopi_assert_worktree_path_fresh`) |
| `internal/testutil/worktree_fresh_shell_test.go` | NEW — drives the shell helper for real + the wiring audit over `scripts/` and `devdoc/` |
| `testsuite/worktree_fresh_path_e2e_test.go` | NEW — Rule 31: the real binary, the real bash tool, both directions |
| `devdoc/worktree-path-must-not-exist.md` | NEW — this report |
| `internal/tools/task_run_ground.go`, `tasks.go`, `merge_build_gate.go`, `rebuild_vet_mergeset.go`, `rebuild_vet_revert.go`, `task_finish_premerge.go`, `path_policy.go` | the Go creation sites + the `create` flag that scopes the predicate to `worktree add` |
| `cmd/agent/merge_run.go`, `merge_union.go` | the two merge probe faces |
| `scripts/acceptance-self-release.sh`, `acceptance-self-release-stop.sh`, `evidence-dirty-closeout.sh`, `nonff-able-cohort-after.sh`, `repro-live-holder-worktree-veto.sh` | the shell creation sites |
| `scripts/lib/baseline-verify.sh` | the destructive `mkdir -p …; rmdir …` probe replaced by the guard |
| `devdoc/demo-park-worktree-release.sh`, `devdoc/wip-worktree-guard-e2e.sh`, `devdoc/worktree-cwd-release-coverage/run.sh` | the three `devdoc/` creators the first audit missed |
| `internal/testutil/baseline_gate_test.go` | a third RED case: an unfresh base path is refused AND survives |

## Evidence (self-filed)

- **RED-before / GREEN-after, both observed** (`TestTaskGroundCreate_RefusesExistingEmptyDirectory`,
  guard clause deleted from `task_run_ground.go` for the RED run, then restored):
```
--- guard clause removed; running the empty-dir test (expect FAIL) ---
--- FAIL: TestTaskGroundCreate_RefusesExistingEmptyDirectory (0.18s)
    worktree_fresh_path_test.go:169: taskGroundCreate reused the pre-existing EMPTY directory
    .../gopi-task-ground (git lets this through)
FAIL	github.com/nick/gopi/internal/tools	0.707s
--- restored; rc of the RED run = 1 (non-zero = the test caught it) ---
 internal/tools/task_run_ground.go | 10 ++++++++++
--- and GREEN again after restore ---
ok  	github.com/nick/gopi/internal/tools	0.696s
```
- **Unit tests, observed PASS** (`-run 'TestWorktreeFreshPath|TestTaskGroundCreate_RefusesExistingEmptyDirectory|TestBashEntryPoint'`):
  `AbsentPathIsNil`, `EmptyDirectoryIsRefused`, `NonEmptyDirectoryIsRefused`,
  `RegularFileIsRefused`, `DanglingSymlinkIsRefused`, `EmptyPathIsRefused`,
  `TestTaskGroundCreate_RefusesExistingEmptyDirectory`,
  `TestBashEntryPoint_RefusesWorktreeAddIntoExistingEmptyDir`,
  `TestBashEntryPoint_AllowsWorktreeAddOnAFreshPath` — 9/9 PASS, `ok
  github.com/nick/gopi/internal/tools 1.042s`.
- **Shell guard tests, observed PASS**: `TestWorktreeFreshShellRefusesExistingEmptyDirectory`,
  `TestWorktreeFreshShellRefusesDanglingSymlink`, `TestWorktreeFreshShellAllowsAbsentPath`,
  `TestWorktreeFreshShellIsSourcedByEveryCreator` — 4/4 PASS, `ok
  github.com/nick/gopi/internal/testutil 0.453s`. The last one printed:
  `the audit read 145 script(s) across [scripts devdoc] and found 10 worktree creator(s)`.
- **E2E, observed PASS**: `ok github.com/nick/gopi/testsuite 10.811s` (both tests).
- **`go vet` on the four touched packages**: exit 0, no diagnostics.
- **The pre-existing empty directory SURVIVES every refusal** — asserted in the
  unit test (`os.Lstat` still resolves, `os.ReadDir` returns 0 entries), in the
  E2E test, and in the shell test.
- The `## Files` list below is the `git status --porcelain` of the worktree
  before the commit.
