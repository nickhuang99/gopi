<!-- doc-class: historical -->
# Task Report: crash-dump startup backfill — false-positive fix

Branch: `feat/fix-crashdump-false-positive` (worktree `~/workspace/gopi-fix-crashdump`)
Commits: `1330138e` (main fix) → `1e1a0b28` (report) → `58314968` (fix + legacy-compat) → `de58dbea` (branch tip: real-data evidence)

**Fix commits (the deliverable):**
- `1330138e` — `fix(crashdump): stop reporting live/ended sessions as crashes at startup`
- `58314968` — `fix(crashdump): accept the legacy session_id spelling in orphan-dump retraction`
- `1e1a0b28` — `docs(devdoc): crash-dump orphan false-positive report — commit hash + evidence`

## Symptom

Every `gopi` startup printed

```
[gopi] crash-dump: backfilled 14 orphan session dump(s) from previous abnormal exits
```

while most of those "crashes" were **live sibling sessions** (their JSONL was
still being appended to) or sessions that had **long since ended cleanly**.
Two independent defects produced it:

1. `DetectOrphanSessions` treated *any* session JSONL without a `session_end`
   tail as an orphan — excluding only the current process's own session. A
   concurrently running sibling has no `session_end` either (it is written at
   exit), so it was written up as a crash at each startup of any *other*
   process. 118 dumps on this machine belong to sessions whose JSONL now ends
   cleanly (measured over `~/.gopi/sessions`: 125 dumps whose session has a
   `session_end`, 118 of them still carrying the orphan marker).
2. Nothing ever **retracted** a dump. A dump is a claim ("this session died
   without an end"); once the session records its `session_end` the claim is
   false, but the file stayed forever.

## Completed

- **Live window (deliverable a).** `orphanLiveWindow = 15 * time.Minute`
  (named constant, `internal/agent/session.go`), overridable once via the
  `orphanLiveWindowSecEnv` env constant (`GOPI_ORPHAN_LIVE_WINDOW_SEC`,
  positive seconds; `0`/unset/invalid → default with the standard stderr
  warning). `DetectOrphanSessions` computes one `quietSince` cut-off and
  `orphanCheckJSONL` requires `mtime <= quietSince` **before** spending the
  `HasSessionEnd` read. An unreadable mtime (zero time) counts as LIVE — a
  stat failure must never manufacture a crash report.
  Rationale for 15 min: a live session writes an event every round, and one
  round is bounded by the bash tool timeout (120 s); 15 min is ~7× that, so
  the guard cannot hide a genuine death — after `kill -9` the mtime freezes at
  the moment of death and the next startup past the window still dumps it.
- **Reconciliation (deliverable b).** `BackfillOrphanCrashDumps` now returns
  `(written, retracted)` and runs `reconcileStaleOrphanDumps`, which walks the
  **same** tree shape (flat files + one level into each session dir) and
  removes a dump only when ALL of: it parses, `type == crashDumpDocType`,
  `orphan == true`, its category is one an orphan backfill ever wrote
  (`orphanDumpCategory` = `orphan-no-session-end`, or the
  `legacyOrphanDumpCategory` = `crash` spelling the pre-retraction dumps
  carry), its `session_id` matches the file name, the session is not
  excluded, and the sibling JSONL **now** has a `session_end`. Everything
  else — an error-path dump from `WriteCrashDump`, a hand-written or
  unparseable file, a dump naming another session — is left alone, so a
  malformed reader can never wipe evidence.
  Two legacy quirks of the on-disk backlog are accepted by the READER (never
  by rewriting files): category `crash` (`legacyOrphanDumpCategory`) and a
  `session_id` that carries the `.jsonl` suffix (verified: all 118 stale
  dumps have it — strict equality would have cleared none of them).
- **Stderr wording (deliverable c).** Now
  `[gopi] crash-dump: backfilled N session dump(s) with no recorded end, retracted M stale dump(s)`
  — the line no longer implies crashes, and it reports both directions.
- **Constants (Rules 1/13/14).** All new numbers/strings are named:
  `orphanLiveWindow`, `orphanLiveWindowSecEnv`, `sessionJSONLSuffix`,
  `bareSessionLogName`, `crashDumpDocType`, `orphanDumpCategory`,
  `legacyOrphanDumpCategory`; the new `envDurationSeconds` funnel in
  `envparse.go` is the single env reader (Rule 2). `session.go`'s
  `.jsonl` literals were backfilled onto `sessionJSONLSuffix` where the same
  file already needed one.
- **No format rewrite (constraint).** Existing dump files are untouched and
  still parse; the legacy category is handled by the *reader* accepting both
  spellings, never by rewriting dumps on disk. The writer emits only
  `orphanDumpCategory`.
- **True-crash visibility preserved (constraint).** A `kill -9`/panic session
  stops writing at the moment of death, so it stays dumped (and is never
  retracted) — pinned by a test and by the real-binary check below.

## Not Done (honest scope)

- The dump filename is derived from `strings.TrimSuffix(..., ".jsonl")`, so a
  hypothetical `session.jsonl` and `<uuid>.jsonl` living in the SAME session
  directory would collide on `session.dump.json`. Today that cannot happen
  (bare sessions and task runs use different directory shapes) and it is a
  pre-existing naming family, so it is left as-is rather than redesigned here.
- `reconcileStaleOrphanDumps` contains its `os.Remove` via a seam
  (`osRemove`, like the existing `osWriteFile`); no test deletes real files.
- The `HasSessionEnd` full-file read (`os.ReadFile`) is still the cost centre
  of the sweep (a known, separately-tracked improvement: read the tail with
  `os.Open`+`ReadAt`). This change slightly *reduces* the cost (the live-window
  check short-circuits before the read for fresh files) but does not fix it.
- `go test ./cmd/agent/` fails `TestCLIContractInventoryMatchesDispatch`
  (dispatch predicate count 26 vs contract suite 25). Verified pre-existing:
  the same failure reproduces on `master` (commit `79d6fe52`) and is
  unrelated to this change.

## Verification

Build:

```
$ go build ./...          → exit 0
$ gofmt -l internal/agent cmd/agent  → (empty)
$ go vet ./internal/agent/ ./cmd/agent/  → clean
```

Targeted tests (the epic's named set, plus the new file):

```
$ go test ./internal/agent/ -run 'CrashDump|Orphan|Backfill|HasSessionEnd' -v
--- PASS: TestBackfill_OrphanLiveWindowAndReconcile      (i)+(ii)+(iii) of the acceptance
--- PASS: TestBackfill_TrueKill9StaysVisible             kill -9 dump survives repeated startups
--- PASS: TestBackfill_ReconcileSparesForeignDumps       error-path / hand-written / mismatched / unparseable dumps survive
--- PASS: TestBackfill_ReconcileClearsLegacyOrphanDumps  the ~117 legacy false positives are cleared
--- PASS: TestBackfill_ReconcileSparesOwnLiveSession     exclusion is symmetric across write and retract
--- PASS: TestOrphanLiveWindowEffective                  default / override / 0 / invalid
--- PASS: TestDetectOrphanSessions / TestDetectOrphanSessionsTornTail / TestHasSessionEnd* (existing, updated for the window)
ok  github.com/nick/gopi/internal/agent
```

Whole package + gates:

```
$ go test ./internal/agent/ -count=1        → ok (86.6s)
$ go test ./internal/standards/ -count=1    → ok (4.2s)   [Rule 16/17/18/19/20/21 gates green]
$ go test ./internal/stylegate/             → FAIL, pre-existing on master (voice-go cmd/agent/cli_voice*.go violations, unrelated files)
$ go test ./cmd/agent/                      → FAIL, pre-existing on master (TestCLIContractInventoryMatchesDispatch)
```

Real-binary end-to-end (the acceptance's user-visible claim — the startup line
and the actual dumps, isolated `--home`):

```
--- before: dead-1.jsonl legacy-1.dump.json legacy-1.jsonl live-1.jsonl
[gopi] crash-dump: backfilled 1 session dump(s) with no recorded end, retracted 1 stale dump(s)
OK: live session (mtime now, no session_end) not dumped
OK: dead session (mtime −2h, no session_end) dumped
OK: legacy orphan dump whose session recorded session_end retracted
dead-1.dump.json: {"category":"orphan-no-session-end","orphan":true,"session_id":"dead-1", ...}
```

Real-DATA end-to-end (25 of the ACTUAL stale pairs copied out of
`~/.gopi/sessions` — every one carrying both legacy quirks, category `crash`
and `session_id` with the `.jsonl` suffix — plus the real binary, isolated
`--home`):

```
seeded 25; before=25
[gopi] crash-dump: backfilled 0 session dump(s) with no recorded end, retracted 25 stale dump(s)
after=1        # the lone survivor is THIS run's own error-path dump (category "vendor"), untouched
```

That is the acceptance's "~117 legacy false positives" claim demonstrated on
real data (25/25 sampled pairs cleared; the machine-wide count is 118).

## Files

- `internal/agent/session.go` — live-window constants, mtime guard in
  `orphanCheckJSONL`, `orphanLiveWindowEffective`, suffix constants.
- `internal/agent/crashdump.go` — `(written, retracted)` return,
  `reconcileStaleOrphanDumps`/`reconcileOrphanDump`/`isOrphanDumpCategory`,
  dump-doc constants, `osReadFile`/`osRemove` seams.
- `internal/agent/envparse.go` — `envDurationSeconds` funnel.
- `cmd/agent/main.go` — reworded startup line.
- `internal/agent/crashdump_orphanwindow_test.go` — the new regression suite.
- `internal/agent/crashdump_test.go`, `internal/agent/sessionend_tail_test.go`
  — updated for the window / new return values.

## Independent re-verification (branch tip `c13f4921`)

Re-run from the worktree after the report was written, on the acceptance's own
check list:

```
$ go build ./...                                          → exit 0
$ go test ./internal/agent/ -run 'CrashDump|Orphan|Backfill' -count=1 -v
    TestBackfill_OrphanLiveWindowAndReconcile      PASS   # (i)(ii)(iii)
    TestBackfill_TrueKill9StaysVisible             PASS
    TestBackfill_ReconcileSparesForeignDumps       PASS
    TestBackfill_ReconcileClearsLegacyOrphanDumps  PASS
    TestBackfill_ReconcileSparesOwnLiveSession     PASS
    TestOrphanLiveWindowEffective                  PASS
    TestDetectOrphanSessions / …TornTail           PASS
  → ok  github.com/nick/gopi/internal/agent
$ go test ./internal/standards/ -count=1                  → ok (Rule 16–21 gates)
```

Real binary on an isolated `--home` (the user-visible claim), one fixture set
carrying all three acceptance cases:

```
before:  dead-1.jsonl  legacy-1.dump.json  legacy-1.jsonl  live-1.jsonl
$ echo hi | HOME=$D bin/gopi --home $D
[gopi] crash-dump: backfilled 1 session dump(s) with no recorded end, retracted 1 stale dump(s)
after:   dead-1.dump.json  dead-1.jsonl  legacy-1.jsonl  live-1.jsonl
```

- (i) `live-1.jsonl` (mtime now, no `session_end`) → **no dump** written.
- (ii) `dead-1.jsonl` (mtime −2 h, no `session_end`) → **`dead-1.dump.json`**,
  `orphan: true`, `category: orphan-no-session-end`.
- (iii) `legacy-1.jsonl` (+`session_end`) with a legacy dump (`category: crash`,
  `session_id: "legacy-1.jsonl"`) → the dump is **retracted**; the live
  session's own dumps were untouched.

Branch tip after this note: `git log --oneline -1`.
