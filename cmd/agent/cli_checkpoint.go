package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/checkpoint"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
)

// rawLogDirEnv relocates the raw-log tree. The name is a constant here for the
// same reason internal/agent declares one (Rule 2/21: ONE declared name for the
// variable a reader reads): this package's two resolvers and the tests that
// probe them must name the same variable the writer honours.
const rawLogDirEnv = "GOPI_RAW_LOG_DIR"

// checkpointFlagPrefix is the argv spelling of a long option for this group.
// It is the same value as the config parser's own prefix; the two are separate
// because this group runs BEFORE config resolution and must refuse an unknown
// flag itself (see runCheckpointSubcommand) instead of handing it to a parser
// that will never see it.
const checkpointFlagPrefix = "--"

// checkpointDirFlag is the per-invocation scan-root override, WITHOUT its
// prefix — the flag the Usage page advertises, the dispatcher switches on and
// the oversized-root refusal hands back as a next step. One name, three
// readers (Rules 1b/13): the page's "1. --dir DIR" row, the switch case and the
// refusal text are the same channel, so the refusal cannot advise a spelling the
// dispatcher does not answer.
const checkpointDirFlag = "dir"

// cpRawLogDirSettingsKey is the settings.json key of the raw-log relocation this
// group's reference text points a reader at. It is the key of the
// internal/config catalog row that carries the same env name (settings.go:
// {"GOPI_RAW_LOG_DIR", "raw_log_dir", …}) — this constant names it so the
// refusal's next step can cite the knob, and the root-channel test pins the
// pair by reading the LIVE catalog row (their equality is asserted there, which
// is what keeps a rename from leaving this string behind).
const cpRawLogDirSettingsKey = "raw_log_dir"

// checkpointSubcmdResume is the RETIRED `resume` word of `gopi session checkpoint
// resume <log>`.
//
// THE DUPLICATE, AND WHY IT IS GONE (epic verb-restructure, 2026-10-05). This
// word forwarded its whole argument tail to runResume — "one implementation,
// two spellings" — for the same continuation `gopi session resume <log>` (and the new
// `gopi session resume <log>`) already performs. Two doors onto one command is
// the shape the restructure removes, so the spelling now DISPATCHES and REFUSES
// BY NAME, naming the two homes that remain (Rule 15: a removal explains
// itself; deleting the case instead would drop the word into the positional
// bucket and `gopi session checkpoint resume <log>` would exit 0 having done nothing —
// the silent no-op this group's whole flag contract exists to prevent).
//
// It is a named constant rather than a bare literal because the refusal text
// and the completion set both need the word (Rule 1b), and the tests drive the
// spelling through the constant rather than re-typing it.
const checkpointSubcmdResume = CliVerbResume

// checkpointSubcmdList and checkpointSubcmdExport are the two LIVE third-level
// words of this group — the read-only halves of a raw-log checkpoint (index the
// logs; print one snapshot). They are named here for the same reason
// checkpointSubcmdResume is (Rule 1b/13): the dispatcher switch, the usage
// page's Usage block and the hierarchy's recursive reverse guard
// (cli_help_hierarchy_reverse_guard_test.go) all need the words, and a
// hand-typed literal at each site is exactly the drift the guard exists to
// catch.
const (
	checkpointSubcmdList   = "list"
	checkpointSubcmdExport = "export"
)

// checkpointResumeRemovedPointer is the ONE statement of that removal (Rule
// 1b/13): the dispatch case below, the usage page's "Removed sub-verb" block and
// the tests all read THIS string, so a reader of any of the three is given the
// same home. Its %s slots are the retired word and the live spelling — filled
// from the constants that own them, never typed twice.
//
// IT USED TO OFFER TWO (`gopi resume <log>` OR `gopi session resume <log>`),
// and the second one is gone: verb-retirement phase 1 turned `gopi resume` into
// a top-level word this binary REFUSES BY NAME, so the old pointer sent the
// reader from one refusal to a second one — a dead end, which is exactly what
// the refusal machinery's own rule forbids (Rule 15). Measured 2026-10-06 on
// this branch: `gopi session checkpoint resume` answered `use \`gopi resume
// <log>\` or \`gopi session resume <log>\`, and `gopi resume <log>` answered
// "RETIRED — use `gopi session resume`". The one spelling that both dispatches
// and is the family's own page is what it prints now.
const checkpointResumeRemovedPointer = "gopi " + CliVerbCheckpoint + " %s: removed — one continuation, not two: " +
	"use `gopi " + CliVerbSession + " %s <log>`"

// checkpointResumeRemovedMessage renders the removal statement the dispatcher
// prints AND the tests compare against — one function, so a test can never pin a
// sentence the binary does not say (the %s slots are the retired word and the
// live spelling, both taken from their own constants).
func checkpointResumeRemovedMessage() string {
	return fmt.Sprintf(checkpointResumeRemovedPointer,
		checkpointSubcmdResume, checkpointSubcmdResume)
}

// checkpointUsage is the help text for the checkpoint/resume subcommands.
const checkpointUsage = `GoPi checkpoint & resume subcommands — continue an interrupted session
from its raw-log checkpoint (every → REQUEST line is a byte-exact context
snapshot: system prompt + full history + tool defs).

Raw logs live in TWO layouts: beside the session that wrote them
(~/.gopi/sessions/<task-id>/<run-uuid>.raw.log since the session-dir
unification), and flat under ~/.gopi/raw/ (gopi-raw-<timestamp>.log) for
runs written before it. Both are indexed; "checkpoint list" prints which
directories it looked at and by what rule.

WHICH ROOT IS SCANNED — three channels, highest first (the order
checkpointDir() applies; there is no --raw-log-dir flag on this group,
so the variable is the only channel an operator has here):
  1. --dir DIR                       — this invocation only
  2. GOPI_RAW_LOG_DIR=<dir>          — the raw-log relocation honoured
     by every gopi process: the same variable that moves where NEW raw
     logs are written (see the "raw_log_dir" knob in CONFIG.md). It
     replaces the default below outright, sessions root included — so a
     script that exports it makes "checkpoint list" scan a completely
     different tree
  3. the default — <gopi dir>/sessions, else <gopi dir>/raw
(--raw-log-dir, the flag CONFIG.md lists beside this variable, belongs
to the top-level config surface and is refused here.)

Usage:
  gopi session checkpoint list [--dir DIR]
      Index every raw log under DIR (default: GOPI_RAW_LOG_DIR when set,
      else ~/.gopi/sessions — the three channels listed above; descending
      one level into the run directories, and a DIR that is not a sessions
      root is listed flat) and show the last checkpoint of each (model, message
      count, sha256, time). The reqs column reads only each log's head
      (bounded, see IndexDir), so a multi-GB raw dir lists fast; logs bigger
      than the head sample show "≥N" (the count is then a lower bound).
      A zero-result listing is SELF-EXPLAINING: it prints the directories it
      scanned and the name rule it applied, never a bare "not found". This is
      the same command the session family teaches as "gopi session checkpoint
      list" — ONE three-level spelling, one sub-verb of argv per invocation,
      which is why the page prints a row per sub-verb instead of a
      list|export shorthand no operator could type.

  gopi session checkpoint export <log> [--dir DIR]
      Print the last → REQUEST of <log> as JSON (the resume snapshot).
      <log> = a path, a bare filename, or a gopi-raw-<id> timestamp prefix;
      both layouts resolve — a bare run-uuid or a filename is looked up in
      DIR and, when DIR is a sessions root, in the run directories below it.

  Both subcommands live under the session family — ` + "`gopi session checkpoint …`" + ` is
  the ONE three-level spelling, and it is the reason this page is printed by
  ` + "`gopi session checkpoint -h`" + ` too. (This note used to be spelled as a THIRD
  synopsis — the single line ` + "`gopi session checkpoint list|export [--dir DIR]`" + `
  — and that line was an invocation nobody could type: the word after
  ` + "`checkpoint`" + ` is a SUBCOMMAND NAME, and ` + "`list|export`" + ` is not one of the two
  this dispatcher answers. ` + "`gopi session checkpoint list|export`" + ` is SHORTHAND
  for the two commands spelled out above, not a third command; a synopsis is a
  promise, so the shorthand stays in prose where it belongs.)

  THE SHORTHAND ITSELF IS NOW REFUSED CROSS-GROUP, not only here: the bar ` + "`|`" + `
  is a choice between spellings a READER resolves, never one an operator types,
  so on every page in this family it belongs inside a bracketed group (where the
  page delimits the choice) or in prose (where it is read as a note). The gate
  that holds all of them at once is cli_help_shorthand_gate_test.go — this page
  is one of the pages it scans, and ` + "`gopi session checkpoint [list|export]`" + ` is
  one of the shapes it admits.

Removed sub-verb:
  The "resume" spelling that used to sit in the Usage block above is GONE
  (epic verb-restructure, 2026-10-05): it and ` + "`gopi session resume`" + ` were the
  same continuation reachable two ways, and one continuation keeps one
  spelling — ` + "`gopi session resume <log>`" + `, the family's own page. The old
  top-level ` + "`gopi resume`" + ` does NOT arrive: it is refused at argv[0] by the
  retirement itself, and the reader is handed that same address. The old
  SPELLING (a bare "resume" inside this group) still ARRIVES here and is
  refused BY NAME with that pointer: never obeyed (it would resume from a
  snapshot under a name this page no longer teaches), never ignored (a
  silently dropped sub-verb reads as "it ran").
`

// isCheckpointSubcommand reports whether argv begins with the checkpoint or
// resume subcommand. When it does, it runs the subcommand and returns true
// (the caller must not treat the remaining args as an input file).
func isCheckpointSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case CliVerbCheckpoint, CliVerbResume:
		runCheckpointSubcommand(args[0], args[1:])
		return true
	default:
		return false
	}
}

// runCheckpointSubcommand dispatches checkpoint list/export and resume.
// Subcommands run before config resolution in main() — each resolves its own
// settings the same way main would (flags > env > settings.json > defaults),
// then picks the raw-log directory (--dir flag > GOPI_RAW_LOG_DIR > default).
//
// EVERY `--flag` THIS GROUP DOES NOT KNOW IS REFUSED BY NAME (Rule 15). The
// group is dispatched before config resolution, so a misspelt or moved flag
// never reaches the config parser that would otherwise reject it: it used to
// fall into the positional bucket and be dropped, and `gopi session checkpoint list
// --bogus` then ran the plain listing and exited 0 — a typo silently obeyed.
// The tolerance that remains is deliberate and narrow: a bare POSITIONAL after
// the subcommand (`checkpoint list extra`) is still ignored, because the
// subcommand decides its own positional arity (`list` takes none, `export`
// takes exactly one) and each of those paths already reports what it needs;
// a `--flag` has no such reading, which is why only flags are refused here.
func runCheckpointSubcommand(cmd string, args []string) {
	dir := checkpointDir()
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(checkpointUsage)
			return
		case checkpointFlagPrefix + checkpointDirFlag:
			if i+1 >= len(args) {
				fmt.Fprint(os.Stderr, "gopi: --dir requires a path\n\n"+checkpointUsage)
				os.Exit(1)
			}
			i++
			dir = args[i]
		default:
			if cmd == CliVerbCheckpoint && strings.HasPrefix(args[i], checkpointFlagPrefix) {
				fmt.Fprintf(os.Stderr, "gopi session checkpoint: unknown flag %s\n\n%s", args[i], checkpointUsage)
				os.Exit(1)
			}
			rest = append(rest, args[i])
		}
	}
	args = rest

	switch cmd {
	case CliVerbCheckpoint:
		if len(args) == 0 {
			fmt.Fprint(os.Stderr, checkpointUsage)
			os.Exit(1)
		}
		switch args[0] {
		case checkpointSubcmdList:
			runCheckpointList(dir)
		case checkpointSubcmdExport:
			if len(args) < 2 {
				fmt.Fprint(os.Stderr, "gopi session checkpoint export: missing <log>\n\n"+checkpointUsage)
				os.Exit(1)
			}
			runCheckpointExport(resolveRawLog(args[1], dir), args[1])
		case checkpointSubcmdResume:
			// THE RETIRED FACE (see checkpointSubcmdResume, epic
			// verb-restructure 2026-10-05). `checkpoint resume` was the second
			// door onto one continuation: it forwarded its whole tail to
			// runResume, which `gopi session resume` and `gopi session resume` already
			// reach. It is refused BY NAME with both homes — a dispatch case
			// that fell through instead would leave `gopi session checkpoint resume
			// <log>` exiting 0 having continued nothing (Rule 15).
			fmt.Fprintln(os.Stderr, checkpointResumeRemovedMessage())
			fmt.Print(checkpointUsage)
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "gopi session checkpoint: unknown subcommand %q\n\n%s", args[0], checkpointUsage)
			os.Exit(1)
		}
	case CliVerbResume:
		// NOTE: `gopi session resume` is implemented in cli_resume.go (feat/resume —
		// session-id + rawLogPath resolution, tested). Kept here only as a
		// guard: if a caller reaches this branch it is a duplicate path; the
		// main dispatch routes resume to isResumeSubcommand first. This case
		// is unreachable and exists to document the split.
		fmt.Fprint(os.Stderr, "gopi: resume handled by cli_resume.go\n")
		os.Exit(1)
	}
}

// checkpointNextSteps renders the way OUT of an oversized-root refusal: the two
// channels that narrow the scan, in the order the caller should try them. It is
// ONE format string with the argv spelling and the env name as its arguments
// (Rules 1b/13/14) — the refusal's next step and the usage page's root section
// name the same two channels, and neither re-types the other's value.
const checkpointNextSteps = "  narrow the scan with:\n" +
	"    %s%s DIR        (scan one directory / one run directory)\n" +
	"    %s=DIR           (relocate the whole raw-log tree; the same variable that moves where new raw logs are written — see the %s knob in CONFIG.md)\n"

// runCheckpointList prints the checkpoint registry: one line per raw log, and
// the SCAN REPORT on stderr (both when it found logs and when it found none, so
// a zero-result listing names the directories it looked at and the judgement it
// applied instead of leaving "not found" unexplainable — Rule 15).
//
// AN OVERSIZED ROOT IS REFUSED BY NAME AND WITH A NEXT STEP (Rule 15). The
// indexer hands back a *checkpoint.NamedRefusal carrying the numbers; this face
// adds the part only the CLI knows — the two spellings that narrow the scan —
// and exits non-zero. Before this, the same root produced NO output at all for
// minutes and then died on a signal (rc=137): the defect was not the slow walk
// but the silence around it.
func runCheckpointList(dir string) {
	idxs, rep, err := checkpoint.IndexTree(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session checkpoint list: %v\n", err)
		var refusal *checkpoint.NamedRefusal
		if errors.As(err, &refusal) {
			fmt.Fprintf(os.Stderr, checkpointNextSteps,
				checkpointFlagPrefix, checkpointDirFlag,
				rawLogDirEnv, cpRawLogDirSettingsKey)
		}
		os.Exit(1)
	}
	for _, line := range rep.Describe() {
		fmt.Fprintln(os.Stderr, line)
	}
	if len(idxs) == 0 {
		fmt.Printf("no raw logs found under %s — see the scan report above for the "+
			"directories listed and the criteria applied\n", dir)
		return
	}
	fmt.Printf("%-24s %8s %-16s %6s %9s %-9s %s\n",
		"log", "reqs", "last-model", "msgs", "bytes", "sha256", "last-message")
	for _, idx := range idxs {
		reqs := fmt.Sprintf("%d", idx.Requests)
		if idx.RequestsTruncated {
			reqs = "≥" + reqs
		}
		// The trimmed label is printed WHOLE: it is the one row whose meaning a
		// 16-column truncation would destroy ("TRIMMED (no check…").
		model := truncate(idx.LastModel, 16)
		if idx.LastModel == checkpoint.TrimmedLogIndexLabel {
			model = idx.LastModel
		}
		fmt.Printf("%-24s %8s %-24s %6d %9d %s %s\n",
			filepath.Base(idx.Path), reqs, model,
			idx.LastMsgs, idx.LastBytes, idx.LastSHA256, idx.LastMessage)
	}
}

// runCheckpointExport prints the last → REQUEST of a raw log verbatim.
func runCheckpointExport(path, id string) {
	_, req, err := checkpoint.LastRequest(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session checkpoint export %s: %v\n", id, err)
		os.Exit(1)
	}
	b, err := marshalIndent(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session checkpoint export %s: %v\n", id, err)
		os.Exit(1)
	}
	fmt.Println(string(b))
}

// resolveRawLog maps a user-supplied log id (path / bare filename /
// gopi-raw-<timestamp> prefix / a co-located <run-uuid>.raw.log name) to an
// existing raw log path.
//
// Both layouts are searched (see checkpoint.IndexTree): dir is always looked in,
// and when dir IS a sessions root its run directories are searched too, one
// level down — the co-located layout the writer has used since the session-dir
// unification. Sharing checkpoint.IsRawLogName with the indexer keeps the two
// faces from refusing a name the other accepts.
func resolveRawLog(id, dir string) string {
	if id == "" {
		fmt.Fprint(os.Stderr, "gopi: missing raw log id\n")
		os.Exit(1)
	}
	// Absolute/relative path that exists wins.
	if fi, err := os.Stat(id); err == nil && !fi.IsDir() {
		return id
	}
	// A bare run uuid (or a filename without its suffix) also resolves to the
	// co-located log: <sessions>/<run-uuid>/<run-uuid>.raw.log.
	candidates := []string{
		filepath.Join(dir, id),
		filepath.Join(dir, id+rawLogFileSuffix),
		filepath.Join(dir, id+sessionRawLogFileSuffix),
		filepath.Join(dir, id, id+sessionRawLogFileSuffix),
		filepath.Join(dir, rawLogNamePrefix+id+rawLogFileSuffix),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	// ONE scan serves both the lookup and the not-found report (Rule 15: the
	// answer and its explanation must come from the SAME reading — a second
	// scan could list a directory that changed in between, or cost the walk
	// twice on a miss).
	idxs, rep, scanErr := checkpoint.IndexTree(dir)
	if scanErr != nil {
		// Same two-arm treatment as `checkpoint list` (runCheckpointList): the
		// refusal is named and followed by the way out, anything else is
		// reported verbatim. `export` on the default root used to reach the same
		// unbounded walk through this path, so a bare "%v" here would leave the
		// second face of the defect silently unhelpful (Rule 15).
		fmt.Fprintf(os.Stderr, "gopi: %v\n", scanErr)
		var refusal *checkpoint.NamedRefusal
		if errors.As(scanErr, &refusal) {
			fmt.Fprintf(os.Stderr, checkpointNextSteps,
				checkpointFlagPrefix, checkpointDirFlag,
				rawLogDirEnv, cpRawLogDirSettingsKey)
		}
		os.Exit(1)
	}
	var prefixMatches []string
	prefix := rawLogNamePrefix + id
	for _, idx := range idxs {
		base := filepath.Base(idx.Path)
		if base == id || strings.HasPrefix(base, prefix) {
			prefixMatches = append(prefixMatches, idx.Path)
		}
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0]
	}
	if len(prefixMatches) > 1 {
		fmt.Fprintf(os.Stderr, "gopi: %q matches %d raw logs — be more specific:\n", id, len(prefixMatches))
		for _, m := range prefixMatches {
			fmt.Fprintf(os.Stderr, "  %s\n", m)
		}
		os.Exit(1)
	}
	// A legacy flat prefix glob: the pattern is a constant, so ErrBadPattern is
	// unreachable.
	globbed, _ := filepath.Glob(filepath.Join(dir, prefix+rawLogFileSuffix+"*")) //stylegate:ignore glob for optional cleanup; the pattern derives from named constants
	if len(globbed) == 1 {
		return globbed[0]
	}
	if len(globbed) > 1 {
		fmt.Fprintf(os.Stderr, "gopi: %q matches %d raw logs — be more specific:\n", id, len(globbed))
		for _, m := range globbed {
			fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(m))
		}
		os.Exit(1)
	}
	// NOT FOUND: the report names every directory searched and the rule applied
	// (Rule 15) — the defect this carries is a lookup that answers "no" without
	// saying where it looked.
	for _, line := range rep.Describe() {
		fmt.Fprintln(os.Stderr, line)
	}
	fmt.Fprintf(os.Stderr, "gopi: no raw log matching %q under %s (searched %d directory(ies), see the report above)\n",
		id, dir, len(rep.Roots))
	os.Exit(1)
	return ""
}

// Names the resolveRawLog candidate ladder builds, taken from the one
// definition each (Rules 1/13/14).
const (
	rawLogNamePrefix        = checkpoint.RawLogNamePrefix
	rawLogFileSuffix        = checkpoint.RawLogFileSuffix
	sessionRawLogFileSuffix = checkpoint.SessionRawLogFileSuffix
)

// rawLogDir returns the raw-log directory (GOPI_RAW_LOG_DIR override or
// ~/.gopi/raw), mirroring agent.NewRawLogger's resolution. It is the LEGACY
// FLAT tree — the place the dispatch error log lives and where pre-unification
// runs wrote.
func rawLogDir() string {
	if v := os.Getenv(rawLogDirEnv); v != "" {
		return v
	}
	gd, err := config.GopiDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi: cannot resolve gopi dir: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(gd, "raw")
}

// checkpointDir is the directory `checkpoint list` / `export` scan by DEFAULT.
//
// The raw-log tree has TWO roots and the default must be the one that covers
// both: GOPI_RAW_LOG_DIR (the operator's explicit relocation, the writer's own
// override — a custom tree holds the flat files it was written to) else the
// SESSIONS root, because that is where every run since the session-dir
// unification (3a816775) co-locates its raw log. A default of the legacy flat
// directory is exactly the defect this fixes: measured on this host it found 6
// flat logs and 0 of the 1265 co-located ones.
func checkpointDir() string {
	if v := os.Getenv(rawLogDirEnv); v != "" {
		return v
	}
	gd, err := config.GopiDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi: cannot resolve gopi dir: %v\n", err)
		os.Exit(1)
	}
	return filepath.Join(gd, config.SessionsDirName)
}

// truncate cuts s to at most n runes, appending an ellipsis.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// marshalIndent pretty-prints a ClaudeRequest for checkpoint export.
func marshalIndent(req *deepseek.ClaudeRequest) ([]byte, error) {
	return json.MarshalIndent(req, "", "  ")
}
