// match and select statements.

package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/token"
)

// parseMatch parses `match subject { arm... }`.
func (p *parser) parseMatch() *ast.MatchStmt {
	start := p.tok.Span
	s := &ast.MatchStmt{Sp: start}
	p.advance() // match
	p.enterHeader()
	s.Subject = p.parseExpr()
	p.leaveHeader()
	p.expect(token.LBrace)
	p.skipNewlines()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		before := p.pos
		if arm := p.parseMatchArm(); arm != nil {
			s.Arms = append(s.Arms, arm)
		}
		if p.pos == before {
			p.advance()
		}
		p.skipNewlines()
	}
	p.expect(token.RBrace)
	s.Sp = p.spanFrom(start)
	return s
}

// parseMatchArm parses `pattern (if guard)? => (block | statement)`.
func (p *parser) parseMatchArm() *ast.MatchArm {
	start := p.tok.Span
	arm := &ast.MatchArm{Sp: start}
	arm.Pattern = p.parsePattern()
	if p.accept(token.If) {
		arm.Guard = p.parseExpr()
	}
	if !p.expect(token.FatArrow) {
		p.sync()
		arm.Sp = p.spanFrom(start)
		return arm
	}
	if p.at(token.LBrace) {
		arm.Body = p.parseBlock()
	} else {
		arm.Body = p.parseSingleLineStmt()
	}
	arm.Sp = p.spanFrom(start)
	return arm
}

// parseSelect parses `select { arm... }`.
func (p *parser) parseSelect() *ast.SelectStmt {
	start := p.tok.Span
	s := &ast.SelectStmt{Sp: start}
	p.advance() // select
	p.expect(token.LBrace)
	p.skipNewlines()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		before := p.pos
		if arm := p.parseSelectArm(); arm != nil {
			s.Arms = append(s.Arms, arm)
		}
		if p.pos == before {
			p.advance()
		}
		p.skipNewlines()
	}
	p.expect(token.RBrace)
	s.Sp = p.spanFrom(start)
	return s
}

// parseSelectArm parses one of the four select arm forms.
func (p *parser) parseSelectArm() *ast.SelectArm {
	start := p.tok.Span
	arm := &ast.SelectArm{Sp: start}
	switch p.tok.Kind {
	case token.Recv:
		arm.Kind = ast.SelectRecv
		p.advance()
		p.expect(token.LParen)
		p.enterDelimiter()
		arm.Chan = p.parseExpr()
		p.leaveDelimiter()
		p.expect(token.RParen)
		p.expect(token.Arrow)
		arm.RecvValue = p.binding()
		p.expect(token.Comma)
		arm.RecvOk = p.binding()
	case token.Send:
		arm.Kind = ast.SelectSend
		p.advance()
		p.expect(token.LParen)
		p.enterDelimiter()
		arm.Chan = p.parseExpr()
		p.expect(token.Comma)
		arm.Value = p.parseExpr()
		p.leaveDelimiter()
		p.expect(token.RParen)
	case token.Timeout:
		arm.Kind = ast.SelectTimeout
		p.advance()
		p.expect(token.LParen)
		p.enterDelimiter()
		arm.Value = p.parseExpr()
		p.leaveDelimiter()
		p.expect(token.RParen)
	case token.Default:
		arm.Kind = ast.SelectDefault
		p.advance()
	default:
		p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
			"write recv(ch) -> v, ok, send(ch, v), timeout(d), or default",
			"unexpected %s in a select arm", describe(p.tok))
		p.advance()
		arm.Sp = p.spanFrom(start)
		return arm
	}
	arm.Body = p.parseBody()
	arm.Sp = p.spanFrom(start)
	return arm
}
