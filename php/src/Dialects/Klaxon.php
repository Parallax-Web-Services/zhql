<?php

declare(strict_types=1);

namespace Zhql\Dialects;

/** Klaxon's grammar is separate from the application-building vocabulary. */
final class Klaxon
{
    private const EXPECTED = 'Expected: klaxon probe <host> for <CVE> [and <CVE>], fix, report back, store as kit.';

    private const MARKS = ['comma', 'semicolon', 'newline', 'stop'];

    private int $pos;

    private function __construct(private array $tokens, private int $length, int $first)
    {
        $this->pos = $first;
    }

    /** Parse syntax only. Executing a probe, applying a fix and saving kits belong to Klaxon. */
    public static function parse(array $tokens, int $length, array $errors, int $first): array
    {
        $parser = new self($tokens, $length, $first);
        $clauses = [];
        if ($errors === []) {
            try {
                $clauses = $parser->clauses();
            } catch (\UnexpectedValueException) {
                $token = $tokens[$parser->pos] ?? null;
                $errors[] = [
                    'code' => 'invalid_agent_request', 'message' => self::EXPECTED,
                    'span' => $token === null ? [$length, $length] : [$token['start'], $token['end']],
                ];
            }
        }
        usort($errors, static fn (array $a, array $b): int => $a['span'][0] <=> $b['span'][0]
            ?: $a['span'][1] <=> $b['span'][1] ?: strcmp($a['code'], $b['code']));

        return ['zhql' => \Zhql\Parser::VERSION, 'dialect' => 'klaxon', 'clauses' => $errors === [] ? $clauses : [], 'errors' => $errors];
    }

    private function clauses(): array
    {
        $this->expect('klaxon');
        $probe = $this->pos;
        $this->expect('probe');
        $host = $this->tokens[$this->pos] ?? null;
        if ($host === null || $host['kind'] !== 'word' || ! self::hostname($host['value'])) {
            $this->fail();
        }
        $target = strtolower($host['value']);
        $this->pos++;
        // The shared tokenizer separates the terminal dot of an FQDN.
        if (($this->tokens[$this->pos]['kind'] ?? null) === 'stop'
            && $this->tokens[$this->pos]['value'] === '.' && $this->tokens[$this->pos]['start'] === $host['end']) {
            $this->pos++;
        }
        $this->expect('for');
        $cves = [$this->cve()];
        $end = $this->tokens[$this->pos - 1]['end'];
        while ($this->pos < count($this->tokens)) {
            $this->separate();
            if (! $this->isCve()) {
                break;
            }
            $cve = $this->cve();
            if (! in_array($cve, $cves, true)) {
                $cves[] = $cve;
            }
            $end = $this->tokens[$this->pos - 1]['end'];
        }
        $clauses = [['type' => 'probe', 'target' => $target, 'cves' => $cves, 'span' => [$this->tokens[$probe]['start'], $end]]];
        $seen = [];
        while ($this->pos < count($this->tokens)) {
            $start = $this->pos;
            $type = match (true) {
                $this->word('fix') => 'fix',
                $this->word('report') => 'report_back',
                $this->word('store') => 'store_kit',
                default => null,
            };
            if ($type === null || isset($seen[$type])) {
                $this->fail();
            }
            if ($type === 'fix') {
                $this->expect('fix');
            } elseif ($type === 'report_back') {
                $this->expect('report');
                $this->expect('back');
            } elseif ($type === 'store_kit') {
                $this->expect('store');
                $this->expect('as');
                $this->expect('kit');
            }
            $seen[$type] = true;
            $clauses[] = ['type' => $type, 'span' => [$this->tokens[$start]['start'], $this->tokens[$this->pos - 1]['end']]];
            if ($this->pos < count($this->tokens)) {
                $this->separate();
            }
        }

        return $clauses;
    }

    private static function hostname(string $host): bool
    {
        if (strlen($host) > 253) {
            return false;
        }
        foreach (explode('.', $host) as $label) {
            if (preg_match('/\A[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\z/iD', $label) !== 1) {
                return false;
            }
        }

        return true;
    }

    private function word(string $word): bool
    {
        $token = $this->tokens[$this->pos] ?? null;

        return $token !== null && $token['kind'] === 'word' && strtolower($token['value']) === $word;
    }

    private function expect(string $word): void
    {
        if (! $this->word($word)) {
            $this->fail();
        }
        $this->pos++;
    }

    private function isCve(): bool
    {
        $token = $this->tokens[$this->pos] ?? null;

        return $token !== null && $token['kind'] === 'word'
            && preg_match('/\ACVE-[0-9]{4}-[0-9]{4,}\z/iD', $token['value']) === 1;
    }

    private function cve(): string
    {
        if (! $this->isCve()) {
            $this->fail();
        }

        return strtoupper($this->tokens[$this->pos++]['value']);
    }

    private function separate(): void
    {
        $start = $this->pos;
        while ($this->pos < count($this->tokens) && (in_array($this->tokens[$this->pos]['kind'], self::MARKS, true)
            || $this->word('and') || $this->word('but') || $this->word('then'))) {
            $this->pos++;
        }
        if ($start === $this->pos) {
            $this->fail();
        }
    }

    private function fail(): never
    {
        throw new \UnexpectedValueException;
    }
}
