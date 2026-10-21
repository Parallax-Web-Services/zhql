package zhql

import "testing"

func TestKlaxonTypedResult(t *testing.T) {
	const input = "klaxon probe web01.coolframes.com for CVE-2026-109618 and CVE-2026-109619, fix, report back, store as kit"
	result := Parse(input)
	if result.Dialect != "klaxon" || len(result.Errors) != 0 || len(result.Clauses) != 4 {
		t.Fatalf("unexpected result: %s", result.JSON(false))
	}
	probe := result.Clauses[0]
	if probe.Target != "web01.coolframes.com" || len(probe.CVEs) != 2 || probe.CVEs[1] != "CVE-2026-109619" {
		t.Fatalf("typed probe fields are missing: %#v", probe)
	}
	result.Clauses[0].CVEs[0] = "mutated by consumer"
	if next := Parse(input); next.Clauses[0].CVEs[0] != "CVE-2026-109618" {
		t.Fatal("parses share mutable CVE storage")
	}
	if core := Parse("clone example.com"); core.Dialect != "" {
		t.Fatalf("agent dialect leaked into core parse: %s", core.JSON(false))
	}
}

func TestKlaxonInvalidUTF8(t *testing.T) {
	result := Parse("klaxon probe host for CVE-2026-1234, " + string([]byte{0xc3, 0x28}))
	if result.Dialect != "" || len(result.Clauses) != 0 || len(result.Errors) != 1 || result.Errors[0].Code != "invalid_encoding" {
		t.Fatalf("invalid Unicode reached the dialect: %s", result.JSON(false))
	}
}
