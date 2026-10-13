# ЖQL 0.2

ЖQL ("Zh", typed `zhql`) is the Zero-Hassle Query Language: a one-line request
for an application, written in plain words.

```
clone reddit.com, call it Savor, and tailor it to chefs
```

This document defines how a request is parsed. Every implementation in this
repository must produce exactly the output listed in `conformance/` for every
case. When this document and the conformance cases disagree, fix one of them;
don't let an implementation follow only one.

The parser is syntactic. It does not decide what `reddit.com` or `relay` means,
whether a capability exists, or whether two clauses conflict. That is the job of
whatever consumes the result.

## Input

A request is a string of Unicode text. Implementations read it as a sequence of
**code points**; every offset in the output counts code points from 0.

If the input is not valid Unicode (invalid UTF-8, or a lone UTF-16 surrogate),
the result has no clauses and one `invalid_encoding` error with span `[0, 0]`.

A `.zhql` file holds one request. Its whole content is the input.

## Tokens

The input is split into tokens, left to right.

| Input | Token |
|---|---|
| A **line break**: U+000A–U+000D, U+0085, U+2028, U+2029 | `newline`. A CR LF pair is two tokens. |
| Other **whitespace**: U+0009, U+0020, U+00A0, U+1680, U+2000–U+200A, U+202F, U+205F, U+3000 | Nothing; it only separates tokens. |
| U+FEFF as the very first code point | Nothing (a byte order mark). Offsets still count it. |
| `#` as the first non-whitespace code point of a line, followed by whitespace, a line break or the end of input | A **comment**: everything up to the next line break produces nothing. |
| `,` | `comma` |
| `;` | `semicolon` |
| `"` or `“` (U+201C) where a token starts | `quoted`, see below |
| Anything else | A run of code points up to the next whitespace, line break, `,` or `;` is a `word`. If the run ends with one or more of `.` `!` `?`, those code points are split off as a `stop` token. A run made only of them is just a `stop`. |

**Quoted tokens.** `"` closes with `"`; `“` closes with `”` (U+201D). Inside, a
backslash followed by any code point stands for that code point (`\"` is `"`,
`\\` is `\`). The token's value is the text between the quotes. If the input ends
before the closing quote, the token runs to the end, keeps the text read so far,
and an `unterminated_quote` error is reported from the opening quote to the end
of input. A quote character in the middle of a word is an ordinary character;
text right after a closing quote starts a new token. Single quotes are ordinary
characters.

`comma`, `semicolon`, `newline` and `stop` are **marks**.

## Words, keywords and connectors

Keywords and connectors are matched against unquoted `word` tokens,
**case-insensitively for ASCII letters only** (A–Z and a–z). No other case
folding is applied, so `İNGEST` and `сlone` (with a Cyrillic `с`) are ordinary
words.

A **connector** is a mark, or one of the words `and`, `but`, `then`.

A **keyword** is one of these word sequences, tried longest first:

| Keyword | Clause type |
|---|---|
| `make it look like` | `look_like` |
| `tailor it to`, `tailor it for` | `tailor` |
| `make it` | `make` |
| `call it`, `name it` | `name` |
| `clone` | `clone` |
| `with` | `with` |
| `use` | `use` |
| `only` | `only` |
| `ingest` | `ingest` |
| `infer` | `infer` |
| `generate` | `generate` |
| `invent` | `invent` |

The words of a keyword must be consecutive tokens. A line break, comment or
punctuation between them means they are not a keyword. Quoting any part of a
keyword makes it ordinary text.

## Clauses

Parsing walks the tokens from the start:

1. Skip any connectors.
2. If a keyword starts here, read that keyword's clause. Otherwise read a `text`
   clause.
3. Repeat until the tokens run out.

A clause's value runs to its **boundary**: the first run of one or more
connectors that is followed by a keyword or by the end of the tokens. The
connectors themselves belong to neither clause. Connectors that are not followed
by a keyword stay inside the value (`make it fast and friendly`).

Two exceptions:

- In a `clone` clause, an unquoted `with` also ends the value, even without a
  connector (`clone reddit.com with laravel`).
- An `ingest` clause takes exactly one token, so whatever follows it starts the
  next clause, with or without a connector.

A keyword that is not preceded by a connector (or by the start of the request,
or by an `ingest` path) is ordinary text: `call it Savor tailor it to chefs`
names the app "Savor tailor it to chefs".

### Values

A **scalar value** is made from a run of tokens:

1. Drop `newline` tokens, then drop any marks at the front.
2. If one `quoted` token remains, the value is its unquoted text.
3. Otherwise the value is each token's source text, in order, with one U+0020
   space wherever the source had a gap between two tokens (whitespace, line
   breaks or comments). Quotes are kept as written.

A **list value** (`with`, `only`) is split at every connector into groups.
If the list contains a mark anywhere, each group is one item, built as a scalar
value (`posts, direct messages, voting`). If it contains no marks, every token of
every group is its own item, using its value (`posts comments voting`), so a
multi-word item without commas needs quotes. Empty items are dropped.

### Clause types

Every clause is an object with `type`, the fields below, and `span`.

| `type` | Fields | Value |
|---|---|---|
| `clone` | `ref` | scalar |
| `with` | `items` | list |
| `use` | `value`, `instead_of` | Split at the first unquoted `instead of`: `value` is the scalar before it, `instead_of` the scalar after it. Without `instead of`, `instead_of` is `null`. |
| `only` | `items` | list |
| `look_like` | `ref` | scalar |
| `make` | `trait` | scalar |
| `name` | `name` | scalar |
| `tailor` | `audience` | scalar |
| `ingest` | `path` | The value of the next token, which must be a `word` or a non-empty `quoted` token. Otherwise the clause is missing its value, and parsing continues from that token. A path made only of `.`, `!` or `?` must be quoted (`ingest ".."`). |
| `infer` | `subject` | scalar |
| `generate` | `subject` | scalar |
| `invent` | `subject` | scalar |
| `text` | `text` | scalar of everything up to the boundary |

`span` is `[start, end]`: the offset of the clause's first token (its keyword,
or the first word of a text clause) and the offset just past its last token.

A clause whose value is empty is not emitted. For a keyword clause, a
`missing_value` error is reported over the keyword instead; for `use … instead
of` with nothing after it, over `instead of`. An empty `text` clause (`""`) is
dropped without an error.

## Errors

Each error is `{ "code", "message", "span" }`.

| `code` | `message` | `span` |
|---|---|---|
| `empty_request` | `The request is empty.` | `[0, length]`. Only when there are no clauses and no other errors. |
| `invalid_encoding` | `The request is not valid Unicode text.` | `[0, 0]` |
| `unterminated_quote` | `A quoted value is missing its closing quote.` | The opening quote to the end of input |
| `missing_value` | `"<keyword>" needs a value.`, where `<keyword>` is the matched keyword in lower case, or `instead of` | The keyword |

Errors are sorted by span start, then span end, then code. Errors never stop
parsing; the result includes every clause that could be read.

## Result

```json
{
  "zhql": "0.2",
  "clauses": [
    { "type": "clone", "ref": "reddit.com", "span": [0, 16] },
    { "type": "name", "name": "Savor", "span": [18, 31] },
    { "type": "tailor", "audience": "chefs", "span": [37, 55] }
  ],
  "errors": []
}
```

Clauses appear in request order. Repeated clause types are all kept.
