package agent

// chat_surface_configured_bound_test.go — the CONFIGURED self-driven turn bound.
//
// What this file pins, and why each half would otherwise rot:
//
//  1. THE DEFAULT IS 20, and it is what a run that declares nothing observes —
//     in the refusal, in the tool's report and in the tool DESCRIPTION the model
//     reads (Rule 11: the description is the persuasion surface, so a
//     description that names a stale number is a bug in the tool's only argument).
//  2. THE OPERATOR'S DECLARATION IS HONOURED, through the ONE config resolution
//     (config.ChatSurfaceTurns): this package keeps no second opinion of the
//     number, so the seam's ceiling, the clamp and the description all move
//     together.
//  3. THE MODEL RAISES NOTHING. max_turns clamps DOWN to the configured ceiling,
//     and a bound written past it (by a future writer, not by this tool) is still
//     read through the ceiling — the guard belongs to the READER, not to one
//     caller.
//  4. THE GUARANTEE SURVIVES: a period still ENDS at the ceiling, the refusal
//     names the numbers, and nothing in this tool renews the counter
//     (devdoc/runaway-loop-20260923.md).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// pinTurnsChannels clears the knob's channels and points the settings channel at
// a scratch profile, so a case can be sure which one decided (Rule 32: a test
// pins the environment; it never reads or writes the operator's own settings).
func pinTurnsChannels(t *testing.T) {
	t.Helper()
	t.Setenv(config.ChatSurfaceTurnsEnv, "")
	t.Setenv(config.SettingsPathEnv, filepath.Join(t.TempDir(), "settings.json"))
}

// TestChatSurfaceConfiguredBound_DefaultIsTwenty pins the raised default as the
// number a run OBSERVES, not merely as a constant: the seam's ceiling, the
// tool's description and the max_turns schema must all read 20 when nobody
// declared anything.
func TestChatSurfaceConfiguredBound_DefaultIsTwenty(t *testing.T) {
	pinTurnsChannels(t)

	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})
	if got := a.ChatSurfaceLoopBound(); got != config.ChatSurfaceTurnsDefault {
		t.Fatalf("ChatSurfaceLoopBound() = %d, want the default %d", got, config.ChatSurfaceTurnsDefault)
	}
	if config.ChatSurfaceTurnsDefault != 20 {
		t.Fatalf("the default bound is %d, want 20 (the operator's ruling: 8 was too small)",
			config.ChatSurfaceTurnsDefault)
	}

	def := chatSurfaceToolDef()
	if !strings.Contains(def.Description, fmt.Sprintf("at most %d self-driven turns", config.ChatSurfaceTurnsDefault)) {
		t.Fatalf("the tool description does not name the default bound %d — the model reads a stale number:\n%s",
			config.ChatSurfaceTurnsDefault, def.Description)
	}
	props, _ := def.InputSchema.(map[string]interface{})["properties"].(map[string]interface{})
	maxTurns, _ := props["max_turns"].(map[string]interface{})
	if got, _ := maxTurns["default"].(int); got != config.ChatSurfaceTurnsDefault {
		t.Fatalf("max_turns schema default = %v, want %d", maxTurns["default"], config.ChatSurfaceTurnsDefault)
	}
}

// TestChatSurfaceConfiguredBound_EnvChannelMovesEverything is the acceptance's
// central claim: declaring the bound on the knob's env channel changes BOTH the
// ceiling the seam enforces and the number the model is told.
func TestChatSurfaceConfiguredBound_EnvChannelMovesEverything(t *testing.T) {
	pinTurnsChannels(t)
	t.Setenv(config.ChatSurfaceTurnsEnv, "40")

	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})
	if got := a.ChatSurfaceLoopBound(); got != 40 {
		t.Fatalf("ChatSurfaceLoopBound() = %d with %s=40, want 40", got, config.ChatSurfaceTurnsEnv)
	}
	def := chatSurfaceToolDef()
	if !strings.Contains(def.Description, "at most 40 self-driven turns") {
		t.Fatalf("the description still names another bound with %s=40:\n%s",
			config.ChatSurfaceTurnsEnv, def.Description)
	}
	if !strings.Contains(def.Description, "(2..40)") {
		t.Fatalf("the description does not clamp max_turns to the configured 40:\n%s", def.Description)
	}
}

// TestChatSurfaceConfiguredBound_SettingsChannelIsHonoured pins the lowest
// declared channel end to end, so the settings key is not a documented-but-dead
// rung of the precedence.
func TestChatSurfaceConfiguredBound_SettingsChannelIsHonoured(t *testing.T) {
	pinTurnsChannels(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := "{\"" + config.ChatSurfaceTurnsSettingsKey + "\": \"30\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	t.Setenv(config.SettingsPathEnv, path)

	if got := chatSurfaceConfiguredBound(); got != 30 {
		t.Fatalf("chatSurfaceConfiguredBound() = %d with %s=30 in the settings file, want 30",
			got, config.ChatSurfaceTurnsSettingsKey)
	}
}

// TestChatSurfaceConfiguredBound_ModelCannotRaiseIt pins the asymmetry the whole
// mechanism exists for: max_turns can tighten a period, never lengthen it. It is
// asserted against a CONFIGURED ceiling (40), not the default, because a clamp
// hard-wired to the default would pass the default-only case and fail the run the
// operator actually configured.
func TestChatSurfaceConfiguredBound_ModelCannotRaiseIt(t *testing.T) {
	pinTurnsChannels(t)
	t.Setenv(config.ChatSurfaceTurnsEnv, "40")

	cases := []struct {
		name string
		args map[string]interface{}
		want int
	}{
		{"absent → the configured ceiling", map[string]interface{}{}, 40},
		{"above the ceiling → CLAMPED DOWN", map[string]interface{}{"max_turns": float64(500)}, 40},
		{"the ceiling itself", map[string]interface{}{"max_turns": float64(40)}, 40},
		{"inside the range → itself", map[string]interface{}{"max_turns": float64(7)}, 7},
		{"below the floor → the floor", map[string]interface{}{"max_turns": float64(1)}, chatSurfaceRestartFloor},
		{"negative → the configured ceiling", map[string]interface{}{"max_turns": float64(-2)}, 40},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatSurfaceLoopBoundArgs(c.args); got != c.want {
				t.Fatalf("chatSurfaceLoopBoundArgs(%v) = %d, want %d (configured ceiling 40)",
					c.args, got, c.want)
			}
		})
	}

	// The READER refuses a session bound above the ceiling even if some future
	// writer installs one: the clamp is a property of the ceiling, not of one
	// caller's good behaviour.
	a := &Agent{}
	a.chatSurface.bound = 999
	if got := a.ChatSurfaceLoopBound(); got != 40 {
		t.Fatalf("a session bound of 999 was READ as %d, want the configured ceiling 40", got)
	}
}

// TestChatSurfaceConfiguredBound_PeriodStillEndsAtTheCeiling is the guarantee
// half: with a CONFIGURED bound the period still terminates exactly at it, the
// refusal names both numbers, and a further `type` keeps refusing — the raised
// default buys room, it does not buy an escape.
func TestChatSurfaceConfiguredBound_PeriodStillEndsAtTheCeiling(t *testing.T) {
	pinTurnsChannels(t)
	const configured = 5
	t.Setenv(config.ChatSurfaceTurnsEnv, fmt.Sprintf("%d", configured))

	a := &Agent{}
	a.SetChatSurface(stubChatSurface{})

	for i := 1; i <= configured; i++ {
		got := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": fmt.Sprintf("step %d", i)})
		if !strings.Contains(got, "typed=true") {
			t.Fatalf("type #%d of a configured bound %d was refused: %q", i, configured, got)
		}
		turn := a.ChatSurfaceTakePending()
		if turn.Kind != ChatSurfaceSeamTyped || turn.Used != i || turn.Bound != configured {
			t.Fatalf("turn #%d = %+v, want used=%d bound=%d", i, turn, i, configured)
		}
		if wantClosing := i == configured; turn.Closing != wantClosing {
			t.Fatalf("turn #%d Closing=%v, want %v", i, turn.Closing, wantClosing)
		}
	}

	refused := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "one more"})
	if !strings.Contains(refused, "at its bound (5 of 5 turns used)") {
		t.Fatalf("the refusal after the configured bound does not name the numbers in force: %q", refused)
	}
	// A second attempt refuses too: the wall does not move because the model
	// asked twice.
	if again := a.runChatSurfaceTool(map[string]interface{}{"action": ChatSurfaceActionType, "text": "and again"}); !strings.Contains(again, "at its bound (5 of 5 turns used)") {
		t.Fatalf("the second refusal moved the wall: %q", again)
	}
	if used := a.ChatSurfaceLoopUsed(); used != configured {
		t.Fatalf("used = %d, want the configured bound %d", used, configured)
	}
}

// TestChatSurfaceConfiguredBound_FloorIsNeverUndercut pins the last acceptance
// clause: a declaration BELOW the floor is refused by the ONE resolver (reported
// on stderr, next channel decides), so no path installs a bound that fires before
// the first self-driven turn.
func TestChatSurfaceConfiguredBound_FloorIsNeverUndercut(t *testing.T) {
	pinTurnsChannels(t)
	t.Setenv(config.ChatSurfaceTurnsEnv, "1") // below config.ChatSurfaceTurnsFloor

	got := chatSurfaceConfiguredBound()
	if got < config.ChatSurfaceTurnsFloor {
		t.Fatalf("a declaration of 1 installed a bound of %d, below the floor %d",
			got, config.ChatSurfaceTurnsFloor)
	}
	if got != config.ChatSurfaceTurnsDefault {
		t.Fatalf("an out-of-range declaration resolved to %d, want the default %d to decide",
			got, config.ChatSurfaceTurnsDefault)
	}
}
