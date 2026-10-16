"""ЖQL (zhql) reference parser for Python.

The behaviour is defined by SPEC.md and checked against conformance/; keep it in
step with js/, php/ and go/, which follow the same structure function by function.
"""

__all__ = ["KEYWORDS", "VERSION", "parse"]

VERSION = "0.3"

WHITESPACE = frozenset(
    chr(c)
    for c in (0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000)
) | frozenset(chr(c) for c in range(0x2000, 0x200B))
LINE_BREAKS = frozenset(chr(c) for c in (0x0A, 0x0B, 0x0C, 0x0D, 0x85, 0x2028, 0x2029))
QUOTES = {'"': '"', chr(0x201C): chr(0x201D)}  # straight, and curly U+201C to U+201D
STOPS = ".!?"
BOM = chr(0xFEFF)

# Punctuation tokens. All of them join clauses; inside a list they also separate items.
MARKS = frozenset(("comma", "semicolon", "newline", "stop"))

# Longest phrases first, so "make it look like" wins over "make it".
KEYWORDS = (
    (("make", "it", "look", "like"), "look_like"),
    (("tailor", "it", "to"), "tailor"),
    (("tailor", "it", "for"), "tailor"),
    (("make", "it"), "make"),
    (("call", "it"), "name"),
    (("name", "it"), "name"),
    (("clone",), "clone"),
    (("with",), "with"),
    (("use",), "use"),
    (("only",), "only"),
    (("ingest",), "ingest"),
    (("infer",), "infer"),
    (("generate",), "generate"),
    (("invent",), "invent"),
)

CONNECTOR_WORDS = frozenset(("and", "but", "then"))

# A phrase may not contain these words or any keyword, so it can never take over
# or swallow a built-in.
RESERVED_WORDS = CONNECTOR_WORDS | {"instead"}
MAX_PHRASE_WORDS = 16  # bounds how far a phrase match can look ahead

# Keywords by first word, so checking a token doesn't try all of them.
_KEYWORDS_BY_FIRST_WORD = {}
for _keyword in KEYWORDS:
    _KEYWORDS_BY_FIRST_WORD.setdefault(_keyword[0][0], []).append(_keyword)

SCALAR_FIELD = {
    "clone": "ref", "look_like": "ref", "make": "trait", "name": "name", "tailor": "audience", "text": "text",
    "infer": "subject", "generate": "subject", "invent": "subject",
}

MESSAGES = {
    "empty_request": "The request is empty.",
    "invalid_encoding": "The request is not valid Unicode text.",
    "unterminated_quote": "A quoted value is missing its closing quote.",
}

# str.lower() would also fold letters such as İ, which the spec does not allow.
_ASCII_LOWER = {c: c + 32 for c in range(ord("A"), ord("Z") + 1)}


def _ascii_lower(s):
    return s.translate(_ASCII_LOWER)


def _well_formed(s):
    try:
        s.encode("utf-8")
    except UnicodeEncodeError:  # lone surrogates, e.g. from undecodable bytes
        return False
    return True


def _error(code, start, end, phrase=""):
    message = '"%s" needs a value.' % phrase if code == "missing_value" else MESSAGES[code]
    return {"code": code, "message": message, "span": [start, end]}


def _with_via(obj, via):
    if via:
        obj["via"] = via
    return obj


def parse(request, phrasebook=None):
    """Parse one ЖQL request, such as the content of a .zhql file, given as str
    or as UTF-8 bytes.

    phrasebook, when given, is a dict of phrases and what they mean: a keyword
    ("spin up": "clone") or whole clauses ("my usual stack": "with laravel,
    postgres and relay"). Entries that break the rules are skipped and reported
    in "phrasebook_problems".
    """
    if not isinstance(request, (str, bytes, bytearray)):
        raise TypeError("zhql: the request must be a string or UTF-8 bytes")
    phrases, problems = _read_phrasebook(phrasebook)
    if isinstance(request, (bytes, bytearray)):
        try:
            request = bytes(request).decode("utf-8")
        except UnicodeDecodeError:
            request = None
    if request is None or not _well_formed(request):
        result = {"zhql": VERSION, "clauses": [], "errors": [_error("invalid_encoding", 0, 0)]}
    else:
        result = _Parser(request, phrases).run()
    if problems:
        result["phrasebook_problems"] = problems
    return result


# Phrases are kept in a tree of words, so finding the longest phrase at a token
# costs no more than the words it walks.
def _new_node():
    return {"next": {}, "entry": None}


def _read_phrasebook(book):
    root = _new_node()
    if book is None:
        return root, []
    if not isinstance(book, dict):
        raise TypeError("zhql: a phrasebook must be a dict of phrases and meanings")
    for phrase, means in book.items():
        if not isinstance(phrase, str) or not isinstance(means, str):
            raise TypeError("zhql: every phrase and meaning in a phrasebook must be a string")
        if not _well_formed(phrase) or not _well_formed(means):
            raise ValueError("zhql: a phrasebook must be valid Unicode text")

    problems = []
    for phrase in sorted(book):  # str order is code point order
        entry, problem = _read_entry(phrase, book[phrase])
        if problem:
            problems.append(problem)
            continue
        node = root
        for word in entry["words"]:
            node = node["next"].setdefault(word, _new_node())
        node["entry"] = entry  # two valid phrases never have the same words
    return root, problems


def _read_entry(phrase, means):
    """Checks one entry. Depends on nothing but the entry."""

    def bad(message):
        return None, {"code": "invalid_phrase", "message": '"%s" %s' % (phrase, message), "phrase": phrase}

    tokens = _Parser(phrase).tokens
    words = [t["value"] for t in tokens]
    if not tokens or any(t["kind"] != "word" for t in tokens) or " ".join(words) != phrase:
        return bad("must be plain words separated by single spaces.")
    if len(words) > MAX_PHRASE_WORDS:
        return bad("must be at most %d words." % MAX_PHRASE_WORDS)
    if any("A" <= ch <= "Z" for ch in phrase):
        return bad("must be written in lower case.")
    for word in words:
        if word in RESERVED_WORDS:
            return bad('contains the reserved word "%s".' % word)
    for i in range(len(words)):
        for kw_words, _ in KEYWORDS:
            if tuple(words[i:i + len(kw_words)]) == kw_words:
                return bad('contains the keyword "%s".' % " ".join(kw_words))

    for keyword in KEYWORDS:
        if _ascii_lower(means) == " ".join(keyword[0]):
            return {"phrase": phrase, "means": means, "words": words, "alias": keyword}, None

    plain = parse(means)
    usable = plain["clauses"] and not plain["errors"] and all(c["type"] not in ("text", "ingest") for c in plain["clauses"])
    if not usable:
        return bad('must mean a keyword or whole clauses, not "%s".' % means)
    clauses = [{k: v for k, v in c.items() if k != "span"} for c in plain["clauses"]]
    list_type = clauses[0]["type"] if len(clauses) == 1 else None
    return {"phrase": phrase, "means": means, "words": words, "clauses": clauses, "list_type": list_type}, None


class _Parser:
    def __init__(self, request, phrases=None):
        self.cps = request  # a Python str indexes by code point already
        self.phrases = phrases or _new_node()
        self.errors = []
        self.tokens = self._tokenize()
        self.n = len(self.tokens)

    def _tokenize(self):
        cps = self.cps
        n = len(cps)
        tokens = []
        i = 1 if cps[:1] == BOM else 0
        line_start = True

        while i < n:
            ch = cps[i]
            if ch in LINE_BREAKS:
                tokens.append({"kind": "newline", "value": ch, "start": i, "end": i + 1})
                line_start = True
                i += 1
                continue
            if ch in WHITESPACE:
                i += 1
                continue
            # "# " as the first thing on a line comments out the rest of that line.
            if line_start and ch == "#" and (i + 1 == n or cps[i + 1] in WHITESPACE):
                while i < n and cps[i] not in LINE_BREAKS:
                    i += 1
                continue
            line_start = False

            if ch in (",", ";"):
                kind = "comma" if ch == "," else "semicolon"
                tokens.append({"kind": kind, "value": ch, "start": i, "end": i + 1})
                i += 1
                continue

            if ch in QUOTES:
                close = QUOTES[ch]
                j = i + 1
                value = []
                closed = False
                while j < n:
                    if cps[j] == "\\" and j + 1 < n:
                        value.append(cps[j + 1])
                        j += 2
                        continue
                    if cps[j] == close:
                        closed = True
                        j += 1
                        break
                    value.append(cps[j])
                    j += 1
                if not closed:
                    self.errors.append(_error("unterminated_quote", i, n))
                tokens.append({"kind": "quoted", "value": "".join(value), "start": i, "end": j})
                i = j
                continue

            j = i
            while j < n and cps[j] not in WHITESPACE and cps[j] not in (",", ";"):
                j += 1
            # Sentence punctuation that ends a word is a separate "stop" token.
            stop = j
            while stop > i and cps[stop - 1] in STOPS:
                stop -= 1
            if stop > i:
                tokens.append({"kind": "word", "value": cps[i:stop], "start": i, "end": stop})
            if stop < j:
                tokens.append({"kind": "stop", "value": cps[stop:j], "start": stop, "end": j})
            i = j

        return tokens

    def is_word(self, k, word):
        return k < self.n and self.tokens[k]["kind"] == "word" and _ascii_lower(self.tokens[k]["value"]) == word

    def is_connector(self, k):
        if k >= self.n:
            return False
        token = self.tokens[k]
        return token["kind"] in MARKS or (token["kind"] == "word" and _ascii_lower(token["value"]) in CONNECTOR_WORDS)

    def keyword_at(self, k):
        for words, kind in _KEYWORDS_BY_FIRST_WORD.get(self.word_at(k), ()):  # longest first, as in KEYWORDS
            if all(self.is_word(k + offset, word) for offset, word in enumerate(words)):
                return words, kind
        return None

    def word_at(self, k):
        if k >= self.n or self.tokens[k]["kind"] != "word":
            return None
        return _ascii_lower(self.tokens[k]["value"])

    def longest_phrase(self, k):
        """The longest phrase whose words start at token k."""
        node = self.phrases
        found = None
        i = k
        while self.word_at(i) in node["next"]:
            node = node["next"][self.word_at(i)]
            found = node["entry"] or found
            i += 1
        return found

    def match(self, entry, k):
        span = [self.tokens[k]["start"], self.tokens[k + len(entry["words"]) - 1]["end"]]
        return {"phrase": entry["phrase"], "means": entry["means"], "span": span}

    def start_at(self, k):
        """What starts a clause at token k: a keyword, else the longest phrase, else nothing."""
        keyword = self.keyword_at(k)
        if keyword is not None:
            return {"keyword": keyword, "length": len(keyword[0])}
        entry = self.longest_phrase(k)
        if entry is None:
            return None
        if "alias" in entry:
            return {"keyword": entry["alias"], "length": len(entry["words"]), "entry": entry}
        return {"shorthand": entry, "length": len(entry["words"])}

    def list_shorthand(self, a, b, kind):
        """A one-clause shorthand of the list's own type, standing exactly for tokens a..b-1."""
        node = self.phrases
        for i in range(a, b):
            node = node["next"].get(self.word_at(i))
            if node is None:
                return None
        entry = node["entry"]
        return entry if entry and entry.get("list_type") == kind else None

    def boundary(self, start, kind):
        """A clause ends at a run of connectors followed by a keyword, a phrase, or the
        end of the request. A clone reference also ends at "with" or an alias of it.
        Inside a with/only list, a one-clause shorthand of the same type joins the list."""
        j = start
        while j < self.n:
            if kind == "clone":
                found = self.start_at(j)
                if found and "keyword" in found and found["keyword"][1] == "with":
                    return j
            if self.is_connector(j):
                k = j
                while k < self.n and self.is_connector(k):
                    k += 1
                if k == self.n:
                    return j
                found = self.start_at(k)
                joins_list = found and "shorthand" in found and kind in ("with", "only") and found["shorthand"]["list_type"] == kind
                if found and not joins_list:
                    return j
                j = k
                continue
            j += 1
        return self.n

    def scalar(self, a, b):
        """A value is its tokens' source text, with every gap between them (whitespace,
        line breaks, comments) written as one space. A lone quoted token is unquoted."""
        parts = [t for t in self.tokens[a:b] if t["kind"] != "newline"]
        first = 0
        while first < len(parts) and parts[first]["kind"] in MARKS:
            first += 1
        parts = parts[first:]
        if len(parts) == 1 and parts[0]["kind"] == "quoted":
            return parts[0]["value"]
        out = []
        prev_end = -1
        for t in parts:
            if prev_end != -1 and t["start"] > prev_end:
                out.append(" ")
            out.append(self.cps[t["start"]:t["end"]])
            prev_end = t["end"]
        return "".join(out)

    def items(self, a, b, kind, via):
        marked = any(self.tokens[k]["kind"] in MARKS for k in range(a, b))
        items = []
        group_start = a
        for k in range(a, b + 1):
            if k < b and not self.is_connector(k):
                continue
            if k > group_start:
                shorthand = self.list_shorthand(group_start, k, kind)
                if shorthand:
                    items.extend(shorthand["clauses"][0]["items"])
                    via.append(self.match(shorthand, group_start))
                elif marked:
                    items.append(self.scalar(group_start, k))
                else:
                    items.extend(self.tokens[t]["value"] for t in range(group_start, k))
            group_start = k + 1
        return [item for item in items if item != ""]

    def run(self):
        tokens = self.tokens
        clauses = []

        pos = 0
        while pos < self.n:
            while pos < self.n and self.is_connector(pos):
                pos += 1
            if pos >= self.n:
                break

            found = self.start_at(pos)
            if found is None:
                end = self.boundary(pos, "text")
                text = self.scalar(pos, end)
                if text != "":
                    clauses.append({"type": "text", "text": text, "span": [tokens[pos]["start"], tokens[end - 1]["end"]]})
                pos = end
                continue

            if "shorthand" in found:
                m = self.match(found["shorthand"], pos)
                for fields in found["shorthand"]["clauses"]:
                    clause = {k: (list(v) if isinstance(v, list) else v) for k, v in fields.items()}
                    clause["span"] = list(m["span"])
                    clause["via"] = [dict(m, span=list(m["span"]))]
                    clauses.append(clause)
                pos += found["length"]
                continue

            words, kind = found["keyword"]
            entry = found.get("entry")
            phrase = entry["phrase"] if entry else " ".join(words)
            via = [self.match(entry, pos)] if entry else []
            start = tokens[pos]["start"]
            kw_end = tokens[pos + found["length"] - 1]["end"]
            value_start = pos + found["length"]

            def missing():
                self.errors.append(_with_via(_error("missing_value", start, kw_end, phrase), via))

            if kind == "ingest":
                token = tokens[value_start] if value_start < self.n else None
                if token is None or token["kind"] not in ("word", "quoted") or token["value"] == "":
                    missing()
                    pos = value_start
                else:
                    clauses.append(_with_via({"type": "ingest", "path": token["value"], "span": [start, token["end"]]}, via))
                    pos = value_start + 1
                continue

            end = self.boundary(value_start, kind)
            span = [start, tokens[end - 1]["end"] if end > value_start else kw_end]

            if kind in ("with", "only"):
                items = self.items(value_start, end, kind, via)
                if items:
                    clauses.append(_with_via({"type": kind, "items": items, "span": span}, via))
                else:
                    missing()
            elif kind == "use":
                split = -1
                for k in range(value_start, end - 1):
                    if self.is_word(k, "instead") and self.is_word(k + 1, "of"):
                        split = k
                        break
                # Connectors just before "instead of" belong to neither part.
                value_end = end if split == -1 else split
                while value_end > value_start and self.is_connector(value_end - 1):
                    value_end -= 1
                value = self.scalar(value_start, value_end)
                instead_of = None if split == -1 else self.scalar(split + 2, end)
                if value == "":
                    missing()
                if instead_of == "":
                    error = _error("missing_value", tokens[split]["start"], tokens[split + 1]["end"], "instead of")
                    self.errors.append(_with_via(error, via))
                if value != "" and instead_of != "":
                    clauses.append(_with_via({"type": "use", "value": value, "instead_of": instead_of, "span": span}, via))
            else:
                value = self.scalar(value_start, end)
                if value != "":
                    clauses.append(_with_via({"type": kind, SCALAR_FIELD[kind]: value, "span": span}, via))
                else:
                    missing()
            pos = end

        if not clauses and not self.errors:
            self.errors.append(_error("empty_request", 0, len(self.cps)))
        errors = sorted(self.errors, key=lambda e: (e["span"][0], e["span"][1], e["code"]))

        return {"zhql": VERSION, "clauses": clauses, "errors": errors}
