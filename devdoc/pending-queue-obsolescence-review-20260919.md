<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# Pending-queue obsolescence review — 2026-09-19

Reviewed by: PA (Moneypenny). Trigger: the operator asked for a review of all pending
tasks against the CURRENT gopi, because the queue carried rows written against a system
that has since moved.

**Method (the part that matters for future re-review).** For every pending row, the
`rework:`/`resolve merge conflict:` family names a *branch*. A branch that no longer exists
in `refs/heads` cannot be reworked — so existence of the named ref was checked directly with
`git rev-parse --verify --quiet refs/heads/<branch>` rather than inferred from the summary.
Rows whose named branch is gone are recorded here and cancelled (the operator's instruction:
*"write a report to record for future review and cancel them"*) — **cancelled, never deleted:
the row is the audit trail, and the reason column records why.**

## What changed in gopi that made these rows stale

Read from `gopi -h` (current build) and the docs it points at:

- **`gopi merger` is RETIRED (2026-09-19)** — `devdoc/merger-retired.md`. The resident daemon
  refuses and exits 1; merging itself is not retired, `gopi merge` (foreground one-shot) is
  the merge face. **Any row that assumes a resident merger exists is written against a
  system that no longer runs.**
- **`gopi cli` EXISTS** — "the same chat in a TEXT-mode browser (links/w3m-class) —
  server-rendered page, one streaming response per turn, live tool lines". The Links POC
  landed; the CLI-chat question it answered is closed.
- **`gopi worker [<task-id>] [--once] [--daemon]`** is the only command that runs a task row;
  plain `gopi worker` drains the queue then self-exits.
- **`devdoc/DOC-INDEX.tsv`** exists with 2085 rows — the devdoc truth-classification campaign
  delivered its manifest.
- `gopi merge --fast` exists (`feat/merge-fast`), and a follow-up gap (verify + withdraw) was
  filed as `01a0b7d2`.

## A. Cancelled — target branch no longer exists (nothing to rework)

17 `rework:` rows + 1 project-specific row. **Every named branch was checked and is GONE.**
None of these can be executed: the ref the task was written to rework is not in the repo.

| task id | priority | named branch | ref state |
|---|---|---|---|
| 01a0b5f5-bf70-7424-a789-08c091158faa | 5 | fix/stylegate-ratchet-master-base | GONE |
| 01a0b5f5-c025-7c2e-a19b-695623f48e30 | 5 | fix/tools-budget-heavy-classification | GONE |
| 01a0b5f6-6c3b-7cbf-af06-b76a6423d68a | 5 | fix/web-menu-keyboard | GONE |
| 01a0b716-cceb-7a67-8327-9fa26420138c | 5 | fix/cmd-agent-ebadf-fd-race | GONE |
| 01a0b717-0115-7e72-8100-892b4a518665 | 5 | feat/merger-leg-homes | GONE |
| 01a0b717-02fb-7811-822c-91806f73246e | 5 | fix/borrowed-fd-sibling-sites | GONE |
| 01a0b717-0442-7a87-b5ec-d0f8323f8ae3 | 5 | fix/master-red-gates | GONE |
| 01a0b717-0574-77ca-bf04-6363ec371ec9 | 5 | fix/merger-status-self-pid-rework | GONE |
| 01a0b717-0701-744b-83b2-c77187c9895a | 5 | fix/stylegate-ceilings-ratchet-rework | GONE |
| 01a0b717-0857-7f05-b8cc-549cf93b6a8b | 5 | fix/takeover-wedge-verdict-rework | GONE |
| 01a0b717-0ad2-76e2-86e5-95a2056f36b6 | 5 | fix/zombie-state-assert-rework | GONE |
| 01a0b717-36a7-7cc3-9172-ca64956bf306 | 5 | fix/docclass-manifest-corpus | GONE |
| 01a0b717-386a-7009-9fad-8b8b94f4f077 | 5 | fix/task-source-worker-derivation-rework | GONE |
| 01a0b717-40cd-772a-98d4-a44b99cee5f9 | 5 | poc/chat-tool-row | GONE from refs/heads — **see the note below** |
| 01a0b73c-05a7-7bce-a7bd-080bf7dbd56b | 5 | proof/fd-finalizer-borrowed-base-red | GONE |
| 01a0b7d7-e40d-7473-9006-e089d9173d3d | 5 | fix/web-body-cap-derived-off-master | GONE |
| 01a0b8af-78ef-7461-a4f3-24a2b379a222 | 5 | fix/merge-verify-landing-witness-20260917 | GONE |
| 01a0b70f-cce2-7b8f-9850-8e530c8e941a | 5 | rework: poc/chat-tool-row (resolve-conflict row) | **see the note below** |

**NOTE on the two `poc/chat-tool-row` rows** (01a0b717-40cd, 01a0b70f-cce2): that branch was
deliberately moved into a protected worktree (`~/workspace/poc_chat_tool_now`) by the operator,
and the branch's tip was checked in before the move. The ref is not in `refs/heads` of this
checkout, so a rework row against it cannot run here. **A future reviewer should check whether
the POC is still wanted** (the operator said he would think about it); if it is, the row should
be re-filed against the worktree's branch, not resurrected from these two.

## B. Read and decided (3 rows) — outcomes

- **01a0b63b-c50c-79fd-9dc3-9d0784370345** — "land the 4 REF-only diverged branches".
  Its premise does not hold any more: it says the fix is on `fix/merge-ignores-branches`
  (commit `eaceb913`), and **that commit is NOT in master and that branch is GONE**; the repo
  now holds 12 branches, not the 33/4-diverged census the row was written against. The
  question it raises (can a diverged branch ever be offered for landing?) is **still a real
  product question**, but this row is not the vehicle for it — its measurements, its branch,
  and its blocker all refer to a state that no longer exists. **Decision: cancel this row;
  the product question is re-raisable with fresh numbers if the operator wants it.**
- **01a0b80d-f104-7b8b-a894-4dd121fc7930** — "internal/tools per-package budget: measure the
  declared 25m". **Still valid and precisely written** (it even warns that the earlier
  evidence is unverified and that Rule 27 forbids manufacturing load). Its context commit
  (`e9d72669`, `fix/peer-alias-inventory-master-red`) is not what matters — the measurement
  itself is still un-done and its instrument (`scripts/test-budget.sh`, `test_budget="25m"`,
  `heavy-tests.tsv` with 9 internal/tools rows) is intact. **Decision: KEEP.**
- **01a0b8a2-8527-757b-b46f-694b20e16d56** — orphan `verdict/merger-takeover-scope-escape-live`.
  **Its subject is already resolved**: the orphan branch is **GONE**, and master already
  carries the report (`tasks/01a0b51d-...-report.md` EXISTS in master, landed by `0674ffec`),
  with both versions reaching the same verdict (OBSOLETE per unit). The row's only open action
  — "confirm the orphan adds nothing, then delete" — has nothing left to act on. **Decision:
  cancel (superseded).**

## C. Verified against current gopi (5 rows)

- **01a0ab3f-7069-7ea5-ab41-068cc6c03248** — REQUIREMENTS B12/12 (verification & gates).
  Still open requirements work; `devdoc/requirements/` unchanged by today's events. **KEEP.**
- **01a0b1cd-fa6a-7bc9-81c0-1ccc54150c92** — "merger-self-driving.md still documents
  `--stop`/`--restart` as RETIRED while the code restored them". **Premise has flipped twice
  and the row is now wrong the other way**: `gopi merger` is RETIRED in full
  (`devdoc/merger-retired.md`), so that document's `--stop`/`--restart` text needs
  reconciling to "the whole daemon is retired", not to "the flags were restored". **Kept as a
  DOC row, but its stated reason must be rewritten** — flagged here for the next reviewer.
- **01a0b4ba-173e-70a6-9f21-72721927fe4f** — bm-pricing devdoc: append the HP capture-script
  text. Independent of today's changes. **KEEP.**
- **01a0b4d6-5131-7a22-bfc1-58d41e289558** — record the missing requirement text for
  `t-1788468254`. Independent. **KEEP.**
- **01a0b52a-ee66-76fa-bbc6-bd42bdb5d016** — "Retarget or gate-align the unreachable
  mergerUsageHead page". **The target may be gone**: `gopi merger` is retired, so that help
  page may need removal rather than retargeting. **Kept, but its option set must be
  re-derived** — the row should be read with `devdoc/merger-retired.md` open.

## HP (same treatment — its ledger is a stale copy)

HP `~/.gopi/memory.db` is not synced with peerbox (known, and the operator has said not to
build a sync). Its pending rows naming old work — `01a0acd7` (ARCH: merger becomes a pure
cronjob), `01a0acc2` (TestMemoryBaseOutOfPolicy...), `01a0ac74` (syncTaskMigrateFrom
undefined), `01a0ac6b` (merger --status trustworthy), `01a0ac6a` (deliver an ff-able branch)
— all predate master's advances, and `01a0acd7`/`01a0ac6b` rest on a merger architecture that
has since been retired outright. **They are cancelled on HP as obsolete, by the same
standard as A** (each row's premise is a system state that no longer holds), with the reason
recorded per row. The operator may re-file anything still wanted from a current base.

## Carried forward as still-live

`01a0b80d`, `01a0ab3f`, `01a0b4ba`, `01a0b4d6`, `01a0b1cd` (reason to be rewritten),
`01a0b52a` (options to be re-derived), plus the two rows filed today
(`01a0b729` sweep-report-not-act, `01a0b7d2` merge verify+withdraw).

## Branches worth the operator's eye (not tasks)

Of the 12 branches in `refs/heads`, 9 are masters-of-nothing (ahead=0 and ancestors — they
only await `branch -d`); **3 carry real work**:
`fix/cmdagent-three-reds` (ahead=5), `fix/rule17-envescape-rework` (ahead=1),
`rework/rule17-merge-lock-escape` (ahead=5).
