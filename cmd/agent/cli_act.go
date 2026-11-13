// cli_act.go — `gopi tool act` : drive the LIVE X11 desktop (move the pointer,
// click, type) with a pure-Go XTEST client.
//
// This is the ACTUATE half of the computer-use loop:
//
//	gopi tool screen_capture --out before.png   (see)
//	gopi tool act move 400 300 && gopi tool act click 1   (touch)
//	gopi tool screen_capture --out after.png    (verify — compare sha256)
//
// It is deliberately a CLI verb reached through the `bash` tool rather than a
// 17th LLM tool: Rule 3 (tool count is cared about) and the CH9329 actuator
// study both concluded that actuation ships as a verb first — an LLM tool slot
// is promoted only with Rule 11 adoption evidence (see devdoc
// screen-capture-and-actuation-tools.md §6).
//
// Safety gating (why this is not a footgun):
//   - `gopi tool act info` is the read-only readiness probe (no injection at all).
//   - --dry-run resolves and prints the exact plan without injecting.
//   - every numeric target is range-checked against the root geometry, and a
//     refused action is a loud error, never a silent no-op (Rule 15).
//   - text is limited to the ASCII keysyms this client can prove, so it can
//     never type mojibake it cannot see.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/shot"
)

// CliVerbAct is the `gopi tool act` verb — synthetic pointer/keyboard input on an
// X11 display.
const CliVerbAct = "act"

// actMinArgs is the smallest argv the subcommand accepts (the sub-action).
const actMinArgs = 1

// actAction* are the sub-action words (Rule 1: no bare literals in the switch).
const (
	actActionInfo  = "info"
	actActionMove  = "move"
	actActionClick = "click"
	actActionType  = "type"
	actActionKey   = "key"
)

// actFlag* are the flag names (Rule 1b: the parser, the member list and the
// usage text all reference these constants, never the bare word).
const (
	actFlagDisplay = "display"
	actFlagButton  = "button"
	actFlagText    = "text"
	actFlagKey     = "key"
	actFlagEnter   = "enter"
	actFlagDryRun  = "dry-run"
	// actFlagSelf is the ADDRESSING flag: type into the window that HOSTS this
	// process rather than into whatever holds the focus. Its value is the one
	// constant shot.SelfAddressSymbol, shared with `gopi tool capture --self`
	// (Rule 14: one definition, every consumer imports it).
	actFlagSelf = shot.SelfAddressSymbol
)

// actEnterRune is what `--enter` appends and what a decoded \n is: the one
// character internal/shot already maps to the Return keysym in TypeText, so
// the CLI has no private keysym table and the shortcut cannot drift from the
// explicit `gopi tool act key --key return` verb.
const actEnterRune = '\n'

// actUsage is the -h text for the act subcommand.
const actUsage = `gopi tool act — drive the LIVE X11 desktop: move the pointer, click, type

Injects real input events through the XTEST extension with a hand-rolled
zero-dependency, zero-CGO X11 client (no xdotool binary needed, nothing
linked). The ACTUATE half of the computer-use loop:

  gopi tool screen_capture --out before.png
  gopi tool act move 400 300
  gopi tool act click 1
  gopi tool screen_capture --out after.png     # compare sha256_16

Display routing: a display names its OWN transport (localhost:N is reached
over TCP, a bare :N over the local unix socket) and that transport is tried
first. If it does not answer, the other LOCAL transport for the same display
number is tried as a fallback — a dead SSH forward still finds the display
this host serves locally. A remote host gets no fallback, and a display
reached by fallback echoes "@fallback". When nothing answers, the error lists
every route tried plus any live local display.

Sub-actions:
  gopi tool act info                      Read-only readiness probe (no input).
  gopi tool act move X Y                  Warp the pointer to root coordinates.
  gopi tool act click [--button N]        Press+release a button at the pointer.
  gopi tool act type [--text STR]         Type text (escape sequences decoded).
  gopi tool act key [--key NAME]          Press one named key.

Addressing — --self (type and key only):
  By default the synthetic keys go wherever the server's input focus already
  is, which is the window the user last clicked. --self instead asks "where am
  I RUNNING" — it reads this process' controlling tty, finds the terminal
  emulator that owns the pty master, and matches that pid against each window's
  _NET_WM_PID — then puts the input there EXPLICITLY before typing:
  XSetInputFocus(that window), then the pointer inside its rectangle. Both are
  done because a bare Xvfb (no window manager) reports PointerRoot focus, where
  only the pointer's position decides who receives the keys.
  It never falls back to the ACTIVE window: if the window cannot be resolved the
  verb REFUSES and prints every leg it tried, why each failed, and what to do.
  Combine it with voice_input for hands-free text entry into this window:
    gopi tool voice_input --client --self

Flags:
  --display DISPLAY   X11 display to drive (default: $DISPLAY).
  --self              Address the window that HOSTS this process (type/key
                      only). The JSON carries window/window_route plus the
                      focus readback around the request.
  --button N          Mouse button: 1 left (default), 2 middle, 3 right.
  --text STR          Text for ` + "`type`" + `. Escape sequences are decoded before
                      typing, so a newline really presses Return:
                        \n \r \t \b \f \v \a \0 \\ \" \xHH \uHHHH \UHHHHHHHH
                      An unknown escape (e.g. \q) is a loud error, never typed
                      literally. Decoding is not a promise that the client can
                      TYPE the result: the typable set is printable ASCII, Tab
                      and a newline (which presses Return). Any other decoded
                      control character (\r, \a, …) is refused loudly by the
                      X11 layer and NOTHING is injected. Quote the value in
                      single quotes so the shell leaves the backslashes alone:
                        gopi tool act type --text 'say hello\n'    # types + Enter
                      A trailing newline is a *shortcut* for the documented
                      act key verb, not a second injection path: it is appended
                      to the text, and the SAME type path presses it (this CLI
                      has no private keysym table, so the shortcut cannot drift
                      from gopi tool act key --key return).
  --enter             Press Return after the text (same as trailing \n).
  --key NAME          One of: return, tab, escape, space, delete, home, end.
  --dry-run           Print the resolved plan and exit WITHOUT injecting.

Examples:
  gopi tool act type --text 'say hello\n'     type the greeting, then submit it
  gopi tool act type --text 'say hello' --enter   the same thing, spelled out
  gopi tool act key --key return              press Return on its own

  # Type into THIS process's own window, wherever the focus happens to be:
  gopi tool act type --text 'ls -l\n' --self
  # See which window that resolves to WITHOUT injecting anything:
  gopi tool act type --text 'x' --self --dry-run
  # Send a transcript voice_input typed into this window (see voice_input --self):
  gopi tool act key --key return --self
`

// actFlagMembers lists every flag the subcommand accepts.
var actFlagMembers = map[string]bool{
	actFlagDisplay: true, actFlagButton: true, actFlagText: true,
	actFlagKey: true, actFlagEnter: true, actFlagDryRun: true,
	actFlagSelf: true,
}

// isActSubcommand reports whether argv begins with the act subcommand.
func isActSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbAct {
		runActSubcommand(args[1:])
		return true
	}
	return false
}

// actRequest is the parsed invocation.
type actRequest struct {
	action  string
	display string
	x, y    int
	button  int
	text    string
	enter   bool
	key     string
	dryRun  bool
	// self addresses the window that HOSTS this process instead of whichever
	// window happens to hold the focus (see actUsage).
	self bool
}

// parseActArgs parses the act flags + positional arguments (unit-testable:
// no process exit, no injection).
func parseActArgs(args []string) (actRequest, error) {
	req := actRequest{button: 1}
	if len(args) < actMinArgs {
		return req, fmt.Errorf("missing sub-action (want info|move|click|type|key)")
	}
	fs := newMemoryFlagSet()
	words, err := fs.parse(args)
	if err != nil {
		return req, err
	}
	for _, name := range fs.order {
		if !actFlagMembers[name] {
			return req, fmt.Errorf("unknown flag --%s", name)
		}
	}
	// memoryFlagSet returns the non-flag tokens in argument order: the first
	// is the sub-action, the rest are its positional arguments.
	if len(words) == 0 {
		return req, fmt.Errorf("missing sub-action (want info|move|click|type|key)")
	}
	req.action = words[0]
	req.display = strings.TrimSpace(fs.get(actFlagDisplay))
	req.dryRun = fs.seen[actFlagDryRun]
	req.self = fs.seen[actFlagSelf]

	switch req.action {
	case actActionInfo:
		if len(words) > 1 {
			return req, fmt.Errorf("'info' takes no arguments")
		}
	case actActionMove:
		if len(words) != 3 {
			return req, fmt.Errorf("'move' wants X Y in root coordinates")
		}
		x, errX := strconv.Atoi(words[1])
		y, errY := strconv.Atoi(words[2])
		if errX != nil || errY != nil {
			return req, fmt.Errorf("'move' wants integer X Y, got %q %q", words[1], words[2])
		}
		req.x, req.y = x, y
	case actActionClick:
		// Both spellings are accepted: `click 1` (positional, xdotool-style)
		// and `click --button 1`.
		switch len(words) {
		case 1:
		case 2:
			n, err := strconv.Atoi(words[1])
			if err != nil || n < 1 || n > 5 {
				return req, fmt.Errorf("'click' wants a button 1..5, got %q", words[1])
			}
			req.button = n
		default:
			return req, fmt.Errorf("'click' takes at most one button argument")
		}
		if len(words) == 2 {
			break
		}
		if v := fs.get(actFlagButton); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 5 {
				return req, fmt.Errorf("--button wants 1..5, got %q", v)
			}
			if len(words) == 2 && req.button != n {
				return req, fmt.Errorf("conflicting buttons: positional %d vs --button %d", req.button, n)
			}
			req.button = n
		}
	case actActionType:
		req.text = fs.get(actFlagText)
		req.enter = fs.seen[actFlagEnter]
		if req.text == "" && !req.enter {
			return req, fmt.Errorf("'type' needs --text STR (or --%s to press Return on its own)", actFlagEnter)
		}
		// Escape sequences are decoded HERE, at the CLI boundary, so the
		// documented `gopi tool act type --text 'hi\n'` presses Return instead of
		// typing a backslash and an n (the defect this flag closes).
		decoded, errDecode := decodeActText(req.text)
		if errDecode != nil {
			return req, errDecode
		}
		req.text = decoded
		if req.enter && !strings.HasSuffix(req.text, string(actEnterRune)) {
			req.text += string(actEnterRune)
		}
	case actActionKey:
		req.key = strings.TrimSpace(fs.get(actFlagKey))
		if req.key == "" {
			return req, fmt.Errorf("'key' needs --key NAME (%s)", strings.Join(shot.NamedKeyNames(), "|"))
		}
	default:
		return req, fmt.Errorf("unknown sub-action %q (want info|move|click|type|key)", req.action)
	}
	// --self is an ADDRESSING statement about an input TARGET, so it belongs to
	// the two verbs that inject keys and to nothing else: a pointer move already
	// names its target in root coordinates, and a click follows the pointer. A
	// flag accepted where it means nothing is the silent no-op Rule 15 refuses,
	// so it is an error naming the verbs that do take it.
	if req.self && req.action != actActionType && req.action != actActionKey {
		return req, fmt.Errorf("--%s applies to 'type' and 'key' only (it addresses the window that HOSTS this process, which is a statement about a key target); %q writes to coordinates it is given",
			actFlagSelf, req.action)
	}
	return req, nil
}

// decodeActText expands the escape sequences a `--text` value may carry, so
// the shell-facing spelling of a control character becomes that character.
//
// The escape grammar is deliberately small and closed — a shell-style subset,
// NOT Go's %q grammar, because two of its entries mean something else here:
//
//	\\   one literal backslash (must be escaped)
//	\"   one literal double quote (so a value survives `sh -c "…"` typing)
//	\n \r \t \b \f \v \a \0
//	\xHH          one byte from two hex digits (HH in [0,7F])
//	\uHHHH        a Unicode code point from four hex digits
//	\UHHHHHHHH    a Unicode code point from eight hex digits
//
// Anything else after a backslash is an ERROR naming the escape, never a
// silently-typed literal (Rule 15: an un-decoded \q must not reach the screen
// as "\q" while the caller believes the escape was understood). A stray
// backslash at the end of the value is likewise refused.
//
// The grammar is WIDER than what the X11 layer can type on purpose: decoding
// and typing are different questions, and the CLI does not silently drop a
// character it cannot press. Of the decoded set, printable ASCII, Tab and the
// newline (typed as Return) reach the screen; \r, \a, \b, \f, \v, \0 and
// non-ASCII code points are refused by internal/shot with the rune named and
// nothing injected — measured live, see the task report.
func decodeActText(s string) (string, error) {
	if !strings.ContainsRune(s, '\\') {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	// Bytes, not runes: every escape in this grammar is ASCII, and a
	// non-ASCII byte is copied through verbatim (multi-byte UTF-8 survives
	// because each byte is >= 0x80 and is never a backslash). A trailing
	// backslash is refused below rather than dropped.
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", fmt.Errorf("dangling backslash at end of %s value — write \\\\ for one literal backslash", actFlagText)
		}
		switch e := s[i+1]; e {
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '"':
			b.WriteByte('"')
			i += 2
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'b':
			b.WriteByte('\b')
			i += 2
		case 'f':
			b.WriteByte('\f')
			i += 2
		case 'v':
			b.WriteByte('\v')
			i += 2
		case 'a':
			b.WriteByte('\a')
			i += 2
		case '0':
			b.WriteByte(0)
			i += 2
		case 'x', 'u', 'U':
			r, width, err := decodeActHexEscape(e, s[i+2:])
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
			i += 2 + width
		default:
			return "", fmt.Errorf("unknown escape \\%c in %s value — supported: \\\\ \\\" \\n \\r \\t \\b \\f \\v \\a \\0 \\xHH \\uHHHH \\UHHHHHHHH", e, actFlagText)
		}
	}
	return b.String(), nil
}

// actHexDigits is how many hex digits each numeric escape consumes.
var actHexDigits = map[byte]int{'x': 2, 'u': 4, 'U': 8}

// decodeActHexEscape decodes one \xHH / \uHHHH / \UHHHHHHHH escape. It
// returns the decoded code point and the number of hex digits consumed.
// \x is a byte escape, so it is bounded to ASCII (a byte above 0x7F would be
// mojibake, not a code point — Rule 15); the two Unicode escapes accept any
// valid code point and refuse surrogates and out-of-range values.
func decodeActHexEscape(kind byte, rest string) (rune, int, error) {
	width := actHexDigits[kind]
	if len(rest) < width {
		return 0, 0, fmt.Errorf("\\%c needs %d hex digits, got %d", kind, width, len(rest))
	}
	digits := rest[:width]
	n, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("\\%c%s is not %d hex digits", kind, digits, width)
	}
	if kind == 'x' && n > 0x7f {
		return 0, 0, fmt.Errorf("\\x%02X is above the printable ASCII ceiling 7F — this client types ASCII only", n)
	}
	r := rune(n)
	if kind != 'x' && (r > 0x10ffff || (r >= 0xd800 && r <= 0xdfff)) {
		return 0, 0, fmt.Errorf("\\%c%s is not a valid Unicode code point", kind, digits)
	}
	return r, width, nil
}

// actEnterSuffix is the JSON detail tail reporting that the returned text
// carries the Return `--enter` appended.
const actEnterSuffix = " + Return (--" + actFlagEnter + ")"

// typeActText types the parsed --text value and records the evidence. The
// Return appended by --enter (or a decoded trailing \n) goes through the SAME
// XActuator.TypeText path as the rest of the value — internal/shot maps '\n'
// to the Return keysym — so the shortcut and an explicit `gopi tool act key --key
// return` cannot drift apart, and the JSON reports exactly what was sent.
func typeActText(a *shot.XActuator, req actRequest, res *actResult) error {
	if err := a.TypeText(req.text); err != nil {
		return err
	}
	res.Text = actTextEvidence(req.text)
	res.Detail = fmt.Sprintf("typed %d character(s)", len([]rune(req.text)))
	if req.enter {
		res.Detail += actEnterSuffix
	}
	return nil
}

// actTextSpan is the longest run of a --text value the JSON echo shows
// before it is elided: the echo is evidence that the escapes decoded, not a
// way to re-emit an unbounded string through the result line.
const actTextSpan = 120

// actTextEvidence renders the DECODED text for the JSON result. A control
// character is spelled with its escape (so a newline is visible as \n in a
// terminal, exactly the way it was written on the command line) and a long
// value is elided.
func actTextEvidence(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < ' ' && r != 0 {
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len([]rune(out)) > actTextSpan {
		out = string([]rune(out)[:actTextSpan]) + "…"
	}
	return out
}

// actResult is the JSON outcome of one invocation.
type actResult struct {
	Action  string `json:"action"`
	Display string `json:"display"`
	DryRun  bool   `json:"dry_run"`
	Pointer string `json:"pointer,omitempty"`
	Focus   string `json:"focus,omitempty"`
	Root    string `json:"root,omitempty"`
	Text    string `json:"text,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Note    string `json:"note,omitempty"`
	// The --self addressing evidence: which window was addressed, the route
	// that resolved it (the same label `gopi tool capture --self` prints), the
	// focus readback around the request, and which leg was relied on. These are
	// omitted entirely on a run that did not address a window, so a reader can
	// tell "addressed and reported" from "not addressed at all" without
	// consulting the argv.
	Self        bool   `json:"self,omitempty"`
	Window      string `json:"window,omitempty"`
	WindowRoute string `json:"window_route,omitempty"`
	SelfFocus   string `json:"self_focus,omitempty"`
	SelfMode    string `json:"self_mode,omitempty"`
}

// runActSubcommand executes the act subcommand.
func runActSubcommand(args []string) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(actUsage)
		return
	}
	req, err := parseActArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbAct, err)
		os.Exit(1)
	}
	out, err := runAct(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbAct, err)
		os.Exit(1)
	}
	fmt.Println(out)
}

// runAct performs one actuation and returns the JSON evidence. It opens the
// display for every verb (short-lived process: no state to leak) and always
// closes it.
func runAct(req actRequest) (string, error) {
	// --self resolves FIRST, before an actuator is even opened for injection:
	// the address is a precondition of the action, not a retry of it. The dry
	// run takes the read-only resolver (`ResolveSelfWindow`), so a --dry-run
	// moves neither the focus nor the pointer — a "plan" that had already
	// injected the focus change would not be a dry run.
	if req.self && req.dryRun {
		res, err := shot.ResolveSelfWindow(req.display)
		if err != nil {
			return "", err
		}
		out := actResult{
			Action: req.action, Display: res.Display, DryRun: true,
			Root: fmt.Sprintf("%dx%d", res.RootW, res.RootH),
			Self: true, Window: fmt.Sprintf("0x%x", res.Window), WindowRoute: res.Label,
			Note: "dry run: the window was resolved read-only (no focus change, no pointer move, nothing typed)",
		}
		if req.action == actActionType {
			out.Text = actTextEvidence(req.text)
		}
		return marshalActResult(out)
	}

	a, err := shot.OpenXActuator(req.display)
	if err != nil {
		return "", err
	}
	defer a.Close()

	res := actResult{Action: req.action, Display: a.Display(), DryRun: req.dryRun}
	w, h := a.RootSize()
	res.Root = fmt.Sprintf("%dx%d", w, h)

	// Address this process's own window and take the input there BEFORE any
	// key is injected. A failure here REFUSES the run (Rule 15): typing into
	// whatever window happened to be focused is exactly the mis-targeting
	// --self exists to remove, so there is no "carry on without it" arm.
	if req.self {
		sf, err := a.FocusSelf()
		if err != nil {
			return "", err
		}
		res.Self = true
		res.Window = fmt.Sprintf("0x%x", sf.Window)
		res.WindowRoute = sf.Label
		res.SelfMode = sf.Mode
		res.SelfFocus = fmt.Sprintf("0x%x -> 0x%x", sf.FocusBefore, sf.FocusAfter)
		if sf.PointerMoved {
			res.Pointer = sf.Pointer
		}
	}

	if req.dryRun {
		res.Note = "dry run: nothing was injected"
		if req.action == actActionType {
			res.Text = actTextEvidence(req.text)
		}
	} else {
		switch req.action {
		case actActionInfo:
			res.Note = "read-only probe: nothing was injected"
		case actActionMove:
			if err := a.MovePointer(req.x, req.y); err != nil {
				return "", err
			}
			res.Detail = fmt.Sprintf("moved pointer to %d,%d", req.x, req.y)
		case actActionClick:
			if err := a.Click(req.button); err != nil {
				return "", err
			}
			res.Detail = fmt.Sprintf("clicked button %d at the pointer", req.button)
		case actActionType:
			if err := typeActText(a, req, &res); err != nil {
				return "", err
			}
		case actActionKey:
			if err := a.Key(req.key); err != nil {
				return "", err
			}
			res.Detail = "pressed key " + req.key
		}
	}

	// Read the server's own view back — the machine-readable proof that the
	// action landed (or, for a dry run, the current state).
	if x, y, err := a.Pointer(); err == nil {
		res.Pointer = fmt.Sprintf("%d,%d", x, y)
	}
	if focus, err := a.Focus(); err == nil {
		res.Focus = fmt.Sprintf("0x%x", focus)
	}
	return marshalActResult(res)
}

// marshalActResult encodes one result as the single JSON line the verb prints.
// It is shared by the dry-run arm (which returns before an actuator exists) and
// the injecting arm, so both produce the same shape.
func marshalActResult(res actResult) (string, error) {
	encoded, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(encoded), nil
}
