<!-- doc-class: historical -->
# Task Report: the ~4 GiB bash-output allocation that aborts `internal/tools` at the PACKAGE level

Task: `01a1315950dd7c51b5a546b78494cf88` · host `hosta` · branch
`task/01a1315950dd7c51b5a546b78494cf88` (cut at `4c3c9d0`)

**STATUS: the fix, the regression guard, the red proof and the report are done.
§Verification's `Ran:`/`Result:` lines carry the verbatim invocations and the
observed output, including the branch-scoped `make test-budget PKGS='./internal/tools'`
run whose result is recorded in **§Branch-scoped suite** below — read that section
before treating the package as green.**

## Completed

### 1. The defect reproduced, with the allocating frame named

The abort is a **runtime-level `fatal error`**, not a test failure: an unbounded
`bytes.Buffer` in `bashInDirCtx`'s stdout read goroutine grew with everything the
command emitted, so a command that produces output faster than the 2 s absolute
budget could stop it drove the buffer into a multi-GiB `growslice`, and the
runtime aborted the **whole test binary**. A binary-level abort carries no
failing test with it, which is exactly why the budget harness can only render
the contradiction `failing tests: 0 / PACKAGE FAILED` — the package's verdict
died with its evidence.

**The allocating frame**, from the captured dump (evidence file
`repro-abort-6g.log`, lines 9–27) — the frame immediately below the runtime is
the repo's own code:

```
runtime: out of memory: cannot allocate 2147483648-byte block (2171731968 in use)
fatal error: out of memory
runtime.(*mcache).allocLarge(0x300000002?, 0x80000000, 0x1)
runtime.mallocgcLarge → runtime.mallocgc → runtime.growslice
bytes.growSlice({0xc041792000, 0x40000000, …})   /usr/local/go/src/bytes/buffer.go:249
bytes.(*Buffer).Write(0xc00076a630, {0xc0000a4000, 0x1000, 0x1000?})
github.com/nick/gopi/internal/tools.bashInDirCtx.func2()
        internal/tools/bash.go:602            ← THE ALLOCATION SITE (stdoutBuf.Write)
created by …bashInDirCtx in goroutine 7
        internal/tools/bash.go:597
```

### 2. The named allocation site and the root cause

- **Site:** `internal/tools/bash.go:602` — `stdoutBuf.Write(buf[:n])` inside the
  stdout read goroutine (`stderrBuf.Write` at `:618` is the same defect on the
  other stream). Both buffers were `var stdoutBuf, stderrBuf bytes.Buffer`
  (`:593`).
- **The constant that was supposed to bound it, and did not:**
  `pipeReadBufferSize = 4096` (`internal/tools/limits_rule1.go`, its own comment:
  *"keeps memory bounded per concurrent command"*). It bounds the **chunk**, not
  the **buffer** — every chunk was appended to a buffer with no cap at all. The
  amount of memory a bash call held was therefore not a policy but whatever the
  command happened to print.
- **Root cause statement:** *an unbounded accumulator behind a bounded chunk
  size.* The read loop is `Read(4 KiB) → Write(4 KiB) → repeat`, and nothing in
  that loop knows `maxOutputSize` exists — the output cap was enforced only in
  `buildOutput`, which runs **after** the command has finished and both buffers
  are already full. So the cap never constrained memory, only the rendered
  result.
- **Boundary, stated honestly (Rule 41.3 / MBGC, kept as a note):** the epic's
  root-cause statement says "~4 GiB single allocation". **That number was
  reproduced on this host as a 2 GiB block request** (twice: `repro-abort-6g.log`,
  and the red proof `guard-red.log`) because `growslice` doubles, so the fatal
  request is the first doubling that cannot be satisfied — 4 GiB is what the
  *next* size would have been and what a slightly different heap state reports.
  The mechanism is identical; the exact byte figure is heap-state dependent.

### 3. The fix: a bounded tail writer, sized by the constant that already bounds the result

New file `internal/tools/bounded_output.go` — `boundedTailWriter`, an
`io.Writer` that retains only the last `limit` bytes, shifting the window in
place with `copy()` so `len+cap` never leave the bound. `Truncated()` exposes
whether anything was dropped, so the loss is observable rather than silent
(Rule 15).

`bashInDirCtx` now accumulates into two of those instead of two `bytes.Buffer`s
(`bash.go:593–599`).

**Why the window is the TAIL, and why its size is derived rather than chosen.**
`buildOutput` throws away every byte beyond `maxOutputSize` and **keeps the
tail** — CLI errors (build failures, test failures, git conflicts) land at the
END of a command's output. So:

- retaining the tail is retaining exactly the half the renderer keeps;
- sizing the window by the **same** constant that bounds the result
  (`pipeRetainedTailSize = maxOutputSize + pipeRetainedTailSlackSize`) makes the
  pair impossible to drift — a retune of `maxOutputSize` moves both.

The 4 KiB slack is not decoration: `buildOutput` truncates on a **strict**
`len(body) > maxOutputSize`, so a window of exactly `maxOutputSize` would stop
the `[output truncated …]` marker from firing and the model would read a 100 KB
tail believing it was the whole output. With the slack the rendered result is
**byte-identical** to what the unbounded buffer produced, in every case
(`TestBashOutputTruncation` still passes, including its
`len(output) > maxOutputSize+200` assertion).

Best case 2 × ~106 KiB per bash call, down from "unbounded".

### 4. A regression guard that fails without the fix

`internal/tools/bash_bounded_output_test.go`:

- `TestBoundedTailWriterKeepsTheTailAndTheBound` — the writer's own contract on
  the smallest sample (Rule 10.4): the bound holds for chunked writes, the
  single-oversized-chunk path, and a `limit <= 0` (which must never yield a
  writer that silently returns nothing).
- `TestBashUnboundedOutputIsBoundedNotFatal` — drives the real surface (`bash`,
  `yes`, 2 s budget, the same shape the suite already runs) and asserts the
  **memory behaviour** those existing tests never asserted: bytes allocated
  during the call stay under a named ceiling. This is the test that goes red
  without the fix (red proof below).

## Not Done

- **No fix in `scripts/test-budget.sh`'s rendering, and none is proposed here.**
  Rule 28 puts `scripts/` on peerbox: this host may only FILE the row, not change
  that layer. See §Deliverable 5 — the rendering is arguably **correct as it
  stands**; it faithfully reports "0 failing tests, package verdict failed",
  and the thing that was wrong was the package aborting. Fixing the abort
  removes the occasion. No bug row was filed, with the reasoning below.
- **The pre-existing `TestBashOutputTruncation` / `yes` tests were left exactly
  as they are.** They are not modified: their assertions are still satisfied,
  and widening them would have made them carry the memory guard that now lives
  in the new file.
- **The ~8 GiB peak RSS of the *unfixed* arm is reported as MEASURED, not
  explained further.** Three consecutive unfixed runs of the single test
  measured 7.95 / 8.23 / 7.73 GB peak RSS and all exited 0 on this host's
  12 GiB `RLIMIT_AS` — the Go runtime's `sysAlloc` survives the address-space
  limit because it does not touch (commit) the mapped pages. That is an
  OBSERVATION about this host; the mechanism was not investigated on this
  budget, and it is why the deterministic reproduction below needed a tighter
  limit.

## Verification

- **Build:** `go build ./...` → `BUILD_OK` (no output). Also
  `go build ./internal/tools/` → clean.
- **Tests:** see §Branch-scoped suite.
- **Ran:** each line below is a verbatim invocation with what it actually
  printed.

### Reproduction (deliverable 1)

```
$ ulimit -v 12582912                       # this host's inherited hard limit, for reference
$ go test -count=1 -run TestBashIdleTimeout_InfiniteOutputCommand -v ./internal/tools/
--- PASS: TestBashIdleTimeout_InfiniteOutputCommand (3.19s)      # ← PASSES, but:
$ /usr/bin/time -v go test -count=1 -run TestBashIdleTimeout_InfiniteOutputCommand -v ./internal/tools/
	Maximum resident set size (kbytes): 7945492     # 8.23 / 7.95 / 7.73 GB over 3 runs
=== attempt 1 exit=0 ===  === attempt 2 exit=0 ===  === attempt 3 exit=0 ===
```

```
$ bash -c 'ulimit -v 6291456; ulimit -v; go test -count=1 -run TestBashIdleTimeout_InfiniteOutputCommand -v ./internal/tools/'
6291456
runtime: out of memory: cannot allocate 2147483648-byte block (2171731968 in use)
fatal error: out of memory
…
github.com/nick/gopi/internal/tools.bashInDirCtx.func2()
        /home/user/workspace/gopi-01a1315950dd7c51b5a546b78494cf88/internal/tools/bash.go:602 +0xb1
EXIT=1
```

The tighter `RLIMIT_AS` makes the heap state deterministic instead of
RSS-dependent; it changes only *when* the doubling fails, never the mechanism.
Evidence: `artifacts/repro-abort-6g.log` (635 lines, exit 1).

- **Result:** the abort reproduced on demand, exit 1, whole binary killed, the
  allocating frame named — `bash.go:602`.

### The fix, measured (deliverable 3)

```
$ /usr/bin/time -v go test -count=1 -run TestBashIdleTimeout_InfiniteOutputCommand -v ./internal/tools/
--- PASS: TestBashIdleTimeout_InfiniteOutputCommand (2.00s)
PASS
	Maximum resident set size (kbytes): 1091148      # 1.09 GB, was 8.23 GB
```

**Peak RSS of the previously-fatal arm: 8.23 GB → 1.09 GB** (~7.5x lower; the
`go test` driver itself accounts for most of the remainder).

### Red proof of the regression guard (Rule 5/10.5)

Production file mutated back to the unbounded form
(`stdoutBuf := &bytes.Buffer{}` + the `bytes` import restored), then:

```
$ bash -c 'ulimit -v 6291456; go test -count=1 -run TestBashUnboundedOutputIsBoundedNotFatal -v ./internal/tools/'
runtime: out of memory: cannot allocate 2147483648-byte block (2192670720 in use)
fatal error: out of memory
	/home/user/workspace/gopi-01a1315950dd7c51b5a546b78494cf88/internal/tools/bash.go:609
FAIL	github.com/nick/gopi/internal/tools	2.879s
FAIL
```

The guard's own failure mode IS the defect it exists for: the binary aborts
rather than reporting a test failure. Evidence: `artifacts/guard-red.log`.

With the fix restored, the same test:

```
$ go test -count=1 -run 'TestBoundedTailWriter|TestBashUnboundedOutputIsBoundedNotFatal' -v ./internal/tools/
--- PASS: TestBoundedTailWriterKeepsTheTailAndTheBound (0.00s)
--- PASS: TestBashUnboundedOutputIsBoundedNotFatal (2.00s)
PASS
ok  	github.com/nick/gopi/internal/tools	3.709s
```

**Restore of the mutated production file (a gate claim, measured with an
explicit revision, not index-relative — `devdoc/redproof-restore-criterion.md`):**
the fix was UNCOMMITTED, so `git checkout -- internal/tools/bash.go` would have
restored the *unmutated-but-unfixed* HEAD blob. The restore was therefore made
from the copy this run took before mutating, and verified by content:

```
$ grep -n "newBoundedTailWriter(pipeRetainedTailSize)" internal/tools/bash.go
598:	stdoutBuf := newBoundedTailWriter(pipeRetainedTailSize)
599:	stderrBuf := newBoundedTailWriter(pipeRetainedTailSize)
$ grep -n '&bytes.Buffer{}' internal/tools/bash.go      # the mutation
(no output — the mutation is gone)
$ go build ./...                                        # → BUILD_OK
```

Re-measured on the **committed** tree, with an explicit revision (the
index-relative form is not evidence — `devdoc/redproof-restore-criterion.md`):

```
$ test "$(git hash-object internal/tools/bash.go)" = "$(git rev-parse HEAD:internal/tools/bash.go)" \
      && echo "bash.go == HEAD blob (OK)"
bash.go == HEAD blob (OK)
$ git diff HEAD --stat -- internal/tools/bash.go
(no output)
$ go test -count=1 -run 'TestBoundedTailWriterKeepsTheTailAndTheBound|TestBashUnboundedOutputIsBoundedNotFatal' ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	3.682s
$ git status --porcelain
(no output)
```

### Branch-scoped suite

```
$ make test-budget PKGS='./internal/tools'
```

**Result: PASS — `EXIT=0`, `outcome=pass`.** The package-level abort is gone: the
suite runs to completion with no runtime abort, and the budget script renders the
package as green rather than `PACKAGE FAILED`.

Recorded verbatim in `artifacts/make-test-budget-tools.log`:

```
$ make test-budget PKGS='./internal/tools'
packages: 1 finished, 0 with a failure
stamp: 2026-10-13T02:43:31+08:00 host=hosta pkg=./internal/tools budget=25m elapsed=250s
       load_start=3.20 1.96 1.99 load_end=2.70 2.17 2.04 cores=32 exit=0 outcome=pass
       stream=run-1791830611-2540496.json
EXIT=0
$ grep -c "out of memory\|fatal error\|PACKAGE FAILED" artifacts/make-test-budget-tools.log
0
```

`packages: 1 finished, 0 with a failure` is the package-level verdict this task
existed to turn green, and the count of abort markers is **0** — the contradiction
`failing tests: 0 / PACKAGE FAILED` did not recur.

**Re-run on the final tip** (`41ef31493`, the report-only follow-up commit), so
the branch-scoped verdict is not carried by a tip that has since moved
(`artifacts/make-test-budget-tools-final.log`):

```
$ git rev-parse --short HEAD
41ef31493
$ make test-budget PKGS='./internal/tools'
packages: 1 finished, 0 with a failure
stamp: 2026-10-13T02:51:55+08:00 host=hosta pkg=./internal/tools budget=25m elapsed=249s
       load_start=5.44 3.40 2.53 load_end=3.24 3.18 2.62 cores=32 exit=0 outcome=pass
       stream=run-1791831115-2700285.json
EXIT=0
$ grep -c "out of memory\|fatal error\|PACKAGE FAILED" artifacts/make-test-budget-tools-final.log
0
```

## Evidence (self-filed)

All paths under this run's own session directory (Rule 37):
`/home/user/.gopi/sessions/01a13159-50dd-7c51-b5a5-46b78494cf88/artifacts/`

| claim | evidence file | what it holds |
|---|---|---|
| abort reproduced, allocating frame named | `repro-abort-6g.log` | 635 lines, exit 1, `bash.go:602` in the stack |
| unfixed peak RSS | `repro-run.log` | 7.95 / 8.23 / 7.73 GB, 3 runs, exit 0 each |
| fixed peak RSS | `fixed-infinite-output.log` | 1.09 GB, PASS |
| guard green with the fix | `guard-green.log` | both tests PASS |
| guard red without the fix | `guard-red.log` | `fatal error: out of memory`, FAIL |
| branch-scoped suite | `make-test-budget-tools.log` | `make test-budget PKGS='./internal/tools'` → `EXIT=0` |
| full-package baseline (pre-change) | `orig-parallel2.log` | `go test ./internal/tools/` → `EXIT=0`, 414 PASS at cut `4c3c9d0` |

## Deliverable 5 — the system-layer question (Rule 28)

**No bug row filed, and the reasoning is the honest half of this section.**

The epic named `scripts/test-budget.sh`'s `failing tests: 0 / PACKAGE FAILED`
rendering as a possible durable fix for the **system layer** (peerbox's, per Rule
28). Reading that code:

- `scripts/test-budget.sh:655` prints `failing tests: $n` from
  `gopi_stream_failure_count`, which counts **test**-kind events;
- `:710` prints `PACKAGE FAILED: $pkg (a package-level verdict — a panic or a
  TestMain exit; no test to name)` and then quotes the package's **own**
  package-level output (`gopi_stream_package_output_for`).

That output is `failing tests: 0` beside `PACKAGE FAILED` **because both are
true**: zero tests failed, and the package's verdict was a failure. The
harness's own gloss ("a panic or a TestMain exit") is accurate and complete.
**The contradiction was in the package's behaviour, not in the rendering** —
`internal/tools` really was aborting at the binary level. Fixing the abort (this
change) removes the occasion; there is no separate rendering defect to file.

What IS thin, and is stated rather than papered over: the `PACKAGE FAILED`
block claims to quote the package's own output, and for a **runtime abort** the
output the script can harvest may be empty — it falls back to *"(the stream
carries no package-level output for this package — the verdict is recorded, its
evidence is not)"*. Whether the runtime's stack dump reaches the stream as
`package_output` rows was **not measured in this run**; if it does not, the
harness is honest about it, which I judge to be correct behaviour and not a bug
worth a row. Stated as inference, not as a finding (Rule 41.3).

## Files

- `internal/tools/bounded_output.go` — new: `boundedTailWriter` (+ its doc).
- `internal/tools/bash.go` — the two accumulators are now bounded; the `bytes`
  import is gone.
- `internal/tools/limits_rule1.go` — `pipeRetainedTailSize` +
  `pipeRetainedTailSlackSize`, with the measured incident in the comment.
- `internal/tools/bash_bounded_output_test.go` — the regression guard.
