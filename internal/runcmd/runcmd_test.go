package runcmd

import (
	"strings"
	"testing"
)

// TestCommand pins the ONE task-show run_cmd renderer: argv[0] reduced to
// its basename (a portable command), every element shell-quoted exactly
// when it needs to be, joined with single spaces.
func TestCommand(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"bare verb", []string{"gopi", "task", "run", "t-1"}, "gopi task run t-1"},
		{"absolute binary reduced to basename", []string{"/usr/local/bin/gopi", "worker"}, "gopi worker"},
		{"empty argv", nil, ""},
		{
			"spaced argument is quoted so the paste survives",
			[]string{"gopi", "task", "submit", "fix the thing"},
			"gopi task submit 'fix the thing'",
		},
		{
			"embedded single quote uses the POSIX escape",
			[]string{"gopi", "task", "run", "it's"},
			`gopi task run 'it'\''s'`,
		},
		{"empty element stays visible", []string{"gopi", ""}, "gopi ''"},
		{
			"a non-default settings path round-trips quoted",
			[]string{"gopi", "--settings", "/home/nick/.gopi/.do settings.json", "task", "run", "t-9"},
			"gopi --settings '/home/nick/.gopi/.do settings.json' task run t-9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Command(tc.argv); got != tc.want {
				t.Fatalf("Command(%q) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

// TestField: the renderer emits a padded "label:  value" line, keeps a
// value verbatim (a run_cmd must be copy-pasteable), and prints NOTHING
// for an empty value — an absent fact never becomes a fictitious line.
func TestField(t *testing.T) {
	// The task-show block's labels align on a 16-column gutter (the width
	// internal/tools renders the run provenance at).
	if got, want := Field(FieldRunMethod, MethodManual, 16), "run_method:     manual\n"; got != want {
		t.Fatalf("Field(run_method) = %q, want %q", got, want)
	}
	if got, want := Field(FieldRunCmd, "gopi task run t-1", 16), "run_cmd:        gopi task run t-1\n"; got != want {
		t.Fatalf("Field(run_cmd) = %q, want %q", got, want)
	}
	if got := Field(FieldRunCmd, "", 16); got != "" {
		t.Fatalf("Field with empty value = %q, want the empty string", got)
	}
}

// TestLauncherAndSelfPath: the command word comes from the launcher's own
// published binary when there is one, else from this process's executable,
// else the conventional name.
func TestLauncherAndSelfPath(t *testing.T) {
	t.Setenv(EnvBinary, "")
	t.Setenv(EnvSelf, "")
	if got := Launcher(); got != fallbackGopiName {
		// The fallback only applies when the OS cannot report an executable;
		// on a normal platform the running test binary is the answer.
		if SelfPath() == "" {
			t.Fatalf("Launcher() = %q, want %q", got, fallbackGopiName)
		}
		if got != SelfPath() {
			t.Fatalf("Launcher() = %q, want this process's executable %q", got, SelfPath())
		}
	}
	t.Setenv(EnvBinary, "/opt/gopi/bin/gopi-real")
	if got := Launcher(); got != "/opt/gopi/bin/gopi-real" {
		t.Fatalf("Launcher() with %s = %q", EnvBinary, got)
	}
	t.Setenv(EnvBinary, "")
	t.Setenv(EnvSelf, "/opt/gopi/bin/gopi-self")
	if got := Launcher(); got != "/opt/gopi/bin/gopi-self" {
		t.Fatalf("Launcher() with %s = %q", EnvSelf, got)
	}
	// The renderer never leaves an absolute path at argv[0].
	t.Setenv(EnvSelf, "/opt/gopi/bin/gopi-self")
	if got := Command([]string{Launcher(), "worker"}); got != "gopi-self worker" {
		t.Fatalf("Command(launcher argv) = %q, want the basename form", got)
	}
}

// TestResumeArgvRendersTheLiveSpelling: a resume command line must be
// RE-RUNNABLE. verb-retirement phase 1 (2026-10-06) moved the resume verb under
// the `session` group and refused the bare word by name, so the composed argv
// and the recorded run_cmd both had to carry the group word — before that test,
// three call sites typed the bare word independently and a spawned child exited
// 1 with a refusal instead of resuming (measured: the cmd/agent suite went red on
// TestResume*/TestWorkerResume* the moment the retirement landed).
//
// Epic resume-surface convergence step 2 (2026-10-18) moves it ONCE MORE, to the
// no-verb face `gopi --session <target>`: the resume has one implementation with
// two addresses (step 1), a MACHINE should spawn the bare one, and the recorded
// command must be the one a reader can paste back. The rendering is asserted
// here, and the EXECUTED half is pinned where it can be executed
// (cmd/agent/cli_worker_resume_argv_exec_test.go drives the real binary).
//
// The assertion is on the RENDERED string (what a reader pastes into a shell),
// not on the argv slice, because that string is the field whose whole purpose is
// to re-run.
func TestResumeArgvRendersTheLiveSpelling(t *testing.T) {
	t.Setenv(EnvBinary, "gopi")
	t.Setenv(EnvSelf, "")

	// A raw-log PATH is a legal target (both resume addresses resolve one), and
	// the row linkage rides --task — the two halves a spawned recovery child
	// needs to (a) find the checkpoint and (b) close the row out.
	if got, want := Command(ResumeArgv("/tmp/raw.log", "t-1")),
		"gopi --session /tmp/raw.log --task t-1"; got != want {
		t.Fatalf("ResumeArgv(target, task) renders %q, want %q", got, want)
	}
	// A resume with no row linkage is a legal shape (a plain session resume):
	// the `--task` half must VANISH, never render as an empty operand.
	if got, want := Command(ResumeArgv("sess-9", "")), "gopi --session sess-9"; got != want {
		t.Fatalf("ResumeArgv(target, \"\") renders %q, want %q — an omitted row must not leave a dangling flag", got, want)
	}
	// The group-verb spelling is NOT dead (its retirement is step 3, and until
	// then the old face must keep working) — but this renderer must not emit it:
	// the spawned child and the recorded command follow the CONVERGED address.
	if got := Command(ResumeArgv("sess-9", "t-1")); strings.Contains(got, VerbSession+" "+VerbResume) {
		t.Fatalf("ResumeArgv renders the not-yet-retired group spelling %q — the converged address is the no-verb one", got)
	}
	// And the bare retired verb must never come back through this renderer.
	if got := Command(ResumeArgv("sess-9", "")); strings.HasPrefix(got, "gopi "+VerbResume+" ") {
		t.Fatalf("ResumeArgv renders the RETIRED top-level spelling %q — the child would be refused", got)
	}
}
