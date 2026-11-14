// launch_test.go — the clamp-escape launcher's shape (Rule 1: the pure parts
// are asserted here; the end-to-end launch is a heavy test, below).
package shot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/heavytest"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestAddressSpaceTooSmall pins the predicate that decides which launch path a
// run takes. The measured chrome reservation is ~51 GiB of virtual address
// space (VmPeak 5.49e7 kB), so anything below the 64 GiB threshold is "will not
// start" and anything at/above it — or RLIM_INFINITY — is fine.
func TestAddressSpaceTooSmall(t *testing.T) {
	cases := []struct {
		name string
		soft uint64
		want bool
	}{
		{"scratch clamp 12GiB", 12 << 30, true},
		{"one byte below the threshold", minChromeAddressSpaceBytes - 1, true},
		{"exactly the threshold", minChromeAddressSpaceBytes, false},
		{"measured reservation 51GiB (still below the 64GiB floor)", 51 << 30, true},
		{"unlimited", rlimitInfinity, false},
	}
	for _, c := range cases {
		if got := addressSpaceTooSmall(c.soft); got != c.want {
			t.Errorf("%s: addressSpaceTooSmall(%d) = %v, want %v", c.name, c.soft, got, c.want)
		}
	}
}

// TestSystemdRunArgsShape pins the escape's argv: the unit is a --user SERVICE
// (a --scope would inherit the caller's limits and defeat the whole point), it
// carries the replacement memory bound, and chrome's argv follows the "--".
func TestSystemdRunArgsShape(t *testing.T) {
	got := systemdRunArgs("gopi-shot-1-abc", "/tmp/p/chrome.err", 512,
		[]string{"TMPDIR=/tmp/declared"}, []string{"/snap/bin/chromium", "--headless=new", "about:blank"})
	joined := strings.Join(got, " ")

	for _, want := range []string{
		"--user",
		"--unit=gopi-shot-1-abc",
		"--collect",
		"-p MemoryMax=512M",
		"-p MemorySwapMax=0",
		"-p StandardError=file:/tmp/p/chrome.err",
		// A transient unit does NOT inherit the caller's environment (measured
		// on systemd 249: the unit's own /proc/self/environ carried no TMPDIR),
		// so the published temp workspace must be handed over explicitly or the
		// escape silently drops the temp-root contract.
		"--setenv=TMPDIR=/tmp/declared -- /snap/bin/chromium --headless=new about:blank",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("systemd-run argv missing %q:\n%s", want, joined)
		}
	}
	// A transient SCOPE is created as a child cgroup but INHERITS the caller's
	// process limits (measured) — the escape is the service-unit form only.
	if strings.Contains(joined, "--scope") {
		t.Errorf("systemd-run argv must not use --scope (it inherits the caller's limits):\n%s", joined)
	}
	// The unit name must be unique per run, or concurrent captures collide.
	if one, two := shotUnitName(), shotUnitName(); one == two {
		t.Errorf("shotUnitName returned the same name twice: %q", one)
	}
	if !strings.HasPrefix(shotUnitName(), shotUnitPrefix) {
		t.Errorf("unit name must carry the %q prefix", shotUnitPrefix)
	}
}

// TestChromeMemoryMaxMB covers the override knob: the default applies when
// unset, a declared positive integer wins, and a typo falls back to the default
// rather than dropping the bound.
//
// The per-case flip is a reviewed os.Setenv, not a t.Setenv: t.Setenv's undo
// scope is the TEST, so a t.Setenv inside this loop would leave the LAST case's
// value installed for the rest of the test (internal/stylegate
// RuleTestSetenvScope). The surrounding SnapshotEnv restores absence, not
// merely "".
func TestChromeMemoryMaxMB(t *testing.T) {
	defer testutil.SnapshotEnv(t, chromeMemMaxEnvName)()
	cases := []struct {
		value string
		want  int
	}{
		{"", defaultChromeMemoryMaxMB},
		{"1024", 1024},
		{"1", 1},
		{"nonsense", defaultChromeMemoryMaxMB},
		{"0", defaultChromeMemoryMaxMB},
		{"-5", defaultChromeMemoryMaxMB},
	}
	for _, c := range cases {
		// env-leak-ok: one reviewed per-case flip under a SnapshotEnv restore; a
		// t.Setenv here would persist the last case's value past the loop
		// (internal/stylegate RuleTestSetenvScope).
		//stylegate:ignore one reviewed per-case flip under a SnapshotEnv restore; a t.Setenv here would persist the last case's value past the loop, so the explicit pair is the correct instrument (RuleTestSetenvScope).
		os.Setenv(chromeMemMaxEnvName, c.value)
		if got := chromeMemoryMaxMB(); got != c.want {
			t.Errorf("%s=%q: chromeMemoryMaxMB() = %d, want %d", chromeMemMaxEnvName, c.value, got, c.want)
		}
	}
}

// TestChromeArgvShared pins that both launch paths use one argv definition —
// a drift here would silently change what the escape launches.
func TestChromeArgvShared(t *testing.T) {
	argv := chromeArgv("/profile/x")
	joined := strings.Join(argv, " ")
	for _, want := range []string{"--headless=new", "--remote-debugging-port=0", "--user-data-dir=/profile/x", "about:blank"} {
		if !strings.Contains(joined, want) {
			t.Errorf("chromeArgv missing %q: %s", want, joined)
		}
	}
}

// TestWaitDevToolsPortInFile reports the port from chrome's redirect file and
// fails (rather than hanging) when the line never appears.
func TestWaitDevToolsPortInFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, chromeErrFileName)
	line := "DevTools listening on ws://127.0.0.1:45321/devtools/browser/3abc8d3f-39fa-41bc-9fb7-6c3518fbdc8f\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	port, err := waitDevToolsPortInFile(path, chromeStartTimeout)
	if err != nil {
		t.Fatalf("waitDevToolsPortInFile: %v", err)
	}
	if port != "45321" {
		t.Errorf("port = %q, want 45321", port)
	}

	// A missing/never-matching file is a bounded failure, and the message
	// carries the file's first line as evidence.
	empty := filepath.Join(dir, "other.err")
	if err := os.WriteFile(empty, []byte("chrome: die\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := waitDevToolsPortInFile(empty, 50*time.Millisecond); err == nil {
		t.Error("waitDevToolsPortInFile on a portless file should fail, not hang or return nil")
	}
}

// TestLaunchChromeUserUnitIsTheHeavyPath is the end-to-end declaration: driving
// a real browser through a real systemd unit is device-exec work
// (CriterionDeviceExec), so it is off the merge/verify path per Rule 27.
func TestLaunchChromeUserUnitIsTheHeavyPath(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)

	bin, err := findChrome(homeDirOrEmpty())
	if err != nil {
		t.Skipf("no chrome on this host: %v", err)
	}
	if !addressSpaceClamped() {
		t.Skip("this process is not address-space clamped — the escape is not exercised")
	}
	profile := t.TempDir()
	proc, err := launchChrome(t.Context(), bin.Path, profile)
	if err != nil {
		t.Fatalf("launchChrome under the clamp: %v", err)
	}
	defer proc.stop()
	if proc.port == "" {
		t.Fatal("launchChrome returned an empty port")
	}
	list, err := listTargets("http://127.0.0.1:" + proc.port)
	if err != nil {
		t.Fatalf("listTargets on the escaped chrome: %v", err)
	}
	if len(list) == 0 {
		t.Error("escaped chrome reported no CDP targets")
	}
}

// homeDirOrEmpty is a small accessor so the test above does not inline
// os.UserHomeDir's two-value call in an argument list.
func homeDirOrEmpty() string {
	h, _ := os.UserHomeDir() //stylegate:ignore cache lookup; an empty home simply disables the cache path
	return h
}
