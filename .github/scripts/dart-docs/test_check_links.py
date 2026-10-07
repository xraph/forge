"""Tests for check_links.py. Run: python3 -m unittest discover -s .github/scripts/dart-docs -p 'test_*.py'"""

import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check_links.py")


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        f.write(textwrap.dedent(text).lstrip())


class CheckLinksTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        write(os.path.join(self.root, "web-client", "runtime.mdx"), """
            ---
            title: Runtime
            ---

            ## Time-based freshness

            ```md
            ## Not a heading
            ```
        """)
        write(os.path.join(self.root, "web-client", "index.mdx"), "---\ntitle: Web\n---\n")
        self.page = os.path.join(self.root, "dart-client", "index.mdx")

    def check(self, body, *extra):
        write(self.page, body)
        return subprocess.run([sys.executable, SCRIPT, self.root, *extra, self.page], capture_output=True, text=True)

    def test_page_and_heading_resolve(self):
        result = self.check("[a](/docs/web-client/runtime#time-based-freshness) and [b](/docs/web-client)\n")
        self.assertEqual(result.returncode, 0, result.stdout)

    def test_card_href_is_checked(self):
        result = self.check('<Card title="x" href="/docs/web-client/missing" />\n')
        self.assertEqual(result.returncode, 1)
        self.assertIn("/docs/web-client/missing (no page)", result.stdout)

    def test_heading_inside_a_code_fence_does_not_count(self):
        result = self.check("[a](/docs/web-client/runtime#not-a-heading)\n")
        self.assertEqual(result.returncode, 1)
        self.assertIn("no heading #not-a-heading", result.stdout)

    def test_pending_page_does_not_fail(self):
        result = self.check("[s](/docs/dart-client/sync)\n", "--pending", "/docs/dart-client/sync,/docs/dart-client/devtools")
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("/docs/dart-client/sync (pending)", result.stdout)

    def test_titled_single_quoted_and_braced_links_are_checked(self):
        for body in (
            '[a](/docs/web-client/missing "a title")\n',
            "<Card href='/docs/web-client/missing' />\n",
            '<Card href={"/docs/web-client/missing"} />\n',
        ):
            result = self.check(body)
            self.assertEqual(result.returncode, 1, body)
            self.assertIn("/docs/web-client/missing (no page)", result.stdout)

    def test_titled_link_that_resolves_passes(self):
        result = self.check('[a](/docs/web-client/runtime#time-based-freshness "why")\n')
        self.assertEqual(result.returncode, 0, result.stdout)

    def test_same_page_anchor_is_checked(self):
        result = self.check("## Real heading\n\n[ok](#real-heading) and [bad](#nowhere)\n")
        self.assertEqual(result.returncode, 1)
        self.assertIn("#nowhere (no heading #nowhere on this page)", result.stdout)
        self.assertNotIn("#real-heading", result.stdout)

    def test_slug_drops_punctuation_and_backticks_and_keeps_double_hyphen(self):
        write(os.path.join(self.root, "web-client", "slugs.mdx"), """
            ---
            title: Slugs
            ---

            ## `useQuery`, what's new? v1.2 (API & SDK)
        """)
        result = self.check("[a](/docs/web-client/slugs#usequery-whats-new-v12-api--sdk)\n")
        self.assertEqual(result.returncode, 0, result.stdout)

    def test_link_inside_a_code_fence_is_not_checked(self):
        result = self.check("Text.\n\n```md\n[x](/docs/web-client/missing)\n```\n\n    ~~~md\n    [y](/docs/web-client/missing)\n    ~~~\n")
        self.assertEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main()
