<!-- doc-class: historical -->
# Task Report: stale handoff claim — `devdoc/handoff-20260924-ui-perception.md` said `ui_state` was NOT built

**Task:** 01a0d59c-7983-7086-8753-34e87ad05da9
**Root cause of the drift, in one line:** the handoff doc's §1 recorded a
*writing-moment* fact ("the tool is not built", task `01a0d337` pending) in a
doc whose class is a **verification checklist for future readers** — so the
snapshot outlived its moment and a reader who trusted it stopped at a door that
is open. A checklist that says "X is not done" has an expiry date and no
mechanism to notice it.

## Completed

- **§1's "The one thing that is NOT done" subsection — corrected, not silently
  deleted.** The heading is struck through and now reads *CORRECTED 2026-09-25:
  `ui_state` HAS landed*; the stale assertion, the pending-task citation and the
  false conclusion ("neither is perceptible by a TOOL CALL") are retracted
  explicitly, with the live commands and their exit codes quoted in the doc.
  Kept rather than removed because the misreading is the lesson: a "not done"
  line in a handoff is a snapshot of its own writing moment, never a fact about
  the tree you are standing in.
- **§6's open-items bullet corrected** — "`ui_state` tool: not built (§1)" →
  "LANDED", with a pointer to §1's correction and to §4.
- **§4 retitled** — "If the `ui_state` tool has landed, verify it like this" →
  "Verify the `ui_state` tool like this (IT HAS LANDED — this is the live
  path)", so the section reads as the current verification path rather than a
  conditional one. Its four verification requirements are otherwise unchanged.
- **§4 corrected in two places against the measured binary** (both are the same
  class of defect as the headline one — a checklist sentence that nothing
  prints): requirement 3 asked the reader to confirm the tool says *"the target
  appears to have exited"*, while the real diagnosis is a fixed constant
  (`uiclient.ExitedNotice` = *"the target has exited cleanly: the dump is the
  LAST frame that surface painted, not the current screen"`), so the doc now
  quotes the sentence that is actually printed; and §4 now says **how to drive
  the surface** (`script -q -c "stty rows 30 cols 100; …"`, waiting on the
  dump's `frames=` counter, never on the file's existence), because the
  `pty.fork()` route this run first tried never reached a painted frame.
- **The verb itself was verified before the doc was touched** (Rule 31), and
  **all four of §4's requirements were then exercised for real** against two
  live pty-driven surfaces — see `## Verification`.
- One commit on this branch.

## Not Done

- **CORRECTION to an earlier draft of this section** (kept visible on purpose):
  it claimed §4's requirements 1–3 "cannot be satisfied on this host from a
  passive run" and left them to the operator's next live session. That was
  wrong — a pty-driven pair of surfaces answered all three in this session (see
  `## Verification`); the earlier text was a guess about feasibility, not a
  measurement.
- **`--pid` was exercised too** (not required by §4, but it is the documented
  disambiguation route): `ui_state --pid 932548` (a listed candidate) → exit `0`,
  header naming that surface, `state=exited` + the fixed diagnosis;
  `ui_state --pid 999999` (no such surface) → exit `1`, *"no ui-screen.dump dump
  written by pid 999999 (dumps on hand: 932548)"* + the table of what IS there —
  no silent fallback to "the only dump I found".
- **A cosmetic wording defect was found and NOT fixed** (out of this run's scope,
  and it needs a system-layer change): when *several dead* surfaces are on hand, `SelectLive` refuses correctly (exit 2, both
  candidates listed, each marked `exited` in the table) but the refusal's first
  line is the shared `AmbiguityError` text — *"2 live gopi surfaces could be the
  target"* — which says **live** about two surfaces the very next lines report
  as `exited`. The verdict is right, the adjective is wrong; a reader who quoted
  only the first line would misreport two exited surfaces as running. Source:
  `internal/uiclient/dumpread.go:643-651` (`AmbiguityError.Error()`, one text for
  both cases) reached from `cmd/agent/cli_uistate.go:206`.
- **The screen reaches a *painted* frame only when the surface gets a terminal
  it accepts as one.** Measured twice on this host: driven through `pty.fork()`
  with an explicit `TIOCSWINSZ` 100x30, `gopi ui --dump-screen` writes its dump
  at startup and then **never paints another frame** — the header stays
  `frames=0 dumps=0` for the full 110s budget and the grid is all-blank, while
  the same surface driven under `script -q -c "stty rows 30 cols 100; …"` paints
  within ~15s (`frames=N`, row 0 = `gopi ui → …`). The dump's own
  `#gopi-ui-dump` header is written both ways, so a poller that waits on "the
  file exists" captures the **unpainted** one; `devdoc/evidence/ui-screen-dump/capture.sh`
  polls the frame counter instead, which is the right readiness key. This run
  used the `script` route for the three §4 requirements and the `pty.fork()`
  route for the exit/refusal paths. **The cause was not established** (candidate:
  the surface blocks on its startup terminal query `ESC ] 11 ; ?` / `CSI 6n`,
  which the `script` route answers differently) — recorded as a measurement with
  its readiness consequence, not as a diagnosis.
- **The pre-existing DocAnchor red is untouched.** `internal/agent`'s
  `TestDocAnchorsResolveInNormativeDocs` is red on this branch's base **and** on
  this branch (identical tally, measured — see Evidence: 244 permitted / 35 new
  / 13 stale permits). That repair is a separate, larger task; this run's edit
  adds no `file:line` anchor, so it neither caused nor can fix it.
- **`gopi` used in the report's own commands is `/tmp/gopi-docdrift`**, a build
  of this branch's tree under `CGO_ENABLED=0`, not an installed binary.

## Verification

- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-docdrift ./cmd/agent/` → OK (silent, exit 0)
- Tests: `go test -count=1 -run 'DocClass|DocIndex|Banner' ./internal/standards/ ./internal/tools/` → `ok github.com/nick/gopi/internal/standards 1.094s`, `ok github.com/nick/gopi/internal/tools 0.846s`
  - Re-run after the §4 edits: `go test -count=1 -run 'DocClass|DocIndex|DocAnchorResolve' ./internal/standards/ ./internal/tools/` → `ok github.com/nick/gopi/internal/standards 1.253s`, `ok github.com/nick/gopi/internal/tools 0.656s [no tests to run]`.
  - Branch-scoped smoke: `make test-budget PKGS='./internal/uiclient/ ./cmd/agent/ ./internal/standards/ ./internal/agent/'` — run as a background job (`job-3`) because the packages are slow; its result is recorded at hand-over, and `internal/agent` alone is known-red from the base (DocAnchor, below).
  - `go test -count=1 -run TestDocAnchorsResolveInNormativeDocs ./internal/agent/` → **FAIL, pre-existing**: identical tally with my edit reverted to HEAD (`2540 across 526 normative docs … 244 pre-existing drift entries permitted, 35 new, 13 stale permits`). Not introduced by this change; the doc I edited carries no gated anchor.
- Ran: `/tmp/gopi-docdrift ui_state -h ; echo $?` → usage page, `0`
- Ran: `GOPI_HOME=/tmp/gopi-docdrift-home /tmp/gopi-docdrift ui_state ; echo $?` → stderr `gopi ui_state: no ui-screen.dump dump found in any scanned location`, exit `1`
- Ran: `SB=$(gopi sandbox create --label docdrift --task-id 01a0d59c… | tail -1); GOPI_HOME=$SB /tmp/gopi-docdrift ui_state --list ; echo $?` → `#gopi-ui-state-list scanned=0 roots=2` + the two resolved roots + `no ui-screen.dump dump found`, exit `0` (the never-refuses surface)
- Ran: `GOPI_HOME=$SB /tmp/gopi-docdrift ui_state --json ; echo $?` → JSON with `"refusal": "no ui-screen.dump dump found in any scanned location"`, `"scanned": 0`, and `blind_spots` naming all 8 (`font`, `ime_composition`, `raw_bytes`, `graphics_protocols`, `private_modes`, `blank_vs_unpainted`, `interior_scroll_loss`, `frame_timing`), exit `1`
- **§4 requirement 1 (live surface, discovered, no path given)** — Ran (verbatim, evidence `r1.txt`): `bash /tmp/uistate-live-check.sh $SB` where `$SB` is a `gopi sandbox create` profile and surface 1 is `setsid env GOPI_HOME=$SB GOPI_SETTINGS=$SB/settings.json TERM=xterm-256color timeout 150 script -q -c "stty rows 30 cols 100; … $BIN ui --addr 127.0.0.1:0 --dump-screen=$SB/.gopi/sessions/live-check/ui-screen.dump" /dev/null` → readiness key `head -1 <dump> | sed -n 's/.*frames=\([0-9]*\).*/\1/p'` reached `>0` (`PAINTED`), then `GOPI_HOME=$SB $BIN ui_state` → exit `0`, stdout line 1 `#gopi-ui-state pid=921249 … state=live dump=…`, and the grid with `000|gopi ui → http://127.0.0.1:35501  (tools off)`, `cursor row=29 col=0`, `blind_spots_begin count=8`. No path argument was passed.
- **§4 requirement 2 (two live surfaces must REFUSE and list both)** — Ran (same script, evidence `r2.txt` for the single-surface run that followed, `r3.txt` for the ambiguous one): a second surface was launched into the same profile → `GOPI_HOME=$SB $BIN ui_state` → **exit 2**, stderr *"2 live gopi surfaces could be the target; refusing to guess — pass --pid (or --list) to choose:"* followed by both candidates (`pid=922395 …` and `pid=921249 …`, each with target + started + dump path), and **nothing chosen silently**.
- **§4 requirement 3 (an exited target is diagnosed, not reported as blank)** — Ran (evidence `round3.log`): one live surface, `ui_state` → exit 0 with `state=live`; then `os.kill(pid, 9)` + `os.waitpid` (so `/proc/<pid>` is gone, confirmed), then `GOPI_HOME=$SB $BIN ui_state` → exit `0` with header `state=exited` and the fixed diagnosis *"the target has exited cleanly: the dump is the LAST frame that surface painted, not the current screen"*, and `--json` carrying `"state": "exited"` + `exited_notice` + the same sentence as the candidate's `reason`. The grid is shown, and the exit is *named* — not reported as a current blank screen.
- Result: **all four of §4's requirements hold on the real binary, measured with a real `gopi ui` under a pty.** The published report's blind-spot list is printed with every answer and `what_this_is` opens with *"an interpreted cell grid (not bytes, not pixels)"* (requirement 4, §4's required first sentence). Exit-code contract: `0` = an answer (`--list`, a live read, an exited read), `1` = refusal with no surface at all, `2` = refusal because several surfaces could be the target.
- Result (defect found, not fixed — out of scope, recorded): when **several dead** surfaces are on hand, the ambiguity refusal is right (exit 2, both candidates, both marked `exited`) but its first line still reads *"2 **live** gopi surfaces could be the target"* — `AmbiguityError.Error()` (`internal/uiclient/dumpread.go:643`) renders one text for the live-pair and dead-pair cases. Cosmetic in the verdict, wrong in the adjective.
- Ran (protocol defect, measured twice): driving the surface through `pty.fork()` + `TIOCSWINSZ` instead of `script` leaves the published dump **unpainted** — header `frames=0 dumps=0`, all-blank grid, still `state=live`, for the whole 110s budget (evidence `round2.log`, `round3.log`, `round5` run in this session; reproduced with `TERM` unset and `TERM=xterm-256color`, so TERM is not the switch). Under `script -q -c "stty rows 30 cols 100; …"` the same binary painted in ~15s. So a readiness test that waits on the file's *existence* reads an unpainted dump as a live one; the frame counter is the right key (as `devdoc/evidence/ui-screen-dump/capture.sh` does). **Cause not established** — the candidate is the startup terminal query (`ESC ] 11 ; ?` + `CSI 6n`, first bytes the surface writes) blocking a pty nobody answers; this is a measurement plus its readiness consequence, not a diagnosis.

## Evidence (self-filed)

- **Stale claim gone**: `grep -n "is NOT built yet\|ui_state\` tool: not built" devdoc/handoff-20260924-ui-perception.md` → no matches (only line 83's `### ~~The one thing that is NOT done~~ — CORRECTED 2026-09-25: ui_state HAS landed` survives, by design).
- **DocAnchor base == head** (the pre-existing-red measurement; the two edited docs were reverted with `git show HEAD:<path> > <path>` after backing them up to a scratch dir, then restored): both runs printed `2540 across 526 normative docs … 244 pre-existing drift entries permitted, 35 new, 13 stale permits` — the same tally, so this change adds no drift.
- **Worktree clean at close-out**: `git status --porcelain` → empty (Rule 30).
- **The verb's own contract lives in a normative doc**: `devdoc/DOC-INDEX.tsv:2311` classes `devdoc/ui-state-reader.md` as *normative — standing claim about master that `gopi ui_state` reads a LIVE `gopi ui` screen state in one call*, with `standing 3 / record 0`. The doc this run fixed is `historical` (DOC-INDEX:1162), which is exactly why its outdated "not done" line could never be caught by a code gate: nothing builds it and nothing re-checks it.
- **Raw §4 evidence, verbatim, kept outside the worktree** (Rule 30 — it is scratch, not a deliverable): `/home/user/.gopi/sessions/01a0d59c-7983-7086-8753-34e87ad05da9/evidence/` — `r1.txt` (req 1: one live surface, discovered with no path, exit 0, row 0 = `gopi ui → …`), `r1list.txt` (`--list`, exit 0), `r3.txt` (req 2: exit 2 ambiguity, both candidates), `round3.log` (req 3: `state=live` → SIGKILL → `state=exited` + `ExitedNotice`), `round2.log` (the all-blank/`frames=0` pty case), `live-check.sh` / `pty-*.py` (the drivers, copy-pasteable).
- **The cited task is completed** — the drift claim's own citation is now false on the ledger: `gopi task show 01a0d337-3e73-79ec-bdcb-1a60ca1bbe9d` → `status: completed`, `summary: ui_state 工具：…（观测闭环的最后一步）`, 1 run `completed exit=0`, 422 turns.
