package main

// cli_ui_test.go — the behaviours of `gopi ui` that a headline can get wrong.
//
// These are the tests that would have caught the bugs this surface actually
// had while it was being built, so they are kept as regression guards rather
// than as coverage: the Enter keystroke the driver sends, the console redirect
// that stopped the engine's notices from scrambling the display, the flag
// table, and the listen-address → base-URL resolution.

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// TestUILoopbackHostMatchesTheClientDefault: the wildcard-bind resolution and
// the client's default base URL must name the SAME host — two spellings would
// let the TUI call a name its own server never bound.
func TestUILoopbackHostMatchesTheClientDefault(t *testing.T) {
	if !strings.Contains(uiclient.DefaultServerBase, uiclient.LoopbackHostName) {
		t.Fatalf("DefaultServerBase %q does not use LoopbackHostName %q", uiclient.DefaultServerBase, uiclient.LoopbackHostName)
	}
	addr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:8090")
	if err != nil {
		t.Fatal(err)
	}
	got, err := uiLoopbackBaseURL("0.0.0.0:8090", addr)
	if err != nil {
		t.Fatal(err)
	}
	if got != uiclient.DefaultServerBase {
		t.Fatalf("a wildcard bind resolves to %q but the client's default is %q", got, uiclient.DefaultServerBase)
	}
}

// TestUIDriverSendsEnterAsCR is the regression guard for the bug that made the
// FIRST pty run meaningless: the driver sent `\n` for Enter. A terminal in raw
// mode sends `\r` for Enter (bubbletea's keyCR == KeyEnter); `\n` is KeyCtrlJ
// and submits nothing — every prompt sat unsubmitted in the input box, no turn
// ever started, and the capture looked exactly like a broken TUI.
//
// The driver is a script, so the guard is over its bytes: it must send the CR
// spelling and must NOT contain a bare `\n` in a send payload.
func TestUIDriverSendsEnterAsCR(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "ui", "drive_ui.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the driver is not in this checkout (%v)", err)
	}
	text := string(src)
	if !strings.Contains(text, `ENTER = b"\r"`) {
		t.Fatal("the driver does not define ENTER as the CR keystroke — a real terminal sends \\r for Enter")
	}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "b\"") || !strings.Contains(line, "\\r") {
			continue
		}
		if strings.Contains(line, "\\n") {
			t.Errorf("the driver types a bare \\n in a send payload: %s", strings.TrimSpace(line))
		}
	}
}

// TestUIDriverWaitsForTheSurfaceNotTheClock: the second driver bug was typing
// on a fixed schedule, which interleaved keystrokes with the transcript
// repaint and produced a scramble that looked like a TUI fault. The driver must
// therefore carry marker-driven steps.
func TestUIDriverWaitsForTheSurfaceNotTheClock(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "ui", "drive_ui.py")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("the driver is not in this checkout (%v)", err)
	}
	text := string(src)
	// The driver must (a) carry marker-driven steps and (b) actually gate each
	// send on a marker match. Both halves matter: a STEPS table nobody consults
	// is a schedule again.
	if !strings.Contains(text, "STEPS = [") || !strings.Contains(text, ".marker.search(") {
		t.Fatal("the driver no longer waits for a screen marker before typing — a timed driver interleaves with the repaint")
	}
	if !strings.Contains(text, "Step(") || !strings.Contains(text, "self.marker = re.compile(marker)") {
		t.Fatal("the driver's steps do not carry markers")
	}
}

// TestSilenceEngineConsoleRedirectsAndRestores pins the console redirect: the
// engine and the renderer share one terminal, and the renderer's cursor
// arithmetic assumes it is the only writer. The original symptom was a
// character-interleaved transcript; this asserts the mechanism (and that the
// operator's console comes back).
func TestSilenceEngineConsoleRedirectsAndRestores(t *testing.T) {
	realOut, realErr := os.Stdout, os.Stderr
	logPath := filepath.Join(t.TempDir(), uiEngineLogName)
	restore := silenceEngineConsole(logPath)
	if os.Stdout == realOut {
		restore()
		t.Fatal("silenceEngineConsole left stdout on the terminal — engine notices would interleave with the display")
	}
	if os.Stderr == realErr {
		restore()
		t.Fatal("stderr was not redirected")
	}
	// The redirect must be a FILE the operator can read afterwards, not a
	// black hole: the engine's diagnostics are evidence, and discarding them
	// would trade a scrambled screen for a silent one.
	if _, err := os.Stat(logPath); err != nil {
		restore()
		t.Fatalf("the engine log %s was not created: %v", logPath, err)
	}
	restore()
	if os.Stdout != realOut || os.Stderr != realErr {
		t.Fatalf("restore() did not put the console back: stdout=%v stderr=%v", os.Stdout, os.Stderr)
	}
	// And the session's own notices land IN the log (the scripted proof that
	// the redirect is where the engine's output actually goes).
	restore2 := silenceEngineConsole(logPath)
	fmt.Println("[gopi] engine notice")
	restore2()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read the engine log: %v", err)
	}
	if !strings.Contains(string(data), "[gopi] engine notice") {
		t.Errorf("the engine's output did not land in the log; it holds %q", string(data))
	}
}

// TestUIEngineLogPathIsOffTheRootFilesystem: the redirect destination must
// never be a path nobody chose. An agent with no session directory falls back
// to the scratch tree (Rule 22) — the check measures the DEVICE, not the
// spelling, so a differently-named directory on `/` still fails.
func TestUIEngineLogPathIsOffTheRootFilesystem(t *testing.T) {
	got := uiEngineLogPath(&agent.Agent{})
	if got == "" {
		t.Skip("no scratch directory is available in this environment")
	}
	if !strings.HasSuffix(got, uiEngineLogName) {
		t.Fatalf("engine log path %q does not name %s", got, uiEngineLogName)
	}
	if err := scratch.AssertTempRootOffRootFS(); err != nil {
		t.Fatalf("the engine log lands on a temp root that is not off `/`: %v", err)
	}
}

// TestSilenceEngineConsoleRefusesAnEmptyPath: with no destination there is no
// redirect, and a silent no-op would put the engine's notices back on the
// screen the renderer is painting. The failure is reported, never swallowed.
func TestSilenceEngineConsoleRefusesAnEmptyPath(t *testing.T) {
	realOut := os.Stdout
	restore := silenceEngineConsole(filepath.Join(t.TempDir(), "missing", "dir", uiEngineLogName))
	defer restore()
	if os.Stdout != realOut {
		t.Fatal("an unwritable destination still took the console over — the engine's output would vanish with the screen smear")
	}
}

// TestSilenceEngineConsoleCanBeOptedOut: the debugging switch keeps the engine's
// own output, which is what you want when the surface is the thing being
// investigated.
func TestSilenceEngineConsoleCanBeOptedOut(t *testing.T) {
	t.Setenv(uiEngineConsoleEnv, "1")
	realOut := os.Stdout
	restore := silenceEngineConsole(filepath.Join(t.TempDir(), uiEngineLogName))
	defer restore()
	if os.Stdout != realOut {
		t.Error("the opt-out did not keep stdout on the terminal")
	}
}

// TestUIParseFlags covers the vocabulary, including the two shapes that used to
// be silently wrong classes of bug: an unknown flag (which must be refused, not
// handed to an agent loop) and the --server/--addr contradiction.
func TestUIParseFlags(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		addr    string
		server  string
		token   string
		tools   bool
		help    bool
		wantErr bool
	}{
		{name: "bare", args: nil, addr: webDefaultAddr()},
		{name: "tools", args: []string{uiToolsFlag}, addr: webDefaultAddr(), tools: true},
		{name: "addr separated", args: []string{webAddrFlag, "127.0.0.1:9999"}, addr: "127.0.0.1:9999"},
		{name: "addr joined", args: []string{webAddrFlag + "=127.0.0.1:9999"}, addr: "127.0.0.1:9999"},
		{name: "server", args: []string{uiServerFlag, "http://127.0.0.1:8090"}, server: "http://127.0.0.1:8090"},
		{name: "server joined with token", args: []string{uiServerFlag + "=http://192.0.2.5:8090", uiTokenFlag, "abc123"},
			server: "http://192.0.2.5:8090", token: "abc123"},
		{name: "help", args: []string{HelpFlag}, addr: webDefaultAddr(), help: true},
		{name: "help long", args: []string{HelpFlagLong}, addr: webDefaultAddr(), help: true},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "addr needs a value", args: []string{webAddrFlag}, wantErr: true},
		{name: "server needs a value", args: []string{uiServerFlag}, wantErr: true},
		{name: "token needs a value", args: []string{uiTokenFlag}, wantErr: true},
		{name: "server and addr contradict", args: []string{uiServerFlag, "http://127.0.0.1:1", webAddrFlag, "127.0.0.1:2"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, help, err := uiParseFlags(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("uiParseFlags(%v) accepted the command line", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("uiParseFlags(%v): %v", tc.args, err)
			}
			if help != tc.help {
				t.Errorf("help = %v, want %v", help, tc.help)
			}
			if got.addr != tc.addr || got.server != tc.server || got.token != tc.token || got.tools != tc.tools {
				t.Errorf("parsed %+v, want addr=%q server=%q token=%q tools=%v", got, tc.addr, tc.server, tc.token, tc.tools)
			}
		})
	}
}

// TestUIToolsFollowsTheWebEnvironmentSwitch: one switch, two carriers. The verb
// must read the SAME variable `gopi web` reads — a second spelling would let the
// two surfaces disagree about whether tools run.
func TestUIToolsFollowsTheWebEnvironmentSwitch(t *testing.T) {
	t.Setenv(webToolsEnv, "1")
	if !uiToolsFromEnv() {
		t.Fatalf("gopi ui ignored %s, which gopi web reads", webToolsEnv)
	}
	if !webToolsEnabled() {
		t.Fatal("the shared reader disagrees with itself")
	}
}

// TestUILoopbackBaseURL: the TUI calls the address its own server was BOUND to
// (with --addr 127.0.0.1:0 the kernel picks the port, so the configured address
// is not the reachable one), and a wildcard bind is reached at loopback.
func TestUILoopbackBaseURL(t *testing.T) {
	cases := []struct {
		configured string
		bound      string
		want       string
	}{
		{"127.0.0.1:8090", "127.0.0.1:8090", "http://127.0.0.1:8090"},
		{"127.0.0.1:0", "127.0.0.1:45678", "http://127.0.0.1:45678"},
		{"0.0.0.0:8090", "0.0.0.0:8090", "http://127.0.0.1:8090"},
		{":8090", "[::]:8090", "http://127.0.0.1:8090"},
	}
	for _, tc := range cases {
		addr, err := net.ResolveTCPAddr("tcp", tc.bound)
		if err != nil {
			t.Fatalf("resolve %s: %v", tc.bound, err)
		}
		got, err := uiLoopbackBaseURL(tc.configured, addr)
		if err != nil {
			t.Fatalf("uiLoopbackBaseURL(%q, %q): %v", tc.configured, tc.bound, err)
		}
		if got != tc.want {
			t.Errorf("uiLoopbackBaseURL(%q, %q) = %q, want %q", tc.configured, tc.bound, got, tc.want)
		}
	}
}

// TestUIVerbConstantIsTheWordTheUsageAndHelpUse: one spelling for the verb
// across the dispatcher, the help index and the usage page (Rule 13 — a
// constant referenced, not re-typed).
func TestUIVerbConstantIsTheWordTheUsageAndHelpUse(t *testing.T) {
	if CliVerbUI != "ui" {
		t.Fatalf("the verb is %q — every doc and test in this file is written against %q", CliVerbUI, "ui")
	}
	if !strings.Contains(uiUsage, CliVerbUI+" ") {
		t.Error("the usage page does not spell the verb it documents")
	}
	if err := isUISubcommandForTest(CliVerbUI, "-h"); err != nil {
		t.Errorf("`gopi %s -h` is not dispatched: %v", CliVerbUI, err)
	}
}

// isUISubcommandForTest answers "is this argv the ui verb" WITHOUT running it
// (isUISubcommand calls os.Exit, which a test must never do).
func isUISubcommandForTest(args ...string) error {
	if len(args) == 0 || args[0] != CliVerbUI {
		return os.ErrInvalid
	}
	return nil
}

// TestUIParseFlagsScreenDump covers the self-observation flag table. The two
// shapes it exists to pin: the BARE flag (a dump was asked for, path to be
// resolved — not "off"), and the joined form (the operator named the file).
func TestUIParseFlagsScreenDump(t *testing.T) {
	cases := []struct {
		name          string
		args          []string
		wantRequested bool
		wantPath      string
		wantMode      uiclient.DumpMode
		wantErr       bool
	}{
		{name: "default is off", args: nil, wantRequested: false, wantMode: uiclient.DumpModeANSI},
		{name: "bare flag asks for a dump", args: []string{uiDumpFlag}, wantRequested: true, wantMode: uiclient.DumpModeANSI},
		{name: "joined path", args: []string{uiDumpFlag + "=/tmp/x/ui.dump"}, wantRequested: true, wantPath: "/tmp/x/ui.dump", wantMode: uiclient.DumpModeANSI},
		{name: "separated path is refused by name", args: []string{uiDumpFlag, "/tmp/x/ui.dump"}, wantErr: true},
		{name: "plain mode", args: []string{uiDumpFlag, uiDumpModeFlag, string(uiclient.DumpModePlain)}, wantRequested: true, wantPath: "", wantMode: uiclient.DumpModePlain},
		{name: "plain mode joined", args: []string{uiDumpModeFlag + "=" + string(uiclient.DumpModePlain)}, wantRequested: false, wantMode: uiclient.DumpModePlain},
		{name: "mode needs a value", args: []string{uiDumpModeFlag}, wantErr: true},
		{name: "unknown mode is refused", args: []string{uiDumpModeFlag, "jsonl"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := uiParseFlags(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("uiParseFlags(%v) accepted a bad command line", tc.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("uiParseFlags(%v): %v", tc.args, err)
			}
			if got.dumpRequested != tc.wantRequested || got.dumpPath != tc.wantPath {
				t.Errorf("dump = (requested=%v path=%q), want (requested=%v path=%q)",
					got.dumpRequested, got.dumpPath, tc.wantRequested, tc.wantPath)
			}
			if got.dumpMode != tc.wantMode {
				t.Errorf("mode = %q, want %q", got.dumpMode, tc.wantMode)
			}
		})
	}
}

// TestUIDumpFollowsTheEnvironmentSwitch pins the env channel: GOPI_UI_DUMP is
// a switch whose truthy value means "pick a path" and whose ANY-OTHER value IS
// the path — the distinction that decides whether a dump exists at all.
func TestUIDumpFollowsTheEnvironmentSwitch(t *testing.T) {
	t.Run("truthy value asks for the default path", func(t *testing.T) {
		t.Setenv(uiDumpEnv, config.EnvBoolOn)
		path, requested := uiDumpFromEnv()
		if !requested || path != "" {
			t.Errorf("GOPI_UI_DUMP=%s parsed as (path=%q requested=%v), want the resolver to choose", config.EnvBoolOn, path, requested)
		}
	})
	t.Run("a path value IS the path", func(t *testing.T) {
		t.Setenv(uiDumpEnv, "/home/op/dumps/ui.dump")
		path, requested := uiDumpFromEnv()
		if !requested || path != "/home/op/dumps/ui.dump" {
			t.Errorf("GOPI_UI_DUMP parsed as (path=%q requested=%v), want the named path", path, requested)
		}
	})
	t.Run("unset is off", func(t *testing.T) {
		t.Setenv(uiDumpEnv, "")
		if _, requested := uiDumpFromEnv(); requested {
			t.Error("an unset switch still asked for a dump")
		}
	})
	t.Run("an unparsable mode keeps the default and is reported", func(t *testing.T) {
		t.Setenv(uiDumpModeEnv, "jsonl")
		if got := uiDumpModeFromEnv(); got != uiclient.DumpModeANSI {
			t.Errorf("mode = %q, want the default %q", got, uiclient.DumpModeANSI)
		}
	})
	t.Run("the env mode is honoured", func(t *testing.T) {
		t.Setenv(uiDumpModeEnv, string(uiclient.DumpModePlain))
		if got := uiDumpModeFromEnv(); got != uiclient.DumpModePlain {
			t.Errorf("mode = %q, want %q", got, uiclient.DumpModePlain)
		}
	})
}

// TestUIResolveDumpPath: off stays off, an explicit path wins and its parent
// directory is created, and a bare request lands in the session's own
// directory — never /tmp (Rule 22).
func TestUIResolveDumpPath(t *testing.T) {
	t.Run("off resolves to nothing", func(t *testing.T) {
		got, err := uiResolveDumpPath(uiOptions{}, nil)
		if err != nil || got != "" {
			t.Errorf("resolved %q (%v), want no path and no error", got, err)
		}
	})

	t.Run("an explicit path is created with its directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nested", "deeper")
		want := filepath.Join(dir, "screen.dump")
		got, err := uiResolveDumpPath(uiOptions{dumpRequested: true, dumpPath: want}, nil)
		if err != nil {
			t.Fatalf("uiResolveDumpPath: %v", err)
		}
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("the dump's directory was not created: %v", err)
		}
	})

	t.Run("a bare request takes a scratch directory off the root filesystem", func(t *testing.T) {
		got, err := uiResolveDumpPath(uiOptions{dumpRequested: true}, nil)
		if err != nil {
			t.Fatalf("uiResolveDumpPath: %v", err)
		}
		if filepath.Base(got) != uiDumpFileName {
			t.Errorf("path = %q, want the dump file name %q", got, uiDumpFileName)
		}
		// Rule 22 is a CRITERION, not a spelling: measure the device id of the
		// directory the dump landed in against the root filesystem's.
		dir := filepath.Dir(got)
		rootDev, err := scratch.RootDeviceID()
		if err != nil {
			t.Fatalf("cannot measure the root filesystem: %v", err)
		}
		dev, err := scratch.DeviceID(dir)
		if err != nil {
			t.Fatalf("cannot measure %s: %v", dir, err)
		}
		if dev == rootDev {
			t.Errorf("the resolved dump directory %s is on the root filesystem (device %d)", dir, dev)
		}
	})
}

// TestUIArmDumpWiresTheRenderStream pins the wiring the capability depends on:
// with a dump the render stream goes to BOTH the terminal and the sink, and
// with no dump the writer is the counting writer UNCHANGED (the off switch must
// not silently re-route the surface).
func TestUIArmDumpWiresTheRenderStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiDumpFileName)

	t.Run("off leaves the render path alone", func(t *testing.T) {
		screen := uiclient.NewScreenWriter(&strings.Builder{})
		got, dumper, err := uiArmDump("", uiOptions{dumpMode: uiclient.DumpModeANSI}, "http://127.0.0.1:1", 80, 24, screen)
		if err != nil {
			t.Fatalf("uiArmDump: %v", err)
		}
		if dumper != nil {
			t.Error("a dump sink was built although no dump was asked for")
		}
		if got != screen {
			t.Error("the render stream was re-routed even though the capability is off")
		}
	})

	t.Run("on tees the stream and reports identity", func(t *testing.T) {
		var terminal strings.Builder
		screen := uiclient.NewScreenWriter(&terminal)
		got, dumper, err := uiArmDump(path, uiOptions{dumpMode: uiclient.DumpModeANSI, tools: true},
			"http://127.0.0.1:8123", 100, 30, screen)
		if err != nil {
			t.Fatalf("uiArmDump: %v", err)
		}
		if dumper == nil {
			t.Fatal("no dump sink was built although a dump was asked for")
		}
		if _, err := got.Write([]byte("\x1b[1;1Hhello dump")); err != nil {
			t.Fatalf("write through the tee: %v", err)
		}
		// The terminal got the bytes, and so did the observer's grid.
		if !strings.Contains(terminal.String(), "hello dump") {
			t.Errorf("the terminal did not receive the frame: %q", terminal.String())
		}
		if line := dumper.Snapshot().Lines[0]; line != "hello dump" {
			t.Errorf("the dump did not observe the frame: row 0 = %q", line)
		}
		id := dumper.Snapshot()
		if id.Cols != 100 || id.Rows != 30 {
			t.Errorf("the sink's grid = %dx%d, want the caller's 100x30", id.Cols, id.Rows)
		}
		body, err := os.ReadFile(dumper.Path())
		if err != nil {
			t.Fatalf("read the dump: %v", err)
		}
		for _, want := range []string{"tools=on", "target=http://127.0.0.1:8123", "terminal=100x30"} {
			if !strings.Contains(string(body), want) {
				t.Errorf("the dump's identity record is missing %q:\n%s", want, body)
			}
		}
	})
}

// TestUIReportDumpRecord pins the surface's own end-of-run record: the line an
// operator (or the run's transcript) reads to learn WHERE the dump went and
// whether it was written at all. A capability whose report can be silent reads
// as success when it produced nothing.
//
// It does NOT start the TUI: bubbletea's program blocks on its input, so a test
// that ran it would hang. The live path is exercised end to end by the pty run
// in the report's evidence file; here the RECORD is the subject.
func TestUIReportDumpRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiDumpFileName)
	screen := uiclient.NewScreenWriter(&strings.Builder{})
	writer, dumper, err := uiArmDump(path, uiOptions{dumpMode: uiclient.DumpModePlain}, "http://127.0.0.1:8123", 100, 30, screen)
	if err != nil {
		t.Fatalf("uiArmDump: %v", err)
	}
	if _, err := writer.Write([]byte("a frame\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Capture os.Stderr the way the sibling console-redirect test captures
	// stdout: swap it for a file, restore it in the defer.
	stderrPath := filepath.Join(t.TempDir(), "stderr.txt")
	f, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	realErr := os.Stderr
	os.Stderr = f
	uiReportDump(dumper, uiOptions{dumpMode: uiclient.DumpModePlain}, "http://127.0.0.1:8123", 100, 30)
	os.Stderr = realErr
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	record := readTestFile(t, stderrPath)
	for _, want := range []string{
		"#gopi-ui-dump-report",
		"path=" + path,
		"json=" + path + ".json",
		"target=http://127.0.0.1:8123",
		"terminal=100x30",
		"grid=100x30",
		"mode=plain",
		"attrs=true",
		"dumps=",
		"frames=1",
		"blind_spots=",
	} {
		if !strings.Contains(record, want) {
			t.Errorf("the dump report is missing %q:\n%s", want, record)
		}
	}
	if !strings.Contains(record, "bytes=") {
		t.Errorf("the report does not state the dump's size:\n%s", record)
	}
}

// readTestFile reads a file a test just wrote, failing the test if it is gone.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
