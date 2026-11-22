package scratch

import (
	"strconv"
	"strings"
)

// sessions_container_name.go — the ONE answer to "is this directory NAME a shape
// this system mints for a session container?".
//
// WHY THIS EXISTS, and what it does NOT answer (measured 2026-10-30, host
// the host, the bug row this file answers): the sessions reclaimer used a
// bare canonical-uuid test (IsUUIDName) as its OURS predicate. That is the
// shape of a FIRST-RUN container and of nothing else, so the two suffix shapes
// this system mints for itself — a reopen successor (`<uuid>-r1`, `-r2`) and a
// clean-room clone (`<uuid>-c1`) — read as FOREIGN names:
//
//	4. 01a17a8a-1037-7ae9-a175-1e7e8162fcea-c1   826.0 MiB   0s   keep
//	   not a session container name (a session directory is named by a canonical uuid)
//
// 47 directories / 4.0 GiB sat past the freshness window and could never be
// collected at ANY age, because the name test said "not ours" and the name test
// runs FIRST. The suffix is minted by this very system
// (internal/tools' taskSuccessorIDFmt `%s-r%d` and TaskCloneSuccessorIDFmt
// `%s-c%d`), and the container directory of such a row is named after its row
// id — so the container name carries the same suffix.
//
// THE SEPARATION OF CONCERNS this predicate keeps: it answers ONLY "is this
// shape one the system writes". It is NOT a liveness answer, NOT an age answer
// and NOT a ledger answer — the sessions arm still asks those three in that
// order (judgeSessionDir), and a minted name with a ledger row is still kept
// with SessionsKeepLedgerRow. Recognising the suffix therefore widens NOTHING
// about ownership: it only stops the name test from answering a question it was
// never asked (for a name the system itself writes, "not ours" is simply
// false).
//
// WHAT IS STILL REFUSED — exactly as before, and this is the safety direction
// Rule 15 pins: a name that is not a canonical uuid with at most ONE minted
// ordinal suffix is not ours at any age. `<uuid>-r`, `<uuid>-r01`, `<uuid>-r1x`,
// `<uuid>-r1-r2`, `<uuid>-z1`, `<uuid>_r1`, a bare 32-hex run (the
// branch/worktree stem spelling internal/sessionlayout refuses by name elsewhere)
// and a plain hand-made name all resolve to KEEP with SessionsKeepNotOurs.

const (
	// SessionContainerSuffixSeparator joins a base session id to the ordinal
	// suffix this system mints (`<uuid>` + "-" + "r" + "1").
	SessionContainerSuffixSeparator = "-"

	// SessionContainerReissueMarker is the infix of a REISSUE (reopen
	// successor) container: `<uuid>-r1`. The value is the marker half of
	// internal/tools' taskSuccessorIDFmt (`%s-r%d`); that package imports this
	// one, so the two spellings are held together by a test in internal/tools
	// (TestSessionContainerSuffixMarkersMatchTheMinters), not by an import.
	SessionContainerReissueMarker = "r"

	// SessionContainerCloneMarker is the infix of a CLEAN-ROOM CLONE container:
	// `<uuid>-c1`. The value is the marker half of internal/tools'
	// TaskCloneSuccessorIDFmt (`%s-c%d`), pinned by the same test.
	SessionContainerCloneMarker = "c"

	// sessionContainerFirstOrdinal is the ordinal of the FIRST minted copy
	// (`-r1` / `-c1`). The minters start here, so an ordinal below it — or a
	// bare `-r` with no digits — is not a spelling this system writes.
	sessionContainerFirstOrdinal = 1

	// sessionContainerOrdinalMinDigits is the shortest ordinal tail
	// (`1`), i.e. the marker carries at least one digit.
	sessionContainerOrdinalMinDigits = 1
)

// IsMintedSessionContainerName reports whether name is a session container name
// this system MINTS: a canonical uuid, or a canonical uuid carrying ONE minted
// ordinal suffix (`<uuid>-r<N>` for a reissue, `<uuid>-c<N>` for a clone).
//
// It is the name half of the sessions reclaimer's OURS predicate and nothing
// else; every caller still asks age, liveness and the ledger afterwards.
func IsMintedSessionContainerName(name string) bool {
	if IsUUIDName(name) {
		return true
	}
	base, ok := trimmedMintedOrdinalSuffix(name)
	return ok && IsUUIDName(base)
}

// trimmedMintedOrdinalSuffix strips ONE minted ordinal suffix and returns the
// base name, or reports that the name does not carry one. It accepts exactly
// the spelling the minters produce: <separator><marker><digits>, digits >= 1,
// no leading zero (the minters never write `-r01`), and NOTHING after the
// digits.
func trimmedMintedOrdinalSuffix(name string) (string, bool) {
	cut := strings.LastIndex(name, SessionContainerSuffixSeparator)
	if cut <= 0 {
		return "", false
	}
	base, tail := name[:cut], name[cut+len(SessionContainerSuffixSeparator):]
	if len(tail) < sessionContainerOrdinalMinDigits+len(SessionContainerReissueMarker) {
		return "", false
	}
	switch tail[:len(SessionContainerReissueMarker)] {
	case SessionContainerReissueMarker, SessionContainerCloneMarker:
	default:
		return "", false
	}
	digits := tail[len(SessionContainerReissueMarker):]
	if len(digits) > 1 && digits[0] == '0' {
		return "", false // `-r01` is not the ordinal spelling the mint produces
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", false
		}
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < sessionContainerFirstOrdinal {
		return "", false
	}
	return base, true
}
