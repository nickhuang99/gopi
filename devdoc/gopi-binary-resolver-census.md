<!-- doc-class: historical -->
# The stale-binary family — a census of hand-rolled gopi-binary resolvers, and the shape gate that keeps them out

**Task**: `01a1027bbb067e7f9bfc85deedd994f8` — a REWORK of the deleted, unlanded
branch `task/01a1025d1ab17561acbab9365e55c4f5` (that branch's own id), delivered
on top of master instead of being replayed.
**Measured at**: `9a612a87` (master) + the ported content — see §0.
**Supersedes**: nothing. It *corrects* a claim the source branch published (a doc
of the same name existed there, unlanded); the correction is §0b.

## 0. Why this is a rework, and what was NOT ported

The source branch was written against `b7432627`, before commit `88583afa`
("the suite's tested binary is the one built from the tree under test") landed on
master. That commit fixed the same stale-binary defect from the OTHER side: it
KEPT and EXTENDED the suite pin — `suitePinnedTestedBin` / `pinSuiteTestedBin` /
`resolveSuiteTestedBin` / `buildSuiteTestedBinFromTree` in `internal/testutil/run.go`,
plus its two guards (`TestGopiBinPublishesTheTreeBuildToChildren`,
`TestGopiBinFailsLoudlyOnAnUnusablePin`).

The source branch's `run.go` hunk **deleted that machinery** and replaced it with a
flat precedence. Porting it would have removed master's fix, so it was NOT taken:
the reconciliation keeps master's four functions and both guards, and adds from the
branch only what composes with them —

* the pin NAMES exported once (`BinaryPinEnv`, `SuiteBinEnv`, `BinDirName`,
  `BinFileName`; Rule 14 — one definition, referenced by resolver, fixtures and
  gate alike, never re-typed: Rule 13);
* `resolveGopiBin` extracted as the ONE precedence decision behind master's
  `GopiBin` (which keeps its exact skip/fatal policy) and the new
  `GopiBinOptional` (the census probe's "unverified, not skipped" split);
* the census, the fixes, and the AST shape gate.

### 0b. The claim this doc CORRECTS

The source branch's census was motivated by a claim that the pin does not cover
the tree's own `bin/gopi`. **That claim is false, and it is corrected here rather
than repeated.** Measured on the source branch's own tree: when the suite pin
runs, `testutil` prints `testutil: tested binary pinned to
/dev/shm/.../suite-tested-bin-*/gopi` and that path REPLACES the tree's `bin/gopi`
for every case that asks `GopiBin` — the pin's scratch root is the suite's pinned
temp root (`/dev/shm` here), not `/tmp` (entry count 3983 → 3983).

The narrow, correct motivation for this gate is different: **the pin cannot help a
resolver that never asks it.** A test that spells `bin/gopi` itself, or reads a pin
channel by literal name, bypasses `GopiBin` entirely — the pin is not in that code
path at all. The family this census closes is the BYPASS, not a hole in the pin.

## The defect this census is about

`internal/testutil.GopiBin` is the one resolver for a gopi binary a test drives.
A test that resolves the binary *itself* is the **stale-binary family**: in a
shared checkout (many linked worktrees side by side, Rule 6a) a path spelled
relative to the test's own process is not necessarily *this* worktree's build, so
the case silently executes another tree's binary and its verdict is about code
nobody is working on. `devdoc/tested-binary-pin.md` carries the measurement: a
reported "flake" that was two different revisions, one of them reached through
exactly the `filepath.Join("..", "..", "bin", "gopi")` spelling. (`88583afa`
closes the path where a case DOES ask; this gate closes the path where it does
not.)

## 1. Census — the generator, and the figure

The census is produced by the gate itself, over the **parsed** module sources
(comments and doc prose that merely discusses a spelling are not hits):

```bash
cd <module root> && go test ./internal/testutil/ \
    -run TestGopiBinaryResolverCensus -count=1 -v
```

It prints one line per hit as `<state> <file>:<line> [<kind>] <snippet>`, then
`census: N hit(s) over M Go files, P permitted`.

Two figures (Rule 38 — a census figure names its generator and the revision it
was measured at):

| revision | hits | Go files scanned | permitted |
|---|---|---|---|
| `b7432627` (pre-fix) | **6** | 1991 | 0 |
| `9a612a87` + this port (shipped) | **0** | 1992 | 0 |
| `adb7dc0` (+ the argv-fixture row, §2c) | **3** | 2858 | 3 |

The pre-fix 6, verbatim (`kind file:line snippet`), as the generator printed it:

```
env-pin-read       cmd/agent/cli_tasks_sweep_zombies_test.go:141  bin := os.Getenv("GOPI_BIN")
env-pin-read       cmd/agent/cli_tasks_sweep_zombies_test.go:236  if ambient := os.Getenv("GOPI_BIN"); ambient != "" {
literal-bin-gopi   cmd/agent/cli_verb_census_test.go:553  bin/gopi
literal-bin-gopi   cmd/agent/cli_verb_census_test.go:553  bin/gopi
join-bin-gopi      cmd/agent/cli_web_align_test.go:502  bin := filepath.Join("..", "..", "bin", "gopi")
literal-bin-gopi   cmd/agent/task_session_cwd_freshness_scope_test.go:64  bin/gopi
TOTAL 6 hit(s) over 1991 Go files
```

The pre-fix figure is **re-derived, not remembered** — the census test can only
measure the tree it stands in, so the SAME detector bytes were lifted into a
standalone scanner and run over the pre-fix tree read out of the object store
(never a second checkout: Rule 29):

```bash
D=$(mktemp -d -p /dev/shm gopi-prefix-census-XXXX)          # off / (Rule 22)
git archive b7432627 cmd internal | tar -x -C "$D/tree"     # read-only extraction
# gate's detector block (gopiResolverHits + kinds + constants, run.go constants inlined)
# + the same walk/exclusions as gopiResolverScanRoot, as package main
cd "$D" && go build -o pc . && ./pc "$D/tree"      # → TOTAL 6 hit(s) over 1991 Go files
cd <this worktree> && ./pc .                        # → TOTAL 0 hit(s) over 1992 Go files
```

The file-count difference (1991 → 1992) is this port's own new gate file; the
shipped run is the census test itself, which prints the same `TOTAL` line under
its `census:` label.

```bash
grep -rn --include=*.go -E '"bin", *"gopi"|Getenv\("GOPI_BIN"\)' cmd internal   # at b7432627
```

### Four detector kinds, and the one class deliberately NOT flagged

| kind | what it is |
|---|---|
| `join-bin-gopi` | `filepath.Join(<module root>, "bin", "gopi")` — a base spelled `".."` or produced by a call (`moduleRoot()`, `repoRootFromCwd()`) |
| `literal-bin-gopi` | one **relative** string literal spelling `bin/gopi` or `…/bin/gopi` |
| `env-pin-read` | a direct `os.Getenv`/`os.LookupEnv` of `GOPI_BIN` / `GOPI_TEST_BIN` outside the one resolver (the names now come from `testutil.BinaryPinEnv` / `testutil.SuiteBinEnv`) |
| `path-resolved-name` | `exec.Command("gopi", …)` — PATH decides which binary runs |

**Not flagged**: `filepath.Join(<identifier>, "bin", "gopi")` — a fixture base.
Every hit of that shape in this tree is a *synthetic repository* built under
`t.TempDir()` (`repo := filepath.Join(t.TempDir(), "repo")`), where the string is
part of the fixture's own declared build contract, not a resolution of the binary
under test. Flagging it would have filled the permit table with rows that lie
about being resolvers. Absolute paths (`/usr/local/bin/gopi`, `/opt/bin/gopi`)
are likewise out: they are synthetic or installed-path literals in this tree, and
they cannot reach another worktree.

## 2. Classification

### (a) must go through the one resolver — FIXED here

| site | why it is the family |
|---|---|
| `cmd/agent/cli_web_align_test.go:502` | `filepath.Join("..", "..", "bin", "gopi")` — the measured shape verbatim; and **the variable was dead** (nothing in the function used `bin` — `testutil.StartGopi` re-resolves), so the `os.Stat` skip could only ever have *cancelled a case* |
| `cmd/agent/cli_verb_census_test.go:553` | the census probe tried `bin/gopi` relative to the package dir — the cwd of the test process, i.e. whichever tree the run stands in |
| `cmd/agent/cli_tasks_sweep_zombies_test.go:141,236` | read the pin channel by literal name instead of through the package that owns the precedence |
| `cmd/agent/task_session_cwd_freshness_scope_test.go:64` (+3 joins) | the fixture hand-spelled `bin/gopi`; it is now named from `testutil.BinDirName` / `BinFileName` |
| `cmd/agent/cli_tasks_rebuild_test.go:71`, `cli_tasks_rebuild_pass_test.go:44`, `cli_tasks_rebuild_guard_test.go:55,78` | hand-spelled the `bin/gopi` tail of a fixture's declared build; now the resolver package's own constants |

| `cmd/agent/gopibin_test.go:31,46` | the `GOPI_TEST_BIN` channel and the artifact's name were re-typed as this package's own literals, and read by this package's own `os.Getenv` — a second spelling of a name the resolver owns. Now `testutil.SuiteBinEnv` / `testutil.BinFileName` (Rule 13/14); the reader stays local on purpose, because this package's build is a DIFFERENT artifact (`go build` of `.`, not `./cmd/agent/`, and its own per-run cache) and routing it through `GopiBin` would make it the resolver's literal, not a caller of it. |

### (b) exempt, with the reason

| class | files | reason |
|---|---|---|
| a **fixture repo's** `filepath.Join(repo, "bin", "gopi")` | `internal/tools/rebuild_tracked_guard_test.go:282`, `internal/tools/rebuild_test.go`, `internal/tools/rebuild_dirty_witness_test.go`, `internal/version/version_report_probe_test.go`, `cmd/agent/cli_tasks_rebuild_*` | these are the **build contract's** subject (`RebuildAfterMerge` writes the declared path of a repo it was given). The repo is a `t.TempDir()` synthetic one; the path is the artifact under test, not the binary the case drives. (The `cmd/agent` ones were still normalised onto the shared constants — see (a) — because the literal there was gratuitous.) |
| installed / foreign absolute paths | `internal/testutil/hp_worker_launchers_test.go:122`, `internal/runcmd/runcmd_test.go:17,78-88`, `internal/shot/textcapture_test.go:500-502`, `internal/daemoncwd/daemoncwd_test.go:285`, `internal/signalrec/signalrec_test.go:1390`, `internal/tools/identity_shared_test.go:81,232`, `cmd/agent/cli_scratch_gc_deps_test.go:329`, `cmd/agent/daemon_cgroup_e2e_test.go:474` | synthetic argv/env strings and installed-path expectations (parser, argv reduction, pid-identity fixtures). They cannot resolve to another worktree's build — the detector does not fire on them, and no permit is needed. |
| prose | `cmd/agent/cli_tasks_sweep_zombies_test.go:218`, `internal/testutil/run.go` comments, `devdoc/*` | a comment *about* the shape is not the shape; the detector parses and ignores comments. |
| `os.Executable()`-based resolution | `internal/shot/surface.go:80`, `internal/sandbox/owner.go:76`, `internal/tools/tasks.go:4873,8321`, `internal/testutil/fixturelifetime.go`, `lifetimeprobe.go` | a **different question**: "which executable is running" (self-identity), never "which gopi build should this case drive". |
| `scripts/*.sh`, `Makefile` | `Makefile:19-22`, `scripts/e2e-task-merge.sh`, `scripts/materials/*` | outside the Go test surface; several are *the* declared build or an operator entry point whose whole job is to build/point at a named binary. |

## 2c. The argv fixture vs the resolver — `cmd/agent/worker_status_task_test.go:134`

*(measured 2026-10-24 at `adb7dc0`, the cut this note was written on)*

The census went red on a THIRD `literal-bin-gopi` hit, in the worker-status
family:

```
$ CGO_ENABLED=0 go test ./internal/testutil -run '^TestGopiBinaryResolverCensus$' -count=1 -v
    gopibin_resolver_gate_test.go:536: UNPERMITTED cmd/agent/worker_status_task_test.go:134 [literal-bin-gopi] ./bin/gopi
    gopibin_resolver_gate_test.go:539: census: 3 hit(s) over 2858 Go files, 2 permitted
```

**The judgment: the SITE is the argv-fixture class, the PREDICATE is right.**
The literal is one element of a `[][]string` table, and the table is the input
under test, not a path anything resolves:

```go
// worker_status_task_test.go:132-136
for _, argv := range [][]string{
	{"gopi", procname.VerbWorker},
	{"./bin/gopi", procname.VerbWorker},   // ← the hit
} {
	got := workerTaskFromArgv(argv)        // worker_status_task.go:116
```

`workerTaskFromArgv` is a **pure argv parser**: it reads `argv[1]` for the worker
verb, hands `argv[2:]` to `scanWorkerArgs`, and returns a struct. The file it
lives in touches no filesystem: `grep -nE
'os\.Stat|os\.Open|exec\.|GopiBin|LookupEnv|Getenv'` over the test **exits 1**
(no match), and its import set is `encoding/json os path/filepath strings
testing time` + `internal/procname`. What the case pins is that a **loop
worker's procedural argv** (however the operator spelled the program — `gopi`,
`./bin/gopi`, an absolute path) carries no task row, and the renderer must print
the not-recorded marker rather than an empty field. Delete the token and the
`./bin/gopi` spelling — the very shape an operator on this host types — stops
being covered.

**Why the predicate should not be narrowed instead.** A predicate able to exempt
this site and still catch a real resolver would have to follow DATA FLOW: is
this literal ever handed to an `exec`, an `os.Stat`, a fixture writer? The
detector is a shape scan by construction (devdoc § "Not done": it does not
resolve data flow), so the narrowing would land on either side of the defect at
once — it would miss the real resolver one caller-link away, and it would
exonerate a literal that reaches `exec.Command` through a variable. The gate's
own design note says the same thing from the other direction: `./bin/gopi` is a
*relative* literal, which is exactly the spelling that wanders into another
worktree when something executes it. The detector cannot tell the two apart, and
the reviewed allow-list is where that difference is supposed to be recorded.

The alternative "fix" in the other direction is worse than a permit: the case
must NOT become an integration test through `testutil.GopiBin` (it would then
need a built binary to test an argv parser, and the `./bin/gopi` spelling would
lose its coverage), and the token must not be assembled from `BinDirName` /
`BinFileName` — `cmd/agent`'s test package would import the resolver package to
obtain two spellings, which is Rule 13's reverse (and, for a token that is
deliberately NOT this tree's path, a lie in code form).

So the row is a `(file, kind)` permit with its reason, on the same footing as
the two taskreport rows — and like them it stays inside the ratchet: if that
table ever stops carrying `./bin/gopi`, the permit becomes a STALE PERMIT and
the census goes red.

**Two-sided red proof** (the mutant lives OUTSIDE the tree; `-overlay` points
the build at it, so nothing is edited in place — Rule 30 stays intact). The
mutant changes the row's KIND to a name no hit can carry:

```
$ CGO_ENABLED=0 go test -overlay /dev/shm/gopi-redproof-…/overlayA.json \
      ./internal/testutil -run '^TestGopiBinaryResolverCensus$' -count=1 -v
    UNPERMITTED cmd/agent/worker_status_task_test.go:134 [literal-bin-gopi] ./bin/gopi
    census: 3 hit(s) over 2858 Go files, 3 permitted
    STALE PERMIT cmd/agent/worker_status_task_test.go [mutant-argv-kind]: ARGV FIXTURE, not a resolver: …
--- FAIL: TestGopiBinaryResolverCensus
$ git status --porcelain          # (before the commit that lands the row) → prints nothing
```

Both arms fail at the same moment — the hit is UNPERMITTED *and* the row is
STALE — which is the property that proves the green comes from the permit's
`(file, kind)` KEY and not from a detector that stopped firing.

## 2d. The refusal fixture vs the resolver — `internal/tools/deploy_drift_test.go:380-381`

*(measured 2026-10-29 at `c460311`, the cut this note was written on)*

A FOURTH and FIFTH hit, from the deploy-drift family landed at `e7a09e4`
(`--drift measures the RUNNING artifact`). Both are `join-bin-gopi`, and both
sit inside ONE `[]string`:

```
$ CGO_ENABLED=0 go test ./internal/testutil/ -run '^TestGopiBinaryResolverCensus$' -count=1 -v
    gopibin_resolver_gate_test.go:536: UNPERMITTED internal/tools/deploy_drift_test.go:380 [join-bin-gopi] filepath.Join(string(filepath.Separator), "usr", "local", "bin", "gopi"),
    gopibin_resolver_gate_test.go:536: UNPERMITTED internal/tools/deploy_drift_test.go:381 [join-bin-gopi] filepath.Join(string(filepath.Separator), "bin", "gopi"), // no parent
    gopibin_resolver_gate_test.go:539: census: 5 hit(s) over 3055 Go files, 3 permitted
```

```go
// deploy_drift_test.go:376-385
for _, bad := range []string{
	"",
	// A bin/ that is not <repo>/bin: /usr/local holds no repository, and this
	// is the exact path this machine's deployed artifact lives at.
	filepath.Join(string(filepath.Separator), "usr", "local", "bin", "gopi"),
	filepath.Join(string(filepath.Separator), "bin", "gopi"), // no parent
} {
	if got, ok := DeployDriftRepoOf(bad); ok {
		t.Errorf("DeployDriftRepoOf(%q) must refuse …; got (%q, true)", bad, got)
	}
}
```

**The judgment: the SITE is the refusal-fixture class, the PREDICATE is right.**
The two joins are the INPUTS the case asserts `DeployDriftRepoOf` REJECTS
(`ok=false`), not paths anything resolves. Measured, in the order a re-checker
would run it:

* **They are the shapes whose parent holds no checkout.** `DeployDriftRepoOf`
  (`internal/tools/deploy_drift.go:246`) returns early on the `bin` level for
  `/bin`, and its only filesystem touch for `/usr/local` is
  `os.Lstat("/usr/local/.git")`, which fails here —
  `ls -d /usr/local/.git` exits 2 (`No such file or directory`). That arm is the
  function's own recorded reason (`deploy_drift.go:235-244`: "a shape-only rule
  answered `/usr/local` — a directory that holds no repository").
* **Nothing executes them.** `grep -nE 'os\.Stat|os\.Open|exec\.|GopiBin|Getenv|LookupEnv'`
  over the test file matches exactly one line — `exec.Command("git", …)` at
  line 70, the repository fixture — so no gopi binary is started by this file
  and `testutil.GopiBin` is not involved at all.
* **What they pin is the installed-artifact shape.** Delete the literals and
  `/usr/local/bin/gopi` — the path this host's deployed artifact actually lives
  at (`/usr/local/bin/gopi -> /home/user/work/gopi/bin/gopi`) — stops being
  covered. Routing them through `testutil.GopiBin` would invert the case: it
  would assert the under-test binary is REFUSED.

So the row is a `(file, kind)` permit with its reason, keyed narrowly on
`internal/tools/deploy_drift_test.go | join-bin-gopi` — it exempts that file's
join-shape hits and nothing else, and it becomes a STALE PERMIT the moment the
table stops carrying them.

**The permit's own narrowness arm was vacuous until this row exposed it.** The
KIND-NARROW arm in `TestGopiBinaryResolverPermitsAreNarrowAndRatcheted` added a
hard-coded `join-bin-gopi` sibling to prove that a permit on one shape does not
cover another — and every earlier row was `literal-bin-gopi`, so the arm had
never been tested against a permit whose OWN kind was the join. With the new row
the sibling re-added the permit's own key and the arm reported `reported: []`
(measured: `permit internal/tools/deploy_drift_test.go [join-bin-gopi] let
ANOTHER shape through in the same file`). The sibling now comes from
`gopiResolverOtherKind(p.Kind)`, so the arm names a kind that is never the key's
own — the ratchet is verified for every row, including the two whose kind is the
join.

After the row (and that arm's fix), same command:

```
    gopibin_resolver_gate_test.go:580: permitted  internal/tools/deploy_drift_test.go:380 [join-bin-gopi] filepath.Join(string(filepath.Separator), "usr", "local", "bin", "gopi"),
    gopibin_resolver_gate_test.go:580: permitted  internal/tools/deploy_drift_test.go:381 [join-bin-gopi] filepath.Join(string(filepath.Separator), "bin", "gopi"), // no parent
    gopibin_resolver_gate_test.go:583: census: 5 hit(s) over 3055 Go files, 4 permitted
--- PASS: TestGopiBinaryResolverCensus
ok  	github.com/nick/gopi/internal/testutil	8.804s
```

## 2e. The production constant definition vs the resolver — `internal/mansource/paths.go:29`

*(measured 2026-10-29 at `982fcba`, task `01a188ad`; the ONLY 6th hit at that cut)*

The census reported one `literal-bin-gopi` hit that is neither a violation nor a
candidate for a permit row:

```
UNPERMITTED internal/mansource/paths.go:29 [literal-bin-gopi] bin/gopi
```

**It is not a resolution.** `internal/mansource` is a leaf: `paths.go` declares no
import and no function, only package-level constants carrying the man pipeline's
shared spellings (Rules 1/13/14). `BuiltBinaryRel = "bin/gopi"` is the declared
build artifact's REPO-RELATIVE path, and its one production consumer hands it to
the generator as an **argv element**, never as a path to walk:

```
internal/devworkspace/man_gate.go:185
		mansource.GeneratorRel, manGateRepoArg, mansource.BuiltBinaryRel, mansource.CheckFlag)
```

`mancensus.go` takes it as `pos[1]` (`bin = pos[1]`), i.e. a string argument. An
argv element has no CWD, so it cannot land in another worktree's build — the
stale-binary defect is exactly the opposite shape (Rule 6a, `devdoc/tested-binary-pin.md`).

**It is not a permit either**, and this is the part worth recording. Every row in
`gopiResolverPermits()` asserts "the detector's pattern is present but NO
gopi-under-test binary is being resolved". Writing one for a PRODUCTION spelling
that the delivery channel really does execute would be a row that is false, and it
would leave the detector's blind spot — it fires on a literal's VALUE, unable to
tell a resolution from the place a shared spelling is DEFINED — permanently in
place. Nor can the site route through `testutil.GopiBin`: that would make a
zero-dependency production package import the test scaffold (and answer a
different question — `BuiltBinaryRel` is "where a repo's declared build writes",
not "which binary does this run drive").

**The fix is the third answer: teach the detector the one structural fact that
makes the site safe.** `gopiResolverDeclarationOnly` excludes a literal only when
BOTH hold — the file declares **no import and no function** (an inert leaf of
const/var/type), and the literal is the **value of a package-level const/var
spec**. A site that can `stat`, `open`, `Join`, read an env channel or `exec`
needs a function body and a package that supplies it, so it fails the first half
and is reported exactly as before. The census PRINTS every excluded literal with
`gopiResolverDeclOnlyWhy`, so the exclusion is auditable from `-v` alone rather
than silent (Rule 15).

Both directions are pinned by `TestGopiBinaryResolverDeclarationOnlyFileIsNotAResolution`:

| arm | fixture | expected |
|---|---|---|
| leaf | no import, no func, `const builtBinaryRel = "bin/gopi"` | excluded, and NAMED as excluded |
| resolver | `import "os"` + `const bin = "bin/gopi"` + `os.Stat(bin)` | a `literal-bin-gopi` hit, exclusion empty |
| table | no import, no func, but a `[]string{"bin/gopi"}` ELEMENT | a hit — the criterion is the definition site, not the file's shape |

## 3. The fixes (minimal, on the one resolver)

* `internal/testutil/run.go` — master's pin machinery is untouched
  (`suitePinnedTestedBin` / `pinSuiteTestedBin` / `resolveSuiteTestedBin` /
  `buildSuiteTestedBinFromTree`, and both guards still pass). What was added is
  the reconciliation: the pin **names** are exported where they were unexported
  (`SuiteBinEnv`, `BinaryPinEnv`) and the artifact's location with them
  (`BinDirName`, `BinFileName`) so a fixture that must *create* a repo's own
  `bin/gopi` spells it from the same definitions (Rule 1/14) — one definition,
  referenced by resolver, fixtures and gate alike, never re-typed (Rule 13).
* `resolveGopiBin` extracts master's precedence as ONE decision with two faces:
  `GopiBin` (unchanged policy — skip when nothing resolves, FATAL when a pin is
  unusable) and `GopiBinOptional` (`(path, ok, reason)`), which the census probe
  uses to report its direction UNVERIFIED instead of adopting the skip policy.
  **No second resolver, no new precedence, no weakened assertion.**
* the `cmd/agent` sites listed in §2(a) now call the resolver / the constants
  (the `cli_web_align_test.go` one is deleted outright — the variable was dead).

## 4. The gate

`internal/testutil/gopibin_resolver_gate_test.go` — two tests:

* `TestGopiBinaryResolverCensus` — parses `cmd/` + `internal/`, prints every hit
  with its kind, and fails on any hit that is neither permitted nor removed; a
  permit whose hit disappeared is itself an error (the table only ratchets down).
  It refuses to pass on a scan that read nothing (`gopiResolverScanFloor = 400`
  files against 1992 present), because "found nothing" and "never ran" must be
  distinguishable (Rule 15).
* `TestGopiBinaryResolverGateRedProof` — the detector's own red/green: a fixture
  carrying all four hand-rolled shapes is reported **by kind**, while the clean
  fixture (a synthetic repo's `bin/gopi`, an installed absolute path, a doc
  comment quoting the spelling) reports **nothing**.

## 5. Evidence (measured at `9a612a87` + this port)

```
$ go test -count=1 -run 'TestGopiBin|TestGopiBinaryResolver' ./internal/testutil/ -v
--- PASS: TestGopiBinPrefersTheSuitePinnedBinary
testutil: tested binary pinned to /dev/shm/suite-internal-testutil-run-…/suite-tested-bin-…/gopi
--- PASS: TestGopiBinPublishesTheTreeBuildToChildren (11.20s)
--- PASS: TestGopiBinFailsLoudlyOnAnUnusablePin (0.37s)      ← master's two guards, intact
--- PASS: TestGopiBinaryResolverGateRedProof
    census: 0 hit(s) over 1992 Go files, 0 permitted        ← GREEN on the shipped tree
```

RED on a REAL-TREE fixture (not only the in-memory source fixture): a hand-rolled
resolver planted at `cmd/agent/zz_temp_redproof_resolver_test.go`, gate re-run —

```
--- FAIL: TestGopiBinaryResolverCensus
    UNPERMITTED cmd/agent/zz_temp_redproof_resolver_test.go:10 [join-bin-gopi] bin := filepath.Join("..", "..", "bin", "gopi")
    census: 1 hit(s) over 1993 Go files, 0 permitted
```

— and GREEN again the moment the fixture file is removed.

The PERMIT RATCHET, both directions: with the fixture in place AND a permit row
keyed `(cmd/agent/zz_temp_redproof_resolver_test.go, join-bin-gopi)` the gate is
GREEN; removing only the fixture file makes the same permit a STALE PERMIT and the
gate RED (`census: 0 hit(s) over 1992 Go files, 1 permitted` + `STALE PERMIT …`),
i.e. the table cannot ratchet up by neglect.

## Not done (honest)

* The detector does not resolve data flow: a path handed to
  `testutil.RunGopi`/`StartGopiBin` through a variable that was *itself* built
  elsewhere, on another code path, is not caught. It catches the four spellings
  measured in this tree.
* `GopiBinOptional`'s skip-vs-unverified split is exercised by the census probe
  only; no unit test pins its two arms (the red proof covers the detector, which
  is what the epic's acceptance names).
* No site was found that drives a *foreign* worktree's binary today; the family
  is closed at the spelling level, which is where the measurement says the
  mechanism lives.
