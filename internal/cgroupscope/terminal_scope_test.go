package cgroupscope

// terminal_scope_test.go — the unit half of the terminal-scope HINT (task
// 01a163f3, measured 2026-10-22 22:04:55: a foreground `gopi worker` inside a
// `vte-spawn-*.scope` was HANGED UP when its terminal tab was torn down,
// `sourced:false`).
//
// WHAT IS PINNED HERE is only the PREDICATE — "is this cgroup path a terminal's
// or a login's?" The banner that consumes it, and the fact that `--daemon` does
// not print it, are pinned where that wiring lives (cmd/agent). The paths below
// are FIXTURES read off this host's own cgroup tree and the daemon seam's own
// naming rule (EscapeTargetPrefix), not invented shapes.

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The three answers the hint rests on, in the exact spellings measured here.
const (
	// terminalScopeFixture is /proc/self/cgroup on this host (a terminal tab).
	terminalScopeFixture = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"app-org.gnome.Terminal.slice/vte-spawn-668117f9-0083-4d3d-9197-a71f438c5647.scope"
	// daemonScopeFixture is the scope `gopi worker --daemon` acquires through the
	// daemon seam: named from EscapeTargetPrefix, so it must NEVER read as a
	// terminal — that is the whole point of the escape.
	daemonScopeFixture = "/user.slice/user-1000.slice/user@1000.service/app.slice/" +
		"gopi-daemon-worker-hosta.scope"
)

// TestTerminalScope_RecognizesATerminalTab pins case (a): the measured vte
// shape is a terminal scope, and every OTHER terminal-backed shape this host
// really produces is too.
func TestTerminalScope_RecognizesATerminalTab(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{terminalScopeFixture, true,
			"case (a): the measured shape — this host's own /proc/self/cgroup, a GNOME Terminal tab"},
		{terminalScopeOfUD, true, "the other vte fixture already used by the session-scope test"},
		{terminalScopeFixture + "/nested", true,
			"a child cgroup of a terminal scope still belongs to the terminal: its teardown reaches the child"},
		{sessionScopeOfUD, true,
			"an ssh login's session scope hangs up the same way (`session-N.scope`)"},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice/app-tmux-1234.scope", true,
			"a tmux server shipped as its own scope"},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice/tmux-spawn-abc.scope", true,
			"the tmux-spawn spelling some distributions use"},
		{"/machine.slice/machine-lxc\\x2dpayload.slice/payload/system.slice/lxc-payload-scope-1.scope", false,
			"a container payload scope carries NO terminal marker: the hint stays quiet rather than guessing " +
				"(the tolerable direction — a wrong verdict would tell an operator to detach something that " +
				"was never terminal-bound)"},
	}
	for _, c := range cases {
		if got := TerminalScope(c.rel); got != c.want {
			t.Errorf("TerminalScope(%q) = %v, want %v (%s)", c.rel, got, c.want, c.why)
		}
	}
}

// TestTerminalScope_RefusesTheDaemonEscape pins case (b) — the one negative the
// banner's whole usefulness depends on: a `gopi-daemon-worker-*.scope` is a
// scope, but it is OURS, and reporting it as terminal-bound would fire the hint
// on exactly the face that already solved the problem.
func TestTerminalScope_RefusesTheDaemonEscape(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{daemonScopeFixture, false,
			"case (b): the escape target as the daemon seam leaves it — still a *.scope, but named after " +
				"EscapeTargetPrefix, so it is outside the terminal's lifetime"},
		{"/user.slice/user-1000.slice/user@1000.service/gopi-daemon", false,
			"mechanism ② (the in-process cgroupfs escape): a plain cgroup under the manager, no scope at all"},
		{"/user.slice/user-1000.slice/user@1000.service", false, "the user manager outlives every session"},
		{"/system.slice/ssh.service", false, "a system service is nobody's terminal"},
		{"/user.slice/user-1000.slice", false, "the per-uid slice is login machinery, not a session"},
		{"/", false, "the cgroup root"},
		{desktopSessionScopeOfUD, false,
			"a DESKTOP session scope is a session but NOT a terminal: nothing an operator closes takes it " +
				"down, so the hint must stay quiet (this is why TerminalScope is narrower than InSessionScope)"},
		{"/user.slice/user-1000.slice/session-4.slice", false,
			"a .slice is not a .scope — a slice carrying a terminal's name is still not the terminal"},
	}
	for _, c := range cases {
		if got := TerminalScope(c.rel); got != c.want {
			t.Errorf("TerminalScope(%q) = %v, want %v (%s)", c.rel, got, c.want, c.why)
		}
	}
}

// TestTerminalScope_NoMeasurementIsNotATerminal pins case (c)'s first half: an
// empty path (nothing was read) and a relative one (not a cgroup membership
// line) are both "no verdict", i.e. silent. A predicate that answered true here
// would make the banner fire on hosts where /proc/<pid>/cgroup is unreadable —
// teaching an operator to skip the one line that matters.
func TestTerminalScope_NoMeasurementIsNotATerminal(t *testing.T) {
	for _, rel := range []string{"", "  ", "user.slice/user-1000.slice/session-1.scope"} {
		if TerminalScope(rel) {
			t.Errorf("TerminalScope(%q) = true; an unmeasured/relative path is never a terminal scope", rel)
		}
	}
}

// TestRelOfPID_UnreadableCgroupIsAnError is case (c)'s second half at the
// SOURCE: the only accepted evidence (a fresh /proc/<pid>/cgroup read) fails
// LOUDLY when it cannot be taken, so the caller's silence is a decision rather
// than an accident of a zero value.
func TestRelOfPID_UnreadableCgroupIsAnError(t *testing.T) {
	if _, err := RelOfPID(getpidForUnreadable()); err == nil {
		t.Fatal("RelOfPID returned no error for an unreadable pid — silence about where a process sits " +
			"is exactly the defect Rule 24 exists to stop")
	}
	// And the real read of THIS process answers something usable: it is the
	// measurement the hint is built on, and on every cgroup-v2 host it must
	// succeed (this host is unified, so a failure here is the finding).
	rel, err := SelfRel()
	if err != nil {
		t.Fatalf("SelfRel: %v — this host should be cgroup v2 unified", err)
	}
	if !strings.HasPrefix(rel, "/") {
		t.Errorf("SelfRel() = %q, want an absolute cgroup path", rel)
	}
}

// getpidForUnreadable returns a pid that cannot exist, so its /proc entry is
// absent and the read fails. The value is derived (never a bare literal at the
// read site) and is far above any PID_MAX a Linux host would use.
func getpidForUnreadable() int {
	return os.Getpid() + impossiblePidOffset
}

// impossiblePidOffset is the distance past the running pid that no live process
// occupies. Linux's default PID_MAX is 4_194_304, and this suite's own pid is
// far below it, so pid+offset cannot be a live process.
const impossiblePidOffset = 3_000_000

// TestUnreadablePidPath pins the fact the case above relies on: the pid really
// has no /proc entry (a suite that "proved" unreadability against an existing
// directory would be testing nothing).
func TestUnreadablePidPath(t *testing.T) {
	path := "/proc/" + strconv.Itoa(getpidForUnreadable())
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists; the unreadable-cgroup case must point at an absent pid", path)
	}
}
