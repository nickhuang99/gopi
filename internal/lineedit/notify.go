package lineedit

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// notify.go — the wake pipe: how a publisher that is NOT the read loop gets a
// draw into the read loop.
//
// The defect it closes (measured, cmd/agent): the chat REPL refreshed the status
// row from its OWN goroutine — Editor.StatusTick → AnswerRenderer.drawStatus —
// while the editor's read loop wrote the editing line through the same writer.
// Two goroutines, two absolute-cursor byte streams, one terminal: the streams
// interleave between a cursor placement and the text that placement was for.
//
// The fix has two halves and this file is the second one. The first is that a
// status refresh is a PUBLISH, not a write (Editor.PublishStatus →
// AnswerRenderer.PublishStatus). This half is what makes the publish reach the
// screen anyway: whoever publishes writes one byte into the wake pipe, the read
// loop's poll(2) wakes on it, and the READ LOOP draws. Every terminal write
// therefore has exactly one owner (the read loop). A bounded poll would
// eventually discover the publish, but the status row exists to be read DURING a
// turn, when no keystroke is coming to wake the loop.

// wakePipeBytes is the wake pipe's buffer: one byte is the whole vocabulary, so
// a full pipe already carries the same information as a new write.
const wakePipeBytes = 1

// wakePipe is a one-way notification: Write is called by whoever publishes
// something the reader must see, and the read end is polled by the terminal
// alongside stdin and the resize pipe.
//
// readFD / writeFD are captured ONCE at creation because Go's (*os.File).Fd()
// forces a descriptor back into BLOCKING mode — a drain that re-derived the fd
// would silently undo the non-blocking flag it depends on.
type wakePipe struct {
	r       *os.File
	w       *os.File
	readFD  int
	writeFD int
}

// noNotify is the sink used when no wake pipe was installed. It does nothing and
// REPORTEDLY does nothing, so a caller can announce the degradation instead of
// pretending the publish reached the screen.
type noNotify struct{}

// Notify does nothing.
func (noNotify) Notify() {}

// NotifyEnabled reports false: a publish will not reach a blocked read.
func (noNotify) NotifyEnabled() bool { return false }

// wakeNotify is the sink a *Terminal exposes as its Notifier: it writes one byte
// into the terminal's wake pipe, which the read loop polls alongside stdin.
type wakeNotify struct{ t *Terminal }

// Notify writes one byte into the wake pipe, interrupting a blocked poll.
func (n wakeNotify) Notify() { n.t.wake.signal() }

// NotifyEnabled reports true only when the pipe is really installed and polled.
func (n wakeNotify) NotifyEnabled() bool { return n.t != nil && n.t.wake != nil }

// funcNotifier adapts a plain wake function to the Notifier seam, so a caller
// that only has a func (a test's scripted source) and a caller that has the
// terminal's sink install the SAME mechanism.
type funcNotifier func()

// Notify calls the wrapped function.
func (f funcNotifier) Notify() { f() }

// NotifyEnabled reports true: a function that was handed over really wakes the
// reader it was built for.
func (f funcNotifier) NotifyEnabled() bool { return true }

// wakeSink adapts a wakePipe to the Notifier seam the editor publishes through.
type wakeSink struct{ p *wakePipe }

// Notify writes one byte into the pipe, waking the read loop's poll.
func (s *wakeSink) Notify() { s.p.signal() }

// NotifyEnabled reports true: the pipe is installed and polled.
func (s *wakeSink) NotifyEnabled() bool { return true }

// newWakePipe builds the pair, both ends non-blocking. A failure is returned so
// the caller can degrade LOUDLY (Rule 15) rather than lose the notification
// with no trace: without it the status row updates on the next keystroke.
func newWakePipe() (*wakePipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("lineedit: create wake pipe: %w", err)
	}
	readFD, writeFD, err := setNotificationPipe(r, w)
	if err != nil {
		// deliberately ignored: the setup failure is the one reported; closing
		// the two descriptors just opened cannot fail usefully.
		_ = r.Close()
		_ = w.Close() //stylegate:ignore the setup failure is the one reported; closing the two descriptors just opened cannot fail usefully
		return nil, err
	}
	return &wakePipe{r: r, w: w, readFD: readFD, writeFD: writeFD}, nil
}

// signal notifies the reader. It never blocks: the reader drains the pipe, and a
// wake-up already pending is the same information as a second one.
func (p *wakePipe) signal() {
	if p == nil {
		return
	}
	// deliberately ignored: a full pipe (EAGAIN) already holds an unread
	// notification, which is all the reader needs.
	_, _ = unix.Write(p.writeFD, []byte{0})
}

// drain empties the pipe so the next poll blocks again. It reads until the pipe
// reports empty, which is only observable on a NON-BLOCKING descriptor (see
// setNotificationPipe) — a blocking read would park forever on the second read.
func (p *wakePipe) drain() {
	if p == nil {
		return
	}
	var scratch [wakePipeBytes]byte
	for {
		n, err := unix.Read(p.readFD, scratch[:])
		if n <= 0 || err != nil {
			return
		}
	}
}

// close tears both ends down. Safe to call more than once.
func (p *wakePipe) close() {
	if p == nil {
		return
	}
	// deliberately ignored: teardown of a pipe we own cannot fail usefully, and
	// nothing is left to report it to.
	_ = p.r.Close()
	_ = p.w.Close() //stylegate:ignore teardown of a pipe we own cannot fail usefully, and nothing is left to report it to
}

// setNotificationPipe turns both ends of a pipe non-blocking and returns their
// raw descriptors.
func setNotificationPipe(r, w *os.File) (readFD, writeFD int, err error) {
	readFD = int(r.Fd())
	writeFD = int(w.Fd())
	if err := unix.SetNonblock(readFD, true); err != nil {
		return 0, 0, fmt.Errorf("lineedit: make wake pipe read end non-blocking: %w", err)
	}
	if err := unix.SetNonblock(writeFD, true); err != nil {
		return 0, 0, fmt.Errorf("lineedit: make wake pipe write end non-blocking: %w", err)
	}
	return readFD, writeFD, nil
}
