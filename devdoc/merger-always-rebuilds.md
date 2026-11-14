<!-- doc-class: historical -->
# Task Report: merger always rebuilds the binary (pass-level rebuild invariant)

Branch: `feat/merger-always-rebuilds` — worktree `/home/user/workspace/gopi-merger-always-rebuild`
Base: master `acc62180`

## Completed

- **The gap, measured.** The post-merge rebuild exists and is correct (`cmd/agent/cli_tasks_rebuild.go:serveRebuildAfterMerge` → `internal/tools/rebuild.go:RebuildAfterMerge`, Rule 12), and it demonstrably fires: `~/.gopi/logs/merger.log` holds **55 `[rebuild] ok`** lines. But it is wired **per landing call site**, and only `mergerSweepRepo` reaches it (`cli_tasks.go:2411`, `cli_tasks.go:2433`). Two other master-advancing paths in the same merger pass never rebuild:
  - `serveAdoptionSweep` (`cmd/agent/cli_tasks_adopt.go:95`) adopts peer branches into master (`AdoptionSweep` ③a commits), mints a version name for the position (`mintAdoptionVersionTag`, line 139) and **never rebuilds**;
  - the merge-execute face (`gopi task merge-execute` → `tools.TaskMergeBacklogExecute`) merges a branch into master with **neither a rebuild nor a mint**.
  So "master advanced ⇒ bin follows" holds only for the landings the sweep remembers.
- **The fix: a PASS-LEVEL INVARIANT, not another call site.** `mergerPassOnce` now ends with `mergerPassCloseLoop(repos)` (`cmd/agent/cli_tasks_rebuild.go`), which brings every configured repo's binary up to its master tip after all pass steps have run. A future landing path cannot forget it — the rebuild no longer depends on an inventory of call sites.
- **Ordering follows the operator's requirement** ("rebuilding done after merger receives child gopi success/failure return"): the invariant runs *after* the per-repo sweep loop, and that loop is exactly where the merger **blocks on its spawned children** (the resolve child and the merge-verify child are started and waited on inside `mergerSweepRepo` → `runMergeVerifyChild` / `mergerResolveConflict`). Every child has returned before a byte of the rebuild is written.
- **Idempotent, so it is safe to run every pass:** `RebuildAfterMerge` probes the artifact first and reports "already current" when it already follows the tip, so a pass that landed nothing costs one `--version` probe — never a build. A repo with no *declared* build (docs-only repo, or a pure merge target with an empty `build`) stays silent — no per-pass log noise.
- **Layering with the existing per-landing call is deliberate and documented:** that one runs earlier with the mechanism-2 revert handle (a failed landing build unwinds the merge the sweep just made). The invariant passes **no handle** — it landed nothing of its own, so a failure is logged + `build-failed` notified (inside `RebuildAfterMerge`), never a rollback of a merge it did not initiate.
- **Tests** (`cmd/agent/cli_tasks_rebuild_pass_test.go`, 3 green):
  - mechanism — a stale artifact is rebuilt, and a **second** pass takes the idempotent shortcut (`already current`), proving no rebuild storm;
  - scope — no declared build contract ⇒ **zero output** (silence is the contract);
  - **wiring + ordering red-proof** (source-level, brace-matched on `mergerPassOnce`): fails if the invariant call is deleted, or moved above the sweep loop.

## Not Done

- **Not merged** — merge is the PA's step after acceptance (Rule 6e).
- **The live-resident half of the operator's report is NOT a code defect and is not fixed here.** A merger daemon that started before this change keeps running the old image (`readlink /proc/<pid>/exe` → `bin/gopi (deleted)`) and cannot adopt the invariant without a restart onto the rebuilt binary. Deploying it: rebuild `bin/gopi` at master, then `gopi merger --restart --wait 5m` (never a blind kill). Memory #2168/#2170/#2510 cover this bootstrap hand-over.
- **No live `--once` run against the operator's repo.** The main checkout has 2 pending merges (`fix/dirty-holder-salvage-dispatch`, `fix/worktree-residue-closeout`); running the merger there would land someone else's branches as a side effect of a test. The end-to-end proof is the sandbox run below instead.

## Verification

- **Build:** `go build ./...` + `go vet ./cmd/agent/` → clean (worktree).
- **Tests:** `go test ./cmd/agent/ -run TestMergerPassCloseLoop -count=1 -v` → 3/3 PASS.
- **Mutation red-proof** (`devdoc/merger-always-rebuilds-mutation-redproof.sh`, run against copies, originals restored — `git status --porcelain` clean afterwards):
  | mutation | result |
  |---|---|
  | delete `mergerPassCloseLoop(repos)` | test RED (exit 1) ✅ killed |
  | move the call ABOVE the sweep loop | test RED (exit 1) ✅ killed |
  | drop `serveRebuildAfterMerge` from the invariant body | test RED (exit 1) ✅ killed |
  A vacuous test would have stayed green on all three.
- **Regression scope:** `go test ./cmd/agent/ ./internal/tools/ ./internal/config/ -count=1` — see the job output attached to this task's session.
- **Commit:** `db1fcef6 fix(merger): rebuild every configured repo at the pass close loop, not only at the landing that remembers`

## Evidence for the diagnosis (why "my binary was NOT rebuilt")

| measurement | value |
|---|---|
| `[rebuild] ok` lines in merger.log | 55 |
| `[rebuild] skip:` lines | 44 (42 × `worktree dirty`, the known untracked-dirt guard of epic 2026-09-12) |
| rounds with `merged=[1-9]` | 50 |
| rebuild call sites in production code | 2, both inside `mergerSweepRepo` |
| master-advancing paths WITHOUT a rebuild | `serveAdoptionSweep` (tag mint only), `task merge-execute` (nothing) |

The log's `merged=N` lines are followed by the mint line and then the rebuild line, so the *per-landing* hook is not dead — the defect is coverage, not wiring.
