<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
# Handoff — 2026-09-20 (session compressed 60×, restarting for accuracy)

**Read this as the first message after restart.** It is self-contained: current
state, what is in flight, what was decided today, and what to do next.

---

## 0. Why the restart

This session hit 60 context compressions and I started making basic errors
(reporting completed commands as "interrupted"; using a weak branch-triage
predicate that mis-classified one branch). The operator's call to restart is
correct. Nothing is lost — everything is in git, the ledger, or here.

---

## 0.5 UPDATE at restart (2026-09-20 19:50) — verified after restart

**The 13 dispatches in §2 are no longer pending — they ran and delivered
branches. The pending queue now holds the NEXT layer (7 rows):**

```
01a0be7c-567d  rework feat/chat-stream-tty-wrap: cannot auto-merge into master (with diff)
01a0be7c-5904  rework feat/chat-ui-holes-u1u2u3v1: cannot auto-merge into master (with diff)
01a0be69-1da3  rework feat/declared-paths-undedeletable: cannot auto-merge into master
01a0be69-2473  rework feat/gc-declared-tree-set: cannot auto-merge into master
01a0be69-274a  rework feat/gc-tmp-residence: cannot auto-merge into master
01a0be69-298b  rework fix/scratch-gc-stale-window-scope: cannot auto-merge into master
01a0be69-2bd6  rework/rework/cmd-agent-master-reds: cannot auto-merge into master
```

Reading: **the text-UI three-holes task delivered as `feat/chat-ui-holes-u1u2u3v1`;
the GC tasks and the declared-paths task each delivered a branch; all of them
failed auto-merge and came back as rework.** This is §4's "带偏/drift" concern
showing up as a merge-backlog again — congestion at the merge step, not a failure
of the work. **Their conflict nature has NOT yet been examined** (open item).

**peerbox now has 3 workers running** (`gopi worker` pids 940485 / 945596 /
3423758). **HP still has ZERO workers** — §2's note and §7.1's question remain
exactly as written.

**master tip = `a129dbf7`** (this handoff). Main worktree clean, HEAD=master.

### 0.6 Bubble Tea on peerbox: the dependency wall is REMOVED (verified)

The operator chose "move the HP tasks back to peerbox". The blocking concern was
that peerbox cannot reach `proxy.golang.org` and its module cache had **zero**
charmbracelet entries. **That wall is now gone** — measured, not assumed:

- The HP branch `feat/gopi-cli-bubbletea` touches `go.mod`/`go.sum` (+24/+36).
- Its `go.sum` names **43 modules**; 10 were missing from peerbox's cache:
  `aymanbagabas/go-osc52` · `erikgeiser/coninput` · `lucasb-eyer/go-colorful` ·
  `mattn/go-localereader` · `mattn/go-runewidth` · `muesli/ansi` ·
  `muesli/cancelreader` · `muesli/termenv` · `rivo/uniseg` · `xo/terminfo`.
- All 10 (+ the whole charmbracelet family) were **rsynced from HP's
  `$GOMODCACHE/cache/download/`** into peerbox's. Recheck: **still_missing=0**.
- **Decisive test** — isolated worktree at the HP branch tip (`145738c1`),
  fetched via `git bundle` (135M; temp files removed on both sides):
  `GOPROXY=off go build ./...` → **rc=0** · `GOPROXY=off go vet ./...` → **rc=0**
  · `CGO_ENABLED=0 … go build -o … ./cmd/agent/` → **rc=0**, 24.6 MB, `ldd`
  reports **`not a dynamic executable`** (satisfies the operator's
  runtime-zero-dependency definition).

**Consequence: Bubble Tea can be built on peerbox offline.** "peerbox can't fetch the
deps" is no longer a reason to keep that work on HP.

> **Do not re-copy these modules.** They live in peerbox's GOMODCACHE permanently.
> For a *new* dependency the recipe is: read the module list from the branch's
> `go.sum`, diff against `$GOMODCACHE/cache/download/`, rsync only the missing
> ones from HP (HP reaches `goproxy.cn`), then prove it with
> `GOPROXY=off go build ./...`.

---

## 1. What was decided today (these are settled, do not re-litigate)

### 1.1 UI direction — **decided**
- **Bubble Tea is the main surface.** The plain text `gopi chat`/`gopi cli` path
  **stays** as the zero-dependency fallback **and as the debugger for the
  Bubble Tea UI** (they share the engine: loopback SSE client of `POST /api/chat`).
- The operator's own framing: the reason to prefer a text UI over web was
  *"agent can be aware its layout natively"* — he then doubted that assertion.
  **Measured answer: the real invariant is "UI state must be assertable from a
  readable model", not "it must be a text browser".** HP's
  `internal/uiclient/model.go` (685 lines) proves Bubble Tea satisfies it too.

### 1.2 The text UI's "primitive" feel is **three named holes, not a bad foundation**
(study: `devdoc/cli-ui-feasibility-study.md`, on master as of `8ce29508` era)
- **U1** input invisible past 4 rows — `editorRowsCap = 4` is a hardcoded
  constant (`internal/lineedit/region.go:57`), not derived from terminal height.
- **U2** the streamed answer is never wrapped by the app — `cmd/agent/chat_stream.go:303`
  writes raw deltas; the wrapping you see is the *terminal's*.
  (Asymmetry: `internal/lineedit/answer.go` **does** wrap correctly, with a test.)
- **U3** wide-terminal cursor row under-reported (`cols=200 → cursor=(0,104)`).
- **Not a defect**: CJK layout math is already correct (`runeCells` returns 2 for
  wide runes, `wideRanges` complete, 13 render tests pin exact screen text).
  "Cannot input Chinese" = the editor has **no IME**, and that is the terminal's job.
- **V1** (do this before adding colour): teach `Screen` to record **SGR per cell**
  so styling is assertable as text. Without it, colour = raw escape bytes and the
  CLI's one structural advantage is lost.

### 1.3 `session_id` in `tasks` — the operator's diagnosis was right
- **`tasks.session_id` cannot hold what it is read as.** `sessions/<task_id>/` is
  **1:N** (129 directories hold >1 run log: a killed run plus its resume). A 1:1
  column names only the first, and it is frequently **wrong**:
  `01a0ba14` ledger `847d61f3` ≠ actual `eab92eca`; `01a0ba37` ledger `0db61d4e`
  ≠ actual `5ee7ef8a` (and `5ee7ef8a` appears **nowhere** in the ledger).
- **`resume` does not need it.** `TaskResumeTarget` (`internal/tools/taskproc.go:868`)
  goes `task_id → row.session_id → taskRawLogPathFromSession(sid)` →
  `taskFindSessionJSONL(sessionsDir, sid)` = **a whole-tree search for a file whose
  directory the task id already names**. Verified: with only a task_id you can
  list `sessions/<task_id>/*.jsonl` and each log's own `session_start` carries
  `{sessionId, taskId}`.
- **The unification (operator's own conclusion):** a chat's `session_id` **IS its
  task_id** — one kind of id, "the name of this run's road", minted either by the
  submitter (`task run`) or by the run itself (bare prompt / chat / web);
  `sessionDirForID` (`internal/agent/session.go:63`) already does exactly this.
- **Where the split came from:** `internal/agent/taskregister.go:229` comment
  *"consolidation: task_id and session_id are different entities"* (2026-09-05).
  Before it, auto rows passed the session uuid as **both** ids — **184 rows still
  carry `task_id == session_id`** (span 08-28…09-02).
- **`chats.session_id` is correct and must not change**: it is that table's
  PRIMARY KEY, it is the disk directory name (`sessions/<sid>/session.jsonl`),
  `parent_session_id` forms the continuation chain, and the doubao importer
  deliberately uses the file-name stem as the session id.
- **Display fix**: `task show` should show **run-count / turn-count**, not a bare uuid.

### 1.4 `session_id`-adjacent, already dispatched
`01a0bbd6-fe03` (peerbox, p1) — brief `briefings/brief-run-identity-unification.md`
(commit `f5e851ab`). It supersedes an earlier plan of mine (adding a `run_uuid`
column beside the weak pointer) — **that plan was wrong**; the task is to unify
on one id per run and stop reading the second column.

### 1.5 voice_input — **deferred, not cancelled**
Operator: *"麦克风是真正的难点…这个我想还是不要着急，等界面UI搞定再行，否则是白费力气."*
Hard ordering dependency: UI undecided → input-box shape undecided → voice's
"where does the text go" undecided → doing it now is rework.
**Measured assets (reusable, do NOT re-measure):** `.mp3` works on all three
channels byte-identically — file (`gopi voice_input x.mp3`, 220 s → 143.5 s),
pipe (`cat x.mp3 | gopi voice_input --server --stdio`, 1760430 B in / 89.6 s,
**output 3165 bytes = identical to file mode**), cross-machine
(`--client --stdio | ssh host '… --server --stdio'`). Also `.wav`/`.flac` work;
bare `.pcm` works **only** on `--stdio` (WAV fallback wrapper in
`voiceMaterializeAudio`, `cmd/agent/cli_voice_server.go:352`). **Zero integration
work.** Two contract defects are dispatched (`01a0bbe5-033a`): help advertises
`ogg` the engine disclaims (`supported: wav/mp3/flac`), and the bare-PCM file
refusal does not name the `--stdio` shape that accepts it.

---

## 2. In-flight dispatches (all on peerbox unless noted)

| id | what | brief |
|---|---|---|
| `01a0bbd6-fe03` | unify run identity (one id per run) | `briefings/brief-run-identity-unification.md` |
| `01a0bbca-ee2b` | **HP** — land the Bubble Tea UI (4047 lines already written) | `briefings/brief-land-bubbletea-ui-hp.md` |
| `01a0bbca-dddf` | text UI three holes + V1 | `briefings/brief-text-ui-three-holes.md` |
| `01a0bb5c-d706` | **HP** — merge HP chat UI: unify duplicated SGR consts | (see ledger) |
| `01a0bbe5-033a` | voice format contract (help lies about ogg) | `briefings/brief-voice-format-contract.md` |
| `01a0bbf1-82f1` | declared paths must survive any sweep (GOTMPDIR incident) | `briefings/brief-declared-paths-survive-sweep.md` |
| `01a0bb86-6631` | GC scope: GOTMPDIR / raw / session raw.log | `briefings/brief-gc-scope-gotmp-and-rawlogs.md` |
| `01a0bb8e-816e` | GC scope: `~/workspace` (4th blind spot) | `briefings/brief-gc-scope-addendum-workspace.md` |
| `01a0bb92-c1f8` | GC scope: `/tmp` (check disk writes, not just script text) | (see ledger) |
| `01a0be63-a250` | classify the 8 unclassified devdoc files + 1 missing banner | `briefings/brief-classify-unclassified-devdocs.md` |
| `01a0be86-be69` | **`gopi cli "prompt" --once`** — non-interactive single turn | `briefings/brief-cli-once-noninteractive.md` |
| `01a0be86-c0e0` | feasibility: CLI as the model's eyes+hands | `briefings/brief-feasibility-cli-eyes-hands.md` |
| `01a0be86-c2db` | web research: how others develop/verify UI; is UI-observation-as-tool a thing | `briefings/brief-research-ui-dev-prior-art.md` |

**Note:** HP has **no worker running** — anything dispatched on HP sits pending
until someone starts one (`setsid` required; Hermes-launched processes die with
the session). Consider whether the HP dispatches should move to peerbox.

---

## 3. HP branch cleanup — done today (24 → 13)

**Predicate used (and the corrected version):** not `git grep <symbol> master`
(weak — it matches unrelated text and mis-classified one branch), but
**"are the branch's newly ADDED files present in master?"**:
`git diff --name-only --diff-filter=A master...<branch> -- '*.go'`.

### A tier — requirement is DEAD → archived (the merger daemon was retired)
`feat/merger-pure-cronjob` → tag `archive/feat/merger-pure-cronjob` (`b79574c4`)
`fix/merger-park-worktree` → `86b891c7`
`fix/merger-status-branch-account` → `24eded5c`
`fix/merger-status-three-defects` → `fe8780f9`
`fix/rework-ff-convergence` → `4f05fa48`

Rationale: `gopi merger --status` says the resident daemon is **retired**
(`devdoc/merger-retired.md`; operator's words *"buggy and useless, it cannot be
interrupted — retire it/disable it and re-develop it"*). The merger *function*
lives on as the one-shot `gopi merge`. **The operator's criterion: "不是merge的成本，
而是那个需求是否变了 … 不要带偏了"** — improvements to an abandoned direction must
not be merged back, however easily they would merge.

### B tier — content already in master → archived (merging back = regression)
`feat/web-m1-b4b7` → `9ebc9554`; `feat/web-m2-b5b8b9b12` → `98fb2942`;
`feat/worker-run-entry2` → `3e425d13`; `fix/serve-lock-race` → `dd9c53b4`;
`feat/hand-archive-gate` → `598fcf02`. (All behind 1196–1292 commits; several
commits are literally `wip(...): rescue uncommitted work before HP cleanup` —
salvage, not development output.)

### ⚠️ One branch was mis-classified and RESTORED
`feat/chat-colour-palette` — its 4 new Go files (`internal/lineedit/colour.go`,
`compose.go`, `palette.go`, `styled.go`) are **NOT in master**, and a colour
palette is part of the **live** UI direction. It is **back** (tip `4f92dc52`,
ahead=4, archive tag removed). **This is the error the corrected predicate fixes.**

### C tier — requirement alive + genuinely undelivered → candidates for back-port
`feat/gopi-cli-bubbletea` (4047 lines, the current main line) ·
`feat/chat-turn-layout` (1857 lines) · `feat/chat-colour-palette` (2107 lines,
the V1 substance).
**They are behind peerbox/master by 476 / 160 / 160 commits**, so per the
operator's 2026-09-14 rule (**never develop on someone else's branch**) the
back-port should be a **re-implementation from peerbox/master**, not a rebase of a
1000+-commit-stale branch.
Others still to triage: `docs/requirements-task-ledger`, `feat/req-b1-task-ledger`,
`feat/req-b10-observability`, `feat/req-b8-cli`, `feat/scan-tip-at-mint`,
`fix/chat-help-command-arg`, `fix/cmd-agent-suite-red`,
`fix/serve-lock-refusal-diagnosis`, `fix/standards-stylegate-master-red`,
`feat/sgr-aware-grid` (already in HP master).

---

## 4. The operator's two open strategic questions (his words)

> "我担心的唯一的就是HP的这些是不是都是过时的任务？回流会不会把peerbox的开发带偏了？"
> "remove the branch may not cost too much effort, right? … we have now 170K line
> of code and thousands of function, so, my concern is it become out of control"

**The measured answer given today, and it still stands:**
- **Yes, much of it was stale** — 10 of 24 branches were dead-or-already-absorbed
  (now archived, refs kept as tags).
- **"带偏" is a real risk, not a hypothetical** — `feat/web-m2-b5b8b9b12`
  (ahead=19, the largest branch) had its content **already in master**; merging
  it back would have been a regression.
- **The right frame is not "wait for the POC verdict"**: a merge is **reversible
  in one command** (`git revert -m 1 <merge>` + delete the branch), while
  **divergence is not** (it deepens daily). Back-port early, revert if wrong.
- **"Out of control" is not the LOC count** (178k production lines with a 1.31:1
  test ratio is healthy). It is **fork depth** (476 commits behind), **un-triaged
  branches** (was 24), and **a stuck un-finished merge** (found and cleared today).

---

## 5. Incidents today (both mine — recorded so they do not repeat)

1. **I deleted `GOTMPDIR`.** A workspace sweep used "not a git tree + not held by
   a live process → delete"; `~/workspace/gopi-go-tmp` is `GOTMPDIR`
   (`~/.config/go/env`) and was merely *idle*, so it was removed. Every `go build`
   then failed with `go: creating work dir: stat …: no such file or directory`.
   **`mkdir -p` of that path fixed it; `go build` and `go vet ./...` both pass, so
   the three merges that were suspected are innocent.** Fix dispatched
   (`01a0bbf1`). The operator's rule: *"不用的东西就把它删除，但在使用的要知道他在用"*
   — **idle ≠ unused.**
2. **I mis-triaged `feat/chat-colour-palette`** using `git grep <symbol> master`
   as the predicate. Corrected predicate above; the branch is restored.
3. **I twice reported an "interrupted" command that had in fact succeeded** —
   the output was lost, not the work. Do not report "interrupted" without
   checking the effect.

---

## 6. Standing rules that mattered today (keep obeying them)

- **PA does not write production code, debug, or run long tests.** Write briefs,
  dispatch, read reports, analyse. `git branch`/tag/merge *mechanical* operations
  are in scope; code is not.
- **Brief + task, never one alone.** The brief is the spec in git (committed);
  the task is the ledger row. `payload` is a one-time snapshot at submit.
- **Before any branch/worktree action: check which branch the worktree has
  checked out** (`git symbolic-ref --short HEAD`). A dirty main worktree on a POC
  branch is *that branch's own work*, not "master got dirty".
- **Never delete an unmerged branch** — `git tag archive/<branch>` + keep the ref.
  (`git branch -d` only succeeds when already merged; that failure mode is the
  signal, not an obstacle.)
- **A requirement that has been abandoned must not be merged back**, no matter
  how clean the merge would be. That is the "带偏" rule.
- **Check `git log`/`ps` before claiming anything about running processes.** The
  merger log is a historical file; `gopi merger --status` is the authority.
- **`make fmt` = `gofmt -l -w .`** — it reformats the *whole tree*. Never run it.
  Check with `gofmt -l .`.
- Long commands: give them timeouts; background them if genuinely long.

---

## 7. Do these first after restart

```bash
cd /home/user/work/GoPi
git log --oneline -5                                  # where master is
./bin/gopi task list --status pending | head -20      # the dispatched queue
git log --oneline -3 --stat                           # today's briefs are committed
```

Then, in order:
1. **Ask the operator** whether to start a worker (peerbox) so the dispatched tasks
   actually run — and whether the **HP** tasks should be moved to peerbox, since
   **HP has no worker at all**.
2. **Back-port the C tier** (Bubble Tea + chat-turn-layout + chat-colour-palette)
   by **re-implementing from peerbox/master**, not rebasing stale branches.
3. Continue triaging HP's remaining 13 branches with the **corrected predicate**
   (do the added files exist in master?), and check requirement-liveness per the
   `merger daemon` precedent.
