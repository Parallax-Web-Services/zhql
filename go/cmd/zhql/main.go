// Command zhql parses a ЖQL request and prints the result as JSON.
//
//	zhql "clone reddit.com, call it Savor"   parse the arguments as one request
//	zhql savor.zhql                          parse a query file
//	echo "call it Savor" | zhql              parse standard input
//
// --pretty indents the output. Exits 1 when the result has errors.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	zhql "github.com/Parallax-Web-Services/zhql/go"
)

func main() {
	pretty := false
	var rest []string
	for _, arg := range os.Args[1:] {
		if arg == "--pretty" {
			pretty = true
		} else {
			rest = append(rest, arg)
		}
	}

	var request []byte
	var err error
	if len(rest) == 1 && strings.HasSuffix(rest[0], ".zhql") && isFile(rest[0]) {
		request, err = os.ReadFile(rest[0])
	} else if len(rest) == 0 || (len(rest) == 1 && rest[0] == "-") {
		request, err = io.ReadAll(os.Stdin)
	} else {
		request = []byte(strings.Join(rest, " "))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "zhql:", err)
		os.Exit(2)
	}

	result := zhql.Parse(string(request))
	os.Stdout.Write(append(result.JSON(pretty), '\n'))
	if len(result.Errors) > 0 {
		os.Exit(1)
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
