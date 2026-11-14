<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The 2026-09-12 audit's §7 location list — location → status

**Status: historical record. This file reproduces the location list of the
ruling's audit, verbatim in its rows, and states what happened to each one.**
It exists so that `.txt` transcripts and files that live OUTSIDE this branch
still have a machine-checkable disposition (the gate's dismiss table can only
name files that exist in the tree it walks).

The rule itself — who owns the worker and what an agent does instead — is NOT
here: it is the ONE canonical block, `devdoc/worker-ownership.md`.

- Source of the list: `devdoc/pa-never-runs-tasks-20260912.md` §7.1 / §7.2
  (lives on the branch `feat/pa-never-runs-tasks`, commit `2583d7d4`; **not on
  master** at this branch's base `09402611`).
- Machine check for the rows that ARE in this tree:
  `internal/testutil/worker_ownership_gate_test.go`.
- The rows below are **quoted, not rewritten** — no audit record is deleted or
  edited by this task (Rule: history intact, additions only).

## §7.1 — scripts that actually start a worker/task-run

| §7.1 location | Status | Evidence |
|---|---|---|
| `scripts/gopi-worker-up.sh` (repo) | **annotated** (operator-only header + pointer) | `scripts/gopi-worker-up.sh:2-7`, gate anchor `#!/usr/bin/env bash` |
| `~/.local/bin/gopi-worker-up.sh`, `~/bin/gopi-worker-up.sh` (symlink) | **out of tree — operator action** | deployed copy differs from the repo script; it predates the banner. The repo script's `--install` (line 264, `install -m 0755 "${BASH_SOURCE[0]}" "$stable"`) re-deploys it: **the operator runs `scripts/gopi-worker-up.sh --install` once** to carry the header to the live path. An agent must not run that (it is the same script) |
| `~/.gopi/glm-night-watch.sh:38` | **out of tree — operator action** | `$HOME` file, untracked, not this repo's deliverable. Its `ssh hp "… setsid nohup ./bin/gopi worker …"` loop is an A-class scripted start: the operator should paste the pointer header from `scripts/gopi-worker-up.sh`. A repo-side consumer was *not* found (`fix/glm-night-watch-delegates` does not touch it either) |
| `cmd/agent/dispatch.go:332-336` | **annotated** (remote-peer exception comment + pointer) | `cmd/agent/dispatch.go:331-336`, gate anchor `func dispatchRemoteCommand(` |
| `scripts/e2e-m421-claim-lock.sh:127` | **annotated** (test-harness note + pointer) | `scripts/e2e-m421-claim-lock.sh:128-134`, gate anchor `CLAIM_OUT=$(GOPI_HOME` |

## §7.2 — devdocs/briefings whose text teaches an agent to spawn a worker

| §7.2 location | Status |
|---|---|
| `devdoc/m420-worker-loop.md` | **annotated** (pointer below the title) |
| `devdoc/bugfix-worker-idle-exit-20260908.md` | **annotated** |
| `devdoc/bugfix-worker-help-20260902.md` | **annotated** |
| `devdoc/review-cli-task-sync.md:18` | **annotated** |
| `devdoc/gopi-home-hygiene.md:25,66-73,178-182,279` | **annotated** (above the launcher paragraph) |
| `devdoc/nightshift-20260911.md:45` | **annotated** |
| `devdoc/OVERNIGHT-SUMMARY-20260910.md:157,231` | **annotated** |
| `devdoc/night-report-20260910.md:116,121` | **annotated** |
| `devdoc/hook-investigation-report.md:34` | **annotated** |
| `devdoc/task-ledger-m46-47.md:79,100` | **annotated** |
| `devdoc/m47-e2e-report.md:7` | **annotated** |
| `devdoc/manual-taskrun-recycle-20260903.md` | **annotated** |
| `devdoc/server-side-worker-recycle-20260904.md` | **annotated** |
| `devdoc/worker-worktree-recycle-investigation-20260908.md` | **annotated** |
| `devdoc/run-method-provenance.md` | **annotated** |
| `devdoc/cli-command-audit.md` | **annotated** |
| `devdoc/salvage-20260912-0412-incident.md` | **annotated** (above the incident line) |
| `devdoc/worker-cohort-kill-20260912.md` | **annotated** |
| `devdoc/worker-count.md` | **annotated** |
| `briefings/brief-worker-idle-exit.md:43,45` | **annotated** |
| `briefings/brief-worker-process-independence.md` | **NOT IN THIS TREE** — untracked in the main checkout, present in no commit (`git log --all -- <path>` is empty) |
| `briefings/brief-worker-cohort-kill.md` | **NOT IN THIS TREE** — untracked, same as above |
| `briefings/brief-worker-daemon-flag.md` | **NOT IN THIS TREE** — untracked, same as above |
| `briefings/brief-worker-up-miscounted-live-count.md` | **NOT IN THIS TREE** — untracked, same as above |

## §7.3 — deliberately NOT listed (references, not instructions)

Reproduced for completeness: `GOPI.md` (Rule 6a describes PA *behaviour*, not
a worker launch), the historical `tasks/*-report.md`, and the source-level
`run_cmd` mentions that describe ledger provenance. The gate dismisses 44 such
files individually (`workerOwnershipDismissed`) — it dismisses by NAME with a
reason, never by a silent path pattern.

## What "NOT IN THIS TREE" means, and how the gap closes

The five files in that status (the ruling `devdoc/pa-never-runs-tasks-20260912.md`
is a sixth, kept off this list because §7.1/§7.2 are reported here) are absent
from `master`, so a worktree created from `master` (Rule 6: never develop in
the main checkout) cannot read, edit, or meaningfully dismiss them. The gate
deliberately does **not** carry dismiss entries for missing files: an entry for
a nonexistent path is a stale permit that would redden the gate the moment the
file lands.

They are therefore handled by **content**, not by a list: the moment any of
them is committed, `TestWorkerOwnershipPointerPresent` sees it (it matches
`gopi worker` / `gopi task run` / `gopi-worker-up.sh` in every `.md`, `.sh` and
`.txt` under the three audited trees) and it fails until it carries the pointer.
The port is mechanical:

1. merge/cherry-pick `feat/pa-never-runs-tasks` into the branch that carries
   the gate;
2. add the pointer line to `devdoc/pa-never-runs-tasks-20260912.md` §7 (the
   audit's OWN header: it quotes the forbidden commands, so it needs the
   pointer most);
3. run the gate — it will name each of the four briefings until each carries
   the pointer;
4. add `devdoc/pa-never-runs-tasks-20260912.md` to `workerOwnershipDismissed`
   ONLY if the ruling's own text is judged the owner of the subject rather
   than an instruction (that judgement is recorded in the report's table).

## No deletions

This file is an addition, like every other file this task touches.
