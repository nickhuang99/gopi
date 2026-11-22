package memoryprefetch

// extract_runtime_identity.go — the SOURCE-AWARE half of candidate extraction
// (epic 01a20003, leg B).
//
// THE DEFECT, MEASURED. A retrieval key assembled at a TURN BOUNDARY is built
// from the turn's own words (turn.go: TurnInput.Parts), and one of its four
// source classes is a `gopi notify --session … --message …` delivery whose body
// is METADATA-DENSE: a task event's own text is a task id, a session id, a model
// name and a status. The extractor was faithful to that input and therefore put
// the SAME identity's dash fragments into the key — measured on this host over
// 15255 recorded query rows (memory-prefetch.jsonl, 405 sessions), counting runs
// of three or more consecutive bare hex tokens:
//
//	len=5  (a uuid v7 task id, e.g. `01a15bb9 9f77 741e b36a e8883e4e8e14`)  4055 rows
//	len=4 / len=3 (a uuid v4 session id, e.g. `d8943792 42a1 4d32`)           913 rows
//
// WHY THE TWO CLASSES MUST NOT SHARE A FILTER. Retrieving a SESSION uuid answers
// "what did we say in this conversation" — a question the model already holds the
// answer to in its own context, so the hits are noise ABOUT the session. It buys
// nothing and it displaces a real term from the keyword budget. Retrieving a TASK
// id is the opposite: `gopi task submit` writes the task id into the memory
// ledger's `sources` column, so the same fragments return that row's actual work
// recipes — MEASURED (memory #32933): the query `01a1ffd9 5e3c 75be a35d
// fcbff76c55ae` returned #32918/#32921/#32922, that task's own backfill
// diagnosis. A "hex is noise" sweep would delete a working path, which is why the
// discriminator below is the uuid's VERSION and never the token's shape.
//
// THE DISCRIMINATOR IS ONE NIBBLE. gopi mints session/run ids with
// agent.newUUIDv4 and task ids with internal/tools' taskIDGenerate (uuid v7,
// `uuid.NewV7`). A canonical uuid's third group opens with its version, and a
// dash-separated uuid keeps that group intact through splitLatinWords — so the
// third group's first nibble survives the split even when the caller's input
// arrives pre-fragmented. Everything below runtimeIdentityTaskVersion is a run
// identity; a task id is v7 or later (uuid v8 is a valid future widening).
//
// IT IS A FILTER OVER THE SAME PASS, NOT A SECOND EXTRACTOR (Rule 3/35): the
// candidates themselves are still produced by ExtractCandidates' Latin and CJK
// passes, and this file only answers "may this token be a candidate at all".

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// runtimeIdentityTaskVersion is the LOWEST uuid version that names a LEDGER
	// ROW (internal/tools mints v7 — time-ordered). A uuid whose version is below
	// this names a RUN (agent.newUUIDv4 mints v4), and a run is not knowledge.
	// uuid v8 and above are kept for the same reason v7 is: a future task-id
	// minter must not silently start being filtered.
	runtimeIdentityTaskVersion = 7

	// uuidVersionGroupIndex is which dash group carries the version (0-based:
	// time_low, time_mid, time_hi_and_version, …), and uuidVersionNibbleOffset is
	// where inside that group it sits. Both are the canonical layout, spelled
	// once (Rule 1).
	uuidVersionGroupIndex   = 2
	uuidVersionNibbleOffset = 0

	// uuidGroupCount is how many dash groups a canonical uuid has; uuidGroupHexFloor
	// is the shortest a group may be and still be recognised as one (4 = the
	// canonical short group, so a clipped or re-spaced spelling still matches).
	uuidGroupCount    = 5
	uuidGroupHexFloor = 4

	// hexTokenMaxLen bounds what isHexToken accepts: a canonical group is at most
	// 12 hex characters (the last group), and nothing longer is one.
	hexTokenMaxLen = 12

	// uuidGroupSeparator is the character between a canonical uuid's groups. It is
	// named (Rule 1) because taskIdentities both SPLITS on it and, for a
	// dash-less input, re-joins with it.
	uuidGroupSeparator = "-"

	// fragmentedUUIDRunFloor is the fewest consecutive hex-shaped tokens that can
	// still be a uuid whose dashes a caller already consumed. The discriminating
	// group is the THIRD one (it carries the version), so three is the floor — a
	// shorter run says nothing about a version and is left alone.
	fragmentedUUIDRunFloor = uuidVersionGroupIndex + 1
)

// canonicalUUIDPattern matches a canonical 8-4-4-4-12 uuid anywhere in the input.
// It is the PRIMARY recogniser; the token-run scan below is the fallback for input
// that already lost its dashes.
var canonicalUUIDPattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// isHexToken reports whether a token is purely hexadecimal and long enough to be
// a uuid group. It deliberately does NOT require a canonical length: the version
// nibble is what decides, and a caller's input may have clipped or re-spaced it.
func isHexToken(tok string) bool {
	if len(tok) < uuidGroupHexFloor || len(tok) > hexTokenMaxLen {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// uuidVersionOf answers which version a dash group names, or -1 when the group
// cannot carry one. The third group is 4 hex characters and its first one IS the
// version (RFC 4122 §3).
func uuidVersionOf(group string) int {
	group = strings.ToLower(strings.TrimSpace(group))
	if len(group) <= uuidVersionNibbleOffset {
		return -1
	}
	c := group[uuidVersionNibbleOffset]
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}

// runtimeIdentities returns the RUN identities in the input, each as the tokens
// it contributes — which must not become candidates. It answers an EMPTY slice
// for a task id: the one case where a source-aware filter must stand down.
//
// Two shapes are recognised, because the input can arrive either way:
//
//  1. a canonical uuid (8-4-4-4-12) — the shape a notify body carries;
//  2. a run of >= fragmentedUUIDRunFloor consecutive hex-shaped tokens — the
//     shape a caller that already split the value hands over (the record file
//     stores candidate terms space-separated, so re-feeding one is ordinary).
//
// In both cases the VERSION decides (see the file header), and the shape that
// arrives FIRST claims the tokens: a canonical uuid whose groups the token scan
// would also match contributes ONE identity, not two. That dedup is why the
// caller's identity count is a fact rather than an artefact of which recogniser
// fired — measured: the double count reported "2 run identity(ies) (5 token(s))"
// for a single 5-group uuid.
func runtimeIdentities(input string) [][]string {
	var out [][]string
	claimed := make(map[string]bool)

	for _, m := range canonicalUUIDPattern.FindAllString(input, -1) {
		groups := strings.Split(m, "-")
		if len(groups) != uuidGroupCount {
			continue
		}
		if v := uuidVersionOf(groups[uuidVersionGroupIndex]); v >= runtimeIdentityTaskVersion {
			continue // a ledger row's address: its fragments are retrieval gold
		}
		for _, g := range groups {
			claimed[strings.ToLower(g)] = true
		}
		out = append(out, groups)
	}

	// The fallback: maximal runs of hex tokens, judged by the same nibble, and
	// skipped when the canonical recogniser already claimed those tokens.
	toks := splitLatinWords(input)
	for i := 0; i < len(toks); {
		if !isHexToken(toks[i]) {
			i++
			continue
		}
		j := i
		for j < len(toks) && isHexToken(toks[j]) {
			j++
		}
		if j-i >= fragmentedUUIDRunFloor {
			run := toks[i:j]
			if v := uuidVersionOf(run[uuidVersionGroupIndex]); v >= 0 && v < runtimeIdentityTaskVersion {
				if !alreadyClaimed(run, claimed) {
					out = append(out, run)
				}
			}
		}
		i = j
	}
	return out
}

// alreadyClaimed reports whether EVERY token of a run was already claimed by the
// canonical recogniser — the dedup that keeps one uuid from being counted twice.
func alreadyClaimed(run []string, claimed map[string]bool) bool {
	for _, t := range run {
		if !claimed[strings.ToLower(t)] {
			return false
		}
	}
	return true
}

// taskIdentitySet is the TASK-ID half of the source-aware pass: the ids that must
// be carried WHOLE, and the dash fragments they would otherwise contribute.
//
// WHY THIS EXISTS — THE MEASURED DEFECT (task 01a2003d, leg A). extract.go's
// splitLatinWords splits on every non-letter rune, so a canonical task id
// `01a20003-1cfc-7980-9f42-e206ca22b02a` became FIVE candidates
// (01a20003 / 1cfc / 7980 / 9f42 / e206ca22b02a), each spending one of
// MaxKeywords=8 slots on a fragment of ONE logical entity. Measured on the input
// `backfill repair <task-id> prefetch`: 8 terms of which 5 were the same id's
// pieces — and NO DegradationKeywordCapReached, because 5+3 = 8 exactly filled the
// budget rather than overflowing it. The budget was eaten in silence, which is the
// part Rule 15 forbids: a truncation nobody can see is worse than a truncated set
// (the pre-fix output was 7 terms because `e206ca22b02a` dedups with nothing, so
// the cap fired only at the 9th — measured, see the task report).
//
// THE FIX IS NOT "WIDEN THE BUDGET": MaxKeywords stays at its default. One
// logical entity is ONE vote — the whole id enters the budget as a single
// candidate and the pieces it would have split into are suppressed, so the three
// ordinary words beside it survive in the same 8 slots.
//
// BOTH INPUT SHAPES ARE RECOGNISED, and the second one is why this is not just a
// regex:
//
//  1. a canonical uuid (8-4-4-4-12) present in the input — the notify shape;
//  2. a run of exactly uuidGroupCount hex tokens whose group LENGTHS are the
//     canonical 8-4-4-4-12 — the shape a caller that already consumed the dashes
//     hands over (the record file stores candidate terms space-separated, so
//     re-feeding one is ordinary). The lengths are what make re-assembly
//     deterministic rather than a guess, and the version nibble is still the
//     discriminator that decides whether re-assembly is allowed at all.
//
// A RUN WHOSE LENGTHS ARE NOT CANONICAL IS LEFT ALONE and SAID SO: the pieces
// stay separate candidates, because joining them would invent a value the input
// never carried. That record is DegradationTaskIdentityFragmented.
type taskIdentitySet struct {
	// wholes are the task ids to add as ONE candidate each, in input order.
	wholes []string
	// fragments is the lower-cased set of dash pieces that must NOT become
	// candidates of their own: without it the whole id and its pieces would both
	// be retrieved, which is the defect wearing a different hat.
	fragments map[string]bool
	// leaders maps an id's FIRST fragment to the whole id, so the collapse
	// happens AT THE POSITION the id occupies in the input rather than at the
	// head of the term list. Order is part of what this package returns (a query
	// is the joined terms), so the collapse must not reorder the set.
	leaders map[string]string
	// reassembled counts the ids recovered from a dash-less token run (shape 2),
	// and ragged counts the runs whose lengths were not canonical (reported, not
	// joined).
	reassembled int
	ragged      int
}

// canonicalGroupLens is the canonical uuid group layout, in order. It is the
// single source of the re-assembly test (Rule 1): a token run is re-assembled
// only when its lengths ARE this, and nothing re-spells the numbers.
var canonicalGroupLens = [uuidGroupCount]int{8, 4, 4, 4, 12}

// taskIdentities is the ONE reading of the input's TASK ids. It returns an EMPTY
// set for input with none, and it never returns a run identity: the version
// nibble is the discriminator (see the file header), the same one
// runtimeIdentities uses for the opposite conclusion.
func taskIdentities(input string) taskIdentitySet {
	set := taskIdentitySet{fragments: make(map[string]bool), leaders: make(map[string]string)}
	// claimed holds the whole ids the CANONICAL pass already read. The token-run
	// pass below re-reads the same input, and splitLatinWords cuts a canonical uuid
	// into its groups — so without this the SAME id is counted twice ("2 task
	// id(s)" for one id, measured before the fix) and the fragment count doubles.
	// The same dedup runtimeIdentities keeps, for the same reason.
	claimed := make(map[string]bool)

	for _, m := range canonicalUUIDPattern.FindAllString(input, -1) {
		groups := strings.Split(m, "-")
		if len(groups) != uuidGroupCount {
			continue
		}
		if v := uuidVersionOf(groups[uuidVersionGroupIndex]); v < runtimeIdentityTaskVersion {
			continue // a run identity: filteredRuntimeIdentities owns that verdict
		}
		set.wholes = append(set.wholes, strings.ToLower(m))
		claimed[strings.ToLower(m)] = true
		set.leaders[strings.ToLower(groups[0])] = strings.ToLower(m)
		for _, g := range groups {
			set.fragments[strings.ToLower(g)] = true
		}
	}

	// Shape 2: a dash-less run that re-assembles into a canonical layout.
	toks := splitLatinWords(input)
	for i := 0; i < len(toks); {
		if !isHexToken(toks[i]) {
			i++
			continue
		}
		j := i
		for j < len(toks) && isHexToken(toks[j]) {
			j++
		}
		if j-i == uuidGroupCount {
			run := toks[i:j]
			if v := uuidVersionOf(run[uuidVersionGroupIndex]); v >= runtimeIdentityTaskVersion {
				if lensMatch(run) {
					whole := strings.Join(run, uuidGroupSeparator)
					if claimed[whole] {
						// Already read by the canonical pass: the SAME id, not a
						// second one. Counting it twice would make the degradation's
						// own number wrong, which is worse than not reporting at all
						// (Rule 15: a figure a reader acts on must be true).
						i = j
						continue
					}
					set.wholes = append(set.wholes, whole)
					set.leaders[strings.ToLower(run[0])] = whole
					for _, g := range run {
						set.fragments[strings.ToLower(g)] = true
					}
					set.reassembled++
				} else {
					set.ragged++
				}
			}
		}
		i = j
	}
	return set
}

// lensMatch reports whether a token run's group lengths ARE the canonical layout.
func lensMatch(run []string) bool {
	if len(run) != len(canonicalGroupLens) {
		return false
	}
	for i, want := range canonicalGroupLens {
		if len(run[i]) != want {
			return false
		}
	}
	return true
}

// taskIdentityCollapsedDegradation names the collapse (Rule 15: a drop is
// recorded). The detail carries the identity count and the fragment count, and
// says WHY the collapse is not the same thing as a truncation: the id keeps its
// retrieval value as one term, and only the duplication is gone.
func taskIdentityCollapsedDegradation(identities, fragments, reassembled, ragged int) Degradation {
	return Degradation{
		Code: DegradationTaskIdentityCollapsed,
		Detail: fmt.Sprintf(
			"%d task id(s) entered the candidate budget as ONE term each: %d dash fragment(s) were suppressed rather than spent as separate keywords (%d id(s) re-assembled from a dash-less token run, %d run(s) left fragmented because their group lengths were not %v)",
			identities, fragments, reassembled, ragged, canonicalGroupLens),
	}
}

// taskIdentityFragmentedDegradation names the OTHER outcome: a run that CARRIES a
// task-id version but whose group lengths are not the canonical layout. It was left
// fragmented — re-assembling a non-canonical run would spell a uuid the input never
// contained — and the line says so, because a reader must be able to tell "there was
// no id here" from "there was one and I could not read it".
func taskIdentityFragmentedDegradation(ragged int) Degradation {
	return Degradation{
		Code: DegradationTaskIdentityFragmented,
		Detail: fmt.Sprintf(
			"%d hex token run(s) carried a task-id version but their group lengths were not the canonical %v, so they were NOT re-assembled into one term; their pieces remain separate candidates",
			ragged, canonicalGroupLens),
	}
}

// filteredRuntimeIdentities is the ONE reading ExtractCandidates makes: the
// fragments to drop, as a lower-cased set, plus how many DISTINCT run identities
// produced them (a token COUNT would read as "some words were dropped"; the
// identity count is the fact a reader fixing the key needs).
func filteredRuntimeIdentities(input string) (drop map[string]bool, identities int) {
	ids := runtimeIdentities(input)
	if len(ids) == 0 {
		return nil, 0
	}
	drop = make(map[string]bool)
	for _, id := range ids {
		for _, f := range id {
			drop[strings.ToLower(f)] = true
		}
	}
	return drop, len(ids)
}

// runtimeIdentityDegradation names the drop (Rule 15: a loss is recorded, never
// absorbed), naming both numbers and the reason the OTHER class is kept.
func runtimeIdentityDegradation(identities, tokens int) Degradation {
	return Degradation{
		Code: DegradationRuntimeIdentityFiltered,
		Detail: fmt.Sprintf(
			"%d run identity(ies) (%d token(s)) named a session/run uuid (version below %d) and were not extracted as candidates; a task id (version %d+) is kept because the memory ledger's sources column carries task ids",
			identities, tokens, runtimeIdentityTaskVersion, runtimeIdentityTaskVersion),
	}
}
