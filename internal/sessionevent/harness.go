package sessionevent

// harness.go — THE MARKER OF A USER ROW THE AGENT HARNESS INJECTED, in the one
// place both sides of the session format can read it.
//
// WHY IT IS HERE AND NOT IN internal/agent. The marker belongs to the harness
// (internal/agent/context.go's harnessMessagePrefix opens every message the
// framework appends for the model: start/review checkpoints, job and truncation
// notices, the per-tool-call clock ticks). A READER of a session log needs the
// same marker — `gopi session log`'s round view and `gopi session replay` both
// have to tell "the operator said this" from "the framework said this" — and a
// reader CANNOT import internal/agent: that package imports this one, so the
// import is a cycle. The alternative, which is what this file replaces, is a
// second spelling of "[system] " in the reader — a mirror that drifts the moment
// the prefix changes, and a mirror is exactly what Rules 1b/13/14 forbid.
//
// So the VALUE lives here, in the leaf package both sides already share (zero
// project imports), and internal/agent's own constant is an ALIAS of it
// (harnessMessagePrefix = HarnessMessagePrefix), so the 18 append sites in that
// package keep reading their own name while the value has exactly one
// definition. internal/agent's harness_marker_shared_test.go fails the build if
// the two ever stop being the same value.
const HarnessMessagePrefix = "[system] "

// The THREE marked injections that do not open with HarnessMessagePrefix.
//
// THEY LIVE HERE, NOT IN internal/agent, and that is the whole point of this
// block (Rule 14). Each of them is a user row the FRAMEWORK appends for the
// model, and each predates the `[system] ` convention, so it keeps its own
// bracket marker. They were defined in internal/agent — which is where they are
// WRITTEN — and internal/agent's own predicate consulted them, while THIS
// package's IsHarnessInjected recognised the prefix alone. A reader and the
// replay therefore answered "whose row is this?" differently, and the price was
// measured on session 80c07a7d: 11 operator rows, 12 `[SELF-RECORD …]`
// re-injections, and three surfaces reporting 17 (`session log --turns`, which
// collapses the injected rows into spurious turn seams), 23 (`chat --replay`,
// which typed each injected row as if the person had), and 18 (`session list`,
// a different quantity entirely) for one recorded conversation.
//
// So the VALUES move to the leaf every side already imports (internal/agent
// aliases them, so its 18 append sites keep reading their own names — Rule 13:
// one definition, referenced, never mirrored), and every marker a harness
// injection can open with is consulted by the ONE predicate below.
const (
	// HarnessConsultCheckpointMarker heads the review-time memory-consult
	// checkpoint (internal/agent/loop.go writes it, its telemetry reader
	// latches on it).
	HarnessConsultCheckpointMarker = "[MEMORY-CONSULT CHECKPOINT]"
	// HarnessTaskAreaConclusionsMarker heads the authoritative committed-
	// conclusions carry-forward block injected after a write_task_area commit.
	HarnessTaskAreaConclusionsMarker = "[WRITE_TASK_AREA — COMMITTED CONCLUSIONS (authoritative)]"
	// HarnessCloneChannelBriefingMarker heads the CLONE CHANNEL's delivery-round
	// briefing (cmd/agent's deliveryRetryBriefing / deliveryNothingToDeliverBriefing
	// / deliveryExhaustedBriefing): the text the delivery loop hands the next
	// agent round on ITS STDIN as the round's first and only user input.
	//
	// IT IS AN INJECTION EVEN THOUGH THE TURN IS REAL. No person wrote it — the
	// harness composed it from a Blocker and the tool's own words — and the row
	// it becomes is recorded from the HUMAN face, because a stdin prompt has no
	// other provenance to be recorded with. Without a marker that row is
	// indistinguishable from an operator line, and it claims the head column of
	// the conversation: measured 2026-10-30 on session
	// 6a0ac1c6-4e6c-4155-8f5b-91d53a441663, whose ONE `session log --turns` turn
	// reads "交付没有落地：3 轮交付尝试全部被挡下…" — the framework's sentence about
	// a delivery that did not land, reported as the person's own ask.
	HarnessCloneChannelBriefingMarker = "[CLONE CHANNEL BRIEFING] "
	// HarnessSelfRecordMarker heads the RE-INJECTED self-recording note carrier:
	// the note block a compaction folded away, carried back verbatim.
	//
	// IT IS AN INJECTION EVEN THOUGH ITS TEXT IS THE RUN'S OWN RECORD. The notes
	// are the model's, but the MESSAGE is the harness's: no person typed it, the
	// surface never showed it as the person's line (measured on the captured
	// screen of 80c07a7d: the carrier's text appears nowhere on it), and a turn
	// count that includes it makes one conversation read as several.
	HarnessSelfRecordMarker = "[SELF-RECORD — your own notes, survived compaction]"
	// HarnessBriefingOnStdinMarker heads a briefing a PARENT process hands a
	// child agent on its stdin (`gopi task run`/`worker`, `gopi task verify`).
	//
	// WHY THE PRODUCER MARKS IT AND THE CHILD CANNOT. The child reads stdin as
	// its prompt, and the session record gives that row `source=human` because
	// the stdin seam has no other honest face. So the text is the only thing
	// that can say "the framework wrote this row".
	//
	// WHY IT IS A MARKER OF ITS OWN RATHER THAN `[EPIC GOAL]` ALONE (measured,
	// and it is the difference between a 2-producer fix and a corpus-wide
	// reclassification). A dispatched child records its briefing on one of TWO
	// faces: FRAMED (`[EPIC GOAL]\n` + the briefing, internal/agent's
	// epicGoalFramingFormat) when it is a plain `gopi` run, or VERBATIM when it
	// inherits a wrapper identity (`GOPI_TASK_WRAPPER_ID` ⇒ a.plainPrompt) —
	// measured on session 75ad9536-284b-4093-bca9-e73c505d3c19, whose first row
	// IS the verify briefing with no frame at all. A prefix on the text the
	// parent hands over is the ONE thing both faces carry, so the reader tests
	// for it (and, for the framed face, for the frame composed WITH it — see
	// hasBriefingMarker below). Declaring the plain frame an injection instead
	// would reclassify the 1118 briefings this profile's tree already holds,
	// which is a different decision with its own evidence.
	HarnessBriefingOnStdinMarker = "[gopi briefing] "
	// HarnessEpicGoalFrameMarker is the frame internal/agent puts around a
	// DISPATCHED run's briefing (epicGoalFramingFormat). It is a VALUE here,
	// not an entry in HarnessMessageMarkers, and the distinction is measured:
	// a plain `[EPIC GOAL]` row is NOT an injection on this corpus. This
	// profile's tree holds 1118 logs opening with it, and calling the bare
	// frame an injection reclassifies every one of them — a corpus-wide
	// re-measurement that is a decision of its own, not a rider on this one
	// (measured: it turns 10 internal/gopilog golden tests red).
	//
	// What IS recognised is the COMPOSED row: the frame wrapped around a
	// briefing the parent MARKED (hasBriefingMarker below). That row can only
	// exist because a producer went through BriefingOnStdin, so classifying it
	// reaches exactly the rows this change creates.
	//
	// THE VALUE MOVES HERE (Rule 14): internal/agent's epicGoalHeader,
	// internal/tools' taskEpicPrefix and internal/config's FrameMarkerEpic are
	// ALIASES of this one definition, so a re-worded frame cannot leave a
	// reader matching the old spelling.
	HarnessEpicGoalFrameMarker = "[EPIC GOAL]"
)

// HarnessMessageMarkers are the leading markers of harness-injected user
// messages that do NOT open with HarnessMessagePrefix, in one list (Rule 14:
// the ONE definition — internal/agent's anchor policy aliases this slice, so
// there is no second copy to drift).
var HarnessMessageMarkers = []string{
	HarnessConsultCheckpointMarker,
	HarnessTaskAreaConclusionsMarker,
	HarnessCloneChannelBriefingMarker,
	HarnessSelfRecordMarker,
	HarnessBriefingOnStdinMarker,
}

// BriefingOnStdin marks a briefing a parent process is about to hand a child
// agent on its stdin — THE ONE PLACE the two stdin briefing producers
// (`cmd/agent/cli_tasks.go`'s task run / worker child and
// `internal/tools/tasks.go`'s independent-verify child) go through.
//
// WHY THE PRODUCER MARKS IT AND THE CHILD CANNOT. The child reads stdin as its
// prompt, and the session record gives that row `source=human` because the
// stdin seam has no other honest face. Nothing downstream can then tell a
// briefing from something a person typed — except the text, which is what this
// marker is: `IsHarnessInjected` recognises it, so `session log --turns`,
// `session replay` and the chat surfaces stop counting the framework's
// briefing as the operator's turn.
//
// IDEMPOTENT BY CONTRACT: a producer that already marked its text (or passes
// one through twice) must not accumulate markers — the second call returns the
// input unchanged. The check is the SAME predicate a reader uses
// (hasHarnessPrefix), so "already marked" and "recognised as an injection"
// cannot disagree.
//
// IT WRITES AT THE HEAD, and the budget it costs is measured rather than
// assumed: the ledger's auto-summary folds a briefing's FIRST line into
// agent.AutoTaskSummaryBudget (200) runes, so every rune this marker spends
// pushes the decisive text of a stored row to the right. 16 runes is the
// measured price (the 54-rune candidate was rejected for the same reason, on a
// real row).
func BriefingOnStdin(body string) string {
	if body == "" {
		return ""
	}
	if hasHarnessPrefix(trimLeadingSpace(body)) {
		return body
	}
	return HarnessBriefingOnStdinMarker + body
}

// IsHarnessInjected reports whether a user row's text is a harness injection.
//
// LEADING WHITESPACE IS TRIMMED BEFORE THE COMPARISON, and that is measured
// rather than defensive: a harness message is assembled from parts, and a real
// row of this host's own session tree begins "\n\n[system] This run's briefing
// is large …". A prefix test against the raw text misses it, and a missed
// injection is rendered as if the operator had typed it.
//
// IT CONSULTS EVERY MARKER A HARNESS MESSAGE CAN CARRY (HarnessMessagePrefix
// plus HarnessMessageMarkers), because this function is the ONE predicate every
// session reader and the replay use — `session log --turns`, `session replay`,
// `chat --replay`'s recording loader, internal/testutil's Session views,
// internal/agent's anchor policy through its own alias. While it knew only the
// prefix it called 12 `[SELF-RECORD …]` re-injections the operator's own words.
func IsHarnessInjected(text string) bool {
	return hasHarnessPrefix(trimLeadingSpace(text))
}

// hasHarnessPrefix is the marker test, split out so the trimming above is the
// only thing that can differ between call sites.
func hasHarnessPrefix(text string) bool {
	if len(text) >= len(HarnessMessagePrefix) && text[:len(HarnessMessagePrefix)] == HarnessMessagePrefix {
		return true
	}
	for _, marker := range HarnessMessageMarkers {
		if len(text) >= len(marker) && text[:len(marker)] == marker {
			return true
		}
	}
	return hasFramedBriefing(text)
}

// hasFramedBriefing recognises the OTHER face of a marked briefing: the
// dispatched child's [EPIC GOAL] frame COMPOSED WITH a briefing the parent
// marked through BriefingOnStdin.
//
// WHY IT IS A COMPOSITION AND NOT A LIST ENTRY (`[EPIC GOAL]` alone is not an
// injection; measured). A dispatched child records its briefing on one of two
// faces — framed, or verbatim (a child that inherits the wrapper identity takes
// `a.plainPrompt` and gets no frame, measured on session
// 75ad9536-284b-4093-bca9-e73c505d3c19). The marker is what both faces carry;
// the frame is an extra layer ONE of them puts on top. Testing the composition
// therefore reaches every row this change creates and NOTHING ELSE, which is
// the difference between this and declaring the bare frame an injection: that
// version reclassifies the 1118 briefings this profile's tree already holds,
// and it turned ten internal/gopilog golden tests red when it was tried.
//
// The frame's own tail is not consulted: the row is the framework's frame
// around a marked briefing, and what follows the two markers is the briefing
// body, whatever it says.
func hasFramedBriefing(text string) bool {
	if !hasPrefix(text, HarnessEpicGoalFrameMarker) {
		return false
	}
	return hasPrefix(trimLeadingSpace(text[len(HarnessEpicGoalFrameMarker):]), HarnessBriefingOnStdinMarker)
}

// trimLeadingSpace drops the leading ASCII whitespace a message may carry
// ("\n\n[system] …"). It is deliberately narrow — space, tab and the two line
// endings — because those are the characters an assembled message is joined
// with; anything else is message text and must not shift the comparison.
func trimLeadingSpace(s string) string {
	i := 0
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}
		break
	}
	return s[i:]
}

// THE TURN'S MARK USED TO LIVE HERE, AS A COMPLEMENT — and that complement is
// the defect this revision removes. `IsHumanRoundBoundary(text)` answered "a
// turn opens wherever the text is NOT a harness marker", which gets one class of
// row wrong by construction: a `gopi notify` delivery carries the framework's
// own sentence, so it IS a harness row, and it is ALSO one complete input the
// harness opens a turn to report. Measured on session
// 4a0957a3-bb4e-4337-a761-20606c87d1fd (2026-10-27): 20 delivery notices and 50
// clock ticks, called identical by the old rule, produced a turn count that no
// two surfaces agreed on.
//
// THE PREDICATE IS now IsTurnOpeningRow(text, source) in turninput.go, next
// door. That file separates the two questions this one used to conflate:
// provenance + input shape decide where a TURN begins; the markers above decide
// only whether a row is the FRAMEWORK TALKING — a different question, and one
// that IS answerable from text alone. Read its header before changing either
// half.
