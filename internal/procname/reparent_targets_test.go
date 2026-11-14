package procname

// The reparent-target set is the MECHANISM `ppid == 1` was a shorthand for, so it
// gets its own assertions: this host's answer is measured, not assumed
// (bug row 01a12e43, 2026-10-12).
//
// THE MEASURED FACT these pin (host-a, 2026-10-12, artifacts in the row's
// session dir — /proc/sys/kernel/child_subreaper does not exist on this 6.8
// kernel, so the mark itself is unreadable and the set must be DERIVED):
//
//	$ sh -c 'sleep 300 & echo $!'; awk '{print $4}' /proc/<orphan>/stat
//	ppid=3100            # /lib/systemd/systemd --user, comm "systemd", argv --user
//	$ awk '{print $4}' /proc/3100/stat
//	1                    # the manager is a child of init, so init alone is not the target
//
// A predicate that demanded ppid == 1 therefore judged EVERY orphan on this host
// as "not an orphan" — and the six fixtures that did so skipped silently instead
// of failing.

import (
	"os"
	"strconv"
	"testing"
)

func TestOrphanReparentTargetsIncludesTheNamespaceInit(t *testing.T) {
	targets, err := OrphanReparentTargets(DefaultProcRoot)
	if err != nil {
		t.Fatalf("OrphanReparentTargets(%s): %v — the set must be measurable on a host with procfs", DefaultProcRoot, err)
	}
	if !targets[OrphanReparentPPID] {
		t.Errorf("the namespace init (pid %d) is always a reparent target — it is what remains when no ancestor is marked; set=%v", OrphanReparentPPID, targets)
	}
}

// TestOrphanReparentTargetsNamesThisHostsUserManager is the assertion that makes
// the set worth having, and it is deliberately DIFFERENTIAL: it measures the live
// systemd user instance first (by its own comm + --user argv, the same positive
// identification the production reader uses) and only then demands that the set
// contains it. On a host with no user session there is nothing to demand, and
// saying so is honest rather than passing on an empty comparison.
func TestOrphanReparentTargetsNamesThisHostsUserManager(t *testing.T) {
	targets, err := OrphanReparentTargets(DefaultProcRoot)
	if err != nil {
		t.Fatalf("OrphanReparentTargets(%s): %v", DefaultProcRoot, err)
	}
	entries, err := os.ReadDir(DefaultProcRoot)
	if err != nil {
		t.Fatalf("read %s: %v", DefaultProcRoot, err)
	}
	found := 0
	for _, e := range entries {
		if _, convErr := strconv.Atoi(e.Name()); convErr != nil {
			continue
		}
		if !userManagerInstance(DefaultProcRoot, e.Name()) {
			continue
		}
		found++
		pid, _ := strconv.Atoi(e.Name()) //stylegate:ignore optional numeric field of a log line; 0 is the documented fallback
		if !targets[pid] {
			t.Errorf("pid %d IS a live systemd user instance (comm %s, argv carries %s) and the kernel hands orphans to it, but the target set omits it: set=%v",
				pid, UserManagerComm, UserManagerArg, targets)
		}
	}
	t.Logf("live systemd user instances on this host: %d (target set: %v)", found, targets)
}

// TestOrphanReparentTargetsRefusesAnUnreadableRoot pins the loud half: a procfs
// root that cannot be read is an ERROR, never an empty set. An empty set would
// make every caller compare an orphan's ppid against nothing and classify NOTHING
// — a lost measurement spent as a verdict (Rule 15).
func TestOrphanReparentTargetsRefusesAnUnreadableRoot(t *testing.T) {
	missing := t.TempDir() + "/no-such-procfs"
	targets, err := OrphanReparentTargets(missing)
	if err == nil {
		t.Fatalf("an unreadable procfs root must be an error, got set=%v", targets)
	}
	if targets != nil {
		t.Errorf("a failed read must not return a partial set, got %v", targets)
	}
}
