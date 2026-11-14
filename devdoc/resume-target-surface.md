<!-- doc-class: historical -->
# Resume's target surface: a run directory's own raw log, and the flag's real home

Epic `resume-target-surface` (task `01a0dd02…`, 2026-09-26). Two defects, one of
them a docs defect, all three about `gopi resume` promising a surface the binary
did not deliver.

## 1. A task-id target whose run directory holds only its raw log was unresolvable

**Symptom** (the briefing's own acceptance probe): with `GOPI_HOME` pointed at a
profile whose ledger holds no row for the target,

```console
$ mkdir -p "$SB/.gopi/sessions/t-1789999999"
$ cp <some real raw log> "$SB/.gopi/sessions/t-1789999999/t-1789999999.raw.log"
$ GOPI_HOME=$SB ./bin/gopi resume t-1789999999
gopi resume: cannot resolve session id "t-1789999999": …
gopi resume: resume target "t-1789999999" is neither a raw log path nor an
             existing session id (looked under …/.gopi/sessions)
```

The checkpoint sat one directory below the message that said it did not exist.

**Root cause (5-why).** `gopi resume <task-id>` resolves through three branches,
in order: `resumeTaskRawLog` (the ledger VIEW, `tools.TaskResumeTarget`), then
`resumeRunLog` (the run DIRECTORY, `rundir`), then `resolveResumeLogPath`
(path/session-id). Why did the middle branch miss?

1. Why the refusal: both filesystem branches returned "" and the third branch
   reported a session-id failure.
2. Why `resumeTaskRawLog` returned "": it reads the ROW first
   (`TaskProcessStatusFor`) — in a profile whose ledger lacks the row, it answers
   `ok=false` before any filesystem question is asked. That is by design, and it
   is the branch the epic's acceptance deliberately takes off the table.
3. Why `resumeRunLog` returned "": it asked `rundir.RecordsDir` for run RECORDS
   and then `WithCheckpoint`/`ExistingRawLog` — and a record resolves a raw log
   only through the `rawLogPath` its `session_start` event records.
4. Why there is no record: **the session log is written by the RUN PATH**, and the
   fixture (like every isolated profile, and like an interrupted first round) has
   the raw log without the session log.
5. **The norm**: the raw log's ADDRESS is its own location. A directory's
   `<run>.raw.log` is a checkpoint whether or not some other file in that
   directory points at it, and no reader may require the pointer.

Measured on this host to size the hole: **274** raw logs live inside session
directories, and the ledger-row layout names them by the RUN's uuid while the
directory is named by the TASK id (`sessions/t-1788721024/4411022b-….raw.log`) —
so basename matching is not an option either.

**Fix.** `internal/rundir.RunDirRawLog(dir)` — the ONE resolver of a run
directory's own raw log (newest first; `""` is a definite absence). It keys the
DECLARED suffix (`scratch.SessionRawLogFileSuffix`, the very constant the writer
`internal/agent.newRawLoggerInDir` builds the name from), so writer and reader
cannot drift. `cmd/agent/cli_resume.go`'s `resumeRunLog` consults it **after** the
record-based answer (the record is authoritative about *which* run's checkpoint is
meant) and **before** the `Latest` fallback. No second directory mapping was added
to `cmd/agent` (Rules 13/14): the address still comes from `rundir`.

## 2. `gopi resume <target> --task <id>` — measured, and NOT a parser bug

The briefing expected `unknown flag --task`. Measured on this tree, the flag is
parsed and accepted at argv[1] by `parseResumeArgs` (`--task` / `--task=`), and
its refusal is *deliberate and loud* for anything else:

```console
$ ./bin/gopi resume t-1789999999 --task t-1      # accepted — a resume runs
$ ./bin/gopi resume <sid> --task t-1             # accepted, row tagged resumed:<sid>
$ ./bin/gopi resume 0 --task t-1 --base-url X    # malformed --task flag: unknown option "--base-url"
```

The `unknown flag --task` message the briefing quotes comes from a **different
argv shape**: a run flag placed BEFORE the verb —

```console
$ ./bin/gopi --base-url http://127.0.0.1:9 --api-key dummy --timeout 8 resume 0 --task t-1
gopi: unknown flag --task
```

— where the routing consumes the leading run flags for a plain agent run, drops
the rest of the line into the prompt, and the top-level parser then meets `--task`
as a run-flag word (it is not in the run-flag catalog, so `config.FlagOverrides`
refuses it by name). So the flag surface is REAL; what was missing is the
**help's** statement of where the flag may sit.

## 3. `gopi resume -h` promised the ledger branch as if it were unconditional

The old text said the task-id target is "located automatically AND the row is
re-linked", with no caveat — true only in a profile whose ledger holds the row.
It now states the real precondition: the target addresses a **run directory**, the
lookup is a **filesystem** read (so it survives a ledger that does not know the
row), and the **tag/linkage** half (the `resumed:<session-id>` stamp, the row's
terminal status, the worktree resume) is the half that needs the row and is
skipped with a warning. The example block gained the flag's true home
(`gopi resume <target> --task <id>` — the verb FIRST) and the concrete broken
shape (a run flag before the verb), plus a co-located raw-log path example.

## Not done / deliberate boundaries

- **No change to the argv routing.** A run flag before the verb still becomes a
  run rather than a verb (`gopi --timeout 3 resume …` runs an agent whose prompt
  begins "resume …"). That is the documented routing of this CLI, unrelated to
  the two defects; changing it is a routing epic of its own, not this fix. The
  help now warns instead of the binary silently doing the surprising thing.
- **No basename matching of the run id against the raw-log name.** It would
  refuse the real layout (task-named directory, uuid-named log).
- `resumeRunLog` remains fail-soft: no target / no directory / no raw log all
  return "" and the pre-existing path/session-id resolution still runs.

## Verification (Rule 5)

- **Build:** `go build ./...` → `BUILD_ALL_OK`; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → ok.
- **Tests:** branch-scoped, both packages the diff touches —
  `go test ./internal/rundir/ ./cmd/agent/ -run 'Resume|RunDir|RawLog' -count=1` →
  `ok github.com/nick/gopi/internal/rundir`, `ok github.com/nick/gopi/cmd/agent`.
  The wider gate suite (`go test ./internal/standards/`, the doc-index classifier)
  is green on this tip as well.
- **Ran:** the briefing's acceptance, verbatim, in a fresh sandbox
  (`SB=/home/user/workspace/gopi-sandbox/sandbox-accept-247eb8d9`,
  `T=t-accept01a0dd02`), a profile whose ledger holds no such row:

  ```console
  $ mkdir -p "$SB/.gopi/sessions/$T" && cp <a real non-empty>.raw.log "$SB/.gopi/sessions/$T/$T.raw.log"
  $ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json ./bin/gopi resume "$T"
  gopi resume: resume: no → REQUEST checkpoint found in
     /home/user/workspace/gopi-sandbox/sandbox-accept-247eb8d9/.gopi/sessions/t-accept01a0dd02/t-accept01a0dd02.raw.log
  ```

  plus `./bin/gopi resume "$T" --task t-1` → no `unknown flag --task`
  (`grep -c` = 0), and `./bin/gopi resume -h` prints the corrected block.
- **Result:** the target resolves to **that FILE** (the run directory's own raw
  log) and the refusal happens one layer down, in the checkpoint parser — not in
  the address resolver. `grep -cE "cannot resolve session id|is neither a raw log
  path nor an existing session id"` over both streams = **0/0**.
- **Commit:** `e0143e36` (resolver + call site + help + tests),
  `bb6b92af` (doc index).

## Evidence (self-filed)

- **Red, at the acceptance level** — the same sandbox tree served to a binary
  built from the merge base (`git archive 79807932` → `RED_BUILD_OK`):

  ```console
  $ GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json ./bin/gopi-base resume t-accept01a0dd02
  gopi resume: resume target "t-accept01a0dd02" is neither a raw log path nor an existing session id
     (looked under /home/user/workspace/gopi-sandbox/sandbox-accept-247eb8d9/.gopi/sessions)
  ```

  That is the briefing's reported string, reproduced; the resolver call is its fix.
- **Red, in the suite** — with the single call site surgically deleted
  (`if raw := rundir.RunDirRawLog(dir); raw != "" { return raw }` → a comment),
  `go test ./cmd/agent/ -run TestResumeTargetResolvesRunDirRawLog` →
  `--- FAIL: TestResumeTargetResolvesRunDirRawLog (0.04s)` /
  `cli_resume_rundir_target_test.go:53: resolveResumeTarget("t-1789999999") errored:
  resume target "t-1789999999" is neither a raw log path nor an existing session id
  ... — the run directory's own raw log is the checkpoint`; call restored →
  `ok`, `git status --porcelain` empty again.
- `internal/rundir`'s own unit test (`TestRunDirRawLogResolvesTheCoLocatedCheckpoint`)
  covers resolve / empty dir / missing dir / empty argument.
