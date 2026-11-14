package main

// chat_surface_identity.go — the chat run PUBLISHES its own surface identity to
// the children it spawns (the write half of internal/config's
// ChatSurfaceIdentityEnv).
//
// WHY HERE, AND WHY AN ENVIRONMENT VARIABLE (Rule 35). `gopi chat` is the
// surface an agent IS, and the agent reaches a shell verb — including
// `gopi ui_state --target chat`, the only way it can read its own screen back —
// through the `bash` tool, i.e. through a CHILD PROCESS. The fact the child
// needs is "which of the profile's chat surfaces am I a child of", and that fact
// is true for exactly one process lifetime and is needed by exactly that
// process's descendants: `exec(2)` already carries the whole environment across,
// the kernel reclaims it at exit, and nothing has to be created, found,
// attributed, aged or garbage-collected. A file (a pid file, a marker beside the
// dump) would be a second artifact with a lifetime nobody owns — a persistent
// dependency bought for a fact that dies with the process that owns it.
//
// WHERE IT IS WRITTEN: `installChatSurface`, which is the run's ARM point — the
// one place where both halves of the identity already exist (this process's pid,
// and the session directory this run is writing its dump into, from the session
// log the surface was handed). Publishing from the same point that installs the
// surface is what keeps the claim and the screen from being able to disagree:
// there is one seam, and it runs once per chat run.
//
// WHAT THE CHILD DOES WITH IT: nothing authoritative. `gopi ui_state` reads it as
// a CLAIM and matches it against artifacts that have ALREADY proved their own
// identity (the dump's (pid, started) stamp against /proc); a claim that matches
// nothing degrades to the general rule — with the miss said out loud (Rule 15) —
// and never to a silent substitution of somebody else's screen.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// chatSurfaceIdentityNotice is printed ONCE at start-up, beside the armed /
// unarmed notice and only on a tools-enabled run: the channel is invisible
// otherwise, and an agent that had to guess whether its children can claim the
// right dump is an agent that will guess wrong. It names the VARIABLE, its VALUE
// (so a report can quote what was published rather than what was intended) and
// the consumer that acts on it.
const chatSurfaceIdentityNotice = "[gopi] chat_surface tool: this run's own surface identity is published to its children → %s=%s (a `gopi ui_state --target chat` this run spawns claims THIS run's dump, rather than guessing among this profile's dumps)\n"

// chatSurfaceIdentityUnpublishedFormat is the OTHER half of the seam, and it is
// not optional (Rule 15). If the export fails, the claim does not exist — and a
// child then falls back to the general rule, which on a profile with two live
// chat surfaces REFUSES. That refusal is correct, but a reader who was never
// told the channel was missing has no way to know why the reader refused: the
// sentence names the variable, the value it tried to publish and the reason.
const chatSurfaceIdentityUnpublishedFormat = "gopi chat: cannot publish this run's surface identity (%s=%s): %v — a child's `gopi ui_state --target chat` will fall back to the general rule, which refuses when several chat surfaces are live\n"

// chatPublishSurfaceIdentity writes the run's identity into this process's OWN
// environment, from which every child inherits it. sessionPath is this run's
// session record; the directory holding it is the session directory — the SAME
// derivation the artifact side makes when it resolves a dump back to its session
// (uiclient.SessionDirOfDump), so the two halves of the comparison are computed
// the same way and cannot drift.
//
// A run with no session path still publishes, with an empty directory half: its
// pid is a real claim (it identifies the artifact this very process wrote), and
// withholding it because the other half is unknown would throw away a fact the
// caller has.
func chatPublishSurfaceIdentity(sessionPath string) string {
	dir := ""
	if strings.TrimSpace(sessionPath) != "" {
		dir = sessionlayout.RecordContainer(sessionPath)
	}
	value := config.FormatChatSurfaceIdentity(os.Getpid(), dir)
	if err := os.Setenv(config.ChatSurfaceIdentityEnv, value); err != nil {
		fmt.Fprintf(os.Stderr, chatSurfaceIdentityUnpublishedFormat, config.ChatSurfaceIdentityEnv, value, err)
		return ""
	}
	return value
}
