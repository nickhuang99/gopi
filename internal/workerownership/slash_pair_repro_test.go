package workerownership

// MINIMAL REPRO for the slash-pair false positive (Rule 5: every bug fix
// ships a minimal repro test).
//
// THE DEFECT: MentionsLaunchShape was a bare strings.Contains over Needles, so
// a needle embedded in the corpus's slash-pair ABBREVIATION counted as a launch
// shape. The measured victim is devdoc/tmpdir-param-not-banner.md, whose
// §"EnvVars" row says the group verbs (`gopi task run/worker`, `gopi merger`)
// return from main() — a sentence about two verbs sharing an exit path, with no
// instruction in it. `task run/worker` CONTAINS the needle `task run`, so the
// document read as an offender and two gate tests reddened on every tip that
// carried it (devdoc/verify-01a0a72c-merger-stop-immediate.md carried
// `worker/merger` the same way).
//
// THIS FILE COMPILES AGAINST THE PRE-FIX SOURCE ON PURPOSE — it references only
// values that already existed (Needles, internal/procname), so the RED proof is
// "check out the pre-fix workerownership.go, run this file, watch it fail",
// rather than a build error (a build error proves nothing about the bug). The
// separator is therefore a LOCAL constant here; the file next door
// (workerownership_test.go) pins the production constant and the helper the
// matcher uses, and asserts this local value equals the production one.
//
// The cases separate the two readings with the smallest sample: the pair forms
// (false positives, every one of them measured in a real document) against the
// written-out forms (true positives that must keep matching — the fix must not
// buy green by weakening the gate).

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
)

// slashPairSeparator is the character the corpus's abbreviations join two verbs
// with, spelled locally so this repro compiles pre-fix; the production value is
// workerownership.LaunchShapeSeparator, pinned by the neighbouring test.
const slashPairSeparator = "/"

func TestMentionsLaunchShapeIgnoresSlashJoinedPairs(t *testing.T) {
	var (
		workerShape = procname.NameGopi + " " + procname.VerbWorker                      // "gopi worker"
		runShape    = procname.NameGopi + " " + procname.VerbTask + " " + TaskRunSubverb // "gopi task run"
		sep         = slashPairSeparator
		pair        = func(left, right string) string { return "`" + left + sep + right + "`" }
	)
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			// devdoc/tmpdir-param-not-banner.md:60, verbatim in shape: the
			// second member is `gopi merger`, NOT a written-out worker shape.
			name: "the run/worker pair: two verbs named at once",
			body: "(" + pair(runShape, procname.VerbWorker) + ", `" + procname.NameGopi + " merger" +
				"`) — which return from `main()` at the subcommand chain",
			want: false,
		},
		{
			// devdoc/verify-01a0a72c-merger-stop-immediate.md, near-verbatim.
			name: "the worker/merger pair in a spawn inventory",
			body: "2 function(s) spawn a real " + workerShape + sep + "merger WITHOUT the repo-context sandbox",
			want: false,
		},
		{
			// devdoc/task-dispatcher-p2.md:85.
			name: "the task group's run/serve pair",
			body: "cross-reference each other plus `" + runShape + sep + "serve` so the loop is closed",
			want: false,
		},
		{
			name: "the launcher name as the left member of a pair",
			body: "see " + LauncherName + sep + "README for the operator's launcher",
			want: false,
		},
		{
			name: "a written-out worker shape still triggers",
			body: "the operator's move is `" + workerShape + "`; an agent submits",
			want: true,
		},
		{
			name: "a written-out task run shape still triggers",
			body: "nohup " + runShape + " <task-id> >/dev/null 2>&1 &",
			want: true,
		},
		{
			name: "the launcher script named alone still triggers",
			body: "start it through " + LauncherName + " 3",
			want: true,
		},
		{
			// Every occurrence is examined: an early abbreviated mention must
			// not mask a later written-out one, or a document could keep a
			// launch recipe while hiding behind an abbreviation earlier on.
			name: "an abbreviation before a written-out shape still triggers",
			body: "(" + pair(workerShape, "merger") + ") are services.\nRun `" + workerShape + "` to drain the queue.",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MentionsLaunchShape(tc.body); got != tc.want {
				t.Errorf("MentionsLaunchShape(%q) = %v, want %v\n(the slash-pair idiom is an abbreviation, not a command; a written-out shape is a command)",
					tc.body, got, tc.want)
			}
		})
	}
}
