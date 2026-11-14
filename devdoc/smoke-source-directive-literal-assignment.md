<!-- doc-class: historical -->
# The staged-script source-closure predicate, second pass: an edge whose path lives in the script's own variable

Task: `01a12d22-dac0-7bde-a9f8-95926cdc64ea` (rework of the deleted branch
`01a12d08655d7ce6a107bad36efe28de`, commit `97394c7ef`, on a fresh branch off master).

## The shape of the task

Two attempts had rewritten the same predicate `sourceDirectivesIn` in
`internal/smoke/e2e_staged_scripts_test.go` to stop reading path MENTIONS as
source EDGES:

| attempt | state | predicate |
|---|---|---|
| `97394c7ef` (branch `task/01a12d08…`, ref deleted) | not on master | command position + one level of the script's own literal assignments |
| `448d4521c` (sibling, landed) | on master | command position only, then a literal `<tree>/scripts/` substring inside the argument |

Master's landed version is a real narrowing (the three false-positive classes
the deleted branch had also identified are all read as values now, and the
guard and its red-proof are green). What it does **not** have is the deleted
branch's second half: it never resolves the argument through the script's own
literal assignments. So a directive whose path the script holds in a variable is
invisible to it.

## The measured class master is blind to

`scripts/test-budget.sh` — the fixture's own entry point — spells its shared
library exactly that way:

```sh
stream_scope_lib="scripts/lib/stream-scope.sh"   # line 154
...
. "$root/$stream_scope_lib"                      # line 212
```

At that directive's argument there is no literal `scripts/` substring, so
master's `strings.Index(arg, e2eEntryPointDir+"/")` finds nothing and the edge
is dropped. Measured, same commit, same tests:

> **Naming, pinned once for this doc.** "master" below means the INTEGRATION
> branch (`refs/remotes/peerbox/master`), whose predicate at measurement time was
> `448d4521c`'s — sha256 `8f8cc667…`. It never means this checkout's local
> `master` ref, which was **110 commits behind** and carried **no narrowing at
> all** (a 26-line predicate with zero `sourceArgument` calls, sha256
> `60cfec8e…`). The two are different code; only the integration branch's is the
> pre-fix baseline the numbers below were measured against.

```
master:      TestEntryPointSourceDirectivesAreSeen → "carries 2 source directive(s):
             [scripts/lib/tmp-ban.sh scripts/lib/heavy-threshold.sh]"
this branch: TestEntryPointSourceDirectivesAreSeen → "carries 3 source directive(s):
             [scripts/lib/tmp-ban.sh scripts/lib/heavy-threshold.sh scripts/lib/stream-scope.sh]"
```

and the red-proof moves with it: removing `scripts/lib` reports **25** directives
on master and **26** on this branch (`scripts/test-budget.sh sources
scripts/lib/stream-scope.sh` is the addition).

**This is a *latent* blindness, and that distinction is the honest one.** The
staged tree is a full mirror of `scripts/`, so the file is present either way and
the fixture guard reads green under both predicates — nothing in this repo is
currently red because of it. It is a hole in the PREDICATE: the guard would fail
to demand a file that a future stage-narrowing dropped, in exactly the way the
2026-09-18 red run happened (a source line the fixture did not carry). Pinning it
belongs at the predicate level, with a sample that does not depend on the staged
tree, which is what the new test does.

## What was delivered

One predicate, on top of master's (Rule 23: fresh branch, master's predicate
kept as the base — the deleted branch was not applied):

- `sourceDirectivesIn` = command-position recognition → expand the argument
  through the file's own literal assignments → keep the path only if it is
  literal. The three read-only helpers (`sourceArgument` / `directiveArgument` /
  `scriptCode`) are master's, untouched.
- `literalAssignmentsIn` records `NAME=literal` lines (first assignment of a name
  wins, so the result is order-independent; a value holding `$`, a command
  substitution or a multi-word value is skipped — the predicate **substitutes
  literals only, never guesses**).
- `expandLiteralVars` substitutes longest-name-first (so `${stream_scope_lib}`
  unfolds while `${stream}` cannot eat a prefix).
- `literalSourcePath` is the single loud/conservative exit: match the
  `<tree>/scripts/` prefix anywhere in the token, then **drop the token if a `$`
  still stands in the relative part**. That one rule is what keeps the narrowing
  non-blind rather than over-broad — `${GOPI_MATERIALS_ENTRIES:-$ROOT/scripts/
  materials/entries-topB.sh}` resolves `$ROOT` (a variable this script does not
  assign) to nothing, the brace-form run of the substitution leaves `${…}` intact,
  a `$` survives, and the token is dropped. It is never demanded of the fixture.

Everything is a named constant (Rule 1/13): `sourceVarSigil`,
`sourceExpansionOpen/Close`, `sourceQuoteChars`, `sourceShellBlanks`,
`sourceAssignOp`, `sourceStreamScopeLib{Name,Rel,Assign,Use}`.

## Read-the-value, not the token: why the default expansion is dropped

`${VAR:-$LITERAL/PATH}` is the awkward one, and the difference between the
landed sibling and the deleted branch is worth recording. The deleted branch
resolved the literal half **in place** — which makes the token read
`…:-$ROOT/scripts/materials/entries-topB.sh}` and `strings.Index` picks up
`scripts/materials/entries-topB.sh}` **with the closing brace attached**, a
spelling that resolves to nothing even though the file exists. (So did master's
pre-narrowing predicate, which is the bug the sibling's commit message quotes.)
The landed sibling has no default-expansion handling at all and drops the token
one step earlier; this branch reaches the same verdict by the same conservative
rule as above — the run that expands `$ROOT` leaves its `${…}` unresolved, so a
`$` survives and the token is dropped. To make it a walkable path instead, the
inner expansion would have to be *re-defined against the outer variable's own
name* (`GOPI_MATERIALS_ENTRIES`, not `ROOT`) — an option-expression semantic this
predicate deliberately does not model. The `TestSourceDirectivesIgnorePathMentions`
case pins the verdict; the deleted branch's five-line residue (`v = v[i+1:]`,
the `v == ""` guard, the `r == ""` disjunct) is not reproduced, because it is not
reachable under the rule this branch keeps.

## Verification (the numbers, as measured)

```
go build ./...                                                     rc=0
go vet ./...                                                       rc=0
gofmt -l internal/smoke/                                           (empty)
go test ./internal/smoke/ -run 'StagedScript|SourceDirective|EntryPoint'   ok, 8 tests
go test ./internal/smoke/                                          ok (whole package)
```

- **Non-blind red-proof**: removing `scripts/lib` → `26` unresolvable directives
  (was `25`); the entry point's own three among them, `stream-scope.sh` the new one.
- **Mutation (pre-fix predicate × new tests)**: `go test -overlay` swapping
  `sourceDirectivesIn` back to the pre-fix body (`448d4521c`'s, sha256 `8f8cc667…`) →
  `FAIL TestSourceDirectivesResolveTheScriptsOwnLiteralAssignment …
  sourceDirectivesIn(…) = [], want exactly [scripts/lib/stream-scope.sh]`.
  Red under the pre-fix predicate, green under this one — the added value is
  measured, not asserted.
- **Non-vacuity**: `unresolvableScriptSources` still returns the measured count
  and `t.Fatalf`s at `0`, so an empty closure can never read as a passing green
  (Rule 15). The new predicate-level sample also re-reads the entry point's own
  text and fails loudly if the spelling it pins ever disappears.

## What this deliberately does NOT do

- It does not re-land the deleted branch's other files: the branch's
  `e2e_source_scanner_test.go` asserts spellings this branch does not have (a
  bare-string positive, `$HERE` as a verbatim argument, a substring that
  tolerates a `$` in the relative part), and it would have restored the
  brace-form false positive above. The device was taken (a predicate-level
  sample that is red under the pre-fix code), not the file.
- It does not widen the predicate to `$HERE` / `$repo` / `$(dirname "$0")` /
  `$script_dir/../lib/x.sh`, which are most of this repo's directives (73 of the
  `.`/`source` directives name a path under `scripts/`; master's and this
  branch's literal spellings are the ones both can resolve). Resolving
  `$script_dir/../lib/x.sh` would mean joining and cleaning a relative path
  against the directive's own file — a real widening of the guard's demand, and
  a different task from the one this branch was given.
