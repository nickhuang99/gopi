<!-- doc-class: normative -->
# The terminal-scope hint: a foreground worker says whose lifetime it is in

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a163f3-7181-7008-9ff9-27df3f39f575`. Measured defect, 2026-10-22 22:04:55.

## The defect

A foreground `gopi worker <task-id>` inherits the **terminal tab's** systemd
scope. Closing that tab — or ending the ssh session it came from — destroys the
scope and **hangs up every member**. The run's own
`~/.gopi/sessions/<id>/signal-provenance.jsonl` recorded it:

```json
{"signal":"SIGHUP","pid":1190977,
 "cgroup":"…/app-org.gnome.Terminal.slice/vte-spawn-c6f69923-….scope",
 "role":"worker","argv":"gopi worker 01a163df-…","sourced":false}
```

`sourced: false` — no gopi stop path claimed the kill. The worker died mid-round
with its row still `in_progress`. The remedy already existed (`--daemon`, which
acquires its own cgroup through the daemon seam in `cli_daemon_scope.go`); what
was missing was **anything at startup that said so**.

## What was added

**1. `cgroupscope.TerminalScope(rel)`** (`internal/cgroupscope/cgroupscope.go`).
It answers a question *narrower* than `InSessionScope`: not "does the scope
teardown reach me" (true for a desktop/app/container scope too) but "is there a
**terminal** whose closing takes me with it". So it requires `InSessionScope`
AND the owning scope's name to carry a terminal marker:

| marker | how it matches | why |
|---|---|---|
| `vte-spawn-` | prefix | GNOME Terminal's per-tab scope — this host's own shape |
| `session-` | prefix | systemd-logind's `session-<n>.scope`, an ssh login |
| `tmux` | containment | the distributions that ship a tmux scope wrap the word (`app-tmux-*`, `tmux-spawn-*`) |

Two things this table learned the hard way, both of which a red proof caught:

- **The owning scope, not the leaf.** A process one cgroup *below* a terminal
  tab is still that tab's member, and the leaf there is a plain child directory.
  `owningScopeName` walks up exactly like `InSessionScope` does, so the two
  predicates cannot disagree about which unit owns a path.
- **Prefix, not containment, for `session-`.** This host really produces
  `app-gnome-gnome-session-quit-34290.scope`, which *contains* `session-` and is
  a GUI app scope, not a login. Containment there would have fired the banner on
  a desktop scope that nothing the operator closes can take down.

The **escape target is excluded first** (`EscapeTargetPrefix`), by the same
constant `InSessionScope` uses: `gopi-daemon-worker-<profile>.scope` exists
precisely to leave a terminal's lifetime, and a marker word inside a profile tag
must never resurrect the verdict.

**2. The banner**, `reportWorkerTerminalScopeHint()` in
`cmd/agent/worker_start_notices.go`, called from `main.go`. One line on stderr:

> `gopi worker: this worker runs inside the TERMINAL's systemd scope (<measured
> path>), so closing that terminal or ending the ssh session takes it down
> mid-round (SIGHUP). Start it detached with `gopi worker --daemon` to survive
> it; a foreground run stays legitimate for debugging.`

It names the **measured path** so an operator can hand it to `systemd-cgls` and
check the claim, and it builds the remedy from `daemonFlagLong`, so the advice
cannot drift from the flag.

## The decisions that are load-bearing

- **A HINT, never a refusal.** A foreground run is the documented way to watch a
  worker work a row. Nothing about the process model changed.
- **`--daemon` never prints it.** The call site sits *after* the `--daemon`
  branch, which returns earlier; a static pin asserts that ordering so a later
  reshuffle cannot quietly move the line onto the face that already escaped.
- **Silence when the measurement fails.** `SelfRel` reads `/proc/self/cgroup`
  (Rule 24: nothing else is a verdict). If the read fails — cgroup v1, a
  hardened `/proc` — the hint prints **nothing**. A hint that guessed would
  teach an operator to skip the one line that matters where it does fire.
- **`cgroup v1` / empty is not a terminal.** Covered by the same silence.

## Verification shape

- `internal/cgroupscope/terminal_scope_test.go` — the predicate, including the
  leaf-vs-owning-scope case and the `app-gnome-…-session-quit` trap.
- `cmd/agent/worker_terminal_scope_hint_test.go` — the three epic cases
  (terminal → line, daemon scope → nothing, empty/failed read → nothing), the
  printer, the reuse pin, and the call-site ordering pin.
- Real runs: a foreground `./bin/gopi worker <id>` printed the line verbatim;
  `./bin/gopi worker --daemon` in a sandbox produced `/proc/<pid>/cgroup` =
  `…/gopi-daemon-worker-<profile>.scope` and `grep -c` for the line in its log
  returned **0**.
