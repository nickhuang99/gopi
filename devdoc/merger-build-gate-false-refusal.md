<!-- doc-class: historical -->
# Task Report: the merger's BUILD gate refused a GOOD branch — root cause, controls, and the re-pointed deliverable

**Epic**: the merger refused `fix/task-list-address-no-scan` at merge time for a FAILED BUILD
(`go: downloading modernc.org/sqlite v1.44.0` / `github.com/google/uuid v1.6.0`) and archived it.
The deliverable asked for: a new branch off master carrying that branch's **intent**, so the merger
lands it next tick, without reviving the archived predecessor.

**Outcome (read first)**: the intent is **already on master** — the merger itself landed it at
`e8afcdfb` (2026-09-15 07:33:19) while this task ran. The epic's landing goal is therefore met, and
the deliverable was re-pointed (verdict B) to what was actually missing: proof of **why** a branch
whose content is sound was refused, plus the pointer to the fix that is still unlanded.

## Completed

1. **Reproduced the refusal's exact text on a cold per-process `GOMODCACHE`** — the gate's own
   environment shape (`mergeBuildGateEnv` replaces the caller's `GOCACHE`/`GOMODCACHE` with a
   per-PROCESS pair inside the gate's scratch root):

   ```
   go: downloading github.com/google/uuid v1.6.0
   go: downloading modernc.org/sqlite v1.44.0
   internal/tools/tasks.go:32:2: github.com/google/uuid@v1.6.0: Get "...v1.6.0.zip": dial tcp [...]:443: i/o timeout
   internal/agencyscan/store.go:14:2: modernc.org/sqlite@v1.44.0: Get "...v1.44.0.zip": dial tcp [...]:443: i/o timeout
   exit 1
   ```

   Same two modules, same failure shape as the refusal. With the operator's warm cache
   (`GOMODCACHE=/home/user/go/pkg/mod`) the same packages build in ~1s.
2. **Control — the failure is PURE MASTER's, not the branch's.** The identical cold-cache command,
   run in a worktree holding **pure master content** (`gopi-cli-help-dedup` @ `6bc0dfeb`, none of the
   predecessor's files), fails **identically** (exit 1, same two modules, same two packages). A
   failure that reproduces with zero branch content cannot implicate the branch.
3. **Control — acceptance green on the branch and on master.** Verbatim
   `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **exit 0** on this branch's tip and on a
   pristine-master checkout (warm cache). The branch's content compiles clean.
4. **The gate's caches are cold in practice, measured**: the gate's scratch class holds 20+
   `queue-runtime-merge-build-*/gomodcache` directories; **19 are 0 bytes**, one is 410 MB — i.e.
   nearly every merger tick weighs a branch with a cache that must fetch the world first.
5. **Why the blast radius is exactly this branch** (gate logic, `internal/tools/merge_build_gate.go`):
   the gate refuses only when the **branch build FAILS** *and* the **target build PASSES**
   (`task_merge.go:280` → archive). Branch and target share the same per-process cold caches, so a
   *target* that cannot fetch ABSTAINS ("not branch-introduced, merging unchanged" — the log shows
   that line repeatedly). The predecessor was therefore refused on the night master's cold build
   happened to succeed: the gate measured a **module-fetch timeout on the branch side**, and
   `go`'s non-zero exit is classified as a compile failure. Root cause = cold per-process module
   cache + an unreachable/erratic proxy; the branch's content was never involved.
6. **Intent parity, file-by-file** — the predecessor's diff is master's tree now:
   `internal/tools/tasks.go` (174/20 numstat), `internal/tools/taskledger.go` (169/0 — moved to
   `cmd/agent/cli_tasks.go` for part of it), the intent test files
   (`cli_tasks_list_ledger_hint_test.go`, `cli_tasks_list_session_scan_test.go`,
   `taskledger_cost_test.go`, `tasks_session_resolve_cost_test.go`, `tasks_session_scan_seam_test.go`)
   and the 2 devdocs are all present at `e8afcdfb`; the predecessor branch's only commit
   (`a0dc51a2`) is the merge of that branch into the rework branch, and
   `git diff master fix/task-list-address-no-scan-rework` is empty for the intent paths.
   `taskstate.go`'s 4-line delta was already master's before the branch.
7. **The remaining defect is owned elsewhere (Rule 26), not duplicated here**: ledger row
   `01a0a248` ("merger BUILD gate: resolve modules from a warm/shared cache …", **pending**) plus the
   written-but-unmerged half on `fix/mergebuildgate-module-download-abstain` (`2ee43d5c`, `45f56317`:
   an unresolvable module download must ABSTAIN, never refuse). This branch adds no code that would
   collide with either.

## Not Done (honest)

* **No code change on this branch** — devdoc only. Master's tree *is* the predecessor's intent, so a
  rework branch carrying the intent again would be an empty merge; editing the gate here would
  collide with row `01a0a248` and with `fix/mergebuildgate-module-download-abstain`.
* **The merger's own archive record of the predecessor is not observable on this host**: no
  `refs/heads/fix/task-list-address-no-scan` and no `archive/fix/task-list-address-no-scan` tag exist
  here (the tag sweep, if any, ran on a peer host). Recorded as unverified, not claimed.
* **Sibling overlap disclosed, not coordinated**: row `01a0a222` ("resolve merge conflict: rework:
  fix/task-list-address-no-scan", in_progress, via worker) owns the worktree
  `/home/user/workspace/gopi-tasklistaddr` on branch `fix/task-list-address-no-scan-rework`. This task
  only **read** that branch; it never wrote there.
* **The gate's timeout→ABSTAIN arm was not exercised** against a real over-budget build: the refusal
  text is `go`'s own module-download error, i.e. a non-zero exit *inside* the 600 s budget.

## Flake note (why a `go test` red here is not a branch finding)

Full-suite runs on this host are load-sensitive. Same tick, two directions: on
`fix/mergebuildgate-module-download-abstain` one `internal/tools` test failed while **pure master**
failed 7 `cmd/agent` e2e tests; on this branch's worktree the **full `internal/tools` suite passed**
(`ok … 173.980s`). Redness therefore has to be attributed with a same-tick control, never assumed.

## Verification

| check | result |
|---|---|
| Acceptance, verbatim, on this branch's tip: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | **exit 0** (binary 22 177 516 bytes) |
| Reproduction, cold per-process `GOMODCACHE`, `./internal/tools/` | **exit 1**, `go: downloading …uuid v1.6.0 / …sqlite v1.44.0`, proxy `i/o timeout` (154 s) |
| Control, identical cold-cache command on **pure master** (`6bc0dfeb`, no branch files) | **exit 1**, identical failure (same modules, same packages) |
| Control, identical command on pristine master, warm cache | **exit 0** |
| 14 targeted intent tests (`internal/tools` 12, `cmd/agent` 2) | **14/14 PASS** |
| Full `internal/tools` suite on this branch's worktree | `ok github.com/nick/gopi/internal/tools 173.980s` |
| `git diff master <this branch>` | devdoc only (1 file) |

## Files

* `devdoc/merger-build-gate-false-refusal.md` — this report (the branch's only delta).

## Landing note

Documentation-only, so the merge is trivially clean (no code, no test, no binary delta). It exists so
that the refusal's cause, the control that clears the branch content, and the "the intent already
landed" fact live in the repo instead of one session's transcript.
