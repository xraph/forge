#!/usr/bin/env python3
"""Prepare a dart-packages/ pubspec for CI.

Packages under dart-packages/ depend on each other with `path:` and, for
forge_client_grove, on grove's crdt-dart, which is not in this repository.
CI and the release need four things the committed pubspecs cannot give them:

1. A pubspec_overrides.yaml that points every local dependency at a directory
   that exists on the runner. `--override NAME=DIR` names one explicitly,
   which is how grove_crdt reaches the grove checkout.
2. For `dart pub publish --dry-run`, a pubspec pub's validator accepts.
   `--hosted` rewrites each local dependency to `^<its version>`, because the
   validator rejects every path and git dependency outright (exit 65), and
   leaves the override in place so resolution still finds it. It also deletes
   `publish_to: none`, because pub refuses a private package before it
   validates anything else.
3. The commit grove is checked out at. `--print-ref NAME` prints the `ref:`
   of NAME's git dependency and writes nothing, so the workflow reads the pin
   instead of repeating it.
4. For a release, one version for every package. `--version X.Y.Z` sets the
   package's `version:`, and with `--hosted` every path dependency becomes
   `^X.Y.Z` instead of `^<its committed version>`, because the packages
   publish in lockstep at the forge tag's version. A git dependency, which
   is not a forge package, keeps its own version. `--changelog` also gives
   CHANGELOG.md a `## X.Y.Z` section pointing at the forge release, unless
   it already has one, because pub warns (and the dry run exits 65) when the
   changelog does not mention the version being published.

Only the shape the packages use is understood: a dependency written as a
mapping under `dependencies:` or `dev_dependencies:`, indented two spaces,
with its keys indented four and a git dependency's keys indented six.
Anything else is left as it is.

    dart_ci_pubspec.py PACKAGE_DIR [--override NAME=DIR ...] [--hosted] [--version X.Y.Z [--changelog]]
    dart_ci_pubspec.py PACKAGE_DIR --print-ref NAME
"""

import argparse
import os
import re
import sys

SECTIONS = ("dependencies:", "dev_dependencies:")
ENTRY = re.compile(r"^  ([a-z][a-z0-9_]*):\s*$")
KEY = re.compile(r"^    ([a-z_]+):\s*(.*?)\s*$")
GIT_REF = re.compile(r"^      ref:\s*['\"]?([^'\"\s]+)['\"]?\s*$")
VERSION = re.compile(r"^version:\s*(\S+)\s*$", re.M)
PRIVATE = re.compile(r"""^publish_to:\s*(none|'none'|"none")\s*$""")
VERSION_LINE = re.compile(r"^version:\s*\S+\s*$")
# No leading zeros: pub reads 01.2.3 as 1.2.3, so the stamped version and the
# one pub.dev is asked about would disagree.
SEMVER = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$")
RELEASES = "https://github.com/xraph/forge/releases/tag/v"


def local_dependencies(lines):
    """Yield (name, start, end, source, value) for each mapping dependency."""
    section = None
    i = 0
    while i < len(lines):
        line = lines[i]
        if not line.startswith(" ") and line.strip():
            section = line.strip() if line.strip() in SECTIONS else None
            i += 1
            continue
        match = ENTRY.match(line) if section else None
        if not match:
            i += 1
            continue
        name, start = match.group(1), i
        i += 1
        keys = {}
        end = i
        while i < len(lines) and (lines[i].startswith("    ") or not lines[i].strip()):
            key = KEY.match(lines[i])
            if key:
                keys[key.group(1)] = key.group(2)
            if lines[i].strip():
                end = i + 1
            i += 1
        if "path" in keys:
            yield name, start, end, "path", keys["path"]
        elif "git" in keys:
            yield name, start, end, "git", keys["git"]


def read_version(directory):
    with open(os.path.join(directory, "pubspec.yaml"), encoding="utf-8") as f:
        match = VERSION.search(f.read())
    if not match:
        sys.exit(f"dart_ci_pubspec: no version in {directory}/pubspec.yaml")
    return match.group(1)


def stamp_changelog(directory, version):
    """Give CHANGELOG.md a section for version unless it already has one."""
    path = os.path.join(directory, "CHANGELOG.md")
    if not os.path.isfile(path):
        sys.exit(f"dart_ci_pubspec: no CHANGELOG.md in {directory}")
    with open(path, encoding="utf-8") as f:
        text = f.read()
    # `# X.Y.Z`, `### vX.Y.Z`, `## [X.Y.Z] - date`, and release-please's
    # `## [X.Y.Z](compare-url)`, but not `## X.Y.Z-rc.1`.
    heading = re.compile(rf"^#{{1,3}}\s+\[?v?{re.escape(version)}\]?(?=[\s(]|$)", re.M)
    if heading.search(text):
        return
    entry = (
        f"## {version}\n\n"
        f"- Released with forge v{version}, which every forge_client package"
        f" follows in lockstep. See the [forge release notes]({RELEASES}{version}).\n\n"
    )
    with open(path, "w", encoding="utf-8") as f:
        f.write(entry + text)


def print_ref(lines, wanted):
    for name, start, end, source, _ in local_dependencies(lines):
        if name != wanted:
            continue
        if source != "git":
            sys.exit(f"dart_ci_pubspec: {name} is not a git dependency")
        for line in lines[start:end]:
            ref = GIT_REF.match(line)
            if ref:
                print(ref.group(1))
                return 0
        sys.exit(f"dart_ci_pubspec: {name} has no ref:")
    sys.exit(f"dart_ci_pubspec: {wanted} is not a git dependency")


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("package")
    parser.add_argument("--override", action="append", default=[], metavar="NAME=DIR")
    parser.add_argument("--hosted", action="store_true")
    parser.add_argument("--print-ref", metavar="NAME")
    parser.add_argument("--version", metavar="X.Y.Z")
    parser.add_argument("--changelog", action="store_true")
    args = parser.parse_args(argv)

    if args.changelog and not args.version:
        sys.exit("dart_ci_pubspec: --changelog needs --version")

    if args.version is not None and not SEMVER.match(args.version):
        sys.exit(f"dart_ci_pubspec: --version wants X.Y.Z with no leading v, got {args.version!r}")

    package = os.path.abspath(args.package)
    pubspec_path = os.path.join(package, "pubspec.yaml")
    with open(pubspec_path, encoding="utf-8") as f:
        lines = f.read().split("\n")

    if args.print_ref:
        return print_ref(lines, args.print_ref)

    explicit = {}
    for item in args.override:
        name, _, directory = item.partition("=")
        if not name or not directory:
            sys.exit(f"dart_ci_pubspec: --override wants NAME=DIR, got {item!r}")
        explicit[name] = os.path.abspath(directory)

    overrides = dict(explicit)
    replacements = []
    for name, start, end, source, value in local_dependencies(lines):
        if name not in overrides:
            if source == "git":
                sys.exit(f"dart_ci_pubspec: {name} is a git dependency; pass --override {name}=DIR")
            overrides[name] = os.path.normpath(os.path.join(package, value))
        replacements.append((name, start, end, source))

    for name, directory in overrides.items():
        if not os.path.isfile(os.path.join(directory, "pubspec.yaml")):
            sys.exit(f"dart_ci_pubspec: {name} -> {directory} has no pubspec.yaml")

    if args.hosted:
        for name, start, end, source in reversed(replacements):
            if args.version and source == "path":
                version = args.version
            else:
                version = read_version(overrides[name])
            lines[start:end] = [f"  {name}: ^{version}"]
        lines = [line for line in lines if not PRIVATE.match(line)]

    if args.version:
        stamped = [i for i, line in enumerate(lines) if VERSION_LINE.match(line)]
        if not stamped:
            sys.exit(f"dart_ci_pubspec: no version in {pubspec_path}")
        lines[stamped[0]] = f"version: {args.version}"

    if args.hosted or args.version:
        with open(pubspec_path, "w", encoding="utf-8") as f:
            f.write("\n".join(lines))

    if args.changelog:
        stamp_changelog(package, args.version)

    with open(os.path.join(package, "pubspec_overrides.yaml"), "w", encoding="utf-8") as f:
        f.write("dependency_overrides:\n")
        for name in sorted(overrides):
            f.write(f"  {name}:\n    path: {overrides[name]}\n")

    for name in sorted(overrides):
        print(f"{name} -> {overrides[name]}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
