package uiclient

// sessiondir_test.go — the SESSION DIRECTORY of a dump, and the selection rule
// that lets a caller pick a surface by naming its session.
//
// WHY THESE EXIST. The compare derives its authoritative transcript from "the
// session this dump belongs to", and the rule it shipped with was a list of two
// hard-coded file names (a bare `session.jsonl` beside the dump, or one level
// up). The sessions tree has more layouts than that — the TASK RUN layout, which
// the task machinery itself uses, names its record `<run uuid>.jsonl` and puts
// its artifacts one directory down — so the derivation found nothing for the very
// surface the caller was looking at, and the one-call check refused.
//
// The unit under test is deliberately the RESOLVER (which directory owns this
// dump?) rather than the compare's own plumbing: a test that asserted the
// compare's derived path would agree with whatever rule the compare happened to
// use, while these cases pin the answer against a hand-built sessions tree whose
// shape is the operator's own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// sessionDirFixture builds a sessions root under a pinned profile and returns it.
// The pin is what makes the walk's BOUNDARY (the sessions root) resolvable at
// all: the resolver stops there, so a test that did not pin GOPI_HOME could not
// exercise the boundary.
func sessionDirFixture(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	t.Setenv("GOPI_HOME", profile)
	root := filepath.Join(profile, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir the fixture sessions root: %v", err)
	}
	return root
}

// TestSessionDirOfDumpWalksEveryLayout pins the four places a dump legally sits,
// including the two the old rule could not see. Each case is the layout a REAL
// producer writes (the file names come from sessionlayout — the package that owns
// the classification — so a case cannot drift from the writer).
func TestSessionDirOfDumpWalksEveryLayout(t *testing.T) {
	root := sessionDirFixture(t)
	const id = "0f0f0f0f-bbbb-4ccc-8ddd-eeeeeeeeeeee"

	cases := []struct {
		name string
		// layout builds the tree and returns the dump path.
		layout func(t *testing.T, root string) string
		want   string
	}{
		{
			name: "bare session: the record beside the dump",
			layout: func(t *testing.T, root string) string {
				dir := filepath.Join(root, id)
				writeSessionRecord(t, filepath.Join(dir, "session.jsonl"))
				return writeSessionDump(t, dir)
			},
			want: filepath.Join(root, id),
		},
		{
			name: "bare session: one artifact subdir down (a capture/recipe dir)",
			layout: func(t *testing.T, root string) string {
				dir := filepath.Join(root, id)
				writeSessionRecord(t, filepath.Join(dir, "session.jsonl"))
				return writeSessionDump(t, filepath.Join(dir, "run1"))
			},
			want: filepath.Join(root, id),
		},
		{
			name: "task run: the run-uuid record one level up (the layout the task machinery writes)",
			layout: func(t *testing.T, root string) string {
				dir := filepath.Join(root, "01a0dd26-a578-7e81-a81e-f751dcf993eb")
				writeSessionRecord(t, filepath.Join(dir, "82e9613e-c26b-4382-a7b6-ebbed154735a.jsonl"))
				return writeSessionDump(t, filepath.Join(dir, "evidence", "frames"))
			},
			want: filepath.Join(root, "01a0dd26-a578-7e81-a81e-f751dcf993eb"),
		},
		{
			name: "legacy flat: the root IS the session dir",
			layout: func(t *testing.T, root string) string {
				writeSessionRecord(t, filepath.Join(root, id+".jsonl"))
				return writeSessionDump(t, root)
			},
			want: root,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dump := tc.layout(t, root)
			got, ok := SessionDirOfDump(filepath.Join(dump, "chat-screen.dump"))
			if !ok {
				t.Fatalf("SessionDirOfDump(%s) found no session dir", dump)
			}
			if got != tc.want {
				t.Fatalf("SessionDirOfDump = %q, want %q", got, tc.want)
			}
		})
	}

	// A dump with NO record anywhere above it resolves to nothing — the resolver
	// never invents one (Rule 15). Its own fixture root: the cases above left a
	// legacy flat record AT the shared root, which is a legitimate owner for
	// anything under it (that is what the flat layout means).
	orphanRoot := sessionDirFixture(t)
	orphan := writeSessionDump(t, filepath.Join(orphanRoot, "no-record-here", "frames"))
	if dir, ok := SessionDirOfDump(filepath.Join(orphan, "chat-screen.dump")); ok {
		t.Fatalf("an orphan dump resolved to %q, want no session", dir)
	}

	// …and the walk stops at the sessions ROOT: a record sitting ABOVE the tree
	// (a stray in the profile dir itself) is not the answer to "which session owns
	// this file". The fixture's own sessions root is the boundary — a record one
	// level above it would otherwise be adopted.
	boundaryRoot := sessionDirFixture(t)
	writeSessionRecord(t, filepath.Join(filepath.Dir(boundaryRoot), "session.jsonl"))
	child := writeSessionDump(t, filepath.Join(boundaryRoot, "orphan-child"))
	if dir, ok := SessionDirOfDump(filepath.Join(child, "chat-screen.dump")); ok {
		t.Fatalf("the walk left the sessions root and answered %q", dir)
	}
}

// TestSelectBySessionPicksTheDumpThatBelongsToIt is the disambiguator: among
// several candidates, the one whose dump sits in the named session's directory is
// the target — the fact a caller who names a session has already stated.
func TestSelectBySessionPicksTheDumpThatBelongsToIt(t *testing.T) {
	root := sessionDirFixture(t)
	want := filepath.Join(root, "bbbbbbbb-1111-4222-8333-444444444444")
	writeSessionRecord(t, filepath.Join(want, "session.jsonl"))
	writeSessionDump(t, want)

	cands := collectCandidates(t, root,
		filepath.Join(root, "aaaaaaaa-1111-4222-8333-444444444444"),
		want,
		filepath.Join(root, "cccccccc-1111-4222-8333-444444444444"))

	sel, err := SelectBySession(cands, filepath.Join(want, "session.jsonl"))
	if err != nil {
		t.Fatalf("SelectBySession refused: %v", err)
	}
	if got := filepath.Dir(sel.Candidate.Path); got != want {
		t.Fatalf("selected %q, want the dump in %q", got, want)
	}

	// A name that identifies NO candidate with SEVERAL on hand is a refusal that
	// says so, listing the sessions the dumps do belong to — never a silent
	// substitution of "the only dump I could find".
	stranger := filepath.Join(root, "dddddddd-1111-4222-8333-444444444444")
	writeSessionRecord(t, filepath.Join(stranger, "session.jsonl"))
	_, err = SelectBySession(cands, filepath.Join(stranger, "session.jsonl"))
	if err == nil {
		t.Fatalf("SelectBySession answered for a session that owns no dump")
	}
	if !containsAll(err.Error(), "belongs to session", "the dumps on hand belong to") {
		t.Fatalf("the refusal does not name the sessions the dumps belong to: %v", err)
	}

	// With ONE candidate on hand the name identifies no dump, and that is the
	// OVERRIDE case (--session is also the authority override): it must not veto a
	// check, or naming a different transcript would make the verb unable to run.
	sel, err = SelectBySession(cands[:1], filepath.Join(stranger, "session.jsonl"))
	if err != nil {
		t.Fatalf("SelectBySession refused the single-candidate override case: %v", err)
	}
	if sel.Candidate.Path != cands[0].Path {
		t.Fatalf("selected %q, want the only candidate %q", sel.Candidate.Path, cands[0].Path)
	}
}

// writeSessionRecord writes a minimal session record at path (its content is
// irrelevant to the classification: a record is recognized by LAYOUT and NAME,
// which is the rule this package shares with internal/sessionlayout).
func writeSessionRecord(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"session_start","sessionId":"fixture"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write the session record %s: %v", path, err)
	}
	return path
}

// writeSessionDump writes a real dump (the product's sink) into dir and returns
// dir. It uses the product's writer for the same reason the reader tests do: a
// hand-typed record line would keep passing after the format changed.
func writeSessionDump(t *testing.T, dir string) string {
	t.Helper()
	writeFixtureDump(t, filepath.Join(dir, ChatDumpFileName), unlivablePID,
		time.Now().Add(-time.Hour).UTC().Truncate(time.Second), "self:chat", "a painted row")
	return dir
}

// collectCandidates scans root after ensuring each named session dir holds a dump,
// and returns the candidates a real scan produces (never hand-built structures —
// the point is that the resolver and the SCANNER agree about what a candidate is).
func collectCandidates(t *testing.T, root string, sessionDirs ...string) []Candidate {
	t.Helper()
	for _, dir := range sessionDirs {
		writeSessionDump(t, dir)
	}
	cands, _, err := Candidates([]string{root}, time.Now())
	if err != nil {
		t.Fatalf("Candidates(%s): %v", root, err)
	}
	return cands
}

// containsAll reports whether s carries every want (a tiny helper so a failure
// message names the missing piece rather than printing two strings).
func containsAll(s string, want ...string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}
