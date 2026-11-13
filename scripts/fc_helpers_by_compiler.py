#!/usr/bin/env python3
"""Rebuild the moved-helpers file by asking the compiler, one declaration at a time.

WHY THIS SHAPE (measured, after four failed attempts in the same session):

  * A whole-file "split into declarations then pick names" pass is wrong here.
    Two shapes break it: a `const (...)` GROUP whose first member collides takes
    its siblings with it, and a group separated by blank lines loses members.
    Both failures are SILENT — the file loses names while the script prints
    success.
  * Chasing one undefined name and emitting that ONE member is also wrong: a
    group member cannot be extracted alone, so the loop stalls on the same name
    forever.

So the rule this script follows: chase one undefined name, and if that name is a
member of a `const`/`var` group, extract THE WHOLE GROUP. Package-level
constants and vars may be unused in Go, so carrying a few extra members costs
nothing and loses nothing.

It never rewrites imports speculatively: a missing standard package is added when
the compiler names it as an undefined package (`undefined: sql`), by one table.
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

# an undefined IDENTIFIER that is really a missing std package
STD = {
    'sql': 'database/sql', 'exec': 'os/exec', 'filepath': 'path/filepath',
    'testing': 'testing', 'os': 'os', 'fmt': 'fmt', 'context': 'context',
    'time': 'time', 'strings': 'strings', 'strconv': 'strconv', 'sort': 'sort',
    'sync': 'sync', 'syscall': 'syscall', 'io': 'io', 'bytes': 'bytes',
    'regexp': 'regexp', 'errors': 'errors', 'json': 'encoding/json',
    'bufio': 'bufio', 'rand': 'math/rand',
}
THIRD = {
    'testutil': 'github.com/nick/gopi/internal/testutil',
    'taskverbs': 'github.com/nick/gopi/internal/taskverbs',
    'removalaudit': 'github.com/nick/gopi/internal/removalaudit',
}

HEADER = (
    '// TEST HELPERS AND TUNABLES THAT OUTLIVED THE FINISH-CLEANUP MECHANISM (Rule 43).\n'
    '//\n'
    '// These were defined inside test files whose SUBJECT was the retired\n'
    '// worktree+branch close-out chain (task 01a18799, slice 3/4). Those test files\n'
    '// went with the mechanism; what is below are the general fixtures, holders,\n'
    '// wait-budgets and probes that LIVE tests in this package still call by name.\n'
    '// Each one is named by `go vet` as an undefined symbol once the mechanism is\n'
    '// deleted, so nothing here was moved by guesswork and no test BODY came along.\n'
)


def sh(args):
    return subprocess.run(args, capture_output=True, text=True)


def decl_end(lines, i):
    """Last line of the top-level declaration that starts at line i."""
    if lines[i].rstrip().endswith(('{', '(')):
        close = '}' if lines[i].rstrip().endswith('{') else ')'
        depth = 0
        for k in range(i, len(lines)):
            depth += lines[k].count('{') + lines[k].count('(')
            depth -= lines[k].count('}') + lines[k].count(')')
            if depth <= 0 and k > i:
                return k
        return len(lines) - 1
    depth = 0
    for k in range(i, len(lines)):
        depth += lines[k].count('{') + lines[k].count('(')
        depth -= lines[k].count('}') + lines[k].count(')')
        if depth <= 0 and k > i:
            return k
    return i


def extract(path, sym):
    """The declaration introducing sym — the WHOLE group when sym is a group member."""
    src = sh(['git', 'show', 'HEAD:' + path]).stdout
    lines = src.split('\n')
    for i, ln in enumerate(lines):
        if not re.match(r'^(func|const|var|type)\b', ln):
            continue
        end = decl_end(lines, i)
        block = lines[i:end + 1]
        text = '\n'.join(block)
        if re.match(r'^func \([^)]*\) %s\(' % re.escape(sym), ln) or \
                re.match(r'^func %s\(' % re.escape(sym), ln):
            pass
        elif re.match(r'^(const|var|type) %s\b' % re.escape(sym), ln):
            pass
        elif re.match(r'^(const|var) \($', ln) and re.search(
                r'^\s+%s\s*=' % re.escape(sym), text, re.M):
            pass
        else:
            continue
        # doc comment directly above
        s = i
        j = i - 1
        while j >= 0 and lines[j].startswith('//'):
            s = j
            j -= 1
        return '\n'.join(lines[s:end + 1])
    return None


def main():
    pkg = sys.argv[1]
    rounds = int(sys.argv[2]) if len(sys.argv) > 2 else 60
    helper = os.path.join(pkg, 'finish_cleanup_moved_helpers_test.go')
    retired = [f for f in sh(['git', 'ls-tree', '-r', '--name-only', 'HEAD', '--', pkg]
                             ).stdout.split()
               if f.endswith('_test.go') and not os.path.exists(f)]
    decls = {}           # first-name of the declaration -> text
    extra_imports = set()

    def render():
        body = '\n\n'.join(decls[k] for k in sorted(decls))
        lines = [HEADER, '\npackage %s\n' % PKG_NAME[pkg], '\nimport (\n']
        for p in sorted(extra_imports):
            lines.append('\t"%s"\n' % p)
        lines.append(')\n\n')
        open(helper, 'w').write(''.join(lines) + body + '\n')
        sh(['gofmt', '-w', helper])

    render()
    for rnd in range(rounds):
        r = sh(['go', 'vet', './%s/' % pkg])
        errs = r.stdout + r.stderr
        und = list(dict.fromkeys(
            re.findall(r'undefined: ([A-Za-z_][A-Za-z0-9_]*)', errs)
            # a missing METHOD is reported against its receiver, not as an
            # undefined identifier: `f.commitInWorktree undefined (type *T ...)`.
            # Matching only the first form is how a loop claims to be done while
            # the package is still red (measured in this session).
            + re.findall(r'\.([A-Za-z_][A-Za-z0-9_]*) undefined \(', errs)))
        if not und:
            print('%s: %d declarations, no undefined left' % (pkg, len(decls)))
            if errs.strip():
                print(errs.strip()[:500])
            return
        sym = und[0]
        if sym in STD or sym in THIRD:
            extra_imports.add(STD.get(sym) or THIRD[sym])
            render()
            continue
        done = False
        for f in retired:
            d = extract(f, sym)
            if d:
                decls[sym] = d
                done = True
                break
        if not done:
            print('%s: %s is defined in NO retired file — cannot move it' % (pkg, sym))
            return
        render()
    print('%s: gave up after %d rounds' % (pkg, rounds))


main()
