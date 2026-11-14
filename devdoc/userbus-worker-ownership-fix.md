<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Devdoc: the user-bus diagnosis' doc captures and the worker-ownership gate

**Branch**: `fix/userbus-doc-worker-ownership` (stacked on master `322daf81`,
after `feat/daemon-userbus-diagnosis` was merged as `975deee2`).

## What broke

`internal/testutil`'s `TestWorkerOwnershipPointerPresent` audits `devdoc/`,
`briefings/` and `scripts/` for **`.md`, `.sh` and `.txt`** files whose text
mentions a worker-launch / task-run shape, and requires each such file to carry
the canonical pointer line
`WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md` (or a reasoned
entry in `workerOwnershipDismissed`).

The user-bus epic captured the before/after operator output of a REAL
`gopi worker --daemon` run — which is exactly a worker-launch shape — and
committed those captures as **`.txt`**. The gate audits `.txt`, so master went
red the moment the branch merged: four files teaching the shape without the
pointer.

## What was done

1. The four captures are now **markdown** (`devdoc/userbus-diagnosis-probes/
   *.md`), each opening with the pointer and holding the verbatim transcript
   inside a fenced block. The file's own note records that the run was an AGENT
   probe with `GOPI_ALLOW_AGENT_SPAWN=1` (the bypass the daemon epic's own tests
   use) — a diagnostic capture, not a launch recipe.
2. `devdoc/raw-prompt-worktree-release.md` was **also** missing the pointer. It
   was already red before this epic; the gate's report names only the first
   `workerOwnershipMaxReportedHits` hits, so it had been hidden behind the
   truncation cap. Fixed in the same commit.

## Trap worth remembering

The gate's scope includes `.txt`, not just markdown — so "commit the raw output
as a plain text capture" is a gate violation even though the file is obviously
not a runbook. Any captured CLI output that contains a worker/task-run command
line needs the pointer (markdown inside a fence is the cheapest honest form), or
a reason in the dismiss table.
