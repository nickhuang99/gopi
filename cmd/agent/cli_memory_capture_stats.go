package main

// cli_memory_capture_stats.go — `gopi memory capture-stats`, the read-only
// hit-rate face of the auto-memory shape gate.
//
// WHAT IT ANSWERS, AND WHY IT IS A COMMAND RATHER THAN A PARAGRAPH IN A REPORT.
// The gate that decides whether a round becomes a memory row
// (internal/agent/selfnotecaptureresponse.go) is calibrated against a corpus —
// this profile's own session logs. A calibration is only honest if anyone can
// re-run it, on their machine, against their corpus, without trusting the
// number the report quoted (Rule 38: a census figure names its generator). This
// is that generator: it reads the session logs, runs every visible assistant
// body through the gate's OWN classifier, and prints the arm-by-arm table.
//
// READ-ONLY, AND PROVABLY SO: it opens session JSONL files for reading, opens no
// ledger, creates no file, starts no process and writes no config (Rule 35).
// Nothing here is scheduled; the operator runs it.
//
// IT IS A GROUPED SUB-VERB, NOT A NEW TOP-LEVEL VERB (Rule 3): the survey is a
// memory-family maintenance action like `provenance` beside it, and the word is
// registered in memorySubcommandNames — one vocabulary, read by the dispatcher,
// the completion set and the help page.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/capturesurvey"
	"github.com/nick/gopi/internal/gopilog"
)

const (
	// memoryCaptureStatsVerb is the sub-verb word. It is a constant because the
	// dispatcher's switch, memorySubcommandNames and the help all read it
	// (Rule 1b) — never a bare literal at two sites.
	memoryCaptureStatsVerb = "capture-stats"

	// memoryCaptureStatsRootFlag is the `--root DIR` spelling: the survey root,
	// i.e. a directory holding session logs (`*/session.jsonl`) or one log file.
	memoryCaptureStatsRootFlag = "root"

	// memoryCaptureStatsJSONFlag prints the same numbers as JSON, so the
	// re-derivation can be diffed between two runs (and between two hosts)
	// without parsing a rendered table.
	memoryCaptureStatsJSONFlag = "json"

	// memoryCaptureStatsNoteRatePct is the note capture rate this survey is read
	// AGAINST: 1.31% of 220,106 rounds, measured on the note corpus and recorded
	// in devdoc/auto-memory-candidate.md (the "并集（candidate）" row of its
	// table). It is a MEASUREMENT of the object the epic replaced, quoted here so
	// the comparison is visible on the surface rather than asserted in prose —
	// and, being a census figure of a past run, it is a quoted constant, never
	// recomputed here (Rule 38: the doc is its source).
	memoryCaptureStatsNoteRatePct = 1.31

	// memoryCaptureStatsMinFiles refuses to print a rate off an empty corpus: a
	// survey that read nothing must fail loudly rather than render 0% and look
	// like a finding (Rule 15).
	memoryCaptureStatsMinFiles = 1

	// memoryCaptureStatsUnknownArgPrefix is the first two words of the parser's
	// refusal ("unknown argument %q …"). It is a constant because TWO readers
	// need the same words: the parser writes it, and the help test asserts the
	// page does NOT carry it (a page plus a refusal line, on stdout, at exit 0,
	// is precisely the shape the regression must catch — Rule 13, Rule 1b).
	memoryCaptureStatsUnknownArgPrefix = "unknown argument"
)

// memoryCaptureStatsOptions is the parsed argv of one invocation.
type memoryCaptureStatsOptions struct {
	root string
	json bool
}

// helpRequested reports whether one argv token asks for the help page. The two
// spellings are the package's own (HelpFlag / HelpFlagLong, cli_verbs.go) — this
// file never types `-h` as a literal (Rule 13), and the scan is over the WHOLE
// argv for the reason every sibling gives: a help request is answered wherever
// it stands, so `--root X --json -h` is the page, not a survey.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == HelpFlag || a == HelpFlagLong {
			return true
		}
	}
	return false
}

// runMemoryCaptureStats is the sub-verb's entry point.
func runMemoryCaptureStats(args []string) {
	// THE HELP SPELLINGS ARE ANSWERED BEFORE THE PARSER SEES ANYTHING. Measured
	// defect (2026-10-30): `gopi memory capture-stats -h` exited 1 with
	// `unknown argument "-h"` and printed NOTHING, while the four siblings in
	// this group (runMemorySubcommand, runMemorySync, runMemoryBackup) all
	// printed a page and exited 0. Beyond the ergonomics, a sub-verb that
	// answers -h with a non-zero status has NO PAGE OF ITS OWN as far as
	// cmd/agent/man_source_gate_test.go's vocabulary is concerned
	// (mansource.HelpSubcommands → subcommandPage), so its flags silently
	// dropped out of the man-flag vocabulary and a TRUE man sentence about
	// --root/--json would be refused as an invented flag.
	if helpRequested(args) {
		fmt.Print(memoryCaptureStatsUsage)
		os.Exit(0)
	}
	opts, err := parseMemoryCaptureStats(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory %s: %v\n%s", memoryCaptureStatsVerb, err, memoryUsage)
		os.Exit(1)
	}
	root := opts.root
	if root == "" {
		// The default is THIS profile's own session directory — the corpus the
		// gate actually sees, resolved by the reader that owns that path
		// (gopilog.ResolveSessionDir: one definition, never re-spelled here).
		dir, err := gopilog.ResolveSessionDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi memory %s: resolve the session directory: %v\n", memoryCaptureStatsVerb, err)
			os.Exit(1)
		}
		root = dir
	}
	stats, err := capturesurvey.Survey(root, agent.ClassifyResponseText)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi memory %s: %v\n", memoryCaptureStatsVerb, err)
		os.Exit(1)
	}
	if stats.Files < memoryCaptureStatsMinFiles {
		fmt.Fprintf(os.Stderr, "gopi memory %s: %s holds no %s — nothing to measure\n",
			memoryCaptureStatsVerb, root, capturesurvey.SurveySessionFileName)
		os.Exit(1)
	}
	if opts.json {
		out, err := json.MarshalIndent(memoryCaptureStatsReport(root, stats), "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi memory %s: render JSON: %v\n", memoryCaptureStatsVerb, err)
			os.Exit(1)
		}
		fmt.Println(string(out))
		return
	}
	printMemoryCaptureStats(root, stats)
}

// parseMemoryCaptureStats accepts `[--root DIR] [--json]` and refuses anything
// else BY NAME: an unknown flag that was silently ignored would make a typo
// (`--roots`) read as "the default corpus", which is the wrong corpus.
func parseMemoryCaptureStats(args []string) (*memoryCaptureStatsOptions, error) {
	opts := &memoryCaptureStatsOptions{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--"+memoryCaptureStatsRootFlag:
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s needs a directory", memoryCaptureStatsRootFlag)
			}
			opts.root = args[i+1]
			i++
		case strings.HasPrefix(a, "--"+memoryCaptureStatsRootFlag+"="):
			opts.root = strings.TrimPrefix(a, "--"+memoryCaptureStatsRootFlag+"=")
		case a == "--"+memoryCaptureStatsJSONFlag:
			opts.json = true
		default:
			return nil, fmt.Errorf("%s %q (want [--%s DIR] [--%s])",
				memoryCaptureStatsUnknownArgPrefix, a, memoryCaptureStatsRootFlag, memoryCaptureStatsJSONFlag)
		}
	}
	return opts, nil
}

// memoryCaptureStatsRow is one arm's line in the JSON form.
type memoryCaptureStatsRow struct {
	Arm   string  `json:"arm"`
	Count int     `json:"count"`
	Pct   float64 `json:"pct_of_bodies"`
}

// memoryCaptureStatsDoc is the JSON document `--json` prints: the same numbers
// as the table, in a shape a script can diff.
type memoryCaptureStatsDoc struct {
	Root            string                  `json:"root"`
	Files           int                     `json:"files"`
	Rounds          int                     `json:"rounds"`
	Bodies          int                     `json:"bodies"`
	Arms            []memoryCaptureStatsRow `json:"arms"`
	Refusals        []memoryCaptureStatsRow `json:"refusals"`
	Runes           map[string]int          `json:"runes"`
	CapturedBodies  int                     `json:"captured_bodies"`
	BodyRatePct     float64                 `json:"body_rate_pct"`
	RoundRatePct    float64                 `json:"round_rate_pct"`
	NoteRatePct     float64                 `json:"note_rate_pct"`
	RoundRateVsNote float64                 `json:"round_rate_vs_note"`
}

// memoryCaptureStatsReport converts a Stats into the JSON document.
func memoryCaptureStatsReport(root string, s *capturesurvey.Stats) memoryCaptureStatsDoc {
	doc := memoryCaptureStatsDoc{
		Root:            root,
		Files:           s.Files,
		Rounds:          s.Rounds,
		Bodies:          s.Bodies,
		Arms:            captureStatsRows(s.Tiers, s.Bodies, s.ArmNames()),
		Refusals:        captureStatsRows(s.Reasons, s.Bodies, []string{capturesurvey.BucketTooShort, capturesurvey.BucketNoStructure}),
		Runes:           captureStatsRunes(s),
		CapturedBodies:  s.CapturedBodies,
		BodyRatePct:     captureStatsPct(s.BodyRate()),
		RoundRatePct:    captureStatsPct(s.RoundRate()),
		NoteRatePct:     memoryCaptureStatsNoteRatePct,
		RoundRateVsNote: captureStatsRatio(s.RoundRate()*100, memoryCaptureStatsNoteRatePct),
	}
	return doc
}

// captureStatsRows renders one counter map in a FIXED key order, so two runs
// print diffable output (a map range would shuffle).
func captureStatsRows(counts map[string]int, total int, order []string) []memoryCaptureStatsRow {
	rows := make([]memoryCaptureStatsRow, 0, len(order))
	for _, name := range order {
		n := counts[name]
		rows = append(rows, memoryCaptureStatsRow{Arm: name, Count: n, Pct: captureStatsPct(captureStatsFraction(n, total))})
	}
	return rows
}

// captureStatsRunes reports the rune-count quantiles the thresholds were
// calibrated against; the keys are the percentiles, literally, because a
// threshold argument has to name the quantile it sits at.
func captureStatsRunes(s *capturesurvey.Stats) map[string]int {
	out := map[string]int{}
	for _, p := range []float64{0, 0.25, 0.5, 0.75, 0.9, 1} {
		if v, ok := s.Quantile(p); ok {
			out[fmt.Sprintf("p%d", int(p*100))] = v
		}
	}
	return out
}

// captureStatsFraction guards the zero denominator at ONE place (a corpus with
// no bodies must print 0, never NaN).
func captureStatsFraction(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

// captureStatsPct renders a fraction as a percentage.
func captureStatsPct(f float64) float64 {
	return float64(int(f*1000+0.5)) / 10
}

// captureStatsRatio is `a / b`, guarded.
func captureStatsRatio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return float64(int(a/b*100+0.5)) / 100
}

// printMemoryCaptureStats renders the human table: the corpus, the arms, the
// refusals, the quantiles, and the comparison the epic asks for by name.
func printMemoryCaptureStats(root string, s *capturesurvey.Stats) {
	fmt.Printf("auto-memory response gate — %s\n", root)
	fmt.Printf("corpus: %d file(s), %d round(s), %d visible response body(ies)\n\n", s.Files, s.Rounds, s.Bodies)

	fmt.Printf("%-12s %8s %10s\n", "arm", "bodies", "% bodies")
	for _, r := range captureStatsRows(s.Tiers, s.Bodies, s.ArmNames()) {
		fmt.Printf("%-12s %8d %9.1f%%\n", r.Arm, r.Count, r.Pct)
	}
	fmt.Printf("%-12s %8d %9.1f%%\n", "CAPTURED", s.CapturedBodies, captureStatsPct(s.BodyRate()))
	fmt.Println()
	for _, r := range captureStatsRows(s.Reasons, s.Bodies, []string{capturesurvey.BucketTooShort, capturesurvey.BucketNoStructure}) {
		fmt.Printf("%-12s %8d %9.1f%%\n", "refused:"+r.Arm, r.Count, r.Pct)
	}
	fmt.Println()

	keys := make([]string, 0, len(captureStatsRunes(s)))
	for k := range captureStatsRunes(s) {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return runeQuantileRank(keys[i]) < runeQuantileRank(keys[j])
	})
	q := captureStatsRunes(s)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, q[k]))
	}
	fmt.Printf("body runes: %s\n\n", strings.Join(parts, " "))

	fmt.Printf("rate: %.1f%% of bodies, %.1f%% of rounds\n", captureStatsPct(s.BodyRate()), captureStatsPct(s.RoundRate()))
	fmt.Printf("note gate, same measure: %.1f%% of rounds (devdoc/auto-memory-candidate.md)\n", memoryCaptureStatsNoteRatePct)
	fmt.Printf("=> the response gate keeps %.2fx what the note gate kept\n", captureStatsRatio(s.RoundRate()*100, memoryCaptureStatsNoteRatePct))
}

// runeQuantileRank orders the p-keys numerically (p0 < p25 < … < p100). The keys
// are built by this file, so the parse cannot fail — a malformed key sorts last
// rather than panicking.
func runeQuantileRank(key string) int {
	n := 0
	for _, r := range strings.TrimPrefix(key, "p") {
		if r < '0' || r > '9' {
			return 1 << 30
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// memoryCaptureStatsUsageHead is the sub-verb's OWN page title. It exists, and
// is not `memoryUsage`, because the man-source reader
// (cmd/agent/man_source_gate_test.go's subcommandPage) decides whether a
// sub-verb has a page OF ITS OWN by comparing `gopi memory <sub> -h` against
// `gopi memory -h`: printing the group page verbatim would make this verb a
// FALLBACK, and its flags would then never enter the man-flag vocabulary.
const memoryCaptureStatsUsageHead = "GoPi memory capture-stats — the auto-memory response gate's re-derivable calibration."

// memoryCaptureStatsUsage is the page `gopi memory capture-stats -h` prints: the
// head, the synopsis (the SAME constant the group page carries), the flag rows,
// and the prose the group page already wrote.
const memoryCaptureStatsUsage = memoryCaptureStatsUsageHead + `

USAGE
` + memoryCaptureStatsUsageLine + `

FLAGS
  --` + memoryCaptureStatsRootFlag + ` DIR     survey this corpus instead of the profile's own session
                 directory: a directory holding per-session logs, or one
                 session log file.
  --` + memoryCaptureStatsJSONFlag + `         print the same numbers as one JSON document, so two runs
                 (or two hosts) can be diffed without parsing a table.

WHAT THE NUMBERS MEAN
  bodies       visible assistant response bodies the gate classified.
  arms         which shape admitted each captured body (table, list, …).
  refusals     why a body was refused, split by bucket.
  body runes   the quantiles the length thresholds are calibrated against.
` + memoryCaptureStatsUsageProse

// memoryCaptureStatsUsageLine is the synopsis line this sub-verb contributes to
// memoryUsage. It is a constant because the help text and the inventory gate
// must not be able to drift apart (Rule 13), and it is spelled as the operator
// types it.
const memoryCaptureStatsUsageLine = "  gopi memory " + memoryCaptureStatsVerb +
	" [--" + memoryCaptureStatsRootFlag + " DIR] [--" + memoryCaptureStatsJSONFlag + "]"

// memoryCaptureStatsUsageProse is the help page's paragraph for the sub-verb.
const memoryCaptureStatsUsageProse = `
      Report which shape the auto-memory RESPONSE gate captures, over this
      profile's own session logs: per-arm counts, the rune quantiles the
      thresholds are calibrated against, and the rate beside the note gate's
      measured 1.31%. READ-ONLY — it opens session logs for reading and writes
      nothing (no ledger, no file, no process). --root points the survey at
      another corpus (a sessions directory, or one session JSONL); with no
      --root it reads the current profile's session directory. Run it after any
      threshold change in internal/agent/selfnotecaptureresponse.go: the numbers
      ARE the calibration, and this is how they are re-derived.
`
