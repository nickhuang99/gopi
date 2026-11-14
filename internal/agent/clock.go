package agent

import (
	"fmt"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// SessionClock is a cheap monotonic session clock (CLOCK-TICK design, the operator
// 定案): t0 is recorded when the session starts, and every tool result
// carries an elapsed tick label like [t+47s]. The LLM is bad at computing
// differences between wall-clock timestamps ("bad at math"), so instead of
// timestamps we hand it a natural-number counter that increments with wall
// time — relative speed (fast round vs stall) is directly perceptible from
// the tick alone. This is the PRIMARY time signal; walltime stays optional.
type SessionClock struct {
	t0 time.Time

	// lastRequest is WHEN the previous request was assembled (the memory
	// field of the per-request turn stamp, TurnStamp). It is in-memory only —
	// no file, no timer, no goroutine (Rule 35) — and it is zero until the
	// first request of the session, which is what makes "is this the first
	// request" answerable rather than guessed.
	//
	// PRODUCTION BOUNDARY: one `gopi chat` process is one run-length session
	// (runChatWithInput reuses ONE agent + ONE clock for every turn, and chat
	// --once is one turn). "Previous request" therefore means "the previous
	// request of this process", NOT "the previous request of an addressed
	// session id": `gopi chat --resume` seeds the conversation from a raw log
	// but builds a FRESH clock, so a resumed session's first request reports
	// First — deliberately, because the pre-resume interval was never measured
	// by this process and reporting it would be an invented number rather than
	// a missing one (Rule 15). Single-threaded loop access only, exactly like
	// the t0 field beside it.
	lastRequest time.Time
}

// NewSessionClock starts a session clock at the current moment.
func NewSessionClock() *SessionClock {
	return &SessionClock{t0: time.Now()}
}

// Elapsed returns the time elapsed since session start (t0). A nil or
// zero-valued clock reports 0 — tests building Agent{} literals leave the
// Clock field nil and must observe no elapsed time.
func (c *SessionClock) Elapsed() time.Duration {
	if c == nil || c.t0.IsZero() {
		return 0
	}
	return time.Since(c.t0)
}

// The TIER CEILINGS — the one place the unit boundaries are decided (Rule 1:
// a threshold is a named constant, never a literal at the comparison). Each
// ceiling is exclusive: a duration BELOW it renders in the coarser unit named
// by the constant it sits under, and the last band has no ceiling (days).
//
// WHY TIERS AT ALL. The label used to be raw seconds ([t+1840s]), which
// reproduces the exact defect the tick replaced: the model still has to divide
// by 60 to learn "about half an hour". A person reads "31m" directly and
// "1840s" not at all, and a model is no better at the division than a person
// is — so past a minute the number is folded into the unit it is already
// thinking in.
const (
	tierSecondCeiling = time.Minute             // < 1m  → "47s"  (exact: sub-minute precision is the point)
	tierMinuteCeiling = time.Hour               // < 1h  → "31m"
	tierHourCeiling   = hoursPerDay * time.Hour // < 1d → "2h"; else "3d"
	hoursPerDay       = 24
)

// durationTier folds a duration into the coarsest unit a reader can hold
// without arithmetic, and reports whether the value is EXACT.
//
// ONE function for both halves of the time signal (the tick's position and the
// per-request gap), because they are one measurement in two places: a tick that
// said "[t+~31m]" beside a gap that said "+1847s" would be two vocabularies for
// one clock, and a reader would have to learn both (Rule 1/13/14).
//
// APPROX is true above the second tier, and it is carried out of this function
// rather than re-derived by each caller: "31m" from 1840s is a ROUNDED reading,
// and saying so with a "~" is what stops the model treating two adjacent ticks
// as a precise subtraction. Below a minute the reading is exact — that is where
// relative speed (a fast round vs a stall) is actually perceptible, and rounding
// there would erase the only fine-grained signal the clock has.
func durationTier(d time.Duration) (label string, approx bool) {
	if d < 0 {
		d = 0
	}
	switch {
	case d < tierSecondCeiling:
		return fmt.Sprintf("%ds", int(d.Seconds())), false
	case d < tierMinuteCeiling:
		return fmt.Sprintf("%dm", int(d.Minutes())), true
	case d < tierHourCeiling:
		return fmt.Sprintf("%dh", int(d.Hours())), true
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/hoursPerDay), true
	}
}

// TickLabel formats the elapsed time as the position marker every tool result
// carries, e.g. [t+47s] / [t+~31m] / [t+~2h] / [t+~3d] — elapsed session time
// in the coarsest unit that needs no arithmetic. The "~" appears only above a
// minute, where the value is rounded rather than measured (durationTier says
// which is which).
func (c *SessionClock) TickLabel() string {
	return tickLabelOf(c.Elapsed())
}

// tickLabelOf renders a duration AS a tick label. It is the single spelling
// site: TickLabel (the live clock), the prompt's decision threshold and any test
// asking "what does the clock paint" all come through here, so a change to the
// tiering moves every one of them together instead of leaving one behind
// (Rule 1/13/14).
func tickLabelOf(d time.Duration) string {
	label, approx := durationTier(d)
	if approx {
		return fmt.Sprintf(tickApproxFmt, label)
	}
	return fmt.Sprintf(tickExactFmt, label)
}

// withTick prefixes a tool result with the session clock's elapsed tick
// (the tiered tick label). Prefixing — rather than restructuring into a meta field —
// keeps the tool-result JSON contract intact: the original content is
// untouched and the marker sits OUTSIDE the payload, so any LLM-side parser
// that reads a JSON result body sees exactly the body it always saw.
// Agents built as literals (tests) have a nil Clock — the result passes
// through unchanged.
func (a *Agent) withTick(result string) string {
	if a == nil || a.Clock == nil {
		return result
	}
	return a.Clock.TickLabel() + " " + result
}

// tickShapeLabel is the tick's spelling as a READER sees it in prose, while
// TickLabel produces the real value ("[t+47s]", "[t+~31m]"). One constant
// because the prompt sentence and any doc that teaches the marker must agree
// with the format strings above — a second literal is how two different shapes
// come to describe the same counter (Rule 1/13). The shape now names the TIERS
// rather than a bare second count: teaching the seconds-only spelling would
// teach a spelling nothing paints any more.
const tickShapeLabel = "[t+Ns]/[t+~Nm]/[t+~Nh]/[t+~Nd]"

// The two tick spellings durationTier folds into: exact below a minute, folded
// (and marked "~") above it. Named so TickLabel and any test that reads a tick
// agree on the punctuation, and so the "~" is a value rather than a character
// typed at the render site (Rule 1).
const (
	tickExactFmt  = "[t+%s]"
	tickApproxFmt = "[t+~%s]"
)

// tickDecisionAfter is the elapsed time past which a single call is the wrong
// tool (the prompt's one attached DECISION — "sense time" with no threshold was
// the measured complaint). It is a DURATION, and tickDecisionLabel below is its
// spelling, derived from it: the prompt used to hard-code the string "t+300s"
// beside a tick that no longer counts in bare seconds, so the threshold was the
// one place the clock's own vocabulary had a second, hand-written copy
// (Rule 1/13/14 — the change that folded ticks into minutes would have left it
// saying "t+300s" about a clock that never paints that).
const tickDecisionAfter = 5 * time.Minute

// tickDecisionLabel is tickDecisionAfter written the way the clock writes
// (durationTier + the tick spelling), e.g. "[t+~5m]".
var tickDecisionLabel = tickLabelOf(tickDecisionAfter)

// TurnStamp is the PER-REQUEST time signal — the tick a REQUEST carries, plus
// how long passed since the previous one.
//
// WHY IT EXISTS (the measured gap). The tick only ever rode a TOOL RESULT
// (withTick above), so a pure conversation turn — and every pause between two
// operator messages — carried no time signal at all. The model therefore
// carried its last round's time assumption forward: it said "I just ran the
// tests" after an hour of the operator being away, because nothing in the
// request contradicted it. The tick says WHERE in the session we are; the gap
// says how much unobserved time is behind that position, which is the half a
// conversation actually needs.
//
// First marks the session's first request: there is no previous request to
// measure a gap from, and reporting "+0s" there would be a fabricated
// measurement rather than an absent one (Rule 15).
//
// At is the ABSOLUTE wallclock the stamp was minted at, WITH its zone offset —
// and it is the one field here that is not a relation. The tick and the gap are
// both measured FROM something inside this process (t0, lastRequest), so both
// are destroyed by a restart: `gopi chat --resume` builds a FRESH clock (see the
// lastRequest comment above), which is correct for a gap and wrong for "when
// did this request happen". A resumed session therefore read "[t+0s] — first
// request of this session" for a moment that may be hours after the log it
// resumed from. The wallclock is not derived from t0, so it cannot be zeroed by
// a new process: it says the same thing in a fresh run, a resumed run, a run
// whose log is read tomorrow, and a run another host is auditing.
//
// A zero At means the moment could not be stated with its zone (see
// turnStampWallclockLayout's comment): the renderer then says so in words
// instead of printing a time in a zone the reader would have to assume (Rule 15).
type TurnStamp struct {
	Tick      string        // the tick label, from TickLabel — never retyped
	SinceLast time.Duration // elapsed since the previous request; 0 when First
	First     bool          // no earlier request in this session
	At        time.Time     // absolute wallclock of this request, zone offset included
}

// TurnStamp records THIS request's moment and returns the time signal to carry
// with it. A nil or zero-valued clock returns the zero stamp ("no time signal
// available"), which every caller treats as the no-op case — the same
// convention withTick uses for the tool-result half.
//
// It is the ONLY writer of lastRequest: the field is advanced here, at the
// moment the stamp is minted, so two callers (ChatOps and Chat) cannot disagree
// about what "the previous request" means.
func (c *SessionClock) TurnStamp() TurnStamp {
	if c == nil || c.t0.IsZero() {
		return TurnStamp{}
	}
	now := time.Now()
	st := TurnStamp{Tick: c.TickLabel(), First: c.lastRequest.IsZero(), At: now}
	if !st.First {
		st.SinceLast = now.Sub(c.lastRequest)
	}
	c.lastRequest = now
	return st
}

// The two rendered forms of a turn stamp. Named constants (Rule 1) because
// each is a wire string a test and the injection site must agree on; the
// harness prefix leads both, so a stamp is recognised as the framework's own
// words rather than the operator's (isHarnessInjectedUserMessage) — it must not
// render in the console transcript as something the operator typed.
const (
	// turnStampFirstSuffix / turnStampGapSuffix are the two closing clauses,
	// named because a test and the renderer must agree on which arm was printed
	// (a test matching a re-worded suffix would pass against text nothing paints
	// any more — Rule 1/13).
	turnStampFirstSuffix = "first request of this session."
	turnStampGapSuffix   = "since your previous request."

	// turnStampWallclockLayout is the ABSOLUTE time's spelling: ISO-8601 in the
	// stamp's own zone, to the SECOND, with a NUMERIC offset written out
	// ("2026-11-07T04:50:12+08:00"). Spelled here rather than at the Format call
	// (Rule 1), and it is time.RFC3339 with its one shortcut removed: RFC3339
	// prints a bare "Z" for a zero offset, which is a legal ISO-8601 designator
	// but not the offset a reader comparing two hosts' logs can subtract with.
	// A zone ABBREVIATION ("CST") is rejected because it names several offsets
	// in the world, and a bare local time is rejected because a log read on
	// another host (or by a peer gopi) would have to assume a zone — i.e. invent
	// one (Rule 15). The value stays parseable as RFC3339, so a reader does not
	// need a second layout to read it back.
	turnStampWallclockLayout = "2006-01-02T15:04:05-07:00"

	// turnStampWallclockUnknown is what the stamp says when it carries no moment
	// at all (a zero At). It states the ABSENCE and its cause rather than
	// rendering a time in some assumed zone: the caller that minted the stamp
	// never recorded one, and inventing "local" here is exactly the silent
	// fallback Rule 15 forbids. It contains no " — " so the gap clause's
	// separation stays unambiguous.
	turnStampWallclockUnknown = "wallclock-unavailable(no-zone-recorded)"

	turnStampFirstFmt = harnessMessagePrefix + "%s @ %s — " + turnStampFirstSuffix

	// turnStampGapFmt takes the GAP already folded by durationTier (including
	// its unit letter), not a raw second count: the tick and the gap are one
	// measurement rendered by one function (Rule 1/13/14), so "+1847s" can never
	// come back beside a "[t+~31m]" tick.
	//
	// WHY THE GAP CARRIES NO "~" WHILE THE TICK DOES. Above a minute the tick's
	// "~" distinguishes a ROUNDED position from the exact sub-minute reading —
	// it is the warning that two ticks are not a precise subtraction. A gap's
	// whole message is already a rough one ("about five minutes passed"), and a
	// tilde there would mark every gap above a minute and so distinguish
	// nothing; the leading "+" already says "interval", not "position".
	turnStampGapFmt = harnessMessagePrefix + "%s @ %s — +%s " + turnStampGapSuffix
)

// The ABSOLUTE half's position in the line, stated once: right after the tick,
// BEFORE the em dash, so both arms keep their two measured clauses exactly where
// they were — the gap clause reads the same bytes as before ("+9m since your
// previous request.") and the wallclock is an insertion into the LEFT half only.
// This is what keeps every existing reader (internal/gopilog, internal/replaydrive,
// internal/sessionevent) matching, and what keeps the marker readable as one
// sentence: "[system] [t+~1m] @ 2026-11-07T04:50:12+08:00 — +9m since your
// previous request."

// The stamp's cost is O(1) and bounded, and these are the bounds a test
// measures against (never a literal at the assertion site): ~80 chars, well
// under a sentence, so the signal can never be the reason a context trims.
//
// THE BOUND MOVED WITH THE ABSOLUTE HALF and was re-derived, not nudged: the
// wallclock contributes a FIXED 25 characters (19-digit layout + " @ "), and the
// tick and the gap can each grow one digit per magnitude (a run past a thousand
// days, an interval past two digits), so the worst realistic line is
// "[system] [t+~1234d] @ 2026-11-07T04:50:12+08:00 — +1234d since your previous
// request." at 94 characters (measured by TestTurnStamp_WallclockIsBounded's own
// arithmetic, which formats a worst-case stamp rather than trusting this note).
const (
	turnStampMaxChars   = 96 // characters, either arm, both counters at 4 digits
	turnStampMaxTokens  = 32 // chars/4 estimate, the budget's own heuristic
	turnStampDigitDrift = 2  // two counters of at most +2 digits can move it
)

// Render is the stamp's wire form: one harness-prefixed line, O(1) length
// (~90 characters), with the tick taken verbatim from TickLabel so
// its single definition (Rule 1/13), the absolute wallclock beside it (with its
// zone offset — see turnStampWallclockLayout), and then the arm's own clause.
//
// An empty Tick means no clock (or a zero-valued one): the caller has nothing
// to say about time, so Render says nothing rather than inventing a tick
// (Rule 15 — an absent measurement is reported as absent, never as "+0s").
//
// A non-empty Tick with a ZERO At is the other absence: something asked for a
// stamp but no moment travelled with it, so the absolute half says
// turnStampWallclockUnknown instead of silently formatting a zero time in the
// local zone (which would print "0001-01-01T00:00:00+08:00" — a fabricated
// instant wearing a real offset).
func (s TurnStamp) Render() string {
	if s.Tick == "" {
		return ""
	}
	at := turnStampWallclockUnknown
	if !s.At.IsZero() {
		at = s.At.Format(turnStampWallclockLayout)
	}
	if s.First {
		return fmt.Sprintf(turnStampFirstFmt, s.Tick, at)
	}
	gap, _ := durationTier(s.SinceLast)
	return fmt.Sprintf(turnStampGapFmt, s.Tick, at, gap)
}

// turnStampLine mints THIS request's stamp, records it in the session JSONL,
// and returns its wire text — "" when there is no clock to read.
//
// WHY THE LOGGING IS PART OF THE MINT (the same reason injectJobNotice states
// for its own injection): the stamp is the only record of when a request was
// assembled, so a reader auditing "did the model see the gap" can answer it
// only from the session record. One function rather than a copy at each
// request site, so the two chat paths cannot disagree about what a request
// carries.
//
// THE BUDGET PROGRESS TAG rides the SAME line (the epic's ask: the 60-minute
// budget must reach EVERY request, not only the commit beat). It is not a
// second rendering of the budget: sessionBudgetLine — the string the commit
// return already carries — is APPENDED here verbatim, so the request stamp and
// the commit report can never drift into two spellings of one measurement
// (Rules 1b/13/14), and the escalation arm's wording (elapsed, remaining, the
// mark, the one available action) arrives with it unchanged once the run is
// past sessionBudgetWarnPercent. Within one message there is one LogUser call,
// so the session record shows the model EXACTLY the line it was sent —
// auditability of "did the request carry the budget" is one grep, not a
// reconstruction.
//
// Degradations, all silent (Rule 15): no clock OR an unlimited budget
// (sessionBudget() <= 0) renders "" from the budget half, leaving the time
// stamp byte-identical to its pre-budget form; no clock at all returns "" from
// this function entirely, which requestMessages reads as "send the plain wire
// messages"; and a plain prompt's FIRST request is exempt by
// turnStampForRequest — the only degradation that withholds a signal a LATER
// request of the SAME run does carry.
func (a *Agent) turnStampLine() string {
	if a == nil || a.Clock == nil {
		return ""
	}
	msg := a.Clock.TurnStamp().Render()
	if msg == "" {
		return ""
	}
	if tag := a.sessionBudgetLine(); tag != "" {
		msg += " " + tag
	}
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), msg)
	}
	return msg
}

// turnStampForRequest is turnStampLine PLUS the one request-level exemption the
// wire shape imposes on it: the FIRST request of a PLAIN PROMPT run carries no
// stamp at all.
//
// WHY THE EXEMPTION IS DECIDED BEFORE THE MINT. Minting a stamp is not a pure
// read — SessionClock.TurnStamp advances lastRequest, the field that makes the
// NEXT request's gap measurable — so the order of the two facts decides which
// number the run gets. Deciding the exemption FIRST (on a.plainPrompt and the
// clock's own First, both readable without minting) means the tick this request
// never carried is genuinely absent, and the run's FIRST MINTED stamp is the
// one its second request really received — the gap it reports is measured from
// a moment the model was actually spoken to. The rejected alternative, minting
// and then discarding the text, would report the second request's gap from a
// stamp nobody received: an invented number rather than a missing one (Rule 15).
//
// WHY ONLY THE FIRST REQUEST, AND ONLY FOR A PLAIN PROMPT. The exemption exists
// because on this surface the stamp has nowhere to sit: requestMessages appends
// it as a user-role message, consecutive user text is MERGED on the wire
// (deepseek/claude_client.go BuildClaudeRequest keeps roles alternating), and on
// a plain prompt the preceding user message IS the operator's text — so the
// stamp would land INSIDE the prompt, and `gopi -h`'s `"<prompt>"  The task
// text` is the whole promise of that surface (RP-1;
// cmd/agent/cli_runpath_boundary_rp1_test.go measures it at the real boundary).
// From the second request on there is an assistant turn in between, so the
// stamp sits in its own user slot where it belongs: the exemption costs the
// prompt's run ONE tick, not its time base — every later request of the SAME
// run takes the normal branch, because the clock's First is false by then.
//
// A chat surface (plainPrompt false) is untouched: its first request keeps the
// stamp its own fixtures pin (cmd/agent/cli_lineedit_test.go's
// stamps == requests, internal/agent/sessionbudget_everyround_test.go).
func (a *Agent) turnStampForRequest() string {
	if a == nil || a.Clock == nil {
		return ""
	}
	if a.plainPrompt && a.Clock.TurnStamp().First {
		return ""
	}
	return a.turnStampLine()
}

// requestMessages returns the message list for ONE request: the conversation
// the wire would send, plus the CURRENT turn stamp appended as the last user
// message.
//
// WHY THE STAMP IS APPENDED HERE RATHER THAN STORED IN Ctx.Messages. It is a
// property of NOW, not of the conversation: a stored stamp would be a second
// copy that goes stale the moment the next request is assembled (and a
// persistent chat would accumulate one per round forever). Appending at the
// request-assembly point gives every request — including a pure conversation
// round with no tool call, which is the whole defect this closes — the current
// tick and the gap since the previous request, while costing exactly ONE extra
// message of O(1) length per request and leaving the conversation itself
// (compaction, checkpoints, the operator's transcript) untouched.
//
// LAST is also the safe position on the wire: a user text message appended at
// the end merges into the preceding block (the "extra text block in a
// tool-result user message" shape claude_client.go documents), so no
// tool_use/tool_result pair can ever be orphaned by it.
//
// A nil Clock (or nil Ctx) returns the plain wire messages: an Agent{} literal
// in a test sends exactly the request it sent before, byte for byte.
//
// THE STAMP IS MINTED BY turnStampForRequest, not by turnStampLine directly:
// that is the one place that knows a plain prompt's FIRST request must reach the
// model with nothing around the operator's text (see its comment — the wire
// merges consecutive user messages, so an appended stamp lands INSIDE the
// prompt). The exemption is decided before the mint, so the run's time base
// survives it.
func (a *Agent) requestMessages(keepThinking bool) []deepseek.Message {
	if a == nil || a.Ctx == nil {
		return nil
	}
	msgs := a.Ctx.WireMessages(keepThinking)
	stamp := a.turnStampForRequest()
	if stamp == "" {
		return msgs
	}
	return append(msgs, deepseek.Message{Role: "user", Content: stamp})
}
