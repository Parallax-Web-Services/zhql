// Runs every case in ../../conformance against the JavaScript parser.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { parse } from '../src/index.js'

const dir = fileURLToPath(new URL('../../conformance/', import.meta.url))

for (const file of readdirSync(dir).filter((name) => name.endsWith('.json')).sort()) {
    const suite = JSON.parse(readFileSync(dir + file, 'utf8'))
    for (const { name, input, expect } of suite.cases) {
        test(`${file.replace('.json', '')}: ${name}`, () => assert.deepStrictEqual(parse(input), expect))
    }
}

// Not expressible in JSON fixtures: a string that is not valid Unicode.
test('a lone surrogate is reported as invalid_encoding', () => {
    const result = parse('call it \ud83c')
    assert.deepStrictEqual(result.clauses, [])
    assert.deepStrictEqual(result.errors.map((e) => e.code), ['invalid_encoding'])
})
