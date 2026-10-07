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


if __name__ == "__main__":
    unittest.main()
