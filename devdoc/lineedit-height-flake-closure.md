<!-- doc-class: historical -->
# Task Report: the small-height chat flake — root cause, the mechanical guard, and why no fix was owed

Task: `01a0c28b` — *bug: TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights
flakes on master (2/20) — pre-existing, not the menu lost-wake*.
Branch: `fix/lineedit-height-flake` (worktree `~/workspace/gopi-lineedit-height`).

## Summary (the one-line answer)

The flake is **real, reproducible and already fixed on master**. It was never a
layout/geometry defect: the test synchronized on a **producer's draw counter**
(`waitDraws(t, Answer().StatusPublishes()+1)`) while its assertion needed the read
loop to have **read the queued keystrokes** — two independent events the test's
source reports as one `EventWake`. Commit `65d7e278` ("a draw never overtakes
queued input") fixed it inside `internal/lineedit` and moved the invariant into a
deterministic guard; it reached master as `c74ca08f`.

So this task's deliverable is **not** a second fix (there is nothing to fix): it is
the measurement that pins the mechanism, the registry row that stops the flake
being re-diagnosed, a mechanical gate that stops the shape coming back, and an
honest dispatch of the one residual the existing guard does *not* cover.

## The measurement

Every claim below is a command that was RUN on this host, with what it printed.

### 1. The flake reproduces on the revision the briefing measured

`~/workspace/gopi-height-baseline` = master **2fdc65e4**, the revision the
briefing measured against:

```
for i in $(seq 1 20); do go test -count=1 -run TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/ || echo FAIL $i; done
→ FAIL 8
  FAIL 11            (2/20 — the briefing's own figure, reproduced)
go test -count=100 -run TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/
→ --- FAIL: … (twice), shapes "you> hel", "you> hell", status "iter 0"
```

### 2. The test's synchronization had no causal order with its assertion

Instrumented (a `t.Logf` at the retired wait's trip point, printing
`StatusPublishes()`, the source's read index, and the two rows), reverted after
use:

```
TRIP draws=2 read=4 of 5 editorRow="you> hell" statusRow="… · iter 0  ready"   FAIL
TRIP draws=2 read=2 of 5 editorRow="you> he"   statusRow="… · iter 0  ready"   FAIL
TRIP draws=2 read=5 of 5 editorRow="you> hello" statusRow="… · iter 0  ready"  FAIL
TRIP draws=2 read=5 of 5 editorRow="you> hello" statusRow="… · iter 1  ready"  (healthy run)
```

The counter trips with **2, 4 or 5 of the 5 keystrokes read** — the wait returns
on the publish's wake, and the assertion then reads a screen still being produced.

### 3. The wait was the flake; the assertion-subject wait is not

On the **fixed** tree (`abf073b3` = master, with `65d7e278` merged), the OLD wait
form re-injected into the same test:

```
go test -count=300 -run TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/
→ 38 failures / 300 runs
```

and with the shipped (assertion-subject) wait:

```
go test -count=10000 -run TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/
→ ok  cmd/agent  119.7s        (0 failures in 10 000 in-process runs)
```

Same production code, same machine, same seeds — only the synchronization differs.
That is the causal attribution, not a correlation.

## Why the briefing said "pre-existing"

Master **contains** the fix. `2fdc65e4` (the briefing's measurement revision) is
an ancestor of master but **predates** `65d7e278` — it was the pre-merge tip of
the `fix/chat-small-height-wait-from-master` work. The briefing's suggested angle
(the region/paint geometry path) was the *sibling* task `01a0c039`, which had
already root-caused **this** flake as the ordering defect and shipped `65d7e278`.
The region arithmetic (`computePlan`, `rowsMinUsable`, `clampAndFallToMenu`) was
never involved: at every failing trip point the measured layout was exactly the
minimum (`EditorTop:2 EditorRows:1 BandTop:0 BandBottom:1 StatusRow:3` at 4 rows),
i.e. the layout was correct and the *screen* was unfinished.

## What this branch lands

1. **`testdata/flakes.tsv`** — one registry row for
   `TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights` (package
   `cmd/agent`, owner `01a0c039`, date 2026-09-21) carrying the deterministic
   mechanism, the measured shapes and rates, the fixing commit, and the residual
   below. The registry's own gate (`TestFlakeRegistryMatchesTheTestsItNames`)
   enforces the row's shape, that the test exists, and that the owner id resolves.
2. **`internal/standards/screen_assertion_gate_test.go`** — a mechanical gate
   (probe `TestScreenAssertionCountersRedProof`) that reports a test call site of
   a producer counter (`StatusPublishes` / `Repaints` / `Writes`) under
   `internal/` + `cmd/`, with a **read floor** so a broken traversal cannot read
   green, an inline escape (`//screen-ok: <what it measures>`) for a genuine
   measurement, and a five-entry baseline naming the existing
   measurement sites (each reasoned, each reproduced by the `grep` in its comment).

**Red-proof (both directions, run):**

```
# gate vs a re-injected retired shape in cmd/agent/lineedit_height_test.go:176
plugin/screen-assertion-sync: NEW violation (checklist 4.3) at cmd/agent/lineedit_height_test.go:176
  [StatusPublishes() is a PRODUCER counter — …]
→ FAIL        ; after `git checkout`: → ok  (control green)
go test -count=1 -run TestScreenAssertion ./internal/standards/   → ok
```

## Not Done (honest)

**No production fix was written, because none is owed** — `65d7e278` already
fixed it and this branch verifies that at 10 000 in-process runs.

**The residual the existing guard does not cover** (filed as a separate task, per
Rule 26):

- `TestChatPublishDoesNotOvertakeQueuedInput` guards the editor's **ordering
  decision**, but its harness cannot exercise the seam the decision *consults*:
  `cmd/agent`'s `scriptSource` does **not** implement `lineedit`'s optional
  `inputBacklog`/`InputPending()` (only `*lineedit.Terminal` does), and
  `internal/lineedit`'s `scriptedSource` implements no backlog either and has no
  park hook. So the guard's red-proof requires **neutralizing the production call
  site** (`if e.inputPending() {` → `if false {`, measured: red 6/6 at rows=4), and
  the shipped `Terminal.InputPending()` (`internal/lineedit/raw.go:372`) has **no
  test at all**.
- Consequence: if the seam regressed (e.g. `Terminal.InputPending()` began
  returning false), the shipped guard would stay green and the old flake would
  return. Closing that needs harness work in two packages, i.e. a separate,
  well-scoped task — not a rider on this branch.

**Not attempted:** a package-level deterministic guard in `internal/lineedit` that
is red on the pre-fix tree. The shipped package-level fix is already in master, so
"red-proof on the unfixed tree" can only be shown by reverting it (done, see §2 of
the measurement, for the `cmd/agent` half) or by neutralizing the seam — which is
what the residual above is about.

## Verification

- Build: `go build ./...` → exit 0; `CGO_ENABLED=0 go build ./cmd/agent/` → exit 0
- Acceptance loop: `for i in $(seq 1 20); do go test -count=1 -run TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights ./cmd/agent/ || echo FAIL $i; done`
  → **PASS=20/20, FAILED_AT=** (empty)
- In-process (the briefing's recipe): `go test -count=25 …` → **ok**; `go test -count=1000 …` → **ok**
- Deterministic guard: `go test -count=5 -run TestChatPublishDoesNotOvertakeQueuedInput ./cmd/agent/` → **ok**
- New gate: `go test -count=1 -run 'TestScreenAssertion|TestFlakeRegistry' ./internal/standards/` → **ok**
- Flake registry gate: `go test -count=1 -run TestFlakeRegistryMatchesTheTestsItNames ./internal/standards/` → **ok** (owner `01a0c039` resolves in the ledger)

## Evidence (self-filed)

| claim | command | observed |
|---|---|---|
| pre-fix flake rate | 20× one-shot loop in `~/workspace/gopi-height-baseline` (2fdc65e4) | `FAIL 8`, `FAIL 11` |
| pre-fix in-process failure | `go test -count=100 -run … ./cmd/agent/` there | 2 `--- FAIL`, shapes `you> hel` / `you> hell` / `iter 0` |
| synchronization, not layout | instrumented `t.Logf` at the retired wait | `read=2/5`, `read=4/5`, `read=5/5` at the trip points; layout at every trip = `EditorTop:2 EditorRows:1 BandTop:0 BandBottom:1 StatusRow:3` |
| the OLD wait still flakes on the FIXED tree | re-injected `waitDraws` shape, `-count=300` on `abf073b3` | 38 `--- FAIL` |
| the shipped wait does not | `-count=10000` on the same tree | `ok github.com/nick/gopi/cmd/agent 119.7s`, 0 failures |
| guard red-proof | `if e.inputPending()` → `if false` (temporary), `-count=10 -run TestChatPublishDoesNotOvertakeQueuedInput` | 6 `--- FAIL`, "the status draw overtook queued input" |
| new gate red-proof | retired shape re-inserted at `cmd/agent/lineedit_height_test.go:176` | `NEW violation … plugin/screen-assertion-sync`; revert → `ok` |
| read floor is measured, not chosen | `grep -rn --include=*_test.go -E '\.(StatusPublishes\|Repaints\|Writes)\(\)' internal cmd` | 5 real call sites, all in `internal/lineedit` budgets (baselined) |
| terminal seams | `grep -rn "InputPending" internal/lineedit/raw.go` | `func (t *Terminal) InputPending() bool` at raw.go:372 — no test names it |

Instrumentation used for the two measurements above (the `t.Logf` in
`chat_stream_test.go`, the re-injected `waitDraws` form, the `if false` seam
neutralization) was **reverted**; `git status --porcelain` in the worktree is
empty apart from the committed deliverable.

## Addendum — independent re-verification, 2026-09-21 (task 01a0c28b, same branch, later run)

The branch above was authored by `01a0c039`'s work and left its acceptance loop
unobserved on this tip. It was re-run here, and one claim was strengthened.

**Re-measured, all on the branch tip `6620b59c` (`fix/lineedit-height-flake`):**

- Acceptance loop (20 × one-shot `-count=1`): **20 ok / 0 FAIL**.
- In-process, the briefing's own recipe: `-count=200` → `ok cmd/agent 4.7s`;
  `-count=1000 -run TestChatPublishDoesNotOvertakeQueuedInput` → all PASS.
- The two gates this branch owns: `go test -run 'TestScreenAssertion|TestFlakeRegistry' ./internal/standards/` → **ok**.
- The whole `internal/standards` package fails **one** test,
  `TestDocManifestCoversEveryDevdocFile` — and it is red on PURE MASTER too
  (`abf073b3`: 2116 rows / 2165 files, 49 unclassified). It is the pre-existing
  manifest backlog, already filed as `01a0c2c0`; this branch's own devdoc has
  its row (`devdoc/DOC-INDEX.tsv:1124`). Not this branch's defect.

**Strengthened claim — the guard, and only the guard, stands between master and
the flake.** The devdoc's own red-proof neutralized the PRODUCTION call site
(`if e.inputPending() {` → `if false {`). The same A/B is available with **no
production edit at all**, by making the test HARNESS's backlog seam answer
"nothing pending" — which is exactly the state a regressed seam would present:

```
# cmd/agent/lineedit_wire_test.go: func (s *scriptSource) InputPending() bool { return false }
go test -count=5 -run TestChatPublishDoesNotOvertakeQueuedInput ./cmd/agent/
→ --- FAIL: rows=4: the status draw overtook queued input — the 2-th recorded chunk
  drew the status, and the read loop only read its first keystroke after 3 chunk(s)
  had been written
# restored → git status --porcelain empty
```

So the shipped guard is not merely green: it goes red the moment the one seam the
fix consults stops reporting the backlog, on code that is otherwise master. That
is the residual `01a0c2cf` — the harness half of the seam — expressed as a
measurement rather than as prose.
