<!-- doc-class: historical -->
# A `failed` row carries two different verdicts in one status field — measured on 30 same-shape rows

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Base: peerbox master **3a3396d06**, measured 2026-10-07 from run
`01a11488-6ab3-73e6-a2d3-9732f0be68ff`. The generator of every figure below is in that
run's session directory (`census.sql`, `census-branches.sh`, `census-landed.sh`,
`census-final.tsv`, `ownership-verdict.txt`).

## 1. The observation

A task row's `status` is ONE field, but `failed` is written there by two different
judgements:

| verdict | what it means | who decided it |
|---|---|---|
| **work failed** | the run did not produce what the briefing asked for | the worker (non-zero exit, abnormal child exit) |
| **report-form failed** | the run's deliverable exists, but its REPORT did not satisfy the gate (e.g. no literal `Ran:` line, Rule 31) | the report gate, at writeback time |

Both land as `status='failed'`, and the ledger keeps no column separating them. The
two are distinguishable only by *reading the reason text* and by measuring the ground
by hand — every downstream consumer sees just the word `failed`.

Measured on peerbox, 2026-10-07 (`census-counts.sql`, re-runnable):

```
all_failed            330
failed+agent           30        <- source='agent' (the selector's scope)
failed+deliverable<>  253        <- a stale deliverable string survives on old rows
SELECTOR (briefing)    30   status=failed AND deliverable!='' AND source='agent' AND created_at>1781000000
selector+reportform    27   ... AND the row has a task_events.detail with 'ran-line-missing'
```

So **27 of the 30** rows the selector catches were failed *for the report form*, not for
the work: the harness's own failure digest records `ran-line-missing: ## Verification no
Ran: line`. The remaining 3 failed on their own reason text.

Two figures about the shape of the measurement, because they change what the number means:

- **`deliverable!=''` is not a live-ground test.** 253 `failed` rows carry a non-empty
  `deliverable`; it is a JSON blob that survives on rows of any age. The selector's
  `created_at>1781000000` term is what narrows it to 30. A "how many failed rows still
  have a branch" question needs the ref measured, not the column read.
- **`report_bytes` is host-relative, and that is the point.** The harness report lives at
  `~/.gopi/sessions/<id>/task-report.md` on the host that RAN the row. 21 of the 30 rows
  read `1135` on peerbox and `no-branch`, i.e. the row never got its own worktree: it
  reached the report gate before ever producing a branch.

## 2. What `failed` costs downstream (each verified, not assumed)

1. **The finish-cleanup chain is skipped silently.** A run that ends `failed` never enters
   the chain, so its branch is neither merged nor pushed. Stated plainly: the ledger says
   `failed`, the branch says `work` — and the branch is what nobody acts on.
2. **Rule 33 parks every child forever.** The parent release set is exactly
   `{completed, verified}`; `failed` is not in it. A child under a failed row is
   unclaimable by design, with no override flag, no cascade and no staleness release.
   Measured on the resc() row's own parent (`01a113ac`): its child
   `01a113e4-6730-7b5b-bc35-0278c2c20b8b` reads `cancelled` — the park was resolved by a
   human cancelling the child, which is the only door Rule 33 documents.
3. **`failed` is terminal, so the row's own liveness evidence is already gone.**
   The ledger clears `proc_pid` / `proc_start_ticks` / `proc_cmd` on every non-`in_progress`
   transition (memory #5260). Measured on `01a113ac`:
   `proc_pid=(NULL) proc_start_ticks=(NULL) proc_cmd=(empty)`, while
   `claimed_by=peerbox:3577762` survives. A reader who asks the row "is a run still alive in
   my ground?" gets **nothing** — see §3.

The compounding is what makes this a structural defect rather than two cosmetic ones: the
row-level consequence (no merge) and the queue-level consequence (parked children) are both
keyed on the same word, and both are wrong when the word was written by the report gate.

## 3. The live-ownership check is not optional — `01a113ac` is held right now

The epic's premise was "the one row whose branch is still alive is the one row worth
rescuing". Measuring the ground overturned the "rescue" half of it. On peerbox,
2026-10-07 17:21–17:26 (`ownership-verdict.txt`):

```
### 1. live processes whose cwd resolves INSIDE the target worktree
  pid=2159778 cwd=.../gopi-01a113ac57927bb6950402126e224aee start=Wed Oct  7 15:21:27 2026
              cmd=/home/user/work/GoPi/bin/gopi session resume 01a113ac-5792-7bb6-9504-02126e224aee --task …
  pid=2993235 cwd=.../gopi-01a113ac57927bb6950402126e224aee cmd=make test-budget PKGS=./cmd/agent
  pid=2993501 cwd=.../gopi-01a113ac57927bb6950402126e224aee cmd=go test -count=1 -timeout 25m -json ./cmd/agent
  pid=2993708 cwd=.../gopi-01a113ac57927bb6950402126e224aee/cmd/agent cmd=…/agent.test …
### 2. branch HEADs
  worktree HEAD : c4bc7b60c 2026-10-07 17:21:15 +0800 revert(config): screen-capture's positional address …
  commits ahead of master: 4      ancestor of master? NO      pushed to hp? (empty answer)
```

Five measurements, one conclusion, and each one is load-bearing:

- A run **resumed the row** 2h before this task began (`gopi worker --resume --branch
  task/01a113ac…`, pid 2159756, started 15:21:27) and is **still writing** — it committed
  at 16:54, merged master at 16:57, and reverted at **17:21:15**, four minutes before the
  process scan found `go test ./cmd/agent` running with its cwd inside the worktree.
- Its report does **not** have the defect this resc() family is about: the branch's
  `tasks/01a113ac-report.md` is 10511 bytes with `## 1.`…`## 7. Not Done`, and carries a
  literal `**Ran:**` line. The row's own `task-report.md` is still the 1140-byte harness
  skeleton — because the run has not finished.
- `claimed_by: peerbox:3577762` is live (`gopi worker` pid 3577762), so the row is claimed.

**Therefore the correct deliverable for this row is evidence, not a commit.** Writing a
`Ran:` line into that branch would mean committing on top of a live writer's HEAD, in the
one ground Rule 29 protects and that memories #3993/#5260 record as mutually destructive
when two writers share it. The verdict is: **held by a live run — this run does not write.**

## 4. Which rows are rescuable, and which must be redone

The census (`census-final.tsv`) gives the criteria as measurable states, not opinions:

| state | meaning | verdict |
|---|---|---|
| `BRANCH_LIVE` **and no live holder** | the ref resolves and nothing is writing in its ground | **rescuable** — fix the report form on the branch, commit, push |
| `BRANCH_LIVE` **with a live holder** | the ref resolves and a process's cwd is inside the ground | **not rescuable by a second run** — evidence only (§3) |
| `branch-gone` | the row recorded `deliverable.branch` and the ref no longer resolves | **redo** — the deliverable is destroyed |
| `no-branch` | the row never produced a branch (it reached the report gate first) | **redo** if the work still matters |

Measured distribution over the 30-row selector: **1** `BRANCH_LIVE` (and it is held by a
live run), **15** `branch-gone`, **14** `no-branch`. Not one row already has its work in
master: `git log master --grep=<uuid>` / `--grep=<hex>` matched for exactly **one** of the
30 (`01a0f522`, 2 hex matches) — for every other row, "the branch says work, the ledger
says failed" means the work is genuinely not in master.

The `no-branch` bucket deserves the blunt reading: those rows did not fail *after*
delivering. They reached the report gate before producing a worktree at all, so their
`failed` is a verdict about a run that never reached the point of having something to
merge. **A resc() pass cannot repair them, and should not be dispatched at them.**

## 5. What this run deliberately did not do

- **No write to any other row's status.** Not one `task update` was issued. In particular
  `01a113ac` stays `failed`: its live run owns that transition, and a second writer setting
  it to `completed` while the run is still testing would be the duplicate-dispatch failure
  (#3993) with a ledger write attached.
- **No code change.** The observation above is about where a word is written; changing the
  writeback, or the Rule 33 release set, is a system-layer change for peerbox and needs its
  own task with its own red proof.
- **No branch in this repo was merged, and none was pushed.**
