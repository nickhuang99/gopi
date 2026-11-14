<!-- doc-class: historical -->
# Is the auto-registered row of a bare CLI prompt claimable? — the decision

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task** `01a0deaa-2173-7c46-a72e-bae8ac6b2e9c` · run 2026-09-27 · worktree
`/home/user/workspace/gopi-01a0deaa21737c46a72ebae8ac6b2e9c`, branch
`task/01a0deaa21737c46a72ebae8ac6b2e9c` · base `4d7cb918`

## The question

A bare `gopi` CLI run whose epic is unvalidated, non-briefing text (the epic's
repro: `gopi --gopi-prompt 1 version`) reaches `maybeRegisterSessionTask`
(`internal/agent/taskregister.go:233`) and auto-registers a real ledger row, then
runs a full agent loop against that epic. **Is such a row CLAIMABLE by the
dispatch funnel** — intended, or a defect?

## The decision

**NOT claimable — and it is intended, by construction, not by accident.** No
dispatcher can claim it as it is born, and the fix to make it claimable would
have to break one of two independently-documented invariants. Nothing is
changed in this branch; the decision is recorded and pinned by a test.

## The gates that decide it (exact source lines)

**Gate 1 — the row is never PENDING; the funnel reads PENDING only.**
These two are independent, and each alone is sufficient:

- `internal/tools/tasks.go:1640` — `TaskNewOID` **forces** the birth status:
  `opts.Status = taskStatusInProgress`, immediately before `insertTaskRow`. The
  `TaskNewOptions` a caller passes cannot influence it. The hook's own comment
  says the same thing in words: *"the row is inserted directly in_progress with
  NO claim"* (`internal/agent/taskregister.go`).
- `internal/tools/tasks.go:5543–5544` — the claim funnel's selection is
  `SELECT … FROM tasks WHERE status = ?` with `args := []any{taskStatusPending}`.
  A row that is not `pending` is not in the result set at all.

**Gate 2 — the row carries no payload, and the shared predicate requires one.**

- `taskRowClaimable` (`internal/tools/tasks.go:5848`) returns `false` for any row
  whose payload is empty (`strings.TrimSpace(payload) == ""`), and
  `maybeRegisterSessionTask` leaves the payload EMPTY **by documented contract**
  (`internal/agent/taskregister.go`: *"Payload stays EMPTY for bare auto rows on
  purpose: the briefing lives in the session JSONL … a payload mirror would
  double-store every prompt"*).
- That predicate is the ONE definition (Rule 14) behind both surfaces, so they
  cannot disagree: `TaskNextFiltered` walks its claim order with it, and
  `taskQueuePositionSuffix` (`internal/tools/tasks.go:5905`) renders the pending
  view's `[next]` / `[queue-skip]` markers with it through `taskNextClaimID`
  (`internal/tools/tasks.go:5929`, which uses the same `WHERE status = ?` +
  `taskClaimOrderBy`).

So the row is excluded twice over: the funnel's `WHERE` never selects it, and
had it been selected, the payload arm would skip it. The pending view then
renders **nothing at all** for it — not even `[queue-skip]`, because that marker
is only ever drawn on a row that IS pending.

## The one path that could make it claimable — and why it does not today

The two gates are not redundant for the question that matters: *what if the row
becomes pending anyway?* There IS such a path — the sync claim rollback
(`syncClaimRemoteRevert`, `internal/tools/memory_sync_claim.go:703`, and
`RollbackLocalClaim`, `…:774`) runs

```sql
UPDATE tasks SET status = ?, updated_at = ? WHERE task_id = ? AND status IN (?, ?)
```

with `taskStatusPending` and the source statuses `claimed`/`in_progress`. Its
purpose is an M4.21 transaction rollback (a `task run` that failed to take the
remote lock must not leave the row claimed), and it is reached from the sync
layer, which on this host is **off** (`task-sync: off (declared in task_sync:
false)` — the run's own startup line). The reclaim/zombie family never writes
`pending`; it closes or shreds rows. So the answer is: **not claimable, and the
only arm that could have made it claimable is (a) sync-only and (b) disabled
here** — which is exactly the border a regression test should pin rather than
leave to a comment.

## The pin (deliverable 2)

`internal/agent/taskregister_claimability_test.go` —
`TestAutoRegisteredRunRowIsNotClaimable`. It drives **the real hook**
(`maybeRegisterSessionTask("1")`, the epic's own repro payload) and then **the
real funnel** (`tools.TaskNextFiltered`, the same call worker/serve/dispatch use)
and the **real views** (`tools.TaskList`), with a control row submitted through
the real `TaskSubmit` to prove the funnel is live in the same ledger:

1. the auto row exists and its status is `in_progress` — gate 1;
2. `TaskNextFiltered` reports an **empty** queue while the auto row is the only
   row present, and never returns its id;
3. the auto row is absent from `task list --status pending`, present in the
   ledger view, and still carries `[via auto]`;
4. the control row (a real briefing, through the real submit funnel) IS pending,
   IS marked `[next]`, and IS claimed by the very same `TaskNextFiltered` call.

## Red/green (the test is not vacuous)

Both gates were mutated in turn, in `internal/tools/tasks.go`, and the test
observed RED for each — the two mutations fail through **different** assertions,
which is why the test is worth having:

| mutation | mechanism | observed |
|---|---|---|
| **A** `opts.Status = taskStatusInProgress` → `taskStatusPending` (tasks.go:1640) | kills gate 1 | `FAIL` — *"auto row status = \"pending\", want \"in_progress\""* **and** *"the auto row is PENDING — it is dispatchable-and-visible"* (line 124) |
| **B** `if false && strings.TrimSpace(payload) == ""` (tasks.go:5848's payload arm) on top of **A** | kills gate 1 + gate 2 | `FAIL` — *"THE CLAIM FUNNEL TOOK THE AUTO-REGISTERED ROW 01a0debb-b499-… — a bare `gopi --gopi-prompt 1 version` run is now dispatchable work"* (line 112) |

Both mutations were reverted (`grep -c REDPROOF-MUTATION internal/tools/tasks.go`
→ `0`) and the test reconfirmed green. Mutation **B** is the one that matters:
it is the exact disaster the test names, and the payload arm is the guard that
holds it out once the birth status has been lost.

## The live transcript (Rule 31)

Full detail in `live-transcript.md` in this run's session directory
(`~/.gopi/sessions/01a0deaa-2173-7c46-a72e-bae8ac6b2e9c/live-transcript.md`).
Headlines:

- **The premise reproduces.** `bin/gopi sandbox run … -- bin/gopi --gopi-prompt 1
  version` registered row `01a0deb8-e3a1-75ae-915e-2e82c18d1819` (`in_progress,
  auto`) and ran **32 iterations / 199.8s / ~1.08M prompt tokens** on the epic
  `1` before the wall-clock kill flipped it `failed`.
- **The funnel never sees it.** In the sandbox's own ledger, `task list` shows
  both `[auto] 1` rows (one caught live at `[in_progress]`, `[via auto]`), while
  `task list --status pending` → **`No tasks found.`**
- **The control proves the funnel is live.** A real briefing submitted into the
  same ledger renders `1 task(s) with status=pending: … control: a genuine
  briefing with a payload … [next]`.
- **One leg refused and is reported as a non-result**: `gopi worker --once` exits
  **3** at the agent-spawn guard (the launcher is itself an agent). Clearing that
  guard would be a deliberate rule violation, so it was not cleared; the
  `--status pending` view is the funnel's verdict rendering required instead.

## Findings recorded, not acted on (Rule 28)

The epic's own scope note reads this as peerbox's system layer (worktree
lifecycle / claim machinery), while this row ran on **hostb-Laptop**
(`[node] hostb-Laptop (client→peerbox)`, and the run's startup line says the
same). Recorded as a finding; **not** silently migrated, per the epic.

Two further observations from the live run, outside this epic's scope and left
as findings for their own task: the sandboxed child logged
`[task-notify] cmd "cat >> /home/user/.gopi/notify/task-events.jsonl": exit code 2
(sh: 1: cannot create …: Directory nonexistent)`, i.e. the notify sink does not
exist in a private profile; and the child burned **32 iterations** on a
one-character epic, which is a question about the run's own refusal behaviour
rather than about claimability.

## Why no code change in this branch

The claimability is correct as it stands, and the two ways to "make the row
claimable" are both wrong: (1) mint it `pending` and a dispatcher will be spawned
onto a row whose entire briefing is `1` — the mutation-B failure above; (2) drop
the payload arm from `taskRowClaimable` and every ledger-only row becomes
dispatchable (bug028's regression, `TestCLIPositionalSubmitIsDispatchable`'s
whole subject). The epic explicitly asks for the repro test in this case, and
that is what shipped. Making the *run* refuse a degenerate epic is probe/refusal
hygiene — a different surface, and a different task.
