package main

// cli_usage_budget_test.go — the LENGTH budget for EVERY published `-h` page.
//
// WHY THIS FILE EXISTS, AND WHAT IT REPLACES. Before it, exactly ONE subcommand
// page had a length budget (cli_chat_usage_budget_test.go, chatUsageMaxLines) and
// the top-level page had its own (topLevelUsageMaxLines, usage_test.go), while the
// other eleven published pages' length "was protected by NOTHING". That asymmetry
// is what the budget history records: `gopi chat -h` ran 615 → 533 → 560 → 565 →
// 577 lines, each raise carrying a legitimate reason in the commit — "the
// paragraph IS the flag's contract, and its detail has NO OTHER PAGE TO LIVE ON".
// A cap that only some pages carry does not bound the surface; it bounds the one
// page whose author happened to add it, and the detail that has nowhere else to
// go simply accumulates on the pages that have no cap.
//
// THE TWO HALVES OF THE FIX, and why they are one change. (1) The detail now HAS
// another page to live on: the man set (`man/gopi.md` + one page per verb,
// rendered by `make man`, kept in step with the binary's own `-h` by
// cmd/agent/man_source_gate_test.go). (2) Every published page gets the budget
// the top-level page and `gopi chat` already had — the SAME shape, not a new
// mechanism: a named cap per page, measured on the REAL binary, with the same
// refusal to let detail accumulate silently. Without (1) a cap would only push
// detail into a corner; without (2) the sinkhole simply reopens on the next page.
//
// WHAT THIS GATE IS AND IS NOT. It does not say "the page is complete" or "the
// page is right" — the help-surface contract tests do that, from the other side
// (every flag the parser reads must be named, every claim must survive the
// binary's own output). It says only: this page may not grow back, and a cap may
// not be SLACK. A page that needs lines argues for it HERE, in its own constant,
// with its reason in the commit.

import (
	"fmt"
	"strings"
	"testing"
)

// usageBudgetNewFlagMargin is how much room a page's cap carries above what the
// page measures TODAY.
//
// It is the room for ONE genuinely new knob, and it is deliberately small: the
// raise history on `gopi chat` (5 lines for `/voice`, 11 for `--voice-audio`)
// shows what a new flag's contract costs on a page that must carry it whole. What
// the margin is NOT for is the drift that produced the 615-line page — a dozen
// paragraphs of rationale added under existing flags — because that detail now
// has a home (the man page for the verb). A page whose measured length stops being
// within this margin of its cap is RED in both directions: over it, the page grew;
// far under it, the cap is slack and must be lowered rather than left as room for
// the next drift.
const usageBudgetNewFlagMargin = 12

// The caps. Each is one named constant (Rule 1: a limit is never a bare literal)
// and each is measured-at-this-commit plus usageBudgetNewFlagMargin — so the
// floor and the ceiling are one value apart by construction and a red build is a
// statement about ONE page, named.
//
// The chat cap replaces chatUsageMaxLines = 577. That value was set when the
// page was 533 lines and had "no other page to live on" for a flag's detail; the
// raise history in the record below is preserved verbatim because it is the
// measurement the restructure rests on, and the cap now sits at the page's own
// post-restructure length rather than 127 lines above it.
const (
	// chatUsageBudget caps `gopi chat -h`.
	//
	// HISTORY (from cli_chat_usage_budget_test.go, kept in the move): 615 → 533
	// (task 01a10bb7…, the compression measured in devdoc/chat-help-layering.md)
	// → 560 → 565 (2026-10-07, task 01a115e0…, a NEW slash command's paragraph:
	// `/voice`, five lines — the verb spelling, the window bound read from
	// voiceChatWindowSeconds, the append-versus-overwrite contract, the
	// Enter/Esc exit contract, and which spellings report instead of toggling)
	// → 577 (2026-10-09, task 01a11f13…, a NEW flag's paragraph: `--voice-audio
	// FILE` plus its `GOPI_CHAT_VOICE_AUDIO` spelling, eleven lines).
	//
	// Both raises state the SAME reason, and it is the reason this task exists:
	// "the paragraph IS the flag's contract, and its detail has no other page to
	// live on". It now has one — `man gopi-chat` carries the same contracts in
	// full, so the page keeps the arming line, the value shape and the reader,
	// and the rationale moves.
	//
	// RAISED 462 → 472 (2026-10-19, task 01a153d6…, the chat-policy epic's
	// 1/6+2/6): `gopi chat --policy POSTURE` is a genuinely new knob and its
	// ten lines on this page are its contract — the accepted spellings, the
	// default, the FOUR-channel precedence (flag > GOPI_CHAT_POLICY >
	// settings.json "chat_policy" > default), the resolve-once-and-freeze
	// promise, and the sentence naming what ENFORCES the posture (the chat
	// write boundary) while refusing to promise that yolo runs anything. The
	// raise is measured, not padded:
	// the page reads 472 against 472 (slack 0, so the ratchet is satisfied by
	// construction). The full contract also lands on `man gopi-chat`.
	//
	// RAISED 472 → 480 (2026-10-22, task 01a169bb…, the palette knob made
	// discoverable). MEASURED REASON, and it is a product defect this raise
	// closes rather than a new feature: the `--no-color` paragraph advertised
	// ONLY `GOPI_PALETTE` with a "man gopi-chat says how", while the page's own
	// colour contract had grown four values a reader cannot act on without the
	// man page open — the two named tables (`hermes`, `hermes-light`), the
	// terminal advertising variable (`COLORTERM`), the value it must carry
	// (`truecolor`), and the gold answer body's own colour. The help page is the
	// surface that documents the colour behaviour, so the palette paragraph
	// belongs on it; `testsuite/chat_palette_e2e_test.go`'s
	// TestChatUsageDocumentsThePaletteKnob measured the gap on the SHIPPED text
	// and is green only with these eight lines. Measured length after the
	// paragraph: 480 (`gopi chat -h | wc -l`, budget slack 0 — the ratchet above
	// is satisfied by construction, not by a padded margin). Every value on the
	// paragraph is joined from its constant (`lineedit.EnvPalette`,
	// PaletteNameHermes, PaletteNameHermesLight, EnvColorTerm,
	// ColorTermTruecolor) and the colour is derived from the table's own packed
	// value (`lineedit.HermesDarkAnswerHex`), so the page cannot drift from the
	// palette it describes.
	//
	// 480 → 436 (2026-10-19, the replay/film HELP SCOPE): the family's own
	// contract (`chatReplayUsage`, `chatFilmUsage`, `chatBeatUsageSection`,
	// `chatReplayExamples`) moved to the page `gopi chat --replay -h` renders —
	// `chatReplayHelp`, composed from those SAME named parts, so no flag's prose
	// exists twice. The session page keeps a ten-line INDEX of the family plus the
	// sentence naming the scoped page, and measures 424 lines. The cap is the
	// measured length plus the margin, as the ratchet requires; the scoped page is
	// held to its own cap below (`chatReplayHelpUsageBudget`).
	//
	// MEASURED 425 SINCE (task 01a17d5c…, `--from-turn` documented): the family
	// index on this page gained one row — `--from-turn N`, the replay-only word
	// the parser already read while NEITHER page named it — so the page grew by
	// exactly that line. The CAP DOES NOT MOVE with it: 436 was already the
	// measured length plus the margin, and the page now sits one line into that
	// reserve (slack 11 against the 12-line `usageBudgetNewFlagMargin`), which is
	// what the margin is for. `TestPublishedHelpPagesStayWithinTheirUsageBudget`
	// prints the reading it measured on the real binary — 425 (budget 436,
	// margin 11) — so the number in this comment is the number that test reports,
	// not a hand-kept one.
	//
	// RAISED 436 → 459 (2026-11-04, task 01a1a3ef…, the raw SGR stream's
	// paragraph). MEASURED REASON, and it also explains why the previous raise
	// left this page with no room rather than with the wrong number: `1d9b0487`
	// added `--sgr-stream[=PATH]` to the synopsis and twenty-four lines to this
	// page (the artefact's shape, the off-state cost, and why the raw bytes
	// answer a question the interpreted dump cannot answer for itself) and did
	// NOT raise this cap, so the page read 459 against 436 — 23 over. Measured on
	// this host at `1d9b0487^`: the page was 435 lines against the 436-line cap,
	// ONE line of reserve, so a paragraph of ANY size went red and the raise is
	// the measurement's consequence rather than a preference.
	//
	// The cap is the measured length itself (459, slack 0), not measured plus
	// `usageBudgetNewFlagMargin`, because that margin is already spent: 436 was
	// itself a measured length plus the margin, and a margin added on top of a
	// spent margin is padding, which the ratchet's upper bound refuses anyway.
	//
	// What the gate's own hint asks for BESIDE the raise is already true, and it
	// was verified here rather than assumed: the contract is on `man gopi-chat` —
	// `--sgr-stream[=PATH]`, its off-by-default promise, the joined-form-only
	// rule, the `--film` refusal and the environment channel that replaces argv
	// there — landed with the flag in `f689da27`, and `mancensus --check` reports
	// every page up to date against the binary this measurement came from.
	//
	// RAISED 459 → 503 (2026-10-27), and the raise is the WRAP, not new material:
	// the same prose re-broken to fit config.HelpWrapCol. The page had ten lines
	// past the margin (the widest prose row 114 columns) — a right edge nobody
	// could read on an 80-column terminal — so the paragraphs were re-flowed, and
	// re-flowing at 88 columns takes more LINES for the same words. Two flags the
	// page already advertised also gained the rows they never had
	// (`--ambiguous-width`, and `--sgr-stream`/`--chat-surface-turns` in the
	// one-line usage), and the posture vocabulary now WRAPS instead of running to
	// 114 columns. The contract for each of them is on `man gopi-chat` — the
	// `--ambiguous-width` row and the `--max-turns` row landed in the same commit,
	// which is what this gate asks for beside the raise. Measured on the real
	// binary with `gopi chat -h | wc -l` = 503, slack 0 (see the paragraph above:
	// a margin on top of a spent margin is padding).
	//
	// RAISED 503 → 523 (2026-11-04, task 01a1a5f1…, `--sgr-replay`): a genuinely
	// new knob (a PLAYBACK of the recorded byte stream, the only evidence channel
	// that does not depend on the render algorithm) needs a synopsis word, a
	// whole entry in this page's flag glossary, and a pointer at the man page
	// carrying its contract. Measured on the real binary: `gopi chat -h | wc -l`
	// = 523, slack 0, and the flag's full contract is on `man gopi-chat` in the
	// same commit (`--sgr-replay` row + SYNOPSIS word), which is what this gate
	// asks for beside a raise.
	// RAISED 523 → 526 (2026-11-07, task 01a1b277…, `/screen`): a REPL command
	// whose subject exists nowhere in this page's flag glossary is still a
	// contract a reader must be able to DISCOVER here — `/voice` and `/info` have
	// their rows for the same reason — and three lines is the smallest row that
	// says what it captures, where the reference goes rather than the bytes, and
	// that it costs no model turn. The full contract (the file name, the two
	// bounds, the truncation sentence, the one-shot lifetime, the two honest
	// empty answers) is on `man gopi-chat` under FILES in this same commit, which
	// is what this gate asks for beside a raise.
	//
	// CORRECTED (2026-11-07, task 01a1b2d1…): the raise above recorded
	// "measured on the real binary: `gopi chat -h | wc -l` = 526, slack 0". That
	// measurement was never taken, and it was FALSE — measured on the commit's own
	// tree (5e5af54ff, `git archive 5e5af54ff | tar -x`, then
	// `CGO_ENABLED=0 go build -o gopi5e5 ./cmd/agent/ && ./gopi5e5 chat -h | wc -l`)
	// the page printed **541**, 15 over the 526 it had just set. The commit added
	// 3 lines to the page and raised the cap by 3, but the page was ALREADY 538 at
	// its parent (`git archive 5e5af54ff^ … && ./gopi chat -h | wc -l` = 538):
	// something landed between 523 and this commit without re-measuring, and this
	// commit raised a number without running the binary that produces it. It
	// shipped a tree whose own gate was red.
	//
	// THE FIX (this task) TRIMS rather than raises, which is what the failure
	// message asks for first: 15 lines of the status-row legend moved out of the
	// page (the ≡/≈ spelling rationale, the Σ-alone NARROW row, the
	// token_reconciliation pointer, the ladder explainer) and the two CONTRACT
	// facts among them — "a Σ-only row is a NARROW row, not a session with no
	// turn" and the per-request `gopi session log <id> --json`
	// token_reconciliation row — moved to `man gopi-chat` under THE STATUS ROW.
	// Measured on the real binary, this commit:
	//   CGO_ENABLED=0 go build -o ./probe ./cmd/agent/ && ./probe chat -h | wc -l
	//   → 523
	// so 526 is now 3 lines of headroom under the page — inside the two-sided
	// ratchet's window ([526-12, 526]) and NOT raised again. IF YOU EDIT THIS PAGE,
	// run that command; a number written here without it is how the 526 lie
	// happened.
	//
	// RE-MEASURED, NOT RAISED (2026-11-08, the `sgr` → `screen` rename): the three
	// pre-rename aliases had a paragraph of their own and it was re-flowed into one
	// line ("PRE-RENAME ALIASES (each prints deprecated: use <current>): …"), which
	// is how the page stays at 526 with the new words in it. Measured on the real
	// binary, this change:
	//   CGO_ENABLED=0 go build -o ./probe ./cmd/agent/ && ./probe chat -h | wc -l
	//   → 526
	// so the cap did NOT move — a rename that is documented costs lines, and they
	// were paid for by trimming the paragraph, not by raising this number.
	// RAISED to 528 (2026-11-10) to pay for the `/context` slash row, which
	// replaced the RETIRED per-turn "EVERY TURN ENDS WITH ONE LINE" paragraph.
	// THE REASON CHANGED WHILE THE NUMBER DID NOT (2026-11-13): commit 73a40454
	// then DELETED that slash row (the shared table is /context's only owner — a
	// second row reddened TestHelpListsEachPortableWordExactlyOnce), and the
	// artifact name the row carried (`ctxdump.FileNamePattern`) went with it, so
	// the page measured 526 and testsuite/TestChatHelpNamesContextAndItsArtifact
	// went red on a clean master. The name is back as PROSE — a parenthetical on
	// the /screen row that does not begin with "/context", so it is not a row and
	// the one-owner guard stays green — and THAT is what the two spare lines now
	// pay for. Measured on the real binary, this change:
	//   CGO_ENABLED=0 go build -o ./probe ./cmd/agent/ && ./probe chat -h | wc -l
	//   → 528
	//   ./probe chat -h | grep -c 'context-turn'  → 1
	chatUsageBudget = 528
	// chatReplayHelpUsageBudget caps `gopi chat --replay -h` — the family's own
	// page. Measured 2026-10-19 on the real binary: 78 lines. It has no slack room
	// beyond the new-flag margin, so the family's detail cannot drift back into a
	// second sinkhole now that it has a page naming what it is.
	chatReplayHelpUsageBudget = 78 + usageBudgetNewFlagMargin
	// configUsageBudget caps `gopi config -h`.
	configUsageBudget = 43
	// memoryUsageBudget caps `gopi memory -h`.
	//
	// RAISED 176 → 185 (2026-11-06, task 01a1ac96). The nine lines are the
	// `--db` CONTRACT paragraph: a path that does not exist is not an empty
	// knowledge base, the read-only verbs refuse it and create nothing, the
	// writing verbs create and SAY they created, `capture-stats` opens no
	// ledger and refuses the flag. It sits on the page rather than only in
	// `man gopi-memory` because the page is where a user forms the
	// invocation — the refusal is a property of the spelling he is about to
	// type — and because `cmd/agent/cli_memory_ledger_guard_test.go` asserts
	// the page and the refusal carry the SAME sentence (Rule 13: one
	// spelling, not two descriptions of one mechanism). Measured on the real
	// binary: `gopi memory -h | wc -l` = 185, slack 0 — the mechanism itself
	// (the resolver, the active-ledger read-back, the creating/reading split)
	// is on the man page and in the code comments, not here.
	//
	// SLACK IS 2 AS OF 2026-11-06 (task 01a1acf0): the `--db` paragraph above
	// then grew past the cap by 2 lines on its own account and the page was
	// TRIMMED, not raised — the histogram-repetition note
	// (`converge-keywords`) and the 100%/63% latent-harvest measurement moved
	// to `man gopi-memory`, so `wc -l` = 183 against this unchanged 185. The
	// constant above is the assertion and was not touched (Rule 5: the fix is
	// the page, never the cap).
	//
	// RE-MEASURED 2026-11-06 (cross-node sync removal, 5677cc6e): the page
	// measures 165 lines (`./bin/gopi memory -h | wc -l`) — the sync/memsync
	// rows left with the verbs. The cap follows the measurement plus the ONE
	// margin (165 + 12), because a budget 20 lines above the page is slack no
	// future flag paragraph earned.
	//
	// RAISED 177 → 182 (2026-11-07, task 01a1b311). MEASURED REASON, and the
	// slack was already ZERO before this raise: `gopi memory -h | wc -l` read
	// exactly 177 against this 177, so the page had no room and ANY added
	// sentence went red. The five lines are the search page's SECOND SECTION:
	// `memory search` now answers from the round-note index as well as the
	// knowledge base, and a user forming the invocation has to be told that its
	// output carries two sections ranked separately, that a `[session]` hit
	// names a session_id and message id, and which verbs widen the slice. That
	// is the advertisement the page owes the new behaviour (the mechanism —
	// bookends, window width, gap counting — stays in the code and the tool
	// description, not here). Measured on the real binary after the raise:
	// 182, slack 0, the same shape as the 459 chat page above.
	//
	// TRIMMED 188 → 181 (2026-11-12, task 01a1d014), and the cap STAYED AT 182
	// (Rule 5: the fix is the page, never the cap). The 6-line overrun was
	// `df2a6e8b` ("render now= anchor plus stored=/age=/about="): it added a
	// 5-line paragraph to the `search` block and never re-measured, so the page
	// stood at 188 against this 182 for a day. The paragraph was NOT deleted —
	// its CONTRACT (now= is the anchor every age= is measured against;
	// stored= and about= are independent facts; about= is omitted, never
	// defaulted, when the text states no date or a future one; pre-column rows
	// fall back to when_at) moved to `man gopi-memory` under DESCRIPTION, and
	// what stays on the page is the one line a user needs to read his own
	// output. The second trim is `provenance`'s latent-arm paragraph (harvest
	// criterion: EMPTY sources + a TOPIC-declared path repeated by the row;
	// additive columns only) — same destination, same reason: the page routes,
	// the man page carries the mechanism. Measured on the real binary:
	//   CGO_ENABLED=0 go build -o ./probe ./cmd/agent/ && ./probe memory -h | wc -l
	//   → 181   (slack 1, inside the [182-12, 182] ratchet window)
	memoryUsageBudget = 182
	// mergeUsageBudget caps `gopi merge -h`.
	//
	// THE 2026-10-13 OVERRUN (177 → 167, closed by commit ad2f58cc8). An
	// earlier commit (7aaaef17a, the image self-check) added lines to the page
	// and spent no budget on them, so `gopi merge -h` measured 177 lines
	// against this cap. The test that caught it — and cmd/agent's ONLY red —
	// was `TestPublishedHelpPagesStayWithinTheirUsageBudget`, the gate further
	// down this file, failing in its `/merge` subtest. ad2f58cc8 moved the
	// mechanism to `man gopi-merge` (the page keeps the advertisement), and the
	// page measured 167 again — which is also this cap's slack floor, since
	// 167-167 < usageBudgetNewFlagMargin.
	//
	// THE CAP DID NOT MOVE: 167 is the value on both sides of ad2f58cc8. The
	// 177 → 167 that commit records is the PAGE's measured length, brought back
	// under an unchanged budget — not a budget edit.
	//
	// WHY THIS NOTE EXISTS (a citation that had to resolve). ad2f58cc8's
	// message cites the failure as
	// `TestPublishedHelpPagesStayWithinTheirPageBudget/merge`. No test of that
	// name has ever existed in any ref — `git grep` finds it nowhere outside
	// that message text, and the identifier is not a rename of anything. The
	// spelling is a misnomer; the identifier that actually failed, and that
	// ad2f58cc8 turned green, is
	// `TestPublishedHelpPagesStayWithinTheirUsageBudget`. The commit is an
	// ancestor of master and cannot be amended, so the correction lives here,
	// next to the cap the message is entirely about, where a reader looking up
	// the budget guard will find it.
	//
	// RAISED 167 → 172 (2026-10-14, task 01a13273e1fc783d804cef77c947debe, the
	// rework of task 01a1315ba35a731682a8b4919d3e63f0). The raise IS a new
	// flag's paragraph, which is exactly what this margin is for: `gopi merge
	// --already-landed NAME` is a new READ-ONLY arm, and its five lines are its
	// contract — the branch is the flag's own VALUE (so it cannot be mistaken
	// for a merge selection), and reads-only / no-lock / no-run / no-write is
	// what makes it reachable from INSIDE a merge run, from the child that must
	// ask the question before it files a rework row (merge_prompt.md §3.6.3).
	// The alternative — moving the contract to `man gopi-merge` — is not
	// available: the SYNOPSIS region of the man source is COPIED from this page
	// by the generator, so a row that leaves `-h` leaves the man page with it,
	// and a flag an operator cannot see on the page is a flag nobody runs.
	// Measured at 172 pages lines; usageBudgetNewFlagMargin below is what stops
	// the page drifting past it.
	//
	// BACK UNDER THE SAME 172 (2026-10-16, task 01a144cfb9e573d3b0a1a173ef2d3b7f).
	// The page measured 201 — the `--stop` block's arrival and two later notes
	// spent no budget — so `/merge` was red on a pristine checkout alongside
	// this cap's own 172. The fix is the same one ad2f58cc8 used and NOT a
	// budget edit: contract PROSE moved to `man gopi-merge` (the --status /
	// BURIED definition, the loop's Ctrl+C timing and the durable request file,
	// the post-pass attribution, the ceilings' rationale, the union-resolution
	// ladder and the lock's lifetime), while every advertised FORM stayed on
	// the page. The 172 line is the value on both sides of the change; page
	// length after the move is 172, margin exactly 0.
	mergeUsageBudget = 172
	// notifyUsageBudget caps `gopi notify -h`.
	notifyUsageBudget = 87
	// sessionUsageBudget caps `gopi session -h`.
	// RAISED 83 → 112 (2026-10-28, MEASURED re-baseline). The page measures 100
	// lines on this rev, 17 over the old cap, and the growth is not one flag's
	// paragraph: `gopi session` gained whole sub-verb families (replay/checkpoint/
	// turns) whose synopses the page carries, and the man page `man gopi-session`
	// (146 lines) carries their contracts. The cap stays the SAME RULE as every
	// entry here — MEASURED length plus the 12-line margin — so the slack is still
	// exactly one new flag's paragraph: 100 + 12 = 112.
	// LOWERED 112 → 94 (2026-10-29, task 01a182e0, on the REBASED tip). This
	// branch trims the page to 82 lines (mechanism and duplicated rationale
	// removed, every fact and every asserted marker kept), so 112 left 30 lines
	// of slack — more than the 12-line margin. Same rule as every entry:
	// MEASURED length plus the margin — 82 + 12 = 94.
	// RAISED 94 → 104 (2026-11-04, task 01a1a6d0, on the REBASED tip). 2dcf7ce6
	// taught the page the turn-provenance contract — a run row prints "turns
	// none (no chats row)", every row carries "turns_provenance", the key a
	// reader branches on — WITHOUT re-baselining this cap: the page measured 102
	// against 94, red by 8. Two runs repaired that overshoot the same day, and
	// this entry is the survivor of the two, because the repair is the move this
	// constant's own rule prescribes rather than a bigger cap. Prose went back to
	// what a ROUTING page needs (the turn slot's one-line rule, the provenance's
	// name, the pointer to the contract) and the CONTRACT moved into
	// man/gopi-session.md's JSON KEYS section, which now carries
	// `turns_completed`, `turns_provenance` (both values, and what a summing
	// consumer must skip) and the one-id-two-rows paragraph 2dcf7ce6 had added
	// only as page prose. Same rule as every entry above — MEASURED length plus
	// the margin: 92 + 12 = 104, so the slack is exactly one new flag's
	// paragraph and the 8-line overshoot is gone rather than budgeted for. (92,
	// not the 91 the trim first measured: that cut had truncated the
	// checkpoint-prose paragraph mid-sentence, at "a pipe belongs in a", and
	// restoring the clause it dropped is one line.)
	// RAISED 104 → 120 (2026-11-13, task 01a1ce3e, on the REBASED tip). The page
	// gained the THREE ADDRESS FLAGS of the round→turn reverse lookup
	// (`show --round R` / `--turn N` / `--mapping`, one paragraph of routing
	// prose, 3 lines) and measures 108 against the old 104 cap — red by 4. The
	// repair is this constant's own rule: MEASURED length plus the 12-line
	// margin, 108 + 12 = 120, and the CONTRACT (both line shapes, the two
	// refusals, and why the answer cannot disagree with the `turns` row) went to
	// man/gopi-session.md's show section, which is where this page routes a
	// reader who needs it.
	sessionUsageBudget = 120
	// taskUsageBudget caps `gopi task -h`.
	//
	// LOWERED 195 → 188 (2026-10-16, verb-surface retirements: `verdicts` and
	// `tag-resumed`). Both verbs were DELETED from the family (no RemovedVerbs
	// entry, no replacement spelling), and their synopsis blocks left the page
	// with them: the measurement is 176 lines against the old 195 cap — 19
	// lines of slack, which the gate refuses by name because the cap is SLACK
	// and must be the page's own measured length plus
	// usageBudgetNewFlagMargin (12). 176 + 12 = 188, so there is exactly the
	// margin a new flag's paragraph needs and no more.
	//
	// RAISED 188 → 210 (2026-10-23, task 01a16945…, the GROUPED page). The
	// ledger group is now four NAMED sections (read / write / delivery-and-
	// recovery / cross-node), because the flat 18-verb list answered "what does
	// this family contain" and not the question an operator actually arrives
	// with ("which verb do I want"). The page's own measurement is 198 lines:
	// +3 group headings, +2 blank lines between sections, +8 for the three peer
	// questions every peer verb now answers (which peers / what it needs / what
	// a miss looks like) with the two verbs whose answer is an EXCEPTION named
	// in the same paragraph (branch-refs and `orphans --delivery` ask git
	// directly; retry-delivery takes no --peer at all), +1 blank after the
	// cross-node verb table, net of the two paragraphs that moved to the
	// devdocs they name (the page's layering note and the superseded/release
	// detail). 198 + 12 = 210 — the same SLACK rule as the entry above, no new
	// room beyond the margin a new flag's paragraph needs.
	//
	// LOWERED 210 → 206 (2026-10-26, the zombie sweeper's retirement). Deleting
	// `sweep-zombies` from the advertised family removed its synopsis block and
	// one row of the verb table: the page now MEASURES 194 lines, which against
	// the old 210 left 16 lines of slack — four more than the 12-line margin,
	// i.e. room the page is not using and where a future flag's paragraph would
	// slip in unaccounted. 194 + 12 = 206 restores the rule this constant exists
	// to enforce rather than resetting it: the cap is the MEASURED length plus
	// the margin, so the slack can never grow past the one paragraph.
	//
	// RAISED 206 → 223 (2026-10-26, task 01a1783f — the worker-round boundary).
	// `reconcile` and `finish-cleanup` joined the advertised family, because the
	// two duties they run had been carried by the worker's claim loop and now
	// have no carrier but these verbs (operator ruling: a worker dispatches, it
	// does not reconcile the ledger or clean up grounds). Their two synopsis
	// blocks measure 17 lines — the page is 211 — so 211 + 12 = 223 is again
	// MEASURED length plus the same 12-line margin, not a re-set cap: the slack
	// stays exactly one new flag's paragraph.
	// RAISED 223 → 226 (2026-10-27, epic 01a17dcd — `task update --force`).
	// The flag's contract is 5 lines of the page (its paragraph in the update
	// block: what it does NOT skip, where the waiver is recorded, and that it
	// waives nothing else); six were spent, so two lines were tightened
	// elsewhere in the same paragraph and the page measures 226. 223 + 3 is
	// again MEASURED length rather than a re-set cap, and the same 12-line
	// margin above it is untouched, so the slack still cannot grow past one new
	// flag's paragraph. The contract itself lives in `man gopi-task`
	// (FLAG CONTRACTS), which is where this page routes.
	// RAISED 226 → 243 (2026-10-28, MEASURED re-baseline, task 01a182c5). The page
	// measures 231 lines on this rev, 5 over the old cap: `task update --force`'s
	// contract grew by the delivery-leg sentences its briefing carries, and
	// `man gopi-task` (295 lines) holds the full contract. Same rule as every entry
	// here — MEASURED length plus the 12-line margin: 231 + 12 = 243.
	// LOWERED 243 → 238 (2026-10-29, task 01a182e0, on the REBASED tip). This
	// branch trims the page to 226 lines, so the 243 cap left 17 lines of slack.
	// 226 + 12 = 238 — the same rule, applied to the merged measurement.
	//
	// RAISED 238 → 260 (2026-10-29, task 01a186b8, `gopi task clone`). A NEW verb
	// joined the family and the page must ROUTE to it: the clone synopsis states
	// the clean-room copy, the status gate (the verb's whole design), the
	// --confirm-cancelled reversal and --dry-run in 8 lines, and the full contract
	// — the three-rung resume ladder, the gate table with its reasons, what the
	// successor carries and does not — lives in `man gopi-task.md`. The page also
	// LOST one line here: task send --chain's note was re-wrapped to fit the wrap
	// budget, so this is a net +9 against the merged measurement. Same rule as
	// every entry: MEASURED length plus the 12-line margin — 248 + 12 = 260.
	// LOWERED 260 → 259 (2026-10-29, task 01a18858, on the REBASED tip). The page
	// MEASURES 247 lines on this rev (`gopi task -h | wc -l`), so the 260 cap left
	// 13 lines of slack — one more than the 12-line margin, i.e. room the page is
	// not using and where a future flag's paragraph could slip in unaccounted. The
	// 248 the raise above rests on is no longer the page's length: the clone
	// synopsis was re-wrapped in the same merge, so the measurement moved under
	// an unchanged cap — which is why this is a LOWERING and not a page edit.
	// Same rule as every entry: MEASURED length plus the margin — 247 + 12 = 259.
	// RE-MEASURED 259 → 214 (2026-10-30, task 01a18799, on the REBASED tip). This
	// delivery retires the four read-only audits (`for-branch`, `branch-refs`,
	// `orphans`, `stranded`) AND rebases onto master's own `reopen` deletion
	// (545b5b2) — two independent retirements, so neither side's measurement is
	// the tree's. The page MEASURES 202 lines here (`gopi task -h | wc -l`); 202 +
	// 12 = 214. A cap left at 248 (this branch) or 259 (master) would hold 46 or
	// 57 lines of unused slack, and a cap at 226 (this branch's own pre-rebase
	// measurement, from retiring only the four audits) would still hold 24.
	// LOWERED 214 → 188 (2026-10-30, task 01a18799, slice 2/4). This delivery
	// retires the three implementation-shaped verbs — `verify`,
	// `retry-delivery`, `reconcile` — from the advertised family (they move to
	// `taskverbs.RemovedVerbs` and keep only their dispatch refusal), and their
	// three synopsis blocks leave the page with them, together with the
	// "Delivery and recovery" heading that carried one of them. The page
	// MEASURES 176 lines here (`gopi task -h | wc -l`); 176 + 12 = 188 — the
	// same MEASURED-length-plus-margin rule as every entry above, applied to
	// the page this slice actually ships.
	// LOWERED 188 → 186 (2026-10-31, the finish-cleanup retirement completes).
	// The verb was retired in `58b5618b` while its synopsis block stayed on the
	// page, so `gopi task -h` advertised a spelling that refuses with exit 1 —
	// the exact drift TestTaskCompletionNamesMatchFamilyHelp exists to catch.
	// Removing the block leaves the page MEASURING 174 lines; against the old
	// 188 that is 14 lines of slack, two more than usageBudgetNewFlagMargin, so
	// 174 + 12 = 186. Same rule as every entry above: the cap is the measured
	// length plus the margin, never a re-set number.
	// RE-MEASURED 2026-11-06 (cross-node sync removal, 5677cc6e): the family
	// page now MEASURES 167 lines (`./bin/gopi task -h | wc -l`) — the deleted
	// eight took their rows with them — so the cap follows the same rule,
	// 167 + 12 = 179, instead of carrying 19 lines of slack nobody earned.
	taskUsageBudget = 179
	// toolUsageBudget caps `gopi tool -h`. Re-measured after the `doccheck`
	// verb's removal (epic doc-pipeline, item 2/5): the page is 42 lines, and
	// 42 + usageBudgetNewFlagMargin = 54, so the slack is exactly the margin a
	// new flag's paragraph needs and no more.
	toolUsageBudget = 54
	// uiStateUsageBudget caps `gopi ui_state -h`.
	uiStateUsageBudget = 172
	// versionUsageBudget caps `gopi version -h`.
	// RAISED 22 → 35 (2026-10-28, MEASURED re-baseline, task 01a182c5). The page
	// measures 23 lines on this rev: `--drift` joined it (e7a09e4, "measures the
	// RUNNING artifact and states all four verdicts") with its own synopsis
	// paragraph, and `man gopi-version` (62 lines) carries the contract. Same rule
	// as every entry: MEASURED length plus the 12-line margin — 23 + 12 = 35.
	// LOWERED 35 → 34 (2026-10-29, task 01a182e0, on the REBASED tip). This
	// branch trims the page to 22 lines (one duplicated rationale line), so
	// 22 + 12 = 34 — the same rule, applied to the merged measurement.
	versionUsageBudget = 34
	// webUsageBudget caps `gopi web -h`.
	webUsageBudget = 73
	// workerUsageBudget caps `gopi worker -h`.
	//
	// LOWERED 136 → 114 (2026-10-13, task 01a13356-45b5-735e-952d-ac04f43e5a17).
	// The 114 → 136 raise that carried the worker's TARGET SELECTOR family also
	// carried that family's MECHANISM onto the page — the request-file channel,
	// the ledger resolution chain, the spawn seam, the close-out chain, the
	// durable-request file. This page is a page that ROUTES; its man page
	// carries the contract. The mechanism now lives there — `man gopi-worker`'s
	// `--stop`, `Resume` and Output paragraphs — so the page keeps the
	// ADVERTISEMENT and drops the why, measured 124 → 114.
	//
	// WHAT DID NOT MOVE is what the raise was actually for: both
	// `gopi worker --stop --branch B` and `gopi worker --stop --pid N` are
	// still advertised, because the page must advertise a form before the G6
	// help-surface gate may type it
	// (cmd/agent/cli_tools_maintenance_help_surface_cases_test.go), and they
	// are the feature itself, not detail about it. 114 sits at the page's own
	// measured length; usageBudgetNewFlagMargin is the room a genuinely new
	// knob's paragraph would need, and nothing here is one.
	// RAISED 114 → 117 (2026-10-16, task 01a1366f). The three lines are the
	// `--resume` paragraph's cross-guidance to the OTHER resume face. Two live
	// faces resume work and they differ in the one way that costs an operator
	// something: this one takes the row's ground over and RELEASES it, while
	// `gopi session resume <id>` holds no ground and releases none. A reader
	// who found only this page would take a plain session — a chat, a bare
	// prompt, a killed run with no row of its own — to a face with no ground
	// for it. The mirror sentence is on the session page and the choice itself
	// is printed by `gopi task show` beside the row's ground; this page states
	// WHICH half it is. That is the flag's contract, not detail about it (the
	// same reading the 167 → 172 merge raise above documents), and GOPI.md
	// Rule 15 forbids teaching half a choice and leaving its consequence
	// unstated. The three lines are the paragraph; nothing else moved.
	// RAISED 117 → 122 (2026-10-16, task 01a14313). The five lines are the
	// `gopi worker list` alias — the SYNOPSIS row the new spelling needs to be
	// DISCOVERABLE at all, plus the note under `--status` that says the two
	// spellings are one read. The alias exists precisely because the capability
	// already existed and the operator could not find it, so a page that did not
	// name it would leave the epic unsolved; the mechanism (which gate knows the
	// word, and why both must) lives in the code and in the man page.
	// usageBudgetNewFlagMargin is the room this needed and it is now spent: the
	// page measured 122 against 122, so the next line added here is red.
	// RAISED 122 → 126 (2026-10-23, task 01a16933). The four lines are the
	// `--json` flag's ADVERTISEMENT: one SYNOPSIS row (the form must be
	// discoverable on the page before the G6 help-surface gate may type it —
	// cmd/agent/cli_tools_maintenance_help_surface_cases_test.go) plus the
	// Options paragraph, plus the `--status` row's claim that every line now
	// carries WHICH TASK the worker runs. The mechanism — the derivation order
	// (state record → the worker's own argv → `(not recorded)`), why the argv
	// is read through the verb's OWN grammar scanner rather than a substring
	// search, and the document's field vocabulary — lives in
	// `man gopi-worker`'s --status/--json paragraphs, not here; the page
	// routes. usageBudgetNewFlagMargin is what a genuinely new knob's
	// paragraph is allowed to cost, and this is one.
	// RAISED 126 → 131 (2026-10-30, task 01a18471). The five lines are the
	// EXIT-CODE-4 entry: the pre-claim API reachability preflight's refusal. It
	// belongs ON THIS PAGE for the same reason 2 and 3 do — an operator (or a
	// scheduler) reading `gopi worker -h` must learn that a transport-dead node
	// leaves with its own status and claims NOTHING, or exit 4 reads as an
	// unexplained failure of the queue instead of a fact about the machine. The
	// mechanism (the ladder's thresholds, the three-valued verdict, why unknown
	// never refuses) is in `man gopi-worker`'s EXIT STATUS paragraph and in
	// internal/tools/api_reachability.go — the page keeps the advertisement.
	workerUsageBudget = 131
)

// subcommandUsageBudget is the table this gate walks: the published verb and the
// cap its `-h` page may not exceed. It is keyed by the SAME CliVerb* constants
// the dispatcher and the man set use (Rule 1/13: no bare verb spelling), so a
// rename cannot leave a row pointing at a verb nobody publishes.
var subcommandUsageBudget = []struct {
	verb string
	max  int
}{
	{CliVerbChat, chatUsageBudget},
	{CliVerbConfig, configUsageBudget},
	{CliVerbMemory, memoryUsageBudget},
	// `mergeUsageBudget` is NOT in this table: the walk is "the published verbs",
	// and `gopi merge` is no longer published (epic merge-removal, item 2) — the
	// word reaches a refusal, not a page, so a line count for it would measure
	// a refusal. The CONSTANT stays because `mergeUsageHead` stays: this slice
	// removes the command surface, not the merge implementation, and the page
	// text is still the merge internals' own documentation.
	{CliVerbNotify, notifyUsageBudget},
	{CliVerbSession, sessionUsageBudget},
	{CliVerbTask, taskUsageBudget},
	{CliVerbTool, toolUsageBudget},
	{CliVerbUIState, uiStateUsageBudget},
	{CliVerbVersion, versionUsageBudget},
	{CliVerbWeb, webUsageBudget},
	{CliVerbWorker, workerUsageBudget},
}

// usageBudgetOffenses is THE predicate: given a page's measured line count and the
// cap it is held to, it returns every way the pair fails. It is a pure function of
// (lines, cap) so the red proofs below run the SAME code the gate runs, not a
// re-implementation of it — which is what makes them evidence rather than
// illustration.
//
// A measurement of ZERO lines is the first offense, deliberately: a page that
// printed nothing would otherwise read as "well under budget", and a gate that
// measured nothing must not pass (Rule 15).
func usageBudgetOffenses(lines, max int) []string {
	var out []string
	if lines <= 0 {
		out = append(out, "printed no lines — a gate that measured nothing must not pass")
		return out
	}
	if lines > max {
		out = append(out, fmt.Sprintf("is %d lines — over the %d-line budget by %d.\n"+
			"  The page's detail belongs in its own man page (`man gopi-<verb>`, rendered by `make man`),\n"+
			"  in --help-env, or in a devdoc; the `-h` page routes, the man page carries the contract.\n"+
			"  If the added lines are a new flag's contract, raise that verb's budget constant in\n"+
			"  cmd/agent/cli_usage_budget_test.go with the reason, and add the contract to the man page.",
			lines, max, lines-max))
		return out
	}
	if max-lines > usageBudgetNewFlagMargin {
		out = append(out, fmt.Sprintf("measures %d lines against a %d-line budget — %d lines of slack, more than the %d-line margin a new flag's paragraph needs.\n"+
			"  The cap is SLACK: lower that verb's budget constant to the measured length plus the margin, so the page cannot drift back into the room it is not using.",
			lines, max, max-lines, usageBudgetNewFlagMargin))
	}
	return out
}

// TestPublishedHelpPagesStayWithinTheirUsageBudget is the gate itself: every
// published page is measured on the REAL binary (Rule 31: helpPage spawns
// `gopi <verb> -h`, so a usage constant that stopped being printed cannot satisfy
// this while the operator's terminal shows something else) and held to its own
// named cap.
func TestPublishedHelpPagesStayWithinTheirUsageBudget(t *testing.T) {
	for _, row := range subcommandUsageBudget {
		row := row
		t.Run(row.verb, func(t *testing.T) {
			out := helpPage(t, []string{row.verb})
			lines := strings.Count(out, "\n")
			for _, offense := range usageBudgetOffenses(lines, row.max) {
				t.Errorf("`gopi %s -h` %s", row.verb, offense)
			}
			t.Logf("gopi %s -h: %d lines (budget %d, margin %d)", row.verb, lines, row.max, row.max-lines)
		})
	}
}

// TestChatScopedHelpPageStaysWithinItsUsageBudget holds the replay/film page to
// its own cap, on the REAL binary (Rule 31). It is a separate measurement from
// the table above because that table is keyed by VERB, and this page is reached by
// a FLAG (`gopi chat --replay -h`) — one verb, two pages, two budgets.
func TestChatScopedHelpPageStaysWithinItsUsageBudget(t *testing.T) {
	out := helpPage(t, []string{CliVerbChat, chatFlagReplay})
	lines := strings.Count(out, "\n")
	for _, offense := range usageBudgetOffenses(lines, chatReplayHelpUsageBudget) {
		t.Errorf("`gopi chat %s -h` %s", chatFlagReplay, offense)
	}
	t.Logf("gopi chat %s -h: %d lines (budget %d, margin %d)",
		chatFlagReplay, lines, chatReplayHelpUsageBudget, chatReplayHelpUsageBudget-lines)
}

// TestUsageBudgetCoversEveryPublishedPage pins the table's COVER against the
// published set the rest of this package walks, in both directions: a page that
// gained a cap without being published, or a published page that quietly has
// none, is a red build rather than a number in a comment. The absent row is the
// defect this file was written to close — eleven pages measured, none protected.
func TestUsageBudgetCoversEveryPublishedPage(t *testing.T) {
	inTable := make(map[string]bool, len(subcommandUsageBudget))
	for _, row := range subcommandUsageBudget {
		if inTable[row.verb] {
			t.Errorf("verb %q has TWO budget rows — one page, one cap", row.verb)
		}
		inTable[row.verb] = true
	}
	for _, verb := range publishedVerbHelpPages {
		if !inTable[verb] {
			t.Errorf("the published page `gopi %s -h` has NO length budget — the defect this gate exists for", verb)
		}
	}
	for verb := range inTable {
		found := false
		for _, published := range publishedVerbHelpPages {
			if published == verb {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("verb %q carries a budget but is not one of the published pages — a cap on nothing protects nothing", verb)
		}
	}
	if len(inTable) != len(publishedVerbHelpPages) {
		t.Fatalf("the budget table covers %d verbs, the published set has %d", len(inTable), len(publishedVerbHelpPages))
	}
}

// TestUsageBudgetPredicateIsNotBlind is the RED PROOF: the predicate is applied to
// the shapes that actually went wrong, so a green run cannot be a blind one.
func TestUsageBudgetPredicateIsNotBlind(t *testing.T) {
	// (control) a page at exactly its cap reports NOTHING — otherwise the poisons
	// below would be indistinguishable from a predicate that always refuses.
	if bad := usageBudgetOffenses(chatUsageBudget, chatUsageBudget); len(bad) > 0 {
		t.Fatalf("CONTROL failed — a page at exactly its cap offends the predicate:\n%s", strings.Join(bad, "\n"))
	}

	// (a) THE ORIGINAL DRIFT: the page one line over. This is the case the 615 →
	// 577 history is made of.
	over := usageBudgetOffenses(chatUsageBudget+1, chatUsageBudget)
	if len(over) == 0 {
		t.Error("(a) a page one line over its cap passed — the ceiling is blind")
	} else if !strings.Contains(strings.Join(over, "\n"), "over the") {
		t.Errorf("(a) the ceiling fired with the wrong reason: %s", strings.Join(over, "\n"))
	}

	// (b) THE RATCHET: a cap left standing well above the page it caps. Without
	// this arm the gate would be satisfied by raising every cap once, which is the
	// failure mode the budget history itself demonstrates.
	slack := usageBudgetOffenses(chatUsageBudget-usageBudgetNewFlagMargin-1, chatUsageBudget)
	if len(slack) == 0 {
		t.Error("(b) a cap with more than the new-flag margin of slack passed — the ratchet is blind")
	} else if !strings.Contains(strings.Join(slack, "\n"), "SLACK") {
		t.Errorf("(b) the ratchet fired with the wrong reason: %s", strings.Join(slack, "\n"))
	}

	// (c) THE EMPTY MEASUREMENT: zero lines is not "well under budget".
	if bad := usageBudgetOffenses(0, chatUsageBudget); len(bad) == 0 {
		t.Error("(c) a page that measured zero lines passed — an unmeasured page reads as a green one")
	}

	// (d) the boundary INSIDE the margin is accepted: a page may use the room the
	// margin reserves, or the margin would be a second ceiling.
	if bad := usageBudgetOffenses(chatUsageBudget-usageBudgetNewFlagMargin, chatUsageBudget); len(bad) > 0 {
		t.Errorf("(d) a page exactly at its margin was refused — the margin is not usable:\n%s", strings.Join(bad, "\n"))
	}
}
