package main

// chat_info.go — the chat REPL's `/info` command: ONE command that prints the
// facts a person needs to POINT AT THIS SESSION FROM OUTSIDE IT.
//
// WHY THIS EXISTS. Everything /info prints is already known to the run, and
// every one of those facts is printed SOMEWHERE — the banner draws the model,
// the node and the directory; the `[gopi] session:` / raw-log / dump lines name
// paths on stderr; the status row carries the tools mode. What was missing was a
// command that answers "which session is this, exactly?" as ONE block whose lines
// can be COPIED — above all the session UUID, which is the handle every other
// face (`gopi session log <id>`, `gopi chat --resume <id>`, the session directory name)
// takes as its argument. Asking a person to reassemble that from the banner and
// three stderr lines is how a wrong id gets typed.
//
// THE REPL-NATIVE FAMILY, and why the word lives here (Rule 13/14). Like /queue
// (chat_queue.go) and unlike the shared table (commands.go), the answers are
// facts about THIS PROCESS'S OWN SESSION: the session logger's UUID, the directory
// that logger writes in, the model the agent was built with, the ground the
// session reports, the tools flag the REPL switch flips. The shared command table
// cannot see any of them — a table entry would have to be handed the state, and
// the table's own carriers (web, term) would then document a command they cannot
// answer. So the word is a REPL-native word, declared here beside its renderer,
// and it joins chatSessionWords so the completion menu, the refusal vocabulary and
// the /help page all learn it from ONE place.
//
// COPY CLEANLINESS (the acceptance criterion, and a BUG FIX rather than a taste).
// The lines below include literal shell commands (`gopi session log <uuid>`), and the
// criterion is that the text a reader copies out of this block IS the command:
// no gutter glyph, no reserved column, no prompt chrome. Measured 2026-10-02
// (internal/lineedit/answer.go, answerRow.copied and gutterGlyph): every answer
// row used to open with the turn's two-cell `▌ ` marker, and the operator's own
// paste leg died on the first line with the word `▌`. The renderer's fix was to
// draw NO gutter on the copyable layer — the same layer this command's block
// belongs to. So this file publishes its block through the REPL's own line seam
// (chatInput.WriteLines), whose rows open at column 0 on the band and whose flow
// path writes the bytes verbatim, and the copyability is asserted rather than
// assumed (chat_info_test.go's clean-paste test, over the REAL rendered block).
//
// NO SHORT ALIAS, AND THAT IS DELIBERATE (the epic's ban). A one-letter or
// shortest-unique prefix ("/i", "/in") would be a SECOND spelling that the
// switch, the menu, the refusal vocabulary and every doc would have to carry —
// and a prefix scheme is the invented dispatch mechanism the epic forbids. ONE
// word, `/info`, in full.

import (
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/runcmd"
)

// The command's vocabulary. Every word the switch, the help page and the
// renderer's argument handling compare against is declared once (Rule 1/13).
const (
	// chatInfoCommandName is the slash word that owns this file. It is a
	// REPL-native word: the facts it prints belong to the RUNNING session, which
	// the shared command table (commands.go) cannot see.
	chatInfoCommandName = "info"

	// The printed recipe's own words are NOT declared here: it is composed from
	// the constants that already own them — chatFlagResume (the parser that
	// consumes it, cli_chat.go), CliVerbChat and CliVerbLog (cli_verbs.go).
	// Deriving the command line from those spellings is what keeps a printed
	// recipe from naming a flag or a verb the binary does not accept (Rule 14,
	// applied to a string a human will paste).

	// chatInfoNotDeclared is what an UNDECLARED fact is printed as. The node
	// line is absent on a host whose topology was never declared
	// (config.NodeIdentityLine returns ""), and a field that names a value
	// nothing measured must say so rather than print a blank the reader fills
	// in with a guess (Rule 15: silence is the defect).
	chatInfoNotDeclared = "(not declared)"
)

// chatInfoFields is everything /info prints, assembled by the CALLER because
// only the caller holds the live session. Keeping the renderer a PURE function of
// this struct is what makes the block testable without a terminal (the same
// shape chatBannerSpec uses) — and it is why the acceptance assertions can be run
// against the exact bytes the REPL prints.
type chatInfoFields struct {
	// SessionID is the session's UUID — the handle every other face takes. It
	// is the ONE field the epic calls hard-required.
	SessionID string
	// SessionDir is this run's session directory (beside session.jsonl).
	SessionDir string
	// SessionFile is the JSONL path this run writes.
	SessionFile string
	// Model is the conversation model (agent.Model).
	Model string
	// Dir is the directory the session stands in (its ground), or the process
	// cwd when no ground was resolved — the same value the banner names, read
	// through the same helpers so the two surfaces cannot disagree.
	Dir string
	// Repo is the repository whose checkout Dir belongs to ("" outside one).
	Repo string
	// InRepo is true when Dir belongs to a git work tree.
	InRepo bool
	// Node is the declared node identity (config.NodeIdentityLine's answer with
	// the banner's prefix trimmed), or "" when the topology is undeclared.
	Node string
	// ToolsOn is the session's CURRENT tools mode — the flag /tools flips, read
	// at the moment the command runs rather than at start-up.
	ToolsOn bool
}

// chatInfoUsage is the REPL's own usage block for the word — what `/help info`
// prints and what the REPL's /help page row is rendered from. ONE definition
// (chatNativeUsage returns it), because a REPL-native word's text has no other
// owner (the same rule chatQueueUsage follows).
//
// Its first line is the table row's summary, and it names the SESSION UUID
// first: the block's reason to exist is that a person can point at this session
// from elsewhere, and the UUID is the handle they point with.
const chatInfoUsage = `Usage: /info
Print THIS session's own identity as one copyable block: the full session
UUID (the handle ` + "`gopi session log <id>`" + ` and ` + "`gopi chat --resume <id>`" + ` take), the
session directory and JSONL path, the model, the working directory and the
repository it belongs to, the node/topology, the current tools mode, and a
replay command you can paste into a shell.

Every printed line is copy-safe: commands appear with no gutter glyph and no
reserved column, so what you select IS what you paste. Local and immediate —
never a model turn, never sent to the model.`

// chatInfoBlock renders the /info block from the fields. It is a PURE function
// so the tests can assert the exact bytes (Rule 31's unit half) and so the REPL
// arm has nothing to decide.
//
// THE SHAPE IS DELIBERATE, in three parts:
//
//  1. one `key: value` block, values LEFT-ALIGNED in a single column and every
//     one of them on its own line — a value that shares a line with another
//     fact cannot be selected on its own;
//  2. a blank line, then a "copy:" section whose lines are BARE COMMANDS with
//     no label, no bullet and no indentation: a copied line must be runnable as
//     it stands;
//  3. nothing decorative inside part 2 — the only characters on those lines are
//     the command itself.
func chatInfoBlock(f chatInfoFields) string {
	var b strings.Builder
	b.WriteString(chatInfoHeader)
	b.WriteString("\n")
	for _, row := range chatInfoRows(f) {
		fmt.Fprintf(&b, "%s%s%s\n", row.label, chatInfoLabelSep, row.value)
	}
	b.WriteString("\n")
	b.WriteString(chatInfoReplayHeader)
	b.WriteString("\n")
	for _, line := range chatInfoCommands(f) {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// chatInfoHeader is the block's title line, and chatInfoReplayHeader the title
// of its command section. Named (Rule 1b) because the tests assert the block's
// SHAPE by them, and a reworded header must not silently change what those
// assertions are looking at.
const (
	chatInfoHeader       = "session info:"
	chatInfoReplayHeader = "copy (these lines are runnable as they stand):"
	// chatInfoLabelSep separates a row's label from its value.
	chatInfoLabelSep = "  "
)

// chatInfoRow is one `label: value` line of the block. The label's WIDTH is not
// padded into the value: padding would put the value's first column two spaces
// right of a fixed offset, and a reader selecting a value by eye would take the
// padding with it. The separator is one reading (chatInfoLabelSep).
type chatInfoRow struct{ label, value string }

// chatInfoHandleLine answers ONE `/info` line. It is the REPL arm's whole body:
// it reads the live session, renders the block through the ONE pure renderer
// above, and publishes it on the REPL's own line seam.
//
// WHY NOTHING IS DERIVED HERE. Every value is read from the surface that already
// owns it — the session logger (UUID, JSONL path) rather than a second UUID
// source; chatSessionDir/chatStartupDir/chatBannerNodeIdentity (lineedit_wire.go,
// chat_banner.go) rather than a second derivation of "where does this session
// write" or "which node is this"; chatBannerToolsValue for the tools spelling. A
// second derivation is how two surfaces come to describe one session differently,
// and a reader of /info would have no way to tell which one lied.
//
// The block goes through WriteLines (the same seam /help's panel uses), so its
// rows are committed on the answer band with NO turn gutter and are byte-exact on
// the piped path — the copy-cleanliness the acceptance criterion names.
//
// SINCE 2026-10-22 IT GOES THROUGH WriteNoticeRuns INSTEAD, and the reason is
// the operator's own ruling about the system's voice: a block this surface
// composed about ITSELF is a remark, not an answer, and it is drawn DIM for its
// labels while every VALUE keeps the terminal's own foreground (term_note_row.go
// says why each role was chosen). The bytes are UNCHANGED in every mode — the
// renderer is the same pure chatInfoBlock text, partitioned into cells, and a
// monochrome run concatenates the cells back into exactly those bytes
// (term_note_row_test.go asserts it against chatInfoBlock itself).
func chatInfoHandleLine(input *chatInput, ag *agent.Agent, sess *chatSession) {
	f := chatInfoFieldsFor(ag, sess)
	// flowWriter (stdout) is this block's historic destination — see WriteNoticeRuns,
	// which takes it per caller precisely so this stays byte-identical.
	input.WriteNoticeRuns(chatInfoTermRows(f, input.tool), input.flowWriter(), true)
}

// chatInfoFieldsFor reads the live session into the renderer's fields. It is the
// ONE place that knows where each fact comes from, which is also what lets a test
// exercise the renderer with a literal struct instead of a running agent.
func chatInfoFieldsFor(ag *agent.Agent, sess *chatSession) chatInfoFields {
	f := chatInfoFields{}
	if ag != nil {
		f.Model = ag.Model
		if ag.SessionLog != nil {
			f.SessionID = ag.SessionLog.SessionID()
			f.SessionFile = ag.SessionLog.Path()
			f.SessionDir = chatSessionDir(ag.SessionLog)
		}
	}
	if sess != nil {
		f.ToolsOn = sess.toolsEnabled
		f.Dir = chatStartupDir(sess)
		f.Repo = sess.ground.Repo
		f.InRepo = !sess.ground.Degraded && sess.ground.Dir != ""
	}
	// The ground is REPORTED, never re-resolved: a session that could not
	// determine its directory says so through the same resolution the startup
	// notice printed. When no ground was recorded at all (a caller that never
	// ran the report), the banner's own accessor supplies the directory, so the
	// two surfaces still name one value.
	if strings.TrimSpace(f.Dir) == "" {
		f.Dir = chatStartupDir(sess)
	}
	f.Node = chatBannerNodeIdentity()
	return f
}

// chatInfoRows is the block's fact section, in the order a person resolves a
// session: WHAT it is (uuid, dir, file), then HOW it is configured (model,
// tools), then WHERE it stands (dir, repo, node). An empty value prints the
// honest spelling rather than a blank (Rule 15).
func chatInfoRows(f chatInfoFields) []chatInfoRow {
	sessionID := strings.TrimSpace(f.SessionID)
	if sessionID == "" {
		sessionID = chatInfoNoSession
	}
	dir := strings.TrimSpace(f.Dir)
	if dir == "" {
		dir = chatInfoNoDir
	}
	repo := strings.TrimSpace(f.Repo)
	switch {
	case repo != "":
		// The checkout is named WITH its relationship to the directory the
		// session stands in — the same distinction the ground notice draws, so
		// nobody reads "repo: X" as "my writes are on a branch of X".
		if f.InRepo && strings.TrimSpace(f.Dir) != repo {
			repo = repo + chatInfoRepoLinkedMarker
		}
	default:
		repo = chatInfoNoRepo
	}
	node := strings.TrimSpace(f.Node)
	if node == "" {
		node = chatInfoNotDeclared
	}
	model := strings.TrimSpace(f.Model)
	if model == "" {
		model = chatInfoNoModel
	}
	return []chatInfoRow{
		{chatInfoLabelSession, sessionID},
		{chatInfoLabelSessionDir, firstNonEmpty(strings.TrimSpace(f.SessionDir), chatInfoNoDir)},
		{chatInfoLabelSessionFile, firstNonEmpty(strings.TrimSpace(f.SessionFile), chatInfoNoDir)},
		{chatInfoLabelModel, model},
		{chatInfoLabelDir, dir},
		{chatInfoLabelRepo, repo},
		{chatInfoLabelNode, node},
		{chatInfoLabelTools, chatInfoToolsValue(f.ToolsOn)},
	}
}

// The block's row labels and the spellings it uses for absent facts. Constants
// because the tests assert the block by label, and a label reworded in one place
// only would make those assertions test the wrong row (Rule 1b).
const (
	chatInfoLabelSession     = "session"
	chatInfoLabelSessionDir  = "session dir"
	chatInfoLabelSessionFile = "session file"
	chatInfoLabelModel       = "model"
	chatInfoLabelDir         = "cwd"
	chatInfoLabelRepo        = "repo"
	chatInfoLabelNode        = "node"
	chatInfoLabelTools       = "tools"
	// chatInfoRepoLinkedMarker marks a repo that is NOT the directory the
	// session stands in — a linked worktree or a nested repository.
	chatInfoRepoLinkedMarker = " (linked checkout, not this directory)"
	chatInfoNoSession        = "(no session log — this run has no UUID)"
	chatInfoNoDir            = "(unknown)"
	chatInfoNoRepo           = "(not inside a git work tree)"
	chatInfoNoModel          = "(no model)"
	// chatInfoNoCommands is the copy section's answer on a run that has no
	// session log yet: there is no UUID to point at, so no command is printed.
	chatInfoNoCommands = "(nothing to copy yet — this run has no session log)"
)

// chatInfoShortestPrefix is the SHORTEST accepted-spelling prefix of the command
// word — the boundary the no-short-form gate is built on. It is DERIVED here,
// beside the word, because it is a property of the word: a gate that hard-coded 2
// would silently stop testing anything the day the word got shorter. For the
// second spelling of a word with a length-1 common prefix ("exit" and "clear"
// share "e"/"c"…) the boundary is 1; for /info it is 1 too, because the shared
// alias table carries a "mem" — nothing else. The gate below asserts the PROPERTY
// (no prefix below this length is accepted) rather than a number.
func chatInfoShortestPrefix() string {
	if len(chatInfoCommandName) <= 1 {
		return chatInfoCommandName
	}
	return chatInfoCommandName[:1]
}

// chatInfoAliasNamespace is the shared alias table the REPL/Carrier vocabulary
// reads. It is named here so the no-short-form gate can assert the property over
// ALL KEPT PREFIXES without reaching into commands.go's internals by spelling the
// map access twice.
func chatInfoAliasTarget(word string) (string, bool) {
	canon, ok := commandAliases[word]
	return canon, ok
}

// chatInfoToolsValue renders the tools mode. It reads the SAME two spellings the
// banner's tools fact uses (chatBannerToolsValue), so /info and the startup panel
// cannot describe one session two ways.
func chatInfoToolsValue(on bool) string { return chatBannerToolsValue(on) }

// chatInfoCommands is the block's copy section: the lines a reader pastes. It is
// separated from the fact rows (and from each other) by NEWLINES ONLY — no
// bullet, no label, no indent — so a selection lands on the command.
//
// The recipe is derived from the session's own identity: the replay line names
// the FULL UUID, so it works from any directory, and it is refused (the reader is
// told WHY) rather than printed broken when this run has no session log yet.
func chatInfoCommands(f chatInfoFields) []string {
	sessionID := strings.TrimSpace(f.SessionID)
	if sessionID == "" {
		// No handle to point at: say so instead of printing a command with a
		// placeholder that would be pasted as typed (Rule 15).
		return []string{chatInfoNoCommands}
	}
	return []string{
		chatInfoReplayCommand(sessionID),
		// The live spelling of the log reader — `gopi log <id>` was retired in
		// verb-retirement phase 1 (2026-10-06) and is refused by name now, so a
		// line whose whole job is to be PASTED must carry the group word
		// (Rule 31: the previous spelling was correct until the surface moved).
		fmt.Sprintf("%s %s %s %s", runcmd.BinaryName, CliVerbSession, CliVerbLog, sessionID),
	}
}

// chatInfoReplayCommand builds the replay line, and the SHAPE of it was decided
// by running it rather than by reasoning about it (Rule 31). The first version
// printed `gopi chat <id> --resume <id>` — the seed positional AND the switch,
// both naming the session — and the binary REFUSED it, exit 2:
//
//	gopi chat: --resume names a session ("83dff97c-…") and the argument
//	"83dff97c-…" names another — one resume continues one session
//
// (cli_chat.go: `--resume <id>` and the positional seed are the same thing asked
// two ways, so stating both is a usage error by design.) The recipe therefore
// names the session ONCE, through the switch — the spelling the verb's own help
// documents as the crash-recovery path ("continue it WITH the options that
// session was started with, restored from the session's own record"), which is
// the stronger of the two spellings and the one a reader coming back to a session
// wants. The binary word is runcmd.BinaryName (the ONE definition of the command
// word on PATH) and the flag is chatFlagResume — declared constants, never
// literals typed here (Rule 1/14), so a rename of either cannot leave /info
// printing a command nothing accepts.
func chatInfoReplayCommand(sessionID string) string {
	return fmt.Sprintf("%s %s %s %s",
		runcmd.BinaryName, CliVerbChat, chatFlagResume, sessionID)
}

// firstNonEmpty returns the first non-empty argument, or "".
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
