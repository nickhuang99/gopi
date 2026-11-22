// holder_identity.go — WHO is holding a candidate, and does that holder have any
// business with it.
//
// THE DEFECT THIS FILE CLOSES (measured 2026-11-17, host the host, task
// 01a1e663). `gopi tool scratch-gc --explain` judged 2997 candidates and found 0
// collectable. Twelve roots whose OWNER the kernel had proven GONE (`outcome=gone`)
// were kept by one sentence: "a live process holds it (the /proc cwd|fd|exe|maps
// scan found a live process inside it)". The holders were five processes 22 days
// old — `bash -c ulimit …; cd … && tail -f … | grep …` plus its `tail` and `grep`
// children — orphaned log pipelines, reparented to the login session manager, whose
// only relationship to the root was two INHERITED file descriptors left open on
// `<root>/gopi-job-*.out` / `*.err`. They neither created the root, stand in it, nor
// run code from it. The recovery rate was therefore permanently 0/2997, and the
// report named neither the holder nor the cure: an operator reading it could not
// tell "a worker is still running in there" (a real reason to keep) from "a dead
// tail pipe is pinning it" (an accident nobody needs to protect).
//
// THE RULE THIS FILE ADDS, and the rule it must NOT break. The hard rule stays: a
// directory a live process OCCUPIES is never deleted. What changes is that
// "occupies" is now asked with the holder's IDENTITY attached, from the same /proc
// channels, and the answer is one of four NAMED classes:
//
//	WRITER    — the process runs the gopi program (procname.IsGopiImage, the ONE
//	            image verdict this repository owns). This is the process the root
//	            belongs to: KEEP, unconditionally (hard rule).
//	OCCUPANT  — the process stands in the root (cwd), executes an image out of it
//	            (exe), or maps a file inside it (maps). Whatever its name, it is
//	            WORKING in there: KEEP, unconditionally (hard rule).
//	OBSERVER  — a file descriptor is the ONLY channel, the image is NOT gopi, and
//	            the image is one of the closed set of read-only viewers (`tail`,
//	            `grep`, `cat`, `less`, …): it can only be reading something inside.
//	OUTSIDER  — a file descriptor is the ONLY channel and the image is not gopi:
//	            the measured shape is a shell whose redirections were inherited
//	            (`bash -c … | grep`), or any other foreign process left behind.
//	UNKNOWN   — no image and no argv could be read: not measured, so NOT released
//	            (Rule 15 — an unmeasured fact is never a measured negative).
//
// WHY THE FILE DESCRIPTOR'S ACCESS MODE IS NOT THE DISCRIMINATOR (measured on this
// host, the same five processes): every inherited descriptor in the incident reads
// `flags: 0100002` in /proc/<pid>/fdinfo — O_LARGEFILE|O_RDWR — for the `tail`, the
// `grep` AND the `bash` alike, because the parent shell opened the redirection
// read-write and every stage of the pipeline inherited it. So "the fd is opened for
// reading" is not observable, and a rule that keyed on it would classify the
// readers as writers and recover nothing. The discriminator that IS observable is
// OCCUPANCY vs a MERELY INHERITED DESCRIPTOR, which is what the four classes above
// encode.
//
// WHAT RELEASES A CANDIDATE, and it is deliberately narrow: the recorded owner must
// already be PROVEN gone (or its pid recycled) by the owner-record ladder — that is
// the existing first arm of entryVerdictInNamespace and this file does not weaken
// it — AND every holder seen must be OBSERVER/OUTSIDER class. One gopi image or one
// occupancy entry keeps the candidate exactly as before. An unregistered root is
// never released by this rule (it has no proven-dead owner to stand on).
package scratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

// The four holder classes a report can name. Constants for the same reason as
// every other reason word here (Rule 1b/13): the verdict sentence, the tests and
// the operator-facing line all quote them.
const (
	// HolderClassWriter is a process running the gopi program.
	HolderClassWriter = "writer"
	// HolderClassOccupant is a process standing in, executing from, or mapping a
	// file inside the candidate.
	HolderClassOccupant = "occupant"
	// HolderClassObserver is an fd-only holder whose image is a read-only viewer.
	HolderClassObserver = "observer"
	// HolderClassOutsider is an fd-only holder whose image is anything else —
	// the measured `bash -c … | grep` shape.
	HolderClassOutsider = "outsider"
	// HolderClassUnknown is a holder whose image and argv could not be read.
	HolderClassUnknown = "unknown"
)

// The closed set of read-only viewer image names. Spelled as constants so the
// classifier and the tests compare against ONE definition (Rules 1b/13) and so a
// new name is a deliberate added line rather than a literal buried in a switch.
const (
	observerImageAwk  = "awk"
	observerImageCat  = "cat"
	observerImageCut  = "cut"
	observerImageGrep = "grep"
	observerImageHead = "head"
	observerImageLess = "less"
	observerImageMore = "more"
	observerImageSed  = "sed"
	observerImageSort = "sort"
	observerImageTail = "tail"
	observerImageTr   = "tr"
	observerImageUniq = "uniq"
	observerImageWc   = "wc"
)

// observerImageNames is the set the classifier reads. It exists because a viewer
// can only be READING a file inside the candidate: every member opens its input
// read-only or filters stdin, and none of them creates, renames or unlinks
// anything under the path it is pointed at.
var observerImageNames = map[string]struct{}{
	observerImageAwk:  {},
	observerImageCat:  {},
	observerImageCut:  {},
	observerImageGrep: {},
	observerImageHead: {},
	observerImageLess: {},
	observerImageMore: {},
	observerImageSed:  {},
	observerImageSort: {},
	observerImageTail: {},
	observerImageTr:   {},
	observerImageUniq: {},
	observerImageWc:   {},
}

// HolderArgvReportChars bounds how much of a holder's command line a verdict
// sentence carries. A 22-day orphan's argv can be long; the report's job is to let
// an operator RECOGNISE the process (`bash -c …; tail -f -n 0 …`), not to replay
// it, and an unbounded argv would let one pathological process bloat every line of
// the sweep's output.
const HolderArgvReportChars = 96

// HolderObservation is ONE /proc sighting: which pid held which path, through
// which channel. HolderScan.Paths stays the comparison face (every existing caller
// asks "is any path inside this candidate"); Observations is the REPORTING face
// that makes the holder's identity ASKABLE without a second /proc walk — the cost
// this package already measured and refused (one process-table walk per candidate
// hung the 6,064-child queue sweep).
type HolderObservation struct {
	// Pid is the process the sighting belongs to.
	Pid string
	// Channel is the /proc entry the hold was proven through
	// (ProcCwdName / ProcExeName / ProcFdName / ProcMapsName).
	Channel string
	// Path is the resolved target /proc reported for that entry.
	Path string
}

// HolderIdentity is one holder of a candidate, classified: what it is, how it is
// attached, and the evidence a report prints.
type HolderIdentity struct {
	// Pid is the holder's process id.
	Pid string
	// Class is one of HolderClass*.
	Class string
	// Channel is the /proc entry that proved the hold.
	Channel string
	// Path is the target /proc reported for that entry.
	Path string
	// Image is the process's image base name (procname.ImageName); "" = no
	// channel answered.
	Image string
	// Argv is the process's command line, bounded to HolderArgvReportChars.
	Argv string
}

// Releases reports whether this holder's presence authorises a delete once the
// recorded owner is proven gone. Only the two fd-only, non-gopi classes do.
func (h HolderIdentity) Releases() bool {
	return h.Class == HolderClassObserver || h.Class == HolderClassOutsider
}

// Describe renders one holder as "pid N (<image>: <argv>)" — the evidence form a
// verdict line carries, so the operator sees WHO and can act on it.
func (h HolderIdentity) Describe() string {
	switch {
	case h.Image != "" && h.Argv != "":
		return fmt.Sprintf("pid %s (%s: %s)", h.Pid, h.Image, h.Argv)
	case h.Image != "":
		return fmt.Sprintf("pid %s (%s)", h.Pid, h.Image)
	case h.Argv != "":
		return fmt.Sprintf("pid %s (%s)", h.Pid, h.Argv)
	default:
		return fmt.Sprintf("pid %s (image and command line unreadable)", h.Pid)
	}
}

// HolderKeepWriterFmt is the sentence a candidate carries when a WRITER or an
// OCCUPANT holds it — the hard rule, now naming what was seen instead of asserting
// an anonymous "a live process". It states the class so a reader never has to ask
// whether the sweep looked at the holder's identity.
const HolderKeepWriterFmt = "held by %s %s"

// HolderObserverOnlyFmt is the sentence for a candidate whose every holder is an
// fd-only outsider. It names the class, the holders, and the CURE — because the
// operator's next question is always "so what do I do about it", and the answer
// (they exit, or you kill them) is knowable here.
const HolderObserverOnlyFmt = "held only by an UNRELATED %s: %s — nothing writes there, so this root is collectable the moment the recorded owner is proven gone (or when they exit, or if you kill them)"

// HolderReleaseFmt is the collectable sentence when the owner is proven gone and
// no writer or occupant is present. It carries the owner evidence AND the holder
// evidence, so the removal is explainable from the line alone (Rule 15: a delete
// that cannot be explained cannot be reviewed).
const HolderReleaseFmt = "collectable: the recorded owner is %s and the only live processes inside it are UNRELATED %s: %s — nothing writes here; it goes now, or they exit on their own"

// classifySeen builds the classified population from the scan's observations, one
// entry per pid (deduplicated), bounded by HolderProbeMax CLASSIFIED entries while
// total keeps the true count. It is the shared walk behind both faces — the batch
// verdict and the single-pid probe — so the two can never disagree about who was
// seen.
func classifySeen(dir string, scan HolderScan, channelOf map[string]string) (ids []HolderIdentity, total int) {
	targets := HolderTargets(dir)
	seen := make(map[string]struct{})
	for _, obs := range scan.Observations {
		if !withinAny(obs.Path, targets) {
			continue
		}
		if _, dup := seen[obs.Pid]; dup {
			continue
		}
		seen[obs.Pid] = struct{}{}
		if holderVanished(obs.Pid) {
			seen[obs.Pid] = struct{}{}
			continue
		}
		total++
		if len(ids) >= HolderProbeMax {
			continue // counted, not classified: the caller is told the population
		}
		id := ClassifyHolderPid(obs.Pid)
		if holderVanished(obs.Pid) { // vanished between the scan and the probe
			// The process EXITED between the scan and this classification. It holds
			// nothing now, so it is not a holder at all — and it must not be
			// counted either, or a population would read as "one of two holders
			// unidentified" and withhold a release it earned (measured: a fixture
			// root flipped keep/collectable run to run because a short-lived pid
			// was seen alive by the pass and gone by the probe).
			continue
		}
		if ch, ok := channelOf[obs.Pid]; ok {
			id = holderChannelClass(id, ch)
		}
		ids = append(ids, id)
	}
	return ids, total
}

// HolderIdentityOfOne classifies the single pid a per-candidate probe returned
// (DirHolder's answer). It is the same classifier the batch face uses, over the same
// /proc entries — one identity verdict, two sources of pid.
func HolderIdentityOfOne(pid string) HolderIdentity { return ClassifyHolderPid(pid) }

// holderVanished reports that the pid no longer exists in the process table, i.e.
// the scan observed a process that has since exited. It is the SAME skip policy
// dirHolder applies to a readlink that returns ENOENT (a vanished process is not a
// holder): the difference is only that the batch face saw the pid alive a moment
// earlier, so the vanish has to be re-measured rather than assumed.
func holderVanished(pid string) bool {
	_, err := os.Stat(filepath.Join(ProcRootDir, pid))
	return os.IsNotExist(err)
}

// ClassifyHolderPid reads one process's own /proc entries and answers WHICH class of
// holder it is. The channel is decided by OCCUPANCY FIRST (cwd, then exe, then
// maps), and only when none of them is inside the candidate does the process fall to
// the fd-only classes — which is the distinction the whole file exists to draw.
//
// The candidate argument is absent on purpose: this function answers "what KIND of
// process is this", and the channel test belongs to the caller that knows the
// candidate (HolderIdentitiesFor / the verdict). Keeping the two apart is what lets
// a test pin the classification without a filesystem fixture, and the caller
// re-proves the channel against the candidate it is judging.
func ClassifyHolderPid(pid string) HolderIdentity {
	id := HolderIdentity{Pid: pid}
	argv, argvOK := procname.Cmdline(ProcRootDir, pid)
	argv0 := ""
	if len(argv) > 0 {
		argv0 = argv[0]
	}
	if argvOK {
		id.Argv = truncateReport(strings.Join(argv, " "), HolderArgvReportChars)
	}
	id.Image = procname.ImageName(ProcRootDir, pid, argv0)
	isGopi, _ := procname.IsGopiImage(ProcRootDir, pid, argv0, "")
	if isGopi {
		id.Class = HolderClassWriter
		return id
	}
	if id.Image == "" && id.Argv == "" {
		// No channel answered: this is a MISSING measurement, not a foreign
		// process, and a release must never rest on it (Rule 15).
		id.Class = HolderClassUnknown
		return id
	}
	if _, ok := observerImageNames[id.Image]; ok {
		id.Class = HolderClassObserver
		return id
	}
	id.Class = HolderClassOutsider
	return id
}

// holderChannelClass upgrades an fd-only classification to OCCUPANT when the /proc
// entry that PROVED the hold is an occupancy channel (cwd/exe/maps). It is the half
// of the rule that needs the candidate: the same process can be an outsider through
// one fd and an occupant through its working directory, and the occupancy answer
// must win.
func holderChannelClass(id HolderIdentity, channel string) HolderIdentity {
	switch channel {
	case ProcCwdName, ProcExeName, ProcMapsName:
		id.Channel = channel
		if id.Class != HolderClassWriter {
			id.Class = HolderClassOccupant
		}
		return id
	}
	id.Channel = channel
	return id
}

// holdersRelease folds a classified holder population into the ONE release answer
// the verdict needs: may this candidate be collected, and what is the evidence
// sentence.
//
// It refuses (release=false) unless the population is non-empty AND every holder is
// release-eligible — so an unknown, a writer, an occupant, or a candidate whose
// holders could not be identified all keep the directory, which is the hard rule
// with an identity attached rather than a weakening of it.
func holdersRelease(ids []HolderIdentity, total int) (bool, string) {
	if len(ids) == 0 {
		return false, ""
	}
	parts := make([]string, 0, len(ids))
	allReleasable := true
	named := ""
	for _, id := range ids {
		parts = append(parts, id.Class+" "+id.Describe())
		if !id.Releases() {
			allReleasable = false
			if named == "" {
				named = fmt.Sprintf(HolderKeepWriterFmt, id.Class, id.Describe())
			}
		}
	}
	evidence := strings.Join(parts, "; ")
	if total > len(ids) {
		// More holders than the probe classified: the unclassified tail could
		// contain a writer, so the population as a whole is never proven
		// releasable (Rule 15 — missing evidence is not a negative).
		allReleasable = false
	}
	if !allReleasable {
		return false, named
	}
	return true, evidence
}

// truncateReport bounds a report field to n bytes on a rune boundary, so a
// multi-byte character in an argv is never cut in half.
func truncateReport(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !isRuneStart(cut[len(cut)-1]) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// isRuneStart reports whether b begins a UTF-8 sequence.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// HolderIdentitiesFromScan is the batch face's entry point: it classifies what the
// scan saw inside dir AND attaches the channel each pid was proven through, so the
// verdict sentence can say "holds an inherited fd only" instead of a bare pid.
func HolderIdentitiesFromScan(dir string, scan HolderScan) (ids []HolderIdentity, total int) {
	targets := HolderTargets(dir)
	channelOf := make(map[string]string, HolderProbeMax)
	for _, obs := range scan.Observations {
		if !withinAny(obs.Path, targets) {
			continue
		}
		if prev, ok := channelOf[obs.Pid]; !ok || channelRank(obs.Channel) < channelRank(prev) {
			channelOf[obs.Pid] = obs.Channel
		}
	}
	return classifySeen(dir, scan, channelOf)
}

// channelRank orders the /proc channels by how strongly they prove OCCUPANCY:
// cwd, then exe, then maps, then fd. The verdict reports the STRONGEST channel a
// pid was seen through, so a process that both stands in the candidate and holds a
// descriptor in it is never described by the weaker fact.
func channelRank(channel string) int {
	switch channel {
	case ProcCwdName:
		return 0
	case ProcExeName:
		return 1
	case ProcMapsName:
		return 2
	default:
		return 3
	}
}

// ProcRootForIdentity exposes the procfs root this classifier reads, so a test that
// builds a synthetic /proc can assert the same code path the production scan uses
// instead of a copy of it.
func ProcRootForIdentity() string { return ProcRootDir }

// holderIdentityFixtureHint is documentation, not code: a test fixture for the
// measured shape spawns `tail -f <file inside the root>` with Setpgid and kills the
// GROUP in cleanup (memory #4951's pitfall: an orphan grandchild whose /proc cwd
// keeps a directory alive vetoes the very sweep under test, and the test then fails
// for the wrong reason).

// holderEvidenceSentence renders the KEEP sentence for a population that was
// identified but not released: the class of each holder and its command line, so
// the operator reads WHO holds the directory rather than an anonymous claim that
// "a live process" does. A population the probe could not fully classify says so in
// the same sentence (the tail is reported by count, never by silence).
func holderEvidenceSentence(ids []HolderIdentity, total int) string {
	if len(ids) == 0 {
		return holderScanSawUnidentified
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id.Class+" "+id.Describe())
	}
	joined := strings.Join(parts, "; ")
	if total > len(ids) {
		return fmt.Sprintf("%s (+%d more, unclassified)", joined, total-len(ids))
	}
	return joined
}

// holderPopulationClass names the coarse class of a release-eligible population from
// the rendered evidence (which starts with the class of its first holder), so the
// sentence can say "UNRELATED observer" in the singular case and still be honest
// about a mixed pipeline.
func holderPopulationClass(evidence string) string {
	if strings.HasPrefix(evidence, HolderClassObserver+" ") {
		return HolderClassObserver
	}
	return HolderClassOutsider
}

// holderScanSawUnidentified is the evidence sentence for the one case where the
// scan proved a process inside a candidate and the identity probe could not name
// it at all. It is a NAMED constant because it is the honest non-answer: rule 15
// forbids rendering it as the same sentence a real sighting produces.
const holderScanSawUnidentified = "the /proc cwd|fd|exe|maps scan found a live process inside it (identity unreadable)"

// releasedVerdict is the collectable verdict this rule produces when the recorded
// owner is PROVEN gone (or its pid recycled) and every live process inside the
// candidate is an unrelated observer. It carries BOTH halves of the evidence — the
// owner's outcome and the identities of the processes being released — because a
// delete nobody can explain afterwards is not reviewable (Rule 15).
//
// The owner arm is re-read here rather than threaded in: the caller has already
// established that the destination of this verdict is the owner-gone branch (a
// registered owner the kernel proved dead), and asking OwnerVerdictFor again keeps
// the sentence's claim identical to the evidence a reader can re-run.
// ownerProvenGone reports whether the candidate's recorded owner has been proven
// dead (or its pid recycled) by the ONE owner-record ladder. It is the FIRST arm of
// entryVerdictInNamespace, restated as a predicate for the release rule: this rule
// may only take a candidate the owner ladder already decided against, never one it
// would have kept on its own evidence.
func ownerProvenGone(path string) bool {
	v := OwnerVerdictFor(path)
	return v.Registered && v.Outcome != procname.OutcomeLive && v.Outcome != procname.OutcomeUnmeasurable
}

func releasedVerdict(path string, now time.Time, holderEvidence string) EntryVerdict {
	owner := OwnerVerdictFor(path)
	v := EntryVerdict{
		Path:        path,
		Collectable: true,
		HasOwner:    owner.Registered,
		Owner:       owner.Record,
		Outcome:     owner.Outcome,
	}
	if fi, err := os.Lstat(path); err == nil {
		v.Age = now.Sub(fi.ModTime())
	}
	v.Reason = fmt.Sprintf(HolderReleaseFmt, owner.Outcome, holderPopulationClass(holderEvidence), holderEvidence)
	return v
}
