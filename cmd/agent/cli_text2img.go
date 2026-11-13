package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/text2img"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// text2img numeric-flag vocabulary and the ONE sentinel both refusal paths
// share.
const (
	text2imgFlagSize   = "size"
	text2imgFlagCols   = "cols"
	text2imgFlagLh     = "lh"
	text2imgFlagLines  = "lines"
	text2imgFlagMargin = "margin"
	text2imgFlagFont   = "font"

	// text2imgFlagOut is the output path as a FLAG, beside the positional
	// [output.png] it means the same thing as. The name is not a second
	// vocabulary: `out` is the agent tool's own parameter name
	// (internal/agent/loop.go's text2img schema), so the CLI spelling of the
	// same argument is the same word — a caller who learned the tool call does
	// not get `unknown flag --out` for asking for exactly that.
	text2imgFlagOut = "out"
)

// text2imgOutputEmptyMessage is the refusal for `--out` spelled with no
// operand (a trailing `--out`, or `--out=`). Given-but-empty is NOT absent
// (Rule 15): asking for an output path and naming none is not the same request
// as not asking, so it is refused rather than rerouted to the default
// destination (the gopi-text2img scratch class tree) the user did not ask for.
const text2imgOutputEmptyMessage = "requires a path"

// text2imgOutputConflictMessage is the refusal when the output is named TWICE
// — positionally and via --out. Both spellings are legal on their own; naming
// two different files in one command is not an ambiguity the parser may
// resolve silently, because whichever it picked the user would be wrong about
// the other (Rule 15).
const text2imgOutputConflictMessage = "output given twice: %q and --%s %q — pass one"

// text2imgInvalidInt is flagInt's sentinel for "the value the user spelled
// cannot be honoured": a non-integer, or an integer below the stated floor
// (0, -5). Both spellings are the RENDERER's own constants (Rule 14) — the
// package whose norm() reroutes a zero to the default is the one that must
// define where "given" stops being legal, so the CLI predicate and the
// library defaulting are one rule, not two that agree today.
const text2imgInvalidInt = text2img.InvalidFlagInt

// text2imgPositiveIntMessage is the refusal for a numeric flag whose value is
// not a positive integer. The predicate raising it enforces exactly this
// sentence: ABSENT takes the default, GIVEN must be positive — 0 spells no
// legal size, so it is refused rather than silently rerouted to the default
// the user did not ask for (Rule 15, no silent failure).
const text2imgPositiveIntMessage = "requires a positive integer"

// text2imgPositiveContractNote is the phrase text2imgUsage uses to state the
// contract the refusal message states. Help and predicate are two faces of one
// rule; TestText2imgUsageStatesThePositiveContract pins the help side, the
// five-flag zero-value regression pins the enforcing side.
const text2imgPositiveContractNote = "must be a positive integer"

// text2imgUsage is the help text for the text2img CLI subcommand. It is
// printed when the subcommand is missing required arguments, passes an invalid
// flag, or the user passes -h/--help.
const text2imgUsage = `gopi tool text2img — render a text file to PNG images (pure-Go TrueType renderer)

The subcommand exposes the text2img tool as a CLI command. Rendering is done
by the zero-dependency TrueType rasterizer in internal/text2img (SFNT parser
+ 4x supersampled scanline rasterizer, standard library only — no CGO, no
x/image). CJK is supported via the autodetected system font, so Chinese text
renders natively. Behavior is identical to the agent's text2img tool call.

Usage:
  gopi tool text2img <input.txt> [output.png] [flags]
      Render <input.txt> to PNG. output.png may be spelled either
      positionally or as --out PATH; with NEITHER, the render lands in the
      gopi-text2img scratch class tree (<scratch home>/dumps/gopi-text2img),
      NEVER beside <input.txt> — a default render must not leave a file in
      your working tree. Text longer than one page auto-paginates: an
      explicit output becomes output-1.png, output-2.png, …; the default
      names pages <stem>-1.dump.png, <stem>-2.dump.png, …

  gopi tool text2img notes.txt
      Defaults: 20px font, 50 cols wrap, 32px line height, 90 lines/page,
      12px margin (the OCR-validated claude-version settings, mean 96%+).
  gopi tool text2img report.txt out.png --size 28 --cols 60
      Larger font, wider wrap.
  gopi tool text2img report.txt --out out.png --size 28
      The same output, named as a flag — out is the agent tool's own
      parameter name, so the CLI accepts the word the tool uses.
  gopi tool text2img paper.txt --lines 60 --lh 36
      Tighter pagination, looser line height.

Numeric flags take --name VALUE or --name=VALUE. A flag that is ABSENT takes
the default below; a flag that IS given must be a positive integer, so
--size 0 and --size -5 are refused (exit 1) and never run at the default.

The output path has TWO spellings for ONE argument: the positional
[output.png] and --out PATH. Either alone works. --out with no path is
refused (exit 1), and naming two different outputs in one command is refused
too, naming both — the output is never chosen for you.

Flags:
  --out PATH   输出 PNG 路径 (同位置参数 [output.png]; 二选一).
  --size INT   字号(px). Default 20 (16px is blurry without hinting; 20px is
               the unhinted optimum).
  --cols INT   超长行折行字符数. Default 50 (short lines keep original
               structure).
  --lh INT     行高(px, ≈字号×1.6). Default 32.
  --lines INT  每页最大行数(≈2800 字符/页). Default 90; exceeding pages
               auto-split into output-1.png, output-2.png, …
  --margin INT 页边距(px). Default 12.
  --font PATH  字体路径. Default: autodetect a system CJK TrueType font.

Output: white-background black-text PNG(s), sized to content.
`

// isText2imgSubcommand reports whether argv begins with the text2img
// subcommand. When it does it runs the subcommand and returns true (the
// caller must not treat the remaining args as an input file).
func isText2imgSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolText2img {
		runText2imgSubcommand(args[1:])
		return true
	}
	return false
}

// text2imgOptions is the parsed result of a text2img subcommand invocation.
type text2imgOptions struct {
	input  string
	output string
	size   int
	cols   int
	lh     int
	lines  int
	margin int
	font   string
}

// parseText2imgArgs parses args for the text2img subcommand without touching
// the process (no os.Exit, no env mutation). It returns the parsed options or
// a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseText2imgArgs(args []string) (*text2imgOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !text2imgFlagMember(fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <input.txt> argument")
	}

	opts := &text2imgOptions{
		input:  strings.TrimSpace(positionals[0]),
		size:   text2imgIntFlag(fs, text2imgFlagSize, text2img.DefaultSize),
		cols:   text2imgIntFlag(fs, text2imgFlagCols, text2img.DefaultCols),
		lh:     text2imgIntFlag(fs, text2imgFlagLh, text2img.DefaultLh),
		lines:  text2imgIntFlag(fs, text2imgFlagLines, text2img.DefaultLines),
		margin: text2imgIntFlag(fs, text2imgFlagMargin, text2img.DefaultMargin),
		font:   strings.TrimSpace(fs.get(text2imgFlagFont)),
	}

	// Input path is required (empty-string positional counts as missing).
	if opts.input == "" {
		return nil, fmt.Errorf("missing <input.txt> argument")
	}

	// At most two positionals: <input.txt> [output.png].
	if len(positionals) > 2 {
		return nil, fmt.Errorf("unexpected extra argument %q", positionals[2])
	}
	if len(positionals) == 2 {
		opts.output = strings.TrimSpace(positionals[1])
	}

	if err := text2imgResolveOutput(opts, positionals, fs); err != nil {
		return nil, err
	}

	return opts, nil
}

// text2imgResolveOutput folds the TWO spellings of the output path — the
// positional [output.png] and the `--out PATH` flag — into opts.output. The
// two are the same argument, so:
//
//   - only one of them given → that one;
//   - `--out` given with an empty operand → refused, because empty is a value
//     the user spelled, not an absence (Rule 15);
//   - both given, naming the same path → accepted, they agree;
//   - both given, naming different paths → refused, naming both, because
//     silently preferring one makes the user wrong about the other.
func text2imgResolveOutput(opts *text2imgOptions, positionals []string, fs *memoryFlagSet) error {
	if !fs.has(text2imgFlagOut) {
		return nil
	}
	flagOut := strings.TrimSpace(fs.get(text2imgFlagOut))
	if flagOut == "" {
		return fmt.Errorf("--%s %s", text2imgFlagOut, text2imgOutputEmptyMessage)
	}
	positionalOut := ""
	if len(positionals) == 2 {
		positionalOut = strings.TrimSpace(positionals[1])
	}
	if positionalOut != "" && positionalOut != flagOut {
		return fmt.Errorf(text2imgOutputConflictMessage, positionalOut, text2imgFlagOut, flagOut)
	}
	opts.output = flagOut
	return nil
}

// text2imgIntFlag reads one numeric flag. ABSENT — the `--name` token never
// appeared in argv — takes def, the documented default. PRESENT must parse as
// a positive integer, so an empty value is the invalid sentinel exactly like
// "abc": the shared flag parser reports "" both for an omitted operand
// (`--size`) and for `--size=`, and asking for a size and spelling nothing is
// not the same request as asking for no size at all (Rule 15 — the help text
// says a flag that IS given must be a positive integer, and this is the
// predicate that makes that sentence true for every spelling).
func text2imgIntFlag(fs *memoryFlagSet, name string, def int) int {
	if !fs.has(name) {
		return def
	}
	return flagInt(fs.get(name), text2imgInvalidInt)
}

// flagInt parses a flag value string as an int, falling back to def when the
// flag was absent (""). A value that is not a positive integer — non-numeric,
// zero, or negative — is reported as text2imgInvalidInt, the ONE sentinel the
// dispatch walk compares against: the same rule the refusal message states.
func flagInt(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return text2imgInvalidInt
	}
	if n < text2img.MinPositiveInt {
		return text2imgInvalidInt
	}
	return n
}

// text2imgFlagMember reports whether a flag name is valid for the text2img
// subcommand.
func text2imgFlagMember(flag string) bool {
	switch flag {
	case text2imgFlagSize, text2imgFlagCols, text2imgFlagLh,
		text2imgFlagLines, text2imgFlagMargin, text2imgFlagFont,
		text2imgFlagOut:
		return true
	}
	return false
}

// runText2imgSubcommand parses the subcommand's own flags and dispatches to
// the shared tools.Text2Img function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors and on rendering failures.
func runText2imgSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(text2imgUsage)
			os.Exit(0)
		}
	}

	opts, err := parseText2imgArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool text2img: %v\n%s", err, text2imgUsage)
		os.Exit(1)
	}

	// flagInt reports text2imgInvalidInt for every value that is not a
	// positive integer — non-numeric, zero or negative — so ONE comparison
	// enforces exactly the rule the refusal states, and the refusal names the
	// flag the user spelled.
	for _, f := range []struct {
		name string
		v    int
	}{
		{text2imgFlagSize, opts.size},
		{text2imgFlagCols, opts.cols},
		{text2imgFlagLh, opts.lh},
		{text2imgFlagLines, opts.lines},
		{text2imgFlagMargin, opts.margin},
	} {
		if f.v == text2imgInvalidInt {
			fmt.Fprintf(os.Stderr, "gopi tool text2img: --%s %s\n%s", f.name, text2imgPositiveIntMessage, text2imgUsage)
			os.Exit(1)
		}
	}

	out, err := tools.Text2Img(opts.input, opts.output, text2img.Options{
		Size:   opts.size,
		Cols:   opts.cols,
		Lh:     opts.lh,
		Lines:  opts.lines,
		Margin: opts.margin,
		Font:   opts.font,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool text2img: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}
