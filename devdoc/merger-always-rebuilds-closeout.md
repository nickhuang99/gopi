<!-- doc-class: historical -->
# Task Report: the merger's rebuild invariant — coverage + ordering (closeout)

Branch: `fix/merger-pass-rebuild-coverage` (worktree `/home/user/workspace/gopi-pass-rebuild-closeout`)
Base: master `acc62180`. Carries and AMENDS the sibling deliverable
`feat/merger-always-rebuilds` @ `db1fcef6` (cherry-picked as `56a8983e`).

## The operator's report, and what was actually wrong

> "gopi merger should always rebuild binary whenever new branch is merged …
> I suspect this is only done at worktree before merging successful … checking
> binary gopi binary time can prove this … make sure this rebuilding is done
> after merger receives child gopi process success/failure return."

Measured on master `acc62180` (evidence, not inference):

| measurement | value |
|---|---|
| `[rebuild] ok` lines in `~/.gopi/logs/merger.log` | **55** (yes, the hook fires) |
| rebuild call sites in production code | **2**, both inside `mergerSweepRepo` |
| `mergerPassOnce` exits that skip the rebuild | **2** (unreadable settings, empty repos table) |
| master-advancing paths with NO rebuild before this task | `serveAdoptionSweep` (mints a name, never builds), `task merge-execute` (nothing at all) |
| merger daemon alive at `~/.gopi/serve.lock` | **no** — pid 802086 dead; last log line 2026-09-15 20:25; no `gopi merger` process running anywhere on this host |
| `bin/gopi` | **current** — reports `acc62180` = master tip (21:48) |

So the engine (`internal/tools/rebuild.go`, Rule 12 contract) is correct and
the per-landing hook is not dead. The defect is **coverage + one ordering
hole**, which is what this branch closes. (The "my binary was not rebuilt"
observation is also explained by the daemon being DOWN: nothing is rebuilding
*now* — that is an operational fact, not a code defect, and no code change can
substitute for a running merger. `gopi merger --daemon` / the service unit is
the fix for that half.)

## Completed

- **Cherry-picked the sibling fix** (`db1fcef6` → `56a8983e`): the pass-level
  rebuild invariant `mergerPassCloseLoop(repos)`. It is a good idea and the
  right unit — a rebuild that exists at N call sites disappears whenever one is
  added without it.
- **Amended it: the invariant is now a DEFER armed as the pass's first
  statement** (`defer func() { mergerPassCloseLoop(repos) }()`), not a statement
  after the sweep loop. Measured defect in the sibling shape: `mergerPassOnce`
  has two `return`s *below* the adoption sweep (unreadable settings; empty
  `repos` table) — on those exits the pass had already had its chance to adopt
  branches into master and then skipped the rebuild entirely. A rebuild a
  `return` can step over is not an invariant.
  - The **closure is load-bearing**: `defer mergerPassCloseLoop(repos)` would
    evaluate `repos` at the defer statement — i.e. *before* the
    `ResolveMergerRepos()` below it — and rebuild nothing, forever, silently.
    `repos` is therefore declared above the defer and assigned by the resolve.
- **Closed the last landing face: `gopi task merge-execute`.** It merges a
  branch into master (→ `tools.TaskMergeBacklogExecute`, which commits the
  staged merge) and had *neither* a rebuild nor a version name. It now reaches
  the same close loop through `mergerCloseLoopAfterLanding(rep.Repo)`, gated on
  the new exported predicate `tools.MergeExecLanded(rep)` (the run actually
  advanced master — a conflict/abort or a guard skip owes nothing), and only
  after the engine has returned (the ordering the operator asked for).
- **One builder for all faces** (`mergerCloseLoopAfterLanding`): the union of
  the *configured* repos (the same settings table the merger sweeps) with the
  repo the face actually landed in, de-duplicated (`mergerUniqueRepos`) so one
  physical checkout is never built twice by one close loop.
- **Fixed the sibling's red-proof script, which could not fail.** It `cd`'d to
  its own directory (`devdoc/`) and then used repo-root-relative paths, so
  `cp` failed and *every* mutation "run" exited before touching a file — a
  green-looking proof of nothing. The rewritten script checks the root, and
  (lesson ②, learned the hard way here) keeps its backups **beside the repo**
  instead of under `TMPDIR`, where this host's scratch sweeper deleted them
  mid-run — which made `restore()` fail silently while the script still printed
  "all mutations killed", leaving the tree mutated. Every restore is now
  checked, and the script diffs the result against the backup before exiting 0.
- **Extended the tests** (`cli_tasks_rebuild_pass_test.go`): the wiring test now
  asserts the *defer* form and scans for any `return` above the arm (offset
  based, so an identical line elsewhere cannot mask it); new pins for the
  merge-execute face's wiring and for `MergeExecLanded`'s semantics. The new
  early-return scan immediately caught a real ordering bug in my own first
  attempt (the arm sat *below* the `ResolveMergerRepos` error return).
- **Live end-to-end run** (sandbox repo, sandbox settings, real `bin/gopi`
  built from this branch — full transcript in `## Verification`): a stale
  artifact is rebuilt on the first `merger --once`, the second pass takes the
  idempotent shortcut, and the empty-repos-table early-return path exits clean.

## Not Done (honest)

- **Not merged** — merge is the PA's step after acceptance (Rule 6e).
- **No runtime assertion for the defer's early-return benefit itself.** With an
  empty repos table the invariant has no repo to act on, so the defer is
  observable only as "no crash, no rebuild"; the guard for that path is
  source-level + mutation-killed (mutations 1–3). The *mechanism* and
  *idempotence* have runtime proof; the *wiring* has mutation proof.
- **No live run against the operator's real repo.** The main checkout has two
  pending merges (`fix/dirty-holder-salvage-dispatch`,
  `fix/worktree-residue-closeout`); running the merger there would land someone
  else's branches as a side effect of a test. Sandbox proof instead.
- **`internal/tools` has a pre-existing failure on this host**
  (`TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` — proxy-off module
  resolution), reproduced red at the pristine main checkout
  (`~/work/GoPi`, master `acc62180`), so it is not branch-introduced and not
  fixed here.
- **The daemon-down half is operational**, deliberately left to the operator:
  starting a merger is a machine-level act (`gopi merger --daemon`), and the
  rebuild only exists while the daemon runs. Not silently done on the
  operator's behalf.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → BUILD_OK; `go vet ./cmd/agent
  ./internal/tools ./internal/config` → VET_OK; `gofmt -l` on the four touched
  files → empty (the three files `gofmt -l` reports in `cmd/agent` are
  pre-existing and untouched — verified identical on the pristine worktree).
- **Tests:** `go test ./cmd/agent/ -run 'TestMergerPassCloseLoop|TestMergeExec'`
  → 6/6 PASS (mechanism, idempotence, silent-without-contract, wiring+ordering,
  merge-execute wiring, `MergeExecLanded` semantics). Full package results in
  the job output attached to this task's session.
- **Mutation red-proof** (`devdoc/merger-always-rebuilds-mutation-redproof.sh`,
  run from the repo root with `bash`, restore verified byte-identical):
  | mutation | expected | observed |
  |---|---|---|
  | delete the armed invariant | test RED | exit 1 ✅ killed |
  | demote the defer to a tail statement (the sibling's shape) | test RED | exit 1 ✅ killed |
  | inject an early `return` above the arm | test RED | exit 1 ✅ killed |
  | drop `serveRebuildAfterMerge` from the invariant body | test RED | exit 1 ✅ killed |
  | unwire the merge-execute close loop | test RED | exit 1 ✅ killed |

  The script itself is gated by the repo's own suite, and the first version
  failed two of those gates (caught by `go test ./internal/testutil`, not by
  review): `TestScriptsAreBoundedAndOffTmp` — it used `mktemp` without sourcing
  `scripts/lib/tmp-ban.sh` (Rule 22: a script that names an unmeasured temp path
  has chosen a partition it cannot verify), and
  `TestTestBudgetHasASingleSourceAndNamesATimeout` — its five `go test` calls
  named no `-timeout`, so each inherited Go's 10m per-package cap and a loaded
  host would report a 600s FAIL that says nothing about the mutation. Both are
  fixed here: backups come from `gopi_scratch_mkdir` (off `/`, registered for
  trap-time cleanup via `gopi_scratch_cleanup`) and every invocation passes the
  named `MUTATION_TEST_TIMEOUT` (Rule 1a). It also had to move from `sh` to
  `bash` — it sources a bash helper. That is the same class of defect as the
  sibling's dead red-proof (a guard script is unverified code): the gate caught
  it because a mutation script is a committed script, and running it is not
  evidence that it satisfies the repo's contracts.
- **Live E2E** (sandbox `/dev/shm/…`, real binary built from this branch):
  ```
  TIP=10c025d
  RUN 1  [merger] [rebuild] ok: …/repoA built in 2.455538ms (10c025d)   → bin now reports 10c025d ✅
  RUN 2  [merger] [rebuild] ok: …/repoA already current at 10c025d (no rebuild needed)   ✅ idempotent
  RUN 3  [merger] [repos] no configured repos …  → --once: single pass done, exit 0   ✅ early-return path clean
  ```
  Malformed settings (the other early return) also exits clean with the
  resolve error reported, not swallowed.
- **Commit:** `56a8983e` (sibling fix) + this branch's closeout commit — see the
  final message for the hash, branch name and recovery command.

## Deliverable declaration (Rule 6d)

- Branch: **`fix/merger-pass-rebuild-coverage`**, worktree
  `/home/user/workspace/gopi-pass-rebuild-closeout`; the sibling's own branch
  `feat/merger-always-rebuilds` is the parent (already reviewed by PA).
- The commit is in the MAIN repo's object store (worktree-shared — no push
  needed, Rule 6c).
- Recovery: `git -C /home/user/work/GoPi show fix/merger-pass-rebuild-coverage:devdoc/merger-always-rebuilds-closeout.md`
