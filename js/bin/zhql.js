#!/usr/bin/env node
// Parses a ЖQL request and prints the result as JSON.
//
//   zhql "clone reddit.com, call it Savor"   parse the arguments as one request
//   zhql savor.zhql                          parse a query file
//   echo "call it Savor" | zhql              parse standard input
//
// --pretty indents the output. Exits 1 when the result has errors.
import { existsSync, readFileSync, statSync } from 'node:fs'
import { parse } from '../src/index.js'

const args = process.argv.slice(2)
const pretty = args.includes('--pretty')
const rest = args.filter((arg) => arg !== '--pretty')

let request
if (rest.length === 1 && rest[0].endsWith('.zhql') && existsSync(rest[0]) && statSync(rest[0]).isFile()) {
    request = readFileSync(rest[0], 'utf8')
} else if (rest.length === 0 || (rest.length === 1 && rest[0] === '-')) {
    request = readFileSync(0, 'utf8')
} else {
    request = rest.join(' ')
}

const result = parse(request)
process.stdout.write(JSON.stringify(result, null, pretty ? 4 : 0) + '\n')
process.exitCode = result.errors.length ? 1 : 0
