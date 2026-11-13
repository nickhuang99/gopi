#!/usr/bin/env bash
# build-links-static.sh — build the VENDORED text browser the `gopi cli`
# carrier execs, as a statically linked binary, and prove it.
#
# WHY A SCRIPT AND NOT A BUILD STEP (GOPI.md Rule 10.7, the dependency
# budget): links2 is a third-party C tool, so it is NOT a go.mod dependency and
# it is NEVER part of `go build ./cmd/agent/` — the shipped gopi artifact stays
# `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` with zero new Go modules.
# gopi execs this binary as a CHILD PROCESS (job-class external tool, like
# vcmi/wine); `cmd/agent/cli_term_links.go` holds the discovery policy (the
# GOPI_LINKS_BIN override, then the vendored per-arch path this script writes).
#
# THE FIVE THINGS THIS SCRIPT GUARANTEES
#   1. one documented configure line — the exact flags are below, and they are
#      the ones the committed binary was built with;
#   2. STATIC linking, PROVEN (`ldd` must say "not a dynamic executable") — a
#      dynamically linked browser would drag the host's glibc into a carrier
#      whose whole point is zero dependencies;
#   3. no graphics, no compression, no SSL: the carrier reads local HTML over
#      loopback, so every optional dependency is a liability, not a feature;
#   4. UTF-8 terminal support (`--enable-utf8`), because the chat carries CJK;
#   5. the scratch tree is OFF the `/` device (Rule 22) — the source extract
#      and the build directory never land in /tmp, and the device is MEASURED,
#      not assumed.
#
# Usage (bash, NOT sh: scripts/lib/tmp-ban.sh is a bash library — sourcing it
# from dash fails with "Bad for loop variable", which is how this was found):
#   bash scripts/build-links-static.sh --source <links-2.25.tar.bz2|dir> [--out DIR]
#   bash scripts/build-links-static.sh --help
#
# The result is written to <repo>/scripts/links/<GOARCH>/links (the vendored
# path the launcher resolves) unless --out names another directory.

set -euo pipefail

# ---------------------------------------------------------------- constants
# Every literal this script depends on is named here (Rule 1a/1b): the version
# the vendored binary is built from, the signature of a successful static link,
# and the vendored layout the launcher and this script must agree on.
PARALLEL_FALLBACK_JOBS=4
LINKS_VERSION="2.25"
LINKS_TARBALL_GLOB="links2_${LINKS_VERSION}*.orig.tar.*"
VENDOR_DIR_NAME="links"
LD_PROOF_STATIC="not a dynamic executable"
REPO_MARKER="go.mod"
ARCH_PLACEHOLDER="<GOARCH>"
BINARY_NAME="links"

# The configure line, verbatim, as one string so the report and the build
# cannot drift. `--enable-utf8` keeps CJK; every `--without-*` removes a
# dependency an HTTP-over-loopback client does not need; `--disable-graphics`
# keeps the terminal build out of the X11/framebuffer drivers entirely.
CONFIGURE_FLAGS="--enable-utf8 --without-ssl --without-zlib --without-lzma --without-bzip2 --without-gpm --without-x --without-svgalib --without-fb --without-directfb --without-pmshell --without-windows --without-atheos --without-haiku --without-grx --disable-graphics"
# `-fno-pie` / `-static -no-pie`: a static PIE is still dynamic-looking to some
# tooling, and Ubuntu's default PIE hardening fights `-static`. These two flags
# are what make the ldd proof below come out "not a dynamic executable".
# `-DHAVE_ANY_COMPRESSION`: links2 2.25 guards the `return_decompressed` LABEL
# with HAVE_ANY_COMPRESSION but its `goto return_decompressed` SITES with the
# per-format macros (HAVE_ZLIB, …, which this configure line leaves undefined).
# With every compression format off, the label vanishes and the build dies on
# "label 'return_decompressed' used but not defined" (measured). Defining it is
# what tells the compiler a decompression path exists at all; the per-format
# blocks stay compiled out, so nothing is actually linked in.
CONFIGURE_CFLAGS="-O2 -fno-pie -DHAVE_ANY_COMPRESSION"
CONFIGURE_LDFLAGS="-static -no-pie"

die() {
    echo "build-links-static: $*" >&2
    exit 1
}

usage() {
    cat <<EOF
build-links-static.sh — build the vendored text browser (links2 ${LINKS_VERSION}, static).

  bash scripts/build-links-static.sh --source <tarball|source-dir> [--out DIR] [--arch ARCH]

Options:
  --source PATH   the links2 ${LINKS_VERSION} source: a .tar.bz2/.tar.gz/.tar.xz
                  tarball or an already-extracted source DIRECTORY. Required:
                  this script never downloads (the box is offline).
  --out DIR       where to write the binary (default: <repo>/scripts/${VENDOR_DIR_NAME}/ARCH/)
  --arch ARCH     the platform directory to write into (default: uname -m)
  --keep          keep the scratch build tree instead of removing it
  -h, --help      this help

Exit status: 0 = built and the static-link proof passed; non-zero otherwise.
EOF
}

# ------------------------------------------------------------ scratch (Rule 22)
# Source the repo's scratch contract when it is available (the normal case:
# this script lives in the repo). It provides gopi_scratch_mkdir, which lands
# scratch data under the workspace and REFUSES a path sharing the device of `/`.
# Without it we still refuse /tmp explicitly rather than silently falling back.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "${SCRIPT_DIR}/lib/tmp-ban.sh" ]; then
    # shellcheck disable=SC1091
    . "${SCRIPT_DIR}/lib/tmp-ban.sh"
fi

scratch_dir() {
    if command -v gopi_scratch_mkdir >/dev/null 2>&1; then
        gopi_scratch_mkdir "links-build"
        return 0
    fi
    # Fallback: a directory under the caller's HOME workspace, with the `/`
    # device CHECKED (the criterion, never a spelling — Rule 22).
    base="${GOPI_TEST_HOME:-$HOME}/workspace/gopi-test-scratch/queue"
    mkdir -p "$base" || die "cannot create scratch base $base"
    if [ "$(stat -c %d "$base")" = "$(stat -c %d /)" ]; then
        die "scratch base $base is on the SAME device as / — refusing (Rule 22: use the workspace, never /tmp)"
    fi
    d="${base}/links-build-$$"
    mkdir -p "$d" || die "cannot create scratch dir $d"
    printf '%s\n' "$d"
}

# ------------------------------------------------------------------- args
source_arg=""
out_arg=""
arch_arg=""
keep=0
while [ $# -gt 0 ]; do
    case "$1" in
        --source)
            [ $# -ge 2 ] || die "--source needs a value"
            source_arg="$2"
            shift 2
            ;;
        --source=*)
            source_arg="${1#--source=}"
            shift
            ;;
        --out)
            [ $# -ge 2 ] || die "--out needs a value"
            out_arg="$2"
            shift 2
            ;;
        --out=*)
            out_arg="${1#--out=}"
            shift
            ;;
        --arch)
            [ $# -ge 2 ] || die "--arch needs a value"
            arch_arg="$2"
            shift 2
            ;;
        --arch=*)
            arch_arg="${1#--arch=}"
            shift
            ;;
        --keep)
            keep=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            die "unknown argument $1 (see --help)"
            ;;
    esac
done

[ -n "$source_arg" ] || die "--source is required: point it at a links2 ${LINKS_VERSION} tarball or source directory (see --help)"
[ -e "$source_arg" ] || die "--source $source_arg does not exist"

# ------------------------------------------------------------------- layout
# Repo root: walk up from this script to the directory holding the marker, so
# `--out`'s default is the path the LAUNCHER resolves (one rule, one path).
repo_root() {
    dir="$SCRIPT_DIR"
    i=0
    while [ "$i" -le 4 ]; do
        [ -f "${dir}/${REPO_MARKER}" ] && { printf '%s\n' "$dir"; return 0; }
        parent=$(dirname -- "$dir")
        [ "$parent" = "$dir" ] && break
        dir="$parent"
        i=$((i + 1))
    done
    return 1
}

arch="${arch_arg:-$(uname -m)}"
if [ -z "$out_arg" ]; then
    root=$(repo_root) || die "cannot find the repo root (no ${REPO_MARKER} walking up from ${SCRIPT_DIR}); pass --out DIR"
    out_arg="${root}/scripts/${VENDOR_DIR_NAME}/${arch}"
fi
mkdir -p "$out_arg" || die "cannot create output directory $out_arg"

work=$(scratch_dir)
# No trap on EXIT for the failure path: a failed build is exactly when the log
# is worth keeping, and the scratch tree is age-GC'd (tmp-ban.sh) either way.
cleanup() {
    if [ "$keep" -eq 1 ]; then
        echo "build-links-static: scratch kept at $work" >&2
        return 0
    fi
    rm -rf -- "$work"
}
trap cleanup EXIT INT TERM

# ------------------------------------------------------------------- source
src_dir=""
case "$source_arg" in
    *.tar.bz2|*.tar.gz|*.tar.xz|*.tgz)
        echo "build-links-static: extracting $source_arg"
        tar xf "$source_arg" -C "$work" || die "cannot extract $source_arg (need tar + the matching decompressor)"
        # The tarball's top-level directory is the source root (links2-2.25 for
        # the 2.25 orig tarball). One level only — never a find over the tree.
        src_dir=$(find "$work" -maxdepth 1 -mindepth 1 -type d | head -n 1)
        [ -n "$src_dir" ] || die "the tarball extracted no directory into $work"
        ;;
    *)
        [ -d "$source_arg" ] || die "--source $source_arg is neither a tarball nor a directory"
        # Copy the tree so the build never dirties the caller's checkout.
        src_dir="${work}/src"
        cp -a "$source_arg" "$src_dir" || die "cannot copy sources from $source_arg"
        ;;
esac

[ -f "${src_dir}/configure" ] || die "${src_dir} has no configure script — is this a links2 source tree?"
[ -f "${src_dir}/links.h" ] || echo "build-links-static: warning: ${src_dir} has no links.h; continuing (the configure step will fail loudly if this is not links2)" >&2

parallel_jobs() {
    if command -v nproc >/dev/null 2>&1; then
        nproc
        return 0
    fi
    printf '%s\n' "$PARALLEL_FALLBACK_JOBS"
}

# --------------------------------------------------------------- configure
# An out-of-tree build directory: links2 historically ships a tarball that
# already carries a configured `Makefile`, so an in-tree build can refuse with
# "source directory already configured". Building out of tree sidesteps that
# class of failure entirely and keeps the build log separate from the sources.
build_dir="${work}/build"
mkdir -p "$build_dir"
echo "build-links-static: configure (flags: ${CONFIGURE_FLAGS})"
(
    cd "$build_dir" || exit 1
    CFLAGS="$CONFIGURE_CFLAGS" LDFLAGS="$CONFIGURE_LDFLAGS" \
        "${src_dir}/configure" $CONFIGURE_FLAGS > "${work}/configure.log" 2>&1
) || die "configure failed — see ${work}/configure.log"

echo "build-links-static: make"
if ! make -C "$build_dir" -j"$(parallel_jobs)" > "${work}/make.log" 2>&1; then
    tail -n 20 "${work}/make.log" >&2 || true
    die "make failed — full log at ${work}/make.log"
fi

built="${build_dir}/${BINARY_NAME}"
[ -f "$built" ] || die "the build produced no ${built}"

# ------------------------------------------------------------------- prove
# THE PROOF IS THE POINT: a dynamically linked browser would make this carrier
# depend on the host's libc version, which is the dependency the whole design
# refuses. `ldd` printing "not a dynamic executable" is the observable fact.
proof=$(ldd "$built" 2>&1 || true)
case "$proof" in
    *"${LD_PROOF_STATIC}"*) : ;;
    *) die "the built binary is DYNAMICALLY linked (ldd said: ${proof}) — -static did not take effect" ;;
esac
echo "build-links-static: ldd proof: ${LD_PROOF_STATIC}"

install -m 755 "$built" "${out_arg}/${BINARY_NAME}" || die "cannot install to ${out_arg}/${BINARY_NAME}"

echo "build-links-static: wrote ${out_arg}/${BINARY_NAME}"
echo "build-links-static: recipe"
echo "  source:    links2 ${LINKS_VERSION}"
echo "  configure: ${CONFIGURE_FLAGS}"
echo "  CFLAGS:    ${CONFIGURE_CFLAGS}"
echo "  LDFLAGS:   ${CONFIGURE_LDFLAGS}"
echo "  proof:     ldd -> ${LD_PROOF_STATIC}"
echo "build-links-static: gopi finds it at scripts/${VENDOR_DIR_NAME}/${ARCH_PLACEHOLDER}/${BINARY_NAME} (or via GOPI_LINKS_BIN=…)"
