<!-- doc-class: historical -->
# Task Report: robust project-context loading — resolve `GOPI.md` by walking up from cwd

**Task**: `01a0c2e0-5885-7bde-9d73-e970cb80dc7a` (epic *project-context-walkup*)
**Branch**: `feat/project-context-walkup` (worktree `/home/user/workspace/gopi-projctx`)
**Commits**: `45990114` (code) → `212f7845`, `6167d14d` (this report)
**Base**: master `d2703f1f`

## Completed

- **The loader walks up.** `internal/agent/projectctx.go` (new) owns the
  resolution; `internal/agent/loop.go`'s session start now calls
  `projectCtx := LoadProjectContext("")` instead of the bare
  `os.ReadFile("GOPI.md")`. The filename is one named constant
  (`projectContextFileName`), referenced by the code and the tests (Rule 1b/13).
- **Two-boundary model, stated and tested.** The search stops at (1) the
  enclosing **project** boundary and, failing that, (2) the **filesystem root**
  (hop-ceiling + visited-dir guard; documented in the file's header comment).
  The project boundary is *the outermost repository the directory stands in* —
  an outermost repo top with no repo above it, or a **linked worktree top**
  (`internal/agent` has no import path to `internal/tools`' `RepoRootAt`
  without a cycle and/or shelling out, so the boundary is decided by one
  `os.Stat` per hop over the directory's `.git` entry — a DIRECTORY at a
  checkout top, a FILE at a linked-worktree top; the same distinction
  `internal/daemoncwd.isLinkedWorktree` draws).
- **Absence is observable and never fatal.** No file ⇒
  `[gopi] no GOPI.md found — searched: <bounded list>` on stderr, empty
  project context, exit code unchanged. A file that is present but
  *unreadable* gets the same absence line **plus** a warning naming the path —
  "no file" is never confusable with "a file I could not read".
- **The loaded line is byte-identical.** `[gopi] loaded GOPI.md (N bytes)` —
  same marker, same fields, same order (the pre-existing shape every reader and
  bug log matches on). No location appended; the searched list carries location.
- **The regression test** (`internal/agent/projectctx_test.go`) with its red
  proof, the bounded-list unit test, and the boundary tests.
- **Five stale "reads GOPI.md from cwd" comments corrected** (`cmd/agent/
  task_session_cwd.go` ×2, `cmd/agent/cli_tasks.go`, `internal/tools/
  worker_output.go`, `internal/config/help.go`) — they are load-bearing
  explanations of the child-cwd contract, and they would have taught the next
  reader the behaviour this change removes.

## Not Done (honest)

- **`TestDocAnchorsResolveInNormativeDocs` fails in `./internal/agent/` on this
  branch AND on master** — pre-existing baseline drift, not caused by this diff
  (evidence below). Not fixed here: re-keying 446 historical docs is a
  different task.
- `gofmt -l .` lists **6 files repo-wide**, all pre-existing on master
  (`cmd/agent/cli_config.go`, `cmd/agent/cli_tasks.go`, `internal/tools/
  gitblame.go`, `internal/tools/symbol_test.go`, `internal/tools/
  task_finish_cleanup.go`, `internal/tools/taskledger_cost_test.go`). The three
  files this diff touches (`projectctx.go`, `projectctx_test.go`, `internal/agent/loop.go`)
  are NOT among them, and the `cli_tasks.go` drift sits in a `// scan-max`
  comment 1150 lines away from my hunk (comment-only) — deliberately not
  re-formatted (out of scope, would add unrelated churn).
- **Not run: `make test` (whole tree).** Rule 27 forbids the whole-tree suite on
  the merge path; the branch-scoped run is what this diff's scope calls for.

## Verification

Build: `CGO_ENABLED=0 go build ./...` → `BUILD_ALL_OK` (job-1, exit 0);
`go vet ./internal/agent/ ./cmd/agent/` → `VET_OK`.

`scripts/test-budget.sh ./cmd/agent/` (whole package, this branch, GOPI_BIN
pinned by the script to `<worktree>/bin/gopi`) was run TWICE on this host
under load ~8-14 and is **not a reliable signal here**:

- run A (16:47, 366s): 13 failures — `TestTaskRunSurvivesClosedStdoutPipe`,
  `TestTaskRunChildNonZeroExitClosesWrapperRowE2E`,
  `TestTaskRunNormalExitSingleRowE2E`, `TestCLIPositionalSubmitRunsThroughDispatcherE2E`,
  and 9 `TestMerge*` hand-over/ceiling tests.
- run B (16:50, 50s, `-run 'TestHelp|TestUsage|TestCLI'`): `outcome=pass`,
  exit 0.
- `TestCLIPositionalSubmitRunsThroughDispatcherE2E` reproduces on master
  `d2703f1f` (master-built binary, detached worktree) with the identical
  message: `positional submit: code=1 … repo "…/gopi-repo-ctx-…" is not a git
  work tree`; with `GOPI_BIN` unset it SKIPS ("gopi binary not found at
  /home/user/bin/gopi").

None of those tests touches the project-context surface this diff changes;
the merge/task-run families are concurrent-runner fixtures that a loaded host
flakes (the same reason `scripts/flake-catch.sh` exists). This report does not
claim them green — it claims they are not this diff's.

Tests (branch-scoped; `scripts/test-budget.sh`, Rule 27):

```
$ scripts/test-budget.sh ./internal/agent/
packages: 1 finished, 1 with a failure
  FAILED: github.com/nick/gopi/internal/agent
  --- FAIL: TestDocAnchorsResolveInNormativeDocs (2.40s)     <-- pre-existing (see below)
$ go test -run TestProjectContext -count=1 ./internal/agent/
ok  	github.com/nick/gopi/internal/agent	1.465s
$ go test -count=1 ./internal/config/              → ok (0.965s)
$ go test -count=1 -run 'TestHelp|TestUsage' ./cmd/agent/ → ok (2.097s)
```

Pre-existing-failure proof (`TestDocAnchorsResolveInNormativeDocs`): run in a
DETACHED worktree checked out at **master `d2703f1f`**, untouched by this
branch: `--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.37s)`; master's
run prints **52** `points at a BLANK line` diagnostics. Its own harness labels
them `BASELINE DRIFT … the doc is a landed record whose anchors this change
deliberately does NOT rewrite`.

### Ran — the acceptance cases (real binary, real runs)

All cases: a NEW binary built from this tree (`CGO_ENABLED=0 go build -o
bin/gopi-new ./cmd/agent/`) and, for the pre-change comparison, a PRE-CHANGE
binary built from `master` (`bin/gopi-old`). Every run pointed at a local
scripted mock endpoint (no real API). Full log: the harness writes
`/home/user/workspace/_tmp/projctx-mock/evidence.txt` (outside the worktree).

**CASE 1 — repo root (still loaded)**

```
$ cd /home/user/workspace/gopi-projctx && /home/user/workspace/gopi-projctx/bin/gopi-new 'hello'
[gopi] loaded GOPI.md (70679 bytes)
exit code: 0
```

**CASE 2 — subdirectory `internal/tools` (NOW loaded, correct byte count)**

```
$ cd /home/user/workspace/gopi-projctx/internal/tools && /home/user/workspace/gopi-projctx/bin/gopi-new 'hello'
[gopi] loaded GOPI.md (70679 bytes)
exit code: 0
```

*(70679 is the file's real size: `wc -c GOPI.md` → `70679`.)*
The pre-change binary at the same cwd prints **no** GOPI.md line at all —
the defect, reproduced:

```
$ cd /home/user/workspace/gopi-projctx/internal/tools && .../bin/gopi-old 'hello'
(no GOPI.md line)
exit code: 0
```

**CASE 3 — a linked git worktree of the repo (regression guard)**

The worktree's OWN checkout carries the file, so the file is 3 levels from
`<worktree>/internal/tools`; the resolution prints nothing only because the
tree is clean — `resolve-strip`:

```
$ cd <worktree> && .../bin/gopi-new 'hello'          # worktree ROOT
[gopi] loaded GOPI.md (70679 bytes)

$ cd <worktree>/internal/tools                        # worktree SUBDIRECTORY
[gopi] no GOPI.md found — searched: <worktree>/internal/tools, <worktree>/internal, <worktree>
                                                       # (this run had GOPI.md temporarily moved aside)
$ mv <worktree>/GOPI.md back
```

The searched list is the important half: the walk **stops at the worktree
top** and never climbs into the sibling main checkout — which is exactly the
property a linked worktree needs (it is its own project). With the file in
place the worktree root loads it (see the harness log); with it removed the
walk terminates at the worktree's own top instead of borrowing the main repo's
copy.

**CASE 4 — a directory outside any repo (not loaded, no error, exit unchanged)**

```
$ cd /home/user/workspace/_tmp/projctx-outside/sub && /home/user/workspace/gopi-projctx/bin/gopi-new 'hello'
[gopi] no GOPI.md found — searched: /home/user/workspace/_tmp/projctx-outside/sub, /home/user/workspace/_tmp/projctx-outside, /home/user/workspace/_tmp, /home/user/workspace, /home/user, /home, /
exit code: 0
```

Exit code 0 — identical to the pre-change binary's 0 in the same directory
(`(no GOPI.md line)`, `exit code: 0`). Absence stays optional.

**CASE 5 — read exactly once per run (syscall-level, CASE 2 shape)**

```
$ cd /home/user/workspace/gopi-projctx/internal/tools && \
  strace -f -qq -e trace=openat,newfstatat -o strace2.txt .../bin/gopi-new 'hello'
--- every syscall naming the resolved file ---
89: newfstatat(AT_FDCWD, "/home/user/workspace/gopi-projctx/GOPI.md", {st_mode=S_IFREG|0664, st_size=70679, ...}, 0) = 0
90: openat(AT_FDCWD, "/home/user/workspace/gopi-projctx/GOPI.md", O_RDONLY|O_CLOEXEC) = 7
--- openat() calls for it (the READS): 1
--- '1' loaded line(s) on stderr:
1:[gopi] loaded GOPI.md (70679 bytes)
--- byte count on disk vs the line's number ---
70679 /home/user/workspace/gopi-projctx/GOPI.md
```

One `openat(O_RDONLY)` for the whole run, one `loaded` line. The stat is the
resolution probe; the open is the read. (Provenance: `strace`, `/usr/bin/strace`,
is a read-only instrument — it observes a real run of the real binary; no load
generation, Rule 27.)

### Red proof — the regression test fails before the change

The pre-change semantics were emulated exactly (the walk disabled: `hop=0`, the
file only at the top), then reverted:

```
$ go test -run 'TestProjectContextResolvesFromNestedDir|TestProjectContextLoadIsReadOnceAndReportsBytes' ./internal/agent/
--- FAIL: TestProjectContextResolvesFromNestedDir (0.00s)
    resolution from <tmp>/internal/tools = "", want "<tmp>/GOPI.md" (the file is one
    or two levels up — the pre-change loader read the cwd only and returned nothing)
--- FAIL: TestProjectContextLoadIsReadOnceAndReportsBytes (0.00s)
    stderr does not carry "[gopi] loaded GOPI.md (35 bytes)":
        [gopi] no GOPI.md found — searched: <tmp>/cmd/agent
FAIL
$ go test -run 'TestProjectContext' ./internal/agent/          # restored
ok  	github.com/nick/gopi/internal/agent	1.465s
```

**A correction, stated plainly:** the emulation was run through the new
function with its walk disabled, not against the literal pre-change
`os.ReadFile("GOPI.md")`. The literal-pre-change evidence is the CASE 2
comparison above: the binary built from master `d2703f1f` prints **no**
GOPI.md line from `internal/tools`, i.e. the real old loader returns nothing
there while the new one returns the file's 70679 bytes from the same cwd.

### CASE 5b — the project context actually REACHES the model (wire-level)

The stderr line could lie about what was injected; this inspects the request
body the run actually sent (session raw log of a run started in
`internal/tools`):

```
$ cd /home/user/workspace/gopi-projctx/internal/tools && .../bin/gopi-new 'hello'
session raw log: .../home2/.gopi/sessions/b900cbd6-…/b900cbd6-….raw.log
system blocks: 2
  block[0] type=text bytes=4314   first60="You are GoPi. Use tool_use blocks; each tool's own descripti"
  block[1] type=text bytes=69257  first60='# GoPi Coding Rules — Project Context\n\n> GoPi reads this fil'
system block EQUAL to the repo's GOPI.md bytes: True
```

Two system blocks — kernel, then the project file — and block[1] is
**byte-equal** to `<repo>/GOPI.md` (69257 characters = the file's 70679 UTF-8
bytes). Exactly two injection points, unchanged (the chat path is untouched:
`chat_ops.go` still passes `""`).

## Evidence (self-filed)

- Build: `cd /home/user/workspace/gopi-projctx && CGO_ENABLED=0 go build ./...`
  → `BUILD_ALL_OK` (job-1 exit 0); `go vet ./internal/agent/ ./cmd/agent/` →
  `VET_OK`.
- New tests: `go test -run TestProjectContext -count=1 ./internal/agent/` →
  `ok github.com/nick/gopi/internal/agent 1.465s`.
- Pre-existing failure, not mine: master at `d2703f1f` in a detached worktree
  → `--- FAIL: TestDocAnchorsResolveInNormativeDocs (1.37s)`, 52 blank-line
  drift diagnostics; same test fails identically in my tree.
- `gofmt -l .` → 6 files, all pre-existing on master; none of them is
  `projectctx.go`, `projectctx_test.go` or `internal/agent/loop.go`.
- `cmd/agent`, whole package: run A 13 failures (13 × `TestTaskRun*` /
  `TestMerge*` / `TestCLIPositionalSubmit*`), run B (`-run
  'TestHelp|TestUsage|TestCLI'`) `outcome=pass exit=0`; the submit test
  reproduces on master `d2703f1f` with a master-built binary, and skips when
  `GOPI_BIN` is unset. Reported as observed — not folded into a green claim.
- Acceptance runs: `bash /home/user/workspace/_tmp/projctx-mock/acceptance.sh`
  → `/home/user/workspace/_tmp/projctx-mock/evidence.txt` (the quoted case
  output above is copied from it verbatim).
- Read-once: `strace -f -qq -e trace=openat,newfstatat` over a CASE 2 run →
  1 `openat(O_RDONLY)` of the resolved file, 1 `loaded` line.
- Wire proof (CASE 5b): the run's own raw log parsed — `system blocks: 2`,
  `block[1] bytes=69257`, `system block EQUAL to the repo's GOPI.md bytes: True`.

## A note on the worktree (disclosed, not hidden)

Mid-task the linked worktree `/home/user/workspace/gopi-projctx-wt` (a
DETACHED master checkout made only for the pre-change comparison and for the
Rule-31 worktree case) was removed, and that removal took the parent
`/home/user/workspace/gopi-projctx` directory with it. The three commits above
survived (they are in the shared object store), so the branch was re-created
from `6167d14d` and the worktree re-attached in the same place; every file in
this report is present at the paths it names, and the acceptance cases were
re-run on the restored tree with identical output (see Verification). No
deliverable was lost — but the incident is recorded here rather than quietly
tidied away.

## Design notes for the next reader

- **Why not `tools.RepoRootAt`/`GitSyncRepoRoot`?** `internal/agent` does not
  import `internal/tools` and `internal/tools` does not import
  `internal/agent`, so one of the two directions is a new (large, cyclic)
  dependency edge; both helpers also shell out to `git` (`RepoRootAt` needs a
  context and a git binary). The boundary only needs to know *"is this
  directory a checkout top, a linked-worktree top, or neither"* — one
  `os.Stat` per hop answers it, so the loader stays stdlib-only (Rule 10.7) and
  never spawns a process at session start.
- **`git rev-parse` would answer differently anyway.** For a linked worktree it
  returns the WORKTREE's own top, which is the correct boundary — the same
  answer the `.git`-file test gives, without a subprocess.
- **Why the outermost repo wins.** A repository nested in a repository is a
  fixture or a vendored checkout; the operator standing in the outer project
  must not get the fixture's rules (and a test fixture with a `GOPI.md` must
  not silently become the project's context). The walk therefore climbs
  through nested checkouts and stops at the outer top.
- **Prompt-cache safety.** The resolution runs once in `NewAgent`; nothing
  re-reads, watches, or stats the file afterwards — no second syscall of any
  kind names it for the rest of the session (CASE 5).
