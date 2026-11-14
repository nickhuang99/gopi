package main

// chat_notice_sink.go — WHERE THIS RUN'S OWN DRIVER NOTICES GO, and why that is
// not always os.Stderr.
//
// THE MEASURED DEFECT (2026-10-04). `gopi chat --replay <id> --film DIR --out
// f.gif` produced a GIF whose frames carried LOG TEXT — `[gopi replay] turn 2/4:
// typing the recorded prompt` and `[gopi] chat table: chat row ensured:
// 8293257b-…` — drawn inside the picture the operator was handed, which is one
// half of the rejection ("多了描述文字…不像 gopi 聊天界面"). The mechanism is not
// the caption band (that half is gone, chat_film_gif.go) and not the grab: the
// driver's notices reach the terminal as BARE writes to os.Stderr, with no
// cursor-positioning sequence ahead of them, so they land wherever the terminal's
// cursor happens to be — which, once the reserved chat layout is installed and
// painting rows at ABSOLUTE positions, is a row the layout owns. The producer's
// own raw stream shows it exactly: `[gopi] chat table: …` appears with no
// preceding `\e[r;cH` (measured byte offset 115131 of producer.log, between two
// layout repaints).
//
// WHY THE FIX IS A DESTINATION AND NOT A FILTER. The notices are diagnostics —
// they say what the replay is doing — and a film must record them (Rule 15: a
// run whose reason for acting is silently dropped is indistinguishable from one
// that never had one). What must not happen is the notice becoming PIXELS OF THE
// DELIVERABLE. So the criterion is:
//
//	when this process is RE-RUNNING A RECORDING (internal/replaymode.Active),
//	the driver's notices go to a file this run owns — console.log beside the
//	session record when there is one (a film's producer), otherwise a file in the
//	chat's own scratch class — and NOT to the screen that is drawing somebody
//	else's conversation.
//
// THE CRITERION IS "THIS PROCESS IS A REPLAY", NOT "THIS PROCESS IS A FILM'S
// PRODUCER", and that is the 2026-11-04 correction. The film shape was the one
// that had been measured (a photographed terminal), so the route was installed
// where the beat protocol is up — inside the film branch — and a plain
// `gopi chat --replay <id>` therefore kept os.Stderr and printed the whole
// `[gopi replay] …` family onto the operator's screen (measured: fifteen lines,
// including the refusal catalogue and the injection budget). A film is one
// CONSUMER of a replay; the criterion must be the replay itself, and
// internal/replaymode is the package that already answers exactly that question
// (Rule 13 — one question, one place), which is why the sink lives there.
//
// A run that is NOT a replay keeps the historic behaviour unchanged (os.Stderr,
// where an operator watching a live chat reads it).
//
// WHY THE DESTINATION IS PROCESS STATE AND NOT A PARAMETER. The writers are in
// two packages and three call paths (chat_replay.go's driver, internal/replaydrive's
// stub, the beat driver's own report), some of them running on a goroutine that
// started before the screen was taken. A parameter would have to be threaded
// through all of them and would still be unable to reach the stub, which lives
// behind an io.Writer interface — while chatDiagnosticsFile (the neighbouring
// redirect, opened by the same run for the same screen) is already package
// state with the same single-run rationale. One run, one process, one sink.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
)

// chatNoticeSink is the CURRENT destination as an io.Writer, resolved per write
// so a route installed after a writer was handed out still takes effect. It is
// what an interface-typed consumer (internal/replaydrive's stub, which cannot
// import this package) is given, instead of a snapshot that would freeze the
// pre-route destination.
type chatNoticeSink struct{}

// Write resolves the sink at write time and forwards the bytes unchanged.
func (chatNoticeSink) Write(p []byte) (int, error) {
	return chatNoticeWriter().Write(p)
}

// chatNoticeWriter reports where a notice written RIGHT NOW should go.
//
// THE DESTINATION BELONGS TO internal/replaymode, and that is the 2026-11-04
// correction rather than a tidy-up. The previous version kept the sink HERE as
// its own package variable, and its ONLY route was installed inside the film
// branch of the REPL (`cli_chat.go`, the `beatDriver != nil` block) — so on a
// plain `gopi chat --replay <id>` the sink stayed os.Stderr and the whole
// `[gopi replay] …` family (the driver's script, the stub's binding, the
// injection budget, the refusal catalogue) printed onto the operator's screen,
// which is a picture of SOMEBODY ELSE'S session (measured: 15 lines). The
// criterion was never "this process is a film's producer" — a film is one
// CONSUMER of a replay — it is "this process is a REPLAY", the same one question
// internal/replaymode already answers (Rule 13). So the sink lives beside that
// answer, and the writers in internal/agent (rawlog, session mint, slot
// registry) can reach it too, instead of each printing to os.Stderr.
func chatNoticeWriter() io.Writer {
	return replaymode.NoticeSinkWriter()
}

// chatNoticef writes one driver notice (a format string and its arguments) to
// the current sink, so every notice of this run goes to ONE destination decided
// in ONE place (Rule 2). It replaces the bare `fmt.Fprintf(os.Stderr, …)` calls
// the driver used to make, which is the whole of the fix: the words are not
// changed, only where they land.
func chatNoticef(format string, args ...any) {
	_, _ = fmt.Fprintf(chatNoticeWriter(), format, args...) //stylegate:ignore a failed write to the run's own notice channel cannot be reported: the channel IS the report
}

// chatReplayNoticeLogDirPattern names the scratch directory a REPLAY's driver
// notices are written into when the run has no session record to sit beside (a
// plain `--replay` mints none — internal/replaymode refuses the mint by name).
// It is the chat's own scratch class, the same one the replay's screen dump and
// raw screen stream use (Rule 22: never `/`, and swept by `gopi tool scratch-gc`).
const (
	chatReplayNoticeLogDirPattern = "chat-replay-notices"
	chatReplayNoticeLogName       = "driver-notices.log"
)

// routeChatNoticesForReplay installs the destination a REPLAY's own diagnostics
// must go to, and returns it. THE CRITERION IS "THIS RUN IS A REPLAY" — not
// "this run is a film's producer" — which is the whole of the 2026-11-04
// correction: a plain `gopi chat --replay <id>` wrote the entire `[gopi replay]`
// family (fifteen lines: the driver's script, the stub's binding, the injection
// budget, the refusal catalogue) onto the operator's screen, because the only
// route installed in the REPL sat inside the film branch.
//
// TWO DESTINATIONS, ONE RULE, and the choice is a fact about the run rather than
// a preference:
//
//   - a run WITH a session record (the film's producer has one) writes beside it,
//     into the console.log cmd/agent/chat_diagnostics.go already opens for the
//     agent's own notices — the historic filmed destination, unchanged;
//   - a run WITHOUT one (a plain replay, whose mint internal/replaymode refuses)
//     gets a file in the chat's own scratch class, the same shape the replay's
//     dump and SGR stream already take. A replay adds NO new artifact class and
//     no new dependency (Rule 35): `gopi tool scratch-gc` already sweeps it.
//
// A destination that cannot be opened is an ERROR the caller announces (Rule 15):
// the notices then stay on the screen, which is worse but STATED, never a silent
// fallback that looks exactly like a run with nothing to report.
func routeChatNoticesForReplay() (string, func(), error) {
	path, err := chatReplayNoticeLogPath()
	if err != nil {
		return "", func() {}, err
	}
	f, oerr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, consoleLogMode)
	if oerr != nil {
		return "", func() {}, fmt.Errorf("open %s for this replay's driver notices: %w", path, oerr)
	}
	inner := replaymode.SetNoticeSink(f)
	return path, func() {
		inner()
		if cerr := f.Close(); cerr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "[gopi] chat: closing the driver-notice log %s: %v\n", path, cerr) //stylegate:ignore the run is ending either way; recording it is the point (Rule 15a)
		}
	}, nil
}

// chatReplayNoticeLogPath is the ONE destination a replay's notices have when the
// run keeps no session record: a directory of the chat's own scratch class. It is
// a separate function for the reason chatReplayDumpPath is one — a reader can
// check ONE name, and the exhaustiveness gates can assert the answer is outside
// the recording tree rather than re-deriving the resolver's precedence.
func chatReplayNoticeLogPath() (string, error) {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, chatReplayNoticeLogDirPattern)
	if err != nil {
		return "", fmt.Errorf("a replay's driver notices need a scratch destination, and none could be created (they are never written into the recording's session directory): %w", err)
	}
	return filepath.Join(dir, chatReplayNoticeLogName), nil
}

// routeChatNoticesToConsoleLog points this run's driver notices at the session's
// own console.log — the file cmd/agent/chat_diagnostics.go already opens for the
// agent's notices — and returns the path plus the restore. It is the FILM's
// destination once the beat protocol is up (the producer has a session record),
// and the reason is in this file's header: the words are diagnostics the run must
// keep, and a record beside the session is where a reader already looks.
//
// It reports "" (and a no-op restore) when this run has no session log: a run
// whose notices have no record keeps whatever destination it already had.
func routeChatNoticesToConsoleLog(ag *agent.Agent) (string, func(), error) {
	if ag == nil || ag.SessionLog == nil {
		return "", func() {}, nil
	}
	path := filepath.Join(sessionlayout.RecordContainer(ag.SessionLog.Path()), consoleLogFileName)
	f, oerr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, consoleLogMode)
	if oerr != nil {
		return "", func() {}, fmt.Errorf("open %s for this run's driver notices: %w", path, oerr)
	}
	// The route's own restore is captured in a LOCAL and called from the returned
	// closure. Returned directly instead, the closure would capture ITSELF (the
	// restore slot it closes over is the one it is being assigned to) and the
	// first call would recurse until the stack died — measured: a stack overflow,
	// not a hang.
	inner := routeChatNotices(f)
	return path, func() {
		inner()
		if cerr := f.Close(); cerr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "[gopi] chat: closing the driver-notice log %s: %v\n", path, cerr) //stylegate:ignore the run is ending either way; recording it is the point (Rule 15a)
		}
	}, nil
}

// routeChatNotices points this process's driver notices at w and returns the
// restore that puts the previous destination back. The previous one is returned
// rather than assumed to be os.Stderr — a caller that had already routed the
// notices keeps its route while this run's screen is not being filmed, the same
// discipline installChatFlowSeam keeps for the agent's logger.
//
// A nil w is REFUSED by name: a route to nowhere would delete the diagnostics
// instead of moving them (Rule 15).
func routeChatNotices(w io.Writer) (restore func()) {
	return replaymode.SetNoticeSink(w)
}
