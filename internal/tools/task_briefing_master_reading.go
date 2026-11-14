package tools

// task_briefing_master_reading.go — WHICH REF A CRITERION MEANS WHEN IT SAYS
// "the master version" (epic 01a13af7, measured 2026-10-14).
//
// THE MEASURED CASE, and it is the sibling of the cut-point clause's own: a run
// was closed against the acceptance criterion «devdoc/DOC-INDEX.tsv 与
// anchor-number baseline 用 master 的版本», and the deliverable's red/green
// depended entirely on WHICH ref the reviewer held. Measured in ONE tree
// (host-b, 2026-10-14), three different blobs each answering to
// "master":
//
//	refs/heads/master                  DOC-INDEX.tsv 578ada5ab  2502 lines
//	refs/remotes/gitPushcheck/master   DOC-INDEX.tsv 4bc6b73d6  2533 lines
//	the ground's cut commit f2824ff    DOC-INDEX.tsv 735712e52  2540 lines
//
// and the local ref was 183 commits behind the peer's mirror on that day (249
// on this run's own ground). A criterion phrased against "master" therefore has
// TWO defensible readings with OPPOSITE verdicts, and neither the briefing nor
// the closing report said which one it meant.
//
// WHY IT IS A GOPI DEFECT AND NOT A BRIEFING-WRITING ONE. The seam that appends
// clauses to a briefing (cmd/agent/cli_tasks.go) rendered the integration branch
// as a BARE NAME — IntegrationBranchName, "master" — and never as the ref and
// commit that name resolves to (task_ground_cutpoint.go's own clause says "The
// integration branch in this tree is master", which is exactly the ambiguous
// spelling). The submitter's prose is their own and has no generator to mend
// (Rule 37's boundary); what gopi OWNS is the clause it appends, and that clause
// said a name where the ambiguity lives in which ref that name denotes.
//
// WHAT IS DELIVERED. One additive clause, rendered by named constants, that
// names the integration branch's CANONICAL REF (refs/heads/<integration>) and
// its tip, the PEER-MIRRORED REF the sync lane maintains and this ground's
// population came from, and the commit distance between them — so a run told
// "use the master version" is told, in the same message, that the sentence has
// two readings and which ref+sha must be named in the deliverable.
//
// NO NEW REF, FILE, ENV VAR OR KNOB (Rule 35). Both refs already exist and are
// already maintained: the local integration ref is what a checkout is on, and
// the mirror ref is the ONE the delivery lane's ancestry gate decides on
// (gitPushbackCheckRef, read here through its own constant — Rule 8/13: reuse
// the reading, do not mint a second one). The count reuses the package's ONE
// relative-refspec spelling and its ONE "unmeasurable" verdict string, so the
// alarm already printed by `gopi task push` and this clause cannot disagree
// about what "behind" means.
//
// AN UNREADABLE PAIR IS STATED, NEVER SUBSTITUTED (Rule 15): the unmeasured arm
// says which refs could not be read AND forbids the fallback that produced this
// epic — reading refs/heads/master as though it were the only "master" there is.

import (
	"fmt"
	"strconv"
	"strings"
)

// MasterReadingRead is the measurement behind the clause: the two refs, their
// tips, and the commit distance between them.
//
// Behind=="" is NOT "aligned" and NOT a zero (Rule 15a): it is a pair whose
// distance could not be read, and the clause that renders it says so in the same
// breath rather than letting the local ref be read as the answer.
type MasterReadingRead struct {
	LocalRef string // refs/heads/<integration> — the ref a local checkout is on
	LocalSHA string // its tip, "" when unmeasured
	TipRef   string // the peer-mirrored integration ref
	TipSHA   string // its tip, "" when unmeasured
	Behind   string // commit distance LocalRef..TipRef, "" when unmeasured
	Note     string // the stated reason, when unmeasured
	Evidence string // the git invocations and their exit statuses

	// Remotes is this repo's configured remote NAMES, read with `git remote`
	// (Rule 15: a scan that found nothing renders as an explicit value, never
	// as a blank). RemotesKnown=false means the listing itself failed, and the
	// clause then states no instruction at all.
	Remotes      []string
	RemotesKnown bool

	// NamespaceIsRemote reports whether a remote is configured under the
	// mirror namespace's own name (gitPushbackCheckNamespace). It is the
	// measurement that decides whether `git fetch <that name>` can be run at
	// all on this tree — measured 2026-10-17: it cannot (exit 128), while the
	// REF exists and is the ground's cut point.
	NamespaceIsRemote bool
}

// Measured reports whether BOTH refs and their distance were read off real git
// answers.
func (r MasterReadingRead) Measured() bool {
	return strings.TrimSpace(r.LocalSHA) != "" && strings.TrimSpace(r.TipSHA) != "" &&
		strings.TrimSpace(r.Behind) != ""
}

// Aligned reports whether the two readings name the SAME commit — the positive
// control's case, where "the master version" is unambiguous on this tree and a
// clause claiming a lag would be a lie.
func (r MasterReadingRead) Aligned() bool {
	return r.Measured() && strings.TrimSpace(r.Behind) == gitSyncLagAlignedValue
}

const (
	// MasterReadingLabel is this clause's leading marker — spelled once and
	// EXPORTED, so the producer, the seam's record and the regression tests (in
	// this package and in cmd/agent) address ONE string (Rules 1/13/14). It is
	// how a reader greps a briefing for "you told me which master you meant".
	MasterReadingLabel = "[Which `master` — the ref a criterion phrased against the integration branch is measured on]"

	// masterReadingRefLineFmt renders ONE row of the clause's ref table: the
	// ref's full name, its tip, and what that ref IS. Declared here so the two
	// rows cannot drift from the reader that greps them (Rule 1b).
	masterReadingRefLineFmt = "  %s   tip %s   ← %s"

	// masterReadingLocalRole / masterReadingTipRole are the two rows' second
	// column. The local one says what a reader can act on (the ref their own
	// checkout is on); the peer one says WHERE the ground's population came from,
	// which is the fact that makes the two readings differ at all.
	//
	// The peer row ALSO says what the mirrored name is and is NOT — a REF, not a
	// remote. Measured 2026-10-17 on host-a (row 01a14877): `git remote -v`
	// listed only `peer1`, while refs/remotes/gitPushcheck/master existed and was
	// the ground's cut point, so `git fetch --quiet gitPushcheck` answered
	// `fatal: 'gitPushcheck' does not appear to be a git repository` (exit 128)
	// — and several briefings told executors to run exactly that. The ref table
	// shipped this spelling without saying which one it was, which is what made
	// a refusable-lane mistake look like a broken probe.
	masterReadingLocalRole = "the integration branch's own ref — what a local checkout is on"
	masterReadingTipRole   = "the PEER's integration tip, mirrored by the sync lane — this ground was cut from its line. It is a REF, not a remote: the name names the lane's scratch namespace (refs/remotes/" + gitPushbackCheckNamespace + "/), never a configured git remote"

	// masterReadingHeadFmt is the clause's opening: what the ambiguity IS. It
	// names the integration branch twice (as the criterion's word, and as the
	// branch whose ref answers to it) — the branch name is PASSED IN from
	// IntegrationBranchName, never typed here.
	masterReadingHeadFmt = "%s\n" +
		"An acceptance criterion that says \"take %s's version\" (or \"as on %s\") names a BRANCH, and this tree carries MORE THAN ONE ref answering to that name — they are different commits, so the criterion's verdict flips with the ref the reader holds. In this tree:\n" +
		"%s\n" +
		"%s\n" +
		"Only ONE of the two is what the criterion's author measured, so a run that satisfies \"the %s version\" must NAME the ref and its sha (a branch name is not enough) and state which of the two it took: `git rev-parse %s %s` is the reading that settles it, and a deliverable that names neither is red or green on whichever ref the reviewer happens to hold.\n"

	// masterReadingBehindFmt is the clause's status line when the two readings
	// DIFFER — the measured case, and the sentence that makes the criterion
	// unambiguous by stating the lag rather than the name.
	masterReadingBehindFmt = "They are NOT the same commit: %s is %s commit(s) behind %s, so a criterion measured on one of them is measured at a commit the other does not have."

	// masterReadingAlignedFmt is the POSITIVE CONTROL's line, and it exists so
	// the clause cannot cry lag on a tree that has none: on a host whose local
	// integration ref IS the peer's tip the two readings name one commit, and
	// the clause says that instead of printing a number that would be a lie.
	masterReadingAlignedFmt = "%s is AT %s — %s commit(s) behind — so on this tree the two readings name the SAME commit and \"the %s version\" is unambiguous here."

	// masterReadingUnmeasuredFmt is the clause's status line when the pair could
	// not be read. It names what failed AND forbids the exact substitution that
	// produced this epic (Rule 15: a record that cannot name its subject says so;
	// the local ref is NOT the fallback).
	masterReadingUnmeasuredFmt = "The refs could NOT be read here (%s), so NO reading is named: do NOT fall back on %s as though it were the only %s there is — read both refs yourself before judging a criterion phrased against \"the %s version\"."

	// masterReadingRecordFmt is the seam's ONE record line for this measurement,
	// rendered by the same function that renders the clause so the two can never
	// state different numbers (Rules 1/13/14).
	masterReadingRecordFmt = "briefing master reading → %s tip %s vs %s tip %s: %s (remotes: %s; evidence: %s)\n"

	// The REMOTE-LAYOUT arms (measured 2026-10-17, row 01a14877). The mirror ref
	// above carries the name of the lane that writes it, and a reader told only
	// the ref's NAME will try `git fetch <that name>` — which is rc=128 on a host
	// whose remotes are named otherwise (measured here: `git remote -v` lists
	// `peer1` only). So the clause states, from a measurement of `git remote`,
	// whether that name is a remote at all, and gives the refresh that is really
	// runnable when it is not.
	//
	// masterReadingRemoteDirectiveFmt — the ref exists and a remote under the
	// mirror name does NOT. It carries the ref, the remotes this repo DOES have,
	// the namespace name, git's own sentence and its exit status, and the exact
	// refresh/reading instruction: %s, %s, %q, %q, %d, %s.
	masterReadingRemoteDirectiveFmt = "%s is a REF this tree already carries, and `git remote` lists %s — the mirror name `%s` is NOT one of them, so `%s %s` fails with \"'%s' does not appear to be a git repository\" (exit %d) and proves nothing about the branch you are judging. %s"

	// masterReadingRemoteReadFmt is the instruction when a usable peer remote
	// exists: read the ref as it stands (an ancestry probe answers on it), or
	// refresh it through THAT remote with the lane's own refspec.
	masterReadingRemoteReadFmt = "Read the ref as it stands — `git merge-base --is-ancestor <sha> %s` answers without any fetch — or refresh it through a remote that DOES exist here: `%s`."

	// masterReadingRemoteNoFetchFmt is the same finding one step emptier: the
	// namespace is not a remote AND this repo has no remote at all, so no
	// refresh exists to name — the ref is READ, never fetched (Rule 15: an
	// instruction that cannot be run is worse than none).
	masterReadingRemoteNoneFmt = "%s is a REF this tree already carries, and `%s` is NOT a configured git remote here (%s), so do NOT run `%s %s` — nothing on this tree can refresh it, and the ref you have is the one to judge on (`git merge-base --is-ancestor <sha> %s` answers on it)."

	// masterReadingRemoteUnreadFmt is the arm where the remote LISTING itself
	// failed: the layout was not read, so no instruction is rendered and the
	// one fact that survives is that the name is a REF's name.
	masterReadingRemoteUnreadFmt = "The remote layout of this tree could NOT be read, so whether `%s` is a configured git remote is UNMEASURED — but %s is a REF either way, and an ancestry probe answers on it without any fetch."
)

// MasterReading reads the two refs a criterion phrased against "master" can mean
// on this tree, and the commit distance between them.
//
// repo is the tree git runs in (the ground's own worktree, so the read follows
// the row rather than the caller's cwd). It reuses the package's ONE bounded git
// runner (mergeVerifyStaleGit) and its ONE "unmeasurable" verdict string
// (gitSyncLagUnknown) rather than opening a second prober or minting a second
// spelling of "we could not count" (Rules 8/13).
func MasterReading(repo string) MasterReadingRead {
	r := strings.TrimSpace(repo)
	out := MasterReadingRead{
		LocalRef: MasterReadingLocalRef(),
		TipRef:   MasterReadingTipRef(),
	}
	if r == "" {
		out.Note = "the ground directory is empty, so no tree was read"
		out.Evidence = "not attempted: repo is empty"
		return out
	}
	remotes, remotesKnown, remotesEv := masterReadingRemotes(r)
	out.Remotes, out.RemotesKnown = remotes, remotesKnown
	out.NamespaceIsRemote = remotesKnown && masterReadingHasRemote(remotes, gitPushbackCheckNamespace)
	out.Evidence = remotesEv
	localSHA, localOK, localEv := masterReadingResolveRef(r, out.LocalRef)
	tipSHA, tipOK, tipEv := masterReadingResolveRef(r, out.TipRef)
	out.Evidence = remotesEv + "; " + localEv + "; " + tipEv
	if !localOK || !tipOK {
		out.Note = fmt.Sprintf("%s and/or %s did not resolve in %s", out.LocalRef, out.TipRef, r)
		return out
	}
	out.LocalSHA, out.TipSHA = localSHA, tipSHA
	raw, exit, detail := mergeVerifyStaleGit(r, integrationClaimRevListVerb, integrationClaimCountFlag,
		out.LocalRef+integrationClaimRelativeRefspecSeparator+out.TipRef)
	out.Evidence = out.Evidence + "; " + fmt.Sprintf("git -C %s %s %s %s%s%s ⇒ exit=%d %s",
		r, integrationClaimRevListVerb, integrationClaimCountFlag, out.LocalRef,
		integrationClaimRelativeRefspecSeparator, out.TipRef, exit, mergeBuildGateTail(detail))
	if exit != 0 {
		out.Note = fmt.Sprintf("`git rev-list --count %s..%s` exited %d: the distance between the two readings was not measured, and an uncounted distance is not 0 (it would read as \"aligned\")",
			out.LocalRef, out.TipRef, exit)
		return out
	}
	count := strings.TrimSpace(raw)
	if _, err := strconv.Atoi(count); err != nil {
		out.Note = fmt.Sprintf("`git rev-list --count %s..%s` printed %q, which is not a count: the distance was not measured",
			out.LocalRef, out.TipRef, count)
		return out
	}
	out.Behind = count
	return out
}

// masterReadingRemotes lists this repo's configured remote NAMES — the
// measurement that decides whether the mirror namespace can be fetched at all
// (measured 2026-10-17, row 01a14877: it cannot, while the REF exists).
//
// known=false is NOT "no remotes" (Rule 15a): it is a listing that could not be
// read, and the clause renders no instruction for it. An empty-but-known list
// renders explicitly, through the package's existing "none" spelling.
func masterReadingRemotes(repo string) (names []string, known bool, ev string) {
	out, exit, detail := mergeVerifyStaleGit(repo, pushbackRemoteNameVerb)
	ev = fmt.Sprintf("git -C %s %s ⇒ exit=%d %s", repo, pushbackRemoteNameVerb, exit, mergeBuildGateTail(detail))
	if exit != 0 {
		return nil, false, ev
	}
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, true, ev
}

// masterReadingHasRemote reports whether a named remote is configured. A remote
// IS its config section, which is what `git remote` enumerates.
func masterReadingHasRemote(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// masterReadingRemoteField renders the measured remote layout for the record —
// the same three states the clause speaks about, so a reader grepping the record
// learns WHICH naming this tree had without re-running `git remote`.
func masterReadingRemoteField(r MasterReadingRead) string {
	switch {
	case !r.RemotesKnown:
		return gitSyncLagUnknown
	case len(r.Remotes) == 0:
		return pushbackRemoteNameNone
	default:
		return strings.Join(r.Remotes, pushbackRemoteNameListSep)
	}
}

// masterReadingRemoteLine is the clause's statement about the mirror NAME — the
// half the ref table alone cannot carry. Three states, and the third never
// borrows an instruction (Rules 15/15a).
func masterReadingRemoteLine(r MasterReadingRead) string {
	switch {
	case !r.RemotesKnown:
		return fmt.Sprintf(masterReadingRemoteUnreadFmt, gitPushbackCheckNamespace, r.TipRef)
	case r.NamespaceIsRemote:
		return ""
	case len(r.Remotes) == 0:
		return fmt.Sprintf(masterReadingRemoteNoneFmt, r.TipRef, gitPushbackCheckNamespace,
			pushbackRemoteNameNone, gitBinaryName, gitPeerFetchVerb+" "+gitPushbackCheckNamespace, r.TipRef)
	default:
		return fmt.Sprintf(masterReadingRemoteDirectiveFmt, r.TipRef,
			strings.Join(r.Remotes, pushbackRemoteNameListSep), gitPushbackCheckNamespace,
			gitBinaryName, gitPeerFetchVerb+" "+gitPushbackCheckNamespace,
			gitPushbackCheckNamespace, mergeVerifyGitExitFatal,
			fmt.Sprintf(masterReadingRemoteReadFmt, r.TipRef, masterReadingRefreshCommand(r.Remotes)))
	}
}

// masterReadingRefreshCommand is the ONE runnable refresh of the mirror ref: the
// lane's own refspec (gitPeerMirrorRefspec) applied to a remote that exists on
// this tree. The peer target the lane uses is preferred when it resolves; a
// configured remote name is the fallback, because the refspec names the
// SOURCE ref explicitly and a remote's own fetch refspec is therefore not what
// this needs.
func masterReadingRefreshCommand(remotes []string) string {
	target := masterReadingRefreshTarget(remotes)
	if target == "" {
		return ""
	}
	return gitBinaryName + " " + gitPeerFetchVerb + " " + target + " " + gitPeerMirrorRefspec()
}

// masterReadingRefreshTarget picks the name a fetch hands the refspec to: the
// lane's own peer when this tree HAS it as a configured remote (the readable
// spelling), otherwise that peer's ssh target half, otherwise the first remote
// the tree has at all. Empty means this tree has nothing to fetch from.
func masterReadingRefreshTarget(remotes []string) string {
	if len(remotes) == 0 {
		return ""
	}
	if peer, _, ok := strings.Cut(strings.TrimSpace(GitSyncServerTarget()), ":"); ok {
		if peer = strings.TrimSpace(peer); peer != "" && masterReadingHasRemote(remotes, peer) {
			return peer
		}
	}
	return remotes[0]
}

// masterReadingResolveRef reports the tip of a FULL ref name and whether it was
// read. It shares mergeVerifyStaleResolveRef's ONE git runner (Rule 8) while
// taking a full ref rather than a branch name, because the second ref this file
// reads is a reserved mirror ref that no branch spelling reaches.
func masterReadingResolveRef(repo, fullRef string) (sha string, ok bool, ev string) {
	out, exit, detail := mergeVerifyStaleGit(repo, gitRevParseVerb, gitVerifyArg, gitQuietArg, fullRef)
	ev = fmt.Sprintf("git -C %s rev-parse --verify --quiet %s ⇒ exit=%d %s", repo, fullRef, exit, mergeBuildGateTail(detail))
	if exit != 0 {
		return "", false, ev
	}
	sha = strings.TrimSpace(out)
	if sha == "" {
		return "", false, ev
	}
	return sha, true, ev
}

// (The count's validity check is strconv.Atoi at the call site above — the
// package's existing decimal reader, rather than a second one here.)

// MasterReadingLocalRef and MasterReadingTipRef name the TWO refs this clause
// speaks about — EXPORTED so the clause, the record line and the regression
// tests in BOTH packages address one definition (Rule 14) instead of re-typing
// a ref prefix outside this package. Each is built from the constant that
// already owns it (Rule 13): the local one from IntegrationBranchName, the tip
// one from the delivery lane's own mirror ref.
func MasterReadingLocalRef() string { return refsHeadsPrefix + IntegrationBranchName }

// MasterReadingTipRef is the peer-mirrored integration ref — the SAME ref the
// delivery lane's ancestry gate decides on (gitPushbackCheckRef).
func MasterReadingTipRef() string { return gitPushbackCheckRef }

// MasterReadingRefs names the TWO refs this clause speaks about, in one string —
// EXPORTED so the seam's record line addresses the SAME pair of definitions
// (Rule 14: a shared value is spelled once, and a consumer outside this package
// must be able to reference it rather than re-type it).
func MasterReadingRefs() string {
	return MasterReadingLocalRef() + " vs " + MasterReadingTipRef()
}

// TaskBriefingMasterReadingClause renders the clause that names WHICH ref a
// "master version" criterion is measured on, and the seam's record line — the
// ONE measurement, never a pass.
//
// It returns ("", "") only when there is no tree to read (an empty repo), the
// same contract the sibling clauses keep: a clause naming no subject is worse
// than no clause (Rule 15).
func TaskBriefingMasterReadingClause(repo string) (clause, record string) {
	r := strings.TrimSpace(repo)
	if r == "" {
		return "", ""
	}
	read := MasterReading(r)
	rec := fmt.Sprintf(masterReadingRecordFmt, read.LocalRef, masterReadingShaField(read.LocalSHA),
		read.TipRef, masterReadingShaField(read.TipSHA), masterReadingRecordVerdict(read),
		masterReadingRemoteField(read), read.Evidence)
	refs := strings.Join([]string{
		fmt.Sprintf(masterReadingRefLineFmt, read.LocalRef, masterReadingShaField(read.LocalSHA), masterReadingLocalRole),
		fmt.Sprintf(masterReadingRefLineFmt, read.TipRef, masterReadingShaField(read.TipSHA), masterReadingTipRole),
	}, "\n")
	// The mirror name's OWN status rides in the same table block: a reader who
	// has just been told "refs/remotes/gitPushcheck/master" is one sentence away
	// from `git fetch gitPushcheck`, which is rc=128 on a host where that name is
	// a namespace and not a remote (measured 2026-10-17, row 01a14877).
	if remoteLine := masterReadingRemoteLine(read); remoteLine != "" {
		refs = strings.Join([]string{refs, remoteLine}, "\n")
	}
	return fmt.Sprintf(masterReadingHeadFmt, MasterReadingLabel,
		IntegrationBranchName, IntegrationBranchName, refs, masterReadingStatusLine(read),
		IntegrationBranchName, read.LocalRef, read.TipRef), rec
}

// masterReadingStatusLine is the clause's one measured sentence — and the whole
// point of the file: three states, and the third one never borrows the local ref
// as its answer.
func masterReadingStatusLine(r MasterReadingRead) string {
	switch {
	case r.Aligned():
		return fmt.Sprintf(masterReadingAlignedFmt, r.LocalRef, r.TipRef, r.Behind, IntegrationBranchName)
	case r.Measured():
		return fmt.Sprintf(masterReadingBehindFmt, r.LocalRef, r.Behind, r.TipRef)
	default:
		return fmt.Sprintf(masterReadingUnmeasuredFmt, r.Note, r.LocalRef, IntegrationBranchName, IntegrationBranchName)
	}
}

// masterReadingShaField renders one tip: gitSyncLagUnknown when it was not read,
// so an unread ref can never print as a sha (Rule 15a — the required semantic is
// not the zero value).
func masterReadingShaField(sha string) string {
	if s := strings.TrimSpace(sha); s != "" {
		return shortSha(s)
	}
	return gitSyncLagUnknown
}

// masterReadingRecordVerdict is the record's one-word verdict, and it never lets
// "unread" print as "equal".
func masterReadingRecordVerdict(r MasterReadingRead) string {
	switch {
	case r.Aligned():
		return "aligned (0 behind) — the two readings name one commit"
	case r.Measured():
		return r.Behind + " behind"
	default:
		return gitSyncLagUnknown + " — " + r.Note
	}
}
