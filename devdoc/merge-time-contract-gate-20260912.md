<!-- doc-class: normative -->
# Merge-Time CLI Contract Gate (2026-09-12)

**Epic**: make master-red from a new CLI subcommand structurally impossible at
**merge** time.
**Branch**: `feat/merge-time-contract-gate` (worktree `~/workspace/gopi-mergegate`).

## The incident this closes

`bd3d34a5` (`feat(voice): gopi voice_input`) landed on master on 2026-09-12. It
added `|| isVoiceSubcommand(dispatchArgs)` to the `cmd/agent/main.go` dispatch
chain and shipped `cmd/agent/cli_voice.go` + `internal/toolnames/names.go` — but
it never touched `cmd/agent/cli_contracts_test.go`. The contract suite's mirror
`cliContractDispatchPredicates` therefore tracked **25** predicates while main.go
wired **26**, so `TestCLIContractInventoryMatchesDispatch` went red **on master**:

```
dispatch predicate count mismatch: main.go has 26 ([… VoiceSubcommand]),
contract suite tracks 25 — add/remove the case and update cliContractDispatchPredicates
```

The drift guard already existed and already worked. It simply never ran on the
path that put the commit on master: the drift guard lives in `go test`, and the
merger (`MergerRepoSweep` → `TaskMerge` → `taskMergeOne`) never runs `go test`.
A guard that fires only where nobody looks is not a guard.

## Design decision: (a) merger-side narrow check vs (b) derived inventory

Two candidate gates were on the table:

- **(b) derived inventory** — run the real `TestCLIContractInventoryMatchesDispatch`
  (or a merger-side re-implementation of the full contract suite) against the
  branch before merging.
- **(a) merger-side narrow check** — read the branch tip's `cmd/agent/main.go` dispatch
  chain and its `cli_contracts_test.go` mirror as *text*, compare the two
  predicate sets, refuse on disagreement.

**(a) was chosen.** Reasons, in order of weight:

1. **Cost.** The merger runs every tick, on every configured repo, for every
   unprotected branch. Measured (see below): the narrow check is **~13.5 ms** for
   a fully-loaded decision — four `git show` calls, no checkout, no build, no
   test process, no LLM. Running `go test` per branch would mean a Go build
   (~10–40 s warm, minutes cold) per candidate **per tick**. That cost is not
   affordable on a polling daemon, and paying it is how a gate gets removed.
2. **Determinism.** (a) is a pure function of two source files. (b) inherits the
   whole test binary's flakiness (network-dependent tests, timing tests, a test
   cache that can be poisoned by the branch's own code).
3. **Blast radius.** (a) cannot execute one line of the branch's code — it reads
   git objects only. (b) executes the branch's test binary inside the merger
   process context.
4. **The merger's design contract.** `devdoc/merger-single-owner-architecture-20260905.md`
   / `merger-self-resolve-20260905.md`: the merger **detects**, semantic
   judgement belongs to the executor. (a) detects a mechanical, syntactic
   disagreement and hands the judgement to a rework agent. (b) is the merger
   making a build/test judgement — exactly the scope creep the architecture
   forbids.

The price of (a) is narrower coverage, stated honestly below.

## What it catches — and what it does not

**Catches (the incident's shape, all three halves):**

| Shipped | Why it was red on master |
|---|---|
| a new `isXxxSubcommand` predicate in the main.go chain, mirror not updated | `bd3d34a5` — count mismatch, 26 vs 25 |
| a predicate mirrored but with NO `dispatcherOfToken` entry | `dispatch predicates without a contract case` — the half-fixed branch the live demo caught |
| a predicate removed from the chain while the mirror still tracks it | the same count mismatch, other direction |

**Does NOT catch — deliberately, stated plainly:**

- **Compile errors.** The gate does not build. A branch that lands syntax errors
  still lands them (measured during the demo: a branch carrying only *part* of
  the voice feature merged and master then failed to compile — the gate said
  nothing, because the dispatch contract was consistent).
- **Any other red test.** A branch that breaks `TestFoo` unrelated to the
  dispatch contract is merged exactly as before.
- **Inventory-fingerprint drift.** `cliContractInventory` usage strings are not
  compared — the gate compares predicate *names* only.
- **`cliVerbs` coverage** (`TestCLIUserVerbTokensAreAllFromCliVerbs`) — untouched.
- **Order-only drift** in the mirror. The comparison is **set-based**: reordering
  `cliContractDispatchPredicates` without changing its membership merges. The
  test-time guard owns order; refusing a merge on a reorder alone would be a
  false positive the executor cannot act on.
- **A branch whose test-file edit is masked by master's own mirror.** Coverage is
  the **union** of both sides' mirrors (see "Union rule" below) — a deliberate
  false-negative bias.

The honest summary: this gate closes **the failure mode that produced the
incident**, and it is a ratchet — it does not close "land red tests at merge
time" in general. Closing that generally means running the project's declared
build/test in the merge path, which is a separate, much larger decision (and the
`repos.<path>.build` contract in Rule 12 is where such a thing would live).

## Where it is wired

`taskMergeOne` (`internal/tools/task_merge.go`) — the merger's per-branch
decision tree — gained a **step ⓪** before the existing ① ancestry check:

```
⓪ mergeContractGate(repo, master, branch)  → contract-refused?  → return, master untouched
① merge-base --is-ancestor  → prune if already merged
② git merge
③a landed → branch -d
③b conflict → merge --abort → dispatch
```

Step ⓪ runs **before `git merge` ever executes**, so a refusal is master-safe **by
construction**, not by cleanup: there is no merge state to abort, no `MERGE_HEAD`,
no index entry, no reflog entry. The refusal test asserts exactly this.

The refusal returns `Action: "contract-refused"` — a distinct action so the sweep
report separates "refused for contract drift" from "conflict" (they need
different fixes: a code edit vs a merge resolution). `TaskMergeCounts` gained
`ContractRefused`.

### Routing into the existing failure path

A refusal is not a conflict — the branch merges fine and fails a contract check.
Sending it to the conflict resolver would spend a 15-minute child on the wrong
problem. So the refusal routes to the **existing rework lane**: right after the
merge pass, `repoSweepRouteContractRefusals` (`task_merge.go`, called from
`MergerRepoSweep`) submits **one** branch-rework task for the refused branch,
per tick, reusing the established machinery verbatim:

- the **same deterministic summary** as the resolve-failure rework lane
  (`resolveRowReworkSummaryPrefix + branch`) → one branch has ONE rework row
  however it was refused, and
- the **same dedup query** → a second tick finds the live row and reports
  `duplicate` instead of minting a pile, and
- the **same tag set** (`resolveRowReworkTags` = `merge-conflict,branch-rework`) so
  `resolveRowHasOpenRework` holds the gate while a reworker owns the branch.

The rework briefing (`contractGateRefuseFmt`) is the gate's own diagnosis, not
the conflict briefing: it quotes the measured drift, names
`cmd/agent/cli_contracts_test.go` as the file to fix, tells the reworker to add
the `cliContractDispatchPredicates` entry **in chain order** plus the matching
`cliContractInventory` case, gives the verification commands, and explicitly
forbids the two wrong reactions (merge master in / delete the branch).

Fail-soft everywhere: a submit error or a ledger error reports `error` in the
outcome and leaves the branch a candidate. Master is untouched regardless.

## The comparison rule (and why it is a conjunction)

`clicontract.Compare(tipMain, tipMirror, targetMain, targetMirror, tipTokens)`
reports what the **branch introduces**, not absolute drift:

- `WiredUntracked = (wired(tip) − wired(target)) − { p : (mirror(tip) ∪
  mirror(target)) tracks p AND an inventory token routes to p }`
- `TrackedUnwired = (wired(target) − wired(tip)) ∩ (mirror(tip) ∪ mirror(target))`

A predicate counts as tracked only when **both** halves hold — the mirror entry
(the count match) and a `dispatcherOfToken` entry (the contract-case match).
This conjunction is not a nicety: the live demonstration on the real incident
caught the gate merging a **half-fixed** branch, one that updated
`cliContractDispatchPredicates` but never added the token-map entry. The
project's guard resolves the mirror through `dispatcherOfToken` to find each
predicate's CASE, so that branch is *still* red on master
(`dispatch predicates without a contract case: [VoiceSubcommand]`) — measured,
not assumed, on the real tree.

Two deliberate properties, both tested:

1. **Branch-introduced only.** A branch that merely carries master's own
   pre-existing drift (a rebase) is **not** refused. Without this, one bad master
   would refuse every branch in the queue — a self-inflicted outage.
2. **Union mirror coverage.** Either side's `cli_contracts_test.go` can be the
   file that survives the merge, so a predicate is considered tracked if
   **either** side tracks it. This over-approximates coverage, which can only
   *suppress* a refusal (a false negative), never manufacture one. On a merge
   path that is the correct bias: a missed catch costs one red test, a spurious
   refusal blocks a repo.

## Abstention vs fail-closed (and the footgun that proved it)

The gate **abstains** (merge proceeds, logged) when:

- either file is unreadable at a revision (a docs-only/foreign repo sharing the
  sweep, or a moved file),
- the contract suite no longer **declares** the `dispatcherOfToken` anchor — a
  project whose contract suite moved on from the shape this gate understands,
- the chain cannot be located, shrinks below 10 predicates, carries a
  non-predicate condition, or **mixes argument names** — all shape changes,
- the operator sets `GOPI_MERGE_CONTRACT_GATE=0` (the ONE reader of that
  constant, Rule 2).

Abstention is logged through `taskNotifyLog` so it is visible rather than silent.

It **fails closed** (no token coverage → more willing to refuse) only when the
anchor is present but its literal cannot be parsed: that is a project that
edited the map, not one that never had it.

**This distinction is not hypothetical — two live demonstrations produced it.**

The first live run merged a drift branch that should have been refused, because
the parser keyed on the literal `(dispatchArgs)` and the synthetic repo used
`(a)`:

```
[task-notify] merger: CLI contract gate ABSTAINS for feat/voice-input
  (tip unreadable: dispatch chain not found (looked for "isCompletionSubcommand(dispatchArgs)"))
  — merging unchanged
```

A gate that goes permanently blind because someone renamed a local variable is
worse than no gate (it reports green while checking nothing). The parser was
hardened to capture the predicate's **argument name** rather than hardcode it —
the first call's argument is treated as canonical, and a chain mixing two
different argument names is a shape change (abstention). Two regression tests
pin this: `TestParseDispatchChain_RenamedArgumentStillParses` and
`TestParseDispatchChain_MixedArgumentsAbstains`.

The second live run — replayed against the **real `bd3d34a5` blob** — merged a
**half-fixed** branch (mirror updated, token map not) that the project's own
guard then reported red on master. That is what forced the two-arm conjunction
above; the gate now refuses it (`TestMergeContractGate_RefusesMissingTokenMapEntry`,
plus the live replay in Evidence §3).

## MEASURED cost per merge tick

> **SUPERSEDED IN PART (2026-09-13) — read this first.** The gate's comparison
> BASIS changed: it now judges the blob a three-way merge would KEEP
> (post-merge winner per file) instead of the branch tip, so it reads 3
> revisions (merge-base, tip, target) and makes one `git merge-base` call. The
> reads ride ONE `git cat-file --batch` process, so the whole decision is **2
> subprocesses** (`mergeContractGateBatchCalls + mergeContractGateBaseRefCalls`),
> not 4 — measured 45.3 ms/branch against 24.2 ms for the old 4-`git show` basis
> and 54.3 ms for a naive one-`git show`-per-blob winner rule; nearly all of the
> increase is the ONE `git merge-base` (21.5 ms on this 831-commit-divergent
> pair), which is the irreducible price of judging the post-merge tree. The
> 8.4 ms figure below was taken with a WARM cache and does not survive a fresh
> subprocess measurement on this machine; treat the subprocess COUNT as the real
> budget.
> See `devdoc/merge-time-contract-gate-20260913-postmerge-winner.md` for why the
> tip-based basis was WRONG (it refused a docs-only branch whose merged tree is
> consistent, and prescribed a fix that would delete valid master entries).

Measured on this machine (`time.Now()` around the production calls in a
temporary probe test, real git in a temp repo, seed `cmd/agent/main.go` + contract suite
whose test file carries both the mirror and the token map):

| Measurement | Value |
|---|---|
| Gate marginal cost, one branch (4 × `git show` + 2 parses + compare) | **6.5–10.6 ms** (mean **≈ 8.4 ms** over 200 runs) |
| `git show` calls issued by the gate per branch | exactly **4** = 2 files × 2 revisions (now **0** — the 6 blobs ride one `git cat-file --batch`) |
| Other git invocations (`fetch`/`checkout`/`rev-parse`/…) | exactly **0** (now **1** — `git merge-base`) |
| Subprocesses spawned | **4** (`git show`); no build, no test process, no LLM |
| A per-branch `go test ./cmd/agent/ -run TestCLIContract` for comparison | **1.1–1.9 s** (≈ 200×) |

The call budget is pinned by `TestMergeContractGate_CostIsBounded`: a PATH shim
records every `git` invocation and asserts exactly
`mergeContractGateMaxShowCalls` (= `mergeContractGateFilesPerRevision` ×
`mergeContractGateRevisions` = 4) `show` calls **and zero other git calls**. A
regression that checks out a tree, builds, or runs `go test` blows the budget
and fails the test. The shim was probe-verified to be able to fail: a
deliberately doubled read made it report 6 calls and fail.

Cost is linear in candidates: ≤ `repoSweepMaxBranches` (20) × ~8.4 ms ≈ **0.17 s
worst case per repo per tick**, against a merger poll interval of minutes.

Design (b) — running the real `TestCLIContract*` per branch on the merge path —
was rejected on this number: 20 branches × ~1.5 s ≈ 30 s per repo per tick, plus
a checkout/worktree per branch, plus the merger design contract's own rule that
semantic judgement (the fix) belongs to the executor.

## Evidence

### 1. RED on the old bug — the negative cases (unit)

Three negative arms, one per way a branch can land this drift:

- `TestMergeContractGate_RefusesVoiceInputShape` — the exact `bd3d34a5` shape
  (predicate added, mirror untouched): `ContractRefused=1`, `Merged=0`,
  `Action="contract-refused"`, Detail names `VoiceSubcommand`, **master tip
  unchanged**, **no `.git/MERGE_HEAD`**, branch ref intact.
- `TestMergeContractGate_RefusesMissingTokenMapEntry` — the **half-fixed** branch
  the live run exposed (mirror updated, no `dispatcherOfToken` entry): refused,
  master untouched.
- `TestParseDispatchChain_RenamedArgumentStillParses` /
  `TestParseDispatchChain_MixedArgumentsAbstains` — the parser footgun pair.

The pure-parser twins `TestCompare_VoiceInputIncidentIsCaught` and
`TestCompare_TokenMapArmIsRequired` do the same on the real 25/26-predicate
fixtures.

### 2. Positive controls (unit)

`TestMergeContractGate_FixedBranchMerges` and
`TestMergeContractGate_FixedBranchMergesTokenMapUpdated`: the SAME branch, with
BOTH halves shipped, merges (`Merged=1`) and `master:cmd/agent/main.go` then
contains `isVoiceSubcommand(dispatchArgs)`. The gate refuses the drift, not the
feature — and the pair together pins the boundary at exactly mirror AND token
map.

### 3. Live demonstration on the REAL incident commit (unmodified `bd3d34a5`)

Stronger than a synthetic repro: a temp repo whose `master` is set to the real
pre-incident parent `7f0980e2`, with the real bug commit `bd3d34a5` as the
branch (`git branch feat8/voice-input bd3d34a5` — no edits, no re-creation), and
the gate run through the real CLI face built from this worktree.

**RED — the unmodified incident branch is refused:**

```
$ gopi task merge --repo <repo> feat8/voice-input
merge feat8/voice-input    contract-refused CLI contract drift — main.go newly wires dispatch
  predicate(s) [VoiceSubcommand] that cmd/agent/cli_contracts_test.go does not track — add them to
  cliContractDispatchPredicates (and give each a cliContractInventory case) in this same branch
  (branch feat8/voice-input, target master, master unchanged)
task merge (no-submit): merged=0 … contract-refused=1 failed=0
EXIT=1
MASTER BEFORE: 7f0980e2681d620ee9a42d05315482b63dbc7878
MASTER AFTER:  7f0980e2681d620ee9a42d05315482b63dbc7878   BYTE-IDENTICAL: YES
MERGE_HEAD: absent        branch INTACT: YES
```

**RED — the half-fixed branch (mirror updated, token map NOT) is refused too.**
This is the case the gate originally MISSED and the live run exposed; the
`dispatcherOfToken` arm now catches it:

```
merge feat5/voice-input    contract-refused CLI contract drift — main.go newly wires dispatch
  predicate(s) [VoiceSubcommand] that cmd/agent/cli_contracts_test.go does not track …
MASTER BEFORE = MASTER AFTER = 7f0980e2681d620ee9a42d05315482b63dbc7878  (byte-identical)
```

(The two earlier attempts at this demo were invalid and are recorded because
they were instructive: I edited the branch's file in the *working tree* and then
checked out master, so the commit I thought I was testing carried **both** fixes.
The gate was right both times. The probe now verifies the **committed blob**
(`git show <branch>:<path>`) before running — a test that trusts its own
working tree is testing the tree, not the commit.)

**GREEN — the fully fixed branch merges and master's contract suite passes:**

```
$ gopi task merge --repo <repo> feat7/voice-input
merge feat7/voice-input    merged     clean merge, branch deleted
task merge (no-submit): merged=1 … contract-refused=0 …       EXIT=0
MASTER 7f0980e2 -> b8760f57
$ go test ./cmd/agent/ -run TestCLIContract -count=1     # ON THE MERGED MASTER
ok  	github.com/nick/gopi/cmd/agent	1.287s
```

That last line is the acceptance the epic actually asked for: the merge path
refused the red shape, and the branch that shipped the whole fix landed with the
contract suite green on the merged tree.

### 4. REAL-BLOB live acceptance, re-run at the final commit `567b5d9f` (2026-09-12)

Re-run after the commit, on the **actual incident blob** rather than the test
fixture: a fresh repo whose `master` is the real pre-incident revision
`7f0980e2` (both `cmd/agent/main.go` and `cmd/agent/cli_contracts_test.go` taken
straight out of git), and whose branch `feat8/voice-input` carries the
**unmodified `bd3d34a5` `cmd/agent/main.go`**.

Blob provenance was verified against git, not assumed:

```
git -C <worktree> show bd3d34a5:cmd/agent/main.go  → TIP_BLOB == bd3d34a5 main.go : YES
git show --stat bd3d34a5    → cmd/agent/main.go | 2 +-   (the ONLY contract-relevant file)
```

**RED — the real incident, run through the real CLI face built from this worktree:**

```
$ gopi task merge --repo <repo> feat8/voice-input
merge feat8/voice-input   contract-refused CLI contract drift — main.go newly wires dispatch
  predicate(s) [VoiceSubcommand] that cmd/agent/cli_contracts_test.go does not track — add them
  to cliContractDispatchPredicates (and give each a cliContractInventory case) in this same
  branch (branch feat8/voice-input, target master, master unchanged)
task merge (no-submit): merged=0 … conflict=0 exempt=0 contract-refused=1 failed=0
EXIT=1

MASTER BEFORE: b589ba285487e85c4a64704376ac09c6bb940edd
MASTER AFTER:  b589ba285487e85c4a64704376ac09c6bb940edd
MASTER BYTE-IDENTICAL: YES      MERGE_HEAD: absent      BRANCH INTACT: YES
WORKTREE CLEAN: YES (git status --porcelain empty)
```

**GREEN — the same real case with the fix shipped, merges and leaves the
contract suite consistent ON THE MERGED MASTER:**

```
$ gopi task merge --repo <repo> feat7/voice-input
merge feat7/voice-input   merged   clean merge, branch deleted
task merge (no-submit): merged=1 … contract-refused=0 failed=0        EXIT=0
MASTER b589ba2 -> 818b9d0f
master:cmd/agent/main.go            contains isVoiceSubcommand  (1)
master:cmd/agent/cli_contracts_test.go tracks VoiceSubcommand   (2: mirror + token map)
```

And a standalone replay of the project's OWN guard semantics on the merged
blobs (`git show master:…`, no build, no test binary — the blob is what landed):

```
main.go predicates : 26      mirror tracked : 26
count match : True           order match : True
mirror preds with NO token-map entry : ['MemorySyncSubcommand']
```

26 == 26 and the order matches, so `TestCLIContractInventoryMatchesDispatch`
passes on the merged master — measured, not assumed. (The one mirror predicate
with no token-map entry is `MemorySyncSubcommand`, a **pre-existing master
condition** present before the incident; it is exactly the case the gate's
`branch-introduced only` rule refuses to act on, and it is why the two
half-fixed negative tests exist rather than the gate trusting the mirror alone.)

The demo artefacts live in the (untracked) `.gate-accept/` directory of the
worktree; the numbers above are the printed output, reproduced verbatim.

### 5. Real merger path (end-to-end, `MergerRepoSweep`)
## Rules compliance

- **Rule 1/13/14** — every literal is a named constant in one place:
  `mergeContractGateActionRefused`, `mergeContractGateRefuseDetailFmt`,
  `mergeContractGateRevisionSep`, `mergeContractGateDisabledValue`,
  `mergeContractGateEnvName`, `mergeContractGateFilesPerRevision`,
  `mergeContractGateRevisions`, `mergeContractGateMaxShowCalls`,
  `mergeContractGatePresenceProbeBytes`, `taskMergeSourceAuto`,
  `contractGateRefuseFmt`, and the parser's `dispatchChainStart` /
  `dispatchChainOpen` / `dispatchJoin` / `chainParseMinPredicates` /
  `mirrorListStart` / `mirrorListEnd` / `mirrorNamePrefix` /
  `dispatcherTokenMapStart` / `dispatcherTokenMapEnd` / `listOpenBrace`. The
  cross-package file paths and the one anchor another package must probe for are
  exported constants in `internal/clicontract` (`DispatchChainFile`,
  `ContractSuiteFile`, `DispatcherTokenMapAnchor`) and referenced — never
  re-typed — from `internal/tools`.
- **Rule 15a/15b** — no silent failure: every git/parse error is either an
  ABSTENTION with a `taskNotifyLog` line naming the revision and the reason, or
  a refusal with the measured drift in the Detail; nothing is swallowed. (This
  rule also caught a real defect here: the first `Compare` arm OR-ed the
  token-map coverage with the mirror coverage, which silently *disabled* the
  mirror half. The unit tests for the two arms are what surfaced it.)
- **Rule 2** — `GOPI_MERGE_CONTRACT_GATE` has exactly one reader
  (`mergeContractGateDisabled`); the default (enabled) is the constant.
- **Rule 3** — no new top-level tool. The extension is a new package
  (`internal/clicontract`) + wiring in existing packages + the existing
  `gopi task merge` CLI face.
- **Rule 4** — edits were surgical (`edit` / scripted single-anchor replacements).
- **Rule 6a** — work done in the worktree `~/workspace/gopi-mergegate` on branch
  `feat/merge-time-contract-gate`. The main repo was never written to.
- **Rule 8** — files added one by one.
- **Merger design contract** — the merger DETECTS; the semantic judgement (fix
  the contract suite) is handed to an executor via the rework task. The refusal
  never conflicts with "a failed merge must leave master buildable/untouched":
  master is untouched **by construction** (no merge started).
- **Not weakened** — `cliContractDispatchPredicates` itself is untouched; the
  gate reads it. `cmd/agent/cli_contracts_test.go` and the `cli_voice*.go` files
  owned by concurrent tasks were not modified in this worktree (they appear in
  `go test ./...` only as the incident the gate exists to prevent).
- **Rule 12 scope note** — `internal/standards/baseline.go` was touched, and
  only to MOVE two existing `internal/tools/task_merge.go` baseline entries
  (`:250`→`:277` for Rule 16, `:223`/`:237`→`:250`/`:264` for Rule 18) that this
  change's insertions shifted. No new baseline entry was taken and no permitted
  count changed: the ratchet did not move; the line numbers did.

## Files

| File | Change |
|---|---|
| `internal/clicontract/clicontract.go` | **new** — parser + comparison core, zero deps beyond stdlib, no git/exec |
| `internal/clicontract/clicontract_test.go` | **new** — parser/comparison tests incl. the incident fixture, the token-map arm, and the renamed-argument/mixed-argument guards |
| `internal/tools/merge_contract_gate.go` | **new** — git I/O + verdict, wired to `clicontract` |
| `internal/tools/merge_contract_gate_test.go` | **new** — merge-path tests, RED repro, cost pin, routing + dedup |
| `internal/tools/task_merge.go` | step ⓪ in `taskMergeOne`, `ContractRefused` count, refusal→rework routing, `taskMergeSourceAuto` |
| `internal/standards/baseline.go` | **line-number-only** move of two existing `task_merge.go` entries (see Rules compliance) |
| `internal/tools/repo_sweep.go` | `RepoSweepResult.ContractRefused`; call the routing pass after the merge pass |
| `cmd/agent/cli_tasks.go` | `contract-refused=%d` in the merger summary line |
| `cmd/agent/cli_tasks_merge.go` | `contract-refused=%d` in the merge report; exit 1 when any refusal |

## Verification (re-run at branch tip `567b5d9f`)

- Build: `CGO_ENABLED=0 go build ./...` — OK (exit 0, no output)
- Format: `gofmt -l cmd internal` — clean (no files listed)
- Tests:
  - `go test -count=1 ./internal/clicontract/` — ok (13/13 PASS, all named tests shown)
  - `go test -count=1 -run 'TestMergeContractGate|TestMergerSweep_ContractRefusal' ./internal/tools/` — **10/10 PASS** (RED repro, token-map arm, two positive controls, no-drift, abstention, env disable, routing, dedup, cost pin)
  - `go test -count=1 ./internal/standards/` — ok (4.68 s)
  - `go test ./cmd/agent/ -run TestCLIContract -count=1` — RED at base (the incident
    itself, owned by concurrent tasks 01a091fb-1cff/-1e7d), and GREEN on the merged
    master of the demo repo after the gate let the fully-fixed branch land.
  - `go test ./internal/stylegate/ -count=1` — red only at sites in
    `cmd/agent/cli_voice*.go`, owned by that same concurrent lane; zero hits in any
    file this change touches.
- Live acceptance on the real blobs (Evidence §4): REFUSED with master
  byte-identical / no MERGE_HEAD / branch intact, then MERGED for the fixed
  branch with the guard's own semantics replayed green on the merged master.
- Cost: **6.5–10.6 ms per branch** (mean ≈ 8.4 ms, n=200), 4 `git show`
  subprocesses, 0 other git calls — pinned by `TestMergeContractGate_CostIsBounded`.
- Commit: `567b5d9f feat(merger): refuse CLI-contract drift at merge time`, the
  tip of branch `feat/merge-time-contract-gate`.
