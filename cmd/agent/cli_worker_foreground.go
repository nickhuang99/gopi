// cli_worker_foreground.go — the FOREGROUND worker's presence, and the control
// face's honest answer about it.
//
// WHY IT EXISTS (bug 01a131c8-0500, measured 2026-10-12 on host-b):
// `gopi worker --stop` printed
//
//	gopi worker: no worker daemon is running (no live worker state under …);
//	a FOREGROUND `gopi worker` holds no lock and is unaffected
//
// while the operator's foreground worker was alive (pid 3642684, 3h34m). Two
// defects in that one sentence:
//
//	(a) the absence of a DAEMON STATE FILE — a cheap proxy — was reported as
//	    the absence of a WORKER, which is the fact the operator asked about
//	    (Rule 15: never state more than what was measured);
//	(b) "is unaffected" made the one command that claims to stop workers
//	    useless on the MOST COMMON shape (`gopi worker`, no --daemon), so the
//	    operator's only exit was `kill` — the tool pushed the operator off the
//	    graceful path it exists to provide.
//
// WHAT THIS FILE DOES. It enumerates the worker-shaped processes that are NOT
// detached daemons (same argv-shape predicate the launcher's --count and
// `--status`'s fallback already share, with the detachment bit INVERTED), and
// it lets `--stop` reach them through the SAME graceful primitive the daemon
// path uses: SIGTERM is the worker's "finish the in-flight round, then exit"
// (installWorkerStopSignals' contract), so a foreground stop is not a second
// semantics — it is the same one, delivered to a process the daemon path
// cannot see.
//
// TWO FACTS THAT ARE REREAD, NEVER ASSUMED (Rule 15):
//
//	① the CALLER is excluded by pid. A `gopi worker --stop` invocation has the
//	   very argv shape a foreground worker has (comm = gopi, argv[1] = worker);
//	   without the exclusion the command would find ITSELF and SIGTERM its own
//	   stop.
//	② the SIGNAL is sent only when the target's executable is PROVABLY this
//	   binary (`workerBinaryIdentity`, /proc/<pid>/exe vs /proc/self/exe). The
//	   durable REQUEST FILE is the delivery on both faces (workerRequestCtxFor
//	   wires it), so a mismatch withholds the SIGNAL and nothing else — and the
//	   three relations it distinguishes keep "the file was replaced under a
//	   running worker" (every build) apart from "a different program" (bug
//	   01a17df2: collapsing those two made --stop distrust every rebuilt worker).

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/tools"
)

// --- named constants (PROJECT.md Rule 1: one place to find and change) ---

const (
	// workerForegroundNoteFmt is the honest line for the no-daemon case: it
	// names what was MEASURED (no live daemon state file) and never asserts the
	// absence of a worker (a foreground worker has no state file to find).
	// %s = program name, %s = state dir.
	workerForegroundNoteFmt = "%s: no DETACHED worker daemon is running (no live daemon state under %s)\n"

	// workerForegroundNoneFmt is the second half of the same answer: it states
	// the fact the process-table half DID measure, so "nothing to stop" is an
	// observation rather than a leftover of the proxy metric. %s = program name.
	workerForegroundNoneFmt = "%s: no FOREGROUND `%s` process is in the process table either\n"

	// workerForegroundFoundFmt reports the foreground workers the daemon-shaped
	// report cannot see: the count, and every pid (bounded by
	// workerForegroundSample, with the remainder counted — a 4KB pid line is
	// not a status report). %s = program name, %d = count, %s = pid list.
	workerForegroundFoundFmt = "%s: %d FOREGROUND `%s` process(es) found (they hold no daemon lock): %s\n"

	// workerForegroundMoreFmt states how many pids the sample omitted, so the
	// count and the list cannot be read as disagreeing. %d = omitted.
	workerForegroundMoreFmt = "  … and %d more"

	// workerForegroundAskingFmt is the graceful arm: the same sentence shape the
	// daemon path prints, with the signal NAMED (Rule 15: the operator must not
	// have to guess which mechanism was used). %s = program name, %d = pid,
	// %s = signal name, %s = the bound.
	workerForegroundAskingFmt = "%s: asking FOREGROUND pid %d to finish the in-flight round and exit (%s, bound %s)\n"

	// workerForegroundForceAskingFmt is the --force arm: SIGUSR1, the worker's
	// existing escalation spelling (workerStopForceSignalName).
	workerForegroundForceAskingFmt = "%s: FORCE requested for FOREGROUND pid %d — interrupting the in-flight round, then exiting (%s)\n"

	// workerForegroundFileAskingFmt is the graceful arm for a target whose
	// binary we cannot prove: the DURABLE REQUEST FILE is the delivery, so the
	// sentence names the file (never a signal) and the same words the daemon
	// arm uses for the same case. %s = program name, %d = pid, %s = the bound.
	workerForegroundFileAskingFmt = "%s: asking FOREGROUND pid %d to finish the in-flight round and exit (stop request file, bound %s)\n"

	// workerForegroundAnsweredFmt is the DELIVERY-PROVEN line: the pid wrote a
	// receipt naming THIS process's request, so the channel worked — stated as
	// the measurement it is, with the latency, and never qualified by what
	// executable the pid happens to be running. %s = program name, %d = pid,
	// %s = the request word, %s = elapsed, %s = the receipt's path.
	workerForegroundAnsweredFmt = "%s: FOREGROUND pid %d ANSWERED the %s request — delivery PROVEN by the receipt (%s after it was made): %s\n"

	// workerForegroundUnansweredFmt states the OTHER measurement, and states it
	// as exactly what was observed on this pid: no receipt arrived inside the
	// settle window. It does NOT name a cause it did not measure — an absent
	// receipt has several possible
	// ones (an out-of-round process such as `--resume`, a worker predating the
	// receipt, a round that had already covered the boundary) and this command
	// can observe none of them from here. What it CAN say with certainty is the
	// one thing the file channel guarantees: the request is ON DISK, and a
	// process's executable identity has no bearing on whether it is read.
	// %s = program name, %d = pid, %s = the settle window, %s = the request
	// file's path.
	workerForegroundUnansweredFmt = "%s: pid %d wrote no stop receipt within %s — the request is DELIVERED and is on disk at %s; whether the pid acts on it is decided by its own consume point, and this command has not observed that\n"

	// workerFaceForeground / workerFaceDaemon name WHICH face is reporting, in
	// the second-invocation lines. Both faces share one channel and one sentence
	// (Rule 2), and the only thing that must differ is the word naming the kind
	// of process the operator asked about.
	workerFaceForeground = "FOREGROUND"
	workerFaceDaemon     = "daemon"

	// workerAlreadyAskedFmt is the ONE-line "nothing to do" report a repeat meets
	// BEFORE anything is read back: a durable stop/force request for this pid is
	// already standing, so this invocation writes nothing and signals nothing.
	// Rule 2: both faces render it from here, and %s names which face is
	// reporting (workerFaceForeground / workerFaceDaemon).
	workerAlreadyAskedFmt = "%s: %s pid %d is already asked to leave (%s) — nothing to do\n"

	// workerAlreadyDeliveredFmt is the SECOND-INVOCATION line: the
	// request on disk was written by an EARLIER sender, and that sender already
	// read the one acknowledgement the protocol produces. It names the request,
	// its recorded receipt and — the point — that no further receipt is awaited.
	// %s = program name, %d = pid, %s = who/when asked, %s = request path,
	// %d = receipts on record, %s = the most recent receipt.
	workerAlreadyDeliveredFmt = "%s: %s pid %d was asked to leave by an EARLIER invocation (%s): the request is on disk at %s and %d acknowledgement(s) are on record (%s) — no further receipt is awaited, because one request gets exactly one acknowledgement and that one is already accounted for\n"

	// workerAlreadyDeliveredNoAckFmt is the same line for a prior
	// delivery whose receipt is NOT recorded anywhere: the request stands, the
	// acknowledgement was never observed, and this invocation still does not wait
	// for one (the reply it would wait for belongs to the earlier sender).
	// %s = program name, %d = pid, %s = who/when asked, %s = request path.
	workerAlreadyDeliveredNoAckFmt = "%s: %s pid %d was asked to leave by an EARLIER invocation (%s): the request is on disk at %s and no acknowledgement is on record for it — the request STANDS and none is awaited from this invocation either, because the one acknowledgement this protocol allows cannot be produced a second time\n"

	// workerPriorUnprovenFmt reports the state an earlier delivery left
	// with no acknowledgement anywhere: delivered, unconsumed as far as this
	// command observed, and NOT reported as stopped (Rule 15: the verdict names
	// what was measured, and offers the operator's own escalation spelling).
	// %s = program name, %d = pid, %s = request path, %s = program name (the
	// escalation command), %s = the force verb, %d = pid again.
	workerPriorUnprovenFmt = "%s: pid %d's stop request was delivered by an EARLIER invocation and no acknowledgement is on record; it stands at %s and the pid is NOT reported as stopped — this command has not observed it acting on the request (escalate with `%s %s --pid %d` to interrupt the in-flight round)\n"

	// workerForegroundReplacedImageFmt names the REBUILD shape on its own: the
	// pid runs the SAME PATH this command does, whose file has been replaced
	// since the process started. That is what the kernel's " (deleted)" marker
	// means (a write-and-rename install, i.e. every `make build`), and it is
	// reported as the ordinary event it is — NOT as a foreign program and NOT as
	// "the signal contract cannot be verified". The signal is still withheld
	// (inode identity is what makes it provable), but it is withheld as an
	// OPTIMISATION: the file is the delivery either way.
	// %s = program name, %d = pid, %s = the path both run.
	workerForegroundReplacedImageFmt = "%s: pid %d runs the image at %s, REPLACED since it started (kernel marker %q) — an ordinary rebuild, not a foreign program; the stop goes by request file, which no executable identity can affect\n"

	// workerForegroundForeignImageFmt names the genuinely DIFFERENT-PATH case:
	// the pid was started from another file altogether (another build, another
	// tag), so its executable can never be proven identical to this one and the
	// signal is withheld. The delivery is unaffected — it is the file — and the
	// receipt, when one arrives, is what proves it.
	// %s = program name, %d = pid, %s = this exe, %s = theirs.
	workerForegroundForeignImageFmt = "%s: pid %d runs a DIFFERENT executable FILE than this command (%s vs %s) — its identity cannot be proven identical, so no signal accompanies the request; the request file is the delivery and the receipt is its proof\n"

	// workerForegroundNoDirFmt is the ONE case that is still nothing-delivered:
	// the control directory (where the request file must live) could not be
	// resolved, so this pid cannot be asked by file at all. It names the
	// failure and the operator's own door rather than pretending the request
	// exists. %s = program name, %d = pid, %v = the resolution error, %s = the
	// program name again (the escalation command), %s = the signal name,
	// %d = pid again.
	workerForegroundNoDirFmt = "%s: cannot resolve the control directory for pid %d (%v) — NO request file was written; the remaining door is a signal: `kill -%s %d`\n"

	// workerForegroundFileFailFmt reports a request file that could not be
	// written (Rule 15b: the failure is named with its cause, never swallowed
	// into a silent "no signal"). %s = program name, %d = pid, %v = the error.
	workerForegroundFileFailFmt = "%s: could not write the stop request file for pid %d: %v\n"

	// workerForegroundExitedFmt is the success line (the requested end state),
	// with the time it took — the fact an operator checks afterwards.
	workerForegroundExitedFmt = "%s: FOREGROUND pid %d exited %s after the %s request\n"

	// workerForegroundStillFmt is the failure line: the end state was NOT
	// reached, and the printed remedy is the operator's own escalation (the
	// same --force spelling the daemon path names).
	workerForegroundStillFmt = "%s: FOREGROUND pid %d is STILL running %s after the %s request — no round boundary was reached; to interrupt it: `%s %s %s` (or `kill -%s %d`)\n"

	// workerForegroundStatusFmt renders ONE foreground worker for `--status`:
	// the pid, the stable TASK FIELD (`task=<id>`, derived from the worker's own
	// argv — epic worker-status-task-field), its command line, and how it is
	// stopped. The field is second on the line, in the same position the record
	// shapes put it, so one grep finds it on every shape.
	// %s = program name, %d = pid, %s = the task field, %s = command line,
	// %s = program name again (the stop command), %s = the stop flag.
	workerForegroundStatusFmt = "%s pid %d: FOREGROUND %s (holds no daemon lock) — %s; `%s %s` signals it"

	// workerForegroundNoCmd is the honest marker for a foreground worker whose
	// argv could not be read (reused from the status face's own marker so the
	// two renders cannot drift).
	workerForegroundNoCmd = workerStatusCommandUnknown

	// workerForegroundSample bounds the pid list on one line before it is
	// summarized — the status face's readability rule, at the control face.
	workerForegroundSample = workerStatusResidueSample

	// workerForegroundSignalWord / workerForegroundForceWord name the request
	// in the wait's messages (the daemon path's workerStopWord).
	workerForegroundSignalWord = "graceful stop"
	workerForegroundForceWord  = "FORCED stop"
)

// foregroundWorkerPIDs lists the pids whose process shape is a gopi WORKER that
// is NOT a detached daemon, in the CALLER'S OWN profile — the counterpart of
// fallbackWorkerPIDs, which deliberately requires detachment.
//
// The caller's own pid is excluded: a `gopi worker --stop`/`--status`
// invocation matches the same shape (comm = gopi, argv[1] = worker) and would
// otherwise find itself. Ownership is the same second question the daemon
// fallback asks, answered the same way (the process's OWN environment), with
// the same degradation: an unattributable worker counts as this profile's.
func foregroundWorkerPIDs(own string) []int {
	entries, err := os.ReadDir(procnameRoot())
	if err != nil {
		return nil
	}
	self := os.Getpid()
	type shaped struct {
		pid     int
		profile string
	}
	var matches []shaped
	for _, e := range entries {
		pid, aerr := strconv.Atoi(e.Name())
		if aerr != nil || pid <= 0 || pid == self {
			continue // not a pid, or THIS invocation (it is a control read, not a worker)
		}
		if !tools.PidAlive(pid) {
			continue
		}
		if workerForegroundShape(pid) {
			matches = append(matches, shaped{pid: pid, profile: workerProfileOf(pid)})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].pid < matches[j].pid })
	var mine []int
	for _, m := range matches {
		if own == "" || m.profile == "" || m.profile == own {
			mine = append(mine, m.pid)
			continue
		}
		// Another profile's foreground worker: not this operator's fleet, and
		// deliberately NOT signalled by this invocation (the caller cannot see
		// whose it is). It is left out of the list rather than acted on.
	}
	return mine
}

// workerForegroundShape is workerProcessShape with the detachment test INVERTED:
// argv[1] == the worker verb, the executable is the gopi program, and the process
// does NOT lead its own session (i.e. it is not the post-setsid daemon).
//
// THE IMAGE NAME IS READ, NOT ASSUMED (measured 2026-10-13). workerProcessShape
// tests `comm == "gopi"`; a foreground worker started from a differently-named
// build (`/tmp/gopi-fix worker`) carries `gopi-fix` in comm, and this run's own
// first real-run pass reported "no FOREGROUND worker in the process table" while
// the pid was alive — the same false absence as the bug, one level down. So the
// name comes from the process's OWN image: /proc/<pid>/exe (the kernel's answer),
// falling back to its own argv[0] when exe cannot be read (a deleted binary, an
// unreadable entry) — and it must name the GOPI program (workerProgramPrefix).
// Signaling keeps the strict test (workerBinaryIdentity, device+inode): find
// broadly, signal conservatively.
func workerForegroundShape(pid int) bool {
	root := procnameRoot()
	argv, ok := procname.Cmdline(root, strconv.Itoa(pid))
	if !ok || len(argv) < 2 || argv[1] != procname.VerbWorker {
		return false
	}
	if !workerGopiImage(root, pid, argv[0]) {
		return false
	}
	return !workerDetached(root, pid)
}

// workerGopiImage reports whether a process runs the gopi program: its image's
// base name begins with workerProgramPrefix. The name is taken from
// /proc/<pid>/exe when the kernel answers, and from the process's own argv[0]
// otherwise — never from a guess ("a process whose image is called nginx and
// whose argv[1] happens to be `worker` is not our worker").
//
// THE JUDGEMENT IS THE LEAF PACKAGE'S (Rules 13/14): procname.IsGopiImage owns
// the two channels and the prefix test, so this face, the `--status` face, the
// launcher script and internal/config cannot drift apart. This function is the
// thin adapter that binds it to a pid's argv[0].
func workerGopiImage(root string, pid int, argv0 string) bool {
	return procname.WorkerImageIsGopi(root, strconv.Itoa(pid), argv0)
}

// workerBinaryIdentity answers WHICH FILE a pid's executable is, in the only
// terms that let three different facts be told apart instead of collapsed into
// "different binary":
//
//	① PROVEN   — same device+inode as this process. The signal is safe on the
//	             identity axis (the binary loaded IS the file this command is).
//	② REPLACED — the two are the SAME PATH and the target's link carries the
//	             kernel's " (deleted)" marker. That is a write-and-rename
//	             install, i.e. every `make build`: the pid is still running THIS
//	             program, just the previous copy of it. It is an ORDINARY EVENT,
//	             not a foreign program — the axis the stop channel depends on is
//	             the request FILE, and no rebuild can affect it.
//	③ FOREIGN  — a different PATH. The process was started from another file
//	             altogether (another build, another tag, another location).
//
// The distinction is the fix (bug 01a17df2): the (dev,inode) test alone cannot
// make it, so it answered "we cannot verify this worker's signal contract" on
// the ordinary rebuild shape — where the answer is "same program, older copy".
//
// It returns both paths so the report names WHICH two files are being compared
// (Rule 15: the operator reads the evidence, not a verdict).
func workerBinaryIdentity(pid int) (workerBinaryRelation, string, string) {
	selfPath := workerExePath(os.Getpid())
	otherPath := workerExePath(pid)
	selfInfo, serr := os.Stat(filepath.Join(procnameRoot(), strconv.Itoa(os.Getpid()), procExeName))
	otherInfo, oerr := os.Stat(filepath.Join(procnameRoot(), strconv.Itoa(pid), procExeName))
	switch {
	case serr == nil && oerr == nil && os.SameFile(selfInfo, otherInfo):
		return workerBinaryProven, selfPath, otherPath
	case workerSameImagePath(selfPath, otherPath):
		// The kernel AND the resolved path agree the file is the same one: the
		// only difference is that the bytes were replaced under a running
		// process.
		return workerBinaryReplaced, selfPath, otherPath
	}
	return workerBinaryForeign, selfPath, otherPath
}

// workerSameImagePath reports whether the target's executable is the SAME FILE
// as this process's on disk, having been replaced since it started.
//
// ITS EVIDENCE IS THE PATH PLUS THE KERNEL'S OWN MARKER, and both are required:
// the path alone is a name (two installs can share one), and a link with no
// marker is the live file — a case SameFile has already answered, so it must not
// be re-decided here on weaker evidence.
//
// The marker is what the kernel appends when the image it resolved has been
// unlinked; that happens on every write-and-rename install, whether or not the
// replacement is still there. Either way the answer is the same and it is the one
// that matters: this pid is running THIS program's image at THIS path — the
// previous copy of it — which is not "a different executable FILE".
func workerSameImagePath(selfPath, otherPath string) bool {
	if selfPath == "" || otherPath == "" {
		return false
	}
	bare, marked := strings.CutSuffix(otherPath, procname.DeletedImageMarker)
	if !marked {
		return false
	}
	return bare == selfPath
}

// workerBinaryRelation is the three-way answer workerBinaryIdentity reaches.
// The zero value is FOREIGN on purpose: an unreadable /proc entry must never be
// read as a proven identity, and the FORBIDDEN direction of a wrong guess is
// "we signalled a process whose binary we did not know".
type workerBinaryRelation int

const (
	// workerBinaryForeign: a different executable FILE (another build, another
	// tag) — the signal is withheld, the request file is the delivery.
	workerBinaryForeign workerBinaryRelation = iota
	// workerBinaryReplaced: the same path, the file replaced since the process
	// started (an ordinary rebuild) — the signal is still withheld because the
	// identity is not provable, and the report says WHY in its own words.
	workerBinaryReplaced
	// workerBinaryProven: same device+inode — the signal is safe to add.
	workerBinaryProven
)

// workerBinarySignallable reports whether the relation permits the signal. It is
// a named predicate rather than an inline comparison (Rule 1b: one spelling of
// the decision, so the ask and the report cannot reach different conclusions).
func (r workerBinaryRelation) workerBinarySignallable() bool {
	return r == workerBinaryProven
}

// procExeName is the procfs entry naming a process's executable — the identity
// workerBinaryIdentity reads. It is the shared constant's alias (Rule 14).
const procExeName = procname.ExeName

// workerProgramPrefix names the gopi PROGRAM: a process whose image base name
// starts with it (gopi, gopi-fix, gopi-<tag>) is running this program — the
// identity that survives a binary built under another file name. It is the
// shared constant's alias in this package (Rule 14: one definition, referenced
// by name, never a re-typed literal), and it is a PREFIX rather than an equality
// because the equality with the file name was exactly the false absence
// (bug 01a131fcb8337c1b).
const workerProgramPrefix = procname.ImageGopiPrefix

// workerExePath resolves a pid's executable path for a report ("" when /proc
// will not say — rendered by the caller, never guessed).
func workerExePath(pid int) string {
	target, err := os.Readlink(filepath.Join(procnameRoot(), strconv.Itoa(pid), procExeName))
	if err != nil {
		return ""
	}
	return target
}

// --- the control face's answer about foreground workers -------------------

// workerForegroundStopNote prints the honest no-daemon answer and, when any
// foreground worker was found, the pids. It returns the pids (empty when none).
//
// It is the ONE renderer of that answer: `--stop` and `--restart` both call it,
// so the two faces cannot describe the same state differently.
func workerForegroundStopNote(dir string) []int {
	fmt.Printf(workerForegroundNoteFmt, workerProgTopLevel, dir)
	pids := foregroundWorkerPIDs(workerStatusOwnProfile())
	if len(pids) == 0 {
		fmt.Printf(workerForegroundNoneFmt, workerProgTopLevel, workerProgTopLevel)
		return nil
	}
	shown := pids
	more := 0
	if len(shown) > workerForegroundSample {
		more = len(shown) - workerForegroundSample
		shown = shown[:workerForegroundSample]
	}
	list := make([]string, 0, len(shown))
	for _, pid := range shown {
		list = append(list, strconv.Itoa(pid))
	}
	line := fmt.Sprintf(workerForegroundFoundFmt, workerProgTopLevel, len(pids), workerProgTopLevel, strings.Join(list, ", "))
	if more > 0 {
		line += fmt.Sprintf(workerForegroundMoreFmt, more)
	}
	fmt.Println(line)
	return pids
}

// workerControlStopForeground asks the foreground workers to leave and waits for
// them, on the same contract workerControlStopOne implements for a daemon:
// `true` = the requested end state holds (every pid is gone OR has acknowledged
// the request), `false` = at least one is still there (or could not be asked —
// the caller exits non-zero, with the remedy printed). The receipt poll is built
// HERE, once per pid and once for the whole wait, so the ask and the wait cannot
// disagree about which file answers (Rule 2).
//
// ORDER: every pid is asked FIRST, then the whole set is waited on — so a stop
// of N workers costs one wait, not N.
func workerControlStopForeground(pids []int, bound time.Duration, force bool) bool {
	dir, derr := tools.ControlDir(workerStateDirName)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "%s: could not resolve the control directory (%v) — no stop receipt can be read\n",
			workerProgTopLevel, derr)
		dir = ""
	}
	acks := make([]*workerAckPoll, len(pids))
	// decided[i] = an EARLIER invocation already delivered pids[i]'s request, so
	// this one writes nothing, signals nothing — and WAITS for nothing.
	// priorOK[i] = that earlier delivery's end state is OBSERVED to hold (its
	// receipt is recorded on disk, or the pid is gone). Together they are what
	// stops the second `--stop` from paying the receipt bound for a reply the
	// protocol can never produce twice (bug: a second --stop hung the full 10m).
	decided := make([]bool, len(pids))
	priorOK := make([]bool, len(pids))
	asked := 0
	for i, pid := range pids {
		if prior := readWorkerStopPriorDelivery(dir, pid, force); prior.Requested {
			fmt.Printf(workerAlreadyAskedFmt,
				workerProgTopLevel, workerFaceForeground, pid, workerStopOrForceWord(force))
			workerReportPriorDelivery(pid, prior)
			decided[i] = true
			priorOK[i] = prior.Receipts > 0 || !tools.PidAlive(pid)
			asked++
			continue
		}
		acks[i] = newWorkerAckPoll(dir, pid)
		if !workerForegroundAsk(pid, bound, force) {
			// Nothing was delivered to this pid by EITHER door (the control
			// directory could not be resolved AND the binary was not proven):
			// the failure has already been reported by name with the operator's
			// own escalation. Nothing was sent to it.
			continue
		}
		asked++
	}
	if asked == 0 {
		return false
	}
	// A PRIOR DELIVERY WITH NO ACKNOWLEDGEMENT ANYWHERE is the one case whose
	// verdict this command must not soften: the request stands on disk and this
	// invocation did not observe the worker act on it, so it is NOT "stopped" —
	// and waiting cannot change that, because the receipt it would wait for
	// belongs to the sender that wrote the request, not to this one.
	unproven := 0
	for i, pid := range pids {
		if !decided[i] || priorOK[i] {
			continue
		}
		fmt.Fprintf(os.Stderr, workerPriorUnprovenFmt, workerProgTopLevel, pid,
			workerRequestPathText(dir, pid, force), workerProgTopLevel, workerControlForce, pid)
		unproven++
	}
	if unproven > 0 {
		return false
	}
	if bound == 0 {
		// NOT WAITING IS NOT NOT-MEASURING. A zero bound means this invocation
		// does not hold the operator here, so the delivery verdict is stated from
		// what is observable RIGHT NOW: a receipt already on disk proves the
		// channel worked, and its absence is reported as the absence it is — with
		// the request file's path, which is the fact that does not depend on the
		// target's executable at all (bug 01a17df2: the old line named a binary
		// mismatch as the reason, which nothing had measured about THIS pid).
		workerForegroundReportUnwaited(pids, force, acks, decided)
		fmt.Printf("%s: request delivered — not waiting (%s 0); the worker exits after the in-flight round\n",
			workerProgTopLevel, workerControlWait)
		return false
	}
	return workerForegroundWaitExit(pids, bound, force, acks, decided)
}

// workerStopPriorDelivery is what a FORMER invocation's delivery of pid's stop
// request left on disk, as this one can read it.
//
// WHY THIS EXISTS (the bug this face was fixed for). One request has exactly ONE
// receipt: the worker writes ACK_<pid> when it CONSUMES the request, and the
// sender that reads it ARCHIVES it — so a second `gopi worker --stop` finds the
// request still standing, no live receipt to read, and used to sit out the whole
// --wait bound (10m by default) waiting for a reply that cannot be produced a
// second time. The request's own footprint IS the answer to "was this delivered
// before?": the file is already there when this invocation looks, and this
// invocation has written nothing yet.
type workerStopPriorDelivery struct {
	// Requested: a durable stop request for this pid is standing on disk.
	Requested bool
	// SenderPID is the pid that wrote it (0 when the header cannot be read) —
	// the evidence that it was NOT this invocation.
	SenderPID int
	// SenderTS is when it was written, in the request's own words.
	SenderTS string
	// RequestPath is the file itself: the fact that survives every executable
	// identity and every receipt that has already been archived.
	RequestPath string
	// Receipts is how many receipts this pid has already been recorded as
	// answering (live + archived): the ACK side of "the protocol worked once".
	Receipts int
	// AckedBy / AckedAt name the most recent recorded receipt (0 / "" when the
	// evidence exists only as a file this process may not parse).
	AckedBy int
	AckedAt string
}

// workerStopPriorDelivery reads the durable evidence of an earlier delivery: the
// standing request file, its sender header, and how many acknowledgements that
// pid already has on record. It never guesses: an unreadable file is reported as
// "requested with an unreadable header", and a receipt count is the number of
// files the two channels actually hold.
func readWorkerStopPriorDelivery(dir string, pid int, force bool) workerStopPriorDelivery {
	var prior workerStopPriorDelivery
	// WHICH request file answers the question is the SAME predicate the ask uses
	// (workerForegroundAsk): a graceful delivery is TERM_<pid>, an escalation is
	// FORCE_<pid>. A --force therefore sees only an EARLIER FORCE as already
	// delivered — escalating a graceful request must still reach the ask (it is
	// the operator's escape hatch, not a repeat).
	path := workerRequestPathText(dir, pid, force)
	prior.RequestPath = path
	if _, err := os.Stat(path); err != nil {
		return workerStopPriorDelivery{}
	}
	prior.Requested = true
	body, err := os.ReadFile(path)
	if err == nil {
		sender := parseWorkerStopSender(body)
		prior.SenderPID, prior.SenderTS = sender.PID, sender.TS
	}
	prior.Receipts, prior.AckedBy, prior.AckedAt = workerAckEvidenceFor(dir, pid)
	return prior
}

// workerAckEvidenceFor counts the receipts pid has on record and names the most
// recent one: LIVE receipts (written, not yet read by a sender) plus ARCHIVED
// ones (read and kept — see workerAckArchivedSep: the archive is the evidence,
// the log is its index). It reads the files, never the index log alone, because a
// host that lost the log still has the receipts.
func workerAckEvidenceFor(dir string, pid int) (count int, ackedBy int, ackedAt string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, ""
	}
	prefix := workerAckPrefix + strconv.Itoa(pid)
	latest := time.Time{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// The live name is exactly <ACK_><pid>; an archived one continues with
		// the archive separator, so pid 41 must not claim pid 412's receipts.
		if name != prefix && !(strings.HasPrefix(name, prefix+workerAckArchivedSep) && workerAckArchivedReqID(name) > 0) {
			continue
		}
		count++
		path := filepath.Join(dir, name)
		if body, rerr := os.ReadFile(path); rerr == nil {
			a := parseWorkerAck(body, path)
			ts, terr := time.Parse(time.RFC3339, a.AckedAt)
			if terr == nil && ts.After(latest) {
				latest, ackedBy, ackedAt = ts, a.AckedBy, a.AckedAt
			}
			continue
		}
		if fi, serr := e.Info(); serr == nil && fi.ModTime().After(latest) {
			latest, ackedAt = fi.ModTime(), ""
		}
	}
	return count, ackedBy, ackedAt
}

// workerReportPriorDelivery states, in one line, WHAT was already delivered and
// WHETHER its receipt is on record — and then the sentence this whole fix exists
// for: no further receipt is awaited, because one request gets one acknowledgement
// and that one is already accounted for.
func workerReportPriorDelivery(pid int, prior workerStopPriorDelivery) {
	switch {
	case prior.Receipts > 0:
		fmt.Printf(workerAlreadyDeliveredFmt, workerProgTopLevel, workerFaceForeground, pid,
			workerStopSenderText(prior), prior.RequestPath, prior.Receipts, workerAckRecordText(prior))
	default:
		fmt.Fprintf(os.Stderr, workerAlreadyDeliveredNoAckFmt, workerProgTopLevel, workerFaceForeground, pid,
			workerStopSenderText(prior), prior.RequestPath)
	}
}

// workerStopSenderText renders WHO asked and WHEN, from the request's own header.
func workerStopSenderText(prior workerStopPriorDelivery) string {
	if prior.SenderPID <= 0 {
		return "sender unreadable (the file's header carries no pid)"
	}
	if prior.SenderTS == "" {
		return "sent by pid " + strconv.Itoa(prior.SenderPID)
	}
	return "sent by pid " + strconv.Itoa(prior.SenderPID) + " at " + prior.SenderTS
}

// workerAckRecordText renders the acknowledgement already on record.
func workerAckRecordText(prior workerStopPriorDelivery) string {
	if prior.AckedAt == "" {
		return "the receipt file is on disk but its body names no time this command can read"
	}
	by := "pid unknown"
	if prior.AckedBy > 0 {
		by = "pid " + strconv.Itoa(prior.AckedBy)
	}
	return by + " at " + prior.AckedAt
}

// workerForegroundReportUnwaited states, per pid, what the zero-bound invocation
// can actually observe about delivery: an existing receipt, or none yet.
func workerForegroundReportUnwaited(pids []int, force bool, acks []*workerAckPoll, decided []bool) {
	for i, pid := range pids {
		if decided[i] {
			continue // already asked by an earlier invocation: no receipt is awaited from THIS one
		}
		if a, ok := acks[i].seen(); ok {
			acks[i].print(a)
			acks[i].consume(a)
			fmt.Printf(workerForegroundAnsweredFmt, workerProgTopLevel, pid,
				workerForegroundWord(force), workerAckImmediateText, a.Path)
			continue
		}
		fmt.Fprintf(os.Stderr, workerForegroundUnansweredFmt, workerProgTopLevel, pid,
			workerAckImmediateText, acks[i].path())
	}
}

// workerForegroundAsk delivers the stop to ONE foreground pid, and reports
// whether the request was DELIVERED (by either door).
//
// THE FILE IS THE DELIVERY; THE SIGNAL IS AN OPTIMISATION (bug 2026-10-13). The
// durable request file is consumed at a claim boundary by BOTH faces now
// (workerRequestCtxFor wires the foreground worker's ctx), so it works whatever
// binary the target runs. The signal is added only when the target's executable
// is PROVABLY this binary (workerBinaryIdentity): a process whose executable is
// not the file this command is can be running anything, and a worker that
// predates the drain contract forwards a stop into its in-flight RUN CHILD and
// kills the very round the request protects (measured 2026-09-13 on the daemon
// face). So an unproven target is NOT a refusal — it is file-only delivery, the
// same shape workerForeignDrainBinary chooses for the daemon.
func workerForegroundAsk(pid int, bound time.Duration, force bool) bool {
	relation, selfPath, otherPath := workerBinaryIdentity(pid)
	same := relation.workerBinarySignallable()
	dir, derr := tools.ControlDir(workerStateDirName)
	if derr != nil || dir == "" {
		// No control directory = no request file can exist = the signal is the
		// only door left, and only for a target we can prove. This is the ONE
		// case that can still end in nothing being delivered, so it is reported
		// as such, with the operator's own command (Rule 15).
		fmt.Fprintf(os.Stderr, workerForegroundNoDirFmt,
			workerProgTopLevel, pid, derr, workerGracefulSignalName, pid)
		return workerForegroundSignalIfProven(pid, same, force)
	}
	// ALREADY-ASKED IDEMPOTENCY (the daemon arm's own contract): a standing
	// request is a durable FACT, so this invocation reports the true state
	// instead of rewriting the file or signalling a second time.
	standing := workerDrainRequestedFor(dir, pid)
	if force {
		standing = workerFileForceRequested(dir, pid)
	}
	var writeErr error
	if !standing {
		if force {
			writeErr = workerRequestForceFile(dir, pid)
		} else {
			writeErr = workerRequestStopFile(dir, pid)
		}
		if writeErr != nil {
			fmt.Fprintf(os.Stderr, workerForegroundFileFailFmt, workerProgTopLevel, pid, writeErr)
		}
	}
	switch {
	case standing:
		fmt.Printf(workerAlreadyAskedFmt,
			workerProgTopLevel, workerFaceForeground, pid, workerStopOrForceWord(force))
	case force:
		fmt.Printf(workerForegroundForceAskingFmt, workerProgTopLevel, pid, workerStopForceSignalName)
	case same:
		fmt.Printf(workerForegroundAskingFmt, workerProgTopLevel, pid, workerGracefulSignalName, workerBoundText(bound))
	case relation == workerBinaryReplaced:
		// THE REBUILD SHAPE, NAMED AS ITSELF (bug 01a17df2). The pid runs the
		// same path as this command; the file was replaced under it. Reported
		// as the ordinary event it is, and the request file — which no
		// executable identity can affect — is the delivery.
		fmt.Printf(workerForegroundFileAskingFmt, workerProgTopLevel, pid, workerBoundText(bound))
		fmt.Printf(workerForegroundReplacedImageFmt,
			workerProgTopLevel, pid, workerPathOrUnknown(selfPath), procname.DeletedImageMarker)
	default:
		// A genuinely different FILE: its identity can never be proven
		// identical, so no signal rides along. Still a delivery — by the file.
		fmt.Printf(workerForegroundFileAskingFmt, workerProgTopLevel, pid, workerBoundText(bound))
		fmt.Printf(workerForegroundForeignImageFmt, workerProgTopLevel, pid,
			workerPathOrUnknown(selfPath), workerPathOrUnknown(otherPath))
	}
	if standing {
		return true // the request the operator wanted is already standing
	}
	if !same {
		// File-only: the request is delivered and the round is protected. The
		// RETURN VALUE IS ABOUT THE FILE, which is the channel that exists for
		// every target; whether the pid then reads it is answered by the RECEIPT
		// (workerAckPoll), and this function must not pre-empt that verdict with
		// a claim about the target's executable.
		return writeErr == nil
	}
	return workerForegroundSignalDelivered(pid, force) || writeErr == nil
}

// workerForegroundSignalIfProven sends the stop signal for a pid whose binary is
// proven to be this one, and reports whether anything was delivered. It is the
// degraded arm (no control directory): a target we cannot prove gets nothing,
// which the caller must know in order to exit non-zero.
func workerForegroundSignalIfProven(pid int, same bool, force bool) bool {
	if !same {
		return false
	}
	return workerForegroundSignalDelivered(pid, force)
}

// workerForegroundSignalDelivered sends the request's signal spelling (SIGTERM,
// or SIGUSR1 beside it for --force) to a target whose binary is proven to be
// this one, and reports whether the process was actually signalled.
func workerForegroundSignalDelivered(pid int, force bool) bool {
	if force {
		workerSignalPID(pid, syscall.SIGUSR1, "operator ran "+workerProgTopLevel+" "+workerControlStop)
		// SIGTERM as well, exactly as the daemon arm does: a worker binary that
		// predates the SIGUSR1 handler must not be left running under an
		// operator who typed --force (its SIGTERM is the graceful stop).
		workerSignalPID(pid, syscall.SIGTERM, "operator ran "+workerProgTopLevel+" "+workerControlForce)
		return true
	}
	workerSignalPID(pid, syscall.SIGTERM, "operator ran "+workerProgTopLevel+" "+workerControlStop)
	return true
}

// workerRequestPathText names the durable request file the ask delivered or
// found: the path an operator (or a triaging agent) reads to check the request
// independently of this command's own word.
func workerRequestPathText(dir string, pid int, force bool) string {
	if force {
		return filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid))
	}
	return tools.ControlRequestPath(dir, workerStopRequestPrefix, pid)
}

// workerStopOrForceWord names the request in the already-standing line.
func workerStopOrForceWord(force bool) string {
	if force {
		return "force requested"
	}
	return "stop requested"
}

// workerPathOrUnknown renders an executable path for a report where /proc may
// have given no answer (an unreadable entry, a deleted binary) — the value is
// NAMED as unknown, never left blank.
func workerPathOrUnknown(p string) string {
	if p == "" {
		return "unknown (unreadable /proc/<pid>/exe)"
	}
	return p
}

// workerBoundText renders the operator's wait bound for the asking line. One
// renderer, so the sentence and the wait cannot disagree.
func workerBoundText(bound time.Duration) string { return workerControlWait + " " + bound.String() }

// workerForegroundWaitExit polls the whole set until every pid is gone, the
// receipt for this process's request has arrived for every pid, or the bound
// expires. `acks[i]` is the receipt poll for pids[i], and a nil entry (an
// unresolved control directory) is inert: that pid is then only watched for its
// exit, exactly as before.
func workerForegroundWaitExit(pids []int, bound time.Duration, force bool, acks []*workerAckPoll, decided []bool) bool {
	deadline := time.Now().Add(bound)
	start := time.Now()
	answered := make([]bool, len(pids))
	for i := range pids {
		// A pid whose request an EARLIER invocation delivered is not this one's
		// to wait for: its receipt was consumed by that sender (one request gets
		// one acknowledgement), so polling for it here would burn the whole bound
		// on a reply the protocol cannot produce again.
		answered[i] = decided[i]
	}
	for {
		remaining := workerForegroundAlive(pids)
		if len(remaining) == 0 {
			for i := range acks {
				acks[i].awaitBriefly() // the receipt is the worker's last act — see workerWaitForExit
			}
			fmt.Printf("%s: FOREGROUND %s exited %s after the %s request\n",
				workerProgTopLevel, workerPIDList(pids), time.Since(start).Round(time.Millisecond), workerForegroundWord(force))
			return true
		}
		// THE RECEIPT (epic worker-stop-ack): a worker that ANSWERED is done
		// being waited on, even while it finishes its in-flight round — that is
		// the whole point of the channel. The exit check above still runs first,
		// so a worker that is already gone is reported as gone.
		allAnswered := true
		for i, pid := range pids {
			if answered[i] {
				continue
			}
			a, ok := acks[i].seen()
			if !ok {
				allAnswered = false
				continue
			}
			acks[i].print(a)
			acks[i].consume(a)
			answered[i] = true
			fmt.Printf("%s: FOREGROUND pid %d acknowledged the %s request %s after it was made — not waiting for the round to finish (bound %s)\n",
				workerProgTopLevel, pid, workerForegroundWord(force), time.Since(start).Round(time.Millisecond), bound)
		}
		if allAnswered {
			return true
		}
		if !time.Now().Before(deadline) {
			for _, pid := range remaining {
				fmt.Fprintf(os.Stderr, workerForegroundStillFmt,
					workerProgTopLevel, pid, bound, workerForegroundWord(force),
					workerProgTopLevel, workerControlStop, workerControlForce, workerGracefulSignalName, pid)
			}
			for i := range pids {
				if answered[i] {
					continue
				}
				acks[i].missing("the bound expired and the process is still running; the request is not withdrawn")
			}
			return false
		}
		for i := range acks {
			if !answered[i] {
				acks[i].noneYet(bound)
			}
		}
		time.Sleep(workerAckPollDefault) //stylegate:allow-sleep bounded wait for a foreground worker to answer or finish the in-flight round
	}
}

// workerForegroundAlive filters pids down to the live ones.
func workerForegroundAlive(pids []int) []int {
	var out []int
	for _, pid := range pids {
		if tools.PidAlive(pid) {
			out = append(out, pid)
		}
	}
	return out
}

// workerPIDList renders a pid set for a one-line message ("pid 12, 34").
func workerPIDList(pids []int) string {
	if len(pids) == 1 {
		return "pid " + strconv.Itoa(pids[0])
	}
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	return "pids " + strings.Join(parts, ", ")
}

// workerForegroundWord names the request in progress messages.
func workerForegroundWord(force bool) string {
	if force {
		return workerForegroundForceWord
	}
	return workerForegroundSignalWord
}
