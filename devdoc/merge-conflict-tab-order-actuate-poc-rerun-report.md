<!-- doc-class: historical -->
# Task Report: resolve merge conflict — rework feat/tab-order-actuate-poc (re-run, 2026-09-13 06:2x)

**Task**: merger branch-rework — `feat/tab-order-actuate-poc` refused by the CLI
merge-time contract gate (`rework: feat/tab-order-actuate-poc`, task `01a096c9`).
**Repo**: `/home/user/work/GoPi` · **branch**: `feat/tab-order-actuate-poc` · **target**: `master`
**Merge-base**: `1555b5c3` (branch 4 commits ahead, 978 behind master) — the branch touches **only** `devdoc/`.

## Completed

### 1. The refusal in this dispatch is STALE — measured, not argued

The briefing's detail text ("main.go no longer wires predicate(s) [ActSubcommand
MergerSubcommand ScratchGCSubcommand ScreenCaptureSubcommand VoiceSubcommand]")
was produced by the **pre-fix gate**. The gate was fixed on master *after* the
refusal log line that routed this row:

| event | time |
|---|---|
| merger pass refuses branch (tip-basis gate) | 02:0x |
| gate fix `daa930a8` merged to master (`449fda3a`) | 01:57 |
| merger runtime actually picked the fixed binary up | ~05:1x (restart) |
| this task row created (`01a096c9`) | 02:02:58 |

Evidence — merger log, passes 03:19 and 03:24 (last refusals, pre-restart):

```
[merger] [/home/user/work/GoPi] merged=0 contract-refused=1 protected=17
[merger] final state: … last pass: 2 repo(s) swept, 0 landing(s), idle since 2026-09-13T03:24:43
```

After the restart the branch is **not** refused again and **not** merged;
it silently joined the protected/worktree arm:

```
06:06:31  [/home/user/work/GoPi] merged=0 protected=16     ← no refusal, no landing
06:20:48  [/home/user/work/GoPi] branch feat/voice-capture-failmatrix (merged)
```

`grep tab-order ~/.gopi/logs/merger.log` has **zero** hits after line 6323
(03:24) — the current merger never re-refuses it.

### 2. Live probe of the PRODUCTION gate: `Refused=false` on every master ref

`internal/tools/merge_contract_gate.go` (`daa930a8`+`72222b47`, ancestor of
current master) run against the REAL repo:

```
PROBE master=master     Refused=false Detail=
PROBE master=b2e2407b   Refused=false Detail=
PROBE master=4b156bd2   Refused=false Detail=
PROBE master=1555b5c3   Refused=false Detail=
PROBE master=567b5d9f   Refused=false Detail=
```

So even against a pre-fix master ref the gate no longer refuses — because it
judges the **post-merge winner** blob, and the branch never touched
`cmd/agent/` (§3). The briefing's prescribed edit is therefore unnecessary
**and** harmful: `clicontract.Compare` counts a predicate covered iff
`targetWired[p] || (mirror ∪ mirror)`, and `targetWired[p]` is true for all five
on master — deleting the five mirror entries + inventory cases would be a no-op
on the (already empty) drift while deleting five valid master entries, i.e.
manufacturing the 22-vs-27 red-master shape the gate exists to prevent
(verified in the earlier run, `devdoc/merge-conflict-tab-order-actuate-poc-20260913-report.md`).
**Not executed.**

### 3. The real blocker: the branch was never DISCOVERABLE — worktree-protected

`repoSweepUnprotectedBranches` excludes every branch a worktree holds
(`protected`, "ACTIVE BOND WORK — never touched"). The branch was held by
**two** worktrees:

- `/home/user/workspace/gopi-taborder` (from the earlier run; 2689356f)
- `/home/user/workspace/gopi-taborder2` (created by this run for verification)

`bash scripts/safe-merge.sh feat/tab-order-actuate-poc` → ❌
`[c] 分支仍被 worktree 占用（未清理）`. This — not the gate — is what keeps
the branch off master on every subsequent tick. Fixed by removing both
worktrees (§5), leaving the branch itself intact.

The earlier run's real fix (`2689356f`, adopting master's `/tmp`-ban probe
versions to clear three add/add conflicts) is on the tip and verified still
effective: the merge is clean (§4).

### 4. Verification (all from this run)

Merge simulation on a detached master worktree (`/tmp/tabsim`):

```
$ git merge --no-ff --no-commit feat/tab-order-actuate-poc
Automatic merge went well; stopped before committing as requested
conflicts=0
main.go          SAME-AS-MASTER   (cmp against git show master:cmd/agent/main.go)
cli_contracts.go SAME-AS-MASTER
devdoc/tab-order-actuate-poc.md present
```

The **post-merge tree** (sim commit `8185fb34`) also passes the project's own
gates, which is the acceptance the branch's own checkout cannot express:

```
$ go build ./...                                       → exit 0 (post-merge)
$ go test ./cmd/agent/ -run TestCLIContract -count=1   → ok  github.com/nick/gopi/cmd/agent  11.447s
```

Branch checkout (`~/workspace/gopi-taborder2` @ `2689356f`), the briefing's own
acceptance commands:

```
$ go build ./...                                       → exit 0
$ go test ./cmd/agent/ -run TestCLIContract -count=1   → ok  github.com/nick/gopi/cmd/agent  5.650s
```

## Not Done

- **Briefing step 2 (edit `cliContractDispatchPredicates`)** deliberately not
  executed: the gate reports no drift on the post-merge tree, so there is no
  entry to add or remove; the prescribed removal is a proven no-op that would
  delete five valid master entries (§2).
- No write to `master`, no merge into the branch, no branch deletion
  (Rule 6d — deliverable preserved).
- Report committed on the branch itself (a docs commit) rather than master,
  per Rule 6a: workers do not write master.

## Files

- `devdoc/merge-conflict-tab-order-actuate-poc-rerun-report.md` (this file, committed on the branch)

## Verification summary

| check | result |
|---|---|
| live gate probe vs real repo (5 master refs) | `Refused=false`, all |
| merger log refusals after 03:24 restart | 0 (branch silently protected) |
| safe-merge.sh before cleanup | ❌ worktree-held |
| safe-merge.sh after cleanup | ✅ exit 0 |
| merge simulation (master ⨝ branch) | 0 conflicts |
| post-merge `go build ./...` | OK |
| post-merge `TestCLIContract` | ok 11.447s |
| branch `go build ./...` | exit 0 |
| branch `TestCLIContract` | ok 5.650s |
