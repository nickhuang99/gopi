// Package removalaudit is gopi's DESTRUCTIVE-OPERATION RECORDER — the one place
// that writes the structured, greppable line every worktree-removal and
// branch-delete site leaves behind BEFORE it mutates anything.
//
// WHY IT EXISTS (epic: two live-worktree-removal incidents, 2026-09-17 and
// 2026-09-18). Two in-progress task rows lost their working directories, and
// neither <gopi dir>/logs/merger.log, logs/worker.log nor
// logs/merger-takeover.log named the removing arm: no arm, no path, no branch,
// no row id, no deciding evidence. An operator holding a destroyed checkout had
// nothing to correlate — the removal was, mechanically, unattributable. Every
// guard in the sibling branch (fix/live-row-session-veto) decides WHETHER a
// removal may happen; this package makes the removals that DO happen readable
// afterwards, so "who removed this, and on what evidence?" is a grep, not an
// investigation.
//
// ONE RECORD PER MUTATION, EMITTED FIRST. The record is appended to the sink
// (below) with the caller's own arm name, the repo, the target (worktree path
// or branch name), the row id when the caller knows one, and the deciding
// evidence when it has any. It is emitted BEFORE the git mutation runs: an
// attempt that git then refuses still leaves the attempt recorded, and a
// process killed mid-removal has already written the line that explains the
// half-removed state (a record written after the mutation explains nothing
// about a crash inside it).
//
// THE SINK. Default <gopi data root>/logs/removal-audit.log — the directory the
// incident post-mortem went to first (logs/merger.log, logs/worker.log). It is
// ONE file, but every line carries the five facts that make a line
// attributable (arm, repo, target, branch, row id), which is exactly what the
// three component logs could not supply. EnvSink overrides the path, which is
// how tests pin a sink of their own; with no gopi data root resolvable the
// record is REFUSED and the refusal is printed to stderr (never silently
// dropped, never written to a guessed path).
//
// THE SINK'S OWN DIRECTORY IS THE SINK'S OWN OBLIGATION (measured 2026-10-15).
// The default sink lives under <gopi data root>/logs/, and the directory is
// created by NOBODY ELSE: every other component log is opened on a path a
// daemon spawner has already made. So a FRESH profile — a sandbox, a new
// machine, a new operator — has no logs/ at all, the append's os.OpenFile
// answered ENOENT, and every record on that profile was lost: one stderr
// refusal per record, and no audit file to read back. The emitter therefore
// creates the directory its DEFAULT sink needs, at append time (MkdirAll: one
// stat when the directory is already there).
//
// AN EXPLICIT EnvSink IS THE CALLER'S OWN CONTRACT, AND ITS DIRECTORY IS PART OF
// IT. The override's parent directory is deliberately NOT created: a caller that
// names a path names where the record goes, and inventing directories on its
// behalf would turn a fixture's typo into an audit trail nobody looks at (the
// same "never written to a guessed path" rule, applied one level up). An
// override whose directory does not exist is therefore refused LOUDLY by the
// open below, and that refusal is the documented, tested behaviour.
// A directory that cannot be created (a file where logs/ should be, a
// permission block, a read-only profile) takes the SAME loud refusal — the
// directory creation is a step of the append, never a separate error path.
//
// NO PUBLIC-DIR FALLBACK, NO HOST-GLOBAL GUESS: the refusal is loud (Rule 15).
// Recording is BEST-EFFORT by contract — a failure to write evidence must never
// fail the removal that evidence exists to explain, and must never panic
// (Rule 19): every emit returns nothing and swallows nothing silently.
//
// THE ARM NAMES ARE CONSTANTS (Rule 1b/13/14): a caller passes one of the Arm
// values below, never a literal. The set is the census of removal sites, and
// the structural gate (internal/standards, Rule 13 pattern) fails when a site
// invokes a worktree-removal or branch-delete git verb with no emit beside it.
package removalaudit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The sink's spelling. LogDirName mirrors the gopi data root's component-log
// directory (logs/merger.log, logs/worker.log): the directory an operator reads
// first after an incident, and therefore where the removal record belongs.
const (
	// EnvSink is the absolute path of the audit file, overriding the default.
	// Rule 21: the reader is sinkPath.
	//
	// The override names a FILE, never a directory: the parent directory it
	// implies is the caller's to provide (see the package comment — the emitter
	// creates the DEFAULT sink's directory and only that one). Pointing it at a
	// directory that does not exist is refused loudly by the open, not repaired.
	EnvSink = "GOPI_REMOVAL_AUDIT_LOG"

	// LogDirName is the component-log directory under the gopi data root.
	LogDirName = "logs"

	// FileName is the removal-audit file inside LogDirName.
	FileName = "removal-audit.log"

	// sinkDirMode is the mode of the sink's own directory when the emitter has
	// to create it (a fresh profile: no logs/ at all). It is the same value the
	// sibling component-log writers use for this very directory (cmd/agent's
	// daemonLogDirMode, internal/signalrec's sessionDirPerm) — the profile's log
	// material is operator-readable by design, which is the whole point of an
	// audit trail.
	sinkDirMode = 0o755

	// Version is the record schema version. It is a field of every line so a
	// future field change is readable in the file itself rather than inferred
	// from the date of the line.
	Version = 1
)

// Kind values: WHICH CLASS of mutation the record describes. A constant, never
// a literal, because a reader greps for it.
const (
	KindWorktreeRemoval = "worktree-removal"
	// KindWorktreeGround is the RUN-GROUND ANCHOR record kind: a run recording
	// the directory it just took, at the moment it took it (epic
	// worktree-reclaim-two-gaps, Gap B). It shares this sink with the removal
	// records on purpose — the one append-only list of what happened to a
	// directory, in file order, so a reader can see the ground taken and the
	// ground released as two entries of one history.
	KindWorktreeGround = "worktree-ground"
	KindBranchDelete   = "branch-delete"
	// KindSandboxRemoval is the SANDBOX RECLAIM record kind: a sandbox
	// directory under the configured sandbox root was DELETED by the sweep.
	//
	// It is a kind of its own rather than a reuse of KindWorktreeRemoval
	// because the two answer different questions about different lifecycles:
	// a worktree is a git ground with a branch and a row behind it, while a
	// sandbox is a private profile directory whose ONLY evidence of ever
	// having existed is this line. Before it existed the sink held 12,305
	// records and not one mentioned a sandbox (measured 2026-11-15,
	// `grep -c sandbox ~/.gopi/logs/removal-audit.log` = 0), so a reclaimed
	// sandbox could not answer "who deleted this, and on what grounds" —
	// the exact question this sink exists to answer.
	KindSandboxRemoval = "sandbox-removal"
)

// Op values: the git operation the arm is about to run. Kept apart from Kind so
// the record can (later) carry an op that is not the class's canonical one
// without the class shifting.
const (
	OpWorktreeRemove = "worktree:remove"
	// OpWorktreeGround is the anchor's op: a directory was TAKEN by a run
	// (never a mutation of git state — the anchor writes one line and no more).
	OpWorktreeGround = "worktree:ground"
	OpBranchDelete   = "branch:delete"
	// OpSandboxRemove is the sandbox sweep's op: a private profile directory
	// under the configured sandbox root was DELETED, bytes and all. Kept a
	// distinct spelling from OpWorktreeRemove because the two remove different
	// KINDS of directory — a git ground versus a sandbox — and a reader who
	// greps the op should not have to disambiguate by kind as well.
	OpSandboxRemove = "sandbox:remove"
)

// Record field names. They are constants so the record's shape has ONE
// definition (Rule 14): a consumer that greps a field name references the
// constant rather than retyping the JSON key.
const (
	FieldVersion  = "version"
	FieldTS       = "ts"
	FieldKind     = "kind"
	FieldArm      = "arm"
	FieldOp       = "op"
	FieldPID      = "pid"
	FieldRepo     = "repo"
	FieldTarget   = "target"
	FieldBranch   = "branch"
	FieldTaskID   = "task_id"
	FieldEvidence = "evidence"
	// FieldSha / FieldDeliveryPoint are carried by a branch-delete record when
	// its caller knows the tip and the point the work was handed over to. They
	// answer the one question the pre-2026-10-11 record could not: "did THIS
	// row's branch reach the server?" — a question an operator had to answer by
	// hand, and answered wrongly at least once (a healthy delivery was filed as
	// a fix that never landed, because the delivery state was in no ledger).
	FieldSha           = "sha"
	FieldDeliveryPoint = "delivery_point"
	// FieldBytes / FieldReason are the sandbox-reclaim record's payload: the
	// measured size of the directory that was removed and the criterion that
	// authorised it, verbatim. They are constants for the same reason as every
	// field above (Rule 14) — a reader that greps for one must not retype it.
	FieldBytes  = "bytes"
	FieldReason = "reason"
)

// Arm names the CALLING ARM — the code path that decided to delete. It is the
// single most valuable field of the record: the 2026-09-17/18 post-mortems had
// the destruction and no arm. One constant per removal site; the site passes
// its own, never a shared "misc" (a record that cannot distinguish the sweeper
// from the finish-cleanup chain is the incident's own blind spot).
//
// The structural gate requires every enumerable removal site to reference one of
// these, and requires every one of these to be referenced by a site.
type Arm string

const (
	// ArmSweepReclaim — the worktree sweep recycling a registration nobody
	// lives in (internal/tools/worktree_sweep.go).
	ArmSweepReclaim Arm = "worktree-sweep-reclaim"

	// ArmSweepDeadRegistrationBranch — the sweep dropping the branch of a DEAD
	// registration after its git bookkeeping was reaped.
	ArmSweepDeadRegistrationBranch Arm = "worktree-sweep-dead-registration-branch"

	// ArmSweepChatGroundBranch — the sweep dropping the branch of an INERT CHAT
	// GROUND (no ledger row owned it, its tree was clean, it was 0 ahead of the
	// integration branch and its session was provably over) after the
	// registration itself was reclaimed. It is a separate arm from the dead
	// registration's on purpose: the two are different decisions over different
	// evidence, and a record that cannot tell them apart cannot be audited.
	ArmSweepChatGroundBranch Arm = "worktree-sweep-chat-ground-branch"

	// ArmRowReclaim — a task row's own reclaim path (internal/tools/task_reclaim.go).
	ArmRowReclaim Arm = "task-row-reclaim"

	// ArmSelfReclaim — the worker's own finished rows being released at the end
	// of its round (internal/tools/task_reclaim_self.go).
	ArmSelfReclaim Arm = "task-self-reclaim"

	// ArmSelfOwnSweep — a registration of this run's own repo that no ledger row
	// owned, freed by the worker's own post-round pass.
	ArmSelfOwnSweep Arm = "task-self-own-sweep"

	// ArmFinishCleanupServer — the delivery chain's worktree step (branch
	// preserved: server / single-node shape).
	ArmFinishCleanupServer Arm = "finish-cleanup-worktree"

	// ArmFinishCleanupForce — the delivery chain's --force removal, the one
	// step that deletes uncommitted work.
	ArmFinishCleanupForce Arm = "finish-cleanup-worktree-force"

	// ArmFinishCleanupBranch — the delivery chain's branch step.
	ArmFinishCleanupBranch Arm = "finish-cleanup-branch"

	// ArmFinishCleanupBranchForce — the delivery chain's force-delete of a ref
	// its own push landed (git had refused the plain -d).
	ArmFinishCleanupBranchForce Arm = "finish-cleanup-branch-force"

	// ArmMergedCleanupWorktree — the merged-cleanup sweep removing a merged
	// branch's checkout.
	ArmMergedCleanupWorktree Arm = "merged-cleanup-worktree"

	// ArmTaskGroundAnchor — the producer's RUN-GROUND ANCHOR: the record a
	// worktree-grounded run writes the moment it takes a directory, so a
	// grounded run is attributable at the moment of grounding rather than
	// reconstructed from /proc later (epic worktree-reclaim-two-gaps, Gap B;
	// devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md).
	ArmTaskGroundAnchor Arm = "task-ground-anchor"

	// ArmTaskGroundAnchorBackfill — the run-ground anchor's BACKFILL leg
	// (internal/tools/run_ground_adopt.go): an anchor written for a ground the
	// axis could not see because the producer that would have recorded it did
	// not exist yet (forward-only, commit 3feaf4a). It is its own arm because
	// its evidence is a different KIND from the producer's: an acquisition
	// records a TAKER (pid, incarnation, session), a backfill records a
	// MEASUREMENT (nobody stands there, the tree is clean, the work is
	// delivered) and stores runGroundAnchorUnrecorded in all three identity
	// parts. A record that could not tell the two kinds apart would let a
	// reader mistake a measurement for a live owner — or, worse, the reverse.
	ArmTaskGroundAnchorBackfill Arm = "task-ground-anchor-backfill"
	// ArmTaskGroundAnchorClone — the SAME anchor axis freeing a CLONE-channel
	// ground (`.git` is a DIRECTORY, internal/devworkspace) rather than a linked
	// worktree. It is its own arm on purpose: the two shapes are destroyed by
	// different verbs (a registration drop vs a whole-directory removal) and by
	// different evidence (the clone leg must also prove the tree clean and the
	// branch delivered, because nothing else will refuse for it) — a record that
	// could not tell them apart could not be audited. Measured 2026-10-26: the
	// shared arm's `git worktree remove` answered exit 128 `is not a working
	// tree` for every one of the 725 collectable clone grounds on this host, so
	// the axis removed nothing while its own plan listed them.
	ArmTaskGroundAnchorClone Arm = "task-ground-anchor-clone"

	// ArmCloneChannelReclaim — THE CLONE CHANNEL'S OWN RECLAIM, the arm that
	// deletes a run's ground directory at the END of a delivery that LANDED
	// (cmd/agent/devworkspace_channel.go, `cloneRunState.finish` →
	// `devworkspace.Reclaim`).
	//
	// WHY IT IS ITS OWN ARM AND NOT ArmTaskGroundAnchorClone. The two destroy the
	// same SHAPE and decide on OPPOSITE evidence: the anchor sweep frees a ground
	// no live process stands in (its evidence is the holder identity), while this
	// one frees a ground whose work the channel has just pushed (its evidence is
	// the accepted push). A record that could not tell them apart could not be
	// audited, which is this package's whole first paragraph.
	//
	// WHY IT WRITES AT ALL (measured 2026-10-29). The channel reclaimed with a
	// bare `os.RemoveAll` and wrote NOTHING, so the ONE artefact that knew the
	// run's tip — the directory's HEAD — disappeared without a record of what it
	// had pointed at, and the delivery proof answered "the tip this run produced
	// cannot be named" for commits that were ancestors of origin/master. This arm
	// emits the removal record WITH that tip (WorktreeRemoveAt), captured while
	// the directory still exists.
	ArmCloneChannelReclaim Arm = "clone-channel-reclaim"

	// ArmMergedCleanupBranch — the merged-cleanup sweep's branch step.
	ArmMergedCleanupBranch Arm = "merged-cleanup-branch"

	// ArmMergedCleanupOrphanBranch — the merged-cleanup sweep's ref-only lane:
	// a merged branch with NO checkout left to cross-check.
	ArmMergedCleanupOrphanBranch Arm = "merged-cleanup-orphan-branch"

	// ArmTaskMergeBranch — the one-shot merge path pruning a spent branch.
	ArmTaskMergeBranch Arm = "task-merge-branch"

	// ArmMergeExecBranch — the merge-executor path pruning a spent branch.
	ArmMergeExecBranch Arm = "merge-exec-branch"

	// (ArmRepoSweepBranch retired 2026-09-19, epic sweep-report-not-act: the
	// repo sweep no longer prunes a spent branch — it REPORTS the branch, its
	// tip, the verdict and the owner evidence, and mutates nothing. A retired
	// arm's constant is deleted with its emit, so the reverse-direction gate
	// keeps refusing a declared-but-unemitted arm.)

	// ArmAdoptionBranchForce — the adoption chain force-deleting a ref whose
	// work it proved already integrated.
	ArmAdoptionBranchForce Arm = "adoption-branch-force"

	// ArmAdoptionBranchClean — the adoption chain deleting a ref with -d after
	// its own merge of that ref was committed.
	ArmAdoptionBranchClean Arm = "adoption-branch-clean"

	// (ArmVerifyGround retired 2026-10-30, slice 2/4 of epic 01a18799: the
	// `verify` verb was retired as dispatch-only, and its private scratch
	// ground (taskVerifyGround in tasks.go) went with it — nothing creates a
	// verifier ground any more, so nothing can reclaim one. A retired arm's
	// constant is deleted with its emit, so the reverse-direction gate keeps
	// refusing a declared-but-unemitted arm.)

	// ArmMergeBuildGateGround — the merge-time BUILD gate dropping the detached
	// scratch checkout it materialised for one build (internal/tools/merge_build_gate.go).
	ArmMergeBuildGateGround Arm = "merge-build-gate-ground"

	// (ArmMergeRunSpentBranch / ArmMergeRunProbeGround retired 2026-11-01, item 3 of
	// epic merge-verb-removal: the one-shot `gopi merge` implementation in
	// cmd/agent was deleted, and those two arms had no other emit — the run that
	// pruned a spent ref and the detached probe worktree it materialised were
	// both its own. A retired arm's constant is deleted with its emit, so the
	// reverse-direction gate keeps refusing a declared-but-unemitted arm.)

	// ArmArchiveReclaimBranch — the DECIDED-EXCLUSION reclaim arm removing the
	// head ref of a branch its archive tag already decided against
	// (internal/tools/archive_ref_reclaim.go, `gopi task archive --reclaim`).
	//
	// WHY IT IS ITS OWN ARM: it is the ONE branch deletion in the tree that does
	// NOT run `git branch -d/-D` — it runs `git update-ref -d`, because git's
	// merged-only check is the wrong question for a branch the merge path will
	// never take (see that file's header). The verb is why no record was written
	// here until 2026-10-12: the structural census in internal/standards
	// classified sites by the `branch` + `-d|-D` verb pair, so a deletion
	// spelled `update-ref -d` was invisible to the gate that owes every removal a
	// record. Measured on this host: 11 refs/heads (16 → 5) disappeared at
	// 09:45–09:46 CST with no `kind=branch-delete` line for any of them, and this
	// arm, driven for real against a fixture repo, removed the ref and wrote
	// nothing. The arm name is the answer to "which path did it?" — without it
	// the record would say only that SOMETHING deleted a ref.
	ArmArchiveReclaimBranch Arm = "archive-reclaim-branch"

	// The SANDBOX RECLAIM arms: which CHANNEL drove the sweep that removed the
	// directory. The sweep itself is one function (SweepSandboxesReport), so
	// the arm is the only thing that distinguishes an operator's explicit
	// `gopi tool scratch-gc sandboxes` from the resident tick that nobody
	// watched — and "who deleted this?" is unanswerable without it.
	ArmSandboxReclaimCLI  Arm = "sandbox-reclaim-cli-sandboxes"
	ArmSandboxReclaimTick Arm = "sandbox-reclaim-tick"
)

// The ref-mutation spelling the SEQUEL arm above performs with, declared once
// (Rule 14: one definition, in a package every consumer imports) because the
// arm's own evidence sentence and the structural census in internal/standards
// both address it by name.
const (
	// RefDeleteVerb is the git subcommand that rewrites a ref directly — the
	// one deletion spelling a `branch -d|-D` census does not see.
	RefDeleteVerb = "update-ref"

	// RefDeleteFlag is RefDeleteVerb's deletion flag.
	RefDeleteFlag = "-d"
)

// EvidenceChannel* names a VETO CHANNEL — one of the independent checks an arm
// consults before it removes, recorded in the evidence field so a post-mortem
// can ask "did the deciding arm even look?". They are constants (Rule 1b)
// because a reader greps for them, and because the sibling branch
// fix/live-row-session-veto names the same three arms; the audit record is what
// turns those names into something an operator can find in a log.
const (
	// EvidenceChannelLiveSession is the cross-session liveness channel: the
	// row's recorded runner still alive, or a live process standing in the
	// row's session directory (sibling arm: worktreeSweepSessionLiveEvidence,
	// shared by the sweep's session arm and the reclaim paths'
	// reclaimLiveSessionRefusal).
	EvidenceChannelLiveSession = "live-session-veto"

	// EvidenceChannelLiveHolder is the holder-scan channel: no live process
	// holds the directory (scratch.LiveHolders / the /proc cwd scan).
	EvidenceChannelLiveHolder = "live-holder-scan"

	// EvidenceChannelLedgerClaim is the ledger channel: no live row claims the
	// branch or the directory (the sibling arm: branchDeleteOrphanRefusal).
	EvidenceChannelLedgerClaim = "ledger-live-claim"
)

// Record is one audit line. Every field is JSON-tagged with the matching
// Field* constant, and the struct's declaration order IS the line's key order
// (encoding/json marshals struct fields in order), so a reader sees the same
// five attributing facts in the same place on every line.
//
// omitempty is on the OPTIONAL fields only (repo, branch, task_id, evidence):
// the attributing core — version, ts, kind, arm, op, pid, target — is ALWAYS on
// the wire, because a record missing one of those is the unattributable case
// this package exists to remove (Rule 15a).
type Record struct {
	Version  int    `json:"version"`
	TS       string `json:"ts"`
	Kind     string `json:"kind"`
	Arm      string `json:"arm"`
	Op       string `json:"op"`
	PID      int    `json:"pid"`
	Repo     string `json:"repo,omitempty"`
	Target   string `json:"target"`
	Branch   string `json:"branch,omitempty"`
	TaskID   string `json:"task_id,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	// Sha is the tip the deleted ref pointed at, and DeliveryPoint the remote
	// the caller had handed the work over to (a push target: `host:/path`, or a
	// local path in a fixture). Both are OPTIONAL because only a caller that
	// KNOWS them may claim them: a worktree record has no tip, and a branch
	// delete whose caller never resolved one (see BranchDelete) must not
	// fabricate a sha — an invented delivery fact is worse than an absent one.
	// A reader asks the pair together: `sha` names the commit, `delivery_point`
	// names where it was kept, so "this row's branch reached the server" is
	// answerable by grep on ONE host.
	Sha           string `json:"sha,omitempty"`
	DeliveryPoint string `json:"delivery_point,omitempty"`
	// The anchor's holder identity (KindWorktreeGround only). Kept as their own
	// fields rather than folded into Evidence so a reader decides liveness from
	// VALUES — a (pid, start-ticks) pair and a session id — and never by parsing
	// a sentence.
	HolderPID   string `json:"holder_pid,omitempty"`
	HolderStart string `json:"holder_start,omitempty"`
	HolderSID   string `json:"holder_sid,omitempty"`
	// Bytes and Reason are what a SANDBOX RECLAIM record (
	// KindSandboxRemoval) carries about the directory it removed: the size
	// measured in the delete loop, and the plan's collect criterion VERBATIM.
	//
	// They are their own fields rather than prose folded into Evidence because
	// a reader must be able to SUM the reclaimed bytes and MATCH the reason
	// against the sweep's own constants by VALUE (Rule 13) — neither is
	// possible on a sentence. Both are OPTIONAL: only a kind that measured a
	// directory has a size, and a record that invented one would report a
	// reclaim that never happened.
	Bytes  int64  `json:"bytes,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// emitDisabled is the RED/GREEN proof hook: with it set, Emit writes nothing,
// so a test can assert that its own record-presence assertion goes RED. Only a
// test sets it (see SetEmitDisabledForTest); production never does.
var emitDisabled bool

// emitDisabledMu guards emitDisabled against a test flipping it while another
// goroutine emits. The hook is single-threaded by contract (installed, driven,
// restored inside one test), so the lock is a correctness backstop, not a
// concurrency design.
var emitDisabledMu sync.RWMutex

// SetEmitDisabledForTest disables (or re-enables) the emit and returns the
// previous value, so a caller can restore it. It exists so the RED half of a
// regression test is DETERMINISTIC — the same assertion, driven through a path
// whose record cannot land, must fail — rather than assumed.
func SetEmitDisabledForTest(disabled bool) bool {
	emitDisabledMu.Lock()
	defer emitDisabledMu.Unlock()
	prev := emitDisabled
	emitDisabled = disabled
	return prev
}

func emitIsDisabled() bool {
	emitDisabledMu.RLock()
	defer emitDisabledMu.RUnlock()
	return emitDisabled
}

// WorktreeRemove records an imminent worktree removal. Call it IMMEDIATELY
// BEFORE the `git worktree remove` runs.
//
//	arm      the calling arm (one of the Arm constants)
//	repo     the repository the removal is run in
//	worktree the path about to be removed
//	branch   the branch the registration was on ("" when detached)
//	taskID   the ledger row, when this arm knows one ("" otherwise)
//	evidence the deciding evidence, when the arm reached its verdict by reading
//	         something beyond the row record ("" when there is none to quote)
func WorktreeRemove(arm Arm, repo, worktree, branch, taskID, evidence string) {
	WorktreeRemoveAt(arm, repo, worktree, branch, taskID, "", evidence)
}

// WorktreeRemoveAt is WorktreeRemove WITH the tip sha the directory's HEAD
// pointed at, captured BEFORE the removal.
//
// WHY THE WORKTREE RECORD NEEDED THIS FIELD (measured 2026-10-29, this sink).
// `sha` was documented as a BRANCH-DELETE fact, and the worktree arm never wrote
// one: 17704 `kind=worktree-removal` records on this host, ZERO carrying a sha,
// while 495 of 5259 branch-delete records do. A CLONE ground is its own
// repository (`devworkspace.Reclaim` → `os.RemoveAll`), so its removal destroys
// the ONLY place its HEAD could be read — and the branch-delete record that
// would have kept the sha belongs to the row's ADDRESS ref, not to the ground.
// The delivery-proof arm therefore answered "the tip this run produced cannot be
// named" for rows whose commits were ancestors of origin/master (01a17de7 /
// 01a17de9): the judgement was made conditional on a DIRECTORY still existing,
// and the directory is exactly what the close-out deletes.
//
// The caller must capture the tip while the directory still exists (only the
// caller can — a moment later the question has no subject), and "" means "the
// caller did not resolve a tip", omitted rather than faked: an invented sha is
// worse than an absent one (the same contract BranchDeleteAt's fields carry).
func WorktreeRemoveAt(arm Arm, repo, worktree, branch, taskID, tip, evidence string) {
	emit(Record{
		Kind:     KindWorktreeRemoval,
		Arm:      string(arm),
		Op:       OpWorktreeRemove,
		Repo:     repo,
		Target:   worktree,
		Branch:   branch,
		TaskID:   taskID,
		Evidence: evidence,
		Sha:      tip,
	})
}

// WorktreeGround records the RUN-GROUND ANCHOR: the directory a run has just
// taken as its own, written AT THE MOMENT IT TAKES IT (epic
// worktree-reclaim-two-gaps, Gap B). This is the producer half of the pair —
// the sweeper reads these records back and never guesses a ground's owner from
// /proc absence.
//
// The identity is the caller's OWN, captured at grounding time, and it is
// composed of three parts precisely because each one covers a different death:
//
//	pid            the grounding process
//	startTicks     the process INCARNATION (the kernel never reuses a
//	               (pid, starttime) pair, so a recycled pid cannot impersonate
//	               the run that took the directory)
//	session        the launch session — and because the kernel KEEPS a session
//	               id while the session leader is dead, a session that answers
//	               `kill(sid, 0)` is live while its (pid, startTicks) pair is
//	               not. That asymmetry is what lets a reader distinguish a
//	               holder that reparented but lives (the pgid/session shape)
//	               from one that is gone.
//
// evidence carries whatever the caller knows that a reader cannot re-derive
// (the model, the spawn channel, a refusal note) — never the liveness claim
// itself, which is the identity fields' job.
func WorktreeGround(arm Arm, repo, worktree, branch, taskID, pid, startTicks, session, evidence string) {
	emit(Record{
		Kind:        KindWorktreeGround,
		Arm:         string(arm),
		Op:          OpWorktreeGround,
		Repo:        repo,
		Target:      worktree,
		Branch:      branch,
		TaskID:      taskID,
		HolderPID:   pid,
		HolderStart: startTicks,
		HolderSID:   session,
		Evidence:    evidence,
	})
}

// SandboxRemove records a sandbox directory the sweep has just DELETED.
//
// Call it IMMEDIATELY AFTER the removal succeeds, never before: a record for a
// directory that survived the sweep would claim a reclaim that did not happen,
// which is the mirror of the gap this record closes. A --dry-run pass writes
// nothing at all, because it deletes nothing.
//
// The record answers the two questions the sink exists for. WHO: `arm` names the
// CHANNEL that drove the sweep (an operator's `gopi tool scratch-gc sandboxes`,
// or the resident tick nobody was watching), plus the emitting pid. WHAT AND ON
// WHAT GROUNDS: `root` is the configured sandbox root, `dir` the removed
// directory, `bytes` its measured size, `reason` the plan's collect criterion
// VERBATIM, and `evidence` the ownership facts it was judged on (marker
// presence, pid, incarnation, liveness outcome, age). `reason` is never
// re-worded here — a summary would let the record disagree with the decision.
func SandboxRemove(arm Arm, root, dir string, bytes int64, reason, evidence string) {
	emit(Record{
		Kind:     KindSandboxRemoval,
		Arm:      string(arm),
		Op:       OpSandboxRemove,
		Repo:     root,
		Target:   dir,
		Bytes:    bytes,
		Reason:   reason,
		Evidence: evidence,
	})
}

// BranchDelete records an imminent branch deletion (any of -d / -D / -r -D).// Call it IMMEDIATELY BEFORE the `git branch` delete runs. See WorktreeRemove
// for the field meanings; branch is the ref about to be deleted and target
// repeats it, so a grep for the branch name finds the record whichever field a
// reader looks at.
//
// IT KNOWS NO TIP AND NO DELIVERY POINT, and it says so by omission: this is the
// shape for a caller that genuinely cannot resolve them (a sweep pruning a
// learned-about ref), and it must never grow a guess. A caller that DOES know
// them — the close-out chain, which captured the tip before its delete and just
// pushed to a named target — calls BranchDeleteAt instead. A caller that does NOT
// hold one but can RESOLVE one before the delete belongs in the second group too:
// `gopi merge`'s three spent-ref prune sites (cmd/agent/merge_run.go, ① ② ③) used
// to call THIS entry point and therefore left every record unresolvable, so the
// sink could not answer "which commit was pruned?" (measured 2026-10-17). They now
// resolve the tip through mergeSpentRefTip and call BranchDeleteAt — a filing that
// says "the merge run records no tip" already has its answer, and the remaining
// no-tip callers are the ones this comment describes.
func BranchDelete(arm Arm, repo, branch, taskID, evidence string) {
	BranchDeleteAt(arm, repo, branch, taskID, "", "", evidence)
}

// BranchDeleteAt is BranchDelete WITH the two facts a reader needs to answer
// "did this branch reach the server without asking anyone?":
//
//	tip             the sha the ref pointed at, captured BEFORE the delete. Only
//	                the caller can capture it — a moment later the question has
//	                no subject — and only the caller knows whether it resolved
//	                at all, so "" means "the caller did not resolve a tip" and
//	                the field is omitted rather than faked.
//	deliveryPoint   where the work was handed over to (`host:/path`, or a local
//	                path when the fixture's "server" is a bare repo). "" when the
//	                caller consulted no delivery point.
//
// WHY BOTH ARE OPTIONAL AND NOT VALIDATED: a missing field is readable as
// "unknown" (the honest state of most callers), while a REQUIRED one would have
// to be filled with something, and every something would be a claim nobody
// measured. The reader's rule is therefore additive: sha + delivery_point
// present ⇒ the delivery is accounted for in the ledger; absent ⇒ go look.
func BranchDeleteAt(arm Arm, repo, branch, taskID, tip, deliveryPoint, evidence string) {
	emit(Record{
		Kind:          KindBranchDelete,
		Arm:           string(arm),
		Op:            OpBranchDelete,
		Repo:          repo,
		Target:        branch,
		Branch:        branch,
		TaskID:        taskID,
		Evidence:      evidence,
		Sha:           tip,
		DeliveryPoint: deliveryPoint,
	})
}

// sinkDirEnsureDisabled is the RED/GREEN proof hook for the SINK'S DIRECTORY:
// with it set, the emitter does NOT create the default sink's parent directory
// — which is exactly the pre-2026-10-15 behaviour a fresh profile lost its
// records to. It exists for the same reason emitDisabled does: the red half of
// the regression test must be DETERMINISTIC (the same assertion, driven through
// a path whose record cannot land, must be observed to fail), not argued from
// the calendar. Only a test sets it.
var sinkDirEnsureDisabled bool

// sinkDirEnsureMu guards the hook; see emitDisabledMu for why the lock is a
// backstop rather than a concurrency design.
var sinkDirEnsureMu sync.RWMutex

// SetSinkDirEnsureDisabledForTest disables (or re-enables) the creation of the
// default sink's parent directory and returns the previous value, so a caller
// can restore it.
func SetSinkDirEnsureDisabledForTest(disabled bool) bool {
	sinkDirEnsureMu.Lock()
	defer sinkDirEnsureMu.Unlock()
	prev := sinkDirEnsureDisabled
	sinkDirEnsureDisabled = disabled
	return prev
}

func sinkDirEnsureIsDisabled() bool {
	sinkDirEnsureMu.RLock()
	defer sinkDirEnsureMu.RUnlock()
	return sinkDirEnsureDisabled
}

// emit stamps the record with its identity and appends it to the sink. It is
// deliberately without a return value: recording is best-effort, and a caller
// must never branch on it (a removal blocked by an unwritable log would trade a
// lost log line for a stale worktree).
func emit(rec Record) {
	if emitIsDisabled() {
		return
	}
	rec.Version = Version
	rec.TS = time.Now().UTC().Format(time.RFC3339Nano)
	rec.PID = os.Getpid()

	line, err := json.Marshal(rec)
	if err != nil {
		// A Record is a flat struct of strings and ints: this cannot happen,
		// and if it ever did the refusal must still be visible (Rule 15).
		refusef("marshal %s record for %s: %v", rec.Kind, rec.Target, err)
		return
	}

	path, why, dir := sinkPath()
	if path == "" {
		refusef("%s record for %s not written: %s", rec.Kind, rec.Target, why)
		return
	}
	// The sink's own directory, created for the DEFAULT sink only (dir == "" for
	// an EnvSink override, whose directory is the caller's contract). A failure
	// here is NOT a separate error path: the open below reports the same
	// condition in the caller's own words, and the record is lost either way.
	if dir != "" && !sinkDirEnsureIsDisabled() {
		if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, sinkDirMode); err != nil {
			refusef("%s record for %s not written: create %s: %v", rec.Kind, rec.Target, dir, err)
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		refusef("%s record for %s not written: open %s: %v", rec.Kind, rec.Target, path, err)
		return
	}
	// One write of one complete line: an interleaved line from a concurrent
	// arm would corrupt the record it was meant to preserve.
	if _, err := f.Write(append(line, '\n')); err != nil {
		refusef("%s record for %s not written: write %s: %v", rec.Kind, rec.Target, path, err)
	}
	if err := f.Close(); err != nil {
		refusef("%s record for %s: close %s: %v", rec.Kind, rec.Target, path, err)
	}
}

// sinkPath resolves where records go, returning ("", why, "") when there is no
// honest path to write to. ONE resolution for the whole package (Rule 2): the
// env override first, then the gopi data root's component-log directory.
//
// The third result is the DIRECTORY the emitter must ensure before appending,
// and it is decided HERE — with the path — because only this function knows
// which of the two branches answered (Rule 2: one place decides). It is the
// default sink's LogDirName for the default branch, and "" for an EnvSink
// override: an explicit path's directory is the caller's own contract, and
// creating it would be guessing at a path the caller named precisely.
func sinkPath() (string, string, string) {
	if override := os.Getenv(EnvSink); override != "" {
		return override, "", ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return "", fmt.Sprintf("no gopi data root (%v) and %s is unset", err, EnvSink), ""
	}
	dir := filepath.Join(gd, LogDirName)
	return filepath.Join(dir, FileName), "", dir
}

// DefaultPath is the sink a record lands in when the env override is unset —
// exported so an operator (and a test) can name the file without re-deriving it
// from config and two constants.
func DefaultPath() (string, error) {
	gd, err := config.GopiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(gd, LogDirName, FileName), nil
}

// SinkPath resolves the audit file the WRITER in this process is addressing —
// the same function `emit` uses (Rule 2: one place reads the env var), so a
// reader and a writer in one process cannot disagree about which file is in
// force. `DefaultPath` deliberately answers the UN-overridden path and is the
// one to use when the question is "where would a record land with no override".
func SinkPath() (string, string) {
	path, why, _ := sinkPath()
	return path, why
}

// BranchDeleteRecords reads the removal-audit log's BRANCH-DELETE records from
// the sink in force, keyed by the branch each record deleted.
//
// WHY THIS READER EXISTS (the ref-reachability invariant, epic 01a13088). The
// log has always been WRITE-ONLY: three arms remove refs, every removal is
// recorded, and nothing reads the record back. So the one artefact that knows a
// branch was deleted by a deliberate arm is consulted by no surface, and a
// terminal ledger row whose branch it names is rendered exactly like a row whose
// work is safely on the integration branch — the (now retired) peer-ref audit answered
// `unresolved` for both, and the operator cannot tell "landed, ref consumed" from
// "ref deleted after a delivery that never happened".
//
// A MISSING FILE IS AN EMPTY HISTORY, NOT AN ERROR. The sink is created on the
// first write; a host that has never removed a branch has no file, and "no
// branch-delete record exists" is the truthful reading of that state. A
// MALFORMED LINE IS AN ERROR (Records' own contract): a sink that silently
// swallowed a line would hide exactly the removal the reader is accounting for.
func BranchDeleteRecords(path string) (map[string][]Record, error) {
	return RecordsByBranch(path, KindBranchDelete)
}

// WorktreeRemovalRecords is the GROUND-DELETION half of the same read: the
// `kind=worktree-removal` records, grouped by the branch the removed directory
// stood on.
//
// WHY IT HAD TO EXIST (measured 2026-10-29). `BranchDeleteRecords` was the ONLY
// reader, and the delivery proof's second witness used it alone — while the arm
// that actually destroys a CLONE ground writes `kind=worktree-removal`. The
// consequence was measurable on this host's sink: 17704 worktree-removal records
// carrying NO sha (the field was documented as a branch-delete fact and the
// worktree entry point never passed one), so a row whose ground had been
// reclaimed had no sha witness at all even though a record of the removal
// existed. `WorktreeRemoveAt` now writes the tip; this is its reader.
func WorktreeRemovalRecords(path string) (map[string][]Record, error) {
	return RecordsByBranch(path, KindWorktreeRemoval)
}

// RecordsByBranch reads the sink once and groups the records of ONE kind by
// their branch. ONE walker for both kinds (Rule 14): a second, hand-rolled scan
// is how the two readers would come to disagree about what "a record naming this
// branch" means. Records with no branch are skipped — they are not addressable
// by the question both callers ask.
func RecordsByBranch(path, kind string) (map[string][]Record, error) {
	if strings.TrimSpace(kind) == "" {
		return nil, fmt.Errorf("no record kind to read: an empty kind would match every removal in the sink")
	}
	if strings.TrimSpace(path) == "" {
		resolved, why := SinkPath()
		if resolved == "" {
			return nil, fmt.Errorf("no removal-audit sink to read: %s", why)
		}
		path = resolved
	}
	recs, err := Records(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]Record{}, nil
		}
		return nil, err
	}
	out := map[string][]Record{}
	for _, rec := range recs {
		if rec.Kind != kind {
			continue
		}
		if strings.TrimSpace(rec.Branch) == "" {
			continue
		}
		out[rec.Branch] = append(out[rec.Branch], rec)
	}
	return out, nil
}

// refusef reports a refused record on stderr. It never fails the caller: see
// emit's contract. The prefix is stable so an operator can grep the refusals
// out of a worker's or the merger's log.
func refusef(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "removal-audit: "+format+"\n", args...)
}

// Records reads back every record in the audit file at path, in file order.
// A malformed line is REPORTED as an error rather than skipped: a sink that
// silently swallows a line would hide exactly the removal an operator is trying
// to account for.
func Records(path string) ([]Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Record
	for i, line := range splitLines(raw) {
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return out, fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// splitLines splits raw into non-empty-suffix lines without pulling in
// bufio.Scanner's line-length cap (a record is short, but the sink is
// append-only and shared, and a cap would silently truncate the very line an
// operator needs).
func splitLines(raw []byte) []string {
	var out []string
	start := 0
	for i, b := range raw {
		if b == '\n' {
			out = append(out, string(raw[start:i]))
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, string(raw[start:]))
	}
	return out
}
