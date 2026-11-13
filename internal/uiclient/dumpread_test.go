package uiclient

// dumpread_test.go — the READ side: discovery, identity, ambiguity refusal and
// the exited-target diagnosis.
//
// NO PIXELS, NO VISION anywhere in this file: every assertion is made against
// the interpreted CELL GRID the surface itself exported — the whole point of the
// capability. A screen_capture/vision_analyze step here would be measurably
// worse and would make the test assert on a different artifact than the one the
// product ships.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
)

// The fixtures below build REAL dump files with the product's own writer
// (NewScreenDumper), never a hand-typed copy of the format: a fixture that
// spelled the record line itself would keep passing after the format changed,
// and the reader would be tested against a shape no surface writes.

// writeFixtureDump writes a dump at path whose identity names pid + started,
// paints one line of text, and returns the path.
func writeFixtureDump(t *testing.T, path string, pid int, started time.Time, target, line string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	d, err := NewScreenDumper(DumpConfig{
		Path: path,
		Cols: 40,
		Rows: 6,
		Identity: DumpIdentity{
			PID:              pid,
			Target:           target,
			Tools:            true,
			ColsRows:         [2]int{40, 6},
			TerminalColsRows: [2]int{40, 6},
			Started:          started,
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	// The sink's throttle is driven by its own clock (dumpMinInterval), so the
	// fixture advances it past the interval rather than sleeping: the test is
	// about what a reader finds, not about the refresh rate.
	fake := int64(0)
	d.now = func() time.Time {
		fake++
		return time.Unix(1_800_000_000+fake*3600, 0)
	}
	if _, err := d.Write([]byte("\x1b[2J\x1b[H" + line + "\r\nsecond row\r\n")); err != nil {
		t.Fatalf("write fixture dump: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the fixture dump was not installed at %s: %v", path, err)
	}
	return path
}

// selfStamp is this process's own verifiable start instant — the value that
// makes a fixture dump read back as LIVE.
func selfStamp(t *testing.T) time.Time {
	t.Helper()
	started, err := ProcessStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessStartTime(%d): %v", os.Getpid(), err)
	}
	return started
}

// TestReadDumpLiveAndExited pins the two verdicts a reader must never confuse:
// a dump whose writer is alive and IS that incarnation reads live; the same dump
// after the writer is gone reads EXITED, carrying the diagnosis sentence — never
// an empty screen reported as fact.
func TestReadDumpLiveAndExited(t *testing.T) {
	dir := t.TempDir()
	live := writeFixtureDump(t, filepath.Join(dir, "live", DumpFileName), os.Getpid(), selfStamp(t), "http://127.0.0.1:9999", "hello live surface")

	cand, err := ReadDump(live)
	if err != nil {
		t.Fatalf("ReadDump(live): %v", err)
	}
	if cand.State != StateLive {
		t.Fatalf("state = %q, want %q (reason: %s)", cand.State, StateLive, cand.Reason)
	}
	if cand.Record.PID != os.Getpid() {
		t.Errorf("record pid = %d, want %d", cand.Record.PID, os.Getpid())
	}
	if cand.Record.Target != "http://127.0.0.1:9999" {
		t.Errorf("record target = %q", cand.Record.Target)
	}
	if !strings.Contains(strings.Join(cand.Dump.Lines, "\n"), "hello live surface") {
		t.Errorf("the dump does not carry the painted line: %q", cand.Dump.Lines)
	}

	// A dump stamped EXACTLY like this process but recorded under a pid that
	// cannot exist is the exited case: the file is complete, the surface is not.
	goneStamp := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	exited := writeFixtureDump(t, filepath.Join(dir, "gone", DumpFileName), unlivablePID, goneStamp, "http://127.0.0.1:9998", "last frame of a departed surface")
	candGone, err := ReadDump(exited)
	if err != nil {
		t.Fatalf("ReadDump(exited): %v", err)
	}
	if candGone.State != StateExited {
		t.Fatalf("state = %q, want %q (reason: %s)", candGone.State, StateExited, candGone.Reason)
	}
	if candGone.Reason != ExitedNotice {
		t.Errorf("the exited verdict's reason = %q, want the fixed diagnosis %q", candGone.Reason, ExitedNotice)
	}
	if len(candGone.Dump.Lines) == 0 {
		t.Error("the exited dump lost its grid — the snapshot must survive its writer")
	}
}

// unlivablePID is a pid no process of this host can hold: the kernel's pid
// ceiling (pid_max ≤ 2^22) means a number above it is provably absent — a
// reliable "the writer is gone" fixture that does not depend on killing
// anything.
const unlivablePID = 1 << 24

// TestReadDumpForeignForRecycledPID pins the check that makes the identity
// unforgeable: a pid that IS alive but whose recorded start instant is not the
// live process's is a RECYCLED pid — the dump is somebody else's snapshot, and
// calling it live would attribute a screen to a process that never painted it.
func TestReadDumpForeignForRecycledPID(t *testing.T) {
	dir := t.TempDir()
	path := writeFixtureDump(t, filepath.Join(dir, DumpFileName), os.Getpid(),
		time.Now().Add(-8*time.Hour).UTC().Truncate(time.Second), "http://127.0.0.1:9997", "somebody else's screen")

	cand, err := ReadDump(path)
	if err != nil {
		t.Fatalf("ReadDump: %v", err)
	}
	if cand.State != StateForeign {
		t.Fatalf("state = %q, want %q (reason: %s)", cand.State, StateForeign, cand.Reason)
	}
	if !strings.Contains(cand.Reason, "recycled") {
		t.Errorf("the foreign verdict must name the cause (a recycled pid), got %q", cand.Reason)
	}
}

// TestReadDumpUnreadableIsNotAnEmptyScreen pins Rule 15 on the read path: a file
// that is not a dump is reported as UNREADABLE, never as a screen with nothing
// on it — the difference between "this surface is blank" and "I could not read
// this file" is exactly the difference that gets reported as fact.
func TestReadDumpUnreadableIsNotAnEmptyScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DumpFileName)
	if err := os.WriteFile(path, []byte("not a dump at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cand, err := ReadDump(path)
	if err == nil {
		t.Fatal("ReadDump accepted a file that is not a dump")
	}
	if cand.State != StateUnreadable {
		t.Fatalf("state = %q, want %q", cand.State, StateUnreadable)
	}
	if len(cand.Dump.Lines) != 0 || cand.Dump.Cols != 0 {
		t.Error("an unreadable file must not produce a screen value")
	}
}

// TestUnpaintedDumpIsNotAnEmptyScreen is the frames=0 regression guard.
//
// MEASURED, and the reason this test exists: an E2E run that quit the TUI with
// Ctrl+C left a dump whose sink had interpreted ZERO frames, and the reader
// reported its all-blank grid as a plain screen — indistinguishable, in the
// output, from "the surface is showing an empty grid". bubbletea treats Ctrl+C
// as an abort and erases its panel on the way out, so that empty grid is a
// former screen, and a caller — or a report — asserting layout on it asserts on
// nothing. The artifact does carry the fact (the sink counts the frames it
// interpreted), so the reader must surface it rather than let the grid speak
// for itself.
func TestUnpaintedDumpIsNotAnEmptyScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DumpFileName)
	d, err := NewScreenDumper(DumpConfig{
		Path: path,
		Cols: 40,
		Rows: 6,
		Identity: DumpIdentity{
			PID:              os.Getpid(),
			Target:           "http://127.0.0.1:9001",
			ColsRows:         [2]int{40, 6},
			TerminalColsRows: [2]int{40, 6},
			Started:          selfStamp(t),
		},
	})
	if err != nil {
		t.Fatalf("NewScreenDumper(%s): %v", path, err)
	}
	// No Write at all: exactly the state the constructor installs before the
	// first frame — the shape the Ctrl+C run left behind.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the constructor did not install a dump at %s: %v", path, err)
	}

	cand, err := ReadDump(path)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", path, err)
	}
	if !cand.Unpainted() {
		t.Fatalf("frames=%d — a dump no frame was written to must read back unpainted", cand.Frames)
	}
	if cand.Frames != UnpaintedFrames {
		t.Errorf("frames = %d, want the named boundary %d", cand.Frames, UnpaintedFrames)
	}
	if cand.Unpainted() == false && cand.Frames == UnpaintedFrames {
		t.Error("frames == UnpaintedFrames must report unpainted")
	}
	if !strings.Contains(strings.ToLower(UnpaintedNotice), "not evidence") {
		t.Errorf("the notice must DENY the empty-screen reading, got %q", UnpaintedNotice)
	}
	// And the painted case must NOT be flagged: a check that always fires is a
	// check that answers nothing.
	painted := writeFixtureDump(t, filepath.Join(dir, "painted", DumpFileName), os.Getpid(), selfStamp(t),
		"http://127.0.0.1:9002", "gopi ui → painted")
	pc, err := ReadDump(painted)
	if err != nil {
		t.Fatalf("ReadDump(%s): %v", painted, err)
	}
	if pc.Unpainted() {
		t.Errorf("a dump whose sink wrote %d frames must not read back unpainted", pc.Frames)
	}
	if pc.Dumps == 0 {
		t.Error("a painted dump must carry a non-zero installed-dump count")
	}
	_ = d
}

// TestSelectLiveRefusesOnAmbiguity is the REFUSAL contract: two live surfaces are
// two screens. The verdict must be an *AmbiguityError that LISTS both (a caller
// told only "ambiguous" cannot resolve it), and the scan's own order must not
// decide for the caller.
func TestSelectLiveRefusesOnAmbiguity(t *testing.T) {
	dir := t.TempDir()
	stamp := selfStamp(t)
	// Two DIFFERENT targets under this pid: the same trick the product uses to
	// make two surfaces distinguishable (the identity is pid+started+target).
	for i, target := range []string{"http://127.0.0.1:8001", "http://127.0.0.1:8002"} {
		writeFixtureDump(t, filepath.Join(dir, "suite", string(rune('a'+i)), DumpFileName),
			os.Getpid(), stamp, target, "surface "+target)
	}
	cands, truncated, err := Candidates([]string{dir}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if truncated {
		t.Error("the scan reported truncation for two files")
	}
	if len(cands) != 2 {
		t.Fatalf("scan found %d candidates, want 2: %+v", len(cands), cands)
	}
	sel, selErr := SelectLive(cands, truncated)
	var ambiguous *AmbiguityError
	if !errors.As(selErr, &ambiguous) {
		t.Fatalf("SelectLive returned %v, want an *AmbiguityError", selErr)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Fatalf("the refusal lists %d candidates, want both", len(ambiguous.Candidates))
	}
	// The refusal must name the pids and the targets: that is what makes it
	// resolvable via --pid.
	msg := selErr.Error()
	for _, want := range []string{"refusing to guess", "--pid", "http://127.0.0.1:8001", "http://127.0.0.1:8002", "started="} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not carry %q:\n%s", want, msg)
		}
	}
	if sel.Candidate.Path != "" {
		t.Errorf("a refusal must not also choose: it chose %s", sel.Candidate.Path)
	}
}

// TestAmbiguityErrorHeadlineNamesTheCandidatesOwnState is the regression guard
// for the headline's ADJECTIVE.
//
// The refusal used to print ONE headline for every case — "N live gopi surfaces
// could be the target" — which is true of a live pair and FALSE of a pair of
// dead ones, whose very next lines read `state=exited`. A caller handed that
// contradiction has to reconcile the first line against the table itself; the
// headline must instead be derived from the candidates' own State verdicts.
//
// Both sides are pinned: a non-live set must never be called live, and the live
// pair must keep the EXACT sentence callers quote.
func TestAmbiguityErrorHeadlineNamesTheCandidatesOwnState(t *testing.T) {
	const (
		wantLive   = "2 live gopi surfaces could be the target; refusing to guess — pass --pid (or --list) to choose:"
		wantExited = "2 exited gopi surfaces could be the target; refusing to guess — pass --pid (or --list) to choose:"
		wantMixed  = "2 gopi surfaces of mixed state (exited 1, foreign 1) could be the target; refusing to guess — pass --pid (or --list) to choose:"
	)
	gone := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	livePair := func(dir string) {
		stamp := selfStamp(t)
		for i, target := range []string{"http://127.0.0.1:8001", "http://127.0.0.1:8002"} {
			writeFixtureDump(t, filepath.Join(dir, "pair", string(rune('a'+i)), DumpFileName),
				os.Getpid(), stamp, target, "surface "+target)
		}
	}

	cases := []struct {
		name  string
		write func(dir string)
		want  string
		state State
	}{
		{
			name:  "a live pair keeps the sentence callers quote",
			write: livePair,
			want:  wantLive,
			state: StateLive,
		},
		{
			name: "two DEAD surfaces are never called live",
			write: func(dir string) {
				for i, target := range []string{"http://127.0.0.1:8001", "http://127.0.0.1:8002"} {
					// Two pids above the kernel's own ceiling are two provably
					// absent writers: the exited case without killing anything.
					writeFixtureDump(t, filepath.Join(dir, "dead", string(rune('a'+i)), DumpFileName),
						unlivablePID+i, gone, target, "last frame "+target)
				}
			},
			want:  wantExited,
			state: StateExited,
		},
		{
			name: "a mixed set states its composition",
			write: func(dir string) {
				writeFixtureDump(t, filepath.Join(dir, "mix", "dead", DumpFileName),
					unlivablePID, gone, "http://127.0.0.1:8001", "last frame of a departed surface")
				// This process HOLDS the pid but not the recorded start instant:
				// a recycled pid, which is the foreign verdict.
				writeFixtureDump(t, filepath.Join(dir, "mix", "recycled", DumpFileName),
					os.Getpid(), gone, "http://127.0.0.1:8002", "another surface's snapshot")
			},
			want:  wantMixed,
			state: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.write(dir)
			cands, _, err := Candidates([]string{dir}, time.Now())
			if err != nil {
				t.Fatalf("Candidates: %v", err)
			}
			if len(cands) != 2 {
				t.Fatalf("scan found %d candidates, want 2", len(cands))
			}
			if tc.state != "" {
				for _, c := range cands {
					if c.State != tc.state {
						t.Fatalf("candidate %s state = %q, want %q (reason: %s)", c.Path, c.State, tc.state, c.Reason)
					}
				}
			}
			_, selErr := SelectLive(cands, false)
			var ambiguous *AmbiguityError
			if !errors.As(selErr, &ambiguous) {
				t.Fatalf("SelectLive returned %v, want an *AmbiguityError", selErr)
			}
			msg := selErr.Error()
			got := strings.SplitN(msg, "\n", 2)[0]
			if got != tc.want {
				t.Errorf("headline =\n  %s\nwant\n  %s", got, tc.want)
			}
			for _, c := range ambiguous.Candidates {
				if c.State != StateLive && strings.Contains(got, string(StateLive)) {
					t.Errorf("the headline claims %q for a %s surface: %s", StateLive, c.State, got)
				}
			}
			// The listing is the half of the refusal a caller ACTS on (it is
			// what makes --pid resolvable), so it must survive the change.
			for _, want := range []string{"refusing to guess", "--pid", "started="} {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal no longer carries %q:\n%s", want, msg)
				}
			}
		})
	}
}

// TestSelectLiveSingleCandidateIsNotAmbiguous pins the other side of the
// refusal: ONE candidate is answered directly, and — when that one is EXITED —
// the answer carries the exited verdict rather than being suppressed. A single
// dead surface is still the answer to "what did it paint", and reporting
// nothing would be the empty-screen failure in another disguise.
func TestSelectLiveSingleCandidateIsNotAmbiguous(t *testing.T) {
	dir := t.TempDir()
	writeFixtureDump(t, filepath.Join(dir, DumpFileName), unlivablePID,
		time.Now().Add(-time.Hour).UTC().Truncate(time.Second), "http://127.0.0.1:9100", "gone already")

	cands, _, err := Candidates([]string{dir}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	sel, selErr := SelectLive(cands, false)
	if selErr != nil {
		t.Fatalf("SelectLive on one candidate refused: %v", selErr)
	}
	if sel.Candidate.State != StateExited {
		t.Fatalf("state = %q, want %q", sel.Candidate.State, StateExited)
	}
}

// TestSelectByPIDIsUnambiguous — an explicit pid is the route that resolves an
// ambiguity, and a miss must NOT fall back to "the dump I did find": a silent
// substitution is how a caller ends up asserting on a screen it did not ask
// about.
func TestSelectByPIDIsUnambiguous(t *testing.T) {
	dir := t.TempDir()
	stamp := selfStamp(t)
	writeFixtureDump(t, filepath.Join(dir, "a", DumpFileName), os.Getpid(), stamp, "http://127.0.0.1:7001", "first")
	writeFixtureDump(t, filepath.Join(dir, "b", DumpFileName), 424242, time.Now().Add(-time.Minute).UTC().Truncate(time.Second), "http://127.0.0.1:7002", "second")

	cands, _, err := Candidates([]string{dir}, time.Now())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	sel, err := SelectByPID(cands, 424242)
	if err != nil {
		t.Fatalf("SelectByPID(424242): %v", err)
	}
	if sel.Candidate.Record.PID != 424242 {
		t.Fatalf("chose pid %d", sel.Candidate.Record.PID)
	}
	if _, err := SelectByPID(cands, 999999); err == nil {
		t.Fatal("SelectByPID accepted a pid no dump carries — a fallback substitution")
	}
}

// TestScanRootsCoverBothDumpLocations — discovery must look where the surface
// actually writes: the profile's sessions tree AND the ui-session runtime
// scratch class (the `--server` shape). A root list that missed either would
// make the verb silently blind to half its targets.
func TestScanRootsCoverBothDumpLocations(t *testing.T) {
	gopiDir := "/tmp/gopi-profile"
	home := "/tmp/gopi-scratch"
	roots := ScanRoots(gopiDir, home)
	joined := strings.Join(roots, "\n")
	if !strings.Contains(joined, filepath.Join(gopiDir, config.SessionsDirName)) {
		t.Errorf("the sessions tree is not a scan root:\n%s", joined)
	}
	if !strings.Contains(joined, dumpRuntimeEntryName(dumpUISessionClass, 0)) {
		t.Errorf("the ui-session runtime class is not a scan root:\n%s", joined)
	}
}

// TestCandidatesReportsNoDumpDistinctly — "nothing to observe" is its own
// answer (ErrNoCandidates), not an empty screen: the caller must be able to say
// "no dump" instead of printing an empty grid as if it were one.
func TestCandidatesReportsNoDumpDistinctly(t *testing.T) {
	cands, _, err := Candidates([]string{t.TempDir()}, time.Now())
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("Candidates on an empty tree returned %v, want ErrNoCandidates", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected no candidates, got %d", len(cands))
	}
}

// TestCandidateScanFindsDumpUnderRuntimeClass — the scan walks a nested tree
// (the sessions layout is <root>/sessions/<id>/ and the scratch layout is
// <home>/queue/queue-runtime-ui-session-<pid>/) and ignores the .json twin, so
// one observation never sees double.
func TestCandidateScanFindsDumpUnderRuntimeClass(t *testing.T) {
	home := t.TempDir()
	classDir := filepath.Join(home, dumpQueueDirName, dumpRuntimeEntryName(dumpUISessionClass, 4711))
	path := writeFixtureDump(t, filepath.Join(classDir, DumpFileName), os.Getpid(), selfStamp(t), "http://127.0.0.1:5555", "runtime class surface")
	if _, err := os.Stat(path + dumpJSONSuffix); err != nil {
		t.Fatalf("the fixture's JSON twin is missing: %v", err)
	}
	roots := ScanRoots(filepath.Join(home, "profile"), home)
	cands, _, err := Candidates(roots, time.Now())
	if err != nil {
		t.Fatalf("Candidates(%v): %v", roots, err)
	}
	if len(cands) != 1 {
		t.Fatalf("scan found %d candidates, want exactly the dump (not its .json twin): %+v", len(cands), cands)
	}
	if cands[0].State != StateLive {
		t.Errorf("state = %q (reason %s)", cands[0].State, cands[0].Reason)
	}
	if pid, ok := ClassPID(filepath.Base(classDir)); !ok || pid != 4711 {
		t.Errorf("ClassPID(%q) = %d, %v; want 4711, true", filepath.Base(classDir), pid, ok)
	}
	if pid, ok := ClassPID(dumpRuntimeEntryName(dumpUISessionClass, 0)); ok {
		t.Errorf("the class prefix reported pid %d; a prefix carries no pid", pid)
	}
}

// TestParseDumpRecordIsFieldNamed — the record is parsed BY NAME, so a writer
// that adds a field cannot shift this reader onto its neighbour.
func TestParseDumpRecordIsFieldNamed(t *testing.T) {
	line := DumpRecordPrefix + " pid=4711 target=http://127.0.0.1:8123 tools=on grid=100x30 mode=ansi future_field=whatever started=2026-09-24T10:00:00Z"
	rec, err := ParseDumpRecord(line)
	if err != nil {
		t.Fatalf("ParseDumpRecord: %v", err)
	}
	if rec.PID != 4711 || rec.Target != "http://127.0.0.1:8123" || !rec.Tools {
		t.Fatalf("record = %+v", rec)
	}
	if !rec.Started.Equal(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("started = %s", rec.Started)
	}
	if rec.Fields["future_field"] != "whatever" {
		t.Errorf("an unmodelled field is not carried: %+v", rec.Fields)
	}
	if _, err := ParseDumpRecord("0|some grid row"); err == nil {
		t.Error("a line that is not the record was accepted")
	}
}
