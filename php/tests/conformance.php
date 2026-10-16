<?php

declare(strict_types=1);

// Runs every case in ../../conformance against the PHP parser.
// Usage: php php/tests/conformance.php

require __DIR__.'/../src/Parser.php';

use Zhql\Parser;

$passed = 0;
$failed = 0;

foreach (glob(__DIR__.'/../../conformance/*.json') as $file) {
    $suite = json_decode(file_get_contents($file), true, 512, JSON_THROW_ON_ERROR);
    foreach ($suite['cases'] as $case) {
        $actual = Parser::parse($case['input'], $case['phrasebook'] ?? null);
        if ($actual === $case['expect']) {
            $passed++;

            continue;
        }
        $failed++;
        $flags = JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES;
        fwrite(STDERR, sprintf(
            "FAIL %s: %s\n  input:    %s\n  expected: %s\n  actual:   %s\n",
            basename($file, '.json'), $case['name'], json_encode($case['input'], $flags),
            json_encode($case['expect'], $flags), json_encode($actual, $flags),
        ));
    }
}

// Not expressible in JSON fixtures: bytes that are not UTF-8, and phrasebooks
// that are not phrasebooks.
$check = static function (string $name, bool $ok) use (&$passed, &$failed): void {
    $ok ? $passed++ : $failed++;
    if (! $ok) {
        fwrite(STDERR, "FAIL {$name}\n");
    }
};
$throws = static function (callable $parse): bool {
    try {
        $parse();
    } catch (InvalidArgumentException) {
        return true;
    }

    return false;
};

$invalid = Parser::parse("call it \xC3\x28");
$check('invalid UTF-8 is reported as invalid_encoding', $invalid['clauses'] === [] && array_column($invalid['errors'], 'code') === ['invalid_encoding']);
$invalid = Parser::parse("call it \xC3\x28", ['Ab' => 'clone']);
$check('phrasebook problems are kept with invalid_encoding', array_column($invalid['phrasebook_problems'] ?? [], 'phrase') === ['Ab']);
$check('a meaning that is not a string is rejected', $throws(static fn () => Parser::parse('x', ['spin up' => 1])));
$check('a phrase that is not UTF-8 is rejected', $throws(static fn () => Parser::parse('x', ["sp\xC3\x28" => 'clone'])));
$check('a meaning that is not UTF-8 is rejected', $throws(static fn () => Parser::parse('x', ['spin' => "cl\xC3\x28"])));
$check('an object phrasebook works', Parser::parse('spin up x', (object) ['spin up' => 'clone'])['clauses'][0]['type'] === 'clone');

printf("%d passed, %d failed\n", $passed, $failed);
exit($failed === 0 ? 0 : 1);
