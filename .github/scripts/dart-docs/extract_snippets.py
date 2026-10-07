#!/usr/bin/env python3
"""Write every dart code block of the Dart client docs to its own Dart file.

Usage: extract_snippets.py DOCS_DIR OUT_DIR

Every .md and .mdx file under DOCS_DIR is read, subdirectories included.

A block with its own import lines is written as it is, because it is meant
to be read as a whole file. A block without imports gets PRELUDE in front,
which imports the runtime packages and the generated orders client the docs
are written against.

Riverpod and Grove stay out of the prelude on purpose. A block that uses
them states its own imports, the same way a reader's file would have to,
which keeps the prelude to the packages every page uses.

A block whose title sits under lib/src/ is generated output pasted into the
page. It is skipped here: the generator's own gate test compiles it, and
re-declaring generated names in a snippet would test nothing.

The extractor fails closed. A fence counts as dart when its opening line is
a run of backticks or tildes, optionally indented, followed by "dart" in any
case. An indented block (inside a list, a Steps or a Tab) is dedented by its
fence's indent. After reading a file the extractor counts those opening
lines and compares the count with the blocks it parsed, so a fence it could
not parse, such as one that is never closed, is an error and not a sample
that quietly goes unchecked. Two files that would write the same output name
(a-b.mdx and a_b.mdx) are an error too.
"""

import os
import re
import sys

OPENER = re.compile(r"^([ \t]*)(`{3,}|~{3,})[ \t]*dart\b(.*)$", re.I)
GENERATED = re.compile(r"title=\"[^\"]*lib/src/")
IMPORT = re.compile(r"^import ", re.M)

PRELUDE = """import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

"""


def dedent(line, indent):
    """Remove up to len(indent) leading blanks from line."""
    cut = 0
    while cut < len(indent) and cut < len(line) and line[cut] in " \t":
        cut += 1
    return line[cut:]


def parse(text):
    """Return (looks_like_dart, blocks) for one file.

    looks_like_dart counts every opening line that matches OPENER. blocks
    holds (meta, body, body_line) for each one that was closed.
    """
    lines = text.replace("\r\n", "\n").split("\n")
    looks = sum(1 for line in lines if OPENER.match(line))
    blocks = []
    i = 0
    while i < len(lines):
        opener = OPENER.match(lines[i])
        if not opener:
            i += 1
            continue
        indent, marker, meta = opener.group(1), opener.group(2), opener.group(3)
        closer = re.compile(r"^[ \t]*" + re.escape(marker[0]) + "{" + str(len(marker)) + r",}[ \t]*$")
        j = i + 1
        while j < len(lines) and not closer.match(lines[j]):
            j += 1
        if j >= len(lines):
            i += 1
            continue
        body = "\n".join(dedent(line, indent) for line in lines[i + 1 : j]) + "\n"
        blocks.append((meta, body, i + 2))
        i = j + 1
    return looks, blocks


def doc_files(docs):
    for base, dirs, files in os.walk(docs):
        dirs.sort()
        for name in sorted(files):
            if name.endswith((".md", ".mdx")):
                yield os.path.join(base, name)


def main(argv):
    if len(argv) != 3:
        sys.exit("usage: extract_snippets.py DOCS_DIR OUT_DIR")
    docs, out = argv[1], argv[2]
    os.makedirs(out, exist_ok=True)
    written = skipped = 0
    used = {}
    for path in doc_files(docs):
        rel = os.path.relpath(path, docs)
        stem = os.path.splitext(rel)[0]
        page = re.sub(r"[^0-9A-Za-z]+", "_", stem).strip("_").lower() or "page"
        with open(path, encoding="utf-8") as f:
            text = f.read()
        looks, blocks = parse(text)
        if looks != len(blocks):
            sys.exit(
                f"extract_snippets: {rel} has {looks} dart fences but only {len(blocks)} could be "
                "read; an unclosed fence is the usual cause"
            )
        for index, (meta, body, line) in enumerate(blocks, start=1):
            if GENERATED.search(meta):
                skipped += 1
                continue
            name = f"{page}_{index:02d}.dart"
            if name in used:
                sys.exit(f"extract_snippets: {rel} and {used[name]} both write {name}; rename one page")
            used[name] = rel
            whole = IMPORT.search(body) is not None
            header = f"// {rel}, block {index}, body starts at line {line}"
            if not whole:
                header += f"; the header and prelude add {PRELUDE.count(chr(10)) + 1} lines"
            with open(os.path.join(out, name), "w", encoding="utf-8") as f:
                f.write(header + "\n" + ("" if whole else PRELUDE) + body)
            written += 1
    if written == 0:
        sys.exit(f"extract_snippets: no dart blocks found under {docs}")
    print(f"extract_snippets: wrote {written} blocks, skipped {skipped} generated excerpts")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
