// Package devworkspace — the CLONE CHANNEL's workspace: clone, the
// pull --rebase → build → push loop, its brake, and the reclaim.
//
// WHY IT EXISTS (operator ruling, 2026-10-18): a local master every agent
// pulled from by hand was a single point. N agents cannot pull one local
// master at once, so each agent's baseline was however stale the last manual
// pull left it — which is the real source of the conflicts, not the code.
// The GitHub shape replaces it: ONE truth at origin, each agent clones its own
// workspace, and per task it runs
//
//	clean → pull --rebase → (the agent's own commits) → build → push
//	push refused → back to clean → pull --rebase
//
// WHY "CLEAN" COMES FIRST (measured, 2026-10-18): `git pull --rebase` REFUSES
// to run over a worktree with uncommitted changes (`error: cannot pull with
// rebase: You have unstaged changes`, exit 128) — it would have to move a tree
// somebody is editing. Discovered that way, a dirty workspace surfaces as git's
// raw exit 128 dressed up as the channel's reason: no paths, no next step, and
// indistinguishable from "the remote is gone". So the loop asks
// `git status --porcelain` FIRST and, when the answer is dirty, refuses the
// pull itself in the channel's own words: which paths, how many, and that the
// operator must commit or revert them (this package never stashes, commits or
// cleans — a workspace is the operator's work, not ours to swallow). Untracked
// entries ('??') do NOT block: git's rebase tolerates them, the agent's build
// products are exactly that, and a gate that refused them would stop the second
// attempt of every run.
//
// The order is therefore: clean → pull → build → push. Clean before pull because
// the pull is the one git step that needs a tree it may move; pull before build
// because the build must run on the merged tree (a build of an unmerged tree
// proves nothing about what lands); build before push because pushing what was
// never built is the one thing the channel exists to prevent.
//
// and hands the workspace back when the push lands (the code is at origin; a
// kept local clone would only rot).
//
// WHAT THIS PACKAGE IS NOT: not a daemon, not a cron, not a tick (Rule 35).
// The whole loop is a `for` inside ONE `gopi worker` invocation. Nothing here
// survives the command that started it except the workspace directory itself,
// which is the point.
//
// THE BRAKE (the loop cannot live-lock): MaxPushAttempts attempts, exponential
// backoff with jitter between them, and then a LOUD stop that KEEPS the
// workspace and writes a re-readable record into it (Rule 15 — never a silent
// give-up). "Refused" is a MEASURED classification (the remote's branch is not
// an ancestor of ours, i.e. somebody pushed first), never "the push command
// exited non-zero": a rejected credential or a deleted remote is a hard error,
// not something to retry.
package devworkspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxPushAttempts is the INNER brake: how many pull → build → push rounds
	// ONE delivery attempt gets when the push keeps being refused because
	// origin moved first. A refusal by a newer commit is a clock, not a
	// decision: only retrying it makes sense, and only the loop can retry it.
	// Named because the report quotes it and a test asserts it.
	MaxPushAttempts = 3
	// MaxDeliveryRounds is the OUTER brake: how many delivery ATTEMPTS one task
	// gets. An attempt is one clean → pull → build → push run of the
	// deterministic steps; when it comes back with a Blocker the caller runs one
	// agent round (giving it the blocker's own words and the workspace path) and
	// tries again. Named once and read by the logic AND by the log lines, so the
	// number an operator sees in "delivery attempt 2/3" is the number that
	// actually bounds the loop (Rule 1b).
	MaxDeliveryRounds = 3
	// BackoffBase is the first wait between attempts; each further attempt
	// multiplies it by BackoffFactor (0.5s / 2s / 8s).
	BackoffBase = 500 * time.Millisecond
	// BackoffFactor is the growth of the wait per attempt.
	BackoffFactor = 4
	// BackoffJitterRatio is the ± fraction of each wait that is randomized, so
	// concurrent agents that were refused together do not retry in lockstep.
	BackoffJitterRatio = 0.2

	// BrakeRecordName is the file written INTO the kept workspace when the
	// brake fires — the re-readable record (Rule 15) of what was refused and
	// why, so an operator (or a repair run) does not have to re-derive it.
	BrakeRecordName = "gopi-push-brake.txt"

	// MaxReportedDirtyPaths is the N of the clean gate's report: how many
	// offending status lines the refusal names by hand. Named because the
	// message quotes it and a test asserts it: a workspace with hundreds of
	// dirty paths must not paste them all into a ledger row, but it must still
	// show enough for the operator to recognise the work.
	MaxReportedDirtyPaths = 5

	// DefaultBranch is the branch a clone starts on and the branch a push
	// targets when the workspace carries no other one.
	DefaultBranch = "master"

	// refsHeadsPrefix is git's own spelling for the local branch namespace. It
	// is declared here (Rule 1b/13) because two steps of this package name it:
	// the ground checkout (does the row's ref already exist?) and the reuse
	// probe. This package imports nothing from internal/tools on purpose, so
	// the value is spelled once HERE rather than mirrored at each call site.
	refsHeadsPrefix = "refs/heads/"
	// checkoutVerb / createBranchFlag are git's branch-moving verb and the flag
	// that creates the branch it moves to, spelled once for checkOutGroundBranch.
	checkoutVerb     = "checkout"
	createBranchFlag = "-b"

	// gitBinary is the one program this package executes.
	gitBinary = "git"

	// THE CHILD'S LANGUAGE (Rule 1: every variable NAME and every value below is
	// spelled once, here, and referenced everywhere it is needed).
	//
	// WHY A CHILD'S LANGUAGE IS A CORRECTNESS FACT AND NOT A COSMETIC ONE: this
	// package PARSES git's human-readable output — couldNotApplyMarker,
	// gitHintLinePrefix, the `CONFLICT (content): Merge conflict in <path>`
	// line — so a git that answers in another language does not fail, it goes
	// SILENT: the marker misses, the pull refusal's Blocker.Words loses the one
	// line that names what stopped it, and the failure is indistinguishable
	// from "git said nothing useful". Measured on git 2.43.0 with the caller's
	// environment exported as zh_CN.UTF-8:
	//
	//	LC_ALL=zh_CN.UTF-8 git pull --rebase  →  错误：不能应用 4eaa590... ours
	//	LC_ALL=C             git pull --rebase  →  error: could not apply 4eaa590... ours
	//
	// A CHILD inherits the caller's locale, so whoever runs a worker under a
	// Chinese desktop makes every parser in this package blind. The fix is
	// therefore at the ONE place a git child is created, never at a parser:
	// read the language git answers in, do not guess what it said.
	//
	// localeEnvName is the variable that OVERRIDES every other LC_* category
	// and LANG when it is set (POSIX): setting it alone pins the message
	// language regardless of what the caller exported.
	localeEnvName = "LC_ALL"
	// cLocaleValue is the POSIX C (untranslated) locale: git's message
	// catalogue resolves to its source strings, which are the strings the
	// markers in this file are written against.
	cLocaleValue = "C"
	// languageEnvName is LC_MESSAGES' value — the SAME pin spelled for the
	// category-specific variable, so a child that ranks the category above
	// LC_ALL still lands on untranslated messages. Measured (git 2.43.0,
	// `rev-parse --verify nosuchref`): `LC_MESSAGES=C LANG=zh_CN.UTF-8` answers
	// in ENGLISH — the category pin holds on its own.
	languageEnvName = "LC_MESSAGES"
	// languageListEnvName is gettext's OWN precedence list (GNU), and it is
	// ranked ABOVE every locale category for a NON-C message locale: measured
	// `LANG=en_US.UTF-8 LANGUAGE=zh_CN.UTF-8` answers `致命错误：…` while
	// `LANG=zh_CN.UTF-8 LANGUAGE=en_US.UTF-8` answers English — the list
	// overrides LANG outright.
	//
	// WHAT IT DOES **NOT** DO, and this was measured rather than assumed, because
	// the obvious guess is wrong: LANGUAGE does NOT defeat an explicit C pin.
	// Six shapes were tried (`LC_ALL=C LANGUAGE=zh_CN.UTF-8`, bare `LANGUAGE=
	// zh_CN`, `+ LC_CTYPE=zh_CN.UTF-8`, `+ LC_MESSAGES=zh_CN.UTF-8`, `LC_ALL=
	// LANGUAGE=zh_CN.UTF-8 LC_MESSAGES=C`, `LANGUAGE=zh_CN.UTF-8` alone) and
	// EVERY one answered in English; `LC_ALL=C` also beats `LANG=zh_CN.UTF-8
	// LC_MESSAGES=zh_CN.UTF-8 LANGUAGE=zh_CN.UTF-8`. So emptying the list is
	// not what saves the two C pins above — it is set because a single
	// environment can carry BOTH a C pin and a translated LANG, and the list is
	// the one variable that can outrank LANG on the day somebody retunes the
	// pin. It is EMPTIED (not merely left unset) because an empty value
	// disables the list, which is the state this package wants in writing.
	languageListEnvName = "LANGUAGE"

	// THE PULL-REFUSAL EVIDENCE (Rule 1: every limit and every semantic
	// spelling below is named once and referenced, so the words this package
	// composes cannot drift from the commands it says it ran).
	//
	// pushVerb is the delivery's last git step (publishGround), spelled once so
	// the command and the prose describing it cannot drift (Rule 1b).
	//
	// gitPullVerb / gitPullRebaseFlag are the step that refuses; unmergedNameOnlyFlag
	// is `git diff`'s "give me the paths that are UNMERGED" — the conflicted file
	// list, which git REPORTS ONLY WHILE THE REBASE IS IN PROGRESS (measured: it
	// prints nothing after `git rebase --abort`), so it is captured before the
	// abort.
	gitPullVerb        = "pull"
	pushVerb           = "push"
	gitPullRebaseFlag  = "--rebase"
	gitRebaseVerb      = "rebase"
	gitRebaseAbortFlag = "--abort"
	// gitMergeVerb / gitMergeNoEditFlag / gitMergeAbortFlag are the FALLBACK
	// integration step (pullMergeFallback): when a rebase cannot replay this
	// branch's commits onto the new base, the same two TIPS can still be
	// integrated with a merge — see that function for the measured reason the
	// two are not the same question. Named once so the command the channel says
	// it ran and the command it did run cannot drift (Rule 1b/13).
	gitMergeVerb         = "merge"
	gitMergeNoEditFlag   = "--no-edit"
	gitMergeAbortFlag    = "--abort"
	gitDiffVerb          = "diff"
	unmergedNameOnlyFlag = "--name-only"
	unmergedNameOnlyArg  = "--diff-filter=U"
	// catFileTypeVerb / catFileTypeFlag are `git cat-file -t <object>`: the one
	// question that turns a commit id in a failure message into a MEASURED fact
	// ("commit" = it is here; "Not a valid object name" = it is not).
	catFileTypeVerb = "cat-file"
	catFileTypeFlag = "-t"

	// gitHintLinePrefix marks git's ADVICE block. It is dropped from a pull
	// refusal's words: the advice answers "how would I resolve this by hand",
	// while the refusal's readers need "which file, which commit". Measured on
	// git 2.43.0: four consecutive hint lines sit at the END of `git pull
	// --rebase`'s conflict output, which is exactly why a tail of six lines kept
	// the advice and discarded `CONFLICT (content): Merge conflict in <path>`.
	gitHintLinePrefix = "hint:"
	// couldNotApplyMarker is git's own phrasing for the commit it stopped on.
	// The id after it is parsed and MEASURED (catFileTypeVerb) rather than
	// quoted as a lead.
	couldNotApplyMarker = "could not apply "
	// couldNotApplyEllipsis is what git prints between that id and the subject.
	couldNotApplyEllipsis = "..."

	// MaxPullWordsLines bounds the pull output carried into a blocker (a rebase
	// across a large tree can print one line per conflicting path). The lines
	// kept are the FIRST ones, not the last: the criteria (`Rebasing`,
	// `CONFLICT … in <path>`, `error: could not apply <id>`) are printed before
	// the advice block, which is the inversion this constant exists to pin.
	MaxPullWordsLines = 16
	// pullBodyLineJoiner JOINs git's own lines inside one evidence part, and
	// pullPartJoiner joins the parts. They are named constants because a reader
	// other than the builder has to split them apart again: the decisive-line
	// extractor (decisive_lines.go) must see the SAME boundaries, or a composed
	// 600-rune string reaches the briefing as one "line" — the very defect the
	// extractor exists to end.
	pullBodyLineJoiner = " ; "
	pullPartJoiner     = " | "
	// tailLineJoiner does the same for a build/push output's last lines.
	tailLineJoiner = " | "

	// MaxMeasuredConflictCommits bounds how many `could not apply <id>` lines get
	// their own `git cat-file -t` measurement, so a pathological rebase cannot
	// turn one refusal into an unbounded number of subprocesses.
	MaxMeasuredConflictCommits = 3
	// shellBinary runs the DECLARED build command (a shell command by
	// contract, exactly like the merge-time build gate's).
	shellBinary = "/bin/sh"

	// gitCommandTimeout bounds one git subprocess (clone excluded: a cold
	// clone of a large repository legitimately takes longer — see
	// CloneTimeout).
	gitCommandTimeout = 120 * time.Second
	// CloneTimeout bounds the initial clone.
	CloneTimeout = 30 * time.Minute
	// DefaultBuildTimeout is used when the caller hands none.
	DefaultBuildTimeout = 300 * time.Second
)

// Config is one task's clone-channel parameters. Every value is supplied by
// the caller (cmd/agent) from the ONE place that resolves it — this package
// reads no settings file, so the channel cannot disagree with the config
// package about where the repo or the root is (Rule 2/14).
type Config struct {
	// TaskID names the workspace subdirectory (the row's id, so ground ↔
	// workspace ↔ ledger stay cross-comparable from the name alone).
	TaskID string
	// DevRepo is the source repository, host:path (git's own ssh spelling).
	DevRepo string
	// Root is the reclaimable PARENT directory.
	Root string
	// Build is the DECLARED build command (settings repos.<path>.build). It
	// is never discovered here: an empty value means the caller found no
	// declaration and refused before calling (Rule 12).
	Build string
	// BuildTimeout caps the build subprocess.
	BuildTimeout time.Duration
	// Branch overrides DefaultBranch ("" = the workspace's current branch).
	Branch string
	// GroundBranch is an OPTIONAL private address for this workspace — the
	// row's own `task/<task-id>` ref — and it is EMPTY for the clone channel's
	// own shape.
	//
	// THE CLONE CHANNEL LEAVES IT EMPTY (the operator's ruling, 2026-11-05,
	// which supersedes the 2026-10-27 wiring this comment used to describe):
	// a run's clone is a workspace of its OWN — nothing else works in it, so
	// there is nobody to collide with and no private ref to mint. An EMPTY
	// GroundBranch therefore means "stand on the INTEGRATION branch and deliver
	// by pushing it", which is exactly what the ruling requires: clone from
	// `master`, work on `master`, push `master`, and resolve any conflict in
	// your own ground. The row is addressed by its WORKSPACE PATH
	// (`deliverable.worktree`), which the claim stamps and `gopi task show
	// --worktree` resolves — never by a branch name.
	//
	// WHAT THE OLD SHAPE COST, kept because it is why BaseBranch exists at all:
	// minting `task/<id>` one second after the clone put the run's commits on a
	// ref no merge path consumes and no server receives (the delivery's ONE
	// destination is `base`), while `deliverable.branch` recorded a name that
	// resolved to no row (before the wiring it recorded `master` — "361 owners
	// of branch master"). Both halves were measured 2026-10-27.
	//
	// A caller that DOES name one still gets it: `Open` checks the workspace out
	// there (creating it when it is missing), so the pre-ruling shape is
	// reachable for anything that needs a private ref. Nothing in this package
	// mints one by itself.
	GroundBranch string
	// BaseBranch is the INTEGRATION branch this workspace rebases onto before
	// it publishes ("" = DefaultBranch). It is separate from Branch because the
	// two are different questions on a ground branch: `pull --rebase` integrates
	// what landed on the integration branch, and the push publishes that SAME
	// integration branch (it is the ONE refspec publishGround builds), while
	// `Branch` stays the row's ADDRESS. Asking the same name for both is exactly
	// the conflation that made the clone channel's rows unaddressable (see
	// GroundBranch).
	BaseBranch string
	// Worktree is `deliverable.worktree`: the directory the ROW records as the
	// place its work lives. It is the other half of the anchor question
	// delivery_anchor.go asks — "is the directory I am about to publish from the
	// directory this row says its deliverable is in?" — and it exists because a
	// run can commit in a DIFFERENT directory than the one the leg publishes
	// from (measured 2026-10-27, row 01a17dee: its own ground was empty, 0 ahead
	// / 0 dirty, and the two real commits were in another row's ground).
	//
	// EMPTY means the caller has no recorded ground to compare against — the
	// legacy shape and every caller with no row to name. The gate then asks no
	// question; it does not guess a value to make a comparison succeed.
	Worktree string
	// DeliveredTip is the tip THIS RUN's own delivery already pushed, when one
	// did — the SECOND fact the empty gate needs, because `rev-list --count`
	// alone cannot tell "this ground produced nothing" from "this ground's work
	// is already on the integration branch" (row 01a182df: the channel's own
	// push moves `origin/<base>` onto our tip, so the count is 0 by definition
	// afterwards).
	//
	// It is a VALUE, not a global and not a latch: the gate still MEASURES
	// whether the base reaches it, and every reading that cannot be taken
	// abstains into the empty kind. EMPTY = this run has pushed nothing, which
	// is the pre-existing behaviour for every caller that has no delivery to
	// report.
	DeliveredTip string
	// RootSource is how Root was resolved ("settings workspace_root",
	// "GOPI_WORKSPACE_ROOT", "default"). Carried so TraceToSource can say who
	// declared the workspace root it is refusing to build in (Rule 1b: the
	// resolution and its narration must not disagree).
	RootSource string
	// Log receives one line per step; nil = no narration.
	Log io.Writer
}

// Result is what one task's clone-channel round did.
type Result struct {
	Dir       string
	Branch    string
	Attempts  int
	Pushed    bool
	Reclaimed bool
	// Exhausted reports that the brake fired: MaxPushAttempts rounds were all
	// refused and the workspace was KEPT on purpose.
	Exhausted bool
	// Detail is the one-line human record of the outcome.
	Detail string
}

func (r Result) printf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "clone-channel: "+format+"\n", args...)
}

// BlockerKind names WHICH deterministic step of one delivery attempt refused to
// go on. It is the only classification the channel makes, and it is a FACT, not
// a verdict: the step decides what the reader can do about it, and this package
// names no remedy (the agent that gets the blocker back decides what to do —
// committing, resolving a conflict, fixing a build — Rule 30's line).
type BlockerKind string

const (
	// BlockerKindDirty: the workspace carries uncommitted paths, so the pull
	// in front of it was refused instead of being allowed to destroy them.
	BlockerKindDirty BlockerKind = "dirty"
	// BlockerKindPull: `git pull --rebase` itself failed (a content conflict,
	// a rejected remote) — the workspace is back on its own HEAD.
	BlockerKindPull BlockerKind = "pull"
	// BlockerKindBuild: the DECLARED build command failed.
	BlockerKindBuild BlockerKind = "build"
	// BlockerKindPush: every push attempt of one delivery was REFUSED because
	// origin moved first (MaxPushAttempts of them, with the inner backoff).
	BlockerKindPush BlockerKind = "push"
	// BlockerKindMan: a man SOURCE's generated region no longer matches the help
	// page the binary this branch just built prints (see man_gate.go) — the branch
	// moved a `-h` text and did not regenerate `man/*.md`. It is a POST-BUILD
	// check by construction: the fact source is the binary, so it cannot run
	// before one exists, and a check against a stale binary would call drift
	// "up to date".
	BlockerKindMan BlockerKind = "man"
	// BlockerKindDiskExhaustion: the declared build was refused by the
	// FILESYSTEM, not by the code — `no space left on device` (see
	// disk_exhaustion.go). It is its OWN kind because the two facts send a reader
	// to completely different places: a code failure means fix the revision, a
	// full disk means the revision was never linked at all and the machine has to
	// be given room. Sharing `BlockerKindBuild`'s wording is what made a full
	// partition look like a compile error and be re-run over and over.
	BlockerKindDiskExhaustion BlockerKind = "disk-exhaustion"
)

// pullStepCommand is the ONE spelling of the command the pull blocker reports it
// ran (Rule 1b/13): `Error()`/`Report()` quote it and the test asserts it, so a
// reader is never given a command this channel did not actually execute. The
// base branch is appended by the caller that knows it; the patched form is
// "%s" + this constant.
const pullStepCommand = "git pull --rebase origin "

// mergeStepCommand is the same spelling for the FALLBACK step, so a refusal
// that reports both attempts names each command the channel actually ran
// (Rule 1b/13 — the reader must never be handed a command nobody executed).
const mergeStepCommand = "git merge origin/"

// Blocker is one delivery attempt's refusal, handed back to the caller with the
// tool's own words intact. See the type's contract:
//
//	Kind  — which step refused (the BlockerKind values above, plus the ones
//	        declared beside the gate that produces them: test-compile,
//	        empty, delivered, inherited).
//	Step  — THE COMMAND whose refusal this is, when the caller knows it
//	        (currently the pull step: `git pull --rebase origin <base>`, plus
//	        the `git diff --name-only --diff-filter=U` the evidence came from).
//	        Empty for the kinds whose kind name already names the step. It exists
//	        because a reader who cannot copy the failing command has to guess it,
//	        and a guessed command is not reproducible.
//	Words — THE TOOL'S OUTPUT, VERBATIM (Rule 15). Never a paraphrase: the
//	        agent that reads this next has to recognise the failure by the
//	        words it would have seen itself, and a reworded failure is a
//	        failure somebody has to re-derive. For the clean gate the tool is
//	        `git status --porcelain`, so Words is its listing byte for byte.
//	Paths — the workspace's uncommitted paths when the gate is what refused
//	        (parsed from those same words, so the list cannot disagree with
//	        them); for the PULL kind, the conflicted paths `git diff
//	        --name-only --diff-filter=U` reported BEFORE the rebase was
//	        aborted. Empty for the other kinds.
//
// A Blocker is NOT an error and must not be collapsed into one: an error ends a
// run, a blocker is the fact the next agent round is given.
type Blocker struct {
	Kind  BlockerKind
	Step  string
	Words string
	Paths []string
	// Ground is the DIRECTORY whose filesystem was measured, for the kinds whose
	// refusal is a fact about a machine rather than about the revision (today:
	// BlockerKindDiskExhaustion). It is filled by the step that measured it, so
	// the report renders figures about the path the build really ran in rather
	// than about whatever directory the reader happens to be in.
	Ground string
	// Marker is the phrase that matched out of the shared disk-exhaustion
	// vocabulary (scratch.DiskExhaustionMarkers) — the measurement, quoted, so a
	// reader can see WHICH spelling arrived (`no space left on device` vs
	// `enospc` vs a quota form) instead of being told "the disk is full".
	Marker string
}

// Report renders the blocker for a human reader (a log line, a ledger reason):
// the step by name, the paths the gate saw (at most MaxReportedDirtyPaths named,
// the rest counted) and the tool's words underneath, QUOTED so it is obvious
// which part is the tool's and which part is this channel's.
//
// It is deliberately NOT the shape handed to an agent: the agent gets Kind and
// Words (cmd/agent's deliveryRetryBriefing), because a report that also says
// what to do next is a step prescription written in prose.
//
// THE SELF-PREFIX CONTRACT (decided 2026-10-31, and this is the decision, not a
// description of the code): Report() returns ONE SENTENCE THAT ALREADY OPENS
// WITH THE CHANNEL'S PREFIX — every Kind branch above begins with
// CloneChannelPrefix, and a caller that wants the speaker's identity gets it
// here. That is the ONLY of the two possible conventions this channel keeps, and
// it is kept because a Blocker also travels as an `error` (Error() IS Report())
// through the paths that are not the delivery loop (Open, a ledger note, a log
// line): a bare report in those places would arrive with no speaker at all.
//
// THE CONSEQUENCE, which is a RULE FOR CALLERS: a line that EMBEDS a Report()
// must not add the prefix itself — it passes the report through
// CloneChannelReason, which strips at most one prefix off the body and adds
// exactly one back. Measured cost of getting this wrong (2026-10-30/31): the
// delivery loop's exhausted() arm read `clone channel: delivery did not land in
// 3 attempt(s); last blocker (dirty): clone channel: refusing to pull: …`
// (PROBE_PREFIX_COUNT=2) on the row's reason AND the returned error, and
// internal/tools' retry face read `delivery owed: clone channel: clone channel:
// …` for the same cause. The check that keeps it decided is the
// strings.Count(reason, prefix) == 1 assertion on each face, never a Contains
// (which is true of a stutter as well as of a correct line), plus the ONE
// composition site in channel_prefix.go.
//
// The sibling shapes are deliberately NOT uniform with this one and must not be
// "fixed" into it: pullFailureWords and AttemptDelivery's other errors return a
// BODY (no prefix — their callers compose), and a *Blocker is the only value
// here that speaks on its own.
func (b *Blocker) Report() string {
	if b == nil {
		return ""
	}
	var sb strings.Builder
	switch b.Kind {
	case BlockerKindDirty:
		shown := b.Paths
		if len(shown) > MaxReportedDirtyPaths {
			shown = shown[:MaxReportedDirtyPaths]
		}
		quoted := make([]string, 0, len(shown))
		for _, line := range shown {
			quoted = append(quoted, strconv.Quote(line))
		}
		more := ""
		if len(b.Paths) > len(shown) {
			more = fmt.Sprintf(" and %d more", len(b.Paths)-len(shown))
		}
		fmt.Fprintf(&sb, CloneChannelPrefix+"refusing to pull: the workspace has %d uncommitted path(s) "+
			"(showing at most %d): %s%s. This channel never stashes, commits or cleans the work, and it will "+
			"not pull over it: git would only answer 'cannot pull with rebase: You have unstaged changes' (exit 128)",
			len(b.Paths), MaxReportedDirtyPaths, strings.Join(quoted, ", "), more)
	case BlockerKindMan:
		// THIS KIND'S Words OPEN WITH THE CHANNEL'S OWN SENTENCE (it names the
		// STALE pages) and end with the generator\'s own output verbatim, so the
		// report prints them as the refusal itself and does not append the
		// "the tool's own words:" label — the label would suggest the whole
		// paragraph came from a program, and the sentence naming the pages is this
		// channel's (see man_gate.go). The STEP rides along: it is the generator
		// invocation a reader reproduces.
		fmt.Fprintf(&sb, CloneChannelPrefix+"the %s step refused", b.Kind)
		if step := strings.TrimSpace(b.Step); step != "" {
			fmt.Fprintf(&sb, " (ran: %s)", step)
		}
		if w := strings.TrimSpace(b.Words); w != "" {
			fmt.Fprintf(&sb, "; %s", w)
		}
		return sb.String()
	case BlockerKindTestCompile:
		// THIS KIND'S OWN SENTENCE (it names the command and states that NO test
		// was run) followed by the compiler's own words — see
		// test_compile_gate.go, which owns both halves.
		sb.WriteString(testCompileGateReport(b))
		return sb.String()
	case BlockerKindDiskExhaustion:
		// THIS KIND'S OWN SENTENCE, and it replaces the generic
		// "the tool's own words:" tail rather than decorating it: the words of a
		// filesystem refusal ("no space left on device") are already the least
		// informative part of the fact, and what a reader needs instead is the
		// filesystem's own figures and the reclaim command. The tool's words ride
		// along verbatim underneath, because they are the evidence the class was
		// claimed at all.
		sb.WriteString(diskExhaustionReport(b))
		if w := strings.TrimSpace(b.Words); w != "" {
			fmt.Fprintf(&sb, "; the build's own words: %s", w)
		}
		return sb.String()
	case BlockerKindPull:
		// WHICH STEP AND WHICH COMMAND come first, then WHICH FILES: the three
		// facts a reader needs, in the order they need them. A pull refusal used
		// to be `the pull step refused; the tool's own words: <a tail of the
		// output>` — i.e. the reader was handed git's advice block and had to
		// derive both the command and the file list from it (defect 01a17a4f).
		if step := strings.TrimSpace(b.Step); step != "" {
			fmt.Fprintf(&sb, CloneChannelPrefix+"the %s step refused (ran: %s)", b.Kind, step)
		} else {
			fmt.Fprintf(&sb, CloneChannelPrefix+"the %s step refused", b.Kind)
		}
		if len(b.Paths) > 0 {
			shown := b.Paths
			if len(shown) > MaxReportedDirtyPaths {
				shown = shown[:MaxReportedDirtyPaths]
			}
			more := ""
			if len(b.Paths) > len(shown) {
				more = fmt.Sprintf(" and %d more", len(b.Paths)-len(shown))
			}
			fmt.Fprintf(&sb, "; the conflicted path(s) to open first: %s%s", strings.Join(shown, ", "), more)
		} else {
			// AN EMPTY LIST IS STILL INFORMATION — say which kind of empty
			// (Rule 15: "no conflicted files" and "the question could not be
			// asked" are different facts and must not share a sentence).
			sb.WriteString("; no conflicted path was reported by git before the rebase was reset")
		}
	case BlockerKindEmpty:
		// THIS KIND IS A MEASUREMENT, NOT A REFUSAL BY A TOOL: `rev-list
		// --count` answered 0, which is a true and complete reading. So the
		// words are printed as the outcome itself (the man branch's
		// reasoning, applied one step further), with the command that produced
		// them, and they are NEVER dressed in the pull vocabulary.
		if w := strings.TrimSpace(b.Words); w != "" {
			fmt.Fprintf(&sb, CloneChannelPrefix+"%s", w)
		} else {
			sb.WriteString(CloneChannelPrefix + "nothing to deliver from this workspace")
		}
		// b.Step is the command that produced the count, and the sentence above
		// already quotes it — repeating it here would read as two measurements.
		return sb.String()
	case BlockerKindInheritedDelivery:
		// THE THIRD MEANING OF THE SAME ZERO: the base already CONTAINS this
		// ground's HEAD, so there is nothing to publish — and, unlike the empty
		// kind, there is nothing MISSING either. Printed as the measurement it
		// is, in its own words, because "you produced nothing" (empty) and "what
		// you were handed is already there" (inherited) send a reader to two
		// different places (Rule 15).
		if w := strings.TrimSpace(b.Words); w != "" {
			fmt.Fprintf(&sb, CloneChannelPrefix+"%s", w)
		} else {
			fmt.Fprintf(&sb, CloneChannelPrefix+"%s", deliveryInheritedDeliveryMarker)
		}
		return sb.String()
	case BlockerKindAlreadyDelivered:
		// THE OTHER MEANING OF THE SAME ZERO (row 01a182df): the ground carries
		// nothing ahead of the base BECAUSE this run's own push put it there.
		// Printed as the measurement it is, and NEVER through the empty kind's
		// sentence — "you produced nothing" and "your work is already at the
		// destination" are opposite facts a reader acts on differently (Rule 15).
		if w := strings.TrimSpace(b.Words); w != "" {
			fmt.Fprintf(&sb, CloneChannelPrefix+"%s", w)
		} else {
			fmt.Fprintf(&sb, CloneChannelPrefix+"%s", deliveryAlreadyDeliveredMarker)
		}
		return sb.String()
	default:
		fmt.Fprintf(&sb, CloneChannelPrefix+"the %s step refused", b.Kind)
	}
	if w := strings.TrimSpace(b.Words); w != "" {
		fmt.Fprintf(&sb, "; the tool's own words: %s", w)
	}
	return sb.String()
}

// Error lets a *Blocker travel through the readers that still speak `error`
// (Open, which is not part of the delivery loop) WITHOUT flattening it: a caller
// that wants the structured facts asserts for *Blocker instead of parsing text.
func (b *Blocker) Error() string { return b.Report() }

// Dir is the workspace directory for one task: Root/<task-id>. One function so
// the opener, the reclaimer and the ledger stamp cannot disagree (Rule 14).
func Dir(root, taskID string) string {
	return filepath.Join(root, taskID)
}

// Open resolves the task's workspace: an existing directory is REUSED (a
// previous attempt of this row — its commits may be worth keeping), a missing
// one is CLONED from DevRepo. Either way the workspace is brought up to date
// with one `pull --rebase` before anybody works in it, which is the whole
// point of the channel.
func Open(ctx context.Context, cfg Config) (Result, error) {
	if strings.TrimSpace(cfg.TaskID) == "" {
		return Result{}, errors.New(CloneChannelPrefix + "task id must not be empty")
	}
	if strings.TrimSpace(cfg.DevRepo) == "" {
		return Result{}, errors.New(CloneChannelPrefix + "dev_repo must not be empty")
	}
	if strings.TrimSpace(cfg.Root) == "" {
		return Result{}, errors.New(CloneChannelPrefix + "workspace root must not be empty")
	}
	dir := Dir(cfg.Root, cfg.TaskID)
	res := Result{Dir: dir}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return res, fmt.Errorf(CloneChannelPrefix+"create workspace root %s: %w", cfg.Root, err)
	}
	if isGitWorkTree(dir) {
		res.printf(cfg.Log, "reusing the existing workspace %s", dir)
	} else {
		// A directory that is NOT a work tree (a half-deleted clone, a stale
		// placeholder) is removed first: `git clone` refuses a non-empty
		// target, and leaving it would turn every later attempt of this row
		// into the same failure.
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			res.printf(cfg.Log, "removing the unusable residue at %s before cloning", dir)
			if err := os.RemoveAll(dir); err != nil {
				return res, fmt.Errorf(CloneChannelPrefix+"clear %s: %w", dir, err)
			}
		}
		cloneCtx, cancel := context.WithTimeout(ctx, CloneTimeout)
		defer cancel()
		if out, err := run(cloneCtx, "", gitBinary, "clone", cfg.DevRepo, dir); err != nil {
			return res, fmt.Errorf(CloneChannelPrefix+"git clone %s %s failed: %w (output: %s)",
				cfg.DevRepo, dir, err, tail(out))
		}
		res.printf(cfg.Log, "cloned %s → %s", cfg.DevRepo, dir)
	}
	branch, err := currentBranch(ctx, dir)
	if err != nil {
		return res, err
	}
	// THE GROUND STANDS ON THE INTEGRATION BRANCH.
	//
	// A caller that names a GroundBranch (the pre-2026-11-05 shape, kept for
	// callers that still mint a private address) is put on it; a caller that
	// names NONE — the clone channel's shape since the operator's ruling of
	// 2026-11-05 — stands on the integration branch, and a REUSED workspace is
	// brought back to it. Both are the same question: "which ref does this
	// workspace work on?", and the answer must hold on the reuse arm too, or a
	// workspace left on an older shape's ref would deliver that ref's commits
	// while its row records the integration branch.
	//
	// WHY THE RECLAIM IS NOT PAINTING OVER WORK: the workspace belongs to ONE
	// row, nothing else commits in it, and everything it carries on its own ref
	// is either already on the integration branch or not committed at all (the
	// clean gate below refuses a dirty tree in the latter case, so nothing is
	// ever left behind by a checkout). A checkout that git itself refuses —
	// uncommitted work, a conflicting untracked file — is reported with git's
	// own words rather than forced (Rule 15, and `git checkout` never gets a
	// force spelling here).
	if ground := strings.TrimSpace(cfg.GroundBranch); ground != "" {
		if err := checkOutGroundBranch(ctx, dir, ground); err != nil {
			return res, err
		}
		branch = ground
	} else if base := baseBranchOf(cfg, branch); branch != base {
		if err := standOnBranch(ctx, dir, base); err != nil {
			return res, err
		}
		branch = base
	}
	res.Branch = branch
	// THE SAME GATE AS AttemptDelivery: Open also pulls, so it also refuses a
	// dirty tree. The gate answers with a Blocker (the same value the delivery
	// loop hands an agent), relayed here as an error because Open is the
	// channel's ENTRY step, not one of its attempts: there is no round to give
	// it to yet.
	blk, err := cleanGate(ctx, dir)
	if err != nil {
		return res, err
	}
	if blk != nil {
		return res, blk
	}
	// THE INTEGRATION BRANCH IS THE REBASE POINT, never the ground ref: the
	// workspace integrates what landed on the source's branch while its own
	// commits stay on its own ref (see Config.BaseBranch). Measured 2026-10-27
	// on git 2.43.0: `git pull --rebase origin task/<id>` on a workspace whose
	// ground ref the server does not have yet fails outright ("fatal: couldn't
	// find remote ref task/<id>", exit 1) — the legacy spelling only worked
	// because the clone stood on a branch the source already carried.
	if o, err := pullRebase(ctx, dir, baseBranchOf(cfg, branch)); err != nil {
		// Open still speaks `error` (it is not part of the delivery loop — see
		// Blocker.Error), so the SAME evidence composition is used here: a pull
		// that refuses during the clone says which command, which paths and what
		// became of the commit it stopped on, rather than a lone id.
		return res, errors.New(CloneChannelPrefix + pullFailureWords(ctx, dir, o))
	}
	res.printf(cfg.Log, "workspace ready at %s on branch %s", dir, branch)
	return res, nil
}

// baseBranchOf is the branch this workspace REBASES onto: the caller's declared
// BaseBranch, the caller's Branch override (legacy callers set only that), or
// the package default. One function so Open and AttemptDelivery cannot disagree
// about the integration point (Rules 13/14).
//
// A GROUND branch is never its own base: `pull --rebase origin <ground>` is the
// spelling that fails when the server does not carry the ref yet (measured
// 2026-10-27), and even when it succeeds it integrates the row's own history
// instead of what landed on the integration branch — the exact conflation this
// epic removes.
func baseBranchOf(cfg Config, standOn string) string {
	if base := strings.TrimSpace(cfg.BaseBranch); base != "" {
		return base
	}
	if b := strings.TrimSpace(cfg.Branch); b != "" && b != strings.TrimSpace(cfg.GroundBranch) {
		return b
	}
	return DefaultBranch
}

// standOnBranch puts the workspace on an EXISTING branch (the integration
// branch), and it is the counterpart of checkOutGroundBranch: that one MINTS a
// ref when it is missing, this one never does. A branch that does not exist is
// git's refusal to report, not a name to create — a missing integration branch
// means the clone did not come from where the caller said it did.
//
// It never forces (no `-f`/`--force`: an uncommitted change is the workspace's
// own work and git's refusal says so), and it re-measures afterwards so the
// caller's `branch` is a FACT about the directory rather than the name it asked
// for.
func standOnBranch(ctx context.Context, dir, branch string) error {
	current, err := currentBranch(ctx, dir)
	if err != nil {
		return err
	}
	if current == branch {
		return nil
	}
	if out, err := runCommand(ctx, dir, gitBinary, checkoutVerb, branch); err != nil {
		return fmt.Errorf(CloneChannelPrefix+"the workspace %s cannot stand on %s: %w (output: %s)",
			dir, branch, err, tail(out))
	}
	return nil
}

// checkOutGroundBranch puts the workspace on the row's own branch, creating it
// at the current HEAD when the ref does not exist yet.
//
// WHY `checkout -b` AND NOT `checkout --track origin/<name>`: the ground ref is
// minted BY this channel (it exists on the server only after a previous round
// of this row delivered it), so the ordinary fresh workspace has no upstream to
// track. Creating it at the cloned HEAD is the whole contract: the row starts on
// top of the integration branch and its commits accumulate on its own ref.
//
// It never forces: an existing ref is checked out as it is (a reused workspace
// keeps the row's own commits), and a checkout that fails is reported with git's
// own words rather than papered over (Rule 15).
func checkOutGroundBranch(ctx context.Context, dir, ground string) error {
	current, err := currentBranch(ctx, dir)
	if err != nil {
		return err
	}
	if current == ground {
		return nil
	}
	// Already on the branch → nothing to do (above). The ref may still EXIST
	// while HEAD is elsewhere: a reused workspace whose round moved away.
	create := createBranchFlag
	if _, err := runCommand(ctx, dir, gitBinary, "rev-parse", "--verify", "--quiet",
		refsHeadsPrefix+ground); err == nil {
		create = ""
	}
	args := []string{checkoutVerb}
	if create != "" {
		args = append(args, create)
	}
	args = append(args, ground)
	if out, err := runCommand(ctx, dir, gitBinary, args...); err != nil {
		return fmt.Errorf(CloneChannelPrefix+"the workspace %s cannot stand on its ledger address %s: %w (output: %s)",
			dir, ground, err, tail(out))
	}
	return nil
}

// AttemptDelivery is ONE delivery attempt: the deterministic steps of
// clean → pull --rebase → the declared build → push, run ONCE.
//
// It is NOT a loop over attempts (that is the caller's job: MaxDeliveryRounds
// rounds, each handing its Blocker to one agent round) and it is not a decision
// site — it ANSWERS with the fact that stopped it, in the tool's own words, so
// an agent can decide what to do. Nothing here commits, stashes or cleans: the
// workspace belongs to whoever worked in it.
//
// The ONE retry that survives inside an attempt is the push RACE
// (MaxPushAttempts, 0.5s/2s/8s with jitter): origin moving between our pull and
// our push is a clock, not a decision — no agent can act on it, only waiting and
// pulling again can.
//
// The three outcomes are distinguishable BY SHAPE, so no caller parses text:
//
//	blocked   — (res, blk, nil): a step refused; blk carries the words.
//	delivered — (res, nil, nil): the push landed.
//	broken    — (res, nil, err): not about the work at all (no declared build, a
//	            git that cannot read the branch, a push that was NOT a race).
func AttemptDelivery(ctx context.Context, cfg Config, dir string) (Result, *Blocker, error) {
	res := Result{Dir: dir}
	branch := strings.TrimSpace(cfg.Branch)
	if g := strings.TrimSpace(cfg.GroundBranch); g != "" {
		// THE LEDGER ADDRESS WINS when the caller named one, and it is what
		// `Result.Branch` reports: the branch this workspace STANDS on (Open put
		// it there) and what the claim records as `deliverable.branch`, so
		// the branch→row lookup (then `gopi task for-branch`, since retired; now
		// `gopi task show --branch`) maps it back to exactly one row. It is NOT what
		// the push addresses — see the ADDRESS-vs-DESTINATION comment below, and
		// publishGround's own: the delivery's one destination is `base`.
		branch = g
	}
	if branch == "" {
		b, err := currentBranch(ctx, dir)
		if err != nil {
			return res, nil, err
		}
		branch = b
	}
	res.Branch = branch
	// THE INTEGRATION POINT IS NOT THE ADDRESS (Config.BaseBranch): the pull
	// integrates what landed on the source's branch, while `branch` is what the
	// workspace STANDS on and what the claim records. One name for both is the
	// conflation this epic removes.
	base := baseBranchOf(cfg, branch)
	// THE ADDRESS AND THE DESTINATION ARE TWO DIFFERENT QUESTIONS, and ONE push
	// answers only the second:
	//   · `deliverable.branch` names the row's OWN ground (`task/<id>`) — that is
	//     what the stamp fix is for, so `for-branch` maps the ref back to exactly
	//     one row instead of to the hundreds sharing `master`;
	//   · the PUSH's ONE destination is the INTEGRATION branch, because that is
	//     where the merge paths and every user of a clone ground look for the
	//     work (the contract three end-to-end gates pin: "the source repo's
	//     master did not move").
	// THE ADDRESS IS NOT PUBLISHED, on purpose: the ground ref stays LOCAL, and
	// the delivery proof measures a gone ground against the integration branch it
	// knows the channel pushes (internal/tools' deliveryProofIntegrationDestination,
	// whose own comment cites "publishGround's ONE refspec"). Publishing `task/<id>`
	// was tried and reverted (9ab17237e): measured 2026-10-28, zpeer carried 15
	// `task/*` branches, every one already merged into master and never deleted —
	// a ref no merge path consumes piles up one dead branch per task, forever.
	build := strings.TrimSpace(cfg.Build)
	if build == "" {
		// Refuse BEFORE touching git: a loop that pushes what it never built
		// is the one thing the channel exists to prevent, and a discovered
		// `go build` would be the hard-coding Rule 12 bans. NOT a blocker: no
		// agent round can conjure a declaration that does not exist.
		return res, nil, errors.New(CloneChannelPrefix + "no build command is declared for this repo " +
			"(settings repos.<path>.build) — refusing to push a workspace that was never built")
	}
	timeout := cfg.BuildTimeout
	if timeout <= 0 {
		timeout = DefaultBuildTimeout
	}
	// THE HEAD-DIRECTORY ASSERTION FIRST, AND BEFORE ANY COUNT (measured defect,
	// 2026-10-29): every git step below is `git -C <dir>` and GIT WALKS UP, so a
	// directory that is not a work tree of its own makes every count a fact
	// about the ENCLOSING repository while the refusal names `dir`. This gate
	// asks git where the HEAD of `dir` actually lives and refuses, naming both
	// paths, when it is somewhere else — so the two gates below (the anchor,
	// which takes its own counts, and the empty gate) never measure a directory
	// this leg is not publishing from.
	if err := deliveryHeadDirGate(ctx, cfg, dir); err != nil {
		// Written directly rather than through printf: the error already opens
		// with the channel's own prefix, and printf would add it a second time.
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "%v\n", err)
		}
		return res, nil, err
	}
	// THE ANCHOR GATE, ONCE PER ATTEMPT, BEFORE ANY GIT STEP (measured defect
	// 2026-10-27, row 01a17dee): the leg published from a directory the run had
	// never committed in — its own ground held 0 commits ahead and 0 dirty
	// lines, while the two real commits sat in ANOTHER row's ground. Publishing
	// from the wrong directory is not a fact about the work in THIS one, and no
	// agent round standing here can repair it, so it is an ERROR (never a
	// blocker) that names BOTH paths. Empty Worktree = no question to ask.
	if err := deliveryAnchorGate(ctx, cfg, dir, base); err != nil {
		// Written directly rather than through printf: the error already opens
		// with the channel's own prefix, and printf would add it a second time —
		// a doubly-prefixed sentence reads like two separate failures.
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "%v\n", err)
		}
		return res, nil, err
	}
	// THE TEST FENCE, ONCE PER ATTEMPT — ahead of the loop's first git step, so a
	// refused push never even pulls (a test that may not deliver must not move the
	// ground either). A refusal is a BLOCKER, not an error: the workspace is fine,
	// the DELIVERY is not permitted (Rule 15 — say which, never a generic failure).
	if why := pushFenceRefusal(ctx, dir); why != "" {
		res.printf(cfg.Log, "%s", why)
		return res, &Blocker{Kind: BlockerKindPush, Words: why}, nil
	}
	for attempt := 1; attempt <= MaxPushAttempts; attempt++ {
		// CLEAN BEFORE PULL (each attempt): checked here rather than once
		// outside the loop because a BUILD can dirty the tree too (a tracked
		// file the declared command rewrites) — the gate belongs immediately
		// in front of the git step that needs a clean one.
		// A refusal here spends no attempt: no pull ran.
		blk, err := cleanGate(ctx, dir)
		if err != nil {
			return res, nil, err
		}
		if blk != nil {
			return res, blk, nil
		}
		// NOTHING TO DELIVER — its own kind, decided HERE: after the clean gate
		// (a dirty tree is a different fact) and BEFORE `pull --rebase`. That
		// order is the defect fix: an empty workspace used to reach the pull,
		// whose collision was then reported as the reason, and the operator was
		// sent chasing a network/conflict ghost while the true fact was "nothing
		// here to publish" (row 01a17dee, measured). BlockerKindEmpty cannot
		// collapse into the pull vocabulary by construction.
		emptyBlk, eerr := deliveryEmptyGate(ctx, cfg, dir, base)
		if eerr != nil {
			return res, nil, eerr
		}
		if emptyBlk != nil {
			res.printf(cfg.Log, "%s", emptyBlk.Words)
			return res, emptyBlk, nil
		}
		res.Attempts = attempt
		res.printf(cfg.Log, "attempt %d/%d: pull --rebase origin %s", attempt, MaxPushAttempts, base)
		if o, err := pullRebase(ctx, dir, base); err != nil {
			// THE EVIDENCE, NOT A TAIL OF IT. pullRebase already aborted the
			// half-done rebase (a workspace left mid-rebase would poison every
			// later attempt) and captured the conflicted paths BEFORE doing so;
			// pullFailureWords puts git's own account, those paths, and each
			// `could not apply <id>`'s measured resolvability into the words —
			// and leaves the abort's output OUT of them, because cleanup text is
			// not the refusal (measured defect 01a17a4f: the abort's four hint
			// lines crowded out `CONFLICT … in <path>` under a six-line tail).
			return res, &Blocker{
				Kind:  BlockerKindPull,
				Step:  pullStepCommand + base,
				Words: pullFailureWords(ctx, dir, o),
				Paths: o.UnmergedPaths,
			}, nil
		}
		// THE CENSUS CITATION LEG IS RETIRED (2026-11-05, operator ruling). It sat
		// here, after `pull --rebase` and before the declared build, and refused a
		// delivery whose census table cited a revision the destination could not
		// show. The whole documentation-citation family went in 78bdf05a — the
		// three generators (config-anchor-census.go, citation-census.go,
		// citation-selector-census.go), the three tables and the gates driving
		// them — and this call was the LAST runtime leg still shelling `go run` at
		// a program that no longer exists. Nothing was restored: the ruling's
		// direction is deletion, so the leg, its gate (census_gate.go), its
		// re-derive leg (census_redrive.go) and their tests are gone together.
		res.printf(cfg.Log, "attempt %d/%d: build via the declared command", attempt, MaxPushAttempts)
		out, err := runBuild(ctx, dir, build, timeout)
		if err != nil {
			// TWO OUTCOMES, TWO KINDS (disk_exhaustion.go): a filesystem that
			// refused the write never linked the revision, so it is not a
			// statement about the branch and must not wear the build kind's
			// wording — that conflation sent round after round of agents looking
			// for a code defect on a full partition.
			return res, buildBlocker(dir, out), nil
		}
		// THE MAN SOURCES, AGAINST THE BINARY THIS BRANCH JUST BUILT (man_gate.go).
		// It sits AFTER the declared build because the binary IS the fact source:
		// the generated SYNOPSIS rows are copied from `gopi <verb> -h`, so checking
		// before the build would compare the help pages of a binary this branch did
		// not produce and call genuine drift "up to date" — the exact false green
		// this step exists to prevent. Its absence is an abstention printed by the
		// gate itself, never a silent pass.
		if blk := manCensusGate(ctx, cfg, dir); blk != nil {
			return res, blk, nil
		}
		// THE TEST TREES, ON THE TIP THIS ATTEMPT PUBLISHES (test_compile_gate.go).
		// It sits AFTER the declared build — because the man gate above reads
		// `bin/gopi`, the binary THIS branch built, and this leg must not stand
		// between the build and the step that consumes its output — and BEFORE
		// the push, which is the whole point: the refusal must land while the ref
		// is still here. WHY IT EXISTS AT ALL: `go build` does not compile
		// `_test.go`, so a branch whose test tree does not compile is green to
		// every build-shaped measurement (measured 2026-11-03: commit 928740fe
		// landed a `type result` collision and `cmd/agent`'s test package was
		// uncompilable for 44 minutes while every guard stayed green — the legs
		// that catch it are wired to the MERGE path, which master no longer
		// travels). Its absence is an abstention printed by the gate itself,
		// never a silent pass (Rule 15).
		if blk := testCompileGate(ctx, cfg, dir); blk != nil {
			return res, blk, nil
		}
		res.printf(cfg.Log, "attempt %d/%d: push", attempt, MaxPushAttempts)
		// ONE DESTINATION, ONE PUSH: the integration branch, which is the
		// contract (see the ADDRESS-vs-DESTINATION comment above for why the
		// row's own `task/<id>` is deliberately NOT published — 9ab17237e).
		pushOut, pushErr := publishGround(ctx, dir, base)
		if pushErr == nil {
			// `push` EXITING 0 IS NOT THE DELIVERY — THE PEER CARRYING THE COMMIT
			// IS (measured defect, row 01a1a647, 2026-11-04). This arm used to
			// print `push accepted` on nothing but the child's exit status, so a
			// push git called successful while the receiving side did not keep
			// the ref — a hook that rejects by restoring, a mirror, a remote
			// whose update is discarded after the report — was written down as a
			// DELIVERED run. PROJECT.md Rule 6c reads that very line as the
			// evidence of delivery, so the channel was manufacturing the
			// evidence instead of taking it: the row closed `completed` with
			// `clone-channel: attempt 2/3: push accepted` as its only witness
			// while `git ls-remote --heads origin` carried no such commit.
			//
			// The measurement is the same read the empty gate already trusts —
			// `ls-remote` of the ref this push ADDRESSED (`base`, never the
			// ground branch: see the ADDRESS-vs-DESTINATION note above) — taken
			// HERE, in the leg, and not in the close-out: the ground is
			// RECLAIMED the moment the delivery returns (cmd/agent's finish()),
			// so after that point the question cannot be asked at all.
			//
			// Both answers that are not "the peer has it" are ERRORS rather than
			// blockers: retrying the inner loop would only repeat the same
			// transport, and the brake's vocabulary ("the remote kept moving
			// first") would be a story about a race nobody ran.
			if landed, why := pushLandedOnPeer(ctx, dir, base); !landed {
				res.printf(cfg.Log, "attempt %d/%d: push reported success but the peer does not carry it — %s",
					attempt, MaxPushAttempts, why)
				return res, nil, fmt.Errorf(CloneChannelPrefix+"push reported success on attempt %d and the peer does not carry the commit (%s) (output: %s)",
					attempt, why, tail(pushOut))
			}
			res.Pushed = true
			// THE DETAIL NAMES THE DESTINATION, not the address: `branch` is what
			// the workspace STANDS on (task/<id>), while what the push moved is
			// `base`. Printing the address here would tell an operator the row's
			// own ref landed when the integration branch is what advanced.
			res.Detail = fmt.Sprintf("pushed %s to origin after %d attempt(s)", base, attempt)
			res.printf(cfg.Log, "attempt %d/%d: push accepted", attempt, MaxPushAttempts)
			return res, nil, nil
		}
		// THE REFUSAL IS CLASSIFIED AGAINST THE REF THE PUSH ADDRESSED — `base`,
		// never `branch` (measured 2026-10-27 while wiring GroundBranch): those
		// two are different names on a ground branch, and asking about the
		// ADDRESS answers a question nobody pushed. Post-fix the ground stands on
		// task/<id>, so `origin/task/<id>` does not exist during the first
		// delivery and pushRefused returned "the remote branch does not exist" —
		// a genuine race (someone landed on master first) would then be reported
		// as a hard error and the brake would never retry.
		refused, why := pushRefused(ctx, dir, base)
		if !refused {
			// NOT a race: a bad credential, a dead remote, a hook failure.
			// Retrying it would only burn the brake — say what happened.
			return res, nil, fmt.Errorf(CloneChannelPrefix+"push failed on attempt %d and the remote did not move (%s): %w (output: %s)",
				attempt, why, pushErr, tail(pushOut))
		}
		res.printf(cfg.Log, "attempt %d/%d: REFUSED — %s", attempt, MaxPushAttempts, why)
		if attempt == MaxPushAttempts {
			res.Exhausted = true
			res.Detail = fmt.Sprintf("push was REFUSED %d times (the remote kept moving first); the workspace %s is KEPT — nothing was deleted",
				MaxPushAttempts, dir)
			if err := writeBrakeRecord(dir, res); err != nil {
				res.printf(cfg.Log, "brake record could not be written (%v) — the report above still stands", err)
			}
			// THE PUSH BLOCKER carries GIT'S OWN refusal, not this channel's
			// summary of it: git is the tool that said no.
			return res, &Blocker{Kind: BlockerKindPush, Words: tail(pushOut)}, nil
		}
		wait := backoff(attempt)
		res.printf(cfg.Log, "attempt %d/%d: waiting %s before pulling again", attempt, MaxPushAttempts, wait)
		select {
		case <-ctx.Done():
			return res, nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	// Unreachable: the step above returns on the last attempt.
	res.Exhausted = true
	res.Detail = fmt.Sprintf("push was REFUSED %d times (the remote kept moving first); the workspace %s is KEPT — nothing was deleted",
		MaxPushAttempts, dir)
	return res, &Blocker{Kind: BlockerKindPush, Words: res.Detail}, nil
}

// Reclaim deletes the task's workspace after a successful push: the code is at
// origin, so keeping the clone would only leave a stale copy behind. It returns
// (removed, error) and REFUSES nothing: a failure is reported, never fatal to
// the run whose deliverable already landed.
func Reclaim(dir string) (bool, error) {
	if strings.TrimSpace(dir) == "" {
		return false, errors.New(CloneChannelPrefix + "refusing to reclaim an empty path")
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

// backoff is the wait before attempt+1: BackoffBase × BackoffFactor^(attempt-1),
// jittered by ±BackoffJitterRatio.
func backoff(attempt int) time.Duration {
	d := BackoffBase
	for i := 1; i < attempt; i++ {
		d *= BackoffFactor
	}
	span := float64(d) * BackoffJitterRatio
	delta := (rand.Float64()*2 - 1) * span //nolint:gosec // jitter, not a secret
	return time.Duration(float64(d) + delta)
}

// cleanGate refuses to pull over uncommitted work — in the channel's own
// words, BEFORE git gets to say "cannot pull with rebase: You have unstaged
// changes" (exit 128) and be mistaken for the channel's reason.
//
// It answers with a *Blocker (nil when the tree is clean) rather than an error,
// because a dirty workspace is the fact the next agent round is handed: the
// Blocker's Words are `git status --porcelain`'s listing VERBATIM and its Paths
// are parsed from those same words, so the two can never disagree.
//
// An error is reserved for "the status could not be read at all" — that is not
// a fact about the work.
//
// It is READ-ONLY by construction: `git status --porcelain` is the only command
// it runs, so the operator's work cannot be stashed, committed or cleaned away
// by a step that was only supposed to check (Rule 15 is about saying what is
// wrong, not about quietly fixing the operator's tree).
func cleanGate(ctx context.Context, dir string) (*Blocker, error) {
	out, err := runCommand(ctx, dir, gitBinary, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf(CloneChannelPrefix+"read the workspace status in %s: %w (output: %s)", dir, err, tail(out))
	}
	dirty := dirtyStatusLines(out)
	if len(dirty) == 0 {
		return nil, nil
	}
	return &Blocker{Kind: BlockerKindDirty, Words: out, Paths: dirty}, nil
}

// uncommittedChanges lists the `git status --porcelain` entries that would stop
// a rebase pull, in git's own two-column spelling (so ' M' vs 'M ' still says
// which side of the tree is dirty).
func uncommittedChanges(ctx context.Context, dir string) ([]string, error) {
	out, err := runCommand(ctx, dir, gitBinary, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf(CloneChannelPrefix+"read the workspace status in %s: %w (output: %s)", dir, err, tail(out))
	}
	return dirtyStatusLines(out), nil
}

// dirtyStatusLines reads `git status --porcelain` output into the entries that
// would stop a rebase pull. Untracked entries ('??') are NOT returned: a rebase
// tolerates them.
func dirtyStatusLines(out string) []string {
	var dirty []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			continue
		}
		dirty = append(dirty, strings.TrimRight(line, " "))
	}
	return dirty
}

// pullRebase runs the pull and, on failure, leaves the workspace OFF the
// half-done rebase (a workspace left mid-rebase would poison every later
// attempt of this row) while REPORTING both facts separately.
//
// WHY THE OUTPUTS ARE NOW APART (measured 2026-10-27, defect 01a17a4f): the
// abort's output used to be appended to the pull's (`out + aborted`) and the
// caller kept the tail of that concatenation. On git 2.43.0 the pull's own
// account ends with a four-line `hint:` block, so a tail of six lines kept the
// advice and DISCARDED `CONFLICT (content): Merge conflict in <path>` — the one
// line naming the file. A refusal that names no file leaves a reader with a
// commit id and nothing to open. So the pull's output is what the caller
// reports, and the abort's output is returned as its own value: cleanup that
// fails is still said out loud (Rule 15), never merged into evidence.
//
// The conflicted-path list is captured HERE, BEFORE the abort, and that order is
// measured, not assumed: `git diff --name-only --diff-filter=U` prints the
// unmerged paths only while the rebase is in progress and prints NOTHING after
// `git rebase --abort` (git 2.43.0, fixture in
// /home/user/.gopi/sessions/01a17a4f-014f-70e6-bd00-729c4925525a/evidence/).
// Abort-then-ask would therefore always answer "no conflicted files".
func pullRebase(ctx context.Context, dir, branch string) (pullOutcome, error) {
	o := pullOutcome{
		PullCmd: pullStepCommand + branch,
	}
	out, err := runCommand(ctx, dir, gitBinary, gitPullVerb, gitPullRebaseFlag, "origin", branch)
	o.Output = out
	o.Err = err
	if err == nil {
		return o, nil
	}
	// BEFORE the abort: the rebase is the only state in which git reports which
	// paths are unmerged.
	o.UnmergedPaths, o.UnmergedErr = unmergedPaths(ctx, dir)
	// NO `git status` here. The line that used to sit on this address
	// (`o.UnmergedStatus, _ = runCommand(... "status", "--porcelain")`,
	// introduced by e523b19) DISCARDED the call's error with `_` and wrote its
	// output into a field that nothing read: a silent failure whose value was
	// dead too (stylegate ignored-error, ceiling 0). Both halves are gone — the
	// field went with it — because the evidence a refused pull reports is
	// already complete without it: the conflicted paths (unmergedPaths, whose
	// error IS propagated), git's own words (o.Output), and the commit it
	// stopped on. A future contributor who wants the working-tree status on
	// this path must PROPAGATE its error (`o.Status, o.StatusErr = ...`) or
	// state a per-site reason; a bare `_` on this line is what put the gate
	// red.
	// A CONFLICT leaves the tree mid-rebase: abort it, then try the one thing
	// git itself offers for the same two tips. A REBASE conflict is a fact about
	// replaying this branch's commits one at a time; it is NOT evidence that the
	// content is in dispute (a commit plus its own revert conflicts with a base
	// that moved the line, while the branch's NET change is nothing). The merge
	// integrates the two TIPS, which is the question the delivery actually asks.
	// See pullMergeFallback for the measurement and for what it refuses to do
	// (a merge that also conflicts is aborted, not resolved).
	aborted, aerr := runCommand(ctx, dir, gitBinary, gitRebaseVerb, gitRebaseAbortFlag)
	o.AbortOutput = aborted
	o.AbortErr = aerr
	if aerr != nil {
		// The workspace may still be mid-rebase; running the fallback on top of
		// that would stack two in-progress operations, so the refusal stands and
		// says why (Rule 15).
		return o, err
	}
	merge := pullMergeFallback(ctx, dir, branch)
	o.MergeCmd = merge.MergeCmd
	o.MergeOutput = merge.MergeOutput
	o.MergeErr = merge.MergeErr
	o.MergePaths = merge.MergePaths
	o.MergePathsErr = merge.MergePathsErr
	o.MergeAbortErr = merge.MergeAbortErr
	o.MergeApplied = merge.MergeApplied
	if merge.MergeApplied {
		// THE DELIVERY CONTINUES: the two tips are integrated and committed, so
		// the step that refused has been answered by a legal integration. The
		// pull's own failure is still REPORTED (o.Err, o.Output) — nothing is
		// rewritten here — but it no longer stops the attempt.
		return o, nil
	}
	return o, err
}

// pullOutcome is ONE refused (or successful) pull: the pull's own account, the
// conflicted paths read BEFORE the abort, and the channel's cleanup step as a
// separate record. They are separate on purpose — the defect this shape exists
// for was a concatenation, not a truncation.
type pullOutcome struct {
	PullCmd       string
	Output        string
	Err           error
	UnmergedPaths []string
	UnmergedErr   error
	AbortOutput   string
	AbortErr      error

	// THE FALLBACK'S OWN RECORD (row 01a1a87b). A rebase conflict is a fact
	// about REPLAYING COMMITS, not about the content being in dispute: a branch
	// whose net change to a line is nothing conflicts with a base that moved
	// that line, and the same two tips merge without a conflict. So after the
	// rebase is aborted this leg tries the merge, and the merge's answer is
	// recorded HERE rather than folded into the pull's fields — a reader must be
	// able to tell "the rebase refused" from "the rebase refused AND the merge
	// also refused" (Rule 15).
	MergeCmd      string
	MergeOutput   string
	MergeErr      error
	MergePaths    []string
	MergePathsErr error
	MergeAbortErr error
	// MergeApplied records the ONE outcome that lets the delivery continue: the
	// merge integrated the two tips and committed the result.
	MergeApplied bool
}

// pullMergeFallback integrates the SAME two tips with a merge after a rebase
// refused, and reports whether it landed. It is the fallback `git pull --rebase`
// has no setting for, and the reason it is sound rather than a "paper over it":
//
//	rebase replays this branch's commits ONE AT A TIME onto the new base, so a
//	commit that edits a line and a later commit that reverts it both conflict
//	with a base that moved that line — although the branch's NET change to that
//	line is nothing.
//	merge integrates the two TIPS, so that same branch merges cleanly.
//
// Measured on this host, fixture = base + `work: edit b` + `work: revert b`
// against `master moves b`:
//
//	git rebase master → CONFLICT (content): Merge conflict in f.txt
//	git merge  master → Merge made by the 'ort' strategy. 1 file changed
//
// WHAT IT REFUSES TO DO: decide a real dispute. When the merge conflicts too,
// the conflicted paths are captured and the merge is ABORTED — the workspace is
// left on its own HEAD, exactly as the aborted rebase leaves it, because a tree
// left mid-merge poisons every later attempt of the row. The content decision
// belongs to the agent that reads the blocker, never to this loop.
//
// It runs ONLY after a refused, aborted rebase, so the ordinary path never
// reaches it and its cost on a clean delivery is zero (arm ③ of
// pull_merge_fallback_test.go pins the linear history that would be lost if the
// fallback ever ran unconditionally).
func pullMergeFallback(ctx context.Context, dir, branch string) pullOutcome {
	o := pullOutcome{MergeCmd: mergeStepCommand + branch}
	// The merge names the TRACKING REF the refused pull just updated
	// (`origin/<branch>`, refreshed by `pull --rebase`'s own fetch), so this step
	// performs no second fetch and cannot integrate an older tip than the one the
	// rebase refused on.
	out, err := runCommand(ctx, dir, gitBinary, gitMergeVerb, gitMergeNoEditFlag, originRemoteName+"/"+branch)
	o.MergeOutput = out
	o.MergeErr = err
	if err == nil {
		o.MergeApplied = true
		return o
	}
	// WHICH FILES, read while the merge is still in progress — the same order
	// pullRebase uses for the rebase, for the same measured reason: git reports
	// the unmerged paths only until the operation is aborted.
	o.MergePaths, o.MergePathsErr = unmergedPaths(ctx, dir)
	if _, aerr := runCommand(ctx, dir, gitBinary, gitMergeVerb, gitMergeAbortFlag); aerr != nil {
		o.MergeAbortErr = aerr
	}
	return o
}

// unmergedPaths lists the paths git reports as UNMERGED right now, read from
// `git diff --name-only --diff-filter=U` (the same spelling the merge lane uses
// for the same question — cmd/agent/merge_union.go). Called only while a rebase
// is in progress; see pullRebase for why the order is load-bearing.
func unmergedPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := runCommand(ctx, dir, gitBinary, gitDiffVerb, unmergedNameOnlyFlag, unmergedNameOnlyArg)
	if err != nil {
		return nil, fmt.Errorf(CloneChannelPrefix+"read the unmerged paths in %s: %w (output: %s)", dir, err, tail(out))
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if p := strings.TrimSpace(strings.TrimRight(line, "\r")); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// pullFailureWords composes CONFLICTS-PRESENT evidence into the words a reader
// gets. It keeps the "tool's own output, verbatim" contract (Rule 15) by
// QUOTING git rather than paraphrasing it, and adds the three facts the raw
// output does not answer on its own:
//
//  1. THE CONFLICTED PATHS — `git diff --name-only --diff-filter=U`, read
//     before the abort. This is the line a reader acts on ("open that file");
//     it survives even when git's own output was crowded out.
//  2. GIT'S CONFLICT SUBJECT — every non-hint line of the pull's output, in
//     git's order and wording (`Rebasing …`, `CONFLICT (content): Merge
//     conflict in <path>`, `error: could not apply <id>...`). The `hint:`
//     block is dropped: it is git's advice about resolving by hand, and this
//     channel has already aborted the rebase.
//  3. THE AND WHAT HAPPENED TO IT — `git cat-file -t <id>` RUN NOW, per id.
//     An id that resolves is a fact the reader can look up; an id that does not
//     is SAID to not resolve, which is the state the shipped row was in when it
//     quoted a commit no repository carries.
//
// Nothing here rewrites git's words: the hints are dropped and git's lines are
// joined with pullBodyLineJoiner, so what remains is exactly what git printed.
func pullFailureWords(ctx context.Context, dir string, o pullOutcome) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%s refused (exit: %s)", o.PullCmd, pullExitReason(o.Err)))
	if body := pullBodyLines(o.Output); len(body) > 0 {
		parts = append(parts, strings.Join(body, pullBodyLineJoiner))
	}
	if len(o.UnmergedPaths) > 0 {
		parts = append(parts, fmt.Sprintf("conflicted paths (git %s %s %s): %s",
			gitDiffVerb, unmergedNameOnlyFlag, unmergedNameOnlyArg, joinedConflictedPaths(o.UnmergedPaths)))
	} else {
		// An EMPTY list is a measurement too, and it must not read as "there were
		// no conflicts" when the question could not be asked: say which.
		parts = append(parts, fmt.Sprintf("conflicted paths (git %s %s %s): none reported%s",
			gitDiffVerb, unmergedNameOnlyFlag, unmergedNameOnlyArg, unmergedEmptyNote(o)))
	}
	// THE FALLBACK'S OWN ANSWER, when one was tried (row 01a1a87b): a refusal
	// that silently dropped it would tell a reader the rebase refused and stop
	// there — hiding that a legal integration was attempted and that IT is what
	// refused last (Rule 15). Only the merge's failure arms are reported here;
	// a merge that SUCCEEDED never reaches this function (the delivery
	// continues), so its words cannot be mistaken for a failure.
	if o.MergeErr != nil {
		parts = append(parts, fmt.Sprintf("%s refused (exit: %s)", o.MergeCmd, pullExitReason(o.MergeErr)))
		if body := pullBodyLines(o.MergeOutput); len(body) > 0 {
			parts = append(parts, strings.Join(body, pullBodyLineJoiner))
		}
		if len(o.MergePaths) > 0 {
			parts = append(parts, fmt.Sprintf("the merge's conflicted paths (git %s %s %s): %s",
				gitDiffVerb, unmergedNameOnlyFlag, unmergedNameOnlyArg, joinedConflictedPaths(o.MergePaths)))
		} else if o.MergePathsErr != nil {
			parts = append(parts, fmt.Sprintf("the merge's conflicted paths (git %s %s %s): %v",
				gitDiffVerb, unmergedNameOnlyFlag, unmergedNameOnlyArg, o.MergePathsErr))
		}
		if o.MergeAbortErr != nil {
			parts = append(parts, fmt.Sprintf("%s %s also failed: %v (the workspace may still be mid-merge)",
				gitMergeVerb, gitMergeAbortFlag, o.MergeAbortErr))
		}
	}
	for _, id := range measuredConflictCommits(o.Output) {
		parts = append(parts, measureCommitID(ctx, dir, id))
	}
	if o.AbortErr != nil {
		parts = append(parts, fmt.Sprintf("%s %s also failed: %v (the workspace may still be mid-rebase)",
			gitRebaseVerb, gitRebaseAbortFlag, o.AbortErr))
	}
	return strings.Join(parts, pullPartJoiner)
}

// joinedConflictedPaths names at most MaxReportedDirtyPaths conflicted paths and
// COUNTS the rest, exactly like the dirty gate's report: a rebase across a large
// tree can conflict in hundreds of files, and the reason column must stay a
// sentence a reader can finish (the same constant, because it is the same
// question — how many paths does a refusal name before it summarizes).
func joinedConflictedPaths(paths []string) string {
	shown := paths
	if len(shown) > MaxReportedDirtyPaths {
		shown = shown[:MaxReportedDirtyPaths]
	}
	out := strings.Join(shown, ", ")
	if rest := len(paths) - len(shown); rest > 0 {
		out = fmt.Sprintf("%s and %d more", out, rest)
	}
	return out
}

// pullExitReason renders the pull's failure without inventing a status code:
// an *exec.ExitError carries one, anything else carries its own message.
func pullExitReason(err error) string {
	if err == nil {
		return "none"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return strconv.Itoa(ee.ExitCode())
	}
	return err.Error()
}

// unmergedEmptyNote explains an empty conflicted-path list, because "" has two
// very different causes and they must not share a sentence (Rule 15).
func unmergedEmptyNote(o pullOutcome) string {
	if o.UnmergedErr != nil {
		return " (" + o.UnmergedErr.Error() + ")"
	}
	return " — the rebase had already been reset, or the refusal was not a content conflict"
}

// pullBodyLines keeps git's own lines from the pull's output, in git's ORDER
// and WORDING, bounded by MaxPullWordsLines. The `hint:` block is dropped (see
// gitHintLinePrefix). Keeping the FIRST lines rather than the last is the whole
// inversion: the criteria are printed before the advice.
func pullBodyLines(s string) []string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, gitHintLinePrefix) {
			continue
		}
		kept = append(kept, trimmed)
		if len(kept) == MaxPullWordsLines {
			kept = append(kept, fmt.Sprintf("…and more (first %d lines kept)", MaxPullWordsLines))
			break
		}
	}
	return kept
}

// measuredConflictCommits returns the object ids git named in `could not apply
// <id>...`, at most MaxMeasuredConflictCommits of them, in git's order.
func measuredConflictCommits(out string) []string {
	var seen []string
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, couldNotApplyMarker)
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(couldNotApplyMarker):])
		if cut := strings.Index(rest, couldNotApplyEllipsis); cut >= 0 {
			rest = rest[:cut]
		} else if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			rest = rest[:sp]
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || contains(seen, rest) {
			continue
		}
		seen = append(seen, rest)
		if len(seen) == MaxMeasuredConflictCommits {
			break
		}
	}
	return seen
}

// measureCommitID answers, RIGHT NOW, whether this workspace's object database
// carries the id git printed. git's own answer is quoted (`commit`, `blob`, or
// `fatal: Not a valid object name <id>`), so the reader is told which of the two
// situations they are in instead of being handed a lead to chase.
//
// The command is printed with git's PATH-RESOLVED spelling so it can be pasted
// straight into a shell: a reader who copies `git cat-file -t e907a50` goes to
// whatever git is FIRST on their PATH, which on a host carrying both
// /usr/bin/git and /usr/local/bin/git is not necessarily the one that answered.
// A measurement that cannot be re-run by the reader is a quote, not evidence.
func measureCommitID(ctx context.Context, dir, id string) string {
	out, err := runCommand(ctx, dir, gitBinary, catFileTypeVerb, catFileTypeFlag, id)
	answer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "fatal: "))
	if err != nil && answer == "" {
		answer = err.Error()
	}
	return fmt.Sprintf("%s %s %s %s: %s", resolvedGitPath(), catFileTypeVerb, catFileTypeFlag, id, answer)
}

// resolvedGitPath is the git that `exec` would actually run, as an absolute
// path: the ONE spelling a reader can paste back. A PATH that cannot be
// resolved falls back to the bare name (the run itself is still attempted, and
// its own error is what gets reported — Rule 15, never a silence).
func resolvedGitPath() string {
	if p, err := exec.LookPath(gitBinary); err == nil {
		return p
	}
	return gitBinary
}

// contains is the tiny membership test measuredConflictCommits needs, kept local
// so this file gains no dependency for one loop (Rule 35).
func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// publishGround pushes HEAD to ONE destination in ONE push: the integration
// branch (`base`). BASE is the contract three end-to-end gates pin ("the source
// repo's master did not move": clone_channel_delivery_before_claim_e2e_test.go,
// rawprompt_clone_delivery_*_e2e_test.go).
//
// THE REFSPEC IS PUSHED ORDINARY — no lease, no `+` force. `base` is SHARED, so
// a non-fast-forward refusal there is information, not noise: somebody else's
// commit landed between our `pull --rebase` and our push, and pushRefused
// classifies exactly that as the race the brake retries.
//
// THE ROW'S ADDRESS `task/<id>` IS NOT A DESTINATION, and that is deliberate
// (measured 2026-10-28: zpeer carried 15 `task/*` branches that were ALL already
// merged into master and were NEVER deleted — a ref nobody merges is not a
// delivery record, it is litter that piles up one ref per task forever; commit
// 9ab17237e removed the second refspec for that reason). The ledger's
// `deliverable.branch` is the ADDRESS the workspace stands on; the DELIVERY's
// destination is the integration branch, which is what this push moves and what
// the delivery proof measures a gone ground against
// (internal/tools/task_delivery_proof.go's deliveryProofIntegrationDestination).
// The ground ref therefore stays LOCAL, where `for-each-ref --contains` still
// resolves it for a close-out's reachability arm.
//
// THE SIGNATURE CARRIES NO `ground` PARAMETER ON PURPOSE (Rule 1/13). It was a
// dead argument from 9ab17237e onward: the function still took it and the prose
// above the call site still claimed it was published as a second destination.
// TestPublishGroundSourceNamesExactlyOneDestination pins the pushed set to
// exactly this one entry and TestPublishGroundPushesExactlyOneDestination
// measures the effect on a real origin, so a second destination cannot come back
// without the prose and a gate disagreeing.
func publishGround(ctx context.Context, dir, base string) (string, error) {
	args := []string{pushVerb, originRemoteName, gitHeadRef + ":" + refsHeadsPrefix + base}
	return runCommand(ctx, dir, gitBinary, args...)
}

// pushLandedOnPeer answers THE ONLY QUESTION `push accepted` may rest on: does
// the peer this push ADDRESSED actually carry the commit the push sent?
//
// IT IS ALREADY IN THE LEG, ON PURPOSE, AND THAT IS A CORRECTNESS CONSTRAINT
// rather than a taste for early reporting. The close-out RECLAIMS the ground
// (`Reclaim`, called from cmd/agent's finish() the moment the delivery returns),
// so a verification performed after the loop has nothing left to ask: the local
// HEAD that names what was sent is gone with the directory. A check that runs
// here can name both sides — the commit this attempt pushed and the tip the peer
// answers with — while they still exist.
//
// TWO READINGS, AND THE SECOND IS NOT A RETRY:
//
//	① the peer's tip for the pushed ref EQUALS our HEAD — the ordinary case;
//	② the peer is at a DIFFERENT commit but OURS IS ITS ANCESTOR — a concurrent
//	  push landed on top of ours between the push and this read, which is a
//	  real race this leg must not report as a lost delivery. It needs one fetch
//	  of that tip (into FETCH_HEAD only, no branch, no remote-tracking ref — the
//	  same shape deliveryCommitsMissing uses) because the peer's commit is
//	  normally an object this store has never seen.
//
// EVERY UNMEASURED SHAPE IS A FAILURE WITH ITS OWN SENTENCE (Rule 15): a
// ground whose HEAD cannot be read, a remote the ground cannot name, a peer that
// does not answer. None of them may read as "landed" — that is the very
// conflation this function exists to end — and none of them claims the delivery
// was refused either. `ok` is the measurement, `why` is what the reader acts on.
func pushLandedOnPeer(ctx context.Context, dir, base string) (ok bool, why string) {
	local, code := deliveryRefTip(ctx, dir, gitHeadRef)
	if code != 0 || local == "" {
		return false, fmt.Sprintf("this ground's own `%s %s` answered %q (exit %d), so the commit the push sent is unknown",
			gateTipVerb, gitHeadRef, local, code)
	}
	operand, ref := deliveryPeerOperand(deliveryOriginURL(ctx, dir, ""), base)
	if operand == "" {
		return false, "the ground names no `origin` to ask, so the peer that received the push is unknown"
	}
	peerTip := deliveryPeerTipFrom(ctx, dir, operand, ref)
	if peerTip == "" {
		return false, fmt.Sprintf("`%s %s %s` printed no %s: the peer does not carry that ref at all",
			gitListRemoteVerb, operand, ref, ref)
	}
	if peerTip == local {
		return true, ""
	}
	if !deliveryCarriesCommit(ctx, dir, peerTip) {
		if _, err := runCommand(ctx, dir, gitBinary, deliveryFetchVerb, deliveryNoTagsFlag,
			deliveryQuietFlag, operand, ref); err != nil {
			return false, fmt.Sprintf("the peer is at %s, not at the %s this attempt pushed, and that commit could not be fetched to compare them",
				peerTip, local)
		}
		fetched, _ := deliveryRefTip(ctx, dir, fetchHeadRef)
		if fetched == "" {
			return false, fmt.Sprintf("the peer is at %s, not at the %s this attempt pushed, and the fetch left no %s to compare",
				peerTip, local, fetchHeadRef)
		}
		peerTip = fetched
	}
	if _, err := runCommand(ctx, dir, gitBinary, gitMergeBaseVerb, gitIsAncestorFlag, local, peerTip); err == nil {
		// OURS IS IN THE PEER'S HISTORY: a concurrent push landed on top of it,
		// which is the delivery having happened — not a refused one.
		return true, ""
	}
	return false, fmt.Sprintf("the peer's %s is at %s, and the %s this attempt pushed is not in its history",
		ref, peerTip, local)
}

// firstField is the first whitespace-separated field of git's ls-remote output
// (the object id), or "" when the output is empty — which is exactly what
// `ls-remote` prints for a ref the remote does not carry.
func firstField(out string) string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

// pushRefused classifies a failed push by MEASUREMENT: it fetches the remote
// branch and reports refused=true only when the remote's tip is NOT an ancestor
// of our HEAD — i.e. somebody else's commit landed first.
func pushRefused(ctx context.Context, dir, branch string) (bool, string) {
	if _, err := runCommand(ctx, dir, gitBinary, "fetch", "origin", branch); err != nil {
		return false, "the remote branch could not be fetched"
	}
	if _, err := runCommand(ctx, dir, gitBinary, "rev-parse", "--verify", "refs/remotes/origin/"+branch); err != nil {
		return false, "the remote branch does not exist"
	}
	if _, err := runCommand(ctx, dir, gitBinary, "merge-base", "--is-ancestor",
		"refs/remotes/origin/"+branch, "HEAD"); err == nil {
		// The remote IS behind us, so the push was not a race — a real error.
		return false, "the remote branch is already an ancestor of HEAD, so the push was not refused by a newer commit"
	}
	return true, "origin/" + branch + " carries commits we do not have (someone pushed first)"
}

// currentBranch is the branch the workspace stands on (DefaultBranch for a
// detached HEAD, which is never how this channel clones).
func currentBranch(ctx context.Context, dir string) (string, error) {
	out, err := runCommand(ctx, dir, gitBinary, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf(CloneChannelPrefix+"read the workspace branch: %w (output: %s)", err, tail(out))
	}
	b := strings.TrimSpace(out)
	if b == "" || b == "HEAD" {
		return DefaultBranch, nil
	}
	return b, nil
}

// isGitWorkTree reports whether dir is already a usable checkout — a CLONE
// (its own .git directory) or a linked worktree (a .git file).
func isGitWorkTree(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	return true
}

// writeBrakeRecord leaves the re-readable record of a fired brake INSIDE the
// kept workspace (Rule 15: a stop that nobody can review afterwards is a
// silent one).
func writeBrakeRecord(dir string, res Result) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("no workspace directory")
	}
	body := fmt.Sprintf(`gopi clone channel — push brake fired

task: %s
workspace: %s
branch: %s
attempts: %d (limit %d)
outcome: %s

The workspace was KEPT on purpose: the agent's commits are here, unmerged.
Nothing was deleted and nothing was force-pushed.

next step: re-run the task — it reuses this workspace, pulls, builds and tries
again — or, if the conflict is content-level, reconcile it by hand in the
workspace above and push when origin accepts the result.
`, res.TaskIDLine(), dir, res.Branch, res.Attempts, MaxPushAttempts, res.Detail)
	if res.Branch == "" {
		body = strings.Replace(body, "branch: \n", "branch: (unknown)\n", 1)
	}
	return os.WriteFile(filepath.Join(dir, BrakeRecordName), []byte(body), 0o644)
}

// TaskIDLine is the task id the record names — derived from the workspace
// directory's own leaf, which IS the task id (Dir).
func (r Result) TaskIDLine() string {
	if r.Dir == "" {
		return "(unknown)"
	}
	return filepath.Base(r.Dir)
}

// runBuild executes the DECLARED build command in dir, as a shell command
// (exactly how the merge-time build gate runs it), bounded by timeout.
//
// IT INHERITS THE LOCALE PIN, and that is deliberate, not incidental: this
// package SCANS the build's output with an English vocabulary (decisive_lines.go
// `BlockerKindBuild`: "make: *** ", "error", "syntax error", "undefined:" …), and
// a build command is as free to translate as git is. Measured on this host —
// `/usr/share/locale-langpack/zh_CN/LC_MESSAGES/{gcc,make,ld,binutils}.mo` are
// all present:
//
//	LC_ALL=zh_CN.UTF-8 make bad  →  make: *** [Makefile:6：bad] 错误 1
//	LC_ALL=C           make bad  →  make: *** [Makefile:6: bad] Error 1
//
// The `make: *** ` token survives both, `错误 1` matches none of "error". The
// command STRING is untouched — nothing here changes what the build runs, only
// the language it complains in. One honest consequence: C also fixes the
// collation, so a build whose output is order-sensitive (`ls` without an
// explicit flag) can print the same lines in another order; no reader of this
// output is order-sensitive.
func runBuild(ctx context.Context, dir, command string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(cctx, dir, shellBinary, "-c", command)
}

// runCommand is the bounded git wrapper: one timeout, one shape, so no caller
// can silently get a different git environment.
func runCommand(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	return run(cctx, dir, name, args...)
}

// run is the ONE subprocess runner of this package. It is the ONE place a
// child environment is composed, which is why the locale pin lives HERE and not
// in each caller (Rule 2/14: one source, read once).
func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// A clone channel run must never block on an interactive credential
	// prompt: a repository that needs one is a configuration error, reported
	// as a failure, not a hang.
	//
	// The language pin rides the same slice: see the localeEnvName block for
	// the measured reason a caller's Chinese desktop must not reach this
	// package's parsers.
	cmd.Env = append(pinChildLocale(os.Environ()), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pinChildLocale returns env with the child's MESSAGE LANGUAGE pinned to
// cLocaleValue — the untranslated strings this package's markers are written
// against — leaving every other variable (the caller's PATH, HOME, GIT_*, the
// operator's own LC_COLLATE) exactly as inherited.
//
// The three names are set EXPLICITLY rather than filtered-then-appended, and the
// reason is measured: the locale is a PRECEDENCE LIST, so which variable wins
// depends on which of them the caller exported. Dropping only localeEnvName
// would leave an exported LANGUAGE list free to outrank a translated LANG (the
// list beats every locale CATEGORY, measured), and appending after an existing
// entry would put the pin where a libc need not look. Setting all three — and
// EMPTYING the list — is the shape that holds no matter what the caller had.
//
// What the list does NOT do is defeat an explicit C pin (six shapes measured,
// all English — see the languageListEnvName block): the emptying is written
// down so the next reader does not have to re-derive that, and so the pin
// survives a future retune that drops the LC_ALL arm.
func pinChildLocale(env []string) []string {
	pinned := map[string]string{
		localeEnvName:       cLocaleValue,
		languageEnvName:     cLocaleValue,
		languageListEnvName: "",
	}
	out := make([]string, 0, len(env)+len(pinned))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, drop := pinned[name]; !drop {
			out = append(out, kv)
		}
	}
	for name, value := range pinned {
		out = append(out, name+"="+value)
	}
	return out
}

// tail keeps the last few lines of a subprocess's output: enough for the
// reader to see WHICH failure, without pasting a whole build log into a
// ledger row.
func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	const keep = 6
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return strings.Join(lines, tailLineJoiner)
}
