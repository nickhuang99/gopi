package tools

// THE TWO FACES OF THE DELIVERY TARGET, and the property that must hold between
// them: for ONE repository they cannot answer differently.
//
// There is ONE resolution (peerRemoteTargetFor) and two faces onto it — the
// checkout-less face (finishCleanupServerTarget, "the repo this process is
// standing in") and the per-repository face
// (finishCleanupServerTargetForRepo(repo) / finishCleanupRowTargetResolution).
// The difference between the faces is a difference of SUBJECT, and that is the
// only thing that may ever make them disagree: a git remote is a per-repository
// setting, so the same declared alias means different things in two checkouts
// (measured 2026-10-16: the process's own answer and the delivered row's answer
// were different strings, both "correct").
//
// What this file pins:
//
//	① ONE repository, the two faces, ONE answer — exercised on the arm where the
//	   two faces COULD disagree (the repo configures a remote named like the
//	   declared alias: the answer is that remote NAME, not the ssh fallback). The
//	   resolution also carries the directory it answered about (peerRemoteTarget.Dir),
//	   which is what makes "whose answer is this?" readable at all.
//	② NO REPOSITORY — the condition that used to answer with the ssh fallback:
//	   the process is in no git repository, so this repo's git config cannot be
//	   asked anything, and `<alias>:~/work/GoPi` would be a real destination for a
//	   repository nobody established. Both faces answer EMPTY, by name (Rule 15:
//	   "no repository here" and "a repository with no such remote" are two
//	   conditions and they get two answers).
//	③ WHICH FACE EACH PRODUCTION LEG READS — the epic's acceptance question,
//	   asked of the production source instead of from memory: the two legs that
//	   deliver a KNOWN row's work must read the per-repository face, and the
//	   niladic face must not be what answers "which repo?" for either.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

const (
	// dtfPeerAlias is the declared peer of both cases below. It is a valid ssh
	// alias (it must be: an unusable one answers an empty target for a different
	// reason and would make this file vacuous) and it is deliberately a name NO
	// checkout here configures as a remote until a case adds one.
	dtfPeerAlias = "delivery-faces-peer"

	// dtfOtherRemoteName is the fixture client's OWN remote — a name that is NOT
	// the declared alias, which is the ordinary shape of a client (sager:
	// sync_peers ["zbook"], `git remote -v` = origin). It is what makes the
	// assertion "the answer is about THIS repo" observable rather than a tautology.
	dtfOtherRemoteName = "faces-client-remote"
)

// TestDeliveryTargetFacesAgreeForOneRepo is the epic's machine-checkable
// property. Both halves are driven through the real resolvers, on a real
// repository and on a real absence of one.
func TestDeliveryTargetFacesAgreeForOneRepo(t *testing.T) {
	// ── ① ONE REPOSITORY, TWO FACES, ONE ANSWER ──────────────────────────────
	f := newFinishCleanupFixture(t)
	// The remote the resolution must find: named exactly like the declared
	// alias, so the REMOTE ARM is the arm under test (the arm where a face that
	// asked the wrong repository would answer differently).
	pushContractGit(t, f.clientDir, "remote", "add", dtfPeerAlias, f.serverDir)
	t.Setenv(config.SyncPeersEnv, dtfPeerAlias)

	byRow := finishCleanupRowTargetResolution(f.clientDir)
	if byRow.Rendered != dtfPeerAlias {
		t.Fatalf("the row's own repo configures the declared alias as a remote (%s) — the answer must be that remote NAME, got %q (reason: %s)",
			dtfPeerAlias, byRow.Rendered, byRow.Reason)
	}
	if byRow.Dir != f.clientDir {
		t.Fatalf("the resolution must carry the repository it answered about, got Dir=%q want %q", byRow.Dir, f.clientDir)
	}
	byRepo := finishCleanupServerTargetForRepo(f.clientDir)
	if byRepo.Rendered != byRow.Rendered || byRepo.Dir != byRow.Dir {
		t.Fatalf("the two per-repository spellings disagreed about ONE repo: %+v vs %+v", byRow, byRepo)
	}

	// The checkout-less face is the SAME resolution with the process's own
	// checkout as its subject. When that checkout is not a repository at all the
	// subject cannot be established, and case ② below is what that answers — so
	// the cwd facet runs only where the suite is standing inside one.
	//
	// The repository the suite runs in is NEVER the fixture's client (a temp
	// root), so the two subjects are two different repositories: it is exactly
	// the case where a face asking the WRONG repository shows up.
	cwdRoot := finishCleanupServerTargetRepo()
	if cwdRoot == "" {
		t.Logf("running outside a git work tree — the checkout-less facet of ① is covered by ②")
	} else {
		byCwd := finishCleanupServerTargetResolution()
		if byCwd.Dir != cwdRoot {
			t.Fatalf("the checkout-less face must name the checkout it answered about, got Dir=%q want %q", byCwd.Dir, cwdRoot)
		}
		if byCwd.Rendered != finishCleanupServerTargetForRepo(cwdRoot).Rendered {
			t.Fatalf("two resolutions of ONE repo disagreed: %q vs %q",
				byCwd.Rendered, finishCleanupServerTargetForRepo(cwdRoot).Rendered)
		}
		if cwdRoot != f.clientDir && byCwd.Rendered == byRow.Rendered {
			t.Fatalf("two DIFFERENT repositories (%s and %s) resolved the same target %q — the resolution is not asking the repository it was handed",
				cwdRoot, f.clientDir, byCwd.Rendered)
		}
	}

	// ── ② NO REPOSITORY AT ALL ──────────────────────────────────────────────
	// The process is put somewhere that is in no git work tree (a fresh temp
	// directory: verified by the probe the resolution itself runs, so this case
	// cannot pass by accident of where the suite was started).
	t.Chdir(t.TempDir())
	if root := finishCleanupServerTargetRepo(); root != "" {
		t.Fatalf("fixture: this case needs a directory in NO git work tree, but %q resolves a repo root", root)
	}
	// The peer IS declared and usable: the ONLY thing absent is the repository,
	// which is what makes the answer below a statement about the repo and not
	// about the topology.
	if alias := peerRemoteDeclaredAlias(); alias != dtfPeerAlias {
		t.Fatalf("fixture: the declared alias must be usable here, got %q", alias)
	}

	niladic := finishCleanupServerTarget()
	if niladic != "" {
		t.Fatalf("with no repository to ask, the checkout-less face must answer NO target, got %q — the ssh fallback %q is a real destination for a repository nobody established",
			niladic, dtfPeerAlias+peerRemoteTailSep+peerRepoPath())
	}
	res := finishCleanupServerTargetResolution()
	if res.Rendered != "" {
		t.Fatalf("the resolution must carry no target, got %+v", res)
	}
	if !strings.Contains(res.Reason, dtfPeerAlias) {
		t.Errorf("the refusal must name the declared peer it could not resolve, got %q", res.Reason)
	}
	if !strings.Contains(res.Reason, peerRemoteRepoProbeFlag) {
		t.Errorf("the refusal must name the measurement behind it (`git %s %s`), got %q",
			peerRemoteRepoProbeVerb, peerRemoteRepoProbeFlag, res.Reason)
	}
	// The same question about a named directory that is not a repository gets
	// the same answer: the two conditions are told apart by the REPOSITORY, not
	// by which face was asked.
	//stylegate:ignore a path that must not exist: the case IS the absent repository, and one hand-built spelling is clearer than a constant
	absent := finishCleanupServerTargetForRepo("/nonexistent-delivery-faces-repo")
	if absent.Rendered != "" || !strings.Contains(absent.Reason, peerRemoteRepoProbeFlag) {
		t.Fatalf("a directory that is no repository must refuse by name, got %+v", absent)
	}
}

// TestDeliveryTargetFacesRefusalSurvivesTheRealRepo is the CONTROL for ②: the
// same call, in the same process, with the process standing in a REAL repository
// — so the empty answer above is attributable to the absent repository and not to
// a resolver that answers empty for everything.
//
// It is the shape Rule 27 asks for in place of pressure: the observation is
// direct and local (one git probe in an existing checkout), and the contrast is
// between two measured states, never between a state and an assumption.
func TestDeliveryTargetFacesRefusalSurvivesTheRealRepo(t *testing.T) {
	dir := t.TempDir()
	pushContractGit(t, dir, "init", "--quiet", "-b", IntegrationBranchName)
	t.Setenv(config.SyncPeersEnv, dtfPeerAlias)

	res := finishCleanupServerTargetForRepo(dir)
	if res.Rendered == "" {
		t.Fatalf("a real repository with no remote of the declared alias must resolve the ssh fallback, got no target: %+v", res)
	}
	want := dtfPeerAlias + peerRemoteTailSep + peerRepoPath()
	if res.Rendered != want {
		t.Fatalf("the fallback arm must be %q, got %q (reason: %s)", want, res.Rendered, res.Reason)
	}
	if !strings.Contains(res.Reason, fmt.Sprintf("%q", dtfPeerAlias)) {
		t.Errorf("the fallback sentence must name the declared alias, got %q", res.Reason)
	}
}

// --- ③ WHICH FACE EACH PRODUCTION LEG READS (the epic's acceptance) ---
//
// The epic's own acceptance sentence is about PRODUCTION, not about the
// resolvers: no production leg may answer "which repo is this row's delivery
// target asked of?" with the checkout-less face. That is a property of the two
// bodies below, and it is read from their SOURCE — the same instrument
// TestClientBranchPushBackTargetResolverIgnoresTaskSync uses one file over, for
// the same reason: an assertion over a function body cannot be satisfied by a
// fixture that injects a target.

// THE CHAIN LEG WAS RE-ANCHORED, and the reason is recorded HERE rather than in
// a commit message a reader of this file would never open. It used to read
// `taskFinishCleanupChain` in `task_finish_cleanup.go`. That file was DELETED by
// the retirement commit 58b5618 ("refactor(task): retire the finish-cleanup
// mechanism (slice 3/4)"), so the leg's `os.ReadFile` failed before it asserted
// ANYTHING — a red that said nothing about the property it guards.
//
// What was measured at the re-anchor (Rule 43's reference distribution, applied
// to a mechanism that is gone):
//
//	grep -rn "func taskFinishCleanupChain(" internal/tools/
//	  internal/tools/finish_cleanup_moved_helpers_test.go:2807
//
// — the only remaining body is a TEST FIXTURE (that file's own header says it is
// carried verbatim from 58b5618^ for live tests that still name the symbol), so
// no production body is left to assert over. The production replacement is the
// leg that performs the per-row delivery push and resolves its target:
//
//	grep -rn "finishCleanupRowTargetResolution(" --include=*.go internal cmd | grep -v _test.go
//	  internal/tools/task_ground_delivery_reading.go:196
//	  internal/tools/task_delivery_retry.go:269
//
// Of those two, `deliveryRetryOne` is the one that PUSHES — it carries the row's
// target into the delivery leg. The other only RENDERS the `task show` delivery
// line and is read-only. So the chain leg is anchored on `deliveryRetryOne`, and
// the assertion is unchanged in strength: the per-repository face called WITH
// THE ROW's own directory, and the checkout-less face absent from the whole
// body. `mainDir` is that directory (the row's repo, through
// finishCleanupMainRepoDir(row.Repo)); a face asking this PROCESS's checkout
// instead is the same defect this file was written for, one mechanism later.
const (
	// dtfChainBodyDecl / dtfGroundBodyDecl are the two production bodies: the
	// delivery-retry leg's target computation (the close-out chain's production
	// successor — see the block above) and the ground baseline's peer fetch.
	dtfChainBodyDecl  = "func deliveryRetryOne("
	dtfGroundBodyDecl = "func taskGroundPeerBaseline("

	// dtfPerRepoFaceSymbol is the per-repository face the ground leg must read,
	// and dtfNiladicFaceSymbol is the checkout-less one neither leg may use to
	// answer "which repo?".
	dtfPerRepoFaceSymbol = "finishCleanupServerTargetForRepo("
	dtfNiladicFaceSymbol = "finishCleanupServerTarget("

	// dtfRowFaceSymbol is the SECOND spelling of the per-repository question —
	// the same resolution, returned whole plus its reason
	// (finishCleanupRowTargetResolution(repoDir), which for a non-empty dir is
	// finishCleanupServerTargetForRepo(dir) and for an empty one is this
	// process's checkout). The delivery leg reads THIS one because it must also
	// report WHY it resolved what it did; that is the same per-repository
	// subject, so the property under test is the same, and pinning only the
	// narrower spelling would fail a correct production body.
	dtfRowFaceSymbol = "finishCleanupRowTargetResolution("
)

// TestDeliveryTargetLegsReadThePerRepoFace pins ③: each leg resolves its target
// through the per-repository face WITH ITS OWN repository.
func TestDeliveryTargetLegsReadThePerRepoFace(t *testing.T) {
	chainSrc, err := os.ReadFile(dtfChainSrcFile)
	if err != nil {
		t.Fatalf("read %s: %v", dtfChainSrcFile, err)
	}
	groundSrc, err := os.ReadFile(dtfGroundSrcFile)
	if err != nil {
		t.Fatalf("read %s: %v", dtfGroundSrcFile, err)
	}
	cases := []struct {
		leg    string
		body   string
		faces  []string
		wantAr string
	}{
		// The delivery leg hands over THE ROW's branch: the subject is the
		// row's repo, resolved into mainDir by finishCleanupMainRepoDir. It
		// reads the whole-resolution spelling because it must report the
		// reason, not only the rendered target.
		{
			leg:    "delivery-retry push target (the close-out chain's production successor)",
			body:   toolsTestFuncBody(t, string(chainSrc), dtfChainBodyDecl),
			faces:  []string{dtfRowFaceSymbol, dtfPerRepoFaceSymbol},
			wantAr: "mainDir",
		},
		// The ground is being cut for a named repo: the subject is that repo.
		{
			leg:    "ground baseline peer fetch",
			body:   toolsTestFuncBody(t, string(groundSrc), dtfGroundBodyDecl),
			faces:  []string{dtfPerRepoFaceSymbol},
			wantAr: "repo",
		},
	}
	for _, c := range cases {
		// WHETHER the per-repository question was asked AT ALL is asserted
		// before the arity, so a body that asked the right face with the WRONG
		// subject still gets the sentence that names the subject.
		asked := ""
		for _, face := range c.faces {
			if strings.Contains(c.body, face) {
				asked = face
				break
			}
		}
		if asked == "" {
			t.Errorf("%s must resolve its target through a PER-REPOSITORY face (%s or %s) — the checkout-less face answers about the PROCESS's checkout, and a remote is a per-repository setting:\n%s",
				c.leg, dtfRowFaceSymbol, dtfPerRepoFaceSymbol, c.body)
			continue
		}
		if !strings.Contains(c.body, asked+c.wantAr) {
			t.Errorf("%s resolves its target with %s but NOT with its own repository as the argument (%s%s) — a per-repository face asked about the wrong repo answers the right question wrongly:\n%s",
				c.leg, asked, asked, c.wantAr, c.body)
		}
		if strings.Contains(c.body, dtfNiladicFaceSymbol) {
			t.Errorf("%s reads the checkout-less face %s — that face answers about the process's own checkout and cannot answer 'which repo is this row's?':\n%s",
				c.leg, dtfNiladicFaceSymbol, c.body)
		}
	}
}

const (
	// dtfChainSrcFile / dtfGroundSrcFile are the two production files the
	// assertion above spans (Rule 1: a file name a test names is a constant).
	// dtfChainSrcFile is the delivery-retry leg's file — the production
	// successor of the retired chain's, `task_delivery_retry.go`.
	dtfChainSrcFile  = "task_delivery_retry.go"
	dtfGroundSrcFile = "task_ground_baseline.go"
)
