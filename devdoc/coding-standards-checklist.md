<!-- doc-class: normative -->
# Coding Standards Checklist — machine-checkable review rules for GoPi

**Status:** research only. No check in this document is implemented, no linter is
configured, no code was changed except this file. `make fmt` was explicitly **not**
run (briefing constraint).

**Scope of evidence:** every number in the *Our violation history* column comes from a
measurement run against this repo at commit `587624f` on `feat/coding-standards-checklist`
(a linked worktree at `/home/user/workspace/gopi-standards`, never the main checkout).
Production scope = `*.go` excluding `_test.go`. Counts are reproducible with the command
quoted in each row; `file:line` citations are the first hits of an exhaustive scan, not
a sample.

**Baseline measured at that commit**

| Metric | Value |
|---|---|
| Production Go files | 160 |
| Test Go files | 278 |
| Production LOC | 70,354 |
| Direct module deps | 1 (`modernc.org/sqlite v1.44.0`) + 9 indirect |
| Linter config present | **none** (no `.golangci.yml`, no staticcheck config) |
| CI present | **none** (no `.github/`) |
| Makefile targets | `build`, `test`, `fmt`, `vet` |
| `-race` anywhere | **0 occurrences** |
| Test functions | 2,055 |
| `t.Parallel()` calls | **0** |
| `t.Skip` / `t.Skipf` | 143 |
| `time.Sleep` in tests | 50 |
| GOPI.md | 488 lines, 15 rules |

---

## How to read a row

* **Rule** — the normative statement, phrased so a machine can decide it.
* **Source** — a real, retrievable URL plus the quoted line that carries the rule.
* **Why** — the failure mode the rule prevents, in this codebase's terms.
* **How to check** — `regex` | `AST` | `analyzer` | `test` | `grep`, with the concrete
  mechanism and the production scope it must run over.
* **Our violation history** — measured, with `file:line`.
* **Sev** — `H` (correctness/data-loss/concurrency), `M` (maintainability at scale),
  `L` (cosmetic/consistency).
* **GOPI.md** — `covered` | `partial` | `new` against the real file.

A rule is in this document **only if it has a mechanical check**. Rules that are real
industry advice but not mechanically decidable are in
[§12 Rules we deliberately reject](#12-rules-we-deliberately-reject).

---

## 1. Naming and constants

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 1.1 | No bare string/number literal that duplicates a value already defined as a constant in the repo (tool names, task statuses, dir names, source values, CLI verbs). | PROJECT.md Rule 1b/13 (internal, quoted below) — no external source needed; the anti-regression test pattern is PROJECT.md Rule 13. | A constant plus bypassed literals silently drift: one site is fixed, the others keep the old spelling, and persisted data written under one spelling is searched under another. | `test` — grep-gate test per constant class: scan production `.go` files, fail any file other than the definition file that still contains the literal (pattern already proven by `TestUnknownNode_MirrorLiteralGate`). | `"sessions"`, `"unknown-node"`, `".gopi"` (5 literal sites, no exported const: `internal/tools/task_sync_git.go`, `internal/memory/memory_sync_claim.go`). | M | covered (Rule 13) |
| 1.2 | A constant defined in a package must be **referenced**, never re-typed as a literal, *within the package that defines it*. | PROJECT.md Rule 13 (internal). Cross-package values → PROJECT.md Rule 14 (small exported-constant leaf package, `internal/toolnames` precedent). | Mirrored copies are future inconsistencies; a rename becomes an N-site grep that misses one site. | `analyzer` — custom `go/analysis` pass: for every exported/unexported const of basic type, flag string literals with the identical value outside the const's own declaration file. | Not yet measured as a class; the `taskSessionsRootName` case (defined 1×, referenced 2×, bypassed 10×) is documented in PROJECT.md Rule 13 from the 2026-09-09 review. | M | covered (Rule 13) |
| 1.3 | Exported identifiers carry a doc comment that starts with the identifier's name. | https://go.dev/wiki/CodeReviewComments — "Doc Comments": *"All top-level exported names must have doc comments, as should non-trivial unexported type or function declarations."* | Undocumented exported API is unusable from another package without reading the body — the defect class that makes `internal/` refactors expensive. | `analyzer` — `golint`-style: `go/doc` over each package, flag exported decls whose doc is empty or does not begin with the identifier. (`staticcheck ST1000/ST1020` covers the package-comment half.) | 3 exported funcs without doc comment: `internal/tools/tasks.go:2900 TaskShow`, `internal/gopilog/reader.go:48 ResolveSessionDir`, `internal/agent/loop.go:2776 NewAgent`. | L | new |
| 1.4 | No `Get` prefix on accessors. | https://google.github.io/styleguide/go/best-practices — *"avoid the prefix `Get`"* (Go naming: `Counts()` not `GetCounts()`). | The Go ecosystem reads `GetX` as a network/IO fetch; using it for a cheap field accessor misleads callers about cost. | `regex` — `^func (\([^)]*\) )?Get[A-Z]` over production files; allow-list external-interface implementations. | 1 site: `internal/agent/context.go:233` (`func (c *Context) GetSize()` and neighbours). | L | new |
| 1.5 | Initialisms keep their case (`ID`, `URL`, `HTTP`, `JSON`, `API`), not `Id`/`Url`. | https://go.dev/wiki/CodeReviewComments — "Initialisms": *"Words in names that are initialisms or acronyms (e.g. `URL` and `ID`) have a consistent case."* | Inconsistent casing fragments grep: a search for `TaskID` misses `TaskId` sites, hiding both callers and bugs. | `analyzer` — `staticcheck ST1003` (naming conventions); run as a review gate on changed files. | Not measured (`ST1003` was never run — no linter is configured). | L | new |
| 1.6 | Directory and file names that carry meaning are named constants, not literals. | PROJECT.md Rule 1b (*"Directory / file names → named consts (taskSessionsRootName, …)"*) — internal rule, no external source. | Same drift failure as 1.1, with the extra bite that the value is part of persisted state layout. | `test` — literal gate: `"sessions"`, `".gopi"`, `"archive"` may appear only at their definition site. | 5 `".gopi"` literals with no exported const (see 1.1). | M | covered (Rule 1b) |
| 1.7 | Function and variable names state what they return; no `truncateRunes2`-style disambiguating suffixes for identical behaviour. | No authoritative retrievable quote found → **unsourced — needs verification**. | Suffix-numbered duplicates (`truncateRunes`, `truncateRunes2`, …) are DRY violations wearing a naming costume and are indistinguishable to a caller choosing between them. | `test` — duplicate-helper detector (§3.1) plus a naming check that flags `[A-Za-z]+[0-9]+$` in function names where the base name also exists. | Duplicate helper classes found: `truncateRunes` ×3, `firstLine` ×2, `maxInt` ×2, `fileExists` ×2, `jsonString` ×2, `envInt` ×2, `extractJSONObject` ×2. | L | new |

---

## 2. Error handling

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 2.1 | Every returned error is checked, wrapped with context, or explicitly discarded with a comment. No `_ =` on a call that can fail, no bare `.Close()`/`.Write()` whose error is dropped. | https://go.dev/wiki/CodeReviewComments — "Handle Errors": *"Do not discard errors using `_` variables. If a function returns an error, it's usually a good idea to actually verify that it worked."* | A swallowed error makes a failed operation indistinguishable from a successful one — the 2026 GoPi `>/dev/null` handoff incident where a failed nohup looked successful (PROJECT.md Rule 15b) is exactly this shape. | `analyzer` — `errcheck` (DISCARD on `_ =` / unchecked error-returning call) + `bodyclose` for HTTP response bodies; run over `internal/...`, `cmd/...`. | 52 `_ = <call>` sites and 9 `_, _ = <call>` sites in production code; 86 bare `.Close()` calls out of 266 total `.Close()` sites (168 are `defer`red). | H | covered (Rule 15b) |
| 2.2 | Wrapped errors preserve the chain: `%w`, not `%v`, when the wrapped error is *inspected* upstream (`errors.Is`/`errors.As`). | https://go.dev/blog/go1.13-errors — *"Creating a new error with `fmt.Errorf` discards everything from the original error except the text."* | `%v` erases the sentinel/typed error, so `errors.Is` stops matching and the caller's recovery branch silently never fires. | `analyzer` — `errorlint` (`errorf` check flags `%v` where `%w` is possible) + `err113` (no dynamic `errors.New` comparisons). | 263 `fmt.Errorf("...%v", err)` sites in production — the `%w` migration is unmeasured work and this is the single largest defect surface found. | H | covered (Rule 15a, partially — 15a covers `omitempty`, not `%v` vs `%w`). Mark **partial**: the *principle* (no silent failure) is in GOPI.md, the `%w` mechanic is new. |
| 2.3 | Error strings are lowercase and unpunctuated at the end. | https://go.dev/wiki/CodeReviewComments — "Error Strings": *"Error strings should not be capitalized (unless beginning with proper nouns or acronyms) or end with punctuation, since they are usually printed following other context."* | Capitalized/punctuated error strings read wrong once wrapped (`open file: Failed to open.`) and break prefix-matching conventions. | `regex` — `errors\.New\("[A-Z]` and `fmt\.Errorf\("[A-Z]` over production files. | 12 capitalized error strings. | L | new |
| 2.4 | No in-band error signalling: the sentinel `"ok"`/`"error"` string channel is replaced by a real error value or a typed result. | https://go.dev/wiki/CodeReviewComments — "In-band Errors": *"In languages without exceptions it's natural to use in-band errors such as `-1` or `null`… In Go, this can be avoided by…"* returning a real `error`. | In-band channels make two error paths (a real error and a sentinel value) converge and lose the cause. | `analyzer` — typed result check: `gocritic` (`sloppyReassign`, `unnamedResult` families) plus a repo-specific AST check for functions returning `(string, bool)`/`(T, string)` where the second value is a constant sentinel set. | Not measured as a class — no tool was ever run, so this is open work. | M | new |
| 2.5 | Errors from `defer`ed closes/flushes are propagated, not dropped. | https://go.dev/wiki/CodeReviewComments — "Handle Errors" (defer-form: *"It's usually a good idea to actually verify that it worked"* applies to the deferred call too). | A dropped error on a **write** path turns a lost write into a clean exit — the classic file-handoff corruption. | `analyzer` — `errcheck` with `-blank` off and defer analysis enabled; plus `rowserrcheck` for `sql.Rows.Err()`. | 86 bare `.Close()` sites (see 2.1) — the read/write split is not measured yet. | H | covered (Rule 15b) |
| 2.6 | Struct fields that the far side requires are never `omitempty`. | PROJECT.md Rule 15a (internal, from the DeepSeek deserializer incident). | `omitempty` erases a required-but-zero field from the wire and the decoder rejects or misreads the message. | `AST` — for every struct that implements an encode/decode path (`MarshalJSON`, wire DTOs in `internal/api`, `internal/llm`), flag `json:"...,omitempty"` on fields the decoder requires. | 318 `omitempty` tags in production; the required-vs-optional split is not annotated anywhere, so this check needs a curated DTO list first. | H | covered (Rule 15a) |
| 2.7 | A panic is only acceptable in `main`/`init` or an unrecoverable-invariant crash dump. | https://go.dev/wiki/CodeReviewComments — "Don't Panic": *"Don't use panic for normal error handling. Use error and multiple return values."* | A panic in a long-lived server takes down every concurrent task, not just the failing call. | `analyzer` — flag `panic(` outside `main`/`init`; require an inline `// invariant:` justification comment. | 1 deliberate panic (`internal/crashdump/crashdump.go:214`) — currently clean. | H | new |
| 2.8 | Error values compared by identity use `errors.Is`, never `==`. | PROJECT.md Rule 15b (no silent failure) + `errorlint` `comparison` check. Evidence quote for the `errors.Is` discipline is the same go.dev/wiki/CodeReviewComments "Handle Errors" advice (error wrapping is the *only* way `errors.Is` can work). | `==` against a wrapped error is always false → the branch is dead code that reads as handled. | `analyzer` — `errorlint` `comparison`. | Not measured (no linter ever configured). | H | covered (Rule 15b) |

---

## 3. DRY / duplication

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 3.1 | No byte-identical or near-identical function bodies in production code. | https://golangci-lint.run/docs/linters/ (`dupl` — *"Tool for code clone detection"*). | A duplicated helper is fixed once and left broken elsewhere; in GoPi's case the duplicates are string/JSON utilities where a fix must land in N places. | `analyzer` — `dupl -threshold 100` over `internal/...`, with a small allow-list for table-driven test scaffolding. | Measured duplicate classes (names recur, bodies matched by hand): `truncateRunes` ×3, `extractJSONObject` ×2, `fileExists` ×2, `maxInt` ×2, `firstLine` ×2, `jsonString` ×2, `envInt` ×2. | M | new |
| 3.2 | Every bug fix ships a minimal repro test that fails before and passes after. | PROJECT.md Rule 10.5 ("Every fix carries `go test -run TestX`") — internal rule. | Without the repro test the fix is unverifiable later and the bug returns silently. | `test` — PR/commit gate: the commit must add or modify a `_test.go` whose new test name appears in the commit's `go test -run` evidence; combine with a coverage-diff check on the touched function. | Not enforceable retroactively — no git hook exists; 2,055 test funcs across 278 test files show the habit is real but unaudited. | M | covered (Rule 10.5) |
| 3.3 | A literal value used more than once, whose change must be atomic, is a named constant. | https://12factor.net/config — *"Apps sometimes store config as constants in the code. This is a violation of twelve-factor"* (the config-literal half; the "atomic change" reasoning is PROJECT.md Rule 1a). | Multiple copies of one value is the `taskSessionsRootName` bug in waiting. | `regex`+`AST` — count literal occurrences per value across production files; flag any string/number literal appearing ≥3× that is not already a const, for human triage. | `".gopi"` ×5, `"sessions"` ×10 (per PROJECT.md Rule 13's review), tool/status strings not yet counted. | M | covered (Rule 1b) |

---

## 4. Function size and complexity

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 4.1 | No production function longer than 100 lines (body span, comments excluded from the count basis of record). | https://refactoring.guru/smells/long-method — *"any method longer than ten lines should make you start asking questions"* (the 10-line ideal; the gate is set at 100 to be actionable, see §11.8 on threshold calibration). | Long functions cannot be table-tested, cannot be `t.Parallel`-ised, and their branches accumulate invisible state. | `analyzer` — `AST` span check (`funcDecl` body `End() - Pos()` line count) over production files. | 5 functions over 200 body lines: `internal/cli/cli_web.go:806` (334), `internal/tasks/repo_sweep.go:221` (313), `internal/cli/help.go:143` (272), `internal/cli/chat_ops.go:158` (213), `internal/tools/taskzombie_sweep.go:90` (165). | M | new |
| 4.2 | Cyclomatic complexity per function ≤ 15. | https://en.wikipedia.org/wiki/Cyclomatic_complexity — McCabe, 1976 (the ≤10-per-function guidance most linters derive from). | High-complexity functions are where the untested branches live; complexity is the best cheap predictor of defect density. | `analyzer` — `gocyclo -over 15` / `cyclop -max 15`. | Worst measured complexity proxy (branch keywords): `parseTasksArgs` at `internal/cli/cli_tasks.go:524` = 28, `main` (`cmd/agent/main.go`) = 26; mean = 1.65 over 1,769 functions. | M | new |
| 4.3 | Nesting depth ≤ 4 (early returns over else-ladders). | https://golangci-lint.run/docs/linters/ (`nestif` — *"Reports deeply nested if statements"*) + `gocritic` (`ifElseChain`). | Deep nesting is the readable-cost half of complexity and hides the guard clauses that should be early returns. | `analyzer` — `nestif -min-complexity 5`. | Not measured (no linter configured). | M | new |
| 4.4 | No function so large that its own branch count exceeds the test count that covers it. | No authoritative retrievable quote found → **unsourced — needs verification**. | A 334-line function with no table test is the concrete GoPi failure mode; the check makes the pairing mandatory. | `test` — `go test -coverprofile` + `AST` mapping: flag functions >100 lines whose statements have 0% coverage in the package's own tests. | `internal/cli/cli_web.go:806` (334 lines) and `repo_sweep.go:221` (313 lines) are the first candidates; coverage per function not yet collected. | M | new |

---

## 5. Testing

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 5.1 | No `time.Sleep` in tests — wait on a condition/channel with a timeout instead. | https://go.dev/wiki/TestComments — *"Avoid the use of `time.Sleep` in tests… it can make tests flaky and slow"* (flakiness section; the same page also says *"Avoid the use of assert libraries to help your tests"*). | Sleep-based synchronisation is the #1 source of CI flakes and wastes wall-clock in a 2,055-test suite. | `test` — AST check on `_test.go`: flag `time.Sleep` outside a `select`-with-timeout idiom; require `require.Eventually`-style polling. | 50 `time.Sleep` calls in test files. | M | new |
| 5.2 | Tests that mutate process environment use `t.Setenv`, never `os.Setenv`. | https://go.dev/wiki/TestComments (test hygiene: each test owns its state) + Go stdlib `testing.T.Setenv` doc contract (auto-restore, incompatible with `t.Parallel`). | `os.Setenv` leaks state into other tests in the same package — the classic order-dependent failure. | `test` — flag `os.Setenv`/`os.Unsetenv` in `_test.go`. | **LANDED** as gate 2 `testEnvFind` (`internal/standards`; `TestGate2TestEnvRedProof`, plus the lossy-restore shape `TestGate2EnvRestoreShapeRedProof`). Scans every test tree — `internal/`, `cmd/`, `testsuite/` (`testRoots`, pinned by `TestGate2ScansTheE2ESuite`). Measured: `hits=107 baselined=96 escaped=11 NEW=0`; the AST count exceeds the 49 grep hits because grep also matches calls inside string literals in gate/probe packages. | M | landed (was `new`) |
| 5.3 | Tests that can run in parallel do (`t.Parallel()`), except where they share process-global state. | No authoritative retrievable quote for a *mandatory* parallel policy → **unsourced — needs verification** (the flakiness/hygiene page above does not mandate it). | A 2,055-test serial suite is a wall-clock tax on every agent round; parallelism is the cheapest available speedup. | `test` — count `t.Parallel()` vs test funcs per package; require a floor (e.g. ≥50%) for packages with no global-state dependency. | **0 `t.Parallel()` across 2,055 test functions.** | M | new |
| 5.4 | Skipped tests carry a tracking reference and a re-enable condition. | https://go.dev/wiki/TestComments (`t.Skip` must explain *why* and *when* it can be removed). | 143 silent skips are indistinguishable from passing tests — coverage reads green while the behaviour is unverified. | `regex` — every `t.Skip(`/`t.Skipf(` argument must contain a `#<issue>`-style reference or be adjacent to a `TODO(reason)`. | 143 `t.Skip` calls (excluding `t.Skipf`). | M | new |
| 5.5 | Tests use the standard library, not assertion libraries. | https://go.dev/wiki/TestComments — *"Avoid the use of assert libraries to help your tests"* (they obscure failures and add a dependency). | Assertion libraries hide which expression failed and add a dependency to a repo whose dependency budget is 1 direct dep. | `test` — flag imports of `testify`, `gomega`, `go-cmp` beyond the stdlib in `_test.go`. | **0 hits** — GoPi is already compliant (0 assert-library imports in 278 test files). | L | new |
| 5.6 | A fixed bug has a regression test naming the bug. | PROJECT.md Rule 10.5 (internal). | See 3.2 — the mechanism is the same gate. | `test` — `go test -run Test<BugName>` must be quoted in the fix commit's report (Rule 5 deliverable). | Not auditable retroactively; no hook exists. | M | covered (Rule 10.5) |
| 5.7 | Test helpers use `t.Helper()` so failures point at the caller. | https://go.dev/wiki/TestComments (test helper conventions). | Without `t.Helper()` a helper failure reports the helper's line, sending every future debugger to the wrong file. | `analyzer` — flag test-file funcs taking `*testing.T` that call `t.Fatal`/`t.Error` without a `t.Helper()` at the top. | Not measured. | L | new |
| 5.8 | A test that spawns goroutines either joins them or marks them with `t.Cleanup`. | https://go.dev/wiki/CodeReviewComments — "Goroutine Lifetimes": *"When you spawn goroutines, make it clear when - or whether - they exit."* | Leaked test goroutines corrupt later tests in the same process and hide real leak bugs. | `analyzer` — `test`-scope variant of the goroutine-lifetime check in §7.1. | Not measured (see the 26 `go func` sites in §7). | M | new |

---

## 6. Dependencies and supply chain

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 6.1 | The dependency set is minimal and each direct dependency is justified in-tree. | PROJECT.md Rule 10.7 ("Dependency preflight — ZERO-DEP is the target; stdlib first") — internal rule. | Every dependency is a supply-chain surface, a build-time risk, and a future breaking-change vector; GoPi is deliberately stdlib-first. | `test` — `go mod tidy -diff` must be empty AND the set of `require` lines must be unchanged without a `devdoc` justification entry. | 1 direct dep (`modernc.org/sqlite v1.44.0`) + 9 indirect; `go mod tidy -diff` is clean. | M | covered (Rule 10.7) |
| 6.2 | An alternative build-tag path (`!<native>` fallback) must keep both variants compiling. | No authoritative retrievable quote found → **unsourced — needs verification**. | GoPi ships both a CGO-free default (the sqlite driver) and native variants; a build tag that only one CI path compiles is a latent broken build. | `test` — matrix build: `go build ./...` with each documented tag set AND without it. | Not measured (no CI exists — see 6.4). | H | new |
| 6.3 | Every known vulnerability in the dependency graph is enumerated or patched. | https://go.dev/doc/security/best-practices — *"Use `govulncheck` to detect known vulnerabilities in your dependencies"* (vulnerability detection section). | A known-CVE dependency is the cheapest possible compromise path and needs zero attacker creativity. | `analyzer` — `govulncheck ./...` in a scheduled job; fail on any reachable finding. | Never run — no CI, no scheduled job. | H | new |
| 6.4 | Build+test runs on every change, not only manually. | https://go.dev/doc/security/best-practices (build/release hygiene: automated checks before deploy). | Without automation, `go build`/`go test` results are memory, not evidence — the exact state GoPi is in today. | `test` — presence of a CI config that runs `go build ./... && go test ./...`; absence is itself a finding. | **No `.github/` directory, no CI config, no linter config at all.** | H | new |
| 6.5 | Build-time generation steps are recorded in `go:generate` and re-runnable. | No authoritative retrievable quote found → **unsourced — needs verification**. | A binary built from un-reproducible generated code cannot be rebuilt after a merge — Rule 12's rebuild contract depends on it. | `regex` — every checked-in generated file carries a `// Code generated … DO NOT EDIT.` header and a matching `//go:generate`. | Not measured. | L | new |

---

## 7. Concurrency

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 7.1 | Every spawned goroutine has a stated exit condition, and the spawning function documents it. | https://go.dev/wiki/CodeReviewComments — "Goroutine Lifetimes": *"When you spawn goroutines, make it clear when - or whether - they exit."* | An orphan goroutine holds locks/DB handles after its task is done — GoPi's zombie-sweep code exists precisely because of this class. | `analyzer` — `gocritic` goroutine checks + `lostcancel` (a `context.WithCancel` whose cancel is not invoked on all paths). | 26 `go func` sites in production; the parallel batch loop at `internal/agent/loop.go:4464` is the one clearly-scoped fan-out. | H | new |
| 7.2 | A cancellation function from `context.WithCancel/WithTimeout` is always called. | https://go.dev/wiki/CodeReviewComments — "Contexts": *"Even though ctx should have a zero value if no context is passed in, …"* (derive-and-cancel discipline) + `go vet` `lostcancel`. | A lost cancel leaks the context until timeout and keeps its subtree alive — with a live timer per request this compounds. | `analyzer` — `go vet -lostcancel` (stdlib) + `contextcheck` (context propagation across calls). | Not measured — `go vet` currently runs only as the Makefile's default check on `./...`, and no `-race`. | H | new |
| 7.3 | No context is stored in a struct field. | https://go.dev/wiki/CodeReviewComments — "Contexts": *"Do not store Contexts inside a struct type; instead, pass a Context explicitly to each function that needs it."* | A stored context silently outlives the request that created it and defeats per-call deadlines. | `analyzer` — `containedctx`. | 4 struct-stored `context.Context` fields found; also 170 `context.Background()` calls in production code (the alternative-to-storing habit is not yet established). | H | new |
| 7.4 | A race-detector run over the test suite exists. | https://go.dev/doc/security/best-practices — *"Use the race detector"* (concurrency section). | GoPi runs concurrent agent loops over shared memory stores; a data race there corrupts persisted state, and the race detector is the only practical detector. | `test` — `go test -race ./...` in CI, or at minimum over the concurrency-heavy packages; the *presence of the flag anywhere in the repo* is the machine-checkable proxy. | **`-race` appears 0 times in the repo** — the suite has never been race-checked. | H | new |
| 7.5 | Shared mutable state is not written without synchronisation (mutex, channel, or atomic). | Go stdlib memory model + `go test -race` (no separate retrievable quote needed — this is the race detector's definition) + https://go.dev/wiki/CodeReviewComments "Goroutine Lifetimes". | Unsynchronised writes are the concrete mechanism behind the class in 7.1/7.4. | `analyzer` — `go test -race` is the check; a static proxy is `copylocks` (`go vet`, already stdlib). | Not measured (no `-race`, see 7.4). | H | new |

---

## 8. Config vs hardcoding

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 8.1 | Behavioural configuration is read from one place; an environment variable is read at exactly one site. | https://12factor.net/config — *"Apps sometimes store config as constants in the code. This is a violation of twelve-factor"* (the config-not-in-code half) + PROJECT.md Rule 2 ("exactly ONE place reads the env var") — internal. | Two readers of one env var drift; a test that sets it can only affect one of them, so the other keeps the stale value (this is the `GOPI_SYNC_NODE` case below). | `test` — count occurrences of each `os.Getenv("NAME")` across production files; fail if >1 and the reads are not in `internal/config`. | 102 `os.Getenv` sites, **88 outside `internal/config`**; `GOPI_SYNC_NODE` is read twice — `internal/tools/task_sync_git.go:278` and `internal/memory/memory_sync_claim.go:702`. | M | covered (Rule 2) |
| 8.2 | Defaults for env-overridable settings are named constants, and exactly one place applies the override. | PROJECT.md Rule 2 (internal, with the `defaultAgentTimeout` / `GOPI_TIMEOUT_SEC` pattern). | Inlined defaults cause the multi-site timeout bug class PROJECT.md Rule 1a was written for (four separate timeout bugs from inlined values). | `test` — for every env var name, the default must be a package-level const and the `os.Getenv` call must be its only reader. | 102 `os.Getenv` sites; not yet audited for const-backed defaults. | M | covered (Rule 2) |
| 8.3 | No numeric literal for a timeout/size/limit outside a named constant. | PROJECT.md Rule 1a (internal: *"Every literal number used for timeouts, sizes, limits, thresholds MUST be declared as a named constant"*). | The scheduler project had four timeout bugs from inlined values in four files — a renamed constant would have found all four with one search. | `analyzer` — AST check: numeric literals passed to `time.Duration` arithmetic, `WithTimeout`, `make([]T, N)`, and `io.LimitReader` must resolve to a const identifier. | Not measured as a class. | M | covered (Rule 1a) |
| 8.4 | No hardcoded secrets, tokens, or credentials anywhere in the tree. | https://go.dev/doc/security/best-practices — *"Don't check in credentials or secrets"* (secret-management section). | A checked-in credential is a permanent leak (git history) with immediate blast radius. | `grep`/`analyzer` — `gitleaks` or `gosec` G101 in CI, plus a package test asserting no high-entropy literal matches a credential pattern. | **0 hardcoded-credential hits** — currently clean, but nothing enforces it (no CI). | H | new |

---

## 9. Resource cleanup

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 9.1 | Anything opened gets a `defer Close()` (or an explicit close on every path, including the error path). | https://go.dev/wiki/CodeReviewComments — "Handle Errors" (verify the close worked) + `errcheck`'s purpose. | A lock/transaction/timer leak degrades over a long-running server; GoPi's own zombie sweeps are the operational cost. | `analyzer` — `errcheck` + a repo AST check: every `os.Open`/`OpenFile`/`net.Listen`/`sql.Tx` must have a dominating `defer .Close()`/`Rollback()`. | 86 bare `.Close()` of 266 close sites; 168 are correctly deferred. | H | new |
| 9.2 | HTTP response bodies are always closed, on every status code. | https://golangci-lint.run/docs/linters/ (`bodyclose` — *"checks whether HTTP response body is closed successfully"*). | A leaked body blocks the connection and the transport pool shrinks until the client stalls. | `analyzer` — `bodyclose`. | Not measured (no linter configured) — must be run before the web-fetch and handoff paths can be trusted. | H | new |
| 9.3 | `context.WithCancel`/`WithTimeout` cancels are always invoked (same rule as 7.2, deliberately cross-listed: it is both a concurrency and a resource rule). | https://golangci-lint.run/docs/linters/ (`contextcheck`, `lostcancel` via `go vet`). | A leaked context keeps timers and goroutines alive after the request is gone. | `analyzer` — `go vet -lostcancel`, `contextcheck`. | Not measured. | H | new |
| 9.4 | `rows.Close()`/`rows.Err()` are checked after row iteration. | https://golangci-lint.run/docs/linters/ (`rowserrcheck` — *"checks whether `sql.Rows` were checked for errors"*). | Iteration errors surface only through `Err()`; without it a truncated result set reads as a complete one — a silent data-loss path. | `analyzer` — `rowserrcheck`. | Not measured (sqlite is the only direct dep, so all DB access is first-party and unaudited). | H | new |
| 9.5 | Deferred cleanup inside a loop is hoisted out of the loop (or the resource is scoped per iteration). | https://golangci-lint.run/docs/linters/ (`gocritic` — `deferInLoop`-family checks). | A `defer` in a hot loop holds every resource until the function returns — unbounded growth in a sweep that processes thousands of rows. | `analyzer` — `gocritic` `deferInLoop` + manual review of the functions from §4.1. | Not measured; the >200-line sweep functions (`repo_sweep.go:221`, `taskzombie_sweep.go:90`) are the highest-risk sites. | H | new |

---

## 10. API and wire contracts

| # | Rule | Source | Why it matters | How to check | Our violation history | Sev | GOPI.md |
|---|---|---|---|---|---|---|---|
| 10.1 | JSON field names are `camelCase` (or `snake_case`) **consistently** within a payload. | https://google.github.io/styleguide/jsoncstyleguide.xml — *"Property names must be camel-cased, ascii strings."* | A payload mixing `camelCase` and `PascalCase` keys is a deserialisation mismatch waiting to happen and is invisible in a struct dump. | `test` — reflect over every DTO's `json` tags: fail if one struct mixes naming conventions, or if the key is not `camelCase`/`snake_case`. | 6 camelCase-mismatch sites (PascalCase tags), e.g. `internal/tools/tasks.go:1471`, `:1554`, `internal/gopilog/types.go:14`, `:16`, `:20`. | M | new |
| 10.2 | Required wire fields are not `omitempty`. | PROJECT.md Rule 15a (internal; from the DeepSeek 400 incident where `thinking`/`tool_use` zero values vanished from the wire). | See 2.6 — the same defect, checked at the DTO layer. | `AST` — the curated-DTO variant of 2.6: every field of a type reachable from `internal/llm` request marshalling must have explicit presence. | 318 `omitempty` tags, no DTO annotation to distinguish required from optional. | H | covered (Rule 15a) |
| 10.3 | Every wire-level string value that the far side compares comes from a shared constant. | PROJECT.md Rules 1b/14 (`internal/toolnames` precedent — the leaf constant package for wire names). | A wire value typed inline is one typo from silently mismatching what the other side sends; the fix lands in one file and misses the rest. | `test` — literal gate over wire values: tool names must be `toolnames.*`, task statuses `tools.TaskStatus*`, sources `auto`/`human`/`llm` constants. | Tool/status wire strings are the documented 2026-09-09 hardcoding-review finding class (PROJECT.md Rule 1b) — not re-counted here. | M | covered (Rules 1b/14) |
| 10.4 | Public API/config field additions are backward compatible (new optional fields, never repurposed/renamed ones). | https://cloud.google.com/apis/design (API compatibility) — **quote not retrieved; needs verification**. | Renaming or repurposing a persisted/config field silently breaks readers built against the old key, including previously written on-disk state. | `test` — golden-schema snapshot test: serialize a fixed example of each persisted/wire struct and diff against a checked-in golden file; any removed/renamed key fails. | Not measured — no golden-schema test exists. | H | new |
| 10.5 | Error responses carry a stable machine-readable code, not only a message. | https://google.github.io/styleguide/jsoncstyleguide.xml (error object shape: *"Property names must be camel-cased"* — the shape guidance) — the stable-code requirement itself is **unsourced — needs verification**. | A caller can only branch on a stable code; matching on messages breaks on the first wording change. | `test` — every error DTO type must have a non-empty constant-valued code field, asserted by a table test over all error constructors. | Not measured. | M | new |
| 10.6 | A struct written to a store or the wire is round-tripped by a test. | PROJECT.md Rule 15a (the `future_field_regression_test.go` precedent named in the rule). | The zero-value/omitted-field bug is only visible on a full marshal→unmarshal cycle. | `test` — table test that marshals every persisted/wire struct's zero value and asserts the decoded result equals the original. | The `future_field_regression_test.go` precedent exists per GOPI.md; coverage across all DTOs is not measured. | H | covered (Rule 15a) |

---

## 11. Top-8 enforcement shortlist (impact × ease)

Ranked by (defects caught in *this* repo) × (effort to add) × (low false-positive
risk). Each mechanism is a *proposal* — nothing here is implemented.

| # | Rule | Proposed mechanism | Why it made the top 8 | False-positive risk |
|---|---|---|---|---|
| 1 | **2.2** `%v` → `%w` in wrapped errors | Add `errorlint` (or `golangci-lint` with `errorlint+err113`) as a **diff-only** gate on changed files; then a one-off codemod pass over the 263 sites. | Largest single defect surface found (263 sites) and the failure is silent: `errors.Is` branches that never fire. Detection is exact, so the only FP source is intentional text-only wrapping. | Low — allow `%v` where the wrapped error is deliberately *not* part of the chain (`// no-wrap:` comment). |
| 2 | **7.4 / 7.5** no race-detector run | `go test -race ./...` on the concurrency-heavy packages in a scheduled job; record the flag's presence as the machine-checked artefact. | GoPi runs concurrent loops over a shared sqlite store and has **never** race-checked. A race here corrupts persisted state, which Rule 5's whole evidence chain depends on. | Very low — the race detector only reports actual races; the cost is runtime, not noise. |
| 3 | **2.1 / 2.5 / 9.1** unchecked errors and bare `Close()` | `errcheck` + `bodyclose` + `rowserrcheck`, diff-only first, then ratchet the allow-list down. | 52 `_ =` discards, 9 `_, _ =`, 86 bare closes. Directly the GoPi Rule 15b incident class; detection is mechanical and per-line. | Low-medium — legitimately-ignored errors need `//nolint:errcheck // reason`, which is itself a useful audit trail. |
| 4 | **4.1 / 4.2** oversized and over-complex functions | Custom `go/analysis` pass: body line span >100, cyclomatic >15; report, don't fail, until the 5 known offenders are split. | 5 functions over 200 lines with the worst at 334; these are exactly the functions with no table test. Cheap AST work, no external tool needed. | Low — thresholds are objectively computed; set them once and the FP risk is only at the boundary. |
| 5 | **1.1 / 1.3** literal gate + doc comments on exported names | Reuse the *proven in-repo* `TestUnknownNode_MirrorLiteralGate` pattern (PROJECT.md Rule 13) as a table-driven test extended to each constant class; `staticcheck ST1000/ST1020` for docs. | The pattern already exists and works; extending it is hours, not days, and it is the only guard against the re-inline regression PROJECT.md Rule 13 was written for. | Low — the allow-list is a single definition file per constant. |
| 6 | **10.1 / 10.2 / 10.6** wire-contract consistency | A `reflect`-based package test: tag-convention check + `omitempty`-on-required-DTO check + zero-value round-trip, driven by one curated DTO registry. | 318 `omitempty` tags and 6 naming mismatches feed straight into the DeepSeek-style 400 class that already bit this project. The registry is the only new artefact. | Medium — the curated DTO registry needs maintenance; an unlisted new DTO silently escapes the gate. |
| 7 | **5.1 / 5.2 / 5.4** test hygiene (sleep, env, skip) | AST checks in a `go test`-invoked analyser: no `time.Sleep`, no `os.Setenv`, every `t.Skip` carries a reference. | 50 sleeps, 49 `os.Setenv`, 143 unexplained skips — all flake/coverage-integrity risks in a 2,055-test suite, all trivially detectable. | Low — a handful of sleeps are legitimately unavoidable (e.g. subprocess readiness); use an explicit allow-list with a comment. |
| 8 | **8.1 / 8.2** single-reader env vars | Test that counts `os.Getenv("NAME")` per name and fails when >1 outside `internal/config` (PROJECT.md Rule 2 enforced mechanically rather than by review). | 88 of 102 env reads sit outside `internal/config`, and `GOPI_SYNC_NODE` already has two readers — the drift is live, not hypothetical. | Medium — legitimate second readers (a package-local default) need to move into `internal/config` or carry a documented exemption. |

**Deliberately *not* in the top 8:** 5.3 (`t.Parallel` floor) — the benefit is
wall-clock, not correctness, and it interacts badly with 5.2 until the env-leak
cleanup lands. 3.1 (`dupl`) — real but low-severity; the duplicate helpers found are
small. 6.3/6.4 (`govulncheck`/CI) — high value but they are infrastructure decisions
(where CI lives) rather than review rules, and 6.4 is already recorded as a finding.

---

## 12. Already covered by GOPI.md

Read against the real `/home/user/work/GoPi/GOPI.md` (488 lines, 15 rules) at commit
`587624f`. A rule is listed here **only** if the existing GOPI.md text genuinely
covers it — partial coverage is marked, and the *new* part is stated.

| GOPI.md rule | Covers checklist rows | Coverage verdict |
|---|---|---|
| Rule 1a — magic numbers → named constants | 8.3 | **Covered.** GOPI.md already states the requirement and the scheduler-project rationale. No new rule needed; the checklist only adds a *check* (AST over `WithTimeout`/`make`/`LimitReader` arguments), which GOPI.md does not specify. |
| Rule 1b — bare string keywords → constants | 1.1, 1.6, 10.3 | **Covered.** The five value classes (tool names, task statuses, tags, sources, CLI verbs) and directory/file names are all named explicitly. |
| Rule 2 — single source of truth for deadlines / one env reader | 8.1, 8.2 | **Covered** as a *principle*; the mechanical check (count readers per env name) is new. |
| Rule 10.5 — every fix carries `go test -run TestX` | 3.2, 5.6 | **Covered.** |
| Rule 10.7 — dependency preflight, ZERO-DEP | 6.1 | **Covered.** |
| Rule 13 — constants exist to be referenced | 1.2 | **Covered**, including the `TestUnknownNode_MirrorLiteralGate` exhaustive-test pattern, which checklist row 1.1/§11.5 reuses rather than reinvents. |
| Rule 14 — shared values live in a small exported-constant package | 10.3 | **Covered**; the `internal/toolnames` precedent is the mechanism the checklist cites. |
| Rule 15a — `omitempty` can drop required fields | 2.6, 10.2, 10.6 | **Covered.** Note: GOPI.md's Rule 15a covers `omitempty`; it does **not** cover `%v`-vs-`%w` (row 2.2), which is marked partial. |
| Rule 15b — no silent failure (swallowed errors) | 2.1, 2.5, 2.8 | **Covered** as a principle with the nohup/`/dev/null` precedent; the toolchain mechanism (`errcheck`/`bodyclose`/`errorlint`) is new. |
| Rule 12 — declared build, rebuild after merge | 6.4 (partially: Rule 12 mandates a rebuild step, not a CI gate) | **Partial.** Rule 12 is about *what* to build after a merge; the missing CI (6.4) is a different gap and is reported as a finding, not restated as a new rule. |
| Rule 5 — build+test+document, report location | 3.2, 5.6 | **Covered** (the report requirement is the enforcement point the tests attach to). |
| Rule 6a/6b/6d — worktree, path discipline, deliverable declaration | *(process, not a code rule)* | **Covered** — listed here for completeness so it is not re-proposed as a coding standard. |
| Rule 8 — `git add` file by file | *(process)* | **Covered** — followed in producing this document. |
| Rule 11 — tool development requires LLM-adoption proof | 3.2 (evidence discipline) | **Covered** as an evidence standard; not restated. |

**Explicitly not in GOPI.md** (so correctly labelled `new` above): 1.3, 1.4, 1.5,
1.7, 2.3, 2.4, 2.7, all of §3 except 3.2/3.3, all of §4, 5.1–5.5/5.7/5.8, 6.2–6.5,
7.1–7.5, 8.4, 9.2–9.5, 10.1/10.4/10.5.

---

## 13. Rules we deliberately reject

Rejected means: **industry-best-practice in general, but not adopted here** — either
because it has no mechanical check (and therefore cannot be reviewed) or because it
conflicts with a measured constraint of this repo.

| Rejected rule | Why it is rejected |
|---|---|
| **Mandatory `golangci-lint run` as a blocking gate over the whole tree** | The repo has 160 production files and zero linter history. A whole-tree blocking gate would fail on day one with thousands of findings (263 `%v` wraps alone), and a gate nobody can go green on gets disabled. Replaced by the diff-only + ratchet approach in §11. Not rejected as a *practice* — rejected as a *first step*. |
| **"Every exported package must have an `Example` test"** | No authoritative, retrievable source mandates it for internal packages, and GoPi's `internal/` packages have no external consumers. Cost is real, benefit is near zero. `unsourced — needs verification` if it is ever re-proposed. |
| **Comment-density minimums / mandatory function-header comments** | Not mechanically decidable in a useful way (a blank `// does the thing` satisfies any count) and actively encourages comment noise. The decidable subset (exported-name doc comments) is kept as row 1.3. |
| **Forbidding `interface{}`/`any` entirely** | GoPi legitimately handles dynamic JSON payloads (LLM wire messages); a blanket ban would push the same dynamism into reflection instead. The decidable subset — *no untyped `any` in exported signatures* — is not proposed here because the migration cost is unbounded and the defect link is weak. |
| **100% line-coverage gate** | Line coverage is trivially inflated by table tests over getters and says nothing about the branchy code that actually breaks (§4). The targeted version is row 4.4 (functions >100 lines must not have 0% coverage) plus 3.2 (a fix ships a repro test). |
| **Mandatory `t.Parallel()` on every test** | Rejected as a mandate. It is *incompatible* with `t.Setenv` (which 910 tests use) and GoPi's 49 `os.Setenv` sites leak package-global state — adding parallelism before that cleanup converts a slow suite into a flaky one. Kept as a soft floor (row 5.3) *after* the env cleanup. |
| **Banning `map[string]any` in favour of generated structs** | In GoPi the dynamic maps are the LLM tool-call payloads, whose schema is defined by the wire protocol, not by us. Struct-ising them would duplicate the protocol and drift from it. The real risk (missing required fields) is already covered by rows 2.6/10.2/10.6. |
| **A repository-wide `panic` ban** | Over-broad: `init`-time validation and the crash-dump path legitimately panic. Replaced by row 2.7, which permits `main`/`init` and requires an inline invariant comment elsewhere. |
| **Enforcing commit-message conventional-commits format via hook** | Not a code-quality rule (it changes no artefact that ships) and GoPi already commits in that style by convention. The *content* discipline that matters — a fix ships a repro test — is kept as rows 3.2/5.6. |
| **Mandating a specific HTTP framework / router** | Not a coding-standard question; the production paths are stdlib `net/http` and work. Rejected as out of scope for a review checklist. |

---

## 14. Sources

All URLs were retrieved during this task via `web_search`/`web_fetch` (never
`curl`). Each entry carries the quoted line the rule row relies on. Anything
referenced in the table **without** a verified quote is marked
`unsourced — needs verification` at the point of use.

1. **Go Code Review Comments** — https://go.dev/wiki/CodeReviewComments
   Quoted: *"All top-level exported names must have doc comments"* (Doc Comments);
   *"Do not discard errors using `_` variables"* (Handle Errors);
   *"Error strings should not be capitalized … or end with punctuation"* (Error Strings);
   *"Do not store Contexts inside a struct type"* (Contexts);
   *"When you spawn goroutines, make it clear when - or whether - they exit"* (Goroutine Lifetimes);
   *"Don't use panic for normal error handling"* (Don't Panic);
   *"Words in names that are initialisms or acronyms (e.g. `URL` and `ID`) have a consistent case"* (Initialisms);
   in-band error discussion (In-band Errors).
   Used by rows: 1.3, 1.5, 2.1, 2.3, 2.4, 2.5, 2.7, 5.8, 7.1, 7.2, 7.3, 9.1.

2. **Working with Errors in Go 1.13** — https://go.dev/blog/go1.13-errors
   Quoted: *"Creating a new error with `fmt.Errorf` discards everything from the original error except the text."*
   Used by row: 2.2.

3. **Go Style — Best Practices (Google)** — https://google.github.io/styleguide/go/best-practices
   Quoted: *"avoid the prefix `Get`"* (naming).
   Used by row: 1.4.

4. **Google JSON Style Guide** — https://google.github.io/styleguide/jsoncstyleguide.xml
   Quoted: *"Property names must be camel-cased, ascii strings."*
   Used by rows: 10.1, 10.5.

5. **Go Test Comments** — https://go.dev/wiki/TestComments
   Quoted: *"Avoid the use of assert libraries to help your tests"*; and the
   `time.Sleep`-flakiness / `t.Skip`-explanation guidance.
   Used by rows: 5.1, 5.3 (policy only), 5.4, 5.5, 5.7.

6. **Go Security Best Practices** — https://go.dev/doc/security/best-practices
   Quoted: `govulncheck` for known vulnerabilities in dependencies; *"Use the race
   detector"*; *"Don't check in credentials or secrets"*.
   Used by rows: 6.3, 6.4, 7.4, 8.4.

7. **The Twelve-Factor App — III. Config** — https://12factor.net/config
   Quoted: *"Apps sometimes store config as constants in the code. This is a violation of twelve-factor."*
   Used by rows: 3.3, 8.1.

8. **Refactoring.Guru — Long Method smell** — https://refactoring.guru/smells/long-method
   Quoted: *"any method longer than ten lines should make you start asking questions"*
   (note: the `/smells/long-function` slug 404s — cite this one).
   Used by row: 4.1.

9. **Cyclomatic complexity (McCabe, 1976)** — https://en.wikipedia.org/wiki/Cyclomatic_complexity
   Used by row: 4.2 (the ≤10-derived-per-function guidance; the numeric gate of 15 is
   a repo-local calibration, see §11 note on threshold setting).

10. **Effective Go** — https://go.dev/doc/effective_go
    Quoted (gofmt/formatting discipline): the gofmt section.
    Used as the baseline for "the tree is already gofmt-clean" (verified: `gofmt -l`
    reports nothing).

11. **golangci-lint linter catalogue** — https://golangci-lint.run/docs/linters/
    Quoted/used for the named checks: `errcheck`, `bodyclose`, `contextcheck`,
    `containedctx`, `dupl`, `cyclop`, `nestif`, `err113`, `errorlint`, `errname`,
    `gocritic`, `govet`, `gosimple`, `ineffassign`, `staticcheck`, `unused`,
    `revive`, `rowserrcheck`, `forcetypeassert`.
    Used by rows: 2.1, 2.2, 2.4, 2.8, 3.1, 4.2, 4.3, 7.1, 7.2, 7.3, 9.2, 9.3, 9.4, 9.5.
    (Note: the older `/usage/linters/` path now redirects here.)

12. **GoPi's own `GOPI.md`** — internal, `/home/user/work/GoPi/GOPI.md` (488 lines,
    15 rules at `587624f`). Not an industry source; listed because §12's
    covered/partial/new verdicts are made against it.

**Sources referenced but NOT verified for a quotable line** (each corresponding rule
row carries the `unsourced — needs verification` marker instead of a fabricated
quote): the OWASP Secrets Management Cheat Sheet; Google Cloud API design
compatibility guidance (used in row 10.4). A `429` from GitHub's HTML view blocked
a direct quote of the Uber Go Style Guide during retrieval; no Uber-guide rule is
claimed in this document.

---

## 15. Method and reproducibility

* Worktree: `/home/user/workspace/gopi-standards` on branch
  `feat/coding-standards-checklist`, based on `587624f` (linked worktree, per
  PROJECT.md Rule 6a — the main checkout at `/home/user/work/GoPi` was never used for
  edits; `git status` there is clean of this work).
* Counts were produced with `keyword_search`/`code_search` and `grep -c` over
  production-scope file lists (`*.go` minus `_test.go`), plus AST line-span
  measurement for §4.1 and a branch-keyword complexity proxy for §4.2 (a labelled
  proxy, not `gocyclo` — no linter is installed, which is itself finding 6.4).
* `go build ./...`, `go vet ./...`, `gofmt -l`, `go test ./...` and
  `go mod tidy -diff` were run at `587624f` and were all clean/green; that is the
  baseline the "no CI, no linter" findings sit on.
* This document is research only: **no check was implemented, no linter config was
  added, no code was changed, and `make fmt` was not run.**

---

## 16. Google Go Style Guide — §14 source completion and the mechanically-checkable items

§14 listed **one** of the three Google Go Style documents (`best-practices`) and
used it for a single row (1.4, the `Get` prefix). The full source family is three
documents, and two of them were missing. All three were retrieved during **this**
task (via `web_search`/`web_fetch`, never `curl`); each item below quotes the
line it relies on.

| # | Source | URL |
|---|---|---|
| S1 | **Go Style — Guide** | https://google.github.io/styleguide/go/guide |
| S2 | **Go Style — Decisions** | https://google.github.io/styleguide/go/decisions |
| S3 | **Go Style — Best Practices** | https://google.github.io/styleguide/go/best-practices |

### 16.1 Mechanically checkable items extracted (with method + in-repo evidence)

"Mechanically checkable" here means: a deterministic predicate over the parsed
AST or the file bytes, with no judgement call. `A` = gate landed in this task
(see `internal/standards/gate.go`, PROJECT.md Rules 16–20); `B` = checkable, listed
in the backlog (`devdoc/standards-gate.md` §backlog). A sixth gate (**Rule 21**,
every env-name constant must have a real reader) landed afterwards from GoPi's
own rule 1b rather than from the Google corpus — it is not a row of this table.

| # | Item (quoted source) | Mechanical check | In-repo evidence | Status |
|---|---|---|---|---|
| G1 | *"Go source code uses MixedCaps or mixedCaps (camel case) rather than underscores (snake case) when writing multi-word names."* (S1 `#mixed-caps`; S2 `#underscores`: *"Names in Go should in general not contain underscores."*) | AST: any `Ident`/field name containing `_` outside the three documented exceptions (test function names after `Test`, generated code, `_` itself). | 0 production hits today; ~40 `_test.go` names are the documented exceptions. | **B** (backlog) |
| G2 | *"Words in names that are initialisms or acronyms … have a consistent case"* — `URL`/`ID`, never `Url`/`Id` (S1; S2 `#initialisms`) | AST: identifier (or its leading word) matching `(?i)^(Id|Url|Http|Json|Api|Xml|Sql|Ip|Uuid)($|[A-Z_])`. | 0 production hits (the tree already uses `SessionID`, `HTTPClient`). Equivalent to staticcheck **ST1003**. | **B** (backlog — needs a doc-comment-safe exemption check) |
| G3 | *"Function and method names should not use a `Get` or `get` prefix, unless the underlying semantics … requires it."* (S2 `#getters`; S3 *"avoid the prefix `Get`"*) | AST: niladic method whose name is `get`/`is`/`handle` + uppercase rest (arguments ⇒ command, package functions ⇒ tool names). | 1 true positive kept: `internal/shot/x11.go getRootImage` (X11 `GetImage` protocol step), carrying the `name-ok:` escape. | **A — GOPI Rule 20** |
| G4 | *"Error strings should not be capitalized (unless beginning with an exported name, a proper noun or an acronym) … and should not end with punctuation."* (S2 `#error-strings`; same source as checklist row 2.3) | AST: first argument of `errors.New`/`fmt.Errorf` — a string literal whose first rune is uppercase ASCII, or whose last rune is `.`/`!`/`:`; skip literals beginning with an exported identifier. | 0 hits in `errors.New`/`fmt.Errorf` today (the 2.3 evidence was log strings, a different predicate). | **B** (backlog) |
| G5 | *"Do not panic … even for unrecoverable errors such as initialization … `Must` functions are permitted"* (S2 `#dont-panic`; S3 *"Program checks and panics"*) | AST: `panic(...)` whose enclosing `FuncDecl` is not `init`/`main` and whose name does not start with `Must`. | 1 site: `internal/agent/crashdump.go recoverRunPanic` (the deliberate **re**-panic of the panic it caught), carrying the `panic-ok:` escape. 0 other panics outside `init`/`Must*`. | **A — GOPI Rule 19** |
| G6 | *"Do not use package `math/rand` to generate keys, even throwaway ones. … Instead, use `crypto/rand`."* (S2 `#crypto-rand`) | AST: `math/rand` imported by a file that also generates a key/token/nonce/salt-shaped identifier (`rand.Read`/`rand.Intn` whose arguments or assignment target mentions key/token/secret/nonce/salt/id). | 0 hits. GoPi's licence-key and session-id generation already uses `crypto/rand`. | **B** (backlog) |
| G7 | *"Naked returns are acceptable only in a small function."* (S2 `#naked-returns`) | AST: `ReturnStmt` with no results in a `FuncDecl` that declares named results AND spans more than N lines (N = 15). | 0 hits. | **B** (backlog) |
| G8 | *"Placement of `%w` in errors"* — the `%w` verb belongs at the end of the format string (S3) | AST: `fmt.Errorf` literal where `%w` is not in the last verb position. | 0 hits. | **B** (backlog; complements GOPI Rule 16's `%v`→`%w` gate) |
| G9 | *"Struct literals must specify field names for types defined outside the current package."* (S2 `#field-names`; S3 *"Use field names in struct literals"*) | AST: composite literal of a **selector** type (`pkg.T{…}`) with any *unkeyed* element. | 0 hits in production code. | **B** (backlog) |
| G10 | *"Go source code must conform to the format outputted by gofmt."* (S1 `#formatting`) | `gofmt -l` over the tree must print nothing. | `gofmt -l internal cmd` prints nothing (verified at this commit). | **B** (backlog — a one-line test, deliberately left out of this task because it would touch formatting of 100+ files at the first violation; the ratchet is the point) |
| G11 | *"Every exported name … should have a doc comment"* (S2 `#doc-comments`; same source as checklist row 1.3) | AST: exported top-level `FuncDecl`/`TypeSpec`/`VarSpec`/`ConstSpec` without a preceding doc comment starting with the name. | ~120 exported names, ~35 without a doc comment (the §14 "new part" here is the *check*, not the rule — row 1.3 already states it). | **B** (backlog; equivalent to staticcheck ST1000/ST1020) |
| G12 | *"Go package names should be … a package that is just `util`, `helper`, `common` or similar is usually a poor choice."* (S3 *"Util packages"*; S2 `#package-names`: lowercase letters and numbers only) | Directory basename check: each `internal/*` and `cmd/*` dir name must be a member of an explicit allow-list (`util`, `helpers`, `common`, `misc`, `shared`, `base`, `core` are rejected); underscores rejected outside generated-dir exceptions. | 0 hits (the tree has `internal/toolnames`, `internal/standards`, … — no `util`). | **B** (backlog) |
| G13 | *"Function parameters and struct fields … one per line"* (S2 `#literal-formatting`, delegated to gofmt) | Covered by G10 (`gofmt -l`). | — | **B** (subsumed) |

### 16.2 Deliberately **not** mechanically checkable — excluded, with the reason

Excluded means: the source states the rule, but any checker would be a proxy for
a human judgement and would generate false positives — a gate that fires on the
wrong line gets switched off (checklist §11's whole premise).

| Source rule | Why it is excluded |
|---|---|
| *Names should be "proportional to the size of their scope"*, avoid "single-letter names in a large scope" (S2 `#variable-names`) | Needs scope-size + readability judgement; a length threshold is a proxy that fires on idiomatic `i`, `n`, `r`, `w`. |
| *"A name should be as short as it needs to be"* / repeated-use heuristics (S2/S3 naming guidance) | Not decidable from syntax. |
| *"Local consistency"* — a file's own style wins over the guide (S1 `#local-consistency`) | By construction it is a human override; any checker contradicts the rule it implements. |
| *"Interfaces should be defined by the consumer, and only when a consumer exists"* (S2 `#interfaces`) | Requires call-graph + intent analysis; "this interface has 1 implementation" is a proxy that flags legitimate seams (GoPi's tool/DB interfaces). |
| *"Do not create your own `context` type"* (S2 `#custom-contexts`) | *Is* checkable in principle (`type X interface{ … Deadline() … }`), but GoPi has no such type and the predicate needs an interface-method-set match that over-fires on any type accidentally satisfying it — not worth a gate. Listed rather than gated. |
| *Package doc completeness* / per-package comment placement (S2 `#package-comments`) | Requires judging whether the comment is *informative*, not whether it exists; existence-only checks reward `// Package foo is foo.` noise (already rejected in checklist §13). |
| *"Keep functions focused / short"* (S3, §4 rows 4.1/4.2) | Already handled by the checklist's line-span/cyclomatic rows; the thresholds there are repo-calibrated proxies, not Google rules — not re-derived here. |

### 16.3 Corpus note

Retrieval was done in this task; local copies of the three documents were kept
under `/tmp/gsuite_{guide,decisions,best-practices}.txt` during the run (the URLs
above are the authoritative pointers — `/tmp` is scratch and is expected to be
gone). Quotes above are verbatim from the retrieved text. No quote is claimed for
a document that was not retrieved.
