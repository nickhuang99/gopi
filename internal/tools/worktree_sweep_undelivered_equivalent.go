package tools

// worktree_sweep_undelivered_equivalent.go — the DATA SOURCE half of the
// undelivered-commits rung (worktree_sweep_undelivered.go): a commit that a
// ground's own, never-fetched `origin/master` snapshot does not contain may
// nonetheless be DELIVERED, because the integration branch on the MAIN checkout
// carries it under another commit id (a rebase rewrites the hash) or under
// another subject (a re-land rewrites the message).
//
// THE DEFECT THIS CLOSES, measured on this host 2026-10-27 (bug row 01a17d1b,
// the ground /home/nick/workspace/01a17a5f-…):
//
//	ground's  refs/remotes/origin/master = 2c8710d  06:29:29   (never fetched)
//	main repo's master                   = 934dd697d 19:23:41   (+12h54m)
//	208 clone grounds, 206 of them carrying a stale snapshot
//
// so `git rev-list --count <branch> --not refs/remotes/origin/master` answers
// "2 commits nobody has" for work that is ON master. The keep sentence is then
// false, and it is PERMANENT (a commit count relaxes with nothing: not age, not
// a dead process), so the ground is never reclaimed and its row sticks at
// in_progress. The rung's PREDICATE is right — "a branch holding commits no
// delivery point has must be kept" — and stays exactly as it is; what changed is
// WHERE the delivery question is asked.
//
// WHY THE FIX IS NOT "fetch in the sweep" (Rule 35, and the epic's own fence):
// a fetch per ground turns a read-only PLAN operation into a network operation
// over 208 directories, and adds a persistent dependency (network reachability)
// to a decision that must work offline. Instead the sweep reads the integration
// tip from the repo it is ALREADY standing in — the main checkout, whose master
// the operator/merger keeps current — and asks the equivalence question the two
// object stores can actually answer:
//
//	LEG ① SUBJECT EQUALITY. The ground's commit has a signal-bearing subject
//	  (reworkLandingSubjectCarriesSignal: long enough, not repo bookkeeping) and
//	  the fresh integration tip carries a commit with EXACTLY that subject. This
//	  is the discriminator this repository already measured and documented
//	  (rework_landing_probe.go: patch-ids and `git cherry` report an
//	  already-landed change as patch-unique; the subject fires). Measured on the
//	  case above: ground 84cfb6f ↔ master 3acbb60dd, subjects byte-identical,
//	  patch-ids 1001d391… vs 3b143db1… — i.e. the patch leg would NOT fire.
//
//	LEG ② CONTENT CONTAINMENT. Every line the ground's commit ADDS is already
//	  present in the fresh tip's copy of every file the commit touches, and every
//	  line it REMOVES is already gone. This covers the re-landed-with-a-new-message
//	  shape the subject leg misses; measured on the same ground: c529b09's subject
//	  ("classify the no-alarm totality assertion for the audit gate") does NOT
//	  exist on master, while its one added line IS in master's
//	  cmd/agent/task_ledger_agreement_structural_test.go, re-landed there as
//	  6e3191153 ("re-point the … at its new carrier").
//
// WHY TWO LEGS AND NOT ONE. Each alone leaves the measured case half-kept: the
// subject leg cannot see a rewritten message, the content leg cannot see a
// rebased-away rename (84cfb6f's own line `a.Peer, len(a.Stats.StatusMismatch),`
// reads `a.Peer, len(s.StatusMismatch)` on master). A commit is delivered when
// EITHER leg fires.
//
// THE DIRECTION OF ERROR IS THE WHOLE SAFETY ARGUMENT, so it is stated as code
// rather than as intent: every unmeasurable input (a git failure, an unparseable
// diff, a cap exceeded, a file the fresh tip does not have) makes a leg DECLINE
// to fire, which leaves the commit counted, which KEEPS the ground. The two legs
// can only ever release a ground whose work has demonstrably reappeared somewhere
// the removal does not touch; nothing here can release one whose work has not.
//
// THE DATABASE IS READ-ONLY HERE AND QUITE DELIBERATELY STALE-TOLERANT: if the
// main checkout's master is itself behind, the legs simply do not fire and the
// rung keeps — the pre-fix behaviour, which is the safe side.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// worktreeUndeliveredSubjectFormat is the ONE format the subject set is read
	// with: the full object id, a TAB, the subject (git log's %s is the subject
	// line, never the body — the body is where the bookkeeping noise lives).
	worktreeUndeliveredSubjectFormat = "--format=%H%x09%s"

	// worktreeUndeliveredEquivCommitCap bounds how many candidate commits the
	// equivalence legs will examine in ONE registration. It bounds WORK, never
	// correctness: past the cap the remaining commits stay counted (KEEP). The
	// measured population is 1..11 commits per ground (01a17aba carries 11), so a
	// cap of 40 is ~4x the largest observed shape.
	worktreeUndeliveredEquivCommitCap = 40

	// worktreeUndeliveredEquivFileCap bounds the files ONE commit's content leg
	// reads from the fresh tip. Past it the commit stays counted (KEEP) — a
	// diff that wide is a change no containment test can honestly judge anyway.
	worktreeUndeliveredEquivFileCap = 24

	// worktreeUndeliveredEquivLineCap bounds the changed lines ONE commit's
	// content leg compares. Same direction: past it, KEEP.
	worktreeUndeliveredEquivLineCap = 4000

	// The diff headers the content leg parses. `--unified=0` keeps the hunk
	// bodies to the changed lines themselves, which is what "containment" means.
	worktreeUndeliveredUnifiedFlag = "--unified=0"
	worktreeUndeliveredNoColorFlag = "--no-color"
	worktreeUndeliveredFormatNone  = "--format="
	worktreeUndeliveredDiffAddPref = "+"
	worktreeUndeliveredDiffDelPref = "-"
	worktreeUndeliveredPlusHeader  = "+++ "
	worktreeUndeliveredMinusHeader = "--- "
	worktreeUndeliveredHunkHeader  = "@@"
	worktreeUndeliveredNoNewline   = "\\ "
	worktreeUndeliveredDevNullPath = "/dev/null"
)

// worktreeUndeliveredFreshPoints resolves the FRESH delivery sources inside the
// repo the sweep is standing in: the integration branch's local ref and the
// remote-tracking copy of it. Both are legitimate delivery points — the merger's
// own target and the server's copy of master — and neither is fetched here.
//
// THE GROUND'S OWN BRANCH IS NOT A CANDIDATE, and the exclusion is the same one
// the caller's delivery-point builder documents: a ref that always resolves to
// the thing being measured answers 0 by construction. When readFrom IS the fresh
// repo (a linked worktree shares it), the equivalence legs are skipped outright
// — the caller's delivery points already cover that shape.
func worktreeUndeliveredFreshPoints(ctx context.Context, freshFrom string) []string {
	freshFrom = strings.TrimSpace(freshFrom)
	if freshFrom == "" {
		return nil
	}
	candidates := []string{
		refsHeadsPrefix + IntegrationBranchName,
		refsRemotesPrefix + deliveryProofOriginRemote + "/" + IntegrationBranchName,
	}
	out := make([]string, 0, len(candidates))
	for _, ref := range candidates {
		if _, err := worktreeUndeliveredGit(ctx, freshFrom, "rev-parse", "--verify", "--quiet", ref); err != nil {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// worktreeUndeliveredFreshSubjectSet answers "which commit on the fresh
// integration tip carries subject S?" in ONE git invocation over the whole
// history. A subject that appears twice maps to the newest commit that carries
// it (git log's own order), which is the one the evidence sentence should name.
func worktreeUndeliveredFreshSubjectSet(ctx context.Context, freshFrom string, refs []string) map[string]string {
	if len(refs) == 0 {
		return nil
	}
	args := append([]string{"log", worktreeUndeliveredSubjectFormat}, refs...)
	out, err := worktreeUndeliveredGit(ctx, freshFrom, args...)
	if err != nil {
		return nil
	}
	set := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, subject, ok := reworkLandingSplitLogLine(line)
		if !ok {
			continue
		}
		if _, seen := set[subject]; !seen {
			set[subject] = sha
		}
	}
	return set
}

// worktreeUndeliveredEquivalent is the legs' ANSWER for one candidate commit.
type worktreeUndeliveredEquivalent struct {
	// Delivered reports whether one of the legs fired.
	Delivered bool
	// Evidence names the leg, the fresh ref, and the commit it matched, so the
	// keep/release record can be reproduced by hand (Rule 15: a claim nobody can
	// check is noise).
	Evidence string
}

// worktreeUndeliveredCommitDelivered asks both legs about ONE ground commit.
// Every failure is a DECLINE (Delivered=false with the reason in Evidence) —
// never a silent release.
func worktreeUndeliveredCommitDelivered(ctx context.Context, readFrom, freshFrom, sha string, refs []string, subjects map[string]string) worktreeUndeliveredEquivalent {
	subject, err := worktreeUndeliveredGit(ctx, readFrom, "log", "-1", worktreeUndeliveredSubjectFormatOnly(), sha)
	if err != nil {
		return worktreeUndeliveredEquivalent{Evidence: "subject of " + worktreeUndeliveredShort(sha) + " unreadable (" + err.Error() + ")"}
	}
	subj := worktreeUndeliveredSubjectOf(subject, sha)

	// LEG ① — subject equality, with this repository's own noise rules.
	if reworkLandingSubjectCarriesSignal(subj) {
		if landed := subjects[subj]; landed != "" {
			return worktreeUndeliveredEquivalent{
				Delivered: true,
				Evidence:  "subject-equivalent to " + worktreeUndeliveredShort(landed) + " on " + strings.Join(refs, ", "),
			}
		}
	}
	// LEG ② — content containment in the fresh tip's own files.
	for _, ref := range refs {
		evidence, ok := worktreeUndeliveredContentContained(ctx, readFrom, freshFrom, ref, sha)
		if ok {
			return worktreeUndeliveredEquivalent{Delivered: true, Evidence: evidence}
		}
	}
	return worktreeUndeliveredEquivalent{Evidence: "not equivalent to anything on " + strings.Join(refs, ", ")}
}

// worktreeUndeliveredSubjectFormatOnly is `--format=%s` — the subject alone, the
// one field both the signal rule and the subject set speak about.
func worktreeUndeliveredSubjectFormatOnly() string { return "--format=%s" }

// worktreeUndeliveredShort is this file's ONE short-id spelling (7 chars, or all
// of a shorter id), used only in evidence text a human reads.
func worktreeUndeliveredShort(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// worktreeUndeliveredSubjectOf reads the subject out of a `%H\t%s` line, falling
// back to the whole line for the `%s`-only form (where no tab exists).
func worktreeUndeliveredSubjectOf(out, sha string) string {
	line := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	if _, subject, ok := reworkLandingSplitLogLine(line); ok && strings.HasPrefix(line, sha[:min(len(sha), 7)]) {
		return subject
	}
	return line
}

// worktreeUndeliveredContentContained asks the containment question for one
// commit against one fresh ref: are all of its added lines already in the fresh
// tip's copies of the files it touches, and all of its removed lines already
// gone?
func worktreeUndeliveredContentContained(ctx context.Context, readFrom, freshFrom, ref, sha string) (string, bool) {
	diff, err := worktreeUndeliveredGit(ctx, readFrom, "show", sha,
		worktreeUndeliveredFormatNone, worktreeUndeliveredUnifiedFlag, worktreeUndeliveredNoColorFlag)
	if err != nil {
		return "", false
	}
	files, ok := worktreeUndeliveredParseDiff(diff)
	if !ok || len(files) == 0 || len(files) > worktreeUndeliveredEquivFileCap {
		return "", false
	}
	added, removed := 0, 0
	for path, change := range files {
		blob, err := worktreeUndeliveredGit(ctx, freshFrom, "show", ref+":"+path)
		if err != nil {
			// The fresh tip does not have this file, so its lines cannot be there.
			return "", false
		}
		present := map[string]bool{}
		for _, line := range strings.Split(blob, "\n") {
			present[strings.TrimSpace(line)] = true
		}
		for _, line := range change.added {
			added++
			if !present[strings.TrimSpace(line)] {
				return "", false
			}
		}
		for _, line := range change.removed {
			removed++
			if present[strings.TrimSpace(line)] {
				return "", false
			}
		}
	}
	if added+removed == 0 || added+removed > worktreeUndeliveredEquivLineCap {
		return "", false
	}
	return fmt.Sprintf("content already in %s (%d changed line(s) across %d file(s))", ref,
		added+removed, len(files)), true
}

// worktreeUndeliveredFileChange is one file's changed lines within one commit.
type worktreeUndeliveredFileChange struct {
	added   []string
	removed []string
}

// worktreeUndeliveredParseDiff splits a `git show --unified=0` body into its
// per-file added/removed lines. ok is false when the diff cannot be trusted (a
// binary file, content before any hunk header) — a decline, never a release.
//
// THE BODY FLAG IS THE POINT, not the line prefixes: with --unified=0 a hunk
// contributes ONLY its changed lines, so every line after a `@@` header until
// the next `diff --git` is content. Keying on the prefixes alone would misread a
// REMOVED line whose text begins with `-- ` (it arrives as `--- …`) as a file
// header, and a misread header silently drops a removal from the containment
// question — i.e. it would ANSWER for a file it never read.
func worktreeUndeliveredParseDiff(diff string) (map[string]*worktreeUndeliveredFileChange, bool) {
	out := map[string]*worktreeUndeliveredFileChange{}
	cur := ""
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur, inHunk = "", false
		case strings.HasPrefix(line, "Binary files "):
			return nil, false
		case strings.HasPrefix(line, worktreeUndeliveredPlusHeader):
			if inHunk {
				out[cur].added = append(out[cur].added, line[1:])
				continue
			}
			path := strings.TrimSpace(strings.TrimPrefix(line, worktreeUndeliveredPlusHeader))
			if path == worktreeUndeliveredDevNullPath {
				cur = ""
				continue
			}
			cur = strings.TrimPrefix(path, "b/")
			if out[cur] == nil {
				out[cur] = &worktreeUndeliveredFileChange{}
			}
		case strings.HasPrefix(line, worktreeUndeliveredHunkHeader):
			inHunk = true
		case inHunk:
			switch {
			case strings.HasPrefix(line, worktreeUndeliveredDiffAddPref):
				if cur == "" {
					return nil, false
				}
				out[cur].added = append(out[cur].added, strings.TrimPrefix(line, worktreeUndeliveredDiffAddPref))
			case strings.HasPrefix(line, worktreeUndeliveredDiffDelPref):
				if cur == "" {
					return nil, false
				}
				out[cur].removed = append(out[cur].removed, strings.TrimPrefix(line, worktreeUndeliveredDiffDelPref))
			case strings.HasPrefix(line, worktreeUndeliveredNoNewline):
				// "\ No newline at end of file" is metadata, not content.
			case line == "":
				// --unified=0 emits no blank context line; a blank line here is
				// git's own separator between the diff and the next section.
			default:
				// A context line where the flag says there are none: refuse to
				// guess what it belongs to.
				return nil, false
			}
		}
	}
	return out, true
}

// worktreeUndeliveredSubtractEquivalent reduces a probe's answer by the commits
// the legs can prove are delivered elsewhere. It NEVER touches a probe it cannot
// measure, and it returns the probe unchanged when `freshFrom` names the same
// directory the candidates were read from (that shape is the caller's own
// delivery-point question, answered already).
func worktreeUndeliveredSubtractEquivalent(readFrom, freshFrom, branch string, p worktreeUndeliveredProbe) worktreeUndeliveredProbe {
	if p.Ahead == 0 || len(p.Commits) == 0 {
		return p
	}
	if strings.TrimSpace(freshFrom) == "" ||
		scratch.CanonicalPath(readFrom) == scratch.CanonicalPath(freshFrom) {
		return p
	}
	if len(p.Commits) > worktreeUndeliveredEquivCommitCap {
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	refs := worktreeUndeliveredFreshPoints(ctx, freshFrom)
	if len(refs) == 0 {
		return p
	}
	subjects := worktreeUndeliveredFreshSubjectSet(ctx, freshFrom, refs)
	remaining := make([]string, 0, len(p.Commits))
	delivered := make([]string, 0, len(p.Commits))
	for _, sha := range p.Commits {
		eq := worktreeUndeliveredCommitDelivered(ctx, readFrom, freshFrom, sha, refs, subjects)
		if eq.Delivered {
			delivered = append(delivered, worktreeUndeliveredShort(sha)+" — "+eq.Evidence)
			continue
		}
		remaining = append(remaining, sha)
	}
	p.Ahead = len(remaining)
	p.Commits = remaining
	p.Equivalent = delivered
	sort.Strings(p.Equivalent)
	return p
}
