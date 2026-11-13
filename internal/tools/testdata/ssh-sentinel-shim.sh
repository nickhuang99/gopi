#!/bin/sh
# ssh-sentinel-shim — a stand-in for ssh, for the fixture-leak regression test.
#
# WHY THIS EXISTS (epic fix/fixture-leak-temp-root): the leaking fixture arm
# declares the peer alias `zbook`, which on the server resolves to 127.0.0.1 +
# ~/work/GoPi — the REAL repository. A test that drives that arm therefore
# attempts a real push whenever the fence is not in force (or is not in force
# yet, on the RED checkout). This shim places a RECEIVER OF THE TEST'S OWN
# CHOOSING at the name the target resolves to, so a RED run can observe the
# leak attempt WITHOUT any possibility of writing the real repo:
#
#   * it logs the ssh argv it was handed (host + receive-pack command) — the
#     evidence that the production path resolved the real node;
#   * it RETARGETS the receive-pack path to the sentinel bare repo and execs
#     `git-receive-pack <sentinel>` in-process, so the pushed bytes land in the
#     sentinel and nowhere else.
#
# The rewrite is the safety property (never touch what the target names); the
# log is the measurement. Env in: GOPI_SSH_SENTINEL_LOG, GOPI_SSH_SENTINEL_REPO.
set -u

log="${GOPI_SSH_SENTINEL_LOG:-}"
sentinel="${GOPI_SSH_SENTINEL_REPO:-}"

# The last argv element is the command git wants run on the remote; the ones
# before it are ssh options (-o …) and the host.
last=""
for a in "$@"; do
	last="$a"
done

if [ -n "$log" ]; then
	printf 'argv=%s\n' "$*" >>"$log"
	printf 'receive=git-receive-pack target=%s\n' "$last" >>"$log"
fi

if [ -z "$sentinel" ]; then
	echo "ssh-sentinel-shim: GOPI_SSH_SENTINEL_REPO unset — refusing to exec" >&2
	exit 127
fi

# Take the quoted path out of the remote command and run the receiver HERE,
# against the sentinel. Never exec the original argv: that is what would reach
# the real repo.
exec git-receive-pack "$sentinel"
