package main

// report_contract_usability_test.go — the END-TO-END acceptance for the
// usability half of the report-contract startup guard (2026-10-28; Rule 31:
// a surface not executed is not delivered).
//
// The unit table in internal/agent proves the DECISION (silent before, named
// refusal after). This file proves the surface: a REAL gopi binary, built from
// THIS tree, launched with an inherited GOPI_TASK_REPORT_PATH whose parent
// directory does not exist — the measured field spelling — must
//   1. print a refusal that NAMES the path and the missing parent, and
//   2. leave the name ABSENT from its own environment, which the model's bash
//      tool observes for itself (the child's shell is the last place a stale
//      destination could survive and be believed).
//
// Pre-fix the run was silent: the path stayed set, the run's context carried it
// as a fact, and the contract's own write (cat > "$GOPI_TASK_REPORT_PATH") died
// on ENOENT far from the cause. That silence is the red proof captured in
// internal/agent/journalreport_usability_test.go; here the subject is the real
// binary's OUTPUT.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/taskreport"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// unusableReportProbeFile is where the model's bash tool records what it SAW in
// its own environment (the absence probe, written inside the fixture's work
// dir, never into a tracked path).
const unusableReportProbeFile = "report-env-probe.txt"

// unusableReportAbsentMarker is what the probe writes when the inherited name
// is gone from the child's shell. It is a literal because it is this test's own
// vocabulary, not a value the product reads.
const unusableReportAbsentMarker = "<unset>"

// TestInheritedUnusableReportDestinationIsRefused proves the startup guard
// names an unusable inherited destination AND removes it before any tool runs.
func TestInheritedUnusableReportDestinationIsRefused(t *testing.T) {
	reportSeamBinary(t) // the real binary from THIS tree; GOPI_BIN stamped

	childHome := t.TempDir()
	work := t.TempDir()

	// The inherited destination: inside a container name that does NOT exist.
	// This is the field's spelling (a cross-channel, dashless id) reduced to its
	// one relevant property — the parent directory is absent.
	missingDir := filepath.Join(childHome, config.ProfileDirName, config.SessionsDirName, nestedReportParentSession, "01a18310f082702e8d067d40bf6444da")
	inherited := filepath.Join(missingDir, taskreport.FileName)
	// Prove the premise rather than assume it: the parent really is absent.
	if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
		t.Fatalf("fixture: %s must not exist, stat err=%v", missingDir, err)
	}

	// The model's one tool call: ask its OWN shell whether the inherited name
	// survived the startup guard. Built from taskreport.PathEnv, never a literal
	// (Rule 1b/13).
	probe := "printf '%s' \"${" + taskreport.PathEnv + "-" + unusableReportAbsentMarker + "}\" > " +
		filepath.Join(work, unusableReportProbeFile) + "\n"
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_report_env_probe",
			Name: "bash",
			Args: map[string]interface{}{"command": probe},
		}},
		testutil.Resp{Text: "probe finished"},
	)
	defer mock.Close()

	// MockEnv strips every GOPI_* name off the ambient environment, so the
	// inherited contract below is the ONLY one this child can see — the same
	// inheritance a nested gopi gets from its launcher, minus the operator's own
	// profile leaking in.
	env := testutil.MockEnv(t, childHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(childHome, "memory.db"),
		// This child's OWN session container, so the run has a session dir.
		tools.TaskWrapperEnv+"="+nestedReportChildSession,
		// THE INHERITED, UNUSABLE CONTRACT.
		taskreport.PathEnv+"="+inherited,
	)

	stdout, stderr, code := runGopiMapped(t, []string{"report destination probe"}, "", env, work)
	if code != 0 {
		t.Fatalf("run failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	// 1. The refusal is PRINTED and it NAMES the path, the parent, and the reason.
	for _, want := range []string{inherited, missingDir, "does not exist"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("the startup refusal must name %q — stderr:\n%s", want, stderr)
		}
	}
	// 2. The name is GONE from the environment the tools inherit. The model's own
	// shell is the observer: if the destination survived, this prints it.
	got, err := os.ReadFile(filepath.Join(work, unusableReportProbeFile))
	if err != nil {
		t.Fatalf("the model's bash tool did not run (no probe file at %s): %v\nstderr:\n%s",
			filepath.Join(work, unusableReportProbeFile), err, stderr)
	}
	// Logged as EVIDENCE, not as an assertion: -v prints what the child's own
	// shell resolved, which is the deliverable's "the variable is gone" half.
	t.Logf("the child shell resolved $%s as %q", taskreport.PathEnv, strings.TrimSpace(string(got)))
	for _, line := range strings.Split(stderr, "\n") {
		if strings.Contains(line, taskreport.PathEnv) {
			t.Logf("child stderr: %s", line)
		}
	}
	if strings.TrimSpace(string(got)) != unusableReportAbsentMarker {
		t.Fatalf("the child's shell still carries %s=%q — an unusable inherited destination must not survive into the tool environment\nstderr:\n%s",
			taskreport.PathEnv, strings.TrimSpace(string(got)), stderr)
	}
	// 3. The guard did NOT mint the missing container: creating the parent would
	// turn the misspelling into a real second sessions directory that nothing
	// reads (this is the refusal's whole point).
	if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
		t.Fatalf("the refusal CREATED %s (stat err=%v) — it must never MkdirAll a parent it just refused", missingDir, err)
	}
}

// TestUsableInheritedReportDestinationIsKept is the counterweight: a destination
// that IS usable (its parent exists, it is writable) must be kept — otherwise
// the fix would break every report ever written.
func TestUsableInheritedReportDestinationIsKept(t *testing.T) {
	reportSeamBinary(t)

	childHome := t.TempDir()
	work := t.TempDir()
	// The child's own session container, created here so the destination's
	// parent exists (the shape the harness hands over).
	ownDir := filepath.Join(childHome, config.ProfileDirName, config.SessionsDirName, nestedReportChildSession)
	if err := os.MkdirAll(ownDir, 0o755); err != nil {
		t.Fatalf("prepare %s: %v", ownDir, err)
	}
	usable := filepath.Join(ownDir, taskreport.FileName)

	probe := "printf '%s' \"${" + taskreport.PathEnv + "-" + unusableReportAbsentMarker + "}\" > " +
		filepath.Join(work, unusableReportProbeFile) + "\n"
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_report_env_keep",
			Name: "bash",
			Args: map[string]interface{}{"command": probe},
		}},
		testutil.Resp{Text: "probe finished"},
	)
	defer mock.Close()

	env := testutil.MockEnv(t, childHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+filepath.Join(childHome, "memory.db"),
		tools.TaskWrapperEnv+"="+nestedReportChildSession,
		taskreport.PathEnv+"="+usable,
	)

	stdout, stderr, code := runGopiMapped(t, []string{"report destination probe"}, "", env, work)
	if code != 0 {
		t.Fatalf("run failed: code=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	got, err := os.ReadFile(filepath.Join(work, unusableReportProbeFile))
	if err != nil {
		t.Fatalf("no probe file at %s: %v\nstderr:\n%s", filepath.Join(work, unusableReportProbeFile), err, stderr)
	}
	if strings.TrimSpace(string(got)) != usable {
		t.Fatalf("a USABLE inherited destination was dropped: the child's shell saw %q, expected %q\nstderr:\n%s",
			strings.TrimSpace(string(got)), usable, stderr)
	}
}
