<!-- doc-class: historical -->
# The Error Classifier: a Missing Spelling, and "false ≠ unknown"

**Status:** landed. **Surface:** `internal/agent/crashdump.go` (the run-error
text classifier), `internal/agent/session.go` (the two renderers),
`internal/agent/taskregister.go` (the ledger hint contract).

## The defect (measured 2026-10-16)

`loop.go`'s round-boundary fallback ended a run with
`agent timed out after %d iterations`. `collectErrorRecord` classifies a run
error from its TEXT, and its switch matched `"timeout"` — while the sentence
says **"timed out"** (one space apart, and *not* a substring). The message fell
to `default:`, which asserted:

```go
default:
    er.RecoverableHint = false
```

Two real peerbox ledger rows consequently read:

```
error(category=unknown iter=52 recoverable=false)
error(category=unknown iter=43 recoverable=false)
```

Two separate defects are visible in those two lines, and they are independent:

1. **A truth that was never asked.** A timeout from the transport layer is a
   network failure — the request was fine, the provider or the wire was not —
   and the taxonomy scores that `recoverable`. The classifier said the
   opposite because it did not recognise the spelling.
2. **A verdict issued where none was earned.** `category=unknown` is the
   taxonomy's own word for "an unknowable error"
   (`deepseek.CategoryUnknown`: *"failure with neither status, body, nor
   transport text"*). Asserting `recoverable=false` beside it says, in one
   breath, "we cannot tell what failed" and "resume certainly cannot help"
   (**Rule 15a — false ≠ unknown**). The row's own text made the contradiction
   visible: it offered `gopi session resume` while reporting
   `recoverable=false`.

The producer half (the misleading sentence itself) is a separate row, already
landed — `internal/runend`, `devdoc/ctx-ended-run-reason.md`. That document
names this half explicitly and leaves it to this row.

## The classification contract

`collectErrorRecord` has two evidence channels, in this order:

1. **The HTTP snapshot** (`Client.LastErrDetail`) — status + body + transport
   text. When it has failed, the taxonomy classifies it
   (`deepseek.FailureSnapshot.Category()` / `.RecoverableHint()`) and this file
   only copies the verdict out.
2. **The run error's TEXT** — the fallback for errors that never produced a
   response (transport failures, the loop's own engine-forced ends).

The text channel's recognition set is now data, not a chain of literals:

```go
runErrVendorMarkers  = []string{"stream error"}
runErrNetworkMarkers = []string{
    "timed out", "timeout",
    "context deadline exceeded", "context canceled",
    "connection refused", "EOF", "no such host",
}
```

- **Both spellings**, because they come from different writers: Go's
  `net/http` says `timeout`/`timed out`, our own timeout wrappers say
  `timed out` (`internal/tools/withtimeout.go`, `search.go`), and the loop's
  context fallback says `context deadline exceeded`/`context canceled`.
- **The set is a table so the next missing spelling fails in a test**: the
  pre-fix arms each matched only the spelling typed in their own literal, which
  is exactly how `"timed out"` went unnoticed.
- **The recoverability verdict is NOT grown here.** The classifier returns a
  category; the hint is derived from the taxonomy
  (`deepseek.FailureSnapshot{TransportErr: msg}.RecoverableHint()`), so a
  text-classified network failure and a snapshot-classified one cannot
  disagree (Rule 13/14).

## The semantics decision on the unknown arm

Three options were considered for "nothing recognised this error":

| option | why not |
|---|---|
| `*bool` (nil = unknown) on `ErrorRecord` | the honest type, but `RecoverableHint bool` is written into the session JSONL line and the dump document, whose shape older readers and dumps already carry — a migration is not this fix's business, and half-migrating the type is a larger change than the defect warrants |
| a second `RecoverableKnown bool` field | same shape problem, plus two fields that can drift apart |
| **in-band spelling (chosen)** | the unasserted state is the pair `(RecoverableHint=false, Category=unknown)`, and every renderer *derives* the distinction through ONE method, `ErrorRecord.recoverableKnown()` |

`recoverableKnown()` is the single reader-side predicate:

```go
func (er *ErrorRecord) recoverableKnown() bool {
	return er.RecoverableHint || er.Category != string(deepseek.CategoryUnknown)
}
```

It is sound because the two cases are disjoint by construction: every category
the taxonomy decides comes with a verdict from its own `RecoverableHint()`
(network/vendor/5xx → assertable; account/4xx-no-1214 → assertable `false`),
and `false` can never be *derived* for a decided category — the only
classification that yields `CategoryUnknown` is "nothing was decided". A
genuine `recoverable=false` therefore always rides a non-unknown category.

**What each surface now says**

| surface | decided-and-true | decided-and-false | not decided |
|---|---|---|---|
| ledger digest (`SummaryDigest`) | `recoverable=true` | `recoverable=false` | `recoverable=unknown` |
| `session_end` error block (JSON) | `recoverable_hint:true` | `recoverable_hint:false` | `recoverable_hint:false` **+ `recoverable_known:false`** |
| crash dump document | `recoverable_hint:true` | `recoverable_hint:false` | `recoverable_hint:false` |

The JSON field is a bool and stays one; the companion `recoverable_known`
is emitted **only** when the hint is not asserted, so its *presence* is the
signal a JSON reader keys on (its absence means "asserted"). The dump document
keeps the bare bool for the same back-compat reason: a dump is evidence and its
format is not rewritten by a later fix.

The ledger digest is where the measured defect was actually READ (`reason:` /
digest in `gopi task show`), and it is now free of the false claim.

## The ledger hint contract — what was deliberately NOT changed

`taskregister.go` appends a resume pointer to every failed row. For an
unclassified failure it **keeps the original wording**
(`autoTaskResumeHintFormat`). That pointer is generic advice and carries no
verdict; what made the two measured rows contradictory was the digest beside it
asserting `recoverable=false`. With the digest saying `recoverable=unknown`,
the row now carries ONE honest statement instead of two that disagree.
Rewording the pointer would be a ledger-contract change for *every* failure —
not what a classifier fix is for. (An earlier draft of this change added a
third hint for the unclassified case; it was removed for that reason.)

## Boundaries

- **Not the producer.** `loop.go`'s sentence is the sibling row's surface; this
  change only stops the consumer from misreading what it is given. A run that
  ends at the context fallback still says so in its own words.
- **Not a taxonomy change.** No category was added, removed or re-defined;
  `deepseek` remains the one place that decides what a failure means.
- **Not an orphan-dump change.** `BackfillOrphanCrashDumps` writes
  `recoverable_hint: true` for a dump that has no request context at all —
  untouched, and legitimately different: for an orphan the JSONL tail IS the
  evidence, so "resume can replay it" is an actual claim about an actual file,
  not a guess about an unknown error.

## Verification

- `internal/agent/crashdump_classify_test.go` — the minimal reproduction: the
  measured text (`agent timed out after 53 iterations`) through the REAL
  collection path, asserting category `network` + a non-false recoverability on
  the digest, the `session_end` block and the dump; the unclassified case
  asserting `recoverable=unknown` (and `recoverable_known:false`); a spelling
  table; and the dump-document path.
- Red proof: with the pre-fix arm restored, the two assertions fail
  (`RecoverableHint = false for "agent timed out after 53 iterations"`,
  `dump category/hint = network/false, want network/true`); restored, green.
- Run of the surface: see the task report's `## Verification` (the classifier
  exercised with the real measured text, plus the full-package budget run).
