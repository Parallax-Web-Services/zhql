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
        $actual = Parser::parse($case['input']);
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

// Not expressible in JSON fixtures: bytes that are not UTF-8.
$invalid = Parser::parse("call it \xC3\x28");
if ($invalid['clauses'] === [] && array_column($invalid['errors'], 'code') === ['invalid_encoding']) {
    $passed++;
} else {
    $failed++;
    fwrite(STDERR, "FAIL invalid UTF-8 is reported as invalid_encoding\n");
}

printf("%d passed, %d failed\n", $passed, $failed);
exit($failed === 0 ? 0 : 1);
