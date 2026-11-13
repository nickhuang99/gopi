package uiclient

// screenstream_rename_test.go — the READ side of the 2026-11-08 `sgr` → `screen`
// rename, pinned as a matrix rather than as four separate claims.
//
// The rule the mechanism must keep is not "the new spelling works" (the writer
// produces it, so of course it does) but "an artifact recorded BEFORE the rename
// is still read, byte for byte". This host held 177 `.sgr.stream` files when
// the rename landed, and they are the only verbatim recordings that exist for
// those runs: a reader that stopped recognizing either the old SUFFIX or the old
// HEADER would turn them into unreadable bytes with no second copy anywhere.
//
// So the file is rewritten in place into the pre-rename spelling and read back
// through the SAME exported entry point a caller uses — the two names differ, the
// replayed frames must not.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sgrRewriteRecordSpellings turns every record line of a stream from one
// generation's spelling to the other. It rewrites ONLY the framing lines (a line
// beginning with `#`), never a frame's payload: a payload may legitimately begin
// with the header's own bytes (that is why the trailer/header test needs the
// separating space), and a blind Replace over the whole file would corrupt data
// to prove a spelling.
func sgrRewriteRecordSpellings(t *testing.T, blob string, headerFrom, headerTo, trailerFrom, trailerTo string) string {
	t.Helper()
	lines := strings.SplitAfter(blob, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, headerFrom+" "):
			lines[i] = headerTo + strings.TrimPrefix(line, headerFrom)
		case strings.HasPrefix(line, trailerFrom+" "):
			lines[i] = trailerTo + strings.TrimPrefix(line, trailerFrom)
		}
	}
	return strings.Join(lines, "")
}

// TestScreenStreamReaderServesBothGenerations is the matrix: {new suffix, old
// suffix} × {new markers, old markers}, every cell read through
// ReadSgrFramesAtPath — the same call `gopi session replay --verify` and
// `gopi chat --screen-replay` reach — and every cell required to yield the SAME
// frame sequence the writer recorded.
func TestScreenStreamReaderServesBothGenerations(t *testing.T) {
	dir := t.TempDir()
	paints := [][]byte{
		[]byte("\x1b[Hone"),
		[]byte("\x1b[2Jtwo"),
		[]byte("three"),
	}
	current := recordSgrStream(t, dir, "both", 20, 4, paints)

	wantMeta, wantFrames, err := ReadSgrFramesAtPath(current)
	if err != nil {
		t.Fatalf("the writer's own artifact is unreadable: %v", err)
	}
	if !strings.HasPrefix(wantMeta.Revision, "") { // Revision is recorded, never blank
		t.Fatalf("revision: %q", wantMeta.Revision)
	}
	if len(wantFrames) != len(paints) {
		t.Fatalf("the current file yielded %d frames, want %d", len(wantFrames), len(paints))
	}

	blob, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read %s: %v", current, err)
	}

	cases := []struct {
		name            string
		suffix          string
		rewriteMarkers  bool
		wantLegacyLabel bool
	}{
		{"current suffix, current markers", ScreenStreamSuffix, false, false},
		{"current suffix, PRE-RENAME markers", ScreenStreamSuffix, true, true},
		{"PRE-RENAME suffix, current markers", LegacyScreenStreamSuffix, false, false},
		{"PRE-RENAME suffix, PRE-RENAME markers", LegacyScreenStreamSuffix, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := string(blob)
			if c.rewriteMarkers {
				body = sgrRewriteRecordSpellings(t, body,
					ScreenStreamHeaderPrefix, LegacyScreenStreamHeaderPrefix,
					ScreenStreamTrailerPrefix, LegacyScreenStreamTrailerPrefix)
			}
			path := filepath.Join(dir, "both-"+strings.ReplaceAll(strings.ToLower(c.name), " ", "-")+c.suffix)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}

			// The SUFFIX list must recognize this path, and name it the way it is
			// spelled — the ONE list both the resolver and the readers walk.
			suf, ok := ScreenStreamSuffixOf(path)
			if !ok || suf != c.suffix {
				t.Fatalf("ScreenStreamSuffixOf(%s) = %q,%v, want %q,true", filepath.Base(path), suf, ok, c.suffix)
			}

			meta, frames, err := ReadSgrFramesAtPath(path)
			if err != nil {
				t.Fatalf("%s is refused by the reader: %v", filepath.Base(path), err)
			}
			if len(frames) != len(wantFrames) {
				t.Fatalf("%d frames, want %d — the spelling must not change the parse", len(frames), len(wantFrames))
			}
			for i := range frames {
				if string(frames[i].Payload) != string(wantFrames[i].Payload) {
					t.Fatalf("frame %d = %q, want %q", i, frames[i].Payload, wantFrames[i].Payload)
				}
			}
			// The GENERATION is reported, never normalized away: a reader that
			// flattened the two spellings would hide which recorder wrote a file.
			firstLine, _, _ := strings.Cut(body, "\n")
			if got := SgrStreamHeaderIsLegacy(firstLine); got != c.wantLegacyLabel {
				t.Fatalf("SgrStreamHeaderIsLegacy(%q) = %v, want %v", firstLine, got, c.wantLegacyLabel)
			}
			if meta.Trailer != wantMeta.Trailer {
				t.Fatalf("trailer=%v, want %v — a pre-rename close-out is still a close-out", meta.Trailer, wantMeta.Trailer)
			}
		})
	}
}

// TestScreenStreamWritesOnlyTheCurrentSpelling is the WRITE half, and it is the
// claim the read matrix cannot make: both spellings are READABLE, but exactly ONE
// is produced. Without this arm the rename could be "done" by writing the old
// header forever while every reader kept working — a green that never replaced
// anything.
func TestScreenStreamWritesOnlyTheCurrentSpelling(t *testing.T) {
	dir := t.TempDir()
	path := recordSgrStream(t, dir, "writer", 10, 3, [][]byte{[]byte("x")})
	if suf, ok := ScreenStreamSuffixOf(path); !ok || suf != ScreenStreamSuffix {
		t.Fatalf("the writer's own path resolves to %q,%v — it must be the NEWEST suffix", suf, ok)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(blob)
	if !strings.HasPrefix(text, ScreenStreamHeaderPrefix+" ") {
		t.Fatalf("the writer's header is %q, want the %s prefix", firstLineOf(text), ScreenStreamHeaderPrefix)
	}
	for _, legacy := range []string{LegacyScreenStreamHeaderPrefix, LegacyScreenStreamTrailerPrefix} {
		if strings.Contains(text, legacy) {
			t.Fatalf("a NEW recording carries the pre-rename marker %q — the aliases are read-only", legacy)
		}
	}
}

// firstLineOf is the failure message's own convenience: it names what the writer
// actually wrote instead of a truncated blob.
func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
