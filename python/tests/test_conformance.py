"""Runs every case in ../../conformance against the Python parser.

Usage: python3 -m unittest discover -s python/tests
"""

import json
import pathlib
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "python" / "src"))

from zhql import parse  # noqa: E402


class Conformance(unittest.TestCase):
    def test_cases(self):
        files = sorted((ROOT / "conformance").glob("*.json"))
        self.assertTrue(files, "no conformance files found")
        for path in files:
            suite = json.loads(path.read_text(encoding="utf-8"))
            for case in suite["cases"]:
                with self.subTest(suite=path.stem, case=case["name"]):
                    actual = parse(case["input"], case.get("phrasebook"))
                    self.assertEqual(json.dumps(actual), json.dumps(case["expect"]))

    # Not expressible in JSON fixtures: text that is not valid Unicode, and
    # phrasebooks that are not phrasebooks.
    def test_lone_surrogate_is_invalid_encoding(self):
        request = b"call it \xc3\x28".decode("utf-8", "surrogateescape")
        result = parse(request)
        self.assertEqual(result["clauses"], [])
        self.assertEqual([e["code"] for e in result["errors"]], ["invalid_encoding"])
        self.assertEqual([p["phrase"] for p in parse(request, {"Ab": "clone"})["phrasebook_problems"]], ["Ab"])

    def test_request_can_be_utf8_bytes(self):
        self.assertEqual(json.dumps(parse("call it Ж".encode())), json.dumps(parse("call it Ж")))
        self.assertEqual(json.dumps(parse(bytearray(b"\xef\xbb\xbfcall it Savor"))), json.dumps(parse(chr(0xFEFF) + "call it Savor")))
        for bad in (b"\xff", b"\xc0\x80", b"\xed\xa0\x80", b"\xf4\x90\x80\x80"):
            result = parse(b"clone " + bad, {"Ab": "clone"})
            self.assertEqual([e["code"] for e in result["errors"]], ["invalid_encoding"])
            self.assertEqual([p["phrase"] for p in result["phrasebook_problems"]], ["Ab"])
        with self.assertRaises(TypeError):
            parse(42)

    def test_phrasebook_must_be_a_dict_of_strings(self):
        for book in ([], "clone", {"spin": 1}, {"spin": None}, {1: "clone"}):
            with self.assertRaises(TypeError):
                parse("x", book)
        for book in ({"sp" + chr(0xD800): "clone"}, {"spin": "cl" + chr(0xDC00)}):
            with self.assertRaises(ValueError):
                parse("x", book)


if __name__ == "__main__":
    unittest.main()
