package main

// Retired-merge-suite survivors (cmd/agent).
//
// Same story as internal/tools/retired_merge_suite_survivors_test.go: these
// declarations lived in merge test files, and non-merge tests still call them
// after the merge suite was retired (task 01a17fdc). Carried out VERBATIM.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// mergeLocaleProbeValue is a locale whose git translation differs from English —
// the condition under test. It is a value with meaning (asserted on, printed in a
// skip message), so it is named rather than spelled at the use site.
const mergeLocaleProbeValue = "zh_CN.UTF-8"

// mergeLocaleIsGeneratable reports whether this host can actually produce the
// locale above, so the behavioural test skips LOUDLY (with the command to fix it)
// instead of passing for the wrong reason on a host that reads English anyway.
//
// IT ASKS glibc, NOT `locale -a`, on purpose: the listing spells the same locale
// `zh_CN.utf8` while a caller writes `zh_CN.UTF-8`, so a string comparison against
// the listing is red on a host where the locale works perfectly (measured — git
// translated fine while this probe said "cannot generate"). Whether a locale is
// usable is what `locale` itself reports when it is pinned to it.
func mergeLocaleIsGeneratable(t *testing.T, locale string) bool {
	t.Helper()
	cmd := exec.Command("locale", "charmap")
	cmd.Env = append(mergeGitEnvWithoutLocale(), "LC_ALL="+locale, "LANG="+locale)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("`locale charmap` under LC_ALL=%s failed (%v): %s", locale, err, strings.TrimSpace(string(out)))
		return false
	}
	return !strings.Contains(string(out), mergeLocaleUnavailableMarker)
}

// procAliveForTest reports whether pid is a live, RUNNING process. Signal 0 is
// not enough on its own: it also succeeds for a ZOMBIE, which would read as
// "still running". A pid we are not permitted to signal (EPERM) counts as alive.
func procAliveForTest(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return !isZombieForTest(pid)
	}
	return errors.Is(err, syscall.EPERM)
}

// mdProcAlive reports whether pid is a live, RUNNING process. Signal 0 alone
// is not enough: it also succeeds for a ZOMBIE (a child this test process has
// not reaped yet), which would read as "still running" — a false alarm about a
// process that is in fact already dead.
func mdProcAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	return !mdIsZombie(pid)
}

// mergeGitEnvWithoutLocale is mergeGitEnv's parent environment with the locale
// spellings removed, for a probe that pins its OWN locale.
func mergeGitEnvWithoutLocale() []string {
	env := mergeGitEnv()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "LC_ALL=") || strings.HasPrefix(kv, "LANG=") ||
			strings.HasPrefix(kv, "LC_MESSAGES=") || strings.HasPrefix(kv, "LANGUAGE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// mergeLocaleUnavailableMarker is what glibc prints when a locale cannot be set —
// the one string that distinguishes "this host has no such locale" from a locale
// that merely spells its charmap differently.
const mergeLocaleUnavailableMarker = "Cannot set"

// isZombieForTest reads the state letter out of /proc/<pid>/stat; an unreadable
// entry is reported as NOT a zombie (conservative: prefer "alive" to silently
// accepting a dead process as running).
func isZombieForTest(pid int) bool {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	i := strings.LastIndex(string(b), ")")
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] == 'Z'
}

// mdIsZombie reads the process state letter out of /proc/<pid>/stat. An
// unreadable /proc entry is reported as NOT a zombie (conservative: prefer
// "alive" to silently accepting a live process as dead).
func mdIsZombie(pid int) bool {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	// The state letter follows the (quoted, possibly space-bearing) comm
	// field — find the LAST ')' so a comm like "gopi (x)" cannot shift it.
	i := strings.LastIndex(string(b), ")")
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] == 'Z'
}
