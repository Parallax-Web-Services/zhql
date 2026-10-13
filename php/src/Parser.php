<?php

declare(strict_types=1);

namespace Zhql;

/**
 * ЖQL (zhql) reference parser for PHP. The behaviour is defined by SPEC.md and
 * checked against conformance/; keep the two in step with js/ and go/.
 */
final class Parser
{
    public const VERSION = '0.2';

    private const WHITESPACE = [
        "\u{9}" => true, "\u{A}" => true, "\u{B}" => true, "\u{C}" => true, "\u{D}" => true,
        "\u{20}" => true, "\u{85}" => true, "\u{A0}" => true, "\u{1680}" => true,
        "\u{2000}" => true, "\u{2001}" => true, "\u{2002}" => true, "\u{2003}" => true,
        "\u{2004}" => true, "\u{2005}" => true, "\u{2006}" => true, "\u{2007}" => true,
        "\u{2008}" => true, "\u{2009}" => true, "\u{200A}" => true,
        "\u{2028}" => true, "\u{2029}" => true, "\u{202F}" => true, "\u{205F}" => true, "\u{3000}" => true,
    ];

    private const LINE_BREAKS = [
        "\u{A}" => true, "\u{B}" => true, "\u{C}" => true, "\u{D}" => true,
        "\u{85}" => true, "\u{2028}" => true, "\u{2029}" => true,
    ];

    private const QUOTES = ['"' => '"', "\u{201C}" => "\u{201D}"];

    private const STOPS = ['.' => true, '!' => true, '?' => true];

    private const BOM = "\u{FEFF}";

    // Punctuation tokens. All of them join clauses; inside a list they also separate items.
    private const MARKS = ['comma' => true, 'semicolon' => true, 'newline' => true, 'stop' => true];

    // Longest phrases first, so "make it look like" wins over "make it".
    private const KEYWORDS = [
        [['make', 'it', 'look', 'like'], 'look_like'],
        [['tailor', 'it', 'to'], 'tailor'],
        [['tailor', 'it', 'for'], 'tailor'],
        [['make', 'it'], 'make'],
        [['call', 'it'], 'name'],
        [['name', 'it'], 'name'],
        [['clone'], 'clone'],
        [['with'], 'with'],
        [['use'], 'use'],
        [['only'], 'only'],
        [['ingest'], 'ingest'],
        [['infer'], 'infer'],
        [['generate'], 'generate'],
        [['invent'], 'invent'],
    ];

    private const CONNECTOR_WORDS = ['and' => true, 'but' => true, 'then' => true];

    private const SCALAR_FIELD = [
        'clone' => 'ref', 'look_like' => 'ref', 'make' => 'trait',
        'name' => 'name', 'tailor' => 'audience', 'text' => 'text',
        'infer' => 'subject', 'generate' => 'subject', 'invent' => 'subject',
    ];

    /** @var list<string> */
    private array $cps;

    /** @var list<array{kind: string, value: string, start: int, end: int}> */
    private array $tokens = [];

    /** @var list<array{code: string, message: string, span: array{int, int}}> */
    private array $errors = [];

    private int $n = 0;

    /** @param list<string> $cps */
    private function __construct(array $cps)
    {
        $this->cps = $cps;
    }

    /**
     * @return array{zhql: string, clauses: list<array<string, mixed>>, errors: list<array{code: string, message: string, span: array{int, int}}>}
     */
    public static function parse(string $request): array
    {
        if (preg_match('//u', $request) !== 1) {
            return ['zhql' => self::VERSION, 'clauses' => [], 'errors' => [self::error('invalid_encoding', 0, 0)]];
        }

        $cps = $request === '' ? [] : preg_split('//u', $request, -1, PREG_SPLIT_NO_EMPTY);

        return (new self($cps))->run();
    }

    private function run(): array
    {
        $this->tokenize();
        $this->n = count($this->tokens);
        $clauses = [];

        $pos = 0;
        while ($pos < $this->n) {
            while ($pos < $this->n && $this->isConnector($pos)) {
                $pos++;
            }
            if ($pos >= $this->n) {
                break;
            }

            $kw = $this->keywordAt($pos);
            if ($kw === null) {
                $end = $this->boundary($pos, 'text');
                $text = $this->scalar($pos, $end);
                if ($text !== '') {
                    $clauses[] = ['type' => 'text', 'text' => $text, 'span' => [$this->tokens[$pos]['start'], $this->tokens[$end - 1]['end']]];
                }
                $pos = $end;

                continue;
            }

            [$words, $type] = $kw;
            $phrase = implode(' ', $words);
            $start = $this->tokens[$pos]['start'];
            $kwEnd = $this->tokens[$pos + count($words) - 1]['end'];
            $valueStart = $pos + count($words);

            if ($type === 'ingest') {
                $token = $this->tokens[$valueStart] ?? null;
                if ($token === null || ($token['kind'] !== 'word' && $token['kind'] !== 'quoted') || $token['value'] === '') {
                    $this->errors[] = self::error('missing_value', $start, $kwEnd, $phrase);
                    $pos = $valueStart;
                } else {
                    $clauses[] = ['type' => 'ingest', 'path' => $token['value'], 'span' => [$start, $token['end']]];
                    $pos = $valueStart + 1;
                }

                continue;
            }

            $end = $this->boundary($valueStart, $type);
            $span = [$start, $end > $valueStart ? $this->tokens[$end - 1]['end'] : $kwEnd];

            if ($type === 'with' || $type === 'only') {
                $items = $this->items($valueStart, $end);
                if ($items !== []) {
                    $clauses[] = ['type' => $type, 'items' => $items, 'span' => $span];
                } else {
                    $this->errors[] = self::error('missing_value', $start, $kwEnd, $phrase);
                }
            } elseif ($type === 'use') {
                $split = -1;
                for ($k = $valueStart; $k + 1 < $end; $k++) {
                    if ($this->isWord($k, 'instead') && $this->isWord($k + 1, 'of')) {
                        $split = $k;
                        break;
                    }
                }
                $value = $this->scalar($valueStart, $split === -1 ? $end : $split);
                $insteadOf = $split === -1 ? null : $this->scalar($split + 2, $end);
                if ($value === '') {
                    $this->errors[] = self::error('missing_value', $start, $kwEnd, $phrase);
                }
                if ($insteadOf === '') {
                    $this->errors[] = self::error('missing_value', $this->tokens[$split]['start'], $this->tokens[$split + 1]['end'], 'instead of');
                }
                if ($value !== '' && $insteadOf !== '') {
                    $clauses[] = ['type' => 'use', 'value' => $value, 'instead_of' => $insteadOf, 'span' => $span];
                }
            } else {
                $value = $this->scalar($valueStart, $end);
                if ($value !== '') {
                    $clauses[] = ['type' => $type, self::SCALAR_FIELD[$type] => $value, 'span' => $span];
                } else {
                    $this->errors[] = self::error('missing_value', $start, $kwEnd, $phrase);
                }
            }
            $pos = $end;
        }

        if ($clauses === [] && $this->errors === []) {
            $this->errors[] = self::error('empty_request', 0, count($this->cps));
        }
        usort($this->errors, static fn (array $x, array $y): int => [$x['span'][0], $x['span'][1], $x['code']] <=> [$y['span'][0], $y['span'][1], $y['code']]);

        return ['zhql' => self::VERSION, 'clauses' => $clauses, 'errors' => $this->errors];
    }

    private function tokenize(): void
    {
        $cps = $this->cps;
        $n = count($cps);
        $i = ($cps[0] ?? null) === self::BOM ? 1 : 0;
        $lineStart = true;

        while ($i < $n) {
            $ch = $cps[$i];
            if (isset(self::LINE_BREAKS[$ch])) {
                $this->tokens[] = ['kind' => 'newline', 'value' => $ch, 'start' => $i, 'end' => $i + 1];
                $lineStart = true;
                $i++;

                continue;
            }
            if (isset(self::WHITESPACE[$ch])) {
                $i++;

                continue;
            }
            // "# " as the first thing on a line comments out the rest of that line.
            if ($lineStart && $ch === '#' && ($i + 1 === $n || isset(self::WHITESPACE[$cps[$i + 1]]))) {
                while ($i < $n && ! isset(self::LINE_BREAKS[$cps[$i]])) {
                    $i++;
                }

                continue;
            }
            $lineStart = false;

            if ($ch === ',' || $ch === ';') {
                $this->tokens[] = ['kind' => $ch === ',' ? 'comma' : 'semicolon', 'value' => $ch, 'start' => $i, 'end' => $i + 1];
                $i++;

                continue;
            }

            if (isset(self::QUOTES[$ch])) {
                $close = self::QUOTES[$ch];
                $j = $i + 1;
                $value = '';
                $closed = false;
                while ($j < $n) {
                    if ($cps[$j] === '\\' && $j + 1 < $n) {
                        $value .= $cps[$j + 1];
                        $j += 2;

                        continue;
                    }
                    if ($cps[$j] === $close) {
                        $closed = true;
                        $j++;
                        break;
                    }
                    $value .= $cps[$j++];
                }
                if (! $closed) {
                    $this->errors[] = self::error('unterminated_quote', $i, $n);
                }
                $this->tokens[] = ['kind' => 'quoted', 'value' => $value, 'start' => $i, 'end' => $j];
                $i = $j;

                continue;
            }

            $j = $i;
            while ($j < $n && ! isset(self::WHITESPACE[$cps[$j]]) && $cps[$j] !== ',' && $cps[$j] !== ';') {
                $j++;
            }
            // Sentence punctuation that ends a word is a separate "stop" token.
            $stop = $j;
            while ($stop > $i && isset(self::STOPS[$cps[$stop - 1]])) {
                $stop--;
            }
            if ($stop > $i) {
                $this->tokens[] = ['kind' => 'word', 'value' => $this->text($i, $stop), 'start' => $i, 'end' => $stop];
            }
            if ($stop < $j) {
                $this->tokens[] = ['kind' => 'stop', 'value' => $this->text($stop, $j), 'start' => $stop, 'end' => $j];
            }
            $i = $j;
        }
    }

    private function isWord(int $k, string $word): bool
    {
        return $k < $this->n && $this->tokens[$k]['kind'] === 'word' && strtolower($this->tokens[$k]['value']) === $word;
    }

    private function isConnector(int $k): bool
    {
        if ($k >= $this->n) {
            return false;
        }
        $token = $this->tokens[$k];

        return isset(self::MARKS[$token['kind']])
            || ($token['kind'] === 'word' && isset(self::CONNECTOR_WORDS[strtolower($token['value'])]));
    }

    /** @return array{list<string>, string}|null */
    private function keywordAt(int $k): ?array
    {
        foreach (self::KEYWORDS as $keyword) {
            foreach ($keyword[0] as $offset => $word) {
                if (! $this->isWord($k + $offset, $word)) {
                    continue 2;
                }
            }

            return $keyword;
        }

        return null;
    }

    /**
     * A clause ends at a run of connectors followed by a keyword or by the end of
     * the request. A clone reference also ends at an unquoted "with".
     */
    private function boundary(int $from, string $type): int
    {
        $j = $from;
        while ($j < $this->n) {
            if ($type === 'clone' && $this->isWord($j, 'with')) {
                return $j;
            }
            if ($this->isConnector($j)) {
                $k = $j;
                while ($k < $this->n && $this->isConnector($k)) {
                    $k++;
                }
                if ($k === $this->n || $this->keywordAt($k) !== null) {
                    return $j;
                }
                $j = $k;

                continue;
            }
            $j++;
        }

        return $this->n;
    }

    /**
     * A value is its tokens' source text, with every gap between them (whitespace,
     * line breaks, comments) written as one space. A lone quoted token is unquoted.
     */
    private function scalar(int $a, int $b): string
    {
        $parts = array_values(array_filter(
            array_slice($this->tokens, $a, max(0, $b - $a)),
            static fn (array $t): bool => $t['kind'] !== 'newline',
        ));
        while ($parts !== [] && isset(self::MARKS[$parts[0]['kind']])) {
            array_shift($parts);
        }
        if (count($parts) === 1 && $parts[0]['kind'] === 'quoted') {
            return $parts[0]['value'];
        }
        $out = '';
        $prevEnd = -1;
        foreach ($parts as $t) {
            if ($prevEnd !== -1 && $t['start'] > $prevEnd) {
                $out .= ' ';
            }
            $out .= $this->text($t['start'], $t['end']);
            $prevEnd = $t['end'];
        }

        return $out;
    }

    /** @return list<string> */
    private function items(int $a, int $b): array
    {
        $marked = false;
        for ($k = $a; $k < $b; $k++) {
            if (isset(self::MARKS[$this->tokens[$k]['kind']])) {
                $marked = true;
            }
        }
        $items = [];
        $groupStart = $a;
        for ($k = $a; $k <= $b; $k++) {
            if ($k < $b && ! $this->isConnector($k)) {
                continue;
            }
            if ($k > $groupStart) {
                if ($marked) {
                    $items[] = $this->scalar($groupStart, $k);
                } else {
                    for ($t = $groupStart; $t < $k; $t++) {
                        $items[] = $this->tokens[$t]['value'];
                    }
                }
            }
            $groupStart = $k + 1;
        }

        return array_values(array_filter($items, static fn (string $item): bool => $item !== ''));
    }

    private function text(int $start, int $end): string
    {
        return implode('', array_slice($this->cps, $start, $end - $start));
    }

    /** @return array{code: string, message: string, span: array{int, int}} */
    private static function error(string $code, int $start, int $end, string $phrase = ''): array
    {
        $message = match ($code) {
            'empty_request' => 'The request is empty.',
            'invalid_encoding' => 'The request is not valid Unicode text.',
            'unterminated_quote' => 'A quoted value is missing its closing quote.',
            'missing_value' => '"'.$phrase.'" needs a value.',
        };

        return ['code' => $code, 'message' => $message, 'span' => [$start, $end]];
    }
}
