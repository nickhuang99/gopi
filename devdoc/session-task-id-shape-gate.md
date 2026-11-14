<!-- doc-class: normative -->
# The task-id shape gate on the session-directory seams

**Status:** landed (branch `task/01a12f3cd888720eb7b5f458f55e98aa`, 2026-10-12).
**Bug row:** `01a12f3c-d888-720e-b7b5-f458f55e98aa` (`--tags bug`, filed from the
`01a12f22` investigation).

## The defect

One task row has more than one spelling in this project, and only some of them
are a *task id*:

| spelling | who mints it | what resolves it |
|---|---|---|
| `01a12dc6-f44d-7347-9b95-2382a4a7d352` | `taskIDGenerate` (uuid v7) | the ledger, `gopi task show`, the session directory, `GOPI_TASK_RUN_ID` |
| `01a12dc6f44d73479b952382a4a7d352` | `taskGroundIDStem` — it drops every non-alphanumeric character to build the ground directory and the branch ref | git only |
| `01a12dc6` | a rendered listing (`internal/fullid` exists because 2409 rows share prefixes) | nothing — a prefix is never resolved |

The **ledger side was always safe**: a mis-spelled id resolves to no row
(`gopi task show 01a12dc6f44d…` → exit 1). The **filesystem side was not**.
Both session-directory seams normalized with nothing but `strings.TrimSpace`
and then `filepath.Join`'d the operand:

- `internal/agent/session.go` — `sessionDirForID`
- `internal/tools/task_session.go` — `taskSessionDir`
  (and every path through it: attachments, `task-report.md`, worker
  `output.log`, the signal-record destination, the executor cwd)

So the wrong spelling was not an error. It was a **new directory**. Measured on
this host (2026-10-12, evidence `08-residue-census.txt`): **11 dash-stripped
containers beside 1334 canonical ones**, every one of them with a live canonical
sibling, and **zero gopi-written files** inside the 11 — they hold a caller's own
`artifacts/` tree and nothing else. The case that surfaced it (task
`01a12dc6`) had a caller reading its evidence out of the directory gopi never
wrote.

## The fix — one shape rule, referenced by both seams

`internal/sessionid` (new leaf: stdlib + `internal/fullid`):

- `IsCanonical(id)` = `fullid.IsUUID(id)` — **not** a second uuid regex
  (Rule 14: `internal/fullid` is the ONE definition of "this id is a full uuid").
- `IsAdmissible(id)` — **a blacklist of what it can PROVE is a mangled uuid**:
  a dashless 32-character lower-case hex run (the branch/worktree spelling) is
  refused; everything else is admitted. This shape — not a whitelist — is the
  load-bearing decision, and it was forced by measurement (below).
  **WIDENED 2026-10-12** (bug row `01a12fd9`, 22 live containers): the refusal is
  now "a uuid's 16 bytes in a NON-CANONICAL GROUPING" rather than "no dash at
  all" — a uuid with ONE dash dropped passed this rule and minted a second
  container. See `devdoc/session-task-id-shape-gate-widened.md`. The blacklist
  shape is unchanged; only what it can PROVE widened.
- `Refuse(id)` — one operator sentence naming the near-miss: the 32-hex run is
  parsed back through the uuid library (never re-dashed by hand), so the refusal
  hands the caller the ROW id it meant. (A shortened uuid head is not gated at
  all — see above — so no prefix sentence is written; had it been, `fullid.Refusal`
  already owns that verdict and its wording.)

Both seams call it, so the two sides of the agent/tools boundary cannot
disagree about what a task id is:

- `sessionDirForID`: refuses when `taskID != ""` and inadmissible. The empty-id
  case is unchanged (it still resolves the sessions root — see
  `TestCovTaskSessionDirEmptyIDBoundary`).
- `taskSessionDir`: refuses **after** `taskIDNormalize` and **before** the
  legacy-container migration and the join, so the alarm faces
  (`NotifyTaskFailed`, the CLI's fallback print) that call it with an unknown id
  in order to *report* a failure are never turned into a second failure.

### Why a blacklist, and not a whitelist (measured, this is the design decision)

The first cut admitted exactly two shapes — a canonical uuid, or a `t-…`
channel id — and it **broke nine real tests**: every one of them failed with
`task session dir: task id shape: "01a11548-tracked" is not a task-id spelling
this tree mints`. The operand was legitimate. This tree really does use
task-id-shaped strings that are not uuids:

- tests: `01a11548-tracked`, `01a11548-ignored`, `01a09aaa-dead-beef-cafe-000000000001`,
  `0000-emptydir`, `01a0a4ee-own-session`, `t-cov-shape`;
- production vocabulary: `internal/scratch/base_residue.go` calls a dashless hex
  token "a task id" (`01a0a392`, in `gocache-01a0a392`), and
  `internal/scratch/sessions_tree_families_test.go`'s fixture is
  `01a0f4f9baf57f13a707d05a68ea12e4`.

Any of those reaching `taskSessionDir` would have been refused, and the callers
that reach it with an unknown id are the **failure-reporting faces**
(`NotifyTaskFailed`, the worker's log/attribution path). A gate that turns a
visible failure into a silent one is a worse defect than the one it fixes
(Rule 15), so the rule refuses only its own near-miss. The narrowed gate was
re-verified: the nine tests pass (`go test -run 'TestSalvage|TestFinishCleanup|
TestServerRecycleDirty|TestClientDelivery' ./internal/tools/` → ok) and the
mis-spelled id is still refused on the real surface.

## Verification

- Red first: both new tests fail on the unmodified tree (`01-red-agent.txt`,
  `02-red-tools.txt`), each showing the directory being minted.
- Green after: `03-green-agent.txt`, `04-green-tools.txt`.
- Real surface (Rule 31): `07-rules31-before-after.txt` — the **old** binary
  (master `3dcdc852b`) mints `sessions/01a12dc6f44d…/`; the **fixed** binary
  refuses twice (raw log + session log) with the near-miss and leaves the
  sessions root empty.
- `go build ./...` OK; branch-scoped suite `05-suite.log`.

## Residue disposition (operator data — nothing deleted here)

The 11 containers are **a caller's evidence trees, not gopi's**, so they are
**kept**, and they are now *inert*: the gate refuses the spelling that made them,
so no new one can appear. Their contents are named by the census; a caller that
still wants that evidence moves it under the row's **canonical** directory
(`~/.gopi/sessions/<canonical-id>/`), which is where every reader looks
(`gopi task show` → session dir, `rundir.DirFor`). Deleting them is an operator
decision, not this change's: `rm -rf` on a path a human named is exactly the
class of action that needs the human (Rule 41's reporter is not the remover).

## Not done (honest)

- The **branch minting** side is untouched: `taskGroundIDStem` still strips
  dashes for the branch and ground directory, which is intentional (a ref cannot
  carry the row's grouping) and is what makes the two spellings easy to confuse.
  A future change that gives the ground its own derivation would remove the
  confusion at the source rather than at the seam; that is a larger change with
  its own blast radius (every existing ground and branch name).
- `internal/rundir.Dir` / `DirFor` (the READ side of the same mapping) were not
  gated: they are readers, they mint nothing, and their `""` behaviour is
  documented. The writer was the defect.
- **Two read/attribution seams still join without a shape check, and they are now
  filed as their own row** (Rule 41: the finder reports, does not fix):
  `01a12f4b-34bb-7c28-bd9a-61d569db30f7` —
  `internal/signalrec/signalrec.go`'s `sessionDest()` step 2
  (`filepath.Join(root, ledgerID())`) and
  `internal/tools/tasks.go`'s `taskFindSessionJSONLByTask` (TrimSpace +
  `filepath.Join`). Neither MINTS a directory (measured: the first requires
  `isSessionDir(dir)`, the second `os.ReadDir` and returns nil when absent), so
  they are not part of this defect; but where a residue container already exists
  they would read and attribute into it (stated as INFERENCE in that row's
  payload — no live reproduction was constructed).
