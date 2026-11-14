<!-- doc-class: normative -->
# A raw `close` must ask the kernel first: this number, still mine?

**Status:** implemented. `internal/borrowedfd/release.go` + the guards listed below.
**Scope:** test fixtures (and any caller) that hold a raw descriptor NUMBER.
**Supersedes nothing** — it is the second half of the borrowed-fd family: the first
half, `devdoc/fd-finalizer-leak.md`, kept a borrowed wrapper from being finalized;
this one governs the moment a raw number is given BACK.

## 1. The defect, stated as a window rather than as an actor

```
1. a fixture takes a raw descriptor number          (syscall.Dup / F_DUPFD)
2. the number stops belonging to it                 (handed to a production path
                                                     that closes it; closed by a
                                                     production path; released by
                                                     the finalizer; released by the
                                                     fixture itself)
3. a LATE syscall.Close(number) arrives
```

Between 2 and 3 the kernel recycles the number: any `open`, `dup` or `pipe` in that
window receives it. So the late close **releases a stranger's live descriptor** —
and reports success, because `syscall.Close` returns `nil` for a number that is
open, *whoever holds it*. The site cannot tell a correct close from a destructive
one, which is exactly why chasing "who closed it" never converges.

**Measured, both at the base commit `d58851d` and on the fixed tree** (the idiom
uses only `os` + `syscall`, so the same evidence is obtainable on either):

```
PRE-FIX IDIOM: late syscall.Close(950) returned <nil>;
the stranger at ".../stranger" is now open? false (F_GETFD errno=bad file descriptor)
DEFECT CONFIRMED: fd 950 was the stranger's, syscall.Close returned nil, and
".../stranger" is gone
```

A second, stronger measurement — the fixture's own guard, with the pre-fix idiom
restored by mutation, names the theft instead of performing it:

```
releasing the stranger's own descriptor 8 was refused: refusing to close descriptor 8:
it is no longer the descriptor this call claimed
  (recorded: fd 8 → ".../stranger" (dev 27, ino 309223673);
   observed now: nothing (the number is closed))
— a close here would release a number the kernel has recycled under whoever opened
it next, and syscall.Close reports success when it does
```

## 2. The judgement this replaces the search with

**Before every raw close, ask the kernel what the number now denotes, and compare
it with what it denoted when the caller claimed it.** Equal → close. Not equal, or
gone → **do not close**, and say which number, what was recorded, and what was
observed (Rule 15: a refusal that does not name its three facts is
indistinguishable from a broken instrument).

The instrument is `internal/borrowedfd/release.go` — ONE definition, in the leaf
every borrower already imports (Rule 14/1: the retention idiom in
`borrowedfd.go`, the measuring window in `settle.go`, and this judgement all live
there rather than being copied per package):

| call | meaning |
|---|---|
| `IdentityOf(n) (Identity, error)` | the kernel's reading of `n` **now**: `readlink /proc/self/fd/n` + `fstat` dev/ino. Refuses (never a zero value) when `n` names nothing. |
| `Identity.Check() error` | re-reads and compares. `nil` **only** when the number still denotes the same object. |
| `Identity.Release() error` | `Check` then close. Returns `*MovedError` when it refused. |
| `Witness(n) Identity` | takes the reading as EVIDENCE and never refuses — for the one shape where a raw close is correct (§4). |

`*MovedError` is a distinct type, not a formatted string, so a caller can tell
**"I refused to close a stranger"** from **"the close failed"** with `errors.As`.
That distinction is the whole point: one is a correct decision, the other is an
I/O error.

**The number is taken at CLAIM time, never at release time.** An identity taken
*after* the window has opened compares the number with itself and always agrees —
a guard green for the wrong reason. This is a rule about the ORDER of two calls,
and it is the one mistake the API cannot prevent for you.

**MEASURED, the hard way** (commit `7509a97a`): the first version of the
`internal/terminalgeom` helper called `IdentityOf(n)` *inside* its own release
function — release time. By then the kernel had recycled the number, so the
**stranger resolved as the claim** and the helper closed it. The guard reported:

```
the helper answered <nil> for a number that is no longer ours — anything else
means it closed (or tried to close) the stranger on fd 3
```

That is the whole failure mode of reading identity late: the comparison has two
sides and both are read after the window opened, so it agrees with itself. The
repair is a **claim VALUE** carried across the window (`ptyMasterClaim`: the number
plus the kernel's reading of it, taken where the fixture opened it), never a
re-derivation at release time.

**A helper that refuses must RETURN the refusal, not report it.** A release
helper calling `t.Errorf` itself cannot be asserted from outside — the only way to
observe its decision is to fail the test, so a guard that must distinguish
"refused" from "closed" cannot be written against it. `releaseOnFatal()` therefore
returns `error`, and each fatal arm decides how loud to be.

(`ClaimAndRelease` was written and then **removed**: it had no caller, and its only
plausible shape — claiming and releasing a number that never crossed a window — is
a decoration of `Witness(n).Release()`. A function with no real caller is a
dependency nobody asked for, Rule 35.)

## 3. Two residual facts the instrument does NOT hide

Rule 15 applies to limits as much as to failures — a limit that is not stated reads
as a guarantee:

1. **The identity is the kernel's reading of the NUMBER** (`readlink` + dev/ino).
   Two different open file descriptions of the SAME file or pipe inode are
   indistinguishable to it, because Linux exposes no identifier for an open file
   description. For this defect that is sufficient, and it is what keeps the
   instrument cheap: the recycle window hands the number to a DIFFERENT object, and
   a different object always resolves differently.
2. **A number claimed and released inside one uninterrupted call has no window at
   all.** The instrument is for numbers that CROSS a boundary; claim and release
   must therefore be two places in the code, with the claim BEFORE anything that
   can hand the number away.

## 4. The sites and their verdicts

The rule is not "never call `syscall.Close`". It is **"know which of these you are
in, and be unable to be wrong about it"**:

| shape | verdict | what the code does |
|---|---|---|
| **OWNED, never transferred** (the fixture's own dup; `daemonDeathPipe` *detaches* rather than closes) | the fixture may release it | guarded release (`Identity.Release`) |
| **HANDED OVER** (the number was given to a production holder, e.g. `workerDaemonLockRef`) | the fixture must NOT close by number | released **through the holder** (`*os.File`), with `Check()` first |
| **RELEASED BY PRODUCTION** (`daemonNote`, `workerDaemonNoteReady`, `writeReadiness` all `defer f.Close()`) | the fixture must NOT close at all | the guard *proves* the late close is defeated (§5) |
| **DELIBERATE FOREIGN RELEASE** (releasing the number IS the arm's premise) | the raw close is correct | `Witness` before it, `Check()` after it → the close is attributable by identity |
| **FATAL ARM BEFORE WRAPPING** (`internal/terminalgeom`'s `openTestPty`: the number exists only raw until it is handed to `os.NewFile`) | the fixture may release it | a claim VALUE taken at `open` and carried into each arm; the helper returns its refusal |

Two specific repairs worth naming:

- **`cmd/agent/borrowed_fd_sites_test.go:274`** — the success path handed the
  wrapper to `workerDaemonLockRef` (the flock IS the single-instance guard) and the
  fixture *still* kept a `defer syscall.Close(borrowed)`. It now releases through
  that holder. The handle must be **captured by value**: the test deliberately
  drops the local (that drop is what makes the package-level ref the only thing
  holding it), so a closure reading the variable finds `nil` at teardown —
  measured, `fd 7 was handed over but the holder is nil`.
- **`internal/terminalgeom/terminalgeom_test.go` (`openTestPty`)** — three
  `unix.Close(mfd)` on `t.Fatalf` arms. They are the only `unix.Close` sites in the
  tree, so the `grep 'syscall.Close'` in §6 cannot see them; a census that only
  greps one spelling is partial. All three now release through the instrument.
- **`cmd/agent/daemon_death_pipe_shape_test.go` (`TestDaemonDeathPipeAcceptsTheSuitesOwnShape`)**
  — a **double close on one number**: the test closed the wrapper it got back from
  `daemonDeathPipe` (which *detaches*, keeping the number open), and the fixture's
  teardown released the same number again. The pre-instrument version absorbed it
  as an ignored `EBADF`; the instrument named it (`refusing to close descriptor 201
  … observed now: nothing (the number is closed)`). One release now, through the
  holder, `Check()` first.
- **`cmd/agent/chat_argv_test.go`** — an **explicit double close**:
  `defer syscall.Close(dupFd)` *plus* an explicit `syscall.Close(dupFd)` in the
  negative arm. The second close lands on whatever the kernel recycled the number
  to. There is now exactly one release, guarded, with a `dupReleased` flag so the
  teardown neither closes twice nor swallows a refusal it should report.

## 5. The guard, and why it cannot be green for the wrong reason

`F_DUPFD` builds the recycle window **deterministically** — the kernel recycles the
lowest free number and the test decides when a number becomes free, so the window is
an ORDER of three calls rather than a timing coincidence. That is also why the
guard needs no load of any kind (Rule 27): nothing here measures the machine.

Two measured traps, both found by writing the guard (they are in the code as
comments, because the next author will hit them):

1. **`os.OpenFile` takes the LOWEST free number** (fd 7 on this host), *not* the
   number you just released. The first version of the helper released the claim,
   opened a stranger, and then `dup`'ed it to the claimed number — which landed on
   fd 8, and the guard failed for a reason that had nothing to do with the defect.
   The helper now checks whether the `open` already took the number.
2. **A zero `Identity` has number `0`, a REAL descriptor.** A `number < 0` sentinel
   therefore never fires, and a forgotten claim gets compared against `/dev/null`
   and reported as a `MovedError` instead of as "never taken". Measured red before
   the `taken bool` field existed. `Equal` refuses an untaken identity against
   everything, including another untaken one.

3. **The stranger must be opened as a RAW NUMBER, never as an `os.OpenFile`
   wrapper.** A wrapper minted before the number is placed gets **abandoned** when
   the descriptor is renumbered, and its finalizer then closes that same number a
   second time — after the kernel recycled it. This happened *inside these guards*,
   and the machine found it: another test in the same package failed with
   `transport-lost` on a pipe it never touched, and `TempDir RemoveAll` failed with
   `unlinkat …: bad file descriptor`. `syscall.Close` returned `nil` both times,
   which is the premise of this whole document. The shape that cannot go wrong is
   `syscall.Open`/`unix.Open` → `F_DUPFD` → *then* `os.NewFile`.

   The corollary is a rule about single-test runs: `-run TestX` was green four
   times over while the victim was a different test. **Only the whole package
   (`-count=1`) could see this defect**, so that is the measurement a guard of this
   family requires.

Each site's guard carries the defect's own shape: the stranger must **survive**, the
refusal must **name** number + recorded + observed, the refusal must be a
`*MovedError`, and — the negative control, without which a release that refused
everything would read as correct — `TestReleaseClosesTheNumberItStillHolds` proves
the instrument really does close a number nothing took away.

## 6. Where to look

```bash
# BOTH spellings — a census of one misses internal/terminalgeom entirely
grep -rn 'syscall\.Close\|unix\.Close' --include='*_test.go' .

go test ./internal/borrowedfd/ -run 'TestRelease|TestCheck|TestIdentityOf' -count=1 -v
go test ./internal/testutil/ -run TestWriteReadinessClosesItsBorrowedDescriptor -count=1 -v
go test ./internal/terminalgeom/ -count=1 -v
go test ./cmd/agent -run 'TestBorrowedFDObservationIsSensitive|TestDaemonDeathPipeDroppedWrapperLeavesTheNumberLive|TestWorkerDaemonInheritLockSuccessKeepsTheBorrowedNumberLive|TestDaemonReadyFDSitesCloseTheirBorrowedWrapper|TestDaemonDeathPipeAcceptsTheSuitesOwnShape' -count=1 -v

# the measurement that actually decides this family (a single-test run cannot)
make test-budget PKGS='./cmd/agent ./internal/borrowedfd ./internal/testutil ./internal/daemonlifetime ./internal/terminalgeom'
```

Every remaining `syscall.Close` in a test is one of §4's four shapes and says which
in its own comment. A new one that does not is the defect returning.
