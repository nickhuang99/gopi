package tools

// worktree_sweep_chat.go — the CHAT axis of the worktree sweep (epic
// chat-ground-reclaim, 2026-09-29).
//
// THE GAP THIS CLOSES, stated exactly. `gopi chat` USED TO mint a ground for
// every RUN (epic chat-session-ground, 2026-09-27): a linked worktree
// `<worktree root>/gopi-<session id stem>` on branch `chat/<session id stem>`.
// **That producer is RETIRED as of 2026-09-29** — the operator's decision,
// implemented in `internal/tools/chat_ground.go` + `cmd/agent/chat_ground.go`:
// a new chat creates no branch and no worktree, and reports in one stderr line
// which directory its writes land in. What remains is the RESIDUE of earlier
// gopi versions, and THIS file is the residue's collector, not a producer's
// partner.
//
// The ground was deliberately never stamped in the ledger — "NOTHING IS STAMPED
// IN THE LEDGER. A chat is not a row" — and that sentence is the origin of the
// residue: every arm that could free a ground is LEDGER-KEYED, so an INERT
// ground (clean, 0 ahead of the integration branch, its session over) has no
// collector at all.
//
// MEASURED 2026-09-28 (devdoc/chat-session-ground-lifecycle-diagnosis.md §3):
// the sweep DOES reach these registrations and keeps every one of them — for
// two PROCESS-GLOBAL reasons and one real one. The real one is the AGE FLOOR
// (`worktree_sweep.go` guard ⑤): 24 h is the shield for "a task just created
// this", and it is measured on the REGISTRATION, so a ground minted today can
// never be reclaimed today however conclusively its session ended. The other
// two are the axes this file also supplies evidence for: a live row unrelated
// to the registration (guard ③'s session arm) and a guest checkout (guard ②).
// The chat axis does not weaken any of them: it supplies the one thing they
// lack — a LEDGER-KEYLESS PROOF that the ground's own session is over.
//
// WHAT IS ADDED HERE, and nothing else:
//
//	① A CHAT SESSION READER (this file): read-only, fail-closed, over the chats
//	   table that already exists in the ledger the profile already resolves.
//	② ONE INERTNESS PREDICATE — is this registration a chat's ground, and is its
//	   session provably over? (worktreeSweepChatInert)
//	③ TWO RUNGS in worktree_sweep.go that use it: one on the PATH axis (where
//	   the age floor is replaced by a proof of endedness) and one on the BRANCH
//	   axis (which no arm could reach while the registration was live).
//
// NOTHING PERSISTENT IS CREATED (Rule 35): no daemon, no cron, no settings key,
// no state file, no script. The arm it feeds is run by the operator verb
// `gopi task merged-cleanup` (mergedCleanupPassOnce → serveWorktreeReapSweep,
// cmd/agent/cli_tasks.go). CORRECTED 2026-10-30 (task 01a18ef7): this line used
// to say the arm "already ticks on the resident hot path
// (cmd/agent/task_lifecycle_tick.go → the worktree reap)", and that path never
// ran it — the pass had no production caller and was deleted.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The chat axis's own vocabulary, each spelled once (Rules 1b/13/14).
const (
	// worktreeSweepChatBranchPrefix is the branch namespace a chat ground is
	// checked out on. It is the producer's own constant, referenced and never
	// re-typed: a mirrored literal is exactly the drift Rule 13 exists to stop.
	worktreeSweepChatBranchPrefix = ChatGroundBranchPrefix
	// worktreeSweepChatTableName is the chat index's table name as a READER
	// spells it. The recorder renders its DDL through chatsTable() (chat.go);
	// this constant is the one name a reader selects from, and the source gate
	// in worktree_sweep_chat_test.go asserts the two cannot drift apart.
	worktreeSweepChatTableName = "chats"

	// UUID spelling, shared by the branch stem and the index row: 32 hex
	// characters in the branch, the same 32 in four dashes' worth of groups
	// (8-4-4-4-12) in the row. Measured on the live profile: branch
	// `chat/c5f1189ace3444f2a40c74ebcb148fc7`, index row
	// `c5f1189a-ce34-44f2-a40c-74ebcb148fc7` — the dashes are the ONLY
	// difference, which is why the reader asks BOTH spellings instead of
	// assuming one.
	worktreeSweepChatUUIDHexLen    = 32
	worktreeSweepChatUUIDDashedLen = 36

	// worktreeSweepChatQuietGrace is how long a chat's own evidence must have
	// been QUIET before its ground is treated as an ended session's, when the
	// index does not positively say "closed". It covers the one exit that runs
	// no code at all — SIGKILL / OOM / power loss (chat_ground.go's own exit
	// table: `ended_at` stays NULL forever) — and it is deliberately the same
	// order as the sweep's other in-flight floor (worktreeSweepInitLockFloor):
	// long enough that a conversation being read, or a run between turns, is
	// never mistaken for residue, short enough that a crash's ground does not
	// need a human to notice it first.
	worktreeSweepChatQuietGrace = time.Hour

	// worktreeSweepChatSessionScanMax bounds how many entries of a session
	// directory the freshness probe stats (a chat log directory is small, and an
	// unbounded walk would make one sweep an unbounded read).
	worktreeSweepChatSessionScanMax = 32
)

// worktreeSweepChatHolderPhrase renders the chat arm's refusal for whichever
// answer forbade the collect. A MEASURED holder keeps the chat-specific
// sentence (a live process standing in a chat ground IS that session), while a
// TRUNCATED scan is its own fact — the ground is kept because nobody finished
// looking, NOT because this session was seen running — and it is stated as
// such, never dressed as the session's liveness (Rule 15).
func worktreeSweepChatHolderPhrase(why string) string {
	if why == scratch.HolderVetoReason {
		return worktreeSweepChatLiveHolderMarker
	}
	return worktreeSweepChatScanTruncatedMarker
}

// The evidence sentences the chat arms render. One spelling each (Rule 13), so
// the judge, this reader and the tests cannot drift apart.
const (
	// worktreeSweepChatNoRowMarker: the index answered, and knows no session
	// with this id. That is the shape a ground whose run produced no row at all
	// leaves behind (measured: the six grounds of the diagnosis document left
	// no row, no session directory).
	worktreeSweepChatNoRowMarker = "the chat index knows no session with this id"
	// worktreeSweepChatClosedMarker: the row says the session was stamped closed.
	worktreeSweepChatClosedMarker = "its chat row records the session closed at "
	// worktreeSweepChatQuietMarker: the row exists but was never closed, and
	// everything the session owns has been quiet past the grace — the crash
	// window. Named separately from the closed case because the evidence is
	// different and an operator reads the difference.
	worktreeSweepChatQuietMarker = "its chat row was never closed and everything the session owns has been quiet for longer than "
	// worktreeSweepChatOpenMarker: the session is young or still writing — a live
	// conversation's ground.
	worktreeSweepChatOpenMarker = "its chat session is still going ("
	// worktreeSweepChatLiveHolderMarker: a live process stands in the ground.
	worktreeSweepChatLiveHolderMarker = "a live process holds the ground — a chat carrier stands in its own ground for the whole session, so a live process here IS that session"
	// worktreeSweepChatScanTruncatedMarker: the holder scan stopped before the
	// end of the process table, so this ground is kept because nobody FINISHED
	// looking — which is not the same fact as "a live process holds it". The
	// chat arm states whichever of the two it actually measured (Rule 15);
	// worktreeSweepChatHolderPhrase picks between them.
	worktreeSweepChatScanTruncatedMarker = "the process scan was truncated before it finished, so nothing was observed holding it — kept because it was not fully looked at"
	// worktreeSweepChatUnreadableMarker: the index could not be read at all. It
	// never frees anything BY ITSELF: the rung falls back to the registration's
	// own creation record, and an unanswerable question is not endedness
	// (Rule 15).
	worktreeSweepChatUnreadableMarker = "the chat index could not be read"
	// worktreeSweepChatUndatableMarker: nothing the session owns carries a
	// usable timestamp AND the registration itself could not be dated. Kept:
	// "I cannot tell when this was" is not endedness.
	worktreeSweepChatUndatableMarker = "nothing the session owns is datable and the ground itself could not be dated — keeping"
)

// chatGroundVerdict is what the chat index says about one session id.
type chatGroundVerdict struct {
	// Found reports that a row exists for this session id.
	Found bool
	// Closed reports that the row carries a positive close stamp.
	Closed bool
	// QuietAt is the newest moment anything the session owns was written — the
	// row's own close stamp or start stamp, and the session directory's newest
	// entry. Zero when nothing datable exists.
	QuietAt time.Time
}

// worktreeSweepChatSessionStem reports the session-id stem a registration names
// when it is a CHAT ground's, and "" for every other registration.
//
// THE BRANCH IS THE ONLY CLASSIFIER, and that is forced, not chosen: the chat
// ground's DIRECTORY is `<worktree root>/gopi-<stem>` — the SAME
// TaskGroundDirPrefix every task ground carries (`gopi-<task id stem>`,
// internal/tools/task_run_ground.go) — so a directory name cannot tell a chat's
// ground from a task's. The namespace that CAN is the branch's: `chat/`
// (ChatGroundBranchPrefix) was minted only by chat_ground.go, whose creation
// path no longer exists — the namespace is now purely ADDITIVE, the marker an
// earlier gopi's ground carries and a new one never will.
//
// MEASURED as the reason this arm exists at all (devdoc/chat-session-ground-lifecycle-diagnosis.md
// §1): a chat ground carries BOTH names, so every chat ground is still
// recognised by the branch half alone, and no task ground can be (a task's
// branch is `task/<stem>`, or the row's own recorded name — `worktree_sweep.go`
// guard ③ judges that case in the ledger's own terms first).
func worktreeSweepChatSessionStem(reg worktreeRegistration) string {
	b := strings.TrimSpace(reg.Branch)
	if !strings.HasPrefix(b, worktreeSweepChatBranchPrefix) {
		return ""
	}
	return strings.TrimPrefix(b, worktreeSweepChatBranchPrefix)
}

// worktreeSweepChatInert decides whether a registration is a CHAT's ground
// whose session is provably OVER. It returns the verdict plus the evidence
// sentence that decided it ("" when the registration is not a chat's ground at
// all, which is the caller's "this rung does not apply").
//
// INERT requires ALL of:
//
//	① a chat ground's branch (the only classifier there is — see
//	   worktreeSweepChatSessionStem);
//	② no live process in the ground — a chat carrier ORDINARILY stood in its
//	   own ground for the whole session while the prospect was alive (the old
//	   cmd/agent/chat_ground.go chdir'd into it before the first turn, so a live
//	   process's cwd inside it was positive proof the session was running, and
//	   that proof survives the SIGKILL of the ledger writer alone). A ground
//	   minted by a NEWER gopi is never a ground at all; the liveness arm is for
//	   the residue, whose carrier may still be running.
//	③ the session is over on evidence that is POSITIVE ("the row records it
//	   closed") or QUIET (nothing the session owns has been written for longer
//	   than worktreeSweepChatQuietGrace).
//
// The QUIET arm is what makes this safe without a human: every action a chat
// takes touches something datable — the index row it opens at its first turn,
// and the session directory it writes its turns into — so "nothing moved for an
// hour" cannot be true of a conversation somebody is having, and an index that
// was never written at all still leaves the REGISTRATION's own creation time as
// the last fact (a ground minted a minute ago is therefore never inert, however
// empty its index is).
//
// createdAt is the registration's own creation instant (guard ⑤'s measurement);
// a zero value means the host could not date the ground at all, and an
// undatable ground is KEPT — "I cannot tell when this was" is not endedness.
func worktreeSweepChatInert(reg worktreeRegistration, holders scratch.HolderScan, opts worktreeSweepOptions, createdAt time.Time) (bool, string) {
	stem := worktreeSweepChatSessionStem(reg)
	if stem == "" {
		return false, ""
	}
	// ② liveness first: it costs no ledger read and no filesystem walk, and it
	//    is the only evidence checkable without asking anything else.
	if why := scratch.HolderVeto(reg.Path, holders); why != "" {
		return false, worktreeSweepChatHolderPhrase(why)
	}
	// ③ the index, then the filesystem. An UNREADABLE index is NOT endedness:
	//    a host whose ledger is missing, corrupt or has no chat index at all
	//    cannot answer the question this rung asks, and the rung falls back to
	//    the ladder's own evidence (the age floor at ⑤) rather than reading its
	//    own ignorance as a session's goodbye. The reclaimable shape is the
	//    index ANSWERING — "no row names this session", or a row that says so.
	verdict, ok := worktreeSweepChatVerdict(stem, opts)
	if !ok {
		return false, worktreeSweepChatUnreadableMarker
	}
	// A CLOSED row is POSITIVE proof: the session ran its own close-out, which
	// only a clean exit does (chat_ground.go's exit table). It is not
	// second-guessed by a grace — the ground is residue the moment its session
	// says goodbye.
	if verdict.Closed {
		return true, worktreeSweepChatClosedMarker + verdict.QuietAt.UTC().Format(time.RFC3339) + ")"
	}
	quiet := verdict.QuietAt
	if newer, found := worktreeSweepChatSessionDirQuietAt(stem); found && newer.After(quiet) {
		quiet = newer
	}
	if quiet.IsZero() {
		quiet = createdAt
	}
	if quiet.IsZero() {
		return false, worktreeSweepChatUndatableMarker
	}
	if opts.Now.Sub(quiet) < worktreeSweepChatQuietGrace {
		return false, worktreeSweepChatOpenMarker + "last activity " + quiet.UTC().Format(time.RFC3339) + ")"
	}
	if verdict.Found {
		return true, worktreeSweepChatQuietMarker + worktreeSweepChatQuietGrace.String() + " (last activity " + quiet.UTC().Format(time.RFC3339) + ")"
	}
	return true, worktreeSweepChatNoRowMarker + " and nothing it owned has been written for longer than " + worktreeSweepChatQuietGrace.String()
}

// worktreeSweepChatVerdict reads the chat index for one session id, asking BOTH
// spellings the host writes (the branch's dash-free stem and the canonical
// dashed form). ok is "the index answered the question" — never "a row was
// found"; a query error, a missing ledger file or an absent table all answer
// ok=false, and the caller then falls back to its own evidence.
func worktreeSweepChatVerdict(stem string, opts worktreeSweepOptions) (chatGroundVerdict, bool) {
	ids := worktreeSweepChatSessionIDs(stem)
	if len(ids) == 0 {
		return chatGroundVerdict{}, false
	}
	answered := false
	for _, id := range ids {
		v, ok := worktreeSweepChatRowFor(id)
		if !ok {
			continue
		}
		answered = true
		if v.Found {
			return v, true
		}
	}
	if !answered {
		return chatGroundVerdict{}, false
	}
	return chatGroundVerdict{}, true
}

// worktreeSweepChatRowFor reads ONE spelling out of the index. The failure
// direction is deliberate: everything that is not a successful query answers
// ok=false, so "I could not ask" can never be read downstream as "the session
// is over".
func worktreeSweepChatRowFor(sessionID string) (chatGroundVerdict, bool) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return chatGroundVerdict{}, false
	}
	path := strings.TrimSpace(chatDBPath())
	if path == "" || !fileExists(path) {
		return chatGroundVerdict{}, false
	}
	db, err := sql.Open("sqlite", LedgerReadOnlyDSN(path))
	if err != nil {
		return chatGroundVerdict{}, false
	}
	defer db.Close() //stylegate:ignore read-only probe handle, closed after its single query
	ctx, cancel := context.WithTimeout(context.Background(), worktreeSweepGitTimeout)
	defer cancel()
	var ended, started sql.NullFloat64
	err = db.QueryRowContext(ctx,
		`SELECT ended_at, started_at FROM `+worktreeSweepChatTableName+` WHERE session_id = ?`, id).Scan(&ended, &started)
	// A MISSING ROW IS AN ANSWER ("the ledger does not know this session"), not
	// a failure of the read: it is the shape a ground whose run wrote no row at
	// all leaves behind, and the caller reports it as exactly that.
	if errors.Is(err, sql.ErrNoRows) {
		return chatGroundVerdict{}, true
	}
	if err != nil {
		return chatGroundVerdict{}, false
	}
	v := chatGroundVerdict{Found: true}
	if ended.Valid && ended.Float64 > 0 {
		v.Closed = true
		v.QuietAt = time.Unix(int64(ended.Float64), 0)
	} else if started.Valid && started.Float64 > 0 {
		// Never closed: the crash window. The row's START is the newest fact it
		// holds, so that is the session's quiet moment.
		v.QuietAt = time.Unix(int64(started.Float64), 0)
	}
	return v, true
}

// worktreeSweepChatSessionIDs renders the spellings of one session id: the
// producer's own stem first (what every ground this host minted carries), then
// the canonical dashed UUID. An unparseable stem yields no id at all — the
// reader refuses to guess at a session's name.
func worktreeSweepChatSessionIDs(stem string) []string {
	s := strings.TrimSpace(stem)
	if s == "" {
		return nil
	}
	ids := []string{s}
	if dashed := worktreeSweepChatDashedSessionID(s); dashed != "" && dashed != s {
		ids = append(ids, dashed)
	}
	return ids
}

// worktreeSweepChatDashedSessionID renders a session id in the canonical UUID
// spelling (8-4-4-4-12) by INSERTING the dashes into the stem the branch
// carries. The stem's 32 hex characters ARE the session id (the retired
// producer kept the whole id, minus the characters a ref cannot carry), so the
// dashes are the only difference between the two spellings — measured against
// the live profile's own pair. "" when the stem is not dash-free 32-hex.
// The residue's stems are the OLD producer's spelling, which is why the
// two-spelling lookup exists at all; nothing in this tree writes such a stem
// any more.
func worktreeSweepChatDashedSessionID(stem string) string {
	s := strings.TrimSpace(stem)
	if s == "" || strings.Contains(s, "-") || len(s) != worktreeSweepChatUUIDHexLen {
		return ""
	}
	for _, r := range s {
		if !isHexRune(r) {
			return ""
		}
	}
	var b strings.Builder
	b.Grow(worktreeSweepChatUUIDDashedLen)
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 12, 16, 20:
			b.WriteByte('-')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// isHexRune reports whether r is one of the sixteen hex digits — the alphabet
// the retired producer's stem left behind for a UUID session id.
func isHexRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		return true
	}
	return false
}

// worktreeSweepChatSessionDirQuietAt reports the newest write inside the
// session's own directory under the sessions root — the filesystem's half of
// "is this session still alive".
//
// It matters because the index is not the only record a chat leaves: a run that
// never wrote its row, or a row whose close never ran, still has a session
// directory the live run appends to (`~/.gopi/sessions/<id>/`, created for
// EVERY chat run — measured: both spellings of one id exist there). Freshness
// is therefore evidence FOR a live conversation, and its absence is silence,
// never a verdict of its own.
func worktreeSweepChatSessionDirQuietAt(stem string) (time.Time, bool) {
	ids := worktreeSweepChatSessionIDs(stem)
	if len(ids) == 0 {
		return time.Time{}, false
	}
	gd, err := config.GopiDir()
	if err != nil {
		return time.Time{}, false
	}
	root := filepath.Join(gd, taskSessionsRootName)
	newest := time.Time{}
	for _, id := range ids {
		dir := filepath.Join(root, id)
		info, serr := os.Stat(dir)
		if serr != nil || !info.IsDir() {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if entries, derr := os.ReadDir(dir); derr == nil {
			for i, entry := range entries {
				if i >= worktreeSweepChatSessionScanMax {
					break
				}
				child, cerr := entry.Info()
				if cerr != nil {
					continue
				}
				if child.ModTime().After(newest) {
					newest = child.ModTime()
				}
			}
		}
	}
	if newest.IsZero() {
		return time.Time{}, false
	}
	return newest, true
}
