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
				Name       string            `json:"name"`
				Input      string            `json:"input"`
				Phrasebook map[string]string `json:"phrasebook"`
				Expect     json.RawMessage   `json:"expect"`
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
				result, err := ParseWith(c.Input, c.Phrasebook)
				if err != nil {
					t.Fatal(err)
				}
				if got := result.JSON(false); !bytes.Equal(got, want.Bytes()) {
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
	result, err := ParseWith("call it \xc3\x28", map[string]string{"Ab": "clone"})
	if err != nil || len(result.PhrasebookProblems) != 1 || result.PhrasebookProblems[0].Phrase != "Ab" {
		t.Errorf("phrasebook problems are not kept with invalid_encoding: %v %s", err, result.JSON(false))
	}
	for _, book := range []map[string]string{{"sp\xc3\x28": "clone"}, {"spin": "cl\xc3\x28"}} {
		if _, err := ParseWith("x", book); err != ErrInvalidPhrasebook {
			t.Errorf("a phrasebook that is not UTF-8 gave %v", err)
		}
	}
}
