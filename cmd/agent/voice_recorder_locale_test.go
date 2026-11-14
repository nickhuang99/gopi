package main

// voice_recorder_locale_test.go — the regression guard for the LOCALE-TRANSLATED
// RECORDER diagnostic. Same defect family as internal/tools/git_child_locale_test.go
// (epic 01a1306b) and internal/testutil's control arm (ea2668b0f); measured here
// 2026-10-12.
//
// THE BUG. `voiceCaptureStreaming` reads the recorder's STDERR and classifies it
// by English literals — and two of those literals are glibc strerror messages:
//
//	voiceCaptureBrokenPipeMarker = "Broken pipe"               (EPIPE)
//	voiceErrBusyMarker           = "Device or resource busy"   (EBUSY)
//
// gettext translates both. The child was spawned with no cmd.Env, so it inherited
// this process's message directories and, under a translating locale, printed
// `stdout: 断开的管道` instead of `stdout: Broken pipe`. The consequence is a
// Rule 15 silent failure: `voiceCaptureContextSensitiveError` stops recognising
// the read side WE closed ourselves, so an ordinary window close is reclassified
// as "the recorder refused" — the wrong CAUSE reported for the right observation
// — and the busy hint disappears from the refusal path.
//
// MEASURED ON THIS HOST against the REAL recorder (not a stand-in), 2026-10-12:
//
//	$ arecord -D pulse -f S16_LE -r 16000 -c 1 | head -c 3200 >/dev/null
//	stdout: Broken pipe
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D pulse … | head -c 3200 >/dev/null
//	stdout: 断开的管道
//
//	$ arecord -D plughw:1,0 -f S16_LE -r 16000 -c 1 -d 1 /dev/null
//	arecord: main:831: audio open error: Device or resource busy
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:1,0 … -d 1 /dev/null
//	arecord: main:831: 音乐打开错误： 设备或资源忙
//
// The fix is `voiceRecorderEnv()`, which pins the locale through
// internal/tools.ChildLocaleEnv (Rule 14: ONE spelling of strip-then-append).
// These tests hold that pin in place and prove it by DRIVING a translating
// locale — never by dropping the assertion.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// voiceLocaleProbeLines is how much text the probe child sorts. It must
	// exceed a pipe buffer (64 KiB) so that, with the read end already closed,
	// the write really reaches EPIPE rather than fitting in the buffer.
	voiceLocaleProbeLines = 20000
	// voiceLocaleProbeLineFmt is the shape of each line.
	voiceLocaleProbeLineFmt = "%08d alpha beta gamma delta\n"
	// voiceLocaleProbeScript is the stand-in recorder. `sort` reports EPIPE the
	// way the MEASURED recorder does — a diagnostic plus exit status 2, NOT a
	// signal — because `trap '' PIPE` keeps the child alive to report the error
	// instead of dying of SIGPIPE. Without it the child is killed (measured:
	// rc=-13), the classifier answers TRUE through its `signal:` branch, and the
	// marker under test would never be the thing deciding the verdict.
	voiceLocaleProbeScript = `trap '' PIPE; exec sort "$1"`
	// voiceLocaleProbeArgZero is the shell's own $0 for that script.
	voiceLocaleProbeArgZero = "sh"
)

// voiceLocaleCanTranslate reports whether this host can actually express a
// translated message, so the behavioural test SKIPS LOUDLY (naming the command
// that would fix it) instead of passing for the wrong reason.
//
// It reuses mergeLocaleIsGeneratable and mergeLocaleProbeValue — the same probe
// and the same locale spelling cmd/agent's merge-lane guard uses (Rule 14: one
// spelling of "a locale whose diagnostics are not English"). The probe asks
// glibc through `locale charmap`, never `locale -a`: the listing spells the same
// locale `zh_CN.utf8` while a caller writes `zh_CN.UTF-8`.
func voiceLocaleCanTranslate(t *testing.T) bool {
	t.Helper()
	return mergeLocaleIsGeneratable(t, mergeLocaleProbeValue)
}

// voiceLocaleProbeFixture writes the text the probe child sorts.
func voiceLocaleProbeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.txt")
	var b bytes.Buffer
	b.Grow(voiceLocaleProbeLines * 32)
	for i := 0; i < voiceLocaleProbeLines; i++ {
		fmt.Fprintf(&b, voiceLocaleProbeLineFmt, i)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("cannot write the probe fixture: %v", err)
	}
	return path
}

// voiceLocaleRecorderRun runs the probe child with exactly the given child
// environment and its read side ALREADY CLOSED, i.e. the shape
// voiceCaptureStreaming produces when it stops reading a recorder. It returns
// what the child wrote to stderr and the error Wait() reported.
//
// The read end is closed before the child writes, so the child meets EPIPE; the
// child's stderr is what production parses.
func voiceLocaleRecorderRun(t *testing.T, env []string, fixture string) (string, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot create the probe pipe: %v", err)
	}
	cmd := exec.Command(voiceLocaleProbeArgZero, "-c", voiceLocaleProbeScript, voiceLocaleProbeArgZero, fixture)
	cmd.Env = env
	cmd.Stdout = write
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		read.Close()  //stylegate:ignore the error path of an already-failing t.Fatalf: the child never started, nothing left to leak-measure
		write.Close() //stylegate:ignore the error path of an already-failing t.Fatalf: the child never started, nothing left to leak-measure
		t.Fatalf("cannot start the probe child: %v", err)
	}
	// The parent drops ITS copy of the write end, and the read end goes away
	// entirely: from here on the child's writes have no reader.
	write.Close() //stylegate:ignore deliberate drop so the child meets EPIPE; a close error is not the property under test here
	if err := read.Close(); err != nil {
		t.Fatalf("cannot close the probe pipe's read end: %v", err)
	}
	waitErr := cmd.Wait()
	return errBuf.String(), waitErr
}

// TestVoiceRecorderEnvPinsTheLocale is the deterministic guard on the pin's
// SHAPE: the recorder child must carry the locale policy, with the gettext
// selectors STRIPPED rather than inherited and the pinned names written exactly
// once each.
//
// It asserts the mechanism, not merely the outcome: `append(os.Environ(),
// "LC_ALL=C")` would satisfy a naive "is C in there" check while the inherited
// translation still won for SOME reader — a duplicated entry is decided by the
// reader, not by one rule (glibc gettext and Go os.Getenv take the FIRST,
// dash and bash take the LAST — tools.ChildLocaleDuplicateKeyRule, measured 2026-10-16). Only
// "exactly one entry, carrying C" rules that out, for every reader at once.
func TestVoiceRecorderEnvPinsTheLocale(t *testing.T) {
	// A caller's hostile environment: the ambient shape that produced the bug
	// (LANG set, LC_ALL unset — see internal/tools/git_child_locale.go's header
	// for the measured shell/job asymmetry).
	t.Setenv("LANG", mergeLocaleProbeValue)
	t.Setenv("LC_ALL", mergeLocaleProbeValue)
	t.Setenv("LANGUAGE", mergeLocaleProbeValue)
	t.Setenv("LC_MESSAGES", mergeLocaleProbeValue)

	env := voiceRecorderEnv()
	counts := map[string]int{}
	values := map[string]string{}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if tools.ChildLocaleEnvName(name) {
			counts[name]++
			values[name] = value
		}
	}
	// WRITTEN: exactly one entry each, carrying the pinned locale.
	for _, name := range tools.ChildLocaleLocalePinnedNames {
		if counts[name] != 1 {
			t.Errorf("%s appears %d time(s) in the recorder env, want exactly 1 — a duplicated entry is decided "+
				"by the READER, not by one rule (tools.ChildLocaleDuplicateKeyRule), so the inherited "+
				"translation would still win for glibc and this is not a pin", name, counts[name])
		}
		if values[name] != tools.ChildLocaleEnglish {
			t.Errorf("%s=%q in the recorder env — the pin must REPLACE it with %q, not leave %q in force",
				name, values[name], tools.ChildLocaleEnglish, mergeLocaleProbeValue)
		}
	}
	// STRIPPED: no entry at all.
	for _, name := range tools.ChildLocaleSelectorNames {
		if counts[name] != 0 {
			t.Errorf("%s appears %d time(s) in the recorder env (%q), want 0 — the gettext selectors are "+
				"stripped, never inherited: leaving one is a second silent input to a classifier that quotes English",
				name, counts[name], values[name])
		}
	}
}

// TestVoiceRecorderReadSideMarkerSurvivesATranslatingLocale is the BEHAVIOURAL
// guard, and it is the one that goes red if the pin is removed.
//
// It runs a real child that really meets EPIPE with the environment production
// uses, under an ambient locale that really translates diagnostics, and asserts:
//
//	RED CONTROL  — with the AMBIENT environment the marker is GONE and the
//	               classifier misses the read side. This is the defect, and it is
//	               asserted rather than described: if the locale on this host
//	               stopped translating, the control would be a lie and this test
//	               must say so.
//	GREEN        — with voiceRecorderEnv() (what production spawns) the child
//	               still reports the English marker, and the shipped classifier
//	               recognises the read side from the MARKER (explicitly not from
//	               the signal branch).
func TestVoiceRecorderReadSideMarkerSurvivesATranslatingLocale(t *testing.T) {
	if !voiceLocaleCanTranslate(t) {
		t.Skipf("this host cannot express %s, so a translated diagnostic cannot be produced here — "+
			"the deterministic guard (TestVoiceRecorderEnvPinsTheLocale) still covers the pin. "+
			"To exercise it: locale-gen %s", mergeLocaleProbeValue, mergeLocaleProbeValue)
	}
	fixture := voiceLocaleProbeFixture(t)

	// The environment a hostile parent would hand a child: the translated
	// selector is SET, so the child's messages come from that catalogue.
	hostile := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LANG=") || strings.HasPrefix(kv, "LC_") || strings.HasPrefix(kv, "LANGUAGE=") {
			continue
		}
		hostile = append(hostile, kv)
	}
	hostile = append(hostile, "LANG="+mergeLocaleProbeValue, "LC_ALL="+mergeLocaleProbeValue)

	// --- RED CONTROL: the unpinned child, which is what cmd.Env == nil gave us ---
	controlErr, controlWait := voiceLocaleRecorderRun(t, hostile, fixture)
	if strings.Contains(controlErr, voiceCaptureBrokenPipeMarker) {
		t.Fatalf("the unpinned control still reported %q under %s, so this arm proves nothing about the "+
			"translation (the child's stderr was %q)", voiceCaptureBrokenPipeMarker, mergeLocaleProbeValue, controlErr)
	}
	if voiceCaptureContextSensitiveError(controlWait, controlErr) {
		t.Errorf("the classifier recognised the read side from a TRANSLATED diagnostic — either the "+
			"translation stopped happening or a non-marker branch matched; the marker is supposed to be "+
			"the only thing that can answer here\n--- child stderr ---\n%s", controlErr)
	}

	// --- GREEN: the environment production actually spawns ---
	pinnedErr, pinnedWait := voiceLocaleRecorderRun(t, voiceRecorderEnv(), fixture)
	if !strings.Contains(pinnedErr, voiceCaptureBrokenPipeMarker) {
		t.Fatalf("the PINNED recorder child no longer reports %q — the pin has been removed or weakened, and "+
			"on a translating host the read side would be misclassified as a recorder failure\n"+
			"--- child stderr ---\n%s", voiceCaptureBrokenPipeMarker, pinnedErr)
	}
	// The classification must come from the MARKER, not from the signal branch:
	// a signalled process would answer true for reasons that have nothing to do
	// with the locale, and this test would then be green for the wrong reason.
	if pinnedWait == nil {
		t.Fatalf("the probe child exited 0 while writing to a closed pipe, so no EPIPE was reported and "+
			"the marker it printed cannot be the measured one\n--- child stderr ---\n%s", pinnedErr)
	}
	if strings.Contains(pinnedWait.Error(), voiceCaptureKilledMarker) {
		t.Fatalf("the probe child was killed rather than reporting its write failure (%v), so the "+
			"classifier would answer through its signal branch and the marker would not be under test\n"+
			"--- child stderr ---\n%s", pinnedWait, pinnedErr)
	}
	if !voiceCaptureContextSensitiveError(pinnedWait, pinnedErr) {
		t.Fatalf("the classifier did NOT recognise the read side from the pinned child's own stderr — the "+
			"marker is in the text but is not being matched\n--- child stderr ---\n%s", pinnedErr)
	}
}
