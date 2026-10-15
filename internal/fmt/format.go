// Package fmt rewrites sources into canonical form. The formatter works on
// the token stream, so a file that does not lex is returned unchanged: a
// formatter never damages a file it cannot read.
package fmt

import (
	"strings"

	"zyx/internal/lexer"
	"zyx/internal/source"
	"zyx/internal/token"
)

// Source returns the canonical text of one file.
func Source(file *source.File) []byte {
	toks, diags := lexer.Lex(file)
	if len(diags) != 0 {
		return append([]byte(nil), file.Data()...)
	}
	p := &printer{}
	p.run(sortImports(toks))
	return []byte(p.buf.String())
}

// A printer accumulates the canonical text.
type printer struct {
	buf strings.Builder
	// indent is the current indentation depth in two-space steps.
	indent int
	// lineStart marks a line that holds nothing yet.
	lineStart bool
	// prev is the last token written.
	prev *token.Token
	// prevText is the text of the last token written.
	prevText string
	// angles counts the open type-argument brackets, inside which `<`, `>`,
	// and `,` attach to their neighbours.
	angles int
	// brackets counts the open parentheses and square brackets, inside
	// which a line break is not a statement separator.
	brackets int
	// braces records, for every open brace, whether it opens a statement
	// block rather than an aggregate literal. A line break inside a
	// literal separates nothing, and a line break inside a block separates
	// statements.
	braces []bool
	// line is the source line the last written token started on.
	line int
	// emptyBody marks an opening brace whose body holds nothing, which the
	// format keeps on one line.
	emptyBody bool
	// lastDecl is the last top-level declaration keyword written, which
	// decides where a blank line separates declarations.
	lastDecl token.Kind
}

// run writes the whole token stream.
func (p *printer) run(toks []token.Token) {
	p.lineStart = true
	for i := range toks {
		p.token(toks, i)
	}
	p.newline()
}

// token writes one token.
func (p *printer) token(toks []token.Token, i int) {
	t := toks[i]
	text := t.Span.Text()
	line, _ := t.Span.StartPos()

	if t.Kind == token.EOF || t.Kind == token.Invalid {
		return
	}
	if t.Kind == token.Newline {
		// A line break right after an opening brace adds nothing: the brace
		// already ended its line, or opened an empty body.
		if !p.emptyBody && p.prevKind() != token.LBrace && p.separates() {
			p.newline()
		}
		p.prev = &toks[i]
		p.prevText = text
		p.line = line
		return
	}
	if t.Kind == token.Comment {
		if !p.lineStart {
			p.space()
		}
		p.write(text)
		p.newline()
		p.prev = &toks[i]
		p.prevText = text
		p.line = line
		return
	}

	// A blank line in the source stays a blank line, and a top-level
	// declaration is separated from the one before it by one blank line.
	if p.separates() && p.indent == 0 {
		switch {
		case p.prev == nil:
		case p.line > 0 && line > p.line+1:
			p.blankLine()
		case isDeclKeyword(t.Kind) && p.lineStart && p.lastDecl != token.Invalid &&
			!(p.lastDecl == token.Import && t.Kind == token.Import):
			p.blankLine()
		}
	}
	if t.Kind == token.RBrace && p.inBlock() && !p.emptyBody {
		p.newline()
		if p.indent > 0 {
			p.indent--
		}
	} else if p.needsSpace(toks, i, text) {
		p.space()
	}
	p.write(text)

	switch t.Kind {
	case token.LParen, token.LBrack:
		p.brackets++
	case token.RParen, token.RBrack:
		if p.brackets > 0 {
			p.brackets--
		}
	case token.LBrace:
		block := p.opensBlock(toks, i)
		p.braces = append(p.braces, block)
		if !block {
			break
		}
		if nextSignificant(toks, i) == token.RBrace {
			// An empty body is written on one line.
			p.emptyBody = true
			p.space()
			break
		}
		p.indent++
		p.newline()
	case token.RBrace:
		block := p.inBlock()
		if n := len(p.braces); n > 0 {
			p.braces = p.braces[:n-1]
		}
		if !block {
			break
		}
		if p.emptyBody {
			p.emptyBody = false
			break
		}
		if i+1 < len(toks) && toks[i+1].Kind == token.Else {
			p.space()
			break
		}
		p.newline()
	case token.Semicolon:
		p.newline()
	case token.Lt:
		if p.angles > 0 || opensAType(p.prevKind(), p.prevText) {
			p.angles++
		}
	case token.Gt:
		if p.angles > 0 {
			p.angles--
		}
	}

	p.prev = &toks[i]
	p.prevText = text
	p.line = line
	if p.indent == 0 && isDeclKeyword(t.Kind) {
		p.lastDecl = t.Kind
	}
}

// isDeclKeyword reports whether a token opens a top-level declaration.
func isDeclKeyword(k token.Kind) bool {
	switch k {
	case token.Package, token.Import, token.Const, token.Var, token.Type,
		token.Func, token.Struct, token.Enum, token.Interface, token.Extern:
		return true
	}
	return false
}

// inBlock reports whether the innermost open brace is a statement block.
func (p *printer) inBlock() bool {
	n := len(p.braces)
	return n > 0 && p.braces[n-1]
}

// nextSignificant returns the kind of the next token that carries text,
// skipping line breaks and comments.
func nextSignificant(toks []token.Token, i int) token.Kind {
	for j := i + 1; j < len(toks); j++ {
		switch toks[j].Kind {
		case token.Newline, token.Comment, token.EOF, token.Invalid:
			continue
		}
		return toks[j].Kind
	}
	return token.Invalid
}

// separates reports whether a line break at this point separates two
// statements rather than two halves of one expression.
func (p *printer) separates() bool {
	if p.brackets > 0 {
		return false
	}
	for _, block := range p.braces {
		if !block {
			return false
		}
	}
	return true
}

// opensBlock reports whether a brace opens a statement block rather than an
// aggregate literal. A literal follows a type name; a declaration keyword
// before that name makes the brace a body.
func (p *printer) opensBlock(toks []token.Token, i int) bool {
	if i == 0 {
		return true
	}
	prev := toks[i-1]
	if !opensAType(prev.Kind, prev.Span.Text()) {
		return true
	}
	if i >= 2 {
		switch toks[i-2].Kind {
		case token.Struct, token.Enum, token.Interface, token.Impl, token.Extern, token.Union:
			return true
		}
	}
	return false
}

// needsSpace reports whether a space separates a token from the one before.
func (p *printer) needsSpace(toks []token.Token, i int, text string) bool {
	if p.lineStart || p.prev == nil {
		return false
	}
	kind := toks[i].Kind
	if noSpaceBefore(kind) {
		return false
	}
	if p.angles > 0 && (kind == token.Lt || kind == token.Gt || kind == token.Comma) {
		return false
	}
	if p.angles > 0 && p.prev.Kind == token.Lt {
		// The first argument of a type argument list attaches to its
		// opening bracket.
		return false
	}
	if noSpaceAfter(p.prev.Kind) {
		return false
	}
	switch kind {
	case token.LParen:
		// A call or a function type attaches to its name.
		switch p.prev.Kind {
		case token.Ident, token.RParen, token.RBrack, token.Func, token.SelfUpper:
			return false
		}
	case token.Lt:
		if opensAType(p.prevKind(), p.prevText) {
			return false
		}
	case token.Minus:
		if opensExpression(p.prev.Kind) {
			return false
		}
	}
	_ = text
	return true
}

// prevKind returns the kind of the last token written.
func (p *printer) prevKind() token.Kind {
	if p.prev == nil {
		return token.Invalid
	}
	return p.prev.Kind
}

// write appends text, starting a line at the current indentation.
func (p *printer) write(text string) {
	if p.lineStart {
		for i := 0; i < p.indent; i++ {
			p.buf.WriteString(indentUnit)
		}
		p.lineStart = false
	}
	p.buf.WriteString(text)
}

// indentUnit is the indentation step the format prescribes.
const indentUnit = "    "

// space writes one space unless the line holds nothing yet.
func (p *printer) space() {
	if p.lineStart {
		return
	}
	p.buf.WriteByte(' ')
}

// newline ends the current line.
func (p *printer) newline() {
	if p.lineStart {
		return
	}
	p.buf.WriteByte('\n')
	p.lineStart = true
}

// blankLine writes one empty line, which separates top-level declarations.
func (p *printer) blankLine() {
	if p.buf.Len() == 0 {
		return
	}
	p.buf.WriteByte('\n')
	p.lineStart = true
}
