# ЖQL

ЖQL ("Zh", typed `zhql`) is the Zero-Hassle Query Language: a request for an
application in one line of plain words.

```
clone reddit.com, call it Savor, and tailor it to chefs
```

This repository holds the parser in three languages. All three follow
[SPEC.md](SPEC.md), pass the same cases in [conformance/](conformance/), and
print byte-for-byte identical JSON.

| Language | Folder | Package |
|---|---|---|
| PHP 8.2+ | [php/](php/) | `parallax/zhql` (Composer, from the repository root) |
| JavaScript (Node 18+, browsers) | [js/](js/) | `zhql` (npm, from `js/`) |
| Go 1.24+ | [go/](go/) | `github.com/Parallax-Web-Services/zhql/go` |

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

## Use

```php
$result = Zhql\Parser::parse('clone reddit.com, call it Savor');
```

```js
import { parse } from 'zhql'
const result = parse('clone reddit.com, call it Savor')
```

```go
import zhql "github.com/Parallax-Web-Services/zhql/go"

result := zhql.Parse("clone reddit.com, call it Savor")
```

Each language also has a `zhql` command that prints the result as JSON:

```bash
php php/bin/zhql "clone reddit.com, call it Savor"
```

```bash
node js/bin/zhql.js examples/savor.zhql --pretty
```

```bash
go run ./go/cmd/zhql examples/savor.zhql
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
node scripts/crosscheck.mjs
```

The last one runs all three command-line tools on every case and checks that
their output is identical. It needs PHP, Node and Go installed.

A change to the language means changing SPEC.md, the conformance cases and all
three parsers together.

## License

MIT. See [LICENSE](LICENSE).
