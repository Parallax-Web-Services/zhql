// ЖQL (zhql) reference parser for JavaScript. The behaviour is defined by
// ../../SPEC.md and checked against ../../conformance; keep the two in step.

export const VERSION = '0.1'

const WHITESPACE = new Set([0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000])
for (let c = 0x2000; c <= 0x200a; c++) WHITESPACE.add(c)

const LINE_BREAKS = new Set([0x0a, 0x0b, 0x0c, 0x0d, 0x85, 0x2028, 0x2029])
const QUOTES = { '"': '"', '\u201c': '\u201d' }
const STOPS = '.!?'
const BOM = '\ufeff'

// Punctuation tokens. All of them join clauses; inside a list they also separate items.
const MARKS = new Set(['comma', 'semicolon', 'newline', 'stop'])

// Longest phrases first, so "make it look like" wins over "make it".
const KEYWORDS = [
    { words: ['make', 'it', 'look', 'like'], type: 'look_like' },
    { words: ['tailor', 'it', 'to'], type: 'tailor' },
    { words: ['tailor', 'it', 'for'], type: 'tailor' },
    { words: ['make', 'it'], type: 'make' },
    { words: ['call', 'it'], type: 'name' },
    { words: ['name', 'it'], type: 'name' },
    { words: ['clone'], type: 'clone' },
    { words: ['with'], type: 'with' },
    { words: ['use'], type: 'use' },
    { words: ['only'], type: 'only' },
    { words: ['ingest'], type: 'ingest' },
]

const CONNECTOR_WORDS = new Set(['and', 'but', 'then'])
const SCALAR_FIELD = { clone: 'ref', look_like: 'ref', make: 'trait', name: 'name', tailor: 'audience', text: 'text' }

const MESSAGES = {
    empty_request: () => 'The request is empty.',
    invalid_encoding: () => 'The request is not valid Unicode text.',
    unterminated_quote: () => 'A quoted value is missing its closing quote.',
    missing_value: (phrase) => `"${phrase}" needs a value.`,
}

const isSpace = (ch) => WHITESPACE.has(ch.codePointAt(0))
const asciiLower = (s) => s.replace(/[A-Z]/g, (c) => c.toLowerCase())
const error = (code, start, end, phrase) => ({ code, message: MESSAGES[code](phrase), span: [start, end] })

function wellFormed(input) {
    if (typeof input.isWellFormed === 'function') return input.isWellFormed()
    return !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(input)
}

function tokenize(cps, errors) {
    const tokens = []
    const n = cps.length
    let i = cps[0] === BOM ? 1 : 0
    let lineStart = true

    while (i < n) {
        const ch = cps[i]
        if (LINE_BREAKS.has(ch.codePointAt(0))) {
            tokens.push({ kind: 'newline', value: ch, start: i, end: i + 1 })
            lineStart = true
            i++
            continue
        }
        if (isSpace(ch)) { i++; continue }
        // "# " as the first thing on a line comments out the rest of that line.
        if (lineStart && ch === '#' && (i + 1 === n || isSpace(cps[i + 1]))) {
            while (i < n && !LINE_BREAKS.has(cps[i].codePointAt(0))) i++
            continue
        }
        lineStart = false
        if (ch === ',' || ch === ';') {
            tokens.push({ kind: ch === ',' ? 'comma' : 'semicolon', value: ch, start: i, end: i + 1 })
            i++
            continue
        }
        if (ch in QUOTES) {
            const close = QUOTES[ch]
            let j = i + 1
            let value = ''
            let closed = false
            while (j < n) {
                if (cps[j] === '\\' && j + 1 < n) { value += cps[j + 1]; j += 2; continue }
                if (cps[j] === close) { closed = true; j++; break }
                value += cps[j++]
            }
            if (!closed) errors.push(error('unterminated_quote', i, n))
            tokens.push({ kind: 'quoted', value, start: i, end: j })
            i = j
            continue
        }
        let j = i
        while (j < n && !isSpace(cps[j]) && cps[j] !== ',' && cps[j] !== ';') j++
        // Sentence punctuation that ends a word is a separate "stop" token.
        let stop = j
        while (stop > i && STOPS.includes(cps[stop - 1])) stop--
        if (stop > i) tokens.push({ kind: 'word', value: cps.slice(i, stop).join(''), start: i, end: stop })
        if (stop < j) tokens.push({ kind: 'stop', value: cps.slice(stop, j).join(''), start: stop, end: j })
        i = j
    }

    return tokens
}

export function parse(input) {
    if (typeof input !== 'string') throw new TypeError('zhql: the request must be a string')
    if (!wellFormed(input)) {
        return { zhql: VERSION, clauses: [], errors: [error('invalid_encoding', 0, 0)] }
    }

    const cps = Array.from(input)
    const errors = []
    const tokens = tokenize(cps, errors)
    const clauses = []
    const n = tokens.length

    const isWord = (k, word) => k < n && tokens[k].kind === 'word' && asciiLower(tokens[k].value) === word
    const isConnector = (k) => k < n && (MARKS.has(tokens[k].kind)
        || (tokens[k].kind === 'word' && CONNECTOR_WORDS.has(asciiLower(tokens[k].value))))
    const keywordAt = (k) => KEYWORDS.find((kw) => kw.words.every((word, offset) => isWord(k + offset, word)))

    // A clause ends at a run of connectors followed by a keyword or by the end of
    // the request. A clone reference also ends at an unquoted "with".
    function boundary(from, type) {
        let j = from
        while (j < n) {
            if (type === 'clone' && isWord(j, 'with')) return j
            if (isConnector(j)) {
                let k = j
                while (k < n && isConnector(k)) k++
                if (k === n || keywordAt(k)) return j
                j = k
                continue
            }
            j++
        }
        return n
    }

    // A value is its tokens' source text, with every gap between them (whitespace,
    // line breaks, comments) written as one space. A lone quoted token is unquoted.
    function scalar(a, b) {
        const parts = tokens.slice(a, b).filter((t) => t.kind !== 'newline')
        while (parts.length && MARKS.has(parts[0].kind)) parts.shift()
        if (parts.length === 1 && parts[0].kind === 'quoted') return parts[0].value
        let out = ''
        let prevEnd = -1
        for (const t of parts) {
            if (prevEnd !== -1 && t.start > prevEnd) out += ' '
            out += cps.slice(t.start, t.end).join('')
            prevEnd = t.end
        }
        return out
    }

    function list(a, b) {
        let marked = false
        for (let k = a; k < b; k++) if (MARKS.has(tokens[k].kind)) marked = true
        const items = []
        let groupStart = a
        for (let k = a; k <= b; k++) {
            if (k < b && !isConnector(k)) continue
            if (k > groupStart) {
                if (marked) items.push(scalar(groupStart, k))
                else for (let t = groupStart; t < k; t++) items.push(tokens[t].value)
            }
            groupStart = k + 1
        }
        return items.filter((item) => item !== '')
    }

    let pos = 0
    while (pos < n) {
        while (pos < n && isConnector(pos)) pos++
        if (pos >= n) break

        const kw = keywordAt(pos)
        if (!kw) {
            const end = boundary(pos, 'text')
            const text = scalar(pos, end)
            if (text !== '') clauses.push({ type: 'text', text, span: [tokens[pos].start, tokens[end - 1].end] })
            pos = end
            continue
        }

        const phrase = kw.words.join(' ')
        const kwEnd = tokens[pos + kw.words.length - 1].end
        const valueStart = pos + kw.words.length
        const missing = () => errors.push(error('missing_value', tokens[pos].start, kwEnd, phrase))

        if (kw.type === 'ingest') {
            const token = tokens[valueStart]
            if (!token || (token.kind !== 'word' && token.kind !== 'quoted') || token.value === '') {
                missing()
                pos = valueStart
            } else {
                clauses.push({ type: 'ingest', path: token.value, span: [tokens[pos].start, token.end] })
                pos = valueStart + 1
            }
            continue
        }

        const end = boundary(valueStart, kw.type)
        const span = [tokens[pos].start, end > valueStart ? tokens[end - 1].end : kwEnd]

        if (kw.type === 'with' || kw.type === 'only') {
            const items = list(valueStart, end)
            if (items.length) clauses.push({ type: kw.type, items, span })
            else missing()
        } else if (kw.type === 'use') {
            let split = -1
            for (let k = valueStart; k + 1 < end; k++) if (isWord(k, 'instead') && isWord(k + 1, 'of')) { split = k; break }
            const value = scalar(valueStart, split === -1 ? end : split)
            const insteadOf = split === -1 ? null : scalar(split + 2, end)
            if (value === '') missing()
            if (insteadOf === '') errors.push(error('missing_value', tokens[split].start, tokens[split + 1].end, 'instead of'))
            if (value !== '' && insteadOf !== '') clauses.push({ type: 'use', value, instead_of: insteadOf, span })
        } else {
            const value = scalar(valueStart, end)
            if (value !== '') clauses.push({ type: kw.type, [SCALAR_FIELD[kw.type]]: value, span })
            else missing()
        }
        pos = end
    }

    if (!clauses.length && !errors.length) errors.push(error('empty_request', 0, cps.length))
    errors.sort((x, y) => x.span[0] - y.span[0] || x.span[1] - y.span[1] || (x.code < y.code ? -1 : x.code > y.code ? 1 : 0))

    return { zhql: VERSION, clauses, errors }
}
