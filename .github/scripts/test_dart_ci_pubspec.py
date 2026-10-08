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
