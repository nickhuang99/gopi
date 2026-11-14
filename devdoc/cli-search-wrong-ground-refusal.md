<!-- doc-class: normative -->
# The wrong-ground refusal: `code-search` / `keyword_search` stop answering for trees they never read

**Task**: `01a0ed98-7172-7dde-95fa-004cb66a9245` · **Base**: master `1a36d70a`

## The defect, measured

Both search verbs answered a search run in the **wrong place** exactly as they
answered a search that ran and found nothing:

```
$ cd $(mktemp -d) && gopi code-search taskverbs.DispatchVerbs   # the symbol EXISTS in the repo
no symbol found: taskverbs.DispatchVerbs          # exit 1  — a claim about a codebase never read
$ cd <empty dir>   && gopi keyword_search handover-budget
No matches.                                       # exit 1  — same code, same shape
$ cd <repo>        && gopi code-search zzzAbsent  # the genuine miss
no symbol found: zzzAbsent                        # exit 1
```

The exit code, the stream and the wording agreed, so an LLM (or a script) could
not tell "I searched and it is not there" from "I never searched your tree".
The second is an **answer about a tree that was never read**; the first is a
**refusal to search**. They are now different outcomes:

| invocation | before | after |
|---|---|---|
| `code-search <real symbol>` in repo (control) | 0 | 0 |
| `code-search <absent symbol>` in repo (control) | 1 | 1 |
| `keyword_search <real token>` in repo (control) | 0 | 0 |
| `keyword_search <absent token>` in repo (control) | 1 | 1 |
| `code-search <real symbol>` in EMPTY dir | 1 | **4** |
| `code-search <absent symbol>` in EMPTY dir | 1 | **4** |
| `keyword_search <absent token>` in EMPTY dir | 1 | **4** |
| `keyword_search <real token>` in EMPTY dir | 1 | **4** |
| `code-search <real symbol>` in text-only dir | 1 | **4** |
| `keyword_search <real token>` in text-only dir | 0 | 0 |
| `code-search --root <missing path>` | 1 | **4** |

(`before` = master tip built before the change, `after` = the worktree; raw table
in the task session dir under `wrong-ground/before-after.txt`.)

## The three-way contract

- **0** — the search ran and found something.
- **1** — the search **ran**, on a real tree, and found nothing. Unchanged from
  before, deliberately: the epic's acceptance is that the genuine miss keeps its
  code. (Usage errors and backend failures stay here too — this change did not
  narrow them.)
- **4** — the search **could not run**. A named refusal on stderr, exit 4.

4 is not a new invention: it is the family's existing "the universe is empty and
that is an ANSWER" code, `tools.TaskForBranchExitNoOwner` (`gopi task for-branch`
for the "no owner" lookup). A test asserts the equality, so the family cannot
grow a second synonym for the same reading.

## What makes a root "searchable"

Not "is it a git repository" — **does a recursive walk find a file this scope
covers**. The extensions come from the SAME include globs the caller hands the
backend (`tools.CodeSearchGlobs`), and the two directories both verbs exclude
(`.git`, `vendor`) are skipped. So:

- a directory full of `.txt`/`.pdf` is a real directory that `code-search` cannot
  read — and the refusal says exactly that, printing the scope's extension list;
- `keyword_search` has no include glob (ripgrep reads every file), so it refuses
  when the walk finds no file at all **that is not gopi's own state** (see the
  2026-10-01 section below: the profile directory and a sandbox's ownership
  marker are entries without being material).

The refusal prints four things: the **absolute root**, the **channel** it came
from (`--root` vs the default cwd), the **git work tree** it sits in and this
root's slot in it (or "not inside any git work tree"), and the **observation**
(entries walked, files matching the scope, the scope's extensions, or the names
of git work trees found beneath it) — plus the remedy.

## Three things it deliberately does NOT do

1. **It does not refuse a miss.** A root holding scannable sources is searched;
   its result is an answer whatever that answer is.
2. **It does not guess when it cannot finish.** A walk that hits its entry cap
   without finding a source **proceeds** as before — "I did not finish looking"
   is not "there is nothing" (Rule 15). Same for an unreadable root: the refusal
   says UNKNOWN rather than claiming emptiness.
3. **It never touches the tool layer.** `tools.CodeSearch` /
   `tools.KeywordSearch` and every `SearchScope` field are untouched — the agent
   loop's contract is not what was ambiguous, the SHELL contract was.

## Two measured traps (both found by the table above, not by reading code)

- **The extension derivation.** `strings.TrimPrefix("*")` on `**/*.go` yields
  `/*.go`, a string matching no filename: the walk found 0 sources in gopi's own
  checkout (5910 entries walked) and the refusal fired on a HEALTHY repository.
  The derivation goes through `path.Ext` now, and a test pins `.go`/`.tsx` and
  rejects any extension carrying glob syntax.
- **The nested-work-tree probe.** From inside an existing work tree, `git
  rev-parse --show-toplevel` answers with the ENCLOSING work tree for every
  subdirectory — so a first cut printed `cmd/`, `internal/` and `devdoc/` as
  "git work tree(s)". A subdirectory now counts only when the answer equals the
  directory that produced it.
- **A fixture that changed its own input** (why the table's EMPTY rows use a
  fresh directory per binary): the PRE-fix `code-search` writes a 588-byte empty
  `.gopi-tags` into the cwd even when it finds nothing, so sharing one fixture
  between the two binaries let the first one alter the second's input. The first
  attempt at this table measured the `keyword_search` rows as `1` because of it.

## Files

- `cmd/agent/cli_search_root.go` (new) — the predicate, the constants, the two
  renderers, and the single `searchGuardOrExit` entry point.
- `cmd/agent/cli_code_search.go`, `cmd/agent/cli_keyword_search.go` — the guard
  call, the named exit codes, `rootGiven`, and the exit-code tables in `-h`
  (rendered from the constants via `codeSearchUsageText()` /
  `keywordSearchUsageText()`, so the documented code and `os.Exit` are one value).
- `cmd/agent/cli_search_root_test.go` (new) — unit + contract + live-binary rows.
- `cmd/agent/cli_code_search_test.go`, `cmd/agent/cli_keyword_search_test.go`,
  `cmd/agent/cli_tools_maintenance_help_surface_*_test.go` — assertions that now
  read the rendered page, and the three g6 code-search cases whose `wantExit`
  became the refusal code (their sandbox home is not a repository — precisely the
  class this change distinguishes).

## Check

```
go test ./cmd/agent/ -run 'CodeSearch|KeywordSearch|SearchRoot|SearchExit|Searchable|SearchGround|SearchUsage|SearchWrongGround' -count=1
go test ./cmd/agent/ -count=1        # the g6 help-surface tables live here
```

## The two faces disagreed about one tree: gopi's own state is not material (2026-10-01, task `01a0edfa`)

The refusal above fired for `code-search` and NOT for `keyword_search` on the same
directory, because the predicate is driven by the caller's include globs and the
full-text verb passes **none**: `searchableExtensionsIn(nil)` means "every file is
scannable", so the criterion degraded to *"is there ANY file?"* — and a gopi
sandbox is a directory with files in it. Measured on the real binaries:

```
$ cd <sandbox-build-…>            # entries: .gopi/  .sandbox-owner.json  (nothing else)
$ gopi code-search taskverbs.DispatchVerbs   → exit 4
    refusing: no code repository at the search root
$ gopi keyword_search handover-budget        → exit 1   "No matches."   ← the defect
```

One tree, two search faces, two readings — and the exit-1 reading is the expensive
one: it is the code that means *"I searched a real tree and it is not there"*. A
sandbox profile is the shape a gopi process is most likely to stand in by accident
(`GOPI_HOME` exported, the sandbox as the shell's cwd), so this was the shape most
likely to produce a confident wrong answer.

**The fix is one class of entry in the shared walk.** `searchGroundWalkRoot` now
recognises gopi's OWN state and neither counts it as material nor descends into it:

- the profile directory — `config.ProfileDirName` (`.gopi`), whose whole subtree is
  state (sessions, raw logs, the ledger, a settings symlink);
- the sandbox ownership marker — `config.SandboxOwnerFileName`
  (`.sandbox-owner.json`).

Both names are taken **from their own definitions** (Rule 14 — never mirrored), and
the match is by **exact basename**, deliberately: a suffix rule on `.json` would call
a colleague's `package.json` gopi state and refuse a healthy tree, which is the false
refusal this family must not ship. The refusal says what it found instead of claiming
the root is empty:

```
  scan: 1 entries under the root, 0 files matching the search scope (this scope reads every file)
  what IS here is gopi's own state, not searchable material: .gopi, .sandbox-owner.json
```

The state line is rendered from ONE constant (`searchGroundStateLead`) shared by both
verbs, so the two faces cannot print different sentences for the same fact.

**What changed in the exit table**: exactly one row, the sandbox profile directory
under `keyword_search` (1 → 4). The empty-directory rows were already 4 for both
verbs; the healthy-repo controls (0 hit / 1 miss) are untouched — a genuine miss
still keeps code 1 and its wording, which is the contract this change had to leave
alone.

**Where the state line does NOT apply**: a root that holds gopi state *and* real
material is a real directory and is searched as before (pinned by a test), and an
empty directory keeps its own observation ("0 files at all") rather than borrowing
the state line.

**Tests** (`cmd/agent/cli_search_root_test.go`):

- `TestSearchScopeStateNamesComeFromTheirOwnDefinitions` — the two names come from
  `config`, and the negative controls (`package.json`, `settings.json`, `.git`,
  `vendor`) are NOT state;
- `TestSearchRefusesAProfileOnlyRootForBothScopes` — the criterion over BOTH scopes,
  plus the sandbox shape, plus the mixed fixture that must stay searchable;
- `TestKeywordSearchRefusalMatchesCodeSearchOnTheRealBinary` — the two-faces contract
  on ONE tree materialised by the real `gopi sandbox create`, **red on the pre-fix
  binary** (`exit = 1, want 4 … "No matches."`) and green on the worktree.

Raw before/after table: the task session dir `evidence/wrong-ground/before-after.txt`.

## Follow-up landing (2026-09-30, task `01a0edb1-b6f1-77c1-8390-447bcd3ab08f`)

The change above was written for task `01a0ed98`, whose row was **cancelled** —
its premise ("both shapes exit 0") was refuted by its own re-measurement (both
exit **1**; what the two shapes share is the sentence and the CODE, not a false
success). The implementation itself was kept and landed here on a fresh branch
off `master`, with the narrowed premise stated in the two material findings.

Four things this run added on top of the port:

1. **The usage pages state the cwd premise.** Both `--root` rows now say the
   default is *the current directory* — the command searches where you stand —
   that the root must hold at least one file the search can read, that a
   directory with nothing searchable is REFUSED, and the remedy (`--root DIR`, or
   run from inside the repository). That premise was silently assumed before;
   the epic's second half is exactly that omission.
2. **The width sweep is told the templates' arguments.**
   `helpTemplateArgs` (cmd/agent/help_width_test.go) is keyed by constant NAME and
   FAILS on a template with no entry — turning the usage pages into format
   strings made the sweep red until `codeSearchUsage` / `keywordSearchUsage` were
   registered. The arguments come from ONE helper per verb
   (`codeSearchUsageArgs()` / `keywordSearchUsageArgs()`), the same one the
   renderer uses, so the measured lines are the printed lines.
3. **The miss operands are unique per run.** The live-binary test's
   `missingSymbol` started as the literal `zzzNoSuchSymbolXYZ` — and the ripgrep
   fallback FOUND it: three hits in `scripts/materials/gen-topA.sh`, a file
   committed after the literal was chosen. Measured: the miss row exited 0. The
   operand is now `"zzzNoSuchSymbol" + uniqueSuffix(t)`, the same rule the
   sibling test in `cli_code_search_test.go` already used.
4. **The probe raises the child's tool timeout.** The rows assert a contract, not
   a latency: under a fully parallel suite run the search tool can exceed the
   default 30s and answer "timed out" instead of its contract (observed once on
   the keyword miss row, 30.04s). `searchProbe` now passes
   `GOPI_TOOL_TIMEOUT_SEC=180`, the ONE reader of that knob
   (`internal/tools/timeout.go`).

The two material findings (`docs/archive/demo/materials/108-code-search/05-finding.md`,
`109-keyword-search/05-finding.md`) carry a **status update** section naming the
two table rows in each that changed (the non-repository rows) and repeating the
8-row before/after table measured on the real binaries.

## Premise correction: the refusal itself was ALREADY on master (2026-09-30)

The epic that dispatched this run asserted "`933c78e2` (exit 4 + named refusal) has
not been merged into master yet". **Measured, that is false**:
`git merge-base --is-ancestor 933c78e2 master` → YES, because `master` = `1f87a99e`,
the merge of task `01a0ed9871727dde95fa004cb66a9245`'s branch — and that branch
delivered exactly `933c78e2`. Master already carried `cmd/agent/cli_search_root.go`,
`cli_search_root_test.go` and `searchExitWrongGround = 4`.

So the real remaining delta was **not** the refusal; it was the epic's second half
plus this run's four hardening items. Concretely, master's `--root` row read only
`search root (default: "."). Must exist and be a directory.` — it did not say the
root has to be *searchable*, nor that a non-repository root is REFUSED, which is
the very omission the epic exists to close. Master also passed the exit codes inline
to `fmt.Sprintf` (so the width sweep and the renderer were not provably reading one
list), wrote the miss operand whole (`"zzzNoSuch" + "SymbolXYZ"`, findable once the
evidence doc landed — the miss row then exited 0), and did not raise the probe's
tool timeout.

`git diff master..HEAD` after the delivery-point merge (`1776e93e`) is therefore the
authoritative list of what this branch delivers: the two `-h` premises, the two
`UsageArgs()` helpers, the test hardening, the devdoc section, and the four
material-edits — **not** the refusal, which was already on `master`. Recorded here
rather than absorbed silently: the previous task was cancelled for asserting a
premise it had not measured, and the shape of that error repeats easily.
