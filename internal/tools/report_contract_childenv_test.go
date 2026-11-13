package tools

// report_contract_childenv_test.go — the SPAWN-BOUNDARY proof of the
// report-destination ownership guard (epic nested-report-path-clobber,
// 2026-09-15).
//
// The defect: taskreport.PathEnv is the report destination of ONE session, but
// the environment travels down the whole tree. A gopi launched from an agent
// session's shell inherited the PARENT's path, took it for its own, and wrote
// its report over the parent's task-report.md (measured, session 01a0a4ee).
// These cases pin BOTH halves of the fix at the env seam every spawned shell
// goes through (the bash tool and `job start` share stripWrapperIdentity):
//
//	foreign destination → dropped (the child that caused the clobber)
//	own destination     → KEPT    (the executor writes its narrative through a
//	                              bash tool call to "$GOPI_TASK_REPORT_PATH" —
//	                              the seam's documented transport, so a strip
//	                              that ignored ownership would break the run it
//	                              is supposed to protect)
//	no claim at all     → KEPT    (nothing inherited)
//
// Ownership is judged against the session directory the process recorded for
// itself (signalrec.SessionDir), which is why these tests set it: the shell
// sanitizer asks the leaf package's ONE predicate
// (taskreport.ProcessOwnsContract) exactly as production does.
//
// The names are referenced from the packages that own them (Rule 1b/13): no
// "GOPI_TASK_REPORT_PATH" literal appears here.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/taskreport"
)

// reportContractSessionName is the directory name of the session this test
// process plays (its OWN session).
const reportContractSessionName = "01a0a4ee-own-session"

// reportContractForeignDir builds two session directories — this process's own
// and ANOTHER session's — and returns both, plus this process's own name (the
// value an owner marker carries).
func reportContractDirs(t *testing.T) (ownDir, foreignDir string) {
	t.Helper()
	base := t.TempDir()
	ownDir = filepath.Join(base, taskreport.FileName+".sessions", reportContractSessionName)
	foreignDir = filepath.Join(base, taskreport.FileName+".sessions", "01a0a62a-other-session")
	for _, d := range []string{ownDir, foreignDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("prepare %s: %v", d, err)
		}
	}
	// The in-process authority on "which session directory is mine".
	signalrec.SetSessionDir(ownDir, "", "")
	t.Cleanup(func() { signalrec.SetSessionDir("", "", "") })
	return ownDir, foreignDir
}

// setReportContractEnv installs a report contract in the process environment:
// the destination (a report inside reportDir) plus the owner marker.
func setReportContractEnv(t *testing.T, reportDir, owner string) {
	t.Helper()
	t.Setenv(taskreport.PathEnv, filepath.Join(reportDir, taskreport.FileName))
	t.Setenv(taskreport.OwnEnv, owner)
}

// TestSpawnedShellDoesNotInheritAForeignReportContract is the regression for
// the clobber itself: a shell spawned by a process that does NOT own the
// inherited report destination must not be handed it — a `gopi` launched from
// that shell can then neither read the path nor write it. On master (a plain
// os.Environ() copy) this case fails: the pair reaches the shell, and that is
// exactly how the parent's deliverable was destroyed.
func TestSpawnedShellDoesNotInheritAForeignReportContract(t *testing.T) {
	ownDir, foreignDir := reportContractDirs(t)
	setReportContractEnv(t, foreignDir, "01a0a62a-other-session")

	// The strip must be surgical: an unrelated variable survives.
	t.Setenv("GOPI_REPORT_CONTRACT_KEEP_PROBE", "keep-me")

	env := bashChildEnv()
	for _, name := range reportContractEnvVars {
		if got := taskreport.EnvValue(env, name); got != "" {
			t.Fatalf("bashChildEnv handed the shell an inherited report contract: %s=%q — "+
				"a nested gopi would write its own report over the owner's %s (this process's own dir is %s)",
				name, got, taskreport.FileName, ownDir)
		}
	}
	if got := taskreport.EnvValue(env, "GOPI_REPORT_CONTRACT_KEEP_PROBE"); got != "keep-me" {
		t.Fatalf("the report-contract strip dropped an unrelated variable (got %q) — it must subtract, never whitelist", got)
	}
}

// TestSpawnedShellKeepsThisRunsOwnReportContract is the counterweight: the
// session's OWN destination must survive the same seam, because its narrative
// is written THROUGH a bash tool call to "$GOPI_TASK_REPORT_PATH" (the
// transport the task-report seam tests exercise). A strip that ignored
// ownership would silently break every report ever written.
func TestSpawnedShellKeepsThisRunsOwnReportContract(t *testing.T) {
	ownDir, _ := reportContractDirs(t)
	setReportContractEnv(t, ownDir, reportContractSessionName)

	env := bashChildEnv()
	if got := taskreport.EnvValue(env, taskreport.PathEnv); got == "" {
		t.Fatalf("bashChildEnv stripped THIS session's own report destination (own dir %s) — the executor writes its report through its bash tool, so the seam must keep it", ownDir)
	}
	if got := taskreport.EnvValue(env, taskreport.OwnEnv); got != reportContractSessionName {
		t.Fatalf("%s = %q, want %q — the owner marker travels with the path", taskreport.OwnEnv, got, reportContractSessionName)
	}
}

// TestSpawnedShellKeepsAMarkerlessOwnContract: the HARNESS hand-off — a
// destination inside this session's own directory and NO owner marker. A rule
// that demanded a matching id refused exactly this (the first design's defect,
// measured on the live session), so the case is pinned here too.
func TestSpawnedShellKeepsAMarkerlessOwnContract(t *testing.T) {
	ownDir, _ := reportContractDirs(t)
	setReportContractEnv(t, ownDir, "")

	env := bashChildEnv()
	if got := taskreport.EnvValue(env, taskreport.PathEnv); got == "" {
		t.Fatalf("a marker-less hand-off of THIS session's own destination was dropped — the harness exports no marker, so ownership must be decided by the DIRECTORY")
	}
}

// TestSpawnedShellDropsAnUnrecordedContract: with no session directory recorded
// for this process there is nothing it can prove ownership against, so the pair
// goes — and the drop is REPORTED (the caller's removed list is the record the
// spawn seam prints; Rule 15: never a silent discard).
func TestSpawnedShellDropsAnUnrecordedContract(t *testing.T) {
	_, foreignDir := reportContractDirs(t)
	signalrec.SetSessionDir("", "", "") // this process knows no directory of its own
	setReportContractEnv(t, foreignDir, "")

	env, removed := stripWrapperIdentity(os.Environ())
	if got := taskreport.EnvValue(env, taskreport.PathEnv); got != "" {
		t.Fatalf("an unowned report destination survived the shell env (%q) — with no session directory of its own the process cannot claim it", got)
	}
	if !reportContractListHas(removed, taskreport.PathEnv) {
		t.Fatalf("the report destination was dropped SILENTLY (removed=%v) — the strip must be reported", removed)
	}
}

// TestStripInheritedReportContractIsUnconditional: the `task run` wrapper's own
// strip (StripInheritedReportContract) removes the pair whatever it says — that
// wrapper is about to export the CHILD's pair, so an inherited path must not
// survive as a destination any descendant could read in the window before the
// child's own export lands.
func TestStripInheritedReportContractIsUnconditional(t *testing.T) {
	env := []string{
		taskreport.PathEnv + "=/tmp/whatever/" + taskreport.FileName,
		taskreport.OwnEnv + "=01a0a62a-other-session",
		"GOPI_UNRELATED=kept",
	}
	stripped, removed := StripInheritedReportContract(env)
	if len(removed) != len(reportContractEnvVars) {
		t.Fatalf("StripInheritedReportContract removed %v, want both contract names", removed)
	}
	if len(stripped) != 1 || stripped[0] != "GOPI_UNRELATED=kept" {
		t.Fatalf("StripInheritedReportContract returned %v, want only the unrelated entry", stripped)
	}
}

// reportContractListHas reports whether list carries name (the removed-name
// record the strip returns).
func reportContractListHas(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}
