package main

// chat_film_test.go — the film DRIVER's own units: the flag's two spellings and
// its refusals, and the reading of the beat protocol's files. The driver's end-to
// -end path is proved by running it (`gopi chat --replay <session> --film <dir>`,
// Rule 31); what is pinned here is everything that must not depend on a machine
// having a display, a terminal or a recording.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/replay"
)

// filmFixtureDir is the fixture's own directory spelling: the parent a bare
// `--film` must fall back to, and the named directory a spelled-out `--film`
// must win with. It is DECLARED, not inlined (PROJECT.md Rules 1b/13): a path
// literal used at several sites in one fixture gets one home with a name, and
// the worktree-root gate classifies a fixture literal as a named value by
// POSITION — the point of the classification is that the spelling has a single
// declared home, not that the value is unavoidable.
const filmFixtureDir = "/tmp/frames"

// TestChatParseFilmArg pins both spellings and every refusal: the value may be in
// the next slot or after `=`, and a film whose directory is missing must be
// refused with the spelling the caller can correct.
func TestChatParseFilmArg(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		next     string
		hasNext  bool
		wantDir  string
		wantHit  bool
		wantFail bool
	}{
		{name: "space form", arg: chatFlagFilm, next: filmFixtureDir, hasNext: true, wantDir: filmFixtureDir, wantHit: true},
		{name: "equals form", arg: chatFlagFilm + "=" + filmFixtureDir, wantDir: filmFixtureDir, wantHit: true},
		{name: "absent", arg: chatFlagReplay, next: "x", hasNext: true},
		// A BARE --film is ACCEPTED: it names no directory of its own, and where
		// the frames go is settled by resolveFilmDir once --out is known (the
		// operator's own command line is `--film --out FILE.gif`). What it must
		// NOT do is consume the next flag as its value.
		{name: "no value at all", arg: chatFlagFilm, hasNext: false, wantHit: true},
		{name: "no value before another flag", arg: chatFlagFilm, next: chatFlagOut, hasNext: true, wantHit: true},
		{name: "empty equals value", arg: chatFlagFilm + "=", wantHit: true, wantFail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, hit, err := chatParseFilmArg(c.arg, c.next, c.hasNext)
			if hit != c.wantHit {
				t.Fatalf("consumed = %v, want %v", hit, c.wantHit)
			}
			if c.wantFail {
				if err == nil {
					t.Fatal("a malformed --film was accepted")
				}
				if !strings.Contains(err.Error(), chatFlagFilm) {
					t.Fatalf("the refusal does not name the flag: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dir != c.wantDir {
				t.Fatalf("dir = %q, want %q", dir, c.wantDir)
			}
		})
	}
}

// TestResolveFilmDirSettlesTheBareSpelling: a bare `--film` takes the
// animation's own directory (the operator's single command line is complete), a
// named directory wins outright, and no directory anywhere is refused by name —
// never silently chosen.
func TestResolveFilmDirSettlesTheBareSpelling(t *testing.T) {
	// The replay that produced the frames sits in the fixture directory, so the
	// animation's "own directory" is that directory: a bare --film takes the
	// parent of the file --out names.
	gif := filepath.Join(filmFixtureDir, "replay.gif")
	if got, err := resolveFilmDir(filmFixtureDir, gif, chatFlagFilm); err != nil || got != filmFixtureDir {
		t.Errorf("a named directory was not used: (%q, %v)", got, err)
	}
	if got, err := resolveFilmDir("", gif, chatFlagFilm); err != nil || got != filmFixtureDir {
		t.Errorf("a bare --film did not take the animation's directory: (%q, %v)", got, err)
	}
	got, err := resolveFilmDir("", "", chatFlagFilm)
	if err == nil {
		t.Fatalf("a bare --film with no --out was accepted into %q — the frames would land somewhere nobody named", got)
	}
	for _, want := range []string{chatFlagFilm, chatFlagOut} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}

// TestReadFilmLedgerReadsOnlyCompleteRows pins the reader against the writer's own
// acceptance: a partially written row (the producer is appending as we read) must
// be SKIPPED rather than parsed into a bogus beat, the header is not a beat, and
// a ledger that does not exist yet is the state before the first beat — not an
// error, and not "an empty ledger".
func TestReadFilmBeatsReadsOnlyCompleteRows(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "not-yet")
	beats, err := readFilmBeats(missing)
	if err != nil {
		t.Fatalf("an absent ledger must be the state before the first beat, got: %v", err)
	}
	if len(beats) != 0 {
		t.Fatalf("an absent ledger reported %d beat(s)", len(beats))
	}

	full := strings.Join([]string{"1", "request", "0", "15", "preview", "dGV4dA=="}, "\t")
	partial := "2\tassistant\t38"
	path := filepath.Join(dir, replay.LedgerName)
	body := replay.LedgerHeader + full + "\n" + partial + "\n" + full + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("fixture ledger: %v", err)
	}
	beats, err = readFilmBeats(path)
	if err != nil {
		t.Fatalf("readFilmBeats: %v", err)
	}
	// Two complete rows, both beat 1: the partial row is not a beat, and a repeated
	// number is the DRIVER's business (it skips what it already shot).
	if len(beats) != 2 {
		t.Fatalf("beats = %v, want the two complete rows only", beats)
	}
	for _, b := range beats {
		if b != 1 {
			t.Fatalf("beats = %v — the partial row was read as a beat", beats)
		}
	}
	rows, err := countFilmRows(path)
	if err != nil {
		t.Fatalf("countFilmRows: %v", err)
	}
	if rows != 2 {
		t.Fatalf("rows = %d, want 2 (the header and the partial row are not rows)", rows)
	}
}
