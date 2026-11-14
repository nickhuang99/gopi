// cli_tools_maintenance_help_surface_basic_test.go — G6 of the help-surface
// hardening epic (briefings/brief-help-surface-hardening.md).
//
// THE DEFECT CLASS. Not a missing line of code: a surface that is implemented,
// compiled, unit-tested, and never TYPED. `gopi task sync --push-only
// --task-id T` was reported complete and failed on its first real invocation
// (Rule 31). A suite that exercises internals through seams its author chose
// cannot see that class, so every case below drives the REAL BINARY over the
// help page's own argv, in a sandboxed GOPI_HOME, and asserts on the help's
// CLAIM — or on the form's FIRST REFUSAL, with the usage dump proven absent
// (that absence is what catches an argv that fell through to the dispatcher's
// fallback, the 2026-09-07 rounding bug's signature).
//
// THE SURFACE IS DERIVED, NOT AUTHORED. Three pieces, and they must agree:
//
//  1. scripts/g6-tools-maintenance-help-surface.sh + g6-help-forms.awk +
//     g6-top-level.awk derive every advertised form from the live `-h` pages.
//  2. devdoc/gopi-tools-maintenance-help-surface-inventory.md is that
//     derivation, checked in with its generating command.
//  3. g6DeriveSurface() below re-derives the same list IN-PROCESS from the
//     usage CONSTANTS the help printer itself reads, and
//     TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation compares (2)
//     against (3) in both directions — so a table that stopped matching the
//     help goes red instead of going stale.
//
// Rule 17: the in-process halves use t.Setenv where they set anything at all.
// Every temp path comes from t.TempDir() (the harness scratch root, Rule 22) —
// this file names no temp root of its own, and no DB path (Rule 32): the probes
// run the binary against a sandbox GOPI_HOME and never touch the operator's.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/toolnames"
)

// --- the group, and the usage pages it is derived from ------------------------

// g6TopLevelVerb and g6TopLevelFlagsVerb name the two pseudo-verbs the
// top-level page contributes: its Usage/Inputs/Commands blocks are invocation
// FORMS, its Options block is a flag TABLE (a different claim — "this spelling
// is admitted", graded by the sweep, not by a per-form invocation).
const (
	g6TopLevelVerb      = "(top-level)"
	g6TopLevelFlagsVerb = "(top-level flags)"
)

// g6GroupVerbs is the G6 group in the order the top-level help lists it. It
// must equal the G6_VERBS list in the derivation script; the gate below
// compares them and fails when one gains a verb the other lacks.
var g6GroupVerbs = []string{
	toolnames.ToolVisionAnalyze,
	toolnames.ToolText2img,
	toolnames.ToolScreenCapture,
	CliVerbAct,
	toolnames.ToolVoiceInput,
	CliVerbShot,
	CliVerbCodeSearch,
	CliVerbArchive,
	CliVerbWorker,
	CliVerbConfig,
	CliVerbCompletion,
	CliVerbScratchGC,
	CliVerbVersion,
	g6TopLevelVerb,
	g6TopLevelFlagsVerb,
}

// g6UsagePage returns the page a verb's `-h` prints, read from the usage
// CONSTANT the printer reads. Two verbs render through a function over such a
// constant (archive, and scratch-gc's fingerprint+tail); one constant is named
// for its first line (scratchGCUsageFingerprint). Reading the constant is what
// makes the derivation cheap and binary-free — and
// TestToolsMaintenanceHelpSurfacePagesComeFromTheirOwnConstant pins that the
// constant really is the page the binary prints, so this shortcut cannot
// silently grade the wrong text.
func g6UsagePage(verb string) string {
	switch verb {
	case toolnames.ToolVisionAnalyze:
		return visionUsage
	case toolnames.ToolText2img:
		return text2imgUsage
	case toolnames.ToolScreenCapture:
		return screenCaptureUsage
	case CliVerbAct:
		return actUsage
	case toolnames.ToolVoiceInput:
		return voiceInputUsage
	case CliVerbShot:
		return shotUsage
	case CliVerbCodeSearch:
		// The RENDERED page: this constant is a format string, and the -h
		// output (what the derivation reads) carries the real exit codes.
		return codeSearchUsageText()
	case CliVerbArchive:
		return archiveUsage()
	case CliVerbWorker:
		return workerUsageHead
	case CliVerbConfig:
		return configUsage
	case CliVerbCompletion:
		return completionUsage
	case CliVerbScratchGC:
		return scratchGCUsage
	case CliVerbVersion:
		return versionUsage
	case g6TopLevelVerb, g6TopLevelFlagsVerb:
		return config.FormatUsage()
	}
	return ""
}

// g6DeriveSurface is the authoritative in-process derivation: pseudo-verb →
// ordered advertised forms. It mirrors the awk scanners' three rules —
// per-LINE (a description block is not a terminator), prose cut at the first
// >= 2-space run whose END column is at or past g6DescriptionCol, and the page
// title skipped — and the gate below is what keeps the two implementations
// honest about each other.
func g6DeriveSurface() map[string][]string {
	out := map[string][]string{}
	for _, verb := range g6GroupVerbs {
		if verb == g6TopLevelVerb {
			out[verb] = g6DeriveTopLevelForms(g6UsagePage(verb))
			continue
		}
		if verb == g6TopLevelFlagsVerb {
			out[verb] = g6DeriveTopLevelFlags(g6UsagePage(verb))
			continue
		}
		out[verb] = g6DeriveForms(verb, g6UsagePage(verb))
	}
	return out
}

// g6DeriveForms extracts the advertised forms of ONE verb page: every line
// starting with `gopi `, prose cut, the page title skipped.
//
// THE PAGE IS READ THROUGH ITS DOOR, AND ITS FLAT SPELLINGS ARE RE-DERIVED
// (verb-retirement phase 1, 2026-10-06). The group's verbs were removed from
// the top-level dispatch chain: `gopi act -h` is refused by name now and the
// page is printed only as `gopi tool act -h`. The page's own text is
// nonetheless still FLAT in places, and each place has to be read for what it
// is, not rewritten wholesale:
//
//   - the page TITLE is `gopi <verb> — <prose>` whatever address printed it,
//     because the function that renders it names the verb, not the door. It is
//     a page name and not a FORM (the awk scanner's rule (c)) — an earlier
//     revision of this derivation listed these as forms, which is how seven em
//     dash rows came to sit in the inventory table;
//   - the one-line page shape (`Usage: gopi version`) declares the same title
//     after `Usage: `, so the same skip must apply to that spelling too;
//   - EVERY OTHER `gopi <verb> …` line on the page is a stale spelling of a
//     form whose live address is the door, so it is REWRITTEN to the door
//     spelling. Two sites in the tree spell it flat today (the fixture tree
//     listings on the family-census page, whose text is assembled from the
//     page's own verb word) and the operator's ruling on the same shape in the
//     refusal vocabulary applies here: a retired flat spelling is not a form
//     this binary dispatches, so listing it would put a copy-pasteable
//     invocation in the table that refuses when typed.
//
// The rewrite is g6DoorSpelling, and it is OFF for a verb with no door (merge,
// worker, config, version): those pages are reached by their own word and their
// forms are already the flat ones, so there is nothing to re-derive — which is
// why the door is looked up rather than assumed from the verb's family.
func g6DeriveForms(verb, page string) []string {
	var out []string
	seen := map[string]bool{}
	for _, ln := range strings.Split(page, "\n") {
		if g6PageTitleRe.MatchString(ln) {
			continue // the page title names the page; it is not a form
		}
		// A one-line verb page declares the same title after "Usage: " rather
		// than at the start of the line (measured on `gopi version -h`), so
		// that shape needs its own rule — for the FORM it names, and for the
		// title it may be naming instead.
		if strings.HasPrefix(ln, "Usage: gopi ") {
			ln = strings.TrimPrefix(ln, "Usage: ")
			if g6PageTitleRe.MatchString(ln) {
				continue
			}
		}
		if !strings.HasPrefix(strings.TrimLeft(ln, " \t"), "gopi ") {
			continue
		}
		form := g6DoorSpelling(verb, g6StripFold(g6CutProse(ln)))
		if form == "" || seen[form] {
			continue
		}
		seen[form] = true
		out = append(out, form)
	}
	return out
}

// g6PageTitleRe is a page's TITLE line: the program word, one or two command
// words, an em dash and prose — `gopi text2img — render …` (a verb reached by
// its own word) or `gopi tool text2img — render …`, `gopi task archive — …`,
// `gopi config completion — …` (a verb reached through a door). The door was
// introduced by the verb-retirement epic and the second word is why the pattern
// is `(word )?word` rather than `word`: an earlier revision matched only the
// flat shape, so seven titles sat in the derived surface as if they were
// invocations, and the STALE branch of the inventory gate asked the table to
// list a page name as a form.
var g6PageTitleRe = regexp.MustCompile(`^gopi ([a-zA-Z0-9_-]+ )?[a-zA-Z0-9_-]+ — `)

// g6DoorAddress is the argv that PRINTS one verb's help page: the door word plus
// the verb, or the verb alone when it has no door. It is the Go half of the
// derivation script's G6_DOOR_* table — the two must agree, and
// TestG6DumpDerivation is the reconciliation aid that diffs them.
//
// g6Doors is the ONE definition of the mapping, and a verb absent from it is
// reached by its own word. The map is keyed by the verb, matching g6GroupVerbs.
var g6Doors = map[string]string{
	toolnames.ToolVisionAnalyze: CliVerbTool,
	toolnames.ToolText2img:      CliVerbTool,
	toolnames.ToolScreenCapture: CliVerbTool,
	CliVerbAct:                  CliVerbTool,
	toolnames.ToolVoiceInput:    CliVerbTool,
	CliVerbShot:                 CliVerbTool,
	CliVerbCodeSearch:           CliVerbTool,
	CliVerbArchive:              CliVerbTask,
	CliVerbCompletion:           CliVerbConfig,
	CliVerbScratchGC:            CliVerbTool,
}

// g6DoorSpelling rewrites a flat `gopi <verb> …` form to the door spelling a
// reader must type — `gopi tool act …` — and leaves every other line alone. A
// form already carrying the door, a verb with no door, and a line that is not
// this verb's flat spelling all pass through untouched: the rewrite is keyed on
// the verb's OWN word, so a page's example that invokes a DIFFERENT verb (the
// act page shows `gopi tool screen_capture --out before.png`) is never mistaken
// for one of its own stale spellings.
func g6DoorSpelling(verb, form string) string {
	door := g6Doors[verb]
	if door == "" {
		return form
	}
	flat := gopiProgramWord + " " + verb
	if form != flat && !strings.HasPrefix(form, flat+" ") {
		return form
	}
	return gopiProgramWord + " " + door + " " + verb + strings.TrimPrefix(form, flat)
}

// g6CutProse cuts the trailing DESCRIPTION from one form line at the first run
// of >= 2 spaces whose END column is at or past g6DescriptionCol, ignoring the
// leading indent. See g6-help-forms.awk for why that is the rule and how both
// simpler drafts were measured wrong against the live pages.
func g6CutProse(s string) string {
	s = strings.TrimLeft(s, " \t")
	run := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			run++
			continue
		}
		if run >= 2 && i >= g6DescriptionCol {
			return s[:i-run]
		}
		run = 0
	}
	return s
}

// g6DescriptionCol is the floor of the help printer's computed description
// column (internal/config helpColumn floors at 16 for its narrowest block), so
// no description on any G6 page opens before it. When that floor moves, this
// constant and the awk scanners' 16 move together — the gate compares their
// outputs, so a one-sided change fails rather than diverging quietly.
const g6DescriptionCol = 16

// g6StripFold drops trailing space and a folded shell continuation.
func g6StripFold(s string) string {
	return strings.TrimSuffix(strings.TrimRight(s, " \t"), " \\")
}

// g6DeriveTopLevelForms reads the top-level page's Usage, Commands and Inputs
// blocks. An ENTRY opens at exactly two spaces of indent; anything deeper is
// that entry's continuation prose (measured: the Inputs block explains a shape
// over three extra lines, and a wrapped continuation beginning "gopi ..." was
// read as a form until the indent rule was added).
func g6DeriveTopLevelForms(page string) []string {
	var out []string
	seen := map[string]bool{}
	block := ""
	for _, ln := range strings.Split(page, "\n") {
		switch {
		case strings.HasPrefix(ln, "Usage:"):
			block = "usage"
			continue
		case strings.HasPrefix(ln, "Inputs:"):
			block = "inputs"
			continue
		case strings.HasPrefix(ln, "Commands"):
			block = "commands"
			continue
		case strings.HasPrefix(ln, "Options"):
			block = "options"
			continue
		case len(ln) > 0 && ln[0] >= 'A' && ln[0] <= 'Z':
			block = ""
			continue
		}
		if block != "usage" && block != "inputs" && block != "commands" {
			continue
		}
		if !strings.HasPrefix(ln, "  ") || strings.HasPrefix(ln, "   ") {
			continue
		}
		entry := g6StripFold(g6CutProse(ln))
		if entry == "" {
			continue
		}
		form := entry
		if block == "inputs" {
			form = "gopi " + entry // the Inputs rows are positional SHAPES
		} else if !strings.HasPrefix(entry, "gopi ") {
			continue
		}
		if seen[form] {
			continue
		}
		seen[form] = true
		out = append(out, form)
	}
	return out
}

// g6DeriveTopLevelFlags reads the top-level page's Options block: one row per
// `--flag`, rendered as `gopi --flag` so the row is a copy-pasteable spelling
// and the sweep below can feed it to the parser.
func g6DeriveTopLevelFlags(page string) []string {
	var out []string
	seen := map[string]bool{}
	block := ""
	for _, ln := range strings.Split(page, "\n") {
		switch {
		case strings.HasPrefix(ln, "Usage:"), strings.HasPrefix(ln, "Inputs:"), strings.HasPrefix(ln, "Commands"):
			block = ""
			continue
		case strings.HasPrefix(ln, "Options"):
			block = "options"
			continue
		case len(ln) > 0 && ln[0] >= 'A' && ln[0] <= 'Z':
			block = ""
			continue
		}
		if block != "options" || !strings.HasPrefix(ln, "  --") {
			continue
		}
		fields := strings.Fields(strings.TrimLeft(ln, " "))
		name := strings.TrimPrefix(fields[0], "--")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, "gopi --"+name)
	}
	return out
}

// g6FormFlags returns the `--flags` spelled on one form, sorted — the claim
// graded by the admission sweep.
func g6FormFlags(form string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(form, -1) {
		f := strings.TrimPrefix(m, "--")
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// --- the gate: the checked-in table must equal the derivation ----------------

// g6InventoryRowRe matches one inventory table row, `| <n> | <verb> | <form> |`.
// The FORM is captured — the third cell — not the first backticked cell in the
// row (which is the verb), and not the fourth (which is the status prose).
var g6InventoryRowRe = regexp.MustCompile("(?m)^\\| *[0-9]+ *\\| *`[^`]+` *\\| *`([^`]+)` *\\|")

// TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation protects the
// deliverable. The inventory is a DOCUMENT a reader believes; a table listing a
// form the help no longer prints (or missing one it does) describes last
// month's binary. Both directions, every group verb.
func TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation(t *testing.T) {
	path := filepath.Join(repoRoot(t), "devdoc", "gopi-tools-maintenance-help-surface-inventory.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the inventory (the deliverable this gate protects): %v", err)
	}
	inTable := map[string]bool{}
	for _, m := range g6InventoryRowRe.FindAllStringSubmatch(string(b), -1) {
		inTable[strings.TrimSpace(m[1])] = true
	}

	derived := g6DeriveSurface()
	total := 0
	for _, verb := range g6GroupVerbs {
		forms := derived[verb]
		if len(forms) == 0 {
			t.Errorf("%s: the derivation found no advertised form — the page is unreadable, so a green run proves nothing", verb)
			continue
		}
		total += len(forms)
		for _, f := range forms {
			if !inTable[f] {
				t.Errorf("`%s` is advertised by the help (%s) but the inventory does not list it — re-run "+
					"scripts/g6-tools-maintenance-help-surface.sh ./bin/gopi and regenerate the table", f, verb)
			}
		}
	}
	// The reverse direction: a table row no page prints any more. Rows of the
	// findings/gaps tables are prose, so only rows that LOOK like a `gopi …`
	// form are graded this way.
	for form := range inTable {
		if !strings.HasPrefix(form, "gopi ") {
			continue
		}
		if !g6IsDerivedForm(derived, form) {
			t.Errorf("the inventory lists %q, which no help page prints any more — stale row", form)
		}
	}
	// The floor is well under the measured surface (147 rows / 143 distinct on
	// 2026-10-01, because a page may echo another page's example), so it stays a
	// floor: a scanner regression reports fewer and must fail, not pass quietly,
	// while the surface's ordinary growth does not have to touch this number.
	if total < 115 {
		t.Fatalf("the derivation extracted only %d advertised forms (measured surface: 147 rows) — the scanner is broken", total)
	}
}

// g6IsDerivedForm reports whether any derived page prints the form. The
// top-level block renders its positional shapes as `gopi <shape>` and its
// options as `gopi --<flag>`, so the two pseudo-verbs are consulted too.
func g6IsDerivedForm(derived map[string][]string, form string) bool {
	for _, forms := range derived {
		for _, f := range forms {
			if f == form {
				return true
			}
		}
	}
	return false
}

// TestToolsMaintenanceHelpSurfacePagesComeFromTheirOwnConstant pins the
// in-process shortcut: the constant g6UsagePage returns must be the page the
// REAL binary prints, or every derivation above grades the wrong text. It is
// also where the one page whose first line is not its own title (`gopi version`
// opens "Usage: gopi version") is caught if a page is retitled.
func TestToolsMaintenanceHelpSurfacePagesComeFromTheirOwnConstant(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	for _, verb := range g6GroupVerbs {
		if verb == g6TopLevelVerb || verb == g6TopLevelFlagsVerb {
			continue
		}
		page := g6UsagePage(verb)
		if page == "" {
			t.Errorf("%s: no usage page resolvable from a constant", verb)
			continue
		}
		out, _ := g6Probe(t, home, bin, verb, "-h")
		want := strings.TrimSpace(strings.SplitN(page, "\n", 2)[0])
		got := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		if got != want {
			t.Errorf("`gopi %s -h` opened with %q but this file derives the surface from a constant opening %q — "+
				"the gate would be grading the WRONG page", verb, got, want)
		}
	}
}

// --- the PNG fixture (dependency-free: no image library, no external file) ----

// g6WriteFixturePNG writes a VALID 8x8 PNG. The vision and screen forms
// advertise image inputs, so a bogus file would be refused for the wrong reason
// before the claim under test is reached.
func g6WriteFixturePNG(t *testing.T, path string) {
	t.Helper()
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	png = append(png, g6PNGChunk("IHDR", g6BE32(8, 8, 8, 2, 0, 0, 0))...)
	var raw []byte
	for y := 0; y < 8; y++ {
		raw = append(raw, 0) // the scanline filter byte
		for x := 0; x < 8; x++ {
			raw = append(raw, 0xff, 0x00, 0x00)
		}
	}
	png = append(png, g6PNGChunk("IDAT", g6ZlibStored(raw))...)
	png = append(png, g6PNGChunk("IEND", nil)...)
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatalf("write PNG fixture: %v", err)
	}
}

func g6BE32(vals ...uint32) []byte {
	var out []byte
	for _, v := range vals {
		out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
	return out
}

func g6PNGChunk(kind string, data []byte) []byte {
	out := append(g6BE32(uint32(len(data))), kind...)
	out = append(out, data...)
	return append(out, g6BE32(g6CRC32(append([]byte(kind), data...)))...)
}

// g6ZlibStored wraps raw bytes as a zlib stream of STORED deflate blocks
// (BTYPE=00) — valid, compressor-free, and all a fixture needs.
func g6ZlibStored(raw []byte) []byte {
	out := []byte{0x78, 0x01} // zlib header, no preset dictionary
	for len(raw) > 0 {
		n := len(raw)
		if n > 0xffff {
			n = 0xffff
		}
		final := byte(0)
		if n == len(raw) {
			final = 1
		}
		out = append(out, final, byte(n), byte(n>>8), byte(^uint16(n)), byte(^uint16(n)>>8))
		out = append(out, raw[:n]...)
		raw = raw[n:]
	}
	// The Adler-32 trailer is written by the caller's own decoder path when the
	// stream is consumed; a stored-block stream is self-delimiting, so the
	// checksum below is the one for the EMPTY input and is never reached.
	return append(out, g6BE32(1)...)
}

var g6CRC32Table = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		c := uint32(i)
		for k := 0; k < 8; k++ {
			if c&1 != 0 {
				c = 0xedb88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		table[i] = c
	}
	return table
}()

func g6CRC32(b []byte) uint32 {
	c := ^uint32(0)
	for _, x := range b {
		c = g6CRC32Table[byte(c)^x] ^ (c >> 8)
	}
	return ^c
}

// --- the admission sweep: an advertised flag is obeyed or refused BY NAME ----

// TestToolsMaintenanceHelpSurfaceAdvertisedFlagsAreAdmitted is the
// help-vs-parser gate (the G1 defect class: `gopi task sweep-worktrees --json`
// was printed by the help and refused by the flag table). Flags are fed to the
// verb's OWN in-process parser where one is callable, so this runs without a
// binary and cannot be defeated by a probe timing out. A refusal for a
// DIFFERENT reason is fine — the question is only whether the spelling is known.
func TestToolsMaintenanceHelpSurfaceAdvertisedFlagsAreAdmitted(t *testing.T) {
	probe := map[string]string{
		"prompt": "describe", "out": filepath.Join(t.TempDir(), "out.png"), "size": "24",
		"cols": "40", "lines": "10", "lh": "32", "display": ":1", "button": "1",
		"text": "hi", "key": "return", "chunk": "5", "device": "default", "seconds": "1",
		"addr": "127.0.0.1:1", "server-url": "http://127.0.0.1:1", "root": ".",
		"url": "http://127.0.0.1:1", "wait-selector": "#x", "viewport": "800x600",
		"settings": filepath.Join(t.TempDir(), "none.json"), "listen": ":0",
		"detail": "d", "reason": "hand-mistake", "branch": "b", "repo": ".",
	}
	derived := g6DeriveSurface()
	scanned := 0
	for _, verb := range g6GroupVerbs {
		if verb == g6TopLevelVerb || verb == g6TopLevelFlagsVerb {
			continue
		}
		for _, form := range derived[verb] {
			// A page may carry an EXAMPLE that invokes a DIFFERENT verb (the act
			// page shows `gopi tool screen_capture --out …`), so the parser that
			// grades a flag is the one of the verb the FORM names — not of the
			// page it was printed on.
			parse, args := g6ParserFor(g6FormCommand(form))
			if parse == nil {
				continue
			}
			for _, f := range g6FormFlags(form) {
				scanned++
				a := append([]string{}, args...)
				if v, ok := probe[f]; ok {
					a = append(a, "--"+f, v)
				} else {
					a = append(a, "--"+f)
				}
				if err := parse(a); err != nil && strings.Contains(err.Error(), "unknown flag --"+f) {
					t.Errorf("`gopi %s --%s` is REFUSED by the parser while %q advertises it: %v\n"+
						"An advertised flag is either obeyed or refused by NAME (Rule 15).", g6FormCommand(form), f, form, err)
				}
			}
		}
	}
	if scanned < 20 {
		t.Fatalf("the sweep inspected only %d advertised flags — the form scanner is reading blind, so a green run proves nothing", scanned)
	}
}

// g6FormCommand is the command word a form invokes: the first word after the
// program word that NAMES one of this group's verbs, falling back to the second
// word when none does (a top-level form). The door is skipped by that rule
// rather than by a list — `gopi tool act move 400 300` names `act`,
// `gopi task archive <branch> …` names `archive` — so a page's example that
// invokes a SIBLING verb is still graded by the sibling's own parser (the act
// page shows `gopi tool screen_capture --out before.png`), and a verb reached by
// its own word (`gopi merge --fast`) keeps that word. The pseudo-verbs map back
// to the top-level page.
func g6FormCommand(form string) string {
	fields := strings.Fields(form)
	if len(fields) < 2 {
		return ""
	}
	if fields[1] == "-h" || strings.HasPrefix(fields[1], "--") {
		return g6TopLevelVerb
	}
	// Bounded at four words: a form is `gopi [door] <verb> …`, so the verb is
	// the second or the third word and nothing deeper is worth reading.
	for i := 2; i < len(fields) && i <= 3; i++ {
		if w := strings.Trim(fields[i], "<>"); g6IsGroupVerb(w) {
			return w
		}
	}
	return strings.TrimPrefix(fields[1], "<")
}

// g6IsGroupVerb reports whether a word is one of the group's own verb words
// (never a pseudo-verb: those are pages, not commands).
func g6IsGroupVerb(word string) bool {
	for _, v := range g6GroupVerbs {
		if v == word {
			return true
		}
	}
	return false
}

// g6ParserFor returns a verb's own argv parser (nil when it parses through a
// path this file cannot call in-process) plus the leading positional it needs.
func g6ParserFor(verb string) (func([]string) error, []string) {
	switch verb {
	case CliVerbAct:
		return func(a []string) error { _, err := parseActArgs(a); return err }, []string{"info"}
	case CliVerbShot:
		return func(a []string) error { _, err := parseShotArgs(a); return err }, []string{"--self"}
	case toolnames.ToolVoiceInput:
		return func(a []string) error { _, err := parseVoiceArgs(a); return err }, nil
	case toolnames.ToolScreenCapture:
		return func(a []string) error { _, _, err := parseScreenCaptureArgs(a); return err }, nil
	case toolnames.ToolText2img:
		return func(a []string) error { _, err := parseText2imgArgs(a); return err }, []string{"in.txt"}
	case CliVerbCodeSearch:
		return func(a []string) error { _, err := parseCodeSearchArgs(a); return err }, []string{"Sym"}
	case CliVerbConfig:
		return func(a []string) error { _, err := parseConfigShowFlags(a); return err }, nil
	}
	return nil, nil
}

// --- the probes: real binary, help's own argv, real assert --------------------

// g6Probe runs the real binary in a sandboxed home and returns its combined
// output and exit code. It never fails on a non-zero exit — the refusal IS the
// observation for half of these forms — but it DOES fail on a timeout, which is
// the signature of an argv that fell through to the agent loop.
func g6Probe(t *testing.T, home, bin string, args ...string) (string, int) {
	t.Helper()
	return g6RunCommand(t, g6Command(bin, args...), args, 0)
}

// g6Command builds one probe command; the caller sets its environment.
func g6Command(bin string, args ...string) *exec.Cmd {
	return exec.Command(bin, familyAddressArgs(args)...)
}

// The two SHAPES a probe's output can have when a run is killed at the wall
// clock, measured on the real binary rather than assumed:
//
//   - An argv that fell through to the AGENT LOOP prints NOTHING on stdout and
//     no verb line at all — the run stops at the client constructor (or hangs
//     in the provider handshake), so the verb's own output never starts.
//   - An argv that DID reach its verb prints that verb's line. A declared-trees
//     sweep prints `role=cli-trees … tree=<name>` once per tree and reports
//     `removed=<n>` for a tree that exists, which is why the arm can be slow
//     without being misrouted: its duration is a property of the TREES, not of
//     its routing.
//
// The two constants below are what makes the timeout message name the right
// one, so a slow arm is no longer reported as a routing bug.
const (
	// g6VerbLineToken is the stable head of a scratch-gc output line (the
	// role=<arm> field) — the mark of a probe that DID reach its verb.
	g6VerbLineToken = "role="
	// g6GoalNone is the Goal of a probe that must therefore END at its own
	// bound: the reported goal is the timeout itself (the argv never produced
	// the verb's output).
	g6GoalNone = "none (no verb output — the argv never reached the verb)"
)

// g6Output collects a probe's stdout+stderr incrementally, so a probe killed at
// its bound can be diagnosed from what it HAD printed by then. This is the whole
// reason the two timeout shapes are separable: exec's copy goroutines write here
// as the data arrives, while a CombinedOutput-style read would only produce
// anything after the process exits — i.e. exactly nothing, on the timeout path
// that needs it.
type g6Output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *g6Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *g6Output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// g6RunCommand runs a prepared probe and classifies a wall-clock expiry into the
// two shapes above, naming which one happened. bound is the probe's own budget
// (cliContractTimeout for the untyped forms; a typed case's own budget, e.g. the
// declared-trees arm's) — it is the ONE number the diagnostic prints.
func g6RunCommand(t *testing.T, cmd *exec.Cmd, args []string, bound time.Duration) (string, int) {
	t.Helper()
	if bound <= 0 {
		bound = cliContractTimeout
	}
	sink := &g6Output{}
	cmd.Stdout = sink
	cmd.Stderr = sink
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("`gopi %s`: start: %v", strings.Join(args, " "), err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return g6ClassifyExit(t, args, err, sink.String())
	case <-time.After(bound):
		_ = cmd.Process.Kill() //stylegate:ignore kills the child process the test spawned and is tearing down
		<-done                 // the copy goroutines have drained: the buffer is final
		out := sink.String()
		t.Fatalf("`gopi %s`: TIMEOUT — %s\n%s", strings.Join(args, " "),
			g6TimeoutDiagnosis(out, bound, time.Since(started)), g6Head(out))
	}
	return "", 0 // unreachable: both branches above return or fail
}

// g6ClassifyExit turns a finished probe's Wait error into the exit code the
// cases assert on.
func g6ClassifyExit(t *testing.T, args []string, err error, out string) (string, int) {
	t.Helper()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("`gopi %s`: %v\n%s", strings.Join(args, " "), err, out)
		}
		code = ee.ExitCode()
	}
	return out, code
}

// g6TimeoutDiagnosis separates the two things a wall-clock expiry can mean, off
// the probe's OWN output (never off an assumption): a probe that reached its
// verb has ALREADY printed its `role=` line, so the finding is about the FIXTURE
// (it measured trees that were slow); a probe with no such line has the
// fallthrough SIGNATURE, which is the failure this suite's bound exists for.
//
// The no-output branch names its own second reading too, because "no line yet"
// is not proof of nothing: a bound set (through the diagnostic override) below
// the time the verb needs to print its FIRST line kills the probe before any
// evidence exists, and that is a statement about the BOUND, not about the
// routing. Measured: with the override at 10ms against this fixture, the child
// is killed before the first line is flushed, and the honest answer is the
// second reading — a diagnosis that called that a fallthrough would be inventing
// the very misdiagnosis this split removes.
func g6TimeoutDiagnosis(out string, bound, elapsed time.Duration) string {
	if strings.Contains(out, g6VerbLineToken) {
		return fmt.Sprintf("SLOW arm (the probe reached its verb — its own %s line is in the buffer — and its "+
			"budget of %v expired at %v) — %s", g6VerbLineToken, bound,
			elapsed.Truncate(time.Millisecond), g6GoalSlowArm)
	}
	return fmt.Sprintf("NO VERB OUTPUT within the %v bound (the argv never printed a %s line, goal=%q). "+
		"Two readings, and the bound is what separates them: the argv FELL THROUGH to the agent loop (the "+
		"signature this bound exists for — no verb output at all), or the probe was killed BEFORE its verb's "+
		"first line was flushed, which a bound below the verb's own startup cannot tell apart from the first",
		bound, g6VerbLineToken, g6GoalNone)
}

// g6GoalSlowArm is the Goal line a slow-arm expiry reports: the arm's own
// advertised budget IS the finding, and the remediation is the fixture the
// caller owns rather than a wider bound.
const g6GoalSlowArm = "the arm exceeds its own advertised bound on this host; " +
	"either the trees it was pointed at are the operator's REAL ones (the caller must pin the fixture) " +
	"or the arm itself regressed"

// g6AssertReachedVerb is the assertion every refusal goes through: the probe
// must NOT have printed the verb's usage page as its own answer. A misrouted
// argv prints a help page and exits 0/1, which every "it refused" assertion
// accepts by accident — this is what makes the refusal an observation.
//
// THE EXEMPTION IS THE VERB'S OWN REFUSAL PREFIX, AND IT CARRIES THE DOOR. A
// verb whose page a refusal quotes opens that refusal with its dispatch name
// (`gopi task archive: unknown --reason …`), so the prefix that marks "this run
// reached the verb" is the door spelling, not the bare word — a check written
// against the bare word reads a verb that answered correctly as misrouted
// (measured on the archive case after the verb-retirement epic moved the verb
// under `gopi task`, and on the tool family before it). Both spellings are
// accepted: the flat one is what a verb with no door prints, and the door is
// looked up rather than guessed (Rule 13/14 — g6Doors is the ONE definition).
func g6AssertReachedVerb(t *testing.T, verb, out string) {
	t.Helper()
	head := strings.TrimSpace(strings.SplitN(g6UsagePage(verb), "\n", 2)[0])
	if head == "" {
		return
	}
	trimmed := strings.TrimSpace(out)
	if !strings.Contains(out, head) {
		return
	}
	for _, prefix := range g6VerbRefusalPrefixes(verb) {
		if strings.HasPrefix(trimmed, prefix) {
			return
		}
	}
	t.Errorf("%s: the probe printed its USAGE PAGE instead of reaching the verb (argv misrouted):\n%s", verb, g6Head(out))
}

// g6VerbRefusalPrefixes lists the prefixes a run that REACHED the verb opens
// its diagnostics with: `gopi <verb>:` and, when the verb has one, the door
// spelling `gopi <door> <verb>:`.
func g6VerbRefusalPrefixes(verb string) []string {
	out := []string{gopiProgramWord + " " + verb + ":"}
	if door := g6Doors[verb]; door != "" {
		out = append(out, gopiProgramWord+" "+door+" "+verb+":")
	}
	return out
}

// g6Head renders the first lines of a probe output for a failure message.
func g6Head(out string) string {
	lines := strings.SplitN(strings.TrimSpace(out), "\n", 6)
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}

// --- one case per advertised form (tested forms) -----------------------------

// g6BlockedForms is the honest half: the advertised forms this suite CANNOT
// drive locally, each with the blocker that makes a mock meaningless. They are
// listed, never mocked — an invented peer/device/browser would make the
// coverage claim a fiction.
var g6BlockedForms = map[string]string{
	// the tools-from-the-shell family: the device/network half
	"gopi tool shot --self && gopi tool vision_analyze <out.png>":                     "a chained shell example, not a form of one verb; needs a Chromium render (see the --self row)",
	"gopi tool shot --url https://example.com":                                        "needs outbound network + a real Chromium render",
	"gopi tool shot --url http://x --wait-selector \"#chat-log\"":                     "needs a live page carrying the selector",
	"gopi tool shot --self --viewport 1440x900 --out web.png":                         "needs the web UI served AND a Chromium render",
	"gopi tool voice_input --server --listen :7788":                                   "opens a listener and downloads the speech model",
	"gopi tool voice_input --server --stdio":                                          "needs the speech engine model on disk",
	"gopi tool voice_input --server --stdio < clip.pcm":                               "needs the speech engine model on disk",
	"gopi tool voice_input <audio-file> [--chunk N]":                                  "transcribes only with the speech model available",
	"gopi tool voice_input talk.wav":                                                  "transcribes only with the speech model available",
	"gopi tool voice_input long-interview.wav --chunk 15":                             "transcribes only with the speech model available",
	"gopi tool voice_input --client [--device ID] [--seconds N]":                      "needs a real capture device (pulse/alsa)",
	"gopi tool voice_input --client --seconds 8":                                      "needs a real capture device (pulse/alsa)",
	"gopi tool voice_input --client --seconds 8 --out nick.wav":                       "needs a real capture device (pulse/alsa)",
	"gopi tool voice_input --client --seconds N --stdio":                              "needs a real capture device (pulse/alsa)",
	"gopi tool voice_input --client --seconds 5 --stdio":                              "needs a real capture device (pulse/alsa)",
	"gopi tool voice_input --client --server-url http://192.168.7.6:7788 --seconds 8": "needs a peer voice server on the LAN",
	// the maintenance verbs: destructive host actions. `merge` HAD seven rows here
	// and left with its command surface (epic merge-removal, item 2): a blocked
	// form is "a form the help advertises that this suite will not drive", and
	// `gopi help` advertises no merge form any more. Keeping them would be the
	// stale claim the reverse direction of this gate is written to catch.
	"gopi tool voice_input --server": "starts the transcription HTTP server and loads the speech-engine model",
	"gopi --output-language":         "the flag is admitted by the parser (a bare flag run reaches the credential refusal, which is typed by the Usage gate TestTopLevelUsageShowsReplyLanguage); driving it as a FORM would need a live provider to observe the reply language it selects",
	"gopi worker":                    "drains the real ledger and runs task children (host action)",
	"gopi worker --once":             "drains the real ledger and runs task children (host action)",
	"gopi worker [<task-id>]":        "runs a task child against the real profile",
	"gopi worker --daemon":           "starts a detached resident process",
	"gopi worker --start":            "starts a detached resident process",
	"gopi worker --restart":          "stops and starts a detached resident process",
	// the top-level forms that reach the model
	"gopi \"<prompt>\"": "a run needs provider credentials (asserted up to the credential refusal)",
	// the top-level flag table outside the group's own verbs
}

// TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason is the
// completeness half of the epic: every advertised form is either driven by a
// case here or declared blocked WITH a blocker, and every case key is a form the
// help actually prints. A form in neither place is the defect the epic exists to
// remove; a case for a form that no longer exists is a stale claim.
func TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason(t *testing.T) {
	derived := g6DeriveSurface()
	total := 0
	for _, verb := range g6GroupVerbs {
		for _, form := range derived[verb] {
			total++
			_, tested := g6Cases[form]
			why, blocked := g6BlockedForms[form]
			switch {
			case tested && blocked:
				t.Errorf("%q is both tested and declared blocked — pick one", form)
			case !tested && !blocked:
				t.Errorf("%q (%s) is advertised and NEITHER tested NOR declared blocked — "+
					"that is exactly the \"advertised but never typed\" defect this group exists to remove", form, verb)
			case blocked:
				if strings.TrimSpace(why) == "" || strings.Contains(strings.ToLower(why), "todo") {
					t.Errorf("%q is declared blocked without a reason (%q) — a gap without a blocker is not an honest gap", form, why)
				}
			}
		}
	}
	for form := range g6Cases {
		// A form printed by TWO pages (the top-level list also names
		// `gopi version`) needs ONE case, not two: the map is keyed by the
		// form, which is what both pages print.
		if !g6IsDerivedForm(derived, form) {
			t.Errorf("a case is keyed on %q, which no help page prints any more — stale case", form)
		}
	}
	for form := range g6BlockedForms {
		if !g6IsDerivedForm(derived, form) {
			t.Errorf("the blocked list names %q, which no page prints any more — stale entry", form)
		}
	}
	// The floors are the MEASURED surface minus a small margin, so a scanner
	// regression that silently loses forms fails here rather than passing with a
	// thinner list: measured 2026-10-01, 112 typed and 31 declared blocked (147
	// advertised forms, 143 distinct). The floors themselves are deliberately
	// BELOW those numbers — a floor, not a target — and were left where the
	// surface first measured them (85/28/115; see devdoc/
	// g6-help-surface-forwarded-display.md).
	// THE BLOCKED FLOOR MOVED 28 → 25 (epic merge-removal, item 2), and the
	// reason is a genuine size change rather than a relaxation: `gopi merge` left
	// the command surface, so the six blocked forms its page contributed (the
	// destructive host actions this suite will not drive) are gone from the
	// derivation entirely — measured before the change, 31; measured after, 25.
	// The margin the floor exists for is untouched: it still sits BELOW the
	// measurement, so a scanner that silently loses a form still fails here.
	// The typed floor stays at 85 and the derived total at 115, both of which the
	// measured surface still clears (90 and 172) — only one verb was removed.
	if len(g6Cases) < 85 || len(g6BlockedForms) < 25 {
		t.Fatalf("coverage floor not met: %d tested forms, %d declared-blocked forms (measured: 90 and 30)", len(g6Cases), len(g6BlockedForms))
	}
	if total < 115 {
		t.Fatalf("only %d forms derived — the completeness claim above would be vacuous", total)
	}
}

// TestG6DumpDerivation is a RECONCILIATION AID, not a gate: it prints the
// in-process derivation in the same TSV shape scripts/
// g6-tools-maintenance-help-surface.sh emits, so a disagreement between the two
// implementations can be diffed line by line while they are being kept in step.
// It is deliberately left in the file — the two scanners document their rules
// separately, and the next agent who changes one will want this.
func TestG6DumpDerivation(t *testing.T) {
	if os.Getenv("G6_DUMP") == "" {
		t.Skip("reconciliation aid: set G6_DUMP=1 to print the derivation TSV")
	}
	derived := g6DeriveSurface()
	for _, verb := range g6GroupVerbs {
		for _, form := range derived[verb] {
			flags := strings.Join(g6FormFlags(form), ",")
			fmt.Printf("%s\t%s\t%s\t%d\n", verb, form, flags, len(g6FormFlags(form)))
		}
	}
}
