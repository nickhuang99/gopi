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
func RegisterChatSessionSlot(dir, sessionDir string) (ChatSessionSlotEntry, error) {
	value := config.FormatChatSurfaceIdentity(os.Getpid(), sessionDir)
	index, err := chatSessionSlotIndex(dir, value)
	if err != nil {
		return ChatSessionSlotEntry{}, err
	}
	path := ChatSessionSlotPath(dir, index)
	// O_TRUNC, not a temp file + rename: one small value, and the reader's
	// contract is "the file holds an identity or it holds nothing" — a partial
	// write is impossible for a value this size, and a truncate-then-write that
	// raced a reader would be read as "" (skipped), never as somebody else's
	// session.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, chatSessionSlotFileMode)
	if err != nil {
		return ChatSessionSlotEntry{}, fmt.Errorf("chat session slots: cannot register %s: %w", path, err)
	}
	if _, err := f.WriteString(value + "\n"); err != nil {
		f.Close() //stylegate:ignore the registration write failed and that error is the returned one; the close is the last act of a path already reporting its own failure
		return ChatSessionSlotEntry{}, fmt.Errorf("chat session slots: cannot write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return ChatSessionSlotEntry{}, fmt.Errorf("chat session slots: cannot close %s: %w", path, err)
	}
	return ChatSessionSlotEntry{Path: path, Index: index, Identity: value}, nil
}

// chatSessionSlotIndex decides WHICH slot this registration writes, in the order
// the file header states: this run's own entry → a free slot → a RECLAIMABLE
// entry (its subject is gone) → refusal.
//
// The first arm is the idempotence: a re-registration, or a resumed run in the
// same session directory, must not consume a second slot — the registry's size is
// the count of DISTINCT live surfaces.
func chatSessionSlotIndex(dir, value string) (int, error) {
	stats := make([]slotStat, 0, ChatSessionSlotLimit)
	for i := 0; i < ChatSessionSlotLimit; i++ {
		path := ChatSessionSlotPath(dir, i)
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				stats = append(stats, slotStat{index: i})
				continue
			}
			return 0, fmt.Errorf("chat session slots: cannot stat %s: %w", path, err)
		}
		if fi.IsDir() {
			return 0, fmt.Errorf("chat session slots: %s is a directory — a slot must be a file holding one identity", path)
		}
		identity := ""
		if data, readErr := os.ReadFile(path); readErr == nil {
			identity = strings.TrimSpace(string(data))
			if identity == value {
				return i, nil
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
	for _, s := range stats {
		if !s.exists {
			return s.index, nil
		}
	}
	for _, s := range stats {
		if s.reclaim {
			return s.index, nil
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
	return 0, fmt.Errorf("chat session slots: all %d slots are held by chats that still look alive and none could be shown to be gone; refusing to evict one — %s",
		ChatSessionSlotLimit, strings.Join(occupants, "; "))
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
	dir, err := chatSessionSlotDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ChatSessionSlotEntry{}, false
	}
	sessionDir := ""
	if strings.TrimSpace(sessionPath) != "" {
		sessionDir = sessionlayout.RecordContainer(sessionPath)
	}
	entry, err := RegisterChatSessionSlot(dir, sessionDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v — this run will not receive `notify.cmd` deliveries that address chats\n", err)
		return ChatSessionSlotEntry{}, false
	}
	termWriteRows(os.Stderr, termNoteRows(chatSlotNotice+chatSessionSlotSelfCheckLine(entry, sessionDir), st))
	return entry, true
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
