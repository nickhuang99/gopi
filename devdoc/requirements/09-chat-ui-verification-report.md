<!-- doc-class: historical -->
# Task Report: Module 10/15 — chat / interactive-UI requirements, VERIFIED at current master

**Deliverable:** `devdoc/requirements/09-chat-ui.md` — verified, corrected and re-anchored
at master `812f9a60`. The briefing's path (`devdoc/requirements/chat-ui.md`) was stale; the
document already existed (937 lines, landed by `9c45da23`), so this pass is
**verification/completion, never a rewrite**.

**Branch / worktree:** `docs/req-09-chat-ui-verified` @ `/home/user/workspace/gopi-req-09chat`
(Rule 6a; created by this task, off `812f9a60`).

## Completed

1. **Citation re-anchoring by symbol.** A no-basename-fallback resolver was written and run
   over the untouched document: **88 distinct `file:line` citations, 73 OK, 15 BAD** — all 15
   bare basenames in `cmd/agent`. Every one was re-anchored by reading the function the
   sentence names, never by guessing an offset; ~20 further in-tree anchors that resolved but
   pointed at the wrong content were corrected in the same pass (drift: `cli_chat.go`
   591→633, `chat_stream.go` 236→275, `lineedit_wire.go` 231→370, `chat_ops.go` 376→422).
   **Final gate: 100 distinct citations, 100 OK, 0 BAD.**
2. **The stale sibling claim retired.** The document asserted `fix/chat-help-command-arg`
   (`f8cca331`) was *not* an ancestor of master and that "master's REPL still ignores the
   argument". Measured 2026-09-18: it **is** an ancestor (merged by `77fac9ac`);
   `cmd/agent/cli_chat.go:507-518` answers `/help <cmd>`. The §Boundary warning and §6.5-E were
   corrected.
3. **The pass's largest correction — every count is store-dependent.** The 2026-09-17 figures
   (116 `chats` rows, 79 pointer-less, 85 shown running) were read from **another host's**
   store; a new §6.5-F re-measures on peerbox (104 rows, 95 with a pointer, 9 pointer-less,
   84 ended, 20 running) and adds a side-by-side table. Root cause of the difference named as
   an **open question** rather than guessed. New finding **F7**: `chats` is **per-host local
   state** (the sync path moves `tasks`/`sync_state`/`distilled_qa` only), so a chat-index claim is a claim
   about one machine — and `log_path` is an absolute path that dangles on any other host.
4. **G1/G2 shown to reproduce, not to have been fixed.** On peerbox the nine pointer-less rows
   carry the identical signature: start times are exactly the migration's two seconds
   (`2026-08-30 10:58:12` ×6, `10:58:13` ×3), `first_msg` still holds runtime telemetry, none
   has `ended_at` set, and none of their session files exists here. The mechanism
   (`chatFindSessionJSONL` matching only a **flat** `<id>.jsonl`) is unchanged.
5. **Two false claims stopped mid-pass** (recorded in the document's §Verification because the
   retraction is the check working): I first wrote that `--no-editor` had been removed (it is
   still accepted, `cli_chat.go:279-280`), and that the `/help <cmd>` fix "shipped with no
   test" (`git show --stat f8cca331` lists `cli_chat_test.go +107`). Both paragraphs were
   corrected before commit.
6. **§7 given real, resolving test names + one genuine new gap.** All 28 test names §7 cites
   were resolved with `grep -rn '^func <TestName>' --include='*_test.go'`; the roadmap's
   shorthand `TestChatPasteBurst` resolved to `TestChatPasteBurst_ThreeLinePasteIsOneMessage`
   (`cmd/agent/lineedit_wire_test.go:321`). New rows: the four `/help` tests, and the cooked
   fallback family. **New hole found:** the *degradation notice* is **unpinned** —
   `cmd/agent/lineedit_wire.go:183` and `:187` print the two notices R8/A8 requires, and no
   test asserts either string (the existing `TestChatCookedFallback_*` pin which reader is
   chosen and that the editor *banner is absent*, not that the operator is *told*).
7. **Live counts re-measured and date-stamped**; §Unknowns gained two items (F7's cause;
   whether `log_path` was ever meant to be portable).
8. **All 8 sections + `## Boundary` + `## What I could not determine` + the C1–C5 constraints
   check confirmed present**; additions are subsections inside the existing shape.

## Not Done

- **§6.1–§6.4 and the C1–C5 verdicts were not re-graded.** They are reading judgements over
  evidence that did not change (the R-rows, the boundary argument, the over-build calls).
  Re-deriving them would be a rewrite, not a verification. Named explicitly as a
  non-claim in the document's §Verification item 7.
- **F7's cause is not settled** (per-host table vs. a later backfill on this host vs. rows
  outliving their logs). Settling it needs the two hosts' stores read side by side — a
  two-host measurement this session cannot perform. Recorded as §Unknowns #7 instead.
- **The 11 pointer rows shown running on peerbox cannot be adjudicated locally** — none of their
  log files exists on this host, so the "finished but shown as running" test the first pass
  applied by hand is not performable here. Recorded, not guessed.
- **No new tests written**, per the briefing. The unpinned degradation notice is *recorded* as
  a gap, not closed.
- **The pre-existing worktree `~/workspace/gopi-req-b9v`** (branch
  `docs/req-09-chat-ui-verify`, zero commits beyond master) was inspected read-only and left
  untouched; this task created its own worktree/branch instead (Rules 29, 23).

## Verification

- **Build:** not run — docs-only diff (`git diff --name-only master..HEAD` = the module doc +
  this report). Per the briefing, the gates are the citation resolver and the section/test
  checks, not an unrelated package build.
- **Gate 1 — citation gate, no basename fallback:**
  `/home/user/workspace/req09-evidence/citation-gate.sh devdoc/requirements/09-chat-ui.md .`
  → `distinct file:line citations: 100   OK: 100   BAD: 0` (from 88/73/15 on the untouched doc).
  Output: `/home/user/workspace/req09-evidence/gate1-citations.txt`.
- **Gate 2 — every §7 test name resolves to a real `func TestX`:**
  `grep -rn '^func <TestName>' --include='*_test.go' .` → 28 of 28 resolve; the one
  `UNRESOLVED` was the roadmap *quotation*'s shorthand, now named alongside its real test.
  Output: `/home/user/workspace/req09-evidence/gate2-testnames.txt`.
- **Gate 3 — sections present:** `## Boundary`, `## 1.`–`## 8.`,
  `## What I could not determine`, `### C1–C5 check` — all PRESENT (grep of headings).
- **Gate 4 — live numbers re-measured, date-stamped in the doc:**
  `/home/user/workspace/req09-evidence/gate4-live-counts.txt` — measured 2026-09-18 on
  `peerbox`, HEAD `812f9a60`: `chats_total=104`, `with_ptr=95 no_ptr=9 ended=84 running=20`,
  the no-pointer signature `2026-08-30 10:58:12 n=6` / `10:58:13 n=3`, `tasks_human=609`,
  `backfill_marker=83`, and `has_file=0` for all nine pointer-less ids.
- **Gate 5 — worktree clean:** `git status --porcelain` prints nothing (see below).
- **Gate 6 — diff scope:** `git diff --name-only master..HEAD` lists only
  `devdoc/requirements/09-chat-ui.md` and this report.

## Evidence (self-filed)

- Gate 1, before the pass: `88   OK: 73   BAD: 15` (15 bare basenames, all `cmd/agent`).
  After: `distinct file:line citations: 100   OK: 100   BAD: 0`.
- Gate 2: `unresolved=1 of 28`, and that one is a quoted roadmap `-run` shorthand, resolved in
  the text to `TestChatPasteBurst_ThreeLinePasteIsOneMessage` at
  `cmd/agent/lineedit_wire_test.go:321`.
- Gate 4 (verbatim from the file): `chats_total=104` / `with_ptr=95 no_ptr=9 ended=84
  running=20` / `no_ptr_sig=2026-08-30 10:58:12 n=6` / `no_ptr_sig=2026-08-30 10:58:13 n=3` /
  `tasks_human=609` / `backfill_marker=83`.
- `/home/user/.gopi/sessions/` holds **3,140** `session.jsonl` files at depth ≤2
  (`find … -maxdepth 2 -name '*.jsonl' | wc -l`), and **0 of the 9** pointer-less ids has one.
- `git merge-base --is-ancestor f8cca331 812f9a60` → exit 0 (**ancestor**; merged by
  `77fac9ac`) — the claim that retired §Boundary's warning.
- `git show --stat f8cca331` → 4 files, incl. `cmd/agent/cli_chat_test.go | 107 +`.
- `grep -rn 'could not enter\|too small for\|falling back' --include='*_test.go' cmd/agent
  testsuite internal/lineedit` → no hits (the unpinned degradation notice).
- `grep -rn '"chats"' internal/sync/` → **VACUOUS (corrected 2026-09-18c)** — `internal/sync/`
  does not exist (`find . -type d -name 'sync*'` → nothing; module B4 of this series says so
  outright: "there is no sync package, and there is no sync directory"), so the grep printed
  `No such file or directory` and the "no hits" it yielded measured nothing. F7's conclusion
  was re-established non-vacuously: the non-test files naming the table are enumerated
  (`grep -rln '"chats"' --include='*.go' .` → 6, all readers: `internal/tools/chat.go`,
  `internal/agent/{chat_ops,chatregister,loop}.go`, `cmd/agent/{cli_chat,cli_web}.go`) and the
  sync path is shown **positively** to move exactly three tables
  (`FROM`/`INTO` over `memory_sync*.go` + `task_sync_git.go` → `tasks`, `sync_state`,
  `distilled_qa`). F7 STANDS; its original proof did not.
- Rule 29: no other task's worktree entered; `~/workspace/gopi-req-b9v` read with `git -C …
  status --porcelain` only.

## Deliverable declaration (Rule 6d)

- **Branch:** `docs/req-09-chat-ui-verified` (worktree `/home/user/workspace/gopi-req-09chat`).
- **Recover:** `git show docs/req-09-chat-ui-verified:devdoc/requirements/09-chat-ui.md`.
- **In the main repo:** a worktree shares the main repo's refs, so the commit is already
  there — no push needed.

---

# Pass 3 — 2026-09-18b: the claims were EXECUTED, not re-read

Same module, same branch `docs/req-09-chat-ui-verified`, same worktree
`/home/user/workspace/gopi-req09v` (the pass-2 worktree `/home/user/workspace/gopi-req-09chat` was
reclaimed mid-session; the branch survived at `ba9e243c` and was re-checked-out under Rule
6a — the worktree, not the commit, was what moved). Baseline master `812f9a60`.

**Why a third pass.** Pass 2's §6.5-F published a number (`93 of 95` pointers resolve) that
it had obtained with `[ -f "$path" ]` on a substituted path rather than from the function
the code actually runs. A verification pass whose own headline measurement was not taken
from the shipped code is a claim, not a verification — so pass 3 ran the shipped function.

## Completed

1. **The resolver claim was executed, not reasoned about.** A throwaway in-package probe
   (deleted before the commit) called the production `chatFindSessionJSONL` and
   `chatSessionFactsOf` directly, against the live store, read-only:
   **89 of 95 pointer rows resolve, 6 do not; all 89 resolutions are to a DIRECTORY; 89 rows
   yield a non-nil but wholly empty facts struct; 0 yield usable facts.** §6.5-A/F and a new
   **G7** carry it; the pass-2 number and the two ids it named as broken are corrected.
2. **The mechanism was identified and is a Rule 15b silent failure.** `chatFindSessionJSONL`
   matches `ReadDir` entries by **name**, and a session *directory* named `<id>` trims to
   `<id>` exactly as `<id>.jsonl` would; `chatSessionFactsOf` then `os.Open`s the directory
   (succeeds on Linux), scans zero events, **never checks `sc.Err()`** (`grep` → no hits),
   and returns a non-nil struct that every caller reads as success
   (`internal/tools/chat.go:524`). Nothing logs.
3. **A fresh `gopi chat backfill` was run — on a COPY.** 104 → **259** rows, **106**
   directory pointers, marker rows 83 → **238**, **0 of 106** closed. The live store was
   read-only throughout and re-measured after: unchanged (104 / 0 / 83). So G7 is
   *manufactured at scale* by re-running the migration, not a legacy relic.
4. **G2's "not performable here" note was withdrawn by looking where the reader looks.**
   All 11 pointer-rows-shown-running have a log on this host (10 under `<id>/<id>.jsonl`,
   1 absent), and **0 of the 10 contain `session_end`**.
5. **NF-3 is new.** 19 `mode='web'` rows: **15** are `first_msg='ping'`, **2** carry a
   `log_path` under a deleted `/tmp` `t.TempDir()` of `cmd/agent/cli_web_test.go`'s own tests
   (`:521`, `:695`), `workdir` names 11 throwaway checkouts. A carrier's adoption cannot be
   read off its own index — §6.4-ND1's usage reading is corrected.
6. **§7 gained the test-side face of G7.** The only session-log fixture,
   `writeChatSessionJSONL` (`internal/tools/chat_test.go:33-56`), writes a **flat**
   `<id>.jsonl` at :50 — the layout production no longer has (0 flat files here; 69
   `<id>/<id>.jsonl`, 20 `<id>/session.jsonl`). **0** tests name the reader. The suite could
   not have caught G7 in its current shape.
7. **§Unknowns #4 withdrawn** (`f8cca331` IS an ancestor of master, merged `77fac9ac`), and
   two new open items added (#9 whether the silent failure ever surfaced; #10 who owns the
   `<id>/…` layout — 69 vs 20 rows disagree with each other).
8. **All gates re-run on the final text** (see Verification).

## Not Done

- **§6.1–§6.4 and the C1–C5 verdicts are still not re-graded** — unchanged from pass 2's
  honest non-claim: reading judgements over evidence that did not move.
- **G7's fix is proposed in words, not applied.** The module is documents-only by the
  briefing; `!e.IsDir()` + descending into `<id>/` + checking `sc.Err()` is named as the
  repair, not performed.
- **No test was written**, per the briefing. The new §7 row names the direction only.
- **The two-host store question (#7) is narrowed, not closed** — candidate (ii) is shown
  *sufficient*, which does not exclude (i).
- **The 74283-entry sessions root was not swept exhaustively**; counts are of the 104 index
  rows, not of every session on the host.

## Verification

- **Build/test:** not run as a gate — the diff is two `.md` files, and the probe file that
  did compile and run was deleted before the commit (`git status --porcelain` shows exactly
  one modified file). The probe's own runs are cited below as evidence, not as a suite.
- **Gate 1 — citation gate, no basename fallback:**
  `/home/user/workspace/req09-evidence/citation-gate2.sh devdoc/requirements/09-chat-ui.md .`
  → `distinct file:line citations: 104   OK: 104   BAD: 0`.
- **Gate 1 negative probe** (the gate must go red — proves it is not vacuous):
  `BARE basename / 99999 > wc -l 633 / file does not exist` → `OK: 104   BAD: 3`.
- **Gate 2 — test-name resolver:** §7's tokens re-resolved at close-out: 27 distinct
  `Test…` identifiers, **25 resolve to a real `func TestX(` in a `_test.go`**, and the 2 that
  do not (`TestChatCompletion_*`, `TestChatCookedFallback_*`) are *written as families* with
  the `_*` glob in the text — their members exist (10 and 4 respectively, e.g.
  `TestChatCompletion_TabCompletesTheSelectedCandidate`,
  `TestChatCookedFallback_ReaderParity`). So every named test in §7 resolves; the two
  asterisks are deliberate shorthands, not unresolved names.
- **Gate 2 negative probe** (run, not asserted): the same resolver over
  `TestChatCompletion_TabCompletesTheSelectedCandidate TestChatCookedFallback_ReaderParity`
  → `cited=2 resolve=2`, and over an invented name → `UNRESOLVED:
  TestThisNameDoesNotExistAnywhereZzz / cited=1 resolve=0`. The resolver can fail, so the
  `resolve` counts above are measurements.
- **Gate 3 — sections:** `## Boundary`, `## 1.`–`## 8.`, `## What I could not determine`,
  `C1–C5 check`, `## Verification of this document` — all PRESENT.
- **Gate 4 — live numbers, re-measured and date-stamped:** `gate4-live-counts-20260918c.txt`
  (measured 2026-09-18T20:00:06+08:00, host peerbox, HEAD `ba9e243c`).
- **Gate 5 — ids and SHAs:** 23 ids cited, 23 resolve against `tasks`/`chats`; 26 backticked
  sha-shaped tokens = 13 git commits + 13 id prefixes, 0 unresolved.
- **Gate 6 — worktree clean:** `git status --porcelain` printed only the module doc during
  the pass; clean after the commit.
- **Gate 7 — diff scope:** `git diff --name-only master..HEAD` = the module doc + this report.

## Evidence (self-filed)

- Probe, shipped functions, live store (read-only):
  `PROBE rows=104 with_ptr=95 log_path_is_a_real_file=20 reader_resolves=89 reader_hit_is_DIR=89 reader_unresolved=6 | facts_nil=6 facts_NONNIL_but_ALL_ZERO=89 facts_with_data=0`
- Probe, single row, proving a directory is returned and read as *success*:
  `PROBE-ONE id=663d74a7-… reader_path=…/663d74a7-… is_dir=true facts_nil=false turns=0 first_msg=""`
- Fresh backfill on the COPY: before `rows=104 non_jsonl_logpath=0` →
  after `rows=259 dir_pointers=106 marker=238 dir_ptr_ended=0_count=106`.
- Live store after that run, unchanged: `live_rows=104 live_dir_pointers=0 live_marker=83`.
- The reader never checks the scan error: `grep -n 'sc.Err()' internal/tools/chat.go` → no hits.
- Fixture writes the flat layout: `internal/tools/chat_test.go:50`
  `path := filepath.Join(dir, sessionID+".jsonl")`; `grep -rn 'chatFindSessionJSONL' --include='*_test.go'`
  → 0 hits outside the deleted probe.
- The 11 rows: ten logs present, `grep -c '"type":"session_end"'` → `0` for each.
- NF-3: `select count(*) from chats where mode='web' and first_msg='ping'` → `15` of `19`;
  `log_path like '/tmp/%' and mode='web'` → `2`.
- Citation negative probe: `OK: 104   BAD: 3` (bare basename, out-of-range line, missing file).

## Deliverable declaration (Rule 6d)

- **Branch:** `docs/req-09-chat-ui-verified` — commit `ba9e243c` (pass 2) + the pass-3 commit.
- **Recover:** `git show docs/req-09-chat-ui-verified:devdoc/requirements/09-chat-ui.md`
- **In the main repo:** a worktree shares the main repo's refs, so the commit is already
  there — no push needed.
