package agent

// chat_surface.go — the CHAT-SCOPED tool: the surface's own eye and hand.
//
// WHY IT EXISTS. A tool-enabled chat turn already has the ordinary surface
// (read/write/bash/code_search/…) and, through bash, the CLI verbs that SENSE
// this machine (`gopi ui_state`, `gopi session log`, `gopi task`, `gopi tool capture`). What
// it did NOT have is the two capabilities that only make sense ON the surface it
// is answering in:
//
//	1. SEE   its own painted screen — the cell grid its answer band, editing
//	         line and status row occupy — and compare a line it believes it
//	         rendered against the authoritative transcript. Both existed as
//	         separate processes (`gopi chat` with `gopi ui_state --compare` in
//	         another terminal; since 2026-10-01 the chat run writes its screen
//	         dump BY DEFAULT, so the surface a turn is drawing into is readable
//	         without any flag at all — `--no-dump-screen` declines it); a turn
//	         could not ask from inside the window it is drawing into.
//	2. TYPE  its own next input. The chat REPL is "type a line, get a turn", so
//	         a turn that could enqueue a line could drive ITSELF one more step —
//	         a bounded self-driven loop, the seam this file's loop-bound half
//	         closes with an operator-visible notice.
//
// WHY IT IS NOT IN claudeToolDefs (Rule 3). That slice is the SSOT of the
// ordinary tool surface and two gates hold it there:
// TestGOPIRule3_MatchesClaudeToolDefs pins its length against PROJECT.md's Rule 3
// inventory and TestToolClassesCoverTheToolSurface demands a class for every
// entry. An entry there IS a new core tool. This one is therefore a SEPARATE
// definition (chatSurfaceToolDef below) appended only to the CHAT wire — see
// chatOpsToolDefs in chat_ops.go — so the task path's tool list is unchanged
// byte-for-byte (asserted by TestChatSurface_ToolDefOnlyOnTheChatWire).
//
// WHY THE DEF IS HERE AND THE BODY IS NOT. The screen this tool reads belongs to
// the PROCESS running the chat REPL, and that process is package main
// (cmd/agent); the agent package must not reach into it. So the def lives here —
// it is the model-facing contract, and it must ship on the same wire as the tool
// list — while the BODY is installed by whoever owns the surface (SetChatSurface,
// called from cmd/agent when it arms the dump). With no body installed every
// action returns chatSurfaceUnavailableNotice, which is a sentence, never a
// silent empty success: "I could not look" must not read as "nothing is there".

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/uiclient"
)

// The tool's action verbs — the wire enum of the schema AND the vocabulary the
// loop-control seam branches on (Rule 1/13: one constant set, never a typed-in
// string on either side).
const (
	// ChatSurfaceActionSee reads this surface's own screen as a cell grid.
	ChatSurfaceActionSee = "see"
	// ChatSurfaceActionCompare checks one line against the painted grid AND the
	// authoritative transcript, and says which side lost it.
	ChatSurfaceActionCompare = "compare"
	// ChatSurfaceActionType types text into this surface's own input line and
	// submits it: the turn drives the next turn. It is the ONLY action that
	// changes the surface, and its effect is bounded (see chatSurfaceLoopBound).
	ChatSurfaceActionType = "type"
	// ChatSurfaceActionKey injects RAW KEY BYTES into this surface's own line
	// editor — through the same decoder a keyboard goes through — and does NOT
	// submit anything. It is the interaction-level half of `type`: what a
	// keyboard can do to the editor's own state (open a menu, walk it, accept a
	// candidate, edit the line) rather than what a whole line can do to the
	// conversation (become the next turn).
	ChatSurfaceActionKey = "key"
	// ChatSurfaceActionStop cancels a self-driven loop, so the model or the
	// operator can end it from inside.
	ChatSurfaceActionStop = "stop"
)

// chatSurfaceActions is the action vocabulary in ONE place: the schema enum, the
// required-parameter check and the dispatch switch all read it, so adding a verb
// cannot leave one of the three behind.
var chatSurfaceActions = []string{
	ChatSurfaceActionSee, ChatSurfaceActionCompare,
	ChatSurfaceActionType, ChatSurfaceActionKey, ChatSurfaceActionStop,
}

// ChatSurfaceActions lists the tool's action verbs for callers outside this
// package (the required-parameter check in validateToolArgs, the usage page).
// The returned slice is a copy: the vocabulary is this file's to change.
func ChatSurfaceActions() []string {
	return append([]string{}, chatSurfaceActions...)
}

// chatSurfaceBounds are the tool's hard limits. They are DECLARED here and
// enforced at the points that can exceed them (Rule 1: a bound is a named
// constant, and a bound nothing reads is not a bound).
const (
	// chatSurfaceLoopBound caps how many turns one self-driven loop may add, at
	// the seam that accepts a pending line. It is what makes `type` safe to
	// expose at all: the loop terminates at a number the operator can see
	// (devdoc/runaway-loop-20260923.md is the incident that made a bound a
	// precondition for shipping a self-driving seam).
	//
	// IT IS THE DEFAULT, NOT THE CEILING ANY MORE (2026-10-30). The bound is a
	// CONFIGURED value: config.ChatSurfaceTurns reads the operator's declaration
	// through gopi's ordinary three channels (--chat-surface-turns > the knob's
	// GOPI_CHAT_SURFACE_TURNS env > settings.json "chat_surface_turns" > this
	// default), and chatSurfaceConfiguredBound is the ONE reader this package
	// uses. What did NOT change: the knob moves the CEILING only. A period is
	// still bounded, the model still cannot renew it, and the operator's own
	// line is still the only reset.
	//
	// WHAT IT BOUNDS: one RENEWABLE PERIOD, not the whole process. The period is
	// the stretch of conversation between two lines a HUMAN wrote, and the
	// operator's ruling on the question is what a period is
	// (task 01a0e44a): a submitted user line grants a FRESH budget, and the
	// model can never grant itself one — the reset is not reachable from this
	// tool, only from a line the operator actually typed. So the number still
	// terminates the loop (a period the operator does not re-enter ends at the
	// bound and never restarts itself, because nothing but a user line resets
	// the counter), and it no longer ends the SESSION: a long autonomous
	// verification is a sequence of periods the human opens, each one bounded
	// and each one ANNOUNCED on the console (see ChatSurfaceUserTurn).
	//
	// THE ABSOLUTE CEILING IS THIS (RESOLVED) VALUE, PER PERIOD — there is
	// deliberately no cross-period total (see the honest note in
	// devdoc/chat-input-self-drive-acceptance.md): an absolute session total would cap the
	// very run the ruling exists to allow, and the operator's own keystroke is
	// the only thing that can lengthen the run. The guard that survives is the
	// one the ruling names: NO MODEL SELF-RENEWAL — the counter is reset in
	// exactly one function, and no tool action reaches it.
	//
	// The value is the CONFIG package's constant, not a second copy of the
	// number (Rule 13/14): the default the resolution falls back to, the number
	// the row summary prints and the bound this package reports are one value.
	chatSurfaceLoopBound = config.ChatSurfaceTurnsDefault
	// chatSurfaceRestartFloor is the smallest max_turns a caller may ask for, so
	// "bound it" cannot be answered with a bound that fires before the first
	// self-driven turn.
	chatSurfaceRestartFloor = 2
	// chatSurfaceTypeRunesMax bounds one enqueued line. A line longer than this
	// is not a chat turn, and accepting it would let one call park an arbitrarily
	// large string in the input queue.
	chatSurfaceTypeRunesMax = 2000
	// chatSurfaceCompareSeed is how many transcript lines a compare returns even
	// when every one of them matched: a bare "match" sentence is unquotable, and
	// the caller needs the exact bytes it was checked against.
	chatSurfaceCompareSeed = 3
	// chatSurfaceKeyCeiling caps how many KEYS one session may inject through
	// action=key. It is a SECOND, separate bound from the self-driven-turn bound
	// because the two spend different things: a turn costs a model round trip,
	// while a key costs (almost) nothing — so a key bound borrowed from the turn
	// bound would be either uselessly small or a way to spend a session's whole
	// turn budget with arrow keys. It exists for the same reason the turn bound
	// does (devdoc/runaway-loop-20260923.md): a self-driving seam without a
	// ceiling is a loop nobody can stop, and its exhaustion is reported where
	// the operator reads (see chatSurfaceExhaustedNotice) rather than silently
	// dropping a key.
	chatSurfaceKeyCeiling = 64
	// chatSurfaceKeyFloor is the smallest ceiling a caller may ask for, so
	// "bound it" cannot be answered with a bound that fires before a single
	// interaction step (open, navigate, accept, submit).
	chatSurfaceKeyFloor = 4
	// chatSurfaceKeyBytesMax bounds ONE injected byte sequence. One injected key
	// is one read unit (the framing rule — see internal/lineedit/inject.go), and
	// a sequence long enough to be a paste would be read as one, which is
	// exactly what this action must not do.
	chatSurfaceKeyBytesMax = 64
	// ChatSurfaceSeeCharsMax is the BACKSTOP on what action=see puts into a
	// model's context, in characters.
	//
	// It is not the first line of defence and it is not a second budget: the
	// scrollback bound (lineedit.DumpScrollbackSeeRows) is what keeps an ordinary
	// `see` in the tens of kilobytes, and this bound exists because the tool
	// return is assembled from a surface it does not own — a foreign run's
	// artifact, a future section added to the rendering, a pathological grid. A
	// chat turn that returned 2,783,126 characters for one glance (measured
	// 2026-11-06) was not a rendering bug alone: NOTHING between the dumping sink
	// and the request body could say "that is too much". This is the place that
	// can, and it is deliberately generous — a full 200-row scrollback plus grid,
	// attrs and notices fits inside it many times over, so a truncation here means
	// the surface itself is out of shape, not that a normal screen was cut.
	ChatSurfaceSeeCharsMax = 262144
)

// chatSurfaceSeeTruncatedNotice builds the sentence appended when the backstop
// fires. It states WHERE the answer was cut and HOW to get the rest, because a
// bare ellipsis in a tool result is indistinguishable from a screen that simply
// ends there.
func chatSurfaceSeeTruncatedNotice(shown, total int, path string) string {
	return fmt.Sprintf("\n[%s %s truncated: the rendering was %d characters, above this tool's %d-character return budget, so the last %d are NOT here — the cut lands on a line boundary and the artifact is complete at %s (`%s` through %s prints it whole)]\n",
		toolnames.ToolChatSurface, ChatSurfaceActionSee, total, ChatSurfaceSeeCharsMax, total-shown, path, cliverbs.UIState, toolnames.ToolBash)
}

// chatSurfaceSeeBound is the backstop's ONE implementation: it returns body
// unchanged when it fits, and otherwise cuts it at the last line boundary at or
// below ChatSurfaceSeeCharsMax and appends the notice.
//
// A LINE BOUNDARY, never a byte offset: the rendering is UTF-8 and a cut inside
// a rune would put a broken character into the model's prompt, and a cut inside a
// row would present half a screen row as a full one. When no boundary exists
// (one pathologically long line) the cut falls back to the last rune boundary, so
// the answer is always valid UTF-8 — the notice still names the real total.
func chatSurfaceSeeBound(body, path string) string {
	if len(body) <= ChatSurfaceSeeCharsMax {
		return body
	}
	cut := ChatSurfaceSeeCharsMax
	if nl := strings.LastIndexByte(body[:cut], '\n'); nl > 0 {
		cut = nl
	} else {
		for cut > 0 && !utf8.RuneStart(body[cut]) {
			cut--
		}
	}
	return body[:cut] + chatSurfaceSeeTruncatedNotice(cut, len(body), path)
}

// core

// The messages this tool returns. Every failure is a SENTENCE that names what
// was missing and which other action gets it (a refusal is an answer, and the
// remedy belongs in it).
const (
	chatSurfaceUnavailableNotice = toolnames.ToolChatSurface + ": no chat surface is installed on this run — this tool reads the screen of an interactive `" +
		cliverbs.Chat + "` session, and it is only advertised on that path. Use `" + cliverbs.UIState + "` through " +
		toolnames.ToolBash + " to read a `" + cliverbs.UI + "` surface, or " + toolnames.ToolScreenCapture + " for the live desktop."
	chatSurfaceNotArmedNotice = toolnames.ToolChatSurface + ": this chat run's own screen dump is OFF, so its screen is not being written anywhere: there is nothing to read. The dump is ON BY DEFAULT; this run declined it (`" +
		cliverbs.Chat + " " + cliverbs.NoDumpScreen + "`, or a falsey `" + cliverbs.ChatDumpEnv + "`). Start the chat without the off switch and its screen is readable again — the switch changes nothing else about the run. With no artifact the surface paints nothing observable, so num_frames stays 0 and this answer is 'no observation', never 'a blank screen'."
	chatSurfaceNoDumpNotice = toolnames.ToolChatSurface + ": the run is armed but its own screen artifact is not readable yet (the sink installs its first snapshot as the layout paints). Call this again in a moment, or " +
		toolnames.ToolBash + " `" + cliverbs.UIState + " --target chat` to see what discovery finds."
	chatSurfaceTypeNeedsText    = toolnames.ToolChatSurface + ": action=" + ChatSurfaceActionType + " needs text — the line to type into this conversation (it is sent as your NEXT turn when this one ends)."
	chatSurfaceCompareNeedsLine = toolnames.ToolChatSurface + ": action=" + ChatSurfaceActionCompare + " needs text — copy the line AS YOU WROTE IT, without the answer band's own gutter (the marker column and any wrapping)."
	chatSurfaceStopNothing      = toolnames.ToolChatSurface + ": no self-driven loop is pending or running — nothing to stop."
)

// chatSurfaceKeyNeedsKey is the missing/failed-operand sentence for action=key.
// It is a VAR rather than a constant for the reason the rule it states needs
// naming: the legal set is READ FROM the package that owns it
// (lineedit.InjectableKeyNames), so the sentence cannot drift from the table it
// promises — and a set that has to be spelled by hand is a set that goes stale
// the moment a key is added (Rule 13/14).
var chatSurfaceKeyNeedsKey = toolnames.ToolChatSurface + ": action=" + ChatSurfaceActionKey + " needs text — the RAW bytes of ONE key (a rune, or an escape sequence); pass a named key (" + strings.Join(chatSurfaceKeyNames(), "/") + ") or the literal bytes of one. A KEY SPELLING THE VOCABULARY DOES NOT NAME (another member of the ctrl family such as ctrl-u, a named key in the wrong case, or an escape spelling of a byte such as \\u0015 or \\x15) is REFUSED, never typed into the line as its own letters — pass the byte itself when that is what you mean."

// chatSurfaceExhaustedNotice is the sentence printed on the OPERATOR's console
// when the key ceiling is reached. It is LOUD and it is printed HERE (once, on
// the call that spent the last key) rather than left to the model's summary: the
// bound is the operator's guarantee, and a guarantee that only the model can see
// is not one (devdoc/runaway-loop-20260923.md is the incident that made a bound
// a precondition for shipping a self-driving seam — the console notice is what
// closes the "silent" half of it).
const chatSurfaceExhaustedNotice = "[gopi] chat_surface action=" + ChatSurfaceActionKey + " has reached its ceiling for this period (%d keys injected, bound %d): no key will be injected until a fresh period opens — submit a line of your own and the next key budget is yours. The conversation continues normally in the meantime.\n"

// chatSurfaceToolDef BUILDS the tool's model-facing definition, in Claude
// Messages API format like every entry of claudeToolDefs. It is NOT in that
// slice (see the file header): chatOpsToolDefs appends it to the CHAT wire
// alone.
//
// It is a FUNCTION, not a package var, because the description and the
// max_turns schema both NAME the configured ceiling (Rule 11: the description
// is the persuasion surface). A var would freeze whatever the bound was at
// package init, so a run started with `--chat-surface-turns 40` would tell the
// model its budget is 20 — a description that lies about the number the seam
// enforces.
//
// chatSurfaceToolDefFor returns the def a GIVEN run is sent: the interactive
// description plus, for a run that declared itself unattended, the one clause
// that redirects the bound's remedy (see the tail below). Two shapes rather
// than a second tool (Rule 3): the description is the tool's ONLY persuasion
// mechanism (Rule 11), and an unattended run must not be told its exit is an
// operator who is not there.
func chatSurfaceToolDefFor(a *Agent) deepseek.ClaudeToolDef {
	def := chatSurfaceToolDef()
	if a != nil && a.Unattended {
		def.Description += chatSurfaceUnattendedRouteClause
	}
	return def
}

// chatSurfaceUnattendedRouteClause is the UNATTENDED run's addendum to the tool
// description: the autonomous exit, stated where the model reads the action's
// contract. It is a BRANCH, not a replacement — an interactive run gets the
// original description untouched, so the console's own documented behaviour is
// unchanged (the acceptance's "交互语境下原文保留").
//
// It names both real routes and invents neither: an ordinary turn (the answer is
// itself a turn, i.e. the run's own line source carries it forward) and, while
// the period still has room, action=type queuing the next turn. It repeats that
// the bound does not move, because a model that believes it can renew itself
// would waste the turns it has trying.
const chatSurfaceUnattendedRouteClause = " THIS RUN IS UNATTENDED (no operator is reading it): at the bound, do NOT ask the operator to type anything — nobody will. Continue with an ordinary turn and deliver; choose a convention yourself wherever the task left a parameter open, and state in one clause which one you chose. " +
	ChatSurfaceActionType + " queues your OWN next turn ONLY on a run that reads a line editor; a piped, one-shot or --no-editor run has NO next-turn seam and refuses it, so there your answer is the only exit — say in it what you would do next. The bound itself never moves and you cannot renew it."

// chatSurfaceToolDef is the interactive description (unchanged by this file's
// unattended branch: chatSurfaceToolDefFor is what the wire reads).
func chatSurfaceToolDef() deepseek.ClaudeToolDef {
	// bound is the CONFIGURED ceiling, read here rather than baked into a
	// package var: the description is the tool's only persuasion surface
	// (Rule 11), and a number frozen at init would tell a model on a run with
	// --chat-surface-turns=40 that its budget is 20.
	bound := chatSurfaceConfiguredBound()
	return deepseek.ClaudeToolDef{
		Name: toolnames.ToolChatSurface,
		// The first sentence is the routing decision (Rule 11): the model must reach
		// for this tool, not for an `ui_state` shell call, when the screen in
		// question is the one it is answering in.
		Description: "Call " + toolnames.ToolChatSurface + " when the screen in question is THE ONE YOU ARE ANSWERING IN — your own chat surface. " +
			"action=" + ChatSurfaceActionSee + " returns this surface's painted screen as an interpreted CELL GRID (no pixels, no vision model): the answer band, the editing line, the status row. " +
			"action=" + ChatSurfaceActionCompare + " checks one line against BOTH the painted grid and the authoritative transcript and tells you which SIDE lost it — that is the one call that separates 'the paint truncated it' from 'it was never said'. " +
			"action=" + ChatSurfaceActionType + " types a LINE into your own input line and submits it, so the turn that produced it is followed by an input **that this turn wrote** — the self-driven loop. It is BOUNDED: at most " +
			fmt.Sprintf("%d", bound) + " self-driven turns per PERIOD, after which the loop refuses and says so on the console the operator is watching; a period is the stretch between two lines a HUMAN wrote, and every line the operator submits grants a fresh budget — you can never renew it yourself, and it does not raise itself (pass max_turns (2.." +
			fmt.Sprintf("%d", bound) + ") to bound a period sooner). Use it only when the task genuinely needs the next step typed for you; it spends a whole turn. " +
			"action=" + ChatSurfaceActionKey + " injects RAW KEY BYTES into your own line editor — through the SAME decoder a real keyboard goes through — and submits NOTHING (bounded separately: " +
			fmt.Sprintf("%d", chatSurfaceKeyCeiling) + " keys per PERIOD, and a period is the same stretch between two lines a HUMAN wrote that bounds " + ChatSurfaceActionType + " — an operator line opens a fresh key budget too, and you can never renew it yourself). CHOOSE BETWEEN " + ChatSurfaceActionType + " AND " + ChatSurfaceActionKey + " BY WHAT YOU ARE TESTING: " +
			ChatSurfaceActionKey + " is for driving the EDITOR's own state — opener a completion menu, walking it with down/up, accepting with tab or enter, editing the line with backspace/left/right — i.e. every interaction that a whole-line submission cannot reach, because " +
			ChatSurfaceActionType + " never enters the editor at all: it enqueues a finished line as the next turn, so a line and a key can end up EXECUTING (a submitted `/quit` with a following Down runs the command instead of opening its menu). " +
			ChatSurfaceActionType + " is for moving the CONVERSATION one step; " + ChatSurfaceActionKey + " is for exercising the INPUT PATH. " +
			"action=" + ChatSurfaceActionStop + " cancels a pending or running self-driven loop. " +
			"Reading is possible while this run still writes its own screen dump — which it does BY DEFAULT; a run started with `" + cliverbs.Chat + " " + cliverbs.NoDumpScreen + "` (or a falsey `" + cliverbs.ChatDumpEnv + "`) has declined it, and then every read answers 'no observation', never 'blank screen'. " +
			"NOT for another gopi surface — a `" + cliverbs.UI + "` window or a second chat is read by " + cliverbs.UIState + " through " + toolnames.ToolBash + " — and NOT for a file, a page or the desktop (read / web_fetch / " + toolnames.ToolScreenCapture + ").",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": chatSurfaceActions,
					"description": ChatSurfaceActionSee + " = read this surface's own painted screen as a cell grid; " +
						ChatSurfaceActionCompare + " = check one line against the grid AND the transcript; " +
						ChatSurfaceActionType + " = type a whole line into this conversation as the NEXT turn (bounded self-driven loop; never enters the line editor); " +
						ChatSurfaceActionKey + " = inject ONE key's raw bytes into your own line editor (bounded; submits nothing); " +
						ChatSurfaceActionStop + " = cancel a pending/running self-driven loop.",
				},
				"text": map[string]interface{}{
					"type": "string",
					"description": "action=" + ChatSurfaceActionCompare + ": the line to check, exactly as you wrote it and WITHOUT the band's gutter. action=" +
						ChatSurfaceActionType + ": the line to type into this conversation (it is submitted as your next turn when this one ends). action=" +
						ChatSurfaceActionKey + ": the RAW bytes of one key — a named key (" + strings.Join(chatSurfaceKeyNames(), "/") + ") or literal bytes, e.g. \"down\", \"enter\", \"/\". A NAME is resolved to that key's own wire bytes; anything else is injected exactly as written, so a rune is injected by passing it. NOT a KEY SPELLING THE VOCABULARY DOES NOT NAME: a value that spells a key (another member of the ctrl family, a name in the wrong case, or an ESCAPE spelling of a byte like \\u0015 or \\x15) is REFUSED and the refusal names the legal set — injecting it as text would edit the operator's line while reporting success. Pass the byte itself when that is what you mean.",
				},
				"session": map[string]interface{}{
					"type": "string",
					"description": "action=" + ChatSurfaceActionCompare + ": the authoritative transcript — a session id, a *.jsonl path, or the `" +
						cliverbs.SessionFlag + "` value. Empty on a run started by this process = this run's own session.",
				},
				"max_turns": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("action=%s: bound the self-driven loop to this many turns (%d..%d). Default %d. Ignored by other actions.", ChatSurfaceActionType, chatSurfaceRestartFloor, bound, bound),
					"default":     bound,
				},
				"max_keys": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("action=%s: bound how many keys this session may inject (%d..%d). Default %d. Ignored by other actions.", ChatSurfaceActionKey, chatSurfaceKeyFloor, chatSurfaceKeyCeiling, chatSurfaceKeyCeiling),
					"default":     chatSurfaceKeyCeiling,
				},
			},
			"required": []string{"action"},
		},
	}
}

// chatSurfaceActionValues is the string form of the enum, read by the
// required-parameter check in validateToolArgs — so a call with an unknown verb
// is refused with the valid list instead of reaching the dispatcher.
func chatSurfaceActionValues() string { return strings.Join(chatSurfaceActions, "|") }

// chatSurfaceKeyNames is the injectable key vocabulary, read from the ONE package
// that owns it (internal/lineedit defines both the names and the raw bytes, and
// asserts every entry against the decoder). It is a function rather than a copied
// slice so the model's schema, the refusal sentences and the bytes on the wire
// cannot drift apart (Rule 14: one definition, consumed — never mirrored).
func chatSurfaceKeyNames() []string { return lineedit.InjectableKeyNames() }

// ChatSurfaceKeyCeiling is this tool's declared key ceiling, exported so the
// process that builds the line editor installs the SAME number the tool enforces
// (cmd/agent's editor wiring). One definition, two consumers: the injector's own
// ceiling is a second line of defence behind the tool's, and it is only a
// defence if both name the same bound (Rule 13/14).
// ChatSurfaceKeyCeiling is the DECLARED key ceiling: the largest number of keys
// one period may inject into this run's line editor (a caller's `max_keys` operand
// is clamped into `ChatSurfaceKeyFloor()..ChatSurfaceKeyCeiling()`).
func ChatSurfaceKeyCeiling() int { return chatSurfaceKeyCeiling }

// ChatSurfaceKeyFloor is the SMALLEST period ceiling a caller may ask for — the
// same number `max_keys` is clamped up to, exported so a caller (a test, a
// probe) can name the tightest legal period instead of hard-coding 4.
func ChatSurfaceKeyFloor() int { return chatSurfaceKeyFloor }

// chatSurfaceKeyCeilingArgs clamps a caller's max_keys into the declared range.
// The direction of the clamp is the same as max_turns': below the floor is
// RAISED to it (asking for fewer than one interaction step is a refusal, not a
// bound) and above the ceiling is clamped DOWN, never up — the declared ceiling is
// the engine's, not the model's.
func chatSurfaceKeyCeilingArgs(args map[string]interface{}) int {
	v, ok := args["max_keys"].(float64)
	if !ok || int(v) <= 0 {
		return chatSurfaceKeyCeiling
	}
	n := int(v)
	if n < chatSurfaceKeyFloor {
		return chatSurfaceKeyFloor
	}
	if n > chatSurfaceKeyCeiling {
		return chatSurfaceKeyCeiling
	}
	return n
}

// validateChatSurfaceArgs checks the action-conditional requirements of one
// call, in the SAME place and shape as the merged families' checks (loop.go's
// validateToolArgs) — a missing operand comes back as the sentence that names
// it, so the model self-corrects instead of watching a silent empty run.
//
// It returns the message, or "" when the call may proceed.
func validateChatSurfaceArgs(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	text, _ := args["text"].(string)
	switch strings.TrimSpace(action) {
	case ChatSurfaceActionSee, ChatSurfaceActionStop:
		// Neither takes an operand: the surface is the target.
		return ""
	case ChatSurfaceActionCompare:
		if strings.TrimSpace(text) == "" {
			return chatSurfaceCompareNeedsLine
		}
		return ""
	case ChatSurfaceActionType:
		if strings.TrimSpace(text) == "" {
			return chatSurfaceTypeNeedsText
		}
		return ""
	case ChatSurfaceActionKey:
		if strings.TrimSpace(text) == "" {
			return chatSurfaceKeyNeedsKey
		}
		if len(text) > chatSurfaceKeyBytesMax {
			return fmt.Sprintf("%s action=%s refused: the byte sequence is %d bytes, beyond one key's bound of %d — one injected key is one read unit, so a longer sequence would be read as a paste.",
				toolnames.ToolChatSurface, ChatSurfaceActionKey, len(text), chatSurfaceKeyBytesMax)
		}
		return ""
	default:
		return fmt.Sprintf("Missing or invalid parameter: action (%s), got %q", chatSurfaceActionValues(), action)
	}
}

// ChatSurface is the surface half of the tool: the ONE method set the process
// that owns the chat REPL implements, so this package can carry the tool's
// contract without knowing what a terminal is.
//
// A nil ChatSurface (the default) is the "this path has no surface" case, and
// every action then answers chatSurfaceUnavailableNotice.
type ChatSurface interface {
	// ScreenGrid reports this surface's own painted screen as the interpreted
	// cell grid (the artifact text), with the facts a reader must not have to
	// reconstruct: whether an artifact exists at all, its path, its age, and the
	// sink's frame/dump counts. armed=false means the run never asked for a dump
	// — an absence of OBSERVATION, which must never be presented as a screen.
	ScreenGrid() ChatScreenObservation
	// Transcript returns the session record's path and its assistant text
	// blocks, one entry per line, AS THE LOG HOLDS THEM (the same bytes the
	// authoritative reader prints). An empty session argument means "this run's
	// own session"; a caller-supplied id or path is resolved by the
	// implementation, which owns the profile's lookup.
	Transcript(session string) (path string, lines []string, err error)
	// Enqueue types text into this surface's own input line and submits it; the
	// line becomes the carrier's NEXT turn when the current one ends. bound is
	// the caller's ceiling on self-driven turns this session (already clamped to
	// the declared range by the caller). It reports the bound actually in force
	// and refuses when the ceiling is reached or no line source can carry one.
	Enqueue(text string, bound int) (accepted bool, inForce int, why string)
	// InjectKey queues ONE key's raw bytes into this surface's own LINE EDITOR —
	// the same input path a keyboard feeds, through the same decoder — and
	// submits nothing. key is either a named key (internal/lineedit's vocabulary:
	// up/down/enter/tab/backspace/…) or literal bytes (a rune, or an escape
	// sequence); the implementation resolves it. bound is the PERIOD's ceiling on
	// keys (already clamped by the caller, and lowerable by the implementation —
	// passing a larger number must never raise a ceiling); the implementation
	// owns the counter's state because only it knows whether this process has a
	// line editor at all. It reports what happened as FACTS: whether the bytes
	// were queued, whether the read loop had consumed them by the time this
	// answered, how many keys the period has now spent, and the sentence when it
	// refused.
	InjectKey(key string, bound int) ChatKeyResult
	// ResetKeys opens a FRESH key period: the keys spent so far go back to zero
	// and the period's ceiling is re-established. The carrier calls it at the
	// SAME boundary ChatSurfaceUserTurn marks (a line the model did not write),
	// and it is deliberately NOT reachable from any tool action — a bound the
	// bounded party can lift is not a bound. A surface with no line editor is a
	// no-op, never an error.
	ResetKeys(bound int)
	// CancelLoop cancels a pending or running self-driven loop and reports
	// whether there was one.
	CancelLoop() bool
}

// ChatScreenObservation is what a surface can say about its own screen. It is a
// struct rather than a formatted string so the TOOL decides what the model sees
// (one rendering, one place) while the surface only reports facts.
type ChatScreenObservation struct {
	// Armed reports whether this run writes its own screen at all.
	Armed bool
	// Found reports whether that artifact was readable.
	Found bool
	// Path is the artifact ("" when none).
	Path string
	// Grid is the interpreted cell grid's text.
	Grid string
	// Cols / Rows / Frames / Dumps are the artifact's own header facts.
	Cols, Rows, Frames, Dumps int
	// Geometry names the authority behind the grid's size ("layout", "terminal",
	// "fallback") — read by the reader, never invented here.
	Geometry string
	// State is the identity verdict (live / exited / …).
	State string
	// Qualification carries the reader's own warning when the grid may NOT be
	// read as a screen (unpainted, all-blank, fallback geometry). Empty means the
	// grid is readable.
	Qualification string
	// AgeSeconds is how long ago the artifact was written, so "stale frame" is
	// visible rather than assumed.
	AgeSeconds float64
}

// chatSurfaceLoop is the self-driven loop's live state, owned by the Agent// because the Agent is the one object a chat session really has: the REPL, the
// session log and the model all hang off it, and the seam that ACCEPTS a pending
// line (the REPL) must consult the SAME counter the tool increments — two
// counters would let the bound be enforced against a number nobody spent.
type chatSurfaceLoop struct {
	// session is the surface, installed by the process that owns the REPL.
	surface ChatSurface
	// pending is the line the model typed, waiting for the carrier to make it
	// the next turn. ONE slot: a second `type` while one is pending replaces it
	// and says so — a queue would grow a backlog the bound does not measure.
	pending string
	// typed counts how many self-driven turns the CURRENT period has added —
	// "period" being the stretch between two lines a human wrote. It is reset
	// by ChatSurfaceUserTurn and by NOTHING else: no tool action reaches it, so
	// the model cannot renew its own budget (the operator's ruling on the
	// question, task 01a0e44a).
	typed int
	// bound is the ceiling in force for this session (0 = the declared default).
	bound int
	// keys is the KEY ceiling in force for this session (0 = the declared
	// default) — the action=key half of the bound, set at the first key and
	// stable afterwards. It is separate from bound because the two spend
	// different things (a turn = a model round trip, a key = nothing), so one
	// counter for both would be a bound against the wrong quantity.
	//
	// IT IS THE PERIOD'S CEILING, not the session's, and that is the whole of
	// the reset contract: `keys` is re-established by ChatSurfaceUserTurn (the
	// carrier's "a line the model did not write is in hand") exactly as `typed`
	// is reset there, so a period the OPERATOR opens gets a fresh key budget.
	// The counter it bounds (the injector's `used`) is reset on the same call,
	// because there is one counter per ceiling and two owners of one number
	// would let the tool's report and the injector's refusal disagree.
	// `sessionKeys` keeps the session's own value so a period can never RAISE
	// the ceiling by renewing (see ChatSurfaceUserTurn).
	keys int
	// sessionKeys is the ceiling the FIRST key of the session established, kept
	// so a renewal restores it instead of inheriting a period's tighter value.
	sessionKeys int
	// stopped latches a `stop` request so a loop that is already parked at the
	// bound cannot be extended by a later `type`.
	stopped bool
}

// SetChatSurface installs the surface half of the chat-scoped tool. Called by
// the process that owns the chat REPL; passing nil removes it (and every action
// then answers chatSurfaceUnavailableNotice).
func (a *Agent) SetChatSurface(s ChatSurface) { a.chatSurface.surface = s }

// ChatKeyResult is what ONE action=key injection reports: FACTS, so the tool
// decides the sentence the model reads while the surface decides what the
// operator sees (the same division ChatScreenObservation makes).
type ChatKeyResult struct {
	// Label is the key as it was asked for (a name, or the literal bytes).
	Label string
	// Accepted reports that the bytes were queued on the editor's input path.
	Accepted bool
	// ReadByEditor reports that the read loop had consumed the key by the time
	// the call answered. False is NOT a failure: the turn that injects a key IS
	// the turn in flight, so the editor may not be reading yet — it takes the
	// key at the next prompt. A caller that needs the key acted on must read the
	// grid AFTER it, never assume this.
	ReadByEditor bool
	// Pending is how many injected bytes are still unread.
	Pending int
	// Used is how many keys the CURRENT PERIOD has injected, including this one.
	// "Period" is the stretch between two lines a human wrote (the same period
	// the self-driven turn bound is measured against, see ChatSurfaceUserTurn);
	// it is NOT a session total.
	Used int
	// Bound is the ceiling in force for this period.
	Bound int
	// Last reports that this key spent the last of the ceiling: the caller must
	// announce the exhaustion ON THIS CALL, never one key later.
	Last bool
	// Why carries the sentence when Accepted is false.
	Why string
}

// ChatSurfaceTurn is what the seam reports at a turn boundary: the line to run
// (when there is one) and the event the CARRIER must put on the operator's
// console. The wording lives in the carrier (it owns the console); this package
// reports the fact and the two counters.
type ChatSurfaceTurn struct {
	// Line is the self-driven turn to run, "" when the carrier reads its own
	// input.
	Line string
	// Kind is the event: one of the ChatSurfaceSeam* values.
	Kind string
	// Used and Bound are the counters in force, so the carrier's sentence carries
	// the real numbers rather than a value it passed in.
	Used, Bound int
	// Closing says this turn is the LAST the loop will ever add: the same line
	// announces both the turn and the loop's imminent end, so the operator never
	// has to notice the silence after it.
	Closing bool
	// Source names the FACE this line came from (internal/inputsource): the
	// `chat_surface` tool's `type` action is the model typing for itself, so it is
	// always inputsource.LLM here. It is carried on the value rather than assumed
	// by the carrier because the carrier's other arms produce the operator's own
	// lines (typed, /queue'd, notified), and a downstream spill must be able to
	// say which one it is holding.
	Source string
}

// The seam's event vocabulary (Rule 1: the carrier branches on these, never on a
// typed-in string).
const (
	// ChatSurfaceSeamNone — nothing pending: the carrier reads its own input.
	ChatSurfaceSeamNone = "none"
	// ChatSurfaceSeamTyped — a self-driven turn, charged against the bound.
	ChatSurfaceSeamTyped = "typed"
	// ChatSurfaceSeamClosed — the loop is over: either a `stop` cancelled a line
	// that was already typed (it is DROPPED here), or the bound has just been
	// reached. The carrier prints the termination notice, ONCE — a self-driving
	// seam whose stop nobody can see is the incident
	// devdoc/runaway-loop-20260923.md records.
	ChatSurfaceSeamClosed = "closed"
)

// ChatSurfaceTakePending consumes the line a `type` action enqueued and reports
// what the carrier must do at this turn boundary — the ONE seam the bound is
// enforced at and the ONE place a typed line is ever dropped.
//
// WHY ONE SEAM. A bound enforced in two places is a bound that can be enforced
// against a number nobody spent, and a line dropped in two places is a line
// whose drop nobody owns. So this function owns both decisions, and the carrier
// owns only what only it can do: read its own input, and say out loud what
// happened.
//
// EVERY EVENT IS ANNOUNCED ONCE, AND THE LAST TURN CARRIES ITS OWN ENDING. The
// turn that reaches the bound comes back with Closing=true, so the operator reads
// the turn AND the loop's end on one line; the CLOSED event is reserved for a
// line that was typed and then LOST (a `stop`, or a counter already spent). A
// notice that fires again every turn would be noise, and noise is how a real stop
// goes unread.
func (a *Agent) ChatSurfaceTakePending() ChatSurfaceTurn {
	l := &a.chatSurface
	bound := a.chatSurfaceLoopBound()
	line := l.pending
	if strings.TrimSpace(line) == "" {
		// Nothing pending: the carrier reads its own input, and there is nothing to
		// announce. The end of the BOUND is reported by the Closing flag on the last
		// turn the seam handed out, and a `stop` with nothing in hand is a NO-OP —
		// it cancelled nothing, so the tool answers the model "nothing to stop" and
		// the console has nothing to say. (That is also why an idle stop does not
		// latch: a session whose loop was never started must stay startable.)
		l.pending = ""
		return ChatSurfaceTurn{Kind: ChatSurfaceSeamNone, Used: l.typed, Bound: bound}
	}
	if l.stopped || l.typed >= bound {
		// Two ways a typed line dies here, and both are VISIBLE:
		//   - a `stop` cancelled it after it was typed (the tool refused to
		//     continue, but this line was already in hand);
		//   - the counter is spent. The tool refuses a `type` past the bound, so
		//     this arm is unreachable THROUGH the tool — but the seam is also the
		//     API a future carrier calls, and a bound walkable because one caller
		//     forgot to check is not a bound.
		// A line that silently disappears between being typed and becoming a turn
		// is the failure this seam exists to make impossible.
		l.pending = ""
		return ChatSurfaceTurn{Kind: ChatSurfaceSeamClosed, Used: l.typed, Bound: bound}
	}
	l.pending = ""
	l.typed++
	closing := l.typed >= bound
	return ChatSurfaceTurn{Line: line, Kind: ChatSurfaceSeamTyped, Used: l.typed, Bound: bound, Closing: closing, Source: inputsource.LLM}
}

// ChatSurfaceLoopUsed reports how many self-driven turns the current period has
// added, and ChatSurfaceLoopBound the ceiling in force — the pair a carrier
// prints when a self-driven turn starts, so the operator watches the loop
// approach its own limit instead of discovering it afterwards.
func (a *Agent) ChatSurfaceLoopUsed() int  { return a.chatSurface.typed }
func (a *Agent) ChatSurfaceLoopBound() int { return a.chatSurfaceLoopBound() }

// ChatSurfaceRenewal is what ChatSurfaceUserTurn reports: the bound now in
// force, whether a reset actually happened, and whether an operator's line was
// seen at a boundary a self-driven line still occupied (the DEFERRAL — refused,
// and announced, never silently applied somewhere else).
type ChatSurfaceRenewal struct {
	// Bound is the ceiling in force after this call.
	Bound int
	// Renewed reports that the counter was reset: a fresh period is open.
	Renewed bool
	// Deferred reports that this line WAS the operator's, but a self-driven line
	// was still in hand, so the boundary belongs to the model's turn and not to
	// the operator's. It is a REFUSAL with a name, so the carrier can say what
	// happened instead of leaving the operator to infer a reset that did not
	// come.
	Deferred bool
}

// ChatSurfaceUserTurn is the RENEWAL, and it is the ONLY thing that renews: the
// carrier calls it when — and only when — a line the MODEL did not write is in
// hand. It resets the self-driven counter, so the next turn the tool types runs
// against a fresh budget, and it reports the bound now in force plus which of
// the three outcomes this boundary produced.
//
// WHERE THE RENEWAL HAPPENS, AND WHY IT IS THE CARRIER THAT SAYS SO. The
// question "was that line the user's?" has exactly one answerable location: the
// carrier, at the point where it has a line and knows WHICH ARM produced it. The
// seam it must not be in is ChatSurfaceTakePending, and that is not a style
// preference — the line the tool typed and the line the operator typed are
// INDISTINGUISHABLE at this package's boundary (both arrive as a string from the
// carrier), so a renewal inferred from the line itself would be a renewal the
// model could trigger by typing a plausible line, i.e. self-renewal. Passing the
// FACT in from the one place that knows it is what keeps the reset unreachable
// from the tool — which is the whole of the operator's ruling (task 01a0e44a).
//
// THE THREE OUTCOMES, each with the reason it exists:
//
//   - RENEWED: a period ends here. The counter goes back to zero and the next
//     self-driven turn starts a fresh budget.
//   - DEFERRED (a self-driven line is still in hand): the line about to run is
//     the MODEL's, so this is not the operator's boundary. This arm is
//     REACHABLE AND MEASURED — the operator's bootstrapping line and the tool's
//     first `type` both land before the first turn boundary — and the honest
//     resolution is to refuse WITH A NAME rather than apply the reset at a
//     boundary the operator's line does not own: a reset applied by inference
//     is exactly the self-renewal this function exists to prevent. The operator
//     re-opens the period by submitting another line, which is also what the
//     refusal says.
//   - NOTHING (no period has ever opened, or a `stop` is latched): there is no
//     budget to renew yet, or the operator deliberately ended the loop and a
//     renewal must never lift that.
func (a *Agent) ChatSurfaceUserTurn() ChatSurfaceRenewal {
	l := &a.chatSurface
	bound := a.chatSurfaceLoopBound()
	if a.chatSurface.surface == nil || l.stopped {
		return ChatSurfaceRenewal{Bound: bound}
	}
	if l.pending != "" {
		return ChatSurfaceRenewal{Bound: bound, Deferred: true}
	}
	// TWO PERIODS, ONE BOUNDARY. The turn period and the key period are separate
	// counters (they spend different things: a model round trip versus a
	// keystroke) but they are opened by the SAME event — a line the model did
	// not write — so a boundary that renews one and not the other is a contract
	// that disagrees with itself. Renewing each only when it is OPEN is what
	// keeps "did anything actually renew?" honest: a fresh line in a session
	// that never typed a turn and never injected a key opens nothing, and saying
	// it did would be the console announcing a budget that does not exist.
	renewed := false
	if l.bound > 0 {
		l.typed = 0
		renewed = true
	}
	if l.keys > 0 {
		// THE KEY BUDGET GOES BACK TO ZERO HERE, and the counter lives in the
		// injector (the object that is charged and the object that refuses), so
		// the surface is asked to reset it. `sessionKeys` is what the ceiling is
		// restored to: a renewal re-establishes what the SESSION declared, never
		// a period's tighter value as the new normal, and never a larger one.
		a.chatSurface.surface.ResetKeys(l.sessionKeys)
		l.keys = l.sessionKeys
		renewed = true
	}
	if !renewed {
		return ChatSurfaceRenewal{Bound: bound}
	}
	return ChatSurfaceRenewal{Bound: bound, Renewed: true}
}

// ChatSurfaceKeyCeiling is the key ceiling in force for the CURRENT PERIOD: the
// session's own value once a key has established one, else the declared default.
// One reader, so the tool's report, the renewal and the injector's charge can
// never disagree about the number (the session's value is kept in `sessionKeys`
// so a renewal restores it rather than a period's tighter one).
func (a *Agent) chatSurfaceKeyCeiling() int {
	if a.chatSurface.sessionKeys > 0 {
		return a.chatSurface.sessionKeys
	}
	return chatSurfaceKeyCeiling
}

// ChatSurfaceKeyUsed reports how many keys the CURRENT PERIOD has injected. It
// is the key-side sibling of ChatSurfaceLoopUsed, and it exists for the same
// reason: the reset's effect must be observable, and a test that wants to prove
// the renewal reached the counter the bound is charged against should not have
// to read a private field.
//
// HONEST SCOPE: the number is the injector's own (it is charged on acceptance,
// and it is the object that refuses), so a stub surface that does not track its
// keys reports 0 here. A caller that needs the live count of a REAL run reads it
// from the TOOL's own answer (`keys_used=N/BOUND`), which is what the end-to-end
// evidence quotes; this accessor is the in-package one.
func (a *Agent) ChatSurfaceKeyUsed() int {
	if a.chatSurface.surface == nil {
		return 0
	}
	// The injector owns the live count; the surface is the only object holding a
	// reference to it. A surface that cannot answer keeps the agent honest by
	// reporting nothing rather than a fabricated number.
	if counter, ok := a.chatSurface.surface.(chatSurfaceKeyCounter); ok {
		used, _ := counter.KeyUsed()
		return used
	}
	return 0
}

// chatSurfaceKeyCounter is the OPTIONAL half of the surface contract: reporting
// how many keys the current period has spent. It is optional because a surface
// that has no injector (a cooked reader, a test stub) genuinely has no count,
// and forcing every implementation to invent one would make "0" ambiguous
// between "none spent" and "not tracked". The tool's own `keys_used` answer is
// the mandatory half, and it comes from the injection itself.
type chatSurfaceKeyCounter interface {
	KeyUsed() (used int, bound int)
}

// chatSurfaceConfiguredBound is the CONFIGURED ceiling: the operator's declared
// bound when there is one, else the built-in default. It is the ONE place this
// package asks the config channel, so the tool's refusal, its clamping, its
// console notices and its description can never disagree about the number
// (Rule 2: one reader; the precedence and the parsing live in
// config.ChatSurfaceTurns).
//
// It is deliberately NOT cached: the resolution reads the environment (which the
// pre-verb flag hoist writes BEFORE the chat verb runs) and the settings file,
// both cheap, and a cached copy would be a second opinion that could go stale in
// a long-lived process whose operator edits settings.json.
func chatSurfaceConfiguredBound() int {
	return config.ChatSurfaceTurnCount()
}

// chatSurfaceLoopBound is the ceiling in force: the session's own value when one
// was set, else the configured ceiling. One reader, so the tool's report and the
// seam's refusal can never disagree about the number.
//
// THE SESSION'S OWN VALUE NEVER EXCEEDS THE CONFIGURED CEILING (Rule: the model
// raises nothing). It is set by chatSurfaceLoopBoundArgs, which clamps into
// [chatSurfaceRestartFloor, chatSurfaceConfiguredBound()]; the extra guard here
// is what makes that a property of the READER rather than of one writer, so a
// future writer cannot install a larger bound by accident.
func (a *Agent) chatSurfaceLoopBound() int {
	ceiling := chatSurfaceConfiguredBound()
	if a.chatSurface.bound > 0 && a.chatSurface.bound <= ceiling {
		return a.chatSurface.bound
	}
	return ceiling
}

// chatSurfaceLoopBoundArgs clamps a caller's max_turns into the declared range.
// A value below the floor is raised to it (asking for fewer than one self-driven
// turn is not a bound, it is a refusal the caller should express with `stop`),
// and one above the ceiling is clamped DOWN, never up: the declared bound is the
// engine's, not the model's.
func chatSurfaceLoopBoundArgs(args map[string]interface{}) int {
	ceiling := chatSurfaceConfiguredBound()
	v, ok := args["max_turns"].(float64)
	if !ok || int(v) <= 0 {
		return ceiling
	}
	n := int(v)
	if n < chatSurfaceRestartFloor {
		return chatSurfaceRestartFloor
	}
	if n > ceiling {
		return ceiling
	}
	return n
}

// runChatSurfaceTool is the tool's dispatcher and the whole model-facing body.
// It is reached from executeToolDefault only, i.e. only when this tool was
// actually advertised — which happens only on the chat wire.
func (a *Agent) runChatSurfaceTool(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	switch action {
	case ChatSurfaceActionSee:
		return a.chatSurfaceSee()
	case ChatSurfaceActionCompare:
		return a.chatSurfaceCompare(args)
	case ChatSurfaceActionType:
		return a.chatSurfaceType(args)
	case ChatSurfaceActionKey:
		return a.chatSurfaceKey(args)
	case ChatSurfaceActionStop:
		return a.chatSurfaceStop()
	default:
		return fmt.Sprintf("Missing or invalid parameter: action (%s), got %q", chatSurfaceActionValues(), action)
	}
}

// chatSurfaceSee renders this surface's own screen: the facts, the
// qualification (when the grid may not be read as a screen), then the grid.
func (a *Agent) chatSurfaceSee() string {
	if a.chatSurface.surface == nil {
		return chatSurfaceUnavailableNotice
	}
	obs := a.chatSurface.surface.ScreenGrid()
	var b strings.Builder
	fmt.Fprintf(&b, "%s action=%s armed=%t found=%t path=%s\n",
		toolnames.ToolChatSurface, ChatSurfaceActionSee, obs.Armed, obs.Found, obs.Path)
	if !obs.Armed {
		b.WriteString(chatSurfaceNotArmedNotice + "\n")
		return b.String()
	}
	if !obs.Found {
		b.WriteString(chatSurfaceNoDumpNotice + "\n")
		return b.String()
	}
	fmt.Fprintf(&b, "surface=chat state=%s geometry=%s cols=%d rows=%d frames=%d dumps=%d age_seconds=%.1f\n",
		obs.State, obs.Geometry, obs.Cols, obs.Rows, obs.Frames, obs.Dumps, obs.AgeSeconds)
	if obs.Qualification != "" {
		// ABOVE the grid, the same way the reader prints it: a qualification
		// that rides below a wall of cells is read after the reader has already
		// believed the cells.
		b.WriteString("qualification: " + obs.Qualification + "\n")
	}
	b.WriteString(obs.Grid)
	if !strings.HasSuffix(obs.Grid, "\n") {
		b.WriteString("\n")
	}
	return chatSurfaceSeeBound(b.String(), obs.Path)
}

// chatSurfaceCompare checks ONE line against both sides and, when they disagree,
// names the direction — which side lost it. The rendering is deliberately
// compact: the model needs the verdicts, not the reader's whole record.
func (a *Agent) chatSurfaceCompare(args map[string]interface{}) string {
	if a.chatSurface.surface == nil {
		return chatSurfaceUnavailableNotice
	}
	line, _ := args["text"].(string)
	if strings.TrimSpace(line) == "" {
		return chatSurfaceCompareNeedsLine
	}
	session, _ := args["session"].(string)
	obs := a.chatSurface.surface.ScreenGrid()
	if !obs.Armed {
		return chatSurfaceNotArmedNotice
	}
	if !obs.Found {
		return chatSurfaceNoDumpNotice
	}
	if obs.Qualification != "" {
		// The grid may not be read as a screen: refuse BEFORE reading the
		// transcript, because asking for one would suggest the missing piece is
		// the piece that would have helped (the same order the verb keeps).
		return fmt.Sprintf("%s action=%s refused: %s", toolnames.ToolChatSurface, ChatSurfaceActionCompare, obs.Qualification)
	}
	path, transcript, err := a.chatSurface.surface.Transcript(strings.TrimSpace(session))
	if err != nil {
		return fmt.Sprintf("%s action=%s could_not_run: %v", toolnames.ToolChatSurface, ChatSurfaceActionCompare, err)
	}
	cand, err := uiclient.ReadDump(obs.Path)
	if err != nil {
		return fmt.Sprintf("%s action=%s could_not_run: cannot read the screen artifact %s: %v",
			toolnames.ToolChatSurface, ChatSurfaceActionCompare, obs.Path, err)
	}
	if refusal := uiclient.CompareGridUnusable(cand); refusal != "" {
		return fmt.Sprintf("%s action=%s refused: %s", toolnames.ToolChatSurface, ChatSurfaceActionCompare, refusal)
	}
	verdict := uiclient.CompareTranscriptLine(line, cand, transcript)
	var b strings.Builder
	fmt.Fprintf(&b, "%s action=%s status=%s in_grid=%t in_transcript=%t grid_row=%d prefix=%d damage=%s display_hits=%d geometry=%s cols=%d rows=%d frames=%d transcript=%s transcript_lines=%d\n",
		toolnames.ToolChatSurface, ChatSurfaceActionCompare, verdict.Status, verdict.InGrid, verdict.InTranscript,
		verdict.GridRow, verdict.GridPrefixRunes, verdict.Damage, verdict.DisplayHits, verdict.Geometry, verdict.Cols, verdict.Rows, verdict.Frames,
		path, len(transcript))
	switch verdict.Status {
	case uiclient.CompareMismatch:
		// The direction IS the diagnosis, so it is stated in words rather than
		// left to a reader comparing two booleans. The DAMAGE word is printed first
		// because it carries the same fact with the class the whole-answer audit
		// uses (one classifier, two faces): a row that displays the line and carries
		// further text is damage=damage_to_row — the paint did NOT lose the text.
		// A line the grid reassembled across rows is not a MISMATCH at all: the
		// verdict is a match carrying wrap_rows, and it is named below.
		switch {
		case verdict.Damage == uiclient.DamageToRow:
			fmt.Fprintf(&b, "direction=row_not_alone — the row displays the WHOLE line (display_hits=%d) and continues past it: the text is on the grid, and the ROW is what is damaged (a stale re-paint, or a second line over its tail) — not the line's text.\n", verdict.DisplayHits)
		case verdict.InTranscript && !verdict.InGrid:
			fmt.Fprintf(&b, "direction=transcript_only — the transcript holds this line and the grid does not: the PAINT lost or wrapped it (prefix=%d runes of it do appear at a row start).\n", verdict.GridPrefixRunes)
		default:
			b.WriteString("direction=grid_only — the grid holds this line and the transcript does not: the frame is stale, or this is not that session's screen.\n")
		}
	case uiclient.CompareMatch:
		if verdict.Rejoined {
			fmt.Fprintf(&b, "direction=both_wrapped — the grid carries the whole line CONTINUED across rows %v: the text arrived, and no single row is the line.\n", verdict.WrapRows)
			break
		}
		b.WriteString("direction=both — the paint is faithful for this line, and the observation channel is intact.\n")
	}
	if verdict.Reason != "" {
		b.WriteString("reason: " + verdict.Reason + "\n")
	}
	// The exact bytes the check ran against: a verdict without its evidence is
	// unquotable, so the caller gets the lines back (bounded).
	shown := 0
	for _, l := range transcript {
		if shown >= chatSurfaceCompareSeed {
			fmt.Fprintf(&b, "transcript_has_more=%d\n", len(transcript)-shown)
			break
		}
		fmt.Fprintf(&b, "transcript[%d]=%s\n", shown, l)
		shown++
	}
	return b.String()
}

// chatSurfaceType is the hand: it types a line into this surface's own input and
// submits it, so this turn is followed by one the turn itself wrote.
func (a *Agent) chatSurfaceType(args map[string]interface{}) string {
	if a.chatSurface.surface == nil {
		return chatSurfaceUnavailableNotice
	}
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return chatSurfaceTypeNeedsText
	}
	runes := []rune(text)
	if len(runes) > chatSurfaceTypeRunesMax {
		return fmt.Sprintf("%s action=%s refused: the line is %d runes, beyond this tool's bound of %d — bound the input, do not paste it.",
			toolnames.ToolChatSurface, ChatSurfaceActionType, len(runes), chatSurfaceTypeRunesMax)
	}
	if a.chatSurface.stopped {
		// A stop is LATCHED: the loop does not restart itself. The message names
		// the wall it actually hit (a stop, not the bound — the two have different
		// remedies), and it names the remedy that exists (a human, or a new
		// session) rather than an override this tool does not offer.
		return fmt.Sprintf("%s action=%s refused: a %s request cancelled this session's self-driven loop and it does not restart itself. Continue the work with an ordinary turn, or start a new session.",
			toolnames.ToolChatSurface, ChatSurfaceActionType, ChatSurfaceActionStop)
	}
	// The bound is set at the FIRST type of the SESSION and is stable after that,
	// so "how many self-driven turns are left" cannot move under the model. The
	// test for "first" is the bound being UNSET (`bound == 0`), never the seam
	// being idle: the seam is idle at EVERY turn boundary (the REPL consumes the
	// pending line before the next model request), so keying on it let a later
	// `type` in the SAME period re-set the number — a model that asked for
	// `max_turns: 2` on its second turn cut the period from the declared
	// chatSurfaceLoopBound down to chatSurfaceRestartFloor, a leash length the
	// model chose and the operator could not see. The sibling action=key has
	// always tested this way (`a.chatSurface.keys == 0`); this is the same shape.
	// A caller that wants a different bound gets it in a NEW session, which is
	// what the refusal below says.
	bound := a.chatSurfaceLoopBound()
	sessionBoundSet := a.chatSurface.bound != 0
	if !sessionBoundSet {
		a.chatSurface.bound = chatSurfaceLoopBoundArgs(args)
		bound = a.chatSurface.bound
	}
	// A max_turns this call could not honour is SAID OUT LOUD, never dropped:
	// the model asked for a leash length and got another, so the answer names
	// the number in force and why the request could not move it (Rule 15 — a
	// silent no-op is a failure the caller cannot see).
	boundIgnored := ""
	if want, ok := args["max_turns"].(float64); sessionBoundSet && ok && int(want) != bound {
		boundIgnored = fmt.Sprintf(" (max_turns=%d was IGNORED: this session's bound is %d, set at its first %s and stable after that — only a NEW session moves it)",
			int(want), bound, ChatSurfaceActionType)
	}
	if a.chatSurface.typed >= bound {
		// THE WALL, AND THE EXIT ON THE OTHER SIDE OF IT. The bound does not
		// move (Rule: no model self-renewal — the ceiling is the CONFIGURED bound
		// and nothing this tool does can raise it). What
		// the refusal must NOT do is hand an UNATTENDED run an exit it cannot
		// take: "say what the operator should type" is a dead remedy when nobody
		// is reading, and a model told to do the impossible stops — the exact
		// failure shape the unattended context exists to remove
		// (chat_unattended.go). So the remedy is branched on WHOSE run this is:
		// an interactive console keeps the historic sentence (a human really is
		// there to type the next period), and an unattended run is told to
		// continue in its own answer and to choose for itself.
		remedy := "Continue the work with an ordinary turn (your answer is itself a turn) and say in it what the operator should type to let you continue autonomously; the bound also rises to a larger value in a NEW session."
		closer := "it does not extend itself, and only a line the OPERATOR submits opens a fresh period"
		if a.Unattended {
			// No operator exists on this run, so the interactive closer — "only
			// a line the OPERATOR submits opens a fresh period" — would name a
			// door that is not there; the unattended closer names the door that
			// is (a NEW session), and the remedy names the exit that exists
			// (this answer).
			closer = "it does not extend itself, and nothing on this run opens a fresh period"
			remedy = "Continue the work with an ordinary turn — your answer IS the next step, and nothing about this run needs the operator: do not wait for one and do not ask it to type anything. Deliver what you can finish NOW, say in one clause which convention you chose wherever the task left a parameter open, and end your answer with the concrete remaining step."
		}
		return fmt.Sprintf("%s action=%s refused: this period's self-driven loop is at its bound (%d of %d turns used)%s — %s. %s",
			toolnames.ToolChatSurface, ChatSurfaceActionType, a.chatSurface.typed, bound, boundIgnored, closer, remedy)
	}
	// The surface's own acceptance decides, because only it knows whether a line
	// source can carry one: a piped or one-shot run has no next-turn seam, and
	// reporting "typed" there would be a promise nothing keeps.
	accepted, inForce, why := a.chatSurface.surface.Enqueue(strings.TrimSpace(text), bound)
	if !accepted {
		// THE NO-SEAM REFUSAL, on the same branch as the wall above and for the
		// same reason: a one-shot run cannot take "wait for the next line", so
		// an unattended run is told the exit that DOES exist for it. The
		// interactive wording is untouched — `why` is the surface's own
		// sentence, and this only appends the unattended run's continuation.
		// (Measured: a real `gopi chat --once` run was told by an earlier
		// wording of the tool description that type was its self-continuation
		// route, tried it, was refused here, and spent turns hunting for a seam
		// that does not exist — a promised route that cannot be taken is a trap,
		// not guidance.)
		extra := ""
		if a.Unattended {
			extra = " — and there is no operator to wait for on this run either: continue IN YOUR ANSWER (it is the next step), deliver what you can finish now, and choose a convention yourself wherever the task left one open."
		}
		return fmt.Sprintf("%s action=%s refused: %s%s", toolnames.ToolChatSurface, ChatSurfaceActionType, why, extra)
	}
	replaced := ""
	if a.chatSurface.pending != "" {
		// ONE slot: the earlier pending line is dropped, and the drop is stated
		// (a silently replaced input is an input the model believes it sent).
		replaced = " (it REPLACED an earlier pending line that had not been consumed)"
	}
	a.chatSurface.pending = strings.TrimSpace(text)
	return fmt.Sprintf("%s action=%s typed=true bound_in_force=%d turns_used=%d/%d pending=true%s%s — this line becomes the NEXT turn when the current one ends. It is your own input: write it as the user would (a question or an instruction), not as a note to yourself.",
		toolnames.ToolChatSurface, ChatSurfaceActionType, inForce, a.chatSurface.typed+1, inForce, boundIgnored, replaced)
}

// chatSurfaceKey is the hand at KEY level: it injects one key's raw bytes into
// this surface's own line editor — through the same decoder a keyboard feeds —
// and submits nothing. It is what closes the interaction-verification loop that
// `type` cannot close (see the action's own documentation in the tool def).
//
// THE CEILING AND THE TURN BOUND ARE SEPARATE, and they have to be: a key costs
// no model round trip, so counting keys against max_turns would either starve
// the interaction or spend the turn budget on arrow keys. The ceiling is set at
// the FIRST key and is stable for the session, exactly as max_turns is, so "how
// many keys are left" cannot move under the model.
func (a *Agent) chatSurfaceKey(args map[string]interface{}) string {
	if a.chatSurface.surface == nil {
		return chatSurfaceUnavailableNotice
	}
	raw, _ := args["text"].(string)
	if strings.TrimSpace(raw) == "" {
		return chatSurfaceKeyNeedsKey
	}
	if len(raw) > chatSurfaceKeyBytesMax {
		return fmt.Sprintf("%s action=%s refused: the byte sequence is %d bytes, beyond one key's bound of %d — one injected key is one read unit, so a longer sequence would be read as a paste.",
			toolnames.ToolChatSurface, ChatSurfaceActionKey, len(raw), chatSurfaceKeyBytesMax)
	}
	if a.chatSurface.keys == 0 {
		// First key of the session: the caller's ceiling becomes the session's.
		// It is recorded as the SESSION's value (what every later period is
		// restored to) as well as this period's, because a renewal must
		// re-establish the declared ceiling and not a period's experiment.
		a.chatSurface.keys = chatSurfaceKeyCeilingArgs(args)
		a.chatSurface.sessionKeys = a.chatSurface.keys
	}
	res := a.chatSurface.surface.InjectKey(raw, a.chatSurface.keys)
	if !res.Accepted {
		return fmt.Sprintf("%s action=%s refused: %s (keys used %d/%d)", toolnames.ToolChatSurface, ChatSurfaceActionKey, res.Why, res.Used, res.Bound)
	}
	// EXHAUSTION IS ANNOUNCED ON THE CALL THAT CONSUMES IT, and on the operator's
	// console as well as in the model's answer: the ceiling is the operator's
	// guarantee, and one that only the model can see is not a guarantee. The
	// model's own line repeats the fact so a caller cannot miss it either.
	out := fmt.Sprintf("%s action=%s injected=%q read_by_editor=%t pending_bytes=%d keys_used=%d/%d (this period) — the key went onto this surface's OWN input path (the same decoder a keyboard goes through); nothing was submitted.",
		toolnames.ToolChatSurface, ChatSurfaceActionKey, res.Label, res.ReadByEditor, res.Pending, res.Used, res.Bound)
	if !res.ReadByEditor {
		// The honest half: a key queued while this turn is still running is read
		// at the NEXT prompt. Saying "the editor acted on it" would be a claim
		// nothing measured, and a caller planning to read the grid would be
		// reading a frame that has not drawn it.
		out += " NOTE read_by_editor=false: the line editor had not consumed it yet — it takes the key at the next prompt, so " +
			"read the grid AFTER it is consumed rather than assuming this call performed it."
	}
	if res.Last {
		fmt.Fprintf(os.Stderr, chatSurfaceExhaustedNotice, res.Used, res.Bound)
		out += fmt.Sprintf(" CEILING REACHED: this was the last key of this period (%d of %d) — no key will be injected until a fresh period opens. Only a line the OPERATOR submits opens one; the bound does not raise itself, and the conversation continues normally in the meantime.", res.Used, res.Bound)
	}
	return out
}

// chatSurfaceStop cancels a pending or running self-driven loop.
//
// A stop with NOTHING to cancel is a NO-OP, and it does not latch: `stop` on an
// idle session must not poison a loop the caller has not started yet. Only a
// stop that actually cancelled something latches, which is what makes the latch
// mean "this session's loop was ended" rather than "someone said stop once".
func (a *Agent) chatSurfaceStop() string {
	if a.chatSurface.surface == nil {
		return chatSurfaceUnavailableNotice
	}
	hadPending := a.chatSurface.pending != ""
	running := a.chatSurface.surface.CancelLoop()
	if !hadPending && !running {
		return chatSurfaceStopNothing
	}
	// The line is NOT dropped here: the seam drops it (ChatSurfaceTakePending),
	// so there is exactly ONE place a line can disappear and exactly one place
	// that reports it.
	a.chatSurface.stopped = true
	return fmt.Sprintf("%s action=%s stopped=true pending_dropped=%t run_cancelled=%t — no further self-driven turn will be added this session.",
		toolnames.ToolChatSurface, ChatSurfaceActionStop, hadPending, running)
}

// ChatSurfaceDumpReader reads a screen artifact (a Candidate + its parsed grid)
// from a path. It is a PARAMETER of the surface implementation rather than a
// call made here: this package must not depend on the session tree's readers
// (internal/gopilog imports this one for the transcript formatter, so a call in
// the other direction is an import cycle), and the surface's installer already
// has the reader it needs.
type ChatSurfaceDumpReader func(path string) (uiclient.Candidate, error)
