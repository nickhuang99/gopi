package tools

// task_delivery_proof_content.go — the CONTENT-EQUIVALENCE arm of the delivery
// proof, and the DIFF CLASSIFIER a refusal carries.
//
// WHY THIS ARM EXISTS (measured 2026-10-27, three real grounds). The clone
// channel delivers with `pull --rebase → build → push`, so a run's commits are
// REPLAYED onto a destination that has moved. Two things then happen to the
// commit identity, and NEITHER is a delivery failure:
//
//	(a) the sha is rewritten — ancestry (`merge-base --is-ancestor`) answers no;
//	(b) `git patch-id --stable` changes when the surrounding context changes —
//	    one blank line or one comment line introduced by the rebase re-derives
//	    the patch, so `git cherry` marks the patch ABSENT as well.
//
// Measured on the three grounds this criterion was reported against:
//
//	ground 01a17a67  50b57f8  its patch-id f9fbd4d1… EQUALS master 5e3ce2e18's,
//	                          yet `git cherry 7734b63 HEAD` prints `+` for it
//	                 34f8039  its table differs from the point's only in
//	                          `measured-at:` plus rows the point re-derived from
//	                          ITS own base — no patch-id can ever match that
//	ground 01a17a5f  84cfb6f  the same change landed on master as 3acbb60dd; the
//	                          ground's copy differs by one blank line and a
//	                          constant's reader
//	                  c529b09  a one-line test comment, byte-identical to the
//	                          destination's blob, and `git cherry` still says `+`
//	ground 01a17abb          the ground's four commits were ADOPTED onto master
//	                          under other shas (1ae64e138/93c450195/…)
//
// So the criterion needs a THIRD witness that compares CONTENT, at the level of
// the CHANGE a commit makes, with the normalisations a rebase is allowed to
// introduce: blank lines, comment-only lines, trailing whitespace, and
// self-referential measurement metadata (`measured-at:` — a row recording the
// revision it was measured at is GUARANTEED to change on every rebase).
//
// WHAT THE ARM ANSWERS, narrowly: for ONE ground commit and ONE destination
// tree, every line the commit ADDED (normalised) must be present in the
// destination's version of that file; a deletion is landed when the destination
// no longer carries the path. A commit whose additions are all there has
// delivered its change. A commit whose additions are missing has not — whatever
// its sha or patch-id says — and the classifier names WHICH lines are missing and
// WHICH destination commit came closest.
//
// IT CANNOT RESCUE AN UNDELIVERED ROW: a commit whose work exists nowhere has
// every added line absent at the destination, so the arm answers false and the
// refusal quotes exactly what is missing (measured: ground 01a17abb's
// `task_delivery_proof.go` carries functions master has not got).
//
// TWO DIRECTORIES, ON PURPOSE. A clone ground answers for its own commits and the
// destination tree answers for the destination, and they need not be the same
// repository: a ground that never fetched the point's tip has the COMMITS but not
// the destination's objects, while the recorded repo has the destination and not
// the ground's commits. Reading each side where it exists is the only way to
// compare them WITHOUT a fetch — and this arm never fetches, never pushes, never
// moves a ref and never writes: every invocation below is `rev-parse`,
// `rev-list`, `diff-tree`, `show`, `log` or `merge-base`.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// The arm's names (Rule 1b/13: one spelling each, referenced everywhere).
const (
	// deliveryProofContentScanCommits bounds how many of the ground's own
	// commits this arm walks. A range larger than this is not a question it
	// answers, and it says so rather than running long.
	deliveryProofContentScanCommits = 32
	// deliveryProofContentClosestCandidates bounds the destination-side history
	// scan behind a refusal's "closest commit" line AND behind the historical
	// landing test (per path, newest first).
	deliveryProofContentClosestCandidates = 8
	// deliveryProofContentPickaxeCandidates bounds how many content-located
	// destination commits (git log -S) are CONFIRMED per probe line: a hit is a
	// candidate, and the blob read behind it is what decides.
	deliveryProofContentPickaxeCandidates = 8
	// deliveryProofContentPickaxeProbes bounds how many of the added lines are
	// used as -S search terms, so a many-line change cannot turn the content
	// comparison into a scan of the destination's history.
	deliveryProofContentPickaxeProbes = 4
	// deliveryProofContentPathsInSummary bounds how many files a summary lists
	// before it says how many more there are.
	deliveryProofContentPathsInSummary = 6
	// deliveryProofContentDetailInSummary bounds how many differing lines are
	// quoted per file, so a refusal stays readable instead of dumping a diff.
	deliveryProofContentDetailInSummary = 3
	// deliveryProofUnifiedZeroContext is the `--unified=0` spelling: the patch
	// carries NO context line, so every `+` line is an ADDITION of this commit
	// and never a context line mistaken for one.
	deliveryProofUnifiedZeroContext = "--unified=0"
	// deliveryProofRevParseVerb / deliveryProofNumstatFlag are the BINARY
	// adjudication's vocabulary (deliveryProofPathIsBinary /
	// deliveryProofBinaryLanded below): `git rev-parse <rev>:<path>` prints the
	// BLOB ID (content identity, never commit identity), and
	// `git diff-tree --numstat` prints `-\t-\t<path>` for a file git calls
	// binary — git's own statement of that fact, not a heuristic of ours.
	deliveryProofRevParseVerb  = "rev-parse"
	deliveryProofNumstatFlag   = "--numstat"
	deliveryProofBinaryNumstat = "-\t-"
	// deliveryProofMetadataMarker is the marker that BEGINS a self-referential
	// measurement metadata line — the row a rebase must change. It is an
	// ANCHORED prefix, never a sub-string: measured (2026-10-28) a Go WORK line
	// that merely NAMES this marker (`deliveryProofMetadataMarker = "measured-at:"`)
	// normalised to ZERO lines under `strings.Contains`, so a commit adding it
	// would have been judged content-free at the destination and delivered by
	// nothing. Every real measurement row in this tree BEGINS with the marker
	// (the constant that spelled it was `MeasuredAtPrefix` = "# measured-at:",
	// which lived in `internal/configanchor` until that package was deleted in
	// `1e3b0976` — `78bdf05a` had already removed its callers, the generators and
	// the `--check` legs; measured on this tree, `git cat-file -e
	// HEAD:internal/configanchor` fails. The row shape it described survives in
	// PROJECT.md Rule 38 and in CONFIG.md's own history), so anchoring
	// costs no legitimate rescue and closes that hole.
	deliveryProofMetadataMarker = "measured-at:"
)

// deliveryProofCommentPrefixes is THE comment-only prefix list (Rule 13), one
// spelling for the normaliser: a trimmed line starting with any of these carries
// no work. It covers the languages a ground in this repository carries (Go,
// shell, SQL, YAML/TOML, and the HTML comment inside markdown/TSV).
//
// `*` IS NOT IN THIS LIST, and that is a measured repair rather than a tidy-up
// (2026-10-28). The byte is the ONLY prefix whose meaning is decided by what
// FOLLOWS it, so a bare prefix entry could not judge it: it dropped every Go
// DEREFERENCE / pointer assignment along with the block-comment continuations.
// The two shapes share the first byte and mean opposite things —
//
//	*state += chatBannerSeedMixA     a deref assignment: WORK
//	 * a block comment continuation  inside `/* … */`: not work
//
// — and dropping the first kind made a commit whose additions are ALL deref
// lines normalise to ZERO lines, i.e. `content-free`, i.e. LANDED at every
// destination without a comparison (measured on 72 real deref lines in this
// tree; the `len(added) == 0` branch of deliveryProofCommitContentLanded). That
// is the false-positive direction, the dangerous one: a run that delivered
// nothing reached `completed`. The shape test is
// deliveryProofIsBlockCommentContinuation, and `*/` stays here on its own merit
// — a lone close marker has no deref reading.
var deliveryProofCommentPrefixes = []string{"//", "#", "/*", "*/", "-->", "--", ";;"}

// The `*` SHAPE criterion (Rule 1/13): ONE marker, ONE gap, ONE predicate. A
// trimmed line beginning with the marker is a block-comment continuation when
// what follows is whitespace or end-of-line, and a deref/pointer assignment
// otherwise (an identifier, `(`, a second marker for `**p = …`).
const (
	// deliveryProofBlockCommentStar is the ONE byte the two shapes share.
	deliveryProofBlockCommentStar = "*"
	// deliveryProofBlockCommentGap is the space that follows it in a reflowed
	// block comment (` * a note`, the shape a `gofmt`/rebase produces).
	deliveryProofBlockCommentGap = " "
	// deliveryProofBlockCommentIndentGap is the same gap spelled with a tab, so
	// an indented continuation is a continuation and not work (a rebase that
	// re-indents a comment block must not be refused as undelivered).
	deliveryProofBlockCommentIndentGap = "\t"
)

// deliveryProofIsBlockCommentContinuation is THE `*` shape test: the marker
// followed by whitespace or end-of-line is a comment continuation (drop it),
// followed by anything else is deref work (keep it).
//
// It is DELIBERATELY shape-only: the normaliser sees one trimmed line at a time
// and never parses Go, so it cannot ask "am I inside `/* … */`". The shape is
// what distinguishes the two spellings in practice, which is why this is a
// criterion rather than a language parser (Rule 35: no new dependency).
func deliveryProofIsBlockCommentContinuation(line string) bool {
	if !strings.HasPrefix(line, deliveryProofBlockCommentStar) {
		return false
	}
	rest := strings.TrimPrefix(line, deliveryProofBlockCommentStar)
	return rest == "" ||
		strings.HasPrefix(rest, deliveryProofBlockCommentGap) ||
		strings.HasPrefix(rest, deliveryProofBlockCommentIndentGap)
}

// deliveryProofCommentOnlyLine is THE judgement the normaliser asks (Rule 13):
// the `*` shape rule first, then the bare prefix list. Keeping it in one
// function is what stops a second reader from re-deriving "is this a comment"
// with a different spelling.
func deliveryProofCommentOnlyLine(line string) bool {
	if deliveryProofIsBlockCommentContinuation(line) {
		return true
	}
	for _, prefix := range deliveryProofCommentPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// The difference CLASSES a refusal names, so a reader can tell "the work is
// missing" from "only the measured base changed" without re-deriving anything.
const (
	// deliveryProofClassAbsent: the path itself is not at the destination (or
	// the commit deletes it and the destination still carries it).
	deliveryProofClassAbsent = "absent-at-destination"
	// deliveryProofClassLinesMiss: the path is there but work lines are not —
	// at the destination's TIP nor in the bounded history behind it.
	deliveryProofClassLinesMiss = "added-lines-missing"
	// deliveryProofClassOutOfWindow: the bounded destination scan RAN OUT before
	// it reached the commit that would have decided the question (or the root of
	// the path's history). It is the one class that is NOT a finding about the
	// work: a caller must report "not measured", never "not landed" (§ the
	// bounded reader's three-valued answer, deliveryProofBoundedWindowExhausted).
	deliveryProofClassOutOfWindow = "outside-the-bounded-window"
)

// deliveryProofOutOfWindowDetail is the ONE sentence behind that class: what the
// scan did, and what its silence does NOT mean.
const deliveryProofOutOfWindowDetail = "the bounded destination scan ran out of history before this path could be " +
	"settled — the work may be behind the window"

// deliveryProofBounded is the THREE-VALUED answer of a BOUNDED destination scan,
// and it exists because a window's silence is a fact about the window, never
// about the work. Two of these values are answers; the third says the question
// was not answered at all, and no caller may read it as a refusal.
type deliveryProofBounded int

const (
	// deliveryProofBoundedAbsent — the scan reached the ROOT of the path's
	// history and nothing there carried the content: a MEASURED negative.
	deliveryProofBoundedAbsent deliveryProofBounded = iota
	// deliveryProofBoundedCarried — a destination commit carries the content.
	deliveryProofBoundedCarried
	// deliveryProofBoundedOutOfWindow — the window ran out before the question
	// could be decided. NOT MEASURED. Callers report this as judged=false; the
	// one thing it must never become is "not landed" (the defect this repairs:
	// an 8-commit window is a guess about how far the destination has moved, and
	// a guess that expires is not evidence the work is missing).
	deliveryProofBoundedOutOfWindow
)

// deliveryProofBoundedWindowExhausted decides the third value: did the bounded
// scan run out of history BEFORE the question could be settled, or did it reach
// the ROOT of the path's history and settle it?
//
// walked is how many commits that scan itself saw (`git log -n <window>`), and
// the test is a SECOND, WIDER read built from the SAME constant — so this repair
// is a change of SEMANTICS, never a widening of the window: `+1` asks "is there
// even one more commit on this path than I looked at?", which stays meaningful at
// any width. walked < window means the window was NOT reached and the whole
// path-history was read, so `wider` can never exceed it.
//
// An unreadable read answers exhausted: a history nobody could read must not
// become a finding (Rule 15).
func deliveryProofBoundedWindowExhausted(ctx context.Context, destDir, destRef, path string,
	walked int,
) bool {
	wider := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates+1), destRef, "--", path)
	return len(strings.Fields(wider)) > walked
}

// deliveryProofCommitUnmeasuredSentence is the WHY a bounded reader owes when it
// declines to judge: which path's window ran out, and the fact that the work may
// still be there. Without it a caller sees judged=false with no reason to
// distinguish "git could not answer" from "git answered about too little".
func deliveryProofCommitUnmeasuredSentence(commit, destRef string, diffs []deliveryProofContentFileDiff) string {
	lines := make([]string, 0, deliveryProofContentPathsInSummary+1)
	quoted := 0
	for _, d := range diffs {
		if d.Class != deliveryProofClassOutOfWindow {
			continue
		}
		if quoted == deliveryProofContentPathsInSummary {
			lines = append(lines, fmt.Sprintf("(+%d more file(s))", len(diffs)-quoted))
			break
		}
		quoted++
		lines = append(lines, fmt.Sprintf("%s — %s: %s", d.Path, d.Class, d.Detail))
	}
	return fmt.Sprintf("commit %s cannot be judged against %s — NOT MEASURED, which is not a finding that the work is "+
		"missing: the bounded destination scan ran out of history before these could be settled [%s]",
		branchDeleteShortHash(commit), destRef, strings.Join(lines, " | "))
}

// deliveryProofContentFileDiff is ONE file's verdict inside one commit. It keeps
// the missing lines THEMSELVES, so ranking a candidate destination commit and
// rendering the refusal read the same measurement instead of re-parsing prose.
type deliveryProofContentFileDiff struct {
	Path    string
	Class   string
	Detail  string
	Missing []string
}

// deliveryProofContentArmOff is the A/B switch the red proof flips. A test that
// only proves the new arm PASSES proves nothing about whether the arm is doing
// the work — so the arm asks this ONE function, and the red proof turns it off
// and watches the three should-pass fixtures go back to REFUSED.
var deliveryProofContentArmOff bool

// deliveryProofContentArmEnabled reports whether the content arm may run. Its
// single falsifier is the red proof's own switch above.
func deliveryProofContentArmEnabled() bool {
	return !deliveryProofContentArmOff
}

// deliveryProofNormalizeLines is THE content normaliser (Rule 14): it drops what
// a rebase may legitimately change and keeps what the work IS — blank lines,
// comment-only lines, trailing whitespace, and self-referential measurement
// metadata.
//
// The metadata test is ANCHORED (HasPrefix), never `Contains`: a work line that
// merely MENTIONS the marker is work, and dropping it would let a commit that
// adds it be judged content-free at the destination (measured 2026-10-28 — the
// Go line `deliveryProofMetadataMarker = "measured-at:"` normalised to zero
// lines under `Contains`). The census headers this rule exists for are
// `# measured-at: …`, which the comment prefixes below drop on their own, and a
// bare `measured-at: …` row (YAML/TOML) is anchored at position 0 — so the
// anchoring narrows the rule to the shapes it names without losing one of them.
func deliveryProofNormalizeLines(text string) []string {
	var out []string
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, deliveryProofMetadataMarker) {
			continue
		}
		// ONE judgement (Rule 13): deliveryProofCommentOnlyLine owns BOTH the
		// `*` shape rule and the bare prefix list, so this loop asks it and
		// nothing else — a second spelling of "is this a comment" here is how
		// the two halves drifted apart in the first place.
		if deliveryProofCommentOnlyLine(line) {
			continue
		}
		out = append(out, line)
	}
	return out
}

// deliveryProofNormalizedSet is the normaliser as a LOOKUP, which is what the
// per-line containment test needs.
func deliveryProofNormalizedSet(text string) map[string]bool {
	set := make(map[string]bool)
	for _, line := range deliveryProofNormalizeLines(text) {
		set[line] = true
	}
	return set
}

// deliveryProofBlobText reads "<rev>:<path>" out of dir. ok=false means git
// could not answer, and an unreadable side is NEVER an equality (Rule 15: the
// caller reports Unknown, never a delivery).
func deliveryProofBlobText(ctx context.Context, dir, rev, path string) (string, bool) {
	if strings.TrimSpace(rev) == "" || strings.TrimSpace(path) == "" {
		return "", false
	}
	out, err := branchDeleteGit(ctx, dir, "show", rev+":"+path)
	if err != nil {
		return "", false
	}
	return out, true
}

// deliveryProofCommitAddedLines is the lines ONE commit ADDS to ONE path,
// normalised. It runs in the directory that holds the commit, against the
// commit's own parent, so the reading is what THIS commit did rather than what
// the branch as a whole ended up with.
func deliveryProofCommitAddedLines(ctx context.Context, dir, commit, path string) []string {
	out := deliveryProofGitRead(ctx, dir, "show", deliveryProofUnifiedZeroContext, "--format=", commit, "--", path)
	if out == "" {
		return nil
	}
	var added []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "++") {
			continue
		}
		added = append(added, line[1:])
	}
	return deliveryProofNormalizeLines(strings.Join(added, "\n"))
}

// deliveryProofCommitPaths is the (status, path) list ONE commit touches, so a
// deletion is not mistaken for an absence.
func deliveryProofCommitPaths(ctx context.Context, dir, commit string) ([][2]string, bool) {
	out, err := branchDeleteGit(ctx, dir, "diff-tree", "-r", "--root", "--no-commit-id", "--name-status", commit)
	if err != nil {
		return nil, false
	}
	var paths [][2]string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		paths = append(paths, [2]string{fields[0], fields[len(fields)-1]})
	}
	return paths, true
}

// deliveryProofPathIsBinary reports whether git itself calls this commit's
// change to this path BINARY, by reading its own `--numstat` row for the path
// (`-\t-`). It is git's statement, never a heuristic of ours: a "does the text
// look like bytes" test would be a second, disagreeing criterion (Rule 13).
//
// ok=false (git could not answer) reads as NOT binary, which routes the path to
// the text leg — the same measurement it would have had before this repair.
func deliveryProofPathIsBinary(ctx context.Context, dir, commit, path string) bool {
	out, err := branchDeleteGit(ctx, dir, "diff-tree", "-r", "--root", "--no-commit-id",
		deliveryProofNumstatFlag, commit)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, deliveryProofBinaryNumstat) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 1 && fields[len(fields)-1] == path {
			return true
		}
	}
	return false
}

// deliveryProofBinaryLanded answers the binary half of the content test by BLOB
// IDENTITY: the blob this commit carries for the path is the blob the
// destination carries — at its tip, or at one of the bounded commits behind it
// (the branch moved past the change). Blob ids are CONTENT, so a rebase that
// rewrote the commit's sha leaves this comparison true, which is exactly the
// property the criterion is built on.
//
// It answers false when it cannot read a side; the caller reports that as a
// difference, and the seam's own three-valued answer is decided ABOVE this
// function (a commit that cannot be judged at all returns judged=false).
func deliveryProofBinaryLanded(ctx context.Context, srcDir, destDir, destRef, commit, path string,
	deepHistory bool,
) (deliveryProofBounded, string) {
	want := deliveryProofGitRead(ctx, srcDir, deliveryProofRevParseVerb, commit+":"+path)
	if want == "" {
		return deliveryProofBoundedAbsent, ""
	}
	if got := deliveryProofGitRead(ctx, destDir, deliveryProofRevParseVerb, destRef+":"+path); got == want {
		return deliveryProofBoundedCarried, destRef
	}
	out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), destRef, "--", path)
	candidates := strings.Fields(out)
	for _, c := range candidates {
		if got := deliveryProofGitRead(ctx, destDir, deliveryProofRevParseVerb, c+":"+path); got == want {
			return deliveryProofBoundedCarried, c
		}
	}
	// NOT IN THE WINDOW IS NOT "NOT THERE": the same three-valued reading the
	// text leg makes, for the same reason. Blob identity itself is EXACT, so the
	// only question left is whether the window was long enough to look — and a
	// text `-S` probe cannot answer it for arbitrary bytes (a wrong probe would
	// MANUFACTURE the determinate negative this reading removes), so the deep
	// reader gives the one honest answer available rather than a guessed one.
	if deliveryProofBoundedWindowExhausted(ctx, destDir, destRef, path, len(candidates)) {
		return deliveryProofBoundedOutOfWindow, path
	}
	return deliveryProofBoundedAbsent, ""
}

// deliveryProofCommitContentLanded judges ONE commit against the DESTINATION
// HISTORY: destDir's destRef and the bounded history behind it.
//
// TWO WAYS TO HAVE LANDED, and the second is what makes the criterion honest.
// A change is at the destination when the destination's CURRENT tip carries its
// added lines, and it is ALSO there when a destination commit behind that tip
// did — because the integration branch keeps moving, so a landed change is
// routinely SUPERSEDED afterwards (`-S` on the real case: the line
// `a.Peer, len(a.Stats.StatusMismatch),` was added by master 3acbb60dd for row
// 01a17a5f and replaced by b4bd4fa4c, which is why comparing against the tip
// alone reports a DELIVERED change as missing).
//
// judged=false means the question could not be answered (an unreadable commit, or
// one that touches nothing) — the caller must NOT read that as "undelivered".
func deliveryProofCommitContentLanded(ctx context.Context, srcDir, destDir, destRef, commit string,
	deepHistory bool,
) (landed bool, diffs []deliveryProofContentFileDiff, judged bool) {
	paths, ok := deliveryProofCommitPaths(ctx, srcDir, commit)
	if !ok || len(paths) == 0 {
		return false, nil, false
	}
	landed = true
	// unmeasured collects the paths whose BOUNDED window ran out. It is kept
	// apart from `landed=false` on purpose: a window that expired is a fact about
	// the WINDOW, and reporting it as a finding about the work is the defect this
	// three-valued reading repairs.
	var unmeasured []deliveryProofContentFileDiff
	for _, entry := range paths {
		status, path := entry[0], entry[1]
		destText, destOK := deliveryProofBlobText(ctx, destDir, destRef, path)
		if strings.HasPrefix(status, "D") {
			// The commit DELETES this path: landed when the destination does not
			// carry it either — now, or at any commit behind the tip.
			if destOK {
				switch answer, _ := deliveryProofDeletionLanded(ctx, destDir, destRef, path, deepHistory); answer {
				case deliveryProofBoundedCarried:
				case deliveryProofBoundedOutOfWindow:
					unmeasured = append(unmeasured, deliveryProofContentFileDiff{Path: path,
						Class: deliveryProofClassOutOfWindow, Detail: deliveryProofOutOfWindowDetail})
				default:
					landed = false
					diffs = append(diffs, deliveryProofContentFileDiff{Path: path, Class: deliveryProofClassAbsent,
						Detail: "this commit deletes the path and the destination still carries it"})
				}
			}
			continue
		}
		// THE BINARY-ONLY PATH — the false-positive direction, measured
		// (2026-10-28) while writing this seam's own fixture: `git show` renders a
		// changed binary file as ONE line (`Binary files … differ`), so
		// deliveryProofCommitAddedLines answers ZERO added lines for a commit whose
		// ONLY content is that binary, and the `len(added) == 0` branch below then
		// reads "content-free, nothing is owed at the destination" — i.e. LANDED
		// for a run that delivered nothing at all.
		//
		// A path git calls binary is therefore answered by BLOB CONTENT IDENTITY
		// (`git rev-parse <rev>:<path>` on both sides), the one measurement that
		// needs no text diff. It compares CONTENT, never commit identity: the two
		// sides are two independent revisions, and the rebase that rewrote the run's
		// commit leaves the blob untouched.
		if deliveryProofPathIsBinary(ctx, srcDir, commit, path) {
			switch answer, _ := deliveryProofBinaryLanded(ctx, srcDir, destDir, destRef, commit, path, deepHistory); answer {
			case deliveryProofBoundedCarried:
			case deliveryProofBoundedOutOfWindow:
				unmeasured = append(unmeasured, deliveryProofContentFileDiff{Path: path,
					Class: deliveryProofClassOutOfWindow, Detail: deliveryProofOutOfWindowDetail})
			default:
				landed = false
				diffs = append(diffs, deliveryProofContentFileDiff{Path: path, Class: deliveryProofClassAbsent,
					Detail: "this commit's binary content is not at the destination, now or in its bounded history"})
			}
			continue
		}
		added := deliveryProofCommitAddedLines(ctx, srcDir, commit, path)
		if len(added) == 0 {
			// Comment/blank/metadata only: content-free by the normaliser's own
			// definition, so nothing further is owed at the destination.
			continue
		}
		missing := deliveryProofLinesMissingAt(destText, added)
		if len(missing) == 0 {
			continue
		}
		switch answer, _ := deliveryProofHistoricallyCarried(ctx, destDir, destRef, path, added, deepHistory); answer {
		case deliveryProofBoundedCarried:
			// Landed under another identity, then superseded by a later commit on
			// the integration branch. The change DELIVERED; the branch simply
			// moved past it.
			continue
		case deliveryProofBoundedOutOfWindow:
			unmeasured = append(unmeasured, deliveryProofContentFileDiff{Path: path,
				Class: deliveryProofClassOutOfWindow, Detail: deliveryProofOutOfWindowDetail})
			continue
		}
		if !destOK {
			landed = false
			diffs = append(diffs, deliveryProofContentFileDiff{Path: path, Class: deliveryProofClassAbsent,
				Detail: "the destination carries no such path, now or in its bounded history"})
			continue
		}
		landed = false
		diffs = append(diffs, deliveryProofContentFileDiff{Path: path, Class: deliveryProofClassLinesMiss, Missing: missing,
			Detail: fmt.Sprintf("%d of %d added line(s) absent from the destination and its bounded history: %s",
				len(missing), len(added), deliveryProofQuoteLines(missing))})
	}
	if len(unmeasured) > 0 {
		// A path the bounded scan could not settle leaves the WHOLE commit
		// unanswered: a commit that is half-measured must not be reported as a
		// measured refusal, and must not be reported as landed either.
		return false, append(diffs, unmeasured...), false
	}
	return landed, diffs, true
}

// deliveryProofLinesMissingAt is the containment test: which of the added lines
// the destination text does not carry (normalised).
func deliveryProofLinesMissingAt(destText string, added []string) []string {
	if destText == "" && len(added) == 0 {
		return nil
	}
	set := deliveryProofNormalizedSet(destText)
	var missing []string
	for _, line := range added {
		if !set[line] {
			missing = append(missing, line)
		}
	}
	return missing
}

// deliveryProofHistoricallyCarried answers whether a destination commit BEHIND
// the tip carried all of these lines — the branch moved past the change, so the
// change landed and was then superseded. The scan is bounded and answers false
// when nothing in it carries them.
//
// TWO READINGS, and the second is why a delivered change is not refused once the
// destination moves on (MEASURED 2026-10-29, master bb673af): the bounded
// newest-first scan below only sees the commits nearest the tip, so a landing
// commit that has since been buried behind more than
// `deliveryProofContentClosestCandidates` commits touching the SAME path falls out
// of it and the work is reported missing. Measured on the real tree: the two lines
// ground commit e0b1726 added were carried by master 1ae64e138, which sits at
// position 10 of `git log master -- internal/tools/task_delivery_proof.go`, while
// master's tip no longer holds them — every arm answered "absent" and a delivered
// row was refused (row 01a17abb's arm E).
//
// The second reading does not widen the window (a window is a guess about how far
// the destination has moved, and ANY width is eventually wrong): it asks git WHERE
// the line entered the destination's history, by content (`git log -S<line>`), so
// the answer is the commit that actually carries it no matter how deep it lies.
// Both readings end the same way — the candidate's BLOB is read and every added
// line must be present in it — so neither can report "carried" on a false hit.
func deliveryProofHistoricallyCarried(ctx context.Context, destDir, destRef, path string, added []string,
	deepHistory bool,
) (deliveryProofBounded, string) {
	out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), destRef, "--", path)
	candidates := strings.Fields(out)
	for _, c := range candidates {
		text, ok := deliveryProofBlobText(ctx, destDir, c, path)
		if !ok {
			continue
		}
		if len(deliveryProofLinesMissingAt(text, added)) == 0 {
			return deliveryProofBoundedCarried, c
		}
	}
	if !deepHistory {
		// The BOUNDED reader (DeliveryContentLanded's third leg) must keep
		// answering from the window alone: its caller's red proof pins that the
		// window IS a limit, and a content-located answer there would quietly
		// make the neighbouring leg decorative. The close-out gate asks the DEEP
		// question, because refusing delivered work is the defect it repairs.
		//
		// AND A WINDOW THAT RAN OUT IS NOT A NEGATIVE: the bounded reader says
		// "not measured" when there is more history behind the window that it
		// did not read, because an 8-commit window is a guess about how far the
		// destination has moved and a guess that expires is not evidence.
		if deliveryProofBoundedWindowExhausted(ctx, destDir, destRef, path, len(candidates)) {
			return deliveryProofBoundedOutOfWindow, path
		}
		return deliveryProofBoundedAbsent, ""
	}
	if carried, hit := deliveryProofPickaxeCarried(ctx, destDir, destRef, path, added); carried {
		return deliveryProofBoundedCarried, hit
	}
	return deliveryProofBoundedAbsent, ""
}

// deliveryProofPickaxeCarried locates the destination commit that BROUGHT these
// lines into the history, by content rather than by distance from the tip.
//
// The probe lines are chosen longest-first (the longest line is the most
// discriminating and the least likely to appear in unrelated edits), bounded by
// `deliveryProofContentPickaxeProbes` so a file with many changed lines cannot
// turn this into a scan. Each probe is a literal `git log -S` search — never a
// regex (`--pickaxe-regex` is deliberately NOT passed: an added line is data, and
// a line containing `.*` must not match something else). A hit is still only a
// CANDIDATE: every one is confirmed by reading its blob, so a probe that matches
// the REMOVAL of a line, or a commit that mentions it without carrying the rest,
// cannot pass.
func deliveryProofPickaxeCarried(ctx context.Context, destDir, destRef, path string, added []string) (bool, string) {
	probes := deliveryProofPickaxeProbes(added)
	seen := make(map[string]bool, deliveryProofContentClosestCandidates)
	for _, line := range probes {
		out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
			fmt.Sprint(deliveryProofContentPickaxeCandidates), "-S"+line, destRef, "--", path)
		for _, c := range strings.Fields(out) {
			if seen[c] {
				continue
			}
			seen[c] = true
			text, ok := deliveryProofBlobText(ctx, destDir, c, path)
			if !ok {
				continue
			}
			if len(deliveryProofLinesMissingAt(text, added)) == 0 {
				return true, c
			}
		}
	}
	return false, ""
}

// deliveryProofPickaxeProbes picks the lines a `-S` search is run with: the
// LONGEST ones (most discriminating), at most
// `deliveryProofContentPickaxeProbes` of them, in a stable order so the answer is
// reproducible. Normalised lines are already free of trailing whitespace, which
// is what makes them usable as a literal search term.
func deliveryProofPickaxeProbes(added []string) []string {
	ordered := make([]string, len(added))
	copy(ordered, added)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	if len(ordered) > deliveryProofContentPickaxeProbes {
		ordered = ordered[:deliveryProofContentPickaxeProbes]
	}
	return ordered
}

// deliveryProofDeletionLanded answers whether a deletion the run made is
// reflected at the destination: the path is gone from the tip, or gone from some
// commit in the bounded history behind it.
func deliveryProofDeletionLanded(ctx context.Context, destDir, destRef, path string,
	deepHistory bool,
) (deliveryProofBounded, string) {
	if _, ok := deliveryProofBlobText(ctx, destDir, destRef, path); !ok {
		return deliveryProofBoundedCarried, destRef
	}
	out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), destRef, "--", path)
	candidates := strings.Fields(out)
	for _, c := range candidates {
		if _, ok := deliveryProofBlobText(ctx, destDir, c, path); !ok {
			return deliveryProofBoundedCarried, c
		}
	}
	// The same three-valued reading as the text and binary legs: a deletion the
	// window did not reach is NOT a deletion that never happened. The deep reader
	// asks git where the path's removal entered the history by content, so a
	// commit buried behind the window is still found.
	if !deepHistory {
		if deliveryProofBoundedWindowExhausted(ctx, destDir, destRef, path, len(candidates)) {
			return deliveryProofBoundedOutOfWindow, path
		}
		return deliveryProofBoundedAbsent, ""
	}
	if got, ok := deliveryProofDeletionLocated(ctx, destDir, destRef, path); ok {
		return deliveryProofBoundedCarried, got
	}
	if deliveryProofBoundedWindowExhausted(ctx, destDir, destRef, path, len(candidates)) {
		return deliveryProofBoundedOutOfWindow, path
	}
	return deliveryProofBoundedAbsent, ""
}

// deliveryProofDeletionLocated finds the destination commit that REMOVED the
// path, by content, however deep it lies — the deep reader's answer for a
// deletion.
//
// The confirmation is the deletion's own shape and it must not be borrowed from
// the addition test (`deliveryProofPickaxeCarried` CONFIRMS a hit by reading the
// candidate's blob and requiring the lines to BE there, which for a deletion is
// the opposite of what has to be true): a candidate counts only when its parent
// carried the path and it does not. A `-S` hit on a commit that merely mentions
// the path therefore cannot pass.
//
// The probes are the lines the path LOST between a candidate and its parent —
// the only place a removed line still exists is history. No candidate, no probe,
// no answer: the caller reports "not measured" rather than inventing one.
func deliveryProofDeletionLocated(ctx context.Context, destDir, destRef, path string) (string, bool) {
	out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
		fmt.Sprint(deliveryProofContentClosestCandidates), destRef, "--", path)
	var probes []string
	for _, c := range strings.Fields(out) {
		before, ok := deliveryProofParentBlobText(ctx, destDir, c, path)
		if !ok {
			continue
		}
		if _, afterOK := deliveryProofBlobText(ctx, destDir, c, path); afterOK {
			continue
		}
		probes = append(probes, deliveryProofNormalizeLines(before)...)
	}
	if len(probes) == 0 {
		return "", false
	}
	// A `-S` search matches a commit that changed the COUNT of its term, so the
	// removed line is what locates the removal — and the confirmation is the
	// deletion's own shape (the parent HELD the path, the candidate does not),
	// never the addition test's "the candidate carries the line", which for a
	// deletion is the opposite of what has to be true.
	for _, line := range deliveryProofPickaxeProbes(probes) {
		hits := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
			fmt.Sprint(deliveryProofContentPickaxeCandidates), "-S"+line, destRef, "--", path)
		for _, c := range strings.Fields(hits) {
			if _, still := deliveryProofBlobText(ctx, destDir, c, path); still {
				continue
			}
			if _, held := deliveryProofParentBlobText(ctx, destDir, c, path); held {
				return c, true
			}
		}
	}
	return "", false
}

// deliveryProofParentBlobText reads the path at a commit's FIRST parent, so a
// deletion is confirmed by the fact that the bytes WERE there and are not now.
// A root commit has no parent; the empty answer says "cannot confirm here",
// which is an honest non-hit rather than a guessed one.
func deliveryProofParentBlobText(ctx context.Context, destDir, commit, path string) (string, bool) {
	parent := deliveryProofGitRead(ctx, destDir, "rev-parse", "--verify", "--quiet", commit+"^")
	if parent == "" {
		return "", false
	}
	return deliveryProofBlobText(ctx, destDir, parent, path)
}

// deliveryProofQuoteLines renders a bounded, quoted excerpt of differing lines:
// enough for a reader to recognise WHICH change is missing, never a diff dump.
func deliveryProofQuoteLines(lines []string) string {
	limit := deliveryProofContentDetailInSummary
	if len(lines) < limit {
		limit = len(lines)
	}
	quoted := make([]string, 0, limit+1)
	for _, line := range lines[:limit] {
		quoted = append(quoted, fmt.Sprintf("%q", line))
	}
	if len(lines) > limit {
		quoted = append(quoted, fmt.Sprintf("(+%d more)", len(lines)-limit))
	}
	return strings.Join(quoted, ", ")
}

// deliveryProofContentLanded is THE arm. Every commit between the destination
// and the tip must clear the content test; the answer is (landed, why, judged).
//
// Bounded on purpose: the walk is capped, and a range larger than the cap, an
// unreadable merge base or an unreadable commit all answer judged=false — a
// refusal is never built on a measurement that did not complete.
func deliveryProofContentLanded(ctx context.Context, srcDir, destDir, destRef, rangeRef, tip string) (landed bool, why string, judged bool) {
	return deliveryProofContentLandedWith(ctx, srcDir, destDir, destRef, rangeRef, tip, true)
}

// deliveryProofContentLandedWith is the ONE implementation, with the deep-history
// reading made explicit because two callers ask two different questions:
//
//	the close-out gate (deliveryProofContentLanded) asks "DID this run's work
//	reach the destination", so a landed-then-superseded change must count as
//	delivered however deep the landing commit has been buried (deepHistory);
//	the content-fingerprint seam (DeliveryContentLanded, leg ③) asks the same
//	question over a BOUNDED destination window, and its own red proof pins that
//	window as the limit — so it keeps the bounded reader.
//
// One implementation, one spelling of the comparison; only the history depth a
// caller is willing to look through differs, and the caller says which it wants
// rather than the callee guessing.
func deliveryProofContentLandedWith(ctx context.Context, srcDir, destDir, destRef, rangeRef, tip string,
	deepHistory bool,
) (landed bool, why string, judged bool) {
	if !deliveryProofContentArmEnabled() {
		return false, "", false
	}
	// srcDir must actually HOLD the commit being judged: a pair whose first
	// directory never saw the run's tip is not a measurement of anything (it
	// would report every path "absent-in-destination" from the wrong side).
	if _, err := branchDeleteGit(ctx, srcDir, "cat-file", "-e", tip+"^{commit}"); err != nil {
		return false, "", false
	}
	// THE RANGE comes from wherever BOTH the tip and a base are readable. A
	// clone ground has the run's commits and its own (possibly stale) tracking
	// ref, and the destination's LIVE sha may exist in neither directory the
	// other holds — so the base is found in this order: the live sha here, the
	// ground's own cache of the same ref here, the live sha in destDir. No fetch
	// runs: a stale cache still delimits the run's own commits, which is all the
	// range needs, and the CONTENT comparison below reads the destination from
	// the directory that actually has it.
	base := deliveryProofGitRead(ctx, srcDir, "merge-base", destRef, tip)
	if base == "" && strings.TrimSpace(rangeRef) != "" {
		base = deliveryProofGitRead(ctx, srcDir, "merge-base", rangeRef, tip)
	}
	if base == "" {
		base = deliveryProofGitRead(ctx, destDir, "merge-base", destRef, tip)
	}
	if base == "" {
		return false, "", false
	}
	out := deliveryProofGitRead(ctx, srcDir, "rev-list", "--no-merges", "--max-count",
		fmt.Sprint(deliveryProofContentScanCommits+1), base+".."+tip)
	if strings.TrimSpace(out) == "" {
		return false, "", false
	}
	commits := strings.Fields(out)
	if len(commits) > deliveryProofContentScanCommits {
		return false, "", false
	}
	var failures []string
	for _, c := range commits {
		landedHere, diffs, ok := deliveryProofCommitContentLanded(ctx, srcDir, destDir, destRef, c, deepHistory)
		if !ok {
			// A commit the bounded window could not settle leaves the ANSWER
			// unmeasured — and it says WHICH path's window ran out, so a caller
			// can tell "git could not read the objects" from "git read too
			// little of the history" (Rule 15: the two are different facts).
			if len(diffs) > 0 {
				return false, deliveryProofCommitUnmeasuredSentence(c, destRef, diffs), false
			}
			return false, "", false
		}
		if landedHere {
			continue
		}
		failures = append(failures, deliveryProofContentFailureSentence(ctx, destDir, destRef, c, diffs))
	}
	if len(failures) == 0 {
		return true, fmt.Sprintf("content-equivalent: the added lines of all %s this run produced are present in %s "+
			"after normalising blank lines, comment-only lines and self-referential measurement metadata",
			plural(len(commits)), destRef), true
	}
	return false, strings.Join(failures, "; "), true
}

// deliveryProofContentFailureSentence is ONE commit's classification: the
// closest destination commit, the class of each difference, and a bounded
// per-file summary — the "说清差在哪" half. A refusal that only says "not in the
// ancestry and not in the patch set" leaves a reader unable to tell a real gap
// from a re-derived base, which is the defect this arm repairs.
func deliveryProofContentFailureSentence(ctx context.Context, destDir, destRef, commit string,
	diffs []deliveryProofContentFileDiff) string {
	closest, kept := deliveryProofClosestContentCommit(ctx, destDir, destRef, diffs)
	head := fmt.Sprintf("commit %s differs from %s at %s", branchDeleteShortHash(commit), destRef, plural(len(diffs)))
	if closest != "" {
		head = fmt.Sprintf("commit %s differs from %s at %s; closest %s commit %s already carries %d of them",
			branchDeleteShortHash(commit), destRef, plural(len(diffs)), destRef,
			branchDeleteShortHash(closest), kept)
	}
	lines := make([]string, 0, deliveryProofContentPathsInSummary+1)
	for i, d := range diffs {
		if i == deliveryProofContentPathsInSummary {
			lines = append(lines, fmt.Sprintf("(+%d more file(s))", len(diffs)-i))
			break
		}
		lines = append(lines, fmt.Sprintf("%s — %s: %s", d.Path, d.Class, d.Detail))
	}
	return head + " [" + strings.Join(lines, " | ") + "]"
}

// deliveryProofContentDirs picks the (srcDir, destDir) pairs a content
// comparison may run in: srcDir must hold the run's COMMITS, destDir must hold
// the DESTINATION ref. The ground comes first for both roles, then the recorded
// repo fills the other side — so a ground whose cache is stale and a repo that
// never saw the ground's commits still meet, WITHOUT a fetch. Duplicates are
// dropped once (Rule 15: the same pair measured twice is noise, not evidence).
func deliveryProofContentDirs(ground, repo string) [][2]string {
	var dirs []string
	for _, d := range []string{ground, repo} {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}
	var pairs [][2]string
	seen := make(map[string]bool)
	for _, src := range dirs {
		for _, dest := range dirs {
			key := src + "\x00" + dest
			if seen[key] {
				continue
			}
			seen[key] = true
			pairs = append(pairs, [2]string{src, dest})
		}
	}
	return pairs
}

// deliveryProofClosestContentCommit names the DESTINATION commit whose tree most
// nearly carries this commit's change, and how many of the differing files it
// carries. Candidates are the destination's own commits touching the same paths
// (newest first, bounded); the winner carries the most of them.
//
// It is what lets a refusal say WHICH master commit a reader should compare
// against: without it, "not delivered" and "delivered under another sha" read
// identically.
func deliveryProofClosestContentCommit(ctx context.Context, destDir, destRef string,
	diffs []deliveryProofContentFileDiff) (string, int) {
	seen := make(map[string]bool)
	var candidates []string
	for _, d := range diffs {
		out := deliveryProofGitRead(ctx, destDir, "log", "--format=%H", "-n",
			fmt.Sprint(deliveryProofContentClosestCandidates), destRef, "--", d.Path)
		for _, c := range strings.Fields(out) {
			if !seen[c] {
				seen[c] = true
				candidates = append(candidates, c)
			}
		}
	}
	best, bestKept := "", -1
	for _, c := range candidates {
		kept := 0
		for _, d := range diffs {
			if len(d.Missing) == 0 {
				continue
			}
			text, ok := deliveryProofBlobText(ctx, destDir, c, d.Path)
			if !ok {
				continue
			}
			set := deliveryProofNormalizedSet(text)
			present := true
			for _, line := range d.Missing {
				if !set[line] {
					present = false
					break
				}
			}
			if present {
				kept++
			}
		}
		if kept > bestKept {
			best, bestKept = c, kept
		}
	}
	if best == "" || bestKept < 0 {
		return "", 0
	}
	return best, bestKept
}
