<!-- doc-class: historical -->
# Unowned-zombie rows: the third arm of the zombie leg (task 01a12b01)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

## The defect (measured, 2026-10-11, hosta ledger)

Two `2026-09-11` auto rows sat `in_progress` for **thirty days** and no arm would
ever dispose of them:

```
#01a08e9b-4573-7026-a6fc-d08e59f0553e  in_progress  claimed_by=peerbox:3824874
#01a08e7f-ccaa-7e1c-a2e0-d35ada3c18fd  in_progress  claimed_by=peerbox:3714484
```

Both refusals are individually correct, and jointly they are a permanent hole:

| arm | why it refuses this row |
|---|---|
| `SweepZombies` (`taskzombie_sweep.go`) | disposes only a **confirmed** zombie; confirmation is the claimed pid's liveness (`zombieVerdict` → `claimHolder`), and `claimHolder` deliberately refuses to resolve a **foreign** pid ("its pid means nothing here"), so a cross-host claim can only ever be `suspected` — never killed |
| `AutoRunLitterSweep` (`taskzombie_extinct.go`) | `autoRunLitterHolderLive` returns **live** for `host != ""` ("unknowable here, never called dead") — for a *disposer* that means forever-live |

Evidence for the rendered contradiction (before the change), on the live hosta
ledger — the row is `in_progress` yet its state label says `died`:

```
$ gopi task list --status in_progress
#01a08e9b-… [in_progress]  [auto] cost-probe … [died 720h0m0s since 22:15:25]
```

## What shipped

1. **`internal/tools/taskzombie_unowned.go`** — a pure predicate
   (`unownedZombieIsOrphan`, over the measured fact set `unownedZombieSignals`)
   plus its disposer (`UnownedZombieSweep`). The predicate decides on an
   **absence** argument, never on liveness:

   - claim stamp present but **not attributable here** — the render ladder's own
     `taskClaimAttributable`, so the label and the verdict cannot drift (D);
   - **no dispatch ground** (`deliverable.worktree` empty);
   - **no run directory** (`sessions/<task_id>` — measured by the same
     `taskProbeRowState` the list renders from);
   - **no session directory** (`sessions/<session_id>`);
   - **no live holder** — the claim pid (when this host can read it, e.g. a
     *previous* claimant's) and the row's recorded runner identity
     (`finishCleanupProcessHolderLive`);
   - **age ≥ the EXISTING floor** `autoRunLitterMinAgeSec` (Rule 2/13: no new
     configuration key).

   `updated_at` is deliberately **not** an input: it is the write-churn column the
   stale re-arm bumps (`taskZombieBumpSQL`), so it says nothing about liveness
   (memory #8179 — the reason this epic was rewritten twice).

2. **Disposal route** — `cmd/agent/cli_tasks_worker.go`
   `autoDisposeConfirmedZombies` (the same leg `SweepZombies` and
   `AutoRunLitterSweep` already ride: same grace, same role, same tick, Rule 35 —
   no new tick, script, daemon or key).

3. **`updated_at` preserved.** The write goes through the ONE update path,
   factored into `taskUpdateStampedCore` with a `preserveUpdatedAt` flag (rather
   than a second writer, Rule 14). The row becomes **`cancelled`**, never
   `completed`: the evidence is an absence set measured on ONE host, and it can
   never certify a delivery (Rule 30's edge, and the deliverable's own words —
   "cancelled, not completed"). `ended_at` IS stamped; the audit event and the
   notify bus are unchanged; the row is never deleted.

4. **The reason states the limit out loud** and `tools.UnownedZombieHostNote()`
   repeats it on the tick's own line:

   > TERMINATED as cancelled, not completed: these are absences measured on one
   > host, which cannot certify a delivery. **LOCAL-FACT LIMIT**: the claiming
   > host's own disk was not read.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK.
- Tests (scoped to the diff, Rule 27's smoke posture):
  `go test ./internal/tools/ -run 'TaskZombie|Zombie|AutoRunLitter|Stale|TaskUpdate|TaskHeartbeat|TaskProbe|TaskState|TaskList|Lifecycle'`
  → **ok, 4.257s**; `go test ./cmd/agent/ -run TestTaskLifecycleTick_…` → **ok**.
  A bare WHOLE-PACKAGE run (`go test ./internal/tools/ ./cmd/agent/`) is
  **not green on this host for reasons that predate this diff**
  (`evidence/`): `ctags` and `rg` are not installed, so every code_search /
  keyword_search test fails with `executable file not found in $PATH`
  (`TestQueryDefinitionSelfHealsUnsearchableIndex`, `TestCodeSearchBackendNoSymbol`,
  `TestKeywordSearchRunPrintsHitsAndNoMatches`, …), and the merge BUILD gate
  tests fail because the suite's scratch root is `/dev/shm` (tmpfs) so the gate
  ABSTAINS by design — the message names the 2026-09-15 incident. Both failing
  test files reference none of this diff's symbols (`grep -c` = 0). The declared
  entry point `make test-budget PKGS='./internal/tools ./cmd/agent ./internal/config'`
  was launched and left running past 11 minutes without writing to its stream
  (killed); the direct runs above are the evidence I actually have.
- Ran (real run, Rule 31) — `~/.gopi/sessions/01a12b01-1c35-71cf-a554-b578dad6c6a5/evidence/acceptance_unowned.sh`
  + `acceptance2_livecontrol.sh` + `acceptance3_render.sh`, driving the EXISTING
  carrier `gopi worker --once` inside a `gopi tool sandbox` profile:

```
SEED-BEFORE t-acc-unowned (in_progress, updated_at=1.79172801762731051444e+09, peerbox:3824874)
[worker] [zombie-dispose] t-acc-unowned cancelled (unowned zombie, claimed by peerbox:3824874 — … LOCAL-FACT set …)
AFTER  t-acc-unowned (cancelled, quote(updated_at)=1.79172801762731051444e+09, ended_at=1791728117.0)
```

  — the `updated_at` value is **byte-identical** to the seeded one, while the
  ordinary arm on the same pass *did* move it (`t-acc-live`: `→1791728117.0`),
  which is the control that proves the preservation is real and not an artefact
  of a no-op write.

- Render face (C/D), same sandbox:

```
#t-acc-live3          [in_progress] … [running 720h0m0s since 22:15:36 + heartbeat stale 720h0m0s]
#t-acc-render-foreign [in_progress] … [unknown: claim stamp names another host, age 720h0m0s]
```

  and after the pass the live row was still `in_progress` with its `updated_at`
  untouched (`1.79172803692286705971e+09` both before and after), while the
  foreign-claim row was `cancelled` with the same value in both reads.

- Red proof (the test fails when the fix is neutered — `unownedZombieIsOrphan`
  forced to `return false`):

```
task_lifecycle_unowned_test.go:110: the cross-host zombie row is "in_progress"
after the tick, want cancelled — this is the reported defect
```

  and the second red proof (preservation flag flipped to `false`):

```
taskzombie_unowned_test.go:183: updated_at must be preserved verbatim:
before=1789136074.0 after=1791728074.0
```

- Real ledger (read-only): `gopi task list --status in_progress` on hosta —
  4 rows, all rendering `running … since …` under their own live worker; the two
  reported rows are **cancelled** by the operator's own earlier disposal
  (`ended_at=1791722718/20`, reason "30-day stale auto cost-probe … cancelled, not
  completed"), which is the same verdict this arm reaches automatically.

### The division of labour, measured (no double-claiming)

The four arms do not overlap, and the boundaries were exercised on real rows in
the same sandbox (`evidence/acceptance2…`, `acceptance3…`, `acceptance4…`):

| row shape | arm that acts | verdict |
|---|---|---|
| claim stamp names a LIVE local pid (`t-acc-live3` during part 3) | none — vetoed | stays `in_progress`, `updated_at` untouched |
| claim stamp names a DEAD local pid (`t-acc-live3` after the holder was killed, part 4) | `SweepZombies` (pre-existing) | `failed: zombie disposal (confirmed, stalled)` |
| claim stamp names ANOTHER host (`t-acc-unowned`, `t-acc-render-foreign`) | **this arm** | `cancelled` (unowned zombie), `updated_at` preserved |
| auto row, never started | `AutoRunLitterSweep` (pre-existing) | `cancelled` |

The stale-local row is deliberately NOT this arm's: `claimHolder` resolves the
pid, so the stamp IS attributable and the pass returns early (`ClaimAttributable`
→ keep). The confirmed-zombie arm owns it and its verdict is stronger than an
absence argument. That is why the predicate's first gate is the render's own
`taskClaimAttributable` rather than a hand-rolled host comparison.

## Not Done (honest)

- **The peer measurement is NOT part of the code path.** "The claiming host's
  disk was not read" is a stated limit, not a wish: disposing on a foreign claim
  means the *local* absences are the whole evidence (the arm must work with no
  peer reachable at all). A row whose runner is genuinely alive on peerbox while it
  created nothing here yet (no ground, no run dir, no session, 60 s+ old) WOULD
  be cancelled. The four vetoes make that window small (a real dispatch allocates
  a ground first — measured on every row in this ledger) but it is not closed,
  and closing it would need a peer query the disposer cannot depend on.
  **Checked by hand for the two reported rows** (`evidence/peer_peerbox_check.txt`):
  on peerbox both claimed pids are ABSENT (`/proc/<pid>` gone — so they were not
  long-lived workers) and neither row has a run directory there, i.e. for these
  two rows the local verdict happens to be the true one. That is corroboration,
  not a guarantee, and it is deliberately not wired into the pass.
- **The two reported rows were already terminal** by the time this branch was
  written (cancelled by hand on 2026-10-11), so the acceptance used **seeded**
  rows of the exact shape rather than the originals; the originals' shape was
  read back from the ledger and is reproduced field for field in the fixtures.
- **Self-inflicted ledger litter (declared).** The FIRST acceptance run used the
  retired verb `gopi sandbox create` (now `gopi tool sandbox create`); it printed
  its redirect on stdout/stderr and **exited 1**, but the script captured the
  value with `SB=$(… | tail -1)`, which keeps only stdout and drops the exit
  status — so `SB` came back EMPTY (evidence:
  `evidence/retired_verb_empty_stdout.txt`), and my script's `GOPI_HOME=""`
  therefore sent two
  `task submit`s into the **operator's live ledger** (`t-acc-unowned`,
  `t-acc-live`). A resident worker claimed both within seconds and failed them
  (reason: the short ids cannot name a ground). They are visible in
  `gopi task list`'s `failed-needs-reissue` line. The scripts were fixed to use
  the current verb AND the real sandbox path is now hard-checked; the two rows
  cannot be un-failed (`failed` has no `cancelled` edge) and are left as audit
  litter, named here rather than quietly ignored.
- **The mechanism behind that litter was filed as a bug** (Rule 41 —
  `gopi task submit --tags bug` on peerbox, the host that owns the system layer,
  Rule 28): **`01a12b5c-6173-73e0-9918-3da01416ebc9** — `task submit` accepts a
  non-uuid `--task-id`, the worker then fails the row at dispatch (no ground name
  can be derived). Evidence: `evidence/bug_shortid.md`. Reproduced on the
  sandbox: `Task queued: t-foo (status=pending …)` exit 0.

## Files

- `internal/tools/taskzombie_unowned.go` (new)
- `internal/tools/taskzombie_unowned_test.go` (new)
- `internal/tools/tasks.go` — `taskUpdateStampedCore` + `taskUpdatePreservingUpdatedAt`
- `cmd/agent/cli_tasks_worker.go` — the arm on the existing disposal leg
- `cmd/agent/task_lifecycle_unowned_test.go` (new) — the lifecycle-carrier repro

## Evidence

- `unownedZombieIsOrphan` unit ladder: `go test ./internal/tools/ -run UnownedZombie` → ok.
- Lifecycle carrier: `go test ./cmd/agent/ -run TestTaskLifecycleTick_CancelsTheCrossHostZombieRow` → ok.
- Preservation control (the diag run): `before=1789135930.0 after=1789135930.0` for
  the preserving path, `before=1789136074.0 after=1791728074.0` for the ordinary one.
