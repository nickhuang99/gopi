package agent

// chat_surface_test.go — the chat-scoped tool's contract, pinned on the REAL
// surfaces it touches: the two tool-def lists (the wire separation Rule 3
// requires), the action-conditional validation, the bounded self-driven loop's
// seam, and the sentence-not-silence property of every unavailable path.

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/toolnames"
)

// toolDefNames lists a def slice's names in order, so a comparison can name the
// DIFFERENCE rather than reporting two long literals as unequal.
func toolDefNames(defs []deepseek.ClaudeToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// TestChatSurface_ToolDefOnlyOnTheChatWire is the Rule 3 gate for this tool: the
// ordinary surface (claudeToolDefs — the SSOT TestGOPIRule3_MatchesClaudeToolDefs
// pins against GOPI.md) is UNTOUCHED, and the chat wire differs from it by
// exactly one extra name when, and only when, a surface is installed.
func TestChatSurface_ToolDefOnlyOnTheChatWire(t *testing.T) {
	// 1) The ordinary surface does not carry it — this is the Rule 3 property,
	// asserted here too so a reader of this file sees it without opening
	// gopidoc_sync_test.go.
	for _, d := range claudeToolDefs {
		if d.Name == toolnames.ToolChatSurface {
			t.Fatalf("claudeToolDefs carries %q: that slice is the SSOT of the ORDINARY surface (Rule 3), "+
				"and a chat-scoped tool in it is a new core tool advertised to every task run",
				toolnames.ToolChatSurface)
		}
	}

	// 2) With no surface installed (the default, and the shape every non-REPL
	// carrier of ChatOps has — `gopi web --tools`, `gopi cli --tools`, tests),
	// the chat list is byte-for-byte the ordinary one.
	bare := (&Agent{}).chatOpsToolDefs()
	if got, want := len(bare), len(claudeToolDefs); got != want {
		t.Fatalf("chat wire without a surface: %d defs, want %d (the ordinary list unchanged)", got, want)
	}
	for i, d := range bare {
		if d.Name != claudeToolDefs[i].Name {
			t.Fatalf("chat wire without a surface: def %d is %q, want %q — the uninstalled list must be the "+
				"ordinary list exactly, or a carrier with nothing behind the tool advertises it anyway",
				i, d.Name, claudeToolDefs[i].Name)
		}
	}

	// 3) With a surface installed, the ONLY difference is the one name.
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})
	with := toolDefNames(a.chatOpsToolDefs())
	if got, want := len(with), len(claudeToolDefs)+1; got != want {
		t.Fatalf("chat wire with a surface: %d defs, want %d", got, want)
	}
	extra := make([]string, 0, 1)
	ordinary := map[string]bool{}
	for _, d := range claudeToolDefs {
		ordinary[d.Name] = true
	}
	for _, n := range with {
		if !ordinary[n] {
			extra = append(extra, n)
		}
	}
	if len(extra) != 1 || extra[0] != toolnames.ToolChatSurface {
		t.Fatalf("chat wire differs from the ordinary list by %v, want exactly [%q]", extra, toolnames.ToolChatSurface)
	}

	// The order is stable: the extra def is appended, so every ordinary def keeps
	// its index and prompt-cache markers cannot shift under a chat turn.
	for i, d := range claudeToolDefs {
		if with[i] != d.Name {
			t.Fatalf("chat wire reordered the ordinary surface at %d: %q, want %q", i, with[i], d.Name)
		}
	}
	if with[len(with)-1] != toolnames.ToolChatSurface {
		t.Fatalf("the chat-scoped def is not last: %q", with[len(with)-1])
	}
}

// TestChatSurface_DefSchemaIsTheActionContract pins the model-facing schema: the
// enum is the ONE vocabulary the dispatcher branches on, so a verb added to the
// constants without reaching the schema (or the reverse) fails here.
func TestChatSurface_DefSchemaIsTheActionContract(t *testing.T) {
	if chatSurfaceToolDef().Name != toolnames.ToolChatSurface {
		t.Fatalf("def name %q, want %q", chatSurfaceToolDef().Name, toolnames.ToolChatSurface)
	}
	if !strings.Contains(chatSurfaceToolDef().Description, toolnames.ToolChatSurface) {
		t.Fatalf("the description does not name the tool: %q", chatSurfaceToolDef().Description)
	}
	schema, ok := chatSurfaceToolDef().InputSchema.(map[string]interface{})
	if !ok {
		t.Fatalf("InputSchema is %T, want a map", chatSurfaceToolDef().InputSchema)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is %T, want a map", schema["properties"])
	}
	action, ok := props["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("action property is %T, want a map", props["action"])
	}
	enum, ok := action["enum"].([]string)
	if !ok {
		t.Fatalf("action enum is %T, want []string", action["enum"])
	}
	if len(enum) != len(chatSurfaceActions) {
		t.Fatalf("schema enum has %d verbs, the vocabulary has %d", len(enum), len(chatSurfaceActions))
	}
	for i, v := range chatSurfaceActions {
		if enum[i] != v {
			t.Fatalf("schema enum[%d] = %q, vocabulary = %q — the wire enum and the dispatcher's vocabulary "+
				"must be the same list, or a verb can be advertised and unroutable", i, enum[i], v)
		}
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want exactly [action]", schema["required"])
	}
	// No PIXEL path: this tool must never grow an image operand — the chat
	// surface's evidence is the cell grid, and a screenshot of the terminal would
	// be a second, uninterpreted channel.
	if _, has := props["image"]; has {
		t.Fatal("the chat-scoped tool grew an image operand: the chat surface is read as a CELL GRID, never as pixels")
	}
}

// TestChatSurface_ValidationIsActionConditional pins the required-operand check:
// a missing operand is answered with the SENTENCE that names it, and an unknown
// verb with the valid list — never with a silent empty run.
func TestChatSurface_ValidationIsActionConditional(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want string // "" = must pass
	}{
		{"see needs nothing", map[string]interface{}{"action": ChatSurfaceActionSee}, ""},
		{"stop needs nothing", map[string]interface{}{"action": ChatSurfaceActionStop}, ""},
		{"compare without text", map[string]interface{}{"action": ChatSurfaceActionCompare}, chatSurfaceCompareNeedsLine},
		{"compare with text", map[string]interface{}{"action": ChatSurfaceActionCompare, "text": "x"}, ""},
		{"type without text", map[string]interface{}{"action": ChatSurfaceActionType}, chatSurfaceTypeNeedsText},
		{"type with blank text", map[string]interface{}{"action": ChatSurfaceActionType, "text": "   "}, chatSurfaceTypeNeedsText},
		{"type with text", map[string]interface{}{"action": ChatSurfaceActionType, "text": "next, please"}, ""},
		{"key without text", map[string]interface{}{"action": ChatSurfaceActionKey}, chatSurfaceKeyNeedsKey},
		{"key with blank text", map[string]interface{}{"action": ChatSurfaceActionKey, "text": "   "}, chatSurfaceKeyNeedsKey},
		{"key with a named key", map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"}, ""},
		{"key with a literal rune", map[string]interface{}{"action": ChatSurfaceActionKey, "text": "/"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := validateChatSurfaceArgs(c.args)
			if c.want == "" && got != "" {
				t.Fatalf("validateChatSurfaceArgs(%v) = %q, want no error", c.args, got)
			}
			if c.want != "" && got != c.want {
				t.Fatalf("validateChatSurfaceArgs(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
	// An unknown verb, and a missing one, both name the vocabulary.
	for _, args := range []map[string]interface{}{{"action": "look"}, {}} {
		got := validateChatSurfaceArgs(args)
		if !strings.Contains(got, chatSurfaceActionValues()) {
			t.Fatalf("unknown action %v got %q, want the vocabulary named", args, got)
		}
	}
}

// TestChatSurface_UninstalledAnswersASentence is the Rule 15 property: with no
// surface installed every action returns a sentence naming what is missing — a
// silent empty result would read as "I looked and there was nothing there".
func TestChatSurface_UninstalledAnswersASentence(t *testing.T) {
	a := &Agent{}
	for _, act := range chatSurfaceActions {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": act, "text": "x"})
		if strings.TrimSpace(got) == "" {
			t.Fatalf("action=%s on an agent with no surface returned an empty string — silence is not an answer", act)
		}
		if act != ChatSurfaceActionSee && act != ChatSurfaceActionStop && !strings.Contains(got, chatSurfaceUnavailableNotice) {
			t.Fatalf("action=%s returned %q, want the unavailable notice", act, got)
		}
		if act == ChatSurfaceActionSee && got != chatSurfaceUnavailableNotice {
			t.Fatalf("see on an agent with no surface returned %q, want the unavailable notice", got)
		}
	}
}

// TestChatSurface_SeeDistinguishesUnarmedFromBlank is the measured failure mode
// this tool must not repeat: an unarmed run writes no artifact, and that ABSENCE
// OF OBSERVATION must never be rendered as a blank screen.
func TestChatSurface_SeeDistinguishesUnarmedFromBlank(t *testing.T) {
	a := &Agent{}
	for _, c := range []struct {
		name string
		obs  ChatScreenObservation
		want string
	}{
		{"unarmed", ChatScreenObservation{Armed: false}, chatSurfaceNotArmedNotice},
		{"armed but no artifact yet", ChatScreenObservation{Armed: true, Found: false}, chatSurfaceNoDumpNotice},
	} {
		t.Run(c.name, func(t *testing.T) {
			a.SetChatSurface(stubChatSurface{obs: c.obs})
			got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionSee})
			if !strings.Contains(got, c.want) {
				t.Fatalf("see returned %q, want it to carry %q", got, c.want)
			}
			// The grid must not be there at all: a qualification followed by cells
			// is a reader that has already believed the cells.
			if strings.Contains(got, "qualification") && c.want != "" {
				t.Fatalf("see returned a grid alongside the refusal: %q", got)
			}
		})
	}

	// An armed, found, QUALIFIED grid reports the qualification above the cells
	// and in the reader's own words.
	a.SetChatSurface(stubChatSurface{obs: ChatScreenObservation{
		Armed: true, Found: true, Path: "/x/chat-screen.dump",
		Grid: "some cells", Qualification: "the grid is not painted",
	}})
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionSee})
	if !strings.Contains(got, "the grid is not painted") {
		t.Fatalf("see dropped the qualification: %q", got)
	}
	if strings.Index(got, "the grid is not painted") > strings.Index(got, "some cells") {
		t.Fatalf("the qualification rides BELOW the grid — it is read after the reader believed the cells: %q", got)
	}

	// A compare refuses on a qualified grid, BEFORE reading anything else: asking
	// for the transcript would suggest the transcript is the missing piece.
	a.SetChatSurface(stubChatSurface{
		obs:      ChatScreenObservation{Armed: true, Found: true, Path: "/x/chat-screen.dump", Qualification: "the grid is not painted"},
		calledTx: false,
	})
	got = a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionCompare, "text": "hello"})
	if !strings.Contains(got, "refused") || !strings.Contains(got, "the grid is not painted") {
		t.Fatalf("compare on a qualified grid returned %q, want a refusal naming the qualification", got)
	}
}

// TestChatSurface_LoopBoundIsEnforcedAtTheSeam pins the self-driven loop's whole
// safety property: the bound is a NUMBER, the seam is the ONE place it is
// charged, and hitting it REFUSES and SAYS SO (a silent refusal is a turn that
// vanished — devdoc/runaway-loop-20260923.md).
func TestChatSurface_LoopBoundIsEnforcedAtTheSeam(t *testing.T) {
	a := &Agent{}
	surf := stubChatSurface{}
	a.SetChatSurface(surf)

	// Each `type` enqueues; each consumption charges the counter, and the LAST
	// one says so on its own event (Closing) — the loop announces its end as it
	// hands out the turn that reaches it.
	for i := 1; i <= chatSurfaceLoopBound; i++ {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "turn"})
		if !strings.Contains(got, "typed=true") {
			t.Fatalf("type #%d returned %q, want an acceptance", i, got)
		}
		turn := a.ChatSurfaceTakePending()
		if turn.Kind != ChatSurfaceSeamTyped || turn.Line != "turn" {
			t.Fatalf("consumption #%d = %+v, want a typed turn carrying \"turn\"", i, turn)
		}
		if wantClosing := i == chatSurfaceLoopBound; turn.Closing != wantClosing {
			t.Fatalf("turn #%d Closing=%t, want %t", i, turn.Closing, wantClosing)
		}
		if turn.Bound != chatSurfaceLoopBound {
			t.Fatalf("turn #%d reports bound %d, want %d", i, turn.Bound, chatSurfaceLoopBound)
		}
	}
	if used, bound := a.ChatSurfaceLoopUsed(), a.ChatSurfaceLoopBound(); used != bound || used != chatSurfaceLoopBound {
		t.Fatalf("after %d turns: used=%d bound=%d", chatSurfaceLoopBound, used, bound)
	}

	// The bound is now spent, and the tool refuses the next type by name — so no
	// line is ever enqueued with the counter spent, which is why the seam's own
	// bound arm is unreachable through the tool (it is still enforced there, for
	// any other carrier).
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "one more"})
	if !strings.Contains(got, "refused") || !strings.Contains(got, "bound") {
		t.Fatalf("type past the bound returned %q, want a refusal naming the bound", got)
	}
	// Nothing was queued, so the carrier reads its own input. The loop's END was
	// already announced — by the Closing flag on the last turn it handed out (one
	// line carries both the turn and the end), so the seam must NOT fire a second
	// closing event here: a notice that repeats every turn is noise, and noise is
	// how a real stop goes unread.
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamNone {
		t.Fatalf("the closing event fired twice (last turn already carried Closing): %+v", turn)
	}
}

// TestChatSurface_StopCancelsAndCannotBeUndone pins `stop`: it cancels, the loop
// does not restart itself, and a later `type` says which wall it hit (a stop is
// not a bound, and conflating the two sends the operator to the wrong remedy).
func TestChatSurface_StopCancelsAndCannotBeUndone(t *testing.T) {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})

	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop}); got != chatSurfaceStopNothing {
		t.Fatalf("stop with nothing pending = %q, want the nothing-to-stop sentence", got)
	}
	// A stop with nothing to cancel must NOT latch: the loop the caller has not
	// started yet is still available.
	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "first"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type after an idle stop returned %q — an idle stop must not poison a loop nobody started", got)
	}
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamTyped || turn.Line != "first" {
		t.Fatalf("consumption after an idle stop = %+v, want the typed turn \"first\"", turn)
	}

	if got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "step"}); !strings.Contains(got, "typed=true") {
		t.Fatalf("type returned %q", got)
	}
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionStop})
	if !strings.Contains(got, "pending_dropped=true") {
		t.Fatalf("stop with a pending line returned %q, want the drop named", got)
	}
	// The seam is the ONE place the line is dropped, and it SAYS SO: a line that
	// vanished silently between being typed and becoming a turn is the failure
	// this signal exists for.
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamClosed || turn.Line != "" {
		t.Fatalf("after stop the seam returned %+v, want the closing event (the typed line dropped)", turn)
	}
	// ...and it drops it ONCE: the next call is an ordinary "nothing pending".
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamNone {
		t.Fatalf("the cancelled line survived its own drop: %+v", turn)
	}
	// A later type names the STOP, not the bound: the remedies differ.
	got = a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "again"})
	if !strings.Contains(got, ChatSurfaceActionStop) {
		t.Fatalf("type after stop returned %q, want it to name the stop as the reason", got)
	}
}

// TestChatSurface_BoundsAreDeclaredAndClamped pins Rule 1's shape for this tool:
// the caller's max_turns cannot exceed the declared bound (an engine bound is
// not the model's to raise) and cannot fall below the floor.
func TestChatSurface_BoundsAreDeclaredAndClamped(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{"absent → the declared bound", map[string]interface{}{}, chatSurfaceLoopBound},
		{"zero → the declared bound", map[string]interface{}{"max_turns": float64(0)}, chatSurfaceLoopBound},
		{"negative → the declared bound", map[string]interface{}{"max_turns": float64(-3)}, chatSurfaceLoopBound},
		{"below the floor → the floor", map[string]interface{}{"max_turns": float64(1)}, chatSurfaceRestartFloor},
		{"inside the range → itself", map[string]interface{}{"max_turns": float64(3)}, 3},
		{"the bound itself", map[string]interface{}{"max_turns": float64(chatSurfaceLoopBound)}, chatSurfaceLoopBound},
		{"above the bound → CLAMPED DOWN", map[string]interface{}{"max_turns": float64(chatSurfaceLoopBound + 100)}, chatSurfaceLoopBound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatSurfaceLoopBoundArgs(c.args); got != c.want {
				t.Fatalf("chatSurfaceLoopBoundArgs(%v) = %d, want %d", c.args, got, c.want)
			}
		})
	}
	// The floor is below the bound, or "bound it sooner" would be unsatisfiable.
	if chatSurfaceRestartFloor >= chatSurfaceLoopBound {
		t.Fatalf("restart floor %d >= bound %d: a caller could not bound it sooner than the default",
			chatSurfaceRestartFloor, chatSurfaceLoopBound)
	}
}

// TestChatSurface_TypeRefusesWhatTheSurfaceCannotCarry pins honesty about the
// carrier: a surface that cannot accept a line (no next-turn seam) must produce
// a REFUSAL, never a "typed" that nothing will honour.
func TestChatSurface_TypeRefusesWhatTheSurfaceCannotCarry(t *testing.T) {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{refuseEnqueue: "no next-turn seam here"})
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "hello"})
	if !strings.Contains(got, "refused") || !strings.Contains(got, "no next-turn seam here") {
		t.Fatalf("type on a carrier that cannot accept one returned %q, want the surface's own refusal", got)
	}
	// Nothing was queued, so the seam has nothing to hand out.
	if turn := a.ChatSurfaceTakePending(); turn.Kind != ChatSurfaceSeamNone {
		t.Fatalf("a refused type left something at the seam: %+v", turn)
	}

	// The SAME refusal on an UNATTENDED run must also say what to do instead:
	// "no next-turn seam" alone tells a run with no operator to keep trying a
	// door that does not exist (measured: a real `gopi chat --once` run did
	// exactly that and burned turns hunting for the seam). The interactive
	// wording above stays byte-for-byte what the surface said — the branch is an
	// append, never a rewrite.
	ag := &Agent{Unattended: true}
	ag.SetChatSurface(stubChatSurface{refuseEnqueue: "no next-turn seam here"})
	un := ag.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "hello"})
	if !strings.Contains(un, "no next-turn seam here") {
		t.Fatalf("the unattended refusal lost the surface's own reason: %q", un)
	}
	if !strings.HasPrefix(un, got) {
		t.Fatalf("the unattended refusal REWROTE the interactive one instead of extending it:\n got %q\nwant prefix %q", un, got)
	}
	if !strings.Contains(un, "there is no operator to wait for on this run either") {
		t.Fatalf("the unattended refusal does not name the exit that exists: %q", un)
	}
}

// TestChatSurface_TypeIsRuneBounded pins the input cap: one call cannot park an
// arbitrarily large string in the queue.
func TestChatSurface_TypeIsRuneBounded(t *testing.T) {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})
	got := a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionType,
		"text":   strings.Repeat("字", chatSurfaceTypeRunesMax+1),
	})
	if !strings.Contains(got, "refused") || !strings.Contains(got, "runes") {
		t.Fatalf("an oversized type returned %q, want a refusal naming the rune count", got)
	}
	// The cap counts RUNES, not bytes: a CJK line of exactly the cap passes.
	ok := a.runChatSurfaceTool(map[string]interface{}{
		"action": ChatSurfaceActionType,
		"text":   strings.Repeat("字", chatSurfaceTypeRunesMax),
	})
	if !strings.Contains(ok, "typed=true") {
		t.Fatalf("a line of exactly %d runes was refused: %q", chatSurfaceTypeRunesMax, ok)
	}
}

// stubChatSurface is the test double for the surface half: it records what the
// tool asked of it, so the assertions are about the CONVERSATION between the two
// halves rather than about a rendering.
type stubChatSurface struct {
	obs           ChatScreenObservation
	refuseEnqueue string
	calledTx      bool
	transcript    []string
	// inject is what the stub's InjectKey answers, plus what it was asked. The
	// two are separate so a case can assert the tool forwarded the RIGHT key
	// while also choosing the answer it wants to render. injectLog is a POINTER
	// because the stub travels through the ChatSurface interface BY VALUE — a
	// value receiver cannot mutate the caller's copy, so the record has to live
	// behind a pointer or "the tool forwarded this key" would be unobservable.
	refuseInject string
	injectLog    *[]string
	injectLast   bool
	// keyUsed is where the stub reports its own key count from (the live number
	// a period's bound is charged against). A POINTER for the same reason
	// injectLog is one: the stub travels through the interface BY VALUE, so a
	// value receiver cannot move the caller's copy and "the reset reached the
	// counter" would be unobservable.
	keyUsed *int
	// resetKeys records every ResetKeys call's argument, so a case can assert
	// that the ONLY caller in the tree (the carrier's line arm, through
	// ChatSurfaceUserTurn) passed the session's own ceiling — and, in the
	// negative direction, that no tool action ever reached this method.
	resetKeys *[]int
}

func (s stubChatSurface) ScreenGrid() ChatScreenObservation { return s.obs }

func (s stubChatSurface) Transcript(session string) (string, []string, error) {
	if s.transcript == nil {
		return "", nil, errStubNoTranscript
	}
	return "/fixture/session.jsonl", s.transcript, nil
}

func (s stubChatSurface) Enqueue(text string, bound int) (bool, int, string) {
	if s.refuseEnqueue != "" {
		return false, bound, s.refuseEnqueue
	}
	return true, bound, ""
}

// InjectKey records the key it was asked for and answers with the stub's chosen
// outcome.
func (s stubChatSurface) InjectKey(key string, bound int) ChatKeyResult {
	if s.injectLog != nil {
		*s.injectLog = append(*s.injectLog, key)
	}
	if s.refuseInject != "" {
		return ChatKeyResult{Label: key, Bound: bound, Why: s.refuseInject}
	}
	return ChatKeyResult{Label: key, Accepted: true, ReadByEditor: true, Used: 1, Bound: bound, Last: s.injectLast}
}

func (s stubChatSurface) CancelLoop() bool { return false }

// KeyUsed reports the stub's key count (see keyUsed for why it is a pointer).
func (s stubChatSurface) KeyUsed() (int, int) {
	if s.keyUsed == nil {
		return 0, 0
	}
	return *s.keyUsed, 0
}

// ResetKeys records the call and zeroes the counter it is asked about. It is the
// stub's half of the renewal contract: a case can see that the boundary reached
// the counter (keyUsed goes to 0) and WHICH ceiling it asked for (resetKeys).
func (s stubChatSurface) ResetKeys(bound int) {
	if s.resetKeys != nil {
		*s.resetKeys = append(*s.resetKeys, bound)
	}
	if s.keyUsed != nil {
		*s.keyUsed = 0
	}
}

type stubChatSurfaceErr string

func (e stubChatSurfaceErr) Error() string { return string(e) }

const errStubNoTranscript = stubChatSurfaceErr("no transcript in this stub")

// TestChatSurface_KeyForwardsTheKeyAndAnswersFacts is the action=key contract at
// the TOOL level: the key reaches the surface verbatim (a name and a literal byte
// both, because the resolution rule lives in one place), and the answer states
// the facts the caller needs — read_by_editor included, because "queued" and
// "the editor acted on it" are different claims.
func TestChatSurface_KeyForwardsTheKeyAndAnswersFacts(t *testing.T) {
	var log []string
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{injectLog: &log})

	for _, key := range []string{"down", "enter", "/"} {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": key})
		if strings.TrimSpace(got) == "" {
			t.Fatalf("action=%s text=%q returned an empty string — silence is not an answer", ChatSurfaceActionKey, key)
		}
		if !strings.Contains(got, "nothing was submitted") {
			t.Fatalf("action=%s text=%q returned %q, want it to say nothing was submitted (the difference from type)", ChatSurfaceActionKey, key, got)
		}
	}
	if len(log) != 3 || log[0] != "down" || log[1] != "enter" || log[2] != "/" {
		t.Fatalf("the surface saw %v, want [down enter /] — the tool must forward the key verbatim", log)
	}
}

// TestChatSurface_KeyRefusalCarriesTheSentenceAndTheCounters pins Rule 15 for
// this action: a refused key is refused WITH the reason and the ceiling's state,
// never as a silent no-op the model would read as success.
func TestChatSurface_KeyRefusalCarriesTheSentenceAndTheCounters(t *testing.T) {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{refuseInject: "this run has no line editor"})
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "down"})
	if !strings.Contains(got, "this run has no line editor") {
		t.Fatalf("the refusal lost its reason: %q", got)
	}
	if !strings.Contains(got, "keys used") {
		t.Fatalf("the refusal lost the ceiling's state: %q", got)
	}
	if strings.Contains(got, "injected=") {
		t.Fatalf("a refused key reported as injected: %q", got)
	}
}

// TestChatSurface_KeyExhaustionIsAnnouncedOnTheCallThatSpendsIt is the loud half
// of the bound: the call that spends the LAST key says so, and the announcement
// reaches the OPERATOR's console too — a bound only the model can see is not a
// bound (devdoc/runaway-loop-20260923.md).
func TestChatSurface_KeyExhaustionIsAnnouncedOnTheCallThatSpendsIt(t *testing.T) {
	a := &Agent{}
	a.SetChatSurface(stubChatSurface{injectLast: true})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionKey, "text": "enter"})
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatalf("close the pipe writer: %v", err)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read the console: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close the pipe reader: %v", err)
	}

	if !strings.Contains(got, "CEILING REACHED") {
		t.Fatalf("the answer that spent the last key did not announce the ceiling: %q", got)
	}
	if !strings.Contains(string(body), "ceiling") {
		t.Fatalf("the ceiling was not announced on the operator's console (stderr carried %q): a bound the model alone can see is not one", body)
	}
}
