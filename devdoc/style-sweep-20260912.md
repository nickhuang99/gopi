<!-- doc-class: normative -->
# Task Report: repo-wide style sweep (mechanical, machine-decided)

Branch: `feat/style-sweep`  ·  worktree: `~/workspace/gopi-style-sweep`
Baseline: `bb76825` (master)  ·  Rules reference: `devdoc/coding-standards-checklist.md`

## Goal and boundary

Close the highest-frequency style debt in the repo, but only where the change is
**machine-decidable and behaviour-preserving**. No logic, control-flow, signature
or refactor changes. Every "this looks wrong but fixing it changes behaviour"
site was left as-is and is listed below with its reason.

The sweep is driven by a new zero-dependency scanner (`internal/stylegate`) that
is both the evidence source (before/after counts) and the anti-regression gate
(`TestStyleGate` runs inside `go test`).

## Completed — the three batches

| # | Batch | Before | After |
|---|-------|-------:|------:|
| 1 | `%v` → `%w` for error values in `fmt.Errorf` (guard-proven non-nil) | 362 | 10 |
| 2 | Explicit reason for every discarded error / bare `Close()` in production code | 122 ignored + 87 close | 0 + 0 |
| 3 | Test hygiene: `time.Sleep`, `os.Setenv`, `t.Skip` without a reason | 55 + 48 + 17 | 0 + 0 + 0 |

Counts come from `go run ./internal/stylegate/cmd/stylegate -root . -exclude internal/stylegate`
(the scanner excludes its own package; the gate test scans everything).

### Batch 1 — `%v` → `%w` (352 sites)

`go/ast` + `go/types` locate each `%v` whose argument implements `error` and
whose byte position is placed by the parser (never a regex). Two independent
guards were applied:

1. **Safety guard.** `%w` and `%v` are NOT interchangeable text: for a **nil**
   error `%w` prints `%!w(<nil>)` where `%v` prints `<nil>`. Measured, not
   assumed (`/tmp` probe, go1.24.5). So a site is converted only when the error
   value is provably non-nil: a dominant `if err != nil {` guard on the **same
   object** (object identity via `go/types`, so a shadowed `err` never inherits
   an outer guard) with no reassignment between guard and use. 10 sites stay `%v`
   for exactly this reason.
2. **Loudness guard.** `errors.Is/As` now reach through 352 previously opaque
   messages — that is a *semantic* change on the error path (wrapping makes an
   existing test that asserts `errors.Is(err, sentinel) == false` fail). The full
   suite was run to prove none exists: **green** (`go test ./...`, job-2). That
   run is the evidence for this batch, not a formality.

Verified mechanically: the diff is 352 changed lines, and every one is a single
`v`→`w` character swap (`git diff -U0` hunk audit: 352 minus/352 plus, 0
mismatching hunks).

### Batch 2 — explicit reason for every silent discard (365 sites)

Two documented forms, both requiring a reason of ≥ 10 characters:

```go
_ = os.Remove(path)      //stylegate:ignore best-effort cleanup of a probe file
//stylegate:ignore best-effort cleanup of a probe file
_ = os.Remove(path)
```

The long form exists because GoPi has multi-line calls — the end of a statement
is where an editor puts a trailing comment, so the scanner accepts a marker
anywhere in the statement's line span.

Ordinary prose does **not** silence the rule. A machine check needs one
unambiguous spelling; otherwise the TODO sitting next to an ignore would pass it.
Rule 15's two prose phrasings ("deliberately ignored", "intentionally ignored")
are also accepted, together with a reason.

**Proof that nothing but comments moved**: `internal/stylegate/cmd/tokenproof`
compares each touched file's token stream — comments stripped — against the
previous commit. Over the commit range `229e1da..09bcc2e`: **122 files checked,
0 differing**. This is strictly stronger than "it still parses", and it is the
definitive answer to *did runtime behaviour change?* for a comment-only batch:
no, the compiler sees the same file.

One caveat worth recording: this proof is only as good as the *placement* of
the comments. A marker inserted one line too low is still comment-only (the
token proof passes) but documents the wrong statement — which is exactly what
happened to 4 markers in the first attempt (see the tooling note at the end).
The proof for batch 2 is therefore *two* checks: token equality plus the
scanner still accepting every annotated site, which is what exposed the
misplacement.

Annotations are applied from a **reason table**
(`internal/stylegate/reasons/*.json`), one reviewed reason per site. A site with
no reviewed reason is left untouched and keeps failing the scan — the table is a
review trail, not a mute button. The annotator reports what it could not
justify:

**RETIRED 2026-11-05 (task 01a1a89d): the table used to be keyed per CALLEE.**
`-annotate` looked a site up as `file:line`, then `file::callee`, then a bare
`callee`, so one reviewed line silenced EVERY call to that function: on the
ground that change was measured at, 48 of 64 ignored-error sites were cleared by
~30 callee keys, and the same table shape is what let five "ignored-error over
its ceiling" recurrences be answered by moving a digit. The table is now keyed
`file:line` and NOTHING else — a callee-wide, `file::callee` or whole-file key is
REFUSED by name (`stylegate.ValidateReasonTable`, `internal/stylegate/reason_table.go`)
— and every rule whose sites are annotated this way has a ceiling of 0, so a new
bare discard is red and names its own line. The code below is quoted as it read
before that change; the entries it mentions are now per-site.

```
unmatched callees (left untouched, still findings): 1
     5  os.Setenv
```

### Batch 3 — test hygiene

- **`os.Setenv` → `t.Setenv`** for 4 files (7 sites). **This batch contained the
  one genuine regression of the whole sweep, and it was found only at the
  follow-up round — see "The sweep's own bug" below.** The short version:
  `t.Setenv`'s undo is registered with `t.Cleanup`, i.e. it runs when the TEST
  ends, whereas the pairs it replaced (`os.Setenv` + `defer os.Setenv(prev)`
  **inside a closure**) restored when the CALL returned. Two of those sites were
  per-call flips inside closures (a dispatch sandbox hook and an ssh exchange
  hook), so the conversion silently changed behaviour. Both are reverted to
  explicit pairs; the other 5 sites were genuinely test-scoped and stay.
- **The remaining 33 `os.Setenv`** keep the process-global form: they configure a
  value a **child process** must inherit (notify command, re-exec helper, task
  wrapper), where `t.Setenv`'s scoped restore is the wrong tool. Each carries a
  reason saying so.
- **55 `time.Sleep`** in tests carry a `//stylegate:allow-sleep` reason. These are
  not lazy sleeps: they are poll intervals, socket/X11 settle windows, mtime
  granularity, or a synthetic clock-tick budget. The rule deliberately does not
  "convert" them — a sleep that IS the wait cannot be replaced by a poll without
  changing what is tested. A `time.Sleep` with no reason still fails the scan.
- **17 `t.Skip`** now carry either a literal reason or a reason comment. The
  scanner resolves the reason syntactically (a string literal is accepted even
  when the package's type information is incomplete — `types.Info` carries no
  constant for a package whose importer degraded).

### The sweep's own bug (found at the follow-up round, bisected and fixed)

`cmd/agent`'s `TestDispatchToNodeDbPushSeedsPeerRowBeforeRun` began failing with
its ①/③ assertions reading the **peer's** row at the **center's** path. Bisected
across the branch's commits: `bb76825` clean, `229e1da` clean, `a73e063` (Batch 3)
failing — the sweep's own `t.Setenv` conversion, not a pre-existing failure and
not a master drift.

Mechanism, and why it is not obvious: the closure's `t.Setenv` registers its undo
with `t.Cleanup`, which fires at TEST end. The code it replaced was
`os.Setenv(...)` + `defer os.Setenv(prev)` **inside the closure**, which restored
when the CALL returned. The dispatch sandbox needs the env to flip center→node→
center *within one test*, so the file's own test-scoped `t.Setenv` calls and the
closure's per-call flips are different scopes that look identical at a glance.

Two instances existed; the second (`memory_sync_bidir2_e2e_test.go`'s ssh
exchange hook) is a **latent** trap — every helper re-points the env before use,
so it passes today while holding the wrong value between calls. Both are fixed.

The class is now mechanically guarded by a new rule, **`test-setenv-scope`**:
a `t.Setenv` inside a closure is reported when it (a) does not own its
`*testing.T` — a `t.Run` subtest body is fine, its cleanup scope IS the subtest —
and (b) sets a **per-call** value (a non-literal argument). The first draft
flagged all 10 closure sites; investigating them narrowed the predicate to the
real hazard, leaving 1 true positive (the latent one) and 0 false positives.
Both fixed sites and the rule's discrimination are pinned by
`TestSetenvScopeIsPerCall` (a per-call closure must be a finding; a
constant-in-a-table closure and a `t.Run` body must not be).

**The lesson worth keeping:** a mechanical style "fix" is only safe when the
replacement has the SAME scope as what it replaces. `t.Setenv` is the right
answer for a test-scoped set and the wrong answer for a per-call flip inside a
closure; the two are textually indistinguishable, which is exactly why the
conversion needs a scanner rule rather than care alone.

## Not Done (honest list)

1. **`unchecked-close` in test files: 32 sites retained, ceiling not zero.** The
   rule is closed in production code (0). Test files keep 32 sites where the
   scanner's reason model does not fit cleanly (e.g. `defer func() { _ = x.Close() }()`
   closures); they are counted by the gate, not silently dropped.
2. **`ignored-error` in test files: 211 retained — 182 after the second master
   merge (§6 of the follow-up record).** *Every production site is now
   documented* (0 non-test findings). The test-file remainder is dominated by
   `_, _ = db.Exec(...)` setup in fixtures, where the whole test is the assertion
   and each site needs its own sentence. Left as a measured, gated backlog rather
   than 211 fabricated reasons.
3. **8 `_ = err` sites swallowed by a branch predicate** (`if err != nil || !ok { _ = err }`).
   Giving each its reason means rewriting the predicate to distinguish the two
   conditions — that is a logic change, out of scope by the task boundary. They
   are documented in the scanner's undecidable/retained set, not fixed here.
4. **No external linter adopted.** Batch 4 (golangci-lint / errcheck) was optional;
   the environment's `go install` path is unreachable (network probe returned no
   HTTP code) and zero-dependency is the repo target (Rule 7). The in-repo
   scanner covers the four chosen rule classes plus the two test-hygiene rules and
   is the gate that keeps them closed.

## Retained sites and why (the decisions a reviewer should check)

**9 `errorf-verb` sites kept as `%v`** — the error value is not provably
non-nil, so `%w` would change the message text on the nil path (line numbers as
of this branch's tip; `stylegate -list` prints them):

`internal/tools/memory.go:2191`, `internal/tools/memory.go:2802`,
`internal/tools/memory_procedural.go:241,437,456,474`,
`internal/tools/memory_sync_claim.go:856`,
`internal/tools/memory_sync_procedural.go:245`, `internal/tools/task_merge.go:250`.

All nine are nil-guarded (`if err != sql.ErrNoRows { … }`,
`if err != nil && err != sql.ErrNoRows`); the report's earlier revisions listed
this set at stale line numbers and included `cmd/agent/cli_tasks.go:1305`.

**`cmd/agent/cli_tasks.go:1305` — fixed during the integration pass.** It was
the one site the two detectors disagreed on. Its `%v` formatted `runErr`, an
error value that is never nil there, and the sibling return 52 lines below
(`cli_tasks.go:1363`, the real failed-child path) already emitted the identical
message with `%w`. Under `%v` the wrapper could not be unwrapped by
`errors.Is/As`; `%w` makes the test escape's message and the production path's
message the same string from the same format verb, which is the property the
recursive-child tests depend on. The standards-gate baseline entry was deleted
(the gate itself flagged it as stale), so both detectors now report 9.

**`%+v` sites are not reported at all** — `%+v` prints the stack of a wrapped
error and `%w` does not; converting would delete information. Out of scope by
construction, not by omission.

**Deferred closes: 361 sites, informational.** `defer x.Close()` is the
conventional, readable form; the repo policy keeps it. The scanner counts them so
the number is visible, and does not report them.

## Verification

> **SUPERSEDED (this round).** The numbers below were measured at this report's
> own tip (`f3d9de7`/`5349526`) and are accurate for it. Two master merges have
> landed since; the current state — full suite EXIT 0 with the doc-anchor failure
> fixed upstream, both gates green on the same tree, and the `ignored-error`
> ceiling tightened 211 → 182 — is in `devdoc/style-sweep-followup.md` §1–§6, and
> the branch tip's own re-verified gate/anchor state is §7 there.

- **Build**: `CGO_ENABLED=0 go build ./...` → OK.
- **Format**: `gofmt -l cmd/ internal/` → empty.
- **Vet**: `go vet ./...` → clean.
- **Tests**: `go test ./...` → green **except** `TestHippocampusDocAnchorsResolve`,
  the pre-existing master failure recorded in the next section (proven to fail on
  `bb76825` too, filed as a bug task).
- **Batch 1 proof**: `git diff -U0` hunk audit — 352 lines changed, each a single
  `v`→`w` swap; build + vet + full suite green.
- **Batch 2 proof**: `229e1da..09bcc2e` → 122 files, **0 differing token streams**
  (comments stripped).
- **Batch 3 proof**: `09bcc2e..a73e063` → the 36 comment-only files are token-identical;
  the 4 conversion files differ only in `os.Setenv`→`t.Setenv` plus the dead
  save/restore lines (the full diff is in the commit message and the task log).
- **Gate**: `go test ./internal/stylegate -run TestStyleGate` → PASS. It fails on
  any new violation of a swept rule.
- **Commits**: `229e1da` (scanner + batch 1), `09bcc2e` (batch 2, rebased after the
  mutating-buffer fix), `a73e063` (batch 3), `f3d9de7` (this report), and the
  integration commit below.

### Did runtime behaviour change?

- Batch 1: **yes, on the error path, deliberately** — errors are now wrapped, so
  `errors.Is/As` reach through them. The full suite is green, which is the
  evidence that nothing depended on the old opacity.
- Batches 2 and 3: **no** — 157 files with byte-identical token streams; the 4
  converted files only swap how a test sets its own env var
  (`os.Setenv`+defer-restore → `t.Setenv`), which is the same value with a
  harness-managed restore.

## Pre-existing failure, not mine (recorded, not fixed)

`TestHippocampusDocAnchorsResolve` fails on **master** (`bb76825`):
`devdoc/agent-five-organs-ontology.md: anchor loop.go:1814 points at a BLANK line`.
Proven pre-existing by scanning master in a detached worktree
(`/tmp/gopi-mastercheck`) before any of my edits existed: the anchor points at a
blank line there too. `internal/agent/loop.go:1903` is unchanged by this branch
(it was in the batch-1 edit set, but the anchor was already off by the time the
sweep started). Filed separately as a bug task against master
(`01a08dc7-28f7-73ad-ba99-2904caf62708`).

**Closed by upstream.** Master `16dfe7e` ("docs: re-anchor the two loop.go
references that drifted (1814->1826, 1993->2019)") fixed it independently, and
that commit is now merged into this branch (`a8f949c`). Re-verified on the
post-merge tree: `go test ./internal/agent/ -run TestHippocampusDocAnchorsResolve
-count=1` → **ok** (0.177s). The bug row is closed as completed with the
`16dfe7e` + `a8f949c` evidence rather than re-fixed here.

**Scope note on the 9 vs 10 count.** The sweep's own type-aware scanner reports
**9** production `%v`-on-error survivors; the standards-gate baseline holds
**10**. The single difference is `cmd/agent/main.go:487`, and it is a false
positive of master's *shape* check, not a site this sweep missed:

```go
return "", fmt.Errorf("Error reading @ payload file %q: %w (escape a literal
    prompt starting with %v as \\%v)", body, err, payloadFilePrefix, payloadFilePrefix)
```

`err` is already wrapped with `%w`; the two `%v` verbs format
`payloadFilePrefix`, a **string constant**. `mentionsError` sees the identifier
`err` among the call's arguments and flags the call; stylegate resolves types and
correctly does not. Both numbers are right for their own detector — and that is
the stated argument for keeping the two gates separate rather than folding one
into the other (see the closing note below).

**A third count: 11 → 10.** The integration pass pruned master's baseline from
352 to 11 entries and, in a later round, deleted the last shape-check false
positive (`cmd/agent/cli_tasks.go:1305`, whose `%v` formatted a never-nil error —
fixed to `%w` so the test escape and the production path emit one identical
message). It is **10** now, and the standards gate itself refused to let the
stale entry linger ("STALE baseline entry … matches no violation"), which is the
stale-entry enforcement this report argues must survive any future merge of the
two gates.

## Tooling added (stdlib only — zero new dependencies)

| Path | What it is |
|------|------------|
| `internal/stylegate/stylegate.go` | `go/ast` + `go/types` scanner: errorf-verb, ignored-error, unchecked-close, test-sleep, test-setenv, test-setenv-scope, test-skip. Type info from `go list -export -json -deps` compiler export data — no external dep. Reports; never rewrites silently. |
| `internal/stylegate/gate_test.go` | The ratchet. Per-rule ceilings (production zero) so a reintroduced violation turns the suite red; `undecidable` sites are logged, never silently dropped. |
| `internal/stylegate/cmd/stylegate` | CLI: counts, `-list`, `-json`, byte-exact `-fix` (`%v`→`%w`), `-annotate` (reason tables). |
| `internal/stylegate/cmd/tokenproof` | Proves a batch is comment-only: token stream vs HEAD, comments stripped. |
| `internal/stylegate/reasons/*.json` | The reviewed callee→reason tables (the audit trail for batch 2). |

A useful negative result from the mechanical path: a **dry run that validates the
edit offsets before writing** (read the target byte, assert it is the expected
`v` inside `%v`) is worth the few lines it costs. An early revision computed
offsets from `token.Pos` values that share one `FileSet` across a package — not
file offsets — and produced a first edit that pointed past the end of a 15 KB
file. That is precisely the class of failure a one-character swap can hide: the
length check would have passed, and a wrong byte would have been rewritten. The
fix (offsets are always file-relative, taken from `fset.Position(x).Offset`) plus
the dry run is why the 352-site batch could be applied in one pass.

An important correctness note for future work on these tools: an earlier revision
of the annotator resolved line-insertions against a **mutating** buffer while
applying byte-offset insertions in the same pass, which silently misplaced 4
comment markers. Batch 2 was rebased and re-applied with the fix (resolve every
insertion to an absolute offset against the original source first, then apply
back-to-front). The misplaced markers were caught by the scanner itself — the
same files it had just annotated still failed — which is the argument for the
gate running in `go test` rather than as an occasional script.

---

## Integration with `feat/standards-gate` (the second half of this task)

The sweep was branched from `bb76825`. Four minutes after that, master merged
`feat/standards-gate` (`60c6306`) — an independent, parallel effort that turned
the same checklist into `internal/standards` gates (Rules 16–20) **with a
line-keyed baseline allow-list**. Master has since moved to `955bb91`.

The two efforts overlap on exactly two rules, and they compose rather than
conflict:

| Rule | `internal/standards` gate (master) | `internal/stylegate` (this branch) |
|------|-----------------------------------|------------------------------------|
| Rule 16 `%v`→`%w` | regex-ish AST shape check, `mentionsError` identifier heuristic; escape `// no-wrap:` | `go/types` check that a **resolved** error value is passed; annotates via reviewed reason tables |
| Rule 17 test env | `os.Setenv`/`os.Unsetenv` in `_test.go`; escape `// env-leak-ok:` | same sites + `//stylegate:ignore` reasons, plus sleep/skip/close hygiene it does not cover |

**The conflict is not semantic, it is the ratchet.** Master's baseline recorded
the 352 Rule-16 sites and 120 Rule-17 sites as *permitted debt*; this branch
*paid that debt* (352 → 9 production sites, 105 → 100 env sites). Master's
`enforce` fails on a baseline entry that matches no violation ("a stale
exemption is a silent permission"), so merging this branch without pruning would
turn the standards gate red with 346 stale Rule-16 + 15 stale Rule-17 entries.
Fixed lines also shift line numbers, so a baseline entry cannot simply be
re-pointed — it must be deleted.

### What was done after merging master

Merged `master` (`955bb91`) into `feat/style-sweep` — one clean merge, **no
conflicts** (the two efforts touch different files). Then:

1. **Pruned the baselines to exactly `current − stale ∪ new`** (hand-maintained
   table, no generator exists):

   | Baseline | Before | After |
   |----------|--------|-------|
   | `errorWrapBaseline` (Rule 16) | 352 | **11** |
   | `testEnvBaseline` (Rule 17) | 115 | **100** |
   | `elseAfterReturnBaseline` (Rule 18) | 7 | **8** |

2. **Added the 5 remaining Rule-16 sites as reviewed baseline entries.** They are
   the `%v` sites this sweep deliberately KEPT, and master's gate now agrees they
   must stay visible:

   `internal/tools/memory.go:2191`, `memory.go:2802`,
   `memory_procedural.go:456`, `memory_procedural.go:474`,
   `memory_sync_claim.go:856` — all nil-guarded
   (`if err != sql.ErrNoRows { … }`, `if err != nil && err != sql.ErrNoRows`): the
   value is nil exactly when it must not enter the chain, and `%w` would render
   `%!w(<nil>)` (the report's "Retained sites" section).

3. **Fixed one genuine new Rule-18 violation this branch introduced** — the
   `decode go list` loop in `internal/stylegate/stylegate.go` (an `else if err !=
   nil` after a terminating `break`) carried a `// else-ok:`-style marker that
   master's `elseAfterReturnEscape` (`// else-ok:`) did not recognize. Rather
   than sit in the baseline, the exemption is recorded on the line with the
   gate's own marker.

4. **Corrected the count-bearing comment** in `internal/standards/gate.go`
   (`maxBaselineEntries`) and added a SUPERSEDED note to
   `devdoc/standards-gate.md` — its measured tables stay as the landing record,
   with current sizes named so no reader takes 352 as today's state.

### Integration verification

- `go test -run 'TestStandardGates|TestStandardGateDataReport|TestBaselinesAreSortedAndUnique' ./internal/standards/` → **ok** (5/5 gates PASS, no stale entries, no new violations).
- `go test ./internal/stylegate/` → **ok** (the sweep's own ratchet still green).
- `go build ./...` → OK; `gofmt -l` → empty.

**Note on the two gates coexisting.** They are deliberately kept separate: the
standards gate is the *lock* (cheap predicate, line-keyed baseline, escape
marker), stylegate is the *scanner* (type-aware, reason tables, and the four
hygiene rules the standards gate does not implement). A future consolidation
should keep the standards gate's stale-entry enforcement — it is what caught
this branch's merge-time drift — and adopt stylegate's type resolution to shrink
Rule 16's `mentionsError` false-positive surface.
