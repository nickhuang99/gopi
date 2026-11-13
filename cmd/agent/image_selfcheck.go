package main

// image_selfcheck.go — the resident faces' adoption of the image self-check
// (internal/tools/image_selfcheck.go; epic merge-image-selfcheck, 2026-10-13).
//
// WHY A RESIDENT FACE CHECKS ITS OWN IMAGE, in one measured sentence: the
// resident `gopi merge --every 2m` that DECIDES landings was found running an
// image whose file had been replaced under it — `readlink -f /proc/<pid>/exe`
// printed `/home/nick/work/GoPi/bin/gopi (deleted)`, and the gate edge its
// source carries was absent from the running process. Every landing that
// decider made was decided without that edge, and nothing anywhere said so.
//
// WHAT THIS FILE ADDS: one prologue check per resident face (both loops —
// `merge --every D` and the worker's claim loop), plus the merge loop's
// per-TICK re-check, because a loop that runs for hours can have the artifact
// replaced underneath it long after it started.
//
// WHAT IT IS NOT (Rule 35): no timer, no unit file, no state file, no watcher,
// no restart helper. The check is a read of facts the process already owns
// (`/proc/self/exe`, its own build info, and the artifact at that path), so it
// adds nothing to the machine — it makes the decider refuse to speak for code
// it is not running.
//
// THE TWO FACES ARE TREATED DIFFERENTLY ON PURPOSE, and the difference is what
// the process DOES with its life:
//
//   - `gopi merge --every D` is a MERGE-ONLY loop: every pass it repeats writes
//     refs. A stale image there is not a degraded mode, it is a decider making
//     permanent decisions with a gate set nobody can see, so the loop refuses to
//     take its next tick and exits (mergeImageStaleExit).
//   - the worker's claim loop is MIXED: it also carries the housekeeping ticks
//     (lifecycle, scratch GC, review sweeps) that a stale image still performs
//     correctly, and refusing them would be a NEW failure introduced by a
//     safety check. It therefore reports the staleness ONCE, loudly, at claim
//     time — where the decision it is about to make actually happens — and keeps
//     ticking.

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/nick/gopi/internal/tools"
)

const (
	// mergeImageStaleExit is the status the merge loop exits with when its
	// image is stale. It is the ordinary failure code, not a new value: what
	// changed is WHICH runs set it (the same reasoning mergeVerifyWithdrawnExitStatus
	// gives). It is spelled as its own constant (Rule 1) because it is the exit
	// status of a refusal a script and the E2E read.
	mergeImageStaleExit = 1

	// imageSelfCheckArmedOnce is the line a resident face prints after a CLEAN
	// check, exactly once per process. It exists so the two states a reader
	// could otherwise not tell apart are distinguishable in a captured log:
	// "the check ran and found the image current" versus "the check is not in
	// this build at all". One line, no heartbeat.
	imageSelfCheckArmedOnce = "%s: image self-check passed (pid %d) — the running image is the artifact at its own path; this is said once so a log that lacks it means the check did not run\n"
)

// residentImagePrologue runs the check at a resident face's start and renders
// its answer.
//
// RETURN CONTRACT: it reports whether the face may CONTINUE — and the caller
// chooses what a refusal means. The merge loop stops; the worker loop reports
// and continues (see this file's header for why the two differ).
//
// Both arms are rendered, and neither is silent: a clean check prints its ONE
// arming line (so the absence of any line is itself readable as "no check"),
// and a stale check prints the tools-layer alarm naming both images.
func residentImagePrologue(prog string) (check tools.SelfImageCheck, ok bool) {
	check = tools.ProbeSelfImage()
	switch {
	case !check.Probed:
		fmt.Println(check.ArmingNote(prog))
		return check, true
	case check.Stale:
		fmt.Println(check.AlarmLine(prog))
		return check, false
	default:
		return check, true
	}
}

// mergeImageTickCheck re-runs the check at a LOOP TICK and prints the alarm
// when the image has gone stale since the prologue. It returns false when the
// loop must stop.
//
// WHY A TICK, and not just the prologue: the incident this closes began with a
// healthy process — the artifact was replaced HOURS after the loop started, and
// a prologue-only check would have passed cleanly and then said nothing for the
// rest of the process's life. The prologue also announces the arming line once,
// so the tick re-check prints nothing while the image stays current.
func mergeImageTickCheck(prog string) bool {
	check := tools.ProbeSelfImage()
	if !check.Stale {
		return true
	}
	fmt.Println(check.AlarmLine(prog))
	return false
}

// workerImageRoundCheck is the WORKER claim loop's per-ROUND re-check of its own
// image, and it exists because the prologue check was not enough.
//
// THE MEASURED HOLE (row 01a18301, 2026-10-28). `gopi worker` pid 1038465
// started at 18:50:43 on revision 84beb309 — a CLEAN prologue, correctly
// reported as armed. The artifact under it was replaced hours later, so the
// process went on deciding with a binary two merged delivery fixes short of the
// disk (20c15007c: empty → nothingToDeliver; 45eb68ece: the exhausted briefing's
// slot swap). Nothing said so, and a row that HAD been delivered was printed as
// "delivery did not land / 工作区被保留，在 empty" — the sentence and the defect
// both produced by code nobody knew was old.
//
// WHY A ROUND BOUNDARY AND NOT A TIMER (Rule 35). The round boundary is a clock
// this loop already has; a timer, a watcher, a unit or a state file would each
// be a new persistent dependency wrapped around gopi, and a wrapper of that
// shape is exactly what the retired hp-worker-autostart cron line was: it makes
// the machine look healthy while nothing explains why. The check is two reads
// the process already owns (`/proc/self/exe` and the artifact at that path), so
// it adds nothing to the machine.
//
// WHY IT REPORTS AND DOES NOT EXIT — the same asymmetry residentImagePrologue
// already states for this face: the merge loop is merge-only (every pass writes
// refs, so a stale image makes permanent decisions with a gate set nobody can
// see) and it stops; this loop ALSO carries housekeeping a stale image performs
// correctly, so stopping would turn a stale image into a new outage. The
// worker's answer is the loud sentence, printed at claim time.
//
// ONCE PER CHANGE, not once per round. A worker can run thousands of rounds; a
// line per round would be a heartbeat nobody reads, and a heartbeat is what a
// real alarm gets lost in. The remembered key is the pair of revisions (and the
// replaced flag), so the log gets exactly one line per transition into a new
// stale state — and nothing while the image stays current.
type workerImageRoundCheck struct {
	// seen is the last stale signature already reported ("" = none yet). Every
	// stale state that differs from it prints; an identical one stays silent.
	seen string
	// probe and out are the two seams a test drives this through, so the
	// assertions are made on INJECTED revisions and a captured stream rather
	// than on whatever binary happens to be running (the epic's own rule: no
	// test reads an unrelated live process). Production takes the defaults in
	// newWorkerImageRoundCheck and never overrides them.
	probe func() tools.SelfImageCheck
	out   io.Writer
}

// newWorkerImageRoundCheck binds the production seams: the real self-check and
// the process's own stdout (the loop's report stream).
func newWorkerImageRoundCheck() *workerImageRoundCheck {
	return &workerImageRoundCheck{probe: tools.ProbeSelfImage, out: os.Stdout}
}

// defaulted returns the receiver with any unset seam filled in, so a zero-value
// check (the shape the round declares) behaves exactly like a constructed one.
func (c *workerImageRoundCheck) defaulted() *workerImageRoundCheck {
	if c.probe == nil {
		c.probe = tools.ProbeSelfImage
	}
	if c.out == nil {
		c.out = os.Stdout
	}
	return c
}

// Report re-runs the self-check and prints the stale warning when the image has
// gone stale since the last report. It never stops the loop and never panics:
// an unmeasurable check is silent here by design, because the prologue already
// announced which arms this build can measure (Rule 15: the absence of the
// alarm is not claimed as freshness).
func (c *workerImageRoundCheck) Report(prog string) {
	c.defaulted()
	check := c.probe()
	if !check.Stale {
		return
	}
	signature := workerImageSignature(check)
	if signature == c.seen {
		return
	}
	c.seen = signature
	// The line is the SELF form of the tools-layer sentence: this process is the
	// one whose image is stale, and it says what will change it (a restart) and
	// what it is deliberately not doing (exiting).
	fmt.Fprintln(c.out, check.AsPeerImage().PeerImageNote(prog, os.Getpid()))
}

// workerImageSignature is the identity of a stale STATE, used to say the warning
// ONCE PER CHANGE rather than once per round (a worker can run thousands of
// rounds, and a heartbeat is what a real alarm gets lost in). It is built from
// the facts that can change — the two revisions and the replaced flag — not from
// the rendered sentence, so a wording change cannot re-print a known state.
func workerImageSignature(c tools.SelfImageCheck) string {
	return c.SelfRevision + "|" + c.DiskRevision + "|" + strconv.FormatBool(c.Replaced)
}
