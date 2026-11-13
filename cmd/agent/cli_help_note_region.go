package main

// cli_help_note_region.go — the PUBLISHED NOTE REGION, and the guard's predicate,
// for the class "a `-h` page promises a note it does not carry".
//
// WHY A REGION AND NOT A PHRASE. The class's first instrument said in its own
// doc-comment that it asserted a PROPERTY — "a pointer into the page must land
// on something" — and implemented one phrase
// (`strings.Contains(page, "note below")`). Measured 2026-10-13: the same promise
// re-spelled as "see the self-check note just under this row" stays GREEN, so the
// guard covered one SPELLING of the defect and the class walked around it. The
// live instance was `gopi ui_state -h` line 63 promising "the note below" on a
// 160-line page whose word `note` occurred exactly ONCE — the pointer itself.
//
// The decision, in code: a `-h` page that points FORWARD at a note must carry
// that note inside a MARKED REGION on the published page —
//
//	<!-- BEGIN <slug> NOTE -->  …  <!-- END <slug> NOTE -->
//
// — the same shape man/gopi-merge.md already uses for the merge face's own note
// (`<!-- BEGIN IMAGE SELF-CHECK -->`), and published for the same reason: a mark
// is evidence a reader, a grep or a reviewer can check, not a convention only a
// test knows. A page that carries a region is then decided STRUCTURALLY (a marked
// region exists, is closed, is not empty) and NO wording can dodge it.
//
// WHAT IS STILL WORDING, AND WHY THAT IS NOT A LIE — measured 2026-10-16. The
// scan that catches a pointer on a page with NO region is built from BINDER
// FAMILIES (helpNotePointerBinders), so a locational promise whose binder is in
// none of them is still MISSED: the class is not closed by this file, it is
// narrowed to the families, and devdoc/help-page-note-region.md carries the
// omitted-case reading. An earlier draft of this file claimed the location half
// was "never about the wording" while its vocabulary was a closed seven-word list
// — a promise the body could not keep, and the measured miss ("see the self-check
// note after this row." on a region-free page → 0 offenses, while "just under" on
// the SAME page → 1) is exactly that false negative (Rule 15: the comment above a
// guard states what the guard does). The wider net below is the repair, and the
// honest statement of its limit is the repair's other half.
//
// ENUMERATING SPELLINGS IS NOT THE FIX (the family is), and the broadest honest
// rule — "any page that mentions a note without carrying a region" — was MEASURED
// AND REJECTED: it false-positives on `gopi session -h` and `gopi -h`, which name
// a note as CONTENT and point nowhere (see the vocabulary below). A guard that
// reds a page for printing the word `note` is one authors route around.
//
// THE MARKS ARE THE SAME CONSTANTS THE PAGES RENDER (Rule 14: a shared value has
// exactly one definition). cli_uistate.go's page builds its marks from the two
// prefix/suffix pairs below, so a re-spelt mark cannot leave the writer and the
// recognizer disagreeing; and the guard (cli_help_note_region_test.go) reads the
// vocabulary from here rather than declaring a second copy.

import (
	"fmt"
	"regexp"
	"strings"
)

// The published mark vocabulary. A mark is a full line of the page and stays
// inside the page's own width budget (config.HelpWrapCol) like every other line.
const (
	helpNoteBeginPrefix = "<!-- BEGIN "
	helpNoteBeginSuffix = " NOTE -->"
	helpNoteEndPrefix   = "<!-- END "
	helpNoteEndSuffix   = " NOTE -->"
)

// helpNoteSlugPattern is what a region's NAME may be built of. A region names
// itself so that ONE page may carry several independently addressable notes and
// so a reader can tell which one a pointer meant.
const helpNoteSlugPattern = `[A-Za-z0-9-]+`

// helpNotePointerBinders are the BINDERS a FORWARD pointer is built from — the
// connectives and locators that tie a note to a place: a note promised "below",
// "beneath", "just under", "further down", "past this" — and "after this row",
// "after the flags", "later on this page", "at the end of this page", "on the
// next screen", which are the SAME family a first draft of this vocabulary left
// out entirely. A pointer that NAMES a carrier instead ("see `man gopi-ui_state`")
// is not this class — it sends the reader to a place that can be read, and no
// region on the page was promised (the budget policy's own words: the `-h` page
// ROUTES, the man page carries the contract).
//
// WHAT WIDENING IT COST, MEASURED — do not add a family on a hunch. The list was
// grown only after reporting, per PUBLISHED page, how many note regions it
// carries and every line that mentions the note referent (a throwaway probe over
// publishedVerbHelpPages + taskverbs.MergerVerbs + the top-level page). Two
// readings decided the shape:
//
//	(a) the "after …" family is what the measured miss used ("see the self-check
//	    note after this row." on a region-free page → 0 offenses while "just
//	    under"/"below" on the SAME page → 1), so it is in — a family, not the one
//	    spelling the report happened to quote.
//	(b) the broadest honest rule, "a page that mentions a note without carrying a
//	    region", was MEASURED and REJECTED: it false-positives on two published
//	    pages that name a note as CONTENT and point nowhere — `gopi session -h`
//	    ("gopi session notes <query> … SEARCH the round-level [IMPORTANT] note")
//	    and `gopi -h` ("notes.txt  A positional …"). A guard that reds a page for
//	    printing the word `note` is a guard authors route around, which is how a
//	    guard dies.
//
// Each family is listed for the same reason: leaving `under` out made "under this
// row" GREEN, and leaving the "after …" family out made "after this row" GREEN —
// the false negative this whole guard exists to refuse. A binder is an ordinary
// English word with a second, harmless meaning ("the dumps under ~/.gopi"), and
// that over-match is BOUNDED rather than denied: a binder counts only when a NOTE
// word sits within helpNotePointerBinderProximity of it AND in the same sentence
// (helpNotePointerRE). Two unrelated clauses on one line therefore do not weld
// together, and the arms of the guard's red proof pin both the catches and the
// prose that must stay green. A bounded, explained over-match beats a silent miss
// (Rule 15), and the offense message tells the author exactly how to make the page
// honest (carry the note in a region, or name the carrier).
var helpNotePointerBinders = []string{
	// What lies AT a place on the page.
	"below", "beneath", "underneath", "under",
	// What the reader reaches by going ON — "further down", "past this".
	"following", "further down", "past this",
	// What the reader reaches by going LATER — prose's own list of forward
	// references is longer than the locative-adverb list above, which is exactly
	// how the measured miss stayed open.
	"after", "later", "further on", "down the page",
	// What the reader reaches ON ANOTHER PLACE — the far end of this page, or a
	// screen the pointer names instead of pointing within this one.
	"at the end", "next page", "next screen",
}

// helpNotePointerBinderProximity is how many characters may sit between a note
// word and a binder for the two to read as ONE promise. It is a named limit
// (Rule 1: never a bare literal) because the guard's verdict moves with it: too
// small and a re-spelling slips through, too large and two unrelated clauses on
// one line get welded together. The scan is sentence-bounded as well (see
// helpNotePointerRE), so this bound only ever applies inside one clause.
const helpNotePointerBinderProximity = 60

// helpNotePointerRE is the sentence-bounded pattern of a FORWARD NOTE POINTER,
// built ONCE from the families above (`[^.\n]` — help text is one sentence per
// line by construction, and a match may never reach across a blank line into
// unrelated text). It is ONE pattern rather than a strict/loose pair: a second,
// narrower vocabulary is how the re-spelled arm of the red proof came back GREEN.
//
// It reads a promise in BOTH directions, because English states it in both: the
// note word may come first ("the note below", "the self-check note just under
// this row", "see the self-check note after this row") or the binder may open the
// clause ("further down you will find the note"). Both are held to the same two
// bounds — ONE sentence, and helpNotePointerBinderProximity characters — which is
// what keeps a binder's ordinary meaning out ("the dumps under ~/.gopi", "the
// deprecation note pointing here"): no note word within reach of the binder, no
// promise.
//
// HONEST LIMIT, measured and stated rather than hidden: a promise built on a
// family this list omits ("see the note ahead", "see the note in a moment") is
// still MISSED, and no rule assembled from English binders can cover every
// spelling — devdoc/help-page-note-region.md carries the reading. What is NOT
// negotiable is the direction of the trade (Rule 15): a bounded, explained
// over-match is refused here in words the author can act on, while a silent miss
// is what let this defect live; and the spelling-independent guarantee stays the
// PAGE-level decision above — carry the note in a marked region and no wording
// matters at all.
var helpNotePointerRE = func() *regexp.Regexp {
	binders := strings.Join(helpNotePointerBinders, "|")
	return regexp.MustCompile(fmt.Sprintf(
		`(?i)(?:^|[^A-Za-z0-9])(?:notes?|noting)[^.\n]{0,%[1]d}?\b(?:%[2]s)\b`+
			`|\b(?:%[2]s)\b[^.\n]{0,%[1]d}?(?:notes?|noting)(?:[^A-Za-z0-9]|$)`,
		helpNotePointerBinderProximity, binders))
}()

// helpNoteMarkRE recognizes any well-formed mark, and
// helpNoteSluglessMarkRE the one shape that is NOT a mark: a head or tail whose
// region name is missing, which names nothing and can be addressed by nobody. A
// page carrying one is MALFORMED — a different fact from "dangling", reported in
// its own words so the two cannot be confused.
var (
	helpNoteMarkRE = regexp.MustCompile(
		`<!--\s*(?:BEGIN|END)\s+` + helpNoteSlugPattern + `\s+NOTE\s*-->`)
	helpNoteSluglessMarkRE = regexp.MustCompile(`<!--\s*(?:BEGIN|END)\s*NOTE\s*-->`)
)

// helpNoteRegionRef is one note region found on a page: the marks are byte
// offsets into the page text, so the body can be measured (an EMPTY region is
// refused) without re-parsing.
type helpNoteRegionRef struct {
	slug     string
	bodyFrom int
	bodyTo   int
}

// helpNoteRegions returns every COMPLETE note region of the page, in order. A
// BEGIN with no END before the next BEGIN (or before the end of the page)
// contributes nothing here: the unclosed mark is reported by helpNotePageOffenses,
// and pretending it delimits a region would be the silent pass this guard refuses.
func helpNoteRegions(page string) []helpNoteRegionRef {
	var out []helpNoteRegionRef
	for i := 0; i < len(page); {
		begin := helpNoteMarkRE.FindStringIndex(page[i:])
		if begin == nil {
			break
		}
		head := page[i+begin[0] : i+begin[1]]
		if !strings.Contains(head, "BEGIN") {
			i += begin[1]
			continue
		}
		end := helpNoteMarkRE.FindStringIndex(page[i+begin[1]:])
		if end == nil {
			break
		}
		tail := page[i+begin[1]+end[0] : i+begin[1]+end[1]]
		if !strings.Contains(tail, "END") {
			i += begin[1] + end[1]
			continue
		}
		out = append(out, helpNoteRegionRef{
			slug:     helpNoteSlug(head),
			bodyFrom: i + begin[1],
			bodyTo:   i + begin[1] + end[0],
		})
		i += begin[1] + end[1]
	}
	return out
}

// helpNoteSlug reads a mark's region name out of the mark itself — the text
// between the verb and the trailing NOTE. Two anchors rather than a regexp
// group so the recognizer and the reader cannot drift (Rule 14).
func helpNoteSlug(mark string) string {
	s := strings.TrimSpace(mark)
	s = strings.TrimSuffix(s, "-->")
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " NOTE"); i >= 0 {
		s = s[:i]
	}
	for _, verb := range []string{"BEGIN", "END"} {
		if i := strings.Index(s, verb); i >= 0 {
			s = s[i+len(verb):]
			break
		}
	}
	return strings.TrimSpace(s)
}

// helpNotePageOffenses is THE predicate, a pure function of one page's text — so
// the gate and its red proofs run the SAME code, never a re-implementation of it.
// It returns every way the page fails, each in its own words:
//
//  1. MALFORMED MARK — the page tried to mark a note without naming the region;
//  2. EMPTY REGION — marks delimiting nothing: the defect wearing the fix's
//     clothes, and the same refusal the merge face's own gate makes;
//  3. A LOCATIONAL NOTE POINTER ON A PAGE WITH NO REGION — the class itself.
//
// A page that printed nothing is an offense too: a guard that cannot see the page
// must not pass (Rule 15).
func helpNotePageOffenses(page string) []string {
	var out []string
	if strings.TrimSpace(page) == "" {
		return []string{"printed nothing — a page this guard cannot see is not a passing page"}
	}
	if m := helpNoteSluglessMarkRE.FindString(page); m != "" {
		out = append(out, fmt.Sprintf(
			"carries a note mark with no region name (%q) — a mark names its region (%s) or it is not a mark",
			m, helpNoteBeginPrefix+"slug"+helpNoteBeginSuffix))
	}
	regions := helpNoteRegions(page)
	for _, r := range regions {
		if strings.TrimSpace(page[r.bodyFrom:r.bodyTo]) == "" {
			out = append(out, fmt.Sprintf(
				"carries an EMPTY note region %q — a mark delimiting nothing would make this guard pass on nothing",
				r.slug))
		}
	}
	if len(regions) > 0 {
		return out
	}
	re := helpNotePointerRE
	for _, line := range strings.Split(page, "\n") {
		if !re.MatchString(line) {
			continue
		}
		out = append(out, fmt.Sprintf(
			"points at a note at a LOCATION (%q) but carries no marked note region — the promise has no target.\n"+
				"  Two honest fixes: (a) carry the note on this page inside a region\n"+
				"      %s … %s\n"+
				"  (the marks are published, so a reader can check the region resolves), or\n"+
				"  (b) drop the pointer and name the page that really carries the note\n"+
				"  (e.g. \"see `man gopi-%s`\"). Enumerating more pointer spellings is NOT a fix:\n"+
				"  the page-level decision is the STRUCTURE (a marked region), which no wording dodges —\n"+
				"  the scan that found this line is a bounded net over binder FAMILIES, and a family it\n"+
				"  omits is a spelling it would still miss (devdoc/help-page-note-region.md).",
			strings.TrimSpace(line),
			helpNoteBeginPrefix+"slug"+helpNoteBeginSuffix,
			helpNoteEndPrefix+"slug"+helpNoteEndSuffix,
			CliVerbUIState))
	}
	return out
}
