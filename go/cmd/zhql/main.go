// Command zhql parses a ЖQL request and prints the result as JSON.
//
//	zhql "clone reddit.com, call it Savor"   parse the arguments as one request
//	zhql savor.zhql                          parse a query file
//	echo "call it Savor" | zhql              parse standard input
//
// --pretty indents the output. --phrasebook FILE reads a JSON object of phrases
// and their meanings. Exits 1 when the request has errors, 2 when the phrasebook
// file is unusable.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	zhql "github.com/Parallax-Web-Services/zhql/go"
)

func main() {
	pretty := false
	hasPhrasebook, phrasebookFile := false, ""
	var rest []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--pretty":
			pretty = true
		case args[i] == "--phrasebook":
			if i+1 == len(args) {
				fmt.Fprintln(os.Stderr, "zhql: --phrasebook needs a file.")
				os.Exit(2)
			}
			i++
			hasPhrasebook, phrasebookFile = true, args[i]
		default:
			rest = append(rest, args[i])
		}
	}

	var book map[string]string
	if hasPhrasebook {
		var ok bool
		if book, ok = readPhrasebook(phrasebookFile); !ok {
			fmt.Fprintf(os.Stderr, "zhql: %s is not a phrasebook (a JSON object of phrases and meanings).\n", phrasebookFile)
			os.Exit(2)
		}
	}

	var request []byte
	var err error
	source := ""
	if len(rest) == 1 && strings.HasSuffix(rest[0], ".zhql") && isFile(rest[0]) {
		source = rest[0]
		request, err = os.ReadFile(rest[0])
	} else if len(rest) == 0 || (len(rest) == 1 && rest[0] == "-") {
		source = "standard input"
		request, err = io.ReadAll(os.Stdin)
	} else {
		request = []byte(strings.Join(rest, " "))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "zhql: cannot read %s.\n", source)
		os.Exit(2)
	}

	result, err := zhql.ParseWith(string(request), book)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zhql: %s is not a phrasebook (a JSON object of phrases and meanings).\n", phrasebookFile)
		os.Exit(2)
	}
	os.Stdout.Write(append(result.JSON(pretty), '\n'))
	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}

// readPhrasebook loads a JSON object whose values are all strings.
func readPhrasebook(path string) (map[string]string, bool) {
	if !isFile(path) {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) || hasLoneSurrogate(data) {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, false
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, false
	}
	book := make(map[string]string, len(object))
	for phrase, means := range object {
		text, ok := means.(string)
		if !ok {
			return nil, false
		}
		book[phrase] = text
	}
	return book, true
}

// hasLoneSurrogate reports whether JSON text escapes half of a UTF-16 surrogate
// pair on its own, such as "\ud800". encoding/json quietly replaces those with
// U+FFFD, where the PHP, JavaScript and Python tools reject the file.
func hasLoneSurrogate(data []byte) bool {
	inString, high := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !inString {
			inString = c == '"'
			continue
		}
		if c != '\\' {
			if high {
				return true
			}
			inString = c != '"'
			continue
		}
		if i+1 < len(data) && data[i+1] != 'u' {
			if high {
				return true
			}
			i++
			continue
		}
		if i+5 >= len(data) {
			return false // malformed; json.Unmarshal rejects it
		}
		v, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 32)
		if err != nil {
			return false
		}
		i += 5
		switch {
		case v >= 0xd800 && v <= 0xdbff:
			if high {
				return true
			}
			high = true
		case v >= 0xdc00 && v <= 0xdfff:
			if !high {
				return true
			}
			high = false
		default:
			if high {
				return true
			}
		}
	}
	return high
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
