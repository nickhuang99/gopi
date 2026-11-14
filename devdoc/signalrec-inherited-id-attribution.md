<!-- doc-class: historical -->
# Devdoc: an inherited `GOPI_TASK_RUN_ID` is executor IDENTITY, not record OWNERSHIP

Task: `01a12f76-c01f-77eb-8dcf-ccd21a7a53b9` (epic reframe verdict DOABLE).
Branch: `task/01a12f76-c01f-77eb-8dcf-ccd21a7a53b9`. Base: master `d4542073a`.

## The defect, measured before it was explained

`gopi memory sync --every 5s --push` launched **by an agent session** and pointed
at a **non-default profile** (`GOPI_HOME=<sandbox>`) inherits that agent's
`GOPI_TASK_RUN_ID`. `internal/signalrec.sessionDest` resolves the record's
destination most-specific-first — handoff → carried ledger id → process-keyed dir
(resident only) → refusal — and step 2 TRUSTED the inherited id: it joined
`<SessionsRoot>/<id>`, that container cannot exist in the sandbox profile, the
role (`memory`) is not in `modesThatOwnTheirSignalHandlers`, so the recorder
**refused** to file the signal-provenance record.

Nothing outside said so. Measured, real binary, real sandbox, real signal
(`artifacts/prefix-run1.log`, `artifacts/prefix-sigusr1.log`):

| observation | value |
|---|---|
| exit code after SIGTERM | **0** |
| the loop's own stop line | printed |
| signal-provenance record on disk | **absent** |
| the refusal, when surfaced (SIGUSR1) | `signal provenance NOT FILED (no public-dir fallback): signalrec: session dir "<sandbox>/.gopi/sessions/01a12f76-…" for task 01a12f76-… is missing or unusable — refusing to write …` |

The refusal is *correct* on its own terms: that id names a container the sandbox
profile never minted, and it is not this process's session to file under. What was
wrong was the OUTCOME — the record was filable one branch away, and the loss was
invisible from the exit status.

**The differential that made the two halves separable, and it is the reason this
work needed two fixes.** `SIGUSR1` is watched by the recorder but is *not*
selected on by the verb's own handler. Under SIGUSR1 the refusal line appeared;
under SIGTERM it did not. That is not a wording problem — it is the delivery race:
the recorder files on its **own goroutine**, woken by the same delivery, and the
verb's handler unwinds and calls `os.Exit` first. Measured with the destination
**pre-created and unambiguously writable** (a real `metadata.json` container):
`memory sync --every` lost its record in **3/3** SIGTERM runs. So a fix that only
repaired attribution would still have shipped a guard that stayed red for a second
reason.

## The design decision: identity, carried — ownership, decided here

The epic asked for exactly this call, and it is the whole of the fix:

- **`GOPI_TASK_RUN_ID` is EXECUTOR IDENTITY.** It is CARRIED into the record
  (`task_run`), because "this process descends from that run" is a true and useful
  fact even when the profile is a sandbox. It is never discarded.
- **It is NOT a RECORD OWNERSHIP CLAIM.** The moment its container is unusable in
  *this* profile the id stops deciding *where the record goes*. The directory the
  record is filed in belongs to the profile the process actually reads and writes;
  an id inherited from another context may describe an ancestor, never a
  destination.
- **What may replace it is a fact about THIS process, not another id.**
  `ClaimSignalHandlers()` is this process's own in-process declaration (never
  argv) that it installed a handler and will unwind GRACEFULLY — and a graceful
  unwind is exactly the shape whose record is the LAST thing it writes.

So `internal/signalrec` gains ONE predicate, `processLayoutEligible(label)`:
`!forwardsDefault(label) || handlersClaimed.Load()`. `sessionDest` step 2 keeps
the refusal **unless** that predicate admits the process-keyed layout; when it
does, the record is FILED there and the record's `note` says which arm admitted it
(`… mode memory claimed its own signal handlers (ClaimSignalHandlers) …`). The
note is a named constant (`signalrec.ClaimMarker`) so a guard asserts the arm
rather than a wording.

### Why the claim and not "any one-shot gets a bucket"

The counter-example is pinned in the tree, and it is why this had to be narrow:
`gopi merge --every D` must **not** mint a process-keyed bucket when it is killed
(`cmd/agent` `TestMergeEveryKilledLoopLeavesNoResidueInTheProfile`, the regression
for the retired `merger` argv alias). That loop never calls
`ClaimSignalHandlers`, so it does not acquire the arm — and this change does not
widen the old alias back into existence. The rule of thumb: **the disposition is
argv-level and free; the attribution is a claim, and a claim implies the door-side
wait below** (a process that claims must also wait, or it has asked for an
attribution it will not live to write).

## Delivery-point merge: master landed the RACE half in parallel

At the delivery-point sync (merge of master `c504b41de`) it turned out a SIBLING
task had independently landed the second half of this seam:
`devdoc/signalrec-record-filed-handshake.md` — `noteRecordFiled()` published at
`write()` success, `DefaultRecordFiledWait = 500 ms`, and
`cmd/agent/signal_verdict.go settleProvenanceRecord` at every claiming door. Its
publication placement is BETTER than this row's (`write()` returns nil ⟹ the bytes
are on disk is a stronger fact than the recorder's `werr == nil`), so the merge
kept it and this row's contribution is exactly the piece master does not have:

- **the ATTRIBUTION arm** — `processLayoutEligible` + `ClaimMarker`: a claiming
  NON-resident one-shot (a `memory sync --every` loop) becomes eligible for the
  process-keyed layout instead of the step-2 REFUSAL that lost the record in a
  foreign profile in the first place. Without it the handshake has nothing to
  settle on: the record is still refused (measured on the pre-fix substrate: 5/5
  `LOST-refused`).
- **the citation of the layout in the record** (`ClaimMarker` in the note), which
  the guard asserts, plus the guard itself (real binary, real profile, real
  SIGTERM, 3 repeats, per-pid lookup).
- The door-side wait now calls master's ONE helper (`settleProvenanceRecord`)
  rather than this row's private `AwaitRecordFiled(error)` signature — one door,
  one wait, no second spelling.

**A gap the merge made visible, filed rather than absorbed** (Rule 41): master's
publication happens ONLY after a successful append, so a **refused** record never
settles — the door's wait burns its budget and then prints `recordLostLine`,
a FALSE "record lost" for a record that was deliberately refused with its own
printed reason. Evidence and the code path:
`artifacts/rule41-refusal-not-settling.log`; filed as bug row
`01a12fba-6942-770d-acc9-a01c3fb994da`. The resolution needs a THIRD state
(`filed` / `refused with its reason` / `nothing yet`) so the bytes-on-disk fact
stays unpolluted — deliberately NOT done here, because it is a different seam
(master's handshake, its own acceptance) and this row is at its budget.

The merged tip is `067bd1f7a` (merge of `c504b41de`); the targeted run on that tip
(`artifacts/merged-targeted-run.log`, 2026-10-12T18:46:32+08:00)
→ `ok github.com/nick/gopi/internal/signalrec 2.312s`,
`ok github.com/nick/gopi/cmd/agent 21.955s`, `TARGETED_EXIT=0` — this row's guard,
master's `TestSignalRecordFiled…` guard and the merge-loop residue guard all green
together.

## The second half: the door-side wait

`signalrec.AwaitRecordFiled(budget)` + `noteRecordSettled(ok)` close the race, in
ONE place and with no new artifact (Rule 35 safe):

- the recorder's handler publishes its OUTCOME after `write()` returns — including
  a refused write, because a refusal is an ANSWER (its line has already been
  printed) and burning the budget for a write that will never happen only delays a
  decided exit;
- the door (`cmd/agent/cli_memory_sync_every.go`, after the loop's own stop line,
  before `os.Exit`) waits for that outcome and **reports** a genuine loss (Rule 15:
  a silently missing record is the defect, one level down);
- a process that was NOT signalled pays only the delivery settle (25 ms) and
  leaves — the ordinary exit is not slowed by a wait that can never succeed;
- `RecordFiledBudget = 1500 ms` is BOUNDED, so a wedged recorder cannot make the
  process unkillable.

## Measured, before and after (real binary, real profile, real SIGTERM)

Rig: `gopi tool sandbox create` profile (Rule 34), 1 round per run, `--every 5s`,
SIGTERM at the wait; the record is looked up **by the victim's pid**, so a stale
record cannot be mistaken for this run's.

| tree | repeats | records landed |
|---|---|---|
| pre-fix (`go build -overlay` of `HEAD:` sources, same rig) | 5 | **0/5** |
| post-fix | 5 | **5/5** |

The acceptance was then RE-TAKEN with a binary built from the committed HEAD
itself, so the log provably corresponds to the commit and not to a warm
`bin/gopi` left by an earlier iteration: `artifacts/acceptance-final-n5.log`
(2026-10-12T18:42:41+08:00, HEAD `2c1dc0aaf`, worktree dirty entries 0,
`sha256=9b77bb8dc30fca1a`) → `RESULT=5/5 landed (binary from committed HEAD
2c1dc0aaf)`. The red proof was re-taken over the text that actually shipped
(`artifacts/guard-redproof-rerun.log`): 3/3 FAIL on the pre-fix binary. Neither
proof mutates the worktree (Rule 30/31: the pre-fix sources reach the build
through `go build -overlay`).

Post-fix record (an excerpt, `layout:process`, id carried, arm named):

```json
{"signal":"SIGTERM","pid":93602,"role":"memory","layout":"process",
 "task_run":"01a12f76-c01f-77eb-8dcf-ccd21a7a53b9",
 "session_dir":"…/gopi-sandbox/sandbox-sigacc-b4a0475b/.gopi/sessions/process-93602",
 "note":"ledger id 01a12f76-… has no usable session dir (…) and mode memory claimed its own signal handlers (ClaimSignalHandlers) — process-keyed session dir; the id could not be honoured"}
```

## Traps hit while doing this (so the next reader does not pay them)

1. **`SIGTERM` alone cannot show a destination defect** — the verb's handler wins
   the race and even the refusal line is lost. Use `SIGUSR1` (watched by the
   recorder, not selected on by the verb) to separate "refused" from "raced".
2. **`go test` from a gopi session inherits the session's env** — `cmd/agent`'s
   mark-dirty TestMain intentionally keeps `GOPI_TASK_RUN_ID`/`GOPI_MEMORY_DB`.
   The guard therefore pins its own env AND runs the real binary, so the heredity
   is stated by the guard instead of inherited by accident.
3. **A round that reaches a real host holds the run open** — the probe uses a
   hermetic settings file whose only peer alias cannot resolve, so a round is one
   failed ssh and the loop is back in its wait.
4. **Doing a red proof by overwriting production files cost me the work.** The
   disk filled mid-command, the backup `cp` had already failed, and the restore
   never ran: two production files were silently reverted and I had to rewrite
   them. The red proof now uses `go build -overlay` (the pre-fix sources live in
   the scratch tree and NOTHING in the worktree is touched). Do not mutate the
   worktree to prove red.
5. **The host root filesystem was 100% full** (unrelated to this task — a 136G
   `~/.cache/go-build`); git index writes fail with "No space left on device" and
   the failure is easy to misread as a code problem. `go clean -cache` recovered
   136G.

## Files

- `internal/signalrec/signalrec.go` — `processLayoutEligible`, the step-2
  fall-through arm + auditable note, `ClaimMarker`, `noteRecordSettled`,
  `AwaitRecordFiled`, `RecordFiledNow`, the three timing constants.
- `cmd/agent/cli_memory_sync_every.go` — the door-side wait at the loop's exit.
- `internal/signalrec/signalrec_test.go` — unit guard for both arms of the
  predicate (with the claim / without it).
- `cmd/agent/memory_sync_signal_record_test.go` — the real-binary, real-profile,
  real-SIGTERM guard (N repeats, asserts WHICH attribution was used).
- `cmd/agent/merge_every_child_test.go` — the residue assertion narrowed to
  tolerate exactly the recorder's own record (and nothing else).
