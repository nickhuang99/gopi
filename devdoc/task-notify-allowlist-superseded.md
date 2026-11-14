<!-- doc-class: historical -->
# Task Report: the notify event allow-list now DECIDES about every writable status (`superseded` included)

Task `01a12f96-c2bf-732f-8cd24b63a367d4af` · Branch `task/01a12f96c2bf732f8cd24b63a367d4af`
Worktree `/home/user/workspace/gopi-01a12f96c2bf732f8cd24b63a367d4af` · Base `17783ee4a` (master)
Bug report this fixes: run `01a12f76`'s
`~/.gopi/sessions/01a12f76-4917-7e38-bcda-275baf52466b/artifacts/bug-notify-allowlist-omits-superseded.md` (Rule 41)

## Completed

### 1. The decision (deliverable 1) — `superseded` IS a default member, and every other writable status is accounted for

The default event set was a hand-spelled string (`"claimed,in_progress,completed,failed"`) sitting
next to a growing status vocabulary with nothing connecting them. `superseded` — writable since the
parent-release-statuses epic (2026-10-07) — was in the string's blind spot: a real
`gopi task update <row> --status superseded` exited 0, the row read `superseded`, and the event was
dropped (Rule 15 diagnostic, delivery filter — the write was never affected).

`internal/tools/tasknotify.go` now carries the decision AT the definition site, in three pieces:

| piece | role |
|---|---|
| `taskNotifyEventsMembers` (`[]string` of status **constants**) | the default set's members — `claimed, in_progress, completed, failed, superseded` |
| `taskNotifyEventsExcludedDefault` (`map[string]string`) | the **recorded exclusions** with a reason each — `pending`, `cancelled`, `verified` |
| `taskNotifyEventsDefault()` | the wire string, `strings.Join(taskNotifyEventsMembers, ",")` — derived, so the string and the table cannot drift |

**Why `superseded` is a member (not merely a default):**
- it is a **Rule-33 parent-GATE RELEASE status** (`taskParentReleaseStatuses`): reaching it releases
  the row's parked children. A release nobody is told about is the silent-success shape Rule 15
  exists to forbid;
- its rank-3 lifecycle peer `cancelled` is on the allow-list the operator actually runs, so silence
  here was an inconsistency between two statuses of equal lifecycle weight;
- its semantic is "the work is carried ELSEWHERE" (a successor row exists) — the operator's
  dependency chain MOVED, the same class of news as `cancelled`;
- its cost is bounded: it is written only by an explicit decision (`task update --status
  superseded`, `task send --cancel-source`), never by a mover path, so it cannot become noise.

**Why the three exclusions are exclusions:** `pending` is a row BIRTH, not a transition (`TaskSubmit`
mints rows born `pending`; a birth has no previous status to announce — the same reason the 2026-09-12
epic never listed it). `cancelled` and `verified` are **writable and fully deliverable on
subscription**; they are a noise-budget call, not a gap — `cancelled` is already in the operator's own
settings.json list, and `verified` is the verify loop's UPGRADE of a row that already announced
`completed`, so defaulting them would only widen the untouched-config default and double-report one
lifecycle. They are recorded as decisions rather than left unmentioned.

### 2. The membership gate (deliverable 2) — `internal/tools/tasknotify_events_default_test.go`

`TestTaskNotifyEventsDefaultCoversEveryWritableStatus` requires every entry of
`taskStatusWritable` to appear in **exactly one** of the two tables. Four failure shapes:

- a status in **NEITHER** table → *the `superseded` defect itself*;
- a status in **BOTH** → a contradiction (the set cannot deliver and refuse the same event);
- an exclusion for a **non-writable** status → the accounting is fake (a `--status reopened` write is
  refused, so excluding it accounts for nothing);
- a member that is not writable, or a rendered default string that disagrees with the member table.

Two companions: `TestTaskNotifyEventsDecidedStatusesAreExactly` pins WHICH statuses are exclusions
(a silence-by-moving-to-the-table change must edit the policy list), and
`TestTaskNotifyEventsDefaultDeliversEveryMember` is the **behavioural** half — with no settings and no
notify env it drives the real filter+delivery path for every member and asserts delivery, and asserts
each exclusion is NOT delivered. A decorative table that did not reach the default string would still
drop `superseded`; this test is what proves it does not.

### 3. Documentation in the same commit — every surface that DESCRIBED the old default

The old default string was documented in more places than the code that used it, and a stale copy of
it is a wrong answer to "which events fire by default?". All of them now carry the new string:

| file | what it described |
|---|---|
| `CONFIG.md` §2e (`events` row) | the knob's default, plus the two decision tables and the gate by name |
| `internal/tools/tasknotify.go` | the file header's settings.json sample and the `GOPI_TASK_NOTIFY_EVENTS` line |
| `internal/config/config.go` (`NotifySettings.Events`) | "Empty = the code default …" |
| `internal/config/settings.go` (the `GOPI_TASK_NOTIFY_EVENTS` catalog row) | the help-text default |
| `internal/tools/memory_sync_claim_test.go`, `internal/agent/cov_agent_cr_test.go`, `internal/tools/repo_sweep_report.go` | comments quoting the default while reasoning about an unsubscribed event |

### 4. Repairs made in-run (things found while verifying, not part of the planned change)

1. **`const NotifyEventLedgerDivergence` had been lost from `internal/tools/tasknotify.go`** — its doc
   comment was left dangling and `cmd/agent/task_ledger_agreement_redproof_test.go:311` still used it.
   `go build ./...` was GREEN (it does not compile `_test.go`); `go vet ./cmd/agent/` was RED. Restored.
   The durable half of that finding is filed as its own bug row (`01a12fcd-e63e-77c1-87df-82c2015b5c30`),
   because Rule 12's post-merge rebuild runs only the declared build and is blind to a test-only break.
2. **The decision-site comment contradicted its own table** — it said "The ONE recorded EXCLUSION is
   `pending`" while the table holds three and the gate asserts exactly three. Corrected.
3. **A normative anchor this change moved was re-pointed, not left to drift.**
   `devdoc/requirements/99-summary.md` anchored `` `internal/tools/tasknotify.go:120`–`:126` ``; inserting
   the decision site moved `taskNotifyEventLedgerDivergence` to `:158`, so the anchor landed on a blank
   line. Re-pointed to `:158`–`:164` (same symbol block, verified).

## Not Done

1. **The measured host's effective filter is unchanged — deliberately, and it cannot be changed by this
   fix.** This host's `~/.gopi/settings.json` carries an explicit
   `"events": "claimed,in_progress,completed,failed,stale,build-failed,cancelled"`, and an explicit
   settings value **wins over the code default** (`config.ResolveNotify`). So on THIS host a `superseded`
   transition was dropped before this change and is **still dropped after it**. The fix makes the CODE
   default correct and the decision recorded; whether this host subscribes `superseded` is the operator's
   settings edit (or `GOPI_TASK_NOTIFY_EVENTS`), not an agent's unilateral change to the operator's config.
2. **`cancelled`/`verified` were NOT promoted to default members** — recorded as exclusions with reasons
   instead. That is a policy call, and the mandate was the membership decision, not a widening of delivery
   semantics.
3. **The notify bus, the delivery targets, the pattern gate and the async gate are untouched** (explicit
   epic constraint).
4. **`TestCommandsDocGateRedProofStaleGeneratedBlock` fails in `internal/standards/`, pre-existing and
   unrelated** — proven at HEAD in a clean detached worktree and filed as its own bug row per Rule 41/26
   rather than repaired inside this change. Row `01a12fc7-90e1-762d-a9d4-caaf55182494`.
5. **A test-ONLY compile break was found and repaired in-run, and its durable half is NOT repaired here** —
   the Rule-12-rebuild blindness above is its own bug row (Rule 41).
6. **`superseded` on THIS host is still filtered until the operator edits their settings** — Not Done #1.
7. **`TestDocAnchorsResolveInNormativeDocs` (`internal/agent`) is RED at this branch's BASE, not from this
   change.** Measured at clean HEAD in a detached worktree (`17783ee4a`, none of this diff present):
   7 anchor-drift errors (`docanchor_test.go:339`) + 3 stale-baseline errors (`:355`). Evidence:
   `$A/anchor-drift-head-vs-mine.txt`. It is the `devdoc/requirements/*` corpus drifting as `master`'s
   refactors move the lines its anchors name — repaired by re-keying those records, which is its own task.
   I did **not** repair it here. (This is why the touched-package test scope is
   `./internal/tools ./internal/config ./cmd/agent`, the packages this diff edits.)
8. **The disappearing evidence tree is NOT explained here** — see the note under Evidence. Filed as its
   own bug row (Rule 41): `01a12fd9-60fd-7656-883e-c785087907b5`.
9. **One load-dependent flake in the sweep, reported rather than omitted** —
   `cmd/agent.TestArchiveCLIUnreachableReportsTheKeepingWindow` failed once under a load average of ~13 on 8
   cores and passes twice in isolation on this same tree (15.1s, then 4.8s). It lives in
   `cmd/agent/cli_archive_unreachable_test.go`, no file this diff touches. I did **not** chase it; it is
   recorded so nobody reads this run's sweep verdict as cleaner than it was.

## Verification

> Evidence paths are written `$A/…`, where
> `$A = /home/user/.gopi/sessions/01a12f96-c2bf-732f-8cd2-4b63a367d4af/artifacts` — **this task's own
> session directory** (Rule 37), the same directory `GOPI_TASK_REPORT_PATH` names. The A/B and the red
> proofs were **re-produced in this session** into `$A/repro2/` and `$A/redproof/`.

- **Build:** `CGO_ENABLED=0 go build ./...` → OK (exit 0); `go vet ./internal/tools/ ./cmd/agent/` → clean;
  `gofmt -l` on the touched Go files → clean (the files it lists are pre-existing and untouched).
- **Tests (branch-scoped):** `make test-budget PKGS='./internal/tools ./internal/config ./cmd/agent'`
  — this run's own stream is `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791803835-2017129.json`
  (its path is printed in the header at `$A/test-budget-current.log`). **Per-package verdicts observed in
  that stream:** `internal/config` 497 pass / 0 fail; `internal/tools` 2146 pass / 78 skip / **0 fail**;
  `cmd/agent` 1294 pass / 9 skip / **1 fail** —
  `TestArchiveCLIUnreachableReportsTheKeepingWindow`. That one failure is **not this diff's**: it passes
  twice in isolation on this same tree (`go test ./cmd/agent/ -run TestArchiveCLIUnreachableReportsTheKeepingWindow -count=1`
  → `ok … 15.114s`, then `ok … 4.757s`), it lives in `cmd/agent/cli_archive_unreachable_test.go` (no file
  this diff touches), and it failed under a load average of ~13 on 8 cores. It is reported here as a
  load-dependent flake rather than omitted, and it is the reason this report does **not** claim a clean
  whole-package sweep.
  Targeted gate run: `go test ./internal/tools/ -run 'TestTaskNotifyEventsDefault|TestTaskNotifyEventsDecided|TestNotifyAllowListDrop|TestNotifyTerminalPushFailedUnsubscribed' -count=1` → **ok**.
  `go test ./internal/standards/ -run ConfigDoc -count=1` → ok. `gopi tool doccheck` → exit 0.
- **Ran (Rule 31 — the real surface, real arguments, real ledger):** the A/B below, run end to end in this
  session on two binaries built from THIS worktree minutes apart (`gopi-cur` = the change; `gopi-prefix` =
  `internal/tools/tasknotify.go` reset to `HEAD` and nothing else), against **one** scratch sandbox
  (`gopi tool sandbox create --label notify-ab`), with `GOPI_TASK_NOTIFY_EVENTS` UNSET (so the resolver
  must fall back to the CODE DEFAULT) and `GOPI_SETTINGS` pointed at a `{}` file (so the operator's own
  list cannot mask which set is in force).

  **PRE-FIX — the drop (the defect):**

  ```
  $ GOPI_HOME=<sandbox> gopi-prefix task update 01a12fcb-9458-7a38-8343-dfac76aabb50 \
        --status superseded --reason "A/B probe pre-fix"
  [task-notify] event "superseded" for 01a12fcb-9458-7a38-8343-dfac76aabb50 not in the events allow-list — not delivered (this is a delivery FILTER, not a failure; the write succeeded — GOPI_TASK_NOTIFY_EVENTS (env) / the "notify" section in settings.json="claimed,in_progress,completed,failed"; add "superseded" to it, or set it to "all" for every event)
  Task updated: 01a12fcb-9458-7a38-8343-dfac76aabb50 (status=superseded, reason=A/B probe pre-fix)
  status:         superseded
  sink file ($A/repro2/events-prefix.jsonl): ABSENT — nothing delivered
  ```

  **POST-FIX — the delivery (same sandbox, same command shape):**

  ```
  $ GOPI_HOME=<sandbox> gopi-cur task update 01a12fcb-273e-7427-95ce-c7ef27fc452f \
        --status superseded --reason "A/B probe post-fix"
  [task-notify] cmd ok (stdout: )
  Task updated: 01a12fcb-273e-7427-95ce-c7ef27fc452f (status=superseded, reason=A/B probe post-fix)
  status:         superseded
  sink file ($A/repro2/events.jsonl):
  {"event":"superseded","previous_status":"pending","task_id":"01a12fcb-273e-7427-95ce-c7ef27fc452f","status":"superseded","summary":"A/B probe row: notify superseded delivery","reason":"A/B probe post-fix","source":"agent",...,"timestamp":"2026-10-12T19:06:45+08:00"}
  ```

  The acceptance-relevant reading: **the row reaches `superseded` on BOTH binaries (exit 0 both times);
  only the post-fix build delivers the event.** The drop was real, and it was never a failed write.
  Note what the pre-fix line does NOT say: it never names `superseded` in the list it quotes, which is the
  defect stated by the diagnostic itself.

- **Result / red proofs (deliverable 3, acceptance b):** each perturbation applied to `tasknotify.go`, the
  gate run, then restored — **re-run in this session**, output kept in `$A/redproof/`.

  1. **`superseded` removed from `taskNotifyEventsMembers`** (re-creating the pre-fix default) → three
     tests red, with the gate's real line numbers:
     ```
     --- FAIL: TestTaskNotifyEventsDefaultCoversEveryWritableStatus (0.00s)
         tasknotify_events_default_test.go:53: writable status "superseded" is NEITHER a member of the default notify event set NOR a recorded exclusion — a `task update --status superseded` would be delivered to nobody by default and no decision about it is written down anywhere. …
     --- FAIL: TestTaskNotifyEventsDecidedStatusesAreExactly (0.00s)
         tasknotify_events_default_test.go:136: "superseded" must be a default-set member — if the decision changed, say so here AND in the decision-site comment, never quietly
         tasknotify_events_default_test.go:155: decided statuses = 3 excluded + 4 members, want the 8 writable statuses to be accounted for exactly once
     --- FAIL: TestTaskNotifyEventsDefaultDeliversEveryMember (0.05s)
         tasknotify_events_default_test.go:197: `superseded` was not delivered by the default notify event set — this is the exact regression the gate exists to catch (a Rule-33 parent-release status whose transition released children silently)
     FAIL	github.com/nick/gopi/internal/tools	0.655s
     ```
  2. **an exclusion recorded for a NON-writable status** (`taskStatusReopened`, retired):
     ```
     --- FAIL: TestTaskNotifyEventsDefaultCoversEveryWritableStatus (0.00s)
         tasknotify_events_default_test.go:79: taskNotifyEventsExcludedDefault excludes "reopened", which is not a WRITABLE status — a `--status reopened` write is refused, so an exclusion for it accounts for nothing (did you mean a writable status?)
     --- FAIL: TestTaskNotifyEventsDecidedStatusesAreExactly (0.00s)
         tasknotify_events_default_test.go:148: taskNotifyEventsExcludedDefault excludes "reopened", which this test does not record as a decided exclusion — an exclusion is a POLICY CHANGE, not a way to silence one status: update this list and the decision-site comment in the same commit
         tasknotify_events_default_test.go:155: decided statuses = 4 excluded + 5 members, want the 8 writable statuses to be accounted for exactly once
     FAIL	github.com/nick/gopi/internal/tools	0.773s
     ```
  3. **Restored** → `ok github.com/nick/gopi/internal/tools 0.829s`.

  The gate went green → **red** → green on both perturbations, and every red message names the decision a
  human must make.

- **Commit:** `473012451` (this branch; `git status --porcelain` empty after it).

## Evidence (self-filed)

> Every path below is relative to `$A` =
> `/home/user/.gopi/sessions/01a12f96-c2bf-732f-8cd2-4b63a367d4af/artifacts` — this task's own session
> directory (Rule 37).
>
> **A note on what happened to the first evidence tree, because a reader will notice the gap.** While this
> run was in flight, the older `artifacts/` tree (`repro/`, `bin/`, and the first copy of
> `docs-commands-gate-stale.md`) disappeared from this session directory: at 19:02 the directory held
> `bin/`, `docs-commands-gate-stale.md`, `repro/` and a header-only `test-budget.log`; at ~19:17 only the
> files this run re-created were left. **Measured, not inferred.** The remedy taken was to RE-MEASURE
> rather than re-quote: the A/B was re-run end to end (`$A/repro2/`), both red proofs were re-run
> (`$A/redproof/`), and the two pre-existing-failure probes were re-run in fresh detached worktrees
> (`$A/docs-commands-gate-stale.md`, `$A/anchor-drift-head-vs-mine.txt`). So every line quoted under
> Verification is backed by a file that exists NOW. The disappearing evidence tree is itself worth a look
> — an evidence directory being collected while its run is still live is exactly the shape that makes a
> report unverifiable after the fact. **Not done:** I did not investigate the mechanism, so I make no claim
> about it beyond the mtimes above.

- **A/B repro, pre-fix**: `$A/repro2/prefix-20-update.txt` — the drop line,
  `Task updated: … (status=superseded)`, and `prefix-15/30-status-*.txt` showing `pending` → `superseded`;
  `$A/repro2/events-prefix.jsonl` does **not** exist (nothing was delivered).
- **A/B repro, post-fix**: `$A/repro2/20-update.txt` (`[task-notify] cmd ok`),
  `$A/repro2/events.jsonl` = the observed sink LINE
  `{"event":"superseded","previous_status":"pending","status":"superseded",…}` — not an assertion from code.
- **Host-settings probe (the effective filter on this host is unchanged)**: the list quoted in Not Done #1
  — `"claimed,in_progress,completed,failed,stale,build-failed,cancelled"` — is the operator's own value
  read from `~/.gopi/settings.json`; the first copy of its output was lost with the tree above, and the
  claim rests on that settings value plus `config.ResolveNotify`'s precedence (settings beats the code
  default), which is exactly why the A/B pins `GOPI_SETTINGS` at a `{}` file to isolate the code default.
- **Gate red proofs** — both quoted above with their real line numbers; each is a `--- FAIL` of the named
  test, followed by a green after restore: `$A/redproof/01-no-member.txt`,
  `$A/redproof/02-nonwritable-exclusion.txt`.
- **`CGO_ENABLED=0 go build ./...` = OK (exit 0)**; `go vet ./internal/tools/ ./cmd/agent/` printed
  nothing; the files `gofmt -l` lists are pre-existing and untouched by this diff.
- **A test-ONLY compile break was found in this tree and repaired in-run** — `go build ./...` cannot see it
  (it does not compile `_test.go`), so the declared build was green while `cmd/agent`'s tests did not compile:
  `go vet ./cmd/agent/` → `vet: cmd/agent/task_ledger_agreement_redproof_test.go:311:21: undefined: tools.NotifyEventLedgerDivergence`
  (output: `$A/repro2/00-broken-test-compile-found.txt`; write-up with a marked inference:
  `$A/found-broken-test-compile.md`; filed as its own bug row, Rule 41 — id `01a12fcd-e63e-77c1-87df-82c2015b5c30`).
- **`TestCommandsDocGateRedProofStaleGeneratedBlock` is PRE-EXISTING, not mine** — re-measured in a clean
  detached worktree at HEAD (`/home/user/workspace/_tmp/cmdcheck`, `17783ee4a`, none of this diff present):
  `GOWORK=off go test ./internal/standards/ -run TestCommandsDocGateRedProofStaleGeneratedBlock -count=1`
  → `--- FAIL … commands_doc_gate_test.go:691: precondition: the committed document's block is already stale`.
  Full output + the empty diff for the test's two inputs (`docs/commands.md`, `cmd/agent/cli_verbs.go`):
  `$A/docs-commands-gate-stale.md`. Filed as its own bug row — `01a12fc7-90e1-762d-a9d4-caaf55182494`,
  verified present in the ledger with `gopi task list`.
- **`TestDocAnchorsResolveInNormativeDocs` is RED AT BASE, and this change's own contribution to it was
  fixed** — measured with the same command in a clean detached HEAD worktree and on this branch; the
  `docanchor_test.go:339` failure sets are byte-identical after the `99-summary.md` re-point. 7 drifts at
  HEAD, 3 stale-baseline errors at HEAD — all pre-existing. Full transcript:
  `$A/anchor-drift-head-vs-mine.txt`.
- **`go test ./internal/standards/ -run ConfigDoc -count=1` → ok** (my `CONFIG.md` edit is inside the gate
  that checks it).
- **`gopi tool doccheck` → the devdoc corpus is consistent** (`DOC-INDEX.tsv` carries this new doc's row;
  the gate that checks the corpus passes).
- **`make test-budget PKGS='./internal/tools ./internal/config ./cmd/agent'`**: log at
  `$A/test-budget-current.log` — the per-package verdict lines and the `EXIT=` tail.
  **Two earlier runs of this command are NOT reported as results**: both were ordered before repairs that
  followed them (the `NotifyEventLedgerDivergence` restore, the `99-summary.md` re-point), so their verdict
  describes a tree that no longer exists; the first was killed and its log holds the header only. The run
  quoted here was started after the last edit to `internal/`, and `internal/` was not edited again.
