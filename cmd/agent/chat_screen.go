package main

// chat_screen.go — `/screen [N]`: hand the model ONE TURN's screen as a FILE,
// instead of bytes the model is asked to swallow.
//
// THE PROBLEM IT SOLVES. This session already records every byte the renderer
// paints (uiclient.SgrStream → `<session-dir>/<sid>.screen.stream`, length-prefixed,
// written for the WHOLE run). That artifact is the right truth and the wrong
// thing to hand a model: it is a stream, it is unbounded, and pasting it into a
// turn is exactly the failure the message boundary (chat_boundary.go) exists to
// prevent. What was missing was a way to say "take the screen AS IT STOOD AT TURN
// N, put it where I can point at it, and TELL the model the path".
//
// WHY THE BARE FORM IS THE CURRENT TURN AND THERE IS NO LENGTH OPTION. A screen
// capture answers one question — "what was I looking at" — and the answer is
// always one turn's painting: the screen as it stood when that turn finished. A
// byte budget is a DIFFERENT question ("how much may I send") asked in the wrong
// place: it cut the segment mid-turn, so the file named for turn 58 held the tail
// of turn 59, and the model was told a size instead of being told which screen it
// had. So the bounds are GONE: `/screen` is the newest turn, `/screen 58` is turn
// 58, and the span is decided by the renderer's own boundary marks rather than by
// a number this command invents. Nothing is dropped from a chosen turn — a
// truncated turn would be a screen the model never saw, described as the screen
// it asked for.
//
// WHY REPL-NATIVE AND NOT A slashCommands ENTRY. The shared table (commands.go)
// is the surface BOTH carriers consult — web and chat — and it is handed nothing
// but the words typed. This command's entire subject is per-PROCESS state that
// the table cannot see: the LIVE `*uiclient.SgrStream` of a run, its path, and
// the next user turn of THIS REPL (the injection rides the message boundary,
// which exists only on the chat path). A table entry would have to be handed the
// stream and a way to reach the next message, and every other table carrier would
// then have to document a command it cannot answer — the same reasoning /info,
// /queue and /voice record, and the same shape: the word, its renderer and its
// usage text live in ONE file (this one), it joins chatSessionWords so the
// completion menu, the refusal vocabulary and /help learn it from one place, and
// it never consumes a model turn.
//
// WHERE THE TURN BOUNDARY COMES FROM. `uiclient.LocateSgrTurns` — the renderer's
// own header row (`──── turn N · <prompt>`) located by a stem DERIVED from
// lineedit.TurnHeader, with a running-maximum rule so a repainted old header is
// chrome and not a boundary. It is the package's subject, it is tested there
// against a real recording, and this file does not keep a second spelling of it.
// A recording that carries no turn header at all is an ERROR with a reason — a
// segment chosen by guesswork is worse than a refusal (取错段比取不到更糟).
//
// WHAT IT INJECTS, AND WHEN. Nothing is sent NOW: `/screen` writes a file and
// parks a one-shot note. The NEXT turn's message is what carries it, because that
// is the only moment a message exists — and the note is CONSUMED ONCE and
// cleared, so a single `/screen` cannot keep re-sending a stale path for the rest
// of the session.
//
// THE INJECTED BLOCK CARRIES FOUR THINGS AND NO BYTES: the absolute path, the
// statement that it is a screen capture of THIS session's raw screen render stream,
// the byte count it holds, and how to read it in pages. It is appended to the
// operator's own line rather than replacing it, and it is appended as a note
// ABOUT the turn, not as a payload inside it — the SGR bytes appear in the
// request NOWHERE.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The command's vocabulary. Every word the switch, the argument parser, the help
// page and the injected block compare against or print is declared once
// (Rule 1/13), so a rename cannot leave a note naming a word nothing accepts.
const (
	// chatScreenCommandName is the slash word that owns this file.
	chatScreenCommandName = "screen"
	// chatScreenOutFlag names the override destination: `--out PATH`.
	chatScreenOutFlag = "--out"
	// chatScreenHelpWord is the argument that prints this command's usage, the
	// same convention `/help screen` uses (both answer from chatScreenUsage).
	chatScreenHelpWord = "help"
	// chatScreenValueSep separates a joined flag from its value, matching the
	// chat verb's own flag spelling family (`--sgr-stream=PATH`).
	chatScreenValueSep = "="
	// chatScreenHelpFlagShort / chatScreenHelpFlagLong are the two flag spellings
	// that also answer from chatScreenUsage, so a caller reaching for a help flag
	// out of habit is not refused for it.
	chatScreenHelpFlagShort = "-h"
	chatScreenHelpFlagLong  = "--help"
	// chatScreenFlagPrefix is what makes a token a FLAG rather than the turn
	// argument. The turn is a bare number on purpose (`/screen 58`), and a token
	// beginning with a dash is never one — naming that here is what lets the
	// refusal tell "you typed a flag I do not have" apart from "you typed a turn".
	chatScreenFlagPrefix = "-"
	// chatScreenRemovedFullFlag is named ONLY so it can be refused BY NAME with
	// the reason: it was a real spelling until this change removed the bound, and
	// silently treating it as "the current turn" would give the caller a capture
	// they did not ask for while telling them nothing.
	chatScreenRemovedFullFlag = "--full"
	// chatScreenRemovedFullReason states why the flag is gone, in the refusal.
	chatScreenRemovedFullReason = "the capture is always one whole turn now — no bound, nothing truncated"
	// chatScreenMaxTurnDigits bounds the digits a turn number may occupy. It is
	// the same carry the locator scans with, and a longer run is not a number this
	// command accepts (a session cannot reach 10^9 turns).
	chatScreenMaxTurnDigits = 9
)

// The bounds that remain: none on the CONTENT, one on the NAME.
const (
	// chatScreenTurnFileNameFormat is the artifact's name when this run derived
	// it: `<session dir>/screen-turn<N>.screen.stream`. N is the turn whose painting
	// the file HOLDS — taken from the located span, never from a counter — so the
	// name and the content cannot disagree (the defect this file used to have:
	// `screen-turn58` holding turn 59's screen).
	chatScreenTurnFileNameFormat = "screen-turn%d" + uiclient.ScreenStreamSuffix
	// chatScreenFilePerm is the artifact's mode — the operator's to read, the same
	// 0644 the recording uses.
	chatScreenFilePerm = 0o644
)

// The notes this command writes to the console. Each names the file, its size and
// WHICH TURN it holds, because a capture whose artifact cannot be found — or one
// whose turn the operator has to guess — is a command that did nothing (Rule 15).
const (
	// chatScreenCaptureNote reports a capture that succeeded.
	chatScreenCaptureNote = "[gopi] screen capture → %s (%d bytes, %d frames, turn %d) — the NEXT turn's message will carry this path, not its bytes.\n"
	// chatScreenNoStreamNotice is the honest refusal-degradation when the run has
	// no recording armed (`--no-sgr-stream`, or a run whose stream failed): there
	// is nothing to capture, and saying so is the only correct answer.
	//
	// BOTH flag positions are the CONSTANT (Rule 1/13): the sentence names the OFF
	// switch twice, and a literal spelling here would be a second definition of a
	// flag whose value the parser compares on.
	chatScreenNoStreamNotice = "[gopi] /screen: this run is not recording its screen stream (%s, or the stream could not be opened), so there is nothing to capture. Start a run without %s to use /screen.\n"
	// chatScreenEmptyNotice reports an armed-but-empty stream: the file exists and
	// holds nothing yet (nothing has been painted), which is a different fact from
	// "no recording".
	chatScreenEmptyNotice = "[gopi] /screen: %s holds no painted frames yet — nothing has been drawn since the stream opened, so there is nothing to capture.\n"
	// chatScreenNoTurnsFormat is the refusal for a recording that paints frames
	// but never prints a turn header: an old stream, or a run whose turns were
	// unnumbered. A segment picked by position would be a screen nobody asked
	// about, so this REFUSES and says how many frames it did look at.
	chatScreenNoTurnsFormat = "[gopi] /screen: %s holds %d painted frame(s) but no rendered turn header, so no turn's screen can be located in it — refusing rather than handing back a segment chosen by guesswork. A recording made by a run that numbers its turns (this one) carries the marker.\n"
	// chatScreenTurnAbsentFormat refuses a turn the recording does not carry, and
	// names the range that WOULD have worked — a refusal without the alternatives
	// leaves the caller guessing at the next thing to type.
	chatScreenTurnAbsentFormat = "[gopi] /screen %d: this recording carries turns %s; turn %d is not among them.\n"
	// chatScreenRefusalFormat reports a failed capture.
	chatScreenRefusalFormat = "[gopi] /screen: %v\n"
	// chatScreenInjectionNoticeFormat confirms what the next turn will carry, so
	// the operator can predict the request instead of being surprised by it.
	chatScreenInjectionNoticeFormat = "[gopi] the next turn's message will carry a reference to that file (%d bytes of framing, no SGR content).\n"
)

// chatScreenInjectionHeader opens the injected block. It is a distinct marker
// (not the boundary's, and not a system-prompt shape) so a reader of the session
// log can see, at the first character, that this run of text was written by the
// REPL and not typed by the operator.
const chatScreenInjectionHeader = "[gopi screen capture]"

// chatScreenInjectionFormat is the block itself. Its placeholders are, in order:
// the absolute path, the byte count, the frame count, the turn number, and the
// sentence describing what the file is.
//
// The four facts the epic requires are all here and NOTHING else is: no bytes, no
// excerpt, no head of the stream. The page advice is a CONSTANT sentence rather
// than a computed number: a model reading a file chooses its own window, and
// inventing a limit here would be a number nothing enforces.
const chatScreenInjectionFormat = chatScreenInjectionHeader + ` %s — a screen capture of THIS session's raw screen render stream for turn %d, holding %d bytes of painted bytes in %d frame(s) (each frame is "<byte-count>\n<that many raw bytes>"). ` +
	`Read it with the read tool in pages (it is one file; read a window at a time rather than in one call). ` +
	`%s`

// chatScreenInjectionSpan says what the span IS, so a model knows the segment's
// edges without being told a size policy that no longer exists: it begins at the
// frame that painted turn N's header and ends where the next turn's header begins.
const chatScreenInjectionSpan = "This file holds turn %d's painting WHOLE — from the frame that drew its header to the frame before the next turn's header, nothing dropped."

// chatScreenUsage is `/help screen` and `/screen help` — the ONE definition
// (chatNativeUsage returns it, so the help page and the refusal vocabulary learn
// the word from the same place).
const chatScreenUsage = `Usage: /screen [N] [--out PATH]
Capture ONE turn's screen: the raw screen render bytes this run recorded (the same
stream ` + "`" + chatSgrStreamFlag + "`" + ` writes) are copied into a file, and the NEXT turn's
message carries that file's PATH, its byte count and how to read it — never the
bytes themselves.

  N            the turn to capture, written as a bare number: /screen 58 is turn
               58's screen. With no N, the CURRENT turn is captured — the screen
               as it stands now
  --out PATH   write the capture to PATH instead of this session's own
               ` + "`screen-turn<N>.screen.stream`" + `

The turn boundary is the renderer's OWN mark (the ` + "`──── turn N · …`" + ` header it
paints above every answer), located frame by frame — not a byte budget and not a
guess. A capture holds that turn's painting WHOLE: nothing is truncated, and
there is no way to ask for less, because a screen the model only half saw is a
screen it will describe wrongly.

A recording that carries no turn header cannot be selected from: /screen says so
and names the turns it DOES hold, rather than handing back a segment nobody asked
for. Local and immediate — never a model turn.`

// chatScreenPending is the ONE-SHOT note a capture parks for the next turn.
//
// IT IS CONSUMED ONCE (the epic's requirement, and a defect this design must not
// reintroduce): `chatScreenTakePending` returns the text and CLEARS the slot in
// the same call, so the block rides exactly one message. A note left in place
// would re-send a stale path on every later turn — a reference to a file whose
// content has moved on, which is worse than no reference at all.
//
// It lives on chatSession (REPL state), not on the agent: the injection point is
// the message boundary, which is this carrier's.
func (s *chatSession) chatScreenTakePending() string {
	if s == nil || s.screenPending == "" {
		return ""
	}
	out := s.screenPending
	s.screenPending = ""
	return out
}

// chatScreenInject appends the parked capture note to the turn's message and
// reports whether it did.
//
// THE OPERATOR'S OWN LINE IS KEPT BYTE-IDENTICAL AT ITS START. The block is
// APPENDED (separated by a blank line), because the operator's words are the
// instruction and the reference is material: an instruction with a reference in
// front of it is a message whose first bytes are not the question asked. An empty
// operator line (a bare `/screen` followed by a bare Enter) would otherwise send
// nothing at all, so the reference alone is sent in that case — and the statement
// that there is no instruction rides with it.
func chatScreenInject(out chatOutbound) (chatOutbound, bool) {
	note := out.screenNote
	if note == "" {
		return out, false
	}
	if strings.TrimSpace(out.Message) == "" {
		out.Message = note
	} else {
		out.Message = out.Message + chatScreenInjectSeparator + note
	}
	return out, true
}

// chatScreenInjectSeparator separates the operator's own words from the appended
// reference. Two newlines: the block is material ABOUT the turn, and a reader of
// the request can see where the operator stopped and the console spoke.
const chatScreenInjectSeparator = "\n\n"

// chatScreenRequest is one parsed `/screen` argument list.
type chatScreenRequest struct {
	// Turn is the turn to capture; 0 means "the current turn". A turn number is a
	// POSITION the operator reads off the screen, so 0 is not a turn and is
	// refused rather than silently read as "the newest".
	Turn int
	// Path is the `--out` override, "" when the caller did not name one.
	Path string
	// Help asks for the usage block (`/screen help`).
	Help bool
}

// parseChatScreenArgs parses the argument list. Every word it does not accept is
// REFUSED BY NAME (never ignored): a caller who typed `--outt=x` believes they
// chose a destination, and a caller who typed `58x` believes they chose a turn —
// silently capturing something else would file their evidence, or their screen,
// somewhere they will not look (Rule 15).
//
// THE TURN IS A POSITIONAL AND THERE IS AT MOST ONE. Two turn numbers is a
// contradiction (which screen did you want?), and it is refused rather than
// resolved by taking the first.
func parseChatScreenArgs(args []string) (chatScreenRequest, error) {
	var req chatScreenRequest
	turnSet := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == chatScreenHelpWord || a == chatScreenHelpFlagShort || a == chatScreenHelpFlagLong:
			req.Help = true
		case a == chatScreenRemovedFullFlag:
			return req, fmt.Errorf("%s was removed: %s — usage: /screen [N] [--out PATH]", chatScreenRemovedFullFlag, chatScreenRemovedFullReason)
		case a == chatScreenOutFlag:
			if i+1 >= len(args) {
				return req, fmt.Errorf("%s needs a PATH", chatScreenOutFlag)
			}
			i++
			req.Path = args[i]
		case strings.HasPrefix(a, chatScreenOutFlag+chatScreenValueSep):
			req.Path = strings.TrimPrefix(a, chatScreenOutFlag+chatScreenValueSep)
			if req.Path == "" {
				return req, fmt.Errorf("%s needs a PATH", chatScreenOutFlag)
			}
		case strings.HasPrefix(a, chatScreenFlagPrefix):
			// A flag this command does not have. Named, with the two flags it does.
			return req, fmt.Errorf("unknown argument %q — usage: /screen [N] [--out PATH]", a)
		default:
			turn, err := chatScreenParseTurn(a)
			if err != nil {
				return req, err
			}
			if turnSet {
				return req, fmt.Errorf("two turn numbers (%d and %d) — /screen takes ONE: the screen of a single turn", req.Turn, turn)
			}
			req.Turn = turn
			turnSet = true
		}
	}
	return req, nil
}

// chatScreenParseTurn reads the ONE positional argument: a bare decimal turn
// number, 1 or greater.
//
// EVERY OTHER SHAPE IS REFUSED BY ITS OWN NAME, and the three that will really be
// tried are refused separately because they mean different things:
//
//	`0`    a real number, but not a turn — a position cannot be zero, and reading
//	       it as "the newest" would give the caller a screen they did not name.
//	`-1`   a number, with a sign this command does not take (there is no "count
//	       back from the end"; the answer to that is a turn number you read off
//	       the screen).
//	`58x`  not a number at all — a typo, and the character that makes it one is
//	       named in the refusal.
func chatScreenParseTurn(arg string) (int, error) {
	if arg == "" {
		return 0, fmt.Errorf("an empty turn argument — /screen takes a number, or nothing for the current turn")
	}
	sign := ""
	digits := arg
	if strings.HasPrefix(digits, "+") || strings.HasPrefix(digits, "-") {
		sign = digits[:1]
		digits = digits[1:]
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, fmt.Errorf("%q is not a turn number — /screen takes a whole number (e.g. /screen 58), or nothing for the current turn", arg)
		}
	}
	if len(digits) > chatScreenMaxTurnDigits {
		return 0, fmt.Errorf("%q is not a turn number — past %d digits", arg, chatScreenMaxTurnDigits)
	}
	if digits == "" {
		return 0, fmt.Errorf("%q is not a turn number — it carries a sign and no digits", arg)
	}
	if sign == "-" {
		return 0, fmt.Errorf("%q is not a turn number — there is no counting back from the end; name the turn you want, or pass none for the current one", arg)
	}
	n := 0
	for i := 0; i < len(digits); i++ {
		n = n*10 + int(digits[i]-'0')
	}
	if n == 0 {
		return 0, fmt.Errorf("%q is not a turn number — turns are numbered from 1; pass no argument for the CURRENT turn", arg)
	}
	return n, nil
}

// chatScreenHandleLine is the REPL arm's whole body: parse, capture, park the
// note, report. It never calls the model and never blocks on one.
func chatScreenHandleLine(input *chatInput, ag *agent.Agent, sess *chatSession, msg string) {
	args := strings.Fields(strings.TrimSpace(msg))
	if len(args) > 0 {
		// Drop the command word itself: `Fields` of "/screen 58" starts at the
		// word the switch already matched.
		args = args[1:]
	}
	req, err := parseChatScreenArgs(args)
	if err != nil {
		input.WriteNotice(fmt.Sprintf(chatScreenRefusalFormat, err))
		return
	}
	if req.Help {
		input.WriteLines(chatScreenUsage)
		return
	}
	note, console, err := chatScreenCapture(ag, sess, req)
	if err != nil {
		input.WriteNotice(fmt.Sprintf(chatScreenRefusalFormat, err))
		return
	}
	if note == "" {
		// The capture reported its own reason on the console (no stream, or one
		// with no frames) and parked nothing: a reference to a file with no
		// frames would be a promise the model cannot use.
		input.WriteNotice(console)
		return
	}
	sess.screenPending = note
	input.WriteNotice(console)
	input.WriteNotice(fmt.Sprintf(chatScreenInjectionNoticeFormat, len(note)))
}

// chatScreenCapture writes the artifact and returns (a) the block to inject next
// turn, (b) the console note, or an error.
//
// The two empty-note cases (no stream armed, an empty stream) are NOT errors:
// nothing was asked for that the run could not do — it has nothing to capture —
// and turning that into a refusal would put a scary line where an honest
// statement belongs. They return an empty note plus the sentence to print.
func chatScreenCapture(ag *agent.Agent, sess *chatSession, req chatScreenRequest) (string, string, error) {
	if sess == nil || sess.sgr == nil || sess.sgr.Path() == "" {
		return "", fmt.Sprintf(chatScreenNoStreamNotice, chatNoSgrStreamFlag, chatNoSgrStreamFlag), nil
	}
	src := sess.sgr.Path()
	// The stream is being APPENDED while this command runs (the console is
	// painting as we speak), so the frame count is a moving fact. The locator
	// tolerates a cut mid-frame by design, which is exactly right for a live
	// artifact: the capture is a snapshot of what had been painted at the instant
	// it was taken.
	turns, err := uiclient.LocateSgrTurns(src)
	if err != nil {
		return "", "", err
	}
	if turns.StreamFrames == 0 {
		return "", fmt.Sprintf(chatScreenEmptyNotice, src), nil
	}
	if len(turns.Spans) == 0 {
		// A recording with frames but no turn header. Nothing here can be selected
		// HONESTLY, so the answer is the reason and the count, not a segment.
		return "", fmt.Sprintf(chatScreenNoTurnsFormat, src, turns.StreamFrames), nil
	}
	var span uiclient.SgrTurnSpan
	if req.Turn == 0 {
		// THE BARE FORM IS THE NEWEST TURN: the last painting on the screen is the
		// turn that just finished, which is the screen the operator is looking at.
		cur, ok := turns.Current()
		if !ok {
			return "", fmt.Sprintf(chatScreenNoTurnsFormat, src, turns.StreamFrames), nil
		}
		span = cur
	} else {
		got, ok := turns.Span(req.Turn)
		if !ok {
			// REFUSED BY NAME, with the range that would have worked: the recording
			// does not hold that turn, and capturing a neighbour instead would be
			// the exact defect this command exists to end.
			return "", fmt.Sprintf(chatScreenTurnAbsentFormat, req.Turn, turns.RangeWords(), req.Turn), nil
		}
		span = got
	}
	dest := req.Path
	if dest == "" {
		dest = chatScreenDerivePath(sess, span.Turn)
		if dest == "" {
			return "", "", fmt.Errorf("this session has no directory to write the capture into; pass %s PATH", chatScreenOutFlag)
		}
	}
	if mkerr := scratch.MkdirAllWithSuiteRunOwnerPerm(filepath.Dir(dest), dumpDirPerm); mkerr != nil {
		return "", "", fmt.Errorf("cannot create the capture's directory %s: %w", filepath.Dir(dest), mkerr)
	}
	rng, frames, err := uiclient.ReadSgrTurnFile(src, span)
	if err != nil {
		return "", "", err
	}
	if rng.Frames == 0 {
		// The span the locator named held no frames by the time it was copied (the
		// stream moved under us). Saying so beats writing an empty file.
		return "", fmt.Sprintf(chatScreenEmptyNotice, src), nil
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, chatScreenFilePerm)
	if err != nil {
		return "", "", fmt.Errorf("cannot write the capture %s: %w", dest, err)
	}
	written, nframes, werr := uiclient.WriteSgrPrefix(f, rng.Meta, frames)
	cerr := f.Close()
	if werr != nil {
		return "", "", werr
	}
	if cerr != nil {
		return "", "", fmt.Errorf("cannot close the capture %s: %w", dest, cerr)
	}
	console := fmt.Sprintf(chatScreenCaptureNote, dest, written, nframes, span.Turn)
	note := fmt.Sprintf(chatScreenInjectionFormat, dest, span.Turn, written, nframes, fmt.Sprintf(chatScreenInjectionSpan, span.Turn))
	return note, console, nil
}

// chatScreenDerivePath composes the default destination: this session's own
// directory, named for the TURN THE FILE HOLDS.
//
// THE TURN COMES FROM THE SPAN, NEVER FROM A COUNTER. The old spelling named the
// file after `chatTurnDisplay(0)` — this session's TURN BASELINE, a number about
// how many turns existed BEFORE this run — while the content was the newest
// frames, i.e. whatever turn the run had reached. Measured: `screen-turn58`
// holding turn 59's screen. A name is a claim about the bytes beside it, so it is
// taken from the bytes.
func chatScreenDerivePath(sess *chatSession, turn int) string {
	if sess.sessionDir == "" {
		return ""
	}
	return filepath.Join(sess.sessionDir, fmt.Sprintf(chatScreenTurnFileNameFormat, turn))
}
