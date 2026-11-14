<!-- doc-class: historical -->
# The suite's own tree, moved under it: naming the freshness refusal's cause

**Task:** 01a13354-4f6b-7c70-a0a4-84b28cd18d72 (ported from 01a132e2-6ad8-76c6-a6dd-b26d8149b022,
whose branch was deleted and preserved as tag `rework-backup/01a132e26ad876c6`)
· **Measured at:** 2026-10-13 (the incident) / 2026-10-14 (this port)
**Related:** `devdoc/test-budget-failure-bodies.md` (the same run's earlier fix, task 01a1329b),
`cmd/agent/task_session_cwd_upstream.md`.

## The report that started this

`make test-budget PKGS='./cmd/agent ./internal/agent'` on task 01a12c66's tree
(run `run-1791849386-333342`) came back with 15 failing tests. Twelve of them read:

```
<queue>/run-1791849386-333342-tested-bin/gopi (mtime 2026-10-13T07:56:32+08:00) is OLDER than
/home/user/workspace/gopi-01a12c66f748786784b95d912571cb84/internal/agent/clock.go
(mtime 2026-10-13T07:58:50+08:00) — this test drives the BUILT binary, so it would verify code
that is not in that binary. Rebuild it (CGO_ENABLED=0 go build -o <…> ./cmd/agent/) and re-run.
```

Two mtimes, one message, and a reader could not tell whether the binary was stale or
something else was going on. That ambiguity is the defect this change fixes.

## Root cause: the same run's own red proof wrote the tree it was testing

| time | event | evidence |
|---|---|---|
| 07:53:54.98 | the run starts a BACKGROUND gate wrapper | session jsonl `e2e89775…`, record 462 (`job` action=start) |
| 07:56:26 | the runner's own start record: `start_iso=2026-10-13T07:56:26+08:00`, host=peerbox, pid=333342 | `queue/test-budget/run-1791849386-333342-start.txt` |
| 07:56:32.037 | the runner builds the tested binary (ONCE, by design) | `…-tested-bin/gopi` mtime `07:56:32` |
| **07:56:33.62** | **the run's main loop executes an INLINE red proof**: `sed -i 's/stamp := a.turnStampForRequest()/stamp := a.turnStampLine()/' internal/agent/clock.go` | record 556 |
| 07:58:49.81 | the restore: `git checkout HEAD -- internal/agent/clock.go` | record 561 |
| 07:58:12 → 08:17 | the refusals, each naming **clock.go mtime 07:58:50** | the stream's 12 refusal rows |

The two mtimes in the message are, to the second, the `sed -i` and the `git checkout` — the
binary really was built at 07:56:32, the file really was newer, and the run really had made it
newer **itself, after the build**.

Three things were true at once and the guard's message asserted only one:

1. the verdict was **correct** (mtime is what it can see, and an older binary must not be used
   to verify a tree);
2. its **advice** ("rebuild and re-run") was wrong for this situation — rebuilding while the
   writer is still in the run reproduces the refusal;
3. the runner's **summary named neither** (the body-quoting half was fixed separately in
   01a1329b; `devdoc/test-budget-failure-bodies.md`).

## The fix: keep the verdict, name the cause — by measurement, not by guessing

The refusal is still composed where the mtime comparison is: **on this master that is
`cmd/agent/gopibin_identity_guard_test.go`'s `decideBinFreshnessByMtime`** (the artifact's
build identity, its content digest, and mtime-as-last-resort are one decision there). Its
evidence arm is `cmd/agent/task_session_cwd_freshness_evidence_test.go`.

`assertGopiBinIsFresh` refuses under exactly the same condition (newest production source
mtime > binary mtime) and still prints the two mtimes, the sentence `… this test drives the
BUILT binary …` and the rebuild command. The refusal now ALSO reports:

1. **the tree's HEAD** — `git rev-parse HEAD` (`cwdProbeTreeHead`), and the tree's own records
   of which newer sources differ from it;
2. **the tested binary's own build revision** — `debug/buildinfo.ReadFile(bin)` (stdlib, zero
   new dependencies) reading `internal/version.BuildInfoKeyRevision` / `BuildInfoKeyModified`;
3. **every production source newer than the binary**, up to `cwdProbeFreshnessSourcesReported`,
   each with its mtime **at scan**;
4. **a VERDICT** — which of the causes this is:

- `cwdProbeFreshnessVerdictMovedTree` — every newer source is **byte-for-byte what HEAD
  holds**, no source moved during the comparison, and HEAD's commit time is not after the
  binary's build time. No content the binary compiles has changed; only an mtime moved. This
  is what a mutate-and-restore red proof leaves behind, and **rebuilding cannot fix it** — the
  remedy line says so instead of repeating "rebuild and re-run".
- `cwdProbeFreshnessVerdictBinPredates` — a newer source **differs from HEAD**, or HEAD was
  committed after the binary was built. The original condition, and the original advice is
  right.
- `cwdProbeFreshnessVerdictLiveWriter` — a source's **stamp moved between the two readings**:
  its mtime and/or its size differs between the scan and the moment the refusal is composed.
  A writer is live in this run right now. This is the only signal that survives a
  mutate-and-restore, and it is the incident's own shape.
- `cwdProbeFreshnessVerdictUndetermined` — the tree's records could not be read (no git, an
  unreadable artifact). Printed INSTEAD of a cause, never guessed (Rule 15).

### Why the evidence takes TWO readings, and what each witness can and cannot see

The two causes are not separable by any single witness, and saying so is the point:

- an **mtime** cannot tell an identical-bytes rewrite from a real edit — a RESTORE moves one
  exactly as an edit does, and that is this incident verbatim (clock.go rewritten at
  07:56:33.6, restored at 07:58:49.8);
- the tree's **content record** cannot either, on its own: a writer that edits AND RESTORES
  leaves the tree byte-identical to HEAD, so by the time the refusal is composed there is
  nothing left for git to see.

So every production source newer than the binary is stamped **twice** — once where the guard
compares (`freshnessTreeReading`), once as the refusal is composed (`describe`) — and a stamp
that moved means a live writer. Stable stamps plus a source differing from HEAD is the other
cause. Stable stamps plus nothing differing plus HEAD not newer than the build is the moved
tree. The stamp is `(mtime at nanosecond precision, size)`: mtime so a rewrite in place cannot
hide behind a coarse clock, size as the second, independent property — a mutate-and-restore is
free to land on the same mtime granularity, but not on the same byte count as a coincidence.

The content question is asked with `git diff --numstat HEAD -- <paths>` over exactly the newer
paths (content, never a timestamp), and HEAD's commit time with `git log -1 --format=%cI`,
compared at **second** granularity — the granularity git records it in; sub-second precision
the record does not have must not decide a cause.

The production-source walk is ONE function (`cwdProbeProductionSources`, newest-first), and
`cwdProbeNewestProductionSource` is a read of it, so the refusal's source list and the guard's
decision cannot drift apart.

### `scripts/test-budget.sh` + `scripts/lib/stream-failures.sh` — the summary names it

The runner builds the tested binary **once**, before the suite, and hands that one artifact to
every package (`tested_bin_dir`). It is the only party that knows that design, and the only
party that can say "these failures are mine". When the stream carries the refusal, the failures
block is followed by:

```
--- STALE-TESTED-BINARY REFUSALS (the failing test never ran a test body) ---
  N distinct refusal(s) in this stream — that many failing test(s) never ran a test body.
  tested binary: <…-tested-bin/gopi> (mtime <the build>)
  runner's design: this script builds the tested binary ONCE, before the suite starts, …
  the guard's own verdict(s), quoted from the stream:
      the tree MOVED while the suite ran — …
  Read it as: N failing test(s) are NOT a verdict about this tree's code …
  refusal(s), verbatim:
      <the refusal text>
full stream: <path>
```

The runner **classifies nothing itself**: the VERDICT lines are relayed verbatim from the guard
(`gopi_stream_freshness_verdicts`), because a second classifier would be a second definition of
a value the guard owns.

The library gained one reader over the existing `test-output` normalisation —
`gopi_stream_freshness_rows` — which requires BOTH the marker in the row's text AND that the
row belongs to a test the stream recorded as **failing** (`gopi_stream_failures`). The second
condition is not decoration: this repository contains tests that assert on the refusal text, so
a PASSING test can print it, and without the condition the summary would report the runner's own
refusals from a string somebody was merely quoting.

## The two verdicts, measured on a real tree — not only on a fixture

A verdict that has never selected the wrong branch is not evidence that it can select the right
one. The arms in `task_session_cwd_freshness_evidence_test.go` build a real git-backed fixture
in four states and read the refusal the guard itself composes:

| fixture state | what was done to the production source | the guard's verdict |
|---|---|---|
| content identical to HEAD, mtime newer than the binary, HEAD committed BEFORE the build | `touch` only — an **mtime round-trip**, content untouched | **the tree MOVED** (and no "rebuild" remedy) |
| content **edited** after the build (differs from HEAD) | a real uncommitted edit | **the binary PREDATES** |
| a source rewritten **between the two readings**, content put back | the writer is live | **a source was WRITTEN while this comparison ran** |
| no git at all | the records cannot be read | **the cause could not be determined** (no cause picked) |

The fixture's commit is stamped 10 minutes in the past (`GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE`)
so the geometry is deterministic instead of racing the clock — the build is newer than HEAD's
commit for a reason that IS the case under test.

## What `built from:` says on this project's own path — and why it is not a gap

On a **linked worktree** the answer is the named absence (the `versionStampSkippedCause` the CLI
already prints, reused rather than retyped), because cmd/go recognizes a repository only by a
`.git` **directory** and a worktree's `.git` is a FILE. The instrument is still the right one
and still reports: it reads the ARTIFACT (stdlib `debug/buildinfo`, zero new dependencies), it
is exercised against a real binary (`TestFreshnessBinRevisionReadsTheArtifact`, a `go build` in
a temp module), and it is the only thing that can answer this question when `root` is not the
tree the binary was built in — the case that matters is precisely the one where the two
disagree.

## Why the marker exists twice, and how that cannot drift

The marker `drives the BUILT binary` is composed in Go (`cwdProbeFreshnessMarker`) and
recognised in shell (`stream_freshness_marker`). A shell literal beside a Go literal drifts
silently — the summary would simply go quiet about a condition it used to name. So
`cmd/agent/task_session_cwd_freshness_evidence_test.go::TestFreshnessMarkerSpellingsAgree` reads
BOTH files and fails when the values differ, and fails when the Go constant is not interpolated
into a message (a declared-but-unused constant would be a claim about a sentence nobody prints).

The drift arm lives in `cmd/agent` rather than in `internal/testutil` (where the handed-over
branch had it) because the refusal is composed by `gopibin_identity_guard_test.go` — both halves
of the pair are held by the same package, and `cmd/agent` has no importable path into
`internal/testutil`'s fixtures.

## How to read this in a future run (the operator's shortcut)

- Several failures at ~0.01s (one at 0s) + `is OLDER than` ⇒ **0 test bodies ran**; that count
  is not a code verdict.
- Check the block's VERDICT line: **MOVED** ⇒ look for a red proof / editor save in the same
  run (the session jsonl's own tool records carry the `sed -i` and the `git checkout HEAD --`);
  **PREDATES** ⇒ rebuild; **WRITTEN WHILE THIS COMPARISON RAN** ⇒ a writer is still active,
  rebuild once it stops.
- The rule that avoids the whole class: do the red proof FIRST, restore it by an explicit
  revision, confirm `find . -name '*.go' -newer <binary> -not -path './bin/*'` prints nothing,
  and only then start the suite.

## Files

- `cmd/agent/gopibin_identity_guard_test.go` — the mtime arm's refusal now calls the evidence
  arm (`freshnessRefusalEvidence`).
- `cmd/agent/task_session_cwd_freshness_evidence_test.go` — the vocabulary, the two readings,
  the composer, the drift gate and the four red/green arms.
- `cmd/agent/task_session_cwd_e2e_test.go` — the production-source walk, now one function,
  stamping each source it returns.
- `scripts/lib/stream-failures.sh` — the freshness readers over the stream.
- `scripts/test-budget.sh` — the summary block and its constants.
- `internal/testutil/testbudget_freshness_gate_test.go` — the shell half: the synthetic stream,
  and the real script against a stub `go`.
