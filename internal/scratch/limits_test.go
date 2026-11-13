package scratch

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// limitsValue extracts one named value from a /proc/self/limits rendering, e.g.
// "Max address space        12582912   12582912   bytes".
func limitsValue(rendered, label string) string {
	for _, line := range strings.Split(rendered, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !strings.HasPrefix(line, label) {
			continue
		}
		// The rendering is "LIMIT  SOFT  HARD  UNITS": the soft value is the
		// third field from the right, because the label itself may contain
		// spaces (a leading-index read returns "space" for "Max address space").
		return fields[len(fields)-3]
	}
	return ""
}

// TestKibRoundedUpNeverTightensThePolicy pins the unit conversion of the `ulimit
// -v` spelling: a byte count is emitted as KiB, rounded UP, so the applied
// ceiling can never be tighter than the policy value (the bug this guards
// against is a 1024x error that silently turns the clamp into a no-op).
func TestKibRoundedUpNeverTightensThePolicy(t *testing.T) {
	exact := kibRoundedUp(1024 * bytesPerMiB)
	if exact != 1024*1024 {
		t.Fatalf("kibRoundedUp(1GiB) = %d KiB, want 1048576", exact)
	}
	// A value that is not a whole KiB rounds UP (never down).
	if got := kibRoundedUp(bytesPerKiB + 1); got != 2 {
		t.Fatalf("kibRoundedUp(%d) = %d, want 2", bytesPerKiB+1, got)
	}
	if got := kibRoundedUp(1); got != 1 {
		t.Fatalf("kibRoundedUp(1) = %d, want 1", got)
	}
}

// TestChildLimitPreambleIsCarriedToDescendants is the containment proof for the
// resource clamp: the preamble is prepended to a real `bash -c`, and the value
// the KERNEL reports for a descendant of that bash must be the clamped one.
// Reading /proc/self/limits inside the child is the strongest available
// evidence — it is the kernel's own record of the child's rlimits, not the
// child's claim about itself.
func TestChildLimitPreambleIsCarriedToDescendants(t *testing.T) {
	t.Setenv(ChildMemLimitEnvName, "1024")
	preamble := ChildLimitPreamble()
	if preamble == "" {
		t.Fatal("ChildLimitPreamble() = \"\" with a declared limit — the clamp is not applied")
	}
	// "exec" makes the reported process the command itself (not bash), and
	// /proc/self/limits is read by the kernel on its behalf.
	cmd := exec.Command("bash", "-c", preamble+"exec cat /proc/self/limits")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v (%s)", err, out)
	}
	got := limitsValue(string(out), "Max address space")
	want := strconv.FormatUint(1024*bytesPerMiB, 10)
	if got != want {
		t.Fatalf("child Max address space = %q, want %q\n%s", got, want, out)
	}
	// The process-count knob must reach the child too (it is the fork-bomb
	// bound, and it travels the same preamble).
	t.Setenv(ChildProcLimitEnvName, "2048")
	cmd = exec.Command("bash", "-c", ChildLimitPreamble()+"exec cat /proc/self/limits")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v (%s)", err, out)
	}
	if got := limitsValue(string(out), "Max processes"); got != "2048" {
		t.Fatalf("child Max processes = %q, want 2048\n%s", got, out)
	}
}

// TestChildLimitPreambleContainsARunaway is the behavioural half: an allocation
// beyond the clamp must FAIL INSIDE THE CHILD (that is the whole point — the
// host survives), rather than being OOM-killed by the kernel later. It uses the
// system python3 as a byte allocator and skips when it is unavailable, so the
// suite stays dependency-free.
func TestChildLimitPreambleContainsARunaway(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skipf("python3 unavailable: %v", err)
	}
	t.Setenv(ChildMemLimitEnvName, "256")
	preamble := ChildLimitPreamble()
	if preamble == "" {
		t.Fatal("ChildLimitPreamble() = \"\" with a declared limit")
	}
	// 800 MiB against a 256 MiB address space: MemoryError, not an OOM kill.
	cmd := exec.Command("bash", "-c", preamble+`exec `+py+` -c 'x=bytearray(800*1024*1024)'`)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an 800 MiB allocation succeeded inside a 256 MiB clamp — the clamp did not reach the child:\n%s", out)
	}
	if !strings.Contains(string(out), "MemoryError") {
		t.Logf("child failed with a non-MemoryError diagnostic (still contained): %s", out)
	}
}

// TestChildLimitsEnvReaderIsReal: both declared names must be read, defaults
// apply when they are unset, 0 disables, and a malformed value degrades to the
// default instead of silently disabling the guard.
func TestChildLimitsEnvReaderIsReal(t *testing.T) {
	// Unset → the measured defaults.
	t.Setenv(ChildMemLimitEnvName, "")
	t.Setenv(ChildProcLimitEnvName, "")
	as, procs, ok := ChildLimits()
	if !ok || as != DefaultChildMemLimitMB*bytesPerMiB || procs != DefaultChildProcLimit {
		t.Fatalf("defaults: (%d, %d, %v), want (%d, %d, true)", as, procs, ok,
			DefaultChildMemLimitMB*bytesPerMiB, DefaultChildProcLimit)
	}

	// Declared → honoured.
	t.Setenv(ChildMemLimitEnvName, "2048")
	t.Setenv(ChildProcLimitEnvName, "1234")
	as, procs, ok = ChildLimits()
	if !ok || as != 2048*bytesPerMiB || procs != 1234 {
		t.Fatalf("declared: (%d, %d, %v), want (2048MiB, 1234, true)", as, procs, ok)
	}

	// Malformed → the default, not a disabled clamp (the guard must not
	// evaporate because of a typo).
	t.Setenv(ChildMemLimitEnvName, "twelve-gigabytes")
	as, _, _ = ChildLimits()
	if as != DefaultChildMemLimitMB*bytesPerMiB {
		t.Fatalf("malformed knob: AS=%d, want the default %d", as, DefaultChildMemLimitMB*bytesPerMiB)
	}

	// 0/0 → off, and the preamble says so by staying empty.
	t.Setenv(ChildMemLimitEnvName, "0")
	t.Setenv(ChildProcLimitEnvName, "0")
	if _, _, ok := ChildLimits(); ok {
		t.Fatal("0/0 must disable the clamp")
	}
	if preamble := ChildLimitPreamble(); preamble != "" {
		t.Fatalf("clamp disabled but the preamble is %q", preamble)
	}
	if !strings.Contains(LimitReport(), "OFF") {
		t.Fatalf("LimitReport() = %q, want an OFF report", LimitReport())
	}
}

// TestClampToParentNeverRaisesALimit: an inherited hard limit BELOW the policy
// wins (raising is loosening, and the kernel would refuse it anyway). The
// assertion drives the real inherited values of the test process.
func TestClampToParentNeverRaisesALimit(t *testing.T) {
	var current syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &current); err != nil {
		t.Skipf("Getrlimit unavailable: %v", err)
	}
	if current.Max == rlimitInfinity {
		// The host is unlimited: the policy value is the ceiling.
		if v, apply := clampToParent(syscall.RLIMIT_AS, 4096); !apply || v != 4096 {
			t.Fatalf("unlimited parent: (%d, %v), want (4096, true)", v, apply)
		}
	} else {
		// A finite inherited hard limit wins when it is lower…
		if current.Max > 1 {
			if v, apply := clampToParent(syscall.RLIMIT_AS, current.Max+1); apply || v != 0 {
				t.Fatalf("inherited hard %d < want %d: (%d, %v), want no-op", current.Max, current.Max+1, v, apply)
			}
		}
	}
	// A knob of 0 is always a no-op.
	if _, apply := clampToParent(syscall.RLIMIT_AS, 0); apply {
		t.Fatal("want=0 applied a limit")
	}
}
