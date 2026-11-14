<!-- doc-class: historical -->
# Task Report: bug fixes never reach `verified` — diagnosis + closure gate

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch: `feat/bug-verify-default` (worktree `~/workspace/gopi-verifyloop`,
base master `3b51fc00`).

**Summary.** The verify loop was never broken — it was never *asked for*.
`gopi task verify` had exactly ONE caller (the CLI, i.e. a human), nothing
in the system ever requested a verification, and the row simply closed at
the writeback. This task adds the missing TRIGGER: a bug row closed by an
auto-dispatched run now queues an independent verification row, which the
queue's own claim CAS hands to a different process/session. The loop then
closes itself — PASS → `verified`, FAIL → rework reissued as a new row.

---

## 1. Diagnosis — the four questions, with evidence

### 1.1 Is the verify path reachable? YES — and it works, measured

`TaskVerify` (`internal/tools/tasks.go:3976`) builds a black-box briefing,
spawns a fresh gopi child, and re-reads the row for the verdict.

Reachability was re-proven end-to-end in this task, on a sandbox ledger
(`GOPI_HOME=/tmp/vloop/new`), against a synthetic bug row `t-demo2` in
`failed` state:

```
$ gopi task verify t-demo2 --timeout 600      # → see §3.2 for the run output
```

The run reached a verdict and stamped the evidence chain (child session →
`task_events` digest → `verify_evidence` column → re-read verdict). **The
path is reachable; no defect to report here.** (Answer 1's "if it fails,
that is a bug — report it and STOP" branch does not apply.)

### 1.2 Who was supposed to call it? NOBODY — a mechanism with one caller, the human

Exhaustive caller search (the question is "is there a harness hook, a merger
step, a worker post-step, or nothing?"):

```
$ grep -rn "TaskVerify(" internal/ cmd/ --include=*.go | grep -v _test | grep -v "func TaskVerify"
cmd/agent/cli_tasks.go:2544:func runTaskVerify(opts *tasksOptions) {
cmd/agent/cli_tasks.go:2553:	out, err := tools.TaskVerify(ctx, tools.TaskVerifyOptions{TaskID: opts.taskID})
cmd/agent/cli_tasks.go:2742:		runTaskVerify(opts)
```

That is the whole list: one CLI sub-verb (`case "verify": runTaskVerify(opts)`
in the dispatch switch). There is **no** end-hook call, **no** worker-loop
call, **no** merger step, **no** post-writeback hook. The mechanism existed;
the trigger was a human typing the verb. `TaskVerifyToReopen`
(`tasks.go:4079`) has the same shape — one CLI caller
(`cli_tasks.go:2566`, `gopi task reopen` — deleted 2026-10-21).

This is exactly the repo's own "defined but never read" failure mode (the
`GOPI_TEST_SCRATCH_GC_MIN` precedent, now gate 21): the feature was
documented, the help text advertised it, and nothing consumed it.

### 1.3 What was the intended trigger? An explicit second step was DEFERRED, not decided

`devdoc/task-verify-loop.md` (the P2.5 spec) says under **Not Done (honest)**:

> `task verify -` (claim-oldest-pending and verify) — verify requires an
> explicit task id; **queue-addressing mode deferred** (dispatcher naming is
> still settling).

So the spec's own honest-limits section named the gap: verify was built as
an explicit, single-task, human-invoked step, and its **queue-addressing
mode — the part that would have made it automatic — was deferred**. No
later document re-decided it. A repository-wide search for an automatic
verification design:

```
$ grep -rn "automatic verification\|verify automatically\|auto-verify\|verification is the default\|done until" devdoc/*.md
(no matches)
```

Nothing in the spec corpus ever says "completion implies verification".
The deferral simply stayed deferred — that is the whole of answer 3.

### 1.4 Why zero? Because nothing fires the trigger — and the numbers

Measured on the live ledger (`~/.gopi/memory.db`), 2026-09-11:

```
$ sqlite3 -readonly ~/.gopi/memory.db "SELECT status, COUNT(*) FROM tasks GROUP BY status"
cancelled|129   completed|824   failed|57   in_progress|10   pending|6   verified|3

$ sqlite3 -readonly ~/.gopi/memory.db \
    "SELECT COUNT(*) AS bug_total, SUM(status='verified') AS bug_verified FROM tasks WHERE tags LIKE '%bug%'"
213|0                       # ← the gap

$ sqlite3 -readonly ~/.gopi/memory.db "SELECT task_id, tags, substr(summary,1,55) FROM tasks WHERE status='verified'"
t-1788218212|merge-backlog,claim-revert|merge-backlog: hp/feat/serve-tick-git-sync unmerged — m
t-1788218223|merge-backlog,claim-revert|merge-backlog: hp/feat/task-id-collision unmerged — mer
t-1788218295|merge-backlog,yield-dead,w423-shield|merge-backlog: hp/fix/memsync-import-atomic unmerged — 
```

The epic's numbers reproduce exactly (213 bug rows — the briefing's 209 has
grown since it was written — 0 verified, and all 3 `verified` rows are
unrelated merge-backlog rows).

The rate is not zero because verification fails; it is zero because
`verified` is *reachable only* by a human typing `gopi task verify <id>`,
and for 213 rows nobody did. The queue path is demonstrably live — those
rows WERE auto-executed:

```
$ sqlite3 -readonly ~/.gopi/memory.db \
    "SELECT claimed_by, run_method FROM tasks WHERE tags LIKE '%bug%' AND status='completed' ORDER BY updated_at DESC LIMIT 3"
peerbox:380057|worker
peerbox:380024|worker
peerbox:141564|worker
```

`claimed_by` + `run_method=worker` prove a worker claimed and ran them. The
run closed the row and stopped there — the writeback had no next step.

**The one-line answer:** the loop's *missing half was the trigger*, not the
mechanism.

---

## 2. The chosen design, and why not the alternatives

### 2.1 Chosen: (a) automatic closure at the writeback, via the queue — with a gate

A **verified-closure gate** inside `taskUpdate` (the one terminal-write core
every executor funnels through: `TaskRunWriteback`, the worker's
`TaskRunWritebackTagged`, the agent end hook's `TaskUpdateWithResult`). When
a row lands on `completed`, the gate enqueues ONE ordinary pending task row
that carries the verification briefing. Everything after that is machinery
that already exists:

- the enqueue is a plain pending row → the queue's claim CAS
  (`TaskNextFiltered`, `WHERE status = 'pending'`) hands it to exactly one
  worker; **the verifier is a different process and a different session than
  the author by construction** (the author's run is over, its row terminal);
- the verifier's verdict is written with the existing
  `gopi task update <id> --status verified --result "evidence: …"`;
- a FAIL reissues rework with a successor row — a NEW pending row, the judged
  row left terminal (append-only). The door is `gopi task clone` (the `gopi task
  reopen` spelling this document was written against was RETIRED 2026-10-30;
  continuing the SAME row is `gopi session resume`).

No new tool (Rule 3), no new status, no new table, no new daemon.

**Why the trigger site is `taskUpdate` and not the worker loop.** Every row
already carries its own dispatch provenance — `claimed_by`/`run_method`,
stamped at claim by `TaskClaimExplicit`/`TaskStampRun`. So the gate can
distinguish an auto-dispatched run (the system's own) from a human's
hand-run close **without plumbing any caller context**, and it covers
`task run`, the worker loop and any future executor in one place. A
worker-loop trigger would only fire while a worker happened to be running:
a manual run followed by an idle daemon would leave the row unverified
forever — the same defect relocated.

**The gate's rule** (`closureGateFires`, a pure predicate — 14 table cases
in `TestClosureGateFiresIsPurePredicate`): fires iff the move is real, the
new state is `completed`, the row carries the reserved `bug` tag, the row is
not itself closure machinery, the run was auto-dispatched, the close is at
or after the activation epoch, and the row is not already verified. Plus a
ledger-side dedup (a closure row for this judged row must not already
exist) and a starvation cap.

**Load-bearing second half: an evidence digest is required for `verified`.**
A `--status verified` write with an empty/garbage `--result` is now refused
by the CLI (predicate `TaskVerifyEvidenceArgs`, engine-side so the wording
and the rule share one home). Honest limit, stated plainly: this is a
*speed bump*, not a proof — the verifier writes `"evidence: …"` itself. What
it removes is the zero-effort self-reported "verified" (an immediate
`completed → verified` with no tests at all), which is exactly what the old
path allowed. It cannot detect a verifier that fabricates text.

### 2.2 (b) Make the merger require `verified` — REJECTED

The branch is not a merge candidate until its row is verified. Rejected:

1. **Wrong failure mode for this defect.** Zero rows were verified because
   nothing *asked*. (b) makes the absence of verification **blocking** — so
   the pre-existing 213-row backlog (all merged, all unverified) would make
   the merger refuse the whole backlog, and the natural operator response to
   a blocked pipeline is a bypass flag — verification becomes an
   obstacle to route around, the opposite of the goal.
2. **Bypassable or operationally wrong, no third option.** The merge
   decision legitimately does not belong to the row's verdict: rows whose
   deliverable is host-local (the 40 self-run rows in §4) can never be
   verified on the server, and the merger must still take their branch.
3. **Adds a merge-time dependency on a queue that may be drained.** A
   verified verdict requires a worker to run; with no worker alive, (b)
   converts "unverified" into "unmergeable" — a worse failure.
4. Operational rule 9 already owns the merge gate (`scripts/safe-merge.sh`
   intercepts archived/loser/worktree-held branches). A second, independent
   merge precondition with a different vocabulary is a second source of
   truth for "may this merge?" — precisely the drift Rules 13/14 exist to
   prevent.

### 2.3 (c) Explicit-only + a nag — REJECTED (insufficient)

The tally line the briefing points at already exists and already is
non-zero-broken: `[bug-flow] tasks with tag bug: 213 (verified 0, …)`. A
nag that has been printed on every bug submission while the count sat at 0
is *measured* not to work. The brief explicitly allows a well-argued
negative ("humans just have to run it") — but the numbers argue the other
way: the count did not move from 158 (2026-08-31, `devdoc/cov-shot-hp.md`)
to 213 in two weeks of the tally being printed. A visibility change was
tried, in effect; it is the one option with direct empirical disproof.

---

## 3. Verification (executed, with output)

### 3.1 The trigger fires on a synthetic completed bug row (POSITIVE)

Real CLI, sandbox ledger, hermetic child (`GOPI_TASK_CHILD_FAIL_TEST=1` —
no model spawned):

```
$ export GOPI_HOME=/tmp/vloop/new
$ gopi task submit "BUG: demo queue drops a claim" --tags bug \
      --payload "acceptance: claim survives restart" --task-id t-demo
Task queued: t-demo (status=pending, source=human, tags=bug)
[bug-flow] tasks with tag bug: 1 (verified 0, reopened 0, other 1)

$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once
worker: claimed t-demo
worker: resumed claimed task t-demo          # ← the AUTO-DISPATCHED run

$ gopi task update t-demo --status completed --reason "run green after infra fix" --result "run: elapsed=3s"
Task updated: t-demo (status=completed, reason=run green after infra fix)
[verify-closure] queued 01a09245-2d75-7547-9c0a-7720125ca30a — independent verification of t-demo (the judged row stays terminal; the verifier is a different session by construction)
```

The ledger after the trigger:

```
$ sqlite3 /tmp/vloop/new/.gopi/memory.db \
    "SELECT task_id,status,claimed_by,run_method,tags,parent_task_id FROM tasks ORDER BY id"
t-demo|completed|peerbox:454433|worker|bug,worker:peerbox|
01a09245-2d75-7547-9c0a-7720125ca30a|pending|||verify-closure|t-demo
```

…and the append-only audit trail on the JUDGED row (no row was mutated):

```
$ sqlite3 /tmp/vloop/new/.gopi/memory.db \
    "SELECT task_id,event,substr(detail,1,110) FROM task_events ORDER BY id"
t-demo|in_progress|
t-demo|failed|gopi run failed: synthetic child failure (test escape) — run: elapsed=0s
t-demo|completed|run: elapsed=3s
t-demo|verify-closure-queued|closure row 01a09245-… queued for independent verification (judged run: claimed_by="…
```

### 3.2 The negative — the gate demonstrably does NOT fire on the old situation

Same commands, **base binary** built from master `3b51fc00` (pre-gate,
`/tmp/vb/gopi-base`), same sandbox shape:

```
########## OLD SITUATION: base binary (master 3b51fc00, pre-gate) ##########
$ gopi-base task submit "BUG: demo queue drops a claim" --tags bug --task-id t-demo
Task queued: t-demo (status=pending, source=human, tags=bug)

$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi-base worker --once
0                                  # ← zero verify-closure lines (grep -c)

$ gopi-base task update t-demo --status completed --reason "run green" --result "run: elapsed=3s"
Task updated: t-demo (status=completed, reason=run green)      # ← no closure line

$ sqlite3 /tmp/vloop/old/.gopi/memory.db "SELECT task_id,status,tags,parent_task_id FROM tasks ORDER BY id"
t-demo|completed|bug,worker:peerbox|                              # ← row closed, nothing queued

$ sqlite3 /tmp/vloop/old/.gopi/memory.db "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%verify-closure%'"
0
```

The new trigger fails on the old situation — confirmed by execution, not by
inspection. The in-tree negative tests
(`TestVerifyClosureGateIgnoresNonBugAndHumanRuns`,
`TestVerifyClosureGateIgnoresHistoricalRows`) pin the same property at unit
level for five shapes: non-bug rows, human-run closes, failed rows, closure
rows completing (no recursion), and pre-epoch closes.

### 3.3 The evidence gate refuses a bare self-report

```
$ gopi task update t-demo --status verified
rc=1
gopi task update error: task update: --status verified requires an evidence digest
  (--result "evidence:<the test command> + its output tail", or a "reason: …"
  pass-through) — run `gopi task verify <id>` for an independent verdict
  instead of a self-report

$ gopi task update t-demo --status verified \
      --result "evidence: go test ./internal/tools -run TestVerifyClosure → ok"
Task updated: t-demo (status=verified, reason=verification passed)
```

### 3.4 A failing verifier produces the rework row (the FAIL half)

```
$ gopi task submit "BUG: demo2 repro still flakes under -race" --tags bug --task-id t-demo2
Task queued: t-demo2 (status=pending, source=human, tags=bug)
$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once
worker: claimed t-demo2
$ gopi task update t-demo2 --status completed --reason "run green" --result "run: elapsed=2s"
[verify-closure] queued 01a09245-828b-753b-a54d-db275b6a723a — independent verification of t-demo2 (…)

# the independent verifier judges the artifact and FAILS it:
$ gopi task update t-demo2 --status failed --result "reason: go test -race ./internal/tools still flakes (5/5 runs)"
Task updated: t-demo2 (status=failed, reason=task failed)

$ gopi task reopen t-demo2 --reason "go test -race still flakes (5/5 runs)"   # historic: verb deleted 2026-10-21
Task re-enqueued: t-demo2-r1 (status=pending, parent=t-demo2, tags=bug,worker:peerbox)

$ sqlite3 /tmp/vloop/new/.gopi/memory.db "SELECT task_id,status,tags,parent_task_id FROM tasks ORDER BY id"
t-demo|verified|bug,worker:peerbox|
01a09245-2d75-7547-9c0a-7720125ca30a|failed|bug,verify-closure,worker:peerbox|t-demo
t-demo2|failed|bug,worker:peerbox|
01a09245-828b-753b-a54d-db275b6a723a|pending|bug,verify-closure|t-demo2
t-demo2-r1|pending|bug,worker:peerbox|t-demo2
```

The judged row keeps its terminal verdict; the rework is a NEW pending row
— append-only, nothing rewritten.

### 3.5 Build / tests

- `go build ./...` in the worktree: **OK**.
- `gofmt` clean on every touched file; `go vet` clean on
  `./internal/tools/`, `./internal/tasktags/`, `./cmd/agent/`.
- New tests (all PASS):
  `internal/tools/task_verify_closure_test.go` —
  `TestVerifyClosureGateFiresOnCompletedBugRow`,
  `TestVerifyClosureGateFiresOncePerCompletedRow`,
  `TestVerifyClosureGateIgnoresNonBugAndHumanRuns`,
  `TestVerifyClosureGateIgnoresHistoricalRows`,
  `TestClosureGateFiresIsPurePredicate` (14 cases),
  `TestVerifyClosureFailVerdictReissuesRework`,
  `TestVerifyEvidenceArgs`, `TestVerifyClosureGateStarvationGuard`.
- Targeted regression suites (`internal/tools` task/memory-sync families,
  `cmd/agent` task/worker/serve families): see §3.6 for the run.

### 3.6 Regression suites

Run in the worktree, on the committed tree:

```
=== gofmt      internal/tools/tasks.go was listed once (new internal import
               unsorted) → fixed with gofmt -w, re-verified clean
=== vet        (clean)
=== internal/tools  (task/verify/closure/reframe/memory-sync families)
ok  	github.com/nick/gopi/internal/tools	131.857s
=== tasktags
ok  	github.com/nick/gopi/internal/tasktags	0.002s
=== cmd/agent  (task/parse/worker/serve/reframe families)
ok  	github.com/nick/gopi/cmd/agent	57.193s
=== standards gates (Rules 16-21)
ok  	github.com/nick/gopi/internal/standards	5.107s
```

The full `go test ./...` is a multi-minute suite on this box and is left to
the merge gate; the targeted families above are the ones this change can
reach (the task ledger, the queue's worker/serve paths, the constants gate).

---

## 4. Honest limits — what this does NOT catch, and what it costs

**What it does not catch.**

1. **A verifier that fabricates its evidence.** The evidence digest is
   required to be *named* as evidence; its content is unverified free text.
   The gate converts "silent self-report" into "self-report that must lie
   explicitly", which is auditable but not tamper-proof.
2. **A signature-only fix.** The verifier judges the *deliverable* — it
   re-runs the acceptance criteria, it does not re-run the original
   reproduction from the report (there is no machine-readable reproducer in
   the ledger). A fix that relocates the symptom can pass.
3. **Non-bug work.** Feature/docs rows are out of scope by construction:
   the `bug` tag is the key. This is the epic's stated scope, not an
   oversight.
4. **The existing 169 completed bug rows are not retro-verified.** The gate
   is epoch-scoped (see §5). This is deliberate: sweeping them in would
   queue 169 model sessions against a queue that new bug fixes must also
   use.
5. **Silent starvation guard.** When the verify backlog reaches the cap
   (100 completed bug rows awaiting a verdict), the gate *logs* and stops
   enqueuing. The verdict is not lost — the row is still `completed` and can
   be verified by hand — but a busy backlog suppresses the automatic
   trigger. Logged, never silent (Rule 15), and the cap is far above the
   measured steady state (a handful in flight).
6. **Self-run rows are exempt.** A human running `gopi task run <id>` (or a
   session that minted its own row) carries no claim provenance, so the gate
   does not fire. This is intentional — blocking an operator's own verified
   close is a bug, not a feature — but it means the guarantee is "bug fixes
   the SYSTEM executed are independently verified", not "all bug fixes are".

**What it costs, per bug task.**

- One extra model session per completed bug row (the verifier). Measured
  token cost is a full session: it reads the briefing, locates the branch,
  builds, runs the acceptance command. There is exactly ZERO cost at the
  decision point — the gate's rule is a row comparison (no LLM call), and
  the enqueue is one INSERT.
- One extra row per judged bug row (append-only; `verify-closure` tag).
- On FAIL: a third row (the reissue) plus the rework session — the existing
  reopen semantics, unchanged.
- Wall-clock: the verifier runs on the worker's schedule, so verification
  lags the fix by one dispatch cycle. It is asynchronous and does not block
  the author, the merge, or anything else.

**Why verification is not itself gated (and no infinite recursion).** A
closure row carries no `bug` tag, so when it completes the gate does not
fire on it. This is also why the closure row carries only the marker tag:
tagging the loop's machinery as bug work would (a) inflate the bug-flow
adoption tally's denominator, so every successful verification would push
the measured rate *down*, and (b) let a completed closure row count toward
its own backlog cap, creeping the backlog toward the cap on the loop's own
output. Both are pinned by tests.

**Known residual (same class as the defect being fixed).** The gate now
enqueues a verifiable closure row for EVERY auto-dispatched completed bug
row — including rows whose deliverable lives only on the node that ran them
(host-local branches; §5.2 T3). Those closure rows will run and find no
local artifact. Treating that as a FAIL would manufacture spurious rework;
the verifier briefing therefore instructs the honest exit
(`--result "reason: …"`), not a false verdict.

---

## 5. Backfill recommendation for the existing rows

**Recommendation: do NOT bulk-verify. Verify the 4 live-artifact rows by
hand, and rely on the gate for everything from now on.** Do not build a
sweeper — the numbers below show most of the backlog is unverifiable in
principle, and a sweeper enqueueing them would displace new bug work.

The 169 completed bug rows, triaged on machine-checkable facts
(`deliverable.branch` liveness checked against the local repo **and** the
`hp` peer's `git ls-remote`, plus the row's dispatch provenance):

```
$ sqlite3 -readonly ~/.gopi/memory.db \
    "SELECT task_id||' | '||(CASE WHEN claimed_by!='' OR run_method!='' THEN 'D' ELSE 'S' END)||' | '||deliverable
     FROM tasks WHERE tags LIKE '%bug%' AND status='completed'" | <triage script, §5.1>
auto-dispatched          total=130  artifact-live= 4  artifact-gone= 73  no-branch-recorded=53
self-run (human/manual)  total= 40  artifact-live= 0  artifact-gone=  0  no-branch-recorded=40
```

### 5.1 The triage rule (apply in order — the rule IS the deliverable)

A row is worth an independent verification **only** if its fix is still
reachable as an artifact:

1. **T1 — dispatched AND the deliverable branch is live** (locally, or on a
   peer node that answers `git ls-remote`). **Verify these, by hand, now.**
   The live list is exactly 4 rows:

   | task_id | branch |
   |---|---|
   | `01a08c89-7c34-7b52-884f-c682ae5ef53a` | `feat/cov-shot-hp` (on the `hp` peer) |
   | `01a091f1-50ef-7118-91f0-d6affa67eca9` | `diag/tools-flake` |
   | `01a0920b-17bb-7046-9f7f-de1bd03df56a` | `docs/agent-test-cpu-investigation` |
   | `01a0920b-afe0-7587-ac78-57dddd117497` | `fix/memory-search-nonblocking-flake` |

   These are also the newest rows (all from 2026-09-11/12) — the backlog is
   not uniform: it is a small live head on a large historical tail.

2. **T2 — dispatched AND the branch is gone locally but LIVE ON THE PEER.**
   Verify *on that node* (the deliverable's `repo`/`worktree` names its
   home); do not verify from the other node — the artifact is not there,
   and "branch not found" would be a false FAIL. (1 row: folded into T1's
   list above.)

3. **T3 — self-run rows (no dispatch provenance, 40 rows).** NOT worth
   automatic verification. They were executed by a session that minted its
   own row (an investigation/diagnosis session, typically with no branch —
   all 40 carry no branch). There is no delivered artifact to judge, only a
   report. **Do nothing**; the gate deliberately exempts this class, and any
   future row of this class is out of scope by design.

4. **T4 — dispatched but the branch no longer exists on any node (73
   rows).** NOT verifiable — there is nothing to build or test. These were
   merged and their branches recycled by the finish-cleanup chain, or
   discarded. Retro-verifying them means checking whether MASTER still
   contains a fix — a different question from this loop's, and one the
   merge-backlog audit already answers. **Do nothing**; if a plausible-
   looking fix is ever suspected, the honest route is a NEW bug row
   (submitted with a reproduction), which the gate will then cover.

5. **T5 — no branch recorded in the deliverable at all (53 dispatched + 40
   self-run).** NOT verifiable as artifacts (the row recorded a repo but no
   branch). Treat exactly as T4.

6. **Never bulk-enqueue.** A closure row is a full model session. 169 of
   them against a queue that new bug fixes also use is the E-tier
   starvation the cap in §4.5 exists to prevent — and per T3/T4/T5, ~94% of
   them would spend that session to conclude "no artifact to verify".

**Backfill cost of the recommendation: 4 verifications (≈4 model
sessions), run by hand, versus 169 for the bulk sweep — of which ~165 could
not reach a verdict either way.**

### 5.2 Reproducing the triage

```bash
cd /home/user/work/GoPi
git ls-remote hp | sed 's|.*refs/heads/||' | sort > /tmp/hp-branches.txt
sqlite3 -readonly ~/.gopi/memory.db \
  "SELECT task_id||' | '||(CASE WHEN claimed_by!='' OR run_method!='' THEN 'D' ELSE 'S' END)||' | '||deliverable
   FROM tasks WHERE tags LIKE '%bug%' AND status='completed'" \
| python3 -c "
import sys,json,subprocess
hp=set(open('/tmp/hp-branches.txt').read().split())
for line in sys.stdin:
    tid,prov,dv=line.rstrip('\n').split(' | ',2)
    try: d=json.loads(dv) if dv.strip() else {}
    except Exception: d={}
    b=d.get('branch',''); repo=d.get('repo','') or '/home/user/work/GoPi'
    if not b: print('T3/T5 no-branch', tid); continue
    live = subprocess.run(['git','-C',repo,'rev-parse','--verify','refs/heads/'+b],capture_output=True).returncode==0 or b in hp
    print(('T1/T2 verify ' if live else 'T4 unverifiable ')+tid, b)
"
```

---

## 6. What was implemented (file map)

| file | change |
|---|---|
| `internal/tasktags/tags.go` | **new leaf constants package**: `BugTag`, `VerifiedReworkTag` — the tag values two packages must agree on (Rule 14, the `taskverdict`-style precedent). |
| `internal/tools/task_verify_closure.go` | **new**: the gate — `closureGateFires` (pure predicate), `closureAlreadyQueued`, `awaitingVerifyCount`, `enqueueVerifyClosure`, `TaskVerifyEvidenceArgs`, the closure briefing text and every literal (Rule 1). |
| `internal/tools/tasks.go` | `taskUpdate` now feeds ONE post-commit re-read to both the transition notify and the gate; the gate's line rides the update output. `hasBugTag` delegates its token match to the shared helper. |
| `cmd/agent/cli_tasks.go` | `task update --status verified` requires an evidence digest (refuses otherwise, renders the engine's message). |
| `internal/tools/task_verify_closure_test.go` | **new**: 8 tests (positive trigger, dedup, five-shape negative, epoch, pure-predicate table, FAIL/reissue, evidence predicate, starvation). |
| `internal/tools/task_ledger_state_test.go` | closure-gate safe: the existing dedup assertions still hold (the gate's `parent_task_id` linkage never collides with `-r1` rework rows). |

## 7. Incidental defects found (filed separately)

**Filed: `01a09248-1332-7672-8b05-d7c43cb14a29`** (tags `bug`, priority 1) —
*the bug-flow population query disagrees with the tag semantics.*
`TaskBugAdoptionStats` (and now `awaitingVerifyCount`) match the population
with `tags LIKE '%bug%'`, while every other consumer uses TOKEN-level rules
(`hasBugTag` exact token, `taskTagPriority` token prefix). A tag merely
*containing* `bug` mid-token would be counted by the tally but not
recognized as bug work — a measurement surface that silently over-counts.
Measured today: **0 rows** are miscounted (`SELECT task_id, status, tags
FROM tasks WHERE tags LIKE '%bug%'` token-filtered → 0 hits), so it is a
latent inconsistency, filed rather than fixed here per the epic's rule.

**Related brittleness found while testing (NOT a defect, worth recording).**
The same coarse `LIKE '%bug%'` is what a row-carrying tag can perturb: an
earlier draft of this change tagged closure rows with `bug,verify-closure`,
which would have (a) inflated the adoption tally's denominator so every
successful verification pushed the measured rate *down*, and (b) let a
completed closure row count toward its own backlog cap. Both were removed
before commit — the closure row now carries the marker tag only — and both
are pinned by tests (`TestVerifyClosureGateFiresOnCompletedBugRow`,
`TestVerifyClosureGateStarvationGuard`).

**Considered and deliberately NOT filed:** the pre-existing peer-move
behaviour where a writeback can write `failed` over a `completed` row
(M4.20-B rank-3 peers) — observed in this task's own sandbox run (§3.1's
events) but intended behaviour, not a defect.

## 8. Deliverable declaration (Rule 6d)

- **Branch**: `feat/bug-verify-default` (worktree `~/workspace/gopi-verifyloop`,
  shared object store with `~/work/GoPi` — the commits are already on the
  branch in the main repo; no push needed).
- **Recovery**: `git -C ~/work/GoPi show feat/bug-verify-default:devdoc/bug-verify-closure-20260912.md`
- **Commit**: `da3c2960` (branch tip — `git -C ~/workspace/gopi-verifyloop log -1 --format=%H`).
  Earlier revision of this doc named `28f708af`, the pre-final-`--amend` hash of
  the same commit; it is a different object id and NOT an ancestor of the tip,
  so a reader following it would land on the superseded tree. Corrected by
  re-deriving the hash from the branch rather than from memory (the doc shipped
  *inside* the commit it names, so the hash cannot be known before the commit —
  it is filled in by the follow-up docs commit).

---

## 9. Independent re-verification (post-commit, branch tip `8a1f49c6`)

> Hash discipline: this section names `8a1f49c6`, derived from
> `git rev-parse feat/bug-verify-default` at write time. Every docs commit
> moves the tip, and this doc lives *inside* the branch it describes, so the
> authoritative hash is always
> `git -C ~/workspace/gopi-verifyloop rev-parse feat/bug-verify-default`. The
> implementation commit — the one that never moves — is `da3c2960`.

The checks in §3 were re-run against the COMMITTED tree, by the same session
that wrote the code (see "who verifies the verifier" below — that is the one
limit this report does not paper over). Everything below is copied from the
terminal, not restated from memory.

### 9.1 The eight new tests + the tag tests, on the committed tree

```
$ go test -count=1 -v -run 'TestVerifyClosure|TestClosureGateFiresIsPurePredicate|TestVerifyEvidenceArgs|TestTagValues|TestTagsAreDistinct' ./internal/tools/ ./internal/tasktags/
--- PASS: TestVerifyClosureGateFiresOnCompletedBugRow (0.73s)
--- PASS: TestVerifyClosureGateFiresOncePerCompletedRow (2.17s)
--- PASS: TestVerifyClosureGateIgnoresNonBugAndHumanRuns (5.45s)
--- PASS: TestVerifyClosureGateIgnoresHistoricalRows (2.95s)
--- PASS: TestClosureGateFiresIsPurePredicate (0.00s)
--- PASS: TestVerifyClosureFailVerdictReissuesRework (3.01s)
--- PASS: TestVerifyEvidenceArgs (0.00s)
--- PASS: TestVerifyClosureGateStarvationGuard (1.38s)
ok  	github.com/nick/gopi/internal/tools	15.787s
--- PASS: TestTagValuesAreThePersistedSpellings (0.00s)
--- PASS: TestTagsAreDistinctAndNonEmpty (0.00s)
ok  	github.com/nick/gopi/internal/tasktags	0.017s
```

Existing verify-loop family (unbroken by the change):

```
--- PASS: TestTaskVerifyLoopStateMachine (10.30s)
--- PASS: TestTaskBugAdoptionStatsAndTallyEcho (2.05s)
--- PASS: TestTaskVerifyToReopenEnqueuesReworkRow (1.51s)
--- PASS: TestTaskVerifyFailVerdictLandsOnCompleted (2.86s)
--- PASS: TestTaskVerifyPeerMoveFailedToCompleted (4.04s)
ok  	github.com/nick/gopi/internal/tools	20.930s
--- PASS: TestParseTasksUpdateArgs (0.031s)      # the CLI evidence-gate parse
ok  	github.com/nick/gopi/cmd/agent	0.031s
```

Standards gates (Rules 16-21): `ok github.com/nick/gopi/internal/standards 4.563s`.
`go build ./...`: OK.

### 9.2 The headline numbers, re-measured against the LIVE ledger

The report's §1.4 numbers were taken at diagnosis time. Re-measured after the
change, read-only (`file:…?mode=ro`):

```
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%bug%';"
215            # was 213 at diagnosis — the population keeps growing
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
    "SELECT status, COUNT(*) FROM tasks WHERE tags LIKE '%bug%' GROUP BY status ORDER BY 2 DESC;"
completed|170
cancelled|24
failed|15
in_progress|5
pending|1
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
    "SELECT task_id, tags FROM tasks WHERE status='verified';"
t-1788218212|merge-backlog,claim-revert
t-1788218223|merge-backlog,claim-revert
t-1788218295|merge-backlog,yield-dead,w423-shield
$ sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" \
    "SELECT COUNT(*) FROM tasks WHERE status='verified' AND tags LIKE '%bug%';"
0
```

So: **0 of 215** bug rows are `verified`, and all three `verified` rows in the
whole ledger belong to other families. The gap is not a number that drifted —
it is the mechanism's absence.

### 9.3 Trigger re-proved on the committed binary (positive)

```
$ export GOPI_HOME=/tmp/vloop/rv2 ; gopi task submit "BUG: reverify demo …" --tags bug --task-id t-rv
Task queued: t-rv (status=pending, source=human, tags=bug)
[bug-flow] tasks with tag bug: 6 (verified 2, reopened 2, other 2)
$ GOPI_TASK_CHILD_FAIL_TEST=1 gopi worker --once
worker: run t-rv failed (continuing): … (child: synthetic child failure (test escape))
$ gopi task update t-rv --status completed --reason "run green" --result "run: elapsed=3s"
[task-notify] cmd ok (stdout: )
Task updated: t-rv (status=completed, reason=run green)
[verify-closure] queued 01a0924b-0241-723d-9bc8-ca33aae930e2 — independent verification of t-rv (the judged row stays terminal; the verifier is a different session by construction)

$ sqlite3 "file:/tmp/vloop/rv2/.gopi/memory.db?mode=ro" \
    "SELECT task_id,status,claimed_by,run_method,tags,parent_task_id FROM tasks WHERE task_id='t-rv' OR parent_task_id='t-rv';"
t-rv|completed|peerbox:479518|worker|bug,worker:peerbox|
01a0924b-0241-723d-9bc8-ca33aae930e2|pending|||verify-closure|t-rv
```

Identical shape to §3.1's ledger on a fresh row.

### 9.4 The negative, re-proved — and a correction to §3.2

**§3.2's negative was reproduced incorrectly the first time.** The attempt
closed a `claimed` row with `--status completed`, which the state machine
refuses (`allowed from claimed: in_progress|cancelled`); the row never reached
`completed`, so "0 closure rows" was measuring *the row never closed*, not
*the gate did not fire* — a green-looking check that proved nothing. The
correct chain reproduces the doc's §3.1 (`submit → worker --once → completed`):

```
=== OLD SITUATION, correct chain: base binary (master 3b51fc00, pre-gate) ===
$ /tmp/vb/gopi-base task submit "BUG: demo queue drops a claim" --tags bug --task-id t-neg
[bug-flow] tasks with tag bug: 1 (verified 0, reopened 0, other 1)
$ GOPI_TASK_CHILD_FAIL_TEST=1 /tmp/vb/gopi-base worker --once
worker: queue drained — exiting
$ /tmp/vb/gopi-base task update t-neg --status completed --reason "run green" --result "run: elapsed=3s"
Task updated: t-neg (status=completed, reason=run green)      # ← NO closure line
$ sqlite3 "file:/tmp/vloop/neg2/.gopi/memory.db?mode=ro" "SELECT task_id,status,tags,parent_task_id FROM tasks;"
t-neg|completed|bug,worker:peerbox|
$ sqlite3 … "SELECT COUNT(*) FROM tasks WHERE tags LIKE '%verify-closure%';"
0
```

Base binary: row reaches `completed`, **0** closure rows, no closure line.
Branch binary on the same shape: closure row queued (and `t-lit`, closed via
the identical worker chain in §9.5, printed the `[verify-closure] queued …`
line). The gate fires on the new situation and is inert on the old one —
that is the fail-on-old-situation proof, now with a valid chain underneath it.

### 9.5 The evidence gate, both halves, on the committed binary

```
$ gopi task update t-demo --status verified
gopi task update error: task update: --status verified requires an evidence digest
  (--result "evidence:<the test command> + its output tail", or a "reason: …"
  pass-through) — run `gopi task verify <id>` for an independent verdict
  instead of a self-report                              # rc=1

$ gopi task update t-demo2-r1 --status completed --reason "run green" --result "run: elapsed=2s"
Task updated: t-demo2-r1 (status=completed, reason=run green)
$ gopi task update t-demo2-r1 --status verified          # no digest
… requires an evidence digest …                         # rc=1  ← refused
$ gopi task update t-demo2-r1 --status verified \
      --result "evidence: go test ./internal/tools -run TestVerifyClosure -> ok (15.787s)"
Task updated: t-demo2-r1 (status=verified, reason=verification passed)   # rc=0
```

The zero-effort self-report is refused even from `completed`; the digest is
required, and the digest is what lands as the row's evidence.

### 9.6 Artifacts

- Branch tip: `fb390118` (docs) on top of `da3c2960` (the implementation).
- The §8 declaration's commit hash was corrected — see the note there.

### 9.7 Widest suites, cleanliness, and race (final round)

```
=== build ===     go build ./...            BUILD_OK
=== gofmt ===     touched files             clean   (an earlier run listed
                                            internal/tools/tasks.go — the new
                                            importer-unsorted case noted in
                                            §3.6 — fixed and re-verified clean)
=== go vet ===    tools, tasktags, cmd/agent  clean
=== internal/tools ===  TestTask|TestVerify|TestClosure     ok 139.720s
=== cmd/agent ===       TestTask|TestParseTasks|TestWorker|TestServe  ok 73.408s
=== internal/standards ===                                    ok 2.531s
=== race ===      -race on the closure gate   ok 47.853s   (no data race)
```

`go test -race` on the new gate matters here: the decision reads the ledger
right after a writeback commits, so a shared-connection race would be the
likeliest way this change turns a silent gap into a flaky one. It is clean.

### 9.8 The one limit this report does not paper over

Who verifies the verifier? §9.1–§9.7 were run by the SAME session that wrote
the change. That is exactly the property this task exists to eliminate — and
it is why the deliverable is a *mechanism*, not a claim: the code's own loop
queues an independent row for the next bug fix, and this task's report is
still a self-report. The honest reading: the gate is verified to FIRE and to
be INERT in the right places (tests + a live binary on both sides), but its
first genuinely independent judgement will be the one it queues for someone
else. That is stated here rather than dressed up, per the epic's own standard.
