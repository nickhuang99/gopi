<!-- doc-class: historical -->
# `merger --status`: showing the merger's log path (row "merger should show its log path")

## The question that had no answer

`gopi merger --status` answered *is it running*, *what would it merge*, and (from
row 01a0a79b) *what is it blocked on*. The next question an operator asks the
moment a pass wedges — **where do I read why?** — had no line. The report printed
the lock path and the in-flight stage, so the reader knew something was stuck in
`stage verify` and still had to go hunting through the profile's `logs/`
directory by hand.

## The premise in the briefing, corrected by measurement

The briefing says *"it is a gopi session, it should have a session log"*. **A
merger is not a gopi session.** Measured: the merger loop never constructs a
session logger (`NewSessionLogger`), so it produces **no JSONL session log and no
raw HTTP log** at all. What it has is:

- `merger --daemon` spawns the loop with its **stdout and stderr both attached to
  one file before the fork** (`cmd/agent/cli_daemon_spawn.go`, step ②),
- the file is `<gopi data root>/logs/merger.log` — resolver
  `mergerDaemonLogPath()` → `daemonLogPathUnder(mergerDaemonLogBaseName)`
  (`cmd/agent/cli_merger_daemon.go`), already named in the start note
  (`merger daemon started: pid N, log <path>`) and in the `merger` help footer.

So the deliverable is **not** to invent a session log for the merger; it is to
surface the log the merger actually writes. That is also why the line is useful
for a merger that is *not* running: the log of the merger that died is the only
evidence left of why it died.

Measured ground truth on the live profile (host `~/.gopi`):

```
$ gopi merger --status          # before this change: no log path anywhere
$ readlink /proc/638001/fd/1 /proc/638001/fd/2
/home/user/.gopi/logs/merger.log
/home/user/.gopi/logs/merger.log
```

## What the status now says

```
merger:      alive
lock file:   /home/user/.gopi/serve.lock
lock holder: pid 638001
log file:    /home/user/.gopi/logs/merger.log
in flight:   pass started …, stage verify
```

`log file:` sits **in the same column** as `lock file:` (width 4 characters in
`mergerLogLineLabel`): the lock says *whether* a merger runs, the log says *what
it is doing*, and they are read together. The label constant carries its own
column width, exactly as the three labels above it carry theirs inline.

Both faces render it — `gopi merger --status` and its alias
`gopi task merger --status` — because both go through the single renderer
`MergerStatus.Render()`. The CLI resolves the path once
(`mergerLogPathReadOnly()`) and passes it in, mirroring how `lockPath` and
`stateDir` are already supplied: **the file name keeps its single definition in
`cmd/agent`** (Rules 1b/13/14) instead of being mirrored in `internal/tools`.

## The qualification — a bare path is a half answer

Three shapes would make a plainly-printed path *misleading*, so each degrades to a
named answer instead (Rule 15 — no silent failure, no claim where there is no
measurement):

| shape | rendered |
|---|---|
| the path exists (healthy) | the bare path, nothing else |
| no merger has ever written there | path + `(not created yet — no merger has written here in this profile)` |
| the data root could not be resolved | `log file:    (unavailable — this profile's gopi data root could not be resolved)` |
| **a LIVE holder whose stdio measurably goes elsewhere** | path + `(a live merger IS running, but its stdout/stderr do not resolve to this path — measured: <actual target>)` |
| a live holder whose descriptors cannot be read | path + `(the live merger's stdout/stderr could not be read — cannot say whether it writes here)` |

The fourth row is the one that keeps the line from being a lie: a daemon started
**by hand under a shell redirect** (`nohup gopi merger >> /some/other/merger.log &`) never
touches the profile's log file, and printing the conventional path unqualified
would send its operator to tail a file nobody writes. The measurement is the same
kernel-fact technique the rest of this file already uses — `os.Readlink` on
`/proc/<pid>/fd/1` and `/proc/<pid>/fd/2`, the descriptors the live holder
actually holds. An appended-to file whose writer has been silent for an hour
still resolves correctly, which is exactly the wedged case.

**"Cannot read" is deliberately not the same word as "writes elsewhere."** An
empty measurement (no procfs, a hardened `/proc`, a pid already exiting) yields
`could not be read`, never a verdict. A descriptor that resolves to *no path at
all* (`socket:[…]`, a pipe) is returned verbatim and printed as the measured
target — a measurement, not an error.

Nothing is claimed when there is no live holder: the path is then simply the one
this profile uses.

## Files

- `internal/tools/merger_status.go` — `MergerStatus.LogPath`, the label/note
  constants, `renderLogLine`/`logNote`, `mergerLogWriterTargets`,
  `MergerStatusReport(lockPath, stateDir, logPath)`.
- `cmd/agent/cli_tasks.go` — `mergerLogPathReadOnly()` (the read-only twin of
  `mergerDaemonLogPath`, creating nothing, degrading to `""`).
- `internal/tools/merger_status_log_test.go` — the rendering contract + the
  measurement against a **real child process**.
- `cmd/agent/merger_status_cli_test.go` — the CLI-face assertion: both faces must
  print the path the spawner's own resolution produces
  (`<home>/<profile dir>/logs/<merger log name>`, built from the same constants).
- `.evid/demo-logpath.sh` / `.evid/demo-elsewhere.sh` — the real-run probes, with
  their captured output (`.evid/logpath-evidence.txt`, `.evid/elsewhere.txt`).
  Both TAKE their sandbox root with `mktemp -d` (so it honours the caller's
  `TMPDIR`, which gopi keeps off the `/` partition — Rule 22) and kill + wait out
  every probe process; neither touches the host profile.

## Not done (honest)

- **No `--json` face was added.** `merger --status` has no machine-readable face
  at all today (`--json` is not a flag of this verb; `gopi merger --status --json`
  falls through to the help text). Adding one is a separate surface with its own
  naming/shape decisions, not a line on this one.
- The measurement reads `/proc` directly, so on a host without procfs the line
  degrades to "could not be read" (by design — see above), it does not fall back
  to guessing.
