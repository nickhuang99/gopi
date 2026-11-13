package tools

// The job-notice WAKE: a LEVEL over `theJobs.notices` plus an EDGE channel a
// parked reader can block on. Epic 01a0eeea (flagship 5).
//
// WHY THE PRIMITIVE EXISTS. The inbox itself is already there and already has
// exactly ONE take point — `TakeJobNotices`, drained at the chat turn's round
// boundary (`internal/agent/chat_ops.go`, "BEFORE the request is built"). What
// was missing is a way for a reader that is BLOCKED at an idle prompt to learn
// that the inbox became non-empty. That is a LEVEL question ("is there a
// pending notice?") and it is answered by the inbox itself; the channel is only
// the EDGE that breaks a parked `select`, and it carries no payload — carrying
// one would be a second take point, and a taken notice can no longer be
// injected by the round boundary that owns the take.
//
// WHY THE CHANNEL IS CAPPED AND NON-BLOCKING. The producer is the job reaper
// (a goroutine that must never park) and the consumers are however many parked
// readers there are — possibly none at all. So the send is
// `select { case ch <- struct{}{}: default: }`: a full buffer means a wake is
// ALREADY pending, and a wake is idempotent because the level is what carries
// the meaning. A blocking send would deadlock the reaper with no reader; an
// unbounded buffer would grow per completed job.
//
// WHY A PACKAGE-LEVEL CHANNEL IS NOT A DEPENDENCY (Rule 35): it exists only
// inside this process, is created by this package's own initialisation, is
// never written to disk and needs no cleanup. Nothing outside the process can
// observe it, and no lifecycle has to be owned.
//
// THE BOUNDED-STALENESS CONTRACT (the one honest window this design has): a
// token may be consumed while the level is false — a reader checks the level
// and a round boundary drains the inbox before the reader runs its check. That
// is why EVERY consumer must re-check `JobNoticePending` after waking and fall
// back to waiting when it is false (never mint a turn from a stale token), and
// why the reader must also re-check the level on the way IN (a notice that
// arrived before the reader parked must mint a turn immediately, not wait for
// the next edge).

// jobNoticeWakeBuf is the wake channel's buffer. ONE, and the value is the
// design: a single pending token already means "the level may have gone true",
// and the level is what the reader consults — so a second token would carry no
// information the first did not.
const jobNoticeWakeBuf = 1

// jobNoticeWake is the EDGE half of the primitive: it is signalled whenever a
// completion notice is appended to the inbox, and it carries no payload.
var jobNoticeWake = make(chan struct{}, jobNoticeWakeBuf)

// NotifyJobNoticeWaiters breaks the park of every reader waiting on
// JobNoticeWake. It is called by the producer (the job reaper, right after the
// notice is appended under the registry lock) and is safe to call from any
// goroutine, with or without a reader. It never blocks and never drops a wake
// that matters: a full buffer IS a pending wake.
func NotifyJobNoticeWaiters() {
	select {
	case jobNoticeWake <- struct{}{}:
	default:
	}
}

// JobNoticeWake returns the channel a parked reader selects on, alongside its
// own input. Receiving from it means "the level may have become true" — the
// level itself is JobNoticePending, and a receiver that skips that check will
// mint a turn out of a wake that no longer has anything behind it.
func JobNoticeWake() <-chan struct{} { return jobNoticeWake }

// JobNoticePending reports whether the inbox holds at least one undrained
// completion notice. This is the LEVEL half: it is the ONLY question a reader
// needs answered to decide whether a turn is owed, and it is answered from the
// same slice TakeJobNotices drains — so "the level is true" and "a take will
// return something" cannot disagree.
func JobNoticePending() bool {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	return len(theJobs.notices) > 0
}

// PeekJobNotices returns a COPY of the pending notices WITHOUT draining them.
// A reader that is about to open a turn needs to say WHAT it opened the turn
// for, and it must be able to say it without becoming a second take point: the
// take stays where it is (the turn's round boundary), so the very notice this
// peek described is still in the inbox for that boundary to inject.
//
// The copy matters: the returned slice is the caller's, and a caller mutating
// it must not be able to edit the registry (or race a concurrent append).
func PeekJobNotices() []JobNotice {
	theJobs.mu.Lock()
	defer theJobs.mu.Unlock()
	out := make([]JobNotice, len(theJobs.notices))
	copy(out, theJobs.notices)
	return out
}
