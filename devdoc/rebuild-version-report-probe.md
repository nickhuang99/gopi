<!-- doc-class: historical -->
# The post-merge rebuild auto-reverted every tagged merge

Epic: `merge verify rework: feat/peak-hour-guard` (task
`01a098b0-ce97-7a30-9802-68c414184d74`, rework of the reverted merge
`bd42e827`). Branch: `feat/peak-hour-guard`, worktree
`/home/user/workspace/gopi-peak-rework`.

## Symptom

Every merge round that reached the rebuild step was unwound:

```
[merger] [rebuild] FAILED: /home/user/work/GoPi cmd="CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"
    exit=0 err=version mismatch: expected faf5f219, binary reports
    "v20260913-1244-faf5f219 (faf5f2190e81)\nrevision: faf5f2190e8176d5c3d8b18562df696c427247c2\ntag:      version/v20260913-1244-faf5f219"
[merger] [rebuild] REVERTED: master restored to pre-merge buildable tip ...
```

`~/.gopi/logs/merger.log`: 15 × `[rebuild] FAILED … version mismatch` / 13 ×
`REVERTED: master restored` (incl. the round landing `18f61785`). The
early rounds had a merger-sweep snapshot and were auto-reverted; the last two
rounds lost the snapshot and were merely *refused* (`no auto-revert (no
merger-sweep snapshot) — merge left as-is`), which is why master's tip today is
a merge commit the rebuild rejected.

**Every reported revision equals the expected tip** (`expected faf5f219` vs
`(faf5f2190e81)` / `revision: faf5f2190e81…`) — the build is correct and the
*assertion about it* is broken. The merger mints a `version/<tag>` **before**
the rebuild (Rule 12's close loop), so the binary is tagged on every round:
this is not branch-specific — `feat/peak-hour-guard` was collateral, not the
cause. (Recorded cause of THIS epic's first revert, cycle 2 `d70dec88` →
`bd42e827`, was the resolved merge's `sync/atomic` import gap + a stale
Rule-16 permit — both were cured on master independently by `dc4c1752` and
`86467b49`/`88c2c454`.)

## Root cause (5-why)

1. The rebuild's version assertion failed although the artifact reported the
   right revision.
2. `binaryVersion` handed the probe a string the parser rejects:
   `revision, dirty, _ = gopiversion.RevisionOf(display)` where
   `display` is the **whole** `sh -c '"$0" --version'` output.
3. `RevisionOf` is the **single-line display** parser. Given the three-line
   report it falls into the legacy branch (`body` = the whole multi-line
   string), `isRevision` fails on it, `ok=false` → `revision ""`.
   The reports show `RevisionOf` already handles `\n` specially, proving the
   multi-line shape was known and the **report** parser was the intended reader.
4. `versionMatches(tip, "")` → false → `err=version mismatch` → `REVERTED`.
5. Why the shape changed under the probe: `bin/gopi --version` answers with a
   REPORT (display + `revision:` / `tag:` / `commits since tag:`) **whenever the
   build carries an identity** (`internal/version.Info.Description`), and the
   merger mints a tag immediately before every rebuild — so the failing shape is
   the *normal* shape, not an edge case.

## Why this is on THIS branch's path

`versionMatches` on current master already calls
`gopiversion.RevisionOfDisplay` ("feat(version): name the merged position",
`22951fe8`, an ancestor of master) — but master's `RevisionOfDisplay` delegates
to the single-line `RevisionOf` **first**, so it inherits the defect. The
producer already names the line (`internal/version/version.go`, the
`revision:` key) and the consumer greps for it. The fix is to make the reader
read the report it is handed.

Crucially, this failure mode sits **on the branch-rework path**: the merger's
rebuild rejects every round, so *no* branch can land — including this rework.
Fixing it here is what makes this branch mergeable at all.

## Fix

- `internal/version.RevisionOfDisplay` reads a whole report: the **first line
  that yields a revision wins** (display first, so it still outranks the fact
  lines), the report's key is the new const `reportRevisionLine`
  (single owner of the spelling; the `Description` producer and this reader
  cannot drift — Rule 13).
- `internal/tools.binaryVersion` uses that reader and drops the revision of a
  report that says it was built from a dirty tree (new `reportIsDirty()`),
  preserving the historical reject while `versionMatches` stays a
  revision-only comparison.

Regression guards: `TestRevisionOfDisplayReadsAWholeReport`
(4 report shapes round-trip + 6 negative shapes),
`TestRebuildVerifiesMultiLineVersionReport`,
`TestRebuildRejectsDirtyMultiLineReport`.

## Verification

| check | result |
| --- | --- |
| RED/GREEN | the new tests fail (`"", false`) against the pre-fix `version.go` and pass with it — the live merger's exact error |
| live probe on the real repo (`RebuildAfterMerge("/home/user/work/GoPi")`, real settings + declared build) | `OK=true Exit=0 Actual="faf5f2190e81" Expected="faf5f219"` — the exact assertion the merger failed |
| `CGO_ENABLED=0 go build ./...` | exit 0 |
| `gofmt -l cmd internal` | empty |
| `go test ./internal/{version,peakhours,config}` | ok |
| `go test ./internal/standards/` | ok (the `cmd/agent/main.go:982` permit still matches its one site) |
| `go test -count=1 ./...` | see the task report |

The remaining whole-tree failures are pre-existing on pristine master and
identical on branch and master (cycle-2 differential: same 3 packages, same
test names, zero normalized delta) — they are the host worker-env leak
(`GOPI_WORKER_DAEMON_CHILD`/`GOPI_DAEMON_READY_FD` inherited by an exec'd
child) and doc-content gates on files this branch never touches.

## Residual half (fixed on `fix/version-dirty-witness`, 2026-09-14)

The report fix above made the probe **report-aware** but left TWO dirty
predicates that disagreed about the same string:

- `versionMatches` rejected a binary only when the whole display string ended
  in `-dirty` (`strings.HasSuffix`) — a **tail-only** check;
- the producer's dirty TAGGED display decorates the NAME, so the marker sits
  in the MIDDLE and the string still ends in `)`:

  ```
  v20260918-0932-8380ce9caeef-dirty (8380ce9caeef)
  ```

  A real dirty build of a tagged commit therefore read as CLEAN and was
  accepted as the committed tip — the dirty witness was silently defeated
  exactly when it mattered (a tag is minted before every rebuild, Rule 12).
- The negative test that was supposed to guard this appended `-dirty` to the
  whole string — a shape `Info.Display()` never emits — so it passed while the
  hole was open (Rule 10.4: verify the producer's shape, not a fabricated one).

**Fix** (`internal/version.IsDirty`, single owner — Rule 13/14): scan EVERY
line for `DirtySuffix` and honour the report's prose witness
(`working tree: dirty …`); `MatchesRevision` and the probe's
`versionMatches` both delegate to it, so no consumer re-derives the predicate
from a guessed string tail. `reportIsDirty` in `internal/tools` is deleted (its
comment says so, to stop a second copy growing back).

**Red-proof on the probe's own API** (`internal/tools/rebuild_versionprobe_test.go`
carried to the pre-fix tip `9c01541e`):
`TestVersionMatchesReadsMultiLineReport` FAILS there —
`versionMatches(report, tip) = false; the report names the tip` — the exact
false mismatch that auto-reverted 8 rounds on 2026-09-13; it PASSES at the
fix tip. `TestProducerDirtyTaggedDisplayNeverMatchesTip` fails on master
(`versionMatches(producerDirtyDisplay, tip) = true`) and passes on the branch.

**Live proof** (`/home/user/workspace/vp-live-382361`, real repo + declared
build, real `gopi merger`): pre-fix binary → `[rebuild] FAILED … error: version
mismatch: expected 09a6fc78, binary reports "v20260914-1258-09a6fc78
(09a6fc78c125)\nrevision: …"` + `[rebuild] REVERTED: master restored to
dcce3a86`; fixed binary on the SAME landing → `[rebuild] ok: … already current
at v20260914-1303-09a6fc78 (09a6fc78c125)` — no REVERTED, master advanced.
