package lineedit

import (
	"sync"
	"sync/atomic"
)

// menu.go — the completion menu (A7).
//
// The menu is drawn BELOW the line being edited, without ever disturbing the
// line: the editor keeps its absolute-placement + redraw contract, and the menu
// is drawn as a separate step after the editor rows (which the editor
// deliberately did not place, so they do not move).
//
// Candidate computation runs OFF the read/redraw path. The editor's redraw is
// called from the key loop; if it computed candidates inline, a slow source
// (a filesystem walk for `@` file completion) would stall typing. So the editor
// only ever reads the LAST PUBLISHED candidate list (an atomic snapshot), and a
// separate goroutine recomputes from the current line whenever the line
// changed. This is the hermes ThreadedCompleter rationale (cli.py:14323-14329):
// the completion backend is threaded, the UI thread never waits.
//
// The stub seam is Completer: a function from (line, cursor, limit) to
// candidates. Tests drive a stub (deterministic, instant); production supplies
// the slash-command + file completer from cmd/agent.

// CompletionPolicy bounds the menu and the completion work. Every value is
// named (Rule 1).
//
// NOTE (constant-source-of-truth rule): cmd/agent's renderer counts the menu
// rows too. It reads the count off the LAST RENDERED MENU (Menu.Rows(), the
// real layout) instead of re-deriving it from a limit constant, so the two
// packages cannot disagree about how many rows a menu occupies.
const (
	// completionCandidateLimit caps how many candidates are kept. The menu
	// shows the first completionPageRows of them; the rest are still reachable
	// by typing (the list narrows).
	completionCandidateLimit = 32
	// completionPageRows is how many candidate rows the menu draws at once.
	completionPageRows = 5
	// completionMinRunes is the shortest line prefix the completer is asked
	// about. One rune (the "/" that opens a slash menu) is enough.
	completionMinRunes = 1
	// completionSingleCandidate is the candidate count that makes Up/Down
	// structurally inert: with one offer the selection has nowhere to go.
	completionSingleCandidate = 1
	// completionNoteRow is the extra row a ONE-candidate menu reserves and
	// draws to state WHY Up/Down cannot move. It is the legibility half of this
	// file's answer to the operator's report on the "/q → /quit" menu: the
	// arrow press IS a no-op by design (a one-item list), and a key that
	// changes nothing on screen reads as a broken shortcut, so the menu says
	// the reason instead of staying silent. It lives inside the SAME row
	// definition as the candidate rows (Rows and MenuLines both count it), so
	// the rows the layout reserves and the rows painted cannot disagree — and
	// it is 0 for every menu with two or more candidates, which therefore
	// behaves exactly as it did before.
	completionNoteRow = 1
)

// Completer computes completion candidates for a line. It is called with the
// buffer text, the cursor (rune index), and the maximum number of candidates
// wanted. It MAY be slow (it may touch the filesystem) — it is never called on
// the editor's read/redraw path.
type Completer func(line string, cursor, limit int) []Candidate

// Candidate is one completion offer.
type Candidate struct {
	// Text replaces the word being completed.
	Text string
	// Display is the row's label (defaults to Text).
	Display string
	// Hint is an optional right-hand annotation (kind, count, description).
	Hint string
}

// Label is the row as drawn.
func (c Candidate) Label() string {
	if c.Display != "" {
		return c.Display
	}
	return c.Text
}

// Menu holds a published candidate list and the selection inside it. A Menu is
// an immutable value: the completer goroutine publishes a NEW one, and the
// editor/redraw reads the latest pointer. That is what keeps the read path
// lock-free with respect to a slow completer.
type Menu struct {
	// Candidates is the published list (already limited by the completer).
	Candidates []Candidate
	// Index is the selected row.
	Index int
	// Open is false when there is nothing to offer — then nothing is drawn and
	// Tab falls through to its normal meaning.
	Open bool
	// pageSize caps the visible rows (0 = the default page size).
	pageSize int
	// Prefix is the word the candidates complete (the replacement span).
	Prefix string
	// From, To are the rune range of the buffer the completion replaces.
	From, To int
}

// Rows is how many rows this menu occupies on screen — the real layout, which
// every consumer (editor, cmd/agent renderer) reads rather than re-deriving it
// from a limit constant. It includes the one-candidate note row (noteRows), so
// a consumer that reserves Rows() rows reserves the note's row too.
func (m Menu) Rows() int {
	if !m.Open {
		return 0
	}
	page := m.pageSize
	if page <= 0 || page > completionPageRows {
		page = completionPageRows
	}
	n := len(m.Candidates)
	if n > page {
		n = page
	}
	if n < 0 {
		n = 0
	}
	rows := n + m.noteRows()
	// An EXPLICIT page (WithPage(n), n > 0) is the menu's total row budget: the
	// one-candidate note row comes OUT of it rather than on top of it. Without
	// this the note could push the menu one row past what the layout reserved on
	// a terminal with room for a single menu row — the rows painted would exceed
	// the rows the layout gave, and the erase that follows a close would reach
	// into the row below (the status row).
	if m.pageSize > 0 && rows > m.pageSize {
		rows = m.pageSize
	}
	return rows
}

// noteRows is how many rows this menu spends STATING that Up/Down cannot move
// the selection: one for a menu whose whole list is a single candidate (the
// operator's case — "/q" offers only "/quit"), zero for every other menu. It is
// a method on the menu so the two places that must agree — Rows() (the layout's
// reservation, which cmd/agent reads) and MenuLines() (the painted cells) —
// read the SAME arithmetic (Rule 13: the constant is referenced, and the count
// is never re-derived).
//
// It is deliberately NOT "the selection did not move": that is true of a
// closed menu and of a menu whose candidate list has one item, and only the
// latter is worth a row. The condition is a property of the LIST, so it is a
// property of what is on screen — never of which key was pressed last.
//
// The decision and its two halves are recorded in
// devdoc/lineedit-one-candidate-menu.md (the one-item ruling and the extra row).
func (m Menu) noteRows() int {
	if !m.Open || len(m.Candidates) != completionSingleCandidate {
		return 0
	}
	return completionNoteRow
}

// noteLine is the one-candidate row as drawn. It names the reason an Up/Down
// press changes nothing — "nothing to move to" — and repeats the candidate so
// the reader is told not to look for another spelling. It is a row of its own,
// not a candidate row: the note carries no selection marker, so it can never be
// mistaken for an offer (and Tab can never complete to it).
func (m Menu) noteLine(width int) string {
	if m.noteRows() == 0 {
		return ""
	}
	// REASON FIRST, so a truncated row (a narrow terminal) still carries the
	// half that answers the key press; the second half is the reassurance, and
	// it is the part allowed to fall off the edge.
	line := menuNoteIndent + completionMenuNote + menuNoteMid + menuNoteMoveInert
	return truncateCols(line, width)
}

// completionMenuNote is the REASON the one-candidate row states — "nothing to
// move to" — exported so the tests (and the `/q` acceptance test in cmd/agent)
// name the reason as a constant instead of matching on a literal, and so a future
// edit to the wording cannot leave a control expecting text that is gone. It is
// the row's LEADING text too, so the reason survives the row's truncation.
const completionMenuNote = menuNoteWhy

// Menu note-row chrome. Named (Rule 1b): this row's spelling is presentation
// vocabulary compared on screen, so it lives in one place with the marker
// glyphs above rather than as a literal inside the format string.
const (
	// menuNoteIndent lines the note up with the candidate LABELS (whose marker
	// column is menuMarkerActive's own width), not with the markers.
	menuNoteIndent = "  "
	// menuNoteMid separates the note's two halves.
	menuNoteMid = " — "
	// menuNoteMoveInert is half one: the arrow key's guarantee.
	menuNoteMoveInert = "the only match"
	// menuNoteWhy is half two: the reason a press on Up or Down cannot be seen
	// to do anything — the other half of "a key press is never a silent no-op".
	menuNoteWhy = "nothing to move to"
)

// WithPage sizes the visible window. Candidates are never dropped: a layout
// with room for fewer rows shows FEWER at once (the window follows the
// selection), so every offer stays reachable.
func (m Menu) WithPage(n int) Menu {
	if n < 0 {
		n = 0
	}
	m.pageSize = n
	return m
}

// window is the first candidate row currently visible: the selection is kept
// inside the visible band, scrolling the list when it moves past the edge.
func (m Menu) window() int {
	rows := m.Rows()
	if rows <= 0 || len(m.Candidates) <= rows {
		return 0
	}
	start := m.Index - rows + 1
	if start < 0 {
		start = 0
	}
	if start+rows > len(m.Candidates) {
		start = len(m.Candidates) - rows
	}
	return start
}

// Selected returns the chosen candidate, or false when the menu is closed or
// empty.
func (m Menu) Selected() (Candidate, bool) {
	if !m.Open || len(m.Candidates) == 0 {
		return Candidate{}, false
	}
	i := m.Index
	if i < 0 || i >= len(m.Candidates) {
		i = 0
	}
	return m.Candidates[i], true
}

// Close returns the menu with nothing offered (Tab falls through, nothing is
// drawn).
func (m Menu) Close() Menu {
	m.Open = false
	m.Candidates = nil
	m.Prefix = ""
	m.From, m.To = 0, 0
	return m
}

// MoveSelection moves the selection by delta rows, wrapping. A closed or empty
// menu is unchanged.
//
// A ONE-CANDIDATE menu does not wrap, it CLAMPS: (Index+delta)%1 is 0 for every
// delta, so the arithmetic already cannot move the selection, and the guard
// below says so in the one place the behaviour is decided. This is the fix's
// "by design" half — there is no second row to move to — and the reason the
// press is not a silent no-op is the note row the menu draws for exactly this
// list (noteRows), not a change of mind about wrapping two or more candidates.
func (m Menu) MoveSelection(delta int) Menu {
	n := len(m.Candidates)
	if !m.Open || n == 0 {
		return m
	}
	if n == completionSingleCandidate {
		return m
	}
	i := (m.Index + delta) % n
	if i < 0 {
		i += n
	}
	m.Index = i
	return m
}

// completionState owns the async side: the last requested line, the published
// menu, and the single worker that recomputes candidates. Only ONE request is
// in flight at a time, and a request that is superseded is dropped — the
// worker always computes the LATEST line, so a fast typist never waits for a
// stale computation's results.
type completionState struct {
	completer Completer
	// started guards the worker goroutine: it is launched by whichever comes
	// first — construction with a completer, or a later SetCompleter.
	started bool

	mu       sync.Mutex
	pending  string // the line to compute for ("" = nothing pending)
	seq      uint64 // request generation; the result is published only if it is still the newest
	finished uint64 // generation of the newest published result
	// rev counts PUBLISHES (worker results, selection moves, cancels). It is
	// NOT the same thing as `finished`: `finished != seq` means "a request is
	// still in flight", which is false the instant the worker publishes — and
	// the worker can publish AFTER the editor's repaint and BEFORE its pending
	// check, so the two together made a drawn menu missable. The editor
	// remembers the rev it last drew and wakes whenever it moves, so a
	// published menu can never be left undrawn.
	rev       uint64
	published Menu

	// notify wakes a blocked read after the worker PUBLISHED candidates. The
	// candidates must be drawn by the read loop (it owns every terminal write),
	// so the publish needs a wake of its own: the bounded completion poll alone
	// is a budget, not a guarantee — a budget that expires before the worker is
	// scheduled leaves the loop parked with the result unpublished on screen.
	notify Notifier
	// published is set by the WORKER when it installs a new menu, and cleared by
	// the read loop when it draws one. It is the "is there a candidate list the
	// screen has not seen yet" flag: the loop draws only when one is pending, so
	// a wake never costs a repaint it does not need. Atomic because the producer
	// is the worker goroutine and the consumer is the read loop.
	unread atomic.Bool

	wake chan struct{}
	quit chan struct{}
	once sync.Once
}

// setNotification installs the wake seam the worker uses after a publish. A nil
// sink is replaced by the no-op one, so the field is never nil and a publish
// cannot panic (production installs the terminal's own seam; the editor passes
// whatever it was given, including the no-op).
func (c *completionState) setNotification(n Notifier) {
	if c == nil {
		return
	}
	if n == nil {
		n = noNotify{}
	}
	// Under the lock, like the func-shaped seam below (setNotifier): the read
	// loop READS this field (notifyLocked, from StartLine's cancel and from the
	// worker's publish), so assigning it bare is a data race with a read loop
	// that is already running — measured in cmd/agent under -race: a caller that
	// installs the status source (SetStatus -> Editor.SetNotification ->
	// completionState.setNotification) after the editor started raced
	// completionState.notifyLocked. The field is small and the lock is not on any
	// hot path, so there is no reason to publish it unsynchronised.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = n
}

// menuUnread reports whether the worker published a candidate list the read loop
// has not drawn yet. It PEEKS: the flag is consumed by noteDrawn, so a peek that
// is followed by a repaint still leaves the flag set and the draw still happens.
func (c *completionState) menuUnread() bool {
	return c != nil && c.unread.Load()
}

// noteDrawn clears the unread flag. The read loop calls it from repaint, i.e.
// exactly when the published menu reached the screen.
func (c *completionState) noteDrawn() {
	if c != nil {
		c.unread.Store(false)
	}
}

// newCompletionState starts the worker. A nil completer yields a state that
// never opens a menu (the editor then behaves exactly as it did before A7).
func newCompletionState(completer Completer) *completionState {
	c := &completionState{
		completer: completer,
		wake:      make(chan struct{}, 1),
		quit:      make(chan struct{}),
	}
	if completer != nil {
		c.started = true
		go c.worker()
	}
	return c
}

// stop ends the worker. Safe to call more than once.
func (c *completionState) stop() {
	c.once.Do(func() { close(c.quit) })
}

// setCompleter replaces the completion source. The worker reads c.completer
// under the mutex, so a later request is served by the new source (the editor's
// own tests inject one after construction; production installs it up front).
func (c *completionState) setCompleter(completer Completer) {
	c.mu.Lock()
	c.completer = completer
	start := c.completer != nil && !c.started
	if start {
		c.started = true
	}
	c.cancelLocked()
	c.mu.Unlock()
	// The worker is launched OUTSIDE the lock: cancelLocked is a no-op for it,
	// but holding the mutex across a goroutine launch is how a future edit
	// (the worker taking the lock on its first iteration) becomes a deadlock.
	if start {
		go c.worker()
	}
}

// CompletionCandidateLimit is how many candidates a completer is asked for — the
// menu's own cap, exported so a caller can ask for the same number the menu
// shows (Rule 1: one definition, referenced, never a copied literal).
func CompletionCandidateLimit() int { return completionCandidateLimit }

// CompletionMenuNote is the REASON a one-candidate menu states on its own row —
// "nothing to move to" — exported so a caller (cmd/agent's `/q` acceptance test)
// can assert the row without copying the wording (Rule 13: the constant is
// referenced, never re-typed). The screen and the control that checks it read one
// string, so a reworded row cannot leave the check passing against text that is
// no longer drawn.
func CompletionMenuNote() string { return completionMenuNote }

// CompletionNoteRow is how many rows a ONE-candidate menu spends stating that
// reason — the very constant Rows() folds into its total, exported so a caller
// can verify the menu the layout reserves is the menu painted (Rule 13/14).
func CompletionNoteRow() int { return completionNoteRow }

// MenuMarkerActive is the marker a menu row carries when it IS the selection —
// exported so a caller reading the painted grid can find the selected row without
// copying the glyph (Rule 1b/13: the marker is presentation vocabulary compared on
// screen, and a control that retyped ">" would keep passing after a re-glyph).
func MenuMarkerActive() string { return menuMarkerActive }

// request asks for candidates for line. It never blocks: it records the line
// and nudges the worker. The editor's read/redraw path calls exactly this.
func (c *completionState) request(line string) {
	if c.completer == nil {
		return
	}
	c.mu.Lock()
	c.pending = line
	c.seq++
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default: // already nudged — the worker reads the latest line anyway
	}
}

// snapshot returns the published menu. It is a lock-protected read of a small
// value, taken once per redraw — never a computation.
func (c *completionState) snapshot() Menu {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.published
}

// requestPending reports whether the newest request has no published result yet
// — the editor's cue to wake on a bounded wait so the candidates reach the
// screen without another keystroke. It is a lock-protected read of two counters,
// taken once per read-loop iteration (never a computation).
func (c *completionState) requestPending() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.finished != c.seq
}

// publish installs a menu (the worker's result, or the editor's own selection
// change — selection is computed on the read path because it is pure policy,
// no I/O).
func (c *completionState) publish(m Menu) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishLocked(m)
}

// publishLocked installs a menu for a caller that already holds the mutex. Every
// install bumps rev, so the editor's "the published menu changed" test is a
// generation comparison — it cannot be missed (only seen), unlike a flag the
// publisher would have to re-arm.
func (c *completionState) publishLocked(m Menu) {
	c.published = m
	c.rev++
	c.notifyLocked()
}

// notifyLocked wakes a blocked reader, if a notifier was installed. It is
// called with the mutex held by every publisher, so a reader that has just
// observed the publish generation is either already past its check (and will
// read the new menu on the next wake) or is woken here — the two orders cannot
// both miss.
func (c *completionState) notifyLocked() {
	if c.notify != nil {
		c.notify.Notify()
	}
}

// setNotifier installs the wake mechanism from a plain function (the tests'
// scripted source). Production installs the terminal's Notifier through
// setNotification; this is the same seam with a func-shaped caller, wrapped so
// the publish path has ONE call to make either way.
func (c *completionState) setNotifier(f func()) {
	if c == nil || f == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notify = funcNotifier(f)
}

// publishedRev reports the publish generation and the menu that goes with it,
// read together so the editor never pairs a menu with a generation it was not
// published under.
func (c *completionState) publishedRev() (uint64, Menu) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rev, c.published
}

// cancel drops the menu AND any request still in flight. A close (Escape) must
// invalidate the pending computation, not merely hide its result: the worker
// publishes whatever it computed for the line it was asked about, so a menu
// closed while candidates were being computed would otherwise REOPEN by itself
// a moment later (the worker's answer arriving after the user said no).
// Bumping seq makes an in-flight result stale, so the worker's own
// `seq == seq` check discards it.
func (c *completionState) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelLocked()
}

// cancelLocked is cancel for a caller that already holds the mutex. The two
// are separate because sync.Mutex is not reentrant: a helper that locked
// unconditionally would WEDGE the first caller that held it (a self-deadlock
// is a process stall, not an error — nothing else in the process can report
// it). Every mutex-taking entry point of completionState is a thin wrapper
// over a *Locked body for exactly this reason.
func (c *completionState) cancelLocked() {
	c.pending = ""
	c.seq++
	c.finished = c.seq
	c.publishLocked(Menu{})
}

// worker recomputes candidates whenever a line is requested. It computes
// OUTSIDE the mutex, so a slow completer never blocks the editor's snapshot.
func (c *completionState) worker() {
	for {
		select {
		case <-c.quit:
			return
		case <-c.wake:
		}
		c.mu.Lock()
		line, seq := c.pending, c.seq
		c.mu.Unlock()

		menu := c.compute(line)

		c.mu.Lock()
		// Publish only if no newer request arrived while we computed: a stale
		// result must never replace a fresher menu.
		if c.seq == seq {
			c.publishLocked(menu)
			c.finished = seq
			c.unread.Store(true)
		}
		c.mu.Unlock()
		// Wake the read loop so it DRAWS what was just published. This is the
		// worker's half of the single-writer rule: the worker computes off-path
		// and never touches the terminal; the loop is the only writer, and this
		// wake is how it learns there is something new to draw. Without it the
		// candidates reach the screen only if a bounded completion poll happens
		// to fire again — a budget, not a guarantee, and one the scripted source
		// drains instantly (so a late-scheduled worker loses the race outright).
		if c.notify != nil {
			c.notify.Notify()
		}
	}
}

// compute runs the completer for one line (off the editor's path).
func (c *completionState) compute(line string) Menu {
	runes := []rune(line)
	if len(runes) < completionMinRunes {
		return Menu{}
	}
	cands := c.completer(line, len(runes), completionCandidateLimit)
	if len(cands) == 0 {
		return Menu{}
	}
	from, prefix := completionSpan(runes, len(runes))
	return Menu{Open: true, Candidates: cands, Prefix: prefix, From: from, To: len(runes)}
}

// completionSpan finds the word being completed at the cursor: the rune range
// from the start of the current token to the cursor. A "/" at the start of the
// line makes the whole first token the completion prefix (a slash command);
// otherwise the token is delimited by unicode whitespace.
func completionSpan(runes []rune, cursor int) (from int, prefix string) {
	if cursor > len(runes) {
		cursor = len(runes)
	}
	from = cursor
	for from > 0 {
		r := runes[from-1]
		if r == ' ' || r == '\t' || r == '\n' {
			break
		}
		from--
	}
	// A slash command is the whole first token, so "/ta" completes against
	// the command table even though the token starts at 0.
	return from, string(runes[from:cursor])
}

// Complete applies the selected candidate to the buffer (Tab's action). It
// reports false when there is nothing to apply (the caller then falls through
// to the key's normal meaning).
func (m Menu) Complete(v *Value) bool {
	cand, ok := m.Selected()
	if !ok || v == nil {
		return false
	}
	runes := []rune(v.Text())
	from, to := m.From, m.To
	if from < 0 || from > len(runes) || to < from || to > len(runes) {
		return false
	}
	v.ReplaceRange(from, to, cand.Text)
	return true
}

// MenuLines renders the menu as rows of text (the cell content, no styling).
// It is the single definition of what a menu row says, used by the editor's
// renderer and by the tests' cell-grid assertions.
func (m Menu) MenuLines(width int) []string {
	rows := m.Rows()
	if rows == 0 {
		return nil
	}
	start := m.window()
	out := make([]string, 0, rows)
	// noteRow is the note row's own index — one past the candidate rows the loop
	// below draws, and the index the note is emitted at. When the page budget
	// ate the note row (Rows() == len(candidates)) the note row is NOT drawn at
	// all: the list is stated and the reason is not, never a half row.
	candidateRows := len(m.Candidates) - start
	if candidateRows > rows {
		candidateRows = rows
	}
	for i := 0; i < candidateRows; i++ {
		idx := start + i
		line := menuMarkerActive + completionSpacing + m.Candidates[idx].Label()
		if hint := m.Candidates[idx].Hint; hint != "" {
			line += completionSpacing + completionHintOpen + hint + completionHintClose
		}
		line = truncateCols(line, width)
		if idx != m.Index {
			// Non-selected rows keep the marker column's width so the labels
			// line up (the marker itself is only drawn on the selected row).
			line = menuMarkerIdle + line[len(menuMarkerActive):]
		}
		out = append(out, line)
	}
	// The one-candidate note row is the LAST row (below the single offer, where
	// a second candidate would have been). It is appended inside this same
	// function so the painted cells and Rows() — the count the layout reserves
	// and cmd/agent reads — are one arithmetic, never two that can disagree.
	if candidateRows < rows {
		if note := m.noteLine(width); note != "" {
			out = append(out, note)
		}
	}
	return out
}

// Menu row chrome. Named (Rule 1b): the marker glyphs and the hint brackets are
// the menu's presentation vocabulary, compared in tests and written by the
// renderer — one place, not a literal per call site.
const (
	menuMarkerActive    = ">"
	menuMarkerIdle      = " "
	completionSpacing   = " "
	completionHintOpen  = "("
	completionHintClose = ")"
)

// DrawnRow is one painted menu row plus where it went (the cell-grid tests
// assert the pair, so a row drawn at the wrong offset fails rather than
// passes).
type DrawnRow struct {
	// Row is the 0-based screen row the line was drawn on.
	Row int
	// Text is the line's cell content.
	Text string
}

// recordMenuPaint remembers the rows the menu now occupies, so the next paint —
// or a close — erases exactly the rows it is vacating. Without this bookkeeping
// a closed menu (Escape) would leave its rows painted on screen, and a menu
// whose list shrank would keep its stale tail: the rows are placed absolutely,
// so nothing else ever overwrites them.
func (r *region) recordMenuPaint(base, rows int) {
	r.paintedBase = base
	r.paintedRows = rows
}

// eraseMenuPaint blanks the menu rows currently on screen and forgets them. It
// is a no-op when nothing was painted (or the region is inactive — then the
// full-screen renderer owns the cursor and the rows are its own layout).
func (r *region) eraseMenuPaint() {
	if !r.active || r.paintedRows == 0 {
		return
	}
	for i := 0; i < r.paintedRows; i++ {
		r.emitAt(r.paintedBase+i, 0, escEraseLine)
	}
	r.paintedRows = 0
}

// draw paints the menu rows and leaves the terminal cursor back where the user
// is typing. It is a no-op for a closed menu or an inactive region.
//
// The rows are placed ABSOLUTELY, one erase-and-write each: the menu is drawn
// below the editing line, so it can never disturb it, and because the editing
// line's rows were placed too (never left to flow), re-drawing the menu between
// keystrokes moves nothing the user is looking at.
func (m Menu) draw(r *region, renderer *Renderer, width int) []DrawnRow {
	if !r.Active() {
		return nil
	}
	// Whatever the previous paint left is erased first — EXCEPT when the caller
	// already erased it before moving the layout (the editor's repaint path
	// erases the old rows before the layout shift, because after the shift
	// those rows carry the editing line). Re-erasing here would blank cells
	// that no longer belong to the menu.
	if r.paintedRows > 0 && r.paintedRows == r.Plan().MenuRows {
		r.eraseMenuPaint()
	}
	rows := m.Rows()
	if rows == 0 {
		return nil
	}
	lines := m.MenuLines(width)
	if len(lines) == 0 {
		return nil
	}
	// The menu's base is its own row in the layout (computed from the editing
	// line's height, never from where the previous paint happened to stop), so
	// a second candidate list of a different length cannot leave the first row
	// behind.
	base := r.Plan().MenuTop
	if below := r.EditorTop() + renderer.DrawnRows(); below > base {
		// The editing line drew more rows than the layout reserved for it: the
		// menu starts below the rows really painted.
		base = below
	}
	drawn := make([]DrawnRow, 0, len(lines))
	for i, line := range lines {
		// The erase, the placement and the line ride ONE write, so the read
		// loop cannot land between the menu's placement and its text (the menu
		// rows are placed absolutely, and a bare placement is the window).
		r.emitAt(base+i, 0, escEraseLine)
		renderer.ControlAt(base+i, 0, line)
		drawn = append(drawn, DrawnRow{Row: base + i, Text: line})
	}
	r.recordMenuPaint(base, len(lines))
	// Hand the cursor back to the editing row (the menu is view-only), using
	// the renderer's own remembered layout — one definition of "where the
	// cursor is" is the point of the redraw contract. The renderer's cursorRows
	// is relative to the editing line's first row, so the layout's own
	// EditorTop is what turns it into a screen row.
	r.place(r.EditorTop()+renderer.cursorRows, renderer.cursorCol)
	return drawn
}
