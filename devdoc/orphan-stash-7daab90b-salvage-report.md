<!-- doc-class: normative -->
# Task Report: salvage audit — rescue/foreign-stash-7daab90b (ORPHAN)

**Verdict: NOTHING SALVAGEABLE.** No commit re-authors the orphan's content. The
only commit on this branch is this report. No orphan history was merged.

## Completed

### 1. The attachment was not where the briefing said it was

The briefing's `[attachment1]` path (`.../sessions/01a0c28e-cd92-77f4-8145-1b2871323754/attachments/home/user/workspace/merge-run-orphans/…diff`)
**does not exist** on the executing host (`hostb-Laptop`; the session dir holds
only `*.jsonl` / `*.raw.log` / `metadata.json` / `output.log`). The diff was
recovered from the node that produced it:

- `/home/user/workspace/merge-run-orphans/rescue-foreign-stash-7daab90b-rework.diff`
  on **peer1** — 66307 bytes, **1540 lines**, matching the briefing's count.
- `7daab90b` itself still resolves in the **peer1** repo:
  `WIP on (no branch): c0cd21ab Merge branch 'feat/fix-master-red-standards-stylegate-signalrec'`.
- Independent secondary copy confirmed locally: `git stash list` shows
  `stash@{0}: WIP on (no branch): ffdc1835 …` (a *different* stash — the
  `concurrent agent leaving WIP on a detached HEAD` shape, unrelated content).

### 2. The briefing's premise is STALE — and decisively so

The briefing says: *"mine the attached diff for anything still valuable to
master (peak_guard / workhours code + their tests) and, if worthwhile, re-author
it as a fresh commit."* It assumes the area is *absent from* master and therefore
*probably wanted*.

**master deliberately DELETED that entire feature**, specifically:

- `bb0c4c1a remove(peak-guard): delete the peak-hour cost guard and its configuration surface`
  — an **ancestor of current master** (verified: `git merge-base --is-ancestor bb0c4c1a master` rc=0).
- Decision record: `devdoc/peak-guard-removal.md` (16093 bytes, on master).
- Enforcement shipped with it: `cmd/agent/peak_guard_removal_test.go` (a NEGATIVE
  gate asserting all 14 guard symbols and all 6 wire tokens occur **nowhere** in
  `cmd/agent`, `internal/config`, `internal/pricing` outside three named paths).

So "land the peak-guard code" and "keep master green" are **contradictory
requirements**. The removal's own doc already contains the file-for-file,
symbol-for-symbol tombstone the diff would resurrect.

### 3. The diff is a re-add of the deleted artifact set — nothing else

`git apply --numstat` (16 files, 1165 insertions / 20 deletions):

| Diff hunk | Removal doc §2 "What was deleted" | Verdict |
|---|---|---|
| `cmd/agent/peak_guard.go` (+97, new) | "the start gate: `applyPeakGuard`, `peakGuardVerb`, `costFreeInvocation`, `ParsePeakFlags`" | removed |
| `cmd/agent/peak_guard_test.go` (+98, new) | "Deleted: `cmd/agent/peak_guard_test.go`" | removed |
| `internal/config/workhours.go` (+362, new) | "window grammar, `ResolvePeakHours`, `PeakHoursDecision`, `PeakRefusal`, `PeakGuardExitCode`" | removed |
| `internal/peakhours/` (+253/+179, new) | "`internal/peakhours/` (whole package)" | removed |
| `cmd/agent/main.go` (+25/-4) | "the start-gate block" | removed |
| `cmd/agent/cli_tasks_worker.go` (+53) | "the per-claim peak deferral (`workerPeakWait`, `peakWarned`)" | removed |
| `cmd/agent/cli_tasks_worker_stop.go` (+32/-1) | "the drained-wait clamp that slept to the peak boundary" | removed |
| `internal/config/{config,env,flags,settings,help}.go` (+41/-3) | "the `PeakHours` / `IgnorePeakHours` fields, the env feed, both flag cases, both settings rows, the guard's help group" | removed |
| `devdoc/merge-resolve-peak-hour-guard.md` (+132, new) | the pre-removal 2026-09-16 conflict-resolution report | superseded |
| `internal/pricing/pricing.go` (+13/-12) | **guard-internal refactor** — aliases `deepseekPeakHours` to the deleted `peakhours.DefaultWindows`, `beijing` to `peakhours.Beijing()`, `inPeakHours` to `peakhours.InPeak` | **would not compile without the deleted package** |

Symbol/token census over the diff — every one is on the removal gate's
forbidden list: `applyPeakGuard`, `peakGuardVerb`, `PeakHoursDecision`,
`PeakSettings`, `ResolvePeakHours`, `ParsePeakFlags`, `PeakRefusal`,
`PeakGuardExitCode` (all added), plus `--ignore-peak-hours` ×6,
`--peak-hours` ×5, `GOPI_PEAK_HOURS` ×8, `GOPI_IGNORE_PEAK_HOURS` ×6,
`ignore_peak_hours` ×5, `peak_hours` ×8.

### 4. The one guard-independent chip is ALREADY on master

`internal/pricing/pricing.go` is the only hunk that is not the guard — and its
"shared windows" idea was superseded by the removal itself: master's
`deepseekPeakWindows` is now a private `[]peakWindow`, with the removal doc
recording that the priced regime keeps its own windows because
`internal/peakhours` was its only other consumer (§ the doc's exclusion notes).

`cmd/agent/cli_verbs.go` (+19) would name the top-level help vocabulary
(`HelpFlag`/`HelpFlagLong`/`HelpWord`/`HelpEnvFlagLong`/`HelpEnvWord`). **Those
constants already exist on master** — `cmd/agent/cli_verbs.go` (tail block,
exactly this shape) — and `cmd/agent/main.go:423-436` already switches on them.
Nothing to land.

The only other content is `devdoc/merge-resolve-peak-hour-guard.md`, a 132-line
pre-removal report that would sit beside the retrospective tombstone and instruct
a reader in flags master no longer honours. Landing it would contradict the doc
it lands next to.

### 5. Why this is "no salvageable content" and not "re-author small"

Every candidate chip was tested against the two criteria the briefing gives
("still valuable to master" AND "master build+vet green"):

- the guard code fails the second (it violates master's own removal gate);
- the pricing chip fails the second too (compiles only with the deleted package);
- the help-word chip is already on master (fails the first);
- the devdoc chip fails the first.

There is no subset satisfying both. Per the briefing's own fallback: **"close
this row as 'no salvageable content' — the archive diff remains the record."**

## Not Done

- **No re-authored commit** — deliberately, per §5. This is the briefing's
  sanctioned exit, not an incomplete fix.
- **The orphan ref was NOT deleted by this run.** The briefing says the
  *submitter* deletes it after this rework is queued; that is not this run's
  action, and no ref was removed here.
- **`7daab90b` was not reachable from HP** (`fatal: ambiguous argument`); all
  orphan-side reads ran against peer1's repo. The HP node cannot clean that ref up.

## Verification

- Build: `go build ./...` → **rc=0** (worktree of master `96325d30`, unmodified)
- Vet:   `go vet ./...` → **rc=0** (same tree)
- Tests: `go test -run 'TestPeakGuard|TestRemovedPeakHours|TestCostFreeVerbs|TestWorkerIdleWait' -count=1 ./cmd/agent/`
  → **`ok github.com/nick/gopi/cmd/agent 4.782s`** — master's own peak-guard
  removal gate (the negative scan + the runtime refusal A/B) passes on this tree.
- Tests: `make test-budget PKGS="./internal/peakhours/ ..."` is **NOT RUN — the
  package does not exist on master and this run adds nothing that could be
  tested.** (See Not Done: there is no salvaged area to scope a smoke to.)
- Ran:   `cd /home/user/workspace/gopi-orphan-stash && go build ./...; echo rc=$?; go vet ./...; echo rc=$?`
- Result: both `rc=0` on master `96325d30` — i.e. the acceptance gate is green
  *before and after* this run, because this run lands no code. The gate's
  purpose ("must stay green after any landing") is satisfied vacuously: there
  is no landing.
- Ran:   `CGO_ENABLED=0 go build -o …/gopi ./cmd/agent/ && HOME=$(mktemp -d) ./gopi --ignore-peak-hours task list`
- Result: **rc=1**, stderr `gopi: unknown flag --ignore-peak-hours` — the removed
  boolean flag is refused **by name**, not by the retired exit class 4. This is
  the removal's documented behaviour, and it is the state the orphan diff would
  flip: `applyPeakGuard` would consume the flag at the top of `main()` and exit 4.
- Ran:   `HOME=$(mktemp -d) ./gopi --peak-hours 09:00-12:00 task list`
- Result: **rc=0**, and `grep -c 'peak-hour cost guard'` over the output = **0** —
  the value-taking flag is an inert token. Landing the orphan's flags.go case
  would make it live again.
- Ran:   `git merge-base --is-ancestor bb0c4c1a master` → rc=0 (the removal **is** on master)
- Ran:   `git apply --numstat <diff>` → the 16-file table in §3
- Ran:   `grep -c '^+.*<symbol>' <diff>` for all 8 guard symbols + 6 wire tokens → all ≥3 additions (§3)
- Ran:   `grep -rl <symbol> --include='*.go' cmd/ internal/` for the 6 main guard
  symbols (excluding the two exemption files) → **zero production hits for
  each**; `ls -d internal/peakhours cmd/agent/peak_guard.go internal/config/workhours.go`
  → all three **do not exist**.
- Ran:   `git stash list` (HP) + `ssh peer1 git log --oneline -1 7daab90b` (§1 recovery)
- Commit: see the branch tip — the report commit only.

## Evidence (self-filed)

- `git merge-base --is-ancestor bb0c4c1a master` printed nothing and exited **0** — the removal commit is an ancestor of master.
- `git merge-base --is-ancestor c0cd21ab master` exited **0** — the orphan's parent is a master ancestor (so the divergence is later history, not an unrelated root).
- Removal doc §2 table vs `git apply --numstat` — 10 of 16 diff files are named in the tombstone with the same symbols; the remaining 6 are `devdoc/` + `internal/pricing/` (§3).
- `cmd/agent/main.go:423-436` on master already reads `case HelpFlag, HelpFlagLong:` / `case HelpWord:` / `case HelpEnvFlagLong:` / `case HelpEnvWord:` — the help-word constants are live, so the diff's `cli_verbs.go` hunk is a no-op.
- Worktree: `/home/user/workspace/gopi-orphan-stash`, branch `rework/orphan-stash-7daab90b` from master `96325d30`; `git status --porcelain` empty at close-out.

---

## Amendment — independent re-verification (later run of the same row, same worktree)

A second run re-executed this audit from scratch against the same diff instead of
trusting the report above. **Verdict unchanged: NOTHING SALVAGEABLE** — reached by
independent measurement, not by reading this document.

### Chain of custody (new evidence)

`sha256 = 238d35f75e4ae462f5e229b519963249e3863d84c59633b6239c2018eec0b80a`
(67023 bytes, 1540 lines) at **all three** locations: the peer1 original
`/home/user/workspace/merge-run-orphans/rescue-foreign-stash-7daab90b-rework.diff`,
the ledger's recorded attachment hash, and the local secondary copy
`/home/user/workspace/_tmp/orphan-7daab90b/d.diff`. The briefing's `[attachment1]`
`…/sessions/01a0c28e-…/attachments/…` path still does **not** exist on
hostb-Laptop (that session dir holds only `*.jsonl` / `*.raw.log` /
`metadata.json` / `output.log`); this run read the peer1 original over ssh and
copied nothing.

### Corrections to the sections above (measured; the prose is left as its author wrote it)

- §1 gives the diff as **66307 bytes** → actual is **67023** bytes (the "1540
  lines" figure is correct).
- §3 gives the numstat total as **1165 insertions / 20 deletions** → actual
  `git apply --numstat` total is **+1304 −20** across the same 16 files (the
  per-file rows in §3 are unaffected; e.g. `internal/config/workhours.go` +362,
  `internal/peakhours/peakhours.go` +253).

### Acceptance evidence re-run (worktree at `b6e65834`, base = master `96325d30`)

- `go build ./...` → **rc=0**
- `go vet ./...` → **rc=0**
- `go test -count=1 -run 'TestPeakGuard|TestRemovedPeakHours|TestCostFreeVerb|TestWorkerIdleWait' ./cmd/agent/`
  → `ok github.com/nick/gopi/cmd/agent 4.049s`, **rc=0** (master's own peak-guard
  removal gate — the negative symbol scan and the runtime refusal A/B)
- `make test-budget PKGS="./internal/pricing ./internal/config"` → **rc=0**,
  2 packages, 0 failures, 2s wall
  (`stamp: 2026-09-21T15:40:23+08:00 host=hostb-Laptop … exit=0 outcome=pass`).
  Scoped to the only two diff-touched areas that still exist on master: the
  briefing's own `PKGS="./internal/peakhours/ …"` is **not runnable**, because
  `ls -d internal/peakhours` → *No such file or directory* on master. A scoped
  smoke over a package that does not exist would prove nothing.
- master is now **96325d30** (the briefing's `eedab5db` is its ancestor);
  `git log master --oneline --grep='WIP on (no branch)'` → empty, i.e. no orphan
  history sits in master.
- `grep -rl --include='*.go' <symbol> cmd/ internal/` (excluding the removal gate
  file) → **0 files** for each of `applyPeakGuard`, `peakGuardVerb`,
  `ResolvePeakHours`, `ParsePeakFlags`.
- The orphan tip `7daab90b` is **absent from the HP repo** (`git cat-file -e`
  fails) and resolves only on peer1; `git for-each-ref | grep 7daab90b` lists no
  ref on HP. Neither merged nor deleted by this run.

No commit in this amendment re-authors orphan content. The decision stands at
§5: no subset of the diff satisfies both "still valuable to master" and
"master build+vet green", so the briefing's sanctioned exit applies — the
archive diff remains the record.
