<!-- doc-class: normative -->
# The note leg's released handle — a SIGSEGV replaced by a named error

**Task:** 01a13329388970dca5514257a19cdb0d · **Branch:** `task/01a13329388970dca5514257a19cdb0d`
**Symptom (task 01a132e2's acceptance run, run-1791858570):** `go test ./cmd/agent/`
reported a **package-level fail with no test name**, and every PASS in the package
was discarded with it. The stack showed `chatNoteQuery` at
`internal/tools/chat_notes.go:574` dereferencing a nil `*sql.DB`; the second
argument of `ChatNoteSearchOn` on that frame was `0x0`, and the call came from
`cmd/agent/chat_prefetch_notes.go:302`.

## Root cause — the handle's LIFETIME outlives the POINTER

The note leg's second retrieval leg asks a warm handle the question *"is the
store I want the one you already hold?"*, and until this change it answered that
question with **path equality alone**:

```go
func (s *prefetchNotesSource) handlesThePath(h *prefetchHandle, path string) bool {
    return h != nil && strings.TrimSpace(h.path) != "" && h.path == strings.TrimSpace(path)
}
```

`prefetchHandle.path` is a **recorded fact** about what the handle was dialled
against, and `(*prefetchHandle).close()` — called by `chatPrefetch.Stop`, the
executor's last act — closes the connection and **nulls only `db`, never
`path`**. So the three states of a handle are not two:

| state | `h` | `h.db` | `h.path` | path equality says |
|---|---|---|---|---|
| never dialled | nil | — | — | no |
| warm | non-nil | set | set | **yes → reuse** |
| **released** | **non-nil** | **nil** | **set** | **yes → reuse** |

The third row is the defect. Any lookup that captured the handle's **pointer**
before `Stop` ran — and both the typing path (`chatPrefetch.fetch`) and the turn
path (`startTurnLookup`) do exactly that, on their own goroutines — could reach
`handleFor` *after* the closure and be told "same file, already warm". The leg
then recorded `reused`, returned the shared handle, and handed
`db.usableDB()` — `nil` — to `tools.ChatNoteSearchOn`.

That function's first statement was `db.QueryContext`, and `database/sql`
dereferences the receiver internally (`(*DB).conn`). A nil receiver therefore did
not produce an error at a seam: it produced a **SIGSEGV that killed the test
binary**, which is why the run stream carries a package-level fail and no test
name — a crashing test process reports nothing about which test crashed it.

**How it was reached in the failing run.** `TestPrefetchSealDoesNotWaitForAnInFlightLookup`
starts `go p.fetch(query)` against a **lock holder that holds the ledger
EXCLUSIVE**, so that lookup cannot finish inside the test's window; the test then
returns and `defer p.Stop()` runs, closing the session handle while the lookup is
still inside `handleFor`. Reading a captured `*prefetchHandle` is not a data race
(the lookup owns its copy of the pointer and `Stop` only nulls the field), so the
race detector is silent — the bug is a **staleness** defect, not a race, and it
was therefore invisible to `-race`.

**Not a fixture defect.** The test constructs nothing unusual; it drives the
production lifecycle (`fetch` → `handle()` → `handleFor`) with the executor's own
`Stop`. A production chat that seals a turn and then ends the session has the
same two lifetimes overlapping. The defect is in the production path; the test is
where it happened to be caught.

## The fix — two halves, neither of which is a `recover`

**`internal/tools` (Rule 15: name the misuse).** `ChatNoteSearchOn` and
`chatNoteQuery` ask one predicate, `chatNoteHandleUsable(*sql.DB) bool`, and
return the exported sentinel `ErrChatNoteUnusableHandle` instead of dereferencing.
The guard sits at **both** entry points into the tier walk: the exported face (a
caller's own handle) and the tier runner (the CLI's walk, which no exported entry
point has vetted). The check is placed *after* the empty-query validation so the
pre-existing contract — an empty query is reported as an empty query — is
unchanged.

**`cmd/agent` (do not offer a handle that cannot be queried).**
`prefetchHandle` grows the predicate it was missing — `usable()` and its value
form `usableDB()` — and `handlesThePath` now reads *`h.usable() &&`* where it
previously read *`h != nil &&`*. One predicate, three call sites
(`handleFor`, `prefetchLookupPhased`'s warm/dial branch, and the note leg's
search), so a released handle is **refused for reuse** and the leg falls back to
its own dial — the fallback the latch already provides. A reader of the run's
record can already see which shape ran: the memory leg's `segments.handle`
reports `session` or `fresh`, and the note leg's `notes.outcome` says
`hits`/`empty`/`failed`, so a session whose handle was released mid-flight is
visible in the record rather than only in a crash.

Nothing is recovered and nothing is swallowed: the mis-handled state is *named at
the seam* and *not produced* by the caller.

## Deliberate choices

- **No `recover`.** A recovered panic still leaves the caller unable to tell
  "no notes matched" from "I was given a broken handle" (Rule 15).
- **No new tool, script or resident artifact** (Rule 35). The fix is a predicate
  and a sentinel inside the two packages that already own these lifetimes.
- **The `excluded`-by-identity list I drafted was NOT shipped.** An earlier design
  remembered the executor's handle on the leg so "released" could be told from
  "never offered". `usable()` answers the same question with the handle's own
  state, and a list would be a second source of truth that can go stale (Rule 14).
- **`handlesThePath` stays a path comparison** — it is the right question when
  asked of a handle that can still be queried; the defect was never the
  comparison, it was asking it of a released handle.

## Checks

- `go test ./internal/tools/ -run 'TestChatNoteSearchOnRefuses'` — the nil-handle
  contract (2 tests).
- `go test ./cmd/agent/ -run 'TestPrefetchNotesLegDoesNotReuseAReleasedHandle|TestPrefetchLookupDrivesAReleasedSharedHandle'`
  — the leg's choice and the whole lookup, each red on the pre-fix code.
- `go test -count 5 -run TestPrefetchSealDoesNotWaitForAnInFlightLookup ./cmd/agent/`
- `make test-budget PKGS='./cmd/agent ./internal/tools'` — no package-level fail.

census-evidence-ok: no project-wide figure is stated in this document.
