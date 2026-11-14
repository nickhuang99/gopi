<!-- doc-class: historical -->
# rid-review e3 fixture: an ambiguous prefix as a live test input (2026-10-08)

A **record** of one probe run — task `01a117c9-2222-7222-8222-222222222222`, branch
`task/01a117c9222272228222222222222222`. No standing claim about master; the code
was not changed.

## What the fixture is

Two ledger rows were minted deliberately sharing the first dash-group of their
uuid v7, so that the token `01a117c9` is genuinely ambiguous on the live ledger:

| id | status | role |
|---|---|---|
| `01a117c9-1111-7111-8111-111111111111` | in_progress | sibling door target |
| `01a117c9-2222-7222-8222-222222222222` | in_progress | second row sharing the prefix (this run) |

Both were minted by `rid-review-probe3.sh` (review session
`01a11758-441c-755e-99b8-ca1da5833286`) with `gopi task submit --task-id …`
against an **unset** `GOPI_HOME`, so they landed in the operator ledger instead of
a sandbox — the same leak channel the review of `01a117082d5473c7a8ed247ec357795d`
corrected. They are fixture rows, not operator work; they stay in the ledger
(append-only) as the evidence of that channel and as the input the rule-id review
needs.

**Landscape (generator:
`/home/user/.gopi/sessions/01a117c9-2222-7222-8222-222222222222/ambiguous-prefix-census.py`,
read-only `mode=ro` on `~/.gopi/memory.db`, 2026-10-08, re-run at close-out
`revision 59f7c8310`)** — named figures are the close-out re-run, earlier
paragraph text was the `d28e20305` reading (the ledger grows between readings, so
the two differ by the rows minted in between): 3774 rows / 2450 distinct first-dash-group
prefixes → **391 prefixes name more than one row (1715 rows)**; of those, exactly
**one is live: `01a117c9`** (a second prefix, `01a117d7`, also names two rows —
both `cancelled`, so it is not live: liveness is the property that matters, not
ambiguity alone). That is what makes this fixture the only place where
the ambiguity arms of the resolution doors can be exercised against *running* work
rather than against terminal rows.

## What was measured — the doors vs `01a117c9`

The `fullid` refusal (`internal/fullid`) is implemented for the task/session id
doors that were fixed by `c7de4ec8b` and its successors. Doors that predate that
work resolve by **exact match**, so the ambiguous token simply misses:

| door | exit | verdict |
|---|---|---|
| `task show 01a117c9` | 1 | `no task with task_id="01a117c9"` — plain not-found, indistinguishable from "no such row" |
| `task status 01a117c9` | 1 | same plain not-found |
| `task update 01a117c9 --status pending --reason probe` (**write door**) | 1 | same plain not-found |
| `task show --branch task/01a117c9` | 1 | no row names that branch (correct: a branch token is not an id) |
| `task for-branch 01a117c9` | **4** | `no owner: no ledger row names branch 01a117c9` — the "nobody owns it" code for an operand that is not a branch at all |
| `session log 01a117c9` | 1 | `Session not found` + the entire session listing |
| `task tag-resumed 01a117c9 deadbeef` | 1 | refuses on the *session* id before the task id is ever resolved |
| `task sync --handover 01a117c9` | 1 | **fullid refusal**, both ids named |
| `task sync --next-eligible 01a117c9` (no `--peer`) | 1 | **fullid refusal**, both ids named |
| `task sync --peer peerbox --next-eligible 01a117c9` | 1 | **fullid refusal**, both ids named |

The three refusals share one sentence:

> `task id "01a117c9" is not a full id — it is ambiguous: 2 identities share that
> prefix (01a117c9-1111-…, 01a117c9-2222-…) — … re-run with the complete id`

The `--peer --next-eligible` row is the interesting one historically: at 01:29 the
sibling review measured that same invocation as **exit 0 with the id read by
nobody**. `388a63c31` ("`--next-eligible <ID>` reaches the resolver — both guards
admit the VALUE spelling", 02:09) is an ancestor of the running revision
`d28e20305`, and the swallowed arm is gone: the value now reaches a resolver and
the ambiguity is refused.

`for-branch` uses a different selector (branch name, not row id), so its exit 4 is
not an id bug — it is noted only because an operator handed a prefix there gets
"nobody owns it", which reads as an answer when it is really a category error.

## The honest gap (no row filed from this fixture)

`task show` / `task status` / `task update` answer a *known-ambiguous prefix* with
the same sentence as a *nonexistent row*. A reader cannot tell "you typed a prefix"
from "this row is gone". The fix is one call at their resolution seam
(`fullid.CanBePrefix(id)` → `fullid.Refusal`) — the pattern
`internal/tools/memory_sync_row.go:370` already uses. It was **not** implemented
here: this row is a fixture, and id resolution is the layer the review pair
(`01a117c9-1111-…`, the sibling door target) is chartered over.
