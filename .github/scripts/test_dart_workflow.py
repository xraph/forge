"""Pins the properties of .github/workflows/dart-packages.yml that a green run
cannot show by itself.

Which changes run it. GitHub decides from the `paths:` filters alone, so a
filter that misses the IR means a PR that only touches the Go side never runs
the Dart gate test, and go.yml cannot catch it because it installs no Flutter.

Which toolchain and caches it uses. Flutter must come from
dart-packages/.fvmrc, and the pub cache must key on pubspec.yaml, because
flutter-action's own pub cache keys on pubspec.lock, which packages do not
commit, so its key would never change.

What the jobs need to not skip or fail for the wrong reason: grove at the
commit forge_client_grove pins, an fvm stand-in that understands `fvm exec`,
and the TypeScript toolchain go.yml installs for the codec parity test.

Run: python3 -m unittest discover -s .github/scripts -p 'test_*.py'
"""

import os
import re
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
WORKFLOW = os.path.join(HERE, "..", "workflows", "dart-packages.yml")
GO_WORKFLOW = os.path.join(HERE, "..", "workflows", "go.yml")


def text():
    with open(WORKFLOW, encoding="utf-8") as f:
        return f.read()


def job(name):
    """Return the lines of jobs.<name>, up to the next job."""
    lines = text().split("\n")
    start = lines.index("jobs:")
    out, inside = [], False
    for line in lines[start + 1:]:
        if re.match(rf"^  {re.escape(name)}:\s*$", line):
            inside = True
            continue
        if inside and re.match(r"^  \S", line):
            break
        if inside:
            out.append(line)
    if not out:
        raise AssertionError(f"no job {name!r} in dart-packages.yml")
    return "\n".join(out)


def filters(event):
    """Return the paths: globs listed under on.<event> in the workflow."""
    lines = text().split("\n")
    globs, in_event, in_paths = [], False, False
    for line in lines:
        if re.match(rf"^  {event}:\s*$", line):
            in_event, in_paths = True, False
            continue
        if in_event and re.match(r"^  \S", line):
            break
        if in_event and re.match(r"^    paths:\s*$", line):
            in_paths = True
            continue
        if in_paths:
            item = re.match(r"^      - '([^']+)'\s*$", line)
            if item:
                globs.append(item.group(1))
            elif line.strip():
                in_paths = False
    return globs


def to_regex(glob):
    """GitHub's filter globs: ** crosses directories, * does not."""
    out, i = "", 0
    while i < len(glob):
        if glob.startswith("**", i):
            out += ".*"
            i += 2
        elif glob[i] == "*":
            out += "[^/]*"
            i += 1
        else:
            out += re.escape(glob[i])
            i += 1
    return re.compile(out + "$")


def runs(path, event):
    return any(to_regex(g).match(path) for g in filters(event))


MUST_RUN = [
    "dart-packages/.fvmrc",
    "dart-packages/forge_client/lib/src/cache.dart",
    "dart-packages/forge_client_offline/pubspec.yaml",
    "internal/client/ir.go",
    "internal/client/spec_parser.go",
    "internal/client/generators/interface.go",
    "internal/client/generators/dart/generator.go",
    "internal/client/generators/dart/testdata/fixtures/orders.json",
    "cmd/forge/plugins/client.go",
    "packages/client-fixtures/snapshot/orders.json",
    "docs/content/docs/dart-client/sync.mdx",
    # forge_client's streaming_kinds_test pins the Dart frame kinds to this file.
    "extensions/streaming/internal/streaming.go",
    ".github/scripts/dart_ci_pubspec.py",
    ".github/scripts/test_dart_ci_pubspec.py",
    ".github/scripts/test_dart_workflow.py",
    ".github/scripts/dart-docs/check_snippets.sh",
    ".github/scripts/dart-docs/catalog.openapi.json",
    ".github/workflows/dart-packages.yml",
]

MUST_NOT_RUN = [
    "internal/client/generators/typescript/generator.go",
    "internal/client/README.md",
    "docs/content/docs/web-client/index.mdx",
    "packages/client-core/src/cache.ts",
    "extensions/hls/extension.go",
    "extensions/streaming/extension.go",
]

# The stand-in's one line of logic: `fvm exec CMD` runs CMD, so drop the exec.
SHIM_EXEC = 'if [ "${1:-}" = exec ]; then shift; fi'


class WorkflowPathsTest(unittest.TestCase):
    def test_push_and_pull_request_filters_match(self):
        self.assertTrue(filters("push"))
        self.assertEqual(filters("push"), filters("pull_request"))

    def test_changes_that_affect_dart_run_it(self):
        for path in MUST_RUN:
            with self.subTest(path=path):
                self.assertTrue(runs(path, "pull_request"), f"{path} does not trigger dart-packages.yml")

    def test_unrelated_changes_do_not(self):
        for path in MUST_NOT_RUN:
            with self.subTest(path=path):
                self.assertFalse(runs(path, "pull_request"), f"{path} triggers dart-packages.yml")


class WorkflowToolchainTest(unittest.TestCase):
    def test_flutter_comes_from_fvmrc(self):
        actions = text().count("uses: subosito/flutter-action@")
        self.assertEqual(actions, 3)
        self.assertEqual(actions, text().count("flutter-version-file: dart-packages/.fvmrc"))
        self.assertNotIn("flutter-version: ", text())

    def test_pub_cache_keys_on_pubspec_yaml(self):
        self.assertIn("pub-cache: false", text())
        self.assertIn("hashFiles('dart-packages/*/pubspec.yaml'", text())

    def test_cache_action_uses_forges_pin(self):
        self.assertIn("uses: actions/cache@v4", text())
        self.assertNotRegex(text(), r"uses: actions/cache@(?!v4\b)")

    def test_every_fvm_stand_in_drops_a_leading_exec(self):
        # tool/build_extension.sh runs `fvm exec dart run ...`. A stand-in
        # that only did `exec "$@"` would run `exec dart ...` as a command.
        shims = text().count('"$RUNNER_TEMP/fvm-shim/fvm" <<')
        self.assertEqual(shims, text().count("uses: subosito/flutter-action@"))
        self.assertEqual(text().count(SHIM_EXEC), shims)
        self.assertNotIn("printf '#!/usr/bin/env bash\\nexec", text())

    def test_the_fvm_stand_in_runs_what_it_is_given(self):
        import subprocess
        import tempfile

        body = re.search(r"<<'SH'\n(.*?)\n\s*SH\n", text(), re.S)
        self.assertIsNotNone(body, "no fvm stand-in heredoc")
        script = "\n".join(line.strip() for line in body.group(1).split("\n")) + "\n"
        with tempfile.TemporaryDirectory() as tmp:
            shim = os.path.join(tmp, "fvm")
            with open(shim, "w", encoding="utf-8") as f:
                f.write(script)
            os.chmod(shim, 0o755)
            for args in (["exec", "echo", "a b"], ["echo", "a b"]):
                with self.subTest(args=args):
                    out = subprocess.run([shim, *args], capture_output=True, text=True, check=True)
                    self.assertEqual(out.stdout, "a b\n")

    def test_generator_job_installs_go_ymls_typescript_toolchain(self):
        # Without esbuild the codec parity test skips, which the job turns
        # into a failure, and without tsc the TypeScript type check fatals.
        with open(GO_WORKFLOW, encoding="utf-8") as f:
            go = f.read()
        pins = set(re.findall(r"npm-global-packages: '([^']+)'", go))
        self.assertEqual(len(pins), 1, pins)
        node = set(re.findall(r"node-version: '([^']+)'", go))
        self.assertEqual(len(node), 1, node)
        generator = job("generator")
        self.assertIn("uses: actions/setup-node@", generator)
        self.assertIn(f"node-version: '{node.pop()}'", generator)
        self.assertIn(f"run: npm install -g {pins.pop()}\n", generator + "\n")


class WorkflowGroveTest(unittest.TestCase):
    def test_grove_ref_is_read_from_the_pin_not_written_down(self):
        self.assertNotIn("GROVE_REF: ", text())
        for name in ("packages", "docs"):
            with self.subTest(job=name):
                body = job(name)
                read = body.find("--print-ref grove_crdt")
                checkout = body.find("repository: xraph/grove")
                self.assertNotEqual(read, -1, f"{name} never reads the grove_crdt pin")
                self.assertNotEqual(checkout, -1, f"{name} never checks grove out")
                self.assertLess(read, checkout)
                self.assertIn("ref: ${{ env.GROVE_REF }}", body)

    def test_packages_job_checks_grove_out_in_full(self):
        # forge_client_grove's conformance tests build grove's Go server,
        # whose go.mod replaces github.com/xraph/grove with the checkout root.
        self.assertNotIn("sparse-checkout", job("packages"))

    def test_conformance_server_modules_are_fetched_before_the_tests(self):
        # The test builds the server with GOPROXY=off, so a cold module cache
        # fails it on the runner.
        body = job("packages")
        warm = body.find("tool/conformance_server")
        self.assertNotEqual(warm, -1)
        self.assertLess(warm, body.index("      - name: Test\n"))


class WorkflowMatrixTest(unittest.TestCase):
    def test_devtools_extension_is_built_before_its_tests_and_dry_run(self):
        body = text()
        build = body.find("run: bash tool/build_extension.sh")
        self.assertNotEqual(build, -1, "no step runs tool/build_extension.sh")
        step = body.rfind("- name:", 0, build)
        self.assertIn("if: matrix.package == 'forge_client_devtools'", body[step:build])
        self.assertLess(build, body.index("      - name: Test\n"))
        self.assertLess(build, body.index("      - name: Publish dry run\n"))

    def test_every_package_has_a_leg(self):
        packages = sorted(
            name for name in os.listdir(os.path.join(HERE, "..", "..", "dart-packages"))
            if os.path.isfile(os.path.join(HERE, "..", "..", "dart-packages", name, "pubspec.yaml"))
        )
        body = job("packages")
        for name in packages:
            with self.subTest(package=name):
                self.assertIn(f"package: {name},", body)

    def test_chrome_runs_only_the_web_network_error_test(self):
        # Two untagged forge_client_offline tests import dart:io and web
        # storage is a stub until plan 04 Task 9, so the rest cannot pass.
        calls = re.findall(r"flutter test --platform chrome(.*)", text())
        self.assertEqual([c.strip() for c in calls], ["test/outbox/network_error_web_test.dart"])

    def test_windows_leg_does_not_gate(self):
        legs = [line for line in job("packages").split("\n") if "windows-latest" in line]
        self.assertEqual(len(legs), 1, legs)
        self.assertIn("package: forge_client_offline,", legs[0])
        self.assertIn("continue-on-error: ${{ matrix.optional == true }}", job("packages"))
        self.assertIn("optional: true", legs[0])

    def test_docs_links_cover_the_web_pages_that_link_in(self):
        body = job("docs")
        self.assertIn("docs/content/docs/dart-client/*.mdx", body)
        self.assertIn("docs/content/docs/web-client/index.mdx", body)
        self.assertIn("docs/content/docs/web-client/not-yet-shipped.mdx", body)
        self.assertNotIn("--pending", body)

    def test_every_python_suite_runs(self):
        body = job("scripts")
        self.assertIn("-s .github/scripts -p 'test_*.py'", body)
        self.assertIn("-s .github/scripts/dart-docs -p 'test_*.py'", body)


if __name__ == "__main__":
    unittest.main()
