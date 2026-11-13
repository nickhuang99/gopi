// Package sessionid is the ONE definition of "this tree's task-id spelling":
// the shape rule the session-directory seams apply before they join an id into
// a path, so a caller holding a DIFFERENT spelling of the same row is refused
// instead of silently minting a second container.
//
// WHY IT EXISTS (bug row 01a12f3c-d888-720e-b7b5-f458f55e98aa, 2026-10-12).
// A task id has three spellings in this project and only some of them are a
// task id:
//
//	01a12dc6-f44d-7347-9b95-2382a4a7d352   the ROW id — the ledger's key, the
//	                                       session directory's name, what
//	                                       `gopi task show` resolves, and what
//	                                       GOPI_TASK_RUN_ID carries.
//	01a12dc6f44d73479b952382a4a7d352       the BRANCH/WORKTREE spelling —
//	                                       taskGroundIDStem drops every
//	                                       non-alphanumeric character to build
//	                                       the ground directory and the ref.
//	01a12dc6                               a PREFIX, copied out of a rendered
//	                                       listing (internal/fullid exists
//	                                       because 2409 rows share prefixes).
//
// The ledger side has always been safe (a mis-spelled id resolves to NO row —
// `gopi task show 01a12dc6f44d…` exits 1). The FILESYSTEM side was not: both
// session-directory seams (internal/agent.sessionDirForID and
// internal/tools.taskSessionDir) normalized with nothing but
// strings.TrimSpace and then filepath.Join'd the operand, so the wrong spelling
// was not an error — it was a NEW directory. Measured on this host: 11
// dash-stripped containers under ~/.gopi/sessions beside 1300 canonical ones,
// and the case that surfaced it (task 01a12dc6) had a caller reading its own
// evidence out of the one gopi never wrote.
//
// WHAT IT IS NOT. This is not a lookup and it never reaches the ledger: a
// refusal here means "this is not how THIS TREE spells a task id", not "no such
// row" (an unknown-but-canonical id stays a caller's own empty-container
// problem, exactly as before — see the empty/unknown boundary the tools tests
// already pin). It is deliberately a LEAF — stdlib plus internal/fullid (which
// owns "is this id a full uuid", Rule 14, so this package does not spell a
// second uuid regex) — and the session seams on BOTH sides of the agent/tools
// boundary import the SAME rule rather than one mirroring the other.
package sessionid

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nick/gopi/internal/fullid"
)

// TaskIDPrefix is the prefix of every NON-uuid task id this tree mints: the
// pre-uuid legacy rows (`t-<epoch>`), their expanded variants
// (`t-<epoch>-<n>`) and the auxiliary channels (`t-worker-…`, `t-acc-…`) that
// share the ledger without being dispatched work. It is exported because TWO
// packages name the same spelling — the ledger's own renderer
// (internal/tools.taskIDPrefix) and the admissibility rule below — and a value
// shared across packages gets exactly ONE definition, in a leaf both can
// import (PROJECT.md Rules 13/14: a second literal is a defect, and here the
// second spelling would mean one rule refusing an id the other mints).
const TaskIDPrefix = "t-"

// The length IsAdmissible refuses: a uuid's 16 bytes written without grouping
// (the branch/worktree spelling taskGroundIDStem derives). It is a named
// constant because both the refusal and the "did you mean…" hint key on it
// (Rule 1).
//
// There is deliberately NO second arm for a shortened uuid (a single 8-hex
// dash-group): this tree really does use such strings as task-id-shaped tokens
// (`01a0a392` names a gocache residue generation in internal/scratch), and
// over-blocking a legitimate operand is the worse defect — the gate refuses
// only what it can PROVE is a mangled uuid.
const undashedCandidateLen = 32

// overlongCandidateLen is the OTHER mangled-uuid length this rule refuses: a
// uuid's 32 hex characters with ONE MORE hexadecimal digit inserted (measured
// on this host 2026-10-17, task 01a1452c: `…-7e0f9-9ef3-…`, 33 hex characters,
// 37 characters with 5 dashes). It is the same CLASS as the non-canonical
// grouping above — a uuid whose DIGITS are wrong rather than whose dashes are —
// and it is refused for the same measured cost: joining it mints a second
// session directory beside the row's own, and a run then reads its own
// evidence back under the canonical name and reports files that "vanished".
//
// It was the one shape the blacklist did NOT catch, because the first cut keyed
// on ONE length (32) and this operand's character count UNDER the dashes is 33.
// Named as its own constant so the refusal and the length test cannot drift
// (Rule 1).
const overlongCandidateLen = undashedCandidateLen + 1

// IsCanonical reports whether id is a task id in this tree's canonical
// spelling: a full lower-case 8-4-4-4-12 uuid. It is a SHAPE test only — no
// ledger access, no existence claim — and it is internal/fullid's rule, not a
// second regex here.
func IsCanonical(id string) bool { return fullid.IsUUID(id) }

// IsAdmissible reports whether id may name a session directory, and it is
// deliberately NARROW: it refuses only the spellings it can PROVE are not the
// id the caller meant. Three shapes are admitted without argument:
//
//   - the canonical spelling above (every auto-minted row id);
//   - an AUXILIARY CHANNEL / legacy id (t-<something>) — `t-<epoch>`, the
//     expanded `t-<epoch>-<n>`, `t-worker-…`, `t-acc-…`; ~256 rows on this host
//     were written before auto-ids, and the ledger still holds them;
//   - ANYTHING ELSE, and this is the important half. The tree's own tests and
//     scratch vocabulary mint task-id-shaped strings that are not uuids
//     ("01a11548-tracked", "01a09aaa-dead-beef-cafe-000000000001",
//     "0000-emptydir"; internal/scratch's base-residue code calls a dashless hex
//     token "a task id"). A whitelist that refused them would break the alarm
//     faces that resolve a run's session directory in order to REPORT a failure
//     — turning a visible failure into a silent one (Rule 15). Over-blocking
//     here is a worse defect than the one this rule fixes, and it was measured:
//     a first cut that required a canonical uuid or t-… broke nine real tests.
//
// What is refused is exactly the near-miss the defect was made of: a uuid's 16
// bytes in a NON-CANONICAL SPELLING — the 32 lower-case hex characters with no
// grouping at all (taskGroundIDStem's branch/worktree spelling), with SOME
// dashes dropped, or with the groups re-arranged. All three are the same
// mistake: a uuid whose 32 hex characters are right and whose DASHES are wrong,
// which is why joining one mints a second container beside the row's own
// (measured on this host: 22 such containers, each with a canonical twin). The
// comparison is against the CANONICAL spelling, not against the other
// spellings — whatever uuid the dashes are removed from, re-grouping it in the
// canonical 8/13/18/23 positions is the only spelling a row id has.
//
// The SECOND half of the same class is the extra-DIGIT spelling
// (overlongCandidateLen — measured 2026-10-17, task 01a1452c): a uuid's bytes
// with one hexadecimal digit too many, e.g. `01a1452c-928d-7e0f9-9ef3-52c1482cfbc3`
// where the row is `01a1452c-928d-7e0f-9ef3-52c1482cfbc3`. It is the same
// mistake seen from the other side (the digits are wrong rather than the
// dashes), it mints the same second container, and it was the one shape this
// blacklist missed because it keyed on the 32-character count alone. Both are
// refused, and both refusals name the canonical row id.
//
// (A shortened uuid HEAD — one 8-hex dash-group, a rendered listing's prefix —
// is NOT gated: the tree uses such tokens as ids, see above. A prefix resolves
// to its own empty container exactly as an unknown id always did, which is the
// pre-existing behaviour and not a silent second container for a row that
// exists.)
//
// An empty id is NOT this function's business — the callers keep their own
// empty contract (the tools seam still resolves the sessions root for an empty
// id; see TestCovTaskSessionDirEmptyIDBoundary).
func IsAdmissible(id string) bool {
	if id == "" {
		// The empty id is NOT admissible — but it is also not GATED: both seams
		// check for it before calling here, because the tools seam's documented
		// contract for "" is to resolve the sessions ROOT (see
		// TestCovTaskSessionDirEmptyIDBoundary). This function answers the shape
		// question, and the empty string is not a task-id shape.
		return false
	}
	if IsCanonical(id) {
		return true
	}
	return !isNonCanonicalUUIDSpelling(id) && !isOverlongHexRun(id)
}

// isNonCanonicalUUIDSpelling reports whether id is a uuid's 16 bytes written in
// a spelling this tree does not use: 32 lower-case hex characters whose dash
// grouping is not the canonical 8/13/18/23. It is the whole refusal, in one
// place, so the two seams and the hint below cannot disagree about it — and it
// asks the SAME question Refuse answers (CanonicalSpellingOf), so an id that is
// refused always carries a reachable spelling in its own refusal.
func isNonCanonicalUUIDSpelling(id string) bool {
	canonical, ok := CanonicalSpellingOf(id)
	// A spelling that IS canonical is not this arm's business (IsCanonical
	// answered it above, and Refuse never runs for it).
	return ok && canonical != id
}

// allLowerCaseHex reports whether every byte of id is a lower-case hexadecimal
// digit. An empty string is not (and cannot reach here: the lengths above are
// fixed and non-zero).
func allLowerCaseHex(id string) bool {
	for i := 0; i < len(id); i++ {
		switch c := id[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return len(id) > 0
}

// Refuse renders the ONE operator-facing refusal for an inadmissible id. Its
// callers add their own prefix ("task session dir: ", "session log: "), so the
// two seams say the same sentence about the same mistake. It answers for the
// shapes IsAdmissible refuses, and its whole value is the third clause: the
// spelling is named back as the row id it MEANT, because a refusal an operator
// cannot act on is the silence this package exists to end.
func Refuse(id string) error {
	// The two arms are named separately because the REPAIR differs: a wrong
	// grouping is fixed by re-grouping (and the canonical spelling is printed),
	// an extra digit by deleting one digit (and NO spelling is printed — see
	// CanonicalSpellingOf for why that repair is not derivable).
	if canonical, ok := CanonicalSpellingOf(id); ok {
		return fmt.Errorf("task id shape: %q is a uuid's 16 bytes in a non-canonical spelling — its grouping is not the canonical 8/13/18/23 (it is either the branch/worktree spelling, taskGroundIDStem strips every non-alphanumeric character, or a spelling with a dash dropped or moved); joining it mints a SECOND session directory beside the row's own — did you mean %q, the ROW id?", id, canonical)
	}
	if isOverlongHexRun(id) {
		return fmt.Errorf("task id shape: %q is a uuid's 16 bytes with ONE HEXADECIMAL DIGIT TOO MANY (a row id has %d hex characters over its dashes, this operand has %d, so it names no row) — joining it mints a SECOND session directory beside the row's own; check the id against the canonical one you meant (the extra digit is not derivable from the operand, so no repair is guessed here)", id, undashedCandidateLen, overlongCandidateLen)
	}
	return fmt.Errorf("task id shape: %q is not a task-id spelling this tree uses (a canonical uuid, a %s… auxiliary-channel id, and any other id a caller carries are all accepted — this refusal exists only for a uuid's bytes written with the wrong dashes or one digit too many)", id, TaskIDPrefix)
}

// CanonicalSpellingOf answers "is id a uuid's 16 bytes in a non-canonical
// spelling, and if so which uuid" — it is BOTH halves of the rule (the
// predicate above asks whether the answer differs from id) and the hint that
// turns the refusal into a repair, so the two can never disagree.
//
// It is EXPORTED for the same reason the refusal is shared: a caller with its
// own refusal face (internal/tools' ledger lookup) must be able to name the
// repair without spelling a second uuid rule (Rule 14) — the session seams get
// it from Refuse itself, and this is what lets `task show` answer for the SAME
// operand the same way.
//
// The re-grouping is done by the uuid LIBRARY, never by hand (Rule 14: this
// package owns no second uuid spelling rule): the dashes are dropped only to
// hand the library the 32 hex characters, and the canonical spelling is the
// library's own String(). A candidate that is not a uuid's 16 bytes at all
// (the legacy `t-<epoch>-<n>`, `01a11548-tracked`, `0000-emptydir`, a name)
// fails the parse and is therefore never this function's answer — which is what
// keeps the gate from over-blocking the ids the tree really carries.
func CanonicalSpellingOf(id string) (string, bool) {
	run, ok := undashedHexRun(id)
	if !ok || len(run) != undashedCandidateLen {
		// The extra-digit arm has NO repair to name: with one digit too many
		// the dropped character is genuinely unknown (delete the wrong one and
		// the remainder still parses as SOME uuid), so a "did you mean" here
		// would be a guess presented as a repair — which is worse than the bare
		// refusal (Rule 15). That arm is refused by IsAdmissible without a
		// hint; this function answers only the grouping question, where the
		// repair IS determined by the operand itself.
		return "", false
	}
	u, err := uuid.Parse(run)
	if err != nil {
		return "", false
	}
	return u.String(), true
}

// isOverlongHexRun reports whether id is a uuid's 16 bytes written with ONE
// hex digit too many (33 characters under the dashes, every one of them
// lower-case hex). It is the second arm of the refusal and it is asked BY
// SHAPE, not by "does deleting a digit produce a row id" — a scan for the one
// deletion that parses does not discriminate (measured: for the live case
// `…-7e0f9-9ef3-…` it answers `1a1452c9-28d7-e0f9-9ef3-52c1482cfbc3`, a uuid that
// names no row, because deleting most digits still yields 32 parseable hex
// characters). So the class is refused and the caller is told which shape to
// check, never handed a fabricated repair.
func isOverlongHexRun(id string) bool {
	run, ok := undashedHexRun(id)
	return ok && len(run) == overlongCandidateLen
}

// undashedHexRun drops the dashes from id and returns the 32 lower-case hex
// characters underneath, when that is what they are: a uuid's 16 bytes.
//
// The count of characters UNDER the dashes is checked first, and that one
// condition is what keeps the gate from over-blocking the ids this tree really
// carries: every legitimate non-uuid id here is either the wrong length
// (`t-1788146419`, `01a11548`, `01a0a392`, a name) or carries a
// NON-hex character (`01a11548-tracked`, `01a09aaa-dead-beef-cafe-000000000001`,
// `0000-emptydir`), so it never reaches the library's uuid parse and is left
// alone. What remains is a uuid's 16 bytes with the wrong dashes, which is the
// mistake this gate exists for.
func undashedHexRun(id string) (string, bool) {
	length := len(id) - strings.Count(id, "-")
	if length != undashedCandidateLen && length != overlongCandidateLen {
		return "", false
	}
	run := make([]byte, 0, length)
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			continue
		}
		run = append(run, id[i])
	}
	if !allLowerCaseHex(string(run)) {
		return "", false
	}
	return string(run), true
}
