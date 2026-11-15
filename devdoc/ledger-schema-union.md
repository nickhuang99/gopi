<!-- doc-class: normative -->
# The shared ledger's shape is the union of its index faces

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**The rule this document states.** One database file holds several index faces —
the knowledge base (`distilled_qa`, `memory_feedback`, the provenance columns),
the tasks ledger, the chat index, the round-note index. `chatDBPath()` defaults
to `memoryDBPath()`, so by default they are the SAME file. Therefore **the
opener that creates that file must create the union of the faces**, and an
opener that meets a file carrying only gopi's own objects must **adopt** it.
Whoever opens first must not decide what the file is.

## The defect, measured

On `3b1a158`, with `GOPI_MEMORY_DB` pointing at a fresh custom path (so the
default-path heal does not cover it):

```
gopi session list   → exit 0
  the file is left carrying 10 objects, ALL chats family:
  chats, chats_fts(+config/data/docsize/idx), chats_tri(+config/data/docsize/idx),
  idx_chats_mode, idx_chats_started — and zero distilled_qa

gopi task list      → exit 1
  gopi task: …/custom-ledger/memory.db is not a distilled knowledge base
             (missing distilled_qa table)
```

and it stays exit 1 for ever: the chat opener had already published the file as
a chats-only database, and `openTasksDB`'s refusal is *correct about what it can
see*. Every `gopi task *`, `gopi memory *`, `gopi worker` invocation on that path
is dead. The same shape produced a whole-package test failure in `cmd/agent`
(order-dependent: the passing `-run` single test, the failing package).

## Why not simply heal on the tasks side

`openTasksDB` already heals the DEFAULT path (`isDefaultMemoryDB` →
`ensureKnowledgeBase`). The gap is the CUSTOM path (`GOPI_MEMORY_DB=/tmp/x.db`,
a peer's ledger, a task's pinned `--db`), where the same code must also keep
refusing **foreign** databases — hermes' production `distilled_knowledge.db`
carries `distilled_qa` but no `memory_feedback`, and gopi must never create
tables inside it (`tasks.go`'s "never mutate foreign DBs", extended from rows
to schema; it is what makes that file read-only compatible).

So the question "may I complete this file?" cannot be answered by a table name
and cannot be answered by a path. It has to be answered by **the file itself**.

## The three shapes, and the three answers

`openLedgerIndexDB` is the ONE door for every face that shares the file
(`openChatDB`, `openChatNotesDB`; `openTasksDB` asks the same question inline
because its surrounding refusal logic is its own):

| the file | answer |
|---|---|
| already carries `distilled_qa` | open it; land this face's own objects (the ordinary fast path — unchanged behaviour) |
| ABSENT, or carries ONLY gopi's objects | build the **union** (`ensureKnowledgeBase`) then this face's objects — every CREATE is `IF NOT EXISTS`, so existing rows survive |
| carries ANY object gopi did not mint | **touch nothing beyond this face's own tables** — the pre-fix foreign behaviour, deliberately unchanged |

The middle row is the fix; the third row is the constraint that must not move.

## How "gopi's own objects" is decided — a measured reference set, never a prefix list

The first draft answered this with a **name prefix list** (`tasks`, `chats`,
`distilled_qa`, …). That was wrong, and its own test caught it before the commit:
`isGopiLedgerObject("tasks_archive_legacy_unknown")` answered **true**. A prefix
list accepts any third-party database whose application happens to own a
`tasks` table, and adopting such a file would build a knowledge base *inside
somebody else's DB* — the exact mutation the refusal exists to prevent.

The shipped answer is `gopiLedgerObjectNames(ctx)`: the **exact set of object
names gopi's own openers mint**, MEASURED once per process by building every
face's schema into a throwaway `:memory:` database through the SAME ensure
functions production calls (`ensureSchema`, `ensureFeedbackSchema`,
`ensureOriginSchema`, `ensureTasksSchema`, `ensureChatSchema`,
`ensureChatNotesSchema`, plus the two faces whose schema is built LAZILY by
their own openers rather than by the first-open build — `ensureProceduralSchema`
and `ensureSessionConclusionsSchema`; a ledger that already carries one of those
must not read as foreign). Two consequences worth stating:

- A new index face joins the reference **by existing** — no list to remember,
  no second source of truth for the DDL (Rule 14).
- `TestLedgerUnionRecognizesEveryGopiObject` compares that set against a REAL
  built ledger **in both directions** and turns red on either drift: an object
  the ledger carries but the reference lacks (the adoption census would call
  gopi's own file foreign) and a reference name no real ledger carries (an
  accidental permit for a foreign DB that happens to use that name).

The failure direction is deliberate: a reference build that fails is an **error**
and the caller then **declines to adopt**, which is the pre-fix behaviour
(refuse the file). A database-shape decision must fail towards "touch nothing".

## What this does NOT change

- The default-path heal (`isDefaultMemoryDB`) is untouched — it still re-initializes
  a schema-less default `~/.gopi/memory.db`.
- `openSearchDB` / `openStoreDB` are untouched: they are knowledge-base faces, and
  a chats-only file is not what they meet in practice.
- The foreign-distilled refusal (`distilled_qa` present, `memory_feedback` absent)
  is byte-for-byte the same message, and the file is byte-for-byte unchanged:
  measured `sha256` identical before and after the refusal.
- Nothing new is scheduled, installed or made resident (Rule 35): the mechanism is
  three functions in `internal/tools/ledger_schema_union.go`.

## Cost

A file that must be built as a union pays the FIRST-OPEN build (the measured
median ~131 ms under WAL, `firstopen_probe_test.go`), not the old chats-only
build. That is the same build the tasks ledger pays today on a fresh profile, and
it now happens at whichever face opens first — which is the point.

## Where the checks live

```
go test ./internal/tools/ -run 'TestChatOpenerThenTasksOpenerOnAFreshLedger|TestChatsOnlyLedgerIsAdoptedByTheTasksOpener|TestChatsOnlyLedgerIsHealedByTheChatOpener|TestLedgerUnionLeavesForeignFilesAlone|TestHermesShapedLedgerIsStillRefused|TestLedgerUnionRecognizesEveryGopiObject|TestLedgerObjectCensusDistinguishesShapes'
```

The first three FAIL on the pre-fix openers (observed red at `3b1a158` with the
three opener files stashed) and PASS with the union door; the foreign-file and
hermes-shape tests PASS on both, which is what makes them boundary pins rather
than regression pins.

## Reproducing the real-binary comparison

```bash
SB=<a sandbox from `gopi tool sandbox create --label chatopener`>
# BEFORE (3b1a158)
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/custom-ledger/memory.db" gopi session list   # exit 0
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/custom-ledger/memory.db" gopi task list    # exit 1
# AFTER
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/fixed-ledger/memory.db"  gopi session list   # exit 0
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/fixed-ledger/memory.db"  gopi task list    # exit 0
# and the file the BEFORE run left behind is ADOPTED, not refused:
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/custom-ledger/memory.db" gopi task list      # exit 0
```

Inspect the shape with an object census (`SELECT type, name FROM sqlite_master`),
never with a filename: the union file carries 58 objects, the pre-fix one 10.
