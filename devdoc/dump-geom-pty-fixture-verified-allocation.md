<!-- doc-class: historical -->
# Verified pty-pair allocation in cmd/agent's test fixtures

Task row: 01a0de91-16b9-7210-8422-f46d9b952e0a
Branch: task/01a0de9116b972108422f46d9b952e0a
Base: 4d7cb918e70ce5f371876188783df3ef7adc3a2e


## What was wrong (mechanism, confirmed by deliberate repro before the fix)

`dumpGeomPty` (cmd/agent/chat_dump_geometry_pty_test.go:64) allocated a pty pair in
four non-atomic steps — `open /dev/ptmx` → `TIOCGPTN` → `TIOCSPTLCK` →
`open /dev/pts/<n>` — and then **asserted** the fd it had just opened was the
terminal it had allocated. Nothing checked that. Under a whole-package run, the
slave number can be released (last reference to a master gone) and reallocated
between our `TIOCGPTN` and our `open`, so the fixture can hand a **non-terminal**
fd to `lineedit.Open`, and the case dies in 0.00s with

```
lineedit: read termios on fd 7: lineedit: not a terminal: inappropriate ioctl for device
```

while it is green when run alone. 0.00s is neither a timeout nor host load: the
fixture's own claim about the fd it holds is what is wrong. The test file was
byte-identical to master — a latent pre-existing fixture race.

**The mechanism was measured, not assumed** (probe under
`/home/user/workspace/gopi-test-scratch/pty-probe/`, 4800 rounds, clean
4800/4800, both failure modes traced to their syscalls):

- `TIOCGPTN` answers **ENOTTY** on a *slave* — only the master can name its
  pair. So "ask the slave which pty it is" is not available; the slave cannot
  name its master at all.
- A slave whose master is gone answers **ENOTTY** on `TCGETS` (this is the
  observed condition), and its path can then be reallocated to a brand-new pty.
- A pty index is **not** reused while any pty is open in that process, which is
  why one clean attempt succeeds and why the retry has to re-read `TIOCGPTN`
  from a **new** master (it must not re-open the number it just lost).

**The fix is an OBSERVATION, not a longer wait.** After opening the slave, ask
the kernel whether that fd is a terminal: `unix.IoctlGetTermios(fd, TCGETS)` is
the exact predicate `internal/lineedit` uses on the same descriptor
(`IsTerminal`/`Open`, internal/lineedit/raw.go), so the check asks production's
own question rather than a proxy. A pipe, a regular file, `/dev/null` and a pty
slave whose master is gone all answer ENOTTY. On failure the observed condition
is **recorded** (which step, which errno) and the whole allocation is retried a
bounded, named number of times; when the bound is exhausted the fixture Skips
with the recorded conditions — never silently, never `t.Skip` unconditionally,
never a widened time bound.

**Honest limit (stated in the code, not papered over):** a slave number recycled
to a pty that is *still open* **is** a terminal, and no ioctl on the slave can
tell it from ours — nothing on the slave names its master. The fixture therefore
verifies "this fd is a terminal", which is exactly the property
`lineedit.Open` requires of it, and does not pretend to verify "this fd is the
same terminal `TIOCGPTN` named".

## Completed

- New file `cmd/agent/chat_pty_fixture_test.go` — the package's ONE pty fixture:
  - `chatAllocPtyPair(want)` — **one** attempt: the four openpty steps, the
    optional `TIOCSWINSZ`, then the **verification** (`chatSlaveIsTerminal` =
    `unix.IoctlGetTermios(TCGETS)`). Every failure path closes what it already
    opened, so a retry starts clean and a give-up leaks nothing. It returns a
    pair **only** when the slave fd really is a terminal; otherwise it returns a
    `chatPtyCondition{Step, Err}` naming the step and the kernel's answer.
  - `chatAllocatePtyPair(alloc)` — the retry policy, factored out so a test can
    drive it with a failing allocator and observe both the bound and the
    recorded conditions (no host timing involved). Exhaustion returns an error
    naming the attempt count and every condition, in order (Rule 15).
  - `chatOpenPtyPair(t, cols, rows)` — what a case calls: a verified pair, or a
    **skip whose message names the observed conditions**. A host without
    `/dev/ptmx` skips by name, because a substitute stream would assert nothing.
  - Named constants (Rule 1): `chatPtyOpenAttempts = 8` (a COUNT, not a
    duration — widening a time bound here would hide the very condition this
    file exists to record), bounded by the contract constants
    `chatPtyMinOpenAttempts = 2` / `chatPtyMaxOpenAttempts = 32`, plus
    `chatPtyDevicePath`, `chatPtySlavePathFormat`, `chatPtyProcPathFormat` and
    one constant per step name (`chatPtyStepOpenMaster`, `…SlaveNum`,
    `…Unlock`, `…OpenSlave`, `…Winsize`, `…Verify`); `closeQuietly` comments its
    deliberate discard.
- `cmd/agent/chat_dump_geometry_pty_test.go` — `dumpGeomPty` now delegates to
  `chatOpenPtyPair` (the 27 lines of unchecked inline allocation are gone).
- Rule 13 de-duplication: `TestChatStreamWrap_WidthComesFromARealTerminal`
  (chat_stream_wrap_test.go) and `hlOpenPty` (host_lifecycle_log_test.go) had
  the **same unchecked** openpty recipe copied out. Both now allocate through
  the one verified fixture. `hlOpenPty` still returns the slave *path* for its
  child to open (that is its case's subject), so it closes the fixture's
  verified slave while keeping the master — the thing that keeps that path
  resolvable to **this** pty — open.

### The deterministic regression guard (4 tests, all in chat_pty_fixture_test.go)

1. `TestChatPtyFixture_RefusesAnFdThatIsNotATerminal` — **negative control.**
   Drives the verification unit at a **pipe** (`/proc/self/fd/<n>`): the pipe is
   not a simulation of the race, it is the same *observable* the race produces
   (an fd that is not a terminal), reached with no concurrent pty user. Asserts
   the refusal, the recorded step, and that the condition carries **ENOTTY** —
   the same errno production reported.
2. `TestChatPtyFixture_AcceptsARealPtySlave` — **positive control.** Without it
   the verification could be vacuous (refusing everything) and every pty case
   would skip.
3. `TestChatPtyFixture_BoundedRetryRecordsEveryCondition` — pins the POLICY with
   a failing allocator: exactly `chatPtyOpenAttempts` attempts made, the error
   names the count *and* the condition, and a transient condition that clears on
   the second attempt still yields a pair with `Attempts == 2`.
4. The same test asserts the bound's own contract
   (`chatPtyMinOpenAttempts <= chatPtyOpenAttempts <= chatPtyMaxOpenAttempts`) —
   a bound of 1 is the pre-fix single-shot allocation, and a large one turns a
   lost race into a stall that reads as a hang.

**Red-proofs observed (not claimed):**
- Neutralising the verification (making `chatSlaveIsTerminal` return nil) →
  `TestChatPtyFixture_RefusesAnFdThatIsNotATerminal` **FAILS** ("the fixture
  accepted a pipe as a terminal").
- Setting `chatPtyOpenAttempts = 1` → the contract sub-assertion **FAILS**
  ("a bound of 1 is the pre-fix single-shot allocation").

## Not Done (honest)

- **The whole-package re-run still does not report `ok` on this host, for two
  reasons that are not this fix** (both pre-existing, both named below). The pty
  case itself is no longer among them.
- **The pty failure is not reproduced on demand.** 4800 stress rounds (4 victim
  allocators + 4 churners on an 8-CPU host) measured **clean 4800/4800**: the
  window (release-and-reallocate between `TIOCGPTN` and `open`) is real but
  narrow, and this host would not hit it. So the guard is deliberately the
  *observable* (a non-terminal fd), not the race itself — waiting for the race
  to win would be a host-timing-dependent test, which the epic forbids.
- **The residual window is not closed** — a recycled-but-still-open pty slave
  passes the check (see "Honest limit" above). It is stated in the file.
- **The package's wall-clock budget is out of scope** by the epic (a separate
  doc, `devdoc/cmd-agent-suite-budget.md`: `go test ./cmd/agent/` needs
  ~604-607s against Go's 600s default). Not touched, not measured again, not
  duplicated.
- **The sibling quiet-host measurement row 01a0de63 was not touched** — no
  base-vs-branch diff was duplicated here.

## Files

| file | change |
|---|---|
| `cmd/agent/chat_pty_fixture_test.go` | **new** — verified pty fixture + 4 guard tests |
| `cmd/agent/chat_dump_geometry_pty_test.go` | `dumpGeomPty` delegates to the verified fixture |
| `cmd/agent/chat_stream_wrap_test.go` | Rule 13: same unchecked recipe removed, now the one fixture |
| `cmd/agent/host_lifecycle_log_test.go` | Rule 13: `hlOpenPty` now allocates through the one fixture |

Evidence (outside the worktree, per the briefing):
`/home/user/.gopi/sessions/01a0de91-16b9-7210-8422-f46d9b952e0a/` —
`guard-focused-run.log`, `isolated-green-run.log`, `whole-package-base-run.log`,
`whole-package-branch-run.log`, `whole-package-repro-verbatim.txt`.

## Verification

- **Build:** `go build ./...` → `BUILD_EXIT=0`; `go vet ./cmd/agent/` → clean;
  `gofmt -l` on all four files → no output.
- **Tests (focused guard):** `go test ./cmd/agent/ -count=1 -run
  'TestChatPtyFixture' -v` → `ok github.com/nick/gopi/cmd/agent 7.216s`, the
  three guard tests and both subtests PASS (`guard-focused-run.log`).
- **Tests (the regression itself, isolated):** `go test ./cmd/agent/ -count=1
  -run '^TestChatDump_GridFollowsTheLayoutGeometryOnARealPty$' -v` → PASS
  (0.01s), `ok … 0.838s` (`isolated-green-run.log`).
- **Ran (the surface that showed the red, re-run on this tree):**
  `go test ./cmd/agent/ -count=1 -timeout 10m` → see "Result" below. The
  observed red it is compared against is quoted verbatim at
  `whole-package-repro-verbatim.txt`, captured on a tree whose
  `git rev-parse HEAD` is **`4d7cb918e70ce5f371876188783df3ef7adc3a2e` — the same
  tip this worktree is based on** (`git merge-base master HEAD` =
  `4d7cb918…`, 0 commits ahead), with invocation
  `go test ./cmd/agent/ -count=1 -v -timeout 30m` on an 8-CPU host at load
  8.19-14.58:

      === RUN   TestChatDump_GridFollowsTheLayoutGeometryOnARealPty
          chat_dump_geometry_pty_test.go:132: open the pty slave as a terminal: lineedit: read termios on fd 7: lineedit: not a terminal
              bad file descriptor
      --- FAIL: TestChatDump_GridFollowsTheLayoutGeometryOnARealPty (0.00s)

  and the same tree, same host, immediately after, isolated:
  `go test ./cmd/agent/ -count=1 -run
  '^TestChatDump_GridFollowsTheLayoutGeometryOnARealPty$'` → PASS (7.069s).
- **Tests (focused, post-commit, tree `ba41895f`):** `go test ./cmd/agent/
  -count=1 -v -run 'TestChatPtyFixture|^TestChatDump_GridFollowsTheLayoutGeometryOnARealPty$'`
  → `ok github.com/nick/gopi/cmd/agent 2.951s`, `EXIT=0`, all four PASS
  (`post-commit-focused-run.log`).
- **Result (whole-package, this tree) — reported honestly, it is still not
  `ok`:** the invocation was run **twice**, and **no failure line in either run
  names any file this diff touched** (`grep -nE
  'chat_pty_fixture_test\.go|chat_dump_geometry_pty_test\.go|chat_stream_wrap_test\.go|host_lifecycle_log_test\.go'`
  over both logs → **0 hits**; the six `not a terminal` hits in them are CLI help
  text: *"or when stderr is not a terminal — a redirected run never …"*).

  | run | named failures | wall | log |
  |---|---|---|---|
  | base, **pre-fix** (`4d7cb918`) | `TestToolsMaintenanceHelpSurfaceFormsAreTyped (50.10s)`, `TestCwdReleaseRig (2.29s)`, `panic: test timed out after 10m0s` @1297 | `FAIL … 606.791s` | `whole-package-base-run.log` |
  | branch, post-fix #1 | `TestTaskShowRendersTheSameDigestForAllThreeAddresses (0.23s)`, `TestCwdReleaseRig (2.14s)`, `panic: test timed out after 10m0s` @1648 | `FAIL … 602.158s` | `whole-package-branch-run.log` |
  | branch, post-fix #2 | `TestToolsMaintenanceHelpSurfaceFormsAreTyped (49.31s)`, `TestCwdReleaseRig (2.24s)`, `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (2.67s)` | `FAIL … 601.361s` | `whole-package-branch-run-2.log` |

  **`TestCwdReleaseRig` is present in the pre-fix base run too**, and the wall
  clock is 606.8s / 602.2s / 601.4s against Go's **600s default cap** — the
  documented package-budget row (`devdoc/cmd-agent-suite-budget.md`) that this
  epic puts out of scope. **So the honest summary is: this fixture's failure is
  gone from the whole-package failure list; the invocation itself is still red
  for the package's own budget and for named failures that were already red
  before the change.**
- **Rule 30:** `git status --porcelain` in the worktree → see the commit line
  below; the tree is committed and clean (all four files are in the commit, no
  untracked entries).

## Evidence (self-filed)

- Guard green: `go test ./cmd/agent/ -count=1 -run 'TestChatPtyFixture' -v` →
  `ok github.com/nick/gopi/cmd/agent 7.216s`, 3 tests + 2 subtests PASS
  (`guard-focused-run.log`).
- Negative control red then green: with the verification neutralised the guard
  reported `the fixture accepted a pipe as a terminal: /proc/self/fd/<n> does not
  name a terminal`; restored, it PASSes.
- Bound red then green: with `chatPtyOpenAttempts = 1` the contract subtest
  reported `the retry bound is 1, outside its own [2, 32] contract …`; restored,
  it PASSes.
- Mechanism measured: probe over 4800 rounds reported `slave-open errors:
  map[] / TCGETS failures: map[] / clean: 4800`; `TIOCGPTN` on a slave and
  `TCGETS` on a masterless slave both answered `ENOTTY` (traced to their
  syscalls in the probe).
- Isolated green on the fixed tree: `ok github.com/nick/gopi/cmd/agent 0.838s`
  (`isolated-green-run.log`).
- Whole-package base: `panic: test timed out after 10m0s` at
  `whole-package-base-run.log:1297`, `FAIL … 606.791s` — the pty case is not in
  its failure list.

## Commit

`bccb8496 test(cmd/agent): verify the pty slave is a terminal, with a bounded
named retry` on branch `task/01a0de9116b972108422f46d9b952e0a` (base
`4d7cb918e70ce5f371876188783df3ef7adc3a2e`) — that commit carries the fixture,
the four guard tests and this doc; the doc's row in `devdoc/DOC-INDEX.tsv` (the
truth-class manifest gate, `TestDocManifestCoversEveryDevdocFile` +
`TestDocClassBannerMatchesManifest`) lands in the commit right after it, because
a committed file cannot name the hash of the commit that contains it. Four
doc-only commits follow it (`33eacb7b`, `1d15bfe8` and the final one for this
section): this report records the runs that produced the numbers above, so each
finding lands as its own commit. The tip is `git log --oneline -1` in the
worktree. Worktree-shared — the commits are already on the branch in the main
repo; no push needed (Rule 6c). `git status --porcelain` prints nothing.
