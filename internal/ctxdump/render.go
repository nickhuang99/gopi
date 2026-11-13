// Package ctxdump renders ONE model request as a human-readable markdown file.
//
// WHAT IT IS FOR. Every → REQUEST record in a session's raw HTTP log IS the
// exact body the model received (internal/checkpoint states that contract, and
// session resume depends on it). That makes every round a complete context
// checkpoint — but the artifact is JSON with base64 images inside, megabytes
// wide, and reading it is not something a person or a model can do. This
// package turns one of those bodies into a PAGE: the system prompt, then the
// messages in order, with tool calls, tool results, thinking blocks and images
// rendered so a reader can see what the model was actually handed.
//
// THE OPERATOR'S RULING (2026-11-09) IS THE SHAPE OF THIS FILE, AND IT REPLACED
// THE OPPOSITE ONE:
//
//	「debug 工具默认就必须逐字 verbatim，不许加 --full 开关」
//
// So the page has NO completeness mode and NO bounds to raise. There is no
// `--full` flag, no Options struct, no per-block character budget, and no
// length cut anywhere in the render path. What this package writes for a block
// is the block's own text, byte for byte, so a reader can compare the page
// against the raw log with `diff` and see nothing but the headings.
//
// THE DEFECT THAT PROMPTED THE RULING was a per-block budget named
// DefaultSystemChars = 20000 that clipped the system prompt in half while the
// page's own heading claimed the full size: the GOPI.md/PROJECT.md block is
// 183,161 characters on this host and the page showed 11,156 of them (6%) under
// a `… [truncated: showed 11156 of 169499 chars]` label. A debug artifact whose
// whole purpose is "what did the model really receive" must not be a subset of
// it wearing the whole one's heading.
//
// TWO PROPERTIES IT STILL HOLDS:
//
//  1. IT NEVER EMITS RAW BASE64. An image block becomes one summary line
//     (`[image: png 124KB sha256:abc…]`). A 124 KB image is ~165 KB of base64 in
//     the body; a page that inlined it would be the JSON again, wearing a
//     heading. This is the ONE substitution the page makes, it is stated on the
//     image's own line, and it is what keeps a 300 KB request readable.
//  2. NOTHING ON THE PAGE IS PRODUCED BY THIS TOOL AND NOT BY THE MODEL. The
//     headings name what follows; the address table states which request the
//     file holds. There is no "N chars, N tokens" heading, no Totals section and
//     no truncation note, because the operator reads the page for the model's
//     bytes and every extra line is a line that is not one of them.
//
// It renders TEXT ONLY. It never posts, never resumes, never mutates a session:
// its whole subject is one already-recorded request body.
package ctxdump

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/deepseek"
)

// The artifact's name and modes. The name is ALWAYS an ordinal on the axis the
// export declares — never "current" and never "latest" — because the file name
// is a claim about the bytes beside it: a name that says "current" is a claim
// that expires the moment the next round lands (the same defect `screen-turn58`
// holding turn 59's screen was).
//
// TWO AXES, TWO NAMES (address.go states why): a turn-scoped export is
// `context-turn<N>.md` where N is the TURN (the number `gopi session show <id>`
// prints on its `turns` row), and a request-scoped one is `context-round<N>.md`
// where N is the ROUND (`--round N`, `gopi session log <id> N`). The old scheme
// wrote the ROUND number under the word "turn"; these two constants are what
// makes that impossible to repeat.
const (
	// FileNameFormat is a TURN-scoped artifact's name inside a session directory.
	FileNameFormat = "context-turn%d.md"
	// FileNamePattern is the same name as PROSE spells it, for a usage page that
	// must not print a concrete turn number it is not writing.
	FileNamePattern = "context-turn<N>.md"
	// RoundFileNameFormat is a ROUND-scoped artifact's name.
	RoundFileNameFormat = "context-round%d.md"
	// RoundFileNamePattern is the same name as prose spells it.
	RoundFileNamePattern = "context-round<N>.md"
	// LegacyRoundNameFormat is the name a PRE-AXIS export wrote for a ROUND
	// number: the turn-named format, fed a round. It is spelled here only so a
	// caller can NAME the historical file a new turn-scoped export shadows
	// (RoundAliasReport); nothing writes it and no historical file is migrated.
	LegacyRoundNameFormat = "context-turn%d.md"
	// LegacyRoundNameNoteFormat tells a caller that a historical artifact of the
	// same round sits beside the new one: the first verb is the historical path,
	// the second the file just written, the third the round both hold.
	LegacyRoundNameNoteFormat = "note: %s is a pre-axis artifact holding ROUND %d as well — it was left in place (historical files are not migrated); this export is %s, named for the TURN it holds"
	// FilePerm is 0644: the operator's to read, like the session log beside it.
	FilePerm = 0o644
	// DirPerm is the mode used when the session directory has to be created.
	DirPerm = 0o755
	// ExportOpenFlags is how the artifact file is opened: create, truncate,
	// write. TRUNCATE is the whole of the "export is an overwrite" property — a
	// second export of the SAME address replaces the file instead of appending a
	// second page to it (measured defect: two `## Request` sections in one file
	// after two runs of `gopi session context --last --out /tmp/ctx.md`). It is a
	// named constant because the append spelling is one edit away and would be
	// invisible in review.
	ExportOpenFlags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
)

// The one-line screen report. It is the epic's exact spelling and it is the
// ONLY thing this package's callers print: the body goes to the FILE, always.
const (
	// LineFormat is the screen summary; its arguments are the path, the file's
	// byte count and the message count. Those counts are SCREEN metrics — they
	// cost the page nothing, and they are what tells a reader whether the export
	// they just asked for is the size they expected. They are never written into
	// the file.
	//
	// A TOOL COUNT IS NOT ONE OF THEM (operator ruling, 2026-11-10): the screen
	// must not measure the artifact in tool calls, because a tool call is a
	// model-layer implementation detail — the same reason ROUND is not on this
	// line. Bytes and messages describe the FILE.
	LineFormat = "context written to %s (%d bytes, %d message(s))\n"
	// AddressTurnLineFormat is the address clause a TURN-scoped export appends to
	// the SCREEN LINE — the same line, never a second one (the epic's rule: the
	// screen carries one line and the payload goes to the file). It states the
	// TURN and nothing else: the ruling of 2026-11-10 makes turn the only unit an
	// operator-facing surface names, so the round the page happens to render and
	// the interval of rounds the turn covered are NOT on this line.
	AddressTurnLineFormat = " — address: TURN %d"
	// AddressRoundLineFormat is the address clause a ROUND-scoped export appends:
	// the ROUND, with its own word, so it can never be read as a turn. The round
	// axis survives as the DEBUG path (`--round N`, `context-round<N>.md`) and is
	// documented on the usage page for that purpose only.
	AddressRoundLineFormat = " — address: ROUND %d (debug axis: one HTTP request/response, NOT a turn)"
	// PageTitleTurnFormat is the page's H1 for a turn-scoped export. Turn only,
	// for the reason above: a title that also spelled the round interval made a
	// model-layer detail the page's headline.
	PageTitleTurnFormat = "# Model request — turn %d\n\n"
	// PageTitleRoundFormat is the page's H1 for a round-scoped export — the debug
	// axis, so the page SAYS which axis it is in and that it is not a turn.
	PageTitleRoundFormat = "# Model request — ROUND %d (debug axis: one HTTP request/response, NOT a turn)\n\n"
	// ImageSummaryFormat summarizes an image block without emitting its bytes.
	ImageSummaryFormat = "[image: %s %s sha256:%s]"
	// ImageUnknownType is the type word for a source whose media type is absent.
	ImageUnknownType = "image"
	// ImageUnknownSize is the size word for a source whose bytes cannot be read.
	ImageUnknownSize = "?"
	// ImageNotDecodable is the hash word for base64 that does not decode.
	ImageNotDecodable = "not-decodable"
	// ToolInputEmpty is what a tool call with no input renders as.
	ToolInputEmpty = "(no input)"
)

// The page's SECTION HEADINGS. They name what follows and nothing else: no
// per-block size, no token count, no "showed N of M". A heading that carried a
// measurement would be the tool reporting on itself in the middle of the
// model's bytes, which is the ruling this file implements.
const (
	// SystemHeading opens the system prompt section.
	SystemHeading = "## System prompt"
	// SystemNoBlocks is the sentence for a request that carried none.
	SystemNoBlocks = "(the request carried no system blocks)"
	// SystemBlockHeadingFormat labels one system block by ORDINAL only.
	SystemBlockHeadingFormat = "### system[%d]"
	// ToolsHeading opens the tool-definitions section.
	ToolsHeading = "## Tools"
	// ToolsNoDefs is the sentence for a request that declared no tools.
	ToolsNoDefs = "(the request carried no tool definitions)"
	// ToolsLineFormat lists one definition by name. The schema is deliberately
	// NOT reproduced: it is in the raw log, and pasting 18 JSON schemas here
	// would bury the conversation the page exists to show.
	ToolsLineFormat = "- `%s`\n"
	// MessagesHeading opens the message section.
	MessagesHeading = "## Messages"
	// RequestHeading opens the address table.
	RequestHeading = "## Request"
	// MessageHeadingFormat labels one message by ordinal and role.
	MessageHeadingFormat = "### %d · %s"
	// BlockTextHeadingFormat labels a text block inside message msgIdx.
	BlockTextHeadingFormat = "#### %d.%d · text"
	// BlockThinkingHeadingFormat labels a thinking block.
	BlockThinkingHeadingFormat = "#### %d.%d · thinking"
	// BlockToolUseHeadingFormat labels a tool call, keeping the id that pairs it
	// with its result. The id is an IDENTIFIER, not a count.
	BlockToolUseHeadingFormat = "#### %d.%d · tool_use `%s` (id %s)"
	// BlockToolResultHeadingFormat labels a tool result by the call it answers.
	BlockToolResultHeadingFormat = "#### %d.%d · tool_result for %s"
	// BlockImageHeadingFormat labels an image block.
	BlockImageHeadingFormat = "#### %d.%d · image"
	// BlockUnknownHeadingFormat labels a block kind this renderer does not know.
	BlockUnknownHeadingFormat = "#### %d.%d · %s (unrecognised block type)"
	// BlockTextFenceFormat wraps a block's text in a fenced code block when the
	// content is a tool payload (JSON or raw output) rather than prose.
	BlockTextFenceFormat = "```%s\n%s\n```\n\n"
	// FenceJSON is the language tag for a JSON payload.
	FenceJSON = "json"
	// FencePlain is the language tag for a plain-text payload.
	FencePlain = ""
)

// Counts are the quantities the one-line SCREEN report carries. They are
// measured while the page is written and never appear inside it: the file is
// the model's bytes, the screen line is the artifact's address and size.
type Counts struct {
	// Messages is how many entries req.Messages holds.
	Messages int
	// ToolBlocks counts tool_use AND tool_result blocks (a call and its answer
	// are two halves of one exchange; a reader asking "how many tool blocks"
	// means both).
	ToolBlocks int
	// ImageBlocks counts image blocks in the message stream.
	ImageBlocks int
	// ToolDefs counts the tool definitions sent beside the messages.
	ToolDefs int
}

// Summary is what a successful export leaves behind: the path it wrote, the
// file's byte count, the ADDRESS it was written under, the SECTION it shows, and
// the counts.
type Summary struct {
	Path    string
	Bytes   int64
	Address Address
	// Section is WHICH PART of the request this artifact holds. It is carried so
	// the one screen line can say it (Line) and so a caller can tell a filtered
	// view from the evidence by reading the summary, never by guessing from the
	// file's size. The zero value is SectionAll — the full page — which keeps
	// every pre-section caller's summary meaning what it always meant.
	Section Section
	// Alias is a non-empty sentence when a historical artifact of the same ROUND
	// sits beside this one under the pre-axis name (RoundAliasReport). It is
	// printed, never acted on: historical files are not migrated.
	Alias string
	Counts
}

// Notes are the lines a caller prints BESIDE the one screen line — never on it.
//
// The screen's promise is one line (a payload or a paragraph printed into the
// conversation is re-sent on every later turn), and a note is a DIAGNOSTIC about
// the artifact rather than the artifact's address. So a note goes to the
// caller's stderr beside the line, and a session with nothing to report prints
// nothing at all — the ordinary case costs no noise.
func (s Summary) Notes() []string {
	if s.Alias == "" {
		return nil
	}
	return []string{s.Alias}
}

// Line renders the ONE line a caller prints on screen.
//
// It states the ADDRESS first, because that is the defect this epic removes: a
// reader comparing the export against `gopi session show <id>` needs to see
// which address space the file is in, and "turn 2 (rounds 277–332)" and
// "round 336" are two different claims a screen must not blur into "turn 336".
func (s Summary) Line() string {
	// ONE line: LineFormat's own terminator is stripped and re-applied after the
	// address clause, so the screen keeps its promise (a payload printed into the
	// conversation is re-sent on every later turn; a second status line is the
	// first step back toward that).
	line := strings.TrimRight(fmt.Sprintf(LineFormat, s.Path, s.Bytes, s.Messages), "\n")
	return line + s.AddressWords() + s.Section.LineWords() + "\n"
}

// AddressWords is the address sentence appended to the screen line.
func (s Summary) AddressWords() string {
	if s.Address.Axis == AxisRound {
		return fmt.Sprintf(AddressRoundLineFormat, s.Address.Round)
	}
	return fmt.Sprintf(AddressTurnLineFormat, s.Address.Turn)
}

// Write renders req at the export's address (see address.go) into
// dir/<fileName-of-address> and returns the summary the caller prints.
//
// The path is DERIVED, never guessed: a caller that wants another destination
// writes the file itself through Render.
func Write(dir string, req *deepseek.ClaudeRequest, a Address) (Summary, error) {
	return WriteSection(dir, req, a, SectionAll)
}

// WriteSection is Write for ONE SECTION of the request. The full-page door above
// delegates here with SectionAll, so the two cannot diverge in any way except
// the section itself (and with SectionAll they do not diverge at all: the path,
// the page and the counts are the ones the full export has always produced).
func WriteSection(dir string, req *deepseek.ClaudeRequest, a Address, sec Section) (Summary, error) {
	if err := a.Validate(); err != nil {
		return Summary{}, err
	}
	if !sec.Valid() {
		return Summary{}, fmt.Errorf("ctxdump: unknown section %q", string(sec))
	}
	if dir == "" {
		return Summary{}, fmt.Errorf("ctxdump: no session directory to write %s into", sec.FileName(a))
	}
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return Summary{}, fmt.Errorf("ctxdump: cannot create %s: %w", dir, err)
	}
	return writeFile(sec.Path(dir, a), req, a, sec)
}

// WriteTo writes the artifact to an EXPLICIT destination when out is non-empty,
// and otherwise to the address's own name inside dir. It is the door both
// carriers use: the CLI passes `--out` (possibly empty, meaning "derive it"),
// the chat arm passes nothing and lets the session directory decide.
//
// An explicit out wins and its own directory is created; that is the caller
// SAYING where the file goes, and honouring it is the whole point of the flag.
// Its name is the CALLER's, so the page header is what carries the axis — which
// is why the header states it whether or not the name does.
//
// BOTH DOORS END IN writeFile, so "an export overwrites its address" is ONE
// behaviour and not two that can drift.
func WriteTo(out, dir string, req *deepseek.ClaudeRequest, a Address) (Summary, error) {
	return WriteToSection(out, dir, req, a, SectionAll)
}

// WriteToSection is WriteTo for ONE SECTION of the request — the door both
// carriers use once an operator has asked for `--system` or `--user`.
//
// An explicit `out` is the CALLER's name and is honoured as given (the page's
// own first paragraph then says which section it holds, so the claim travels
// with the bytes); with no `out` the name is derived from the SECTION, which is
// why a filtered export can never land on the full page's file.
func WriteToSection(out, dir string, req *deepseek.ClaudeRequest, a Address, sec Section) (Summary, error) {
	if !sec.Valid() {
		return Summary{}, fmt.Errorf("ctxdump: unknown section %q", string(sec))
	}
	if out == "" {
		return WriteSection(dir, req, a, sec)
	}
	if err := a.Validate(); err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(filepath.Dir(out), DirPerm); err != nil {
		return Summary{}, fmt.Errorf("ctxdump: cannot create %s: %w", filepath.Dir(out), err)
	}
	return writeFile(out, req, a, sec)
}

// writeFile is the ONE writer: open the path with ExportOpenFlags, render into
// it, close, stat, and report. Every caller above goes through it so the
// overwrite semantics cannot be true at one door and false at the other.
func writeFile(path string, req *deepseek.ClaudeRequest, a Address, sec Section) (Summary, error) {
	f, err := os.OpenFile(path, ExportOpenFlags, FilePerm)
	if err != nil {
		return Summary{}, fmt.Errorf("ctxdump: cannot write %s: %w", path, err)
	}
	counts, rerr := RenderSection(f, req, a, sec)
	cerr := f.Close()
	if rerr != nil {
		return Summary{}, rerr
	}
	if cerr != nil {
		return Summary{}, fmt.Errorf("ctxdump: cannot close %s: %w", path, cerr)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Summary{}, fmt.Errorf("ctxdump: cannot stat %s after writing it: %w", path, err)
	}
	return Summary{Path: path, Bytes: fi.Size(), Address: a, Section: sec, Alias: RoundAliasReport(filepath.Dir(path), a), Counts: counts}, nil
}

// Render writes the markdown page for req at the export's ADDRESS into w.
//
// The page's shape is FIXED, so two exports of the same request are byte-equal
// and a reader (or a diff) can compare two turns by reading the headings:
//
//	# Model request — turn N                (the header: the address — TURN only)
//	## Request                               (the address as a table)
//	## System prompt                         (one subsection per system block)
//	## Tools                                 (names only, never the full schema)
//	## Messages                              (one subsection per message)
//
// The FURNITURE above `## System prompt` speaks in TURNS and nothing else
// (operator ruling, 2026-11-10): no round number, no interval of rounds, no
// tool-call count. A round is a model-layer detail; the round AXIS survives as
// the debug path (`--round N`, `context-round<N>.md`), whose own page says
// ROUND because there the round IS the subject.
//
// Every block body is written VERBATIM. The only substitution the page makes is
// an image block's base64 → a one-line summary, and that substitution is
// stated on the image's own line.
func Render(w io.Writer, req *deepseek.ClaudeRequest, a Address) (Counts, error) {
	return RenderSection(w, req, a, SectionAll)
}

// RenderSection is Render for ONE SECTION of the request.
//
// THE FULL PAGE IS NOT A MODE OF THIS FUNCTION; SectionAll is simply the section
// that wants everything, and with it this function emits exactly the bytes it
// emitted before sections existed (the same title, the same table, the same
// three headings, the same counted blocks). A filtered page differs ONLY in
// which of the three body sections it carries, plus one paragraph under the
// title stating that it is a view — so a reader can never mistake a 4 KB
// `-system.md` for a truncated evidence file.
func RenderSection(w io.Writer, req *deepseek.ClaudeRequest, a Address, sec Section) (Counts, error) {
	if req == nil {
		return Counts{}, fmt.Errorf("ctxdump: no request to render")
	}
	if err := a.Validate(); err != nil {
		return Counts{}, err
	}
	if !sec.Valid() {
		return Counts{}, fmt.Errorf("ctxdump: unknown section %q", string(sec))
	}
	var c Counts
	bw := &countingWriter{w: w}

	// THE TITLE AND THE AXIS NOTE ARE RENDERED FROM THE ADDRESS, so the file's
	// name and its own statement of itself cannot drift (Rule 14). A page whose
	// title says "turn 336" while `session show` says the session ran 2 turns is
	// the exact defect this epic removes, and `--out PATH` means the name is not
	// available to catch it.
	if a.Axis == AxisRound {
		fmt.Fprintf(bw, PageTitleRoundFormat, a.Round)
	} else {
		fmt.Fprintf(bw, PageTitleTurnFormat, a.Turn)
	}
	// A FILTERED page says so under its title, before any content: the name is
	// the caller's when `--out` is used, so the page is the only thing that can
	// state which part of the request this file holds.
	if !sec.IsFull() {
		fmt.Fprintf(bw, PageTitleSectionSuffixFormat, sec.Name())
	}

	// ---- the address table ----
	// It stays because the FILE NAME is the caller's when `--out` is used, so
	// this table is the only place the artifact states which request it holds.
	// It carries the REQUEST's own scalars — the parameters the body was sent
	// with — and no measurement of this renderer.
	//
	// THE TURN-SCOPED TABLE NAMES THE TURN AND NOTHING ELSE (ruling 2026-11-10):
	// no `rounds` interval row and no `rendered round` row, because both count
	// model-layer requests on a page an operator reads in turns. The ROUND axis
	// keeps its own rows — it is the debug address, and there the round IS the
	// subject.
	fmt.Fprintf(bw, RequestHeading+"\n\n")
	fmt.Fprintf(bw, "| field | value |\n|---|---|\n")
	if a.Axis == AxisRound {
		fmt.Fprintf(bw, "| turn | (not a turn — this export is a ROUND, the debug axis) |\n")
		fmt.Fprintf(bw, "| round | %d |\n", a.Round)
	} else {
		fmt.Fprintf(bw, "| turn | %d |\n", a.Turn)
	}
	fmt.Fprintf(bw, "| model | %s |\n", orNone(req.Model))
	fmt.Fprintf(bw, "| max_tokens | %d |\n", req.MaxTokens)
	fmt.Fprintf(bw, "| stream | %t |\n", req.Stream)
	if req.Thinking != nil {
		fmt.Fprintf(bw, "| thinking | %s (budget_tokens %d) |\n", orNone(req.Thinking.Type), req.Thinking.BudgetTokens)
	} else {
		fmt.Fprintf(bw, "| thinking | (unset) |\n")
	}
	fmt.Fprintf(bw, "| reasoning_effort | %s |\n", orNone(req.ReasoningEffort))
	fmt.Fprintf(bw, "\n")

	// ---- system prompt ----
	// VERBATIM, with no heading counts and no cut. This is the block the old
	// DefaultSystemChars budget clipped to 6% of itself while the heading claimed
	// the whole size; the block now stands alone under its ordinal so a reader can
	// pull it straight out with `sed -n` and `diff` it against the raw log.
	//
	// A messages-only export SKIPS this section entirely (and Tools, and the
	// message section is skipped by a system-only one): the sections a reader
	// compares are whole, never trimmed, because a half-section would be the
	// truncation the operator's ruling retired, wearing a flag's name.
	if sec.WantsSystem() {
		fmt.Fprintf(bw, SystemHeading+"\n\n")
		if len(req.System) == 0 {
			fmt.Fprintf(bw, SystemNoBlocks+"\n\n")
		}
		for i, blk := range req.System {
			fmt.Fprintf(bw, SystemBlockHeadingFormat+"\n\n", i+1)
			fmt.Fprintf(bw, "%s\n\n", blk.Text)
		}
	}

	// ---- tools ----
	if sec.WantsTools() {
		fmt.Fprintf(bw, ToolsHeading+"\n\n")
		if len(req.Tools) == 0 {
			fmt.Fprintf(bw, ToolsNoDefs+"\n\n")
		}
		for _, t := range req.Tools {
			fmt.Fprintf(bw, ToolsLineFormat, t.Name)
		}
		if len(req.Tools) > 0 {
			fmt.Fprintf(bw, "\n")
		}
		c.ToolDefs = len(req.Tools)
	}

	// ---- messages ----
	if sec.WantsMessages() {
		fmt.Fprintf(bw, MessagesHeading+"\n\n")
		for i, msg := range req.Messages {
			c.Messages++
			fmt.Fprintf(bw, MessageHeadingFormat+"\n\n", i+1, orNone(msg.Role))
			for j, blk := range msg.Content {
				renderBlock(bw, &c, i+1, j+1, blk)
			}
		}
	}

	return c, bw.err
}

// renderBlock writes one content block under its message, verbatim.
func renderBlock(w io.Writer, c *Counts, msgIdx, blkIdx int, blk deepseek.ClaudeContentBlock) {
	switch blk.Type {
	case "text":
		fmt.Fprintf(w, BlockTextHeadingFormat+"\n\n", msgIdx, blkIdx)
		fmt.Fprintf(w, "%s\n\n", blk.Text)
	case "thinking":
		fmt.Fprintf(w, BlockThinkingHeadingFormat+"\n\n", msgIdx, blkIdx)
		fmt.Fprintf(w, "%s\n\n", blk.Thinking)
	case "tool_use":
		c.ToolBlocks++
		input, ierr := json.Marshal(blk.Input)
		if ierr != nil {
			// An unmeasurable input is STATED rather than rendered as "(no input)",
			// which is the message for a call that genuinely carried none
			// (Rule 15: absence stated, never invented).
			fmt.Fprintf(w, BlockToolUseHeadingFormat+" (input UNMEASURABLE: %v)\n\n",
				msgIdx, blkIdx, orNone(blk.Name), orNone(blk.ID), ierr)
			return
		}
		fmt.Fprintf(w, BlockToolUseHeadingFormat+"\n\n", msgIdx, blkIdx, orNone(blk.Name), orNone(blk.ID))
		if len(input) <= 2 {
			fmt.Fprintf(w, "%s\n\n", ToolInputEmpty)
			return
		}
		fmt.Fprintf(w, BlockTextFenceFormat, FenceJSON, string(input))
	case "tool_result":
		c.ToolBlocks++
		fmt.Fprintf(w, BlockToolResultHeadingFormat+"\n\n", msgIdx, blkIdx, orNone(blk.ToolUseID))
		fmt.Fprintf(w, BlockTextFenceFormat, FencePlain, toolResultText(blk.Content))
	case "image":
		// The ONE substitution the page makes: bytes → a summary line that keeps
		// what a reader can act on (kind, size, hash) and states itself.
		c.ImageBlocks++
		fmt.Fprintf(w, BlockImageHeadingFormat+"\n\n%s\n\n", msgIdx, blkIdx, ImageSummary(blk.Source))
	default:
		// An unknown block type is NAMED with its own JSON so a reader sees what
		// arrived instead of an empty section (Rule 15: absence stated).
		raw, rerr := json.Marshal(blk)
		if rerr != nil {
			fmt.Fprintf(w, BlockUnknownHeadingFormat+" — unrenderable: %v\n\n",
				msgIdx, blkIdx, orNone(blk.Type), rerr)
			return
		}
		fmt.Fprintf(w, BlockUnknownHeadingFormat+"\n\n", msgIdx, blkIdx, orNone(blk.Type))
		fmt.Fprintf(w, BlockTextFenceFormat, FenceJSON, string(raw))
	}
}

// ImageSummary renders an image block WITHOUT its bytes: the type, the decoded
// size and the sha256 of the decoded content.
//
// WHY THIS SHAPE. The epic's red line is "never the raw base64": a page that
// inlined it would be the JSON again. The summary keeps what a reader can
// actually act on — is this the same image I saw last turn (hash), how big is it
// (cost), what format — and nothing that is only machine-readable.
func ImageSummary(src *deepseek.ClaudeImageSource) string {
	if src == nil {
		return fmt.Sprintf(ImageSummaryFormat, ImageUnknownType, ImageUnknownSize, ImageNotDecodable)
	}
	kind := imageKind(src.MediaType)
	// A file_id source carries a handle, not bytes: there is nothing to size or
	// hash, and saying so is the honest answer (the file itself is on the
	// provider's side).
	if src.Type == "file" || (src.Data == "" && src.FileID != "") {
		return fmt.Sprintf("[image: %s file_id:%s]", kind, orNone(src.FileID))
	}
	raw, err := base64.StdEncoding.DecodeString(src.Data)
	if err != nil {
		// The data does not decode. Report the base64 length as the size it
		// claims and NAME the fact that no hash could be taken — never a made-up
		// hash, which would make two different images compare equal.
		return fmt.Sprintf(ImageSummaryFormat, kind, humanBytes(int64(base64.StdEncoding.DecodedLen(len(src.Data)))), ImageNotDecodable)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf(ImageSummaryFormat, kind, humanBytes(int64(len(raw))), hex.EncodeToString(sum[:]))
}

// imageKind turns a media type into the one-word kind the summary prints.
func imageKind(mediaType string) string {
	if mediaType == "" {
		return ImageUnknownType
	}
	if i := strings.LastIndexByte(mediaType, '/'); i >= 0 && i+1 < len(mediaType) {
		return mediaType[i+1:]
	}
	return mediaType
}

// humanBytes renders a byte count as a short human size (KB / MB).
func humanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%dMB", n/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%dKB", n/1024)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// toolResultText flattens a tool_result's Content, which the wire carries as a
// string OR as an array of blocks.
func toolResultText(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("(tool result of an unrenderable shape: %v)", err)
		}
		return string(b)
	}
}

// orNone names an absent value instead of leaving a column blank, so a reader
// can tell "unset" from "the renderer forgot to fill it in".
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// countingWriter counts the bytes it passes through, so the page's size is
// measured as it is written rather than by a second pass.
type countingWriter struct {
	w   io.Writer
	n   int64
	err error
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	if err != nil {
		c.err = err
	}
	return n, err
}
