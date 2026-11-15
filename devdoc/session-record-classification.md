<!-- doc-class: normative -->
# Session-tree record classification: one shared home in production

## Why

A session DIRECTORY legitimately holds more than its session log. Since the
signal-provenance epic, `internal/signalrec` files its record beside the
session it describes:

```
<gopi dir>/sessions/<uuid>/session.jsonl              the session RECORD
<gopi dir>/sessions/<uuid>/signal-provenance.jsonl    a SIDE FILE of it
```

Every reader in the repo discovered that tree by globbing `*.jsonl` and
counting hits, which silently equated "a JSONL file" with "a session". The
first casualty was fixed on the test side (commit 612a015d,
`internal/testutil/session.go`): `TestWebToolsSignalShutdownWritesSessionEnd`
counted 2 sessions for 1 run. The production readers kept the glob — correct
only for as long as no reader ever had to *reject* a JSONL.

## What changed

### The shared home — `internal/sessionlayout`

A new leaf package (stdlib only: `os`, `path/filepath`, `strings`; zero
project imports) that owns the layout key:

| Layout | Path | Classification |
|---|---|---|
| legacy flat | `<root>/<uuid>.jsonl` | record (the root IS its dir) |
| task run | `<root>/<task_id>/<uuid>.jsonl` | record — one per RUN |
| bare session | `<root>/<uuid>/session.jsonl` | THE record of that dir |
| side file | `<root>/<uuid>/<known>.jsonl` | NOT a record — same session |
| unknown | `<root>/<uuid>/<other>.jsonl` in a BARE dir | reported, never counted |

Exported surface: `JSONLSuffix`, `RecordName`, `SideFileNames()`, `IsJSONL`,
`IsSideFile`, `Kind` (+`KindRecord`/`KindSideFile`/`KindUnknown`/`KindOther`),
`Entry`, `Classify`, `DirHasRecord`, `HasBareRecord`, `SideFilesIn`, `Record`,
`Records`, `RecordInDir`, `RecordPath`.

Two directory shapes with different record COUNTS is the load-bearing part:

- a **bare** dir holds `session.jsonl` — exactly one session ran there, so
  every other JSONL is an artifact of that session;
- a **task-run** dir holds `<uuid>.jsonl` — one file per run, so several
  files are several run logs, each a record.

That is why a per-FILE classification cannot be the whole answer, and why the
package exposes both a scanning form (`Records` — every record file) and an
accounting form (`RecordInDir` — "this dir is one session"). A reader that
collapses the two would either count a task's second run twice or lose it.

### Why not `internal/signalrec`

`signalrec` owns the side file's VALUE (`FileName`) but is not a leaf — it
reads the process's own cgroup and imports `config`. `internal/gopilog` (which
`cmd/agent`, `internal/tools` and the CLI sit on) would then link the whole
signal recorder for a filename. Instead `sessionlayout` names the side file
once and
`TestSideFileNamesMatchWriters` asserts its value EQUALS
`signalrec.FileName` — one value, one production home, drift impossible
(Rule 14 without an import cycle).

### Readers wired to it

| Site | Change |
|---|---|
| `internal/gopilog/reader.go` — `collectSessionFiles` | glob → `sessionlayout.Records`; `FileToID`/bare-name fallback now use `RecordName`/`JSONLSuffix` (local `bareSessionFileName` constant deleted) |
| `internal/gopilog/rawlog.go` — `SessionsRecordingRawLog` | two-level glob → `Records` |
| `internal/agent/session.go` — `DetectOrphanSessions` | glob → `Records`; `sessionJSONLSuffix`/`bareSessionLogName` now alias the shared constants |
| `internal/agent/crashdump.go` — `reconcileStaleOrphanDumps`, `listOrphanDumpPaths` | "walk for `*.dump.json`" → the dump family **derived from each record's path** (`orphanDumpPath`), so the two passes cannot drift and a stray dump of no session is not resurrected |
| `internal/tools/memory_brew_run.go` — `listSessionJSONLs` | glob → `Records` (a provenance line has no conversation to slice) |
| `internal/tools/tasks.go` — `taskFindSessionJSONL` | per-dir glob → `Records` filtered to session dirs; `taskSessionJSONLExt`/bare-name literal now come from the shared package |
| `cmd/agent/cli_tasks.go` — `newestSessionJSONLIn` | suffix match → `Classify`; `taskSessionExt` mirrors nothing (`sessionlayout.JSONLSuffix`) |
| `internal/testutil/session.go` | its private layout code replaced by a delegation to `Records`/`RecordPath`/`SideFilesIn`; `jsonlSuffix`/`sessionRecordName`/side-file list now come from the shared package |
| `internal/signalrec` | writer's `sessionJSONLSuffix` now the shared value |

`internal/gopilog`'s per-FILE readers (`ParseFile`, `SessionHasActivity`,
`SessionStartTime`, `FindSession`) keep taking a file path: their unit really
IS a file, and the caller supplies a path discovered by the classified scan.
Made explicit here per the epic's acceptance wording.

**Deliberately NOT changed:** `internal/agent/crashdump.go`'s
`<id>.jsonl` ↔ `<id>.dump.json` path arithmetic (`sessionJSONLSuffix` stays in
that role), and `internal/tools`' `findSessionJSONLFlat` (a flat `*.jsonl` at
the sessions ROOT *is* a record, exactly as the classifier's root-layout key
says — verified by `TestClassify_LayoutKeyDecides`).

## Verification

- `CGO_ENABLED=0 go build ./...` — OK.
- `go test` greens, touched packages: `internal/sessionlayout`,
  `internal/testutil`, `internal/gopilog`, `internal/agent`,
  `internal/tools`, `internal/signalrec`, `cmd/agent` (see the task report for
  the raw runs).
- Regression tests (each with the pre-fix accounting kept in the test as a
  red-proof oracle where the contrast is meaningful):
  - `internal/sessionlayout/sessionlayout_test.go` —
    `TestRecords_BareSessionWithProvenanceSideFileIsOneSession` (asserts the
    naive glob sees 2 and `Records` sees 1),
    `TestRecords_DirWithOnlySideFileIsNotASession`,
    `TestRecords_FlatAndDirLayoutsCountOnceEach`,
    `TestClassify_LayoutKeyDecides`, `TestRecordPath_ReadinessProbeAgrees`,
    `TestSideFileNamesMatchWriters`.
  - `internal/gopilog/session_accounting_test.go` —
    `TestCollectSessionFiles_SideFileIsNotASession` (glob oracle = 2,
    reader = 1, plus `FindSession` resolving the record and NOT the side file),
    `TestListSessionFiles_IgnoresSideFile`,
    `TestSessionsRecordingRawLog_IgnoresSideFile`.
  - `internal/agent/crashdump_test.go` —
    `TestDetectOrphanSessions_SideFileIsNeverACandidate`.
  - `internal/tools/memory_brew_session_accounting_test.go` —
    `TestListSessionJSONLs_SkipsSideFile`,
    `TestListSessionJSONLs_StillSeesEveryRecordLayout`.
  - `cmd/agent/newest_session_test.go` —
    `TestNewestSessionJSONLInSkipsSideFile`.
  - `internal/testutil/session_accounting_test.go` (existing) — now runs
    against the shared classifier.

## Notes for the next agent

- A new `*.jsonl` inside a session dir must be REGISTERED in
  `sessionlayout.sideFileNames` (with its writer's constant pinned by a test)
  or it will be reported as an unknown artifact. Silent hiding is the one
  outcome the package refuses.
- `internal/sessionlayout`'s tests are an EXTERNAL test package
  (`package sessionlayout_test`): its `TestMain` needs `internal/testutil` for
  the scratch-root pin (Rule 22), and testutil imports the package under test.
- `internal/sessionlayout` carries the scratch-root `TestMain` like every
  other test suite — without it the Rule 22 gate fails on `t.TempDir()`.
