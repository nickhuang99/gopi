#!/usr/bin/env python3
"""tmp_ban.py — the Python half of the GoPi scratch-root contract.

Every Python dev/test harness in this repo (scripts/m1_acceptance.py,
scripts/m2_acceptance.py, ...) creates throwaway directories and load output.
On this host `tempfile.mkdtemp()` defaults to /tmp, which lives on the 147G
`/` partition (87% full) — the partition the 2026-09-11 exhaustion filled with
13,044 abandoned scratch dirs plus an unbounded stress loop
(devdoc/peerbox-resource-exhaustion-20260911.md).

The contract, identical to scripts/lib/tmp-ban.sh and
internal/testutil/scratchroot.go (one rule, three languages):

  1. LOCATION — scratch data lands under <home>/workspace/gopi-test-scratch/,
     the same tree the Go suite GC sweeps; /tmp is never a candidate.
  2. ASSERTION — the chosen path's device id is compared with `/`'s and a
     match is a HARD failure with the fix spelled out, not a warning.
  3. BOUNDED — a scratch directory is removed when its owner is done (context
     manager), and any loop over an external process is bounded by both time
     and iterations.

Usage:
    from tmp_ban import scratch_dir, bounded_loop, assert_off_root_fs

    with scratch_dir("m2-acc") as tmp:
        db = os.path.join(tmp, "memory.db")
"""
from __future__ import annotations

import atexit
import os
import shutil
import subprocess
import tempfile
import time
from contextlib import contextmanager
from typing import Iterator, List, Sequence

# ScratchDirName mirrors testutil.ScratchRootDirName (Go) and the shell
# helper's literal: ONE name for the tree, three languages (Rule 14).
SCRATCH_DIR_NAME = "gopi-test-scratch"
# QueueDirName mirrors testutil.ScratchQueueDirName: suite-less large
# artifacts (DB copies, load output) live here, under the same GC.
QUEUE_DIR_NAME = "queue"
# ScratchHomeEnv / WorkspaceHomeEnv mirror the Go constants of the same name —
# the ONLY variables this module reads (Rule 2: one reader per value).
SCRATCH_HOME_ENV = "GOPI_TEST_SCRATCH_HOME"
WORKSPACE_HOME_ENV = "GOPI_TEST_HOME"
# ScratchEnvVarName is the standard temp-dir variable. A process that leaves
# it at /tmp is exactly what this module exists to catch.
SCRATCH_ENV_VAR_NAME = "TMPDIR"

# Go-side naming parity: a queue entry is "queue-<what>-<pid>", which the
# scratch sweep (testutil.isScratchEntry) recognises and age-GCs.
QUEUE_PREFIX = "queue"


def scratch_home() -> str:
    """The scratch home — <home>/workspace/gopi-test-scratch.

    GOPI_TEST_SCRATCH_HOME wins (the Go suites publish it to children so a
    grandchild's scratch stays attributable), then GOPI_TEST_HOME, then the
    real home. os.TempDir() is deliberately never consulted.
    """
    env = os.environ.get(SCRATCH_HOME_ENV)
    if env:
        return env
    home = os.environ.get(WORKSPACE_HOME_ENV) or os.path.expanduser("~")
    return os.path.join(home, "workspace", SCRATCH_DIR_NAME)


# X11SocketDirName / X11BaseDirName mirror the shell helper's accessors
# (scripts/lib/tmp-ban.sh) and internal/shot/x11.go's x11UnixSocketDir. The X11
# socket directory is the ONE /tmp path this repository keeps, under the same
# reason as the Go side: a unix SOCKET in it is created by the X server, never
# written by us — it is not scratch, so the "off / by device id" rule does not
# apply. It is spelled HERE, once, so no probe script has to.
X11_SOCKET_DIR_ENV = "GOPI_X11_SOCKET_DIR"
X11_BASE_DIR_ENV = "GOPI_X11_BASE_DIR"
X11_SOCKET_DIR_DEFAULT = "/tmp/.X11-unix"
X11_BASE_DIR_DEFAULT = "/tmp"


def x11_socket_dir() -> str:
    """The X11 socket directory (one definition; env-overridable)."""
    return os.environ.get(X11_SOCKET_DIR_ENV) or X11_SOCKET_DIR_DEFAULT


def x11_socket_path(display: str) -> str:
    """The socket path for a display spelling (":99" → <dir>/X99)."""
    return os.path.join(x11_socket_dir(), "X" + display.lstrip(":"))


def x11_base_dir() -> str:
    """The directory the X server's own lock files live in (X<n>-lock)."""
    return os.environ.get(X11_BASE_DIR_ENV) or X11_BASE_DIR_DEFAULT


def scratch_queue() -> str:
    """The suite-less queue inside the scratch home (large throwaway files)."""
    return os.path.join(scratch_home(), QUEUE_DIR_NAME)


def device_id(path: str) -> int:
    """The kernel's device id of path — the key `stat -c %d /` reports."""
    return os.stat(path).st_dev


def shares_root_filesystem(path: str) -> bool:
    """True when path lives on the same filesystem as `/` (i.e. /tmp here)."""
    return device_id(path) == device_id(os.sep)


def assert_off_root_fs(path: str) -> None:
    """Fail HARD (SystemExit) when path shares a filesystem with `/`.

    A hard failure with an actionable message, never a warning: the whole
    point of the ban is that a warning is ignored — the 2026-09-11 incident
    left 13,044 abandoned directories in /tmp behind one.
    """
    if not os.path.isdir(path):
        raise SystemExit(f"tmp-ban: {path} is not a directory — cannot verify it is off /")
    if shares_root_filesystem(path):
        raise SystemExit(
            f"tmp-ban: scratch path {path} is on the SAME filesystem as / "
            f"(device {device_id(path)}).\n"
            f"  /tmp is that filesystem here: the 147G partition at 87% full whose\n"
            f"  2026-09-11 exhaustion (unbounded stress loop + 13,044 abandoned scratch\n"
            f"  dirs) is why GoPi bans it as a test/dev workspace.\n"
            f"  FIX: use scratch_dir()/scratch_file() from scripts/lib/tmp_ban.py\n"
            f"  (they land under {scratch_home()}), and unset {SCRATCH_ENV_VAR_NAME}\n"
            f"  if it points at /tmp."
        )


@contextmanager
def scratch_dir(what: str) -> Iterator[str]:
    """Create a scratch directory under the queue and remove it on exit.

    The removal is unconditional (a killed harness leaks one directory, which
    the age GC then collects — the same belt-and-braces the Go side uses).
    """
    queue = scratch_queue()
    os.makedirs(queue, exist_ok=True)
    assert_off_root_fs(queue)  # never hand back a /tmp path silently
    path = tempfile.mkdtemp(prefix=f"{QUEUE_PREFIX}-{what}-", dir=queue)
    try:
        yield path
    finally:
        shutil.rmtree(path, ignore_errors=True)


def scratch_file(name: str) -> str:
    """A path inside a fresh scratch directory for a load artifact."""
    queue = scratch_queue()
    os.makedirs(queue, exist_ok=True)
    assert_off_root_fs(queue)
    path = os.path.join(queue, f"{QUEUE_PREFIX}-load-{os.getpid()}", name)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    return path


def queue_path(name: str) -> str:
    """A stable path inside the scratch QUEUE (an input/output location a
    harness's flags default to). The queue is under the scratch home — never
    /tmp — and is age-GC'd by the Go sweep."""
    queue = scratch_queue()
    os.makedirs(queue, exist_ok=True)
    assert_off_root_fs(queue)
    return os.path.join(queue, name)


# _scratch_cleanup_paths collects every directory make_scratch_dir created, so
# a script that never exits through its own cleanup still removes them.
_scratch_cleanup_paths: List[str] = []
_scratch_atexit_registered = False


def _cleanup_scratch_dirs() -> None:
    for path in list(_scratch_cleanup_paths):
        shutil.rmtree(path, ignore_errors=True)
    _scratch_cleanup_paths.clear()


def make_scratch_dir(what: str) -> str:
    """Create a scratch directory under the queue, removed at interpreter exit.

    The plain-function form of scratch_dir() for a script whose flow is not a
    single `with` block. Removal is registered with atexit (a killed process
    leaks one directory, which the age GC then collects — same belt and braces
    as the Go side).
    """
    global _scratch_atexit_registered
    queue = scratch_queue()
    os.makedirs(queue, exist_ok=True)
    assert_off_root_fs(queue)  # never hand back a /tmp path silently
    path = tempfile.mkdtemp(prefix=f"{QUEUE_PREFIX}-{what}-", dir=queue)
    _scratch_cleanup_paths.append(path)
    if not _scratch_atexit_registered:
        atexit.register(_cleanup_scratch_dirs)
        _scratch_atexit_registered = True
    return path


def bounded_loop(seconds: float, iterations: int, fn, *args, **kwargs) -> List[object]:
    """Call fn(...) at most `iterations` times and for at most `seconds`.

    Both bounds are structural: the iteration cap stops a FAST spin (a `while
    True` calling a subprocess tens of thousands of times), the deadline stops
    a SLOW stall. Returns the list of results collected.
    """
    results: List[object] = []
    deadline = time.monotonic() + seconds
    for _ in range(iterations):
        if time.monotonic() > deadline:
            break
        results.append(fn(*args, **kwargs))
    return results


def run_bounded(cmd: Sequence[str], seconds: float = 60, env=None) -> subprocess.CompletedProcess:
    """Run a subprocess with a HARD timeout.

    Every subprocess a dev harness starts goes through here (or passes its own
    timeout): a hung child with no timeout is how a load script turns into an
    unattended resource leak. An expired child is killed by subprocess.run,
    and the TimeoutExpired is returned as a non-zero CompletedProcess so the
    caller's accounting stays uniform.
    """
    try:
        return subprocess.run(list(cmd), capture_output=True, text=True,
                              timeout=seconds, env=env)
    except subprocess.TimeoutExpired as exc:
        return subprocess.CompletedProcess(
            args=list(cmd), returncode=124,
            stdout=(exc.stdout or "") if isinstance(exc.stdout, str) else "",
            stderr=f"timeout after {seconds}s",
        )
