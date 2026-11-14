<!-- doc-class: normative -->
# `gopi chat --replay` is really read-only — the writes it used to perform, and the one flag that stops them

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a12d0b-e0ff-71ad-9f7b-f1c78fbf616a` (epic `chat-replay-is-really-read-only`).
Branch: `task/01a12d0be0ff71ad9f7bf1c78fbf616a`.

## The defect, measured before any change

`gopi chat --replay <session>` already took two things out of the run's hands:
the **model** (an in-process stub serves the recording's responses) and the
**tool dispatch** (`agent.SetToolExecutor` answers every recorded call with the
recorded result, so `executeToolDefault` is never reached). A previous task
(`01a12662`) relocated the run's own **session** into scratch
(`cmd/agent/chat_sessions_root.go`).

> **That relocation is GONE** (task `01a15981`): the mint is now refused at its
> source and a replay lands nowhere. See `devdoc/chat-replay-mints-no-session.md`
> — the relocation moved a write instead of stopping it, and the two were
> indistinguishable from the operator's own sessions tree.

What none of that touches is every *other* write a real REPL performs simply by
being a real REPL. Measured on this machine, with the replay impl at
`0b3c1de29` and **before** the fix in this branch:

```
$ script -qec "./bin/gopi chat --replay bd3ecf46-d9f7-49d6-8576-798cde8e8a08" /dev/null
$ ls -l --time-style=full-iso ~/.gopi/chat_session_id_2.txt
-rw------- 1 nick nick 162 2026-10-12 06:26:25 …   # mtime was 2026-10-11 21:29:07
$ cat ~/.gopi/chat_session_id_2.txt
1293979:/home/user/workspace/gopi-test-scratch/queue/queue-runtime-ui-session-1293979/chatreplay-sessions-100619221/sessions/122864df-…
```

Three writes, all into the **operator's own profile**, none of them a copied
session:

| # | write | evidence (before the fix) |
|---|---|---|
| 1 | the profile's chat slot registry (`chat_session_id_<n>.txt`) | slot 2 rewritten with the replay's *scratch* identity; `sha256` moved `70b54f7b…` → `753bf291…` |
| 2 | the editor history file (`~/.gopi/chat_history`) | `+3840` bytes; the run TYPES the recording's prompts, and the editor records each one as if the operator had typed it |
| 3 | the `chats` table in `~/.gopi/memory.db` | a row appeared for `122864df-…`, a session that exists only under `gopi-test-scratch/` |

Write 1 is the one with a downstream victim: `notify.cmd` globs the slot files and
delivers to the session they name, so the replay **stole a live slot** and, for as
long as it held it, every delivery that should have reached that chat went to a
directory nobody was reading.

## The mechanism: one authoritative flag, one question

`internal/replaymode` is a **leaf package** — a `sync.RWMutex`, a bool and a
source string, and no imports at all. It exports exactly five things: `Arm`,
`Disarm`, `Active`, `Source`, `Refuse`.

**One definition, asked by every writer** (Rule 13/14). The alternative — a
`replay bool` threaded through the writers — was rejected because the writers sit
in three packages on four code paths (`cmd/agent`'s slot registry and history
path, `internal/agent`'s chats-table hooks, and the tool dispatch), none of which
is a caller of the replay driver. Four parameters would be four answers to one
question, and the fifth writer added later would simply forget.

**Arming order is the guarantee.** `startReplay` arms the flag the moment the
recording is loaded and *before* `newAgent` — i.e. before the session logger, the
surface, the slot registry or any chats-table hook exists. A flag armed after the
writer it is meant to stop has already run is prose. `cleanup` disarms it, so the
flag describes the run in flight rather than the process's history.

**There is no escape hatch.** Nothing outside the binary can arm or disarm it:
the package reads no environment variable, no settings key and no CLI flag, and
`replaymode_test`-style red proofs are unnecessary because there is nothing to
misspell. A switch a caller could turn *off* would defeat the guarantee it exists
to provide.

## The write-path inventory

`replaymode.Active()` is asked at **16 production gate sites** (re-measured
2026-10-26 on this ground's cut point `620cbbf`:
`grep -rn "replaymode.Active()" --include=*.go internal/ cmd/ | grep -v _test`
after dropping comment lines → 16 calls in 12 files; the figure in the paragraph
below, "six", was the 2026-10-20 reading and is kept only because the table's own
rows are the real inventory). The count is now MEASURED by a machine rather than
by a grep in prose: `TestChatReplayDumpCatalogueIsExhaustive`
(`cmd/agent/chat_replay_dump_catalogue_test.go`) walks the tree and fails on any
production site that `replayActiveSiteRows` does not claim — so this sentence can
go stale again without the suite noticing, but the CATALOGUE cannot.

| # | write | where it is reached | how it is answered | evidence |
|---|---|---|---|---|
| 1 | profile chat slot registry `chat_session_id_<0..9>.txt` | `chatSessionSlotRegister` (`cmd/agent/chat_session_slots.go:353`), called from the arm point `installChatSurface` | **short-circuited**: returns `(empty entry, false)` + one stderr refusal | before: slot 2 mtime `2026-10-11 21:29:07`→`2026-10-12 06:26:25`, sha `70b54f7b…`→`753bf291…`. after: 10 slots byte-identical (`SLOTS_AND_HISTORY_BYTE_IDENTICAL`); refusal line printed |
| 2 | editor history `~/.gopi/chat_history` | `chatHistoryPath()` (`cmd/agent/lineedit_wire.go:1183`), reached by `lineedit.LoadHistory` at `lineedit_wire.go:960` | **short-circuited**: path answers `""` = the editor's documented memory-only mode; the refusal is announced at the load site | before: `146808`→`150648` bytes. after: byte-identical; refusal line printed |
| 3 | `chats` table in `~/.gopi/memory.db` | `chatRegisterEnabled()` (`internal/agent/chatregister.go:57`), the ONE gate all three hooks ask (`RegisterChatSession` / `RecordChatTurn` / `CloseChatRow`) | **short-circuited**: answers false, so no hook reaches `tools.ChatEnsureRow` | before: `[gopi] chat table: chat row ensured: 122864df-…` ×1, `chats` count 179. after: **0** `chat table` lines in the whole run |
| 4 | every tool dispatch | `agent.executeToolCtx` (`internal/agent/loop.go:6398`) — the real read/write/bash/executor path | **answered from the recording**: `SetToolExecutor` is installed before the first turn, so `executeToolFn` is taken and `executeToolDefaultCtx` is unreachable. An unrecorded call is refused **by name**, not silently: `[gopi replay] no recorded tool_result for <id> (<tool>)` | run2 served 72 recorded results, all `recorded=true`, `unrecorded: 0` |
| 5 | **the auto-memory stores** — `distilled_qa` rows for a self-note, a captured response, and the model's own `memory` tool `store` | ONE funnel, `memoryStoreReplayRefused` (`internal/agent/memory_replay_guard.go:43`), asked by `captureSelfNote` (`selfnotecapture.go`), the response capture (`selfnotecaptureresponse.go`) and the `memory` store action (`loop.go`). The check sits **before** the dedup map and the provenance stamp, so a refused note is not recorded as "already captured this round" | **short-circuited** + one refusal per attempt; the `memory` tool answers with the refusal sentence, never an empty string (an empty `tool_result` reads as "stored") | before (measured on session `2c394553-…`): 10 rows in the operator's `distilled_qa`. after a real pty replay: **10 rows, maxid unchanged**, `sha256(memory.db)` byte-identical `31f00a77…` |
| 6 | **the memory prefetch reads** — both legs: the typing path and the sealed-turn path | `chatPrefetch.fetch` (`cmd/agent/chat_prefetch.go:465`) and `startTurnLookup` (`cmd/agent/chat_prefetch_turn.go:331`) — BOTH, because the turn leg calls `prefetchLookupDetailed` directly and never passes through `fetch` | **short-circuited** at the entry point: no stat, no dial, no SQL; the record line says `outcome=refused` with `hits`/`ms` **absent** (not `0` — a retrieval nobody made is not a retrieval that found nothing) | guarding `fetch` alone left `Counter({'hits': 8, 'adopted': 8})` in a real replay's `memory-prefetch.jsonl`. With both legs: `Counter({'refused': 8})`, sample `{'outcome': 'refused', 'hits': None, 'ms': None}` |
| 7 | **the screen dump** `chat-screen.dump` (+ its `.json` twin) | `chatResolveDumpPath` (`cmd/agent/chat_dump.go`), whose middle branch was `filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiclient.ChatDumpFileName)` — and under a replay `ag.SessionLog` **is the recording's own logger**, so that branch wrote the dump INTO the session directory being re-run | **REDIRECTED, not refused**: the resolver asks `replaymode.Active()` FIRST and answers `chatReplayDumpPath()` — the chat's own scratch class, the class the ordinary resolver already falls back to. The artifact still exists (it is a visualization product, and a replay is exactly the run whose picture someone wants); it just cannot land in the recording tree | a real replay of `77d26121-…`: all four files in the recording dir byte-identical before/after (`session.jsonl fa7e675d…`, `metadata.json 6372ac4e…`, `console.log 5a9c43cc…`, the recording's own `chat-screen.dump 2c2956ad…`), `diff` empty; the replay's own dump went to `…/queue-runtime-ui-session-578115/chat-screen2112497166/chat-screen.dump`, 4297 bytes. RED-proof with the two production files reverted: `TestChatReplayDumpStaysOutsideTheRecording` fails naming the path INSIDE the recording tree |

Writes **already** relocated or neutralised before this task, listed so the
inventory is complete rather than merely new:

| write | why it is safe |
|---|---|
| the run's own session tree (JSONL, `.raw.log`, `console.log`, self-record) | **The relocation this row used to describe is GONE** (task `01a15981`): `applyChatReplaySessionsRoot` / `cmd/agent/chat_sessions_root.go` no longer exist, and the mint is refused at its source (`agent.NewRawLoggerForRun` / `agent.NewSessionLoggerForRun`, row 7 of the catalogue's own list). **The screen dump is NOT covered by this row any more** — it was named here, which is precisely how it escaped the inventory: the row claimed a relocation that had already been deleted, so the one writer whose path is not a session MINT read as accounted for. It is row 7 above now, with its own gate. |
| a human's keystrokes changing the line | `lineedit.Editor.SetInputLocked` + `InputLockDrops` (the pre-existing keyboard defender, unchanged here) |

Writes **deliberately out of scope** (operator's ruling):

- `scripts/notify/notify.sh` is not the object of this fix. The slot registry is
  this run's problem; the subscriber's own behaviour is not.
- A `bash` tool call inside a *live* chat mutating the tree is the chat write
  boundary's business (`internal/tools/chat_write_guard.go`), and in a replay
  that path is unreachable anyway — call 4 answers every call from the recording.

## Honest limits of the guarantee

- **The round stamp was a THIRD defect, found while proving the first two**
  (2026-10-20). Every auto-memory row of a chat run carried
  `round_index = 0` — the absence value — while the session's own log held 52
  `request` events. The cause is not the caller: the chat surface's ONLY round
  writer is `stampHippocampusRound` (`internal/agent/chat_ops.go:158`), which
  returns EARLY when `a.Hippocampus == nil`, and that host is nil whenever
  `GOPI_HIPPOCAMPUS` is off (it is off on this host). The cursor therefore kept
  its zero value for the whole conversation. `storeRoundNumber` now falls back to
  the session's own `RequestsSoFar()` — the definition
  `selfrecord_noteindex.go`'s `noteRoundNumber` already used — instead of
  reporting "no round" for a conversation that demonstrably had 52 of them.
  Rows already written with 0 are NOT backfilled (that is a separate, explicitly
  authorised write to the operator's ledger).

- **Short-circuit, not sandbox.** The flag stops the writers *this run's code
  paths* reach. A writer reached by a shape nobody has measured yet would not be
  stopped, which is exactly why the inventory above is enumerated rather than
  asserted, and why `replayRefusalCatalogue()` prints the same list at arm time.
  **"Enumerated" is now checked, not invited** — see the screen-dump section at
  the end of this document: `TestChatReplayDumpCatalogueIsExhaustive` walks every
  production call site of `replaymode.Active()` and fails on a gate with no
  catalogue row, a row with no gate, or a row whose phrase the printed sentence
  does not carry. Before that machine existed this bullet ended with "a reader can
  compare the sentence against the four call sites", and the screen dump was the
  site that comparison never reached.
- **`replaymode` is process-global.** Two replays in one process are not a shape
  this verb has; `Arm` is idempotent and keeps the first source rather than
  silently repointing the announcement.
- **The `memory.db` family moved in run2** (`memory.db`, `-shm`, `-wal` mtimes).
  Those are the **live worker/merger ticks on this machine** writing the ledger
  for their own reasons — not this run: the discriminating measurement is the
  `chat table` line count (2 → 0) and the absent `chats` row. A file mtime alone
  cannot attribute a write, and the ledger is written by a resident process on
  this host.

## Rework pass — the branch was merged into master and RE-verified from there

The delivery above landed on `origin/feat/replay-readonly-01a1597d` (6 commits).
A SIGINT'd session was re-run to carry it onto **master** (base `8d29ad7`), and
that pass re-ran the acceptance rather than trusting the branch's own report
(Rule 31). Merged as `a890284` — a clean merge, 16 files / +689 lines, no
conflict, and the tree is `git status --porcelain`-clean.

Re-measured on the merged tree, with the binary that merge produced
(`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, sha256 `7d27d6db…`):

```
$ GOPI_MEMORY_PREFETCH=1 script -qec './bin/gopi chat --replay 2c394553-… --no-dump-screen -v' /dev/null
```

| claim | before | after |
|---|---|---|
| `sha256(~/.gopi/memory.db)` | `e729e9b3…` | `e729e9b3…` (identical) |
| `distilled_qa` rows / max id | `11968` / `12016` | `11968` / `12016` |
| `chats` rows | `218` | `218` |
| that session's rows | `12`, `round_index` all 0 | `12`, unchanged |
| refusals on stderr | — | 10 turn-boundary prefetch + 3 auto-memory note + 9 captured response |

Three facts the re-run corrected or added to this document's own claims:

1. **`memory-prefetch.jsonl` does not exist for a replay any more**, and that is
   now the CORRECT observable, not a missing one. `1a3466c` deleted the scratch
   sessions root, so a replay mints no session directory at all; the prefetch
   record is opened inside that directory (`openPrefetchRecord`), finds none, and
   says so — `[gopi] memory prefetch: on, but this session has no session
   directory — no prefetch record will be written`. The acceptance criterion
   "every line in this run's memory-prefetch.jsonl is `refused`" therefore has
   to be read against the run that still HAD a tree (`Counter({'refused': 10})`,
   measured pre-`1a3466c`); on the merged tree the same property is observable as
   **10 refusal sentences on stderr and no file**, which is stronger — nothing was
   written where nothing may be written. The regression tests still pin the
   record-line contract directly (`chat_replay_prefetch_test.go` drives
   `chatPrefetchFor` with an explicit directory, so the `refused` line with
   `hits`/`ms` absent is measured on the merged tree).
2. **The `sha256` reading is only valid because it was taken around a run with no
   other writer active in that window** — and this host has resident writers
   (`gopi worker`, `gopi chat --tools` were alive in the earlier pass). The
   rework's reading held identical across 22:38:09 → 22:40:16 with those two
   resident processes alive, so the identical value is evidence here rather than
   luck; the row counts above are the second, independent arm.
3. **`round_index` is still 0 for the historical rows** (`0=>12`) and this pass
   did NOT write any new row for that session — which is exactly the guarantee
   under test. The fallback in `storeRoundNumber` is a FORWARD fix (proven by
   `TestStoreRoundNumberComesFromTheSessionWhenTheChatCursorIsUnstamped`, a real
   `SessionLogger` + real `LogRequest`), never a backfill.

## Delivery round 2 — the pull that could not replay, and the A/B that re-proves the guard

Round 1's delivery stopped HERE, verbatim:

```
error: could not apply 5953d23... fix(replay): a replay writes nothing into the operator's ledger
hint: Resolve all conflicts manually, mark them as resolved with
hint: "git add/rm <conflicted_files>", then run "git rebase --continue".
```

`git pull --rebase` was asked to replay a commit against a base it was never
written on, over regions `origin/master` had itself rewritten since
(`bbfdbf7` removed the `chat --replay` step keys; `7b441bf` restored
`internal/replayplay` untouched) — and two of the run's commits were MERGES,
which `--rebase` flattens and re-applies one by one. The conflict is therefore
structural, not a one-off, and re-running it reproduces it exactly:

```
$ git merge --no-edit origin/master          # ancestry now contains origin/master
$ git pull --rebase origin master
error: could not apply 5953d23...            # still
$ go build ./...
cmd/agent/chat_replay.go:136:1: syntax error: unexpected <<, expected expression   # conflict markers
```

The landing shape that removes the replay instead of surviving it: **one commit
whose base already contains `origin/master`**. `git pull --rebase` then has
nothing to replay — measured on the delivered tree: `Current branch master is up
to date.`, no rebase started. The pre-squash tip is kept on tag
`rework/pre-squash-01a1597d-r1` (`31819e07`).

**Content identity is the durable acceptance; a tree-id equality is not.** The
first landing (`32a57c8`) had tree `6f1f7079…`, byte-identical to the tag's — but
a tree id describes ONE commit, and the tree under it then moved for a good
reason: the delivery pull fast-forwarded `origin/master`'s 5 new commits
(`de8bcd5`, touching `scripts/g6-top-level.awk`, `cmd/agent/inputpath.go`, two
tests and a devdoc) beneath this branch. So what is checked, and what a reader
can re-check at any time, is the FIX's own patch:

```
$ git diff be65b35 31819e07 | git patch-id --stable     → 16399c81fc52e7f26f342fafd305fb5bc4ae426d
$ git diff de8bcd5 e6c6f12 | git patch-id --stable     → 16399c81fc52e7f26f342fafd305fb5bc4ae426d
```

Same patch, re-based onto a base that already held the upstream — which is the
whole claim. `origin/master`'s step-key removal stands; the two sides are
disjoint, so no `MERGE NOTE` is owed at any site.

### Rule 31 A/B — same replay, ONE variable (the binary)

Two `gopi tool sandbox create` sandboxes, each with its OWN ledger seeded by
`GOPI_SANDBOX_LEDGER_SEED` (a copy of an empty-but-initialised database, so
"0 rows written" is a reading and not a missing table), the same recording
(`2c394553-…`) copied in, and the same command:

```
$ GOPI_HOME=$SB GOPI_SETTINGS=$SB/.gopi/settings.json \
  script -qec "<binary> chat --replay 2c394553-e1e9-4953-ae34-79938f994098 --tools --no-color" /dev/null
```

| observable | merged tree (`32a57c8`) | `origin/master` (`be65b35`) |
|---|---|---|
| `[gopi replay] refused to store …` (auto-memory notes + captured responses + the memory tool's own store) | **12** | **0** |
| `[gopi replay] refused to run …` (both prefetch legs: `chatPrefetch.fetch` + the sealed-turn `startTurnLookup`) | **10** | **0** |
| `refused to mint` / `register` / `append` / `open` (the four writes `replaymode` already stopped) | 1 each | 1 each |
| `distilled_qa` rows in that sandbox's ledger after the run | **0** | 0 — NOT a like-for-like reading: the two arms differ in the REFUSAL SENTENCES above, which is the observable; where the control's stores went is not something this pass measured |

The four pre-existing refusals are IDENTICAL on both sides, which is what makes
the two new families attributable to this diff rather than to the replay
machinery in general.

**Correction (same pass, Rule 41 discipline against my own prose).** An earlier
draft of this paragraph claimed the merged run's `memory-prefetch.jsonl`
carried `"outcome":"refused"` lines. That was NOT observed and is withdrawn:
the file found in the sandbox is the RECORDING's own prefetch journal, copied
in before the run, and it is byte-for-byte the source file (10166 bytes, 31
lines, mtime 23:06:54 = the copy, while the run finished at 23:08). So the
stronger reading is the true one: **the replay appended nothing to it at all** —
`hits`/`shared` lines and all, the journal was not touched, matching the
`[gopi] memory prefetch: on, but this session has no session directory — no
prefetch record will be written` sentence on stderr. The `refused` record line
with `hits`/`ms` ABSENT is pinned where it is actually observable — the unit
test `cmd/agent/chat_replay_prefetch_test.go` (:111/:137/:233 drive
`chatPrefetchFor` with an explicit directory) — and is NOT claimed as a runtime
observation here.

The control arm's ledger reads 0 — but that is NOT a like-for-like reading and
is not offered as one: the two arms differ in the REFUSAL SENTENCES above, and
where a control's stores physically landed is not something this pass measured.

The operator's own ledger was measured directly, before and after the whole
pass, with a read-only open:

```
$ python3 -c 'sqlite3 file:/home/user/.gopi/memory.db?mode=ro ... select count(*), max(id)'
before the A/B:   distilled_qa = 11974   max id = 12022
after  the A/B:   distilled_qa = 11976   max id = 12024
$ ... where id > 12022 → (12023,'规则文件拆分 GOPI.md→PROJECT.md…', session 76652fcd-…),
                          (12024,'response',                       session 76652fcd-…)
$ ... select count(*) from distilled_qa where session_id like '2c394553%'  → 12
```

The count MOVED, and attributing it honestly matters more than a tidy number:
both new rows carry session `76652fcd-c9b4-4f9d-a00e-6f0dc34cb190` — a
DIFFERENT, concurrent run on this host (this machine has resident workers), not
the replayed `2c394553`. The replay's own row count is still 12, i.e. the run
under test wrote nothing; the +2 belongs to somebody else. That is also why a
file mtime or a row count alone cannot attribute a write, which is the exact
trap this host keeps setting. The sandbox arms remain the attributable
measurement (private ledger, one run each).

## Delivery round 3 — the pull was replaying SOMEBODY ELSE'S commit

Round 1's delivery step stopped at:

```
error: could not apply 5953d23... fix(replay): a replay writes nothing into the operator's ledger — …
Could not apply 5953d23...
```

Round 2 read that as "the fix cannot be replayed onto a moved base" and landed a
re-derived copy (`e6c6f12`) so the pull would have nothing to replay. That
diagnosis was half right and its premise was wrong, so it is corrected here:

```
$ git branch -a --contains 5953d23
  remotes/origin/feat/replay-readonly-01a1597d        ← only this branch has it
$ git log --oneline HEAD..origin/feat/replay-readonly-01a1597d
  b211c5f merge master into feat/replay-readonly-01a1597d
  0e98d33 fix(replay,docs): keep loop.go's line table still, …
  8954542 …  b48a9b1 …  b321baf …
  5953d23 fix(replay): a replay writes nothing into the operator's ledger — …
```

**`5953d23` was never on this run's `master`.** It is a commit on
`feat/replay-readonly-01a1597d`, a different branch's work. `git pull --rebase`
tried to apply it onto `master` because the range it was replaying contained it —
and on `master` the regions it targets have since diverged, so the patch could
not apply. The rebase was not fighting this branch's history; it was replaying a
foreign commit into a tree it was never written against.

The two commits are also genuinely different patches, which is why "the same
content" was never a meaningful claim:

```
$ git show 5953d23 | git patch-id --stable   → 9d494248bdd75b7929a60607ff4aae23b14f739a
$ git show e6c6f12 | git patch-id --stable   → 16399c81fc52e7f26f342fafd305fb5bc4ae426d
```

`e6c6f12` is this run's own implementation of the same requirement (it carries
`devdoc/*`, `DOC-INDEX.tsv` and the docanchor baseline too).

### Why the delivery path is a push, not a pull-and-replay

The property to check is whether there is anything **of our own** left to replay.
Measured on the worktree:

```
$ git rev-list --left-right --count origin/master...HEAD   → 0	5      (0 behind, 5 ahead)
$ git rev-list --count --merges origin/master..HEAD        → 0
$ git merge-base origin/master HEAD                        → de8bcd5… = origin/master
$ git log --first-parent --oneline -6
  f383922 … 57e914e … 15aabbf … 4a98812 … e6c6f12 … de8bcd5            ← straight line
```

`HEAD` descends from `origin/master` in a straight line with no merge commits, so
the delivery is a **fast-forward push** and `git pull --rebase` has nothing to do.
That was checked before it was relied on (`--dry-run`), and then run:

```
$ git push origin master        → de8bcd5..f383922  master -> master
$ git fetch origin && git rev-parse origin/master HEAD
  f3839225913c32fb44c59b5fc4356c3c48f62573   f3839225913c32fb44c59b5fc4356c3c48f62573
$ git pull --rebase origin master              → "Current branch master is up to date."
```

The last line is the point: the step that blocked round 1 now succeeds as a
no-op, because the condition that made it fail (a replayed range containing a
foreign commit) no longer exists — `origin/master` is an ancestor of `HEAD`.

### What is NOT claimed

- The interrupted rebase left no state to resume (`.git/rebase-merge` and
  `.git/rebase-apply` both absent, `HEAD` = `master` = `f383922`). Nothing was
  resolved by hand; nothing needed to be.
- `origin/feat/replay-readonly-01a1597d` was **not** touched. It still holds
  `5953d23` and four follow-ups of its own. Whether those merge or archive is not
  this row's decision: this row's recorded deliverable branch is `master`, and its
  fix is already there. The branch is named here so a future reader does not
  mistake it for an unlanded second copy of this work.

## Delivery round 4 — the prefetch is not BUILT in a replay, instead of being refused per event

The three rounds above put a guard inside each memory-prefetch LOOKUP
(`chatPrefetch.fetch` and the sealed-turn `startTurnLookup`). The outcome was
right and the placement was wrong: a replay still CONSTRUCTED the executor
(`chatPrefetchFor`) and ARMED it, so its typing sampler and its turn-boundary
machine ran on every recorded event and printed one refusal per event, plus the
record's own complaint about a session directory a replay does not have. Measured
on the pre-fix binary (`SCRIPT ... chat --replay 004c3578 …`, stderr, 24 lines):

```
[gopi] memory prefetch: on, but this session has no session directory — no prefetch record will be written
```

A display whose whole job is to draw a session that already happened should never
have contained the machinery. The fix moves the decision to the ONE construction
point, so the rejection happens before anything is allocated, opened or started:

```go
func chatPrefetchFor(sessionDir string, line func() string, stderr *os.File) *chatPrefetch {
	if !config.MemoryPrefetchEnabled() {
		return nil
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if replaymode.Active() {                                    // ← the new short-circuit
		fmt.Fprintln(stderr, replaymode.Refuse(replayPrefetchSkippedWhat))
		return nil
	}
	…
```

**One announcement for the RUN, not one per event** (Rule 15). `nil` means no
goroutine, no record file and no sampling face, so the per-event guards behind it
can never fire — which is exactly why the sentence has to be spoken here. The
guards themselves are untouched and stay as defence in depth: a future caller
that reaches the executor by another route still cannot query the operator's
ledger from a replay (`TestReplayRefusesTheMemoryPrefetchLookup`,
`TestReplayRefusesTheTurnBoundaryLookup` still drive them).

### The real run, and the control

Same tree, same recording, same knob (`GOPI_MEMORY_PREFETCH` is ON by default;
it was set explicitly so "off" cannot be mistaken for the fix):

```
$ GOPI_MEMORY_PREFETCH=1 ./bin/gopi chat --replay 004c3578-37f7-4e57-852f-b9c362e5c460 --no-dump-screen
  → exit 0, 24 stderr lines
$ grep -n "memory prefetch" fixed-replay.err | grep -v "read-only in force"
  20:[gopi replay] refused to build the typing-time and turn-boundary memory prefetch for this run: this process is re-running …
$ grep -c "no session directory\|no prefetch record" fixed-replay.err
  0
```

The pre-fix binary built from the same tree (`git stash`-free: the file was
restored from `HEAD`, rebuilt to `bin/gopi-prefix`, then the fixed file was put
back and its blob hash re-compared), replayed the same recording:

```
$ grep -n "memory prefetch" prefix-replay.err
  4:[gopi replay] read-only in force … (the catalogue line, both binaries)
  20:[gopi] memory prefetch: on, but this session has no session directory — no prefetch record will be written
```

**The control (not replaying) still prefetches**, run in a sandbox profile so the
operator's own sessions directory is not the target:

```
$ SB=$(./bin/gopi tool sandbox create --label ctl --task-id 01a16f9d | tail -1)
$ GOPI_HOME="$SB" GOPI_MEMORY_PREFETCH=1 ./bin/gopi chat -v < /dev/null
  19:[gopi] memory prefetch: on (source: GOPI_MEMORY_PREFETCH) — record: …/sessions/b03bbc04-…/memory-prefetch.jsonl
$ test -f "$SB"/.gopi/sessions/b03bbc04-…/memory-prefetch.jsonl   → present, created by this run
```

So the change is a real short-circuit and not a switch that is off everywhere.

### Evidence seals

- the red proof: with the four-line `replaymode.Active()` short-circuit REMOVED
  from the production file, `go test ./cmd/agent -run TestReplayBuildsNoMemoryPrefetch`
  failed on `chatPrefetchFor built a prefetch executor while a replay is armed`;
  the file was then restored and `git hash-object` compared equal to the copy
  taken before the mutation, and the same test went green.
- the vocabulary is one value (Rule 13/14): `replaymode.RefuseMarker` re-exports
  `refusePrefix` for callers that MATCH a refusal instead of speaking one, and
  `prefetchNoSessionDirMarker` is shared by `openPrefetchRecord` (which prints
  the sentence) and the regression (which requires it to be absent).

### Honest limits

- The announcement is one line per RUN, printed at construction. A replay that
  somehow builds two executors would print two; there is exactly one
  construction site (`cmd/agent/cli_chat.go`), so that is a property of the code
  rather than of a counter.
- The two per-lookup guards are now unreachable from the REPL's own path. They
  are kept deliberately (defence in depth) and are still exercised directly by
  their tests, which is what makes "unreachable, not removed" checkable.

## The screen dump — the writer the catalogue invited a reader to find, and did not

Task: `01a17a24-28ef-7476-812b-843cf74f0633`. Ground cut point `620cbbf`.

### The defect, as measured

Two facts, both read out of the tree rather than reasoned about:

```
$ sed -n '141,153p' cmd/agent/chat_replay.go      # replayRefusalCatalogue()
seven entries; `grep -ci dump` on its output = 0

$ grep -c "replaymode.Active()" cmd/agent/chat_dump.go
0
```

The dump's resolver had three branches and asked the flag in none of them. The
second branch is the one that matters:

```go
if ag != nil && ag.SessionLog != nil {
    return filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), uiclient.ChatDumpFileName), nil
}
```

While a replay is armed, `ag.SessionLog` **is the recording's own logger** when
the agent was handed one, so that branch answers
`<recording-dir>/chat-screen.dump` — a path INSIDE the session directory the run
is re-running. The guarantee this verb prints at arm time ("a replay writes
nothing at all: it reads the recording in place and mints no session of its own")
would therefore be false, and false in the worst way: the write stays invisible
until somebody hashes the recording.

The old catalogue comment said, verbatim: *"A reader comparing this function
against the four call sites of `replaymode.Active()` is doing the check the
comment invites."* The reader was invited. Nobody came. **That is the whole
reason this entry exists as a MACHINE and not as one more paragraph.**

### The fix — redirect, not refusal

`chatResolveDumpPath` asks `replaymode.Active()` **first**, before the explicit
path and before `ag.SessionLog`, and while it is true answers the new
`chatReplayDumpPath()`: a directory of the chat's own scratch class
(`scratch.RuntimeClassUISession`, the class the ordinary resolver already falls
back to when a run has no session). No new artifact class, no new dependency —
`gopi tool scratch-gc` already sweeps it (Rule 35).

The ordering is the guarantee, and it is `internal/replaymode`'s own stated
requirement: *"a flag armed after the writer it is meant to stop has already run
is prose."*

**Why the explicit path is redirected too.** `--dump-screen=PATH` can name the
recording by accident (a shell glob, a resumed command line), and the resolver
cannot tell that from a destination the operator chose deliberately. So a
replay's answer does not depend on the operator's spelling at all. The cost is
that a replay cannot be told to dump somewhere of the operator's choosing; the
dump still EXISTS, which is the property that matters (it is a visualization
product, and a replay is exactly the run whose picture someone wants).

**Why not simply switch the dump off.** Refusing it would delete a product the
capability exists for, and the epic's constraint says so in as many words. The
redirect keeps the artifact and moves it.

The move is announced by `chatDumpPathNotice`, which is the ONE spelling of
"where the dump went" — the print site no longer decides which sentence applies:

```
gopi chat: screen dump → /home/user/workspace/gopi-test-scratch/queue/queue-runtime-ui-session-578115/chat-screen2112497166/chat-screen.dump (a replay writes nothing into the recording it re-runs, so this artifact lives in the chat's own scratch class, never beside the recording)
```

### The catalogue, and the machine that keeps it honest

`replayRefusalCatalogue()` gained one row, and the arm-time sentence now
distinguishes the two kinds of answer (seven refused, one redirected):

```
... ; the screen dump (redirected, not dropped: chatResolveDumpPath answers
chatReplayDumpPath while this flag is set, so the dump is written to the chat's
own scratch class and never beside the recording it re-runs) (recording: …)
```

The prose claim is GONE, replaced by two cases in
`cmd/agent/chat_replay_dump_catalogue_test.go`:

| case | what it decides |
|---|---|
| `TestChatReplayDumpCatalogueIsExhaustive` | walks every PRODUCTION (non-test) `.go` file in the tree, collects every real call site of `replaymode.Active()` (comment lines excluded), and fails in THREE directions: a site no row of `replayActiveSiteRows` claims; a row whose gate no longer exists; a row whose phrase is absent from `replayRefusalCatalogue()`'s OUTPUT. Non-vacuity floors on both sides, so a broken walk cannot pass as a clean tree. |
| `TestChatReplayDumpCatalogueNamesTheScreenDump` | the operator-visible half: the printed sentence names the screen dump AND says it was redirected rather than dropped. |

`replayActiveSiteRows` is one row per (`file`, `symbol`, `phrase`); the phrase is
the JOIN between the code and the sentence the run prints, so the table cannot
vouch for itself.

### Evidence

RED (production files reverted to the cut point, `git checkout HEAD -- cmd/agent/chat_dump.go cmd/agent/cli_chat.go`, then the same test):

```
--- FAIL: TestChatReplayDumpStaysOutsideTheRecording (0.00s)
    chat_replay_dump_catalogue_test.go:378: a replay resolved its dump to
    "…/11111111-2222-4333-8444-5555555555dd/chat-screen.dump" — INSIDE the recording
    tree "…/11111111-2222-4333-8444-5555555555dd". A replay writes nothing into the
    recording it re-runs; the dump belongs in the chat's own scratch class
```

GREEN after restoring (the restored file's `sha256` equals the snapshot taken
before the mutation: `5c4a2576…` both sides — a content-identity reading, not an
index-relative one).

End-to-end, a real replay of a real recording, hashed before and after:

```
$ GOPI_HOME=<sandbox> ./bin/gopi chat --replay <sandbox>/recording/session.jsonl --no-editor
[gopi replay] re-running session 77d26121-986b-496a-a226-06bc95851c15 … (1 turn(s), 5 scripted response(s), 4 recorded tool result(s))
… screen dump → /home/user/workspace/gopi-test-scratch/queue/queue-runtime-ui-session-578115/chat-screen2112497166/chat-screen.dump (a replay writes nothing into the recording it re-runs …)
```

| file in the recording dir | before | after |
|---|---|---|
| `session.jsonl` | `fa7e675d…` | `fa7e675d…` |
| `metadata.json` | `6372ac4e…` | `6372ac4e…` |
| `console.log` | `5a9c43cc…` | `5a9c43cc…` |
| `chat-screen.dump` (the recording's OWN, from the recorded run) | `2c2956ad…` | `2c2956ad…` |

`diff before.txt after.txt` printed nothing (exit 0), and the directory's file
list is the same four entries. The replay's own dump went to scratch: `4297
bytes`, `frames=0` (no reserved layout under `--no-editor`), `pid=578115`.

### Honest limits

- **The gate is a walk over Go SOURCE, not a proof about writes.** It decides
  "every gate that exists is catalogued", not "every write goes through a gate".
  A writer reached by a shape nobody has measured yet is still not stopped —
  which is exactly why the inventory is enumerated rather than asserted, and
  exactly what this entry now makes checkable one step earlier.
- **A gate whose call is spelled through a variable is invisible to the walk.**
  The detector matches the literal `replaymode.Active()`; an alias imported as
  `rm.Active()` would be missed. The tree has no such alias today
  (`grep -rn "replaymode\." --include=*.go | grep -v replaymode/` shows the
  package imported as itself at every site).
- **The dump is still written.** A replay's observation is a file in scratch that
  `scratch-gc` collects on its ordinary schedule; it is not the recording tree,
  and that is the property this task bought.
