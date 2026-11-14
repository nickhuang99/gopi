<!-- doc-class: historical -->
# Task Report: stylegate ceiling audit — pre-existing red master → green, two rules

Branch: `fix/stylegate-ceiling-audit` · worktree `~/workspace/gopi-sgfix` ·
base: master `37dc3f5e`

## Completed

**The red is gone, and both rules now sit at a measured, reviewed ceiling.**
`TestStyleGate` was failing on master on two rules at once:

```
stylegate ignored-error:    1 production hit(s), ceiling 0
stylegate unchecked-close: 34 test hit(s),       ceiling 32
```

### 1. `ignored-error` — `internal/tools/memory_provenance.go:475` (commit `c7ae8569`)

The site is `defer func() { _ = tx.Rollback() }()` in `provenanceBackfill`.
Fixed with the rule's own documented inline escape, **control flow untouched**:

```go
// The deferred Rollback runs after the Commit below on the success path,
// where database/sql returns ErrTxDone — a documented no-op, not a failure
// to report. Restructuring the control flow to satisfy the gate would be
// the real defect (never change semantics for a gate). The error path is
// covered because every failure between BeginTx and Commit returns its own
// wrapped error.
defer func() { _ = tx.Rollback() }() //stylegate:ignore defer Rollback after a successful Commit returns ErrTxDone (a no-op); the error path already returns its own wrapped failure, so there is nothing this error could add
```

Why annotate rather than restructure: after a successful `Commit`, `Rollback`
returns `ErrTxDone` — reporting it would be reporting a fictitious failure, and
dropping the defer to dodge the rule removes the rollback from the error paths.
The reason is specific and names the mechanism (`ErrTxDone` on the success
path), not a bare "ignore".

### 2. `unchecked-close` — all 34 test sites audited, then the ceiling ratcheted (commits `08320587`, `43b4fcbb`, `a4cfa316`, `08b8f096`, `b47abcad`)

**Every one of the 34 sites was reviewed individually** — not bulk-annotated.
The audit's classification (all 34 are `_test.go`; **zero production paths were
involved**, so no error needed genuine handling):

| # | site | verdict | reason class |
|---|------|---------|--------------|
| 1 | `cmd/agent/cli_resume_worktree_test.go:194` | test-cleanup | pipe-writer closed before the read that IS the assertion |
| 2 | `cmd/agent/cli_serve_test.go:39` | test-cleanup | flock released to prove re-acquisition |
| 3 | `cmd/agent/cli_serve_test.go:45` | test-cleanup | second flock released at test end; assertion already ran |
| 4 | `cmd/agent/cli_web_test.go:469` | test-cleanup | os.Pipe capture; the read below is the assertion |
| 5 | `cmd/agent/cli_web_test.go:501` | test-cleanup | same bounded-capture shape as #4 |
| 6 | `cmd/agent/main_payload_test.go:99` | test-cleanup | pipe-writer closed to give `readPayloadFile` EOF |
| 7 | `internal/agent/chat_session_test.go:34` | test-cleanup | SessionLogger drain-flush at t.Cleanup |
| 8 | `internal/agent/chat_session_test.go:35` | test-cleanup | RawLogger drain-flush at t.Cleanup |
| 9 | `internal/agent/crashdump_test.go:25` | test-cleanup | SessionLogger flush at t.Cleanup |
| 10 | `internal/agent/crashdump_test.go:36` | test-cleanup | close FORCES the flush the assertions read |
| 11 | `internal/agent/crashdump_test.go:73` | test-cleanup | same forced-flush shape as #10 |
| 12 | `internal/agent/crashdump_test.go:323` | test-cleanup | same forced-flush shape as #10 |
| 13 | `internal/agent/hippocampus_test.go:735` | test-cleanup | flush forced before the off/on log comparison |
| 14 | `internal/agent/loop_test.go:697` | test-cleanup | os.Stderr capture; read happens two lines below |
| 15 | `internal/agent/rawlog_test.go:47` | test-cleanup | error-path close, test already on `t.Fatal` |
| 16 | `internal/agent/rawlog_test.go:62` | test-cleanup | error-path close, test already on `t.Fatal` |
| 17 | `internal/agent/taskregister_test.go:39` | test-cleanup | SessionLogger flush at t.Cleanup (registration state is the assertion) |
| 18 | `internal/shot/capture_test.go:201` | test-cleanup | fake-CDP listener teardown (unwinds accept loop) |
| 19 | `internal/shot/cov_shot_x11_test.go:257` | test-cleanup | fake-X listener teardown after the shutdown channel |
| 20 | `internal/shot/cov_shot_x11_test.go:761` | test-cleanup | intentional socket break forcing the asserted write error |
| 21 | `internal/shot/cov_shot_x11_test.go:1042` | test-cleanup | bindability probe released, then `os.Remove`d |
| 22 | `internal/shot/shot_test.go:138` | test-cleanup | fake listener teardown |
| 23 | `internal/tools/cov_vision_text2img_test.go:192` | test-cleanup | error-path close on an already-failing `t.Fatalf` |
| 24 | `internal/tools/cov_vision_text2img_test.go:195` | test-cleanup | fixture close so `LoadImage` reads complete bytes |
| 25 | `internal/tools/memory_m1_test.go:114` | test-cleanup | close forces the reopen the test is about |
| 26 | `internal/tools/memory_m1_test.go:132` | test-cleanup | close before the covered `MemoryStore` reopen |
| 27 | `internal/tools/memory_provenance_test.go:157` | test-cleanup | cursor closed after a complete iteration |
| 28 | `internal/tools/memory_provenance_test.go:188` | test-cleanup | cursor closed after a complete iteration |
| 29 | `internal/tools/memory_sync_task_gitpushback_test.go:72` | test-cleanup | reopen handle closed after the commit was verified |
| 30 | `internal/tools/tasks_stale_test.go:40` | test-cleanup | close forces the reopening `TaskShow` read |
| 31 | `internal/tools/tasks_stale_test.go:73` | test-cleanup | close before the terminal-state reopen |
| 32 | `internal/tools/tasks_stale_test.go:119` | test-cleanup | close so `DetectStaleTasks` opens its own connection |
| 33 | `internal/tools/writetaskarea_test.go:248` | test-cleanup | error-path close on an already-failing `t.Fatalf` |
| 34 | `internal/tools/writetaskarea_test.go:251` | test-cleanup | close before the write-area path opens the DB |

Each site now carries its **own** reason comment (not one shared key), applied
with the gate's own annotator (`stylegate -annotate <reasons.json> -fix`) so the
edit is comment-only by construction. Ceiling moved **32 → 0** in the same diff
as the audit (`b47abcad`), so the rule now fails on *any* new bare `Close()` in
tests — the ratchet kept its teeth rather than being raised.

Note the direction of the move: the briefing allowed lowering or raising — the
audited actual is **0**, so the ceiling *tightened* from 32. That is the
strongest form of the fix: no headroom remains.

## Not Done

- **Nothing deferred.** All 34 sites and the 1 production site were resolved in
  this task; no site was left as "revisit later".
- No production (non-test) `unchecked-close` site existed to genuinely handle —
  the rule's production ceiling was already 0 and every one of the 34 hits is in
  a `_test.go` file.
- The `ignored-error` *test* ceiling (182) and `errorf-verb` (9) were left
  **untouched** — out of scope, and moving them would have been an unrelated
  count change the acceptance criteria forbid.
- **One inherited headroom, disclosed rather than quietly absorbed.** The
  `ignored-error` *test* ceiling is 182 while the measured test hits are 181 —
  i.e. it already sat 1 above its actual on base `37dc3f5e` (base total 182 =
  1 production + 181 test). That 1-slot slack **pre-dates this task** and is not
  a consequence of the Rollback annotation (which closed the *production* hit,
  dropping the total 182 → 181 while the test split stayed 181). The value is
  byte-identical to base, so no unrelated rule's count moved; tightening it to
  181 would have been a separate, un-audited edit to a rule this task did not
  sweep. Recorded here so a future reviewer does not mistake the slack for
  headroom this change created.

## Verification

Build (the repo's declared command):

```
$ CGO_ENABLED=0 go build ./...
BUILD OK   (no output, exit 0)
```

Gate (the acceptance gate):

```
$ CGO_ENABLED=0 go test ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/stylegate	1.469s
```

Measured counts after the change — **no unrelated rule moved**:

```
$ CGO_ENABLED=0 go run ./internal/stylegate/cmd/stylegate -json -o /tmp/sg-full.json
errorf-verb          9      (unchanged)
ignored-error      181      (base 182 = 1 production + 181 test → now 0 production + 181 test;
                              the ONE production hit is what the Rollback annotation closed; the
                              182 test *ceiling* is untouched — test hits were always 181)
unchecked-close      0      (was 34)  ← the audited actual
test-chdir/setenv/setenv-scope/skip/sleep   0  (unchanged)
undecidable         87  (informational, not findings)
defer-close        376  (informational, not findings)
packages_failed:  none   (type info complete — findings are exhaustive)
```

gofmt (the whole tree, as the acceptance criterion specifies):

```
$ gofmt -l .
(empty)
```

Touched packages' tests:

```
$ CGO_ENABLED=0 go test ./internal/tools/ ./cmd/agent/ ./internal/agent/ ./internal/shot/ ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/tools
ok  	github.com/nick/gopi/cmd/agent
ok  	github.com/nick/gopi/internal/agent
ok  	github.com/nick/gopi/internal/shot
ok  	github.com/nick/gopi/internal/stylegate
```

**Zero-behaviour-change proof (mechanical, not eyeballed).** The annotator only
inserts comments, but "comment-only" is asserted here independently: a throwaway
Go program compares the **token stream** (comments stripped —
`go/scanner` with mode `0`, so `ScanComments` is off) of each changed file at
`HEAD` against the working copy, and requires byte equality:

```
$ go run . ~/workspace/gopi-sgfix HEAD <19 changed files>
all changed files comment-only: token stream identical to HEAD
$ go run . ~/workspace/gopi-sgfix c7ae8569 internal/tools/memory_provenance.go
all changed files comment-only: token stream identical to HEAD
```

(An earlier attempt used `go/parser` + `go/printer`; it reported 4 false
"SEMANTIC CHANGE"s because `go/printer` preserves token positions — blank lines
and re-indentation — so a pure comment insertion renders as a diff. The
token-stream comparison is the exact criterion the annotator's own `-prove`
documents. Lesson stored to memory, id 1903.)

Commits on the branch (one per step):

```
79935ed9 docs(stylegate): independent verification re-run of every report claim
7665587f fix(stylegate): correct the audit date in the unchecked-close ceiling comment
b47abcad fix(stylegate): ratchet unchecked-close to 0 after the 34-site audit
08b8f096 fix(stylegate): audit the 15 internal/tools unchecked-close sites (test-cleanup, per-site reason)
a4cfa316 fix(stylegate): audit the 5 internal/shot unchecked-close sites (test-cleanup, per-site reason)
43b4fcbb fix(stylegate): audit the 10 internal/agent unchecked-close sites (test-cleanup, per-site reason)
08320587 fix(stylegate): audit the 4 cmd/agent unchecked-close sites (test-cleanup, per-site reason)
c7ae8569 fix(stylegate): document the Commit-then-Rollback no-op at memory_provenance.go:475
```

## Verification re-run (independent, by the reviewer pass)

Every claim above was reproduced from scratch rather than taken on trust:

**Base-vs-now count table** (the gate binary, run at the base commit in a
throwaway worktree and at the branch tip):

```
$ git worktree add /tmp/sgbase 37dc3f5e --detach
$ (cd /tmp/sgbase && go run ./internal/stylegate/cmd/stylegate -json -o /tmp/sg-base.json)
BASE  counts: errorf-verb 9, ignored-error 182, unchecked-close 34
      packages_failed: none
$ (cd ~/workspace/gopi-sgfix && go run ./internal/stylegate/cmd/stylegate -json -o /tmp/sg-verify.json)
NOW   counts: errorf-verb 9, ignored-error 181, unchecked-close 0
      packages_failed: none
```

The split matters and confirms the review's reading: `ignored-error` **182 → 181**
and the delta is exactly the one production site (base: 1 production + 181 test;
now: 0 production + 181 test). `unchecked-close` **34 → 0**. `errorf-verb` is
**9 → 9** — no unrelated count moved.

**The ratchet still has teeth** (a ceiling lowered to 0 is worth nothing unless
it fails on a new site — proved with a temporary probe, not argued):

```
$ cat > internal/stylegate/zz_redproof_probe_test.go   # a bare f.Close() in a test
$ go test ./internal/stylegate/ -run TestStyleGate -count=1
--- FAIL: TestStyleGate (3.10s)
    gate_test.go:126: stylegate unchecked-close: 1 test hit(s), ceiling 0 — new
      violations of a swept rule are regressions …
FAIL
$ rm internal/stylegate/zz_redproof_probe_test.go
$ go test ./internal/stylegate/ -run TestStyleGate -count=1
ok  	github.com/nick/gopi/internal/stylegate	3.057s
```

The probe was deleted immediately; `git status` is clean of it.

**Comment-only proof, per commit.** The report's earlier token-stream check
compared the whole batch against one revision, which is weaker than it looks
(the later commits' files were compared against a revision that already carried
their comments). Re-done per file against **that file's own parent commit**:

```
$ for f in <20 changed non-gate .go files>; do
      par=$(git rev-parse $(git log --format=%H -1 37dc3f5e..HEAD -- $f)^)
      /tmp/tpcmp/tpcmp "$par" "$f" | tail -1
  done | sort | uniq -c
      1 all 1 file(s) comment-only vs 37dc3f5e…   (memory_provenance.go, vs base)
      4 all 1 file(s) comment-only vs c7ae8569…
      6 all 1 file(s) comment-only vs 08320587…
      3 all 1 file(s) comment-only vs 43b4fcbb…
      6 all 1 file(s) comment-only vs a4cfa316…
```

All 20 files: token stream (comments stripped, `go/scanner` mode 0) identical to
their parent. **Zero code tokens changed — only comments.** The one file *not*
in that list is `internal/stylegate/gate_test.go`, whose diff is the ceiling
constant plus its rationale comment (read in full in the re-run; `RuleErrorfVerb`
and `RuleIgnoredError` values are byte-identical to base).

**Touched-package tests, isolated** (the acceptance criterion):

```
$ CGO_ENABLED=0 go test ./internal/tools/ ./cmd/agent/ -count=1
ok  	github.com/nick/gopi/internal/tools	216.069s
ok  	github.com/nick/gopi/cmd/agent	38.352s
EXIT=0

$ CGO_ENABLED=0 go test ./internal/agent/ ./internal/shot/ -count=1
ok  	github.com/nick/gopi/internal/agent
ok  	github.com/nick/gopi/internal/shot

$ gofmt -l .
(empty; prints nothing — see "gofmt" above for the same result)
```

**One honest flake, reported rather than hidden.** A single run of all five
packages at once (`go test ./internal/tools/ ./cmd/agent/ ./internal/agent/
./internal/shot/ ./internal/stylegate/ -count=1`) returned FAIL for
`internal/tools` while the other four passed. `internal/tools` re-run alone
immediately after, **verbosely**, was PASS with zero `--- FAIL` lines
(`ok … 174.325s`), and the two-package acceptance run above also passed. The
one-off failure is under parallel package load and is not reproducible from the
changed files — every touched file in `internal/tools` is a `_test.go` whose
only change is a comment (token-stream proof above). Recorded here so a future
reader does not rediscover it as a mystery.

## Files changed

- `internal/tools/memory_provenance.go` (comment + reason, control flow untouched)
- 19 `_test.go` files across `cmd/agent`, `internal/agent`, `internal/shot`,
  `internal/tools` (one per-site reason each)
- `internal/stylegate/gate_test.go` (ceiling 32 → 0, with the audit recorded in
  a comment above the constant)

## Deliverable declaration (Rule 6d)

- **Branch**: `fix/stylegate-ceiling-audit` in `~/work/GoPi` (the worktree
  `~/workspace/gopi-sgfix` shares the main repo's refs, so the commits are
  already on the branch — no push needed).
- **Stable handle**: tag `stylegate-ceiling-audit-v1`, which resolves to the
  final commit of this branch. A report cannot name its own commit hash (the
  hash does not exist until the report is written), so the tag is the
  authoritative handle; `git rev-parse fix/stylegate-ceiling-audit` gives the
  same number for any reader who prefers the branch name.
- **Recovery**:
  `git show stylegate-ceiling-audit-v1:devdoc/stylegate-ceiling-audit.md`
  (or `git show fix/stylegate-ceiling-audit:devdoc/stylegate-ceiling-audit.md`)
- **Merge state**: NOT merged (Rule 6e — merge is the PA's step after review).
  The commits are audit + report only; nothing was merged into master from this
  task.

## Final acceptance appendix (fresh run, branch tip)

Every acceptance criterion re-measured on the finished branch in one detached
run, so the numbers above are not the only evidence:

```
$ go test ./internal/tools/ ./cmd/agent/ -count=1
ok  	github.com/nick/gopi/internal/tools	202.467s
ok  	github.com/nick/gopi/cmd/agent	34.822s
TOOLS_CMDAGENT_EXIT=0

$ CGO_ENABLED=0 go build ./cmd/agent/
BUILD_EXIT=0

$ gofmt -l .
(empty)                                        GOFMT_EXIT=0

$ go test ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/stylegate	5.471s
GATE_EXIT=0
```

The count table was also re-derived independently from the gate's own JSON
output rather than copied from the earlier run — findings grouped by rule and
test/production location:

```
('errorf-verb', False) 9        ← production ceiling 9, unchanged
('ignored-error', True) 181     ← test ceiling 182, unchanged (false:0 after the Rollback annotation)
packages_failed: None           ← findings exhaustive, no package dropped out of type-checking
unchecked-close: absent         ← 0 hits, exactly what the 32 → 0 ratchet asserts
```

`unchecked-close` appearing nowhere in the findings is the ratchet's proof: the
ceiling is now 0 and the gate still passes, so the 34 audited sites really are
zero unannotated hits — not "the ceiling was raised to cover them".

## Independent re-verification (base-vs-branch, this round)

The claim "the ceiling reflects the audited actual and no unrelated count
moved" was re-tested by running the **same gate binary** against base
`37dc3f5e` (detached worktree `/tmp/sgfix-base`, since removed) and against the
branch tip, and diffing the JSON:

```
BASE  $ go run ./internal/stylegate/cmd/stylegate -json   # at 37dc3f5e
      counts: {errorf-verb: 9, ignored-error: 182, unchecked-close: 34, ...}
      packages_failed: None

NOW   $ go run ./internal/stylegate/cmd/stylegate -json   # at branch tip
      counts: {errorf-verb: 9, ignored-error: 181, unchecked-close: 0, ...}
      packages_failed: None
```

`ignored-error` 182 → 181 is exactly the one production hit the Rollback
annotation closed; `unchecked-close` 34 → 0 is the audited actual;
`errorf-verb` is unchanged at 9. `packages_failed: None` on both sides means
type information was complete, so the counts are exhaustive rather than a
silent package drop-out.

**Comment-only proof with the repo's own tool.** `internal/stylegate/cmd/
tokenproof` compares a file's working copy with its HEAD version as a token
stream with comments stripped. Run from the base worktree over the 20 changed
`.go` files *other than* the ceiling file:

```
$ go run ./internal/stylegate/cmd/tokenproof <20 files>
OK: 20 file(s) — identical token streams (comments stripped)
```

The single file whose tokens DO change is `internal/stylegate/gate_test.go` —
the ceiling 32 → 0, which is the intended content change.

**Red-proof re-run.** With the ceiling at 0, a temporary `_test.go` carrying a
bare `f.Close()` must make the gate fail — otherwise the ratchet is inert:

```
$ (temp internal/stylegate/zz_probe_ceiling_test.go added)
--- FAIL: TestStyleGate (2.40s)
    gate_test.go:126: stylegate unchecked-close: 1 test hit(s), ceiling 0 — new
    violations of a swept rule are regressions; fix them or document an explicit
    reason (see devdoc/style-sweep-20260912.md)
FAIL	github.com/nick/gopi/internal/stylegate	2.430s

$ (probe removed)  go test ./internal/stylegate/ -count=1   => ok
$ git status --porcelain                                    => (clean)
```

**Annotation-claim spot check.** The Rollback reason asserts "the error path
already returns its own wrapped failure". `provenanceBackfill`
(`internal/tools/memory_provenance.go` lines 462–531) was read in full to test
that claim rather than trust it: every failure between `BeginTx` and `Commit`
(the count scan, the `UPDATE` exec, `RowsAffected`, the honest-unknown scan)
returns `fmt.Errorf("provenance backfill: %w", err)`, and `Commit` itself does
too. So the discarded `Rollback` error is genuinely additive-free, and the
annotation describes the real control flow.

**Scope of the acceptance evidence (which commits it covers).** The detached
acceptance run above executed at `c9357e7d`. Everything committed after it is
`devdoc/`-only — `git diff --name-only c9357e7d..HEAD` lists exactly one file,
this report — so no compiled artifact changed between that run and the final
tip (`git rev-parse fix/stylegate-ceiling-audit`). The gate was additionally
re-run at the final tip and is green there:

```
$ go test ./internal/stylegate/ -count=1
ok  	github.com/nick/gopi/internal/stylegate	1.722s     GATE_EXIT=0
$ gofmt -l .
(empty)
$ git status --porcelain
(clean)
$ git diff --name-only 37dc3f5e..HEAD -- '*.go' | wc -l
21          ← 20 comment-only files + gate_test.go (the ceiling)
```
