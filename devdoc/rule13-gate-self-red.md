<!-- doc-class: historical -->
# Rule-13 literal gates were red on master: they flagged their own explanatory comments

**Task:** fix the two Rule-13 "no bare literal" gates commit `b43b8c4c` turned red
on the master tip `dc75ea64`.
**Branch:** `fix/rule13-gate-self-red` (worktree `/home/user/workspace/gopi-rule13-selfred`, base `dc75ea64`).

## The defect (5-why)

1. Two tests failed on master tip `dc75ea64`:
   - `TestRule13_NoBareCharsPerTokenLiteral` (internal/agent)
   - `TestRule13_NoBareBufferLiteral` (internal/tools)
2. Each gate found its banned literal in the production file it was written to protect —
   `internal/agent/context_consts.go` and `internal/tools/limits_rule1.go`.
3. The literal is not in the code. The **runtime call sites are correct**: `charsPerToken`,
   `pipeReadBufferSize`, `maxPathSuggestions` and `suggestionPrefixLen` are the single
   sources of truth (Rule 13/14 satisfied). The literal sits in a `//` comment that
   *explains which* bare literal was named and why.
4. Why does a comment trip it? The gate is a **plain substring scan of the whole file
   text** — `strings.Contains(string(b), "/4 + 1")` — and it does not strip comments.
   The comment exists precisely to spell the literal the gate bans, so the commit that
   added the constant + its explanation turned its own gate red (a self-red commit:
   the gate was authored in the same commit it judges, so nothing exercised it against
   its own file before landing).

## The chosen fix: re-word the two comments (NOT teach the gate to parse comments)

| option | verdict |
|---|---|
| **comment reword** (chosen) | The gate's blunt whole-text scan *is* the property that makes it cheap, dependency-free and un-bypassable: no comment syntax to get right, no nested/block-comment or string-literal edge cases, no way to hide a literal behind a construct the parser does not model. Re-wording keeps that property at zero cost and zero behaviour change. |
| comment-aware gate | Adds a parser to the gate: every construct it fails to model becomes a hole a future re-inlined literal can hide in (`/* */` block comments, `//` inside a string, escaped quotes, build tags, generics-era syntax drift). A gate with a parser has a bypass surface; this one has none. It also makes the gate *larger* than the defect it guards. |

So: the comments were re-worded to say the same thing **without spelling the banned
literal**, and both gates were left byte-identical (their read-floor `scanned == 0 → Fatal`
untouched).

### The edits (Rule 4 — surgical, 2 hunks, comments only)

`internal/agent/context_consts.go` — the divide-by-four-plus-one heuristic is now
described in prose instead of spelling the expression:

```go
// GOPI.md Rule 1/13: the divide-by-four-plus-one token heuristic was inlined
// at five sites in context.go as a bare arithmetic expression; it is declared
// ONCE here and every site references it.
```

`internal/tools/limits_rule1.go` — the named limits are described by their role and
size unit instead of spelling the expressions:

```go
// path-suggestion hint (GOPI.md Rule 1/13: each bare literal was inlined at
// its call site — the 4 KiB pipe read buffer twice, the slice bound and the
// suggestion cap once each — and is declared ONCE here so a retune is a
// one-line change).
```

Explanatory content preserved: **which** heuristic/literal was named, **where** it was
inlined and **why** the constant lives in a separate file (the line-number doc anchors
that the neighbouring paragraph still explains). No bare literal is re-introduced
(Rule 1/13/14): the reworded text carries no numeric literal from either gate's banned
set — `4 KiB` is prose with a unit, not the Go expression `4096`.

Note the asymmetry the fix respects: `charsper_token_test.go` and
`named_limits_rule1_test.go` DO still spell `"/4 + 1"` / `"make([]byte, 4096)"` — that
is the gate's own scan needle, in a `_test.go` file the gate skips. It is supposed to
be there and hands it its precision, so it was left alone. Test files may also spell the
formula (`context_test.go`, `compact_cycle_test.go` compute the expectation directly) —
that is the gate's deliberate scope, unchanged.

## Evidence

### RED-before — on a detective checkout of the offending commit

`git worktree add --detach /home/user/workspace/gopi-rule13-red b43b8c4c`

```
=== RED-BEFORE: agent ===
--- FAIL: TestRule13_NoBareCharsPerTokenLiteral (0.00s)
    charsper_token_test.go:46: context_consts.go carries the bare /4 + 1 heuristic — reference charsPerToken instead
FAIL	github.com/nick/gopi/internal/agent	0.437s

=== RED-BEFORE: tools ===
--- FAIL: TestRule13_NoBareBufferLiteral (0.01s)
    named_limits_rule1_test.go:83: limits_rule1.go carries the bare pipe-buffer literal — reference pipeReadBufferSize
FAIL	github.com/nick/gopi/internal/tools	0.411s
```

### GREEN-after — the two gates, narrowed by `-run`, on the fix

```
$ go test ./internal/agent/ -run TestRule13_NoBareCharsPerTokenLiteral -count=1
ok  	github.com/nick/gopi/internal/agent	0.780s

$ go test ./internal/tools/ -run TestRule13_NoBareBufferLiteral -count=1
ok  	github.com/nick/gopi/internal/tools	0.774s

$ go test ./internal/tools/ -run 'TestRule13_NoBareBufferLiteral|TestPipeReadBufferSize|TestWriteSuggestions_RespectNamedLimits' -count=1 -v
--- PASS: TestWriteSuggestions_RespectNamedLimits (0.00s)
--- PASS: TestRule13_NoBareBufferLiteral (0.01s)
--- PASS: TestPipeReadBufferSize (0.00s)

$ go test ./internal/agent/ -run 'TestRule13_NoBareCharsPerTokenLiteral|TestEstimateTokensUsesCharsPerToken' -count=1
ok  	github.com/nick/gopi/internal/agent	0.342s
```

### Non-vacuity probe — the gates still catch a planted literal

A temporary production file spelling the banned literal in each package
(`internal/agent/zz_probe.go`, `internal/tools/zz_probe.go`), then removed:

```
--- FAIL: TestRule13_NoBareCharsPerTokenLiteral (0.00s)
    charsper_token_test.go:46: zz_probe.go carries the bare /4 + 1 heuristic — reference charsPerToken instead
--- FAIL: TestRule13_NoBareBufferLiteral (0.01s)
    named_limits_rule1_test.go:83: zz_probe.go carries the bare pipe-buffer literal — reference pipeReadBufferSize
```

Probes removed → both gates `ok` again. So the warding is proven still a real check on
this tree, not a scan that reads green because it scans nothing.

### Read-floor unchanged (verified by reading the gate source)

```
internal/agent/charsper_token_test.go:49:	if scanned == 0 {
internal/agent/charsper_token_test.go-50-		t.Fatal("scanned 0 production files — the glob is broken, this gate reads green-blind")
internal/agent/charsper_token_test.go-51-	}
internal/tools/named_limits_rule1_test.go:86:	if scanned == 0 {
internal/tools/named_limits_rule1_test.go-87-		t.Fatal("scanned 0 production files — the glob is broken, this gate reads green-blind")
internal/tools/named_limits_rule1_test.go-88-	}
```

Both files are untouched by this branch — `git diff --name-only` lists only the two
production files — so the gate bodies (scan predicate + `scanned` counter + Fatal
floor) are byte-identical to `b43b8c4c`.

### Trees remain green

```
$ go build ./...
build rc=0
$ go vet ./internal/agent/ ./internal/tools/
vet rc=0
$ go vet ./...
full vet rc=0
$ gofmt -l internal/agent/context_consts.go internal/tools/limits_rule1.go
(no output)
```

## Not Done (honest)

- The other three smoke failures on the master tip are **not** touched and are **not**
  attributed to this fix — they reproduce on pristine `f26f2bf1`
  (`TestDocAnchorsResolveInNormativeDocs`, `TestAssertionTotalityAuditGate`,
  `TestBareRunKeepsAllItsArtifactsInOneSessionDir`; the third's file is not in any
  landing diff). Recorded here so nobody reads them as regressions of this branch.
- No whole-tree test run is claimed from this branch: the merge/verify path is
  smoke and branch-scoped (`internal/smokegate`), and the branches' two packages were
  run with their named arms above.
- The gate itself was deliberately NOT made comment-aware; the residual exposure
  remains that a future comment spelling a banned literal turns the gate red again —
  which is exactly the intended, cheap, self-reporting behavior.
