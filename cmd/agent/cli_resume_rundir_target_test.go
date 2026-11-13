package main

// Regression (epic resume-target-surface, 2026-09-26): a well-formed task-id
// target whose RUN DIRECTORY holds only its raw log must resolve to THAT FILE.
//
// The defect this pins, measured before the fix: the ledger-view branch
// (tools.TaskResumeTarget) needs a row, and the run-directory fallback
// (resumeRunLog) resolved a checkpoint only from a session log that RECORDS a
// rawLogPath. A directory holding just <run>.raw.log therefore listed no run at
// all, the target fell through to session-id resolution, and the operator got
//
//	gopi session resume: cannot resolve session id "<task-id>": ...
//	gopi session resume: resume target "<task-id>" is neither a raw log path nor an
//	             existing session id
//
// while the checkpoint sat one directory away — the exact shape of an isolated
// profile. The test drives resolveResumeTarget, the ONE resolution entry point,
// under a pinned GOPI_HOME so the fixture is the whole filesystem truth.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

func TestResumeTargetResolvesRunDirRawLog(t *testing.T) {
	// The run directory is resolved through the SAME mapping production uses
	// (config.GopiDir → rundir), so the fixture lands where the resolver
	// looks. The suite pins GOPI_HOME to a per-test scratch home (Rule 34);
	// asking for the effective value is what keeps this test honest instead
	// of asserting against a path nothing reads.
	gopiDir, err := config.GopiDir()
	if err != nil {
		t.Fatalf("GopiDir: %v", err)
	}

	const target = "t-1789999999"
	runDir := filepath.Join(gopiDir, config.SessionsDirName, target)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	raw := filepath.Join(runDir, target+scratch.SessionRawLogFileSuffix)
	if err := os.WriteFile(raw, []byte("→ REQUEST\n{}\n"), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	got, err := resolveResumeTarget(target)
	if err != nil {
		t.Fatalf("resolveResumeTarget(%q) errored: %v — the run directory's own raw log is the checkpoint", target, err)
	}
	if got != raw {
		t.Fatalf("resolveResumeTarget(%q) = %q, want %q", target, got, raw)
	}
	if !resumeTargetWasTaskID {
		t.Errorf("resumeTargetWasTaskID = false: a target resolved through the run-directory branch must be treated as a task id (the row-linkage path)")
	}

	// A target with NO run directory keeps the pre-existing behaviour: it falls
	// through to session-id resolution and reports the target unresolvable.
	// The message must name the resolution it actually attempted.
	if _, err := resolveResumeTarget("t-1789999998"); err == nil {
		t.Fatal("a target with no run directory must still be refused")
	} else if !strings.Contains(err.Error(), "t-1789999998") {
		t.Errorf("refusal = %v, want it to name the target", err)
	}
}
