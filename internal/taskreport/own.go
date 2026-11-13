package taskreport

// own.go — the REPORT-OWNERSHIP side of the seam: the value a gopi process
// exports about ITSELF, and the ONE predicate that decides whether a report
// destination may be used.
//
// The defect this closes (measured 2026-09-15, session 01a0a4ee): PathEnv is a
// PER-SESSION delivery contract — the exporter hands a process the path of the
// report IT owns — but the environment travels DOWN the whole process tree. A
// gopi spawned by an agent session (a self-probe, a nested `go test`, a worker
// child) inherited the PARENT's PathEnv, read it as its own destination, and
// wrote its own report over the parent's task-report.md — the parent's
// deliverable destroyed by a child that had no business writing there. The child
// did nothing wrong: the variable is indistinguishable from "yours" unless the
// process can say whose it is.
//
// THE RULE IS THE DIRECTORY, NOT THE ID. A session's report lives INSIDE that
// session's own directory, so the honest question is "does this destination
// live in MY directory" — asked against the session directory THIS process
// knows it owns. The id-based rule was WRONG and measurably so: the harness
// injects PathEnv with NO owner marker and its destination is
// <sessions>/<session-uuid>/task-report.md — neither the task id nor the run id
// of anything — so a rule that demanded a matching id refused the harness
// hand-off, i.e. it broke the very contract it protects (measured on this
// task's own session: GOPI_TASK_REPORT_PATH pointed at the session uuid's dir
// while GOPI_TASK_RUN_ID carried the task id).
//
// Both exporters are covered by the directory test:
//
//	`task run`   <gopi dir>/sessions/<task id>/task-report.md
//	harness      <gopi dir>/sessions/<session uuid>/task-report.md
//
// The owner marker is a SECOND, narrower refusal: when it is present and names
// a different session, the contract is refused whatever the directory says.
// An empty marker is not a claim — it is simply the marker-less hand-off.
//
// Rule 14: the values AND the predicate live here, in the leaf package that
// already owns the report contract, so the writer (the spawn seams), the
// readers (internal/agent's startup guard, internal/tools' shell env sanitizer)
// and the tests reference ONE definition — never a mirrored copy or a bare
// literal (Rule 1b/13).

import (
	"os"
	"path/filepath"
	"strings"
)

// OwnEnv is the environment variable carrying the session/task DIR NAME whose
// report PathEnv names — the marker that makes an exported report path
// self-identifying. `gopi task run` exports it beside PathEnv (one writer,
// Rule 2); a harness that exports no marker is covered by the directory rule.
const OwnEnv = "GOPI_TASK_REPORT_OWNER"

// OwnedBy reports whether a report destination belongs to the session whose
// own directory is sessionDir:
//
//	dir   the destination lives in THIS session's directory (the general rule);
//	marker no owner marker names a DIFFERENT session.
//
// Both paths are resolved before comparison (symlinks, relative forms) — an
// equivalent spelling of the same directory IS the same directory, and a
// textual comparison would refuse a legitimate hand-off by accident.
//
// An empty sessionDir means "this process cannot name a directory of its own",
// so nothing is provably its own; an empty owner marker means "no claim was
// made", which is not a claim of ownership.
func OwnedBy(reportPath, ownerMarker, sessionDir string) bool {
	reportPath = strings.TrimSpace(reportPath)
	ownerMarker = strings.TrimSpace(ownerMarker)
	sessionDir = strings.TrimSpace(sessionDir)
	if sessionDir == "" {
		return false
	}
	if ownerMarker != "" && !sameName(ownerMarker, filepath.Base(sessionDir)) {
		return false
	}
	if reportPath == "" {
		// A marker with no destination names nothing this session could write.
		return false
	}
	return SameDir(filepath.Dir(reportPath), sessionDir)
}

// sameName compares two dir names exactly (after trimming). Kept separate from
// SameDir: a marker is a NAME, never a path.
func sameName(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

// SameDir reports whether two directory paths denote the same directory,
// resolving both (symlinks + relative forms) and comparing case-sensitively
// (Linux). An unresolvable path falls back to its cleaned absolute form, so two
// spellings of one path still compare equal inside a sandbox whose parents
// differ.
func SameDir(a, b string) bool { return resolveDir(a) == resolveDir(b) }

// resolveDir returns the canonical absolute form of dir ("" for "").
func resolveDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(dir)
}

// OwnerMatches reports whether an owner marker names the session/task ownID —
// the narrow id-based check, kept for the marker's own comparison sites (a
// marker against a task id). The directory rule above is the general one; this
// helper exists so no caller re-implements it with a bare equality.
func OwnerMatches(owner, ownID string) bool {
	owner = strings.TrimSpace(owner)
	return owner != "" && owner == strings.TrimSpace(ownID)
}

// ContractPresent reports whether env carries any part of the report contract
// (the path or the marker, present even with an empty value). An empty value
// still counts as PRESENT: a launcher exported the name, which is the fact the
// ownership decision is about (Rule 15 — a present-but-empty contract is not
// "nothing to see").
func ContractPresent(env []string) bool {
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == PathEnv || name == OwnEnv {
			return true
		}
	}
	return false
}

// EnvValue returns the value of name in env ("" when absent or valueless). On a
// duplicate name this scan yields the LAST entry — that is THIS FUNCTION's
// contract, and it says nothing about how a child's own loader reads a
// duplicated name. Which copy a reader takes is order- AND reader-dependent:
// tools.ChildLocaleDuplicateKeyRule (internal/tools/git_child_locale.go) is the
// ONE statement of it, and the measured reading lives there.
func EnvValue(env []string, name string) string {
	val := ""
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == name {
			val = v
		}
	}
	return val
}

// EnvName returns the NAME half of a "NAME=value" entry.
func EnvName(kv string) string {
	name, _, _ := strings.Cut(kv, "=")
	return name
}

// osEnviron is the process environment (a named indirection so this file owns
// no bare call site a test cannot reason about).
func osEnviron() []string { return os.Environ() }

// ProcessOwnsContract reports whether the REPORT CONTRACT currently in this
// process's environment is this process's own, judged against sessionDir (the
// directory this process knows it owns; "" when it has none yet). It is the
// whole ownership decision in one call, so the startup guard and the shell env
// sanitizer cannot drift apart.
func ProcessOwnsContract(sessionDir string) bool {
	env := osEnviron()
	if !ContractPresent(env) {
		return true // nothing inherited: nothing to disown
	}
	return OwnedBy(EnvValue(env, PathEnv), EnvValue(env, OwnEnv), sessionDir)
}
