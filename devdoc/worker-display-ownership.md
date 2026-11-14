<!-- doc-class: normative -->
# Worker display ownership — who owns DISPLAY, and what a spawn door does about it

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status: canonical decision record.** Epic `worker-spawn-doors` (task
`01a099e3-fa5c-7276-b08f-82a5c31926d3`), 2026-09-14. Every measurement below was
taken on THIS host (peer1), on the real binary, in CONTAINED sandboxes (a private
`HOME` + `GOPI_HOME` + output root) — no live worker, daemon or merger was
started, stopped or restarted to obtain them (WORKER-OWNERSHIP: OPERATOR-ONLY).

The companion documents are `devdoc/worker-ownership.md` (who may START a
worker) and `devdoc/worker-daemon.md` §F2 (the finding this record answers).

---

## 1. The doctrine — the launcher OWNS the whole session environment

**The launcher is the DISPLAY owner.** A worker is a DETACHED process: `setsid`
+ `nohup` put it in its own session and take away its controlling terminal, but
they change **nothing** about its ENVIRONMENT. Every variable the launcher's
shell carries — `DISPLAY`, `XAUTHORITY`, `XDG_*`, `cwd` — is inherited verbatim
by the worker, and by every `gopi worker --daemon` child the worker spawns.

The measurable consequence, and the sentence this document exists for:

> **A detached process can NEVER re-acquire a display it did not inherit a
> working source for.** `DISPLAY=localhost:N` names an SSH-forwarded socket that
> lives in the LAUNCHER's session. When that forward dies, the socket is gone;
> the worker's `DISPLAY` is then a dangling name, and `XAUTHORITY` does not help
> — it resolves to a FILE (`~/.Xauthority`), so setting `DISPLAY` correctly IS
> sufficient, while a dead `DISPLAY` cannot be repaired from inside the worker by
> any amount of environment editing at the tool-call layer (the auth file is not
> what is missing; the socket is).

### The measurement (this host, 2026-09-14)

| DISPLAY the launcher carried | `screen_capture` from the launched process |
|---|---|
| `:99` (Xvfb, pid 1377285 — the durable LOCAL display) | **exit 0**, 1280x800 PNG, `sha256_16 2b3f359828500f26` |
| `localhost:15.0` (an SSH forward of my agent shell) | **exit 1** — no such socket |
| unset | **exit 1** |

Probe cost 10–60 ms, so a canary at loop start is free. Independently, the live
agent shells on this host carry `DISPLAY=localhost:11.0 / :13.0 / :15.0` — every
one of them an SSH forward — which is how the defect reaches production: the
workers those shells would start inherit a display that dies with the ssh
session, and nothing says so.

### What to DO with a stale/forwarded DISPLAY

1. **Launch from a shell whose DISPLAY is the durable local one** — on this host
   `Xvfb :99` (`DISPLAY=:99`). That is the operator's action, taken in HIS
   shell, once, before starting the fleet.
2. **Read the worker's own verdict** — since this epic every worker prints one
   `display preflight` line at loop start over all three spawn doors, so "is the
   GUI half of my fleet armed?" is answered at launch instead of hours later by
   a failing `screen_capture`.
3. **Never try to repair it from inside** — a detached worker has no launcher
   left to ask and no session to re-forward; the only remedy is a restart from a
   correct shell (see §4, decision 2).

---

## 2. DECISION 1 — the launcher owns the session env, and the worker ANNOUNCES its display

**Chosen: ANNOUNCE (never refuse).** The worker loop prints exactly one line at
start, whatever the outcome:

```
[worker] display preflight: DISPLAY=":99" reachable (screen_capture/act will work from this process)
[worker] display preflight: DISPLAY="localhost:15.0" UNREACHABLE: connect to X display …
[worker] display preflight: this worker inherits DISPLAY from its launcher and can NEVER re-acquire one —
         every screen_capture/act it runs will fail (exit 1). Restart it from a shell whose DISPLAY is the
         durable LOCAL display (Xvfb :99 on this host), not an SSH forward (localhost:N)
```

**Why ANNOUNCE and not refuse.** DISPLAY is a **capability**, not a
precondition: a worker that only drains text tasks is fully useful with no
display at all, and nothing in the process can decide "do you intend to use the
GUI?" — a question the refusal would have to answer on the operator's behalf.
Refusing would therefore take the operator's whole fleet down for a capability
most tasks never touch, and would make the start path the arbiter of an
intention it cannot observe. Announcement is the honest form of the same
information: one line at launch, and the consequence + the remedy are named in
it (Rule 15: a degradation is announced, never passed off as working).

**Rejected alternatives, and why:**

* **REFUSE the start when the display is unreachable.** Rejected: it converts a
  missing GUI capability into a total outage for text-only work, and it invents
  a policy ("this worker was meant to do GUI work") the process cannot know.
* **NEITHER — say nothing, let the first `screen_capture` fail.** Rejected: that
  IS the defect. The failure surfaced hours later as a tool error attributed to
  the tool rather than to the launch; the operator had no line to read and no
  reason to suspect the launch environment.
* **A launcher-side preflight (a new env knob / a `--display` flag).** Rejected
  outright: `internal/testutil/procname_gate_test.go` **forbids**
  `scripts/gopi-worker-up.sh` from reading any env knob it does not declare, and
  asserts every declared default equals its Go constant (`internal/procname`)
  (Rule 1b/13/14/21). A new knob is a new contract, and this decision needs
  none: the worker reads `DISPLAY` through `internal/shot`'s ONE reader
  (`ProbeDisplay` → `captureDisplayName` → `$DISPLAY`), the same lookup
  `screen_capture` and `act` use, so the announcement can never disagree with
  the capability it describes. **No new env knob, no second supervisor.**
* **A `--daemon`-only preflight.** Rejected: the daemon is one of three doors
  (launcher script, foreground, `--daemon`) and all three converge on
  `runTaskWorkerLoop`, so one call covers all of them; a per-door check would be
  three seams with one behaviour.

**Where it lives (one seam, every door).** `cmd/agent/worker_display_preflight.go`
(`announceWorkerDisplay()`, label constant `workerDisplayLabel = "display preflight"`)
is called from `runTaskWorkerLoop` in `cmd/agent/cli_tasks_worker.go`, and
`internal/shot.ProbeDisplay` is the reachability predicate (socket dial + the
MIT-MAGIC-COOKIE-1 setup handshake, then close — deliberately weaker than
`OpenXActuator`, which additionally requires XTEST: a display that can be
captured but not actuated is still reachable, which is the question asked).

---

## 3. DECISION 2 — the launcher-script door: the GUARD covers it by ENVIRONMENT, not by name; the script stays deliberately un-gated

**Chosen: the guard is NOT extended to recognize the launcher script; the
"what an agent does instead" enforcement stays at the REMAINING start door
(`gopi worker`, which every door ends at) plus the announcement from §6.
The door stays deliberately open FOR THE OPERATOR, and closed for an agent by
environment inheritance — with one honest exception recorded below.**

### The measured facts (this host, real binary)

| Launch | Result |
|---|---|
| agent-env shell → `scripts/gopi-worker-up.sh 1` | the CHILD was refused (exit 3), but the launcher printed `started worker 1: pid …` and **exited 0** — the silent failure of §6 |
| `env -i` (fully clean env) shell → `scripts/gopi-worker-up.sh 1` | **a real worker started** (loop banner in the sandbox startup log; "1 idle round(s) … exiting 0") |
| agent-env shell → `gopi worker` (bare verb) | refused, exit 3 |
| agent-env shell wrapping the verb in a `sh` script (inherited env) | refused, exit 3 — `cmd/agent/agent_spawn_guard_test.go` ⑤ |

So **the claim "a script cannot launder this" is FALSE as written**, and TRUE
only for the narrower case it was actually measuring. The guard judges the
LAUNCHER's environment (§6), so:

* a launcher that merely WRAPS the verb inherits the agent's signature and is
  refused exactly like the bare verb — the original claim's real content;
* a launcher that first CLEARS the environment (`env -i …`,
  `env -u GOPI_EXECUTOR_SESSION_ID -u HERMES_SESSION_ID -u GOPI_AGENT_SESSION`)
  gets past the guard, because there is no signature left to judge.

**Why the correction is a correction and not a hole.** The environment is the
ONLY thing the guard can read, and an agent that deliberately deletes a
signature is not being caught by accident-detection — it is a deliberate rule
violation (the guard's package comment already says so: `env -u … gopi worker`
is the documented honest limit). What the epic adds is that this limit is
reachable through the launcher script TOO, and that it must be stated instead of
denied. It remains impractical as a way to FOOL the system because the door
alone is not enough — the launched process must still become a worker this
machine accepts:

* the worker GHOST gate refuses a second worker for a session that already has a
  live one;
* the task-lifecycle **flock** is bound to a path/inode, so a second holder is
  refused (the `refusing to start: the flock is bound to an unlinked inode`
  refusal measured on this host);
* the binary-path/hash checks refuse an unexpected `bin/gopi`.

**Rejected alternatives, and why:**

* **Gate the launcher script itself (a marker the script must carry, a check
  like `gopi-worker-up.sh --require-operator`).** Rejected: the script is the
  OPERATOR's own tool, and the operator's shell is exactly the environment the
  gate would have to distinguish from an agent's — which it cannot do reliably
  (the operator legitimately opens shells inside hermes; that case is why
  `GOPI_HUMAN_TERMINAL` exists as a per-invocation declaration). A gate that
  can't tell them apart either blocks the operator or is decoration.
* **Gate by process ancestry inside the script (walk `/proc/<ppid>` for a gopi
  agent).** Rejected: the script would have to re-implement the guard's procfs
  logic (Rule 14 — a second definition of the same predicate), and the same
  ancestry read is already available at the real door.
* **Refuse to launch when the environment carries a signature (a script-side
  mirror of the guard).** Rejected: it duplicates the guard in a second language,
  drifts from it, and the child already refuses (exit 3) — the missing half was
  never detection, it was REPORTING (§6).
* **Record the door as a security hole and require the operator to stop using
  it.** Rejected: it is the operator's documented, single point of control for
  starting his fleet; closing it would break the fleet to close a path whose
  only exploit is a deliberate rule violation that the queue + GHOST + flock
  already contain.

**"What NOT to do", recorded as firmly as the "what to do":**

* **Do NOT add a new env knob** for this door — not to the launcher (`GOPI_*`
  read by `gopi-worker-up.sh` is forbidden unless declared in
  `internal/procname` and asserted by `procname_gate_test.go`), and not to the
  worker.
* **Do NOT add a second supervisor** — no second spawner, no second launcher, no
  inline `setsid nohup gopi worker` in any script
  (`hp_worker_launchers_test.go` / `glm_night_watch_test.go` fail on exactly
  that shape), and no `pgrep`/`pkill` anywhere near it.
* **Do NOT make the launcher a policy arbiter** — it starts N workers or it
  fails loudly (§6); it does not decide who is allowed to ask.
* **Do NOT silently degrade** — a refused child or a missing display is printed
  and is a non-zero exit at the launcher, and one `display preflight` line at
  the worker (Rule 15).

---

## 4. What an AGENT does instead (unchanged, restated because it is the point)

```bash
gopi task submit --summary "<what>" --payload "<self-contained briefing>"
```

The queue is the handoff; the operator's worker drains it. An agent's verb is
the submission — never a launch, never a launcher script, never an
`env -u`-cleared anything.

## 5. Who owns what (the display/fleet half)

| Thing | Owner | An agent's relationship |
|---|---|---|
| `DISPLAY` / `XAUTHORITY` for a worker | **the launcher** (inherited verbatim, never re-acquired) | never sets it for a worker; reads the `display preflight` line |
| the durable local display (`Xvfb :99` on this host) | **the operator** | may probe it; may launch GUI work into it only via his worker |
| refusing/announcing an unreachable display | **decision 2 above**: announce, per worker, every spawn door | may read it in the log |
| the launcher-script door | **the operator's** (un-gated by design, §3) | never runs it |

## 6. The companion fix — the launcher's launch verdict (GAP 2's silent failure)

Measured, this host: an agent-env launcher run printed `started worker 1: pid …`
and **exited 0** while the child had been refused (exit 3). The launcher
reported a launch that never happened — the same defect class
`glm-night-watch.sh` was fixed for ("the pre-fix watcher logged `started worker N
pid …` for a launch that never happened").

**Fix AS BUILT (no new knob, reusing the launcher's own counter):** the launch
loop's per-worker line was changed from `started worker $i: pid …` to
`requested worker $i: pid …` — the launcher may only describe a REQUEST there,
because it has not counted anything yet — and after the cadence
`verify_launched_workers` prints the verdict FROM THE COUNT ALREADY MADE by
`report_counts`:

* `launch verdict: N start request(s) issued, M live gopi worker(s)` and exit 0
  when `M > 0`;
* `launch verdict: UNCONFIRMED …` on stderr, naming the two causes (a refused
  child; a frozen host), telling the reader **not** to start a second
  supervisor, and exit **1** when `M == 0`.

**Why the count, and NOT `$!`/`kill -0`** (a correction of this document's
first draft, made from a measurement rather than a preference). Measured on this
host:

```
$ setsid nohup sleep 30 </dev/null & echo $!
1591838
$ ps -o pid,ppid,pgid,sid,comm -p 1591838      # <- $!
1591838 1591837 1591837 2829404 bash           # the setsid WRAPPER, not the child
$ pgrep -P 1591838 → 1591840 / sleep           # the real child, a NEW session (sid 1591840)
```

`$!` is the PID of the `setsid` process the shell forked — `setsid` **forks** its
own child into the new session and waits for it, so the pid the launcher prints
is the wrapper's, not the worker's (`ps -o comm` shows `bash`, and the worker is
one level below). A `kill -0`/`pgrep -x` on the printed pid therefore answers a
question about the wrong process entirely (`pgrep -f` would self-match — the
repo's recorded trap). `count_gopi_processes` answers the question that matters
(“is there a process NAMED like the worker with the worker verb as argv[1]?”)
with the argv SHAPE predicate, and is already run one line later; the verdict is
derived from that same read, so no new tool, no new knob, no second predicate
(Rule 14).

**Measured, this host, contained sandbox (Home/GOPI_HOME private; the only
sibling worker observed was one started by THIS launcher run and then stopped by
env-identified pid, never by name — see §7):**

| Launch (same script, same private HOME) | children | verdict | exit |
|---|---|---|---|
| agent env (`GOPI_AGENT_SESSION` inherited) | child refused, exit 3 | `UNCONFIRMED … NO live gopi worker is running` | **1** |
| operator-style env (anchor cleared; `GOPI_HUMAN_TERMINAL` unset — the guard accepts a clean env by design, §3) | worker started and served rounds | `1 start request(s) issued, 1 live gopi worker(s)` | **0** |

The second row is also the proof that the verdict is not a constant complaint,
and the correction of a first-draft claim: the ACCEPTED direction is a real
worker, so a "count unchanged ⇒ refuse" rule would have been wrong in exactly
the case it was written for.

## 7. Verification recipe (reusable)

```bash
# (1) the display canary, all three doors, no worker fleet involved
DISPLAY=:99        bin/gopi worker --once   # → reachable line
DISPLAY=localhost:15.0 bin/gopi worker --once  # → UNREACHABLE line + remedy, exit 0
DISPLAY=           bin/gopi worker --once   # → "DISPLAY=\"\" UNREACHABLE"

# (2) the guard, both directions, on the REAL binary
GOPI_EXECUTOR_SESSION_ID=x HERMES_SESSION_ID=y GOPI_AGENT_SESSION=1 \
  bin/gopi worker --once ; echo "exit=$?"        # 3
GOPI_HUMAN_TERMINAL=1 bin/gopi worker --once ; echo "exit=$?"   # 0 (loop banner)

# (3) the launcher, contained (private HOME, no real fleet)
HOME=$SANDBOX GOPI_HOME=$SANDBOX GOPI_WORKER_REPO=<checkout> \
  bash scripts/gopi-worker-up.sh --dry-run ; echo "exit=$?"     # 0
GOPI_WORKER_N=1 … bash scripts/gopi-worker-up.sh 1
#   → "requested worker 1: pid …" + "launch verdict: 1 start request(s) issued, N live …" (exit 0)
#   a refused child → "launch verdict: UNCONFIRMED …" + exit 1     (asserted by TestWorkerUpLauncherVerdict*)

# (4) the launcher's own count must not self-match the agent's shell
#     (the 2026-09-12 incident class): the shell's cmdline mentions the verb, the count does not move
bash scripts/gopi-worker-up.sh --count   # → idempotent across invocations
```

## 8. Honest limits

* An agent that deliberately clears its environment gets past the guard (§3) —
  MEASURED both ways in §6's table: an env-cleared shell really does start a
  worker. Detection is the worker count + the log, not a refusal; this document
  states it rather than denying it.
* The launcher's verdict is about ITS OWN launch, deliberately: it reads the
  host-wide worker count, so on a host that already had workers an
  all-children-refused launch still reads `M > 0` and exits 0. Making the
  verdict per-launch is impossible with a name-shape count and is why the
  count is the honest evidence and the exit status is the secondary signal.
* `ProbeDisplay` reports reachability, not usability: a display that authenticates
  but cannot be actuated is "reachable". That is the deliberate scope (§2).
* The `display preflight` line is evidence at START; a display that dies LATER
  (an ssh forward dropped mid-run) is a capability that degrades after the
  announcement, and the failing tool call is then the only signal.
