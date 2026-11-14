package sandbox

// run.go — the EXECUTION half of the sandbox: RunIn executes a child INSIDE a
// sandbox.
//
// WHY THIS FILE EXISTS (the defect it closes). A sandbox was, until now, only a
// PATH: `gopi tool sandbox create` printed a directory and nothing bound subsequent
// execution to it. So a child launched by absolute path inherited the CALLER's
// working directory — typically the run's own git worktree — and when the
// launcher was killed, the orphan kept cwd=<worktree> and finish-cleanup's
// holder scan CORRECTLY refused to reclaim that worktree, forever (the guard is
// right: a live process stands in the tree). The producer-side fix is to give
// the child a cwd that is its OWN: the sandbox it runs in.
//
// THE ONE RULE THIS FILE OBEYS: gopi itself NEVER chdir's. The child's
// directory is set per-exec (exec.Cmd.Dir). A launcher that chdir'd would move
// EVERY other thing it does for the rest of its life — the measured anti-pattern
// (`disownProcessCwd` existed only to undo exactly that, and covered one call
// site of many).
//
// It adds NO allocation path of its own: the caller materialises the sandbox
// through the ONE initializer (Create/Ensure) and hands this function the path.
// RunIn is deliberately about EXECUTION only.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nickhuang99/gopi/internal/config"
)

// RunSpec is what to execute inside a sandbox. The zero value is not runnable
// (Argv must name at least one element).
type RunSpec struct {
	// Dir is the sandbox directory the child runs in — the resolved path from
	// the ONE resolver + the ONE initializer, never an invented directory.
	Dir string
	// Argv is the child's argv: Argv[0] is the program (resolved through PATH
	// exactly as a shell would), the rest are its arguments.
	Argv []string
	// Env is the child's complete environment. Nil means os.Environ(), which is
	// the ordinary case: a caller that wants the sandbox profile also exports
	// it (see ProfileEnv).
	Env []string
}

// ExitError reports a child that ran and failed. It carries the child's own
// exit status so the caller can propagate it verbatim — a launcher that
// flattened every child failure into its own generic "1" would make the sandbox
// useless for scripting.
type ExitError struct {
	// ExitCode is the child's exit status (or the shell's 128+N for a signal
	// death — see exitedWithSignal).
	ExitCode int
	// Detail is the one-line human account of HOW it ended.
	Detail string
}

// Error renders the launcher's own message; the caller still exits with
// ExitCode, which is the part a script reads.
func (e *ExitError) Error() string {
	return fmt.Sprintf("sandbox: the child %s (exit status %d)", e.Detail, e.ExitCode)
}

// exitedWithSignal is the exit status convention for a child killed by a signal:
// shells report 128+N for signal N (130 = SIGINT, 137 = SIGKILL). Named because
// a bare 128 in an exit path is exactly the magic number Rule 1a bans.
const exitedWithSignal = 128

// RunIn executes spec.Argv with its working directory set to spec.Dir, waits for
// it, and propagates its result:
//
//   - the child's stdout/stderr/stdin are the launcher's, wired straight
//     through: the child is the foreground job, not a captured subprocess;
//   - a clean child returns nil;
//   - a child that ran and failed returns *ExitError carrying its status;
//   - a CHILD THAT COULD NOT BE STARTED AT ALL returns a plain error naming the
//     path and the cause (there is no status to propagate — reporting "exit -1"
//     would invent one).
//
// The launcher's own working directory is untouched: only the child's is set.
func RunIn(spec RunSpec) error {
	if len(spec.Argv) == 0 {
		return errors.New("sandbox: nothing to run (an empty argv)")
	}
	if strings.TrimSpace(spec.Dir) == "" {
		return errors.New("sandbox: refusing to run a child with no directory to run in")
	}
	info, err := os.Stat(spec.Dir)
	if err != nil {
		return fmt.Errorf("sandbox: the child's working directory %s: %w", spec.Dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sandbox: %s is not a directory — a child cannot run in it", spec.Dir)
	}

	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("sandbox: could not start %q in %s: %w", spec.Argv[0], spec.Dir, err)
	}
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &ExitError{ExitCode: exitStatus(exitErr), Detail: fmt.Sprintf("%q %s", spec.Argv[0], describeWait(exitErr))}
		}
		// Wait itself failed (a broken pipe on the wiring, an I/O error): the
		// child's status is unknown, so none is reported.
		return fmt.Errorf("sandbox: waiting for %q: %w", spec.Argv[0], err)
	}
	return nil
}

// exitStatus reads the child's own status out of a non-zero wait: the numeric
// exit code, or the shell's 128+N when a signal killed it.
func exitStatus(exitErr *exec.ExitError) int {
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return exitedWithSignal + int(ws.Signal())
	}
	return exitErr.ExitCode()
}

// describeWait renders how the child ended, for the launcher's message.
func describeWait(exitErr *exec.ExitError) string {
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("was killed by signal %s", ws.Signal())
	}
	return "exited non-zero"
}

// ProfileEnv returns env with the sandbox's private profile in force: GOPI_HOME
// and GOPI_SETTINGS pointing INTO the sandbox, so the child reads the sandbox's
// own settings COPY (settings_doc.go — never a link) and writes its
// sessions/raw/logs there instead of into the operator's profile.
//
// It is the EXECUTION-side half of the shape Rule 34 describes ("a private
// profile, the standard shape") — the manual recipe wrote exactly these two
// variables by hand, and a hand-written export is what the rule exists to
// replace.
//
// Two properties, both load-bearing:
//
//   - the sandbox PATH WINS over an inherited value: the child of a sandbox
//     must not silently inherit the caller's home just because the launcher ran
//     inside one;
//   - nothing else in the environment is touched, and no new env var is
//     invented — GOPI_HOME and GOPI_SETTINGS are the two names the whole tree
//     already resolves through (config.GopiDirEnv, config.SettingsEnv).
//
// The caller that has no use for a gopi profile inside the child passes nil Env
// and gets the launcher's environment unchanged.
func ProfileEnv(env []string, dir string) []string {
	home := config.GopiDirEnv + "=" + dir
	settings := config.SettingsEnv + "=" + SettingsFileNameIn(dir)
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if strings.HasPrefix(kv, config.GopiDirEnv+"=") || strings.HasPrefix(kv, config.SettingsEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, home, settings)
}

// SettingsFileNameIn is the path of a sandbox's settings DOCUMENT — INSIDE the
// sandbox's profile root (<sandbox>/.gopi/settings.json), which is where
// config.ResolveSettingsPath lands when GOPI_HOME is the sandbox (the profile
// root is <GOPI_HOME>/.gopi, and the default settings path is
// <gopi data root>/settings.json). The entry is a read-only COPY of the
// operator's document, never a link (settings_doc.go). Named so the exec seam
// and the CLI's report cannot disagree about the file the child reads.
func SettingsFileNameIn(dir string) string {
	return filepath.Join(config.ProfileDirIn(dir), config.SandboxSettingsFileName)
}
