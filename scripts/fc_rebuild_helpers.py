#!/usr/bin/env python3
"""Rebuild the moved-helpers file, taking ONE declaration at a time.

The earlier versions tried to split a whole file into declarations up front and
then pick names out of that pool. That is where they broke: a const GROUP whose
first name collided took its siblings with it, and a blank-line gap inside a
group produced an empty entry, so the file lost names while the script reported
success. This version does the opposite — it asks `go vet` for ONE missing name,
finds that name's own definition line in the retired files at HEAD, and cuts from
the line above it to the line where its braces balance. No pool, no splitting,
no silent loss.

Usage: fc_rebuild_helpers.py <pkgdir> [max-rounds]
"""
import os
import re
import subprocess
import sys

PKG_NAME = {
    'internal/tools': 'tools',
    'cmd/agent': 'main',
    'internal/agent': 'agent',
    'internal/standards': 'standardgate',
}

HEADER = (
    '// TEST HELPERS AND TUNABLES THAT OUTLIVED THE FINISH-CLEANUP MECHANISM (Rule 43).\n'
    '//\n'
    '// These were defined inside test files whose SUBJECT was the retired\n'
    '// worktree+branch close-out chain (task 01a18799, slice 3/4). Those test files\n'
    '// went with the mechanism; what is below are the general fixtures, holders and\n'
    '// wait-budgets that LIVE tests in this package still call by name. Each one is\n'
    '// named by `go vet` as an undefined symbol once the mechanism is deleted, so\n'
    '// nothing here was moved by guesswork, and no test BODY was carried over.\n'
)

IMPORTS = [
    ('context', r'\bcontext\.'),
    ('database/sql', r'\bsql\.'),
    ('fmt', r'\bfmt\.'),
    ('os', r'\bos\.'),
    ('os/exec', r'\bexec\.'),
    ('path/filepath', r'\bfilepath\.'),
    ('strconv', r'\bstrconv\.'),
    ('strings', r'\bstrings\.'),
    ('sync', r'\bsync\.'),
    ('syscall', r'\bsyscall\.'),
    ('io', r'\bio\.'),
    ('sort', r'\bsort\.'),
    ('bytes', r'\bbytes\.'),
    ('regexp', r'\bregexp\.'),
    ('errors', r'\berrors\.'),
    ('testing', r'\btesting\.'),
    ('time', r'\btime\.'),
]
THIRD = [
    ('github.com/nickhuang99/gopi/internal/taskverbs', r'\btaskverbs\.'),
    ('github.com/nickhuang99/gopi/internal/testutil', r'\btestutil\.'),
]


def sh(args):
    return subprocess.run(args, capture_output=True, text=True)


def missing(pkg):
    r = sh(['go', 'vet', './%s/' % pkg])
    errs = r.stdout + r.stderr
    out = []
    for pat in (r'undefined: ([A-Za-z_][A-Za-z0-9_]*)',
                r'\.([A-Za-z_][A-Za-z0-9_]*) undefined \(',
                r'^\.([A-Za-z_][A-Za-z0-9_]*) undefined '):
        out += re.findall(pat, errs, re.M)
    return list(dict.fromkeys(out)), errs


def retired_files(pkg):
    """Test files at HEAD that are GONE from the working tree (the mechanism's own)."""
    files = sh(['git', 'ls-tree', '-r', '--name-only', 'HEAD', '--', pkg]).stdout.split()
    return [f for f in files if f.endswith('_test.go') and not os.path.exists(f)]


DECL_RE = r'^(func %s\(|func \([^)]*\) %s\(|const %s\b|var %s\b|type %s\b|%s +=|%s +[A-Za-z_])'


def find_decl(files, sym):
    """(path, declaration text) for sym's own top-level declaration, or (None, None)."""
    pat = re.escape(sym)
    for f in files:
        src = sh(['git', 'show', 'HEAD:' + f]).stdout
        lines = src.split('\n')
        for i, ln in enumerate(lines):
            if not re.match(r'^(func|const|var|type)\b', ln):
                continue
            # does THIS declaration introduce sym?
            head = ln
            if head.startswith('func %s(' % sym) or re.match(r'^func \([^)]*\) %s\(' % pat, head):
                pass
            elif re.match(r'^(const|var|type) %s\b' % pat, head):
                pass
            elif re.match(r'^(const|var) \($', head):
                # a group: look for a member line naming sym
                k = i + 1
                found = None
                while k < len(lines) and lines[k].rstrip() != ')':
                    m = re.match(r'^\s+(%s)\s*(=|$)' % pat, lines[k])
                    if m:
                        found = k
                        break
                    k += 1
                if found is None:
                    continue
                # emit that MEMBER as its own single declaration
                doc = []
                j = found - 1
                while j > i and lines[j].strip().startswith('//'):
                    doc.insert(0, lines[j])
                    j -= 1
                kw = 'const' if head.startswith('const') else 'var'
                return f, ('\n'.join(doc) + '\n' if doc else '') + kw + ' ' + lines[found].strip()
            else:
                continue
            # doc comment directly above
            s = i
            j = i - 1
            while j >= 0 and lines[j].startswith('//'):
                s = j
                j -= 1
            # cut to a balanced close
            text = '\n'.join(lines[s:])
            depth = 0
            end = s
            for k in range(i, len(lines)):
                depth += lines[k].count('{') + lines[k].count('(')
                depth -= lines[k].count('}') + lines[k].count(')')
                if depth <= 0 and k > i:
                    end = k
                    break
                if depth == 0 and k == i and not lines[i].rstrip().endswith(('{', '(')):
                    end = k
                    break
            return f, '\n'.join(lines[s:end + 1])
    return None, None


def main():
    pkg = sys.argv[1]
    rounds = int(sys.argv[2]) if len(sys.argv) > 2 else 80
    helper = os.path.join(pkg, 'finish_cleanup_moved_helpers_test.go')
    files = retired_files(pkg)
    have = []            # declaration texts, in the order they were needed
    names = set()
    for rnd in range(rounds):
        todo, errs = missing(pkg)
        todo = [s for s in todo if s not in names]
        if not todo:
            print('%s: nothing left to move (%d declarations)' % (pkg, len(have)))
            if errs.strip():
                print(errs.strip()[:600])
            break
        progressed = False
        for sym in todo:
            path, decl = find_decl(files, sym)
            if not decl:
                print('%s: %s is defined in NO retired file — left alone' % (pkg, sym))
                names.add(sym)
                continue
            names.add(sym)
            have.append(decl)
            progressed = True
        if not progressed:
            break
        body = '\n\n'.join(have)
        std = [p for p, use in IMPORTS if re.search(use, body)]
        third = [p for p, use in THIRD if re.search(use, body)]
        blk = 'import (\n' + ''.join('\t"%s"\n' % p for p in std)
        if third:
            blk += '\n' + ''.join('\t"%s"\n' % p for p in third)
        blk += ')\n\n'
        open(helper, 'w').write(HEADER + '\npackage %s\n\n' % PKG_NAME[pkg] + blk + body + '\n')
        sh(['gofmt', '-w', helper])
        print('%s: round %d, %d declaration(s)' % (pkg, rnd, len(have)))


main()
