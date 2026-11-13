Resolve gopi's machine-owned temp root first: it is the directory the `TMPDIR`
environment variable names (default `<home>/workspace/_tmp`, published by
`gopi` for itself and every child; it is never `/tmp`, which sits on the `/`
partition). Do NOT create a `scratch/` directory inside the repository
checkout — that is not a convention, and a file written into a checkout is
one `git add` away from being committed. Then create a file at
`$TMPDIR/gopi-special-chars/payload.md` with EXACTLY this content (no
modifications, no additions):

SPECIAL & <chars> "quoted" | piped $(dollar) `backtick` *star* $HOME
Line2: quotes "double" 'single' & ampersand <angle> >redirect <<heredoc

Then read `$TMPDIR/gopi-special-chars/payload.md` back and reply with the
file's content.
