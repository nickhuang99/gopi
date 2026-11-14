package tools

// shared_integration_ref_guard_verbs_test.go — the VERB-COVERAGE acceptance set
// for the shared `refs/heads/*` namespace guard (the extension: `branch -f`,
// `branch -M`, `checkout -B` / `switch -C`, and the `fetch` refspec
// destination).
//
// WHY this file exists beside the original acceptance set: the original set
// pinned the measured GAP as a deliberate scope (`permit/branch-f-is-out-of-
// scope-measured-gap`) and its own comment said what to do the day the guard was
// widened — "this case must be MOVED to the refused set together with its own
// acceptance rows, not quietly deleted". These are those rows.
//
// THREE properties per verb, and the split is the point (Rule 31):
//
//	RED    — through RAW git (exec.Command, no BashInDir) the verb really does
//	         rewrite the shared ref on this git, and really does leave the main
//	         checkout tracked-dirty. The guard's predicate is NOT on that path,
//	         which is why the damage lands; the same case asserts the predicate
//	         DOES cover the same command TEXT, so RED and GREEN cannot drift.
//	GREEN  — every newly guarded verb REFUSED through BashInDir, and every
//	         no-weakening case ALLOWED through the same entry point.
//	RE-READ— after each refusal the shared ref, the trunk checkout's porcelain
//	         and its staged set are read again: "nothing was written" is a
//	         MEASUREMENT, never an inference from an error string.
//
// ONE FIXTURE PER CASE. Each subtest builds its own repository, because a
// PERMITTED case legitimately moves the caller (`checkout -B <free>` switches
// HEAD onto the new branch, `branch -M <old> <new>` renames it under it), and a
// shared fixture lets one case decide another's world. That cascade is measured,
// not theoretical: the first version of the acceptance set shared one fixture and
// a single refused row (which had moved the caller) turned seven later rows red.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/branchform"
)

// refGuardGitVersionVerb asks the host's git to identify itself; the RED half
// reports the answer with every premise it measures, because whether raw git
// performs the write at all is a property of THIS binary.
const refGuardGitVersionVerb = "--version"

// refGuardVerbWorld is one case's repository: the two-checkout fixture plus a
// single-checkout repository beside it (the ordinary local flow, where every one
// of these verbs is an everyday command).
type refGuardVerbWorld struct {
	refGuardFixture
	single string
	third  string
}

// refGuardBuildVerbWorld builds the fixture for ONE verb case.
func refGuardBuildVerbWorld(t *testing.T) refGuardVerbWorld {
	t.Helper()
	f := refGuardBuildFixture(t)
	single := refGuardBuildSingleCheckout(t, f.base)
	// A repository that holds NEITHER the trunk's branch nor the linked worktree's:
	// the caller shape git's own fetch refusal does not answer (git refuses only the
	// caller's CURRENT branch).
	third := refGuardBuildSingleCheckoutNamed(t, f.base, "third-repo")
	return refGuardVerbWorld{refGuardFixture: f, single: single, third: third}
}

// refGuardVerbCase is one row: the command TEMPLATE (`%s` fields are the world's
// own paths and shas, so every row runs against its own repository), the
// directory it runs in, and the verdict expected of the guard.
type refGuardVerbCase struct {
	name   string
	refuse bool
	// red marks a row whose DAMAGE reproduces through raw git. Only these rows are
	// run without the guard: a permit row is by definition either git-refused on its
	// own (`--move` without `--force`) or aimed at a ref nobody holds, so running it
	// raw would assert a damage that does not exist. The refused-without-a-RED rows
	// still assert RECOGNITION, which is what keeps them from going silently dead.
	red bool
	// renamesOnto marks the form whose damage is a RENAME ONTO the held name (the
	// held ref's sha may be unchanged, so the observable is the caller's own ref
	// disappearing); renamesAway marks the rename-off-the-held-name form, whose
	// observable is the held ref going ABSENT.
	renamesOnto bool
	renamesAway bool
	// want are strings the refusal must carry (Rule 15: the branch, the holder
	// worktree, the consequence, a non-destructive route).
	want []string
	// build renders the row's command in a given world.
	build func(w refGuardVerbWorld) (cmd, dir string)
}

// refGuardVerbCases is the acceptance table shared by the RED, GREEN and RE-READ
// tests, so the three cannot disagree about which command they are talking about.
func refGuardVerbCases() []refGuardVerbCase {
	held := func(w refGuardVerbWorld) string { return refsHeadsPrefix + refGuardFixtureTrunkBranch }
	return []refGuardVerbCase{
		// ── `git branch`'s ref-moving flags, aimed at a held branch ─────────
		{
			name:   "refuse/branch-f-onto-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch, "%OTHER%", "ANOTHER worktree", "Rule 30", "gopi tool sandbox run"},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -f " + refGuardFixtureTrunkBranch + " " + w.older, w.other
			},
		},
		{
			name:   "refuse/branch-force-long-onto-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch --force " + refGuardFixtureTrunkBranch + " " + w.older, w.other
			},
		},
		{
			// The rename DESTINATION is the judged ref: renaming one's OWN branch
			// onto a branch a third checkout holds rewrites that checkout's ref.
			name:        "refuse/branch-M-renaming-onto-a-held-branch",
			refuse:      true,
			red:         true,
			renamesOnto: true,
			want:        []string{"%TRUNK%", refGuardFixtureTrunkBranch, "rev-parse"},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -M " + refGuardFixtureOtherBranch + " " + refGuardFixtureTrunkBranch, w.other
			},
		},
		{
			// Long spellings: `--move --force` is `-M`'s exact long form, and
			// `--force` on its own is `-f`'s. MEASURED: `--move other main` alone is
			// REFUSED by git (`A branch named 'main' already exists.`) because --move
			// without --force will not overwrite — so that spelling is git's answer,
			// not a hole, and it is not a RED row.
			name:        "refuse/branch-move-force-long-renaming-onto-a-held-branch",
			refuse:      true,
			red:         true,
			renamesOnto: true,
			want:        []string{"%TRUNK%", refGuardFixtureTrunkBranch},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch --move --force " + refGuardFixtureOtherBranch + " " + refGuardFixtureTrunkBranch, w.other
			},
		},

		// ── checkout's / switch's force-create ──────────────────────────────
		{
			name:   "refuse/checkout-B-onto-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch, "%OTHER%", "Rule 30"},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " checkout -B " + refGuardFixtureTrunkBranch + " " + w.older, w.other
			},
		},
		{
			name:   "refuse/switch-C-onto-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " switch -C " + refGuardFixtureTrunkBranch + " " + w.older, w.other
			},
		},
		{
			name:   "refuse/switch-force-create-long-onto-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " switch --force-create " + refGuardFixtureTrunkBranch + " " + w.older, w.other
			},
		},

		// ── the fetch refspec destination ──────────────────────────────────
		{
			name:   "refuse/fetch-refspec-into-a-held-branch",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch, "%OTHER%", "Rule 30"},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " fetch -q . +" + refsHeadsPrefix + refGuardFixtureSrcBranch +
					":" + refsHeadsPrefix + refGuardFixtureTrunkBranch, w.other
			},
		},
		{
			name:   "refuse/fetch-refspec-with-a-SHORT-destination",
			refuse: true,
			red:    true,
			want:   []string{"%TRUNK%", refGuardFixtureTrunkBranch},
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " fetch -q . +" + refsHeadsPrefix + refGuardFixtureSrcBranch +
					":" + refGuardFixtureTrunkBranch, w.other
			},
		},

		// ── PERMITTED: the caller's OWN branch (the no-weakening half) ──────
		{
			name: "permit/branch-f-on-the-callers-own-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -f " + refGuardFixtureOtherBranch + " " + w.older, w.other
			},
		},
		{
			name: "permit/checkout-B-on-the-callers-own-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " checkout -B " + refGuardFixtureOtherBranch + " " + w.older, w.other
			},
		},
		{
			name: "permit/switch-C-on-the-callers-own-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " switch -C " + refGuardFixtureOtherBranch + " " + w.older, w.other
			},
		},

		// ── PERMITTED: a branch NO worktree holds ───────────────────────────
		{
			name: "permit/branch-f-on-a-branch-nobody-holds",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -f " + refGuardFixtureNewBranch + " " + w.older, w.other
			},
		},
		{
			name: "permit/checkout-B-on-a-branch-nobody-holds",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " checkout -B " + refGuardFixtureNewBranch + " " + w.older, w.other
			},
		},
		{
			name: "permit/fetch-into-a-branch-nobody-holds",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " fetch -q . +" + refsHeadsPrefix + refGuardFixtureSrcBranch +
					":" + refGuardFixtureNewBranch, w.other
			},
		},
		{
			// The BASE of a `branch -f` is READ, never written (measured): moving a
			// free branch onto a held ref's value must stay allowed.
			name: "permit/branch-f-with-a-held-branch-as-its-BASE",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -f " + refGuardFixtureNewBranch + " " + refGuardFixtureTrunkBranch, w.other
			},
		},
		{
			// A rename AWAY from a name another checkout holds, to a name nobody
			// holds: measured to move no ref and stage nothing (refs/heads/main goes
			// absent, which desyncs no index). Deliberately NOT judged — the
			// measurement and the reason live in the guard's file head.
			name:        "permit/branch-M-renaming-a-held-branch-AWAY",
			renamesAway: true,
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -M " + refGuardFixtureTrunkBranch + " " + refGuardFixtureRenamedBranch, w.other
			},
		},
		{
			// A rename of a branch NOBODY holds is the ordinary local rename.
			name: "permit/branch-M-renaming-a-branch-nobody-holds",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " branch -M " + refGuardFixtureSrcBranch + " " + refGuardFixtureSrcRenamedBranch, w.other
			},
		},

		// ── PERMITTED: gopi's OWN lanes (the false-positive surface) ────────
		{
			// GOPI.md's own branch-creation step. Git itself refuses a name another
			// worktree holds, and the guard does not judge `worktree add -B` at all:
			// a bare `-B` moves a ref only when `worktree add` / `checkout` /
			// `switch` says so.
			name: "permit/worktree-add-B-a-branch-nobody-holds",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.trunk + " worktree add -q " + filepath.Join(w.base, "wt-from-add-b") +
					" " + branchform.FlagCreateForce + " " + refGuardFixtureNewBranch + refGuardWorktreeSuffix + " " + w.older, w.other
			},
		},
		{
			// The merger / finish-cleanup / sync lane: the MAIN checkout writing its
			// OWN integration branch. Refusing this would kill gopi's own merge path.
			name: "permit/main-checkout-force-moves-its-own-integration-ref",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.trunk + " branch -f " + refGuardFixtureTrunkBranch + " " + w.head, w.trunk
			},
		},
		{
			// A single-checkout repository shares nothing: every form above is the
			// ordinary local flow.
			name: "permit/single-checkout-branch-f-on-its-own-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.single + " branch -f " + refGuardFixtureTrunkBranch + " " + w.older, w.single
			},
		},
		{
			name: "permit/single-checkout-fetch-into-its-own-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.single + " fetch -q . +" + refsHeadsPrefix + refGuardFixtureTrunkBranch +
					":" + refsHeadsPrefix + refGuardFixtureTrunkBranch, w.single
			},
		},
		{
			// `symbolic-ref`'s FIRST operand is the ref being SET and the second is
			// the ref it is pointed AT, which is only READ. Judging the second would
			// refuse this: it writes a free ref whose TARGET happens to be a held
			// branch.
			name: "permit/symbolic-ref-pointed-AT-a-held-branch",
			build: func(w refGuardVerbWorld) (string, string) {
				return gitBinaryName + " -C " + w.other + " symbolic-ref " + refGuardFixtureSymrefName + " " + held(w), w.other
			},
		},
		{
			name: "permit/echo-of-a-guarded-verb-is-not-a-write",
			build: func(w refGuardVerbWorld) (string, string) {
				return "echo 'git branch -f " + refGuardFixtureTrunkBranch + " " + w.older + "'", w.other
			},
		},
	}
}

// refGuardRenderCase retargets a row's `%TOKEN%` placeholders onto one world and
// returns the command, its directory and the refusal strings to look for.
func refGuardRenderCase(c refGuardVerbCase, w refGuardVerbWorld) (cmd, dir string, want []string) {
	cmd, dir = c.build(w)
	for _, token := range c.want {
		want = append(want, strings.NewReplacer(
			"%TRUNK%", w.trunk,
			"%OTHER%", w.other,
		).Replace(token))
	}
	return cmd, dir, want
}

// refGuardGitVersion is the host's git, named in every premise report: whether
// raw git performs a write at all is a property of THIS binary (see
// refGuardGitRaisedTheOccupancyWall), so the run's log has to say which one it
// measured instead of assuming the version the table was written on.
func refGuardGitVersion(t *testing.T) string {
	t.Helper()
	out, err := refGuardRawGit("", refGuardGitVersionVerb)
	if err != nil {
		t.Fatalf("cannot read this host's git version (%s %s): %v\n%s — the RED half's premise is undecidable, and an undecidable premise is never a pass (Rule 15)",
			gitBinaryName, refGuardGitVersionVerb, err, out)
	}
	return out
}

// refGuardGitRaisedTheOccupancyWall reports whether GIT ITSELF refused the raw
// command because a worktree holds the branch it would rewrite, and returns the
// one-line reason for the log.
//
// WHY THE PREMISE HAS TO BE MEASURED PER HOST, NOT ASSUMED (measured, 2026-10-12,
// task 01a12e42). This file's verb table was measured on git 2.34.1, where the
// whole family walks through a refs/heads/<b> another worktree holds and the RED
// half therefore observes the damage. On a host whose `git` is NEWER, git answers
// that family itself: on git 2.43.0 (`/usr/local/bin/git`, the binary `git`
// resolves to on host-a) `branch -f`/`--force`/`-M --move --force`,
// `checkout -B`, `switch -C` and a `fetch` whose destination is a held branch all
// exit 128 with "cannot force update the branch '<b>' used by worktree at '<wt>'"
// / "refusing to fetch into branch 'refs/heads/<b>' checked out at '<wt>'" —
// measured verb by verb, generator + raw output in the task's own artifacts
// (refguard-git-version-census.{sh,txt}). So "raw git performs the write" is a
// HOST premise, not a fact about the guard: on such a host the row has no damage
// to reproduce, and asserting one would fail the suite for the host's git being
// safer — the same class of defect as asserting "ppid == 1" on a host whose
// orphan lands on systemd --user (Rule 24).
//
// The instrument is git's OWN answer, never a version number typed here: the
// caller only learns that git refused, and the refusal is recognised by the
// holding worktree's path appearing in git's output. Any other failure stays
// FATAL — a fixture that cannot build is not a passed premise (Rule 15).
func refGuardGitRaisedTheOccupancyWall(out string, w refGuardVerbWorld) (string, bool) {
	if !strings.Contains(out, w.trunk) {
		return "", false
	}
	return fmt.Sprintf("%s refused the write itself, naming the holding worktree %s: %s",
		gitBinaryName, w.trunk, strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]), true
}

// refGuardMessage reports the RED half's premise when it could not be
// established on this host, and the verdict the row DOES still carry.
func refGuardRedPremiseNotEstablished(t *testing.T, cmd, reason string) {
	t.Helper()
	t.Skipf("RED premise not established on this host (%s): %s — raw git performs no write, so this row has no damage to reproduce here (command: %s). Its RECOGNITION is asserted above (the guard still parses it as a write) and its REFUSAL is the GREEN half's verdict; the row stays RED on a git where the write succeeds (the 2.34.1 table this file was measured on).",
		refGuardGitVersion(t), reason, cmd)
}

// TestSharedIntegrationRefGuard_VerbFamilyRedThroughRawGit is the RED half: for
// every newly guarded verb form, the command SUCCEEDS through raw git (exit 0 —
// git does not refuse it) and rewrites the ref the OTHER checkout is standing on.
//
// "git does not refuse it" is MEASURED per row against THIS host's git, never
// assumed from the version the table was written on: where git itself raises the
// occupancy wall the row SKIPS, named and with its reason printed (see
// refGuardGitRaisedTheOccupancyWall), and the GREEN half still asserts the
// refusal. No row is silently dropped — the skip line names the git and the
// holding worktree.
func TestSharedIntegrationRefGuard_VerbFamilyRedThroughRawGit(t *testing.T) {
	for _, c := range refGuardVerbCases() {
		t.Run(c.name, func(t *testing.T) {
			w := refGuardBuildVerbWorld(t)
			cmd, _, _ := refGuardRenderCase(c, w)
			before := refGuardSharedRef(t, w.refGuardFixture)
			if before != w.head {
				t.Fatalf("fixture precondition: trunk ref is %s, want the second commit %s", before, w.head)
			}
			if got := refGuardPorcelain(t, w.refGuardFixture); got != "" {
				t.Fatalf("fixture precondition: a fresh fixture must be clean, got %q", got)
			}

			// The guard RECOGNISES this command as a write to judge — the link
			// between the reproduced damage and the thing being fixed. Asserted on the
			// PARSE (not on the refusal, which is the GREEN half's job) so a widening
			// that silently stopped parsing a verb cannot pass by permitting it.
			if c.refuse {
				if tg, _ := refGuardRecognisedTargets(cmd, w.other); len(tg) == 0 {
					t.Fatalf("the guard does not RECOGNISE %q as a branch-ref write — the RED and GREEN halves have drifted apart", cmd)
				}
			}

			if !c.red {
				// A row whose damage does not reproduce through raw git (a permit row,
				// or one git itself refuses) has nothing to prove here: its verdict is
				// the GREEN half's job. Its RECOGNITION has already been asserted above
				// for the refused ones.
				return
			}

			// ── THE DAMAGE: raw git, no BashInDir, no guard ────────────────────
			args := strings.Fields(cmd)[1:] // drop the leading `git` word
			rawOut, rawErr := refGuardRawGit(w.other, args...)
			if rawErr != nil {
				// THE PREMISE IS MEASURED, NOT ASSUMED: where THIS host's git raises
				// the occupancy wall itself the row has no damage to reproduce.
				if reason, raised := refGuardGitRaisedTheOccupancyWall(rawOut, w); raised {
					refGuardRedPremiseNotEstablished(t, cmd, reason)
				}
				t.Fatalf("the command must SUCCEED when the guard is absent (%s), got: %v\n%s", cmd, rawErr, rawOut)
			}
			after := refGuardGit(t, w.trunk, "rev-parse", "--verify", "--quiet",
				heldRev(w)+"^{commit}")

			if c.renamesOnto {
				// The rename-ONTO form: the caller's branch REPLACES the held name, so
				// the sha may be unchanged (measured: `-M other main` when both point
				// at the same commit) while the branch that the trunk checkout is
				// standing on has been rewritten to be the caller's branch — the
				// OBSERVABLE is that the caller's own ref is gone. Comparing shas would
				// report "no damage" here, which is exactly what a sha-only check misses.
				if callerRef := refGuardRefValue(t, w.trunk, refsHeadsPrefix+refGuardFixtureOtherBranch); callerRef != "" {
					t.Fatalf("RED proof (%s): the caller's branch %s still exists — the rename-onto did not reproduce",
						cmd, refGuardFixtureOtherBranch)
				}
				if after == "" {
					t.Fatalf("RED proof (%s): the held ref went absent, want it to hold the caller's branch", cmd)
				}
				t.Logf("RED (%s): exit=0, %s renamed ONTO %s (ref %s), trunk staged=%d",
					c.name, refGuardFixtureOtherBranch, refGuardFixtureTrunkBranch, refGuardShortSha(after), refGuardStagedCount(t, w.refGuardFixture))
				return
			}
			if c.renamesAway {
				// The rename-AWAY form: the ref goes ABSENT rather than to another
				// sha, and nothing is staged (measured) — the damage is the name
				// disappearing under the other checkout.
				if after != "" {
					t.Fatalf("RED proof (%s): the held ref still resolves to %s, want it renamed AWAY", cmd, after)
				}
				t.Logf("RED (%s): exit=0, ref %s → (absent), trunk staged=%d", c.name, before[:12], refGuardStagedCount(t, w.refGuardFixture))
				return
			}
			if after == before {
				t.Fatalf("RED proof (%s): the held ref is still %s — the shared namespace write did not reproduce", cmd, before)
			}
			if staged := refGuardStagedCount(t, w.refGuardFixture); staged == 0 {
				t.Fatalf("RED proof (%s): the trunk checkout staged nothing — the Rule 30 park did not reproduce (porcelain %q)",
					cmd, refGuardPorcelain(t, w.refGuardFixture))
			}
			t.Logf("RED (%s): exit=0, ref %s → %s, trunk staged=%d porcelain=%q",
				c.name, before[:12], refGuardShortSha(after), refGuardStagedCount(t, w.refGuardFixture), refGuardPorcelain(t, w.refGuardFixture))
		})
	}
}

// heldRev is the ref the RED proof re-reads: the trunk checkout's branch.
func heldRev(w refGuardVerbWorld) string { return refsHeadsPrefix + refGuardFixtureTrunkBranch }

// refGuardShortSha renders a sha (or the empty answer) for a log line.
func refGuardShortSha(sha string) string {
	if sha == "" {
		return "(absent)"
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// TestSharedIntegrationRefGuard_VerbCoverageAcceptanceSet is the GREEN half: every
// newly guarded verb through BashInDir, plus every no-weakening case.
func TestSharedIntegrationRefGuard_VerbCoverageAcceptanceSet(t *testing.T) {
	if escapeDangerous {
		// An auditable skip, not a silent one (Rule 15/27): the guard shares the
		// operator's GOPI_ALLOW_DANGEROUS hatch by design, so its verdict cannot be
		// observed in this process.
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	for _, c := range refGuardVerbCases() {
		t.Run(c.name, func(t *testing.T) {
			w := refGuardBuildVerbWorld(t)
			cmd, dir, want := refGuardRenderCase(c, w)
			before := refGuardSharedRef(t, w.refGuardFixture)

			out, err := BashInDir(cmd, dir)
			if !c.refuse {
				if err != nil {
					t.Fatalf("%q in %s must be PERMITTED, got refusal: %v (output %q)", cmd, dir, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%q in %s must be REFUSED (git ran it), got output: %q", cmd, dir, out)
			}
			refusal := err.Error()
			for _, want := range want {
				if !strings.Contains(refusal, want) {
					t.Errorf("%q: refusal must name %q, got:\n%s", cmd, want, refusal)
				}
			}
			if !strings.Contains(refusal, "dangerous command rejected:") {
				t.Errorf("%q: refusal is not on the dangerous-command channel:\n%s", cmd, refusal)
			}
			// The refusal must name the invocation AS THE CALLER SPELLED IT: the
			// base version rendered `git <verb> <ref>`, which for a `-C <repo>`
			// invocation named neither the argv nor the repo it acted on — and
			// rendered the binary twice once the operand was moved into the text.
			if !strings.Contains(refusal, cmd) {
				t.Errorf("%q: the refusal must quote the invocation as spelled, got:\n%s", cmd, refusal)
			}
			if strings.Contains(refusal, gitBinaryName+" "+gitBinaryName) {
				t.Errorf("%q: the refusal renders the git binary twice:\n%s", cmd, refusal)
			}
			// Nothing was written — the measurement, not the error string.
			if after := refGuardSharedRef(t, w.refGuardFixture); after != before {
				t.Fatalf("%q: the refusal let the shared ref move: %s → %s", cmd, before, after)
			}
			if got := refGuardPorcelain(t, w.refGuardFixture); got != "" {
				t.Fatalf("%q: the refusal wrote into the trunk checkout: porcelain %q", cmd, got)
			}
			if n := refGuardStagedCount(t, w.refGuardFixture); n != 0 {
				t.Fatalf("%q: the refusal left %s staged paths in the trunk checkout", cmd, strconv.Itoa(n))
			}
		})
	}
}

// TestSharedIntegrationRefGuard_ConfigFetchWithoutRefspec closes the SIXTH hole
// this run measured: with `remote.<name>.fetch` mapped to a refs/heads wildcard and
// the CALLER DETACHED (so git has no current branch of its own to defend), a fetch
// with NO refspec at all writes the branch a third checkout holds. Git's own refusal
// does not cover it — its refusal keys on the CALLER's current branch — which is why
// the guard reads the configured mappings too.
func TestSharedIntegrationRefGuard_ConfigFetchWithoutRefspec(t *testing.T) {
	if escapeDangerous {
		t.Skip("GOPI_ALLOW_DANGEROUS is set: the guard shares that hatch by design, so the refusal cannot be observed in this process")
	}
	w := refGuardBuildVerbWorld(t)

	// A bare origin whose `main` is a DIFFERENT commit from the trunk checkout's,
	// so the wildcard fetch has something to move.
	origin := filepath.Join(w.base, "origin.git")
	refGuardGit(t, w.base, "init", "-q", "--bare", origin)
	seed := filepath.Join(w.base, "origin-seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatalf("mkdir origin seed: %v", err)
	}
	refGuardGit(t, seed, "init", "-q", "-b", refGuardFixtureTrunkBranch)
	if err := os.WriteFile(filepath.Join(seed, refGuardFixtureTrunkFile), []byte(refGuardFixtureOriginBody), 0o644); err != nil {
		t.Fatalf("write origin file: %v", err)
	}
	refGuardGit(t, seed, "add", refGuardFixtureTrunkFile)
	refGuardGit(t, seed, "commit", "-q", "-m", "origin-tip")
	refGuardGit(t, seed, "push", "-q", origin, refGuardFixtureTrunkBranch)
	originTip := refGuardGit(t, origin, "rev-parse", refsHeadsPrefix+refGuardFixtureTrunkBranch)

	// The caller: the LINKED worktree, detached (its own branch needs no defending —
	// exactly the state git's refusal does not cover), with a wildcard fetch mapping.
	refGuardGit(t, w.other, "checkout", "-q", "--detach", refGuardFixtureTrunkBranch)
	refGuardGit(t, w.other, "remote", "add", refGuardFixtureOriginRemote, origin)
	refGuardGit(t, w.other, "config", refGuardRemoteFetchKey(refGuardFixtureOriginRemote), "+"+refsHeadsPrefix+refGuardRefspecGlob+":"+refsHeadsPrefix+refGuardRefspecGlob)

	fetchCmd := gitBinaryName + " -C " + w.other + " fetch -q " + refGuardFixtureOriginRemote
	if reason := guardSharedIntegrationRef(fetchCmd, w.other); reason == "" {
		t.Fatalf("the guard does not cover a config-driven wildcard fetch: %q", fetchCmd)
	}

	// RED through raw git: the held ref really moves and the trunk checkout really
	// turns staged — the Rule 30 park, one fetch away, with no refspec spelled.
	//
	// The RED proof is its OWN subtest so that a host where git itself raises the
	// occupancy wall (2.43.0 on host-a: "refusing to fetch into branch
	// 'refs/heads/main' checked out at '<trunk-checkout>'") skips ONLY the damage
	// reproduction below — the GREEN refusal and the two no-weakening probes after
	// it are this arm's verdict and they keep running. Aborting the whole function
	// here would have turned a host premise into a hole in the acceptance set.
	before := refGuardSharedRef(t, w.refGuardFixture)
	t.Run("RED/raw-git-moves-the-held-ref", func(t *testing.T) {
		rawOut, rawErr := refGuardRawGit(w.other, "-C", w.other, "fetch", "-q", refGuardFixtureOriginRemote)
		if rawErr != nil {
			if reason, raised := refGuardGitRaisedTheOccupancyWall(rawOut, w); raised {
				refGuardRedPremiseNotEstablished(t, fetchCmd, reason)
			}
			t.Fatalf("the config-driven fetch must SUCCEED when the guard is absent, got: %v\n%s", rawErr, rawOut)
		}
		if after := refGuardSharedRef(t, w.refGuardFixture); after != originTip {
			t.Fatalf("RED proof: the held ref is %s, want the origin tip %s (was %s)", after, originTip, before)
		}
		staged := refGuardStagedCount(t, w.refGuardFixture)
		if staged == 0 {
			t.Fatalf("RED proof: the trunk checkout staged nothing — the park did not reproduce (porcelain %q)", refGuardPorcelain(t, w.refGuardFixture))
		}
		t.Logf("RED (config fetch, no refspec): %s → %s, trunk staged=%d", before[:12], originTip[:12], staged)
	})

	// Restore, then GREEN through the real entry point, then re-read.
	refGuardGit(t, w.trunk, "reset", "-q", "--hard", w.head)
	if got := refGuardSharedRef(t, w.refGuardFixture); got != w.head {
		t.Fatalf("fixture restore failed: ref is %s, want %s", got, w.head)
	}
	out, err := BashInDir(fetchCmd, w.other)
	if err == nil {
		t.Fatalf("the config-driven fetch must be REFUSED, got output: %q", out)
	}
	if got := refGuardSharedRef(t, w.refGuardFixture); got != w.head {
		t.Fatalf("the refusal let the shared ref move: %s (want %s)", got, w.head)
	}
	if got := refGuardPorcelain(t, w.refGuardFixture); got != "" {
		t.Fatalf("the refusal wrote into the trunk checkout: porcelain %q", got)
	}

	// NO WEAKENING — measured twice, because the first version of this check got the
	// fact wrong and the guard was right.
	//
	// (a) The wildcard mapping still rewrites the TRUNK's branch even from a caller
	// standing on a branch of its own: git's refusal keys on the CALLER's current
	// branch (`other`), so it does not answer for `main`. The guard refusing here is
	// CORRECT, not over-reach: two checkouts hold a ref this fetch would move.
	refGuardGit(t, w.other, "checkout", "-q", refGuardFixtureOtherBranch)
	if reason := guardSharedIntegrationRef(fetchCmd, w.other); reason == "" {
		t.Fatal("a wildcard-mapped fetch must stay REFUSED for the branch a THIRD checkout holds, even from a caller standing on its own branch")
	}

	// (b) git's own DEFAULT mapping is `+refs/heads/*:refs/remotes/<remote>/*`, which
	// writes TRACKING refs and no local branch: that is an ordinary `git fetch`, and
	// the guard must leave it entirely alone.
	refGuardGit(t, w.other, "config", refGuardRemoteFetchKey(refGuardFixtureOriginRemote),
		"+"+refsHeadsPrefix+refGuardRefspecGlob+":"+refGuardRefspecGlob+refGuardRemoteTrackingDestFmt)
	if reason := guardSharedIntegrationRef(fetchCmd, w.other); reason != "" {
		t.Fatalf("the DEFAULT tracking-ref mapping writes no local branch and must be permitted, got: %s", reason)
	}
}
