#!/usr/bin/env python3
"""Write every ```dart block of the Dart client docs to its own Dart file.

Usage: extract_snippets.py DOCS_DIR OUT_DIR

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
"""

import os
import re
import sys

FENCE = re.compile(r"^```dart([ \t][^\n]*)?\n(.*?)^```[ \t]*$", re.M | re.S)
GENERATED = re.compile(r"title=\"[^\"]*lib/src/")
IMPORT = re.compile(r"^import ", re.M)

PRELUDE = """import 'dart:async';

import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

"""


def main(argv):
    if len(argv) != 3:
        sys.exit("usage: extract_snippets.py DOCS_DIR OUT_DIR")
    docs, out = argv[1], argv[2]
    os.makedirs(out, exist_ok=True)
    written = skipped = 0
    for name in sorted(os.listdir(docs)):
        if not name.endswith(".mdx"):
            continue
        page = name[: -len(".mdx")].replace("-", "_")
        with open(os.path.join(docs, name), encoding="utf-8") as f:
            text = f.read()
        for index, match in enumerate(FENCE.finditer(text), start=1):
            meta, body = match.group(1) or "", match.group(2)
            if GENERATED.search(meta):
                skipped += 1
                continue
            line = text.count("\n", 0, match.start()) + 2
            whole = IMPORT.search(body) is not None
            header = f"// {name}, block {index}, body starts at line {line}"
            if not whole:
                header += f"; the prelude adds {PRELUDE.count(chr(10))} lines"
            with open(os.path.join(out, f"{page}_{index:02d}.dart"), "w", encoding="utf-8") as f:
                f.write(header + "\n" + ("" if whole else PRELUDE) + body)
            written += 1
    if written == 0:
        sys.exit(f"extract_snippets: no dart blocks found under {docs}")
    print(f"extract_snippets: wrote {written} blocks, skipped {skipped} generated excerpts")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
