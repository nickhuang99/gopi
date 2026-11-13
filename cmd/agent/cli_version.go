package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/nick/gopi/internal/tools"
	gopiversion "github.com/nick/gopi/internal/version"
)

// versionUsage is the one-line help for the version subcommand.
const versionUsage = `Usage: gopi version
      Print this binary's version — the internal version constant compiled
      INTO the artifact (internal/version.DefaultVersion) — followed by the
      build metadata Go embeds in the binary itself: the VCS revision it was
      built from, the commit time, the toolchain and the target platform.
      Nothing here is read from the directory you are standing in: gopi runs
      in other people's repositories, so its identity travels with the
      binary, and this command needs no git, no filesystem and no network.
      A build the toolchain could not stamp is reported as such rather than
      guessed at: ` + versionStampSkippedCause + `.
      On a ground the toolchain never stamps — a linked worktree (the shape
      every gopi task runs in) — the build RECIPE can name the revision it
      built instead:
        CGO_ENABLED=0 go build -ldflags "-X ` + gopiversion.RevisionLdflagPath + `=$(git rev-parse HEAD)" -o bin/gopi ./cmd/agent/
      and the report then prints that value with ` + versionRevisionSourceKey + ` on
      the line below it, so a declared revision is never read as an attested
      one. A toolchain stamp always wins: it is attested, this is declared.
      ` + VersionDriftFlag + ` adds the DEPLOYMENT distance, measured on the artifact
      THIS PROCESS IS RUNNING (os.Executable(), not the directory you stand in):
      the revision that binary attests and how far it is behind or ahead of
      refs/heads/master, origin/master and the integration mirror. Read-only:
      a correct source can still leave the artifact the machine runs behind.
      The verdict at the bottom counts only the refs that can carry a commit
      the artifact LACKS: a ref the artifact is AHEAD of (it holds that ref's
      whole history) and any ref in the push-back lane's scratch namespace
      (nothing an operator rebuilds moves the artifact toward it) are measured
      and printed, and are stated NOT to vote.`

// The build-metadata keys, named (Rule 1b) because the producer and the
// readers must never disagree about them: `revision:` is the ONE key the
// post-merge rebuild verifier recovers a revision from
// (gopiversion.RevisionOfDisplay), and `modified:` is the key
// gopiversion.IsDirty reads to reject a binary built from a dirty tree. A
// typo in either is a silent identity loss, not a compile error.
const (
	// versionRevisionKey labels the revision `go build` stamped into the
	// binary. It is deliberately the SAME spelling as
	// gopiversion.ReportRevisionKey (see that constant's doc): a report and a
	// probe answer must be parseable by one reader.
	versionRevisionKey = gopiversion.ReportRevisionKey
	// versionMilestoneKey labels MilestoneDate: the day the VERSION this binary
	// reports was declared. It is a hand-written constant in internal/version,
	// set in the same commit that bumps DefaultVersion.
	//
	// WHY NOT A TIMESTAMP OF THIS BINARY OR OF ITS COMMIT — both were tried and
	// both were wrong for the same reason (operator's ruling, 2026-10-25):
	//
	//   `built:`      — the link instant. It is not in the source and cannot be
	//                   (it exists only after the binary does), so any value
	//                   would have to be injected from OUTSIDE by a shell
	//                   `date` in the build recipe. This report must be produced
	//                   by gopi FROM THE SOURCE with no extra script in the
	//                   path, so that label was removed rather than filled.
	//   `committed:`  — `vcs.time`, the committer date of the binary's
	//                   revision. TRUE, but it dates ONE COMMIT and a milestone
	//                   is not a commit: measured 2026-10-25, v0.3.0 was
	//                   declared while the report still said 2026-10-24,
	//                   because the commit carrying the bump had an older date.
	//                   A reader of a version line is asking when the MILESTONE
	//                   was declared, and only the author can answer that.
	//
	// So the line states the declaration, and it is the author's job to set it
	// when bumping (internal/version/self.go's MilestoneDate says so too).
	versionMilestoneKey = "milestone:"
	// versionGoKey labels the toolchain the binary was compiled with
	// (debug.BuildInfo.GoVersion, falling back to runtime.Version()).
	versionGoKey = "go:"
	// versionPlatformKey labels the target platform the binary was built FOR
	// (GOOS/GOARCH), which is a fact about this artifact and about nothing
	// else on the machine.
	versionPlatformKey = "platform:"
	// versionModifiedKey labels the dirty stamp (vcs.modified=true) and is the
	// producer of gopiversion.MetadataModifiedKey — one spelling, shared with
	// the reader that rejects such a binary.
	versionModifiedKey = gopiversion.MetadataModifiedKey
	// versionModifiedTrue is the value that makes the modified line a dirty
	// claim; any other value (or no line at all) means the build was clean.
	versionModifiedTrue = gopiversion.MetadataModifiedTrue
	// versionStampSkippedCause states WHY the toolchain embedded no VCS facts,
	// as a cause the toolchain actually supports — and it is the ONE wording,
	// shared by the report line and the usage text (Rule 1b: an inline copy
	// drifts, and a help text that promises a different answer than the binary
	// prints is worse than no help).
	//
	// IT DOES NOT NAME A CHECKOUT, and that is the whole point (measured
	// 2026-09-26, Go 1.24.5, `go version -m` in both places). The string this
	// replaced said "built outside a git checkout", which is FALSE on the path
	// every agent's build takes: a build inside a LINKED WORKTREE carries no
	// `vcs.*` setting either (main checkout: 3 of them; `git worktree add`
	// checkout of the very same commit: 0) even though
	// `git rev-parse --is-inside-work-tree` answers `true` there. The cause is
	// cmd/go's, not the checkout's: `vcs.FromDir`/`isVCSRoot`
	// (Go's internal/vcs/vcs.go) recognizes a repository root by a `.git`
	// DIRECTORY, and a linked worktree's `.git` is a FILE pointing at the main
	// clone's gitdir — so stamping is skipped, and it is skipped SILENTLY: with
	// `-buildvcs=true`, where cmd/go would otherwise fail loudly, the worktree
	// build still yields 0 `vcs.*` settings rather than an error.
	//
	// So the message states what IS knowable from the artifact alone — the
	// toolchain stamped none of these build settings, whatever the directory
	// was (`-buildvcs=false` in a plain checkout produces the identical binary
	// shape) — and names the toolchain as the party that would have written
	// them, without guessing a reason it does not record. It never infers a
	// checkout, and it stays a constant: `gopi version` reads the artifact,
	// never git (internal/version/self.go).
	versionStampSkippedCause = "the Go toolchain embedded no VCS build settings for it — a " +
		"linked-worktree build gets none, and so does -buildvcs=false — and this command reads " +
		"only the artifact, so no revision can be recovered here"
	// versionStampSkippedDroppedOperandCause names the SECOND way an artifact
	// reaches the line above, and the one this message used to omit — the
	// linker's own silence. `-X importpath.name=value` is NOT checked against
	// the symbol it names: an operand spelling a variable that does not exist is
	// DROPPED in silence — build exit 0, empty stderr, the variable left at its
	// zero value (measured 2026-10-13, Go 1.24.0: the documented recipe with
	// `github.com/youngminz/gopi` in place of this go.mod's module path produced
	// a binary whose `version` printed exactly this line). So the recipe's own
	// stamp can go missing for a reason the artifact cannot show, and a reader
	// who did use `-ldflags -X` is pointed at the operand — named from the ONE
	// constant (Rule 1b/13/14), never retyped — instead of at the tree.
	versionStampSkippedDroppedOperandCause = ". The recipe's own operand can go missing the same way: the " +
		"linker does not check that `-X importpath.name=value` names a variable that exists, so an " +
		"operand naming none is dropped in silence (build exit 0, nothing on stderr) — if this build " +
		"passed -ldflags -X, the operand has to spell " + gopiversion.RevisionLdflagPath
	// versionNoVCSStamp is the honest answer when the toolchain embedded no
	// VCS facts at all. It states the absence instead of omitting the line, so a
	// reader can tell "this artifact carries no revision" from "this command
	// stopped reporting revisions" (Rule 15: never a silent failure).
	versionNoVCSStamp = "unavailable — this build carries no VCS stamp: " + versionStampSkippedCause +
		versionStampSkippedDroppedOperandCause
	// versionMetadataMissing is what the metadata block says when the binary
	// carries no build info at all — reading the artifact answered nothing, so
	// the command says so rather than printing an empty block.
	versionMetadataMissing = "build metadata: unavailable (this artifact carries no build info)"

	// versionRevisionSourceKey labels the line that answers "and who says so?"
	// for a revision the TOOLCHAIN did not write. It is printed only in that
	// arm, and its value states exactly as much as is knowable: the build recipe
	// declared the value, so this `revision:` is a DECLARATION where a toolchain
	// stamp would be an attestation. Presenting the two as one fact is the
	// silent-identity-loss defect this line exists to prevent (Rule 15) — a
	// reader who needs attestation must go and get a stamped build.
	versionRevisionSourceKey = "revision-source:"
	// versionRevisionSelfDeclared is that line's value, and it names the recipe
	// that produced it (the ONE constant, so help text and report cannot drift).
	versionRevisionSelfDeclared = "self-declared by the build recipe (-ldflags " +
		gopiversion.RevisionLdflagPath + ") — NOT attested by the toolchain, which writes " +
		gopiversion.BuildInfoKeyRevision + " only in a checkout it recognizes (a linked worktree " +
		"gets none)"
)

// isVersionSubcommand handles `gopi version` / `gopi --version` / `gopi -v`.
// Like the other subcommands it runs before config resolution in main() —
// printing a version must never require (or start) an agent session, which is
// exactly the bug this closes: an unknown `gopi version` used to fall through
// to the prompt path and spin up a whole agent run.
func isVersionSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case CliVerbVersion, VersionFlagLong, VersionFlagShort:
		// Consistent with the "every command has -h" standard, even though
		// there is nothing more to explain than the one line below.
		for _, a := range args[1:] {
			if a == HelpFlag || a == HelpFlagLong || a == HelpWord {
				fmt.Println(versionUsage)
				return true
			}
		}
		fmt.Println(versionReport())
		// `--drift`: the SAME verb's second question — not "what is this
		// artifact" but "how far is it from the branch this checkout would build
		// from". It stays on this verb because the subject is the same object
		// (the artifact), and because a measured DEPLOYMENT distance is a
		// report, not a checker: it writes nothing, schedules nothing and
		// refuses nothing (Rule 35 — the thing that rebuilds after a landing
		// already ticks; this only lets an operator SEE the distance).
		for _, a := range args[1:] {
			if a == VersionDriftFlag {
				printDeployDrift()
				return true
			}
		}
		return true
	default:
		return false
	}
}

// deployDriftRepoOf resolves the repository a deployed artifact was built from,
// and STATES how it decided — the second half is not decoration: a report that
// silently measured the wrong repo is exactly the defect this function fixes,
// so a fallback must be visible in the output a reader sees.
func deployDriftRepoOf(self string) (repo, how string) {
	if repo, ok := tools.DeployDriftRepoOf(self); ok {
		return repo, versionDriftRepoFromArtifact
	}
	// The artifact is not in a <repo>/bin/ layout (a container install, a
	// stripped path). Fall back to the current directory — the pre-existing
	// answer — and SAY SO rather than passing it off as the artifact's own repo.
	cwd, err := os.Getwd()
	if err != nil {
		return "", versionDriftRepoUnresolved + ": " + err.Error()
	}
	return cwd, versionDriftRepoFromCwd
}

// printDeployDrift answers `gopi version --drift`: the measured distance between
// the artifact THIS PROCESS IS RUNNING and every ref the repo it was built from
// can resolve. Read-only; it writes nothing (see
// internal/tools/deploy_drift.go for the measured case that motivated it).
//
// THE SUBJECT IS THE RUNNING ARTIFACT, NOT <cwd>/bin/gopi — measured 2026-10-28,
// and the difference is the whole point. /usr/local/bin/gopi is a symlink to
// /home/nick/work/gopi/bin/gopi. Started from a task ground, this command used to
// probe THAT GROUND's bin/gopi and print `refs/heads/master: follows the ref —
// the artifact was built from it`: a true statement about the ground's own fresh
// build, and a false answer to the question the operator asked, since the
// deployed binary was 67 commits behind. A report about the DEPLOYMENT measures
// the deployed file (os.Executable()), and the repo whose refs it compares
// against is the one that file was built in — resolved from the artifact's own
// path, with the cwd as the stated fallback rather than a silent substitution.
//
// A repo that cannot be resolved is STATED, never silently clean (Rule 15): this
// report's whole purpose is to make a stale artifact visible, and an
// unmeasurable one printing nothing would be the same silence the deployment gap
// produced in the first place.
func printDeployDrift() {
	self, err := os.Executable()
	if err != nil {
		fmt.Println(versionDriftUnmeasurablePrefix + ": " + err.Error())
		return
	}
	repo, how := deployDriftRepoOf(self)
	d := tools.DeployDriftAt(repo, self)
	fmt.Printf("%s %s\n", versionDriftRepoKey, d.Repo)
	fmt.Printf("%s %s\n", versionDriftRepoSourceKey, how)
	fmt.Printf("%s %s\n", versionDriftArtifactKey, d.Artifact)
	if d.Revision == "" {
		fmt.Printf("%s %s\n", versionDriftRevisionKey, versionDriftNoRevisionValue)
	} else {
		fmt.Printf("%s %s\n", versionDriftRevisionKey, d.Revision)
	}
	for _, r := range d.Refs {
		fmt.Printf("%s %s\n", r.Ref+versionDriftLabelSep, deployDriftRefLine(r))
	}
	fmt.Println(deployDriftSummary(d))
}

// deployDriftSummary renders the report's ONE conclusion, and it is derived
// from the SAME predicate the per-ref lines are — tools.DeployDriftRef.CountsAsStale
// — so the sentence under the list can never contradict a line inside it.
//
// THAT IS THE DEFECT THIS FUNCTION REPLACES. The summary used to be printed on
// `d.Stale()` where Stale() meant "some ref is not followed", while the per-ref
// line for an AHEAD ref said "this artifact is AHEAD of it, not stale behind
// it", and DeployDriftVerdictAhead's own doc said the same. Measured 2026-10-29
// on host nick-sager: refs/heads/master `follows`, refs/remotes/origin/master
// `1 ahead`, refs/remotes/gitPushcheck/master `426 ahead` — and a printed
// `STALE:` beside three lines that each denied it.
//
// Two things changed, and both are needed:
//
//   - the predicate is now "the ref carries commits the artifact LACKS"
//     (Stale/CountsAsStale), so AHEAD stops voting while DIVERGED and BEHIND
//     keep voting — a genuine `5 commits behind master` still prints STALE;
//   - a SCRATCH-namespace ref (the push-back lane's private mirror) never votes,
//     because no operator action moves it toward the artifact — it freezes and
//     falls further behind on every landing (measured 3 days / 405 → 426
//     commits). It is still MEASURED and PRINTED, and its own line now says it
//     does not vote, so nothing is silently dropped.
//
// A clean verdict is STATED positively rather than left as the absence of a
// warning line: an operator scanning for "is it stale?" must be able to tell
// "measured, nothing behind" from "the command stopped reporting" (Rule 15).
func deployDriftSummary(d tools.DeployDrift) string {
	behind := make([]string, 0, len(d.Refs))
	for _, r := range d.Refs {
		if r.CountsAsStale() {
			behind = append(behind, r.Ref)
		}
	}
	if len(behind) == 0 {
		return versionDriftCleanLine
	}
	return fmt.Sprintf(versionDriftStaleFmt, strings.Join(behind, versionDriftRefJoinSep)) +
		versionDriftRemedyTail
}

// deployDriftRefLine renders ONE measured ref through the verdict the tool layer
// computed — the print site never re-derives "behind == 0 means fine", which is
// exactly the false-clean that shipped (Rule 15; see tools.DeployDriftRef.Ahead).
//
// A SCRATCH ref (one in the push-back lane's private namespace) gets its own
// stated note appended: its distance is real and is printed, but it does not vote
// on the summary, and a reader who saw a number with no such note would take the
// next line to have counted it (Rule 15 — the exclusion is stated where the
// number is met, not left to be inferred).
func deployDriftRefLine(r tools.DeployDriftRef) string {
	line := deployDriftRefVerdictLine(r)
	if r.Scratch {
		line += versionDriftScratchNotVoted
	}
	return line
}

// deployDriftRefVerdictLine is the verdict sentence alone, without the scratch
// note — one renderer per question, so appending the note can never change the
// measurement's own wording.
func deployDriftRefVerdictLine(r tools.DeployDriftRef) string {
	switch r.Verdict() {
	case tools.DeployDriftVerdictFollows:
		return fmt.Sprintf("%s — %s", versionDriftFollowsValue, r.Tip)
	case tools.DeployDriftVerdictBehind:
		return fmt.Sprintf("%d %s (%s %s)", r.Behind, versionDriftBehindKey, versionDriftWouldBuildFromKey, r.Tip)
	case tools.DeployDriftVerdictAhead:
		return fmt.Sprintf("%d %s %s (%s %s)", r.Ahead, versionDriftAheadKey, versionDriftAheadNotStale, versionDriftTipKey, r.Tip)
	case tools.DeployDriftVerdictDiverged:
		return fmt.Sprintf("%d %s / %d %s — %s (%s %s)",
			r.Ahead, versionDriftAheadKey, r.Behind, versionDriftBehindKey,
			versionDriftDivergedValue, versionDriftTipKey, r.Tip)
	default:
		// The verdict IS the reason when the measurement could not be taken: an
		// absent ref and a clean one must never read the same.
		return versionDriftUnmeasuredValue + " — " + r.Verdict()
	}
}

// versionDriftRepoKey ... are the report's labels, spelled once (Rule 1).
const (
	versionDriftRepoKey            = "drift-repo:"
	versionDriftArtifactKey        = "drift-artifact:"
	versionDriftRevisionKey        = "drift-revision:"
	versionDriftBehindKey          = "behind"
	versionDriftAheadKey           = "ahead"
	versionDriftTipKey             = "ref tip"
	versionDriftRepoSourceKey      = "drift-repo-source:"
	versionDriftRepoFromArtifact   = "resolved from the RUNNING artifact's own path (<repo>/bin/gopi) — this is the file the machine executes"
	versionDriftRepoFromCwd        = "the running artifact is not in a <repo>/bin/gopi layout, so the CURRENT DIRECTORY was used — the refs below are that checkout's, not necessarily this artifact's"
	versionDriftRepoUnresolved     = "version --drift: the repo could not be resolved"
	versionDriftLabelSep           = ": "
	versionDriftNoRevisionValue    = "none attested by the artifact"
	versionDriftUnmeasuredValue    = "UNMEASURED"
	versionDriftFollowsValue       = "follows the ref — the artifact was built from it"
	versionDriftWouldBuildFromKey  = "the ref is at"
	versionDriftAheadNotStale      = "commits the ref does not have — this artifact is AHEAD of it, not stale behind it"
	versionDriftDivergedValue      = "DIVERGED: the artifact and the ref share a merge base and then part, so neither distance alone is the answer"
	versionDriftUnmeasurablePrefix = "version --drift: unmeasurable"
	// versionDriftRemedyLine is the report's ONE actionable sentence for a
	// genuinely stale artifact, and it names the doors that already exist rather
	// than a new mechanism: the declared build contract (Rule 12) and the
	// `-ldflags -X` recipe above (so the next build attests a revision even from
	// a linked worktree).
	//
	// It is split in two because the STALE sentence now NAMES THE REFS THAT
	// VOTED (`versionDriftStaleFmt` fills them in) and this tail is the remedy
	// that follows: the operator's next action depends on WHICH ref carried the
	// commits, so a summary that did not say which one spoke would send them to
	// rebuild against a ref that never voted.
	versionDriftRemedyTail = "The remedy is the build the repo already declares (Rule 12: settings \"repos\" → build), " +
		"run in a CLEAN checkout of the ref named above — after making sure that ref itself is current, which this " +
		"report does NOT check (a ref can be behind its own upstream while the artifact follows it exactly)."
	// versionDriftStalePrefix is the summary sentence's leading token, named so
	// the report and the tests read ONE spelling: a test that grepped a re-typed
	// "STALE:" would pass on a report that had renamed the line, and the whole
	// point of the summary is the word an operator greps for.
	versionDriftStalePrefix = "STALE:"
	// versionDriftStaleFmt is the STALE sentence; %s = the voting refs that carry
	// commits the artifact lacks.
	versionDriftStaleFmt = versionDriftStalePrefix + " this machine runs an artifact that is BEHIND %s — that ref " +
		"carries commits this artifact does not. "
	// versionDriftCleanPrefix is the OTHER conclusion's leading token. It is
	// deliberately NOT a negation of versionDriftStalePrefix ("NOT STALE:") —
	// measured 2026-10-29: a clean line spelling the stale token would make
	// `grep STALE:` match on a healthy machine, so the one grep an operator
	// actually runs would stop being an answer.
	versionDriftCleanPrefix = "CURRENT:"
	// versionDriftCleanLine is the OTHER conclusion, and it is printed just as
	// loudly: a report whose clean state was the ABSENCE of a line would be
	// indistinguishable from a report that stopped measuring (Rule 15).
	versionDriftCleanLine = versionDriftCleanPrefix + " no measured ref carries a commit this artifact lacks — " +
		"every ref above is either followed by the artifact or carries nothing it does not already have. A ref " +
		"that answers AHEAD, and any ref in the push-back lane's scratch namespace, cannot make this artifact " +
		"stale: nothing an operator rebuilds moves the artifact toward them."
	// versionDriftRefJoinSep separates the refs named in the STALE sentence.
	versionDriftRefJoinSep = ", "
	// versionDriftScratchNotVoted is appended to a SCRATCH ref's own line, so its
	// exclusion from the summary is STATED where a reader meets the number rather
	// than left to be inferred (a scratch mirror freezes and falls further behind
	// on every landing; nothing an operator does moves the artifact toward it).
	versionDriftScratchNotVoted = " — a push-back lane SCRATCH mirror: measured but NOT counted in the verdict below, " +
		"because no build an operator can run moves this artifact toward it"
)

// versionDriftFlag is `gopi version --drift`.
const VersionDriftFlag = "--drift"

// versionReport is what `gopi version` prints: the binary's own version as
// the TOP LINE, then the build metadata the toolchain embedded in the
// artifact.
//
// The top line is gopiversion.Version — a string compiled into this binary,
// so the command answers identically from a git checkout, from a scratch
// repository with no tags, from /tmp and from a tarball extract. The
// metadata below is read from the RUNNING ARTIFACT (runtime/debug's build
// info: the revision and time `go build` stamped, the toolchain, the target
// platform), which is the same category of fact — properties of the file on
// disk, never of the directory the process was started in. There is no git
// call here, and a failure to read the metadata cannot touch the top line.
func versionReport() string {
	var b strings.Builder
	b.WriteString(gopiversion.Version)
	meta, ok := readBuildInfo()
	if !ok {
		b.WriteString("\n" + versionMetadataMissing)
		return b.String()
	}
	b.WriteString("\n" + versionGoKey + " " + meta.GoVersion)
	b.WriteString("\n" + versionPlatformKey + " " + runtime.GOOS + "/" + runtime.GOARCH)
	if meta.Revision == "" {
		b.WriteString("\n" + versionRevisionKey + " " + versionNoVCSStamp)
		return b.String()
	}
	b.WriteString("\n" + versionRevisionKey + " " + meta.Revision)
	// The provenance line is printed ONLY for the declared arm: where a
	// toolchain stamp exists, the VALUE alone is the stronger statement and a
	// second line would dilute it. Its presence is therefore itself the signal
	// (the same shape the `modified:` line uses, read by gopiversion.IsDirty).
	if meta.RevisionSelfDeclared {
		b.WriteString("\n" + versionRevisionSourceKey + " " + versionRevisionSelfDeclared)
	}
	if gopiversion.MilestoneDate != "" {
		b.WriteString("\n" + versionMilestoneKey + " " + gopiversion.MilestoneDate)
	}
	// Printed only when true: the line's PRESENCE is the dirty claim, so a
	// clean build prints nothing here and a reader (gopiversion.IsDirty) can
	// never mistake an absent line for an absent check.
	if meta.Modified {
		b.WriteString("\n" + versionModifiedKey + " " + versionModifiedTrue)
	}
	return b.String()
}

// buildVersion is the ONE line that states this binary's version — the
// internal constant, shared by every caller that embeds the version in a
// larger report (the worker state render, the worker-stop banner). It stays a
// single line on purpose: a metadata block dropped into those renders would
// break the shape they document, so `gopi version` adds the metadata itself
// (versionReport) and this accessor does not.
func buildVersion() string {
	return gopiversion.Version
}

// binaryBuildInfo is the metadata the Go toolchain embedded in this binary.
// Every field is a fact about the ARTIFACT, read back from the artifact with
// runtime/debug.ReadBuildInfo — never about the checkout the process happens
// to be standing in.
type binaryBuildInfo struct {
	// GoVersion is the toolchain that compiled the binary.
	GoVersion string
	// Revision is the vcs.revision stamp ("" = this build carries none).
	Revision string
	// CommitTime is the vcs.time stamp, the committer date of Revision.
	CommitTime string
	// Modified is the vcs.modified stamp: the tree had uncommitted tracked
	// changes when the binary was built.
	Modified bool
	// RevisionSelfDeclared is true when Revision did NOT come from the toolchain
	// but from the build recipe's `-X` operand (gopiversion.Revision). It is the
	// difference between an attested fact and a declared one, so it travels with
	// the value instead of being re-derived by every reader.
	RevisionSelfDeclared bool
}

// readBuildInfo reads the build metadata out of the running binary. ok=false
// means the artifact carries no build info at all (a stripped or otherwise
// unrecognizable binary) — the caller states that instead of inventing facts.
func readBuildInfo() (binaryBuildInfo, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return binaryBuildInfo{}, false
	}
	info := binaryBuildInfo{GoVersion: bi.GoVersion}
	if info.GoVersion == "" {
		info.GoVersion = runtime.Version()
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case gopiversion.BuildInfoKeyRevision:
			info.Revision = strings.TrimSpace(s.Value)
		case gopiversion.BuildInfoKeyTime:
			info.CommitTime = strings.TrimSpace(s.Value)
		case gopiversion.BuildInfoKeyModified:
			info.Modified = s.Value == gopiversion.BuildInfoValueModifiedTrue
		}
	}
	// No toolchain stamp: fall back to the revision the BUILD RECIPE declared.
	// The switch above reads the toolchain's own wire keys, so an absent
	// BuildInfoKeyRevision is exactly the linked-worktree case (and the
	// `-buildvcs=false` one) — and gopiversion.Revision is the answer the recipe
	// supplied at link time. The provenance flag travels WITH the value, so the
	// report can say which of the two it just printed instead of passing a
	// declaration off as an attestation.
	if info.Revision == "" && gopiversion.Revision != "" {
		info.Revision = gopiversion.Revision
		info.RevisionSelfDeclared = true
	}
	return info, true
}
