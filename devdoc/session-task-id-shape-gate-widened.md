<!-- doc-class: historical -->
# The task-id shape gate, WIDENED to any non-canonical uuid grouping

**Status:** landed (branch `task/01a1300f294075e1ab1944998c6afb89`, 2026-10-12).
**Class:** historical (this run's record — the classifier's own verdict; the
STANDING statement of the rule lives in `internal/sessionid`'s package comment
and doc comments, which are the code itself, plus the pointer added to
`devdoc/session-task-id-shape-gate.md`).
**Bug row:** `01a12fd9-60fd-7656-883e-c785087907b5` (`--tags bug`), filed from the
diagnosis of row `01a12f96-c2bf-732f-8cd2-4b63a367d4af` ("files vanish from a live
run's session directory" — they did not; the run typed the wrong spelling).
**Supersedes the scope of:** `devdoc/session-task-id-shape-gate.md` (the first cut,
bug row `01a12f3c`, which refused the DASHLESS spelling only).

## The defect this closes

The gate above refused exactly one shape: a uuid's 16 bytes with **no dash at
all** (`len != 32 || contains "-"` → not a mangled uuid → admitted). Every other
grouping of the same 32 hex characters walked through, was `filepath.Join`'d into
a path and `MkdirAll`'d, and **minted a second session container beside the row's
own**:

```
01a12f96-c2bf-732f-8cd24b63a367d4af   len 35, dashes at [8,13,18]   ← the row's FIRST spelling, admitted
01a12f96-c2bf-732f-8cd2-4b63a367d4af  len 36, dashes at [8,13,18,23] ← canonical
```

Both containers exist on this host right now, each holding exactly the files its
own spelling wrote. Census over the live root (`~/.gopi/sessions`, this host):
**8182 dirs, 8133 canonical, 22 hex+dash but NOT canonical, and every one of the
22 has a live canonical twin** — i.e. each is a duplicate container of a row that
exists. Measured dash-offset shapes: `[8,13,18]` (the 4th dropped), `[8,13]` (the
last two dropped) and `[12,17,22]` (a different grouping at the same 32 hex).

The cost is not disk: a run writes its evidence into a container no reader, no
report path and no merge path looks at, then reads it back under the canonical
name and reports **"files vanished from a live session directory"** — a phantom
data-loss report that costs a whole diagnostic run and invites a WRONG repair
(a GC/collector hunt, which is how this defect was reported to us).

## The fix

`internal/sessionid` (the same ONE leaf, no new regex — Rule 14):

- The refusal is now **"a uuid's 16 bytes in a non-canonical grouping"**, asked as
  ONE question: `CanonicalSpellingOf(id)` drops the dashes, confirms 32
  lower-case hex characters, parses them back through the uuid LIBRARY, and
  returns the library's own `String()`. `IsAdmissible` refuses exactly when that
  answer exists and differs from the operand. Predicate and hint therefore ask
  **the same question** and cannot disagree — an id that is refused always
  carries a reachable canonical spelling in its own refusal.
- **The blacklist shape is preserved, deliberately.** Every legitimate non-uuid id
  this tree carries is either the wrong length under the dashes
  (`t-1788146419`, `t-1788146419-2`, `01a11548`, `01a0a392`, a name) or carries a
  non-hex character (`01a11548-tracked`, `01a09aaa-dead-beef-cafe-000000000001`,
  `0000-emptydir`), so it never reaches the uuid parse and stays admitted. The
  nine-test breakage the first cut caused is the reason the rule is still a
  blacklist of what it can PROVE, not a whitelist of what it accepts.
- The refusal text names the canonical spelling for every refused grouping, so
  the repair is the same sentence whatever the mistake was.
- `canonicalSpellingOf` → **`CanonicalSpellingOf`** (exported) so a caller with
  its own refusal face can name the repair without spelling a second uuid rule.

### The table row that REVERSED, and the argument

```
- {"32 hex with a dash (not the mangled form)", "01a12c9f-5a1b2c3d4e5f60718293a4b5", true},
+ {"32 hex with a dash (a uuid's bytes, non-canonical grouping)", "01a12c9f-5a1b2c3d4e5f60718293a4b5", false},
```

The operand is 32 hex characters = a uuid's 16 bytes, badly grouped
(`01a12c9f-5a1b` + `2c3d` + `4e5f` + `60718293a4b5`), i.e. **precisely the shape
this change must refuse**: it is not a name, not a `t-…` channel id, and not any
id this tree mints — joining it mints `sessions/01a12c9f-5a1b2c3d4e5f60718293a4b5/`,
a container that is no row's. The old pin recorded the belief that "with a dash"
made it safe; the widened rule is exactly the discovery that it does not. The
same reversal applies to `{"uuid with the wrong grouping",
"01a12dc6-…-2382a4a7d35-2", true}` (a uuid with the dash moved one character
left, 33 characters with 5 dashes).

Every other row of that table is UNCHANGED and still true, which is the
non-over-blocking half of this change — the ids the tree really carries
(`01a11548-tracked`, `01a11548-ignored`, `01a11548`, `01a09aaa-dead-beef-cafe-000000000001`,
`0000-emptydir`, `t-1788146419`, `t-1788146419-2`, `t-worker-stale`, `01a0a392`,
`capture-round-probe`, the uppercase uuid, the empty string) are all still
admitted/refused exactly as before.

### Which surfaces carry it

The rule is applied at the seams that already shared it — no new call site was
added, so the blast radius is the one the first cut established:

- `internal/tools/taskSessionDir` (`TaskSessionDir`) → attachments, `task-report.md`,
  worker `output.log`, the signal-record destination, the executor cwd.
- `internal/agent/sessionDirForID` → the run directory, through
  `NewSessionLoggerForRun` (the caller that `MkdirAll`s it).
- `internal/signalrec.sessionDest` → the provenance record's destination.

And ONE new refusal face, added because the defect report starts there:

- `internal/tools.TaskShow` — `no task with task_id="<near-miss>"` now carries
  `"… it is a uuid's 16 bytes in a non-canonical spelling (dashes dropped or
  moved); did you mean "<canonical>"?"`. The reported run's FIRST command was
  `gopi task show <wrong spelling>`, whose bare "no task with task_id" read as
  "the row does not exist" rather than "your spelling is wrong". This is the same
  question from the same definition (`CanonicalSpellingOf`), phrased for the
  ledger's own refusal (Rule 14); an unknown id that is NOT a uuid's bytes keeps
  its ordinary answer with no invented hint (Rule 15).

## Verification

- **Red first (observed):** with the new table rows in place and the OLD predicate,
  `go test ./internal/sessionid/ -run TestIsAdmissibleIsTheShapeRule` failed with
  five rows reading `= true, want false` — including the live near-miss
  `01a12f96-c2bf-732f-8cd24b63a367d4af`.
- **Green after (observed):** the same command passes; `TestRefuseNamesTheRepair…`
  passes with the widened rows and asserts each refusal names the canonical id
  (`01a12f96-c2bf-732f-8cd2-4b63a367d4af`, `01a127d7-fdc3-…`, `01a0dff1-21cb-722e-…`,
  `01a12c9f-5a1b-2c3d-…`).
- **Seam tests (new):** `internal/tools TestTaskSessionDirRefusesANearMissGrouping`
  and `internal/agent TestSessionRunDirRefusesANearMissGrouping` — each asserts a
  REFUSAL (with the canonical id named) instead of a path, and that NOTHING was
  minted under the sessions root.
- **Ledger face (new):** `internal/tools TestTaskShowNamesTheCanonicalSpellingForANearMiss`
  — the near-miss names the repair, the named id resolves to the real row, and a
  non-uuid unknown id gets no invented hint.
- **Rule 31 (a real surface):** the built binary, run for real (see the task
  report's `## Verification`).

## Not done (honest)

- **The 22 live containers are untouched** — the brief forbade removing them, and
  removing a directory a human named is an operator decision (Rule 41: the
  reporter is not the remover). They are now *inert*: the gate refuses the
  spellings that made them, so no new one can appear. Their contents are the
  caller's own evidence trees, not gopi's.
- **The branch/ground spelling is still mintable by design**: `taskGroundIDStem`
  drops every non-alphanumeric character to build the branch ref and the ground
  directory. A ref cannot carry the row's grouping, so this is intentional — it is
  also what makes the two spellings easy to confuse. Removing the confusion at
  the source is a larger change with its own blast radius.
- **The READ seams named in the previous devdoc**
  (`internal/signalrec/signalrec.go` step 2, `internal/tools/taskFindSessionJSONLByTask`)
  are unchanged and remain inert for this defect: they do not mint directories.
  They now inherit the widened refusal automatically, since they call the same
  `IsAdmissible`.
