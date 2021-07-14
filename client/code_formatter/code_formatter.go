package code_formatter

import (
	"alda.io/client/parser"
	"fmt"
	"io"
)

func Format(tokens []parser.Token, w io.Writer) {
	for _, token := range tokens {
		fmt.Fprint(w, token.Text)
	}
}
