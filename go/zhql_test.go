package zhql

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConformance runs every case in ../conformance. The expected output is
// compared byte for byte, so key order and string escaping are checked too.
func TestConformance(t *testing.T) {
	files, err := filepath.Glob("../conformance/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no conformance files found: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var suite struct {
			Cases []struct {
				Name   string          `json:"name"`
				Input  string          `json:"input"`
				Expect json.RawMessage `json:"expect"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(data, &suite); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, c := range suite.Cases {
			name := filepath.Base(file) + ": " + c.Name
			t.Run(name, func(t *testing.T) {
				var want bytes.Buffer
				if err := json.Compact(&want, c.Expect); err != nil {
					t.Fatal(err)
				}
				if got := Parse(c.Input).JSON(false); !bytes.Equal(got, want.Bytes()) {
					t.Errorf("input %q\n got: %s\nwant: %s", c.Input, got, want.Bytes())
				}
			})
		}
	}
}

// Not expressible in JSON fixtures: bytes that are not UTF-8.
func TestInvalidUTF8(t *testing.T) {
	result := Parse("call it \xc3\x28")
	if len(result.Clauses) != 0 || len(result.Errors) != 1 || result.Errors[0].Code != "invalid_encoding" {
		t.Errorf("got %s", result.JSON(false))
	}
}
