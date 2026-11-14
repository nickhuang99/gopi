<!-- doc-class: historical -->
# Master-red fixture: TestTaskIDWhitespaceNormalizedAllAccessors

## Root-cause verdict: FIXTURE, not a production over-check

`internal/tools/taskid_whitespace_normalize_test.go` seeded a ledger row and then
called `TaskTagResumed(taskIDWSSeed, " sess-ws-2 ")`. The F16.1 sibling change
(`internal/tools/task_resumed_session.go`) made `TaskTagResumed` validate its
session operand against the session artifacts this profile actually wrote —
`sess-ws-2` names none, so the call was refused and the test failed at line 104:

```
--- FAIL: TestTaskIDWhitespaceNormalizedAllAccessors (0.04s)
    taskid_whitespace_normalize_test.go:104: TaskTagResumed: task: tag-resumed:
    session id "sess-ws-2" names no session of this profile (no session log, raw log
    or evidence directory on disk) — nothing tagged, and a row's resume lineage must
    name a real session
```

### The four pieces of evidence that decided it

1. **The check cannot fire on a real caller — the producer runs first.** The ONE
   in-product caller is `cmd/agent/cli_resume.go:642`, which stamps
   `ag.SessionLog.SessionID()`. The session logger has already created that
   session's JSONL at that point (`NewSessionLoggerForRun` writes before
   `runResume` reaches the stamp), so the artifact is on disk before the probe
   reads it. The ordering is a property of the caller, and the caller satisfies
   it — the check is not standing in the product's way.
2. **The check is satisfiable by the fixtures that were written for it.** The six
   cases of `TestTaskTagResumedAcceptsEveryRealArtifactShape` (task-run JSONL,
   raw-log-only, bare-session dir, legacy flat, another task's run dir, short-id
   expansion) all pass with the validation intact. Only the pre-existing fixture
   that never laid an artifact down failed.
3. **That fixture is older than the check.** It was written for the
   whitespace-normalization epic; F16.1 added the validation afterwards, and the
   fixture was never updated to lay down what the funnel requires. A test that
   predates a validation and never taught it the new precondition is a stale
   fixture by construction.
4. **The fixture was not hermetic in its own right.** It pinned `GOPI_MEMORY_DB`,
   `TMPDIR` and `GOTMPDIR` but **neither `HOME` nor `GOPI_HOME`**, so
   `config.GopiDir()` resolved — and the probe walked — the operator's live
   `~/.gopi/sessions`. That is a Rule 32 violation independent of the red: a
   fixture reading a profile it does not own.

The refusal's own shape is also the correct one, and is NOT the defect: it names
the operand (`%q`) and writes nothing to the row's tags. An audit stamp that any
string satisfies is not an audit stamp (Rule 15), and F16.1 exists precisely
because the all-zero uuid used to be stamped silently with exit 0. The
validation is kept intact; the fixture was made truthful.

## The fix

`internal/tools/taskid_whitespace_normalize_test.go` only — **no production file
was touched.**

- **`taskIDWSSandbox`** now pins the ledger through the ONE scaffold
  (`testutil.NewTestDBPath`, Rule 32 — a fresh per-test `memory.db` under the
  pinned scratch root, on the device the suite was pinned to, masking the legacy
  `GOPI_MEMORY_DB` spelling), and pins `HOME`/`GOPI_HOME` plus `TMPDIR`/`GOTMPDIR`
  at a `testutil.TestScratchDir` root (off `/`, Rule 22 — not a hand-rolled
  `t.TempDir()`). It then **asserts** the pin: `config.GopiDir()` must equal
  `<root>/.gopi`, so a fixture that drifted back to the live profile fails loudly
  instead of quietly reading it (原则无效，检查才有效).
- **`taskIDWSSessionArtifact`** lays down the real artifact shape — the
  bare-session layout `sessions/<session-id>/session.jsonl` whose first line is
  the `session_start` event carrying the id — with the path assembled from the
  SAME constants the production probe uses (`config.GopiDir` +
  `config.SessionsDirName` + `sessionlayout.RecordName`), so fixture and check
  cannot drift apart.
- **The session leg became an exact/padded comparison.** The artifacts are seeded
  **unpadded**; the operand `TaskTagResumed` receives is padded on both sides.
  Only the trim reaches the artifact, so the check is genuinely EXERCISED (never
  bypassed) and the pad is the single variable. The unstamped extra call the old
  fixture made (`TaskTagResumed(taskIDWSSeed, " sess-ws-2 ")`) is now a stamped,
  asserted one.

**No assertion was weakened and no test was skipped** (Rule 27): every original
assertion is present, the exact tag content is asserted (`resumed:sess-ws-2` —
i.e. the stored id is the TRIMMED one), and both new fixtures add coverage rather
than relax it.

## Regression test (the red half)

`TestTaskIDWhitespaceNormalizedRefusesAnUnknownSession` pins the preserved
behaviour: an operand naming no artifact is refused, the refusal names it, and
**nothing is written** (tags byte-identical before/after, resume count unchanged,
and the padded task id still resolves the row). A real artifact for a DIFFERENT
session is laid down first, so the refusal cannot pass merely because the
sandbox tree is empty. Deleting the validation fails it (see Evidence).

## Not Done

- Nothing deferred. The acceptance surface, the regression test and the report
  are all in this branch.
- Honest scope note: this is a **fixture** repair. The session-existence
  validation itself was not reviewed for correctness beyond confirming that its
  ONE production caller satisfies its precondition — that caller/check ordering
  is the evidence in (1) above, not a new audit.
