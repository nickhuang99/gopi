package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// chat_surface_identity.go — the RUN-SCOPED identity a `gopi chat` publishes to
// the children it spawns.
//
// WHAT PROBLEM THIS SOLVES. A chat run writes its own screen to an artifact
// (`--dump-screen`, internal/uiclient) and an agent reads that artifact back
// through `gopi ui_state --target chat` — usually from a BASH CHILD of the chat
// itself, because that is how the tool reaches a shell verb. When several chat
// surfaces run against one profile (the ordinary case: the operator's chat
// beside the agent's), the reader has several candidate dumps and no way to tell
// which one its own run wrote: the general rule (uiclient.SelectLive) refuses on
// an ambiguity, and a refusal that lists every surface on the host does not
// answer the one question the caller actually holds — "WHICH OF THESE IS MINE?".
//
// WHY AN ENVIRONMENT VARIABLE AND NOT A FILE, A LOCK, A DAEMON OR A NEW VERB
// (Rule 35). The fact being published is true for exactly one process lifetime
// and is needed by exactly that process's descendants, which is the definition of
// something the process tree already carries: `exec(2)` copies the environment
// for free, the kernel reclaims it at exit, and nothing has to be cleaned up,
// aged out or owned. A file would be a second artifact to create, find, attribute
// and garbage-collect — a persistent dependency bought for a fact that dies with
// the process that owns it. So the channel is the process tree's own, and its
// least-privileged spelling is one variable.
//
// THE VALUE IS AN IDENTITY, AND IT IS READ ONLY AS A CLAIM. It never authorises
// anything: a reader that consumes it matches it against artifacts that ALREADY
// passed the artifact-side verification (the dump's own (pid, started) stamp
// against /proc, uiclient.dumpIdentityVerdict). A stale, forged or malformed
// value therefore degrades to the general rule with the miss SAID OUT LOUD
// (Rule 15) — never to a silent substitution of somebody else's screen.
//
// WHOSE VALUE IS IT. `gopi chat` writes it into its OWN environment at start-up
// (cmd/agent exports it there); every child it spawns inherits it — including
// the `bash` tool's shell, which is the process that runs `gopi ui_state`
// (internal/tools/bash.go builds the child env from `os.Environ()`, minus the
// wrapper-child identity names: this one is not among them, deliberately — it
// describes the SURFACE the child belongs to, not a right the child must not
// have). The pid field is the CHAT PROCESS's pid, never the child's: the identity
// answers "which surface am I a child of", and a reader must be able to tell that
// from its own pid (`os.Getpid()`).

// ChatSurfaceIdentityEnv is the channel's name. Exported because the WRITER lives
// in another package (cmd/agent exports it) and Gate 21 requires the declared
// name to have a real reader (ReadChatSurfaceIdentity below is it, Rule 1b/13).
const ChatSurfaceIdentityEnv = "GOPI_CHAT_SURFACE_ID"

// The value's own vocabulary. The format is
//
//	<pid|self>:<session-directory>
//
// and it is parsed in TWO halves so each can be absent: a run with a session but
// no meaningful pid says `self:`, and a run whose session directory cannot be
// resolved says `<pid>:`. An EMPTY variable means "no claim at all" — a different
// fact from a malformed one, and the two are reported differently.
const (
	// ChatSurfaceIdentitySeparator separates the pid from the session directory.
	// Exported for the same reason the channel name is: a reader that is not this
	// package (cmd/agent's slot reclaimability check) must cut the value at the
	// SAME byte rather than spell ":" for itself — two spellings of one separator
	// is exactly the drift Rule 13 closes. The unexported alias below is the
	// package's own name for it, so this file's readers keep one spelling.
	ChatSurfaceIdentitySeparator = ":"
	// chatSurfaceIdentitySep separates the pid from the session directory.
	chatSurfaceIdentitySep = ChatSurfaceIdentitySeparator
	// chatSurfaceIdentitySelf is the spelling for a writer that does not know its
	// own pid (a shell function, a wrapper that predates this channel): the field
	// is present and claims nothing, which is what keeps the value's SHAPE stable
	// while its halves stay independently optional.
	chatSurfaceIdentitySelf = "self"
)

// ChatSurfaceIdentity is one run's self-declaration, as a reader received it.
type ChatSurfaceIdentity struct {
	// PID is the surface process the declaring run named, 0 when it named none.
	PID int
	// SessionDir is the session directory that surface is writing its dump into,
	// "" when the declaring run named none. It is the SAME directory
	// uiclient.SessionDirOfDump derives on the artifact side, which is what makes
	// the two sides comparable at all.
	SessionDir string
	// Raw is the value exactly as the environment carried it, so a report can
	// quote what it read rather than what it understood.
	Raw string
}

// Empty reports that no claim was declared at all. A caller MUST distinguish this
// from a claim that failed to match: the first is "this run published nothing"
// (the general rule applies, and it always has), the second is a claim that was
// made and did not hold (a fact worth stating).
func (id ChatSurfaceIdentity) Empty() bool { return id.Raw == "" }

// FormatChatSurfaceIdentity renders the value a chat run publishes:
// `<pid|self>:<session-directory>` (chatSurfaceIdentitySelf when pid <= 0). It is
// exported so the WRITER cannot invent a second spelling of the shape its reader
// parses — the one-drift Rule 13 closes.
func FormatChatSurfaceIdentity(pid int, sessionDir string) string {
	first := chatSurfaceIdentitySelf
	if pid > 0 {
		first = strconv.Itoa(pid)
	}
	return first + chatSurfaceIdentitySep + sessionDir
}

// ReadChatSurfaceIdentity reads the channel and classifies it. Three outcomes,
// and the caller must keep them apart (Rule 15):
//
//   - EMPTY: nothing was published — an identity that is not there is not a
//     failure, and a reader that refused on it would break every caller that has
//     no chat ancestor;
//   - PARSED: the claim, with either half possibly absent (see the vocabulary
//     above);
//   - MALFORMED: the value cannot be read as the declared shape. It is an ERROR,
//     never a silently-ignored string: a channel whose garbage reads as "no
//     claim" is a channel whose defects are invisible, and the honest consumer's
//     move is to say what it could not read and then use the general rule.
//
// A path that itself contains the separator would split wrongly, and this says so
// rather than truncating: the session directory is the field a reader matches on,
// so a silently shortened one would match nothing and look like a miss.
func ReadChatSurfaceIdentity() (ChatSurfaceIdentity, error) {
	raw := strings.TrimSpace(os.Getenv(ChatSurfaceIdentityEnv))
	if raw == "" {
		return ChatSurfaceIdentity{}, nil
	}
	id := ChatSurfaceIdentity{Raw: raw}
	pidField, dirField, ok := strings.Cut(raw, chatSurfaceIdentitySep)
	if !ok {
		return id, fmt.Errorf("%s=%q is not <pid|%s>%s<session-directory>: the pid and the session directory are separated by %q",
			ChatSurfaceIdentityEnv, raw, chatSurfaceIdentitySelf, chatSurfaceIdentitySep, chatSurfaceIdentitySep)
	}
	dirField = strings.TrimSpace(dirField)
	if strings.ContainsRune(dirField, rune(chatSurfaceIdentitySep[0])) {
		return id, fmt.Errorf("%s names a session directory that itself contains %q (%q): the value's two fields cannot be told apart",
			ChatSurfaceIdentityEnv, chatSurfaceIdentitySep, dirField)
	}
	if dirField != "" && !filepath.IsAbs(dirField) {
		return id, fmt.Errorf("%s names the session directory %q, which is not an absolute path", ChatSurfaceIdentityEnv, dirField)
	}
	id.SessionDir = filepath.Clean(dirField)
	if dirField == "" {
		id.SessionDir = ""
	}
	pidField = strings.TrimSpace(pidField)
	if pidField == "" || pidField == chatSurfaceIdentitySelf {
		return id, nil
	}
	pid, err := strconv.Atoi(pidField)
	if err != nil || pid <= 0 {
		return id, fmt.Errorf("%s names the pid %q, which is neither a positive pid nor %q", ChatSurfaceIdentityEnv, pidField, chatSurfaceIdentitySelf)
	}
	id.PID = pid
	return id, nil
}
