"""Parses a ЖQL request and prints the result as JSON.

    zhql "clone reddit.com, call it Savor"   parse the arguments as one request
    zhql savor.zhql                          parse a query file
    echo "call it Savor" | zhql              parse standard input

--pretty indents the output. --phrasebook FILE reads a JSON object of phrases
and their meanings. Exits 1 when the request has errors, 2 when the phrasebook
file is unusable. Also runs as: python -m zhql
"""

import json
import os
import sys

from zhql import parse


def _read_phrasebook(path):
    """A JSON object whose values are all strings, or None."""
    try:
        with open(path, "rb") as f:
            book = json.loads(f.read().decode("utf-8"))
        if not isinstance(book, dict):
            return None
        parse("", book)  # rejects meanings that are not strings, and invalid Unicode
        return book
    except (OSError, UnicodeDecodeError, ValueError, TypeError, RecursionError):
        return None


def _read_file(path):
    with open(path, "rb") as f:
        return f.read()


def _read_or_exit(read, name):
    try:
        return read()
    except OSError:
        sys.stderr.write("zhql: cannot read %s.\n" % name)
        sys.exit(2)


def main(argv=None):
    args = sys.argv[1:] if argv is None else argv
    pretty = False
    phrasebook_file = None
    rest = []
    i = 0
    while i < len(args):
        if args[i] == "--pretty":
            pretty = True
        elif args[i] == "--phrasebook":
            if i + 1 == len(args):
                sys.stderr.write("zhql: --phrasebook needs a file.\n")
                return 2
            i += 1
            phrasebook_file = args[i]
        else:
            rest.append(args[i])
        i += 1

    phrasebook = None
    if phrasebook_file is not None:
        phrasebook = _read_phrasebook(phrasebook_file) if os.path.isfile(phrasebook_file) else None
        if phrasebook is None:
            sys.stderr.write("zhql: %s is not a phrasebook (a JSON object of phrases and meanings).\n" % phrasebook_file)
            return 2

    # Files and standard input are passed to parse() as bytes, and arguments keep
    # undecodable bytes as lone surrogates, so text that is not UTF-8 is reported
    # as invalid_encoding.
    if len(rest) == 1 and rest[0].endswith(".zhql") and os.path.isfile(rest[0]):
        path = rest[0]
        request = _read_or_exit(lambda: _read_file(path), path)
    elif not rest or rest == ["-"]:
        request = _read_or_exit(sys.stdin.buffer.read, "standard input")
    else:
        request = " ".join(rest)

    result = parse(request, phrasebook)
    output = json.dumps(result, ensure_ascii=False, indent=4 if pretty else None, separators=None if pretty else (",", ":"))
    sys.stdout.buffer.write((output + "\n").encode("utf-8"))
    return 1 if result["errors"] else 0


if __name__ == "__main__":
    sys.exit(main())
