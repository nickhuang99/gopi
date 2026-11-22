package tools

// image_selfcheck.go — "is the image I am RUNNING still the image the machine
// would start?" — the self-check a RESIDENT decider owes the rest of the
// system (epic merge-image-selfcheck, 2026-10-13).
//
// THE DEFECT IT CLOSES, measured (2026-10-13, host peer1):
//
//	$ readlink -f /proc/3869823/exe
//	/home/user/work/GoPi/bin/gopi (deleted)      # a replacement happened under it
//	$ strings /proc/3869823/exe | grep -c "devdoc-anchor family may have drifted"
//	0
//	$ strings /home/user/work/GoPi/bin/gopi | grep -c "devdoc-anchor family may have drifted"
//	1
//
// The resident `gopi merge --every 2m` (the process that DECIDES landings) was
// running a binary whose file had been replaced under it, so a gate edge that
// exists in the working tree and in the rebuilt binary did not exist in the
// decider. Nothing said so: the loop kept printing its ordinary per-pass
// reports, and every reader of the source — a grep, a code_search, a review —
// saw the new code and concluded the machine had it. A code_search hit in the
// tree is NOT evidence the running decider has the code.
//
// WHY THE FIX IS HERE AND NOT A RESTART SCRIPT (Rule 35): a cron line, a
// watcher or a `restart-on-rebuild` unit file would be a new persistent
// dependency wrapped around gopi, and it would COVER the problem the same way
// the retired hp-worker-autostart cron line covered dying workers — the machine
// would look healthy while nothing explained why. The decider itself can see
// this state with two facts it already has, so the check belongs in the
// decider: no new artifact, no daemon, no state file.
//
// TWO ARMS, BOTH POSITIVE EVIDENCE, NEVER A GUESS (Rule 15):
//
//	① REPLACED — the kernel's own marker. /proc/self/exe resolving to a target
//	   carrying procDeletedSuffix (" (deleted)") is the kernel SAYING the name
//	   no longer reaches this inode: the path was unlinked or renamed over. It
//	   is the same evidence merger_status.go's orphan-lock probe already relies
//	   on, and it is proof, not inference.
//	② STALE STAMP — both images carry a vcs.revision stamp and the two differ.
//	   This arm catches the rebuild that writes the artifact IN PLACE (same
//	   inode, new bytes), which arm ① cannot see.
//
// An UNSTAMPED image (a linked-worktree build carries no vcs.* settings at all
// — see cli_version.go's versionStampSkippedCause) leaves arm ② unmeasurable,
// and that is REPORTED as such (ArmingNote) rather than silently treated as
// fresh: a check that answered "fine" because it could not compare would be the
// exact silent failure this file exists to end.

import (
	"debug/buildinfo"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/nickhuang99/gopi/internal/procname"
	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

const (
	// selfProcDir is the procfs entry naming THIS process — the only pid whose
	// image this check may speak for. Reading another process's /proc/<pid>/exe
	// answers a different question ("is THEIR image current?") and this check
	// is deliberately about the decider judging itself: a resident that
	// inspected its peers would need a roster, a policy for strangers and a
	// reason to be trusted, and none of that is needed to refuse its own
	// decisions.
	selfProcDir = "self"

	// selfImageStaleExit is the exit status a resident face reports when it
	// refuses to keep deciding with a stale image. It is the ordinary failure
	// code rather than a new value: what changed is WHICH runs set it (the same
	// reasoning mergeVerifyWithdrawnExitStatus gives for its own 1).
	selfImageStaleExit = 1

	// selfImageNoRevision is what the report says about a revision that is not
	// there. Named (Rule 1b) because both the alarm and the arming note print
	// it, and two spellings of "no stamp" would read as two different states.
	selfImageNoRevision = "no VCS stamp"

	// selfImageProcUnreadable names the failure of the ONLY channel that can
	// prove arm ①. It is a named spelling because it is printed by the arming
	// note AND is the reason a caller refuses: two wordings would read as two
	// different states.
	selfImageProcUnreadable = "this process's own /proc/self/exe could not be read"
)

// SelfImageCheck is the answer to ONE question: is this process still running
// the image the machine would start from the path it was started at?
//
// Every field is a fact read from the kernel or from an artifact — no field is
// inferred from a timestamp, an mtime or a version string's shape.
type SelfImageCheck struct {
	// Probed is false when procfs would not answer, and then Unmeasurable
	// carries the reason. A check that could not run must never be rendered as
	// a clean bill of health.
	Probed bool
	// Unmeasurable names why the check could not run ("" when Probed).
	Unmeasurable string
	// SelfPath is the resolved /proc/self/exe target — verbatim, so it carries
	// the kernel's " (deleted)" marker when the file was replaced.
	SelfPath string
	// Replaced is the kernel's marker, decoded: the name no longer reaches this
	// inode.
	Replaced bool
	// DiskPath is the path the machine would start (SelfPath without the
	// kernel's marker). It is where the fresh artifact is expected, and the
	// path the alarm hands the operator.
	DiskPath string
	// SelfRevision / DiskRevision are the vcs.revision stamps of the running
	// image and of the artifact at DiskPath ("" = none).
	SelfRevision string
	DiskRevision string
	// DiskStamped reports whether the artifact at DiskPath could be read as a
	// stamped Go binary at all (arm ②'s precondition).
	DiskStamped bool
	// Stale is the verdict, and Reason is the sentence naming WHY — both are
	// set together, so a reader can never see a refusal without its evidence.
	Stale  bool
	Reason string
}

// ProbeSelfImage runs the check against the real procfs and this process's own
// real path. It is the production entry point.
func ProbeSelfImage() SelfImageCheck {
	return SelfImageProbeAt(procRoot)
}

// SelfImageProbeAt is the same check against an arbitrary procfs root — the
// seam every red proof needs (a fabricated <root>/self/exe link is the only way
// to reach the deleted-target arm without replacing the test runner's own
// binary).
//
// An EMPTY root means "this host has no procfs" and is reported as
// unmeasurable rather than defaulted: the whole check is a kernel fact, and a
// defaulted root would read someone else's /proc.
func SelfImageProbeAt(root string) SelfImageCheck {
	if strings.TrimSpace(root) == "" {
		return SelfImageCheck{Unmeasurable: "no procfs root to read (the image self-check is a kernel fact and has no fallback)"}
	}
	link := filepath.Join(root, selfProcDir, procname.ExeName)
	target, err := os.Readlink(link)
	if err != nil {
		return SelfImageCheck{Unmeasurable: fmt.Sprintf("%s: %v (%s)", selfImageProcUnreadable, err, link)}
	}
	c := SelfImageCheck{
		Probed:       true,
		SelfPath:     target,
		Replaced:     strings.HasSuffix(target, procDeletedSuffix),
		DiskPath:     strings.TrimSuffix(target, procDeletedSuffix),
		SelfRevision: selfImageRevision(),
	}
	if c.DiskPath == "" {
		return SelfImageCheck{Unmeasurable: fmt.Sprintf("%s — it resolved to nothing, so the path this process was started from is not knowable here", selfImageProcUnreadable)}
	}
	c.DiskRevision, c.DiskStamped = artifactRevision(c.DiskPath)
	switch {
	// THE THIRD STATE, shared with the peer renderer through ONE predicate
	// (imageSameRevisionStamp): the file was rewritten under this process but the
	// artifact carries the SAME revision, so the disk holds no fix this process
	// is missing. It stays STALE — the kernel's marker is real evidence and is
	// still reported — with a reason that says what actually happened instead of
	// claiming the disk's fixes never arrived.
	case c.Replaced && imageSameRevisionStamp(c.SelfRevision, c.DiskRevision, c.DiskStamped):
		c.Stale = true
		c.Reason = fmt.Sprintf("the kernel reports this process's image as %q — %s: running and disk are both revision %s, so the disk holds no fix this process is missing",
			target, peerImageSameRevisionClause, c.SelfRevision)
	case c.Replaced:
		c.Stale = true
		c.Reason = fmt.Sprintf("the kernel reports this process's image as %q — the file at %s was replaced (or unlinked) while this process kept running, so the code it is deciding with is NOT the code at that path; it runs %s, that path now carries %s",
			target, c.DiskPath, revisionForReport(c.SelfRevision), revisionForReport(c.DiskRevision))
	case c.SelfRevision != "" && c.DiskStamped && c.DiskRevision != "" && c.SelfRevision != c.DiskRevision:
		c.Stale = true
		c.Reason = fmt.Sprintf("this process runs revision %s while %s now carries %s — the artifact was rebuilt (in place: this process's inode was never unlinked) and the running decider is the older image",
			c.SelfRevision, c.DiskPath, c.DiskRevision)
	}
	return c
}

// selfImageRevision reads the vcs.revision stamp of the RUNNING image.
//
// It is deliberately NOT syncGeneratorVersion (memory_sync.go): that helper
// answers "what should I stamp into a patch header", so it collapses "no stamp"
// into the literal "dev" — a lossy answer for a comparison whose whole point is
// to tell "both sides stamped, and they differ" apart from "one side carries no
// stamp, so nothing can be compared".
func selfImageRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return buildInfoRevision(bi.Settings)
}

// artifactRevision reads a NON-RUNNING artifact's stamp from disk — the sibling
// this process might have been. debug/buildinfo reads the embedded metadata
// without executing anything, so a replaced or half-written artifact costs a
// reply and never a subprocess (and an artifact that is not a Go binary at all
// is reported as unstamped instead of being run to find out).
func artifactRevision(path string) (revision string, stamped bool) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", false
	}
	return buildInfoRevision(bi.Settings), true
}

// buildInfoRevision picks the vcs.revision setting out of a build-info table.
// The key spelling comes from the shared leaf package (Rule 13/14) so the
// producer and this reader cannot drift.
func buildInfoRevision(settings []debug.BuildSetting) string {
	for _, s := range settings {
		if s.Key == gopiversion.BuildInfoKeyRevision {
			return strings.TrimSpace(s.Value)
		}
	}
	return ""
}

// revisionForReport renders a revision for a printed line, naming the absence
// rather than leaving a gap (Rule 15).
func revisionForReport(rev string) string {
	if rev == "" {
		return selfImageNoRevision
	}
	return rev
}

// AlarmLine is the line a resident face prints when this check came back stale:
// what it found, what it will therefore NOT do, and what the operator does
// about it. It names both paths and both revisions, because the operator's
// first question is "stale relative to WHAT?".
func (c SelfImageCheck) AlarmLine(prog string) string {
	return fmt.Sprintf("%s: REFUSING to decide with a stale image (pid %d) — %s; running image %s (%s), artifact at that path %s (%s); restart this process (stop it, then start it again from %s) — the decisions it would make without the newest code are the defect this refusal prevents",
		prog, os.Getpid(), c.Reason,
		revisionForReport(c.SelfRevision), c.SelfPath,
		revisionForReport(c.DiskRevision), c.DiskPath, c.DiskPath)
}

// AsPeerImage lowers this SELF check onto the PEER shape (image_peer.go), so
// the ONE renderer that names both revisions and the remedy serves both
// resident faces instead of there being a second copy of that sentence in this
// package (Rule 13: one meaning, one spelling). The fields line up one-for-one
// — both are answers to "is the file I am running still the file on disk?" —
// and the only difference is WHO the answer is about.
func (c SelfImageCheck) AsPeerImage() PeerImageCheck {
	return PeerImageCheck{
		Probed:        c.Probed,
		Unmeasurable:  c.Unmeasurable,
		ExePath:       c.SelfPath,
		Replaced:      c.Replaced,
		DiskPath:      c.DiskPath,
		ImageRevision: c.SelfRevision,
		DiskRevision:  c.DiskRevision,
		DiskStamped:   c.DiskStamped,
		Stale:         c.Stale,
		Reason:        c.Reason,
	}
}

// ArmingNote is the ONE line a resident face prints when it starts, saying
// which of the two arms this run can actually measure. A run whose second arm
// is unmeasurable (an unstamped image) says so instead of implying a check it
// cannot perform (Rule 15).
func (c SelfImageCheck) ArmingNote(prog string) string {
	switch {
	case !c.Probed:
		return fmt.Sprintf("%s: image self-check NOT ARMED — %s; this process cannot tell whether the code it decides with is the code on disk", prog, c.Unmeasurable)
	case c.SelfRevision == "" || !c.DiskStamped || c.DiskRevision == "":
		return fmt.Sprintf("%s: image self-check armed for REPLACEMENT only — %s (%s) vs %s (%s), so a rebuild written in place (same inode, new bytes) is not detectable here; the path being unlinked or renamed over IS",
			prog, selfImageNoRevision, c.SelfPath, revisionForReport(c.DiskRevision), c.DiskPath)
	default:
		return fmt.Sprintf("%s: image self-check armed — running revision %s at %s, and the artifact at that path carries %s",
			prog, c.SelfRevision, c.SelfPath, c.DiskRevision)
	}
}
