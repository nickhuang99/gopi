<!-- doc-class: normative -->
# The chat argv fixture's TEXT claim is decided on the transport, like every other arm

**Task**: 01a194d6-a729-767a-8989-744efe72a7e8 · **Surface**:
`cmd/agent/chat_argv_test.go` (`chatRunArgsTextClaimOf` and its callers)

This is the sixth repair of one family. The five before it taught the fixture's
*release*, *read*, *write-end* and *exit-status* arms to decide "did this cost the
case its claim?"; this one is the last path that still read a stream bare.

## The defect, as the misattribution it is

`TestChatCLIUnknownFlagIsRefusedAsAFlag` read its claim off the drained text:

```go
_, stderr, code := chatRunArgs(t, c.argv...)
if !strings.Contains(stderr, "unknown flag "+c.word) {
    t.Errorf("runChat(%v): the refusal does not name %q as a FLAG:\n%s", c.argv, c.word, stderr)
}
```

`chatRunArgs` is the SAMPLING face of `chatRunArgsWitnessed` — it DISCARDS the
fixture's evidence record. So when a path outside the fixture released the stderr
stream's descriptor, the read was empty and the case said *"the refusal does not
name \"--bogus-flag\" as a FLAG"* about a refusal that was never delivered, while
the same run's record already said `chatRunArgsReadRecordedLoss` (lost before its
first byte, sibling carrying the claim, non-fatal). One event, two verdicts.

## The classification (five answers, never two)

| record | claim present | claim absent |
|---|---|---|
| `readClean` | **Present** — holds | **Missing** — the case's own failure |
| `readLostData` | **Truncated** — FATAL (a prefix decides nothing) | same |
| `readRecordedLoss` | **Unobservable** — short-circuit, marked, reported | same |
| unobserved verdict / unknown stream name | **Unknown** — FATAL, named | same |

The two arms that keep this honest are `Missing` and `Truncated`: the repair cannot
become "a missing sentence is never reported", and it cannot become "any read error
is recorded". Both are pinned by negative controls in
`TestChatRunArgsTextClaimIsDecidedOnTheTransport` and by the same-bytes/no-loss
control in `TestChatRunArgsUnknownFlagClaimSurvivesAForeignRelease`.

**Rule 15.** A short-circuit prints one line carrying `[transport-lost]`
(`chatRunArgsTransportLostMarker`), the stream, the fd, the kernel's pipe identity,
the undecided claim and the captured bytes. Nothing is silent; one grep reads a
whole-package run.

**The absence half is asymmetric on purpose.** `chatRunArgsAssertNoTextClaim` — used
by the cases that assert "resume target" did NOT come back — treats an unobservable
stream as *not decided*, not as a hold: an absence measured over a stream nobody
read is a vacuous pass, which is the defect class this whole family exists to
remove.

## THE RELEASER, NAMED — a late raw close on a RECYCLED number

The suite-wide releaser was open since `6ecc0244`
(`chat-argv-fixture-release.md` §"Not covered"). It is now named, and the naming
came from an in-package reproduction of its MECHANISM rather than from a census of
the 2026-11-01 occurrence.

**The class.** A fixture takes a raw descriptor, HANDS ITS OWNERSHIP to a
production path (a function whose documented job is to close it), and then keeps
its own release pending:

```go
borrowed := bfdRawDup(t, w)   // this fixture opened the number, and w is closed next
_ = w.Close()
defer syscall.Close(borrowed) // and then releases the SAME number again at teardown
workerDaemonNoteReady()       // the production path closes it first
```

When nothing is opened between the two closes, the second one answers `EBADF` and
the site is harmless — which is why it has looked correct for as long as it has.
When anything in the same process opens a descriptor in that window, **the kernel
hands back the freed number**, and the deferred close now releases *a stranger's
live descriptor*. `syscall.Close` on an open number returns `nil`, so the site
cannot tell the two events apart: it has no observation that fires only on the
damage.

**Measured — the minimal sample** (`/dev/shm/gopi-dcprobe`, one number, one
recycle, two closes; no load, no goroutines):

```
step1 fixture borrowed fd=3 target=/dev/null
step2 production closed fd=3 alive=false
step3 unrelated fixture's pipe read end fd=3 target=pipe:[303667468]
step4 deferred close returned nil; stranger fd=3 alive=false target=(gone)
step5 stranger write=write |1: broken pipe read n=0 err=read |0: bad file descriptor
```

`step5` is the field signature this whole family has been chasing: a stream that
was open, then is not, with **no code path that meant to close it**.

**Measured — inside the real package** (probe source archived as
`evidence/zz_doubleclose_probe_test.go`; never committed):

```
STEP2 production closed borrowed fd 8
STEP3 stranger descriptor fd=8 (borrowed was 8)
STEP4 after the sibling's deferred close: stranger fd 8 alive=false
```

Both runs are in `evidence/doubleclose-reproduction.txt`.

### Which site, exactly

The population is every test file that both wraps/borrows raw numbers and
raw-closes one (`3 wrap / 4 raw-close cmd/agent/borrowed_fd_sites_test.go`,
`internal/borrowedfd/borrowedfd_test.go`, `internal/testutil/borrowed_fd_ready_test.go`).
Of those, the shape above — *ownership passed to production, raw release still
pending* — appears at **three** sites, all one loop body in
`cmd/agent/borrowed_fd_sites_test.go` (`:231`, `:274`, `:366`).

Which of them can collide with this fixture is decided by ONE question: **can
`TestChatCLI*` run while that site's borrowed number is in the window?**

| site | what is opened in the window | can it be the chat fixture? |
|---|---|---|
| `:366` `TestDaemonReadyFDSitesCloseTheirBorrowedWrapper` — `daemonNote` / `workerDaemonNoteReady` | nothing: the window is three statements, and neither the production close nor the fixture's teardown opens a descriptor | **no** (the site is still wrong in kind — a raw close on a number it handed over — but it cannot fire on its own) |
| `:274` `TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive` | **yes, in two steps**: the fixture's deferred raw close frees the number while the production handle lives on in `workerDaemonLockRef`; the number is then free for the rest of the binary, and the handle's LATER finalizer (`borrowedfd.SettleFinalizers` in a sibling guard, or the GC) closes it a second time | **yes — REPRODUCED** (probe D) |
| `:231` `TestWorkerDaemonInheritLockRefusalKeepsTheBorrowedNumberLive` | **yes, unboundedly**: the fixture's raw dup is released at teardown while the production handle outlives it — an fd whose close is "on the 32-bit finalizer queue, earliest in two minutes" (the site's own comment) — so the number is FREE at exactly the width of `TestChatCLI*` | **yes** |

Probe D is the one that corrected an earlier draft of this table: it pinned
`:274` as "harmless because both numbers are still open when the fixture ends",
and the measurement says otherwise — the deferred raw close frees the number at
teardown and the retained handle's finalizer then closes whatever holds it:

```
STEP3 stranger took fd=7 (borrowed was 7, reached=true)
STEP4 stranger fd 7 alive before settle=true after settle=false
VERDICT: REPRODUCED — the production handle's finalizer closed a number that now belongs to a stranger (fd 7)
```

So the answer to "who releases this fd outside the fixture" is: **nothing in
production, and nothing on purpose anywhere** — it is a fixture releasing a number
it had already handed over, and (at `:274`/`:231`) a retained production handle
releasing the recycled number again. The 2026-11-01 occurrence is consistent with
this and is not proof of it: its record said `resolvable=false` — *no other
descriptor held that pipe at the moment of the report* — which is exactly what a
completed late close leaves behind.

**The fix belongs to a new row, not to this one** (Rule 41): every one of the
three sites must either stop releasing a number it handed over, or keep an
observation that can tell "I released my own" from "I released a stranger's"
(`F_GETFD` on a number whose owner is still expected to hold it is not enough —
the check has to run *before* the close and name the recovery). Changing them here
would land a second, unrelated repair on this document's commit.

## What this repair does not do

It does not make the occurrence disappear; it makes its COST disappear. A case that
meets a lost transport now records it (stream, fd, pipe identity, undecided claim,
captured bytes) and does not red — while the same run prints one greppable
`[transport-lost]` line, so the next occurrence is found by
`grep -n "\[transport-lost\]"` instead of by a red sentence about a refusal that
was never delivered.

## Ruled out by measurement on this host

- **No production `Close` on a standard stream**: `grep -rn "os.Stdout.Close()\|os.Stderr.Close()\|syscall.Close(1)\|syscall.Close(2)" --include=*.go cmd/agent internal` → 0 hits.
- **No `t.Parallel()` in this fixture's cases**: the package's 20 occurrences are
  confined to `web_voice_test.go` and `web_voice_live_test.go`.
- **Every borrowed-number wrapper in production is either `Detach`ed or explicitly
  closed** (7 sites), so no finalizer fires under the fixture's number during a
  `runChat` usage path — none of those paths runs inside `chatRunArgsWitnessed`.

## The red proof

The pre-fix sentence and the post-fix classifier, over ONE event (one pipe, one raw
`syscall.Close`, one `read |0: bad file descriptor`), at base
`7c8982978b0a39d9c2483f9721540186f2cbaa38`:

```
=== RUN   TestRedProbeOldShapeFailsOnALostTransport
    zz_chat_argv_redprobe_test.go:44: runChat([--bogus-flag]): the refusal does not name "--bogus-flag" as a FLAG:
    zz_chat_argv_redprobe_test.go:47: PRE-FIX SHAPE saw 0 byte(s) over fd 3 (read |0: bad file descriptor)
--- FAIL: TestRedProbeOldShapeFailsOnALostTransport (0.00s)
=== RUN   TestRedProbeNewShapeShortCircuits
    zz_chat_argv_redprobe_test.go:61: NEW SHAPE short-circuited over fd 3 (same event, same 0 bytes)
--- PASS: TestRedProbeNewShapeShortCircuits (0.00s)
```

The probe was never committed; its source and output live in the run's session
directory (`evidence/zz_chat_argv_redprobe_test.go`, `evidence/red-probe.txt`).
