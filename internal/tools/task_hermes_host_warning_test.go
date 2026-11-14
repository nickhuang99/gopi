// Host-ownership tests for the hermes-agent submit warning (PROJECT.md Rule 40).
//
// The defect the warning exists for, measured 2026-10-03: a hermes-agent task
// submitted on zpeer's ledger, repo verification PASSED (the path exists on
// both hosts), and the row sat there unclaimed until a human migrated it to HP
// (01a0fe63 → 01a0fe67).
//
// Cases:
//
//	(a) a hermes-agent repo on a NON-owner host → the warning fires
//	(b) the same repo on the OWNER host (host-b, any case) → silent
//	(c) a non-hermes repo on a non-owner host → silent
//	(d) an empty/unknown repo → silent
//	(e) the WRITE face: TaskSubmit with such a repo still queues the row (it is
//	    a warning, never a refusal) and prints the warning beside the confirmation
//	(f) a grep gate keeping the host literal and the template at their single
//	    definition site (Rule 13)
//
// Every assertion is hostname-INDEPENDENT: the decision is the pure predicate
// (host passed in), and the write-face case derives its expectation from the
// same predicate on this host — so the test cannot be red just because it is
// running on HP.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// hermesHostWarningDefinitionFile is the single definition site of the
// warning's literals (Rule 13); the grep gate below scans the rest of the
// package for them.
const hermesHostWarningDefinitionFile = "task_hermes_host_warning.go"

func TestHermesHostWarningFiresOnANonOwnerHost(t *testing.T) {
	cases := []struct {
		name string
		repo string
		host string
	}{
		{"absolute work tree path", "/home/user/work/hermes-agent", "zpeer"},
		{"a worktree of it", "/home/user/workspace/gopi-01a0fe67/hermes-agent", "some-other-host"},
		{"plugin subtree", "/home/user/work/hermes-agent/plugins/x", "zpeer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := taskHermesHostWarningFor(c.repo, c.host)
			if got == "" {
				t.Fatalf("repo %q on host %q: no warning — the misroute is silent again", c.repo, c.host)
			}
			if !strings.Contains(got, c.repo) {
				t.Errorf("warning does not name the repo %q:\n%s", c.repo, got)
			}
			if !strings.Contains(got, taskHermesOwnerHost()) {
				t.Errorf("warning does not name the owning host %q:\n%s", taskHermesOwnerHost(), got)
			}
			if !strings.Contains(got, c.host) {
				t.Errorf("warning does not name the submitting host %q:\n%s", c.host, got)
			}
			if !strings.Contains(got, "never a refusal") && !strings.Contains(got, "Rule 40") {
				t.Errorf("warning does not point at the rule:\n%s", got)
			}
		})
	}
}

func TestHermesHostWarningSilentOnTheOwnerHost(t *testing.T) {
	// The owner's name in any case: hostname spelling must not decide whether
	// the owner sees a false alarm.
	for _, host := range []string{taskHermesOwnerHost(), strings.ToLower(taskHermesOwnerHost()), strings.ToUpper(taskHermesOwnerHost())} {
		if got := taskHermesHostWarningFor("/home/user/work/hermes-agent", host); got != "" {
			t.Errorf("host %q owns hermes-agent but got a warning:\n%s", host, got)
		}
	}
}

func TestHermesHostWarningSilentForOtherRepos(t *testing.T) {
	for _, repo := range []string{"", "   ", "/home/user/work/GoPi", "/home/user/work/GoPi-hermes", "/srv/agent"} {
		if got := taskHermesHostWarningFor(repo, "zpeer"); got != "" {
			t.Errorf("repo %q is not the hermes-agent tree but got a warning:\n%s", repo, got)
		}
	}
}

// TestTaskSubmitWarnsButStillQueuesOnAHermesRepo is the WRITE face: the
// warning rides the confirmation the CLI and the tool both print, and the row
// is still created — the two halves of "warn, never refuse".
func TestTaskSubmitWarnsButStillQueuesOnAHermesRepo(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	root := t.TempDir()
	repo := filepath.Join(root, taskHermesRepoMarker) // the path fragment IS the trigger
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustGitInit(t, repo)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "hermes-agent work", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit = %v (the host warning must never refuse a row)", err)
	}
	fields := strings.Fields(out)
	if len(fields) < 3 {
		t.Fatalf("confirmation unreadable: %q", out)
	}
	taskID := strings.TrimSpace(strings.TrimPrefix(fields[2], ""))
	if taskID == "" {
		t.Fatalf("no task id in %q", out)
	}
	host, _ := os.Hostname() //stylegate:ignore reads this host's name only to build the expected warning text for a host that is not this one
	want := taskHermesHostWarningFor(repo, host)
	switch {
	case want == "":
		// Running ON the owning host: the warning is correctly absent.
		if strings.Contains(out, "warning: repo") {
			t.Errorf("running on %s (the owner) but the submit warned:\n%s", host, out)
		}
	default:
		if !strings.Contains(out, want) {
			t.Errorf("submit confirmation is missing the host warning.\nwant:\n%s\ngot:\n%s", want, out)
		}
	}

	// The row exists and carries the repo stamp — the warning changes nothing
	// about the deliverable.
	show, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "deliverable.repo: "+repo) {
		t.Errorf("row lost its deliverable.repo stamp:\n%s", show)
	}
	if strings.TrimSpace(path) == "" {
		t.Fatal("no ledger path")
	}
}

// TestHermesHostWarningLiteralsLiveAtOneSite: the hostname and the warning
// template are REFERENCED, never re-typed (Rules 1b/13). Absence half scans the
// package; presence half makes deletion an unsatisfying answer.
func TestHermesHostWarningLiteralsLiveAtOneSite(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	needles := []string{
		`taskHermesOwnerHostDefault = "host-b"`,
		`HermesOwnerHostEnv = "GOPI_HERMES_OWNER_HOST"`,
		"warning: repo %q belongs to %s",
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == hermesHostWarningDefinitionFile {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		scanned++
		for _, n := range needles {
			if strings.Contains(string(data), n) {
				t.Errorf("%s carries the hermes-host literal %q — reference the constant in %s instead (Rule 13)",
					name, n, hermesHostWarningDefinitionFile)
			}
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d production files — the gate's glob is broken, refusing a vacuous pass", scanned)
	}
	def, err := os.ReadFile(hermesHostWarningDefinitionFile)
	if err != nil {
		t.Fatalf("read definition file: %v", err)
	}
	for _, n := range needles {
		if !strings.Contains(string(def), n) {
			t.Errorf("%s no longer defines %q — the gate cannot be satisfied by deletion", hermesHostWarningDefinitionFile, n)
		}
	}
}

// TestHermesOwnerHostIsDeployableNotWelded: the owning host is a DEPLOYMENT
// fact. Before this knob existed the owner was a bare constant, so a second
// fleet had no way to stop reading one operator's machine name as a rule about
// itself — every submit on its own owner box produced the "file it on HP"
// warning. Red-proof: with the constant-only form, setting the env var changed
// nothing and the warning still fired.
func TestHermesOwnerHostIsDeployableNotWelded(t *testing.T) {
	const repo = "/home/someone/workspace/hermes-agent"
	const fleetBox = "fleet-owner-1"

	// The operator's own fleet: the box IS the owner, so there is nothing to say.
	t.Setenv(HermesOwnerHostEnv, fleetBox)
	if got := taskHermesHostWarningFor(repo, fleetBox); got != "" {
		t.Fatalf("on the configured owner %q the warning must be silent, got: %s", fleetBox, got)
	}
	// And a NON-owner box still gets the warning, naming the CONFIGURED owner.
	got := taskHermesHostWarningFor(repo, "some-other-box")
	if got == "" {
		t.Fatal("a non-owner box must still be warned")
	}
	if !strings.Contains(got, fleetBox) {
		t.Errorf("the warning must name the configured owner %q, got: %s", fleetBox, got)
	}
	// Unset ⇒ the measured default owner is back in force.
	t.Setenv(HermesOwnerHostEnv, "")
	if taskHermesHostWarningFor(repo, taskHermesOwnerHostDefault) != "" {
		t.Error("with the knob unset the measured default owner must still silence the warning")
	}
}
