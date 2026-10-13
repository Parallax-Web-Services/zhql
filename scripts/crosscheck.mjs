// Feeds every conformance input to the PHP, JavaScript and Go command-line tools
// on standard input and checks that all three print exactly the same bytes,
// compact and with --pretty. Needs php, node and go on the PATH.
//
// Usage: node scripts/crosscheck.mjs
import { execFileSync, spawnSync } from 'node:child_process'
import { mkdtempSync, readdirSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('..', import.meta.url))
const build = mkdtempSync(join(tmpdir(), 'zhql-'))
const goBinary = join(build, 'zhql')
execFileSync('go', ['build', '-o', goBinary, './go/cmd/zhql'], { cwd: root, stdio: 'inherit' })

const tools = {
    php: ['php', [join(root, 'php/bin/zhql')]],
    js: ['node', [join(root, 'js/bin/zhql.js')]],
    go: [goBinary, []],
}

let checked = 0
let failed = 0
try {
    for (const file of readdirSync(join(root, 'conformance')).filter((name) => name.endsWith('.json')).sort()) {
        const { cases } = JSON.parse(readFileSync(join(root, 'conformance', file), 'utf8'))
        for (const { name, input } of cases) {
            for (const flags of [[], ['--pretty']]) {
                const outputs = Object.entries(tools).map(([tool, [command, args]]) => {
                    const run = spawnSync(command, [...args, ...flags], { input, encoding: 'utf8' })
                    return [tool, `${run.status}\n${run.stdout}${run.stderr}`]
                })
                checked++
                if (outputs.some(([, out]) => out !== outputs[0][1])) {
                    failed++
                    console.error(`DIFFERENT ${file}: ${name} ${flags.join(' ')}`)
                    for (const [tool, out] of outputs) console.error(`  ${tool}: ${JSON.stringify(out)}`)
                }
            }
        }
    }
} finally {
    rmSync(build, { recursive: true, force: true })
}

console.log(`${checked - failed} identical, ${failed} different`)
process.exitCode = failed ? 1 : 0
