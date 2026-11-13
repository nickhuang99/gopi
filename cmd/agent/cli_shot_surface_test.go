package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/toolnames"
)

// TestShotUsageShowsSurfaces — the subcommand's own -h must advertise the three
// surfaces and the flags that configure them, or the capability exists only for
// someone who reads the source (Rule 11: the surface is discoverable, not just
// implemented).
func TestShotUsageShowsSurfaces(t *testing.T) {
	got := shotUsageHeader()
	for _, want := range []string{
		"--surface NAME",
		shot.SurfaceWeb, shot.SurfaceChat, shot.SurfaceScreen,
		"--cols N", "--rows N", "--display NAME", "--scrollback",
		"gopi tool shot --self [--addr HOST:PORT] [--out PATH] [flags]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shot usage is missing %q", want)
		}
	}
}

// TestShotUsageArgsMatchTemplate pins the template/argument arity: a usage
// template with more verbs than shotUsageArgs supplies renders a literal
// "%!(EXTRA …)", and one with fewer renders "%!d(MISSING)". Both land in the
// user-visible help, so the count is asserted where it is defined.
func TestShotUsageArgsMatchTemplate(t *testing.T) {
	rendered := shotUsageText()
	if strings.Contains(rendered, "%!") {
		t.Fatalf("the usage template did not render cleanly — an argument is missing or extra:\n%s", rendered)
	}
	// The named defaults must actually be PRINTED (a template that silently
	// dropped a slot would still render cleanly).
	for _, want := range []string{
		fmt.Sprintf("Default %dx%d", shot.DefaultViewportWidth, shot.DefaultViewportHeight),
		fmt.Sprintf("Default %s", shot.DefaultSurface),
		fmt.Sprintf("Default %dx%d", shotTextColsDefault, shotTextRowsDefault),
		shot.ChromeMemMaxEnvName,
		fmt.Sprintf("default %d", shot.DefaultChromeMemoryMaxMB),
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered usage is missing %q", want)
		}
	}
}

// TestParseShotSurfaceFlags covers the surface selection: the default is the web
// SPA (so nothing changes for an existing invocation), chat/screen stand alone
// without --url or --self, the cell geometry parses, and a bad value is refused
// naming the accepted set.
func TestParseShotSurfaceFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
		check   func(t *testing.T, o shot.Options)
	}{
		{
			name: "default surface is web",
			args: []string{"--self"},
			check: func(t *testing.T, o shot.Options) {
				if o.Surface != shot.SurfaceWeb {
					t.Errorf("surface = %q, want %q", o.Surface, shot.SurfaceWeb)
				}
			},
		},
		{
			name: "chat needs neither url nor self",
			args: []string{"--surface", "chat"},
			check: func(t *testing.T, o shot.Options) {
				if o.Surface != shot.SurfaceChat || o.Self {
					t.Errorf("chat parse: %+v", o)
				}
				if o.Text.Cols != shotTextColsDefault || o.Text.Rows != shotTextRowsDefault {
					t.Errorf("default geometry = %dx%d, want %dx%d", o.Text.Cols, o.Text.Rows, shotTextColsDefault, shotTextRowsDefault)
				}
				if o.Text.Ready != chatPromptMark {
					t.Errorf("ready marker = %q, want the prompt %q", o.Text.Ready, chatPromptMark)
				}
				if o.Text.ExitKeys != shotChatExitVerb {
					t.Errorf("exit verb = %q, want %q", o.Text.ExitKeys, shotChatExitVerb)
				}
			},
		},
		{
			name: "chat geometry and scrollback",
			args: []string{"--surface", "chat", "--cols", "132", "--rows", "48", "--scrollback"},
			check: func(t *testing.T, o shot.Options) {
				if o.Text.Cols != 132 || o.Text.Rows != 48 {
					t.Errorf("geometry = %dx%d, want 132x48", o.Text.Cols, o.Text.Rows)
				}
				if !o.Text.Scrollback {
					t.Error("--scrollback did not reach the options")
				}
			},
		},
		{
			name: "screen needs neither url nor self",
			args: []string{"--surface", "screen", "--display", ":99"},
			check: func(t *testing.T, o shot.Options) {
				if o.Surface != shot.SurfaceScreen || o.Display != ":99" {
					t.Errorf("screen parse: %+v", o)
				}
			},
		},
		{
			name: "surface is case-insensitive",
			args: []string{"--surface", "CHAT"},
			check: func(t *testing.T, o shot.Options) {
				if o.Surface != shot.SurfaceChat {
					t.Errorf("surface = %q, want %q", o.Surface, shot.SurfaceChat)
				}
			},
		},
		{
			name:    "unknown surface names the accepted set",
			args:    []string{"--surface", "bogus"},
			wantErr: "web|chat|screen",
		},
		{
			name:    "bad cols",
			args:    []string{"--surface", "chat", "--cols", "0"},
			wantErr: "--cols",
		},
		{
			name:    "bad rows",
			args:    []string{"--surface", "chat", "--rows", "wide"},
			wantErr: "--rows",
		},
		{
			name:    "web still needs a target",
			args:    []string{"--surface", "web"},
			wantErr: "nothing to shoot",
		},
		{
			name:    "url and self stay exclusive",
			args:    []string{"--self", "--url", "example.com"},
			wantErr: "mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseShotArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parse %v: want an error mentioning %q, got none", tc.args, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse %v: %v", tc.args, err)
			}
			tc.check(t, opts)
		})
	}
}

// TestShotSurfaceValuesExistInShotPackage is the anti-drift gate for the surface
// names (Rules 1b/13): each value is defined ONCE (internal/shot/surface.go) and
// every other file REFERENCES the constant. The scan is limited to the files
// whose subject IS the surface — cmd/agent/cli_shot.go and internal/shot's
// production files — because "chat" is also the CLI VERB (CliVerbChat) and
// "screen"/"web" name other things entirely; a package-wide scan would flag
// those legitimate uses and be turned off.
func TestShotSurfaceValuesExistInShotPackage(t *testing.T) {
	targets := []string{"cli_shot.go"}
	for _, surface := range shot.Surfaces() {
		needle := `"` + surface + `"`
		for _, f := range targets {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			if strings.Contains(string(data), needle) {
				t.Errorf("%s carries the surface literal %s — reference the shot.Surface* constant (defined in internal/shot/surface.go)", f, needle)
			}
		}
	}
	// The engine's own package must reference its constants in every file but
	// the one that defines them. A line may carry the literal with an explicit
	// reviewed escape (`// surface-literal-ok: <reason>`) when the literal names
	// something else — the repo's own baseline/escape idiom, so an exemption is
	// visible rather than implied by a silenced file.
	for _, f := range productionGoFilesIn(t, filepath.Join("..", "..", "internal", "shot")) {
		if filepath.Base(f) == "surface.go" {
			continue // the single definition site
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, surface := range shot.Surfaces() {
				if !strings.Contains(line, `"`+surface+`"`) {
					continue
				}
				if strings.Contains(line, surfaceLiteralEscape) {
					continue // reviewed: the literal names something other than a surface
				}
				t.Errorf("%s:%d carries the surface literal %q — reference the constant in surface.go, or mark the line %s: <reason>",
					f, i+1, surface, surfaceLiteralEscape)
			}
		}
	}
}

// surfaceLiteralEscape is the reviewed-exemption marker for the surface-literal
// gate: a line that carries one of the surface words while naming something else
// (the CLI VERB "chat", say) marks itself with this comment and a reason.
const surfaceLiteralEscape = "surface-literal-ok:"

// productionGoFilesIn lists this directory's non-test .go files.
func productionGoFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	if len(out) == 0 {
		t.Fatal("no production .go files found — the gate would pass vacuously")
	}
	return out
}

// TestChatPromptMarkIsTheRenderableForm — the marker the capture waits for is
// the prompt WITHOUT its trailing blank, because the renderer trims a row's
// trailing cells and positions the cursor with a CSI. Waiting for the padded
// spelling times out on a healthy UI (the defect this pins was observed live:
// "screen never showed \"you> \"" with 457 bytes received).
func TestChatPromptMarkIsTheRenderableForm(t *testing.T) {
	if strings.HasSuffix(chatPromptMark, " ") {
		t.Fatalf("ready marker %q keeps the trailing blank the renderer trims", chatPromptMark)
	}
	if !strings.HasPrefix(chatPrompt, chatPromptMark) {
		t.Fatalf("marker %q is not the prompt %q minus its trailing blanks", chatPromptMark, chatPrompt)
	}
	if chatPromptMark != strings.TrimRight(chatPrompt, " ") {
		t.Fatalf("marker %q must be the prompt with its trailing blanks trimmed", chatPromptMark)
	}
}

// TestShotChatExitVerbIsTheCLIsQuitVerb — the verb the capture sends to end the
// chat TUI comes from cmd/agent's own command table, never a second spelling.
func TestShotChatExitVerbIsTheCLIQuitVerb(t *testing.T) {
	want := "/" + quitCommandName
	if shotChatExitVerb != want {
		t.Fatalf("exit verb = %q, want the CLI's own %q", shotChatExitVerb, want)
	}
}

// TestShotTextDefaultIsTheChatVerb — the engine's default TUI is `gopi chat`,
// and the verb it names must be the CLI's CliVerbChat (internal/shot cannot
// import package main, so this test is where the two spellings are held
// together).
func TestShotTextDefaultIsTheChatVerb(t *testing.T) {
	argv := shot.DefaultTUIArgv("/x/gopi")
	if len(argv) != 2 || argv[1] != CliVerbChat {
		t.Fatalf("DefaultTUIArgv = %v, want [gopi %s]", argv, CliVerbChat)
	}
}

// TestShotCommandIsRegistered — the verb and the LLM-facing tool name stay in
// step with the CLI (the subcommand is reachable, and toolnames still names the
// capture tool the docs point at).
func TestShotCommandIsRegistered(t *testing.T) {
	if CliVerbShot != "shot" {
		t.Fatalf("CliVerbShot = %q", CliVerbShot)
	}
	if toolnames.ToolScreenCapture == "" {
		t.Fatal("toolnames.ToolScreenCapture is empty")
	}
	help := shotUsageText()
	if !strings.Contains(help, CliVerbShot) {
		t.Fatalf("the usage text does not mention the %q verb", CliVerbShot)
	}
}
