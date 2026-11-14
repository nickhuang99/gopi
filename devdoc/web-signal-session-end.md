<!-- doc-class: historical -->
# Fix: TestWebToolsSignalShutdownWritesSessionEnd — the sandbox env never reached the child

**Branch:** `fix/web-signal-session-end` (worktree `~/workspace/gopi-web-signal-end`, base 0d7c979)
**Commit:** see `git log --oneline -1` at the end of this file's "Verification" section.
**Ticket:** bug(test) — "readiness probe races the session-file write (`go test ./cmd/agent/` cannot go green)".

## Symptom

`go test -count=3 -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/` → 3/3 FAIL:

```
cli_web_align_test.go:499: no session dir/files under /dev/shm/gopi-test-scratch/.../001:
  open <...>/.gopi/sessions: no such file or directory
```

## Root cause (NOT the reported one)

The ticket hypothesised a readiness race: "the probe is POST /api/chat, the
listener is up as soon as the socket binds, then SIGTERM kills the server before
it writes the session file". That is not what happens. The real defect is one
line, and it is a silent no-op:

```go
mp := testutil.StartMockProcess(t, bin, "web", "--addr=127.0.0.1:"+port) // ← starts the child
cmd := mp.Cmd()
cmd.Env = append(os.Environ(), "GOPI_HOME="+home)                        // ← TOO LATE
```

`StartMockProcess` calls `cmd.Start()` itself, and `exec(2)` freezes the
environment at that moment. Assigning `cmd.Env` afterwards changes **nothing**
for the already-running child (`os/exec` only reads `Cmd.Env` in `Start`). The
spawned `gopi web` therefore ran with the *parent's* environment: it logged into
the operator's **real `~/.gopi/sessions`**, while the test asserted against the
empty `t.TempDir()` sandbox. The failure message — "no session dir/files under
<t.TempDir()>" — is exactly what an empty sandbox looks like, and it is
deterministic on a fast idle host, which is why it looked like a race to the
filer.

### Evidence

1. **Deterministic repro of the red test** on unmodified master (0d7c979), this
   host: `-count=3 -v` → 3/3 FAIL, ~2-3s each.
2. **A/B on the same binary and the same package**: the original function,
   extracted *verbatim* (renamed only), FAILED 2/2, while an otherwise identical
   replica that wired the env **before** the spawn PASSED 2/2. The only
   difference between the two runs was line 465.
3. **Direct proof the sandbox is bypassed**: running the original test once grew
   the real `~/.gopi/sessions` from 21015 to 21017 entries, with `session_start`
   `workdir` = the test's CWD. The child wrote a complete session — just not
   where the assertion looked.
4. **The other reported suspect was ruled out**: `internal/scratch`'s GC (the
   `suite-`/`queue-` markers, 6h StaleAge, /proc holder scan) is namespace-, age-
   and holder-limited and never touches a `t.TempDir()`; the observed error is a
   plain ENOENT on an empty directory.

## Secondary defect found (and fixed): readiness proved nothing

The old probe loop was "can I POST to /api/chat?", which is true as soon as the
listener binds — it never waited for the artifact. Worse, the first POST *is a
real turn*: the probe started a model call and then SIGTERMed the server ~ms
later, which is a genuine (if less deterministic) hazard — the server spends
`webShutdownGrace` (5s) waiting for the in-flight turn, and the old 15s exit
budget had to cover it.

**Fix:** readiness is now the artifact the assertion reads — the session JSONL
under the *sandbox* sessions root, written by agent construction *before*
`Serve` — and the single driven turn is consumed to its terminal `event: done`
frame, so the SIGTERM hits a settled server.

## Third finding: the skip was unnecessary

`gopi web` boots, creates `sessions/<uuid>/session.jsonl` and writes the single
`session_end` on SIGTERM **with no API key at all** (verified:
`env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_API_KEY` → the
session file and exactly one `session_end`). The `DEEPSEEK_API_KEY` skip was
therefore removed: the test now always RUNS, which kills the reported hazard that
"a non-verbose `go test` prints neither PASS nor SKIP, so an 'ok' line from
another host proves nothing about whether this test ran". The test still uses a
real model call when a key is present (its live-e2e value is preserved), but the
assertions hold either way.

## What changed (cmd/agent/cli_web_align_test.go)

- **Env before spawn**: `testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, "", env, "")`
  — the standard launcher wires `Env` before `Start`, captures output
  race-free, and owns the pgid-scoped reaper (no stray `gopi web` can survive a
  failing assertion).
- **No hardcoded port**: `--addr=127.0.0.1:0` (kernel-assigned) + the listening
  banner (`p.WebBaseURL()`) — the old fixed `18477` could collide.
- **Readiness = the artifact**: `p.WaitFor("the sandboxed session JSONL", …)`
  polling `home/.gopi/sessions` (one level down, the S1 bare layout). A
  mis-wired `GOPI_HOME` now times out *naming the sandbox*, instead of failing
  with a confusing "no session dir/files".
- **One settled turn**, read to `event: done` with a bounded client timeout; a
  model failure is tolerated (logged), a broken stream is fatal.
- **SIGTERM + bounded exit** with `webSignalExitTimeout = 20s` (> the server's
  `webShutdownGrace = 5s`), and the exit code is now asserted to be 0.
- **Structure assertions via the library** (`testutil.LoadSession` +
  `AssertSingleSession` + exact counts of `sessionevent.EventTypeSessionStart` /
  `EventTypeSessionEnd`), replacing the hand-rolled JSONL scanner.
- **Named constants (Rule 1a/1b/13/14)**: `webSignalBootTimeout`,
  `webSignalPollInterval`, `webSignalTurnTimeout`, `webSignalExitTimeout`,
  `webSignalSessionFileBase`; `config.SessionsDirName` and the
  `sessionevent.EventType*` constants instead of bare `"sessions"` /
  `"session_end"` literals.
- Helpers `firstSessionJSONL` / `isSessionJSONLName` (one-level scan, S1
  layout-aware).

## Red proof / regression guard

- **Pre-existing red, captured**: on master 0d7c979 with the UNMODIFIED test,
  `-count=3 -v` → 3/3 FAIL (reproduced twice, plus the verbatim-function A/B
  2/2 FAIL vs the pre-spawn-env replica 2/2 PASS).
- **Post-fix green**: `-count=3 -v` → 3/3 PASS; also green with every
  `DEEPSEEK_API_KEY`/`ANTHROPIC_*` variable removed from the environment (2/2).
- The test is now its own regression guard for the *class* of bug: because the
  readiness wait reads the sandbox and the child is spawned with the sandbox env,
  re-introducing a post-spawn `cmd.Env` assignment (or any other `GOPI_HOME`
  plumbing break) fails the boot wait loudly rather than passing vacuously.

### Honest scope of the red proof (follow-up check)

The acceptance asked for "a red-proof that reverting the wait makes it fail
again". Checked directly, and the answer is **no — and that is a finding, not a
pass**:

- **Red (proven, re-measured after the fix):** breaking the env wiring —
  `env := os.Environ()` with no `GOPI_HOME` — makes the test FAIL in 30.2s
  (`timed out waiting for the sandboxed session JSONL`), with the child's own
  stderr naming the operator's real `~/.gopi/sessions/<uuid>/session.jsonl`:
  the child logs outside the sandbox and the wait times out. That is the
  actual defect reverted, and it is red.
- **NOT red (measured):** deleting *only* the artifact-readiness line
  (`p.WaitFor("the sandboxed session JSONL", …)`) while keeping pre-spawn env,
  the `--addr=:0` banner wait and the settled turn still PASSES 2/2 on this
  host. The `event: done` turn handshake already guarantees the session JSONL
  exists, so the extra wait is redundant *here*, not load-bearing. It is kept
  because it is the direct readiness signal (it does not depend on a model
  round-trip, so it holds on a host with no reachable model), but nobody should
  read its removal as a red-proof of the fix.
- **The load-bearing revert is the env wiring** (env before spawn), which is the
  actual defect, and that revert IS red.

## Not done / limitations

- The full `cmd/agent` suite was run as the gate (`go test -count=1 ./cmd/agent/`);
  unrelated pre-existing flakes in the package (documented in memory #1319,
  e.g. `TestTaskRunSurvivesClosedStdoutPipe`) are out of scope for this fix.
- `gopi web` still has no health/ready endpoint; the test waits on the session
  file. A future `--print-ready` / readiness line would be a cleaner seam, but
  that is a production change, deliberately not taken here (this ticket is a
  test fix).
- The two throwaway diagnostics used during the investigation
  (`cmd/agent/zz_debug_signal_test.go`, `cmd/agent/zz_orig_verbatim_test.go`,
  `dbg-web*.sh`) were deleted before the commit; the evidence they produced is
  recorded above.

## Verification

```
# build
CGO_ENABLED=0 go build -buildvcs=false -o bin/gopi ./cmd/agent/     → OK

# acceptance (reportedly red before, green now)
go test -count=3 -v -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/
   --- PASS ×3   (0.94s / 0.80s / 1.08s)   ok  github.com/nick/gopi/cmd/agent  2.900s

# no API key in the environment (proves the skip removal is sound)
env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN -u ANTHROPIC_API_KEY \
  go test -count=2 -v -run TestWebToolsSignalShutdownWritesSessionEnd ./cmd/agent/
   --- PASS ×2   ok  github.com/nick/gopi/cmd/agent  3.229s

# package gate
go test -count=1 ./cmd/agent/    → ok  github.com/nick/gopi/cmd/agent  176.957s  (exit 0)
```

**Package gate:** `go test -count=1 ./cmd/agent/` → `ok  github.com/nick/gopi/cmd/agent  176.957s` (exit 0) — the
package the ticket said "cannot go green" is green with this fix in place.

**Commit:** `fix/test-web-signal-session-end` on branch `fix/web-signal-session-end`
(worktree `~/workspace/gopi-web-signal-end`) — see the commit hash printed by
`git log --oneline -1` in that worktree.
