// Feeds every conformance input to the PHP, JavaScript, Go and Python
// command-line tools on standard input and checks that all four print exactly the
// same bytes, compact and with --pretty. Cases with a phrasebook pass it with
// --phrasebook. It also feeds all four tools phrasebook files that are unusable or
// awkward, and checks they agree on those too. Needs php, node, go and python3 on
// the PATH.
//
// Usage: node scripts/crosscheck.mjs
import { execFileSync, spawnSync } from 'node:child_process'
import { chmodSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const build = mkdtempSync(join(tmpdir(), 'zhql-'))
const goBinary = join(build, 'zhql')
execFileSync('go', ['build', '-o', goBinary, './go/cmd/zhql'], { cwd: root, stdio: 'inherit' })

const tools = {
    php: ['php', [join(root, 'php/bin/zhql')], {}],
    js: ['node', [join(root, 'js/bin/zhql.js')], {}],
    go: [goBinary, [], {}],
    python: ['python3', ['-m', 'zhql'], { PYTHONPATH: join(root, 'python/src') }],
}

let checked = 0
let failed = 0

function compare(label, flags, input) {
    const outputs = Object.entries(tools).map(([tool, [command, args, env]]) => {
        const run = spawnSync(command, [...args, ...flags], { input, encoding: 'utf8', env: { ...process.env, ...env } })
        return [tool, `${run.status}\n${run.stdout}${run.stderr}`]
    })
    checked++
    if (outputs.some(([, out]) => out !== outputs[0][1])) {
        failed++
        console.error(`DIFFERENT ${label} ${flags.join(' ')}`)
        for (const [tool, out] of outputs) console.error(`  ${tool}: ${JSON.stringify(out)}`)
    }
}

// Phrasebook files the tools must reject (exit 2), or read alike although awkward.
const phrasebookFiles = [
    ['not JSON', 'spin up: clone', 'spin up reddit.com'],
    ['an array', '["clone"]', 'spin up reddit.com'],
    ['null', 'null', 'spin up reddit.com'],
    ['a string', '"clone"', 'spin up reddit.com'],
    ['a number meaning', '{"spin": 1}', 'spin up reddit.com'],
    ['a null meaning', '{"spin": null}', 'spin up reddit.com'],
    ['an object meaning', '{"spin": {"up": "clone"}}', 'spin up reddit.com'],
    ['a byte order mark', Buffer.from([0xef, 0xbb, 0xbf, ...Buffer.from('{"spin":"clone"}')]), 'spin reddit.com'],
    ['bytes that are not UTF-8', Buffer.from([...Buffer.from('{"sp'), 0xc3, 0x28, ...Buffer.from('":"clone"}')]), 'spin reddit.com'],
    ['a lone high surrogate escape', '{"sp\\ud800":"clone"}', 'spin reddit.com'],
    ['a lone low surrogate escape', '{"spin":"\\udc00"}', 'spin reddit.com'],
    ['a high surrogate escape before the quote', '{"spin":"cl\\ud83c"}', 'spin reddit.com'],
    ['text after the object', '{"spin":"clone"} x', 'spin reddit.com'],
    ['a paired surrogate escape', '{"\\ud83c\\udf27":"clone"}', '\u{1f327} reddit.com'],
    ['a __proto__ phrase', '{"__proto__":"clone"}', '__proto__ reddit.com'],
    ['duplicate phrases', '{"spin":"clone","spin":"invent"}', 'spin a logo'],
    ['escaped letters', '{"sp\\u0069n":"clone"}', 'spin reddit.com'],
    ['an escaped quote', '{"say \\"hi\\"":"clone"}', 'say reddit.com'],
    ['an empty object', '{}', 'clone reddit.com'],
    ['a phrase that starts with NUL', '{"\\u0000x":"clone","spin":"clone"}', 'spin reddit.com'],
    ['nesting 10000 deep', '{"a": ' + '['.repeat(10000) + ']'.repeat(10000) + '}', 'clone reddit.com'],
    ['spread over lines', '{\n  "spin up": "clone",\n  "my usual stack": "with laravel, postgres and relay"\n}\n', 'spin up reddit.com with my usual stack'],
]

try {
    for (const file of readdirSync(join(root, 'conformance')).filter((name) => name.endsWith('.json')).sort()) {
        const { cases } = JSON.parse(readFileSync(join(root, 'conformance', file), 'utf8'))
        for (const [index, { name, input, phrasebook }] of cases.entries()) {
            let bookFlags = []
            if (phrasebook !== undefined) {
                const bookFile = join(build, `${file}-${index}.json`)
                writeFileSync(bookFile, JSON.stringify(phrasebook))
                bookFlags = ['--phrasebook', bookFile]
            }
            for (const flags of [[], ['--pretty']]) compare(`${file}: ${name}`, [...bookFlags, ...flags], input)
        }
    }
    for (const [index, [name, content, input]] of phrasebookFiles.entries()) {
        const bookFile = join(build, `phrasebook-file-${index}.json`)
        writeFileSync(bookFile, content)
        compare(`phrasebook file: ${name}`, ['--phrasebook', bookFile], input)
    }
    compare('phrasebook file: missing', ['--phrasebook', join(build, 'missing.json')], 'clone reddit.com')
    compare('phrasebook file: a directory', ['--phrasebook', build], 'clone reddit.com')
    compare('phrasebook flag without a file', ['--phrasebook'], 'clone reddit.com')
    compare('phrasebook flag with an empty path', ['--phrasebook', ''], 'clone reddit.com')

    // Request bytes that are not UTF-8, on standard input and in a .zhql file,
    // with and without a phrasebook that has problems.
    const problemBook = join(build, 'problem-book.json')
    writeFileSync(problemBook, '{"Ab":"clone","spin":"clone"}')
    for (const bad of [[0xff], [0xc0, 0x80], [0xed, 0xa0, 0x80], [0xf4, 0x90, 0x80, 0x80], [0xc3]]) {
        const bytes = Buffer.from([...Buffer.from('clone '), ...bad, ...Buffer.from(' x')])
        const file = join(build, `bad-${bad.join('-')}.zhql`)
        writeFileSync(file, bytes)
        for (const book of [[], ['--phrasebook', problemBook]]) {
            compare(`request bytes ${bad.join(' ')} on stdin`, book, bytes)
            compare(`request bytes ${bad.join(' ')} in a file`, [...book, file], '')
        }
    }

    // A request file that can't be read (unless running as root, which reads it anyway).
    const unreadable = join(build, 'unreadable.zhql')
    writeFileSync(unreadable, 'clone reddit.com')
    chmodSync(unreadable, 0o000)
    compare('request file that cannot be read', [unreadable], '')
} finally {
    rmSync(build, { recursive: true, force: true })
}

console.log(`${checked - failed} identical, ${failed} different`)
process.exitCode = failed ? 1 : 0
