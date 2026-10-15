// Import grouping and ordering.

package fmt

import (
	"sort"

	"zyx/internal/token"
)

// sortImports returns the token stream with every run of consecutive
// top-level import declarations in byte order, which is the order the
// language writes them in.
func sortImports(toks []token.Token) []token.Token {
	out := make([]token.Token, 0, len(toks))
	for i := 0; i < len(toks); {
		if toks[i].Kind != token.Import || depthAt(toks, i) != 0 {
			out = append(out, toks[i])
			i++
			continue
		}
		// Collect the run of imports and the statements that follow it.
		var run [][]token.Token
		j := i
		for j < len(toks) && toks[j].Kind == token.Import {
			start := j
			for j < len(toks) && !endsStatement(toks[j]) {
				j++
			}
			if j < len(toks) {
				j++
			}
			run = append(run, toks[start:j])
		}
		if len(run) < 2 {
			out = append(out, toks[i:j]...)
			i = j
			continue
		}
		sort.SliceStable(run, func(a, b int) bool {
			return statementText(run[a]) < statementText(run[b])
		})
		for _, stmt := range run {
			out = append(out, stmt...)
		}
		i = j
	}
	return out
}

// endsStatement reports whether a token closes a statement.
func endsStatement(t token.Token) bool {
	return t.Kind == token.Semicolon || t.Kind == token.Newline
}

// statementText returns the text of a statement, without its terminator.
func statementText(stmt []token.Token) string {
	out := ""
	for _, t := range stmt {
		if endsStatement(t) {
			break
		}
		out += t.Span.Text() + " "
	}
	return out
}

// depthAt returns the brace depth at a position in the token stream.
func depthAt(toks []token.Token, index int) int {
	depth := 0
	for i := 0; i < index; i++ {
		switch toks[i].Kind {
		case token.LBrace:
			depth++
		case token.RBrace:
			depth--
		}
	}
	return depth
}
