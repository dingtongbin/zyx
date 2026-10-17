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
	// indent is the current indentation depth in four-space steps.
	indent int
	// lineStart marks a line that holds nothing yet.
	lineStart bool
	// prev is the last token written.
	prev *token.Token
	// prevText is the text of the last token written.
	prevText string
	// prevContent is the kind of the last token written that was not a line
	// break, comments included. It tells whether a comment run sits directly
	// above a declaration and whether a brace has just opened.
	prevContent token.Kind
	// angles counts the open type-argument brackets, inside which `<`, `>`,
	// and `,` attach to their neighbours.
	angles int
	// closedArgs marks a `>` that closed a type argument list, which the
	// call parenthesis attaches to. A `>` that compares does not.
	closedArgs bool
	// brackets counts the open parentheses and square brackets, inside
	// which a line break is not a statement separator.
	brackets int
	// braces records, for every open brace, whether it opens a statement
	// block rather than an aggregate literal. A line break inside a
	// literal separates nothing, and a line break inside a block separates
	// statements.
	braces []bool
	// indents records, for every open brace, whether it raised the
	// indentation. A brace that holds something indents its members, and the
	// closing brace returns to the level that opened it.
	indents []bool
	// line is the source line the last written token ended on.
	line int
	// blank marks output that already ends with a blank line, so a second
	// one is never written.
	blank bool
	// emptyBody marks an opening brace whose body holds nothing, which the
	// format keeps on one line.
	emptyBody bool
	// lastDecl is the last top-level declaration keyword written, which
	// decides where a blank line separates declarations.
	lastDecl token.Kind
	// attach marks that the last token was a unary operator, so the token
	// after it attaches to its operand.
	attach bool
	// pending marks a line deferNewline left open, which a comment on the
	// same line is written into before it ends.
	pending bool
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
		p.writeComment(toks, i, text, line)
		return
	}
	p.flushPending()
	p.breakTo(line)

	// The source's blank lines survive, one at most, and never at the start
	// or the end of a brace. A top-level declaration is separated from the
	// one before it by a blank line; a comment run directly above it belongs
	// to it, so no blank line goes between the run and the declaration.
	p.blankIfSource(line, t.Kind == token.RBrace)
	if p.separates() && p.indent == 0 && p.lineStart &&
		isDeclKeyword(t.Kind) && p.lastDecl != token.Invalid &&
		p.prevContent != token.Comment && !sameRun(p.lastDecl, t.Kind) {
		p.blankLine()
	}
	if t.Kind == token.RBrace && p.indented() {
		// The closing brace returns to the level that opened its body.
		if p.indent > 0 {
			p.indent--
		}
	}
	if p.needsSpace(toks, i) {
		p.space()
	}
	p.write(text)

	closes := false
	switch t.Kind {
	case token.LParen, token.LBrack:
		p.brackets++
	case token.RParen, token.RBrack:
		if p.brackets > 0 {
			p.brackets--
		}
	case token.LBrace:
		p.pushBrace(toks, i)
	case token.RBrace:
		p.popBrace(toks, i)
	case token.Semicolon:
		// A semicolon between statements ends a line; one inside brackets
		// separates the parts of a type or a literal, which stay together.
		if p.separates() {
			p.deferNewline()
		}
	case token.Lt:
		if p.angles > 0 || opensAType(p.prevKind(), p.prevText) {
			p.angles++
		}
	case token.Gt:
		if p.angles > 0 {
			p.angles--
			p.closedArgs = p.angles == 0
			closes = p.closedArgs
		}
	case token.Shr:
		// A `>>` closes two type argument lists when two are open, and is a
		// shift otherwise.
		if p.angles >= 2 {
			p.angles -= 2
			p.closedArgs = p.angles == 0
			closes = p.closedArgs
		}
	}

	if !closes {
		// The mark lasts for the token that follows the closing bracket.
		p.closedArgs = false
	}
	p.attach = t.Kind == token.Minus && opensExpression(p.prevKind())
	p.prev = &toks[i]
	p.prevText = text
	p.line = line
	p.prevContent = t.Kind
	if p.indent == 0 && isDeclKeyword(t.Kind) {
		p.lastDecl = t.Kind
	}
}

// writeComment writes one comment where the program wrote it. A line comment
// ends the line it stands on; a block comment does not, so the line goes on
// past its closing marker.
func (p *printer) writeComment(toks []token.Token, i int, text string, line int) {
	t := toks[i]
	if p.pending && line == p.line {
		// The comment stands on the line the printer has not ended yet,
		// which is where it was written.
		p.pending = false
	} else {
		p.flushPending()
		p.breakTo(line)
	}
	p.blankIfSource(line, false)
	// A comment at the top level opens the run that belongs to the
	// declaration below it, so the blank line goes before the run, and only
	// when the comment starts a line of its own.
	if p.lineStart && p.separates() && p.indent == 0 && p.lastDecl != token.Invalid &&
		p.prevContent != token.Comment {
		p.blankLine()
	}
	if !p.lineStart {
		p.space()
	}
	p.write(text)
	if !t.Block {
		p.newline()
	}
	p.prev = &toks[i]
	p.prevText = text
	p.line = commentEnd(t, line)
	p.prevContent = token.Comment
}

// commentEnd returns the last line a comment covers. A block comment spans
// several lines, so a blank line after it is measured from where it ends.
func commentEnd(t token.Token, start int) int {
	if !t.Block || t.Span.File == nil {
		return start
	}
	end, _ := t.Span.File.LineCol(t.Span.End)
	return end
}

// pushBrace records an opening brace. A body that holds something raises the
// indentation, and a statement block ends its line there.
func (p *printer) pushBrace(toks []token.Token, i int) {
	block := p.opensBlock(toks, i)
	p.braces = append(p.braces, block)
	if nextSignificant(toks, i) == token.RBrace {
		// An empty body is written on one line.
		p.indents = append(p.indents, false)
		if block {
			p.emptyBody = true
			p.space()
		}
		return
	}
	p.indents = append(p.indents, true)
	p.indent++
	if block {
		p.deferNewline()
	}
}

// popBrace leaves an opening brace and resets the marks it set. The line is
// ended after the brace, unless an `else` the format keeps beside it follows.
func (p *printer) popBrace(toks []token.Token, i int) {
	block := p.inBlock()
	if n := len(p.braces); n > 0 {
		p.braces = p.braces[:n-1]
	}
	if n := len(p.indents); n > 0 {
		p.indents = p.indents[:n-1]
	}
	if !block {
		return
	}
	if p.emptyBody {
		p.emptyBody = false
		return
	}
	if i+1 < len(toks) && toks[i+1].Kind == token.Else {
		return
	}
	p.deferNewline()
}

// isDeclKeyword reports whether a token opens a top-level declaration.
func isDeclKeyword(k token.Kind) bool {
	switch k {
	case token.Package, token.Import, token.Const, token.Var, token.Type,
		token.Func, token.Struct, token.Enum, token.Interface, token.Extern,
		token.Fast:
		return true
	}
	return false
}

// sameRun reports whether two declaration keywords open a run of declarations
// the format keeps together without a blank line between them: consecutive
// imports, and consecutive constants, variables, or type aliases. A blank line
// the source wrote inside a run is still kept.
func sameRun(prev, next token.Kind) bool {
	if prev != next {
		return false
	}
	switch next {
	case token.Import, token.Const, token.Var, token.Type:
		return true
	}
	return false
}

// inBlock reports whether the innermost open brace is a statement block.
func (p *printer) inBlock() bool {
	n := len(p.braces)
	return n > 0 && p.braces[n-1]
}

// indented reports whether the innermost open brace raised the indentation.
func (p *printer) indented() bool {
	n := len(p.indents)
	return n > 0 && p.indents[n-1]
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
// aggregate literal. A literal follows the name of a type; a token that can
// only stand before a body makes the brace a body even when the token before it
// is a type name, which is how a result type of a user-declared type is told
// from a construction.
func (p *printer) opensBlock(toks []token.Token, i int) bool {
	if i == 0 {
		return true
	}
	j := i - 1
	if toks[j].Kind == token.Gt {
		// The brace follows the closing bracket of a type argument list, so
		// the type name stands before the list. Step back over it.
		depth := 0
		for ; j >= 0; j-- {
			switch toks[j].Kind {
			case token.Gt:
				depth++
			case token.Lt:
				depth--
			}
			if depth == 0 {
				break
			}
		}
		j--
	}
	if j < 0 || !opensAType(toks[j].Kind, toks[j].Span.Text()) {
		return true
	}
	if j >= 1 {
		switch toks[j-1].Kind {
		case token.Struct, token.Enum, token.Interface, token.Impl, token.Extern, token.Union,
			token.Arrow, token.Is, token.For:
			return true
		}
	}
	return false
}

// needsSpace reports whether a space separates a token from the one before.
func (p *printer) needsSpace(toks []token.Token, i int) bool {
	if p.lineStart || p.prev == nil {
		return false
	}
	if p.attach && toks[i].Kind != token.Minus {
		// The token is the operand of a unary operator, which attaches to
		// it. Two minus signs keep their space so they read as two.
		return false
	}
	kind := toks[i].Kind
	if kind == token.RBrace && !p.inBlock() && p.prevKind() != token.LBrace {
		// A literal with members closes with a space before its brace.
		return true
	}
	if noSpaceBefore(kind) {
		return false
	}
	if p.angles > 0 && (kind == token.Lt || kind == token.Gt || kind == token.Comma ||
		(kind == token.Shr && p.angles >= 2)) {
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
		// A call or a function type attaches to its name, and a call on a
		// type argument list attaches to the list it closes.
		switch p.prev.Kind {
		case token.Ident, token.RParen, token.RBrack, token.Func, token.SelfUpper:
			return false
		case token.Gt, token.Shr:
			if p.closedArgs {
				return false
			}
		}
	case token.Lt:
		if opensAType(p.prevKind(), p.prevText) {
			return false
		}
	case token.LBrack:
		// An index attaches to the expression it indexes; a type or a
		// literal written after anything else keeps its space.
		switch p.prev.Kind {
		case token.Ident, token.RParen, token.RBrack, token.SelfUpper:
			return false
		}
	}
	return true
}

// prevKind returns the kind of the last token written.
func (p *printer) prevKind() token.Kind {
	if p.prev == nil {
		return token.Invalid
	}
	return p.prev.Kind
}

// breakTo ends the current line when a token stands on a later source line
// than the last token written. The lexer folds a line break into a preceding
// continuation token such as `>` or `,`, so the line structure is read from
// the token positions rather than from a line-break token.
func (p *printer) breakTo(line int) {
	if p.emptyBody || p.lineStart || line <= p.line {
		return
	}
	p.newline()
}

// blankIfSource writes at most one blank line for the break the source held
// before this token. A blank line is never written after an opening brace or
// before a closing one.
func (p *printer) blankIfSource(line int, closing bool) {
	if !p.lineStart || p.buf.Len() == 0 || p.line == 0 {
		return
	}
	if line <= p.line+1 || closing || p.prevContent == token.LBrace {
		return
	}
	p.blankLine()
}

// write appends text, starting a line at the current indentation.
func (p *printer) write(text string) {
	if p.lineStart {
		for i := 0; i < p.indent; i++ {
			p.buf.WriteString(indentUnit)
		}
		p.lineStart = false
	}
	p.blank = false
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
	p.pending = false
	p.blank = false
}

// deferNewline ends the current line, unless a comment the program wrote on
// the same line follows: the format keeps a trailing comment where it was
// written, so the line is ended after that comment rather than before it.
func (p *printer) deferNewline() {
	if p.lineStart {
		return
	}
	p.pending = true
}

// flushPending ends a line that deferNewline left open.
func (p *printer) flushPending() {
	if !p.pending {
		return
	}
	p.pending = false
	p.buf.WriteByte('\n')
	p.lineStart = true
	p.blank = false
}

// blankLine writes one empty line. It separates top-level declarations and
// keeps the blank lines the source held, and a second one in a row is never
// written.
func (p *printer) blankLine() {
	if p.buf.Len() == 0 || p.blank {
		return
	}
	p.buf.WriteByte('\n')
	p.lineStart = true
	p.blank = true
}
