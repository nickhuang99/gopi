package main

// chat_version.go — `/version`: what THIS BINARY is, asked from inside a running
// session.
//
// WHY IT EXISTS (operator's question, 2026-11-22). The CLI has `gopi version`
// and `gopi --version`; a person sitting in a chat had no way to ask the same
// question without leaving the console and typing it in another terminal — and
// the answer matters most exactly there: "am I running the build that has the fix
// I just landed?" is a question about the process you are speaking to, and the
// process is the only thing that can answer it.
//
// WHY IT IS A SHARED-TABLE ENTRY AND NOT A REPL-NATIVE WORD (Rule 14). The
// criterion the portability census settled is whether the answer is a fact about
// THIS SESSION or a fact about THE ARTIFACT. `/info` first looked native because
// it reads a live session, but every field it prints is a value the web carrier
// also holds — so it moved into the table. `/version` is the easier half of that
// same test: `versionReport()` reads `runtime/debug`'s build stamp, which belongs
// to the FILE ON DISK and to no session at all, so there is nothing a carrier
// could supply and nothing for a second implementation to disagree about. One
// renderer, both carriers, no state.
//
// WHAT IT DOES NOT DO. It never calls git and never reads the checkout (see
// versionReport's own note): the answer is properties of the running artifact, so
// it stays true even when the directory around it has moved on — and that is
// precisely the deployment question a chat asks. `gopi version --drift` is the
// OTHER question (how far is this binary from what the checkout would build); it
// shells out and it is a CLI report, so this word deliberately does not reach for
// it.
//
// THE `modified:` LINE. versionReport prints it only when the binary was built
// from a dirty tree, and its PRESENCE is the dirty claim. That matters here more
// than on the CLI: a chat binary built from a dirty tree is a binary whose
// revision does not name its content, and the operator asking `/version` mid
// session is exactly the reader who would otherwise trust the revision line.
//
// There is NO SHORT ALIAS, for the reason /info's file records: a prefix scheme
// ("/v") is a second spelling every door would have to carry, and a prefix is not
// a command word.

import "fmt"

// The command's vocabulary (Rules 1/13): the word, its usage page and the
// argument that asks for the page are declared once, so the switch, the table and
// the tests cannot describe three different spellings.
const (
	// chatVersionCommandName is the slash word that owns this file.
	chatVersionCommandName = "version"
)

// chatVersionUsage is the page `/version`, `/version -h` and `/help version`
// print. It states WHAT is printed and, just as important, what is NOT: the
// absence of a git call is the property that makes the answer a statement about
// the artifact rather than about a directory.
const chatVersionUsage = `Usage: /version
Print what THIS BINARY is: its version, the toolchain and platform it was built
for, the revision the build stamped, and the milestone date that revision
declares. A ` + "`modified:`" + ` line appears ONLY when the tree it was built from was
dirty — its presence is the claim that the revision does not fully name the
content.

Same report as ` + "`gopi version`" + ` (one renderer, versionReport). Reads the build
metadata embedded in this executable: NO git call, so it answers "which build am
I talking to?" even from a directory that has moved on. Local and immediate —
never a model turn, never sent to the model. For how far this binary has drifted
from what this checkout would build, run ` + "`gopi version --drift`" + ` in a shell.`

// runSlashVersion answers /version. It accepts the help request (the HARD help
// standard: every command answers -h/--help/help) and refuses anything else BY
// NAME rather than silently ignoring it (Rule 15) — a typo that printed the
// report anyway would teach a reader that arguments are inert here.
func runSlashVersion(args []string, ctx CommandContext) (string, error) {
	for _, a := range args {
		switch a {
		case HelpFlag, HelpFlagLong, HelpWord, helpShortWord:
			return chatVersionUsage, nil
		}
		return "", fmt.Errorf("/version takes no arguments: %q — usage is bare `/version`", a)
	}
	return versionReport(), nil
}
