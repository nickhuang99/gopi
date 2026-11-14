// cli_help_shorthand_gate_test.go — the CROSS-GROUP guard for the one class of
// advertised invocation no page in this family could previously be caught on:
// an ALTERNATIVE-BRANCH SHORTHAND sitting inside what looks like argv.
//
// THE CLASS. A help page routinely writes a CHOICE between spellings as a
// syntax summary — `bash|zsh|fish`, `--useful|--useless|--unused`,
// `patch.json | --import patch.json`, `"why it failed"|@FILE`. Read as syntax
// that is fine; read as an INVOCATION it is a line nobody can type, and the
// five pages measured here (`gopi config completion`, `gopi memory feedback`,
// `gopi memory provenance`, `gopi memory memsync`, and `gopi task clone` — the
// verb `gopi task reopen` then occupied) each carried one. WHY THE EXISTING
// GATES MISSED IT: every per-group derivation in
// this package reads a synopsis line for its VERB and its `--flags` — the
// shorthand tokens (`zsh`, `useless`, `@FILE`) are not flags, so they never
// became a row, and a row that does not exist cannot fail. G4 closed the
// variant of this class on its OWN page by deriving rows and then EXECUTING
// each one; the other groups derive only. THIS FILE IS THE ADMISSION HALF of
// that discipline: it derives the same rows and refuses the shapes that could
// never be executed, for every group at once, from the page CONSTANTS (never
// from `-h` output), so it is one fixed cost rather than four more runners.
//
// WHAT IT PINS, so a broken classifier cannot pass empty (the failure mode this
// class of gate is famous for):
//   - a NON-VACUITY FLOOR on the scanned line count, and a floor per class, so
//     an extractor that returns ten lines fails instead of reporting zero
//     violations;
//   - the page family's existing GREEN shapes as NAMED CONTROLS — the `| jq`
//     worked example (cli_stats.go), `-h | --help` (main.go's help door) and
//     `[--mode chat|web]` (cli_session.go's optional group) — each of which
//     must classify exactly as named, in BOTH the red and the green state;
//   - RED controls for every refused shape, so the refusal is proven reachable
//     rather than merely unreached.
//
// THE RULE, and why each of its four arms is needed (all five arms are
// measured against real lines, not assumed):
//
//	A `|` at bracket depth >= 1 is an OPTIONAL GROUP (`[--mode chat|web]`,
//	`[--tier hot|warm|cold]`) — the page is saying "one of these, optionally",
//	which is exactly how an operator reads it. ALLOWED.
//
//	At depth 0 with the alternatives GLUED to the pipe (`a|b`, no spaces) the
//	line advertises one literal token that no dispatcher accepts
//	(`bash|zsh|fish`). REFUSED.
//
//	At depth 0, SPACED (` | `), the tail decides: a shell PROGRAM word there
//	(`--json | jq .prompt_tokens`) is a worked EXAMPLE and is the page's own
//	control. REFUSED only when the tail is flag-shaped (`--out patch.json |
//	--import patch.json`) — a flag cannot be the command of a pipeline, so that
//	pipe is an alternative-branch wearing the spacing of a shell example.
//
//	The one exemption is HELP SPELLINGS: `gopi -h | --help` and `gopi task
//	-h|--help` name the page's own two doors to the SAME request, glued or
//	spaced, and both spellings are typeable verbatim. This arm is an explicit
//	allow-list of two words, not a heuristic — a pipe between any other flag
//	pair is refused (`--useful|--useless|--unused` is a choice among three
//	MODES, which must be typed one at a time).
//
// AN UNCLOSED BRACKET GROUP is refused too: the page has been read as syntax
// while the closing `]` fell off a wrap, which is the same defect one step
// earlier (the reader cannot tell where the choice ends).
//
// Rule 1/13/14: every column, indent, floor and class name below is a named
// constant, and the class values are one vocabulary. Rule 22/32: no temp root
// is spelled here — this gate reads constants and touches no filesystem.
package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// hsgClass is the five-way classification of an advertised invocation. One
// vocabulary, read by the classifier, the controls and the census.
type hsgClass string

const (
	// hsgTypeable — every token is a literal an operator types as written.
	hsgTypeable = hsgClass("typeable")
	// hsgMetavar — a token is a placeholder (`<task_id>`, `PATH`, `WHO`).
	hsgMetavar = hsgClass("metavariable")
	// hsgOptional — a `[…]` (or `(…)`) group: someone/something must be
	// supplied, and the page delimits the choice.
	hsgOptional = hsgClass("bracket-optional")
	// hsgBranch — an alternative-branch shorthand at depth 0: the defect.
	hsgBranch = hsgClass("alternative-branch")
	// hsgPipe — a worked example piped into a shell program.
	hsgPipe = hsgClass("shell-pipe")
)

const (
	// hsgIndentUnit is the indent of the two-space grid a page's rows are laid
	// out on; a row's DEPTH is its leading-space count over this unit.
	hsgIndentUnit = 2

	// hsgMaxRowDepth is the deepest indent still read as a NEW advertised row.
	// Deeper lines are the value block of the flag above them (measured: the
	// whole G5/G6 family explains each flag's accepted values on its own
	// indented lines, and a `yes|no` there is a VALUE CHOICE, not an
	// invocation). Only rows nested under another ROW are advertised surfaces.
	hsgMaxRowDepth = 2

	// hsgDescriptionCol is the column the description column opens at. It is
	// the same floor the G6 derivation uses (g6DescriptionCol) shifted by the
	// two-space indent this scanner keeps: a run of >= 2 spaces whose END lands
	// at or past it is the column separator, never argv.
	hsgDescriptionCol = 18

	// hsgContinuationIndent is the indent of a WRAPPED synopsis continuation —
	// the same 18 the family's per-group derivations cut their flag sets at. A
	// line at or past it, under a row that had no description column of its own,
	// is that row's invocation continued: it is JOINED before classification, so
	// a choice the page breaks over two lines (`[--latent-candidates |
	// --harvest-latent` / `| --latent-crosscheck …]`) is read as the ONE bracket
	// group it is, not as two half-rows with an unclosed bracket.
	hsgContinuationIndent = 18

	// The non-vacuity floors. They are floors on a measured family, sized well
	// under what the family carries today (measured: >350 rows) so ordinary
	// help churn never trips them, while a broken extractor — which returns
	// near-zero — always does.
	hsgMinRows     = 120
	hsgMinTypeable = 60
	hsgMinMetavar  = 30
	hsgMinOptional = 8
)

// hsgPipeWord is the separator the pages write a worked shell example with.
const hsgPipeWord = "|"

// hsgProgramTailWords are the words a shell pipeline's TAIL may open with: a
// program the operator runs. The list is closed on purpose — a tail that is not
// here and is not a flag is a METAVARIABLE tail (`| PATH`), which the classifier
// refuses, so an unknown program word fails loudly and gets added here rather
// than silently passing.
var hsgProgramTailWords = []string{
	"jq", "grep", "sed", "sort", "head", "tail", "wc", "awk", "cut", "uniq",
	"tr", "xargs", "bash", "sh", "zsh", "fish", "tee", "column", "less",
}

// hsgHelpSpellings are the alternatives a page may name at depth 0 with a pipe:
// the two spellings of the SAME request, both typeable verbatim. This is an
// allow-list of two words (see the file doc), not a heuristic.
var hsgHelpSpellings = []string{"-h", "--help"}

// The line shapes the scanner reads.
var (
	// hsgRowRe opens a new row at the two-space grid: a `gopi …` invocation, or
	// a flag-definition row (`--flag  description`), which opens a SCOPE whose
	// value lines must not be read as invocations.
	hsgRowRe = regexp.MustCompile(`^  (-{1,2}[^ ]|gopi( |$))`)
	// hsgSectionRe is a section header: a bare `Words:` title at column 1.
	hsgSectionRe = regexp.MustCompile(`^[A-Za-z(][^ ].*:$`)
)

// hsgRow is one advertised invocation, with where it was read from.
type hsgRow struct {
	page    string
	section string
	depth   int
	text    string
}

// hsgPage is one help page under this gate: the word that prints it and the
// usage CONSTANT the printer reads (never `-h` output, so the gate is
// in-process and binary-free, exactly like the per-group derivations).
type hsgPage struct {
	name  string
	usage string
}

// hsgFamilyPages returns every page in the family that advertises `gopi …` lines.
// It is a function because three groups expose their page through the SAME
// accessor their own gate uses (G5's g5Surfaces, G6's g6UsagePage, and the
// checkpoint page's checkpointUsage), so this gate cannot drift onto a page those
// groups stopped printing — and cannot MISS one they added to the family, which
// is exactly how the checkpoint page's own `list|export` kept its shorthand: it
// is printed by `gopi session checkpoint -h`, not by the session group page.
func hsgFamilyPages() []hsgPage {
	pages := []hsgPage{
		{name: "top-level", usage: config.FormatUsage()},
		{name: CliVerbTask, usage: tasksUsage},
		{name: CliVerbMemory, usage: memoryUsage},
		{name: CliVerbSession, usage: sessionUsage},
		{name: CliVerbCheckpoint, usage: checkpointUsage},
		{name: CliVerbConfig, usage: configUsage},
	}
	for _, s := range g5Surfaces() {
		pages = append(pages, hsgPage{name: s.Name, usage: s.Help})
	}
	for _, verb := range g6GroupVerbs {
		pages = append(pages, hsgPage{name: verb, usage: g6UsagePage(verb)})
	}
	return pages
}

// hsgPageRows extracts every advertised invocation of one page.
//
// THE EXTRACTOR DISCIPLINE (the one the family's per-group derivations already
// share, stated once here because this gate is the only cross-group reader):
//   - a row opens at the page's row indent (two spaces) with `gopi …` or a flag
//     definition; deeper indents are that row's own value block, so their lines
//     belong to the row that opened the scope and are NOT scanned as rows;
//   - a WRAPPED continuation (indent >= hsgContinuationIndent, under a row with
//     no description column) is JOINED onto the row it continues, with any line
//     joining punctuation kept — the page folds a long synopsis there, and a
//     fold inside a `[…]` group is the difference between the group the page
//     asserts and a phantom unclosed bracket;
//   - the DESCRIPTION column is cut before classification, so prose never
//     contributes argv;
//   - blank lines under a row are that row's layout, not an ended scope
//     (measured: several pages put a blank line between a synopsis and its
//     body, and ending the scope there lost half the family — the memory and G6
//     groups, 128 rows, disappeared);
//   - a section header (column-1 `Words:`) closes any open scope and names the
//     section the following rows are printed under.
//
// A row's OWN value block is excluded by the scope stack rather than by a
// pattern: `--mode chat|web` inside a flag's body is a VALUE CHOICE, not an
// advertised invocation, and the family writes one on every page.
func hsgPageRows(p hsgPage) []hsgRow {
	var out []hsgRow
	section := "Usage"
	// stack[i] is the indent of the open scope at depth i+1, and joinable[i]
	// says whether that scope's row may still be continued.
	var stack []int
	var joinable []bool
	inHead := false // the open scope is a row whose body is running
	// contOK is true only while the line just read was a ROW's own line or its
	// continuation. A wrapped synopsis continuation is the line directly under
	// the line it continues; anything else at a deep indent is that row's BODY
	// PROSE (measured: releasing this and joining by indent alone concatenated
	// the whole `voice_input` flag table onto one synopsis line).
	contOK := false
	for _, raw := range strings.Split(p.usage, "\n") {
		if hsgSectionRe.MatchString(raw) {
			section = strings.TrimSuffix(raw, ":")
			stack, joinable, inHead, contOK = stack[:0], joinable[:0], false, false
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		body := strings.TrimLeft(raw, " ")
		if body == "" {
			// A blank line inside a row's block is layout; the scope it was
			// opened in survives it.
			continue
		}
		if indent >= hsgContinuationIndent && len(stack) > 0 && joinable[len(joinable)-1] &&
			contOK && !hsgRowRe.MatchString(raw) {
			last := &out[len(out)-1]
			last.text = strings.TrimSpace(last.text + " " + body)
			continue
		}
		if !hsgRowRe.MatchString(raw) {
			inHead, contOK = false, false
			// Not a row: body text. It may CLOSE a scope but never open one.
			for len(stack) > 0 && indent < stack[len(stack)-1] {
				stack = stack[:len(stack)-1]
				joinable = joinable[:len(joinable)-1]
			}
			continue
		}
		// A row. Close the scopes it is not nested in, then open its own.
		for len(stack) > 0 && indent <= stack[len(stack)-1] {
			stack, joinable = stack[:len(stack)-1], joinable[:len(joinable)-1]
		}
		depth := len(stack) + 1
		stack = append(stack, indent)
		cut := hsgCutDescription(raw)
		// A row with a description column closes its own sync: the column IS the
		// separator, so what follows it is prose and a deep line under it is
		// that prose, never argv.
		joinable = append(joinable, cut == strings.TrimSpace(raw))
		if depth > hsgMaxRowDepth {
			// The value block of a flag row: not an advertised surface.
			inHead = false
			continue
		}
		if !strings.HasPrefix(body, "gopi") {
			inHead = true // a flag row: opens a scope, advertises no argv
			continue
		}
		if inHead {
			// A `gopi …` line directly under another row is that row's body
			// text, not a new invocation (measured: a synopsis body explains a
			// mode with a `gopi …` sentence).
			contOK = false
			continue
		}
		out = append(out, hsgRow{
			page:    p.name,
			section: section,
			depth:   depth,
			text:    cut,
		})
		// Bare help doors (`gopi session checkpoint -h`) belong to the family's
		// own hierarchy guard, not to a surface an operator types to RUN
		// something: the exit-code runners hold these rows to a 0 exit, and a
		// page's own `-h` prints the page and is asserted elsewhere.
		if isHelpDoorRow(cut) {
			out = out[:len(out)-1]
		}
		contOK = true
	}
	return out
}

// hsgCutDescription cuts a row's trailing DESCRIPTION at the column, then drops
// a shell redirect tail and a folded continuation. Both are layout, not argv:
// the redirect belongs to the SHELL (the page's own examples say so) and the
// fold is how a long synopsis wraps.
func hsgCutDescription(raw string) string {
	run := 0
	cut := len(raw)
	for i := 0; i < len(raw); i++ {
		if raw[i] == ' ' || raw[i] == '\t' {
			run++
			continue
		}
		if run >= 2 && i >= hsgDescriptionCol {
			cut = i - run
			break
		}
		run = 0
	}
	s := strings.TrimRight(raw[:cut], " \t")
	if i := strings.Index(s, " > "); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, "\\")
	return strings.TrimSpace(s)
}

// hsgVerdict is what the classifier decided about one row.
type hsgVerdict struct {
	class  hsgClass
	branch []string // the offending tokens (empty = admitted)
	unclsd bool     // an unclosed bracket group
}

// hsgWords splits an invocation the way the SHELL does, which is the only
// tokenizer that reports the token an operator would actually type: whitespace
// separates words EXCEPT inside a double-quoted span, where a value like
// `"why it failed"` is one word. Without this the reported offending token for
// the reopen page would be a fragment (`failed"|@FILE`), and a gate whose
// message does not name the page's own text is a gate nobody can act on.
func hsgWords(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case !inQuote && (c == ' ' || c == '\t'):
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// hsgClassify reads one invocation's argv and decides whether it is a shape an
// operator could TYPE.
//
// THE CLASS IS THE ROW'S DOMINANT STRUCTURE, in this order: a refusal, then a
// worked shell example piped into a program, then an optional `[…]` group, then
// a metavariable, then a literal. The order is forced by the real lines: the
// `--json | jq` example also carries `<id>`, and `[--mode chat|web]` also
// carries a pipe — the pipe there being INSIDE the group, which is what makes
// it the optional choice rather than an example.
func hsgClassify(text string) hsgVerdict {
	var v hsgVerdict
	words := hsgWords(text)
	depth := 0
	sawOptional, sawMetavar, sawPipe := false, false, false
	for i, w := range words {
		switch {
		case w == hsgPipeWord:
			if depth > 0 {
				break
			}
			// Depth 0: the tail decides. A program word there = a worked
			// example (admitted); a flag tail = an alternative branch wearing
			// the spacing of a shell example (refused).
			next, prev := hsgNeighbour(words, i, 1), hsgNeighbour(words, i, -1)
			if hsgIsHelpPair(prev, next) {
				break
			}
			if hsgIsProgramTail(next) {
				sawPipe = true
				continue
			}
			v.branch = append(v.branch, prev+" "+hsgPipeWord+" "+next)
		case hsgIsGroupToken(w):
			// A `[…]` (or `(…)`) group. Depth rises with the opening brackets
			// and falls with the closing ones; an interior pipe inside a group
			// is the optional choice the page asserts.
			depth += hsgOpenCount(w) - hsgCloseCount(w)
			sawOptional = true
		default:
			// One token. A GLUED alternative (`bash|zsh|fish`) is refused at
			// depth 0 — it is a literal no dispatcher accepts.
			if strings.Contains(w, hsgPipeWord) && depth == 0 && !hsgIsHelpPairInToken(w) {
				v.branch = append(v.branch, w)
				break
			}
			if hsgIsPlaceholder(w) {
				sawMetavar = true
			}
		}
	}
	if depth != 0 {
		v.unclsd = true
	}
	switch {
	case len(v.branch) > 0 || v.unclsd:
		v.class = hsgBranch
	case sawPipe:
		v.class = hsgPipe
	case sawOptional:
		v.class = hsgOptional
	case sawMetavar:
		v.class = hsgMetavar
	default:
		v.class = hsgTypeable
	}
	return v
}

// hsgIsGroupToken reports whether a token carries a GROUP bracket — `[`, `(`
// or `{`. The angle brackets are deliberately NOT group brackets: `<id>` is a
// metavariable, and counting it as an optional group classified the family's
// own `--json | jq` control as a choice instead of an example (measured).
func hsgIsGroupToken(w string) bool {
	return strings.ContainsAny(w, "[](){}")
}

// hsgOpenCount / hsgCloseCount count a token's GROUP brackets.
func hsgOpenCount(w string) int {
	return strings.Count(w, "[") + strings.Count(w, "(") + strings.Count(w, "{")
}

func hsgCloseCount(w string) int {
	return strings.Count(w, "]") + strings.Count(w, ")") + strings.Count(w, "}")
}

// hsgNeighbour returns the token `off` positions away, or "" at the edge.
func hsgNeighbour(words []string, i, off int) string {
	j := i + off
	if j < 0 || j >= len(words) {
		return ""
	}
	return words[j]
}

// hsgIsHelpPair reports whether a spelled pipe joins the two spellings of the
// page's own help door.
func hsgIsHelpPair(a, b string) bool {
	return hsgIsHelpSpelling(a) && hsgIsHelpSpelling(b)
}

// hsgIsHelpSpelling reports whether a token is one of the help spellings.
func hsgIsHelpSpelling(w string) bool {
	for _, h := range hsgHelpSpellings {
		if w == h {
			return true
		}
	}
	return false
}

// hsgIsHelpPairInToken reports whether a GLUED token is nothing but help
// spellings joined by pipes (`-h|--help`).
func hsgIsHelpPairInToken(w string) bool {
	parts := strings.Split(w, hsgPipeWord)
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if !hsgIsHelpSpelling(p) {
			return false
		}
	}
	return true
}

// hsgIsProgramTail reports whether a spelled pipe's tail opens a shell program.
func hsgIsProgramTail(w string) bool {
	for _, p := range hsgProgramTailWords {
		if w == p {
			return true
		}
	}
	return false
}

// hsgIsPlaceholder reports whether a token is a value somebody must supply:
// angle/brace/Lua-bracketed, or an UPPERCASE metavariable (WHO, PATH, FILE,
// ID, X …). A lowercase word is a literal the page means verbatim (hot, warm,
// cold, bash), which is what keeps the classification from swallowing the real
// alternatives as "just placeholders".
func hsgIsPlaceholder(w string) bool {
	w = strings.TrimSuffix(w, ",")
	if w == "" {
		return false
	}
	switch w[0] {
	case '<', '{', '"', '@':
		return true
	}
	for i := 0; i < len(w); i++ {
		c := w[i]
		if c >= 'a' && c <= 'z' {
			return false
		}
		if (c >= 'A' && c <= 'Z') || c == '_' {
			continue
		}
		return false
	}
	// All uppercase (or underscore): a metavariable.
	for i := 0; i < len(w); i++ {
		if w[i] >= 'A' && w[i] <= 'Z' {
			return true
		}
	}
	return false
}

// --- the named controls -------------------------------------------------------

// hsgControl is a named shape whose classification is PINNED. Half of them are
// the family's existing GREEN shapes (so a classifier that refuses everything
// fails), half are the refused shapes (so a classifier that admits everything
// fails).
type hsgControl struct {
	name       string
	text       string
	want       hsgClass
	wantBranch []string
}

// hsgControls are read in BOTH states of the gate — they are the reason a
// broken classifier fails instead of passing empty.
func hsgControls() []hsgControl {
	return []hsgControl{
		{
			// The GREEN control the briefing names first: a worked example whose
			// tail is a shell program (cli_stats.go).
			name: "worked-example-pipe-into-jq",
			text: "gopi session stats <id> --json | jq .prompt_tokens",
			want: hsgPipe,
		},
		{
			// GREEN: the page's own help door, both spellings (main.go).
			name: "help-door-spaced",
			text: "gopi -h | --help",
			want: hsgTypeable,
		},
		{
			// GREEN: the same door as one page writes it (cli_tasks.go).
			name: "help-door-glued",
			text: "gopi task -h|--help",
			want: hsgTypeable,
		},
		{
			// GREEN: an optional group holding a choice (cli_session.go).
			name: "optional-group-mode",
			text: "gopi session list [--mode chat|web] [--source WHO] [--limit N] [--json]",
			want: hsgOptional,
		},
		{
			name: "optional-group-tier",
			text: "gopi memory update <id> [--tier hot|warm|cold] [--topic T]",
			want: hsgOptional,
		},
		{
			name: "optional-group-flag-list",
			text: "gopi memory provenance [--shape|--backfill|--reclassify] [--db PATH]",
			want: hsgOptional,
		},
		{
			// RED: the glued choice of shells (cli_config.go, pre-fix).
			name:       "glued-shell-choice",
			text:       "gopi config completion bash|zsh|fish",
			want:       hsgBranch,
			wantBranch: []string{"bash|zsh|fish"},
		},
		{
			// RED: the glued choice of modes (cli_memory.go, pre-fix).
			name:       "glued-flag-choice",
			text:       "gopi memory feedback <id> --useful|--useless|--unused [--reason TEXT]",
			want:       hsgBranch,
			wantBranch: []string{"--useful|--useless|--unused"},
		},
		{
			// RED: a spaced pipe whose tail is a FLAG, not a program
			// (cli_memory.go's memsync, pre-fix).
			name:       "spaced-pipe-with-flag-tail",
			text:       "gopi memory memsync --export --out patch.json | --import patch.json",
			want:       hsgBranch,
			wantBranch: []string{"patch.json | --import"},
		},
		{
			// RED: a glued alternative whose right side is a FILE spelling
			// (cli_tasks.go's pre-fix synopsis, then on the verb `gopi task
			// clone` carries today).
			name:       "glued-file-choice",
			text:       `gopi task clone <task_id> --reason "why it failed"|@FILE [--db PATH]`,
			want:       hsgBranch,
			wantBranch: []string{`"why it failed"|@FILE`},
		},
		{
			// RED: a metavariable alternative, the spelling a fix is most
			// likely to reach for by accident — `TEXT|@FILE` is still one
			// literal token.
			name:       "glued-metavariable-choice",
			text:       "gopi task clone <task_id> --reason TEXT|@FILE [--db PATH]",
			want:       hsgBranch,
			wantBranch: []string{"TEXT|@FILE"},
		},
		{
			// RED: an optional group that never closes — the same defect one
			// step earlier.
			name:       "unclosed-optional-group",
			text:       "gopi memory provenance [--latent-candidates | --harvest-latent",
			want:       hsgBranch,
			wantBranch: nil,
		},
	}
}

// TestHelpShorthandClassifierControls pins the classifier in both directions.
// Every named control — the family's existing GREEN shapes and every refused
// shape — must classify exactly as named.
func TestHelpShorthandClassifierControls(t *testing.T) {
	for _, c := range hsgControls() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			v := hsgClassify(c.text)
			if v.class != c.want {
				t.Errorf("%q classified %q, want %q (branch=%v unclosed=%v)",
					c.text, v.class, c.want, v.branch, v.unclsd)
			}
			if len(c.wantBranch) != len(v.branch) {
				t.Fatalf("%q named %v as the offending token(s); the classifier said %v",
					c.text, c.wantBranch, v.branch)
			}
			for i := range c.wantBranch {
				if v.branch[i] != c.wantBranch[i] {
					t.Errorf("%q: offending token %d is %q, want %q",
						c.text, i, v.branch[i], c.wantBranch[i])
				}
			}
		})
	}
}

// TestHelpPagesAdvertiseNoBareAlternativeBranch is the cross-group gate: every
// advertised invocation on every page of the family is classified, and a bare
// alternative-branch shorthand at bracket depth 0 (or an unclosed bracket
// group) fails, naming the page, the section and the line an operator would
// have typed.
func TestHelpPagesAdvertiseNoBareAlternativeBranch(t *testing.T) {
	counts := map[hsgClass]int{}
	rows := 0
	var offenders []string
	for _, p := range hsgFamilyPages() {
		for _, r := range hsgPageRows(p) {
			rows++
			v := hsgClassify(r.text)
			counts[v.class]++
			for _, tok := range v.branch {
				offenders = append(offenders, hsgRender(p.name, r, "advertises the alternative-branch shorthand `"+tok+"`"))
			}
			if v.unclsd {
				offenders = append(offenders, hsgRender(p.name, r, "leaves a bracket group unclosed"))
			}
		}
	}

	// NON-VACUITY: a page family this size cannot legitimately scan to nothing,
	// and a class the family is full of cannot legitimately be empty. Without
	// these floors a broken extractor reports zero violations and passes.
	if rows < hsgMinRows {
		t.Fatalf("only %d advertised line(s) were scanned — the extraction is blind "+
			"(floor %d); a gate that scans nothing cannot fail", rows, hsgMinRows)
	}
	for class, floor := range map[hsgClass]int{
		hsgTypeable: hsgMinTypeable,
		hsgMetavar:  hsgMinMetavar,
		hsgOptional: hsgMinOptional,
	} {
		if counts[class] < floor {
			t.Errorf("only %d %s row(s) were classified (floor %d) — the "+
				"classification is not reading the family", counts[class], class, floor)
		}
	}
	t.Logf("scanned %d advertised invocation(s): %s=%d %s=%d %s=%d %s=%d %s=%d",
		rows,
		hsgTypeable, counts[hsgTypeable],
		hsgMetavar, counts[hsgMetavar],
		hsgOptional, counts[hsgOptional],
		hsgPipe, counts[hsgPipe],
		hsgBranch, counts[hsgBranch])

	for _, o := range offenders {
		t.Errorf("%s", o)
	}
	if len(offenders) > 0 {
		t.Errorf("%d advertised invocation(s) are not shapes an operator can type: "+
			"write the choice as a `[…]` group an operator reads, or name ONE spelling "+
			"and describe the others in the description column", len(offenders))
	}
}

// hsgRender formats one offender with the provenance needed to fix it.
func hsgRender(page string, r hsgRow, what string) string {
	return page + " § " + r.section + ": `" + r.text + "` " + what
}

// isHelpDoorRow reports whether a row is a bare help door — the page's own `-h`
// spelled as an invocation. It is classified like any other row (a help door is
// typeable and carries no shorthand), but it is not a surface an operator RUNS,
// and the family's hierarchy guard owns it: keeping it out of the scanned set
// keeps this gate's line count a count of INVOCATIONS.
func isHelpDoorRow(text string) bool {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return false
	}
	last := fields[len(fields)-1]
	return last == "-h" || last == "--help"
}
