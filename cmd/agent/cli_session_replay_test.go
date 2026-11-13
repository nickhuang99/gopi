package main

// cli_session_replay_test.go — the `gopi session replay` sub-verb driven for REAL.
//
// The cases here run the built binary over a pty (the fixture in
// chat_pty_fixture_test.go) against a SYNTHETIC session directory: they assert
// what the operator's terminal receives and what the exit status is, which is
// the only place the transport, the restore path and the material loader are all
// exercised together. A separate non-pty case covers the two static views
// (--rounds / --round N), which are how a caller — and this suite — can see the
// material without a terminal at all.
//
// THE UNIT IS A ROUND, and this suite asserts the SAME number the other surface
// prints: `--rounds` reports the session's own round count (from
// internal/gopilog, which is also what `gopi session log <id> --rounds` reads),
// and the pty case drives the transport against it — so "3/3" on the status line
// and "round 3 of 3" in the log listing cannot drift apart without a case here
// failing.

import (
	"encoding/json"
	"fmt"
	"github.com/nick/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/replayplay"
)

// A session directory the pty cases replay: a 3x8 dump with two scrollback lines
// and a timeline of TWO rounds, so the material has rounds AND dump frames — the
// two materials that must not be confused for one another.
const (
	replayTestCols = 60
	replayTestRows = 8
)

// replayTestSession writes a synthetic session (dump + timeline) into a fresh
// profile under t.TempDir and returns the profile root and the session id.
//
// The directory shape is the profile's own (config.SessionsDirName under
// config.ProfileDirIn), composed from the OWNERS' constants: a test that spelled
// ".gopi/sessions" would keep passing after the layout moved while the verb
// under it started refusing every session.
//
// THE TIMELINE CARRIES TWO MODEL ROUNDS, so the static views (`--rounds`,
// `--round N`) and the pty driver have a sequence whose length, order and
// content can all be told apart: the material's steps are its ROUNDS (the dump's
// screenfuls ride beside them, in their own field), and a one-round fixture could
// not tell "the second step" from "the first".
func replayTestSession(t *testing.T) (profileRoot, sessionID string) {
	t.Helper()
	home := t.TempDir()
	sessionID = "test-session-0001"
	dir := filepath.Join(config.ProfilePathIn(home, config.SessionsDirName), sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create the session dir: %v", err)
	}
	row := strings.Repeat(" ", replayTestCols)
	grid := []string{
		"hello from the recorded screen",
		"second row",
		row,
	}
	body := map[string]any{
		"identity": map[string]any{"surface": "chat"},
		"screen": map[string]any{
			"version":        1,
			"cols":           replayTestCols,
			"rows":           replayTestRows,
			"attrs_recorded": false,
			"grid":           grid,
			"lines":          grid,
			"scrollback":     []string{"first scrollback line", "second scrollback line"},
			"blind_spots":    []string{},
			"cursor":         map[string]int{"row": 0, "col": 0},
		},
	}
	blob, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatalf("marshal the dump: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, replayplay.DumpJSONName), blob, 0o600); err != nil {
		t.Fatalf("write the dump: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, replayplay.SessionJSONLName), []byte(replayTestTimeline), 0o600); err != nil {
		t.Fatalf("write the timeline: %v", err)
	}
	return home, sessionID
}

// replayTestTimeline is the fixture log: TWO model rounds, each a set of
// assistant rows closed by the `request` row that records the exchange's usage —
// THE WRITER'S OWN ORDER (internal/agent writes LogAssistant*, then LogRequest
// with the usage of the call that just finished; measured on a real session:
// user, assistant, request, tool_result, …). A fixture that puts the `request`
// row FIRST makes the reader attribute every exchange's content to the NEXT one,
// which is the defect this fixture had and the round view exposed. The first
// round also carries a tool call and its result, so the round's rows exercise
// every role the renderer colours.
var replayTestTimeline = `{"type":"user","message":{"role":"user","content":"a recorded question"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing the options"},{"type":"text","text":"a recorded answer"},{"type":"tool_use","id":"t1","name":"bash","input":{"command":"echo hi"}}]}}
{"type":"request","iteration":0}
{"type":"tool_result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"hi from the tool"}]}}
{"type":"user","message":{"role":"user","content":"a second recorded question"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the second answer"}]}}
{"type":"request","iteration":1}
`

// replayTestRoundCount is how many ROUNDS replayTestTimeline yields: the rounds
// the session reader finds, which is what the fixture is written for.
//
// IT IS ALSO THE TURN COUNT HERE, and that coincidence is a LIMIT of this
// fixture (two user rows, each answered with exactly one exchange), so it cannot
// tell a turn-stepped surface from a round-stepped one. The discriminating
// assertion lives where the fixture CAN differ — internal/replayplay's
// turnstep_test.go builds one operator row answered with three exchanges and
// requires 1 step / 3 rounds. This constant stays 2 so the round-addressing
// assertions below keep meaning what they say.
const replayTestRoundCount = 2

// replayTestTurnCount is how many TURNS the same fixture yields — one per user
// row. It is stated separately so a reader can see the two quantities are not
// the same variable, even when they hold the same value in this fixture.
const replayTestTurnCount = 2

// replayTestEnv is the environment the verb is run with: the synthetic profile
// plus the two terminal variables that decide the colour arm. COLUMNS/LINES are
// deliberately absent — the pty's own winsize is what the player must ask.
func replayTestEnv(t *testing.T, profileRoot string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case config.GopiDirEnv, "TERM", "COLORTERM", "COLUMNS", "LINES":
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		config.GopiDirEnv+"="+profileRoot,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
}

// The driver's own two bounds: how long it waits for the first output, and how
// long it waits for the process to leave after the quit key.
const (
	replayFirstOutputWait = 15 * time.Second
	replayExitWait        = 5 * time.Second
	replayReadStep        = 50 * time.Millisecond
)

// replayRunningWord and replayPausedWord are the two transport words the status
// line carries. They are read OFF the rendered status line rather than re-typed:
// the player owns them, and this file asserts the operator-visible result, not a
// copy of the vocabulary.
var replayRunningWord, replayPausedWord = mustReplayStatusWords()

// mustReplayStatusWords renders a status line twice (running, paused) and picks
// the distinguishing word out of each — so a reworded status line reaches this
// test automatically instead of breaking it. It is a Must* (Rule 19): a status
// line whose two transport states read alike would make every assertion below
// vacuous, and that is a construction error, not a test outcome.
func mustReplayStatusWords() (running, paused string) {
	base := replayplay.RenderOptions{
		Round:      replayplay.Round{Index: 1, Kind: replayplay.RoundModel, Rows: []replayplay.FrameRow{{Cells: "x"}}},
		RoundTotal: 3, RoundIndex: 1, Cols: 100, Rows: 4,
		Palette: replayplay.DefaultPalette(), Truecolor: true, Speed: replayplay.DefaultSpeed,
	}
	base.Paused = false
	a := replayplay.Render(base)
	base.Paused = true
	b := replayplay.Render(base)
	running = replayStatusWord(a, b)
	paused = replayStatusWord(b, a)
	if running == "" || paused == "" {
		// A word that cannot be told apart from its counterpart would make every
		// assertion below vacuous (strings.Contains(anything, "") is true).
		panic("the status line has no distinguishing transport word — the player and this test disagree about its shape")
	}
	return running, paused
}

// replayStatusWord returns the word that appears in the status line of one
// render and not the other, on the row the status line occupies.
func replayStatusWord(with, without string) string {
	for _, token := range strings.Fields(stripAnsi(with)) {
		if strings.Contains(stripAnsi(without), token) {
			continue
		}
		if strings.ContainsAny(token, "0123456789/") {
			continue
		}
		return token
	}
	return ""
}

// stripAnsi drops every CSI sequence, leaving a rendered frame's text.
func stripAnsi(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && !(s[j] >= 'A' && s[j] <= 'Z') && !(s[j] >= 'a' && s[j] <= 'z') {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// replayScrollMarker is the status line's scroll marker at offset ONE, read OFF a
// render rather than re-typed: the player owns the spelling (a marker re-typed
// here would keep passing after the player changed it), and the marker for the
// first row is exactly what one ↓ must produce.
var replayScrollMarker = mustReplayScrollMarker()

// mustReplayScrollMarker renders a round that overflows the canvas at scroll 0 and
// at scroll 1 and returns the token that only the scrolled one carries. A Must*
// (Rule 19): a renderer that showed no marker for a scrolled view would make the
// arrow assertion below vacuous — the pty case drives ArrowDown and then has
// nothing to look for — and that is a construction error, not a test outcome.
func mustReplayScrollMarker() string {
	rows := make([]replayplay.FrameRow, 0, replayScrollFixtureRows)
	for i := 0; i < replayScrollFixtureRows; i++ {
		rows = append(rows, replayplay.FrameRow{Cells: "x"})
	}
	base := replayplay.RenderOptions{
		Round:      replayplay.Round{Index: 1, Kind: replayplay.RoundModel, Rows: rows},
		RoundTotal: 2, RoundIndex: 1, Cols: 100, Rows: replayScrollFixtureRows / 2,
		Palette: replayplay.DefaultPalette(), Truecolor: true, Speed: replayplay.DefaultSpeed,
	}
	atTop := stripAnsi(replayplay.Render(base))
	base.Scroll = 1
	scrolled := stripAnsi(replayplay.Render(base))
	for _, tok := range strings.Fields(scrolled) {
		if !strings.Contains(atTop, tok) {
			return tok
		}
	}
	panic("the status line carries no scroll marker for a scrolled view — the arrow's effect cannot be observed")
}

// replayScrollFixtureRows is the row count the marker fixture renders: twice the
// canvas, so the view really can scroll (a round that fits has no marker at all).
const replayScrollFixtureRows = 12

// replayDrive runs `gopi session replay <id>` over a pty and returns a helper that
// sends keys and collects the output each key produced.
type replayDrive struct {
	t      *testing.T
	cmd    *exec.Cmd
	master *os.File
	pair   chatPtyPair
	out    strings.Builder
	exited bool
	status int
	// masterFD is the master's raw descriptor, captured through SyscallConn so
	// reading it does not silently clear O_NONBLOCK (see startReplayDrive).
	masterFD int
	// answered records which startup probes have been replied to, so a reply is
	// sent once and never re-enters the input stream.
	answered map[string]bool
}

// startReplayDrive starts the player over a pty whose answers are supplied.
func startReplayDrive(t *testing.T, profileRoot, sessionID string) *replayDrive {
	t.Helper()
	pair := chatOpenPtyPair(t, replayTestCols, replayTestRows)
	bin := gopiTestBinPath(t)
	cmd := exec.Command(bin, CliVerbSession, sessionSubcmdReplay, sessionID)
	cmd.Env = replayTestEnv(t, profileRoot)
	cmd.Stdin = pair.Slave
	cmd.Stdout = pair.Slave
	cmd.Stderr = pair.Slave
	cmd.SysProcAttr = &unix.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the player: %v", err)
	}
	// The driver POLLS before it reads. A bare read would park the reader past
	// the window it asked for and turn every bounded wait into an unbounded one
	// (measured: the first version of this driver parked the whole package for
	// two minutes). Polling also survives the trap that defeated O_NONBLOCK
	// here — `os.File.Fd()` puts the descriptor BACK into blocking mode, so
	// `SetNonblock(Fd())` undoes itself silently.
	rc, err := pair.Master.SyscallConn()
	if err != nil {
		t.Fatalf("master syscall conn: %v", err)
	}
	masterFD := -1
	if err := rc.Control(func(fd uintptr) { masterFD = int(fd) }); err != nil {
		t.Fatalf("read the master fd: %v", err)
	}
	if err := unix.SetNonblock(masterFD, true); err != nil {
		t.Fatalf("set the master non-blocking: %v", err)
	}
	d := &replayDrive{t: t, cmd: cmd, master: pair.Master, pair: pair, masterFD: masterFD, answered: map[string]bool{}}
	t.Cleanup(func() {
		if !d.exited {
			_ = cmd.Process.Kill()    //stylegate:ignore kills the session-replay child once the replayed frame was read from the pty
			_, _ = cmd.Process.Wait() //stylegate:ignore reaps that child so it does not linger as a zombie of this test
		}
	})
	return d
}

// pump reads whatever the master has, recording it, and answers the startup
// terminal-capability probe the first time it appears.
func (d *replayDrive) pump(dur time.Duration) string {
	d.t.Helper()
	end := time.Now().Add(dur)
	var got strings.Builder
	buf := make([]byte, 1<<16)
	for time.Now().Before(end) {
		ready, perr := unix.Poll([]unix.PollFd{{Fd: int32(d.masterFD), Events: unix.POLLIN}}, int(replayReadStep/time.Millisecond))
		if perr == unix.EINTR {
			continue
		}
		if perr != nil {
			d.t.Fatalf("poll the player's terminal: %v", perr)
		}
		if ready == 0 {
			continue
		}
		n, err := unix.Read(d.masterFD, buf)
		if n > 0 {
			chunk := string(buf[:n])
			got.WriteString(chunk)
			d.out.WriteString(chunk)
			d.answerProbe(chunk)
		}
		if err != nil && err != unix.EAGAIN && err != unix.EINTR {
			break
		}
	}
	return got.String()
}

// answerProbe answers the capability queries a gopi process issues at startup,
// ONCE each, so a pty case does not pay the unanswered-probe window.
//
// WHY THIS IS NECESSARY, and it is a measurement rather than a convenience:
// `bubbletea`'s package init calls lipgloss.HasDarkBackground(), which writes
// `OSC 11;?` + `CSI 6n` to the terminal and waits up to termenv.OSCTimeout —
// FIVE SECONDS — for a reply (termenv_unix.go). Measured on this host by strace
// of a real `gopi replay`: one `pselect6(2, [1], …, {tv_sec=5, tv_nsec=0})`
// returning `= 0 (Timeout) <5.005350>` before any code of this verb ran. A real
// emulator answers in microseconds; a bare pty answers nothing and the cost
// lands on the first output byte. (internal/lineedit's own probe_pty_test.go
// answers the same queries for the same reason.)
//
// The query precedes our own player's output, so an un-answered run still works
// — it is simply five seconds slower, which is why replayFirstOutputWait is
// larger than the window.
func (d *replayDrive) answerProbe(chunk string) {
	for _, q := range replayProbeQueries {
		if !strings.Contains(chunk, q.query) {
			continue
		}
		if d.answered[q.name] {
			continue
		}
		d.answered[q.name] = true
		if _, err := unix.Write(d.masterFD, []byte(q.answer)); err != nil {
			d.t.Logf("answering the %s probe: %v", q.name, err)
		}
	}
}

// replayProbeQueries are the two questions the terminal is asked at startup and
// the answers a terminal that supports both would give. The sequences are the
// standard ones (OSC 11 background report; DSR 6 cursor position report).
var replayProbeQueries = []struct {
	name   string
	query  string
	answer string
}{
	{"background colour", "\x1b]11;?", "\x1b]11;rgb:1111/1111/1111\x1b\\"},
	{"cursor position", "\x1b[6n", "\x1b[1;1R"},
}

// waitFor reads until the accumulated output satisfies want, or fails.
func (d *replayDrive) waitFor(want string, limit time.Duration) string {
	d.t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if strings.Contains(d.out.String(), want) {
			return d.out.String()
		}
		d.pump(replayReadStep)
	}
	d.t.Fatalf("the player never printed %q within %s. Output so far:\n%s", want, limit, d.out.String())
	return ""
}

// send writes a key sequence to the player's terminal. It goes through the raw
// descriptor with an EAGAIN retry (the descriptor is non-blocking; a dropped key
// would look exactly like a broken binding).
//
// THE WHOLE SEQUENCE GOES IN ONE WRITE, and that is a fix rather than a detail
// (measured 2026-10-06, this host). A multi-byte key is ONE message: a terminal
// emulator sends `ESC [ D` as a unit, and the player's reader resolves it as the
// left arrow. Writing it BYTE BY BYTE across separate syscalls lets a slow
// schedule interleave the writes, so the reader can see a lone ESC (a key of its
// own) and then `[` and `D` as ordinary keys — the arrow silently becomes
// something else and the case fails with "the left arrow did not step back",
// which is exactly what this case did on this host while passing on the next run
// with no source change. One write per key sequence removes the interleaving.
func (d *replayDrive) send(keys string) {
	d.t.Helper()
	rest := []byte(keys)
	for len(rest) > 0 {
		n, err := unix.Write(d.masterFD, rest)
		if err == unix.EAGAIN {
			time.Sleep(replayReadStep) //stylegate:allow-sleep EAGAIN backoff between pty writes; the replay reader under test needs the write to land, not a faster loop
			continue
		}
		if err != nil {
			d.t.Fatalf("send %q: %v", keys, err)
		}
		rest = rest[n:]
	}
}

// after clears the record and returns what the NEXT key produces.
func (d *replayDrive) after(keys string, dur time.Duration) string {
	d.t.Helper()
	d.out.Reset()
	d.send(keys)
	return d.pump(dur)
}

// waitExit joins the process and records its status.
func (d *replayDrive) waitExit(limit time.Duration) int {
	d.t.Helper()
	done := make(chan error, 1)
	go func() { done <- d.cmd.Wait() }()
	select {
	case err := <-done:
		d.exited = true
		if err == nil {
			d.status = 0
			return 0
		}
		var ee *exec.ExitError
		if ok := replayAsExitError(err, &ee); ok {
			d.status = ee.ExitCode()
			return d.status
		}
		d.t.Fatalf("waiting for the player: %v", err)
	case <-time.After(limit):
		d.t.Fatalf("the player did not exit within %s after the quit key", limit)
	}
	return -1
}

// replayAsExitError unwraps an *exec.ExitError so the driver can report the
// exit STATUS (a signal death is 128+N through exec's own convention).
func replayAsExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// TestSessionReplayStatic_RoundsListsTheMaterial runs the verb WITHOUT a
// terminal: the round listing is how a caller sees the material's extent, and it
// must work from a pipe (no tty, no keys).
//
// THE THREE NUMBERS ARE NAMED, and the case asserts each: `rounds=` is the
// SESSION's own round count (`gopi session log <id> --rounds`'s number, from the
// same reader), `steps=` is what the player steps through, and the dump frames
// are a SECOND material printed under their own heading — a screenful of cells
// is not a round.
func TestSessionReplayStatic_RoundsListsTheMaterial(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--rounds")
	if err != nil {
		t.Fatalf("--rounds failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		// The header's source= WORD is the data PLANE, not the file list: a
		// reader must be able to tell the renderer's own bytes from this
		// program's interpretation at a glance (`SourceLabel`, replayplay). The
		// fixture has no recording, so what it draws IS the interpretation and
		// the word has to say so — the old `SourceBoth` file list still rides
		// `mat.Source` for the callers that ask which artifacts were read.
		"source=" + replayplay.SourceLabelDump,
		// turns= is the TURN count — the ONE unit of this playback. The old
		// `steps=` field is GONE: the count had three meanings under one name
		// and now carries one name per unit.
		fmt.Sprintf("turns=%d", replayTestTurnCount),
		"dump frames=",
		"scrollback",
		"screen",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("--rounds does not report %q:\n%s", want, out)
		}
	}
	// The USER surface carries no `steps` at all (the operator's ruling,
	// 2026-10-25): `--rounds` is the INTERNAL view now, so it may print a round
	// count — but it must SAY it is internal, and the retired word must be gone
	// from its output entirely.
	if strings.Contains(out, "steps") {
		t.Fatalf("the internal round listing still prints the retired word `steps`:\n%s", out)
	}
	if !strings.Contains(out, "[internal]") {
		t.Fatalf("the round listing does not mark itself internal — a reader cannot tell the transport's unit from the playback's:\n%s", out)
	}
	if strings.Contains(out, "the second answer") {
		t.Fatalf("the listing printed a round's PROSE: this view names each round's prompt and its size, so a full-text excerpt belongs to --round N (and would put an unbounded amount of text in a listing):\n%s", out)
	}
	// THE ORDER IS THE SESSION'S, asserted on the view's own numbering rather
	// than on the position of two strings inside the output: `1  round` must
	// come before `2  round`, and the first round is the one that carried the
	// request.
	first := strings.Index(out, "round 1/")
	second := strings.Index(out, "round 2/")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("the rounds are listed out of order (round 1 at %d, round 2 at %d):\n%s", first, second, out)
	}
}

// TestSessionReplayStatic_RoundRendersPlainText asserts the static view is PLAIN: no
// escape sequences, so reading one round never requires interpreting ANSI.
func TestSessionReplayStatic_RoundRendersPlainText(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", "1")
	if err != nil {
		t.Fatalf("--round 1 failed: %v\n%s", err, out)
	}
	// ONE ROUND IN FULL: the request it answered, its reasoning, its prose and
	// its tool call with the tool's own answer — everything the old per-row view
	// split across steps or dropped.
	for _, want := range []string{"a recorded question", "weighing the options", "a recorded answer", "echo hi", "hi from the tool"} {
		if !strings.Contains(out, want) {
			t.Fatalf("--round 1 does not carry %q — a round shows its whole content:\n%s", want, out)
		}
	}
	if strings.Contains(out, "the second answer") {
		t.Fatalf("--round 1 leaked the SECOND round's content:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("--round emitted an escape sequence; the static view must be plain text:\n%q", out)
	}
}

// TestSessionReplayStatic_RoundSelectorMatchesTheLogSurface is the ANTI-DRIFT
// case: `session replay --round N` resolves through internal/gopilog's own
// grammar, so the ordinal it renders is the ordinal `gopi session log <id>
// --rounds` tabulates — the two surfaces are run here and their answers
// compared, not two copies of a number typed in a test.
//
// THE WORD, THOUGH, MUST NOT MATCH (added 2026-10-11; the FLAG joined it
// 2026-10-12). The two surfaces used to print their two different quantities
// under one word: on a real log (`log --rounds` 169 request rows, `replay
// --rounds` rounds=169) the same word named an exchange on one surface and a
// human round on the other. The log surface moved the exchange table to
// `--exchanges` and gave `--rounds` to the human round, so the flag and the unit
// agree on both verbs and there is nothing left to disambiguate in prose.
//
// The comparison itself is still made, and it is now an EQUALITY rather than a
// contrast: the last round the log lists is the round count the replay reports.
func TestSessionReplayStatic_RoundSelectorMatchesTheLogSurface(t *testing.T) {
	home, id := replayTestSession(t)
	logOut, err := runGopiCapture(t, home, CliVerbSession, CliVerbLog, id, "--"+logFlagRounds)
	if err != nil {
		t.Fatalf("`session log --rounds` failed: %v\n%s", err, logOut)
	}
	// The log surface heads its table with THE ROUND's own word — the same unit
	// the replay prints as `rounds=` (the operator's ruling, 2026-10-12), so the
	// two outputs name one quantity under one flag.
	if banner := "SESSION " + gopilog.SessionRoundTableWord(); !strings.Contains(logOut, banner) {
		t.Fatalf("`session log --rounds` does not head its table %q — a reader cannot tie it to the replay's rounds:\n%s",
			banner, logOut)
	}
	if turnBanner := "SESSION " + gopilog.SessionTurnTableWord(); strings.Contains(logOut, turnBanner) {
		t.Fatalf("`session log --rounds` heads its table %q — that banner belongs to the --%s table:\n%s",
			turnBanner, logFlagTurns, logOut)
	}
	// The log listing names each round's number; the LAST one it names is the
	// session's round count, which is what the replay's own listing must agree
	// with.
	nums := regexp.MustCompile(`(?m)^\s*(?:round\s+)?(\d+)[\s:.]`).FindAllStringSubmatch(logOut, -1)
	if len(nums) == 0 {
		// The listing's spelling may differ; fall back to the count the header
		// prints, and fail loudly if neither is present rather than passing
		// vacuously.
		t.Skipf("`session log --rounds` printed no per-round numbers to compare:\n%s", logOut)
	}
	last, err := strconv.Atoi(nums[len(nums)-1][1])
	if err != nil || last < 1 {
		t.Fatalf("the log listing's last round number is %q:\n%s", nums[len(nums)-1][1], logOut)
	}
	replayOut, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--rounds")
	if err != nil {
		t.Fatalf("`session replay --rounds` failed: %v\n%s", err, replayOut)
	}
	// The playback walks one unit per TURN and `--turns` reports that count; the
	// ROUND count now lives on the internal listing under its own name. This
	// still cross-checks the two surfaces against each other: the number the log
	// tabulates as its last round is the number the internal view reports as
	// `rounds=`.
	if want := fmt.Sprintf("rounds=%d", last); !strings.Contains(replayOut, want) {
		t.Fatalf("`session log --rounds` numbers %d round(s) but `session replay --rounds` reports %q:\n%s",
			last, want, replayOut)
	}
	// And the selector reaches the LAST round by the same number the log uses.
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", strconv.Itoa(last))
	if err != nil {
		t.Fatalf("--round %d failed: %v\n%s", last, err, out)
	}
	if !strings.Contains(out, fmt.Sprintf("round %d/%d", last, last)) {
		t.Fatalf("--round %d rendered a different round than the log numbers %d:\n%s", last, last, out)
	}
}

// TestSessionReplayStatic_RefusalsNameTheReason pins the refusals that matter: an
// unknown session, an out-of-range round, TWO views at once, and a missing input
// — each must say WHAT it wanted, never guess.
func TestSessionReplayStatic_RefusalsNameTheReason(t *testing.T) {
	home, id := replayTestSession(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown session", []string{CliVerbSession, sessionSubcmdReplay, "no-such-session"}, "no session"},
		{"round out of range", []string{CliVerbSession, sessionSubcmdReplay, id, "--round", "9999"}, "out of range"},
		{"both views at once", []string{CliVerbSession, sessionSubcmdReplay, id, "--rounds", "--round", "1"}, "one"},
		{"no session named", []string{CliVerbSession, sessionSubcmdReplay}, "which session"},
		{"unknown flag", []string{CliVerbSession, sessionSubcmdReplay, id, "--nope"}, "not an option"},
		{"refused terminal", []string{CliVerbSession, sessionSubcmdReplay, id, sessionReplayTerminalFlag, "gnome-terminal"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := runGopiCapture(t, home, c.args...)
			if err == nil {
				t.Fatalf("the verb accepted %v (expected a refusal):\n%s", c.args, out)
			}
			if c.want != "" && !strings.Contains(out, c.want) {
				t.Fatalf("the refusal does not name %q:\n%s", c.want, out)
			}
		})
	}
}

// TestSessionReplayStatic_SelectorGrammarIsTheLogs is the refusal half of the
// anti-drift pair: an out-of-range selector is REFUSED rather than clamped to a
// different round — in BOTH directions — and the from-the-end spellings resolve
// to the round they name. A clamped selector would show a round the operator did
// not ask for, which is how "round 4 of 2" quietly becomes "round 2".
func TestSessionReplayStatic_SelectorGrammarIsTheLogs(t *testing.T) {
	home, id := replayTestSession(t)
	// The from-the-end spellings reach the LAST round, which the listing names.
	last, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", "0")
	if err != nil {
		t.Fatalf("--round 0 (the last round, by the log's own grammar) failed: %v\n%s", err, last)
	}
	if !strings.Contains(last, fmt.Sprintf("round %d/%d", replayTestRoundCount, replayTestRoundCount)) {
		t.Fatalf("--round 0 did not resolve to the last round (%d):\n%s", replayTestRoundCount, last)
	}
	negative, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", "-1")
	if err != nil {
		t.Fatalf("--round -1 failed: %v\n%s", err, negative)
	}
	if stripAnsiLine(negative) != stripAnsiLine(last) {
		t.Fatalf("--round -1 and --round 0 named different rounds:\n-1: %s\n 0: %s", negative, last)
	}
	// Out of range in BOTH directions, never clamped.
	for _, arg := range []string{"3", "-3", "9999"} {
		out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--round", arg)
		if err == nil {
			t.Fatalf("--round %s was accepted with only %d round(s):\n%s", arg, replayTestRoundCount, out)
		}
		if !strings.Contains(out, "out of range") {
			t.Fatalf("--round %s refused without naming the reason:\n%s", arg, out)
		}
	}
}

// stripAnsiLine drops escape sequences from a captured output (the static views
// emit none, but the pty's line discipline can add some), so two renderings of
// the same round compare equal on their text.
func stripAnsiLine(s string) string {
	return strings.TrimSpace(stripAnsi(s))
}

// TestSessionReplayKeyboard_DrivesTheTransportOverPty is THE case this verb exists for:
// a real terminal, real keys, and the terminal given back at the end.
func TestSessionReplayKeyboard_DrivesTheTransportOverPty(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	home, id := replayTestSession(t)
	d := startReplayDrive(t, home, id)
	// The first step: the alternate screen is entered and the status line names
	// the material and the position.
	d.waitFor("\x1b[?1049h", replayFirstOutputWait)
	d.waitFor(replayRunningWord, replayFirstOutputWait)

	if s := d.after(" ", time.Second); !strings.Contains(s, replayPausedWord) {
		t.Fatalf("space did not pause the playback:\n%q", tailOf(s, 300))
	}
	if s := d.after("n", time.Second); !strings.Contains(s, "2/") {
		t.Fatalf("n did not step forward:\n%q", tailOf(s, 300))
	}
	if s := d.after("\x1b[D", time.Second); !strings.Contains(s, "1/") {
		t.Fatalf("the left arrow did not step back:\n%q", tailOf(s, 300))
	}
	// End jumps to the LAST round: the total is the material's own step count,
	// read from the static view this same session supports.
	total := replayStepTotal(t, home, id)
	if s := d.after("\x1b[F", time.Second); !strings.Contains(s, fmt.Sprintf("%d/%d", total, total)) {
		t.Fatalf("End did not go to the last round (%d/%d):\n%q", total, total, tailOf(s, 300))
	}
	if s := d.after("\x1b[H", time.Second); !strings.Contains(s, "1/") {
		t.Fatalf("Home did not go to the first round:\n%q", tailOf(s, 300))
	}
	// THE IN-ROUND SCROLL IS DRIVEN FOR REAL, because a round is ONE step and can
	// be taller than the screen: PgUp at the top says so rather than looking like
	// a dead key.
	if s := d.after("\x1b[5~", time.Second); !strings.Contains(s, "top of this round") {
		t.Fatalf("PgUp at the top of a round did not report the edge:\n%q", tailOf(s, 300))
	}
	// THE VERTICAL ARROWS READ THE ROUND; THEY DO NOT STEP IT. This is the
	// contract the key map was changed for: ↓ moves the VIEW (the status line's
	// scroll marker appears, the round number does not), ↑ moves it back, and the
	// horizontal arrow is the one that changes the round.
	if s := d.after("\x1b[B", time.Second); !strings.Contains(s, replayScrollMarker) {
		t.Fatalf("↓ did not scroll a row inside the round (no %q marker):\n%q", replayScrollMarker, tailOf(s, 300))
	}
	if s := d.after("\x1b[B", time.Second); !strings.Contains(s, "1/") {
		t.Fatalf("↓ changed the round instead of the view:\n%q", tailOf(s, 300))
	}
	// ↑ moves the view back: at offset 0 the marker is GONE (it is printed only
	// when the view is scrolled), and the round is still the first one.
	if s := d.after("\x1b[A", time.Second); strings.Contains(s, replayScrollMarker) {
		t.Fatalf("↑ left the view scrolled (the %q marker is still there):\n%q", replayScrollMarker, tailOf(s, 300))
	}
	if s := d.after("\x1b[C", time.Second); !strings.Contains(s, "2/") {
		t.Fatalf("→ did not step forward one round:\n%q", tailOf(s, 300))
	}
	if s := d.after("?", time.Second); !strings.Contains(s, "pause / resume") {
		t.Fatalf("? did not open the help overlay:\n%q", tailOf(s, 300))
	}
	if s := d.after("x", time.Second); strings.Contains(s, "pause / resume") {
		t.Fatalf("a key did not return from the overlay:\n%q", tailOf(s, 300))
	}
	if s := d.after("+", time.Second); !strings.Contains(s, "2x") {
		t.Fatalf("+ did not raise the rate:\n%q", tailOf(s, 300))
	}
	if s := d.after("-", time.Second); !strings.Contains(s, "1x") {
		t.Fatalf("- did not lower the rate back:\n%q", tailOf(s, 300))
	}

	quit := d.after("q", time.Second)
	for _, want := range []string{"\x1b[?1049l", "\x1b[?25h"} {
		if !strings.Contains(quit, want) {
			t.Fatalf("quitting did not restore the terminal (missing %q):\n%q", want, tailOf(quit, 400))
		}
	}
	if code := d.waitExit(replayExitWait); code != 0 {
		t.Fatalf("the player exited %d after a quit key, want 0", code)
	}
}

// replayStepTotal reads the material's TURN count from the verb's own static
// view, so the keyboard case asserts against the SAME material it is driving
// rather than a number typed here.
func replayStepTotal(t *testing.T, home, id string) int {
	t.Helper()
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, "--turns")
	if err != nil {
		t.Fatalf("--turns: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`turns=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("--turns does not report a turn count:\n%s", out)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		t.Fatalf("--turns reported %q turns:\n%s", m[1], out)
	}
	return n
}

// tailOf renders the tail of a byte string for a failure message (the whole
// thing is escape soup).
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// runGopiCapture runs the built binary with the synthetic profile and returns
// its COMBINED output and error.
func runGopiCapture(t *testing.T, profileRoot string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(gopiTestBinPath(t), args...)
	cmd.Env = replayTestEnv(t, profileRoot)
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestSessionReplayNoTerminal_RefusesToPlayFromAPipe pins the boundary the static views
// sit on: the INTERACTIVE path needs a tty and says so, rather than reading keys
// from a pipe and appearing to hang.
func TestSessionReplayNoTerminal_RefusesToPlayFromAPipe(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id)
	if err == nil {
		t.Fatalf("the player started with no terminal:\n%s", out)
	}
	if !strings.Contains(out, "not a terminal") {
		t.Fatalf("the refusal does not name the reason:\n%s", out)
	}
}

// TestSessionReplayTerminal_RefusesAnUnusableEmulator pins the registry integration: a
// terminal the registry refuses (gnome-terminal, whose row is a refusal) must be
// refused with the registry's OWN words, never silently replaced by another
// emulator.
func TestSessionReplayTerminal_RefusesAnUnusableEmulator(t *testing.T) {
	home, id := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, id, sessionReplayTerminalFlag, "gnome-terminal")
	if err == nil {
		t.Fatalf("the verb accepted a refused terminal:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "gnome") {
		t.Fatalf("the refusal does not name the terminal:\n%s", out)
	}
}

// TestSessionReplayHelpPageCarriesTheKeyMap ties the usage page to the bindings: every
// action the help page shows must be an action the decoder knows, printed from
// the ONE list both read.
func TestSessionReplayHelpPageCarriesTheKeyMap(t *testing.T) {
	home, _ := replayTestSession(t)
	out, err := runGopiCapture(t, home, CliVerbSession, sessionSubcmdReplay, "-h")
	if err != nil {
		t.Fatalf("-h failed: %v\n%s", err, out)
	}
	for _, row := range replayplay.KeyMapRows {
		if !strings.Contains(out, row[1]) {
			t.Fatalf("the usage page is missing the action %q:\n%s", row[1], out)
		}
	}
	if !strings.Contains(out, sessionReplayUsageHead) {
		t.Fatalf("the usage page does not open with %q:\n%s", sessionReplayUsageHead, out)
	}
}
