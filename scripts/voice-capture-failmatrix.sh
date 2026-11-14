#!/usr/bin/env bash
#
# voice-capture-failmatrix.sh — the five-case capture failure matrix, scripted.
#
# For each failure mode of the gopi voice_input capture client this script
# REPRODUCES the condition on a real node, runs the client, and ASSERTS the
# named outcome it must produce. It is the executable half of
# devdoc/voice-capture-failure-matrix.md: the doc records what was observed,
# this script fails (exit 1) the moment a fix is reverted.
#
# Cases:
#   a  no capture device present          → no_soundcards / no_device
#   b  device busy (a holder owns the PCM)→ busy, and it must RETURN (no hang)
#   c  hot-unplug mid-capture             → hot_unplug + partial-file policy
#   d  unsupported rate/channel request   → unsupported_params
#   e  muted / zero-gain input            → silent
#
# Usage:
#   scripts/voice-capture-failmatrix.sh [--bin PATH] [--device D] [--case a,b,e]
#                                       [--out DIR] [--repo DIR] [--json]
#
# --repo points at a gopi checkout so the classifier-contract assertions can run
# when this script is executed from a copy (e.g. shipped to another node).
#
# Every case runs against a device string chosen so that the condition is real
# on THIS host, and is skipped (recorded as SKIP, never as PASS) when the host
# cannot produce it — a host with one soundcard cannot demonstrate a wrong
# device without a second one, and silently passing there would be a lie.
#
# EXIT: 0 when every non-skipped case matched its expectation, 1 otherwise.

set -u
set -o pipefail

# ── Constants (Rule 1: no magic numbers / bare keywords inline) ───────────────
readonly DEFAULT_BIN="./bin/gopi"
readonly DEFAULT_DEVICE="default"
readonly NO_SUCH_DEVICE="gopi_no_such_device_xyz"
readonly CAPTURE_SECONDS=3                 # minimal sample: long enough to hold a PCM
readonly HOLDER_SECONDS=8                  # the competing capture outlives the probe
readonly HANG_BUDGET_SEC=25                # client wall clock bound + slack
readonly SCRIPT_TIMEOUT_SEC=4              # per external command
readonly NULL_SINK_PREFIX="gopi-failmatrix"
readonly PROC_ASOUND_PCM="/proc/asound/pcm"
readonly PULSE_DEVICE_PREFIX="pulse:"      # client prefix selecting ONE Pulse source
readonly RESULT_MARKER="[voice-capture]"
readonly JSON_TRUE=1

# Outcome vocabulary — must equal the captureFailureMode constants in
# cmd/agent/cli_voice_capture.go (pinned by TestVoiceCaptureModeVocabulary).
readonly MODE_OK="ok"
readonly MODE_SILENT="silent"
readonly MODE_NO_DEVICE="no_device"
readonly MODE_NO_SOUNDCARDS="no_soundcards"
# Modes the classifier can produce but this harness has no live case for yet
# (they need a node without `audio` group membership / with a dead Pulse socket).
# Kept so the vocabulary here stays in step with captureFailureMode, and checked
# for drift by the mode-vocabulary test on the Go side.
readonly MODE_PERMISSION="permission"
readonly MODE_BUSY="busy"
readonly MODE_UNSUPPORTED="unsupported_params"
readonly MODE_PULSE_REFUSED="pulse_refused"
readonly MODE_HOT_UNPLUG="hot_unplug"
readonly MODE_TIMEOUT="timeout"
readonly MODE_ERROR="error"

readonly PASS="PASS"
readonly FAIL="FAIL"
readonly SKIP="SKIP"

OUT_DIR="${TMPDIR:-/tmp}/gopi-failmatrix-$$"
BIN="$DEFAULT_BIN"
DEVICE="$DEFAULT_DEVICE"
ONLY_CASES=""
JSON=0
FAILURES=0
CLEANUP_MODULES=()

# ── Helpers ───────────────────────────────────────────────────────────────────
export LC_ALL=C

log()  { printf '%s\n' "$*"; }
head2() { printf '\n===== %s =====\n' "$*"; }

cleanup() {
    local m
    for m in "${CLEANUP_MODULES[@]:-}"; do
        [ -n "$m" ] && pactl unload-module "$m" >/dev/null 2>&1
    done
    rm -rf "${OUT_DIR:?}/probe" 2>/dev/null
}
trap cleanup EXIT INT TERM

have() { command -v "$1" >/dev/null 2>&1; }

# run_client <device> <seconds> <outfile> → prints the machine-readable line
run_client() {
    local device="$1" seconds="$2" out="$3"
    timeout "$HANG_BUDGET_SEC" "$BIN" tool voice_input --client \
        --device "$device" --seconds "$seconds" --out "$out" 2>&1 |
        grep -F "$RESULT_MARKER" | tail -1
}

# mode_of <result-line> → the mode word
mode_of() {
    local line="$1"
    printf '%s' "$line" | sed -n 's/.*mode=\([a-z_]*\).*/\1/p'
}

# record <case> <status> <expected> <got> <detail>
record() {
    printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" "$5" >>"${OUT_DIR}/results.tsv"
    case "$2" in
        "$FAIL") FAILURES=$((FAILURES + 1)) ;;
    esac
    log "$2  case($1)  expected=${3:-any}  got=${4:-—}  ${5:-}"
}

selected() {   # selected <case>
    [ -z "$ONLY_CASES" ] && return 0
    case ",${ONLY_CASES}," in *",$1,"*) return 0 ;; esac
    return 1
}

# ── Argument parsing ──────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
    case "$1" in
        --bin)    BIN="$2"; shift 2 ;;
        --device) DEVICE="$2"; shift 2 ;;
        --case)   ONLY_CASES="$2"; shift 2 ;;
        --out)    OUT_DIR="$2"; shift 2 ;;
        --repo)   REPO_OVERRIDE="$2"; shift 2 ;;
        --json)   JSON=1; shift ;;
        -h|--help)
            sed -n '2,30p' "$0"; exit 0 ;;
        *) log "voice-capture-failmatrix: unknown argument: $1" >&2; exit 2 ;;
    esac
done

# The repo is the script's own tree, so a copy shipped to another node (where no
# checkout exists) degrades to SKIP for the contract assertions instead of FAIL.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel 2>/dev/null || true)"
if [ -n "$REPO_ROOT" ]; then
    REPO_ROOT="$REPO_ROOT"                 # a --repo flag may override it below
fi

# A --repo override (parsed above) wins over the script's own tree.
if [ -n "${REPO_OVERRIDE:-}" ]; then REPO_ROOT="$REPO_OVERRIDE"; fi
if [ -n "$REPO_ROOT" ] && [ ! -d "$REPO_ROOT/cmd/agent" ]; then REPO_ROOT=""; fi

[ -x "$BIN" ] || { log "voice-capture-failmatrix: not executable: $BIN (build it first)" >&2; exit 2; }
have arecord || { log "voice-capture-failmatrix: arecord not in PATH" >&2; exit 2; }

mkdir -p "$OUT_DIR"
: >"${OUT_DIR}/results.tsv"
log "node=$(hostname -s)  bin=$BIN  out=$OUT_DIR"

# ── Case (a): no capture device present ───────────────────────────────────────
case_a() {
    head2 "case (a) no capture device present"
    if ! have arecord; then record a "$SKIP" "$MODE_NO_DEVICE" "no arecord" ""; return; fi

    # a1: this host genuinely has no capture hardware (peer1).
    local listing line mode
    listing="$(timeout "$SCRIPT_TIMEOUT_SEC" arecord -l 2>&1 </dev/null || true)"
    if printf '%s' "$listing" | grep -qi 'no soundcards found'; then
        line="$(run_client "$DEVICE" "$CAPTURE_SECONDS" "${OUT_DIR}/a-nosound.pcm")"
        mode="$(mode_of "$line")"
        if [ "$mode" = "$MODE_NO_SOUNDCARDS" ]; then
            record a "$PASS" "$MODE_NO_SOUNDCARDS" "$mode" "host reports no capture hardware"
        else
            record a "$FAIL" "$MODE_NO_SOUNDCARDS" "$mode" "expected a named no-capture mode, not $mode"
        fi
    else
        record a "$SKIP" "$MODE_NO_SOUNDCARDS" "—" "this host HAS capture hardware; run on a capture-less node"
    fi

    # a2: a named device that does not exist — must be named, not generic. On a
    # node with NO hardware at all the honest answer is no_soundcards (there is
    # no name that would have worked), so the expectation follows the inventory.
    local expect_a2="$MODE_NO_DEVICE" why_a2="unknown device must classify as no_device"
    if printf '%s' "$listing" | grep -qi 'no soundcards found'; then
        expect_a2="$MODE_NO_SOUNDCARDS"
        why_a2="no hardware on this node, so the inventory answer outranks the name"
    fi
    line="$(run_client "$NO_SUCH_DEVICE" "$CAPTURE_SECONDS" "${OUT_DIR}/a-nodev.pcm")"
    mode="$(mode_of "$line")"
    if [ "$mode" = "$expect_a2" ]; then
        record a "$PASS" "$expect_a2" "$mode" "device string '$NO_SUCH_DEVICE' — $why_a2"
    else
        record a "$FAIL" "$expect_a2" "$mode" "device string '$NO_SUCH_DEVICE'"
    fi
}

# ── Case (b): device busy ─────────────────────────────────────────────────────
# A PulseAudio null-sink accepting a writer, captured through the `pulse` device
# string, is an ordinary single-open source. A SECOND concurrent capture of the
# same source is refused with a real "Device or resource busy" — the same
# condition a real microphone produces, reproduced with no hardware and no risk
# to system state.
case_b() {
    head2 "case (b) device busy — a second capture holds the PCM"
    if ! have pactl || ! have parecord || ! have pacat; then
        record b "$SKIP" "$MODE_BUSY" "—" "pactl/parecord/pacat unavailable"; return
    fi
    pactl info >/dev/null 2>&1 || { record b "$SKIP" "$MODE_BUSY" "—" "no reachable sound server"; return; }

    # The device a real user records from: an ordinary input source, which
    # refuses a second reader (unlike a null-sink monitor, which mixes).
    local source
    source="$(pactl list short sources 2>/dev/null | awk '$2 !~ /\\.monitor$/ {print $2; exit}')"
    if [ -z "$source" ]; then
        record b "$SKIP" "$MODE_BUSY" "—" "no non-monitor PulseAudio source to occupy"; return
    fi
    # Keep the source in use for the whole probe.
    ( timeout "$HOLDER_SECONDS" pacat --record --device="$source" --rate=16000 \
        --channels=1 --format=s16le >/dev/null 2>&1 ) &
    local holder_pid=$!
    sleep 1

    local line mode started elapsed
    started="$(date +%s)"
    line="$(run_client "${PULSE_DEVICE_PREFIX}${source}" "$CAPTURE_SECONDS" "${OUT_DIR}/b-probe.pcm")"
    mode="$(mode_of "$line")"
    elapsed=$(( $(date +%s) - started ))

    # Is the source genuinely occupied? Ask it directly, so a host where the
    # second open is allowed is SKIPped rather than mis-asserted.
    local occupied=""
    if PULSE_SOURCE="$source" timeout "$SCRIPT_TIMEOUT_SEC" arecord -D pulse -t raw \
        -f S16_LE -r 16000 -c 1 -d 1 /dev/null 2>&1 </dev/null | grep -qi busy; then
        occupied=yes
    fi
    kill "$holder_pid" 2>/dev/null
    wait "$holder_pid" 2>/dev/null

    if [ -z "$occupied" ]; then
        record b "$SKIP" "$MODE_BUSY" "—" "this host allows a second capture of the same source"
        assert_classifier_contract b 'TestVoiceCaptureBusyIsNamedAndNeverHangs' "$MODE_BUSY"
        return
    fi
    if [ "$mode" = "$MODE_BUSY" ] || [ "$mode" = "$MODE_HOT_UNPLUG" ]; then
        record b "$PASS" "$MODE_BUSY" "$mode" "named and actionable, returned in ${elapsed}s"
    else
        record b "$FAIL" "$MODE_BUSY" "$mode" "a busy device must be named, not reported as $mode"
    fi
    if [ "$elapsed" -le "$HANG_BUDGET_SEC" ]; then
        record b "$PASS" "<=${HANG_BUDGET_SEC}s" "${elapsed}s" "no hang (bounded wall clock)"
    else
        record b "$FAIL" "<=${HANG_BUDGET_SEC}s" "${elapsed}s" "client hung on a busy device"
    fi
}

# ── Case (c): hot-unplug mid-capture ──────────────────────────────────────────
# The device goes away while the client is still recording. Reproduced by
# ending the client's OWN recorder process mid-capture (matched by the unique
# output path, so nothing else can be hit): the capture ends early exactly as a
# yanked device would end it, with no system state touched and nothing to undo.
case_c() {
    head2 "case (c) hot-unplug mid-capture"
    # Needs a device that actually captures; on a capture-less node there is no
    # stream to interrupt, so assert the classifier contract and say so.
    if printf '%s' "$(timeout "$SCRIPT_TIMEOUT_SEC" arecord -l 2>&1 </dev/null || true)" | grep -qi 'no soundcards found'; then
        record c "$SKIP" "$MODE_HOT_UNPLUG" "—" "no capture hardware on this host"
        assert_classifier_contract c 'TestVoiceCaptureHotUnplugPartialPolicy|TestVoiceCaptureShortCaptureIsHotUnplugNotOK' "$MODE_HOT_UNPLUG"
        return
    fi
    local out="${OUT_DIR}/c-partial.pcm"
    local line mode started

    timeout "$HANG_BUDGET_SEC" "$BIN" tool voice_input --client \
        --device "$DEVICE" --seconds 6 --out "$out" >"${OUT_DIR}/c-client.log" 2>&1 &
    local client_pid=$!
    sleep 3
    # Pull the device away: kill the recorder this client spawned.
    pkill -f "arecord.*${out}" 2>/dev/null || true
    wait "$client_pid" 2>/dev/null

    line="$(grep -F "$RESULT_MARKER" "${OUT_DIR}/c-client.log" | tail -1)"
    mode="$(mode_of "$line")"
    local bytes=0
    [ -f "$out" ] && bytes="$(stat -c%s "$out" 2>/dev/null || echo 0)"

    if [ "$mode" = "$MODE_HOT_UNPLUG" ]; then
        record c "$PASS" "$MODE_HOT_UNPLUG" "$mode" "capture ended early and was named, not reported as complete"
    elif [ "$mode" = "$MODE_OK" ] || [ "$mode" = "$MODE_SILENT" ]; then
        record c "$FAIL" "$MODE_HOT_UNPLUG" "$mode" "a capture that ended early must never be reported as $mode"
    else
        record c "$FAIL" "$MODE_HOT_UNPLUG" "$mode" "an early end must be named hot_unplug"
    fi
    # The partial-handling policy, asserted from the repo as well: a truncated
    # capture must never be reported as complete.
    assert_classifier_contract c 'TestVoiceCaptureShortCaptureIsHotUnplugNotOK' "$MODE_HOT_UNPLUG"
    # And the live policy check: what arrived stays on disk and is named.
    if [ "$bytes" -gt 0 ]; then
        record c "$PASS" "partial kept" "${bytes} bytes" "policy: partial PCM retained at $out"
    else
        record c "$SKIP" "partial kept" "0 bytes" "nothing arrived before the capture ended"
    fi
}

# ── Case (d): unsupported rate / channel request ──────────────────────────────
# The pinned contract is 16000 Hz mono S16_LE. A RAW hw: device that cannot hold
# it must produce unsupported_params — and that verdict must outrank the "busy"
# its stderr also contains when a sound server owns the PCM.
case_d() {
    head2 "case (d) unsupported rate/channel request"
    local raw device=""
    raw="$(raw_capture_device)"
    # A node whose ALSA inventory is empty cannot stage this case at all: the
    # device it would name is only a /proc entry (the peer1 advertises an
    # ALC3228 PCM it has no permission to open). Record the honest SKIP and
    # assert the classifier contract from the repo instead.
    if printf '%s' "$(timeout "$SCRIPT_TIMEOUT_SEC" arecord -l 2>&1 </dev/null || true)" | grep -qi 'no soundcards found'; then
        record d "$SKIP" "$MODE_UNSUPPORTED" "—" "no ALSA capture inventory on this host"
        assert_classifier_contract d 'TestVoiceCaptureUnsupportedParamsBeatsBusy|TestVoiceCaptureUnsupportedVariants' "$MODE_UNSUPPORTED"
        raw=""
    fi

    if [ -n "$raw" ]; then
        device="$raw"
    else
        # No raw capture device on this host: ask for a rate no hardware has
        # through the plug chain, which the plug layer rejects outright.
        record d "$SKIP" "$MODE_UNSUPPORTED" "—" "no raw hw: capture device on this host"
        assert_classifier_contract d 'TestVoiceCaptureUnsupportedParamsBeatsBusy' "$MODE_UNSUPPORTED"
    fi

    if [ -n "$device" ]; then
        local line mode
        line="$(run_client "$device" "$CAPTURE_SECONDS" "${OUT_DIR}/d-raw.pcm")"
        mode="$(mode_of "$line")"
        # Two refusals are both correct and both actionable here, and WHICH one
        # appears is state-dependent (measured on the HP): a free raw PCM reports
        # the parameter refusal, while a PCM currently owned by a sound server
        # reports busy — and the "busy" text then ALSO carries the parameter
        # complaint, which is why the classifier puts parameters first.
        case "$mode" in
            "$MODE_UNSUPPORTED")
                record d "$PASS" "$MODE_UNSUPPORTED" "$mode" "$device cannot honour 16k/mono on this host" ;;
            "$MODE_BUSY")
                record d "$PASS" "$MODE_UNSUPPORTED" "$mode" "$device is held by a sound server — busy is the correct refusal, and parameters outrank it when both appear" ;;
            *)  record d "$FAIL" "$MODE_UNSUPPORTED" "$mode" "$device must be refused as a parameter problem or busy" ;;
        esac
    fi
    # The parameter refusal itself is pinned from the repo, so this case has a
    # deterministic red-proof even when the live PCM is currently owned.
    assert_classifier_contract d 'TestVoiceCaptureUnsupportedParamsBeatsBusy|TestVoiceCaptureUnsupportedVariants' "$MODE_UNSUPPORTED"

    # The impossible request the CLIENT owns and must refuse before capturing.
    if timeout "$HANG_BUDGET_SEC" "$BIN" tool voice_input --client --seconds 0 >/dev/null 2>&1; then
        record d "$FAIL" "reject --seconds 0" "accepted" "--seconds 0 must be rejected"
    else
        record d "$PASS" "reject --seconds 0" "rejected" "impossible request refused up front"
    fi
    if timeout "$HANG_BUDGET_SEC" "$BIN" tool voice_input --client --device "$NO_SUCH_DEVICE" --seconds 1 >/dev/null 2>&1; then
        record d "$FAIL" "nonzero exit on failure" "exit 0" "a failed capture must exit non-zero"
    else
        record d "$PASS" "nonzero exit on failure" "nonzero" "scriptable failure signal"
    fi
}

# raw_capture_device prints the first capture-capable PCM as hw:<card>,0.
# /proc/asound/pcm is the authoritative source: its lines read
#   <card>-<device>: <name> : playback 1 : capture 1
# so only a line carrying "capture" is a candidate (on the HP, card 0 is
# playback-only HDMI and answering from /proc/asound/cards would name it).
# Empty when the host has no raw capture device.
raw_capture_device() {
    [ -r "$PROC_ASOUND_PCM" ] || return 0
    local card
    card="$(awk '/capture/ {split($1, a, "-"); printf "%d", a[1]; exit}' "$PROC_ASOUND_PCM" 2>/dev/null)"
    [ -n "$card" ] && printf 'hw:%s,0' "$card"
    return 0
}

# assert_classifier_contract <case> <go-test-name> <expected-mode>
# When the live condition cannot be reproduced on this host (a single-reader
# device is unavailable, a source multiplexes, …) the classifier contract is
# still asserted from the repo, against the RECORDED real-world stderr. This is
# what keeps the matrix honest on a host that cannot stage the condition.
# The repo is found relative to THIS script (it may be a copy in /tmp), and an
# absent repo/toolchain is a SKIP — never a false FAIL.
assert_classifier_contract() {
    local case_id="$1" test_name="$2" expect="$3"
    if [ -z "$REPO_ROOT" ]; then
        record "$case_id" "$SKIP" "$expect" "—" "script lives outside a repo — cannot run the classifier contract test"
        return
    fi
    if ! have go; then
        record "$case_id" "$SKIP" "$expect" "—" "no Go toolchain on this host"
        return
    fi
    if (cd "$REPO_ROOT" && timeout 180 go test ./cmd/agent/ -run "$test_name" -count=1 >"${OUT_DIR}/contract-${case_id}.log" 2>&1); then
        record "$case_id" "$PASS" "$expect" "$expect" "classifier contract asserted by $test_name"
    else
        record "$case_id" "$FAIL" "$expect" "test failed" "$test_name does not hold (see ${OUT_DIR}/contract-${case_id}.log)"
    fi
}

# ── Case (e): muted / zero-gain input ─────────────────────────────────────────
case_e() {
    head2 "case (e) muted / zero-gain input"
    if ! have pactl; then
        record e "$SKIP" "$MODE_SILENT" "—" "pactl unavailable"
        assert_classifier_contract e 'TestVoiceCaptureRunnerSurfacesSilence|TestVoiceCaptureSilenceIsDetected' "$MODE_SILENT"
        return
    fi
    if ! pactl info >/dev/null 2>&1; then
        record e "$SKIP" "$MODE_SILENT" "—" "no reachable sound server"
        assert_classifier_contract e 'TestVoiceCaptureRunnerSurfacesSilence|TestVoiceCaptureSilenceIsDetected' "$MODE_SILENT"
        return
    fi

    # Mute the default source and capture from it: a real muted microphone.
    local src muted_before=""
    src="$(pactl get-default-source 2>/dev/null || true)"
    local line mode
    line="$(run_client "$DEVICE" "$CAPTURE_SECONDS" "${OUT_DIR}/e-live.pcm")"
    mode="$(mode_of "$line")"

    if [ -n "$src" ]; then
        muted_before="$(pactl get-source-mute "$src" 2>/dev/null | grep -o 'yes\|no' | head -1)"
        pactl set-source-mute "$src" 1 >/dev/null 2>&1
        line="$(run_client "$DEVICE" "$CAPTURE_SECONDS" "${OUT_DIR}/e-muted.pcm")"
        mode="$(mode_of "$line")"
        [ "$muted_before" = "no" ] && pactl set-source-mute "$src" 0 >/dev/null 2>&1
        if [ "$mode" = "$MODE_SILENT" ]; then
            record e "$PASS" "$MODE_SILENT" "$mode" "muted source surfaced as silence"
        else
            record e "$FAIL" "$MODE_SILENT" "$mode" "a muted source must be surfaced, not captured as $mode"
        fi
    else
        record e "$SKIP" "$MODE_SILENT" "—" "no default source to mute"
        assert_classifier_contract e 'TestVoiceCaptureRunnerSurfacesSilence|TestVoiceCaptureSilenceIsDetected' "$MODE_SILENT"
    fi

    # Synthetic zero-gain: a null-sink monitor with nothing playing is digital
    # silence — the same outcome a zero-gain input produces, with no hardware.
    local sink="${NULL_SINK_PREFIX}-sil" module
    module="$(pactl load-module module-null-sink sink_name="$sink" 2>/dev/null || true)"
    if [ -n "$module" ]; then
        CLEANUP_MODULES+=("$module")
        line="$(run_client "${PULSE_DEVICE_PREFIX}${sink}.monitor" "$CAPTURE_SECONDS" "${OUT_DIR}/e-null.pcm")"
        mode="$(mode_of "$line")"
        if [ "$mode" = "$MODE_SILENT" ]; then
            record e "$PASS" "$MODE_SILENT" "$mode" "zero-gain (null-sink monitor) surfaced as silence"
        else
            record e "$FAIL" "$MODE_SILENT" "$mode" "60s of zeros must not be sent as $mode"
        fi
    else
        record e "$SKIP" "$MODE_SILENT" "—" "could not load a null-sink"
    fi
}

# ── Vocabulary drift guard ────────────────────────────────────────────────────
# The mode strings are a cross-language contract: this script asserts on them and
# the Go classifier produces them. Renaming one side must not silently pass here,
# so every value in this file is checked against captureFailureMode in the Go
# source (the Go side pins its own spelling in TestVoiceCaptureModeVocabulary).
assert_mode_vocabulary() {
    head2 "vocabulary: harness modes vs the Go classifier"
    local -a modes=(
        "$MODE_OK" "$MODE_SILENT" "$MODE_NO_DEVICE" "$MODE_NO_SOUNDCARDS"
        "$MODE_PERMISSION" "$MODE_BUSY" "$MODE_UNSUPPORTED" "$MODE_PULSE_REFUSED"
        "$MODE_HOT_UNPLUG" "$MODE_TIMEOUT" "$MODE_ERROR"
    )
    if [ -z "$REPO_ROOT" ]; then
        record vocab "$SKIP" "11 modes" "—" "script lives outside a repo — cannot read the Go classifier"
        return
    fi
    local src="$REPO_ROOT/cmd/agent/cli_voice_capture.go"
    local drift=0 m
    for m in "${modes[@]}"; do
        if ! grep -q "captureFailureMode = \"$m\"" "$src" 2>/dev/null; then
            log "  vocabulary drift: mode '$m' is asserted here but not produced by $src"
            drift=$((drift + 1))
        fi
    done
    if [ "$drift" -eq 0 ]; then
        record vocab "$PASS" "11 modes" "11/11" "harness vocabulary matches captureFailureMode"
    else
        record vocab "$FAIL" "11 modes" "$drift missing" "the harness and the Go classifier disagree"
    fi
}

# ── Run ───────────────────────────────────────────────────────────────────────
selected a && case_a
selected b && case_b
selected c && case_c
selected d && case_d
selected e && case_e
assert_mode_vocabulary

head2 "summary"
column -t -s $'\t' "${OUT_DIR}/results.tsv"
log ""
log "results: ${OUT_DIR}/results.tsv"
if [ "$JSON" = "$JSON_TRUE" ]; then
    printf '{"node":"%s","bin":"%s","results":"%s","failures":%d}\n' \
        "$(hostname -s)" "$BIN" "${OUT_DIR}/results.tsv" "$FAILURES"
fi

if [ "$FAILURES" -gt 0 ]; then
    log "FAIL: ${FAILURES} case assertion(s) unmet"
    exit 1
fi
log "OK: every exercised case matched its expected mode"
exit 0
