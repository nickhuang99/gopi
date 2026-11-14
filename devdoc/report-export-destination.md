<!-- doc-class: normative -->
# The exported report destination is a PRECONDITION, not a guess

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> Bug row: task `01a18310-f082-702e-8d06-7d40bf6444da` (epic `report-export-destination`).
> Measured 2026-10-28/29 on `host-b`.

## The contract, and the hole in it

The spawn seam exports an absolute report path to the executor child before
spawn (`cmd/agent/cli_tasks.go`, the `taskreport.PathEnv` / `taskreport.OwnEnv`
pair). The child's contract — stated in the system prompt and obeyed by every
run that writes a report — is:

```bash
cat > "$GOPI_TASK_REPORT_PATH"
```

Nothing in the export made the destination's **parent directory** a
precondition. `TaskReportPath` answered a STRING (`<gopi-dir>/sessions/<id>/task-report.md`)
without touching the filesystem, so a row whose session container was absent at
spawn time — never created, pruned since, or removed by the run itself — handed
the child a path `cat >` cannot open. The run's only report goes nowhere, while
the harness's close-out seal resolves the same path, `MkdirAll`s it, and writes
a SKELETON there. Two views of one contract, disagreeing, and the run cannot
tell.

The criterion the epic states is the fix's whole specification:

> **an exported path must be WRITABLE, else it must not enter the environment.**

## WHO produced the dashless value (root cause, measured)

The briefed symptom was an exported path naming a **dashless** session
directory (`…/sessions/<32-hex>/task-report.md`) while the run's canonical
hyphenated container sat beside it. Two measurements settle who can write that
value:

**1. This tree's ONE writer cannot.** `TaskReportPath` → `taskSessionDir`
gates the id on `sessionid.IsAdmissible` (bug row `01a12f3c`, present at the
revision that ran the affected session — `git show ee873ec6:internal/tools/task_session.go`
carries the gate at line 157). Driven for real:

```
$ GOPI_HOME=<scratch> go run ./internal/tmpprobe \
    01a18310-f082-702e-8d06-7d40bf6444da \
    01a18310f082702e8d067d40bf6444da
EXPORTED 01a18310-f082-702e-8d06-7d40bf6444da -> <scratch>/.gopi/sessions/01a18310-f082-702e-8d06-7d40bf6444da/task-report.md
         parent-exists=true isdir=true write-probe=<nil>
REFUSED  01a18310f082702e8d067d40bf6444da   -> task report path for 01a18310f082702e8d067d40bf6444da:
         task session dir: task id shape: "…" is a uuid's 16 bytes in a non-canonical spelling …
         did you mean "01a18310-f082-702e-8d06-7d40bf6444da", the ROW id?
```

**2. The affected run's environment carried the path WITHOUT its owner marker.**
`GOPI_TASK_REPORT_OWNER` appears **0 times** in that session's `output.log`,
while the pair is written by exactly ONE place in this tree and always together
(`taskreport.PathEnv` + `taskreport.OwnEnv`, `cmd/agent/cli_tasks.go:3356-3357`).
A `PathEnv` with no `OwnEnv` therefore did **not** come from this tree's spawn
seam — it came from the launching channel, and that channel built it from the
**branch/worktree stem**, which is the dashless spelling by construction
(`taskGroundIDStem` strips every non-alphanumeric character). The affected run's
branch is `task/01a1828dabc47238af50759d5d69e555` — the dashless string exactly.

**So: the value's producer is outside this tree; the defect that made it FATAL
is inside.** An exporter that hands over an unusable destination silently is
what turned a wrong spelling into a lost deliverable. That is what this change
closes, and it closes it for every producer — including a third-party launcher
this tree does not own.

The 31 dashless residue directories on this host are the operator's data and are
**not touched** by this change or by its tests.

## The fix: one resolution, one precondition

`internal/tools/task_report.go`:

```go
func TaskReportDestination(taskID string) (string, error) {
    path, err := TaskReportPath(taskID)      // Rule 14: no second resolution
    …
    dir := filepath.Dir(path)
    os.MkdirAll(dir, taskReportDirMode)      // idempotent: the row's own container
    taskReportProbeWritable(dir)             // a write, not a stat
    return path, nil
}
```

* **The path is the SAME one the read faces use.** `TaskReportPath` stays the
  single resolution; the export adds a precondition and nothing else, so
  `task show` (which reads `TaskReportPath`) and a run's own destination cannot
  name different directories.
* **The probe is a WRITE, not a `stat`.** Existence without writability (a
  read-only mount, another user's container, a full filesystem) is a state this
  seam could plausibly leave behind, and the criterion is writability. The probe
  file is released on success; a probe that **cannot** be released also REFUSES
  the destination — a leftover file in the child's own report directory is
  exactly the litter the precondition exists to stop, and `internal/stylegate`'s
  ignored-error ratchet is what made that half a reported result rather than a
  discarded error.
* **A failure REFUSES and is printed** — the seam's existing `else` arm already
  says `report path for <id> unresolved (continuing without GOPI_TASK_REPORT_PATH): <err>`;
  the destination simply cannot reach a child's environment (Rule 15).
* **Read faces do not create directories.** `TaskReportPath` is untouched:
  a `task show` that mkdir'd would be a display command mutating the disk.

## The regression arms

`internal/tools/task_report_destination_test.go`:

| arm | what it pins |
|---|---|
| `TestReportDestinationParentExists` | container DELETED first; the export re-materialises the parent and accepts the contract's own write; the probe leaves nothing behind |
| `TestReportDestinationIsTheReportPath` | one resolution (Rule 14) |
| `TestReportDestinationRefusesUnwritableParent` | a `0o500` container is REFUSED, and the refusal names the directory and says "not writable" |

`cmd/agent/cli_tasks_report_destination_test.go` drives the REAL `gopi worker`
wrapper through a mock model whose first action is the contract's own write
(`printf > "$(dirname $GOPI_TASK_REPORT_PATH)/…"`), with the row's session
container deleted before the run: the verdict is the filesystem.

## Red, measured

With `TaskReportDestination` reduced to `return TaskReportPath(taskID)` (the
pre-fix behaviour) and both new test files in place:

```
--- FAIL: TestReportDestinationParentExists (0.00s)
    task_report_destination_test.go:71: the exported destination
    "…/.gopi/sessions/01a11500-0000-7000-8000-0000000000e2/task-report.md" has no parent
    directory (stat …: no such file or directory) — a run obeying
    `cat > "$GOPI_TASK_REPORT_PATH"` gets ENOENT there
--- FAIL: TestReportDestinationRefusesUnwritableParent (0.00s)
    task_report_destination_test.go:133: a read-only destination directory (…) was accepted
FAIL
```

Green after the fix: `ok github.com/nick/gopi/internal/tools 8.243s`.

## Related, and deliberately out of scope

* The producer of the dashless spelling lives outside this tree; a guard for it
  belongs at `main.go`'s startup ownership check (`SanitizeReportContract`),
  which currently asks "is this destination MINE", not "is this destination
  USABLE". Filed as a separate observation rather than widened here.
* `devdoc/path-enoent-was-a-misspelled-name.md` records the sibling defect: the
  `write`/`edit` tools `MkdirAll` a misspelled path into a second container.
  That is a different seam and a different writer.
