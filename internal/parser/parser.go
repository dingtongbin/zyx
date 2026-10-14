// Package parser builds the syntax tree from the token stream of one source
// file. It is the second stage of the front end, after the lexer.
package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/lexer"
	"zyx/internal/source"
	"zyx/internal/token"
)

// parser holds the scan position, pending doc comments, and diagnostics for
// one file.
type parser struct {
	file  *source.File
	toks  []token.Token
	pos   int
	tok   token.Token
	last  source.Span
	docs  []token.Token
	diags []diag.Diagnostic

	// headerDepth counts the enclosing if, for, match, and select headers.
	// While it is positive a `Name {` sequence opens a body block rather
	// than an aggregate literal, which is how those headers stay
	// unambiguous.
	headerDepth int
	// gtCount counts closing angle brackets owed to a `>>` token that closed
	// two nested type argument lists at once.
	gtCount int
}

// Parse lexes and parses one source file. It returns a nil tree when the
// file is not valid UTF-8, because no token stream exists in that case.
func Parse(file *source.File) (*ast.File, []diag.Diagnostic) {
	toks, diags := lexer.Lex(file)
	if len(toks) == 0 {
		return nil, diags
	}
	p := &parser{file: file, toks: toks}
	p.skipComments()
	p.tok = p.toks[p.pos]
	f := p.parseFile()
	p.reportPendingDocs()
	return f, append(diags, p.diags...)
}

// advance moves to the next significant token, skipping comments and
// recording doc comments for the next declaration.
func (p *parser) advance() {
	if p.pos < len(p.toks)-1 {
		p.last = p.toks[p.pos].Span
		p.pos++
	}
	p.skipComments()
	p.tok = p.toks[p.pos]
}

// skipComments steps over comment tokens at the scan position.
func (p *parser) skipComments() {
	for p.pos < len(p.toks) && p.toks[p.pos].Kind == token.Comment {
		if p.toks[p.pos].Doc {
			p.docs = append(p.docs, p.toks[p.pos])
		}
		p.pos++
	}
}

// peek returns the n-th significant token after the current one; peek(1) is
// the next token.
func (p *parser) peek(n int) token.Token {
	i := p.pos
	for {
		if i >= len(p.toks)-1 {
			return p.toks[len(p.toks)-1]
		}
		i++
		if p.toks[i].Kind == token.Comment {
			continue
		}
		n--
		if n <= 0 {
			return p.toks[i]
		}
	}
}

// at reports whether the current token has kind k.
func (p *parser) at(k token.Kind) bool { return p.tok.Kind == k }

// accept consumes the current token when it has kind k.
func (p *parser) accept(k token.Kind) bool {
	if p.tok.Kind != k {
		return false
	}
	p.advance()
	return true
}

// skipNewlines consumes statement terminators.
func (p *parser) skipNewlines() {
	for p.tok.Kind == token.Newline {
		p.advance()
	}
}

// spanFrom returns the span from start to the end of the last consumed
// token.
func (p *parser) spanFrom(start source.Span) source.Span {
	return source.Span{File: start.File, Start: start.Start, End: p.last.End}
}

// parseFile parses the fixed file structure: package, imports, then the
// remaining top-level declarations.
func (p *parser) parseFile() *ast.File {
	f := &ast.File{Sp: source.Span{File: p.file, Start: 0, End: p.file.Len()}}
	p.skipNewlines()
	f.Package = p.parsePackage()
	p.skipNewlines()
	for p.tok.Kind == token.Import {
		f.Imports = append(f.Imports, p.parseImport())
		p.skipNewlines()
	}
	for p.tok.Kind != token.EOF {
		if p.tok.Kind == token.Newline {
			p.advance()
			continue
		}
		before := p.pos
		if d := p.parseDecl(); d != nil {
			f.Decls = append(f.Decls, d)
		}
		if p.pos == before {
			p.advance()
		}
		p.skipNewlines()
	}
	return f
}

// reportPendingDocs flags doc comments that never attached to a declaration.
func (p *parser) reportPendingDocs() {
	for _, doc := range p.docs {
		p.errorHelpAt(doc, diag.CodeDocComment,
			"attach the doc comment to the declaration that follows it",
			"doc comment is not attached to a declaration")
	}
	p.docs = nil
}

// takeDoc returns the doc comment sitting on the lines directly above the
// current token, or nil. A comment that does not attach stays pending and is
// reported when the parse ends.
func (p *parser) takeDoc() *ast.Doc {
	if len(p.docs) == 0 {
		return nil
	}
	line, _ := p.tok.Span.StartPos()
	n := 0
	for n < len(p.docs) {
		got, _ := p.docs[len(p.docs)-1-n].Span.StartPos()
		if got != line-1-n {
			break
		}
		n++
	}
	if n == 0 {
		return nil
	}
	used := p.docs[len(p.docs)-n:]
	p.docs = p.docs[:len(p.docs)-n]
	lines := make([]string, n)
	for i, c := range used {
		lines[i] = docText(c.Text())
	}
	sp := source.Span{
		File:  used[0].Span.File,
		Start: used[0].Span.Start,
		End:   used[n-1].Span.End,
	}
	return &ast.Doc{Sp: sp, Lines: lines}
}

// docText strips the leading slashes and one space from a doc comment line.
func docText(text string) string {
	i := 0
	for i < len(text) && text[i] == '/' {
		i++
	}
	if i < len(text) && text[i] == ' ' {
		i++
	}
	return text[i:]
}
