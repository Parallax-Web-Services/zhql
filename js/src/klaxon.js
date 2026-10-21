// Klaxon's agent dialect. It shares ZhQL tokens and code point spans, while
// keeping security workflow vocabulary outside the application-building dialect.
const MARKS = new Set(['comma', 'semicolon', 'newline', 'stop'])
const CONNECTORS = new Set(['and', 'but', 'then'])
const EXPECTED = 'Expected: klaxon probe <host> for <CVE> [and <CVE>], fix, report back, store as kit.'
const lower = (value) => value.replace(/[A-Z]/g, (letter) => letter.toLowerCase())
const cvePattern = /^[Cc][Vv][Ee]-[0-9]{4}-[0-9]{4,}$/

const hostname = (value) => value.length <= 253 && value.split('.').every((label) =>
    label.length <= 63 && /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(label))

export function klaxonStart(tokens) {
    let index = 0
    while (index < tokens.length && MARKS.has(tokens[index].kind)) index++
    return tokens[index]?.kind === 'word' && lower(tokens[index].value) === 'klaxon' ? index : -1
}

export function parseKlaxon(cps, tokens, errors, version, start) {
    const clauses = []
    const seen = new Set()
    let pos = start + 1
    const word = (index, value) => tokens[index]?.kind === 'word' && lower(tokens[index].value) === value
    const isCve = (index) => tokens[index]?.kind === 'word' && cvePattern.test(tokens[index].value)
    const connector = (index) => MARKS.has(tokens[index]?.kind)
        || (tokens[index]?.kind === 'word' && CONNECTORS.has(lower(tokens[index].value)))
    const result = () => {
        errors.sort((a, b) => a.span[0] - b.span[0] || a.span[1] - b.span[1]
            || (a.code < b.code ? -1 : a.code > b.code ? 1 : 0))
        return { zhql: version, dialect: 'klaxon', clauses: errors.length ? [] : clauses, errors }
    }
    const invalid = (index) => {
        const token = tokens[index]
        errors.push({ code: 'invalid_agent_request', message: EXPECTED,
            span: token ? [token.start, token.end] : [cps.length, cps.length] })
        return result()
    }

    if (errors.length) return result()
    if (!word(pos, 'probe')) return invalid(pos)
    const probeStart = tokens[pos++].start
    if (tokens[pos]?.kind !== 'word' || !hostname(tokens[pos].value)) return invalid(pos)
    const target = lower(tokens[pos++].value)
    // Tokenization separates a fully qualified host's final dot as sentence
    // punctuation. Only one adjacent dot belongs to the host.
    if (tokens[pos]?.kind === 'stop' && tokens[pos].value === '.' && tokens[pos - 1].end === tokens[pos].start) pos++
    if (!word(pos, 'for')) return invalid(pos)
    pos++
    if (!isCve(pos)) return invalid(pos)
    const probe = { type: 'probe', target, cves: [tokens[pos].value.toUpperCase()],
        span: [probeStart, tokens[pos++].end] }
    clauses.push(probe)
    let readingCves = true

    while (pos < tokens.length) {
        if (!connector(pos)) return invalid(pos)
        while (pos < tokens.length && connector(pos)) pos++
        if (pos === tokens.length) break
        if (readingCves && isCve(pos)) {
            const cve = tokens[pos].value.toUpperCase()
            if (!probe.cves.includes(cve)) probe.cves.push(cve)
            probe.span[1] = tokens[pos++].end
            continue
        }
        readingCves = false
        const first = pos
        let type
        let words
        if (word(pos, 'fix')) { type = 'fix'; words = ['fix'] }
        else if (word(pos, 'report')) { type = 'report_back'; words = ['report', 'back'] }
        else if (word(pos, 'store')) { type = 'store_kit'; words = ['store', 'as', 'kit'] }
        else return invalid(pos)
        if (seen.has(type)) return invalid(pos)
        for (const part of words) {
            if (!word(pos, part)) return invalid(pos)
            pos++
        }
        seen.add(type)
        clauses.push({ type, span: [tokens[first].start, tokens[pos - 1].end] })
    }

    return result()
}
