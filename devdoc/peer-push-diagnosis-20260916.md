<!-- doc-class: normative -->
# HP → peer1 push failure: diagnosis (2026-09-16)

> **CORRECTED 2026-11-05.** The cadence this page reasons about —
> `GOPI_SYNC_GIT_INTERVAL` "default 15 min" and the `serveGitSyncSweep` pass that
> read it — is **DELETED**: the knob was advertised by the catalog and
> `gopi --help-env` while **no production path read it** (`serveGitSyncInterval`,
> the reader this page names, does not exist in any revision of this tree), and it
> was removed 2026-11-05 with the whole cross-node sync surface. The diagnosis's
> CONCLUSION still stands and is why the page is kept — "the topology *permits* a
> push; nothing *schedules* one" — but the mechanism that answered it is now the
> clone channel's delivery loop, not a serve-tick pass. Gate that keeps the knob
> from returning: `internal/config/sync_knob_removal_gate_test.go`.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a0aab1-587a-7ca3-ba98-4b66b11b8b90` — *diagnose: why did HP never push its
finished branches to peer1? (client leg silently skipped)*
**Host the run executed on:** `hostb-Laptop` (the CLIENT — verified: `hostname`,
`~/.gopi/settings.json` `"role":"client"`).
**Diagnosis only.** No HP branch/log/worktree was deleted, no settings.json touched,
no binary rebuilt (stated constraint: rebuilding would change the state under
investigation).

---

## 1. Verdict

**The three branches did reach peer1 (hand-pushed), but HP's automatic client
close-out leg never delivered them — and the reason is a *chain skip*, not a broken
push.**

Confirmed cause (Hypothesis A, with the general defect behind it):

> The client close-out chain's **dirty-worktree guard sits BEFORE the push**
> (`internal/tools/task_finish_cleanup.go:992–1007`, then push at `:2028`). The push
> is a *non-destructive, idempotent* step, but it is gated behind a guard that exists
> to protect the *destructive* steps (`worktree remove --force`, `branch -d`).
> When the worktree is dirty the **whole chain — push included — returns skipped**.
> HP's runs routinely end with untracked `.evid/` residue, so the deliverable branch
> was never handed off, and nothing anywhere said "this branch was not delivered".

Supporting + contributing findings:

| # | Finding | Status |
|---|---|---|
| A | dirty gate precedes the push; a dirty worktree skips the **push itself** | **PROVEN** (code + ledger event + worker log line) for row `01a0aa61` |
| A′ | a worktree already **gone** skips the chain at `git status`, before any push | **PROVEN** for row `01a0aa0e` (`chdir … no such file or directory`) |
| A″ | a **foreign row's** residue release removes the worktree, keeps the branch, runs no push | **PROVEN** (log line, `gopi-mscount`) / delivery decision for `01a0aa74` **INFERRED** |
| B | the chain never ran on this close-out path | **REJECTED** — it ran and skipped, twice (close-out + sweep) |
| C | stale HP binary | **ELIMINATED** (measurement below) |
| — | the resident finish-cleanup sweep **does** run on the client and re-skipped in silence | **PROVEN** (0 `[finish-cleanup]` lines vs 2 per-row refusals) |
| — | the *push* safety net rides the **merger** loop — a process a client host does not run | **PROVEN** |
| — | `gitSyncMaxBranches = 20` truncates an **unsorted** branch list silently (latent; HP is at 10/20) | **PROVEN (latent)** |
| — | a skipped close-out renders as an opaque `git status: chdir …: no such file or directory` | **PROVEN** (reproduced today) |
| — | the dirty skip is filtered OUT of the sweep's report (`FinishCleanupSkipIsReportable`) | **PROVEN** (code + zero-line log) |

**Uncertainty (explicit, revised in §2.8 after the resident worker's own log was
located):** two of the three rows now have a **direct** per-row skip line, and they are
**not the same cause** — `01a0aa61` is the dirty gate (§2.3/§2.8), while `01a0aa0e`
skipped because its worktree was **already gone** (`git status: chdir …`) — a second
mechanism, not the one this epic hypothesised. Only
`fix/merger-status-branch-account` (`01a0aa74`) remains **without an attributable
line**; its end state (worktree gone, branch survives, never delivered) matches a
*different* path — a foreign/ownerless worktree release with no push leg (§2.8).

---

## 2. Evidence chain

### 2.1 The three branches exist on peer1 with matching SHAs (deliverable 3)

```
$ for b in feat/chat-m2-streaming fix/merger-status-branch-account fix/finish-cleanup-single-node-wording; do git rev-parse --short $b; done
900c0a05 feat/chat-m2-streaming
24eded5c fix/merger-status-branch-account
7264cc5a fix/finish-cleanup-single-node-wording

$ git ls-remote peer1 'refs/heads/*' | grep -E 'chat-m2-streaming|merger-status-branch-account|finish-cleanup-single-node-wording'
900c0a052a4ac5b9cd4f907b7838ff9e520d61d3  refs/heads/feat/chat-m2-streaming
7264cc5a2ab5861c007d45c5b8b7a8cdf0b01f92  refs/heads/fix/finish-cleanup-single-node-wording
24eded5c66cba7c7537c46cef884f296b8ec9e9b  refs/heads/fix/merger-status-branch-account
```

All three SHAs match byte-for-byte. peer1-side reflog stamps the arrival:

```
$ ssh peer1 'cd ~/work/GoPi && git reflog show --date=iso refs/heads/<branch>'
24eded5c …@{2026-09-16 22:30:02 +0800}: push          (fix/merger-status-branch-account)
7264cc5a …@{2026-09-16 22:48:23 +0800}: push          (fix/finish-cleanup-single-node-wording)
6cc98c61 …@{2026-09-16 22:48:23 +0800}: push          (feat/chat-m2-streaming, superseded by the 22:49:26 commit+push)
```

`git rev-list --count master..<branch>` = 20 / 3 / 5 ahead — the briefing's
"19-ahead" is 20 by current count (one commit landed after the briefing was written);
not material.

> ⚠️ **The 22:48:23 `push` on two branches is a HAND-PUSH** (three branches appear at
> the same second — a scripted batch, not three independent close-outs). This means peer1
> holding the branch is **not** evidence that the automatic leg worked. That distinction
> is the whole point of the diagnosis.

### 2.2 The defect: push is behind the dirty gate (`file:line`)

`internal/tools/task_finish_cleanup.go` — client arm `taskFinishCleanupChain` (`:963`):

- `:992` dirty gate — `git status --porcelain` on the worktree
- `:1007–1012` `if reason := finishCleanupWorktreeDirtyReason(rep, dirty); reason != "" { rep.Skipped = true; return }`
  — **the chain returns here, before the push**
- the push only happens later, inside `runFinishCleanupChainTarget` (`:1929`) at `:2028`:
  `finishCleanupGit(ctx, d.Repo, finishCleanupPushVerb, target, branch+":"+branch)`

The file's own header states the ordering as intentional:
`① guard … (dirty > 0 = a live Bond is still writing there — skip, never touch)`.
The guard is correct for step ③ (`worktree remove --force`, the data-loss path) and
step ④ (`branch -d`) — it is **over-broad** applied to step ②, a push that is
idempotent by contract (the same file reuses the bug038 "Everything up-to-date exit-0"
no-op). This is the "a safe step is blocked by a guard for destructive steps" defect —
already filed by the operator as `01a0aab8` (*"client push must not be gated by the
worktree dirty guard"*).

### 2.3 Direct ledger evidence that the gate fired (row `01a0aa61`)

`task_events` for `01a0aa61-9e26-7031-8b8e-69f3cc2bb168` (`feat/chat-m2-streaming`,
completed 2026-09-16 22:31:50):

```
event: completed-unclean
detail: run 01a0aa61-… closed out with its OWN untracked residue in
        /home/user/workspace/gopi-chat-m2: worktree is DIRTY (untracked-only residue,
        no tracked change) … entries: ?? .evid/bg2-A2.png | ?? .evid/… (≈75 entries)
```

Emitter: `internal/tools/task_reclaim.go:1130` (`reclaimRunResidueNote`, event value
`taskStatusCompleted + "-unclean"`). This is the *only* `%unclean%` row in the whole
ledger — and it is one of the three rows under investigation.

Ledger row (deliverable carries the branch + worktree):

```
task_id 01a0aa61-9e26-7031-8b8e-69f3cc2bb168 | completed | 2026-09-16 21:21:59 → 22:31:50
{"branch":"feat/chat-m2-streaming","repo":"/home/user/work/GoPi","worktree":"/home/user/workspace/gopi-chat-m2"}
```

So at the instant of close-out the worktree **was dirty (untracked-only)** — exactly
the state that makes the chain of §2.2 return before the push.

### 2.4 HP's log surface is dominated by that same skip

```
$ grep -rho "worktree dirty (live bond protected)…" ~/.gopi/**/*.jsonl | sort | uniq -c
   2873 worktree dirty (live bond protected) —
     66 worktree dirty (live bond protected) — tracked changes: …
     66 worktree dirty (live bond protected) — carries ONLY UNTRACKED files …
     22 worktree dirty (live bond protected) — ?? wip.txt
```

Corroboration, **not** per-row proof: it shows the skip is the ordinary, routine
outcome on this host, not a rare accident.

### 2.5 Hypothesis C — stale HP binary: ELIMINATED

```
$ ls -l bin/gopi          → 2026-09-16 21:01:28
$ git reflog show master  → 69975030 master@{2026-09-16 20:59:19}: pull --ff-only
$ ./bin/gopi version      → v20260916-1304-69975030
$ git log -1 master       → 69975030 (2026-09-16 20:48:05)
```

The binary was built at 21:01, i.e. **after** master reached its tip at 20:59, and it
self-reports that tip. It contains the current client-arm vocabulary
(`live bond protected` ×3, `carries ONLY UNTRACKED files`, `to recycle (main checkout /
already gone)`). The client arm itself is old (`finishCleanupServerTarget` dates to
`8f7e824a`, 2026-08-31). **C is not the cause.**

*Measured limitation, honestly:* the resident `gopi worker` (pid 2928345, started
17:14:45) holds a **deleted** binary:
`/proc/2928345/exe → /home/user/work/GoPi/bin/gopi (deleted)`, exe inode 27697131 vs
`bin/gopi` inode 29521088. So the worker's code is *older than* 21:01. It still
contains the dirty-split constants (`carries ONLY UNTRACKED files` present), so the
worker is not stale for *this* question — but I could not date the worker's exact
commit, and the deleted-binary state is itself a finding (it is a different epic's
subject: the worker keeps running the code it started with).

### 2.6 The close-out path that ran for these rows: **server-recycle, not the push chain**

At completion, all three deliveries ended the same way — **branch survives, worktree
gone**:

```
$ for w in gopi-chat-m2 gopi-cleanup-singlenode gopi-mscount; do test -d ~/workspace/$w && echo EXISTS || echo GONE; done
GONE / GONE / GONE        # while all three branches still exist locally and on peer1
```

That is precisely the `finishCleanupServerRecycle` outcome (`:821`) — worktree removed,
branch **preserved**, **no push step in that arm**. Combined with §2.3's dirty event,
the chain for these rows classified as server/single-node or skipped before its push.

### 2.7 Reproduced today — the skip is *invisible* (Rule 15)

```
$ ./bin/gopi task finish-cleanup --dry-run
  01a0aa61-… completed  branch=feat/chat-m2-streaming  worktree=/home/user/workspace/gopi-chat-m2
            — git status: chdir /home/user/workspace/gopi-chat-m2: no such file or directory
  01a0aa74-… completed  branch=fix/merger-status-branch-account  worktree=/home/user/workspace/gopi-mscount
            — git status: chdir /home/user/workspace/gopi-mscount: no such file or directory
  … (10 candidates)
finish-cleanup (dry-run): candidates=10 would-run=0 settled=9 refused=1
```

The reported reason for a row whose branch was **never delivered** is an opaque
`chdir … no such file or directory` — it names a *directory*, never the *condition*
("branch not delivered to peer1"). An operator reading this cannot tell a delivered
branch from a stranded one. This is memory #1776's predicted "misleading hygiene
refusal" reproduced live.

### 2.8 The resident worker's OWN log — the sweep ran, and re-skipped in silence

The resident `gopi worker` (pid 2928345) was launched with its stdout redirected, so its
own log exists and is readable — `/proc/2928345/fd/1 → /home/user/workspace/peer-worker-2.log`
(54 KB, 1019 lines):

```
$ grep -n 'finish-cleanup skipped:' ~/workspace/peer-worker-2.log
167:task 01a0aa0e-88ca-7625-9943-d73c68a09d28: finish-cleanup skipped:
     git status: chdir /home/user/workspace/gopi-cleanup-singlenode: no such file or directory
471:task 01a0aa61-9e26-7031-8b8e-69f3cc2bb168: finish-cleanup skipped:
     worktree dirty (live bond protected) — carries ONLY UNTRACKED files …
```

Three things this closes and one it opens:

1. **Row `01a0aa61` is now proven at TWO layers** — the ledger event (§2.3) *and* the
   chain's own refusal line, printed by the resumed run's close-out path
   (`cmd/agent/cli_tasks.go:1830`, `finish-cleanup skipped: <reason>`). The dirty gate of
   §2.2 fired on the real close-out of a real row under investigation.
2. **Row `01a0aa0e` (`fix/finish-cleanup-single-node-wording`) has a direct line — and a
   *different* cause.** It is **not** the dirty gate: the chain stopped at the very first
   step because the recorded worktree directory was **already gone**
   (`chdir /home/user/workspace/gopi-cleanup-singlenode: no such file or directory` —
   the `git status` failure branch, `task_finish_cleanup.go:995–999`). A close-out that
   runs only *after* the worktree is gone can never push: it resolves the row's
   `worktree` field, cannot `chdir` into it, and returns `Skipped` before step ②.
   **This is a second, independent way the same branch is stranded**, and it explains why
   `git status: chdir …` is the wording in §2.7's live `--dry-run` today.
3. **The resident finish-cleanup sweep DID run — and re-skipped the same rows in
   silence.** The worker's lifecycle tick fires `serveFinishCleanupSweep` every 10 min
   (`cli_tasks_worker.go:326` → `task_lifecycle_tick.go:264`). The sweep **is** a periodic
   client pass, and it re-examined these rows. But it printed **nothing** for them:

```
$ grep -c '\[finish-cleanup\]' ~/workspace/peer-worker-2.log
0            # the sweep emitted zero lines while the per-row refusals above exist
```

   The sweep's report filter (`cli_tasks.go:3013`) prints a skip only when
   `FinishCleanupSkipIsReportable` (owner-mismatch / owner-stale / in-use —
   `task_finish_cleanup.go:676`) **or** `TaskFinishCleanupSkipIsLiveOwner` ("live ledger
   row references the …" — `:2430`) matches. The dirty reason carries **neither** marker
   (it is `finishCleanupDirtyUntrackedPrefix`, `:610`), so **a dirty skip is silently
   dropped by the very pass that exists to catch stranded rows**. The safety net
   re-attempted and re-failed without a word.

4. **`01a0aa74` (`fix/merger-status-branch-account`) — no attributable line, but a
   mechanism is on record.** The chain never names that row (grep: 0 hits), and the
   22:30:02 peer1 reflog `push` (§2.1) means a push *did* happen for its branch — so the
   close-out leg may have run for it after all, and the delivery is explained by the
   hand-push only insofar as the *later* hand-push re-sent the same tip. Separately, the
   log does record a **foreign-worktree release** performed by *another* row's run:

```
471/719: task 01a0aa61-…: own-worktree-release: own registered worktree freed:
         /home/user/workspace/gopi-mscount (a worktree of this run's repo that no ledger
         row owned; branch fix/merger-status-branch-account untouched)
```

   `gopi-mscount` **is** `01a0aa74`'s recorded worktree (§2.3 mapping). A *different*
   row's run removed it as "a worktree … that no ledger row owned", explicitly keeping the
   branch and running **no push leg** (`reclaimSelfOwnFreedMarker`,
   `internal/tools/task_reclaim_self.go:389`; emitter found by its marker). Once the
   worktree is gone, `01a0aa74`'s own close-out is in exactly the state that stranded
   `01a0aa0e` (finding 2). **Marked inferred**: I can measure the release and the gone
   worktree, but no line attributes the *delivery* decision for that row.

> **Revised root-cause statement.** The hypothesis filed for this epic — *the dirty guard
> precedes the push* — is **PROVEN for `01a0aa61`** at two layers. But it is **not the
> only strand mechanism**: a row whose worktree has already been removed (by itself, by
> the residue reclaim, or by a foreign row's release) skips the chain at the `git status`
> step before any push, with no delivery record. The general defect is therefore one
> level up: **on a client host, branch delivery is a side effect of a worktree-cleanup
> chain, so every condition that stops the cleanup also silently stops the delivery.**

---

## 3. Is there a periodic client push pass? (deliverable 2)

**Answer in two halves, because they differ.**

**(a) A periodic client *finish-cleanup* pass DOES run — and it does not help.** The
worker's resident lifecycle tick fires `serveFinishCleanupSweep` every 10 minutes
(`cmd/agent/cli_tasks_worker.go:326` → `cmd/agent/task_lifecycle_tick.go:264`;
throttle `serveFinishCleanupThrottleMin = 10 * time.Minute`, `cli_tasks.go:2843`). It
re-examined the stranded rows and **re-skipped them silently** (§2.8 finding 3): a dirty
skip matches neither report predicate, so the pass whose whole purpose is to catch a
stranded close-out printed **zero lines**. It also cannot recover the second mechanism —
`serveFinishCleanupStrandedIDs` explicitly `continue`s when the worktree directory is
gone (`cli_tasks.go:3309`), so a row stranded *because* its worktree vanished is outside
the sweep's scan by construction.

**(b) A periodic *push* pass exists but never fires on a client.** `serveGitSyncSweep`
(`cmd/agent/cli_tasks_sync_git.go:57`; client side `GitSyncClientPass`,
`internal/tools/task_sync_git.go:193`) is called at **`cmd/agent/cli_tasks.go:2436`, inside
`mergerPassOnce`** (`:2354`) — the **merger** pass.
- The merger is declared **server-only** in its own doc comment (`cli_tasks.go:2290+`:
  *"the SERVER-ONLY merge daemon"*), and HP runs **no** production merger:

```
$ pgrep -a -f 'bin/gopi|gopi-merger'
2928345 ./bin/gopi worker                          ← the only resident HP loop
438268 …/suite-cmd-agent-run-…/merger-daemon-bin/gopi-merger-daemon merger --every 1h  ← TEST artifact
3830989 ~/workspace/gopi-prefix/bin/gopi chat
```

- The **worker** loop's own lifecycle passes (`runTaskWorkerLoop`,
  `runTaskWorkerLoop`, `cmd/agent/cli_tasks_worker.go:347`) include stale sweep + finish-cleanup sweep + merged
  cleanup + merge-exec — **and no git push pass**. A worker round pushes only the task
  it *runs* ("claim → run → must-succeed push").
- Cadence/throttle for the pass: `GOPI_SYNC_GIT_INTERVAL`, default 15 min — but with no
  merger process on HP, the clock never advances.
- Settings: `role=client`, `sync_to=peer1`, `sync_peers=["peer1"]` — the topology
  *permits* a push; nothing *schedules* one.

**Branch coverage — checked, and a claim I had to withdraw.** The pass's doc comment
(`task_sync_git.go:189`) says *"push local `feat/*` back to the server"*, and that prose
is **stale**: the implementation pushes **every non-master branch**, including `fix/*`.

```
internal/tools/task_sync_git_pushback.go:86  gitPushbackLoadBranches:
    git for-each-ref --format=%(refname:short) refs/heads     ← all branches
    skip master/main; stop at gitSyncMaxBranches (= 20)
$ git for-each-ref refs/heads | grep -vc -xE 'master|main'  → 10   (HP today)
```

So `fix/merger-status-branch-account` and `fix/finish-cleanup-single-node-wording`
**would** have been covered by a periodic pass — the earlier reading (from the stale
comment) was wrong and is withdrawn. Two real, smaller gaps remain:

- **The comment lies** (`feat/*` vs "all non-master branches") — a reader who trusts it
  draws exactly the wrong conclusion about `fix/*` coverage. Documentation drift, not a
  behaviour bug, but it defeated this diagnosis for several rounds.
- **`gitSyncMaxBranches = 20` is a silent cap with an arbitrary selector**: the branch
  list comes from `for-each-ref` with **no sort**, and the loop simply `break`s once 20
  names are collected. On a repo with >20 non-master branches, which branches get pushed
  is arbitrary and the rest are **never pushed, silently**. HP is at 10/20 today
  (measured), so this is latent, not the cause here.

**Implication:** the safety net is absent on a client host because it rides the **wrong
process** (the merger, which a client does not run) — not because of branch selection.
Losing the close-out push therefore loses the delivery with no backstop; and the one
component that could have backstopped it is wired to a loop HP never starts.

---

## 4. Fix design (deliverable 4)

Three changes, in dependency order.

### 4.1 Push before the dirty check (the defect)

In `taskFinishCleanupChain` (`internal/tools/task_finish_cleanup.go:963`) the dirty gate
(`:992–1012`) must stop gating the push.

- **Preferred:** move step ② above the dirty gate — push first, then run the dirty guard
  as the gate for steps ③ (`worktree remove --force`) and ④ (`branch -d`) only. The
  push is idempotent (up-to-date ⇒ exit 0) and non-destructive, so running it on a dirty
  tree cannot lose work; the guard keeps protecting exactly what it was written for.
- **Fallback if the reorder is judged too invasive for the merge window:** keep the
  order but change the *outcome* of the dirty gate — record `Skipped` for ③/④ while
  still executing ② (a per-step skip, not a whole-chain return). The chain already has
  the vocabulary (`FinishCleanupStep{OK:false, Detail:…}`).
- The same over-broad gate exists in the server arm (`:878`) — harmless there (that arm
  has no push), leave it.

### 4.2 A skipped close-out must be visible (no silent non-delivery)

Today a stranded branch reports `chdir …: no such file or directory` (§2.7), and the
resident sweep drops the dirty case entirely (§2.8 finding 3). Three repairs, the first
being a one-liner:

0. **Make the dirty skip reportable — this is the cheap, high-value fix.**
   `FinishCleanupSkipIsReportable` (`internal/tools/task_finish_cleanup.go:676`) lists
   owner-mismatch / owner-stale / in-use. Add the two dirty prefixes
   (`finishCleanupDirtyTrackedPrefix` :609, `finishCleanupDirtyUntrackedPrefix` :610) to
   that list (or fold them into the sweep's filter at `cli_tasks.go:3013`). The existing
   "routine skips stay quiet" trade-off was written when a dirty tree meant *nothing to
   do*; once dirty means *delivery lost*, silence is the defect. **A dirty skip that
   stranded a branch must be as greppable as an owner-mismatch skip.**
1. **Name the state.** When `git status` fails because the worktree is gone
   (`:995–999`), classify it as its own reason — e.g. *"worktree already gone — branch
   delivery NOT verified"* — via a named constant, not an interpolated git error
   (`finishCleanupDiagnosticLine` already exists for exactly this class of repair).
2. **Emit an event when the branch is not on the peer.** Before returning `Skipped`,
   ask the peer whether the branch tip is already there (cheap `ls-remote` on the push
   target). If it is absent, write a `task_events` row and a notify event — mirroring
   the existing, proven pattern `completed-unclean` / `reclaimRunResidueNote`
   (`internal/tools/task_reclaim.go:1130`). A branch that fails to leave the host must
   be as greppable as a worktree that fails to clean itself.

### 4.3 A periodic client safety net

0. **Make delivery independent of worktree cleanup.** The deeper defect (§2.8) is that
   *every* condition that stops the cleanup chain also stops the push — dirty tree,
   gone worktree, a foreign row's release. A push needs the **repo + branch name**, both
   of which the ledger row carries, and neither of which requires the worktree to exist.
   A delivery pass keyed on `deliverable.branch` + `refs/remotes/<peer>` (not on the
   worktree path) closes the whole class at once.
1. **Wire `serveGitSyncSweep` into a loop a client actually runs.** A client's resident
   process is the **worker** (`runTaskWorkerLoop`), so the pass belongs in the worker
   lifecycle list beside the other sweeps (keeping its 15-min throttle +
   `GOPI_SYNC_GIT_INTERVAL`). Alternative (more config surface, not preferred): require
   a merger daemon on the client.
2. **Stop `serveFinishCleanupStrandedIDs` skipping gone-worktree rows** as
   undelivered-by-construction (`cli_tasks.go:3309`) — a gone worktree is precisely the
   signature of the strand, not a reason to look away.
3. **Fix the stale comment + the silent cap.** `GitSyncPushLocalBranches` pushes all
   non-master branches (verified in §3) while its doc comment claims `feat/*` — correct
   the comment. And `gitSyncMaxBranches = 20` truncates an **unsorted** `for-each-ref`
   list with a silent `break`: at >20 branches, which ones get pushed is arbitrary and
   the rest are dropped without a word. Order the list deterministically (deliverables
   first) and **report** the truncation instead of `break`ing quietly (Rule 15).
4. **Reconcile against the ledger.** A safety-net push has no idea which branches *owe*
   a delivery; a pass that diffs `task_events`/`deliverable.branch` against the peer's
   `refs/heads/*` would close the loop that §4.2 only reports.

### 4.4 Proposed follow-up task title

> **"client close-out: deliver the branch independently of worktree cleanup (push before
> the dirty guard, report every stranded skip, worker-side periodic push pass)"**

(Absorbs the already-filed `01a0aab8` — *"client push must not be gated by the
worktree dirty guard"* — which is §4.1, and this epic's §4.2.0, the one-line version an
operator asked for.)

**DISPATCHED 2026-09-16** as task **`01a0aaef-5a90-7e02-9276-471d61b8f565`**
(`bug,system-layer,run-on-peer1`, repo `/home/user/work/GoPi`); the row carries the whole
file:line chain, the one-liner, the regression-test requirements and the acceptance
criteria, so it needs no further briefing.

**Why the one-liner (§4.2.0) was not landed from this host:** it is genuinely two lines,
and it would have been permitted as a one-liner — but this diagnosis ran on the **client**
(HP), and Rule 28 assigns the close-out/cleanup path to the **system layer (peer1)**.
Landed from here it would have been the exact two-hosts-one-layer edit the split exists to
prevent. It is therefore handed over as the **first, cheapest unit** of `01a0aaef`.

*Layer note (Rule 28):* §4.1/§4.2 are close-out semantics — **system layer (peer1)**.
§4.3's worker wiring is also a scheduling change (system layer). HP's part is the
diagnosis and the evidence; the fix belongs to the system host per the two-host split.

---

## 5. Rule 6d — deliverable declaration

- **Branch:** `docs/peer-push-diagnosis` (worktree `~/workspace/gopi-push-diagnosis`)
- **Recovery:** `git -C ~/work/GoPi show docs/peer-push-diagnosis:devdoc/peer-push-diagnosis-20260916.md`
- Follow-up dispatched: `01a0aaef-5a90-7e02-9276-471d61b8f565` (system layer, peer1).
- **Persistent in the main repo:** the worktree shares `~/work/GoPi`'s refs, so the
  commit is in the main repo — no push required for this branch to survive.
- Hand-push confirmation for the three investigated branches is §2.1 (all three
  present on peer1, SHAs matching).

## 6. Constraint compliance

Read-only on HP: no branch, log, worktree or settings.json was modified; `bin/gopi` was
**not** rebuilt; `task finish-cleanup` was run in `--dry-run` (plan mode — by
construction it performs no push and touches nothing; it is the documented
daemon-independent read face, and it was used precisely to prove the stranded state is
still present). No device load (Rule 27). Work was done in a fresh Rule 6a worktree
(Rule 23 — no one else's failed branch was continued).
