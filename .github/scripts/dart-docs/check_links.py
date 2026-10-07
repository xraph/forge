#!/usr/bin/env python3
"""Check that every /docs/... link in the given MDX files resolves.

Usage: check_links.py DOCS_CONTENT_ROOT [--pending PATH,...] FILE...

A link resolves when DOCS_CONTENT_ROOT holds <path>.mdx or <path>/index.mdx
and, for a link with a #fragment, that page has a heading whose slug matches.
Headings inside code fences do not count.

--pending names pages that are planned but not written yet, as /docs/...
paths. A link to one of them is reported as pending and does not fail the
run, so a section can be written a page at a time.

Exits 1, printing one line per broken link, when anything does not resolve.
"""

import argparse
import os
import re
import sys

LINK = re.compile(r"\]\((/docs/[^)\s]+)\)|href=\"(/docs/[^\"]+)\"")
HEADING = re.compile(r"^#{1,6}\s+(.*?)\s*$", re.M)
FENCE = re.compile(r"^```.*?^```", re.M | re.S)


def slug(text):
    text = text.replace("`", "").strip().lower()
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


def page_file(root, path):
    rel = path[len("/docs/"):].strip("/")
    for candidate in (os.path.join(root, rel + ".mdx"), os.path.join(root, rel, "index.mdx")):
        if os.path.isfile(candidate):
            return candidate
    return None


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("root")
    parser.add_argument("--pending", default="")
    parser.add_argument("files", nargs="+")
    args = parser.parse_args(argv)
    pending = {p.rstrip("/") for p in args.pending.split(",") if p}

    broken = []
    for name in args.files:
        with open(name, encoding="utf-8") as f:
            body = FENCE.sub("", f.read())
        for match in LINK.finditer(body):
            target = match.group(1) or match.group(2)
            path, _, fragment = target.partition("#")
            found = page_file(args.root, path)
            if not found:
                if path.rstrip("/") in pending:
                    print(f"{name}: {target} (pending)")
                else:
                    broken.append(f"{name}: {target} (no page)")
                continue
            if fragment:
                with open(found, encoding="utf-8") as f:
                    headings = {slug(h) for h in HEADING.findall(FENCE.sub("", f.read()))}
                if fragment not in headings:
                    broken.append(f"{name}: {target} (no heading #{fragment})")
    for line in broken:
        print(line)
    return 1 if broken else 0


if __name__ == "__main__":
    sys.exit(main())
