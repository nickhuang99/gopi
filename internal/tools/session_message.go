package tools

// session_message.go — the INBOUND side of `gopi notify`: an external process
// hands one message to a session it does not own.
//
// THE SEMANTIC, and why the verb is `notify` and not a new one. `gopi notify`
// has always been gopi as the SUBJECT: gopi tells somebody else that something
// happened (internal/tools/tasknotify.go delivers each lifecycle event to
// notify.cmd / a webhook). `gopi notify --session <id> --message <text>` keeps
// that direction — gopi tells THAT SESSION it has a message — so the name does
// not change; only the object does (an external sink → a session's own inbox).
//
// THE THREE INVARIANTS THIS FILE OBEYS, each with its reason:
//
//  1. ONE TAKE POINT. An external message is NEVER a second queue. It is merged
//     into `theJobs.notices` (job.go) and leaves through the SAME `TakeJobNotices`
//     the background-job notices use — so the round boundary that drains the
//     inbox, and every counting/peek surface built on it, cannot disagree about
//     what is pending (job_notice_wake.go states the same contract for the wake).
//
//  2. ONE WRITER, ONE READER, ONE FILE. The durable half is a single
//     append-only file inside the session's own directory — no database, no
//     index, no second store (Rule 35). The writer is another PROCESS (the
//     session may not be running at all); the reader is the session's own
//     process, at its round boundary.
//
//  3. JSON STREAM DECODER, NEVER A LINE LOOP. The file is read with
//     json.Decoder over a reader, so a record carrying an escaped newline is
//     ONE record (the same defect the notify sink had before e2b7e379 — a
//     line-oriented reader returns a whole record per fragment, or a fragment
//     per record).
//
// WHY APPEND-ONLY, WITH THE MARKER INSIDE THE SAME FILE. A message that has been
// delivered to the model must not be delivered again (the session's history
// already carries it — a resumed session replays that history, so a re-injection
// would be a duplicate instruction, which is worse than noise: an instruction
// like "run the tests" would run twice). That state has to survive the process
// that read it, so it has to be on disk. It is expressed as a SECOND RECORD KIND
// APPENDED TO THE SAME FILE rather than as an edit of the message's own line:
// a rewrite would need either truncate-in-place (a crash mid-rewrite corrupts
// everything already in the inbox) or write-then-rename (which invalidates every
// concurrent appender's descriptor, so two processes can hold the lock on two
// different inodes and lose an update). Appending has neither failure mode, and
// a torn last record is tolerated: the decoder stops at the first undecodable
// object and reports it (Rule 15 — it is named, never silently dropped).

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// The inbox's names and numbers (Rule 1: every literal is a named constant).
const (
	// sessionInboxDirName is the inbox directory inside a session dir: the
	// value comes from the layout package, which owns the shape of a sessions
	// tree (Rule 14 — never re-spelled here).
	sessionInboxDirName = sessionlayout.SessionMessagesDirName
	// sessionInboxFileName is the append-only inbox itself.
	sessionInboxFileName = "inbox.jsonl"
	// sessionMessageRecordMessage / ...Delivered are the TWO record kinds the
	// one file holds: the message, and the marker saying it reached the model.
	sessionMessageRecordMessage   = "message"
	sessionMessageRecordDelivered = "delivered"
	// sessionMessageKind is the record's own kind field value for a message
	// that arrived from OUTSIDE the process (as opposed to anything gopi
	// itself produced).
	sessionMessageKind = "external"
	// sessionMessageSource is the provenance stamped on every delivery made by
	// the CLI verb. Its VALUE is not spelled here: internal/inputsource owns the
	// one definition of the message-source vocabulary (Rule 14), and the paste
	// reference a large delivery renders carries the SAME value — two spellings
	// of one face would let a reader disagree with itself.
	sessionMessageSource = inputsource.Notify
	// sessionInboxDirPerm / sessionInboxFilePerm: the inbox lives in the
	// operator's own profile, beside session.jsonl (0644), and is read by the
	// same user only.
	sessionInboxDirPerm  = 0o755
	sessionInboxFilePerm = 0o644
	// sessionInboxReadCap bounds how much of the inbox the READER looks at.
	// The window is the TAIL: the newest records are the interesting ones, and
	// a bounded read keeps a pathological file from being read into memory at
	// a round boundary. A tail read starts mid-record, which the stream decoder
	// absorbs (the first object it cannot decode is skipped, not fatal).
	sessionInboxReadCap = 1 << 20 // 1 MiB
	// sessionInboxRefuseBytes is where the WRITER refuses to append any more.
	// Nothing prunes the inbox automatically (see the file header): a bound
	// that fails LOUDLY and names the fix is the honest alternative to silent
	// unbounded growth (Rules 15/35).
	sessionInboxRefuseBytes = 4 << 20 // 4 MiB
	// sessionInboxTakeBudget bounds how many messages ONE round boundary
	// merges: a delivery loop from another host must not be able to hand the
	// model a thousand instructions in a single round. The remainder stays
	// pending — a later boundary takes the next slice (the level stays true).
	sessionInboxTakeBudget = 16
	// sessionMessageIDHexLen is the hex LENGTH of a delivery id, named once
	// because two producers mint one: sessionMessageID() from 8 random bytes,
	// and NotifyDeliveryID by truncating a content digest. Both must produce the
	// same SHAPE, or a reader would need a rule for "which kind of id is this".
	sessionMessageIDHexLen = 16
)

// ExternalMessage is one inbound message as the AGENT side sees it: the record
// a delivered message becomes when it is merged into the in-process inbox and
// injected into the conversation.
type ExternalMessage struct {
	// ID identifies the delivery (the caller's idempotency anchor: re-injecting
	// the same id is what the delivered marker prevents).
	ID string
	// SessionID is the session the message was addressed to.
	SessionID string
	// Dir is the session directory holding the inbox (evidence path a reader
	// can open without resolving the layout again).
	Dir string
	// Message is the text the external caller asked to be delivered.
	Message string
	// Source names the face that wrote the record.
	Source string
	// CreatedAt is when the delivery was accepted (RFC3339, UTC).
	CreatedAt string
}

// SessionMessageDelivery is what the DELIVER face reports back: where the
// message landed and which id it carries. A caller that wants to say "queued
// message <id>" (or to retry by id) needs both, and deriving them a second time
// at the call site would be a second spelling of the same facts.
type SessionMessageDelivery struct {
	ID        string
	SessionID string
	Dir       string
	Path      string
	CreatedAt string
}

// sessionMessageRecord is the ONE on-disk shape of both record kinds. A single
// struct (with the kind naming which fields carry meaning) keeps the writer and
// the decoder from drifting into two schemas for one file.
type sessionMessageRecord struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message,omitempty"`
	Source    string `json:"source,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// sessionInboxPath returns the inbox file inside a session directory.
func sessionInboxPath(dir string) string {
	return filepath.Join(dir, sessionInboxDirName, sessionInboxFileName)
}

// SessionInboxPath exposes the inbox path for a session directory: a report
// cites it, and a test asserts against it, so the spelling lives in ONE place
// (Rule 13/14).
func SessionInboxPath(dir string) string { return sessionInboxPath(dir) }

// sessionInboxLock opens the inbox for appending and takes its exclusive
// advisory lock, creating the directory and the file when asked to.
//
// The lock is on the inbox file ITSELF and the file is never renamed (see the
// header), so every writer and the reader serialise on ONE inode: two processes
// cannot hold it at the same time, and a process that dies holding it releases
// it the moment the kernel closes the descriptor (flock has no stale-lock
// failure mode — which is why this is a dependency-free primitive rather than a
// lock FILE, one fewer artifact in the operator's profile, Rule 35).
func sessionInboxLock(dir string, create bool) (*os.File, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("session inbox: empty session directory")
	}
	if create {
		if err := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Join(dir, sessionInboxDirName), sessionInboxDirPerm); err != nil {
			return nil, fmt.Errorf("session inbox: cannot create %s: %w", filepath.Join(dir, sessionInboxDirName), err)
		}
	}
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(sessionInboxPath(dir), flags, sessionInboxFilePerm)
	if err != nil {
		return nil, fmt.Errorf("session inbox: cannot open %s: %w", sessionInboxPath(dir), err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close() //stylegate:ignore the lock could not be taken, so nothing is serialised through this descriptor; the caller gets the error
		return nil, fmt.Errorf("session inbox: cannot lock %s: %w", sessionInboxPath(dir), err)
	}
	return f, nil
}

// sessionInboxUnlock releases the lock and closes the descriptor. Both happen
// on every path (deferred at the call site), so a refusal never leaves the
// inbox locked for the next caller.
func sessionInboxUnlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //stylegate:ignore close() below releases the lock regardless; this is the tidy path
	_ = f.Close()                                   //stylegate:ignore the descriptor is done with; a close failure cannot be acted on here
}

// sessionInboxAppend writes one record at the end of the file, under the lock
// the caller already holds. It is the ONE append in this file: the deliver face
// and the delivered-marker writer both go through it, so the on-disk shape has
// a single producer.
func sessionInboxAppend(f *os.File, rec sessionMessageRecord) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("session inbox: cannot encode record: %w", err)
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return fmt.Errorf("session inbox: cannot seek to end: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("session inbox: cannot append: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("session inbox: cannot flush: %w", err)
	}
	return nil
}

// sessionInboxRecords decodes the inbox's tail into records, using a JSON
// STREAM decoder (never a line loop — see the header). The second return value
// names a record the decoder could not read, so the caller can report it
// instead of silently counting a corrupt tail as "no messages" (Rule 15).
//
// A truncated HEAD (the tail window starting mid-record) is expected and is
// skipped silently: it is a window artefact, not damage — the difference is
// that the reader knows it started mid-file because it did not read from 0.
func sessionInboxRecords(f *os.File) (recs []sessionMessageRecord, damaged error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("session inbox: cannot stat: %w", err)
	}
	size := fi.Size()
	start := int64(0)
	partialHead := false
	if size > sessionInboxReadCap {
		start = size - sessionInboxReadCap
		partialHead = true
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("session inbox: cannot seek: %w", err)
	}
	dec := json.NewDecoder(io.LimitReader(f, int64(sessionInboxReadCap)))
	first := true
	for {
		var rec sessionMessageRecord
		err := dec.Decode(&rec)
		if errors.Is(err, io.EOF) {
			return recs, nil
		}
		if err != nil {
			if first && partialHead {
				// The window opened inside a record: drop this fragment and
				// carry on from the next decodable object.
				first = false
				continue
			}
			return recs, fmt.Errorf("session inbox: unreadable record after %d good one(s): %w", len(recs), err)
		}
		first = false
		recs = append(recs, rec)
	}
}

// sessionInboxPending folds the records into "which messages have NOT reached
// the model yet": a message is pending unless a delivered marker names its id.
// The fold is the whole read semantics of the file, stated once so the reader
// and any future inspector cannot disagree.
func sessionInboxPending(recs []sessionMessageRecord) []sessionMessageRecord {
	delivered := make(map[string]bool, len(recs))
	for _, r := range recs {
		if r.Kind == sessionMessageRecordDelivered && r.ID != "" {
			delivered[r.ID] = true
		}
	}
	out := make([]sessionMessageRecord, 0, len(recs))
	for _, r := range recs {
		if r.Kind != sessionMessageRecordMessage || r.ID == "" {
			continue
		}
		if delivered[r.ID] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// SessionInboxDeliver appends ONE external message to the inbox of the session
// directory `dir`. It creates the inbox on first use and never rewrites what is
// already there.
//
// Delivery is at-least-once at this seam: the caller's invocation is the unit,
// so a caller that retries after an ambiguous failure appends a SECOND record
// (a new id). The counterweight lives on the read side (a message whose
// delivered marker exists is never merged again), so the pair of faces gives
// the model each message exactly once on the steady path.
//
// A caller whose retry CANNOT be told apart from a fresh send — the cross-peer
// hop, whose failure mode is an ssh leg that died after the peer had already
// written — uses SessionInboxDeliverDerived instead: it derives the id from the
// content, so the same send twice IS the same record.
func SessionInboxDeliver(dir, sessionID, message, source string) (SessionMessageDelivery, error) {
	del, _, err := sessionInboxDeliver(dir, sessionID, message, source, "")
	return del, err
}

// SessionInboxDeliverDerived is SessionInboxDeliver for a caller that DERIVES
// the record's id from the delivery's own content (NotifyDeliveryID), and it is
// therefore IDEMPOTENT on it: an inbox that already carries a message record
// with this id gets NOTHING appended, and the existing record's location comes
// back with recorded=false.
//
// WHY THE IDEMPOTENCE LIVES HERE AND NOT AT THE CALLER. The check has to be
// inside the same locked critical section as the append, or two concurrent
// senders both read "absent" and both append — a check-then-act race the lock
// already solves for the write. The caller could not fix that from outside.
//
// WHAT IT DOES NOT DO. It does not look at the delivered marker: a message
// whose marker exists is a message the session has ALREADY TAKEN, and appending
// a copy would hand the model the same instruction twice — that refusal belongs
// to the cross-peer handler (NotifyPeerServe), which knows the initiator's
// intent and can say so by name. Here the id is simply the identity of the
// record, and the record exists.
func SessionInboxDeliverDerived(dir, sessionID, message, source string) (SessionMessageDelivery, bool, error) {
	return sessionInboxDeliver(dir, sessionID, message, source, NotifyDeliveryID(sessionID, message, source))
}

// sessionInboxDeliver is the ONE body behind both faces above. id == "" means
// "mint a fresh random id and append unconditionally" (the historic behavior);
// a non-empty id means "this delivery's identity is already known — append only
// if the inbox does not carry it".
func sessionInboxDeliver(dir, sessionID, message, source, id string) (SessionMessageDelivery, bool, error) {
	text := strings.TrimSpace(message)
	if text == "" {
		return SessionMessageDelivery{}, false, errors.New("session inbox: message must not be empty")
	}
	if source == "" {
		source = sessionMessageSource
	}
	f, err := sessionInboxLock(dir, true)
	if err != nil {
		return SessionMessageDelivery{}, false, err
	}
	defer sessionInboxUnlock(f)

	fi, err := f.Stat()
	if err != nil {
		return SessionMessageDelivery{}, false, fmt.Errorf("session inbox: cannot stat %s: %w", sessionInboxPath(dir), err)
	}
	if fi.Size() >= sessionInboxRefuseBytes {
		return SessionMessageDelivery{}, false, fmt.Errorf(
			"session inbox: %s is at its %d-byte ceiling — no automatic pruning exists (nothing new is created to do it); remove the consumed records and deliver again",
			sessionInboxPath(dir), sessionInboxRefuseBytes)
	}

	path := sessionInboxPath(dir)
	if id != "" {
		// A DAMAGED INBOX IS NOT AN EMPTY ONE (Rule 15). The records reader
		// reports a file it could not fully decode, and that answer must not be
		// read as "this id is absent": appending would then quietly turn an
		// unreadable record into a second copy of a message that may already be
		// there — and the idempotence this function exists for would be lost
		// exactly when the file is already suspect.
		recs, damaged := sessionInboxRecords(f)
		if damaged != nil {
			return SessionMessageDelivery{}, false, fmt.Errorf("session inbox: cannot tell whether id %s is already in %s: %w", id, path, damaged)
		}
		for _, r := range recs {
			if r.ID != id {
				continue
			}
			if r.Kind != sessionMessageRecordMessage {
				// The id is present as a delivered MARKER with no message
				// record beside it: the inbox is damaged (the marker names a
				// delivery that is not in the file). Appending would "repair"
				// it by re-injecting a message the session has already taken,
				// so it is refused by name instead (Rule 15).
				return SessionMessageDelivery{}, false, fmt.Errorf(
					"session inbox: %s carries a %s marker for id %s but no %s record for it — the file is damaged; repair it by hand rather than re-delivering (a re-injection would repeat an instruction the session already took)",
					path, sessionMessageRecordDelivered, id, sessionMessageRecordMessage)
			}
			return SessionMessageDelivery{
				ID:        id,
				SessionID: r.SessionID,
				Dir:       dir,
				Path:      path,
				CreatedAt: r.CreatedAt,
			}, false, nil
		}
	}
	if id == "" {
		id = sessionMessageID()
	}
	created := time.Now().UTC().Format(time.RFC3339)
	rec := sessionMessageRecord{
		Kind:      sessionMessageRecordMessage,
		ID:        id,
		SessionID: sessionID,
		Message:   text,
		Source:    source,
		CreatedAt: created,
	}
	if err := sessionInboxAppend(f, rec); err != nil {
		return SessionMessageDelivery{}, false, err
	}
	return SessionMessageDelivery{
		ID:        rec.ID,
		SessionID: sessionID,
		Dir:       dir,
		Path:      path,
		CreatedAt: created,
	}, true, nil
}

// NotifyDeliveryID derives the delivery id for a (session, message, source)
// triple — the id a message carries on EVERY host it is delivered to, and the
// anchor that makes a re-send land on the record it already made.
//
// WHY DERIVED AND NOT RANDOM. The cross-peer hop's failure mode is an ssh leg
// that times out AFTER the peer appended: the sender cannot tell "the peer
// never got it" from "the peer got it and the answer was lost", so its only
// safe retry is one that produces the SAME record. A random id (what a local
// delivery mints) makes that retry a second record with a second id, i.e. the
// duplicate injection this id exists to prevent. Deriving also makes the
// printed id and the peer's inbox id the same string, which is what the
// operator's evidence needs.
//
// WHY THIS IS SAFE (the collision question). The digest is framed by LENGTH
// (each field is prefixed with its byte count), so no two distinct triples
// serialize to the same byte stream — a message ending in "12" and one
// beginning with it cannot trade places. What remains is an 8-byte collision,
// and the outcome of one is bounded and safe rather than silent: the second
// delivery is treated as a repeat, appends nothing, and the sender is TOLD
// (recorded=false + Note). Losing a duplicate is the opposite failure from
// injecting one, and it is the direction this feature chooses.
func NotifyDeliveryID(sessionID, message, source string) string {
	h := sha256.New()
	for _, part := range []string{sessionMessageKind, source, sessionID, message} {
		fmt.Fprintf(h, "%d:", len(part))
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))[:sessionMessageIDHexLen]
}

// SessionInboxLoad is the READ face: it merges every message this session has
// not yet been given into the in-process inbox (`theJobs.notices`, the ONE
// queue) and marks each of them delivered.
//
// It is called by the round boundary of a chat turn (internal/agent/chat_ops.go)
// immediately before that same boundary drains the inbox with TakeJobNotices —
// so an external message takes EXACTLY the path a background-job completion
// notice takes, through the same take point, in the same round.
//
// Returns the messages merged this call (the tests assert on it; production
// ignores it, because the take is the boundary's own).
//
// The marker is appended AFTER the merge, in the same locked critical section:
// the residual window (the process dies between the in-process merge and the
// marker's flush) degrades to at-least-once for that ONE message on the NEXT
// start. That direction is deliberate — a repeated instruction is visible and
// recoverable, a silently dropped one is neither.
func SessionInboxLoad(dir string) ([]ExternalMessage, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("session inbox: empty session directory")
	}
	if _, err := os.Stat(sessionInboxPath(dir)); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no inbox: nothing was ever delivered here
		}
		return nil, fmt.Errorf("session inbox: cannot stat %s: %w", sessionInboxPath(dir), err)
	}
	f, err := sessionInboxLock(dir, false)
	if err != nil {
		return nil, err
	}
	defer sessionInboxUnlock(f)

	recs, damaged := sessionInboxRecords(f)
	pending := sessionInboxPending(recs)
	if len(pending) > sessionInboxTakeBudget {
		// The remainder stays pending: the level (JobNoticePending) is still
		// true, so the next boundary takes the next slice.
		pending = pending[:sessionInboxTakeBudget]
	}

	out := make([]ExternalMessage, 0, len(pending))
	for _, r := range pending {
		out = append(out, ExternalMessage{
			ID:        r.ID,
			SessionID: r.SessionID,
			Dir:       dir,
			Message:   r.Message,
			Source:    r.Source,
			CreatedAt: r.CreatedAt,
		})
	}

	// ONE locked critical section: the merge into the single queue, then the
	// markers that say it happened.
	theJobs.mu.Lock()
	for _, m := range out {
		theJobs.notices = append(theJobs.notices, JobNotice{
			Status:   JobRunning, // no job produced this fact; nothing has exited
			External: &ExternalMessage{ID: m.ID, SessionID: m.SessionID, Dir: m.Dir, Message: m.Message, Source: m.Source, CreatedAt: m.CreatedAt},
		})
	}
	theJobs.mu.Unlock()

	for _, m := range out {
		if err := sessionInboxAppend(f, sessionMessageRecord{Kind: sessionMessageRecordDelivered, ID: m.ID}); err != nil {
			// The merge already happened; the marker failing means the next
			// start re-injects this message (at-least-once). Reported, never
			// swallowed (Rule 15) — and the pair already in hand is kept, so a
			// caller that logs this still gets its messages.
			return out, err
		}
	}
	if len(out) > 0 {
		// The EDGE of the notice primitive: the level went true, so any reader
		// parked at an idle prompt learns that the inbox became non-empty
		// without the operator typing anything. It carries no payload — the
		// TAKE stays at the round boundary (job_notice_wake.go).
		NotifyJobNoticeWaiters()
	}
	if damaged != nil {
		// The FILE is named in the report: the caller is a round boundary that
		// printed it to the operator's stderr, and "unreadable record" without
		// a path is a message nobody can act on (Rule 15).
		return out, fmt.Errorf("%s: %w", sessionInboxPath(dir), damaged)
	}
	return out, nil
}

// sessionMessageID mints a delivery id: 64 random bits, hex. It is a LOCAL
// message identity — the anchor the delivered marker names — not a protocol
// uuid, so it is generated here from the standard library rather than by
// importing a second uuid implementation into this package (Rule 35: a
// dependency has to earn its place, and 64 random bits carry no collision risk
// at one operator's delivery volume).
func sessionMessageID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A message id that cannot be random is still better than a message
		// that is not delivered: fall back to the clock plus the pid, which is
		// unique across the processes that can write one inbox concurrently.
		return fmt.Sprintf("%0*x", sessionMessageIDHexLen, uint64(time.Now().UnixNano())^(uint64(os.Getpid())<<32))
	}
	return hex.EncodeToString(b[:])
}
