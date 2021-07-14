package code_formatter

import (
	"alda.io/client/parser"
	"fmt"
	"io"
)

// Format formats Alda token text into readable code
// Format can be used as the last step in the decompiler process, or by itself
// as a standalone formatting command 
func Format(tokens []parser.Token, w io.Writer) {
	for _, token := range tokens {
		fmt.Fprint(w, token.Text)
	}
}
