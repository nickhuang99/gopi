<!-- doc-class: historical -->
# The chat argv fixture's WRITE end, and the one release that was still fatal

**Task**: 01a0fb72-94db-7ea8-a095-6bbf7dffaf92 · **Branch**: `task/01a0fb7294db7ea8a0956bbf7dffaf92`
**Surface**: `cmd/agent/chat_argv_test.go` (`chatRunArgs` and its write-end verdict)
**Predecessor**: `devdoc/chat-argv-fixture-release.md` (commit `6ecc0244`) — the same
fixture's READ end. This file is the other end of that repair.

## What was asked, and what the log actually supports

The epic asked for the mechanism behind one unreproduced failure:
`TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--verbose` →
`chat_argv_test.go:494: close stdout writer: close |1: bad file descriptor`.
The instruction was explicit: reproduce, or say honestly that it does not
reproduce and name the missing evidence — never invent a mechanism.

Reproduction, measured on this branch's base (`bbc9e6a8`):

| command | result |
|---|---|
| `go test ./cmd/agent/ -run '^TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses$' -count=50 -v` | `ok`, 50/50 subtests PASS, `--verbose` 50/50, rc=0 |
| `go test -race ./cmd/agent/ -run '<same>' -count=50` | `ok`, rc=0 — **until** the run collides with the stall watchdog (`51442d69`, the other half of this branch) |

The `--verbose` case did **not** fail 100 measured iterations. So the *flake* is not
reproduced and no mechanism is claimed for it.

**What the record does support, and it is more than a suspicion**: the cited text
names a real line, and that line was still the only release in this file that
answered ANY errno by ending the case. Reading it is not a guess about the flake; it
is the fixture's own asymmetry, in the tree.

## The occurrence, reproduced verbatim

`|1` is `os.Pipe`'s name for a **write** end and `|0` for a read end
(`GOROOT/src/os/pipe_linux.go`), so the briefing's `close stdout writer: close |1:
bad file descriptor` and the repaired family's `close stderr reader: close |0: bad
file descriptor` are **one fixture's two ends**, not two actors.

The pre-fix shape, restored for the red proof (the `chatRunArgsReleaseForeignNumber`
arm replaced by the line the fixture used to carry,
`t.Fatalf("close stdout writer: %v", err)`):

```
=== RUN   TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal
    chat_argv_test.go:518: close stdout writer: close |1: bad file descriptor
--- FAIL: TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal (0.00s)
FAIL	github.com/nick/gopi/cmd/agent	0.617s
RED rc=1
```

That is the briefing's failure text, produced by a real pipe and a real foreign
release of its number — not quoted from the briefing.

## The defect, stated exactly

`6ecc0244` taught the READ-end release to decide **on the errno**: `os.ErrClosed` is
the wrapper's own earlier release, `EBADF` is a path outside the fixture having freed
the number (recorded, non-fatal, with the kernel's reading and the occurrence's
stack), and an unreasoned errno stays fatal. The WRITE ends (then
`chat_argv_test.go:124-129`) kept the old shape:

```go
if err := outW.Close(); err != nil { t.Fatalf("close stdout writer: %v", err) }
if err := errW.Close(); err != nil { t.Fatalf("close stderr writer: %v", err) }
```

Two things follow, and the second is why this is worth a commit of its own:

1. An errno that the sibling end *attributes* was, here, an unattributable verdict:
   the text names no actor — not the stream's number, not the kernel's reading, not
   whether the descriptor was free or was taken away.
2. **A `Fatalf` here ends the case while two readers are still draining that pipe.**
   The read goroutines started above cannot finish on a `Fatalf` path, so they run
   into `chatRunArgsStreamLimit` and the next failure a reader sees is the fixture's
   own teardown. That is the same misattribution the read-end repair removed, on the
   other end of the same pipe.

## The repair

- `chatRunArgs` closes both write ends **and keeps the errnos**; the verdicts are
  reported by `chatRunArgsWriteEndClosed` only *after* both streams are drained and
  both read ends released — so no reader is ever left parked by a case that is being
  ended.
- The verdict comes from the **same classifier** as the read end
  (`chatRunArgsReleaseShapeOf`): one function, two ends, so the two halves cannot
  drift into two policies.
- Fatal arms are unchanged: an errno nobody reasoned about still reports (Rule 15),
  and every record names the stream, the number and the kernel's reading.

## Guards (Rule 10.5 — a fix ships its repro)

`TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal` reproduces the shape
end to end: a pipe, a raw `syscall.Close` of its write-end number (the foreign
release, produced exactly as the read-end guard produces its own), the kernel's
confirmation that the number is gone, the fixture's own `Close` answering EBADF, and
then the verdict — which must be the foreign-release classification and must
**return**. The final assertion is reachable only if the helper did not end the case,
so a restored `t.Fatalf` cannot pass this guard by accident; a nil errno is still
classified clean, so the guard cannot be satisfied by making everything non-fatal.

Red-then-green, measured in one artifact
(`…/01a0fb72-…/evidence/writeend-red-green.txt`): pre-fix shape → `--- FAIL … close
stdout writer: close |1: bad file descriptor`, rc=1; fix restored → `--- PASS …`,
rc=0.

## Verification

- `go vet ./cmd/agent/` → rc=0.
- `go build ./...` → rc=0.
- `go test ./cmd/agent/ -run 'TestChatRunArgs|TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses' -count=1 -v`
  → 3/3 guards PASS, `--verbose` PASS, `suite goroutine sweep: parked repository
  goroutines: 0`, rc=0.
- `go test ./cmd/agent/ -run '^TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses$' -count=50 -v`
  → 500/500 subtest PASS, 0 `FAIL` lines, rc=0 (`postfix-count50-norace.log`).
- `go test -race ./cmd/agent/ -run '<same>' -count=50` → rc=0, 0 `WARNING: DATA RACE`
  (`postfix-count50-race.log`).
- No tolerance tuned, no sleep added: the fixture's only constants
  (`chatRunArgsStreamLimit`, `chatArgvChanBuf`) are untouched, and the change adds no
  `time.Sleep` or bound.

## Not covered (open, honestly)

- **The `--verbose` flake itself is not reproduced and no mechanism is claimed.**
  100 measured iterations are a weak probe against a timing window, not a refutation.
  **Missing evidence, named**: a log line of that case actually failing
  (`…/--verbose` with its own output) from the run that produced the briefing's text.
  No session log on this host carries it — `rg "close stdout writer"` over
  `~/.gopi/sessions/` finds nothing, and the parent run's own `output.log` contains
  the *subtest name* 8 times and the failure text zero times. The text survives only
  in the task briefing, which is why this commit fixes the *line* it names rather than
  the *flake* it describes.
- **The suite-wide releaser is still not named.** This change makes a foreign release
  of either end readable and harmless; it does not point at the code that frees a
  number it does not own. That remains the open half of the family
  (`devdoc/chat-argv-fixture-release.md`, "Not covered").
- The write-end verdict's `AlreadyReleased` arm is exercised by the classifier guard,
  not by an end-to-end case: the fixture holds and closes its own writer, so no
  measured occurrence of that arm exists to reproduce.
