<!-- doc-class: normative -->
# Worker "gone" / phantom-worker investigation (task 01a09df5 "worker is gone")

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Operator report (attachment 1, verbatim shape):

```
gopi worker pid 2642443: idle — gopi-worker worker (no state file …)
gopi worker: 14 stale state file record(s) found — NONE of these is a worker:
  worker-state-2622147: pid 2622147 is gone — SIGKILL residue, no cleanup ran
  … (14 of them)
to clear them now: rm -f /home/user/.gopi/worker/worker-state-*   (14 file(s))
```

Two questions were asked: **why "SIGKILL"?** and **why do workers seemingly
spawn themselves, when only 3–4 are started by hand?**

## Completed

### 1. "SIGKILL residue" was an unprovable claim (root cause of the alarm)

`residueReasonDead` rendered **"pid %d is gone — SIGKILL residue, no cleanup
ran"**. A state file's own bytes cannot distinguish a SIGKILL from a crash,
from a driver that died holding it, or from **a foreign writer that never owned
the record**. Measured on this host: the 14 files were not workers at all —
they were written by a **test fixture** (see 2). The claim sent the operator
hunting a killer that never existed, which is exactly what the report did.

Fix: the reason now names the fact it measured and refuses the cause —

```
pid %d is gone — cleanup never ran (hard kill, crash, or a foreign writer;
the file alone does not say which)
```

The same overclaim was removed from the file's header comment, the
`workerStateResiduePrune` doc and the dead-pid branch comment.

### 2. The phantom workers were a TEST FIXTURE writing into the real profile

**Not self-spawn.** No production path creates a `worker-state-<pid>` out of
nothing: `flush()` writes the file from inside a running worker, `Forget()`
removes it, `pruneWorkerStateResidue()` removes dead pids' files. There is no
spawn site that could multiply workers.

Contract: `tools.ControlDir(name)` → `config.GopiDir()` → `$GOPI_HOME/.gopi`,
else `$HOME/.gopi`. A child process's env cannot relocate the PARENT.

`cmd/agent/status_action_conflict_e2e_test.go` (TestStatusWithActingActionTouchesNoRequestFile)
seeded a "live-looking" worker record with `tools.ControlDir(workerStateDirName)`
**in-process** while sandboxing only the child (`sscPrivateHome` returns an env
vector for `runGopiIn`). The seed therefore landed in the operator's real
`~/.gopi/worker`, named after the **test binary's own pid** — and the test
*passed*, because it only ever asserted on its private home.

**Red-proof (captured twice on this host).** With the hermetic pin temporarily
removed, one run of that single test:

```
=== DELTA without the fix ===
7a8
> worker-state-3542103
ok  	github.com/nick/gopi/cmd/agent	3.229s     ← still green
```

With the pin in place, the identical run leaves the real directory byte-identical
(`DELTA: EMPTY`). Residue fingerprints consistent with the mechanism: one pid in
the report (`3487624`) is an `agent.test` process, seven more are gone
`agent.test`/`go test` children, and every file contains the exact fixture body
`{"pid":N,"started":"2026-09-13T00:00:00Z","state":"idle"}`.

Fix: `t.Setenv(config.GopiDirEnv, home)` in that test **plus** a suite-level pin
so the whole tree is hermetic, not just this one fixture:

- `cmd/agent/testmain_test.go`: `testutil.PinSuiteGopiProfile(cmdAgentSuiteName)`
  — the write-side counterpart to the read-side pin `internal/tools` already had
  (`internal/testutil/suiteprofile.go`), and the guard
  `TestSuiteGopiProfileIsSandboxed` (`AssertSuiteGopiProfileIsPinned`), which was
  the only suite missing it.

### 3. Honest limit of the fix

A `go test ./cmd/agent/` run still fails on this host for an unrelated,
pre-existing reason: `TestWorkerDaemon_StatusReportsTheLiveDaemon` asserts the
status output does not contain `"<home>/.gopi/worker"`, but the "no workers"
block prints the sibling path `"<home>/.gopi/worker.lock"` — a **leftover
collision at master**, not caused by this change. The suite-level pin makes the
`cmd/agent` suite hermetic, which incidentally makes that collision visible
instead of masked by ambient state.

### 4. The durable guard: no test may resolve the developer's real ~/.gopi

A file-level pin fixes today's leak; it does not stop a NEW fixture from
re-introducing it (the failing test stays green — the damage surfaces in the
operator's next `worker --status`). The ratchet is a Rule 16-21-style standards
gate: `internal/standards/ambient_profile_gate_test.go`,
`TestNoTestResolvesTheAmbientGopiProfile`, scanning all 32 test packages for any
call to `config.GopiDir()` / `tools.ControlDir()` / `tools.ControlDirReadOnly()`
in a top-level test function that neither pins the profile (an argument naming
`config.GopiDirEnv` or the `"GOPI_HOME"` literal) nor carries
`// ambient-profile-ok: <reason>`. A package whose `TestMain` pins is covered
wholesale.

Red-proof is non-vacuous end to end: dropping `PinSuiteGopiProfile` from
`cmd/agent`'s `TestMain` makes the gate fail on the REAL tree
(`cmd/agent/cli_worker_status_test.go:713`), not merely on a synthetic snippet.
Tree-wide the gate has exactly ONE true positive —
`internal/agent/sessionend_tail_equivalence_test.go:178` (`realSessionsRoot`), a
read-only real-tree oracle that SKIPS when the tree is absent — annotated as a
reviewed exception so it stays visible in the data report.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (worktree `gopi-ambient-gate`,
  branch `fix/ambient-profile-gate` @4598f03c).
- Red-proof of the reason fix: `TestWorkerStatusResidueReasonClaimsNoUnprovableCause`
  RED on the pre-fix string, GREEN after (worktree history).
- Red-proof of the fixture leak: measured delta `worker-state-3542103` without the
  pin (test still green), empty delta with it.
- New guards: `TestWorkerStatusResidueReasonClaimsNoUnprovableCause`,
  `TestWorkerStatusEnumerateTouchesNoFileOutsideItsDir`,
  `TestSuiteGopiProfileIsSandboxed` — `go test ./cmd/agent/ -run 'TestWorkerStatus|TestStatusWithActingActionTouchesNoRequestFile' -count=1` → PASS.
- `go test ./internal/standards/` → PASS.
- **Real-profile delta after the WHOLE `cmd/agent` suite (the decisive
  measurement, 19:44–19:53):** `go test ./cmd/agent/ -count=1` (531s, `ok`) with
  the operator's real `~/.gopi/worker` fingerprinted before and after — the
  listing hash is unchanged (`aeb40458…`, 19 files) and the newest file's mtime
  (19:33:02) PREDATES the run. Under the same conditions a pre-fix worktree
  (no pin) added `worker-state-<its own test pid>` at every invocation.
- **Second mechanism measured live (the operator's second question, answered
  with a live observation rather than an argument):** a run of the `cmd/agent`
  suite from worktree `gopi-wo-gate-red` printed four
  `gopi worker pid N` lines whose argv was
  `.../TestGlmNightWatchOperatorPathStartsWorkersUnattended.../bin/gopi worker`
  — test fixtures that LAUNCH worker processes. They are stopped by the suite's
  own teardown (hence no state files), and the fixed reader correctly reports
  them as live pids with an argv-fallback note. So the second source of "so
  many workers" is `go test ./cmd/agent/`, which starts real workers on purpose
  — those runs, not a self-spawning worker, are what the operator's process
  table showed.
- **"SIGKILL" is refuted host-wide, not merely unproven:** `signal-
  provenance.log` (43,935 bytes, 62 records) contains **zero** SIGKILL records
  and **exactly one** signal ever delivered to a `role=worker` process — a
  SIGTERM to pid 2140461 at 2026-09-14 02:54:45. `dmesg -T | grep -i oom`
  reports two kills in the whole ring buffer (rustc, 2026-08-23; python3,
  2026-09-11) — no gopi process, so the kernel OOM path is out too. And no
  worker-spawns-worker site exists at all: `daemonSpawn` has exactly two
  production callers (`cli_worker_daemon.go:361/368` for `gopi worker --daemon`,
  `cli_merger_daemon.go:217` for `gopi merger --daemon`).
- **Full-suite A/B on the branch (19:41, 661s):** `go test ./... -count=1` on
  `fix/ambient-profile-gate` fails in exactly TWO packages, both reproduced in
  clean master worktrees with none of this branch's changes present
  (`/dev/shm/gopi-vf-master` @16f871c3 and `gopi-master-pre` @1335daed):
  `internal/stylegate` `TestStyleGate` (`internal/doubaoasr/doubaoasr_test.go:458`
  — a `time.Sleep` the sweep's ceiling is 0 for; filed as task `01a09fba`) and
  `internal/testutil` `TestWorkerOwnershipPointerPresent` (already tracked as
  `01a09e6b`). This branch touches neither package (`git diff master...HEAD
  --name-only` = 3 files). Everything else is `ok`, including
  `internal/standards` (all seven Rule 16–21 gates plus the new one).

## Not Done

- The `worker.lock` substring collision in `TestWorkerDaemon_StatusReportsTheLiveDaemon`
  (pre-existing at master) — submitted as its own bug task.
- Deleting the operator's remaining `worker-state-*` files: they are residue of
  the fixture era; `rm -f ~/.gopi/worker/worker-state-*` is safe (the next worker
  start sweeps dead-pid files anyway).
- **OPERATOR NOTE — who is still adding files (measured 2026-09-14, after this
  branch's fixes were written).** The per-fixture fix (b75eea2b) and the suite
  pin (2822c1cb) are ALREADY on master (16f871c3), so a run from a master
  checkout is clean — this branch's own `go test ./cmd/agent/` left the directory
  byte-identical (`md5sum` unchanged). Every new file arriving after that came
  from an OLDER worktree still pinned at a pre-fix commit. Measured live at
  19:33 — `worker-state-3805938` materialised while the test binary
  `goprocessor/…/agent.test` (pid 3805938, started 19:26) was running with
  **cwd = `/home/user/workspace/gopi-voice-client-e2e`** (HEAD 91e9f7ac, LACKS
  the per-fixture fix). Sibling worktrees measured in the same state:
  `gopi-daemon-cwd` (5c4569d6), `gopi-ledger-leak` (b9ae5cbc), `gopi-wo-pointer`
  (67445327). The count grew 14 → 19 over roughly half an hour purely from those
  runs. This branch adds the RATCHET (the standards gate) so the pattern cannot
  come back through a new fixture; the growth stops as the older worktrees are
  merged or removed.
- The "SIGKILL" wording that remains in prose where it is a *possibility*
  (the header's list of causes) is deliberate: prose may enumerate causes, a
  per-file rendered verdict may not assert one.
