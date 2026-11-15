<!-- doc-class: normative -->
# The cross-node retirement's deletion ORDER — measured closures, not batch lists

> **COMPLETED 2026-11-05.** Every batch this page orders has since RUN: the
> cross-node sync surface is deleted from the tree, and the two knobs the page
> defers to a later batch are gone with it — `GOPI_SYNC_GIT_INTERVAL` (which this
> page says "keeps 3 and belongs to 第5批") and `GOPI_SYNC_INTERVAL` /
> `GOPI_SYNC_FULL_EVERY`. None of the three ever had a production reader (the
> catalog and `gopi --help-env` advertised them anyway — Rule 21's defect shape).
> A `grep -rn 'GOPI_SYNC_GIT_INTERVAL' --include=*.go internal/ cmd/` today returns
> **0** (the ~419-line command below was the acceptance check for this: it now
> passes). The anti-re-advertisement gate that keeps them from coming back is
> `internal/config/sync_knob_removal_gate_test.go`. The ORDERING METHOD this page
> argues for stays valid; its batch lists are history.

**The subject.** Epic `01a1a87f` retires ~17k lines of cross-node code in five
batches (rows from `01a1ac6c-0da9` through `01a1ac6c-0e8f`), plus a sixth row
(`01a1ac6c-0d7f`) that builds the dependency table. The epic's acceptance
standard for every batch is `go build ./...` exit 0 with no output, and the
batch numbers say the batches run in order. **This page records that those two
statements cannot both be true, measures what the real closures are, and gives
the order that does hold.**

Every figure below is a command's output, quoted. The raw logs are in this
run's evidence directory:
`/home/user/.gopi/sessions/01a1ad46-7ee2-72ab-b3cd-958d06f9dab7/exp*/build.log`.

**What is NOT in this page's scope.** It deletes nothing. It decides one
classification (`script_install_sync.go`, §7) and performs the moves that
must precede a deletion (§6). The deletions themselves stay with the rows that
own them.

---

## 1. The measurement method — removal experiments, not symbol tables

A symbol table answers "who mentions X". It does not answer the only question
that matters for a batched deletion: **after this batch, is the tree still
buildable?** That is a different question, and it is answered by removing the
files and reading the compiler.

```bash
E=/home/user/.gopi/sessions/01a1ad46-7ee2-72ab-b3cd-958d06f9dab7
mkdir -p $E/expN
for f in <the batch's production files, without .go>; do mv internal/tools/$f.go $E/expN/; done
go build -gcflags="-e" ./... 2>&1 | tee $E/expN/build.log
mv $E/expN/*.go internal/tools/     # restore; git status --porcelain must print nothing
```

`-gcflags=-e` is not optional: the default Go compiler stops after ten errors
**per package**, so a plain `go build` reports the first handful of a larger
set and a reader mistakes a truncation for a closure. Every log quoted here is a
full `-e` run.

Four experiments were run. Their results are the evidence behind §2–§5.

| exp | what was removed | error lines | consumer files | distinct symbols |
|---|---|---|---|---|
| `exp2` | 第2批's 8 pure-peer production files, **alone** | 73 | 18 | 24 |
| `exp3` | 第2批's 8 + 第3批's 13, **together** | 215 | 27 | 73 |
| `exp4` | 第3批's 13, **alone** | 166 | 23 | — |
| `exp5` | 30 files: 第2批 + 第3批 + 第4批's partners + `task_sync_git.go` | 92 | 19 | 36 |
| `exp7` | **the maximal closure**: all 32 production files any batch names, plus `task_sync_git.go` and `task_sync_git_pushback.go` | **94** | **17** | — |

The numbers that matter are `exp2` and `exp7`, and they say opposite things
about the plan.

---

## 2. 第2批 cannot go first — 18 consumers, none of them its own files

`exp2` removes exactly what 第2批's row says it deletes (the 8 pure-peer
production files: `peer_alias.go`, `peer_remote_target.go`,
`peer_repo_identity.go`, `peer_repo_path.go`, `peer_self_host.go`,
`peer_topology.go`, `notify_peer.go`, `git_peer_fetch.go` — 2539 lines) and
nothing else:

```
$ go build -gcflags="-e" ./...    # after removing those 8 files
error lines: 73    consumer files: 18    distinct symbols: 24
```

**All 18 consumers are files 第2批 does not touch.** Measured, with each file
attributed to the batch row whose own text names it
(`gopi task show <id> | grep -oE '[a-z_0-9]+\.go'`):

| consumer file | lines | errors | named by a batch row |
|---|---|---|---|
| `internal/tools/task_sync_git.go` | 1186 | 22 | **no** |
| `internal/tools/finish_cleanup_survivors.go` | 6258 | 7 | **no** |
| `internal/tools/task_briefing_master_reading.go` | 447 | 4 | **no** |
| `internal/tools/task_sync_git_pushback.go` | 1138 | 4 | **no** |
| `internal/tools/task_ground_baseline.go` | 113 | 2 | **no** |
| `internal/tools/task_delivery_proof.go` | 1481 | 1 | **no** |
| `internal/tools/task_run_ground.go` | 1378 | 1 | **no** |
| `internal/tools/memory_sync_reconcile.go` | 582 | 5 | 第3批 |
| `internal/tools/memory_sync_row.go` | 1217 | 5 | 第3批 |
| `internal/tools/task_transfer.go` | 1001 | 5 | 第3批 |
| `internal/tools/memory_sync_task.go` | 2764 | 4 | 第3批 |
| `internal/tools/memory_sync_assets.go` | 1062 | 2 | 第3批 |
| `internal/tools/memory_sync_row_chain.go` | 350 | 2 | 第3批（**DELETED** — see the note under this table） |
| `internal/tools/task_submit_forward.go` | 485 | 2 | 第3批 |
| `internal/tools/ledger_agreement.go` | 219 | 1 | 第3批 |
| `internal/tools/ledger_backup.go` | 849 | 1 | 第3批 |
| `internal/tools/memory_sync_session.go` | 326 | 1 | 第3批 |
| `internal/tools/memory_sync_claim.go` | 1492 | 3 | 第4批 |

> **DELETED 2026-11-05** (task `01a1ad78-a0f4-7b84-84b8-93952bf83af3`): the
> `memory_sync_row_chain.go` row above is history. That file, plus
> `internal/tools/task_chain_transfer.go` and `task_chain_transfer_test.go`, are
> gone: the chain SEND leg's only entry point was `task send --chain`, and
> `taskverbs.Send` is in `RemovedVerbs`, so the mechanism was already
> unreachable. One symbol had readers OUTSIDE it and was MOVED rather than
> deleted, in the same commit (Rule 43): `chainTransferMaxRows` — the peer
> handler's inbound patch bound, read by `internal/tools/memory_sync_row.go`
> and its test — now lives in that file as `syncRowPatchMaxRows`, value
> unchanged at 64. It stays a wire defence against a malformed sender; there is
> no second bound on the import path, which is why it was not relaxed.

`internal/tools/task_sync_git.go` is the largest single consumer of 第2批 at
22 errors, and **no batch row names it**. This is the finding the epic's own
第2批 run reported in its *own* brief (its table's row for `peer_repo_identity.go`
reads "自己批 / 都不在", and for `git_peer_fetch.go` "第 3 批 + 永不删的交付腿")
— but the row's own file list still asks the batch to delete those files, and
its acceptance standard still asks for a green build. **The brief's evidence
and the row's instruction contradict each other, and the instruction is what
the executor runs.**

---

## 3. 第2批 and 第3批 are each other's upstream — there is no "first" that is green

`exp3` removes 第2批's 8 and 第3批's 13 together; `exp4` removes 第3批's 13
alone. Read together with `exp2`:

```
exp2 (b2 alone):  73 error lines / 18 consumer files
exp4 (b3 alone): 166 error lines / 23 consumer files
exp3 (b2+b3):    215 error lines / 27 consumer files
```

`exp4` is the mirror image of `exp2`: removing 第3批 alone breaks 第2批's own
files. **Four of the 166 errors are in `notify_peer.go` — one of the files
第2批 is asked to delete — because it consumes `SyncRowOrigin` and `SyncTaskRow`
from files 第3批 is asked to delete.**

```
$ grep -cE '^internal/tools/(peer_alias|peer_remote_target|peer_repo_identity|peer_repo_path|peer_self_host|peer_topology|notify_peer|git_peer_fetch)\.go' exp4/build.log
4
```

And `exp3` — doing both at once — is **larger, not smaller**, than either half:
215 errors across 27 files. **Union does not close the set.** That is the fact
that kills the "combine the two batches into one commit" remedy: it is a bigger
red, not a green, and the missing consumers are the same unowned files §2
already listed.

⇒ **Both directions are red, and their union is redder. The layering is not a
scheduling problem that a better order fixes.**

---

## 4. The maximal closure is still red — the plan has no termination at all

`exp7` is the strongest possible reading of the 5 batches: every production file
any batch row names (32 files), **plus** `task_sync_git.go` and
`task_sync_git_pushback.go`, which the batches do not name but which the earlier
experiments show consume the same symbols:

```
$ go build -gcflags="-e" ./...    # 33 files removed
94 error lines across 17 files
```

Grouped by the surviving file that needs the symbol — **none of these 17 is in
any batch's deletion list**:

```
internal/tools/branch_delete_guard.go           gitPushbackCheckRootRef
internal/tools/deploy_drift.go                  gitPushbackCheckRootRef
internal/tools/finish_cleanup_survivors.go      GitSyncRepoRoot peerRemoteDeclaredAlias peerRemoteTarget peerRemoteTargetFor peerRepoPathForRepo
internal/tools/integration_claim_target.go      gitPushbackCheckRef
internal/tools/master_claim.go                  gitPushbackCheckRef
internal/tools/rework_landing_probe.go          shortSha
internal/tools/task_briefing_master_reading.go  GitSyncServerTarget gitPeerFetchVerb gitPeerMirrorRefspec gitPushbackCheckNamespace gitPushbackCheckRef gitSyncLagAlignedValue gitSyncLagUnknown pushbackRemoteNameListSep pushbackRemoteNameNone pushbackRemoteNameVerb shortSha
internal/tools/task_deliverable_git.go          shortSha
internal/tools/task_delivery_loop.go            gitPushbackCheckRef gitPushbackClassifyPeerRef gitPushbackFastForwardsOntoMaster gitPushbackFetchMaster gitPushbackFetchTimeout gitPushbackGitRun gitPushbackLocalSha gitPushbackPeerRefCollision gitPushbackPeerRefHeadsFmt gitPushbackPeerRefMirrorFmt gitPushbackPeerRefTip gitPushbackPush gitPushbackRefusal gitPushbackSharesHistoryWithMaster pushbackUnrelatedHistoryReasonFmt shortSha
internal/tools/task_delivery_proof.go           gitPeerCommitSuffix
internal/tools/task_delivery_retry.go           gitPushbackCheckRef gitPushbackFetchMaster
internal/tools/task_ground_baseline.go          SyncPeerFetch gitPeerFetchTimeout shortSha
internal/tools/task_ground_cutpoint.go          shortSha
internal/tools/task_reclaim_self.go             TaskClaimOwner
internal/tools/task_run_ground.go               gitPeerRun
internal/tools/taskrunlog.go                    maxInt
internal/tools/tasks.go                         guardTaskSubmitLedgerAddress taskSubmitForward taskSubmitForwardRefusal taskSubmitLedgerTargets taskSubmitLocalIDs taskSubmitMintForwardID
```

**`internal/tools/task_delivery_loop.go` alone is 16 of the 17 symbols it
needs, and it is the clone channel's delivery leg.** PROJECT.md Rule 6c makes
that leg *"not the run's"*: it is the machinery that publishes a clone ground's
`master` and it exists **because** gopi's dispatch model is clone-per-run. It is
**never** a deletion candidate. Same for `finish_cleanup_survivors.go` (the
close-out chain), `tasks.go` (the ledger), `task_reclaim_self.go`,
`taskrunlog.go`.

⇒ **"Delete all five batches" does not produce a green build either.** The
closure of the deletion set is larger than the deletion set, and its extra
members are the system's own delivery and close-out machinery.

---

## 5. The corrected order — DAG, with each step's closure stated

The unit of delivery is **not** "the batch's file list". It is a **leaf closure**:
a set of files whose removal leaves `go build ./...` at exit 0 with no output,
verified by `exp`-style removal before the commit is made. Rule 43's own lesson
(`devdoc/rules-reference.md`, "先搬后删") generalises: a general tool that has a
surviving consumer **moves out first**, in the same commit as the deletion.

**Step 0 — the moves (do these first; they are green on their own).**
Each move is "a symbol whose definition sits in a to-be-deleted file but whose
consumers survive". Done in this run, §6. Nothing else in the plan can start
until these are landed, because every later closure's residual size is measured
*after* them.

**Step A — the leaf closure (green, and it is a FOUR-point closure, not three).**
This is the only deletion the whole plan can do in one green commit, and it was
measured here rather than assumed. Removing the three files
`internal/tools/git_sync_lag_check.go` **and** `cmd/agent/worker_ref_lag_check.go`
**and** `cmd/agent/ref_lag_tick_clock.go` is **NOT** enough:

```bash
$ go build -gcflags="-e" ./...    # the three files removed
# github.com/the operator/gopi/cmd/agent
cmd/agent/cli_tasks_worker.go:667:12: undefined: newWorkerRefLagCheck
1 error line
```

`cmd/agent/cli_tasks_worker.go:667` constructs the check at the top of the claim
loop and `:670` calls `Report()` each round. Those **three lines are the wiring**,
and they are part of the closure. Removing the two files and the two statements
gives the measured zero:

```bash
$ go build -gcflags="-e" ./...    # three files + the two statements removed
0 error lines          # $E/expA/build.log is empty
```

The fourth point is the test that owns the type: `cmd/agent/worker_ref_lag_check_test.go`
must go in the same commit (its `workerRefLagTestCheck` constructs
`&workerRefLagCheck{...}` directly, so it does not compile once the type is gone).

Acceptance: `go build -gcflags="-e" ./...` prints nothing, plus the
env-orphan check — `refLagTickEnv` / `refLagTickDefault` / `refLagTickInterval`
drop to **zero** Go hits tree-wide (measured after removal), while
`GOPI_SYNC_GIT_INTERVAL` keeps 3 and belongs to 第5批, not here.

**Step B — the clone-channel delivery leg is EXCLUDED from the epic, by name.**
`task_delivery_loop.go`, `task_delivery_proof.go`, `task_delivery_retry.go`,
`task_sync_git_pushback.go`'s `gitPushback*` family, `finish_cleanup_survivors.go`,
`task_ground_baseline.go`, `task_ground_cutpoint.go`, `task_run_ground.go`,
`task_briefing_master_reading.go`, `branch_delete_guard.go`,
`deploy_drift.go`, `integration_claim_target.go`, `master_claim.go`,
`rework_landing_probe.go`, `task_deliverable_git.go`, `task_reclaim_self.go`
stay. This is not a judgement call: they are the machinery that makes the
clone-per-run model work, and the epic's premise ("keep the single-node state
machine") **requires** them.

**Step C — 第3批's `memory_sync` body, as the FIRST real deletion, after Step 0.**
`exp4` (166 errors) is the *pre-move* reading. With §6's moves applied the
residual drops (measured: exp5's 36 distinct symbols → exp6's 26 after eight
symbols moved). 第3批 goes first because after 第2批's pure-peer files are *not*
removed, its own consumers (`RequireDeclaredPeer`, `ValidateSSHPeerAlias`,
`selfPeerWriteRefusal`) still exist — the direction that keeps the
`memory_sync` half deletable **and** the peer half present.

**Step D — 第2批's pure-peer production files, as a unit, only after Step C.**
By then its `memory_sync` consumers are gone; what remains is the Step-B set,
which is excluded, plus `task_sync_git.go`. `task_sync_git.go` is a **batch of
its own** (§7 explains why `script_install_sync.go` is not, but
`task_sync_git.go` is: 1186 lines, 22 errors against 第2批, and its whole file
header is the cross-node git sync pass — `syncGitClientPass` /
`syncGitServerPass`, triggered by the serve tick). It must be deleted **with**
its `gitSync*` constants and its `cmd/agent` dispatch surface, in one commit,
because splitting it leaves 22 dangling references on both sides.

**Step E — 第4批 (`memory_sync_claim.go`'s Phase-2/3 legs).** It consumes
`RequireDeclaredPeer` / `ValidateSSHPeerAlias` (第2批) and `syncUnknownHost`
(第3批), so it can only go after both — but after Steps C+D those symbols are
gone and its residual is its own CLAIM LOCK PROTOCOL section in
`cmd/agent/cli_tasks.go`, which is a code change, not a deletion.

**Step F — 第5批 (config keys, `CONFIG.md`, `GOPI.md` prose).** Last, because it
is the only step whose acceptance is verifiable without a build, and doing it
earlier would make `settings.json` stop describing keys the code still reads.

**The invariant that replaces "batches run in order":** *a commit is allowed to
land only when `go build ./...` is silent on its own ground*. Steps C and D
therefore cannot be two commits if the tree between them is red — measured,
`exp4`/`exp2` prove it is. **Either C+D is one commit, or D's file list is
narrowed to what §6's moves + Step E make independent.** The measured fact is
that the current row split does neither.

---

## 6. The moves performed in this run (Rule 43, demonstrable)

Eight symbols were defined in files the plan deletes while their consumers
survive. Each was moved **without rewriting its body** (Rule 4: a move, not an
edit), and the receiving file's header quotes the measured consumers.

| symbol | was in | now in | surviving consumers |
|---|---|---|---|
| `QueryExecer` | `memory_sync.go` | `memory.go` | `memory.go`, `tasks.go`, `task_session_stamp.go`, `memory_provenance_latent.go`, `task_ledger_read_schema_align.go`, `archive_row_reconcile.go`, `task_merge_backlog_terminal_guard.go`, `task_reclaim.go` |
| `SyncRowOrigin` + `originLocalOnlyRender` / `originLocalOnlyNote` / `originLocallyReachable` / `sameOriginHost` / `originOfWire` / the three `originLocalOnly*` constants / `originReachability` + its three values | `memory_sync_origin.go` | `memory_origin_wire.go` (new) | `memory.go:675` (`MemoryHit.MemoryOriginWire`) and `memory.go:5226` (the rendered hit) |
| `IsServerNode` | `memory_sync_delta.go` | `node_role.go` (new) | `finish_cleanup_survivors.go:1631`, `task_ground_baseline.go:90` |
| `taskIDPrefixCandidates` | `memory_sync_row.go` | `tasks.go` | `tasks.go` (`taskIDPrefixRefusal`), plus its own `syncRowResolveTaskID` (deleted with 第3批) |
| `taskSyncDB` + `taskStatusDirect` | `memory_sync_task.go` | `tasks.go` | `run_ground_anchor.go`, `task_transfer.go` |
| `maxInt` | `memory_sync_procedural.go` | `memory_sync_procedural.go` (kept + exported comment) | `taskrunlog.go:93,596` — see the note below |
| `syncUnknownHost` | `memory_sync_task.go` | `host_identity.go` (new) | `tasks.go:8716`, `task_reclaim_self.go:302` |
| `TakeOverFlagSpelling` | `memory_sync_row.go` | `host_identity.go` (new) | `taskprogress.go:116` |

**Measured effect on the closure**, by re-running `exp5`'s removal set as `exp6`
after the moves:

```
before (exp5): 92 error lines / 36 distinct symbols
after  (exp6): 46 error lines / 26 distinct symbols
removed from the residual set:
  IsServerNode QueryExecer SyncRowOrigin TakeOverFlagSpelling originLocalOnlyRender
  syncUnknownHost taskIDPrefixCandidates taskSyncDB
```

**One symbol is deliberately NOT moved: `maxInt`.** Its body already lives in a
surviving file after the move-and-keep above, and its only out-of-mechanism
consumer is `taskrunlog.go`'s label padding — a two-line helper duplicated in
`internal/gopilog/stats.go` (7 call sites, separate package, separate
definition). Duplicating it a third time would be the "mirror the literal"
anti-pattern Rule 13/14 forbid, so the definition stays where the sync
mechanism and `taskrunlog.go` can both reach it: `memory_sync_procedural.go`
is **not** in the deletion set after this reframe — it survives as the home of
`maxInt` only if 第3批 narrows its file list. That narrowing is the owning
row's decision, recorded here so it is not silently lost.

---

## 7. `script_install_sync.go` — a third misclassification: DELETE it from the plan

第2批's file list includes `script_install_sync.go` (270 lines). Measured, it
belongs to **no** cross-node mechanism:

```bash
$ grep -cE 'ValidateSSHPeerAlias|RequireDeclaredPeer|peerRemoteTarget|SyncPeer|peerRepo|NotifyPeer|GitSync|selfPeerWriteRefusal|PeerDeclared' internal/tools/script_install_sync.go
0
```

Zero calls into the peer/sync abstraction. Its header says what it is: the
post-merge close loop's second half — after a merge changes a script in the
repo, refresh the **installed copy** in `~/.local/bin` (the operator's shell
runs the installed copy, not the repo's). It is a single-node concern: no peer,
no wire, no ledger.

Its consumer surface is one exported function:

```bash
$ grep -rn "\bSyncInstalledScripts\b" --include=*.go . | grep -v script_install_sync
./cmd/agent/cli_tasks_rebuild.go:380:	res := tools.SyncInstalledScripts(repo)
```

And that consumer is not in any batch's list — confirmed by removing the
function:

```bash
$ go build ./...    # after renaming SyncInstalledScripts
# github.com/the operator/gopi/cmd/agent
cmd/agent/cli_tasks_rebuild.go:380:15: undefined: tools.SyncInstalledScripts
```

⇒ **Verdict: strike `script_install_sync.go` from 第2批.** It is a leaf of the
**clone/merge close loop** (`cli_tasks_rebuild.go` is the merge round's rebuild
step), not of the cross-node sync mechanism. Deleting it would delete a
surviving single-node capability and force `cli_tasks_rebuild.go` to go with it
— a file the epic has no business touching.

**The generalisable criterion** (this page's reusable part): a file's *name*
is evidence of nothing. 第2批's list contained three misclassifications by this
criterion, and the epic's own run found two of them
(`image_peer.go` — "which image is THAT process running?" — and
`firstopen_sync_decision.go`). `script_install_sync.go` is the third. The test
that separates them is the one run above: **count the calls into the mechanism
being retired; zero means the file is not in it.** Its filename's `sync` and
its `Script` prefix are both misleading, and only the grep is not.

---

## 8. `QueryExecer` — the leak 第3批's row would have created

第3批's list includes `memory_sync.go`, which declared:

```go
type QueryExecer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
```

Nothing about it is cross-node. It is the **minimal SQL surface the store core
needs** — `*sql.DB` and `*sql.Tx` both satisfy it, which is how one core
implementation serves the standalone store *and* the single-transaction import.
Its consumers are eight files no batch names, `memory.go` first among them
(26 of `exp3`'s 215 errors).

Removing `memory_sync.go` without moving it would have broken the **store**,
i.e. the one subsystem the epic exists to protect. It now lives in `memory.go`,
and the invariant is checkable in one line:

```bash
$ git grep -c 'type QueryExecer'
internal/tools/memory.go:1
```

---

## 9. Where this page's own claims are measured, and how to re-run them

```bash
# 1. the four removal experiments (restore after each; git status must be empty)
#    see §1 for the exact loop
go build -gcflags="-e" ./...

# 2. the move invariants — one definition site each
git grep -c 'type QueryExecer'                       # 1
grep -rn 'func IsServerNode' internal/tools/         # internal/tools/node_role.go
grep -rn 'type SyncRowOrigin struct' internal/tools/ # internal/tools/memory_origin_wire.go
grep -rn 'func taskIDPrefixCandidates' internal/tools/ # internal/tools/tasks.go
grep -rn 'func taskSyncDB' internal/tools/           # internal/tools/tasks.go
grep -rn 'func maxInt' internal/tools/               # internal/tools/memory_sync_procedural.go

# 3. the tree is green at this page's own rev
go build ./...      # exit 0, no output
go vet ./internal/tools/   # exit 0, no output

# 4. the env-orphan check the plan must carry (Rule 21's blind spot):
#    after any deletion, an env var that loses its last Go reader but keeps a
#    string-table entry is NOT caught by the env-reader gate
grep -rn 'GOPI_SYNC_GIT_INTERVAL' --include=*.go internal/ cmd/ | wc -l
```

**Honest limits of this page.** It does not delete anything, so no acceptance
line here proves a deletion works — it proves *which deletions are admissible
and in what order*. §5's Steps C+D still need their own removal experiment once
`task_sync_git.go`'s 22 references are resolved, because this page did not
decide how `task_sync_git.go`'s `gitSync*` constants are re-homed into the
Step-B survivor set; that is the next row's work, and it is a **move**, not a
deletion. The 第3批/第2批 rows are still open with their original file lists;
this page is the evidence that those lists must change before either is
dispatched.
