#!/usr/bin/env python3
"""REPRODUCTION — classify.py's incremental mode answers for more paths than it
was asked about, and the rewrite rides along in the caller's own git commit.

The defect under measurement, in one sentence: `--add-missing` has no scope. It
asks the manifest which paths LACK a row and classifies every one of them — a
set that includes paths belonging to ANOTHER RUN's half-state tree (or an
inherited/not-yet-landed doc in the caller's own worktree), not just the path
the caller wrote. The caller then commits its own devdoc/ directory and the
other run's row rides along.

Two trees are measured, and neither is the operator's live checkout:
  A. a throwaway git FIXTURE (tools/docclass copied in, its own devdoc corpus);
  B. a READ-ONLY COPY of the live corpus on scratch (`git archive HEAD` +
     tar -x, so the live worktree is never written). The live manifest is
     hashed before and after to prove the live corpus was untouched.
"""
import hashlib
import json
import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, os.path.join(REPO, "scripts", "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

CLASSIFY_REL = ("tools", "docclass", "classify.py")
MANIFEST_REL = "devdoc/DOC-INDEX.tsv"
# The generator UNDER MEASUREMENT: the repo's own by default, or a pre-fix copy
# (DOCCLASS_CLASSIFY=<path>), which is how the SAME fixture shows the defect and
# then the closure. Copied into each fixture, so the tree under test is the tree.
CLASSIFY_SRC = os.environ.get("DOCCLASS_CLASSIFY",
                              os.path.join(REPO, "tools", "docclass", "classify.py"))
LABEL = os.environ.get("DOCCLASS_LABEL", os.path.basename(CLASSIFY_SRC))
# The path the CALLER writes — the only path a scoped run may answer for.
MINE = "devdoc/mine.md"
# The path ANOTHER RUN wrote / the tree inherited: present on disk, row absent.
INHERITED = "devdoc/other-run-half-state-report.md"


def run(cmd, cwd, check=True):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True)
    if check and p.returncode != 0:
        raise SystemExit("FAILED: %s\n%s%s" % (" ".join(cmd), p.stdout, p.stderr))
    return p


def sha(text):
    return hashlib.sha256(text.encode()).hexdigest()[:16]


def rows(path):
    """path -> raw row line (byte-exact), manifest order."""
    out, order = {}, []
    with open(path, encoding="utf-8", errors="surrogateescape") as fh:
        for line in fh:
            if not line.strip() or line.startswith("#") or line.startswith("path\t"):
                continue
            p = line.split("\t", 1)[0]
            out[p] = line
            order.append(p)
    return out, order


def diff_summary(before, after):
    added = [p for p in after if p not in before]
    removed = [p for p in before if p not in after]
    changed = [p for p in before if p in after and before[p] != after[p]]
    return {"added": sorted(added), "removed": sorted(removed), "changed_rows": sorted(changed)}


def seed_fixture(root):
    os.makedirs(os.path.join(root, "tools", "docclass"), exist_ok=True)
    os.makedirs(os.path.join(root, "devdoc"), exist_ok=True)
    shutil.copy(CLASSIFY_SRC, os.path.join(root, "tools", "docclass", "classify.py"))
    shutil.copy(os.path.join(REPO, "tools", "docclass", "banner.py"),
                os.path.join(root, "tools", "docclass", "banner.py"))
    run(["git", "init", "-q", "."], root)
    run(["git", "config", "user.email", "repro@local"], root)
    run(["git", "config", "user.name", "repro"], root)
    files = {
        "devdoc/contract.md": "# Standing Contract\n\nThe gate MUST hold. This is a contract.\n",
        "devdoc/task-report-probe.md": "# Task Report: probe\n\nA run observed X.\n",
        "devdoc/notes.txt": "hello\n",
    }
    for rel, body in files.items():
        with open(os.path.join(root, rel), "w") as fh:
            fh.write(body)
    run([sys.executable, os.path.join(*CLASSIFY_REL), "."], root)
    run(["git", "add", "-A"], root)
    run(["git", "commit", "-qm", "base corpus"], root)
    return files


def part_a_fixture(scratch):
    print("=" * 72)
    print("A. GIT FIXTURE — --add-missing answers for a path the caller never wrote")
    print("=" * 72)
    root = os.path.join(scratch, "fixture")
    os.makedirs(root, exist_ok=True)
    seed_fixture(root)
    man = os.path.join(root, MANIFEST_REL)

    # The other run's half-state tree: a doc on disk whose row is absent. In the
    # live incident this is a sibling run's uncommitted file; here it is written
    # by "another run" and never named by the caller.
    with open(os.path.join(root, INHERITED), "w") as fh:
        fh.write("# Task Report: another run's probe\n\nA record of a run.\n")
    # The caller's own path.
    with open(os.path.join(root, MINE), "w") as fh:
        fh.write("# Mine\n\nA note this run wrote.\n")

    before, _ = rows(man)
    print("caller wrote exactly:      %s" % MINE)
    print("caller never named:        %s" % INHERITED)
    print("manifest rows before:      %d;  %s has a row? %s"
          % (len(before), INHERITED, INHERITED in before))
    print("VERBATIM INVOCATION: python3 tools/docclass/classify.py . --add-missing")
    p = run([sys.executable, os.path.join(*CLASSIFY_REL), ".", "--add-missing"], root, check=False)
    after, _ = rows(man)
    d = diff_summary(before, after)
    print("exit code:                 %d" % p.returncode)
    print("classify.py stdout:        %s" % p.stdout.strip().replace("\n", " "))
    if p.stderr.strip():
        print("classify.py stderr:        %s" % p.stderr.strip().replace("\n", "\n                           "))
    print("ROWS THE RUN WAS ASKED FOR:   1 (%s)" % MINE)
    print("ROWS IT WROTE:                %d %s" % (len(d["added"]), json.dumps(d["added"])))
    refused = p.returncode != 0 and INHERITED not in after
    print("REFUSED (recorded, nothing written): %s" % refused)
    if INHERITED in after:
        print("  -> DEFECT: %s was written although NOTHING in the caller's scope names it"
              % INHERITED)
        print("  row byte-compare: before ABSENT; after: %s" % after[INHERITED].rstrip("\n"))
    else:
        print("  -> CLOSED: no row was written for %s (nor for %s)" % (INHERITED, MINE))

    # The riding-along: the caller stages ITS OWN corpus directory and commits.
    # A REFUSED run leaves this staging empty, which is the closure: there is
    # nothing of another run's state for the caller's commit to carry.
    run(["git", "add", "devdoc/"], root)
    staged = run(["git", "diff", "--cached", "--name-only"], root).stdout.split()
    print("caller runs `git add devdoc/` — what the commit would carry:")
    print("  %s" % json.dumps(staged))
    print("  -> the manifest is in the caller's commit: %s" % (MANIFEST_REL in staged))
    # THE RIDE-ALONG, proved on a real commit: the caller commits, and the row
    # for the path it never touched is inside that commit's manifest diff.
    run(["git", "commit", "-qm", "docs: mine"], root)
    in_commit = run(["git", "show", "--name-only", "--format=", "HEAD"], root).stdout.split()
    print("the caller's own commit carries: %s" % json.dumps(in_commit))
    body = run(["git", "show", "HEAD", "--", MANIFEST_REL], root).stdout
    committed_rows = [l[1:].split("\t", 1)[0] for l in body.split("\n")
                      if l.startswith("+") and not l.startswith("+++")]
    print("rows the commit ADDED to the manifest: %s" % json.dumps(committed_rows))
    return {"fixture_rows_written": d["added"], "staged": staged,
            "rows_committed_by_the_caller": committed_rows, "refused": refused}


def part_b_live_copy(scratch):
    print()
    print("=" * 72)
    print("B. COPY OF THE LIVE CORPUS — the same command, live tree proven untouched")
    print("=" * 72)
    copy = os.path.join(scratch, "live-copy")
    os.makedirs(copy, exist_ok=True)
    live_man = os.path.join(REPO, MANIFEST_REL)
    live_before = open(live_man, "rb").read()
    # READ-ONLY COPY: git archive emits the committed tree; the live worktree is
    # never written by this script (no checkout, no stash, no worktree add).
    tar = subprocess.run(["git", "archive", "HEAD", "devdoc", "tools"], cwd=REPO,
                         capture_output=True)
    subprocess.run(["tar", "-x", "-C", copy], input=tar.stdout, check=True)
    shutil.copy(CLASSIFY_SRC, os.path.join(copy, "tools", "docclass", "classify.py"))
    man = os.path.join(copy, MANIFEST_REL)
    before_raw = open(man, "rb").read()
    before, _ = rows(man)
    print("live tree copied read-only via: git archive HEAD devdoc tools | tar -x")
    print("copy manifest rows:        %d" % len(before))
    live_inherited = [p for p in os.listdir(copy) if False]  # noqa: F841 (kept explicit)
    # The half-state of the live incident: a doc present on disk with no row.
    target = "devdoc/zz-repro-inherited-probe.md"
    with open(os.path.join(copy, target), "w") as fh:
        fh.write("# Task Report: inherited probe\n\nA record of a run that never landed.\n")
    print("injected into the COPY only: %s" % target)
    print("VERBATIM INVOCATION: python3 tools/docclass/classify.py . --add-missing   (cwd=%s)" % copy)
    p = run([sys.executable, os.path.join(*CLASSIFY_REL), ".", "--add-missing"], copy, check=False)
    after, _ = rows(man)
    d = diff_summary(before, after)
    print("exit code:                 %d" % p.returncode)
    print("classify.py stdout:        %s" % p.stdout.strip().replace("\n", " "))
    if p.returncode != 0:
        print("classify.py stderr:        %s" % p.stderr.strip().split("\n")[0])
    print("rows added in the COPY:    %d" % len(d["added"]))
    print("copy manifest changed:     %s" % (open(man, "rb").read() != before_raw))
    if target in after:
        print("row written for the injected path (%s):" % target)
        print("  %s" % after[target].rstrip("\n"))
    else:
        print("CLOSED: no row was written for the injected path %s" % target)
    live_after = open(live_man, "rb").read()
    print("LIVE manifest sha256 before/after: %s / %s  (identical=%s)"
          % (sha(live_before.decode("utf-8", "surrogateescape")),
             sha(live_after.decode("utf-8", "surrogateescape")),
             live_before == live_after))
    status = run(["git", "status", "--porcelain"], REPO).stdout
    live_corpus_dirty = [l for l in status.split("\n") if l.strip() and "DOC-INDEX" in l]
    print("live `git status --porcelain` DOC-INDEX entries: %s" % json.dumps(live_corpus_dirty))
    return {"copy_rows_added": len(d["added"]), "live_untouched": live_before == live_after,
            "copy_refused": p.returncode != 0 and target not in after}


def part_c_only_is_atomic(scratch):
    print()
    print("=" * 72)
    print("C. CONTROL — `--only P` is already pair-atomic (the fix's target shape)")
    print("=" * 72)
    # A FRESH fixture: part A left rows behind, and reusing its tree would measure
    # idempotence instead of the write itself.
    root = os.path.join(scratch, "fixture-only")
    os.makedirs(root, exist_ok=True)
    seed_fixture(root)
    man = os.path.join(root, MANIFEST_REL)
    with open(os.path.join(root, MINE), "w") as fh:
        fh.write("# Mine\n\nA note this run wrote.\n")
    before, _ = rows(man)
    raw_before = open(man, "rb").read()
    print("VERBATIM INVOCATION: python3 tools/docclass/classify.py . --only %s" % MINE)
    p = run([sys.executable, os.path.join(*CLASSIFY_REL), ".", "--only", MINE], root)
    after, after_order = rows(man)
    d = diff_summary(before, after)
    print("classify.py stdout:        %s" % p.stdout.strip().replace("\n", " "))
    print("rows added: %s  rows changed: %s  rows removed: %s"
          % (json.dumps(d["added"]), json.dumps(d["changed_rows"]), json.dumps(d["removed"])))
    print("order preserved:           %s" % (list(before) == [p for p in after_order if p in before]))
    print("manifest really changed:   %s" % (open(man, "rb").read() != raw_before))
    d["manifest_changed"] = open(man, "rb").read() != raw_before
    return d


def main():
    scratch = make_scratch_dir("docindex-inherited-row-repro")
    print("GENERATOR UNDER MEASUREMENT: %s  (%s)" % (CLASSIFY_SRC, LABEL))
    print("scratch: %s  (device %s, / device %s)"
          % (scratch, os.stat(scratch).st_dev, os.stat("/").st_dev))
    a = part_a_fixture(scratch)
    b = part_b_live_copy(scratch)
    c = part_c_only_is_atomic(scratch)
    print()
    print("=" * 72)
    print("VERDICT")
    print("=" * 72)
    print("A: --add-missing refused: %s; wrote %d row(s): %s"
          % (a["refused"], len(a["fixture_rows_written"]), json.dumps(a["fixture_rows_written"])))
    print("   the caller's own commit carried %d manifest row(s): %s"
          % (len(a["rows_committed_by_the_caller"]), json.dumps(a["rows_committed_by_the_caller"])))
    print("   -> include of a path the caller never named: %s"
          % (INHERITED in a["rows_committed_by_the_caller"]))
    print("B: copy of the live corpus — refused: %s, rows written: %d; live tree untouched: %s"
          % (b["copy_refused"], b["copy_rows_added"], b["live_untouched"]))
    # The manifest's own row is not a "foreign" write: the manifest is the file
    # this command writes, and it must index itself (the coverage gate reads it).
    print("C: --only wrote exactly the ONE named path and rewrote no other row: %s (added=%s "
          "changed=%s removed=%s)"
          % (c["added"] == [MINE] and not c["changed_rows"] and not c["removed"],
             json.dumps(c["added"]), json.dumps(c["changed_rows"]), json.dumps(c["removed"])))


if __name__ == "__main__":
    main()
