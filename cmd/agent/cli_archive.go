// cli_archive.go — `gopi task archive`: the ONE supported way to put a branch into
// the decided-exclusion namespace (refs/tags/archive/<branch>) from a shell.
//
// WHY THIS VERB EXISTS (epic hand-archive-gate, 2026-09-16): at 21:04 nine
// branches in /home/nick/work/GoPi were archived by hand — `git tag
// archive/<branch> <branch>`, nine loose refs written inside one second, five
// of them over task rows the ledger calls `completed` — with no ledger row
// and no reason recorded anywhere. The tag is the project's decided-exclusion
// namespace and five readers honour it (repoSweepArchiveExempt, taskMergeExempt,
// task_deliverable_branch.go, task_merged_cleanup.go, scripts/safe-merge.sh),
// so the burial was EFFECTIVE and invisible at once.
//
// The verb makes the two things that were missing mandatory:
//
//	--reason <code>   the WHY, from a fixed vocabulary (tools.ArchiveReasons)
//	--override-completed  the explicit acknowledgement when the owning task
//	                      row reached completed/verified
//
// and it reads the decision back, so the operator sees the record they just
// made rather than trusting that a tag appeared.
//
// Shape follows cli_shot.go (isXSubcommand predicate + hand-rolled flag parse
// in a testable parse function; os.Exit only in the runner), so the parser can
// be asserted without a process.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// archiveUsage is the -h text for the archive verb. It is a function (not a
// const) because it INTERPOLATES the reason vocabulary — a const would freeze
// the help text against tools.ArchiveReasons (Rule 13).
func archiveUsage() string {
	return `gopi task archive — archive a branch WITH a recorded reason (the decided-exclusion lane)

Archiving marks a branch a DECIDED LOSER: it is written as the tag
refs/tags/archive/<branch> — a LIGHTWEIGHT ref on the branch tip, exactly the
shape every archive lane has always written — which every merge path in gopi
honours as "already decided — never merge this". The ref name and the ref's
shape are unchanged; what this verb adds is a WHY, recorded in the companion
ref refs/gopi/archive-reason/<branch>. The archive ref is never rewritten.

A branch whose owning task row is completed/verified is REFUSED unless you name
a reason AND acknowledge the override. (A failed/cancelled row proceeds; a
branch no row tracks proceeds, and its reason is FORCED to --reason unowned —
the record then says the fact "nothing tracked this branch" instead of carrying
a story the caller typed. So a caller passing --reason superseded for an unowned
branch reads "reason: unowned" back.)

Reclaiming: archiving a branch is a DECISION, so the verb also carries the
decision through — refs/heads/<branch> is removed while the archive tag stays.
That removal is the step every other lane was missing: the merger's candidate
scan, the close-out exemption and this writer all SKIP an archived branch, so
without it the decided ref survives every tick forever. It refuses (and reports)
when the tag and the branch ref name DIFFERENT commits, and it never touches a
branch a worktree is holding, master/main.

A task/<id> branch is NOT refused for its name: it is refused only when a FACT
says the ground is still occupied — a registered worktree holds it (the refusal
names the worktree path), or a ledger row still tracks it and that row is not
terminal (the refusal names the task id and its status). An unowned, unheld
task/<id> branch is reclaimed like any other.

Usage:
  gopi task archive <branch> --reason <code> [--detail TEXT] [--override-completed]
  gopi task archive --check [--repo PATH]        classify EVERY archive tag,
                                            and name the decided-but-not-landed
                                            set a re-dispatch can act on
  gopi task archive --reclaim [--repo PATH]      remove the decided branches' refs
  gopi task archive --verify-commit <rev> [--repo PATH]
                                            is <rev> usable as evidence that
                                            master ALREADY implements something?
  gopi task archive --unreachable [--repo PATH]
                                            report the commits NO ref names
                                            (the backlog), with what keeps each
                                            alive. Read-only.
  gopi task archive --rescue-tip <rev> --reason <code> [--repo PATH]
                                            give ONE unreachable tip a durable
                                            ref + a recorded reason.


Reasons (--reason):
` + archiveReasonUsageLines() + `
Flags:
  --reason CODE        Why the branch is being archived (required).
  --detail TEXT        Free text recorded under the reason (who/what context).
  --override-completed Acknowledge archiving a completed/verified task's branch.
  --check              Classify EVERY archived branch, not only the decided
                       ones: reason (recorded/absent) × owner row
                       (decided/open/none) × LANDING (is the tag's work on the
                       integration ref? — the tag's tip ancestry, and for a
                       tip that is not an ancestor, whether the paths the
                       branch ADDED are present). Then name the
                       decided-but-NOT-LANDED set: a row that says the work
                       finished over a branch whose work is nowhere. The
                       reason-less tags over a completed/verified row are
                       reported first, unchanged. Exit 1 on either finding.
  --verify-commit REV  ONE COMMAND that falsifies "X is already on master": it
                       peels REV to a commit, asks git merge-base --is-ancestor
                       <c> master (the EXIT STATUS is the verdict), and lists
                       every ref holding that commit — naming the archive tag
                       when one does. Exit 0 = ancestor (usable evidence);
                       exit 1 = not an ancestor (NOT evidence). The measured
                       case: a run read the tip of an ARCHIVED branch as "master
                       already implements it" and closed exit=0.
  --unreachable        Read-only census of this repo's UNREACHABLE TIPS: per
                       tip its short sha, age, subject, the ARCHIVE DECISION
                       naming it (or none), and the keeping window —
                       reflog:<refs> when a reflog still names it (one
                       git reflog expire --expire=now --all from gone), or
                       past-reflog when nothing does (a git gc --prune=now
                       alone removes it). Exit 0; a scan that could not run
                       exits 1.
  --rescue-tip REV     Give the commit REV a DURABLE ref: the archive tag
                       refs/tags/archive/rescued-<short-sha> plus its reason
                       record, through the SAME writer every other archive lane
                       uses (there is no second ref-minting path). Requires
                       --reason. Refuses, writing nothing, when a durable ref
                       already holds the commit; a holder that is only the
                       pushback lane's per-pass mirror does NOT count as one.
  --repo PATH          Repo to inspect/act on. Default: the repo of the cwd.
  -h, --help           This text.

Examples:
  gopi task archive feat/old-idea --reason superseded --detail "landed as feat/new-idea"
  gopi task archive fix/broken --reason compile-failure
  gopi task archive feat/forgotten --reason hand-mistake --override-completed
  gopi task archive --check
  gopi task archive --verify-commit e1701ac8
  gopi task archive --unreachable
  gopi task archive --rescue-tip 00fb325abef8 --reason hand-mistake --detail "orphaned tip from task/01a10e23"
`
}

// archiveReasonUsageLines renders the vocabulary into the usage text. Derived
// from tools.ArchiveReasons so adding a reason cannot leave the help stale
// (Rule 13: the help is a reader of the same definition, never a copy).
func archiveReasonUsageLines() string {
	var b strings.Builder
	for _, r := range tools.ArchiveReasons {
		fmt.Fprintf(&b, "  %-20s %s\n", r, tools.ArchiveReasonDoc(r))
	}
	return b.String()
}

// archiveFlagMembers lists every flag the archive verb accepts (the parser
// rejects anything else rather than silently ignoring a typo — a
// misspelled --override-completed must not read as "no override needed").
var archiveFlagMembers = map[string]bool{
	"reason": true, "detail": true, "override-completed": true,
	"check": true, "reclaim": true, "repo": true, "verify-commit": true,
	"unreachable": true, "rescue-tip": true,
}

// archiveOptions is the parsed archive command line.
type archiveOptions struct {
	Branch   string
	Reason   string
	Detail   string
	Override bool
	Check    bool
	Reclaim  bool
	Repo     string
	// VerifyCommit is the revision `--verify-commit` measures ("" when the
	// flag was not given). It is a REVISION (sha, tag, short sha, ref name),
	// never a branch being archived, which is why it is its own mode.
	VerifyCommit string
	// Unreachable is the census mode: report the repo's unreachable tips and
	// what is keeping each one alive. Read-only.
	Unreachable bool
	// RescueTip is the revision `--rescue-tip` gives a durable ref to ("" when
	// the flag was not given). Like --verify-commit it is an operand-LESS
	// mode's PARAMETER: the subject is a commit, not a branch name, so the
	// positional slot stays the branch archive's.
	RescueTip string
}

// archiveProgTopLevel is the top-level spelling this verb's error lines name.
const archiveProgTopLevel = "gopi " + CliVerbArchive

// isArchiveSubcommand reports whether argv begins with the archive verb.
func isArchiveSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbArchive {
		runArchiveSubcommand(archiveProgTopLevel, args[1:])
		return true
	}
	return false
}

// archiveOperandlessModes lists the modes this invocation asked for that name
// no branch. Derived from the parsed options, never hand-listed against a
// second copy of the flag names (Rule 13), and returned in the usage text's own
// order so a refusal sentence is stable.
func archiveOperandlessModes(opts archiveOptions) []string {
	var modes []string
	if opts.Check {
		modes = append(modes, archiveCheckFlag)
	}
	if opts.Reclaim {
		modes = append(modes, archiveReclaimFlag)
	}
	if opts.VerifyCommit != "" {
		modes = append(modes, archiveVerifyCommitFlag)
	}
	if opts.Unreachable {
		modes = append(modes, archiveUnreachableFlag)
	}
	if opts.RescueTip != "" {
		modes = append(modes, archiveRescueTipFlag)
	}
	return modes
}

// The flags this verb's modes are spelled with, named once (Rule 1b): the
// parser, the refusal sentence and the usage text all read THESE, so a renamed
// flag cannot leave a refusal naming a flag that no longer exists.
// archiveReasonNamespaceLabel is the section header `--check` prints above the
// malformed-entry report (Rule 1b: one spelling, shared with the tools renderer).
const archiveReasonNamespaceLabel = "archive reason namespace"

const (
	archiveCheckFlag        = "--check"
	archiveReclaimFlag      = "--reclaim"
	archiveVerifyCommitFlag = "--verify-commit"
	archiveUnreachableFlag  = "--unreachable"
	archiveRescueTipFlag    = "--rescue-tip"
)

// parseArchiveArgs parses the archive flags without touching the process
// (unit-testable, per the parseShotArgs pattern).
func parseArchiveArgs(args []string) (archiveOptions, error) {
	fs := newMemoryFlagSet()
	positional, err := fs.parse(args)
	if err != nil {
		return archiveOptions{}, err
	}
	for _, name := range fs.order {
		if !archiveFlagMembers[name] {
			return archiveOptions{}, fmt.Errorf("unknown flag --%s", name)
		}
	}
	opts := archiveOptions{
		Reason:       strings.TrimSpace(fs.get("reason")),
		Detail:       strings.TrimSpace(fs.get("detail")),
		Override:     fs.seen["override-completed"],
		Check:        fs.seen["check"],
		Reclaim:      fs.seen["reclaim"],
		Repo:         strings.TrimSpace(fs.get("repo")),
		VerifyCommit: strings.TrimSpace(fs.get("verify-commit")),
		Unreachable:  fs.seen["unreachable"],
		RescueTip:    strings.TrimSpace(fs.get("rescue-tip")),
	}
	// THE OPERAND-LESS MODES ARE MUTUALLY EXCLUSIVE, and the refusals are
	// spelled here rather than left to whichever arm happens to run first: a
	// command line asking for two of them is a misread flag, and the census
	// (which only READS) must never be the mode that silently wins over the
	// rescue (which writes a durable ref). The same discipline the
	// --verify-commit/--check pair already keeps.
	if modes := archiveOperandlessModes(opts); len(modes) > 1 {
		return opts, fmt.Errorf("%s are different modes; pick one", strings.Join(modes, " and "))
	}
	if opts.Unreachable || opts.RescueTip != "" {
		if len(positional) > 0 {
			return opts, fmt.Errorf("--unreachable/--rescue-tip take no branch argument (got %q)", positional[0])
		}
		return opts, nil
	}
	// --verify-commit is the FALSIFIER mode (master_claim.go): it measures a
	// revision instead of archiving anything, so `--reason` is not part of its
	// contract and an positional branch would be a misread flag — both are
	// refused rather than ignored (the same reason the flag set is closed).
	if opts.VerifyCommit != "" {
		if len(positional) > 0 {
			return opts, fmt.Errorf("%s takes no branch argument (got %q)", archiveVerifyCommitFlag, positional[0])
		}
		return opts, nil
	}
	if opts.Check && opts.Reclaim {
		return opts, fmt.Errorf("%s and %s are two different sweeps; pass one", archiveCheckFlag, archiveReclaimFlag)
	}
	if opts.Check || opts.Reclaim {
		if len(positional) > 0 {
			return opts, fmt.Errorf("--check/--reclaim take no branch argument (got %q)", positional[0])
		}
		return opts, nil
	}
	if len(positional) == 0 {
		return opts, fmt.Errorf("no branch given")
	}
	if len(positional) > 1 {
		return opts, fmt.Errorf("expected one branch, got %d", len(positional))
	}
	opts.Branch = strings.TrimSpace(positional[0])
	if opts.Branch == "" {
		return opts, fmt.Errorf("branch must not be blank")
	}
	if opts.Reason == "" {
		return opts, fmt.Errorf("--reason is required (one of: %s)", tools.ArchiveReasonList())
	}
	if !tools.ArchiveReasonKnown(opts.Reason) {
		return opts, fmt.Errorf("unknown --reason %q (want one of: %s)", opts.Reason, tools.ArchiveReasonList())
	}
	return opts, nil
}

// runArchiveSubcommand executes the archive verb (the only place that exits).
//
// progName is the spelling the operator actually typed — "gopi task archive" from
// the top-level verb, "gopi task archive" from the re-homed family spelling
// (epic verb-restructure, 2026-10-05). Every line this verb prints that names
// ITSELF (the usage-refusal pair and the two error prefixes) uses it, so a
// reader of `gopi task archive --bogus` is not told about a verb they did not
// type; the branch-archive DECISION text (formatArchiveResult) is the
// archive's own record and keeps its own spelling for the same reason Rule 13
// exists. Rule 1b: the caller passes the constant, never a literal.
func runArchiveSubcommand(progName string, args []string) {
	if len(args) > 0 && (args[0] == HelpFlag || args[0] == HelpFlagLong) {
		fmt.Print(archiveUsage())
		return
	}
	opts, err := parseArchiveArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n\n%s", progName, err, archiveUsage())
		os.Exit(1)
	}
	if opts.Check {
		runArchiveCheck(opts.Repo)
		return
	}
	if opts.VerifyCommit != "" {
		runArchiveVerifyCommit(opts)
		return
	}
	if opts.Reclaim {
		runArchiveReclaim(opts.Repo)
		return
	}
	if opts.Unreachable {
		runArchiveUnreachable(opts.Repo)
		return
	}
	if opts.RescueTip != "" {
		runArchiveRescueTip(opts)
		return
	}
	// THE REPO IS RESOLVED BY THE SAME RESOLVER THE OTHER FOUR MODES USE
	// (measured defect 01a1301b, 2026-10-12): this lane used to call
	// tools.ArchiveBranchCwd, which resolved the repo from the PROCESS CWD and
	// never looked at opts.Repo — so `--repo` was parsed, advertised in the
	// usage text, honoured by --check/--reclaim/--unreachable/--verify-commit,
	// and silently IGNORED here. The three invocations (with --repo pointing at
	// the target, with no --repo at all, and with --repo naming a path that does
	// not exist) produced one byte-identical output and one exit status, and the
	// write failed with git's own `Failed to resolve '<branch>' as a valid ref`
	// — a message about a MISSING BRANCH, which the operator had asked to
	// archive in ANOTHER repo. A wrong repo for the archive tag is the worst
	// outcome of a verb whose whole purpose is to make a decision legible, so
	// the resolution happens ONCE, before anything is read or written, and a
	// --repo that cannot be resolved is an error (Rule 15: never a silent
	// fallback to the cwd).
	repo, err := archiveTargetRepo(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		os.Exit(1)
	}
	res, err := tools.ArchiveBranch(repo, opts.Branch, opts.Reason, opts.Detail, opts.Override)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		os.Exit(1)
	}
	fmt.Print(formatArchiveResult(res))
	if !res.Recorded {
		os.Exit(1)
	}
	// THE DECISION IS CARRIED THROUGH. The tag was just written; the branch ref
	// that the decision was made ABOUT is what this arm removes, so the verb
	// does not leave the decided ref behind for every other lane to skip
	// forever. Its own guards (same-commit, no worktree, never master/task/*)
	// are the arm's, not this caller's — a refusal is printed, never forced.
	//
	// THE CONTRADICTION THIS LEG CANNOT REACH (measured defect 2026-10-12, fixed
	// in tools.ArchiveDecide's guard ⓪): the tag used to be written for
	// master/main, and THIS leg then refused to reclaim it — "archived master as
	// archive/master" and "skipped … never a reclaim candidate" in one output.
	// The integration branch is now refused at the gate, before the write, so a
	// successful archive here is always a branch this leg is allowed to act on
	// (or to refuse for its OWN reasons: a moved tip, a live worktree, a live
	// row). A branch the gate let through and this leg refuses is a real,
	// reportable divergence between a NAME-shaped veto and a FACT-shaped one, so
	// the row is printed rather than swallowed.
	runArchiveReclaimAfterArchive(repoOfArchiveTarget(opts.Repo), opts.Branch)
}

// formatArchiveResult renders the outcome. A refusal is printed as the
// gate's own message (it already names the row, its status and the exact
// override) — never re-worded here, so the operator sees the same text a
// future reader of the code will.
func formatArchiveResult(res tools.ArchiveBranchResult) string {
	if !res.Recorded {
		return res.Refusal + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "archived %s as %s\n", res.Branch, res.Tag)
	fmt.Fprintf(&b, "  reason: %s\n", res.Reason)
	switch {
	case res.Unowned:
		b.WriteString("  owner:  (no task row tracked this branch)\n")
	case res.Owner != "":
		fmt.Fprintf(&b, "  owner:  task %s (%s)\n", res.Owner, res.Status)
	}
	// The SUCCESSOR exception, when it was taken: a LIVE owner did not stop the
	// archive because that row is the branch's rework successor, not its tenant
	// (tools.ArchiveDecision.SuccessorTaskID). Rendered here so the operator
	// sees WHY a live row passed — the alternative is an operator wondering
	// whether the gate was bypassed.
	if res.Successor != "" {
		fmt.Fprintf(&b, "  successor: task %s is this branch's %s SUCCESSOR — the branch is its SOURCE, not its deliverable, so the ref is spent and the work lives on that row\n",
			res.Successor, tools.ArchiveReworkLaneName())
	}
	fmt.Fprintf(&b, "  read back: git cat-file blob refs/gopi/archive-reason/%s\n", res.Branch)
	return b.String()
}

// runArchiveCheck runs the backlog check and exits 1 on findings (a check
// whose failure mode is silent is not a check). The report itself is the
// deliverable — it names the tags AND their owning rows.
//
// TWO SECTIONS, ONE EXIT CODE. The backlog report answers the 21:04 question
// (a reason-less tag over a decided row) and prints FIRST, unchanged, so a
// reader's existing grep keeps working. The classification report answers the
// question the backlog's own narrowing left open — EVERY archived branch,
// classified by reason × owner row × landing — and names the re-dispatchable
// set inside it. Either section finding something is a non-zero result.
func runArchiveCheck(repoPath string) {
	repo, err := archiveTargetRepo(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --check: %v\n", err)
		os.Exit(1)
	}
	// THE MALFORMED SECTION COMES FIRST, and it is a FINDING (non-zero exit),
	// not a warning: a companion ref the namespace scan had to SKIP used to
	// abort every archive face (measured defect 01a143b4), and the silence that
	// replaced it would be the same defect in a quieter costume — a reason
	// record that cannot be read is not a reason nobody wrote (Rule 15).
	malformed, err := tools.ArchiveNamespaceMalformed(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --check: %v\n", err)
		os.Exit(1)
	}
	if report := tools.ArchiveReasonMalformedReport(malformed); report != "" {
		// The report names its own subject ("archive reason namespace:") and
		// every refused ref with its type and the repair — the CLI adds no
		// second header, so one finding reads as one paragraph.
		fmt.Printf("%s: %s\n", archiveReasonNamespaceLabel, repo)
		fmt.Print(report)
	}

	report, n, err := tools.ArchiveBacklogReport(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --check: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report)

	classReport, unlanded, err := tools.ArchiveBacklogClassReport(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --check: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(classReport)

	if n > 0 || unlanded > 0 || len(malformed) > 0 {
		os.Exit(1)
	}
}

// runArchiveReclaim is the `gopi task archive --reclaim` face: the reclaim sweep
// alone, with no branch argument. A SCAN failure exits 1 (the sweep could not
// answer), while rows the sweep REFUSED are reported and exit 0 — a refusal is
// a decision, not a failure of the command that asked (Rule 15: the operator
// reads the reason, not a silent green).
func runArchiveReclaim(repoPath string) {
	repo, err := archiveTargetRepo(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --reclaim: %v\n", err)
		os.Exit(1)
	}
	res := tools.ArchiveReclaimDecidedRefs(repo)
	fmt.Print(tools.ArchiveReclaimReport(res))
	if res.Err != "" {
		os.Exit(1)
	}
}

// runArchiveUnreachable is the `--unreachable` face: the census of commits no
// ref names, with what is keeping each one alive. READ-ONLY, exit 0; a scan
// that could not run exits 1 (Rule 15 — a census that could not measure must
// never render as a clean zero).
func runArchiveUnreachable(repoPath string) {
	repo, err := archiveTargetRepo(repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive %s: %v\n", archiveUnreachableFlag, err)
		os.Exit(1)
	}
	census, err := tools.ArchiveUnreachableCensusAt(repo, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive %s: %v\n", archiveUnreachableFlag, err)
		os.Exit(1)
	}
	fmt.Print(tools.ArchiveUnreachableReport(census))
}

// runArchiveRescueTip is the `--rescue-tip` face: give ONE unreachable tip a
// durable ref (the archive tag) plus the recorded WHY, through the project's ONE
// archive writer. The reason is mandatory: a rescue is a decision, and a
// decision without a why cannot be reviewed or reversed (the 21:04 lesson this
// verb's whole gate exists for).
//
// A REFUSAL (a durable ref already holds the commit) prints and exits 0 — the
// operator asked a question and got the answer. A scan/write FAILURE exits 1.
func runArchiveRescueTip(opts archiveOptions) {
	prog := "gopi task archive " + archiveRescueTipFlag
	repo, err := archiveTargetRepo(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
		os.Exit(1)
	}
	if !tools.ArchiveReasonKnown(opts.Reason) {
		fmt.Fprintf(os.Stderr, "%s: --reason is required (one of: %s)\n", prog, tools.ArchiveReasonList())
		os.Exit(1)
	}
	res, err := tools.ArchiveRescueTip(repo, opts.RescueTip, opts.Reason, opts.Detail)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
		os.Exit(1)
	}
	fmt.Print(tools.ArchiveRescueReport(res))
	if !res.Recorded {
		return
	}
}

// runArchiveReclaimAfterArchive runs the same sweep over the repo the archive
// just landed in, best-effort: the archive itself already succeeded and is
// recorded, so a failure to reclaim must be REPORTED without undoing it.
//
// It prints THIS decision's row (and only rows that name the branch just
// archived — the rest are the operator's to ask for with --reclaim) through the
// arm's own row renderer, so the automatic leg and the sweep cannot render the
// same outcome two ways (Rule 13). A row that is NOT this branch's is not
// printed here, but a scan FAILURE is: a reclaim leg that could not run must
// never look like one that found nothing (Rule 15).
func runArchiveReclaimAfterArchive(repo, branch string) {
	if repo == "" || branch == "" {
		return
	}
	res := tools.ArchiveReclaimDecidedRefs(repo)
	for _, r := range res.Rows {
		if r.Branch != branch {
			continue
		}
		fmt.Printf("  %s\n", tools.ArchiveReclaimRowLine(r))
	}
	if res.Err != "" {
		fmt.Fprintf(os.Stderr, "gopi task archive: reclaim sweep could not run: %s\n", res.Err)
	}
}

// repoOfArchiveTarget resolves the repo an archive acted in, for the reclaim
// leg. It fails SOFT ("" means "do not reclaim"): the archive is already
// written and the reclaim leg must never turn a successful archive into an
// error.
func repoOfArchiveTarget(explicit string) string {
	repo, err := archiveTargetRepo(explicit)
	if err != nil {
		return ""
	}
	return repo
}

// archiveTargetRepo resolves the repo to act on: the explicit --repo, else the
// cwd's own repo.
func archiveTargetRepo(explicit string) (string, error) {
	if explicit != "" {
		return tools.RepoRootAt(explicit)
	}
	return tools.RepoRootAt(".")
}

// runArchiveVerifyCommit runs `--verify-commit`: ONE command that falsifies
// "X is already on master" — the acceptance criterion of the master-claim
// epic, and the surface a reviewer or a worker can run without reading a
// report.
//
// The verdict IS the exit status (0 = the commit is an ancestor of master,
// therefore usable as evidence; 1 = it is not). The output carries the
// observation that produced it — the commands and their exit codes/refs — so
// the line can be pasted into a report's evidence section verbatim (Rule 5 /
// Rule 31: a claim carries the instrument that produced it).
//
// An UNRESOLVABLE revision also exits 1: it is not evidence of anything. The
// output distinguishes the two cases in words, so the exit status never has to
// carry a meaning a reader would have to guess.
func runArchiveVerifyCommit(opts archiveOptions) {
	repo, err := archiveTargetRepo(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi task archive --verify-commit: %v\n", err)
		os.Exit(1)
	}
	ev := tools.MasterClaimEvidenceAt(repo, opts.VerifyCommit)
	fmt.Printf("repo: %s\n", repo)
	fmt.Printf("integration branch: %s\n", tools.IntegrationBranchName)
	fmt.Printf("measured against: %s\n", ev.ClaimRef)
	fmt.Println(ev.VerdictLine())
	for _, obs := range ev.Observations() {
		fmt.Printf("  %s\n", obs)
	}
	if ev.OK() {
		return
	}
	os.Exit(1)
}
