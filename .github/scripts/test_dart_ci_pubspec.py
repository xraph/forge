"""Tests for dart_ci_pubspec.py. Run: python3 -m unittest discover -s .github/scripts -p 'test_*.py'"""

import os
import shutil
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "dart_ci_pubspec.py")


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(textwrap.dedent(text).lstrip())


def run(*args):
    return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True)


class DartCiPubspecTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.root)
        write(os.path.join(self.root, "forge_client", "pubspec.yaml"), """
            name: forge_client
            version: 1.0.0-dev
            publish_to: none
        """)
        write(os.path.join(self.root, "grove", "crdt-dart", "pubspec.yaml"), """
            name: grove_crdt
            version: 1.0.0-dev
        """)
        self.pkg = os.path.join(self.root, "forge_client_grove")
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_grove
            version: 1.0.0-dev
            publish_to: none
            environment:
              sdk: ^3.13.0
            dependencies:
              forge_client:
                path: ../forge_client
              grove_crdt:
                git:
                  url: https://github.com/xraph/grove.git
                  path: crdt-dart
                  ref: 11453d954cbb830703813e4a8ec858ad80862220
              meta: ^1.19.0
            dev_dependencies:
              test: ^1.32.0
        """)

    def read(self, name):
        with open(os.path.join(self.pkg, name), encoding="utf-8") as f:
            return f.read()

    def test_git_dependency_without_override_fails(self):
        result = run(self.pkg)
        self.assertEqual(result.returncode, 1)
        self.assertIn("grove_crdt is a git dependency", result.stderr)

    def test_overrides_point_at_existing_directories(self):
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        overrides = self.read("pubspec_overrides.yaml")
        self.assertIn(f"  forge_client:\n    path: {os.path.join(self.root, 'forge_client')}\n", overrides)
        self.assertIn(f"  grove_crdt:\n    path: {grove}\n", overrides)
        self.assertIn("forge_client:\n    path: ../forge_client", self.read("pubspec.yaml"))

    def test_without_hosted_the_pubspec_is_untouched(self):
        before = self.read("pubspec.yaml")
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.read("pubspec.yaml"), before)

    def test_hosted_rewrites_local_dependencies_only(self):
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        pubspec = self.read("pubspec.yaml")
        self.assertIn("  forge_client: ^1.0.0-dev\n", pubspec)
        self.assertIn("  grove_crdt: ^1.0.0-dev\n", pubspec)
        self.assertIn("  meta: ^1.19.0\n", pubspec)
        self.assertIn("  test: ^1.32.0\n", pubspec)
        self.assertNotIn("path:", pubspec)
        self.assertNotIn("git:", pubspec)
        self.assertNotIn("ref:", pubspec)

    def test_hosted_deletes_publish_to_none(self):
        # pub refuses a private package outright ("A private package cannot be
        # published"), so the dry run only proves anything without the line.
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        pubspec = self.read("pubspec.yaml")
        self.assertNotIn("publish_to", pubspec)
        self.assertTrue(pubspec.startswith("name: forge_client_grove\nversion: 1.0.0-dev\nenvironment:\n"))

    def test_hosted_deletes_a_quoted_publish_to_none(self):
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_grove
            version: 1.0.0-dev
            publish_to: 'none'
            dependencies:
              forge_client:
                path: ../forge_client
        """)
        result = run(self.pkg, "--hosted")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("publish_to", self.read("pubspec.yaml"))

    def test_hosted_keeps_a_publish_to_url(self):
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_grove
            version: 1.0.0-dev
            publish_to: https://pub.example.com
            dependencies:
              forge_client:
                path: ../forge_client
        """)
        result = run(self.pkg, "--hosted")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("publish_to: https://pub.example.com\n", self.read("pubspec.yaml"))

    def test_hosted_keeps_the_trailing_newline(self):
        grove = os.path.join(self.root, "grove", "crdt-dart")
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_grove
            version: 1.0.0-dev
            dependencies:
              forge_client:
                path: ../forge_client
        """)
        result = run(self.pkg, "--hosted", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.read("pubspec.yaml").endswith("  forge_client: ^1.0.0-dev\n"))

    def test_version_stamps_the_package_and_every_path_dependency(self):
        # Lockstep: v1.13.0 publishes 1.13.0 of everything, so a sibling's
        # committed 1.0.0-dev must not leak into the constraint.
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--version", "1.13.0", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        pubspec = self.read("pubspec.yaml")
        self.assertTrue(pubspec.startswith("name: forge_client_grove\nversion: 1.13.0\nenvironment:\n"))
        self.assertIn("  forge_client: ^1.13.0\n", pubspec)
        self.assertNotIn("1.0.0-dev", pubspec.replace("grove_crdt: ^1.0.0-dev", ""))
        self.assertNotIn("publish_to", pubspec)
        self.assertIn("  meta: ^1.19.0\n", pubspec)

    def test_version_leaves_a_git_dependency_at_its_own_version(self):
        # grove_crdt is grove's package, not one of forge's lockstepped ones.
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--version", "1.13.0", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("  grove_crdt: ^1.0.0-dev\n", self.read("pubspec.yaml"))

    def test_version_stamps_dev_dependencies_too(self):
        write(os.path.join(self.root, "forge_client_offline", "pubspec.yaml"), """
            name: forge_client_offline
            version: 1.0.0-dev
        """)
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_flutter
            version: 1.0.0-dev
            publish_to: none
            dependencies:
              forge_client:
                path: ../forge_client
            dev_dependencies:
              forge_client_offline:
                path: ../forge_client_offline
        """)
        result = run(self.pkg, "--hosted", "--version", "2.0.1")
        self.assertEqual(result.returncode, 0, result.stderr)
        pubspec = self.read("pubspec.yaml")
        self.assertIn("  forge_client: ^2.0.1\n", pubspec)
        self.assertIn("  forge_client_offline: ^2.0.1\n", pubspec)

    def test_version_keeps_the_overrides_on_the_local_copies(self):
        # The dry run resolves a sibling that is not on pub.dev at ^X.Y.Z
        # through these.
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--version", "1.13.0", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"  forge_client:\n    path: {os.path.join(self.root, 'forge_client')}\n",
                      self.read("pubspec_overrides.yaml"))

    def test_version_without_hosted_only_changes_the_version(self):
        before = self.read("pubspec.yaml")
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--version", "1.13.0", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.read("pubspec.yaml"), before.replace("version: 1.0.0-dev", "version: 1.13.0"))

    def test_version_rejects_a_leading_v_and_non_versions(self):
        for bad in ("v1.13.0", "1.13", "latest", "", "01.2.3", "1.02.3", "1.2.03"):
            with self.subTest(version=bad):
                result = run(self.pkg, "--hosted", "--version", bad)
                self.assertEqual(result.returncode, 1)
                self.assertIn("--version wants X.Y.Z", result.stderr)
        self.assertNotIn("1.13", self.read("pubspec.yaml"))

    def test_changelog_gets_a_section_for_the_version(self):
        # pub warns "CHANGELOG.md doesn't mention current version", and the
        # dry run exits 65 on any warning.
        write(os.path.join(self.pkg, "CHANGELOG.md"), """
            ## 1.0.0-dev

            - Initial release.
        """)
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--version", "1.13.0", "--changelog", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        changelog = self.read("CHANGELOG.md")
        self.assertTrue(changelog.startswith("## 1.13.0\n\n- Released with forge v1.13.0"))
        self.assertIn("https://github.com/xraph/forge/releases/tag/v1.13.0", changelog)
        self.assertTrue(changelog.endswith("## 1.0.0-dev\n\n- Initial release.\n"))

    def test_changelog_keeps_notes_already_written_for_the_version(self):
        notes = "## 1.13.0\n\n- Real notes.\n\n## 1.0.0-dev\n\n- Initial release.\n"
        with open(os.path.join(self.pkg, "CHANGELOG.md"), "w", encoding="utf-8") as f:
            f.write(notes)
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--hosted", "--version", "1.13.0", "--changelog", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.read("CHANGELOG.md"), notes)

    def test_version_accepts_zero_components_and_prereleases(self):
        for good in ("0.1.0", "1.0.0", "10.20.30", "1.13.0-rc.1"):
            with self.subTest(version=good):
                result = run(self.pkg, "--version", good, "--override",
                             f"grove_crdt={os.path.join(self.root, 'grove', 'crdt-dart')}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(f"version: {good}\n", self.read("pubspec.yaml"))

    def test_changelog_recognises_the_common_heading_styles(self):
        # Prepending a generic section above real notes would show the
        # version twice on pub.dev, boilerplate first.
        grove = os.path.join(self.root, "grove", "crdt-dart")
        for heading in (
            "## [1.13.0](https://github.com/xraph/forge/compare/v1.12.0...v1.13.0) (2026-10-07)",
            "## [1.13.0] - 2026-10-07",
            "## v1.13.0",
            "# 1.13.0",
            "### 1.13.0",
            "## 1.13.0 (2026-10-07)",
        ):
            with self.subTest(heading=heading):
                notes = f"{heading}\n\n- Real notes.\n\n## 1.0.0-dev\n\n- Initial release.\n"
                with open(os.path.join(self.pkg, "CHANGELOG.md"), "w", encoding="utf-8") as f:
                    f.write(notes)
                result = run(self.pkg, "--version", "1.13.0", "--changelog", "--override", f"grove_crdt={grove}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(self.read("CHANGELOG.md"), notes)

    def test_changelog_ignores_deeper_headings_and_other_versions(self):
        grove = os.path.join(self.root, "grove", "crdt-dart")
        for heading in ("#### 1.13.0", "## 1.13.01", "## 11.13.0", "## [1.13.0-rc.1](https://x)"):
            with self.subTest(heading=heading):
                with open(os.path.join(self.pkg, "CHANGELOG.md"), "w", encoding="utf-8") as f:
                    f.write(f"{heading}\n\n- Other.\n")
                result = run(self.pkg, "--version", "1.13.0", "--changelog", "--override", f"grove_crdt={grove}")
                self.assertEqual(result.returncode, 0, result.stderr)
                changelog = self.read("CHANGELOG.md")
                self.assertTrue(changelog.startswith("## 1.13.0\n\n- Released with forge v1.13.0"))
                self.assertEqual(changelog.count("- Released with forge"), 1)

    def test_changelog_does_not_take_a_longer_version_for_this_one(self):
        notes = "## 1.13.0-rc.1\n\n- Candidate.\n"
        with open(os.path.join(self.pkg, "CHANGELOG.md"), "w", encoding="utf-8") as f:
            f.write(notes)
        grove = os.path.join(self.root, "grove", "crdt-dart")
        result = run(self.pkg, "--version", "1.13.0", "--changelog", "--override", f"grove_crdt={grove}")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.read("CHANGELOG.md").startswith("## 1.13.0\n\n"))

    def test_changelog_needs_a_version(self):
        result = run(self.pkg, "--hosted", "--changelog")
        self.assertEqual(result.returncode, 1)
        self.assertIn("--changelog needs --version", result.stderr)

    def test_missing_override_directory_fails(self):
        result = run(self.pkg, "--override", "grove_crdt=/nonexistent")
        self.assertEqual(result.returncode, 1)
        self.assertIn("has no pubspec.yaml", result.stderr)

    def test_print_ref_prints_the_git_ref_and_writes_nothing(self):
        before = self.read("pubspec.yaml")
        result = run(self.pkg, "--print-ref", "grove_crdt")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "11453d954cbb830703813e4a8ec858ad80862220\n")
        self.assertEqual(self.read("pubspec.yaml"), before)
        self.assertFalse(os.path.exists(os.path.join(self.pkg, "pubspec_overrides.yaml")))

    def test_print_ref_fails_on_a_path_dependency(self):
        result = run(self.pkg, "--print-ref", "forge_client")
        self.assertEqual(result.returncode, 1)
        self.assertIn("forge_client is not a git dependency", result.stderr)

    def test_print_ref_fails_on_a_git_dependency_without_a_ref(self):
        write(os.path.join(self.pkg, "pubspec.yaml"), """
            name: forge_client_grove
            version: 1.0.0-dev
            dependencies:
              grove_crdt:
                git:
                  url: https://github.com/xraph/grove.git
                  path: crdt-dart
        """)
        result = run(self.pkg, "--print-ref", "grove_crdt")
        self.assertEqual(result.returncode, 1)
        self.assertIn("grove_crdt has no ref:", result.stderr)

    def test_the_real_grove_pin_is_readable(self):
        package = os.path.join(os.path.dirname(SCRIPT), "..", "..", "dart-packages", "forge_client_grove")
        result = run(package, "--print-ref", "grove_crdt")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(result.stdout, r"^\S+\n$")


if __name__ == "__main__":
    unittest.main()
