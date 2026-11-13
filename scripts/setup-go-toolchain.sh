#!/usr/bin/env bash
# setup-go-toolchain.sh — make this machine able to build gopi, with NO root.
#
# WHY THIS EXISTS (the measured defect):
#   go.mod declares `go 1.24.0`, and two direct dependencies declare it too
#   (github.com/charmbracelet/bubbletea v1.3.10, modernc.org/sqlite v1.44.0).
#   Ubuntu 22.04 (jammy) ships ONLY the gccgo front-end — /usr/bin/go ->
#   go-11, "go1.16.5 gccgo" — and apt's candidate for golang-go is
#   2:1.18~0ubuntu2. Neither can even PARSE a '1.24.0' directive:
#
#     go: errors parsing go.mod:
#     /home/nick/work/GoPi/go.mod:3: invalid go version '1.24.0': must match format 1.23
#
#   That message is a PARSER limit (%d.%d), not a request to rewrite go.mod:
#   rewriting the directive to `go 1.23` cannot work either, because the
#   dependencies genuinely need 1.24 language/std features. The only fix is a
#   real Go >= 1.24 toolchain.
#
# WHY THE TOOLCHAIN IS INSTALLED UNDER $HOME (and not /usr/local/go):
#   Installing to /usr/local/go needs root, and this project's rule is that a
#   privilege is never the remedy. A Go toolchain is a self-contained directory;
#   it needs no package manager and no root. Same for the PATH fix.
#
# A SECOND, EASIER-TO-MISS HALF: the two shell shapes have different PATHs.
#   Ubuntu's default .profile PREPENDS $HOME/.local/bin (login shells get it
#   first), while the default non-login PATH ends with it (line 118 of the
#   stock .bashrc rebuilds PATH from scratch). gopi's own post-merge rebuild
#   runs the declared build command via `sh -c` and inherits the CALLER's
#   environment, so a gopi started from a non-login shell would still compile
#   with gccgo. This script prepends $HOME/.local/bin in BOTH .profile and
#   .bashrc for that reason. A gopi launched by a NON-shell mechanism (cron, a
#   minimal systemd unit) inherits neither — see the note the script prints at
#   the end; that case must carry PATH in its own unit/command, and the fix is
#   configuration, never root.
#
# Idempotent: re-running is a no-op on an already-provisioned machine.
# No root anywhere. To undo: delete $HOME/workspace/toolchains/go1.24.*, the
# $HOME/.local/bin/{go,gofmt} symlinks, and the marked block this script appends
# to ~/.bashrc / ~/.profile (backups are not needed — the block is marked).

set -euo pipefail

# --- named limits (Rule 1: no bare literals for a version/threshold) -------
GO_SERIES="${GO_SERIES:-go1.24}"            # the minimum series go.mod requires
GO_VERSION="${GO_VERSION:-go1.24.13}"       # the patch actually fetched
TOOLCHAIN_ROOT="${TOOLCHAIN_ROOT:-$HOME/workspace/toolchains}"
LOCAL_BIN="$HOME/.local/bin"
# Module proxy. proxy.golang.org is UNREACHABLE from this network (measured:
# connection timeout), while goproxy.cn answers http=200 in ~0.3s. Without this
# the build fails with "dial tcp: i/o timeout" fetching any uncached module.
# (History: devdoc/go-toolchain-env-both-nodes-20260920.md set this per node.)
GOPROXY_VALUE="${GOPROXY_VALUE:-https://goproxy.cn,https://goproxy.io,direct}"
DOWNLOAD_BASE="https://dl.google.com/go"    # go.dev/download redirects here
MARKER="Go >= 1.24 toolchain for the gopi build"   # idempotency guard in the rc files

say() { printf '%s\n' "$*"; }

# --- 0. is a sufficient Go already on PATH? --------------------------------
have_sufficient_go() {
    command -v go >/dev/null 2>&1 || return 1
    # `go env GOVERSION` is the reliable probe; `go version` prints front-end
    # noise on gccgo. A 1.24+ toolchain is the pass condition.
    local v
    v="$(go env GOVERSION 2>/dev/null || true)"
    case "$v" in
        go1.2[4-9]*|go1.[3-9][0-9]*|go[2-9]*) return 0 ;;
        *) return 1 ;;
    esac
}

if have_sufficient_go; then
    say "already have $(go env GOVERSION) at $(command -v go) — nothing to install"
    # Probe further with the toolchain we just vouched for.
    GO_BIN="$(command -v go)"
else
    say "no Go >= ${GO_SERIES} on PATH; installing ${GO_VERSION} under ${TOOLCHAIN_ROOT}"
    mkdir -p "$TOOLCHAIN_ROOT"
    tarball="$TOOLCHAIN_ROOT/${GO_VERSION}.linux-amd64.tar.gz"
    dest="$TOOLCHAIN_ROOT/${GO_VERSION}"

    if [ ! -x "$dest/bin/go" ]; then
        if [ ! -f "$tarball" ]; then
            say "downloading ${tarball}.linux-amd64.tar.gz (~75 MB)"
            curl -fL --retry 4 --retry-delay 2 -C - \
                 -o "$tarball" "${DOWNLOAD_BASE}/${GO_VERSION}.linux-amd64.tar.gz"
        fi
        say "extracting"
        tar -xzf "$tarball" -C "$TOOLCHAIN_ROOT"
        # `tar` yields ".../go"; give it the versioned name so a second version
        # can coexist without overwriting this one.
        rm -rf "$dest"
        mv "$TOOLCHAIN_ROOT/go" "$dest"
    fi

    # Symlinks (not copies) so `go` and the installed toolchain cannot drift.
    # ~/.local/bin is user-owned: no root, and it is already on the default PATH.
    mkdir -p "$LOCAL_BIN"
    ln -sfn "$dest/bin/go"    "$LOCAL_BIN/go"
    ln -sfn "$dest/bin/gofmt" "$LOCAL_BIN/gofmt"
    say "symlinked $LOCAL_BIN/go -> $(readlink -f "$LOCAL_BIN/go")"
    # Probe with the toolchain JUST installed, never `command -v go`: on a fresh
    # machine that still resolves to the too-old one on PATH, and an old Go does
    # not know GOTOOLCHAIN — it exits "unknown go command variable GOTOOLCHAIN",
    # aborting the script half-configured. (Measured on a fake fresh HOME: the
    # symlink was written, then the script died before the PATH fix.)
    GO_BIN="$dest/bin/go"
fi

# --- 1. Go's own environment: proxy + no surprise toolchain switch ---------
# GOPROXY is needed on BOTH branches above (a pre-existing 1.24 toolchain still
# cannot reach proxy.golang.org). GOTOOLCHAIN=local pins the toolchain we just
# installed: without it Go may try to *download* another one, which is exactly
# the network path that is blocked.
"$GO_BIN" env -w GOPROXY="$GOPROXY_VALUE"
"$GO_BIN" env -w GOTOOLCHAIN=local
say "wrote $("$GO_BIN" env GOENV ):"
sed 's/^/    /' "$("$GO_BIN" env GOENV)"

# --- 2. PATH for both shell shapes (login AND non-login) -------------------
ensure_path_line() {
    local rc="$1" line='export PATH="$HOME/.local/bin:$PATH"'
    [ -f "$rc" ] || return 0
    if grep -qF "$MARKER" "$rc"; then
        say "$rc already carries the PATH line — left alone"
        return 0
    fi
    {
        printf '\n# --- %s (added by scripts/setup-go-toolchain.sh) ---\n' "$MARKER"
        printf '%s\n' "$line"
    } >> "$rc"
    say "appended the PATH line to $rc"
}
ensure_path_line "$HOME/.bashrc"
ensure_path_line "$HOME/.profile"

# --- 3. prove it -----------------------------------------------------------
# The three shapes are probed with an ISOLATED environment, because a child
# shell otherwise just inherits the parent's PATH and every probe would agree
# no matter what the rc files say. `bash -c` with an inherited PATH proves
# nothing; `env -i PATH=<stock>` is the honest test of the stock shape.
#   login            → reads ~/.profile   (must now find go1.24)
#   interactive      → reads ~/.bashrc    (must now find go1.24)
#   bare non-login   → reads NEITHER rc   (cannot be fixed without root — see note)
STOCK_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
say ""
say "VERIFY — expecting go1.2x, never 'gccgo 1.16':"
bash -lc 'command -v go; go env GOVERSION' 2>/dev/null \
    | sed 's/^/    login shell:      /' || true
# The interactive shape is NOT probed by spawning `bash -i`: an interactive bash
# ignores SIGTERM, so a probe that blocks cannot be timed out and hangs the
# script. It reads ~/.bashrc, and the thing this script writes there is the very
# line the login probe above just proved — so check the MECHANISM instead.
if grep -qF "export PATH=\"\$HOME/.local/bin:\$PATH\"" "$HOME/.bashrc" 2>/dev/null; then
    say "    interactive:      ~/.bashrc carries the PATH line (same prepend the login probe proved)"
else
    say "    interactive:      ~/.bashrc is MISSING the PATH line — interactive shells will use gccgo"
fi
env -i HOME="$HOME" PATH="$STOCK_PATH" sh -c 'command -v go; go env GOVERSION' 2>/dev/null \
    | sed 's/^/    bare non-login:   /' || true
say ""
say "Now build gopi:   make build      (or: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)"
say ""
say "NOTE — a gopi started by a NON-SHELL mechanism (cron, minimal systemd unit)"
say "does not inherit either rc file. Give that unit PATH explicitly; do NOT use"
say "sudo to install Go into /usr/local/go:"
say '    Environment="PATH=/home/<you>/.local/bin:/usr/local/bin:/usr/bin:/bin"'
say "A user systemd unit can also set it:   systemctl --user edit <unit>"
say ""
say "OPTIONAL, NEEDS ROOT — only if you want the gccgo shadowing gone for EVERY"
say "launcher (including a bare 'sh -c', which cannot be reached from \$HOME)."
say "This is the operator's own deliberate system-level switch; the user-local"
say "install above is sufficient for login, interactive and documented unit cases."
say "Prefer update-alternatives: it is reversible, unlike overwriting the file."
say "    # 1) put the toolchain where the system expects a Go root"
say "    sudo tar -C /usr/local -xzf ${TOOLCHAIN_ROOT}/${GO_VERSION}.linux-amd64.tar.gz"
say "    # 2) register it as an alternative instead of clobbering gccgo's symlink"
say "    sudo update-alternatives --install /usr/local/bin/go go \\"
say "         /usr/local/go/bin/go 100"
say "    # 3) verify (a BARE sh, not your shell) and roll back if needed"
say "    env -i PATH=\"$STOCK_PATH\" sh -c 'go version'"
say "    sudo update-alternatives --remove go /usr/local/go/bin/go"
