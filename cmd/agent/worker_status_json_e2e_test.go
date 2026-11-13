package main

// worker_status_json_e2e_test.go — the SHIPPED binary's machine form (epic
// worker-status-task-field, 2026-10-23).
//
// The unit tests drive the derivation; this one drives `gopi worker --status
// --json` as a real subprocess the way an operator (or a jq pipeline) does, and
// pins the two properties a pipeline depends on:
//
//	① the document is VALID JSON on stdout with the task keys always present
//	   (a consumer parses it, never greps a sentence);
//	② it is the SAME read — read-only, and refused beside an act, exactly like
//	   the text form (an unclassified alias would silently drop the act).
//
// The process table is seamed through procname.EnvProcRoot (an empty root means
// "no live worker on this host"), so the assertions do not depend on what the
// developer's machine happens to be running.

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/nick/gopi/internal/procname"
)

// wsjRun drives `gopi worker --status --json` with the given env and returns
// the exit code, stdout and stderr separately — the machine form's WHOLE point
// is that stdout is parseable, so stderr must be separable.
func wsjRun(t *testing.T, env []string, bin string, extra ...string) (int, []byte, []byte) {
	t.Helper()
	args := append([]string{CliVerbWorker, workerControlStatus, workerStatusJSONFlag}, extra...)
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run %v: %v\nstdout:%s\nstderr:%s", args, err, stdout.String(), stderr.String())
		}
		code = ee.ExitCode()
	}
	return code, stdout.Bytes(), stderr.Bytes()
}

// TestWorkerStatusJSONCLIEmitsOneParseableDocument: on a profile with no worker,
// stdout is ONE JSON object whose worker list is empty and whose keys are all
// present, and the exit code is 0 — an empty fleet is a FACT, not a failure.
func TestWorkerStatusJSONCLIEmitsOneParseableDocument(t *testing.T) {
	bin := gopiTestBinPath(t)
	// Same sandbox the text-form e2e uses: the child must not enumerate (and
	// free) the developer's worktrees.
	SandboxRepoContext(t)
	home := t.TempDir()
	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+t.TempDir())

	code, stdout, stderr := wsjRun(t, env, bin)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s %s` exited %d, want 0\nstderr:%s", CliVerbWorker,
			workerControlStatus, workerStatusJSONFlag, code, stderr)
	}
	var doc workerStatusJSONDoc
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not ONE parseable JSON document: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout, stderr)
	}
	// The keys a consumer addresses must exist even when the fleet is empty
	// (Rule 15a: an absent key is a different lie than an empty list).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout, &raw); err != nil {
		t.Fatalf("decode the raw object: %v", err)
	}
	for _, key := range []string{"program", "state_dir", "workers", "foreign", "residue_count"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("the document omits %q — a consumer cannot tell 'no workers' from 'no such field':\n%s", key, stdout)
		}
	}
	if len(doc.Workers) != 0 {
		t.Fatalf("a seamed-empty process table produced %d worker(s): %+v", len(doc.Workers), doc.Workers)
	}
	if doc.Program != workerProgTopLevel {
		t.Fatalf("program = %q, want %q", doc.Program, workerProgTopLevel)
	}
}

// TestWorkerStatusJSONCLIIsStillRefusedBesideAnAct: `--json` is a MODIFIER of
// the read (workerControlRoleOf → read-only), so pairing it with --stop must be
// refused exactly like the verbose alias — measured precedent:
// `gopi worker --status-verbose --stop` printed the report and exited 0,
// silently dropping the stop (the F1 defect).
func TestWorkerStatusJSONCLIIsStillRefusedBesideAnAct(t *testing.T) {
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	env := append(wsrStatusEnv(t, home), procname.EnvProcRoot+"="+t.TempDir())

	code, stdout, stderr := wsjRun(t, env, bin, workerControlStop)
	combined := string(stdout) + string(stderr)
	if code == 0 {
		t.Fatalf("`%s %s %s %s` exited 0 — a read beside an act must be refused, never a silent no-op:\n%s",
			CliVerbWorker, workerControlStatus, workerStatusJSONFlag, workerControlStop, combined)
	}
	// Nothing was emitted as a document: a refusal is not a report.
	var doc workerStatusJSONDoc
	if err := json.Unmarshal(stdout, &doc); err == nil {
		t.Fatalf("the refused run still printed a status document:\n%s", stdout)
	}
	for _, want := range []string{workerStatusJSONFlag, workerControlStop} {
		if !bytes.Contains([]byte(combined), []byte(want)) {
			t.Fatalf("the refusal must name %q:\n%s", want, combined)
		}
	}
}
