package standardgate

// gapdoc_contract_gate.go — 阶段 3 差距文档（`devdoc/competitor/gap-<tool_id>.md`）
// 的形态契约，PROJECT.md 的 Rule 1/14 落地在这里（等级名与桶名取常量）。
//
// WHY THIS FILE EXISTS, measured 2026-11-06. Two delivered gap documents were
// written as FOUR tables each, and each of the four tables carried ALL SEVENTEEN
// dimensions — 68 rows = 17 × 4. The tables were not buckets; they were four
// copies of one full table. The defect is not cosmetic, because the two
// MUTUALLY EXCLUSIVE buckets both claimed the same dimension:
//
//	devdoc/competitor/gap-astrbot.md:86   | D01 | none | partial | ...  under 表 3 — 我有敌无（我方有、对方 none）
//	devdoc/competitor/gap-cline.md:86     (all seventeen rows under 表 3)
//
// A row whose 我方 level is `none` standing under a heading that reads
// 「我方有、对方 `none`」 is a self-contradiction, and it is the reason this
// gate is about FORM and ENTRY LOGIC only: it never judges whether a level is
// right, only whether the row's bucket agrees with its own two levels.
//
// THE ROOT CAUSE, and why the contract below has SIX verdicts rather than the
// historic four. A bucket vocabulary is a FUNCTION of (our level, opponent
// level) onto names, and it must be TOTAL — every one of the sixteen level
// pairs has to land somewhere — or the author is left holding rows with no
// home and does the only thing available: copies the whole table into every
// bucket. The historic four (敌有我无 / 敌强我弱 / 我有敌无 / 平手) are not
// total: `full` vs `partial` — 15 of 17 rows in both delivered documents — has
// no bucket at all. So the four tables were full of rows they did not own, and
// the prose had to apologise for it in the same breath:
//
//	devdoc/competitor/gap-cline.md:  「表 3 行数 = 17，但 我有敌无 在本对比里是空集」
//
// A table of 17 rows whose own caption says the bucket is empty is the defect
// stating itself. The contract therefore ADDS 我强敌弱 (ours strictly higher,
// both present) and 不可比 (either side `unknown`, framework §2 A4), and the six
// are provably total and mutually exclusive over the four levels.
//
// WHAT THE GATE CHECKS (deterministic; no judgement call, the bar Rules 16–21
// set). For every file matching `devdoc/competitor/gap-*.md`:
//
//  1. THE MAIN TABLE has EXACTLY gapDocDimCount rows, one per dimension id, and
//     the ids are exactly D01..D17 with no duplicate and no gap.
//  2. EACH ROW'S VERDICT CELL equals gapDocVerdictFor(our, opp) — the mechanical
//     function, computed from that row's own two level cells. A hand-written
//     verdict that disagrees with its levels is reported with both values.
//  3. EACH LEVEL CELL is one of the four framework levels (§2). Any other
//     spelling (`N/A`, `-`, empty) is reported rather than coerced.
//  4. THE BUCKET VIEW declares every verdict EXACTLY ONCE — including the empty
//     ones, which are written as an em dash. A bucket that is simply absent is
//     an error: an undefined bucket cannot be told apart from a forgotten one.
//  5. EVERY DIMENSION IS LISTED IN THE BUCKET ITS OWN VERDICT NAMES, exactly
//     once across all buckets. This is the arm the 2026-11-06 defect fails: a
//     `我方=none / 对方=partial` row listed under 我有敌无 is named with its two
//     levels and the bucket it was found in.
//
// The union rule is stated as an EQUALITY with the dimension set, not as a
// count, so "17 rows, all in one bucket" (what both delivered documents did)
// fails on membership rather than passing on arithmetic.
//
// WHAT IT DELIBERATELY DOES NOT DO. It never edits a level, an evidence cell or
// a narrative; it is a READER. Whether `D11` really is `partial` for a given
// competitor is a collection question judged in stage 2/3, and a gate that
// adjudicated it would be re-deciding collected evidence with a regex. Form and
// entry logic are what a machine can hold an author to, so they are all it holds.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// the vocabulary — Rule 1: names, never bare literals at the use site
// ---------------------------------------------------------------------------

// The four framework levels (devdoc/competitor-analysis-framework.md §2).
const (
	levelFull    = "full"
	levelPartial = "partial"
	levelNone    = "none"
	levelUnknown = "unknown"
)

// gapLevelScores is the only place a level's ordinal lives. `unknown` is
// deliberately ABSENT: it is not a third rung below `none`, it is the absence
// of a reading (§2 A4), so gapLevelScore reports it as non-comparable rather
// than giving it a number that could be compared.
var gapLevelScores = map[string]int{
	levelFull:    2,
	levelPartial: 1,
	levelNone:    0,
}

// The six verdicts. The first four are the historic names the delivered
// documents used; 我强敌弱 and 不可比 complete the function (see the header).
const (
	verdictEnemyHasOursNot = "敌有我无"
	verdictEnemyStronger   = "敌强我弱"
	verdictOursHasEnemyNot = "我有敌无"
	verdictOursStronger    = "我强敌弱"
	verdictEven            = "平手"
	verdictIncomparable    = "不可比"
)

// gapVerdicts is the complete bucket set, in the order the bucket view must
// declare them. Ordering it here (rather than discovering it from the
// document) is what lets a MISSING bucket be an error.
var gapVerdicts = []string{
	verdictEnemyHasOursNot,
	verdictEnemyStronger,
	verdictOursHasEnemyNot,
	verdictOursStronger,
	verdictEven,
	verdictIncomparable,
}

const (
	// gapDocDirRel is where stage 3's per-tool documents live.
	gapDocDirRel = "devdoc/competitor"
	// gapDocGlob selects them. A document of this shape that this glob does not
	// match is out of the gate's reach by design — the naming IS the contract.
	gapDocGlob = "gap-*.md"
	// gapDocDimCount is the number of frozen dimensions (§3): seventeen.
	gapDocDimCount = 17
	// gapDocDimPrefix / gapDocDimDigits spell a dimension id (D01 … D17).
	gapDocDimPrefix = "D"
	gapDocDimDigits = 2
	// gapMainRowFields is how many cells a main-table row must carry:
	// 维度 | 我方等级 | 对方等级 | 判定 | 证据 (the evidence cell may itself
	// contain no pipe; the parser refuses a row that has more, because a
	// shifted cell is how a verdict silently becomes evidence).
	gapMainRowFields = 5
	// gapBucketRowFields is 桶 | 维度清单.
	gapBucketRowFields = 2
	// gapCellSep is the table's cell separator.
	gapCellSep = "|"
	// gapBucketEmptyMark is how an empty bucket is written. A bucket's absence
	// and its emptiness must not look alike, so the mark is mandatory.
	gapBucketEmptyMark = "—"
	// gapDimListSep splits a bucket's dimension list.
	gapDimListSep = ","
	// gapLevelWrap is the character an author may decorate a cell with. Levels
	// and verdicts are rendered in backticks throughout the corpus, so the
	// parser strips it rather than making the contract about punctuation.
	gapLevelWrap = "`"
)

// gapBucketViewHeadingCue marks the bucket-view section by name. A bucket view
// an author names otherwise is not found, so the cue is part of the contract
// rather than a convenience.
const gapBucketViewHeadingCue = "桶视图"

// gapMainRowRE matches a main-table row's first cell: `| D01 | …`.
var gapMainRowRE = regexp.MustCompile(`^\| *D([0-9]{2}) *\|`)

// gapDimID renders a dimension id from its number.
func gapDimID(n int) string {
	return fmt.Sprintf("%s%0*d", gapDocDimPrefix, gapDocDimDigits, n)
}

// gapDimSet is the expected dimension set, D01..D17.
func gapDimSet() map[string]bool {
	set := make(map[string]bool, gapDocDimCount)
	for i := 1; i <= gapDocDimCount; i++ {
		set[gapDimID(i)] = true
	}
	return set
}

// gapCell normalises one table cell: surrounding space and the render wrapper.
func gapCell(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), gapLevelWrap))
}

// gapLevelScore returns a level's ordinal and whether it is comparable at all.
// `unknown` is explicitly NOT comparable — that is §2 A4, and it is the one
// place a level is deliberately not a number.
func gapLevelScore(level string) (int, bool) {
	s, ok := gapLevelScores[gapCell(level)]
	return s, ok
}

// gapLevelKnown reports whether the token is any of the four framework levels.
func gapLevelKnown(level string) bool {
	l := gapCell(level)
	if l == levelUnknown {
		return true
	}
	_, ok := gapLevelScores[l]
	return ok
}

// gapDocVerdictFor is THE mechanical function: (我方级, 对方级) -> 桶.
//
// It is total and mutually exclusive over the four levels, which is the whole
// reason the vocabulary carries six names instead of the historic four. The
// error arm exists for a token outside §2 — a parser that guessed here would
// manufacture a bucket for a level nobody defined.
func gapDocVerdictFor(our, opp string) (string, error) {
	o, p := gapCell(our), gapCell(opp)
	if !gapLevelKnown(o) {
		return "", fmt.Errorf("我方等级 %q 不是框架 §2 的四级之一（%s/%s/%s/%s）",
			o, levelFull, levelPartial, levelNone, levelUnknown)
	}
	if !gapLevelKnown(p) {
		return "", fmt.Errorf("对方等级 %q 不是框架 §2 的四级之一（%s/%s/%s/%s）",
			p, levelFull, levelPartial, levelNone, levelUnknown)
	}
	// A4: `unknown` is "no channel was ever executed", not a low score. One
	// unknown side makes the pair undecidable — never 平手, which is the
	// mis-writing §2 A4 forbids by name.
	if o == levelUnknown || p == levelUnknown {
		return verdictIncomparable, nil
	}
	so, _ := gapLevelScore(o) // both sides are known here
	sp, _ := gapLevelScore(p) // so the score lookups cannot miss
	switch {
	case so == 0 && sp == 0:
		// Both absent. Equal by level, and the narrative is where the author
		// says whether that is a shared gap or an unexamined one.
		return verdictEven, nil
	case so == 0:
		return verdictEnemyHasOursNot, nil
	case sp == 0:
		return verdictOursHasEnemyNot, nil
	case sp > so:
		return verdictEnemyStronger, nil
	case so > sp:
		return verdictOursStronger, nil
	default:
		return verdictEven, nil
	}
}

// ---------------------------------------------------------------------------
// parsing + checking
// ---------------------------------------------------------------------------

// gapMainRow is one row of the main table.
type gapMainRow struct {
	dim     string
	our     string
	opp     string
	verdict string
	line    int
}

// gapDoc is a parsed gap document: its main rows and its bucket view.
type gapDoc struct {
	rows        []gapMainRow
	buckets     map[string][]string // bucket name -> dimension ids it lists
	bucketOrder []string            // bucket names as declared, for a stable report
}

// gapParseError renders a violation with the line an author can open.
func gapParseError(line int, format string, args ...any) string {
	return fmt.Sprintf("line %d: %s", line, fmt.Sprintf(format, args...))
}

// parseGapDoc reads a gap document into its two checkable halves. It reports
// violations of the SHAPE (a row that is not five cells, a bucket row with no
// dimension cell, a bucket declared twice) and never coerces one away.
func parseGapDoc(text string) (*gapDoc, []string) {
	doc := &gapDoc{buckets: make(map[string][]string)}
	var problems []string
	bucketSet := make(map[string]bool, len(gapVerdicts))
	for _, v := range gapVerdicts {
		bucketSet[v] = true
	}

	inBucketView := false
	for i, raw := range strings.Split(text, "\n") {
		line := i + 1
		trimmed := strings.TrimSpace(raw)
		// The bucket view is a SECTION, and the section boundary is what makes
		// an unknown bucket name reportable: outside the section, a two-cell
		// row is an ordinary table row (the 判定口径 legend), inside it, a
		// two-cell row whose name is not one of the six is a typo'd bucket.
		if strings.HasPrefix(trimmed, "#") {
			inBucketView = strings.Contains(trimmed, gapBucketViewHeadingCue)
			continue
		}
		if !strings.HasPrefix(trimmed, gapCellSep) {
			continue
		}
		cells := strings.Split(strings.TrimSpace(raw), gapCellSep)
		// `| a | b |` splits into ["", " a ", " b ", ""] — drop the two edges.
		if len(cells) < 3 {
			continue
		}
		cells = cells[1 : len(cells)-1]
		if len(cells) == 0 {
			continue
		}
		first := gapCell(cells[0])

		if m := gapMainRowRE.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
			id := gapDocDimPrefix + m[1]
			// The first FOUR cells are the contract; the evidence cell is free
			// text that legitimately carries a `|` (a shell pipeline, a grep
			// alternation — measured in the delivered documents), so a row is
			// split at its first four separators and the remainder is the
			// evidence VERBATIM. A row with fewer than four is a defect; a row
			// with more is not, and re-joining must never rewrite a byte of it.
			if len(cells) < gapMainRowFields {
				problems = append(problems, gapParseError(line,
					"主表行 %s 只有 %d 个单元格，契约至少 %d 个（维度|我方等级|对方等级|判定|证据）",
					id, len(cells), gapMainRowFields))
				continue
			}
			doc.rows = append(doc.rows, gapMainRow{
				dim:     id,
				our:     gapCell(cells[1]),
				opp:     gapCell(cells[2]),
				verdict: gapCell(cells[3]),
				line:    line,
			})
			continue
		}

		// A table's own header and separator rows are not data. They are
		// recognised by shape (never by a bucket name) so the legend tables
		// elsewhere in the document stay out of the bucket section's way.
		if first == "---" || strings.HasPrefix(first, "--") || first == "桶" {
			continue
		}
		if bucketSet[first] || inBucketView {
			if !bucketSet[first] {
				problems = append(problems, gapParseError(line,
					"桶视图里有未知桶 %q（合法桶：%s）",
					first, strings.Join(gapVerdicts, " / ")))
				continue
			}
			if gapContainsString(doc.bucketOrder, first) {
				problems = append(problems, gapParseError(line,
					"桶 %s 被声明了两次（桶视图里每个桶恰好一行）", first))
				continue
			}
			if len(cells) != gapBucketRowFields {
				problems = append(problems, gapParseError(line,
					"桶行 %s 有 %d 个单元格，契约为 %d 个（桶|维度清单）",
					first, len(cells), gapBucketRowFields))
				continue
			}
			doc.bucketOrder = append(doc.bucketOrder, first)
			doc.buckets[first] = gapSplitDimList(gapCell(cells[1]))
		}
	}
	return doc, problems
}

// gapSplitDimList turns a bucket's dimension cell into ids. The empty mark, a
// hyphen and an empty cell all mean "no members"; anything else is split on the
// declared separator (a space is tolerated because a hand-written list often
// uses it).
func gapSplitDimList(cell string) []string {
	c := strings.TrimSpace(cell)
	if c == "" || c == gapBucketEmptyMark || c == "-" {
		return nil
	}
	fields := strings.FieldsFunc(c, func(r rune) bool {
		return string(r) == gapDimListSep || r == ' ' || r == '\t' || r == '、'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// checkGapDoc is the gate: every violation of the contract, named.
func checkGapDoc(name, text string) []string {
	doc, problems := parseGapDoc(text)

	// (1) exactly gapDocDimCount rows, exactly D01..D17, once each.
	expected := gapDimSet()
	seen := make(map[string]int, gapDocDimCount)
	for _, r := range doc.rows {
		seen[r.dim]++
		if !expected[r.dim] {
			problems = append(problems, gapParseError(r.line,
				"维度 %s 不在冻结的 %d 维里（D01..%s）",
				r.dim, gapDocDimCount, gapDimID(gapDocDimCount)))
		}
	}
	if len(doc.rows) != gapDocDimCount {
		problems = append(problems, fmt.Sprintf(
			"主表有 %d 行，契约要求恰好 %d 行（一维一行，共 %d 维）",
			len(doc.rows), gapDocDimCount, gapDocDimCount))
	}
	for i := 1; i <= gapDocDimCount; i++ {
		id := gapDimID(i)
		switch seen[id] {
		case 0:
			problems = append(problems, fmt.Sprintf("主表缺少维度 %s（每维必须恰好一行）", id))
		case 1:
		default:
			problems = append(problems, fmt.Sprintf(
				"维度 %s 在主表出现 %d 次（每维必须恰好一行）", id, seen[id]))
		}
	}

	// (2)+(3) each row: legal levels, and the verdict the function computes.
	rowVerdict := make(map[string]string, len(doc.rows))
	for _, r := range doc.rows {
		want, err := gapDocVerdictFor(r.our, r.opp)
		if err != nil {
			// The function names the offending TOKEN; the row names the
			// DIMENSION. A refusal that carries only one of the two makes the
			// author search the document for the other.
			problems = append(problems, gapParseError(r.line, "维度 %s: %s", r.dim, err.Error()))
			continue
		}
		rowVerdict[r.dim] = want
		if r.verdict != want {
			problems = append(problems, gapParseError(r.line,
				"维度 %s 判定列写的是 %q，而 (我方=%s, 对方=%s) 机械判定为 %q",
				r.dim, r.verdict, r.our, r.opp, want))
		}
	}

	// (4) every bucket declared exactly once, empty ones included.
	for _, v := range gapVerdicts {
		if !gapContainsString(doc.bucketOrder, v) {
			problems = append(problems, fmt.Sprintf(
				"桶视图缺少桶 %q（空桶也要写，用 %s 占位；缺一个桶无法与忘记一个桶区分）",
				v, gapBucketEmptyMark))
		}
	}
	for _, v := range doc.bucketOrder {
		if !gapContainsString(gapVerdicts, v) {
			problems = append(problems, fmt.Sprintf("桶视图里有未知桶 %q", v))
		}
	}

	// (5) membership: each dimension in the bucket ITS OWN VERDICT names, once.
	listedIn := make(map[string]string, gapDocDimCount)
	for _, v := range doc.bucketOrder {
		for _, dim := range doc.buckets[v] {
			if prev, dup := listedIn[dim]; dup {
				problems = append(problems, fmt.Sprintf(
					"维度 %s 同时列在桶 %q 与 %q（并集必须每维恰好一次）", dim, prev, v))
				continue
			}
			listedIn[dim] = v
		}
	}
	for _, r := range doc.rows {
		want, ok := rowVerdict[r.dim]
		if !ok {
			continue // its row was already reported above
		}
		got, listed := listedIn[r.dim]
		switch {
		case !listed:
			problems = append(problems, fmt.Sprintf(
				"维度 %s 没有列进任何桶（它自己的判定是 %q）", r.dim, want))
		case got != want:
			// THE 2026-11-06 ARM. Name the dimension, both levels, both buckets:
			// the contradiction is only legible with all four.
			problems = append(problems, fmt.Sprintf(
				"维度 %s 列在桶 %q，而 (我方=%s, 对方=%s) 机械判定为 %q —— 入表条件与它收的行矛盾",
				r.dim, got, r.our, r.opp, want))
		}
	}
	for dim, bucket := range listedIn {
		if !expected[dim] {
			problems = append(problems, fmt.Sprintf(
				"桶 %q 列了维度 %s，它不在冻结的 %d 维里", bucket, dim, gapDocDimCount))
		}
	}
	// The union rule as an EQUALITY with the dimension set: a count would pass
	// the shape both delivered documents had (17 rows, one bucket).
	for dim := range expected {
		if _, ok := listedIn[dim]; !ok {
			problems = append(problems, fmt.Sprintf(
				"桶视图的并集缺少维度 %s（并集必须等于 D01..%s）", dim, gapDimID(gapDocDimCount)))
		}
	}

	if len(problems) != 0 {
		sort.Strings(problems)
		return append([]string{fmt.Sprintf("%s: %d 处契约违例", name, len(problems))}, problems...)
	}
	return nil
}

// containsString reports membership in a small ordered list.
func gapContainsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// gapDocFiles lists the gap documents under root, sorted. A directory that
// cannot be read is an error — an unread directory must never read as "no
// documents, nothing to check".
func gapDocFiles(root string) ([]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(gapDocDirRel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		matched, merr := filepath.Match(gapDocGlob, e.Name())
		if merr != nil {
			return nil, fmt.Errorf("match %s: %w", gapDocGlob, merr)
		}
		if matched {
			out = append(out, filepath.ToSlash(filepath.Join(gapDocDirRel, e.Name())))
		}
	}
	sort.Strings(out)
	return out, nil
}
