// The clone channel's TEST FENCE — the boundary `AttemptDelivery`'s
// `git push origin HEAD:<branch>` sits on.
//
// WHY THIS FILE EXISTS (measured 2026-10-26, zpeer's own repository): a
// fixture's throwaway root arrived in the operator's real checkout as
// refs/heads/feat/exists —
//
//	$ git log --format='%H|%an <%ae>|%s' feat/exists
//	3043963598210ca6d6edb4075feb389a77de7c5e|t <t@t>|work
//	89ed62db66a125cde90939468c825d4f649aa789|t <t@t>|init
//	$ git merge-base master feat/exists   → NO MERGE BASE (orphan root)
//	$ git reflog show feat/exists
//	30439635 feat/exists@{0}: push
//
// and the `push` in that reflog is this package's verb. The close-out chain and
// the push-back lane have asked internal/pushfence since 2026-09-22/27; THIS
// boundary could not, because the predicate lived inside internal/tools and
// devworkspace importing that package is a dependency it must not carry.
// Extracting the predicate (Rule 14) is what makes the third boundary askable;
// this file is the ask.
//
// WHAT IT REFUSES, AND WHAT IT DOES NOT: only a TEST process pushing to a
// recipient OUTSIDE its own temp space. A shipped binary is never a test
// (testing.Testing() is false), and a fixture whose origin lives under the
// suite's own temp root is inside test space — the ordinary case, unaffected.
package devworkspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/pushfence"
)

const (
	// originRemoteName is the remote a clone-channel workspace delivers to (git
	// clone names it this way, and nothing in this package renames it).
	originRemoteName = "origin"

	// pushFenceReasonFmt wraps the fence's own sentence with this lane's verb, so
	// the refusal names the STEP that was stopped as well as the target.
	pushFenceReasonFmt = CloneChannelPrefix + "push refused before it ran — %s"
)

// pushFenceRefusal answers "may this push run?" for the clone channel, in the
// same words every other boundary uses (internal/pushfence owns the sentence).
//
// The recipient is read from the workspace's OWN remotes rather than assumed:
// the push command says `origin`, and `origin` resolves to whatever URL the
// clone was made from — that URL is the target the fence has to judge, because
// it is where the bytes land.
//
// An UNREADABLE remote URL is not a fence question: with no URL the push itself
// cannot run, so git reports it a step later with its own words. Silence here is
// therefore a pass so that a real delivery is never withheld for a reading this
// gate could not take.
func pushFenceRefusal(ctx context.Context, dir string) string {
	target := strings.TrimSpace(pushRecipient(ctx, dir))
	if target == "" {
		return ""
	}
	if why := pushfence.Refusal(target); why != "" {
		return fmt.Sprintf(pushFenceReasonFmt, why)
	}
	return ""
}

// pushRecipient resolves the URL `origin` points at in dir, or "" when git
// cannot answer.
func pushRecipient(ctx context.Context, dir string) string {
	out, err := runCommand(ctx, dir, gitBinary, "remote", "get-url", originRemoteName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
