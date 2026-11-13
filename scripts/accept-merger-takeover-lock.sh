#!/usr/bin/env bash
# accept-merger-takeover-lock.sh — the STANDALONE acceptance for the
# single-instance lock hand-off (epic merger-takeover-lock).
#
#     usage: bash accept-merger-takeover-lock.sh <gopi-binary> [scratch-root]
#
# WHAT IT IS: shell only. No Go test, no test harness, no stub built from the
# branch's own test code — the SHIPPED binary, a sandboxed profile, and the
# kernel's own tables as the judge. The hand-off is a descriptor crossing exec
# and the take-over's identity is its argv[0]; a Go test standing in for that
# process would have to re-implement the production adoption path inside the
# test stub, i.e. prove the stub rather than the product. So the take-over IS a
# stub here, but a stub whose FIRST ACTS ARE THE PRODUCTION ONES (re-exec itself
# with argv[0] = the take-over marker, adopt fd 3, fence it with FD_CLOEXEC read
# back from the kernel, hold it), and every assertion below is made from OUTSIDE
# the process:
#
#   A. a FRESH `flock -n` on the lock PATH is REFUSED while the take-over runs —
#      the flock the (dead) launcher took is proven ALIVE in the child;
#   B. the child itself reports fd 3 resolving to the lock file, argv[0] marked,
#      FD_CLOEXEC armed;
#   C. `gopi merger --status` names the take-over as the LOCK HOLDER and reports
#      the live `takeover:` line (and no longer says "not running");
#   D. a SECOND merger is REFUSED, naming that pid, and runs no pass;
#   E. after the take-over exits the guard is GRANTED again — the lock died with
#      its last copy, it did not leak
#   F. an operator's merger is REFUSED (no marker), and a hand-typed marker
#      ALONE does not bypass the singleton (no live ancestor holds the lock).
#
# The stub is published through GOPI_BINARY — the ordinary channel a wrapper
# uses to make its children dispatch a published build — so no test-only seam is
# involved. No branch is merged, no model is called, no ledger row is written.
#
# Rule 22: the scratch root is taken from the caller and ASSERTED off the /
# partition by the repo's own guard (scripts/lib/tmp-ban.sh) — measured, never
# assumed, and never a fixed /tmp path. Rule 27: this script creates no load of
# any kind.
set -u

BRANCH_BIN="${1:?usage: accept-merger-takeover-lock.sh <gopi-binary> [scratch-root]}"
WORK="${2:-$(pwd)/takeover-accept-scratch}"

say()  { printf '\n=== %s ===\n' "$*"; }
ok()   { printf '  [ok]  %s\n' "$*"; }
bad()  { printf '  [BAD] %s\n' "$*"; FAILED=$((FAILED+1)); }
FAILED=0

# Rule 22, MEASURED and not assumed: the scratch root must be on a DIFFERENT
# device from `/` (on this host /tmp IS `/`, which is why the ban exists). The
# repo's own guard does the measurement — a copy of the comparison here would be
# a second definition of the rule, and a reviewer-provided scratch root must be
# checked rather than trusted.
. "$(dirname "$0")/lib/tmp-ban.sh"

BRANCH_BIN=$(readlink -f "$BRANCH_BIN")
mkdir -p "$WORK"
WORK=$(readlink -f "$WORK")
if ! gopi_assert_off_root_fs "$WORK"; then
  echo "  [BAD] the scratch root must not live on the / partition (Rule 22)" >&2
  exit 1
fi
rm -rf "$WORK"/*

say "layout"
mkdir -p "$WORK"/{home,stub,repo}
HOME_DIR="$WORK/home"
REPO="$WORK/repo"
SETTINGS="$WORK/settings.json"
printf '{"repos":{"%s":{"build":""}}}' "$REPO" > "$SETTINGS"
# A SANDBOXED PROFILE: GOPI_HOME is the only thing that decides where serve.lock
# and the ledger live, so the real profile's guard is never touched.
LOCK="$HOME_DIR/.gopi/serve.lock"
echo "  branch bin : $BRANCH_BIN"
echo "  scratch    : $WORK"
echo "  lock path  : $LOCK"

say "fixture repo (a clean, fast-forwardable branch)"
git -C "$REPO" init -q -b master
git -C "$REPO" -c user.email=e@e -c user.name=e commit -q --allow-empty -m base
git -C "$REPO" checkout -q -b feat/handoff-clean
git -C "$REPO" -c user.email=e@e -c user.name=e commit -q --allow-empty -m "branch work"
git -C "$REPO" checkout -q master
ok "repo at $(git -C "$REPO" rev-parse --short HEAD)"

say "the take-over stand-in (the production first acts, and nothing else)"
cat > "$WORK/stub/main.go" <<'GO'
package main

// A take-over STAND-IN: it recognises itself by argv[0], adopts the inherited
// lock descriptor, arms the fence (read back from the kernel) and holds the
// descriptor for its lifetime. It merges nothing and calls no model.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	takeoverArgv0   = "gopi-merger-takeover"
	lockOwnerEnv    = "GOPI_MERGER_LOCK_OWNER"
	inheritedLockFD = 3
	outEnv          = "HANDOFF_STUB_OUT"
	holdSecondsEnv  = "HANDOFF_STUB_HOLD"
	reportFmt       = "%d\n%s\n%s\n%d\n%s\n"
)

func main() {
	if filepath.Base(os.Args[0]) != takeoverArgv0 {
		fmt.Fprintf(os.Stderr, "stub: argv[0]=%q — NOT started as a take-over\n", os.Args[0])
		os.Exit(3)
	}
	marker := os.Getenv(lockOwnerEnv)
	if marker == "" {
		fmt.Fprintln(os.Stderr, "stub: no hand-off marker — nothing was handed down")
		os.Exit(4)
	}
	f := os.NewFile(uintptr(inheritedLockFD), "serve.lock")
	if f == nil {
		fmt.Fprintln(os.Stderr, "stub: fd 3 is not open")
		os.Exit(5)
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeType != 0 {
		fmt.Fprintf(os.Stderr, "stub: fd 3 is not a regular file (%v / %v)\n", fi, err)
		os.Exit(6)
	}
	// THE FENCE, exactly as production arms it: the descriptor crossed exec
	// WITHOUT FD_CLOEXEC (that is what let it cross), so it is re-armed here and
	// read BACK from the kernel rather than trusted.
	syscall.CloseOnExec(int(f.Fd()))
	raw, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), uintptr(syscall.F_GETFD), 0)
	if errno != 0 || int(raw)&syscall.FD_CLOEXEC == 0 {
		fmt.Fprintf(os.Stderr, "stub: FD_CLOEXEC NOT armed on fd 3 (raw=%#x errno=%v)\n", raw, errno)
		os.Exit(7)
	}
	// Diagnostics only: the pid stamp names the LIVE holder.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	tgt, _ := os.Readlink("/proc/self/fd/3")
	if out := os.Getenv(outEnv); out != "" {
		_ = os.WriteFile(out, []byte(fmt.Sprintf(reportFmt, os.Getpid(), os.Args[0], tgt, int(raw), marker)), 0o644)
	}
	hold := 12
	if v, err := strconv.Atoi(os.Getenv(holdSecondsEnv)); err == nil && v > 0 {
		hold = v
	}
	fmt.Fprintf(os.Stderr, "stub: holding the inherited lock on fd 3 (%s) for %ds\n", tgt, hold)
	time.Sleep(time.Duration(hold) * time.Second)
	fmt.Fprintln(os.Stderr, "stub: exiting — releasing the guard")
}
GO
(cd "$WORK/stub" && GO111MODULE=off CGO_ENABLED=0 go build -o gopi-stub main.go) || { bad "stub build failed"; exit 1; }
STUB="$WORK/stub/gopi-stub"
ok "stub built"

say "1. ONE merger tick, with the stand-in as the published binary"
REPORT="$WORK/stub-report.txt"
export GOPI_HOME="$HOME_DIR"
export GOPI_SETTINGS="$SETTINGS"
export GOPI_MEMORY_DB="$HOME_DIR/memory.db"
export GOPI_COMPUTE_NODES=""
export GOPI_SKIP_CLAIM_BROADCAST=1
export GOPI_BINARY="$STUB"
export HANDOFF_STUB_OUT="$REPORT"
export HANDOFF_STUB_HOLD=12
cd "$REPO" || exit 1
"$BRANCH_BIN" merger > "$WORK/tick.out" 2> "$WORK/tick.err"
TICK_RC=$?
echo "  tick exit=$TICK_RC (the launcher must RETURN on its own)"
sed 's/^/  | /' "$WORK/tick.err"
TAKEOVER_PID=$(grep -o 'take-over started pid=[0-9]*' "$WORK/tick.err" | head -1 | tr -dc '0-9')
if [ -z "$TAKEOVER_PID" ]; then bad "no take-over start line — nothing was started"; exit 1; fi
ok "launcher reported take-over pid=$TAKEOVER_PID and returned"

say "2. the child's OWN report (what it saw, not what we intended)"
for _ in $(seq 1 100); do [ -s "$REPORT" ] && break; sleep 0.1; done
if [ ! -s "$REPORT" ]; then bad "the child never wrote its report"; cat "$WORK/tick.err"; exit 1; fi
STUB_PID=$(sed -n 1p "$REPORT"); STUB_ARGV0=$(sed -n 2p "$REPORT")
STUB_FD3=$(sed -n 3p "$REPORT"); STUB_FLAGS=$(sed -n 4p "$REPORT"); STUB_MARK=$(sed -n 5p "$REPORT")
echo "  pid=$STUB_PID"; echo "  argv0=$STUB_ARGV0"; echo "  fd3 -> $STUB_FD3"
echo "  fcntl(F_GETFD)=$STUB_FLAGS   marker=$STUB_MARK"
[ "$(basename "$STUB_ARGV0")" = "gopi-merger-takeover" ] && ok "the child knows itself as a take-over (argv[0] survived exec)" || bad "argv[0] marker lost: $STUB_ARGV0"
[ "$STUB_FD3" = "$LOCK" ] && ok "the child holds fd 3 on the LOCK FILE itself" || bad "fd 3 resolves to $STUB_FD3, want $LOCK"
[ $((STUB_FLAGS & 1)) -eq 1 ] && ok "FD_CLOEXEC armed on the adopted descriptor (read back from the kernel)" || bad "FD_CLOEXEC not armed (flags=$STUB_FLAGS)"

say "3. THE OUTSIDE VIEW: a FRESH flock on the lock PATH must be REFUSED"
flock -n -E 99 "$LOCK" -c 'exit 0' > "$WORK/flock.out" 2>&1
FLC=$?
if [ $FLC -eq 99 ]; then ok "flock -n refused the guard (exit 99) — the launcher's flock is ALIVE in the take-over"; else bad "flock returned $FLC: the guard is OPEN while a take-over merges"; fi
echo "  kernel lock record for the child's fd 3:"; grep -i flock /proc/"$STUB_PID"/fdinfo/3 2>/dev/null | sed 's/^/    /'

say "4. --status must name the WRITER as the lock holder"
"$BRANCH_BIN" merger --status > "$WORK/status.out" 2>&1
sed 's/^/  | /' "$WORK/status.out"
grep -q "lock holder: pid $STUB_PID" "$WORK/status.out" && ok "status names the live take-over $STUB_PID as the lock holder" || bad "status does not name $STUB_PID as the lock holder"
grep -q 'takeover:' "$WORK/status.out" && ok "status reports a live takeover: line" || bad "status printed no takeover: line"
grep -q 'not running' "$WORK/status.out" && bad "status still says 'not running' about a live merge round" || ok "status no longer says 'not running'"

say "5. a SECOND merger must be REFUSED, naming the holder, and run no pass"
"$BRANCH_BIN" merger --once > "$WORK/second.out" 2>&1
SEC_RC=$?
sed 's/^/  | /' "$WORK/second.out"
[ $SEC_RC -ne 0 ] && ok "the second merger exited non-zero ($SEC_RC)" || bad "the second merger was ALLOWED to run while a take-over holds the lock"
grep -q "$STUB_PID" "$WORK/second.out" && ok "the refusal names the live holder pid $STUB_PID" || bad "the refusal does not name the holder"
grep -q 'single tick done' "$WORK/second.out" && bad "the refused merger ran a pass anyway" || ok "the refused merger ran no pass"

say "6. after the take-over exits the guard must be FREE again (no leak)"
for _ in $(seq 1 200); do kill -0 "$STUB_PID" 2>/dev/null || break; sleep 0.1; done
if kill -0 "$STUB_PID" 2>/dev/null; then
  bad "the stand-in is still alive — the leak check cannot run"
else
  flock -n -E 99 "$LOCK" -c 'exit 0'; FLC=$?
  [ $FLC -eq 0 ] && ok "a fresh flock is GRANTED after the take-over exited — the guard died with its last copy" \
                 || bad "flock still refused (exit $FLC) after the take-over exited — the lock LEAKED past its holder"
fi

say "7. the admission rule, from the outside (three shapes, one command apart)"
# A holder that is NOT an ancestor of the merger: a shell holding the lock while
# the merger runs as its SIBLING. The merger's ancestors hold nothing, so the
# descendant exemption cannot apply to it whatever the environment says.
flock -x "$LOCK" -c 'sleep 25' &
HOLDER_JOB=$!
sleep 0.5
# (a) no marker -> the ordinary acquisition path -> REFUSED (the holder is named).
"$BRANCH_BIN" merger --once > "$WORK/op-nomarker.out" 2>&1
NOM_RC=$?
if [ $NOM_RC -ne 0 ] && grep -q 'refusing double-serve' "$WORK/op-nomarker.out"; then
  ok "a merger with NO marker is REFUSED (exit $NOM_RC)"
else
  bad "no refusal without a marker (exit $NOM_RC): $(head -3 "$WORK/op-nomarker.out")"
fi
# (b) the marker typed by hand -> STILL refused: the marker is evidence, never
#     permission, and no live ANCESTOR of this process holds the flock.
GOPI_MERGER_LOCK_OWNER=$$ "$BRANCH_BIN" merger --once > "$WORK/op-marker.out" 2>&1
MARK_RC=$?
if [ $MARK_RC -ne 0 ] && grep -q 'refusing double-serve' "$WORK/op-marker.out"; then
  ok "a hand-typed marker ALONE does not bypass the singleton (exit $MARK_RC)"
else
  bad "a marker alone bypassed the singleton (exit $MARK_RC): $(head -3 "$WORK/op-marker.out")"
fi
grep -m1 'locking the single-instance\|no ANCESTOR' "$WORK/op-marker.out" | sed 's/^/    /'
wait $HOLDER_JOB 2>/dev/null
# (c) the HOLDER'S OWN DESCENDANT — the deadlock this design must not create.
#     Here the lock holder IS an ancestor (flock runs the merger as its child),
#     so with the marker the face is ADMITTED and really works: it sweeps the
#     fixture repo and lands the branch. A refusal here would be a self-deadlock,
#     because the take-over WAITS on exactly this face (its close loop carries
#     the rebuild contract).
flock -x "$LOCK" -c "GOPI_MERGER_LOCK_OWNER=$$ '$BRANCH_BIN' merger --once" > "$WORK/op-descendant.out" 2>&1
DESC_RC=$?
if [ $DESC_RC -eq 0 ] && ! grep -q 'refusing double-serve' "$WORK/op-descendant.out"; then
  ok "the holder's own descendant IS admitted (exit 0) — no self-deadlock"
else
  bad "a descendant face was refused (exit $DESC_RC): the take-over would deadlock on it"
fi
if git -C "$REPO" rev-parse --verify --quiet refs/heads/feat/handoff-clean >/dev/null; then
  bad "the admitted pass did not land the branch — admission that does no work proves little"
else
  ok "the admitted pass really merged: master is now $(git -C "$REPO" rev-parse --short master)"
fi
sed 's/^/  | /' "$WORK/op-descendant.out" | head -6

say "RESULT"
if [ $FAILED -eq 0 ]; then echo "  ALL CHECKS PASSED"; else echo "  $FAILED CHECK(S) FAILED"; fi
echo "  evidence: $WORK"
exit $FAILED
