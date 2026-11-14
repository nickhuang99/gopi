<!-- doc-class: historical -->
# chat_surface action=key — key-level injection into the run's own editor

Task `01a0e438-fee6-72f6-99c2-531efd7aff5c`. Deliverable: `feat/chat-surface-key-injection`.

## The problem this closes

`chat_surface` could already **observe** the run's own screen (`see`, `compare`)
and **drive the conversation** (`type`, which enqueues a whole line as the next
turn). It could not **drive the editor**.

That gap is not cosmetic — it made a class of interaction unverifiable from
inside a turn. The measurement that produced this epic: a self-driven turn
wanting to exercise the completion menu had to type `"/q"` and then send Down.
`type` submits a whole LINE, so `"/q"` *executed* the command instead of opening
its menu; the Down then arrived at a prompt that no longer had a menu. The
interaction the agent wanted to test was exactly the one it could not enter.

## The design decision, and the measurement behind it

**Injection sits at the `Source` boundary — below the editor's decoder, above the
terminal — as a decorator (`internal/lineedit.Injector`).**

The editor's input path has one shape: `Run` reads a `Source` (`ReadEvent`), and
every byte goes through `Decoder.Feed` → `handleKey`. That is where "a key" is
defined (`ESC [ B` → `KeyDown`, `\r` → `KeyEnter`), and where the editor's own
policies live: the completion menu owns Enter and Up/Down, a lone ESC is the
menu's close key, and a paste burst is classified by timing.

An `action=key` that called `handleKey` directly — or that edited `Value` —
would have been a *second key path*: it could not be the thing a keyboard is,
and every one of those policies would have to be re-implemented beside the one
that already works. Sitting at the Source means the editor cannot tell an
injected arrow from a typed one, **because the bytes are identical and the
decoder is the same object**.

### The trap: an injected sequence is classified as a paste

This is the part that cost the most and is the reason the design has a pacing
rule. The editor classifies a paste by two independent laws:

1. `PasteThreshold` — one read at/above ~4 KiB is a paste by SIZE.
2. The burst clock (`Value.NoteKeys`) — the gap between two **reads** is compared
   against `PasteBurstCharInterval`; once `PasteBurstMinChars` (3) characters
   arrive inside that window the burst is armed, and an Enter within
   `PasteEnterSuppressWindow` of the burst's last key is read as **pasted
   content** (a newline in the buffer), not as a submit.

A single write of `"/quit\r"` is exactly three contiguous characters plus an
Enter: it is indistinguishable from a paste, so the `\r` becomes a newline and
the line never submits. **Measured twice, independently:**

- In the package case: removing the pacing made the acceptance case fail with
  "the line never submitted after the second injected Enter" while the line was
  visibly present.
- In the end-to-end case: the *bootstrap* line written to the pty master in one
  syscall (`"/help\r"`) sat unsubmitted on the editor row — the very phenomenon
  this feature exists to make expressible.

So the injector **delivers one key per read and paces them**: a second unit is
held until `PasteEnterSuppressWindow × injectKeyIntervalFactor` has passed since
the previous delivery. The base interval is the **Enter-suppression window**, not
the intra-burst interval, because an injected *arrow* is a single 3-byte read —
it satisfies `PasteBurstMinChars` by itself and arms the burst alone. That was
found the hard way (a pacing base of `PasteBurstCharInterval` failed the
acceptance case).

### Getting the bytes read: two designs that failed, and the one that works

Pacing is only half the problem. The other half is that the read loop's wait is
**unbounded whenever nothing is outstanding** (`editor.go` decides
`pollTimeoutBlock`), so a queued key has to be able to interrupt a parked read.
Two designs were tried and rejected, both by measurement:

1. **Slicing the inner read** into fixed polls. It looked harmless and was not:
   it rewrote the read's *contract*. A source may treat a bounded wait and an
   unbounded one differently — the chat harness's script source expires a bounded
   wait immediately and parks an unbounded one until its stop arrives — so slicing
   made that stop path unreachable and the editor's read loop never returned
   (`the editor's read loop did not return: the caller's writes cannot be
   attributed`). A decorator may delay the caller's read; it may not redefine it.
2. **Relying on that same fallback after removing the slicing.** With the contract
   respected, the queued arrow simply sat there: `pending=3` and an unmoved
   selection for a full second, because nothing was going to return from a
   blocking read.

The mechanism that exists for exactly this is the source's **wake seam**, and the
injector uses it: queueing a key notifies the inner source, the parked read
returns `EventWake`, and the editor comes back to the injector to take the key
(measured after the change: `read=true, pending=0`, and the selection moved).
Two properties keep it honest:

- it resolves the seam through the package's ONE resolver (`SourceNotifier`),
  because a source may carry its notifier either as the `Notifier` interface or
  behind a `WakeNotifier()` accessor — the chat harness's script source uses the
  first, a real terminal the second, and a decorator that knew only one of them
  silently downgraded the other (measured: `the harness's source supplied no wake
  seam`);
- it only wakes a source with a REAL seam — `NotifyEnabled() false` means the
  notification is a no-op, and the injector then falls back to the inner read's
  own cadence rather than pretending.


## `type` vs `key` — the documented distinction

Both are in the tool's description and each names the other and its use case:

| | `type` | `key` |
|---|---|---|
| what it touches | the **conversation**: enqueues a finished line as the next turn | the **input path**: raw bytes into the editor's decoder |
| enters the editor? | never | always |
| submits? | yes (that is its purpose) | no — never |
| bound | `max_turns` (a model round trip each) | `max_keys` (a key costs nothing) |

The two bounds are **separate on purpose**: counting keys against `max_turns`
would either starve the interaction or spend the turn budget on arrow keys.

## The bound, and its loudness

- `chatSurfaceKeyCeiling` = 64 keys per session (declared in `internal/agent`,
  installed into the injector by `cmd/agent`'s wiring and enforced *again* by the
  agent's own counter — one definition read by two consumers).
- The ceiling is charged at **acceptance**, not at consumption: a queued key has
  been spent.
- Exhaustion is reported **on the call that spends the last key** (`Last`), the
  answer says `CEILING REACHED`, and the same fact is printed to **stderr** — the
  operator's console. A bound only the model can see is not a bound
  (`devdoc/runaway-loop-20260923.md` is that incident).
- Nothing about the ceiling silently drops a key: the refusal carries the reason
  and the counters (`keys used N/M`).

## Verification

- Build: `go build ./...` → OK.
- Package + agent tests: green (see the report's `Tests:` line).
- **The changed surface was executed for real** — three rungs, weak to strong:
  1. `internal/lineedit` — the injector against its own editor, with the
     accept+submit leg asserted on the cell grid (`TestInject_MenuOpensWalksAndSubmitsOnTheGrid`).
  2. `cmd/agent` — the production constructor over a real pty
     (`TestChatInject_KeysReachTheEditorOnARealPty`).
  3. `cmd/agent` — **the full stack**: the real REPL (`runChatWithInput`), the
     real editor on a pty, the real `chatSurface` installed by
     `installChatSurface`, and a mock model whose only act is
     `chat_surface action=key` (`TestChatSurfaceKey_DrivesTheRealREPLEditorOnARealPty`).
- **Red proofs** (each reproduced, then reverted):
  - remove the pacing → the acceptance case's submit leg fails ("the line never
    submitted");
  - drop the injector wrapper from the constructor → `NO injection seam`;
  - make `chatSurface.InjectKey` always refuse → the run's menu never opens;
  - slice the inner read / drop the wake → a queued key sits unread (measured:
    `pending=3`, selection unmoved for 1 s), and the chat suite's own read-loop
    case fails with "the editor's read loop did not return".
- **Regression guards for the two defects this design cost**:
  `TestInject_PassesTheCallersTimeoutThroughToTheInnerSource` (the decorator must
  not rewrite the caller's read contract) and
  `TestInject_ForwardsTheWakeSeamInBOTHSpellings` (both notifier spellings, plus
  the no-op sink that says so).

## Files

- `internal/lineedit/inject.go` — the injector, the key table, the ceiling.
- `internal/lineedit/inject_test.go` — the package acceptance case + bounds.
- `internal/lineedit/scripted_test.go` — the source's stop is now observable from
  a bounded wait (required by the sliced read).
- `internal/agent/chat_surface.go` — the action, its schema, validation,
  dispatch, the ceiling, the exhaustion notice.
- `cmd/agent/chat_surface_surface.go` — the surface half (no editor → a sentence,
  not a promise).
- `cmd/agent/lineedit_wire.go` — the injector installed where the terminal is.
- `cmd/agent/chat_inject_pty_test.go`, `cmd/agent/chat_surface_key_e2e_test.go` —
  the wiring case and the full-stack case.

## Not done (honest)

- The wake the injector raises is the source's OWN seam, so it is only raised when
  that seam is real (`NotifyEnabled()`); a source with no seam leaves the key to
  the inner read's cadence. No second mechanism was invented for that case.
- Nothing was deleted or cleaned: the `/dev/shm/gopi-base-check` detached
  worktree used to prove one failure pre-existing could not be removed from
  inside this session's declared roots (the guard refused `git worktree remove`
  and `rm -rf` outside them). Its registration is still listed by
  `git worktree list`; its directory was already removed. An operator can drop it
  with `git worktree prune` from the main repo.
