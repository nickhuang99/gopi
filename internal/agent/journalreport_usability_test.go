package agent

// journalreport_usability_test.go — the USABILITY half of the report-contract
// startup guard (2026-10-28).
//
// OWNERSHIP is not ENOUGH. The ownership rule asks "does this destination live
// in MY session directory"; it says nothing about "can a file be created there".
// The measured failure (session 01a18310-f082-702e-8d06-7d40bf6444da) was an
// inherited GOPI_TASK_REPORT_PATH whose PARENT DIRECTORY did not exist, so the
// contract's own write (cat > "$GOPI_TASK_REPORT_PATH") died on ENOENT after
// the path had entered the run's context as a fact.
//
// THE TWO CASE SHAPES, and why both are here:
//
//	pure usability   the parent IS this session's own directory (ownership
//	                 passes) and it cannot be written — a read-only mount, a
//	                 foreign-owned container, a full filesystem. ONLY the
//	                 usability criterion can refuse this, so it proves the
//	                 criterion exists.
//	both             a missing / file-shaped parent inside the session's tree.
//	                 Ownership refuses it too, and the refusal must then carry
//	                 BOTH reasons — the field's path failed both, and an
//	                 operator fixing one and finding the other still broken
//	                 would be back where they started.
//
// PRE-FIX (master) the pure-usability case is SILENT: the names stay set, the
// dead path is believed, and the run learns the truth at write time — or never.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
)

// TestSanitizeReportContractRefusesAnUnusableDestination is the epic's
// acceptance arm: an unusable destination must leave the environment and be
// named.
func TestSanitizeReportContractRefusesAnUnusableDestination(t *testing.T) {
	base := t.TempDir()
	// The session's OWN directory — a destination directly inside it passes
	// the ownership predicate, so only usability can be the reason to refuse.
	sessionDir := filepath.Join(base, config.SessionsDirName, "01a18310-session-uuid")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("prepare session dir: %v", err)
	}
	// The field spelling: a container name that does not exist, inside this
	// session's tree.
	missingDir := filepath.Join(sessionDir, "01a18310f082702e8d067d40bf6444da")
	missingParentReport := filepath.Join(missingDir, taskreport.FileName)
	// The other way a write dies and ownership cannot see: a FILE where the
	// parent directory should be (ENOTDIR).
	fileParent := filepath.Join(sessionDir, "not-a-dir")
	if err := os.WriteFile(fileParent, []byte("x"), 0o644); err != nil {
		t.Fatalf("prepare the file-shaped parent: %v", err)
	}

	cases := []struct {
		name string
		// path is the destination handed over in taskreport.PathEnv.
		path string
		// inOwnDir says the destination's PARENT is the session's own directory,
		// so ownership passes and only usability can refuse.
		inOwnDir bool
		// readOnlyOwnDir makes that directory unwritable for the duration of
		// the case (the pure-usability shape). Restored in Cleanup so the
		// temp tree can still be removed.
		readOnlyOwnDir bool
		// skipAsRoot: access(2) answers writable for uid 0, so the criterion
		// cannot be exhibited there — an AUDITABLE skip, never a silent pass.
		skipAsRoot  bool
		wantRefused bool
		// wantInLine lists substrings the refusal line must carry: the PATH, the
		// offending DIRECTORY, and each REASON. All of it is the deliverable.
		wantInLine []string
		why        string
	}{
		{
			name:     "a usable destination inside this session's dir is KEPT",
			path:     filepath.Join(sessionDir, taskreport.FileName),
			inOwnDir: true,
			why:      "the ordinary case: ours AND usable, so the contract stays a fact and nothing is printed",
		},
		{
			name:           "this session's OWN directory being unwritable is REFUSED",
			path:           filepath.Join(sessionDir, taskreport.FileName),
			inOwnDir:       true,
			readOnlyOwnDir: true,
			skipAsRoot:     true,
			wantRefused:    true,
			wantInLine:     []string{sessionDir, taskreport.FileName, "not writable"},
			why:            "PURE USABILITY: ownership passes, and the write still cannot happen — the criterion the guard did not have",
		},
		{
			name:        "a MISSING parent directory is REFUSED",
			path:        missingParentReport,
			wantRefused: true,
			wantInLine: []string{
				missingParentReport, missingDir, "does not exist",
				"another session's report",
			},
			why: "the measured ENOENT — and the field path failed ownership too, so the refusal must carry both reasons",
		},
		{
			name:        "a FILE where the parent directory should be is REFUSED",
			path:        filepath.Join(fileParent, taskreport.FileName),
			wantRefused: true,
			wantInLine:  []string{fileParent, "not a directory"},
			why:         "ENOTDIR at write time is the same class of unusable destination",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipAsRoot && os.Geteuid() == 0 {
				t.Skip("running as root: access(2) answers writable for uid 0, so the unwritable-parent criterion cannot be exhibited here")
			}
			if tc.readOnlyOwnDir {
				if err := os.Chmod(sessionDir, 0o555); err != nil {
					t.Fatalf("make %s read-only: %v", sessionDir, err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(sessionDir, 0o755); err != nil {
						t.Fatalf("restore %s: %v", sessionDir, err)
					}
				})
			}
			own := ""
			if tc.inOwnDir {
				own = sessionDir
			}
			t.Setenv(taskreport.PathEnv, tc.path)
			t.Setenv(taskreport.OwnEnv, "")
			var refusals []string
			removed := SanitizeReportContract(own, func(reason string) { refusals = append(refusals, reason) })

			present := func(name string) bool {
				_, ok := os.LookupEnv(name)
				return ok
			}
			if !tc.wantRefused {
				if len(removed) != 0 || len(refusals) != 0 {
					t.Fatalf("%s: the guard refused a usable destination (removed=%v, refusals=%v) — %s", tc.name, removed, refusals, tc.why)
				}
				if !present(taskreport.PathEnv) {
					t.Fatalf("%s: the guard removed a contract that is ours and usable", tc.name)
				}
				return
			}
			// PRE-FIX THIS IS THE SILENCE: the names stay set and the dead path
			// is believed. Both halves of the refusal are asserted.
			for _, name := range []string{taskreport.PathEnv, taskreport.OwnEnv} {
				if present(name) {
					val, _ := os.LookupEnv(name)
					t.Fatalf("%s: %s is still present (%q) after the guard — an unusable destination must not survive as a fact in this run's context (%s)",
						tc.name, name, val, tc.why)
				}
			}
			if len(removed) != 2 {
				t.Fatalf("%s: expected both names in the removal record, got %v", tc.name, removed)
			}
			if len(refusals) != 1 {
				t.Fatalf("%s: expected exactly one refusal line for the operator, got %d: %v", tc.name, len(refusals), refusals)
			}
			for _, want := range tc.wantInLine {
				if !strings.Contains(refusals[0], want) {
					t.Fatalf("%s: the refusal must NAME %q — got: %s", tc.name, want, refusals[0])
				}
			}
		})
	}
}

// TestReportDestinationUsableCreatesNothing pins the read face's promise that
// the usability decision WRITES NOTHING and MKDIRs NOTHING: every startup runs
// this guard, so a probe file (the exporter's shape) would leave litter in
// other people's containers — and, worse, a refusal that MkdirAll'd the parent
// would MINT the misspelling the bad path named as a second sessions container.
func TestReportDestinationUsableCreatesNothing(t *testing.T) {
	sessionDir := t.TempDir()
	before := dirEntries(t, sessionDir)
	if err := reportDestinationUsable(filepath.Join(sessionDir, taskreport.FileName)); err != nil {
		t.Fatalf("a writable existing parent must be usable: %v", err)
	}
	missing := filepath.Join(sessionDir, "nope", taskreport.FileName)
	if err := reportDestinationUsable(missing); err == nil {
		t.Fatalf("a missing parent directory %s was reported USABLE — that is the ENOENT this guard exists to catch", filepath.Dir(missing))
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("the guard CREATED %s while refusing it (stat err=%v) — a refusal must never MkdirAll: that mints the misspelling as a second session container", filepath.Dir(missing), err)
	}
	after := dirEntries(t, sessionDir)
	if before != after {
		t.Fatalf("the usability check left entries behind in %s: before=%q after=%q — the read face must not write", sessionDir, before, after)
	}
}

// dirEntries renders a directory's entries as one comparable string.
func dirEntries(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}
