// cli_worker_ack.go — the REVERSE channel of the durable stop contract: the
// worker's own receipt for a request it has CONSUMED.
//
// THE DEFECT THIS CLOSES. `gopi worker --stop --pid N` had ONE direction. The
// sender wrote the durable request (TERM_<pid>) and then BLIND-WAITED: the whole
// 10m foreground / 25m daemon bound if the worker was busy, with no way to tell
// "the worker read your request and is leaving at the boundary" from "the worker
// never saw it". The code said so itself, in the sender's own note: the stop
// still lands, it just cannot report the sender. An operator therefore could not
// tell a healthy drain from a lost request without waiting the bound out — and a
// script (a unit's ExecStop, a restart) had no signal at all.
//
// WHY A FILE, NOT A SIGNAL. A signal carries no reply: Linux hands a signal
// handler no sender identity and gives the receiver no channel back — that fact
// is the premise of internal/signalrec and of the request file itself. A receipt
// FILE has neither limitation, and it has two properties the acceptance turns
// on: it needs no shared binary (so a worker whose /proc/<pid>/exe reads
// `(deleted)` — every rebuild — or a DIFFERENT inode still answers, where a
// signal contract could not be verified), and it OUTLIVES the process that wrote
// it (so a receipt written an instant before the worker exits is still
// observable by the sender).
//
// THE SHAPE, in one line: request TERM_<pid> goes operator→worker, receipt
// ACK_<pid> comes back worker→operator, both in the SAME control directory, both
// key=value bodies in the same vocabulary (workerAckSep IS workerStopFieldSep).
//
// WHAT IT DOES NOT CHANGE (backward compatibility, both directions):
//   - a worker that writes no receipt keeps the sender's old wait unchanged: it
//     still polls for the process to exit on the operator's own bound — and it
//     now SAYS what it observed (no receipt inside the bound, the request still
//     standing) instead of waiting in silence (Rule 15). It does NOT guess a
//     cause: an absent receipt has several, and this sender can see none of them
//     (bug 01a17df2 measured the old "older binary" attribution to be false);
//   - an old SENDER is unaffected: the new worker still reads the same request
//     file, and an unrecognised ACK_ file is read by nobody.
//
// WRITTEN ONCE, IDEMPOTENTLY. The receipt is written on the FRESH transition
// only (the consume that establishes the drain/force), and even then only when
// no receipt for the same sender is standing — so the three consumers of a
// request (the loop's boundary, the 2s watcher, the forwarder) produce exactly
// one file, and none of them re-nudges anything. The 2026-10-15 self-waking hot
// loop (514 ticks of CPU in 5s) came from a wake-up that re-armed itself; this
// file writes a receipt and wakes nothing.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// workerAck is one parsed receipt. Its zero value means "no receipt": only a
// body that names the requester it answers is accepted, so a half-written or
// foreign file can never be read as an answer (Rule 15).
type workerAck struct {
	// ReqID / ReqTS name WHICH request this answers: the sender's pid and the
	// instant it wrote the request. They are what make the file self-describing
	// — a receipt for someone else's request is ignored, never printed.
	ReqID int
	ReqTS string
	// AckedAt is when the WORKER consumed the request, AckedBy its pid.
	AckedAt string
	AckedBy int
	// Mode says which request shape was read (workerAckModeDrain / Force).
	Mode string
	// Round is the claim-loop round the receipt was written at (0 when the
	// worker has no state file to count rounds in — the foreground face).
	Round int
	// Exit is the worker's own statement of what happens next, in words the
	// operator can act on ("exit at the next round boundary …").
	Exit string
	// Path / Raw are the file it came from and its bytes — so the printed
	// receipt is checkable independently of this process's word.
	Path string
	Raw  string
}

// workerAckPathText is the receipt's path for pid under dir. ONE renderer
// (Rule 2): the writer and the polling reader cannot disagree about the name.
func workerAckPathText(dir string, pid int) string {
	return filepath.Join(dir, workerAckPrefix+strconv.Itoa(pid))
}

// parseWorkerAck decodes a receipt body. Parsing NEVER fails: a malformed body
// degrades to the zero workerAck (ReqID 0), which no sender accepts as an answer
// to its own request.
func parseWorkerAck(body []byte, path string) workerAck {
	a := workerAck{Path: path, Raw: strings.TrimSpace(string(body))}
	for _, line := range strings.Split(string(body), "\n") {
		key, val, ok := strings.Cut(line, workerAckSep)
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if key == "" || val == "" {
			continue
		}
		switch key {
		case workerAckFieldReqID:
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				a.ReqID = n
			}
		case workerAckFieldReqTS:
			a.ReqTS = val
		case workerAckFieldAckedAt:
			a.AckedAt = val
		case workerAckFieldAckedBy:
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				a.AckedBy = n
			}
		case workerAckFieldMode:
			a.Mode = val
		case workerAckFieldRound:
			if n, err := strconv.Atoi(val); err == nil {
				a.Round = n
			}
		case workerAckFieldExit:
			a.Exit = val
		}
	}
	return a
}

// workerAckAnswers reports whether this receipt answers the request written by
// reqID — the ONE acceptance test, shared by the writer's idempotency check and
// the sender's poll, so "already answered" cannot mean two different things.
func (a workerAck) workerAckAnswers(reqID int) bool {
	return reqID > 0 && a.ReqID == reqID
}

// workerWriteStopAck writes the receipt for pid, ONCE per request: a receipt
// already standing for the same sender is left untouched (no rewrite, no
// re-publish — the property that keeps the three request consumers from
// producing three files), and the file is installed through the shared durable
// writer (write-to-tmp + rename), so the sender never reads a half body.
//
// THE EXIT SENTENCE IS AN ARGUMENT, not this file's opinion (bug 01a17df2). The
// receipt's job is to tell the requester what the recipient WILL DO, and that
// answer differs per face: a claim-loop worker exits at its next round boundary,
// while `gopi worker --resume` runs exactly one resumed task and has no next
// boundary to reach. Writing the claim-loop sentence for a resume would be a
// receipt that lies about the one thing it exists to state — so the caller names
// it, and the two call sites use their own constant.
func workerWriteStopAck(dir string, pid int, mode string, round int, sender workerStopSender) error {
	return workerWriteStopAckWithExit(dir, pid, mode, round, sender, workerAckExitSemantic)
}

// workerWriteStopAckWithExit is workerWriteStopAck with the exit sentence named
// by the face that is answering (see its comment).
func workerWriteStopAckWithExit(dir string, pid int, mode string, round int, sender workerStopSender, exit string) error {
	path := workerAckPathText(dir, pid)
	if prev, err := os.ReadFile(path); err == nil {
		if parseWorkerAck(prev, path).workerAckAnswers(sender.PID) {
			return nil // this request is already answered — write nothing
		}
	}
	var b strings.Builder
	writeField := func(key, val string) {
		if val == "" {
			return
		}
		b.WriteString(key)
		b.WriteString(workerAckSep)
		b.WriteString(val)
		b.WriteString("\n")
	}
	writeField(workerAckFieldReqID, strconv.Itoa(sender.PID))
	writeField(workerAckFieldReqTS, sender.TS)
	writeField(workerAckFieldAckedAt, time.Now().Format(time.RFC3339))
	writeField(workerAckFieldAckedBy, strconv.Itoa(pid))
	writeField(workerAckFieldMode, mode)
	if round > 0 {
		writeField(workerAckFieldRound, strconv.Itoa(round))
	}
	writeField(workerAckFieldExit, exit)
	return tools.WriteControlText(path, []byte(b.String()))
}

// --- the WORKER side: publishing the receipt -------------------------------

// publishStopAck writes the receipt for a request this process has just
// CONSUMED. It is called on the fresh transition only (see the file header), and
// a failure is REPORTED rather than swallowed: a receipt that could not be
// written is exactly the case the sender will experience as a silent wait, so
// the worker says so where an operator can see it (Rule 15b).
func (c workerDaemonCtx) publishStopAck(mode string, sender workerStopSender) {
	if !c.consumesRequests() {
		return
	}
	round := 0
	if c.writer != nil {
		round = c.writer.rounds()
	}
	if err := workerWriteStopAckWithExit(c.dir, c.pid, mode, round, sender, c.ackExitSentence()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: could not write the stop receipt for pid %d: %v (the requester will keep waiting for the process to exit)\n",
			workerProgTopLevel, c.pid, err)
	}
}

// ackExitSentence is the exit sentence THIS face writes into its receipt: the
// sentence its constructor declared, or the claim-loop one for the contexts that
// declared nothing (the daemon and the plain foreground worker, which both stop
// at a round boundary). One reader, so a new face cannot forget to declare and
// silently promise a boundary it does not have.
func (c workerDaemonCtx) ackExitSentence() string {
	if c.ackExit != "" {
		return c.ackExit
	}
	return workerAckExitSemantic
}

// workerAckSenderFromForceRequest reads the sender header of pid's standing
// FORCE request — the force arm's mirror of workerStopSenderFromRequest, so both
// request shapes answer with the identity that asked (Rule 14: one vocabulary).
func workerAckSenderFromForceRequest(dir string, pid int) workerStopSender {
	body, err := os.ReadFile(filepath.Join(dir, tools.ForceRequestPrefix+strconv.Itoa(pid)))
	if err != nil {
		return workerStopSender{}
	}
	return parseWorkerStopSender(body)
}

// --- the SENDER side: polling for the receipt ------------------------------

// workerAckPoll is the sender's receipt reader, shared by BOTH waiting faces
// (the daemon's workerWaitForExit and the foreground's workerForegroundWaitExit)
// so the two cannot describe the same answer differently.
//
// A nil *workerAckPoll is inert by contract: the arms that have no control
// directory (the signal-only case) construct nothing and call nothing.
type workerAckPoll struct {
	dir string
	pid int
	// reqID is THIS process, the sender the receipt must name. A receipt for
	// another operator's request is not this command's answer.
	reqID int
	start time.Time
	// noted latches the one "does not acknowledge" line (Rule 15: said once,
	// and said at all).
	noted bool
}

// newWorkerAckPoll arms the poll for pid under dir.
func newWorkerAckPoll(dir string, pid int) *workerAckPoll {
	return &workerAckPoll{dir: dir, pid: pid, reqID: os.Getpid(), start: time.Now()}
}

// seen reads the standing receipt and accepts it only when it answers THIS
// process's request (workerAckAnswersRequest). An unreadable or foreign file is
// "not yet" — never an error, because the poll runs inside a wait whose other
// facts (liveness, the bound) stand on their own.
func (p *workerAckPoll) seen() (workerAck, bool) {
	if p == nil || p.dir == "" {
		return workerAck{}, false
	}
	path := workerAckPathText(p.dir, p.pid)
	body, err := os.ReadFile(path)
	if err != nil {
		return workerAck{}, false
	}
	a := parseWorkerAck(body, path)
	if !workerAckAnswersRequest(a, p.reqID, p.start) {
		return workerAck{}, false
	}
	return a, true
}

// print renders the receipt the way the operator asked for it: the raw body
// first (checkable, verbatim), then the two sentences that make it actionable.
// It prints, it does not decide — the caller owns the return value.
func (p *workerAckPoll) print(a workerAck) {
	mode := a.Mode
	if mode == "" {
		mode = "stop"
	}
	fmt.Printf("%s: pid %d ACKNOWLEDGED the %s request — receipt %s\n",
		workerProgTopLevel, p.pid, mode, a.Path)
	fmt.Printf("%s:   %s\n", workerProgTopLevel, strings.ReplaceAll(a.Raw, "\n", " | "))
	fmt.Printf("%s:   requested by pid %d at %s; read by worker pid %d at %s; %s\n",
		workerProgTopLevel, a.ReqID, workerAckOrUnknown(a.ReqTS), a.AckedBy, workerAckOrUnknown(a.AckedAt), a.Exit)
	fmt.Printf("%s: stopped waiting — the request is ANSWERED; the worker is still finishing its in-flight round and exits at its next boundary\n",
		workerProgTopLevel)
}

// workerAckArchivedPath is the archived name of a LIVE receipt path:
// "<ACK_ name><sep><req_id><suffix>". ONE renderer (Rule 2) — the archiver and
// the retention rule below both compute the name from it, so a receipt the
// sweep cannot parse the name of is a receipt the archiver would not have
// written.
func workerAckArchivedPath(live string, reqID int) string {
	dir, base := filepath.Split(live)
	return filepath.Join(dir, base+workerAckArchivedSep+strconv.Itoa(reqID)+workerAckArchivedSuffix)
}

// workerAckArchivedReqID recovers the req_id an ARCHIVED name carries (0 when
// the name is not an archived receipt name). It is the ONE decoder for the
// encoder above, and it is what lets the retention rule keep working after the
// receipt body is gone.
func workerAckArchivedReqID(name string) int {
	if !strings.HasSuffix(name, workerAckArchivedSuffix) {
		return 0
	}
	rest := strings.TrimSuffix(name, workerAckArchivedSuffix)
	sep := strings.LastIndex(rest, workerAckArchivedSep)
	if sep < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[sep+len(workerAckArchivedSep):])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// workerAckIsLiveName reports whether name is a receipt the POLL may read: the
// answered-receipt namespace and NOT the archived one. Every reader of the
// channel goes through this predicate, so "archived" cannot mean two things.
func workerAckIsLiveName(name string) bool {
	return strings.HasPrefix(name, workerAckPrefix) && workerAckArchivedReqID(name) == 0
}

// ackedAtOf is the receipt's own statement of when it was consumed — the clock
// every retention decision uses. An unparsable stamp falls back to the file's
// mtime rather than to "now": "we could not read the stamp" must not be the
// same answer as "it was just written" (Rule 15).
func (a workerAck) ackedAtOf(modTime time.Time) time.Time {
	if ts, err := time.Parse(time.RFC3339, a.AckedAt); err == nil {
		return ts
	}
	return modTime
}

// ackRetentionElapsed reports whether an archived receipt or an unread live one
// is old enough for the resident sweep to collect it. ONE predicate for both
// (§2 of the delivery: the crash path and the normal path share the rule).
//
// A live receipt is judged by acked_at (its body), an ARCHIVED one by the same
// clock — the body is still there until the file is deleted, and the fallback
// for an unparsable body is the mtime of a rename, which preserves the author's
// write instant.
func ackRetentionElapsed(a workerAck, modTime time.Time, live bool, now time.Time) bool {
	age := workerAckArchiveRetention
	if live {
		age = workerAckOrphanStaleAge
	}
	return now.Sub(a.ackedAtOf(modTime)) > age
}

// consume ARCHIVES the receipt this command has just read — it does NOT delete
// it. See workerAckArchivedSep for why: the printed receipt is the operator's
// convenience, the file is the evidence, and evidence is not destroyed by the
// act of reading it.
//
// The move is a rename within one directory (atomic), so the live name is free
// at once: a later request from the same sender cannot read this answer as its
// own. A failure is reported loudly and in full (Rule 15): the receipt is left
// WHERE IT IS, the operator is told, and the resident sweep's orphan arm still
// has a standable file to archive later.
func (p *workerAckPoll) consume(a workerAck) {
	if p == nil || a.Path == "" {
		return
	}
	if _, err := archiveWorkerAck(a); err != nil {
		fmt.Fprintf(os.Stderr, "%s: could not archive the read receipt %s: %v (it is left where it is; the resident sweep archives an unread one by age)\n",
			workerProgTopLevel, a.Path, err)
	}
}

// archiveWorkerAck moves the read receipt a out of the LIVE namespace and
// records one line about it. It returns the archived path.
//
// WHY IT APPENDS AN INDEX LINE AS WELL AS KEEPING THE FILE: the file is the
// verbatim evidence and it is collected after workerAckArchiveRetention; the
// line is the durable FACT ("pid X acknowledged the drain request from pid Y at
// T") and it costs one append. A host that stops waking the sweep still has the
// file; a reader months later still has the line.
//
// A receipt BODY THAT CANNOT BE READ is still archived — the file is evidence
// whether or not this code can parse it (an older or a newer writer), so the
// move never depends on the parse succeeding.
func archiveWorkerAck(a workerAck) (string, error) {
	dst := workerAckArchivedPath(a.Path, a.ReqID)
	if err := os.Rename(a.Path, dst); err != nil {
		return "", fmt.Errorf("archive receipt %s → %s: %w", a.Path, dst, err)
	}
	logPath := filepath.Join(filepath.Dir(dst), workerAckArchiveLogName)
	line := strings.Join([]string{
		strconv.FormatInt(time.Now().Unix(), 10),
		strconv.Itoa(a.ReqID),
		strconv.Itoa(a.AckedBy),
		a.AckedAt,
		a.Mode,
		a.Exit,
		filepath.Base(dst),
	}, workerAckArchiveLogFieldSep) + "\n"
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, tools.ControlFileMode)
	if err != nil {
		// The FILE was archived: the fact is not lost, only its index line. Say
		// which half survived rather than reporting a bare failure.
		return dst, fmt.Errorf("receipt archived to %s, but the index line could not be appended to %s: %w", dst, logPath, err)
	}
	_, werr := f.WriteString(line)
	cerr := f.Close()
	if werr != nil {
		return dst, fmt.Errorf("receipt archived to %s, but writing the index line %s: %w", dst, logPath, werr)
	}
	if cerr != nil {
		return dst, fmt.Errorf("receipt archived to %s, but closing the index %s: %w", dst, logPath, cerr)
	}
	return dst, nil
}

// sweepWorkerAckArchive is the RECLAIM leg, and it rides an existing tick
// (scratchGCTick — Rule 35: no new scheduler, no second cadence, no second off
// switch). One pass, two ages, ONE rule:
//
//	NORMAL PATH  an ARCHIVED receipt (".<req_id>.consumed") older than
//	             workerAckArchiveRetention is deleted.
//	CRASH PATH   a LIVE receipt the sender never read — its requester died, so
//	             nobody will archive it — older than workerAckOrphanStaleAge is
//	             archived first (evidence kept) and then collected by the same
//	             rule above on a later pass. Archiving, never deleting, is what
//	             keeps the two paths one rule: a receipt the sweep finds is
//	             archived whether or not anybody read it.
//
// An unreadable entry, a failed delete and a failed archive are each REPORTED
// and the pass continues (fail-open, like every other arm of that tick: disk
// hygiene must never break the host task), but never silently — the caller is
// told which path and which reason (Rule 15).
func sweepWorkerAckArchive(dir string, now time.Time) (archived, removed int, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil // no control directory yet: nothing to reclaim
		}
		return 0, 0, []error{fmt.Errorf("read %s: %w", dir, err)}
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		archivedReqID := workerAckArchivedReqID(name)
		live := workerAckIsLiveName(name)
		if archivedReqID == 0 && !live {
			continue // not this channel's file
		}
		path := filepath.Join(dir, name)
		fi, ierr := e.Info()
		if ierr != nil {
			errs = append(errs, fmt.Errorf("stat receipt %s: %w", path, ierr))
			continue
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil && !os.IsNotExist(rerr) {
			errs = append(errs, fmt.Errorf("read receipt %s: %w", path, rerr))
			continue
		}
		a := parseWorkerAck(body, path)
		if !ackRetentionElapsed(a, fi.ModTime(), live, now) {
			continue
		}
		// AN ARCHIVED name whose body lost its req_id keeps the name's own id, so
		// the index line still says WHICH request the file answers.
		if a.ReqID == 0 {
			a.ReqID = archivedReqID
		}
		if live {
			// Crash path: the receipt is evidence first. It is archived now and
			// collected by the age rule on its own later pass.
			if _, aerr := archiveWorkerAck(workerAck{Path: path, ReqID: a.ReqID, AckedBy: a.AckedBy, AckedAt: a.AckedAt, Mode: a.Mode, Exit: a.Exit}); aerr != nil {
				errs = append(errs, aerr)
				continue
			}
			archived++
			continue
		}
		if rmerr := os.Remove(path); rmerr != nil && !os.IsNotExist(rmerr) {
			errs = append(errs, fmt.Errorf("collect archived receipt %s: %w", path, rmerr))
			continue
		}
		removed++
	}
	return archived, removed, errs
}

// sweepWorkerAckArchiveQuietly is the tick's one-line call site: it reports what
// it did (nothing at all when nothing was due, so a clean tick stays silent) and
// NEVER fails its caller.
func sweepWorkerAckArchiveQuietly(role string, now time.Time) {
	dir, derr := tools.ControlDirReadOnly(workerStateDirName)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "[%s] [%s] stop-receipt sweep skipped (host task unaffected): resolve the worker control dir: %v\n",
			role, scratchGCRoleLabel, derr)
		return
	}
	archived, removed, errs := sweepWorkerAckArchive(dir, now)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "[%s] [%s] %v (host task unaffected)\n", role, scratchGCRoleLabel, e)
	}
	if archived > 0 || removed > 0 {
		// The directory must not CREATE a control dir on a host that never ran a
		// worker: ControlDirReadOnly is the reader's half, and a hygiene pass
		// that materialised one would be a side effect of sweeping nothing.
		fmt.Printf("[%s] [%s] stop receipts: archived %d unread, collected %d aged from %s\n",
			role, scratchGCRoleLabel, archived, removed, dir)
	}
}

// noneYet reports, ONCE and mid-wait, that no receipt has arrived yet — said so
// the operator is not left wondering, and said as "yet" so it cannot be read as
// a verdict about the worker (the round it must reach may simply not have ended;
// the prologue is a measurement of the host — measured 2026-10-03: 3-8s at load
// 4-10, >20s at load 15.9).
//
// IT NAMES NO CAUSE (bug 01a17df2). This function used to add "a worker from an
// OLDER binary never writes one, so this may be the unchanged timeout path" —
// a GUESS, and one that was measured false: the pid this line was printed about
// had been started 33 minutes earlier than the pids that DID answer, so its age
// was not the discriminator. An absent receipt has several possible causes
// (a process outside a claim loop — `--resume` is one; a worker predating the
// receipt; a boundary that had already passed), and THIS command can observe
// none of them: it holds a pid, a request file and a deadline. So it states
// those three facts and attributes nothing.
func (p *workerAckPoll) noneYet(bound time.Duration) {
	if p == nil || p.noted {
		return
	}
	if time.Since(p.start) < workerAckForeignGrace {
		return
	}
	p.noted = true
	fmt.Fprintf(os.Stderr, "%s: pid %d has written no receipt yet at %s (%s into a %s bound); the request is on disk at %s and stands until the pid acts on it\n",
		workerProgTopLevel, p.pid, p.path(), time.Since(p.start).Round(time.Millisecond), bound, p.requestPath())
}

// missing states the verdict at a wait's own exit point: no receipt was observed
// for this request inside the bound. It is a statement about what THIS command
// measured — the receipt's absence — and about the fact that survives it (the
// request file is still there, and is still the delivery).
//
// IT DOES NOT DIAGNOSE (bug 01a17df2). The predecessor sentence attributed the
// silence to "a worker from an older binary", which is a guess about a process
// this command never inspected the age of; it also framed the outcome as "the
// old timeout behaviour", which quietly asserted the channel had not been used.
// Neither is observable from here. What an operator can act on is named instead.
func (p *workerAckPoll) missing(why string) {
	if p == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: no stop receipt from pid %d inside the wait (would have been %s) — the request is DELIVERED and still standing at %s; the pid has not been observed reading it, and this command cannot see why: %s (%s)\n",
		workerProgTopLevel, p.pid, p.path(), p.requestPath(), workerAckUnobservedCause, why)
}

// workerAckUnobservedCause names, in one place, what this command is NOT able to
// conclude — so no renderer of the receipt channel can drift into inventing one
// (Rule 1b, and Rule 15: the boundary of a measurement is stated, not crossed).
// The list is the observed set: a process that never enters a claim loop
// (`gopi worker --resume` runs one task and takes no rounds, so it has no
// boundary at which the request is read), a worker whose binary predates the
// receipt, and a boundary that had already gone by when the request landed.
const workerAckUnobservedCause = "a process outside a claim loop, a worker predating the receipt, and a boundary already passed all produce this same silence"

// workerAckExitResume is the exit sentence of a receipt written by the RESUME
// face (bug 01a17df2). That face runs exactly one resumed task in a child and
// then closes out; it takes no claim rounds, so it has no "next round boundary"
// to promise, and reusing the claim-loop sentence would be a receipt that
// misstates the one fact it exists to carry. What it CAN state is exactly what
// it does: the request was read, and the process leaves when the resumed task
// returns — the in-flight task is not interrupted, which is the drain guarantee
// the stop channel makes to every face.
const workerAckExitResume = "this process runs ONE resumed task and takes no claim rounds; the request is read, and it exits when the resumed task returns (the in-flight task is not interrupted)"

// requestPath names the request file this poll's receipt would answer — the
// durable fact that does not depend on the target's executable. It is derived
// from the SAME directory as the receipt (one resolution, Rule 2).
func (p *workerAckPoll) requestPath() string {
	if p == nil || p.dir == "" {
		return "no control directory"
	}
	// The ONE renderer of that path (cli_worker_foreground.go): the sentence the
	// sender prints and the file the ask wrote cannot disagree about the name.
	return workerRequestPathText(p.dir, p.pid, false)
}

// path names the receipt this poll reads ("" when no directory was resolved —
// rendered by the caller rather than guessed).
func (p *workerAckPoll) path() string {
	if p == nil || p.dir == "" {
		return "no control directory"
	}
	return workerAckPathText(p.dir, p.pid)
}

// workerAckOrUnknown renders a receipt timestamp that is missing as UNKNOWN
// rather than as an empty slot (Rule 15: an absent fact is stated, never
// silently dropped).
func workerAckOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown (written by a sender that predates the receipt)"
	}
	return s
}
