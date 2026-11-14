<!-- doc-class: historical -->
# Task Report: cmd/agent /tmp probe-binary scratch — verify + close out

Ticket: `bug(test): cmd/agent writes test-built gopi binaries to fixed /tmp paths and
never cleans them` (test-code scratch discipline; found by the test-suite rigor audit,
`devdoc/test-code-discipline-audit.md`).

## TL;DR (what the re-measure changed)

**The three named sites were already fixed on master** — commit `d7b6d3b`
("test(scratch): extend the /tmp gate to Go test sources + stop building probe binaries
into /tmp", 2026-09-11 08:11) landed BOTH halves of the ticket's fix direction: the
probe binaries now build into the suite's pinned scratch root, and the /tmp literal gate
was extended to Go test sources. Re-fixing them would have been a no-op against master.

What this run delivers instead: the **mechanical acceptance measurement** the ticket
asks for ("after `go test -count=1 ./cmd/agent/`, no new entry appears in `ls -1 /tmp |
grep gopi`"), plus an audit of the **one hole the fix does not cover** (the anonymous /
ambient temp root), and an honest statement of what is left for the operator.

## Completed

- **Re-measured the epic on master** (`0cd8a90`). The three named sites are gone:
  - `cmd/agent/cli_version_test.go` — builds into `os.MkdirTemp(testutil.SuiteTempRoot(),
    versionBuildDirPrefix)` (line ~155), caching into `gopiBinOnce`.
  - `cmd/agent/home_settings_test.go` — same shape into `homeSettingsBuildDirPrefix`
    (line ~103).
  - `cmd/agent/settings_nilpanic_test.go` — same shape, and the suite's `TestMain`
    (`cmd/agent/testmain_test.go` → `testutil.PinSuiteForTestMain` /
    `testutil.FinishSuiteForTestMain`) removes the per-run root, so the "no cleanup"
    half is closed too.
- **Acceptance, measured twice, on the live tree**: `ls -1 /tmp | grep gopi` before vs
  after a full `go test -count=1 ./cmd/agent/`:
  - run 1: `comm -13 before after` → **empty** (no new entry).
  - run 2: same (see the mtime proof below).
- **mtime proof (stronger than the diff)**: the three pre-fix artifacts still on disk
  are dated `2026-09-07 22:16` (`/tmp/gopi-contract-test`), `2026-09-08 22:19`
  (`/tmp/gopi-home-settings-test`), `2026-09-11 07:35` (`/tmp/gopi-version-test`) —
  i.e. **older than every run performed here**. Were any test still building to those
  fixed paths, these mtimes would have moved; they did not.
- **Gate is live and non-vacuous**: `assertTestSourcesCreateNoTmpPath`
  (`internal/testutil/scripts_gate_test.go:533`, invoked from
  `TestScriptsAreBoundedAndOffTmp`) scans **325 test files, 3 exempted by name**, and
  carries a vacuous-pass guard (`gateMinTestGoFiles`) plus a "stale exemption" check —
  an exemption whose file disappeared is itself an error.
- **Residual hole identified, precisely** (see Not Done): the gate's literal rule needs a
  `/tmp` **string literal**, so it cannot see the ambient spelling
  (`os.MkdirTemp("", …)`, `os.TempDir()`) — the one form that reaches `/tmp` without
  naming it. Repo-wide there are **6** such mentions in test sources; classifying them
  one by one leaves exactly **one live defect**:
  `internal/agent/testmain_isolation_test.go:58` `os.MkdirTemp("", agentSuiteScratchPrefix)`
  — in a suite whose `TestMain` **does** pin the scratch root
  (`testutil.PinSuiteForTestMain`), so that scratch (the pinned knowledge-base dir) was
  silently created on the ambient root, outside the per-run cleanup. That defect is
  already fixed on the in-flight branch `fix/test-scratch-ambient-tmp` (worktree
  `~/workspace/gopi-tmpbin-fix`, commit `6093c76`), which adds the second predicate
  (`ambientTmpRootIsCreatedBy`) and a minimal sample
  (`TestAmbientTmpRootCreationPredicate`); I verified that branch is green here:
  `Go /tmp gate: 325 test files scanned, 3 exempted by name (3/3)` **PASS**.

### The other 5 ambient mentions are NOT defects (classification, so they are not
"fixed" by a future sweep)

| site | why it is inert |
| --- | --- |
| `internal/tools/read_test.go:224` | derives a display NAME (`filepath.Base(os.TempDir())`), creates nothing |
| `internal/agent/session_gopihome_test.go:116` | an assertion; `os.TempDir()` is the pinned root inside that suite, so it asserts hermeticity |
| `internal/testutil/scratchroot_test.go:185-190` | asserts the scratch home is *not* `os.TempDir()` — the guard, not the defect |
| `internal/testutil/scripts_gate_test.go:316` | marker-table data for the detector itself |
| `internal/testutil/pin_coverage_test.go:143,144,193` | synthetic inputs of the predicate's minimal sample |

## Not Done (honest)

- **The gate still has a same-line blind spot**: a `/tmp` path built on one line and
  written on another is not caught. Closing it needs an AST/dataflow pass — a different
  (much larger) gate, deliberately not claimed here.
- **No code change was made by this ticket**, because master already carried the fix
  (`d7b6d3b`). The ambient-form half is owned by `fix/test-scratch-ambient-tmp`
  (`6093c76`, unmerged as of this write-up) — I verified it rather than duplicating it.
- **The sibling triplication ticket is untouched**: the `gopiBinOnce` build cache is
  duplicated across `cmd/agent/{cli_version,home_settings,cli_contracts,settings_nilpanic}_test.go`
  with last-writer-wins on package vars (queue row `01a08dcd-1ed0-…`). Separate work item,
  by design.
- **Host scratch left in place** (a destructive host action is outside a test-only
  ticket, and removing other agents' `/tmp` artifacts would be worse than leaving them).
  The three old gopi binaries are re-buildable artifacts, mapped by **no** live process
  (`/proc/*/maps` → 0 hits each), ~60MB total. Operator command:
  `rm -f /tmp/gopi-contract-test /tmp/gopi-home-settings-test /tmp/gopi-version-test`
  (the `/tmp` tree as a whole holds 226 `gopi*` entries / 2.7GB from earlier work —
  age-sweeping it is the separate scratch-GC ticket `01a08d8f`).
- **An unrelated, reproducible test failure is red in the suite**:
  `TestWebToolsSignalShutdownWritesSessionEnd` (`cmd/agent/cli_web_align_test.go:499`,
  "no session dir/files under …/.gopi/sessions"). It reproduces **3/3** in isolation
  (`go test -count=3 -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/`) and
  once in a full-suite run. Root cause: the test treats a successful `POST /api/chat` as
  "server up" and then immediately SIGTERMs, so it races the session-dir creation the
  assertion reads. **Not caused by this ticket** — this ticket changes no production or
  test code, and the failure fired in a run whose `/tmp` diff was empty. Attribution
  honesty: a probe at the pre-merge commit `38bf23d` printed `ok`, but non-verbose
  `go test` prints no PASS/SKIP and the test `t.Skip`s when `DEEPSEEK_API_KEY` is unset —
  that result is consistent with "passed" OR "skipped" and therefore does NOT establish
  the failure as pre-existing. Filed as its own queue row:
  `01a08dfb-a1dd-74d6-86e3-fbb5428a48bb`.

## Verification

- Build: `go build ./...` — **OK**
- Gate + minimal samples (master): `go test -count=1 -run
  'TestScriptsAreBoundedAndOffTmp|TestAmbientTmpRootCreationPredicate|TestTmpPathCreationPredicate'
  ./internal/testutil/` — **ok**; gate log line: `Go /tmp gate: 325 test files scanned, 3
  exempted by name (3/3)`
- Ambient branch (`~/workspace/gopi-tmpbin-fix`): same three tests — **PASS**
  (`TestScriptsAreBoundedAndOffTmp`, `TestAmbientTmpRootCreationPredicate`,
  `TestTmpPathCreationPredicate`)
- Acceptance (no new `/tmp` entry), twice:
  `cd <repo> && ls -1 /tmp | grep gopi | sort > before; go test -count=1 ./cmd/agent/;
  ls -1 /tmp | grep gopi | sort > after; comm -13 before after` → **empty**
- Artifact mtimes unchanged across both runs (Sep 07 / Sep 08 / Sep 11 07:35, all
  predating the runs) — the fixed-path builds are provably gone
- Commit: this devdoc's own commit on branch `docs/test-tmpbin-closeout` (head hash:
  `git log --oneline -1 docs/test-tmpbin-closeout`)
- Incident during this task (recorded honestly): the first attempt let a `git commit` run
  in the shared main repo (a `cd` into a not-yet-existing worktree silently failed), so
  the devdoc briefly landed on `master`. Corrective action taken immediately:
  `git reset --mixed 0cd8a90` (pointer + index only — never `--hard`, which could have
  wiped another agent's tracked edits), the doc removed from the main tree, master
  verified back at `0cd8a90`, and the same commit re-applied on the proper branch in a
  worktree.

## Deliverable

Branch **`docs/test-tmpbin-closeout`** (worktree `~/workspace/gopi-tmpbin-doc`, shared
repo — commits are already on the branch, no push needed).
Recovery: `git show docs/test-tmpbin-closeout:devdoc/tmpbin-test-scratch-closeout.md`

---

# Appendix — second run (independent re-verification, same day)

A second session re-ran this ticket from scratch against master `0cd8a90` and reached the
same conclusion, by measurement rather than by reading the above. Commands and results:

| check | command | result |
| --- | --- | --- |
| the three named sites are gone | `git merge-base --is-ancestor d7b6d3b master` | **ancestor — yes** |
| they now build into the pin | `git grep -n 'SuiteTempRoot()' master -- cmd/agent/{cli_version,home_settings,settings_nilpanic}_test.go` | 3 hits: `cli_version_test.go:155`, `home_settings_test.go:103`, `settings_nilpanic_test.go:101` — all `os.MkdirTemp(testutil.SuiteTempRoot(), …)` |
| acceptance (empty diff) | `go test -count=1 ./cmd/agent/` with `ls -1 /tmp \| grep gopi` captured before and after | `comm -13` → **empty** (2 runs) |
| the artifacts are fossil | `ls -l /tmp/gopi-{contract,home-settings,version}-test` | `Sep 7 22:16` / `Sep 8 22:19` / `Sep 11 07:35` — all mtimes predate both runs, so no test rebuilt them |
| build green | `go build ./...` (in both worktrees) | **OK** |
| the ambient-hole branch is green and ff-able | `go build ./... && go test -run 'TestScriptsAreBoundedAndOffTmp\|TestAmbientTmpRootCreationPredicate\|TestTmpPathCreationPredicate' ./internal/testutil/` | `ok … 0.266s`; `git merge-base --is-ancestor master HEAD` → branch is ahead of master, **ff-able** |

**The residual hole's fix is real, not a stub.** The second predicate is two-sided: it
fires on the ambient spellings (`os.MkdirTemp(""`, `os.CreateTemp(""`, `os.TempDir()`)
only when a creating call shares the line (`os.MkdirTemp(`, `os.WriteFile(`, `os.Chdir(`, …),
and it explicitly clears a line that redirects the ambient root into
`testutil.SuiteTempRoot()`. Its minimal sample asserts the two rules stay **separable**
(the literal rule must NOT claim an ambient path), so the second half cannot be dead
weight hiding behind the first. The single live defect it catches,
`internal/agent/testmain_isolation_test.go:58`, is fixed **and** has a regression
assertion added at the same time (`TestSuiteLedgerPin_IsActive` now requires the ledger
path to sit under `testutil.SuiteTempRoot()` — the ambient form produced the same
basename on `/tmp` while the suite's pin was bypassed, which is exactly why the basename
check alone was not enough).

## Handoff (both branches are the PA's to land)

`safe-merge.sh` reports both branches as **not yet merged into master**, blocked solely by
guard `[c]` — the branch is still checked out in a worktree. That is the guard working as
designed (it will not merge a branch whose worktree is live), not a defect:

```
bash scripts/safe-merge.sh fix/test-scratch-ambient-tmp   → exit 1, only [c]
bash scripts/safe-merge.sh docs/test-tmpbin-closeout      → exit 1, only [c]
```

To land them, remove the worktrees first, then merge (fast-forward is available for both):

```bash
git worktree remove ~/workspace/gopi-tmpbin-fix   # → then merge fix/test-scratch-ambient-tmp
git worktree remove ~/workspace/gopi-tmpbin-doc   # → then merge docs/test-tmpbin-closeout
```

**Nothing in this ticket is unmerged *code* that master needs**: the ticket's own named
sites were fixed by `d7b6d3b` (already in master). `fix/test-scratch-ambient-tmp` is the
hardening of the gate plus the one live defect that hardening caught; it is a strict
improvement (green, ff-able) but the ticket's acceptance is **already satisfied on
master**.
