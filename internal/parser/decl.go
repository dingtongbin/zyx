// Top-level and type-member declarations.

package parser

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/source"
	"zyx/internal/token"
)

// parsePackage parses the required package clause that opens a file.
func (p *parser) parsePackage() *ast.PackageDecl {
	doc := p.takeDoc()
	d := &ast.PackageDecl{Doc: doc, Sp: p.tok.Span}
	if !p.at(token.Package) {
		p.errorHelpAt(p.tok, diag.CodeMisplacedDecl,
			"start the file with `package <name>`",
			"file must begin with a package declaration")
		return d
	}
	start := p.tok.Span
	p.advance()
	if p.at(token.Ident) {
		d.Name = p.name()
	} else {
		p.expected(token.Ident)
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseImport parses `import "path"` or `import c "header"`.
func (p *parser) parseImport() *ast.ImportDecl {
	start := p.tok.Span
	d := &ast.ImportDecl{Doc: p.takeDoc(), Sp: start}
	p.advance() // import
	if p.at(token.C) {
		d.CHeader = true
		p.advance()
	}
	if p.at(token.Str) {
		d.Path = p.tok.Str
		d.PathSp = p.tok.Span
		p.advance()
	} else {
		p.expected(token.Str)
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseDecl parses one top-level declaration.
func (p *parser) parseDecl() ast.Decl {
	switch p.tok.Kind {
	case token.Const:
		return p.parseConst()
	case token.Var:
		return p.parseVar()
	case token.Type:
		return p.parseTypeAlias()
	case token.Struct:
		start, doc := p.tok.Span, p.takeDoc()
		return p.parseStruct(false, start, doc)
	case token.Enum:
		return p.parseEnum()
	case token.Interface:
		return p.parseInterface()
	case token.Func, token.Comptime, token.C:
		start, doc := p.tok.Span, p.takeDoc()
		return p.parseFunc(doc, start)
	case token.Extern:
		start, doc := p.tok.Span, p.takeDoc()
		switch p.peek(1).Kind {
		case token.Struct:
			p.advance()
			return p.parseStruct(true, start, doc)
		case token.Union:
			p.advance()
			return p.parseUnion(start, doc)
		}
		return p.parseFunc(doc, start)
	case token.Import:
		p.errorHelpAt(p.tok, diag.CodeMisplacedDecl,
			"move every import above the other declarations",
			"imports must precede other declarations")
		p.sync()
		return nil
	case token.Package:
		p.errorHelpAt(p.tok, diag.CodeMisplacedDecl,
			"keep a single package clause at the top of the file",
			"duplicate package declaration")
		p.sync()
		return nil
	}
	p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
		"write const, var, type, func, struct, enum, or interface",
		"unexpected %s at top level", describe(p.tok))
	p.advance()
	return nil
}

// parseConst parses `const Name (: Type)? = ComptimeExpr`.
func (p *parser) parseConst() *ast.ConstDecl {
	start := p.tok.Span
	d := &ast.ConstDecl{Doc: p.takeDoc(), Sp: start}
	p.advance() // const
	d.Name = p.name()
	if p.accept(token.Colon) {
		d.Type = p.parseType()
	}
	if p.expect(token.Assign) {
		d.Value = p.parseExpr()
	} else {
		p.sync()
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseVar parses `var a, b (: Type)? = Expr`.
func (p *parser) parseVar() *ast.VarDecl {
	start := p.tok.Span
	d := &ast.VarDecl{Doc: p.takeDoc(), Sp: start}
	p.advance() // var
	d.Names = append(d.Names, p.binding())
	for p.accept(token.Comma) {
		d.Names = append(d.Names, p.binding())
	}
	if p.accept(token.Colon) {
		d.Type = p.parseType()
	}
	if p.expect(token.Assign) {
		d.Value = p.parseExpr()
	} else {
		p.sync()
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseTypeAlias parses `type Name = Type`.
func (p *parser) parseTypeAlias() *ast.TypeAlias {
	start := p.tok.Span
	d := &ast.TypeAlias{Doc: p.takeDoc(), Sp: start}
	p.advance() // type
	d.Name = p.name()
	if p.expect(token.Assign) {
		d.Type = p.parseType()
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseFunc parses a function, a method, or an interface signature.
func (p *parser) parseFunc(doc *ast.Doc, start source.Span) *ast.FuncDecl {
	d := &ast.FuncDecl{Doc: doc, Sp: start}
	var qualCount int
	var firstQual token.Token
	for p.at(token.Comptime) || p.at(token.Extern) || p.at(token.C) {
		if qualCount == 0 {
			firstQual = p.tok
		}
		qualCount++
		switch p.tok.Kind {
		case token.Comptime:
			d.Quals.Comptime = true
		case token.Extern:
			d.Quals.Extern = true
		case token.C:
			d.Quals.CAbi = true
		}
		p.advance()
	}
	if qualCount > 1 {
		p.errorHelpAt(firstQual, diag.CodeUnexpectedToken,
			"keep at most one of comptime, extern, or c",
			"a function takes at most one qualifier")
	}
	if !p.expect(token.Func) {
		p.sync()
		d.Sp = p.spanFrom(start)
		return d
	}
	d.Name = p.name()
	d.TypeParams = p.parseTypeParams()
	d.Params = p.parseParams()
	if p.accept(token.Arrow) {
		d.Result = p.parseType()
	}
	switch p.tok.Kind {
	case token.LBrace:
		d.Body = p.parseBlock()
	case token.FatArrow:
		p.advance()
		d.BodyExpr = p.parseExpr()
	}
	d.Sp = p.spanFrom(start)
	return d
}

// parseTypeParams parses `<T, U: Eq + Show>`, returning nil without `<`.
func (p *parser) parseTypeParams() []*ast.TypeParam {
	if !p.at(token.Lt) {
		return nil
	}
	p.advance()
	var params []*ast.TypeParam
	for {
		if p.gtCount > 0 {
			break
		}
		if p.at(token.Gt) || p.at(token.Shr) || p.at(token.EOF) {
			break
		}
		start := p.tok.Span
		tp := &ast.TypeParam{Name: p.name()}
		if p.accept(token.Colon) {
			tp.Constraints = append(tp.Constraints, p.parseType())
			for p.accept(token.Plus) {
				tp.Constraints = append(tp.Constraints, p.parseType())
			}
		}
		tp.Sp = p.spanFrom(start)
		params = append(params, tp)
		if !p.accept(token.Comma) {
			break
		}
	}
	p.expectGt()
	return params
}

// parseParams parses a parameter list. A trailing `...` is the variadic
// parameter of an extern function.
func (p *parser) parseParams() []*ast.Param {
	p.expect(token.LParen)
	var params []*ast.Param
	for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
		start := p.tok.Span
		pr := &ast.Param{}
		if p.at(token.Ellipsis) {
			pr.Variadic = true
			p.advance()
		} else {
			pr.Name = p.name()
			if p.expect(token.Colon) {
				pr.Type = p.parseType()
			}
			if p.accept(token.Assign) {
				pr.Default = p.parseExpr()
			}
		}
		pr.Sp = p.spanFrom(start)
		params = append(params, pr)
		if !p.accept(token.Comma) {
			break
		}
	}
	p.expect(token.RParen)
	return params
}

// parseStruct parses a struct declaration and its members.
func (p *parser) parseStruct(extern bool, start source.Span, doc *ast.Doc) *ast.StructDecl {
	d := &ast.StructDecl{Doc: doc, Extern: extern, Sp: start}
	p.advance() // struct
	d.Name = p.name()
	d.TypeParams = p.parseTypeParams()
	p.expect(token.LBrace)
	d.Members = p.parseTypeMembers(memberStruct)
	p.expect(token.RBrace)
	d.Sp = p.spanFrom(start)
	return d
}

// parseUnion parses `extern union Name { fields }`. A union is always C
// layout: every member starts at offset 0.
func (p *parser) parseUnion(start source.Span, doc *ast.Doc) *ast.UnionDecl {
	d := &ast.UnionDecl{Doc: doc, Sp: start}
	p.advance() // union
	d.Name = p.name()
	p.expect(token.LBrace)
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		if p.at(token.Newline) || p.at(token.Semicolon) {
			p.advance()
			continue
		}
		before := p.pos
		if p.at(token.Ident) {
			d.Fields = append(d.Fields, p.parseField())
		} else {
			p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
				"write a field", "unexpected %s in a union body", describe(p.tok))
			p.advance()
		}
		if p.pos == before {
			p.advance()
		}
		p.skipSeparators()
	}
	p.expect(token.RBrace)
	d.Sp = p.spanFrom(start)
	return d
}

// parseEnum parses an enum declaration and its variants.
func (p *parser) parseEnum() *ast.EnumDecl {
	start := p.tok.Span
	d := &ast.EnumDecl{Doc: p.takeDoc(), Sp: start}
	p.advance() // enum
	d.Name = p.name()
	d.TypeParams = p.parseTypeParams()
	p.expect(token.LBrace)
	d.Members = p.parseTypeMembers(memberEnum)
	p.expect(token.RBrace)
	d.Sp = p.spanFrom(start)
	return d
}

// parseInterface parses an interface declaration and its method signatures.
func (p *parser) parseInterface() *ast.InterfaceDecl {
	start := p.tok.Span
	d := &ast.InterfaceDecl{Doc: p.takeDoc(), Sp: start}
	p.advance() // interface
	d.Name = p.name()
	d.TypeParams = p.parseTypeParams()
	p.expect(token.LBrace)
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		if p.at(token.Newline) || p.at(token.Semicolon) {
			p.advance()
			continue
		}
		before := p.pos
		if p.at(token.Func) {
			doc, fstart := p.takeDoc(), p.tok.Span
			d.Methods = append(d.Methods, p.parseFunc(doc, fstart))
		} else {
			p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
				"write a method signature",
				"unexpected %s in an interface body", describe(p.tok))
			p.advance()
		}
		if p.pos == before {
			p.advance()
		}
		p.skipSeparators()
	}
	p.expect(token.RBrace)
	d.Sp = p.spanFrom(start)
	return d
}

// memberKind selects the variants a type body accepts.
type memberKind uint8

// Type body kinds.
const (
	memberStruct memberKind = iota
	memberEnum
)

// parseTypeMembers parses the members of a struct or enum body.
func (p *parser) parseTypeMembers(kind memberKind) []ast.Decl {
	var members []ast.Decl
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		if p.at(token.Newline) || p.at(token.Semicolon) {
			p.advance()
			continue
		}
		before := p.pos
		switch p.tok.Kind {
		case token.Func, token.Comptime, token.Extern, token.C:
			doc, start := p.takeDoc(), p.tok.Span
			members = append(members, p.parseFunc(doc, start))
		case token.Impl:
			members = append(members, p.parseImpl())
		case token.Const:
			members = append(members, p.parseConst())
		case token.Ident:
			if kind == memberEnum {
				members = append(members, p.parseVariant())
			} else {
				members = append(members, p.parseField())
			}
		default:
			p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
				"write a field, method, impl block, or const",
				"unexpected %s in a type body", describe(p.tok))
			p.advance()
		}
		if p.pos == before {
			p.advance()
		}
		p.skipSeparators()
	}
	return members
}

// parseField parses a struct field `Name: Type`.
func (p *parser) parseField() *ast.FieldDecl {
	start := p.tok.Span
	f := &ast.FieldDecl{Doc: p.takeDoc(), Sp: start}
	f.Name = p.name()
	if p.expect(token.Colon) {
		f.Type = p.parseType()
	}
	f.Sp = p.spanFrom(start)
	return f
}

// parseVariant parses an enum variant `Name` or `Name(T, U)`.
func (p *parser) parseVariant() *ast.VariantDecl {
	start := p.tok.Span
	v := &ast.VariantDecl{Doc: p.takeDoc(), Sp: start}
	v.Name = p.name()
	if p.accept(token.LParen) {
		for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
			v.Payload = append(v.Payload, p.parseType())
			if !p.accept(token.Comma) {
				break
			}
		}
		p.expect(token.RParen)
	}
	v.Sp = p.spanFrom(start)
	return v
}

// parseImpl parses `impl TypeParams? Interface { methods }`.
func (p *parser) parseImpl() *ast.ImplBlock {
	start := p.tok.Span
	b := &ast.ImplBlock{Doc: p.takeDoc(), Sp: start}
	p.advance() // impl
	b.TypeParams = p.parseTypeParams()
	b.Interface = p.parseType()
	p.expect(token.LBrace)
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF {
		if p.at(token.Newline) || p.at(token.Semicolon) {
			p.advance()
			continue
		}
		before := p.pos
		switch p.tok.Kind {
		case token.Func, token.Comptime, token.C:
			doc, mstart := p.takeDoc(), p.tok.Span
			b.Methods = append(b.Methods, p.parseFunc(doc, mstart))
		default:
			p.errorHelpAt(p.tok, diag.CodeUnexpectedToken,
				"write a method", "unexpected %s in an impl block", describe(p.tok))
			p.advance()
		}
		if p.pos == before {
			p.advance()
		}
		p.skipSeparators()
	}
	p.expect(token.RBrace)
	b.Sp = p.spanFrom(start)
	return b
}
