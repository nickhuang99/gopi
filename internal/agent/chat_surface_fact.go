package agent

// chat_surface_fact.go — the armed/unarmed FACT as a system note on the request.
//
// WHY IT EXISTS (the operator's ruling, 2026-10-11). The chat REPL used to state
// this run's own arm state as a `[gopi] …` prose line on the SCREEN — first in the
// answer band, then (after the 2026-10-03 ruling that tips belong on the startup
// screen) in the startup screen's own notices section, whose short copy is
// `screen dump → %s`. The screen is the OPERATOR's; the arm state is the MODEL's
// business, and the model already reads it in the tool's schema on the wire, in
// the tool's own refusal sentences, and — verbatim, on a successful `see` — in
// ChatScreenObservation. So the fact moves to the ONE carrier that was always
// meant for it: a note on the system prompt of the very request it describes.
// "屏幕上只留和人有关的东西."
//
// WHAT IT DOES NOT REPLACE. A screen dump that could not be ARMED, an identity
// that could not be PUBLISHED: those are anomalies the operator has to be able to
// act on, and they stay on the screen (Rule 15 — an absence nobody is told about
// is a silent failure). This file carries the ORDINARY case: the fact behind a
// capability the operator asked for and got.
//
// WHY THE NOTE IS ASSEMBLED, NOT FROZEN (Rule 2 in chat_context.go's own terms).
// The arm state is a property of the RUNNING PROCESS, and the path it names embeds
// this run's identity, so a package-level Text would freeze whatever an init-time
// evaluation saw. The fact is therefore published to the Agent at the ARM SEAM
// (cmd/agent's installChatSurface — the one place where both halves exist) and
// RENDERED here, per assembly.
//
// WHY internal/agent CANNOT DERIVE IT ITSELF. package main owns the surface and
// cannot be imported by this package (Rule 14/3: the direction is fixed), so the
// fact travels as a publish-and-render pair: cmd PUBLISHES what it knows
// (SetChatSurfaceFact), this package RENDERS it. No second guess, no mirror of
// the values — the two sentences below are the only spelling of the note, and the
// operator-facing constants in cmd/agent stay where they are (the startup screen's
// own `surface` fact row).

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/cliverbs"
)

// The note's two halves (Rule 1: every meaning-carrying string is a named
// constant; the head is separate so "is this fact present at all" is a one-
// substring question, independent of which arm the run is on).
const (
	// chatSurfaceFactMarker opens the note. It carries "\n\n" for the same reason
	// chatSessionBlockMarker does: this note is appended to an assembled prompt,
	// not to a section registry, so it has to draw its own paragraph break.
	chatSurfaceFactMarker = "\n\n[CHAT SURFACE TOOL — "

	// chatSurfaceFactArmedNote is the ORDINARY case: the run writes its own
	// screen. `%s` is the artifact path, which is the one operand the model needs
	// to quote the dump and the one it cannot derive.
	chatSurfaceFactArmedNote = "ARMED] this run writes its own screen: every frame it paints lands in %s, " +
		"and the `chat_surface` tool's `see`/`compare` actions read that artifact back as a CELL GRID " +
		"(attrs per cell-range — no pixels, no vision). " +
		"`type` enqueues your next turn; `key` injects a keystroke into this run's line editor. " +
		"These are the FACTS behind the tool whose def is on this wire."

	// chatSurfaceFactUnarmedNote is the DECLINE case — the run turned the dump off
	// on purpose (well, the operator did). It states the ABSENCE OF OBSERVATION in
	// words, never "the screen is blank": a reader that cannot tell "no observer"
	// from "nothing painted" will report the second when it has the first.
	chatSurfaceFactUnarmedNote = "NOT ARMED] this run declined its own screen dump (the dump is on by default; " +
		"this run passed `gopi chat " + cliverbs.NoDumpScreen + "` or a falsey " + cliverbs.ChatDumpEnv + "), " +
		"so the `chat_surface` tool's `see`/`compare` actions answer \"no observation\" — never a blank screen. " +
		"`type` and `key` work either way. " +
		"These are the FACTS behind the tool whose def is on this wire."
)

// ChatSurfaceFactMarker returns the note's opening, so a caller OUTSIDE this
// package can assert "this request carried the arm fact" against the marker the
// prompt itself draws, instead of re-typing the wording (Rule 13/14 — the same
// contract ChatSurfacePromptHead already has for the chat block; a re-worded head
// would leave the outside check passing against text nothing paints any more).
func ChatSurfaceFactMarker() string { return chatSurfaceFactMarker }

// chatSurfaceFact is this run's arm state as PUBLISHED by the process that owns
// the surface. The zero value means "nothing was published", and it renders as
// nothing: a run whose carrier installs no chat surface (a task run, `gopi web
// --tools`, a plain chat with no tools on the wire) must not be handed a note
// about a capability it does not have.
type chatSurfaceFact struct {
	// published latches that SetChatSurfaceFact ran. The zero value is the
	// ordinary case for every non-chat run.
	published bool
	// armed reports whether this run writes its own screen
	// (cmd/agent's chatSurfaceArmed: a nil dump sink is a declined dump).
	armed bool
	// path is the artifact the armed run writes to ("" when unarmed — there is no
	// artifact to name, and naming one would be a fact this run cannot support).
	path string
}

// SetChatSurfaceFact publishes this run's own arm state to the agent that will
// answer in it. Called by the process that owns the chat REPL, from the SAME seam
// that installs the surface (installChatSurface), because that is the one place
// where both halves of the fact exist: the resolved dump sink and the run's own
// session.
//
// toolsOn is part of the contract rather than a convenience: the note describes a
// TOOL, so it is published only where that tool's def is genuinely on the wire.
// The plain chat path (`Agent.Chat`) carries no tools at all — its own block says
// so in its own words (chatNoToolsLimb) — and a note there would be an instruction
// the agent cannot act on, which is worse than none.
func (a *Agent) SetChatSurfaceFact(armed bool, path string, toolsOn bool) {
	if a == nil || !toolsOn {
		return
	}
	a.chatSurfaceFact = chatSurfaceFact{published: true, armed: armed, path: path}
}

// ChatSurfaceFact renders this request's arm note, or "" when this run published
// none. It is the ONE spelling of the fact on the request side: both chat
// assemblies call it (assembleChatSystemPrompt and chatOpsSystemPrompt), so the
// two carriers cannot describe one run differently — the same rule the chat block
// follows for its per-run half.
func ChatSurfaceFact(a *Agent) string {
	if a == nil || !a.chatSurfaceFact.published {
		return ""
	}
	if a.chatSurfaceFact.armed {
		return chatSurfaceFactMarker + fmt.Sprintf(chatSurfaceFactArmedNote, a.chatSurfaceFact.path)
	}
	return chatSurfaceFactMarker + chatSurfaceFactUnarmedNote
}

// chatSurfaceFactRules is the assembly-side alias: both chat assemblies spell the
// call the way they spell their other per-run blocks, and the fact itself is
// rendered from the Agent they already hold.
func chatSurfaceFactRules(a *Agent) string { return ChatSurfaceFact(a) }
