<!-- doc-class: historical -->
# Task Report: Collision-free shared dump naming (1214 / failed-request dumps)

## Completed

- **Root cause fixed.** Both death-dump writers named their file
  `time.Now().Format("20060102-150405") + ".dump.json"` — 1-second resolution
  with no uniqueness suffix — and wrote it with `os.WriteFile`, which
  truncates. A second dump inside the same wall-clock second therefore
  overwrote the first, silently losing the failed request (the dump's only
  purpose is replay/resume).
  - `internal/deepseek/error_1214.go:220` (`dump1214Request`)
  - `internal/deepseek/minimal_core.go:125` (`dumpFailedRequest`)
- **ONE shared helper** — new file `internal/deepseek/dump_file.go`:
  - `writeDumpFile(dir, body, logTag) string` — the only production entry
    point, used by BOTH writers.
  - `writeDumpFileAt(dir, ts, body, logTag) string` — same function with an
    injected timestamp, so the same-second rule is testable with **no sleeps**.
  - Collision-free create: `os.OpenFile(path, O_WRONLY|O_CREATE|O_EXCL, 0644)`;
    on `EEXIST` retry with `-1`, `-2`, … (`dumpMaxNameAttempts = 100` runaway
    guard — a retry only happens when the candidate already exists, never
    overwriting).
  - Named constants (Rule 1): `dumpNameTimeLayout`, `dumpFileSuffix`,
    `dumpMaxNameAttempts`, `dumpFilePerm`, `dumpDirPerm`.
  - Rule 15b: mkdir/create/write/close failures are `log.Printf`-ed and return
    `""` (the caller then reports "dump write failed"); the fail-fast error
    still carries the real cause — never fatal, never silent.
- **No duplicated naming logic left** (Rules 13/14): the local
  name-building + `MkdirAll`/`WriteFile` blocks in both writers were replaced by
  a single `writeDumpFile(...)` call.

## Not Done

- Dump *directories* remain per-writer (`GOPI_1214_DUMP_DIR` → `gopi-1214-dumps`,
  `GOPI_ERROR_DUMP_DIR` → `gopi-dumps`). That split is deliberate and untouched
  — the collision is on the file name inside a directory, not on the directory
  choice, and keeping the two streams separate is existing behaviour.
- No change to what a dump contains (payload shape, `errorDumpMaxTail`) — out of
  scope for this fix.

## Verification

Build / test (worktree `~/workspace/gopi-dump-collision`, branch
`fix/dump-name-collision`, off master `b7b8000`):

```
$ go build ./...
BUILD_OK
$ go test -count=1 ./internal/deepseek/     # full package
ok  github.com/nick/gopi/internal/deepseek  2.046s
$ gofmt -l internal/deepseek/               # no output
$ go vet ./internal/deepseek/
VET_OK
```

New regression tests (`internal/deepseek/dump_collision_regression_test.go`,
deterministic — no sleeps/network/API key, dirs via `t.TempDir()` + env):

- `TestWriteDumpFile_SameSecondYieldsTwoFiles` — the core repro: two writes
  with the SAME timestamp ⇒ **2 files** (`<ts>.dump.json` and
  `<ts>-1.dump.json`), each holding its OWN body.
- `TestWriteDumpFile_SameSecondThirdDumpGetsNextSuffix` — a third same-second
  dump lands at `<ts>-2.dump.json` (3 files).
- `TestDumpWriters_SameSecondEndToEnd` — drives BOTH real writers
  (`dump1214Request` + `dumpFailedRequest`) in one second, each in its own
  `t.TempDir()`: exactly 1 file per dir, each retaining its own request body
  (and the 1214 dump is valid JSON with `original_request`).
- `TestWriteDumpFile_WriteFailureIsNonFatal` — unwritable dir ⇒ `""`, logged,
  no panic (Rule 15b).
- `TestDumpNaming_HasExactlyOneDefinitionSite` — Rule 13 gate: the literals
  `"20060102-150405"` and `".dump.json"` may appear in production code ONLY in
  `dump_file.go`; a future re-inline fails CI.

Repro proof on the OLD code (master `b7b8000`, lines quoted from
`git show b7b8000:...`): `error_1214.go:220` and `minimal_core.go:125` both had
`name := time.Now().Format("20060102-150405") + ".dump.json"`, and a standalone
program replicating that naming reported:

```
OLD code: same-second writes -> files=1 paths_equal=true
OLD surviving body: {"session":"second"} (first request LOST)
```

After the fix the same-second case yields `files=2` with both bodies intact
(the tests above). One-definition check:
`grep -rn 'writeDumpFile' internal/deepseek/*.go` shows the definition in
`dump_file.go` and the two call sites (`error_1214.go:246`,
`minimal_core.go:142`) — no second naming implementation.

Commit: **b28d04c** — `fix(deepseek): collision-free shared dump naming for
1214 + failed-request dumps`.
