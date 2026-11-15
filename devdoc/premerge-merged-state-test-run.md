<!-- doc-class: normative -->
# The merge path measured the COMPOSITION with a build — the branch-scoped test run on the trial-merged tree

Task: `01a12cbc534b782bb0e57a181ebe949e` · branch `task/01a12cbc534b782bb0e57a181ebe949e`
Predecessor: `01a12ca0bdbd796e92350c0ccb2e30e1` (the measurement: the peer's shared master carried two
red chat fixtures for ≈ 2 d 9.5 h).

## The defect, in one sentence

The client pre-merge gate constructed the trial-merged tree (`git merge --no-edit
<peer-master mirror>` in the run's own worktree) and measured it with the repo's
**declared BUILD only** — and `go build` **compiles no `_test.go`**, so a defect
that exists only in the **composition** of two branches (a writer green on its
branch + a reader green on its branch, red together) was invisible in every place
it was checked, on both sides of the wire.

The pre-change gate was not wrong about what it measured; it was wrong about what
a build can see. A clean merge + a passing build is exactly the state the peer's
master was in for two and a half days.

## What was added

`internal/tools/task_finish_premerge_smoke.go` — the merged-state TEST check —
and `finishCleanupPreMergeMergedChecks` in `task_finish_premerge.go`, which runs
**both** checks in **ONE** detached checkout of the merged commit (Rule 29's
one-ground economy; the checkout is created first because the test check's
package selection is derived FROM the merged tree).

### The check, and where each of its parts comes from

| part | source | why that source |
|---|---|---|
| command | `smokegate.ScopedForPackages(pkgs, quote)` → `make test-budget PKGS='…'` | `smokegate` owns the target, the assignment and the line's head (`ScopedTarget`, `ScopedPackagesAssignment`); the gate supplies only the list and a quoting function |
| packages | `smoke.TouchedPackages(dir, <peer-master mirror>, HEAD)` | the ONE branch→packages primitive; the root module is addressed `./`, **never `./...`** |
| time budget | `finishCleanupPreMergeTestTimeoutSec` (30 min) | derived from the entry point's own per-package budget (25 min, `scripts/test-budget.sh`) — it sits *above* it, so it only catches a wedged process the script's own accounting cannot |
| kill switch | `GOPI_FINISH_CLEANUP_PREMERGE_TEST` (`finishCleanupPreMergeTestEnv`) | read in exactly ONE place (`finishCleanupPreMergeTestOn`, Rule 2) |
| declaration probe | `smokegate.DeclaresScopedTarget(dir)` | asks the MERGED TREE whether it declares the target — a tree that declares nothing is an abstention, never a substitute command (Rule 12) |
| refusal marker | `finishCleanupPreMergeTestFailMarker`, check name `mergeCheckSmokeName` (`smoke`) | the refusal names the failing *check* and quotes the failing test's own message |

### The seam this exposed, and the fix

The first version of the check used `smokegate.Command` — and the fixture tests
caught it immediately: `make test`'s recipe is `scripts/test-budget.sh ./...`,
i.e. **the WHOLE MODULE**, which is the run `WholeTreeNeedle` exists to forbid.
The scoped half lived only inside `ScopedCommand`'s briefing TEXT
(`make test-budget PKGS='<…>'`), with no code-facing form — so a code caller had
no way to reach the bounded command except by re-spelling it (Rule 1b/13).

`internal/smokegate` now exposes `ScopedTarget`, `ScopedPackagesAssignment`,
`EntryPointFile`, `ScopedForPackages(pkgs, quote)` and
`DeclaresScopedTarget(dir)`; `ScopedCommand` is *built from* the same two
constants, so the briefing text and a caller cannot drift. Measured: the stray
`/pmsf…` package name the first run printed came from the whole-tree default
reading a leftover `/tmp` path — the evidence that the wrong command was being
run, before any assertion fired.

**The selection is taken against the peer-master mirror, not the local
integration branch.** The question this gate answers is "does *this branch*
compose with *the peer's* master", so the diff base must be the peer's master —
the mirror ref the gate itself just fetched. The delivery-point sync's `vet`
check answers the local question and keeps its local base; the two are separate
checks with separate purposes, deliberately.

Every argv token is single-quoted (`mergeCheckShellQuote`) before the line is
handed to `sh -c`, because an unquoted package list is re-split by the shell —
the same failure `smokegate.ScopedCommand`'s single-quoted `PKGS='…'` slot exists
to prevent one level up.

### Refusal vs abstention

A **refusal** is reserved for what the tree itself did: a failing test. It
unwinds exactly like the build refusal (`finishCleanupPreMergeUnwind`) — the
trial merge is rolled back, the branch keeps its reviewed tip, and **nothing is
pushed**.

Everything else **abstains** and is *reported* (Rule 15 — an unmeasured check is
never a silent pass): the check disabled by its kill switch, a merged tree
carrying no declared smoke entry point, a diff that touches no Go package, a
budget trip, an unresolvable module set. The abstention clauses are joined into
the step's detail by `finishCleanupMergeCheckAbstainSuffix` → `— not measured: …`.

The build's own abstention sentence survives the refactor: the client arm used to
print `finishCleanupPreMergeNoBuildMarker` for a repo with no build contract, and
that is what a repo with neither contract nor entry point still gets
(`build: no declared build contract; the merged tree carries no declared smoke
entry point (scripts/test-budget.sh)`).

## The proof (red → green, real git)

`internal/tools/task_finish_premerge_smoke_test.go` builds a three-tree world out
of real git (the `finishCleanupFixture` convention) whose **one package** forks
in a way that is green on each side and red together:

```
branch (feat)      table.go Mode="lower" + Table "alpha" + reader that upper-cases under Mode="upper"
                   + the BRANCH's own lookup_test.go expecting "alpha"        ⇒ GREEN alone
peer master        table.go Mode="upper" + Table "ALPHA" + reader with no upper-casing
                   (NO test)                                                 ⇒ GREEN alone
trial merge        Mode="upper" + the BRANCH's upper-casing reader + the BRANCH's test
                                                                             ⇒ RED
```

The merge is **clean** (different files) and the fixture's declared build is
`true`, so a refusal can only come from the test run — that is what makes the arm
un-passable by the check the gate already had.

| arm | what it measures |
|---|---|
| `TestPreMergeRefusesAComposedRedTheBranchAloneDoesNotHave` | the gate REFUSES, the detail names the check (`smoke`) **and** quotes the failing test's message, **no push happens**, the worktree unwinds clean |
| `TestPreMergeTestCheckOffIsReportedNotSilent` | with the switch off the gate behaves as it did before (build only ⇒ verified, push happens, abstention reported) — **and then measures the pushed commit directly** and shows it is RED under the same check. This is the *red half of the proof as a measurement*, not an inference |
| `TestPreMergeIsGreenWhenOnlyOneSideMoved` | the healthy world is not refused (no false alarm) |
| `TestPreMergeTestCheckIsBranchScopedAndNeverTheWholeTree` | the selection is exactly the touched package, the command goes through `smokegate.Command`, and the whole-module needle never appears |
| `TestPreMergeTestCheckAbstainsWithoutADeclaredEntryPoint` | a tree with no entry point ABSTAINS by name |

To run the declared entry point *in the fixture*, the fixture stages the repo's
own `scripts/` tree and `testdata/heavy-tests.tsv` into the fixture repo (a copy,
never a mutation, Rule 6). The Makefile's smoke target resolves `$(CURDIR)` to
the fixture repo, which is exactly the production behaviour (the check runs
inside the merged checkout).

## The stale-tracking-ref arm — MEASURED

The predecessor filed: *a reviewer read "the peer's master is broken" off
`refs/remotes/peer1/master`, a tracking ref 174 commits stale — nothing on any
agent-facing path had refreshed it.* This task decided that arm explicitly.

**Measured on this host, at the moment of writing:**

```
$ git for-each-ref --format='%(refname:short)=%(objectname:short)' refs/remotes/peer1/master
peer1/master=2c2acd773
$ git ls-remote peer1 refs/heads/master
61f58a3f17e43908a0a8912cf614b7f5a60d9eef	refs/heads/master
```

The tracking ref is stale **right now** and by a wide margin. Who writes it?

`grep` over the tree finds exactly three writers of remote-tracking refs, and
**none of them is `refs/remotes/<peer>/master`**:

1. **`SyncPeerFetch`** (`internal/tools/git_peer_fetch.go`) — the ONE peer fetch
   used by the pre-merge gate, the push-back ancestry gate and the worker's
   ground baseline. Its refspec is a **forced mirror into the reserved ref**:
   `+refs/heads/master:refs/remotes/gitPushcheck/master`. It never touches
   `refs/remotes/peer1/*`.
2. **`GitSyncClientPass`** (`internal/tools/task_sync_git.go`) — fetches with
   `--prune` and **no refspec**, which writes `refs/remotes/<peer>/*`. It is the
   **only** agent-facing writer of the tracking ref, and it runs in the client
   sync *pass* (a scheduled/CLI path), **not** in the merge/verify read path.
   Measured here: `refs/remotes/gitPushcheck/master=2c2acd773` and
   `refs/remotes/peer1/master=2c2acd773` were both 174 commits behind the real
   head, i.e. the tracking ref carries whatever the last client pass saw.
3. **`AdoptionSweep`** (`internal/tools/task_adoption.go`) — deletes
   `refs/remotes/origin/*` entries it has adopted; it does not refresh anything.

**Decision (a written decline, with its reason).** The arm is decided as:
**do not make the tracking ref fresh; do not add a second fetch.** The reasons,
in order of weight:

1. **The gate already reads a fresh ref and that is the point of the reserved
   mirror.** `SyncPeerFetch` writes `refs/remotes/gitPushcheck/master` and this
   gate reads *that* (plus the `ls-remote`-equivalent tip of the same fetch).
   A fresh-fetch read path for `refs/remotes/<peer>/master` would be a **second**
   channel for "what is the peer's master now", i.e. Rule 14's defect (two
   answers to one question) and Rule 35's (a new persistent behaviour around a
   problem that code already solves).
2. **No correctness property depends on the tracking ref.** Everything that
   judges a delivery resolves peer master through the mirror: the pre-merge gate
   (`gitPushbackCheckRef`), the push-back ancestry gate
   (`gitPushbackIsMerged`), and the ground baseline. So a stale tracking ref
   cannot make a gate accept something it should refuse — it can only mislead a
   **human or an agent reading `git branch -r` by hand**, which is exactly what
   the predecessor did.
3. **The cost of "fixing" it is real and recurring.** A fetch on every
   agent-facing read adds a network round-trip to a path that already has one
   bounded fetch per attempt, and it would have to be *forced* to survive a
   rewound peer — i.e. it would duplicate the mirror refspec's contract.
4. **What the reader needs already exists.** The honest instrument for "the peer's
   real head" is `git ls-remote <peer> refs/heads/master` (network truth) or the
   mirror ref (the gate's own fresh answer). The residual gap is *documentation*,
   not mechanism — and the mechanism that would close it (this same file,
   `git_peer_fetch.go`, whose header already states the mirror is the durable ref
   the gate needs) is where a reader lands when they ask.

Typed here as an explicit, falsifiable decline rather than an omission: if a
future incident shows a *gate* (not a reader) deciding on `refs/remotes/<peer>/*`,
this reason is void and the arm must be re-opened.

## What is NOT done (honest)

- **The check runs the repo's declared entry point, i.e. the repo's own Makefile
  smoke target.** For a foreign repo with no such target the check **abstains by
  name** (`the merged tree carries no declared smoke entry point`) — it never
  invents a command, per Rule 12. So the coverage this adds is real for *this*
  repo (and for any repo declaring the same entry point) and honestly reported as
  an abstention elsewhere.
- **No whole-tree run anywhere on this path** (Rule 27) — the selection is the
  touched diff, and `internal/smokegate.WholeTreeNeedle` is untouched.
- **The `-count=1` / flake axis is not addressed** here: the check runs the
  suite as declared. A flaky package could refuse a delivery; that is the
  declared suite's existing behaviour, not something this check introduces, and
  the flake path (`make flake-catch`) is a separate lever.
- **The peer-side (merger) lane was not touched.** Rule 28 puts the post-merge
  lane on peer1; the pre-merge gate is the client's, and this change is scoped to
  it. A composed red that is *already* on shared master is caught by the
  post-merge verify lane, unchanged here.
- **Budget note.** The check adds a test run to each delivery that has a clean
  trial merge. It is bounded (the entry point's own per-package budget) and
  branch-scoped, but it is not free; the kill switch exists for exactly the case
  where an operator needs the delivery to proceed without it.
