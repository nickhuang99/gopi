package main

// chat_queue.go — the chat REPL's OWN prompt queue (`/queue`).
//
// WHAT THIS IS, AND WHAT IT IS NOT. There are two "next turn" seams in a chat
// session and they are OWNED BY DIFFERENT THINGS:
//
//   - the AGENT owns the self-driven seam (internal/agent ChatSurfaceTakePending,
//     ONE slot, spent by the model's `chat_surface type` action and bounded by
//     chatSurfaceLoopBound). It exists so the MODEL can type the next turn.
//   - the OPERATOR owns this one: a multi-slot FIFO of prompts typed at the
//     prompt while a turn is still running. It exists because a person who knows
//     what they want to ask next should not have to hold it in their head until
//     the turn ends.
//
// The two are ONE LOOP at the top of the REPL: the agent's pending line is taken
// FIRST (the model's own turn keeps its precedence), and only when the agent has
// nothing pending does this queue hand the loop its next line. That is the whole
// design: NO second next-turn mechanism is invented, and NO second bound is
// invented either — the agent's own loop bound is untouched by this file, and the
// accounting that keeps the two budgets from being confused lives at the seam in
// cli_chat.go.
//
// BOUNDED, AND A REFUSAL IS A SENTENCE (Rule 15). The queue has a named ceiling
// (chatQueueBound). An add past it is REFUSED — naming the bound and what the
// operator can do about it — never a silent drop. This is the one fact the design
// must not get wrong: a prompt that was accepted and then vanished is worse than a
// prompt that was never accepted.
//
// The state is VISIBLE on every mutation: each add, clear and refusal writes its
// own note row through the SAME notice seam the self-driven loop uses
// (input.WriteNotice), so the operator reads the queue change on the console
// instead of inferring it from whatever the next turn turns out to be.

import (
	"fmt"
	"strings"
	"sync"
)

// The queue's vocabulary. Every word this type answers to, and the bound it
// enforces, is a constant (Rule 1): the dispatcher, the refusal sentence, the
// list rendering, the help block and the tests all read the SAME spelling, so a
// rename can never leave a refusal naming a word the switch no longer accepts.
const (
	// chatQueueCommandName is the slash word that owns this type. It is a
	// REPL-native word (like /quit, /tools, /help): it mutates REPL state the
	// shared command table (cmd/agent/commands.go) cannot see, so the table must
	// not be asked to dispatch it, and chatReplSlashVocabulary — not the registry
	// — is what answers `/help queue`.
	chatQueueCommandName = "queue"
	// chatQueueClearWord is the argument that empties the queue: the same word
	// `/clear` resets the conversation with, deliberately, because one vocabulary
	// for "throw it away" is one thing to learn across the REPL's two lists.
	chatQueueClearWord = webClearAliasName
	// chatQueueListWord is the explicit spelling of the bare form. Bare `/queue`
	// already lists; the word exists so a scripted line can say what it means, and
	// because `/queue list` must be read as "list", never as a prompt about lists.
	chatQueueListWord = "list"
)

// chatQueueBound is the queue's ceiling — how many prompts may wait at once. It is
// DECLARED here and enforced at the ONE point that can exceed it (chatQueue.Add),
// because a bound nothing reads is not a bound (Rule 1). The number is the same
// order as the agent's own self-driven loop bound (chatSurfaceLoopBound) without
// SHARING its value: those are two different budgets spent by two different owners
// — a person typing ahead, and the model typing for itself — and one number
// serving both would let a change to either silently retune the other.
const chatQueueBound = 8

// The notes this type writes to the console. Every mutation has one — that IS the
// visibility half of the feature — and each names the count that remains, so the
// operator never has to remember what the previous one said.
const (
	// chatQueueAddedNote is written when a prompt is parked, with its position in
	// the FIFO and the queue's depth.
	chatQueueAddedNote = "[gopi] queued #%d of %d: %s\n"
	// chatQueueEmptyNote is written when the queue has nothing to show. A bare
	// silence would be indistinguishable from a command that never ran.
	chatQueueEmptyNote = "[gopi] queue is empty — `/queue <prompt>` parks a prompt for after the running turn.\n"
	// chatQueueHeaderNote opens the list rendering, naming the depth and the
	// bound, so "3 items" and "how many fit" are both on screen at once.
	chatQueueHeaderNote = "[gopi] queue (%d of %d, in order — sent as the next turns when the current one ends):\n"
	// chatQueueItemNote is one row of the list.
	chatQueueItemNote = "[gopi]   %d. %s\n"
	// chatQueueClearedNote is written when the queue is emptied, naming how many
	// prompts were dropped (never "cleared." with the count left to memory).
	chatQueueClearedNote = "[gopi] queue cleared — %d parked prompt(s) dropped; nothing will run from the queue.\n"
	// chatQueueClearEmptyNote is the no-op case, stated so the operator knows the
	// command ran and there was simply nothing to drop.
	chatQueueClearEmptyNote = "[gopi] queue was already empty — nothing to clear.\n"
	// chatQueuePopNote is written when a parked prompt becomes a turn, naming the
	// prompt. Without this the queued text would simply appear as the next turn
	// and read as if it had been typed twice.
	chatQueuePopNote = "[gopi] → (queued) %s"
	// chatQueueQuitNote is the closeout's statement about the prompts it is
	// abandoning. It is the honest half of the exit contract: the queue is
	// in-memory REPL state and ending the session DROPS it — stated, never left
	// to inference.
	chatQueueQuitNote = "[gopi] %d parked prompt(s) in the queue will be DROPPED — the queue does not survive the session. `/queue clear` first, or re-queue them next time.\n" // chatQueueNoSeamRefusal is the refusal for a run whose input layer cannot
	// carry a queued turn at all. A cooked/one-shot source reads ONE line per
	// caller push: there is no loop-boundary seam for a parked prompt to become a
	// turn at, so accepting one would be a promise this carrier cannot keep.
	chatQueueNoSeamRefusal = "`/" + chatQueueCommandName + "` needs the interactive line editor: this run reads its lines from the caller (piped, one-shot or " + chatFlagNoEditor + "), so nothing here can carry a parked prompt into a turn. The prompt was NOT queued."
)

// chatQueueRefusalPrefix opens every `/queue` refusal, so the operator reads WHICH
// command refused before reading why (Rule 1: the verb is the constant, never
// retyped inside the sentence).
const chatQueueRefusalPrefix = "`/" + chatQueueCommandName + "` refused: "

// chatQueueNeedsPromptRefusal is the refusal for a `/queue` argument that is only
// whitespace. An empty prompt accepted here would be eaten by the loop's own
// blank-line skip — exactly the silent drop this type exists to make impossible.
const chatQueueNeedsPromptRefusal = chatQueueRefusalPrefix + "it needs a prompt. Pass text to park, or a bare `/" + chatQueueCommandName + "` to list what is parked."

// chatQueueUsage is the REPL's own usage block for the word: what `/help queue`
// prints and what the REPL's vocabulary answers with. It is the ONE definition of
// this text (chatReplSlashVocabulary returns it), because the shared command table
// is not the owner of a REPL-native word — a second copy is exactly the drift the
// vocabulary test exists to prevent.
//
// It is a var rather than a const for one reason: the paragraph states the BOUND,
// and the bound is interpolated from chatQueueBound (fmt.Sprintf is not a constant
// expression) so the help text can never disagree with the number the refusal
// enforces. The VALUE still lives in one place — this renders it.
var chatQueueUsage = `Usage: /queue [<prompt> | list | clear]

/queue <prompt>   park a prompt WITHOUT interrupting the turn in flight. When
                  that turn ends the parked prompt becomes the next turn's input
                  automatically — no extra Enter, no retyping.
/queue            list what is parked, in order (same as /queue list).
/queue clear      empty the queue.

The queue holds at most ` + fmt.Sprintf("%d", chatQueueBound) + ` prompts; an add past that is refused, naming the
bound — a queued prompt is never silently dropped. /quit states what happens to
anything still parked when the session ends.
`

// chatQueueAddedRefusal is the over-bound refusal. It is a FUNCTION so the
// sentence always carries the REAL bound rather than a number typed into a string
// (Rule 1): a refusal that does not name the bound it enforced is a refusal the
// reader cannot act on (Rule 15).
func chatQueueAddedRefusal(bound int) string {
	return fmt.Sprintf("%sthe queue is FULL (%d parked, and that is the maximum). Nothing was queued and nothing was dropped — let a turn end so one runs, or `/%s %s` to empty it.",
		chatQueueRefusalPrefix, bound, chatQueueCommandName, chatQueueClearWord)
}

// chatQueue is the REPL-owned bounded FIFO of prompts waiting to become turns.
//
// IT IS SHARED BY TWO GOROUTINES, and that is the whole reason it carries a
// mutex. The REPL loop is BLOCKED inside the turn it is running, so a `/queue
// <prompt>` typed while the model answers reaches the READER goroutine first —
// and the reader is where the prompt is parked (parkQueueReaderLine). The loop
// then drains the queue at the turn boundary. Two goroutines, one small state:
// items, notes and parked, each touched from one side and read from the other.
//
// The mutex is held for the duration of one method and never across a callout:
// every method here is a few slice operations and a Sprintf, and no method
// invokes anything outside this type (a lock held across a console write would
// put the reader on the writer's critical path, which is exactly the coupling
// this design avoids — the reader never writes to the console).
type chatQueue struct {
	mu sync.Mutex

	// items is the FIFO, head first: items[0] is the next prompt to become a
	// turn. A slice rather than a linked list because the queue is bounded at
	// chatQueueBound and read whole by the list rendering.
	items []string

	// notes holds console lines RECORDED BY THE READER GOROUTINE, waiting for the
	// loop to write them. It exists because the console has exactly ONE writer
	// under the reserved layout (the editor's read loop — lineedit/region.go:
	// "the read loop stays the only writer"), while a `/queue` typed mid-turn is
	// seen by the reader, not the loop. The reader therefore RECORDS what it did
	// and the loop REPLAYS it at the turn boundary, so a park that happened while
	// the operator was watching still gets its note row in the right order —
	// never a second writer, never a silent mutation (which is the one thing this
	// feature must not do).
	notes []string

	// parked remembers the `/queue <prompt>` lines the READER parked, so the LOOP
	// can tell a line it has already accounted for from a turn it still owes the
	// operator: the parked line is consumed by the queue at the turn boundary, so
	// running it as an ordinary turn as well would ask the model the same question
	// twice. It lives here, under the same lock, because it is the same
	// two-goroutine conversation (record from the reader, consume from the loop).
	parked []string
}

// Note records one console line for the loop to write. The reader goroutine calls
// it (it may not write to the console itself — one writer, see the header), and
// the loop drains it at the turn boundary.
func (q *chatQueue) Note(text string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.notes = append(q.notes, text)
}

// DrainNotes returns and clears the recorded console lines, in the order the
// reader recorded them. The loop calls it at the turn boundary, BEFORE it takes
// the prompt, so the operator reads "queued #N of M" and then "→ (queued) …"
// rather than the pop alone.
func (q *chatQueue) DrainNotes() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.notes) == 0 {
		return nil
	}
	out := q.notes
	q.notes = nil
	return out
}

// recordParked notes that the reader parked this line, for the loop's consume.
func (q *chatQueue) recordParked(line string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.parked = append(q.parked, strings.TrimSpace(line))
}

// consumeParked reports whether line was parked by the reader, removing that
// record. A record is consumed at most once, so a parked prompt can never account
// for a SECOND line the operator types later with the same text.
func (q *chatQueue) consumeParked(line string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, parked := range q.parked {
		if parked != line {
			continue
		}
		q.parked = append(append([]string{}, q.parked[:i]...), q.parked[i+1:]...)
		return true
	}
	return false
}

// Add parks a prompt at the tail. It returns the 1-based position on success; a
// refusal sentence and false when the prompt is empty or the queue is full — the
// CALLER renders it, because only the caller owns the console. The text is
// trimmed: two prompts differing only by surrounding whitespace are one prompt,
// and this is a queue of PROMPTS, not of terminal lines.
func (q *chatQueue) Add(prompt string) (int, bool, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	text := strings.TrimSpace(prompt)
	if text == "" {
		return 0, false, chatQueueNeedsPromptRefusal
	}
	if len(q.items) >= chatQueueBound {
		return 0, false, chatQueueAddedRefusal(chatQueueBound)
	}
	q.items = append(q.items, text)
	return len(q.items), true, ""
}

// Len is the current depth. It is what the closeout and the list header report, so
// the count has ONE source — a second counter in the REPL is a number nobody
// spends.
func (q *chatQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Items returns a COPY of the queued prompts in order: the caller renders them and
// must not be able to mutate the queue by doing so.
func (q *chatQueue) Items() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string{}, q.items...)
}

// Clear empties the queue and reports how many prompts it dropped, so the note can
// carry the real number (0 is a valid answer and gets its own wording).
func (q *chatQueue) Clear() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	q.items = nil
	return n
}

// Take removes and returns the head — the prompt that becomes the next turn —
// reporting false when the queue is empty. It is the ONLY place an item leaves the
// queue other than Clear, so "a parked prompt either runs or is explicitly
// cleared" is a property of the type rather than a promise made in the loop.
func (q *chatQueue) Take() (string, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return "", false
	}
	head := q.items[0]
	// Reslice into a fresh slice rather than shift in place: the consumed entry
	// must not stay reachable through the backing array (a shift keeps it alive
	// and lets a later append write over a string nobody reads).
	q.items = append([]string{}, q.items[1:]...)
	return head, true
}

// chatQueueListLines renders the queue as notice lines, ready for
// input.WriteLines. It returns nil for an empty queue; the caller decides whether
// to say so (chatQueueEmptyNote), because "nothing parked" reads differently at an
// interactive prompt than it does in a captured transcript.
func chatQueueListLines(q *chatQueue) []string {
	if q.Len() == 0 {
		return nil
	}
	out := make([]string, 0, q.Len()+1)
	out = append(out, fmt.Sprintf(chatQueueHeaderNote, q.Len(), chatQueueBound))
	for i, item := range q.Items() {
		out = append(out, fmt.Sprintf(chatQueueItemNote, i+1, item))
	}
	return out
}

// chatQueueBusyLine answers a `/queue` line that arrives while a turn owns the
// REPL loop, and reports whether it was one (so the loop does not ALSO see it as
// a turn's input).
//
// WHY THIS EXISTS AT ALL, and it is the whole reason the feature works. The loop
// is BLOCKED inside the turn it is running: its `select` on lineCh is not
// executing, so a `/queue` typed while a turn is in flight is not handled until
// that turn ends — which is exactly when it is useless. The READER goroutine,
// however, is the one place that sees every line the instant it is typed. So the
// reader parks the prompt (the same chatQueue the loop drains at the boundary),
// and the loop is not involved at all: no interrupt, no second mechanism.
//
// A ONE-LINE ARGUMENT-ONLY SURFACE, deliberately. The reader can reach
// `chatQueue.Add` and nothing else: the list rendering and the clear both write
// to the console, and the console has exactly ONE writer (the editor's read
// loop under the reserved layout — lineedit/region.go: "the read loop stays the
// only writer"). Having the reader write a notice would be a second writer and a
// torn layout, so those two forms are answered by the LOOP instead (it reaches
// them the moment the turn ends), and the one line the reader must itself answer
// is stated plainly rather than refused silently.
// parkQueueReaderLine is the READER goroutine's half of `/queue`: it parks a
// `/queue <prompt>` the instant it is typed and reports whether it consumed the
// line. The loop's own `/queue` case (cli_chat.go) is the other half, and the
// split is not an accident — it is forced by two facts:
//
//  1. THE LOOP IS BLOCKED INSIDE A TURN. Its `/queue` case runs between turns,
//     so a prompt typed while the model is answering would be parked at exactly
//     the moment parking is useless. The reader is the only place that sees a
//     line the instant it is typed.
//
//  2. THE CONSOLE HAS ONE WRITER. Under the reserved layout the editor's read
//     loop is the only writer (lineedit/region.go); a notice from this goroutine
//     would be a second writer and a torn layout. So the reader RECORDS the note
//     and the loop REPLAYS it at the turn boundary (chatQueue.notes/DrainNotes).
//     Following (2) honestly is what fixes the FORM SPLIT below.
//
// THE FORM SPLIT, stated because it is a real limit rather than an oversight:
// this function answers `/queue <prompt>` ONLY. Bare `/queue`, `/queue list` and
// `/queue clear` all WRITE to the console (a list block, a count, a no-op
// sentence), and a form whose answer is a console write cannot be completed
// here — it is handed to the loop (return false) and answered at the next turn
// boundary, through the writer that owns the screen. The alternative — the
// reader writing it — is the torn layout this file refuses to create.
func parkQueueReaderLine(turnRunning func() bool, sess *chatSession, input *chatInput, line string) bool {
	if !turnRunning() {
		// NO TURN IS RUNNING, so there is nothing to not-interrupt: the loop is
		// sitting at its prompt and will dispatch this line itself (its own
		// `/queue` case), which is also where the list and the clear get their
		// console writes. Parking it here instead would make a `/queue X` typed at
		// an idle prompt become a turn immediately — indistinguishable from having
		// typed X — and would leave the queue's own bound unreachable in the very
		// state where an operator would want to fill it.
		return false
	}
	msg := strings.TrimSpace(line)
	if !strings.HasPrefix(msg, completionSlashPrefix+chatQueueCommandName) {
		return false
	}
	word := strings.ToLower(strings.Fields(msg)[0])
	if word != completionSlashPrefix+chatQueueCommandName {
		return false
	}
	arg := strings.TrimSpace(strings.TrimPrefix(msg, completionSlashPrefix+chatQueueCommandName))
	if arg == "" || strings.EqualFold(arg, chatQueueListWord) || strings.EqualFold(arg, chatQueueClearWord) {
		// Console-writing forms: NOT answered here (see the FORM SPLIT above).
		return false
	}
	if refusal := chatQueueSeamRefusal(input); refusal != "" {
		// No seam, no queue. Handed to the LOOP so it states the refusal through
		// the one writer, rather than this goroutine writing it.
		return false
	}
	pos, ok, _ := sess.queue.Add(arg)
	if !ok {
		// The over-bound refusal is a console sentence, and this goroutine may
		// not write one — so the line goes to the LOOP (return false), which
		// states the refusal through the one writer. A refusal is the case that
		// must never be silent.
		return false
	}
	// Recorded, not written: the loop replays it at the turn boundary through the
	// writer that owns the screen, so the park is visible in the right order.
	sess.queue.Note(fmt.Sprintf(chatQueueAddedNote, pos, sess.queue.Len(), arg))
	sess.queue.recordParked(msg)
	return true
}

// chatQueueSeamRefusal reports the sentence that refuses `/queue` on a run whose
// input layer has no loop-boundary seam, or "" when this run can carry a queued
// turn. It is the ONE predicate the dispatcher and the tests consult, so "which
// runs can queue" has one answer rather than a comment.
//
// A cooked or one-shot source reads exactly ONE line per push from its caller
// (lineedit_wire.go's ReadLine): there is no boundary at which a parked prompt
// could become a turn, so accepting one would be a promise this carrier cannot
// keep — refused, named, never accepted-then-forgotten.
func chatQueueSeamRefusal(input *chatInput) string {
	if input != nil && input.Mode() == chatInputEditor {
		return ""
	}
	return chatQueueNoSeamRefusal
}

// The turn-in-flight predicate is NOT defined here. The REPL already owns one —
// `turnRunning(sess)` (cli_chat.go), which Ctrl+C's per-turn claim and the queue
// reader BOTH consult — and a second flag would be two answers to one question:
// the loop setting one and the interrupt path the other, drifting apart exactly
// when a key and a keystroke race. So the reader takes the predicate as a
// parameter (`parkQueueReaderLine`) and this file defines none.
