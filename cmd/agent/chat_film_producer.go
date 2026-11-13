package main

// chat_film_producer.go — the producer's argv, and the ONE question the driver
// can only ask AFTER the terminal has mapped a window.
//
// WHY THIS FILE EXISTS. The film's canvas is decided in two steps, and they
// happen on opposite sides of the terminal starting:
//
//	(a) the RESOLVED canvas — the surface's ideal geometry (or the recording's
//	    own), known before anything is started, and the canvas the filmed
//	    surface is asked to draw at;
//	(b) the DELIVERY canvas — the largest cell count whose frame still fits the
//	    playable width ON THE FACE THIS TERMINAL ACTUALLY DRAWS, which is a
//	    measurement of the terminal's own window (shot.TerminalFace, read from
//	    WM_NORMAL_HINTS) and therefore unknowable until that window exists.
//
// The defect this closes (task 01a10603): the predecessor collapsed (b) into a
// constant derived from xterm's reference face, so the cap was 82 columns =
// 496 px while the film's default terminal draws the same 82 columns 836 px —
// the cap let through a canvas the composer then REFUSED, and the parity half
// of the delivery contract (the frame crop) was never reached on a real
// session. Deriving the cap from the real face is the fix; the ORDER is the
// cost of it, and the order is what this file pays.
//
// THE SURFACE RE-STATEMENT PROBLEM, AND WHY IT IS A DENIAL GUARD. A producer
// that draws BELOW a terminal is legible and fine (the layout is drawn at its
// canvas width and the terminal shows it in a bigger window); one that draws
// ABOVE its terminal WRAPS EVERY ROW, and the frames are then a picture of a
// broken surface. The producer refuses that by NAME ("this terminal is %dx%d
// but the beat protocol was asked for %dx%d"), which is the correct refusal —
// so the film cannot simply be shot at a canvas NARROWER than the resolved one
// without also re-stating that canvas to the surface, and the surface has not
// started yet at the moment the face is read.
//
// The mechanism is therefore: the producer is armed to WAIT for one bit before
// it execs, the driver starts the terminal, reads the face, decides, and either
// clears the bit (the resolved canvas is deliverable — the ordinary case, and
// the pre-existing behaviour) or rewrites the canvas into the same script
// (the surface draws exactly the canvas the film delivers). BOTH outcomes are
// the surface being told at ONE moment, which is the only property that matters
// — and the second one is measurably better than what the predecessor did at
// the same moment, because a NARROWER canvas is not a "smaller picture": it is
// the only canvas whose frame the delivery contract accepts.
//
// NOT A DEPENDENCY (Rule 35). It is one file in the run's OWN scratch tree
// (internal/scratch — off `/`, swept when this process dies, Rule 22), written
// and removed by this run, started by the same terminal that starts the
// producer. Nothing is installed, nothing lives past the run, and the driver
// removes the tree it allocated (chatFilm.Close).

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

const (
	// filmProducerGuardScriptName is the file the producer is started THROUGH.
	filmProducerGuardScriptName = "film-producer.sh"
	// filmProducerGuardFlagName is the bit the script waits for. Its ABSENCE is
	// the wait; its presence is "run the argv in this file".
	filmProducerGuardFlagName = "film-producer.argv"
	// filmProducerGuardStep is how often the driver looks for the flag. It is a
	// POLL, not a sleep the run depends on for correctness: the flag is written
	// within microseconds of the terminal mapping, so this bounds the delay
	// before the producer starts, and the producer is on the other side of a
	// pty write either way.
	filmProducerGuardStep = 50 * time.Millisecond
	// filmProducerGuardShell is the interpreter the script names in its own
	// shebang. /bin/sh is what the filmed terminal already has (it is what `-x`
	// runs), so this adds nothing to the dependency set.
	filmProducerGuardShell = "/bin/sh"
)

// chatFilmProducerArgv is the producer's own command line — ONE definition,
// referenced by the guard (which writes it into the script) and by the terminal
// starter (which runs the script), so the argv a run announces and the argv it
// executes cannot drift (Rules 13/14: it used to be a literal slice built at the
// call site, and the two readers of it were the notice and the exec).
//
// The VALUES are arguments rather than closed over, because the canvas is
// decided after the terminal has mapped: the same function is called twice, at
// the two costs above.
func chatFilmProducerArgv(exe, replayTarget, scratchDir string, cols, rows int) []string {
	return []string{
		exe,
		CliVerbChat,
		chatFlagReplay, replayTarget,
		chatFlagScratch, scratchDir,
		chatFlagCols, strconv.Itoa(cols),
		chatFlagRows, strconv.Itoa(rows),
	}
} // guardArgv is what the terminal is asked to run: the guard's SCRIPT when one is
// armed (the argv is still a value the driver may rewrite), and the argv itself
// otherwise. It is a function rather than a method call at the call site so the
// nil guard — the one case the caller has already reported — has ONE home.
func guardArgv(guard *filmProducerGuard, argv []string) []string {
	if guard == nil {
		return argv
	}
	return guard.argv()
}

// filmDeliveryCanvas decides the canvas the film will be SHOT at, from the face
// the terminal just published.
//
// The three outcomes, and why each is what it is:
//
//	the resolved canvas already fits  → released at it, nothing announced (the
//	                                    ordinary case: no size change happened)
//	it is wider than the cap          → the cap is applied (chatFilmPlayableGeometry,
//	                                    which announces the change with both pixel
//	                                    widths on the face that decided it) and the
//	                                    producer is released at the NARROWER canvas,
//	                                    so the surface draws exactly what the film
//	                                    delivers — the same rule an explicitly
//	                                    narrow --cols already gets: release the
//	                                    surface at the canvas the driver states.
//	the cap is zero                   → a REFUSAL with the numbers: the limit does
//	                                    not fit even one cell on this face, which
//	                                    means the face and the limit are
//	                                    incompatible (Rule 15 — never a canvas of
//	                                    zero columns, never a silent no-op).
//
// It DECIDES and does not act: the caller resizes the terminal to the answer and
// only then releases the producer (see the driver's own ordering note), so the
// producer never starts against a window that is still at the old size.
//
// A face that could not be measured does not narrow anything: chatFilmPlayableGeometry
// says so in words and the composer refuses the frame with the face it used.
func filmDeliveryCanvas(cols, rows int, explicit bool, errOut io.Writer) (int, int, error) {
	if explicit {
		// The operator stated the canvas: it is shot AS STATED and refused by the
		// composer if its frame is still too wide (the pre-existing contract, and
		// the reason the refusal exists at all). No narrowing, no re-statement.
		return cols, rows, nil
	}
	shotCols, shotRows := chatFilmPlayableGeometry(filmDeliveryFace, cols, rows, errOut)
	if filmDeliveryFace.Known() && shotCols == 0 {
		return 0, 0, fmt.Errorf(chatFilmPlayableNoColsRefused,
			playableGIFMaxWidth, filmDeliveryFace, chatTerminalFlag, chatFlagCols, chatFlagRows)
	}
	return shotCols, shotRows, nil
}

// chatFilmPlayableNoColsRefused is the refusal for a face on which not one cell
// fits inside the playable width: the limit is about the READER and the face is
// about the CAMERA, and a pair that cannot be reconciled is refused with both
// numbers and the two things a caller can change (the terminal, whose face it
// is; the flag that states the size). Args: the limit in px, the face, the
// terminal word, the columns flag, the rows flag.
const chatFilmPlayableNoColsRefused = "no cell count is playable on this terminal: the %d px limit is not wide enough for even one character on %s — shoot the film in a terminal with a smaller face (the %s word), or take the frames without an animation, since no %s/%s makes this combination fit"

// filmProducerInvocation renders an argv for a notice, shell-quoted, so a reader
// can paste it back. The quoting is the shell's OWN minimal form (single quotes,
// with embedded quotes re-escaped) rather than a format of this file's
// invention.
func filmProducerInvocation(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// shellQuote wraps one word in single quotes, escaping any it contains. A word
// that is entirely safe is left bare, so the common case stays readable.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`*?[](){};&|<>~!#") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// filmProducerGuard holds ONE producer until the driver has decided the canvas
// it will draw at.
//
// The zero value is NOT usable: it is constructed by newFilmProducerGuard, whose
// own name says that the allocation (a file in the scratch tree) can fail, and
// whose failure the caller must be able to see before a terminal is started
// (Rule 15). Every method is safe on a DISARMED guard, so the driver's deferred
// disarm runs on every exit path without a condition.
type filmProducerGuard struct {
	mu     sync.Mutex
	dir    string
	script string
	flag   string
	// words is the producer's argv, the value the driver may still REWRITE at the
	// release (the narrowed canvas). It is not called argv because the accessor
	// below answers with what the terminal should RUN, which is the script while
	// the guard is holding the producer (Rules 1b/13: two different values do not
	// share one name).
	words []string
	// done closes when the waiting producer has been released, so the poll
	// goroutine stops and the run leaves no reader behind. A nil channel means
	// the poll has ended.
	done   chan struct{}
	closed bool
}

// newFilmProducerGuard allocates the guard's own directory in the scratch tree
// (Rule 22: off `/`, under the root this process publishes, swept when it dies).
func newFilmProducerGuard() (*filmProducerGuard, error) {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassShot, "filmproducer-")
	if err != nil {
		return nil, fmt.Errorf("allocate the producer guard's directory: %w", err)
	}
	return &filmProducerGuard{
		dir:    dir,
		script: filepath.Join(dir, filmProducerGuardScriptName),
		flag:   filepath.Join(dir, filmProducerGuardFlagName),
	}, nil
}

// argv is the producer's own command line, for the terminal starter: the guard's
// SCRIPT when it is armed (the argv is a value the driver may still rewrite),
// and the argv itself otherwise. A guard that could not arm falls back to the
// argv — the producer then starts immediately, which is the pre-existing
// behaviour and is REPORTED by arm.
func (g *filmProducerGuard) argv() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed && len(g.words) > 0 {
		return []string{filmProducerGuardShell, g.script}
	}
	return g.words
}

// arm writes the guard and starts waiting.
//
// A guard that cannot be written is NOT fatal: the producer is started directly
// (argv() answers the argv itself) and the fact is reported on errOut, because
// the ordinary canvas — the resolved one — is the one the driver would have
// chosen anyway, and a run that cannot write one file in its own scratch tree
// has bigger news for the operator than this optimisation.
func (g *filmProducerGuard) arm(argv []string, errOut io.Writer) {
	g.mu.Lock()
	g.words = argv
	script := filmProducerGuardScript(g.flag)
	if err := os.WriteFile(g.script, []byte(script), 0o700); err != nil {
		g.closed = true
		g.mu.Unlock()
		fmt.Fprintf(errOut, chatFilmNotice+"the producer guard %s could not be written (%v), so the producer starts IMMEDIATELY at the resolved canvas and no narrowing is possible on this run\n", g.script, err)
		return
	}
	g.done = make(chan struct{})
	done := g.done
	g.mu.Unlock()

	go func() {
		ticker := time.NewTicker(filmProducerGuardStep)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if _, err := os.Stat(g.flag); err == nil {
					// The driver decided: the flag file's CONTENT is the argv it
					// decided on, and the script reads it itself.
					g.mu.Lock()
					g.closed = true
					g.mu.Unlock()
					fmt.Fprintf(errOut, chatFilmNotice+"the producer was released at the canvas this run decided (%s)\n", g.argvString())
					return
				}
			}
		}
	}()
}

// argvString renders the CURRENT argv for the release notice, under the lock.
func (g *filmProducerGuard) argvString() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return filmProducerInvocation(g.words)
}

// release writes the argv file the waiting script reads, which is what lets the
// producer exec. It is IDEMPOTENT: the driver's ordinary path releases it exactly
// once (after the terminal has been sized), and its deferred disarm releases it
// again on any other exit path — a second write would be a no-op on a producer
// that has already run, and a refusal would turn a torn-down run into an error
// nobody can act on.
func (g *filmProducerGuard) release() error {
	return g.releaseAt(0, 0)
}

// releaseAt is release with the canvas REWRITTEN: the two flags in the argv are
// replaced by the given counts before the script is handed the words.
//
// WHY A REWRITE AND NOT A SECOND PRODUCER. The producer refuses a terminal that
// disagrees with the canvas it was asked for ("this terminal is 82x26 but the
// beat protocol was asked for 117x26" — measured), so a run that shoots at a
// narrower canvas MUST state that canvas to the surface. The producer is a
// process the terminal starts, and it has already been started by the time the
// face is known — which is exactly why it is started through the guard script:
// the words are still this driver's to write at the moment it knows them.
//
// A canvas of zero or less means "as resolved" (release's own spelling).
func (g *filmProducerGuard) releaseAt(cols, rows int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	if cols > 0 && rows > 0 {
		words := make([]string, len(g.words))
		copy(words, g.words)
		for i := 0; i+1 < len(words); i += 2 {
			switch words[i] {
			case chatFlagCols:
				words[i+1] = strconv.Itoa(cols)
			case chatFlagRows:
				words[i+1] = strconv.Itoa(rows)
			}
		}
		g.words = words
	}
	body, err := filmProducerArgvFile(g.words)
	if err != nil {
		return err
	}
	if err := os.WriteFile(g.flag, []byte(body), 0o600); err != nil {
		return fmt.Errorf("release the producer (write %s): %w", g.flag, err)
	}
	g.closed = true
	return nil
}

// disarm releases the producer at its current canvas and stops the poll. It is
// what the driver's defer calls, and it is safe on a guard that was never armed
// or is already released.
func (g *filmProducerGuard) disarm() {
	if g == nil {
		return
	}
	if err := g.release(); err != nil {
		fmt.Fprintf(os.Stderr, chatFilmNotice+"%v\n", err)
	}
	g.mu.Lock()
	done := g.done
	g.done = nil
	g.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// filmProducerGuardScript is the shell program the producer is started through:
// wait for the flag, read the argv the flag's file holds (one word per line),
// then exec it.
//
// It is POSIX sh with no external command at all, so it runs on the same /bin/sh
// the terminal would have used, and it `exec`s so the filmed command REPLACES
// the shell — the terminal's own child is then the producer itself, exactly as
// before this file existed (the process tree the driver stops by pid is
// unchanged).
//
// The `while read` loop is redirected from the FILE and not piped: a pipeline
// would run it in a subshell (measured while writing this: `set --` inside a
// `cat | while` loop sets nothing in the shell that execs), so the words would be
// read and thrown away.
func filmProducerGuardScript(flagPath string) string {
	quoted := shellQuote(flagPath)
	return "#!/bin/sh\n" +
		"# written and removed by `gopi chat --film --out` (cmd/agent/chat_film_producer.go).\n" +
		"# It exists for ONE reason: the producer must be told its final canvas AFTER\n" +
		"# the filmed terminal has mapped a window and published its pixel face, and\n" +
		"# waiting here is what buys that.\n" +
		"while [ ! -f " + quoted + " ]; do sleep 1; done\n" +
		"set --\n" +
		"while IFS= read -r film_guard_word; do\n" +
		"  set -- \"$@\" \"$film_guard_word\"\n" +
		"done < " + quoted + "\n" +
		"exec \"$@\"\n"
}

// filmProducerArgvFile renders an argv as one word per line, which is the form
// filmProducerGuardScript reads: one word per line needs no quoting rules of its
// own, and a path with a space in it survives intact.
//
// A word carrying a NEWLINE cannot be represented, so it is refused by the
// caller's own contract (a path with a newline is not a path this run was given a
// way to express) — the read loop would silently split it into two words, which
// is exactly the silent corruption Rule 15 forbids.
func filmProducerArgvFile(argv []string) (string, error) {
	var b strings.Builder
	for _, a := range argv {
		if strings.ContainsRune(a, '\n') {
			return "", fmt.Errorf("the producer's argv carries a word with a newline in it (%q) — the release file is read one word per line, so this word would arrive as two and the producer would run a command nobody wrote (the column and row counts and the scratch path are the words this run composes, and none of them can hold a newline; a replay target from the caller is the one that could)", a)
		}
		b.WriteString(a)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// remove deletes the guard's own directory, so the run leaves no waiting shell
// and no released argv behind. The error is RETURNED rather than reported here:
// Close is the one reporting site, and a second writer of the same sentence
// would be a second definition of it.
func (g *filmProducerGuard) remove() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	dir := g.dir
	g.mu.Unlock()
	if dir == "" {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove the producer guard %s: %w", dir, err)
	}
	return nil
}
