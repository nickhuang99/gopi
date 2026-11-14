package tools

// image_peer.go — "which image is THAT process running?" — the question
// `gopi worker --status` owes a dispatch-time reader (epic image-selfcheck
// broadcast half, row 01a18301).
//
// WHY THIS EXISTS SEPARATELY FROM image_selfcheck.go. That file answers the
// SELF question ("am I stale?") and only the self question: a resident that
// inspected its peers would need a roster and a policy for strangers, so it
// reads /proc/self/exe and nothing else. But the operator's question at
// DISPATCH time is a PEER question — "the worker that will take my row is
// pid N; is IT still the artifact on disk?" — and until now it could only be
// answered by hand, with `go version -m /proc/<pid>/exe` plus a
// `git merge-base --is-ancestor`:
//
//	$ readlink /proc/1038465/exe
//	/home/user/work/gopi/bin/gopi (deleted)
//	$ go version -m /proc/1038465/exe | head -3
//	... mod  v0.0.0-20261028000000-84beb3091e
//
// — the worker was running a revision two merged delivery fixes short of the
// disk, so an ALREADY-DELIVERED row was printed as "delivery did not land",
// and the sentence that said so was produced by code nobody realised was old.
//
// SO THIS FILE IS A READER, NOT A DECIDER. It reports facts about one pid: the
// path the kernel says it is executing (verbatim, marker included), whether
// that path was unlinked out from under it, the revision that file carries, and
// the revision the artifact at the same path carries NOW. It never decides what
// the caller should do — the resident face's own policy (report vs exit)
// belongs to that face, which is why the two resident faces differ by design
// (cmd/agent/image_selfcheck.go).
//
// THE TWO ARMS, AND THE ONE WHICH IS HONESTLY BLIND HERE (Rule 15):
//
//	⓪ REBUILT, SAME REVISION — arm ①'s marker fired AND the artifact at that path
//	   carries the revision the process is already running. This is a `make
//	   build` of unchanged (or dirty) work: the file was rewritten, nothing on
//	   disk is missing here, and a restart gains nothing. It gets its OWN word,
//	   its own machine token and its own sentence, because printing arm ①'s
//	   sentence for it asserts a stranded fix that does not exist.
//
//	① REPLACED — /proc/<pid>/exe resolves to a target carrying the kernel's own
//	   procDeletedSuffix. That is a write-and-rename install, i.e. every `make
//	   build`: proof, not inference. Reading the link's target still yields the
//	   UNLINKED inode's bytes, i.e. the revision the process is really running —
//	   and the disk path yields the new one, so both revisions are nameable.
//	② STAMP — the two revisions disagree. For a PEER this arm is deliberately
//	   NOT claimed as a detector of a rebuild written IN PLACE: the magic link
//	   resolves the very file on disk, so an in-place rewrite is invisible to
//	   any reader outside the process (only the SELF check can see it, because
//	   it reads its own in-memory build info). The comparison is still made —
//	   it fires when the link and the disk path resolve to different files —
//	   and the blind half is stated in the row rather than implied away.
//
// An UNSTAMPED artifact leaves the stamp side unmeasurable, and that is
// REPORTED rather than read as "fresh": a check that answered "fine" because it
// could not compare is the exact silent failure this epic is about.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// peerImageUnmeasurablePrefix starts the clause that says WHICH state a row
	// is in when the probe could not answer at all. It is a named spelling
	// because the row, its test and any grep of a captured report read the same
	// token (Rule 1b).
	peerImageUnmeasurablePrefix = "image not measurable — "

	// peerImageReplacedWord / peerImageOlderWord are the two STALE verdicts, and
	// they are DISTINCT words on purpose: "replaced" is the kernel's unlink
	// marker (arm ①), "older-than" is the stamp disagreement (arm ②). One word
	// for both would hide which arm fired.
	peerImageReplacedWord = "image replaced under the process —"
	peerImageOlderWord    = "image older than the artifact —"

	// peerImageRebuiltSameRevisionWord is the THIRD stale word, and it exists
	// because the first two were printing the same sentence for a state that is
	// not staleness of the CODE: a `make build` rewrites the artifact at the
	// same path, so the kernel marks every running process's image " (deleted)"
	// even when the rebuilt bytes carry the SAME vcs.revision. The marker is
	// real (and is still reported); the inference — "the fix on disk does not
	// reach this process" — is not, so it gets its own word.
	peerImageRebuiltSameRevisionWord = "image rebuilt under the process (same revision) —"

	// peerImageSameRevisionClause names that state ONCE (Rule 13): the verdict,
	// the row clause and the worker note all render this same spelling, and a
	// reader/grep of a captured report sees one sentence rather than three
	// paraphrases.
	peerImageSameRevisionClause = "both sides carry the same revision, so the file was rewritten (a build) with unchanged code and a restart gains nothing from it"

	// peerImageCurrentWord is the clean verdict, said out loud rather than left
	// as an empty clause.
	peerImageCurrentWord = "image current"

	// PeerImageState* are the MACHINE words a consumer switches on (the JSON
	// form renders them; the human form renders the sentences above). They are
	// separate from the prose on purpose: comparing against a sentence is a
	// coupling to wording (Rule 1b), and a reworded alarm must not silently
	// change what a machine consumer sees.
	PeerImageStateCurrent       = "current"
	PeerImageStateReplaced      = "replaced"
	PeerImageStateOlderThanDisk = "older-than-disk"
	// PeerImageStateRebuiltSameRevision is the state the epic asked for: the
	// kernel's unlink marker fired AND the artifact on disk carries the same
	// revision as the running image, so nothing the disk holds is missing from
	// this process. It is DISTINCT from PeerImageStateReplaced on purpose — the
	// two demand opposite operator action (nothing vs restart), so a machine
	// consumer switching on this token must not have to parse prose.
	PeerImageStateRebuiltSameRevision = "rebuilt-same-revision"
	PeerImageStateUnmeasurable        = "not-measurable"
)

// PeerImageCheck is the answer to ONE question about ONE pid: is the process
// still running the image the machine would start from that path?
//
// Every field is read from the kernel or from an artifact on disk. Nothing is
// inferred from an mtime, a version string's shape or a process name.
type PeerImageCheck struct {
	// Probed is false when procfs (or the pid's own link) would not answer, and
	// then Unmeasurable carries the reason. A check that could not run must
	// never be rendered as a clean bill of health.
	Probed bool
	// Unmeasurable names why the check could not run ("" when Probed).
	Unmeasurable string
	// ExePath is the resolved /proc/<pid>/exe target, VERBATIM — it carries the
	// kernel's " (deleted)" marker when the file was replaced.
	ExePath string
	// Replaced is the kernel's marker, decoded.
	Replaced bool
	// DiskPath is ExePath without the kernel's marker: the path the machine
	// would start from, and where the fresh artifact is expected.
	DiskPath string
	// ImageRevision is the vcs.revision stamp of the file the pid is executing
	// (read through the magic link, so a replaced image reports the OLD bytes),
	// and DiskRevision the stamp of the artifact at that path now. "" = none.
	ImageRevision string
	DiskRevision  string
	// DiskStamped reports whether the artifact at DiskPath could be read as a
	// Go binary carrying build info at all.
	DiskStamped bool
	// Stale is the verdict and Reason is the sentence naming WHY; both are set
	// together, so a reader never sees a verdict without its evidence.
	Stale  bool
	Reason string
}

// PeerImageProbe is the production entry point: the real procfs, the given pid.
func PeerImageProbe(pid int) PeerImageCheck {
	return PeerImageProbeAt(procRoot, pid)
}

// PeerImageProbeAt answers the peer question for pid against an arbitrary
// procfs root — the seam every test needs (a fabricated <root>/<pid>/exe link
// is the only way to reach the replaced arm without replacing a real binary).
//
// AN EMPTY ROOT IS REPORTED, NEVER DEFAULTED: the whole check is a kernel fact,
// and a defaulted root would silently read someone else's /proc.
func PeerImageProbeAt(root string, pid int) PeerImageCheck {
	if strings.TrimSpace(root) == "" {
		return PeerImageCheck{Unmeasurable: "no procfs root to read (the image probe is a kernel fact and has no fallback)"}
	}
	if pid <= 0 {
		return PeerImageCheck{Unmeasurable: fmt.Sprintf("pid %d is not a process to speak for", pid)}
	}
	link := filepath.Join(root, strconv.Itoa(pid), procname.ExeName)
	target, err := os.Readlink(link)
	if err != nil {
		return PeerImageCheck{Unmeasurable: fmt.Sprintf("pid %d: %v (%s)", pid, err, link)}
	}
	c := PeerImageCheck{
		Probed:   true,
		ExePath:  target,
		Replaced: strings.HasSuffix(target, procDeletedSuffix),
		DiskPath: strings.TrimSuffix(target, procDeletedSuffix),
	}
	if c.DiskPath == "" {
		return PeerImageCheck{Unmeasurable: fmt.Sprintf("pid %d: /proc/%s/%s resolved to nothing, so the path it was started from is not knowable", pid, strconv.Itoa(pid), procname.ExeName)}
	}
	// THE RUNNING IMAGE IS READ THROUGH THE PROC LINK, never through ExePath:
	// a replaced image's target no longer exists by name (that is what the
	// kernel's marker says), while the magic link still reads the live inode.
	c.ImageRevision, _ = artifactRevision(link)
	c.DiskRevision, c.DiskStamped = artifactRevision(c.DiskPath)
	c.Stale, c.Reason = peerImageVerdict(pid, c.ExePath, c.ImageRevision, c.DiskRevision, c.DiskStamped)
	return c
}

// imageSameRevisionStamp is the ONE predicate behind the "rebuilt but unchanged
// revision" state (Rule 13: one meaning, one spelling — the peer verdict and the
// self check both ask THIS function, so the two faces cannot drift apart on what
// counts as "the same code on both sides").
//
// It requires BOTH sides stamped: an unstamped artifact leaves the comparison
// unmeasurable, and an unmeasurable comparison must not be read as "same" — that
// is the silent failure this package exists to end (Rule 15).
func imageSameRevisionStamp(imageRev, diskRev string, diskStamped bool) bool {
	return imageRev != "" && diskStamped && diskRev != "" && imageRev == diskRev
}

// peerImageVerdict is the WHOLE decision, as a pure function of strings, so
// every scenario can be driven by injected revisions instead of by a live
// process (the epic's own requirement: no test reads an unrelated running
// process, and the two revisions and their slots are asserted directly).
//
// THE EXACT INCIDENT IS ITS FIRST CASE: pid 1038465 running 84beb309 against a
// disk carrying ee873ec6, with the kernel reporting "bin/gopi (deleted)".
func peerImageVerdict(pid int, target, imageRev, diskRev string, diskStamped bool) (stale bool, reason string) {
	replaced := strings.HasSuffix(target, procDeletedSuffix)
	diskPath := strings.TrimSuffix(target, procDeletedSuffix)
	switch {
	// arm ① with a MEASURED, UNCHANGED revision — the third state, and the one
	// this epic exists for. The kernel's marker is real (a build replaced the
	// inode), but the artifact on disk carries the same stamp as the running
	// image, so nothing on disk is missing here and the arm-① sentence's
	// inference ("the code it executes is NOT the code at that path") would be
	// FALSE. It stays STALE — the marker is evidence and is still reported —
	// with a reason that says what actually happened.
	case replaced && imageSameRevisionStamp(imageRev, diskRev, diskStamped):
		return true, fmt.Sprintf("the kernel reports pid %d's image as %q — %s: running and disk are both revision %s, so the disk holds no fix this process is missing", pid, target, peerImageSameRevisionClause, imageRev)
	case replaced:
		// The REAL alarm, and it keeps its full force: the marker fired AND the
		// stamp comparison (when measurable) says the two sides are different
		// code. Both revisions are named, because "stale relative to WHAT?" is
		// the reader's first question.
		return true, fmt.Sprintf("the kernel reports pid %d's image as %q — the file at %s was replaced (or unlinked) while the process kept running, so the code it executes is NOT the code at that path; it runs %s, that path now carries %s", pid, target, diskPath, revisionForReport(imageRev), revisionForReport(diskRev))
	case imageRev != "" && diskStamped && diskRev != "" && imageRev != diskRev:
		return true, fmt.Sprintf("pid %d runs revision %s while %s now carries %s — the two stamps disagree, so the running process is not the artifact on disk", pid, imageRev, diskPath, diskRev)
	}
	return false, ""
}

// RowNote renders the ONE-SENTENCE image clause a `--status` row carries. It
// never returns "" — a row states WHICH state it is in, so a missing clause can
// only mean the row was not rendered at all (Rule 15).
func (c PeerImageCheck) RowNote() string {
	switch {
	case !c.Probed:
		return c.UnmeasurableNote()
	case c.sameRevisionRebuild():
		// The third state: the kernel's marker is quoted (that string IS the
		// evidence) together with the measured equality, so the row cannot be
		// read as "the disk has a fix this process lacks".
		return fmt.Sprintf("%s running %s (%s), artifact at that path %s — %s",
			c.StaleWord(), revisionForReport(c.ImageRevision), c.ExePath,
			revisionForReport(c.DiskRevision), peerImageSameRevisionClause)
	case c.Stale:
		// The REPLACED arm quotes the kernel's own link target, marker and all:
		// that string IS the evidence, and an operator forwarding a row to
		// somebody else must not have to re-run readlink to prove it.
		return fmt.Sprintf("%s running %s, artifact at that path %s",
			c.StaleWord(), c.RunningImageForReport(), revisionForReport(c.DiskRevision))
	default:
		return fmt.Sprintf("%s %s", c.CurrentWord(), revisionForReport(c.ImageRevision))
	}
}

// sameRevisionRebuild asks the ONE predicate (imageSameRevisionStamp) against
// this check's own fields, so every renderer below agrees on which state it is
// in without repeating the condition three times.
func (c PeerImageCheck) sameRevisionRebuild() bool {
	return c.Replaced && imageSameRevisionStamp(c.ImageRevision, c.DiskRevision, c.DiskStamped)
}

// UnmeasurableNote names the arm that could not run, as a sentence rather than
// an empty string: the absent case must be distinguishable from the clean case
// in a captured report.
func (c PeerImageCheck) UnmeasurableNote() string {
	reason := c.Unmeasurable
	if strings.TrimSpace(reason) == "" {
		reason = "no reason recorded"
	}
	return fmt.Sprintf("%s%s", peerImageUnmeasurablePrefix, reason)
}

// StaleWord is the single spelling the stale verdict renders with (Rule 1b: a
// downstream grep and the tests read the SAME token).
func (c PeerImageCheck) StaleWord() string {
	switch {
	case c.sameRevisionRebuild():
		return peerImageRebuiltSameRevisionWord
	case c.Replaced:
		return peerImageReplacedWord
	default:
		return peerImageOlderWord
	}
}

// RunningImageForReport renders the running image as "<revision> (<path>)" for
// the REPLACED verdict — the path verbatim, so the kernel's " (deleted)" marker
// travels with it — and as the bare revision otherwise, where the path adds
// nothing a reader does not already have on the row.
func (c PeerImageCheck) RunningImageForReport() string {
	rev := revisionForReport(c.ImageRevision)
	if c.Replaced {
		return fmt.Sprintf("%s (%s)", rev, c.ExePath)
	}
	return rev
}

// StateWord is the verdict as ONE machine token (PeerImageState* above), which
// is what the JSON form publishes — a consumer switches on this, never on the
// rendered sentence.
func (c PeerImageCheck) StateWord() string {
	switch {
	case !c.Probed:
		return PeerImageStateUnmeasurable
	case c.sameRevisionRebuild():
		return PeerImageStateRebuiltSameRevision
	case c.Replaced:
		return PeerImageStateReplaced
	case c.Stale:
		return PeerImageStateOlderThanDisk
	default:
		return PeerImageStateCurrent
	}
}

// CurrentWord is the single spelling of the clean verdict.
func (c PeerImageCheck) CurrentWord() string { return peerImageCurrentWord }

// PeerImageNote is the one-line, NON-FATAL form the WORKER loop prints at a
// round boundary: that face keeps ticking on purpose (it also carries
// housekeeping a stale image performs correctly, so exiting would turn a stale
// image into a new outage), so its sentence says what is stale and what to do
// about it, and claims no refusal it is not making — unlike the merge loop,
// which stops.
//
// It names BOTH paths and BOTH revisions because the operator's first question
// is "stale relative to WHAT?".
func (c PeerImageCheck) PeerImageNote(prog string, pid int) string {
	// THE THIRD STATE GETS ITS OWN SENTENCE, and this is the sentence the epic
	// is about: the old text said "the fix already on disk does NOT reach this
	// process until it is restarted" for a state in which the disk's fixes are
	// ANCESTORS of the running revision, so a restart gains nothing. Claiming a
	// fix is stranded when it is not is how a real alarm gets deleted.
	if c.sameRevisionRebuild() {
		// The clause is NOT appended again: c.Reason already carries it (it is
		// the verdict's own sentence), and printing it twice reads as two
		// findings rather than one.
		return fmt.Sprintf("%s: WARNING — this process's image was REBUILT under it (pid %d) — %s; running image %s (%s), artifact at that path %s (%s). This loop keeps ticking on purpose: it also carries housekeeping a rebuilt image performs correctly",
			prog, pid, c.Reason,
			revisionForReport(c.ImageRevision), c.ExePath,
			revisionForReport(c.DiskRevision), c.DiskPath)
	}
	return fmt.Sprintf("%s: WARNING — this process is running a STALE image (pid %d) — %s; running image %s (%s), artifact at that path %s (%s); the fix already on disk does NOT reach this process until it is restarted (stop it, then start it again from %s). This loop keeps ticking on purpose: it also carries housekeeping a stale image performs correctly, so exiting would turn a stale image into an outage",
		prog, pid, c.Reason,
		revisionForReport(c.ImageRevision), c.ExePath,
		revisionForReport(c.DiskRevision), c.DiskPath, c.DiskPath)
}
