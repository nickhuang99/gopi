<!-- doc-class: historical -->
# The unsourced-kill guard — a kill that nobody announced is now a recorded fact

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: `signal-provenance-unsourced` (2026-09-17). Branch
`feat/unsourced-kill-provenance`, worktree `~/workspace/gopi-unsourced-kill`.

This document has two halves, because the epic had two deliverables:

- **§1 — the forensics**: the 8-PID SIGTERM cohort of 2026-09-16T22:23:13–14, what
  the evidence positively establishes, and what no evidence establishes.
- **§2 — the guard**: the consumer-side classification that makes an unannounced
  kill a readable fact (session record + session log + ledger event).

Every claim in §1 carries the command that produced it. Where a link is
circumstantial the text says so.

---

## 1. The cohort: what happened at 22:23:13–14

### 1.1 The eight victims, quoted

`~/.gopi/sessions/*/signal-provenance.jsonl`, the incident window:

| # | pid | role | cgroup | parent |
|---|-----|------|--------|--------|
| 1 | 2444398 | run child (`/home/user/work/GoPi/bin/gopi`) | `session-18257.scope` | 952831 (`gopi worker`) |
| 2 | 952831 | `gopi worker` | `session-18257.scope` | 2034244 (`-bash`) |
| 3 | 2858364 | run child | `session-18205.scope` | 3680671 (`gopi worker`) |
| 4 | 3680671 | `gopi worker` | `session-18205.scope` | 1809064 (`-bash`) |
| 5 | 2833700 | run child | `session-18200.scope` | 3680484 (`gopi worker`) |
| 6 | 3680484 | `gopi worker` | `session-18200.scope` | 1785518 (`-bash`) |
| 7 | 2949215 | `web --addr=0.0.0.0:8099` | `session-18200.scope` | 1 (`systemd`) |
| 8 | 638001 | `merger` | `gopi-daemon-merger-nick-95fc9d79cb8f.scope` | 1 (`systemd`) |

Two structural facts the table already forces:

- **The victims are three runner+worker PAIRS.** Each worker (ppid = a `-bash`)
  was SIGTERMed at 22:23:14; each worker's child (the runner, ppid = the worker)
  at 22:23:13. The child died FIRST.
- **The victims span four unrelated cgroups, three unrelated login sessions
  (18200 / 18205 / 18257) and two unrelated parents (`bash` and `systemd`).**

Nothing about a cgroup, a process group, a login session or a parent explains
"these eight, together". That is the whole difficulty of this incident.

### 1.2 The sender announced nothing — measured, not assumed

```
$ python3 <count intents vs signals per window>
total intents: 1032   total signals: 591
window 2026-09-16T22:23   signal records: 8   intent records: 0
```

**1,032 `kind=intent` records exist in the tree, and not one of them falls in
this window** (nor in any of the three other mass-kill windows, §1.6). An intent
record is the only in-tree artifact that says "gopi is about to kill this pid and
here is why" — `signalrec.Intent`, emitted by `child_stop.go`,
`cli_worker_control.go` (`worker --stop`) and `cli_merger_stop.go`. Every gopi
stop surface writes one, and it is written BEFORE the signal.

So the kill was delivered by something that is not a gopi stop path. That is not
an inference from silence in a log that might not be written: the intent protocol
is the one path that always fires, and it did not fire eight times.

### 1.3 The mechanism is systemd's, and journald says so

```
Sep 16 22:23:15 peerbox systemd[316390]: gopi-daemon-merger-nick-95fc9d79cb8f.scope: Consumed 4h 41min 59.650s CPU time.
```

journald records the merger's scope terminating at 22:23:15 — one second after
the mergers' own record — and the `Consumed … CPU time` line is the
scope-teardown accounting systemd writes for exactly this event. The merger died
because **its cgroup was reaped**, not because a signal was addressed to it.

The worker pairs fit the same shape one level down: `session-18200.scope` holds
BOTH the worker 3680484 and the orphaned `web` 2949215 (ppid 1), so that scope's
teardown alone accounts for four of the eight. Teardown is SIGTERM to every
member, which is the signal all eight recorded.

Two of the four victims' records even name their own killer in plain JSON (§1.1):
`parent_name: "systemd"`, `cgroup: …/gopi-daemon-merger-…scope`.

### 1.4 Ruled out, each with its output

| Hypothesis | Verdict | Evidence |
|---|---|---|
| Reboot / uptime | **ruled out** | `uptime -s` = 2026-07-15 21:33:20 — same boot before and after ("deserialize 107" in the parent argv agrees, and the systemd pid is stable at 316390/1). |
| OOM / systemd-oomd | **ruled out** | `journalctl --since 2026-09-16 22:00 --until 23:00 \| grep -ci 'out of memory\|oom-kill'` → **0**. |
| A gopi stop path | **ruled out** | §1.2: 0 intent records in the window, against 1,032 elsewhere. |
| A `gopi worker --stop`/`--restart` fleet command | **ruled out** | same measure; also `--stop` names ONE pid per invocation, and the victims are in four cgroups. |
| In-tree code signalling a foreign pid | **ruled out** | the only `syscall.Kill` sites reach children this process owns (`signalChildOnly`, `killWithProvenance`); no code path enumerates foreign processes and signals them. |
| `pkill`/`killall` in the operator's shell history | **not found** | `~/.bash_history`, `~/.hermes_history`: no `pkill`/`killall`/`kill -` line at all. |
| A per-minute cron/watchdog sweep | **ruled out** | the `scratch-gc-watchdog` log is silent in the window; its stated contract is "touches NO running process, and no worker (no `pkill`, no `pgrep -f`)". |

### 1.5 What is NOT established — and will not be asserted

**The act is not attributable to a named sender.** The mechanism (cgroup scope
teardown) is proven; who or what caused the teardown is not.

- journald logged **nothing** during 22:22:40–22:23:30 except the ScreenSaver's
  DPMS complaint, the scope accounting line, and (at 22:23:29, sixteen seconds
  after) a `sudo … COMMAND=/usr/bin/true` probe — test residue, too late to be
  the cause.
- The scope teardown was **not journaled** as a stop request: there is no
  `Stopping …`/`Stopped …` line for any of the four scopes, and no logind
  "Removed session" near the window. That absence is itself informative — a
  `systemctl stop` by a human or a `loginctl terminate-session` would normally
  leave a line — but it does not name the actor.

A **same-tree, near-time observation worth stating** without overclaiming: at
22:23:29 a gopi session was running `go test` inside
`/home/user/workspace/gopi-both-axes` (same PWD as its 21:53–22:04 sudo probes),
i.e. **an agent-run test suite was executing on the host at that minute**. The
suite entry point arms a daemon-lifetime binding and sweeps orphan daemons
(`internal/testutil.ArmAndSweepForSuite`). I read that sweep (`orphansweep.go`):
it operates **strictly inside its own suite's scope** — it does not and cannot
signal a resident worker in `session-18200.scope`, and it writes nothing to
journald, so it is not a positive match either. Recorded as context; **not** named
as the sender.

**Verdict: the cohort is provably NOT a gopi stop; it IS a cgroup-scope teardown
of four scopes inside ~1 s; the actor is undetermined, and this report refuses to
name one.**

### 1.6 The same signature has now happened FOUR times

| Window | Victims | Leader | Zero-intent? |
|---|---|---|---|
| 2026-09-12 04:12:19–04:13:30 | 6+ workers (see `devdoc/worker-cohort-kill-20260912.md`) | — (records were in the retired global log; not re-derivable per-session) | yes |
| 2026-09-15 20:03:34 | **14** (4×worker, 3×web, 1×merger, 1×task run, 5 hand-off processes) | 1 s | yes |
| 2026-09-16 22:23:13–14 | **8** | 2 s | yes |
| 2026-09-17 05:43:25 | **4** workers | **same second** | yes |

This is why the epic is worth finishing: it is not one incident, it is a
recurring class.

#### The 2026-09-17 05:43:25 recurrence rules out the "one login died" reading

```
2026-09-17T05:43:25 worker 3067418  session-18252.scope
2026-09-17T05:43:25 worker 3066922  session-18855.scope
2026-09-17T05:43:25 worker 3066414  session-18200.scope
2026-09-17T05:43:25 worker 3074388  session-18257.scope
```

**Four workers, four DIFFERENT login scopes, all in the SAME second.** No single
scope teardown reaches four scopes at once. Whatever sweeps them selects by
**process identity** (`gopi` / `gopi worker`), not by cgroup membership — a
name-based sweep makes the 09-16 victims' "four scopes at once" ordinary rather
than mysterious, and it is consistent with `session-18200` still existing at
09-17 05:43 (a scope that had been torn down on 09-16 would not).

This does **not** name the sender. It does pin the *class* the sender belongs to:
an identity-selecting sweep (`pkill -f gopi`, a supervisor, a host tool), not a
per-scope stop.

---

## 2. The guard

### 2.1 The defect, in one sentence

Two deaths that are **not the same fact** produced the **same trail**: an
operator's `gopi worker --stop` and an unannounced `kill <pid>` both wrote a
`kind=signal` record with no statement about the sender, and both printed
`draining` on the console — so the eight victims of §1.1 were, in the log,
indistinguishable from a graceful drain.

### 2.2 The protocol, and the hole that was in it

`internal/signalrec` has written TWO record kinds for years:

- `kind=intent` — "I am about to kill pid X, because Y" (`signalrec.Intent`),
  written BEFORE the signal, so a SIGKILL leaves a trace;
- `kind=signal` — the victim's own record of the signal it caught.

The **intent side was written and never read.** Nothing consulted it, so the
consumer half of the protocol did not exist — `signalrec` could say *who killed*
only to a human reading the sender's file, never to the victim.

### 2.3 The fix: the intent gets mirrored to the victim, and the victim reads it

1. **`Intent()` mirrors its record into the TARGET's own session dir**
   (`writeIntentToTarget` → `targetSessionDir`), not only its own. The victim
   then answers "was this announced?" with **one file read**, from inside its
   signal handler — no host-wide scan (the tree holds 73,845 session dirs and
   1,032 intents; enumeration was never an option).
2. **`targetSessionDir(pid)`** resolves the victim's dir from OUTSIDE, from
   procfs, by three facts in order: `GOPI_SIGNALREC_SESSION_DIR` in the target's
   environment → the target's CWD when it *is* a session dir → the
   **pid-keyed** path for a resident mode (a `worker`'s and a `merger`'s dir is a
   *pure function of its pid*, which is what makes an external resolve sound).
3. **`classifySourced` reads it back** and stamps `sourced: true|false` on the
   victim's record, plus `intent_evidence` quoting the intent the verdict rests
   on.

### 2.4 Three verdicts, never two

`sourced` is a **pointer**, so `nil` is a distinct third state:

| `sourced` | meaning | why it is not folded into a neighbour |
|---|---|---|
| `true` | a gopi stop path announced this kill | — |
| `false` | **UNSOURCED** — a readable trail holds no intent naming this pid | the 2026-09-12/16/17 shape |
| absent (`nil`) | **not classified** — no session dir to read, or a read that failed | the word accuses an operator of a kill nobody announced; it must rest on a read that *happened*. A guess here is worse than silence. |

The "unreadable" arm is an in-process test on purpose: the end-to-end shape is
self-defeating (the fault that makes the trail unreadable also stops the victim
writing its death record), so the *branch* is what gets pinned.

### 2.5 Where the verdict lands — three sinks, one classification

| sink | file / surface | what a reader gets |
|---|---|---|
| session record | `<session dir>/signal-provenance.jsonl`, field `sourced` (`signalrec.SourcedField`) | machine-readable, one JSON line |
| session log | the victim's log, `… signal provenance — SIGTERM (UNSOURCED …)` | the operator's grep, no JSON parsing |
| ledger | `task_events.event = signal-sourced` / `signal-unsourced`, detail carrying the evidence | the durable "was this run stopped or killed?" for that task row |

The ledger event is written by `internal/tools.RecordSignalVerdict` against the
row the run was executing (`signalrec.CurrentTaskID`). It is **best-effort by
design** — a shutdown path may not block or fail the exit — but never silent:
every failure is reported to stderr (`ReportSignalVerdictFailure`, Rule 15b).

### 2.6 What the operator sees now

Both arms, from ONE `worker.log` of the real binary (evidence run below):

```
worker: signal provenance — SIGTERM (UNSOURCED — no gopi stop path announced this kill
        (an external signal: kill(1), a supervisor's timeout, a service manager or a session teardown))
worker: signal provenance — SIGTERM (announced by a gopi stop path)
```

Before this change both lines read `draining`.

---

## 3. Verification (proof, not assertion)

### 3.1 Unit — both arms, one selector

`internal/signalrec/signalrec_unsourced_test.go`:

- **Arm A** (`TestSignalRecUnsourcedKillIsClassified`) — a victim with no intent
  filed against it must record `sourced=false` and spell `"sourced":false` in the
  file.
- **Arm B** (`TestSignalRecAnnouncedKillIsClassifiedSourced`) — the killer files
  its intent into the victim's dir BEFORE signalling; the same victim must record
  `sourced=true` **carrying the reason as evidence**. This is the
  anti-over-firing proof: a guard that labels every kill unsourced is worse than
  none.
- Both arms run the same victim binary and differ **only** in the selector, so
  neither can pass by accident of a different code path.
- Plus the third state (unreadable trail → no verdict), the no-session-dir state,
  and the bounded-wait contract.

### 3.2 Real binary, both arms (Rule 11)

```
$ stat -c %d /                → 2051
$ stat -c %d $EH              → 2052      # evidence off the / partition (Rule 22)
$ GOPI_HOME=$EH/home gopi worker --daemon
worker daemon started: pid 1626174 (cgroup escape: systemd-scope)
$ kill -TERM 1626174                        # ARM A: nobody announced it
```

Arm A — session record:

```json
{"kind":"signal","signal":"SIGTERM","pid":1626174,"role":"worker", … ,
 "cgroup":"0::/user.slice/…/gopi-daemon-worker-home-150201e08c10.scope",
 "layout":"process","sourced":false}
```

Arm A — console:

```
worker: signal provenance — SIGTERM (UNSOURCED — no gopi stop path announced this kill …)
worker: the ledger will carry unsourced for task "01a0ac30-…"
```

Arm B — `gopi worker --stop` (an announced kill):

```
gopi worker: asking pid 1638495 to finish the in-flight round and exit (SIGTERM, bound 10m0s)
gopi worker: pid 1638495 exited 2.002s after the graceful stop request
worker: signal provenance — SIGTERM (announced by a gopi stop path)
```

Both arms' records, side by side:

```
pid=1626174  role=worker  SIGTERM  sourced=False  intent=-
pid=1638495  role=worker  SIGTERM  sourced=True   intent=2026-09-17T07:11:18+08:00 worker → gopi worker: operator ran
```

The mirror actually landing (arm B), both sinks for one kill:

```
dir=process-1638495 target=1638495 reason=… operator ran gopi worker --stop [by pid …]   layout=mirror
dir=process-1638742 target=1638495 reason=… operator ran gopi worker --stop              layout=process
```

The ledger:

```
(1, '01a0ac30-…', 'signal-unsourced', 'SIGTERM sourced=false no known gopi stop path announced this kill')
(2, '01a0ac30-…', 'signal-sourced',   'SIGTERM sourced=true intent=2026-09-17T07:11:18+08:00 worker → gopi worker: operator ran gopi worker --stop [by pid 1638742]')
```

### 3.3 Build / tests

```
CGO_ENABLED=0 go build ./...            OK
go test ./internal/signalrec/           ok
```

---

## 4. Design decisions worth keeping

1. **The env var is named for what it does.** `GOPI_SIGNALREC_SESSION_DIR` is a
   READER of the handoff pair a launcher exported; the writer-side, from-scratch
   policy lives inside the recorder (`processDest`). Rule 21 (every env-name
   constant has a real reader) is only satisfied by this orientation, so the
   variable does not pretend to be a parent-setter knob.
2. **A resident victim is found by its pid-keyed path, even when the env names
   it.** The alternative was to demand a signed handoff marker, which would have
   forced the stop path to know a per-victim secret. The path is a pure function
   of the pid, so both sides compute the same string from the same two facts
   (`residentDirFor`), and accepting it is not a trust decision.
3. **The mirror may create a resident victim's dir but never a guessed one.** A
   resident mode owns `process-<pid>` by definition, so creating it is sound; a
   *non-resident* victim with no resolvable dir gets **no** mirror, and its
   absence must not be read as UNSOURCED on the sender's side (the sender's own
   record carries `parent_name`/`cgroup` for those — see §1.1, where the victim
   records are verbatim).
4. **MODE, not CLI verb, keys the signal policy** (pre-existing, honoured here):
   `gopi task list` is a one-shot that must die on SIGTERM; `gopi task run <id>`
   owns its handlers. Keying off the verb would make the one-shot unkillable.

## 5. Not done (honest)

- **The sender of the four mass kills is still unnamed**, and this work does not
  name it. §1.5 states exactly what is and is not established; the 05:43
  recurrence narrows the *class* (identity-selecting sweep) without naming an
  actor. Catching it needs a host-level instrument (an eBPF/audit watch on
  `kill`, or a logind/scope-teardown audit rule) — that is **outside this
  repo's process tree** and is the operator's call, not something gopi may
  install for itself (Rules 22, 25).
- The 2026-09-12 window could not be re-measured per-session: that incident's
  records predate the per-session layout and live in the retired global log.
- The ledger event is written for a run that owns a row. A resident daemon
  between rounds owns no row, so its verdict is in the session record and the
  log; the console line says so explicitly rather than implying a ledger entry.
- **The mirror is absent for a victim whose dir cannot be resolved from outside**
  (a runner child with no session dir handed to it, an agent-session child).
  Those victims are externally announced by the sender's own record
  (`parent_pid` / `parent_name` / `cgroup` — see §1.1, where the runner records
  are exactly of this shape) rather than by an intent mirror, so nothing is lost
  today. Closing it properly would mean the launcher **exporting the child's
  session dir**, which is a system-layer change (Rule 28) and is left as a
  follow-up rather than smuggled in here.
