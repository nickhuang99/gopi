package main

// chat_session_slots.go — a chat run REGISTERS ITSELF into a fixed set of slot
// files under ITS OWN TREE, so the configured `notify.cmd` can find it.
//
// WHY A FILE, WHEN THE SURFACE IDENTITY IS AN ENVIRONMENT VARIABLE
// (chat_surface_identity.go). The two channels answer different questions and
// their lifetimes are different facts:
//
//   - the environment variable travels DOWN (this run → the children it spawns)
//     and dies with the process, which is exactly right for "which screen am I a
//     child of";
//   - a slot file travels SIDEWAYS: `notify.cmd` is exec'd by the LEDGER's own
//     machinery — the worker tick, the merge loop, a `gopi task update` run from
//     a third shell — none of which is a descendant of the chat run it must
//     reach. A chat cannot be told about a task transition through a channel
//     that only its own children inherit.
//
// So the chat run WRITES its identity where the sink command can read it. The
// value is the SAME value, spelled by the SAME function
// (config.FormatChatSurfaceIdentity: `<pid|self>:<session-directory>`), because
// two spellings of one identity is the drift Rule 13 closes.
//
// THE SET IS FIXED AND BOUNDED (Rule 1/13/14). ChatSessionSlotLimit files, named
// <prefix><index><suffix>, are the whole registry — there is no directory scan
// that could grow, no index, no lock, no cleanup step and no daemon watching
// them (Rule 35: nothing here ticks, and the files outlive nothing that matters:
// a stale entry costs one refused `gopi notify --session` in the sink command,
// which the subscriber owns). Registration is IDEMPOTENT on the session: a run
// re-registering finds its own entry by value and rewrites it in place rather
// than consuming a second slot.
//
// WHICH SLOT A NEW SESSION TAKES WHEN EVERY SLOT IS OCCUPIED (task 01a12d05, the
// shape that lost this machine eight hours of notifications). The old rule was
// "the oldest by mtime", and mtime is not evidence about LIFETIME: it records
// when a run STARTED, so a long-lived console could be evicted in favour of
// nothing, and the only trace was a line saying a replacement had happened. The
// order is now by evidence that an entry's SUBJECT still exists:
//
//  1. this run's own slot (above);
//  2. a free slot;
//  3. a slot whose subject is GONE — its session directory no longer exists, or
//     the pid half names a process that is no longer running. Reclaiming one of
//     these is not a choice between two sessions at all: it is what a dead run
//     left behind;
//  4. NOTHING, when every entry still looks alive: the registration is REFUSED
//     with an error that names the live entries. The registry is out of room,
//     and the honest answer to "may I take one of these?" is no.
//
// WHY REFUSAL AND NOT EVICTION. Evicting a live chat costs that console every
// later notification and tells nobody; refusing costs THIS run the same silence
// and says so on stderr, naming the slots (Rule 15 — 静默降级必须出声). Both
// outcomes deliver nothing to one chat; only one of them can be seen, and only
// one of them can be acted on.
//
// WHERE THE REGISTRY LIVES: config.ChatSlotsDir — the directory this run's OWN
// session tree lives in. That is the profile root for an ordinary run and the
// scratch tree for a run that relocated itself (a replay/film run publishes
// GOPI_SESSIONS_ROOT before its session is minted). Resolving the registry
// through the profile root alone is what filed a relocated run into a tree it
// does not belong to: measured on the operator's profile 2026-10-12, nine of the
// ten slots named long-swept scratch sessions.
//
// THE WRITE IS open(2) WITH O_TRUNC, deliberately: it is a single atomic-ish
// overwrite of one small value, so a crash mid-registration leaves either the old
// value or the new one — never a half-written file that reads as an identity.
// No temp file is created (Rule 22 has nothing to guard here: nothing is
// written outside the profile root).

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// ChatSessionSlotLimit is how many chat runs the registry can name at once.
	// It is a NAMED constant because three independent readers depend on the
	// number agreeing: the registration loop, the subscriber script
	// (scripts/notify/notify.sh, whose header states it) and CONFIG.md.
	ChatSessionSlotLimit = 10
	// ChatSessionSlotPrefix and ChatSessionSlotSuffix spell the file name
	// `chat_session_id_<0..9>.txt`. The subscriber globs the prefix, so the
	// spelling is the channel's contract, not a local choice.
	ChatSessionSlotPrefix = "chat_session_id_"
	ChatSessionSlotSuffix = ".txt"
	// chatSessionSlotFileMode is the mode a slot file carries: the operator's
	// own session directory names ride in it, so it is not group- or
	// world-readable.
	chatSessionSlotFileMode os.FileMode = 0o600
	// chatSlotNotice heads this file's own lines on stderr (cli_chat.go's
	// `[gopi] chat: ` shape — one prefix per surface).
	chatSlotNotice = "[gopi] chat: "
)

// ChatSessionSlotName is the file name of slot i (0-based).
func ChatSessionSlotName(i int) string {
	return ChatSessionSlotPrefix + strconv.Itoa(i) + ChatSessionSlotSuffix
}

// ChatSessionSlotPath is the path of slot i inside the registry dir — the tree
// this run's own session lives in (config.ChatSlotsDir).
func ChatSessionSlotPath(dir string, i int) string {
	return filepath.Join(dir, ChatSessionSlotName(i))
}

// chatSessionSlotMatch says HOW a registration found the slot it writes — the
// fact that decides what the arm point REPORTS, and the reason "this session was
// already registered" must be a different observable from "this run filed the
// first entry" (Rule 15).
//
// The distinction exists because the two arms write the SAME slot for opposite
// reasons: the identity arm is the run re-registering itself (a retry, a
// web/chat run re-arming), while the session arm is a DIFFERENT PROCESS
// continuing an already-registered session — `gopi chat --resume <id>`, whose
// pid half necessarily differs. See registerChatSessionSlot's header for the
// measured defect the second arm closes.
type chatSessionSlotMatch string

const (
	// chatSessionSlotMatchFresh: no entry named this run — a free or reclaimed
	// slot was taken. The pre-existing case.
	chatSessionSlotMatchFresh chatSessionSlotMatch = "fresh"
	// chatSessionSlotMatchIdentity: the WHOLE identity is already filed, i.e.
	// this is literally the same surface re-registering.
	chatSessionSlotMatchIdentity chatSessionSlotMatch = "identity"
	// chatSessionSlotMatchSession: a DIFFERENT identity in the registry names
	// the same session directory — a resumed run in its session's own slot.
	chatSessionSlotMatchSession chatSessionSlotMatch = "session"
)

// ChatSessionSlotEntry is one registered slot as a READER sees it: the file and
// the identity value it carries. An entry with an empty Identity is noise (a
// truncated write, an operator's stray edit) and a reader must skip it rather
// than deliver to a session named "".
type ChatSessionSlotEntry struct {
	Path     string
	Index    int
	Identity string
}

// ChatSessionSlotSessionDir returns the session DIRECTORY half of a slot's
// identity (`<pid|self>:<session-directory>`), which is the operand
// `gopi notify --session` resolves. ok is false when the value carries no
// directory half — the caller states that rather than delivering to "".
func ChatSessionSlotSessionDir(identity string) (string, bool) {
	_, dir, ok := strings.Cut(strings.TrimSpace(identity), ":")
	dir = strings.TrimSpace(dir)
	if !ok || dir == "" {
		return "", false
	}
	return dir, true
}

// ReadChatSessionSlots returns every slot that currently names a session, in
// slot order. A missing file is not an error (a fresh profile has none) and an
// unreadable one is REPORTED rather than skipped silently (Rule 15) — a reader
// that silently dropped an unreadable slot would quietly stop delivering to a
// live chat with nothing on stderr to say so.
func ReadChatSessionSlots(dir string) ([]ChatSessionSlotEntry, error) {
	var out []ChatSessionSlotEntry
	var firstErr error
	for i := 0; i < ChatSessionSlotLimit; i++ {
		path := ChatSessionSlotPath(dir, i)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("chat session slots: cannot read %s: %w", path, err)
			}
			continue
		}
		id := strings.TrimSpace(string(data))
		if id == "" {
			continue
		}
		out = append(out, ChatSessionSlotEntry{Path: path, Index: i, Identity: id})
	}
	return out, firstErr
}

// RegisterChatSessionSlot files this run's identity into the registry and
// returns the slot it used.
//
// The identity value is built by config.FormatChatSurfaceIdentity — the SAME
// function the environment channel's writer uses — so a reader of either
// channel parses one shape.
//
// A session with an empty directory half still registers: its pid is a real
// fact and the reader's own refusal (no directory to deliver to) is the honest
// answer, whereas withholding the entry would look like "no chat is running".
//
// IT DOES NOT REWRITE A SESSION THAT IS ALREADY FILED (epic 01a1fae5). A run
// that continues an existing session publishes a DIFFERENT identity — the pid
// half is this process, not the one that filed the entry — so a registry keyed
// on the whole identity could not see the two as one session and took a free
// slot, delivering every broadcast event twice (scripts/notify/notify.sh's
// per-slot loop). The write is therefore conditional on the MATCH: an entry
// that already names this session is LEFT EXACTLY AS IT IS, and the caller is
// told which match it was so it can say so.
func RegisterChatSessionSlot(dir, sessionDir string) (ChatSessionSlotEntry, error) {
	entry, _, err := registerChatSessionSlot(dir, sessionDir)
	return entry, err
}

// registerChatSessionSlot is RegisterChatSessionSlot plus the MATCH the
// registration settled on — the fact the arm point needs to report honestly
// (chatSessionSlotMatchFresh vs …Identity vs …Session, Rule 15).
func registerChatSessionSlot(dir, sessionDir string) (ChatSessionSlotEntry, chatSessionSlotMatch, error) {
	value := config.FormatChatSurfaceIdentity(os.Getpid(), sessionDir)
	index, match, err := chatSessionSlotIndex(dir, value, sessionDir)
	if err != nil {
		return ChatSessionSlotEntry{}, "", err
	}
	path := ChatSessionSlotPath(dir, index)
	if match == chatSessionSlotMatchSession {
		// ALREADY FILED BY AN EARLIER RUN OF THIS SESSION. Nothing is written:
		// the entry a resume must keep is the one that is there, and rewriting it
		// with this process's pid would turn "registered exactly once" into
		// "registered once, by whoever ran last". Its identity is returned as it
		// READS on disk, so a caller cannot report a value the file does not hold.
		current, err := os.ReadFile(path)
		if err != nil {
			return ChatSessionSlotEntry{}, "", fmt.Errorf("chat session slots: cannot re-read %s: %w", path, err)
		}
		return ChatSessionSlotEntry{Path: path, Index: index, Identity: strings.TrimSpace(string(current))},
			match, nil
	}
	// O_TRUNC, not a temp file + rename: one small value, and the reader's
	// contract is "the file holds an identity or it holds nothing" — a partial
	// write is impossible for a value this size, and a truncate-then-write that
	// raced a reader would be read as "" (skipped), never as somebody else's
	// session.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, chatSessionSlotFileMode)
	if err != nil {
		return ChatSessionSlotEntry{}, "", fmt.Errorf("chat session slots: cannot register %s: %w", path, err)
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		f.Close() //stylegate:ignore the registration write failed and that error is the returned one; the close is the last act of a path already reporting its own failure
		return ChatSessionSlotEntry{}, "", fmt.Errorf("chat session slots: cannot write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return ChatSessionSlotEntry{}, "", fmt.Errorf("chat session slots: cannot close %s: %w", path, err)
	}
	return ChatSessionSlotEntry{Path: path, Index: index, Identity: value}, match, nil
}

// chatSessionSlotIndex decides WHICH slot this registration writes, in the order
// the file header states: this run's own entry → a free slot → a RECLAIMABLE
// entry (its subject is gone) → refusal.
//
// THE FIRST ARM IS TWO ARMS, and the second is the one epic 01a1fae5 added. An
// exact identity match is the pre-existing idempotence (the same surface
// re-registering). A match on the SESSION DIRECTORY ALONE is a DIFFERENT
// process continuing an already-registered session — `gopi chat --resume <id>`,
// whose pid half necessarily differs from the run that filed the entry — and it
// must land in that entry's slot, because the registry's size is the count of
// DISTINCT SESSIONS a sink can deliver to (scripts/notify/notify.sh loops over
// slots, so a second entry is a second delivery of every event).
//
// WHY THE SESSION ARM COMES BEFORE THE RECLAIM ARM, and why it does not matter
// that such an entry often LOOKS reclaimable: the filing run has usually
// exited, so chatSessionSlotReclaimable answers true (dead pid, live directory).
// Taking a free or reclaimed slot is still wrong for it — that is the duplicate
// this epic measured — so the session match is decided first and the entry is
// reused AS IT STANDS (registerChatSessionSlot writes nothing for it).
func chatSessionSlotIndex(dir, value, sessionDir string) (int, chatSessionSlotMatch, error) {
	stats := make([]slotStat, 0, ChatSessionSlotLimit)
	sessionMatch := -1
	wantDir := strings.TrimSpace(sessionDir)
	for i := 0; i < ChatSessionSlotLimit; i++ {
		path := ChatSessionSlotPath(dir, i)
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				stats = append(stats, slotStat{index: i})
				continue
			}
			return 0, "", fmt.Errorf("chat session slots: cannot stat %s: %w", path, err)
		}
		if fi.IsDir() {
			return 0, "", fmt.Errorf("chat session slots: %s is a directory — a slot must be a file holding one identity", path)
		}
		identity := ""
		if data, readErr := os.ReadFile(path); readErr == nil {
			identity = strings.TrimSpace(string(data))
			if identity == value {
				return i, chatSessionSlotMatchIdentity, nil
			}
			if wantDir != "" && sessionMatch < 0 && chatSessionSlotNamesSession(identity, wantDir) {
				sessionMatch = i
			}
		}
		stats = append(stats, slotStat{
			index:    i,
			path:     path,
			exists:   true,
			identity: identity,
			reclaim:  chatSessionSlotReclaimable(identity),
		})
	}
	if sessionMatch >= 0 {
		return sessionMatch, chatSessionSlotMatchSession, nil
	}
	for _, s := range stats {
		if !s.exists {
			return s.index, chatSessionSlotMatchFresh, nil
		}
	}
	for _, s := range stats {
		if s.reclaim {
			return s.index, chatSessionSlotMatchFresh, nil
		}
	}
	// EVERY ENTRY STILL LOOKS ALIVE. The registry is out of room and no slot here
	// is ours to take, so this registration is REFUSED and the error names the
	// occupants — the caller prints it, and the operator can see which chats the
	// next one had to stand aside for.
	occupants := make([]string, 0, len(stats))
	for _, s := range stats {
		occupants = append(occupants, fmt.Sprintf("slot %d (%s) holds %q", s.index, s.path, s.identity))
	}
	return 0, "", fmt.Errorf("chat session slots: all %d slots are held by chats that still look alive and none could be shown to be gone; refusing to evict one — %s",
		ChatSessionSlotLimit, strings.Join(occupants, "; "))
}

// chatSessionSlotNamesSession reports whether an entry identity names the given
// session DIRECTORY. It is the ONE predicate behind the resume arm of
// chatSessionSlotIndex (Rule 13/14): the identity's own parser decides the
// directory half, so the writer cannot disagree with the reader about what a
// slot names.
func chatSessionSlotNamesSession(identity, sessionDir string) bool {
	got, ok := ChatSessionSlotSessionDir(identity)
	if !ok {
		return false
	}
	return filepath.Clean(got) == filepath.Clean(sessionDir)
}

// chatSessionSlotReclaimable reports whether a slot's SUBJECT is gone, so the
// slot may be reused. Two independent facts, either of which is enough, and
// both are measured rather than inferred:
//
//   - the session DIRECTORY the entry names no longer exists (a swept scratch
//     tree, a deleted profile, a run that never minted a session). This is the
//     case that filled nine of this machine's ten slots;
//   - the PID half names a positive process id that is not running. A positive
//     half is what `FormatChatSurfaceIdentity` writes, so anything else (the
//     `self` marker, an empty field) is UNKNOWN and never reclaimed on this
//     ground — an unknown is a KEEP (the same rule the scratch sweep applies).
//
// A malformed entry is reclaimable only through the first arm, and only when it
// carries no directory at all: an entry with a directory that still exists is
// kept, because the reader is what decides whether the identity is deliverable
// and this function must not be the second opinion that silently disagrees.
func chatSessionSlotReclaimable(identity string) bool {
	dir, ok := ChatSessionSlotSessionDir(identity)
	if !ok {
		// No directory half at all: nothing to deliver to, and nothing to keep
		// the slot for. (The reader would skip it; the writer may reuse it.)
		return true
	}
	if _, err := os.Stat(dir); err != nil {
		return os.IsNotExist(err)
	}
	pidField, _, _ := strings.Cut(identity, config.ChatSurfaceIdentitySeparator)
	pid, err := strconv.Atoi(strings.TrimSpace(pidField))
	if err != nil || pid <= 0 {
		return false // `self` or an unreadable pid: UNKNOWN, therefore a KEEP
	}
	return !tools.PidAlive(pid)
}

// slotStat is one slot file as the CHOICE sees it: whether it exists at all, the
// identity it carries, and whether that identity's subject was measured to be
// gone (chatSessionSlotReclaimable).
type slotStat struct {
	index    int
	path     string
	exists   bool
	identity string
	reclaim  bool
}

// chatSessionSlotDir is the ONE resolution of the registry's directory: the tree
// THIS RUN's own session lives in, through config.ChatSlotsDir (Rule 2 — one
// resolution, and the same one the session writer used). It is the profile root
// for an ordinary run, character for character, and the scratch tree for a run
// that relocated its sessions. A run whose tree cannot be resolved reports that
// and registers nothing — it must not invent a directory, and the caller says so
// on stderr.
func chatSessionSlotDir() (string, error) {
	dir, err := config.ChatSlotsDir()
	if err != nil {
		return "", fmt.Errorf("chat session slots: cannot resolve this run's own tree: %w", err)
	}
	return dir, nil
}

// chatSessionSlotDeclineDir is the directory a DECLINED run names in its
// refusal. It resolves the same directory the write would have used (so the
// sentence points a reader at the place to check), and it falls back to the
// resolution ERROR — the refusal must still print when the tree cannot be
// resolved, because "declined" is true either way.
func chatSessionSlotDeclineDir() string {
	dir, err := config.ChatSlotsDir()
	if err != nil {
		return "the operator's profile (whose chat-slot directory could not be resolved: " + err.Error() + ")"
	}
	return dir
}

// chatSessionSlotRegister is the ARM-POINT call: it files this run's identity
// and reports on stderr BOTH when it could not and WHERE it did (Rule 15 — a chat
// whose entry the sink cannot find is a chat that silently receives nothing,
// which is exactly the failure this channel exists to remove).
//
// The success line is not decoration. The registry is a channel with two ends —
// this run writes a file, and the sink command scans a directory derived from ITS
// OWN environment — and until now nothing in the writing run said which directory
// that was, so a run writing into the wrong tree (the defect this fixes) looked
// exactly like a run writing into the right one. One line naming the tree, the
// registry path and the slot makes the channel checkable by the person watching
// the console. It goes to stderr, which is this verb's documented plumbing
// channel (stdout is the answer).
//
// It never fails the run: the chat is fully usable without a registry entry, so
// the cost of a failure is the missing notifications and the operator is told
// that in one line.
func chatSessionSlotRegister(sessionPath string, st termToolStyles) (ChatSessionSlotEntry, bool) {
	return chatSessionSlotRegisterUnless(sessionPath, st, "")
}

// chatSessionSlotRegisterUnless is the ARM POINT's own body, with the operator's
// OFF SWITCH as an explicit argument: `declineWord` is the flag word this run
// was started with (`--no-notify` / `--no-events`), and "" means the run
// subscribes (the default).
//
// WHY IT CARRIES THE WORD AND NOT A BOOL: the refusal below names what was
// TYPED. A sentence that said only "notifications are off" would leave a reader
// unable to tell which of the two spellings took effect, and the epic that
// introduced them is precisely about making the switch observable (Rule 15).
//
// WHY THE DECLINE IS CHECKED HERE AND NOT AT THE CALLER. This is the one call
// that performs the write, so a run that declined is a run that NEVER WROTE a
// slot — not a run whose slot was written and then removed, which leaves a
// window in which `notify.cmd` could deliver to a chat that is not listening.
//
// AND IT IS NOT SILENT (Rule 15). A declined run prints the sentence that says
// so, naming the switch and the directory it declined to write into, because
// "this chat receives no events" and "the write went somewhere I did not look"
// must be different observables to whoever reads the run's stderr. The refusal
// is plain (not dimmed), and the return value is the same "not registered"
// answer the replay short-circuit and a resolved-directory failure give, so no
// caller can tell them apart by shape — only by the sentence, which is the
// point of printing it.
//
// THE REPLAY SHORT-CIRCUIT COMES FIRST AND STAYS FIRST: a replay already
// refuses to write (and says so in its own vocabulary); a replay that ALSO
// carried --no-notify would otherwise get two sentences for one non-write, and
// the replay sentence is the one that is true about the run as a whole.
func chatSessionSlotRegisterUnless(sessionPath string, st termToolStyles, declineWord string) (ChatSessionSlotEntry, bool) {
	// A REPLAY WRITES NO SLOT (internal/replaymode — measured 2026-10-12). The
	// slot registry is the operator's OWN index of live chats; a `--replay` run
	// is a picture of a session that already happened, so filing its scratch
	// identity into that index both LIES to `notify.cmd` (whose deliveries then
	// address a session nobody is reading — the misdelivery the operator
	// measured) and consumes a real slot. The short-circuit is HERE, at the ARM
	// POINT, because this is the one call that performs the write.
	//
	// It ANNOUNCES rather than returning quietly (Rule 15): a reader of the
	// replay's stderr must be able to count what the run refused, or "the replay
	// wrote nothing" and "the write went somewhere I did not look" are the same
	// observable. The false return is the same "not registered" answer a profile
	// that could not be resolved gives — the run continues, and the caller's own
	// reporting contract is unchanged.
	//
	// WHICH LINES WEAR THE SYSTEM VOICE (the `st` parameter, resolved ONCE in
	// runChat). The ONE line that is the surface reporting a fact about ITSELF —
	// the self-check line below — is a termRow in the run's dim voice. The three
	// REFUSALS on this path stay plain: a refusal is information, and dimming a
	// failure is the one emphasis the operator's ruling forbids (a normal state
	// must not be dressed as an incident, and neither may an incident be
	// dressed as a normal state).
	if replaymode.Active() {
		replaymode.Noticef("%s\n", replaymode.Refuse("register "+ChatSessionSlotPrefix+"* in the operator's profile"))
		return ChatSessionSlotEntry{}, false
	}
	// THE OPERATOR'S OFF SWITCH (--no-notify / --no-events). Checked BEFORE the
	// directory is resolved, so a declined run does not even ask where it would
	// have written: the answer is "nowhere", and asking would make the directory
	// resolution's own failure a second, competing report about a write this run
	// already declined.
	//
	// It names the DIRECTORY it declined into rather than saying "notifications
	// off", because the directory is the half that is checkable: a reader can
	// look there and see no file of this run's, which is the observable the
	// acceptance criterion asks for.
	if declineWord != "" {
		fmt.Fprintf(os.Stderr, "%s%s: this run declines the chat event slot — no %s* file was written to %s, so no `notify.cmd` delivery will reach it\n",
			chatSlotNotice, declineWord, ChatSessionSlotPrefix, chatSessionSlotDeclineDir())
		return ChatSessionSlotEntry{}, false
	}
	dir, err := chatSessionSlotDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ChatSessionSlotEntry{}, false
	}
	sessionDir := ""
	if strings.TrimSpace(sessionPath) != "" {
		sessionDir = sessionlayout.RecordContainer(sessionPath)
	}
	entry, match, err := registerChatSessionSlot(dir, sessionDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v — this run will not receive `notify.cmd` deliveries that address chats\n", err)
		return ChatSessionSlotEntry{}, false
	}
	line := chatSessionSlotSelfCheckLine(entry, sessionDir)
	if match == chatSessionSlotMatchSession {
		line = chatSessionSlotKeepLine(entry, sessionDir)
	}
	termWriteRows(os.Stderr, termNoteRows(chatSlotNotice+line, st))
	return entry, true
}

// chatSessionSlotKeepLine is what a RESUMED run says instead of the filing line:
// this session was already registered, so this run added no subscription and
// rewrote nothing. It exists because the two arms of the registry write the SAME
// slot for opposite reasons, and "one entry" has to be a different observable
// from "the write went somewhere I did not look" (Rule 15) — otherwise a resume
// that silently filed a SECOND slot and a resume that correctly kept the one
// entry would read identically on the console.
func chatSessionSlotKeepLine(entry ChatSessionSlotEntry, sessionDir string) string {
	return fmt.Sprintf("slot %d (%s) already names this session — already registered, so this run added no second %s* file and rewrote nothing (the sink delivers once per session; the entry kept is %s)",
		entry.Index, filepath.Dir(entry.Path), ChatSessionSlotPrefix, sessionDir)
}

// chatSessionSlotSelfCheckLine renders the one line that makes this run's
// registration checkable — what was written, where, and what the sink must be
// able to read. It is a pure function of the entry so a test can pin the exact
// sentence (a diagnostic nobody can assert on drifts silently).
func chatSessionSlotSelfCheckLine(entry ChatSessionSlotEntry, sessionDir string) string {
	return fmt.Sprintf("slot %d registered in %s (this run's own tree) — a chat sink reading that directory will find %s",
		entry.Index, filepath.Dir(entry.Path), chatSessionSlotSinkOperand(entry, sessionDir))
}

// chatSessionSlotSinkOperand names what a sink would feed `gopi notify --session`
// for this entry: the session directory, or an explicit statement that the entry
// carries none (which the sink refuses — and that refusal is the honest answer,
// so the check line says it up front rather than implying deliverability).
func chatSessionSlotSinkOperand(entry ChatSessionSlotEntry, sessionDir string) string {
	if dir, ok := ChatSessionSlotSessionDir(entry.Identity); ok {
		return dir
	}
	if strings.TrimSpace(sessionDir) == "" {
		return "no session directory (this run has no session to deliver to)"
	}
	return "no session directory in its identity (the sink will refuse this slot)"
}
