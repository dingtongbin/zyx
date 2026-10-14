// Type expressions.

package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// parseType parses a type expression.
func (p *parser) parseType() ast.TypeExpr {
	start := p.tok.Span
	switch p.tok.Kind {
	case token.LBrack:
		return p.parseBracketType(start)
	case token.LParen:
		return p.parseTupleType(start)
	case token.Func:
		return p.parseFuncType(start)
	case token.Chan:
		p.advance()
		p.expect(token.Lt)
		elem := p.parseType()
		p.expectGt()
		return &ast.ChanType{Sp: p.spanFrom(start), Elem: elem}
	case token.Ident, token.SelfUpper, token.Type, token.C:
		return p.parseNamedType(start)
	}
	p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
		"write a type here", "expected a type, found %s", describe(p.tok))
	p.advance()
	return &ast.TypeName{Sp: p.spanFrom(start)}
}

// parseNamedType parses `Name`, `pkg.Name`, or `Name<Args>`.
func (p *parser) parseNamedType(start source.Span) ast.TypeExpr {
	t := &ast.TypeName{Name: &ast.Ident{Sp: p.tok.Span, Name: p.tok.Text()}}
	p.advance()
	if p.accept(token.Dot) {
		t.Qual = t.Name
		t.Name = p.name()
	}
	if p.at(token.Lt) {
		t.Args = p.parseTypeArgs()
	}
	t.Sp = p.spanFrom(start)
	return t
}

// parseTypeArgs parses `<Type, ...>`.
func (p *parser) parseTypeArgs() []ast.TypeExpr {
	p.advance() // <
	var args []ast.TypeExpr
	for {
		if p.gtCount > 0 {
			break
		}
		if p.at(token.Gt) || p.at(token.Shr) || p.at(token.EOF) {
			break
		}
		args = append(args, p.parseType())
		if !p.accept(token.Comma) {
			break
		}
	}
	p.expectGt()
	return args
}

// parseBracketType parses the slice type `[T]` or the array type `[T; N]`.
func (p *parser) parseBracketType(start source.Span) ast.TypeExpr {
	p.advance() // [
	elem := p.parseType()
	if p.accept(token.Semicolon) {
		n := p.parseExpr()
		p.expect(token.RBrack)
		return &ast.ArrayType{Sp: p.spanFrom(start), Elem: elem, Len: n}
	}
	p.expect(token.RBrack)
	return &ast.SliceType{Sp: p.spanFrom(start), Elem: elem}
}

// parseTupleType parses `(A, B)`; the language has no unit or one-element
// tuple type.
func (p *parser) parseTupleType(start source.Span) ast.TypeExpr {
	p.advance() // (
	t := &ast.TupleType{Elems: []ast.TypeExpr{p.parseType()}}
	for p.accept(token.Comma) {
		if p.at(token.RParen) {
			break
		}
		t.Elems = append(t.Elems, p.parseType())
	}
	p.expect(token.RParen)
	if len(t.Elems) < 2 {
		p.errorHelpAt(p.tok, diag.CodeUnitTuple,
			"write the element type without parentheses",
			"a tuple type needs at least two elements")
	}
	t.Sp = p.spanFrom(start)
	return t
}

// parseFuncType parses `func(A, B) -> C`; the result is optional.
func (p *parser) parseFuncType(start source.Span) ast.TypeExpr {
	p.advance() // func
	t := &ast.FuncType{}
	p.expect(token.LParen)
	for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
		t.Params = append(t.Params, p.parseType())
		if !p.accept(token.Comma) {
			break
		}
	}
	p.expect(token.RParen)
	if p.accept(token.Arrow) {
		t.Result = p.parseType()
	}
	t.Sp = p.spanFrom(start)
	return t
}
