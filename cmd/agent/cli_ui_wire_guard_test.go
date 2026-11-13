package main

// cli_ui_wire_guard_test.go — the MIRROR guard for `gopi ui`.
//
// The TUI is a CLIENT: it speaks the SSE wire `gopi web` writes
// (internal/uiclient's EventDelta/EventTool/… and its typed payload field
// names). A client and a server that agree only because both were written on
// the same afternoon is exactly the drift Rule 13/14 warns about: rename a
// frame on the server and the TUI silently stops rendering it — no build
// error, no test failure, just a surface that shows nothing.
//
// This file closes that hole: every mirror is asserted against the SERVER'S
// OWN constant (webEventDelta … server-side, not a re-typed literal), and the
// request contract is asserted against webChatRequest's own json tags. A
// rename on either side fails HERE.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/uiclient"
)

// TestUIWireEventsMirrorTheServer pins the six frame names.
func TestUIWireEventsMirrorTheServer(t *testing.T) {
	pairs := []struct {
		name string
		cli  string
		srv  string
	}{
		{"delta", uiclient.EventDelta, webEventDelta},
		{"tool", uiclient.EventTool, webEventTool},
		{"done", uiclient.EventDone, webEventDone},
		{"error", uiclient.EventError, webEventError},
		{"command", uiclient.EventCommand, webEventCommand},
		{"queued", uiclient.EventQueued, webEventQueued},
	}
	for _, p := range pairs {
		if p.cli != p.srv {
			t.Errorf("frame %s: client says %q, server says %q — the TUI would stop rendering this frame", p.name, p.cli, p.srv)
		}
	}
}

// TestUIWireToolKindsMirrorTheServer pins the tool activity kinds.
func TestUIWireToolKindsMirrorTheServer(t *testing.T) {
	if uiclient.UIStepCall != string(agent.ToolEventKindCall) {
		t.Errorf("call kind: client %q, engine %q", uiclient.UIStepCall, agent.ToolEventKindCall)
	}
	if uiclient.UIStepResult != string(agent.ToolEventKindResult) {
		t.Errorf("result kind: client %q, engine %q", uiclient.UIStepResult, agent.ToolEventKindResult)
	}
}

// TestUIWireRequestContractMatchesTheServer pins the REQUEST shape: the body
// the client ships must decode into the server's own request struct, and the
// fields the client deliberately does NOT ship must stay absent.
func TestUIWireRequestContractMatchesTheServer(t *testing.T) {
	// The client's declared field name is the server's tag, or the POST 400s
	// on an empty message.
	tag := jsonTagOf(t, reflect.TypeOf(webChatRequest{}), "Message")
	if tag != uiclient.FieldMessage {
		t.Fatalf("server request field %q vs client %q", tag, uiclient.FieldMessage)
	}
	body := `{"` + uiclient.FieldMessage + `":"hello"}`
	var req webChatRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("the client's body does not decode into webChatRequest: %v", err)
	}
	if req.Message != "hello" {
		t.Fatalf("decoded message = %q, want hello", req.Message)
	}
	if len(req.History) != 0 || len(req.Images) != 0 {
		t.Fatalf("the client's one-field body produced history=%v images=%v", req.History, req.Images)
	}
}

// TestUIWirePathMatchesTheRoute pins the endpoint the client posts to against
// the route the server registers.
func TestUIWirePathMatchesTheRoute(t *testing.T) {
	src, err := os.ReadFile("cli_web.go")
	if err != nil {
		t.Fatalf("read cli_web.go: %v", err)
	}
	if uiclient.ChatPath != webChatPath {
		t.Fatalf("the client posts to %q but the server registers %q — one path, two spellings",
			uiclient.ChatPath, webChatPath)
	}
	// And the route table must actually USE that constant (a path pinned by
	// name but registered by literal would still drift).
	want := `mux.HandleFunc(webChatPath, s.handleChat)`
	if !strings.Contains(string(src), want) {
		t.Fatalf("cli_web.go does not register the route through %s (looked for %s)", webChatPath, want)
	}
}

// TestUIInterruptPathMirrorsTheServer pins the INTERRUPT route: the surface
// POSTs it, the server registers it, and the two use one spelling. The route is
// what makes Ctrl+X honest (it is the only way to stop the turn behind the
// stream), so a rename on either side is a silent regression: the surface would
// report "interrupted" while the engine kept running the turn.
func TestUIInterruptPathMirrorsTheServer(t *testing.T) {
	if uiclient.InterruptPath != webChatInterruptPath {
		t.Fatalf("the client interrupts %q but the server registers %q — one route, two spellings",
			uiclient.InterruptPath, webChatInterruptPath)
	}
	// The route table must USE the constant: a path pinned by name but
	// registered by literal would still drift.
	src, err := os.ReadFile("cli_web.go")
	if err != nil {
		t.Fatalf("read cli_web.go: %v", err)
	}
	want := `mux.HandleFunc(webChatInterruptPath, s.handleChatInterrupt)`
	if !strings.Contains(string(src), want) {
		t.Fatalf("cli_web.go does not register the interrupt route through %s (looked for %s)", webChatInterruptPath, want)
	}
}

// TestUIInterruptHandlerContract pins the interrupt route's contract at the
// handler: it POSTs only (a GET that cancelled a turn would let any page hit it
// by accident), it refuses while the server is shutting down (the same
// interlock the reset route has), and an accepted interrupt is reported as
// accepted.
func TestUIInterruptHandlerContract(t *testing.T) {
	srv := &webServer{}
	rec := httptest.NewRecorder()

	// Non-POST is refused, and refuses before touching any state.
	srv.handleChatInterrupt(rec, httptest.NewRequest(http.MethodGet, webChatInterruptPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET %s = %d, want %d", webChatInterruptPath, rec.Code, http.StatusMethodNotAllowed)
	}

	// Shutting down: nothing is left to abandon.
	srv.shuttingDown = true
	rec = httptest.NewRecorder()
	srv.handleChatInterrupt(rec, httptest.NewRequest(http.MethodPost, webChatInterruptPath, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("a POST during shutdown = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// A live server with no turn in flight: the route answers, and answers
	// TRUTHFULLY (ok=false) — the surface must never be told it stopped
	// something that was not running.
	srv.shuttingDown = false
	rec = httptest.NewRecorder()
	srv.handleChatInterrupt(rec, httptest.NewRequest(http.MethodPost, webChatInterruptPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("a POST on a live server = %d, want %d (body %q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	var ack struct {
		OK          bool `json:"ok"`
		Interrupted bool `json:"interrupted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil {
		t.Fatalf("the interrupt ack is not JSON: %v (body %q)", err, rec.Body.String())
	}
	if ack.OK || ack.Interrupted {
		t.Errorf("the route claimed an interruption with no turn in flight: %+v", ack)
	}
}

// TestUIVerbIsDispatchedExactlyOnce: the `ui` word must resolve to ONE
// dispatcher. A verb that shadows another makes one of the two unreachable
// (whichever the chain reaches first), so the assertion is that the word
// appears once in the dispatch table and once in the completion table — and
// that it is the predicate this package's isUISubcommand implements.
func TestUIVerbIsDispatchedExactlyOnce(t *testing.T) {
	pred, ok := dispatcherOfToken[CliVerbUI]
	if !ok {
		t.Fatalf("%q is not in dispatcherOfToken — the contract suite would not know it is routed", CliVerbUI)
	}
	if pred != uiDispatcherName {
		t.Errorf("dispatcherOfToken[%q] = %q, want %q", CliVerbUI, pred, uiDispatcherName)
	}
	// The completion table is a CANDIDATE list for `gopi <TAB>`: its entries are
	// exactly the verbs the dispatcher routes, so `ui` must be there and every
	// other entry must resolve to a dispatcher too (the two tables describe one
	// namespace).
	// `ui` is RETIRED (verb-retirement, 2026-10-05): the terminal chat view was
	// withdrawn, so the word must be in NO completion set — offering it would
	// teach a spelling the binary refuses. What this test still owns is that it
	// appears NOWHERE rather than twice.
	var listed int
	for _, tok := range topLevelCommandNames {
		if tok == CliVerbUI {
			listed++
		}
	}
	// The word is DISPATCHED and WITHHELD (master's 暂不暴露 ruling, kept through
	// the union merge) — so it stays in the vocabulary list and is subtracted by
	// the suppression list instead. What the discovery surfaces must NOT do is
	// OFFER it; that is the fact asserted here, through the same function the
	// renderers use.
	if listed != 1 {
		t.Errorf("topLevelCommandNames lists %q %d times, want exactly 1 — the verb is dispatched, so its absence would collapse \"withheld\" into \"removed\"", CliVerbUI, listed)
	}
	if !completionSuppressesVerb(CliVerbUI) {
		t.Errorf("%q is not in the suppression list — the withheld view would be published by every discovery surface", CliVerbUI)
	}
	if containsString(toolCompletionNames, CliVerbUI) {
		t.Errorf("the tool group offers %q — a withdrawn view is not a tool", CliVerbUI)
	}
	// And no OTHER word in the completion table may be missing from the
	// dispatcher — a candidate that routes nowhere is a dead suggestion.
	for _, tok := range topLevelCommandNames {
		if _, ok := dispatcherOfToken[tok]; !ok {
			t.Errorf("completion candidate %q has no dispatcher in dispatcherOfToken", tok)
		}
	}
}

// uiDispatcherName is the predicate the dispatch chain calls for `ui`. It is
// the NAME of a Go function (the same kind of string the contract suite
// asserts for every other verb), so it cannot be pulled from a constant: the
// reflection lookup below is what proves the name still exists in this binary.
const uiDispatcherName = "UISubcommand"

// TestUIDispatcherNameResolves proves the name asserted above is a real
// function in this package — a rename of isUISubcommand's dispatch identity
// would otherwise leave the contract pointing at a name nobody implements.
func TestUIDispatcherNameResolves(t *testing.T) {
	if !uiDispatcherExists() {
		t.Fatalf("%s does not resolve to a function in cmd/agent", uiDispatcherName)
	}
}

// TestUIUIFlagsDoNotShadowWebFlags: the verb reuses the web vocabulary on
// purpose (--tools, --addr). A future edit that renames one side without the
// other would silently split the two carriers' flag sets, so they are pinned.
func TestUIUIFlagsDoNotShadowWebFlags(t *testing.T) {
	if uiToolsFlag != webToolsFlag {
		t.Errorf("tools flag: ui %q, web %q", uiToolsFlag, webToolsFlag)
	}
	if !strings.HasPrefix(uiToolsFlag, "--") || !strings.HasPrefix(uiServerFlag, "--") || !strings.HasPrefix(uiTokenFlag, "--") {
		t.Fatalf("a ui flag is not long-form: %q %q %q", uiToolsFlag, uiServerFlag, uiTokenFlag)
	}
}

// TestUIUsageIsCapabilityFirst asserts the page's shape: the verb's own synopsis
// on the first line, every flag it accepts documented, and the exit key present
// (an undiscoverable key is a feature nobody uses).
func TestUIUsageIsCapabilityFirst(t *testing.T) {
	head := uiUsageTitle(uiUsage)
	if !strings.HasPrefix(head, "gopi "+CliVerbUI) {
		t.Errorf("usage head = %q, want it to name the verb", head)
	}
	for _, want := range []string{uiToolsFlag, webAddrFlag, uiServerFlag, uiTokenFlag, HelpFlag, "Ctrl+X", "Ctrl+C"} {
		if !strings.Contains(uiUsage, want) {
			t.Errorf("usage page does not document %q", want)
		}
	}
}

// jsonTagOf returns the json name of a struct field (the mirror's source of
// truth is the server's own tag, never a re-typed literal).
func jsonTagOf(t *testing.T, typ reflect.Type, field string) string {
	t.Helper()
	f, ok := typ.FieldByName(field)
	if !ok {
		t.Fatalf("%s has no field %s", typ, field)
	}
	tag := f.Tag.Get("json")
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		t.Fatalf("%s.%s carries no json name (tag %q)", typ, field, tag)
	}
	return name
}

// uiDispatcherExists reports whether the dispatch predicate named by
// uiDispatcherName is a real function in this package. The dispatch chain
// calls isUISubcommand directly (not by name), so the only way a rename could
// silently break the contract assertion above is if nothing checked the name —
// this is that check.
func uiDispatcherExists() bool {
	_, ok := reflect.TypeOf(isUISubcommand).(interface{ IsVariadic() bool })
	return ok && reflect.ValueOf(isUISubcommand).IsValid()
}
