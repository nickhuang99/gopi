package main

// chat_film.go — `gopi chat --film <outdir>`: the FILM DRIVER, in gopi.
//
// WHAT THIS REPLACES. Filming a recorded session used to need a shell pipeline:
// scripts/replay/replay.sh started an Xvfb, started a terminal on it, launched a
// producer, read <scratch>/beats.tsv, grabbed the window once per beat with
// tools/replay/grab.py (python3), answered <scratch>/ack, and ended on
// <scratch>/done.txt. Every one of those steps is a driver's job, and a driver is
// something gopi can be: the display allocation is internal/shot's
// (displayalloc.go), the window-by-pid identity and the settled grab are
// internal/shot's (terminalterm.go), the pixels are internal/shot's own X11
// GetImage, and the beat protocol's files are internal/replay's. So the paper
// cuts along the seam that already exists:
//
//	gopi chat --replay <session> --film <outdir>
//
// ONE command, no shebang, no python3, no xdotool, no xwininfo, no ImageMagick.
//
// WHO IS THE DRIVER AND WHO IS FILMED. This process is the DRIVER: it does not
// re-run the recording itself, and it needs no terminal of its own. It owns a
// private X display, starts ONE terminal on it, and inside that terminal runs
// THIS binary again as the PRODUCER:
//
//	<gopi> chat --replay <session> --scratch <scratch> --cols C --rows R
//
// (CliVerbChat is taken from cli_verbs.go, not typed: the first end-to-end run of
// this file omitted the verb, so the child printed the top-level usage and exited
// 0 — and every symptom downstream read as "the terminal never mapped a window".)
//
// The producer draws the REAL chat surface beat by beat (chat_replay_beats.go),
// appends each beat to <scratch>/beats.tsv and BLOCKS until this driver writes
// that beat number into <scratch>/ack. So frame N is the screen after beat N BY
// CONSTRUCTION rather than by timestamp inference, and the ledger's rows and the
// frames cannot drift apart into two accounts of the same run.
//
// THE FRAME IS TAKEN ONCE THE PICTURE HAS SETTLED (two consecutive grabs must
// agree) — see internal/shot's SettledGrab for the measurement that makes this
// the judgement rather than an optimisation. A beat that never settles is
// delivered and REPORTED (settled=no in frames.tsv), never hidden.
//
// WHERE THINGS LAND. The frames, beats.tsv, the settled-grab evidence and the
// run's closing report all land under <outdir>; the producer's scratch (the
// ledger, the acknowledgements, the per-beat dumps) lands under a scratch
// directory resolved through internal/scratch — never in the worktree (Rule 30)
// and never on `/` (Rule 22). The caller never has to name that scratch: it is
// allocated here, so `--film <outdir>` is the whole command line.
//
// DRY-RUN SEMANTICS (`--replay`), stated in the usage text below as well: a
// replay masks exactly two things — the model and the tool dispatch. Every
// recorded model answer comes from the in-process stub and every recorded tool
// call is answered from the recording through the agent's executor seam, so
// NOTHING is computed and NO tool is dispatched; the operator's turns are typed
// into the real editor. Everything else is `gopi chat` exactly as a human runs
// it: the same banner, the same line editor, the same answer band, the same
// status row, the same closeout. Filming therefore changes NOTHING about the
// surface — it only decides where the model's words come from and who types.
//
// ZERO NEW DEPENDENCIES (Rule 35): Xvfb and the terminal emulator are the
// existing bare-system dependencies, both already required by this pipeline; this
// file installs nothing, starts no daemon of its own, and leaves nothing behind —
// the display server and the terminal are this run's own children and are stopped
// when it ends. WHICH emulator that is comes from the terminal registry
// (internal/shot/terminalregistry.go), whose built-in default is xfce4-terminal —
// the terminal that renders a CJK surface — while xterm remains a selectable name
// for its historical measurements.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/replay"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/shot"
)

// The film driver's vocabulary and its bounds. Every literal is a named constant
// (Rule 1/1b), and every name this file shares with another package (the ledger,
// the acknowledgement, the completion marker, the dump directory) is TAKEN FROM
// that package rather than re-typed (Rule 13/14).
const (
	// chatFlagFilm is the switch. It TAKES A VALUE: the directory the film lands
	// in, which must already exist or be creatable (a film is a durable artifact,
	// so the caller names where it goes rather than getting it somewhere
	// implicit).
	chatFlagFilm = "--film"
	// chatFilmFrameFormat names one frame under the output directory. The beat
	// number IS the frame number, which is what makes the film and the ledger
	// checkable against each other row by row.
	chatFilmFrameFormat = "frame-%04d.png"
	// chatFilmEvidenceName is the settled-grab evidence: one row per frame saying
	// how many grabs it took and whether the screen ever agreed with itself.
	chatFilmEvidenceName = "frames.tsv"
	// chatFilmEvidenceHeader is that file's one header row. The sixth column is
	// the count of "the window I was photographing was not there and I
	// re-resolved it" events for that frame: without it the recovery exists only
	// on a terminal that has since scrolled away, and a run that re-resolved its
	// window twenty times would look exactly like one that never had to.
	chatFilmEvidenceHeader = "#frame\tgrabs\tstale\tsettled\tbytes\tvanished\n"
	// chatFilmFrameTimeout bounds the wait for ONE beat to appear in the ledger. A
	// producer that stopped without writing done.txt must not hang the driver.
	chatFilmFrameTimeout = 120 * time.Second
	// chatFilmPollMS is the cadence at which the ledger is re-read. It is the
	// granularity at which a condition is observed, never a fixed wait.
	chatFilmPollMS = 10
	// chatFilmScratchLabel / chatFilmOutLabel are the two directories named in a
	// refusal, in the vocabulary the CALLER used.
	chatFilmScratchLabel = "--scratch"
	chatFilmOutLabel     = chatFlagFilm
	// chatTerminalFlag is the switch that CHOOSES the filmed terminal. It takes a
	// NAME from internal/shot's terminal registry (an unknown one is refused with
	// the reason and the usable list — never silently replaced by the default),
	// and it is the top channel of the knob the registry resolves:
	//
	//	--film-terminal NAME > GOPI_FILM_TERMINAL > settings "film_terminal" > xterm
	//
	// The spelling is composed from internal/config's constant, so the flag, the
	// env name and the settings key have ONE definition each (Rules 1b/13/14).
	chatTerminalFlag = config.FilmTerminalFlag
	// chatFilmWindowTitlePrefix names the terminal window this run started, so a
	// human looking at the private display can tell whose window it is.
	chatFilmWindowTitlePrefix = "gopi film "
	// chatFilmTerminalAttempts is how many times the terminal is started before
	// the run gives up. It exists because a terminal can exit before it maps a
	// window — a race between the X server's own readiness and the client's
	// first connection, measured 2026-10-03: xterm died with status 0 having
	// drawn nothing, and the private display was left without a client. The
	// bound is small and every attempt's failure is printed, so a terminal that
	// cannot start AT ALL is still reported as such rather than retried away.
	chatFilmTerminalAttempts = 3
	// chatFilmTerminalLogName is the file the terminal's own stderr is appended
	// to, beside the frames. Empty-handed failures are the ones that cost hours.
	chatFilmTerminalLogName = "terminal.log"
	// chatFilmNoticeLogName is the file internal/shot's notices about the filmed
	// terminal are ALSO written to — the window re-resolution lines above all.
	// A notice whose only trace is a terminal that has since scrolled away cannot
	// be checked, and the event it reports (the window rebuilt mid-frame) is
	// exactly what a reader of the film needs to see. It is a THIRD file rather
	// than a share of the two above because those carry the terminal's words and
	// the producer's words; this one carries the mechanism's own.
	chatFilmNoticeLogName = "terminal-notices.log"

	// chatFilmProducerLogName is the file the FILMED command's own output is
	// appended to. It is separate from the terminal's log because the two answer
	// different questions: this one is what the producer said, the other is what
	// the terminal said while trying to run it.
	chatFilmProducerLogName = "producer.log"
	// chatFilmTerminalExitWait bounds the wait for the producer's terminal to close
	// AFTER done.txt: the run is over, and this is how long its last paint is given
	// to reach the display before the terminal is stopped.
	chatFilmTerminalExitWait = 3 * time.Second
	// chatFilmNotice prefixes every sentence this driver prints, so its own
	// diagnostics are distinguishable from the producer's.
	chatFilmNotice = "[gopi film] "
	// chatReplayDirsReport is the driver's closing report: the film's own
	// arithmetic, so a reader can check the frames against the ledger.
	chatReplayDirsReport = chatFilmNotice + "film written to %s: %d frame(s) from %d ledger row(s), %d frame(s) reached the settled criterion, %d did not"
	// chatFilmTimeoutReport is the honest end of a producer that stopped without
	// finishing.
	chatFilmTimeoutReport = chatFilmNotice + "the producer wrote no new beat for %s — the film ends here with the beats that were delivered (last beat %d); the producer's own error, if it printed one, is above"
	// chatFilmProducerGoneReport is the honest end of a producer that died before
	// finishing: the status is the terminal's own, and the two logs are named
	// because between them they hold the reason.
	// chatFilmEvidenceHint is the closing half of every report that points at the
	// producer's own output, and it is a SWITCH, not a sentence: the producer's
	// output is captured only when the filmed terminal has a session-log channel
	// (xterm's -l/-lf), so on a terminal without one the file is absent and naming
	// it as the place the reason IS would send the operator to a path that does not
	// exist — the exact silent misdirection Rule 15 forbids. Both halves are
	// composed by the ONE function below and used by every site, so a report cannot
	// promise a file this run did not write.
	chatFilmProducerLogHint   = "Its output is in %s and its terminal's own words are in %s"
	chatFilmProducerLogAbsent = "This terminal captures no producer output (%s is not written for it — see the notice above), so its terminal's own words in %s are the whole record"

	chatFilmProducerGoneReport = chatFilmNotice + "the filmed terminal is gone (%s) and no completion marker was written — the film ends here with %d beat(s). %s"
	// chatFilmVanishedReport is the closing line for a run that had to re-resolve
	// its terminal's window. It exists so the recovery is a NUMBER on the closing
	// report and not only a line somebody had to be watching for: a terminal that
	// rebuilt its toplevel twenty times is not the same run as one that never had
	// to, and without this both end with the same summary.
	chatFilmVanishedReport = chatFilmNotice + "the filmed window was gone mid-frame %d time(s) across %d frame(s) — it was re-resolved by the terminal's own window rule and the frame continued; the per-frame count is the vanished column of %s (a frame refused after the bound is an ERROR, never a delivered frame)"
	// chatFilmNoReplay is the refusal for a film with nothing to film: a film of a
	// live conversation would film a model that answers differently every time.
	chatFilmNoReplay = chatFlagFilm + " films a REPLAYED session and no " + chatFlagReplay + " was given — write " + chatFlagReplay + " SESSION " + chatFlagFilm + " DIR"
	// chatFilmScratchConflict is the refusal for a scratch that cannot hold this
	// run's protocol files.
	chatFilmScratchConflict = chatFlagFilm + " could not prepare a scratch directory for the beat protocol: %w"
)

// chatFilmUsage is the usage page's block for the film and its animation. It is a
// `var`, not a `const`, because it states the pace the animation is written at and
// that number belongs to internal/gifgen (chatFilmGIFDelayCS): composing it in is
// what keeps the help from RESTATING the encoder's default as a literal (Rules
// 1/13 — a value spelled twice is two definitions, and the help is a place a
// reader believes). Go refuses a function call in a constant expression, so the
// page is a var; nothing about it changes, since every reader is a string
// operation.
var chatFilmUsage = "  " + chatFlagFilm + " DIR       film a replayed session: run the recorded session inside a\n" +
	"             private terminal on a private X display (started by this run) and\n" +
	"             photograph it once per drawn beat, writing " + chatFilmFrameFormat + "\n" +
	"             and " + replay.LedgerName + " into DIR. Needs " + chatFlagReplay + ". Replay semantics are\n" +
	"             DRY-RUN: only the model and the tool dispatch are masked (the answers\n" +
	"             come from the recording and no tool runs) — everything else is\n" +
	"             `gopi chat` exactly as a human runs it (man gopi-chat).\n" +
	"  " + chatFlagOut + " FILE.gif\n" +
	"             animate the film: encode the frames into ONE looping GIF (the same\n" +
	"             encoder `gopi tool gif` uses, no external tool), each frame captioned\n" +
	"             with its beat number and the ledger's words for it. Requires " + chatFlagFilm + ".\n" +
	"             One frame per drawn beat, " + strconv.Itoa(chatFilmGIFDelayCS) + "cs apart (uniform — a beat is a\n" +
	"             state change, not a video frame), paced by the encoder's own default.\n" +
	"  " + chatFlagWechat + "\n" +
	"             request the PHONE-PLAYABLE animation: the same GIF, held to a phone\n" +
	"             conversation view's own byte ceiling — at most " + strconv.Itoa(wechatGIFMaxBytes) + " byte(s). Requires\n" +
	"             " + chatFlagOut + "; over the ceiling the run REFUSES with the numbers rather than\n" +
	"             handing over a file the phone would reject (man gopi-chat).\n" +
	"  " + chatTerminalFlag + " NAME\n" + shot.FilmTerminalHelpRow()

// chatParseFilmArg consumes the `--film` switch and its value, in both spellings
// (`--film DIR` and `--film=DIR`). It is the value-carrying twin of
// chatParseReplayArg and exists as a function for the same reason: the argv walk
// must consume the value in the next slot, which a ranged loop cannot express.
//
// A BARE `--film` IS ACCEPTED HERE, and "where do the frames go" is settled at the
// DISPATCH point instead (resolveFilmDir) — because the answer depends on a word
// that may not have been parsed yet. The operator's own command line is
//
//	gopi chat --replay <session> --film --out /tmp/replay.gif
//
// and a parser that refused the bare word would refuse the command this feature
// exists to serve. Bare is NOT the same as "no directory anywhere": with no
// `--out` either, the run genuinely has nowhere to put its artifacts, and that is
// refused — naming both spellings — rather than guessed at.
func chatParseFilmArg(a, next string, hasNext bool) (dir string, consumed bool, err error) {
	if a != chatFlagFilm && !strings.HasPrefix(a, chatFlagFilm+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s needs a directory — write %s DIR", chatFlagFilm, chatFlagFilm)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		// The bare spelling: no value in the next slot, and nothing in this argv
		// that could be one (a word starting with `-` is a flag, never a
		// directory). A non-empty dir means the caller named one; an empty dir with
		// consumed set means BARE, which resolveFilmDir settles.
		return "", true, nil
	}
	return next, true, nil
}

// chatParseTerminalArg parses the filmed-terminal switch in both spellings
// (`--film-terminal NAME` and `--film-terminal=NAME`). It is the value-carrying
// twin of the other parsers on this surface, and it exists for the same reason:
// the argv walk must consume the value in the next slot, which a ranged loop
// cannot express.
//
// An EMPTY value is a usage error naming the flag (Rule 15): "--film-terminal="
// asks for a terminal nobody named, and reading it as "unset" would silently film
// with the default — the operator would get a picture of a terminal they did not
// choose, with nothing on screen saying so. Whether the NAME is a real terminal is
// NOT decided here: that is the registry's judgment (shot.ResolveFilmTerminal),
// which is also where the unknown-name refusal and the usable list live, so the
// parser cannot hold a second opinion of what a terminal is called.
func chatParseTerminalArg(a, next string, hasNext bool) (name string, consumed bool, err error) {
	if a != chatTerminalFlag && !strings.HasPrefix(a, chatTerminalFlag+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s was given an empty value — write %s NAME (usable names: %s)",
				chatTerminalFlag, chatTerminalFlag, shot.FilmTerminalUsableList())
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a terminal name — write %s NAME (usable names: %s)",
			chatTerminalFlag, chatTerminalFlag, shot.FilmTerminalUsableList())
	}
	return next, true, nil
}

// resolveFilmDir settles where a film's artifacts go, once the WHOLE command line
// is known. A directory the caller gave wins outright. A BARE `--film` takes the
// ANIMATION's own directory: the caller named that path as the destination for
// this run's product, so the frames, the ledger and the evidence land beside the
// picture they produced, and this run invents no path of its own (a location is
// configuration, never a guess — and here the caller configured it by naming the
// GIF).
//
// No directory at all — bare `--film` and no `--out` — is refused by name: the
// film would have nowhere to write its frames, and choosing somewhere silently is
// how a directory full of PNGs turns up where nobody looked. label is the word the
// CALLER typed, so the refusal reads in their own vocabulary.
func resolveFilmDir(filmDir, gifOut, label string) (string, error) {
	if filmDir != "" {
		return filmDir, nil
	}
	if gifOut == "" {
		return "", fmt.Errorf("%s was given with no directory and there is no %s to derive one from — write %s DIR, or %s %s FILE.gif (the frames then land beside the animation)",
			label, chatFlagOut, label, label, chatFlagOut)
	}
	return filepath.Dir(gifOut), nil
}

// chatFilm is the driver's own state: the two directories it writes into, the
// display and terminal it owns, and the arithmetic of its closing report.
type chatFilm struct {
	outDir    string
	scratch   string
	display   *shot.FilmDisplay
	terminal  *shot.FilmTerminal
	evidence  *os.File
	lastBeat  int
	frames    int
	settled   int
	unsettled int
	rows      int
	// vanished counts the frames this run had to recover by re-resolving the
	// terminal's window (see chatFilmEvidenceHeader). It is reported at the end
	// for the same reason the unsettled count is: a recovery that only ever
	// happened on a scrolled-away terminal is not a fact the run can be held to.
	vanished int
	// gifPath is the animation this run was asked to write ("" = none). The frame
	// PATHS are accumulated in BEAT ORDER as they are photographed, so the
	// animation is composed from THIS run's own arithmetic (what it actually
	// shot) rather than from a later re-scan of the output directory — a re-scan
	// would fold a pre-existing frame-NNNN.png of another run into this film's
	// animation. Nothing else is accumulated: the animation carries the surface's
	// pixels and no caption (chat_film_gif.go's header).
	gifPath   string
	gifFrames []string
	// wechat records that this run was asked for the PHONE-SHAPED product
	// (chatFlagWechat): the animation is then held to the even-height and
	// byte-ceiling properties on top of the delivery contract every `--out` film
	// already meets. It is a pure product attribute — see chat_film_gif.go for why
	// it is deliberately NOT a four-channel knob.
	wechat bool
	// producer is the guard that held the producer until the terminal had
	// published its pixel face (chat_film_producer.go). It is released by the
	// delivery-canvas decision and removed by Close; nil means no guard was armed
	// (reported where that happened, never silent).
	producer *filmProducerGuard
	// evenCropped counts the frames whose photographed size was ODD in a
	// dimension and were therefore written one pixel narrower (chat_film_frames.go).
	// It is reported for the same reason every other count here is: a run that had
	// to correct its own geometry should be readable as such, and on the reference
	// face (xterm) it is zero.
	evenCropped int
	// termSpec is the terminal this film is running inside (the registry row the
	// name resolved to). It is kept so the reports BELOW can say whether the
	// producer's log exists at all: the file is written by the terminal's own
	// session-log channel, and a terminal without one never creates it.
	termSpec shot.FilmTerminalSpec
}

// evidenceHint is the one place a report points at the producer's output, and it
// answers for the terminal this run actually filmed with (see the two halves'
// constants). A run that never resolved a terminal cannot reach the reporting
// paths — they all run after startTerminal — so an unset spec is not a case this
// has to invent a sentence for, and it is reported as that rather than guessed at.
func (f *chatFilm) evidenceHint() string {
	producer := filepath.Join(f.outDir, chatFilmProducerLogName)
	terminal := filepath.Join(f.outDir, chatFilmTerminalLogName)
	if !f.termSpec.SupportsSessionLog() {
		return fmt.Sprintf(chatFilmProducerLogAbsent, producer, terminal)
	}
	return fmt.Sprintf(chatFilmProducerLogHint, producer, terminal)
}

// chatFilmScratchDir allocates the producer's scratch directory through
// internal/scratch (Rule 22: it is provably off `/`, swept when this process
// dies, and never spelled by a caller). It reuses an EXISTING class constant
// rather than inventing one: the film driver IS the shot subsystem's extension,
// and a new class string would be a new sweep entry for no new lifetime.
func chatFilmScratchDir() (string, error) {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassShot, "chatfilm-")
	if err != nil {
		return "", fmt.Errorf("allocate the film scratch: %w", err)
	}
	return dir, nil
}

// runChatFilm is the WHOLE driver: preflight, private display, private terminal,
// the beat loop, and the closing report. It is called from runChatWithInput
// BEFORE any surface is built — the driver is not a chat run and needs no editor,
// which is the point of moving it out of the shell and into this verb's own
// process.
func runChatFilm(filmDir, replayTarget, terminalName string, beat chatBeatFlag) int {
	return runChatFilmOut(filmDir, replayTarget, "", terminalName, beat, false)
}

// runChatFilmOut is the driver with the animation arm: gifPath is where the
// playable GIF goes ("" = this run films frames only, which is what `--film`
// alone has always meant).
func runChatFilmOut(filmDir, replayTarget, gifPath, terminalName string, beat chatBeatFlag, wechat bool) int {
	// QUESTION 0, asked before anything is started: WHICH TERMINAL the film runs
	// inside. The knob's ONE resolver answers it (Rule 2/34 — flag > env >
	// settings > the registry's built-in default), and a name it cannot resolve
	// ENDS the run here, with the reason and the usable list (Rule 15): starting a
	// display server and a terminal before learning that the terminal is unknown
	// would spend the operator's time to reach a refusal they could have read
	// first.
	term, termSource, terr := shot.ResolveFilmTerminal(terminalName)
	if terr != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", terr)
		return 1
	}
	// QUESTION 1 of the three, and the two directories are asked in the order the
	// caller reads them: the OUTPUT first (a film that cannot be written must not
	// start a display server), then the protocol's scratch.
	if err := shot.CheckFilmScratch(filmDir, chatFilmOutLabel); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	scratchDir := beat.scratch
	if scratchDir == "" {
		dir, err := chatFilmScratchDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
			return 1
		}
		scratchDir = dir
	}
	if err := shot.CheckFilmScratch(scratchDir, chatFilmScratchLabel); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	// QUESTION 2: the display server, present AND runnable. Asked before anything
	// is started, so a machine without it hears that instead of twelve failed
	// attempts.
	if err := shot.CheckFilmServer(""); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	// The protocol needs the geometry the LAYOUT will be drawn at, because the
	// producer refuses a terminal that disagrees with it (a film drawn at another
	// width is a different picture, and the driver's legibility floor belongs to
	// the width it asked for). The defaults are the chat surface's OWN ideal
	// geometry (config.ChatIdealGeometry), not a second literal pair: the film
	// forces the producer to the size the surface is designed for, so the driver,
	// the beat protocol and the live chat cannot hold three ideas of the canvas.
	cols, rows, gerr := chatFilmGeometry(beat, replayTarget, os.Stderr)
	if gerr != nil {
		// THE REFUSAL IS MADE BEFORE ANYTHING IS STARTED, which is the reason the
		// check lives in the resolution and not in the producer: a stated count
		// above what the session was recorded at cannot be drawn at all, and the
		// run ends HERE — no display server, no terminal, no producer — with the
		// two numbers named. Returning non-zero is what makes the failure
		// distinguishable from the silent 0-frame exit this replaces (Rule 15).
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", gerr)
		return 1
	}
	// ... AND, WHEN AN ANIMATION IS ASKED FOR, its DELIVERY half. It is applied
	// BELOW, after the terminal has mapped its window and published the face the
	// pixels will really come out on (filmProbeTerminalFace): the cap is a CELL
	// COUNT whose frame fits the limit on THIS terminal, and a count derived from
	// another terminal's face is the defect this arm closes (measured 2026-10-04:
	// 82 columns is 496 px on xterm's face and 836 px on xfce4-terminal's).
	//
	// ORDER: the display server is started first because the terminal needs one,
	// and the terminal is started at the RESOLVED canvas because that is the size
	// the surface is designed for. The delivery cap is then applied to the CELL
	// COUNT once the face is known — and the terminal, which has already mapped,
	// is RESIZED to the canvas the film delivers (shot.FilmTerminal.Resize, the
	// same X11 ConfigureWindow the terminal tool has): the surface in it is a
	// child that owns its pty and re-reads its size, so the picture and the
	// delivered frames are the same canvas without anyone having to state it.
	//
	// QUESTION 3: a number derived from what THIS machine holds, then a server
	// this run owns.
	display, err := shot.StartFilmDisplay(scratchDir, shot.X11SocketDir(), 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	defer func() {
		if cerr := display.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", cerr)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"the running gopi binary could not be named: %v\n", err)
		return 1
	}
	geometry := strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
	fmt.Fprintf(os.Stderr, chatFilmNotice+"display %s (server pid %d), terminal %s (%s) at %dx%d cells on it\n",
		display.Name(), display.PID, term.Name, termSource, cols, rows)
	producerLog := filepath.Join(filmDir, chatFilmProducerLogName)
	// The ANIMATION's destination is checked HERE, beside the film directory and
	// for the same reason: a GIF that cannot be written must be refused before a
	// display server is started, not after a whole run has been photographed. The
	// check WRITES (a tree that cannot hold a file cannot hold a GIF) and removes
	// its probe again — shot.CheckFilmScratch, the same predicate the two
	// directories are held to.
	if gifPath != "" {
		if err := shot.CheckFilmScratch(filepath.Dir(gifPath), chatFilmGifLabel); err != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
			return 1
		}
	}
	title := chatFilmWindowTitlePrefix + filepath.Base(filmDir)
	terminalLog := filepath.Join(filmDir, chatFilmTerminalLogName)
	f := &chatFilm{outDir: filmDir, scratch: scratchDir, display: display, gifPath: gifPath, wechat: wechat, termSpec: term}
	// explicit: the canvas was STATED by the operator rather than derived by this
	// driver, which is what decides whether the delivery cap applies to it (see
	// filmDeliveryCanvas).
	explicit := beat.colsSet || beat.rowsSet
	producer := chatFilmProducerArgv(exe, replayTarget, scratchDir, cols, rows)
	if term.SupportsSessionLog() {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"producer: %s (its output is appended to %s)\n", filmProducerInvocation(producer), producerLog)
	}
	// THE GUARD, armed before the terminal starts — see chat_film_producer.go for
	// the question it exists to ask and why it can only be asked here. It is a
	// file in this run's scratch tree, it is removed by this run's Close, and a
	// guard that cannot be allocated is REPORTED and the producer started
	// directly (the resolved canvas is the ordinary answer anyway).
	guard, gerr := newFilmProducerGuard()
	if gerr != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v — the producer starts immediately at the resolved canvas and no narrowing is possible on this run\n", gerr)
	}
	if guard != nil {
		f.producer = guard
		defer guard.disarm()
		guard.arm(producer, os.Stderr)
	}
	if err := f.startTerminal(term, guardArgv(guard, producer), geometry, title, terminalLog, producerLog); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	terminal := f.terminal
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", cerr)
		}
	}()
	// THE FACE, READ FROM THE WINDOW THAT JUST MAPPED — the run's own pixel model,
	// and the input to the delivery cap. It is read HERE: the producer has not been
	// released yet (the guard is still holding it), so nothing has been drawn at
	// the wrong width, and the answer still arrives in time to decide the canvas
	// the producer is given.
	// shotCols/shotRows are what this run DECIDES the film is shot at; they equal
	// cols/rows unless the delivery cap moved them (or the canvas was stated by
	// the operator, in which case they are whatever was stated).
	shotCols, shotRows := cols, rows
	narrowed := false
	if gifPath != "" {
		filmProbeTerminalFace(terminal, os.Stderr)
		var derr error
		shotCols, shotRows, derr = filmDeliveryCanvas(cols, rows, explicit, os.Stderr)
		if derr != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", derr)
			return 1
		}
		narrowed = shotCols != cols || shotRows != rows
		if narrowed {
			// THE WINDOW IS MOVED TO THE CANVAS THE FILM DELIVERS, before the
			// producer is released: the surface inside the terminal owns its pty and
			// re-reads its size on SIGWINCH, so ONE move leaves the picture and the
			// delivered frames the same canvas without anyone stating a size to
			// anybody. A window that does not take the size is refused here rather
			// than photographed at a size nobody asked for.
			rect, rerr := terminal.Resize(filmDeliveryFace, shotCols, shotRows)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", rerr)
				return 1
			}
			terminal.Geometry = rect
		}
		cols, rows = shotCols, shotRows
		geometry = strconv.Itoa(cols) + "x" + strconv.Itoa(rows)
	}
	// THE PRODUCER IS RELEASED LAST, and it is told the canvas THIS RUN DECIDED:
	// the narrowed one when the cap moved it (the surface refuses a terminal that
	// disagrees with the canvas it was asked for, so the two numbers must be the
	// same one), and the resolved one otherwise. A guard that was never armed is
	// nil, and both releases are idempotent, so every path above reaches this one.
	if guard != nil {
		// ONE release, carrying the canvas: releaseAt IS release when the canvas
		// did not move (zero means "as resolved"), and the first release is the
		// one that lands — the guard closes itself as it writes, so a second call
		// is a no-op by design.
		relErr := guard.releaseAt(shotCols, shotRows)
		if relErr != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", relErr)
			return 1
		}
	}
	if _, terr := fmt.Fprintf(os.Stderr, chatFilmNotice+"terminal window %d (%q) area %s, expected %s for %dx%d cells — the ONE window-selection rule (%s) picked it and a frame that finds it gone re-applies that same rule; up to %d re-resolution(s) are announced on this stream\n",
		terminal.Window, terminal.Name, terminal.Geometry, filmFrameGeometryOn(filmDeliveryFace, cols, rows), cols, rows, terminal.TermSpec().WindowRule, shot.WindowVanishedSteps()); terr != nil {
		// A driver whose own report cannot be written has lost its evidence
		// channel; the run continues (the film is the deliverable) but the fact is
		// not swallowed (Rule 15b).
		fmt.Fprintf(os.Stderr, chatFilmNotice+"the terminal identity could not be reported: %v\n", terr)
	}
	// The LOCALE the filmed child was pinned to, reported for the same reason the
	// window identity is: it decides the pixels of every Chinese frame, so a
	// reader who cannot see it cannot tell a correct film from a mojibake one.
	// Empty (no pin) is reported as such rather than omitted — the two states are
	// different facts.
	if locale, lerr := shot.ResolveFilmChildLocale(); lerr != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"the filmed child's locale could not be reported: %v\n", lerr)
	} else if _, nerr := fmt.Fprintf(os.Stderr, chatFilmNotice+"%s\n", shot.FilmChildLocaleNotice(locale)); nerr != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"the filmed child's locale notice could not be written: %v\n", nerr)
	}
	// THE MECHANISM'S OWN VOICE, captured for the length of the run. The window
	// re-resolution notices are printed by internal/shot — that is where the
	// selection rule lives — and this run keeps its own copy of them, so the film
	// carries the record of what it had to do to keep shooting (a frame recovered
	// from a rebuilt window is a fact about the film).
	restoreNotices, nerr := f.openNoticeSink()
	if nerr != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", nerr)
		return 1
	}
	defer restoreNotices()
	if err := f.drive(); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
		return 1
	}
	return 0
}

// openNoticeSink sends internal/shot's notices about the filmed terminal to this
// run's OWN notice file as well as to the stream the operator is watching, and
// returns the restore (which also closes the file).
//
// WHY A FILE AND NOT ONLY stderr. The window re-resolution is the mechanism this
// run exists to make visible, and a mechanism whose only trace is a terminal that
// has since scrolled away cannot be checked by anyone reading the film afterwards
// (Rule 15: an event recorded nowhere is an event that can be denied).
func (f *chatFilm) openNoticeSink() (func(), error) {
	path := filepath.Join(f.outDir, chatFilmNoticeLogName)
	w, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open the terminal-notice log %s: %w", path, err)
	}
	restore := shot.FilmTerminalNoticeSink(&filmNoticeWriter{path: path, mirror: w})
	return func() {
		restore()
		if cerr := w.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, chatFilmNotice+"close %s: %v\n", path, cerr)
		}
	}, nil
}

// filmNoticeWriter writes every notice to the operator's stream and COPIES it to
// the run's own file.
//
// The copy is best-effort in the one direction that matters: a notice the file
// could not hold is REPORTED on the operator's stream (once per failure) and the
// write still reports the full length — the record must not be able to break the
// mechanism it is recording, and a silent gap in the record is the defect this
// writer exists to prevent (Rule 15b).
type filmNoticeWriter struct {
	path   string
	mirror io.Writer
}

func (w *filmNoticeWriter) Write(p []byte) (int, error) {
	if _, err := w.mirror.Write(p); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"the terminal-notice log %s could not be written: %v\n", w.path, err)
	}
	if _, err := os.Stderr.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// startTerminal starts the filmed terminal, retrying a bounded number of times.
//
// The retry exists for ONE measured failure (2026-10-03): a terminal can exit
// before it maps a window — a race between the private display's readiness and the
// client's first connection — and a run that gives up there films nothing while
// the display it paid for is perfectly good. Every attempt's failure is PRINTED,
// so a terminal that cannot start at all is still reported as such rather than
// retried away; and each attempt's own stderr is appended to the same log.
func (f *chatFilm) startTerminal(spec shot.FilmTerminalSpec, producer []string, geometry, title, terminalLog, sessionLog string) error {
	var lastErr error
	for attempt := 1; attempt <= chatFilmTerminalAttempts; attempt++ {
		term, err := shot.StartFilmTerminalSpec(spec, f.display.Name(), geometry, producer, title, terminalLog, sessionLog)
		if err == nil {
			f.terminal = term
			return nil
		}
		lastErr = err
		fmt.Fprintf(os.Stderr, chatFilmNotice+"terminal attempt %d of %d failed: %v\n", attempt, chatFilmTerminalAttempts, err)
	}
	return lastErr
}

// drive runs the beat loop: watch the ledger, grab one settled frame per new
// beat, acknowledge it, and stop on the completion marker.
func (f *chatFilm) drive() error {
	ledgerPath := filepath.Join(f.scratch, replay.LedgerName)
	evidencePath := filepath.Join(f.outDir, chatFilmEvidenceName)
	ev, err := os.OpenFile(evidencePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open the settled-grab evidence %s: %w", evidencePath, err)
	}
	f.evidence = ev
	if _, err := ev.WriteString(chatFilmEvidenceHeader); err != nil {
		return fmt.Errorf("write the settled-grab evidence header: %w", err)
	}
	// THE FRAMES ARE WRITTEN AS THEY ARE TAKEN, and they are the ONLY durable
	// thing this run makes: a beat whose frame cannot be written is an ERROR that
	// stops the run, because a ledger with no frame behind it is precisely the
	// half-written film the protocol's acknowledgement exists to prevent.
	deadline := time.Now().Add(chatFilmFrameTimeout)
	for {
		rows, rerr := readFilmLedger(ledgerPath)
		if rerr != nil {
			return rerr
		}
		done, derr := f.producerDone()
		if derr != nil {
			return derr
		}
		for _, r := range rows {
			if r.beat <= f.lastBeat {
				continue
			}
			if err := f.shootBeat(r); err != nil {
				return err
			}
			f.lastBeat = r.beat
			deadline = time.Now().Add(chatFilmFrameTimeout)
		}
		if done >= 0 && f.lastBeat >= done {
			break
		}
		// A terminal that is GONE while the marker is still absent means the producer
		// died inside it: report that at once with the two logs that hold its
		// reasons, rather than waiting out a bound aimed at a slow-but-alive run.
		if done < 0 {
			if exited, status := f.terminal.Exited(); exited {
				fmt.Fprintf(os.Stderr, chatFilmProducerGoneReport+"\n", status, f.lastBeat, f.evidenceHint())
				break
			}
		}
		if done < 0 && time.Now().After(deadline) {
			// The producer stopped without writing the completion marker: its beats
			// are delivered and counted, and the shortfall is STATED (Rule 15).
			fmt.Fprintf(os.Stderr, chatFilmTimeoutReport+"\n", chatFilmFrameTimeout, f.lastBeat)
			break
		}
		time.Sleep(chatFilmPollMS * time.Millisecond) //stylegate:allow-sleep polling the producer's own ledger
	}
	// A FILM WITH NO FRAMES IS NOT A FILM. The producer may have refused before its
	// first beat (a bad session id, a missing terminal), and in that case the
	// reason is in its own output — so it is reported as a FAILURE naming that
	// file, never as a successful run that happened to write nothing. Rule 15: the
	// empty result and the good result must not look the same.
	if f.frames == 0 {
		return fmt.Errorf("no frame was taken: the filmed command never delivered a beat — %s", f.evidenceHint())
	}
	// The terminator: the producer's beat count, which is the number the ledger
	// must hold. A disagreement is the film refusing to claim more frames than it
	// has rows (or vice versa).
	done, derr := f.producerDone()
	if derr != nil {
		return derr
	}
	if done >= 0 && f.lastBeat != done {
		return fmt.Errorf("the producer finished at beat %d but the film holds %d frame(s) — the ledger and the frames disagree, so this film is refused rather than reported as complete", done, f.lastBeat)
	}
	// The ledger is COPIED beside the frames: the film's own record of which beat
	// produced which frame travels with the film (the scratch it was written in is
	// a temp directory, and a film whose ledger stayed behind would be
	// uncheckable).
	if err := f.copyLedger(); err != nil {
		return err
	}
	rows, cerr := countFilmRows(filepath.Join(f.outDir, replay.LedgerName))
	if cerr != nil {
		return cerr
	}
	f.rows = rows
	fmt.Fprintf(os.Stderr, chatReplayDirsReport+"\n", f.outDir, f.frames, f.rows, f.settled, f.unsettled)
	if f.unsettled > 0 {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%d frame(s) never reached the settled criterion (two consecutive grabs were never equal) — they are delivered and marked settled=no in %s; a still surface and a lost repaint must not be the same number\n",
			f.unsettled, chatFilmEvidenceName)
	}
	if f.vanished > 0 {
		fmt.Fprintf(os.Stderr, chatFilmVanishedReport+"\n", f.vanished, f.frames, chatFilmEvidenceName)
	}
	fmt.Fprintf(os.Stderr, chatFilmNotice+"beats.tsv: %s\n", filepath.Join(f.outDir, replay.LedgerName))
	// THE ANIMATION, if one was asked for. It is composed LAST, from the frames
	// this run wrote and the ledger rows it read — so the GIF cannot be built from
	// a frame set that differs from the directory's, and a failure here is
	// reported as a failure of the run (the caller asked for a GIF, and a film
	// that silently did not produce one is the defect this arm exists to close).
	if f.gifPath != "" {
		if err := f.composeGIF(); err != nil {
			return err
		}
	}
	return nil
}

// composeGIF encodes this run's frames into the requested animation through
// internal/gifgen, then holds the result to the LEDGER's own arithmetic: the GIF's
// frame count must equal the ledger's row count, and the caller is told both
// numbers. The check is the acceptance's own criterion made a runtime invariant —
// a film that animates fewer beats than it recorded is refused rather than
// reported as complete (Rule 15).
func (f *chatFilm) composeGIF() error {
	res, err := chatFilmComposeGIF(f.gifPath, f.gifFrames, f.wechat)
	if err != nil {
		return fmt.Errorf("compose the animation %s: %w", f.gifPath, err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, chatFilmGIFWarning, f.gifPath, w)
	}
	if res.Frames != f.rows {
		return fmt.Errorf(chatFilmGIFDisagreement, f.gifPath, res.Frames, f.rows)
	}
	pace := gifgenFramesPerSecond(res.DelayCS)
	fmt.Fprintf(os.Stderr, chatFilmGIFReport+"\n", f.gifPath, res.Frames, res.DelayCS, pace, res.Bytes, f.rows, f.frames)
	// THE PHONE-SHAPED PRODUCT SAYS ITS OWN FOUR READINGS, read off the WRITTEN
	// result rather than off the request: the two dimensions (and their parity),
	// the byte count against the ceiling, and the loop block the FILE carries.
	// The acceptance's claims are these numbers, so the run prints them where the
	// report can quote them verbatim.
	if f.wechat {
		fmt.Fprintf(os.Stderr, chatFilmWechatReport+"\n", res.Out, res.Width, res.Height,
			res.Bytes, wechatGIFMaxBytes, loopReadBackSpelling(res.LoopReadBack), res.Frames)
	}
	if f.evenCropped > 0 {
		fmt.Fprintf(os.Stderr, chatFilmEvenCroppedReport+"\n", f.evenCropped, f.frames)
	}
	// The ledger is the animation's words, and it is a FILE beside it rather than
	// text drawn into it (chat_film_gif.go's header). Saying so here is what keeps
	// a reader from concluding the beat identity was lost with the band.
	fmt.Fprintf(os.Stderr, chatFilmFrameLedgerHint+"\n", res.Out, filepath.Join(f.outDir, replay.LedgerName))
	return nil
}

// gifgenFramesPerSecond renders the film's pace for the closing report. It is a
// PRESENTATION of gifgen's delay, never a second definition of it: the number the
// GIF carries is the centisecond value, and this is only how a reader reads it.
// A delay below one centisecond cannot happen (gifgen's own floor is one), and a
// zero divisor is guarded rather than allowed to produce a nonsense figure
// (Rule 15).
func gifgenFramesPerSecond(delayCS int) float64 {
	if delayCS <= 0 {
		return 0
	}
	return float64(100) / float64(delayCS)
}

// shootBeat takes ONE beat's frame: the settled grab, the PNG, the evidence row,
// then the acknowledgement — in that order, because the acknowledgement releases
// the producer to draw the NEXT beat and the frame must exist first.
//
// The row is carried WHOLE (filmRow) rather than as a bare beat number: the
// animation's caption is the row's own kind and preview, so the words on frame N
// and the ledger's line N come from ONE read of the ledger (a second parse is how
// a caption ends up describing a different row than the frame beside it).
func (f *chatFilm) shootBeat(row filmRow) error {
	beat := row.beat
	grab, err := shot.SettledGrab(f.display.Name(), f.terminal, filmGrabTries(), filmGrabSettle())
	if err != nil {
		return err
	}
	name := fmt.Sprintf(chatFilmFrameFormat, beat)
	framePath := filepath.Join(f.outDir, name)
	// THE FRAME IS MADE EVEN BEFORE IT IS WRITTEN (chat_film_frames.go): the grab
	// is the WINDOW's own pixels, and a terminal whose face is a fraction of a
	// pixel tall turns the same 26 rows into an ODD 521 px — a height an encoder
	// that subsamples chroma refuses outright. On the reference face (xterm) the
	// frame is already even and `frame.Out` IS the grab's own bytes, untouched, so
	// the no-flag product does not change by a byte.
	frame, err := chatFilmEvenFrame(grab.PNG)
	if err != nil {
		return fmt.Errorf("hold the frame %s to the even-dimension contract: %w", name, err)
	}
	if frame.Cropped {
		f.evenCropped++
		fmt.Fprintf(os.Stderr, chatFilmEvenFrameNotice+"\n", name, frame.SrcW, frame.SrcH, frame.Width, frame.Height)
	}
	if err := os.WriteFile(framePath, frame.Out, 0o644); err != nil {
		return fmt.Errorf("write the frame %s: %w", name, err)
	}
	// The animation arm's input is accumulated HERE, in beat order, as the frame
	// lands: the PATH of the PNG this run just wrote, so the GIF is composed from
	// what this run actually shot and from nothing else (no caption — chat_film_gif.go's
	// header says why the band was removed).
	if f.gifPath != "" {
		f.gifFrames = append(f.gifFrames, framePath)
	}
	f.frames++
	if grab.Settled {
		f.settled++
	} else {
		f.unsettled++
	}
	f.vanished += grab.Vanished
	settled := "no"
	if grab.Settled {
		settled = "yes"
	}
	if _, err := fmt.Fprintf(f.evidence, "%d\t%d\t%d\t%s\t%d\t%d\n", beat, grab.Tries, grab.Stale, settled, len(frame.Out), grab.Vanished); err != nil {
		return fmt.Errorf("write the settled-grab evidence row for beat %d: %w", beat, err)
	}
	if err := f.evidence.Sync(); err != nil {
		return fmt.Errorf("flush the settled-grab evidence for beat %d: %w", beat, err)
	}
	ack := filepath.Join(f.scratch, replay.AckName)
	if err := os.WriteFile(ack, []byte(strconv.Itoa(beat)+"\n"), 0o644); err != nil {
		return fmt.Errorf("acknowledge beat %d in %s: %w", beat, ack, err)
	}
	return nil
}

// producerDone reports the producer's own beat count from the completion marker,
// or -1 while it is absent. It reads the file the protocol names rather than
// watching for the terminal's window to close: a terminal outlives the process
// inside it, so the window is not a statement about the run.
func (f *chatFilm) producerDone() (int, error) {
	raw, err := os.ReadFile(filepath.Join(f.scratch, replay.DoneName))
	if err != nil {
		if os.IsNotExist(err) {
			return -1, nil
		}
		return -1, fmt.Errorf("read the completion marker: %w", err)
	}
	n, cerr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if cerr != nil {
		return -1, fmt.Errorf("the completion marker %s does not hold a beat count (%q): %w", replay.DoneName, strings.TrimSpace(string(raw)), cerr)
	}
	return n, nil
}

// copyLedger brings the beat ledger beside the frames.
func (f *chatFilm) copyLedger() error {
	raw, err := os.ReadFile(filepath.Join(f.scratch, replay.LedgerName))
	if err != nil {
		return fmt.Errorf("read the beat ledger for the film: %w", err)
	}
	if err := os.WriteFile(filepath.Join(f.outDir, replay.LedgerName), raw, 0o644); err != nil {
		return fmt.Errorf("write the beat ledger beside the frames: %w", err)
	}
	return nil
}

// Close stops the terminal and the display this run owns, in that order, and
// closes the evidence file. Every failure is reported, never swallowed.
//
// It also releases the producer guard (idempotent — the ordinary path has
// already released it) and removes the guard's scratch directory, so a run
// leaves neither a waiting shell nor a file behind (Rule 30's spirit: the
// deliverable's own tree is the only thing this run adds).
func (f *chatFilm) Close() error {
	var first error
	if f.evidence != nil {
		if err := f.evidence.Close(); err != nil {
			first = fmt.Errorf("close the settled-grab evidence: %w", err)
		}
		f.evidence = nil
	}
	if f.producer != nil {
		f.producer.disarm()
		if err := f.producer.remove(); err != nil && first == nil {
			first = err
		}
		f.producer = nil
	}
	if f.terminal != nil {
		// The producer is given a bounded moment to finish its own closeout before
		// its terminal is stopped: done.txt means the run is over, and this is the
		// last paint reaching the display.
		time.Sleep(chatFilmTerminalExitWait) //stylegate:allow-sleep a bounded settle after the producer's own completion marker
		if err := f.terminal.Close(); err != nil && first == nil {
			first = err
		}
		f.terminal = nil
	}
	return first
}

// filmRow is ONE complete row of the beat ledger, as the driver reads it. The
// three fields are the ones the film CONSUMES: which beat it was, what kind of
// beat it was, and the preview that is its words. A reader that carried fewer
// would make the animation's caption a second, lossier parse of the same file.
type filmRow struct {
	beat    int
	kind    string
	preview string
}

// readFilmLedger reads every COMPLETE row the producer has written so far, in
// file order. A partially written row is SKIPPED rather than parsed: the
// acceptance (six columns) comes from internal/replay — the package that writes
// the format — so a reader and a writer cannot disagree about what a complete row
// is.
//
// A ledger that does not exist yet is not an error: it is the state before the
// producer's first beat, and the caller's loop is what waits.
func readFilmLedger(path string) ([]filmRow, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read the beat ledger %s: %w", path, err)
	}
	var out []filmRow
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if !replay.LedgerRowComplete(len(cols)) {
			continue
		}
		n, cerr := strconv.Atoi(strings.TrimSpace(cols[0]))
		if cerr != nil {
			continue
		}
		out = append(out, filmRow{beat: n, kind: cols[1], preview: cols[4]})
	}
	return out, nil
}

// readFilmBeats is readFilmLedger's beat-number projection: the driver's frame
// loop only needs to know WHICH beats exist, and the animation arm needs the row
// it came from. One reader, two callers (Rules 13/14) — a second parse of the
// same file is how a caption ends up describing a different row than the frame
// beside it.
func readFilmBeats(path string) ([]int, error) {
	rows, err := readFilmLedger(path)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.beat)
	}
	return out, nil
}

// countFilmRows counts the ledger's data rows in a file, for the closing report.
func countFilmRows(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s for the film's arithmetic: %w", path, err)
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if replay.LedgerRowComplete(len(strings.Split(line, "\t"))) {
			n++
		}
	}
	return n, nil
}

// filmGrabTries / filmGrabSettle expose the settled-grab bounds the driver uses.
// They are functions rather than inline literals so the ONE definition stays in
// internal/shot (Rule 14) and a test can read the same numbers it asserts on.
func filmGrabTries() int { return shot.FilmGrabDefaultTries() }

func filmGrabSettle() time.Duration { return shot.FilmGrabDefaultSettle() }
