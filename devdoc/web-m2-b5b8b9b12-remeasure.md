<!-- doc-class: normative -->
# Re-measure: `archive/feat/web-m2-b5b8b9b12` — per-unit SALVAGE vs OBSOLETE

**Subject tag:** `archive/feat/web-m2-b5b8b9b12`
**Tip:** `98fb29425453b3fef141e5d5d2e94234b03d8e41` — `docs(web): state the master fail-set baseline in the M2 report verification` (2026-09-16)
**Audit's claim being re-measured:** UNLANDED (cherry +12 / rev-list 19)
**Measured against:** master `fa12d97f` (2026-09-19)
**Method:** read-only archaeology (`git show`/`git log -S`/`git grep`/blob identity); Rule 23 respected — the tag was never checked out, branched from, or committed to. This run's own work happens on `fix/web-m2-b5b8b9b12-verdict`, a fresh worktree off master.

---

## 1. Instruments — observed output (verbatim)

```
$ git merge-base master archive/feat/web-m2-b5b8b9b12
c6abf8b9a5c9796611262da8263d01b27ddb5fd5

$ git merge-base --is-ancestor archive/feat/web-m2-b5b8b9b12 master ; echo $?
1

$ git rev-list --count master..archive/feat/web-m2-b5b8b9b12
19

$ git cherry -v master archive/feat/web-m2-b5b8b9b12
- a979ac9d99acd9a9338a93e66aa2923f35b9a74d feat(web): B1 shared-table /new conversation reset command
- 5b51392d742707f7dc90e144ca3802216d91e0ef feat(chat): /new is the same reset as /clear (one behaviour, two spellings)
- 456f6a29573883284317d22ae57995a022f89300 feat(web): B1 reset endpoint, B2 pre-lock queued frame, B3 follow-threshold gate
- 21e19b21b4f976152ad03f578d0f7d8013c512b6 test(web): B1/B2/B3 regression tests (reset amnesia, queued frame under held lock, follow gate)
- 5e47bb76b7f2384b215ade2f0d970b6e4cd4af29 docs(web): M1 B1/B2/B3 report + roadmap clarifications
- d4e7b32640b4ac0f3858ed997a966d460e29f836 docs(roadmap): mark M1.4/M1.5 shipped and clarify the s.mu TryLock clause
+ 76b4a8db767546c98fa20c93d6a52bc2c1d2571b feat(agent): one tool-digest rule — export ToolCallSummary, add ToolResultDigest
+ 51b0166c83a05ede335cf126dd9c0018d089be1a feat(web): B4 per-turn timeout, B5 replayed tool rows, B6 one image budget, B7 composer guards
+ 354c016fbb51f15bec014bf3e608fcfcdef93345 test(web): B4/B5/B6/B7 regression tests (in-flight timeout, tool-row replay, one image budget, composer guards)
+ 9a2152bad707e7ff9ceb30d415627968482bf4ec docs(web): M1.5/M2.3 B4-B7 report + roadmap rows marked shipped
+ 80e582be3dc3f83bbf20699bc89b5ef92a4d21ed docs(web): record the B7 DOM-shim behavioural evidence (12/12 assertions)
+ 88cb29fa5c7e34ba51b8e63518e39a0d33dd772c docs(web): independent M1.5/M2.3 re-verification — master-vs-branch gate diffs, red-proofs re-run, live evidence, session tally
+ 9ebc955489c71d9ac51e2ef7e998340b85db702f docs(web): closure session — master merged in, third red-proof pass, fresh live evidence (queued frame first, reset amnesia, tool replay, single-session tally)
+ cecf0d978dd70880c0d3b581ddbf1524b90e0721 feat(web): B8 markdown, B9 input-trigger menus, B12 visible turn queue + steering
+ 127d51b6d89aa90a143441a5e2f26d793c1f5ac4 docs(web): M2 depth report — B5/B8/B9/B12 (verification, red-proof table, honest limits)
+ bb4623235406238123abae00828de36260bbfbe8 docs(web): record the worktree-residue false FAIL in the M2 report verification
+ e6c4dcdb59ba82beb6938236486462b21a63342a docs(web): M2 verification — fail-set comparison proves zero branch regressions (11=11 vs master)
+ 98fb29425453b3fef141e5d5d2e94234b03d8e41 docs(web): state the master fail-set baseline in the M2 report verification
```

**12 `+` / 6 `-`, rev-list 19.** The three numbers reconcile exactly, and the gap is *not* an audit error:
12 (`+`, unlanded by patch-id) + 6 (`-`, already on master by patch-id) + **1 internal merge** = 19.
The internal merge is `15229754 Merge commit 'c6abf8b9' into feat/web-m1-b4b7` (`git cherry` never lists merges), whose date (2026-09-15) sits between the `-` and `+` blocks — the tag merged its own base in mid-run.

Tag footprint: `git diff --stat c6abf8b9...98fb2942` → **14 files, +4570 / −77**, in five unit families:
`cmd/agent/cli_web.go`, `cmd/agent/{cli_web_m1_test,cli_web_m2_test,cli_web_m2b_test}.go`, the two page JS harness files, `internal/agent/chat_ops.go`, `cmd/agent/{cli_chat,commands}.go`, four devdocs, `.live/verify.py`.

### Sibling tags (same branch prefix)

| tag | tip | cherry `+` | rev-list | relation to master |
|---|---|---|---|---|
| `archive/feat/web-m2-b5b8b9b12` | `98fb2942` | 12 | 19 | not an ancestor (`--is-ancestor` → 1) |
| `archive/feat/web-m2-b5b8b9b12-20260917` | `98fb2942` | 12 | 19 | same tip, dated copy |
| **`archive/feat/web-m2-rework`** | `ee5e0a07` | **0** | **0** | **ancestor of master** (`--is-ancestor` → 0) |

The third tag is the decisive find: it is the **rework twin** — the same B-increment work, re-derived off master by another run, merged in, and archived only as a tag.

---

## 2. The decisive structural finding (why the audit's UNLANDED verdict is not the issue)

Master did **not** miss the tag's work; master received **the same increments by another path**, and it also carries this tag's documents verbatim as references:

```
$ git log --oneline master --ancestry-path c0c722e3..master | tail -1
a392310c Merge branch 'feat/web-m2-rework'
$ git log -1 --format='%h %ad %s' --date=short c0c722e3
c0c722e3 2026-09-16 feat(web): B1/B2/B3/B5/B8/B9/B12 rebuilt off master — transcript tool rows, input-trigger menus, per-turn budget, queued/steering turns
```

`c0c722e3` is an ancestor of master (verified `git merge-base --is-ancestor c0c722e3 master` → 0), landed via merge `a392310c`, preceded by `26406a67 docs(web): devdoc for the M2 rework + verification evidence` and `25c5dd47 docs(web): task report — what landed, what did not, baseline-vs-delta`.

**The tag's own documents and test files were rescued into master's `.ref/` reference tree** by the rework run's rescue commit:

```
$ git log --oneline -2 master -- .ref/m2_test.go
ee5e0a07 wip(feat/web-m2-rework): rescue uncommitted work before stopping the workers (PA)
```

Blob identity (`git rev-parse master:.ref/<p>` vs `git rev-parse <tag>:devdoc/<p>`):

| tag path | master path | blob | identity |
|---|---|---|---|
| `devdoc/web-m1-b4b7.md` | `.ref/web-m1-b4b7.md` | `58d455ad` | **IDENTICAL** |
| `devdoc/m2-web-m2.md` | `.ref/m2-web-m2.md` | `8d4ae9c3` | **IDENTICAL** |
| `devdoc/web-m1-b1b2b3.md` | `.ref/web-m1-b1b2b3.md` | `6aee9b69` | **IDENTICAL** |
| `cmd/agent/cli_web_m2_test.go` | `.ref/m2_test.go` | `11183698` | **IDENTICAL** |
| `cmd/agent/cli_web_m2b_test.go` | `.ref/m2b_test.go` | `e7b94041` | **IDENTICAL** |
| `cmd/agent/cli_web_m1_test.go` | `.ref/m1_test.go` | `14e36bbb` | **IDENTICAL** |
| `cmd/agent/cli_web_page_harness.js` | `.ref/page_harness.js` | `8a90beaa` | **IDENTICAL** |
| `cmd/agent/cli_web_page_scenarios.js` | `.ref/page_scenarios.js` | `f196f8ee` | **IDENTICAL** |

(The `.ref/` copies are the tag's own files verbatim. Master's *live* `cmd/agent/cli_web_m1_test.go` is a later revision of the same test — blob `624087b2`, one hunk relaxed so B1's page clear is accepted through the rework's shared `clearNode(logEl)` helper as well as the direct `logEl.innerHTML=''`; that is the rework editing its ancestor, not a divergence in the unit.)

Master's `devdoc/web-m2-rework.md` states the same thing from the author's side: the archived branch was *"read as GUIDANCE only (no merge/rebase/commit on it — Rule 23)"*, and that master's `cli_web.go` differed from the reference by 949/234 lines **because master had gained `feat/web-local-trust`** — i.e. that is why the work is a rework and not a cherry-pick.

**The generalizable point:** an archived multi-unit tag can audit UNLANDED while being *fully superseded content-wise*, if the same units were re-derived off master and the originals were rescued as reference copies. The instrument cannot see that; only a per-unit content test can.

---

## 3. Per-unit verdicts (CONTENT test, never the audit's list)

`master:` = present on master today. Each row is a unit the tag's own commit messages name.

| # | unit (tag commits) | content test on master | verdict | evidence |
|---|---|---|---|---|
| U1 | **B1** reset endpoint `/api/chat/reset` (`456f6a29`) | `webChatResetPath`, `webResetBodyMessage`, `handleChatReset`, `resetConversationLocked`, `serveResetCommand`, `webResetOutput` all present | **OBSOLETE** | landed by older master commit `68642cb7 feat(web): B1 reset endpoint, B2 pre-lock queued frame, B3 follow-threshold gate` (`git log -S'webChatResetPath' master -- cmd/agent/cli_web.go`); tests on master `TestWebM1B1_SlashNewResetsUnderLockViaSharedCommandTable` |
| U2 | **B1'** `/new` = same reset as `/clear` (`a979ac9d`, `5b51392d`) | `cmd/agent/commands.go:704 const webClearCommandName = "new"`, `:713 webClearAliasName = "clear"`, table entry at `:810`; REPL `case "/" + webClearAliasName, "/" + webClearCommandName:` (`cli_chat.go:1750`); help line `"  /clear   forget the conversation and start fresh (also /new)"` (`cli_chat.go:54`) | **OBSOLETE** | master carries the tag's exact end state; master even generalised it (the tag's literal `case "/clear", "/" + webClearCommandName:` became constants) |
| U3 | **B2** pre-lock queued frame (`456f6a29`) | `webQueuedPayload`, `webBusyWaitNotice`, `writeSSEHeaders`, `writeSSEFrame`, `webPlacementQueued` present; `TestWebM1B2_SecondConcurrentPostGetsQueuedFrameWhileFirstHoldsLock` | **OBSOLETE** | landed `68642cb7`; tag's own roadmap row on master marks M1.5 SHIPPED |
| U4 | **B3** follow-threshold gate (`456f6a29`) | `webFollowThreshold = 24` (server) + page `FOLLOW_THRESHOLD=24`; `TestWebM1B3_FollowThresholdGatesStreamScrollAndJumpAffordance` | **OBSOLETE** | landed `68642cb7` |
| U5 | **B4** per-turn timeout (`51b0166c`) | `webTurnTimeoutEnv`, `webDefaultTurnTimeout`, `webTurnTimeout()`, `ag.Timeout = webTurnTimeout()`; `TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite` | **OBSOLETE** | `git log -S'webTurnTimeoutEnv' master -- cmd/agent/cli_web.go` → `c0c722e3`; on master the increment is named **B12** (the rework's numbering), but it is the same knob |
| U6 | **B5** transcript tool rows (`76b4a8db` + `51b0166c`) | `internal/agent/chat_ops.go`: `ToolCallSummary`, `ToolResultDigest`, `toolResultHeaderMarker`, `toolResultLines`, `firstLineOf`, `digestEllipsis`; `webTranscriptDigestMaxRunes`, `transcriptTools`; `TestWebM2B5_TranscriptCarriesToolNameAndSummaryForReplay` | **OBSOLETE (strengthened)** | landed `c0c722e3`. Master's rework is *stronger* than the tag: the digest skips the harness `[tool_use id: …]` header line (the tag's first-line rule rendered harness metadata instead of tool output — recorded in master's `devdoc/web-m2-rework.md`) |
| U7 | **B8** minimal markdown (`cecf0d97`) | `webMarkdownFence`, page `MD_TICK`/`MD_FENCE`; `TestWebM2B_PageKeepsTheXSSDiscipline`, scenario `'B8 markdown is minimal, safe and streaming-friendly'` | **OBSOLETE** | landed `c0c722e3` |
| U8 | **B9** input-trigger menus (`cecf0d97`) | `detectSlashMenu`, `detectFileMenu`, `capMenuList`, `handleMenu`, `webMenuPath`, `webMenuMaxItems`, `webFileListMaxEntries`, `webFileListMaxDepth`, `webMenuMoreSuffix`; `TestWebM2B9_MenuEndpointServesTheSharedTable`, `TestWebM2B9_MenuHonoursItsOneBudgetAndItsWalkRoot` | **OBSOLETE (strengthened)** | landed `c0c722e3`. Master's menu reads the existing shared command table (`slashCommandNames()`) instead of the tag's own list; the walk is bounded three ways with an honest `total` |
| U9 | **B12** visible turn queue + steering (`cecf0d97`) | `webPlacementSteering`, `webSteerPrefix`, `webQueuedPayload`, page `STEER_PREFIX`/`PLACEMENT_QUEUED`/`PLACEMENT_STEERING`; `TestWebM2B12_MidTurnMessage...` family in the page scenarios | **OBSOLETE** | landed `c0c722e3`; master's page scenario `'B12 composer stays usable during a turn; a mid-turn message is a visible QUEUED entry'` |
| U10 | **B7** composer guards (`51b0166c`) | page: `input.addEventListener('keydown'`, `if(e.isComposing\|\|e.keyCode===IME_PROCESSING_KEYCODE)return;`, `if(e.repeat)return;`, `function autogrow()`, `input.addEventListener('input',autogrow)`, `INPUT_MAX_ROWS=8`, `INPUT_LINE_HEIGHT=22`, `Math.min(input.scrollHeight,max)` | **OBSOLETE** | only the commit label differs: `git log -S'e.repeat' master -- cmd/agent/cli_web.go` and `-S'IME_PROCESSING_KEYCODE'` both → `c0c722e3`, where the same guards land as part of the B9 composer arbitration. The tag's B7 test asserts master's page **verbatim** |
| U11 | tag docs: `devdoc/web-m1-b4b7.md`, `devdoc/m2-web-m2.md`, `devdoc/web-m1-b1b2b3.md`, `devdoc/gui-chat-roadmap.md` rows, `.live/verify.py`, page JS harness/scenarios, regression tests | blob-identical copies live at master `.ref/web-m1-b4b7.md`, `.ref/m2-web-m2.md`, `.ref/web-m1-b1b2b3.md`, `.ref/m1_test.go`, `.ref/m2_test.go`, `.ref/m2b_test.go`, `.ref/page_harness.js`, `.ref/page_scenarios.js`; the roadmap rows shipped | **OBSOLETE (preserved)** | `git rev-parse` table in §2; the tag's M1.6 acceptance line is on master's roadmap: *"a 25 MiB decoded image is accepted by both sides; held Enter sends once"* |
| U12 | **B6** dead cap removed + client/server budget aligned (`51b0166c`) | **`maxWebHistoryMessages` re-added** at `cmd/agent/cli_web.go:932-934`; **`webMaxChatBodyBytes = 4 << 20 // 4 MiB`** still a flat literal at `:1854`, while the page ships up to ~42.7 MiB of base64 per image and `validateWebImages` accepts 32 MiB decoded | **SALVAGE** | RED observed on clean master (§4) — the server's own decoder refuses a body the server's own validator accepts |

The `live` half of U12 is what makes this a salvage rather than a re-measurement note: master's rework landed **B5/B8/B9/B12 and the B7 guards**, but never carried the **one-budget** half of the original B6. Master's `cli_web.go` had previously *deleted* the dead `maxWebHistoryMessages` (commit `9fe3d48c feat(web,agent): web-align P0 — server-owned context, --tools tool path, graceful shutdown`; `git log -S'maxWebHistoryMessages' master -- cmd/agent/cli_web.go`), and the rework's rescue of the tag's test file into `.ref/` put back a *documentation-only* copy at `:932` — its comment literally says it is *"retained only as documentation of the pre-R1 cap"*, and it is referenced by nothing (`master:cmd/agent/cli_web.go:934` is its only hit, `.ref/` and `webBusyWaitNotice`-style comments aside).

---

## 3.1 The content-test transcript (raw, as observed)

The table above is a summary; this is the instrument output behind it, run in
the salvage worktree against master.

**Presence on master (first hit per symbol):**

```
$ grep -n webChatResetPath cmd/agent/cli_web.go
715:    mux.HandleFunc(webChatResetPath, s.handleChatReset)   // B1: conversation reset
1305:const webChatResetPath = "/api/chat/reset"
$ grep -n handleChatReset cmd/agent/cli_web.go
1556:// handleChatReset clears the server-owned conversation (B1): s.history — the
1574:func (s *webServer) handleChatReset(w http.ResponseWriter, r *http.Request) {
$ grep -n resetConversationLocked cmd/agent/cli_web.go
1597:// resetConversationLocked is the ONLY mutation of the reset state: it clears
1602:func (s *webServer) resetConversationLocked() int {
$ grep -n webFollowThreshold cmd/agent/cli_web.go
1547:// webFollowThreshold is the page's bottom-follow gate in CSS px — the same
1554:const webFollowThreshold = 24
$ grep -n webQueuedPayload cmd/agent/cli_web.go
1291:// webQueuedPayload is the `queued` frame's body (B2): the typed placement plus
$ grep -n webPlacementQueued cmd/agent/cli_web.go
1250:// webPlacementQueued is the queued frame's typed placement value — the ONE
1254:const webPlacementQueued = "queued"
$ grep -n webChatResetPath / webClearCommandName / webClearAliasName cmd/agent/commands.go cmd/agent/cli_chat.go
commands.go:704:const webClearCommandName = "new"
commands.go:713:const webClearAliasName = "clear"
commands.go:766:	case webClearAliasName, webClearCommandName:
cli_chat.go:508:		case "/" + webClearAliasName, "/" + webClearCommandName:
$ grep -n webTurnTimeoutEnv cmd/agent/cli_web.go
733:// webTurnTimeoutEnv is the env var that overrides the per-turn web turn budget
735:const webTurnTimeoutEnv = "GOPI_WEB_TURN_TIMEOUT_SEC"
$ grep -n detectSlashMenu cmd/agent/cli_web.go
1396:// detectSlashMenu is the SERVER half of the shared detect core: the query a
1401:func detectSlashMenu(query string) []string {
$ grep -n webMarkdownFence cmd/agent/cli_web.go
1331:const webMarkdownFence = "```"
$ grep -n webPlacementSteering cmd/agent/cli_web.go
1261:const webPlacementSteering = "steering"
$ grep -n ToolCallSummary internal/agent/chat_ops.go
72:		summaries[j] = ToolCallSummary(tc)
$ grep -n ToolResultDigest cmd/agent/cli_web.go
1833:		return []transcriptTool{{ID: m.ToolCallID, Name: name, Result: agent.ToolResultDigest(m.Content, webTranscriptDigestMaxRunes)}}
```

**Which master commit introduced each surface** (`git log -1 -S<sym> master`):

```
webChatResetPath           68642cb7 feat(web): B1 reset endpoint, B2 pre-lock queued frame, B3 follow-threshold gate
webTurnTimeoutEnv          c0c722e3 feat(web): B1/B2/B3/B5/B8/B9/B12 rebuilt off master — transcript tool rows, input-trigger menus, per-turn budget, queued/steering turns
detectSlashMenu            c0c722e3 …
webMarkdownFence           c0c722e3 …
webPlacementSteering       c0c722e3 …
ToolCallSummary            c0c722e3 …
e.repeat                   c0c722e3 …
IME_PROCESSING_KEYCODE     c0c722e3 …
maxWebHistoryMessages      9fe3d48c feat(web,agent): web-align P0 — server-owned context, --tools tool path, graceful shutdown
```

**Present on master as test functions** (the tag's own regression tests have
master equivalents; the `.ref/` copies are listed by `grep -rl` because they
still carry the text):

```
cmd/agent/cli_web_m1_test.go   TestWebM1B1_ResetClearsHistoryUnderLockTurnNPlusOneCannotSeeTurnOne
cmd/agent/cli_web_m1_test.go   TestWebM1B1_SlashNewResetsUnderLockViaSharedCommandTable
cmd/agent/cli_web_m1_test.go   TestWebM1B2_SecondConcurrentPostGetsQueuedFrameWhileFirstHoldsLock
cmd/agent/cli_web_m1_test.go   TestWebM1B3_FollowThresholdGatesStreamScrollAndJumpAffordance
cmd/agent/cli_web_m2_test.go   TestWebM2B12_PerTurnTimeoutAppliedFromOneEnvReadSite
cmd/agent/cli_web_m2_test.go   TestWebM2B5_TranscriptCarriesToolNameAndSummaryForReplay
cmd/agent/cli_web_m2_test.go   TestWebM2B9_MenuHonoursItsOneBudgetAndItsWalkRoot
cmd/agent/cli_web_m2b_test.go  TestWebM2B_PageBudgetMatchesTheServerImageCeiling
```

Note the distinction this makes for U1: `cli_web_m1_test.go` on master
(blob `624087b2`) is a **later revision** of the tag's file (blob `14e36bbb`,
preserved as `.ref/m1_test.go`) — the live one keeps the tag's three B1/B2/B3
tests and adds the rework's `...ResetClearsHistoryUnderLock...` case. The unit
did not merely survive; it grew.

**Which half of the tag the `cherry` line already settles.** `git cherry`’s
`-` lines (patch-id present on master) are the six commits
`a979ac9d, 5b51392d, 456f6a29, 21e19b21, 5e47bb76, d4e7b326` — i.e. B1/B2/B3
plus their regression tests plus the M1 B1/B2/B3 report. Those units are
therefore already proved landed *by the instrument itself* (patch-id identity,
no judgement call), and the §3 content test for U1–U4 merely names the master
commit (`68642cb7`) that carries the same end state. The twelve `+` commits —
B4/B5/B6/B7/B8/B9/B12 and the M1.5/M2 documents — are the ones the instrument
cannot settle, and they are the ones the content test in §3 was run for.

**Sibling/rework corroboration:**

```
$ git merge-base --is-ancestor archive/feat/web-m2-rework master ; echo $?
0
$ git cherry -v master archive/feat/web-m2-rework | wc -l
0
$ git log --oneline -1 68642cb7 / c0c722e3 / a392310c / ee5e0a07
68642cb7 feat(web): B1 reset endpoint, B2 pre-lock queued frame, B3 follow-threshold gate
c0c722e3 feat(web): B1/B2/B3/B5/B8/B9/B12 rebuilt off master — transcript tool rows, input-trigger menus, per-turn budget, queued/steering turns
a392310c Merge branch 'feat/web-m2-rework'
ee5e0a07 wip(feat/web-m2-rework): rescue uncommitted work before stopping the workers (PA)
$ git merge-base --is-ancestor <each> master ; echo $?   # 0, 0, 0, 0
$ comm -12 <(git rev-list 68642cb7 c0c722e3 ee5e0a07 | sort -u) \
           <(git rev-list 98fb2942 --not c6abf8b9 | sort -u)
	                                  # EMPTY: the tag shares no commit object with the landing
```

That last empty `comm` is the structural fact the per-unit test rests on: the
rework shares **no commit** with the tag, so "landed" cannot be shown by
ancestry — only by content, unit by unit, as tabulated above.

**The blob-identity pairs** (`git rev-parse <tag>:<p>` vs `git rev-parse master:<q>`),
all `IDENTICAL`:

```
devdoc/web-m1-b4b7.md              -> .ref/web-m1-b4b7.md              58d455ad5bcb7c41301e328ab4211c0a2d76a0ff
devdoc/m2-web-m2.md                -> .ref/m2-web-m2.md                8d4ae9c34bc82573ac1e642f3d3fabf5bc9a7bc0
devdoc/web-m1-b1b2b3.md            -> .ref/web-m1-b1b2b3.md            6aee9b69846021bf5963f4c8025cd77b5ea6bf17
cmd/agent/cli_web_m2_test.go       -> .ref/m2_test.go                  111836988a1470bd4291f070300e27c838a5b675
cmd/agent/cli_web_m2b_test.go      -> .ref/m2b_test.go                 e7b94041edaaf4b6b0e90c86d89e0c714aed5a05
cmd/agent/cli_web_m1_test.go       -> .ref/m1_test.go                  14e36bbbe192cf8369e9d9286235499ec133f397
cmd/agent/cli_web_page_harness.js  -> .ref/page_harness.js             8a90beaaf1dda7a15e630a42400717d4ef05d62a
cmd/agent/cli_web_page_scenarios.js -> .ref/page_scenarios.js          f196f8eea50fbc8dc17ed60c84a4e8d835dd07b0
```

`git log --oneline -1 master -- .ref/m2_test.go` → `ee5e0a07` (the rescue
commit); master tracks 11 files under `.ref/`, all from that one rescue.

## 4. The salvage: unit B6, with its observed RED proof

**The defect (measured, not inferred).** `handleChat` reads the body through

```go
if err := json.NewDecoder(io.LimitReader(r.Body, webMaxChatBodyBytes)).Decode(&req); err != nil {
    http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
```

(`cmd/agent/cli_web.go:1095`) with `webMaxChatBodyBytes = 4 << 20`. Master's page caps the base64 **it ships** by the *decoded* ceiling: `MAX_IMAGE_BYTES=32*1024*1024`, `MAX_B64=Math.ceil(MAX_IMAGE_BYTES*BASE64_RATIO_NUM/BASE64_RATIO_DEN)+4` (`cli_web.go:2007-2008`). One maximal image therefore reaches the server as ~42.7 MiB of base64 inside a ~44.7 MB body — **while `validateWebImages` accepts exactly that image** (`len(data) > webMaxChatImageBytes`, `:1710`). The two budgets disagree by an order of magnitude, which is precisely the W7 gap the tag's B6 closed and master never did.

**RED on clean master** — the new test (`cmd/agent/cli_web_m2b6_budget_test.go`) run against unmodified master:

```
--- FAIL: TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest (0.43s)
    cli_web_m2b6_budget_test.go:62: the handler's own reader could not decode a request its
    own validator accepts: body is 44739332 bytes, webMaxChatBodyBytes is 4194304,
    decode error: unexpected EOF
FAIL	github.com/nick/gopi/cmd/agent	0.962s
```

The test runs the handler's **own** read path (`io.LimitReader(r.Body, webMaxChatBodyBytes)` + `Decode`) over a body built from `webMaxChatImageBytes` — hermetic, no turn, no model, no network. It is committed with the fix, so it is the regression guard for exactly this failure mode.

**GREEN after the fix** (same test, same worktree):

```
=== RUN   TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest
--- PASS: TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest (1.10s)
PASS
ok  	github.com/nick/gopi/cmd/agent	1.687s
```

**The re-derived change** (fresh worktree off master, `fix/web-m2-b5b8b9b12-verdict`):

- `cmd/agent/cli_web.go` — `webMaxChatBodyBytes` becomes **derived from one budget** (the tag's rule, re-derived against master's own constant names, none of which the tag had):
  ```go
  const webMaxChatBodyBytes = webMaxChatImageBytes*webMaxImagesPerRequest*webBase64ExpansionRatioNum/webBase64ExpansionRatioDen + webBase64PaddingSlack + webMaxChatTextBytes
  ```
  with named `webBase64ExpansionRatioNum/Den`, `webBase64PaddingSlack`, `webMaxChatTextBytes` (Rule 1 — no inline numbers, one place to change) and a doc comment recording the failure mode; plus the now-false sentence in `webMaxChatImageBytes`'s comment (*"the effective per-request image budget is the body cap"*) corrected.
- `cmd/agent/cli_web_m2b6_budget_test.go` — the new regression test (the red proof above).
- `cmd/agent/cli_web_p1_test.go` — a stale NOTE ("a 33MiB decoded image … beyond the 4MiB request-body cap") updated: that cap is no longer a flat 4 MiB, it is the derived one.

Not salvaged from the tag, deliberately: `maxWebHistoryMessages` stays gone (removed by `9fe3d48c`; its only surviving trace on master is `ee5e0a07`'s `.ref/`-rescued documentation copy).

---

## 5. The (a)/(b) answer

**(a) Yes — one unit is SALVAGE: unit B6, the derived one-budget request-body cap.** It is re-derived above on a fresh branch off master, with the RED→GREEN evidence in §4; it lands through the normal verify path (this branch), never by touching the tag.

**(b) Every other unit is OBSOLETE**, each with the master surface named in §3: U1–U4 by `68642cb7` (B1/B2/B3, landed before the tag was archived), U5–U11 by `c0c722e3` (`feat(web): B1/B2/B3/B5/B8/B9/B12 rebuilt off master …`, merged by `a392310c Merge branch 'feat/web-m2-rework'`), with the tag's documents and tests preserved blob-identically under master `.ref/` by `ee5e0a07`.

Nothing in this run merges, deletes or rewrites the tag, the archived branch, or any sibling archive tag.

---

## Verification

All commands below were run in the salvage worktree
`/home/user/workspace/gopi-web-m2-remeasure` (branch `fix/web-m2-b5b8b9b12-verdict`,
off master `fa12d97f`), except the RED proof, whose whole point is a tree that
does **not** carry the fix.

- **Build:** `CGO_ENABLED=0 go build ./...` → exit 0 (observed twice: as
  `BUILD_OK`, and again with `bin/gopi` built — `BUILT`).
- **RED proof (clean master, test file only):** in a throwaway detached
  worktree at `fa12d97f` (`gopi-web-m2-redprobe`, since removed) with only
  `cmd/agent/cli_web_m2b6_budget_test.go` copied in (`git status --porcelain` →
  `?? cmd/agent/cli_web_m2b6_budget_test.go`):
  ```
  --- FAIL: TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest (0.59s)
      cli_web_m2b6_budget_test.go:62: the handler's own reader could not decode a request its own validator accepts: body is 44739332 bytes, webMaxChatBodyBytes is 4194304, decode error: unexpected EOF
  FAIL	github.com/nick/gopi/cmd/agent	1.546s
  ```
- **GREEN on the branch:** `go test -count=1 -run
  'TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest|TestWebChat_R5_ImageValidation4xx'
  ./cmd/agent/` → `ok github.com/nick/gopi/cmd/agent 1.867s` (exit 0), i.e. the
  new guard passes *and* the pre-existing oversize-body 4xx expectation still
  holds under the widened cap.
- **Branch-scoped smoke (Rule 27 — one package, never the whole tree, no
  device load):** attributed by a self-redirected json stream on this run's own
  file (`go test -count=1 -json ./cmd/agent/ > _tmp/wm2rem/cmdagent.json`),
  because `scripts/test-budget.sh` renders its summary from ONE shared
  `queue/test-budget/last.json` that a sibling run overwrote mid-measurement —
  full account and output in §6.2. Verdict: one package `fail`, two failing
  tests, **both attributed away from this diff** — `TestEveryHelpConstantFits-
  TheWrapBudget` is master's (reproduced on clean `fa12d97f`, §6.1), and
  `TestTaskRunSeamWritesReportAndShowRendersIt` was reading *this* worktree
  while the report doc was uncommitted (its own output quotes the path and the
  entry); it passes once the tree is clean.
- **`gofmt`/`go vet`:** `gofmt -l` on the three touched Go files prints
  nothing; `go vet ./cmd/agent/` exit 0.
- **Tag untouched:** `git tag -l 'archive/feat/web-m2-b5b8b9b12*'` still lists
  both tags; `git rev-parse archive/feat/web-m2-b5b8b9b12` is still
  `98fb29425453b3fef141e5d5d2e94234b03d8e41`; no commit exists on the archived
  branch from this run (`git worktree list` shows only this run's own branch at
  `fa12d97f`).
- **Worktree hygiene (Rule 30):** `git status --porcelain` prints **nothing**
  after the close-out commit — §6.

## 6. Observed run output

### 6.1 The smoke run, and its one failure is PRE-EXISTING on master

`scripts/test-budget.sh ./cmd/agent/` (branch tree, with `bin/gopi` built):

```
--- summary (713s wall, load 6.36 5.48 5.63 -> 7.06 6.60 6.15) ---
packages: 1 finished, 1 with a failure
  FAILED: github.com/nick/gopi/cmd/agent
--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestEveryHelpConstantFitsTheWrapBudget (0.05s)
stamp: 2026-09-19T11:16:05+08:00 host=peer1 pkg=./cmd/agent/ budget=25m elapsed=713s exit=1 outcome=fail
```

(This run started before `bin/gopi` was built, so it printed the harness NOTE
above: the CLI E2E guards *fail* rather than skip without a binary. That note
is a hint, not the failure — no CLI E2E guard is what failed. A second
`scripts/test-budget.sh ./cmd/agent/` run on the same tree **with**
`bin/gopi` present is recorded in §6.3.)

**Attribution — the failure is master's, not this branch's.** This branch's
diff touches `cli_web.go`, its two page tests and one devdoc; the failing gate
measures *help-text width*, and `cmd/agent/cli_chat.go` (the file it names) is
**byte-identical to master** in this worktree (`git hash-object` ==
`git rev-parse master:cmd/agent/cli_chat.go`). Measured directly on a clean
detached checkout of master `fa12d97f` (`gopi-web-m2-verdict-probe`, since
removed, no branch content at all):

```
$ go test -count=1 -run 'TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/
--- FAIL: TestEveryHelpConstantFitsTheWrapBudget (0.05s)
    help_width_test.go:72: cli_chat.go: chatUsage line 8 is 143 display cols (> 88): "--no-editor        disable the raw-mode line editor and read plain cooked                   lines (the pre-editor behavior). Automatic whenever"
FAIL	github.com/nick/gopi/cmd/agent	0.404s
```

The offending line is the `--no-editor` row of `chatUsage` in
`cmd/agent/cli_chat.go:243`, landed on master by
`d9df443d feat(chat): a clean start — one line above the prompt, plumbing behind --verbose`
(2026-09-19) — after the tag was archived, and unrelated to it. **Reported,
not fixed here**: it is a master-side help-wrap regression on a file this task
never touches, and quietly re-wrapping another change's help text inside a
salvage report would hide it. It belongs on its own task row.

### 6.2 The with-binary run is NOT attributable — and neither is the second one

A second `scripts/test-budget.sh ./cmd/agent/` run, this time after
`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (`BUILT`), printed a summary
naming **`internal/tools`** tests (`TestBidir2ImportSymmetry`,
`TestProposalRowsAreNeverClaimable`, `TestRollbackLocalClaimRevertsToPending`, …)
while its own stamp read `pkg=./cmd/agent/`:

```
stamp: 2026-09-19T11:30:22+08:00 host=peer1 pkg=./cmd/agent/ budget=25m elapsed=900s exit=1 outcome=fail
```

**Cause, measured:** `scripts/test-budget.sh` writes every run's stream to ONE
shared path, `<scratch>/queue/test-budget/last.json`, and its summary is
rendered from that path. A sibling task's `bash scripts/test-budget.sh
./internal/tools` run (pid 3327299, started 13m earlier, still live) overwrote
it between this run's test execution and its summary. `internal/tools` is not
this branch's scope, and this worktree cannot fail it — the lines are another
run's, not a second failure. **The instrument has a shared-output collision;
its summary is only readable when no sibling test-budget run is in flight.**
Nothing in this run writes that path by hand, and no shared state was modified.

So the attributable measurement is a direct, self-redirected one — per-package
json stream, this run's own file, no shared intermediate:

```
$ CGO_ENABLED=0 go test -count=1 -timeout 1200s -json ./cmd/agent/ > _tmp/wm2rem/cmdagent.json
EXIT=1
pkg outcomes: {'github.com/nick/gopi/cmd/agent': 'fail'}
FAILs: TestEveryHelpConstantFitsTheWrapBudget, TestTaskRunSeamWritesReportAndShowRendersIt
```

**Both failures named, then attributed:**

1. `TestEveryHelpConstantFitsTheWrapBudget` — **pre-existing on master**, §6.1
   (reproduced on a clean detached `fa12d97f`).
2. `TestTaskRunSeamWritesReportAndShowRendersIt` — **my own tree, mid-run.**
   That test builds a fixture task whose `task run` close-out gate reads the
   ledger row's *own* worktree, and the fixture row resolved to
   `/home/user/workspace/gopi-web-m2-remeasure`, i.e. this session's worktree,
   which at that moment held the uncommitted report doc. Its own output says so
   verbatim:

   ```
   result: run t-report-seam left UNCOMMITTED TRACKED WORK in its OWN worktree
   /home/user/workspace/gopi-web-m2-remeasure: worktree is DIRTY (uncommitted work) —
   entries: M devdoc/web-m2-b5b8b9b12-remeasure.md; the completed writeback is REFUSED
   ```

   Re-run with the worktree clean (report committed):

   ```
   $ go test -count=1 -run 'TestTaskRunSeamWritesReportAndShowRendersIt|TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/
   --- FAIL: TestEveryHelpConstantFitsTheWrapBudget (0.05s)
       help_width_test.go:72: cli_chat.go: chatUsage line 8 is 143 display cols (> 88): … the pre-editor behavior). Automatic whenever
   FAIL	github.com/nick/gopi/cmd/agent	17.416s
   ```

   The seam test passes; only the master-side help-width red remains. A
   worktree that is dirty *because a task is mid-close-out* is exactly what that
   gate is for (Rule 30), so this is the gate working, not a defect.

### 6.3 Porcelain at close-out

`git status --porcelain` in the salvage worktree prints nothing (the salvage,
the new regression test, the stale-NOTE correction and the report are all
committed on `fix/web-m2-b5b8b9b12-verdict`).

## Not Done (honest)

- The page-side half of the tag's B6 (its client cap derivation) needed no salvage: master's page already derives `MAX_B64` from `MAX_IMAGE_BYTES`, and its `TestWebM2B_PageBudgetMatchesTheServerImageCeiling` pins it.
- No branch-scoped test was invented for the OBSOLETE units — there is nothing to test for work master already carries; their evidence is the content test in §3.
- The tag's `.live/verify.py` live driver was **not** resurrected (it drives a built binary over HTTP; the surface it measured is master's now, and re-running a live driver was outside a read-only re-measure).

## Evidence (self-filed)

- `git cherry -v master archive/feat/web-m2-b5b8b9b12` — 12 `+` / 6 `-` lines, printed verbatim in §1.
- `git rev-list --count master..archive/feat/web-m2-b5b8b9b12` → `19`, printed in §1; reconciled as 12 + 6 + 1 internal merge `15229754`.
- `git merge-base master archive/feat/web-m2-b5b8b9b12` → `c6abf8b9a5c9796611262da8263d01b27ddb5fd5`; `git merge-base --is-ancestor … master` → exit 1.
- `git merge-base --is-ancestor archive/feat/web-m2-rework master` → exit 0; `git cherry -v` on it → 0 `+`; `git rev-list --count master..archive/feat/web-m2-rework` → `0`.
- Blob identity for the eight `.ref/` path pairs — the `git rev-parse` pairs printed in §2 and §3.1, all equal.
- Content test per unit — the presence grep + `git log -1 -S<sym> master` + master test-function transcript in §3.1, plus the empty `comm` proving the tag and the rework share no commit object.
- Pre-existing master red (`TestEveryHelpConstantFitsTheWrapBudget`) — reproduced on a **detached clean master checkout with no branch content** (`gopi-web-m2-verdict-probe`), output in §6.1, same 143-col `chatUsage` line, same failure.
- `gofmt -l` on the three touched Go files → no output; `go vet ./cmd/agent/` → exit 0.
- `git tag -l 'archive/feat/web-m2-b5b8b9b12*'` → both tags present; `git rev-parse archive/feat/web-m2-b5b8b9b12` → `98fb29425453b3fef141e5d5d2e94234b03d8e41` (unchanged before and after the run).
- `git status --porcelain` in the salvage worktree → empty at close-out.
- RED→GREEN transition — the two test outputs quoted in §4, both observed in this run.
- `CGO_ENABLED=0 go build ./...` → printed `BUILD_OK` in the salvage worktree.
