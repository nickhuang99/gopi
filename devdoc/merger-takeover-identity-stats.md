<!-- doc-class: historical -->
# The take-over you can identify, and the round that reports itself

**Epic:** merger take-over — three-factor identity + throughput statistics in
`gopi merger --status`.
**Branch:** `feat/merger-takeover-identity-stats` (worktree
`~/workspace/gopi-takeover-id-stats`), based on `master` `bd621dc0`.

## The two questions the old surface could not answer

`gopi merger --status` rendered a live take-over as one line:

```
pid 294157 running 14s
```

Two questions were unanswerable from it:

1. **Which process is this, really?** `pid` alone is a number the kernel reuses.
   The probe matched on **pid + argv[0]** — a name. A recycled pid with the same
   name is indistinguishable from the take-over, and the name is settable by any
   caller (`exec -a`).
2. **What has it DONE, and is it still working?** Nothing recorded throughput.
   An operator watching a long round could not tell "landing branches steadily"
   from "stuck on one branch for 40 minutes", and the zombie-kill judgement had
   no liveness input at all.

## What changed

### ① Identity: the three factors, measured from `/proc`

The take-over line now carries **pid + starttime + command line**, and names any
factor it could not read instead of substituting a weaker one:

```
takeover:    pid 339938+starttime 2026-09-18T06:20:50+08:00+cmdline "gopi-merger-takeover 300" running for 9s; …
```

- **Avoid the trap already in the tree.** `internal/tools/taskproc.go` already
  had the reusable ladder (`procStartTicks`, `procCmdline`, `taskIdentityVerdict`)
  and the reason the third factor matters: `pid+starttime` is what an exec
  changes, `pid+starttime+cmdline` is what a task row records. The probe now
  consumes **those readers**, not a second parse of `/proc` (Rule 2).
- **`argv[0]` is not the cmdline, and the line says so.** The old line printed
  `argv[0]=gopi-merger-takeover`, which reads as an identity factor. When the
  cmdline is unreadable the render says `+cmdline UNREADABLE (only its argv[0]
  "…" was read — the factor this probe matched on, not an identity)`.
- **argv[0] remains the DISCOVERY filter** (which pids to look at) — it is
  unchanged, and the identity factors are what the line asserts.

### ② Progress: the round reports itself, in its own control directory

A new `gopi merger takeover-progress <verb>` face, and a line-oriented
`takeover-progress.txt` record next to the take-over log:

```
gopi-takeover-progress v1
pid=340394
start_ticks=556126056
cmdline=gopi-merger-takeover 300
started=2026-09-18T06:09:00+08:00
updated=2026-09-18T06:21:00+08:00
state=working
interval=5m0s
processed=3
ff_merged=2
resolved_merged=1
rework_filed=0
skipped_protected=0
undecidable=0
remaining=2
last_branch=feat/hard
```

Rendered into the status line:

```
… running for 11s; progress: stage=working processed=3 (ff-merged=3 resolved=0
rework-filed=0 skipped=0 undecidable=0) remaining=4; 2 interval(s) of 5m0s
elapsed so far; last update 1s ago; …
```

### Design decisions, and why the obvious alternative lost

**Why a file, not a DB or JSON.** The two questions the record answers are
*throughput* and *still-working*. The record is rewritten per concluded branch,
so a partial write is a live possibility, and the format had to survive one: a
line-oriented `key=value` record keeps every line already written, and the reader
**names the fields it did not find** rather than defaulting them to zero. (A
JSON blob cut mid-write parses as nothing, and a counter defaulted to 0 renders
as "0 merged" — a measurement of a round that may have merged plenty.)

**Why the writer is the AGENT, not a Go wrapper.** There is no Go harness around
the round's judgement: the round *is* an agent session following
`merger_takeover_prompt.md`, and its branch outcomes are that session's. A
periodic Go heartbeat around the child would report the **wrapper's** liveness,
not the round's — and the question is "is it still concluding branches". So the
writer is a tool the session calls (prompt §3.10.1), with the honest caveat
stated in the code, the prompt and `--status`: **a record exists only if the
session writes one**, and its absence renders as a named state (`progress:
none`), never as zeroes.

**Why the record is created by the process and only AMENDED by the verbs.** One
slot, one file name. If *any* command could create it, an ordinary gopi could
claim to be a merge round. So:
- creation happens in exactly one place — `mergerTakeoverBriefing()`, the moment
  a process proves it is the take-over, gated on the **same** predicate that
  gates the lock adoption (`mergerTakeoverIdentity()`);
- every verb **refuses** when no live round owns the record (measured: exit 1,
  with the reason, and no file created);
- an amendment **rewrites the round's own identity**, never the caller's — the
  verbs run in *child* gopis, so a writer stamping its own pid would make
  `--status` declare the round's own record "NOT THIS ROUND".

**Why `--every` travels in the environment.** The take-over child has no command
line (`cmd.Args = {argv0}`), so the tick period reaches it as
`GOPI_MERGER_TAKEOVER_EVERY` — the launcher's actual interval, so "N intervals of
5m0s elapsed" is the period *this host* runs, not a compiled-in guess. An
absent/unparseable value falls back to `MergerIntervalDefault` **and says so**.

**The staleness edge is a question, never a verdict.** Past 15 minutes without an
update the render says "⚠ no record update for 15m0s — it may have stopped
working; judge it the way the take-over's own §3.5 requires and DO NOT kill it on
this line alone". The constant is anchored on the prompt's own numbers (a single
branch is capped at ~20m), not picked for feel; the peer-liveness judgement stays
where it belongs.

## The prompt (the LLM-adoption surface, Rule 11)

Three things the round is now told, all gated by
`TestMergerTakeoverPromptCarriesItsSections` (so a future edit that drops one
fails the build rather than silently returning the surface to a bare pid):

- **§3.3.1 ff-batch-first** — land *every* fast-forwardable branch in one cheap
  pass **before** resolving anything, delete those branches, then the conflict
  branches **easiest-first**. A fast-forward cannot conflict and costs almost
  nothing, so the ordering is what maximises what a round lands.
- **§3.4 elapsed-time check** — read your own elapsed time (`date +%s`, or
  `takeover-progress show`) *before* starting each branch; past the round budget,
  conclude in hand and exit with an honest report rather than starting another.
- **§3.10.1 progress-record** — one `done` call per concluded branch, with the
  outcome→word table, plus why it matters to the round (`it is your liveness
  evidence`). A failed call is explicitly *not* a failed round.

## Not Done (honest)

- **No JSON surface.** `merger --status` has no `--json` mode to extend
  (measured: zero JSON tags/encoders in `merger_status.go`), so the counters are
  rendered text only. Adding a machine-readable surface is a separate task.
- **The prompt discipline is not forceable.** A round can ignore §3.10.1 and
  write nothing; the surface then honestly says `progress: none`, and says which
  binary would have written one. Nothing here can make an LLM call a tool.
- **The `--every` round trip is untested end-to-end against a real launcher**
  (`mergerTakeoverLaunchedEvery` is exercised only through its callers' compile
  path and the parse fallback). The environment variable's *effect* is proven
  (a seeded record with an interval renders "2 interval(s) of 5m0s elapsed").
- **Not run: the full `go test ./...`.** Reason: the unmodified
  `./internal/tools/` suite exceeded the 10-minute bash budget on the shared
  host (Rule 27 territory — no load was generated; the run was abandoned and a
  targeted run used instead).
- **`./cmd/agent/` carries four pre-existing failures on this branch AND on its
  base master — measured, and NOT fixed here** (they are outside this task's
  files; each was reproduced on a clean worktree at `bd621dc0` built with
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`):
  `TestServiceHelpIsCapabilityFirst` (mergerUsageHead carries "setsid" — the
  Rule 25 family), `TestSpawnersPinTheRepoContext` (two `merger_restart_e2e_test.go`
  spawn sites lack the repo-context sandbox),
  `TestMergerRestartDoesNotTouchTheTakeoverInFlight` (the stop never reaches a
  tick boundary within 60s), and `TestTaskRunSeamWritesReportAndShowRendersIt`
  (the dirty-closeout gate renders a *inherited* line — its own fixture worktree
  points at this live checkout, so the assertion fails while the tree is dirty).
  The last one is the Rule 30 gate working correctly against a dirty tree; it
  passes once the work is committed. `internal/standards/` was RED on the base
  too and IS fixed here — see the second commit below.

**A second commit (`0153132f`) turned the Rule 16 gate green, on the base as
well as here.** `go test ./internal/standards/` was red at `bd621dc0` itself
(measured on a clean export of the base: same two NEW violations), so the whole
`./cmd/agent/` red is not this branch's. Rule 16 found three sites; all three
are fixed now, and the `cmd/agent/main.go` permit is DELETED from
`baseline.go` rather than re-keyed a sixth time. That last site is the one this
gate could never ratchet on: its two `%v` verbs formatted the
`payloadFilePrefix` STRING constant while the same call already wrapped a real
error with `%w` — correct output, wrong verb, and a permit that every edit
above it re-keyed (five recorded re-keys, each leaving the gate red: NEW at the
new line, STALE at the old one). Changing the two verbs to `%s` removes the
drift at its source and moves the entry count 10 → 9, which a re-key never does.

## Verification

- Build: `go build ./...` — OK; `go build -o bin/gopi ./cmd/agent/` — OK
- `go vet ./cmd/agent/ ./internal/tools/` — clean
- Targeted tests (all green):
  - `go test -count=1 ./internal/tools/ -run
    'TestTakeoverProgress|TestProgressTriple|TestReadTakeoverProgress|TestMergerTakeoverLine|TestMergerStatus'`
  - `go test -count=1 ./cmd/agent/ -run 'TestTakeoverProgress|TestMergerTakeoverBriefing'`
  - `go test ./cmd/agent/ -run TestMergerTakeoverPrompt` (the prompt gate)
- **`go test ./internal/standards/ -count=1` — GREEN** (was RED on the base; see
  the Rule 16 note above): `Rule 16 (%w error wrapping): 7 total hits, 6
  baselined, 1 escaped, 0 new, 0 stale`, and `TestStandardGateDataReport` green.
- `gofmt -l` clean on every touched file. `internal/tools/task_finish_premerge.go`,
  `internal/tools/task_staleness_guard_test.go` and
  `internal/tools/taskledger_cost_test.go` are UNFORMATTED **on the base too**
  (measured: `git diff bd621dc0 -- <those three>` is empty, i.e. byte-identical
  to master) — not touched here.

## Evidence (self-filed)

Operational proof, run against the built `bin/gopi` in a throwaway
`GOPI_HOME` with a **real** process exec'd as `gopi-merger-takeover` (its stdio
on the profile's take-over log, which is how the probe attributes it). The
harness was transient and was deleted with its fixtures before the commit.

| claim | what I ran / what I saw |
|---|---|
| identity is three factors, on a real pid | `gopi merger --status` → `takeover: pid 334299+starttime 2026-09-18T06:20:19+08:00+cmdline "gopi-merger-takeover 300" running for 1s` |
| no record ⇒ honest absence, not zeroes | same run, before seeding → `progress: none — no progress record yet — the take-over has not written one (a round started by a binary from before this record existed writes none at all)` |
| the record renders counters + intervals + liveness | after seeding `ff_merged=3, remaining=4` → `progress: stage=working processed=3 (ff-merged=3 resolved=0 rework-filed=0 skipped=0 undecidable=0) remaining=4; 2 interval(s) of 5m0s elapsed so far; last update 1s ago` |
| pid-reuse is refused (same pid, starttime+1) | `sed` the record's `start_ticks` one above `/proc/<pid>/stat` f22 → `progress: NOT THIS ROUND — it describes pid 339938 (started …06:08:59…), which is not this take-over` |
| an amendment preserves the ROUND's identity | round pid 340394; six verbs run as child gopis → `show` still prints `pid=340394`, `ff_merged=2 resolved_merged=1 processed=3` |
| no round ⇒ every verb refuses, nothing created | fresh `GOPI_HOME` → `takeover-progress: NOT RECORDED — no take-over round is registered …`, `exit=1`, and `ls …/.gopi/merger` → `No such file or directory` |
| a finished round's record cannot be amended | killed the round → `takeover-progress: NOT RECORDED — the recorded take-over round is no longer running (pid 340394 is gone)`, `exit=1` |
| closed vocabularies | `done --outcome fast-forwarded` → `unknown outcome "fast-forwarded" (want one of ff-merged, resolved-merged, rework-filed, skipped, undecidable)`, exit 1; `stage --stage triage` → `"triage" is not a stage (want one of starting, ff-batch, working, budget)`, exit 1; `remaining --remaining -1` → refused, exit 1 |

Two bugs were found by these same minimal samples, not by inspection:

1. **Absent counters were not reported.** The first truncation test failed:
   `missing=[started updated]` — a record cut in half would have rendered "0
   merged". The reader now reports **every known key it did not find**
   (`progressKnownKeys`), so absence is the default and only present keys are
   exempt.
2. **Re-registering reset a live round's counters.** The idempotence test failed
   with `ff=0, want 1`: `mergerTakeoverProgressRegister` replaced a *live* record
   when it was this process's own — a round assembling its briefing twice would
   lose its accumulated evidence. A live record is now never overwritten by
   anyone.
3. (Same family) the store cached the resolved control directory across
   invocations, so a second invocation in one process could amend a record in the
   *first* directory. Now keyed on the resolved dir.

## Files

- `internal/tools/merger_takeover_live.go` — identity factors on the probe; the
  three-factor `identityPhrase`; the rendered `Line()`; `attachTakeoverProgress`.
- `internal/tools/merger_takeover_progress.go` — the format, the reader, the
  verdicts (`absent`/`unreadable`/`malformed`/`foreign`/`attached`), the identity
  match, the round-liveness guard, the interval/age arithmetic.
- `internal/tools/merger_takeover_progress_test.go` — 425 lines of reader tests.
- `internal/tools/merger_status.go` — one call attaching progress to this
  profile's take-overs.
- `cmd/agent/merger_takeover_progress.go` — the writer: register/amend/atomic
  write, the CLI face, the `--every` env ingestion.
- `cmd/agent/merger_takeover_progress_test.go` — the writer-side guards and the
  cross-process identity claim.
- `cmd/agent/merger_takeover.go` — `mergerTakeoverEveryEnv` export, the register
  hook in `mergerTakeoverBriefing`, the invalid-interval naming.
- `cmd/agent/main.go` — dispatch of the verb inside the merger family.
- `cmd/agent/merger_takeover_prompt.md` — §3.3.1, §3.4, §3.10.1, §1.
- `cmd/agent/merger_takeover_prompt_gate_test.go` — the gate rows for the above.
