<!-- doc-class: historical -->
# Task Report: merger status — `gopi merger --status` (verify + close)

Epic row: "need to know gopi merger status" — *the merger runs in the
background as a service, but how does the user learn its status? add a CLI
flag like `gopi merger --status`?*

## Completed

### 1. The requested feature already exists on master (verified, not assumed)

`gopi merger --status` is **already implemented and merged**. Investigation
(`code_search` + `git log` on the files, memory #2023/#1988):

| Piece | Where |
|---|---|
| Read-only status engine | `internal/tools/merger_status.go` (`MergerStatusReport`, `MergerStatus.Render`) |
| CLI renderer / dispatch | `cmd/agent/cli_tasks.go` (`runTaskMergerStatus`, `opts.statusOnly`) |
| Top-level face | `cmd/agent/main.go` (`isMergerSubcommand` → `if opts.statusOnly { runTaskMergerStatus() }`) |
| Liveness probe | `/proc/locks` inode join (never `acquireServeLock`) |
| Tests | `internal/tools/merger_status_test.go` (17 tests), `cmd/agent/merger_status_cli_test.go` (2 tests) |
| Commits | `9e2dc7c0` feat, `bb519cf7` unlink-underneath/guard-repair, `52a03ae0` dead-holder names |

**Live proof on the real binary** (built from my worktree base `07499f15`,
an ancestor of master):

```
$ gopi merger --status                     # exit 0
merger:      alive
lock file:   /home/user/.gopi/serve.lock
lock holder: pid 98279

repo:        /home/user/Downloads/games/Maps
  protected: 0 branch(es) held by a live worktree
  pending:   none — nothing to merge

repo:        /home/user/work/GoPi
  protected: 12 branch(es) held by a live worktree
  pending:   3 branch(es) the next sweep would merge
    - feat/tab-order-actuate-poc
    - fix/task-show-run-cmd-verbatim
    - verify/worker-writeback-01a09435
```

**Cross-checked against kernel ground truth** (the pid *stamped* in the file,
98290, is the daemon's argv but **not** the lock holder — the report's 98279
is correct):

```
$ grep -i flock /proc/locks | grep -w 29112398
9: FLOCK  ADVISORY  WRITE 98279 08:04:29112398 0 EOF        # inode of serve.lock
$ ls -l /proc/98279/fd | grep serve.lock
lrwx------ 3 -> /home/user/.gopi/serve.lock
```

**Read-only contract proven on the filesystem** (the epic's core risk — a
status command that takes the lock would leave a trace making the next check
look like a daemon):

- lock file mtime stayed `Sep 13 00:01` across the status call;
- in a private `GOPI_HOME` the command printed `merger: not running` and
  **created no `serve.lock` at all**;
- a stale stamped pid (`999999`) still rendered `not running` — no crash, no
  false "alive".

Also verified: both faces (`gopi merger --status`, deprecated alias
`gopi task merger --status` — prints the deprecation notice plus the same
report) and `gopi merger -h` (exit 0, starts nothing, and **advertises**
`--status` in its Usage block). Tests green: `go test ./internal/tools -run
Merger` **ok** and `go test ./cmd/agent -run MergerStatus` **ok**.

### 2. Residual defect found and fixed (the actual delta of this task)

Three doc comments advertised a **phantom verb `gopi status`** which does not
exist and is not dispatched — the typed word falls through to the chat
session path. An operator grepping the source for "how do I ask the merger
what it is doing" was sent to a command that starts an LLM session instead
(Rule 15 / Rule 1b: a documented contract must name the real verb).

- `internal/tools/merger_status.go` — header + side-effect-freedom note now
  name `gopi merger --status` (and its alias).
- `internal/tools/repo_sweep.go` — `RepoSweepPreview` doc now names
  `gopi merger --status`.
- `internal/tools/merger_status_doc_verb_test.go` — **new regression guard**:
  a literal gate (Rule 13 pattern, `TestUnknownNode_MirrorLiteralGate`
  precedent) scanning every production `.go` in `internal/tools`, failing any
  that re-inlines `gopi status`. Red-proof: a planted literal fails the gate
  with the pointed message; removing it is green.

## Not Done

- No re-implementation of the status surface — it already existed; rebuilding
  it would have been a duplicate (and the memory recipe #2023 already had it).
- `gopi status` was **not** promoted to a real top-level verb: the shipped
  contract is `gopi merger --status`, and adding a second spelling is a
  larger, separate decision (see memory #1974 on verb-tier archaeology).
- The two **pre-existing** red gates on master were left alone (see
  Verification): `internal/stylegate` (sleeps in tests) and
  `internal/standards` Rule 17 (2 new env-isolation hits at
  `cmd/agent/cli_voice_review_test.go:1301`,
  `cmd/agent/worktree_release_accept_test.go:225`). Master's own tree is red
  for the identical sites — baseline drift on a moving master, not this
  change's regression (Rule: diff a failing gate against master by RUNNING
  master's copy). The Rule 17 baseline in `internal/standards/baseline.go`
  pins `file:LINE` outside `internal/tools`, so these edits cannot shift a
  permit.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK (and `go build -o bin/gopi ./cmd/agent/` OK).
- Tests: `go test ./internal/tools -run 'Merger|Phantom' -count=1` — **ok 14.187s**;
  `go test ./cmd/agent -run 'MergerStatus' -count=1` — **ok**;
  literal-gate red-proof: planted `gopi status` → FAIL with the pointed
  message, removed → **ok**.
- Real-binary evidence: alive/no-daemon/stale-pid/alias/help transcripts
  above; lock file unmodified (`Sep 13 00:01`) and not created in a clean
  `GOPI_HOME`.
- Pre-existing gates (master, unchanged): `internal/stylegate` FAIL,
  `internal/standards` Rule 17 FAIL 2 NEW — both reproduce on master's tree.

## Commit

Branch `fix/merger-status-doc-verb` in worktree
`/home/user/workspace/gopi-merger-status` (base `07499f15`, an ancestor of
master).
