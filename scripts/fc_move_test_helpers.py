#!/usr/bin/env python3
"""Move ONLY the test helpers `go vet` names into a per-package helpers file.

Rule 43, applied to the test tree: the mechanism's test files went with it, but
some helpers inside them are general fixtures that LIVE tests in the same
package still call. This script asks the compiler which symbols are actually
needed (never a grep census), finds the file that DEFINED each one at HEAD, and
extracts exactly those declarations.

It never overwrites a helper already present in the live tree: a redeclaration
is a symbol the package already has, which means it was never missing.
"""
import json
import os
import re
import subprocess
import sys

PKG_DIR = {
    'cmd/agent': 'main',
    'internal/agent': 'agent',
    'internal/tools': 'tools',
    'internal/standards': 'standardgate',
}
HELPERS = {d: os.path.join(d, 'finish_cleanup_moved_helpers_test.go') for d in PKG_DIR}


def run(cmd):
    return subprocess.run(cmd, capture_output=True, text=True)


def defining_files(sym):
    """Files at HEAD that DEFINE sym (func/const/var/type at top level)."""
    out = run(['git', 'grep', '-l', '-E',
               r'^(func %s\(|func \([^)]*\) %s\(|(const|var|type) %s\b)'
               % (sym, sym, sym), 'HEAD', '--', '*_test.go']).stdout
    return [ln.split(':', 1)[1] for ln in out.strip().split('\n') if ln]


def imports_needed(body):
    """The import block the moved body needs, from the identifiers it uses."""
    std = []
    for pkg, use in (('testing', r'\btesting\.'), ('os', r'\bos\.'),
                     ('path/filepath', r'\bfilepath\.'), ('strings', r'\bstrings\.'),
                     ('fmt', r'\bfmt\.'), ('os/exec', r'\bexec\.'),
                     ('context', r'\bcontext\.'), ('time', r'\btime\.'),
                     ('sort', r'\bsort\.'), ('strconv', r'\bstrconv\.'),
                     ('io', r'\bio\.'), ('bytes', r'\bbytes\.'),
                     ('bufio', r'\bbufio\.'), ('regexp', r'\bregexp\.'),
                     ('errors', r'\berrors\.'), ('sync', r'\bsync\.'),
                     ('encoding/json', r'\bjson\.'),
                     ('database/sql', r'\bsql\.')):
        if re.search(use, body):
            std.append(pkg)
    return std


def normalize_imports(helper):
    """Rewrite the import region as ONE block: header, package, imports, body.

    Rewriting (not appending) is the point. The first version partitioned on the
    first ")\n\n" it found, so every pass appended a SECOND import block and the
    file stopped parsing ("missing import path"). A normalizer that can corrupt
    the file it normalizes is not a normalizer (Rule 15).
    """
    if not os.path.exists(helper):
        return
    lines = open(helper).read().split('\n')
    # header = leading comment block; then the package line
    i = 0
    while i < len(lines) and lines[i].startswith('//'):
        i += 1
    while i < len(lines) and not lines[i].startswith('package '):
        i += 1
    pkgline = lines[i]
    # body = the first column-0 decl after the package line's blank tail
    j = i + 1
    while j < len(lines):
        ln = lines[j]
        if ln.startswith(('//', 'func ', 'const ', 'var ', 'type ')) or ln.startswith('/*'):
            break
        j += 1
    body = '\n'.join(lines[j:])
    imps = imports_needed(body)
    block = ''
    if imps:
        block = 'import (\n' + ''.join('\t"%s"\n' % x for x in imps) + ')\n\n'
    if re.search(r'\btestutil\.', body) and 'internal/testutil' not in block:
        block = block.replace(')\n\n', '\n\t"github.com/nickhuang99/gopi/internal/testutil"\n)\n\n')
    if re.search(r'\btaskverbs\.', body) and 'internal/taskverbs' not in block:
        block = block.replace(')\n\n', '\n\t"github.com/nickhuang99/gopi/internal/taskverbs"\n)\n\n')
    header = '\n'.join(lines[:i]).rstrip()
    open(helper, 'w').write(header + '\n\n' + pkgline + '\n\n' + block + body)


def main():
    pkg = sys.argv[1]
    helper = HELPERS[pkg]
    moved = set()
    for _ in range(30):
        r = run(['go', 'vet', './%s/' % pkg])
        errs = r.stdout + r.stderr
        # THREE spellings, all of them a missing symbol (a method's absence is
        # reported against its receiver, so matching only `undefined: X` misses
        # every missing METHOD — measured: commitInWorktree kept the package red
        # while the loop believed it was done).
        todo = sorted(set(
            re.findall(r'undefined: ([A-Za-z_][A-Za-z0-9_]*)', errs)
            + re.findall(r'\.([A-Za-z_][A-Za-z0-9_]*) undefined \(', errs)
            + re.findall(r'^\.([A-Za-z_][A-Za-z0-9_]*) undefined ', errs, re.M)))
        todo = [s for s in todo if s not in moved]
        if not todo:
            normalize_imports(helper)
            print('%s: vet clean' % pkg if not errs.strip() else
                  '%s: no undefined left; remaining:\n%s' % (pkg, errs[:800]))
            return
        nxt = todo[0]
        files = defining_files(nxt)
        if not files:
            print('%s: %s is defined NOWHERE at HEAD — skipped' % (pkg, nxt))
            moved.add(nxt)
            continue
        body = ''
        for f in files:
            o = run(['python3', 'scripts/extract_decls.py', 'GIT:' + f, '--sym', nxt])
            if o.stdout.strip():
                body = o.stdout
                break
        if not body.strip():
            print('%s: %s found in %s but not extracted — skipped' % (pkg, nxt, files[0]))
            moved.add(nxt)
            continue
        moved.add(nxt)
        print('%s: + %s (from %s)' % (pkg, nxt, files[0]))

        decls = []
        if os.path.exists(helper):
            s = open(helper).read()
            head, _, decls_part = s.partition(')\n\n')
            decls = [decls_part.strip()] if decls_part.strip() else []
        decls.append(body.strip())
        all_body = '\n\n'.join(decls)
        imps = imports_needed(all_body)
        header = (
            '// TEST HELPERS THAT OUTLIVED THE FINISH-CLEANUP MECHANISM (Rule 43).\n'
            '//\n'
            '// These were defined inside test files whose SUBJECT was the retired\n'
            '// worktree+branch close-out chain (task 01a18799, slice 3/4). The tests went\n'
            '// with the mechanism; the helpers below are general fixtures that LIVE tests\n'
            '// in this package still call. Each one is named by `go vet` as an undefined\n'
            '// symbol after the mechanism is deleted — nothing was moved speculatively.\n'
            '\n'
            'package %s\n\n' % PKG_DIR[pkg])
        block = ''
        if imps:
            block = 'import (\n' + ''.join('\t"%s"\n' % i for i in imps) + ')\n\n'
        open(helper, 'w').write(header + block + all_body + '\n')
    print('%s: gave up after 30 rounds' % pkg)


main()
