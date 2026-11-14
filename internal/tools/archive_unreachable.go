package tools

// The UNREACHABLE BACKLOG census + rescue (epic 01a12f65, the BACKLOG half of
// the orphan family whose PRODUCER half is branch_delete_guard.go).
//
// THE MEASURED DEFECT. A branch is deleted and its tip is left reachable from
// NO ref: the object is still present (`git cat-file -t` answers `commit`) and
// NOTHING in the repo names it. What keeps it alive is the REFLOG GRACE WINDOW
// — and nothing in the project could tell an operator the difference between
//
//	"archived"  — a DURABLE ref holds the tip (refs/tags/archive/<branch>
//	              plus its refs/gopi/archive-reason/<branch> WHY record), and
//	"deleted,  with a grace period" — no ref holds it, one
//	              `git reflog expire --expire=now --all && git gc --prune=now`
//	              and it is gone.
//
// Measured on the peer peerbox (/home/user/work/GoPi, 2026-10-12): 1672 commits
// unreachable from any ref, 329 of them TIPS (parents of no other unreachable
// commit) — each one the last holder of a line of work that no ref records.
//
// This file is the READ side (`gopi task archive --unreachable`) and the
// RESCUE side (`gopi task archive --rescue-tip`) of that difference. Both are
// read-only-or-one-write and both go through machinery that already exists:
// the census reads with `git fsck`/`git log`/`git reflog`, the rescue writes
// through ArchiveTagReasoned (the project's ONE archive writer, Rule 9), so
// this file mints no second archive path.
//
// THE MECHANISM THAT KEEPS A TIP ALIVE — measured, and the census classifies on
// exactly it (this is the distinction the whole surface exists to render):
//
//	(a) an entry in SOME reflog still names the tip ⇒ it survives until that
//	    reflog is expired. `git reflog expire --expire=now --all` is the
//	    decisive act, and it is one command away;
//	(b) no reflog entry names it ⇒ it is already past the reflog's keeping and
//	    `git gc --prune=now` alone removes it.
//
// Both readings were reproduced in a scratch repo (the sibling memory #9676
// carries the transcript: `git gc --prune=now` alone left the object, adding
// the reflog expire made it gone).
//
// WHY THE TIP SET IS NOT ASKED OF `git fsck --unreachable=TIP`. The obvious
// spelling (`git fsck --unreachable=<sha> --no-reflogs`) answers for a commit
// that is unreachable *because of* that commit, which is the right shape for a
// commit whose PARENTS are all reachable (the ordinary single-commit orphan)
// and the wrong one for a commit inside an unreachable CHAIN — the exact shape
// this backlog is made of. The tip set here is therefore derived from the
// parents of the unreachable commits, read from the ONE `git log --no-walk`
// call described at archiveUnreachableCommitChunk (Rule 13: one enumerator, so
// the census and any future reader cannot disagree about what a tip is).

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// The git spellings this surface needs, each spelled once (Rule 1b/13).
const (
	// archiveUnreachableFSCKArg — `git fsck --unreachable` prints one line per
	// object that is present but reachable from no ref, as
	// "<kind> <type> <sha>": the `^unreachable ` PREFIX is what selects the
	// lines, and the middle field is the git OBJECT TYPE the census counts.
	archiveUnreachableFSCKArg = "--unreachable"
	// archiveUnreachableNoReflogsArg — WITHOUT this, git keeps an object alive
	// while any reflog names it and never reports it as unreachable, so the
	// reflog-held half of the backlog is invisible. Every number in this file
	// is measured with reflogs DISABLED, which is what makes "reflog-held"
	// an independent fact rather than a tautology.
	archiveUnreachableNoReflogsArg = "--no-reflogs"
	// archiveUnreachableLinePrefix — the first field of an `unreachable` line.
	archiveUnreachableLinePrefix = "unreachable"
	// gitObjectTypeTree / gitObjectTypeBlob / gitObjectTypeTag — the other
	// object kinds fsck reports; the census counts commits and REPORTS the
	// rest as a breakdown (an unreachable tag is a decision record that lost
	// its ref, which is worth seeing).
	gitObjectTypeTree = "tree"
	gitObjectTypeBlob = "blob"
	gitObjectTypeTag  = "tag"

	// archiveUnreachableFSFieldCount — `unreachable <type> <sha>`.
	archiveUnreachableFSFieldCount = 3
	// archiveFSCKFieldSep — fsck's own field separator is a single space.
	archiveFSCKFieldSep = " "

	// The git subcommands this file (and the archive writer beside it) spell.
	// Each one is a MEANINGFUL string (Rule 1b): a subcommand name IS the
	// contract, so it is written once and referenced, never retyped at a call
	// site — the same discipline gitPushbackCheckRootRef and archiveTagRefPrefix
	// already follow.
	gitFSCKVerb       = "fsck"
	gitLogVerb        = "log"
	gitTagVerb        = "tag"
	gitReflogVerb     = "reflog"
	gitForEachRefVerb = "for-each-ref"
	gitRevParseVerb   = "rev-parse"
	gitCatFileVerb    = "cat-file"
	gitUpdateRefVerb  = "update-ref"
	gitHashObjectVerb = "hash-object"
	gitMergeBaseVerb  = "merge-base"

	// The argument spellings, likewise once.
	gitNoProgressArg    = "--no-progress"
	gitNoWalkArg        = "--no-walk"
	gitReflogShowArg    = "show"
	gitReflogAllArg     = "--all"
	gitVerifyArg        = "--verify"
	gitQuietArg         = "--quiet"
	gitContainsArg      = "--contains"
	gitIsAncestorArg    = "--is-ancestor"
	gitFormatFlagPrefix = "--format="

	// archiveRefNameFormat / archiveObjectNameFormat / archiveObjectTypeFormat —
	// the three for-each-ref field specifiers this surface reads: the ref's own
	// name (what a holder enumeration prints), the object it names (what a
	// lookup keyed by commit joins on), and that object's TYPE — the atom that
	// lets a reader CLASSIFY a ref instead of assuming its shape, so one
	// wrong-typed ref in a namespace is a reportable entry and not a fatal
	// `cat-file` (measured defect 01a143b4: a commit-typed reason ref aborted
	// the whole archive namespace scan).
	archiveRefNameFormat    = "%(refname)"
	archiveObjectNameFormat = "%(objectname)"
	archiveObjectTypeFormat = "%(objecttype)"
)

// archiveReflogRefOf derives the REF NAME out of a %gD reflog selector: the
// text before the `@{n}` suffix (`refs/heads/x@{3}` → `refs/heads/x`, `HEAD@{0}`
// → `HEAD`). One function, so the census's print and the operator's expire
// command name the same reflog.
func archiveReflogRefOf(selector string) string {
	if i := strings.Index(selector, archiveReflogSelectorSep); i >= 0 {
		return selector[:i]
	}
	return selector
}

// The ONE enumerator of the unreachable commit set's shape: the `--format`
// handed to `git log --no-walk`, its field separator, and the field count.
//
// `%H%x1f%cI%x1f%s%x1f%P` answers, per commit: the full sha, the ISO-8601
// committer date, the subject, and the PARENT LIST — all four in one git call
// for a whole chunk of commits, so the census costs a handful of subprocesses
// rather than one per commit (the measured backlog is 1672 commits).
//
// A single space is git's own parent-list separator and is safe here because
// the OTHER fields are separated by US (0x1f), which a commit subject cannot
// contain: the line therefore splits into exactly four fields however exotic a
// subject is.
const (
	archiveUnreachableLogFormat  = "%H%x1f%cI%x1f%s%x1f%P"
	archiveUnreachableFieldSep   = "\x1f"
	archiveUnreachableFieldCount = 4
	// archiveParentListSep — git's separator inside %P.
	archiveParentListSep = " "
	// archiveUnreachableCommitChunk — how many revisions go on one `git log`
	// argv. It is an ARGV LIMIT, not a product limit (Rule 1): the whole tip
	// set must fit in one command line, and 500 shas is ~20 KiB, far below any
	// platform's ceiling, while keeping the call count small.
	archiveUnreachableCommitChunk = 500
)

// archiveReflogFormat / archiveReflogFieldSep — one `git reflog show --all`
// call yields "<sha>\t<gD>" per entry, where %gD is the fully-qualified reflog
// selector (`refs/heads/x@{3}`, `HEAD@{0}`). The REF NAME is everything before
// the first `@{` (archiveReflogSelectorSep) — the same derivation the census
// prints, so a reader can go and expire exactly that reflog.
const (
	archiveReflogFormat      = "%H%x09%gD"
	archiveReflogFieldSep    = "\t"
	archiveReflogFieldCount  = 2
	archiveReflogSelectorSep = "@{"
	// archivePeelToCommitSuffix — the `^{commit}` peel, spelled once: it is
	// what makes `rev-parse --verify --quiet <rev>` fail LOUDLY on a rev that
	// names a tag or a tree instead of answering with a non-commit object the
	// rescue would then tag.
	archivePeelToCommitSuffix = "^{" + gitObjectTypeCommit + "}"
)

// archiveUnreachableBranchPrefix names a RESCUED tip's branch. It is a branch
// NAME (the value ArchiveTagReasoned turns into refs/tags/archive/<name> and
// refs/gopi/archive-reason/<name>), never a ref path, so the namespace stays
// the archive lane's own: a rescue is an ARCHIVE decision, recorded where every
// merge path already looks.
const archiveUnreachableBranchPrefix = "rescued-"

// archiveUnreachableShortSHALen — how much of the sha the census and the rescue
// tag display. Long enough to be unambiguous among the tips of one repo (the
// full sha is always printed beside it in the census), short enough to read.
const archiveUnreachableShortSHALen = 12

// archiveHoursPerDay / archiveMinutePerHour / archiveSecondPerMinute render an
// age as "NdNh" without pulling a duration formatter into the census.
const (
	archiveSecondsPerMinute = 60
	archiveMinutesPerHour   = 60
	archiveHoursPerDay      = 24
)

// ArchiveUnreachableTip is ONE unreachable tip: the sha, when its work landed,
// what it says, whether a reflog still holds it, and whether any archive
// decision in the repo names it.
type ArchiveUnreachableTip struct {
	SHA         string
	Short       string
	CommittedAt time.Time
	Age         time.Duration
	Subject     string
	// ReflogHeld is the DECISIVE field: true means the reflog grace window is
	// the only thing keeping this commit (one `git reflog expire` from gone),
	// false means it is already past that keeping (a `git gc --prune=now`
	// removes it).
	ReflogHeld bool
	// ReflogRefs names the reflog(s) holding it (`refs/heads/x`, `HEAD`), so
	// the operator can see WHICH keeping window is about to close. Empty when
	// no reflog names the tip.
	ReflogRefs []string
	// ArchiveBranch is the branch whose archive tag points AT THIS COMMIT, when
	// one does: the tip is then already archived (and would not be reported as
	// unreachable by git — see ArchiveUnreachableCensus's doc) and this field is
	// the lookup's positive control.
	ArchiveBranch string
	// ArchiveReason is the reason code recorded beside that tag ("" = the tag
	// carries no reason, the 21:04 shape).
	ArchiveReason string
}

// ArchiveUnreachableCensus is the measured backlog as DATA: every unreachable
// tip plus the counts the summary line renders. The caller renders; this
// function measures, so the CLI, a test and a future report surface read one
// set (Rule 13).
type ArchiveUnreachableCensus struct {
	Repo string
	// UnreachableCommits is every commit fsck reported (the backlog's total),
	// of which Tips is the subset nothing else unreachable descends from.
	UnreachableCommits int
	// OtherObjects is the non-commit unreachable objects fsck reported
	// (tree/blob/tag), by type — reported, never counted as tips.
	OtherObjects map[string]int
	Tips         []ArchiveUnreachableTip
	// ReflogHeld / PastReflog split Tips by the keeping window (see
	// ArchiveUnreachableTip.ReflogHeld). ReflogHeld+PastReflog == len(Tips).
	ReflogHeld int
	PastReflog int
	// Undescribed counts unreachable commits for which `git log` returned no
	// row. It is REPORTED rather than ignored: a census whose enumeration
	// silently covered a subset is the Rule 15 failure this field exists to
	// make impossible (an empty value and an unanswered question must not look
	// alike).
	Undescribed int
	// Unrecognized counts the non-empty lines of git's report that this census
	// could not classify (not `<kind> <type> <sha>` with a kind it knows). It is
	// reported for the SAME reason as Undescribed, and it is the guard the
	// locale incident earned: git's diagnostics are translated per the ambient
	// locale (see git_child_locale.go), so a report full of `不可达 commit <sha>`
	// used to be dropped line by line and rendered as a clean zero. The locale
	// is pinned now; this field is what makes the NEXT such divergence LOUD
	// instead of silent (measured 2026-10-12: with the pin removed, an orphaned
	// tip rendered as "no unreachable tip" while git had named it).
	Unrecognized int
	// Notes carries every degradation the measurement made, in words, for the
	// renderer to print verbatim.
	Notes []string
}

// archiveUnreachableCommit is one row of the enumeration: the sha, its
// committer date and subject, and its parents.
type archiveUnreachableCommit struct {
	sha     string
	date    time.Time
	subject string
	parents []string
}

// ArchiveUnreachableCensusAt measures the unreachable backlog of repo.
//
// It is READ-ONLY: `git fsck`, `git log --no-walk`, `git reflog show` and the
// two archive-namespace scans (for-each-ref + cat-file of the reason blobs).
// No ref is written and no object is touched.
//
// An empty repo, a repo with no unreachable objects and a repo whose reflogs
// are all empty are all VALID, non-failing answers (zero tips is a measurement,
// not an error). Only a real failure — fsck could not run, the archive
// namespace could not be read — is returned as an error, and the caller exits 1
// on it (Rule 15: a scan that could not answer must never look like a clean
// answer).
func ArchiveUnreachableCensusAt(repo string, now time.Time) (ArchiveUnreachableCensus, error) {
	c := ArchiveUnreachableCensus{Repo: repo, OtherObjects: map[string]int{}}
	out, err := archiveUnreachableFSCK(repo)
	if err != nil {
		return c, err
	}
	var commits []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		f := strings.SplitN(line, archiveFSCKFieldSep, archiveUnreachableFSFieldCount)
		if len(f) < archiveUnreachableFSFieldCount || f[0] != archiveUnreachableLinePrefix {
			// git said something this census cannot read. Dropping it silently
			// is the exact failure the locale incident produced (git's
			// translated diagnostics matched no line and the census rendered a
			// clean zero for a repo full of orphans), so it is COUNTED and
			// reported — never swallowed (Rule 15).
			c.Unrecognized++
			continue
		}
		switch f[1] {
		case gitObjectTypeCommit:
			commits = append(commits, f[2])
		default:
			c.OtherObjects[f[1]]++
		}
	}
	c.UnreachableCommits = len(commits)
	if c.Unrecognized > 0 {
		// A LOUD line, never a silent drop. This is the guard the locale
		// incident earned: had it existed, the translated report would have
		// announced itself instead of rendering as "no unreachable tip —
		// every commit is named by a ref".
		c.Notes = append(c.Notes, fmt.Sprintf(
			"%d line(s) of git's report were not `%s <type> <sha>` and could not be classified — the counts below may UNDER-report; git's diagnostics are translated per the ambient locale, which this census pins (git_child_locale.go)",
			c.Unrecognized, archiveUnreachableLinePrefix))
	}
	if len(commits) == 0 {
		c.Tips = []ArchiveUnreachableTip{}
		return c, nil
	}
	rows, err := archiveUnreachableDescribe(repo, commits)
	if err != nil {
		return c, err
	}
	reflogs, err := archiveUnreachableReflogs(repo)
	if err != nil {
		return c, err
	}
	decisions, err := archiveUnreachableDecisions(repo)
	if err != nil {
		return c, err
	}
	if missing := len(commits) - len(rows); missing > 0 {
		c.Undescribed = missing
		c.Notes = append(c.Notes, fmt.Sprintf(
			"%d unreachable commit(s) returned no `git log` row — the tip set below describes %d of %d",
			missing, len(rows), len(commits)))
	}
	c.Tips = archiveUnreachableTips(rows, reflogs, decisions, now)
	for _, t := range c.Tips {
		if t.ReflogHeld {
			c.ReflogHeld++
			continue
		}
		c.PastReflog++
	}
	return c, nil
}

// archiveFSCKTimeout — the ceiling the census's object walk gets.
//
// It is deliberately NOT repoSweepGitTimeout: fsck walks the WHOLE object
// store, and the measured backlog repo (the peer, 2026-10-12) took 36.7s for
// `git fsck --unreachable --no-reflogs` — a single ref read beside it costs
// milliseconds, so one shared ceiling would either starve the walk or bloat
// every ordinary call. Measured, not guessed: the number is ~8× the observed
// walk, and a walk that exceeds it is reported as a failure (Rule 15), never
// rendered as an empty census.
const archiveFSCKTimeout = 5 * time.Minute

// archiveUnreachableFSCK runs the one fsck read behind the census and returns
// its output VERBATIM (both streams).
//
// fsck signals "I found unreachable objects" on its EXIT STATUS, so a non-zero
// exit is NOT a failure here: only a git that could not produce a report at all
// (a missing repo, an unreadable object store) is. The distinction is made by
// what the command WROTE: output + `unreachable` lines is a measurement; no
// output at all with a non-zero status is a failure, and it is reported by name
// (Rule 15 — an empty census and an unanswered question must not look alike).
func archiveUnreachableFSCK(repo string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), archiveFSCKTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo,
		gitFSCKVerb, gitNoProgressArg, archiveUnreachableFSCKArg, archiveUnreachableNoReflogsArg)
	// THE LOCALE IS PINNED, and it is a CORRECTNESS requirement: this function's
	// caller selects rows by the literal word `unreachable`, and git TRANSLATES
	// its diagnostics (gettext). Under zh_CN the same objects print
	// `不可达 commit <sha>`, the prefix matched nothing, and the census rendered
	// its clean zero for a repo full of orphans — measured 2026-10-12 through
	// the shipped binary. One pin, in the shared helper (Rule 14).
	cmd.Env = ChildLocaleEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		if strings.Contains(out, archiveUnreachableLinePrefix) {
			// fsck's ordinary exit status when it HAS findings.
			return out, nil
		}
		return "", fmt.Errorf("git fsck %s %s in %s: %w: %s", archiveUnreachableFSCKArg,
			archiveUnreachableNoReflogsArg, repo, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// archiveUnreachableDescribe enumerates the commits' shape: sha, committer
// date, subject and parents, in chunks of archiveUnreachableCommitChunk.
//
// THE PARENT LIST IS WHY THIS IS ONE CALL AND NOT A WALK. `git log --no-walk`
// prints exactly the commits it was given (never their ancestors — which is the
// whole point: walking would leave the unreachable world), and %P carries each
// one's parents. The TIP set is then a pure Go question over that table: a
// commit is a tip iff none of the unreachable commits lists it as a parent.
func archiveUnreachableDescribe(repo string, commits []string) ([]archiveUnreachableCommit, error) {
	var rows []archiveUnreachableCommit
	for start := 0; start < len(commits); start += archiveUnreachableCommitChunk {
		end := start + archiveUnreachableCommitChunk
		if end > len(commits) {
			end = len(commits)
		}
		args := append([]string{gitLogVerb, gitNoWalkArg, gitFormatFlagPrefix + archiveUnreachableLogFormat},
			commits[start:end]...)
		out, err := repoSweepGit(repo, args...)
		if err != nil {
			return nil, fmt.Errorf("enumerate unreachable commits in %s: %w", repo, err)
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimRight(line, "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			f := strings.SplitN(line, archiveUnreachableFieldSep, archiveUnreachableFieldCount)
			if len(f) < archiveUnreachableFieldCount {
				continue
			}
			r := archiveUnreachableCommit{sha: f[0], subject: f[2]}
			// A malformed date is not a reason to drop the row: the sha is
			// what the operator acts on, so the date degrades to the zero time
			// and the renderer says so — but the row stays (Rule 15: report the
			// degradation, never silently lose the entry).
			if ts, perr := time.Parse(time.RFC3339, strings.TrimSpace(f[1])); perr == nil {
				r.date = ts
			}
			r.parents = strings.Fields(strings.TrimSpace(f[3]))
			rows = append(rows, r)
		}
	}
	return rows, nil
}

// archiveUnreachableReflogs maps sha → the reflog selectors naming it, in ONE
// `git reflog show --all` call. A sha may be held by several reflogs; the ref
// NAMES are what the census prints, deduplicated and sorted so the line is
// stable between runs.
func archiveUnreachableReflogs(repo string) (map[string][]string, error) {
	out, err := repoSweepGit(repo, gitReflogVerb, gitReflogShowArg, gitReflogAllArg,
		gitFormatFlagPrefix+archiveReflogFormat)
	if err != nil {
		return nil, fmt.Errorf("read reflogs of %s: %w", repo, err)
	}
	held := map[string]map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, archiveReflogFieldSep, archiveReflogFieldCount)
		if len(f) < archiveReflogFieldCount {
			continue
		}
		sha, selector := f[0], f[1]
		name := archiveReflogRefOf(selector)
		if name == "" {
			name = selector
		}
		if held[sha] == nil {
			held[sha] = map[string]bool{}
		}
		held[sha][name] = true
	}
	out2 := map[string][]string{}
	for sha, set := range held {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out2[sha] = names
	}
	return out2, nil
}

// archiveUnreachableDecisions maps a COMMIT sha → the branch whose archive
// refs (refs/tags/archive/<branch>) point at it, plus the reason recorded in
// its companion ref. It is the census's archive-decision lookup, and it reads
// the SAME two namespaces the rest of the archive lane reads:
//
//	refs/tags/archive/<branch>         the decide ref (created by the one writer)
//	refs/gopi/archive-reason/<branch>  the recorded WHY
//
// A commit no archive tag points at stays absent (the ordinary shape for a
// tip): the renderer prints the absence as "none", which is a fact — the tip
// holds its work by nothing durable.
func archiveUnreachableDecisions(repo string) (map[string]ArchiveUnreachableTip, error) {
	out, err := repoSweepGit(repo, gitForEachRefVerb,
		gitFormatFlagPrefix+archiveRefNameFormat+archiveFieldSep+archiveObjectNameFormat, archiveTagRefPrefix)
	if err != nil {
		return nil, fmt.Errorf("archive namespace scan in %s: %w", repo, err)
	}
	reasons, _, err := archiveReasonRecords(repo)
	if err != nil {
		return nil, err
	}
	dec := map[string]ArchiveUnreachableTip{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.SplitN(line, archiveFieldSep, archiveDecisionFieldCount)
		if len(f) < archiveDecisionFieldCount {
			continue
		}
		branch := archiveBranchOfEntry(f[0], "")
		dec[f[1]] = ArchiveUnreachableTip{ArchiveBranch: branch,
			ArchiveReason: ArchiveTagReason(reasons[branch])}
	}
	return dec, nil
}

// archiveDecisionFieldCount — the archive namespace's (ref, objectname) scan:
// two fields, because a lookup keyed by COMMIT has no use for the ref's
// message (and `%(contents:subject)` on this namespace is the commit's own
// subject, not the branch name — see archiveNamespaceSubjectSep).
const archiveDecisionFieldCount = 2

// archiveUnreachableTips turns the enumeration into the census's rows: the tips
// (commits nothing else unreachable descends from), each annotated with its
// keeping window and its archive decision, SORTED by committer date descending
// with the sha as the tie-break (so two runs over an unchanged repo render
// identical output — a census whose line order drifts between runs cannot be
// diffed).
func archiveUnreachableTips(rows []archiveUnreachableCommit,
	reflogs map[string][]string, decisions map[string]ArchiveUnreachableTip,
	now time.Time) []ArchiveUnreachableTip {
	cited := map[string]bool{}
	for _, r := range rows {
		for _, p := range r.parents {
			cited[p] = true
		}
	}
	tips := make([]ArchiveUnreachableTip, 0, len(rows))
	for _, r := range rows {
		if cited[r.sha] {
			continue
		}
		t := ArchiveUnreachableTip{
			SHA: r.sha, Short: archiveShortSHA(r.sha), CommittedAt: r.date,
			Subject: r.subject, ReflogRefs: reflogs[r.sha], ReflogHeld: len(reflogs[r.sha]) > 0,
		}
		if !r.date.IsZero() {
			t.Age = now.Sub(r.date)
		}
		if d, ok := decisions[r.sha]; ok {
			t.ArchiveBranch, t.ArchiveReason = d.ArchiveBranch, d.ArchiveReason
		}
		tips = append(tips, t)
	}
	sort.SliceStable(tips, func(i, j int) bool {
		if !tips[i].CommittedAt.Equal(tips[j].CommittedAt) {
			return tips[i].CommittedAt.After(tips[j].CommittedAt)
		}
		return tips[i].SHA < tips[j].SHA
	})
	return tips
}

// archiveShortSHA truncates for DISPLAY only; every decision (and the rescue
// tag) names the full sha, so a shortened display can never be the operand.
func archiveShortSHA(sha string) string {
	if len(sha) <= archiveUnreachableShortSHALen {
		return sha
	}
	return sha[:archiveUnreachableShortSHALen]
}

// ArchiveUnreachableAgeLabel renders an age as "NdNh" (or "-" for an unknown
// date). It lives beside the census so the CLI and a test cannot format the
// same duration two ways (Rule 13).
func ArchiveUnreachableAgeLabel(age time.Duration) string {
	if age <= 0 {
		return archiveAgeUnknown
	}
	hours := int(age.Hours())
	return fmt.Sprintf("%dd%dh", hours/archiveHoursPerDay, hours%archiveHoursPerDay)
}

// archiveAgeUnknown — the honest label for an age that could not be derived
// (an unparseable committer date). Never "0d0h", which would read as "today".
const archiveAgeUnknown = "-"

// ArchiveUnreachableHeldLabel renders the keeping window of one tip: "reflog:<refs>"
// when a reflog still names it, archiveHeldPastReflog otherwise.
func ArchiveUnreachableHeldLabel(t ArchiveUnreachableTip) string {
	if t.ReflogHeld {
		return archiveHeldReflogPrefix + strings.Join(t.ReflogRefs, ",")
	}
	return archiveHeldPastReflog
}

const (
	archiveHeldReflogPrefix = "reflog:"
	// archiveHeldPastReflog is the label for a tip NO reflog names: it is
	// already past the reflog's keeping. The wording is load-bearing — it must
	// not read as "held" (the mirror-image mistake of the sibling fix, where a
	// scratch ref that would be pruned in the same pass read as a holder).
	archiveHeldPastReflog = "past-reflog"
	// archiveDecisionNone is what a tip with no archive tag prints: the fact
	// that no durable decision names it.
	archiveDecisionNone = "none"
)

// ArchiveUnreachableDecisionLabel renders a tip's archive-decision lookup:
// "<branch>(<reason>)" when an archive tag points at the commit, "none" when
// nothing does, and the tag's own branch with ArchiveReasonMissingLabel when the
// tag exists without a reason (the 21:04 shape).
func ArchiveUnreachableDecisionLabel(t ArchiveUnreachableTip) string {
	if t.ArchiveBranch == "" {
		return archiveDecisionNone
	}
	if t.ArchiveReason == "" {
		return t.ArchiveBranch + " (" + ArchiveReasonMissingLabel + ")"
	}
	return t.ArchiveBranch + " (" + t.ArchiveReason + ")"
}

// ArchiveUnreachableReport renders the census (Rule 5: a census that returns
// only a count is unreadable — an operator must see WHICH tips and what holds
// them). One line per tip, then the summary, then the rescue hint.
func ArchiveUnreachableReport(c ArchiveUnreachableCensus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "unreachable census: %s\n", c.Repo)
	fmt.Fprintf(&b, "  unreachable commits: %d\n", c.UnreachableCommits)
	fmt.Fprintf(&b, "  unreachable tips:    %d\n", len(c.Tips))
	fmt.Fprintf(&b, "  kept by a reflog:    %d  (one `git reflog expire --expire=now --all` from gone)\n", c.ReflogHeld)
	fmt.Fprintf(&b, "  past the reflog:     %d  (`git gc --prune=now` alone removes these)\n", c.PastReflog)
	if len(c.OtherObjects) > 0 {
		kinds := make([]string, 0, len(c.OtherObjects))
		for k := range c.OtherObjects {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		parts := make([]string, 0, len(kinds))
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%s=%d", k, c.OtherObjects[k]))
		}
		fmt.Fprintf(&b, "  other unreachable objects: %s\n", strings.Join(parts, " "))
	}
	if c.Undescribed > 0 {
		fmt.Fprintf(&b, "  UNDESCRIBED: %d unreachable commit(s) had no `git log` row\n", c.Undescribed)
	}
	for _, n := range c.Notes {
		fmt.Fprintf(&b, "  note: %s\n", n)
	}
	if len(c.Tips) == 0 {
		b.WriteString("  no unreachable tip — every commit is named by a ref\n")
		return b.String()
	}
	b.WriteString("  tips (newest first):\n")
	for _, t := range c.Tips {
		fmt.Fprintf(&b, "    %s  %-8s  %-28s  archive=%-24s  %s\n",
			t.Short, ArchiveUnreachableAgeLabel(t.Age), ArchiveUnreachableHeldLabel(t),
			ArchiveUnreachableDecisionLabel(t), t.Subject)
	}
	fmt.Fprintf(&b, "  rescue one with: gopi task archive --rescue-tip <sha> --reason <%s>\n", ArchiveReasonList())
	return b.String()
}

// ArchiveRescueResult is the outcome of `--rescue-tip`: the durable ref that now
// holds the tip, or the refusal that says why nothing was written.
type ArchiveRescueResult struct {
	Tip      string `json:"tip"`
	Branch   string `json:"branch"`
	Tag      string `json:"tag"`
	Reason   string `json:"reason"`
	Refusal  string `json:"refusal,omitempty"`
	Recorded bool   `json:"recorded"`
	// NonDurableHolders names the refs that DO point at the tip while not being
	// durable holders (the pushback lane's per-pass mirrors): a rescue over one
	// of those is legitimate — the mirror goes away by itself — and saying so
	// is what keeps the guard's decision legible (the sibling fix's lesson).
	NonDurableHolders []string `json:"non_durable_holders,omitempty"`
}

// ArchiveRescueTip gives ONE unreachable tip a DURABLE ref: it mints
// refs/tags/archive/<archiveUnreachableBranchPrefix><short-sha> and the
// companion reason record, through ArchiveTagReasoned — the project's ONE
// archive writer (Rule 9: no second ref-minting path exists, and this function
// deliberately adds none; it is the writer's AT-A-REVISION door, which the
// writer itself needed because a rescued commit is by definition named by no
// branch).
//
// It REFUSES — writing nothing — when a DURABLE ref already holds the commit
// (nothing to rescue; the refusal names the holders), and it proceeds while
// only NON-durable holders exist (they are pruned by their own pass, which is
// exactly how the tip became unreachable in the first place). The durable
// predicate is branchDeleteNonDurableRefPrefixes, the same one the sibling
// producer-side guard decides on — read from there, never restated here
// (Rule 13).
//
// It does NOT judge whether the tip is WORTH keeping: that is policy and it is
// the caller's (an operator's) decision, which is why the reason code is
// mandatory and comes from the closed vocabulary.
func ArchiveRescueTip(repo, tip, reason, detail string) (ArchiveRescueResult, error) {
	res := ArchiveRescueResult{Tip: strings.TrimSpace(tip)}
	if res.Tip == "" {
		return res, fmt.Errorf("rescue: a tip revision is required")
	}
	sha, err := repoSweepGit(repo, gitRevParseVerb, gitVerifyArg, gitQuietArg, res.Tip+archivePeelToCommitSuffix)
	if err != nil {
		return res, fmt.Errorf("rescue: %q does not resolve to a commit in %s: %w", res.Tip, repo, err)
	}
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return res, fmt.Errorf("rescue: %q resolved to no commit in %s", res.Tip, repo)
	}
	holders, err := repoSweepGit(repo, gitForEachRefVerb, gitContainsArg, sha, gitFormatFlagPrefix+archiveRefNameFormat)
	if err != nil {
		return res, fmt.Errorf("rescue: cannot enumerate the refs holding %s: %w", sha, err)
	}
	var durable []string
	for _, ref := range strings.Split(holders, "\n") {
		ref = strings.TrimSpace(strings.TrimRight(ref, "\r"))
		if ref == "" {
			continue
		}
		if branchDeleteNonDurableRef(ref) {
			res.NonDurableHolders = append(res.NonDurableHolders, ref)
			continue
		}
		durable = append(durable, ref)
	}
	sort.Strings(res.NonDurableHolders)
	sort.Strings(durable)
	if len(durable) > 0 {
		res.Refusal = fmt.Sprintf(
			"rescue refused: %s is already held by a durable ref (%s) — there is nothing to rescue, and minting a "+
				"second durable ref for one commit would give one tip two names to keep in step.",
			archiveShortSHA(sha), strings.Join(durable, ", "))
		return res, nil
	}
	res.Branch = archiveUnreachableBranchPrefix + archiveShortSHA(sha)
	if !ArchiveReasonKnown(reason) {
		return res, fmt.Errorf("rescue: unknown reason %q (want %s)", reason, ArchiveReasonList())
	}
	tag, err := archiveTagReasonedAt(repo, res.Branch, sha, reason, detail)
	if err != nil {
		return res, err
	}
	res.Tag, res.Reason, res.Recorded = tag, reason, true
	return res, nil
}

// ArchiveRescueReport renders the rescue outcome for the CLI. A refusal is
// printed as the refusal (the operator reads the reason they were given), and
// the positive case names BOTH refs the rescue wrote, so the operator can read
// the decision back rather than trust that a tag appeared.
func ArchiveRescueReport(res ArchiveRescueResult) string {
	if !res.Recorded {
		return res.Refusal + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "rescued %s as %s\n", archiveShortSHA(res.Tip), res.Tag)
	fmt.Fprintf(&b, "  reason: %s\n", res.Reason)
	fmt.Fprintf(&b, "  durable holder: %s%s\n", archiveTagRefPrefix, res.Branch)
	for _, ref := range res.NonDurableHolders {
		fmt.Fprintf(&b, "  ignored non-durable holder: %s (pruned by its own pass)\n", ref)
	}
	fmt.Fprintf(&b, "  read back: git cat-file blob %s%s\n", archiveReasonRefPrefix, res.Branch)
	return b.String()
}
