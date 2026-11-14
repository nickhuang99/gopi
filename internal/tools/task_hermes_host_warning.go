package tools

// Host-ownership warning for rows whose deliverable.repo names the
// hermes-agent tree (PROJECT.md Rule 40).
//
// WHY THIS EXISTS (measured, 2026-10-03, rows 01a0fe63 → 01a0fe67): a
// hermes-agent task was submitted on the ZBOOK ledger while hermes-agent is
// HP's (host-b) working tree. The repo verification at submit time
// PASSED — because /home/user/work/hermes-agent EXISTS ON ZBOOK TOO (it is
// HP's git upstream, a real work tree, non-bare, on branch `hot-note`) — so
// `git rev-parse --is-inside-work-tree` said "true" on the WRONG host. The
// row sat on zpeer's ledger (`role=server`, `sync_peers=[]` ⇒ nothing
// forwards it) until a human noticed, exported the payload and re-submitted
// it as 01a0fe67 on HP.
//
// The lesson this file encodes: PASSING A REPO CHECK IS NOT EVIDENCE THE ROW
// WAS FILED ON THE RIGHT HOST. Path existence is a fact about a filesystem;
// ownership of the work is a fact about a machine. No comparison of paths can
// tell them apart — both hosts carry the same path — so the only honest
// instrument is a WARNING naming the collision, raised where the submitter
// looks.
//
// IT IS A WARNING, NEVER A REFUSAL. The predicate's own reason: a row that
// names the right repo on the wrong host is still dispatchable and still
// readable (the operator's migration is three steps, not a re-think), while a
// submit that REFUSES would block the legitimate HP-side submit the moment
// hostname detection is wrong (a renamed host, a container, a peer shell) —
// and the submit seam is the funnel every internal auto-submit path also
// passes through. Same shape as the attachment false-claim warning
// (task_attachment_claim.go): report it, never block it.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//   - It does not fire on a repo path that merely MENTIONS hermes in prose;
//     only the repo PATH is inspected.
//   - It does not try to detect "a submit on the wrong host" in general. The
//     table has exactly ONE entry, because exactly one such collision has been
//     measured; inventing a general host-ownership model from one incident
//     would be a guess wearing a rule's clothes (Rule 35: nothing is created
//     casually — a table with one measured entry is the honest size).
//   - It is not a gate: the row submits unchanged.

import (
	"fmt"
	"os"
	"strings"
)

const (
	// taskHermesRepoMarker is the path fragment that identifies a
	// hermes-agent deliverable. A repo path CONTAINING it is the tree this
	// warning is about (a worktree of it — e.g. HP's
	// ~/workspace/gopi-01a0fe67… — is not: the worktree lives on the path
	// the row names, and the marker is the project, not the checkout).
	taskHermesRepoMarker = "hermes-agent"
)

// HermesOwnerHostEnv names the knob that sets hermes-agent's owning host, so
// the Rule 40 route is a DEPLOYMENT fact rather than a welded literal: an
// operator whose fleet differs sets it instead of editing this package.
const HermesOwnerHostEnv = "GOPI_HERMES_OWNER_HOST"

// taskHermesOwnerHostDefault is the MEASURED owner (PROJECT.md Rule 40): the OS
// hostname of that node (`ssh hp hostname`, 2026-10-03). It is the default
// only — taskHermesOwnerHost resolves the knob first, so a second fleet does
// not read this one operator's machine name as a rule about itself.
const taskHermesOwnerHostDefault = "host-b" // the measured owner: see GOPI.md Rule 40

// taskHermesOwnerHost resolves the owning host: the knob, else the measured
// default. One reader, so the predicate and the warning text agree.
func taskHermesOwnerHost() string {
	if v := strings.TrimSpace(os.Getenv(HermesOwnerHostEnv)); v != "" {
		return v
	}
	return taskHermesOwnerHostDefault
}

// TaskSubmitHermesHostWarning is the warning TEMPLATE appended to TaskSubmit's
// confirmation when the row's deliverable.repo names the hermes-agent tree and
// this process is NOT running on the owning host. The %q slots take the repo
// path and this host's name; callers use TaskSubmitHermesHostWarningText,
// never Sprintf directly (Rules 1/13 — one spelling, one renderer).
const TaskSubmitHermesHostWarning = "warning: repo %q belongs to %s (hermes-agent is developed and executed on HP only; this host is %s, its git upstream) — a row filed here will NOT be executed by HP's workers: submit it on HP's ledger (and see GOPI.md Rule 40 for the migration of a row already filed here)"

// TaskSubmitHermesHostWarningText renders the write-face warning. One helper
// owns the Sprintf so the template has a single consumer (Rule 13).
func TaskSubmitHermesHostWarningText(repo, ownerHost, thisHost string) string {
	return fmt.Sprintf(TaskSubmitHermesHostWarning, repo, ownerHost, thisHost)
}

// TaskHermesHostWarning reports the warning for a submit whose deliverable
// repo is `repo`, or "" when there is nothing to say. It reads this process's
// hostname through the OS (the same value the claim/holder stamps use); an
// UNREADABLE hostname produces NO warning — "cannot compare" is not evidence
// of a misroute, and a warning nobody can act on is noise.
func TaskHermesHostWarning(repo string) string {
	host, err := os.Hostname()
	if err != nil {
		return ""
	}
	return taskHermesHostWarningFor(repo, host)
}

// taskHermesHostWarningFor is the pure half: the whole decision, with the host
// as a parameter, so the predicate is testable without a real hostname and the
// exported wrapper stays a one-line reader.
func taskHermesHostWarningFor(repo, host string) string {
	return taskHermesHostWarningForOwner(repo, host, taskHermesOwnerHost())
}

// taskHermesHostWarningForOwner is the decision with the OWNER also as a
// parameter, so a test can prove the knob changes the answer.
func taskHermesHostWarningForOwner(repo, host, owner string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" || !strings.Contains(repo, taskHermesRepoMarker) {
		return ""
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, owner) {
		return ""
	}
	return TaskSubmitHermesHostWarningText(repo, owner, host)
}
