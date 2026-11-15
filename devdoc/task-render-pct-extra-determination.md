<!-- doc-class: normative -->
# Determination: the `%!(EXTRA …XML syntax error… )` line — ledger DATA, not a renderer defect

**Bug row**: `01a12e53-a1c9-7836-87af-2b36b0dbaac5`
**Reporter row**: `01a12dc0-5616-7498-a7ce-73821aa6cdb2` (Rule 41 note inside its report)
**Determination**: **no fmt verb/argument mismatch was found.** The marker is text carried
by a ROW'S OWN SUMMARY and echoed verbatim. The renderers are correct. The reporting
run's quoted evidence does not reproduce.

Every claim below is a measured one; the command is quoted with its raw output.

---

## 1. The quoted evidence does not exist

The report's `artifacts/parent-row.txt` is 14216 bytes and byte-identical to the file the
producing run wrote (`md5sum` of the archived original and of the copy attached to this
task row agree):

```
$ md5sum /home/user/.gopi/sessions/01a12dc0-5616-7498-a7ce-73821aa6cdb2/artifacts/parent-row.txt \
         /home/user/.gopi/sessions/01a12e53-a1c9-7836-87af-2b36b0dbaac5/attachments/home/user/.gopi/sessions/01a12dc0-5616-7498-a7ce-73821aa6cdb2/artifacts/parent-row.txt
fad3943f96157ec8fe2ddb6274289248  …/01a12dc0-…/artifacts/parent-row.txt
fad3943f96157ec8fe2ddb6274289248  …/01a12e53-…/attachments/…/artifacts/parent-row.txt

$ grep -c 'EXTRA' parent-row.txt
0

$ sed -n '108p' parent-row.txt
```
```

Line 108 is a **code fence**, not a rendered row — and the file contains no `EXTRA` at all.
The quoted line also cannot be what the report's own command would print: its command was
`gopi task show 01a12dbc-… | grep -n 'EXTRA'`, and in the row that command names the
`summary:` label is on **line 3**, while `(model …, source …, created …)` is not a shape
`task show` renders at all (that is the `task list` row format):

```
$ gopi task show 01a12dbc-8452-7419-9e98-41442370bf9c | grep -nE '^(summary|model|source|created_at):'
3:summary:        关闭链每步须可单独调用：暴露 gopi task push / release-worktree / drop-branch，finish-cl
12:model:          deepseek-flash
17:source:         human
280:created_at:     2026-10-12 09:31:31

$ gopi task show 01a12dbc-8452-7419-9e98-41442370bf9c | grep -c 'EXTRA'
0
```

**Inference (labelled)**: the marker text was first composed while writing the report row's
own summary, and the "raw output" block around it was reconstructed from memory rather than
pasted. That is a Rule 41 evidence-discipline failure in the *reporting* run — the marker it
quoted is real Go output, and its attribution to the parent row's render was not.

## 2. Where the text actually lives: three rows' summaries

The marker is in the ledger as **data**. `gopi task list --all` renders 3433 rows; exactly
three carry `%!`, and each one is the row's own `[summary]` field:

```
$ gopi task list --all | grep -c '^#'
3433
$ gopi task list --all | grep -n '%!'
6:#01a12e53-a1c9-7836-87af-2b36b0dbaac5  [in_progress]  task show 的一行渲染打印 %!(EXTRA &errors.errorString=&{XML syntax error on line 1: unexpected EOF}) …
978:#01a0ac31-1438-77c9-8e96-ad5de54c107b  [completed]  merger --status swallows the branch-attribution error — %!w(<nil>) tells the operator nothing (Maps repo)  …
984:#01a0ac28-9851-7a19-b15c-80506d009d3e  [cancelled]  merger --status defects: phantom pending (behind=125 reported as mergeable) + owner-ended worktrees + %!w(<nil>)  …
```

Row `01a0ac31` is the same phenomenon with a **real** defect behind it (`%!w(<nil>)` was
`fmt.Errorf("… %w", err)` handed a nil error — a genuine bug, filed and described, its site
named in `internal/tools/branch_attribution.go`). Its `%!` is likewise the summary's text.
A `%!` inside the summary column is this ledger's normal shape for a bug report ABOUT a
marker, and it is never evidence about the renderer that prints it.

## 3. The renderer cannot produce it, and there is a structural reason

`internal/tools/tasks.go:2221` is the `task list` row — the only renderer with a
`(model …, source …, created …)` column:

```go
fmt.Fprintf(&out, taskListRowFormat,
    taskHashDisplay(t.TaskID), t.Status, sanitizeTaskSummary(t.Summary), model, t.Source, formatTaskTime(t.CreatedAt),
    taskRunningSuffix(t, now), …, via, queue, ground, closeout)
```

**Template verbs = 13, arguments = 13, and the compiler enforces it** (the format is a
constant string literal, so `cmd/vet`'s printf check is part of `go test`/`go build`):

```
$ go vet ./internal/tools/ ./cmd/agent/
VET_EXIT=0
```

**And the binary cannot even name that error class**: `encoding/xml` is absent from the
whole transitive closure, so no Go package in `gopi` can construct
`XML syntax error on line 1: unexpected EOF`:

```
$ go list -deps ./cmd/agent | grep -x 'encoding/xml'   # exit 1, no line
$ go list -deps ./cmd/agent | grep '^encoding/'
encoding/binary  encoding/base64  encoding/hex  encoding/json
encoding/asn1    encoding/pem     encoding/gob
$ grep -rn 'encoding/xml' --include=*.go internal cmd   # exit 1, no line
```

(Go's `encoding/xml` is the only source of that exact string, and the marker's shape
`&errors.errorString=&{…}` is `%v` of an `*errors.errorString`.)

### The empirical discriminator: `MISSING` vs `EXTRA`

The two markers mean OPPOSITE things, and the live ledger settles which one is present:

- `%!s(MISSING)` / `%!d(MISSING)` — the template has MORE verbs than arguments, i.e. a real
  renderer defect.
- `%!(EXTRA …)` — arguments outnumber verbs, OR the format string was a data value.

Measured on the live ledger's own rows (branch binary, `artifacts/branch-task-list-all.txt`):

```
$ grep -c '%!' <3434 task-list rows>          # 3 rows carry a marker
$ grep -c '%!.*MISSING' <same file>           # 0  ← the arity-defect shape is ABSENT
```

Zero `MISSING` in 3434 rows is not a coincidence to explain away: with the templates constant
and `go vet` enforcing the arity, there is no way to render one. The three markers that ARE
there all sit in a `[summary]`, which is the DATA column.

## 4. The one shape that WOULD produce the reported line — and why it is absent

If a renderer used a row's own text as the **format string** (argument list unchanged), the
reported line is reproduced exactly. Measured on the real renderer, one line injected:

```
### RED PROOF — cmd: replace the list renderer's FORMAT with the row's own summary text, keeping all 13 arguments
$ go test ./internal/tools/ -run TestTaskRendersCarryNoFmtMarkers -count=1
--- FAIL: TestTaskRendersCarryNoFmtMarkers (0.06s)
    payload %!!(string=t-marker-row)(EXTRA &errors.errorString=&{XML syntax error on line 1: unexpected EOF}) verbs in_progress %!d(string=payload %!(EXTRA &errors.errorString=&{XML syntax error on line 1: unexpected EOF}) verbs %s %d %q %% tail) "glm-marker" …
```

That is the reported marker, produced from data — and master contains no such call site:

```
$ grep -rn 'Fprintf(&out, [a-z]' --include=*.go internal/tools
internal/tools/tasks.go:2221:		fmt.Fprintf(&out, taskListRowFormat,
internal/tools/tasks.go:5629:			fmt.Fprintf(&out, taskSearchRowFormat,
```

Both callers pass the module-level constant. **Master is correct here.**

## 5. What this run DID change (and what it is not)

Two renderers' row templates were lifted from inline literals to named constants
(`taskListRowFormat`, `taskSearchRowFormat`, defined in `internal/tools/task_row_formats.go`)
and a guard test was added — `TestTaskRendersCarryNoFmtMarkers`
(`internal/tools/task_render_pct_marker_test.go`). The constants live in their own file so
that `tasks.go`'s line numbers are untouched: this repo freezes `tasks.go` file:line anchors
(`internal/docclass/testdata/anchor-number-baseline.tsv`), and a 17-line insertion above
`tasks.go:2395` made `gopi tool doccheck` report that frozen anchor as drifted.

**Stated plainly, so a later reader does not mistake it for a fix**: this is **not** a
repair of a reproduced defect, because **no defect was reproduced**. It closes the gap the
bug row actually opened — the ledger's row renderers had no arity guard, while the shot
usage template has had one since `cli_shot_surface_test.go`
(`TestShotUsageArgsMatchTemplate`). A template edit that drops a verb *and* passes a
variable format (which bypasses vet, exactly as the injection above did) would otherwise
reach the operator's view of the ledger as `%!s(MISSING)`. The test pins both directions
on one fixture: a **clean** row must render through list/show/search with no fmt diagnostic,
and a row whose summary is *itself* full of markers and live verbs must echo that text
verbatim (data is never a format string).

## 6. Reproducing / refuting this determination

```bash
gopi task list --all | grep -c '%!'        # 3 — one per row, all inside [summary]
gopi task show 01a12dbc-8452-7419-9e98-41442370bf9c | grep -c EXTRA   # 0
go test ./internal/tools/ -run TestTaskRendersCarryNoFmtMarkers -count=1   # PASS
```

The same two queries on the **branch binary** (the changed renderers built from this branch,
run against a copy of the live ledger) give the same answers — 3434 rows, 3 markers, all in
`[summary]`, and `0` `EXTRA` in that row's `show`:

```bash
CGO_ENABLED=0 go build -o /dev/shm/gopi-pct ./cmd/agent/
GOPI_MEMORY_DB=/dev/shm/pct-ledger.db /dev/shm/gopi-pct task list --all | grep -n '%!'
7:#01a12e53-a1c9-7836-87af-2b36b0dbaac5  [in_progress]  task show 的一行渲染打印 %!(EXTRA &errors.errorString=&{XML syntax error on line 1: une
979:#01a0ac31-1438-77c9-8e96-ad5de54c107b  [completed]  merger --status swallows the branch-attribution error — %!w(<nil>) tells the operator nothin
985:#01a0ac28-9851-7a19-b15c-80506d009d3e  [cancelled]  merger --status defects: phantom pending (behind=125 reported as mergeable) + owner-ended work
```

The one thing this determination does **not** have is the reporting run's original terminal
scrollback: if the marker was ever really printed by a renderer, it left no trace in that
run's session JSONL (every occurrence there is either agent-written text or a later echo of
agent-written text — `artifacts/reporter-session-provenance.txt`). It is a refutation on
the evidence available, not a proof that no screen ever showed it.
