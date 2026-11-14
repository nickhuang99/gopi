<!-- doc-class: normative -->
# Branch-creation forms: which spellings are REFUSED, which are DESTRUCTIVE

**Class:** normative — the standing claim is "a shipped instruction never spells
a destructive branch creation, and the check that holds that claim is
`TestBranchForceGateRedProof`". See `devdoc/DOC-INDEX.tsv`.

## The defect

GOPI.md's branch-creation step — the instruction every agent runs, at three
sites — went straight from "pick a name" to "create it". `feat/<name>` is derived
from a task summary or a word the agent picks, and this repo has measured name
collisions (2026-09-21: four sibling branches at the same commit, names differing
only by a timestamp suffix). So a reuse is a matter of when.

What git does on reuse is the whole subject of this doc, and it was MEASURED
(transcript below) rather than read — the two classes are not "convenient vs
strict", they differ in whether anything is refused at all:

| spelling | on an existing branch name | exit |
|---|---|---|
| `git worktree add <path> -b <name>` | `fatal: A branch named 'feat/demo' already exists.` | 128 |
| `git checkout -b <name>` / `git switch -c <name>` | same refusal | 128 |
| `git checkout -b <name> -f` (`-F`, `--force`) | same refusal — the force short-circuits the DIRTY CHECK, not the ref check | 128 |
| **`git checkout -B <name>`** | **`Switched to and reset branch 'feat/demo'`** — the ref is RESET to HEAD | 0 |
| **`git switch -C <name>`** | **same reset** (the capital letter is the whole difference) | 0 |
| **`git worktree add <path> -B <name>`** | **same reset**, whenever the branch is not checked out anywhere | 0 |
| **`git branch -f <name> <base>`** (`--force`) | **same reset**, refs-only — no checkout needed | 0 |

The reset is silent and reported as success. The commits the ref held become
unreachable except through the reflog (`branch: Reset to HEAD`, the lost tip one
entry below) — measured victim `b428a68` → branch tip `c120a09` with
`merge-base --is-ancestor` NO. That ref is what the ledger, the merger and the
worktree sweep all treat as that task's work; pointing it somewhere else is the
destruction, and it happens in one line typed from habit.

**Refused = git is the guard.** `-b` and `switch -c` need no check of ours; git
never overwrites an existing ref with them. This is exactly why the safe forms
are the prescription. `git worktree add -B` behaves like `-B` when the branch is
idle and is refused when the branch is checked out somewhere (git refuses the
second checkout) — so its danger is not theoretical, it is conditional on state
the writer cannot see.

**Out of scope, deliberately** (a gate that fires on prose gets switched off):
`git branch -D`/`-d` is a loud, named, deliberate deletion that PROJECT.md Rule 9
prescribes for archiving a loser; `git worktree remove --force` removes a
checkout and resets no ref.

## What changed

### 1. GOPI.md — the precheck, at all three sites

`grep -n 'worktree add' GOPI.md` (the proof that every site was found):

```
208:git worktree add ~/workspace/gopi-<task> -b feat/<name>
830:(`git worktree add /home/user/workspace/gopi-<task> -b feat/<name>`) and implement
1147:git worktree add /home/user/workspace/gopi-<mine> -b feat/<mine>   # YOUR OWN worktree only
```

All three now carry the precheck — **FOUR** site lines, because master's
`feat/commit-path-ancestry-check` merge (which landed between this branch's
creation and its delivery-point sync) added "The Commit Path — 7 Steps In Order",
whose step 1 is a fourth place the `worktree add … -b` command is prescribed.
`grep -n 'worktree add' GOPI.md` (the proof that every site was found):

```
208:git worktree add ~/workspace/gopi-<task> -b feat/<name>
311:1. **Create the worktree + branch from master** — `git worktree add … -b feat/<name>`
854:(`git worktree add /home/user/workspace/gopi-<task> -b feat/<name>`) and implement
1171:git worktree add /home/user/workspace/gopi-<mine> -b feat/<mine>   # YOUR OWN worktree only
```

All of them now carry the precheck (`grep -n 'rev-parse --verify --quiet
refs/heads/' GOPI.md` → 205, 852, 1170, plus step 1's inline pointer):

- **Rule 6a (site 1, line 205)** — the full step. `git rev-parse --verify
  --quiet refs/heads/feat/<name>`; exit 1 = free → create; **exit 0 = TAKEN →
  pick another name and re-run step 1**. Says plainly that reuse is a claim on
  another task's work, that the force spellings do not refuse but RESET (naming
  them, with the reflog consequence), and that the decision to reuse is not the
  agent's to take silently. Cross-references
  `briefings/brief-worktree-path-must-be-fresh.md` for the PATH half and states
  it is not fixed here.
- **Rule 23 (site 2, line 828)** — the "open a FRESH branch" step now names the
  precheck first and points at Rule 6a for the full step, so a reader of Rule 23
  cannot follow the creation without meeting the check.
- **Rule 29 (site 3, line 1170)** — the read-only-inspection code block, where
  the worktree line is a command a reader copies, carries the precheck line
  directly above `worktree add`.
- **"The Commit Path — 7 Steps In Order", step 1 (line 311)** — a site that
  arrived with master's `feat/commit-path-ancestry-check` merge AFTER this branch
  was cut. Found by re-running `grep -n 'worktree add' GOPI.md` after the
  delivery-point sync (which is exactly why the step is LAST, Rule 30a): step 1
  now also names the precheck and Rule 6a. A branch-scoped change to a file that
  another branch is also editing is precisely the case where the grep proof has
  to be re-taken against the merged file, not the file you started from.

Why the precheck is `rev-parse --verify --quiet refs/heads/<branch>` and not
`git branch --list`: the full-ref spelling is the established form in this repo
(`task_merge_backlog_terminal_guard.go`'s `mergeBacklogRefResolvable`,
`repo_sweep_ref_guard.go`), it cannot be confused by a same-named TAG, and the
exit status is the verdict (0 = taken, 1 = free) with **no output on the free
path** — which is what makes it composable in a one-line step.

Because the rule file must NAME the banned flags to ban them, the gate's
predicate requires a **verb** beside the flag: a bare `-B` in a table row is
vocabulary, `git checkout -B x` is an instruction. That single rule is what lets
GOPI.md state the ban without becoming its own first violation (RED 4 vs GREEN 4
below).

### 2. `internal/branchform` — the reusable predicate (Rule 14)

A zero-dependency leaf package (stdlib only) that owns the whole question "is
this command line a DESTRUCTIVE branch creation?". One exported entry point,
`branchform.Destructive(line) []Form`, plus the vocabulary as exported constants
(`VerbCheckout`, `FlagCreateForce`, …) so no caller re-spells `-B`.

Design decisions, each measured:

- **Rule 14 justified by measurement.** No production Go string literal carries a
  verb+force-flag pair (verified across `internal/` + `cmd/`), so the package's
  only consumers today are the gate and the shipped instruction. It lands as a
  leaf package because that is what the SECOND consumer needs: a briefing that
  teaches the precheck must be able to say which spellings it is warning about,
  and a per-site helper (a second, drifting copy of the vocabulary) is Rule 13's
  defect. `internal/smokegate` + `internal/toolnames` are the precedents.
- **Segments, not lines.** A line is split on `;`, `&&`, `||`, `|`, backtick and
  newline before the verb is located, so a force flag in a LATER command never
  implicates an earlier one (`git worktree add … -b feat/x && git checkout -f y`
  is GREEN). Without this rule the gate would fire on ordinary command chains.
- **A verb is required** — see above; this is also what keeps `grep -B2`,
  `git worktree remove --force`, and a prose mention of `-B` invisible.
- **`git -C <dir> worktree add -B …` is caught**: the flags belong to the LAST
  verb token in the segment, so the `-C` of a path-carrying invocation does not
  masquerade as a verb.

`internal/branchform/branchform_test.go` carries one row per shape (12
destructive, 14 safe) — so a vocabulary that grows a shape without a case is
visible as an unproven row rather than as a silently greener gate.

### 3. The gate — EXTENDED the existing family, not a second one

**Checked first**: this repo's existing gates that could already cover the
surface were surveyed before writing anything. The result, and the reason each
was not the answer:

| candidate | verdict |
|---|---|
| the Rules 16–21 family in `internal/standards` (one shared `gate` type: `find`/`baseline`/`escape`/`scanTextRoot`/`scanRoots`, driven by `gates()` in `gate_test.go`, with `TestStandardGates` + `TestStandardGateDataReport` + the per-gate red proof) | **EXTENDED** — the new gate is one more member of `gates()`, so it inherits the baseline/stale-entry accounting, the inline escape, the data report and the red-proof convention for free |
| `internal/standards/conflict_marker_gate.go` (text topology over the whole tree, fence-aware) | already covers a DIFFERENT surface (a conflict marker in any text file); not extended — its fence semantics would have to fight the rule file's need to name the flags |
| `internal/standards/smoke_path_gate_test.go` (Go string literals under `internal/tools` + `cmd/agent`, plus one shipped markdown artifact) | already covers a different instruction (the whole-tree suite). Its **carrier technique was reused**, its subject was not — it reads LITERALS only, and the reason is the same here |
| `internal/tools/bash_path_policy_test.go` (allows `git worktree add -B …` as an in-root target — it is about WHERE, not WHICH FORM) | **not the gate**: it was READ as the search's "is it already covered?" evidence and it is orthogonal by construction (its fixture uses `-B` as an arbitrary legal command). Changing it would conflate a path rule with a form rule |
| `internal/tools/task_deliverable_worktree.go` (`taskBranchCreateFlagForce = "-B"` — a DETECTOR for "did this round create a branch?") | **not the gate**: it RECOGNISES the form as an event for the ledger; it never forbids it. Its constants are *bare flag tokens in a comment-documented const block*, no verb — so they are not forms and the new gate does not report them |
| `internal/tools/task_deliverable_branch.go:263` (`fields[j] == "-b" \|\| fields[j] == "-B"` — the worktree-add branch-name reader) | same family as above: a reader, not a guard |

So: **one new gate on the existing family, no second gate written**, and the
pre-existing helpers were reused rather than re-implemented
(`goFilesUnder`/`parseRepoFile`/`parsed`/`hit`/`repoRoot`/`linesAbove`/`markerOn`).

`internal/standards/branch_force_gate.go` — `branchForceGate()`:

- **Carriers**: production Go **string literals** under `prodRoots`
  (`internal`, `cmd`) — a literal RENDERS into a briefing, a comment documents
  the code and is legitimately allowed to name the forms — plus **GOPI.md**, read
  line by line (a markdown file has neither literals nor comments, so every line
  is instruction).
- **Escape**: `branch-force-ok: <reason>`, for a site whose SUBJECT is the form
  (Rule 25: mention is not prescription). It suppresses the failure but the site
  stays visible in `TestStandardGateDataReport`.
- **Baseline: EMPTY by construction.** Measured: no production literal carries a
  verb+force-flag pair, and GOPI.md (after this change) carries none — it names
  the flags, never the `verb + flag` shape. A baseline entry here would be a
  permanent exemption for an instruction that destroys a ref.
- **Non-vacuity, both halves**: a Go-file floor (`branchForceMinGoFiles`, 300 —
  measured production file count is well above it) AND a byte floor on GOPI.md
  (`branchForceMinInstructionBytes`) whose read is a HARD failure, plus a
  content assertion (`rev-parse --verify --quiet refs/heads/`) proving the gate
  read the real instruction rather than a same-named copy.

**Shapes covered, each with its own red proof** in
`internal/standards/branch_force_gate_test.go` (the deliverable's "red-before-green
test": the fixtures PLANT the form and assert the report):

| # | shape | carrier | asserts |
|---|---|---|---|
| RED 1 | `git worktree add … -B <name>` | Go literal | line 6, names `-B` |
| RED 2 | `git checkout -B <name>` | Go literal | names `-B` |
| RED 3 | `git switch -C <name>` | Go literal | names `-C` |
| RED 4 | `git checkout -b <name> -f` (force-into-place) | Go literal | names `-f` |
| RED 5 | `git branch -f <name> <base>` | Go literal | names `-f` |
| RED 6 | `git worktree add … -B <name>` | **markdown code span** | line 4, names `-B` |
| RED 7 | `` `git checkout -B <name>` `` inside a rule-table ROW | markdown | names `-B` |
| RED 8 | the flag in a LATER `&&` segment | Go literal | names `-B` |
| GREEN 1 | `-b`, `checkout -b`, `switch -c` | Go literal | silent |
| GREEN 2 | `git branch -D`, `git worktree remove --force` | Go literal | silent |
| GREEN 3 | `git checkout -f -- .`, `git rev-parse …`, a later force flag | Go literal | silent |
| GREEN 4 | a rule-table row naming `` `-B`, `-C`, `--force` `` with no verb | markdown | silent |
| GREEN 5 | the escape contract: the pair is still REPORTED by the predicate, silenced only by the escape through `classify` | markdown | visible + silenced |

Plus `TestBranchForceGateScansTheRealTree`: reads the real tree and asserts it is
GREEN, that the Go floor was met, and that the file it read carries the precheck.

## Rule 31 — the surface, RUN (real git, not `-h`)

Scratch repo in `/home/user/workspace/_tmp` (device `2052`, `/` is `2051` — the
Rule 22 check runs first in the script). Script:
`/home/user/workspace/_tmp/rule31-branch-precheck.sh`.

```
=========== STEP 1 — the precheck on a FREE name ===========
$ git rev-parse --verify --quiet refs/heads/feat/demo
  exit=1                                  ← free, no output

=========== STEP 2 — create it with the prescribed -b form ===========
$ git worktree add -q .../wt-demo -b feat/demo
  exit=0                                  ← branch feat/demo, at da6d325

=========== STEP 3 — the other task commits work on that branch ===========
victim tip = b428a683f398df78ce610f9ea053bb97619225e5
master tip = c120a09141ad7f3384a6a2d931c34379427927cf

=========== STEP 4 — the precheck on a TAKEN name (Rule 6a's verdict) ===========
$ git rev-parse --verify --quiet refs/heads/feat/demo
b428a683f398df78ce610f9ea053bb97619225e5
  exit=0                                  ← TAKEN → pick another name

=========== STEP 5 — the refused forms are loud (good) ===========
$ git checkout -b feat/demo
fatal: A branch named 'feat/demo' already exists.
  exit=128
$ git -C .../wt-demo checkout -b feat/demo
fatal: A branch named 'feat/demo' already exists.
  exit=128

=========== STEP 6 — the FORCE forms do not refuse: they RESET the ref ===========
$ git checkout -B feat/demo
Switched to and reset branch 'feat/demo'
  exit=0
  feat/demo now = c120a09...   (was victim=b428a68...)
  the other task's commit still in the ref? NO
  reflog of feat/demo:
    c120a09 feat/demo@{0}: branch: Reset to HEAD
    b428a68 feat/demo@{1}: commit: the other task's work
    da6d325 feat/demo@{2}: branch: Created from HEAD
```

`worktree add -B` on the same branch while it is checked out is refused
(`fatal: 'feat/demo' is already checked out at …`, exit 128) — that refusal is
about the checkout, not the ref, so the shape was re-measured on an IDLE branch,
which is the realistic case (the victim's own worktree is long gone):

```
$ git worktree add -q .../wtX -B feat/idle      # feat/idle is checked out NOWHERE
  exit=0
  feat/idle now = b5932eb...   (was victim=d1ac6aa...)
  the other task's commit still an ancestor? NO
  reflog: b5932eb feat/idle@{0}: branch: Reset to HEAD
          d1ac6aa feat/idle@{1}: commit: victim work
```

And the instruction itself, read back from the tree the gate reads:

```
$ grep -n 'worktree add' GOPI.md
208:git worktree add ~/workspace/gopi-<task> -b feat/<name>
830:(`git worktree add /home/user/workspace/gopi-<task> -b feat/<name>`) and implement
1147:git worktree add /home/user/workspace/gopi-<mine> -b feat/<mine>   # YOUR OWN worktree only
$ grep -n 'rev-parse --verify --quiet refs/heads/' GOPI.md
205:git rev-parse --verify --quiet refs/heads/feat/<name>
828:(`git rev-parse --verify --quiet refs/heads/feat/<name>`; exit 0 = TAKEN → pick
1146:git rev-parse --verify --quiet refs/heads/feat/<mine>   # exit 0 = TAKEN → pick another name
```

## Red-before-green on the REAL tree (the gate is not vacuous)

The fixture proofs above plant each shape in a synthetic source. This is the
stronger half: the form planted in the ACTUAL shipped instruction, the gate
observed failing, then the plant reverted and the gate observed passing.

```
########## PLANT — `-b` → `-B` on GOPI.md:208 (the shipped instruction) ##########
$ sed -i '208s/ -b feat/ -B feat/' GOPI.md && sed -n '208p' GOPI.md
git worktree add ~/workspace/gopi-<task> -B feat/<name>
$ go test ./internal/standards/ -run TestBranchForceGateScansTheRealTree
--- FAIL: TestBranchForceGateScansTheRealTree (0.43s)
    branch_force_gate_test.go:182: GOPI.md:208 carries a destructive branch-creation spelling
          destructive branch creation: `git worktree -B` resets an existing branch (reflog-only recovery)
          use the create forms (branchform.FlagCreate), which git refuses loudly — or mark a site
          whose SUBJECT is the form with "branch-force-ok:" and a reason (devdoc/branch-creation-forms.md)
FAIL

########## REVERT ##########
$ git checkout -- GOPI.md && go test ./internal/standards/ -run TestBranchForceGateScansTheRealTree
ok      github.com/nick/gopi/internal/standards   0.871s
$ git status --porcelain
                                   ← empty (Rule 30)
```

## Not done / honest limits

- **The `-B` inside `internal/tools/bash_path_policy_test.go` is untouched.** It
  is a test fixture whose subject is a path, and the gate deliberately does not
  scan `_test.go` files (a fixture must be able to construct the shape). Flagged
  here rather than changed: conflating a path rule with a form rule is the kind
  of widening Rule 31 ¶2 bans.
- **`scripts/nonff-able-cohort-after.sh:129` carries `git -C "$L/repo" branch -f
  "$BR" "origin/$BR"`** — a real, live force form in a shipped script. It is
  measured evidence that the shape is used, and it is deliberately OUT of the
  gated carriers (the mandate names GOPI.md + shipped Go literals). It resets a
  branch to its own remote tip in a scratch cohort repo, so it is not the defect
  this task fixes — but a future task that widens the carriers to `scripts/`
  should classify it first.
- **The PATH half is not fixed** (a pre-existing EMPTY worktree directory is
  silently reused by `git worktree add`). Cross-referenced from PROJECT.md Rule 6a
  and `briefings/brief-worktree-path-must-be-fresh.md` owns it.
- **A rule file can still STATE the banned spelling in prose** without any gate
  objection as long as no verb sits beside the flag — that is the intended
  precision, but it means the gate is not a proof that a human read the sentence.
  The escape exists for the verb+flag case.

## Gates found — classification

| gate | classification |
|---|---|
| Rules 16–21 family (`internal/standards`, `gates()` + `TestStandardGates`) | **EXTENDED** — `branchForceGate()` added to `gates()`; each new shape got its own red proof (the brief's "extend to the SHAPES") |
| `smoke_path_gate_test.go` | **already covers** a different instruction (whole-tree suite); its literal-only carrier rule was REUSED as the technique |
| `conflict_marker_gate.go` | **already covers** a different surface (conflict markers, whole text tree) |
| `internal/tools/bash_path_policy_test.go` | orthogonal (WHERE, not WHICH FORM); fixture left alone |
| `internal/standards/config_doc_gate_test.go` | unrelated (CONFIG.md knobs) |
| new | `branchForceGate()` + `internal/branchform` — the ONLY new check in this change |
