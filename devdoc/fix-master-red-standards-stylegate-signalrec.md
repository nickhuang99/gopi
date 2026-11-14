<!-- doc-class: historical -->
# Task Report: fix master-own red — standards Rule 16, stylegate ceilings, signalrec e2e

**Branch**: `feat/fix-master-red-standards-stylegate-signalrec` (worktree
`/home/user/workspace/gopi-master-red`), commit **a9e0775f** on top of master 13acf700.
**Base of measurement**: pristine master 45f213d9 (an ancestor of 13acf700), worktree
`/dev/shm/master-baseline-wt`.

## Completed

Three independent classes of master-own red, each fixed surgically. No ceiling was widened,
no blanket ignore was added, no baseline entry was relaxed or deleted.

### 1. internal/standards Rule 16 — re-keyed `cmd/agent/main.go:955` → `:961`

Master was red **both ways at once** on pristine 45f213d9:

```
gate_test.go:110: Rule 16 (%w error wrapping): NEW violation (checklist 2.2) at cmd/agent/main.go:961
gate_test.go:110: Rule 16 (%w error wrapping): STALE baseline entry cmd/agent/main.go:955 matches no violation
```

The `:961` hit is `readPayloadFile` (added by 0bea9c0d), whose `fmt.Errorf` **does** wrap with
`%w`; its two `%v` verbs format the `payloadFilePrefix` **STRING constant** — the literal
`%v` / `\%v` text deliberately describes how to escape a payload, nothing is being wrapped, so
`%w` is impossible there. The site was re-keyed (with a dated reason in the comment block), not
fixed and not removed: an entry that matches no violation is itself an error, so neither
deleting nor relaxing it was available. The shift came from master's own `signalrec.Install`
wiring (2fd022ca) inserting lines above the site.

### 2. internal/stylegate ceilings — 4 NEW un-annotated sites, ceilings untouched

Measured with the tree's own scanner (`internal/stylegate/cmd/stylegate`), the count moved
182 → 186 after the ratchet commit 0dde9423 (`ignored-error` test ceiling 182):

| site | rule | disposition |
|---|---|---|
| `internal/testutil/tmproot_assert_test.go:446` | test-sleep (ceiling 0) | inline `//stylegate:allow-sleep` — the sleep IS the fixture: the parent reads `/proc/<pid>/environ` only while this child is still alive |
| `internal/testutil/tmproot_assert_test.go:152,178,357` | test-setenv (ceiling 0) | inline `//stylegate:ignore` — the override must live in the process env for a child at exec time and for production `os.Getenv`; `t.Setenv`'s undo would defeat the case |
| `cmd/agent/tmpdir_inject.go:141` | ignored-error (production, ceiling 0) | inline `//stylegate:ignore` — the `/`-diagnostic report is deliberately non-fatal; the refusal belongs to the caller (`scratch.AssertTempRootOffRootFs`) |

`internal/stylegate/gate_test.go` is **byte-identical to master** (`git diff --exit-code`):
no ceiling value changed.

### 3. internal/signalrec — the killed child's missing record was a start-up RACE

Both process-level red proofs failed on pristine master:

```
--- FAIL: TestSignalRecForwardingPreservesDefaultDeath
    signalrec_test.go:686: child died but left no record — forwarding must not outrun the evidence write
```

Root cause (5-why): the parent signalled a **fixed `time.Sleep(300ms)`** after `Start()`
→ "the victim is INSIDE its handler" was an *assumption* about exec+`Init`+`Install`
latency → on a loaded host the signal landed **before** `signal.Notify` installed the handler
→ the kernel's default disposition killed the child (correct kernel behaviour, no recorder,
no record) → the parent then asserted a record must exist. Reproduced deterministically with
`-count=N` (later iterations) and with a single run under 6–8-way CPU load.

Fix: a **readiness handshake**. The victim child prints a token
(`signalrec-test-child: recorder installed`) on stderr immediately after `Install()` returns,
and the parent polls for it (bounded by `srReadyTimeout`, cadence `srReadyPollInterval`) before
signalling — the ordering is now a fact the child states, not a timing assumption. The child
reports through a file (stderr is inherited, so `cmd.Wait` is unaffected — no pipe+Wait race),
and the match is an **exact trimmed line** so a signal delivered before readiness cannot be
mistaken for readiness via the panic trace it prints. Three fixed sleeps (the two red tests +
`TestSignalRecOffIsHonored`, which was racing for the same reason) are gone; zero `allow-sleep`
markers are needed in this file for them.

**Rejected first, on evidence**: a `/proc/<pid>/status` `SigCgt` readiness probe. A minimal
sample (`/home/user/scratch/tmp-masterred/sigprobe`) showed Go's runtime sets `SigCgt` for
SIGTERM **at exec**, before `signal.Notify` — the bit cannot distinguish "runtime caught it"
from "our handler installed", so it would have been a false oracle.

## Not Done

- **No behavioural change to the Rule 16 site itself.** It is a permit re-key: the `%v` verbs
  are correct and `%w` is impossible there. Changing the message to satisfy the gate's shape
  would have been the wrong fix.
- **The `ignored-error` test ceiling was NOT ratcheted down** (stays 182 while reality is 185,
  i.e. 3 spare permits). Tightening it is a separate, repository-wide audit (the 185 includes
  sites whose reasons are per-site prose); doing it here would have widened scope beyond the
  three red classes. Recorded honestly rather than silently.
- **Pre-existing reds NOT touched** (out of scope, all present on pristine 45f213d9):
  `cmd/agent` e2e/help-width failures (`TestEveryHelpConstantFitsTheWrapBudget`,
  `TestParseTasksSweepZombiesArgs`, the worker/task-run cwd tests), `internal/testutil`
  `TestWorkerOwnershipPointerPresent`, and the brief `internal/signalrec` `/dev/shm`-TMPDIR
  variant — see the acceptance-run note below.

## Verification

Environment scrub used for **every** run (the repro requires it):

```bash
env -u GOPI_DAEMON_READY_FD -u GOPI_AGENT_SESSION -u GOPI_EXECUTOR_SESSION_ID \
    -u GOPI_WORKER_DAEMON_CHILD TMPDIR=/home/user/scratch/tmp-masterred ...
```

`TMPDIR` is on `/dev/sda4` (device 2052), `/` is device 2051, `/dev/shm` is device 28 and 94%
full — Rule 22 satisfied, no run used `/dev/shm` as a work root.

### Before (pristine master 45f213d9)

```
$ go test ./internal/standards/ -count=1     → FAIL (Rule 16 NEW at :961 + STALE at :955)
$ go test ./internal/stylegate/ -count=1     → FAIL (test-sleep 1/0, test-setenv 3/0,
                                               ignored-error 1/0 production, 185/182 test)
$ go test ./internal/signalrec/ -count=1     → FAIL TestSignalRecForwardingPreservesDefaultDeath
```

`internal/signalrec` is order/timing dependent: the failure also reproduces as
`TestSignalRecChildVictimRecordsEndToEnd` (`-count=10`, and the full-suite run recorded in
`/dev/shm/gopi-accept-3478385/final-master-suite.txt:438-440`). One unlucky-free single run can
pass — which is why the gate for this fix is the load/repeat run below, not a single pass.

### After (branch a9e0775f)

```
$ go test ./internal/standards/ -count=1   → ok  github.com/nick/gopi/internal/standards   4.009s
$ go test ./internal/stylegate/ -count=1   → ok  github.com/nick/gopi/internal/stylegate   2.236s
$ go test ./internal/signalrec/ -count=1   → ok  github.com/nick/gopi/internal/signalrec   1.613s
```

Gate data report (Rule 16 must be NEW=0 / 0 stale):

```
$ go test -run TestStandardGateDataReport -v ./internal/standards/
Rule 16 (%w error wrapping) (checklist 2.2): hits=8 baselined=8 escaped=0 NEW=0
Rule 17 (test env isolation):  hits=111 baselined=94 escaped=17 NEW=0
Rule 18 / 19 / 20 / 21 / 13:   NEW=0
```

Independent scanner counts before → after (`cmd/stylegate -root . -exclude internal/stylegate`):
`ignored-error 186 → 182`, `test-setenv 3 → 0`, `test-sleep 1 → 0`, `errorf-verb` unchanged.

Minimal-sample repros (Rule 10.5), 6 spinners on a many-core host:

```
# the repro, ONE test, -count=1, under load
pristine 45f213d9 → signalrec_test.go:686: child died but left no record … FAIL
branch  a9e0775f → ok  github.com/nick/gopi/internal/signalrec  1.882s

# the fix holds where it used to fail: 3 tests × 10 repeats under load
branch a9e0775f → ok  github.com/nick/gopi/internal/signalrec  51.996s
```

Gate fixes (1 and 2) are drift/annotation classes with no runtime behaviour to reproduce
minimally; their smallest sample is the gate's own red proof of the SINGLE site it names —
`internal/standards` `TestGate1ErrorWrapRedProof` (`cmd/agent/main.go:961`) and the stylegate
rule line + one named site (`tmproot_assert_test.go:446`). Both are cited verbatim above, and
the data report proves each is now baselined/escaped rather than ignored.

Regressions checked: `go build ./...` OK; `go test ./internal/testutil/ -run 'TempRoot'`
(4 touched/related sites) ok.

**Note on a brief red variant**: with `TMPDIR=/dev/shm/...` (a 94%-full tmpfs) the pristine
tree reports ~28 bogus "build failed" packages. That is the documented /dev/shm artefact, not
this branch; every acceptance run above used an off-`/dev/shm` TMPDIR.

## Rule 6d — deliverable declaration

1. **Branch**: `feat/fix-master-red-standards-stylegate-signalrec`, head **a9e0775f**
   (`a9e0775fb16781be164b44baa1692b1da8ad4554`).
2. **Recovery command**:
   `git show feat/fix-master-red-standards-stylegate-signalrec:devdoc/fix-master-red-standards-stylegate-signalrec.md`
   (full diff: `git diff master...feat/fix-master-red-standards-stylegate-signalrec`).
3. **In the main repo**: yes — the branch lives in a linked worktree of `/home/user/work/GoPi`,
   so the commit is already in the main repo's refs; no push needed
   (`git -C /home/user/work/GoPi log --oneline -1 feat/…` → a9e0775f).

Files changed (4): `internal/standards/baseline.go`, `internal/stylegate/…` (untouched — proof
of no widening), `internal/testutil/tmproot_assert_test.go`, `cmd/agent/tmpdir_inject.go`,
`internal/signalrec/signalrec_test.go`.

---

## Independent re-verification (2026-09-14, second session, same worktree)

The three classes above were **re-measured from scratch** in this worktree without trusting the
earlier transcript: pristine master `45f213d9` (`/dev/shm/master-baseline-wt`) vs branch head
`5400f493`, both under the documented env scrub with `TMPDIR` on `/dev/sda4` (device 2052;
`/` = 2051, `/dev/shm` = 28 at 94% — Rule 22 satisfied).

### Pristine 45f213d9 (8-way spinner load, one run)

```
--- standards ---  FAIL
gate_test.go:110: Rule 16: NEW violation at cmd/agent/main.go:961
gate_test.go:110: Rule 16: STALE baseline entry cmd/agent/main.go:955 matches no violation
gate_test.go:123: Rule 16: hits=8 baselined=7 escaped=0 NEW=1
--- stylegate ---  FAIL (TestStyleGate, gate_test.go:126)
test-sleep:      1 test hit(s),  ceiling 0   [internal/testutil/tmproot_assert_test.go:446]
test-setenv:     3 test hit(s),  ceiling 0   [tmproot_assert_test.go:152, :178, :357]
ignored-error:   1 production hit(s), ceiling 0
ignored-error: 185 test hit(s), ceiling 182
--- signalrec -count=10 under load ---  FAIL
signalrec_test.go:558: a killed gopi-shaped child left NO provenance record — the incident is not traceable
signalrec_test.go:686: child died but left no record — forwarding must not outrun the evidence write
(4 of the 10 iterations failed BOTH tests)
```

The race is timing-dependent by construction: the same two tests **pass** on pristine master in
an unloaded single run (`job-1`, 0.32s/0.56s). That is the defect, not a measurement problem —
the parent signalled a fixed 300 ms after `Start()` and lost the race under load.

### Branch 5400f493 (same load, same scrub)

```
go test ./internal/standards/ -count=1   → ok  (4.08s)
go test ./internal/stylegate/ -count=1   → ok  (2.19s)   [TestStyleGate + TestGateCeilingsAreReachable PASS]
go test ./internal/signalrec/ -count=1   → ok  (1.59s)
go test ./internal/signalrec/ -count=10 under 8-way load → ok (66.66s, 0 failures)
go test ./internal/standards/ -run TestStandardGateDataReport -v:
  Rule 16 hits=8 baselined=8 escaped=0 NEW=0     ← 0 stale (pristine was NEW=1 + 1 stale)
  Rules 13/17/18/19/20/21: NEW=0
go build ./... → OK          gofmt -l (touched pkgs) → empty
```

Independent scanner on the branch (`go run ./internal/stylegate/cmd/stylegate -root . -exclude internal/stylegate`):
`ignored-error 182` (== ceiling, no spare permit taken), `errorf-verb 8` (unchanged).

### No-widening proof (the ratchet constraint)

```
$ git diff master...HEAD --stat -- internal/stylegate/     → EMPTY
$ git diff master...HEAD -- internal/stylegate/gate_test.go → EMPTY
```
`internal/stylegate/gate_test.go` is byte-identical to master: every ceiling is still the value
master shipped (`RuleTestSleep {true:0}`, `RuleTestSetenv {true:0}`, `RuleIgnoredError {false:0,
true:182}`). The 5 sites were closed by per-site reason markers, not by headroom. The one
out-of-class extra (`cmd/agent/tmpdir_inject.go:141`, an `ignored-error` **production** site,
ceiling 0) was annotated in the same pass — leaving it would have kept the class red.

### Minimal-sample repros (Rule 10.5)

- **Rule 16**: the gate's own single-site red proof — one named site (`cmd/agent/main.go:961`)
  reported as NEW and one stale entry (`:955`) on pristine, both resolved on the branch; the
  data report shows it baselined, not ignored.
- **stylegate**: the scanner names exactly the 5 sites on pristine and 0 on the branch (the
  smallest sample is the single rule line + the 5 named file:line sites, quoted above).
- **signalrec**: the smallest failing sample is one test, `-count=1`, under 8-way load —
  pristine FAILS (`signalrec_test.go:686`), branch PASSES; `-count=10` is the repeat arm that
  shows the branch holding where pristine failed 4/10 iterations.

### Rejected first, on a minimal sample

A `/proc/<pid>/status` `SigCgt` readiness probe (the obvious "is the handler installed?" oracle)
was measured and **rejected**: Go's runtime sets the SIGTERM bit in `SigCgt` at exec, before
`signal.Notify`, so the bit cannot distinguish "runtime caught it" from "our handler installed".
The readiness line the child itself prints after `Install()` returns is the only oracle that
states the fact.
