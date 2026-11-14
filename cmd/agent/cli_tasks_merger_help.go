package main

// cli_tasks_merger_help.go — the `-h` pages of the git atoms that survive the
// one-shot merge verb's removal (epic merge-verb-removal, item 3).
//
// WHAT CHANGED, AND WHY THIS FILE IS SMALLER (2026-11-01). The canonical
// one-shot face `gopi merge` is GONE: its implementation (merge_run.go and its
// siblings, 8k LOC) was deleted, together with the flag table that page was
// derived from (--max-context-tokens, --max-conflicted-files, --handover-budget,
// --branch, --every, --status, --already-landed, --fast, --no-verify). A page
// that advertised those flags would advertise spellings no parser reads any
// more — the "advertised-but-not-admitted" class this package has been fixed
// for twice. So the canonical page went with the parser that gave it its rows.
//
// WHAT SURVIVES, AND WHY IT IS NOT PART OF THE MERGE VERB: the four atoms
// (`adopt`, `audit-merges`, `merged-cleanup`, `merge-execute`) are separate
// faces over their own engines in internal/tools — each is reachable, each takes
// its own single-instance lock, and NONE of them is the deleted implementation.
// Their shared pointer page survives verbatim.
//
// `gopi task merge` still exists and still MERGES (epic merge-removal, item 2
// removed only the top-level `gopi merge` spelling): it is the thin family face
// over tools.TaskMerge, and it prints its own short page below. The engine and
// its lock are untouched; only the CLI page derived from the deleted parser is
// gone.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// mergerAtomUsageHead is the pointer page of the four atoms that do not MERGE:
// adopt discovers branches, audit-merges only reports, merged-cleanup reaps
// branches/worktrees AND collects dead worktree registrations (it is the
// ownership-independent reap's carrier — see serveWorktreeReapSweep), and
// merge-execute lands ONE ledger row's branch.
//
// They are one page rather than four for the same reason they are one group in
// the vocabulary (taskverbs.MergerVerbs): each is a narrow, once-only face of a
// pass, and none of them is something an operator reads about at length — he
// reads which one he typed, what it does, and where the manual is. Four pages
// would be four copies of that sentence.
//
// The first %s is the spelling being asked about (a taskverbs constant, echoed
// back by taskMergerAtomUsage) so the reader is told which door he opened.
const mergerAtomUsageHead = `gopi task %s — a git atom of the merged-branch chain.

  None of these atoms is a ledger verb: each moves GIT (branches, worktrees,
  merges) and writes the ledger only as a side effect (a conflict row, a reaped
  branch). Every one of them takes the merger's single-instance lock, so two
  writers never touch the same repos.

%s

  The merge pass itself: 'gopi task merge -h'. The task-executing service:
  'gopi worker -h'. The resident merger daemon is RETIRED (2026-09-19):
  'gopi merger' and its deprecated aliases 'gopi task merger' / 'gopi task
  serve' refuse and exit 1.

  -h is answered HERE: without taking the lock, without running anything, exit
  0. A usage query must succeed whatever the state of the world — a live merge
  in another process is a statement about the world, not about your question.
  (Any OTHER argv of these verbs IS a run and does take the lock.)
`

// taskMergeUsageHead is the page `gopi task merge -h` prints: the family
// spelling of the merge engine, and the ONLY merge trigger the binary has left
// (the canonical `gopi merge` face was removed by epic merge-verb-removal).
//
// The page carries the atom's OWN flag set (mergeAtomFlagLines) and nothing
// else: the canonical face's flags died with its parser, so the page cannot
// point a reader at them. The %s slots are: the admitted-flag block, the atoms
// beside it, and the program name the prologue names.
const taskMergeUsageHead = `gopi task merge — merge accepted branches (the git atom).

  The surviving merge trigger: 'gopi merge' was removed (epic
  merge-verb-removal, 2026-11-01) and this family spelling is what remains —
  the same engine (tools.TaskMerge), the same single-instance lock, the same
  counters, under the spelling the git post-receive hook types.

  gopi task merge <branch>...   decide ONLY the named branches, then stop
  gopi task merge --all         decide every local branch except master
  gopi task merge -h | --help   this page: prints, runs nothing, takes no lock
%s

  A BARE 'gopi task merge' is REFUSED (exit 1, "missing required parameter: …")
  and this is a change of answer (task 01a1ac9e, 2026-11-06): it used to take
  the merge lock and sweep EVERY configured repo — on this host that is the
  operator's live working checkout, with a build gate behind it — before it had
  printed which repo it was about to decide. The sweep itself is unchanged and
  still reachable: --repo names the ONE repo it runs against, and the repos
  table remains the merger tick's own sweep list. What the CLI no longer does
  is start a decision nobody asked for.

  A table that is empty or unreadable is an ERROR naming the remedy, never a
  run that reports success having examined no repo. The counters carry repos=<n>
  so the two answers cannot be confused.

  The ceilings, the conflict semantics and the exit codes are the engine's:
  'man gopi-merge' carries the contract.

THE ATOMS BESIDE IT (four of the five do not merge):
%s
`

// mergeAtomFlagLines renders the rows of `gopi task merge`'s OWN flag set — the
// only flags this verb's parser admits — from the constants taskFlagMember's
// case for the verb reads (Rules 1b/13: the page, the admission table and the
// refusal's pointer all come from one list, so the page cannot advertise a word
// the parser refuses and the parser cannot admit a word the page hides).
//
// A spelling that this verb stopped admitting is SKIPPED rather than printed:
// the row set is the intersection of the constants above with the predicate, so
// the two can only drift red, never silently.
func mergeAtomFlagLines() string {
	var b strings.Builder
	for _, row := range []struct{ name, value, what string }{
		{mergeAtomAllFlagName, "",
			"sweep every local branch except the\nintegration branch"},
		{mergeAtomRepoFlagName, mergeAtomPathPlaceholder,
			"act on this repo instead of the cwd (with no\nbranch it must be in the repos table)"},
		{mergeAtomSubmitFlagName, "",
			"route conflicts into the ledger for an\nexecutor agent (default: report only)"},
		{mergeAtomDBFlagName, mergeAtomPathPlaceholder,
			"override the memory db path"},
	} {
		if !taskFlagMember(taskverbs.Merge, row.name) {
			continue
		}
		form := mergeAtomFlagPrefix + row.name + row.value
		merged := strings.ReplaceAll(row.what, "\n", "\n"+strings.Repeat(" ", mergeAtomFlagColumn))
		fmt.Fprintf(&b, "  gopi task merge %s%s%s\n",
			form, strings.Repeat(" ", mergeAtomFlagColumn-len("  gopi task merge ")-len(form)), merged)
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergeAtomFlagColumn is the column the admitted-flag rows' descriptions start
// on: `  gopi task merge ` (19 columns) plus the widest form (`--submit`, 8)
// plus a two-space gutter. Named because the hand-wrapped continuation above
// and the rendered rows must agree exactly (Rule 1b).
const mergeAtomFlagColumn = 33

// mergeAtomPathPlaceholder is what a value-taking row prints where its argument
// goes — `--repo PATH`, never a bare `--repo`. A flag shown without its
// argument reads as a switch, which for these two is the difference between
// "act on this repo" and "act on a repo named --db" (Rule 15 — the page must
// say what to type).
const mergeAtomPathPlaceholder = " PATH"

// mergeAtomFlagPrefix is the argv spelling of a flag NAME (`--repo` for the
// name `repo`): one constant, because the same word is written in every row of
// this file and nowhere else.
const mergeAtomFlagPrefix = "--"

// mergerAtomColumn is the same for the atom list: `    gopi task ` plus the
// longest atom spelling (`merged-cleanup`) plus a gutter.
const mergerAtomColumn = 31

// mergerAtomLinesWithExcept drops one spelling from the shared table — a page
// that names the verb in its own TITLE must not list it again below.
//
// It EXISTS because the merge page needs it (its siblings below the flags)
// while a sibling's own page does not: there its title verb is one of the lines
// and reads as the reader's own row in the list. ONE call site means the
// exception can never silently become dead code (Rule 15: a parameter nobody
// exercises is a claim nobody checks).
func mergerAtomLinesWithExcept(except string) string {
	if except == "" {
		return mergerAtomLines()
	}
	var kept []string
	for _, line := range strings.Split(mergerAtomLines(), "\n") {
		if !strings.HasPrefix(line, "    gopi task "+except+" ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// mergerAtomLines renders one line per git atom: the spelling, what it does,
// and whether it MERGES.
//
// ONE table serves every page, so two pages cannot come to describe the same
// verb differently. Every description is a POINTER: it says what the verb is
// and whether it writes refs, and restates no other page's flag semantics.
func mergerAtomLines() string {
	var b strings.Builder
	for _, row := range []struct{ verb, what string }{
		{taskverbs.Merge, "merge accepted branches into master — MERGES"},
		{taskverbs.MergeExecute, "merge ONE pending merge-backlog row — MERGES"},
		{taskverbs.MergedCleanup, "reap merged branches, worktrees, and dead registrations"},
		{taskverbs.Adopt, "take existing branches into the ledger"},
		{taskverbs.AuditMerges, "report the unmerged-branch backlog"},
		{taskverbs.Merger, "the retired daemon's family alias — refuses"},
	} {
		fmt.Fprintf(&b, "    gopi task %s%s%s\n",
			row.verb, strings.Repeat(" ", mergerAtomColumn-len("    gopi task ")-len(row.verb)), row.what)
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergerAtomUsagePage returns the raw template AND the fmt arguments of a
// merger atom's page. Both halves come from ONE place so the page a user reads
// and the lines a width gate measures are rendered from the same arguments — a
// gate whose arguments drifted from the page's would measure `%!s(MISSING)` as
// text and pass (Rule 15).
//
// A spelling that is NOT a merger atom returns ("", nil), so the caller keeps
// its own fallback: this file must never answer for a verb it does not own.
func mergerAtomUsagePage(verb string) (string, []any) {
	if verb == taskverbs.Merge {
		// Four slots, four values: the admitted-flag block, the sibling atom
		// table, and the atom's own spelling twice (prologue and atoms caption
		// read "gopi task merge" from the ONE constant, mergeFaceProg).
		return taskMergeUsageHead, []any{
			mergeAtomFlagLines(), mergerAtomLinesWithExcept(taskverbs.Merge)}
	}
	for _, v := range taskverbs.MergerVerbs {
		if v == verb {
			return mergerAtomUsageHead, []any{
				verb, mergerAtomLines()}
		}
	}
	return "", nil
}

// taskMergerAtomUsage returns the page `gopi task <verb> -h` prints for a
// merger atom: the surviving merge atom's own flag set for `merge`, and the
// shared pointer page — with the asked-about spelling filled in — for the
// siblings.
func taskMergerAtomUsage(verb string) string {
	page, args := mergerAtomUsagePage(verb)
	if page == "" {
		return ""
	}
	return fmt.Sprintf(page, args...)
}
