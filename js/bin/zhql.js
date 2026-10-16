#!/usr/bin/env node
// Parses a ЖQL request and prints the result as JSON.
//
//   zhql "clone reddit.com, call it Savor"   parse the arguments as one request
//   zhql savor.zhql                          parse a query file
//   echo "call it Savor" | zhql              parse standard input
//
// --pretty indents the output. --phrasebook FILE reads a JSON object of phrases
// and their meanings. Exits 1 when the request has errors, 2 when the phrasebook
// file is unusable.
import { existsSync, readFileSync, statSync } from 'node:fs'
import { parse } from '../src/index.js'

const args = process.argv.slice(2)
let pretty = false
let phrasebookFile = null
const rest = []
for (let i = 0; i < args.length; i++) {
    if (args[i] === '--pretty') pretty = true
    else if (args[i] === '--phrasebook') {
        if (i + 1 === args.length) {
            process.stderr.write('zhql: --phrasebook needs a file.\n')
            process.exit(2)
        }
        phrasebookFile = args[++i]
    } else rest.push(args[i])
}

const isFile = (path) => existsSync(path) && statSync(path).isFile()

let phrasebook
if (phrasebookFile !== null) {
    try {
        if (!isFile(phrasebookFile)) throw new Error('not a file')
        const text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(readFileSync(phrasebookFile))
        phrasebook = JSON.parse(text)
        const proto = phrasebook !== null && typeof phrasebook === 'object' ? Object.getPrototypeOf(phrasebook) : undefined
        if (proto !== Object.prototype) throw new Error('not an object')
        parse('', phrasebook) // rejects meanings that are not strings, and invalid Unicode
    } catch {
        process.stderr.write(`zhql: ${phrasebookFile} is not a phrasebook (a JSON object of phrases and meanings).\n`)
        process.exit(2)
    }
}

// Files and standard input are passed to parse() as bytes, so text that is not
// UTF-8 is reported as invalid_encoding. Node has already decoded arguments,
// replacing such bytes with U+FFFD, so pass those requests in a file instead.
const readOrExit = (source, name) => {
    try {
        return readFileSync(source)
    } catch {
        process.stderr.write(`zhql: cannot read ${name}.\n`)
        process.exit(2)
    }
}

let request
if (rest.length === 1 && rest[0].endsWith('.zhql') && isFile(rest[0])) {
    request = readOrExit(rest[0], rest[0])
} else if (rest.length === 0 || (rest.length === 1 && rest[0] === '-')) {
    request = readOrExit(0, 'standard input')
} else {
    request = rest.join(' ')
}

const result = parse(request, phrasebook)
process.stdout.write(JSON.stringify(result, null, pretty ? 4 : 0) + '\n')
process.exitCode = result.errors.length ? 1 : 0
