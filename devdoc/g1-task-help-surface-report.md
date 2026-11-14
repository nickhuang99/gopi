<!-- doc-class: historical -->
# Task Report: G1 — `gopi task` help-surface hardening (17 advertised verbs × BASIC test + inventory)

**Task**: 01a0c2d1-9b10-7290-b3bb-7cf17dbede99 (epic brief: `briefings/brief-help-surface-hardening.md`, family G1).
**Branch**: `feat/g1-task-help-surface` (worktree `~/workspace/gopi-g1-tasksurface`, Rule 6a).
**Companion deliverable**: `devdoc/gopi-task-help-surface-inventory.md` — the
re-derivable verb/flag inventory (17 verbs, 51 advertised verb-flag pairs).

## Completed

1. **The surface list is DERIVED, twice, from the help itself.**
   - `scripts/g1-task-help-surface.sh [bin]` runs `gopi task -h` and emits a TSV
     of `verb ⇥ sorted-flags ⇥ flag-count` from the synopsis heads (the command
     quoted in the inventory doc is exactly this).
   - `TestTaskHelpSurfaceMatchesInventory` re-derives the same list inside the
     test package from `tasksUsage` — the constant the binary prints — and
     fails when the help gains/loses a verb the pinned expectation and the
     covered set do not account for. Result: **17 verbs**, equal to
     `taskverbs.FamilyVerbs` (the briefing's "17 subcommands", confirmed rather
     than assumed).
   - `TestTaskHelpSurfaceInventoryTableMatchesHelp` reads the checked-in table
     back and fails if it drifts from the help (red-proof below), so the
     deliverable cannot go stale silently.
2. **17 per-verb BASIC tests**, each driving the REAL binary over the help's own
   argv (`gopiTestBinPath` + `cliContractEnv` + a per-test ledger) — happy path
   plus the verb's first refusal, never `-h`:
   `TestTaskVerb{List,Show,ForBranch,Status,TagResumed,Archive,Update,Verdicts,
   SweepWorktrees,FinishCleanup,SweepZombies,Search,Improvements,DedupScan,
   Submit,Verify,Reopen}RunsAsAdvertised`.
3. **Four surface gates** (21 Test funcs in the file, all listed in Verification):
   the two derivation gates above; `TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted`,
   which sweeps all 51 advertised verb-flag pairs through `parseTasksArgs` so
   "advertised but not admitted" is closed for the WHOLE family rather than per
   verb; and `TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest` (the
   inventory is a normative devdoc and must be classed in `devdoc/DOC-INDEX.tsv`
   with a resolvable spot-check).
4. **Two contained one-invocation defects FIXED** (red→green transcripts kept;
   see Verification):
   - `sweep-worktrees --json` — advertised in the synopsis, read by
     `runTaskSweepWorktrees` (`flags.jsonOut`), refused by `taskFlagMember`.
   - `verdicts <stray>` — the parser's comment promised "no positional" while
     the code accepted and ignored one (the enforcing arm was unreachable dead
     code); now refused by name, dead arm removed.
5. **One finding filed as a bug task** (design decision, not a drive-by):
   `01a0cb36-1bb4-708b-a962-39a4955b9d65` — `gopi task improvements` advertises
   no `--db` while the same help's "Common flags" paragraph advertises `--db`
   as the group-wide ledger override, and the verb refuses it
   (`improvements: unknown flag --db`). Recorded as F3 in the inventory.

## Not Done (honest)

- `verify`'s happy path runs a live LLM child: the basic test proves the argv
  reaches the verb (not the usage dump) and asserts the guard refusal; the
  verified-green path needs a live model and remains in the e2e suites. Not a
  silent skip — stated in the inventory's "honest limits" section.
- The DESTRUCTIVE halves of `finish-cleanup --apply` / `sweep-worktrees --apply`
  (they move real git worktrees) are not re-run here; their existing homes keep
  that coverage, and the G1 cases exercise the plan/`--json` faces + argv
  errors. Also recorded in the inventory.
- Sibling surfaces G2–G6 (the `sync` pins, the merger/worker pages) are out of
  scope by the family boundary; the inventory records them as pointers, not
  rows.
- The `improvements` `--db` defect is NOT fixed here (F3): the correct
  resolution is a help/behaviour contract decision, and the briefing forbids
  rewriting help to match broken behaviour.

## Verification

**Build**
```
$ gofmt -l cmd/agent/cli_tasks.go cmd/agent/cli_tasks_help_surface_basic_test.go
(no output — clean)
$ go build ./...
(no output — green)
$ go vet ./cmd/agent/
(no output)
```

**Tests — the 21 new tests, by exact invocation** (whole `-run` selector shown,
since a per-test `-run` changes nothing but the selection). Times are from the
recorded run in `g1-newtests.txt`; the transcript is the authority:
```
$ go test -count=1 -run 'TestTaskHelpSurface|TestTaskVerb' -v ./cmd/agent/
--- PASS: TestTaskHelpSurfaceMatchesInventory (0.00s)
--- PASS: TestTaskHelpSurfaceInventoryTableMatchesHelp (0.00s)
--- PASS: TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest (0.00s)
--- PASS: TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted (0.00s)
--- PASS: TestTaskVerbListRunsAsAdvertised (4.53s)
--- PASS: TestTaskVerbShowRunsAsAdvertised (0.74s)
--- PASS: TestTaskVerbForBranchRunsAsAdvertised (1.88s)
--- PASS: TestTaskVerbStatusRunsAsAdvertised (0.88s)
--- PASS: TestTaskVerbTagResumedRunsAsAdvertised (1.61s)
--- PASS: TestTaskVerbArchiveRunsAsAdvertised (3.03s)
--- PASS: TestTaskVerbUpdateRunsAsAdvertised (2.03s)
--- PASS: TestTaskVerbVerdictsRunsAsAdvertised (0.93s)
--- PASS: TestTaskVerbSweepWorktreesRunsAsAdvertised (4.60s)
--- PASS: TestTaskVerbFinishCleanupRunsAsAdvertised (0.87s)
--- PASS: TestTaskVerbSweepZombiesRunsAsAdvertised (0.72s)
--- PASS: TestTaskVerbSearchRunsAsAdvertised (0.63s)
--- PASS: TestTaskVerbImprovementsRunsAsAdvertised (0.86s)
--- PASS: TestTaskVerbDedupScanRunsAsAdvertised (0.90s)
--- PASS: TestTaskVerbSubmitRunsAsAdvertised (0.80s)
--- PASS: TestTaskVerbVerifyRunsAsAdvertised (1.66s)
--- PASS: TestTaskVerbReopenRunsAsAdvertised (2.31s)
ok  	github.com/nick/gopi/cmd/agent	31.312s
exit=0
21 passed, 0 failed
```

**Tests — scope-limited suite** (smokegate / Rule 27: smoke, branch-scoped to
`cmd/agent` — the only package the diff touches):
```
$ make test-budget PKGS='./cmd/agent'
```
…plus the gates this change must not trip (`./internal/standards/`,
`./internal/testutil/`, `./internal/stylegate/`, `./internal/taskverbs/`).
Full transcript + exit codes: session evidence `g1-scope-suite.txt`.

**A pre-existing RED in `internal/standards` (NOT this branch's, filed as
`01a0cb38-2e21-711a-b60c-a784d09f3630`).** The docclass truth-class gates are
red on master with this branch's devdoc changes removed and `DOC-INDEX.tsv` at
HEAD: `devdoc/merge-live-owner-fence.md` has no manifest row, and
`tool-use-tool-result-pairing-invariant.md` / `unknown-single-dash-option.md`
are classed `historical` with no class banner. Transcript and the pristine-tree
control: `g1-docclass-master-red.txt`. The two rows THIS change adds for its own
devdoc files are verified by `TestTaskHelpSurfaceInventoryRowIsInTheTruthManifest`
(and `docclass.ParseEvidence(...).Check(...)` on the row's spot-check).

**Ran (Rule 31) — the surfaces the fix changed, one invocation each.** Both
defects were captured RED on a binary built from `cli_tasks.go` at HEAD
(`git show HEAD:cmd/agent/cli_tasks.go` → `go build`) and GREEN on the branch's
tree, from the SAME ledger path:```
=== PRE-FIX binary (cmd/agent/cli_tasks.go at HEAD) ===
$ /tmp/.../gopi-prefix task sweep-worktrees --json --db <tmp>/g.db
gopi task sweep-worktrees: unknown flag --json            exit=1
$ /tmp/.../gopi-prefix task verdicts stray --db <tmp>/g.db
no reframe records yet (no task(action=reframe) call…)   exit=0
=== POST-FIX binary (the worktree's cli_tasks.go) ===
$ /tmp/.../gopi-fixed task sweep-worktrees --json --db <tmp>/g.db
{ "findings": [ …                                        exit=0
$ /tmp/.../gopi-fixed task verdicts stray --db <tmp>/g.db
gopi task verdicts: unexpected positional argument "stray" (… takes no positional)
                                                          exit=1
```
Full transcript: session evidence `g1-redproof-green.txt`.

**Re-derivation of the surface list** (the command quoted in the inventory):
```
$ scripts/g1-task-help-surface.sh ./bin/gopi
list	all,db,status,sweep	4
… (17 rows, 51 verb-flag pairs) …
reopen	db,reason	2
```

**Red proofs (each gate is shown failing where it must).**
```
# (a) the flag gate, with the two fixes reverted (cli_tasks.go at HEAD):
$ go test -count=1 -run TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted ./cmd/agent/
--- FAIL: TestTaskHelpSurfaceAdvertisedFlagsAreAdmitted (0.00s)
    `gopi task sweep-worktrees --json` is REFUSED by the flag table while the synopsis advertises it: unknown flag --json
FAIL
# … green again on the branch: ok  github.com/nick/gopi/cmd/agent

# (b) the inventory-table gate, with one verb row deleted from the doc:
$ go test -count=1 -run TestTaskHelpSurfaceInventoryTableMatchesHelp ./cmd/agent/
--- FAIL: TestTaskHelpSurfaceInventoryTableMatchesHelp (0.00s)
    want-only (advertised/claimed but not present): tag-resumed
FAIL
# … green again with the row restored: ok
```
Transcripts: `g1-flaggate-redproof.txt`, `g1-inventory-redproof.txt`.

**A third red proof, this one found by the CLOSE-OUT suite and fixed in the same
change (the self-inflicted shape of the same class).** Running
`go test -run TestStandardGates ./internal/standards/` on the branch tree went
RED while the PRISTINE master control (`~/workspace/_redproof-escape`,
`ccdd5f1e`, no branch changes) was GREEN — so this one was mine, not master's:

```
$ cd ~/workspace/gopi-g1-tasksurface && go test -count=1 -run TestStandardGates ./internal/standards/
--- FAIL: TestStandardGates (5.73s)
    gate_test.go:113: Rule 13 (no hardcoded worktree root literal): NEW violation
      (checklist 1b/worktree-root) at cmd/agent/cli_tasks_help_surface_basic_test.go:298
      ["/tmp": reaches for the BARE HOST ROOT inside a fixture — name it as a fixture
      constant in this file, or reference worktreeroots.ShmDir / .AmbientTempRoot /
      .FixtureBannedRoot …]
$ cd ~/workspace/_redproof-escape && go test -count=1 -run TestStandardGates ./internal/standards/
ok  	github.com/nick/gopi/internal/standards	7.566s     # pristine master: the difference is this branch's
```

The offending site was the flag-sweep gate's probe values (`"repo": "/tmp"`,
`"attachment": "/tmp/…"`) — a bare host temp root typed into a fixture, exactly
what Rule 13/22 forbid and what the flag gate's own error text names. Fixed by
taking the directory from `t.TempDir()` (the harness's own pinned scratch root,
Rule 22/32) instead of a literal; no assertion changed.
`gofmt -l` clean, the branch tree's `TestStandardGates` green again
(`ok … 8.486s`), the 21 new tests still green, and the fix is in the branch
commit. Transcripts: `g1-branch-vs-pristine-standards.txt`,
`g1-pristine-control-2.txt`.

## Evidence (self-filed)

- `g1-newtests.txt` — the focused run: `exit=0`, **21 `--- PASS`** (17 per-verb
  cases + 4 surface gates), `0 --- FAIL`, `ok github.com/nick/gopi/cmd/agent`.
- `g1-redproof-green.txt` — the pre-fix/post-fix binary transcript above
  (identical ledger path both sides of the pair).
- `g1-flaggate-redproof.txt` / `g1-inventory-redproof.txt` — each gate's FAIL
  text and its green re-run.
- `g1-docclass-master-red.txt` — the pristine-tree control showing the
  `internal/standards` docclass red is master's, not this branch's.
- `g1-preexisting-coverage.txt` — the re-derivation of the inventory's
  "pre-G1 coverage" column (`grep -lE '"task", *"<verb>"'` plus a
  `taskverbs.<Verb>` sweep over `cmd/agent`'s test files).
- `g1-surface.tsv` / `task-help.txt` — the derived surface and the help it was
  derived from (`gopi task -h`, 281 lines, 17 synopsis heads by
  `grep -cE '^  gopi task [a-z]'`).
- `g1-scope-suite.txt` — the scope-limited suite transcript.

## Rule 30 close-out

`git status --porcelain` in the worktree prints NOTHING after the branch's
single commit (`test(task-help): a BASIC invocation test for every advertised
gopi task verb…`; its id is `git rev-parse HEAD` in the worktree, deliberately
not quoted inside the file that commit contains). The six deliverable paths are
all in it: `cmd/agent/cli_tasks.go` (the two contained fixes),
`cmd/agent/cli_tasks_help_surface_basic_test.go` (21 tests: 17 per-verb cases + 4 surface gates),
`devdoc/gopi-task-help-surface-inventory.md` (the task-facing inventory),
`devdoc/g1-task-help-surface-report.md` (this report),
`devdoc/DOC-INDEX.tsv` (the two manifest rows), and
`scripts/g1-task-help-surface.sh` (the re-derivation command). The ad-hoc
`zz_g1dump_test.go` probe used for the scanner measurement was removed, not
left behind.

## Addendum (close-out session) — the suite reds are master's, proven by a pristine control

The `## Verification` section above reported the scope-limited suite. This
addendum completes that measurement, because the suite is RED on this branch
and a red suite is only reportable next to a control: **a pristine master
worktree with none of this branch's changes produces the IDENTICAL failing
set**, so every one of those reds is master's, not this diff's.

Control: `git worktree add --detach /home/user/workspace/g1-pristine-ctl master`
(`ccdd5f1e`, `git status --porcelain` empty), `CGO_ENABLED=0 go build -o
bin/gopi ./cmd/agent/` — the binary matters: without it five of these e2e cases
report `gopi binary not found … run scripts/e2e_smoke.sh first` and the
comparison is vacuous (measured: the first control run "passed" for exactly
that reason, and was discarded).

```
$ cd /home/user/workspace/g1-pristine-ctl && go test -count=1 -p 1 -run <the-15-selector> ./cmd/agent/
$ cd /home/user/workspace/gopi-g1-tasksurface && go test -count=1 -p 1 -run <the-15-selector> ./cmd/agent/
$ diff <(…pristine FAIL lines…) <(…branch FAIL lines…)
diff_exit=0        # identical failing set, both sides
```

Both sides report the SAME seven, in the same order —
`TestManualRunRecyclesItsWorktree`, `TestResumeReclaimsTheWorktreeItFinishedIn`,
`TestTaskRunSeamWritesReportAndShowRendersIt`,
`TestWorkerDeliveryRecyclesDespiteAStaleSiblingRow`,
`TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves`,
`TestWorkerRunRecordsTheMissingRanLineE2E`,
`TestWorkerServerDeliveryRecyclesThenMergerMerges`. The 15th
(`TestBareRunKeepsAllItsArtifactsInOneSessionDir`, 0.00s in the budget run)
passes under `-p 1` on BOTH trees and fails under the package's parallel budget
run on both — order/machine-state dependent, never one side only. Gate
packages, same control, same conclusion:

```
PRISTINE master ccdd5f1e:  FAIL internal/standards (29.577s)  FAIL internal/testutil (41.991s)  FAIL internal/stylegate
BRANCH:                    FAIL internal/standards (21.704s)  FAIL internal/testutil              FAIL internal/stylegate
```

The `internal/standards` failures are the two docclass gates already filed as
`01a0cb38` (`devdoc/merge-live-owner-fence.md` has no row — reproduced on the
pristine control with this branch's devdoc changes absent — and two `historical`
docs without their banner). The control's failure LINES and the branch's are
byte-identical (`diff` of just the `no row in` / `does not open` lines →
`IDENTICAL`); only the manifest's own counters move, by exactly this change's
two added rows (2210→2212 rows, 744→745 bannered docs, 0 mislabelled on both
sides). `internal/stylegate` and `internal/testutil` fail the same way on
master with the same two named tests (`TestStyleGate`,
`TestScriptsAreBoundedAndOffTmp` / `TestRule27NoDeviceLoadGenerators`).

**What this session re-verified on the branch (unchanged from the first run):**
`gofmt -l` on both changed files → no output; `CGO_ENABLED=0 go build ./...` →
exit 0; `go test -count=1 -run 'TestTaskHelpSurface|TestTaskVerb' -v
./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 68.199s`, 21 `--- PASS`,
0 `--- FAIL`; the surface re-derivation
(`scripts/g1-task-help-surface.sh ./bin/gopi`, and the in-package re-derivation)
→ the same 17 rows / 51 verb-flag pairs as the checked-in table.

Evidence: `g1-pristine-control-e2e.txt` (the 7-case control + `diff_exit=0`),
`g1-pristine-control-all15.txt`, `g1-pristine-control-gates.txt`,
`g1-branch-vs-pristine-standards.txt`, `g1-final-verify-closeout.txt`.

**Rule 31 re-check of the two fixed surfaces, one invocation each on the
branch's own binary** (`./bin/gopi`, built from this tree), exit codes captured
(`g1-rule31-invocations.txt`):

```
$ ./bin/gopi task sweep-worktrees --json --db <tmpdir>/g2.db
{ "findings": [ …                                          exit=0
$ ./bin/gopi task verdicts stray --db <tmpdir>/g2.db
gopi task verdicts: unexpected positional argument "stray" …   exit=1
$ ./bin/gopi task verdicts --db <tmpdir>/g2.db
no reframe records yet …                                   exit=0
$ ./bin/gopi task improvements --db <tmpdir>/g2.db        # F3, unchanged by design
gopi task improvements: unknown flag --db                  exit=1
```

`verdicts` with no positional still succeeds (the contract is "no positional",
not "always refuse"), and both fixes' claimed effects are visible: JSON on
stdout, refusal with a non-zero exit.

**Final re-verify on the branch tip** (after this addendum's edits, so the
quoted numbers belong to the tree that ships):

```
$ gofmt -l cmd/agent/cli_tasks.go cmd/agent/cli_tasks_help_surface_basic_test.go
(no output — clean)
$ CGO_ENABLED=0 go build ./...
exit=0
$ go test -count=1 -run 'TestTaskHelpSurface|TestTaskVerb' -v ./cmd/agent/
… 21 × --- PASS, 0 × --- FAIL …
ok  	github.com/nick/gopi/cmd/agent	26.941s
$ scripts/g1-task-help-surface.sh ./bin/gopi | wc -l
17
$ git status --porcelain
(no output)
```

Transcript: `g1-final-verification.txt`. **The commit hash is deliberately not
quoted inside the files that commit contains** — a hash written into a file its
own commit produces is stale by construction; `git rev-parse HEAD` in the
worktree `/home/user/workspace/gopi-g1-tasksurface` (branch `feat/g1-task-help-surface`)
is the authority, and the single commit is
`test(task-help): a BASIC invocation test for every advertised `gopi task` verb, plus the derived surface inventory`.

**Honest note added by the close-out session**: the scope-limited suite
`make test-budget PKGS='./cmd/agent'` is RED on this branch AND on pristine
master (identical failing set, above) — the reds are master's, and this change
introduces none of them. The 21 G1 tests this change adds are green on every run
recorded here.
