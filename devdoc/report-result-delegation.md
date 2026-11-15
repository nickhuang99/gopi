<!-- doc-class: normative -->
# The result-delegation record: a report that hands its gate conclusion to a path

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class:** the DOC-INDEX row for this file is `historical` (the corpus classifier
reads its dense run-record prose that way); it is nonetheless a **standing
description** of a check that runs on every close-out. The code it names lives in
`internal/taskreport/delegated.go` and `internal/tools/delegated_report.go`.

## The measured case

Row `01a12d6b` (2026-10-12) wrote this sentence into its `## Verification`:

```
- **Tests**: `make test-budget PKGS='./internal/testutil'` → see the stream at
  `artifacts/make-testbudget-testutil.txt`.
```

The sentence states **no result**. It hands the whole conclusion to a path. That
is not an accident of that run — it is a shape the done-gate chain *forces*:

> build → tests → **write the report** → `task(action=commit/review)`

The report is authored **before** the chain's last measurement can exist, so
"the report is written and the row is complete" and "the acceptance command has
no verdict yet" are the same state. In that run the acceptance stream afterwards
exited **2** with two red tests (`TestPinCoverageTable`,
`TestEveryTempUsingSuiteIsPinned`), and the report stayed exactly as written —
nothing at close-out looked at the sentence. The run noticed only because it had
put the acceptance command in a background job and still had a round left to
read it.

The finding is not "the report lied". It is: **the only place that run's
acceptance conclusion lived was a path, and a path proves nothing by itself.**
A reviewer reading `see the stream at <path>` cannot distinguish a green
acceptance from one that was never green — and, before this change, the path was
not even required to exist.

## What is delivered

### The leaf predicate — `internal/taskreport/delegated.go`

`DelegatedClaimsIn(body)` returns the sentences that **delegate** a gate result
to a path while stating none, each with the paths it names. The predicate needs
**four** things at once:

1. an **instrument** — `make `, `go test`, `go build`, `gopi `, `./bin/gopi`, …
2. a **delegation phrase** — `see the stream at`, `see `, `refer to`, and the
   Chinese spellings this repo's operator-facing reports use (`见 `, `详见`, `参见`);
3. a **path token** shaped like an artifact — carries a separator and a short
   extension, is not a URL (so `./bin/gopi` and `./internal/testutil` — the
   instrument's own argv — are not artifacts);
4. **no result** in the sentence's own prose.

Clause 4 is judged on the sentence **with its path tokens removed**, because a
filename is not a verdict: `artifacts/red-capture.txt` is how a run names a red
proof it *did* state elsewhere.

### The filesystem half — `internal/tools/delegated_report.go`

`DelegatedClaimsIn` is pure text (the leaf contract). `os.Stat` lives in the
caller, which measures **every path at seal time** and stamps the result into the
record:

| status | meaning |
|---|---|
| `ABSENT — no such file at seal time (dangling delegation)` | the report hands the reader to a file that is not there; the conclusion is **unrecoverable** |
| `present, N bytes (recoverable — the report states no result itself)` | the artifact exists; the delegation is checkable, and the record says only that |
| `status UNKNOWN — stat failed: <err>` | existence could not be established (Rule 15: the unknown is stated, never invented) |

A `~/…` token is deliberately **not** resolved — guessing a home directory for a
path a report wrote would measure the wrong file — and a directory is reported as
a directory, not as an artifact.

### The action: RECORD, and nothing else

Identical to the three sibling records this seam already carries
(`gateclaim.go`, `runcheck.go`, `master_claim_report.go`): the finding goes to

- the ledger's **append-only digest** (prefix `report-result-delegated: `),
- **stderr** at close-out,
- `task show`'s `task_report_delegated: ` render,

and the report is **never rewritten** and the seal is **never refused**. Rule 31's
own measured justification (`devdoc/rule31-ran-check.md`: a refusal keyed on that
predicate would refuse 71% of this repo's real reports) applies here with the
same force, and this predicate is narrower still.

## Two measured false positives the predicate had to survive

Both were found by running the predicate over a real corpus, not by reasoning
about it — and both fixes are pinned by tests in `internal/taskreport/delegated_test.go`:

1. **A pasted shell script** (session `01a0dec6`) — a fenced `Ran:` block listing
   `GOPI_HOME=$SB bin/gopi … /tmp/fx/nope.txt` looked like a delegation to three
   paths. A fence holds a **transcript**, not the report's prose about an
   outcome, and Rule 31's `Ran:` block is the surface being *executed*: flagging
   it would punish the behaviour the rules prescribe. ⇒ fenced blocks are
   skipped (`stripDelegatedFences`).
2. **A passing aside after a stated result** (session `01a10f9c`) —
   `` `./bin/gopi tool doccheck` → prints the pre-existing `normative-no-check
   devdoc/…md` gap and `1 gap(s)` (see Not Done). `` states its result and then
   says "see Not Done" in passing, while a path token sits *earlier* in the
   sentence. What a delegation hands over sits **after** the phrase. ⇒ the path
   must fall within a bounded window after the earliest delegation phrase.

The predicate's whole-corpus result, measured on this host on 2026-10-12 over
**372** real reports under `~/.gopi/sessions/`:

```
CORPUS reports=372 hitFiles=1 hits=1
HIT 01a12d6b-b00a-7d1d-9477-1be79b5a8fe7 :: [artifacts/make-testbudget-testutil.txt]
```

One file, one sentence: the motivating defect, and nothing else. (The
four-hit/369-report measurement before the two fixes above is what produced
them.)

## A measurement correction that mattered

`DelegatedClaimsIn` adjudicates the **union** of `## Verification` and
`## Evidence (self-filed)`, not the first present section. The sibling checks
pick one section, and copying that choice here produced `claims=0` **on the
motivating report itself** — because that report carries *both* sections and its
delegating sentence lives under Verification while the evidence section is
present and non-empty. A check that cannot fire on the case that produced it is
not a check. A body with neither section is still adjudicated on its whole body,
so deleting a heading cannot dodge it.

## Verification (the gates this branch actually ran)

See `tasks/01a12d81-report.md` in this repo, and the raw streams under
`~/.gopi/sessions/01a12d81-f6da-724f-a3a9-46946817de11/artifacts/`.

- **A/B red proof**: `internal/tools/zz_redproof_delegated_test.go` compiles and
  runs against **both** trees (pre-fix API only). On a detached worktree at
  master `1508e3821` it FAILS (`UNRECORDED DELEGATION: …`) and `PRE_FIX_EXIT=1`;
  on this branch it PASSES.
- **E2E through the REAL close-out**: `cmd/agent/cli_tasks_delegated_seam_test.go`
  drives `gopi worker <task-id>` with a mock model that writes the report to
  `$GOPI_TASK_REPORT_PATH` — the red arm asserts the finding reaches worker
  stderr, the digest and `task show`; the green arm asserts silence when the
  result IS stated.
- **The read face on the real row**: `gopi task show 01a12d6b-…` renders
  `task_report_delegated:` with the sentence and
  `artifacts/make-testbudget-testutil.txt — present, 3508 bytes`.

## Why this is not a new dependency (Rule 35)

Nothing resident, scheduled or configured is added: no cron line, no daemon, no
state file, no new config key, no new CLI verb. The check is a pure function
called by a seam that already runs on every close-out, and its record rides a
digest the ledger already appends to.

## Honest limits

- **It does not know whether the delegated command was green.** It cannot: the
  artifact is a file, and reading it would be a second, unevidenced
  interpretation (and re-running the gate is precisely what this family refuses
  to do). What the record buys is that the *state* — a conclusion that exists
  only as a path — stops being invisible.
- **It fires on the shape, not on the intent.** A report may legitimately point
  at a stream while stating the result elsewhere in the same report; that is why
  clause 4 is judged per sentence, and why a report whose sentence carries
  `exit=0` / `2 red` draws nothing.
- **The corpus figure is a census** (Rule 38): generator = a `DelegatedClaimsIn`
  sweep over `~/.gopi/sessions/*/task-report.md` (a temporary probe test in
  `internal/taskreport`, removed after measuring); **measured at commit
  `4a535c2ab`** on this host. It is a count of *this host's* session directory
  (372 reports on 2026-10-12), not of the repo.
