# zhql

Python parser for ЖQL ("Zh", typed `zhql`), the Zero-Hassle Query Language: a
request for an application in one line of plain words.

```python
from zhql import parse

result = parse("clone reddit.com, call it Savor, and tailor it to chefs")
```

`python -m zhql` (or the `zhql` command) prints the result as JSON for a request,
a `.zhql` file, or standard input.

No dependencies. The language is defined in
[SPEC.md](https://github.com/Parallax-Web-Services/zhql/blob/master/SPEC.md); this
parser gives byte-for-byte the same results as the PHP, JavaScript and Go parsers
in the [same repository](https://github.com/Parallax-Web-Services/zhql).
