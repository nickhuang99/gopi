// cli_annotate.go — `gopi tool annotate <in.png> --out <out.png> --mark …`: draw
// marks (rectangle, point, arrow, label) onto an EXISTING PNG.
//
// RULE 3'S BOUNDARY. This is not a new top-level TOOL: the toolset stays at the
// fixed 17, and annotate is a CLI verb reached through `bash` — the same shape
// `gopi tool capture` and `gopi tool text2img` already have. The engine lives in
// internal/text2img (internal/text2img/annotate.go), the only package that owns
// a glyph rasteriser, so no second rasteriser and no new dependency is added.
//
// WHY THE VERB IS NOT `capture --annotate`. A capture is evidence of what the
// screen showed; letting it draw would contaminate the record it exists to be.
// So the two are separate verbs, and an annotation is always a NEW file whose
// input is never modified.
//
// GEOMETRY IS PIXELS OF THE OUTPUT IMAGE. The letters are the same letters
// `gopi tool capture --area` accepts (WxH[+X+Y]), because two verbs that take a
// rectangle must take it the same way — and a mark whose rectangle does not fit
// the image is REFUSED by name, never clipped (the same rule capture already
// applies to its rectangle; see devdoc/capability-annotate.md §refusals).
//
// A CELL ANCHOR is the one exception to the pixel rule, and it is the point of
// the feature for a terminal surface: with --display (or discovery) naming a
// `*-screen.dump`, a mark can be anchored to the dump's OWN cell grid
// (`12#4` = row 12, col 4; `12#4-12#30` = that row's cells 4..30) and is
// converted to pixels through this repository's ONE cell→pixel derivation
// (text2img.CellWidth for x, text2img.FontScale for y — the same fields the
// grid renderer lays a terminal capture out with). One drawing path, two ways
// to name one rectangle.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/shot"
	"github.com/nick/gopi/internal/text2img"
	"github.com/nick/gopi/internal/uiclient"
)

// annotateUsage is the -h text for the annotate subcommand.
const annotateUsage = `gopi tool annotate — draw marks onto an EXISTING PNG (a screenshot, a render)

Draws rectangles, points, arrows and text labels onto a still that already
exists, and writes a NEW png. The input is never modified: an annotation is
evidence ON TOP of a record, so the record must survive untouched (that is why
this is not "gopi tool capture --annotate").

Geometry is PIXELS of the OUTPUT image, spelled WxH[+X+Y] — the same grammar
"gopi tool capture --area" takes. A mark that does not fully fit the image is
REFUSED by name, never clipped (a clipped box changes what it claims to point
at).

Usage:
  gopi tool annotate <in.png> --out <out.png> [--mark SPEC]... [flags]

Flags:
  --out PATH          Output PNG. REQUIRED, and must differ from <in.png>.
  --mark SPEC         One mark, repeatable: <kind>:<geometry>:<label>:<anchor>
                        kind      rect | arrow | point
                        geometry  WxH[+X+Y] in output pixels (or cells when
                                  the anchor is a cell reference)
                        label     free text, spaces allowed, may be empty
                        anchor    fixed | <row>#<col> | <row>#<col>-<row>#<col>
                      rect  = rectangle outline, point = filled box,
                      arrow = shaft along the box's diagonal + a two-barb head
                      (a flat arrow is a 1px-tall box, e.g. 120x1+40+90).
  --size INT          Label font size, px. Default 20 (text2img's default).
  --lh INT            Label line spacing, px. Default: the size's own line height.
  --font PATH         Font path. Default: text2img's autodetected CJK chain.
  --display PATH      Read the cell grid from this dump and resolve cell
                      anchors against it. Default: discovery — the newest
                      *-screen.dump whose recorded pid is alive (the same
                      resolution "gopi ui_state" performs).
  --scrollback-rows N The image carries N history rows above the screen (a
                      render made with a scrollback band). Default 0: the image
                      starts at the screen's first row. A cell anchor's row is
                      measured inside that band.
  --json              Emit the result as JSON. Default OFF: the one-line
                      summary is printed instead.

The result is JSON: the output path, the output's width/height, how many marks
were drawn, anchor_source (fixed|cell|mixed), the grid's cols/rows and path
when a cell anchor was used, and ink_pixels — the MEASURED count of pixels this
run changed. N marks requested with 0 pixels changed is a refusal, never a
success (see devdoc/capability-annotate.md).

Examples:
  gopi tool capture --window --out before.png
  gopi tool annotate before.png --out marked.png \
    --mark "rect:200x40+14+13:status row:fixed" \
    --mark "arrow:120x1+40+120:shrinks:fixed" \
    --mark "rect:117x1+0+325:row 12:12#0-12#116"
  gopi tool annotate before.png --out marked.png --mark "point:6x6+88+40::fixed" --json

Deps: none added. Stdlib image/png plus internal/text2img's own rasteriser (no
module, no dependency, no font blob).
`

// The flag names, as constants (Rule 1b: one spelling, referenced).
const (
	annotateFlagOut     = "out"
	annotateFlagMark    = "mark"
	annotateFlagSize    = "size"
	annotateFlagLh      = "lh"
	annotateFlagFont    = "font"
	annotateFlagDisplay = "display"
	annotateFlagJSON    = "json"
	// annotateFlagScrollbackRows is how many history rows the IMAGE carries above
	// the screen. The verb cannot read that off the file, so the caller states it
	// (default 0: the image starts at the screen's first row, which is what a
	// `gopi tool text2img` grid render or a plain `gopi tool capture` produces). It is the
	// band term of text2img.GridCellRect, and the ONE knob that moves a cell mark
	// vertically without moving the grid.
	annotateFlagScrollbackRows = "scrollback-rows"
)

// annotateFlagMembers lists every flag the subcommand accepts.
var annotateFlagMembers = map[string]bool{
	annotateFlagOut:            true,
	annotateFlagMark:           true,
	annotateFlagSize:           true,
	annotateFlagLh:             true,
	annotateFlagFont:           true,
	annotateFlagDisplay:        true,
	annotateFlagJSON:           true,
	annotateFlagScrollbackRows: true,
}

// The `--mark` grammar's constants: the field separator, the field count, the
// cell-anchor separator and the row#col separator.
const (
	markFieldSep     = ":"
	markFieldCount   = 4
	markAnchorSep    = "-"
	markCellSep      = "#"
	markAnchorMixed  = "mixed"
	markAnchorCell   = "cell"
	markAnchorNone   = "none"
	markSpecWantText = "<kind>:<geometry>:<label>:<anchor>"
	// annotateLabelMaxCols caps a resolved cell span so a malformed anchor
	// cannot ask for a label chip the size of the image.
	annotateLabelMaxCols = 4096
)

// annotateRequest is one parsed invocation.
type annotateRequest struct {
	in      string
	out     string
	marks   []string
	size    int
	lineHei int
	font    string
	display string
	json    bool
	// scrollbackRows is the band the IMAGE carries above the screen (see
	// annotateFlagScrollbackRows).
	scrollbackRows int
}

// isAnnotateSubcommand reports whether argv begins with the annotate
// subcommand and runs it.
func isAnnotateSubcommand(args []string) bool {
	if len(args) > 0 && args[0] == CliVerbAnnotate {
		runAnnotateSubcommand(args[1:])
		return true
	}
	return false
}

// parseAnnotateArgs parses the flags without touching the process
// (unit-testable, per the parseCaptureArgs pattern).
func parseAnnotateArgs(args []string) (annotateRequest, error) {
	var req annotateRequest
	fs := newMemoryFlagSet()
	fs.boolean(annotateFlagJSON)
	fs.repeat(annotateFlagMark)
	positional, err := fs.parse(args)
	if err != nil {
		return req, err
	}
	for _, name := range fs.order {
		if !annotateFlagMembers[name] {
			return req, fmt.Errorf("unknown flag --%s", name)
		}
	}
	if len(positional) != 1 {
		return req, fmt.Errorf("want exactly one input PNG (got %d positional argument%s)", len(positional), annotatePlural(len(positional)))
	}
	req.in = positional[0]
	req.out = strings.TrimSpace(fs.get(annotateFlagOut))
	req.marks = fs.list(annotateFlagMark)
	req.font = strings.TrimSpace(fs.get(annotateFlagFont))
	req.display = strings.TrimSpace(fs.get(annotateFlagDisplay))
	req.json = fs.has(annotateFlagJSON)
	if err := parseIntFlag(fs.get(annotateFlagSize), annotateFlagSize, &req.size); err != nil {
		return req, err
	}
	if err := parseIntFlag(fs.get(annotateFlagLh), annotateFlagLh, &req.lineHei); err != nil {
		return req, err
	}
	if err := parseIntFlag(fs.get(annotateFlagScrollbackRows), annotateFlagScrollbackRows, &req.scrollbackRows); err != nil {
		return req, err
	}
	if req.scrollbackRows < 0 {
		return req, fmt.Errorf("--%s %d is negative", annotateFlagScrollbackRows, req.scrollbackRows)
	}
	return req, nil
}

// parseIntFlag fills dst from a flag's raw value, refusing a non-number by
// naming the flag (an error that says "invalid syntax" without saying which
// flag is not a refusal, it is a puzzle).
func parseIntFlag(raw, flag string, dst *int) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("--%s %q is not a number", flag, raw)
	}
	*dst = n
	return nil
}

// plural is the tiny s-suffix helper for the argument-count refusal.
func annotatePlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// runAnnotateSubcommand executes the verb.
func runAnnotateSubcommand(args []string) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(annotateUsage)
		return
	}
	req, err := parseAnnotateArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbAnnotate, err)
		os.Exit(1)
	}
	doc, err := annotateRun(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi %s: %v\n", CliVerbAnnotate, err)
		os.Exit(1)
	}
	if req.json {
		blob, encErr := json.MarshalIndent(doc, "", "  ")
		if encErr != nil {
			fmt.Fprintf(os.Stderr, "gopi %s: cannot render the result as JSON: %v\n", CliVerbAnnotate, encErr)
			os.Exit(1)
		}
		fmt.Println(string(blob))
		return
	}
	fmt.Println(doc.summary())
}

// annotateJSON is the machine-readable result. anchor_source is the field that
// says WHERE the geometry came from, so a caller never has to guess whether a
// rectangle was pixels or cells.
type annotateJSON struct {
	Tool         string `json:"tool"`
	Path         string `json:"path"`
	Input        string `json:"input"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Marks        int    `json:"marks"`
	Labels       int    `json:"labels"`
	InkPixels    int    `json:"ink_pixels"`
	AnchorSource string `json:"anchor_source"`
	GridCols     int    `json:"grid_cols"`
	GridRows     int    `json:"grid_rows"`
	GridPath     string `json:"grid_path,omitempty"`
	GridGeometry string `json:"grid_geometry,omitempty"`
	CellW        int    `json:"cell_w,omitempty"`
	CellH        int    `json:"cell_h,omitempty"`
}

// summary is the one-line text form.
func (d annotateJSON) summary() string {
	line := fmt.Sprintf("✅ %s → %s  %dx%d  marks=%d labels=%d 墨迹=%d 像素 anchor=%s",
		d.Input, d.Path, d.Width, d.Height, d.Marks, d.Labels, d.InkPixels, d.AnchorSource)
	if d.GridPath != "" {
		line += fmt.Sprintf(" grid=%dx%d cells cell=%dx%d px", d.GridCols, d.GridRows, d.CellW, d.CellH)
	}
	return line
}

// annotateRun parses the marks (resolving cell anchors), then draws.
func annotateRun(req annotateRequest) (annotateJSON, error) {
	doc := annotateJSON{Tool: CliVerbAnnotate, Path: req.out, Input: req.in}
	if strings.TrimSpace(req.out) == "" {
		return doc, fmt.Errorf("--%s is required: annotate writes a NEW png and never edits the input in place", annotateFlagOut)
	}
	if len(req.marks) == 0 {
		return doc, fmt.Errorf("no --%s given: nothing to draw (want --%s %s)", annotateFlagMark, annotateFlagMark, markSpecWantText)
	}
	grid, err := annotateResolveGrid(req)
	if err != nil {
		return doc, err
	}
	opts := text2img.AnnotateOptions{Size: req.size, Lh: req.lineHei, Font: req.font}
	marks := make([]text2img.Mark, 0, len(req.marks))
	for _, spec := range req.marks {
		m, err := parseMarkSpec(spec, grid)
		if err != nil {
			return doc, err
		}
		marks = append(marks, m)
	}
	blob, err := os.ReadFile(req.in)
	if err != nil {
		return doc, fmt.Errorf("cannot read the base image %s: %w", req.in, err)
	}
	dim, err := decodePNGDims(req.in, blob)
	if err != nil {
		return doc, err
	}
	doc.Width, doc.Height = dim.Dx(), dim.Dy()
	doc.AnchorSource, doc.CellW, doc.CellH = annotateAnchorSource(marks, opts)
	if grid != nil {
		doc.GridCols, doc.GridRows = grid.cols, grid.rows
		doc.GridPath, doc.GridGeometry = grid.path, string(grid.source)
	}
	res, err := text2img.AnnotateFile(req.in, req.out, marks, opts)
	if err != nil {
		return doc, err
	}
	doc.Path, doc.Input = res.Path, res.Input
	doc.Width, doc.Height = res.Width, res.Height
	doc.Marks, doc.Labels, doc.InkPixels = res.Marks, res.Labels, res.InkPixels
	return doc, nil
}

// decodePNGDims reads only the image's bounds — the geometry check the mark
// resolver needs BEFORE anything is drawn, so an off-image mark is refused
// before a single pixel changes.
func decodePNGDims(inPath string, blob []byte) (image.Rectangle, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(blob))
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("cannot decode the base image %s as PNG: %w", inPath, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return image.Rectangle{}, fmt.Errorf("the base image %s is %dx%d — an empty canvas cannot carry a mark", inPath, cfg.Width, cfg.Height)
	}
	return image.Rect(0, 0, cfg.Width, cfg.Height), nil
}

// annotateAnchorSource reports where the geometry came from, and the cell size
// when any mark was anchored to a grid.
func annotateAnchorSource(marks []text2img.Mark, o text2img.AnnotateOptions) (source string, cellW, cellH int) {
	cells, fixed := 0, 0
	for _, m := range marks {
		if strings.TrimSpace(m.Anchor) == "" || m.Anchor == text2img.MarkAnchorFixedWord {
			fixed++
			continue
		}
		cells++
	}
	switch {
	case cells == 0:
		return markAnchorNone, 0, 0
	case fixed == 0:
		source = markAnchorCell
	default:
		source = markAnchorMixed
	}
	size := o.Size
	if size <= 0 {
		size = text2img.DefaultSize
	}
	cellH = o.Lh
	if cellH <= 0 {
		cellH = text2img.FontScale(size)
	}
	return source, text2img.CellWidth(size), cellH
}

// annotateGrid is a resolved cell grid: the dump's own geometry plus the ONE
// cell-sizing derivation both the y conversion and the grid renderer use.
type annotateGrid struct {
	path     string
	source   uiclient.GeometrySource
	cols     int
	rows     int
	cellW    int
	cellH    int
	cells    []string // the grid, one rune per cell (row-major)
	scrollba []string
	// opts is the GridOptions the cell anchor is measured with — the SAME shape
	// the grid renderer takes, so text2img.GridCellRect can honour every term of
	// that layout (margin, row pitch, cell advance) from one source.
	opts text2img.GridOptions
	// bandRows is the history band the IMAGE carries above the screen, as the
	// caller declared it (--scrollback-rows) — never inferred from the dump,
	// which describes the sink's buffer rather than this file.
	bandRows int
}

// annotateResolveGrid resolves the dump a cell anchor is measured against.
//
// DISCOVERY IS REUSED, NEVER REIMPLEMENTED: the candidate scan, the identity
// verdict and the live-surface selection are uiclient's — the same three the
// `gopi ui_state` verb performs — so a mark can never be anchored to a grid the
// reader would have refused to show. An explicit --display path must be a dump
// (its record line is parsed); a discovery result that is ambiguous is a refusal
// naming what is on hand.
func annotateResolveGrid(req annotateRequest) (*annotateGrid, error) {
	if !annotateNeedsGrid(req.marks) {
		return nil, nil
	}
	size := req.size
	if size <= 0 {
		size = text2img.DefaultSize
	}
	lineHei := req.lineHei
	if lineHei <= 0 {
		lineHei = text2img.FontScale(size)
	}
	g := &annotateGrid{
		cellW:    text2img.CellWidth(size),
		cellH:    lineHei,
		opts:     text2img.GridOptions{Size: size, Lh: lineHei},
		bandRows: req.scrollbackRows,
	}
	if req.display != "" {
		cand, err := uiclient.ReadDump(req.display)
		if err != nil {
			return nil, fmt.Errorf("--%s %s: %w", annotateFlagDisplay, req.display, err)
		}
		g.path = cand.Path
		g.source = uiclient.GeometryOf(cand)
		g.cols, g.rows = cand.Dump.Cols, cand.Dump.Rows
		g.cells = cand.Dump.Grid
		g.scrollba = cand.Dump.Scrollback
		return g, annotateGridUsable(g, req.display)
	}
	gopiDir, err := config.GopiDir()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the gopi data root: %w", err)
	}
	cands, truncated, scanErr := uiclient.Candidates(uiclient.ScanRoots(gopiDir, scratch.HomeDir()), time.Now())
	if scanErr != nil && !uiclient.DumpAbsent(scanErr) {
		return nil, fmt.Errorf("cannot scan for a screen dump: %w", scanErr)
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("a cell anchor was asked for but no *-screen.dump is on hand: %s", uiclient.DumpNotArmedNotice)
	}
	sel, selErr := uiclient.SelectLive(cands, truncated)
	if selErr != nil {
		return nil, fmt.Errorf("a cell anchor was asked for but the grid could not be resolved (pass --%s PATH to name it): %w",
			annotateFlagDisplay, selErr)
	}
	cand := sel.Candidate
	g.path = cand.Path
	g.source = uiclient.GeometryOf(cand)
	g.cols, g.rows = cand.Dump.Cols, cand.Dump.Rows
	g.cells = cand.Dump.Grid
	g.scrollba = cand.Dump.Scrollback
	return g, annotateGridUsable(g, cand.Path)
}

// annotateNeedsGrid reports whether any --mark carries a cell anchor.
func annotateNeedsGrid(specs []string) bool {
	for _, s := range specs {
		fields := strings.SplitN(s, markFieldSep, markFieldCount)
		if len(fields) == markFieldCount && !isFixedAnchor(strings.TrimSpace(fields[3])) {
			return true
		}
	}
	return false
}

// annotateGridUsable refuses a grid that cannot be anchored to, by name.
//
// TWO REFUSALS, both about the AUTHORITY of the numbers rather than their
// presence: a FALLBACK geometry is the sink's own documented "this is not the
// screen's layout" (uiclient.GeometryFallbackNotice), and an empty grid is a
// dump that carried no cells. A mark anchored to either would be a mark whose
// cell→pixel arithmetic has no basis — exactly the silent failure this feature
// exists to avoid.
func annotateGridUsable(g *annotateGrid, path string) error {
	if g.source == uiclient.GeometrySourceFallback {
		return fmt.Errorf("the dump %s carries a FALLBACK geometry: %s — a cell anchor against it would be measured against a grid that is not the screen's",
			path, uiclient.GeometryFallbackNotice)
	}
	if g.cols <= 0 || g.rows <= 0 || len(g.cells) == 0 {
		return fmt.Errorf("the dump %s carries no cell grid (cols=%d rows=%d rows_of_cells=%d): a cell anchor has nothing to measure against",
			path, g.cols, g.rows, len(g.cells))
	}
	return nil
}

// parseMarkSpec parses one `--mark` argument.
func parseMarkSpec(spec string, grid *annotateGrid) (text2img.Mark, error) {
	var m text2img.Mark
	trimmed := strings.TrimSpace(spec)
	fields := strings.SplitN(trimmed, markFieldSep, markFieldCount)
	if len(fields) != markFieldCount {
		return m, fmt.Errorf("--%s %q: want %d %s-separated fields (%s), got %d",
			annotateFlagMark, spec, markFieldCount, markFieldSep, markSpecWantText, len(fields))
	}
	kindWord := strings.TrimSpace(fields[0])
	kind, err := text2img.ParseMarkKind(kindWord)
	if err != nil {
		return m, fmt.Errorf("--%s %q: %w", annotateFlagMark, spec, err)
	}
	anchor := strings.TrimSpace(fields[3])
	m = text2img.Mark{Kind: kind, Label: fields[2], Anchor: anchor}
	if isFixedAnchor(anchor) {
		rect, err := shot.ParseArea(strings.TrimSpace(fields[1]))
		if err != nil {
			return m, fmt.Errorf("--%s %q: %w", annotateFlagMark, spec, err)
		}
		m.Rect = image.Rect(rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H)
		return m, nil
	}
	if grid == nil {
		return m, fmt.Errorf("--%s %q: the anchor %q is a cell reference but no grid was resolved", annotateFlagMark, spec, anchor)
	}
	rect, err := grid.cellRect(anchor, spec)
	if err != nil {
		return m, err
	}
	m.Rect = rect
	return m, nil
}

// isFixedAnchor reports whether an anchor names the pixel geometry itself. An
// EMPTY anchor is `fixed`: the grammar's fourth field may be omitted with a
// trailing colon, and "no anchor given" must not mean "no geometry given".
func isFixedAnchor(anchor string) bool {
	a := strings.TrimSpace(anchor)
	return a == "" || a == text2img.MarkAnchorFixedWord
}

// cellRect converts a `row#col` / `row#col-row#col` anchor into an output-pixel
// rectangle through the repository's ONE cell→pixel derivation,
// text2img.GridCellRect — the function that lives beside the grid renderer so
// the arithmetic that PLACES a mark and the arithmetic that LAYS a grid out
// cannot drift apart. The verb asks the renderer what layout it used rather than
// re-deriving it (the defect this replaced: a conversion that omitted the page
// margin and put every cell mark 12px up and left of the cell it named).
func (g *annotateGrid) cellRect(anchor, spec string) (image.Rectangle, error) {
	from, to, hasRange := strings.Cut(anchor, markAnchorSep)
	r0, c0, err := g.parseCell(from)
	if err != nil {
		return image.Rectangle{}, fmt.Errorf("--%s %q: %w", annotateFlagMark, spec, err)
	}
	r1, c1 := r0, c0
	if hasRange {
		r1, c1, err = g.parseCell(to)
		if err != nil {
			return image.Rectangle{}, fmt.Errorf("--%s %q: %w", annotateFlagMark, spec, err)
		}
	}
	if r1 < r0 || c1 < c0 {
		return image.Rectangle{}, fmt.Errorf("--%s %q: the anchor %s runs backwards (end cell is above/left of the start)",
			annotateFlagMark, spec, anchor)
	}
	// The scrollback band the dump carried is part of the layout the anchor is
	// measured against, and is passed through rather than guessed: the band plus
	// its one closing rule row sit above row 0.
	return text2img.GridCellRect(r0, c0, r1+1, c1+1, g.bandRows, g.opts), nil
}

// parseCell parses `row#col` against the grid's own bounds. An out-of-grid cell
// is refused by name — never clamped, because a clamped anchor is a mark
// pointing at a cell the caller did not name.
func (g *annotateGrid) parseCell(s string) (row, col int, err error) {
	raw := strings.TrimSpace(s)
	rs, cs, ok := strings.Cut(raw, markCellSep)
	if !ok {
		return 0, 0, fmt.Errorf("cell reference %q wants <row>%s<col> (e.g. 12%s4)", raw, markCellSep, markCellSep)
	}
	row, err = strconv.Atoi(strings.TrimSpace(rs))
	if err != nil {
		return 0, 0, fmt.Errorf("cell reference %q: row %q is not a number", raw, rs)
	}
	col, err = strconv.Atoi(strings.TrimSpace(cs))
	if err != nil {
		return 0, 0, fmt.Errorf("cell reference %q: col %q is not a number", raw, cs)
	}
	if row < 0 || row >= g.rows || col < 0 || col >= g.cols {
		return 0, 0, fmt.Errorf("cell reference %q is outside the dump's %dx%d grid", raw, g.cols, g.rows)
	}
	if col >= annotateLabelMaxCols {
		return 0, 0, fmt.Errorf("cell reference %q: col %d is past the %d-column cap", raw, col, annotateLabelMaxCols)
	}
	return row, col, nil
}
