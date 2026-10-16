# ЖQL 0.3

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

A request is a string of Unicode text, or UTF-8 bytes. Implementations read it as
a sequence of **code points**; every offset in the output counts code points from 0.

If the input is not valid Unicode (bytes that are not UTF-8, or a lone surrogate),
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

With a phrasebook, phrases can also start clauses; see [Phrasebooks](#phrasebooks).
Without one, this section is the whole story.

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
| `use` | `value`, `instead_of` | Split at the first `instead of`, matched like a keyword: the words `instead` and `of` as two consecutive unquoted `word` tokens, ignoring ASCII case (`INSTEAD OF` counts; `instead` and `of` on different lines don't). `value` is the scalar before it, leaving out any connectors just before `instead of` (`use postgres, instead of mysql`), and `instead_of` is the scalar after it. Without `instead of`, `instead_of` is `null`. |
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

Each error is `{ "code", "message", "span" }`, plus `via` when it comes from a
phrasebook alias (see [Output](#output)).

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
  "zhql": "0.3",
  "clauses": [
    { "type": "clone", "ref": "reddit.com", "span": [0, 16] },
    { "type": "name", "name": "Savor", "span": [18, 31] },
    { "type": "tailor", "audience": "chefs", "span": [37, 55] }
  ],
  "errors": []
}
```

Clauses appear in request order. Repeated clause types are all kept. With a
phrasebook, the result may also have `phrasebook_problems` after `errors`.

A result is **complete** when it has no errors and no `text` clauses: every word
of the request was understood. An incomplete result is the cue for a tool to ask
for help, for example from an AI (see [Learning](#learning)).

## Phrasebooks

A phrasebook teaches the parser one user's or one team's words. It is optional;
without one, nothing above changes.

```json
{
  "spin up": "clone",
  "brand it": "call it",
  "including": "with",
  "my usual stack": "with laravel, postgres and relay",
  "the savor treatment": "call it Savor and tailor it to chefs"
}
```

A phrasebook is one JSON object. Each key is a **phrase**, something the user
says; each value is its **meaning**, a string. With that phrasebook,
`spin up reddit.com, my usual stack` reads as
`clone reddit.com, with laravel, postgres and relay`.

### Entries

Every entry is checked on its own, so whether it works never depends on other
entries or on their order. An entry that fails a check is skipped and reported as
a problem (see [Output](#output)); the rest of the phrasebook still works. The
checks, in this order, with the first failure's message:

| Check | Message, after the quoted phrase |
|---|---|
| The phrase is one or more words separated by single U+0020 spaces: tokenizing it as a request gives only `word` tokens, and joining their values with single spaces gives the phrase back. This rules out empty phrases, extra spaces, other whitespace, line breaks, `,` `;`, quotes at the start of a word, `.` `!` `?` at the end of one, a `# ` comment and a byte order mark. | `must be plain words separated by single spaces.` |
| The phrase has at most 16 words. | `must be at most 16 words.` |
| The phrase has no ASCII capitals (A–Z). Phrases are written in lower case and matched ignoring ASCII case, like keywords. | `must be written in lower case.` |
| No word of the phrase is `and`, `but`, `then` or `instead`. | `contains the reserved word "<word>".` |
| No run of the phrase's words is a keyword, anywhere in the phrase (checked from the first word on, trying keywords longest first at each word). A phrase may be the start of a keyword, such as `make` or `tailor it`; the keyword still wins wherever it fully matches. | `contains the keyword "<keyword>".` |
| The meaning is an alias target or a usable shorthand (below). | `must mean a keyword or whole clauses, not "<meaning>".` |

These rules mean a phrase can never take over a built-in keyword, swallow one, or
take the `instead of` out of a `use` clause.

**Alias.** If the meaning, with ASCII letters lowercased, is exactly one keyword
from the table, written with single spaces (`clone`, `CALL IT`, `tailor it to`),
the phrase is an alias of that keyword.

**Shorthand.** Any other meaning must be whole clauses. It is parsed once, as a
request on its own with no phrasebook, and is usable only if that gives at least
one clause, no errors, no `text` clause and no `ingest` clause. (So only paths
the user typed are ever read as files.) Those clauses, without their spans, are
what the phrase stands for. A shorthand can't refer to other phrases.

Phrases are processed in code point order. Two different valid phrases can never
match the same words, so the order never changes a result.

### Matching

A phrase matches where its words are consecutive unquoted `word` tokens, compared
ignoring ASCII case, exactly as keywords are. A line break, comment or punctuation
between the words, or quotes around any of them, means no match.

Phrases are tried only where a keyword could start a clause: the start of the
request, after a run of connectors, after an `ingest` path, and after a
shorthand. At each such place the built-in keywords are tried first, unchanged.
Only if none matches, the longest phrase that matches is used. In the boundary
rule, "followed by a keyword" becomes "followed by a keyword or a phrase", so a
phrase after a connector ends the previous value as a keyword would
(`make it fast and spin up reddit.com`).

**An alias** starts exactly the clause its keyword would, with the same value
rules: an alias of `ingest` takes one token, an alias of `use` splits at
`instead of`, and an alias of `with` also ends a `clone` value without a
connector (`clone reddit.com including laravel`). The span starts at the alias's
first word, and a `missing_value` error names the phrase: `"spin up" needs a value.`

**A shorthand** at the start of a clause produces its clauses, in order. Each
has the fields the meaning gave it, and the span of the phrase. Parsing then
continues right after the phrase, and what follows needs no connector:
`my usual stack plus redis` gives a `with` clause, then the text `plus redis`.

**Inside a list.** In a `with` or `only` value, a shorthand whose meaning is
exactly one clause of that same type joins the list instead of ending it. When the
list is split into groups, a group that is exactly that shorthand's words is
replaced by the shorthand's items, in place; every other group follows the usual
list rules. So `with redis, my usual stack and horizon` is one `with` clause with
the items `redis`, `laravel`, `postgres`, `relay` and `horizon`. A shorthand of
another type still ends the list, and a group with more words than the shorthand
(`with my usual stack plus redis`) is read literally.

An alias of `with` (such as `"for": "with"`) ends any `clone` value it appears in
(`clone the app for chefs`), and a phrase after a connector ends a value just as
a keyword does. Both behave like the keywords themselves, and `via` shows when it
happened.

### Output

A clause or error that came from the phrasebook has a `via` key, after `span`,
listing the entries used, in request order:

```json
{
  "type": "with",
  "items": ["laravel", "postgres", "relay"],
  "span": [17, 41],
  "via": [
    { "phrase": "including", "means": "with", "span": [17, 26] },
    { "phrase": "my usual stack", "means": "with laravel, postgres and relay", "span": [27, 41] }
  ]
}
```

Each match's `span` covers the phrase's words in the request. A clause started by
an alias lists the alias first, then any shorthands used in its list. Every
clause produced by a shorthand at a clause start lists that shorthand. Clauses and
errors that used no phrase have no `via`.

Skipped entries are listed in `phrasebook_problems`, after `errors`, sorted by
phrase in code point order. The key is present only when there is at least one:

```json
"phrasebook_problems": [
  { "code": "invalid_phrase", "message": "\"go with\" contains the keyword \"with\".", "phrase": "go with" }
]
```

Problems are not errors. They never make a request fail, and they never affect
`empty_request`. A phrasebook that matched nothing and has no problems gives
exactly the output without one.

A phrasebook that is not an object of strings, or whose text is not valid
Unicode, is not a phrasebook. The library refuses it (an exception or an error
value, as the language does), and the command-line tools exit with status 2:
`zhql: <file> is not a phrasebook (a JSON object of phrases and meanings).`

## Learning

How a phrasebook grows is up to the tool around the parser, such as Slipstream
or the CLI. The parser needs no model and stays deterministic. This is the loop
the language is designed for:

1. Parse the request with the user's phrasebook. If the result is complete, use
   it; no AI is involved.
2. Otherwise, ask the AI to rewrite the request in plain ЖQL, using the exported
   keyword table, and to propose phrasebook entries for the user's own words.
3. Accept the proposal only if the rewrite parses complete, the request parses
   complete with the proposed entries added and gives the same clauses as the
   rewrite (ignoring `span` and `via`), no new problems appear, and every
   proposed entry shows up in some `via`.
4. Show the user each entry and save it only when they confirm. Never overwrite
   an existing phrase.

Next time the same words parse straight from the phrasebook, with no AI call. A
miss becomes a hit.
