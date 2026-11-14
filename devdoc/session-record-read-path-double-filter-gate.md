<!-- doc-class: normative -->
# The five-red `cmd/agent` family: the verdict is the READ PATH, not the record

**Task**: `01a12c7a-43b2-7684-84d2-4ad6a2d5c914` · **Verdict**: DOABLE · **Host**: host-b
**Base of the investigation**: `96a4320f2` · **This worktree**: `21284bdf3`
(`task/01a12c7a43b2768484d24ad6a2d5c914`, clean)

## The one-line answer

A session record that appears to omit a user turn the model really received is,
in this family, a **reader-side** defect: `testutil.Session.UserContents()`
began returning the **operator-only** view (`ea1357381`, it applies
`sessionevent.IsHarnessInjected` internally), and five fixtures kept the OLD
semantics and re-applied the same predicate to the already-filtered slice —
a **second filter that partitions nothing**. The record was correct throughout.

The fix landed upstream and **is inherited by this worktree's HEAD**; this run
proves that inheritance, re-verifies all five individually on the clean tree, and
adds the gate that stops the shape returning a third time.

## Completed

1. **Settled which side is stale, from evidence** — the write side is correct at
   `96a4320f2` itself; the read path manufactured the zero.
2. **Re-verified all five fixtures individually** on the clean HEAD tree, each
   with `-run`, raw output retained under the session artifacts directory.
3. **Named the record's contract** at constant/function level (below), so the
   next reader does not have to re-derive it.
4. **Added the anti-regression gate** — `internal/testutil/session_view_filter_gate_test.go`
   (`TestSessionViewIsNeverFilteredASecondTime` + `TestSessionViewGateHasTeeth`),
   with a real-tree red→green proof.
5. **Classified this document** in `devdoc/DOC-INDEX.tsv` in the same commit
   (Rule 36).

## The evidence chain — write side vs read side

### 1. The read-path line, at the base, verbatim

`git show 96a4320f2:cmd/agent/cli_lineedit_test.go` (lines 95–100):

```go
	var operator, injected []string
	for _, u := range sess.UserContents() {
		if sessionevent.IsHarnessInjected(u) {
			injected = append(injected, u)
			continue
```

`sess.UserContents()` is already the operator-only view at that commit, so
`injected` can only ever be empty — while the assertion that reads it says:

```go
	if len(injected) != requests {
		t.Errorf("harness turn stamps = %d, model requests = %d — want exactly one stamp per request (stamps: %v)", ...)
	}
```

That is the exact failure string the bug report quotes (`harness turn stamps = 0,
model requests = 2`). **The two numbers disagree because the read path lost one
half, not because the run wrote nothing.**

### 2. The writer was never the suspect — it is pinned by its own passing tests

The record's user rows come from `(*agent.Agent).turnStampLine`
(`internal/agent/clock.go`), which mints the stamp AND calls
`a.SessionLog.LogUser(...)` in the same function — one stamp per assembled
request, and `requestMessages` is its only caller
(`internal/agent/clock.go:237`, `internal/agent/loop.go:4418`, `:5016`,
`internal/agent/chat_ops.go:623`). Its own green guards:
`internal/agent/chat_turn_clock_test.go` and
`internal/agent/sessionbudget_everyround_test.go` (`stamps == requests`). A
behaviour with its own passing pins is contract; the fixture reading it with the
old semantics is the stale half.

### 2b. The write side, measured in the base checkouts' OWN record (this run)

The verdict does not rest on reading the source. In a detached worktree at
`96a4320f2` (`dirty=0`) I instrumented the base fixture's own read path to dump
the record it was reading — the SAME record the failing assertion reads:

```
PROBE raw user rows = 4 (Counts[user]=4, requests=2)
PROBE raw row 0: "hello there"
PROBE raw row 1: "[system] [t+0s] — first request of this session."
PROBE raw row 2: "what is two plus two"
PROBE raw row 3: "[system] [t+0s] — +0s since your previous request."
harness turn stamps = 0, model requests = 2 — want exactly one stamp per request (stamps: [])
```

**The record holds the two stamps the run really wrote; the read path returns
zero of them.** `Counts()["user"]=4` (raw census, 2 operator + 2 harness) while
`len(injected)=0` in the same function over the same record — the second filter
in one print-out. That is the whole family in four lines, and it is why the
"write side" reading is dead: nothing is missing from the file.

(Raw log: `artifacts/base-write-side-probe.out`. The instrumented copy was taken
from `artifacts/base-cli_lineedit-probe-backup.go` and restored; the probe
worktree's `git status --porcelain` printed nothing afterwards.)

### 3. The fixes exist, and HEAD has them

Every commit that settles this family is an **ancestor of HEAD** (measured with
`git merge-base --is-ancestor`, exit 0 for each):

| commit | date | what it settles |
|---|---|---|
| `ea1357381` | 2026-10-12 | `UserContents` reports the OPERATOR's turns, not harness rows |
| `7ae446e47` | 2026-10-12 | the A4 census reads the unfiltered view |
| `6ab91facd` | 2026-10-12 | restate the A4 non-TTY fixture for the stamp + prefetch sidecar |
| `839093ad0` | 2026-10-12 | the idle-notify record check reads the harness view |
| `9867ed461` | 2026-10-12 | **the two notify-DELIVER fixtures** read the harness view, not the operator's |
| `4cec86abb` | 2026-10-12 | the notify-duplicate fixture counts the delivered row in the harness view |
| `6c06db641` | 2026-10-12 | three fixtures read the host's own answer, not the code's — **this one carries the RP-1 prefix restatement** |

`git merge-base --is-ancestor 96a4320f2666819a521f6c7288eda894a2a8768b HEAD` →
**exit 0** (the investigation's base is inside HEAD's history) and
`git merge-base --is-ancestor master HEAD` → **exit 0** (HEAD is a descendant of
master, not a stale branch).

**But "older than the repair" is too loose, and this run measured why.** The
chain is `master a3a31da42` ⊂ `96a4320f2` ⊂ `HEAD` (measured: each
`--is-ancestor` exit 0 downward, exit 1 upward), and the family does **not**
divide cleanly at either point:

| rev | `UserContents()` (the view) | the fixtures | verdict |
|---|---|---|---|
| `master a3a31da42` | operator-only (`ev.IsHarnessUser()` applied inside) | **old shape** (`sess.UserContents()` re-tested, `:50/:53/:60`) | **RED — this run reproduced `:50/:53/:60` here** |
| `96a4320f2` (the base) | operator-only, byte-identical to master's | new shape (`AllUserContents()` + `IsHarnessInjected`, `:113/:116`) | RED — 0 of 2 stamps seen |
| `HEAD` | same | same | **GREEN** |

So the repairs are **two different halves landing at two different times**:
`ea1357381` (the VIEW becomes operator-only) is already **inside the base**
(`--is-ancestor ea1357381 96a4320f2` → exit 0), while the fixture restatements
(`7ae446e47`, `839093ad0`, `4cec86abb`, `6c06db641`, `86a6998d6`) are **not**
(exit 1 for each). The base is therefore red because its fixture was already
restated against the new view but the *assertion under investigation* was not —
it belongs to a branch the merge base does not carry.

### 4. The fifth assertion, and why it is the same verdict (not a product bug)

`TestRunPathPlainPromptIsVerbatim` was red for a different-looking reason:
`user events = 4, want 2`-class readings, and an equality assertion
(`readings[0].First == rp1PromptToken`) against a wire that legitimately carries
`<prompt><turn stamp>`. Its restatement (`6c06db641`, which lands the RP-1
prefix anchor at `cli_runpath_boundary_rp1_test.go:286`; the A4 sibling is
`6ab91facd`, a different file) keeps the RP-1 claim at full strength as a
**prefix**, which is strictly stronger than the `Contains` the RP-1 defect
would have slipped past:

```go
	if got := readings[0].First; !strings.HasPrefix(got, rp1PromptToken) {
```

This half is a **fixture-contract restatement agreed with its own sibling
decision** — the product's behaviour (append the every-round stamp) is documented
and pinned elsewhere, so no product code was changed to make it pass.

## The record's contract — named, not paraphrased

| what | whose name decides it |
|---|---|
| the marker of a harness-injected user row | `sessionevent.HarnessMessagePrefix` (`"[system] "`), predicate `sessionevent.IsHarnessInjected` — the alias `harnessMessagePrefix` in `internal/agent/context.go:75` is the SAME value, and `harness_marker_shared_test.go` fails the build if they drift |
| the OPERATOR's turns | `testutil.Session.UserContents()` (predicate already applied inside) |
| the harness half | `testutil.Session.HarnessUserContents()` |
| every user row, unfiltered | `testutil.Session.AllUserContents()` — the view a fixture MUST range over when it wants both halves |
| per-EVENT split | `testutil.Event.IsHarnessUser()` (via `AllUserContents`) |
| the stamp's mint + its record write | `(*agent.Agent).turnStampLine` (`internal/agent/clock.go`), formats `turnStampFirstFmt` / `turnStampGapFmt` built from `turnStampFirstSuffix` / `turnStampGapSuffix` |
| the sidecar beside the record | `config.MemoryPrefetchRecordName` (`memory-prefetch.jsonl`), opened by `cmd/agent/chat_prefetch.go`; registered as a side file so `LoadSession` counts **one** session |

The contract in one sentence: **`Counts()` stays the RAW census, `UserContents`
is the operator's, and the two-halves view is `HarnessUserContents` /
`AllUserContents`.** A fixture asserting "the model was told X, and the record
shows it" must read the harness view — never the operator view filtered again.

## The anti-regression gate

`internal/testutil/session_view_filter_gate_test.go` — stdlib `go/parser` only
(Rule 35: zero new dependency, no type information, nothing to build).

- **Both spellings** are reported: a range directly over the view call, and the
  view bound to a variable first (one hop). The second is what a call-only gate
  would wave through.
- **The sets are DERIVED, never spelled** (Rule 14): the filtered-view set comes
  from the methods that define it — a `[]string` reader on `*Session` whose body
  already calls a harness predicate — so `UserContents` **and**
  `HarnessUserContents` are covered while `AllUserContents` is deliberately NOT
  (splitting the unfiltered view is the FIX). The predicate set is derived from
  the files naming `IsHarness*`.
- **A read floor** makes a walk that saw no tree FAIL rather than read green
  (Rule 15). Measured: 24 view call sites green, 25 with the pre-fix shape
  restored (the pre-fix range named a view inside the derived set; the fix names
  `AllUserContents`, which is not in it). The floor sits below both.
- **Teeth**: `TestSessionViewGateHasTeeth` drives the same scan over six
  synthetic sources — the shipped shape (both arms) must be reported, the
  legitimate unfiltered split and a re-test-free range must not be, and an escape
  marker without a reason is still a violation.

### Real-tree red → green proof

Restoring the shipped-before shape in a REAL fixture
(`cmd/agent/cli_lineedit_test.go:105`, `AllUserContents` → `UserContents`):

```
--- FAIL: TestSessionViewIsNeverFilteredASecondTime (0.11s)
    session_view_filter_gate_test.go:194: cmd/agent/cli_lineedit_test.go:105 ranges over UserContents() and re-tests each element with IsHarnessInjected — UserContents() has ALREADY applied the harness predicate ...
    session_view_filter_gate_test.go:197: scanned 1652 test files, 25 view call sites, 1 re-filter site(s)
FAIL
```

Restored to the committed state:

```
ok  	github.com/nick/gopi/internal/testutil	2.063s
```

Raw logs: `artifacts/gate-red-proof.out`, `artifacts/gate-green-proof.out`.
The file was copied to `artifacts/cli_lineedit_test.go.backup` **before** the
patch and restored from it; `git status --porcelain` after restore lists only the
new gate file.

## Measured vs inferred

**MEASURED (this run, commands and outputs in `## Verification` below):**

- all five fixtures **PASS** individually on the clean HEAD tree, each with `-run`;
- the whole five in one `-run` set: `ok github.com/nick/gopi/cmd/agent 108.248s`;
- **the base red, reproduced by this run** in a detached worktree at `96a4320f2`
  (`dirty=0`): `harness turn stamps = 0, model requests = 2` at
  `cli_lineedit_test.go:113`;
- **the write side, measured in that base tree's own record** (probe above):
  4 raw user rows, `Counts()["user"]=4`, `requests=2`, 2 of them stamps —
  while the same function's `injected` slice is empty;
- the read-path line verbatim at `96a4320f2`, in that commit's own blob;
- `96a4320f2` and `master a3a31da42` are both ancestors of HEAD (and
  `master a3a31da42` is an ancestor of `96a4320f2`);
- **all five are RED at the base `96a4320f2`, reproduced first-hand by this run**
  in a detached worktree at that rev (`dirty=[]` before and after; its own code):
  5/5 `--- FAIL`, each at the family's line —
  `cli_lineedit_test.go:113` `harness turn stamps = 0, model requests = 2`,
  `cli_lineedit_test.go:116` `the run wrote no turn stamp at all`,
  `cli_runpath_boundary_rp1_test.go:246` `the FIRST user message is not the
  prompt verbatim`,
  `cli_notify_deliver_test.go:329` / `cli_notify_deliver_idle_test.go:200` `the
  session record carries no user turn with the delivered message`,
  `cli_notify_duplicate_test.go:165` `carries 0 user message(s) with the
  delivered text, want exactly 1`. Archived:
  `artifacts/base-96a4320f2-five-reds.out`;
- **`master a3a31da42` is RED, reproduced first-hand by this run** in a detached
  worktree at that rev (`dirty=0`): `--- FAIL: TestChatCookedFallback_NonTTY`
  printing `cli_lineedit_test.go:50`, `:53` (`session: user events = 4, want 2`)
  and `:60` — the very line numbers the source report could not place. The four
  notify fixtures and the RP-1 fixture are restated in the same commits as that
  one (`git merge-base --is-ancestor <fix> a3a31da42` → exit 1 for **all eight**
  settling commits, `ea1357381` included), so the same rev carries their old
  shape too;
- `ea1357381` (the VIEW becomes operator-only) is **already inside the base**
  (`--is-ancestor ea1357381 96a4320f2` → exit 0), while the fixture restatements
  are not (exit 1 each) — the two halves landed at different times;
- `turnStampLine` is the only mint and it logs what it sends, with its own green pins;
- the gate is green on the real tree with a read floor met, and red by name when
  the shape is restored.

**INFERRED (stated as inference, not used as a judgement):**

- the stale-shape **mechanism** attribution in the three notify fixtures — i.e.
  that each of them reports its failure *because* it ranges over the
  operator-only view. The base run above MEASURED that they fail at exactly
  those record-check lines (`cli_notify_deliver_test.go:329`,
  `cli_notify_deliver_idle_test.go:200`, `cli_notify_duplicate_test.go:165`), and
  the base blob shows each of them ranging `sess.UserContents()`; that the
  delivered row is a harness-injected one (so it is the operator view that hides
  it) is read from the fix's own comment (`9867ed461`: "the delivered sentence is
  recorded … `sessionMessageFact` prefixes it") rather than re-derived here.

**NO LONGER UNEXPLAINED — this run placed `cli_lineedit_test.go:50/53/60`**
(the source report flagged them as matching no tree it saw). They are the
**`master a3a31da42` fixture**, reproduced above: `:53` is its
`sess.AssertCounts(t, map[string]int{… "user": 2 …})`, and `Counts()` is the RAW
census, so the extra two `[system]` rows make the helper print exactly
`session: user events = 4, want 2 (types: [session_start user user assistant
request user user assistant request session_end])`; `:60` is that file's
`t.Errorf("user messages = %v, want the two piped turns", got)`. The `:50/53/60`
figures and the base's `:113/116` are therefore **the same defect at two
different line numbers on two different revs** — the old-shape and the
partially-restated shape — and not a third, unexplained tree. Archived:
`artifacts/master-a3a31da42-is-red-505360.out`.

## Not Done

- **Every fixture in the family was re-run at the base by this run** (5/5 FAIL,
  archived), so the base half is measured, not inherited. The *master* half is
  measured too (the A4 fixture at `a3a31da42`, `:50/:53/:60`). What remains
  inherited-and-verified rather than re-authored: the restatements themselves —
  this run confirms they are ancestors of HEAD and reads each one's hunks.
- The sibling branch `task/01a12c77…` (commits `222b42aa8`, `f7aa86c6a`) carries a
  **complementary** guard: an in-fixture identity assertion (raw user-row census
  == operator + stamps) so the specific fixture names its own double filter. That
  is not duplicated here — this gate is the repo-wide form and covers fixtures
  that do not exist yet. Both are worth landing; if they collide in
  `cmd/agent/cli_lineedit_test.go`, keep the stronger arm, not both spellings.
- `TestRunPathPlainPromptIsVerbatim`'s restatement is inherited from HEAD, not
  authored here; this run verifies it, it does not re-decide it.

## Verification

- **Build**: `go build ./...` → OK (before the commit).
- **Tests**: `go vet ./internal/testutil/` → clean;
  `go test ./internal/testutil/ -count=1 -run 'TestSessionViewIsNeverFilteredASecondTime|TestSessionViewGateHasTeeth'`
  → `ok github.com/nick/gopi/internal/testutil` (gate green, 24 view call sites,
  0 re-filter sites; teeth 6/6 PASS);
  `go test ./internal/testutil/ -count=1 -run 'TestSession'` →
  `ok github.com/nick/gopi/internal/testutil 0.780s` (the existing session
  accounting fixtures still pass);
  all five `cmd/agent` fixtures in one set →
  `ok github.com/nick/gopi/cmd/agent 108.248s`.
- **Ran** (verbatim, on the clean HEAD tree `21284bdf3`):

```
$ for n in TestChatCookedFallback_NonTTY TestRunPathPlainPromptIsVerbatim \
        TestNotifyDeliverReachesARunningChatSession \
        TestNotifyDeliverStatesTheMessageOnceInTheSession \
        TestNotifyDeliverWakesAnIdleChatSessionWithNoInput; do
      go test ./cmd/agent/ -count=1 -v -run "^$n$"; done
```

- **Result** — each of the five, individually, on the clean tree:

```
--- PASS: TestChatCookedFallback_NonTTY (1.44s)                  ok  cmd/agent 2.860s
--- PASS: TestRunPathPlainPromptIsVerbatim (5.42s)               ok  cmd/agent 6.849s
--- PASS: TestNotifyDeliverReachesARunningChatSession (31.62s)    ok  cmd/agent 33.123s
--- PASS: TestNotifyDeliverStatesTheMessageOnceInTheSession (33.40s) ok cmd/agent 35.453s
--- PASS: TestNotifyDeliverWakesAnIdleChatSessionWithNoInput (33.53s) ok cmd/agent 34.406s
```

- **The base red, reproduced by this run** (detached worktree at `96a4320f2`,
  `dirty=0`, that tree's own code):

```
--- FAIL: TestChatCookedFallback_NonTTY (12.27s)
    cli_lineedit_test.go:113: harness turn stamps = 0, model requests = 2 — want exactly one stamp per request (stamps: [])
    cli_lineedit_test.go:116: the run wrote no turn stamp at all — every request carries one (internal/agent/clock.go)
FAIL	github.com/nick/gopi/cmd/agent	14.375s
```

- **The master red, and the `:50/53/60` placement — measured this run**
  (detached worktree at `a3a31da42`, `dirty=0`, that rev's own code;
  archived as `artifacts/master-a3a31da42-is-red-505360.out`):

```
$ go test ./cmd/agent/ -count=1 -v -run '^TestChatCookedFallback_NonTTY$'
=== RUN   TestChatCookedFallback_NonTTY
    cli_lineedit_test.go:50: session: … carries side file(s) [memory-prefetch.jsonl (unknown .jsonl)] — same session, not a second one
    cli_lineedit_test.go:53: session: user events = 4, want 2 (types: [session_start user user assistant request user user assistant request session_end])
    cli_lineedit_test.go:60: user messages = [hello there [system] [t+0s] — first request of this session. what is two plus two [system] [t+0s] — +0s since your previous request.], want the two piped turns
--- FAIL: TestChatCookedFallback_NonTTY (1.30s)
FAIL	github.com/nick/gopi/cmd/agent	3.377s
```

- **The five, archived verbatim this run** — `artifacts/five-run-archive-finaltip.out`
  (loop over the five names, `-count=1 -v -run ^<name>$`, header naming
  `HEAD dbc459804` and the host, and a trailing `git status --porcelain` that
  printed nothing; the earlier `artifacts/five-run-archive.out` was taken at
  `b9295e780` — the same tree for `cmd/agent/`, whose four fixture files the
  intervening commit did not touch). At the **delivery tip** the same loop was
  re-run as `artifacts/five-run-archive-tip-c01cfcc35.out`, where the names are
  **resolved from the source** and echoed before the run and `[no tests to run]`
  is counted — a mistyped `-run` prints `ok … [no tests to run]` with no
  `--- PASS` line and reads exactly like success; that archive is the one whose
  green is load-bearing:

```
--- PASS: TestChatCookedFallback_NonTTY (1.66s)                    ok  cmd/agent 5.395s
--- PASS: TestRunPathPlainPromptIsVerbatim (2.45s)                 ok  cmd/agent 5.632s
--- PASS: TestNotifyDeliverReachesARunningChatSession (31.55s)     ok  cmd/agent 32.371s
--- PASS: TestNotifyDeliverStatesTheMessageOnceInTheSession (33.51s) ok cmd/agent 36.972s
--- PASS: TestNotifyDeliverWakesAnIdleChatSessionWithNoInput (33.56s) ok cmd/agent 37.705s
```

- **The gate, re-run first-hand this run on the committed tree**:
  `go test ./internal/testutil/ -count=1 -v -run 'TestSessionViewIsNeverFilteredASecondTime|TestSessionViewGateHasTeeth'`
  → `ok github.com/nick/gopi/internal/testutil 3.148s`, with
  `views derived from internal/testutil/session.go: [HarnessUserContents UserContents]`
  printed and all six teeth sub-cases passing (arm 1, arm 2, the two legitimate
  shapes, the documented escape, and an escape without a reason).
- **The declared entry point, this run**:
  `make test-budget PKGS='./internal/testutil'` → exit 0.
- **Rule 36 / doccheck, this run**: `gopi tool doccheck` →
  *"the devdoc corpus is consistent — every file under devdoc/ has a well-formed
  row"*, exit 0; `go test ./internal/standards/ -run 'DocAnchor|DocClass|DocIndex|DocManifest'`
  → `ok github.com/nick/gopi/internal/standards 3.554s`.

- **The write side, measured in that tree's own record** — the probe quoted in
  §2b (`artifacts/base-write-side-probe.out`): 4 raw user rows, 2 of them
  stamps, `requests=2`, `injected` empty.

- **Rule 31 (the surface this run changed)**: the changed surface is a test gate,
  so its execution IS its red proof, run for real against the real tree:

```
$ sed -i 's|range sess.AllUserContents()|range sess.UserContents()|' cmd/agent/cli_lineedit_test.go
$ go test ./internal/testutil/ -count=1 -run TestSessionViewIsNeverFilteredASecondTime
--- FAIL: TestSessionViewIsNeverFilteredASecondTime (0.11s)
    session_view_filter_gate_test.go:194: cmd/agent/cli_lineedit_test.go:105 ranges over UserContents() and re-tests each element with IsHarnessInjected — UserContents() has ALREADY applied the harness predicate ...
    session_view_filter_gate_test.go:197: scanned 1652 test files, 25 view call sites, 1 re-filter site(s)
FAIL
$ cp artifacts/cli_lineedit_test.go.backup cmd/agent/cli_lineedit_test.go
$ go test ./internal/testutil/ -count=1 -run TestSessionViewIsNeverFilteredASecondTime
ok  	github.com/nick/gopi/internal/testutil	2.063s
```

- **Rule 30**: `git status --porcelain` prints nothing in this worktree after the
  final commit.
- **Commit**: `a880e1c9c test(testutil): a fixture must not filter an already-filtered session view`

### Evidence (self-filed)

- base red: `/home/user/workspace/gopi-stage2-base-96a4320f2` (detached at
  `96a4320f2`, `dirty=0`) →
  `cli_lineedit_test.go:113: harness turn stamps = 0, model requests = 2`, `FAIL`.
- write side at base: the same tree, instrumented read path →
  `PROBE raw user rows = 4 (Counts[user]=4, requests=2)` with rows
  `"hello there"` / `"[system] [t+0s] — first request of this session."` /
  `"what is two plus two"` / `"[system] [t+0s] — +0s since your previous request."`.
- gate red on the real tree: `1 re-filter site(s)` naming
  `cmd/agent/cli_lineedit_test.go:105`; gate green after restore: `ok … 2.063s`.
- ancestry: `git merge-base --is-ancestor 96a4320f2… HEAD` → exit 0;
  `git merge-base --is-ancestor master HEAD` → exit 0.
- five green individually at HEAD: the five `--- PASS` lines above.

## Files

- `internal/testutil/session_view_filter_gate_test.go` (new, this run)
- `devdoc/session-record-read-path-double-filter-gate.md` (this document)
- `devdoc/DOC-INDEX.tsv` (classification row, this run)
- Inherited and verified, not modified: `internal/testutil/session.go`,
  `cmd/agent/cli_lineedit_test.go`, `cmd/agent/cli_notify_deliver_test.go`,
  `cmd/agent/cli_notify_deliver_idle_test.go`,
  `cmd/agent/cli_notify_duplicate_test.go`,
  `cmd/agent/cli_runpath_boundary_rp1_test.go`
