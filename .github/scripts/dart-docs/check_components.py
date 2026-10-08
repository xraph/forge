#!/usr/bin/env python3
"""Check that MDX pages use only components the website renders.

Usage: check_components.py FILE...

The website maps Callout, Cards, Card, Steps, Step, Tabs and Tab. Any other
capitalised tag degrades to raw text. It also renders Tabs as a code group, so
a Tab that holds prose comes out as a raw code block: a Tab may hold one
fenced code block and nothing else.

Tags inside code fences and inline code are examples, not components, and are
ignored. Exits 1, printing one line per problem, when a page breaks either rule.
"""

import re
import sys

ALLOWED = {"Callout", "Cards", "Card", "Tabs", "Tab", "Steps", "Step"}

FENCE = re.compile(r"^[ \t]*(`{3,}|~{3,})[^\n]*\n.*?^[ \t]*\1[`~]*[ \t]*$", re.M | re.S)
INLINE_CODE = re.compile(r"`[^`\n]*`")
TAG = re.compile(r"<([A-Z][A-Za-z0-9]*)")
TAB = re.compile(r"<Tab(?:\s[^>]*)?>(.*?)</Tab>", re.S)
HOLE = "\x00FENCE\x00"


def problems(path, text):
    # A fenced block becomes one placeholder, so what is left is the prose and
    # the tags, and a Tab is well formed when it holds exactly one placeholder.
    prose = FENCE.sub(HOLE, text)
    found = []
    stripped = INLINE_CODE.sub("", prose)
    for name in sorted(set(TAG.findall(stripped)) - ALLOWED):
        found.append(f"{path}: <{name}> is not in the website allowlist")
    for inner in TAB.findall(prose):
        if inner.strip() != HOLE:
            found.append(f"{path}: a <Tab> holds something other than one code block")
    return found


def main(argv=None):
    paths = sys.argv[1:] if argv is None else argv
    if not paths:
        print("usage: check_components.py FILE...", file=sys.stderr)
        return 2
    bad = []
    for path in paths:
        with open(path, encoding="utf-8") as f:
            bad += problems(path, f.read())
    if bad:
        print("\n".join(bad))
        return 1
    print(f"components ok ({len(paths)} pages)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
