<!-- doc-class: historical -->
# The ledger wall-clock window trap — the `'utc'` modifier, one definition, and the gate

**Branch**: `fix/ledger-wallclock-window` (worktree `/home/user/workspace/gopi-ledger-window`)
**Date**: 2026-09-17
**Package**: `internal/ledgerwindow` (+ `cmd/ledger-window`, the shell bridge)
**Rule 10 recipe**: this document is the recipe — a future report surface should read §4
before writing a single date function.

---

## 1. The trap (root cause, 5-why)

**Symptom**: a windowed ledger report returns a number that looks like a quiet
night — no error, no warning, plausible value.

```console
$ sqlite3 -readonly ~/.gopi/memory.db \
  "SELECT COUNT(*) FROM tasks WHERE claimed_at BETWEEN
     strftime('%s','2026-09-16 22:00') AND strftime('%s','2026-09-17 05:00');"
6
$ ... (same window, with the 'utc' modifier)
38
```

Same window, same data, **6 vs 38 rows** (measured 2026-09-17 06:5x CST; the
ledger is live, so the two numbers drift between runs — the mechanism does not).

**Why** — the chain:

1. The window is written as the operator reads it off a report: a wall clock,
   `'2026-09-16 22:00'`.
2. SQLite's date functions parse a bare `'YYYY-MM-DD HH:MM'` string **as UTC**.
   Nothing in the call says so; the parse is documented behaviour, not a guess.
3. The ledger's `created_at` / `claimed_at` / `ended_at` columns do **not** hold
   UTC. They hold TRUE epoch seconds as the local clock stamped them (the value
   `date +%s` reports) — that is the convention every writer follows.
4. So `strftime('%s','<wallclock>')` yields an epoch that is **the host's UTC
   offset away** from the epoch the ledger stores for that same wall clock. On
   this CST/+0800 host the offset is 28800 s:

   ```
   strftime('%s','2026-09-16 22:00')        = 1789596000   ← UTC reading
   strftime('%s','2026-09-16 22:00','utc')  = 1789567200   ← local reading (what the ledger means)
   difference                               =  -28800 s
   ```

5. The window silently moves **8 hours later**. On 2026-09-16/17 the overnight
   work sat in 22:00–05:00 local, so the bare form asked about 06:00–13:00 the
   next day and found a nearly empty interval. A wrong NUMBER with no error is
   the whole defect: it reads as "nothing happened", which is a conclusion, not
   a question.

**Why it survives review**: the bare form is *correct-looking SQL*. Nothing in
it is misspelled, no type is wrong, the query parses and runs. The only signal
is the number, and the number is exactly what a quiet period looks like.

## 2. The repro (synthetic, in-memory — never the live ledger)

`internal/ledgerwindow/window_test.go` builds an in-memory ledger with the three
epoch columns and seeds an hourly grid spanning the window plus 6 hours on each
side (rows OUTSIDE the window are what make a shifted window visibly different).
Every expected count is computed by plain Go integer arithmetic over that grid —
the test shares no code with the predicate it checks.

Measured output on this host (offset 28800 s):

```
=== RUN   TestWindowResultMatchesGoEpochs        --- PASS
=== RUN   TestBareFormShiftsTheWindowByTheHostOffset
    red-proof on this host (offset 28800 s): 'utc' form = 7 rows, bare form = 5 rows —
    the bare form's window is 8 h later than the operator meant
=== RUN   TestFixtureEpochsAreTheLocalReading
    premise on this host: bare=1789596000 local=1789567200 offset=28800 s
```

Three independent assertions, so a single "fix" cannot satisfy the test by
accident:

1. **Window result** — the helper's count equals the Go-computed count over
   `[since, until]` (and equals an independent plain-epoch `BETWEEN ? AND ?`).
2. **Shift is exactly the offset** — the modifier-less form's bound differs from
   the correct one by exactly the host's UTC offset, and its row count equals
   the Go arithmetic over the shifted window. The test SKIPS (with a stated
   reason) only on a host whose offset is zero, and does not fail there — the
   window-result assertion still holds.
3. **Premise** — `ParseBound` (Go, local zone) and the modifier-less SQL reading
   differ by the offset, i.e. the package's foundational claim is verified on
   the machine running the test rather than assumed.

## 3. The fix: one definition, in `internal/ledgerwindow`

```go
ledgerwindow.Where("claimed_at")
// claimed_at BETWEEN strftime('%s',?,'utc') AND strftime('%s',?,'utc')

ledgerwindow.WhereLiteral("claimed_at", w)  // same predicate, bounds inlined
ledgerwindow.CountInWindow(ctx, db, ledgerwindow.ColumnClaimedAt, w)  // the whole recipe
ledgerwindow.ResolveBounds(ctx, db, w)      // Bounds{Since, Until, OffsetSec}
```

* Every SQL token (`strftime`, `'%s'`, `'utc'`, `BETWEEN`, `AND`, the column and
  table names) is a named constant in `ledgerwindow.go` — referenced, never
  re-typed (Rule 1b/13/14).
* The modifier is appended by `boundExpr` from its constant, so **no caller can
  assemble the predicate and omit it**; the two call shapes (`Where` for bound
  parameters, `WhereLiteral` for surfaces that build SQL text) share that one
  builder.
* An **inverted** window and an **empty** bound are ERRORS, not empty results:
  the trap's signature is an empty result, so no other malformed window may be
  reported as one.

### Shell surfaces: `cmd/ledger-window`

A script has to turn `'YYYY-MM-DD HH:MM'` into epoch seconds before it can
compare against the columns. `cmd/ledger-window '<since>' '<until>'` (an
ordinary CLI verb on the existing binary surface — Rule 3 is untouched; compare
`cmd/text2img-verify`) prints the three fields a report wants, resolved by the
SAME predicate:

```console
$ go run ./cmd/ledger-window '2026-09-16 22:00' '2026-09-17 05:00'
1789567200	1789592400	28800
#  ^ since          ^ until          ^ host UTC offset (the shift a missing modifier costs)
```

It opens the ledger **read-only** (`file:…?mode=ro&_pragma=query_only(true)`), so
read-only audit discipline (memory #3317) holds for every caller.

## 4. The gate: `TestNoBareWallclockStrftimeInCheckedInSources`

`internal/ledgerwindow/gate_test.go` scans, every `go test`:

* every **production** Go file under `internal/` and `cmd/` (`*_test.go`
  excluded — the surfaces are production code), and
* every `.sh`/`.py` under `scripts/`, `devdoc/`, `testdata/` — the same three
  roots the sibling `/tmp` gate audits, because a one-off harness lives next to
  its finding in `devdoc/`.

It fails any call of `strftime(` or `unixepoch(` that (a) lacks the `'utc'`
modifier **and** (b) has an argument spelling a calendar date (`YYYY-MM-DD`).

**Why the second half matters.** `'now'` is an absolute instant and a `?`
placeholder is resolved at runtime; neither is a wall-clock literal being
misread. The gate spares them **because they carry no calendar date**, not
because an allowlist names them — and there is no wildcard, no whole-file
exemption, and only one escape marker (`//ledgerwindow:allow-wallclock <reason>`,
≥10 characters, on the call's line or the line above).

**Deliberately NOT covered** (so a later reader does not "complete" it into a
false-positive machine — the discipline the six standards gates record):

* `datetime()` / `date()` **formatting** of a literal — a different class; the
  ledger's epoch columns are not compared against a formatted string.
* a wall-clock string passed as a **query parameter** (`?`) — that is the
  helper's own sanctioned shape, and its correctness is pinned by §2's
  window-result test, not by a text scan.
* a bare form that is **correct on a UTC host**: undetectable by construction
  (the text is identical). The gate bans the shape; §2's test is what
  demonstrates the divergence.

Vacuity guards: ≥200 production Go files, ≥20 script files, and ≥2 date-function
call sites must be SEEN (the tree has 5 today: `strftime('%s','now')` writers in
`scripts/backfill-task-model.sh` and `scripts/repro-live-holder-worktree-veto.sh`,
plus the package's own). A scan that sees nothing fails rather than passes.

### The gate's red-proof (demonstrated, then reverted)

A probe file was added in EACH audited class — `internal/ledgerwindow/zz_probe_bare.go`
(production Go) and `devdoc/probe-bare-wallclock-window.sh` (script) — and the gate
went RED on both, naming file, line and the offending call (measured 2026-09-17
07:0x CST, output verbatim, elided to the first line of each pair):

```
$ CGO_ENABLED=0 go test ./internal/ledgerwindow/ -run TestNoBareWallclockStrftimeInCheckedInSources
--- FAIL: TestNoBareWallclockStrftimeInCheckedInSources (0.33s)
    gate_test.go:160: internal/ledgerwindow/zz_probe_bare.go:3 converts a wall-clock literal
        without the 'utc' modifier: strftime('%s','2026-09-16 22:00')
    gate_test.go:160: internal/ledgerwindow/zz_probe_bare.go:3 … '2026-09-17 05:00'
    gate_test.go:160: devdoc/probe-bare-wallclock-window.sh:3 … '2026-09-16 22:00'
    gate_test.go:160: devdoc/probe-bare-wallclock-window.sh:3 … '2026-09-17 05:00'
    gate_test.go:185: gate: 338 production Go files + 171 script files scanned, 9 date-function call site(s) seen
FAIL	github.com/nick/gopi/internal/ledgerwindow	0.354s
$ rm internal/ledgerwindow/zz_probe_bare.go devdoc/probe-bare-wallclock-window.sh
$ CGO_ENABLED=0 go test ./internal/ledgerwindow/ -run TestNoBareWallclockStrftimeInCheckedInSources
ok  	github.com/nick/gopi/internal/ledgerwindow	0.460s
```

Both probes were then deleted (verified with `git status --porcelain`, which
lists only the three deliverable paths) and the gate re-ran green — 4 flagged
sites (2 per probe: one per bound), 9 call sites seen overall. The detector also
has an in-test red-proof (`TestDetectorFlagsTheBareFormAndSparesTheCorrectOne`)
that runs on every `go test`, so the gate cannot silently stop detecting.

## 5. The live-ledger measurement (read-only, honest)

`~/.gopi/memory.db` was **read** only (`sqlite3 -readonly`, DSN `mode=ro`), never
written — the file's size and mtime were unchanged before/after (96223232 bytes,
2026-09-17 07:04). Measured 2026-09-17 07:04:44 CST, window
`2026-09-16 22:00 .. 2026-09-17 05:00`:

| column | bare form | `'utc'` form |
|---|---|---|
| `created_at` | 5 | **151** |
| `claimed_at` | 6 | **38** |
| `ended_at` | 66 | **77** |

```
$ sqlite3 -readonly ~/.gopi/memory.db \
    "SELECT strftime('%s','2026-09-16 22:00','utc') - strftime('%s','2026-09-16 22:00');"
-28800
```

`cmd/ledger-window '2026-09-16 22:00' '2026-09-17 05:00'` printed
`1789567200	1789592400	28800` — the two bounds the helper resolves plus the
offset, i.e. the shell path and the Go path agree on the same numbers.

The briefing quoted 0 vs 39; this run measured 6 vs 38 (and 5 vs 151 on
`created_at`). The difference is the live ledger (rows are claimed and written
while the report is read — memory #3317's drift warning), not the mechanism: the
shift is the host's offset in every measurement. The honest statement is
therefore "a wrong NUMBER, off by the window itself", not "always 0".

## 6. Not done / honest scope

* The gate does not cover the **textual** form of a bare wall-clock comparison
  (a window computed in Go from `time.Now().Add(-8*time.Hour)`) — no calendar
  literal appears, so there is nothing for a text scan to match. Such a call is
  a different defect class (an off-by-hand-typed offset) and is out of scope
  here.
* No existing master surface used the bare form (`grep` over production Go:
  zero date-function call sites on the ledger columns; the only scripts using
  `strftime` use `'now'`), so this change **prevents** the class rather than
  repairing a call site. `scripts/night-throughput.sh` already carries the
  `'utc'` form on its own unmerged branch (`feat/night-worker-throughput`,
  commit 12093a01) and was deliberately NOT touched.
* The package is not yet wired into `gopi task list --since…` surfaces: no
  master surface today computes a wall-clock window (the existing `--since` is a
  unix-seconds override). The next windowed report should call `CountInWindow` /
  `cmd/ledger-window` rather than re-derive the predicate.

## 7. Verification

```
CGO_ENABLED=0 go build ./...                                  → OK
CGO_ENABLED=0 go test ./internal/ledgerwindow/ -v              → 16/16 PASS
CGO_ENABLED=0 go test ./internal/standards/ ./internal/testutil/ → PASS
CGO_ENABLED=0 go test ./internal/stylegate/                    → FAIL on master too:
    identical per-rule hit counts on master and this branch (1 test-skip, 3 test-sleep,
    1 test-setenv, 2 unchecked-close, 203 ignored-error) — pre-existing, untouched
    (this branch's own t.Skip was reformed into a literal reason and left master's counts unchanged)
gate red-proof with probe files present                        → FAIL (4 sites named), reverted → PASS
live ledger, read-only: claimed_at bare=6 vs utc=38            → divergence reproduced
```

Verification note (honesty): an earlier draft of this document said "12/12 PASS";
the measured number is **16** passing tests (`grep -c '^--- PASS'` over
`go test -v`). The count was corrected here rather than left standing.
