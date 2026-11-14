package testutil

// WHO THE LOCK'S HOLDER IS, IN TERMS OF OWNERSHIP — the fact a refusal has to
// teach before anybody reaches for a signal.
//
// THE DEFECT THIS CLOSES (bug row 01a1d278, operator's own report). The refusal
// used to end with
//
//	FIX: wait for the holder above to finish; or stop that run deliberately; or …
//
// — and "stop that run deliberately" names an ACTION without naming whose
// PROCESS it is. Measured on this host, in the operator's words: a foreground
// `make test-budget` was killed by the bash tool's idle net, scripts/test-budget.sh's
// `setsid`-detached `go test` survived (that escape is Rule 42's containment and
// is deliberately NOT removed), and the next run of that package was refused with
// a holder pid the operator had NEVER STARTED. The refusal then invited a kill on
// that number. Rule 42 forbids exactly that signal: nothing may address a process
// it did not create and does not hold, and a pid read out of a lock record is a
// NUMBER — the kernel recycles pid numbers, so the number names whatever process
// holds it now.
//
// SO THE REFUSAL NOW CARRIES A MEASUREMENT AND A CHECK, not an invitation:
//
//	1. the holder's SESSION, compared with this run's own session — the one fact
//	   that separates "a process your own run started" (signalling it is your
//	   call, and it is inside your tree) from "a process that is not in your
//	   session at all" (signalling it by number would be a cross-ownership kill);
//	2. the holder's START TICKS — the incarnation, so a recycled pid cannot be
//	   mistaken for the recorded process;
//	3. the holder's CWD, which is the tree an operator recognises.
//
// Every one of those is /proc evidence (Rule 24), read at refusal time. A field
// that cannot be read is NAMED as unmeasured and never spent as a verdict
// (Rule 15a: an unknown is not a "no", and it is certainly not a "yes, kill it").

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// suiteHolderOwnershipSameSession: the holder is a member of THIS process's
	// session, i.e. a process this run (or its launcher tree) started.
	suiteHolderOwnershipSameSession = "same-session"
	// suiteHolderOwnershipOtherSession: the holder lives in a DIFFERENT session.
	// This is the shape the measured defect produced — a `setsid`-detached
	// survivor — and it is the one the refusal must not invite a signal against.
	suiteHolderOwnershipOtherSession = "other-session"
	// suiteHolderOwnershipUnmeasured: /proc would not answer. Never upgraded into
	// either of the two verdicts above (Rule 15a).
	suiteHolderOwnershipUnmeasured = "unmeasured"
)

// suiteHolderOwnership is the measured verdict plus the sentence a reader acts
// on. It is one struct so the two can never be rendered apart.
type suiteHolderOwnership struct {
	Verdict string
	Pid     int
	Start   string
	Session int
	SelfSid int
	Cwd     string
}

// suitePackageHolderOwnership measures the recorded holder against THIS process.
// busy.HolderPid is the recorded pid (0 when the record states none), and a pid
// that is gone or unreadable yields the unmeasured verdict rather than a guess.
func suitePackageHolderOwnership(holderPid int) suiteHolderOwnership {
	own := suiteHolderOwnership{Verdict: suiteHolderOwnershipUnmeasured, Pid: holderPid}
	if sid, ok := procname.ProcSessionKey(procname.DefaultProcRoot, strconv.Itoa(os.Getpid())); ok {
		own.SelfSid = sid
	}
	if holderPid <= 0 {
		return own
	}
	pidStr := strconv.Itoa(holderPid)
	if ticks, ok := procname.StartTicks(procname.DefaultProcRoot, pidStr); ok {
		own.Start = ticks
	}
	if target, err := os.Readlink(filepath.Join(procname.DefaultProcRoot, pidStr, procname.CwdName)); err == nil {
		own.Cwd = target
	}
	sid, ok := procname.ProcSessionKey(procname.DefaultProcRoot, pidStr)
	if !ok || own.SelfSid == 0 {
		// Either side unreadable: this is exactly the state in which a reader
		// must NOT be told "it is yours" (Rule 15a).
		return own
	}
	own.Session = sid
	own.Verdict = suiteHolderOwnershipOtherSession
	if sid == own.SelfSid {
		own.Verdict = suiteHolderOwnershipSameSession
	}
	return own
}

// render is the sentence the refusal prints. It NEVER prescribes a signal: each
// branch names what was measured and what that measurement does and does not
// license (Rule 42).
func (o suiteHolderOwnership) render() string {
	ticks := o.Start
	if ticks == "" {
		ticks = "(unmeasured — /proc would not answer for that pid)"
	}
	cwd := o.Cwd
	if cwd == "" {
		cwd = "(unmeasured)"
	}
	switch o.Verdict {
	case suiteHolderOwnershipSameSession:
		return fmt.Sprintf(
			"  ownership: pid %d startticks %s is in THIS process's session (sid %d) — it is a process this run's own tree started, "+
				"so a signal aimed at it stays inside your own tree. directory: %s\n",
			o.Pid, ticks, o.SelfSid, cwd)
	case suiteHolderOwnershipOtherSession:
		return fmt.Sprintf(
			"  ownership: pid %d startticks %s is in session %d, NOT in this process's session (sid %d) — so it is a process "+
				"that left the tree that started it (a `setsid`-detached child, a daemon, or another run's) and you do not hold it.\n"+
				"             directory: %s\n"+
				"             the pid above is a RECYCLABLE NUMBER: /proc can hand it to an unrelated process the moment that one exits. "+
				"Treat the startticks value as the identity, and do not signal the number (Rule 42: nothing signals what it does not create and hold).\n",
			o.Pid, ticks, o.Session, o.SelfSid, cwd)
	}
	return fmt.Sprintf(
		"  ownership: UNMEASURED for pid %d (startticks %s, directory %s) — this process or the holder could not be read from /proc "+
			"(restricted procfs, or the holder exited between the record and this read). An unmeasured holder is never evidence that it is yours, "+
			"and it is never evidence that it is gone.\n",
		o.Pid, ticks, cwd)
}
