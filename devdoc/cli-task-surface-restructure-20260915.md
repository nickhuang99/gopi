<!-- doc-class: normative -->

> **NOTE 2026-10-26 — the verb this page is about NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left where it is. The defect analysis below is kept as history
> (the Rule 15 lesson is still live in other verbs), but no command here can be re-run.
> Current behaviour: `devdoc/zombie-sweep-removed.md`.

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
<!-- Disposition: this report is a USAGE-HELP/grouping record, not a launch recipe.
     Its `gopi task run` / `gopi worker` mentions are (a) the ledger surface table's
     row for `run` — the row whose recorded EVIDENCE is the guard REFUSING the move
     ("refusing to start: this process is inside an agent session", exit 3) — and
     (b) the restructure's own description of which usage HEAD advertises the worker
     verb. No fenced block in this file teaches how to start a worker or run a task;
     the one command it does teach a reader to copy is `gopi task submit`. -->

# Task Report: `gopi task` CLI surface restructure — one function, one home, one help

Branch: `feat/cli-task-surface-restructure` (worktree `~/workspace/gopi-cli-surface`,
off master `6bc0dfeb`, PROJECT.md Rule 6a — never in `~/work/GoPi`). Master was
merged in at the end (`1f741097` → merge commit `b02ea536`) and the full
acceptance set re-run on the merged tree; the merge touched neither
`internal/taskverbs` nor the help constants, and master's two new `cmd/agent`
ledger tests pass unchanged.

## Completed

### 1. The grouping is derived, not hand-copied (Rule 13/14)

`internal/taskverbs/verbs.go` — the leaf package (constants only, no project
imports, because `internal/config` imports it) now carries four disjoint
groups instead of one blob:

| group | verbs | advertised where |
|---|---|---|
| `FamilyVerbs` | archive, improvements, list, reopen, run, search, show, status, submit, sweep-worktrees, sweep-zombies, tag-resumed, update, verdicts, verify | `gopi task -h` + `gopi task <TAB>` + the top-level index row |
> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

| `SyncVerbs` | diff, harvest, push, sync | `gopi task sync -h` (`syncUsageHead`) |
| `MergerVerbs` | adopt, audit-merges, merge, merge-execute, merged-cleanup, merger | `gopi merger -h` (`mergerUsageHead`) |
| `ServiceAliases` | serve | `gopi merger -h` |

`DispatchVerbs` is **derived** as their union — never a hand-typed second list:

```go
var DispatchVerbs = func() []string {
	out := make([]string, 0, len(FamilyVerbs)+len(SyncVerbs)+len(MergerVerbs)+len(ServiceAliases))
	out = append(out, FamilyVerbs...)
	out = append(out, SyncVerbs...)
	out = append(out, MergerVerbs...)
	out = append(out, ServiceAliases...)
	return out
}()
```

That derivation is why the carve-out is safe: moving a spelling between groups
changes only **how it is advertised**, never **whether it dispatches**.
`serve` was already advertised nowhere yet dispatched — now it is *named* by a
group instead of being an untracked tail.

### 2. Before/after: every dispatched spelling

`--help` is `-h`; every row was actually run with the built `bin/gopi`
(evidence column = the invocation, verbatim). 26 spellings total
(`FamilyVerbs` 15 + `SyncVerbs` 4 + `MergerVerbs` 6 + `ServiceAliases` 1).

| spelling | kind | canonical home (AFTER) | help owner | still dispatched? | evidence (actual command run) |
|---|---|---|---|---|---|
| `list` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task list` → `2 task(s):` |
| `show` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task show <id>` → `task_id: <id>` |
| `status` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task status <id>` → `task: <id>` |
| `search` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task search probe` → `1 task(s) matching "probe"` |
| `improvements` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task improvements` → `agency scan: 1 row(s) examined` |
| `verdicts` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task verdicts` → `no reframe records yet` |
| `tag-resumed` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task tag-resumed <id> sess-probe` → `tagged resumed:sess-probe` |
| `update` | ledger | `gopi task -h` | tasksUsage | yes (reachability proven) | `gopi task update <id> --status completed --result X` → `illegal status transition pending → completed` (the parse+dispatch path ran; the refusal is the state machine) |
| `reopen` | ledger | **RETIRED 2026-10-30 — verb deleted, absent from `gopi task -h`** | — | — | (the row is kept as the census record of what was probed on 2026-09-15; the live spelling is `gopi task clone <id>`) |
| `archive` | ledger | `gopi task -h` | tasksUsage | yes (reachability proven) | `gopi task archive <id>` → `archive refused: <id> is pending (not terminal)` |
| `submit` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task submit "third probe" --tags a,b --payload body` → `Task queued: 01a0a256-…` |
| `verify` | ledger | `gopi task -h` | tasksUsage | yes (reachability proven) | `gopi task verify <id>` → entered the verify path (spawned the child; timeout-killed at 25s by the probe harness) |
| `run` | ledger | `gopi task -h` | tasksUsage | yes (exit 0 for `-h`; guard fires on execution) | `gopi task run` → `refusing to start: this process is inside an agent session`; `gopi task run -h` → full help |
| `sweep-zombies` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task sweep-zombies --dry-run` → `no zombie rows` |
| `sweep-worktrees` | ledger | `gopi task -h` | tasksUsage | yes (exit 0) | `gopi task sweep-worktrees --dry-run` → `repos=2 reclaimed=0` |
| `sync` | **sync** | `gopi task sync -h` → `syncUsageHead` | syncUsageHead | yes (exit 0) | `gopi task sync -h` prints the sync manual alone; `gopi task sync --status` → exit 0 (silent when no peer) |
| `diff` | **sync** | `syncUsageHead` | syncUsageHead | yes (reached the peer protocol) | `gopi task diff --peer nosuchpeer` → `diff: ssh nosuchpeer: exit status 255` |
| `harvest` | **sync** | `syncUsageHead` | syncUsageHead | yes (reached the peer protocol) | `gopi task harvest --peer nosuchpeer` → `harvest: ssh nosuchpeer: exit status 255` |
| `push` | **sync** | `syncUsageHead` | syncUsageHead | yes (exit 0) | `gopi task push` → `git-client: skipped: no sync peer` |
| `merge` | **git-infra** | `gopi merger -h` | mergerUsageHead | yes (exit 0) | `gopi task merge --repo <r> --db <d>` → `merged=0 deleted=0 …` |
| `merge-execute` | **git-infra** | `gopi merger -h` | mergerUsageHead | yes (exit 0) | `gopi task merge-execute <id>` → `status=skipped reason="summary does not parse as merge …"` |
| `merged-cleanup` | **git-infra** | `gopi merger -h` | mergerUsageHead | yes (exit 0) | `gopi task merged-cleanup` → `removed=0 skipped=3 failed=0` |
| `adopt` | **git-infra** | `gopi merger -h` | mergerUsageHead | yes (exit 0) | `gopi task adopt --repo <r>` → `scanned=1 adopted=0 …` |
| `audit-merges` | **git-infra** | `gopi merger -h` | mergerUsageHead | yes (exit 0) | `gopi task audit-merges --repo <r> [--submit]` → `scanned=1 submitted=0/1` — **see the defect fixed in §5** |
| `merger` | **service** | `gopi merger -h` | mergerUsageHead | yes (exit 0, deprecation pointer on stderr) | `gopi task merger --status` → `deprecated alias …` + `merger: alive` |
| `serve` | **service alias** | `gopi merger -h` | mergerUsageHead | yes (exit 0, deprecation pointer on stderr) | `gopi task serve --status` → same pointer + same status |

Delivered deltas, measured on the shipped binary:

- `gopi task -h` synopsis heads: **28 → 15** (the 13 removed are the 4 sync +
  6 merge-atom/service + `gopi worker` heads).
- `gopi task -h | grep -cE '^  gopi (task )?(merger|worker|serve)( |$)'` → **0**.
- `gopi task sync|diff|harvest|push -h` each print `syncUsageHead` **alone**
  (previously `syncUsageHead` + the whole family blob).
- `gopi merger -h` carries all five git-atom synopses (grep count 6 including
  the daemon's own line).

### 3. Justification per move

- **`list` block trimmed** — the `--sweep` paragraph carried the throttle
  mechanism (the persisted 10-min stamp), the HP-2026-09-02 incident, and the
  daemon-vs-no-daemon advice. That is *implementation* of one flag, not the
  ledger's contract; the flag itself stays (`--sweep` still advertised and
  still dispatches: `gopi task list --sweep` → `6 task(s):`).
  > **Retired 2026-11-05 (commit `e4521fad`).** The sweep and its flag were
  > then deleted end to end, so this move's premise (a flag that stays) no
  > longer holds: `gopi task list --sweep` answers `unknown flag --sweep` and
  > exits 1. The measurement above is the record of the move, not a command
  > to run.
- **sync verbs → their own page** — their flags (`--peer/--peers/--serve-remote/
  --claim-remote/--push-only/--full/--task-id/--tags`) describe a **peer
  protocol**, not the ledger. A reader typing `gopi task -h` to find `list`
  should not have to read a fleet-replication manual. `harvest`/`push` were
  promoted from "mentioned in the blob's sync section" into `syncUsageHead`, so
  the sync page is now *complete* (it can be read alone).
- **git atoms → `gopi merger -h`** — `merge`, `merge-execute`, `merged-cleanup`,
  `adopt`, `audit-merges` each move **git** (branches, worktrees, merges) and
  only write the ledger as a side effect. They are one-pass faces of what the
  merger daemon does every tick, so the daemon's page is their one home; the
  family group now *points* there.
- **`merger`/`serve` → no Usage block in the ledger group** — a resident
  service is not a ledger verb. Both keep dispatching, and the dispatcher still
  prints the deprecation pointer on stderr, so an operator running the old
  spelling learns the new home from the run they made.
- **`gopi worker` → never a family Usage block** — the ledger group documented
  the *worker's* loop (`gopi worker [--once]` plus a `--daemon`/env discussion)
  as if it were a `gopi task` subcommand. It now lives only on `gopi worker -h`.
  Note the pre-restructure blob taught `--daemon`/`--status`/`--stop`/`--start`
  for a worker, none of which the **family** spelling ever accepted
  (`taskFlagMember` never listed them) — a second home that had drifted into
  being wrong.

### 4. The anti-readd gate (deliverable 5) — red-then-green

New file `cmd/agent/cli_tasks_advert_gate_test.go`:

- **`TestNoHelpTextIsDuplicatedAcrossPages`** (new) — the epic's invariant
  applied to the TEXT: no two of the three big pages (`tasksUsage`,
  `mergerUsageHead`, `workerUsageHead`) may carry the same **semantics** line
  (indent ≥ 6, ≥ 40 bytes — a shared short flag word does not count).
  `…RedProof` runs the predicate over a verbatim historical duplicate and
  proves the pointer shape is not flagged.
- **`TestNoServiceOrSyncVerbInLedgerGroup`** — the named gate. Predicate
  `ledgerAdvertOffenses` runs over the **real** `tasksUsage` body (read from
  source, so it gates what ships) and flags (a) any `gopi task <verb>` SYNOPSIS
  line for a `MergerVerbs`/`ServiceAliases`/`SyncVerbs` member, and (b) any
  service flag (`--daemon --force --wait --every --no-branch-scan`) restated in
  the ledger group. It also fails if the *pointers* were deleted
  (`no non-ledger verb is even MENTIONED`), so "fix" by amputation is not
  available.
- **`TestNoServiceOrSyncVerbInLedgerGroupRedProof`** — runs the same predicate
  over the **pre-restructure text** (verbatim excerpts this change deleted) and
  requires each probe to be flagged; the final probe is the correct *pointer*
  shape, which must produce **no** finding (scope honesty).
- **`TestLedgerGroupTeachesNoServiceBlock`** — rejects a bare `gopi worker`/
  `gopi merger`/`gopi serve` synopsis line in the family usage.
- **`TestSyncVerbsHaveExactlyOneHeadHome`** — `syncUsageHead` must carry the
  sync flags and the family body must not restate them.

**Red-then-green, actually performed** (not asserted from theory). I re-inserted
the pre-restructure merger synopsis into `tasksUsage` in the working tree:

```
--- FAIL: TestNoServiceOrSyncVerbInLedgerGroup (0.00s)
    cli_tasks_advert_gate_test.go:158: ledger group re-advertises "merger": the family usage carries a
    `gopi task merger` SYNOPSIS line — that verb's one advertised home is gopi merger -h; keep the
    pointer sentence, not the synopsis
    cli_tasks_advert_gate_test.go:158: ledger group re-advertises "adopt": the family usage documents
    the flag --every, which belongs to the service/git-infrastructure surface …
    (… one finding per git atom + service …)
FAIL	github.com/the operator/gopi/cmd/agent	1.291s
```

then restored the file and re-ran:

```
ok  	github.com/the operator/gopi/cmd/agent	2.923s
```

**Re-proved after merging master (`1f741097`, commit `b02ea536`)** with a
THREE-probe injection in one edit — a git atom, a sync verb and a bare service
block, the three exit routes the epic forbids:

```
$ # injected into tasksUsage: `gopi task merge-execute <task_id>`,
$ # `gopi task sync --peer <alias>`, and a bare `gopi worker --daemon` block
$ go test -run 'TestNoServiceOrSyncVerbInLedgerGroup$|TestLedgerGroupTeachesNoServiceBlock' ./cmd/agent/
--- FAIL: TestNoServiceOrSyncVerbInLedgerGroup (0.00s)
    cli_tasks_advert_gate_test.go:158: ledger group re-advertises "merge-execute": the family usage
    carries a `gopi task merge-execute` SYNOPSIS line — that verb's one advertised home is gopi merger -h
    cli_tasks_advert_gate_test.go:158: ledger group re-advertises "sync": … one advertised home is
    gopi task sync -h; keep the pointer sentence, not the synopsis
    cli_tasks_advert_gate_test.go:158: ledger group re-advertises "merger": the family usage documents
    the flag --daemon, which belongs to the service/git-infrastructure surface
--- FAIL: TestLedgerGroupTeachesNoServiceBlock (0.00s)
    cli_tasks_advert_gate_test.go:279: the family usage carries a service SYNOPSIS block
    (`gopi worker …` / `gopi merger …`) — a service is documented by its own page
FAIL	github.com/the operator/gopi/cmd/agent	2.068s
$ # restored (git status → clean for cli_tasks.go), re-ran:
ok  	github.com/the operator/gopi/cmd/agent	1.359s
```

Each of the three re-addition routes is independently caught, so the gate is
neither vacuous nor narrower than the invariant it names.

### 5. Defect found and fixed while moving `audit-merges` (Rule 15)

Writing the new `gopi merger -h` synopsis forced me to check that every flag I
was about to publish actually works. `audit-merges` did not:

```
$ gopi task audit-merges --repo ~/workspace/vt/repo
gopi task audit-merges: unknown flag --repo
```

Root cause (not a parameter issue): `taskFlagMember` had **no `AuditMerges`
case**, and the parse arm assigned **neither** `opts.repo` nor
`opts.submitAudit` — while the synopsis advertised `--repo/--submit/--db` and
`runTaskAuditMerges` consumed both fields. So every documented invocation was
refused. This is a master-side defect, found here because the restructure made
me read the synopsis as a *contract*. Fixed in place (both halves: the flag
table and the parse assignment), because publishing a synopsis that lies is
exactly what this epic exists to stop:

```
$ gopi task audit-merges --repo ~/workspace/vt/repo
merge-backlog audit (report-only): scanned=1 submitted=0 duped=0 skipped=0 failed=0
$ gopi task audit-merges --repo ~/workspace/vt/repo --submit
merge-backlog audit (submit): scanned=1 submitted=1 duped=0 skipped=0 failed=0
```

Regression guard (Rule 10.5): `cmd/agent/cli_tasks_advert_audit_flags_test.go`
— `TestTaskFlagMemberAuditMerges` (flag table) + 
`TestParseTasksArgsAuditMergesReadsItsFlags` (the documented invocation parses
and the values reach the options struct). Red-proof performed by reverting the
fix: `unknown flag --repo` + 3 membership failures; restored → `ok`.

### 6. Guards updated to the NEW invariant (never deleted, never relaxed)

| guard | old assertion | new assertion |
|---|---|---|
| `TestTaskCompletionNamesMatchDispatch` | dispatch set == completion set (one direction, `serve` skipped by name) | **bidirectional** derivation: every dispatch case label is declared by `DispatchVerbs`, every `DispatchVerbs` member has a case, and every label not in the ledger surface must belong to a **named group** |
| `TestTaskCompletionNamesMatchFamilyHelp` | documented set == completion set | documented set is a **superset**: completion ⊆ documented, and a documented verb outside completion must be a `SyncVerbs` member (everything else is the gate's business) |
| `TestTaskCompletionNamesAreTheSharedFamilySurface` | "contains" representative verbs | exact length+set equality with `FamilyVerbs`, **plus** exclusion of `SyncVerbs`/`MergerVerbs`/`ServiceAliases` |
| `TestTopLevelUsageTaskVerbEnumMatchesCompletion` / `TestTopLevelUsageShowsTaskLedger` | representative verbs (`adopt`, `merger`) | ledger verbs only; the row must **not** enumerate any `MergerVerbs`/`SyncVerbs` member |
| `TestNoProductionFileRestatesTheTaskVerbEnumeration` | one enumeration | unchanged (still passes; the enumeration is still derived) |
| `TestDispatchVerbsAreDerived` (taskverbs) | `FamilyVerbs ∪ {serve}` | `FamilyVerbs ∪ SyncVerbs ∪ MergerVerbs ∪ ServiceAliases`, plus a coverage assertion |
| `TestTasksSyncFamilyHelp` | family blob must *also* document `diff`/`--full` | the sync `-h` arm must print `syncUsageHead` and **not** append `tasksUsage`; the family body must not restate a sync flag |
| `TestMergerRestart_HelpDocumentsTheControlFlags` (e2e) | the family usage must document `--stop`/`--restart`/`--force` | the family usage must NOT restate any control flag but MUST point at `gopi merger -h`, and the deprecated spelling must still DISPATCH them (`gopi task merger --stop --status` is refused as read+act, not as an unknown flag) |

New `internal/taskverbs/verbs_test.go` tests: `TestSyncVerbsMatchConstants`,
`TestMergerVerbsMatchConstants`, `TestServiceAliasesMatchConstants`,
`TestFamilyGroupingsAreDisjoint` (the structural invariant: one home per verb).

### 7. Existing gated surface stays green

`internal/standards/baseline.go`: the Rule 16 permit for the `%w` site in
`cmd/agent/main.go` was **re-pinned, never deleted** — `917 → 954`, because the
merger/worker usage additions sit above it. Re-measured in the actual tree
(`grep -n 'escape a literal prompt' cmd/agent/main.go` → exactly one line, 954);
the gate reports 9 hits / 7 baselined / 1 escaped / **0 NEW / 0 STALE**.

## Not Done

- **A dedicated `gopi sync` top-level verb** was *not* added. The epic allows
  "their own `gopi sync` surface"; `gopi sync` does not exist as a top-level
  command today, and creating one would be a **new dispatch surface** — a
  17-tool/verb-surface change well outside "help/grouping restructure only"
  with zero new behaviour allowed. The sync surface's *help* is nonetheless
  exactly one place: `syncUsageHead`, printed by `gopi task sync|diff|harvest|
  push -h`. If a real `gopi sync` verb is wanted, it is its own task (it must
  also decide whether the `gopi task sync` spellings alias it).
- **`gopi worker` remains a separate top-level page** rather than moving under
  `gopi merger -h`: they are peers (one merges, one executes tasks), and merging
  their pages would create the very two-homes problem this epic removes. The
  ledger group now points at both.
- The e2e/`testsuite` trees beyond the named three packages were not swept for
  help-text assertions; the three required trees plus `internal/standards` are
  green.

## Verification

```
$ go build ./...
BUILD_OK
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
BIN_OK
$ go test -count=1 ./cmd/agent/... ./internal/taskverbs/... ./internal/config/... ./internal/standards/
ok  	github.com/the operator/gopi/cmd/agent	235.777s
ok  	github.com/the operator/gopi/internal/taskverbs	0.051s
ok  	github.com/the operator/gopi/internal/config	1.543s
ok  	github.com/the operator/gopi/internal/standards	12.208s
```

FINAL run, on the tree with master merged (`1f741097` → `b02ea536`):

```
$ go build ./...
go build ./... OK
$ go test -count=1 ./cmd/agent/... ./internal/taskverbs/... ./internal/config/...
ok  	github.com/the operator/gopi/cmd/agent	210.149s
ok  	github.com/the operator/gopi/internal/taskverbs	0.064s
ok  	github.com/the operator/gopi/internal/config	1.842s
$ go test -count=1 ./internal/standards/
ok  	github.com/the operator/gopi/internal/standards	7.909s
```

(One intermediate merged-tree run failed two `task_session_cwd_e2e_test.go`
cases: the STALE-BINARY guard, tripped because the gate red-proof had just
rewritten `cli_tasks.go` after the last build. Rebuilt → green. Reported here
rather than dropped: it is a guard firing correctly, and a reader who sees only
the green log should know the binary mtime is part of this suite's contract.)

(`internal/standards` includes the six machine gates — Rule 16 reports
9 hits / 7 baselined / 1 escaped / **0 NEW / 0 STALE** after the permit
re-pin.)

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
  → OK (binary rebuilt **before** the e2e probes; `bin/gopi` mtime checked —
  `task_session_cwd_e2e_test.go` fails loudly on a stale binary).
- Tests: `go test -count=1 ./cmd/agent/... ./internal/taskverbs/... ./internal/config/... ./internal/standards/`
  → `ok github.com/the operator/gopi/internal/taskverbs`, `ok …/internal/config`,
  `ok …/internal/standards`, `ok github.com/the operator/gopi/cmd/agent` (see the
  final run log in the commit message / `## Verification` of the report below).
- Acceptance checks on the shipped binary (all run, output quoted above):
  `gopi task -h` service/worker usage blocks = **0**; `gopi merger -h` + `gopi worker -h`
  parse (exit 0) and are the single complete help for their services;
  `gopi task sync -h` = the sync manual alone; every one of the 26 dispatched
  spellings re-run and shown dispatching.
- Gate red-then-green: `TestNoServiceOrSyncVerbInLedgerGroup` (shown FAIL, then ok).
- Regression red-then-green: `TestParseTasksArgsAuditMergesReadsItsFlags` (shown
  FAIL with the fix reverted, then ok).

## Files

- `internal/taskverbs/verbs.go`, `internal/taskverbs/verbs_test.go` — the four
  groups + derived union + disjointness/derivation tests.
- `cmd/agent/cli_tasks.go` — `tasksUsage` rewritten (ledger blocks only, pointers
  for services/git atoms/sync; `list --sweep` throttle text removed);
  `audit-merges` flag parsing fixed.
- `cmd/agent/cli_tasks_sync.go` — `syncUsageHead` is now the complete standalone
  sync manual.
- `cmd/agent/main.go` — `mergerUsageHead` documents the five git atoms + the
  sync pointer; `workerUsageHead` unchanged as the worker's one home.
- `cmd/agent/cli_tasks_advert_gate_test.go` (new) — the anti-readd gate.
- `cmd/agent/cli_tasks_advert_audit_flags_test.go` (new) — the audit-merges
  regression guard.
- `cmd/agent/cli_completion_test.go`, `cmd/agent/cli_tasks_test.go`,
  `cmd/agent/usage_test.go` — guards updated to the new invariant.
- `internal/standards/baseline.go` — Rule 16 permit re-pinned `917 → 954`.
