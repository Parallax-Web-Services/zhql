package zhql

import (
	"regexp"
	"sort"
	"strings"
)

// Each agent owns its verbs and their grammar. Klaxon's clauses describe a
// requested workflow; parsing them never probes a host or performs a fix.
const klaxonExpected = "Expected: klaxon probe <host> for <CVE> [and <CVE>], fix, report back, store as kit."

var klaxonCVE = regexp.MustCompile(`^[Cc][Vv][Ee]-[0-9]{4}-[0-9]{4,}$`)
var klaxonLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func klaxonHostname(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !klaxonLabel.MatchString(label) {
			return false
		}
	}
	return true
}

func (p *parser) runKlaxon(first int) Result {
	result := Result{Zhql: Version, Dialect: "klaxon", Clauses: []Clause{}, Errors: append([]Error{}, p.errors...)}
	sort.SliceStable(result.Errors, func(i, j int) bool {
		x, y := result.Errors[i], result.Errors[j]
		if x.Span[0] != y.Span[0] {
			return x.Span[0] < y.Span[0]
		}
		if x.Span[1] != y.Span[1] {
			return x.Span[1] < y.Span[1]
		}
		return x.Code < y.Code
	})
	if len(result.Errors) > 0 {
		return result
	}
	bad := func(pos int) Result {
		at := [2]int{len(p.cps), len(p.cps)}
		if pos < p.n {
			at = [2]int{p.tokens[pos].start, p.tokens[pos].end}
		}
		result.Clauses = []Clause{}
		result.Errors = []Error{{Code: "invalid_agent_request", Message: klaxonExpected, Span: at}}
		return result
	}

	pos := first + 1
	if !p.isWord(pos, "probe") {
		return bad(pos)
	}
	probeStart := p.tokens[pos].start
	pos++
	if pos >= p.n || p.tokens[pos].kind != "word" || !klaxonHostname(p.tokens[pos].value) {
		return bad(pos)
	}
	host := asciiLower(p.tokens[pos].value)
	hostEnd := p.tokens[pos].end
	pos++
	// The shared tokenizer splits the final dot from a fully qualified host.
	if pos < p.n && p.tokens[pos].kind == "stop" && p.tokens[pos].value == "." && p.tokens[pos].start == hostEnd {
		pos++
	}
	if !p.isWord(pos, "for") {
		return bad(pos)
	}
	pos++

	cves := []string{}
	seenCVEs := map[string]bool{}
	lastCVE := 0
	for {
		if pos >= p.n || p.tokens[pos].kind != "word" || !klaxonCVE.MatchString(p.tokens[pos].value) {
			return bad(pos)
		}
		cve := strings.ToUpper(p.tokens[pos].value)
		if !seenCVEs[cve] {
			cves = append(cves, cve)
			seenCVEs[cve] = true
		}
		lastCVE = p.tokens[pos].end
		pos++
		if pos == p.n {
			break
		}
		if !p.isConnector(pos) {
			return bad(pos)
		}
		for pos < p.n && p.isConnector(pos) {
			pos++
		}
		if pos >= p.n || p.tokens[pos].kind != "word" || !klaxonCVE.MatchString(p.tokens[pos].value) {
			break
		}
	}
	result.Clauses = append(result.Clauses, Clause{Type: "probe", Target: host, CVEs: cves, Span: [2]int{probeStart, lastCVE}})

	seenSteps := map[string]bool{}
	for pos < p.n {
		start := pos
		kind := ""
		switch {
		case p.isWord(pos, "fix"):
			kind = "fix"
		case p.isWord(pos, "report"):
			kind = "report_back"
		case p.isWord(pos, "store"):
			kind = "store_kit"
		default:
			return bad(pos)
		}
		if seenSteps[kind] {
			return bad(start)
		}
		seenSteps[kind] = true
		switch kind {
		case "fix":
			pos++
		case "report_back":
			if !p.isWord(pos+1, "back") {
				return bad(pos + 1)
			}
			pos += 2
		case "store_kit":
			if !p.isWord(pos+1, "as") {
				return bad(pos + 1)
			}
			if !p.isWord(pos+2, "kit") {
				return bad(pos + 2)
			}
			pos += 3
		}
		result.Clauses = append(result.Clauses, Clause{Type: kind, Span: [2]int{p.tokens[start].start, p.tokens[pos-1].end}})
		if pos < p.n && !p.isConnector(pos) {
			return bad(pos)
		}
		for pos < p.n && p.isConnector(pos) {
			pos++
		}
	}
	return result
}
