package agent

// journalreport_test.go — the READ-FACE proof of the report-destination
// ownership guard (epic nested-report-path-clobber, 2026-09-15).
//
// The guard runs at the one moment ownership is knowable — when the session's
// own directory exists (cmd/agent's printLogPaths) — and its decision is
// tabulated here, because it is the half of the fix that does not depend on
// WHO spawned the process (a script, a test harness or an operator hands the
// environment over untouched).
//
// The table deliberately carries the case that BROKE THE FIRST DESIGN: the
// harness exports taskreport.PathEnv with NO owner marker and a destination
// inside the SESSION's own directory (<sessions>/<session-uuid>/task-report.md),
// which is neither the run id nor the task id. A run-id-only rule refused it —
// i.e. it would have broken the very hand-off this epic protects. The
// directory is the authoritative owner; the marker is the extra refusal.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
)

func TestSanitizeReportContract(t *testing.T) {
	// One sandbox plays every directory: the session's own, a sibling
	// session's, and a bare temp dir (the shape the observed clobber used).
	base := t.TempDir()
	sessionDir := filepath.Join(base, config.SessionsDirName, "01a0a4ee-session-uuid")
	otherSessionDir := filepath.Join(base, config.SessionsDirName, "01a0a62a-other-uuid")
	scratchDir := filepath.Join(base, "somewhere-else")
	for _, d := range []string{sessionDir, otherSessionDir, scratchDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("prepare %s: %v", d, err)
		}
	}

	cases := []struct {
		name       string
		path       string
		owner      string
		sessionDir string
		// absent declares that the two names are NOT in the environment at all
		// (the ordinary session): the t.Setenv calls first make the restore
		// correct even when the test process itself inherited a contract
		// (their cleanup snapshots with os.LookupEnv, so an UNSET variable
		// comes back UNSET), then the vars are removed for the case.
		absent      bool
		wantRefused bool
		why         string
	}{
		{
			name: "harness hand-off with no marker is KEPT", path: filepath.Join(sessionDir, taskreport.FileName),
			owner: "", sessionDir: sessionDir, wantRefused: false,
			why: "the harness exports the path with no owner marker; the destination is inside THIS session's directory, so it is ours (a run-id rule refused it — the defect of the first design)",
		},
		{
			name: "marker naming this session's dir is KEPT", path: filepath.Join(sessionDir, taskreport.FileName),
			owner: filepath.Base(sessionDir), sessionDir: sessionDir, wantRefused: false,
			why: "the marker agrees with the directory",
		},
		{
			name: "another session's directory is REFUSED", path: filepath.Join(otherSessionDir, taskreport.FileName),
			owner: "", sessionDir: sessionDir, wantRefused: true,
			why: "the destination belongs to a different session's directory — writing it destroys that session's deliverable (the observed clobber)",
		},
		{
			name: "a bare path outside any session dir is REFUSED", path: filepath.Join(scratchDir, taskreport.FileName),
			owner: "", sessionDir: sessionDir, wantRefused: true,
			why: "not this session's directory = not this session's report",
		},
		{
			name: "a foreign marker is REFUSED even inside the directory", path: filepath.Join(sessionDir, taskreport.FileName),
			owner: filepath.Base(otherSessionDir), sessionDir: sessionDir, wantRefused: true,
			why: "the marker is decisive: it exists to catch a contract carried in by a process that shares the exporter's directory",
		},
		{
			name: "no contract at all is a no-op", absent: true, path: "", owner: "", sessionDir: sessionDir, wantRefused: false,
			why: "an ordinary session that inherited nothing must not be touched",
		},
		{
			name: "no session dir of its own cannot claim", path: filepath.Join(sessionDir, taskreport.FileName),
			owner: "", sessionDir: "", wantRefused: true,
			why: "with no directory of its own the process can prove nothing about the destination, so the contract goes — the safe direction, and the same one the shell sanitizer takes (one predicate)",
		},
		{
			name: "a marker with no path is REFUSED", path: "", owner: filepath.Base(sessionDir), sessionDir: sessionDir, wantRefused: true,
			why: "a marker names no destination on its own, and a present-but-empty path is not a destination this session could write — the leftover pair is exactly the ambiguity this epic removes, so it goes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv restores automatically; passing "" still makes the
			// variable PRESENT, which is the state a harness actually hands
			// over — the guard must read presence, not truthiness.
			t.Setenv(taskreport.PathEnv, tc.path)
			t.Setenv(taskreport.OwnEnv, tc.owner)
			if tc.absent {
				// The ABSENT state is this case's subject, and t.Setenv can only
				// SET a variable: after it os.LookupEnv reports the name PRESENT
				// (holding ""), which is exactly the state the guard must tell
				// apart from absence. Cleanup still restores both names — the
				// t.Setenv calls above recorded their prior state. The same
				// absence-probe shape as internal/testutil/envsnapshot_test.go.
				// merge-note (fix/standards-baseline-rekey2, 2026-09-17): both
				// parents carry the SAME escape at these two sites and only
				// differ in how they spell it — the branch side
				// (fix/standards-baseline-rekey, d6929017) wrote one
				// `env-leak-ok:` marker per os.Unsetenv call and noted that the
				// pair must go absent TOGETHER for the no-contract case; master
				// (f9311942) folded the pair into ONE loop with a single marker
				// and added the os.LookupEnv-snapshot rationale. Union: master's
				// loop and its snapshot rationale, plus the branch's togetherness
				// fact — the loop is exactly that fact expressed in code, so one
				// marker now covers the single remaining call site.
				for _, name := range []string{taskreport.PathEnv, taskreport.OwnEnv} {
					// env-leak-ok: absence is the SUBJECT of this case — the
					// guard decides on PRESENCE (os.LookupEnv), so the two
					// names must actually leave the environment; t.Setenv
					// cannot express that (it only SETS, after which LookupEnv
					// reports present). The restore is the t.Setenv cleanup
					// above — it snapshots with os.LookupEnv, so a variable
					// that was UNSET comes back UNSET (never "").
					if err := os.Unsetenv(name); err != nil {
						t.Fatalf("prepare the absent case: %v", err)
					}
				}
			}

			var refusals []string
			removed := SanitizeReportContract(tc.sessionDir, func(reason string) { refusals = append(refusals, reason) })

			for _, name := range []string{taskreport.PathEnv, taskreport.OwnEnv} {
				_, present := os.LookupEnv(name)
				if tc.absent {
					if present {
						t.Fatalf("%s: %s should not be present at all", tc.name, name)
					}
					continue
				}
				if present == tc.wantRefused {
					val, _ := os.LookupEnv(name)
					t.Fatalf("%s: %s present=%v value=%q after the guard (removed=%v, refused=%v) — %s",
						tc.name, name, present, val, removed, tc.wantRefused, tc.why)
				}
			}
			if tc.wantRefused {
				// Rule 15: a refusal is a RECORD, not a silence — the caller
				// prints exactly these names.
				if len(removed) == 0 {
					t.Fatalf("%s: the guard dropped the contract but returned no record — a silent refusal is indistinguishable from nothing having happened", tc.name)
				}
				if len(refusals) != 1 {
					t.Fatalf("%s: the guard dropped the contract but reported %d refusal line(s) (%v) — the operator must be able to tell WHICH path was dropped and WHY", tc.name, len(refusals), refusals)
				}
			} else if len(removed) != 0 {
				t.Fatalf("%s: the guard reported removals %v but nothing should have been touched (refusals: %v)", tc.name, removed, refusals)
			} else if len(refusals) != 0 {
				t.Fatalf("%s: the guard kept the contract but still reported a refusal: %v", tc.name, refusals)
			}
		})
	}
}

// TestReportContractOwnershipResolvesEquivalentPaths: an equivalent spelling of
// the SAME directory (a symlinked sessions root, a relative form) must count as
// ownership — a textual comparison would refuse a legitimate hand-off.
func TestReportContractOwnershipResolvesEquivalentPaths(t *testing.T) {
	base := t.TempDir()
	sessionDir := filepath.Join(base, config.SessionsDirName, "sess")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("prepare session dir: %v", err)
	}
	link := filepath.Join(base, "sessions-link")
	if err := os.Symlink(filepath.Join(base, config.SessionsDirName), link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	// The same directory reached through the symlink is still ours.
	if !taskreport.OwnedBy(filepath.Join(link, "sess", taskreport.FileName), "", sessionDir) {
		t.Fatalf("a symlinked spelling of this session's own directory was refused — ownership must be resolved, not compared textually")
	}
}
