// Package zhql parses ЖQL (zhql) requests, the Zero-Hassle Query Language.
//
// The behaviour is defined by SPEC.md and checked against conformance/; keep it
// in step with js/, php/ and python/, which follow the same structure function
// by function.
package zhql

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Version is the ЖQL specification version this parser implements.
const Version = "0.3"

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
	Via       []Match  // phrasebook entries this clause came from, in request order
}

// Error describes a problem in a request. Parsing never stops at an error.
type Error struct {
	Code    string
	Message string
	Span    [2]int
	Via     []Match // set when the error comes from a phrasebook alias
}

// Match is a phrasebook entry found in a request.
type Match struct {
	Phrase string
	Means  string
	Span   [2]int // where the phrase's words are in the request
}

// Problem is a phrasebook entry that was skipped.
type Problem struct {
	Code    string // always invalid_phrase
	Message string
	Phrase  string
}

// Result is a parsed request.
type Result struct {
	Zhql               string
	Clauses            []Clause
	Errors             []Error
	PhrasebookProblems []Problem // only from ParseWith
}

type token struct {
	kind  string // word, quoted, comma, semicolon, newline or stop
	value string
	start int
	end   int
}

// Keyword is a built-in keyword and the clause type it starts.
type Keyword struct {
	Words []string
	Type  string
}

// Keywords are the built-in keywords, longest first, so "make it look like"
// wins over "make it". Useful for writing AI prompts.
var Keywords = []Keyword{
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

// A phrase may not contain these words or any keyword, so it can never take
// over or swallow a built-in.
var reservedWords = map[string]bool{"and": true, "but": true, "then": true, "instead": true}

// maxPhraseWords bounds how far a phrase match can look ahead.
const maxPhraseWords = 16

// Punctuation tokens. All of them join clauses; inside a list they also separate items.
var marks = map[string]bool{"comma": true, "semicolon": true, "newline": true, "stop": true}

// Straight quotes, and curly quotes U+201C to U+201D.
var quotes = map[rune]rune{'"': '"', 0x201c: 0x201d}

const bom = 0xfeff

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

// entry is a usable phrasebook entry: an alias of a keyword, or a shorthand
// standing for whole clauses.
type entry struct {
	phrase   string
	means    string
	words    []string
	alias    *Keyword // set for an alias
	clauses  []Clause // set for a shorthand, without spans
	listType string   // the clause type when a shorthand is exactly one clause
}

// node is one word in the tree of phrases, so finding the longest phrase at a
// token costs no more than the words it walks.
type node struct {
	next  map[string]*node
	entry *entry // the phrase ending at this word, if any
}

type phrasebook struct {
	root     *node
	problems []Problem
}

// ErrInvalidPhrasebook is returned when a phrasebook's text is not valid Unicode.
var ErrInvalidPhrasebook = errors.New("zhql: a phrasebook must be valid Unicode text")

func readPhrasebook(book map[string]string) (*phrasebook, error) {
	pb := &phrasebook{root: &node{next: map[string]*node{}}, problems: []Problem{}}
	phrases := make([]string, 0, len(book))
	for phrase, means := range book {
		if !utf8.ValidString(phrase) || !utf8.ValidString(means) {
			return nil, ErrInvalidPhrasebook
		}
		phrases = append(phrases, phrase)
	}
	sort.Strings(phrases) // byte order of UTF-8 is code point order
	for _, phrase := range phrases {
		e, problem := readEntry(phrase, book[phrase])
		if problem != nil {
			pb.problems = append(pb.problems, *problem)
			continue
		}
		n := pb.root
		for _, word := range e.words {
			if n.next[word] == nil {
				n.next[word] = &node{next: map[string]*node{}}
			}
			n = n.next[word]
		}
		n.entry = e // two valid phrases never have the same words
	}
	return pb, nil
}

// readEntry checks one entry. It depends on nothing but the entry.
func readEntry(phrase, means string) (*entry, *Problem) {
	bad := func(message string) (*entry, *Problem) {
		return nil, &Problem{Code: "invalid_phrase", Message: `"` + phrase + `" ` + message, Phrase: phrase}
	}

	reader := &parser{cps: []rune(phrase)}
	reader.tokenize()
	words := make([]string, len(reader.tokens))
	plain := len(reader.tokens) > 0
	for i, t := range reader.tokens {
		words[i] = t.value
		plain = plain && t.kind == "word"
	}
	if !plain || strings.Join(words, " ") != phrase {
		return bad("must be plain words separated by single spaces.")
	}
	if len(words) > maxPhraseWords {
		return bad("must be at most " + strconv.Itoa(maxPhraseWords) + " words.")
	}
	if strings.ContainsAny(phrase, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return bad("must be written in lower case.")
	}
	for _, w := range words {
		if reservedWords[w] {
			return bad(`contains the reserved word "` + w + `".`)
		}
	}
	for i := range words {
		for _, kw := range Keywords {
			if i+len(kw.Words) <= len(words) && strings.Join(words[i:i+len(kw.Words)], " ") == strings.Join(kw.Words, " ") {
				return bad(`contains the keyword "` + strings.Join(kw.Words, " ") + `".`)
			}
		}
	}

	for i := range Keywords {
		if asciiLower(means) == strings.Join(Keywords[i].Words, " ") {
			return &entry{phrase: phrase, means: means, words: words, alias: &Keywords[i]}, nil
		}
	}

	plainResult := Parse(means)
	usable := len(plainResult.Clauses) > 0 && len(plainResult.Errors) == 0
	for _, c := range plainResult.Clauses {
		usable = usable && c.Type != "text" && c.Type != "ingest"
	}
	if !usable {
		return bad(`must mean a keyword or whole clauses, not "` + means + `".`)
	}
	e := &entry{phrase: phrase, means: means, words: words}
	for _, c := range plainResult.Clauses {
		c.Span = [2]int{}
		e.clauses = append(e.clauses, c)
	}
	if len(e.clauses) == 1 {
		e.listType = e.clauses[0].Type
	}
	return e, nil
}

type parser struct {
	cps    []rune // the request as code points
	tokens []token
	n      int
	errors []Error
	book   *phrasebook
}

// Parse reads one ЖQL request, such as the content of a .zhql file.
func Parse(request string) Result {
	result, _ := ParseWith(request, nil)
	result.PhrasebookProblems = nil
	return result
}

// ParseWith reads one request with a phrasebook: phrases mapped to what they
// mean, either a keyword ("spin up": "clone") or whole clauses ("my usual
// stack": "with laravel, postgres and relay"). Entries that break the rules are
// skipped and reported in Result.PhrasebookProblems. The error is only for a
// phrasebook whose text is not valid Unicode.
func ParseWith(request string, book map[string]string) (Result, error) {
	pb, err := readPhrasebook(book)
	if err != nil {
		return Result{}, err
	}
	if !utf8.ValidString(request) {
		return Result{Zhql: Version, Clauses: []Clause{}, Errors: []Error{newError("invalid_encoding", 0, 0, "")}, PhrasebookProblems: pb.problems}, nil
	}
	p := &parser{cps: []rune(request), book: pb}
	p.tokenize()
	p.n = len(p.tokens)
	result := p.run()
	result.PhrasebookProblems = pb.problems
	return result, nil
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
func (p *parser) keywordAt(k int) *Keyword {
	for i := range Keywords {
		matched := true
		for offset, word := range Keywords[i].Words {
			if !p.isWord(k+offset, word) {
				matched = false
				break
			}
		}
		if matched {
			return &Keywords[i]
		}
	}
	return nil
}

// wordAt returns token k lowercased, if it is a word.
func (p *parser) wordAt(k int) (string, bool) {
	if k >= p.n || p.tokens[k].kind != "word" {
		return "", false
	}
	return asciiLower(p.tokens[k].value), true
}

// longestPhrase returns the longest phrase whose words start at token k, or nil.
func (p *parser) longestPhrase(k int) *entry {
	if p.book == nil {
		return nil
	}
	var found *entry
	n := p.book.root
	for i := k; ; i++ {
		word, ok := p.wordAt(i)
		if !ok || n.next[word] == nil {
			return found
		}
		n = n.next[word]
		if n.entry != nil {
			found = n.entry
		}
	}
}

func (p *parser) match(e *entry, k int) Match {
	return Match{Phrase: e.phrase, Means: e.means, Span: [2]int{p.tokens[k].start, p.tokens[k+len(e.words)-1].end}}
}

// start is what begins a clause: a keyword (possibly through an alias) or a shorthand.
type start struct {
	keyword   *Keyword
	alias     *entry
	shorthand *entry
	length    int
}

// startAt finds what starts a clause at token k: a keyword, else the longest
// phrase, else nothing.
func (p *parser) startAt(k int) *start {
	if kw := p.keywordAt(k); kw != nil {
		return &start{keyword: kw, length: len(kw.Words)}
	}
	e := p.longestPhrase(k)
	if e == nil {
		return nil
	}
	if e.alias != nil {
		return &start{keyword: e.alias, alias: e, length: len(e.words)}
	}
	return &start{shorthand: e, length: len(e.words)}
}

// listShorthand finds a one-clause shorthand of the list's own type that
// stands exactly for tokens a..b-1.
func (p *parser) listShorthand(a, b int, kind string) *entry {
	if p.book == nil {
		return nil
	}
	n := p.book.root
	for i := a; i < b; i++ {
		word, ok := p.wordAt(i)
		if !ok || n.next[word] == nil {
			return nil
		}
		n = n.next[word]
	}
	if n.entry != nil && n.entry.listType == kind {
		return n.entry
	}
	return nil
}

// boundary finds where a clause's value ends: at a run of connectors followed by
// a keyword, a phrase or the end of the request. A clone reference also ends at
// "with" or an alias of it. Inside a with/only list, a one-clause shorthand of
// the same type joins the list.
func (p *parser) boundary(from int, kind string) int {
	j := from
	for j < p.n {
		if kind == "clone" {
			if s := p.startAt(j); s != nil && s.keyword != nil && s.keyword.Type == "with" {
				return j
			}
		}
		if p.isConnector(j) {
			k := j
			for k < p.n && p.isConnector(k) {
				k++
			}
			if k == p.n {
				return j
			}
			s := p.startAt(k)
			joinsList := s != nil && s.shorthand != nil && (kind == "with" || kind == "only") && s.shorthand.listType == kind
			if s != nil && !joinsList {
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

// items builds a list value from tokens a..b-1, adding a Match to via for each
// shorthand used.
func (p *parser) items(a, b int, kind string, via *[]Match) []string {
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
			if e := p.listShorthand(groupStart, k, kind); e != nil {
				for _, item := range e.clauses[0].Items {
					add(item)
				}
				*via = append(*via, p.match(e, groupStart))
			} else if marked {
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

		s := p.startAt(pos)
		if s == nil {
			end := p.boundary(pos, "text")
			if text := p.scalar(pos, end); text != "" {
				clauses = append(clauses, Clause{Type: "text", Text: text, Span: [2]int{p.tokens[pos].start, p.tokens[end-1].end}})
			}
			pos = end
			continue
		}

		if s.shorthand != nil {
			m := p.match(s.shorthand, pos)
			for _, c := range s.shorthand.clauses {
				c.Items = append([]string(nil), c.Items...)
				c.Span = m.Span
				c.Via = []Match{m}
				clauses = append(clauses, c)
			}
			pos += s.length
			continue
		}

		kw := s.keyword
		phrase := strings.Join(kw.Words, " ")
		var via []Match
		if s.alias != nil {
			phrase = s.alias.phrase
			via = []Match{p.match(s.alias, pos)}
		}
		first := p.tokens[pos].start
		kwEnd := p.tokens[pos+s.length-1].end
		valueStart := pos + s.length
		missing := func() {
			e := newError("missing_value", first, kwEnd, phrase)
			e.Via = via
			p.errors = append(p.errors, e)
		}

		if kw.Type == "ingest" {
			if valueStart >= p.n || (p.tokens[valueStart].kind != "word" && p.tokens[valueStart].kind != "quoted") || p.tokens[valueStart].value == "" {
				missing()
				pos = valueStart
			} else {
				t := p.tokens[valueStart]
				clauses = append(clauses, Clause{Type: "ingest", Path: t.value, Span: [2]int{first, t.end}, Via: via})
				pos = valueStart + 1
			}
			continue
		}

		end := p.boundary(valueStart, kw.Type)
		span := [2]int{first, kwEnd}
		if end > valueStart {
			span[1] = p.tokens[end-1].end
		}

		switch kw.Type {
		case "with", "only":
			if items := p.items(valueStart, end, kw.Type, &via); len(items) > 0 {
				clauses = append(clauses, Clause{Type: kw.Type, Items: items, Span: span, Via: via})
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
			// Connectors just before "instead of" belong to neither part.
			valueEnd := end
			if split != -1 {
				valueEnd = split
			}
			for valueEnd > valueStart && p.isConnector(valueEnd-1) {
				valueEnd--
			}
			value := p.scalar(valueStart, valueEnd)
			var insteadOf *string
			if split != -1 {
				after := p.scalar(split+2, end)
				insteadOf = &after
			}
			if value == "" {
				missing()
			}
			if insteadOf != nil && *insteadOf == "" {
				e := newError("missing_value", p.tokens[split].start, p.tokens[split+1].end, "instead of")
				e.Via = via
				p.errors = append(p.errors, e)
			}
			if value != "" && (insteadOf == nil || *insteadOf != "") {
				clauses = append(clauses, Clause{Type: "use", Value: value, InsteadOf: insteadOf, Span: span, Via: via})
			}
		default:
			value := p.scalar(valueStart, end)
			if value == "" {
				missing()
				break
			}
			c := Clause{Type: kw.Type, Span: span, Via: via}
			switch kw.Type {
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
	errs := append([]Error{}, p.errors...)
	sort.SliceStable(errs, func(i, j int) bool {
		x, y := errs[i], errs[j]
		if x.Span[0] != y.Span[0] {
			return x.Span[0] < y.Span[0]
		}
		if x.Span[1] != y.Span[1] {
			return x.Span[1] < y.Span[1]
		}
		return x.Code < y.Code
	})

	return Result{Zhql: Version, Clauses: clauses, Errors: errs}
}
