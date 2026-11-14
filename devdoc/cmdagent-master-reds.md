<!-- doc-class: historical -->
# The cmd/agent master reds — one list written twice, one index over budget

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch `fix/cmdagent-three-reds` (task `01a0b948`, base master `9e6d7844`).

## What an agent arriving here needs to know

The briefing named three reds; the tree carried six. The three extra ones are worth
knowing about because **each is a different shape of the same disease** — a value or a
rule with more than one home:

1. **A list written down twice.** `memoryCompletionNames` (tab completion) was a
   hand-typed copy of `memorySubcommandNames` (the dispatcher's own list). `backup` was
   added to one and not the other. The lock test compares the two, which is how a
   *duplicate* announces itself — but the durable fix is to delete the duplicate, not to
   keep syncing it. `memoryCompletionNames = memorySubcommandNames` is now a reference.
2. **A verb wired into dispatch with no help entry.** `MemoryBackupSubcommand` was in
   `cmd/agent/main.go`'s chain and in no `internal/config/help.go` row. Two halves are needed: the map entry in
   `usage_test.go` (built from constants, never a `"backup"` literal) and a real help row
   that the mapping's assertion can match.
3. **A parse re-inlined.** `readPPID` hand-rolled the `LastIndex(')')` + `Fields` read of
   `/proc/<pid>/stat`; `internal/procname` is its single home.

## The help budget: what moved, and why nothing was lost

`internal/config/help.go` renders `gopi -h`. Its own doc comment states the rule that
makes the 162→145 fix legitimate rather than a trim: *the top-level help is an index, and
the full contract lives in each command's own `-h`*.

So the fix moves the **enumeration** and the **flag lists** down one level and keeps every
**semantic** claim at the top:

- the `gopi task` row's verb list (8 rendered lines) → `gopi task -h`;
- the `gopi worker` / `gopi merger` narratives → their own pages, keeping `RETIRED` and the
  pointer to `gopi merge`;
- `voice_input` / `shot` flag lists → their own pages.

Two traps when editing this file:

- **`commandIndex` is a Go slice literal; the inner backticks are help text, not
  delimiters.** `internal/config/help.go` is not a raw string (unlike `cli_*.go` usage
  constants), so backticks are safe *here* — but a usage constant in `cmd/agent` IS a raw
  string, and an inner backtick there terminates it with a confusing error on a later line
  (memory #1353).
- **The rows are reflowed at render time.** Assertions must collapse whitespace
  (`strings.Join(strings.Fields(usage), " ")`) or they test the column arithmetic instead
  of the semantics (see `TestTopLevelUsageShowsTaskLedger`'s comment).

## The ledger DSN trap (cost: six red tests, measured)

`LedgerReadOnlyDSN` first reused `ledgerPragmas()` — the shared list with
`journal_mode(WAL)` and `synchronous(NORMAL)`. On a `mode=ro` connection SQLite **refuses**
those pragmas, and because go-sqlite delivers pragmas with the first statement, the refusal
surfaces on the *caller's* query:

```
artifact "…/memory.db.backup-….db.partial" is not usable (select count(*) from tasks):
attempt to write a readonly database (8)
```

Six `internal/tools` tests went red before the split. **A reader reads whatever journal mode
the writer chose**; only the shared busy timeout and the read-only guards belong on a
reader's DSN. Conversely `ledgerBackupVacuumInto` needs the WRITE-capable `LedgerDSN`,
because `VACUUM INTO` creates its output through the same connection and `mode=ro` refuses
that (`attempt to write a readonly database (8)`) — the file's own LOAD-BEARING DETAIL
comment already recorded this.

## The repair recipe (reusable)

```
# 1. reproduce each name on the untouched tree, one precise -run each
go test ./cmd/agent/ -count=1 -run 'TestCompletionCandidateSetsMatchGroupDispatch'
# 2. find the duplicate home before editing the assertion
keyword_search '<the list/const name>'      # >1 definition = the root cause
# 3. fix the single source, then re-measure the RENDER
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && ./bin/gopi -h | wc -l
# 4. prove RED on a detached worktree at the base commit (differential)
# 5. full package: job(action=start) — never a foreground bash call (~20 min)
```

**Measurement beats the briefing.** Five reds were reported load-flaky and two of the six
here were not in the briefing at all; the only way to tell them apart is to run each name
alone with `-count=1` and read the failure text.

## Correction to a later list (2026-09-25) — and one red the list could not see

A later master-reds row (`01a0d65f`, base `d3e94762`) reported 14 cmd/agent-family reds.
Two corrections to what it says are recorded in
**`tasks/master-reds-01a0d65f-corrections-report.md`** (both items in full there):

- its doc-anchor figure is **38 new + 13 stale permits** (not 34), and the rework branch
  adds **zero** new drift — both trees measure identically;
- it **omits** `cmd/agent TestParseTasksArgsSubmitRepoFlag`, which is red on pristine master
  in any LINKED worktree while green in the main checkout — **fixed**, and the shape, the
  one-command repro and the fix are written up in that same report (Correction 2). Read it
  before trusting a main-checkout green on a package that resolves a default from
  `os.Getwd()`. The shape, in short:

- **A test that compares a resolved default against `os.Getwd()` is RED IN EVERY
  LINKED WORKTREE and GREEN IN THE MAIN CHECKOUT.** The main repo and a run's
  ground are different directories sharing one object store, so a default that
  resolves to "the repo that outlives the run" differs from `cwd` exactly there.
  Two greppable shapes: an `os.Getwd()` compared to a parsed option, and any
  assertion on the pre-`01a0d55b` value of the durable-repo family
  (`tools.DurableRepoForSubmit` and its callers).
- **`gopi task run` / worker grounds ARE linked worktrees**, so a family's own
  branch-scoped suite runs sit in this blind spot by default — and the natural
  re-check (run it in the main checkout) comes back GREEN, which reads as
  "already fixed" rather than "not measured".
- **The one-command repro**, in any linked worktree you already have: drop the
  base commit's version of the suspect test file in (`git checkout <base> --
  <the test file>`), run the single test with `-count=1`, then restore it with
  `git checkout HEAD -- <the test file>` and confirm `git status --porcelain`
  is empty. Red there + green in the main checkout on the same commit is this
  class by definition.
