<!-- doc-class: historical -->
# Task Report: tools / crash / build design-spec truth audit (epic C6)

> Scope: `devdoc/remote-tool-design.md`, `devdoc/shot-design.md`,
> `devdoc/crash-dump-design.md`, `devdoc/rust-port-roadmap.md`.
> Base commit: **d9df443d** (`feat(chat): a clean start …`), worktree
> `/home/user/workspace/gopi-c6-tools-build-doc-audit`, branch
> `feat/c6-tools-build-doc-audit` (base `d9df443d`).
> Parent epic: `01a0b49f` C6, plan `devdoc/doc-truth-audit-plan.md` §2.2/§4.

## Summary

Four design/build specs, four verdict rows, all resolved against the tree at
`d9df443d` — **all four docs describe something that is real code or a real,
honest non-implementation; none of them is a fabrication.** The defects found are
*reader-legibility* defects (a planned doc that does not say "planned", a roadmap
whose headline verdict reads as a project decision) plus two stale "Not Done"
paragraphs — not silent false claims about shipped behaviour.

Plainly, per the acceptance ask — **does the described tool/port exist today?**

| doc | described thing | exists TODAY? |
|---|---|---|
| `remote-tool-design.md` (86 L) | a `remote` LLM tool (5-tool SSH closure) | **NO** — no tool, no symbol, no constant; design only |
| `shot-design.md` (86 L) | `gopi shot` screenshot subcommand (raw-CDP, zero-dep) | **YES** — shipped, `internal/shot/` (12 files) + `cmd/agent/cli_shot.go` |
| `crash-dump-design.md` (90 L) | crash dump + `session_end` error block + failed-ledger linkage + orphan sweep | **YES** — every named symbol resolves; tests pass |
| `rust-port-roadmap.md` (53 L) | a Go→Rust port | **NO** — 0 `.rs`, 0 `Cargo.toml` anywhere; the doc is a *reframe record*, not a plan of record |

## Verdict rows (one per doc)

| # | doc | class | exists? | evidence (this tree) | drift |
|---|---|---|---|---|---|
| 1 | `remote-tool-design.md` | SPEC / **planned-absent** | **NO** | `git grep -n 'ToolRemote\|"remote"' -- '*.go'` → no tool wire-name hit; `internal/toolnames/names.go` has 29 consts and **no** remote one; `claudeToolDefs` (Rule 3 source of truth, `internal/agent/loop.go`) lists 17 defs, no remote; GOPI.md:87 "GoPi has 17 tools" (no remote) | doc carries `> 状态: 设计阶段` (line 5) but **no code-facing status**, and its "系统提示词注入" block asserts a tool set that contradicts the shipped one; the plan's own §2.2 already filed it as failure-mode **(B)** |
| 2 | `shot-design.md` | SPEC / **shipped** | **YES** (as a CLI subcommand, not an LLM tool) | `cmd/agent/cli_shot.go` (flags `--url/--self/--out/--viewport/--wait/--wait-selector/--addr`; `isShotSubcommand` at :65), dispatched from `cmd/agent/main.go:841`; `internal/shot/{shot,cdp,chrome,x11,xactuate}.go`; `shot.DefaultViewportWidth/Height = 1440/2800`; design->code self-citation at `internal/shot/cdp.go:5` | implemented exactly on route (c); `--fullpage` explicitly deferred (and still absent); `--node` deferred (absent) |
| 3 | `crash-dump-design.md` | report-as-design / **shipped** | **YES** | `session.go:370 LogSessionEnd(… *ErrorRecord)`, `session.go:594 WriteCrashDump`, `session.go:688 HasSessionEnd`, `session.go:770 DetectOrphanSessions`, `crashdump.go:204 panicCollectorStore`, `:228 recoverRunPanic`, `:288 BackfillOrphanCrashDumps` (called `cmd/agent/main.go:1006`), `taskregister.go:326 finishSessionTaskRecorded`, `deepseek/lasterr.go:202 IsAccountLevelStatus` (consulted at `:147`), `:211 RemedyText`, `session.go:433 SummaryDigest` | two **stale "Not Done"** items (both since fixed on master: the `bug036` matrix, and the resume-hint taxonomy) and one drifted count (see F3) |
| 4 | `rust-port-roadmap.md` | REPORT / **informational** (a reframe record) | **NO port** — correctly so | `git ls-files` → 0 `*.rs`, 0 `Cargo*`; `find . -name Cargo.toml -o -name '*.rs'` → empty; Makefile/scripts mention no `cargo`/`rustc` | not drift: the doc's *own* verdict is "no Rust exists, this is greenfield". Its risk is interpretation (F2) and age (its inventory is true at `91201ba`, not at `d9df443d`) |

## Findings (ranked)

### F1 — `remote-tool-design.md`: a complete design for a tool that does not exist, with nothing in the file that says so at the *code* level
**Evidence.** The doc's `> 状态: 设计阶段，独立于 scheduler 项目` (line 5) describes the
2026-08-02 writing phase, not the implementation state; the body reads as
present-tense commitment ("方案：5 工具最小闭包", "硬边界（写进系统提示词，不可绕过）").
Measured today: no `remote` tool constant (`internal/toolnames/names.go`, 29 consts),
no `toolnames.ToolRemote` (`code_search` → not found), no remote entry among the 17
`claudeToolDefs` (`internal/agent/loop.go`), and GOPI.md:87 still declares 17 tools
without it. Every other hit for the string `remote` in the tree is unrelated
(`peer_alias.go`, `repo_sweep.go`, git remotes).
**Why it matters.** A reader (human or agent) scanning `devdoc/` for "how do I do
cross-host work" finds a full interface spec and a prompt injection block, and has
no in-file signal that none of it exists — the plan's §2.2 named exactly this shape
as the audit's target failure mode (B).
**Suggested fix.** One status header at the top: `> **Status: NOT IMPLEMENTED
(planned design, 2026-08-02).** No `remote` tool exists; `toolnames` has no
`ToolRemote` and Rule 3's list is 17 tools. See devdoc/doc-truth-audit-plan.md §2.2.`
Optionally a one-line note that the design's "5-tool closure + prompt-injection hard
boundary" would require amending Rule 3 first.
**Bug-worthiness:** **LOW–MEDIUM** — zero runtime impact, but it is the highest
misreading risk in this slice (a design for an SSH tool is exactly what a
multi-host task would search for). Doc-only fix, ≤5 lines.

### F2 — `rust-port-roadmap.md`: the reader cannot tell "verdict" from "decision", and the numbers moved
**Evidence.** Line 5: "`task(action=reframe)` returned **TOO_BIG** … The harness
enqueued one child task per roadmap item (13 children, ids `01a088f9-f6b2…` …
`01a088fa-0154…`)". Measured on this host: `gopi task list --all` (2038-line dump)
contains **no** row whose id starts `01a088f9` or `01a088fa` — the ids do not
resolve here, so a reader cannot follow the promise "children 1–13 are queued
`pending` in the ledger". No `*rust*` branch and no `Cargo.toml` exists.
**Evidence (age).** The doc's inventory is measured at "HEAD `91201ba`": that commit
exists (2026-09-10) and the numbers are **exactly right there** — `git ls-tree -r
91201ba` → 394 `.go`, `git grep '^func Test' 91201ba` → 1,913 (verified this
session). At `d9df443d` the same measurements are 1,096 `.go` / 4,444 test funcs /
59 package dirs / 380,857 total Go LOC / 144 CLI verbs / 29 toolname consts. So the
doc is **not wrong** — it is a snapshot whose age is invisible.
**How a reader is meant to interpret it (the judgment the briefing asked for).**
It is a *reframe record*, i.e. REPORT-class: "I investigated a Go→Rust port epic,
judged it TOO_BIG, and produced an ordered decomposition instead of porting".
Correct readings: (a) no Rust port is planned or in progress; (b) the 13 items are a
*proposed* decomposition whose queue state is not verifiable from this doc; (c) the
inventory is a 2026-09-10 measurement, not a current one. **The doc invites the wrong
reading on two points** — the harness-enqueue sentence reads as a durable ledger
fact, and the bare "394 `.go` files / 1,913 tests" invites re-measurement mismatch.
**Suggested fix.** Add a header block: `> **Status: reframe record. No Rust port
exists (0 .rs / 0 Cargo.toml) and none of the 13 children is queued as a live ledger
row on this host. Inventory below is a snapshot at 91201ba (2026-09-10); the tree has
since grown (d9df443d: 1,096 .go / 4,444 test funcs / 380,857 LOC).**`
**Bug-worthiness:** **LOW** — no false claim about code; the fix is a status header.
Worth doing because this doc is the *only* Rust surface a reader will find.

### F3 — `crash-dump-design.md` ①: the "四个发送点" count is drifted — the mechanism now funnels through one sender-level recorder
**Evidence.** Doc line 39: "四个发送点（非流/流 × retryDo/ladder roundtrip）在非 200 或
transport error 时 `record(status, body, path, stream, err)`". On `d9df443d` the
recorder is a **single method**, `recordClientFailure` (`deepseek/lasterr.go:88`),
with **nine** call sites in one file — `claude_client.go:858, 864, 868, 912, 916,
1042, 1046, 1106, 1112` — spanning both non-stream and stream paths. The doc's
intent (collection sits at the send site, status+body in hand) is exactly what
shipped and is *better* than described; only the count/locus is stale.
**Suggested fix.** Replace the sentence with a pointer: "collection is the one
sender-level recorder `Client.recordClientFailure` (deepseek/lasterr.go:88); call
sites: `git grep -n recordClientFailure -- internal/deepseek`".
**Bug-worthiness:** **LOW** (a count, not a behaviour).

### F4 — `crash-dump-design.md` "Not Done": two items are now DONE on master (stale honesty, not dishonesty)
**Evidence.** Doc's Not Done says *"session_end error 块的 `recoverable_hint` 目前是
启发式（status+body 关键字），未接 bug036 完整分类矩阵实现"* (line 69). Shipped today:
`FailureSnapshot.Category()` (`deepseek/lasterr.go:142`) derives the full bug036
taxonomy (`account/network/4xx/5xx/vendor/panic/unknown`, account checked **before**
429 so GLM's 1113-behind-429 classifies correctly) and `RecoverableHint()`
(`lasterr.go:178`) implements the matrix including the 400-1214 compaction case —
with `TestErrorCategoryMatrix` green (`go test -run TestErrorCategoryMatrix
./internal/agent/` is part of the passing run below).
**Suggested fix.** Close both Not Done bullets with the landing commits (the file's
own history shows them: `5324130f`, `2cb7d9cb`) — they were already partially
retrofitted into §①'s body, the Not Done section was not.
**Bug-worthiness:** **LOW** — the risk is a reader believing a *better* mechanism is
still pending and re-implementing it.

### F5 — `shot-design.md`: shipped doc carrying two forward-looking references that did not come true
**Evidence.** The doc defers `--fullpage` to "future" (line 72) — `git grep
'fullpage\|fullPage'` → **0 hits**, still absent. It defers `--node` (remote nodes)
to a TODO — no `--node` flag in `cli_shot.go`'s documented flag set (:34–:55); the
`--addr` flag shipped instead. Its "测试策略" names a single `internal/shot/shot_test.go`
while `internal/shot/` now carries **7** test files (incl. `capture_test.go`,
`x11_test.go`, `testmain_scratchroot_test.go`). The doc's core claims (route (c),
zero-dep, chrome detection chain, `--wait-selector` polling, vision-loop closure)
are all implemented as written, and `cdp.go:5` still cites the doc as the design.
**Suggested fix.** A one-line "shipped, with these deviations" note (`--addr` instead
of `--node`; `--fullpage` still absent; 7 test files not 1).
**Bug-worthiness:** **LOW** — the doc currently reads *better* than reality only in
the test-file count, and *ahead* of reality on `--fullpage`.

### F6 — cross-cutting: none of the 4 docs carries the repo's doc-class banner, and the machinery that would demand one is not on master
**Evidence.** The whole-tree grep for `DOC-INDEX` finds a live classification
subsystem — `internal/docclass/docclass.go` (`Path = Root + "/DOC-INDEX.tsv"`),
`internal/standards/docclass_gate_test.go`, `internal/agent/docanchor_test.go:231`,
`scripts/docclass-red-proofs.sh:35`, `tools/docclass/{classify,banner}.py` (banner
form `<!-- doc-class: historical -->`), the manifest `devdoc/DOC-INDEX.tsv` (2012
rows) and its report `devdoc/doc-truth-classification-report.md` — but **none of it
is tracked at `d9df443d`**: `git ls-files` for those paths errors "did not match any
file(s) known to git", and `internal/docclass/` does not exist on disk in this
worktree. The machinery lives on the unmerged branch `feat/gates-doc-truth-audit`
(commits `c30b709a`, `ed1be6df` — `git merge-base --is-ancestor ed1be6df HEAD` fails).
**Why it matters.** The banner requirement is the project's own answer to the
"planned vs shipped" ambiguity F1/F2 describe; until that branch lands, these four
docs (and every other devdoc) carry no machine-readable class, so this audit's
per-doc class calls are model judgments with no gate behind them.
**Suggested fix.** On landing of that branch: classify `shot-design` / `crash-dump-design`
as shipped-design (retain, add status header), `rust-port-roadmap` as historical
(reframe record), and `remote-tool-design` as planned-absent (status header, keep —
the design has value). No change to prod code.
**Bug-worthiness:** **MEDIUM as a project-level coverage gap, NOT a defect of these
4 docs** — reported, not filed (acceptance says nothing is submitted).

### F7 — adjacent code nit found while resolving claims (out of the 4-doc scope, recorded for completeness)
**Evidence.** `internal/agent/crashdump.go:254-255` carries the comment line
"`// BackfillOrphanCrashDumps is the startup orphan detector (crash-dump epic`"
**twice** in a row (grep returns the same text at both line numbers).
**Suggested fix.** Delete one line. **Bug-worthiness:** **TRIVIAL** (cosmetic).

## Coverage and limits

- **Swept exactly** the four docs named by the epic and the two neighbouring
  artifacts their claims need (`GOPI.md` Rule 3, `devdoc/doc-truth-audit-plan.md`
  §2.2/§4). No other devdoc was audited; this is not a re-run of the §4 partition.
- Claims were resolved against **`d9df443d`** only; earlier commits were consulted
  solely to test a doc's own stated base (`91201ba`), never to excuse drift.
- Ledger statements use the text of `gopi task list --all` (a 2038-line dump), not a
  SQL query: `~/.gopi/tasks.db` is a 0-byte file and holds no `tasks` table
  (`sqlite3 … ".tables"` → empty), so the store is not there.
- Absence claims ("no Rust", "no remote tool", "no `--fullpage`") are exhaustive
  greps over tracked + on-disk files in this worktree; they are not a proof about
  other machines' checkouts.
- The shot verification is `-short` (chrome-dependent E2E cases are its own skip
  contract); no live screenshot was taken, as the claim under test is symbol
  existence and flag surface, not PNG output.
- Nothing was submitted to the task queue (acceptance) and no production file was
  modified — report-only.

## Verification

- Build: `go build ./...` → **OK** (no output, exit 0; no code was changed anyway).
- Tests (branch-scoped to the audited surfaces, all on `d9df443d` + this doc):
  - `go test -run 'TestRecoverRunPanic|TestWriteCrashDump|TestDetectOrphanSessions|TestLogSessionEnd' -count=1 ./internal/agent/`
    → `ok github.com/nick/gopi/internal/agent 4.481s`
  - `go test -run 'TestErrorCategory|TestLastErrDetail|TestRecoverable' -count=1 ./internal/deepseek/`
    → `ok github.com/nick/gopi/internal/deepseek 0.610s`
  - `go test -short -count=1 ./internal/shot/` → `ok github.com/nick/gopi/internal/shot 23.058s`
- Commit: report-only commit on `feat/c6-tools-build-doc-audit`; the hash is in
  `git log -1 --format=%h -- devdoc/tools-build-doc-truth-audit.md` (a report cannot
  cite its own commit hash — it is written before the commit exists).

## Evidence (self-filed)

One line per gate/claim — the command and what it printed.

- `git rev-parse --short HEAD` @ worktree → `d9df443d`; `git log -1 --format='%H %s'` → `d9df443deb0b3d3d4acc78e2ebaa32b838e72d30 feat(chat): a clean start …`.
- Remote tool absent: `code_search ToolRemote` → `No symbol "ToolRemote" found in ctags index or ripgrep`; `git grep -n 'ToolRemote\|"remote"' -- '*.go'` → only unrelated hits (`archive/taskingest.go:126 p.RemoteNode = "remote"`, a git-remote in a test).
- Tool contract: `grep -c '^	Tool' internal/toolnames/names.go` → `29`; `claudeToolDefs` name list → 17 entries (`ToolRead … ToolScreenCapture`); `grep -n 'GoPi has' GOPI.md` → line 87, "17 tools", no `remote`.
- Shot shipped: `cmd/agent/cli_shot.go:65 func isShotSubcommand`; `cmd/agent/main.go:841` includes `isShotSubcommand(dispatchArgs)`; `internal/shot/*.go` → 12 files (7 `_test.go`); `grep -n 'DefaultViewport' internal/shot/shot.go` → `51: DefaultViewportWidth = 1440`, `52: DefaultViewportHeight = 2800`; flags via `cli_shot.go:60-61` (`url,self,out,viewport,wait,wait-selector,addr`).
- Shot zero-dep: `grep -c 'chromedp\|rod' go.mod` → `0`; `git grep -n chromedp -- '*.go'` → only the doc-citing comment `internal/shot/cdp.go:9`; `grep -rn 'import "C"' internal/shot/` → empty.
- Shot tests: `go test -short -count=1 ./internal/shot/` → `ok … 23.058s`; `TestFindChromeEnvOverride` at `internal/shot/shot_test.go:35`, `TestUsageAdvertisesShot` at `cmd/agent/cli_shot_test.go:13`.
- Crash-dump symbols: `grep -rn 'func …' internal/agent` → `session.go:370 LogSessionEnd`, `session.go:594 WriteCrashDump`, `session.go:688 HasSessionEnd`, `session.go:770 DetectOrphanSessions`, `crashdump.go:204 panicCollectorStore`, `crashdump.go:228 recoverRunPanic`, `crashdump.go:288 BackfillOrphanCrashDumps`, `taskregister.go:326 finishSessionTaskRecorded`, `session.go:433 SummaryDigest`.
- Crash-dump wiring: `git grep -n 'BackfillOrphanCrashDumps' -- '*.go'` → `cmd/agent/main.go:1006 BackfillOrphanCrashDumps(sessionIDOf(ag))`; `loop.go:2698 a.recoverRunPanic(r, a.finishSessionTask)`.
- Crash-dump tests exist and pass: `TestRecoverRunPanic_WritesDumpAndRepanics` `crashdump_test.go:425`, `TestErrorCategoryMatrix` `:473`, `TestWriteCrashDump_NamingFamily` `:108`, `TestLogSessionEnd_NormalExitHasNoErrorBlock` `:35`, `TestDetectOrphanSessions_Matrix` `orphanclassify_test.go:227`, `TestCollectErrorRecord_HTTPAndPanic` `:246`, `TestSummaryDigest` `:407`; run → `ok … 4.481s`.
- Taxonomy landed: `read internal/deepseek/lasterr.go` → `Category()` at :142 (`IsAccountLevelStatus` checked before the 429 clause), `RecoverableHint()` :178, `IsAccountLevelStatus` :202, `RemedyText()` :211, `CategoryAccount = "account"` const; `go test -run 'TestErrorCategory' ./internal/deepseek/` → `ok … 0.610s`.
- Send-point drift: `git grep -n recordClientFailure -- internal/deepseek | grep -v _test` → `lasterr.go:88` (definition) + `claude_client.go:858, 864, 868, 912, 916, 1042, 1046, 1106, 1112` (nine call sites).
- Rust absent: `git ls-files | grep -c '\.rs$'` → `0`; `git ls-files | grep -c -i Cargo` → `0`; `find . -name Cargo.toml -o -name '*.rs'` → empty; `grep -rn 'cargo\|rustc' Makefile scripts/*.sh` → empty.
- Rust doc's base verified: `git cat-file -t 91201ba` → `commit`; `git ls-tree -r 91201ba --name-only | grep -c '\.go$'` → `394`; `git grep -h '^func Test' 91201ba -- '*_test.go' | wc -l` → `1913`; date → `2026-09-10 09:30:08 +0800`.
- Rust numbers now: `git ls-files '*.go' | wc -l` → `1096`; `git grep -h '^func Test' -- '*_test.go' | wc -l` → `4444`; `git ls-files '*.go' | xargs cat | wc -l` → `380857`; `grep -c '^	Tool' internal/toolnames/names.go` → `29`.
- Rust children unresolvable: `gopi task list --all | grep -c '01a088f9\|01a088fa'` → `0` (the same dump contains other `01a088xx` rows, so the ids are genuinely absent).
- Banner machinery not on master: `git ls-files --error-unmatch devdoc/DOC-INDEX.tsv internal/docclass/docclass.go` → `error: pathspec … did not match any file(s) known to git`; `ls internal/docclass/` → `No such file or directory`; `git branch --contains ed1be6df` → `feat/gates-doc-truth-audit` only; `git merge-base --is-ancestor ed1be6df HEAD` → non-zero ("NOT in my base history"); `git show feat/gates-doc-truth-audit:tools/docclass/banner.py` → banner form `<!-- doc-class: historical -->`.
- Build: `go build ./...` → exit 0, no output.

## Not Done (honest)

- **Not exhaustive.** Four docs were swept, not the ~1010-file devdoc corpus; the
  §4 family partition is untouched and no sibling slice's scope was re-audited.
- **No fix applied to the four docs.** The acceptance is report-only ("nothing
  submitted"); F1–F5 are each ≤5-line doc edits, deliberately left to the operator's
  disposition rather than made unilaterally in an audit branch.
- **F6 is reported, not filed.** The doc-class banner machinery is unreachable from
  `d9df443d`, so this report cannot carry a banner the gate would recognise, and I
  did not classify other docs.
- **F7 (duplicated comment line) is an adjacent observation**, not a row of this
  audit's scope; no code change was made for it.
- The `01a088f9…` children were checked only for presence in this host's ledger text;
  I did not attempt to trace whether they were cancelled, pruned or never created.
