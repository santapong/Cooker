import importlib.util
import contextlib
import io
import json
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("dependencies", pathlib.Path(__file__).with_name("dependencies.py"))
scanner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scanner)


class DependencyGateTests(unittest.TestCase):
    def test_cli_fails_for_findings_and_incomplete_scans(self):
        for result, expected in [("findings", 1), ("network failure", 2)]:
            with self.subTest(result=result), tempfile.TemporaryDirectory() as temporary:
                output = pathlib.Path(temporary) / "report.json"
                package = {"package": {"ecosystem": "npm", "name": "example"}, "version": "1.0.0"}
                match = {**package, "advisories": [{"id": "GHSA-test"}]}
                with patch.object(sys, "argv", ["scanner", "--output", str(output)]), \
                     patch.object(scanner, "inventory", return_value=[package]), \
                     patch.object(scanner, "lookup") as lookup, \
                     contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                    if expected == 1:
                        lookup.return_value = ([match], 1)
                    else:
                        lookup.side_effect = TimeoutError("network failure")
                    self.assertEqual(scanner.main(), expected)
                self.assertEqual(json.loads(output.read_text())["status"],
                                 "findings" if expected == 1 else "incomplete")

    def test_affected_subpackage_added_to_build_reopens_the_finding(self):
        match = [{"package": {"ecosystem": "Go", "name": "example.com/lib"}, "version": "v1.0.0",
                  "advisories": [{"id": "GO-test"}]}]
        record = {"affected": [{"package": match[0]["package"],
                                "ecosystem_specific": {"imports": [{"path": "example.com/lib/unsafe"}]}}]}
        active, absent = scanner.check_go_imports(match, {"example.com/lib/safe"}, fetch=lambda _: record)
        self.assertEqual((len(active), len(absent)), (0, 1))
        active, absent = scanner.check_go_imports(match, {"example.com/lib/unsafe"}, fetch=lambda _: record)
        self.assertEqual((len(active), len(absent)), (1, 0))

    def test_missing_import_metadata_cannot_clear_an_advisory(self):
        match = [{"package": {"ecosystem": "Go", "name": "example.com/lib"}, "version": "v1.0.0",
                  "advisories": [{"id": "GO-test"}]}]
        active, absent = scanner.check_go_imports(match, {"example.com/lib/safe"}, fetch=lambda _: {})
        self.assertEqual((len(active), len(absent)), (1, 0))

    def test_pagination_keeps_a_later_advisory(self):
        responses = iter([{"results": [{"next_page_token": "second"}]},
                          {"results": [{"vulns": [{"id": "GHSA-test"}]}]}])
        findings, count = scanner.lookup(
            [{"package": {"ecosystem": "npm", "name": "example"}, "version": "1.0.0", "sources": []}],
            fetch=lambda _: next(responses))
        self.assertEqual(count, 2)
        self.assertEqual(findings[0]["advisories"][0]["id"], "GHSA-test")

    def test_partial_api_response_cannot_pass(self):
        with self.assertRaises(ValueError):
            scanner.lookup([{"package": {"ecosystem": "Go", "name": "example"}, "version": "v1.0.0"}],
                           fetch=lambda _: {"results": []})

    def test_endless_pagination_fails_instead_of_reporting_clean(self):
        with self.assertRaisesRegex(ValueError, "incomplete"):
            scanner.lookup([{"package": {}, "version": "1.0.0"}],
                           fetch=lambda _: {"results": [{"next_page_token": "again"}]})

    def test_dev_and_scoped_packages_are_included_and_stdlib_is_not_guessed(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            (root / "backend").mkdir()
            (root / "frontend").mkdir()
            (root / "backend/go.mod").write_text("module example\ngo 1.27.3\nrequire example.com/lib v1.0.0\n")
            (root / "frontend/package-lock.json").write_text(
                '{"lockfileVersion":3,"packages":{"":{},"node_modules/@scope/dev":'
                '{"version":"2.0.0","dev":true}}}')
            packages = scanner.inventory(root)
            self.assertEqual({p["package"]["name"] for p in packages}, {"example.com/lib", "@scope/dev"})
            with (root / "backend/go.mod").open("a") as stream:
                stream.write("replace example.com/lib => ../local\n")
            with self.assertRaisesRegex(ValueError, "resolved"):
                scanner.inventory(root)


if __name__ == "__main__":
    unittest.main()
