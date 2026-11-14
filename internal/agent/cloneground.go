package agent

// cloneground.go — THE BARE-PROMPT FACE'S CLONE GROUND.
//
// THE DEFECT THIS CLOSES (measured on peerbox 2026-10-23, row 01a16520): a bare
// prompt (`echo "…" | gopi`, no ledger row supplied by anybody) auto-registers
// its own row and, with a non-empty settings `dev_repo`, STILL took the
// worktree channel — the run created a linked `gopi-<stem>` worktree inside the
// repo's main checkout, because:
//
//	① tools.TaskAutoDeliverableJSON (internal/agent/taskregister.go) stamps the
//	   process's own git work tree as deliverable.repo, and
//	② the clone channel's ONLY production call site is
//	   cmd/agent/cli_tasks.go's taskRunCloneChannelFor, which needs a task id a
//	   `worker`/`task run` invocation CLAIMED. A bare prompt claims nothing: it
//	   mints its row mid-run (logAutoTaskf's start hook), long after the face
//	   has been chosen.
//
// So `dev_repo` was a switch that only one of the two faces honoured. This file
// is the second face, and it reuses the clone channel's machinery rather than
// growing a second one (Rule 35 / the brief's "no second clone implementation"):
//
//	devworkspace.Open              — clone (or reuse) workspace_root/<task-id>
//	tools.TaskRunGroundForClone    — record it as the row's ground, the same
//	                                 stamp every other channel writes
//
// WHAT IT DELIBERATELY DOES NOT DO: run the DELIVERY LOOP (clean → pull →
// build → push → reclaim). That loop belongs to the clone channel's close-out
// half (cmd/agent/devworkspace_channel.go), it is shaped for a face that can
// spawn agent ROUNDS against a Blocker, and a bare-prompt run IS the agent —
// there is no round to hand a blocker to. The workspace therefore STAYS, with
// the run's commits in it and its path named on stderr and in the row, which is
// exactly the state the channel already defines for a delivery that did not
// land ("the workspace is KEPT"). Wiring a delivery leg for this face is its
// own row; inventing a second one here is what the brief forbids.
//
// BACKWARD COMPATIBILITY IS THE CONTRACT, one byte at a time: with dev_repo
// empty, cloneChannelOn reports false and this file returns before touching
// anything — no clone, no chdir, no env, no log line.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// cloneGroundRefusalFmt is the ONE sentence this seam prints when the
	// channel is ON but the row's deliverable cannot be pinned to the declared
	// source repo (Rule 1b: the log line and the returned error are the same
	// text, so an operator reading either reads the same account).
	cloneGroundRefusalFmt = devworkspace.CloneChannelPrefix + "%s — staying in the process's own working tree for this run"
	// cloneGroundStampFmt is the ONE line that names the ground this run took.
	cloneGroundStampFmt = devworkspace.CloneChannelPrefix + "ground %s on branch %s (row %s; the workspace is KEPT — delivery is the channel's close-out leg)"
	// cloneGroundAdoptedFmt is the ONE line for a run that was SPAWNED INTO a
	// ground and therefore adopts it instead of opening its own (see
	// adoptAnnouncedCloneGround).
	cloneGroundAdoptedFmt = devworkspace.CloneChannelPrefix + "adopting the announced ground %s on branch %s (row %s; this run was spawned into it — it works, commits and publishes THERE, so the briefing and the delivery leg name one directory)"
	// cloneGroundAnnouncedNotOursFmt is the ONE line for an announcement that
	// belongs to ANOTHER LIVE ROW (see adoptAnnouncedCloneGround): the run says
	// which directory it was told about, which row actually holds it, and that
	// it is opening its own workspace instead. A refusal that only said "not
	// adopting" would leave an operator unable to tell an inherited environment
	// from a broken runner (Rule 15).
	cloneGroundAnnouncedNotOursFmt = devworkspace.CloneChannelPrefix + "the announced ground %s (branch %s) is NOT adopted by row %s — %s; this run opens its own workspace instead"
	// THE BRANCH-ANNOUNCEMENT LINES (epic ground-branch cross-check). Three
	// distinct facts, so three distinct sentences: the measurement could not be
	// taken, the derivation and the measurement disagree, or there is nothing
	// publishable at all. A single "announcement problem" sentence would make
	// an operator unable to tell a broken ground from a diverged one (Rule 15).
	cloneGroundBranchUnmeasuredFmt  = devworkspace.CloneChannelPrefix + "cannot read the branch the ground %s stands on (%v) — the announcement is DERIVED from the row id, not measured"
	cloneGroundBranchDivergenceFmt  = devworkspace.CloneChannelPrefix + "%s"
	cloneGroundBranchUnpublishedFmt = devworkspace.CloneChannelPrefix + "nothing publishable for %s (the row has no derivable ledger address and the ground named no branch; claimed %q) — %s stays unset and no empty claim is made"
	// cloneGroundOpenFailFmt is the ONE line for a channel that is ON and a
	// workspace that could not be opened. It is NOT a silent fallback: it
	// names the reason and says where the run actually is (Rule 15).
	cloneGroundOpenFailFmt = devworkspace.CloneChannelPrefix + "workspace %s could not be opened (%v) — this run continues in %s, which is the worktree channel"
	// cloneGroundAdoptRebindFmt / …FailFmt are the ADOPT arm's two lines: the
	// row's `deliverable.worktree` now names the ground this run adopted, and —
	// when it could not be moved — WHY, said out loud, because a row whose record
	// disagrees with the directory the run works in is a delivery the anchor gate
	// will refuse later with a sentence about paths and no mention of this cause.
	cloneGroundAdoptRebindFmt     = devworkspace.CloneChannelPrefix + "row %s deliverable.worktree now names the adopted ground %s (branch %s)"
	cloneGroundAdoptRebindFailFmt = devworkspace.CloneChannelPrefix + "row %s deliverable.worktree could NOT be moved to the adopted ground %s: %v — the anchor gate will refuse this run's delivery by name until the record is repaired"
)

// autoTaskDeliverableJSON answers "what deliverable does the row this run
// mints carry?" — the CLONE CHANNEL's answer when the channel is ON, the
// historical one otherwise.
//
// THE OFF ARM IS THE COMPATIBILITY CONTRACT, byte for byte: it is the
// unchanged tools.TaskAutoDeliverableJSON() (the process's own git work tree).
// A channel that is unreadable does NOT fall back to it: the refusal is
// reported and the row keeps the historical value, because the alternative —
// writing a deliverable naming a repository nothing validated — is exactly the
// wrong repo this epic exists to stop naming.
func (a *Agent) autoTaskDeliverableJSON() string {
	repo, on, err := autoTaskCloneRepo()
	if err != nil {
		a.logAutoTaskf(cloneGroundRefusalFmt, err.Error())
		return tools.TaskAutoDeliverableJSON()
	}
	if !on {
		return tools.TaskAutoDeliverableJSON()
	}
	return tools.TaskDeliverableRepoJSONFor(repo)
}

// cloneGround is what the bare-prompt seam resolved and opened: the directory
// the run must stand in, the branch it must commit to, and the source repo the
// row's deliverable names.
type cloneGround struct {
	Workspace string
	Branch    string
	Repo      string
}

// autoTaskCloneRepo answers "which repository does the auto row name when the
// clone channel is ON?" — the declared `dev_repo`, rendered to a LOCAL path by
// the channel's own reader (devworkspace.SourceRepoDisplayPath, Rule 14: one
// spelling of host:path → path, never a second one here).
//
// It returns ("", false, nil) when the channel is OFF, which is the whole
// backward-compatibility contract: every caller then behaves exactly as before.
// An error is a REFUSAL to name a repo — never a guess.
func autoTaskCloneRepo() (string, bool, error) {
	on, _, err := config.CloneChannelConfigured()
	if err != nil {
		return "", false, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	if !on {
		return "", false, nil
	}
	devRepo, _, err := config.DevRepoConfigured()
	if err != nil {
		return "", false, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	display, err := devworkspace.SourceRepoDisplayPath(devRepo)
	if err != nil {
		return "", true, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s=%q names no local checkout path: %w", config.DevRepoSettingsKey, devRepo, err)
	}
	if readable, why := devworkspace.RepoPathReadable(display); !readable {
		return "", true, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s=%q → %q is not usable: %s", config.DevRepoSettingsKey, devRepo, display, why)
	}
	return display, true, nil
}

// openCloneGround opens (or reuses) the row's workspace and resolves it into a
// ground: the clone channel's own two steps, called — not reimplemented.
//
// The build command is NOT resolved here (devworkspace.Open does not need one,
// and the declaration's only reader for the refusal sentence lives in package
// main). That is the delivery leg's question, which this face does not run.
func openCloneGround(taskID string, logf func(string, ...any)) (cloneGround, error) {
	devRepo, _, err := config.DevRepoConfigured()
	if err != nil {
		return cloneGround{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unreadable: %w", config.DevRepoSettingsKey, err)
	}
	root, rootSource, err := config.WorkspaceRootConfigured()
	if err != nil {
		return cloneGround{}, fmt.Errorf(devworkspace.CloneChannelPrefix+"%s unusable: %w", config.WorkspaceRootSettingsKey, err)
	}
	cfg := devworkspace.Config{
		TaskID:  taskID,
		DevRepo: devRepo,
		Root:    root,
		// THE GROUND STANDS ON THE INTEGRATION BRANCH — GroundBranch stays
		// EMPTY (the operator's ruling, 2026-11-05: a clone ground is a
		// workspace of this run's own, so there is nobody to collide with and
		// no private ref to mint). `master` is what the workspace rebases onto
		// and the one branch its delivery pushes; the row is addressed by this
		// workspace's PATH (deliverable.worktree), not by a branch name.
		BaseBranch: devworkspace.DefaultBranch,
		RootSource: rootSource,
	}
	if logf != nil {
		// devworkspace narrates through an io.Writer; the agent's own log is
		// what an operator reads for this session (the same destination
		// logAutoTaskf uses), so the channel's lines land there too.
		cfg.Log = logWriterFunc(logf)
	}
	opened, err := devworkspace.Open(context.Background(), cfg)
	if err != nil {
		return cloneGround{Workspace: opened.Dir}, err
	}
	res, err := tools.TaskRunGroundForClone(taskID, opened.Dir)
	if err != nil {
		return cloneGround{Workspace: opened.Dir}, err
	}
	return cloneGround{Workspace: res.Dir, Branch: res.Branch}, nil
}

// adoptCloneGround is the START-HOOK half: after the row exists, open its
// workspace, move this process into it, and publish the ground announcement.
//
// THE CHDIR IS THE MECHANISM, and it is the same one `gopi task run`'s wrapper
// uses for its own session directory: once the process stands in the
// workspace, every tool that resolves a relative path or shells out
// (`bash`'s cwd default, `read`/`write`/`edit`, the project-context walk that
// loads the clone's own PROJECT.md) is in the clone. The alternative — passing
// a workdir to every tool call — would be a second, per-tool answer to "where
// does this run work", which is exactly the drift the ground mechanism exists
// to prevent.
//
// It is deliberately restricted to the BARE-PROMPT face (a.plainPrompt): the
// two chat faces call RegisterSessionTask too (Rule 37: a chat owns no branch),
// and a task-run CHILD never gets here (the wrapper gate returns first).
func (a *Agent) adoptCloneGround(taskID string) {
	if !a.plainPrompt || strings.TrimSpace(taskID) == "" {
		return
	}
	on, _, err := config.CloneChannelConfigured()
	if err != nil {
		a.logAutoTaskf(cloneGroundRefusalFmt, fmt.Sprintf("%s unreadable: %v", config.DevRepoSettingsKey, err))
		return
	}
	if !on {
		return
	}
	// A GROUND THIS RUN WAS HANDED IS ADOPTED, NOT RE-CREATED (measured
	// 2026-10-27, rows 01a17d6d → 01a17da5). The delivery-retry round is spawned
	// INTO the workspace it is meant to deliver (spawnCloneChannelAgent's
	// cmd.Dir) and announces it through GOPI_RUN_GROUND — but that child is
	// still a `gopi` run reading a briefing on stdin, so it reaches this
	// function. Treating it as a fresh run minted a SECOND workspace
	// (<root>/<new-row-id>), chdir'd there, and published THAT as the ground:
	// the briefing named the directory holding the three undelivered commits,
	// the run worked in an empty one, and the close-out delivery leg publishes
	// the announced ground — so delivery could never land, which is the exact
	// failure the retry round exists to prevent.
	//
	// The predicate is the ANNOUNCEMENT, read through its ONE reader (Rule 2),
	// and the clone discriminator every other face uses
	// (devworkspace.GroundModelOf, Rules 13/14): a linked worktree is not this
	// channel's ground and is left to the worktree path below.
	if a.adoptAnnouncedCloneGround(taskID) {
		return
	}
	original, cwdErr := os.Getwd()
	if cwdErr != nil {
		original = ""
	}
	ground, err := openCloneGround(taskID, a.logAutoTaskf)
	if err != nil {
		a.logAutoTaskf(cloneGroundOpenFailFmt, orDashPath(ground.Workspace), err, orDashPath(original))
		return
	}
	if err := os.Chdir(ground.Workspace); err != nil {
		// The workspace exists (devworkspace.Open succeeded) but this process
		// cannot stand in it. Reported, and the run stays where it was — a
		// ground this process cannot enter is not a ground (Rule 15).
		a.logAutoTaskf(cloneGroundOpenFailFmt, ground.Workspace, fmt.Errorf("chdir: %w", err), orDashPath(original))
		return
	}
	// THE ANNOUNCEMENT, ENV HALF: the same pair the worker's spawn seam exports
	// to its child, exported here to THIS process's own environment — there is
	// no child, the run is this process, and every bash call it makes inherits
	// this environment. One writer, and the reader is
	// tools.TaskGroundAnnounced / the publish-target guard, which then treats
	// this run as a clone ground (Rule 21: the reader the constants were
	// declared for).
	if err := os.Setenv(tools.TaskGroundDirEnv, ground.Workspace); err != nil {
		a.logAutoTaskf(devworkspace.CloneChannelPrefix+"publishing %s failed: %v", tools.TaskGroundDirEnv, err)
	}
	// THE BRANCH HALF GOES THROUGH THE ONE CROSS-CHECK (tools.TaskGroundBranchPublish).
	//
	// The value published here is what the publish-target guard reads as the ONE
	// destination this run may push, so an ANNOUNCEMENT that disagrees with the
	// ground is not cosmetic: a reader handed the wrong name does not fail — the
	// guard falls back to a measured HEAD and a disagreement there is a SKIP, and
	// a skip reads exactly like a pass. So the comparison happens HERE, where the
	// value is published: the ground is asked what it actually stands on, and a
	// disagreement is named in this run's log instead of becoming a silent skip
	// three layers away.
	//
	// Since the operator's ruling of 2026-11-05 the AGREEMENT case is the
	// INTEGRATION branch (a clone ground stands on `master` and pushes it),
	// while a descriptive or foreign ref is still a divergence — published as the
	// row's derived address `task/<id>` and REPORTED, never silently accepted.
	a.publishCloneGroundBranch(taskID, ground.Workspace, ground.Branch)
	a.cloneGroundDir = ground.Workspace
	a.cloneGroundBranch = ground.Branch
	a.logAutoTaskf(cloneGroundStampFmt, ground.Workspace, ground.Branch, taskID)
}

// adoptAnnouncedCloneGround adopts the ground the RUNNER announced, when there
// is one and it is a clone workspace this channel can stand in.
//
// It answers FALSE — and touches nothing — for every run that was not handed a
// ground: an operator's `echo "…" | gopi`, a chat face, a row whose ground is a
// linked worktree. That is the compatibility contract, and it is why this is a
// separate predicate rather than a branch buried in the cloning path.
//
// WHAT IT DOES NOT DO: it does not pull, rebase or clone. The announced
// directory is the one the channel already opened and announced before spawning
// this run; a run that re-fetched into it would be a second writer to a ground
// whose state the channel owns, and the retry round's own briefing carries the
// blocker's words for the case where that state is bad.
func (a *Agent) adoptAnnouncedCloneGround(taskID string) bool {
	announced, branch := tools.TaskGroundAnnounced()
	if announced == "" {
		return false
	}
	// THE ANNOUNCEMENT IS A CLAIM, NOT AN OWNERSHIP DEED — and an INHERITED one
	// is not even a claim (measured 2026-11-01, rows 01a19648-4fbe →
	// 01a196bf-8cbb). GOPI_RUN_GROUND reaches this function through
	// os.Environ(), so a `gopi --prompt` that a RUNNING RUN started from its
	// bash tool inherits its PARENT's announcement verbatim: the child then
	// chdir'd into the parent's workspace, and the stamp that followed wrote the
	// parent's directory and branch onto the child's own row. That row was a
	// read-only question and answer — it never touched a file — and it failed
	// its close-out delivery leg on a dirty tree its neighbour was still
	// editing.
	//
	// SO THE ONE-OWNER RULE IS ASKED HERE, BEFORE ANY CHDIR, through the same
	// predicate the two stamping seams use (tools.TaskGroundOwnedByAnotherLiveRow
	// ← taskCreationGroundBorrowed, Rules 13/14). A ground another LIVE row names
	// is that row's: this run says so out loud and opens its own workspace
	// instead. The refusal is a NON-adoption (return false), which is exactly the
	// compatibility contract below — the caller falls through to the cloning
	// path.
	if why := tools.TaskGroundOwnedByAnotherLiveRow(taskID, tools.TaskDeliverableRepoFor(taskID), announced, branch); why != "" {
		a.logAutoTaskf(cloneGroundAnnouncedNotOursFmt, announced, orDashPath(branch), taskID, why)
		return false
	}
	return a.adoptCloneGroundDir(taskID, announced, branch, cloneGroundAdoptedFmt)
}

// adoptCloneGroundDir is THE ADOPTION BODY, shared by the two readers of "which
// directory is this run's clone ground": the RUNNER's announcement
// (adoptAnnouncedCloneGround) and the process's own cwd on the CONTINUATION
// face (adoptContinuationCloneGround, below).
//
// It exists as one function because the two callers must not be two answers
// (Rules 13/14): everything that makes an adopted ground sound — the clone
// discriminator, the chdir, the row's stamp, the worktree rebind, the branch
// read back FROM the directory rather than from the announcement — is the same
// work in both cases, and a second copy would drift exactly where the delivery
// leg reads it.
func (a *Agent) adoptCloneGroundDir(taskID, announced, branch, adoptedFmt string) bool {
	// THE ONE DISCRIMINATOR (Rules 13/14): `.git` a DIRECTORY is a clone of its
	// own; a FILE is a linked worktree, whose ground this face never takes.
	if devworkspace.GroundModelOf(announced) != devworkspace.GroundModelClone {
		return false
	}
	if err := os.Chdir(announced); err != nil {
		// Reported, and the caller falls through to opening its own workspace —
		// a ground this process cannot enter is not a ground (Rule 15).
		a.logAutoTaskf(cloneGroundOpenFailFmt, announced, fmt.Errorf("chdir: %w", err), orDashPath(announced))
		return false
	}
	// THE STAMP GOES THROUGH THE ONE WRITER the cloning path uses
	// (tools.TaskRunGroundForClone, Rule 14): the adopted ground is the row's
	// ground, so the ledger, the anchor the sweeper reads and the delivery leg
	// all name the same directory. Its branch is read FROM the ground — the
	// announcement's is a spawn-time value and the directory is the fact.
	res, err := tools.TaskRunGroundForClone(taskID, announced)
	if err != nil {
		// A ground that cannot be recorded is not silently kept: the run still
		// works here (the chdir succeeded), and the failure is named so the row
		// does not read as having no ground at all (Rule 15).
		a.logAutoTaskf(cloneGroundRefusalFmt, err.Error())
	}
	// THE ROW'S RECORD FOLLOWS THE ADOPTION (measured 2026-10-29). The shared
	// stamp above never REBINDS an existing `deliverable.worktree` — correctly,
	// because it is a DETECTION seam and the 2026-09-12 hijack is what a rebinding
	// probe costs. But an ADOPTED ground is not a guess: the runner announced it,
	// this process chdir'd into it (measured above), and the run now works,
	// commits and publishes THERE. A row still naming the OTHER directory is a row
	// whose delivery leg will refuse with "deliverable.worktree is A, but this
	// attempt publishes from B" — the refusal is right and the record is stale, so
	// the record is what moves.
	//
	// The writer is its own function (tools.TaskDeliverableWorktreeAdopt) for the
	// same reason: it is the ONE place allowed to replace a recorded ground, and
	// its predicate (a work tree whose HEAD lives in itself) is measured, not
	// assumed. A refusal is REPORTED — the run carries on in the adopted ground,
	// and the anchor gate will refuse its delivery by name rather than publishing
	// from a directory the ledger does not know about.
	if changed, aerr := tools.TaskDeliverableWorktreeAdopt(taskID, announced, res.Branch); aerr != nil {
		a.logAutoTaskf(cloneGroundAdoptRebindFailFmt, taskID, announced, aerr)
	} else if changed {
		a.logAutoTaskf(cloneGroundAdoptRebindFmt, taskID, announced, orDashPath(res.Branch))
	}
	if res.Branch != "" {
		branch = res.Branch
	}
	a.cloneGroundDir = announced
	a.cloneGroundBranch = branch
	a.logAutoTaskf(adoptedFmt, announced, orDashPath(branch), taskID)
	return true
}

// cloneGroundContinuationFmt is the ONE line for a CONTINUATION face
// (`gopi session resume <row>`, and therefore `gopi worker --resume`) that
// adopts the clone it is ALREADY STANDING IN. The sentence names the fact that
// makes the adoption sound — the directory was measured, not assumed — and what
// it buys: the close-out's delivery leg is armed for a run whose only ground
// ever existed is this one.
//
// It is declared HERE, below the adoption body, and not beside its siblings in
// the const block at the top of this file: CONFIG.md cites a line of this file
// (`internal/agent/cloneground.go:162`, the ADDRESS/DESTINATION stamp), and
// GOPI.md Rule 38 makes that anchor's exact line a published fact. A constant
// inserted above it moves it, so every sentence this file grows belongs BELOW
// the anchored line unless the anchor is re-derived in the same commit.
const cloneGroundContinuationFmt = devworkspace.CloneChannelPrefix + "adopting the ground this run already stands in, %s on branch %s (row %s; a CONTINUATION re-enters the ground its first run worked in — it does not open a second one, and the close-out's delivery leg publishes from HERE)"

// adoptContinuationCloneGround is the START-HOOK half of a run that CONTINUES
// an existing row — `gopi session resume <row>`, and therefore every
// `gopi worker --resume <row>`.
//
// THE DEFECT IT CLOSES (measured 2026-10-31, rows 01a190bb @05:57 and
// 01a190b4 @06:02, two independent samples). The continuation face never
// reached the clone channel at all: loop.go's start hook registers a row and
// adopts a ground ONLY `if !resume`, because a resume mints no row. So on a
// resume, `cloneGroundDir` stayed empty, `CloneRunGround()` answered ("", ""),
// and `finishCloneDelivery` returned at its own compatibility arm — the arm
// written for the WORKTREE channel — before reading config, before running
// `devworkspace.AttemptDelivery`, before any log line. The end hook then wrote
// `completed` having never attempted a delivery, and the completion witness
// recorded `witnessless` and changed no status, exactly as its contract says.
// Net effect: commits stayed in the clone and the row read completed, which is
// the state Rule 5/6c forbid.
//
// WHY THE GROUND IS THE CWD AND NOT AN ANNOUNCEMENT. A continuation has no
// runner behind it announcing anything: the resume path itself resolved the
// row's recorded ground and chdir'd into it before the engine started
// (cmd/agent's resumeChdirToWorktree — the ONE reader of
// deliverable.worktree). The directory this process stands in IS that ground,
// measured rather than declared, so the adoption asks the process where it is
// and applies the same discriminator the announcement arm applies. A run that
// stands somewhere that is not a clone of its own (a chat face, a bare prompt,
// a row in the worktree channel) takes the same FALSE answer as before and
// keeps the compatibility arm byte-identical.
func (a *Agent) adoptContinuationCloneGround(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	on, _, err := config.CloneChannelConfigured()
	if err != nil {
		a.logAutoTaskf(cloneGroundRefusalFmt, fmt.Sprintf("%s unreadable: %v", config.DevRepoSettingsKey, err))
		return false
	}
	if !on {
		return false
	}
	// A GROUND THE RUNNER ANNOUNCED STILL WINS (the delivery-retry round is a
	// continuation that IS handed one) — the announcement is the runner's own
	// statement about a directory it opened, so it is read before the cwd is
	// consulted, through the ONE reader every other face uses.
	if a.adoptAnnouncedCloneGround(taskID) {
		return true
	}
	dir, err := os.Getwd()
	if err != nil {
		// Not swallowed: without the reading there is no ground to adopt and no
		// directory to name, and the run must not silently take the
		// compatibility arm as if the channel were off (Rule 15).
		a.logAutoTaskf(cloneGroundOpenFailFmt, "-", fmt.Errorf("getwd: %w", err), "-")
		return false
	}
	return a.adoptCloneGroundDir(taskID, dir, "", cloneGroundContinuationFmt)
}

// publishCloneGroundBranch publishes GOPI_RUN_GROUND_BRANCH for THIS process's
// own ground, and is the ONE place this face crosses the two spellings of the
// branch name.
//
// The MEASUREMENT is the ground's own HEAD (`git -C <workspace> rev-parse
// --abbrev-ref HEAD`) — the branch the run will actually commit on. The
// VERDICT is tools.TaskGroundBranchPublish, which returns the value to publish
// plus the divergence sentence ("" when the derivation and the measurement
// agree). A measurement that cannot be taken is passed through as "" rather
// than as `claimed`: TaskGroundBranchPublish then publishes the DERIVED address
// and says so, which is the honest shape (Rule 15) — the run is told the
// announcement is a derivation, not a reading of where it stands.
//
// Both lines land in this run's log because a ground whose name is not its
// row's is exactly the fact an operator needs when a delivery leg demurs about
// the branch it is publishing.
func (a *Agent) publishCloneGroundBranch(taskID, workspace, claimed string) {
	standing, err := groundHeadBranch(workspace)
	if err != nil {
		// Not swallowed: the announcement still goes out (the derivation is a
		// real answer to "which ref carries this row?"), and the reason the
		// MEASUREMENT is absent is read by the operator rather than guessed.
		a.logAutoTaskf(cloneGroundBranchUnmeasuredFmt, workspace, err)
		standing = ""
	}
	branch, divergence := tools.TaskGroundBranchPublish(taskID, standing)
	if divergence != "" {
		a.logAutoTaskf(cloneGroundBranchDivergenceFmt, divergence)
	}
	if branch == "" {
		// tools.TaskGroundBranchPublish answers "" only for a row with no
		// ledger address AND an unmeasurable ground; publishing that would be
		// an empty claim, so it is refused by name.
		a.logAutoTaskf(cloneGroundBranchUnpublishedFmt, taskID, strings.TrimSpace(claimed), tools.TaskGroundBranchEnv)
		return
	}
	if err := os.Setenv(tools.TaskGroundBranchEnv, branch); err != nil {
		a.logAutoTaskf(devworkspace.CloneChannelPrefix+"publishing %s failed: %v", tools.TaskGroundBranchEnv, err)
	}
}

// groundHeadBranch reads the branch a directory's HEAD names, and reports a
// DETACHED HEAD as the empty branch rather than as an error: a detached ground
// has no branch name to publish, and that is a fact about the ground, not a
// failure of the read.
func groundHeadBranch(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("no ground directory to ask")
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s rev-parse --abbrev-ref HEAD: %w", dir, err)
	}
	b := strings.TrimSpace(string(out))
	if b == tools.TaskGroundDetachedHeadBranch {
		return "", nil
	}
	return b, nil
}

// CloneRunGround reports the clone workspace THIS run took, or ("", "") when it
// ran in the worktree channel. Exported for the CLI's close-out narration: an
// operator must be able to read where the run worked without parsing the log.
func (a *Agent) CloneRunGround() (workspace, branch string) {
	return a.cloneGroundDir, a.cloneGroundBranch
}

// orDashPath renders a path for a log line without printing an empty string as
// if it were a real directory.
func orDashPath(p string) string {
	if strings.TrimSpace(p) == "" {
		return "-"
	}
	return p
}

// logWriterFunc adapts the agent's logf to the io.Writer devworkspace narrates
// through, so the channel's step lines ("cloned X → Y", "workspace ready…")
// reach the SAME stream as this session's own diagnostics (Rule 2: one writer,
// one destination).
func logWriterFunc(logf func(string, ...any)) *lineWriter {
	return &lineWriter{logf: logf}
}

// lineWriter is the minimal io.Writer over a logf; the channel writes one line
// per call, so a partial-line buffer would only add a second state machine.
type lineWriter struct{ logf func(string, ...any) }

func (w *lineWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if line != "" && w.logf != nil {
		w.logf("%s", line)
	}
	return len(p), nil
}
