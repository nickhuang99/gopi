"""Build the archive-tag verdict table, measuring EVERY row against the CURRENT master.

Usage:  python3 devdoc/archive-tag-sweep-build-verdicts.py [repo] [master-ref]
Output: TSV on stdout (tag, verdict, tip, delta, absent_all, absent_src, syms_absent,
        task_id, task_status, evidence).

Every number is measured here, in one pass, against one master tip: the tag set is not
frozen (two tags arrived during the day) and master is not frozen either (between two runs
of this script `internal/heavytest/heavy.go` landed on master and turned a RECOVER row into
a CLOSE row). A census copied from an earlier run is therefore stale by construction - that
regeneration is why this file exists.

Verdict vocabulary - what a RECOVER is NOT matters as much as what it is:

  CLOSE         - tip is an ancestor of master: the branch landed, the tag is a spent ref.
  CLOSE-TESTDOC - not an ancestor, but the only paths master lacks are tests/docs/.live
                  fixtures: every PRODUCTION path it touches exists on master. (Most archive
                  tags are "ahead" of master merely because they branched off an older
                  master; `git diff master...<tag>` then lists their own base files as
                  "missing". The census below is what separates that noise from a real gap.)
  REVIEW        - not an ancestor and 0 files missing: master carries the same paths, moved
                  on. Weakest class - a path match is not a symbol match - and it is where
                  `archive/single-row-verify-20260905` sits (its 3-dot diff is EMPTY: it
                  differs from master by nothing, so it is CLOSE-class in substance; the
                  empty diff is printed as the evidence rather than hidden).
  RECOVER       - an absent production .go file whose own defined symbols appear NOWHERE on
                  master. Note the trap: an absent file whose symbols ARE all on master is
                  NOT a recovery - master has the content under a different file. The symbol
                  half of this predicate is what makes it survive that case (only one tag,
                  testsuite-codex-20260827, is an absent-file-but-symbols-present case; it is
                  reported REVIEW, not RECOVER).

A tag whose branches is not owned by any ledger row (task_id '-') is reported as such: the
"was this finished?" question has no answer from the ledger, and that is a finding, not a
blank.
"""
import collections
import csv
import os
import re
import sqlite3
import subprocess
import sys

REPO = sys.argv[1] if len(sys.argv) > 1 else "/home/user/work/GoPi"
MASTER = sys.argv[2] if len(sys.argv) > 2 else "master"
DB = "/home/user/.gopi/memory.db"

# Hand-adjudicated rows: the mechanical predicate is a NAME test, so it cannot see a
# capability that master re-implemented under a different name. Each entry carries the
# file:line on master that supplies the capability, checked by opening both sides.
# This is the ONE false-positive mode of the RECOVER predicate, and it is recorded here
# rather than silently tolerated: an "absent symbol" is a question, not a verdict.
ADJUDICATED = {
    "archive/hippocampus-host-superseded-20260910": (
        "SUPERSEDED",
        "the binding IS on master under a renamed surface: master:internal/agent/hippocampus_agent.go:174 "
        "calls hippocampus.NewHostEngine(...) (master:internal/hippocampus/host.go:109), i.e. the same live-organ "
        "binding this tag's internal/agent/hippocampus_binding.go introduced as bindHippocampusOrgan/hippoTransport. "
        "The tag's own name (host-superseded) says so; the RECOVER verdict the name predicate returned is wrong"),
}

DECL = re.compile(r"^(?:func|type|var|const)\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)", re.M)


def git(*args):
    p = subprocess.run(["git", "-C", REPO] + list(args), capture_output=True, text=True)
    return p.returncode, p.stdout.strip()


def main():
    tags = [t for t in git("tag", "-l", "archive/*")[1].split("\n") if t.strip()]
    master_files = set(f for f in git("ls-tree", "-r", "--name-only", MASTER)[1].split("\n") if f)
    master_go = "\n".join(git("show", f"{MASTER}:{f}")[1] for f in sorted(master_files) if f.endswith(".go"))
    master_syms = set(m.group(1) for m in DECL.finditer(master_go))

    con = sqlite3.connect(f"file:{DB}?mode=ro", uri=True, timeout=15.0)
    rows = []
    for tag in sorted(tags):
        branch = tag[len("archive/"):] if tag.startswith("archive/") else tag
        rc, tip = git("rev-parse", tag + "^{commit}")
        if rc != 0:
            rows.append([tag, "BADREF", "-", "-", "-", "-", "-", "-", "-", "tag does not resolve to a commit"])
            continue
        delta = int(git("rev-list", "--count", f"{MASTER}..{tip}")[1] or 0)
        mb = git("merge-base", MASTER, tip)[1]
        changed = [f for f in git("diff", "--name-only", mb, tip)[1].split("\n") if f]
        absent = [f for f in changed if f not in master_files]
        src = [f for f in absent if f.endswith(".go") and not f.endswith("_test.go")]
        # The ledger row that OWNS this branch: deliverable.branch == the branch. The summary
        # match is the fallback (older rows predate the deliverable stamp) and it is chosen
        # EARLIEST-first, because a recovery task filed against the same branch carries the
        # branch name in its summary too - the owner is the one that predates it. Getting this
        # wrong inverts the finding: it would report the fresh recovery row instead of the
        # FINISHED row whose deliverable the tag buried.
        cur = con.execute("select task_id,status from tasks where instr(deliverable,?)>0 "
                          "order by created_at limit 1", ('"branch":"%s"' % branch,)).fetchone()
        if not cur:
            cur = con.execute("select task_id,status from tasks where instr(summary,?)>0 "
                              "order by created_at limit 1", (branch,)).fetchone()
        tid, st = cur if cur else ("-", "-")
        base = [tag, tip, delta, len(absent), len(src)]

        if git("merge-base", "--is-ancestor", tip, MASTER)[0] == 0:
            verdict, syms, ev = "CLOSE", 0, f"tip {tip} is an ancestor of master - the branch landed; the tag is a spent ref"
        elif not absent:
            verdict, syms, ev = ("REVIEW", 0,
                                 f"not an ancestor, 0 files missing: master carries every path it touches "
                                 f"({', '.join(changed[:3]) if changed else 'its 3-dot diff against master is EMPTY'})")
        elif src:
            # The symbol half of the predicate: count the symbols each absent production file
            # defines that appear NOWHERE on master. The rule is "ANY missing" (>=1), not "all":
            # a file's methods are named from a small vocabulary (signal/close/String/...), so
            # "every symbol missing" is a false-negative machine - it would have called
            # internal/lineedit/event.go present on the strength of the word `close`.
            gone = []
            for f in src:
                body = git("show", f"{tip}:{f}")[1]
                missing = [m.group(1) for m in DECL.finditer(body) if m.group(1) not in master_syms]
                if missing:
                    gone.append(f)
            syms = sum(1 for f in src for m in DECL.finditer(git("show", f"{tip}:{f}")[1])
                       if m.group(1) not in master_syms)
            if gone:
                verdict, ev = "RECOVER", (f"{delta} commit(s) beyond master; {syms} symbol(s) defined in absent "
                                          f"production file(s) appear nowhere on master: {', '.join(gone[:4])}")
            else:
                verdict, ev = "REVIEW", (f"production file(s) absent ({', '.join(src[:3])}) but EVERY symbol they "
                                         f"define exists on master - master carries the content under another file")
        else:
            verdict, syms, ev = ("CLOSE-TESTDOC", 0,
                                 f"only tests/docs absent ({len(absent)} file(s): {', '.join(absent[:4])}); "
                                 f"every production path it touches exists on master")
        if tag in ADJUDICATED:
            verdict, ev = ADJUDICATED[tag]
        rows.append(base[:1] + [verdict] + base[1:] + [syms, tid, st, ev])

    out = sys.stdout
    out.write("tag\tverdict\ttip\tdelta\tabsent_all\tabsent_src\tsyms_absent\ttask_id\ttask_status\tevidence\n")
    for r in rows:
        out.write("\t".join(map(str, r)) + "\n")
    sys.stderr.write("tags=%d %s\n" % (len(rows), dict(collections.Counter(r[1] for r in rows))))


if __name__ == "__main__":
    main()
