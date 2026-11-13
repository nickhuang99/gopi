package main

// cli_uistate.go — `gopi ui_state`: ONE call to read the current `gopi ui`
// screen state, without pixels and without a vision model.
//
// WHAT IT ADDS OVER `cat <dump>`. The self-observation dump delivered earlier
// (`gopi ui --dump-screen`, internal/uiclient/dump.go) answers "what did the
// surface paint" — as a FILE, whose path an agent must first find, and whose
// currency the file itself does not attest (the sink is throttled, so the mtime
// is not evidence that a surface is still running). This verb closes the loop an
// agent actually needs:
//
//   - DISCOVERY: with no --pid it scans the two places a dump can legally land
//     (the profile's sessions/ tree and the ui-session runtime scratch class)
//     and finds it, instead of making every caller know the layout;
//   - IDENTITY: it re-reads /proc and requires the dump's own (pid, started)
//     stamp to match the live process, so a recycled pid cannot hand back
//     somebody else's screen;
//   - AMBIGUITY IS A REFUSAL: two live surfaces are two screens, and this verb
//     LISTS them and exits non-zero rather than picking one;
//   - AN EXITED TARGET IS DIAGNOSED: a dump whose writer is gone is reported as
//     "the target has exited cleanly; this is the last frame", never as an empty
//     screen and never as current truth;
//   - THE BLIND SPOTS RIDE ALONG: the first sentence of the description says
//     this is an interpreted CELL GRID (not bytes, not pixels), and the 8 things
//     it cannot see are printed in every output — text and JSON alike.
//
// It is a CLI SUBCOMMAND, not a new agent tool: the word `ui_state` is the
// TOOL-shaped spelling (underscore, like screen_capture / vision_analyze), so
// an agent already reaches it through the `bash` tool it uses for every other
// shell verb. Rule 3's 17-tool budget is untouched.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/uiclient"
)

// The verb's flag vocabulary (Rule 1b): the parser, the help page and the tests
// read these rather than re-typing them.
const (
	uiStateFlagPID  = "--pid"
	uiStateFlagJSON = "--json"
	uiStateFlagList = "--list"
	// uiStateFlagTarget is `--target ui|chat`: which self-observing surface the
	// caller means. It exists because BOTH surfaces write a dump into the same
	// session tree, so "the dumps on hand" and "the surface I mean" are two
	// questions — and answering the second with a false ambiguity (two live
	// dumps, one screen wanted) would make the reader unusable the moment the
	// operator runs a chat beside a ui.
	uiStateFlagTarget = "--target"
	// uiStateFlagCompare / uiStateFlagSession are the compare pair; their
	// constants live in cli_uistate_compare.go beside the code that reads them.
)

// uiStateExitCodeAmbiguous is the exit code a refusal uses, rendered into the
// usage page; it is the SAME number runUIStateSubcommand returns (Rule 13: one
// definition, referenced — a page that named a different code than the process
// returns would be worse than no page at all).
const uiStateExitCodeAmbiguous = "2"

// uiStateFlagSep / uiStateFlagPrefix are shared with the other cli_*.go parsers'
// spelling of "--flag=value" and of what marks an argument as a flag.
const (
	uiStateFlagSep    = "="
	uiStateFlagPrefix = "-"
)

// uiStateExitAmbiguous is the exit code a refusal uses, so a script can tell
// "several screens, you must choose" from "no screen at all" (exit 1).
const uiStateExitAmbiguous = 2

// uiStateCompareExitCodeStr is the compare-mismatch exit code as the usage page
// spells it — the SAME number the process returns (uiStateExitCompareMismatch),
// rendered from ONE place so a page can never name a code the verb does not
// return (Rule 13).
const uiStateCompareExitCodeStr = "3"

// uiStateExitOffGridStr is the off-grid exit code as the usage page spells it —
// the SAME number the process returns (uiStateExitOffGrid), rendered from ONE
// place so a page can never name a code the verb does not return (Rule 13).
const uiStateExitOffGridStr = "4"

// uiStateCompareMaxLinesStr is the compare bound as the usage page spells it
// (uiStateCompareMaxLines).
const uiStateCompareMaxLinesStr = "64"

// uiStateAuditExitCouldNotRunStr is the audit's "could not run" code as the usage
// page spells it — the SAME number the process returns
// (uiStateExitCouldNotRun), rendered from one place so a page can never name
// a code the verb does not return (Rule 13).
const uiStateAuditExitCouldNotRunStr = "1"

const uiStateUsageTitleLine = "gopi " + CliVerbUIState + " — read a live `gopi ui` screen state as an interpreted cell grid"

// uiStateNoteRegionSlug names this page's ONE note region. The marks are rendered
// from the shared vocabulary (cli_help_note_region.go) so the page's writer and
// the guard's recognizer cannot spell them differently (Rule 14).
const uiStateNoteRegionSlug = "self-observation"

// uiStateNoteRegionBegin/End are the region's two marks, BUILT rather than typed
// so the slug above is their single source.
const (
	uiStateNoteRegionBegin = helpNoteBeginPrefix + uiStateNoteRegionSlug + helpNoteBeginSuffix
	uiStateNoteRegionEnd   = helpNoteEndPrefix + uiStateNoteRegionSlug + helpNoteEndSuffix
)

const uiStateUsage = uiStateUsageTitleLine + uiStateUsageBody

// uiStateUsageBody is the usage page's text. It is a raw string constant (a
// second constant rather than one concatenated expression) so the page stays
// readable — and so the -h fingerprint and the tests can assert on ONE piece.
const uiStateUsageBody = `
Usage:
  gopi ` + CliVerbUIState + ` [--pid N] [--target ui|chat] [--json] [--list]
  gopi ` + CliVerbUIState + ` --compare "<line>" [--session <id|path>] [--target ui|chat] [--json]
  gopi ` + CliVerbUIState + ` --all [--session <id|path>] [--target ui|chat] [--last N] [--json]

One call, zero pixels, zero vision. The gopi ui --dump-screen sink writes the
surface's own interpreted screen state (text + a .json twin); this verb FINDS
that dump, proves whose it is, and prints the state - or refuses.

WHAT IT IS: ` + uiclient.ScreenGridNotice + `.
WHAT IT IS NOT: not a pixel grab, not a byte stream (sequence order and timing
are gone), and not a font rendering - the blind spots below are printed with
every answer.

Flags:
  --all      AUDIT THE WHOLE ANSWER, one line of shell:
               gopi ` + CliVerbUIState + ` --target chat --all
             The session flag is OPTIONAL here: the authority is DERIVED from the
             dump itself (the session record one level above it), so the command
             above is the whole invocation - nothing is handed over, and the
             verdict is the only thing a reader sees. Name a session only to audit
             against a DIFFERENT transcript on purpose.
               gopi ` + CliVerbUIState + ` --all --session <session-id> --target chat
             Every line of the session's text is checked against the ARMED dump's
             grid, as ONE unit, and the ledger says how many lines matched and how
             many disagreed - with, per disagreeing line, the DIRECTION (which
             layer lost it), the line's own width in cells vs runes, and the
             geometry/frames the verdict was computed on. The damage it NAMES:
               paint_loss                in the log, fits the grid, not on it:
                                         the band never drew it (a real loss);
               wrap_artifact_or_loss     in the log, WIDER than the grid: a correct
                                         wrap explains it as well as a loss, and one
                                         frame cannot tell the two apart;
               cjk_wrap                  as above, wider in CELLS than in RUNES: the
                                         layout's double-width arithmetic is the
                                         candidate explanation;
               emphasis_unpainted        the text arrived and the format marked
                                         part of it emphasised, while the grid's
                                         cells carry the row's plain style: the
                                         emphasis was lost (or never applied).
             A line the grid WRAPPED is re-joined and reported as arrived, never as
             a loss. The ledger's outcome= word is one of ` + uiStateAuditWordAgreed + ` / ` + uiStateAuditWordDisagreed + ` /
             ` + uiStateAuditWordCouldNotRun + `, and the exit code is derived from it:
             ` + uiStateCompareExitCodeStr + ` = at least one line disagreed; ` + uiStateAuditExitCouldNotRunStr + ` = the audit COULD NOT RUN (no dump
             armed, nothing painted, a fallback geometry) - never read as agreement.
  --last N   Narrow the whole-answer audit to the answer's LAST N lines (the last
             turn). The window is stated on the ledger: an audit of the last N
             lines never claims to have covered the whole answer.
  --compare TEXT  THE ONE-CALL CHECK, and it is ONE call: is this line of the
             model's output on the PAINTED grid, and is it in the AUTHORITATIVE
             transcript? The authority is DERIVED from the dump itself (the
             transcript of the session that wrote it, one level above the dump
             directory) — so the flag plus the line alone runs. Pass the session
             flag only to check against a DIFFERENT transcript than the dump's own.
             Several
             lines may be separated by newlines (max ` + uiStateCompareMaxLinesStr + `). The verdict per line is
             match (byte-exact on both sides), mismatch (one side only - the
             direction says which layer lost it), refused (the grid may not be
             read as a screen at all: never painted, or a fallback geometry),
             no-transcript (a transcript was READ and held no lines), ` + uiStateAuditWordOffGrid + `
             (the line is OUTSIDE the observed window: the grid's saved history
             holds it, the visible grid does not), or
             ` + uiStateAuditWordNoAuthoritative + ` (no authoritative record exists for
             this artifact at all - see the note below). Exit ` + uiStateCompareExitCodeStr + ` = a line the two sides disagree about;
             exit ` + uiStateExitOffGridStr + ` = the line is off the observed window (` + uiStateAuditWordOffGrid + `), which is
             UNDECIDABLE for the visible grid rather than a contradiction - read the
             reason's FIRST WORD (` + uiclient.CompareReasonSideTranscript + ` / ` + uiclient.CompareReasonSideGrid + ` / ` + uiclient.CompareReasonSideNeither + ` / ` + uiclient.CompareReasonSideOffGrid + `)
             to learn which side a finding is attributed to, without parsing prose.
             THE ROW GRAMMAR IS PER SURFACE: a grid row carries the LABEL its own
             surface draws to the left of the text, and each is removed before the
             comparison - the chat surface's band markers, the ui surface's
             ` + "`you>`" + `/` + "`gopi>`" + ` entry prefixes. Both surfaces' records derive the same way
             (the session record one level above the dump).
             THE UI SURFACE: ` + string(uiclient.SurfaceUI) + ` runs the SAME engine as ` + string(uiclient.SurfaceChat) + ` and
             writes the same session record beside its dump, so --compare works
             there too - the lines to look for are what the model produced, WITHOUT
             the row's label (` + "`gopi> UI-AUTH-OK-8842 qwerty`" + ` is the ROW; the
             transcript line is ` + "`UI-AUTH-OK-8842 qwerty`" + `). Pass the row's text verbatim
             only when checking the surface's own drawing rather than the model's.
             THE EXIT CONTRACT (deliberate, not incidental): exit 0 means the
             check RAN and agreed; exit 1 means the check COULD NOT RUN - no dump
             armed, a grid nothing painted, or a fallback geometry. An unchecked
             screen is not a verified one, so "could not run" must never be read
             as "it all agrees": branch on 0 vs non-zero, which is why the two
             live on different codes.
             USE IT WHEN a wrap looks like a truncation: a dump re-interpreted
             at the wrong width fabricates corruption, and this tells the two
             apart - "in the log, not on the grid" is a paint/re-wrap question,
             while "in neither" is a loss.
  --session ID|PATH  OPTIONAL override of the authority the check reads: a
             session id (resolved by ` + "`gopi session log`" + `'s own lookup) or a path to
             session.jsonl. Omitted, the transcript is derived from the dump
             (that file, in the dump's own session directory). The text compared
             is exactly what ` + "`gopi session log <id> --response --text`" + ` prints. It is a
             SEPARATE flag rather than an argument of --compare so that the
             default case needs no ceremony.
             WHEN THE DERIVATION FINDS NOTHING, the verb says WHICH fact it is, and
             it does not say it the same way twice: a transcript it READ and found
             empty is ` + uiStateAuditWordCouldNotRun + ` (with the verdict no-transcript), while an
             artifact whose surface keeps NO record to derive - a dump with no
             session record beside it at all - is ` + uiStateAuditWordNoAuthoritative + `:
             the check had no other SIDE, which is a different statement from "the
             screen could not be checked" and never reads as a paint loss.
  --pid N    Read the dump written by that process. The unambiguous route: a
             caller that knows which surface it means gets exactly that screen.
  --target S Which self-observing surface to read: ` + string(uiclient.SurfaceUI) + ` (the BubbleTea TUI,
             whose dump is ` + uiclient.DumpFileName + `) or ` + string(uiclient.SurfaceChat) + ` (the line editor,
             ` + uiclient.ChatDumpFileName + `). Both surfaces write into the same session tree, so
             without it a chat running beside a ui is a FALSE ambiguity.
  --json     Print the machine-checkable rendering (the dump JSON, the identity
             verdict, and the blind-spot list).
  --list     Print every dump found, with each one's identity verdict; never
             refuses (this is the surface that resolves an ambiguity).

The THIRD channel: both checks above compare the transcript with the dump's grid,
which is what the dump's EMULATOR believes was painted. That interpreter has its
own fidelity property, and it is checkable without an operator (internal/lineedit,
TestScreenReplayAgreesAtEveryChunkSize): a recorded pty capture, given as
GOPI_REPLAY_RAW=<pty.raw> with the geometry read off this same dump header
(cols/rows from terminal= and grid=), must yield ONE screen no matter how many
bytes each read returns — a chunk boundary is the reader's artifact, never the
paint's. Run it before blaming a layer:

  GOPI_REPLAY_RAW=<capture> GOPI_REPLAY_COLS=117 GOPI_REPLAY_ROWS=26 \
    go test ./internal/lineedit/ -run TestScreenReplayAgreesAtEveryChunkSize -v

A failure there means the dump cannot be read as the paint at all — fix that
before comparing channels.

Discovery (no --pid): both surfaces' dumps are looked for in
  <profile>/sessions/**/{ui,chat}-screen.dump     (a surface that owns its session)
  <scratch>/queue/queue-runtime-ui-session-*/ui-screen.dump   (a --server surface)
PROFILE is $GOPI_HOME/.gopi (or ~/.gopi). A run given an explicit dump path is
found by --pid, or read directly.

Verdicts:
  live       the dump's writer is still that very process (pid + start ticks)
  exited     the writer is gone - the dump is its LAST frame (said out loud)
  foreign    the pid is alive but is NOT the writer (a recycled pid)
  unreadable the file is not a dump this build can parse

Exit codes: 0 the check RAN and the two sides AGREED; 1 no dump / a bad flag / a
check that COULD NOT RUN (nothing painted, blank grid, fallback geometry) OR with NO
AUTHORITATIVE SIDE (` + uiStateAuditWordNoAuthoritative + `) - never reported as verified, and outcome=
says which; ` + uiStateExitCodeAmbiguous + ` several live surfaces (pass --pid); ` + uiStateExitOffGridStr + ` a --compare line OUTSIDE the
observed window (the grid's SAVED HISTORY has it, the visible grid does not: ` + uiStateAuditWordOffGrid + `)
- UNDECIDABLE, never agreement, never a mismatch; contract in ` + "`man gopi-" + CliVerbUIState + "`" + `.

AN ALL-BLANK GRID IS QUALIFIED, NOT ANSWERED. Two different facts leave a blank
grid, and the output says which one it is, above the grid it qualifies:
  unpainted  frames=0 - the sink never interpreted a frame (nothing painted
             yet, or the panel was erased as the surface quit);
  blank      frames>0 and every cell blank - the sink interpreted frames and the
             grid still holds no content. This is the CHAT surface's ordinary
             shape while it is idle: the layout paints only when the screen
             CHANGES, so an idle 117x26 chat measures frames=3 dumps=1 and its
             artifact does not change again (measured: byte-identical header over
             10s). A --compare against such a grid is REFUSED (exit 1), never
             answered "not in the grid" - the grid has no content to disagree
             with.
Read frames= as "how much paint happened", never as "the screen is this": a
turn that painted reads frames=117 dumps=10, a chat that answered reads
` + "`frames=687 dumps=45`" + `. The exact command sequence is on ` + "`gopi chat -h`" + ` under
--dump-screen.
` + uiStateNoteRegionBegin + `
NOTE — the two exit-1 facts. A check can fail to RUN without the screen being
wrong: outcome= ` + uiStateAuditWordNoAuthoritative + ` means there was no OTHER SIDE to compare,
outcome= ` + uiStateAuditWordCouldNotRun + ` means the screen could not be read as a paint. Neither is a
paint loss. The full contract - both words, the damage taxonomy, the exit codes -
is in ` + "`man gopi-" + CliVerbUIState + "`" + `.
` + uiStateNoteRegionEnd + `
`

// uiStateRequest is the parsed invocation.
type uiStateRequest struct {
	pid  int
	json bool
	list bool
	// target narrows the scan to ONE surface ("" = every known surface).
	target uiclient.Surface
	// compare is the line(s) to check against the painted grid, session the
	// authoritative transcript, and audit the WHOLE-ANSWER mode (all / last N
	// lines). An empty compare with all/last unset means the verb behaves exactly
	// as it did before this mode existed.
	compare string
	session string
	// all audits every line of the transcript; lastN narrows that to the answer's
	// last N lines (0 = no window).
	all   bool
	lastN int
}

// parseUIStateArgs parses the verb's argv without touching the process
// (unit-testable, per the parseCaptureArgs / uiParseFlags pattern). An unknown
// flag is REFUSED by name — never an agent-loop fallthrough.
func parseUIStateArgs(args []string) (uiStateRequest, bool, error) {
	req := uiStateRequest{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == HelpFlag || a == HelpFlagLong {
			return req, true, nil
		}
		name, inline := a, ""
		if k := strings.Index(a, uiStateFlagSep); k > 0 {
			name, inline = a[:k], a[k+1:]
		}
		switch name {
		case uiStateFlagJSON:
			req.json = true
		case uiStateFlagList:
			req.list = true
		case uiStateFlagTarget:
			value := inline
			if value == "" {
				if i+1 >= len(args) {
					return req, false, fmt.Errorf("%s needs a surface (%s or %s)", uiStateFlagTarget, uiclient.SurfaceUI, uiclient.SurfaceChat)
				}
				i++
				value = args[i]
			}
			surface, err := uiclient.ParseSurface(value)
			if err != nil {
				return req, false, fmt.Errorf("%s: %w", uiStateFlagTarget, err)
			}
			req.target = surface
		case uiStateFlagCompare, uiStateFlagSession, uiStateFlagLast:
			value := inline
			if value == "" {
				if i+1 >= len(args) {
					return req, false, fmt.Errorf("%s needs a value", name)
				}
				i++
				value = args[i]
			}
			switch name {
			case uiStateFlagCompare:
				req.compare = value
			case uiStateFlagSession:
				req.session = value
			default:
				n, err := strconv.Atoi(value)
				if err != nil || n <= 0 {
					return req, false, fmt.Errorf("%s %q is not a positive line count", uiStateFlagLast, value)
				}
				req.lastN = n
			}
		case uiStateFlagAll:
			req.all = true
		case uiStateFlagPID:
			value := inline
			if value == "" {
				if i+1 >= len(args) {
					return req, false, fmt.Errorf("%s needs a pid", uiStateFlagPID)
				}
				i++
				value = args[i]
			}
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 0 {
				return req, false, fmt.Errorf("%s %q is not a positive pid", uiStateFlagPID, value)
			}
			req.pid = pid
		default:
			if !strings.HasPrefix(a, uiStateFlagPrefix) {
				return req, false, fmt.Errorf("unexpected positional argument %q (flags only)", a)
			}
			return req, false, fmt.Errorf("unknown flag %s", name)
		}
	}
	return req, false, nil
}

// isUIStateSubcommand reports whether argv begins with the ui_state verb and
// runs it to completion (the process exits inside).
func isUIStateSubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbUIState {
		return false
	}
	os.Exit(runUIStateSubcommand(args[1:]))
	return true
}

// runUIStateSubcommand executes the verb and returns its exit code.
func runUIStateSubcommand(args []string) int {
	req, help, err := parseUIStateArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, err)
		return 1
	}
	if help {
		fmt.Print(uiStateUsage)
		return 0
	}
	roots, err := uiStateRoots()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, err)
		return 1
	}
	cands, truncated, scanErr := uiclient.Candidates(roots, time.Now())
	// The target filter is applied BEFORE the selection rule, so "no dumps of
	// that surface" is reported as the absence it is (exit 1, ErrNoCandidates)
	// rather than as an ambiguity between surfaces the caller did not ask for.
	cands = uiclient.FilterSurface(cands, req.target)
	if len(cands) == 0 {
		scanErr = uiclient.ErrNoCandidates
	}
	if scanErr != nil && !errors.Is(scanErr, uiclient.ErrNoCandidates) {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, err)
		return 1
	}
	if req.list {
		return uiStateRenderList(cands, truncated, req.json, roots)
	}
	// THIS RUN'S OWN SURFACE IDENTITY, read before the selection rule runs
	// (ui_state_self.go). An unusable value is SAID and the read continues under
	// the general rule — a malformed channel is a fact to report, never a reason
	// to refuse a screen, and never a claim to believe (Rule 15).
	claim, claimErr := uiStateSelfIdentity("")
	if claimErr != nil {
		fmt.Fprintf(os.Stderr, uiStateClaimUnusableFormat+"\n", CliVerbUIState, claimErr)
	}
	selection := uiStateSelect(req, cands, truncated, claim)
	sel, selErr := selection.Sel, selection.Err
	if selErr != nil {
		// A refusal is an ANSWER, and it is rendered as one: the ambiguity
		// candidates are listed, and the exit code says which refusal it was.
		code := 1
		var ambiguous *uiclient.AmbiguityError
		if errors.As(selErr, &ambiguous) {
			code = uiStateExitAmbiguous
		}
		if req.json {
			uiStatePrintJSON(uiStateJSONDoc(cands, nil, selErr, truncated, roots))
		} else {
			fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbUIState, selErr)
			// The absence is a fact; the ROUTE OUT of it is a second fact, and
			// only an absence-shaped refusal gets it: an ambiguity already names
			// its remedy (`--pid`) and lists the candidates.
			if uiclient.DumpAbsent(selErr) {
				fmt.Fprintln(os.Stderr, uiclient.DumpNotArmedNotice)
			}
			if len(cands) > 0 {
				fmt.Fprint(os.Stderr, uiStateCandidateTable(cands))
			}
		}
		return code
	}
	if req.all || req.lastN > 0 {
		// The WHOLE-ANSWER audit: every line of the session's text against this
		// dump's grid. It replaces the grid rendering for the same reason the
		// one-line compare does — the caller asked a question about the artifact,
		// and the answer is the ledger, not the whole screen again.
		return uiStateRunAudit(sel.Candidate, req.session, req.lastN, req.json)
	}
	if req.compare != "" {
		// The ONE-CALL compare: this dump's grid against the authoritative
		// transcript. It replaces the grid rendering rather than adding to it —
		// the caller asked a question about the artifact, and the answer is the
		// verdict, not the whole screen again.
		return uiStateRunCompare(sel.Candidate, req.compare, req.session, req.json)
	}
	if req.json {
		doc := uiStateJSONDoc(cands, &sel.Candidate, nil, truncated, roots)
		doc.Claim = selection.ClaimNote
		uiStatePrintJSON(doc)
		return 0
	}
	// The claim sentence rides ABOVE the observation it qualifies, the same
	// order every other qualification in this verb keeps: a reader that quotes
	// only the top of the output still learns HOW the artifact was claimed.
	if selection.ClaimNote != "" {
		fmt.Println(selection.ClaimNote)
	}
	uiStatePrintText(sel.Candidate)
	return 0
}

// uiStateRoots resolves the two discovery roots for THIS profile: the gopi data
// root (the sessions tree) and the scratch home (the ui-session runtime class).
// Resolution is delegated to the packages that own each path (Rules 13/14) —
// this verb is not a second place where a profile path is decided.
func uiStateRoots() ([]string, error) {
	gopiDir, err := config.GopiDir()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the gopi data root: %w", err)
	}
	return uiclient.ScanRoots(gopiDir, scratch.HomeDir()), nil
}

// uiStateSelect applies the request's selection rule.
//
// THE ORDER IS THE CONTRACT. An explicit `--pid` is the caller naming a
// PROCESS, and it wins outright (it is the unambiguous route, and a --session
// must not be able to redirect it). Otherwise a `--session` the caller named —
// with a check to run — is the caller naming the SESSION, and the candidate dump
// that belongs to it IS the target: that is the disambiguator twelve exited chat
// dumps were missing (measured 2026-09-27: the verb refused an ambiguity even
// though the caller's own session was on its command line). With neither,
// SelectLive's rule stands: the one live surface, else a refusal that lists what
// is on hand.
//
// A `--session` the caller named is answered BY the session rule in both cases:
// when a dump belongs to it, that dump is the target; when none does, the refusal
// is the one that NAMES the sessions the dumps on hand belong to — a caller who
// said which session they meant must not be answered with a list of surfaces they
// did not ask about. The `--pid` route (or `--list`) stays available, and a
// `--session` that cannot be resolved at all falls through to that live rule,
// because the read is the surface that reports an unresolvable authority — by
// name, with the code for "could not run".
func uiStateSelect(req uiStateRequest, cands []uiclient.Candidate, truncated bool, claim uiclient.SelfIdentity) uiStateSelection {
	if req.pid > 0 {
		sel, err := uiclient.SelectByPID(cands, req.pid)
		return uiStateSelection{Sel: sel, Err: err}
	}
	if strings.TrimSpace(req.session) != "" && (req.compare != "" || req.all || req.lastN > 0) {
		named, err := uiStateNamedSession(req.session)
		if err == nil {
			sel, serr := uiclient.SelectBySession(cands, named)
			return uiStateSelection{Sel: sel, Err: serr}
		}
	}
	// THIS RUN'S OWN ARTIFACT, claimed by the identity it published to its
	// children (ui_state_self.go), and the general rule only when there is
	// nothing to claim with. The two routes above keep their precedence: a
	// caller that NAMED a process or a session is not overridden by a claim
	// the caller did not make in this invocation.
	return uiStateSelectOwn(cands, claim, truncated)
}

// uiStateNamedSession resolves a `--session` operand to a session RECORD path, so
// the dump-selection rule and the transcript read agree about which session the
// caller meant. An id is resolved through gopilog's own lookup (the same lookup
// the read uses); a path is used as given. A failure is returned so the caller
// can fall back to the live selection rule and let the read report it.
func uiStateNamedSession(session string) (string, error) {
	session = strings.TrimSpace(session)
	if strings.HasSuffix(session, ".jsonl") || strings.ContainsRune(session, filepath.Separator) {
		return session, nil
	}
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "", err
	}
	found, err := gopilog.FindSession(dir, session)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(found) == "" {
		return "", fmt.Errorf(uiStateCompareSessionUnresolved, session, uiStateNoSessionForID)
	}
	return found, nil
}

// uiStatePrintText renders one observation in the readable form.
func uiStatePrintText(c uiclient.Candidate) {
	fmt.Printf("#gopi-ui-state surface=%s pid=%d target=%s started=%s state=%s dump=%s\n",
		c.Surface, c.Record.PID, c.Record.Target, c.Record.Started.UTC().Format(time.RFC3339),
		c.State, c.Path)
	fmt.Println(uiclient.ScreenGridNotice)
	if c.State == uiclient.StateExited {
		// The diagnosis, in the same shape the identity verdict carries it: an
		// exited surface's last frame is NOT the current screen, and a reader
		// that renders it without this sentence has made a false claim.
		fmt.Println(uiclient.ExitedNotice)
	}
	if c.Unpainted() {
		// A grid nobody ever painted is not a screen fact. The sentence is
		// emitted BEFORE the grid so a reader (or a report) that quotes only
		// the top of the output still carries the qualification.
		fmt.Println(uiclient.UnpaintedNotice)
	} else if c.BlankUnpainted() {
		// frames>0 but every cell blank: a DIFFERENT fact from frames=0, and the
		// one the chat surface really produces (measured `frames=3 dumps=1` on an
		// idle 117x26 pty, frozen for 10 s). Emitted in the same place and for the
		// same reason: the qualification must ride above the grid it qualifies.
		fmt.Println(uiclient.BlankUnpaintedNotice)
	}
	if c.SinkError != "" {
		fmt.Printf("sink_error: %s\n", c.SinkError)
	}
	if c.Reason != "" && c.State != uiclient.StateExited {
		fmt.Printf("why: %s\n", c.Reason)
	}
	fmt.Print(c.Dump.Text())
}

// uiStateRenderList prints every candidate with its verdict, so an ambiguity is
// resolvable (this is the non-refusing surface) — in text, or as JSON.
func uiStateRenderList(cands []uiclient.Candidate, truncated, asJSON bool, roots []string) int {
	if asJSON {
		uiStatePrintJSON(uiStateJSONDoc(cands, nil, nil, truncated, roots))
		return 0
	}
	fmt.Printf("#gopi-ui-state-list scanned=%d roots=%d\n", len(cands), len(roots))
	fmt.Println(uiclient.ScreenGridNotice)
	fmt.Print(uiStateCandidateTable(cands))
	if len(cands) == 0 {
		fmt.Printf("no %s dump found; roots:\n", uiclient.DumpFileName)
		for _, r := range roots {
			fmt.Printf("  %s\n", r)
		}
		fmt.Println(uiclient.DumpNotArmedNotice)
	}
	return 0
}

// uiStateCandidateTable renders one line per candidate: the fields that resolve
// an ambiguity (pid, target, start, verdict) plus the path.
func uiStateCandidateTable(cands []uiclient.Candidate) string {
	if len(cands) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-5s %-8s %-10s %-30s %-22s %s\n", "surf", "pid", "state", "target", "started", "dump")
	for _, c := range cands {
		started := "-"
		if !c.Record.Started.IsZero() {
			started = c.Record.Started.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "%-5s %-8d %-10s %-30s %-22s %s\n",
			c.Surface, c.Record.PID, c.State, c.Record.Target, started, c.Path)
	}
	return b.String()
}

// uiStateJSONRecord is one candidate as the JSON rendering carries it: the
// identity (so an ambiguity is resolvable from the JSON alone), the verdict and
// its reason, and — for a parsed dump — the whole screen state plus the blind
// spots that bound it.
type uiStateJSONRecord struct {
	Path       string                   `json:"path"`
	Surface    string                   `json:"surface"`
	State      uiclient.State           `json:"state"`
	Reason     string                   `json:"reason,omitempty"`
	PID        int                      `json:"pid"`
	Target     string                   `json:"target"`
	Tools      bool                     `json:"tools"`
	Started    time.Time                `json:"started"`
	ModTime    time.Time                `json:"dump_mtime"`
	Frames     int                      `json:"frames"`
	Dumps      int                      `json:"dumps"`
	SinkError  string                   `json:"sink_error,omitempty"`
	Screen     *lineedit.ScreenDump     `json:"screen,omitempty"`
	BlindSpots []lineedit.DumpBlindSpot `json:"blind_spots,omitempty"`
	Fields     map[string]string        `json:"record_fields,omitempty"`
}

// uiStateJSONDoc is the machine-checkable rendering: the chosen state (when one
// was chosen), every candidate, the roots scanned, the refusal, and the blind
// spots — which are carried SEPARATELY from the dump so they are present even
// when no dump could be read at all. A JSON caller must never have to guess
// what this representation cannot see (the whole point of the blind-spot list).
type uiStateJSONDocument struct {
	Tool string `json:"tool"`
	// Surface is the surface of the CHOSEN dump ("ui" or "chat"); empty on a
	// refusal, when no dump was chosen.
	Surface    string `json:"surface,omitempty"`
	GridNotice string `json:"what_this_is"`
	State      string `json:"state,omitempty"`
	Exited     string `json:"exited_notice,omitempty"`
	Unpainted  string `json:"unpainted_notice,omitempty"`
	Refusal    string `json:"refusal,omitempty"`
	// RouteOut is the way to have a dump NEXT time, present exactly when the
	// refusal is the absence of one (a machine caller must be able to act on the
	// absence without parsing an English sentence out of Refusal).
	RouteOut  string `json:"route_out,omitempty"`
	Ambiguous bool   `json:"ambiguous,omitempty"`
	// Claim is HOW the artifact was claimed, when a claim applied at all: the
	// matching half (`by=pid`, `by=session`, `by=pid+session`) or the scarcity
	// arm (`own-identity matched no artifact; this is the only one on hand`).
	// It is carried in the machine rendering for the same reason the blind spots
	// are: a JSON caller must never have to parse an English sentence out of
	// another field to learn what the evidence for this read was.
	Claim      string                   `json:"claim,omitempty"`
	Truncated  bool                     `json:"scan_truncated,omitempty"`
	Roots      []string                 `json:"roots"`
	Scanned    int                      `json:"scanned"`
	Candidates []uiStateJSONRecord      `json:"candidates"`
	BlindSpots []lineedit.DumpBlindSpot `json:"blind_spots"`
}

// uiStateJSONDoc builds the JSON document for one observation.
func uiStateJSONDoc(cands []uiclient.Candidate, chosen *uiclient.Candidate, refusal error, truncated bool, roots []string) uiStateJSONDocument {
	doc := uiStateJSONDocument{
		Tool:       CliVerbUIState,
		GridNotice: uiclient.ScreenGridNotice,
		Truncated:  truncated,
		Roots:      roots,
		Scanned:    len(cands),
		// The blind spots belong to the REPRESENTATION, so they are emitted
		// from the package-level definition — present even on a refusal, when
		// no dump was read at all.
		BlindSpots: uiStateAllBlindSpots(),
	}
	if refusal != nil {
		doc.Refusal = refusal.Error()
		if uiclient.DumpAbsent(refusal) {
			doc.RouteOut = uiclient.DumpNotArmedNotice
		}
		var ambiguous *uiclient.AmbiguityError
		doc.Ambiguous = errors.As(refusal, &ambiguous)
	}
	for i := range cands {
		doc.Candidates = append(doc.Candidates, uiStateRecord(&cands[i]))
	}
	if chosen == nil {
		return doc
	}
	doc.State = string(chosen.State)
	if chosen.State == uiclient.StateExited {
		doc.Exited = uiclient.ExitedNotice
	}
	if chosen.Unpainted() {
		doc.Unpainted = uiclient.UnpaintedNotice
	} else if chosen.BlankUnpainted() {
		doc.Unpainted = uiclient.BlankUnpaintedNotice
	}
	doc.Candidates = nil
	doc.Candidates = append(doc.Candidates, uiStateRecord(chosen))
	return doc
}

// uiStateAllBlindSpots returns the blind spots of the representation itself.
// They are read from a dump taken from a throwaway grid, not from a second list
// typed here: the blind spots belong to the cell grid, so the grid is asked
// (Rule 13 - a copied list stops being the representation's the moment the
// representation changes).
func uiStateAllBlindSpots() []lineedit.DumpBlindSpot {
	return lineedit.NewScreen(1, 1).Dump().BlindSpots
}

// uiStateRecord renders one candidate, screen included.
func uiStateRecord(c *uiclient.Candidate) uiStateJSONRecord {
	dump := c.Dump
	return uiStateJSONRecord{
		Path:       c.Path,
		Surface:    string(c.Surface),
		State:      c.State,
		Reason:     c.Reason,
		PID:        c.Record.PID,
		Target:     c.Record.Target,
		Tools:      c.Record.Tools,
		Started:    c.Record.Started,
		ModTime:    c.ModTime,
		Frames:     c.Frames,
		Dumps:      c.Dumps,
		SinkError:  c.SinkError,
		Screen:     &dump,
		BlindSpots: dump.BlindSpots,
		Fields:     c.Record.Fields,
	}
}

// uiStatePrintJSON writes the document, reporting a marshal failure loudly
// (Rule 15: a rendering nobody can parse must say so, never print an empty
// object that reads as "no screen").
func uiStatePrintJSON(doc uiStateJSONDocument) {
	blob, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: cannot render the JSON: %v\n", CliVerbUIState, err)
		return
	}
	fmt.Println(string(blob))
}
