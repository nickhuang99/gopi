<!-- doc-class: normative -->
# The gopi profile root's direct children — declared, observed, and one narrow reclaim arm

Task: `01a0f500-05cb-75af-b9ed-d410ace9fd13` · branch `task/01a0f50005cb75afb9edd410ace9fd13`

## The defect this closes

Four directories sat directly under `~/.gopi` on this host — `evid-01a0e562`
(418M), `sigrec-red-1882254` (266M), `evid-01a0c630`, `evid-01a0cba8` — holding
per-run binaries, whole checkout copies and log dumps an LLM session had
hand-made while it worked. **684 MB, ten days, invisible.**

They were invisible because every collector this system owns is
namespace-limited **by construction**, and all four limits ran out before the
profile root:

| arm | scope | why it missed |
|---|---|---|
| `gc.go` home sweep | `HomeDir()` | collects suite-/legacy/queue names (`IsScratchEntry`) — `evid-*` is not one |
| `base_residue.go` | the memory base | `gopi-*` / `gocache-*` / `probe-*` / `suite-*-run-*` (`IsBaseResidueName`) |
| `declared_trees.go` | GOTMPDIR, `raw/`, `sessions/**/*.raw.log`, build cache | four **trees**, not the profile root |
| `sandbox_gc.go` | the configured sandbox root | a different root entirely |

The profile root was in **nobody's scope** — which means it had no vocabulary:
not "owned", not "foreign", simply unmodelled. Nothing on the machine could say
whether a directory appearing there was expected. The census below is what the
new arm answers, and it is the first time the question was askable at all.

## The ruling this encodes (D1)

**`<GOPI_HOME>/.gopi` is a STATE directory, not a scratch directory.** It holds
the live ledger, the settings, the session history, the plugin tree, the
component logs and the service locks — the state a gopi process *reads to be
itself*. It is not a place to put work. A run's own evidence belongs in its
session directory (`<profile>/sessions/<task-id>/`, the path the run's own report
path already names, Rule 37); a deliverable belongs on a branch.

The measured census (this host, 2026-10-01, 89 direct children) is therefore
**not a list of things to reap**. It is a statement of what belongs, and of how
much of what is there does not belong to anything the system knows:

```
$ gopi scratch-gc profile --dry-run
scratch-gc role=cli-profile home=/home/user/.gopi removed=0 stale=6h0m0s disabled=false \
  children=89 declared=25 undeclared=64 dry-run=true
```

25 declared (13 registry rows + the two residue families matching 4 children).
64 undeclared, every one of them named on stderr: session dumps and screenshots
(`*.png`, `*.log`), hand-made scratch (`tabs`, `tabs2`, `tmp`,
`night-watch-scratch`, `verify-reports`, `pricing-snapshots`, `dumps-1214`),
rotated settings copies a session kept (`.ds_flash_settings.json`,
`.glm_settings.json`, …) and stamps (`task-list-sweep.stamp`,
`crash-dump-sweep.marker`). None of them is wrong. All of them were
**unexplained**, and an unexplained directory in the operator's state directory
is exactly the thing nobody could previously see.

## What was built (D2–D4)

| part | file | what it does |
|---|---|---|
| D2 registry + observation + narrow reclaim | `internal/scratch/profile_root.go` (new) | `ProfileRootChildren()` — one row per child: `{name, kind, class, writer, evidence, bound}`; `ProfileResidueName()` — the residue rule as SHAPE; `ProfileRootReport()` — judges every direct child, deletes nothing; `ProfileRootRefusal()` — refuses a path that is not a profile root; `SweepProfileRootResidue()` — the explicit-only delete; `TallyProfileRoot()` — the one tally every face prints |
| D2 resolver seam | `internal/config/scratch_trees.go` (+3 lines) | `scratch.RegisterProfileRootResolver(GopiDir)` — the same leaf-package seam the two declared-tree resolvers already use, so `<gopi dir>` keeps ONE spelling |
| D3 gate | `internal/scratch/profile_root_test.go` | range-driven: the candidate child names are **derived from the writers' own source** (`filepath.Join(gd, "…")`) and every one must be declared; 13 anchors pin each constant-defined child to the file that still carries its evidence string; red-proof drives the same decider over a deleted row |
| D4 CLI | `cmd/agent/cli_scratch_gc_profile.go` (new) + `cli_scratch_gc.go` | `gopi scratch-gc profile [--dry-run] [--explain] [--yes]` |
| D4 tick | `cmd/agent/scratch_gc_tick.go` (+~30 lines) | the resident tick **reports** undeclared children and has **no way to reach the delete** |

## The residue arm is SHAPE, not a prefix — and it is deliberately stricter than the base sweep

`ProfileResidueName` accepts `<family> "-" <token> ["-" <token>]*` where the
family is `evid-` / `sigrec-` (the two names this host's sessions actually used)
and at least one token must look **generated** rather than chosen. The tail rule
in `profileResidueTailGenerated` takes four shapes: a lowercase-hex run of ≥8
(an `os.MkdirTemp` token — `01a0e562`), a decimal run of ≥5 (a pid —
`sigrec-red-1882254`), a canonical UUID, or a token carrying an underscore.

**Why it is narrower than `baseResidueSuffixGenerated`** (which accepts any token
carrying a digit): that fold is right for a shared world-writable directory whose
job is to recognise a family by its own naming scheme. This arm governs the
operator's **state** directory — the ledger lives one level up — so it takes the
narrow shape. The two measurable differences:

- `evidence` (the bare word, no dash) is refused by the family rule, not by luck;
- `evid-notes-2026` is refused because a 4-digit run is not a pid
  (`profileResiduePIDMinDigits = 5`), while a 5-digit run after a word **is**
  accepted. A token rule cannot tell a pid from any other 5-digit run, and that
  limit is written down in the table rather than hidden.

`evid-notes` — a person's directory — is refused by both halves.

## Hard rules the arm obeys unchanged

- **Age**: `ScratchEntryStale` with the same `stale` knob every other arm reads.
- **Liveness**: `/proc` cwd + exe + fd + maps (`LiveHolders`), never age alone;
  the shared predicate is `staleUnheldDirectory` — the profile arm does NOT get
  a weaker second rule.
- **Namespace**: the shape rule above, plus a LAST check before each delete
  (`removeCollectableOfProfileChild`) that re-decides both the family and the
  parent, so a path assembled elsewhere cannot reach a removal.
- **The root is never a candidate**, at any age; every declared child is off
  limits at any age; a symlink in the family is kept (`removeCollectable`
  refuses non-directories), which matters because the reclaim would otherwise
  follow a link out of the profile.

## Report-always, delete-only-when-asked (constraint ⑧)

`SweepProfileRootResidue` is called from exactly one place —
`cmd/agent/cli_scratch_gc_profile.go`, the operator's verb, and **only when the
operator spelled `--yes`**. The bare form and `--dry-run` both REPORT (the same
convention the worktree-stub arm already uses, and the first version of this arm
got it wrong — the `--explain` form deleted the fixture, which the G6 help-surface
case caught). The resident tick calls `ProfileRootReport` only; there is no flag,
no env var and no default that lets a tick delete here. The precedent is measured (memory #7096): a resident
tick deleted a SHARED family its own comment declared off-limits, on a machine
where nobody could watch it happen. The profile root holds the ledger, so the
automatic face observes and the human face acts.

## Verification

- `CGO_ENABLED=0 go build ./...` — OK.
- `go test ./internal/scratch/ -run 'ProfileRoot|ProfileResidue'` — PASS (7 tests).
- **Red-proof**: deleting the `sessions` row from `ProfileRootChildren()` makes
  `TestProfileRootChildrenCoverRepoWriters` FAIL naming the child
  (`child "sessions" is written by internal/config/gopihome.go but is NOT
  declared`); restoring the row turns it green. Both states observed in one run.
- `go test ./cmd/agent/ -run 'ScratchGCProfile|ScratchGCTickReportsUndeclared'` — 4 PASS.
- **Real machine**: `./bin/gopi scratch-gc profile --dry-run` → 89 children,
  25 declared, 64 undeclared, **removed=0**, exit 0; all four orphan directories
  reported `class=residue removable=true` and still present afterwards. The bare
  form reported identically (`removed=0`) — no delete without `--yes`.
- **Help-surface gate** (the group's own completeness gate, which the new verb
  tripped on the first run): 151 rows / 147 distinct / **116 typed** / 31 blocked,
  after regenerating the inventory with
  `scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi` +
  `scripts/g6-inventory-doc.py`. `go test ./cmd/agent/ -run
  'TestToolsMaintenanceHelpSurface'` PASS.

## Known limits (honest)

- The derivation reads only `filepath.Join(<root var>, "<literal>")`, so a child
  whose name is a **constant** in a leaf package is covered by the 13 anchors
  rather than by the scan. A new constant-defined child needs an anchor row —
  the gate will not invent it.
- The undeclared set is reported, never classified as litter. 64 of 89 children
  on this host are session leftovers; deciding which of them may be reaped is a
  separate task with its own evidence (the family rule would have to be derived
  from what sessions can safely lose).
- The tick's report goes to stdout/stderr of the worker/merger process; there is
  no aggregating surface yet (`gopi log` does not index it).

## Addendum — the row that crosses a package boundary

`sessions` is the largest state child of the profile root, and its NAME is
defined in `internal/config` (`SessionsDirName`), which imports
`internal/scratch`. Spelling it in the registry would have been a second
spelling of it — and the package's own guard
(`TestSessionsDirNameNoHardcodedLiterals`, red at
`internal/scratch/profile_root.go:216`) caught exactly that on the first run, so
the row is supplied the other way: `internal/config/scratch_trees.go` calls
`scratch.RegisterProfileRootChildren(...)` from its own `init`, passing the
constant. The registry is `staticProfileRootChildren()` +
`registeredProfileRootChildren`, so a package that owns a name declares it where
it is defined.

Two consequences, both pinned by tests:

- `cmd/agent` (which imports config) asserts the row is present —
  `TestProfileRootSessionsRowIsWiredFromConfig`;
- the leaf package's own test binary has no `internal/config` on its import path,
  so `registerSessionsRowForThisTest()` supplies the same row for the gate and
  says so in its comment rather than quietly skipping the child.

The red-proof is unchanged and still observed: deleting the `sessions` row (from
either side) makes the gate FAIL naming the child and the writer that creates it.
