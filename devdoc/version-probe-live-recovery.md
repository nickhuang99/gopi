<!-- doc-class: historical -->
# The version-probe fix on a fresh branch — and the stale daemon that kept it broken

Task: post-merge rebuild auto-revert (fresh re-derivation + live redeploy).
Branch: `fix/version-probe-revision-of-display`, worktree
`/dev/shm/gopi-test-scratch/gopi-verprobe-live`, forked from master `faf5f219`
(Rule 23 — never commit on someone else's branch;
`feat/peak-hour-guard` was read as evidence only).

## Symptom (evidence, before)

`~/.gopi/logs/merger.log`: **18 × `version mismatch`** and
**14 × `REVERTED: master restored`**. Every failure line names a binary whose
report agrees with the expected tip:

```
[merger] [rebuild] FAILED: … err=version mismatch: expected faf5f219, binary reports
  "v20260913-1244-faf5f219 (faf5f2190e81)\nrevision: faf5f2190e8176d5c3d8b18562df696c427247c2\ntag:      version/v20260913-1244-faf5f219"
```

The build was correct; the **assertion about it** was broken.

## Root cause (5-why)

1. The rebuild's version assertion failed although the artifact reported the
   right revision.
2. `binaryVersion` handed the probe a string the parser rejected:
   `revision, dirty, _ = gopiversion.RevisionOf(display)` where `display` is
   the **whole** `--version` output (master `faf5f219`,
   `internal/tools/rebuild.go:306-307`).
3. `RevisionOf` is the **single-line display** parser: given a multi-line
   report it treats the blob as one line, `isRevision` fails → `ok=false` →
   `revision ""`.
4. `versionMatches(expected, "")` → false → `err=version mismatch` →
   `REVERTED: master restored`.
5. Why the failing shape is the *normal* shape: the merger mints a
   `version/<tag>` immediately **before** every rebuild (Rule 12's close
   loop), and `--version` answers with the multi-line REPORT whenever the
   build carries an identity (`internal/version.Info.Description`).

**Second, independent half:** the resident merger was an in-memory build from
before the fix, so it kept applying the broken in-process probe regardless of
master's source — fixing the source alone would not have stopped the reverts.

## What this branch delivers

Master landed `feat/peak-hour-guard` (`39ff51bc` + `d0408eae`) **mid-task** —
the merger finally pushed that branch through — so an equivalent probe fix is
now on master too. This branch was still re-derived from `faf5f219` and adds:

1. **One exported owner for the report keys (Rules 1b/13/14).**
   `internal/version.ReportRevisionKey` / `ReportTagKey` are written by the
   producer (`Info.Description`) and stripped by the reader
   (`RevisionOfDisplay`) — master keeps an unexported `reportRevisionLine` for
   the reader while `Description` still types `"\nrevision: "` inline, i.e. the
   producer/reader pair can drift.
2. **The real report strings replayed as the regression (Rule 15a).**
   `internal/version/version_report_probe_test.go` feeds the EXACT three-line
   report from merger.log, and covers the zero-value cases (empty report,
   empty revision fact line, prose, dirty) plus the wrong-reader half: the
   single-line `RevisionOf` must stay UNABLE to read a report (if it ever
   becomes able, the failure mode changed and the test says so).
3. **Exhaustive guard (Rule 13 shape).**
   `TestRevisionOfHasNoProductionCallersOutsideItsDefinition` scans every
   production `.go` file and fails if `.RevisionOf(` appears anywhere outside
   its own definition file — the re-inline cannot return unnoticed.
4. **A rebuild-level fixture** (`TestRebuildAcceptsMultiLineVersionReport`)
   whose `--version` prints the real 3-line report naming its own repo's tip.

## Verification

| check | result |
| --- | --- |
| RED-PROOF (master `faf5f219` + the new tools tests) | `TestRebuildAcceptsMultiLineVersionReport` FAILS with the live error `Err="version mismatch: expected 803429a, binary reports \"v…(803429a20cba)\\nrevision: 803429a20cbab7fc7e75a1e8def998f911f1a5ef\\ntag: …"`; `TestVersionMatchesReadsMultiLineReport` FAILS; `TestRevisionOfHasNoProductionCallersOutsideItsDefinition` FAILS (master's `rebuild.go:307` still calls `.RevisionOf(`) |
| RED-PROOF on master, reader half | probe against master's `RevisionOfDisplay(real 3-line report)` = `("", false)` → `versionMatches(expected, "")` = false → auto-revert |
| GREEN-PROOF (this branch, same probe) | `RevisionOfDisplay(real 3-line report)` = `("faf5f2190e81", true)` — the assertion flips to true |
| `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | BUILD_OK |
| `go test ./internal/version/ ./internal/standards/ ./internal/stylegate/ ./internal/tools/ -count=1` | `ok` — version 1.141s, standards 7.662s, stylegate 10.159s, tools 116.871s (no new baseline permits needed; gates green) |
| `gofmt -l internal/version internal/tools` | empty |
| Ladder up from the wrong tip | `versionMatches(<other report>, faf5f219)` = false — no blanket always-true |

## Live recovery (the second half)

1. **The deployed artifact was stale**: `bin/gopi --version` reported
   `faf5f2190e81` while master was `d0408eae` — a restart at that moment would
   have reloaded the **broken** probe.
2. Rebuilt the deployed artifact at master:
   `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in `/home/user/work/GoPi`,
   then `./bin/gopi --version` →
   `v20260913-1244-faf5f219+16 (d0408eae6f48)\nrevision: d0408eae6f4829fd7111e1f52983a14ca9b4bd79\ntag: version/v20260913-1244-faf5f219` —
   the report's revision equals master's tip `d0408eae`.
3. Deliberate restart (never a blind kill): `gopi merger --restart --wait 5m`
   — graceful stop of the in-flight pass, release the lock, fresh daemon
   re-execs the CURRENT binary. Old daemon pid 3274318
   (`/proc/3274318/exe` = `bin/gopi (deleted)`, started 16:30:20, holder of
   `~/.gopi/serve.lock`) → new daemon.
4. Post-restart, MEASURED: the old pid 3274318 finished its in-flight pass
   (the 13:10 round) and **that pass reverted one more time** — counters went
   to `19 × version mismatch` / `15 × REVERTED: master restored` and master
   went back to `faf5f219`; the daemon then exited gracefully (1m03s after the
   request; 27 passes).
5. The new daemon — pid **558723**, `/proc/558723/exe` = `bin/gopi`, holder of
   `~/.gopi/serve.lock` — wrote a fresh `pass start`, re-landed the same
   branches, minted `version/v20260913-1319-e51b554c`, ran the rebuild, and
   **the counters did not move (still 19/15)**: no `version mismatch`, no
   revert. Master stayed at the new position and the deployed artifact
   followed it:

   ```
   $ ./bin/gopi --version
   v20260913-1319-e51b554c (e51b554c1660)
   revision: e51b554c16605a25f08f742af24f47b48f8888ed
   tag:      version/v20260913-1319-e51b554c
   $ git rev-parse --short HEAD      # master
   e51b554c
   ```

   The report's revision equals the master tip that round landed — the exact
   assertion the old daemon failed 19 times. The round ended `sleeping 5m0s`
   with no unwind.

Ordering mattered: rebuilding `bin/gopi` at master BEFORE the restart is what
made the fresh daemon load the fixed probe — a restart first would have
re-armed the old one.

## Not done / caveats

- The stale-daemon half is a **process fact, not a code fix**: nothing in the
  repo forces the resident merger to re-exec after its own binary changes.
  `gopi merger --restart` is the operator's idiom; this report records the
  measured pre/post state rather than leaving it implicit.
- `cmd/agent/out.txt` (untracked, dated 19:33 before this session) remains in
  the main repo — pre-existing, not this task's artifact.
- Whole-tree `go test ./...` was not run on this branch; the targeted set
  (version, standards, stylegate, tools) is the acceptance set, and the
  host-known red packages (`cmd/agent`, `internal/agent`, `internal/testutil`
  worker-env leak) are pre-existing on pristine master.
