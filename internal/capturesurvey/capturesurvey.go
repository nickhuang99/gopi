package capturesurvey

// capturesurvey — the READ-ONLY, RE-RUNNABLE hit-rate face of the response shape
// gate (internal/agent/selfnotecaptureresponse.go).
//
// WHY IT IS ITS OWN PACKAGE (Rule 14, and a measured import direction):
// internal/agent IMPORTS internal/gopilog (internal/gopilog/formatter.go imports
// internal/agent), so agent cannot read the session JSONL through gopilog
// without a cycle. The survey therefore owns its own tiny reader of the log's
// documented wire shape (the same shape internal/gopilog/reader.go parses: a
// `message.content` array whose `text` parts are the visible body), and it
// DEPENDS ON NOTHING BUT THE STDLIB.
//
// WHOSE GATE IS IT. This package does not classify anything. The strings and
// groups here are the ENUMERATION (which files, which bodies); the verdict for
// every body comes through the Classify callback the caller supplies — in
// production ONE line wiring it to agent.ClassifyResponseText, so the survey and
// the response-end capture cannot disagree about what a shape is. A duplicated
// classifier here would be exactly the second source of truth Rule 13/14 forbid.
//
// RULE 35: no process, no timer, no config key, no state file. It is a function
// call over files the operator already has, printing a table.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// SurveySessionFileName is the per-session log this survey reads. It is the
	// same basename internal/gopilog resolves (one spelling, Rule 13); it is
	// spelled here rather than imported because importing gopilog from a package
	// agent may also import would put the cycle back.
	SurveySessionFileName = "session.jsonl"

	// The event discriminator values of the log's own vocabulary — never typed
	// twice (Rule 1b). Only the two this reader acts on are named: every other
	// event type is skipped by falling through the switch, so naming one the
	// reader ignores would be a constant no code path reads (Rule 21's lesson,
	// one level down).
	surveyEventAssistant = "assistant"
	surveyEventRequest   = "request"

	// The content-block types whose text is NOT the visible body. `text` is the
	// body; `thinking` is the model's reasoning (a different object, deliberately
	// not surveyed here) and `tool_use` is a call.
	surveyBlockTypeText = "text"

	// surveyMaxLineBytes caps ONE JSONL line. A session log can carry a briefing
	// with an attachment inline (measured in the note-corpus work: one file over
	// 60 MiB, whose single line is a briefing, not a conversation turn), and the
	// scanner must be able to step over it rather than die on it.
	surveyMaxLineBytes = 64 << 20

	// surveyScannerInitBytes is the scanner's starting buffer — large enough that
	// ordinary rounds (tens of KiB) are read without a single regrow.
	surveyScannerInitBytes = 1 << 20

	// The arms' tier names, as the survey reports them. They are the gate's own
	// values (agent.respCaptureTier*) — asserted equal by the caller's wiring and
	// by TestSurveyTierNamesMatchTheGate, so a renamed tier cannot leave the
	// report attributing rows to an arm that no longer exists.
	TierTopology   = "topology"
	TierTable      = "table"
	TierConclusion = "conclusion"

	// The two buckets that are not captures: bodies below the floor, and bodies
	// that cleared the floor and carried no shape at all. They are reported
	// SEPARATELY because "too short to matter" and "long and shapeless" are
	// different findings — the second is the arm that may need work.
	BucketTooShort    = "too-short"
	BucketNoStructure = "no-structure"

	// RunePercentiles are the rune-count quantiles the report prints. They are
	// the numbers a threshold can be argued from, so a report that prints none
	// (an empty corpus) is reported as such rather than rendered as zeros.
	RunePercentiles = 9
)

// LogEvent is the one event shape this survey reads out of a session log. Every
// other key on the line is ignored, which is what keeps the reader honest about
// a log format that grows: an unknown event type is skipped, never an error.
type LogEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ContentBlock is one element of a `message.content` array. Only `type` and
// `text` are read.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Classify answers what the gate says about ONE body: the tier that captured it
// ("" when refused) and, for a refusal, the reason.
type Classify func(body string) (tier, reason string)

// Stats is the survey's answer over a corpus: per-bucket counts, the rune
// distribution, and the two rates the report is read for.
type Stats struct {
	// Files is how many session logs were read.
	Files int
	// Rounds is how many `request` events the corpus carried: one per send to
	// the model, which is the denominator the note corpus' 1.31% was measured
	// against (`占全部轮`), so the two rates are comparable.
	Rounds int
	// Bodies is how many visible assistant text bodies were found.
	Bodies int
	// Tiers maps a capturing arm (TierTopology/TierTable/TierConclusion) to its
	// count.
	Tiers map[string]int
	// Reasons maps a refusal reason to its count.
	Reasons map[string]int
	// Runes is every body's rune count, for the quantiles.
	Runes []int
	// CapturedBodies is how many bodies any arm captured (the sum of Tiers).
	CapturedBodies int
	// CapturedRounds is CapturedBodies, counted against Rounds instead of
	// against Bodies — the number that compares to the note corpus' 1.31%.
	CapturedRounds int
}

// Survey reads every session log under dir (recursively, so a profile's session
// directory is one argument) and classifies each visible assistant body with
// classify.
//
// It is READ-ONLY: no file is created, written or renamed, and a session
// database is never opened. A file that cannot be read is COUNTED and reported,
// never silently skipped (Rule 15) — a survey that quietly read half a corpus
// would print a rate about nothing.
func Survey(dir string, classify Classify) (*Stats, error) {
	files, err := SessionLogFiles(dir)
	if err != nil {
		return nil, err
	}
	s := newStats()
	for _, path := range files {
		bodies, rounds, err := readSessionLog(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		s.Files++
		s.Rounds += rounds
		for _, body := range bodies {
			s.observe(body, classify)
		}
	}
	sort.Ints(s.Runes)
	return s, nil
}

// SessionLogFiles lists every session log under dir, sorted, recursively.
func SessionLogFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat survey root: %w", err)
	}
	if !info.IsDir() {
		// A single log file is a legal corpus (the smallest sample, Rule 10.4).
		return []string{dir}, nil
	}
	var out []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != SurveySessionFileName {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk survey root: %w", err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no %s found under %s", SurveySessionFileName, dir)
	}
	return out, nil
}

// newStats returns a Stats with every counter present, so a report can never
// render a nil map as an empty arm.
func newStats() *Stats {
	return &Stats{
		Tiers:   map[string]int{},
		Reasons: map[string]int{},
	}
}

// observe records ONE body's verdict.
func (s *Stats) observe(body string, classify Classify) {
	s.Bodies++
	s.Runes = append(s.Runes, utf8.RuneCountInString(body))
	tier, reason := classify(body)
	if tier != "" {
		s.Tiers[tier]++
		s.CapturedBodies++
		s.CapturedRounds++
		return
	}
	if reason == "" {
		reason = BucketNoStructure
	}
	s.Reasons[reason]++
}

// readSessionLog returns the visible assistant bodies and the round count of one
// session log.
//
// THE BODY IS the newline-joined text of every `text` block in one assistant
// event. Measured on the corpus this was written for: every assistant event
// carries exactly ONE content block, so joining per event and joining per API
// iteration produce byte-identical bodies — and the per-event rule is the
// simpler one to state (an event is one assistant message; a round is one send).
func readSessionLog(path string) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, surveyScannerInitBytes), surveyMaxLineBytes)
	var (
		bodies []string
		rounds int
	)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev LogEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A malformed line is not the survey's business to fail on: the
			// events this reader cares about are the ones it can parse, and an
			// unparsable line cannot be one of them.
			continue
		}
		switch ev.Type {
		case surveyEventRequest:
			rounds++
		case surveyEventAssistant:
			if body, ok := assistantBody(ev.Message.Content); ok {
				bodies = append(bodies, body)
			}
		}
	}
	if err := sc.Err(); err != nil {
		// bufio.ErrTooLong is named, not swallowed: a line over the cap means the
		// corpus is not what this reader believes it is (Rule 15).
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, 0, fmt.Errorf("line exceeds %d bytes", surveyMaxLineBytes)
		}
		return nil, 0, err
	}
	return bodies, rounds, nil
}

// assistantBody extracts the visible text of one assistant event: every `text`
// block, in order, joined by newline. It reports ok=false when the event carried
// no non-blank text at all (a tool-only round is the common case: measured 1897
// tool_use blocks against 267 text blocks on the calibration corpus).
func assistantBody(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", false
	}
	var parts []string
	for _, b := range blocks {
		if b.Type != surveyBlockTypeText {
			continue
		}
		if t := strings.TrimSpace(b.Text); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

// Quantile returns the p-th percentile (0..1) of the observed rune counts. The
// slice is sorted by Survey; an empty corpus returns 0 and false, so a caller
// can print "no bodies" rather than a fabricated zero.
func (s *Stats) Quantile(p float64) (int, bool) {
	if len(s.Runes) == 0 {
		return 0, false
	}
	idx := int(p * float64(len(s.Runes)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s.Runes) {
		idx = len(s.Runes) - 1
	}
	return s.Runes[idx], true
}

// Largest returns the longest body's rune count.
func (s *Stats) Largest() (int, bool) {
	if len(s.Runes) == 0 {
		return 0, false
	}
	return s.Runes[len(s.Runes)-1], true
}

// BodyRate is the capture rate over BODIES: how much of what the console showed
// the gate keeps.
func (s *Stats) BodyRate() float64 {
	if s.Bodies == 0 {
		return 0
	}
	return float64(s.CapturedBodies) / float64(s.Bodies)
}

// RoundRate is the capture rate over ROUNDS: the spelling the note corpus'
// measured 1.31% uses (`占全部轮`), so the two are directly comparable.
func (s *Stats) RoundRate() float64 {
	if s.Rounds == 0 {
		return 0
	}
	return float64(s.CapturedRounds) / float64(s.Rounds)
}

// ArmNames returns the three capturing arms in the gate's own order.
func (s *Stats) ArmNames() []string {
	return []string{TierTopology, TierTable, TierConclusion}
}
