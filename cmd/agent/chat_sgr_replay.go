package main

// chat_sgr_replay.go — `gopi chat --sgr-replay <SESSION-ID|FILE.sgr.stream>`: play
// back the RAW render bytes that `--sgr-stream` recorded, byte for byte.
//
// WHAT QUESTION THIS ANSWERS, AND WHY NOTHING ELSE CAN. `gopi chat --replay` re-RUNS
// a session: it re-types the recording and repaints the screen through THIS build's
// renderer, so its output is a function of the session AND of the algorithm that is
// installed today — the turn numbering, the cache rows, the layout, the throttle.
// Two different builds replay the same session and print different bytes, correctly.
// The SGR stream is the one artifact that is NOT a rendering of anything: it is the
// bytes the renderer wrote to the terminal at the time, recorded verbatim and
// unparsed (internal/uiclient/sgrstream.go). Playing it back therefore measures the
// ARTIFACT, not the interpreter: any build, on any machine, replaying one stream
// must produce the same bytes, and the sha256 of the two runs must be equal. That is
// the only evidence channel that does not depend on the render algorithm, and it is
// what this verb adds.
//
// IT IS A DIFFERENT AXIS FROM --replay, so it is a different flag and not a mode of
// that one: --replay needs a session ledger, a checkpoint, a model stub and a
// driver; this needs a file and a terminal. Sharing a verb would make an artifact
// comparison pay for machinery that cannot affect its answer.
//
// THE PICTURE CARRIES NO TEXT OF ITS OWN. Every OSD line `chatReplayControl` would
// print (PAUSED / RESUMED / LEAVING) is DROPPED here, because the product of this
// verb is the recording's own bytes: a line of ours written between two frames is a
// byte the comparison would see and the recording never contained. The state is not
// hidden by that — a paused replay's stdout stops growing, which is a fact a
// watcher (and a test) can measure, and it is the exact thing a pause is FOR. Two
// exceptions exist, and both are printed on STDERR, after the picture is complete:
// a stream that cannot be read at all (the operator gets the reason and a non-zero
// status), and a stream that ends without its close-out record (the picture is
// genuine but partial, and the run that recorded it died mid-frame). Rule 15 is
// about failures not being swallowed; it is not a licence to paint over the
// artifact with narration, and both exceptions keep their evidence.
//
// CONTROLS ARE THE REPLAY'S, NOT A SECOND INVENTED SET: this file installs no key
// bindings. It decodes the terminal's bytes with lineedit's own decoder and offers
// every key to the ONE state machine `chat_replay_control.go` already owns — any
// key toggles the pause, Esc leaves — so the vocabulary cannot drift between the two
// playback surfaces.
//
// THROTTLE. The recording is played at chatSgrReplayFrameDelay per frame so a human
// can watch it; the ORIGINAL timing is deliberately not reproduced (a stream holds
// no timestamps, and inventing them would be a rendering of its own). The delay
// applies only when a terminal is watching: with stdin redirected there is nobody to
// watch, the picture is being captured or hashed, and paying 8ms × ~29k frames would
// turn a two-second artifact read into a four-minute wait with no reader.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/uiclient"
)

// The flag's own vocabulary (Rule 1b/13): the parser, the usage page, the refusals
// and the tests all reference these, so a second spelling of the flag cannot appear.
const (
	// chatSgrReplayFlag is `--screen-replay <SESSION-ID|FILE>`. It takes its value
	// in the next slot or joined, the same two spellings `--replay` accepts — the
	// value is a name or a path a human types, and both shapes are how those are
	// written.
	//
	// The identifier keeps its `Sgr` spelling for the same reason
	// chatSgrStreamFlag does (chat_sgr_stream.go): the VALUE lives once, in
	// internal/cliverbs (Rule 13), and what was renamed is the world-facing word —
	// the recorder, the reader and the reports are all still `SgrStream`.
	chatSgrReplayFlag = cliverbs.ScreenReplay
	// chatSgrReplayDeprecatedFlag is the PRE-RENAME spelling. It still PARSEs and
	// prints cliverbs.DeprecatedFlagNoticeFormat, so a script written before the
	// rename keeps working and is told the current word in the same breath.
	chatSgrReplayDeprecatedFlag = cliverbs.SgrReplay
)

// The playback's own numbers. Named (Rule 1) because they are the verb's behaviour:
// the pace a human watches at, the granularity at which a pause is noticed, the
// window that tells a lone Esc from the head of an arrow sequence, and the read size.
const (
	// chatSgrReplayFrameDelay is the pace ONE frame is written at when a terminal is
	// watching. It is a thumb, not a measurement: the stream records bytes, not time,
	// so the original pace is not recoverable in principle (chat_sgr_replay.go's
	// header states this), and a replay that claimed to reproduce it would be
	// fabricating one.
	chatSgrReplayFrameDelay = 8 * time.Millisecond
	// chatSgrReplayTick is how often the driver wakes to notice a pause or a quit
	// while it is inside a frame gap. A smaller number makes the controls feel more
	// immediate and costs more wake-ups; a frame's own write is never sliced.
	chatSgrReplayTick = time.Millisecond
	// chatSgrReplayEscapeWindow is how long a lone ESC is held before it is resolved
	// AS an Escape. ESC is also the first byte of every arrow and function key, and
	// this verb's only Escape binding is "leave" — so the window has to be long
	// enough to let a real sequence complete, and short enough that a watcher's Esc
	// does not feel ignored. It is the same shape the editor uses for the same
	// reason (lineedit.Decoder.BareEscape).
	chatSgrReplayEscapeWindow = 30 * time.Millisecond
	// chatSgrReplayReadBufBytes bounds one read of the watcher's keys.
	chatSgrReplayReadBufBytes = 4096
	// chatSgrReplayBlockMS is the read loop's poll timeout: negative means "block
	// until something happens", which is what a reader with no other work to do
	// wants (it is the same value lineedit's own loop passes).
	chatSgrReplayBlockMS = -1
)

// The verb's own sentences. They are constants because the parser, the runner and
// the tests must not be able to disagree about what the refusal says.
const (
	// chatSgrReplayNeedsTarget is the missing-value refusal. It names BOTH forms the
	// operand can have, because the operand is the whole interface: a reader who does
	// not know a session id works here would go looking for a file name.
	chatSgrReplayNeedsTarget = "%s takes a session id or a %s path — write %s <SESSION-ID|FILE%s>"
	// chatSgrReplayNoStreamFormat is the session-id form's failure: the id resolved
	// (or did not), and the path it WOULD have used is printed so the operator can
	// see both the expectation and where to look.
	chatSgrReplayNoStreamFormat = "no %s stream for %q — expected %s; a session records one only when it was started with %s or %s"
	// chatSgrReplayMissingFileFormat is the path form's failure. The path is quoted
	// verbatim and nothing is substituted: the operator typed it.
	chatSgrReplayMissingFileFormat = "no %s stream at %s"
	// chatSgrReplayUnreadableFormat is the "the file is there and cannot be read"
	// case, which is a different finding from "there is no file" and therefore gets
	// its own sentence (Rule 15). It wraps with %w on BOTH call sites (the open and
	// the close), each reached only under its own `err != nil`, so the value is an
	// error by construction: %w can never render `%!w(<nil>)`, the sentence is
	// byte-identical, and the cause stays reachable through errors.Is/As (Rule 16).
	chatSgrReplayUnreadableFormat = "cannot read the %s stream at %s: %w"
	// THE ENDING'S THREE SENTENCES, and there are three because the file can end
	// in three DIFFERENT ways (uiclient.SgrStreamEnd): it reached its close-out
	// record, it is whole but was never closed, or its own records contradict its
	// own length. They are printed AFTER the picture, on stderr, and they are the
	// only notices this verb may add to a capture.
	//
	// WHY THREE AND NOT ONE (measured 2026-11-08, bug 01a1b823-5070). This verb
	// used to decide the whole thing from `meta.Trailer == false` and print one
	// sentence naming a TRUNCATION. A census of every stream on this host found 14
	// with no trailer and a record-boundary walk measured `remaining=0` on all 14 —
	// the recordings were whole, the close-out record was missing. The old sentence
	// told an operator their bytes were cut when the bytes were fine (Rule 15: two
	// different failures said as one).
	//
	// SENTENCE 1 — UNCLOSED, and the last frame ended on its own boundary. Nothing
	// above is missing: the picture is the whole recording, and what failed is the
	// writer's close-out.
	chatSgrReplayUnclosedWholeFormat = "%sthe recording is whole — every frame above ends on a record boundary — but its recording run never wrote the close-out record, so nothing says the run finished. Nothing is truncated; the artifact is simply unclosed\n"
	// SENTENCE 2 — UNCLOSED, and the last frame was announced but is not all there.
	// Offset is where that frame begins, in bytes from the start of the file.
	chatSgrReplayUnclosedCutFormat = "%sthe stream ends inside a frame at byte %d — its recording run never reached close-out, so the picture above is what reached that terminal and no more\n"
	// SENTENCE 3 — the FILE contradicts itself. The findings are quoted from the
	// reader (: SgrStreamContradiction.String), so the sentence names the offset and
	// the measured size rather than describing a suspicion.
	chatSgrReplayContradictionFormat = "%sthe recording contradicts itself (%d byte(s) on disk): %s. This is a fact about the FILE, not about the code that reads or writes it, so the picture above is not evidence about either\n"
	// chatSgrReplayEndUnreadableFormat is the ending scanner's own failure. It is
	// separate from the picture's reader because the two answer different questions
	// ("what were the frames" vs "how did the file end"), and a scan that could not
	// run must not be reported as any of the three states (Rule 15).
	chatSgrReplayEndUnreadableFormat = "%scannot tell how the recording at %s ends: %v\n"
	// chatSgrReplayConflictFormat refuses a command line that names TWO things to
	// run. One run plays one thing; accepting both would silently pick one.
	chatSgrReplayConflictFormat = "%s plays a recorded byte stream and cannot be combined with %s — one run replays one thing"
	// chatSgrReplayBadStreamFormat wraps the reader's own refusal (not a screen stream,
	// an unknown format version, a corrupt frame prefix). The reader's sentence is
	// quoted whole rather than paraphrased, because the reader is what knows which of
	// those findings it made.
	// %w, not %v: the reader's refusal is an error by construction (it is returned,
	// not read off a field), so the chain survives and the wording does not move.
	chatSgrReplayBadStreamFormat = "the recording cannot be played: %w"
	// chatSgrReplayWriteFormat is a failed write to stdout — a closed pipe, a gone
	// terminal. It ends the run with a non-zero status: the picture did not reach
	// anyone, and reporting "done" would be a lie about the artifact.
	chatSgrReplayWriteFormat = "cannot write the %s stream to stdout: %v"
	// chatSgrReplayNoArchiveFormat states — BEFORE the picture, on stderr, once —
	// that the recording switch has nothing to record here. It is a statement and not
	// a refusal: the caller's playback is exactly what they asked for, and the only
	// wrong outcome would be silence about an artifact that will not appear.
	chatSgrReplayNoArchiveFormat = "%s%s is not armed for this run: a playback writes no chat surface, so there is nothing to record (the switch applies to a session run)\n"
)

// chatSgrReplayTarget is the parsed request: the operand exactly as given, plus the
// fact that the flag was present at all. A bare `--sgr-replay` is a usage error, so
// "present with no operand" never reaches the runner — but the zero value still has
// to mean "not asked for", which is why presence is its own bit.
type chatSgrReplayTarget struct {
	target string
	set    bool
}

// parseChatSgrReplayArg handles ONE argv element in the flag's vocabulary. Both
// spellings work (`--sgr-replay=X`, `--sgr-replay X`) because the operand is a name
// or a path a human types, unlike `--sgr-stream`'s joined-only PATH rule (that flag's
// remaining word is a session seed, so a separated value would be misread; here the
// value is consumed before the positional handler).
func parseChatSgrReplayArg(a, next string, hasNext bool) (t chatSgrReplayTarget, consumed bool, err error) {
	return parseChatSgrReplayArgWithNotice(a, next, hasNext, nil)
}

// parseChatSgrReplayArgWithNotice is parseChatSgrReplayArg with the DEPRECATION
// line routed to `warn` (nil = the silent reader: a test, the known-word probe).
// The alias arm translates the word ONCE, at the top, so the rest of the function
// sees exactly one spelling — a second arm further down is how the two spellings
// would start behaving differently.
func parseChatSgrReplayArgWithNotice(a, next string, hasNext bool, warn io.Writer) (t chatSgrReplayTarget, consumed bool, err error) {
	if a == chatSgrReplayDeprecatedFlag || strings.HasPrefix(a, chatSgrReplayDeprecatedFlag+chatFlagValueSep) {
		if warn != nil {
			fmt.Fprintf(warn, cliverbs.DeprecatedFlagNoticeFormat, chatSgrReplayDeprecatedFlag, chatSgrReplayFlag)
		}
		a = chatSgrReplayFlag + strings.TrimPrefix(a, chatSgrReplayDeprecatedFlag)
	}
	if a != chatSgrReplayFlag && !strings.HasPrefix(a, chatSgrReplayFlag+chatFlagValueSep) {
		return chatSgrReplayTarget{}, false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return chatSgrReplayTarget{}, true, chatSgrReplayNeedsTargetErr()
		}
		return chatSgrReplayTarget{target: inline, set: true}, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return chatSgrReplayTarget{}, true, chatSgrReplayNeedsTargetErr()
	}
	return chatSgrReplayTarget{target: next, set: true}, true, nil
}

// chatSgrReplayNeedsTargetErr renders the missing-value refusal. One definition, so
// both spellings of the parse path produce the SAME sentence (Rule 13).
func chatSgrReplayNeedsTargetErr() error {
	return fmt.Errorf(chatSgrReplayNeedsTarget, chatSgrReplayFlag, uiclient.ScreenStreamSuffix,
		chatSgrReplayFlag, uiclient.ScreenStreamSuffix)
}

// chatSgrReplayConflict refuses a command line that names TWO things to run. One
// run plays one thing, and silently picking one of the words would be the quiet drop
// this verb's refusals exist to prevent (Rule 15). The checks are in the CLI's own
// order: the recording, then a continued session, then a film.
func chatSgrReplayConflict(replayTarget string, resumed bool, seed string, filmSet bool) error {
	switch {
	case replayTarget != "":
		return fmt.Errorf(chatSgrReplayConflictFormat, chatSgrReplayFlag, chatFlagReplay)
	case resumed || seed != "":
		return fmt.Errorf(chatSgrReplayConflictFormat, chatSgrReplayFlag, chatFlagResume)
	case filmSet:
		return fmt.Errorf(chatSgrReplayConflictFormat, chatSgrReplayFlag, chatFlagFilm)
	}
	return nil
}

// chatResolveSgrReplayPath turns the operand into the stream file it names. Two
// forms, and the SHAPE is what decides which one is tried:
//
//	① an existing regular file — used as given. A path the operator typed is never
//	   re-derived: re-deriving it would silently play a different file than the one
//	   they named (the same rule chatResolveSgrPath's explicit branch keeps);
//	② anything else — read as a SESSION: resolve the session the way every other
//	   session verb does (`findSessionLogPath`, the shared resolver, so this verb and
//	   `gopi session log` cannot disagree about one operand) and then name the stream
//	   beside that session's record, composed from the SAME pair the writer used
//	   (sessionlayout.RecordContainer + SessionID + uiclient.ScreenStreamSuffix) — a
//	   second composition is how a reader stops finding files a writer still writes.
//
// A path that carries the suffix but is not on disk is REFUSED as a path rather than
// re-read as a session id: the operator named a file, and "no stream at <path>" is
// the true answer, where "no session <path>" would be a different (and false) one.
func chatResolveSgrReplayPath(target string) (string, error) {
	if target == "" {
		return "", chatSgrReplayNeedsTargetErr()
	}
	if fi, err := os.Stat(target); err == nil && fi.Mode().IsRegular() {
		return target, nil
	}
	if suf, ok := uiclient.ScreenStreamSuffixOf(target); ok {
		return "", fmt.Errorf(chatSgrReplayMissingFileFormat, strings.TrimPrefix(suf, "."), target)
	}
	root, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve the sessions directory to look %q up: %w", target, err)
	}
	record, ferr := findSessionLogPath(root, target)
	if ferr != nil {
		return "", fmt.Errorf("cannot resolve session %q: %w", target, ferr)
	}
	if record == "" {
		return "", fmt.Errorf(chatSgrReplayNoStreamFormat, strings.TrimPrefix(uiclient.ScreenStreamSuffix, "."),
			target, filepath.Join(root, target+uiclient.ScreenStreamSuffix), chatSgrStreamFlag, chatSgrStreamEnv)
	}
	// The stream is looked for under EVERY suffix in ScreenStreamSuffixes, newest
	// first — a session recorded BEFORE the 2026-11-08 sgr→screen rename keeps its
	// bytes in `.sgr.stream`, and that file is the only verbatim recording the run
	// has. MEASURED: /home/user/.gopi/sessions holds 177 `.sgr.stream` files against
	// 4 `.screen.stream`, so a resolver that composed only the new name would report
	// "no screen.stream stream" for virtually every recording on the host. Composing
	// ONE name and stat-ing it is the defect; the list is the law (Rule 14).
	var want []string
	for _, suf := range uiclient.ScreenStreamSuffixes {
		cand := filepath.Join(sessionlayout.RecordContainer(record), sessionlayout.SessionID(record)+suf)
		want = append(want, cand)
		if fi, err := os.Stat(cand); err == nil && fi.Mode().IsRegular() {
			return cand, nil
		}
	}
	return "", fmt.Errorf(chatSgrReplayNoStreamFormat, strings.TrimPrefix(uiclient.ScreenStreamSuffix, "."),
		target, strings.Join(want, " or "), chatSgrStreamFlag, chatSgrStreamEnv)
}

// chatSgrReplayPlayer is ONE playback: the frames, the pace, and the control layer
// that may stop it. It owns nothing else — the frames are already in memory (a
// stream is read whole, because the reader is what validates its framing and a
// half-parsed stream is not a stream).
type chatSgrReplayPlayer struct {
	frames [][]byte
	delay  time.Duration
	ctl    *chatReplayControl
	out    io.Writer
	errw   io.Writer
}

// chatSgrReplayRead loads the recording at path. The READ is the reader's own
// parser, quoted rather than re-implemented: ReadSgrFrames knows the four findings a
// stream can produce (no header, an unknown version, an unreadable grid, a corrupt
// frame prefix) and names which one it made, so a second parser here would be a
// second opinion about one file format.
func chatSgrReplayRead(path string) (uiclient.SgrStreamMeta, [][]byte, error) {
	f, oerr := os.Open(path)
	if oerr != nil {
		return uiclient.SgrStreamMeta{}, nil, fmt.Errorf(chatSgrReplayUnreadableFormat,
			chatSgrReplayArtifactNoun(), path, oerr)
	}
	meta, frames, perr := uiclient.ReadSgrFrames(f)
	// THE CLOSE IS CHECKED, and reported through the SAME sentence as an unreadable
	// open: a stream that could not be closed may have been read through a failure
	// (a bad medium, a truncated NFS read), and the picture would then be built from
	// bytes nobody vouched for.
	if cerr := f.Close(); cerr != nil {
		return uiclient.SgrStreamMeta{}, nil, fmt.Errorf(chatSgrReplayUnreadableFormat,
			chatSgrReplayArtifactNoun(), path, cerr)
	}
	if perr != nil {
		return uiclient.SgrStreamMeta{}, nil, fmt.Errorf(chatSgrReplayBadStreamFormat, perr)
	}
	return meta, frames, nil
}

// chatSgrReplayArtifactNoun is the file kind's name as this verb's sentences use it
// (`sgr.stream`, without the leading dot): one definition, referenced by every
// sentence above, so a refusal and its test cannot drift (Rule 13).
func chatSgrReplayArtifactNoun() string {
	return strings.TrimPrefix(uiclient.ScreenStreamSuffix, ".")
}

// chatSgrReplayPlay writes the frames to out at the given pace and returns the
// status. It owns no terminal and opens nothing: the pace is a parameter (0 means
// "as fast as the writer accepts", which is what a capture wants) and the control
// layer is passed in, so the picture can be produced without a tty — which is how
// the behaviour below is tested at all (Rule 31: the surface is executed, not
// described).
func chatSgrReplayPlay(frames [][]byte, out io.Writer, errw io.Writer, delay time.Duration, ctl *chatReplayControl) int {
	p := &chatSgrReplayPlayer{frames: frames, ctl: ctl, out: out, errw: errw, delay: delay}
	return p.play()
}

// chatSgrReplaySayEnding is the ONE place this verb states how the recording ended,
// and it states ONE of three things (uiclient.SgrStreamEnd): the file reached its
// close-out record and there is nothing to say, it is whole but was never closed,
// or its own records contradict its own length.
//
// IT ASKS THE FILE, NOT THE PICTURE'S READER. `ReadSgrFrames`'s meta.Trailer says
// only whether a trailer LINE was seen, which is why the verb could not tell
// "unclosed" from "truncated" (bug 01a1b823-5070). The ending is a record-boundary
// walk (uiclient.ScanSgrStreamEndPath), and its three answers are the reason the
// three sentences above exist.
//
// A SCAN THAT CANNOT RUN IS ITS OWN SENTENCE. It is never folded into one of the
// three: collapsing "I could not look" into "nothing is wrong" is the failure this
// whole rule is about (Rule 15).
func chatSgrReplaySayEnding(path string, errw io.Writer) {
	end, size, err := uiclient.ScanSgrStreamEndPath(path)
	if err != nil {
		fmt.Fprintf(errw, chatSgrReplayEndUnreadableFormat, chatErrorPrefix, path, err)
		return
	}
	if end.Contradicts() {
		parts := make([]string, 0, len(end.Findings))
		for _, f := range end.Findings {
			parts = append(parts, f.String())
		}
		fmt.Fprintf(errw, chatSgrReplayContradictionFormat, chatErrorPrefix, size, strings.Join(parts, "; "))
		return
	}
	if end.Closed() {
		// NOTHING IS SAID, and that is the contract: a capture of a healthy
		// recording must see stderr exactly as it was.
		return
	}
	if end.BoundaryWhole() {
		fmt.Fprintf(errw, chatSgrReplayUnclosedWholeFormat, chatErrorPrefix)
		return
	}
	fmt.Fprintf(errw, chatSgrReplayUnclosedCutFormat, chatErrorPrefix, end.CutFrameAt)
}

// runChatSgrReplay is the verb: resolve, read, play, and say the two things this
// surface is allowed to say. It returns the process status.
func runChatSgrReplay(target string) int {
	return chatSgrReplayRun(target, os.Stdout, os.Stderr)
}

// chatSgrReplayRun is the verb with its streams as parameters, so a test can watch
// the picture and the notices without a terminal. The ONE thing it reads from the
// environment rather than a parameter is the tty-ness of stdin, because that IS the
// question ("is a human watching?") and it is answered by the process's own fd.
func chatSgrReplayRun(target string, out io.Writer, errw io.Writer) int {
	path, rerr := chatResolveSgrReplayPath(target)
	if rerr != nil {
		fmt.Fprintf(errw, "%s%v\n", chatErrorPrefix, rerr)
		return chatExitFailed
	}
	_, frames, rerr := chatSgrReplayRead(path)
	if rerr != nil {
		fmt.Fprintf(errw, "%s%v\n", chatErrorPrefix, rerr)
		return chatExitFailed
	}

	// WHO IS WATCHING decides the pace (see the header): a terminal means a human may
	// pause or leave, and pays the throttle; anything else means the picture is being
	// captured or hashed, and it plays at full speed.
	watched := lineedit.IsTerminal(os.Stdin)
	// THE CONTROL LAYER IS THE REPLAY'S OWN, and its OSD DESTINATION IS EMPTY on
	// purpose: this verb's product is the recorded bytes, so a state word written
	// between two frames would be a byte the comparison sees and the recording never
	// held. The state is still observable — a paused run's stdout stops growing —
	// which is the property the pause exists for (this file's header).
	ctl := newChatReplayControl(func(string, ...any) {})

	delay := time.Duration(0)
	var term *lineedit.Terminal
	var keysDone chan struct{}
	if watched {
		delay = chatSgrReplayFrameDelay
		t, terr := lineedit.Open(os.Stdin, os.Stdout)
		if terr != nil {
			// NO CONTROLS, AND SAY SO (Rule 15): a terminal that refuses raw mode
			// still gets the picture, but a watcher who presses a key and sees
			// nothing must be told why rather than left believing the binding broke.
			fmt.Fprintf(errw, "%s%s\n", chatErrorPrefix, terr)
		} else {
			term = t
			keysDone = make(chan struct{})
			go chatSgrReplayReadKeys(term, ctl, keysDone, errw)
		}
	}
	// NOTHING IS SAID ABOUT THE ABSENT CONTROLS, and that is the rule this surface
	// follows rather than an oversight: stdin not being a terminal means nobody can
	// press anything, so there is no watcher to be confused by a binding that did
	// not fire — and a line of ours on stderr is the one thing a captured comparison
	// would still have to filter out. The help page states the behaviour
	// (the --sgr-replay page: with stdin redirected there are no controls and no
	// throttle), which is where a reader looks for it.

	status := chatSgrReplayPlay(frames, out, errw, delay, ctl)
	ctl.markClosing()

	// THE TERMINAL IS THE OPERATOR'S AGAIN BEFORE ANYTHING ELSE IS PRINTED: a notice
	// written while raw mode is still installed lands on the picture's last row.
	if term != nil {
		if cerr := term.Close(); cerr != nil {
			fmt.Fprintf(errw, "%s%s\n", chatErrorPrefix, cerr)
			status = chatExitFailed
		}
		<-keysDone
	}

	// THE ONE LEGITIMATE POST-PICTURE LINE (the second exception). ONE line, chosen
	// by how the file ENDED — never by a single boolean that flattens three
	// different findings (see the sentences above).
	chatSgrReplaySayEnding(path, errw)
	return status
}

// play writes the frames in order, honouring the pause and the quit. It returns the
// process status: 0 for a completed picture (the watcher's departure included — that
// is the surface's own exit), non-zero when a write failed.
func (p *chatSgrReplayPlayer) play() int {
	for _, frame := range p.frames {
		if p.ctl.quitRequested() {
			return chatExitOK
		}
		// THE PAUSE IS CHECKED BEFORE THE WRITE, not inside it: a paused run writes
		// no byte at all, which is what makes "the stream stopped growing" a fact
		// rather than a rate.
		p.ctl.waitWhilePaused()
		if p.ctl.quitRequested() {
			return chatExitOK
		}
		if _, werr := p.out.Write(frame); werr != nil {
			fmt.Fprintf(p.errw, "%s"+chatSgrReplayWriteFormat+"\n", chatErrorPrefix,
				chatSgrReplayArtifactNoun(), werr)
			return chatExitFailed
		}
		if !p.gap() {
			return chatExitOK
		}
	}
	return chatExitOK
}

// gap waits one frame's pace, in slices, so a pause or a quit raised inside the gap
// is obeyed at the next slice instead of after the whole delay. It reports whether
// the run may continue: false means the watcher left.
func (p *chatSgrReplayPlayer) gap() bool {
	if p.delay <= 0 {
		return !p.ctl.quitRequested()
	}
	deadline := time.Now().Add(p.delay)
	for {
		if p.ctl.quitRequested() {
			return false
		}
		// A pause raised inside a gap parks here, so the picture stops at the frame
		// boundary rather than one frame later.
		p.ctl.waitWhilePaused()
		if p.ctl.quitRequested() {
			return false
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		step := chatSgrReplayTick
		if remaining < step {
			step = remaining
		}
		time.Sleep(step)
	}
}

// chatSgrReplayReadKeys feeds the watcher's keys to the control layer and closes done
// on the way out. It opens no second reader: the terminal this run opened IS the
// reader, and every key is decoded with lineedit's own decoder so a key means here
// exactly what it means on `--replay`.
func chatSgrReplayReadKeys(term *lineedit.Terminal, ctl *chatReplayControl, done chan struct{}, errw io.Writer) {
	defer close(done)
	dec := lineedit.NewDecoder()
	buf := make([]byte, chatSgrReplayReadBufBytes)
	for {
		// THE RUN IS OVER: stop reading. The control layer knows before this loop does
		// (its quit bit is set by the key that asked to leave), and reading on would
		// keep claiming keys for a run that is closing.
		if ctl.leftByWatcher() {
			return
		}
		ev, err := term.ReadEvent(buf, chatSgrReplayBlockMS)
		if err != nil {
			fmt.Fprintf(errw, "%s%s\n", chatErrorPrefix, err)
			return
		}
		if ev.Kind != lineedit.EventInput {
			// A resize or a wake-up is not a key: this surface has no layout to
			// recompute (the frames carry their own coordinates), so there is nothing
			// to do with one, and it is not an error that one arrived.
			continue
		}
		if ev.EOF || ev.Bytes <= 0 {
			return
		}
		for _, k := range dec.Feed(buf[:ev.Bytes]) {
			// THE ORIGIN IS THE TERMINAL'S: these bytes were typed, so the control
			// layer treats them as the watcher's (its GateKey refuses anything else,
			// and on this surface nothing else can exist).
			k.Origin = lineedit.ReadOriginTyped
			ctl.GateKey(k)
		}
		// A LONE ESC IS A KEY, AND ITS SEQUENCE HAS A WINDOW: the decoder holds it
		// because ESC also heads every arrow, so it is resolved as Escape only once
		// the window passes with nothing following — the same bound the editor
		// applies, for the same reason.
		if dec.BareEscape() {
			time.Sleep(chatSgrReplayEscapeWindow)
			if k := dec.FlushEscape(); k.Code != lineedit.KeyNone {
				k.Origin = lineedit.ReadOriginTyped
				ctl.GateKey(k)
			}
		}
	}
}
