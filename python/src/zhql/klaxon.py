"""Klaxon's agent dialect, compiled from the shared ZhQL token stream.

This module describes requests only. Parsing never probes a target or applies a fix.
"""

import re

from . import VERSION, _ascii_lower


_EXPECTED = (
    "Expected: klaxon probe <host> for <CVE> [and <CVE>], "
    "fix, report back, store as kit."
)
_CVE = re.compile(r"CVE-[0-9]{4}-[0-9]{4,}", re.ASCII | re.IGNORECASE)
_LABEL = re.compile(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", re.ASCII)


class _InvalidRequest(Exception):
    def __init__(self, pos):
        self.pos = pos


def parse_klaxon(parser, start):
    """Compile one Klaxon request, preserving code-point spans and lexical errors."""
    tokens = parser.tokens
    clauses = []
    errors = sorted(parser.errors, key=lambda error: (error["span"][0], error["span"][1], error["code"]))
    if errors:
        return {"zhql": VERSION, "dialect": "klaxon", "clauses": [], "errors": errors}

    def require_word(pos, word):
        if not parser.is_word(pos, word):
            raise _InvalidRequest(pos)
        return pos + 1

    def cve_at(pos):
        if pos >= parser.n or tokens[pos]["kind"] != "word":
            return None
        value = tokens[pos]["value"]
        return value.upper() if _CVE.fullmatch(value) else None

    try:
        pos = require_word(start + 1, "probe")
        probe_start = tokens[pos - 1]["start"]
        if pos >= parser.n or tokens[pos]["kind"] != "word":
            raise _InvalidRequest(pos)
        target = _ascii_lower(tokens[pos]["value"])
        if len(target) > 253 or any(not _LABEL.fullmatch(label) for label in target.split(".")):
            raise _InvalidRequest(pos)
        host_end = tokens[pos]["end"]
        pos += 1
        # The core tokenizer separates a hostname's optional final dot.
        if (pos < parser.n and tokens[pos]["kind"] == "stop"
                and tokens[pos]["value"] == "." and tokens[pos]["start"] == host_end):
            pos += 1
        pos = require_word(pos, "for")
        first_cve = cve_at(pos)
        if first_cve is None:
            raise _InvalidRequest(pos)
        cves = [first_cve]
        seen_cves = {first_cve}
        cve_end = tokens[pos]["end"]
        pos += 1
        while pos < parser.n and parser.is_connector(pos):
            following = pos
            while following < parser.n and parser.is_connector(following):
                following += 1
            next_cve = cve_at(following)
            if next_cve is None:
                break
            if next_cve not in seen_cves:
                seen_cves.add(next_cve)
                cves.append(next_cve)
            cve_end = tokens[following]["end"]
            pos = following + 1
        clauses.append({"type": "probe", "target": target, "cves": cves,
                        "span": [probe_start, cve_end]})
        seen = {"probe"}
        while pos < parser.n:
            if not parser.is_connector(pos):
                raise _InvalidRequest(pos)
            while pos < parser.n and parser.is_connector(pos):
                pos += 1
            if pos == parser.n:
                break
            step_start = pos
            if parser.is_word(pos, "fix"):
                kind, words = "fix", ("fix",)
            elif parser.is_word(pos, "report"):
                kind, words = "report_back", ("report", "back")
            elif parser.is_word(pos, "store"):
                kind, words = "store_kit", ("store", "as", "kit")
            else:
                raise _InvalidRequest(pos)
            if kind in seen:
                raise _InvalidRequest(step_start)
            for word in words:
                pos = require_word(pos, word)
            seen.add(kind)
            clauses.append({"type": kind, "span": [tokens[step_start]["start"], tokens[pos - 1]["end"]]})
    except _InvalidRequest as invalid:
        token = tokens[invalid.pos] if invalid.pos < parser.n else None
        span = [token["start"], token["end"]] if token else [len(parser.cps), len(parser.cps)]
        errors.append({"code": "invalid_agent_request", "message": _EXPECTED, "span": span})
    errors.sort(key=lambda error: (error["span"][0], error["span"][1], error["code"]))
    return {"zhql": VERSION, "dialect": "klaxon", "clauses": [] if errors else clauses, "errors": errors}
