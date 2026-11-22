package main

// chat_surface_surface.go — the SURFACE half of the chat-scoped tool.
//
// internal/agent owns the tool's CONTRACT (the def that ships on the chat wire,
// the action vocabulary, the dispatcher, the loop's bound); this process owns the
// SCREEN, because the screen belongs to the process running the REPL. The two
// halves meet at agent.ChatSurface — one interface, installed here with
// SetChatSurface, so the tool can read a grid without knowing what a terminal is.
//
// THREE FACTS THIS FILE KEEPS STRAIGHT (each one is a measured failure mode of a
// self-observation channel, not a preference):
//
//  1. UNARMED IS NOT BLANK. With no sink this run writes no artifact; the
//     observation then says armed=false and stops. A grid that was never
//     written must never be rendered as an empty screen. Since 2026-10-01 this
//     absence is a DECLINED default rather than an unasked-for capability — the
//     dump is armed unless the run passes `--no-dump-screen` (or a falsey
//     GOPI_CHAT_DUMP) — but the reader's rule is unchanged either way: absence
//     of observation is not observation of a blank screen.
//  2. A GRID THAT MAY NOT BE READ AS A SCREEN IS QUALIFIED, NOT QUOTED. An
//     unpainted grid, an all-blank one and a fallback-geometry one are refused
//     with the reader's OWN sentence (uiclient's notices, never a second wording
//     invented here), and the compare refuses on the same predicate.
//  3. THE AUTHORITY IS THE SESSION LOG. The screen shows a throttled SAMPLE of
//     the paint stream (dumpMinInterval); the session JSONL holds the model's
//     text byte-exact. A compare that has only one of them has no answer, so the
//     transcript is read through the reader the authoritative verb uses — never
//     re-derived here.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The two notices the self-driven loop puts on the OPERATOR's console. They are
// the visible half of the bound (devdoc/runaway-loop-20260923.md): the loop
// announces itself when it starts and announces its own termination when it
// fires, so the person watching never has to infer either from silence.
const (
	// chatSelfDrivenTurnNotice is printed when a self-driven turn begins, with
	// the counters of the CURRENT period, so the operator watches the budget
	// rather than discovering it when it is gone.
	chatSelfDrivenTurnNotice = "[gopi] self-driven turn %d/%d of this period (typed by the chat_surface tool) — this loop stops at its bound\n"
	// chatSelfDrivenBoundNotice is printed when the loop is OVER — it reached its
	// bound, or a `stop` dropped a line it had already typed — naming the counters
	// and the way out. A refusal the operator cannot see is a turn that vanished.
	chatSelfDrivenBoundNotice = "[gopi] self-driven loop CLOSED (%d/%d used): no further self-driven turn will be added in this period. The conversation continues normally — submit a line yourself to open a fresh period, or start a new session for a fresh bound.\n"
	// chatSelfDrivenClosingNotice is printed on the LAST self-driven turn, so the
	// operator sees the bound arrive instead of inferring it from a silence.
	chatSelfDrivenClosingNotice = "[gopi] this is the last self-driven turn of this period (bound %d): the loop closes when it ends.\n"
	// chatSelfDrivenRenewalNotice is printed when a line the OPERATOR submitted
	// renews the budget. The renewal is announced for the same reason the bound
	// is: a budget that silently refills is a bound the operator cannot watch,
	// and this one is granted by their own keystroke — so the console says which
	// keystroke did it and how many turns it just granted.
	chatSelfDrivenRenewalNotice = "[gopi] budget RENEWED by your own submitted line: the self-driven turn budget (%d turns) and the chat_surface key budget are open again — they are reset only this way, never by the model itself.\n"
	// chatSelfDrivenRenewalTransientNotice is the OTHER half of the renewal
	// seam, and it exists because two lines can arrive before the turn boundary.
	// The operator's line and the tool's first `type` both land while a typed
	// line is still in hand, so the renewal is DEFERRED to the boundary that
	// runs the operator's line — where it happens anyway, silently, on the way
	// in (the budget is renewed, nothing is denied). Announcing the deferral is
	// what keeps the console from going quiet about a reset it did not perform
	// yet, and the turn that consumes the operator's line announces it.
	chatSelfDrivenRenewalTransientNotice = "[gopi] that line is not a period boundary yet — a self-driven line is still in hand, so the renewal waits for the operator's own line to become a turn.\n"
)

// chatSurfaceLoopNoticeFormat names a self-driven line on its way in, so the
// transcript shows what was typed by the tool rather than presenting the model's
// own words as the user's.
const chatSurfaceLoopNoticeFormat = "[gopi] → (self-driven) %s\n"

// The two things a tools-enabled chat states ONCE about its own eye have MOVED
// OFF THE SCREEN and onto the REQUEST. The armed/unarmed sentence used to be a
// `[gopi] …` notice (in the answer band, then in the startup screen's notices
// section); the operator's ruling of 2026-10-11 is that the screen is for the
// human and the arm state is the MODEL's business, so the fact is now a system
// note on the very request it describes (internal/agent/chat_surface_fact.go —
// the "方案 A" disposition), published from the arm seam below. The startup
// screen keeps the `surface` FACT row (armed / unpublished, chat_panel_notice.go)
// because a fact about this run belongs there beside the session id and the model.
//
// The ANOMALIES do not move: a dump that cannot be ARMED stops the run with the
// reason on stderr (cli_chat.go), and a surface identity that cannot be
// PUBLISHED still takes the historic stream line, because a human has to be able
// to act on those (Rule 15).

// chatSurfaceArmed reports whether this run writes its own screen.
func chatSurfaceArmed(dumper *uiclient.ScreenDumper) bool { return dumper != nil }

// chatSurfaceFactPath names the artifact the REQUEST-side arm note quotes, or ""
// for a run that writes none. It is a function (not dumper.Path() at the call
// site) because an UNARMED run has no artifact to name, and a note that named one
// anyway would be a fact this run cannot support — the same rule the unarmed
// sentence states in words.
func chatSurfaceFactPath(dumper *uiclient.ScreenDumper) string {
	if dumper == nil {
		return ""
	}
	return dumper.Path()
}

// chatSurface is the REPL's implementation of agent.ChatSurface: the artifact
// this run writes its own screen to, plus the line source that can carry a
// self-driven turn.
type chatSurface struct {
	// dumper is this run's screen sink, nil when the run is not armed.
	dumper *uiclient.ScreenDumper
	// input is the REPL's line layer: it is what a self-driven line is checked
	// against (a cooked/one-shot source cannot carry one) and what the transcript
	// notice is written through.
	input *chatInput
	// sessionPath is this run's own session record — the DEFAULT authority for a
	// compare that names none ("which transcript?" has one answer here: the one
	// this run is writing).
	sessionPath string
}

// ScreenGrid reports this run's own painted screen. It reads the LIVE sink when
// this process owns one, and falls back to the artifact ON DISK otherwise — a
// resumed or foreign run's dump is still that run's screen, and refusing to read
// it would make "I cannot see myself" indistinguishable from "there is nothing
// to see".
func (s *chatSurface) ScreenGrid() agent.ChatScreenObservation {
	if s.dumper != nil {
		snap := s.dumper.Snapshot()
		geom, cols, rows := s.dumper.Geometry()
		obs := agent.ChatScreenObservation{
			Armed: true, Found: true,
			Path: s.dumper.Path(),
			// TextScoped, not Text: the grid is what a reader quotes and it is the
			// same rendering `gopi ui_state` prints for the same artifact, but the
			// TOOL RETURN has a budget the artifact does not. A 24-row screen whose
			// turn history had grown to 278 turns arrived as 2,783,126 characters,
			// 99.9% of it scrollback (measured 2026-11-06) — so this path asks for
			// the bounded rendering and the artifact keeps its whole history.
			Grid:     s.dumper.Snapshot().TextScoped(lineedit.DumpScrollbackSeeRows),
			Cols:     cols,
			Rows:     rows,
			Frames:   s.dumper.Frames(),
			Dumps:    s.dumper.Writes(),
			Geometry: string(geom),
			State:    string(uiclient.StateLive),
		}
		obs.Qualification = chatSurfaceQualification(
			s.dumper.Frames() == uiclient.UnpaintedFrames,
			uiclient.ScreenDumpAllBlank(snap),
			geom == uiclient.GeometrySourceFallback,
		)
		if info, err := os.Stat(s.dumper.Path()); err == nil {
			obs.AgeSeconds = time.Since(info.ModTime()).Seconds()
		}
		return obs
	}
	// Not armed by THIS process: ask the profile for a chat dump anyway, through
	// the same discovery the `ui_state` verb uses, so the answer distinguishes
	// "no dump anywhere" from "a dump that is not this run's".
	return chatSurfaceObserveFromProfile(s.sessionPath)
}

// chatSurfaceObserveFromProfile is the unarmed/foreign path: it scans the
// profile's own roots (uiclient.Candidates, the same function `ui_state` calls)
// for a chat surface's dump and reads it.
//
// WHOSE DUMP IT READS IS THE WHOLE POINT. The rule it shipped with was
// uiclient.SelectLive — "the one live chat surface, else refuse" — which has no
// ground to prefer one of several and therefore REFUSES on the ordinary case (a
// chat run beside the agent's own, both armed, both live). The caller here is not
// in that position: it is the run whose session path it was handed, so it holds
// exactly the fact a scan cannot derive. uiclient.SelectBySelf takes that claim
// and matches it against artifacts that already proved their own identity, and
// the fallback is the general rule ONLY when there is nothing to claim with
// (sessionPath == "").
//
// SessionPath is the OPERAND THAT ALREADY EXISTS and crosses no boundary: the
// comparison is between the directory an artifact resolves to
// (uiclient.SessionDirOfDump) and the directory of THIS RUN's own session record
// — the string the caller already holds. Nothing is published, read back or
// inferred; a caller whose session path is unresolvable (the scratch path above)
// degrades to the general rule, which is where it was before.
func chatSurfaceObserveFromProfile(sessionPath string) agent.ChatScreenObservation {
	roots, err := uiStateRoots()
	if err != nil {
		return agent.ChatScreenObservation{Qualification: fmt.Sprintf("cannot resolve this profile's dump roots: %v", err)}
	}
	cands, truncated, err := uiclient.Candidates(roots, time.Now())
	if err != nil {
		return agent.ChatScreenObservation{Qualification: fmt.Sprintf("cannot scan this profile for a screen artifact: %v", err)}
	}
	cands = uiclient.FilterSurface(cands, uiclient.SurfaceChat)
	obs := agent.ChatScreenObservation{Armed: true}
	if truncated {
		obs.Qualification = "the scan hit its own limit: the artifacts below are the ones it read, not necessarily the only ones on this profile"
	}
	if len(cands) == 0 {
		// No dump at all: the run that asks this question did not arm a dump and
		// none is on the profile either. Reported as UNARMED, which is what it is
		// for this run — "there is no observer here", never "the screen is blank".
		return agent.ChatScreenObservation{Armed: false}
	}
	sel, selErr := chatSurfaceClaimOwn(cands, sessionPath, truncated)
	if selErr != nil {
		// An ambiguity (several live chat surfaces, and this run's own claim did
		// not settle which) is an ANSWER: name it rather than pick one, because
		// reading the wrong surface's screen is worse than reading none.
		return agent.ChatScreenObservation{Armed: true, Qualification: selErr.Error()}
	}
	cand := sel.Candidate
	obs = agent.ChatScreenObservation{
		Armed: true, Found: true, Path: cand.Path,
		// TextScoped for the same reason as the live arm above: this is a TOOL
		// RETURN, and a foreign run's artifact may hold an entire session's
		// scrollback behind a 24-row grid.
		Grid:       cand.Dump.TextScoped(lineedit.DumpScrollbackSeeRows),
		Cols:       cand.Dump.Cols,
		Rows:       cand.Dump.Rows,
		Frames:     cand.Frames,
		Dumps:      cand.Dumps,
		Geometry:   string(uiclient.GeometryOf(cand)),
		State:      string(cand.State),
		AgeSeconds: time.Since(cand.ModTime).Seconds(),
		Qualification: chatSurfaceQualification(
			cand.Unpainted(),
			cand.BlankUnpainted(),
			uiclient.GeometryOf(cand) == uiclient.GeometrySourceFallback,
		),
	}
	return obs
}

// chatSurfaceClaimOwn picks THIS run's own artifact out of a chat-dump scan.
//
// The claim is built from the run's own session path (the operand the surface
// already holds) and handed to uiclient.SelectBySelf, whose contract is the one
// that matters here: a claim that matches NOTHING never resolves to a nearest
// candidate — with several artifacts on hand it REFUSES (the sentence names the
// claim and the pids), and only when the scan found exactly ONE artifact does it
// answer with it, because then there is nothing to confuse it with.
//
// A run with no session path (a scratch-directory dump, a --server-shape run)
// has no claim to make and gets the general rule unchanged — uiclient.SelectLive
// — which is exactly the behaviour this path had before, so the deterministic
// arm is an addition and never a regression.
func chatSurfaceClaimOwn(cands []uiclient.Candidate, sessionPath string, truncated bool) (uiclient.Selection, error) {
	claim := uiclient.SelfIdentity{}
	if dir := sessionlayout.RecordContainer(strings.TrimSpace(sessionPath)); strings.TrimSpace(sessionPath) != "" && dir != "." {
		claim.PID = os.Getpid()
		claim.SessionDir = dir
	}
	if claim.Empty() {
		return uiclient.SelectLive(cands, truncated)
	}
	sel, available, err := uiclient.SelectBySelf(cands, claim, truncated)
	if !available {
		// Unreachable with a non-empty claim (SelectBySelf reports "not applied"
		// only for an empty one); the general rule is the safe answer if it ever
		// is reached, rather than a second reading of what "unavailable" meant.
		return uiclient.SelectLive(cands, truncated)
	}
	return sel, err
}

// chatSurfaceQualification returns the reader's OWN sentence for a grid that may
// not be read as a screen, or "" when the grid is readable. The three sentences
// come from uiclient (the package that measures the facts) rather than being
// re-spelled here: a second wording of "this is not the screen" is how the two
// sides start disagreeing about which grids are readable. The order is the
// reader's order, and it is checked by the same predicate
// (uiclient.CompareGridUnusable) the compare refuses on.
func chatSurfaceQualification(unpainted, blank, fallback bool) string {
	switch {
	case unpainted:
		return uiclient.UnpaintedNotice
	case blank:
		return uiclient.BlankUnpaintedNotice
	case fallback:
		return uiclient.GeometryFallbackNotice
	default:
		return ""
	}
}

// Transcript resolves the authoritative transcript: the operand when one was
// named, this run's own session otherwise. The id/path lookup lives in
// gopilog — the package that owns the session tree — so this tool is not a
// second implementation of session discovery, and every failure is a sentence
// (Rule 15: a lookup that answers "not found" with a nil error is
// indistinguishable from a missing session).
func (s *chatSurface) Transcript(session string) (string, []string, error) {
	path := session
	if path == "" {
		path = s.sessionPath
		if path == "" {
			// No handle and no operand: the artifact's own directory names the
			// run that wrote it (the dump lives beside session.jsonl).
			if s.dumper != nil {
				if sibling, ok := chatSurfaceSiblingSession(s.dumper.Path()); ok {
					path = sibling
				}
			}
		}
		if path == "" {
			return "", nil, fmt.Errorf("%s", chatSurfaceNoTranscript)
		}
	}
	resolved, err := chatSurfaceResolveSession(path)
	if err != nil {
		return "", nil, err
	}
	lines, err := chatSurfaceTranscriptLines(resolved)
	if err != nil {
		return resolved, nil, err
	}
	return resolved, lines, nil
}

// Enqueue is the line source's acceptance test and the typing itself. It refuses
// when no editor owns the screen, because then there is no next prompt for a
// pending line to become a turn at — a promise the carrier cannot keep.
func (s *chatSurface) Enqueue(text string, bound int) (bool, int, string) {
	if s.input == nil || s.input.Mode() != chatInputEditor {
		return false, bound, "this run has no next-turn seam: the input line editor is not active (a piped, one-shot or --no-editor run reads its lines from the caller). The tool can still SEE this surface, but nothing can be typed into it."
	}
	// The line is announced where the operator reads, BEFORE it becomes a turn:
	// the transcript must not present the model's own words as the user's.
	s.input.WriteNotice(fmt.Sprintf(chatSurfaceLoopNoticeFormat, text))
	return true, bound, ""
}

// InjectKey is the line editor's own injection seam, reached from the tool's
// action=key. It translates the tool's report into the FACTS the tool renders,
// and it refuses — with a sentence — when no editor owns the screen: a cooked,
// piped or one-shot run has no line editor to inject into, and reporting
// "injected" there would be a promise nothing keeps (the same rule Enqueue
// follows).
func (s *chatSurface) InjectKey(key string, bound int) agent.ChatKeyResult {
	refuse := func(why string) agent.ChatKeyResult {
		return agent.ChatKeyResult{Label: key, Bound: bound, Why: why}
	}
	if s.input == nil || s.input.Mode() != chatInputEditor {
		return refuse("this run has no line editor: a piped, one-shot or --no-editor run reads its lines from the caller, so there is no input path to inject a key into. `see`/`compare` still work.")
	}
	if s.input.injector == nil {
		return refuse("this run's line editor has no injection seam installed — the editor was built over a source that cannot be wrapped (this is a wiring defect, not a caller error).")
	}
	// THE BOUND GOES THROUGH, and dropping it is exactly the defect this line
	// fixes (measured: a live run asked for `max_keys: 4` and was charged against
	// 64 — the tool's clamp and the injector's own charge were both correct, and
	// the number never reached the injector because the surface called the
	// unbounded spelling). The unit cases could not see it: they drive the agent's
	// dispatcher, and the stub surface they install takes the bound. Only a real
	// `gopi chat --tools` run showed `keys_used=1/64` after `max_keys: 4`.
	res := s.input.injector.InjectKeyBounded(key, bound)
	return agent.ChatKeyResult{
		Label: res.Name, Accepted: res.Accepted, ReadByEditor: res.ReadByEditor,
		Pending: res.Pending, Used: res.Used, Bound: res.Bound, Last: res.Last, Why: res.Why,
	}
}

// KeyUsed reports how many keys the CURRENT PERIOD has injected, and the ceiling
// in force. It forwards the INJECTOR's own counters — the object that is charged
// on acceptance and the one that refuses — rather than keeping a second count
// here, because one number with two owners is how a report and a refusal drift
// apart.
func (s *chatSurface) KeyUsed() (int, int) {
	if s.input == nil || s.input.injector == nil {
		return 0, 0
	}
	return s.input.injector.Used(), s.input.injector.Bound()
}

// ResetKeys opens a FRESH key period on this run's line editor. It is the action
// half of agent.ChatSurface.ResetKeys, and it is called ONLY from the carrier's
// own line arm (see the renewal seam in cli_chat.go) — never from a tool action,
// which is the whole of the guarantee.
//
// A run with no injector (a cooked/one-shot reader, or `--no-editor`) is a NO-OP,
// not an error: there is no key budget to renew because no key could ever have
// been spent, and inventing a failure there would turn "this run cannot inject"
// into a spurious console error on every operator line.
func (s *chatSurface) ResetKeys(bound int) {
	if s.input == nil || s.input.injector == nil {
		return
	}
	s.input.injector.ResetKeys(bound)
}

// CancelLoop cancels the self-driven loop. It reports whether a running loop was
// cancelled; the pending line is dropped by the caller-side seam (the Agent owns
// it, and one owner is the whole point).
//
// HONESTY ABOUT WHAT IT CANNOT DO: a turn already in flight cannot be
// interrupted from inside itself — this tool runs INSIDE that turn. What `stop`
// guarantees is that no FURTHER self-driven turn is added; the operator's own
// Ctrl+C remains the way to end the running one (and it ends the session).
func (s *chatSurface) CancelLoop() bool { return false }

// chatSurfaceNoTranscript is the sentence a compare with no authority at all
// gets: it names what is missing and the operand that supplies it.
const chatSurfaceNoTranscript = "no authoritative transcript is available: this run has no session log (or the artifact's directory holds no session record) — pass session=<session-id|*.jsonl>."

// chatSurfaceResolveSession resolves an operand (id, path, or the value the
// `--session` flag would take) to a session record path.
func chatSurfaceResolveSession(operand string) (string, error) {
	if _, err := os.Stat(operand); err == nil {
		return operand, nil
	}
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		// The tree itself could not be resolved: said with its own reason, never
		// flattened to "no such session" (Rule 15).
		return "", fmt.Errorf("cannot resolve the session %q: %w", operand, err)
	}
	found, err := gopilog.FindSession(dir, operand)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the session %q: %w", operand, err)
	}
	if strings.TrimSpace(found) == "" {
		return "", fmt.Errorf("no session matches %q (a session id is the full uuid; `gopi session log` lists this profile's sessions)", operand)
	}
	return found, nil
}

// chatSurfaceTranscriptLines reads a record's assistant text blocks, one entry
// per line, AS THE LOG HOLDS THEM — the bytes the authoritative reader prints,
// so the compare's two sides are the same two things the `ui_state --compare`
// check puts against each other.
func chatSurfaceTranscriptLines(path string) ([]string, error) {
	sess, err := gopilog.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the transcript %s: %w", path, err)
	}
	var lines []string
	for _, iter := range sess.Iterations {
		for _, block := range iter.TextBlocks {
			if strings.TrimSpace(block) == "" {
				continue
			}
			for _, l := range strings.Split(block, "\n") {
				lines = append(lines, l)
			}
		}
	}
	return lines, nil
}

// chatSurfaceSiblingSession names the session record that lives beside a screen
// artifact: the dump's directory IS the session directory, so the artifact names
// its own authority.
func chatSurfaceSiblingSession(dumpPath string) (string, bool) {
	dir, ok := uiclient.SessionDirOfDump(dumpPath)
	if !ok {
		return "", false
	}
	rec, ok := sessionlayout.RecordInDir(dir)
	if !ok {
		return "", false
	}
	return rec.Path, true
}

// installChatSurface wires the chat-scoped tool to THIS run's surface and returns
// the function that unwires it, plus the identity value published to this run's
// children ("" when publishing failed — the caller states that as a FACT on the
// startup screen and in the flush record, Rule 15).
//
// It is called once per chat session, after the dump has been resolved and the
// input layer built (the surface needs both), and the unwire matters: a process
// that hands the agent a stale terminal would let a later turn read a screen
// nobody is painting any more.
//
// IT PRINTS NOTHING. It used to announce the publication here, through
// WriteNotice — i.e. into the answer band, where the operator read it as
// transcript (chat_panel_notice.go carries the report and the fix). The fact now
// rides the returned value: the caller puts it on the startup screen's side
// panel, which is the one carrier already built for this run's facts.
func installChatSurface(ag *agent.Agent, input *chatInput, dumper *uiclient.ScreenDumper, st termToolStyles, noEventSlotWord string) (func(), string) {
	if ag == nil {
		return func() {}, ""
	}
	s := &chatSurface{dumper: dumper, input: input}
	if ag.SessionLog != nil {
		s.sessionPath = ag.SessionLog.Path()
	}
	// PUBLISH THE ARM FACT TO THE REQUEST (chat_surface_fact.go). This is the same
	// seam and for the same reason as the identity publication below: it is the one
	// place where both halves of the fact exist (the resolved sink, and the run's
	// own session directory). The fact travels to the model on the system prompt of
	// the request it describes; it does NOT print (the operator's ruling of
	// 2026-10-11: the screen is for the human).
	ag.SetChatSurfaceFact(chatSurfaceArmed(dumper), chatSurfaceFactPath(dumper), true)
	// PUBLISH THIS RUN'S IDENTITY TO ITS CHILDREN (chat_surface_identity.go).
	// This is the run's ARM POINT — the one seam where both halves exist
	// (this process's pid and the session directory this run writes into) — and
	// it is the seam the `bash` tool's children inherit from, which is how a
	// `gopi ui_state --target chat` this run spawns claims THIS run's dump
	// instead of guessing among the profile's dumps. An empty session path is
	// still published with its pid half: see chatPublishSurfaceIdentity.
	ag.SetChatSurface(s)
	// REGISTER THIS RUN IN THE SLOT REGISTRY (chat_session_slots.go), from the
	// SAME seam and for the same reason as the environment publication above:
	// this is the one place where both halves of this run's identity exist
	// (its pid and the session directory it is writing into). The environment
	// channel reaches this run's CHILDREN; the registry reaches the SIDEWAYS
	// readers — `notify.cmd` is exec'd by the ledger's own machinery, which is
	// nobody's descendant. A failure is reported on stderr and is never fatal
	// (the chat runs fine without notifications).
	//
	// st is the run's system voice, handed down from the ONE capability
	// resolution in runChat: this seam registers a slot during start-up, i.e.
	// before any editor exists, so it cannot read a table off the input — and it
	// must not resolve one of its own (a second capability decision).
	//
	// noEventSlotWord is the operator's OFF SWITCH for that registration
	// (`--no-notify` / `--no-events`, or "" for the default). It rides down from
	// the parser through this seam because the ARM POINT is where the write
	// happens: the decision has ONE reader, and it is the call that performs the
	// effect (Rule 15 — a switch whose only consumer is a comment is a switch
	// that does nothing).
	chatSessionSlotRegisterUnless(s.sessionPath, st, noEventSlotWord)
	if value := chatPublishSurfaceIdentity(s.sessionPath); value != "" {
		return func() { ag.SetChatSurface(nil) }, value
	}
	return func() { ag.SetChatSurface(nil) }, ""
}
