// Statements, blocks, and the control-flow forms.

package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/token"
)

// parseBlock parses a brace-delimited statement list.
func (p *parser) parseBlock() *ast.BlockStmt {
	start := p.tok.Span
	b := &ast.BlockStmt{Sp: start}
	p.expect(token.LBrace)
	p.skipNewlines()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		before := p.pos
		if s := p.parseStmt(); s != nil {
			b.Stmts = append(b.Stmts, s)
		}
		if p.pos == before {
			p.advance()
		}
		p.skipNewlines()
	}
	p.expect(token.RBrace)
	b.Sp = p.spanFrom(start)
	return b
}

// parseBody parses a `{ ... }` block or a single-line `: statement` body.
func (p *parser) parseBody() ast.Stmt {
	if p.at(token.LBrace) {
		return p.parseBlock()
	}
	if p.accept(token.Colon) {
		return p.parseSingleLineStmt()
	}
	p.errorHelpAt(p.tok, diag.CodeExpectedToken,
		"write a `{ ... }` block or a `: statement` body",
		"expected a body, found %s", describe(p.tok))
	return &ast.BlockStmt{Sp: p.tok.Span}
}

// parseSingleLineStmt parses the statement of a single-line body. A
// declaration is not allowed there.
func (p *parser) parseSingleLineStmt() ast.Stmt {
	s := p.parseStmt()
	switch s.(type) {
	case *ast.VarDecl, *ast.ConstDecl:
		p.errorHelpAt(p.tok, diag.CodeDeclarationInSingleLineBody,
			"wrap the declaration in a `{ ... }` block",
			"a single-line body cannot declare a variable or constant")
	}
	return s
}

// parseStmt parses one statement.
func (p *parser) parseStmt() ast.Stmt {
	switch p.tok.Kind {
	case token.Var:
		return p.parseVar()
	case token.Const:
		return p.parseConst()
	case token.If:
		return p.parseIf()
	case token.For:
		return p.parseFor(nil)
	case token.Break:
		return p.parseBreak()
	case token.Continue:
		return p.parseContinue()
	case token.Return:
		return p.parseReturn()
	case token.Defer:
		return p.parseDefer()
	case token.Region:
		return p.parseRegion()
	case token.Comptime:
		if p.peek(1).Kind == token.LBrace {
			return p.parseComptimeBlock()
		}
		p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
			"write `comptime { ... }`",
			"`comptime func` is a declaration and cannot stand inside a block")
		p.advance()
		return nil
	case token.Match:
		return p.parseMatch()
	case token.Select:
		return p.parseSelect()
	case token.Ident:
		if p.peek(1).Kind == token.Colon && p.peek(2).Kind == token.For {
			label := p.name()
			p.advance() // :
			return p.parseFor(label)
		}
	}
	return p.parseSimpleStmt()
}

// parseSimpleStmt parses an assignment or an expression statement.
func (p *parser) parseSimpleStmt() ast.Stmt {
	start := p.tok.Span
	x := p.parseExpr()
	if isAssignOp(p.tok.Kind) {
		op := p.tok.Kind
		p.advance()
		value := p.parseExpr()
		if !isAssignTarget(x) {
			p.errorHelpSpan(x.Span(), diag.CodeInvalidAssignTarget,
				"assign to a variable, a field, or an index",
				"invalid assignment target")
		}
		return &ast.AssignStmt{Sp: p.spanFrom(start), Target: x, Op: op, Value: value}
	}
	if !isExprStatement(x) {
		p.errorHelpSpan(x.Span(), diag.CodeInvalidStatement,
			"call the expression, assign its result, or remove the line",
			"expression cannot stand as a statement")
	}
	return &ast.ExprStmt{Sp: p.spanFrom(start), X: x}
}

// parseIf parses `if cond Body (else (if ... | Body))?`.
func (p *parser) parseIf() *ast.IfStmt {
	start := p.tok.Span
	s := &ast.IfStmt{Sp: start}
	p.advance() // if
	p.enterHeader()
	s.Cond = p.parseExpr()
	p.leaveHeader()
	s.Then = p.parseBody()
	if p.at(token.Newline) && p.peek(1).Kind == token.Else {
		p.advance()
	}
	if p.accept(token.Else) {
		if p.at(token.If) {
			s.Else = p.parseIf()
		} else {
			s.Else = p.parseBody()
		}
	}
	s.Sp = p.spanFrom(start)
	return s
}

// parseFor parses one of the three loop forms, with an optional label.
func (p *parser) parseFor(label *ast.Ident) *ast.ForStmt {
	start := p.tok.Span
	if label != nil {
		start = label.Sp
	}
	s := &ast.ForStmt{Sp: start, Label: label}
	p.advance() // for
	if p.at(token.LBrace) || p.at(token.Colon) {
		s.Body = p.parseBody()
		s.Sp = p.spanFrom(start)
		return s
	}
	p.enterHeader()
	if p.forIsIterator() {
		s.Bindings = append(s.Bindings, p.binding())
		for p.accept(token.Comma) {
			s.Bindings = append(s.Bindings, p.binding())
		}
		p.expect(token.In)
		s.Iter = p.parseExpr()
	} else {
		s.Iter = p.parseExpr()
	}
	p.leaveHeader()
	s.Body = p.parseBody()
	s.Sp = p.spanFrom(start)
	return s
}

// forIsIterator reports whether the loop head is a binding list followed by
// `in`, as opposed to a condition expression.
func (p *parser) forIsIterator() bool {
	if !p.at(token.Ident) && !p.at(token.Underscore) {
		return false
	}
	for n := 1; ; {
		switch p.peek(n).Kind {
		case token.In:
			return true
		case token.Comma:
			n++
			if k := p.peek(n).Kind; k != token.Ident && k != token.Underscore {
				return false
			}
			n++
		default:
			return false
		}
	}
}

// parseBreak parses `break` with an optional label.
func (p *parser) parseBreak() *ast.BreakStmt {
	start := p.tok.Span
	s := &ast.BreakStmt{Sp: start}
	p.advance()
	if p.at(token.Ident) {
		s.Label = p.name()
	}
	s.Sp = p.spanFrom(start)
	return s
}

// parseContinue parses `continue` with an optional label.
func (p *parser) parseContinue() *ast.ContinueStmt {
	start := p.tok.Span
	s := &ast.ContinueStmt{Sp: start}
	p.advance()
	if p.at(token.Ident) {
		s.Label = p.name()
	}
	s.Sp = p.spanFrom(start)
	return s
}

// parseReturn parses `return` with an optional value list.
func (p *parser) parseReturn() *ast.ReturnStmt {
	start := p.tok.Span
	s := &ast.ReturnStmt{Sp: start}
	p.advance()
	switch p.tok.Kind {
	case token.Newline, token.RBrace, token.EOF, token.Colon:
	default:
		s.Values = append(s.Values, p.parseExpr())
		for p.accept(token.Comma) {
			s.Values = append(s.Values, p.parseExpr())
		}
	}
	s.Sp = p.spanFrom(start)
	return s
}

// parseDefer parses `defer call`.
func (p *parser) parseDefer() *ast.DeferStmt {
	start := p.tok.Span
	s := &ast.DeferStmt{Sp: start}
	p.advance()
	s.Call = p.parseExpr()
	if !isCallExpr(s.Call) {
		p.errorHelpSpan(s.Call.Span(), diag.CodeUnexpectedToken,
			"defer a call", "defer applies to a call")
	}
	s.Sp = p.spanFrom(start)
	return s
}

// parseRegion parses `region { ... }`.
func (p *parser) parseRegion() *ast.RegionStmt {
	start := p.tok.Span
	s := &ast.RegionStmt{Sp: start}
	p.advance()
	s.Body = p.parseBlock()
	s.Sp = p.spanFrom(start)
	return s
}

// parseComptimeBlock parses `comptime { ... }`.
func (p *parser) parseComptimeBlock() *ast.ComptimeBlockStmt {
	start := p.tok.Span
	s := &ast.ComptimeBlockStmt{Sp: start}
	p.advance()
	s.Body = p.parseBlock()
	s.Sp = p.spanFrom(start)
	return s
}
