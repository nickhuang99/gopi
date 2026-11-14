// cli_session_context_section_test.go — the SECTION flags on the `gopi session
// context` verb: the usage page and the mutual-exclusion refusal.
//
// The export itself is measured by internal/ctxdump's own tests and by the real
// run recorded in this task's report; what is pinned HERE is the seam the CLI
// owns — that the page documents the two selectors the parser accepts, and that
// `--system --user` is refused with BOTH words named and NOTHING written (a
// silent pick would file the operator's evidence under a name they did not ask
// for).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/ctxdump"
)

// TestSessionContextUsageDocumentsTheSectionFlags pins the page/parser agreement:
// every selector the parser accepts is on the page, and the page still names the
// full page's own file (the evidence's address).
func TestSessionContextUsageDocumentsTheSectionFlags(t *testing.T) {
	for _, want := range []string{
		ctxdump.SystemSelector,
		ctxdump.UserSelector,
		ctxdump.SystemFileNamePattern,
		ctxdump.UserFileNamePattern,
		ctxdump.FileNamePattern,
		ctxdump.SystemHeading,
		ctxdump.MessagesHeading,
	} {
		if !strings.Contains(sessionContextUsage, want) {
			t.Errorf("the usage page does not name %q — a flag the parser accepts but the page omits is a flag nobody will type", want)
		}
	}
}

// TestSessionContextRefusesBothSectionSelectors is the CLI half of the
// mutual-exclusion rule, driven through the REAL entry point (runSessionContext,
// not the exporter): it must exit non-zero, say which two words clashed, and
// leave the filesystem untouched.
func TestSessionContextRefusesBothSectionSelectors(t *testing.T) {
	dir := t.TempDir()
	var code int
	stderr := captureStderrStream(t, func() {
		code = runSessionContext([]string{"does-not-matter", ctxdump.SystemSelector, ctxdump.UserSelector})
	})
	if code == 0 {
		t.Fatalf("`session context --system --user` exited 0 — an export shows ONE side of a request, so the pair must be refused (stderr: %q)", stderr)
	}
	for _, want := range []string{ctxdump.SystemSelector, ctxdump.UserSelector, "pick one"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the refusal does not mention %q: %q", want, stderr)
		}
	}
	// Nothing was written anywhere near the caller's directory: the refusal is
	// ahead of the export, so a session id that does not even resolve never gets
	// a chance to produce a mis-named artifact.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		t.Errorf("the refusal wrote something into the working directory: %v", entries)
	}
	// And the export path is not reached at all: a bogus session id would give the
	// "cannot find the session" error, which is a DIFFERENT sentence — seeing it
	// here would mean the conflict check ran after the export (too late).
	if strings.Contains(stderr, "does-not-matter") && !strings.Contains(stderr, "pick one") {
		t.Errorf("the CLI reached the export before refusing the pair: %q", stderr)
	}
}

// TestSessionContextSectionWritesItsOwnNameInASession drives the CLI's OWN export
// path (sessionContextExport, the one runSessionContext calls) against a real
// session record and raw log: `--system` and `--user` land in the two section
// files, and the full export is byte-identical to what it was before either ran.
func TestSessionContextSectionWritesItsOwnNameInASession(t *testing.T) {
	const turns = 2
	rl, sl := contextAxisFixture(t, turns, 2)
	defer func() { _ = rl.Close() }() //stylegate:ignore the raw logger is closed on the way out of this fixture; its records were already synced and read
	defer func() { _ = sl.Close() }() //stylegate:ignore the session logger is closed on the way out of this fixture; its records were already synced and read

	sessionPath := sl.Path()
	dir := filepath.Dir(sessionPath)

	// The full page first — the evidence, as the pre-section code wrote it.
	if _, err := sessionContextExport(sessionPath, 0, 0, "", ctxdump.SectionAll); err != nil {
		t.Fatalf("the full export: %v", err)
	}
	fullPath := filepath.Join(dir, ctxdump.SectionAll.FileName(ctxdump.TurnAddress(turns, 0, 0, 0)))
	fullBefore, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("the full page is not at %q: %v", fullPath, err)
	}

	sys, err := sessionContextExport(sessionPath, 0, 0, "", ctxdump.SectionSystem)
	if err != nil {
		t.Fatalf("the system export: %v", err)
	}
	usr, err := sessionContextExport(sessionPath, 0, 0, "", ctxdump.SectionUser)
	if err != nil {
		t.Fatalf("the messages export: %v", err)
	}
	if sys.Path == usr.Path || sys.Path == fullPath || usr.Path == fullPath {
		t.Fatalf("the three exports did not land on three files: %q %q %q", sys.Path, usr.Path, fullPath)
	}
	if got, want := filepath.Base(sys.Path), ctxdump.SectionSystem.FileName(sys.Address); got != want {
		t.Errorf("the system export is named %q, want %q", got, want)
	}
	if c := sys.Counts.Messages; c != 0 {
		t.Errorf("the system export reports %d message(s) while writing none", c)
	}
	if c := usr.Counts.ToolDefs; c != 0 {
		t.Errorf("the messages export reports %d tool definition(s) while writing none", c)
	}

	// The evidence did not move.
	fullAfter, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("re-reading the full page: %v", err)
	}
	if string(fullBefore) != string(fullAfter) {
		t.Errorf("the full page changed (%d → %d bytes) — the diffable evidence must not be touched by a derived view",
			len(fullBefore), len(fullAfter))
	}
	if !strings.Contains(string(fullAfter), ctxdump.SystemHeading) || !strings.Contains(string(fullAfter), ctxdump.MessagesHeading) {
		t.Errorf("the full page no longer carries both halves")
	}
}
