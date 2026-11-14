package devworkspace

// delivery_anchor.go — THE ANCHOR GATE, and the "nothing to deliver" outcome.
//
// THE TWO DEFECTS THIS FILE EXISTS FOR (measured 2026-10-27, row 01a17dee):
//
//	① THE LEG PUBLISHED FROM A DIRECTORY NOBODY HAD COMMITTED IN. The row owned
//	  the ground /home/user/workspace/01a17dee-… and its delivery leg ran there,
//	  but the two real commits were in ANOTHER row's ground (01a17da2). The leg
//	  never asked "is the directory I am about to publish from the directory the
//	  run actually committed in?", so it published an empty tree.
//	② AN EMPTY TREE AND A REFUSED PULL WERE ONE SENTENCE. With nothing ahead of
//	  the base, the attempt went on to `pull --rebase`, which collided for a
//	  reason that had nothing to do with the run, and the operator read
//
//	    "clone channel: the pull step refused (ran: git pull --rebase origin master)"
//
//	  — a network/conflict story — while the true fact was "there is nothing here
//	  to publish". The reader is then sent to chase a ghost.
//
// THE SPLIT IS BY SHAPE, not by prose:
//
//	anchor mismatch    → an ERROR: it is not about the work in THIS directory at
//	                     all (the work is elsewhere, and no agent round standing
//	                     here can change that). Both paths are named.
//	nothing to deliver  → a BLOCKER of its own Kind (BlockerKindEmpty): the
//	                     directory is fine, the run simply has no commit here to
//	                     publish — a fact the next agent round CAN act on by
//	                     committing in this directory, and one that can never
//	                     again be reported through the pull's vocabulary.
//
// EVERY FACT HERE IS MEASURED, and the anchor refusal measures the SIDE TOO:
// which directory each path resolves to (`filepath.Abs` + `EvalSymlinks`, so two
// spellings of one directory are not a disagreement) and how many commits each
// one's HEAD carries ahead of the integration base (`git rev-list --count`). An
// unmeasurable value is an error, never a quiet pass (Rule 15).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// BlockerKindEmpty is "there is nothing here to deliver" — its OWN kind, so
	// it can never again be folded into the pull/conflict vocabulary. It is a
	// blocker rather than an error because the fact is about the work (this
	// directory holds no commit), which is exactly what the agent round that
	// receives a blocker is for.
	BlockerKindEmpty BlockerKind = "empty"

	// BlockerKindAlreadyDelivered is the OTHER meaning of the very same
	// `count == 0`, and it gets its own kind because the two are OPPOSITE facts
	// (row 01a182df, measured 2026-10-28): `git rev-list --count HEAD --not
	// origin/<base>` answering 0 says "nothing here to publish" ONLY until this
	// run's own push moves `origin/<base>` onto its tip — after which the same
	// number is the arithmetic of a COMPLETED delivery, not of an empty ground.
	// A reader who is told "you produced nothing" about work that is already on
	// the integration branch is sent to look for a commit that exists (Rule 15:
	// two reasons never share one sentence).
	//
	// It remains a BLOCKER, not an error: there is still nothing for this
	// attempt to publish, so the attempt stops here — what changes is the FACT
	// the reader is given, which is a measurement of this run's own delivery.
	BlockerKindAlreadyDelivered BlockerKind = "delivered"

	// BlockerKindInheritedDelivery is the THIRD meaning of the very same
	// `count == 0`, and it is the one that fires most often (measured: 33 ledger
	// rows matching 交付没有内容可发布, 50 sessions whose log carries `carries no
	// commit to deliver`). `git rev-list --count HEAD --not origin/<base>`
	// answering 0 says HEAD carries nothing the base lacks — and when the base
	// already CONTAINS HEAD while this run pushed nothing, the count is 0
	// because the content this ground was handed is ALREADY ON the integration
	// branch (a ground cut from a peer tip, a row whose work the server adopted,
	// a run asked to produce no change). The first draft of this gate folded
	// that into BlockerKindEmpty, whose sentence says "you are N behind, a pull
	// can recover" — a FALSE statement about an immutable fact.
	//
	// It BREAKS the loop, which is the second half of the defect: `empty` did
	// not stop the delivery loop, so up to MaxDeliveryRounds rounds each spawned
	// a real agent round that re-measured the same immutable fact and registered
	// a new ledger row. Nothing an agent round can do changes "your content is
	// already on the base", so the loop stops here exactly as it does for
	// BlockerKindAlreadyDelivered.
	BlockerKindInheritedDelivery BlockerKind = "inherited"

	// deliveryInheritedDeliveryMarker is the token the inherited report carries
	// and every other kind must never carry — the test asserts the kinds'
	// sentences are DIFFERENT, so one spelling is composed here (Rules 1/13).
	deliveryInheritedDeliveryMarker = "the content this run was handed is already on the integration branch"

	// deliveryShortShaLen is the abbreviation length this sentence names a commit
	// with: a full 40-character sha in a ledger reason pushes the reason past the
	// row's own summary budget, and a shorter one than git's default abbreviation
	// would not be copy-pasteable back into a command (Rule 1: named, not typed).
	deliveryShortShaLen = 7

	// deliveryInheritedDeliveryFmt is the ONE spelling of the inherited outcome:
	// the directory, the abbreviated tip whose content the base contains, and the
	// denials a reader acts on. It makes NO claim about pulling — the measures
	// this kind rests on are `ahead == 0` and "the base contains HEAD", and a
	// pull claim would be a third fact nobody measured here (Rule 15). `%s`
	// order: dir, short sha, integration spelling.
	deliveryInheritedDeliveryFmt = "%s carries no commit to deliver: `%s` answered 0 against %s, and %s is " +
		"contained by %s, so " + deliveryInheritedDeliveryMarker +
		": there is nothing to push, and this is NOT a failure. This is NOT a refused pull and NOT a " +
		"conflict — do not chase the network"

	// deliveryInheritedBehindFmt is the SAME fact when the peer's reading ALSO
	// counted commits the ground does not have — which is the normal state of an
	// inherited delivery, because the base moved on BECAUSE it landed this
	// ground's content and then other work. It reports the count as a fact about
	// OTHER PEOPLE'S work and STOPS THERE: no `pull --rebase` recovery clause,
	// because the base already CONTAINS this ground's HEAD, so a pull recovers
	// this run nothing it could deliver (row 01a19483: the pre-fix sentence
	// printed "nothing to push, but … recovers them" in one breath, sending the
	// reader to a network step that cannot change the outcome).
	//
	// `%s` order: dir, command, integration, short sha, integration, integration,
	// count — the MEASURED HEAD FIRST, exactly as its empty sibling, because the
	// ledger row is cut at 200 runes from the START and this is where the
	// measurable part has to live (row 01a18801's defect, one kind over).
	deliveryInheritedBehindFmt = "%s carries no commit to deliver: `%s` answered 0 against %s, and %s is " +
		"contained by %s, so " + deliveryInheritedDeliveryMarker +
		": there is nothing to push, and this is NOT a failure (%s still carries %d commit(s) this " +
		"ground does not have, which is other work — a pull would bring it in, and it is not a " +
		"delivery of this run). This is NOT a refused pull and NOT a conflict — do not chase the network"

	// deliveryAlreadyDeliveredMarker is the token the already-delivered report
	// carries and the empty one must never carry. Named (Rule 1/13) because the
	// test asserts the two texts are DIFFERENT and `Report` composes the
	// sentence — one spelling, so they cannot drift into agreement.
	deliveryAlreadyDeliveredMarker = "this run's delivery already landed"

	// deliveryAlreadyDeliveredFmt is the ONE spelling of the already-delivered
	// outcome: the directory, the tip this run pushed, and the integration
	// branch that now REACHES it (each a measurement this gate took itself).
	deliveryAlreadyDeliveredFmt = "%s carries no commit to deliver — but %s (%s): the tip this run pushed is " +
		"reachable from %s, so `%s` answers 0 because the delivery SUCCEEDED, not because this ground is empty. " +
		"There is nothing to push and nothing to repair"

	// deliveryAnchorRefusalFmt is the ONE spelling of the anchor refusal (Rule
	// 14: the sentence a caller prints and a test asserts are the same text).
	// It names the row's record, the directory the leg was asked to publish from,
	// and what it therefore refuses — then, in the parens, the MEASUREMENT that
	// makes the refusal a fact instead of an opinion.
	deliveryAnchorRefusalFmt = "deliverable.worktree is %s, but this attempt publishes from %s — " +
		"refusing to publish (%s)"

	// deliveryAnchorBothFmt is the reason clause when BOTH directories carry
	// commits ahead of the base: the two are different pieces of work, and this
	// channel publishes one tree at a time.
	deliveryAnchorBothFmt = "both directories carry commits ahead of %s (%d and %d), so publishing one of " +
		"them would silently leave the other behind"

	// deliveryEmptyRefusalFmt is the ONE spelling of "nothing to deliver": the
	// directory, the MEASURED count, the command that produced it, the pull/push
	// clause `%s` (two spellings below, chosen by a COUNT), and the two stories it
	// explicitly denies. `%s` order: dir, command, base, pull-push clause.
	deliveryEmptyRefusalFmt = "%s carries no commit to deliver: `%s` answered 0 against %s, %s. " +
		"This is NOT a refused pull and NOT a conflict — do not chase the network"

	// The pull/push clause has TWO measured spellings, and which one is printed is
	// decided by a count of its own — never by the ahead reading alone. `ahead == 0`
	// says this ground has nothing to PUBLISH; it says NOTHING about what the ground
	// is MISSING. The pre-fix sentence asserted both from the one reading, and row
	// 01a1826e is the measured cost: its refusal said "there is nothing to pull"
	// while `rev-list --count origin/master --not HEAD` answered 4 in that very
	// directory — a reader told not to look at the network while four landed commits
	// sat one `pull --rebase` away. That run spent its first two responses proving
	// the sentence wrong instead of delivering (Rule 15: an unmeasured claim inside
	// a refusal is worse than a shorter refusal).
	deliveryEmptyNothingToMoveText = "so there is nothing to pull and nothing to push"
	deliveryEmptyBehindFmt         = "so there is nothing to push, but %s carries %d commit(s) this ground " +
		"does not have — `%s` recovers them; this refusal is about publishing, not about fetching"

	// deliveryEmptySiblingFmt is the hint's ONE sentence: the candidate, the
	// address ref it carries, the MEASURED count of commits its tip has that the
	// integration branch does not, and a shape clause. Three shapes, three
	// statements (deliverySiblingRowGroundFmt / …ScratchFmt / …BaseReachableFmt),
	// all of them facts this file measured — see deliveryEmptySibling.
	deliveryEmptySiblingFmt = " (measured now: %s carries %s with %d commit(s) ahead — %s)"

	// deliveryAnchorRecordStaleFmt is the report for the ONE disagreement the
	// gate ALLOWS: the publish directory DOES carry the commits and the row's
	// record names another place. Refusing there would block a legitimate
	// delivery over a stale stamp, so the fact is said out loud and the delivery
	// proceeds (Rule 15 — no silent divergence either way).
	deliveryAnchorRecordStaleFmt = "the row records deliverable.worktree=%s, but this attempt publishes from " +
		"%s, which is the directory carrying the commits — delivering from the measured directory and " +
		"reporting the disagreement"

	// deliveryRevListVerb / deliveryCountFlag / deliveryNotFlag are git's own
	// spelling for the count this file takes; named once (Rule 1b/13).
	deliveryRevListVerb = "rev-list"
	deliveryCountFlag   = "--count"
	deliveryNotFlag     = "--not"
	// deliveryOriginPrefix is how git spells the remote-tracking namespace of the
	// integration point this count is taken against.
	deliveryOriginPrefix = "origin/"
	// deliveryCatFileVerb / deliveryExistsFlag ask whether an OBJECT is in this
	// directory's object store — the only read that answers it. `rev-parse
	// --verify` echoes a full 40-hex sha whether or not the object is there
	// (measured while building the abstention arm: a clone that had never seen
	// the base's new tip "carried" it), so it must never be the existence probe.
	deliveryCatFileVerb = "cat-file"
	deliveryExistsFlag  = "-e"
	// revParseVerifyQuiet are the flags of the read-only ref existence probe.
	revParseVerifyQuiet = "--verify"
	revParseQuietFlag   = "--quiet"

	// deliveryRefsRemoteFmt is git's spelling of a remote-tracking namespace,
	// and of one ref inside it. Named once (Rule 1b/13): the base tip, the
	// child-tip measurement and the integration spelling below are all built
	// from them.
	deliveryRefsRemoteFmt = "refs/remotes/%s/%s"
	// deliveryIntegrationFmt is the REMOTE-TRACKING spelling of the integration
	// branch — the ref a count is taken "against". It is a NAME, and how exact
	// that name is depends ENTIRELY on the directory's refspec (see
	// deliveryBaseTip): a clone whose fetch refspec is `refs/tags/*` keeps no
	// `origin/<base>` at all, so this name is only ever REPORTED, never trusted.
	deliveryIntegrationFmt = deliveryOriginPrefix + "%s"
	// deliveryShapeRowGround / …Scratch / …BaseReachable are the three SHAPES a
	// candidate directory can have, each stated as the measured fact it is:
	//   · its address ref points at the very commit its HEAD points at → the
	//     checkout IS this row's ground (the address was minted there);
	//   · that ref points at a DIFFERENT commit than its HEAD → it was created
	//     for a previous attempt and the directory has moved since (a scratch
	//     clone — the very thing the checks-out-a-given-branch defect produces);
	//   · the tip carries nothing the integration branch lacks → it IS the
	//     integration branch's own tip, a clone of it, never "work in flight".
	deliveryShapeRowGround = "that ref points at the very commit that directory's HEAD points at, so the " +
		"checkout is this row's ground; the address ref is a stamp, not evidence about that work"
	deliveryShapeScratch = "that ref's tip is NOT that directory's HEAD, so the ref was created for a previous " +
		"attempt and the checkout has moved since — a scratch clone of an earlier run, not a ground " +
		"standing on this row's address"
	// deliveryEmptySiblingUnmeasuredFmt is the ABSTENTION: candidates that carry
	// the address ref but cannot be measured, because the commit the count is
	// taken against is not in their object store. Saying it is the difference
	// between "nothing is in flight" and "nothing was measured" (Rule 15).
	deliveryEmptySiblingUnmeasuredFmt = " (%d other candidate(s) under %s carry this row's address ref %s " +
		"but cannot read the integration tip %s, so how many commits they carry that the base lacks " +
		deliveryUnmeasuredMark + ")"

	// deliverySiblingRecoveryFmt is the ONE actionable step this hint owes its
	// reader once it has named a directory — WHERE is not a recovery, and the
	// measured cost of stopping at WHERE is row 01a1826e: its delivery attempt
	// was blocked, the refusal named the sibling carrying the commits, and the
	// step that would have recovered them had to be invented by hand inside a
	// delivery budget. `%s` order: fetch verb, candidate dir, full ref name.
	//
	// It fetches from the SIBLING DIRECTORY, never from origin: the sibling's
	// commit is this run's own work that was never pushed, so origin does not
	// carry it (measured on row 01a17e74: origin's master never held 61d0d36).
	// The ref is spelled in FULL — `refs/heads/<branch>`, the row's address — so
	// the command cannot land on whatever the sibling's checkout stands on.
	deliverySiblingRecoveryFmt = " — bring those commits into this ground with `git %s %s %s` and " +
		"deliver from HERE; fetching from origin will not help, nothing pushed them"

	// deliveryFetchVerb is git's own word for the one step above, named once
	// (Rule 1b/13) so the command a reader copies is the command this file
	// builds.
	deliveryFetchVerb = "fetch"

	// deliveryPeerRefFmt asks the PEER — never a local ref — where the
	// integration base is now. It is the ONE read whose answer the empty gate's
	// behind count may be taken from: `refs/remotes/origin/<base>` only advances
	// when something FETCHES, and nothing on this leg fetches before the gate
	// (the first network step is the pull, which an empty ground never reaches),
	// so on a workspace clone that ref is a snapshot of CLONE TIME. Measured on
	// row 01a18317's own ground, 2026-10-28: origin/master read 977e8ca (the tip
	// the clone was cut from at 23:05:47) while the peer carried c22e1c3, three
	// commits landed at 23:11:56 — so the count answered 0 and the refusal still
	// said "there is nothing to pull".
	deliveryPeerRefFmt = refsHeadsPrefix + "%s"

	// gateReflogVerb / gateReflogShowVerb are git's own spelling for the ONE
	// read this file takes of a ref's reflog — the record a PUSH writes and a
	// FETCH writes differently. deliveryReflogNewestFlag reads a single entry
	// (the newest), deliveryReflogFormatFlag+deliveryReflogFormat prints the
	// commit and the update's SUBJECT on one line, separated by
	// deliveryReflogFieldSep (git's own `%H %gs` is space-separated, and a
	// subject may itself contain spaces — so the split is on the FIRST space,
	// which is what strings.Cut does with this separator).
	gateReflogVerb           = "reflog"
	gateReflogShowVerb       = "show"
	deliveryReflogNewestFlag = "-1"
	deliveryReflogFormatFlag = "--format="
	deliveryReflogFormat     = "%H %gs"
	deliveryReflogFieldSep   = " "
	// gitUpdateByPushSubject is git's OWN subject for a reflog update a push
	// wrote, verbatim, and the unpushable half of the shape-① discriminator
	// (measured on this tree, 2026-11-02:
	// `git reflog show --format='%H %gs' refs/remotes/origin/master` →
	// `<sha> update by push`). gitFetchFastForwardSubject is the sibling a
	// FETCH writes, and the test uses it as its counter-example — a fetch is
	// not a delivery.
	gitUpdateByPushSubject     = "update by push"
	gitFetchFastForwardSubject = "fetch -q " + originRemoteName + ": fast-forward"

	// deliveryPeerRefPeeledSuffix is what `ls-remote` prints for a TAG's target
	// ("^{}") — accepted here so the reader cannot be fooled if a peer ever
	// advertises the base that way; the unpeeled line is tried first.
	deliveryPeerRefPeeledSuffix = "^{}"

	// deliveryFileURLScheme turns a LOCAL PATH operand into an address rather
	// than an upload-pack invocation (see deliveryPeerBaseTip).
	deliveryFileURLScheme = "file://"

	// pathSeparator / currentDirPrefix are what tell "a repository address some
	// transport serves" (`zpeer:/home/user/work/gopi`, `git@host:repo`) from "a
	// directory on this machine" (`/srv/repo`, `./repo`). A file:// address is
	// built ONLY from the second kind — prefixing an ssh address with file://
	// would ask the wrong transport and the answer would be a failure that looks
	// like an unreachable peer.
	pathSeparator    = "/"
	currentDirPrefix = "."

	// gitRemoteVerb / gitGetURLVerb ask the GROUND which address its own `origin`
	// stands for — the very address the recovery sentence tells the reader to pull
	// from, so the measurement and the advice cannot name two different peers.
	gitRemoteVerb = "remote"
	gitGetURLVerb = "get-url"

	// deliveryNoTagsFlag / deliveryQuietFlag / fetchHeadRef are the flags and the
	// alias of the ONE fetch this file runs — the counting fetch below. `--no-tags`
	// keeps it from dragging the peer's tags into this store, and FETCH_HEAD is
	// where a fetch of an explicit ref lands, so the count never depends on a
	// remote-tracking ref advancing.
	deliveryNoTagsFlag = "--no-tags"
	deliveryQuietFlag  = "-q"
	fetchHeadRef       = "FETCH_HEAD"

	// deliveryHexSHA1Len / deliveryHexSHA256Len are the two lengths a full object
	// id can have, and the check that a peer's answer is one.
	deliveryHexSHA1Len   = 40
	deliveryHexSHA256Len = 64

	// deliveryUnmeasuredMark is the phrase a reader (and a test) recognises an
	// abstention by: it is the ONE way this file says "asked, not answered",
	// and it lives in a constant so the sentence and its assertion agree.
	deliveryUnmeasuredMark = "was NOT measured there"

	// ── THE HEAD-DIRECTORY ASSERTION (deliveryHeadDirGate) ──────────────────
	//
	// THE HOLE THIS CLOSES (measured 2026-10-29 on this host): every step of
	// this leg runs `git -C <dir> …`, and GIT WALKS UP. A `dir` that is not a
	// work tree of its own — a directory that was never cloned, a ground that
	// was wiped and re-created, a path inside another row's checkout — makes
	// `git -C <dir> rev-parse HEAD` answer with the HEAD OF THE ENCLOSING
	// REPOSITORY, exit 0, no warning. Measured here: with `.probe-anchor/nested`
	// (no `.git` of its own) inside this checkout,
	// `git -C .probe-anchor/nested rev-parse --show-toplevel` printed
	// /home/user/workspace/01a17dfb-… and
	// `git -C .probe-anchor/nested rev-parse HEAD` printed this repo's HEAD.
	// So every count this file takes ("ahead of the base", "carries no commit")
	// could be about ANOTHER DIRECTORY while the refusal names the one it was
	// asked about — which is exactly how a leg that never looked at its own
	// ground blames `pull --rebase`.
	//
	// THE ASSERTION IS THEREFORE FIRST, BEFORE ANY COUNT: the directory this
	// attempt publishes from must be the directory HEAD lives in, MEASURED by
	// git itself, and the refusal names both when they differ. It cannot live
	// inside deliveryAnchorGate: that gate asks a question about the ROW'S
	// RECORD and abstains when the record is absent, while this one is about the
	// PUBLISH DIRECTORY and has an opinion whether or not a row records
	// anything.
	deliveryTopLevelVerb = "rev-parse"
	deliveryTopLevelFlag = "--show-toplevel"

	// deliveryHeadElsewhereFmt is the ONE spelling of the head-directory
	// refusal. Three facts, in the order a reader needs them: which directory it
	// was asked to publish from, which directory HEAD actually lives in (git's
	// own answer), and WHY that makes every later step a lie about the wrong
	// directory. `%s` order: publish dir, measured head dir, the probe command,
	// publish dir, head dir.
	deliveryHeadElsewhereFmt = "this attempt was told to publish from %s, but that directory's HEAD lives in %s " +
		"(git %s %s answered the enclosing work tree) — refusing to publish: every count this leg " +
		"would take about %s would actually be about %s"

	// deliveryHeadUnreadableFmt is the abstention's counterpart on this gate: a
	// directory whose head directory cannot be measured is not published from
	// (Rule 15 — an unmeasured question is never a pass).
	deliveryHeadUnreadableFmt = "cannot measure which directory the HEAD of %s lives in: `git %s %s` failed there " +
		"— refusing to publish from a directory nobody could identify"

	// deliveryHeadMatchesFmt is what the leg says when the assertion HOLDS — one
	// line, so an operator reading a delivery log can tell "the gate asked and
	// the answer was yes" from "the gate did not run".
	deliveryHeadMatchesFmt = "the publish directory %s is where its HEAD lives (measured, not assumed)"
)

// deliveryHeadDir reports the directory in which dir's HEAD actually lives, as
// git itself answers it (`rev-parse --show-toplevel`), resolved to an identity so
// two spellings of one directory never read as a disagreement.
//
// WHY THIS IS NOT `isGitWorkTree`: that predicate stats `<dir>/.git` and answers
// "does this directory claim to be a checkout of its own". It says NOTHING about
// where git will actually READ from — and git WALKS UP. A directory with no
// `.git` at all, sitting anywhere inside another repository's tree, answers
// `git -C <dir> rev-parse HEAD` with THAT repository's HEAD, exit 0, no warning
// (measured 2026-10-29 on this host; see the block above
// deliveryTopLevelVerb). So the question this file must ask is git's, and the
// answer is a PATH, never a boolean.
//
// An empty answer means NOT MEASURED — the directory does not exist, git is not
// installed, or git refused (a bare repository, an unreadable parent). The
// caller refuses; it never treats silence as agreement (Rule 15).
func deliveryHeadDir(ctx context.Context, dir string) (string, error) {
	out, err := runCommand(ctx, dir, gitBinary, deliveryTopLevelVerb, deliveryTopLevelFlag)
	if err != nil {
		return "", fmt.Errorf(deliveryHeadUnreadableFmt, dir, deliveryTopLevelVerb, deliveryTopLevelFlag)
	}
	head := deliveryDirIdentity(out)
	if head == "" {
		return "", fmt.Errorf(deliveryHeadUnreadableFmt, dir, deliveryTopLevelVerb, deliveryTopLevelFlag)
	}
	return head, nil
}

// deliveryHeadDirGate asserts that the directory this attempt is about to publish
// from is the directory its commits actually live in.
//
// THE DEFECT IT CLOSES — the "found commits in <dir>" half of the layering. Every
// count this leg takes (`rev-list --count HEAD --not <base>`, "carries no
// commit to deliver", "N commit(s) ahead") is a `git -C <dir>` question, so when
// `<dir>` is not a work tree of its own git answers about the ENCLOSING
// repository. The leg then reports facts about a directory it never named, and
// the operator reads a refusal about the wrong place — or, worse, a `pull
// --rebase` collision that has nothing to do with the run (the shape of row
// 01a17dee). This gate runs FIRST, before the anchor gate (which needs a count
// itself) and before the empty gate, so no count is ever taken on an
// unqualified directory.
//
// IT IS AN ERROR, NEVER A BLOCKER, for the same reason deliveryAnchorGate's
// mismatch is one: no agent round standing in THIS directory can change where
// its HEAD lives — the directory is not a checkout, and committing "here" would
// commit into somebody else's ground. A repair needs a ground that exists; that
// is the operator's call, not a round's.
//
// BOTH PATHS ARE NAMED, and the sentence says which is which: the one the leg was
// told to publish from, and the one git measured. An operator copies the second
// into a command; naming only the first is what made the original defect
// unreadable.
func deliveryHeadDirGate(ctx context.Context, cfg Config, dir string) error {
	head, err := deliveryHeadDir(ctx, dir)
	if err != nil {
		return err
	}
	if head == deliveryDirIdentity(dir) {
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "clone-channel: "+deliveryHeadMatchesFmt+"\n", dir)
		}
		return nil
	}
	return fmt.Errorf(CloneChannelPrefix + deliveryHeadElsewhereFmt,
		dir, head, deliveryTopLevelVerb, deliveryTopLevelFlag, dir, head)
}

// deliveryDirIdentity answers "which directory is this, really" — an absolute,

// symlink-resolved path, so two spellings of one directory never read as a
// disagreement (a row whose deliverable.worktree was recorded through a symlink
// is the same ground as the real path the channel opened).
func deliveryDirIdentity(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		return resolved
	}
	// A path that cannot be resolved (it does not exist) is compared as spelled:
	// dropping the comparison would turn a real difference into a pass.
	return filepath.Clean(abs)
}

// deliveryRefTip reads one ref's tip in dir. It answers the sha and the exit
// code the read ended with, BECAUSE THE CODE IS THE ANSWER: 0 = the ref exists
// and this is its tip, 1 = no such ref here, and anything else = git could not
// look (Rule 4.10 — the status is never a proxy for a measurement).
func deliveryRefTip(ctx context.Context, dir, ref string) (string, int) {
	out, err := runCommand(ctx, dir, gitBinary, gateTipVerb, "--verify", "--quiet", ref)
	if err == nil {
		return strings.TrimSpace(out), 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return "", ee.ExitCode()
	}
	return "", -1
}

// deliveryFullRef spells one branch inside the remote-tracking namespace.
func deliveryFullRef(branch string) string {
	return fmt.Sprintf(deliveryRefsRemoteFmt, originRemoteName, branch)
}

// deliveryPeerOperand spells the address and the ref of the peer's integration
// branch, from the clone's OWN remote. A LOCAL PATH operand is turned into a
// file:// address: the `uploadpack` hooks a receiving repository may carry are a
// surface a read-only measurement must not run, and a file:// transport is the
// same repository over the arm this file honours.
func deliveryPeerOperand(remote, base string) (operand, ref string) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", ""
	}
	operand = remote
	if strings.HasPrefix(remote, pathSeparator) || strings.HasPrefix(remote, currentDirPrefix) {
		if abs, err := filepath.Abs(remote); err == nil {
			operand = deliveryFileURLScheme + abs
		}
	}
	return operand, fmt.Sprintf(deliveryPeerRefFmt, base)
}

// deliveryOriginURL answers the address the GROUND's own `origin` stands for,
// falling back to the configured clone source when the ground cannot say (a
// caller that has only a config, a directory that is not a checkout). One
// function, so the measurement and the recovery sentence agree on the peer.
func deliveryOriginURL(ctx context.Context, dir, configured string) string {
	if out, err := runCommand(ctx, dir, gitBinary, gitRemoteVerb, gitGetURLVerb, originRemoteName); err == nil {
		if url := strings.TrimSpace(out); url != "" {
			return url
		}
	}
	return strings.TrimSpace(configured)
}

// deliveryPeerBaseTip asks the PEER where the integration base is NOW.
//
// It is `ls-remote <remote> refs/heads/<base>` — a read of the remote itself,
// which writes nothing (no fetch, no ref update) and therefore cannot perturb the
// very comparison it feeds. It is the ONE answer a behind reading may rest on:
// `refs/remotes/origin/<base>` only advances when something FETCHES, and nothing
// on this leg fetches before the empty gate (the first network step is the pull,
// and the pull is exactly what an empty ground never reaches). Measured on row
// 01a18317's own ground, 2026-10-28: origin/master read 977e8ca, the tip the
// clone was cut from at 23:05:47, while the peer carried c22e1c3 with three
// commits landed at 23:11:56 — so the count answered 0 and the refusal still
// said "there is nothing to pull".
//
// It answers "" for EVERY unmeasured shape — the remote did not answer, or it
// does not carry the branch — because a caller's next move is the ABSTENTION
// (Rule 15), never a count taken against a local opinion.
func deliveryPeerBaseTip(ctx context.Context, dir, remote, base string) (tip string) {
	return deliveryPeerReadingTip(ctx, dir, remote, base).tip
}

// deliveryPeerReading is ONE measurement of where the peer's integration base is,
// carried as a VALUE rather than re-taken per question.
//
// WHY IT IS A VALUE (row 01a19483, measured 2026-11-01). The empty gate asks the
// same peer two questions — "how many commits am I missing?" and "which shape of
// `ahead == 0` is this?" — and asking them separately let each be answered from a
// different world: the count asked the PEER while the shape was taken against
// `refs/remotes/origin/<base>`, a ref only a FETCH advances (and nothing here
// fetches before the gate). On a ground the base had moved past, the stale tip
// still equalled HEAD, so the "genuinely empty" branch was chosen forever, the
// inherited shape could not be reached, and the sentence contradicted itself in
// one breath: "carries no commit to deliver … but origin/master carries 2
// commit(s) this ground does not have". A round cannot change "the content this
// ground was handed is already on the base", so the loop re-measured an immutable
// fact once per round. ONE reading, taken once, is the fix.
//
// The zero value is the UNMEASURED answer (`tip == ""`), which is what every
// caller must abstain on — never "false", never a fallback to a local ref.
type deliveryPeerReading struct {
	// operand / ref are how this peer is addressed for the one fetch the count
	// may need; they are resolved HERE so the measurement and that fetch can
	// never name two different remotes.
	operand string
	ref     string
	// tip is the peer's answer, or "" when it gave none.
	tip string
}

// deliveryPeerReadingTip takes that one reading.
func deliveryPeerReadingTip(ctx context.Context, dir, remote, base string) deliveryPeerReading {
	operand, ref := deliveryPeerOperand(remote, base)
	if operand == "" {
		return deliveryPeerReading{}
	}
	return deliveryPeerReading{operand: operand, ref: ref, tip: deliveryPeerTipFrom(ctx, dir, operand, ref)}
}

// deliveryPeerTipFrom is the `ls-remote` parse: the peer's live answer for one
// fully-spelled ref, or "" when the peer did not answer or does not carry it.
func deliveryPeerTipFrom(ctx context.Context, dir, operand, ref string) (tip string) {
	out, err := runCommand(ctx, dir, gitBinary, gitListRemoteVerb, operand, ref)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if (fields[1] == ref || fields[1] == deliveryPeerRefPeeledSuffix+ref) && isFullHexObjectID(fields[0]) {
			return fields[0]
		}
	}
	return ""
}

// isFullHexObjectID answers whether s is a full object id (SHA-1 or SHA-256): the
// only shape `ls-remote`'s first column takes when the peer really carries the
// ref. A remote that does not carry it prints NOTHING, and a failed verb can
// leave a word behind — so the column is checked, not assumed.
func isFullHexObjectID(s string) bool {
	if len(s) != deliveryHexSHA1Len && len(s) != deliveryHexSHA256Len {
		return false
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		if !isDigit && !isLower {
			return false
		}
	}
	return true
}

// deliveryCommitsMissing counts the commits the PEER's base tip carries that dir
// does NOT have — the number the empty refusal quotes.
//
// IT TAKES ITS READING AS A PARAMETER, and that is the fix for row 01a19483, not
// a style choice: the empty gate asks the same peer TWO questions (this count,
// and "which shape of ahead == 0 is this?"), and when each took its own reading
// they landed on two different tips — the count on the PEER's, the shape on a
// remote-tracking ref nothing on this leg fetches. ONE reading, taken once by the
// caller, is the shape. An unmeasured reading is an unmeasured count: ok=false,
// with no fallback to a local opinion.
//
// TWO STEPS, and the second is the only network step this file owns:
//
//	① the peer's tip is already in this store (a ground that fetched, or one
//	  whose clone was taken after the base last moved) → `rev-list --count
//	  <peerTip> --not HEAD`, exact, no network.
//	② the object is absent — which is the normal state of a ground the base has
//	  moved past. It CANNOT be counted from a local ref: an object this store
//	  does not hold cannot be walked, and the tree-identity idiom git's own "you
//	  are behind" notice uses answers `fatal: Invalid symmetric difference
//	  expression` for exactly that reason (measured; see the file's evidence
//	  note). So the commit is brought in by the ONE fetch — explicit ref, no
//	  tags, no branch update — and `FETCH_HEAD` is counted.
//
// A FAILED fetch is not an answer: it returns ok=false and the caller prints the
// claim-free clause (Rule 15). The fetch cannot move this ground: it updates no
// local branch and no remote-tracking ref (git says so itself when it fetches an
// explicit refspec into FETCH_HEAD only), so the work the attempt is about to
// publish is untouched.
func deliveryCommitsMissing(ctx context.Context, dir string, reading deliveryPeerReading) (int, bool) {
	peerTip := reading.tip
	if peerTip == "" {
		return 0, false
	}
	tip := peerTip
	if !deliveryCarriesCommit(ctx, dir, peerTip) {
		if reading.operand == "" || reading.ref == "" {
			return 0, false
		}
		if _, err := runCommand(ctx, dir, gitBinary, deliveryFetchVerb, deliveryNoTagsFlag,
			deliveryQuietFlag, reading.operand, reading.ref); err != nil {
			return 0, false
		}
		fetched, _ := deliveryRefTip(ctx, dir, fetchHeadRef)
		if fetched == "" {
			return 0, false
		}
		tip = fetched
	}
	n, err := deliveryOutstanding(ctx, dir, tip, gitHeadRef)
	if err != nil {
		return 0, false
	}
	return n, true
}

// deliveryBaseTip answers WHERE the integration base actually is in dir.
//
// WHY THIS IS NOT `origin/<base>`: a clone's refspec decides which remote refs
// it keeps, and the two ground shapes on this host keep DIFFERENT ones. The
// workspace's own clone keeps `+refs/heads/*:refs/remotes/origin/*`, so
// `origin/master` is a true remote-tracking ref. A scratch clone made with
// `--single-branch --branch <ground>` keeps no such thing — its
// `origin/master` is the branch's own fork point and stays there forever.
// Measured on this host, 2026-10-27:
//
//	$ git ls-remote origin master                    → 26008d87…  (the base moves)
//	$ git rev-parse refs/remotes/origin/master        → 20c15007…  (unchanged)
//
// so `git rev-list --count HEAD --not origin/master` counted 11 commits that
// the REAL base had already carried for hours: a count taken against a local
// opinion of the base, never against the base. The resolved ref is tried
// FIRST; a directory that keeps no such ref falls back to the base BRANCH NAME
// itself, which is only ever right when that name does resolve to the base —
// hence the report says which spelling produced the tip.
func deliveryBaseTip(ctx context.Context, dir, base, ground string) (tip, integration string, err error) {
	// A GROUND branch is never its own base (the same rule baseBranchOf states
	// for the pull): counting the address against itself would answer 0 always.
	ground = strings.TrimSpace(ground)
	base = strings.TrimSpace(base)
	if base == "" || base == ground {
		return "", "", fmt.Errorf(CloneChannelPrefix + "the integration base of %s is %q, which cannot be "+
			"measured (a ground branch is never its own base)", dir, base)
	}
	ref := deliveryFullRef(base)
	if sha, _ := deliveryRefTip(ctx, dir, ref); sha != "" {
		return sha, fmt.Sprintf(deliveryIntegrationFmt, base), nil
	}
	// THE ABSTENTION IS SAID, not hidden: with no remote-tracking ref the base
	// is named by its own branch name, and the caller's sentence carries that
	// spelling with the tip it resolved to.
	out, err := runCommand(ctx, dir, gitBinary, gateTipVerb, "--verify", "--quiet", base)
	if err != nil {
		return "", "", fmt.Errorf(CloneChannelPrefix + "the integration base %s cannot be resolved in %s "+
			"(neither %s nor %q): %w (output: %s)", base, dir, ref, base, err, tail(out))
	}
	return strings.TrimSpace(out), base, nil
}

// deliveryShortSha abbreviates a full sha for a sentence (Rule 1: the length is
// a named constant). A resolved value shorter than the abbreviation length is
// returned unchanged, so a caller never slices past its input.
func deliveryShortSha(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= deliveryShortShaLen {
		return sha
	}
	return sha[:deliveryShortShaLen]
}

// deliveryCarriesCommit answers whether dir can READ commit at all — the
// precondition of measuring anything against a tip (a clone taken before the
// base moved holds no such object, and no fetch is this leg's to run).
func deliveryCarriesCommit(ctx context.Context, dir, commit string) bool {
	_, err := runCommand(ctx, dir, gitBinary, deliveryCatFileVerb, deliveryExistsFlag, commit)
	return err == nil
}

// deliveryReachedFromBase answers whether tip carries NOTHING the integration
// base lacks — i.e. it is already history the base landed.
//
// THE THIRD VALUE IS NOT "FALSE": ok=false means the question was NOT answered
// (git could not look), and a caller must then say it was not measured rather
// than treat it as either answer (Rule 15).
func deliveryReachedFromBase(ctx context.Context, dir, tip, baseTip string) (reached, ok bool) {
	_, err := runCommand(ctx, dir, gitBinary, gitMergeBaseVerb, gitIsAncestorFlag, tip, baseTip)
	if err == nil {
		return true, true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == exitNotAncestor {
		return false, true
	}
	return false, false
}

// deliveryOwnPushLanded answers whether THIS clone's own push put the
// remote-tracking ref of the integration base onto tip — the unforgeable half of
// shape ① that no caller has to supply.
//
// The evidence is the ground's reflog for that ref, newest entry first: git
// writes gitUpdateByPushSubject as the update's subject when a PUSH moves the
// local remote-tracking ref, and a FETCH writes its own subject instead
// (measured on this tree, 2026-11-02: `update by push` vs `fetch -q origin:
// fast-forward`). Two spellings of the same fact are therefore distinguishable
// by the one thing a caller cannot fake by accident.
//
// AND IT CANNOT BE BOUGHT: a push refuses to move a remote-tracking ref onto a
// commit this clone does not already hold, so an entry naming some other commit
// is not something a push could have written. The measurement does not rest on
// that alone — it demands the entry name THIS very tip, at the level the ref
// moved.
//
// THE ABSTENTION IS THE DEFAULT (Rule 15): no reflog, an unreadable verb, a
// newest entry that says something else, an empty head — every one of them
// answers false, and the caller then prints the empty kind's claim-free clause.
// "The question could not be put" is never evidence of a delivery.
func deliveryOwnPushLanded(ctx context.Context, dir, base, tip string) bool {
	if strings.TrimSpace(tip) == "" {
		return false
	}
	out, err := runCommand(ctx, dir, gitBinary, gateReflogVerb, gateReflogShowVerb,
		deliveryReflogNewestFlag, deliveryReflogFormatFlag+deliveryReflogFormat, deliveryFullRef(base))
	if err != nil {
		return false
	}
	newest := strings.TrimSpace(out)
	if newest == "" {
		return false
	}
	sha, subject, ok := strings.Cut(newest, deliveryReflogFieldSep)
	if !ok || strings.TrimSpace(subject) != gitUpdateByPushSubject {
		return false
	}
	return strings.TrimSpace(sha) == strings.TrimSpace(tip)
}

// deliveryOutstanding counts the commits tip carries that the integration base
// does not — the ONE measurement a message quotes.
func deliveryOutstanding(ctx context.Context, dir, tip, baseTip string) (int, error) {
	out, err := runCommand(ctx, dir, gitBinary, deliveryRevListVerb, deliveryCountFlag, tip,
		deliveryNotFlag, baseTip)
	if err != nil {
		return 0, fmt.Errorf(CloneChannelPrefix + "count the commits %s carries against %s in %s: %w (output: %s)",
			tip, baseTip, dir, err, tail(out))
	}
	n, perr := strconv.Atoi(strings.TrimSpace(out))
	if perr != nil {
		return 0, fmt.Errorf(CloneChannelPrefix + "`%s` printed %q, which is not a commit count: %w",
			deliveryRevListVerb+" "+deliveryCountFlag+" "+tip+" "+deliveryNotFlag+" "+baseTip,
			strings.TrimSpace(out), perr)
	}
	return n, nil
}

// deliveryOutstandingOrNone is deliveryOutstanding for a directory that may not
// be a usable repository at all (a recorded worktree that was deleted, or one
// whose base ref does not exist). It answers (0, reason) rather than an error,
// because "that other directory cannot be read" is not a refusal — it is the
// absence of the evidence a refusal would need, and the caller says so instead
// of guessing.
func deliveryOutstandingOrNone(ctx context.Context, dir, base string) (int, string) {
	if strings.TrimSpace(dir) == "" {
		return 0, "no directory"
	}
	if !isGitWorkTree(dir) {
		return 0, "not a git checkout"
	}
	baseTip, _, err := deliveryBaseTip(ctx, dir, base, "")
	if err != nil {
		return 0, "unreadable: " + tail(err.Error())
	}
	n, err := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip)
	if err != nil {
		return 0, "unreadable: " + tail(err.Error())
	}
	return n, ""
}

// deliverySiblingShape says what KIND of directory the candidate is, from the
// ONLY facts that distinguish the two: does its address ref point at its own
// HEAD (a ground standing on this row's address), or elsewhere (a ref left by a
// previous attempt while the checkout has moved)?
//
// It answers "" when the candidate's own HEAD could not be read: a shape nobody
// measured is never reported as either of the two (Rule 15). A tip the base
// already carries never reaches here — deliveryEmptySibling filters it out on
// the ancestry measurement, so "already landed" is a reason NOT to speak rather
// than a third sentence.
func deliverySiblingShape(candTip, tip string) string {
	if candTip == "" {
		return ""
	}
	if candTip == tip {
		return deliveryShapeRowGround
	}
	return deliveryShapeScratch
}

// deliveryAnchorGate asks the question the leg never asked: is the directory
// about to be published from the directory the row's work actually lives in?
//
// THE MEASUREMENT, not the spelling, decides — and the refusal is only made
// where publishing would be WRONG:
//
//	publish == recorded            → nothing to ask.
//	publish empty, recorded ahead  → REFUSE: the commits are somewhere else, and
//	                                 no agent round in this directory can reach
//	                                 them. Both paths are named.
//	both ahead                     → REFUSE: two different pieces of work; this
//	                                 channel publishes one tree at a time.
//	publish ahead, recorded empty  → DELIVER, and say that the row's record is
//	                                 stale: the commits ARE in the publish
//	                                 directory, so refusing would block a
//	                                 legitimate delivery over a bad stamp.
//	neither ahead                  → no refusal here; that is the EMPTY gate's
//	                                 fact, and it has its own words.
//
// An absent recorded ground asks no question (the legacy shape and every caller
// with no row to name): this file never invents a value to make a comparison
// succeed.
func deliveryAnchorGate(ctx context.Context, cfg Config, dir, base string) error {
	recorded := strings.TrimSpace(cfg.Worktree)
	if recorded == "" {
		return nil
	}
	here, there := deliveryDirIdentity(dir), deliveryDirIdentity(recorded)
	if here == "" || there == "" {
		// Unmeasurable is not a pass: name the side that could not be resolved
		// instead of publishing from a directory nobody could identify.
		return fmt.Errorf(CloneChannelPrefix + "cannot decide whether %q is the directory this row's deliverable lives in "+
			"(%q): one side resolves to no path; refusing to publish from %q", dir, recorded, dir)
	}
	if here == there {
		return nil
	}
	pubAhead, pubWhy := deliveryOutstandingOrNone(ctx, dir, base)
	recAhead, recWhy := deliveryOutstandingOrNone(ctx, recorded, base)
	pubWhy = strings.TrimSpace(pubWhy)
	recWhy = strings.TrimSpace(recWhy)
	if pubWhy != "" {
		pubAhead = 0
	}
	if recWhy != "" {
		recAhead = 0
	}
	switch {
	case recAhead > 0 && pubAhead > 0:
		return fmt.Errorf(CloneChannelPrefix + deliveryAnchorRefusalFmt,
			recorded, dir,
			fmt.Sprintf(deliveryAnchorBothFmt, deliveryOriginPrefix+base, recAhead, pubAhead))
	case recAhead > 0:
		return fmt.Errorf(CloneChannelPrefix + deliveryAnchorRefusalFmt,
			recorded, dir,
			fmt.Sprintf("%d commit(s) ahead of %s live there; this directory carries %d",
				recAhead, deliveryIntegrationFmt, pubAhead))
	case pubAhead > 0:
		// THE STALE-STAMP ARM: the commits are HERE, so the delivery proceeds —
		// loudly, because the row's record and the measurement disagree.
		if cfg.Log != nil {
			fmt.Fprintf(cfg.Log, "clone-channel: "+deliveryAnchorRecordStaleFmt+"\n", recorded, dir)
		}
		return nil
	default:
		// Neither directory carries work: the empty gate owns that sentence.
		return nil
	}
}

// deliveryEmptyGate decides whether this attempt has anything to publish at all.
//
// It is asked AFTER the anchor gate (a wrong directory is a different fact from
// an empty one) and BEFORE `pull --rebase` — that order IS the split: an empty
// workspace never reaches the pull, so its refusal can never again be worded as
// a pull failure.
func deliveryEmptyGate(ctx context.Context, cfg Config, dir, base string) (*Blocker, error) {
	baseTip, integration, err := deliveryBaseTip(ctx, dir, base, cfg.GroundBranch)
	if err != nil {
		return nil, err
	}
	ahead, err := deliveryOutstanding(ctx, dir, gitHeadRef, baseTip)
	if err != nil {
		return nil, err
	}
	if ahead > 0 {
		return nil, nil
	}
	// THE SECOND READING, AND IT IS ABOUT THIS RUN, NOT ABOUT THE NUMBER.
	// `ahead == 0` has TWO meanings, and the one-dimensional count cannot tell
	// them apart: the ground produced nothing, OR this run's OWN push already
	// moved the integration base onto this very tip — after which the count is 0
	// BY CONSTRUCTION (row 01a182df, measured 2026-10-28: the same ground read
	// "carries no commit to deliver" 3s after its own push accepted). The
	// caller supplies the tip that push landed (Config.DeliveredTip) and this
	// gate MEASURES whether the base reaches it now; a reachable tip is a
	// delivery, not an empty ground, and it gets its own kind and its own words
	// (Rule 15). EVERY other outcome — no tip supplied, an unreadable object, a
	// base that cannot answer — falls through to the empty kind: an unanswered
	// second reading is an ABSTENTION, never a claim.
	if delivered := strings.TrimSpace(cfg.DeliveredTip); delivered != "" {
		if reached, ok := deliveryReachedFromBase(ctx, dir, delivered, baseTip); ok && reached {
			return &Blocker{Kind: BlockerKindAlreadyDelivered,
				Step: deliveryRevListVerb + " " + deliveryCountFlag + " " + gitHeadRef + " " + deliveryNotFlag + " " + integration,
				Words: fmt.Sprintf(deliveryAlreadyDeliveredFmt, dir, deliveryAlreadyDeliveredMarker, delivered,
					integration,
					deliveryRevListVerb+" "+deliveryCountFlag+" "+gitHeadRef+" "+deliveryNotFlag+" "+integration)}, nil
		}
	}
	// THE SECOND READING, because `ahead == 0` answers only half the question.
	// "Nothing here to publish" and "nothing to pull" are two different facts, and
	// the pre-fix sentence printed the second one on the strength of the first —
	// row 01a1826e measured the cost (a ground 4 commits BEHIND read "there is
	// nothing to pull"). The clause is chosen by this count, never assumed; an
	// unreadable count ABSTAINS into the wider claim-free half (it says nothing to
	// PUBLISH, which the ahead reading did measure) rather than claiming a pull
	// that nobody counted (Rule 15).
	//
	// ONE READING, TWO CONSUMERS (row 01a19483). This count and the SHAPE test
	// further down are two questions about the same peer, and answering them
	// separately let each come from a different world: the count asked the peer
	// (`ls-remote` + the FETCH_HEAD-only fetch), while the shape compared HEAD
	// against `refs/remotes/origin/<base>` — a ref only a FETCH advances, and
	// nothing on this leg fetches before the gate. On a ground the base had moved
	// past, the stale tip still equalled HEAD, so the "genuinely empty" branch was
	// taken forever, "inherited" was unreachable, and the printed sentence
	// contradicted itself ("carries no commit to deliver … but origin/master
	// carries 2 commit(s) this ground does not have") while the delivery loop
	// re-measured an immutable fact once per round. So the peer is read ONCE here
	// and BOTH questions are answered from it.
	reading := deliveryPeerReadingTip(ctx, dir, deliveryOriginURL(ctx, dir, cfg.DevRepo), base)
	behind, behindOK := deliveryCommitsMissing(ctx, dir, reading)
	moveText := deliveryEmptyNothingToMoveText
	if behindOK && behind > 0 {
		moveText = fmt.Sprintf(deliveryEmptyBehindFmt, fmt.Sprintf(deliveryIntegrationFmt, base), behind,
			gitPullVerb+" "+gitPullRebaseFlag+" "+originRemoteName+" "+base)
	}
	// THE THIRD MEANING OF THE SAME ZERO (epic: inherited delivery), decided only
	// after the first two are exhausted — so it can never swallow either:
	//
	//	· the DeliveredTip arm above owns "this run's own push put it there";
	//	· a SIBLING directory carrying this row's commits owns the sibling hint —
	//	  a measurement of WHERE the work is, strictly more actionable than any
	//	  statement about this directory;
	//	· an unreadable HEAD (git could not name it) owns nothing and ABSTAINS.
	//
	// What is left is ONE discriminator, and it is measured, not assumed:
	// `ahead == 0` already means HEAD is REACHABLE from the base (the base
	// carries everything HEAD has). So `reached` alone says nothing new. The
	// question that is left is WHICH reachable shape it is:
	//
	//	① HEAD *IS* the base tip — a ground cut from the base that produced
	//	   nothing. That is genuinely EMPTY, it keeps the claim-free clause, and
	//	   the fix must not touch it.
	//	② HEAD is a STRICT ancestor of the base tip — the ground's content was
	//	   landed by someone else and the base has moved on since. This is the
	//	   INHERITED delivery: there is nothing to push (measured: 0) because the
	//	   content this run was handed is already on the integration branch. The
	//	   behind count, when the peer reports one, is REPEATED as a fact about
	//	   other people's work — never as a recovery this run can perform
	//	   (deliveryInheritedBehindFmt), because a pull cannot make content that
	//	   is already on the base something this run still has to deliver.
	//
	// WHICH TIP DECIDES, and it is the peer's — the same reading as the count.
	// A shape taken from the stale remote-tracking ref is the defect; a shape
	// taken from a peer that did not answer is not a measurement at all, so an
	// unmeasured peer ABSTAINS into the empty kind with the claim-free clause
	// (Rule 15: never assert what was not measured).
	//
	// The 33 交付没有内容可发布 rows and the 49 交付没有落地 rows the epic cites are
	// shape ②: classified EMPTY, so the delivery loop did not break, and every
	// round it spawned re-measured an immutable fact and registered a new row.
	// A round cannot change "your content is already on the base", so ② breaks
	// the loop exactly as BlockerKindAlreadyDelivered does.
	sibling := deliveryEmptySibling(ctx, cfg, dir, base, baseTip)
	if sibling == "" && reading.tip != "" {
		head, _ := deliveryRefTip(ctx, dir, gitHeadRef)
		if reached, ok := deliveryReachedFromBase(ctx, dir, gitHeadRef, reading.tip); ok && reached && head != "" && head != reading.tip {
			command := deliveryRevListVerb + " " + deliveryCountFlag + " " + gitHeadRef + " " + deliveryNotFlag + " " + integration
			inheritedText := fmt.Sprintf(deliveryInheritedDeliveryFmt, dir, command, integration,
				deliveryShortSha(head), integration)
			if behindOK && behind > 0 {
				inheritedText = fmt.Sprintf(deliveryInheritedBehindFmt, dir, command, integration,
					deliveryShortSha(head), integration, fmt.Sprintf(deliveryIntegrationFmt, base), behind)
			}
			return &Blocker{Kind: BlockerKindInheritedDelivery,
				Step:  deliveryRevListVerb + " " + deliveryCountFlag + " " + gitHeadRef + " " + deliveryNotFlag + " " + integration,
				Words: inheritedText}, nil
		}
		// ① OR THIS CLONE'S OWN PUSH — the FOURTH meaning of the same zero, and
		// the half of shape ① that no caller can supply (epic: deliveryEmptyGate
		// judges a SUCCESSFUL push as an empty ground). The DeliveredTip arm at
		// the top of this function owns "a tip the caller SAW its push land", and
		// it is deliberately optional ("no tip supplied → an ABSTENTION, never a
		// claim"), so on the delivery loop's ordinary shape it answers nothing —
		// and shape ① then holds BOTH of these at once, because a push that landed
		// and a ground that produced nothing both read `ahead == 0` and
		// `head == reading.tip`:
		//
		//	this run pushed, and the base now stands on our tip  → a DELIVERY
		//	a ground cut from the base that produced nothing     → EMPTY, untouched
		//
		// The discriminator is a signature GIT writes and nothing in this file
		// can manufacture: the ground's reflog for the remote-tracking base, newest
		// entry first, carries git's own push subject and names exactly this HEAD
		// (measured on this tree, 2026-11-02: a push writes `update by push`, a
		// fetch writes `fetch -q <remote>: fast-forward`). A push also refuses to
		// move a remote-tracking ref onto a commit this clone does not hold, so a
		// backdated or fabricated entry can only name a commit the ground already
		// had — it cannot buy the claim it was forged for.
		//
		// It is the same FACT as the delivered arm and therefore the same KIND and
		// the same words: `empty` does not break the delivery loop, and every round
		// it spawns re-measures an immutable fact. An unreadable reflog is an
		// ABSTENTION into the empty kind with the claim-free clause, never a claim
		// (Rule 15).
		if head != "" && head == reading.tip && deliveryOwnPushLanded(ctx, dir, base, head) {
			integrationText := fmt.Sprintf(deliveryIntegrationFmt, base)
			return &Blocker{Kind: BlockerKindAlreadyDelivered,
				Step: deliveryRevListVerb + " " + deliveryCountFlag + " " + gitHeadRef + " " + deliveryNotFlag + " " + integration,
				Words: fmt.Sprintf(deliveryAlreadyDeliveredFmt, dir, deliveryAlreadyDeliveredMarker, head,
					integrationText,
					deliveryRevListVerb+" "+deliveryCountFlag+" "+gitHeadRef+" "+deliveryNotFlag+" "+integration)}, nil
		}
		// ① or an unmeasured reading: the empty kind's own sentence stands.
	}
	words := fmt.Sprintf(deliveryEmptyRefusalFmt, dir,
		deliveryRevListVerb+" "+deliveryCountFlag+" "+gitHeadRef+" "+deliveryNotFlag+" "+integration,
		integration, moveText)
	if sibling != "" {
		words += sibling
	}
	return &Blocker{Kind: BlockerKindEmpty,
		Step:  deliveryRevListVerb + " " + deliveryCountFlag + " " + gitHeadRef + " " + deliveryNotFlag + " " + integration,
		Words: words}, nil
}

// deliveryEmptySibling is the answer to the operator's immediate next question
// — "0 commits HERE, so where did the run commit?" — and EVERY WORD OF IT IS A
// MEASUREMENT this function took, in the candidate directory itself.
//
// THE TWO THINGS IT REFUSED TO MEASURE (defect 01a18221, measured 2026-10-27).
// It used to count a candidate's HEAD against the CANDIDATE'S OWN `origin/<base>`
// — a ref a scratch clone never moves — and call any non-zero count "the commits
// this run made may be THERE". Two failures followed:
//
//	① THE COUNT WAS AGAINST A LOCAL OPINION OF THE BASE, not the base: measured
//	  on this host, `origin/master` in a single-branch scratch clone still reads
//	  20c15007 while the real base is 26008d87, so eleven commits the base had
//	  landed hours earlier counted as "ahead".
//	② NOTHING WAS EVER MEASURED AGAINST THE REAL BASE TIP, so a directory that
//	  already carried nothing outstanding — the base's own commit, or a clone of
//	  it — was offered to the operator as work in flight.
//
// So a candidate must now (a) carry THIS ROW's address ref, (b) have a tip that
// was measured NOT to be already history the base landed, and, when that ref is
// ahead, (c) have its commit count taken against the REAL base tip — the one the
// caller resolved in ITS OWN directory. The shape is then stated, never implied:
// a ref pointing at the directory's own HEAD is a ground standing on the row's
// address; a ref pointing elsewhere was left by a previous attempt and the
// checkout has moved since (a scratch clone — exactly what "clone a given
// branch" produces).
//
// An unmeasured question keeps the candidate OUT: this function may abstain, it
// may never guess.
func deliveryEmptySibling(ctx context.Context, cfg Config, dir, base, baseTip string) string {
	root := strings.TrimSpace(cfg.Root)
	branch := strings.TrimSpace(cfg.GroundBranch)
	if root == "" || branch == "" {
		// Without the address ref there is nothing that would make a sibling
		// candidate THIS run's work, so this file invents no second question.
		return ""
	}
	self := deliveryDirIdentity(dir)
	lead := ""
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	ref := refsHeadsPrefix + branch
	unmeasured := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cand := filepath.Join(root, e.Name())
		if deliveryDirIdentity(cand) == self {
			continue
		}
		if !isGitWorkTree(cand) {
			continue
		}
		tip, _ := deliveryRefTip(ctx, cand, ref)
		if tip == "" {
			continue // this directory does not carry the run's address ref at all
		}
		// THE COUNT MUST BE TAKEN ON THE REAL BASE TIP, so the candidate has to
		// be able to READ that commit. A clone taken before the base moved
		// cannot, and there is no measurement it could offer instead: fetching
		// here is not this leg's to do (it writes refs), and counting against
		// the candidate's OWN frozen origin/<base> is exactly defect ① —
		// eleven landed commits reported as "ahead". So the candidate is
		// COUNTED AS UNMEASURED and the sentence says so (Rule 15), instead of
		// being offered on a number nobody measured.
		if !deliveryCarriesCommit(ctx, cand, baseTip) {
			unmeasured++
			continue
		}
		reached, ok := deliveryReachedFromBase(ctx, cand, tip, baseTip)
		if !ok {
			unmeasured++
			continue
		}
		if reached {
			// Already history the base landed: no work of this row is in flight
			// there, so it is not offered as a lead.
			continue
		}
		n, nerr := deliveryOutstanding(ctx, cand, tip, baseTip)
		if nerr != nil {
			unmeasured++
			continue
		}
		candTip, _ := deliveryRefTip(ctx, cand, gitHeadRef)
		shape := deliverySiblingShape(candTip, tip)
		if shape == "" {
			unmeasured++
			continue // the shape nobody measured is not a shape to speak
		}
		lead = fmt.Sprintf(deliveryEmptySiblingFmt, cand, branch, n, shape) +
			fmt.Sprintf(deliverySiblingRecoveryFmt, deliveryFetchVerb, cand, ref)
		break
	}
	if unmeasured > 0 {
		lead += fmt.Sprintf(deliveryEmptySiblingUnmeasuredFmt, unmeasured, root, ref, baseTip)
	}
	return lead
}
