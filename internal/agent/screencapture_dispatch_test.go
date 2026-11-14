// screencapture_dispatch_test.go — the screen_capture dispatch arm: name
// resolution, argument plumbing, error contract, and the def↔arm wiring.
//
// The live engine itself (internal/tools/screencapture.go, the hand-rolled
// zero-dependency X11 GetImage client) has its own tests and needs a display;
// these tests drive the DISPATCHER through the screenCaptureEngine seam so the
// wiring the epic added — arg names, the JSON result handed to the model, and
// the error prefix the dead-end detector keys on — is pinned without X.
// The real end-to-end turn (an actual capture producing a PNG + sha256_16)
// is Rule 11 evidence and lives in the session log, not in a unit test.
package agent

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/toolnames"
	"github.com/nickhuang99/gopi/internal/tools"
)

// screenCaptureStubResult is the shape the real engine returns (JSON with the
// capture's ground truth). Kept as a literal mirror of the engine's contract so
// a change in that contract shows up here as a test failure.
const screenCaptureStubResult = `{"path":"stub.png","display":":99","width":1024,"height":768,"sha256_16":"deadbeefdeadbeef"}`

// withScreenCaptureEngine swaps the dispatch seam for the duration of the test
// and restores the production binding (the CLI-shared tools.ScreenCapture).
func withScreenCaptureEngine(t *testing.T, fn func(display, out string) (string, error)) {
	t.Helper()
	prev := screenCaptureEngine
	screenCaptureEngine = fn
	t.Cleanup(func() { screenCaptureEngine = prev })
}

// TestScreenCaptureDispatch_PlumbsArgsAndReturnsEngineResult pins the arm's
// contract: the two model-supplied arguments reach the engine by their wire
// names (display / out — the same names the input schema advertises), the
// engine's JSON result is delivered verbatim, and the tool name resolves.
func TestScreenCaptureDispatch_PlumbsArgsAndReturnsEngineResult(t *testing.T) {
	var gotDisplay, gotOut string
	calls := 0
	withScreenCaptureEngine(t, func(display, out string) (string, error) {
		calls++
		gotDisplay, gotOut = display, out
		return screenCaptureStubResult, nil
	})

	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	out := ag.executeToolDefault(deepseek.ToolCall{
		ID:   "call_scr1",
		Name: toolnames.ToolScreenCapture,
		Arguments: map[string]interface{}{
			"display": ":99",
			"out":     "/some/where/eye.png",
		},
	})

	if calls != 1 {
		t.Fatalf("engine called %d times, want exactly 1", calls)
	}
	if gotDisplay != ":99" || gotOut != "/some/where/eye.png" {
		t.Errorf("engine got (display=%q, out=%q), want (\":99\", \"/some/where/eye.png\")", gotDisplay, gotOut)
	}
	if out != screenCaptureStubResult {
		t.Errorf("result = %q, want the engine's result verbatim %q", out, screenCaptureStubResult)
	}
	// The model must be able to parse the ground truth out of the result.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("result is not valid JSON for the model: %v (%q)", err, out)
	}
	if parsed["sha256_16"] != "deadbeefdeadbeef" {
		t.Errorf("sha256_16 missing from the model-visible result: %v", parsed)
	}
	if strings.Contains(out, "Unknown tool") {
		t.Errorf("the tool name resolved to the default arm: %s", out)
	}
}

// TestScreenCaptureDispatch_BothArgsOptional pins the schema's contract that
// BOTH arguments are optional (display → $DISPLAY; out → an ABSOLUTE path in the
// capture class of gopi's scratch tree, resolved by the engine — never the cwd,
// which for a task run is its clone ground, Rule 30): an omitted argument arrives
// as an empty string, and the call still reaches the engine — validateToolArgs
// must not reject it.
func TestScreenCaptureDispatch_BothArgsOptional(t *testing.T) {
	var gotDisplay, gotOut string
	withScreenCaptureEngine(t, func(display, out string) (string, error) {
		gotDisplay, gotOut = display, out
		return screenCaptureStubResult, nil
	})

	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	// No arguments at all (the JSON decoder leaves the map empty/nil).
	out := ag.executeToolDefault(deepseek.ToolCall{
		ID:        "call_scr2",
		Name:      toolnames.ToolScreenCapture,
		Arguments: map[string]interface{}{},
	})
	if gotDisplay != "" || gotOut != "" {
		t.Errorf("omitted args reached the engine as (%q, %q), want empty strings", gotDisplay, gotOut)
	}
	if out != screenCaptureStubResult {
		t.Errorf("optional-arg call did not reach the engine: %q", out)
	}
	if msg := validateToolArgs(toolnames.ToolScreenCapture, map[string]interface{}{}); msg != "" {
		t.Errorf("validateToolArgs rejected an argument-less screen_capture call: %q", msg)
	}
	// The JSON result (not "Missing required parameter:") is what the model
	// gets — a validation arm here would make the tool uncallable with defaults.
	if strings.HasPrefix(out, "Missing required parameter:") {
		t.Errorf("screen_capture demands a parameter its schema declares optional: %s", out)
	}
}

// TestScreenCaptureDispatch_ErrorContract pins the failure shape: an engine
// error becomes the tool's own error prefix (never a silent success), and that
// prefix is registered in toolErrorMarkers so the dead-end detector fires on it
// (Rule 15: no silent failure).
func TestScreenCaptureDispatch_ErrorContract(t *testing.T) {
	withScreenCaptureEngine(t, func(display, out string) (string, error) {
		return "", errors.New("display :99: dial unix: no such file")
	})

	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	out := ag.executeToolDefault(deepseek.ToolCall{
		ID:        "call_scr3",
		Name:      toolnames.ToolScreenCapture,
		Arguments: map[string]interface{}{"display": ":99"},
	})
	if !strings.HasPrefix(out, screenCaptureErrorPrefix) {
		t.Fatalf("error result = %q, want prefix %q", out, screenCaptureErrorPrefix)
	}
	if !strings.Contains(out, "no such file") {
		t.Errorf("error result dropped the engine's cause: %q", out)
	}
	registered := false
	for _, m := range toolErrorMarkers {
		if m == screenCaptureErrorPrefix {
			registered = true
		}
	}
	if !registered {
		t.Errorf("%q is not in toolErrorMarkers — a failed capture would not be seen as a dead end", screenCaptureErrorPrefix)
	}
}

// TestScreenCaptureDispatch_EngineIsTheCLISharedOne is the anti-drift guard: the
// seam's PRODUCTION binding must be tools.ScreenCapture — the same engine the
// `gopi tool screen_capture` CLI verb calls — so the model path and the human path
// cannot diverge (the two-faces-one-engine contract, Rule 14's spirit applied to
// a behaviour rather than a constant). Compared by function pointer, so the
// assertion is exact and needs no display.
func TestScreenCaptureDispatch_EngineIsTheCLISharedOne(t *testing.T) {
	got := reflect.ValueOf(screenCaptureEngine).Pointer()
	want := reflect.ValueOf(tools.ScreenCapture).Pointer()
	if got != want {
		t.Fatalf("screenCaptureEngine is not bound to tools.ScreenCapture (the CLI-shared engine) — "+
			"the model path and the `gopi %s` path would drift apart", toolnames.ToolScreenCapture)
	}
}

// TestScreenCaptureDeclarationsStayBelowDocAnchors USED TO LIVE HERE, and its
// subject is gone with the corpus. It was a maintenance guard: the two
// screen_capture declarations sit at the TAIL of loop.go because
// devdoc/hippocampus.md and devdoc/agent-five-organs-ontology.md carried
// `loop.go:NNN` anchors that a statement inserted above them would shift. Those
// anchors, the gate that resolved them and the manifest they were read through
// were internal/docclass and its corpus — deleted with the document pipeline
// (removal slice 3/5). Nothing measures a
// `loop.go:NNN` anchor any more, so the guard's premise ("moving this up turns
// the anchor gate red") is no longer true and keeping it would assert a
// constraint that nothing enforces. The declarations stay where they are;
// nothing requires it.

// withScreenCaptureModeEngine swaps the RECT-mode seam (the one the self /
// window / area modes reach) for the duration of a test.
func withScreenCaptureModeEngine(t *testing.T, fn func(tools.ScreenCaptureOptions) (string, error)) {
	t.Helper()
	prev := screenCaptureModeEngine
	screenCaptureModeEngine = fn
	t.Cleanup(func() { screenCaptureModeEngine = prev })
}

// TestScreenCaptureDispatch_SelfArgSelectsTheSelfMode pins the model path's new
// argument: `self: true` must reach the RECT-mode seam with mode "self" — NOT
// the active-window mode (a model asking "where am I running" must never be
// answered with "what the user last clicked"), and not the plain whole-screen
// engine either.
func TestScreenCaptureDispatch_SelfArgSelectsTheSelfMode(t *testing.T) {
	wholeCalled := 0
	withScreenCaptureEngine(t, func(display, out string) (string, error) {
		wholeCalled++
		return screenCaptureStubResult, nil
	})
	var got tools.ScreenCaptureOptions
	modeCalled := 0
	withScreenCaptureModeEngine(t, func(opts tools.ScreenCaptureOptions) (string, error) {
		modeCalled++
		got = opts
		return screenCaptureStubResult, nil
	})

	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	out := ag.executeToolDefault(deepseek.ToolCall{
		ID:        "call_self1",
		Name:      toolnames.ToolScreenCapture,
		Arguments: map[string]interface{}{"self": true, "out": "/tmp/self.png"},
	})
	if wholeCalled != 0 {
		t.Fatalf("`self` was routed to the whole-screen engine %d time(s) — the mode never saw it", wholeCalled)
	}
	if modeCalled != 1 {
		t.Fatalf("the RECT-mode seam was called %d times, want 1", modeCalled)
	}
	if got.Mode != tools.ScreenCaptureModeSelf {
		t.Fatalf("mode = %q, want %q — `self` must not degrade to the active window", got.Mode, tools.ScreenCaptureModeSelf)
	}
	if got.Decorations {
		t.Errorf("Decorations = true without the flag being set")
	}
	if out != screenCaptureStubResult {
		t.Errorf("result = %q, want the engine's result verbatim", out)
	}
}

// TestScreenCaptureDispatch_DecorationsRidesWithSelf pins that the decorations
// flag reaches the engine WITH the self mode (and is what the schema promises:
// a frame instead of the client area).
func TestScreenCaptureDispatch_DecorationsRidesWithSelf(t *testing.T) {
	var got tools.ScreenCaptureOptions
	withScreenCaptureModeEngine(t, func(opts tools.ScreenCaptureOptions) (string, error) {
		got = opts
		return screenCaptureStubResult, nil
	})

	ag := &Agent{Ctx: &Context{}}
	ag.Ctx.AddUserMessage("epic")
	ag.executeToolDefault(deepseek.ToolCall{
		ID:        "call_self2",
		Name:      toolnames.ToolScreenCapture,
		Arguments: map[string]interface{}{"self": true, "decorations": true},
	})
	if got.Mode != tools.ScreenCaptureModeSelf || !got.Decorations {
		t.Fatalf("engine got mode=%q decorations=%v, want self with decorations", got.Mode, got.Decorations)
	}
}

// TestScreenCaptureDispatch_SelfSchemaIsAdvertised pins the schema half of the
// wiring: both arguments are declared, so the model can see them at all
// (`validateToolArgs` must also keep accepting them).
func TestScreenCaptureDispatch_SelfSchemaIsAdvertised(t *testing.T) {
	captureDef := ""
	for _, d := range claudeToolDefs {
		if d.Name != toolnames.ToolScreenCapture {
			continue
		}
		captureDef = d.Description
		schema, ok := d.InputSchema.(map[string]interface{})
		if !ok {
			t.Fatalf("the screen_capture input schema is %T, want a properties map", d.InputSchema)
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("the screen_capture schema has no properties map")
		}
		for _, name := range []string{"self", "decorations", "window", "area"} {
			if _, ok := props[name]; !ok {
				t.Errorf("the screen_capture schema does not declare %q", name)
			}
		}
	}
	if captureDef == "" {
		t.Fatal("screen_capture is not in claudeToolDefs")
	}
	if !strings.Contains(captureDef, "_NET_WM_PID") && !strings.Contains(captureDef, "HOSTS") {
		t.Errorf("the tool description does not distinguish `self` from `window`: %q", captureDef)
	}
	if msg := validateToolArgs(toolnames.ToolScreenCapture, map[string]interface{}{"self": true, "decorations": true}); msg != "" {
		t.Errorf("validateToolArgs rejected self/decorations: %q", msg)
	}
}
