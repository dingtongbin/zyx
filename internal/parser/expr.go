// Expressions, from literals through the full operator precedence ladder.

package parser

import (
	"strconv"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/lexer"
	"zyx/internal/source"
	"zyx/internal/token"
)

// enterHeader marks the start of a control-flow header, where a following
// brace opens a body block rather than an aggregate literal.
func (p *parser) enterHeader() { p.headerDepth++ }

// leaveHeader undoes enterHeader.
func (p *parser) leaveHeader() { p.headerDepth-- }

// enterDelimiter lifts the body-brace reading inside parentheses, brackets,
// and literal braces, where a brace cannot start a body.
func (p *parser) enterDelimiter() { p.headerDepth-- }

// leaveDelimiter undoes enterDelimiter.
func (p *parser) leaveDelimiter() { p.headerDepth++ }

// aggregateAllowed reports whether a `Name {` sequence may start an
// aggregate literal at the current position.
func (p *parser) aggregateAllowed() bool { return p.headerDepth <= 0 }

// parseExpr parses a full expression. Assignment is a statement in zyx, so
// the assignment operators are left for the statement parser.
func (p *parser) parseExpr() ast.Expr { return p.parseBinary(1) }

// binaryPrec returns the precedence of a binary operator, or 0 when kind
// starts no binary operator. A larger number binds tighter, matching the
// ladder in the language reference.
func binaryPrec(k token.Kind) int {
	switch k {
	case token.Star, token.Slash, token.Percent:
		return 10
	case token.Plus, token.Minus:
		return 9
	case token.Shl, token.Shr:
		return 8
	case token.Amp, token.Caret, token.Pipe:
		return 7
	case token.Eq, token.Ne, token.Lt, token.Le, token.Gt, token.Ge:
		return 6
	case token.AndAnd:
		return 5
	case token.OrOr:
		return 4
	case token.Range:
		return 3
	}
	return 0
}

// parseBinary parses operators of precedence minPrec and higher, plus the
// `is` test that binds just below them.
func (p *parser) parseBinary(minPrec int) ast.Expr {
	x := p.parseIs()
	last := 0
	for {
		prec := binaryPrec(p.tok.Kind)
		if prec == 0 || prec < minPrec {
			return x
		}
		if prec == last && (prec == 6 || prec == 3) {
			p.errorHelpAt(p.tok, diag.CodeChainedOperator,
				"write a separate comparison or range",
				"operator %s cannot be chained", p.tok.Kind)
		}
		op := p.tok
		p.advance()
		y := p.parseBinary(prec + 1)
		start := x.Span()
		if op.Kind == token.Range {
			x = &ast.RangeExpr{Sp: p.spanFrom(start), Lo: x, Hi: y}
		} else {
			x = &ast.BinaryExpr{Sp: p.spanFrom(start), Op: op.Kind, X: x, Y: y}
		}
		last = prec
	}
}

// parseIs parses `x is T`; the test does not chain.
func (p *parser) parseIs() ast.Expr {
	x := p.parseUnary()
	if !p.at(token.Is) {
		return x
	}
	p.advance()
	t := p.parseType()
	e := &ast.IsExpr{Sp: p.spanFrom(x.Span()), X: x, Type: t}
	if p.at(token.Is) {
		p.errorHelpAt(p.tok, diag.CodeChainedOperator,
			"test one interface at a time", "`is` cannot be chained")
	}
	return e
}

// parseUnary parses the prefix operators, `try`, `fast`, and `spawn`.
func (p *parser) parseUnary() ast.Expr {
	start := p.tok.Span
	switch p.tok.Kind {
	case token.Minus, token.Bang, token.Tilde:
		op := p.tok.Kind
		p.advance()
		x := p.parseUnary()
		return &ast.UnaryExpr{Sp: p.spanFrom(start), Op: op, X: x}
	case token.Amp:
		p.advance()
		x := p.parsePostfix()
		return &ast.AddrOfExpr{Sp: p.spanFrom(start), X: x}
	case token.Try:
		p.advance()
		x := p.parsePostfix()
		return &ast.TryExpr{Sp: p.spanFrom(start), X: x}
	case token.Fast:
		p.advance()
		x := p.parsePostfix()
		if !isCallExpr(x) {
			p.errorHelpSpan(x.Span(), diag.CodeUnexpectedToken,
				"mark a C call with fast", "`fast` applies to a call")
		}
		return &ast.FastExpr{Sp: p.spanFrom(start), X: x}
	case token.Spawn, token.SpawnWatched:
		return p.parseSpawn()
	}
	return p.parsePostfix()
}

// parseSpawn parses `spawn call` or `spawn_watched call, watcher`.
func (p *parser) parseSpawn() ast.Expr {
	start := p.tok.Span
	watched := p.at(token.SpawnWatched)
	p.advance()
	x := p.parsePostfix()
	if !isCallExpr(x) {
		p.errorHelpSpan(x.Span(), diag.CodeUnexpectedToken,
			"start a coroutine by calling a function",
			"spawn applies to a call")
	}
	e := &ast.SpawnExpr{Sp: p.spanFrom(start), X: x}
	if watched {
		p.expect(token.Comma)
		e.Watcher = p.parseUnary()
	}
	e.Sp = p.spanFrom(start)
	return e
}

// parsePostfix parses calls, indexing, slicing, selectors, and tuple element
// reads.
func (p *parser) parsePostfix() ast.Expr {
	x := p.parsePrimary()
	for {
		switch p.tok.Kind {
		case token.LParen:
			args := p.parseArgs()
			x = &ast.CallExpr{Sp: p.spanFrom(x.Span()), Fn: x, Args: args}
		case token.LBrack:
			p.advance()
			p.enterDelimiter()
			idx := p.parseExpr()
			p.leaveDelimiter()
			p.expect(token.RBrack)
			x = &ast.IndexExpr{Sp: p.spanFrom(x.Span()), X: x, Index: idx}
		case token.Dot:
			p.advance()
			if p.at(token.Int) {
				index := p.parseTupleIndex(p.tok)
				p.advance()
				x = &ast.TupleIndexExpr{Sp: p.spanFrom(x.Span()), X: x, Index: index}
				continue
			}
			name := p.name()
			x = &ast.SelectorExpr{Sp: p.spanFrom(x.Span()), X: x, Name: name}
		default:
			return x
		}
	}
}

// parseTupleIndex reads the element position that follows a dot.
func (p *parser) parseTupleIndex(tok token.Token) int {
	n, err := strconv.Atoi(strings.ReplaceAll(tok.Text(), "_", ""))
	if err != nil || tok.Suffix != token.SuffixNone {
		p.errorHelpAt(tok, diag.CodeInvalidTupleIndex,
			"write the element position as a plain integer",
			"invalid tuple element position %q", tok.Text())
		return 0
	}
	return n
}

// parsePrimary parses a literal, a name, a parenthesized form, an array
// literal, an allocation, a function value, or an inline C block.
func (p *parser) parsePrimary() ast.Expr {
	start := p.tok.Span
	switch p.tok.Kind {
	case token.Int, token.Float, token.Char, token.Str, token.ByteStr,
		token.True, token.False:
		lit := &ast.Literal{
			Sp:     start,
			Kind:   p.tok.Kind,
			Value:  literalValue(p.tok),
			Suffix: p.tok.Suffix,
		}
		p.advance()
		return lit
	case token.FString:
		lit := &ast.Literal{
			Sp:    start,
			Kind:  p.tok.Kind,
			Parts: p.fstringParts(p.tok),
		}
		p.advance()
		return lit
	case token.Ident:
		id := &ast.Ident{Sp: start, Name: p.tok.Text()}
		p.advance()
		if p.at(token.LBrace) && p.aggregateAllowed() {
			return p.parseAggregate(id)
		}
		return id
	case token.SelfLower, token.C:
		id := &ast.Ident{Sp: start, Name: p.tok.Text()}
		p.advance()
		return id
	case token.LParen:
		return p.parseParen()
	case token.LBrack:
		return p.parseArrayLit()
	case token.New:
		return p.parseNew()
	case token.Func:
		return p.parseFuncExpr()
	}
	p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
		"write an expression here", "expected an expression, found %s", describe(p.tok))
	p.advance()
	return &ast.Ident{Sp: p.spanFrom(start)}
}

// literalValue returns the payload a literal node keeps: decoded text for
// char, string, and byte string literals, source text for numbers and
// booleans.
func literalValue(tok token.Token) string {
	switch tok.Kind {
	case token.Char, token.Str, token.ByteStr:
		return tok.Str
	}
	return tok.Text()
}

// parseParen parses `(expr)` or the tuple `(a, b)`. A single parenthesized
// expression keeps no node of its own.
func (p *parser) parseParen() ast.Expr {
	start := p.tok.Span
	p.advance() // (
	p.enterDelimiter()
	first := p.parseExpr()
	if !p.at(token.Comma) {
		p.leaveDelimiter()
		p.expect(token.RParen)
		return first
	}
	t := &ast.TupleExpr{Elems: []ast.Expr{first}}
	for p.accept(token.Comma) {
		if p.at(token.RParen) {
			break
		}
		t.Elems = append(t.Elems, p.parseExpr())
	}
	p.leaveDelimiter()
	p.expect(token.RParen)
	if len(t.Elems) < 2 {
		p.errorHelpAt(p.tok, diag.CodeUnitTuple,
			"drop the trailing comma or add a second element",
			"a tuple needs at least two elements")
	}
	t.Sp = p.spanFrom(start)
	return t
}

// parseArrayLit parses `[a, b]` or the repeated construction `[a] * n`.
func (p *parser) parseArrayLit() ast.Expr {
	start := p.tok.Span
	p.advance() // [
	p.enterDelimiter()
	var elems []ast.Expr
	for p.tok.Kind != token.RBrack && p.tok.Kind != token.EOF {
		elems = append(elems, p.parseExpr())
		if !p.accept(token.Comma) {
			break
		}
	}
	p.leaveDelimiter()
	p.expect(token.RBrack)
	if len(elems) == 1 && p.at(token.Star) {
		p.advance()
		count := p.parseExpr()
		return &ast.ArrayRepeatExpr{Sp: p.spanFrom(start), Elem: elems[0], Count: count}
	}
	return &ast.ArrayLitExpr{Sp: p.spanFrom(start), Elems: elems}
}

// parseNew parses `new Type(args)`.
func (p *parser) parseNew() ast.Expr {
	start := p.tok.Span
	p.advance() // new
	e := &ast.NewExpr{Sp: start}
	e.Type = p.parseType()
	e.Args = p.parseArgs()
	e.Sp = p.spanFrom(start)
	return e
}

// parseFuncExpr parses a function value reference or a lambda.
func (p *parser) parseFuncExpr() ast.Expr {
	start := p.tok.Span
	p.advance() // func
	if !p.at(token.LParen) {
		ref := &ast.FuncRefExpr{Name: p.name()}
		if p.accept(token.Dot) {
			ref.Qualifier = ref.Name
			ref.Name = p.name()
		}
		ref.Sp = p.spanFrom(start)
		return ref
	}
	l := &ast.LambdaExpr{Sp: start}
	l.Params = p.parseParams()
	if p.accept(token.Arrow) {
		l.Result = p.parseType()
	}
	switch p.tok.Kind {
	case token.FatArrow:
		p.advance()
		l.BodyExpr = p.parseExpr()
	case token.LBrace:
		l.Body = p.parseBlock()
	default:
		p.expected(token.FatArrow)
	}
	l.Sp = p.spanFrom(start)
	return l
}

// parseArgs parses a call argument list.
func (p *parser) parseArgs() []ast.Expr {
	p.expect(token.LParen)
	p.enterDelimiter()
	var args []ast.Expr
	for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
		args = append(args, p.parseExpr())
		if !p.accept(token.Comma) {
			break
		}
	}
	p.leaveDelimiter()
	p.expect(token.RParen)
	return args
}

// parseAggregate parses the entries of `Name { ... }`. An entry is a bare
// value or a `key: value` pair.
func (p *parser) parseAggregate(name *ast.Ident) ast.Expr {
	start := name.Sp
	lit := &ast.AggregateLitExpr{Sp: start, Name: name}
	p.advance() // {
	p.enterDelimiter()
	p.skipLineBreaks()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		before := p.pos
		e := &ast.AggregateEntry{Sp: p.tok.Span}
		first := p.parseExpr()
		if p.accept(token.Colon) {
			e.Key = first
			e.Value = p.parseExpr()
		} else {
			e.Value = first
		}
		e.Sp = p.spanFrom(e.Sp)
		lit.Entries = append(lit.Entries, e)
		if p.pos == before {
			p.advance()
		}
		p.skipLineBreaks()
		p.accept(token.Comma)
		p.skipLineBreaks()
	}
	p.leaveDelimiter()
	p.expect(token.RBrace)
	lit.Sp = p.spanFrom(start)
	return lit
}

// fstringParts turns the lexer's f-string segments into tree segments,
// parsing each interpolation expression from its source span.
func (p *parser) fstringParts(tok token.Token) []ast.FStrPart {
	parts := make([]ast.FStrPart, 0, len(tok.Parts))
	for _, seg := range tok.Parts {
		if seg.Kind == token.FStrText {
			parts = append(parts, ast.FStrPart{Text: seg.Text})
			continue
		}
		parts = append(parts, ast.FStrPart{X: p.parseInterpolation(seg.Span)})
	}
	return parts
}

// parseInterpolation parses the expression inside an f-string
// interpolation by re-lexing its source span.
func (p *parser) parseInterpolation(sp source.Span) ast.Expr {
	toks, diags := lexer.LexExpr(p.file, sp.Start, sp.End)
	p.diags = append(p.diags, diags...)
	if len(toks) == 0 {
		return &ast.Ident{Sp: sp}
	}
	sub := &parser{file: p.file, toks: toks}
	sub.skipComments()
	sub.tok = sub.toks[sub.pos]
	x := sub.parseExpr()
	sub.skipLineBreaks()
	sub.expect(token.EOF)
	sub.reportPendingDocs()
	p.diags = append(p.diags, sub.diags...)
	return x
}

// isCallExpr reports whether x is a call.
func isCallExpr(x ast.Expr) bool {
	_, ok := x.(*ast.CallExpr)
	return ok
}

// isAssignOp reports whether k is an assignment operator.
func isAssignOp(k token.Kind) bool {
	switch k {
	case token.Assign, token.AddAssign, token.SubAssign, token.MulAssign,
		token.DivAssign, token.ModAssign, token.AndAssign, token.OrAssign,
		token.XorAssign, token.ShlAssign, token.ShrAssign:
		return true
	}
	return false
}

// isAssignTarget reports whether x is a variable, a field, or an index.
func isAssignTarget(x ast.Expr) bool {
	switch x.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr:
		return true
	}
	return false
}

// isExprStatement reports whether x may stand alone as a statement: a call,
// a spawn, a `fast` call, or a `try` call.
func isExprStatement(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.CallExpr, *ast.SpawnExpr:
		return true
	case *ast.FastExpr:
		return isCallExpr(v.X)
	case *ast.TryExpr:
		return isCallExpr(v.X)
	}
	return false
}
