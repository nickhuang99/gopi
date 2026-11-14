<!-- doc-class: historical -->
# Task Report: a verb after a run flag is refused, not run as a prompt

Task `01a0dd02-d0a2-7433-8b7f-2dec08b76b12` (branch `task/01a0dd02d0a274338b7f2dec08b76b12`).

## Overview

`gopi --timeout 3 version` did not print a version. It started a **real agent
run**: a session directory, a live `[auto] version` ledger row, tokens spent —
for a word the operator meant as a command.

**Why.** Dispatch reads the verb from the **FIRST argument** (the chain at the
top of `cmd/agent/main.go` switches on `argv[1]`, then on `argv[1]+argv[2]`),
while the **run path** accepts *any leftover positional* as the task prompt
(`FlagOverrides` collects flag-aware positionals; `readInput` turns them into
the prompt). `--timeout 3` consumes two slots, so `version` is `argv[3]` —
dispatch never sees it, and the run path happily accepts it.

**Mechanism, precisely.** Two readers disagree about what a positional *is*:
the dispatcher reads position `1`, the runner reads position `N`. Any verb that
lands after a run flag therefore passes the dispatcher unexamined and arrives at
the runner as free text. This is not a validation gap but a **missing cross
check** between two components that both behaved as written.

## Completed

- **The guard** (`internal/config/cli_verb_guard.go`): `PositionalVerbs` (the
  dispatched vocabulary), `PositionalCommandVerb(cfg)` (flag-aware: returns the
  leftover positional iff it names a dispatched verb **and** no explicit prompt
  is in force) and `PositionalCommandVerbError(verb)` (the pointed message).
- **Wired into the run path** (`cmd/agent/main.go`): the refusal runs
  immediately **before `readInput`** — the one moment a leftover positional is
  about to become the prompt of a real run — printing to stderr and exiting `1`.
- **Opt-out that is not a fallback** (Rule 15): `--prompt version` (and the
  `GOPI_PROMPT` / settings `"prompt"` channels) means *the word is literal*, so
  nothing is refused. Deliberate speech stays possible; silence never happens —
  the guard either dispatches (verb first), runs (explicit prompt / non-verb
  word), or refuses with a message naming both spellings.
- **Rule 1b/13 sweep**: `GOPI_PROMPT` was a bare literal in three places
  (`env.go` publisher, `settings.go` catalog row, `main.go` reader). It is now
  the named `config.PromptEnvVar`, referenced everywhere.
- **Tests**: the predicate unit (`internal/config/cli_verb_guard_test.go`, 4
  tests) and the run path + vocabulary mirror (`cmd/agent/cli_verb_guard_test.go`,
  3 tests). One shared probe helper was extracted in `cli_contracts_test.go`
  (`cliContractProbeWithTimeout`) so the run-path test reuses the existing
  no-agent-loop probe instead of hand-rolling a second one.
- **Doc-index compliance** (found by the standards gate, not by me): a new
  `devdoc/*.md` needs a `DOC-INDEX.tsv` row **and** a `<!-- doc-class: historical -->`
  banner on its first line, or `internal/standards/docclass_gate_test.go` fails.
  Both added for this report.
- **An inherited orphan row repaired**: master carries
  `devdoc/docindex-write-path-row-ownership.md` **twice** in `DOC-INDEX.tsv` —
  once at its sorted position (line 10) and once as a duplicate in the
  `docindex-*` block (line 762). Commit `673c8883` ("a write stops
  re-adjudicating a document that was already classified") deleted the line-762
  copy as a duplicate but left the *file* in the tree, and the branch
  base measured here had **zero** rows for that path — so the coverage gate saw
  an unclassified file. I restored exactly one row at the sorted position
  (line 2168 area), which is what `master` itself holds at line 10.

## The message (what the operator actually sees)

```
gopi: "version" is a gopi command, not prompt text — gopi reads the verb from the
FIRST argument, so it must precede every run flag: `gopi version`. To send the word
as a prompt instead, say so explicitly: `--prompt version`.
(For this command's own help: `gopi version -h`.)
```

Three things are named, in the order the operator needs them: **what went
wrong**, **the correct spelling**, **the escape hatch** — plus this command's
own help line, so the next keystroke is productive.

## Two measured facts that shaped the design

1. **`--timeout 3 task list` ALREADY dispatched — before this change.** The
   `task` verb is special-cased: `isTasksSubcommand` scans the *whole* argv for
   the word (an ssh-passthrough legacy). So the guard's claim is narrower than
   the original bug report assumed: a verb after a run flag is refused **unless
   some other scan already dispatches it**. `task list` is therefore *excluded
   from the refusal claim* and listed here rather than silently counted as a win.
2. **`web_search` / `web_fetch` dispatch but are absent from
   `topLevelCommandNames`** (deprecation aliases). A vocabulary derived only
   from the completion table would have missed them, so `PositionalVerbs` was
   built from a **measured sweep** — every word in the set was verified to
   dispatch (`gopi <word> -h` → rc=0) — not copied from a table. The exhaustive
   mirror test (`TestPositionalVerbGuardVocabularyMatchesDispatch`) keeps the two
   in step from now on.

## Where the guard lives, and why

`internal/config`, not `cmd/agent`, because the classification must run on the
**same flag-aware positional slice** that decides what the prompt is
(`FlagOverrides` lives there), and `internal/config` cannot import `cmd/agent`
(package `main` → import cycle). So the vocabulary is a **self-contained copy,
pinned by an exhaustive contract test in `cmd/agent`** — the package that owns
the real dispatch surface. When the two disagree, the build fails rather than
the guard going quietly stale. This is the honest shape of the constraint: a
mirror is acceptable only with a gate that makes the mirror impossible to drift.

`PositionalVerbs` are words, not CLI constants, for the same reason:
`cmd/agent/cli_verbs.go`'s constants cannot be imported from here. The names in
that file remain the CLI's own source of truth for its verbs; the contract test
is what binds them.

## Not Done

- **`PositionalVerbs` is a hand-maintained mirror.** The contract test makes
  drift *detectable*, not *impossible*: a new verb added to the dispatcher in
  `cmd/agent` fails the test, so a developer must add the word here — a
  deliberate two-step rather than a single source. Unifying it would need the
  vocabulary to move to a leaf package both sides can import, which is a
  refactor of the dispatch chain, not this fix. Named here so it is not
  mistaken for a finished single source of truth.
- **One word in the set is not a dispatched verb's own help path**: `completion`
  and `raw`/`stats` are in the vocabulary because they are real top-level verbs;
  the sweep confirmed rc=0 for each. No known gap remains in the set as
  measured today.
- **No guard for the mirror case of a *flag value* that names a verb**: a
  positional is only classified when it is a *leftover*, which is the correct
  (flag-aware) behaviour — a verb-named flag value is never a candidate.
- **Three unrelated `cmd/agent` tests fail on this machine — proven INHERITED,
  not introduced here.** `TestMergePrelandGateRefusesTheIncidentShapeEndToEnd`,
  `TestMergeUnionLaneRefusesAGenuineSemanticConflict`,
  `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness` fail on my
  branch **and** on a fresh `git worktree` of clean `master` (79807932) built the
  same way — identical FAIL set, no code of mine involved (they are merge-lane /
  serve-lock tests; my diff touches neither). An earlier run also showed two
  stale-binary guards (`TestRawPromptRunReleasesItsWorktree`,
  `TestWorkerRefusesWhenTheGroundCannotBeMade`) firing because a source file was
  newer than `bin/gopi`; after the rebuild they no longer appear in the failing
  set. They belong to their own task.
- The pre-existing `internal/stylegate` failures (unattributed `time.Sleep`
  sites, `os.Setenv` in `internal/lineedit/palette_test.go`, `t.Skip` without a
  literal reason) are likewise **inherited** — every listed site is outside this
  diff.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`
- Tests: `go test ./internal/config/ -count=1` → `ok github.com/nick/gopi/internal/config 6.225s`;
  `go test ./internal/standards/ -count=1` → `ok github.com/nick/gopi/internal/standards 59.885s`
  (this run is also how the DOC-INDEX row + class banner for this very report were
  found and fixed — the gate caught its own devdoc);
  `go test ./cmd/agent/ -count=1 -v -run 'TestPositionalVerbGuardVocabularyMatchesDispatch|TestRunPathRefusesVerbAfterRunFlag|TestRunPathStillRunsBarePromptShapes'`
  → `ok github.com/nick/gopi/cmd/agent 25.900s`, **3/3 PASS** (10 subtests), branch-scoped.
- Ran (Rule 31 — the real binary, real sandbox, real ledger):
  `SB=$(./bin/gopi sandbox create --label epic-01a0dd02 | tail -1)` then
  `GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json ./bin/gopi --timeout 3 version`
- Result: `rc=1`, the refusal message above on stderr, **zero** session dirs for
  that invocation (`.gopi/sessions` holds only the two later run-path probes,
  mtimes 18:11:21 / 18:11:59), and **no** `[auto] version` row created by it —
  the ledger's two `[auto] version` rows trace to the two deliberate
  `--prompt version` probes (18:11:01, 18:14:32). Full transcript:
  `/home/user/.gopi/sessions/01a0dd02-d0a2-7433-8b7f-2dec08b76b12/evidence/acceptance-probe-transcript.txt`
  (evidence lives in the session dir, not the worktree — Rule 30).
- Commit: 04a1262b (the code commit this report describes; this docs commit follows it)

### Probe matrix (all six shapes, measured)

| probe | invocation | expected | observed |
|---|---|---|---|
| P1 | `--timeout 3 version` | refuse, no session, no row | rc=1, pointed message, no session, no row ✅ |
| P2 | `--timeout 3 task list` | real list | `No tasks found.` rc=0 (already dispatched pre-change) ✅ |
| P3 | `--timeout 20 --prompt version` | RUN | ran, session + `[auto] version` row ✅ |
| P4 | `--timeout 5 "say hi"` | RUN | ran, session + `[auto] say hi` row ✅ |
| P5 | `--timeout 5 /…/probe-task.txt` | RUN | ran (positional input file) ✅ |
| P6 | `--timeout 3 --prompt version` | RUN | ran, session + `[auto] version` row ✅ |
| P7 | `version` (verb FIRST — the spelling the refusal prescribes) | dispatch, print version | `v0.1.0` + go/platform lines, rc=0 ✅ |

**The refusal closes its own loop**: the message tells the operator to type
`gopi version`; P7 runs exactly that and prints `v0.1.0`. Advice that does not
work is a second bug, so the prescribed spelling is executed, not asserted.

Sandbox truth (`$SB/sessions`, the initializer's own directory): **0 entries**,
before and after — the sandbox's own `sessions/` is never written by these
probes; run sessions land in `$SB/.gopi/sessions/`.

### Red-proof (the regression test can actually fail)

With the guard **call** removed from `cmd/agent/main.go`,
`TestRunPathRefusesVerbAfterRunFlag` fails (the probe runs to the agent loop
instead of exiting 1 with the message); restoring the call turns it green. A
test that cannot go red proves nothing.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK` (exit 0).
- `go test ./internal/config/ -count=1` → `ok github.com/nick/gopi/internal/config 6.225s` (exit 0).
- `go test ./internal/standards/ -count=1` → `ok github.com/nick/gopi/internal/standards 59.885s` (exit 0).
- `go test ./cmd/agent/ -count=1 -v -run '<my three tests>'` → `ok … 25.900s`, 3/3 PASS
  (`TEST_RUN_STATE`: all three `--- PASS`, subtests included).
- A/B on the three unrelated `cmd/agent` failures: identical FAIL set on a clean
  `master` worktree (79807932, built the same way) → inherited.
- P1 → `rc=1` with the stderr line; `ls -l $SB/.gopi/sessions` → two dirs, both
  later than P1's 18:10 window; `gopi task list` → 4 rows, none created by P1;
  `$SB/sessions` → 0 entries (never written).
- P7 → `gopi version` prints `v0.1.0` (the spelling the refusal tells the operator
  to use actually works), rc=0.
- Red-proof: guard call removed → `TestRunPathRefusesVerbAfterRunFlag` FAIL;
  restored → PASS (transcript in the session dir).
- `internal/stylegate` FAIL sites: all outside this diff (`internal/tools/*`,
  `internal/uiclient/*`, `internal/lineedit/palette_test.go`, `testsuite/*`) —
  inherited, unchanged by this branch.

# Addendum — epic `01a0dd87` (2026-09-26): the flag is named, and the five prefix flags are pinned

Task `01a0dd87-8242-73e7-b665-ebbffdc25e99` (branch `task/01a0dd87824273e7b665ebbffdc25e99`,
base master `79807932`). This run re-derived the defect from scratch and found the
fix already written and already merged on the PEER's master — my host's master was
behind it. What follows is what this run measured, what it did NOT re-do, and the
one thing the epic asked for that the landed fix did not have.

## What the base actually contained (measured, not assumed)

- `git merge-base --is-ancestor 79807932 remotes/peerbox/master` → my base is an
  ANCESTOR of the peer master: the host is behind, nothing is lost.
- `git cat-file -e remotes/peerbox/master:internal/config/cli_verb_guard.go` →
  PRESENT. `cmd/agent/cli_verb_guard_test.go`, `internal/config/cli_verb_guard_test.go`,
  `devdoc/verb-after-run-flag-guard.md` → PRESENT. The guard was merged on the peer.
- On MY base, `devdoc/verb-after-run-flag-guard.md` and `internal/config/cli_verb_guard.go`
  were ABSENT — hence a row whose premise ("the fix does not exist") is true of this
  host and false of the network. The work was brought over rather than rewritten
  (Rule 23: read the old path first; its reasoning is the deliverable).

## What this run ADDED (the epic's requirements the landed fix did not meet)

**1. The refusal now NAMES the misplaced run flag.** The landed message named the
verb and the verb's correct position, but not the flag the operator misplaced:

```
gopi: "version" is a gopi command, not prompt text — gopi reads the verb from the
FIRST argument, so it must precede every run flag: `gopi version`. To send the word
as a prompt instead, say so explicitly: `--prompt version`.
```

The epic requires naming the flag. The guard now carries the flag token, taken from
the SAME arity table the parser binds flags through (`flagArity`, shared by
`FlagOverrides` and `scanPrefixingRunFlag` — Rules 1b/13/14: one definition, read
twice, never mirrored), so the two can never disagree about where the positionals
begin:

```
gopi: "version" is a gopi command, not prompt text — it is a POSITIONAL here,
following the run flag --db, and gopi reads the verb from the FIRST argument.
Put the verb first: `gopi version` (the run flag is not the problem:
`gopi --db <VALUE>` is correct). To send the word as a prompt instead, say so
explicitly: `--prompt version`. (For this command's own help: `gopi version -h`.)
```

The flag's VALUE is deliberately NOT echoed — the guard holds the name, so a path
or an API key cannot be printed back into a terminal; the example carries a
placeholder.

**2. The five prefix run flags are a test matrix** (`cmd/agent/cli_verb_guard_run_flags_test.go`),
which the landed fix did not have (it pinned `--timeout` as one representative
flag). For each of `--db`, `--timeout`, `--raw-log`, `--model`, `--base-url`:

- the MISPLACED order (`gopi --flag VALUE version`) exits non-zero, the refusal
  names the flag and the verb, no session directory is minted, **no `session_start`
  is written anywhere under the sandbox home** (a content walk, not a directory
  count — a layout change cannot make it vacuous), and the run path's ledger banner
  is absent;
- the CORRECT order (`gopi version --flag VALUE`) still dispatches and prints
  `internal/version.Version`, so the refusal cannot be bought by breaking the
  legal spelling (Rule 31).

## Measurements (this run's own instruments)

**RED PROOF — the pre-change binary swallows the verb** (my base's `bin/gopi`,
sandbox profile `GOPI_HOME`/`GOPI_SETTINGS`, unreachable provider `127.0.0.1:9`,
`evidence-redproof.txt`):

```
cmd: gopi --db <scratch>/probe.db version
rc=1   stdout bytes=0   stderr bytes=3069
[gopi] session: …/sandbox-…/.gopi/sessions/8e517c2b-…/session.jsonl
[gopi] task ledger: registered 01a0dd95-3fbf-7177-97c5-2ed12d0c37be (in_progress, auto, …)
sessions before: 0 → after: 1      (new dir 8e517c2b-…)
```

A real run started for the word `version`: a session directory, a live `[auto]`
ledger row, an empty stdout and rc=1 — the defect reproduced exactly as reported.

**GREEN PROOF — the merged guard refuses, all five flags, zero sessions minted**
(peer binary built from `remotes/peerbox/master` `5710f683` by `git archive` +
`go build`, `evidence-peer-acceptance.txt`):

```
--db P version                     rc=1   sessions 3->3   err: …following the run flag --db…
--timeout 3 version                rc=1   sessions 3->3
--raw-log off version              rc=1   sessions 3->3
--model deepseek-v4 version        rc=1   sessions 3->3
--base-url U version               rc=1   sessions 3->3
version --db P (correct order)     rc=0   sessions 3->3   stdout: v0.1.0
sessions total after all: 3
```

Three independent instruments agree on "no run happened": the exit code, the
session-directory count (unchanged) and the ledger registration banner (absent).

**Honest note on the ledger instrument.** The pre-fix probe's `[auto]` row could
not be located after the fact (`01a0dd95-3fbf-7177-97c5-2ed12d0c37be` is in neither
`/home/user/.gopi/memory.db` nor the sandbox's) — the sandbox profile that run used predates
the fix that makes a sandbox's `memory.db` a real writable private file, and the
row's registration banner is quoted above from the run's own stderr instead. The
session directory count and `session_start` content walk are the instruments the
acceptance test relies on, and both are unambiguous.

## Manifest (DOC-INDEX.tsv) — a base-state defect, repaired under its own incident record

My base carried a DUPLICATE row for `devdoc/docindex-write-path-row-ownership.md`
(lines 10 and 762 — the documented MERGE UNION incident). `classify.py` refuses to
write while a duplicate exists, and `TestDocManifestCoversEveryDevdocFile` was
already red at base:

```
--- FAIL: TestDocManifestCoversEveryDevdocFile
    docclass_gate_test.go:762: duplicate row for devdoc/docindex-write-path-row-ownership.md
```

The repair is the one the tool documents ("the repair keeps the LAST"), and the
same repair is being landed by sibling task `01a0dd83fdf47920ae90e378db6f603a`
whose manifest carries a single row. My manifest diff is therefore exactly two
lines: that duplicate dropped, and my own document's row inserted at its sorted
position by the repo's generator (`classify.py --add-missing --only <path>`) — no
row was reclassified, no other row touched.

## Not Done (honest)

- **One verb word is out of the refusal's reach, and it is not a defect**:
  `gopi --timeout 3 task list` still DISPATCHES (`rc=0`, `No tasks found.` — measured
  on this branch's binary). The words `task` are found by `isTasksSubcommand`'s
  ssh-passthrough scanner, which searches the WHOLE argv for `task` and re-dispatches
  what follows; the verb therefore never reaches the guard as a leftover positional.
  The swallow this epic is about happens for every OTHER verb (`version`, `config`,
  `resume`, `stats`, …) — `task` was already safe by accident, and this row says so
  rather than counting it as a win.
- **The refusal reads `--db <VALUE>` as its example, not the operator's literal
  value.** Deliberate: the guard is handed the flag NAME so a path/api-key cannot
  be echoed. A caller wanting the literal value needs the value plumbed separately.
- **`bin/gopi` is built but not committed** (`.gitignore` excludes `bin/`); the
  fresh binary is the one the acceptance tests drive, and the freshness guard
  fails the test if it predates the tree.
- **The peer-merged fix was not rewritten.** This branch carries it (cherry-pick of
  `04a1262b`) rather than a second implementation of the same guard, so the branch
  is mergeable against a peer master that already has it.

## The scoped gate's red set is inherited (A/B, not assumption)

`make test-budget PKGS='./internal/config ./cmd/agent'` failed 15 tests. Every
one of them is red on UNMODIFIED master too — measured by running the same cases
in a detached worktree at the branch's own base (`79807932`,
`/home/user/workspace/gopi-ab-base-79807932`), with the method difference
controlled:

- `scripts/test-budget.sh:218` exports `GOPI_BIN=$root/bin/gopi` when the binary
  exists, so the CLI E2E cases RUN there. A bare `go test` without `GOPI_BIN`
  makes `resolveGopiTestBin` fall back to an in-scratch `go build`, which the
  loaded host's 26-suite contention turns into a build failure → `t.Skipf`. The
  first A/B pass therefore compared SKIP against FAIL; both sides were re-run
  with `GOPI_BIN` set to their OWN fresh binary, which is the honest comparison.

Identical verdicts, both sides, same command:

```
BASE 79807932 (GOPI_BIN=<base>/bin/gopi):
  --- FAIL: TestCLIPositionalSubmitRunsThroughDispatcherE2E (0.06s)
  --- FAIL: TestTaskRunChildReceivesSummaryAndPayloadE2E (0.07s)
  --- FAIL: TestTaskRunNormalExitSingleRowE2E (0.09s)
BRANCH 69e692a0 (GOPI_BIN=<branch>/bin/gopi):
  --- FAIL: TestCLIPositionalSubmitRunsThroughDispatcherE2E (0.06s)
  --- FAIL: TestTaskRunChildReceivesSummaryAndPayloadE2E (0.06s)
  --- FAIL: TestTaskRunNormalExitSingleRowE2E (0.07s)
```

The same A/B covered the other red families, all failing at base as well:
`TestCwdReleaseRig`, `TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves`,
`TestMergeHandoverFailureLeavesMainCheckoutMergeable`,
`TestMergeAfterHandoverFailureAcceptsTheRepo`,
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
`TestToolsMaintenanceHelpSurfaceFormsAreTyped` (the last two ran 17–57s under
`load average 30.88` on 8 cores).

**What this branch owns** is green in every run: `internal/config` (full),
`internal/standards` (full), `internal/docindex` (full), `cmd/agent` CLI contract
+ completion + guard suites. Neither branch's diff touches the cwd-release,
merge-handover or tools-help surfaces.
