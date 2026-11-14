<!-- doc-class: normative -->
# devdoc: payload revision — `gopi task update <id> --payload TEXT|@FILE`

Epic `01a0fea1` item **[2/5]** (single-node unlock of the payload column).
Executed on the worktree this run owns; branch `task/01a0fea165dd733e9eb2b25496ee28a1`.

## The defect this closes

`gopi task update` had exactly four arms — `--status`, `--reason`, `--result`,
`--repo` — and its own help page stated the consequence:

```
Task CONTENT (summary/payload) is immutable — cancel and re-submit to change it.
```

The payload column **is the row's briefing**: `taskRunBriefing` hands it to the
executing instance, `TaskNextFiltered` skips payload-less rows on purpose
(bug028), and `memory_sync_delta` reads "payload non-empty" as "this is a real
ticket". So a briefing with a wrong command in it had one remedy: cancel the row
and re-submit — which mints a **new task id**, breaks every reference to the old
row, and loses the history of the work that was already dispatched.

## What changed (and what deliberately did NOT)

**CLI (`cmd/agent/cli_tasks.go`).** `--payload` joins `task update`:

- the spelling is the constant `taskPayloadFlagName` (every site references it,
  never a typed literal — Rules 1b/13);
- `taskFlagMember(taskverbs.Update, …)` admits it;
- the parse arm reads the value through `ExpandFileRef`, the SAME `@path`
  expansion `submit` uses (one function, two faces, so `@` cannot come to mean
  two things);
- a status-less invocation is now a **pure edit** (`statusOmitted`) rather than
  the old "nothing to update" refusal; the refusal that remains lists every arm;
- the dispatch arm runs `--repo` (repair) → `--payload` (revision) → `--status`
  (transition), so a multi-arm invocation does what it says **in the order it
  says it** and a refused edit never consumes the transition.

**Engine (`internal/tools/tasks.go`).** `TaskSetPayload(taskID, payload, reason)`
is the one writer, in ONE transaction:

1. an **INSERT** of `event=payload_revised` into `task_events` — the reason, then
   the OLD and the NEW value verbatim, each with its sha256 and byte count;
2. `UPDATE tasks SET payload = ?, updated_at = ? WHERE task_id = ?`.

The order is the contract: the value being replaced is captured from the row
that still holds it, and a row that does not exist matches no row, so the trail
entry rolls back with the write. **Nothing is deleted and no column other than
`payload` moves** — the status machine, `ended_at`, the claim trail, the notify
bus and the verify-closure enqueue are not reachable from this door (that is why
it is NOT routed through `taskUpdateStampedFrom`; its contract is a status
transition).

**The FTS index follows for free.** `payload` is one of `tasksFTSColumns`, so
`tasks_au` re-indexes the row on the UPDATE — measured: after a revision,
`task search release` matches and `task search ORIGINAL` does not. A hand-rolled
UPDATE that bypassed the trigger would pass the column assertion and fail here,
which is why both are asserted.

### The two refusals, and why they are refusals

| input | answer | why |
|---|---|---|
| `--payload ""` | **refused by name** | the payload is the row's ONLY briefing; an empty one makes the row invisible to every dispatcher (bug028) and drops the real-ticket marker the sync layer reads. `--status cancelled` is the honest way to retire a row. |
| `--payload X` with no `--reason` | **refused by name** | a revision of an audit row must record WHY. Every other audit write in this ledger carries a reason; this one is not exempt. |
| re-sending the current value | **no-op**, nothing written | not a revision; saying so beats printing a diff-shaped success. |

### What was NOT opened

`summary` stays immutable. The epic narrowed the payload because the payload is
the **operational** briefing; the summary is the row's display identity, and its
history is what peers key on. The sibling item [3/5] (`01a0fea1-667e-…`) is the
one that moves the identity judgment to `task_id(UUID) + summary`; this item
therefore leaves `summary`, `parent_task_id` and `tags` alone.

## Single-node behaviour (the epic's own regression question)

`TaskSetPayload` reads and writes **`tasks` + `task_events` + the FTS shadow
tables through their triggers** and nothing else. Neither the function nor its
callers import or invoke `internal/tools/memory_sync_*`. The CLI parse/dispatch
arms are in `cmd/agent/cli_tasks.go` only.

The single-node profile is pinned in the engine test fixture
(`{"sync_peers":[]}` in the settings the test writes, rather than the operator's
ambient whitelist), so the assertion "no sync path runs" is made **in the
profile the claim is about**, not in whatever the test host happened to have.

## Known interaction, stated rather than hidden

`memory_sync_delta.go` still treats "same status, different payload" as a
content collision (both sides editing = a bug). With this door open, a
**multi-node** profile can now produce that state legitimately. That judgment is
item [4/5] (`01a0fea1-66e4-…`, "delta 内容冲突降级") and it is explicitly **out of
this item's scope**; the operator's framing was that the chain must not touch
`memory_sync_*` from here. Single-node profiles never reach that code.

## Rendering: a revision is recorded, not announced

`task show` renders the newest event's detail on `result:` unless the event name
is a status. Without a guard, the revision's old/new block would land there —
burying the run digest and, worse, occupying the line the close-out chain, the
report seam and Rule 31's ran-line check **read**. So `contentRevisionEventNames`
(one entry, `PayloadRevisionEventName`) is consulted by the result-line reader
and the revision renders nowhere. Nothing is lost: the row's payload column
carries the current value and `task_events` carries every previous one.

`payload_revised` is deliberately NOT a member of `verdictTrailEventNames`: a
revision is not a judgment on the row, and rendering it on the verdict line
would misreport an operator's edit as a verdict.

## Verification recipe (reusable)

```bash
# engine: the write, the trail, the FTS follow-through, every refusal
go test ./internal/tools/ -run 'TestTaskSetPayload' -count=1

# CLI: whitelist + parse arms (unit) and the real binary end to end
go test ./cmd/agent/ -run 'TestParseTasksUpdatePayload|TestTaskFlagMemberUpdate|TestTaskVerbUpdatePayloadRevisesThroughTheRealBinary' -count=1

# the help-surface gates the new flag must not break
go test ./cmd/agent/ -run 'TaskHelpSurface' -count=1
```

The real-ledger invocation and its verbatim output are in
`~/.gopi/sessions/01a0fea165dd733e9eb2b25496ee28a1/evidence/real-ledger-payload-revision.txt`.
