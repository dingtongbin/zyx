// Match patterns.

package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// parsePattern parses a pattern, including the `p1 | p2` alternatives.
func (p *parser) parsePattern() ast.Pattern {
	start := p.tok.Span
	first := p.parsePatternAtom()
	if !p.at(token.Pipe) {
		return first
	}
	or := &ast.OrPattern{Patterns: []ast.Pattern{first}}
	for p.accept(token.Pipe) {
		or.Patterns = append(or.Patterns, p.parsePatternAtom())
	}
	or.Sp = p.spanFrom(start)
	return or
}

// parsePatternAtom parses one pattern without alternatives.
func (p *parser) parsePatternAtom() ast.Pattern {
	start := p.tok.Span
	switch p.tok.Kind {
	case token.Underscore:
		p.advance()
		return &ast.WildcardPattern{Sp: start}
	case token.LParen:
		return p.parseTuplePattern(start)
	case token.Ident:
		name := p.name()
		if !p.at(token.LParen) {
			return &ast.BindPattern{Sp: p.spanFrom(start), Name: name}
		}
		v := &ast.VariantPattern{Name: name}
		p.advance()
		p.enterDelimiter()
		for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
			v.Payload = append(v.Payload, p.parsePattern())
			if !p.accept(token.Comma) {
				break
			}
		}
		p.leaveDelimiter()
		p.expect(token.RParen)
		v.Sp = p.spanFrom(start)
		return v
	case token.Int, token.Char, token.Str, token.True, token.False, token.Minus:
		lo := p.parsePatternLiteral()
		if p.at(token.Range) {
			p.advance()
			hi := p.parsePatternLiteral()
			return &ast.RangePattern{Sp: p.spanFrom(start), Lo: lo, Hi: hi}
		}
		return &ast.LitPattern{Sp: p.spanFrom(start), X: lo}
	case token.Float:
		p.errorHelpAt(p.tok, diag.CodeFloatPattern,
			"compare the value in a guard instead",
			"float literals cannot be patterns")
		p.advance()
		return &ast.WildcardPattern{Sp: start}
	}
	p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
		"write a pattern here", "expected a pattern, found %s", describe(p.tok))
	p.advance()
	return &ast.WildcardPattern{Sp: start}
}

// parsePatternLiteral parses the literal of a literal or range pattern. A
// leading minus makes a negative integer.
func (p *parser) parsePatternLiteral() *ast.Literal {
	start := p.tok.Span
	neg := p.accept(token.Minus)
	switch p.tok.Kind {
	case token.Int, token.Char, token.Str, token.True, token.False:
	default:
		p.expected(token.Int)
		return &ast.Literal{Sp: p.spanFrom(start)}
	}
	lit := &ast.Literal{
		Kind:   p.tok.Kind,
		Value:  literalValue(p.tok),
		Suffix: p.tok.Suffix,
	}
	if neg {
		lit.Value = "-" + lit.Value
	}
	p.advance()
	lit.Sp = p.spanFrom(start)
	return lit
}

// parseTuplePattern parses `(p, q)`; the language has no one-element tuple
// pattern.
func (p *parser) parseTuplePattern(start source.Span) ast.Pattern {
	p.advance() // (
	p.enterDelimiter()
	t := &ast.TuplePattern{Elems: []ast.Pattern{p.parsePattern()}}
	for p.accept(token.Comma) {
		if p.at(token.RParen) {
			break
		}
		t.Elems = append(t.Elems, p.parsePattern())
	}
	p.leaveDelimiter()
	p.expect(token.RParen)
	if len(t.Elems) < 2 {
		p.errorHelpAt(p.tok, diag.CodeUnitTuple,
			"write the pattern without parentheses",
			"a tuple pattern needs at least two elements")
	}
	t.Sp = p.spanFrom(start)
	return t
}
