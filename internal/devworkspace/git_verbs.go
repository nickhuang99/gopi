package devworkspace

// The git spellings the delivery channel needs, in ONE home (Rule 13/14: a value
// that is written, compared and reported lives once).
//
// WHY THIS FILE EXISTS. These six constants used to live in `census_gate.go`,
// the file that hosted the census-citation leg. That leg was RETIRED on
// 2026-11-05 (operator ruling: 78bdf05a deleted the documentation-citation
// family — the three generators, the three tables and the gates driving them —
// and this was the last runtime leg still shelling `go run` at a program that no
// longer exists). Deleting the mechanism took them with it and `go build ./...`
// named every survivor: `delivery_anchor.go` and `devworkspace.go` use all of
// them, so under Rule 43 they are GENERAL TOOLS, not mechanism parts — moved out
// in the SAME commit that deletes the rest, so no intermediate state of this
// branch is red.
const (
	// gitMergeBaseVerb / gitIsAncestorFlag ask `git merge-base --is-ancestor
	// <rev> HEAD` — exit 0 when the object is reachable from HEAD, 1 when it is
	// not, and anything else is git being unable to answer (Rule 4.10: the
	// status IS the answer, never a proxy for it).
	gitMergeBaseVerb  = "merge-base"
	gitIsAncestorFlag = "--is-ancestor"

	// gitHeadRef names the tip an attempt publishes: publishGround pushes HEAD,
	// so HEAD is the revision whose reachable set the push uploads.
	gitHeadRef = "HEAD"

	// gateTipVerb is `git rev-parse`, the read that turns HEAD into the sha the
	// destination can be asked about by name.
	gateTipVerb = "rev-parse"

	// gitListRemoteVerb asks the destination what it can show. A bare
	// `ls-remote origin` lists every ref and its tip; a revision that is one of
	// those tips is already resolvable on the server even when no local ref
	// reaches it.
	gitListRemoteVerb = "ls-remote"

	// exitNotAncestor is `git merge-base --is-ancestor`'s status for "not
	// reachable": the answer, told apart from git's own inability to answer
	// (which is any other non-zero status).
	exitNotAncestor = 1
)
