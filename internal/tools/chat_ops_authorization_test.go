package tools

// chat_ops_authorization_test.go — the CATEGORY criterion's own proofs (Rule 31:
// a surface is proven by driving it, not by compiling it).
//
// The three paths the operator's ruling defines are each pinned end to end —
// operator work RUNS, a repository write is REFUSED with the task route, a long
// operation ASKS — plus the two things that make the criterion honest rather than
// decorative: the CHANNEL resolution (an unrecognised env spelling must not read
// as the default; an unreadable settings file must land on the safe posture), and
// the OPERATOR-READING (the authorisation for a long operation is read from this
// turn's message, never stored).
//
// WHY THESE THREE PATHS ARE THE TESTS. The predecessor criterion was a stopwatch,
// and the defect it left was a VACUUM: deleting the 30/60 constants alone would
// have sent every undeclared call to ASK, which is STRICTER than what stood. A
// test that only asserted "the constants are gone" would pass on exactly that
// regression, so these tests assert the VERDICTS of the three classes instead —
// an ordinary operation is admitted with NOTHING declared, at the default
// posture.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/runcmd"
	"github.com/nickhuang99/gopi/internal/taskverbs"
)

// --- helpers ------------------------------------------------------------------

// chatOpWithChannel runs body with the posture channel pinned to one value, the
// settings channel pinned to a path that does NOT exist (so the operator's own
// settings file can never decide a test — Rule 32's spirit applied to a config
// knob: a test owns its inputs and never writes the profile it runs under), and
// the environment restored afterwards (t.Setenv, Rule 17).
func chatOpWithChannel(t *testing.T, value string, body func()) {
	t.Helper()
	t.Setenv(ChatOpAutoEnv, value)
	t.Setenv(config.SettingsPathEnv, filepath.Join(t.TempDir(), chatOpTestSettingsName))
	body()
}

// chatOpTestSettingsName is the settings-file name the tests that EXERCISE the
// settings channel write inside their own scratch directory. Named (Rule 1), and
// never the operator's profile.
const chatOpTestSettingsName = "settings.json"

// --- path 1: operator work RUNS ------------------------------------------------

// TestChatOpCategoryOperatorWorkRuns is path 1: the work the operator would have
// done by hand is admitted with NOTHING declared and at the DEFAULT posture. This
// is the regression the stopwatch's removal could have introduced (every
// undeclared call resolving to ASK), so it is asserted as a VERDICT on real
// command lines rather than as a property of a constant.
func TestChatOpCategoryOperatorWorkRuns(t *testing.T) {
	operatorWork := []string{
		// ops / DevOps / network / machine operation
		"ls -la", "git status", "git worktree list", "git log --oneline -5", "df -h", "systemctl status gopi",
		"ip addr show", "ping -c3 192.0.2.6", "ssh peer1 'gopi task list'", "scp a.tar gopi@gopi:/tmp/a.tar",
		// a script, a document, a query, a diagnosis
		"./scripts/gopi-stats.sh", `read -p "?" x`, "grep -rn todo internal/", "ps -p 1234",
		// the ledger, the session, the self-inspection verbs
		"gopi task list", "gopi task show 01a14731", "gopi session log --last", "gopi tool sandbox show",
		"gopi ui_state --target chat",
	}
	chatOpWithChannel(t, "", func() {
		for _, cmd := range operatorWork {
			adm := ChatOpAdmit(cmd, "")
			if !adm.Allowed() {
				t.Errorf("%q was refused (%q) — ordinary operator work must run with nothing declared and at the "+
					"DEFAULT posture, which is the whole point of the category criterion", cmd, adm.Reason())
			}
			if got := adm.Class(); got != ChatOpClassOperatorWork {
				t.Errorf("%q classified as %s, want %s", cmd, got, ChatOpClassOperatorWork)
			}
		}
	})
}

// --- path 2: a repository write is ADMITTED ------------------------------------

// TestChatOpCategoryRepoWriteRefused is path 2, REWRITTEN: writing the repository
// — source editing, the gate run over that edit, a stage/commit — is ADMITTED at
// every posture since the operator's 2026-10-24 ruling, which reversed the old
// hard no-go (a console that cannot commit cannot finish anything; on the night
// of the ruling the dispatched workers were the thing breaking the machine, so
// "route it to a worker" routed the work to the unreliable party).
//
// The test name is kept on purpose — it is the RED-then-GREEN evidence that the
// criterion's own suite moved with the criterion, and a rename would hide that the
// assertion was REVERSED rather than deleted. What it asserts now: the CLASS is
// unchanged (these are still repository writes), the ADMISSION carries no reason,
// and the sentence the refusal-side renders states the commit path's discipline —
// so a model told "the class is yours" is told what practice still binds it.
func TestChatOpCategoryRepoWriteRefused(t *testing.T) {
	// THE TREES ARE READ FROM THE TRACKED PROJECT FILE, not typed here: the rows
	// that name THIS repository's source live in `.gopi/chat-policy.md` since
	// task 01a196bf, and a test that typed them would pass while the file said
	// something else. The three forms below are the ones a console actually
	// writes — an in-place edit, a rewrite, a redirection — applied to a path
	// under a tree the project itself declared.
	// THE TABLE IS THIS REPOSITORY'S OWN, frozen from the tracked file: the
	// shipped fallback names no path (a compiled-in table cannot know a project's
	// layout), so "editing gopi's source is a repository write" is now a
	// statement about `.gopi/chat-policy.md`, and this test drives exactly that.
	freezeChatOpProjectCategories(t)
	root, trees := chatOpProjectTreeRoots(t)
	firstTree := trees[0]
	repoWrites := []string{
		"sed -i 's/a/b/' " + firstTree + "x.go",
		"gofmt -w " + firstTree + "y.go",
		"echo x >> " + firstTree + "z.go",
		"gofmt -w internal/agent/chat_write_guard.go",
		"echo x >> internal/tools/chat.go",
		"git add " + firstTree + "x.go",
		"git commit -m 'fix: x'",
		// `git push` WAS EXCLUDED HERE ONCE (it was the ref-move family's, and Rule 37
		// kept it the operator's decision). The operator's 2026-11-03 ruling
		// ("push 都可以", row 01a1a786) put every push spelling back in
		// REPOSITORY WORK, so it belongs in this list again and is asserted at the
		// classifier in TestEveryPushSpellingIsARepositoryWrite.
		"make test-budget PKGS='" + "./" + firstTree + "'",
		"go test ./" + firstTree,
		"go vet ./" + firstTree,
		"golangci-lint run ./...",
	}
	_ = root
	// Every posture, INCLUDING auto: an opt-in for long operations is not an
	// opt-in for a repository write.
	for _, posture := range []string{"", config.EnvBoolOn} {
		chatOpWithChannel(t, posture, func() {
			for _, cmd := range repoWrites {
				adm := ChatOpAdmit(cmd, "")
				// ADMITTED, at every posture, WITH NO REASON — the operator's
				// 2026-10-24 ruling opened this class and nothing opens a long one.
				if !adm.Allowed() {
					t.Errorf("%q was REFUSED (%q, posture %q) — a repository write is the console's since the "+
						"2026-10-24 ruling", cmd, adm.Reason(), posture)
				}
				if got := adm.Reason(); got != "" {
					t.Errorf("%q was admitted with a reason (%q) — a granted call carries no reason", cmd, got)
				}
				// The CLASS did not move: only the permission did. A future edit
				// that "fixes" the admission by reclassifying these as operator
				// work would leave the category criterion with no member.
				if got := adm.Class(); got != ChatOpClassRepoWrite {
					t.Errorf("%q classified as %s, want %s", cmd, got, ChatOpClassRepoWrite)
				}
				// And nothing refuses it, so the refusal rendering is empty.
				if r := ChatOpRefusal(cmd, ""); r != "" {
					t.Errorf("the guard still renders a refusal for the admitted class %q:\n%s", cmd, r)
				}
			}
		})
	}
	// The one sentence the model is handed for this class states the DISCIPLINE
	// that survives a permission: per-file staging, a clean tree, no ref move.
	admission := ChatOpRepoWriteRefusalSentence()
	for _, want := range []string{"YOURS to perform in this turn", "no permission question", "git add"} {
		if !strings.Contains(admission, want) {
			t.Errorf("the repository-write admission sentence does not carry %q:\n%s", want, admission)
		}
	}
	// The COUNTERPART, and it is what keeps the class from eating the boundary's
	// own mandated destination: a redirection into the session directory is an
	// ARTIFACT, not a repository write.
	chatOpWithChannel(t, "", func() {
		for _, cmd := range []string{
			"echo evidence > ~/.gopi/sessions/01a14731/artifacts/e.md",
			"printf 'x' >> /home/user/.gopi/sessions/01a14731/log.txt",
			"git ls-files internal/",
		} {
			if adm := ChatOpAdmit(cmd, ""); !adm.Allowed() {
				t.Errorf("%q was refused as %s — an artifact outside the repository (and a READ of it) is not a "+
					"repository write", cmd, adm.Class())
			}
		}
	})
}

// --- path 3: a long operation ASKS ---------------------------------------------

// TestChatOpCategoryLongAsks is path 3: an action that IS itself long is the one
// class that must be asked about — refused with nothing declared at the default
// posture, opened by the operator's own words in THIS turn, opened by the standing
// posture, and NEVER opened by a repository write's route.
func TestChatOpCategoryLongAsks(t *testing.T) {
	longs := []string{
		"go build ./...", "go test ./...", "make test", "make build",
		"npm install", "pip install -r req.txt",
		// THE GROUP SPELLINGS ARE THE LIVE ONES (task 01a1958f). The list used
		// to carry `gopi merge` and `gopi serve`; both were measured to name
		// nothing that dispatches (the first is retired and exits 1 naming its
		// survivor, the second prints the top-level help), so the entries that
		// prove the LONG class works now name the commands an operator can
		// actually run — which is the only shape under which this list is
		// evidence. Each is composed from the leaf constants that own its
		// words, never re-typed (Rules 1/13/14).
		runcmd.BinaryName + " " + cliverbs.Worker,
		runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Merge + " --all",
		runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Sync,
		runcmd.BinaryName + " " + cliverbs.Task + " " + taskverbs.Push,
		runcmd.BinaryName + " " + cliverbs.Tool + " " + cliverbs.ScratchGC + " sandboxes",
		"apt-get install jq",
	}
	chatOpWithChannel(t, "", func() {
		for _, cmd := range longs {
			adm := ChatOpAdmit(cmd, "")
			if adm.Allowed() {
				t.Errorf("%q was admitted with nothing declared and the default posture — a long operation is the "+
					"one class the operator must be asked about", cmd)
			}
			if got := adm.Class(); got != ChatOpClassLong {
				t.Errorf("%q classified as %s, want %s", cmd, got, ChatOpClassLong)
			}
			refusal := ChatOpRefusal(cmd, "")
			for _, want := range []string{"CHAT OPS CATEGORY", ChatOpClassLong.String(), ChatOpAutoEnv} {
				if !strings.Contains(refusal, want) {
					t.Errorf("refusal for %q does not carry %q:\n%s", cmd, want, refusal)
				}
			}
			// The operator's OWN words open it — that is the authorisation the
			// class is waiting for.
			if adm := ChatOpAdmit(cmd, "跑吧，最多 5 分钟"); !adm.Allowed() {
				t.Errorf("%q was still refused after the operator authorised 5 minutes — refusing the human's own "+
					"authorisation would make the console argue with them", cmd)
			}
			if adm := ChatOpAdmit(cmd, "最多 30 秒"); !adm.Allowed() {
				t.Errorf("%q was refused although the operator authorised 30 seconds: the class is a question, "+
					"not a stopwatch — ANY declared authorisation in the turn opens it", cmd)
			}
		}
	})
	// The standing posture opens the undeclared long case, and remains the
	// operator's own word rather than a guess.
	chatOpWithChannel(t, config.EnvBoolOn, func() {
		adm := ChatOpAdmit("make test", "")
		if !adm.Allowed() || adm.Channel() != ChatOpChannelAuto {
			t.Errorf("auto posture did not open an unauthorised long operation: %+v", adm)
		}
	})
}

// --- the class predicate -------------------------------------------------------

// TestChatOpCategoryClassPredicateIsExhaustiveAndDisjoint pins the ORDER that gives
// the classification its meaning: a gate run over this repository's own code
// (`go test ./internal/tools/`, `make test-budget`) matches BOTH vocabularies, and
// the ruling makes it a REPOSITORY WRITE (a task), never a long operation to ask
// about.
func TestChatOpCategoryClassPredicateIsExhaustiveAndDisjoint(t *testing.T) {
	for _, cmd := range []string{
		"make test-budget PKGS='./internal/tools'", "go test ./internal/tools/",
		"go build ./cmd/agent/", "go vet ./internal/agent",
	} {
		if got := ChatOpClassify(cmd); got != ChatOpClassRepoWrite {
			t.Errorf("%q classified as %s — a gate run over this repository's own code is the verification half "+
				"of a code change, which the ruling keeps a task (repo-write wins over long)", cmd, got)
		}
	}
	// Every table entry must place its own class (a line the predicate never
	// reaches would be a dead word the prompt still renders). The line is
	// reconstructed from the entry's own PATTERN words — never by pasting it into
	// a sentence — because a category line is a WORD PREFIX now: `x <pattern> y`
	// must classify as the class the line declares.
	for _, w := range ChatOpRepoWriteVocabulary() {
		cmd := chatOpCommandFromPattern(w)
		if got := ChatOpClassify(cmd); got != ChatOpClassRepoWrite {
			t.Errorf("repo-write pattern %q (as %q) classifies as %s", w, cmd, got)
		}
	}
	for _, w := range ChatOpLongOperationVocabulary() {
		cmd := chatOpCommandFromPattern(w)
		// `go test ./...` / `go build ./...` / `make test` are long, and the
		// repo-write prefixes do NOT shadow them: repo-write names `go test *`
		// (specificity 2) and long names `long go test ./... *` (specificity 3), so
		// the longer prefix wins — the same "longest prefix" rule the enforcement
		// table uses.
		if got := ChatOpClassify(cmd); got != ChatOpClassLong {
			t.Errorf("long pattern %q (as %q) classifies as %s", w, cmd, got)
		}
	}
	// The vocabularies handed to the other surface are COPIES: a caller mutating
	// what the prompt renders must not disarm the criterion.
	v := ChatOpLongOperationVocabulary()
	v[0] = "definitely-not-a-long-operation"
	if got := ChatOpClassify(chatOpCommandFromPattern(ChatOpLongOperationVocabulary()[0])); got != ChatOpClassLong {
		t.Error("ChatOpLongOperationVocabulary handed out its own backing array — a caller could mutate the " +
			"criterion's vocabulary")
	}
}

// chatOpCommandFromPattern turns one category line's pattern into a command LINE
// that should classify as the line declares: the pattern's words joined by
// spaces, with the trailing wildcard dropped and a directory word completed with
// one child (so `> internal/` becomes `> internal/x.go`).
func chatOpCommandFromPattern(pattern string) string {
	fields := strings.Fields(pattern)
	out := make([]string, 0, len(fields))
	for i, f := range fields {
		if f == "*" && i == len(fields)-1 {
			if len(out) == 0 {
				return "*"
			}
			out = append(out, "ARG")
			continue
		}
		if strings.HasSuffix(f, "/") {
			f += "x.go"
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// TestChatOpCategoryNonASCIITurnStillAuthorises — the operator writes Chinese; the
// authorisation reader must see it.
func TestChatOpCategoryNonASCIITurnStillAuthorises(t *testing.T) {
	cases := []struct {
		turn string
		want int
	}{
		{"", 0},
		{"最多 5 分钟", 300},
		{"give it 10 minutes", 600},
		{"up to 90 seconds, no more", 90},
		{"这个要跑 1 小时", 3600},
		{"最多 30 秒", 30},
		{"给我 2 min", 120},
		// The MINIMUM reading: a message that offers a minute and caps at 30 s
		// authorises the cap.
		{"可以跑 1 分钟，但不要超过 30 秒", 30},
		// Not a duration: the unit word must follow the number, and an ASCII unit
		// must end as a word.
		{"v0.1.0", 0},
		{"see lines 40-60 of the file", 0},
		{"5 monkeys", 0},
		{"git log -3", 0},
	}
	for _, c := range cases {
		if got := chatOpDeclaredBudgetSeconds(c.turn); got != c.want {
			t.Errorf("chatOpDeclaredBudgetSeconds(%q) = %d, want %d", c.turn, got, c.want)
		}
	}
}

// --- the channels -------------------------------------------------------------

// TestChatOpChannelPrecedenceAndVocabulary pins env > settings > default, and the
// two properties that make a switch of this shape safe: an UNRECOGNISED spelling
// must not be read as the default silently, and the default itself is the SAFE
// posture (all-ask), never auto.
func TestChatOpChannelPrecedenceAndVocabulary(t *testing.T) {
	for _, spelling := range []string{"1", "true", "on", "yes", "TRUE"} {
		chatOpWithChannel(t, spelling, func() {
			if got := ChatOpChannel(); got != ChatOpChannelAuto {
				t.Errorf("%s=%q → %q, want %q", ChatOpAutoEnv, spelling, got, ChatOpChannelAuto)
			}
		})
	}
	for _, spelling := range []string{"0", "off", "false", "no"} {
		chatOpWithChannel(t, spelling, func() {
			if got := ChatOpChannel(); got != ChatOpChannelAllAsk {
				t.Errorf("%s=%q → %q, want %q", ChatOpAutoEnv, spelling, got, ChatOpChannelAllAsk)
			}
		})
	}
	// The unset case: whatever the operator's own settings file says, the answer
	// must be one of the two postures and — because the settings file in this
	// environment does not carry the key — the DEFAULT is all-ask. This is the
	// property that matters: a missing declaration never resolves to "the console
	// may act unattended".
	chatOpWithChannel(t, "", func() {
		if got := ChatOpChannel(); got != ChatOpChannelAllAsk {
			t.Errorf("unset %s and no settings declaration → %q, want the default %q",
				ChatOpAutoEnv, got, ChatOpChannelAllAsk)
		}
	})
}

// TestChatOpChannelIgnoresAnUnrecognisedSpelling — a typo must not answer the
// question (Rule 15). The value is reported on stderr and the SETTINGS channel
// decides; here the settings file carries no key, so the answer is the default.
func TestChatOpChannelIgnoresAnUnrecognisedSpelling(t *testing.T) {
	chatOpWithChannel(t, "ture", func() {
		if got := ChatOpChannel(); got != ChatOpChannelAllAsk {
			t.Errorf("an unrecognised %s spelling resolved to %q — a value that cannot answer the question must "+
				"fall through to the next channel (and to the safe default), never be coerced",
				ChatOpAutoEnv, got)
		}
	})
}

// TestChatOpSettingsChannelIsRead — the settings.json half of the channel, driven
// through a file this test owns (never the operator's profile, Rule 32: the file
// lives in the test's own scratch directory and the path is passed explicitly).
func TestChatOpSettingsChannelIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), chatOpTestSettingsName)
	if err := os.WriteFile(path, []byte(`{"chat_op_auto": true}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	chatOpWithChannel(t, "", func() {
		// Set INSIDE the body: the helper pins the settings channel to a
		// nonexistent path on the way in, which is what makes every OTHER test
		// independent of this machine's settings file.
		t.Setenv(config.SettingsPathEnv, path) //stylegate:ignore the path is computed once per CASE (t.TempDir()), never per call, so t.Cleanup's scope and the per-call scope coincide — nothing flips per call to unwind
		if got := ChatOpChannel(); got != ChatOpChannelAuto {
			t.Fatalf("settings.json chat_op_auto:true resolved to %q, want %q — the settings channel is not read "+
				"(and the operator's declaration would land nowhere)", got, ChatOpChannelAuto)
		}
	})
	// The env channel still wins over the file that says true.
	chatOpWithChannel(t, "off", func() {
		if got := ChatOpChannel(); got != ChatOpChannelAllAsk {
			t.Errorf("%s=off must beat the settings file's true, got %q", ChatOpAutoEnv, got)
		}
	})
}

// TestChatOpUnreadableSettingsFileStillAnswers — "I could not read the file" must
// not become "the key is absent" silently, and it must land on the SAFE posture.
func TestChatOpUnreadableSettingsFileStillAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), chatOpTestSettingsName)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	chatOpWithChannel(t, "", func() {
		t.Setenv(config.SettingsPathEnv, path) //stylegate:ignore same shape in the unreadable-file case: path is that case's own constant, so the test-scoped undo is the correct scope
		if got := ChatOpChannel(); got != ChatOpChannelAllAsk {
			t.Errorf("an unreadable settings file resolved to %q — unknown must land on the safe posture", got)
		}
	})
}

// --- the rendered surfaces ----------------------------------------------------

// TestChatOpRefusalNamesWhatItReadAndTheWayOut — the refusal is the ONLY thing
// the model sees, so it must carry: the class it read, the class that runs, and
// the ONE next move (a question for the long class, the task route for a write).
func TestChatOpRefusalNamesWhatItReadAndTheWayOut(t *testing.T) {
	chatOpWithChannel(t, "", func() {
		long := ChatOpRefusal("make test", "")
		for _, want := range []string{
			"CHAT OPS CATEGORY", ChatOpClassLong.String(), ChatOpClassOperatorWork.String(), ChatOpAutoEnv,
		} {
			if !strings.Contains(long, want) {
				t.Errorf("long refusal does not carry %q:\n%s", want, long)
			}
		}
		// A repository write is ADMITTED since the 2026-10-24 ruling, so there is no
		// refusal to name a way out of. What must still hold is the other direction:
		// the class renders NO refusal (a stale refusal here would tell the model a
		// permission question exists where the guard has none), and the ADMISSION's
		// own sentence is what the prompt renders instead — checked by the same-origin
		// test in internal/agent (chat_ops_category_test.go, PATH 2).
		if write := ChatOpRefusal("git commit -m x", ""); write != "" {
			t.Errorf("a repository write still renders a refusal — the operator's 2026-10-24 ruling admitted it:\n%s",
				write)
		}
		if adm := ChatOpAdmit("git commit -m x", ""); !adm.Allowed() || adm.Class() != ChatOpClassRepoWrite {
			t.Errorf("git commit: allowed=%v class=%s, want an admitted %s", adm.Allowed(), adm.Class(),
				ChatOpClassRepoWrite)
		}
		if r := ChatOpRefusal("ls -la", ""); r != "" {
			t.Errorf("ordinary operator work was refused: %s", r)
		}
	})
}

// TestChatOpCriterionReadsNoStopwatch is the anti-regression for the VACUUM: the
// criterion must not carry a declared-duration band at all. It asserts the two
// removed constants are GONE from the package (a reintroduction would restore the
// stopwatch the operator's ruling replaced) and that the admission no longer takes
// a duration.
func TestChatOpCriterionReadsNoStopwatch(t *testing.T) {
	for _, gone := range []string{"ChatOpQuickBudgetSec", "ChatOpAskThresholdSec", "ChatOpAdmitLike"} {
		if symbolExistsInPackage(t, gone) {
			t.Errorf("%s exists again — the criterion has been re-turned into a stopwatch", gone)
		}
	}
}

// symbolExistsInPackage reports whether this package still declares an identifier,
// by asking the compiler's own view of the package (a reflection-free probe: the
// build of the test binary fails on an undefined identifier, so the check is done
// over the source the package ships).
func symbolExistsInPackage(t *testing.T, name string) bool {
	t.Helper()
	// Read the package's own non-test sources through the runtime's file list.
	matches, err := filepath.Glob("chat_ops_authorization.go")
	if err != nil || len(matches) == 0 {
		t.Fatalf("cannot list the criterion's sources (%v) — this probe would pass vacuously", err)
	}
	for _, f := range matches {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(body), name) {
			return true
		}
	}
	return false
}
