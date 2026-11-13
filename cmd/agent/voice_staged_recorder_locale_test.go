package main

// voice_staged_recorder_locale_test.go — the regression guard for the STAGED
// recorder's diagnostic: the second recorder child (cli_voice_audio_source.go's
// voiceStagedRecorderSource), which spawns the SAME argv as the one-shot capture
// path but was left with no locale pin, while its stderr is now READ (the source's
// Diagnostic) and quoted in the refusals an operator sees.
//
// THE BUG, in one sentence: a child's diagnostic is text a HUMAN reads and text a
// CLASSIFIER matches, and both are destroyed by gettext. Measured on this host
// 2026-10-12 against the REAL recorder, the same absent device:
//
//	$ arecord -D plughw:99,0 -f S16_LE -r 16000 -c 1 -q 2>&1 >/dev/null      # LANG=C
//	arecord: main:831: audio open error: Invalid argument
//	$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:99,0 … -q 2>&1 >/dev/null
//	arecord: main:831: 音乐打开错误： 无效的参数
//
// The staged source spawned with cmd.Env == nil (the zero value), so on the
// operator's zh_CN host the ONE sentence explaining why their microphone produced
// nothing arrived in a language the rest of the run's output is not written in —
// and the same two strings are what voiceRecorderHint matches for its busy/missing
// advice.
//
// THE FIX IS TWO HALVES OF ONE DECISION (devdoc/voice-staged-recorder-stderr.md):
// the diagnostic is READ (health → refusals) and therefore the child is PINNED,
// through internal/tools.ChildLocaleEnv — the ONE spelling of strip-then-append
// (Rule 14), the same pin the one-shot path took in d808a8745.
//
// THE STAND-IN IS A TRANSLATING CHILD, like the sibling guard's `sort` probe: the
// stub prints the C-locale string or the zh_CN one DEPENDING ON THE ENVIRONMENT IT
// WAS HANDED, which is what makes the arm behavioural rather than a re-assertion
// of the production source's own code. The RED CONTROL asserts that the ambient
// locale really does translate here — if this host stopped translating, the test
// says so instead of passing for the wrong reason.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

const (
	// voiceStagedStubBinary is the name the production spawn resolves: the source
	// execs voiceArecordBinary by NAME, so a stand-in on PATH is reached with no
	// production seam (and no test-only branch in the code under test).
	voiceStagedStubBinary = "arecord"
	// voiceStagedStubEnglish is the RECORDER's own C-locale line, verbatim from the
	// measurement in this file's header.
	voiceStagedStubEnglish = "audio open error: Invalid argument"
	// voiceStagedStubTranslated is the same line under zh_CN, also verbatim.
	voiceStagedStubTranslated = "音乐打开错误： 无效的参数"
	// voiceStagedStubDevice is the device the stand-in is told to open. It is never
	// really opened — the stub only names it back.
	voiceStagedStubDevice = "stub"
	// voiceStagedDrainChunks bounds the loop that drains the source's hand-off: the
	// stub produces nothing, so the channel closes immediately, and a loop that did
	// not terminate would hang the suite instead of failing it.
	voiceStagedDrainChunks = 64
)

// voiceStagedStubScript is the stand-in recorder: a child that REFUSES, in the
// locale it was handed.
func voiceStagedStubScript() string {
	return fmt.Sprintf("#!/bin/sh\ncase \"$LANG$LC_ALL\" in\n  *zh*) printf '%%s\\n' '%s' >&2 ;;\n  *) printf '%%s\\n' '%s' >&2 ;;\nesac\nexit 1\n",
		voiceStagedStubTranslated, voiceStagedStubEnglish)
}

// voiceStagedStubInstall puts the stand-in on PATH in front of everything else.
func voiceStagedStubInstall(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, voiceStagedStubBinary)
	if err := os.WriteFile(path, []byte(voiceStagedStubScript()), 0o755); err != nil {
		t.Fatalf("cannot install the recorder stand-in: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// voiceStagedStubRun runs the stand-in with exactly the given child environment
// (nil = inherit this process's, which is the unpinned shape under test) and
// returns what it wrote to stderr.
func voiceStagedStubRun(t *testing.T, env []string) string {
	t.Helper()
	cmd := exec.Command(voiceStagedStubBinary, "-D", voiceStagedStubDevice)
	cmd.Env = env
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	_ = cmd.Run() //stylegate:ignore the stub's exit status is deliberately unconstrained here: the held-open pipe IS the subject and the test kills the process itself
	return errBuf.String()
}

// voiceStagedStubSource opens the production source against the stand-in and
// returns it once its reader has stopped (the recorder exited without audio).
func voiceStagedStubSource(t *testing.T) *voiceStagedRecorderSource {
	t.Helper()
	src := &voiceStagedRecorderSource{dev: voiceDevice{ID: voiceStagedStubDevice, Name: voiceStagedStubDevice}}
	if err := src.Open(); err != nil {
		t.Fatalf("the staged source could not spawn the stand-in recorder: %v", err)
	}
	for drained := 0; ; drained++ {
		if drained > voiceStagedDrainChunks {
			t.Fatalf("the source's hand-off did not close after %d chunks — the stand-in produces no audio",
				voiceStagedDrainChunks)
		}
		if _, ok := <-src.Chunks(); !ok {
			break
		}
	}
	return src
}

// TestVoiceStagedRecorderEnvPinsTheLocale is the deterministic guard on the SHAPE
// of the staged spawn: the child carries the locale policy, the gettext selectors
// are STRIPPED rather than inherited, and each pinned name is written exactly once.
//
// It asserts the mechanism, not the outcome: `append(os.Environ(), "LC_ALL=C")`
// would satisfy a naive "is C in there" check while the inherited translation could
// still win — a duplicated entry is decided by the READER, not by one rule, and
// neither "first" nor "last" is that rule (glibc's catalogue lookup and Go's
// os.Getenv take the FIRST copy; dash and bash take the LAST; a wrapper collapses
// the pair before the reader sees it — MEASURED 2026-10-16, the reading and its
// host are in tools.ChildLocaleDuplicateKeyRule and
// internal/tools/git_child_locale.go), so an appended pin beside an inherited
// selector is a pin only while the order AND the reader happen to agree. Exactly
// one entry is the property that does not depend on either.
func TestVoiceStagedRecorderEnvPinsTheLocale(t *testing.T) {
	t.Setenv("LANG", mergeLocaleProbeValue)
	t.Setenv("LC_ALL", mergeLocaleProbeValue)
	t.Setenv("LANGUAGE", mergeLocaleProbeValue)
	t.Setenv("LC_MESSAGES", mergeLocaleProbeValue)
	voiceStagedStubInstall(t)

	src := voiceStagedStubSource(t)
	defer src.Close()
	if src.cmd == nil {
		t.Fatal("the staged source spawned no process, so there is no environment to check")
	}

	counts := map[string]int{}
	values := map[string]string{}
	for _, kv := range src.cmd.Env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if tools.ChildLocaleEnvName(name) {
			counts[name]++
			values[name] = value
		}
	}
	for _, name := range tools.ChildLocaleLocalePinnedNames {
		if counts[name] != 1 {
			t.Errorf("the staged recorder child has %s %d time(s), want exactly 1 — an env carrying TWO values "+
				"for the same selector leaves the child's language to a duplicate-entry resolution that is "+
				"READER-dependent, not order-fixed (tools.ChildLocaleDuplicateKeyRule: glibc and Go take the FIRST, "+
				"the shells take the LAST, measured 2026-10-16), so this is not a pin", name, counts[name])
		}
		if values[name] != tools.ChildLocaleEnglish {
			t.Errorf("the staged recorder child has %s=%q, want %q", name, values[name], tools.ChildLocaleEnglish)
		}
	}
	for _, name := range tools.ChildLocaleSelectorNames {
		if counts[name] != 0 {
			t.Errorf("the staged recorder child inherits %s=%q — the gettext selectors are STRIPPED, never inherited",
				name, values[name])
		}
	}
}

// TestVoiceStagedRecorderDiagnosticSpeaksThePinnedLocale is the BEHAVIOURAL guard,
// and it is the one that goes red if the pin is removed: the recorder is spawned by
// the production source, really refuses, and the sentence the source surfaces —
// the one an operator reads — must be the line its child wrote under the pinned
// locale.
func TestVoiceStagedRecorderDiagnosticSpeaksThePinnedLocale(t *testing.T) {
	// A hostile ambient environment: the operator's own zh_CN host.
	t.Setenv("LANG", mergeLocaleProbeValue)
	t.Setenv("LC_ALL", mergeLocaleProbeValue)
	voiceStagedStubInstall(t)

	// RED CONTROL — the unpinned child (cmd.Env == nil, what the staged spawn used
	// to be) really does speak the other language here. If it did not, this test
	// would be measuring nothing.
	control := voiceStagedStubRun(t, nil)
	if !strings.Contains(control, voiceStagedStubTranslated) {
		t.Fatalf("the unpinned child did not print %q under %s (it printed %q), so this arm proves nothing "+
			"about the translation — the stand-in reads $LANG/$LC_ALL and this host's locale is not reaching it",
			voiceStagedStubTranslated, mergeLocaleProbeValue, control)
	}

	src := voiceStagedStubSource(t)
	defer src.Close()
	note := src.Diagnostic()
	if note == "" {
		t.Fatalf("the staged source surfaced no diagnostic at all for a recorder that exited 1 with stderr — "+
			"the child's own account of the failure is being captured and then dropped (Rule 15). "+
			"child stderr was %q", src.stderr.String())
	}
	if !strings.Contains(note, voiceStagedStubEnglish) {
		t.Errorf("the staged recorder's diagnostic does not carry the pinned child's line %q — the child was "+
			"spawned with an inherited locale and gettext rewrote what an operator reads:\n%s",
			voiceStagedStubEnglish, note)
	}
	if strings.Contains(note, voiceStagedStubTranslated) {
		t.Errorf("the staged recorder's diagnostic is the TRANSLATED line %q, i.e. the child inherited the "+
			"ambient locale:\n%s", voiceStagedStubTranslated, note)
	}

	// THE WIRING: the note reaches the sentence a refusal builds, so the read is
	// not an island — the refusal an operator meets for a silent capture carries it.
	// The builder is constructed DIRECTLY (no Open): the source is already open, and
	// this arm is about the sentence the refusal is rendered from.
	refusal := (&voiceStagedStream{source: src}).sourceSilenceNote("X produced no audio")
	if !strings.Contains(refusal, voiceStagedStubEnglish) {
		t.Errorf("the silence refusal does not carry the recorder's own diagnostic, so the operator is told "+
			"only that nothing arrived when the child said why:\n%s", refusal)
	}
}
