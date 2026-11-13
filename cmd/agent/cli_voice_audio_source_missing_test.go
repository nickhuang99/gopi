package main

// cli_voice_audio_source_missing_test.go — WHICH PATH COMPONENT IS MISSING (task
// 01a1222d).
//
// WHAT IT PINS, in one sentence: when the file named by `--audio-source` /
// `--voice-audio` cannot be opened, the refusal names the DIRECTORY that is not
// there (or says it is the file itself), and when a near-miss directory of the
// SAME PARENT exists it is named too.
//
// WHY IT IS THE CASE THAT MATTERS. The measured failure (2026-10-10, raw pty
// runs in the task's session evidence dir) was a caller spelling the session id
// with the dashes REMOVED —
//
//	--voice-audio /home/nick/.gopi/sessions/01a121fceebc73918785db2d149f931e/artifacts/probe.wav
//
// — where the directory that exists is `01a121fc-eebc-7391-8785-db2d149f931e`.
// The de-dashed directory exists nowhere, so the ENOENT was CORRECT and the run
// was not a gopi defect at all: what was wrong was the sentence. It quoted the
// whole path and `open …: no such file or directory`, which is true of every
// component at once, so it sent two readers after a fake mechanism (leading to
// the now-falsified "a wrapper script makes the open fail" note). A refusal that
// names the missing COMPONENT answers the question in one line.
//
// THE RED PROOF: with the pre-fix message the three cases below fail on the
// missing-directory half — the directory's path is absent from the sentence, so
// the only thing an operator can read is the file's.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

// audioSourceMissingScratch makes an empty scratch directory for these cases.
//
// It is NOT t.TempDir(): every device-backed artefact of a test in this
// repository is written through testutil.TestDeviceScratchDir (Rule 22/32) — the
// one answer to "where may a test write". Only the DIRECTORY is made; whether a
// name inside it exists is exactly what the cases vary.
func audioSourceMissingScratch(t *testing.T, sub string) string {
	t.Helper()
	return testutil.TestDeviceScratchDir(t, sub)
}

// TestVoiceAudioSourceMissingDirectoryIsNamed is the case the finding is about:
// the file's own directory does not exist, so the refusal must say that the
// DIRECTORY is what is missing — not leave the operator to guess between the
// path's components.
func TestVoiceAudioSourceMissingDirectoryIsNamed(t *testing.T) {
	dir := audioSourceMissingScratch(t, "missing-dir")
	missingDir := filepath.Join(dir, "01a121fceebc73918785db2d149f931e", "artifacts")
	path := filepath.Join(missingDir, "probe.wav")

	err := voiceValidateAudioSource(path)
	if err == nil {
		t.Fatalf("a path under a directory that does not exist was accepted: %s", path)
	}
	msg := err.Error()
	if !strings.Contains(msg, missingDir) {
		t.Fatalf("the refusal does not name the missing DIRECTORY %q — it is "+
			"unreadable as which component is wrong:\n%s", missingDir, msg)
	}
	if !strings.Contains(msg, path) {
		t.Fatalf("the refusal does not quote the path that was given:\n%s", msg)
	}
	if !strings.Contains(msg, voiceAudioSourceDirMissingNote) {
		t.Fatalf("the refusal does not say which component is missing (Rule 15: a mistake is "+
			"answered with the mistake):\n%s", msg)
	}
	if !strings.Contains(msg, dir) {
		t.Fatalf("the refusal does not name the near-miss PARENT %q that does exist:\n%s", dir, msg)
	}
}

// TestVoiceAudioSourceMissingFileNamesTheFile separates the OTHER component: the
// directory is there and only the file is missing, and then the sentence must not
// blame the directory.
func TestVoiceAudioSourceMissingFileNamesTheFile(t *testing.T) {
	dir := audioSourceMissingScratch(t, "missing-file")
	path := filepath.Join(dir, "not-written-yet.wav")

	err := voiceValidateAudioSource(path)
	if err == nil {
		t.Fatalf("a file that does not exist was accepted: %s", path)
	}
	msg := err.Error()
	if strings.Contains(msg, voiceAudioSourceDirMissingNote) {
		t.Fatalf("the refusal blames a directory for a file that is missing from a directory "+
			"that exists:\n%s", msg)
	}
	if !strings.Contains(msg, dir) {
		t.Fatalf("the refusal does not name the directory it looked in (%q):\n%s", dir, msg)
	}
}

// TestVoiceAudioSourceNearMissDirectoryIsNamed is the whole point of the fix,
// driven on the SHAPE that was measured: one character class removed from a path
// component (here the dashes of a session id), a sibling that still exists.
func TestVoiceAudioSourceNearMissDirectoryIsNamed(t *testing.T) {
	parent := audioSourceMissingScratch(t, "near-miss")
	realDir := filepath.Join(parent, "01a121fc-eebc-7391-8785-db2d149f931e")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatalf("make %s: %v", realDir, err)
	}
	deDashed := strings.ReplaceAll(filepath.Base(realDir), "-", "")
	path := filepath.Join(parent, deDashed, "artifacts", "probe.wav")

	start := time.Now()
	err := voiceValidateAudioSource(path)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("the de-dashed path was accepted: %s", path)
	}
	msg := err.Error()
	if !strings.Contains(msg, deDashed) {
		t.Fatalf("the refusal does not name the missing directory %q it was given:\n%s", deDashed, msg)
	}
	if strings.Contains(msg, "?.") {
		t.Fatalf("the sentence ends in \"?.\" — the hint's own question mark already closes it:\n%s", msg)
	}
	if !strings.Contains(msg, realDir) {
		t.Fatalf("the refusal does not name the near-miss directory %q that DOES exist — the one "+
			"line that would have ended the 2026-10-10 investigation:\n%s", realDir, msg)
	}
	// A near miss is a hint, not a second opinion: the check stays a stat of the
	// given path, so it cannot become a resolution layer (Rule 35).
	if elapsed > time.Second {
		t.Fatalf("the refusal took %s — it is looking at more than the path it was given", elapsed)
	}
}

// TestVoiceAudioSourceExistingPathIsAccepted guards the other direction: the fix
// must not refuse a file that is there.
func TestVoiceAudioSourceExistingPathIsAccepted(t *testing.T) {
	path := seamScratchFile(t, "present.wav", voicePCM16ToWAV(seamFixture()))
	if err := voiceValidateAudioSource(path); err != nil {
		t.Fatalf("a file that exists was refused: %v", err)
	}
}
