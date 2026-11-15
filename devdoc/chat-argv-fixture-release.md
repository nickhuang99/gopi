<!-- doc-class: normative -->
# The chat argv fixture's descriptor release, and the errno that names the actor

**Task**: 01a0f978-70d0-79f3-a02d-212c187be5e0 · **Branch**: `task/01a0f97870d079f3a02d212c187be5e0`
**Surface**: `cmd/agent/chat_argv_test.go` (`chatRunArgs` and its release helpers)

## The occurrence (measured, not described)

A whole-package run of `cmd/agent` on the base commit failed a case whose every
assertion had already held:

```
--- FAIL: TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses (0.00s)
    --- FAIL: TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--resume (0.00s)
        chat_argv_test.go:297: chatRunArgs: the stdout reader read end (fd 7) was already released
            when this helper came to free it (close |0: bad file descriptor, kernel reading "")
        chat_argv_test.go:297: read stdout: read |0: bad file descriptor
```

Evidence: `~/.gopi/sessions/01a0f978-…/prefix-whole-pkg.txt` (base, 2 occurrences of
`bad file descriptor`, exit 1) and `postfix-whole-pkg.txt` (the same run after the
repair). Duration ~9 minutes per whole-package run, hence the deliberate budget below.

Two facts from that line are worth more than the rest:

- **`|0` is not a descriptor number.** `os.Pipe` names its read end `|0` and its
  write end `|1` (`GOROOT/src/os/pipe_linux.go` → `newFile(fd, …)`), and
  `*os.PathError` carries `f.name`, not `f.Fd()`. So `read |0` / `close |0` says
  *which END of which pipe*, never *which number*. A previous reading of this
  family (`|0` read as "fd 0 ⇒ the suite released stdin") was a misreading of the
  error text; the fd in the line above (7) comes from the fixture's own log.
- **The release saw the AFTERMATH, not the cause.** The `close |0: bad file
  descriptor` was answered by the kernel for fd 7 — a number this fixture had
  opened microseconds earlier — and the reader's `read |0: bad file descriptor`
  says the same number was already gone while the fixture was measuring it.

## What the errno actually distinguishes (measured)

A descriptor can be released under a wrapper by a path that does not own it, and
the *wrapper* never learns about it. Measured this session
(`~/.gopi/sessions/01a0f978-…/pipemeasure/main.go`), on go1.24.5:

```
(A) wrapper fd=3 name="|0"
(A) ReadAll after a RAW close: n=0 err=read |0: bad file descriptor | Is(EBADF)=true Is(os.ErrClosed)=false
(B) wrapper Close after raw close: err=close |0: bad file descriptor | Is(EBADF)=true Is(ErrClosed)=false
(C) second raw close: err=bad file descriptor | Is(EBADF)=true
```

So the two errnos name **two different actors**, and that is the whole repair:

| errno | who released the number | meaning for this fixture |
|---|---|---|
| nil | — | the release did what it wanted |
| `os.ErrClosed` | **the wrapper itself** | this fixture's own earlier release; nothing was left to free |
| `EBADF` | **a path outside it** | the kernel says the number is not open and this fixture's wrapper never closed it — a suite descriptor leak |

The pre-fix release answered `EBADF || os.ErrClosed` as **one boolean**, so the
whole-package occurrence was indistinguishable from this fixture's own teardown —
which is why the earlier sibling repair (commit `059b62fb`, "the argv fixture's
descriptor release is hygiene, not an assertion") stopped the case from *failing*
without ever making the occurrence *readable*. This change keeps both: the foreign
release is recorded with the kernel's own reading of the number and the
occurrence's stack, and it does not end the case, because every stream and the exit
status are drained and captured before the release runs.

## What was ruled out, and with what

- **A production `Close` on a standard stream** — no `os.Stdin/Stdout/Stderr.Close`
  in non-test code (grep over `internal/`, `cmd/`).
- **`t.Parallel()` in this package** — zero occurrences, so the case does not run
  beside a sibling that could free its number.
- **`internal/testutil`'s lifetime probe** — its `writeReadiness` wraps a BORROWED
  number and `defer f.Close()`s it, which clears the finalizer; its own guard
  (`borrowed_fd_ready_test.go`) asserts the descriptor is gone afterwards.
- **The fixture's own release freeing a read end another goroutine was still
  draining** — the pre-fix helper released only after `o, e := <-outCh, <-errCh`,
  i.e. both readers had already returned.
- **`os/exec`'s "found a race" stderr marker** — 0 `[stderr]` lines in the
  baseline run, so no exec-installed race wrapper was involved.
- **Finalizer timing inside my own fix** — measured and rejected: a `syscall.Close`
  release *did* produce `EBADF` on a number the same fixture had opened (three
  cases red in one targeted run) and the same run produced **zero stray `close`
  failures** from a fix that preserves the fixture's existing `defer` shape. The
  fixture installs its pipes as `os.Stdout`/`os.Stderr`, so it cannot retain every
  claim it ever makes on a number; releasing through the wrapper lets
  `os.File.Close` clear the finalizer in the same call, so exactly one path ever
  frees a number and a stale wrapper cannot close a recycled one.

## The strace window, and its honest boundary

`strace -f -y -e trace=pipe2,close` over the pre-fix test binary (3× `-test.run 'TestChat'`)
shows a **doubled failing close of a live pipe number, by one process**:

```
3572498 pipe2([3<pipe:[37808464]>, 7<pipe:[37808464]>], O_CLOEXEC) = 0
3572498 close(3<pipe:[37808464]>)       = 0
3572498 close(3)                        = -1 EBADF (Bad file descriptor)
3572498 close(3)                        = -1 EBADF (Bad file descriptor)
```

`close(3)` twice for one number that was freed earlier in the same process, with
the fd still alive as a wrapper's stale cache — the extra close is **invisible to
Go** (it closes an already-closed number, so the syscall returns no error the
runtime can act on), which is why the library layer says *file already closed*
while the kernel already said *bad file descriptor*, and why an operator reading
Go's error text can be led to the wrong culprit.

**What that window does NOT do**: it ran `-test.run 'TestChat'` (and failed two
unrelated cases at the unpatched-master tip — see "Not covered"), so it does not
name the *suite-wide* releaser. On the pre-fix binary it showed no EBADF at all, so
it is not the whole-package occurrence reproduced; it is the same *shape* (a second,
non-owning close of one number) read at the syscall layer. Naming the suite-wide
releaser is left open below rather than guessed at.

## The repair (what a reader should take from this file)

1. **The classification is by errno, in one pure function**
   (`chatRunArgsReleaseShapeOf`), so every arm is pinned by a test without a case
   having to fail. `os.ErrClosed` and `EBADF` are asserted to classify
   *differently* — that assertion is the RED carrier: restore the pre-fix
   single-boolean classifier and the guard fails with
   `EBADF and os.ErrClosed still classify the same way`.
2. **A foreign release is recorded, never fatal.** Both streams and the exit
   status are in hand before the release runs, so failing there destroys a case
   whose every claim held — the defect this file was first repaired for. The record
   names the stream, the number, the kernel's reading of the number, who released
   it, and the occurrence's stack.
3. **An errno nobody reasoned about stays fatal.** Widening the accepted set to
   "any error" is the failure mode this taxonomy exists to prevent; the unknown
   arm reports rather than continues (Rule 15).
4. **Every read failure names its stream and number.** `read stdout: read |0: bad
   file descriptor` is unattributable; the reader says which pipe and which
   descriptor it was draining.
   **SUPERSEDED IN PART** (`devdoc/chat-argv-lost-stream-verdict.md`, task
   01a12bb87fe8): "the reader is now fatal" was true only when the stream had DATA.
   A stream lost before its first byte, with the sibling carrying the claim, cost
   the case nothing — and the one-fate rule made ONE foreign release print
   "recorded and not fatal" and `read … bad file descriptor` on adjacent lines.
   `chatRunArgsReadVerdictOf` now decides the read the way this file already
   decided the release (fatal only when the claim was harmed).
5. **A read that cannot finish is bounded** (`chatRunArgsStreamLimit`, 15s, armed
   only after the write end is closed). Bounded, not waited on forever: a pipe held
   open by a borrowed descriptor leaves `io.ReadAll` running, and the honest report
   of that state is a failure naming the stream — never a package timeout.

## Not covered (open, honestly)

- **The suite-wide releaser is not named.** This change makes it visible (recorded
  with the kernel's reading and a stack) and harmless (non-fatal), which is what
  the acceptance asked for; it does not yet point at the code that releases a
  number it does not own.
- The strace window above is `-test.run 'TestChat'` inside a repo-root cwd and
  failed two unrelated cases at the unpatched-master tip
  (`TestChatBoundaryReferenceSpellingHasOneProducer`,
  `TestChatHelpSwitchRoutesThroughTheArgument` — the same commit `bc984bb3` this
  branch descends from); it is not the whole-package reproduction, and it is not
  claimed as one.
