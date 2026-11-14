package tools

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// The session-container NAME predicate lives in internal/scratch (it is the
// sessions reclaimer's OURS half), but the two SUFFIXES it accepts are DEFINED
// HERE — `taskSuccessorIDFmt` (`%s-r%d`, a reissue successor) and
// `TaskCloneSuccessorIDFmt` (`%s-c%d`, a clean-room clone). internal/tools
// imports internal/scratch and not the reverse, so the two spellings cannot be
// held together by an import; this test is the pin instead.
//
// THE PIN OUTLIVED THE MINT, ON PURPOSE. Since the operator's 2026-11-05 ruling
// neither shape is written any more (an id is an identity; a clone mints a fresh
// uuid through taskCloneMintID). The constants and this test remain because the
// rows written BEFORE the ruling are still on disk under those ids — their
// session containers carry the suffix, and the reclaimer must keep reading them
// as OUR names or they can never be collected at any age (the 47-directory /
// 4.0 GiB freeze this predicate was written for).
//
// WHY IT MATTERS (the measured defect, host host-a 2026-10-30): the
// reclaimer's OURS test was a bare canonical-uuid test, so a container named
// after a SUCCESSOR row (`<uuid>-r1`) or a CLONE row (`<uuid>-c1`) read as a
// foreign name and could never be collected at any age — 47 directories / 4.0 GiB
// frozen, reported as:
//
//  4. 01a17a8a-1037-7ae9-a175-1e7e8162fcea-c1   826.0 MiB   0s   keep
//     not a session container name (a session directory is named by a canonical uuid)
//
// A future third marker (or a rename of either format) must fail HERE rather
// than silently re-freeze the containers it mints.
func TestSessionContainerSuffixMarkersMatchTheMinters(t *testing.T) {
	const srcID = "01a17a8a-1037-7ae9-a175-1e7e8162fcea"
	cases := []struct {
		mintedBy string
		id       string
		marker   string
	}{
		{"taskSuccessorIDFmt", fmt.Sprintf(taskSuccessorIDFmt, srcID, taskSuccessorFirstOrdinal), scratch.SessionContainerReissueMarker},
		{"TaskCloneSuccessorIDFmt", fmt.Sprintf(TaskCloneSuccessorIDFmt, srcID, taskCloneFirstOrdinal), scratch.SessionContainerCloneMarker},
	}
	for _, tc := range cases {
		// The minted id must be a name the reclaimer recognises as a session
		// container; anything else means the suffix this system writes is the
		// suffix its own sweep calls foreign.
		if !scratch.IsMintedSessionContainerName(tc.id) {
			t.Errorf("%s mints %q, which the sessions reclaimer does NOT read as a session container name — the two spellings have drifted", tc.mintedBy, tc.id)
		}
		// …and it must be the SHAPE, not a coincidence about this uuid: the
		// separator + marker are the shared values.
		wantTail := scratch.SessionContainerSuffixSeparator + tc.marker + fmt.Sprint(taskSuccessorFirstOrdinal)
		if !strings.HasSuffix(tc.id, wantTail) {
			t.Errorf("%s mints %q, which does not end in %q — the mint and scratch.SessionContainer*Marker disagree", tc.mintedBy, tc.id, wantTail)
		}
	}
}
