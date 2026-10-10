"""Pins the tagging and publish hand-off in release.yml and npm-publish.yml.

On 2026-10-08 a manual dispatch of 1.12.1, a version whose tag already
existed at an older commit, tagged all 18 extensions at that day's main and
pushed them, and the proxy now serves those for good. These tests run the
version-reuse guard and the tag step, taken from release.yml, against a
scratch repository with a bare origin, so a regression shows up here and not
in another permanent tag. They also run npm-publish.yml's check shell against
a fake `npm`.

Run: python3 -m unittest discover -s .github/scripts -p 'test_*.py'
"""

import json
import os
import re
import subprocess
import tempfile
import textwrap
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.join(HERE, "..", "..")
RELEASE = os.path.join(HERE, "..", "workflows", "release.yml")
NPM = os.path.join(HERE, "..", "workflows", "npm-publish.yml")


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def run_block(text, step_name):
    """The run: | body of the step with this name, dedented as GitHub renders it."""
    lines = text.split("\n")
    starts = [i for i, line in enumerate(lines) if line.strip() == f"- name: {step_name}"]
    if len(starts) != 1:
        raise AssertionError(f"expected one step named {step_name!r}, found {len(starts)}")
    run = next(i for i in range(starts[0], len(lines)) if lines[i].strip() == "run: |")
    indent = len(lines[run + 1]) - len(lines[run + 1].lstrip())
    body = []
    for line in lines[run + 1:]:
        if line.strip() and len(line) - len(line.lstrip()) < indent:
            break
        body.append(line[indent:])
    return "\n".join(body).rstrip() + "\n"


def job(text, name):
    """The lines of jobs.<name>, up to the next job or section comment."""
    lines = text.split("\n")
    out, inside = [], False
    for line in lines[lines.index("jobs:") + 1:]:
        if re.match(rf"^  {re.escape(name)}:\s*$", line):
            inside = True
            continue
        if inside and re.match(r"^  \S", line):
            break
        if inside:
            out.append(line)
    if not out:
        raise AssertionError(f"no job {name!r}")
    return "\n".join(out)


def release_extensions():
    match = re.search(r"\n  RELEASE_EXTENSIONS: >-\n((?:    [a-z ]+\n)+)", read(RELEASE))
    if not match:
        raise AssertionError("no RELEASE_EXTENSIONS in release.yml")
    return match.group(1).split()


GUARD = run_block(read(RELEASE), "Guard against version reuse")
CREATE = run_block(read(RELEASE), "Create tags (manual dispatch)")


def git(cwd, *args):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout.strip()


class TagTest(unittest.TestCase):
    """A clone of a bare origin with two commits: OLD, then HEAD."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.origin = os.path.join(self.tmp.name, "origin.git")
        self.repo = os.path.join(self.tmp.name, "repo")
        subprocess.run(["git", "init", "-q", "--bare", self.origin], check=True)
        subprocess.run(["git", "init", "-q", self.repo], check=True)
        for key, value in (("user.name", "t"), ("user.email", "t@example.com"), ("commit.gpgsign", "false"),
                           ("tag.gpgsign", "false")):
            git(self.repo, "config", key, value)
        git(self.repo, "remote", "add", "origin", self.origin)
        git(self.repo, "commit", "-q", "--allow-empty", "-m", "old")
        self.old = git(self.repo, "rev-parse", "HEAD")
        git(self.repo, "commit", "-q", "--allow-empty", "-m", "head")
        self.head = git(self.repo, "rev-parse", "HEAD")
        git(self.repo, "push", "-q", "origin", "HEAD:refs/heads/main")

    def step(self, script, version="1.2.3", is_all="false", tag=None):
        env = {
            "PATH": os.environ["PATH"],
            "HOME": self.tmp.name,
            "GIT_CONFIG_NOSYSTEM": "1",
            "TAG": tag or f"v{version}",
            "VERSION": version,
            "IS_ALL": is_all,
            "RELEASE_EXTENSIONS": " ".join(release_extensions()),
        }
        return subprocess.run(["bash", "-c", script], cwd=self.repo, env=env, capture_output=True, text=True)

    def tags(self, where=None):
        out = git(where or self.repo, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/tags")
        return dict(line.split(" ", 1) for line in out.splitlines())

    def assertPasses(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def assertFails(self, result, *messages):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        for message in messages:
            self.assertIn(message, result.stdout + result.stderr)


class GuardTest(TagTest):
    def test_an_absent_tag_passes(self):
        self.assertPasses(self.step(GUARD))
        self.assertPasses(self.step(GUARD, is_all="true"))

    def test_a_tag_at_head_passes_as_a_rerun(self):
        git(self.repo, "tag", "v1.2.3", self.head)
        result = self.step(GUARD)
        self.assertPasses(result)
        self.assertIn("re-run of the same release", result.stdout)

    def test_an_annotated_tag_at_head_passes(self):
        git(self.repo, "tag", "-a", "-m", "release", "v1.2.3", self.head)
        self.assertPasses(self.step(GUARD, is_all="true"))

    def test_a_tag_at_another_commit_fails_before_anything_is_tagged(self):
        git(self.repo, "tag", "v1.2.3", self.old)
        before = self.tags()
        self.assertFails(self.step(GUARD, is_all="true"),
                         f"Tag v1.2.3 already exists at {self.old}", "nothing was tagged or pushed")
        self.assertEqual(self.tags(), before)
        self.assertEqual(self.tags(self.origin), {})

    def test_an_extension_tag_at_another_commit_fails(self):
        git(self.repo, "tag", "extensions/webrtc/v1.2.3", self.old)
        self.assertFails(self.step(GUARD, is_all="true"),
                         f"Tag extensions/webrtc/v1.2.3 already exists at {self.old}")

    def test_the_1_12_1_shape_fails_even_on_the_release_commit(self):
        # Main tag on the release commit, extension tags somewhere else: the
        # exact state 2026-10-08 left behind. Running on the tag must still stop.
        git(self.repo, "checkout", "-q", self.old)
        git(self.repo, "tag", "v1.2.3", self.old)
        git(self.repo, "tag", "extensions/auth/v1.2.3", self.head)
        self.assertFails(self.step(GUARD, is_all="true"), "extensions/auth/v1.2.3 already exists")

    def test_an_extension_tag_elsewhere_does_not_block_a_main_only_release(self):
        git(self.repo, "tag", "extensions/webrtc/v1.2.3", self.old)
        self.assertPasses(self.step(GUARD))

    def test_an_extension_release_checks_its_own_tag(self):
        git(self.repo, "tag", "extensions/auth/v1.2.3", self.old)
        self.assertFails(self.step(GUARD, tag="extensions/auth/v1.2.3"), "extensions/auth/v1.2.3 already exists")

    def test_a_branch_named_like_the_tag_is_not_mistaken_for_it(self):
        git(self.repo, "branch", "v1.2.3", self.old)
        self.assertPasses(self.step(GUARD))


class CreateTest(TagTest):
    def test_all_mode_tags_every_module_at_head_and_pushes_them(self):
        self.assertPasses(self.step(GUARD, is_all="true"))
        self.assertPasses(self.step(CREATE, is_all="true"))
        expected = {"v1.2.3": self.head}
        expected.update({f"extensions/{ext}/v1.2.3": self.head for ext in release_extensions()})
        self.assertEqual(self.tags(self.origin), expected)

    def test_a_rerun_creates_and_pushes_nothing(self):
        self.assertPasses(self.step(CREATE, is_all="true"))
        before = self.tags(self.origin)
        self.assertPasses(self.step(GUARD, is_all="true"))
        result = self.step(CREATE, is_all="true")
        self.assertPasses(result)
        self.assertIn("nothing to push", result.stdout)
        self.assertEqual(self.tags(self.origin), before)

    def test_a_missing_extension_tag_is_filled_in_at_head(self):
        git(self.repo, "tag", "v1.2.3", self.head)
        git(self.repo, "push", "-q", "origin", "refs/tags/v1.2.3")
        self.assertPasses(self.step(CREATE, is_all="true"))
        self.assertEqual(self.tags(self.origin)["extensions/hls/v1.2.3"], self.head)

    def test_the_tag_step_refuses_a_main_tag_off_head_on_its_own(self):
        git(self.repo, "tag", "v1.2.3", self.old)
        self.assertFails(self.step(CREATE, is_all="true"), "Refusing to tag")
        self.assertEqual(self.tags(self.origin), {})
        self.assertNotIn("extensions/auth/v1.2.3", self.tags())

    def test_stray_local_tags_are_not_pushed(self):
        git(self.repo, "tag", "scratch", self.old)
        self.assertPasses(self.step(CREATE))
        self.assertEqual(self.tags(self.origin), {"v1.2.3": self.head})


class WiringTest(unittest.TestCase):
    def test_test_package_loop_works_with_macos_bash(self):
        with tempfile.TemporaryDirectory() as directory:
            fake_go = os.path.join(directory, "go")
            args_file = os.path.join(directory, "args")
            with open(fake_go, "w", encoding="utf-8") as f:
                f.write('#!/bin/bash\ncase "$1" in\nlist) printf "example/a\\nexample/b\\n";;\ntest) printf "%s\\n" "$@" > "$RELEASE_TEST_ARGS";;\nesac\n')
            os.chmod(fake_go, 0o755)
            block = run_block(job(read(RELEASE), "test"), "Run tests")
            block = block.replace("${{ needs.detect.outputs.module_path }}", ".")
            env = dict(os.environ, PATH=directory + os.pathsep + os.environ["PATH"],
                       RUNNER_TEMP=directory, RELEASE_TEST_ARGS=args_file)
            subprocess.run(["/bin/bash", "-e", "-o", "pipefail", "-c", block],
                           cwd=directory, env=env, check=True)
            self.assertEqual(read(args_file).splitlines()[-2:], ["example/a", "example/b"])

    def test_release_checkouts_share_the_pinned_source(self):
        workflow = read(RELEASE)
        self.assertIn("source_commit:", workflow)
        for name in ("detect", "test", "release-module", "dry-run"):
            self.assertIn("ref: ${{ inputs.source_commit || github.ref }}", job(workflow, name), name)
        self.assertIn("ref: main", job(workflow, "update-changelog"))
        self.assertIn('MODULE_TYPE="library"', workflow)
        self.assertIn("Pinned sources are supported for library and extension releases only", workflow)
        self.assertIn("module_type == 'library' && 'false'", job(workflow, "release-module"))

    def test_every_listed_extension_has_a_module_and_a_dispatch_option(self):
        options = re.search(r"options:\n((?:\s+- [-a-z]+\n)+)", read(RELEASE)).group(1).split()
        options = [o for o in options if o != "-"]
        listed = release_extensions()
        self.assertEqual(sorted(set(options) - {"forge", "forge-library", "cli", "all"}), sorted(listed))
        for ext in listed:
            self.assertTrue(os.path.exists(os.path.join(ROOT, "extensions", ext, "go.mod")), ext)

    def test_the_guard_runs_on_every_dispatch_before_the_tag_step(self):
        text = read(RELEASE)
        detect = job(text, "detect")
        self.assertIn("- name: Guard against version reuse\n        if: github.event_name == 'workflow_dispatch'\n",
                      detect)
        self.assertLess(detect.index("Guard against version reuse"), detect.index("Create tags (manual dispatch)"))
        self.assertNotIn("git push origin --tags", text)

    def test_publishing_is_dispatched_only_for_a_real_main_release(self):
        body = job(read(RELEASE), "trigger-publish")
        for condition in ("github.event_name == 'workflow_dispatch'",
                          "needs.detect.outputs.module_type == 'main'",
                          "needs.detect.outputs.dry_run == 'false'"):
            self.assertIn(condition, body)
        self.assertIn("permissions:\n      actions: write\n", body)
        self.assertIn('gh workflow run "$wf" --ref "$TAG" -f dry-run=false -f version="$VERSION"', body)
        self.assertIn("for wf in npm-publish.yml dart-publish.yml; do", body)


FAKE_NPM = textwrap.dedent("""\
    #!/usr/bin/env bash
    # npm view SPEC version, answered from $NPM_PRESENT (one spec per line).
    spec="$2"
    if [ -n "${NPM_BROKEN:-}" ]; then echo "npm error code ETIMEDOUT" >&2; exit 1; fi
    if grep -qxF "$spec" <<< "$NPM_PRESENT"; then echo "${spec##*@}"; exit 0; fi
    echo "npm error code E404" >&2
    exit 1
""")


class NpmCheckTest(unittest.TestCase):
    PACKAGES = ["packages/a", "packages/b"]

    def check(self, present, dry_run="false", version="1.12.2", broken=False):
        with tempfile.TemporaryDirectory() as tmp:
            for i, path in enumerate(self.PACKAGES):
                os.makedirs(os.path.join(tmp, path))
                with open(os.path.join(tmp, path, "package.json"), "w", encoding="utf-8") as f:
                    json.dump({"name": f"@forge-go/{path.split('/')[1]}", "version": "0.0.1"}, f)
            bindir = os.path.join(tmp, "bin")
            os.makedirs(bindir)
            with open(os.path.join(bindir, "npm"), "w", encoding="utf-8") as f:
                f.write(FAKE_NPM)
            os.chmod(os.path.join(bindir, "npm"), 0o755)
            out, summary = os.path.join(tmp, "out"), os.path.join(tmp, "summary")
            env = {
                "PATH": bindir + os.pathsep + os.environ["PATH"],
                "VERSION": version,
                "DRY_RUN": dry_run,
                "PACKAGES": json.dumps(self.PACKAGES),
                "NPM_PRESENT": "\n".join(present),
                "NPM_BROKEN": "1" if broken else "",
                "GITHUB_OUTPUT": out,
                "GITHUB_STEP_SUMMARY": summary,
            }
            result = subprocess.run(["bash", "-c", run_block(read(NPM), "Check")], cwd=tmp, env=env,
                                    capture_output=True, text=True)
            outputs = {}
            if os.path.exists(out):
                with open(out, encoding="utf-8") as f:
                    outputs = dict(line.split("=", 1) for line in f.read().splitlines())
            return result, outputs

    def test_all_present_skips_the_publish(self):
        result, outputs = self.check(["@forge-go/a@1.12.2", "@forge-go/b@1.12.2"])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["skip"], "true")
        self.assertEqual(json.loads(outputs["packages"]), self.PACKAGES)

    def test_none_present_publishes(self):
        result, outputs = self.check([])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["skip"], "false")

    def test_some_present_fails_naming_both_sides(self):
        result, outputs = self.check(["@forge-go/a@1.12.2"])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Already published: @forge-go/a@1.12.2", result.stdout)
        self.assertIn("Not yet published: @forge-go/b@1.12.2", result.stdout)
        self.assertNotIn("skip", outputs)

    def test_an_empty_version_reads_each_package_json(self):
        result, outputs = self.check(["@forge-go/a@0.0.1", "@forge-go/b@0.0.1"], version="")
        self.assertEqual(outputs.get("skip"), "true", result.stdout + result.stderr)

    def test_a_dry_run_never_skips(self):
        result, outputs = self.check(["@forge-go/a@1.12.2", "@forge-go/b@1.12.2"], dry_run="true")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(outputs["skip"], "false")

    def test_an_npm_error_other_than_404_fails(self):
        result, outputs = self.check([], broken=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Could not ask npm", result.stdout)
        self.assertNotIn("skip", outputs)

    def test_publish_takes_its_list_from_check_and_is_skipped_on_its_word(self):
        body = job(read(NPM), "publish")
        self.assertIn("needs: [prepare, check]", body)
        self.assertIn("if: needs.check.outputs.skip != 'true'", body)
        self.assertIn("packages: ${{ needs.check.outputs.packages }}", body)


if __name__ == "__main__":
    unittest.main()
