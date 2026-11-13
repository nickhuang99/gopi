package main

// commands_session.go — the SESSION-DESCRIPTIVE half of the shared command table
// (`/info`, `/context`), and the ONE carrier-supplied view of a live session they
// read.
//
// WHY THESE TWO WORDS MOVED INTO THE TABLE (epic 01a1b818-e02f, the portability
// census). Both were REPL-native words, and both were native for a reason that
// turned out to be about RENDERING rather than about the FACT: /info reads the
// session logger's UUID, the ground, the model and the tools flag; /context reads
// this run's raw HTTP log. A web server holds every one of those values too (it
// has ONE persistent agent for its whole life — cli_web.go), so the table could
// answer both words on both carriers with the SAME renderer (chatInfoBlock,
// internal/ctxdump) and no second implementation. What stays carrier-local is the
// CONSOLE SEAM: `gopi chat` answers /info through WriteNoticeRuns so its labels
// draw dim, and the table's answer is the same BYTES on the flow path (asserted
// by TestSlashInfoMatchesTheReplBlock). That split is the one `/new` already has
// (webClearCommand): a table entry for discoverability + dispatch, with a
// carrier arm in front of it where the carrier owns state the table cannot see.
//
// THE SEAM IS DATA, NOT A NEW MECHANISM (Rule 35). CommandContext was declared
// with the comment "ctx exists for future per-carrier additions (e.g. an inline
// task-list id) without breaking the registry signature" — this file is that
// addition and nothing else: the carrier fills a struct of FACTS it already holds,
// the command reads it. No goroutine, no file, no second resolver, no new
// dependency; a command whose facts are absent refuses by name (Rule 15) instead
// of printing a block full of blanks.
//
// THE CENSUS (why only these two of chat's eight) is in the run's report and in
// devdoc/slash-command-portability.md: /quit, /exit, /tools, /queue, /voice and
// /screen are each recorded there with the measured reason they stay where they
// are. The short form: /quit+/exit cannot be shared (the web carrier is a
// long-lived SERVER — a shared /quit would end the service, and closing the tab is
// the browser's own equivalent); /tools flips REPL state no wire affordance can
// flip back; /queue is the OPERATOR's park-a-prompt seam that the browser fills by
// POST, so one word would mean two different things; /voice and /screen already
// exist on the web carrier as BUTTONS (🎤 / 📷) with server routes behind them, and
// re-minting them as commands would be the second implementation the epic forbids.

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/tools"
)

// CommandSession is the carrier-supplied description of the session a shared
// command runs inside. Every field is a FACT the carrier already holds — nothing
// here is computed, resolved twice or cached, because a second derivation is how
// two surfaces come to describe one session differently (chat_info.go's own
// warning, applied to the seam).
//
// THE ZERO VALUE IS MEANINGFUL: a carrier that fills nothing (a test, a future
// face) leaves SessionID empty, and the commands refuse by name rather than print
// a block of "(unknown)" lines as though they had read something.
type CommandSession struct {
	// SessionID / SessionFile / SessionDir are the session logger's identity —
	// the same three values `gopi session log <id>` and `gopi chat --resume <id>`
	// take.
	SessionID   string
	SessionFile string
	SessionDir  string
	// Model is the conversation model the carrier's agent was built with.
	Model string
	// Dir / Repo / InRepo are the GROUND, resolved once by the carrier through
	// tools.ChatGroundFor (the same call `gopi chat` makes), so /info's cwd and
	// repo rows mean the same thing on both carriers.
	Dir    string
	Repo   string
	InRepo bool
	// Node is the declared node identity ("" when the topology is undeclared).
	Node string
	// ToolsOn is the carrier's CURRENT tools mode.
	ToolsOn bool
	// RawLogPath is the file this run appends its request bodies to ("" when the
	// run records none — `/context` then refuses with that reason).
	RawLogPath string
}

// slashSessionFields turns the carrier's facts into the ONE /info renderer's
// input, answering chatInfoFields directly rather than through a second struct.
func slashSessionFields(s CommandSession) chatInfoFields {
	return chatInfoFields{
		SessionID:   s.SessionID,
		SessionDir:  s.SessionDir,
		SessionFile: s.SessionFile,
		Model:       s.Model,
		Dir:         s.Dir,
		Repo:        s.Repo,
		InRepo:      s.InRepo,
		Node:        s.Node,
		ToolsOn:     s.ToolsOn,
	}
}

// slashInfoCommand is the shared table's `/info`: the SAME block the REPL prints
// (chatInfoBlock), composed from the facts the carrier supplied.
//
// A carrier that supplied no session log has no UUID to point at, so the command
// REFUSES with the reason rather than rendering chatInfoBlock's "(no session log)"
// rows — a block whose every line is a placeholder tells a reader the command ran
// and found nothing, when what actually happened is that this carrier has nothing
// to say through this door (Rule 15's criterion: name the cause).
func slashInfoCommand(_ []string, ctx CommandContext) (string, error) {
	if strings.TrimSpace(ctx.Session.SessionID) == "" {
		return "", fmt.Errorf("this carrier has no session log to describe, so /info has nothing to print")
	}
	return chatInfoBlock(slashSessionFields(ctx.Session)), nil
}

// slashContextCommand is the shared table's `/context`: parse, export, and print
// the ONE line the export returns. The argument grammar, the refusals and the
// engine call are the REPL's (`parseChatContextArgs` + chatContextExportFrom), so a
// reader who learns the spelling on one carrier has learned it on both.
func slashContextCommand(args []string, ctx CommandContext) (string, error) {
	req, err := parseChatContextArgs(args)
	if err != nil {
		return "", err
	}
	if req.Help {
		return chatContextUsage, nil
	}
	summary, err := chatContextExportFrom(ctx.Session.RawLogPath, ctx.Session.SessionFile, ctx.Session.SessionDir, req)
	if err != nil {
		return "", err
	}
	// THE SHARED TABLE'S ANSWER IS THE ONE LINE, with no notes appended: the
	// historical-collision note names a pre-axis file (`context-turn6.md`), i.e.
	// a ROUND number under the word "turn", and returning that string would put
	// the very confusion this epic removes back into the channel /context answers
	// on. See the console arm (chatContextHandleLine) for the same reasoning.
	return summary.Line(), nil
}

// slashConfigCommand is the shared table's `/config`: the SAME runtime snapshot
// the REPL arm prints (runtime_config_snapshot.go), rendered as ONE string.
//
// IT TAKES NO ARGUMENT beyond the help word, because the report describes the
// PROCESS and there is nothing to select. A carrier with no live *agent.Agent on
// this path (the web server's table) still gets the ENVIRONMENT half, which is
// where the "document states it, process does not hold it" rows live — the half
// that answered the 2026-11-12 question. That is a real answer, not a degraded
// one: the agent rows say what the request carries, the environment rows say
// what reached the process, and the second is the one only this process knows.
func slashConfigCommand(args []string, _ CommandContext) (string, error) {
	if commandHelpRequested(args) {
		return chatConfigUsage, nil
	}
	lines := renderRuntimeConfigSnapshot(buildRuntimeConfigSnapshot(nil, liveRuntimeConfigDeps(runtimeConfigKnownKeys)))
	return joinRuntimeConfigLines(lines), nil
}

// replCommandContext builds the CommandSession for the `gopi chat` REPL. It
// reads the facts through chatInfoFieldsFor — the SAME assembler /info's block
// has always used — so the table's answer and the console arm's block are one
// derivation, not two (the property TestSlashInfoMatchesTheReplBlock asserts).
func replCommandContext(ag *agent.Agent, sess *chatSession) CommandContext {
	f := chatInfoFieldsFor(ag, sess)
	cs := CommandSession{
		SessionID:   f.SessionID,
		SessionDir:  f.SessionDir,
		SessionFile: f.SessionFile,
		Model:       f.Model,
		Dir:         f.Dir,
		Repo:        f.Repo,
		InRepo:      f.InRepo,
		Node:        f.Node,
		ToolsOn:     f.ToolsOn,
	}
	if ag != nil && ag.RawLog != nil {
		cs.RawLogPath = ag.RawLog.Path()
	}
	return CommandContext{Session: cs}
}

// webCommandContext builds the CommandSession for THIS web server. It is called
// per command (cheap: three field reads and one pure ground resolution) so the
// values are read at the moment the human asked, never captured at startup — the
// tools flag and the ground are facts about now.
//
// THE GROUND IS RESOLVED, NOT RE-DERIVED: tools.ChatGroundFor is the call
// chatGroundReport makes for the console, so /info's `cwd`/`repo` rows cannot mean
// one thing in the browser and another in the terminal.
func (s *webServer) webCommandContext() CommandContext {
	cwd, _ := os.Getwd() //stylegate:ignore ChatGroundFor re-resolves an empty dir itself and names a getwd failure in report.Detail
	sess := CommandSession{Dir: cwd, ToolsOn: s.tools, Node: chatBannerNodeIdentity()}
	if ag := s.agent; ag != nil {
		sess.Model = ag.Model
		if ag.SessionLog != nil {
			sess.SessionID = ag.SessionLog.SessionID()
			sess.SessionFile = ag.SessionLog.Path()
			sess.SessionDir = chatSessionDir(ag.SessionLog)
		}
		if ag.RawLog != nil {
			sess.RawLogPath = ag.RawLog.Path()
		}
	}
	report := tools.ChatGroundFor(sess.SessionID, cwd)
	sess.Repo = report.Repo
	if !report.Degraded && report.Dir != "" {
		sess.Dir = report.Dir
		sess.InRepo = true
	}
	return CommandContext{Session: sess}
}
