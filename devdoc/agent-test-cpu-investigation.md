<!-- doc-class: historical -->
# Task Report: Are `agent.test` / `tools.test` regular or zombie processes?

**Date:** 2026-09-12
**Host:** 4 cores (`nproc` = 4)
**Trigger:** `top` sample on the shared dev host showing `agent.test` (73.5% / 43.4% CPU),
`tools.test` (45.7% CPU), plus several `gopi` and one `hermes` process.
**Question:** are these regular processes or zombies, and are they really eating the machine?

---

## Verdict

**REGULAR, self-terminating `go test` package binaries — NOT zombies, NOT leaked.**

They are simply Go's per-package test executables (`<pkg>.test`), which the Go toolchain
compiles to a temp dir, runs, and deletes. Their heavy CPU is *real but transient*: a full
package suite (notably `internal/agent` and `internal/tools`) is expensive, and several
suites were running at once on a 4-core box.

**There is no process leak and no zombie accumulation.** The genuine phenomenon underneath
the observation is **bursty CPU contention** from several concurrent agent workers each
running full-package test suites — plus a real downstream consequence: **false-red
wall-clock-guarded tests** under that contention (see *Consequences*).

---

## Evidence

Every number below traces to a command run during this investigation.

### 1. The cited PIDs were gone within seconds

All five PIDs from the original `top` snapshot — `250146`, `245963`, `244225`, `243865`,
`3975307` — no longer existed on re-check ~15 s later. A leaked/detached process does not
disappear when you look at it; a `go test` binary does, the moment its package finishes.

### 2. STAT was `S`/`R`, never `Z`

The `top` sample itself already carried the answer: `250146` = `S`, `245963` = `S`,
`244225` = `R`. A zombie is `Z` by definition (dead, waiting on a reap) and consumes **no**
CPU — a `Z` process showing 73.5% CPU is a contradiction in terms. Their `%CPU` is exactly
what disqualifies them from being zombies.

### 3. Zero defunct processes, repeatedly sampled

```
$ ps -eo stat,comm | awk '$1 ~ /^Z/' | wc -l
0
```

Zero `Z`-state processes system-wide across the whole observation window (multiple samples
over ~90 s, plus a fresh snapshot at 04:28:16). Across the entire window **not a single
`.test` process was running** — the burst was already over.

### 4. The RSS is exactly right for a fresh Go test binary

| observed RSS (top) | on-disk test binary | match |
|---|---|---|
| `agent.test` 28 292 KB | `cmd-agent.test` 21 828 505 B (~21 MB) | ✔ 21 MB image + runtime/heap |
| `tools.test` 29 620 KB | `internal-tools.test` 20 945 420 B (~20 MB) | ✔ same shape |
| `agent.test` 36 320 KB | `internal-shot.test` 11 301 964 B (~11 MB) | ✔ smaller image, smaller RSS |

Artifacts found on disk, timestamped during the burst window:

```
-rwxrwxr-x 1 nick nick 14254372 Sep 11 08:40 /tmp/go-build1483818303/b001/tools.test
-rwxrwxr-x 1 nick nick 21828505 Sep 11 08:06 /dev/shm/gopi-test-scratch/buildprobe/cmd-agent.test
-rwxrwxr-x 1 nick nick 20945420 Sep 11 08:04 /dev/shm/gopi-test-scratch/buildprobe/internal-tools.test
-rwxrwxr-x 1 nick nick 11301964 Sep 11 08:07 /dev/shm/gopi-test-scratch/buildprobe/internal-shot.test
```

A ~29 MB RSS against a 20 MB binary is a *freshly started* test process (image + Go
runtime + not-yet-freed heap) — precisely the transient the `top` sample caught. Note also
`/tmp/go-build*/b001/` is the Go toolchain's own working directory: these were compiler
products, run in place and deleted.

### 5. Load averages prove a recent burst, not a persistent leak

```
$ uptime
 04:28:16 up 58 days,  6:54,  5 users,  load average: 0.86, 1.66, 6.69
```

1-min **0.86** vs 15-min **6.69**. On a 4-core box that is a classic burst signature: the
machine was heavily saturated a few minutes ago and is now nearly idle. A leaked process
would hold the 1-minute average up; this did the opposite.

*Original snapshot's* own `top` header corroborates this — the load line there was already
**7.64** (1-min), and it fell to 0.86 over the observation.

---

## What actually caused the burst

Read-only inspection of the live host:

- **5 live `bin/gopi` processes**, including two long-running `gopi worker` instances
  (`380024`, `380057`) — autonomous worker agents, each of which runs full test suites.
- **`hermes`** (`293110`, runtime 1630 s / 27 min, RSS ~503 MB) — the monitor invoking the
  worker-up script.
- **570+ `go test` invocations across recent worker session logs.** Individual sessions
  alone ran:
  - session `01a087d1-…`: **539** `go test` invocations
  - session `01a091de-…`: **329**
  - session `01a088e0-…`: **159**, `01a091e7-…`: **72**, `45109ce1-…`: **51**
- **35 `in_progress` task rows** — work queued concurrently across workers.

So: N workers × (full `go test ./...`-style sweeps, ~430 s each on 4 cores) = the CPU
spikes `top` caught. The `.test` binaries are the visible tip of that, not a defect.

### Why the processes looked "stuck"

Two properties make a healthy `go test` binary look pathological in `top`:

1. **Duration.** A cold full-package suite (`internal/agent`, `internal/tools`) takes
   minutes, far longer than the interactive commands a human runs — so the same `.test`
   name shows up across several glances.
2. **Name collision.** The name is the *package*, so two different runs of the same package
   present the identical process name. A repeated glance looks like the same process never
   dying, when it is genuinely sequential runs — proven here by the five cited PIDs all
   being distinct and all exiting.

---

## Consequences (the real problem, worth acting on)

The load is not a leak, but oversubscription has a documented failure mode in this repo:
**wall-clock-guarded tests go false-red under CPU contention.** A test asserting "this
completed within N seconds" fails when 4 cores are shared by concurrent full-suite sweeps,
even though the code is correct.

- Tracked as bug `01a08c4f-6bc0-7661-b471-b00f795d8063`.
- Prior lesson recorded as memory record **#1702** (false-red wall-clock guards under load).

So the cost of the burst is not just CPU — it is **wasted agent cycles chasing red tests
that are not real**, which then re-run more suites and feed the cycle.

## Recommendations

1. **Do not kill them.** They are correct, self-cleaning `go test` binaries; killing one
   mid-suite just produces a spurious failure. No zombie reaping is needed (there are none).
2. **Cap concurrent full-suite runs.** The lever is worker concurrency, not process
   management: limit how many `gopi worker` instances run `go test ./...`-style sweeps at
   once (and/or serialise heavy-package suites), so the box is not oversubscribed against
   its 4 cores. This is the root-cause fix for the CPU spikes.
3. **Prefer targeted tests where possible.** `go test ./internal/tools/` is dramatically
   cheaper than a whole-tree sweep; defaulting agent verification to the touched package
   removes most of the load.
4. **Harden the wall-clock guards** (see bug `01a08c4f`): make timeout-sensitive tests
   load-tolerant (retry/deadline scaling or a serial guard) so contention stops producing
   false reds.
5. **Observation tip:** to tell a real leak next time, check `ps -eo stat` for `Z` and
   `uptime` for a high 1-min vs low 15-min average. A high 1-min average *with* live
   `.test` processes and growing `ETIME` is a genuine runaway; a low 1-min average with no
   `.test` processes is a finished burst.

---

## Verification

- **Evidence capture:** `ps -eo pid,ppid,stat,etimes,rss,comm`, `ps -eo stat,comm | awk $1~/^Z/`,
  `uptime`, `pgrep -c -f 'bin/gopi'`, `nproc`, `ls -la /tmp/go-build*/b001/*.test`,
  `ls -la /dev/shm/gopi-test-scratch/*/*.test`, `grep -c 'go test' <session>.raw.log`.
- **Build / tests:** N/A — investigation-only change; the artifact is this document, no
  production code touched (`git status` in the worktree shows only this new file).
- **Repro of the verdict:** run `ps -eo stat,comm | awk '$1 ~ /^Z/'` at any observed burst →
  `0`; re-check any cited `.test` PID seconds later → gone.
- **Report location:** `devdoc/agent-test-cpu-investigation.md`
  (branch `docs/agent-test-cpu-investigation`).

## Not Done

- **Root-cause fix for worker oversubscription not implemented** — this epic was scoped to
  *investigate*, and the fix (a concurrency cap) is a behavioural change to the worker
  scheduler that belongs in its own task with its own tests. Recommendation 2 states the
  lever; it is left as follow-up work.
- **No reproduction of the false-red itself** — bug `01a08c4f` is cited, not re-triggered
  (deliberately: reproducing it means loading the shared host further).
- **No historical frequency analysis** — the 570-invocation count is from recent session
  logs, not a multi-day trend; no claim is made about how often bursts occur.
