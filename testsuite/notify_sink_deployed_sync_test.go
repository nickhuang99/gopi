package testsuite

// notify_sink_deployed_sync_test.go — the deployed TASK-EVENT SINK must be the
// bytes the repo ships, or say why it is not ours.
//
// THE MEASURED GAP THIS CLOSES (task 01a16942, 2026-10-24, host host-a).
// `~/.gopi/notify/notify.sh` — the file `notify.cmd` execs on every task
// transition — was a copy from 2026-10-13 (23449 bytes) while
// `scripts/notify/notify.sh` had grown to 55815 bytes with nine functions the
// copy did not have (refuse_checkout_home, peer_health_write, deliver_batch,
// deadline_expired, peer_drain_batches, sweep_stale_spools, cursor_age_sec,
// seconds_left, positive_int_or_default). Every one of those behaviours had been
// landed, reviewed and merged in the repository, and NONE of them was running on
// the machine they were written for. Nothing noticed: `deploy.sh` is a one-shot
// an operator runs by hand, and a run that is never re-run is indistinguishable
// from a run that succeeded — the Rule 15 silence this rule's sibling already
// refuses for the websearch plugin tree (TestWebFetchContract_DeployedScriptSync,
// same suite, same shape).
//
// THE SAME FAMILY, one host over: HP's profile declared the identical
// `notify.cmd` and carried NO notify.sh at all, so bash exited 127 before the
// script body and every lifecycle event on that machine was silently
// undelivered. An absent deployed sink is therefore NOT a skip here — it is the
// loudest form of the drift this test measures.
//
// WHY A SUITE TEST AND NOT A NEW VERB, A NEW SCRIPT, OR A WATCHER (Rule 35). The
// failure is a fact about a MOMENT ("the bytes on disk are not the bytes the repo
// ships"), and the mechanism that republishes them already exists and is
// idempotent (`scripts/notify/deploy.sh`). A cron line or a resident poller would
// be a new persistent dependency, would hide a deploy nobody ran by re-running it,
// and would answer a question nobody asked at a time nobody chose. What was
// missing is a place where the answer is already looked at: the suite every
// change passes through, in the SAME form the websearch half of this contract
// already takes — no timer, no state file, no process, nothing to reclaim.
//
// WHAT IT DOES NOT DO. It writes nothing, copies nothing, and deploys nothing:
// the verdict is a sentence naming the drift and the one command that repairs it.
// A checker that repaired its own finding would be a second deploy path.
//
// THE PREDICATE IS DRIVEN, NOT ASSUMED. `notifySinkDrift` is a pure function over
// two byte slices and a settings document, so every branch has a fixture arm
// (current / stale / absent / not-ours) that runs on any host, including one
// whose own deployed copy is perfectly current — the real-corpus arm below then
// applies the same function to the operator's actual profile. A test that only
// read the live file would be green on every machine that happens to be right and
// would measure nothing about the branches that are wrong.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// The deployed sink's location and the repo's copy, each spelled ONCE (Rule 1/13).
// The two directory names are the values the sink script itself declares as
// `readonly SINK_DIR_NAME` / `readonly NOTIFY_SCRIPT_NAME`; they are repeated here
// because a Go test cannot read a shell variable, and the shell side is already
// pinned by cmd/agent/chat_session_slots_script_test.go.
const (
	// notifySinkDirName is the profile subdirectory the sink lives in
	// (notify.sh's SINK_DIR_NAME, and deploy.sh's SINK_SUBDIR).
	notifySinkDirName = "notify"
	// notifySinkScriptName is the file deploy.sh installs (its SCRIPT_NAME).
	notifySinkScriptName = "notify.sh"
	// notifySinkRepoRelPath is the shipped source of truth, relative to this
	// package's directory — ONE level up, because testsuite/ sits directly under
	// the repo root (the same depth convention the sibling deployed-sync check in
	// webfetch_contract_test.go uses for plugins/).
	notifySinkRepoRelPath = "../scripts/notify/notify.sh"
	// notifySinkProfileSettingsName is the profile document that decides whether
	// the canonical sink is this host's target at all.
	notifySinkProfileSettingsName = "settings.json"
	// notifySinkRepairCommand is what a reader does about a verdict; printed with
	// it so the finding is actionable without a search.
	notifySinkRepairCommand = "bash scripts/notify/deploy.sh"
)

// notifySinkVerdict is the decision table's outcome, one value per shape the
// deployed file can take relative to the repo copy. Named (Rule 1) because the
// message and the assertion both switch on it.
type notifySinkVerdict string

const (
	// notifySinkCurrent: the deployed bytes ARE the repo bytes.
	notifySinkCurrent notifySinkVerdict = "current"
	// notifySinkStale: both exist and differ — the drift this test exists for.
	notifySinkStale notifySinkVerdict = "stale"
	// notifySinkAbsent: notify.cmd names the canonical sink and the file is not
	// there (HP's measured shape: bash exits 127 before the body).
	notifySinkAbsent notifySinkVerdict = "absent"
	// notifySinkNotOurs: the host's notify.cmd names something else, so the
	// repo's sample is not its source of truth and there is nothing to compare.
	notifySinkNotOurs notifySinkVerdict = "not-ours"
	// notifySinkUnconfigured: notify.cmd is empty — notifications are off on this
	// host, which is a configuration, not a drift.
	notifySinkUnconfigured notifySinkVerdict = "unconfigured"
)

// notifySinkDrift is the WHOLE predicate, as one pure function: the profile's
// configured notify command, the deployed bytes (nil = the file is not there) and
// the repo's bytes in, a verdict out.
//
// Pure on purpose: the fixture arms drive it directly, so a branch cannot go
// unmeasured on a host that happens to be in a different state.
func notifySinkDrift(notifyCmd, deployedPath string, deployed, repo []byte) notifySinkVerdict {
	if strings.TrimSpace(notifyCmd) == "" {
		return notifySinkUnconfigured
	}
	// Is the canonical sink the file this host's command actually execs? A host
	// that points notify.cmd at its own script owns that script; comparing it
	// against the shipped sample would report a drift nobody can act on.
	if !strings.Contains(notifyCmd, deployedPath) {
		return notifySinkNotOurs
	}
	if deployed == nil {
		return notifySinkAbsent
	}
	if string(deployed) == string(repo) {
		return notifySinkCurrent
	}
	return notifySinkStale
}

// notifySinkProfilePath resolves the OPERATOR's profile root from the OS user
// record — never from GOPI_HOME / GOPI_SETTINGS, which the suite's own
// hermeticity pin points at a scratch path that does not exist.
//
// That distinction is the whole point: this test asks "what is deployed on the
// machine that runs these notifications", so the hermetic sandbox is the wrong
// subject, and reading the pinned settings would make the test skip on every
// run — a green that measured nothing.
func notifySinkProfilePath(t *testing.T, name string) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	return filepath.Join(home, config.ProfileDirName, name)
}

// TestNotifySinkDeployedSync is the REAL-CORPUS arm: apply notifySinkDrift to
// this machine's profile and fail loudly on the two shapes that mean "the repo's
// sink behaviour is not what this machine runs".
func TestNotifySinkDeployedSync(t *testing.T) {
	repoRel, err := filepath.Abs(notifySinkRepoRelPath)
	if err != nil {
		t.Fatalf("resolve the repo sink: %v", err)
	}
	repoBytes, err := os.ReadFile(repoRel)
	if err != nil {
		t.Fatalf("the shipped sink %s is unreadable: %v", repoRel, err)
	}

	settingsPath := notifySinkProfilePath(t, notifySinkProfileSettingsName)
	cfg, err := config.FromSettingsFile(settingsPath)
	if err != nil {
		t.Fatalf("read %s: %v", settingsPath, err)
	}
	notifyCmd := cfg.Notify.Cmd

	profileDir := filepath.Dir(settingsPath)
	deployedPath := filepath.Join(profileDir, notifySinkDirName, notifySinkScriptName)
	deployed, readErr := os.ReadFile(deployedPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read %s: %v", deployedPath, readErr)
	}

	verdict := notifySinkDrift(notifyCmd, deployedPath, deployed, repoBytes)
	// The reading is printed for EVERY verdict, so a skip is a stated skip and
	// not a silence (Rule 15): a reader must be able to see what was compared.
	t.Logf("notify sink drift: verdict=%s deployed=%s repo=%s repo_bytes=%d deployed_bytes=%d",
		verdict, deployedPath, repoRel, len(repoBytes), len(deployed))

	switch verdict {
	case notifySinkUnconfigured:
		t.Skipf("this host configures no notify.cmd (%s) — notifications are off, nothing is deployed to compare", settingsPath)
	case notifySinkNotOurs:
		t.Skipf("this host's notify.cmd names a different sink (%q), so %s is not its source of truth", notifyCmd, repoRel)
	case notifySinkCurrent:
		// The good case, and it says so in the log line above.
	case notifySinkAbsent:
		// HP's measured shape. An absent file is the loudest drift, never a skip:
		// the configured command exits 127 before the script body, so the sink's
		// own self-reporting machinery cannot run at all.
		t.Errorf("the configured sink %s does not exist, but this host's notify.cmd is %q — every lifecycle event is lost before the script body runs.\n repair: %s",
			deployedPath, notifyCmd, notifySinkRepairCommand)
	case notifySinkStale:
		t.Errorf("the deployed sink %s drifted from the repo copy\n repo:     %s (%d bytes)\n deployed: %s (%d bytes)\n repo is the source of truth; the behaviours landed in the repo are NOT running on this machine.\n repair: %s (idempotent — a current copy is left alone, mtime included)",
			deployedPath, repoRel, len(repoBytes), deployedPath, len(deployed), notifySinkRepairCommand)
	}
}

// TestNotifySinkDriftDecisionTable drives every branch of the predicate against
// FIXTURES, so the green real-corpus arm above cannot be a vacuum: this is the
// red proof, and it runs on every host whatever that host's own deployed copy
// happens to be.
//
// Each case is a shape the deployed file can take, and the point of the table is
// that a state which LOOKS like "nothing to check" (an absent file, a command
// naming somebody else's script) gets a DIFFERENT verdict — the two must never
// collapse into one silence.
func TestNotifySinkDriftDecisionTable(t *testing.T) {
	const (
		deployedPath = "/home/operator/.gopi/notify/notify.sh"
		oursCmd      = "bash " + deployedPath
		theirsCmd    = "cat >> /home/operator/.gopi/notify/task-events.jsonl"
	)
	repo := []byte("#!/usr/bin/env bash\n# the shipped sink\n")
	older := []byte("#!/usr/bin/env bash\n# three weeks behind\n")

	cases := []struct {
		name     string
		cmd      string
		deployed []byte
		want     notifySinkVerdict
		why      string
	}{
		{
			name:     "identical bytes are current",
			cmd:      oursCmd,
			deployed: repo,
			want:     notifySinkCurrent,
			why:      "a re-deploy must be a no-op, so this verdict is the one a repair produces",
		},
		{
			name:     "different bytes are stale",
			cmd:      oursCmd,
			deployed: older,
			want:     notifySinkStale,
			why:      "the measured 23449-vs-55815 shape: the repo's landed behaviour is not what runs",
		},
		{
			name:     "a single different byte is stale",
			cmd:      oursCmd,
			deployed: []byte("#!/usr/bin/env bash\n# the shipped sink\n\n"),
			want:     notifySinkStale,
			why:      "one trailing newline is a difference; a byte compare must not tolerate it",
		},
		{
			name:     "an absent file named by our command is ABSENT, never a skip",
			cmd:      oursCmd,
			deployed: nil,
			want:     notifySinkAbsent,
			why:      "HP's measured shape: bash exits 127 and the sink's own reporting cannot run",
		},
		{
			name:     "a command naming another sink is not ours",
			cmd:      theirsCmd,
			deployed: older,
			want:     notifySinkNotOurs,
			why:      "that host owns its script; the shipped sample is not its source of truth",
		},
		{
			name:     "an absent file is still not ours when the command names another sink",
			cmd:      theirsCmd,
			deployed: nil,
			want:     notifySinkNotOurs,
			why:      "the ownership question is answered BEFORE existence, or a host with its own sink would be accused of a missing file it never wanted",
		},
		{
			name:     "no notify command at all is a configuration, not a drift",
			cmd:      "",
			deployed: older,
			want:     notifySinkUnconfigured,
			why:      "notifications off is a decision; reporting it as drift would train a reader to ignore this test",
		},
		{
			name:     "no notify command and no file is still just unconfigured",
			cmd:      "   ",
			deployed: nil,
			want:     notifySinkUnconfigured,
			why:      "whitespace is empty; and an absent file on an unconfigured host is not a finding",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := notifySinkDrift(c.cmd, deployedPath, c.deployed, repo)
			if got != c.want {
				t.Fatalf("verdict = %q, want %q (%s)", got, c.want, c.why)
			}
		})
	}
}

// TestNotifySinkDriftVerdictsAreDistinct pins that the decision table's five
// outcomes are five DIFFERENT strings: a refactor that made "absent" and "stale"
// the same value would leave the switch above unable to report HP's shape, and
// every case would still pass on its own.
func TestNotifySinkDriftVerdictsAreDistinct(t *testing.T) {
	all := []notifySinkVerdict{
		notifySinkCurrent, notifySinkStale, notifySinkAbsent,
		notifySinkNotOurs, notifySinkUnconfigured,
	}
	seen := map[notifySinkVerdict]bool{}
	for _, v := range all {
		if v == "" {
			t.Fatal("an empty verdict would read as a verdict that was never decided")
		}
		if seen[v] {
			t.Fatalf("verdict %q is declared twice — two shapes would be reported as one", v)
		}
		seen[v] = true
	}
}
