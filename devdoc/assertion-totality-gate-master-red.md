<!-- doc-class: normative -->
# Task Report: TestAssertionTotalityAuditGate RED on untouched master

Task row: `01a0c3db-f42c-7284-adae-c270b20d16b8` (self-filed by a worker as
`01a0c337-d621`; re-issued on peerbox). Repo `/home/user/work/GoPi`, branch
`fix/assert-totality-gate-master-red`, worktree
`/home/user/workspace/gopi-assert-totality` (the task row's recorded ground).

## What was actually red

The briefing's list of "3 named tests unaccounted" was produced by a run whose
tail only showed the **data report** (the `classified <key>` lines are
`t.Logf`, printed for every already-classified site). The two real failures are
different, and both are in `internal/tools/assertion_totality_audit_test.go`:

| failing test | what it actually reported |
|---|---|
| `TestAssertionTotalityAuditGate` (`:118`) | **1 UNCLASSIFIED** live hit: `cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove` (`merge_run_test.go:902`) |
| `TestAssertionTotalityAuditGate` (`:141`) | **1 STALE key**: `internal/tools/task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped` — no such hit exists in the tree any more |
| `TestAssertionTotalityAuditGateClassifiesTheLiveTree` (`:245`) | its witness key *is* that same stale key, so the witness was not in the scan result |

So the tree was red in **both directions of the same ratchet** at once: one new
unclassified site, and one permit that had outlived its assertion. The three
names the briefing listed (`verify_baseline_delta`, `wip_worktree_repro` ×2)
already carry reviewed rows and were green before the change — they are
classified in this report because the briefing asked, not because they failed.

## Per-test classification (all three named tests + the three real sites)

### 1. `internal/tools/verify_baseline_delta_test.go:TestMergeVerifyBaselineFailuresEmptyOnAGreenRun`

**RETIRED 2026-10-30, with the parser it judged.** This section is a
`report of a past run, and both of its subjects are gone: slice 2/4 of epic
`01a18799` retired the `verify` verb, the `MergeVerifyBaselineFailures` parser
had no production consumer left (its only caller was `TaskVerify`), so the
parser, `MergeVerifyFormatBaseline`, `MergeVerifyBaselineRunEnv` and
`MergeVerifyBaselineRunTimeout` were deleted together with
`internal/tools/verify_baseline_delta_test.go`, and the row this section
defends left `internal/tools/assertion_totality_allow_test.go` in the same
change (a stale key is an error in that gate). The classification below is
kept verbatim as the record of what was decided on the day, because it is why
the row was a legitimate exception rather than a gap.

**Verdict (as recorded then): legitimate reviewed exception — row already
present, row kept, reason re-read against the code.** Not a gap, not a false
positive.

The row reads *"a pure function over a captured green test log: 'a green run
must yield no failing packages' … there is no step to run, and the same
assertion pins final==true."* Re-read against the source (`merge_run_test.go`
-adjacent production: `MergeVerifyBaselineFailures`, `internal/tools/verify_gate.go:1077`):
the assertion is `if len(got) != 0 || !final`. `got` is a **package-name list
parsed out of a test log** — no step set exists to contain an idempotent
member, which is the one thing this gate exists to detect. The `!final` half of
the same line pins the completeness direction (`verify_gate.go:1100`: a run
with per-test failures but no package verdicts returns `nil, false`). Nothing to
fix; the row is accurate.

### 2/3. `internal/tools/wip_worktree_repro_test.go:TestMergedCleanupProgressedWorktreeStillReported` and `:TestMergedCleanupWIPWithCommitIsProtected`

**Verdict: legitimate reviewed exceptions — rows kept.** Both assertions are
negations of `rep.Removed` in the **report-only reaper** (epic
sweep-report-not-act, 2026-09-19): production's only setting is
`ReportOnly: true` (the `serveFinishCleanupWorktreeQuery` region of `cmd/agent/cli_tasks.go`, `:3053`), so
`Removed` is empty **by construction in production**, and
`task_merged_cleanup.go:562` appends `Reported` and returns before any removal.
The first test also pins the positive half in the same function
(`len(rep.Reported) == 1`, line 109); the second guards a data-loss path (a
worktree whose own commit is not in master).

**One repair made here (Rule 7 honesty, no verdict change):** the second row's
cited evidence, `TestMergedCleanupProgressedWorktreeStillRemoved (:106)`, **no
longer exists** — the same epic renamed it to `…StillReported`. A permit whose
cited evidence is a dead test name is the exact failure this gate's stale-key
half exists to prevent, at one remove. The row now cites
`TestMergedCleanupOrphanWorktreeStillReported` (`task_merged_cleanup_test.go:647`)
and records that the old name is dead.

### The three sites the gate actually reported

**A. `cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove` — UNCLASSIFIED → correct, half (b). Row ADDED.**

*(Re-measured 2026-11-01, epic merge-verb removal item 3: `cmd/agent/merge_run_test.go`
and `cmd/agent/merge_run.go` were DELETED with the top-level merge verb, so every
anchor in this section — `:939`, `:943`, `:947`, `merge_run_test.go:845` — names a
file this tree no longer has (`ls cmd/agent/merge_run_test.go` → No such file). The
audit row itself survives in `internal/tools/assertion_totality_allow_test.go:156`,
which is what the gate reads; what is gone is the code it vouched for. The section
below is therefore the record of the audit as performed on 2026-08-30, not a
description of live code.)*

The claim is *"a run that moved nothing minted %v"* over
`len(mergeVersionTagsAt(t, repo, integrationBranchForTest)) != 0`. The guard is
`mintAdvancedPositionAtRunClose` (`cmd/agent/merge_run.go:939`), and its two
pre-mint returns are the whole claim:

- `:943` — unreadable HEAD, **or HEAD == `snap.PreTip`** → *"a repo this run
  did not move"* → return;
- `:947` — `gopiversion.TagAt(repo) != ""` (the per-landing mint, or an
  operator's release tag, already named it) → return.

The test drives exactly that case: the branch conflicts and the child **never
lands** (`mergeAgentRunFn` is a no-op seam), master stands still. On an unmoved
repo there is nothing to name, so the set has **no idempotent member a guard
could wrongly exclude** — half (b) of the gate's own question is satisfied
because the member does not exist. A tag here would be the defect the test
exists to catch (a name pointing at a position nobody moved). Its positive
counterpart lives in the same file: `TestMergeNamesAPositionTheChildLanded`
(`merge_run_test.go:845`) pins `len(tags) == 1` after a real child landing. Row
written with the guard's two returns, the fixture's shape, and the carrier.

**B. `internal/tools/task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped` — STALE KEY → key DELETED, history documented.**

This is a **rewrite**, not a removal, and it is a genuine *fix* of the bundled
claim:

- `95650429` had `if len(rep.Steps) != 0 && !onlyUntrackedResidueNotes(rep.Steps)`
  with the message *"the server recycle has no other act than the removal, so
  none may run on a dirty worktree"* — a real negative totality, correctly keyed;
- `6b1d753c` ("feat(align): make integration-branch alignment a precondition of
  delivery") hoisted the **alignment step ahead of the dirty gate**
  (`task_finish_cleanup.go`: *"ALIGNMENT IS A PRECONDITION OF THE DELIVERY,
  ASKED HERE — BEFORE THE DIRTY GATE BELOW"* — that comment was rewritten
  2026-10-12 when the alignment was replaced by the branch's own test run; see
  `devdoc/client-trial-merge-removal.md`). A dirty worktree now
  legitimately records an align step, so the *old* condition would fire on a
  **correct** tree. The assertion became
  `if len(rep.Steps) == 0 || !strings.Contains(rep.Steps[len(rep.Steps)-1].Detail, finishCleanupUntrackedResidueNote)`
  — a **positive** claim that the last recorded step is the removal's refusal note.

It is no longer a negation of a count, so the predicate stops firing and the
key is owed by nobody. **The predicate is not widened to reach it**: the
comment at `auditCondIsNegatedCount` explains why `len(x) != 0` is the hazard
and `== 0` is the wrong polarity; reaching `len(x) == 0 || …` would re-hunt
every positive-emptiness assertion in the tree — a weakening, not a fix. The
key is **deleted** (the gate's own words: *"a stale permit silently re-arms the
next agent"*) and the reviewed history moved into the allow file's documented
`rows NOT in the gate's hit list` section, which is this file's existing
precedent for exactly this situation.

**C. `cmd/agent/merge_run_test.go` witness re-point — the third failure, and the reason the fix is not just an allow-row.**

`TestAssertionTotalityAuditGateClassifiesTheLiveTree` required witness key `B`
above. That key can no longer be a witness, so the check is re-pointed to **two
live keys**: `internal/tools/task_finish_cleanup_test.go:TestFinishCleanupServerRecycleSkipsWorktreeInUse`
(the surviving carrier of the incident's shape *at the same removal site*:
`finishCleanupServerRecycle`, which takes no `--force`; with a live CWD holder
the arm returns at the liveness gate `:1267`, **before** the align step `:1343`,
so genuinely no step runs) **and** the newly-classified
`cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove`
(a different family, so the witness measures the predicate and not one file).
Re-pointing, never dropping, is this check's own recorded precedent.

## Why the briefing's "three named tests" and the gate's report differ

The run that produced the briefing printed `t.Logf` data lines for **every**
classified site, then the two `t.Errorf` findings. The briefing's three names
are data-report lines, not failures. Recorded here because the mismatch is the
kind of thing that sends the next agent to the wrong file: **the gate's findings
were one unclassified site and one stale key — nothing else was red.**

## Files changed

- `internal/tools/assertion_totality_allow_test.go` — added row A; deleted stale
  key B (history moved to the not-a-hit section); re-pointed the dead citation
  in site 2/3's row.
- `internal/tools/assertion_totality_audit_test.go` — witness set is one key →
  two live keys, with the reason the third had to move.
- `devdoc/assertion-totality-gate-master-red.md` — this report.

No production code, no test deleted, no predicate loosened, no inline escape.

### Three dead citations repaired while verifying the rows

Verifying the row I was adding turned up the same defect at one remove — a
citation that no longer resolves. Each was checked against the live file and
corrected, and every other `file:line` anchor in both changed files was then
re-checked programmatically (walks the tree, resolves the path, compares the
line number to the file's length): **no out-of-range anchor remains.**

- `cmd/agent/cli_tasks.go` `:3660` → **`:3660`** and `:3286` → **`:3286`** — the
  two `ReportOnly: true` production call sites. They had drifted by exactly
  +74 lines (one insertion, both anchors), and **five** rows cited the old
  pair. Live: the sweep function itself is unchanged in shape.
- `TestMergedCleanupProgressedWorktreeStillRemoved (:106)` → 
  `TestMergedCleanupOrphanWorktreeStillReported (task_merged_cleanup_test.go:649)`
  — the cited test no longer exists (the sweep-report epic renamed it); the row
  now says the old name is dead.
- `TestFinishCleanupServerRecycleSkipsWorktreeInUse (task_finish_cleanup_test.go:1305)`
  → **`:1363`**; and the mergeset row's bare `(:421)` → 
  `rebuild_vet_mergeset_test.go:427` (the `len(refs) != 1 || refs[0] != wantRef`
  assertion).

## Not Done

- The predicate was **not** widened to catch the rewritten shape
  `len(x) == 0 || !Contains(…)`. Recorded above as a deliberate rejection, not
  an oversight: it would re-hunt positive-emptiness assertions tree-wide.
- Promotion of the gate from `internal/tools` to `internal/standards` remains a
  follow-up (the file's own header says so — Rule 28: `internal/standards` is
  the system layer, this epic ran on a feature host).

## Verification

- Build: `go build ./...` → exit 0, no output.
- Vet: `go vet ./...` → exit 0, no output.
- Tests: `go test ./internal/tools/ -run 'AssertionTotality' -count=1` →
  **before**: `FAIL github.com/nick/gopi/internal/tools 1.463s` (2 findings);
  **after**: `ok github.com/nick/gopi/internal/tools 4.875s`.
- Formatted: `gofmt -l internal/tools/assertion_totality_allow_test.go internal/tools/assertion_totality_audit_test.go` → no output. (7 unrelated pre-existing
  offenders on master, untouched.)
- Smoke, branch-scoped: `make test-budget PKGS='./internal/tools'` (see Result).

### Ran:

```
$ cd /home/user/workspace/gopi-assert-totality
$ go test ./internal/tools/ -run 'AssertionTotality' -count=1 ; echo "exit=$?"
```

### Result (before — the failing run, raw):

```
exit=1
--- FAIL: TestAssertionTotalityAuditGate (0.44s)
    assertion_totality_audit_test.go:118: cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove (cmd/agent/merge_run_test.go:902): negative-totality assertion "a run that moved nothing minted %v\n--- output ---\n%s" is UNCLASSIFIED.
          - is the bundled claim right, or does the set contain an idempotent step that must still run (the 2026-09-16 push/dirty defect)?
          - then add: auditTotalityAllow["cmd/agent/merge_run_test.go:TestMergeCloseOutNamesNothingWhenThePositionDidNotMove"] = "<correct | over-broad: which half> — <evidence>"
    assertion_totality_audit_test.go:141: auditTotalityAllow carries 1 STALE key(s) — no such assertion is in the tree any more.
          The claim was fixed, renamed or removed: re-read the reason and DELETE the entry (a stale permit silently re-arms the next agent).
          internal/tools/task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped
--- FAIL: TestAssertionTotalityAuditGateClassifiesTheLiveTree (0.67s)
    assertion_totality_audit_test.go:245: the incident's own assertion (internal/tools/task_finish_cleanup_test.go:TestServerRecycleDirtyWorktreeSkipped) is not in the scan result — the predicate no longer sees the shape it was written for.
FAIL
FAIL	github.com/nick/gopi/internal/tools	1.463s
FAIL
```

### Result (after — the same command, green, raw):

```
exit=0
ok  	github.com/nick/gopi/internal/tools	4.875s
```

### Ran: the predicate still fails on a fresh violation (mutation probe)

The red proof's synthetic cases pass, but a proof can pass for the wrong reason.
So the gate was **run against a live injection** of the incident's shape, removed
again, in one branch state:

```
$ cat > internal/tools/zz_mutation_probe_test.go <<'EOF'
package tools
import "testing"
func TestMutationProbeUnclassifiedShape(t *testing.T) {
	var steps []string
	if len(steps) != 0 {
		t.Errorf("no step may run on a dirty worktree: %+v", steps)
	}
}
EOF
$ go test ./internal/tools/ -run 'TestAssertionTotalityAuditGate' -count=1
```

Result — RED as required:

```
--- FAIL: TestAssertionTotalityAuditGate (1.94s)
    assertion_totality_audit_test.go:118: internal/tools/zz_mutation_probe_test.go:TestMutationProbeUnclassifiedShape (internal/tools/zz_mutation_probe_test.go:7): negative-totality assertion "no step may run on a dirty worktree: %+v" is UNCLASSIFIED.
```

After deleting the probe:

```
$ rm internal/tools/zz_mutation_probe_test.go
$ go test ./internal/tools/ -run 'AssertionTotality' -count=1
ok  	github.com/nick/gopi/internal/tools	4.525s
```

### Ran: the red proof's own cases

```
$ go test ./internal/tools/ -run 'TestAssertionTotalityAuditGateRedProof' -v -count=1
--- PASS: TestAssertionTotalityAuditGateRedProof (0.00s)
```

Its seven sub-cases (`-v` list) include the incident's shape → 1 hit, the
positive-polarity `!= 1` → 0, a negated count whose message claims nothing → 0,
and the inline escape → 0. The predicate's red half is therefore intact **and**
`TestAssertionTotalityAuditGateStaleKeyRedProof` still pins the staleness half
(a live key is fine, a key with no site is stale, one live + one stale reports
exactly the stale one).

## Evidence (self-filed)

- `go test ./internal/tools/ -run 'AssertionTotality' -count=1` before →
  `exit=1`, `FAIL github.com/nick/gopi/internal/tools 1.463s`, two findings
  quoted verbatim above (unclassified key A, stale key B); after → `exit=0`,
  `ok … 4.875s`.
- Mutation probe: `go test … -run 'TestAssertionTotalityAuditGate'` with
  `zz_mutation_probe_test.go` in the tree → `--- FAIL: TestAssertionTotalityAuditGate`,
  naming the probe's key; after `rm`, same command → `ok`.
- `go build ./...` → exit 0; `go vet ./...` → exit 0.
- `gofmt -l` on the two changed files → empty list.
- `git status --porcelain` in the worktree at close-out → empty (below).

## Commit

`fix(gate): classify the close-out mint site and retire the dead permit` —
`internal/tools/assertion_totality_allow_test.go`,
`internal/tools/assertion_totality_audit_test.go`.
