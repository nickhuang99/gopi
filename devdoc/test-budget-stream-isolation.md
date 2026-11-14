<!-- doc-class: historical -->
# Task Report: test-budget.sh — a per-run stream, `last.json` as a pointer, and the scope anomaly

**Branch**: `fix/test-budget-stream-isolation` · **Commit**: `92fe4386`
**Manifest row**: `devdoc/DOC-INDEX.tsv` — class `historical` (this doc's title
declares a past run's record; no current artifact cites it as authority), with
the file's own `<!-- doc-class: historical -->` banner. The manifest row is
**regenerated** by `tools/docclass/classify.py`, not hand-written.

## Rework note (this revision)

This work originally landed on a branch based at master `90e664e0`; master then
moved to `92d14684`, and the branch's `devdoc/DOC-INDEX.tsv` row — generated
against the stale base — was the ONE merge conflict that blocked the merge
(bookkeeping only; the six substantive files still applied cleanly). The rework
is on a FRESH branch from `92d14684` carrying those same six files, with the
manifest row recomputed against the CURRENT index:

- the branch no longer rewrites the 6 pre-existing index rows whose anchors had
  drifted with master (`attachment-index-contract`, `bug-verify-closure-20260912`,
  `claim-time-ground-stamp`, `memsync-bidir-m49`, `memsync-import-atomic`,
  `merge-backlog-terminal-guard`) — that drift is master's, not this work's owed
  output, and driving `classify.py` to regenerate the whole 2090-row manifest
  would re-anchor ~130 rows of code comments unrelated to the stream;
- the diff against master is exactly ONE added line in the manifest, so the
  merge conflict is gone by construction rather than by resolution.

## The defect

`scripts/test-budget.sh` redirected its raw `go test -json` stream into ONE
host-wide path — `$(gopi_scratch_queue)/test-budget/last.json` — and rendered
the summary, the `--- FAIL:` fan-out, the timeout markers and the heavy-drift
report from that same file. Every budget run on the host therefore fought over
one inode, so a run's numbers could be drawn from a neighbour's events.
Measured 2026-09-19: one `last.json` held 2422 `cmd/agent` events followed by
1328 `internal/tools` events from a concurrent run.

The earlier fix in the same script kept a per-run COPY (`cp` after the run) —
that made a quoted verdict citable, but the redirect itself still went to the
shared name for the whole run, which is the part that mixed the streams.

## The fix, three parts

1. **Per-run stream, from the first byte.** The redirect target IS this run's
   file: `run_tag="$raw_json_prefix${start_epoch}-$$"` →
   `raw_json="$budget_dir/$run_tag$raw_json_suffix"` →
   `go test … -json "${args[@]}" >"$raw_json"`. There is nothing to copy
   afterwards any more. The name carries **epoch AND pid**: the epoch alone is
   second-granular and two runs started in the same second would collide — not
   hypothetical, both acceptance runs in this task started at `21:35:14`.

2. **The canonical "latest" is a POST-HOC POINTER.** `publish_latest_stream`
   creates `last.json.<pid>.tmp` with `ln -s "$(basename "$raw_json")"` and
   `mv -f`s it over `last.json`, i.e. an atomic relative symlink to a COMPLETED
   run's stream. Nothing is ever opened for writing at that name, so no `>`
   can truncate a neighbour. Failures are echoed, never swallowed (Rule 15b).
   Consumers need no change: `[ -f ]` and `readlink` follow symlinks, so
   `scripts/heavy-drift-report.sh` (default `last.json`),
   `scripts/heavy-derive.sh` (fallback) and `internal/smoke`'s stamp reader
   keep working. A new `--latest` verb prints the RESOLVED path for humans.

   **The pointer carries a seal, and the seal is not optional.** Publishing a
   NAME that resolves to a finished run's stream re-creates an alias to the
   run's evidence, so anything that writes *through* the name destroys
   completed work. Two such writers exist, and one of them is REAL and was
   observed destroying evidence in this very task: while the two acceptance
   runs of 21:41 were in flight, a long-lived budget run started at 21:29:45
   from the **main repo's out-of-date checkout** (old script, `> last.json`)
   was still going; it truncated the `last.json` it had resolved at start —
   which the new script had already re-pointed at the FIRST acceptance run's
   completed stream — and both acceptance runs' stream files ended up holding
   the same 17258-byte `heavytest` stream (same md5), the `smokegate` run's
   own evidence gone after the fact and with no error printed anywhere. So
   `seal_run_stream` (`chmod a-w`) makes the stream read-only BEFORE
   `publish_latest_stream` points at it: a stale writer following the name now
   gets `Permission denied` instead of silently destroying a finished run. The
   gate pins the ORDER (seal before publish) as well as the presence of both
   calls, because the window between them is exactly the hazard.

   That incident also settles the isolation question: **two runs that share a
   scratch root must be running the same script version.** The shipped fix
   cannot defend the scratch tree from an out-of-date writer it does not
   control (Rule 29's "one task, one worktree" is the discipline that keeps
   such a writer from existing); the acceptance evidence below therefore runs
   with `GOPI_TEST_SCRATCH_HOME` pointed at a fresh root, which is also the
   answer the anomaly report itself gives a reader.

3. **A stream outside the requested scope is reported.** After the run the
   script derives the packages the command line actually addressed, reads the
   packages the stream actually carries, and prints
   `STREAM OUT OF SCOPE` naming the ones the scope does not cover, with a
   "do not quote these numbers as this run's evidence" instruction. Properties
   chosen deliberately:
   - the predicate lives in `scripts/lib/stream-scope.sh` (Rule 14: ONE
     definition), so it is callable with synthetic input — the mingled case is
     ASSERTED by the gate instead of waited for;
   - it is reachable as `scripts/test-budget.sh --scope-check <stream.json>
     [pattern...]` (exit 4 = out of scope) so a review can ask the question
     without re-implementing the comparison;
   - packages from ANOTHER module are not reported (they are outside every
     scope by definition; the branch-scoped smoke gate executes this script
     from a fixture module), so the warning cannot cry wolf;
   - if no package pattern can be read from the command line the script SAYS
     the check did not run, rather than reporting a vacuous green (Rule 15).

The module prefix the check compares against is READ from the running module's
`go.mod` (fallback constant `module_import_path_fallback`, reported on stderr),
because the script is executed inside fixture modules.

## Gate (Rule 5 / the epic's regression ask)

`internal/testutil` — extends the existing `TestBudget` gate
(`go test ./internal/testutil/ -run TestBudget`):

| check | what it pins |
|---|---|
| `assertBudgetStreamIsPerRunAndTheLatestIsAPointer` | the ONLY `go test -json` redirect targets `>"$raw_json"`; `run_tag` carries epoch+pid; `latest_link_name` is a constant; the pointer is created with `ln -s`; `budgetLatestWriteRe` refuses any `>` naming `last.json` |
| `assertNoQuotedFixedStreamPath` | the `last.json` literal occurs exactly ONCE, in its constant declaration (Rule 1b/13) |
| `assertBudgetStreamScopeAnomalyIsChecked` | the library path, its `source` directive, the marker, the predicate, and the report CALL on an **executable** line (a commented-out call cannot satisfy it) |
| `assertBudgetScopePrefixMatchesGoMod` | the fallback constant equals `go.mod`'s module path, and the script reads go.mod at run time |
| `TestBudgetStreamScopePredicateNamesTheMingledCase` (dynamic, 6 cases) | in-scope silent · mingled names the neighbour · whole-module covers all · `-run` values are not patterns · another module is not evidence · an unrelated sibling is named |
| `TestBudgetScopeCheckVerbReportsMinglingThroughTheScript` | the shipped verb: exit 0 in scope, exit 4 + marker + package name out of scope |
| `TestBudgetConcurrentRunsKeepTheirOwnStreams` (dynamic, Rule 27) | **two REAL runs of DIFFERENT packages, started together on ONE scratch root**: each `raw json:` line names a DISTINCT per-run stream, each stream's `Package` set is exactly its own package, both streams are per-run names under the budget dir, and `last.json` is a SYMLINK pointing at one of those two runs |

The concurrent row is the epic's "test exercising the concurrent shape". It
stands a stub `go` on `PATH` ahead of the real one and runs the REAL script
twice in parallel: the property under test is the REDIRECT (which file each
run's events land in), not what the packages under test do, so the race stays
deterministic and ~1.5 s — and generating device load to observe it would be
Rule 27's ban (a stub that manufactures the interleave without touching a
device needs none). Two red-proofs were observed on the mutant script:
redirecting `raw_json` back to `"$budget_dir/last.json"` → *"both runs rendered
from ONE stream …"*; publishing the pointer with `cp` instead of `ln -s` →
*"last.json is a ----------, not a symlink"*.

The `go test` budget audit also gained one honest, file-scoped exemption:
`scripts/docclass-red-proofs.sh` (each of its `go test` lines runs ONE named
test in `internal/standards`).

## Verification (all observed in this run)

- `bash -n scripts/test-budget.sh` → exit 0, no output; same for `scripts/lib/stream-scope.sh`.
- `go test -count=1 -run 'TestBudget|TestTestBudget' ./internal/testutil/` → `ok … 1.962s` (final state; 0.863s on re-run).
- `gofmt -l internal/testutil/` → empty; `go vet ./internal/testutil/` → clean.
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Branch-scoped consumer: `go test -count=1 -run TestBranchScopedSmokeRunIsScopedAndSkipsHeavy ./internal/smoke/` → **PASS** (scoped run: one package, exit 0, `stream=run-1789825128-2254586.json`; whole-tree control unchanged).
- `scripts/heavy-drift-report.sh` against the pointer (resolved) and via the default `last.json` path → both print the report.
- **Concurrency (Rule 27: no synthetic load — two real runs of DIFFERENT packages, one shared scratch home, both at 21:35:14):**
  - `run-1789824914-2089563.json` = `internal/heavytest` only — 81 events, sha256 `a4a05b60c2d126c897bfba7e7a457554957165bbf0038f847f741d72f9d46f0b`;
  - `run-1789824914-2089565.json` = `internal/tasktags` only — 12 events, sha256 `f883441dde977f6d3748cdf4bb2896f60af9453fcc4d04224537dda21b5a1b7e`;
  - `stamps.log` names both, `last.json -> run-1789824914-2089563.json`; each printed summary's slowest block names only its own package.
- **Defect red-proof**: a scratch copy of this worktree (`/home/user/workspace/gopi-tb-red`, driver `/home/user/workspace/gopi-tb-redproof.sh`) with ONE line reverted to the pre-fix fixed name — both runs' `raw json:` line named the SAME file, and that file held 522 `internal/standards` events while the heavytest run rendered its summary from it (the heavytest run's own events were lost). The pre-fix shape is what the epic reports, reproduced on demand.
- **Gate red-proofs** (each mutation reverted after observing the failure): redirect to `"$budget_dir/last.json"` → both "redirects … somewhere other than the per-run $raw_json" and "writes to the canonical latest name"; comment out `report_stream_scope_anomaly "${args[@]}"` → "no longer CALLS … on an executable line"; comment out the `stream_scope_lib` source directive → "no longer contains …".

## Re-verification (independent pass, 21:45-21:58, same branch)

An independent reviewer re-ran the acceptance on a FRESH scratch root
(`GOPI_TEST_SCRATCH_HOME=/home/user/workspace/gopi-verify-scratch3`, script
`…/conc-proof.sh`) — two REAL concurrent runs of DIFFERENT packages, both
launched in the same second:

- `run-1789825559-2614110.json` → package set `{github.com/nick/gopi/internal/smokegate}`, exit 0, 2145 bytes, mode `-r--r--r--`;
- `run-1789825559-2614111.json` → package set `{github.com/nick/gopi/internal/heavytest}`, exit 0, 17261 bytes, mode `-r--r--r--`;
- A's slowest block names `smokegate` 2/2 rows, B's names `heavytest` 15/15 — no cross-contamination in either printed summary;
- `last.json -> run-1789825559-2614111.json`; `echo x >> last.json` → `Permission denied` (the seal holds against a writer following the pointer);
- `--scope-check` on B's stream with `./internal/smokegate/` → exit **4** + `STREAM OUT OF SCOPE` + `out of scope: …/internal/heavytest`; the same stream with `./...` → exit 0;
- `bash -n scripts/test-budget.sh` → exit 0; `go test ./internal/testutil/ -run TestBudget -count=1` → `ok … 0.893s` with all four `TestBudget*` tests PASS; `gofmt -l internal/testutil/` empty; `CGO_ENABLED=0 go build ./cmd/agent/` OK.
- Base re-check: `internal/smoke`'s `TestStagedScriptTree*` FAIL at master `90e664e0` in a fresh detached worktree → pre-existing, not this branch.

## Not Done (honest)

- **The dynamic gate stands in a stub `go`; it does not run two real package
  suites.** The epic's test ask — "case/test exercising the concurrent shape if
  the existing gate can host one" — is met by
  `TestBudgetConcurrentRunsKeepTheirOwnStreams` (two REAL script processes,
  racing on ONE scratch root), but the stub answers `go test`, so the unit test
  stays a ~1.5 s smoke. The two-real-package proof remains a measured
  acceptance run (below), not a unit test: compiling and running two suites
  inside one package test is minutes of real device work, which is exactly the
  cost Rule 27 classifies rather than tolerates.
- **`internal/smoke` `TestStagedScriptTree*` failures are NOT this branch.**
  They reproduce with this branch's changes stashed (tree at `90e664e0`): the
  fixture cannot satisfy `scripts/build-links-static.sh`'s
  `sources scripts/${VENDOR_DIR_NAME}/${arch}` directive (that line was also
  listed in this branch's run, as any other source directive is). The behaviour
  this change feeds into that package passes.
- **`internal/standards` `TestDocManifestCoversEveryDevdocFile` is red on the
  untouched base** — 5 devdoc files have no `devdoc/DOC-INDEX.tsv` row
  (`pending-queue-obsolescence-review-20260919.md`,
  `proc-identity-single-predicate.md`, `rule13-gate-self-red.md`,
  `task-update-status-message.md`, `web-page-token-fetch-fix.md`). Pre-existing,
  unrelated to the stream; left alone rather than silently classified.
  **Re-measured on this rework's base `92d14684` (2026-09-20)**: the same gate is
  red there for **6** tracked files with no row — `cli-ui-feasibility-study.md`,
  `report-seam-hermetic.md`, `requirements/12-verification-and-gates.md`,
  `task-notify-study-20260920.md`, `taskid-vs-sessionid-verdict.md`,
  `tools-master-reds-90e664e0.md` — and `TestDocClassBannerMatchesManifest` is red
  for `gopi-statistics-20260920.md` (classed `historical`, no banner). Both are
  proven pre-existing from **git refs**, so the branch's own edits cannot have
  produced them (observation in `Evidence` below). This rework does NOT classify
  them: silently adding manifest rows for files another task owns would edit the
  manifest to hide a gate the BASE is failing.

## Evidence (self-filed)

- **Pre-existence of the docclass reds — the observation, not the claim**: read
  from git refs on this branch, `for f in cli-ui-feasibility-study.md
  report-seam-hermetic.md requirements/12-verification-and-gates.md
  task-notify-study-20260920.md taskid-vs-sessionid-verdict.md
  tools-master-reds-90e664e0.md; do git cat-file -e master:devdoc/$f && echo
  tracked; done` → **all six print `tracked` on master**;
  `git show master:devdoc/DOC-INDEX.tsv | grep -c '^devdoc/'` → **2089** rows, and
  none of the six has a `^devdoc/<file>\t` row; `git show
  master:devdoc/gopi-statistics-20260920.md | head -1` → `# Task Report: GoPi
  project statistics …` (no `<!-- doc-class: … -->` banner). The same gate then
  fails on THIS branch for exactly those files, so the branch did not introduce them.
- Gate red-proof outputs (verbatim lines observed): `testbudget_gate_test.go:141: scripts/test-budget.sh redirects its go test -json stream somewhere other than the per-run $raw_json: … >"$budget_dir/last.json"`; `testbudget_gate_test.go:145: scripts/test-budget.sh no longer CALLS "report_stream_scope_anomaly \"${args[@]}\"" on an executable line`; `testbudget_gate_test.go:151: scripts/test-budget.sh no longer contains "# . \"$root/$stream_scope_lib\""`. Each file was restored immediately and the gate re-run green.
- Pre-existing smoke failures verified by `git stash push -u` → same two `TestStagedScriptTree*` errors at base → `git stash pop`.
- Worktree litter check: `git status --porcelain` lists only the delivered paths; a stray `internal/testutil/TestSomething` (produced by an early version of the dynamic test) was removed with `git clean -f -- internal/testutil/TestSomething`.
