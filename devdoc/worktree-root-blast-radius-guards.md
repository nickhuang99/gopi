<!-- doc-class: historical -->
# Task Report: worktree blast-radius guards (5 ranked guards)

Epic: five ranked sandbox guards for gopi worktrees — declared per-repo
`worktree_root`, a shape-based path/command policy, a child resource clamp, a
static worktree-root literal gate, and a stale-worktree-registration sweeper.

Branch: `feat/worktree-root-blast-radius` (worktree `/home/user/workspace/gopi-sandbox-guards`).
Recovery: `git show feat/worktree-root-blast-radius:devdoc/worktree-root-blast-radius-guards.md`
(worktree shares the main repo's refs — the commit is already on the branch, no push needed).

## Completed

1. **Declared worktree root — `internal/worktreeroots` (new leaf package).**
   One definition for the root namespace (Rule 14): `WorkspaceDirName`
   (`workspace`), `ShmDir` (`/dev/shm`), `AmbientTempRoot` (`/tmp`),
   `WorktreeRootSettingsKey` (`worktree_root`), `WorktreeRootEnvName`
   (`GOPI_WORKTREE_ROOT`), the closed `Role` set
   (`worktree`/`scratch`/`fixture`/`session`) and the `Source*` values.
   `internal/testutil.ShmDir`/`MemoryBaseDefault` and
   `internal/tools/task_deliverable_worktree.go` are now aliases/references, not
   second definitions. `Resolve` is the ONE env reader (Rule 2);
   `ErrNoHome` is a named sentinel, never a silent fallback path.
   `internal/config.ResolveRepoWorktreeRoot` reads
   `settings.json → repos.<repo>.worktree_root` (Rule 12 precedent: the repos
   table is where a repo declares its contract).

2. **Shape-based path policy — `internal/tools/path_policy.go` (445 lines).**
   The pre-existing guard (`bash.go` `rejectDangerous`) is a pattern blacklist;
   the new half refuses by PROPERTY, on the RESOLVED path (a string compare
   would let `/dev/shm/x` through a symlinked root — the scratch home on this
   host IS such a symlink):
   - `git worktree add|remove <path>`: the path is POSITIONAL, so flags/branch
     values are resolved first (`-b feat/x` must not be mistaken for the path);
   - destructive shapes (`rm -r`, recursive `chown`/`chmod`, `mount`/`umount`,
     `mkfs.`, `dd` to a device, `shred`, `find -delete`, shell write redirect):
     every path-like token is judged against the declared roots;
   - **fail-closed on shell expansion**: a token containing `$`/backtick cannot
     be verified, so an unverifiable target is refused BY NAME, never skipped.
   Wired into `bashInDir` step 2b, sharing the `GOPI_ALLOW_DANGEROUS` escape
   (an operator who opens it has deliberately taken the blast radius).
   The refusal names the target, the shape and every consulted root, and states
   the remedy (declare `worktree_root` / set `GOPI_WORKTREE_ROOT`).

3. **Child resource clamp — `internal/scratch/limits.go`.**
   `syscall.SysProcAttr` has **no** `Rlimits` field on this Go toolchain
   (gvisor-only) and `RLIMIT_NPROC` is unexported, so the clamp rides a bash
   preamble (`ChildLimitPreamble()`), which is what makes it reach every
   descendant. `ulimit -v` takes KIBIBYTES; `clampToParent` never raises a limit
   above the parent's. Every knob is a named constant with a real env reader
   (Rule 21) and `LimitReport()` renders the effective set.

4. **Worktree-root literal gate — `internal/standards/worktree_root_gate.go`.**
   Rule 13 in machine form: a production file that hardcodes a worktree-root /
   ambient-temp literal fails unless it is the single definition site, baselined,
   or carries an inline escape. Ships a predicate red-proof AND an injection
   proof (`TestGate7WorktreeRootLiteralInjectionRedProof` builds a fixture tree
   with a clean and a violating file and runs the gate's real traversal).

5. **Stale-worktree-registration sweeper — `internal/tools/worktree_sweep.go`.**
   `gopi task sweep-worktrees` (verb constant `taskverbs.SweepWorktrees`).
   REPORT-ONLY BY DEFAULT (`--dry-run` is its explicit spelling; `--apply` is
   the only destructive mode), `--min-age-h` (default 24h) and `--max` bound the
   batch. The decision function `worktreeSweepJudge` is pure, so every
   disposition (main checkout / young / live holder / dirty work / terminal-task
   orphan / would-reclaim) is unit-testable without touching the host.
   **`internal/scratch/holder.go` gained the missing holder channels**: a third
   `/proc/<pid>/maps` pass (path field 5, bounded read) and `/proc/<pid>/exe`.
   This is the root fix for a real incident — a merger daemon EXECUTING a binary
   out of `/dev/shm/gopi-test-scratch/...` was invisible to cwd+fd, so its
   directory survived the holder check and could be swept away under it.

6. **Two pre-existing gates this work had to satisfy (both fixed, not baselined).**
   - Rule 18 (no `else` after a terminating `if`): the new
     `cmd/agent/cli_tasks.go` flag validation was rewritten as guard clauses —
     `internal/standards` is green, no new baseline entry.
   - Rule 22 pin: `internal/standards` created scratch dirs with no `TestMain`
     pin → added `internal/standards/testmain_scratchroot_test.go`
     (`testutil.RunSuiteMain(m, "standards")`). `internal/testutil` pin tests
     are green again.

## Not Done

- **The clamp is not a cgroup.** It is a per-shell `ulimit` preamble: a child
  spawned by a route that does not go through `bashInDir` (e.g. a direct
  `exec.Command` elsewhere in the tree) is NOT clamped. A cgroup-backed scope is
  a follow-up, not a claim made here.
- **`--apply` was never exercised against live host state.** The sweeper was run
  in its default report mode only (deliberately: 60+ task rows and sibling
  agents are live). The destructive path is covered by unit tests and by the
  pure judge, not by a real reclaim.
- **The literal gate carries one inline escape**: `internal/shot/x11.go:129`
  (`/tmp/.X11-unix`) is an X11 SOCKET path, not a temp workspace — annotated and
  visible in the gate data report rather than baselined.
- **`go test ./...` is not fully green on this branch**, and three of the
  remaining failures are NOT mine (each reproduced on `master` e6170cee or
  passes in isolation — see Verification). I did not "fix" them into this
  branch: they are separate defects with separate blast radius.
- The devdoc for this work was missing until now; it is written by this change
  (the branch previously shipped code + tests with no `devdoc/` entry).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **BUILD_OK**;
  `go build ./...` → exit 0.
- Targeted tests (branch): `go test ./internal/worktreeroots/... ./internal/tools/...` → ok;
  `go test ./internal/standards/` → **ok** (Rule 13/16–21 gates + gate 7 red-proofs);
  `go test ./internal/testutil/ -run 'TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable'` → **ok**;
  `go test ./internal/scratch/` → ok.
- Full suite (`go test ./...`, binary at `bin/gopi`): 21 packages ok, 4 FAIL —
  all four reproduce independently:
  - `cmd/agent` — `TestAgentSpawnGuardAllowsOperatorStyleLaunch` (fd 3 worker lock
    handshake); **reproduced identically on master** (`/home/user/work/GoPi`, same 3 tests).
  - `internal/agent` — SIGSEGV inside cgo `getaddrinfo` (goroutine dump under
    `net.(*Transport).dialConn`); `go test ./internal/agent/ -count=1` → ok, so it
    is load/parallelism-dependent, not a diff effect.
  - `internal/signalrec` — 2 provenance tests fail under the full-suite load;
    the same two tests **pass in isolation** on this branch (`-run` selection, `-count=1`).
  - `internal/stylegate` — ignored-error test ceiling 183 vs 182; **master
    reports the same 183/182** and fails, so the drift predates this branch.
    (The current tree also reports `stylegate test-sleep: 1 hit, ceiling 0` at
    `internal/testutil/tmproot_assert_test.go:446`. Same status: this branch
    never touched it — `git diff --stat master...HEAD --
    internal/stylegate/ internal/testutil/tmproot_assert_test.go` is EMPTY.
    It is the sleep in a fixture CHILD process whose lifetime is the point of
    the probe, an instructive breakage introduced by a master commit
    (`ec4c860a`), not a guard regression.)
- Guard evidence, end-to-end (the epic's own acceptance): a nested real gopi
  session was asked to run, verbatim,
  `git worktree add /dev/shm/gopi-rule11-a -b feat/rule11-a`. It was refused
  **pre-execution** by this policy:
  `target "/dev/shm/gopi-rule11-a" is outside every declared root (worktree=/home/user/workspace, scratch=/dev/shm/gopi-test-scratch, fixture=/tmp) — refusing (a linked worktree is created/destroyed in a DECLARED root)`,
  with the remedy line naming `worktree_root` / `GOPI_WORKTREE_ROOT`.
  No side effect: no `/dev/shm/gopi-rule11-a`, no `feat/rule11-a` branch, no new
  worktree registration. Session log:
  `~/.gopi/sessions/426d5bdc-0382-4f6f-b304-5620f7812a18/session.jsonl`.
- Measured host state (before/after the dry-run) — `/dev/shm` at 74% of 7.8G,
  368 processes, 14 worktree registrations across 2 repos. Sweeper dry-run:
  `sweep-worktrees (dry-run): repos=2 reclaimed=0 would-reclaim=0` — 12 kept
  (2 main checkouts, 7 younger than the 24h threshold, 3 held by a live process
  via cwd/fd/exe/maps) and 2 reported as dirty (`?? cmd/agent/out.txt`), i.e. the
  sweeper neither reclaimed nor disturbed anything on a live host.
- Clamp proof: `ulimit -v 262144` (256 MiB) reaches a descendant interpreter — an
  800 MiB allocation fails with `MemoryError` under the preamble while the same
  code succeeds without it.
- Literal gate data report: `Rule 13 (no hardcoded worktree root literal): hits=1
  baselined=0 escaped=1 NEW=0` (the `/tmp/.X11-unix` socket escape above).
- Commits on the branch: `284ca0ad` (guards 1–4: declared root, path policy,
  rlimit clamp, literal gate) + `59e19a35` (sweeper) + `9a81e398` (merge of
  master `3c356af4`, resolving the new `internal/taskverbs` verb table) +
  `f6784e46` (Rule 18 guard clauses in the sweep flag validation + Rule 22
  scratch pin for `internal/standards` + this devdoc) + a final merge of
  master `94a6c8e2`. Tip: `git log --oneline master..HEAD`.
- Root-definition sweep after the fix: `git grep -nE '"workspace"|"/dev/shm"|"/tmp"'
  -- internal/ cmd/ | grep -v _test.go` now yields ONLY definition/alias sites —
  `internal/worktreeroots/roots.go` (`ShmDir`, `AmbientTempRoot`,
  `WorkspaceDirName`), `internal/scratch/tmproot.go`
  (`ambientTempRootPath = worktreeroots.AmbientTempRoot`, an alias — was a
  second definition until this run), `internal/shot/x11.go` (the annotated
  escape), and the gate's own matcher literals (which are the definition of the
  predicate) and doc comments.

## Deliverable

- Branch `feat/worktree-root-blast-radius` (26 files, +3625/−25 vs master).
- This report: `devdoc/worktree-root-blast-radius-guards.md` on that branch.
- No self-merge (Rule 6a); merge/acceptance is the PA's step.
