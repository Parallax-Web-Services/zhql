// ЖQL (zhql) reference parser for JavaScript. The behaviour is defined by
// ../../SPEC.md and checked against ../../conformance; keep the two in step.

export const VERSION = '0.3'

const WHITESPACE = new Set([0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000])
for (let c = 0x2000; c <= 0x200a; c++) WHITESPACE.add(c)

const LINE_BREAKS = new Set([0x0a, 0x0b, 0x0c, 0x0d, 0x85, 0x2028, 0x2029])
const QUOTES = { '"': '"', '\u201c': '\u201d' }
const STOPS = '.!?'
const BOM = '\ufeff'

// Punctuation tokens. All of them join clauses; inside a list they also separate items.
const MARKS = new Set(['comma', 'semicolon', 'newline', 'stop'])

// Longest phrases first, so "make it look like" wins over "make it".
export const KEYWORDS = [
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
    { words: ['infer'], type: 'infer' },
    { words: ['generate'], type: 'generate' },
    { words: ['invent'], type: 'invent' },
]

const CONNECTOR_WORDS = new Set(['and', 'but', 'then'])
const SCALAR_FIELD = {
    clone: 'ref', look_like: 'ref', make: 'trait', name: 'name', tailor: 'audience', text: 'text',
    infer: 'subject', generate: 'subject', invent: 'subject',
}

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

// Phrasebooks. A phrase may not contain these words or any keyword, so it can
// never take over or swallow a built-in.
const RESERVED_WORDS = new Set([...CONNECTOR_WORDS, 'instead'])
const MAX_PHRASE_WORDS = 16
const KEYWORD_BY_PHRASE = new Map(KEYWORDS.map((kw) => [kw.words.join(' '), kw]))

const comparePoints = (a, b) => {
    const x = Array.from(a, (c) => c.codePointAt(0))
    const y = Array.from(b, (c) => c.codePointAt(0))
    for (let i = 0; i < Math.min(x.length, y.length); i++) if (x[i] !== y[i]) return x[i] - y[i]
    return x.length - y.length
}

const problem = (phrase, message) => ({ code: 'invalid_phrase', message: `"${phrase}" ${message}`, phrase })

// Checks one entry. Returns { entry } or { problem }. Depends on nothing but the entry.
function readEntry(phrase, means) {
    const tokens = tokenize(Array.from(phrase), [])
    const words = tokens.map((t) => t.value)
    if (!tokens.length || tokens.some((t) => t.kind !== 'word') || words.join(' ') !== phrase) {
        return { problem: problem(phrase, 'must be plain words separated by single spaces.') }
    }
    if (words.length > MAX_PHRASE_WORDS) return { problem: problem(phrase, `must be at most ${MAX_PHRASE_WORDS} words.`) }
    if (/[A-Z]/.test(phrase)) return { problem: problem(phrase, 'must be written in lower case.') }
    const reserved = words.find((w) => RESERVED_WORDS.has(w))
    if (reserved) return { problem: problem(phrase, `contains the reserved word "${reserved}".`) }
    for (let i = 0; i < words.length; i++) {
        const kw = KEYWORDS.find((k) => k.words.every((w, o) => words[i + o] === w))
        if (kw) return { problem: problem(phrase, `contains the keyword "${kw.words.join(' ')}".`) }
    }

    const target = KEYWORD_BY_PHRASE.get(asciiLower(means))
    if (target) return { entry: { phrase, means, words, alias: target } }

    const plain = parse(means)
    const usable = plain.clauses.length && !plain.errors.length && plain.clauses.every((c) => c.type !== 'text' && c.type !== 'ingest')
    if (!usable) return { problem: problem(phrase, `must mean a keyword or whole clauses, not "${means}".`) }
    const clauses = plain.clauses.map(({ span, ...fields }) => fields)
    return { entry: { phrase, means, words, clauses, listType: clauses.length === 1 ? clauses[0].type : null } }
}

// Phrases are kept in a tree of words, so finding the longest phrase at a token
// costs no more than the words it walks.
const newNode = () => ({ next: new Map(), entry: null })

function readPhrasebook(book) {
    const root = newNode()
    const problems = []
    if (book === undefined || book === null) return { root, problems }
    const proto = typeof book === 'object' ? Object.getPrototypeOf(book) : undefined
    if (proto !== Object.prototype && proto !== null) throw new TypeError('zhql: a phrasebook must be an object of phrases and meanings')
    const phrases = Object.keys(book)
    for (const phrase of phrases) {
        if (typeof book[phrase] !== 'string') throw new TypeError('zhql: every meaning in a phrasebook must be a string')
        if (!wellFormed(phrase) || !wellFormed(book[phrase])) throw new TypeError('zhql: a phrasebook must be valid Unicode text')
    }
    for (const phrase of phrases.sort(comparePoints)) {
        const { entry, problem: bad } = readEntry(phrase, book[phrase])
        if (bad) { problems.push(bad); continue }
        let node = root
        for (const word of entry.words) {
            if (!node.next.has(word)) node.next.set(word, newNode())
            node = node.next.get(word)
        }
        node.entry = entry // two valid phrases never have the same words
    }
    return { root, problems }
}

const UTF8 = typeof TextDecoder === 'function' ? new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }) : null

// The request is a string, or UTF-8 bytes (a Uint8Array or Buffer).
export function parse(request, phrasebook) {
    if (typeof request !== 'string' && !(request instanceof Uint8Array)) {
        throw new TypeError('zhql: the request must be a string or UTF-8 bytes')
    }
    const book = readPhrasebook(phrasebook)
    const finish = (result) => (book.problems.length ? { ...result, phrasebook_problems: book.problems } : result)
    let input = request
    if (request instanceof Uint8Array) {
        try {
            input = UTF8.decode(request)
        } catch {
            input = null
        }
    }
    if (input === null || !wellFormed(input)) {
        return finish({ zhql: VERSION, clauses: [], errors: [error('invalid_encoding', 0, 0)] })
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
    const wordAt = (k) => (k < n && tokens[k].kind === 'word' ? asciiLower(tokens[k].value) : null)
    // The longest phrase whose words start at token k.
    function longestPhrase(k) {
        let node = book.root
        let found = null
        for (let i = k; wordAt(i) !== null && node.next.has(wordAt(i)); i++) {
            node = node.next.get(wordAt(i))
            if (node.entry) found = node.entry
        }
        return found
    }
    const match = (entry, k) => ({ phrase: entry.phrase, means: entry.means, span: [tokens[k].start, tokens[k + entry.words.length - 1].end] })

    // What starts a clause at token k: a keyword, else the longest phrase, else nothing.
    function startAt(k) {
        const kw = keywordAt(k)
        if (kw) return { kw, length: kw.words.length }
        const entry = longestPhrase(k)
        if (!entry) return null
        return entry.alias ? { kw: entry.alias, length: entry.words.length, entry } : { shorthand: entry, length: entry.words.length }
    }

    // A one-clause shorthand of the list's own type, standing exactly for tokens a..b-1.
    function listShorthand(a, b, type) {
        let node = book.root
        for (let i = a; i < b; i++) {
            node = wordAt(i) === null ? undefined : node.next.get(wordAt(i))
            if (!node) return null
        }
        return node.entry && node.entry.listType === type ? node.entry : null
    }

    // A clause ends at a run of connectors followed by a keyword, a phrase, or the
    // end of the request. A clone reference also ends at "with" or an alias of it.
    // Inside a with/only list, a one-clause shorthand of the same type joins the list.
    function boundary(from, type) {
        let j = from
        while (j < n) {
            if (type === 'clone') {
                const start = startAt(j)
                if (start && start.kw && start.kw.type === 'with') return j
            }
            if (isConnector(j)) {
                let k = j
                while (k < n && isConnector(k)) k++
                if (k === n) return j
                const start = startAt(k)
                if (start && !(start.shorthand && start.shorthand.listType === type && (type === 'with' || type === 'only'))) return j
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
        const all = tokens.slice(a, b).filter((t) => t.kind !== 'newline')
        let first = 0
        while (first < all.length && MARKS.has(all[first].kind)) first++
        const parts = all.slice(first)
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

    function list(a, b, type, via) {
        let marked = false
        for (let k = a; k < b; k++) if (MARKS.has(tokens[k].kind)) marked = true
        const items = []
        let groupStart = a
        for (let k = a; k <= b; k++) {
            if (k < b && !isConnector(k)) continue
            if (k > groupStart) {
                const shorthand = listShorthand(groupStart, k, type)
                if (shorthand) {
                    for (const item of shorthand.clauses[0].items) items.push(item)
                    via.push(match(shorthand, groupStart))
                } else if (marked) items.push(scalar(groupStart, k))
                else for (let t = groupStart; t < k; t++) items.push(tokens[t].value)
            }
            groupStart = k + 1
        }
        return items.filter((item) => item !== '')
    }

    const withVia = (object, via) => (via.length ? { ...object, via } : object)

    let pos = 0
    while (pos < n) {
        while (pos < n && isConnector(pos)) pos++
        if (pos >= n) break

        const start = startAt(pos)
        if (!start) {
            const end = boundary(pos, 'text')
            const text = scalar(pos, end)
            if (text !== '') clauses.push({ type: 'text', text, span: [tokens[pos].start, tokens[end - 1].end] })
            pos = end
            continue
        }

        if (start.shorthand) {
            const m = match(start.shorthand, pos)
            for (const fields of start.shorthand.clauses) {
                const copy = fields.items ? { ...fields, items: [...fields.items] } : { ...fields }
                clauses.push({ ...copy, span: [...m.span], via: [{ ...m, span: [...m.span] }] })
            }
            pos += start.length
            continue
        }

        const { kw, length } = start
        const phrase = start.entry ? start.entry.phrase : kw.words.join(' ')
        const via = start.entry ? [match(start.entry, pos)] : []
        const kwEnd = tokens[pos + length - 1].end
        const valueStart = pos + length
        const missing = () => errors.push(withVia(error('missing_value', tokens[pos].start, kwEnd, phrase), via))

        if (kw.type === 'ingest') {
            const token = tokens[valueStart]
            if (!token || (token.kind !== 'word' && token.kind !== 'quoted') || token.value === '') {
                missing()
                pos = valueStart
            } else {
                clauses.push(withVia({ type: 'ingest', path: token.value, span: [tokens[pos].start, token.end] }, via))
                pos = valueStart + 1
            }
            continue
        }

        const end = boundary(valueStart, kw.type)
        const span = [tokens[pos].start, end > valueStart ? tokens[end - 1].end : kwEnd]

        if (kw.type === 'with' || kw.type === 'only') {
            const items = list(valueStart, end, kw.type, via)
            if (items.length) clauses.push(withVia({ type: kw.type, items, span }, via))
            else missing()
        } else if (kw.type === 'use') {
            let split = -1
            for (let k = valueStart; k + 1 < end; k++) if (isWord(k, 'instead') && isWord(k + 1, 'of')) { split = k; break }
            // Connectors just before "instead of" belong to neither part.
            let valueEnd = split === -1 ? end : split
            while (valueEnd > valueStart && isConnector(valueEnd - 1)) valueEnd--
            const value = scalar(valueStart, valueEnd)
            const insteadOf = split === -1 ? null : scalar(split + 2, end)
            if (value === '') missing()
            if (insteadOf === '') errors.push(withVia(error('missing_value', tokens[split].start, tokens[split + 1].end, 'instead of'), via))
            if (value !== '' && insteadOf !== '') clauses.push(withVia({ type: 'use', value, instead_of: insteadOf, span }, via))
        } else {
            const value = scalar(valueStart, end)
            if (value !== '') clauses.push(withVia({ type: kw.type, [SCALAR_FIELD[kw.type]]: value, span }, via))
            else missing()
        }
        pos = end
    }

    if (!clauses.length && !errors.length) errors.push(error('empty_request', 0, cps.length))
    errors.sort((x, y) => x.span[0] - y.span[0] || x.span[1] - y.span[1] || (x.code < y.code ? -1 : x.code > y.code ? 1 : 0))

    return finish({ zhql: VERSION, clauses, errors })
}
