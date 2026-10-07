"""Tests for extract_snippets.py. Run: python3 -m unittest discover -s .github/scripts/dart-docs -p 'test_*.py'"""

import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "extract_snippets.py")

PAGE = textwrap.dedent('''
    ---
    title: Sample
    ---

    ```dart
    final ref = getOrder(const GetOrderArgs(id: '7'));
    ```

    <Tabs items={["a"]}>
    <Tab value="a">
    ```dart title="lib/main.dart"
    import 'package:flutter/material.dart';

    void main() => runApp(const SizedBox());
    ```
    </Tab>
    </Tabs>

    ```dart title="lib/src/ops.dart"
    const opGetOrder = OperationMeta(id: 'op_get_order', method: 'GET', path: '/orders/{id}');
    ```

    ```yaml
    name: not_dart
    ```
''').lstrip()


class ExtractSnippetsTest(unittest.TestCase):
    def setUp(self):
        self.docs = tempfile.mkdtemp()
        self.out = tempfile.mkdtemp()
        with open(os.path.join(self.docs, "flutter-adapter.mdx"), "w", encoding="utf-8") as f:
            f.write(PAGE)

    def run_script(self):
        return subprocess.run([sys.executable, SCRIPT, self.docs, self.out], capture_output=True, text=True)

    def read(self, name):
        with open(os.path.join(self.out, name), encoding="utf-8") as f:
            return f.read()

    def test_writes_one_file_per_checked_block(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sorted(os.listdir(self.out)), ["flutter_adapter_01.dart", "flutter_adapter_02.dart"])
        self.assertIn("wrote 2 blocks, skipped 1 generated excerpts", result.stdout)

    def test_fragment_gets_the_prelude(self):
        self.run_script()
        text = self.read("flutter_adapter_01.dart")
        self.assertIn("import 'package:orders_forge_client/orders_forge_client.dart';", text)
        self.assertTrue(text.rstrip().endswith("final ref = getOrder(const GetOrderArgs(id: '7'));"))
        self.assertIn("flutter-adapter.mdx, block 1, body starts at line 6", text)

    def test_whole_file_is_left_alone(self):
        self.run_script()
        text = self.read("flutter_adapter_02.dart")
        self.assertNotIn("orders_forge_client", text)
        self.assertIn("void main() => runApp(const SizedBox());", text)

    def test_no_blocks_is_an_error(self):
        os.remove(os.path.join(self.docs, "flutter-adapter.mdx"))
        result = self.run_script()
        self.assertEqual(result.returncode, 1)
        self.assertIn("no dart blocks found", result.stderr)

    def add(self, name, text):
        path = os.path.join(self.docs, name)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as f:
            f.write(textwrap.dedent(text).lstrip())

    def test_indented_fence_is_extracted_and_dedented(self):
        self.add("steps.mdx", """
            <Steps>
              <Step>
                ```dart
                final a = 1;
                  final b = 2;
                ```
              </Step>
            </Steps>
        """)
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        text = self.read("steps_01.dart")
        self.assertIn("\nfinal a = 1;\n  final b = 2;\n", text)

    def test_tilde_and_uppercase_fences_are_extracted(self):
        self.add("odd.mdx", "~~~dart\nfinal t = 1;\n~~~\n\n```Dart\nfinal u = 2;\n```\n")
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("final t = 1;", self.read("odd_01.dart"))
        self.assertIn("final u = 2;", self.read("odd_02.dart"))

    def test_unclosed_fence_fails_instead_of_passing_unchecked(self):
        self.add("broken.mdx", "```dart\nfinal lost = 1;\n")
        result = self.run_script()
        self.assertEqual(result.returncode, 1)
        self.assertIn("broken.mdx", result.stderr)
        self.assertIn("unclosed", result.stderr)

    def test_unclosed_fence_swallowing_the_next_block_fails(self):
        self.add("swallow.mdx", "```dart\nfinal a = 1;\n\n```dart\nfinal b = 2;\n```\n")
        result = self.run_script()
        self.assertEqual(result.returncode, 1)
        self.assertIn("swallow.mdx", result.stderr)

    def test_subdirectories_and_plain_markdown_are_read(self):
        self.add("deep/nested/page.mdx", "```dart\nfinal n = 1;\n```\n")
        self.add("notes.md", "```dart\nfinal m = 1;\n```\n")
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("final n = 1;", self.read("deep_nested_page_01.dart"))
        self.assertIn("final m = 1;", self.read("notes_01.dart"))

    def test_two_pages_writing_the_same_name_fail(self):
        self.add("a-b.mdx", "```dart\nfinal x = 1;\n```\n")
        self.add("a_b.mdx", "```dart\nfinal y = 1;\n```\n")
        result = self.run_script()
        self.assertEqual(result.returncode, 1)
        self.assertIn("a_b_01.dart", result.stderr)

    def test_header_counts_the_header_line_and_the_prelude(self):
        self.run_script()
        text = self.read("flutter_adapter_01.dart")
        self.assertIn("the header and prelude add 9 lines", text)
        first_body_line = text.split("\n").index("final ref = getOrder(const GetOrderArgs(id: '7'));") + 1
        self.assertEqual(first_body_line - 9, 1)


if __name__ == "__main__":
    unittest.main()
