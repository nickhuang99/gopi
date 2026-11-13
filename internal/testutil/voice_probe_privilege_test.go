package testutil

// The PRIVILEGE CONFLATION guard for scripts/voice-mic-probe.sh (Rule 5:
// every bug fix ships its minimal repro test).
//
// The bug: the probe measured ONE privilege level (the calling uid) and the
// documents built on it read that level's refusals as the NODE's capability.
// On zbook uid 1000 is outside group `audio`, so ALSA renders its access
// refusal as "no soundcards found" — and the epic concluded "zbook has no
// capture path", which is false: the same hardware records as root (`sudo -n`,
// NOPASSWD) and as uid 1000 with group `audio` added.
//
// This file makes that conclusion unreachable in two halves:
//
//	STATIC  — the script declares the privilege vocabulary as constants and the
//	          misleading ALSA phrasing is a constant too (Rule 1b/13: no bare
//	          literals, and a declared constant must actually be referenced).
//	BEHAVIOURAL — a fake `arecord` and a fake `sudo` on PATH drive the probe
//	          through both privilege branches with no audio hardware, no root
//	          and no interactive input, and the resulting table is asserted:
//	          a device that only captures privileged must be reported
//	          SUDO_REQUIRED and the node verdict CAPTURE_SUDO_REQUIRED — never
//	          UNUSABLE / NO_SOUNDCARDS. A node with no passwordless sudo must
//	          report SUDO_UNAVAILABLE rows and the honest UNMEASURED verdict.
//
// Everything is hermetic: the fake binaries live in a t.TempDir() (off `/`,
// per the scratch pin), the probe's artifacts go to a t.TempDir(), and no real
// device or privilege is touched.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/heavytest"
)

const (
	// voiceProbeScript is the audited probe, repo-root relative.
	voiceProbeScript = "scripts/voice-mic-probe.sh"
	// The OUTCOME / mode vocabulary the fix introduced. A rename here without
	// the script (or the reverse) must fail, so both spellings live in one
	// place: this const block matches the script's `readonly` names.
	voiceProbeSudoRequiredConst    = "OUTCOME_SUDO_REQUIRED"
	voiceProbeSudoUnavailableConst = "OUTCOME_SUDO_UNAVAILABLE"
	voiceProbeSudoRequiredValue    = "SUDO_REQUIRED"
	voiceProbeSudoUnavailValue     = "SUDO_UNAVAILABLE"
	voiceProbeModeUserConst        = "PRIV_MODE_USER"
	voiceProbeModeRootConst        = "PRIV_MODE_ROOT"
	voiceProbeModeUserValue        = "user"
	voiceProbeModeRootValue        = "root"
	voiceProbeSudoBinaryConst      = "SUDO_BINARY"
	voiceProbeSudoFlagConst        = "SUDO_NONINTERACTIVE_FLAG"
	voiceProbeSudoTimeoutConst     = "SUDO_TIMEOUT_SEC"
	voiceProbeNoSoundcardsConst    = "ARECORD_NO_SOUNDCARDS_MARKER"
	voiceProbeNoSoundcardsValue    = "no soundcards found"
	// voiceProbeSudoFunnel is the one function allowed to spell the sudog
	// invocation; every privileged call must route through it.
	voiceProbeSudoFunnel = "run_sudo()"
	// voiceProbeVerdicts are the CAPTURE_TRUTH verdict lines the fix introduced.
	voiceProbeVerdictSudoRequired = "CAPTURE_SUDO_REQUIRED"
	voiceProbeVerdictUnmeasured   = "UNMEASURED"
	voiceProbeVerdictOk           = "CAPTURE_OK"
	// voiceProbeTableSentinelBegin/End bound the machine-readable table in
	// stdout (section() prints them inside a `===== … =====` banner, so the
	// parser matches by containment).
	voiceProbeTableSentinelBegin = "VOICE_MIC_TABLE_BEGIN"
	voiceProbeTableSentinelEnd   = "VOICE_MIC_TABLE_END"
	// voiceProbeFakeCardLine is what the privileged fake `arecord -l` prints.
	voiceProbeFakeCardLine = "card 1: FAKE [Fake Card], device 0: FakeMic [FakeMic]"
	// voiceProbeFakeWAVPayload is the PCM the privileged fake capture emits
	// (8 samples of silence is enough: the probe counts bytes past the header).
	voiceProbeFakeWAVPayload = 64
)

// voiceProbeSudoInvocationRe matches sudo in a real COMMAND position (start of a
// command, or after a shell separator) — never a mention in prose or a quoted
// log message.
var voiceProbeSudoInvocationRe = regexp.MustCompile(`(^|[;&|]\s*)sudo\s`)

// TestVoiceProbePrivilegeVocabularyIsDeclaredAndUsed is the STATIC half: the
// privilege dimension is a first-class, named, referenced part of the probe.
func TestVoiceProbePrivilegeVocabularyIsDeclaredAndUsed(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, voiceProbeScript))
	if err != nil {
		t.Fatalf("read %s: %v", voiceProbeScript, err)
	}
	body := string(raw)

	for _, c := range []string{
		voiceProbeSudoRequiredConst, voiceProbeSudoUnavailableConst,
		voiceProbeModeUserConst, voiceProbeModeRootConst,
		voiceProbeSudoBinaryConst, voiceProbeSudoFlagConst, voiceProbeSudoTimeoutConst,
		voiceProbeNoSoundcardsConst, voiceProbeSudoFunnel,
	} {
		if !strings.Contains(body, c) {
			t.Errorf("%s does not declare %s — the privilege dimension is what this probe exists to measure; "+
				"a probe that measures one privilege level and reports it as the node's capability is the "+
				"defect this guard exists for (see devdoc/voice-mic-probe-privileged.md)", voiceProbeScript, c)
		}
	}

	// Rule 13: a declared constant that is never referenced is a bare literal
	// in disguise. Each of these must appear at least twice (declaration + use).
	referenced := map[string]int{
		voiceProbeSudoRequiredConst:    2,
		voiceProbeSudoUnavailableConst: 2,
		voiceProbeModeUserConst:        2,
		voiceProbeModeRootConst:        2,
		voiceProbeSudoBinaryConst:      2,
		voiceProbeSudoFlagConst:        2,
		voiceProbeNoSoundcardsConst:    2,
	}
	for name, want := range referenced {
		if got := strings.Count(body, name); got < want {
			t.Errorf("%s references %s %d time(s), want at least %d — a declared-but-unused constant "+
				"means the value is being typed inline somewhere (Rule 13)", voiceProbeScript, name, got, want)
		}
	}

	// The sudo invocation itself must live only in the funnel: a scattered
	// `sudo -n` is how a probe acquires an un-timed, prompting call that hangs a
	// non-interactive harness. Only a real COMMAND position counts (a bare name
	// after a separator) — prose ("rerun with sudo") and the funnel's own
	// `"$SUDO_BINARY"` are not invocations.
	for i, line := range strings.Split(body, "\n") {
		code := stripComment(line)
		if code == "" || strings.Contains(code, voiceProbeSudoBinaryConst) {
			continue
		}
		if !voiceProbeSudoInvocationRe.MatchString(code) {
			continue
		}
		t.Errorf("%s:%d spells a sudo invocation outside %s: %q — route it through the funnel so it "+
			"keeps the -n (never prompt) flag and the wall-clock timeout",
			voiceProbeScript, i+1, voiceProbeSudoFunnel, strings.TrimSpace(line))
	}

	// The misleading ALSA phrasing must not be typed inline where it MATTERS:
	// the outcome classification is the one site whose literal would silently
	// undo the fix (a re-inlined grep is how the old probe misread the access
	// refusal). Prose that quotes the phrase to a human is documentation.
	for i, line := range strings.Split(body, "\n") {
		code := stripComment(line)
		if !strings.Contains(code, voiceProbeNoSoundcardsValue) {
			continue
		}
		if strings.Contains(code, voiceProbeNoSoundcardsConst) || !strings.Contains(code, "grep") {
			continue
		}
		t.Errorf("%s:%d classifies with the bare ALSA phrasing %q — reference %s: this exact string is "+
			"ALSA's rendering of an ACCESS refusal, and the whole defect was reading it as a "+
			"hardware fact", voiceProbeScript, i+1, voiceProbeNoSoundcardsValue, voiceProbeNoSoundcardsConst)
	}
}

// TestVoiceProbeReportsSudoRequiredNotUnusable is the BEHAVIOURAL half, case 1:
// passwordless sudo IS available and the device captures only under it.
func TestVoiceProbeReportsSudoRequiredNotUnusable(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	out := runFakeProbe(t, true)
	rows := parseProbeTable(t, out)
	if len(rows) == 0 {
		t.Fatalf("the probe produced no table rows — the fake harness did not reach the capture section:\n%s", out)
	}
	if !tableHasOutcome(rows, voiceProbeSudoRequiredValue) {
		t.Errorf("no %s row in the table: a device that captures ONLY privileged must say so — reporting it "+
			"as UNUSABLE/%s is the exact defect (table:\n%s)",
			voiceProbeSudoRequiredValue, voiceProbeNoSoundcardsValue, strings.Join(rows, "\n"))
	}
	if strings.Contains(out, voiceProbeVerdictOk) {
		t.Errorf("the node verdict claims %s although no unprivileged capture succeeded — the verdict must "+
			"never round a privilege-gated node up to a plain success\n%s", voiceProbeVerdictOk, out)
	}
	if !strings.Contains(out, voiceProbeVerdictSudoRequired) {
		t.Errorf("CAPTURE_TRUTH does not report %s for a privilege-gated node:\n%s", voiceProbeVerdictSudoRequired, out)
	}
	// The mode column carries the privilege level of each row, and every row
	// must name one (an unlabelled row is the old, ambiguous table).
	for _, row := range rows {
		f := strings.Split(row, "\t")
		if len(f) < 11 {
			t.Fatalf("row has %d columns, want at least 11 (mode appended): %q", len(f), row)
		}
		if f[10] != voiceProbeModeUserValue && f[10] != voiceProbeModeRootValue {
			t.Errorf("row's mode column is %q, want %q or %q: %q",
				f[10], voiceProbeModeUserValue, voiceProbeModeRootValue, row)
		}
	}
}

// TestVoiceProbeReportsSudoUnavailableHonestly is the BEHAVIOURAL half, case 2:
// no passwordless sudo. The privilege level was NOT measured, and the probe must
// say that rather than emitting a verdict it cannot support.
func TestVoiceProbeReportsSudoUnavailableHonestly(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionHostCondition)
	out := runFakeProbe(t, false)
	rows := parseProbeTable(t, out)
	if !tableHasOutcome(rows, voiceProbeSudoUnavailValue) {
		t.Errorf("no %s row although the fake sudo refuses: an unmeasured privilege level must be recorded, "+
			"never silently missing (table:\n%s)", voiceProbeSudoUnavailValue, strings.Join(rows, "\n"))
	}
	if tableHasOutcome(rows, voiceProbeSudoRequiredValue) {
		t.Errorf("the probe claims %s although it never reached the privileged level — a verdict it cannot "+
			"support (table:\n%s)", voiceProbeSudoRequiredValue, strings.Join(rows, "\n"))
	}
	if !strings.Contains(out, voiceProbeVerdictUnmeasured) {
		t.Errorf("CAPTURE_TRUTH does not report %s when the privileged level is unreachable:\n%s",
			voiceProbeVerdictUnmeasured, out)
	}
}

// runFakeProbe runs the real probe against a fake audio stack and returns its
// stdout. sudoWorks=false installs a `sudo` that always fails (the npm-less
// host); sudoWorks=true installs a `sudo` that runs its command (the zbook
// case: passwordless sudo, so the privileged branch is reachable).
func runFakeProbe(t *testing.T, sudoWorks bool) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	binDir := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "probe-out")
	writeVoiceProbeExecutable(t, filepath.Join(binDir, "sudo"), fmt.Sprintf(`#!/usr/bin/env bash
# fake sudo: %s
%s
[ "$1" = "-n" ] && shift
export FAKE_PROBE_PRIV=1
exec "$@"
`, passFailLine(sudoWorks), sudoRefusalBody(sudoWorks)))
	writeVoiceProbeExecutable(t, filepath.Join(binDir, "arecord"), `#!/usr/bin/env bash
# fake arecord: the CARDS THE USER SEES depend on the privilege level, which is
# exactly the axis the probe must separate. A privileged caller sees the card
# and can capture; an unprivileged one gets ALSA's misleading refusal text.
# The fake sudo sets FAKE_PROBE_PRIV=1 only in the CHILD it execs, so the user
# rows stay unprivileged (parity with the real probe, which never sees such a flag).
priv="${FAKE_PROBE_PRIV:-0}"
has() { case " $* " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }
if has -l; then
    if [ "$priv" = "1" ]; then
        echo "**** List of CAPTURE Hardware Devices ****"
        echo "`+voiceProbeFakeCardLine+`"
        exit 0
    fi
    echo "arecord: device_list:274: `+voiceProbeNoSoundcardsValue+`..." >&2
    exit 1
fi
if has -L; then
    if [ "$priv" = "1" ]; then echo "plughw:CARD=FAKE,DEV=0"; echo "default"; exit 0; fi
    echo "arecord: device_list:274: `+voiceProbeNoSoundcardsValue+`..." >&2
    exit 1
fi
# a capture attempt: only the privileged caller gets audio
if [ "$priv" != "1" ]; then
    echo "ALSA lib confmisc.c:165:(snd_config_get_card) Cannot get card index for 1" >&2
    echo "arecord: main:831: audio open error: No such file or directory" >&2
    exit 1
fi
printf 'RIFF'; head -c 104 /dev/zero; head -c `+fmt.Sprint(voiceProbeFakeWAVPayload)+` /dev/zero
exit 0
`)
	script := filepath.Join(root, voiceProbeScript)
	cmd := exec.Command("bash", script, "--out", outDir, "--node", "faketest")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOPI_VOICE_PROBE_TRIALS=1",
		"GOPI_VOICE_PROBE_OUT="+outDir,
	)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s must exit 0 whenever it ran (the fake stack is a RESULT, not an error): %v\n%s",
			voiceProbeScript, err, raw)
	}
	return string(raw)
}

// passFailLine returns a description of the fake sudo's behaviour.
func passFailLine(works bool) string {
	if works {
		return "accepts -n without a password (the zbook case: NOPASSWD: ALL)"
	}
	return "refuses -n (no passwordless sudo on this host)"
}

// sudoRefusalBody is the executable half: a fake sudo that WORKS runs its
// command; one that does not exits 1 with sudo's own -n refusal, which is what
// a host without NOPASSWD does in a non-interactive shell.
func sudoRefusalBody(works bool) string {
	if works {
		return `:` // no-op: a working sudo just runs the command
	}
	return `echo "sudo: a password is required" >&2
exit 1`
}

// writeVoiceProbeExecutable writes a 0755 shell script (a distinct name: the
// package already has a writeExecutable for the GLM harness).
func writeVoiceProbeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

// parseProbeTable extracts the TSV rows between the table sentinels.
func parseProbeTable(t *testing.T, out string) []string {
	t.Helper()
	var rows []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, voiceProbeTableSentinelEnd):
			in = false
		case strings.Contains(line, voiceProbeTableSentinelBegin):
			in = true
		case in:
			if line != "" && !strings.HasPrefix(line, "#") {
				rows = append(rows, line)
			}
		}
	}
	return rows
}

// tableHasOutcome reports whether any row carries the given outcome (column 9).
func tableHasOutcome(rows []string, outcome string) bool {
	for _, row := range rows {
		f := strings.Split(row, "\t")
		if len(f) >= 9 && strings.TrimSpace(f[8]) == outcome {
			return true
		}
	}
	return false
}
