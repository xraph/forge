"""Tests for check_components.py. Run: python3 -m unittest discover -s .github/scripts/dart-docs -p 'test_*.py'"""

import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check_components.py")


class CheckComponentsTest(unittest.TestCase):
    def run_on(self, *pages):
        paths = []
        tmp = tempfile.mkdtemp()
        for i, page in enumerate(pages):
            path = os.path.join(tmp, f"p{i}.mdx")
            with open(path, "w", encoding="utf-8") as f:
                f.write(textwrap.dedent(page).lstrip())
            paths.append(path)
        return subprocess.run([sys.executable, SCRIPT, *paths], capture_output=True, text=True)

    def test_allowed_components_pass(self):
        r = self.run_on("""
            <Callout type="info">Words.</Callout>

            <Cards>
              <Card title="A" href="/docs/a">Text.</Card>
            </Cards>

            <Steps>
              <Step>One.</Step>
            </Steps>

            <Tabs items={['a', 'b']}>
              <Tab value="a">
            ```dart
            final x = 1;
            ```
              </Tab>
              <Tab value="b">

            ```go
            x := 1
            ```

              </Tab>
            </Tabs>
        """)
        self.assertEqual(r.returncode, 0, r.stdout)
        self.assertIn("components ok", r.stdout)

    def test_an_unknown_component_fails_and_is_named(self):
        r = self.run_on("<Accordion>Hi</Accordion>\n")
        self.assertEqual(r.returncode, 1)
        self.assertIn("<Accordion>", r.stdout)

    def test_tags_in_code_are_examples(self):
        r = self.run_on("""
            Write `<Widget>` in prose.

            ```dart
            Column(children: [<Widget>[]])
            ```
        """)
        self.assertEqual(r.returncode, 0, r.stdout)

    def test_a_tab_with_prose_fails(self):
        r = self.run_on("""
            <Tabs items={['a']}>
              <Tab value="a">
            Use the builder.

            ```dart
            final x = 1;
            ```
              </Tab>
            </Tabs>
        """)
        self.assertEqual(r.returncode, 1)
        self.assertIn("<Tab>", r.stdout)

    def test_a_tab_with_two_code_blocks_fails(self):
        r = self.run_on("""
            <Tab value="a">
            ```dart
            a
            ```
            ```dart
            b
            ```
            </Tab>
        """)
        self.assertEqual(r.returncode, 1)

    def test_an_empty_tab_fails(self):
        r = self.run_on("<Tab value=\"a\"></Tab>\n")
        self.assertEqual(r.returncode, 1)

    def test_every_page_is_checked(self):
        r = self.run_on("fine\n", "<Bad />\n")
        self.assertEqual(r.returncode, 1)

    def test_no_files_is_a_usage_error(self):
        r = subprocess.run([sys.executable, SCRIPT], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2)


if __name__ == "__main__":
    unittest.main()
