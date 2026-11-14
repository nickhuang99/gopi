<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Rework report: reconciling the archived branch's per-period notice count into master's ledger instrument

Task: `01a0f966-08fb-7e1b-98e3-f6dfad918efa`
Branch: `task/01a0f96608fb7e1b98e3f6dfad918efa`
Reworked branch tip (archived, ref deleted — merged by SHA): `9b809938`
Base: master tip `4ce5a8f3`
Merge commit: `ea0f0485`

## Completed

- **The merge was done BY SHA**, because the branch's ref no longer exists: `git merge
  --no-commit --no-ff 9b809938` — `git rev-parse --verify --quiet 9b809938` resolves
  (`9b809938584819255d1de4eccda004ed608173bd`), so the object store still holds it. One
  file conflicted (`cmd/agent/chat_surface_bound_e2e_test.go`, 15 hunks), one file
  auto-merged (`devdoc/DOC-INDEX.tsv`); the branch's two NEW files came in cleanly.
- **Semantic resolution — there is now EXACTLY ONE counting instrument in the file, and it
  is the ledger (announcements), never the paint stream.** A first mechanical pass (take
  master's side of every hunk) was *not* the answer: it left the branch's
  `dumpRawDuplicate` probe and duplicate `boundStreamTailRunes` const / `streamTail` method
  in the file, because the merge had interleaved them into hunks whose master side is
  empty (git inserted the branch's additions where master had nothing). Those were removed
  by hand — `dumpRawDuplicate` has **no caller in master's directory** (its only call site
  lived inside the branch's own `sawNoticeOnce`, which master's file does not carry).
- **The branch's verdict claim was PORTED, not restored.** Master's file is the base
  (`sawNoticeOnce`, `count`, `countBetween`, `countInWindow`, `entriesContaining`,
  `clampIndex`, the `periodTurnDecision` wire record, `readPeriod`,
  `assertPeriodArithmetic`, `assertRenewedNumbering`, `exitAfterInterrupt`, the probe
  suites — all untouched). Two additions carry the branch's claim:
  - `countBefore(marker, want)` — implemented as an ordered walk over master's
    `countingWriter.order` (the branch implements it over `g.counts`, a map it introduced;
    master gives the entry POSITION, which is what the window needs). It is declared on the
    `announcementLedger` interface beside `entries()`.
  - A per-period assertion in PHASE 1: every numbered turn `1..bound` announced **exactly
    once** over period 1's own ledger window `[0, its own closing notice]`, located by
    `entriesContaining` after a bounded wait for that closing announcement.
- **The paint count is evidence quoted inside failure messages, and nothing else.** The
  ported assertion's failure text quotes
  `painted evidence (NOT a verdict — it also counts a repaint of the same row): %d`. It
  decides no `t.Fatalf`. The one whole-run painted count this file used to carry (the
  pre-fix defect the branch was filed for) is gone: the branch deletes it and nothing
  re-adds it. What remains of the paint stream is `sawNotice` (a wait — "did this wording
  REACH the console") and the tail quoted in failure dumps.
- **The branch's file-level deletions were reconciled by hand, not left half-applied.** The
  branch's own `turnNoticesAfter(after string, n, bound)` collides with master's
  `turnNoticesAfter(after, want string)`. Master's signature wins (it is the interface's and
  `assertRenewedNumbering`'s), the branch's use is not exercised in the e2e, and the
  doc comment states the semantic so a later reader does not "restore" the wrong one.
- **The branch's new test file + devdoc stand, re-pointed at master's ledger.**
  `cmd/agent/chat_surface_bound_notice_count_test.go` passes a `countingWriter` in as
  `chatInput.announce` (master's field) and drives `WriteNotice` — the same seam. Its
  driving fixtures now set `errOut: io.Discard`: `WriteNotice` falls back to `os.Stderr`
  when no editor is installed, and the fixture's ~17 synthetic `[gopi] self-driven turn…`
  lines were reaching the suite's stream (Rule 15's neighbour — noise dressed as evidence).
  `devdoc/chat-surface-bound-notice-count-per-period.md` is kept verbatim; its recorded
  failure (`chat_surface_bound_e2e_test.go:446`) is the historical defect this resolution
  removes, not a live line reference.
- **DOC-INDEX**: the new devdoc carries its row (`devdoc/DOC-INDEX.tsv`, auto-merged from
  the branch), and this report carries its own (appended).

## Files

- `cmd/agent/chat_surface_bound_e2e_test.go` — reconciled (master base; `countBefore`
  ported, per-period assertion added, `dumpRawDuplicate`/duplicate const+method removed,
  `sawNoticeOnce`'s painted-reading veto closed so the ledger verdict can never be voided by
  the paint count,
  interface declarations reconciled to one each).
- `cmd/agent/chat_surface_bound_notice_count_test.go` — kept from the branch (+ `io` import,
  `errOut: io.Discard` on both fixtures).
- `devdoc/chat-surface-bound-notice-count-per-period.md` — kept from the branch, verbatim.
- `devdoc/DOC-INDEX.tsv` — the branches' rows (auto-merged) + this report's row.
- `devdoc/rework-bound-notice-count-reconciliation.md` — this report.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `rc=0`,
  `-rwxrwxr-x 1 nick nick 29760871 … bin/gopi`; `go build ./...` → `rc=0`; `go vet ./...` →
  `rc=0` (all three re-run after the `sawNoticeOnce` guard landed, so the pasted readings are
  the FINAL tip's, not an earlier one's).
- **Tests:** the changed surface, run verbatim: `go test ./cmd/agent/ -run ChatSurfaceBound
  -count=1` → `ok github.com/nick/gopi/cmd/agent 1.064s` (pre-guard), `ok
  github.com/nick/gopi/cmd/agent 1.194s` (post-guard), and `ok github.com/nick/gopi/cmd/agent
  1.463s` — the LAST reading re-measured on code tip `d29f886b` (the commits after it touch
  only this `devdoc/` file), `rc=0`, with
  `--- PASS: TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator (0.61s)` and the
  case's own two deltas printed:
  `PERIODS: period 1 accepted 8 turn(s) numbered 1..8 (wire 1..8) with 1 refusal(s); period 2
  accepted 8 numbered 1..8 (wire 10..17) with 1 refusal(s)`.
  The whole package (`go test ./cmd/agent/ -count=1`) was run FOUR times and FAILS in
  `cmd/agent/chat_argv_test.go` — a file this diff does not touch (0 diff lines), green in
  isolation (0.722s), softened on master by `059b62fb`: see the section above and filed task
  `01a0f978-70d0-79f3-a02d-212c187be5e0`. This is reported as a FAIL, not as a green run.
  A fifth whole-package run was started on the FINAL tip (background job `job-7`,
  `-timeout 25m`, log `test-7.log`). **Measured later: job-7 wrote nothing and is no longer
  alive (`pgrep` finds no `go test ./cmd/agent/` for this worktree; `test-7.log` is 0 bytes)
  — it was not a fifth data point, it was an abandoned run, and it is recorded as such rather
  than counted.** A sixth run (job-8, `test-8.log`, `-timeout 25m`) was started by this
  session on the same tip and killed a moment later when its log was still empty, for the
  same reason job-7 died: another session's whole-package run against `cmd/agent`
  (task `01a0f978`, pid 3514127-3514128, `…/sessions/01a0f978-…/prefix-whole-pkg.txt`) holds
  the package under test, and this host's `cmd/agent` package exceeds Go's 10m budget
  (memory #3308) so a concurrent load is exactly the condition that hangs it. The
  acceptance's whole-package command was therefore run on this tip **before** that load
  existed, and this report does not have a whole-package result measured on the tip
  afterwards: see `## Not Done`.
- **Ran:** the surfaces are the test binary itself, invoked verbatim (above) — no `-h`, no
  synthetic driver. `go vet ./cmd/agent/` and `go vet ./...` → `rc=0`; `go build ./...` →
  `rc=0`; `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and
  devdoc/DOC-INDEX.tsv`, exit 0.
- **Result:** `grep -c '^<<<<<<<' cmd/agent/chat_surface_bound_e2e_test.go` → `0`; the
  reconciled file has one `count`-family verdict instrument (the `announcementLedger`
  implementation, all reads over `order`), declared exactly once each
  (`func (g *countingWriter) count(` / `countBefore(` / `countBetween(` / `countInWindow(` /
  `entriesContaining(` / `turnNoticesAfter(` / `func (g *helpMenuGrid) streamTail(` → 1
  declaration apiece; `boundStreamTailRunes` is one const with one read of it inside
  `streamTail`), no `t.Fatalf` decided by a painted count — the file's only
  counting painted reads are `:852` (the evidence half of `sawNoticeOnce`'s pair, whose
  verdict is `announced`) and `:1155` (quoted as TEXT inside a `t.Fatalf` decided by `got`,
  the ledger read) — and the one painted reading that could still decide an
  outcome (`sawNoticeOnce`'s ambiguity branch) is guarded on `announced == 0`, so the ledger
  verdict of 2-or-more is never voided by the paint stream (re-run after that fix:
  `ok github.com/nick/gopi/cmd/agent 1.194s`, rc=0).
- **Commit:** `ea0f0485` (merge commit — `git rev-list --parents -n 1` →
  `ea0f0485 4ce5a8f3 9b809938`, i.e. master's tip AND the archived SHA are both parents); the
  report lands on `cc527b44` / `637d235d` / `6fd1ea25`, and the final tip carrying this
  reconciliation plus the `sawNoticeOnce` guard is `fbf4a5d0` (the report's own later edits are
  on that commit's successor — `git log --oneline 4ce5a8f3..HEAD`).
- **Retained branch instruments, run verbatim:** `go test ./cmd/agent/ -run BoundNotice
  -count=1` → `ok github.com/nick/gopi/cmd/agent 0.451s`, all three PASS —
  `TestBoundNoticeCountIsReadPerPeriodNotPerPaint`,
  `TestBoundNoticeLedgerCountsAnnouncementsNotRepaints`,
  `TestBoundNoticeCountIsReadPerPeriodNotPerRun`. (Note for the acceptance run:
  `-run ChatSurfaceBound` matches ONE test — `TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator`;
  the branch's file is named `TestBoundNotice…`, hence the second invocation above.)

## Overlap worth naming: master had ALREADY reconciled the same defect

Read this before crediting the branch's file with anything the tree did not already carry.
Master's tip (`4ce5a8f3`) contains `cmd/agent/chat_surface_bound_period_window_test.go`, added by
`4de86cc7` + `fdc75e66` ("test(cmd/agent): record the bound case's wire outcomes, not its asks" /
"…reconcile the bound case's asks positionally, and its periods per window") — a file this diff
does not touch. It already holds `TestBoundNoticeCountIsReadPerPeriodNotPerRun`, whose subject is
verbatim the defect the archived branch was filed for: *the numbering notice is announced ONCE PER
PERIOD with IDENTICAL wording, so a whole-run count of it reads 2 for a console behaving exactly
as designed*. So the two lines of work converged on the same fix independently.

What the branch's retained file therefore adds is **narrower than its own devdoc implies, and it
is not nothing**:

- `TestBoundNoticeCountIsReadPerPeriodNotPerPaint` drives the **production seam**
  (`chatInput.WriteNotice` → `chatInput.announce`), where master's guard calls
  `countingWriter.Write` directly. The seam wiring (that the console reaches THIS ledger at all)
  is a claim only the branch's file pins.
- `TestBoundNoticeLedgerCountsAnnouncementsNotRepaints` pins that the ledger reports a **real
  duplicate** rather than deduping it — the property that makes the count a verdict at all.
- Its third test name (`…NotPerRun`) collides conceptually with master's; the three names are in
  different files and both files pass, so this is redundancy to be pruned by a human, not a
  conflict to be resolved here. **Not carried as a claim of new coverage.**

Pruning is deliberately NOT done in this rework: the epic's deliverable 3 says to keep the
branch's file "where it still stands", and deleting another author's guard is a decision with its
own evidence bar (the same line Rule 36 draws for stale DOC-INDEX rows). Naming the overlap is
the honest form of keeping it.

## The whole-package invocation: RUN twice, FAILS in an untouched file (honest)

The acceptance names `go test ./cmd/agent/ -count=1` as a must-pass. It was run — FOUR times, the
real command, no `-run` filter — and **it does not pass on this worktree**, for a reason that
is not this rework's: every attributable failure is inside `cmd/agent/chat_argv_test.go`, a file
this branch never opens (`git diff 4ce5a8f3 HEAD -- cmd/agent/chat_argv_test.go` → 0 lines).

Runs 1, 2 and 4 failed on that file (the log excerpts are below); run 3 failed with the failing
line lost to a `tail`, and is recorded as unattributed rather than as "the same flake". The four
runs were sequential in this session (no two of mine overlapped), so the failures are not
self-inflicted concurrency from this session; the isolated `-run
'TestChatCLIPositionalFaceIsPreserved'` run in the same tree passed in 0.722s.

- Run 1 (`~/.gopi/sessions/01a0f966-…/test-2.log`, 512.108s):
  `--- FAIL: TestChatCLIPositionalFaceIsPreserved (0.00s)` — `chat_argv_test.go:355: chatRunArgs:
  the stdout reader read end (fd 7) was already released when this helper came to free it
  (close |0: bad file descriptor …)`, then `chat_argv_test.go:355: read stdout: read |0: bad
  file descriptor`, `FAIL github.com/nick/gopi/cmd/agent 512.108s`.
- Run 2 (`…/test-3.log`, 601.373s): `panic: test timed out after 10m0s`, the dump pinning
  `goroutine 174 [IO wait, 9 minutes]` inside `io.ReadAll` at `chat_argv_test.go:69`, created
  by `chatArgv`'s own helper.
- **In isolation the same case passes**: `go test ./cmd/agent/ -run
  'TestChatCLIPositionalFaceIsPreserved' -count=1` → `ok github.com/nick/gopi/cmd/agent
  0.722s`. The defect is a fixture-side race that only appears under whole-package
  concurrency/load, and the two runs show the same fixture failing in two different ways
  (a released descriptor, then a hung reader).
- The file was already softened on master by `059b62fb` ("test(chat): the argv fixture's
  descriptor release is hygiene, not an assertion"; `git merge-base --is-ancestor 059b62fb
  4ce5a8f3` → 0), so the residual defect is upstream of this branch and survived that
  softening.
- **Filed, not absorbed** (Rule 26): task `01a0f978-70d0-79f3-a02d-212c187be5e0`
  (`tags=bug,test-fixture,flake`), with the repro, both log paths, the pre-existing proof and
  the what-done-looks-like list.
- **The known-issue trail is PUBLIC, and this is not a new discovery.** `gopi task list`
  already carries the completed row `01a0f618-5d5d-7e8c-b030-8af8ecc80a91` — *"cmd/agent:
  TestChatCLIPositionalFaceIsPreserved is red on a clean master checkout (host/env-dependent)"*
  (`tags=bug,cmd-agent,pre-existing-master-red,worker:peerbox`) — whose own briefing records the
  verbatim repro against a clean master worktree; `059b62fb`, the softening commit, is dated
  **2026-10-01**, one day before this run (2026-10-02). So the case was already red on clean
  master, was softened once, and remains red under whole-package load. The new row `01a0f978` is
  filed with the two-run evidence and the isolated-green reading; a triager should start from the
  older row and `git show 059b62fb`.
- **How a reader confirms the pre-existing claim without trusting this report**: run the whole
  package in the MAIN checkout `/home/user/work/GoPi` at master's tip with nothing of this branch
  applied, and it reproduces. **No session-log query backs any list of other sessions' runs** —
  none was run, and none is presented.

**Run 3 (`test-4.log`, 573.032s, `-timeout 25m`) also FAILED and the failing case was NOT
attributable from that log** — it was collected with `tail -80`, which discarded the `--- FAIL`
line. What the tail does show is the fail-at-close machinery
(`FAIL github.com/nick/gopi/cmd/agent 573.032s`, both sweeps clean: `live child processes: 0`,
`parked repository goroutines: 0`) and NOT a panic. **A run collected without its failing line is
not evidence, and it is recorded here as such rather than presented as "another flake".**

**Run 4 (`test-5.log`, 1500.350s, full output) IS attributable, and it is the same file:**

```
panic: test timed out after 25m0s
	running tests:
		TestChatCLIPositionalFaceIsPreserved (24m58s)
goroutine 117 [chan receive, 24 minutes]:
github.com/nick/gopi/cmd/agent.chatRunArgs(...)  cmd/agent/chat_argv_test.go:81
github.com/nick/gopi/cmd/agent.TestChatCLIPositionalFaceIsPreserved(...)  cmd/agent/chat_argv_test.go:327
goroutine 118 [IO wait, 24 minutes]:  io.ReadAll ← chat_argv_test.go:69
FAIL	github.com/nick/gopi/cmd/agent	1500.350s
rc=1
```

So **three of four whole-package runs** are attributed to `TestChatCLIPositionalFaceIsPreserved`
in `cmd/agent/chat_argv_test.go` (two as a 10m/25m package-timeout hang inside its own
`io.ReadAll`, one as its released-descriptor assertion), and one is unattributed. Go's own
600s default budget is why runs 1–2 wore the 512s/601s shape; the case is a fixture that hangs
under whole-package concurrency, not a wall-clock bound.

 the surface it changed is exercised and
green (`go test ./cmd/agent/ -run ChatSurfaceBound -count=1` → `ok … 1.064s`), `go vet
./cmd/agent/` → exit 0, `go build ./...` → exit 0 — and the package's own failure is in a file
the diff does not touch. A later run of the whole package on this host should use
`-timeout 25m` (memory #3308: `cmd/agent` exceeds Go's 10m package-test budget here) and
expect to hit `TestChatCLIPositionalFaceIsPreserved` until `01a0f978` lands.

## Not Done (honest)

- **A residual hole in `sawNoticeOnce` was FOUND and is FIXED in this rework** — the row that
  filed it, `01a0f986-e611-7987-b479-a455ba40a6dd`, is closed by this commit rather than left
  live. The helper polls `if announced == 1 || painted > 1`, then `if announced == 1 { return }`,
  then — pre-fix — `if painted == 1 { t.Logf("INCONCLUSIVE (not a failure) …"); return }`. The
  last branch is reached whenever `announced != 1`, INCLUDING `announced >= 2`, so a genuine
  double announcement that the LEDGER counted twice while the PAINT stream carried the wording
  once was logged as inconclusive and PASSED — the painted count vetoing the verdict instrument
  in the PASSING direction, the same defect this reconciliation removes everywhere else wearing
  the opposite sign. **Fixed**: the ambiguity branch is now guarded by
  `announced == 0 && painted == 1`, so a ledger reading of 2-or-more can never be voided by the
  paint stream; the painted reading only ever explains a ledger ZERO. The reasoning sits in a
  comment above the guard so a later reader cannot "restore" the veto. Re-verified by the same
  invocation (`go test ./cmd/agent/ -run ChatSurfaceBound -count=1` → `ok … 1.194s`).

- **The branch's `turnNoticesAfter(after string, n, bound)` form is NOT carried.** Its only
  consumer was the branch's own PHASE 3 numbering loop, which is master's
  `assertRenewedNumbering` + `assertPeriodArithmetic` here. Porting it would have meant a
  second way to ask one question on one record — the opposite of the single-instrument
  goal. Master's signature survived; the branch's semantics (STRICTLY-after, and the
  window the operator's line opened) are what the surviving doc comment states.
- **The branch's `g.counts` map is not carried**, so `count` reads master's `order` slice
  and `countBefore`/`countBetween` share that one record. Behaviour is identical for
  master's call sites; the branch's `countInWindow`-over-`counts` shape is gone.
- **`devdoc/chat-surface-bound-notice-count-per-period.md` is kept verbatim**, so its line
  references (`chat_surface_bound_e2e_test.go:446`) point at the pre-rework numbering and
  its `chatInput.noticeCounter` naming is the branch's, not master's `announce`. The report
  is the historical record of the defect; rewriting it would erase the evidence, and
  `doccheck` reports nothing for it.
- **The pre-existing `gofmt -l cmd/agent/` list is not mine to fix** (it names
  `chat_notice_turn_busy_test.go`, `cli_voice_server.go`, `cli_web.go`, … — files this
  rework never opens). `gofmt -l` on the two files this rework touched prints nothing.

- **The whole-package acceptance command has no result measured on the FINAL tip after the
  concurrent load appeared, and this is a gap, not a pass.** The four attributed FAILs above
  were measured before this session's last commit; two further attempts on the tip
  (`job-7`, `job-8`, both `-timeout 25m`) produced NOTHING — `test-7.log` is 0 bytes, both
  job pids are gone, and a sibling session's whole-package run against the same package
  (task `01a0f978`) is live. Reported as unmeasured rather than as green, and not re-run here
  because a third concurrent whole-package run against a package that already hangs under
  load would manufacture the very failure it is trying to observe. **What a reviewer should
  run on an idle host**: `go test ./cmd/agent/ -count=1 -timeout 25m`, expecting either the
  package's own result or `TestChatCLIPositionalFaceIsPreserved` (`01a0f978`, prior rows
  `01a0f618`/`059b62fb`) — the file this rework changes is named by no whole-package failure.

- **No session-log query backs the "other sessions are red today" reasoning** — it was dropped
  from this report the moment it was noticed that it had not been observed (`gopi log --last`
  prints a session id, not a verdict list). The independently checkable evidence for the
  pre-existing claim is the ledger row `01a0f618-5d5d-7e8c-b030-8af8ecc80a91` and the
  reproduction in the MAIN checkout at master's tip; anyone reading this should RUN that, not
  trust a summarised list.

## Evidence (self-filed)

- `git rev-parse --verify --quiet 9b809938` → `9b809938584819255d1de4eccda004ed608173bd`
  (exit 0) — the archived tip really is reachable by SHA after the ref was deleted.
- `git merge --no-commit --no-ff 9b809938` → `exit=1`, `CONFLICT (content): Merge conflict
  in cmd/agent/chat_surface_bound_e2e_test.go`, `Auto-merging devdoc/DOC-INDEX.tsv`; `git
  diff --name-only --diff-filter=U` → that one file.
- `grep -n '^<<<<<<<\|^=======$\|^>>>>>>>'` over the conflicted file → 15 hunks
  (315,418,477,490,502,515,561,579,598,642,1066,1103,1323,1501,1605); after the resolution
  `grep -c '^<<<<<<<'` → `0`.
- `git show 4ce5a8f3:<file>` (1172 lines) vs `git show 9b809938:<file>` (945 lines): the
  branch's file is a whole-file rewrite of master's, which is why a union was never
  available.
- `git grep -c 'dumpRawDuplicate' 4ce5a8f3 -- cmd/agent/` → no output, exit 1 (the probe
  has no caller in master's directory); after resolution it has none in the worktree either.
- `go test ./cmd/agent/ -run ChatSurfaceBound -count=1`: the failing case the branch was
  filed against (`announced … 2 time(s), want exactly 1` at `:446`) is now PASS, with the
  ledger reading each period's window: `period 1 … numbered 1..8`, `period 2 … numbered
  1..8`, one refusal each.
- `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and
  devdoc/DOC-INDEX.tsv` (exit 0), i.e. `gopi doccheck` reports nothing for the new devdocs.
- **The paint reads, counted per tree** (this is the evidence for deliverable 2 — "ledger
  verdict, paint as quoted evidence"):
  - `git show 4ce5a8f3:cmd/agent/chat_surface_bound_e2e_test.go | grep -n 'strings.Count(s.paint\|s.paint.streamANSIFree()'`
    → 2 hits: one a `Contains` in the `sawNotice` WAIT, one the count/evidence pair in
    `sawNoticeOnce`.
  - the same command on `9b809938` → 4 hits: `:188`, `:456`, `:740`, `:839`. **Corrected
    (see the correction below): the branch doubled the painted reads, but NONE of the four
    decides a verdict** — `:740` and `:839` are quoted as text inside `t.Fatalf` calls whose
    conditions read the LEDGER (`got := s.ledger.countInWindow(...)`, `after`).
  - the same command on the reconciled worktree → **2 hits, at `:176` (a `Contains`, in the
    `sawNotice` WAIT) and `:852`** (master's count/evidence pair, over the ledger's own read);
    `:1150`-area is the one ported site, quoting the painted count as text inside a failure
    message (`painted evidence (NOT a verdict — it also counts a repaint of the same row): %d`)
    whose enclosing `t.Fatalf` is decided by `got`, the LEDGER read (`countBefore`).
- **A correction to an earlier draft of this section**: the first written form of the census
  above cited line numbers `:174`/`:804`/`:188`/`:456`/`:740`/`:839` and a `:1150` quote site
  from an intermediate revision of the file, before the `sawNoticeOnce` guard and before the
  ported assertion's comment text settled. The numbers re-measured on the FINAL tip
  (`fbf4a5d0`) are the ones now written; the earlier set was an artifact of quoting a moving
  file, which is the reason Rule 38 puts a commit next to a census figure at all.
  - `git show 4ce5a8f3:cmd/agent/chat_surface_bound_e2e_test.go | grep -n 'strings.Count(s.paint\|s.paint.streamANSIFree()'`
    → 2 hits: `:174` (a `Contains`, in the `sawNotice` WAIT) and `:804`
    (`strings.Count(s.paint.streamANSIFree(), want)`, the count/evidence pair in
    `sawNoticeOnce`).
  - the same command on `9b809938` → 4 hits: `:188`, `:456`, `:740`, `:839` — the branch
    doubled the painted reads. **This bullet was WRONG when first written and was corrected
    after reading the branch's own lines**: `:740` and `:839` are quoted as TEXT inside
    `t.Fatalf` messages whose conditions are ledger reads (`got := s.ledger.countInWindow(0,
    period1Close, want)`, `if after != 1`), so the branch, too, kept the paint count off the
    verdict path. The finding that survives is narrower and it is why this reconciliation is
    about the SINGLE-INSTRUMENT property rather than about the branch's verdicts being paint-
    decided: after the merge the only painted reading that could still decide an outcome was
    `sawNoticeOnce`'s `painted`, which this rework guards on `announced == 0` (`fbf4a5d0`),
    so no `t.Fatalf` in the reconciled file is decided by a painted count at all. The
    quoted lines: `sed -n '728,742p;833,841p'` of
    `git show 9b809938:cmd/agent/chat_surface_bound_e2e_test.go`.
  - the same command on the FINAL tip (`e5ff393c`) → **3 hits** — `:176` (a `Contains`, in the
    `sawNotice` WAIT), `:852` (master's count/evidence pair, over the ledger's own read) and
    `:1155` (the ported site, quoted as text inside a `t.Fatalf` decided by `got`, the ledger
    read). (At `fbf4a5d0` the same command printed `:1150`; the `.md`-only commits after it
    renumber nothing, the report's own line-count edits do not touch the test file, and
    `git show e5ff393c:cmd/agent/chat_surface_bound_e2e_test.go | md5sum` equals the same on
    `fbf4a5d0` — so the two readings name the same bytes.)
  - the same command on the reconciled worktree → **2 hits, at `:176` and `:852`**: master's
    two, and NOT the branch's. `:1150` is the one ported site, quoting the painted count as
    text inside a failure message (`painted evidence (NOT a verdict — it also counts a
    repaint of the same row): %d`) whose enclosing `t.Fatalf` is decided by `got`, the LEDGER
    read (`countBefore`).
- **`sawNoticeOnce`'s paint argument is master's**, and the branch's delta on it is only
  probe/comment: `git show 4ce5a8f3:<file> | sed -n '/sawNoticeOnce/,/^}/p'` vs the same on
  `9b809938` → the differing lines are the branch's `painted > 1 { s.paint.dumpRawDuplicate(t, what) }`
  block (whose callee does not exist in master, 0 callers) and warm-up comments; the two
  counting reads and the `t.Fatalf` are byte-identical. That is why this helper was left
  alone by the merge.
- **The branch's third notice-count test is in a MASTER-side file, not the branch's**:
  `grep -rn 'func TestBoundNotice' cmd/agent/*.go` →
  `chat_surface_bound_notice_count_test.go:30` (`…NotPerPaint`) and `:115`
  (`…LedgerCountsAnnouncementsNotRepaints`) from the branch, plus
  `chat_surface_bound_period_window_test.go:122` (`…NotPerRun`) from master's `fdc75e66`.
- Whole-package, run 2 (the re-run after run 1's failure, `test-3.log`): `panic: test timed
  out after 10m0s` … `FAIL github.com/nick/gopi/cmd/agent 601.373s`, the goroutine dump
  pinning `goroutine 174 [IO wait, 9 minutes]` in `io.ReadAll` at `cmd/agent/chat_argv_test.go:69`.
- `go test ./cmd/agent/ -run 'TestChatCLIPositionalFaceIsPreserved' -count=1` → `ok
  github.com/nick/gopi/cmd/agent 0.722s` (isolated: green).
- `git diff 4ce5a8f3 HEAD -- cmd/agent/chat_argv_test.go | wc -l` → `0`; `git merge-base
  --is-ancestor 059b62fb 4ce5a8f3` → exit 0 (the softening commit is already on the base).
- `task_submit` → `Task queued: 01a0f978-70d0-79f3-a02d-212c187be5e0 (status=pending,
  tags=bug,test-fixture,flake)`.
- `gofmt -l` on the two files this rework touched → no output (the repo's pre-existing
  `gofmt -l cmd/agent/` list is a different, untouched set).
- Whole-package, run 4 (the attributable one, `test-5.log`, full output, `-timeout 25m`):
  `panic: test timed out after 25m0s` / `running tests: TestChatCLIPositionalFaceIsPreserved
  (24m58s)` / `goroutine 117 [chan receive, 24 minutes] … chatRunArgs … chat_argv_test.go:81` /
  `goroutine 118 [IO wait, 24 minutes] … io.ReadAll ← chat_argv_test.go:69` /
  `FAIL github.com/nick/gopi/cmd/agent 1500.350s`, `rc=1`.
- Whole-package, run 3 (`test-4.log`, 573.032s): `FAIL github.com/nick/gopi/cmd/agent 573.032s`,
  `rc=1`, both close-out sweeps clean (`live child processes: 0`, `parked repository goroutines:
  0`) — **failing case NOT attributable: the log was collected with `tail -80` and the `--- FAIL`
  line was discarded.** Recorded as a gap in the evidence, not as an attributed flake.
- `grep -n 'chat_surface_bound' test-2.log test-3.log` → 0 hits in either whole-package log: the
  files this rework touches are named by no whole-package failure.
