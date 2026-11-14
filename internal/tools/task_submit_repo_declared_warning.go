package tools

// Repo-DECLARED warning for submits whose deliverable.repo is a live git work
// tree that the settings `repos` table does not declare.
//
// WHY THIS EXISTS (measured, row 01a159a9-ecb8-7664-8beb-57438ac4023f, and the
// rows it repeats): /home/user/work/GoPi (uppercase, a checkout frozen at
// 2026-10-18) and /home/user/work/gopi (lowercase, master) both exist on this
// machine. The settings `repos` table declares a build command for the
// LOWERCASE path only. `task submit` WITHOUT --repo stamps the process CWD
// (taskSubmitResolveDefaultRepo), so a submit typed while standing in the
// uppercase checkout records a path that then sails through the ONE repo
// check the submit seam performs — `git rev-parse --is-inside-work-tree`
// (taskRepoVerifiable) — because it really IS a git work tree. The row queues,
// and dispatch refuses it later and terminally:
//
//	clone channel: repo "/home/user/work/GoPi" declares no build command in
//	settings repos → /home/user/work/GoPi.build — refusing to push a workspace
//	that was never built
//
// TWO CHECKS, TWO DIFFERENT QUESTIONS, NEVER CROSSED: "is this a work tree"
// (git) and "does this repository declare a build contract" (settings). Both
// are true statements about different facts, so passing the first says nothing
// about the second — exactly the shape PROJECT.md Rule 40 records for the
// hermes host collision, and the reason this file is modelled on
// task_hermes_host_warning.go.
//
// REPAIR IS NOT THIS FILE'S JOB. RepairDurableRepoForSubmit recovers a DEAD
// workspace path from the ledger; a path that is a live work tree is IDENTITY
// there (it returns the same string, no note), so the uppercase checkout never
// reaches any repair arm. Nothing about the path is wrong — the DECLARATION is
// missing — and the cheapest honest place to say so is where the submitter is
// still looking at the command they just typed.
//
// IT IS A WARNING, NEVER A REFUSAL, for the same measured reason the hermes
// warning is: a row naming an undeclared repo is still dispatchable whenever
// the clone channel is OFF (`dev_repo` empty ⇒ the worktree channel, which
// never asks for a build command), and the submit seam is the funnel every
// internal auto-submit path also passes through (branch-scan merge-backlog,
// push notify, conflict dispatch, adoption). A refusal here would block rows
// whose run would have worked.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//   - It does not fire when the settings table cannot be READ. "I could not
//     look" is not evidence of an undeclared repo, and a warning nobody can
//     act on is noise (the same direction task_hermes_host_warning.go takes
//     for an unreadable hostname).
//   - It does not care whether the path EXISTS. internal/devworkspace already
//     answers that question with its own measured sentence
//     (RepoRefusalDeadPathFmt), and duplicating it here would give one fact
//     two spellings.
//   - It is not a gate: the row submits unchanged, with the same
//     deliverable.repo stamp.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/config"
)

// TaskSubmitRepoUndeclaredWarningFmt is the ONE spelling of "this repo is not
// in the settings repos table" (Rules 1/13 — the template has a single
// definition site and a single renderer). The %q slots take, in order: the
// repo path, the settings table key, the comma-joined DECLARED repo list, the
// repo path again, the table key again, and the channel sentence.
const TaskSubmitRepoUndeclaredWarningFmt = "warning: repo %q is not a key of the settings %q table, so no build command is declared for it — the git check above passed only because the path IS a work tree, which says nothing about its build contract. %s Declared repos: %s. Declare this path's %q entry, or submit against a repo that is already declared (see CONFIG.md)."

// TaskSubmitRepoUndeclaredChannelRefuses is the channel sentence when the
// CLONE CHANNEL IS ON: the exact refusal this row will meet at dispatch,
// quoted from resolveCloneChannelBuild so the submitter can grep for it.
const TaskSubmitRepoUndeclaredChannelRefuses = "dev_repo is set, so this row's run takes the clone channel, which refuses it: clone channel: repo %q declares no build command in settings %s → %q.build — refusing to push a workspace that was never built."

// TaskSubmitRepoUndeclaredChannelWaits is the channel sentence when the clone
// channel is OFF: today's run would work (the worktree channel asks for no
// build contract), and the warning says so rather than crying wolf.
const TaskSubmitRepoUndeclaredChannelWaits = "dev_repo is empty, so the worktree channel is in force today and this row would still run — the refusal arrives the day the clone channel is switched on."

// taskSubmitRepoUndeclaredSep joins the declared repo list. A named separator
// keeps the render in one place (Rule 1) and matches the ledger's own
// comma-joined tag spelling.
const taskSubmitRepoUndeclaredSep = ", "

// taskSubmitRepoUndeclaredNone stands in for an EMPTY declared table: the
// submitter must be able to tell "nothing is declared" from "you picked one of
// the declared repos".
const taskSubmitRepoUndeclaredNone = "(none — the repos table is empty)"

// TaskSubmitRepoUndeclaredWarning reports the warning for a submit whose
// deliverable repo is `repo`, or "" when there is nothing to say. It reads the
// settings table and the clone-channel switch through their ONE readers
// (config.ResolveMergerRepos / config.CloneChannelConfigured) so the warning
// can never disagree with the machinery that will refuse the row.
func TaskSubmitRepoUndeclaredWarning(repo string) string {
	declared, err := config.ResolveMergerRepos()
	if err != nil {
		// Unreadable settings: no judgement, no warning (see the header's
		// "what it deliberately does not do").
		return ""
	}
	on, _, err := config.CloneChannelConfigured()
	if err != nil {
		// Same direction: a settings file that cannot be read is not evidence
		// that the clone channel is on.
		on = false
	}
	return taskSubmitRepoUndeclaredWarningFor(repo, declared, on)
}

// taskSubmitRepoUndeclaredWarningFor is the pure half: the whole decision,
// with the table and the channel as parameters, so the predicate is testable
// without touching the operator's settings file.
func taskSubmitRepoUndeclaredWarningFor(repo string, declared []string, cloneChannelOn bool) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	for _, d := range declared {
		if strings.TrimSpace(d) == "" {
			continue
		}
		// SameDir, not string equality alone: an operator's spelling (a
		// trailing slash, a symlinked home, a `./` prefix) must not be reported
		// as an undeclared repository. SameDir answers "same directory" only
		// when BOTH sides exist, so the byte-identical spelling is checked
		// first: a declaration and a stamp that are the same string IS the
		// working case, whether or not anything is on disk.
		if d == repo || SameDir(d, repo) {
			return ""
		}
	}
	channel := fmt.Sprintf(TaskSubmitRepoUndeclaredChannelWaits)
	if cloneChannelOn {
		channel = fmt.Sprintf(TaskSubmitRepoUndeclaredChannelRefuses,
			repo, config.ReposSettingsKey, repo)
	}
	list := taskSubmitRepoUndeclaredNone
	if len(declared) > 0 {
		list = strings.Join(declared, taskSubmitRepoUndeclaredSep)
	}
	return fmt.Sprintf(TaskSubmitRepoUndeclaredWarningFmt,
		repo, config.ReposSettingsKey, channel, list, config.ReposSettingsKey)
}
