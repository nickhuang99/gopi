package main

// cli_uistate_compare_ui_surface_test.go — `gopi ui_state --compare` against the
// REAL artifact of a `gopi ui` surface, and the two absences it must not confuse.
//
// THE MEASURED DEFECT (task 01a0e62d, 2026-09-28, live 110x40 pty). The dump of a
// real `gopi ui --dump-screen` run carried the answer ON ITS GRID —
//
//	003|gopi> UI-AUTH-OK-8842 qwerty
//
// — and the session record beside it (the same file `gopi session log --response --text`
// prints) carried the model's line `UI-AUTH-OK-8842 qwerty`. The verb reported
//
//	transcript_lines=1 outcome=could-not-run        (before this branch)
//	  refused  … the grid is ALL-BLANK …
//
// i.e. an observation of a screen that had painted the line perfectly, reported
// under a word that describes neither agreement nor loss. The fixture below is
// that exact run's artifact pair, captured live and archived verbatim, so this
// case fails if the reader drifts from the real thing.
//
// WHAT THE CASES PIN:
//  ① the real ui dump + a real transcript → agreed, exit 0 (the line IS on the grid
//     behind the ui surface's own `gopi> ` label, and the label is the surface's
//     grammar, not the text);
//  ② the word `no-authoritative-side` belongs to the artifact with NO derivable
//     record — never to the artifact whose transcript is merely empty. The two
//     used to print the same `transcript_lines=0`, which is the ambiguity this
//     word exists to remove.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The real artifact pair, archived as base64 tar.gz (the repo's own precedent for
// a captured fixture: internal/uiclient/testdata/*.sse.b64). Named here rather
// than inline so the case reads as "the artifact", not as a blob.
const (
	uiLiveDumpFixture = "testdata/ui-live-dump.tar.gz.b64"
	// uiLiveDumpTextName / uiLiveDumpTwinName are the pair's names INSIDE the
	// archive — the artifact's own contract names, spelled as the archive carries
	// them rather than read from the implementation.
	uiLiveDumpTextName = "ui-screen.dump"
	uiLiveDumpTwinName = "ui-screen.dump.json"
	// uiLiveDumpLine is the model's line the live run's session record holds and
	// its grid paints (behind the ui surface's label).
	uiLiveDumpLine = "UI-AUTH-OK-8842 qwerty"
	// uiLiveDumpRow is the same line as the LIVE GRID draws it.
	uiLiveDumpRow = "gopi> " + uiLiveDumpLine
)

// uiLiveDumpPair unpacks the captured pair into a session directory under the
// fixture profile and returns the dump's path.
//
// The pair is written TOGETHER and named for the session id constant the rest of
// this family uses, so the derivation (uiclient.SessionDirOfDump → the record
// beside it) is exercised exactly as it is on a real run.
func uiLiveDumpPair(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(uiLiveDumpFixture)
	if err != nil {
		t.Fatalf("read the captured artifact %s: %v", uiLiveDumpFixture, err)
	}
	blob, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(raw)), ""))
	if err != nil {
		t.Fatalf("the captured artifact is not base64: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("the captured artifact is not gzip: %v", err)
	}
	tr := tar.NewReader(zr)
	gopiDir, _ := uiStateFixtureProfile(t)
	dir := filepath.Join(gopiDir, "sessions", compareSessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir the fixture session dir: %v", err)
	}
	want := map[string]bool{uiLiveDumpTextName: false, uiLiveDumpTwinName: false}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the captured artifact is not a tar: %v", err)
		}
		if _, ok := want[hdr.Name]; !ok {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s from the captured artifact: %v", hdr.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, hdr.Name), body, 0o644); err != nil {
			t.Fatalf("write %s: %v", hdr.Name, err)
		}
		want[hdr.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("the captured artifact does not carry %s — the fixture is not the pair it claims", name)
		}
	}
	return filepath.Join(dir, uiLiveDumpTextName)
}

// TestUIStateCompare_RealUISurfaceDumpAgrees is ①: the REAL artifact pair of a
// live `gopi ui --dump-screen` run, compared through the verb.
//
// The line is the model's own text; the grid paints it behind the ui surface's
// row label. `agreed` is the only honest answer — the authority holds the line and
// the grid draws it — and the exit code is the contract's 0.
func TestUIStateCompare_RealUISurfaceDumpAgrees(t *testing.T) {
	dump := uiLiveDumpPair(t)
	// The transcript is the session record BESIDE the dump — the derived route, no
	// --session named, which is the one-call form the usage page promises.
	writeCompareSessionLog(t, filepath.Join(filepath.Dir(dump), compareSessionFileName), []string{uiLiveDumpLine})

	out, code := uiStateRun(t, []string{uiStateFlagCompare, uiLiveDumpLine})
	if code != 0 {
		t.Fatalf("exit = %d, want 0: the real ui dump paints this line and the record beside it holds it\noutput:\n%s", code, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "surface"); got != string(uiclient.SurfaceUI) {
		t.Fatalf("surface=%q, want %q — this case is the ui surface's artifact", got, uiclient.SurfaceUI)
	}
	if got := compareHeaderField(t, header, "outcome"); got != compareWordAgreed {
		t.Fatalf("outcome=%q, want %q\nheader:\n%s", got, compareWordAgreed, header)
	}
	if got := compareHeaderField(t, header, "transcript_lines"); got == "0" {
		t.Fatalf("transcript_lines=0: the authority beside the real dump was not read\nheader:\n%s", header)
	}
	if !strings.Contains(out, "in_grid=true") {
		t.Fatalf("the verdict does not report the line ON the grid:\n%s", out)
	}
}

// TestUIStateCompare_RealUISurfaceDumpRowSpellingIsNotTheTranscript is the honesty
// control on ①: the ROW's spelling (`gopi> …`) is NOT the model's text, so asking
// for it verbatim is a legitimate question with its own answer. It must not be
// reported as an agreement (that would mean the label is being treated as text),
// and it must not be a paint loss either — the grid DOES carry those bytes.
func TestUIStateCompare_RealUISurfaceDumpRowSpellingIsNotTheTranscript(t *testing.T) {
	dump := uiLiveDumpPair(t)
	writeCompareSessionLog(t, filepath.Join(filepath.Dir(dump), compareSessionFileName), []string{uiLiveDumpLine})

	out, code := uiStateRun(t, []string{uiStateFlagCompare, uiLiveDumpRow})
	if code == uiStateExitAgreed {
		t.Fatalf("exit = 0 for the ROW's spelling (%q): the ui surface's label is not the model's text, so this is not an agreement\noutput:\n%s",
			uiLiveDumpRow, out)
	}
	if code != uiStateExitCompareMismatch {
		t.Fatalf("exit = %d, want %d (the two sides differ: on the grid, not in the log)\noutput:\n%s",
			code, uiStateExitCompareMismatch, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "outcome"); got != compareWordDisagreed {
		t.Fatalf("outcome=%q, want %q\nheader:\n%s", got, compareWordDisagreed, header)
	}
}

// TestUIStateCompare_NoDerivableAuthorityNamesItsOwnOutcome is ② — the word this
// branch adds, driven through the verb on a REAL ui artifact whose session
// directory holds no record at all.
//
// The check has no other SIDE: nothing was named and nothing sits beside the dump.
// Before this branch that printed `outcome=could-not-run transcript_lines=0`
// beside a verdict that blamed the GRID (an all-blank refusal, or a mismatch),
// which reads as "the screen is broken" — the one thing the evidence does not
// support. Now it says `no-authoritative-side`, names the surface, and leaves the
// grid blameless.
func TestUIStateCompare_NoDerivableAuthorityNamesItsOwnOutcome(t *testing.T) {
	dump := uiLiveDumpPair(t)
	// No record is written at all: the artifact is real, the derivation finds
	// nothing (and the fixture asserts the absence, so a case that passed because
	// a stale file happened to sit there would be caught).
	record := filepath.Join(filepath.Dir(dump), compareSessionFileName)
	if _, err := os.Stat(record); err == nil {
		t.Fatalf("the fixture directory already holds %s — this case needs its ABSENCE", record)
	}

	out, code := uiStateRun(t, []string{uiStateFlagCompare, uiLiveDumpLine})
	if code != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d (nothing was compared — never an agreement)\noutput:\n%s",
			code, uiStateExitCouldNotRun, out)
	}
	if code == uiStateExitCompareMismatch {
		t.Fatalf("exit = %d (the DISAGREEMENT code): a missing authority must never be reported as a paint loss\noutput:\n%s", code, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "outcome"); got != uiStateAuditWordNoAuthoritative {
		t.Fatalf("outcome=%q, want %q — the two absences (no authority here / an authority read and empty) are different facts and the reader must be able to tell them apart\nheader:\n%s",
			got, uiStateAuditWordNoAuthoritative, header)
	}
	if got := compareHeaderField(t, header, "surface"); got != string(uiclient.SurfaceUI) {
		t.Fatalf("surface=%q, want %q\nheader:\n%s", got, uiclient.SurfaceUI, header)
	}
	if !strings.Contains(out, string(uiclient.CompareNoAuthority)) {
		t.Fatalf("the verdict does not carry the status word %q:\n%s", uiclient.CompareNoAuthority, out)
	}
	if !strings.Contains(out, "--session") {
		t.Fatalf("the reason does not name the flag that supplies a side when one exists elsewhere:\n%s", out)
	}
	// The screen is NOT blamed: no loss class may appear on this answer.
	for _, forbidden := range []string{string(uiclient.DamagePaintLoss), string(uiclient.DamageToRow)} {
		if strings.Contains(out, "damage="+forbidden) {
			t.Fatalf("a missing authority was reported with the loss class %q — the evidence does not support blaming the paint:\n%s", forbidden, out)
		}
	}
}

// TestUIStateCompare_EmptyAuthorityIsNotTheSameWord is the negative control that
// gives ② its meaning: a record that EXISTS and holds no assistant text is
// `could-not-run` with the verdict `no-transcript` — never the new word. If the
// two collapsed, the new word would be a synonym rather than a distinction.
func TestUIStateCompare_EmptyAuthorityIsNotTheSameWord(t *testing.T) {
	dump := uiLiveDumpPair(t)
	writeCompareSessionLog(t, filepath.Join(filepath.Dir(dump), compareSessionFileName), nil)

	out, code := uiStateRun(t, []string{uiStateFlagCompare, uiLiveDumpLine})
	if code != uiStateExitCouldNotRun {
		t.Fatalf("exit = %d, want %d\noutput:\n%s", code, uiStateExitCouldNotRun, out)
	}
	header := compareHeaderLine(t, out, "#gopi-ui-state-compare")
	if got := compareHeaderField(t, header, "outcome"); got != compareWordCouldNotRun {
		t.Fatalf("outcome=%q, want %q: a record that was READ and holds no answer is not a missing side\nheader:\n%s",
			got, compareWordCouldNotRun, header)
	}
	if got := compareHeaderField(t, header, "session_file"); got == uiStateSessionNotRead {
		t.Fatalf("session_file=%q: the authority WAS read (and held nothing) — the word no-authoritative-side must not be reachable from this artifact\nheader:\n%s",
			got, header)
	}
	if !strings.Contains(out, string(uiclient.CompareNoTranscript)) {
		t.Fatalf("the verdict is not the read-and-empty one (%q):\n%s", uiclient.CompareNoTranscript, out)
	}
}

// TestUIStateCompare_UISurfaceRefusalNamesItsOwnSurfaceKeepsTheTwinHonest is a
// fixture-integrity guard: the captured pair must still be the artifact of a ui
// surface, with a grid that carries the row. A fixture that silently stopped
// being the measured one would make every case above pass for the wrong reason.
func TestUIStateCompare_UISurfaceRefusalNamesItsOwnSurfaceKeepsTheTwinHonest(t *testing.T) {
	dump := uiLiveDumpPair(t)
	blob, err := os.ReadFile(dump + ".json")
	if err != nil {
		t.Fatalf("read the twin: %v", err)
	}
	var twin struct {
		Identity struct {
			Surface string `json:"surface"`
		} `json:"identity"`
		Screen struct {
			Grid []string `json:"grid"`
		} `json:"screen"`
	}
	if err := json.Unmarshal(blob, &twin); err != nil {
		t.Fatalf("the twin is not a dump record: %v", err)
	}
	if twin.Identity.Surface != string(uiclient.SurfaceUI) {
		t.Fatalf("the captured pair names surface %q, want %q — the fixture is not a ui artifact",
			twin.Identity.Surface, uiclient.SurfaceUI)
	}
	found := false
	for _, row := range twin.Screen.Grid {
		if strings.Contains(row, uiLiveDumpLine) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the captured grid does not carry %q: the fixture is not the measured run", uiLiveDumpLine)
	}
}
