// task_child_fail_guard.go — the SANDBOX PRECONDITION on the hermetic test
// escapes of the task-run funnel.
//
// ROOT CAUSE THIS FILE EXISTS FOR (incident 2026-09-16, the 5 burned rows):
// taskChildFailTestEnv ("GOPI_TASK_CHILD_FAIL_TEST=1", cli_tasks.go) was
// compared straight against os.Getenv with ZERO precondition. The variable was
// the whole authority: ANY process holding it — and every process the holder
// spawns, since env is inherited — writes back every claimed ledger row as
// `failed`, with a synthetic error and elapsed=0s. A resident `gopi worker`
// inherited the variable from its launcher shell, claimed 5 REAL operator rows
// and terminally burned them in 6 seconds (claimed_at 15:57:26–15:58:11,
// claimed_by peer1:558874, every writeback detail "synthetic child failure (test
// escape)"). Nothing was attempted and nothing was judged — a test seam was
// remotely deciding production task outcomes.
//
// THE FIX (this file): the escape is bound to its SANDBOX. The resolved ledger
// (or the resolved gopi data root) must be INSIDE a MEASURED temp root —
// TMPDIR's effective value, or the scratch home — and that root must be off the
// `/` partition. Nothing here is spelled: the 2026-09-16 shape was
// `mktemp -d`, which is /tmp, and /tmp IS on `/` on this host (Rule 22).
//
// WHY "INSIDE A TEMP ROOT" AND NOT MERELY "OFF /" (measured 2026-09-16 on this
// host — the naive predicate is a false negative): `/` is device 2051 while
// /home is device **2052**, and the operator's real profile `~/.gopi` lives on
// /home. A device test alone therefore calls the VERY ledger the 5 rows were
// burned into a "sandbox". The temp-root CONTAINMENT test is what separates
// `t.TempDir()` from `~/.gopi`: a throwaway sandbox is a child of the temp
// root, an operator profile is not.
//
// Outside a sandbox the escape is REFUSED LOUDLY (Rule 15: never a silent pass,
// never a silently-ignored variable) and the process stops with a distinct exit
// code instead of failing rows. Inside a sandbox (a t.TempDir, a throwaway
// GOPI_HOME under TMPDIR) it behaves exactly as before — which is what keeps
// every hermetic test green, TestRunTaskServe_SurvivesFailedRun included.
//
// WHY THE REFUSAL STOPS THE PROCESS RATHER THAN "ignoring the escape": an
// operator who exported the variable deliberately wants the synthetic child; an
// operator who inherited it wants real work. Continuing on the REAL path would
// be the dangerous default (real task execution decided by an env var the
// operator forgot), so the refusal stops the process BEFORE it claims anything.
// No row changes status — the worker-loop gate runs at the loop entry, the
// funnel gate at the top of runTaskRunOnce.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// taskChildFailRefusalExitCode is the distinct exit code of the refusal, so
	// a launcher (or an operator reading `$?`) can tell "the escape was refused
	// for lack of a sandbox" from every other worker/task failure. 96 is unused
	// by the sibling refusals (workerUnexpectedArgExitCode; the suite's
	// SuiteScratchUnsafeExitCode = 97). A named constant, never a literal
	// (Rule 1a).
	taskChildFailRefusalExitCode = 96

	// taskChildFailVerbWorker is the worker face's label in the refusal text —
	// the CLI verb constant itself (Rule 1b/13: no second spelling of "worker").
	taskChildFailVerbWorker = CliVerbWorker
	// taskChildFailVerbRun labels the task-run face. It is taskProgName — the
	// same program label the child-spawn path prints (Rule 13: one spelling).
	taskChildFailVerbRun = taskProgName

	// taskChildFailRefusalFmt is the refusal itself — ONE spelling (Rule 1b),
	// because the worker loop, the funnel and the guard's own tests must read
	// the same sentence. Args: the measured path, the device of `/`, the temp
	// roots that were probed, the effective temp root, GOPI_HOME's name, the
	// memory-db variable's name, the escape's own name.
	taskChildFailRefusalFmt = "REFUSED: %s=1 is armed but the resolved root %q is NOT inside a sandbox " +
		"(measured against the %s device, not spelled: a temp root on `%s` such as %s is not a sandbox). " +
		"This variable makes EVERY claimed ledger row fail with a synthetic error and elapsed=0s, with no " +
		"attempt and no judgement — on 2026-09-16 a resident worker inherited it from its launcher shell and " +
		"terminally burned 5 real operator rows in 6 seconds. Refusing instead: no row has been claimed or " +
		"changed. FIX: run the escape against a throwaway profile UNDER the temp root — %s=<a dir under %s> " +
		"with %s=<a ledger inside it> (in tests: t.Setenv both) — or unset %s to run the real path."

	// taskChildFailNoRootsMsg is the refusal when NEITHER the ledger nor the
	// gopi data root resolves to a path at all: an unresolvable root is not a
	// sandbox (Rule 15 — "cannot tell" is never "safe"). Args: GOPI_HOME's name,
	// the memory-db variable's name.
	taskChildFailNoRootsMsg = "neither %s nor %s resolves to a path — cannot prove a sandbox root"
)

// taskChildFailEscapeArmed reports whether the hermetic child-failure escape is
// armed — the ONE comparison of the variable (Rule 2), the same shape the
// funnel's own branch uses.
func taskChildFailEscapeArmed() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(taskChildFailTestEnv)), "1")
}

// taskChildFailRefusal renders the refusal for a measured non-sandbox path.
func taskChildFailRefusal(path string) string {
	roots := describeTempRootsForMessage(taskChildFailTempRoots())
	return fmt.Sprintf(taskChildFailRefusalFmt,
		taskChildFailTestEnv, path, scratch.RootDeviceProbePath, scratch.RootDeviceProbePath,
		roots, config.GopiDirEnv, roots,
		config.MemoryDBEnv, taskChildFailTestEnv)
}

// describeTempRootsForMessage renders the probed temp roots for the refusal.
// An unmeasurable temp root is reported by NAME, never as an empty path — the
// message must not claim a path nobody could measure (Rule 15).
func describeTempRootsForMessage(roots []string) string {
	if len(roots) == 0 {
		return scratch.TmpDirEnvName
	}
	return strings.Join(roots, " or ")
}

// taskChildFailTempRoots are the MEASURED temp roots a sandbox may live in:
//
//   - the effective temp root (an explicit TMPDIR, else os.TempDir() — the same
//     root every t.TempDir() and every scratch.MkdirTemp lands in), and
//   - the scratch home (scratch.HomeDir(), the tree the whole suite system
//     keeps its throwaway runs in).
//
// Both are measured, never spelled (Rule 22). The list is deliberately small:
// a sandbox must be a child of a root whose PURPOSE is throwaway data.
func taskChildFailTempRoots() []string {
	roots := make([]string, 0, 2)
	add := func(p string) {
		if strings.TrimSpace(p) == "" {
			return
		}
		p = scratch.CanonicalPath(p)
		for _, seen := range roots {
			if seen == p {
				return
			}
		}
		roots = append(roots, p)
	}
	if effective, _, err := scratch.EffectiveTempRoot(); err == nil {
		add(effective)
	}
	add(scratch.HomeDir())
	return roots
}

// realProfilePaths are the operator's LIVE profile root and ledger — the two
// paths a sandbox test must never be confused with.
//
// They are computed from the OS home ALONE (config.GopiDir is deliberately not
// used: it honours GOPI_HOME, so it would answer "where does this process point",
// not "where does the operator's real profile live"). The comparison is a
// containment test, so anything INSIDE the live profile is refused too — a
// sandbox is never inside the operator's profile.
func realProfilePaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	root := filepath.Join(scratch.CanonicalPath(home), config.ProfileDirName)
	return []string{root, filepath.Join(root, config.MemoryDBFileName)}
}

// pathInAnyRoot reports whether path lives inside one of roots (component-wise
// containment via scratch.PathWithin — a prefix STRING match would call
// "/scratch-other" a child of "/scratch").
func pathInAnyRoot(path string, roots []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = scratch.CanonicalPath(abs)
	for _, root := range roots {
		if scratch.PathWithin(abs, root) {
			return true
		}
	}
	return false
}

// taskChildFailSandboxRoot reports the FIRST sandbox root among the paths that
// decide where this process's task data lands, or ("", error) when NONE is a
// sandbox.
//
// Both halves are needed, because either one alone relocates the run:
//
//   - the LEDGER (config.LedgerPath — GOPI_MEMORY_DB > GOPI_HIPPO_DB >
//     <gopi dir>/memory.db) is where a failed row would be written; and
//   - the GOPI DATA ROOT (config.GopiDir — $GOPI_HOME/.gopi) is where sessions,
//     raw logs and the task's own session directory land.
//
// A path is a sandbox only when BOTH measurements say so: it lives INSIDE one
// of the measured temp roots (taskChildFailTempRoots), and the measurement
// scratch.IsTempRootOffRootFS puts it off the `/` partition. It must also not
// live inside the operator's real profile (realProfilePaths) — belt and braces
// for a host whose TMPDIR points at $HOME.
//
// A measurement that cannot be made aborts the scan with its error, so the
// caller refuses loudly rather than proceeding on an unproven root (Rule 15).
func taskChildFailSandboxRoot() (string, error) {
	candidates := make([]string, 0, 2)
	if ledger := config.LedgerPath(); ledger != "" {
		candidates = append(candidates, ledger)
	}
	if gd, err := config.GopiDir(); err == nil && gd != "" {
		candidates = append(candidates, gd)
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf(taskChildFailNoRootsMsg, config.GopiDirEnv, config.MemoryDBEnv)
	}
	tempRoots := taskChildFailTempRoots()
	live := realProfilePaths()
	notSandbox := ""
	for _, path := range candidates {
		if pathInAnyRoot(path, live) {
			if notSandbox == "" {
				notSandbox = path
			}
			continue
		}
		if !pathInAnyRoot(path, tempRoots) {
			if notSandbox == "" {
				notSandbox = path
			}
			continue
		}
		offRoot, err := scratch.IsTempRootOffRootFS(path)
		if err != nil {
			return "", fmt.Errorf("measuring %q: %w", path, err)
		}
		if offRoot {
			return path, nil
		}
		if notSandbox == "" {
			notSandbox = path
		}
	}
	return "", fmt.Errorf("%s", taskChildFailRefusal(notSandbox))
}

// refuseIfUnsafeTaskChildFailEscape is the gate BOTH faces share: it returns
// nil when the escape is not armed, or when a sandbox root was proven — else it
// renders the refusal on stderr AND returns it as an error (never a bare print,
// Rule 15: the caller's own error path must carry it too).
//
// It is deliberately the error-returning half: the funnel (runTaskRunOnce) has
// many callers (`task run`, `task run next`, resume, the worker round) and only
// some of them may end a process, so it propagates while the worker-loop half
// exits.
func refuseIfUnsafeTaskChildFailEscape(verb string) error {
	if !taskChildFailEscapeArmed() {
		return nil
	}
	_, err := taskChildFailSandboxRoot()
	if err == nil {
		return nil
	}
	refusal := fmt.Sprintf("%s: %s: %v", verb, taskChildFailTestEnv, err)
	fmt.Fprintf(os.Stderr, "%s\n", refusal)
	return errors.New(refusal)
}

// assertTaskChildFailEscapeSandboxed is the WORKER-loop gate: the loop must not
// run a process that would write back real rows as `failed`. It EXITS (with
// taskChildFailRefusalExitCode) after putting the refusal on stderr, BEFORE any
// claim — so no row's status can change. That ordering is the whole point: the
// 2026-09-16 incident changed 5 rows, and a gate placed after the first claim
// would still change one.
func assertTaskChildFailEscapeSandboxed(verb string) {
	if err := refuseIfUnsafeTaskChildFailEscape(verb); err != nil {
		fmt.Fprintf(os.Stderr, "%s: refusing to start (exit %d)\n", verb, taskChildFailRefusalExitCode)
		os.Exit(taskChildFailRefusalExitCode)
	}
}
