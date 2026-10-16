package zhql

import (
	"fmt"
	"strconv"
	"strings"
)

// JSON returns the result exactly as the JavaScript and PHP command-line tools
// print it: compact, or indented by four spaces, with keys in the order SPEC.md
// shows. encoding/json is not used because it escapes <, >, &, U+2028 and U+2029
// differently from JSON.stringify, which would break byte-for-byte comparison.
func (r Result) JSON(indent bool) []byte {
	w := jsonWriter{indent: indent}
	w.value(r.object(), 0)
	return []byte(w.b.String())
}

// MarshalJSON gives encoding/json users the same structure.
func (r Result) MarshalJSON() ([]byte, error) { return r.JSON(false), nil }

type member struct {
	key   string
	value any
}

type object []member

func (r Result) object() object {
	clauses := make([]any, len(r.Clauses))
	for i, c := range r.Clauses {
		clauses[i] = c.object()
	}
	errors := make([]any, len(r.Errors))
	for i, e := range r.Errors {
		errors[i] = withVia(object{{"code", e.Code}, {"message", e.Message}, {"span", span(e.Span)}}, e.Via)
	}
	o := object{{"zhql", r.Zhql}, {"clauses", clauses}, {"errors", errors}}
	if len(r.PhrasebookProblems) > 0 {
		problems := make([]any, len(r.PhrasebookProblems))
		for i, p := range r.PhrasebookProblems {
			problems[i] = object{{"code", p.Code}, {"message", p.Message}, {"phrase", p.Phrase}}
		}
		o = append(o, member{"phrasebook_problems", problems})
	}
	return o
}

// withVia adds "via" as the last key, only when there is something to show.
func withVia(o object, via []Match) object {
	if len(via) == 0 {
		return o
	}
	matches := make([]any, len(via))
	for i, m := range via {
		matches[i] = object{{"phrase", m.Phrase}, {"means", m.Means}, {"span", span(m.Span)}}
	}
	return append(o, member{"via", matches})
}

func (c Clause) object() object {
	o := object{{"type", c.Type}}
	switch c.Type {
	case "clone", "look_like":
		o = append(o, member{"ref", c.Ref})
	case "with", "only":
		items := make([]any, len(c.Items))
		for i, item := range c.Items {
			items[i] = item
		}
		o = append(o, member{"items", items})
	case "use":
		var insteadOf any // stays nil, written as null, without "instead of"
		if c.InsteadOf != nil {
			insteadOf = *c.InsteadOf
		}
		o = append(o, member{"value", c.Value}, member{"instead_of", insteadOf})
	case "make":
		o = append(o, member{"trait", c.Trait})
	case "name":
		o = append(o, member{"name", c.Name})
	case "tailor":
		o = append(o, member{"audience", c.Audience})
	case "ingest":
		o = append(o, member{"path", c.Path})
	case "infer", "generate", "invent":
		o = append(o, member{"subject", c.Subject})
	case "text":
		o = append(o, member{"text", c.Text})
	}
	return withVia(append(o, member{"span", span(c.Span)}), c.Via)
}

func span(s [2]int) []any { return []any{s[0], s[1]} }

type jsonWriter struct {
	b      strings.Builder
	indent bool
}

func (w *jsonWriter) newline(depth int) {
	if w.indent {
		w.b.WriteString("\n" + strings.Repeat("    ", depth))
	}
}

func (w *jsonWriter) value(v any, depth int) {
	switch v := v.(type) {
	case nil:
		w.b.WriteString("null")
	case string:
		w.str(v)
	case int:
		w.b.WriteString(strconv.Itoa(v))
	case []any:
		if len(v) == 0 {
			w.b.WriteString("[]")
			return
		}
		w.b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				w.b.WriteByte(',')
			}
			w.newline(depth + 1)
			w.value(item, depth+1)
		}
		w.newline(depth)
		w.b.WriteByte(']')
	case object:
		w.b.WriteByte('{')
		for i, m := range v {
			if i > 0 {
				w.b.WriteByte(',')
			}
			w.newline(depth + 1)
			w.str(m.key)
			w.b.WriteByte(':')
			if w.indent {
				w.b.WriteByte(' ')
			}
			w.value(m.value, depth+1)
		}
		w.newline(depth)
		w.b.WriteByte('}')
	default:
		panic(fmt.Sprintf("zhql: cannot write %T as JSON", v))
	}
}

// str writes a JSON string the way JSON.stringify does.
func (w *jsonWriter) str(s string) {
	w.b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			w.b.WriteString(`\"`)
		case '\\':
			w.b.WriteString(`\\`)
		case '\b':
			w.b.WriteString(`\b`)
		case '\f':
			w.b.WriteString(`\f`)
		case '\n':
			w.b.WriteString(`\n`)
		case '\r':
			w.b.WriteString(`\r`)
		case '\t':
			w.b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&w.b, `\u%04x`, r)
			} else {
				w.b.WriteRune(r)
			}
		}
	}
	w.b.WriteByte('"')
}
