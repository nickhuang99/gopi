<!-- doc-class: historical -->
# The delivery loop's test-compile leg — closing the guard that master stopped travelling through

**What this records.** Commit `928740fe` landed a `type result` collision between
a `cmd/agent` production file and a `cmd/agent` `_test.go`. `go build` stayed
green, `cmd/agent`'s test package was uncompilable for 44m36s, and **every
existing guard was green throughout**. This page states what those guards are,
how they were measured to be off the landing path, and the one leg that is now on
it.

## 1. The defect class, reduced to its minimum

`go build ./...` does not read a `_test.go` file. So two declarations of one
identifier — one in production code, one in a test file, same package — is
perfectly legal for the build and fatal for the test package:

```
$ cat app.go          → package app; type result struct{ N int }
$ cat app_test.go     → package app; type result struct{ M int }
$ go build ./...      → exit 0
$ go test -run '^$' -count=1 ./...
app/app_test.go:3:6: result redeclared in this block
    app/app.go:3:6: other declaration of result
```

This was measured in this run (see `## Verification` in the task report). It is
not a tuning issue, it is what the toolchain does.

## 2. The guards existed, and worked, and were unreachable

Two test-compile legs already ran exactly the right command:
`mergeBuildGateTestCompileCmd` (`internal/tools/merge_build_gate.go`,
`mergeBuildGateLegs`) and `mergeUnionGateGoTestCompileCmdFmt`
(`internal/tools/merge_union_gate.go`, `mergeUnionGateResolveSpec`). Their
callers, measured on the run's own tree:

```
$ grep -rn 'mergeBuildGate(repo\|mergeUnionGate(repo' --include='*.go' internal/ cmd/ | grep -v _test.go
internal/tools/task_merge.go:910
internal/tools/task_merge.go:945
internal/tools/repo_sweep.go:1461
internal/tools/repo_sweep.go:1465
```

All four are the MERGE path. And master has essentially stopped travelling
through it — measured on this clone at `40d5b21`:

```
$ git rev-list --merges --count 1189b758..HEAD     → 0
$ git rev-list --count 1189b758..HEAD              → 192
$ grep -h 'go test' internal/devworkspace/*.go | wc -l → 0
```

192 commits since the last merge commit, zero of them merged. The landing path is
the clone channel's delivery loop (`AttemptDelivery`): clean → `pull --rebase` →
declared build → push. Its only compile leg was the declared build, which is
`go build`, which does not read `_test.go`.

**The criterion this teaches**: "does a guard exist" is answered by grepping for
it; "does a guard COVER" is answered by asking whether its call site sits on the
path work actually lands through. The two answers were different here, and only
the second one mattered.

## 3. The leg

`internal/devworkspace/test_compile_gate.go`, called from `AttemptDelivery`
immediately after the man gate and before the push. Order matters twice over:
after the build, because the man gate reads `bin/gopi` — the binary THIS branch
produced — and before the push, because a refusal that lands after the ref moved
has refused nothing.

Four shapes, only one blocks (Rule 15):

| shape | outcome |
|---|---|
| no `go.mod` | abstention naming what was looked for |
| leg exited 0 | silent |
| leg exited non-zero | `BlockerKindTestCompile`, quoting the compiler |
| leg could not be STARTED (no toolchain, timeout, cancelled ctx) | abstention with the reason |

A timeout is deliberately an abstention: "the compiler did not finish" is not the
fact "the branch does not compile", and a leg that refuses on its own slowness is
a leg the first operator who meets it switches off.

`BlockerKindTestCompile` is its own kind so the refusal names the right failure:
sharing `BlockerKindBuild`'s kind would make it say "the build failed" about a
build that succeeded.

## 4. One spelling, and why the leaf package is forced

The command had two homes (`mergeBuildGateTestCompileCmdFmt` and the union gate's
`Sprintf` format). The clone channel cannot read either, because the edge
direction is fixed — measured:

```
$ grep -rln 'nick/gopi/internal/devworkspace' internal/tools/*.go | wc -l   → 46
$ grep -rn  'nick/gopi/internal/tools'        internal/devworkspace/*.go    → (no hits)
```

`internal/tools` imports `internal/devworkspace`, so `devworkspace` can never
import `tools` (Rule 14: the shared value lives in a leaf exported-constant
package every consumer can import). `internal/testcompile` is that package — no
imports but `fmt`/`time`, constants only:

```
GoBinary         = "go"
RunNothingFilter = "^$"
CommandFormat    = "%s test -run '%s' -count=1 ./..."   ← the ONE spelling
Command          = fmt.Sprintf(CommandFormat, GoBinary, RunNothingFilter)
TimeoutSec       = 300
Timeout          = time.Duration(TimeoutSec) * time.Second
```

Both merge gates and the delivery loop read it; nobody re-types it:

```
$ grep -rn "test -run '%s' -count=1 ./..." --include='*.go' .
./internal/testcompile/testcompile.go:83:	CommandFormat = "%s test -run '%s' -count=1 ./..."   ← 1 hit
$ grep -rn "go test -run '^\$' -count=1 ./..." --include='*.go' . | grep -vE ':[0-9]+:\s*//'
(no live code site — remaining matches are prose in comments/devdocs)
```

`CommandFormat` is a format and not only a finished string because one caller
(the union gate) composes the command from its own leg struct; a finished string
beside a format would be the second spelling this package exists to prevent.

## 5. Rule 27 — the leg cannot generate load, by construction

`-run '^$'` matches no test. Every test binary is BUILT and linked; nothing is
RUN: no ledger opened, no daemon started, no fixture executed. The green control
in `test_compile_gate_test.go` relies on exactly this — its fixture test calls
`t.Fatal` unconditionally, so a delivery that goes green proves the test body
never executed.

## 6. The cost, measured so it can be priced

```
$ /usr/bin/time -f 'WALL=%es' go test -run '^$' -count=1 ./...
WALL=40.17s          (warm caches, whole tree, this ground)
```

That is the price of the leg on every delivery attempt of a Go-module repo. It
buys the class of defect that took `cmd/agent` down for 44m36s while every guard
read green.

## 7. Anti-representation: the leg must be CALLED, not merely defined

`finishCleanupBranchCheckVerify` is the cautionary example in this same file
tree: `grep -rn 'finishCleanupBranchCheckVerify(' --include='*.go' internal/ cmd/
| grep -v _test.go` returns exactly one line — the definition. Zero production
callers. A guard in that state is a decoration.

So the regression test does not stop at "the refusal works": it reads
`devworkspace.go` and asserts the call is present AND ordered
(`manCensusGate` < `testCompileGate` < `publishGround`). Removing the call turns
it red — verified in this run (see the report's mutation evidence).
