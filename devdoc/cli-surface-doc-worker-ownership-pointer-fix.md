<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
<!-- Disposition: the fix report for the gate breach it describes — the command shapes
     below are QUOTED as the gate's needles and as the offending file's table row, not
     taught as a launch recipe. -->

# Devdoc: master-red fix — the CLI-surface report's worker-ownership pointer

**Branch**: `fix/cli-surface-doc-worker-ownership-pointer` (worktree
`~/workspace/gopi-wowner-ptr`, off master `b3761dc5`, GOPI.md Rule 6a).

## What broke

Master `b3761dc5` was red on `internal/testutil`'s
`TestWorkerOwnershipPointerPresent`:

```
--- FAIL: TestWorkerOwnershipPointerPresent (0.04s)
    worker_ownership_gate_test.go:365: files that teach a worker-launch / task-run command
    without the operator-only pointer:
          devdoc/cli-task-surface-restructure-20260915.md
```

**Provenance** (not the user-bus epic, despite the timing): the file arrived
with the sibling branch `feat/cli-task-surface-restructure` (master `3e76cb43`)
and was never annotated. The gate was widened to `.txt` by `fa1a1e7a`, and the
user-bus fix (`b3761dc5`) repaired the four `.txt` captures but not this `.md`.

## Why it needs the shape at all

The report is a **usage-surface record**, not a runbook — that is the reason the
disposition note (not a bare pointer) was added, per the operator ruling:

1. its live-probe table has a row for `run` on the ledger surface, and the
   recorded EVIDENCE in that row is the guard **refusing** the move
   (`refusing to start: this process is inside an agent session`) — the denial is
   the measurement;
2. the rest of the matches describe which usage HEAD advertises the worker verb
   as part of the restructure rationale.

No fenced code block in the file teaches a launch:

```
$ grep -nE 'gopi worker|gopi task run|gopi-worker-up\.sh' devdoc/cli-task-surface-restructure-20260915.md
63:  | `run` | … `gopi task run` → `refusing to start: this process is inside an agent session` … |
81:  … (`gopi worker` heads).                       # prose: which synopsis heads were removed
110-112: … `gopi worker` → never a family Usage block …   (prose)
140: … *TestLedgerGroupTeachesNoServiceBlock* — rejects a bare `gopi worker`/ … (test-name prose)
171/182: the red-proof injection quotes … (verification transcript, prose)
258/311: the "Not Done" note and the acceptance line (prose)
```

Every hit is a table cell, a prose sentence or a transcript line — so the
pointer is **not** a contradicting instruction here, and the disposition
paragraph in the file says exactly why.

## The fix

One markdown comment pair as the file's first lines (the file's language
comment form), the second comment stating the disposition — the shape the
user-bus captures and `devdoc/cli-surface-audit-20260915.md` use, plus the
"why this is not a recipe" reason the operator ruling requires:

```
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
<!-- Disposition: … a USAGE-HELP/grouping record, not a launch recipe … -->
```

`workerOwnershipPointerLine` matches a single line carrying BOTH the marker
(`WORKER-OWNERSHIP: OPERATOR-ONLY`) and the canonical path
(`devdoc/worker-ownership.md`); the disposition lives on its own comment lines
so the pointer line stays a clean one-liner (the gate's own expectation).

**A dismiss entry was considered and rejected**: `workerOwnershipDismissed`
exists for files whose mention is evidence *and* which should not point a
reader anywhere — but a reader of this doc who greps a command shape SHOULD be
told who owns it, so the pointer is the honest disposition, not a suppression.

## Verification

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -count=1 -run TestWorkerOwnershipPointerPresent
ok  	github.com/nick/gopi/internal/testutil	0.661s          # was FAIL before the edit

$ CGO_ENABLED=0 go test ./internal/testutil/ -count=1
ok  	github.com/nick/gopi/internal/testutil
```

## Trap worth remembering

The gate's needles are `gopi worker`, `gopi task run` and the launcher script
name — a **table row or a prose sentence** matches, so a doc that merely
*mentions* the move (a usage-audit, a review record, a transcript) is in scope
even when it teaches nothing. Two dispositions are legal (pointer, or dismiss
entry), and a bare pointer on a file that does not teach the shape is a false
instruction — hence the disposition comment.
