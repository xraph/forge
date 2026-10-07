#!/usr/bin/env python3
"""Check that every generated excerpt in the Dart client docs is real output.

Usage: check_excerpts.py DOCS_DIR CLIENT_DIR [CLIENT_DIR ...]

A dart block whose title sits under lib/src/ is a piece of generated output
pasted into a page. extract_snippets.py does not compile it, because the
generator's own tests do that. This script checks the other half: that the
page still says what the generator writes.

For each such block it takes the file named by the title, looks for it under
every CLIENT_DIR, and requires each of the block's lines to appear in that
file, with whitespace collapsed. It passes when one client has the file and
carries all the lines. Lines may be left out of an excerpt and shown in a
different order, but not changed. Blank lines and "//" comments (not "///" doc
comments) are the author's elisions and are not checked. A title with a "*" in
it, such as lib/src/bindings/*.dart, stands for every file it matches in one
client, and an excerpt of it may draw lines from any of them.

Exits 0 when every excerpt matches, 1 when any does not, 2 on bad usage.
"""

import glob
import os
import re
import sys

from extract_snippets import GENERATED, doc_files, parse

TITLE = re.compile(r'title="([^"]*)"')


def collapse(line):
    return " ".join(line.split())


def checked_lines(body):
    """The lines of an excerpt that must exist in the generated file."""
    out = []
    for line in body.split("\n"):
        text = collapse(line)
        if not text:
            continue
        if text.startswith("//") and not text.startswith("///"):
            continue
        out.append(text)
    return out


def missing_line(wanted, generated):
    """Return the first wanted line the generated text lacks, or None."""
    have = {collapse(line) for line in generated.split("\n")}
    for text in wanted:
        if text not in have:
            return text
    return None


def generated_texts(title, clients):
    """One text per client that has the file, or the files a glob matches."""
    texts = []
    for client in clients:
        names = sorted(glob.glob(os.path.join(client, title)))
        parts = []
        for name in names:
            if os.path.isfile(name):
                with open(name, encoding="utf-8") as f:
                    parts.append(f.read())
        if parts:
            texts.append("\n".join(parts))
    return texts


def main(argv):
    if len(argv) < 3:
        print(__doc__.strip().split("\n")[2], file=sys.stderr)
        return 2
    docs, clients = argv[1], argv[2:]
    checked = 0
    failures = []
    for path in doc_files(docs):
        rel = os.path.relpath(path, docs)
        with open(path, encoding="utf-8") as f:
            _, blocks = parse(f.read())
        for index, (meta, body, line) in enumerate(blocks, start=1):
            if not GENERATED.search(meta):
                continue
            title = TITLE.search(meta).group(1)
            where = f"{rel}, block {index} (line {line}), {title}"
            candidates = generated_texts(title, clients)
            if not candidates:
                failures.append(f"{where}: no generated file matches that title in any client")
                continue
            wanted = checked_lines(body)
            misses = [missing_line(wanted, text) for text in candidates]
            checked += 1
            if all(miss is not None for miss in misses):
                failures.append(f"{where}: not in the generated file; first line that is missing: {misses[0]!r}")
    for failure in failures:
        print(f"check_excerpts: {failure}", file=sys.stderr)
    if failures:
        return 1
    print(f"check_excerpts: {checked} generated excerpts match the generated files")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
