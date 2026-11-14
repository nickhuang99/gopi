<!-- doc-class: historical -->
# Task Report: task/01a11b5d48887a4a9b13ff18a2c83673 — the streaming-dictation design conflict, resolved and verified

**Task id:** `01a11dd0095a706b8aab797df42abc1e` (this run) — reworking the row
`01a11b5d48887a4a9b13ff18a2c83673`
**Worktree / branch:** `/home/user/workspace/gopi-01a11dd0095a706b8aab797df42abc1e`
on `task/01a11dd0095a706b8aab797df42abc1e` (Rule 6a — the assigned ground; no second
worktree was created and the branch was not renamed)
**Commit:** the commits this run made sit on top of the delivery-point sync
`70c6535e3` — `git log --oneline 70c6535e3..HEAD` prints them (the first is
`456a0c0d1`, the port; the second the report; the rest this document's own
corrections, each fixing a claim the previous one had stated too strongly). Naming
them in a list here is exactly the self-reference those later commits remove, so the
list is read off the ref; a commit cannot name its own hash — `git log --oneline -1`
prints the tip.
**Previous tip:** `70c6535e3` — a 2-parent merge (parents `6e1b841dd` = this
lineage, `22f3b74b1` = master's landing `823305a17`); master is an ancestor.

## The measured starting point (correcting the briefing)

The briefing said the branch tip `96a2f812f` no longer exists, the attached diff is
gone, and the worktree's own branch **already** holds the lineage plus a
delivery-point sync that resolved the overlap in favour of master. All of that is
confirmed, with one correction that changes the work:

- `96a2f812f`, `309bcb92b`, `cd7e46d81` are **gone**; only `9d781ad18` (`feat(doubaostream):
  an incremental Session — Push audio on a live clock, OnPartial live`) survives as an
  object, and it carries **no** `devdoc/chat-voice-live-streaming.md`. The doc's text
  was recovered from this session's own evidence file
  (`$SESSION_DIR/evidence/branch-recovered.txt`, section `###DOC`).
- The sync kept **master's** side for every overlap: `internal/doubaostream/session.go`
  (`46980ed3`), `internal/lineedit/draft.go` (`e3b0d251`) and `cmd/agent/chat_voice_live.go`
  (`ffe9e154`) are byte-identical to master, and the losing attempt's
  `cli_voice_live.go` / `livedraft.go` are absent. Verified exhaustively: every
  `internal/lineedit/*.go`, the recorder/wire/pty files and `cli_chat.go` are
  `IDENTICAL to master` (10/10 checked).
- **Correction to briefing item (b).** The briefing asked for the branch's
  "server-own-read await" to be ported. Master **already has it**: `session.go`'s
  `Close` (line 278) and `Finish` (line 260) both end in `s.awaitReader()`. What was
  missing on master was **the case that would notice its removal**, not the await.
  That is what this run ported. Likewise the commit+draft-drop await: master's
  `Editor.drainDraft` clears the draft in the SAME pass as the settle
  ("the operator never sees the text twice") and is pinned by
  `TestDraftSegment_SettlesOntoTheLineExactlyOnce` + `TestDraftSegment_LastWinsIsLevelTriggered`.

## Design decision (recorded, as the epic requires)

**Master's landed design survives; the losing attempt's parallel design is NOT
re-introduced.** Concretely:

| survives (master `823305a17`) | dropped with its design |
|---|---|
| `doubaostream.Session`: `Open`/`Say`/`Finish`/`Close` + `PartialCount`/`FinishTimedOut`, `readLoop`, `awaitReader` | `Session.Push`/`End`/`Wait` + `SessionOptions`, `readerDone`/`sendDone` channels |
| `lineedit/draft.go`: `SetDraft(text,done)`/`Draft`/`DraftVisible`/`drainDraft` | `lineedit/livedraft.go`: `SetLiveDraft`/`ClearLiveDraft`/`LiveDraft` |
| the live half on the existing recorder: `voiceChatRecorderStaged.Live` | `voiceChatRecorderLive` + the optional `Listen(...)` seam |
| the finish key `Ctrl+G` (`lineedit.KeyLiveFinish`) | the spoken stop word (`voiceChatLiveSaidStop` / strip) |
| CLI side `cli_voice_stream.go` | CLI side `cli_voice_live.go` |

**Why this direction**: the landed design is what master advertises and what the
`Ctrl+G` contract in the commit message names; re-introducing the parallel session
type would mean two live-streaming designs in the tree. The losing attempt's value
was carried over **where it does not depend on which design survived** — see below.

## Completed

1. **Verified the sync's resolution is coherent.** `git diff --stat master` is
   9 files, all of them the branch's own orthogonal work (the four-defect voice fixes
   in `chat_voice.go`/`cli_voice.go`/`cli_voice_local.go` + `voice_module_defects_test.go`,
   the chat-PA duty work in `internal/agent/`, and 2 devdocs + 2 DOC-INDEX rows). No
   half-merged file, no residue of the losing design, and the branch's intent is
   intact rather than merely absent: the four-defect fixes are the task's own
   deliverable and they survive unpicked.
2. **Ported the hardening as a real test that fails if the await is removed** —
   `internal/doubaostream/session_test.go` (606 lines) drives the **surviving** API
   against a real loopback WebSocket fake server (`net/http/httptest`, stdlib only,
   Rule 35), and `internal/doubaostream/session_teardown_guard_test.go` (174 lines,
   `TestSessionTeardownAwaitsTheReader`) makes "the teardown FORGETS to join" a RED
   build by parsing `session.go` with `go/ast`.
3. **Restored `devdoc/chat-voice-live-streaming.md`, truthful about the surviving
   design.** It now records which design won and why, the two defects **both**
   independent attempts found on their first real run (the wake-level that was not a
   level; describing the recorder before arming it), what the surviving design does
   instead, what was dropped, and the honest non-ports. Its blurb is backed by its
   own citation — every identifier it names was verified to exist in the tree.
4. **Unioned the DOC-INDEX row**: exactly one insertion, no row dropped or rewritten
   (all of master's rows and the branch's 2 rows are present).

## Files

The branch is 13 paths ahead of `master` (`823305a17`) — `git diff --stat master...HEAD`
prints exactly this set, and it is the same set the merge probe stages:

| path | what it is |
|---|---|
| `cmd/agent/chat_voice.go` | the branch's orthogonal work: four-defect voice fixes (+ the chat clause (1b) commits it already carried) |
| `cmd/agent/cli_voice.go` | same, CLI voice surface |
| `cmd/agent/cli_voice_local.go` | same, local-engine path |
| `cmd/agent/voice_module_defects_test.go` | regression tests for those four defects |
| `internal/agent/chat_pa_duty.go` | the branch's chat-PA duty work (orthogonal to the conflict) |
| `internal/agent/chat_pa_duty_test.go` | its tests |
| `internal/doubaostream/session_test.go` | **NEW this run** — 606 lines, drives the surviving API (`Open`/`Say`/`Finish`/`PartialCount`/`FinishTimedOut`/`Close`) against a real loopback WS fake server (stdlib `net/http/httptest`) |
| `internal/doubaostream/session_teardown_guard_test.go` | **NEW this run** — 174 lines, `TestSessionTeardownAwaitsTheReader`: a `go/ast` source guard making "the teardown forgot to join" a RED build |
| `devdoc/chat-voice-live-streaming.md` | **restored this run, truthful** — states which design survived, which was dropped, the two defects both attempts found, the hard port, the honest non-ports |
| `devdoc/task-01a11b5d-streaming-design-conflict-report.md` | **NEW this run** — this document (Rule 5) |
| `devdoc/chat-pa-quick-ops-no-permission.md` | the branch's own doc (orthogonal work) |
| `devdoc/voice-module-four-defects-01a11da05.md` | the branch's own doc (orthogonal work) |
| `devdoc/DOC-INDEX.tsv` | the unioned row set (Rule 36) — every master row kept, the branch's rows kept, this run's rows added |

**Not in the set, deliberately**: `internal/doubaostream/session.go`,
`internal/lineedit/draft.go`, `cmd/agent/chat_voice_live.go` and every other file the
sync took master's side on — they are byte-identical to master and therefore contribute
no diff. The losing design's `cmd/agent/cli_voice_live.go` and
`internal/lineedit/livedraft.go` do not exist anywhere on this branch; that is the
design decision above, not an omission.

## Not Done (honest)

- **Console quota is $0 for this run**, so the full `make test-budget` console run could
  not be re-run to a clean exit after the A/B work; the attribution above is by targeted
  re-runs, not by one green full-suite line. Note for reproducibility: `session.go`'s
  mtime is touched by the RED proof, and the budget runner's derived+buildvcs binary then
  refuses the 10 binary-driving tests with the mtime message — re-run in a tree where the
  same run builds the binary.
- **The losing attempt's Esc-mid-turn case is an honest non-port.** Its assertion was
  on the draft accessor (`Editor.LiveDraft() == ""`); the surviving design exposes no
  such accessor and asserts on the pty GRID instead. The portable half of its intent
  is the src guard. Whether the **surviving** design has that defect (Esc mid-utterance
  discarding a partial the operator watched appear) is **not measured** — the pty
  cases drive the finish key, not Esc-mid-partial. Recorded in the devdoc as an open
  question, not dressed as a port.
- **No code fix was needed in `internal/doubaostream/session.go` or
  `internal/lineedit/draft.go`** — the hardening the briefing asked for was already
  landed on master; this run's deliverable there is the missing proof, not the code.
- The stop-word machinery exists only in the losing design and is described as
  dropped, never as a TODO.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **rc=0** (no output). Re-measured as the LAST
  act of this run, whole-tree, in ONE command with the vet and the tests below — same
  tree, and the command's own first line prints the tip it ran at — output
  `BUILD_RC=0` / `VET_RC=0` / `TEST_RC=0`
- Tests: `make test-budget PKGS='./cmd/agent ./internal/doubaostream ./internal/lineedit'`
  → **`internal/doubaostream` and `internal/lineedit` PASS**; `cmd/agent` reports a
  failure, **every item of which is attributed by A/B against pristine master below**
  (console quota is $0 for this run, see ## Not Done). Targeted re-runs — the last two
  taken standalone at the tip that carried them, so the mtime artifact below cannot
  apply: the FINAL measurement of this run is
  `go test ./internal/doubaostream/ ./internal/lineedit/ -count=1` in that same
  build+vet+test command → `ok github.com/nick/gopi/internal/doubaostream  3.999s` +
  `ok github.com/nick/gopi/internal/lineedit  49.888s`, rc=0. Earlier:
  `go test ./internal/doubaostream/ -run 'Session|Teardown' -count=1` →
  `ok github.com/nick/gopi/internal/doubaostream 4.333s`;
  `go test ./internal/doubaostream/ -run 'TestSessionTeardownAwaitsTheReader' -count=1`
  → `--- PASS` / `ok ... 1.799s`
- Vet: `go vet ./...` → **rc=0** (whole tree; re-measured in the same final command as
  the build and the tests above). The earlier scoped run
  `go vet ./internal/doubaostream/... ./internal/lineedit/... ./cmd/agent/...` was also rc=0
- gofmt: `gofmt -l` on both new files → **empty**
- Ran (Rule 31): the RED proof, verbatim —
  `cp internal/doubaostream/session.go /tmp/session.go.bak` → delete `s.awaitReader()`
  from `Close` → `go test ./internal/doubaostream/ -run TestSessionTeardownAwaitsTheReader -count=1`
- Result: **FAIL**, with the guard's own message:
  `session_teardown_guard_test.go:87: Close does not call awaitReader — a teardown that
  returns without the join hands its caller a statement about a socket and a result that
  are not yet true`. `session.go` restored (`git diff --stat internal/doubaostream/session.go`
  empty) and `TestSessionTeardownAwaitsTheReader` green again.
- Ran (Rule 31), the merge probe, verbatim — a detached worktree at master, then
  `git merge --no-commit --no-ff task/01a11dd0095a706b8aab797df42abc1e`:
  ```
  Automatic merge went well; stopped before committing as requested
  MERGE RC=0
  M cmd/agent/chat_voice.go        M cmd/agent/cli_voice.go        M cmd/agent/cli_voice_local.go
  A cmd/agent/voice_module_defects_test.go   M devdoc/DOC-INDEX.tsv
  A devdoc/chat-pa-quick-ops-no-permission.md  A devdoc/chat-voice-live-streaming.md
  A devdoc/task-01a11b5d-streaming-design-conflict-report.md
  A devdoc/voice-module-four-defects-01a11da05.md
  M internal/agent/chat_pa_duty.go           M internal/agent/chat_pa_duty_test.go
  A internal/doubaostream/session_teardown_guard_test.go   A internal/doubaostream/session_test.go
  unmerged count = 0
  ```
  **The command above is the run FORM, not one run's output**: it was executed six times
  in this session, at six different tips of this branch as its commits landed, and every
  execution printed the same rc and the same 13-path set. The command block quoted in
  this section is the first of them, so an earlier form of this section named its tip;
  the rc and the set never changed. A detached worktree at `master` = `823305a17`,
  `git merge --no-commit --no-ff task/01a11dd0095a706b8aab797df42abc1e` → `MERGE_RC=0`,
  `staged count = 13`, `diff` of the staged set against
  `git diff --name-only master...HEAD` → **STAGED-SET-IDENTICAL-TO-BRANCH-SET**, and the
  stronger statement behind it: **the merge index's tree and the branch tip's tree are
  the same object** — `git write-tree` in the probe and `git rev-parse HEAD^{tree}` on
  the branch print the same hash (this run measured it at every tip it produced; the
  value is tip-bound, so it is not quoted as a standing figure here). So the merge
  adds nothing and drops nothing, byte for byte, not merely file-name for file-name.
  Probe then `git merge --abort` → rc=0 → `git status --porcelain` empty →
  `git worktree remove` (no `--force` needed) → `git worktree list` no longer lists it.

  **Which parts of that reading are INVARIANT and which are not — measured, not argued.**
  The probe was re-run at each further tip this run produced, every time with
  `MERGE_RC=0`, `staged=13`, `unmerged=0` and `merge tree == branch tree`. The tree object moves with every
  document-only commit and is therefore **true only at the tip that names it**; the rc,
  the staged SET and the "tree == branch tree" identity **do not** — a document-only
  commit cannot change the file set, and it moves both trees together. So the standing
  verdict is the invariant half, and it re-measures green at whatever tip
  `git log --oneline -1` prints.
- doccheck: `gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every
  file under devdoc/ has a well-formed row`
- Rule 30: `git status --porcelain` in the worktree → **empty** (after the commit)
- Commit: **every commit above the delivery-point sync `70c6535e3` on
  `task/01a11dd0095a706b8aab797df42abc1e` is this session's** — `git log --oneline
  70c6535e3..HEAD` prints them and `git log --oneline 70c6535e3` is the hand-off point, so
  the list is read off the ref rather than quoted here (the first of them is the port,
  `test(doubaostream): prove the landed teardown join against a real loopback service`;
  the rest are this document's own corrections, each one fixing a claim the previous one
  had stated too strongly — listing their hashes here would only reproduce the
  self-reference the document commits exist to remove).

## The `cmd/agent` reds, attributed (A = pre-existing / environmental, B = mine)

`go test ./cmd/agent/ …` was run twice — once on a detached worktree of **pristine
master** (`823305a17`, clean) with the same `-run` set, once on this branch — and the
two runs are compared test-by-test.

**A — environmental, reproduced identically on pristine master (not this branch).**
`TestToolsMaintenanceHelpSurfaceFormsAreTyped` fails with the SAME 4 subtests
(`gopi_tool_voice_input`, `…_--seconds_5`, `…_clip.ogg`, `…_clip.pcm`) and the SAME
cause on pristine master and on this branch. The cause is the machine, not the code:
this host has **no `/home/user/.doubao/apikey.txt`** (`ls` → *No such file or
directory*), no `GOPI_VOICE_ENGINE` and no `voice_engine` key in `~/.gopi/settings.json`,
so the cloud engine answers and the run stops at
`gopi tool voice_input: doubao engine needs an API key: …`. The case's own text says so
("WHICH engine answers depends on the ENV — the cloud engine is the default"). Nothing
in this branch's diff touches `voice_input` or the engine resolver.

**Environmental ARTIFACT of the budget runner, green on re-run with a fresh binary.**
10 tests failed in the budget run with the SAME refusal, not an assertion:
`<tested-bin>/gopi (mtime 08:15:01) is OLDER than .../internal/doubaostream/session.go
(mtime 08:16:28) — this test drives the BUILT binary, so it would verify code that is
not in that binary. Rebuild it…`. That mixture is an artifact **this run created**:
those tests see `internal/` touched, so they declare `dirtySource` and rebuild, while
`make test-budget` had already stamped its own derived binary as non-dirty. Re-run with
an ordinary build (no stamped binary in play), **all 10 PASS** — a targeted re-run of
the same `-run` set on this branch produced only the environmental
`TestToolsMaintenanceHelpSurfaceFormsAreTyped` failure and nothing else. The 10:
`TestRawPromptRunReleasesItsWorktree`, `TestWorkerExecutorChildStandsInTheTasksRepoNotTheLaunchers`,
`TestWorkerChildDoesNotInheritTheDaemonsSessionHandle`,
`TestWorkerAnnouncesTheGroundToTheChildAndLeavesOneWorktree`,
`TestTaskRunSpawnsTheChildInsideItsOwnLinkedWorktree`, `TestWorkerRefusesWhenTheGroundCannotBeMade`,
`TestTaskRunStandsInTheSessionDirAndHandsTheWorkdirToTheChild`,
`TestFrontRunConsoleOutputLandsInItsSessionDir`, `TestWorkerForegroundPrintsTheTaskOnItsConsole`,
`TestEditToolAnnotatesTeachingDocsInAuditedTrees`.

**B — none.** No failure is attributable to a change this branch makes: the branch's
diff touches `chat_voice.go` (four-defect fixes), `cli_voice*.go`, `internal/agent/chat_pa_duty.go`
and test/doc files, and every one of those packages' tests is green apart from the
environmental case above.

**How to see this attributed set red-free**: re-run
`go test ./cmd/agent/ -count=1 -run '<the 11 names above>'` in a tree where the tested
binary is built by that same run — the 10 artifact failures disappear; the remaining 4
subtests need a voice channel (`GOPI_VOICE_ENGINE=local` or the doubao key file) to
reach the state they assert on.

## Evidence (self-filed)

- **"master already has the await"** — `git show master:internal/doubaostream/session.go |
  grep -n awaitReader` → `260: s.awaitReader()`, `278: s.awaitReader()`, `420: func (s
  *Session) awaitReader()`. `git diff --stat master -- internal/doubaostream/session.go`
  → empty (identical).
- **"the sync took master's side everywhere"** — 10/10 files (all
  `internal/lineedit/*.go`, `chat_voice_pty_test.go`, `chat_voice_recorder_staged.go`,
  `lineedit_wire.go`, `cli_voice_recorder_staged.go`, `cli_chat.go`) compared with
  `git rev-parse HEAD:<f>` vs `git rev-parse master:<f>` → all IDENTICAL.
- **"the doc's text was recoverable"** — `git cat-file -e 9d781ad18:devdoc/chat-voice-live-streaming.md`
  → missing; recovered instead from `$SESSION_DIR/evidence/branch-recovered.txt`
  (`###DOC` section, 108 lines).
- **"every identifier the doc names exists"** — 16/16 symbols (`voiceLiveEngineSelected`,
  `SetDraft`, `DraftVisible`, `drainDraft`, `voiceLiveFeeder`, `draftSink`,
  `voiceLiveFinishKeyClaims`, `KeyLiveFinish`, `voiceLiveUsageNote`, `voiceChatLiveRunner`,
  `awaitReader`, `readLoop`, `PartialCount`, `FinishTimedOut`, `sessionCloseWait`,
  `voiceChatRecorderStaged`) resolved by `git grep -c <sym> HEAD -- cmd/agent internal/`.
  This check caught a real defect in my own first draft: it cited
  `TestChatVoiceLive_StopWordEndsTheTurnAndIsNotCommitted`, a test of the **losing**
  design that does not exist in this tree (the surviving design has no stop word) —
  replaced with what is actually here.
- **"the RED proof"** — the failing run quoted above, and the restore verified by an
  empty `git diff --stat`.
- **"the merge probe"** — the rc and staged set quoted above; probe worktree removed
  (`git worktree list` output confirms).
- **Rule 22** — `stat -c '%d %n' / /home/user/workspace` → `66307 /` and
  `66309 /home/user/workspace`: the worktree is off the `/` partition, and the probe
  worktree lived in the same (off-`/`) tree.
- **"the merge probe is reproducible, not one lucky run"** — the probe has been executed
  repeatedly across this run's tips (the earlier executions used the inline form quoted
  above). It is now also a script in the SESSION directory
  (`$SESSION_DIR/evidence/merge-probe.sh`; Rule 37: evidence stays out of the worktree),
  which prints the probe tip it is running at from `git rev-parse HEAD`, so no run has to
  be taken on faith. One execution's verbatim output (probe tip `823305a17`; my branch
  was at `c32b24b4b`):
  `bash $SESSION_DIR/evidence/merge-probe.sh` →
  `ADD_RC=0` / probe tip `823305a17a9cfa111d2b93a7d4c5c13ba6018849` /
  `MERGE_RC=0` / `staged_count=13` / `unmerged_count=0` /
  **`STAGED-SET-IDENTICAL-TO-BRANCH-SET`** /
  `probe_tree=f2b339b2fa0aeacd3a98ad3b3a29416ba473afa8` ==
  `branch_tree=f2b339b2fa0aeacd3a98ad3b3a29416ba473afa8` →
  **`MERGE-TREE-EQUALS-BRANCH-TREE`** / `ABORT_RC=0` /
  `probe_status_after_abort=[]` / `REMOVE_RC=0` / `my_status=[]`.
  The rc, the 13-path set and the tree *equality* are the invariants (they were the same
  at every tip); the tree *hash* is not, so it is quoted with the probe tip that produced
  it — and nothing here claims the script made a run it did not: the earlier tips were
  driven by the same three commands typed inline, verified by the same two programs.
- **"the DOC-INDEX rows are a UNION, not a replacement"** —
  `comm -23 <(git show master:devdoc/DOC-INDEX.tsv | cut -f1 | sort) <(cut -f1 devdoc/DOC-INDEX.tsv | sort)`
  → **EMPTY** (no master row is missing), and
  `comm -13 …` prints exactly 4 additions (`chat-pa-quick-ops-no-permission.md`,
  `chat-voice-live-streaming.md`, `task-01a11b5d-…-report.md`,
  `voice-module-four-defects-01a11da05.md`). Row counts: `master=2174` → `HEAD=2178`
  (`git diff --stat master...HEAD -- devdoc/DOC-INDEX.tsv` → `4 insertions(+)`). No
  landed test was deleted either: `git diff --name-status master...HEAD | grep -E '^(D|R)'`
  → empty, and no `_test.go` is in the `--diff-filter=D` set.
- **"the docs the epic asked to keep are on the ref, not merely in the worktree"** —
  `git cat-file -e HEAD:internal/doubaostream/session_teardown_guard_test.go` → present,
  `…:internal/doubaostream/session_test.go` → present, and the restored doc's row reads
  `devdoc/chat-voice-live-streaming.md  normative  normative: standing claim about master
  that names code that resolves (path); standing 0 / record 0  path cmd/agent/chat_voice.go`.
- **"the corpus is consistent"** — `gopi tool doccheck` → *doccheck: the devdoc corpus is
  consistent — every file under devdoc/ has a well-formed row*, rc=0. **Bare `gopi
  doccheck` is RETIRED and fails**; the working surface is `gopi tool doccheck`. The
  composite gates were re-measured once at the final tip in a single background command
  whose own first line prints that tip: `CGO_ENABLED=0 go build ./...` → `BUILD_RC=0`,
  `go vet ./...` → `VET_RC=0`, `go test ./internal/doubaostream/ ./internal/lineedit/
  -count=1` → `ok github.com/nick/gopi/internal/doubaostream 3.999s`,
  `ok github.com/nick/gopi/internal/lineedit 49.888s`, `TEST_RC=0`.

## Deliverable declaration (Rule 6d)

- **Branch:** `task/01a11dd0095a706b8aab797df42abc1e` (`git log --oneline -1` for the tip;
every commit above the delivery-point sync `70c6535e3` is this run's, and all but the
first two touch only this document)
- **Recovery:** `git show task/01a11dd0095a706b8aab797df42abc1e:devdoc/chat-voice-live-streaming.md`
  and `...:internal/doubaostream/session_teardown_guard_test.go`
- **Location:** the worktree is a **linked worktree of `/home/user/work/GoPi`** — it
  shares the object store and refs, so the commit already updates the branch in the
  main repo. **No push was needed and none was done** (Rule 6c).
