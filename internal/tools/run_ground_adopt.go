package tools

// run_ground_adopt.go — the BACKFILL leg of the run-ground anchor axis
// (epic worktree-reclaim-two-gaps, the population half).
//
// THE GAP. The anchor axis has exactly ONE population source: RunGroundAnchors
// reads kind=worktree-ground records out of the removal-audit sink, and a
// directory with no such record is invisible to the whole axis — not
// collectable, not kept, not even counted (RunGroundCollectableGrounds never
// sees it). The producer that writes those records landed at commit 3feaf4a
// (task 01a17b5b) and it is FORWARD-ONLY: every clone ground opened by the
// producer that predates it is permanently unnameable. Measured on this host
// (2026-10-27): 14 uuid-shaped clone grounds, 2150 MB, with no anchor and
// therefore no reclaim path — on a 96 %-full `/` partition.
//
// WHY THIS IS NOT THE RECONSTRUCTION run_ground_anchor.go FORBIDS. The design
// principle is that ownership is RECORDED BY THE OWNER AT ACQUISITION, never
// reconstructed from /proc absence. So this leg does not invent a taker: it
// writes `unrecorded` into all three identity fields (pid, start-ticks,
// session — TaskGroundAnchorRecord's own sentinel for an identity it could not
// read) and carries its OWN MEASUREMENT in the evidence field instead. Three
// facts, each of which a later reader can re-ask:
//
//	① NOBODY STANDS THERE — runGroundOccupied, the shared cwd/fd/exe guard every
//	   other removal arm consults (Rule 14: one implementation of "is anyone in
//	   there").
//	② THE TREE IS CLEAN — reclaimDirtyVerdict over `git status --porcelain`,
//	   through runGroundCollectCloneVerdict, the clone leg's own rung.
//	③ THE WORK IS DELIVERED — worktreeUndeliveredRefusal, the rung that asks
//	   whether this directory is some commit's only home.
//
// ② and ③ arrive through runGroundCollectCloneVerdict (which also asks the
// workspace-root origin question), so the backfill and the removal leg agree
// about a ground BY CONSTRUCTION rather than by two similar implementations.
//
// WHAT IT IS NOT ALLOWED TO DO: remove anything. This leg writes one record per
// ground and stops; the removal is the existing collect leg's job, under its own
// --apply and its own re-checks. A backfill that deleted what it just adopted
// would be one command that turns "no record" into "deleted", which is the
// reversibility this whole design exists to keep.
//
// AND THE RUNG IT ADDS TO THE LADDER IS NAMED. RunGroundAnchorLiveness reads a
// backfilled anchor through runGroundBackfillTakerReason (see
// runGroundAnchorLiveness), never through the launch ladder: the launch ladder's
// first rung is "the recorded pid is alive", and `unrecorded` is not a pid — the
// ladder would answer "unmeasurable ⇒ LIVE" and keep the ground forever, which
// is the very state this task exists to end. The named rung says what was
// measured instead, and the age floor, the occupancy guard and the two clone-leg
// rungs still run on top of it.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/removalaudit"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionid"
)

const (
	// runGroundAdoptOccupiedReason is the backfill's OCCUPANCY refusal, quoted by
	// the report line and by the test. It is its own sentence rather than the
	// collection leg's ("a live process still stands in the ground (cwd/fd/exe)")
	// because the two are different acts: the one refuses to FREE a ground, this
	// one refuses to declare a ground unowned while somebody is in it.
	runGroundAdoptOccupiedReason = "kept: a live process still stands in the ground (cwd/fd/exe) — a ground somebody is working in is not unowned"
	// runGroundAdoptDirtyPrefix is the DIRT rung's prefix ALONE — it may only be
	// prepended to a sentence that is actually about a tree's cleanliness.
	//
	// IT USED TO BE APPLIED TO ALL THREE CLONE-LEG REFUSALS, and that was a Rule 15
	// defect with a measured cost: runGroundCollectCloneVerdict answers three
	// different questions (① is the ground inside the workspace root, ② is the
	// tree clean, ③ has the branch's work reached a delivery point), and a caller
	// that stamped "kept: the tree is not clean" onto every one of them told an
	// operator standing in a ground whose `git status --porcelain` was EMPTY that
	// the problem was dirt. Measured on this host 2026-10-27 with
	// `gopi task sweep-worktrees --backfill-anchors --dry-run`: five of the seven
	// refusals carrying this prefix were the DELIVERY rung (their grounds were
	// clean — the sentence above the branch name contradicted the branch sentence
	// below it), and the instruction they gave ("clean the tree") was not the
	// repair (push the branch or merge it into master).
	//
	// The rung sentences carry their own subject and their own next step, so the
	// caller now prints `why` VERBATIM (see RunGroundAdoptableGrounds). This
	// constant keeps the cleanliness wording for the one caller that has really
	// measured dirt, and runGroundAdoptRungPrefix renders the labels below.
	runGroundAdoptDirtyPrefix = "kept: the tree is not clean"
	// runGroundAdoptRungUnrootedPrefix names the ORIGIN rung's refusal (the ground
	// is not inside the configured workspace root). Distinguishable from the two
	// others by name, never by guessing from the sentence body: the caller that
	// prefixes it knows which leg it is prefacing (runGroundAdoptRungPrefix).
	runGroundAdoptRungUnrootedPrefix = "kept: the ground's origin could not be confirmed"
	// runGroundAdoptRungDeliveryPrefix names the DELIVERY rung's refusal (the
	// branch holds commits no delivery point has). Its subject is the BRANCH, not
	// the tree — a ground that reaches this rung has already passed the
	// cleanliness rung, so it is known clean.
	runGroundAdoptRungDeliveryPrefix = "kept: the ground's work has not been delivered"
	// runGroundAdoptYoungFmt is the age-floor refusal. A ground is measured by its
	// DIRECTORY's mtime: an unanchored ground has no record to age, and inventing
	// one is the reconstruction this leg refuses.
	runGroundAdoptYoungFmt = "kept: the ground is younger than the floor (%s < %s)"
	// runGroundAdoptAgeUnreadableFmt — an unmeasurable age keeps the ground
	// (Rule 15a: unknown is never "old enough").
	runGroundAdoptAgeUnreadableFmt = "kept: the ground's age could not be measured (%s)"
	// runGroundAdoptNotUuidShapeFmt — the backfill's population is the grounds the
	// CLONE CHANNEL opened, and it names them by the row id it was told to clone
	// for. A directory whose name is not a canonical task id is somebody else's
	// clone (a hand-made checkout, a fixture), and claiming it as this axis'
	// business would be the unowned-directory grab the origin rung exists to stop.
	runGroundAdoptNotUuidShapeFmt = "out of scope: the directory name is not a canonical task id (%s) — not a ground this axis opened"
	// runGroundAdoptRootUnresolvedFmt — no measurable workspace root means no
	// measurable population: nothing may be adopted, because nothing can be shown
	// to be the clone channel's.
	runGroundAdoptRootUnresolvedFmt = "the workspace root could not be resolved (%s) — no ground can be shown to be this axis' to adopt"
	// runGroundAdoptTakerReason is the NAMED rung a backfilled anchor reaches on
	// the liveness ladder, and the reason a dry run prints beside the ground.
	runGroundAdoptTakerReason = "the ground carries no anchor from a taker — the backfill recorded a MEASUREMENT instead " +
		"(no live process stands in it, the tree is clean, its branch holds no undelivered commit), so no record here names a run that is still alive"
	// runGroundAdoptEvidenceFmt is what the writer puts in the record: the three
	// measured facts, so a reader of the sink sees WHY the ground was adopted
	// without re-deriving it.
	runGroundAdoptEvidenceFmt = "backfilled anchor from MEASUREMENT (arm %s): no live process in the ground, `git status --porcelain` empty, branch %s holds no commit absent from its delivery points — no taker identity was invented (pid/start-ticks/session are %q)"
)

// RunGroundAdopt is one ground the backfill leg judged, with the reason it
// reached — the reason is the deliverable (Rule 15), exactly as on the collect
// axis.
type RunGroundAdopt struct {
	Dir    string
	Branch string
	Repo   string
	Reason string
	// Age is the directory's measured age, for the report line.
	Age time.Duration
}

// RunGroundAdoptableGrounds walks the configured workspace root and answers
// which clone grounds the axis cannot see but CAN prove unowned, plus the ones
// it must leave alone and why.
//
// The population filter is deliberately narrow, in this order, and every rung is
// a fact about the DIRECTORY (never about a record — there is no record):
//
//	① it is a clone ground       — deliveryProofIsCloneGround, the package's ONE
//	                               shape discriminator (.git is a DIRECTORY)
//	② it is named like a row     — sessionid.IsCanonical, the tree's ONE
//	                               canonical-spelling rule; a bare/other name is
//	                               somebody else's clone
//	③ the axis cannot see it     — RunGroundAnchors holds no record for it
//	④ it is old enough           — the directory's mtime against the same floor
//	                               the sweep uses (one knob for "how long may a
//	                               directory look abandoned")
//	⑤ nobody stands in it        — runGroundOccupied
//	⑥ / ⑦ / ⑧ clean, delivered and inside the root — runGroundCollectCloneVerdict,
//	                               the clone leg's OWN three refusals (reused, not
//	                               re-implemented: Rule 14)
func RunGroundAdoptableGrounds(now time.Time, minAge time.Duration) (adoptable, refused []RunGroundAdopt, err error) {
	root, source, rerr := config.WorkspaceRootConfigured()
	if rerr != nil || strings.TrimSpace(root) == "" {
		return nil, nil, fmt.Errorf(runGroundAdoptRootUnresolvedFmt, runGroundAdoptRootWhy(source, rerr))
	}
	path, perr := TaskGroundAnchorPath()
	if perr != nil {
		return nil, nil, perr
	}
	anchors, aerr := RunGroundAnchors(path)
	if aerr != nil {
		return nil, nil, aerr
	}
	anchored := map[string]bool{}
	for _, a := range anchors {
		if dir := strings.TrimSpace(a.Worktree); dir != "" {
			anchored[scratchKey(dir)] = true
		}
	}
	entries, derr := os.ReadDir(root)
	if derr != nil {
		return nil, nil, fmt.Errorf(runGroundAdoptRootUnresolvedFmt, groundFirstLine(derr.Error()))
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirs = append(dirs, filepath.Join(root, e.Name()))
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if !deliveryProofIsCloneGround(dir) {
			continue // not this axis' shape at all
		}
		base := filepath.Base(dir)
		if !sessionid.IsCanonical(base) {
			refused = append(refused, RunGroundAdopt{Dir: dir, Reason: fmt.Sprintf(runGroundAdoptNotUuidShapeFmt, base)})
			continue
		}
		if anchored[scratchKey(dir)] {
			continue // already visible to the axis; not this leg's business
		}
		info, serr := os.Stat(dir)
		if serr != nil {
			refused = append(refused, RunGroundAdopt{Dir: dir, Reason: fmt.Sprintf(runGroundAdoptAgeUnreadableFmt, groundFirstLine(serr.Error()))})
			continue
		}
		age := now.Sub(info.ModTime())
		cand := RunGroundAdopt{Dir: dir, Repo: dir, Age: age}
		if age < minAge {
			cand.Reason = fmt.Sprintf(runGroundAdoptYoungFmt, age.Round(time.Minute).String(), minAge.String())
			refused = append(refused, cand)
			continue
		}
		branch := runGroundAdoptBranch(dir)
		cand.Branch = branch
		if runGroundOccupied(dir) {
			cand.Reason = runGroundAdoptOccupiedReason
			refused = append(refused, cand)
			continue
		}
		// THE THREE CLONE-LEG RUNGS, reused whole. runGroundCollectCloneRungVerdict
		// answers origin (inside the workspace root), cleanliness and delivery in
		// one call, with the same sentences the removal leg will print — so an
		// adoption cannot pass a rung the removal would then refuse.
		//
		// EACH RUNG KEEPS ITS OWN SENTENCE (Rule 15). The caller used to prefix ALL
		// THREE with runGroundAdoptDirtyPrefix ("kept: the tree is not clean"), so a
		// clean ground refused by the DELIVERY rung was reported as a dirty tree —
		// a reason an operator cannot act on, since the only repair the sentence
		// names is one they had already done. The rung is carried as a VALUE, so the
		// label below is chosen from a measurement rather than from the wording of
		// the sentence it precedes, and `why` is printed VERBATIM: it already names
		// its own subject (origin / the tree's measured dirt / the branch's
		// undelivered commits) and its own next step.
		if rung, why := runGroundCollectCloneRungVerdict(dir, dir, branch); rung != runGroundCollectCloneRungNone {
			cand.Reason = runGroundAdoptRungReason(rung, why)
			refused = append(refused, cand)
			continue
		}
		cand.Reason = "adoptable: no anchor, no live process, clean, and every commit on " + branch + " exists at its delivery point"
		adoptable = append(adoptable, cand)
	}
	return adoptable, refused, nil
}

// runGroundAdoptRungReason renders one clone-leg refusal for the BACKFILL report,
// with a prefix chosen from the MEASURED rung rather than from the sentence.
//
// Three answers, three prefixes, three repairs (Rule 15): the refusal that says
// "not inside the workspace root" is not about the tree, the one that says the
// branch holds undelivered commits is not about the tree either, and only the
// two cleanliness rungs are. A single shared prefix was the defect — measured on
// this host 2026-10-27 (`--backfill-anchors --dry-run`): five of seven refusals
// carrying "kept: the tree is not clean" were DELIVERY refusals on grounds whose
// `git status --porcelain` was empty, and the instruction they carried ("clean
// the tree") was not the repair.
//
// `why` is returned VERBATIM: every rung sentence already names its own subject
// and its own next step, so this function adds a label and never a second
// explanation (a paraphrase here would be the same defect one layer down).
func runGroundAdoptRungReason(rung runGroundCollectCloneRung, why string) string {
	switch rung {
	case runGroundCollectCloneRungUnrooted:
		return runGroundAdoptRungUnrootedPrefix + " — " + why
	case runGroundCollectCloneRungDirty, runGroundCollectCloneRungCleanlinessUnmeasured:
		// The cleanliness rungs, and ONLY these: "dirty" measured content in the
		// tree, "unmeasured" could not ask the question at all. Both are about the
		// tree, so the prefix is true of both — and the sentence after it says
		// which of the two it was, because that is the operator's next step.
		return runGroundAdoptDirtyPrefix + " — " + why
	case runGroundCollectCloneRungUndelivered:
		return runGroundAdoptRungDeliveryPrefix + " — " + why
	case runGroundCollectCloneRungNone:
		// Not a refusal. Answered rather than defaulted: a caller that reaches this
		// function with no rung has a bug, and printing a dirt prefix for it would
		// be exactly the wrong-attribution this function exists to stop.
		return why
	}
	return why
}

// runGroundAdoptRootWhy names WHICH channel resolved (or failed to resolve) the
// workspace root, because "the root could not be resolved" without the source is
// a sentence the operator cannot act on: a settings key missing from the profile
// and a read failure are different repairs (Rule 15).
func runGroundAdoptRootWhy(source string, err error) string {
	why := "channel " + strings.TrimSpace(source)
	if rerr := groundFirstLine(fmt.Sprint(err)); rerr != "" && rerr != "<nil>" {
		why += ", error " + rerr
	}
	return why
}

// RunGroundAdoptGrounds writes one backfilled anchor per adoptable ground and stops
// there — it removes nothing (see the file doc). Every rung is re-asked
// immediately before the write, because the scan and the act are two moments and
// the check that matters is the one closest to the act.
func RunGroundAdoptGrounds(adoptable []RunGroundAdopt) (adopted, skipped []string, err error) {
	for _, a := range adoptable {
		dir := strings.TrimSpace(a.Dir)
		if dir == "" {
			continue
		}
		if !deliveryProofIsCloneGround(dir) {
			skipped = append(skipped, dir+" (the shape changed since the scan: .git is no longer a directory)")
			continue
		}
		if runGroundOccupied(dir) {
			skipped = append(skipped, dir+" (a live process appeared after the scan)")
			continue
		}
		branch := a.Branch
		if strings.TrimSpace(branch) == "" {
			branch = runGroundAdoptBranch(dir)
		}
		if why := runGroundCollectCloneVerdict(dir, dir, branch); why != "" {
			skipped = append(skipped, dir+" ("+why+")")
			continue
		}
		taskGroundAnchorBackfill(dir, branch, dir, filepath.Base(dir))
		adopted = append(adopted, dir)
	}
	return adopted, skipped, nil
}

// taskGroundAnchorBackfill is the ONE writer of a backfilled anchor. It passes
// runGroundAnchorUnrecorded for all three identity parts — never a pid, never
// start-ticks, never a session: a fabricated identity would be indistinguishable
// from a measured one and would let the ladder answer a question nobody asked.
//
// The arm is what makes the record read back as Backfilled (RunGroundAnchors),
// which is what routes the liveness ladder to the named rung. One spelling, two
// readers (Rule 13).
func taskGroundAnchorBackfill(dir, branch, repo, measured string) {
	removalaudit.WorktreeGround(
		removalaudit.ArmTaskGroundAnchorBackfill,
		strings.TrimSpace(repo),
		strings.TrimSpace(dir),
		strings.TrimSpace(branch),
		taskIDFromGroundName(filepath.Base(strings.TrimSpace(dir))),
		runGroundAnchorUnrecorded,
		runGroundAnchorUnrecorded,
		runGroundAnchorUnrecorded,
		fmt.Sprintf(runGroundAdoptEvidenceFmt, removalaudit.ArmTaskGroundAnchorBackfill, strings.TrimSpace(branch), runGroundAnchorUnrecorded)+": "+measured,
	)
}

// taskIDFromGroundName returns the row id a clone ground's directory name
// encodes, or "" when the name is not one (the ledger rung then answers "no row
// to ask about", which is its documented direction — unknown is never dead).
func taskIDFromGroundName(name string) string {
	name = strings.TrimSpace(name)
	if !sessionid.IsCanonical(name) {
		return ""
	}
	return name
}

// runGroundAdoptBranch reads the ground's current branch. An unreadable branch
// is "" — which makes the delivery rung answer "no branch to measure" and keeps
// the ground (Rule 15a), the safe direction.
func runGroundAdoptBranch(dir string) string {
	out, err := taskGroundGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// scratchKey is the path identity the anchor map is keyed on — scratch's ONE
// canonical-spelling function (the same one runGroundCollectCloneVerdict compares
// against the workspace root), so two spellings of one ground cannot be adopted
// twice and a ground the removal leg would refuse cannot slip in by spelling.
func scratchKey(dir string) string {
	return scratch.CanonicalPath(dir)
}

// runGroundAged is the AGE RUNG, and it answers ONE question two ways because
// there are two KINDS of record (Rule 14: one implementation):
//
//	· an ACQUISITION anchor (Backfilled == false) was written at the moment the
//	  taker took the directory, so the RECORD's stamp IS the ground's age;
//	· a BACKFILL anchor (Backfilled == true) was written NOW, by this leg, for a
//	  ground that is already old — its stamp says nothing about the ground, so
//	  the DIRECTORY's mtime is measured instead.
//
// An unmeasurable age on either path is NOT "old enough" (Rule 15a): Aged stays
// false and Why names the reason, so the caller keeps the ground.
func runGroundAged(a RunGroundAnchor, now time.Time, minAge time.Duration) runGroundAgeVerdict {
	if !a.Backfilled {
		if a.At.IsZero() {
			return runGroundAgeVerdict{Why: "the anchor's timestamp could not be read (condition is unmeasurable)"}
		}
		if age := now.Sub(a.At); age < minAge {
			return runGroundAgeVerdict{Why: "the ground is younger than the floor (" + age.Round(time.Minute).String() + " < " + minAge.String() + ")"}
		}
		return runGroundAgeVerdict{Aged: true}
	}
	info, err := os.Stat(strings.TrimSpace(a.Worktree))
	if err != nil {
		return runGroundAgeVerdict{Why: "the backfilled ground's age could not be measured (" + groundFirstLine(err.Error()) + ")"}
	}
	if age := now.Sub(info.ModTime()); age < minAge {
		return runGroundAgeVerdict{Why: fmt.Sprintf(runGroundAdoptYoungFmt, age.Round(time.Minute).String(), minAge.String())}
	}
	return runGroundAgeVerdict{Aged: true}
}

// runGroundAgeVerdict is the age rung's answer: Aged, or not-aged with the
// sentence an operator reads. The two are one struct because a caller that read
// only the bool would have to re-derive the reason (Rule 14).
type runGroundAgeVerdict struct {
	Aged bool
	Why  string
}

// The two labels the sweep's backfill face prints. They live here, beside the
// criteria, so the render cannot drift from the decision (Rule 1b): a run that
// printed "collectable" for a ground the criterion REFUSED would be the silent
// failure Rule 15 forbids.
const (
	RunGroundAdoptAdoptableLabel = "adoptable"
	RunGroundAdoptKeptLabel      = "kept"
)

// RunGroundAdoptMinAge resolves the backfill's age floor from the sweep's own
// --min-age-h spelling, through the SAME resolver the sweep uses — one knob for
// "how long may a directory look abandoned" (Rule 14), so an operator who tunes
// it for the removal leg tunes it for the backfill too.
func RunGroundAdoptMinAge(minAgeH string) time.Duration {
	return WorktreeSweepOptionsFromFlags(WorktreeSweepFlags{MinAgeH: minAgeH}).MinAge
}
