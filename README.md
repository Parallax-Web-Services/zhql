# ЖQL

ЖQL ("Zh", typed `zhql`) is the Zero-Hassle Query Language: a request for an
application in one line of plain words.

```
clone reddit.com, call it Savor, and tailor it to chefs
```

This repository holds the parser in four languages. All four follow
[SPEC.md](SPEC.md), pass the same cases in [conformance/](conformance/), and
print byte-for-byte identical JSON.

| Language | Folder | Package |
|---|---|---|
| PHP 8.2+ | [php/](php/) | `parallax/zhql` (Composer, from the repository root) |
| JavaScript (Node 18+, browsers) | [js/](js/) | `zhql` (npm, from `js/`) |
| Go 1.24+ | [go/](go/) | `github.com/Parallax-Web-Services/zhql/go` |
| Python 3.9+ | [python/](python/) | `zhql` (PyPI, from `python/`) |

None has any dependencies.

## The language

A request is a list of clauses joined by commas, line breaks, sentence
punctuation, or the words `and`, `but` and `then`:

| Clause | Example |
|---|---|
| `clone …` | `clone reddit.com` |
| `with … and …` | `with laravel and relay` |
| `use … instead of …` | `but use postgres instead of mysql` |
| `only …` | `but only posts comments voting and communities` |
| `make it …` | `and make it private for our company` |
| `make it look like …` | `make it look like linear` |
| `call it …` / `name it …` | `call it Savor` |
| `tailor it to …` / `tailor it for …` | `tailor it to chefs` |
| `ingest …` | `ingest ./reference-notes.md` |
| `infer …` | `infer the data model from ./menu.csv` |
| `generate …` | `and generate seed data` |
| `invent …` | `then invent a logo` |

Anything else is kept as free text. Quotes make anything literal:
`call it "Make It So"`. A `.zhql` file holds one request and may use one clause
per line and `# ` comment lines; see [examples/savor.zhql](examples/savor.zhql).

The parser only reads the request. Deciding what `reddit.com` or `relay` means
belongs to whatever consumes the result.

## Phrasebooks

A phrasebook teaches ЖQL someone's own words. It is a JSON object: each key is
something they say, each value is what they mean.

```json
{ "spin up": "clone", "brand it": "call it", "my usual stack": "with laravel, postgres and relay" }
```

- If the meaning is a keyword, the phrase is an **alias**: `spin up reddit.com`
  reads as `clone reddit.com`.
- Otherwise the meaning is a **shorthand** for whole clauses:
  `clone reddit.com with my usual stack` gives `with laravel, postgres and relay`.

A phrase works where a keyword would: at the start, or after a comma, line break,
`and`, `but` or `then`. Built-in keywords always win, and a phrase can't contain
one, or the words `and`, `but`, `then` or `instead`. Write phrases in lower case,
as single-spaced words. Every clause that came from a phrase says so in `via`; an
entry that breaks a rule is skipped and listed in `phrasebook_problems`. See
[examples/phrasebook.json](examples/phrasebook.json) and the
[Phrasebooks section of the spec](SPEC.md#phrasebooks).

Phrasebooks are how ЖQL learns. When a request isn't fully understood (it has
errors or free text), a tool such as Slipstream can ask an AI to rewrite it in
plain ЖQL and propose entries for the user's words; once the user confirms, the
same words parse instantly next time with no AI call. The steps and checks are in
[Learning](SPEC.md#learning). The parser itself never calls a model.

## Use

```php
$result = Zhql\Parser::parse('clone reddit.com, call it Savor');
$result = Zhql\Parser::parse('spin up reddit.com', ['spin up' => 'clone']);
```

```js
import { parse } from 'zhql'
const result = parse('clone reddit.com, call it Savor')
const withPhrases = parse('spin up reddit.com', { 'spin up': 'clone' })
```

```go
import zhql "github.com/Parallax-Web-Services/zhql/go"

result := zhql.Parse("clone reddit.com, call it Savor")
withPhrases, err := zhql.ParseWith("spin up reddit.com", map[string]string{"spin up": "clone"})
```

```python
from zhql import parse
result = parse("clone reddit.com, call it Savor")
with_phrases = parse("spin up reddit.com", {"spin up": "clone"})
```

Each language exports its keyword table (`KEYWORDS`, `Parser::KEYWORDS`,
`zhql.Keywords`), for example to build an AI prompt. JavaScript and Python also
accept the request as UTF-8 bytes (a `Uint8Array`/`Buffer`, or `bytes`), so text
that isn't UTF-8 is reported as `invalid_encoding` rather than silently replaced;
PHP and Go strings are bytes already.

Each language also has a `zhql` command that prints the result as JSON. Add
`--phrasebook FILE` to use a phrasebook. (Node decodes command-line arguments
before any program code runs, replacing bytes that aren't UTF-8, so give such
requests to the JavaScript tool in a file or on standard input.)

```bash
php php/bin/zhql "clone reddit.com, call it Savor"
```

```bash
node js/bin/zhql.js examples/savor.zhql --pretty
```

```bash
node js/bin/zhql.js --phrasebook examples/phrasebook.json "spin up reddit.com with my usual stack"
```

```bash
go run ./go/cmd/zhql examples/savor.zhql
```

```bash
PYTHONPATH=python/src python3 -m zhql examples/savor.zhql
```

## Tests

```bash
php php/tests/conformance.php
```

```bash
cd js && npm test
```

```bash
go test ./...
```

```bash
python3 -m unittest discover -s python/tests
```

```bash
node scripts/crosscheck.mjs
```

The last one runs all four command-line tools on every case and checks that
their output is identical. It needs PHP, Node, Go and Python installed.

A change to the language means changing SPEC.md, the conformance cases and all
four parsers together.

## License

MIT. See [LICENSE](LICENSE).
