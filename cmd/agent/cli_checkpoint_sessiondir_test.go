// cli_checkpoint_sessiondir_test.go — `gopi session checkpoint list` against BOTH raw-log
// layouts, driven through the REAL binary in a private sandbox profile.
//
// THE DEFECT (session-dir-unify S1, 3a816775). Raw HTTP logs moved from the
// flat <gopi>/raw/gopi-raw-<stamp>.log into the run directory that wrote them
// (<sessions>/<run-dir>/<run-uuid>.raw.log). The indexer kept its one-level,
// "gopi-raw-" prefix filter AND the CLI kept defaulting to the flat directory,
// so the listing answered "no raw logs found" over logs it never opened.
//
// WHAT THIS PINS, through argv (Rule 31 — the binary, not the function):
//   - the co-located layout is LISTED (the zero-result that was the defect),
//   - the zero result is SELF-EXPLAINING (directories + criteria on stderr),
//   - the legacy flat layout still lists (regression),
//   - `--dir` naming the sessions root behaves like the default,
//   - a private sandbox profile is used throughout (Rules 22/34): the child's
//     env carries GOPI_HOME and nothing else GOPI-shaped (cliContractEnv), so
//     the operator's own ~/.gopi is never read or written.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// cpsdRunLogBody is one complete round, byte-shaped as the raw logger writes it
// (marker + payload + newline; see internal/agent.RawLogger.LogFunc).
const cpsdRunLogBody = "→ REQUEST\n" +
	`{"model":"m-sessiondir","max_tokens":9,"messages":[{"role":"user","content":[{"type":"text","text":"colocated"}]}]}` + "\n" +
	"← RESPONSE\n" +
	`{"id":"r","content":[{"type":"text","text":"ok"}]}` + "\n"

// cpsdProfile is a private profile whose sessions root holds one co-located raw
// log, plus a legacy flat log in the flat raw directory — both layouts at once.
type cpsdProfile struct {
	home        string // GOPI_HOME (the parent of the .gopi data dir)
	sessionsDir string // <home>/.gopi/sessions
	runLog      string // <sessionsDir>/<run-uuid>/<run-uuid>.raw.log
	flatDir     string // <home>/.gopi/raw
	flatLog     string // <flatDir>/gopi-raw-<stamp>.log
}

func cpsdNewProfile(t *testing.T) cpsdProfile {
	t.Helper()
	home := t.TempDir()
	gopiDir := filepath.Join(home, config.ProfileDirName)
	sessionsDir := filepath.Join(gopiDir, config.SessionsDirName)
	// The run id doubles as the run DIRECTORY name — the writer's own layout
	// (internal/agent.sessionDirForID) and the reason the co-located log is one
	// directory below the sessions root.
	const runID = "01a0ed99-0000-4000-8000-000000000000"
	runDir := filepath.Join(sessionsDir, runID)
	flatDir := filepath.Join(gopiDir, scratch.RawLogDirName())
	for _, d := range []string{runDir, flatDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	runLog := filepath.Join(runDir, runID+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(runLog, []byte(cpsdRunLogBody), 0o600); err != nil {
		t.Fatalf("write co-located raw log: %v", err)
	}
	flatLog := filepath.Join(flatDir, scratch.RawLogNamePrefix+"20260101-000000"+scratch.RawLogFileSuffix)
	if err := os.WriteFile(flatLog, []byte(cpsdRunLogBody), 0o600); err != nil {
		t.Fatalf("write flat raw log: %v", err)
	}
	return cpsdProfile{home: home, sessionsDir: sessionsDir, runLog: runLog, flatDir: flatDir, flatLog: flatLog}
}

// cpsdRun drives the real binary with the sandbox env and returns the three
// observables. Timeout-guarded: a parked child would otherwise hang the suite.
//
// THE ADDRESS IS THE SESSION FAMILY'S (`gopi session checkpoint <word>`), which
// is the same page through the door the hierarchy teaches and the spelling that
// survives verb-retirement. The bare `gopi checkpoint` answers in this tree too;
// driving the family address keeps this file's subject (`checkpoint list`
// against both raw-log layouts) the same surface on either side of the
// retirement (task 01a10e039a5e7a31ad0ba8455e7dc3ee, 2026-10-06).
func cpsdRun(t *testing.T, home string, sub ...string) (string, string, int) {
	t.Helper()
	args := append([]string{CliVerbSession, CliVerbCheckpoint}, sub...)
	cmd := exec.Command(gopiTestBinPath(t), familyAddressArgs(args)...)
	cmd.Env = cliContractEnv(home)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("gopi %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
		code = ee.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

// TestCheckpointListCoLocatedLayout_IsListed is the defect's green: the default
// listing finds a raw log that lives beside its run, one directory below the
// sessions root.
func TestCheckpointListCoLocatedLayout_IsListed(t *testing.T) {
	p := cpsdNewProfile(t)
	stdout, stderr, code := cpsdRun(t, p.home, "list")
	if code != 0 {
		t.Fatalf("checkpoint list exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, filepath.Base(p.runLog)) {
		t.Errorf("the co-located raw log %s was NOT listed (the defect)\n--- stdout ---\n%s\n--- stderr ---\n%s",
			filepath.Base(p.runLog), stdout, stderr)
	}
	if strings.Contains(stdout, "no raw logs found") {
		t.Errorf("zero-result message over a log that exists:\n%s", stdout)
	}
	if !strings.Contains(stdout, "reqs") {
		t.Errorf("the listing header is missing:\n%s", stdout)
	}
	// The self-report rides stderr in EVERY outcome, found or not.
	if !strings.Contains(stderr, "criteria") || !strings.Contains(stderr, p.sessionsDir) {
		t.Errorf("the scan report (directories + criteria) is not on stderr:\n%s", stderr)
	}
}

// TestCheckpointListZeroResult_ExplainsItself pins the diagnosability half: with
// nothing to find, the child names every directory it listed and the exact name
// rule it applied — not the bare "no raw logs found" that made the defect
// unreadable.
func TestCheckpointListZeroResult_ExplainsItself(t *testing.T) {
	home := t.TempDir()
	empty := filepath.Join(home, config.ProfileDirName, config.SessionsDirName)
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stdout, stderr, code := cpsdRun(t, home, "list")
	if code != 0 {
		t.Fatalf("a zero-result listing is not an error: exit=%d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "no raw logs found") {
		t.Fatalf("expected the zero-result line on stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "criteria") {
		t.Errorf("the zero-result line must point at the scan report:\n%s", stdout)
	}
	for _, want := range []string{empty, "criteria", "scanned"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not carry %q — a zero result is unexplained:\n%s", want, stderr)
		}
	}
	for _, want := range []string{scratch.RawLogNamePrefix, scratch.SessionRawLogFileSuffix} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the report does not state the acceptance rule %q:\n%s", want, stderr)
		}
	}
}

// TestCheckpointListDirSessionsRoot pins the `--dir` face: naming the sessions
// root explicitly lists exactly what the default does.
func TestCheckpointListDirSessionsRoot(t *testing.T) {
	p := cpsdNewProfile(t)
	stdout, stderr, code := cpsdRun(t, p.home, "list", "--dir", p.sessionsDir)
	if code != 0 {
		t.Fatalf("checkpoint list --dir <sessions> exit=%d\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, filepath.Base(p.runLog)) {
		t.Errorf("--dir naming the sessions root listed nothing from the run directories:\n%s%s", stdout, stderr)
	}
}

// TestCheckpointListLegacyFlatStillLists is the REGRESSION guard: the flat
// layout a pre-unification run wrote is still indexed, by --dir and by the
// GOPI_RAW_LOG_DIR override alike.
func TestCheckpointListLegacyFlatStillLists(t *testing.T) {
	p := cpsdNewProfile(t)
	for _, tc := range []struct {
		name string
		env  string
		args []string
	}{
		{name: "explicit --dir", args: []string{"list", "--dir", p.flatDir}},
		{name: "GOPI_RAW_LOG_DIR", env: p.flatDir, args: []string{"list"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(gopiTestBinPath(t), append([]string{CliVerbSession, CliVerbCheckpoint}, tc.args...)...)
			cmd.Env = cliContractEnv(p.home)
			if tc.env != "" {
				cmd.Env = append(cmd.Env, rawLogDirEnv+"="+tc.env)
			}
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("checkpoint list exit=%v\n%s%s", err, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), filepath.Base(p.flatLog)) {
				t.Errorf("the legacy flat raw log %s is no longer listed:\n%s%s",
					filepath.Base(p.flatLog), stdout.String(), stderr.String())
			}
		})
	}
}

// TestCheckpointExportCoLocatedByRunID pins that export's id resolution reaches
// the co-located log from its bare run id — the address a reader has (the run
// directory's name) rather than a full path.
func TestCheckpointExportCoLocatedByRunID(t *testing.T) {
	p := cpsdNewProfile(t)
	runID := filepath.Base(filepath.Dir(p.runLog))
	stdout, stderr, code := cpsdRun(t, p.home, "export", runID)
	if code != 0 {
		t.Fatalf("checkpoint export %s exit=%d\n%s%s", runID, code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"m-sessiondir"`) {
		t.Errorf("export did not print the co-located checkpoint body:\n%s%s", stdout, stderr)
	}
}
