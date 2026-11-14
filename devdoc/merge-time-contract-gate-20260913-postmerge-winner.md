<!-- doc-class: historical -->
# Merge-Time CLI Contract Gate — Post-Merge Winner Basis (2026-09-13)

**Trigger**: branch `feat/tab-order-actuate-poc` (a docs-only POC) was REFUSED
at merge time by the CLI contract gate with:

```
CLI contract drift — main.go no longer wires dispatch predicate(s)
[ActSubcommand ScratchGCSubcommand ScreenCaptureSubcommand VoiceSubcommand]
that cmd/agent/cli_contracts_test.go still tracks
```

**Branches**: gate fix `fix/contract-gate-postmerge-winner` (worktree
`~/workspace/gopi-contractgatefix`); branch rework `feat/tab-order-actuate-poc`
(worktree `~/workspace/gopi-taborder`).

---

## 1. The refusal was a FALSE POSITIVE, and the prescribed fix was a no-op

### What the branch actually is

`feat/tab-order-actuate-poc` delivers a Tab-order vs a11y-actuation POC:
`devdoc/tab-order-actuate-poc.md` plus `devdoc/tab-order-poc/*` (8 probes +
60 shots). Its history is **three docs-only commits** on top of merge-base
`1555b5c3`; master has moved **831 commits** since that fork.

Measured delta of the branch against its own merge-base:

```
git diff --stat 1555b5c3 feat/tab-order-actuate-poc -- cmd/agent/   → (empty)
```

**The branch changed nothing under `cmd/agent/`** — not `main.go`, not
`cli_contracts_test.go`. Both contract files are at the fork point.

### Why the gate refused anyway

`mergeContractGate` compared the branch **TIP**'s blobs against master's tip.
Measured with the project's own parser (`internal/clicontract`):

```
BRANCH (tip)  chain=22 mirror=22 tokens=21
MASTER (tip)  chain=27 mirror=27 tokens=26
DRIFT WiredUntracked=[] TrackedUnwired=[ActSubcommand MergerSubcommand
      ScratchGCSubcommand ScreenCaptureSubcommand VoiceSubcommand]
```

Master grew five dispatch predicates (Act / Merger / ScratchGC /
ScreenCapture / Voice) *after* the fork. Against the tip's stale fork copy
they read as `TrackedUnwired` — even though **a three-way merge takes MASTER's
files** (the branch side is unchanged from the base), so the merged tree is
perfectly consistent. Verified directly:

```
$ git -C /tmp/gopi-mergesim merge --no-commit --no-ff feat/tab-order-actuate-poc
Automatic merge went well
$ cmp <(git show master:cmd/agent/main.go) cmd/agent/main.go            → SAME
$ cmp <(git show master:cmd/agent/cli_contracts_test.go) …cli_contracts_test.go → SAME
```

### The briefing's prescribed fix is a NO-OP (proven, not argued)

The refusal text instructs: *"remove them from cliContractDispatchPredicates
(and their inventory cases) in this same branch."* Deleting those five entries
from the branch mirror changes the verdict **not at all**. Proven by replaying
the real `clicontract.Compare` on both variants:

```
DRIFT (as-is)          TrackedUnwired=[ActSubcommand MergerSubcommand ScratchGCSubcommand ScreenCaptureSubcommand VoiceSubcommand]
DRIFT (after deletion) TrackedUnwired=[ActSubcommand MergerSubcommand ScratchGCSubcommand ScreenCaptureSubcommand VoiceSubcommand]
→ prescribed fix is a NO-OP on TrackedUnwired
```

**Why**: `Compare`'s covering set is `mirror(tip) ∪ mirror(target)`. Master's
mirror still tracks those five predicates, so the union covers them and
`TrackedUnwired` is unchanged. Worse, the edit would have **deleted five valid
entries from master's own mirror** (the branch's test file wins per-file for
any file the branch changed) and left `TestCLIContractInventoryMatchesDispatch`
RED on master at a count of 22 vs 27 — the gate would have *caused* the exact
incident it exists to prevent.

This is a concrete case of the Rule-10 habit: **root cause before params** —
the prescribed edit was a parameter tweak on a comparison basis that was
itself wrong, and it could never have worked.

## 2. Second, independent blocker: three REAL merge conflicts

The branch never had a chance to be judged on the contract anyway — the merge
itself failed. `devdoc/tab-order-poc/{channel_bench.py,popup_probe.sh,tab_rate_bench.py}`
exist on **both** sides in different versions (add/add):

| file | master (6581b2d1) | branch (fork) |
|---|---|---|
| `channel_bench.py` | migrated off `/tmp` via `scripts/lib/tmp_ban.py` | hardcoded `/tmp/tabpoc/...` |
| `popup_probe.sh` | `source ../../scripts/lib/tmp-ban.sh` | `source /tmp/tabpoc/env.sh` |
| `tab_rate_bench.py` | migrated off `/tmp` | hardcoded `/tmp/...` |

Master's versions are strictly NEWER (the scripts `/tmp`-ban migration) and the
branch's are the pre-migration copies, so adopting master's versions clears the
conflict with **zero deliverable loss**.

## 3. Fix A — unblock the branch (`feat/tab-order-actuate-poc`, `2689356f`)

One surgical commit adopting master's newer versions of the three probe files,
per Rule 8 (`git add <file>` one by one, never `-A`). Post-fix merge
simulation from a clean master checkout:

```
MERGE_CONFLICTS=[]
SAME-AS-MASTER cmd/agent/main.go
SAME-AS-MASTER cmd/agent/cli_contracts_test.go
devdoc/tab-order-actuate-poc.md        → present
devdoc/tab-order-poc/                  → 9 files, all 8 probes + shots
```

No code file is touched; the branch stays docs-only. Master was never merged
into the branch (that would have hidden the fork relationship the gate keys on).

## 4. Fix B — the gate judges the post-merge winner tree

`internal/tools/merge_contract_gate.go` now resolves, **per contract file**, the
blob a three-way merge would KEEP — git's own rule:

| branch side vs base | winner |
|---|---|
| unchanged (`branch == base`) | **target (master)** — always the real post-merge content |
| changed, target unchanged (`master == base`) | branch |
| changed, same content both sides (`master == branch`) | either (identical) |
| **changed on BOTH sides** | none — a real conflict; **ABSTAIN** |

The pair handed to `Compare` is therefore the post-merge tree, and the
comparison becomes tip-vs-tip-comparable (winner vs target), which is what the
original `Compare` semantics already assumed. Master is still untouched, the
refusal still happens before any `git merge`, and `Compare` itself is unchanged
(its union rule is still the right conservative choice inside a single tree).

**Abstention on both-sides-rewrite** is deliberate: the merged content of such a
file is decided by whoever resolves the conflict, so no blob-derived pair
predicts it. The gate's design contract is DETECT-never-block-on-ambiguity, and
the conflict lane owns that file.

## 5. New cost budget (measured + pinned)

The winner rule needs the base revision and the winner tree:

| | before | after |
|---|---|---|
| git subprocesses | 4 (`git show`, 2 files × 2 revisions) | **2** — ONE `git cat-file --batch` carrying all 6 blobs (2 files × 3 revisions) + ONE `git merge-base` |
| checkout / build / test process / LLM | none | none |

**Measured attribution** (python `perf_counter`, n=12, warm, real repo
`/home/user/work/GoPi`, branch 831 commits behind master):

| component | median |
|---|---|
| one `git show <rev>:<small blob>` | 5.6 ms |
| one `git cat-file --batch` carrying all 6 blobs | 4.2 ms |
| one `git merge-base` on a widely divergent pair | **21.5 ms** |
| whole gate, tip basis (4 `show`) | 24.2 ms/branch |
| whole gate, winner rule, one `show` per blob (6 `show` + 1 merge-base) | 54.3 ms/branch |
| whole gate, winner rule, batched (1 batch + 1 merge-base) | 45.3 ms/branch |

Two honest conclusions, both different from an earlier draft of this file:

1. **The batch is what keeps the correctness fix from tripling the gate.** Six
   blob reads cost ~34 ms as six `git show` processes and ~4 ms as one batch.
2. **The irreducible new cost is the `git merge-base`** (21.5 ms here, because
   the pair is 831 commits divergent) — that single call IS the price of judging
   the post-merge tree, and it cannot ride the batch pipe (a different
   subcommand). So the winner basis is ~26 ms/branch, i.e. *comparable to* the
   ~24 ms tip basis, not dramatically cheaper. Earlier figures in this file
   (337 ms / 124 ms / 100 ms) were taken on a loaded machine and do NOT
   reproduce; the durable statement is the SUBPROCESS COUNT (2), which the unit
   test pins, not a wall-clock claim.

Counts are named constants (`mergeContractGateBatchCalls`,
`mergeContractGateBaseRefCalls`, `mergeContractGateSubprocessBudget`,
`mergeContractGateFilesPerRevision`, `mergeContractGateRevisions`) and pinned by
`TestMergeContractGate_CostIsBounded` via a PATH shim recording every `git`
argv — a regression that spawns a subprocess per blob, checks out a tree, or
runs `go test` blows the budget.

## 6. Verification

See the task report `devdoc/merge-conflict-tab-order-actuate-poc-20260913-report.md`
for the full command output. Summary:

- `go build ./...` — OK (both worktrees)
- `go test ./internal/tools/ -run 'TestMergeContractGate|TestMergerSweep_ContractRefusal' -count=1` — all PASS
- `go test ./internal/clicontract/ -count=1` — all PASS
- `go test ./cmd/agent/ -run TestCLIContract -count=1` — PASS (the branch's own §3 requirement)
- New regression repro: `merge_contract_gate_winner_test.go`
  - `TestMergeContractGate_StaleBranchOnUntouchedContractMerges` — **fails on the
    pre-fix gate** (refused) and passes after; it also asserts the tip-vs-tip
    drift shape still parses as `[VoiceSubcommand]`, so the false positive's
    mechanism stays pinned.
  - `TestMergeContractGate_BothSidesRewroteContractAbstains` — pins the
    conflict-rule abstention, with a control proving the one-sided bd3d34a5
    shape is still refused.

## 7. Files touched

| File | Change |
|---|---|
| `internal/tools/merge_contract_gate.go` | winner rule; `mergeContractGateFileWinner`; pair reader takes a reader func; package doc + constant comments |
| `internal/tools/merge_contract_gate_test.go` | cost test rewritten to pin the batched budget: exactly 1 `git cat-file --batch` + 1 `git merge-base` (a per-blob fallback now fails) |
| `internal/tools/merge_contract_gate_winner_test.go` | **new** — regression repro + abstention control |
| `devdoc/merge-time-contract-gate-20260912.md` | cost section annotated as superseded (the 4-call/0-other figures) |

## 8. Not done / honest limits

- The gate still detects exactly ONE failure mode (chain vs mirror drift in the
  post-merge tree). It does not compile, does not run tests, does not validate
  inventory fingerprints or `cliVerbs` coverage.
- The both-sides-rewrite abstention means a real conflict on the contract files
  is NOT gate-checked; the conflict lane resolves it and the next tick's merge
  is not re-checked by the gate (the gate runs pre-merge, and a conflict
  resolution commit is authored by the resolver). A follow-up could re-run the
  gate after a conflict resolution; out of scope here.
- `Compare`'s union covering rule remains a deliberate false-negative bias
  (documented in the original devdoc); this change does not alter it.
