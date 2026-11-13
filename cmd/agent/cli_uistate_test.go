package main

// cli_uistate_test.go — the `gopi ui_state` verb, driven through its REAL
// entry point (runUIStateSubcommand), over real dump files produced by the
// product's own sink.
//
// ZERO PIXELS, ZERO VISION: every assertion here is made against the
// interpreted CELL GRID — the artifact the capability exists to expose. A
// screen_capture / vision_analyze step in this suite would assert on a
// different, less precise artifact (measured earlier in this project: a vision
// model returns prose about a layout, never "the status row is row 29").

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/uiclient"
)

// uiStateAbsentPID is a pid no process of this host can hold (the kernel's
// pid ceiling is far below it), so a fixture bearing it is provably a surface
// whose writer is GONE — the exited case, without killing anything.
const uiStateAbsentPID = 1 << 24

// uiStateScratchClass is the runtime scratch class a `gopi ui` dump lands in
// (internal/scratch's RuntimeClassUISession).
const uiStateScratchClass = "ui-session"

// uiStateFixtureRefreshWait clears the sink's own refresh interval
// (uiclient's dumpMinInterval) so a fixture's painted frame reaches the FILE,
// not only the in-memory grid. It is a wait on a documented throttle, not a
// tuned sleep.
const uiStateFixtureRefreshWait = 150 * time.Millisecond

// uiStateFixture is one dump file this suite installs.
type uiStateFixture struct {
	path string
	line string
	// pid is the identity the dump RECORDS (what --pid matches), which is not
	// the pid the scratch directory name happens to carry.
	pid int
}

// uiStateFixtureProfile points the profile and scratch-home resolution at fresh
// temporary trees, so discovery runs against ONLY this test's dumps: a real
// profile's dumps on the host must never enter an assertion (and vice versa).
// It returns (gopiDir, scratchHome).
func uiStateFixtureProfile(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	profile := t.TempDir()
	scratchHome := filepath.Join(home, "workspace", "gopi-test-scratch")
	t.Setenv("GOPI_HOME", profile)
	t.Setenv("GOPI_TEST_HOME", home)
	// The scratch home has its OWN knob (internal/scratch.HomeDir reads it
	// FIRST), and the suite publishes one for every test — so pinning it here is
	// what makes discovery resolve against THIS test's tree rather than the
	// suite's. Both knobs, or the two roots come from different places.
	t.Setenv("GOPI_TEST_SCRATCH_HOME", scratchHome)
	return filepath.Join(profile, ".gopi"), scratchHome
}

// writeUIStateDump writes a dump via the product's sink, under root/subdir, with
// the given identity and one painted line.
func writeUIStateDump(t *testing.T, dir string, pid int, started time.Time, target, line string) uiStateFixture {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, uiclient.DumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Cols: 60,
		Rows: 8,
		Identity: uiclient.DumpIdentity{
			PID:              pid,
			Target:           target,
			Tools:            true,
			ColsRows:         [2]int{60, 8},
			TerminalColsRows: [2]int{60, 8},
			Started:          started,
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	// The sink installs its first (empty) snapshot at construction; paint, wait
	// past the throttle, then paint again so the SECOND write is the one the
	// file carries.
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + line + "\r\n")); err != nil {
		t.Fatalf("paint fixture dump: %v", err)
	}
	//stylegate:allow-sleep lets the fixture's dump sink observe the repaint before the next write; the sink is change-detecting, so a slow host only retries later
	time.Sleep(uiStateFixtureRefreshWait)
	if _, err := d.Write([]byte("\x1b[H" + line + "\x1b[3;1Hstatus: idle · turns 2\r\n")); err != nil {
		t.Fatalf("repaint fixture dump: %v", err)
	}
	return uiStateFixture{path: path, line: line, pid: pid}
}

// liveStartStamp is this process's verifiable start instant: a fixture stamped
// with it reads back as LIVE.
func liveStartStamp(t *testing.T) time.Time {
	t.Helper()
	started, err := uiclient.ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime: %v", err)
	}
	return started
}

// TestParseUIStateArgs pins the flag grammar: both --pid spellings, the boolean
// flags, -h, and the three refusals (unknown flag, positional argument, a
// non-pid value). A typo must never reach the agent loop.
func TestParseUIStateArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantPID int
		wantJS  bool
		wantLs  bool
		wantErr bool
	}{
		{name: "bare", args: nil},
		{name: "pid separated", args: []string{"--pid", "4711"}, wantPID: 4711},
		{name: "pid joined", args: []string{"--pid=4711"}, wantPID: 4711},
		{name: "json and list", args: []string{"--json", "--list"}, wantJS: true, wantLs: true},
		{name: "pid json list", args: []string{"--pid=42", "--json", "--list"}, wantPID: 42, wantJS: true, wantLs: true},
		{name: "unknown flag", args: []string{"--nope"}, wantErr: true},
		{name: "positional", args: []string{"dump.txt"}, wantErr: true},
		{name: "pid not a number", args: []string{"--pid", "abc"}, wantErr: true},
		{name: "pid zero", args: []string{"--pid=0"}, wantErr: true},
		{name: "pid missing value", args: []string{"--pid"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, help, err := parseUIStateArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("args %v were accepted: %+v", tc.args, req)
				}
				return
			}
			if err != nil {
				t.Fatalf("args %v: %v", tc.args, err)
			}
			if help {
				t.Fatalf("args %v asked for help", tc.args)
			}
			if req.pid != tc.wantPID || req.json != tc.wantJS || req.list != tc.wantLs {
				t.Fatalf("args %v -> %+v, want pid=%d json=%t list=%t", tc.args, req, tc.wantPID, tc.wantJS, tc.wantLs)
			}
		})
	}
	if _, help, err := parseUIStateArgs([]string{"-h"}); err != nil || !help {
		t.Fatalf("-h -> help=%t err=%v", help, err)
	}
}

// uiStateRun invokes the verb's real entry point with BOTH streams captured.
// The verb's answer is printed, not returned, and a refusal is reported on
// stderr (where a shell caller reads it: `gopi ui_state 2>&1`) — a test that
// captured only stdout would miss exactly the answers the refusals carry.
func uiStateRun(t *testing.T, args []string) (string, int) {
	t.Helper()
	out := captureStdoutAndStderr(t, func() int { return runUIStateSubcommand(args) })
	return out, uiStateLastCode
}

// uiStateLastCode is set by captureStdoutAndStderr's callee.
var uiStateLastCode int

// TestUIStateRefusesAmbiguityThenAnswers is the refusal contract, end to end
// through the verb: two live surfaces + one exited surface -> exit
// uiStateExitAmbiguous with every candidate LISTED; one live left -> exit 0 and
// the screen; nothing live -> the exited diagnosis, still exit 0.
func TestUIStateRefusesAmbiguityThenAnswers(t *testing.T) {
	gopiDir, scratchHome := uiStateFixtureProfile(t)
	sessions := filepath.Join(gopiDir, "sessions")
	stamp := liveStartStamp(t)

	first := writeUIStateDump(t, filepath.Join(sessions, "aaa"), os.Getpid(), stamp, "http://127.0.0.1:8123", "gopi> first live surface")
	second := writeUIStateDump(t, filepath.Join(sessions, "bbb"), os.Getpid(), stamp, "http://127.0.0.1:8124", "gopi> second live surface")
	// The exited surface: a pid above the kernel's ceiling is provably absent,
	// which is what "the writer is gone" means without killing anything.
	gone := writeUIStateDump(t, filepath.Join(scratchHome, "queue",
		"queue-runtime-"+uiStateScratchClass+"-"+strconv.Itoa(uiStateAbsentPID)),
		uiStateAbsentPID, time.Now().Add(-time.Hour).UTC().Truncate(time.Second), "http://127.0.0.1:8125", "gopi> last frame")

	out, code := uiStateRun(t, nil)
	if code != uiStateExitAmbiguous {
		t.Fatalf("exit code = %d, want %d (the ambiguity refusal)\n%s", code, uiStateExitAmbiguous, out)
	}
	for _, want := range []string{"refusing to guess", "--pid", first.path, second.path, gone.path, "exited"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "grid_begin") {
		t.Error("a refusal printed a screen body — a refusal must not also answer")
	}

	// Resolved by --pid while still ambiguous: the explicit route picks the
	// exited surface and DIAGNOSES it rather than reporting an empty screen.
	out, code = uiStateRun(t, []string{"--pid", strconv.Itoa(gone.pid)})
	if code != 0 {
		t.Fatalf("--pid on the exited surface: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, uiclient.ExitedNotice) {
		t.Errorf("the exited target was not diagnosed with the fixed sentence:\n%s", out)
	}
	if !strings.Contains(out, "state=exited") {
		t.Errorf("the exited state is not in the header:\n%s", out)
	}

	// One live surface left: the verb answers with it.
	if err := os.RemoveAll(filepath.Dir(second.path)); err != nil {
		t.Fatal(err)
	}
	out, code = uiStateRun(t, nil)
	if code != 0 {
		t.Fatalf("with one live surface: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "state=live") {
		t.Errorf("the header does not say live:\n%s", out)
	}
	if !strings.Contains(out, "status: idle") {
		t.Errorf("the answer does not carry the surface's own status row:\n%s", out)
	}
	if strings.Contains(out, "state=exited") {
		t.Errorf("the exited candidate was rendered as the answer while a live one existed:\n%s", out)
	}
}

// TestUIStateCarriesGridNoticeAndBlindSpots — the FIRST sentence of every
// answer states what the artifact is (an interpreted cell grid, not bytes, not
// pixels), and the 8 blind spots are present in BOTH renderings. A reader that
// never sees the limits will assert past them.
// TestUIStateUnpaintedSurfaceIsNotReportedAsAScreen is the CLI half of the
// frames=0 guard: the notice must reach the reader through BOTH renderings, and
// the JSON must carry the counter itself (a caller that only machine-reads must
// be able to detect the condition without parsing an English sentence).
//
// Measured provenance: an E2E run that quit the TUI with Ctrl+C left exactly
// this artifact, and the verb rendered its blank grid as an ordinary screen.
func TestUIStateUnpaintedSurfaceIsNotReportedAsAScreen(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	// A dump the sink installed and NOBODY ever wrote to: zero interpreted
	// frames, which is what the abort path leaves behind.
	dir := filepath.Join(gopiDir, "sessions", "unpainted")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, uiclient.DumpFileName)
	if _, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		Cols: 60,
		Rows: 8,
		Identity: uiclient.DumpIdentity{
			PID:              os.Getpid(),
			Target:           "http://127.0.0.1:8300",
			Tools:            false,
			ColsRows:         [2]int{60, 8},
			TerminalColsRows: [2]int{60, 8},
			Started:          liveStartStamp(t),
		},
	}); err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}

	text, code := uiStateRun(t, nil)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, text)
	}
	if !strings.Contains(text, uiclient.UnpaintedNotice) {
		t.Errorf("an unpainted dump was rendered without the notice:\n%s", text)
	}
	// The notice must come BEFORE the grid: a reader that quotes the head of
	// the output keeps the qualification with the thing it qualifies.
	if gi, ui := strings.Index(text, uiclient.ScreenGridNotice), strings.Index(text, uiclient.UnpaintedNotice); gi < 0 || ui < gi {
		t.Errorf("the unpainted notice must follow the grid notice (grid=%d unpainted=%d)\n%s", gi, ui, text)
	}

	jsonOut, code := uiStateRun(t, []string{"--json"})
	if code != 0 {
		t.Fatalf("--json exit %d\n%s", code, jsonOut)
	}
	var doc struct {
		Unpainted  string `json:"unpainted_notice"`
		Candidates []struct {
			Frames int `json:"frames"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("--json is not JSON: %v\n%s", err, jsonOut)
	}
	if doc.Unpainted != uiclient.UnpaintedNotice {
		t.Errorf("JSON unpainted_notice = %q, want %q", doc.Unpainted, uiclient.UnpaintedNotice)
	}
	if len(doc.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(doc.Candidates))
	}
	if doc.Candidates[0].Frames != uiclient.UnpaintedFrames {
		t.Errorf("JSON frames = %d, want %d", doc.Candidates[0].Frames, uiclient.UnpaintedFrames)
	}
}

// TestUIStatePaintedSurfaceCarriesNoUnpaintedNotice is the negative half: a
// surface that painted must NOT be qualified with the notice. Without this, the
// guard could pass by warning on everything — a check that always fires carries
// no information.
func TestUIStatePaintedSurfaceCarriesNoUnpaintedNotice(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "painted"), os.Getpid(), liveStartStamp(t),
		"http://127.0.0.1:8400", "gopi> a painted surface")

	text, code := uiStateRun(t, nil)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, text)
	}
	if strings.Contains(text, uiclient.UnpaintedNotice) {
		t.Errorf("a painted surface was qualified as unpainted:\n%s", text)
	}
	if !strings.Contains(text, "a painted surface") {
		t.Errorf("the painted content is missing from the answer:\n%s", text)
	}
}

// TestUIStateCarriesGridNoticeAndBlindSpots pins the two things every output
// must carry, in text and in JSON, on a plain successful read.
func TestUIStateCarriesGridNoticeAndBlindSpots(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	stamp := liveStartStamp(t)
	writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "solo"), os.Getpid(), stamp, "http://127.0.0.1:8200", "gopi> the only surface")

	text, code := uiStateRun(t, nil)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, text)
	}
	if !strings.Contains(text, uiclient.ScreenGridNotice) {
		t.Errorf("the grid notice is missing from the text answer:\n%s", text)
	}
	if !strings.Contains(text, "blind_spots_begin") || !strings.Contains(text, "blind_spots_end") {
		t.Errorf("the blind-spot section is missing:\n%s", text)
	}
	for _, spot := range uiStateAllBlindSpots() {
		if !strings.Contains(text, spot.ID) {
			t.Errorf("blind spot %q is not in the text answer", spot.ID)
		}
	}

	jsonOut, code := uiStateRun(t, []string{"--json"})
	if code != 0 {
		t.Fatalf("--json exit %d\n%s", code, jsonOut)
	}
	var doc struct {
		Tool       string `json:"tool"`
		GridNotice string `json:"what_this_is"`
		State      string `json:"state"`
		Candidates []struct {
			Screen struct {
				Lines     []string `json:"lines"`
				Grid      []string `json:"grid"`
				Cols      int      `json:"cols"`
				Rows      int      `json:"rows"`
				BlindSpot []struct {
					ID     string `json:"id"`
					Detail string `json:"detail"`
				} `json:"blind_spots"`
				AttrsRecorded bool `json:"attrs_recorded"`
				Cursor        struct {
					Row int `json:"row"`
					Col int `json:"col"`
				} `json:"cursor"`
			} `json:"screen"`
		} `json:"candidates"`
		BlindSpots []struct {
			ID     string `json:"id"`
			Detail string `json:"detail"`
		} `json:"blind_spots"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("the --json answer is not JSON: %v\n%s", err, jsonOut)
	}
	if doc.Tool != CliVerbUIState {
		t.Errorf("tool = %q, want %q", doc.Tool, CliVerbUIState)
	}
	if doc.GridNotice != uiclient.ScreenGridNotice {
		t.Errorf("the JSON answer does not state what the artifact is: %q", doc.GridNotice)
	}
	if doc.State != string(uiclient.StateLive) {
		t.Errorf("state = %q", doc.State)
	}
	if len(doc.BlindSpots) != 8 {
		t.Errorf("the JSON answer carries %d blind spots, want 8", len(doc.BlindSpots))
	}
	for _, spot := range doc.BlindSpots {
		if strings.TrimSpace(spot.Detail) == "" {
			t.Errorf("blind spot %q carries no explanation", spot.ID)
		}
	}
	if len(doc.Candidates) != 1 {
		t.Fatalf("the JSON answer carries %d candidates", len(doc.Candidates))
	}
	screen := doc.Candidates[0].Screen
	if len(screen.BlindSpot) != 8 {
		t.Errorf("the screen value carries %d blind spots, want 8", len(screen.BlindSpot))
	}
	if !screen.AttrsRecorded {
		t.Error("the screen value does not report per-cell attribute recording")
	}
	// TEXT vs JSON CONSISTENCY: the two renderings are the same observation.
	// The text answer's grid rows must appear, in order, as the JSON grid.
	jsonGrid := strings.Join(screen.Grid, "\n")
	for _, row := range screen.Lines {
		if row == "" {
			continue
		}
		if !strings.Contains(jsonGrid, row) {
			t.Errorf("JSON line %q is not in its own grid — the two renderings disagree", row)
		}
		if !strings.Contains(text, row) {
			t.Errorf("line %q is in the JSON answer but not in the text one — the two renderings disagree", row)
		}
	}
	if screen.Cols != 60 || screen.Rows != 8 {
		t.Errorf("geometry = %dx%d, want 60x8", screen.Cols, screen.Rows)
	}
}

// TestUIStateListNeverRefuses — --list is the surface that RESOLVES an
// ambiguity, so it must answer under every condition, including "no dump at
// all" (where it names the roots it scanned instead of refusing).
func TestUIStateListNeverRefuses(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	stamp := liveStartStamp(t)
	a := writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "a"), os.Getpid(), stamp, "http://127.0.0.1:8301", "gopi> A")
	b := writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "b"), os.Getpid(), stamp, "http://127.0.0.1:8302", "gopi> B")

	out, code := uiStateRun(t, []string{"--list"})
	if code != 0 {
		t.Fatalf("--list exit %d\n%s", code, out)
	}
	for _, want := range []string{a.path, b.path, "state", uiclient.ScreenGridNotice} {
		if !strings.Contains(out, want) {
			t.Errorf("--list does not carry %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "grid_begin") {
		t.Error("--list printed a screen body: its job is the inventory, not the answer")
	}

	empty := t.TempDir()
	t.Setenv("GOPI_HOME", filepath.Join(empty, "profile"))
	t.Setenv("GOPI_TEST_HOME", empty)
	t.Setenv("GOPI_TEST_SCRATCH_HOME", filepath.Join(empty, "scratch-home"))
	out, code = uiStateRun(t, []string{"--list"})
	if code != 0 {
		t.Fatalf("--list with no dumps exit %d, want 0 (this surface never refuses)\n%s", code, out)
	}
	if !strings.Contains(out, "no "+uiclient.DumpFileName) {
		t.Errorf("--list did not report the absence of a dump:\n%s", out)
	}
	// And the plain read in that state refuses with exit 1, naming the absence
	// rather than printing an empty screen.
	plain, code := uiStateRun(t, nil)
	if code != 1 {
		t.Fatalf("a read with no dump exit %d, want 1\n%s", code, plain)
	}
}

// TestUIStateUnknownPIDNeverSubstitutes — a --pid that matches nothing is an
// error naming what IS there; it must never fall back to the one dump the scan
// happened to find. A silent substitution is how a caller asserts on a screen
// it never asked about.
func TestUIStateUnknownPIDNeverSubstitutes(t *testing.T) {
	gopiDir, _ := uiStateFixtureProfile(t)
	stamp := liveStartStamp(t)
	only := writeUIStateDump(t, filepath.Join(gopiDir, "sessions", "only"), os.Getpid(), stamp, "http://127.0.0.1:8400", "gopi> not the one you asked for")

	out, code := uiStateRun(t, []string{"--pid", "999991"})
	if code != 1 {
		t.Fatalf("--pid with no match exit %d, want 1\n%s", code, out)
	}
	if strings.Contains(out, "grid_begin") {
		t.Errorf("a missed --pid still printed a screen (a substitution):\n%s", out)
	}
	_ = only
}

// captureStdoutAndStderr runs fn with BOTH standard streams redirected into one
// pipe, so a verb whose answer spans them (the state on stdout, a refusal's
// candidate list on stderr) is asserted on as the caller sees it.
func captureStdoutAndStderr(t *testing.T, fn func() int) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w
	code := fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	uiStateLastCode = code
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(data)
}
