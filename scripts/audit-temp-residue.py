#!/usr/bin/env python3
"""Audit (and, with --apply, reclaim) the ambient-temp-root residue.

CRITERION, not a path list (Rule 22): an entry is RECLAIMABLE when ALL hold —

  1. it is OWNED by the invoking user (another user's or root's temp data is
     never ours to judge, let alone delete);
  2. it is at least RECLAIM_MIN_AGE_DAYS old (a window wide enough that no
     plausible live session on this host is inside it);
  3. NO live process holds it — the same liveness criterion the scratch GC
     uses: no /proc/<pid>/cwd and no /proc/<pid>/fd/* points at or under it.

Everything else is reported and LEFT ALONE. Deletion is per verified path, one
at a time, with its size printed; there is no recursive wipe of the temp root.
"""
import os
import shutil
import sys
import tempfile
import time

# The root is RESOLVED, never spelled (Rule 22): the default is the ambient temp
# root as this platform resolves it for a process (tempfile.gettempdir() — the
# TMPDIR-or-default rule), and --root=PATH is an operator's explicit choice.
# Either way the script MEASURES the root's device id against `/` and prints it,
# so it never audits a partition it cannot name.
ROOT_ARG = next((a.split("=", 1)[1] for a in sys.argv if a.startswith("--root=")), "")
TEMP_ROOT = ROOT_ARG or tempfile.gettempdir()
RECLAIM_MIN_AGE_DAYS = 7.0
APPLY = "--apply" in sys.argv
LIST_OUT = next((a.split("=", 1)[1] for a in sys.argv if a.startswith("--list=")), "")


def age_days(st):
    return (time.time() - st.st_mtime) / 86400.0


def bucket(days):
    if days < 1:
        return "<1d"
    if days < 7:
        return "1-7d"
    if days < 30:
        return "7-30d"
    return ">30d"


def entry_size(path):
    total = 0
    try:
        if os.path.islink(path) or os.path.isfile(path):
            return os.lstat(path).st_size
        for root, _dirs, files in os.walk(path, onerror=lambda e: None):
            for name in files:
                try:
                    total += os.lstat(os.path.join(root, name)).st_size
                except OSError:
                    pass
    except OSError:
        pass
    return total


def held_paths():
    """Every path a live process holds: cwd + open descriptors."""
    held = set()
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        for probe in (f"/proc/{pid}/cwd", f"/proc/{pid}/root"):
            try:
                held.add(os.path.realpath(probe))
            except OSError:
                pass
        fddir = f"/proc/{pid}/fd"
        try:
            fds = os.listdir(fddir)
        except OSError:
            continue
        for fd in fds:
            try:
                held.add(os.path.realpath(os.path.join(fddir, fd)))
            except OSError:
                pass
    return held


def is_held(path, held):
    real = os.path.realpath(path)
    for h in held:
        if h == real or h.startswith(real + os.sep):
            return True
    return False


def main():
    me = os.getuid()
    held = held_paths()
    counts, sizes = {}, {}
    reclaimable = []
    rows = []
    for name in sorted(os.listdir(TEMP_ROOT)):
        path = os.path.join(TEMP_ROOT, name)
        try:
            st = os.lstat(path)
        except OSError:
            continue
        days = age_days(st)
        size = entry_size(path)
        b = bucket(days)
        counts[b] = counts.get(b, 0) + 1
        sizes[b] = sizes.get(b, 0) + size
        owner_ok = st.st_uid == me
        old_ok = days >= RECLAIM_MIN_AGE_DAYS
        hold = is_held(path, held)
        rows.append((path, days, size, owner_ok, old_ok, hold))
        if owner_ok and old_ok and not hold:
            reclaimable.append((path, days, size))

    root_dev = os.stat(TEMP_ROOT).st_dev
    slash_dev = os.stat("/").st_dev
    total_e = sum(counts.values())
    total_mb = sum(sizes.values()) / (1024 * 1024)
    print(f"# temp root {TEMP_ROOT}: {total_e} entries, {total_mb:.0f} MB")
    print(f"# MEASURED device ids: root={root_dev} /={slash_dev} "
          f"(on / partition: {root_dev == slash_dev})")
    print("# age histogram (entries / MB):")
    for b in ("<1d", "1-7d", "7-30d", ">30d"):
        print(f"#   {b}: {counts.get(b, 0)} / {sizes.get(b, 0) / 1048576:.0f} MB")

    # What the holder/owner criteria additionally exclude from the oldest bucket.
    mine_old = [r for r in rows if r[3] and r[4]]
    foreign_old = [r for r in rows if not r[3] and r[4]]
    held_old = [r for r in mine_old if r[5]]
    print(f"# entries >= {RECLAIM_MIN_AGE_DAYS:g}d owned by us: {len(mine_old)}; of those HELD by a live process: {len(held_old)}")
    print(f"# entries >= {RECLAIM_MIN_AGE_DAYS:g}d owned by ANOTHER user: {len(foreign_old)} (never touched)")
    if held_old:
        for path, days, size, _o, _a, _h in held_old[:10]:
            print(f"#   HELD (kept): {path} ({days:.1f}d, {size / 1048576:.1f} MB)")

    reclaim_mb = sum(s for _p, _d, s in reclaimable) / 1048576
    print(f"# RECLAIMABLE (owned by us, >= {RECLAIM_MIN_AGE_DAYS:g}d, unheld): {len(reclaimable)} entries, {reclaim_mb:.0f} MB")

    if LIST_OUT:
        with open(LIST_OUT, "w") as fh:
            for path, days, size in sorted(reclaimable, key=lambda r: -r[2]):
                fh.write(f"{size:>12d}  {days:7.1f}d  {path}\n")
        print(f"# explicit path list written to {LIST_OUT}")

    for path, days, size in sorted(reclaimable, key=lambda r: -r[2])[:20]:
        print(f"#   reclaim: {path} ({days:.1f}d, {size / 1048576:.1f} MB)")

    if not APPLY:
        print("# DRY RUN — nothing removed (pass --apply to reclaim the list above)")
        return 0

    removed = 0
    freed = 0
    failed = []
    for path, _days, size in reclaimable:
        try:
            if os.path.islink(path) or os.path.isfile(path):
                os.remove(path)
            else:
                shutil.rmtree(path)
            removed += 1
            freed += size
        except OSError as exc:
            failed.append((path, exc))
    print(f"# reclaimed {removed} entries, {freed / 1048576:.0f} MB")
    for path, exc in failed:
        print(f"#   FAILED (kept): {path}: {exc}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
