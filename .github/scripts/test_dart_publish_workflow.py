"""Pins .github/workflows/dart-publish.yml and .github/scripts/dart_publish.sh.

A real publish runs only on a forge version tag, so nothing short of a
release exercises it. These tests pin what would otherwise first fail there:
the publish order against each package's real dependencies, the grove_crdt
exclusion, who can mint a pub.dev token, the dry-run default, and the
script's behaviour when a package is already on pub.dev, fails, or never goes
live. The last of those run the script for real against a local stand-in for
pub.dev and a fake `dart`.

Run: python3 -m unittest discover -s .github/scripts -p 'test_*.py'
"""

import http.server
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import textwrap
import threading
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "..")
WORKFLOW = os.path.join(HERE, "..", "workflows", "dart-publish.yml")
SCRIPT = os.path.join(HERE, "dart_publish.sh")

sys.path.insert(0, HERE)
import dart_ci_pubspec  # noqa: E402

ORDER = [
    "forge_client",
    "forge_client_offline",
    "forge_client_flutter",
    "forge_client_riverpod",
    "forge_client_devtools",
]

# The stand-in's one line of logic: `fvm exec CMD` runs CMD, so drop the exec.
SHIM_EXEC = 'if [ "${1:-}" = exec ]; then shift; fi'


def text():
    with open(WORKFLOW, encoding="utf-8") as f:
        return f.read()


def script():
    with open(SCRIPT, encoding="utf-8") as f:
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
        raise AssertionError(f"no job {name!r} in dart-publish.yml")
    return "\n".join(out)


def published_packages():
    """The DART_PACKAGES list both publishing jobs pass to dart_publish.sh."""
    match = re.search(r"\n  DART_PACKAGES: >-\n((?:    [a-z_]+\n)+)", text())
    if not match:
        raise AssertionError("no DART_PACKAGES list in dart-publish.yml")
    return match.group(1).split()


def run_block(job_body, step_name):
    """The run: | body of the named step, dedented as GitHub renders it."""
    lines = job_body.split("\n")
    start = next(i for i, line in enumerate(lines) if line.strip() == f"- name: {step_name}")
    run = next(i for i in range(start, len(lines)) if lines[i].strip() == "run: |")
    indent = len(lines[run + 1]) - len(lines[run + 1].lstrip())
    body = []
    for line in lines[run + 1:]:
        if line.strip() and len(line) - len(line.lstrip()) < indent:
            break
        body.append(line[indent:])
    return "\n".join(body).rstrip() + "\n"


def sibling_dependencies(package):
    """Every sibling a package names by path, dev_dependencies included."""
    with open(os.path.join(ROOT, "dart-packages", package, "pubspec.yaml"), encoding="utf-8") as f:
        lines = f.read().split("\n")
    return {name for name, _, _, source, _ in dart_ci_pubspec.local_dependencies(lines) if source == "path"}


class PublishOrderTest(unittest.TestCase):
    def test_the_order_is_pinned(self):
        self.assertEqual(published_packages(), ORDER)

    def test_every_package_comes_after_everything_it_depends_on(self):
        # A real publish resolves against live pub.dev with no overrides, and
        # pub resolves the root's dev_dependencies too, so a dev-only sibling
        # has to be live first as well. forge_client_flutter dev-depends on
        # forge_client_offline, which is why offline goes before it.
        order = published_packages()
        for i, package in enumerate(order):
            for dependency in sibling_dependencies(package):
                with self.subTest(package=package, dependency=dependency):
                    self.assertIn(dependency, order[:i], f"{package} needs {dependency} published first")

    def test_grove_is_left_out_until_grove_crdt_is_on_pub_dev(self):
        self.assertNotIn("forge_client_grove", published_packages())
        header = text().split("\non:\n")[0]
        self.assertIn("forge_client_grove is left out on purpose", header)
        self.assertIn("grove_crdt", header)

    def test_every_other_package_is_published(self):
        # A new package has to be added to the list, or left out with a reason
        # the way forge_client_grove is.
        packages = {
            name for name in os.listdir(os.path.join(ROOT, "dart-packages"))
            if os.path.isfile(os.path.join(ROOT, "dart-packages", name, "pubspec.yaml"))
        }
        self.assertEqual(set(published_packages()), packages - {"forge_client_grove"})

    def test_the_header_lists_the_order_it_explains(self):
        header = text().split("\non:\n")[0]
        explained = re.findall(r"^#   (forge_client\w*)\s", header, re.M)
        self.assertEqual(explained, ORDER)


class PublishTriggerTest(unittest.TestCase):
    def test_only_a_version_tag_publishes(self):
        on = text().split("\non:\n")[1].split("\npermissions:")[0]
        self.assertIn("  push:\n    tags:\n      - 'v[0-9]+.[0-9]+.[0-9]+'\n", on)
        self.assertNotIn("branches", on)
        self.assertNotIn("pull_request", on)

    def test_a_manual_run_is_a_dry_run_by_default(self):
        on = text().split("\non:\n")[1].split("\npermissions:")[0]
        dispatch = on[on.index("  workflow_dispatch:"):]
        dry = dispatch[dispatch.index("      dry-run:"):dispatch.index("      version:")]
        self.assertIn("type: boolean", dry)
        self.assertIn("default: true", dry)
        self.assertIn("default: ''", dispatch[dispatch.index("      version:"):])

    def test_prepare_turns_only_a_push_into_a_real_publish(self):
        body = job("prepare")
        self.assertIn('if [ "$EVENT" = "push" ]; then\n            version="${REF_NAME#v}"\n            dry_run=false\n', body)
        self.assertIn('dry_run="${INPUT_DRY_RUN:-true}"', body)
        self.assertIn('version="${INPUT_VERSION#v}"', body)

    def test_each_mode_has_its_own_job_fed_by_prepare(self):
        dry, real = job("dry-run"), job("publish")
        self.assertIn("if: needs.prepare.outputs.dry-run == 'true'", dry)
        self.assertIn("if: needs.prepare.outputs.dry-run == 'false'", real)
        for body, mode in ((dry, "'true'"), (real, "'false'")):
            with self.subTest(mode=mode):
                self.assertIn("needs: [prepare, devtools]", body)
                self.assertIn("VERSION: ${{ needs.prepare.outputs.version }}", body)
                self.assertIn(f"DRY_RUN: {mode}", body)
                self.assertIn('read -r -a packages <<< "$DART_PACKAGES"\n'
                              '          bash .github/scripts/dart_publish.sh "${packages[@]}"', body)

    def test_the_script_defaults_to_a_dry_run(self):
        self.assertIn("dry_run=${DRY_RUN:-true}", script())


class PrepareTest(unittest.TestCase):
    """Runs prepare's shell, taken from the workflow, for each way in."""

    def prepare(self, event, ref, version="", dry_run=""):
        with tempfile.TemporaryDirectory() as tmp:
            out, summary = os.path.join(tmp, "out"), os.path.join(tmp, "summary")
            env = {
                "PATH": os.environ["PATH"],
                "EVENT": event,
                "REF": ref,
                "REF_NAME": ref.rsplit("/", 1)[-1],
                "INPUT_VERSION": version,
                "INPUT_DRY_RUN": dry_run,
                "GITHUB_OUTPUT": out,
                "GITHUB_STEP_SUMMARY": summary,
            }
            result = subprocess.run(["bash", "-c", run_block(job("prepare"), "Resolve")], env=env,
                                    capture_output=True, text=True)
            outputs = {}
            if os.path.exists(out):
                with open(out, encoding="utf-8") as f:
                    outputs = dict(line.split("=", 1) for line in f.read().splitlines())
            return result, outputs

    def assertResolves(self, expected, *args, **kwargs):
        result, outputs = self.prepare(*args, **kwargs)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(outputs, expected)

    def assertRefuses(self, message, *args, **kwargs):
        result, outputs = self.prepare(*args, **kwargs)
        self.assertNotEqual(result.returncode, 0, outputs)
        self.assertEqual(outputs, {})
        self.assertIn(message, result.stdout + result.stderr)

    def test_a_tag_push_publishes_its_version(self):
        self.assertResolves({"version": "1.13.0", "dry_run": "false"}, "push", "refs/tags/v1.13.0")

    def test_a_manual_run_defaults_to_a_dry_run(self):
        self.assertResolves({"version": "", "dry_run": "true"}, "workflow_dispatch", "refs/heads/main", "", "true")
        # An absent input, as an older caller would send, is a dry run too.
        self.assertResolves({"version": "", "dry_run": "true"}, "workflow_dispatch", "refs/heads/main", "", "")

    def test_a_manual_dry_run_can_stamp_any_version_from_any_ref(self):
        self.assertResolves({"version": "2.0.0", "dry_run": "true"},
                            "workflow_dispatch", "refs/heads/feature/x", "v2.0.0", "true")
        self.assertResolves({"version": "1.13.0-rc.1", "dry_run": "true"},
                            "workflow_dispatch", "refs/heads/main", "1.13.0-rc.1", "true")

    def test_a_real_manual_run_from_a_branch_is_refused(self):
        self.assertRefuses("must run on refs/tags/v1.13.0, not refs/heads/main",
                           "workflow_dispatch", "refs/heads/main", "1.13.0", "false")

    def test_a_real_manual_run_without_a_version_is_refused(self):
        self.assertRefuses("A real publish needs a version", "workflow_dispatch", "refs/tags/v1.13.0", "", "false")

    def test_a_real_manual_run_on_another_tag_is_refused(self):
        self.assertRefuses("must run on refs/tags/v1.14.0, not refs/tags/v1.13.0",
                           "workflow_dispatch", "refs/tags/v1.13.0", "1.14.0", "false")

    def test_a_real_manual_run_on_the_matching_tag_is_a_tag_push(self):
        self.assertResolves({"version": "1.13.0", "dry_run": "false"},
                            "workflow_dispatch", "refs/tags/v1.13.0", "v1.13.0", "false")

    def test_a_real_prerelease_is_refused_even_on_its_own_tag(self):
        self.assertRefuses("is a prerelease or build version",
                           "workflow_dispatch", "refs/tags/v1.13.0-rc.1", "1.13.0-rc.1", "false")
        self.assertRefuses("is a prerelease or build version",
                           "workflow_dispatch", "refs/tags/v1.13.0+build.7", "1.13.0+build.7", "false")

    def test_versions_pub_would_misread_are_refused(self):
        # The trigger glob matches v01.2.3, so prepare is the gate.
        self.assertRefuses("not a version pub accepts", "push", "refs/tags/v01.2.3")
        for bad in ("1.13", "1.02.3", "1.2.03", "latest", "1.2.3.4"):
            with self.subTest(version=bad):
                self.assertRefuses("not a version pub accepts", "workflow_dispatch", "refs/heads/main", bad, "true")

    def test_zero_components_are_fine(self):
        self.assertResolves({"version": "0.1.0", "dry_run": "false"}, "push", "refs/tags/v0.1.0")
        self.assertResolves({"version": "10.0.0", "dry_run": "false"}, "push", "refs/tags/v10.0.0")


class PublishPermissionsTest(unittest.TestCase):
    def test_only_the_publish_job_can_mint_an_oidc_token(self):
        self.assertEqual(text().count("id-token: write"), 1)
        self.assertIn("    permissions:\n      contents: read\n      id-token: write\n", job("publish"))
        for name in ("prepare", "devtools", "dry-run"):
            with self.subTest(job=name):
                self.assertNotIn("id-token", job(name))
                self.assertNotIn("permissions", job(name))
                self.assertNotIn("setup-dart", job(name))

    def test_the_workflow_default_is_read_only(self):
        top = text().split("\njobs:\n")[0]
        self.assertIn("\npermissions:\n  contents: read\n", top)
        self.assertEqual(top.count("permissions:"), 1)

    def test_no_secret_is_used(self):
        self.assertNotIn("secrets.", text())

    def test_setup_dart_registers_the_token_before_flutter_shadows_its_dart(self):
        body = job("publish")
        self.assertEqual(text().count("uses: dart-lang/setup-dart@"), 1)
        self.assertLess(body.index("uses: dart-lang/setup-dart@"), body.index("uses: subosito/flutter-action@"))

    def test_the_script_mints_a_fresh_token_for_every_real_pub_command(self):
        # pub sends the registered token on reads too, so `pub get` needs a
        # live one as much as the upload does.
        body = script()
        self.assertIn("pub_fresh() {\n  refresh_pub_token || return 1\n  \"$tool\" pub \"$@\"\n}", body)
        real = body[body.index("      rm -f pubspec_overrides.yaml\n"):body.index("    echo \"::endgroup::\"\n    current=\"\"\n")]
        self.assertNotIn('"$tool" pub', real)
        self.assertIn("pub_fresh publish --force", real)
        self.assertIn("while ! pub_fresh get --no-example; do", body)
        self.assertIn("audience=https://pub.dev", body)
        self.assertIn('echo "::add-mask::$token"', body)

    def test_every_action_is_pinned_by_sha(self):
        uses = re.findall(r"uses: (\S+)(.*)", text())
        self.assertTrue(uses)
        for action, comment in uses:
            with self.subTest(action=action):
                self.assertRegex(action, r"@[0-9a-f]{40}$")
                self.assertRegex(comment, r"^ # v\d+\.\d+\.\d+$")

    def test_flutter_comes_from_fvmrc_with_no_caches(self):
        self.assertNotIn("actions/cache", text())
        for name in ("devtools", "dry-run", "publish"):
            with self.subTest(job=name):
                body = job(name)
                self.assertIn("flutter-version-file: dart-packages/.fvmrc", body)
                self.assertIn("cache: false", body)
                self.assertIn("pub-cache: false", body)


class PublishStepsTest(unittest.TestCase):
    def test_the_hosted_rewrite_is_hidden_from_git(self):
        # pub's GitStatusValidator warns on a modified checked-in file, and
        # --dry-run exits 65 on any warning. The rewrite modifies pubspec.yaml
        # and, with a version, CHANGELOG.md.
        lines = [line.strip() for line in script().split("\n")]
        rewrite = max(i for i, line in enumerate(lines) if "dart_ci_pubspec.py" in line and "--hosted" in line)
        after = [line for line in lines[rewrite + 1:] if line and not line.startswith("#")]
        self.assertEqual(after[:2], ["fi", "git update-index --assume-unchanged pubspec.yaml CHANGELOG.md"])
        first_get = next(i for i, line in enumerate(lines) if "pub get" in line and not line.startswith("#")
                         and i > rewrite)
        self.assertLess(lines.index("git update-index --assume-unchanged pubspec.yaml CHANGELOG.md"), first_get)

    def test_a_stamped_version_also_stamps_the_changelog(self):
        self.assertIn('--hosted --version "$VERSION" --changelog', script())

    def test_the_devtools_extension_is_built_without_the_token_and_handed_over(self):
        build = job("devtools")
        self.assertIn("working-directory: dart-packages/forge_client_devtools\n        run: bash tool/build_extension.sh", build)
        self.assertLess(build.index("run: bash tool/build_extension.sh"), build.index("uses: actions/upload-artifact@"))
        self.assertIn("path: dart-packages/forge_client_devtools/extension/devtools/build", build)
        self.assertIn("if-no-files-found: error", build)
        self.assertIn("digest: ${{ steps.digest.outputs.digest }}", build)
        self.assertEqual(text().count("run: bash tool/build_extension.sh"), 1)

    def test_both_publishing_jobs_check_the_build_arrived_before_publishing(self):
        fingerprint = run_block(job("devtools"), "Fingerprint the build").split("\n")[0]
        for name in ("dry-run", "publish"):
            with self.subTest(job=name):
                body = job(name)
                download = body.index("uses: actions/download-artifact@")
                check = body.index("- name: Check the DevTools build arrived whole")
                self.assertLess(download, check)
                self.assertLess(check, body.index("bash .github/scripts/dart_publish.sh"))
                self.assertIn("name: devtools-extension\n          path: dart-packages/forge_client_devtools/extension/devtools/build", body)
                verify = run_block(body, "Check the DevTools build arrived whole")
                self.assertIn("test -f index.html && test -f main.dart.js", verify)
                self.assertIn(fingerprint, verify)
                self.assertIn("EXPECTED: ${{ needs.devtools.outputs.digest }}", body)

    def test_the_fingerprint_matches_a_tree_and_catches_a_missing_file(self):
        verify = run_block(job("publish"), "Check the DevTools build arrived whole")
        fingerprint = run_block(job("devtools"), "Fingerprint the build").split("\n")[0]
        with tempfile.TemporaryDirectory() as tmp:
            for name in ("index.html", "main.dart.js", "assets/a.bin", ".last_build_id"):
                os.makedirs(os.path.dirname(os.path.join(tmp, name)) or tmp, exist_ok=True)
                with open(os.path.join(tmp, name), "w", encoding="utf-8") as f:
                    f.write(name)
            digest = subprocess.run(["bash", "-c", fingerprint + '\necho "$digest"'], cwd=tmp,
                                    capture_output=True, text=True, check=True).stdout.strip()
            self.assertRegex(digest, r"^[0-9a-f]{64}$")
            # The artifact drops hidden files, so they must not count.
            os.remove(os.path.join(tmp, ".last_build_id"))
            ok = subprocess.run(["bash", "-e", "-c", verify], cwd=tmp, env={**os.environ, "EXPECTED": digest},
                                capture_output=True, text=True)
            self.assertEqual(ok.returncode, 0, ok.stdout + ok.stderr)
            os.remove(os.path.join(tmp, "assets", "a.bin"))
            bad = subprocess.run(["bash", "-e", "-c", verify], cwd=tmp, env={**os.environ, "EXPECTED": digest},
                                 capture_output=True, text=True)
            self.assertNotEqual(bad.returncode, 0)
            self.assertIn("differs", bad.stdout)

    def test_the_fvm_stand_in_drops_a_leading_exec(self):
        body = job("devtools")
        self.assertEqual(text().count('"$RUNNER_TEMP/fvm-shim/fvm" <<'), 1)
        self.assertIn(SHIM_EXEC, body)
        self.assertLess(body.index(SHIM_EXEC), body.index("run: bash tool/build_extension.sh"))

    def test_the_publish_job_outlasts_the_scripts_budget(self):
        self.assertIn("timeout-minutes: 90", job("publish"))
        self.assertIn("BUDGET_SECONDS=${BUDGET_SECONDS:-4500}", script())

    def test_runs_for_the_same_ref_queue_instead_of_cancelling(self):
        self.assertIn("cancel-in-progress: false", text())


# The fake `dart`/`flutter`: logs each call with the token it was handed,
# fails the first FAIL_GET_TIMES `pub get`s of each package, and on
# `pub publish --force` marks the package published on the fake pub.dev
# unless told to fail.
FAKE_TOOL = r"""#!/usr/bin/env bash
pkg=$(basename "$PWD")
overrides=no
[ -f pubspec_overrides.yaml ] && overrides=yes
echo "$pkg $* overrides=$overrides token=${PUB_TOKEN:-}" >> "$FAKE_LOG"
if [ "$1 $2" = "pub get" ] && [ -n "${FAIL_GET_TIMES:-}" ]; then
  count_file="$FAKE_STATE/.gets-$pkg"
  count=$(cat "$count_file" 2>/dev/null || echo 0)
  echo $((count + 1)) > "$count_file"
  [ "$count" -lt "$FAIL_GET_TIMES" ] && exit 1
fi
if [ "$*" = "pub publish --force" ]; then
  [ "$pkg" = "${FAIL_ON:-}" ] && exit 1
  [ -n "${NEVER_LIVE:-}" ] || touch "$FAKE_STATE/$pkg"
fi
exit 0
"""


class FakePubDev(http.server.BaseHTTPRequestHandler):
    """Answers the version endpoint from a directory of marker files."""

    state = None
    answers = {}
    requests = []
    tokens = 0

    def log_message(self, *args):
        pass

    def do_GET(self):
        FakePubDev.requests.append(self.path)
        token = re.match(r"^/token\?x=1&audience=(.+)$", self.path)
        if token:
            ok = self.headers.get("Authorization") == "bearer request-token" and token.group(1) == "https://pub.dev"
            # A different token every time, so a test can tell a fresh one
            # from a reused one.
            FakePubDev.tokens += 1
            return self.reply(200 if ok else 403, {"value": f"oidc-{FakePubDev.tokens}"})
        version = re.match(r"^/api/packages/([a-z_]+)/versions/([^/]+)$", self.path)
        if not version:
            return self.reply(404, {})
        name = version.group(1)
        if name in FakePubDev.answers:
            return self.reply(FakePubDev.answers[name], {})
        live = os.path.exists(os.path.join(FakePubDev.state, name))
        return self.reply(200 if live else 404, {})

    def reply(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def write(path, body):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(textwrap.dedent(body).lstrip())


class PublishScriptTest(unittest.TestCase):
    """Runs dart_publish.sh against a fake pub.dev, three packages a <- b <- c."""

    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), FakePubDev)
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()
        cls.base = f"http://127.0.0.1:{cls.server.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.tmp)
        self.repo = os.path.join(self.tmp, "repo")
        self.state = os.path.join(self.tmp, "state")
        self.log = os.path.join(self.tmp, "calls.log")
        self.summary = os.path.join(self.tmp, "summary.md")
        os.makedirs(self.state)
        FakePubDev.state = self.state
        FakePubDev.answers = {}
        FakePubDev.requests = []
        FakePubDev.tokens = 0

        packages = os.path.join(self.repo, "dart-packages")
        write(os.path.join(packages, "a", "pubspec.yaml"), """
            name: a
            version: 1.0.0-dev
            publish_to: none
        """)
        write(os.path.join(packages, "b", "pubspec.yaml"), """
            name: b
            version: 1.0.0-dev
            publish_to: none
            dependencies:
              a:
                path: ../a
        """)
        write(os.path.join(packages, "c", "pubspec.yaml"), """
            name: c
            version: 1.0.0-dev
            publish_to: none
            dependencies:
              a:
                path: ../a
            dev_dependencies:
              b:
                path: ../b
        """)
        for name in ("a", "b", "c"):
            write(os.path.join(packages, name, "CHANGELOG.md"), "## 1.0.0-dev\n\n- Initial release.\n")
        git = ["git", "-c", "user.email=t@example.com", "-c", "user.name=t"]
        subprocess.run(["git", "init", "-q"], cwd=self.repo, check=True)
        subprocess.run(["git", "add", "-A"], cwd=self.repo, check=True)
        subprocess.run([*git, "commit", "-qm", "init"], cwd=self.repo, check=True)

        bin_dir = os.path.join(self.tmp, "bin")
        os.makedirs(bin_dir)
        for tool in ("dart", "flutter"):
            path = os.path.join(bin_dir, tool)
            with open(path, "w", encoding="utf-8") as f:
                f.write(FAKE_TOOL)
            os.chmod(path, 0o755)
        self.env = {
            "PATH": bin_dir + os.pathsep + os.environ["PATH"],
            "HOME": self.tmp,
            "FAKE_LOG": self.log,
            "FAKE_STATE": self.state,
            "PUB_API": self.base,
            "WAIT_ATTEMPTS": "3",
            "WAIT_SECONDS": "0",
            "RESOLVE_SECONDS": "0",
            "VERSION": "2.0.0",
            "DRY_RUN": "false",
            "GITHUB_STEP_SUMMARY": self.summary,
            "ACTIONS_ID_TOKEN_REQUEST_URL": f"{self.base}/token?x=1",
            "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "request-token",
        }

    def run_script(self, **env):
        merged = dict(self.env)
        for key, value in env.items():
            if value is None:
                merged.pop(key, None)
            else:
                merged[key] = value
        return subprocess.run(["bash", SCRIPT, "a", "b", "c"], cwd=self.repo, env=merged,
                              capture_output=True, text=True, timeout=120)

    def calls(self):
        if not os.path.exists(self.log):
            return []
        with open(self.log, encoding="utf-8") as f:
            return f.read().splitlines()

    def uploads(self):
        return [line.split()[0] for line in self.calls() if " pub publish --force " in line]

    def read_summary(self):
        with open(self.summary, encoding="utf-8") as f:
            return f.read()

    def test_publishes_in_order_against_pub_dev(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.uploads(), ["a", "b", "c"])
        for line in self.calls():
            with self.subTest(call=line):
                # A real run resolves against pub.dev, never the local copies.
                self.assertIn("overrides=no", line)
        self.assertIn("Published: a 2.0.0, b 2.0.0, c 2.0.0.", self.read_summary())

    def test_every_real_pub_command_gets_its_own_fresh_token(self):
        # pub sends the token on `pub get` too, so a stale one there fails the
        # run before any upload.
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        self.assertEqual([" ".join(line.split()[1:3]) for line in calls[:3]], ["pub get", "pub publish", "pub publish"])
        tokens = [re.search(r"token=(\S*)", line).group(1) for line in calls]
        self.assertEqual(tokens, [f"oidc-{i}" for i in range(1, len(calls) + 1)])
        gets = [line for line in calls if " pub get " in line]
        self.assertEqual(len(gets), 3)

    def test_pub_get_is_retried_with_a_fresh_token_each_time(self):
        result = self.run_script(FAIL_GET_TIMES="2")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        a_gets = [line for line in self.calls() if line.startswith("a pub get ")]
        self.assertEqual(len(a_gets), 3)
        self.assertEqual(len({re.search(r"token=(\S*)", line).group(1) for line in a_gets}), 3)
        self.assertEqual(self.uploads(), ["a", "b", "c"])

    def test_pub_get_gives_up_after_its_attempts(self):
        result = self.run_script(FAIL_GET_TIMES="99", RESOLVE_ATTEMPTS="4")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len([line for line in self.calls() if line.startswith("a pub get ")]), 4)
        self.assertEqual(self.uploads(), [])
        self.assertIn("Failed: a. Not attempted: b c.", self.read_summary())

    def test_a_real_run_needs_a_version(self):
        result = self.run_script(VERSION="")
        self.assertEqual(result.returncode, 2)
        self.assertIn("a real publish needs VERSION", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_a_spent_budget_stops_before_the_next_package(self):
        result = self.run_script(BUDGET_SECONDS="0")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertIn("Out of time", result.stderr)
        self.assertIn("Failed: a. Not attempted: b c.", self.read_summary())

    def test_each_upload_waits_for_the_previous_version_to_go_live(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        polls = [p for p in FakePubDev.requests if p.startswith("/api/")]
        # Checked once before its upload (404), polled after it (200).
        self.assertEqual(polls, [
            "/api/packages/a/versions/2.0.0", "/api/packages/a/versions/2.0.0",
            "/api/packages/b/versions/2.0.0", "/api/packages/b/versions/2.0.0",
            "/api/packages/c/versions/2.0.0", "/api/packages/c/versions/2.0.0",
        ])

    def test_the_package_is_stamped_and_the_rewrite_hidden_from_git(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        with open(os.path.join(self.repo, "dart-packages", "c", "pubspec.yaml"), encoding="utf-8") as f:
            pubspec = f.read()
        self.assertIn("version: 2.0.0\n", pubspec)
        self.assertIn("  a: ^2.0.0\n", pubspec)
        self.assertIn("  b: ^2.0.0\n", pubspec)
        out = subprocess.run(["git", "ls-files", "-v"], cwd=self.repo, capture_output=True, text=True, check=True)
        hidden = sorted(line[2:] for line in out.stdout.splitlines() if line.startswith("h "))
        self.assertEqual(hidden, sorted(f"dart-packages/{n}/{f}" for n in "abc" for f in ("CHANGELOG.md", "pubspec.yaml")))

    def test_a_re_run_skips_versions_already_on_pub_dev(self):
        open(os.path.join(self.state, "a"), "w").close()
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.uploads(), ["b", "c"])
        self.assertIn("Already on pub.dev, skipped: a 2.0.0.", self.read_summary())

    def test_a_failure_stops_the_run_and_says_what_was_published(self):
        result = self.run_script(FAIL_ON="b")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.uploads(), ["a", "b"])
        summary = self.read_summary()
        self.assertIn("Published: a 2.0.0.", summary)
        self.assertIn("Failed: b. Not attempted: c.", summary)
        self.assertIn("re-run", summary)

    def test_a_version_that_never_goes_live_is_reported_as_uploaded_not_failed(self):
        result = self.run_script(NEVER_LIVE="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.uploads(), ["a"])
        self.assertIn("a 2.0.0 was uploaded, but pub.dev had not served it after", result.stderr)
        summary = self.read_summary()
        self.assertIn("Published: none.", summary)
        self.assertIn("Uploaded, not yet visible on pub.dev: a 2.0.0. Not attempted: b c.", summary)
        self.assertNotIn("Failed:", summary)

    def test_an_unclear_answer_from_pub_dev_stops_before_uploading(self):
        FakePubDev.answers = {"a": 500}
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.uploads(), [])
        self.assertIn("pub.dev answered 500 for a 2.0.0", result.stderr)

    def test_a_real_run_without_an_oidc_token_uploads_nothing(self):
        result = self.run_script(ACTIONS_ID_TOKEN_REQUEST_URL=None)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.uploads(), [])
        self.assertIn("id-token: write", result.stderr)
        self.assertEqual(self.calls(), [])

    def test_a_dry_run_keeps_the_overrides_and_never_calls_pub_dev(self):
        result = self.run_script(DRY_RUN=None)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.uploads(), [])
        self.assertEqual(FakePubDev.requests, [])
        dry = [line for line in self.calls() if " pub publish --dry-run " in line]
        self.assertEqual([line.split()[0] for line in dry], ["a", "b", "c"])
        for line in dry:
            self.assertIn("overrides=yes", line)
        self.assertIn("Dry run passed: a 2.0.0, b 2.0.0, c 2.0.0.", self.read_summary())

    def test_an_empty_version_keeps_each_pubspec_version(self):
        result = self.run_script(VERSION="", DRY_RUN="true")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        with open(os.path.join(self.repo, "dart-packages", "b", "pubspec.yaml"), encoding="utf-8") as f:
            pubspec = f.read()
        self.assertIn("version: 1.0.0-dev\n", pubspec)
        self.assertIn("  a: ^1.0.0-dev\n", pubspec)
        with open(os.path.join(self.repo, "dart-packages", "b", "CHANGELOG.md"), encoding="utf-8") as f:
            self.assertTrue(f.read().startswith("## 1.0.0-dev"))

    def test_dry_run_must_be_true_or_false(self):
        result = self.run_script(DRY_RUN="yes")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(self.calls(), [])


if __name__ == "__main__":
    unittest.main()
