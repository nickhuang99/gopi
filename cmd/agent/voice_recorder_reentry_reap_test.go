package main

// voice_recorder_reentry_reap_test.go — ONE UNREAPED RECORDER CHILD PER `/voice`
// ENTRY: the `/voice` re-entry leak, measured on a live `gopi chat` of this tree.
//
// THE DEFECT, as it appeared in the process table (2026-10-26, host host-a,
// the chat's own binary revision b9be76f — whose four recorder files are
// BYTE-IDENTICAL to this tree, so the leak is live in current master):
//
//	$ pgrep -x parecord | wc -l
//	14
//	$ ps -o pid,ppid,stat,lstart,cmd -p $(pgrep -x parecord | paste -sd,)
//	    PID    PPID STAT                  STARTED CMD
//	 431808  331978 Z    Sun Oct 25 06:15:12 2026 [parecord] <defunct>
//	 …
//	2212490  331978 Z    Sun Oct 25 20:42:20 2026 [parecord] <defunct>
//	$ ps -o pid,stat,etime,cmd -p 331978
//	 331978 Sl+     18:09:13 gopi chat --resume f729d555-…
//
// Fourteen ZOMBIES, every one a child of ONE long-lived chat process, one per
// `/voice` recording, accumulating across an 18-hour session.
//
// WHY THE PREVIOUS FIX DID NOT COVER IT (task 01a163df, 2026-10-22, added the
// forced Kill+Wait inside stopVoiceRecorder and Setpgid on the spawn). That fix
// made ONE teardown correct. It did not make the teardown REACHABLE a second
// time: voiceChatRecorderStaged's teardown guards (`readerOnce`, `stopOnce`,
// `closeOnce`) are fields of the OBJECT, and the object is built ONCE per chat
// session (cli_chat.go: newChatVoiceMode(input, chatVoiceRecorder(...))) while
// `/voice` is a TOGGLE that can be entered any number of times. So:
//
//	entry 1  Select → spawns recorder #1, readerOnce/closeOnce armed
//	         Close  → stopReader + stream.Close → recorder #1 reaped   ✓
//	entry 2  Select → spawns recorder #2, re-assigns r.stream
//	         Close  → closeOnce.Do(…) is ALREADY SPENT → no-op
//	                → recorder #2 is never signalled and never Wait()ed
//	                → it exits on its own and nobody reaps it          ✗ <defunct>
//
// `readerOnce` is spent for the same reason, so entry 2's capture has no reader
// either: its recorder writes into a pipe nobody drains, hits EOF and becomes
// exactly the Z-state row measured above.
//
// THE CRITERION IS THE PROCESS TABLE, NOT A SIGNAL. A zombie answers
// kill(pid, 0) with success, so "did we reap it" is only answerable by ESRCH
// plus the absence of /proc/<pid> — the same criterion the sibling guard
// (voice_recorder_reap_test.go) uses.
//
// EVERY BOUND IS A NAMED CONSTANT (Rule 1) and every wait POLLS the process
// table; no verdict rests on a sleep.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// voiceReentryPidEnv carries the pid file's path into the stand-in recorder.
	// The staged source pins ONLY the locale names (tools.ChildLocaleEnv strips
	// and appends four gettext selectors), so an ordinary variable set here
	// reaches the child — which is what lets the case learn the pid the
	// production spawn chose without a test-only seam in the code under test.
	voiceReentryPidEnv = "GOPI_RECORDER_STUB_PIDFILE"

	// voiceReentryPactlLine is ONE `pactl list short sources` row: index, name,
	// driver, spec, state (tab separated, as voiceParsePactlSources expects). A
	// non-monitor name makes the Pulse default-source wildcard the first
	// enumerated device, which is the tier-4 selection a real chat takes — and
	// that device is EXEMPT from voiceVerifyDevice, so this case's recorder count
	// is exactly "one spawn per entry" instead of being polluted by liveness
	// probes.
	voiceReentryPactlLine = "0\tstubmic\tmodule-x\tfloat32le 1ch 48000Hz\tRUNNING"
	// voiceReentryPactlDefaultSource is what `pactl get-default-source` answers.
	voiceReentryPactlDefaultSource = "stubmic"
	// voiceReentryPactlServerMarker is the substring voicePulseServerUp looks
	// for. It is present, so tier 3 ("ALSA default") is skipped and the run goes
	// straight to tier 4 — the chat's own ordering on a desktop with a server up.
	voiceReentryPactlServerMarker = "Server Name: stub"

	// voiceReentryPCMBytes is how much PCM the stand-in produces before it
	// exits: ~1.2 s of the 16 kHz mono PCM16 capture contract, i.e. one full
	// window at the bound the case asks for plus the bytes that close it. The
	// stand-in then EXITS ON ITS OWN, which is the shape that turns "never
	// Wait()ed" into a Z-state row rather than into a live process — the state
	// measured on the live chat.
	voiceReentryPCMBytes = 38400
	// voiceReentryWindowSeconds is the window bound the case's Round asks for.
	voiceReentryWindowSeconds = 1
	// voiceReentryReapWait is the window the process-table check is polled in.
	// A reaped pid leaves /proc immediately, so this is a readiness window and
	// not a budget.
	voiceReentryReapWait = 3 * time.Second
	// voiceReentryPoll is that poll's step.
	voiceReentryPoll = 20 * time.Millisecond
)

// voiceReentryStubScript is the stand-in recorder: announce the pid it was
// given, emit `pcmBytes` bytes of PCM, then exit. It is deliberately NOT a
// forever process — a live leftover and an unreaped exit are different defects,
// and the measured one is the second.
//
// `pcmBytes` 0 is the NO-AUDIO device: the child opens, produces nothing and
// exits. That is not a contrived shape — it is the muted/wrong-device case the
// capture paths already classify ("produced no audio … the device is reachable
// but silent"), and it is the arm the epic names explicitly, because a recorder
// that exits OF ITS OWN before any window is cut takes a different exit path
// through the recorder than one the operator's leave stops.
func voiceReentryStubScript(pcmBytes int) string {
	head := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$$\" >> \"$%s\"\n", voiceReentryPidEnv)
	if pcmBytes <= 0 {
		return head + "exit 0\n"
	}
	return head + fmt.Sprintf("exec dd if=/dev/zero bs=%d count=1 2>/dev/null\n", pcmBytes)
}

// voiceReentryStubFile writes one executable stand-in into dir.
func voiceReentryStubFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("cannot install the %s stand-in: %v", name, err)
	}
}

// voiceReentryStubs installs the three stand-ins the production path resolves by
// NAME (pactl for enumeration, arecord for the ALSA listing, parecord for the
// capture) in front of everything else on PATH, and returns the pid file's path.
// `pcmBytes` is what the stand-in recorder emits (0 = a device with no audio).
//
// NO PRODUCTION SEAM IS ADDED FOR THIS: the code under test execs those names,
// so a directory earlier on PATH is reached with the production code unmodified
// (the same technique voice_staged_recorder_locale_test.go uses).
func voiceReentryStubs(t *testing.T, pcmBytes int) string {
	t.Helper()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "recorder-pids")
	if err := os.WriteFile(pidFile, nil, 0o600); err != nil {
		t.Fatalf("cannot create the pid file: %v", err)
	}
	voiceReentryStubFile(t, dir, voicePactlBinary, fmt.Sprintf(
		"#!/bin/sh\ncase \"$1\" in\n  list) printf '%%s\\n' '%s' ;;\n  get-default-source) printf '%%s\\n' '%s' ;;\n  info) printf '%%s\\n' '%s' ;;\nesac\nexit 0\n",
		voiceReentryPactlLine, voiceReentryPactlDefaultSource, voiceReentryPactlServerMarker))
	voiceReentryStubFile(t, dir, voiceArecordBinary, "#!/bin/sh\nexit 0\n")
	voiceReentryStubFile(t, dir, voiceParecordBinary, voiceReentryStubScript(pcmBytes))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(voiceReentryPidEnv, pidFile)
	return pidFile
}

// voiceReentryAwaitPids waits until the stand-in has announced `want` recorder
// pids, and returns the pids it saw.
//
// IT WAITS FOR THE ARTIFACT THE ASSERTION READS rather than sampling it: a
// spawned child announces its own pid a few microseconds after exec, so reading
// the file immediately after the round is a race with the child's own startup,
// and a lost race would be reported as "no recorder was spawned" — a different
// claim from the one this case makes. The bound is voiceReentryReapWait because
// the child either announces itself promptly or never runs at all.
func voiceReentryAwaitPids(pidFile string, want int) []int {
	deadline := time.Now().Add(voiceReentryReapWait)
	for {
		raw, _ := os.ReadFile(pidFile) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
		pids := parseVoiceReentryPids(string(raw))
		if len(pids) >= want {
			return pids
		}
		if time.Now().After(deadline) {
			return pids
		}
		time.Sleep(voiceReentryPoll) //stylegate:allow-sleep this IS the poll: the loop re-reads the process table / the child's pid file and returns the moment the condition holds, so the sleep is its step, not a fixed delay standing in for an event
	}
}

// parseVoiceReentryPids turns the pid file's lines into pids, skipping blank
// lines and a line the child had not finished writing.
func parseVoiceReentryPids(raw string) []int {
	var pids []int
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// voiceReentryAwaitReap polls the process table until pid is GONE, and reports
// whether it got there inside voiceReentryReapWait.
//
// THE CRITERION, and why it is the process table: a zombie still answers
// kill(pid, 0) with success and still has a /proc entry whose state field is Z.
// Asserting ESRCH plus the absence of /proc is the only reading that separates
// "reaped" from "left in the table", which is exactly what the live chat's
// fourteen <defunct> rows disprove.
func voiceReentryAwaitReap(pid int) bool {
	deadline := time.Now().Add(voiceReentryReapWait)
	for {
		gone := syscall.Kill(pid, 0) == syscall.ESRCH
		if gone {
			if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(voiceReentryPoll) //stylegate:allow-sleep this IS the poll: the loop re-reads the process table / the child's pid file and returns the moment the condition holds, so the sleep is its step, not a fixed delay standing in for an event
	}
}

// voiceReentryChildState names what the table says about pid, for the failure
// message: the row's state character is the fact an operator reads
// (`Z` = exited, unreaped; `S`/`R` = still running and still ours).
func voiceReentryChildState(pid int) string {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "gone from /proc"
	}
	line := string(raw)
	// The state is the third field; the second (comm) is parenthesised and may
	// itself contain spaces, so the split is taken after the LAST ')'.
	if i := strings.LastIndexByte(line, ')'); i >= 0 && i+2 < len(line) {
		return "state=" + string(line[i+2])
	}
	return "state unreadable: " + line
}

// voiceReentryRecorderCmd digs the exec.Cmd out of the recorder's live capture,
// so the case can reap a child it deliberately left behind (a RED run's
// leftover). It is test-side introspection of an unexported field, which is the
// same door the sibling guards use — and it is NOT a production seam.
func voiceReentryRecorderCmd(rec *voiceChatRecorderStaged) *exec.Cmd {
	if rec == nil || rec.stream == nil || rec.stream.source == nil {
		return nil
	}
	src, ok := rec.stream.source.(*voiceStagedRecorderSource)
	if !ok {
		return nil
	}
	return src.cmd
}

// TestVoiceChatRecorderReapsEveryEntrysRecorder is THE re-entry guard: TWO
// `/voice` entries over ONE recorder object — the production shape, since the
// mode's recorder is built once per chat session — and each entry's recorder
// child must be GONE from the process table once that entry has been left.
//
// The first entry is the control: it passes on the pre-fix tree, so a failure
// here is about the SECOND entry and not about teardown in general. The second
// entry is the defect: `closeOnce`/`readerOnce` are object-scoped, so entry 2
// spawns a recorder that is never signalled and never Wait()ed. MEASURED before
// the fix, verbatim:
//
//	--- FAIL: TestVoiceChatRecorderReapsEveryEntrysRecorder (3.04s)
//	    entry 2: recorder pid 3431484 is still in the process table after the entry
//	    was left (state=Z) — …
func TestVoiceChatRecorderReapsEveryEntrysRecorder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pcmBytes int
		// wantsAudio says whether the round is expected to produce a transcript
		// at all. The no-audio device (a muted input, a wrong device) must be
		// REPORTED as such and must ALSO leave no child behind — the epic names
		// that arm explicitly, because a recorder that exits of its own before a
		// window is cut takes a different exit path through the recorder.
		wantsAudio bool
	}{
		{name: "a capture that produced audio", pcmBytes: voiceReentryPCMBytes, wantsAudio: true},
		{name: "a device that produced no audio", pcmBytes: 0, wantsAudio: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			voiceReentryRunEntries(t, tc.pcmBytes, tc.wantsAudio)
		})
	}
}

// voiceReentryRunEntries drives TWO entries over ONE recorder object and asserts
// that each entry's recorder child is gone from the process table after that
// entry was left.
func voiceReentryRunEntries(t *testing.T, pcmBytes int, wantsAudio bool) {
	t.Helper()
	pidFile := voiceReentryStubs(t, pcmBytes)
	rec := chatVoiceRecorder("")
	rec.engine = func(*voiceOptions, string) (string, time.Duration, error) {
		return "stub transcript", time.Millisecond, nil
	}
	t.Cleanup(func() {
		// A RED run leaves entry 2's child unreaped — and a case that leaves the
		// very state its subject is about would be lying to the suite's own child
		// sweep. Reap it here (the code under test owns Wait, so this only bites
		// on a failure) and make sure no capture is left open.
		rec.Close()
		if cmd := voiceReentryRecorderCmd(rec); cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill() //stylegate:ignore fixture teardown; the child is expected to be gone already
			_ = cmd.Wait()         //stylegate:ignore reaping it is the point; a second Wait is a no-op
		}
	})

	for entry := 1; entry <= 2; entry++ {
		if _, _, err := rec.Select(""); err != nil {
			t.Fatalf("entry %d: the production recorder refused the stand-in capture: %v", entry, err)
		}
		// ONE REAL ROUND, so this is a capture that actually recorded rather than
		// an open/close pair: the reader is started (entry 1) and the window is
		// cut and transcribed through the engine seam.
		_, roundErr := rec.Round(&voiceOptions{}, voiceReentryWindowSeconds)
		if wantsAudio {
			if roundErr != nil && !isVoiceStreamEnd(roundErr) {
				t.Fatalf("entry %d: the round over the stand-in capture failed: %v", entry, roundErr)
			}
		} else if roundErr == nil {
			// RULE 15: a device that produced no audio is REPORTED, never handed
			// up as an empty transcript — the same verdict the one-shot capture
			// gives ("the device is reachable but silent").
			t.Fatalf("entry %d: a recorder that produced no audio returned no error — a silent device must be reported", entry)
		}
		pids := voiceReentryAwaitPids(pidFile, entry)
		if len(pids) != entry {
			t.Fatalf("entry %d: the stand-in announced %d recorder pid(s), want %d — the assertion would be about the wrong child",
				entry, len(pids), entry)
		}
		pid := pids[entry-1]

		// THE ENTRY IS LEFT, exactly as `/voice stop` or Esc leaves it.
		rec.Close()

		if !voiceReentryAwaitReap(pid) {
			t.Fatalf("entry %d: recorder pid %d is still in the process table after the entry was left (%s) — "+
				"this is the `/voice` re-entry leak: every entry after the first spawns a recorder "+
				"whose teardown guard was already spent, so it exits and nobody Wait()s it (the "+
				"fourteen <defunct> parecord children measured under a live `gopi chat`, pid 331978)",
				entry, pid, voiceReentryChildState(pid))
		}
	}
}
