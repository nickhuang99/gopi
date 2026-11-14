<!-- doc-class: historical -->
# Fix: `TestWorkerDaemon_StatusReportsTheLiveDaemon` red at master — and the second defect it was hiding

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Task: master `1335daed` red, `cmd/agent: TestWorkerDaemon_StatusReportsTheLiveDaemon`.
Branch: `fix/worker-status-lockdir-assert` (worktree `~/workspace/gopi-worker-status-assert`,
base `16f871c3`).

## Repro (deterministic)

```bash
cd ~/work/GoPi
GOPI_HOME=/dev/shm/wg-check/amb go test ./cmd/agent/ \
  -run 'TestWorkerDaemon_StatusReportsTheLiveDaemon' -count=1
```

## Defect 1 — the assertion measured a string, not its intent

`worker_daemon_singleton_e2e_test.go:143` asserted

```go
strings.Contains(out, filepath.Join(home, ".gopi", workerStateDirName))   // "worker"
```

The no-daemon block **legitimately** prints the lock path `<home>/.gopi/worker.lock`,
which *contains* `<home>/.gopi/worker`. The intent was "no PID/record of THIS
home was named"; the assertion said "this string must not appear".

**Fix (anchoring, not widening).** The needle is the state file's own path shape —
`<home>/.gopi/worker/` **with the trailing separator**:

```go
needle := wdWorkerStateDir(home) + string(filepath.Separator)   // <home>/.gopi/worker/
```

Nothing is weakened: a real state line (`… .gopi/worker/worker-state-<pid>`) and
the residue listing's remedy (`rm -f <dir>/worker-state-*`) both carry the
anchored form, while the lock path does not. The lock path is still printed
(it is useful) — the check was fixed, not the output.

`wdWorkerStateDir`/`wdWorkerLockPath` build the paths from the *production*
constants (`testutil.GopiProfileDirName`, `workerStateDirName`,
`workerLockFilename`) — no bare `".gopi"`/`"worker"` literals (Rules 1b/13).

## Defect 2 — the argv "fallback" pre-empted the strong source (found by the fix)

While re-checking the daemon-alive half (`"state "` + `worker-state-<pid>`) the
assertion kept failing with a *different* message:

```
gopi worker pid 4158421: idle — gopi-worker worker
  (no state file — identified by argv; it predates the state record or could not write it)
```

…while `<home>/.gopi/worker/worker-state-4158421` existed on disk with the
record in it. Independent repro (sandboxed profile, real binary):

```
$ gopi worker --daemon           → worker daemon started: pid 4158421
$ ls $GOPI_HOME/.gopi/worker/     → worker-state-4158421
$ gopi worker --status            → "no state file — identified by argv"
```

**Root cause (mechanical).** `workerStatusEnumerate` ran the argv (process-table)
half FIRST and set `seen[pid]` for every shape match, so the state-file half
skipped the pid — rendering every *recorded* worker (including a live daemon)
from the WEAKEST source, and suppressing the idle|busy word, the task/session,
the version and the round stamps the record exists to carry. The package header
documents the opposite contract ("process-table (the fallback)"), and
`workerStatusFallbackNote` claims a fallback is only for a worker that "could
not write it".

**Fix.** The record half runs first (authoritative), the process-table half last,
skipping `seen` pids — the dedupe is still true by construction, and the emitted
worker is now the recorded one. A worker with no state file is still found
(fallback tests unchanged).

**Fixture half.** The E2E probe binary was named `gopi-worker-daemon`, so the
live daemon's `/proc/<pid>/comm` was `gopi-worker-dae` (kernel 15-char
truncation) and the reader's attestation (`comm == procname.NameGopi`) rejected
its own record as a "RECYCLED pid". Production uses `bin/gopi` (comm `gopi`;
measured on the launcher's live workers). `wdBinName = procname.NameGopi` — the
fixture now has the production shape, no production rule relaxed.

## Red proof — the tightened assertion still bites

`TestWorkerDaemon_StatusAssertionCatchesASeededStateFile` seeds a state file of
THIS home (a dead pid — the residue shape a leaked fixture leaves), runs the
**real** binary over that profile, and requires the report to name the seeded
path; it then pins the false positive itself: the no-daemon block's lock line
(`<dir>worker.lock`, built from the production constant `workerStatusNoWorkersLock`,
so it cannot drift from the renderer) carries the bare directory spelling yet
must NOT fire the anchored needle. Both halves are hermetic — the assertion is
about THIS home, so unrelated workers on the host neither satisfy nor suppress it
(the first draft measured the live report instead, and the ambient workers of the
host suppress its no-daemon block; the full-suite run exposed that as ambient
coupling).

**A/B on the assertion itself** (same binary, same profile):

| needle | `TestWorkerDaemon_StatusReportsTheLiveDaemon` |
|---|---|
| `wdWorkerStateDir(home)` (the old bare form) | **FAIL** — fires on the legitimate `lock: …/.gopi/worker.lock` line |
| `wdWorkerStateDir(home)+separator` (anchored) | **PASS** |

So the anchored form fixes the false positive, and the seeded-file proof above
shows it is not a widened check.

## Not Done

- `pruneWorkerStateResidue` still writes fixture residue into a profile's worker
  dir on any worker start; that leak is a separate report
  (`devdoc/worker-phantom-selfspawn.md`) and was not touched here.
- The daemon-alive assertion still requires the daemon's record to be attested;
  that is the intended contract, not a gap.

## Verification

- `go build` / `go vet` (cmd/agent): OK
- `GOPI_HOME=/dev/shm/wg-check/amb go test ./cmd/agent/ -run 'TestWorkerDaemon_StatusReportsTheLiveDaemon|TestWorkerDaemon_StatusAssertionCatchesASeededStateFile' -count=1`: **ok**
- `GOPI_HOME=/dev/shm/wg-check/amb go test ./cmd/agent/ -run TestWorkerStatus -count=1`: **ok**
- `GOPI_HOME=/dev/shm/wg-check/amb go test ./cmd/agent/ -count=1`: see report footer
- `go test ./internal/standards/ -count=1`: **ok** (Rules 16–21 gates green)
