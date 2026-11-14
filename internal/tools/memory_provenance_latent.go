package tools

// Latent provenance harvest for distilled_qa (2026-09-11, Rule 16 follow-up).
//
// THE QUESTION THIS FILE ANSWERS: 500 live rows have an EMPTY free-text
// `sources`, yet 495 of them name a path-shaped token somewhere in their own
// topic/question/answer text. Is that in-text path the row's ORIGIN (where the
// knowledge came from) or merely its SUBJECT (what the row is about)? The
// previous pass (internal/tools/memory_provenance.go) answered only the
// FIELD-level question ("no origin is RECORDED here") and left the semantic
// question open. Fabricating an origin is the exact failure this file exists to
// prevent, so the signal had to be MEASURED against the recorded data before a
// single byte was written.
//
// THE MEASUREMENT THAT DECIDED IT (live DB, immediately after the harvest,
// 2026-09-11; re-run any time with `gopi memory provenance --latent-calibrate`):
//
//   - Of the 567 rows whose `sources` IS a recorded path, only 370 (65%)
//     resolve to a real file; 197 carry no file-shaped token that resolves.
//   - Of the 90 EMPTY-sources rows whose TOPIC BEGINS with a path that its own
//     question/answer repeats, 90/90 (100%) resolve to a real file on disk.
//
// (The recorded column is NOT a gold standard: rows like
// ~/work/GoPi/TASK-END-STORE-REPORT2.md or docs/developers/LLM_AI_Roadmap.md
// name artifacts that have since moved or never existed. Beating it is a
// low bar — but a signal that resolves MORE often than the human-recorded
// column is not a WEAKER guess than the recorded data.)
//
// What separates the two populations is the SHAPE of the mention:
//
//   - ORIGIN (harvestable): the path heads the row's topic as a declaration
//     ("hermes_cli/main.py — CLI dispatcher & update/TUI machinery") AND the
//     row's own question/answer repeats it. Here the row is a DESCRIPTION of
//     that artifact — the artifact is what the knowledge came from.
//   - SUBJECT (NOT harvestable): the path is merely mentioned inside a sentence
//     ("补法（已在 gopi internal/tools/memory.go 实现并测试）" — the row is about
//     the CJK-retrieval technique, and memory.go is where the fix happened to
//     land. Filing this row's origin as memory.go would be a fabrication.
//
// So the heading rule is the same "verbatim repetition in the addressable
// field" test the human-written `sources` column already passes, applied to the
// row's own text. Everything else is deliberately LEFT UNHARVESTED and reported
// as such; the honest residual is the deliverable, not a number to inflate.
//
// WHY ONLY 90 OF THE 502 EMPTY-sources rows — the MEASURED decomposition, which
// the binary now prints itself (`--latent-candidates`, lines 4-5) precisely
// because an earlier hand-typed version of this paragraph did not survive
// re-measurement (it claimed 292/202/3/11; the rule's own predicate says
// 491/290/200/0/201/11):
//
//	491 name a path-shaped token SOMEWHERE in topic+question+answer
//	 290 of those name a FILE-shaped path (a known extension)
//	   90  declare it as the topic heading AND repeat it  → HARVESTED
//	  200  only mention it inside a sentence             → refused (SUBJECT)
//	    0  declare it and never repeat it                → refused (clause 4)
//	 201 name only slash-joined NON-file tokens ("env_int/env_float/env_bool",
//	     "mode/owner") — path-shaped by the character class, but no file
//	  11 name no path-shaped token at all ("smoke topic")
//
// Those origins are not recoverable by a defensible signal, and this file says
// so rather than guessing. The devdoc states the split plainly.
//
// HONESTY INVARIANTS (the point of the whole exercise):
//
//   - the recorded `sources` / `source_type` are NEVER touched (additive only);
//   - a harvested row is marked with provenance `harvested` plus the evidence
//     pattern that earned it, so a reader can always tell a DERIVED origin from
//     a RECORDED one, and can re-check the derivation from the evidence alone;
//   - a row the harvest cannot defend is left UNLABELLED (never filled with a
//     guess) — the fill percentage is a diagnostic, never the goal;
//   - the classifier proves its own resolve rate against the recorded column
//     (CalibrateProvenanceLatent), because "just as good as the recorded data"
//     is a measurement, not an assertion.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Constants (Rule 1: no magic numbers / bare string keywords).
// ---------------------------------------------------------------------------

const (
	// latentSourceTypeCol is the additive column holding a HARVESTED
	// source_type. It is deliberately a DIFFERENT column from source_type:
	// that one answers "what does the RECORDED sources say", this one answers
	// "what can be defensibly INFERRED from the row's own text". Mixing the two
	// would destroy the harvested-vs-recorded distinction the deliverable
	// requires — and would silently overwrite a measured value with a guess.
	latentSourceTypeCol = "source_type_latent"

	// latentProvenanceCol carries the provenance MARKER: how the latent label
	// was obtained. Three values, all from this file's constants.
	latentProvenanceCol = "provenance"

	// latentProvHarvested marks a label DERIVED from the row's own text — never
	// a recorded origin. The evidence column names the pattern that earned it.
	latentProvHarvested = "harvested"

	// latentProvRecorded marks a row whose origin was ALREADY recorded in
	// `sources` (source_type is addressable): the latent column mirrors the
	// recorded verdict so a reader can compare the two side by side without
	// joining anything. It never overwrites — it restates.
	latentProvRecorded = "recorded"

	// latentProvNone is the honest "we cannot say": no recorded origin, and no
	// pattern in the row's own text that defensibly identifies one. This is the
	// UNTRACEABLE bucket and the devdoc reports it as a count, not as a gap.
	latentProvNone = "none"

	// latentEvidenceCol holds the EVIDENCE for a harvested label — the declared
	// heading token itself, so a reader can re-derive the verdict (and re-check
	// addressability) without re-running anything. It is exactly one token: the
	// rule harvests the heading path and only the heading path, and a second
	// token in this column would imply the rule had considered one.
	latentEvidenceCol = "provenance_evidence"

	// latentResidualBodySep joins a row's three text fields when the residual
	// tally looks for path-shaped tokens "anywhere in the row" — the same body
	// the loose 495-of-500 measurement used, so the residual base
	// 'no path token at all' means what it says. A space, because the fields
	// are prose: a token must not be manufacturable by gluing two fields.
	latentResidualBodySep = " "

	// latentBareHeadingSep are the characters that END a topic's heading
	// declaration — a space, a tab, or one of the separators headings are
	// written with ('—', '·', ':', '('). Used by latentBareFilenameHeading.
	latentBareHeadingSep = " \t—·:("

	// latentColumnProbeRootEnv names the operator's override for the
	// REPORTING probe root below. It is the honest lever for a deployment
	// whose rows describe a checkout that is not this process's own (an
	// imported tree, a peer's layout): the value was a compiled-in
	// /home/user/work/GoPi, which made the report correct on exactly one box.
	latentColumnProbeRootEnv = "GOPI_PROVENANCE_PROBE_ROOT"

	// latentColumnProbeRoot is the repository root consulted when REPORTING
	// whether a harvested path is addressable. It is a REPORTING input only —
	// no classification decision reads the filesystem, because a classifier
	// that depends on the current checkout would relabel the same row
	// differently on a different day (the row's provenance must be a property
	// of the ROW, not of today's disk).
	//
	// The value is DERIVED, with the env override above winning: a probe root
	// that is a compiled-in absolute path answers "does this machine happen to
	// be the operator's laptop", never "is the artifact addressable".
	latentColumnProbeRoot = ""
)

// latentProbeRoot resolves the REPORTING probe root: the declared override
// first, then this process's own repository. Empty when neither answers — the
// callers below then report nothing rather than probing a path that means
// nothing here.
func latentProbeRoot() string {
	if v := strings.TrimSpace(os.Getenv(latentColumnProbeRootEnv)); v != "" {
		return v
	}
	if latentColumnProbeRoot != "" {
		return latentColumnProbeRoot
	}
	if root, err := repoRootOf("."); err == nil {
		return strings.TrimSpace(root)
	}
	return ""
}

// latentRepoRoots are the trees a harvested path may resolve against: the
// probe root above, plus the extra trees the operator declares for rows whose
// artifacts live outside this checkout (the imported hermes_* rows). The
// hermes paths used to be compiled in — they are kept, because the rows that
// name them are real and their resolve rate is the evidence behind the
// harvest rule, but they are now DECLARED by name instead of assumed, so a
// box without them reports those rows as unresolvable instead of claiming a
// resolution it never made.
//
// Read BOTH by the resolve-rate report and by the calibration probe; never by
// classification.
func latentRepoRoots() []string {
	roots := []string{}
	if r := latentProbeRoot(); r != "" {
		roots = append(roots, r)
	}
	for _, extra := range strings.Split(os.Getenv(latentRepoRootsEnv), string(os.PathListSeparator)) {
		if v := strings.TrimSpace(extra); v != "" {
			roots = append(roots, v)
		}
	}
	return roots
}

// latentRepoRootsEnv is the operator's declaration of additional trees a
// harvested path may resolve against, PATH-separator joined. Unset means "this
// checkout only", which is the honest answer for a box that never imported
// another tree's rows.
const latentRepoRootsEnv = "GOPI_PROVENANCE_REPO_ROOTS"

// latentFileExtensions are the file extensions that make a path-shaped token a
// FILE rather than a directory. Measured: every one of the 90 harvested heading
// tokens carries one of these.
var latentFileExtensions = []string{
	".py", ".go", ".md", ".rs", ".sh", ".json", ".txt", ".yaml", ".yml",
	".toml", ".js", ".ts", ".sql", ".cfg", ".ini", ".db",
}

// latentMaxPathDepth bounds how deep a harvested token may be. Measured on the
// live corpus: the 90 harvested heading tokens are 2 segments deep (34) or 3
// (56) — never more. The cap is therefore generous by two segments, because it
// is a PROSE guard (an unbounded token would span half a sentence of slashes),
// not a fit to the current population.
const latentMaxPathDepth = 5

// The remaining constants are the CANDIDATE-POPULATION definition (deliverable
// a): pure SQL signatures that must reproduce the numbers reported in the
// devdoc when run by hand against the live DB.
const (
	// latentPathTokenSQL is the token character class shared by the Go regexp
	// below and mirrored in the SQL probes: a run of path characters
	// containing at least one '/'.
	latentPathTokenSQL = "[A-Za-z0-9_./+~-]*/[A-Za-z0-9_./+~-]+"
)

// latentPathTokenRe matches one path-shaped token: a run of path characters
// containing at least one '/'. Anchored at the token boundary only (not
// full-match) so it can be used both to HEAD a topic and to FIND repeats in the
// question/answer body.
var latentPathTokenRe = regexp.MustCompile(latentPathTokenSQL)

// latentTokenTrim is the punctuation trimmed off both ends of a matched token
// (measured: headings end paths with ':' '·' '(' and sentences with ',' '.'
// ')' '`'), plus CJK pairing brackets.
const latentTokenTrim = ".,;:()[]<>\"'`*·—“”‘’（）、。"

// ---------------------------------------------------------------------------
// The harvesting rule.
// ---------------------------------------------------------------------------

// LatentRule is one rung of the harvest rule. Unlike the recorded-source ladder
// (which must PARTITION every row into a bucket), this rule is deliberately
// PARTIAL: a row it cannot defend stays unlabelled. That is the design — the
// rule's virtue is what it REFUSES to claim.
type LatentRule struct {
	// Provenance is the marker written for a row this rule fires on.
	Provenance string
	// Why is the measured justification, echoed in the report.
	Why string
}

// latentHarvestRule is THE rule, in one place, so the classifier, the report and
// the tests cannot drift:
//
//	A row is HARVESTABLE iff ALL of:
//	  1. its recorded `sources` is empty (a recorded origin always wins);
//	  2. its topic BEGINS (after trimming) with a FILE-SHAPED path token;
//	  3. that token does not merely continue into a longer word (a heading
//	     token ends at a separator, never inside an identifier);
//	  4. the row's OWN question or answer repeats the token verbatim.
//
// Clause 3 is a purity guard: without it a topic like "internal/agent/loop.go" is
// fine, but a prose fragment starting with "a/b" would be read as a path. Clause
// 4 is the load-bearing one — it is the same "the addressable field names the
// artifact" test a human-written sources passes, and it is what takes the
// resolve rate from "as good as the recorded column" to 100% on the measured
// population.
func latentHarvestRule() LatentRule {
	return LatentRule{
		Provenance: latentProvHarvested,
		Why: "the row's topic DECLARES a file-shaped path as its subject heading and the row's " +
			"own question/answer repeats that path verbatim — the same test a recorded `sources` " +
			"passes; measured resolve rate 90/90 (100%) vs 370/567 (65%) for the recorded column",
	}
}

// latentHeadingToken returns the file-shaped path token a topic DECLARES as its
// heading, or "" when the topic declares none. It is the Go half of the harvest
// predicate; the SQL candidate query is the set-based mirror of it (see
// MemoryProvenanceLatentCandidates), and TestLatentPredicateMatchesSQL pins the
// two against each other on the live corpus.
func latentHeadingToken(topic string) string {
	t := strings.TrimSpace(topic)
	m := latentPathTokenRe.FindStringIndex(t)
	if m == nil || m[0] != 0 {
		// A heading token must START the topic — a path appearing mid-sentence
		// is a mention, not a declaration.
		return ""
	}
	tok := strings.Trim(t[m[0]:m[1]], latentTokenTrim)
	if !latentFileShaped(tok) {
		return ""
	}
	// Purity guard: the token must END the declaration. A letter/digit/PATH
	// character immediately after it means the match merely stopped at an
	// unsupported character (a CJK glyph glued to the path, or a longer word),
	// so the "token" is an artefact of the character class, not a real path.
	if rest := t[m[1]:]; rest != "" {
		r := rest[0]
		if isLatentPathChar(r) || r > 127 {
			return ""
		}
	}
	return tok
}

// latentFileShaped reports whether a token is a FILE-shaped artifact path:
// it carries a known file extension, and is a single path (no whitespace, not
// deeper than latentMaxPathDepth, not a URL).
func latentFileShaped(tok string) bool {
	if tok == "" || strings.ContainsAny(tok, " \t") || strings.Contains(tok, "://") {
		return false
	}
	if strings.Count(tok, "/") >= latentMaxPathDepth {
		return false
	}
	lower := strings.ToLower(tok)
	for _, ext := range latentFileExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// isLatentPathChar reports whether r is a byte the path-token character class
// accepts (ASCII path characters). Used by the purity guard.
func isLatentPathChar(r byte) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.IndexByte("_./+~-", r) >= 0
}

// latentHarvestRow applies the rule to one row. It returns the harvested
// label, the evidence string, and whether the rule fired. A row it cannot
// defend returns ok=false and NO label — the caller must leave it alone.
func latentHarvestRow(topic, question, answer string) (label, evidence string, ok bool) {
	tok := latentHeadingToken(topic)
	if tok == "" {
		return "", "", false
	}
	// Clause 4: the row's own question/answer must repeat the token. Count the
	// repetitions so the evidence can say how strongly the row declares it.
	repeats := strings.Count(question, tok) + strings.Count(answer, tok)
	if repeats == 0 {
		return "", "", false
	}
	return provenanceTypeLocalPath, tok, true
}

// ---------------------------------------------------------------------------
// Schema heal (additive).
// ---------------------------------------------------------------------------

// ensureLatentProvenanceSchema adds the three additive latent columns when
// missing — the same PRAGMA-probe + ALTER pattern as ensureProvenanceSchema.
// NEVER drops, NEVER rewrites any existing column: the latent harvest is a view
// that can be recomputed from the row text at any time.
func ensureLatentProvenanceSchema(ctx context.Context, db *sql.DB) error {
	cols, err := tableColumns(ctx, db, "distilled_qa")
	if err != nil {
		return err
	}
	for _, c := range []struct{ name, ddl string }{
		{latentSourceTypeCol, "TEXT DEFAULT ''"},
		{latentProvenanceCol, "TEXT DEFAULT ''"},
		{latentEvidenceCol, "TEXT DEFAULT ''"},
	} {
		if cols[c.name] {
			continue
		}
		// The PRAGMA probe has a TOCTOU window under concurrent first use — the
		// loser's ALTER reports "duplicate column name", which means the column
		// exists, which is all this heal needs (ensureProvenanceSchema precedent).
		if _, err := db.ExecContext(ctx, "ALTER TABLE distilled_qa ADD COLUMN "+c.name+" "+c.ddl); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Candidate population + harvest (the write path).
// ---------------------------------------------------------------------------

// LatentProvenanceResult reports one harvest pass. The counters are the
// deliverable's numbers and are reported as MEASURED:
//
//   - Total/Recorded/EmptySources: the population split (recorded origin vs no
//     origin recorded at all);
//   - HeadingShaped: empty-sources rows whose topic declares a file-shaped
//     path — the candidate population the rule considers;
//   - Harvested: rows the rule actually labelled (a subset of HeadingShaped);
//   - Unharvestable: empty-sources rows with no origin and no defensible
//     signal — the honest untraceable count;
//   - Untouched: rows already carrying a latent verdict (the idempotency
//     witness — a second run reports Harvested=0 and Untouched=Total).
type LatentProvenanceResult struct {
	Total          int `json:"total"`
	Recorded       int `json:"recorded"`
	EmptySources   int `json:"empty_sources"`
	HeadingShaped  int `json:"heading_shaped"`
	Harvested      int `json:"harvested"`
	Unharvestable  int `json:"unharvestable"`
	Untouched      int `json:"untouched"`
	ResolvesOnDisk int `json:"resolves_on_disk"`

	// Harvestable counts rows the rule can DEFEND (the verdict population) —
	// distinct from Harvested, which counts only rows this PASS wrote. On the
	// first pass the two are equal; on a second pass Harvestable is unchanged
	// while Harvested is 0, which is what makes idempotency visible rather than
	// claimed.
	Harvestable int `json:"harvestable"`

	// Written counts rows this pass actually UPDATED. It is the acceptance
	// criterion "run it twice, the second run changes 0 rows" as a number: the
	// first pass reports Written=Total, the second Written=0.
	Written int `json:"written"`

	// The RESIDUAL BASES: how the Unharvestable rows fail the rule. Each is
	// computed with the rule's OWN predicate over the same scan (never typed
	// into a report by hand — hand-typed residuals are exactly how a report
	// starts disagreeing with its own rule), and the FIVE MUST sum to
	// Unharvestable (TestLatentResidualBasesSum pins it):
	//   - ResidualNoPathToken: the row names no token of the path class at all
	//     ("smoke topic") — there is nothing to even consider. PRECISELY: the
	//     class requires a '/', which is what makes a token a PATH rather than a
	//     word; a bare filename is a different base (ResidualBareFilename).
	//   - ResidualTokenNotFileShape: it names path-shaped tokens, but none
	//     carries a known file extension, so none is a FILE (a slash-joined
	//     attribute list like "env_int/env_float/env_bool" lands here);
	//   - ResidualMentionOnly: a file-shaped path IS present, but the row's
	//     topic does not DECLARE it — the path is mentioned inside a sentence.
	//     This is the SUBJECT-vs-ORIGIN refusal, the biggest honest bucket;
	//   - ResidualDeclaredNoRepeat: the topic declares a file-shaped heading
	//     and the row's own question/answer never repeats it — the clause-4
	//     refusal (measured 0 on the live corpus; kept as a live guard);
	//   - ResidualBareFilename (see below): the topic declares a BARE filename
	//     and the row repeats it — a fifth base, checked FIRST (a row leading
	//     with "hermes_time.py" claims that file, and filing it under a weaker
	//     reason would misdescribe the claim).
	ResidualNoPathToken       int `json:"residual_no_path_token"`
	ResidualTokenNotFileShape int `json:"residual_token_not_file_shaped"`
	ResidualMentionOnly       int `json:"residual_mention_only"`
	ResidualDeclaredNoRepeat  int `json:"residual_declared_not_repeated"`

	// ResidualBareFilename counts the rung that was CONSIDERED and REFUSED —
	// the honest negative result the deliverable asks for: rows whose topic
	// declares a BARE FILENAME (no directory part — "hermes_time.py —
	// timezone-aware clock") that their own question/answer repeats. Such a row
	// meets the heading-and-repeat test in spirit, but the token names no
	// ADDRESSABLE artifact: a bare basename does not tell the next task which
	// file to read. Measured on the live corpus (2026-09-11), 14 rows declare a
	// repeated bare filename, and the basenames resolve to exactly ONE file in
	// some trees but to 178 (utils.py), 30 (cli.py) or 4 (memory.db) in others —
	// so the rung fails Rule 16's addressability test ("能被下一个任务检索到")
	// rather than the origin test, and the harvest refuses it. Reported, never
	// written.
	ResidualBareFilename int `json:"residual_bare_filename"`

	// BareFilenameDeclared counts a rung that was CONSIDERED and REFUSED — the
	// honest negative result the deliverable asks for: rows whose topic declares
	// a BARE FILENAME (no directory part — "hermes_time.py — timezone-aware
	// clock") that their own question/answer repeats. Such a row meets the
	// heading-and-repeat test in spirit, but the token names no ADDRESSABLE
	// artifact: a bare basename does not tell the next task which file to read.
	// Measured on the live corpus (2026-09-11), 14 rows declare a repeated bare
	// filename and the basenames resolve to exactly ONE file in some trees but
	// to 178 (utils.py), 30 (cli.py) or 4 (memory.db) in others — so the rung
	// fails Rule 16's addressability test ("能被下一个任务检索到") rather than the
	// origin test, and the harvest refuses it. Reported, never written.
	BareFilenameDeclared int `json:"bare_filename_declared"`
}

// LatentProvenanceRow is one harvested row as reported to a human — the
// 10-row cross-check of the acceptance criteria reads these.
type LatentProvenanceRow struct {
	ID         int    `json:"id"`
	LatentType string `json:"source_type_latent"`
	Provenance string `json:"provenance"`
	Evidence   string `json:"evidence"`
	Recorded   string `json:"recorded_sources"`
	Topic      string `json:"topic"`
	Resolves   bool   `json:"resolves_on_disk"`

	// AgreementFound/AgreementTotal are the CONTENT cross-check: how many of
	// the identifiers the row's own answer names occur verbatim in the file the
	// row was filed under (latentContentAgreement). AgreementChecked is false
	// when the file could not be read, so a rendering never shows a 0/0 as if
	// it were a failed check.
	AgreementFound   int  `json:"agreement_found"`
	AgreementTotal   int  `json:"agreement_total"`
	AgreementChecked bool `json:"agreement_checked"`
	// Already is true when the row carried a latent verdict BEFORE this pass —
	// the row-level witness behind the idempotency claim (json:"-" : it is
	// pass-state, not data). Probed through the provenance MARKER, which is set
	// for every verdict: keying it on the latent TYPE would treat the
	// legitimately-empty type of a 'none' row as "not yet examined" and rewrite
	// 426 rows on every pass (measured — the first cut of this file did exactly
	// that).
	Already bool `json:"-"`
}

// MemoryProvenanceLatentCandidates is the RE-RUNNABLE candidate query
// (deliverable a): it returns, per row, the empty-sources population split by
// whether the row's own topic declares a file-shaped path. Implemented as a Go
// scan using the SAME predicate as the harvester, so the report and the write
// path can never disagree; the SQL mirror of the predicate lives in
// latentCandidateSQL and is asserted equivalent by test.
func MemoryProvenanceLatentCandidates() (*LatentProvenanceResult, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// READ-ONLY: no schema heal here. A report must never ALTER the production
	// knowledge base — if the latent columns do not exist yet the scan reports
	// the POPULATION the rule would harvest (and Remembered stays 0), which is
	// the honest pre-harvest answer.
	rep, _, err := latentScan(ctx, db)
	return rep, err
}

// latentColumnsPresent reports whether the additive latent columns exist.
// Best-effort: an unreadable schema answers false (the scan then degrades to
// "nothing harvested yet" instead of failing a read-only report).
func latentColumnsPresent(ctx context.Context, q QueryExecer) bool {
	cols, err := tableColumns(ctx, q, "distilled_qa")
	if err != nil {
		return false
	}
	return cols[latentSourceTypeCol] && cols[latentProvenanceCol]
}

// latentCandidateSQL renders the candidate query — the set-based mirror of the
// heading rule, printed by the CLI so a reader can reproduce the population
// WITHOUT this binary:
//
//	./bin/gopi memory provenance --latent-candidates | sed -n '/^SELECT/,$p' | sqlite3 -readonly /home/user/.gopi/memory.db
//
// It is RENDERED from latentFileExtensions and latentTokenTrim ({{tok}}), so the
// mirror cannot drift from the predicate the harvest applies. Two decisions make
// it EXACT rather than approximate, both forced by measurement on the live
// corpus:
//
//  1. The subject is the topic's FIRST WHITESPACE-SEPARATED WORD (wordExpr),
//     not the whole topic. A whole-topic LIKE mirror reported 91 candidates for
//     a rule that defends 90 — the extra row's topic merely MENTIONS a path in
//     prose ("Merge-conflict resolution in a gate allow-list
//     (internal/standards/baseline.go) …"), which the rule correctly refuses.
//  2. The extension test is LIKE '%<ext>' (ends-with), not GLOB '*.<ext>*'. GLOB
//     is case-SENSITIVE and its `*` will not cross a '-', so 'a-z0-9_./+~-*.py'
//     silently failed on "hermes_cli/main.py" — measured 0 matches where 90 were
//     expected. An ends-with LIKE over lower() is the same test the Go rule
//     (strings.HasSuffix on lower()) performs.
func latentCandidateSQL() string {
	// latentTokenTrim carries an apostrophe; SQL string literals escape it by
	// doubling — the trim set is otherwise embedded verbatim.
	trim := strings.ReplaceAll(latentTokenTrim, "'", "''")
	word := "rtrim(substr(topic,1,instr(topic||' ',' ')-1),'" + trim + "')"
	terms := make([]string, 0, len(latentFileExtensions))
	for _, ext := range latentFileExtensions {
		terms = append(terms, "lower(w) LIKE '%"+ext+"'")
	}
	// A CTE names the subject once so the printed query stays readable (it is
	// deliverable "a re-runnable query", not a wall of text) and so the
	// question/answer repetition test reads as plainly as the Go rule does.
	return "WITH empty_sources AS (\n" +
		"  SELECT id, question, answer,\n" +
		"         " + word + " AS w\n" +
		"    FROM distilled_qa\n" +
		"   WHERE trim(coalesce(sources,''))=''\n" +
		")\n" +
		"SELECT count(*) FROM empty_sources\n" +
		" WHERE w LIKE '%/%'\n" +
		"   AND (" + strings.Join(terms, " OR ") + ")\n" +
		"   AND (instr(question,w) > 0 OR instr(answer,w) > 0);"
}

// LatentCandidateSQL returns the human-runnable candidate query (deliverable a).
func LatentCandidateSQL() string { return latentCandidateSQL() }

// MemoryProvenanceLatentHarvest runs the harvest: it computes the latent verdict
// for every distilled_qa row and records it in the three additive columns.
//
// Contract:
//   - ADDITIVE ONLY: the single UPDATE writes source_type_latent, provenance and
//     provenance_evidence. `sources`, `source_type`, topic, question, answer and
//     search_kw are never touched (proven by dumping them before/after and
//     diffing — the test does exactly that).
//   - HONEST: a row the rule cannot defend is written as provenance='none' with
//     an EMPTY latent type — never a guess, never a filled-in path.
//     A row carrying a recorded origin is written as provenance='recorded',
//     mirroring source_type so the two are comparable side by side.
//   - IDEMPOTENT: the UPDATE's WHERE anchors on the latent columns being UNSET,
//     so a second run writes 0 rows (Harvested=0, Untouched=Total).
//   - TRANSACTIONAL: one BEGIN IMMEDIATE; a crash mid-pass leaves the table
//     untouched rather than half-labelled.
func MemoryProvenanceLatentHarvest() (*LatentProvenanceResult, error) {
	ctx := context.Background()
	db, err := openDB(memoryDBPath(), false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := ensureLatentProvenanceSchema(ctx, db); err != nil {
		return nil, fmt.Errorf("latent harvest: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("latent harvest: %w", err)
	}
	defer func() { _ = tx.Rollback() }() //stylegate:ignore after a successful Commit the rollback is a no-op; recording it would log a false failure every run

	rep, rows, err := latentScan(ctx, tx)
	if err != nil {
		return nil, err
	}
	rep.Harvested = 0 // recomputed below from what this pass actually WROTE

	// Write EVERY row's verdict — including the honest 'none' — because the
	// point of the column is to distinguish "no origin recorded AND none
	// recoverable" from "not yet examined". Idempotency is not achieved by
	// writing fewer rows: it is achieved by the WHERE clause, which touches
	// only rows whose latent columns are still unset.
	written := 0
	recorded := 0
	for _, r := range rows {
		if r.Already {
			recorded++
			continue
		}
		res, err := tx.ExecContext(ctx,
			"UPDATE distilled_qa SET "+latentSourceTypeCol+"=?, "+latentProvenanceCol+"=?, "+
				latentEvidenceCol+"=? WHERE id=? AND ("+latentProvenanceCol+" IS NULL OR "+
				latentProvenanceCol+"='')",
			r.LatentType, r.Provenance, r.Evidence, r.ID)
		if err != nil {
			return nil, fmt.Errorf("latent harvest: write row %d: %w", r.ID, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("latent harvest: write row %d: %w", r.ID, err)
		}
		if int(n) > 0 {
			written += int(n)
			if r.Provenance == latentProvHarvested {
				rep.Harvested++
			}
		}
	}
	// Untouched counts rows that ALREADY carried a verdict when the pass began —
	// the idempotency witness (a second run: Harvested=0, Written=0,
	// Untouched=Total).
	rep.Untouched = recorded
	rep.Written = written
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("latent harvest: %w", err)
	}
	return rep, nil
}

// latentScan computes the latent verdict for every row, using the SAME query
// interface as the write path (so the harvest never labels a row the candidate
// report did not count). It returns the aggregate report, the per-row verdicts,
// and the rows the rule refused to label (needed by the devdoc's residual
// count).
func latentScan(ctx context.Context, q QueryExecer) (*LatentProvenanceResult, []LatentProvenanceRow, error) {
	rep := &LatentProvenanceResult{}
	// Both additive column families may be absent on a corpus that predates
	// them (a fresh test DB, or a production DB before the first backfill). The
	// scan degrades to a literal empty rather than failing: it then reports the
	// POPULATION the rule would label, with no prior verdicts — the honest
	// pre-harvest answer, and never a fabricated one.
	cols, err := tableColumns(ctx, q, "distilled_qa")
	if err != nil {
		return nil, nil, fmt.Errorf("latent scan: %w", err)
	}
	latentSel, recordedSel := "''", "''"
	if cols[latentProvenanceCol] {
		latentSel = "coalesce(" + latentProvenanceCol + ",'')"
	}
	if cols[provenanceSourceTypeCol] {
		recordedSel = "coalesce(" + provenanceSourceTypeCol + ",'')"
	}
	rows, err := q.QueryContext(ctx,
		"SELECT id, topic, question, answer, coalesce(sources,''), "+recordedSel+", "+
			latentSel+" FROM distilled_qa ORDER BY id")
	if err != nil {
		return nil, nil, fmt.Errorf("latent scan: %w", err)
	}
	defer rows.Close()

	var out []LatentProvenanceRow
	var emptyRows []LatentProvenanceRow
	for rows.Next() {
		var r LatentProvenanceRow
		var topic, question, answer, sources, recordedType, priorProvenance string
		if err := rows.Scan(&r.ID, &topic, &question, &answer, &sources, &recordedType, &priorProvenance); err != nil {
			return nil, nil, fmt.Errorf("latent scan: %w", err)
		}
		r.Already = priorProvenance != ""
		rep.Total++
		r.Topic = topic

		if strings.TrimSpace(sources) != "" {
			// A RECORDED origin always wins: the latent column restates it and
			// never competes with it. provenance='recorded' is what makes the
			// harvested-vs-recorded distinction readable at a glance.
			rep.Recorded++
			r.Provenance = latentProvRecorded
			r.LatentType = recordedType
			r.Evidence = sources
			r.Recorded = sources
			out = append(out, r)
			continue
		}

		rep.EmptySources++
		if latentHeadingToken(topic) != "" {
			rep.HeadingShaped++
		}
		label, evidence, ok := latentHarvestRow(topic, question, answer)
		if !ok {
			// The honest residual: no origin recorded, and nothing in the row
			// that defensibly identifies one. Deliberately labelled 'none'
			// rather than filled with the first path token the row mentions.
			rep.Unharvestable++
			tallyLatentResidual(rep, topic, question, answer)
			r.Provenance = latentProvNone
			r.Evidence = ""
			emptyRows = append(emptyRows, r)
			out = append(out, r)
			continue
		}
		rep.Harvestable++
		r.LatentType = label
		r.Provenance = latentHarvestRule().Provenance
		r.Evidence = evidence
		if latentPathResolves(evidence) {
			rep.ResolvesOnDisk++
			r.Resolves = true
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("latent scan: %w", err)
	}
	return rep, out, nil
}

// MemoryProvenanceLatentRows returns the harvested rows as a report — the
// 10-row manual cross-check reads straight off it. Read-only.
func MemoryProvenanceLatentRows(limit int) ([]LatentProvenanceRow, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if !latentColumnsPresent(ctx, db) {
		return nil, fmt.Errorf("latent rows: %s is absent — run --harvest-latent first", latentSourceTypeCol)
	}
	if limit <= 0 {
		limit = latentCrossCheckRows
	}
	q := "SELECT id, " + latentSourceTypeCol + ", " + latentProvenanceCol + ", " + latentEvidenceCol +
		", coalesce(sources,''), topic, coalesce(answer,'') FROM distilled_qa WHERE " +
		latentProvenanceCol + "=? ORDER BY id LIMIT ?"
	rows, err := db.QueryContext(ctx, q, latentProvHarvested, limit)
	if err != nil {
		return nil, fmt.Errorf("latent rows: %w", err)
	}
	defer rows.Close()
	var out []LatentProvenanceRow
	for rows.Next() {
		var r LatentProvenanceRow
		var answer string
		if err := rows.Scan(&r.ID, &r.LatentType, &r.Provenance, &r.Evidence, &r.Recorded, &r.Topic, &answer); err != nil {
			return nil, fmt.Errorf("latent rows: %w", err)
		}
		r.Resolves = latentPathResolves(r.Evidence)
		r.AgreementFound, r.AgreementTotal, r.AgreementChecked = latentContentAgreement(r.Evidence, answer)
		out = append(out, r)
	}
	return out, rows.Err()
}

// latentCrossCheckRows is the cross-check sample size the acceptance criteria
// require (10 rows, hand-verified against the tree).
const latentCrossCheckRows = 10

// latentIdentifierRe matches an identifier-shaped word in a row's answer — the
// candidate list the content cross-check looks up in the harvested file.
var latentIdentifierRe = regexp.MustCompile("[A-Za-z_][A-Za-z0-9_]{2,}")

// latentIdentifierMinLen is the shortest word accepted as an identifier: three
// characters. Measured rationale: at two characters the candidate list fills
// with prose fragments ("is", "of") that match any file and inflate the
// agreement rate; at four, real short symbols (the row's own `TickLabel`) start
// dropping out of the denominator, weakening the check rather than tightening
// it. Three is the smallest length at which every candidate is either a
// snake_case / camelCase symbol or a word long enough that its presence in the
// file is meaningful evidence.
const latentIdentifierMinLen = 3

// latentIdentifierTokens returns the identifier-shaped candidates in a row's
// answer, per latentIdentifierRe + the symbol filter: a candidate counts only
// when it carries an underscore or an internal capital (snake_case or
// camelCase). That filter is what makes the check measure SYMBOLS rather than
// English words — "session" appearing in a file says nothing, "NewSessionClock"
// appearing in it says the row really does describe that file.
func latentIdentifierTokens(answer string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range latentIdentifierRe.FindAllString(answer, -1) {
		if len(w) < latentIdentifierMinLen || !latentSymbolShaped(w) {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// latentSymbolShaped reports whether a word is symbol-shaped (snake_case or
// camelCase) rather than an ordinary prose word.
func latentSymbolShaped(w string) bool {
	if strings.Contains(w, "_") {
		return true
	}
	for i := 1; i < len(w); i++ {
		if w[i] >= 'A' && w[i] <= 'Z' && w[i-1] >= 'a' && w[i-1] <= 'z' {
			return true
		}
	}
	return false
}

// latentContentAgreement measures CONTENT provenance for one harvested row: of
// the identifiers the row's own answer names, how many occur verbatim in the
// file the row was filed under? It returns (found, total, ok); ok is false when
// the file cannot be read, in which case there is nothing honest to report.
//
// WHY A PROGRAM AND NOT A HAND COUNT: the devdoc's first cut carried a table of
// per-row counts a human had picked identifiers for, and it did not survive
// re-measurement — no stated rule reproduces it (the same answers yield
// 145/149 under every candidate rule tried, not the tabled 159/163 and not the
// headline 199/203 it summed to). A number nobody can recompute is a number the
// next reader has to trust; this one they can re-run.
func latentContentAgreement(evidence, answer string) (int, int, bool) {
	path := ""
	for _, root := range latentRepoRoots() {
		p := filepath.Join(root, strings.TrimPrefix(evidence, "/"))
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return 0, 0, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		// A directory-shaped evidence token, or an unreadable file: report
		// nothing rather than a misleading 0/0.
		return 0, 0, false
	}
	text := string(body)
	toks := latentIdentifierTokens(answer)
	found := 0
	for _, t := range toks {
		if strings.Contains(text, t) {
			found++
		}
	}
	return found, len(toks), true
}

// latentPathResolves reports whether a harvested path names a real file under
// one of latentRepoRoots — the ADDRESSABILITY test of Rule 16 ("能被下一个任务
// 检索到"). Reporting only.
func latentPathResolves(tok string) bool {
	if tok == "" {
		return false
	}
	for _, root := range latentRepoRoots() {
		p := filepath.Join(root, strings.TrimPrefix(tok, "/"))
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Calibration (the measurement that justifies the rule).
// ---------------------------------------------------------------------------

// LatentCalibrationReport compares the harvested signal against the recorded
// column — the measurement that decides whether harvesting is defensible.
//
// WHY IT MUST EXIST: "the in-text path is the origin" is a claim, and a claim
// about provenance is exactly the kind that gets fabricated. The only honest
// way to support it is to test the SAME rule against rows whose origin is
// ALREADY KNOWN (source_type='local_path'): if the rule fires on rows the human
// recorded a path for, and stays silent elsewhere, it measures something real.
type LatentCalibrationReport struct {
	// RecordedRows is the population whose `sources` records a path.
	RecordedRows int `json:"recorded_rows"`
	// RecordedResolving counts those whose recorded path exists on disk.
	RecordedResolving int `json:"recorded_resolving"`
	// HeadingRows is the empty-sources population whose topic declares a
	// file-shaped path.
	HeadingRows int `json:"heading_rows"`
	// HeadingResolving counts those whose declared path exists on disk.
	HeadingResolving int `json:"heading_resolving"`
}

// RecordedResolveRate / HeadingResolveRate are the two numbers the rule's
// justification rests on.
func (r *LatentCalibrationReport) RecordedResolveRate() float64 {
	return latentRate(r.RecordedResolving, r.RecordedRows)
}

// HeadingResolveRate is the harvested population's resolve rate.
func (r *LatentCalibrationReport) HeadingResolveRate() float64 {
	return latentRate(r.HeadingResolving, r.HeadingRows)
}

// latentRate is a guard-free percentage helper (0 for an empty denominator, so a
// dry corpus reports 0% rather than NaN).
func latentRate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

// CalibrateProvenanceLatent measures both populations' resolve rates — the
// evidence behind the rule, recomputable at any time. Read-only.
func CalibrateProvenanceLatent() (*LatentCalibrationReport, error) {
	ctx := context.Background()
	db, err := openSearchDB(ctx, memoryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rep := &LatentCalibrationReport{}

	// Recorded population: does the RECORDED sources path actually exist?
	rows, err := db.QueryContext(ctx,
		"SELECT coalesce(sources,'') FROM distilled_qa WHERE "+provenanceSourceTypeCol+"=?",
		provenanceTypeLocalPath)
	if err != nil {
		return nil, fmt.Errorf("calibrate latent: %w", err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			if cerr := rows.Close(); cerr != nil {
				// Both errors are real and both stay in the chain: the scan
				// error is the cause, the close error is the reason iteration
				// ended. errors.Join keeps BOTH findable (errors.Is against
				// either), which is what the %v form lost.
				return nil, fmt.Errorf("calibrate latent: scan: %w", errors.Join(err, cerr))
			}
			return nil, fmt.Errorf("calibrate latent: %w", err)
		}
		rep.RecordedRows++
		toks := latentPathTokens(s)
		for _, t := range toks {
			if latentPathResolves(t) {
				rep.RecordedResolving++
				break
			}
		}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("calibrate latent: close: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("calibrate latent: %w", err)
	}

	// Harvested population: does the row's DECLARED heading path exist?
	rows2, err := db.QueryContext(ctx,
		"SELECT topic, question, answer FROM distilled_qa WHERE trim(coalesce(sources,''))=''")
	if err != nil {
		return nil, fmt.Errorf("calibrate latent: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var topic, question, answer string
		if err := rows2.Scan(&topic, &question, &answer); err != nil {
			return nil, fmt.Errorf("calibrate latent: %w", err)
		}
		tok := latentHeadingToken(topic)
		if tok == "" {
			continue
		}
		rep.HeadingRows++
		if latentPathResolves(tok) {
			rep.HeadingResolving++
		}
	}
	return rep, rows2.Err()
}

// latentPathTokens extracts every path-shaped token from a string, trimmed of
// surrounding punctuation — used by the calibration probe on RECORDED sources.
func latentPathTokens(s string) []string {
	var out []string
	for _, raw := range latentPathTokenRe.FindAllString(s, -1) {
		t := strings.Trim(raw, latentTokenTrim)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// latentBareFilenameHeading returns the BARE FILENAME a topic declares as its
// heading, or "" — the token before the first separator, when that token is a
// single file-shaped name with NO directory part. It is the rung the harvest
// deliberately does NOT fire on (see BareFilenameDeclared): a bare basename is
// not addressable, because the same name can exist many times in a tree.
func latentBareFilenameHeading(topic string) string {
	t := strings.TrimSpace(topic)
	if t == "" {
		return ""
	}
	i := strings.IndexAny(t, latentBareHeadingSep)
	if i <= 0 {
		return ""
	}
	tok := strings.TrimRight(t[:i], latentTokenTrim)
	if tok == "" || strings.Contains(tok, "/") || strings.Contains(tok, "://") {
		return ""
	}
	if !latentFileShaped(tok) {
		return ""
	}
	return tok
}

// tallyLatentResidual classifies ONE unharvestable row into the base that made
// the rule refuse it, so the report's residual numbers come from the rule's OWN
// predicate rather than from a number a human typed while writing the report.
// A typo here is impossible to hide — the bases are asserted to sum to
// Unharvestable (TestLatentResidualBasesSum) — which is exactly what the
// hand-typed version could not do.
//
// The order of the tests IS the taxonomy: a row is attributed to the FIRST
// reason it fails the rule, so each base means "this is what stopped it".
func tallyLatentResidual(rep *LatentProvenanceResult, topic, question, answer string) {
	if bare := latentBareFilenameHeading(topic); bare != "" &&
		(strings.Contains(question, bare) || strings.Contains(answer, bare)) {
		// The bare-filename rung: a filename declared AND repeated, yet not
		// ADDRESSABLE. It is checked FIRST because it is the strongest thing
		// the row says about itself — a topic that leads with "hermes_time.py"
		// declares that file, and filing the row under a weaker base ("names
		// only non-file tokens", because its body happens to contain
		// "path/env/platform") would misdescribe what the row claimed.
		rep.ResidualBareFilename++
		return
	}
	toks := latentPathTokens(topic + latentResidualBodySep + question + latentResidualBodySep + answer)
	if len(toks) == 0 {
		rep.ResidualNoPathToken++
		return
	}
	fileShaped := 0
	for _, t := range toks {
		if latentFileShaped(t) {
			fileShaped++
		}
	}
	if fileShaped == 0 {
		rep.ResidualTokenNotFileShape++
		return
	}
	// A file-shaped path IS present. Either the topic declared it and the row
	// never repeated it (clause 4 refused), or it was only mentioned (clause 2
	// refused) — the two are distinguished by the heading predicate itself.
	if h := latentHeadingToken(topic); h != "" && latentFileShaped(h) {
		rep.ResidualDeclaredNoRepeat++
		return
	}
	rep.ResidualMentionOnly++
}
