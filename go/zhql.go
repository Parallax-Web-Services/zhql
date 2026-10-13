// Package zhql parses ЖQL (zhql) requests, the Zero-Hassle Query Language.
//
// The behaviour is defined by SPEC.md and checked against conformance/; keep it
// in step with js/ and php/, which follow the same structure function by function.
package zhql

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Version is the ЖQL specification version this parser implements.
const Version = "0.2"

// Clause is one part of a request. Only the fields for its Type are set.
type Clause struct {
	Type      string   // clone, with, use, only, look_like, make, name, tailor, ingest, infer, generate, invent or text
	Ref       string   // clone, look_like
	Items     []string // with, only
	Value     string   // use
	InsteadOf *string  // use; nil when the request has no "instead of"
	Trait     string   // make
	Name      string   // name
	Audience  string   // tailor
	Path      string   // ingest
	Subject   string   // infer, generate, invent
	Text      string   // text
	Span      [2]int   // code point offsets: first token, and just past the last
}

// Error describes a problem in a request. Parsing never stops at an error.
type Error struct {
	Code    string
	Message string
	Span    [2]int
}

// Result is a parsed request.
type Result struct {
	Zhql    string
	Clauses []Clause
	Errors  []Error
}

type token struct {
	kind  string // word, quoted, comma, semicolon, newline or stop
	value string
	start int
	end   int
}

type keyword struct {
	words []string
	kind  string
}

// Longest phrases first, so "make it look like" wins over "make it".
var keywords = []keyword{
	{[]string{"make", "it", "look", "like"}, "look_like"},
	{[]string{"tailor", "it", "to"}, "tailor"},
	{[]string{"tailor", "it", "for"}, "tailor"},
	{[]string{"make", "it"}, "make"},
	{[]string{"call", "it"}, "name"},
	{[]string{"name", "it"}, "name"},
	{[]string{"clone"}, "clone"},
	{[]string{"with"}, "with"},
	{[]string{"use"}, "use"},
	{[]string{"only"}, "only"},
	{[]string{"ingest"}, "ingest"},
	{[]string{"infer"}, "infer"},
	{[]string{"generate"}, "generate"},
	{[]string{"invent"}, "invent"},
}

var connectorWords = map[string]bool{"and": true, "but": true, "then": true}

// Punctuation tokens. All of them join clauses; inside a list they also separate items.
var marks = map[string]bool{"comma": true, "semicolon": true, "newline": true, "stop": true}

var quotes = map[rune]rune{'"': '"', '\u201c': '\u201d'}

const bom = '\ufeff'

func isLineBreak(r rune) bool {
	switch r {
	case 0x0a, 0x0b, 0x0c, 0x0d, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

func isSpace(r rune) bool {
	switch {
	case r == 0x09, r == 0x20, r == 0xa0, r == 0x1680, r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	case r >= 0x2000 && r <= 0x200a:
		return true
	}
	return isLineBreak(r)
}

func isStop(r rune) bool { return r == '.' || r == '!' || r == '?' }

// asciiLower lowercases A-Z only. strings.ToLower would also fold letters such
// as İ, which the spec does not allow.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func newError(code string, start, end int, phrase string) Error {
	messages := map[string]string{
		"empty_request":      "The request is empty.",
		"invalid_encoding":   "The request is not valid Unicode text.",
		"unterminated_quote": "A quoted value is missing its closing quote.",
		"missing_value":      `"` + phrase + `" needs a value.`,
	}
	return Error{Code: code, Message: messages[code], Span: [2]int{start, end}}
}

type parser struct {
	cps    []rune // the request as code points
	tokens []token
	n      int
	errors []Error
}

// Parse reads one ЖQL request, such as the content of a .zhql file.
func Parse(request string) Result {
	if !utf8.ValidString(request) {
		return Result{Zhql: Version, Clauses: []Clause{}, Errors: []Error{newError("invalid_encoding", 0, 0, "")}}
	}
	p := &parser{cps: []rune(request)}
	p.tokenize()
	p.n = len(p.tokens)
	return p.run()
}

func (p *parser) text(start, end int) string { return string(p.cps[start:end]) }

func (p *parser) tokenize() {
	cps := p.cps
	n := len(cps)
	i := 0
	if n > 0 && cps[0] == bom {
		i = 1
	}
	lineStart := true

	for i < n {
		ch := cps[i]
		if isLineBreak(ch) {
			p.tokens = append(p.tokens, token{"newline", string(ch), i, i + 1})
			lineStart = true
			i++
			continue
		}
		if isSpace(ch) {
			i++
			continue
		}
		// "# " as the first thing on a line comments out the rest of that line.
		if lineStart && ch == '#' && (i+1 == n || isSpace(cps[i+1])) {
			for i < n && !isLineBreak(cps[i]) {
				i++
			}
			continue
		}
		lineStart = false

		if ch == ',' || ch == ';' {
			kind := "comma"
			if ch == ';' {
				kind = "semicolon"
			}
			p.tokens = append(p.tokens, token{kind, string(ch), i, i + 1})
			i++
			continue
		}

		if closing, ok := quotes[ch]; ok {
			j := i + 1
			var value strings.Builder
			closed := false
			for j < n {
				if cps[j] == '\\' && j+1 < n {
					value.WriteRune(cps[j+1])
					j += 2
					continue
				}
				if cps[j] == closing {
					closed = true
					j++
					break
				}
				value.WriteRune(cps[j])
				j++
			}
			if !closed {
				p.errors = append(p.errors, newError("unterminated_quote", i, n, ""))
			}
			p.tokens = append(p.tokens, token{"quoted", value.String(), i, j})
			i = j
			continue
		}

		j := i
		for j < n && !isSpace(cps[j]) && cps[j] != ',' && cps[j] != ';' {
			j++
		}
		// Sentence punctuation that ends a word is a separate "stop" token.
		stop := j
		for stop > i && isStop(cps[stop-1]) {
			stop--
		}
		if stop > i {
			p.tokens = append(p.tokens, token{"word", p.text(i, stop), i, stop})
		}
		if stop < j {
			p.tokens = append(p.tokens, token{"stop", p.text(stop, j), stop, j})
		}
		i = j
	}
}

func (p *parser) isWord(k int, word string) bool {
	return k < p.n && p.tokens[k].kind == "word" && asciiLower(p.tokens[k].value) == word
}

func (p *parser) isConnector(k int) bool {
	if k >= p.n {
		return false
	}
	t := p.tokens[k]
	return marks[t.kind] || (t.kind == "word" && connectorWords[asciiLower(t.value)])
}

// keywordAt returns the keyword starting at token k, or nil.
func (p *parser) keywordAt(k int) *keyword {
	for i := range keywords {
		matched := true
		for offset, word := range keywords[i].words {
			if !p.isWord(k+offset, word) {
				matched = false
				break
			}
		}
		if matched {
			return &keywords[i]
		}
	}
	return nil
}

// boundary finds where a clause's value ends: at a run of connectors followed by
// a keyword or by the end of the request. A clone reference also ends at an
// unquoted "with".
func (p *parser) boundary(from int, kind string) int {
	j := from
	for j < p.n {
		if kind == "clone" && p.isWord(j, "with") {
			return j
		}
		if p.isConnector(j) {
			k := j
			for k < p.n && p.isConnector(k) {
				k++
			}
			if k == p.n || p.keywordAt(k) != nil {
				return j
			}
			j = k
			continue
		}
		j++
	}
	return p.n
}

// scalar builds a value from tokens a..b-1: their source text, with every gap
// between them (whitespace, line breaks, comments) written as one space. A lone
// quoted token is unquoted.
func (p *parser) scalar(a, b int) string {
	var parts []token
	for k := a; k < b; k++ {
		if p.tokens[k].kind != "newline" {
			parts = append(parts, p.tokens[k])
		}
	}
	for len(parts) > 0 && marks[parts[0].kind] {
		parts = parts[1:]
	}
	if len(parts) == 1 && parts[0].kind == "quoted" {
		return parts[0].value
	}
	var out strings.Builder
	prevEnd := -1
	for _, t := range parts {
		if prevEnd != -1 && t.start > prevEnd {
			out.WriteByte(' ')
		}
		out.WriteString(p.text(t.start, t.end))
		prevEnd = t.end
	}
	return out.String()
}

// items builds a list value from tokens a..b-1.
func (p *parser) items(a, b int) []string {
	marked := false
	for k := a; k < b; k++ {
		if marks[p.tokens[k].kind] {
			marked = true
		}
	}
	items := []string{}
	add := func(item string) {
		if item != "" {
			items = append(items, item)
		}
	}
	groupStart := a
	for k := a; k <= b; k++ {
		if k < b && !p.isConnector(k) {
			continue
		}
		if k > groupStart {
			if marked {
				add(p.scalar(groupStart, k))
			} else {
				for t := groupStart; t < k; t++ {
					add(p.tokens[t].value)
				}
			}
		}
		groupStart = k + 1
	}
	return items
}

func (p *parser) run() Result {
	clauses := []Clause{}

	pos := 0
	for pos < p.n {
		for pos < p.n && p.isConnector(pos) {
			pos++
		}
		if pos >= p.n {
			break
		}

		kw := p.keywordAt(pos)
		if kw == nil {
			end := p.boundary(pos, "text")
			if text := p.scalar(pos, end); text != "" {
				clauses = append(clauses, Clause{Type: "text", Text: text, Span: [2]int{p.tokens[pos].start, p.tokens[end-1].end}})
			}
			pos = end
			continue
		}

		phrase := strings.Join(kw.words, " ")
		start := p.tokens[pos].start
		kwEnd := p.tokens[pos+len(kw.words)-1].end
		valueStart := pos + len(kw.words)
		missing := func() { p.errors = append(p.errors, newError("missing_value", start, kwEnd, phrase)) }

		if kw.kind == "ingest" {
			if valueStart >= p.n || (p.tokens[valueStart].kind != "word" && p.tokens[valueStart].kind != "quoted") || p.tokens[valueStart].value == "" {
				missing()
				pos = valueStart
			} else {
				t := p.tokens[valueStart]
				clauses = append(clauses, Clause{Type: "ingest", Path: t.value, Span: [2]int{start, t.end}})
				pos = valueStart + 1
			}
			continue
		}

		end := p.boundary(valueStart, kw.kind)
		span := [2]int{start, kwEnd}
		if end > valueStart {
			span[1] = p.tokens[end-1].end
		}

		switch kw.kind {
		case "with", "only":
			if items := p.items(valueStart, end); len(items) > 0 {
				clauses = append(clauses, Clause{Type: kw.kind, Items: items, Span: span})
			} else {
				missing()
			}
		case "use":
			split := -1
			for k := valueStart; k+1 < end; k++ {
				if p.isWord(k, "instead") && p.isWord(k+1, "of") {
					split = k
					break
				}
			}
			var value string
			var insteadOf *string
			if split == -1 {
				value = p.scalar(valueStart, end)
			} else {
				value = p.scalar(valueStart, split)
				after := p.scalar(split+2, end)
				insteadOf = &after
			}
			if value == "" {
				missing()
			}
			if insteadOf != nil && *insteadOf == "" {
				p.errors = append(p.errors, newError("missing_value", p.tokens[split].start, p.tokens[split+1].end, "instead of"))
			}
			if value != "" && (insteadOf == nil || *insteadOf != "") {
				clauses = append(clauses, Clause{Type: "use", Value: value, InsteadOf: insteadOf, Span: span})
			}
		default:
			value := p.scalar(valueStart, end)
			if value == "" {
				missing()
				break
			}
			c := Clause{Type: kw.kind, Span: span}
			switch kw.kind {
			case "clone", "look_like":
				c.Ref = value
			case "make":
				c.Trait = value
			case "name":
				c.Name = value
			case "tailor":
				c.Audience = value
			case "infer", "generate", "invent":
				c.Subject = value
			}
			clauses = append(clauses, c)
		}
		pos = end
	}

	if len(clauses) == 0 && len(p.errors) == 0 {
		p.errors = append(p.errors, newError("empty_request", 0, len(p.cps), ""))
	}
	errors := append([]Error{}, p.errors...)
	sort.SliceStable(errors, func(i, j int) bool {
		x, y := errors[i], errors[j]
		if x.Span[0] != y.Span[0] {
			return x.Span[0] < y.Span[0]
		}
		if x.Span[1] != y.Span[1] {
			return x.Span[1] < y.Span[1]
		}
		return x.Code < y.Code
	})

	return Result{Zhql: Version, Clauses: clauses, Errors: errors}
}
