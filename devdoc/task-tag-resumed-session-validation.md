<!-- doc-class: historical -->
# Task Report: F16.1 — `gopi task tag-resumed` validates its session id (bug `01a0ed37-4092-7ec5-a050-21da34cef04c`)

## Completed

- **The session id at the funnel is now checked against real artifacts** —
  `internal/tools/task_resumed_session.go` (new). `TaskTagResumed`
  (`internal/tools/tasks.go`) resolves the second positional against THIS
  profile's session artifacts BEFORE it touches the row, and refuses with the
  operand NAMED and a NONE-ZERO exit when no artifact exists. The all-zero
  uuid, a typo, a pruned id, a task id and an empty run directory are all the
  same answer: refused, nothing written. The zero-value path is the only
  refusal — every shape a real session leaves is admitted.
- **The shapes checked** (name probes, one `stat` set per directory — never a
  walk of the tree, which holds ~10^5 entries on the live profile):
  `sessions/<task-id>/<session-id>.jsonl` (a task run's log),
  `sessions/<task-id>/<session-id>.raw.log` (its co-located raw log),
  `sessions/<session-id>/session.jsonl` (a bare prompt/chat session), and
  `sessions/<session-id>.jsonl` (the legacy flat layout). Directories searched
  are the sessions root, the caller's task id, and the LEDGER's own hint
  directories (`taskSessionDirHints` — a session that submitted rows is filed
  under its own task id), so an audit operand naming another task's session is
  still found.
- **Short ids resolve to the ONE canonical full id** whose artifact is on disk
  (the doc comment's promise — the code used to store whatever it was handed).
  A chunk naming SEVERAL ids is refused with both ids named
  (`internal/fullid.Refusal`, the repo's one spelling); one naming NONE is
  refused as unknown. Resolution scans the root and the hinted task
  directories only.
- **A bare directory name is NOT evidence**: session dirs and task dirs are
  spelled identically, so a directory counts only when it holds
  `session.jsonl` (the bare-session record). This is the `resumed:<task-id>`
  pollution class from the live ledger, measured as a future pitfall.
- **Help text** (`cmd/agent/cli_tasks.go`, `tasksUsage`) rewritten to state the
  contract: artifact required, refusal named + exit 1 + nothing written,
  short-id expansion, and the `resumed: ×N` counting semantics (one per
  distinct session stamped; a re-stamp is a no-op). The inventory gate
  (`devdoc/gopi-task-help-surface-inventory.md` synopsis table) is unchanged
  and green — the synopsis line was not touched.
- **Tests** (Rule 5/10.5 — the fix ships its repro):
  - `internal/tools/task_tag_resumed_session_test.go` (new): the exact
    finding operand refused + nothing written + count 0; gibberish; a one-off
    uuid; a TASK id; an empty run dir; every accepted shape, each asserted to
    store the CANONICAL id and to be idempotent (count stays 1); the named
    ambiguity refusal; distinct-session counting (`×2`); the empty operand.
  - Updated to lay down artifacts (the funnel refuses ids with none):
    `cmd/agent/cli_resume_task_test.go` (round-trip + list marker),
    `cmd/agent/cli_resume_failed_row_test.go` (provenance),
    `cmd/agent/cli_tasks_help_surface_basic_test.go` (the G1 probe of the real
    binary — now also pins the second refusal, over the real CLI).

## Not Done

- **Pre-existing junk tags on live rows are NOT cleaned.** The fix is
  refuse-new; a row already carrying `resumed:00000000-…` keeps it. Cleaning
  the operator's ledger is an operator decision (which rows were really
  resumed), not a check's — deliberately left.
- **The `resumed:` tag still does not back-fill `session_id:`.** The finding
  observed the row contradicting itself (`session_id:` empty beside
  `resumed: ×1`). The check makes that state much harder to reach — a tag now
  implies an artifact on disk — but it does not write the row's `session_id:`
  column, which is a separate write path: a row tagged by a legacy
  (pre-fix) call, or one whose session is filed non-canonically, still shows
  an empty `session_id:`. Changing what that column means is a
  ledger-semantics change, out of this bug's scope.
- **No standalone `resume-audit` verb.** The road not taken (design decision
  D): a read-only audit listing `resumed:` tags with an on-disk verdict would
  cover the pre-existing pollution, but it is a NEW tool surface (Rule 3's
  bar) and a new deliverable — a follow-up task if the operator wants it.
- **`resumed:<id>-claim` style operands** (a wrapper's claim directory name)
  are refused: the suffix makes the name a non-UUID, and the check admits
  canonical shapes only. Correct for the finding, but any caller that
  genuinely writes such a tag must be re-pointed at the session id — none does
  today (the only in-repo caller passes a real session id).

**Design decision D (the "Narrow semantics" choice, stated for review):** the
check REQUIRES an artifact (never accepts an id it merely cannot prove wrong),
because that is the whole finding — an accepted-but-unsatisfiable stamp. The
cost is a coupling: a future caller holding a legitimately unknown id (a
pruned session, a remote peer's id) is refused. `gopi resume --task` is safe
by construction (it stamps the session it has JUST opened, whose JSONL exists
before the call — `NewSessionLoggerForRun` creates it), and an operator with a
genuinely unknown id gets an explicit refusal naming the operand, not a silent
stamp. The alternative (accept when "plausible") was rejected: it reproduces
exactly the reported defect.

## Verification

- Build: `go build ./...` → OK (`CGO_ENABLED=0 go build -o /tmp/gopi-f16 ./cmd/agent/` → BUILD_OK)
- Tests: `go test -count=1 -run 'Resume|Resumed|TagResumed|HelpSurface|SessionDirName|NamedLimits' ./internal/tools/ ./cmd/agent/ ./internal/config/ ./internal/agent/` →
  `ok internal/tools 3.769s / ok cmd/agent 23.022s / ok internal/config 1.006s / ok internal/agent 1.180s`
  `go test -count=1 ./internal/standards/` → `ok 60.704s`
  RED-PROOF (validation disabled in `TaskTagResumed`) → `--- FAIL: TestTaskTagResumedRefusesASessionWithNoArtifact`, `--- FAIL: TestTaskTagResumedAcceptsEveryRealArtifactShape/short_id_expands…`, `--- FAIL: TestTaskTagResumedRefusesAnAmbiguousShortID`; restored → green.
- Ran: a sandbox profile, then the verb with real arguments (Rule 31) —
  `SB=$(gopi sandbox create --label f16 --task-id 01a0ed37…)`;
  `GOPI_HOME="$SB" /tmp/gopi-f16 task tag-resumed 01a0ed54-3903-7603-afd9-8838c35f70a0 00000000-0000-0000-0000-000000000000` → **exit=1**, stderr `task: tag-resumed: session id "00000000-0000-0000-0000-000000000000" names no session of this profile (no session log, raw log or evidence directory on disk) — nothing tagged, and a row's resume lineage must name a real session`.
  `… tag-resumed <T> <T>` (a task id as a session id) → **exit=1**; `… tag-resumed <T> zzzz-not-a-session` → **exit=1**; `… tag-resumed <T> 0005b3f4-…-189249529fed` (one digit off a real session) → **exit=1**.
  `… tag-resumed <T> 0005b3f4` (an 8-char chunk) → **exit=0**, `Task 01a0ed54-… tagged resumed:0005b3f4-61ff-41a7-8740-189249529fec` (expanded to the canonical id); re-running the full id → `already carries resumed:0005b3f4-…`, exit 0.
- Result: `gopi task show <T>` after the ACCEPTED stamp → `tags: resumed:0005b3f4-61ff-41a7-8740-189249529fec` + `resumed: ×1`; after the three REFUSALS → the row still showed only that one tag (queried before and after — nothing added). The finding's own transcript (`exit=0`, tag written) no longer reproduces.
- Commit: `25f45e09` `fix(task): refuse a tag-resumed session id that names no session (F16.1)` (branch `task/01a0ed3740927ec5a05021da34cef04c`)

## Evidence (self-filed)

- `go test -count=1 -run 'TestTaskTagResumed' ./internal/tools/` → `ok github.com/nick/gopi/internal/tools 2.085s` (the new file alone).
- Red→green transition observed for the three refusal/expansion tests (commands above; the RED run was made by short-circuiting the validation call in `tasks.go`, restored from a copy — `git status` shows no residue in `internal/agent/loop.go`, which was tried and reverted within the run).
- CLI observations are the `exit=N` values and the stderr lines quoted above, taken in one shell invocation; the row state was re-queried with `task show` AFTER each call rather than inferred.
- The sandbox's own ledger/sessions tree was used (not the operator's profile): `GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-f16-9a63e9ec`, id verified in the `sandbox create` stderr line.

## Post-sync re-verification (delivery-point sync — Rule 30a)

The delivery-point sync ran LAST, after the work was committed and the worktree was clean (`git status --porcelain` printed nothing), as Rule 30a requires — never as a mid-run milestone.

- `git rev-list --left-right --count master...HEAD` → `5  2` (master had moved 5 commits ahead).
- Dry run first, without touching a worktree: `git merge-tree $(git merge-base master HEAD) master HEAD | grep -c '<<<<<<<'` → `0` — no conflict marker. The two files changed on BOTH sides (`cmd/agent/cli_tasks.go`, `devdoc/DOC-INDEX.tsv`) merged automatically.
- `git merge master --no-edit` → `Merge branch 'master' into task/01a0ed3740927ec5a05021da34cef04c`, commit `098002fd`; `git status --porcelain` → nothing.
- Nothing about the merge was inferred — every claim below was re-measured on the merged tree:
  - `CGO_ENABLED=0 go build -o /tmp/gopi-f16-sync ./cmd/agent/` → build OK.
  - `go test -count=1 -run 'Resume|Resumed|TagResumed|HelpSurface|Merger' ./internal/tools/ ./cmd/agent/` → `ok internal/tools 12.689s`, `ok cmd/agent 18.312s`.
  - `go test -count=1 ./internal/standards/` → `ok internal/standards 29.818s` (the two files touched on both sides both keep their gates green).
  - `grep -n artifact cmd/agent/cli_tasks.go` → line 414 still carries the help contract (`session_id must name a REAL artifact of this profile …`) — the merge did not drop this run's help text.

**The changed surface was RE-RUN for real on the merged binary** (Rule 31), in a fresh sandbox profile (Rule 34), `GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-f16sync-ae5c7cb0`:

| invocation | observed |
|---|---|
| `task tag-resumed <T> 00000000-0000-0000-0000-000000000000` | `exit=1`, stderr names the operand, nothing tagged |
| `task tag-resumed <T> not-a-session` | `exit=1`, same refusal shape |
| `task tag-resumed <T> ""` | `exit=1`, `tag-resumed requires both a task_id and a session_id` |
| `task tag-resumed <T> aaaa1111` (short chunk, artifact seeded) | `exit=0` → `Task <T> tagged resumed:aaaa1111-2222-3333-4444-555566667777` — the CANONICAL id, not the operand |
| `task tag-resumed <T> aaaa1111` again | `exit=0` → `already carries resumed:aaaa1111-…` (idempotent) |
| `task tag-resumed <T> <T>` (a TASK id) | `exit=1` — see the trap note below |
| `task show <T>` afterwards | `tags: resumed:aaaa1111-…`, `resumed: ×1 (see resumed:<session-id> tags above)` |

**The trap survived the merge, re-measured by hand**: the sandbox's `sessions/<T>/` directory EXISTED (it holds `metadata.json` and `attachments/`), yet the operand `<T>` was refused and `aaaa1111` (same directory, a real `<sid>.jsonl`) was accepted. That is the property this fix exists for — a bare directory is not evidence; only a directory holding `session.jsonl`, or a file stamped with the id, is. Had the probe accepted any existing directory, `tag-resumed <T> <T>` would have stamped a TASK id as a session.

**Sited on the operator's profile?** No — the seed artifacts were written under the sandbox's own `$GOPI_HOME/.gopi/sessions/`, and the row was a throwaway `sandbox probe row` submitted inside that sandbox's private ledger.

## Files

- new: `internal/tools/task_resumed_session.go`, `internal/tools/task_tag_resumed_session_test.go`
- changed: `internal/tools/tasks.go` (`TaskTagResumed`), `cmd/agent/cli_tasks.go` (help), `cmd/agent/cli_resume_task_test.go`, `cmd/agent/cli_resume_failed_row_test.go`, `cmd/agent/cli_tasks_help_surface_basic_test.go`
