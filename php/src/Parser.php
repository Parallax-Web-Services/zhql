<?php

declare(strict_types=1);

namespace Zhql;

/**
 * ЖQL (zhql) reference parser for PHP. The behaviour is defined by SPEC.md and
 * checked against conformance/; keep it in step with js/, go/ and python/.
 */
final class Parser
{
    public const VERSION = '0.3';

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
    public const KEYWORDS = [
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

    // A phrase may not contain these words or any keyword, so it can never take
    // over or swallow a built-in.
    private const RESERVED_WORDS = ['and' => true, 'but' => true, 'then' => true, 'instead' => true];

    private const MAX_PHRASE_WORDS = 16;

    private const SCALAR_FIELD = [
        'clone' => 'ref', 'look_like' => 'ref', 'make' => 'trait',
        'name' => 'name', 'tailor' => 'audience', 'text' => 'text',
        'infer' => 'subject', 'generate' => 'subject', 'invent' => 'subject',
    ];

    /** @var list<string> */
    private array $cps;

    /** @var list<array{kind: string, value: string, start: int, end: int}> */
    private array $tokens = [];

    /** @var list<array<string, mixed>> */
    private array $errors = [];

    private int $n = 0;

    /**
     * Phrases as a tree of words, so finding the longest phrase at a token costs
     * no more than the words it walks. Each node is ['next' => [word => node], 'entry' => ?entry].
     *
     * @var array<string, mixed>
     */
    private array $phrases;

    /**
     * @param  list<string>  $cps
     * @param  array<string, mixed>|null  $phrases
     */
    private function __construct(array $cps, ?array $phrases = null)
    {
        $this->cps = $cps;
        $this->phrases = $phrases ?? ['next' => [], 'entry' => null];
    }

    /**
     * Parse one ЖQL request, such as the content of a .zhql file, optionally with
     * a phrasebook: an array or object of phrases and their meanings.
     *
     * @param  array<array-key, mixed>|\stdClass|null  $phrasebook
     * @return array<string, mixed>
     */
    public static function parse(string $request, array|\stdClass|null $phrasebook = null): array
    {
        [$phrases, $problems] = self::readPhrasebook($phrasebook);
        $finish = static fn (array $result): array => $problems === [] ? $result : $result + ['phrasebook_problems' => $problems];

        if (preg_match('//u', $request) !== 1) {
            return $finish(['zhql' => self::VERSION, 'clauses' => [], 'errors' => [self::error('invalid_encoding', 0, 0)]]);
        }

        $parser = new self(self::codePoints($request), $phrases);
        $parser->tokenize();

        // An explicit agent name selects its vocabulary before the core grammar.
        $first = 0;
        while (isset($parser->tokens[$first]) && isset(self::MARKS[$parser->tokens[$first]['kind']])) {
            $first++;
        }
        $token = $parser->tokens[$first] ?? null;
        if ($token !== null && $token['kind'] === 'word' && strtolower($token['value']) === 'klaxon') {
            require_once __DIR__.'/Dialects/Klaxon.php';

            return $finish(Dialects\Klaxon::parse($parser->tokens, count($parser->cps), $parser->errors, $first));
        }

        return $finish($parser->run());
    }

    /** @return list<string> */
    private static function codePoints(string $text): array
    {
        return $text === '' ? [] : preg_split('//u', $text, -1, PREG_SPLIT_NO_EMPTY);
    }

    /**
     * @param  array<array-key, mixed>|\stdClass|null  $book
     * @return array{array<string, mixed>, list<array{code: string, message: string, phrase: string}>}
     */
    private static function readPhrasebook(array|\stdClass|null $book): array
    {
        $root = ['next' => [], 'entry' => null];
        if ($book === null) {
            return [$root, []];
        }
        $book = $book instanceof \stdClass ? get_object_vars($book) : $book;
        $entries = [];
        foreach ($book as $phrase => $means) {
            // PHP turns keys such as "10" into integers.
            $phrase = (string) $phrase;
            if (! is_string($means)) {
                throw new \InvalidArgumentException('zhql: every meaning in a phrasebook must be a string');
            }
            if (preg_match('//u', $phrase) !== 1 || preg_match('//u', $means) !== 1) {
                throw new \InvalidArgumentException('zhql: a phrasebook must be valid Unicode text');
            }
            $entries[$phrase] = $means;
        }
        uksort($entries, static fn (int|string $a, int|string $b): int => strcmp((string) $a, (string) $b));

        $problems = [];
        foreach ($entries as $phrase => $means) {
            $phrase = (string) $phrase;
            $entry = self::readEntry($phrase, $means);
            if (isset($entry['problem'])) {
                $problems[] = $entry['problem'];

                continue;
            }
            $node = &$root;
            foreach ($entry['words'] as $word) {
                $node['next'][$word] ??= ['next' => [], 'entry' => null];
                $node = &$node['next'][$word];
            }
            $node['entry'] = $entry; // two valid phrases never have the same words
            unset($node);
        }

        return [$root, $problems];
    }

    /**
     * Checks one entry. Depends on nothing but the entry.
     *
     * @return array<string, mixed>
     */
    private static function readEntry(string $phrase, string $means): array
    {
        $problem = static fn (string $message): array => ['problem' => [
            'code' => 'invalid_phrase', 'message' => '"'.$phrase.'" '.$message, 'phrase' => $phrase,
        ]];

        $reader = new self(self::codePoints($phrase));
        $reader->tokenize();
        $words = array_column($reader->tokens, 'value');
        $plain = $reader->tokens !== [] && implode(' ', $words) === $phrase;
        foreach ($reader->tokens as $token) {
            $plain = $plain && $token['kind'] === 'word';
        }
        if (! $plain) {
            return $problem('must be plain words separated by single spaces.');
        }
        if (count($words) > self::MAX_PHRASE_WORDS) {
            return $problem('must be at most '.self::MAX_PHRASE_WORDS.' words.');
        }
        if (preg_match('/[A-Z]/', $phrase) === 1) {
            return $problem('must be written in lower case.');
        }
        foreach ($words as $word) {
            if (isset(self::RESERVED_WORDS[$word])) {
                return $problem('contains the reserved word "'.$word.'".');
            }
        }
        foreach (array_keys($words) as $i) {
            foreach (self::KEYWORDS as [$keywordWords]) {
                if (array_slice($words, $i, count($keywordWords)) === $keywordWords) {
                    return $problem('contains the keyword "'.implode(' ', $keywordWords).'".');
                }
            }
        }

        foreach (self::KEYWORDS as $keyword) {
            if (strtolower($means) === implode(' ', $keyword[0])) {
                return ['phrase' => $phrase, 'means' => $means, 'words' => $words, 'alias' => $keyword];
            }
        }

        $plainResult = self::parse($means);
        $usable = ! isset($plainResult['dialect']) && $plainResult['clauses'] !== [] && $plainResult['errors'] === [];
        foreach ($plainResult['clauses'] as $clause) {
            $usable = $usable && $clause['type'] !== 'text' && $clause['type'] !== 'ingest';
        }
        if (! $usable) {
            return $problem('must mean a keyword or whole clauses, not "'.$means.'".');
        }
        $clauses = array_map(static function (array $clause): array {
            unset($clause['span']);

            return $clause;
        }, $plainResult['clauses']);

        return [
            'phrase' => $phrase, 'means' => $means, 'words' => $words, 'clauses' => $clauses,
            'listType' => count($clauses) === 1 ? $clauses[0]['type'] : null,
        ];
    }

    /** @return array<string, mixed> */
    private function run(): array
    {
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

            $start = $this->startAt($pos);
            if ($start === null) {
                $end = $this->boundary($pos, 'text');
                $text = $this->scalar($pos, $end);
                if ($text !== '') {
                    $clauses[] = ['type' => 'text', 'text' => $text, 'span' => [$this->tokens[$pos]['start'], $this->tokens[$end - 1]['end']]];
                }
                $pos = $end;

                continue;
            }

            if (isset($start['shorthand'])) {
                $match = $this->match($start['shorthand'], $pos);
                foreach ($start['shorthand']['clauses'] as $fields) {
                    $clauses[] = $fields + ['span' => $match['span'], 'via' => [$match]];
                }
                $pos += $start['length'];

                continue;
            }

            [$words, $type] = $start['keyword'];
            $phrase = isset($start['entry']) ? $start['entry']['phrase'] : implode(' ', $words);
            $via = isset($start['entry']) ? [$this->match($start['entry'], $pos)] : [];
            $first = $this->tokens[$pos]['start'];
            $kwEnd = $this->tokens[$pos + $start['length'] - 1]['end'];
            $valueStart = $pos + $start['length'];

            if ($type === 'ingest') {
                $token = $this->tokens[$valueStart] ?? null;
                if ($token === null || ($token['kind'] !== 'word' && $token['kind'] !== 'quoted') || $token['value'] === '') {
                    $this->errors[] = self::withVia(self::error('missing_value', $first, $kwEnd, $phrase), $via);
                    $pos = $valueStart;
                } else {
                    $clauses[] = self::withVia(['type' => 'ingest', 'path' => $token['value'], 'span' => [$first, $token['end']]], $via);
                    $pos = $valueStart + 1;
                }

                continue;
            }

            $end = $this->boundary($valueStart, $type);
            $span = [$first, $end > $valueStart ? $this->tokens[$end - 1]['end'] : $kwEnd];

            if ($type === 'with' || $type === 'only') {
                $items = $this->items($valueStart, $end, $type, $via);
                if ($items !== []) {
                    $clauses[] = self::withVia(['type' => $type, 'items' => $items, 'span' => $span], $via);
                } else {
                    $this->errors[] = self::withVia(self::error('missing_value', $first, $kwEnd, $phrase), $via);
                }
            } elseif ($type === 'use') {
                $split = -1;
                for ($k = $valueStart; $k + 1 < $end; $k++) {
                    if ($this->isWord($k, 'instead') && $this->isWord($k + 1, 'of')) {
                        $split = $k;
                        break;
                    }
                }
                // Connectors just before "instead of" belong to neither part.
                $valueEnd = $split === -1 ? $end : $split;
                while ($valueEnd > $valueStart && $this->isConnector($valueEnd - 1)) {
                    $valueEnd--;
                }
                $value = $this->scalar($valueStart, $valueEnd);
                $insteadOf = $split === -1 ? null : $this->scalar($split + 2, $end);
                if ($value === '') {
                    $this->errors[] = self::withVia(self::error('missing_value', $first, $kwEnd, $phrase), $via);
                }
                if ($insteadOf === '') {
                    $this->errors[] = self::withVia(self::error('missing_value', $this->tokens[$split]['start'], $this->tokens[$split + 1]['end'], 'instead of'), $via);
                }
                if ($value !== '' && $insteadOf !== '') {
                    $clauses[] = self::withVia(['type' => 'use', 'value' => $value, 'instead_of' => $insteadOf, 'span' => $span], $via);
                }
            } else {
                $value = $this->scalar($valueStart, $end);
                if ($value !== '') {
                    $clauses[] = self::withVia(['type' => $type, self::SCALAR_FIELD[$type] => $value, 'span' => $span], $via);
                } else {
                    $this->errors[] = self::withVia(self::error('missing_value', $first, $kwEnd, $phrase), $via);
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

    private function wordAt(int $k): ?string
    {
        return $k < $this->n && $this->tokens[$k]['kind'] === 'word' ? strtolower($this->tokens[$k]['value']) : null;
    }

    /**
     * The longest phrase whose words start at token k.
     *
     * @return array<string, mixed>|null
     */
    private function longestPhrase(int $k): ?array
    {
        $node = $this->phrases;
        $found = null;
        for ($i = $k; ($word = $this->wordAt($i)) !== null && isset($node['next'][$word]); $i++) {
            $node = $node['next'][$word];
            $found = $node['entry'] ?? $found;
        }

        return $found;
    }

    /**
     * @param  array<string, mixed>  $entry
     * @return array{phrase: string, means: string, span: array{int, int}}
     */
    private function match(array $entry, int $k): array
    {
        return [
            'phrase' => $entry['phrase'], 'means' => $entry['means'],
            'span' => [$this->tokens[$k]['start'], $this->tokens[$k + count($entry['words']) - 1]['end']],
        ];
    }

    /**
     * What starts a clause at token k: a keyword, else the longest phrase, else nothing.
     *
     * @return array<string, mixed>|null
     */
    private function startAt(int $k): ?array
    {
        $keyword = $this->keywordAt($k);
        if ($keyword !== null) {
            return ['keyword' => $keyword, 'length' => count($keyword[0])];
        }
        $entry = $this->longestPhrase($k);
        if ($entry === null) {
            return null;
        }

        return isset($entry['alias'])
            ? ['keyword' => $entry['alias'], 'length' => count($entry['words']), 'entry' => $entry]
            : ['shorthand' => $entry, 'length' => count($entry['words'])];
    }

    /**
     * A one-clause shorthand of the list's own type, standing exactly for tokens a..b-1.
     *
     * @return array<string, mixed>|null
     */
    private function listShorthand(int $a, int $b, string $type): ?array
    {
        $node = $this->phrases;
        for ($i = $a; $i < $b; $i++) {
            $word = $this->wordAt($i);
            if ($word === null || ! isset($node['next'][$word])) {
                return null;
            }
            $node = $node['next'][$word];
        }
        $entry = $node['entry'];

        return $entry !== null && ($entry['listType'] ?? null) === $type ? $entry : null;
    }

    /**
     * A clause ends at a run of connectors followed by a keyword, a phrase, or the
     * end of the request. A clone reference also ends at "with" or an alias of it.
     * Inside a with/only list, a one-clause shorthand of the same type joins the list.
     */
    private function boundary(int $from, string $type): int
    {
        $j = $from;
        while ($j < $this->n) {
            if ($type === 'clone') {
                $start = $this->startAt($j);
                if (isset($start['keyword']) && $start['keyword'][1] === 'with') {
                    return $j;
                }
            }
            if ($this->isConnector($j)) {
                $k = $j;
                while ($k < $this->n && $this->isConnector($k)) {
                    $k++;
                }
                if ($k === $this->n) {
                    return $j;
                }
                $start = $this->startAt($k);
                $joinsList = isset($start['shorthand']) && ($type === 'with' || $type === 'only') && $start['shorthand']['listType'] === $type;
                if ($start !== null && ! $joinsList) {
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
        $first = 0;
        while ($first < count($parts) && isset(self::MARKS[$parts[$first]['kind']])) {
            $first++;
        }
        $parts = array_slice($parts, $first);
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

    /**
     * @param  list<array<string, mixed>>  $via  receives a match for each shorthand used
     * @return list<string>
     */
    private function items(int $a, int $b, string $type, array &$via): array
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
                $shorthand = $this->listShorthand($groupStart, $k, $type);
                if ($shorthand !== null) {
                    foreach ($shorthand['clauses'][0]['items'] as $item) {
                        $items[] = $item;
                    }
                    $via[] = $this->match($shorthand, $groupStart);
                } elseif ($marked) {
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

    /**
     * @param  array<string, mixed>  $object
     * @param  list<array<string, mixed>>  $via
     * @return array<string, mixed>
     */
    private static function withVia(array $object, array $via): array
    {
        return $via === [] ? $object : $object + ['via' => $via];
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
