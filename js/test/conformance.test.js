// Runs every case in ../../conformance against the JavaScript parser.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { parse } from '../src/index.js'

const dir = fileURLToPath(new URL('../../conformance/', import.meta.url))

for (const file of readdirSync(dir).filter((name) => name.endsWith('.json')).sort()) {
    const suite = JSON.parse(readFileSync(dir + file, 'utf8'))
    for (const { name, input, phrasebook, expect } of suite.cases) {
        // Compared as JSON text, so key order counts too.
        test(`${file.replace('.json', '')}: ${name}`, () => assert.equal(JSON.stringify(parse(input, phrasebook)), JSON.stringify(expect)))
    }
}

// Not expressible in JSON fixtures: strings that are not valid Unicode, and
// phrasebooks that are not phrasebooks.
test('a lone surrogate is reported as invalid_encoding', () => {
    const result = parse('call it \ud83c')
    assert.deepStrictEqual(result.clauses, [])
    assert.deepStrictEqual(result.errors.map((e) => e.code), ['invalid_encoding'])
    assert.deepStrictEqual(parse('call it \ud83c', { Ab: 'clone' }).phrasebook_problems.map((p) => p.phrase), ['Ab'])
})

test('a request can be UTF-8 bytes', () => {
    assert.equal(JSON.stringify(parse(Buffer.from('call it Ж'))), JSON.stringify(parse('call it Ж')))
    assert.equal(JSON.stringify(parse(new TextEncoder().encode('\ufeffcall it Savor'))), JSON.stringify(parse('\ufeffcall it Savor')))
    for (const bytes of [[0xff], [0xc0, 0x80], [0xed, 0xa0, 0x80], [0xf4, 0x90, 0x80, 0x80]]) {
        const result = parse(Uint8Array.from([0x63, 0x6c, 0x6f, 0x6e, 0x65, 0x20, ...bytes]), { Ab: 'clone' })
        assert.deepStrictEqual(result.errors.map((e) => e.code), ['invalid_encoding'])
        assert.deepStrictEqual(result.phrasebook_problems.map((p) => p.phrase), ['Ab'])
    }
    assert.throws(() => parse(42), TypeError)
})

test('a phrasebook must be an object of strings in valid Unicode', () => {
    for (const book of [[], 'clone', new Map([['spin', 'clone']]), { spin: 1 }, { spin: null }, { 'sp\ud800': 'clone' }, { spin: 'cl\udc00' }]) {
        assert.throws(() => parse('x', book), TypeError)
    }
    assert.equal(parse('spin x', Object.assign(Object.create(null), { spin: 'clone' })).clauses[0].type, 'clone')
})
