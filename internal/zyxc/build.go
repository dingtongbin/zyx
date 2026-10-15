// The builder turns a checked package into the .zyxc tree.

package zyxc

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"zyx/internal/ast"
	"zyx/internal/cheader"
	"zyx/internal/check"
	"zyx/internal/coro"
	"zyx/internal/marks"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/types"
)

// Build renders one package's analyzed form as a .zyxc tree: the imports
// first in byte order, then every declaration in source order, with the
// symbol id of every reference and the marks of every operation.
func Build(pkg *resolve.Package, rinfo *resolve.Info, cpkg *check.Package, minfo *marks.Info, cinfo *coro.Info, target string, cdecls map[string]*cheader.Table) (*File, error) {
	b := &builder{pkg: pkg, rinfo: rinfo, cpkg: cpkg, marks: minfo, coro: cinfo, target: target, cdecls: cdecls}
	f := &File{Target: target, Module: pkg.Path}
	var imports []*Node
	for _, file := range pkg.Files {
		for _, imp := range file.Imports {
			imports = append(imports, b.importNode(imp))
		}
	}
	sort.SliceStable(imports, func(i, j int) bool {
		if imports[i].Kind != imports[j].Kind {
			return imports[i].Kind < imports[j].Kind
		}
		return firstPayload(imports[i]) < firstPayload(imports[j])
	})
	f.Decls = append(f.Decls, imports...)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			f.Decls = append(f.Decls, b.decl(decl))
		}
	}
	f.Decls = compact(f.Decls)
	if b.err != nil {
		return nil, b.err
	}
	f.Exports = ExportsHash(f.Decls)
	return f, nil
}

// nodeName returns the concrete kind of an abstract syntax node, which a
// report names when the builder cannot render it.
func nodeName(node ast.Node) string {
	if node == nil {
		return "nothing"
	}
	name := fmt.Sprintf("%T", node)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// fail records the first node the builder cannot render. A node the builder
// does not know is a hole in the representation, so it is reported rather
// than dropped.
func (b *builder) fail(node ast.Node, what string) {
	if b.err != nil || node == nil {
		return
	}
	b.err = fmt.Errorf("zyxc: %s at %v is not rendered", what, node.Span())
}

// firstPayload returns the first payload token of a node.
func firstPayload(n *Node) string {
	if len(n.Payload) == 0 {
		return ""
	}
	return n.Payload[0]
}

// compact drops nil nodes.
func compact(nodes []*Node) []*Node {
	out := nodes[:0]
	for _, n := range nodes {
		if n != nil {
			out = append(out, n)
		}
	}
	return out
}

// builder carries the state of one package's emission.
type builder struct {
	// err holds the first node the builder could not render.
	err   error
	pkg   *resolve.Package
	rinfo *resolve.Info
	cpkg  *check.Package
	marks *marks.Info
	coro  *coro.Info
	// target is the triple the layout of every type is settled for.
	target string
	// cdecls holds the declarations of every C header the package imported,
	// keyed by the header they came from.
	cdecls map[string]*cheader.Table
}

// mark attaches the marks of a node. Every statement and every expression
// passes through here, so a mark the analysis recorded reaches the format.
func (b *builder) mark(n *Node, at ast.Node) *Node {
	if n == nil {
		return nil
	}
	for _, m := range b.marks.MarksOf(at) {
		n.Mark(m.Name, m.Arg)
	}
	return n
}

// pos renders a source position relative to the module root.
func (b *builder) pos(sp source.Span) string {
	file := sp.File.Name()
	startLine, startCol := sp.StartPos()
	endLine, endCol := sp.File.LineCol(sp.End)
	return file + ":" + strconv.Itoa(startLine) + ":" + strconv.Itoa(startCol) +
		"-" + strconv.Itoa(endLine) + ":" + strconv.Itoa(endCol)
}

// at sets a node's position.
func (b *builder) at(n *Node, sp source.Span) *Node {
	return n.At(b.pos(sp))
}

// typeText renders a type the way the format writes it. Every named type
// the package does not declare is written with its package qualifier, so
// the text names its package wherever it appears and a reader resolves it
// without guessing.
func (b *builder) typeText(t *types.Type) string {
	if t == nil {
		return "invalid"
	}
	return b.qualified(t).String()
}

// qualified returns a copy of a type in which every named type the current
// package does not declare carries its package qualifier.
func (b *builder) qualified(t *types.Type) *types.Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case types.KindTuple, types.KindFunc:
		out := *t
		out.Elems = make([]*types.Type, len(t.Elems))
		for i, e := range t.Elems {
			out.Elems[i] = b.qualified(e)
		}
		out.Result = b.qualified(t.Result)
		return &out
	case types.KindArray, types.KindSlice, types.KindChan:
		out := *t
		out.Elem = b.qualified(t.Elem)
		return &out
	}
	if !t.IsNamed() || t.Pkg == "" || t.Pkg == b.pkg.Path || t.Qual != "" {
		return t
	}
	out := *t
	out.Qual = packageQualifier(t.Pkg)
	if len(t.Args) > 0 {
		out.Args = make([]*types.Type, len(t.Args))
		for i, a := range t.Args {
			out.Args[i] = b.qualified(a)
		}
	}
	return &out
}

// packageQualifier returns the name a package is written by, which is its
// last path segment.
func packageQualifier(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// importNode renders one import declaration. A C header carries the
// declarations clang read from it, so the intermediate form holds the whole C
// surface a program was compiled against.
func (b *builder) importNode(imp *ast.ImportDecl) *Node {
	if imp.CHeader {
		n := b.at(New("import-c", imp.Path), imp.Span())
		table := b.cdecls[imp.Path]
		if table == nil {
			return n
		}
		for _, name := range table.Names() {
			n.Child(cDeclaration(table, name))
		}
		return n
	}
	return b.at(New("import", imp.Path), imp.Span())
}

// cDeclaration renders one declaration a C header exposes.
func cDeclaration(table *cheader.Table, name string) *Node {
	if fn, ok := table.Functions[name]; ok {
		n := New("c-func", name)
		for i, pt := range fn.Params {
			p := New("param", fn.Names[i], pt.String())
			n.Child(p)
		}
		if fn.Result != nil {
			n.Child(New("result", fn.Result.String()))
		}
		if fn.Variadic {
			n.Child(New("c-variadic"))
		}
		return n
	}
	if record, ok := table.Records[name]; ok {
		kind := "struct"
		if record.Union {
			kind = "union"
		}
		return New("c-record", name, kind)
	}
	if alias, ok := table.Aliases[name]; ok {
		return New("c-alias", name, alias.String())
	}
	if value, ok := table.Constants[name]; ok {
		return New("c-const", name, strconv.FormatInt(value, 10))
	}
	if global, ok := table.Globals[name]; ok {
		return New("c-global", name, global.String())
	}
	return nil
}

// decl renders one top-level declaration.
func (b *builder) decl(decl ast.Decl) *Node {
	switch d := decl.(type) {
	case *ast.ConstDecl:
		n := New("const", d.Name.Name, b.symID(d.Name))
		if t := b.declaredType(d.Type, d.Name, d.Value); t != nil {
			n.Child(New("type", b.typeText(t)))
		}
		if d.Value != nil {
			n.Child(b.wrap("value", b.expr(d.Value)))
		}
		return b.at(n, d.Sp)
	case *ast.VarDecl:
		n := New("var")
		for _, name := range d.Names {
			n.Payload = append(n.Payload, name.Name, b.symID(name))
		}
		if t := b.declaredType(d.Type, firstName(d.Names), d.Value); t != nil {
			n.Child(New("type", b.typeText(t)))
		}
		if d.Value != nil {
			n.Child(b.wrap("value", b.expr(d.Value)))
		}
		return b.at(n, d.Sp)
	case *ast.TypeAlias:
		n := New("alias", d.Name.Name, b.symID(d.Name))
		if d.Type != nil {
			n.Child(New("type", b.typeText(b.cpkg.Nodes[d.Type])))
		}
		return b.at(n, d.Sp)
	case *ast.FuncDecl:
		return b.funcNode(d, "")
	case *ast.StructDecl:
		return b.structNode(d)
	case *ast.UnionDecl:
		n := New("extern-union", d.Name.Name)
		for _, f := range d.Fields {
			n.Child(b.fieldDecl(f))
		}
		return b.at(n, d.Sp)
	case *ast.EnumDecl:
		n := New("enum", d.Name.Name, b.symID(d.Name))
		n.Child(b.typeParams(d.TypeParams)...)
		for _, member := range d.Members {
			if v, ok := member.(*ast.VariantDecl); ok {
				n.Child(b.variantNode(v))
			}
		}
		selfType := b.selfType(d.Name)
		for _, member := range d.Members {
			if _, ok := member.(*ast.VariantDecl); !ok {
				if child := b.member(member, selfType); child != nil {
					n.Child(child)
				}
			}
		}
		return b.at(n, d.Sp)
	case *ast.InterfaceDecl:
		n := New("interface", d.Name.Name, b.symID(d.Name))
		n.Child(b.typeParams(d.TypeParams)...)
		for _, m := range d.Methods {
			n.Child(b.sigNode(m))
		}
		return b.at(n, d.Sp)
	}
	b.fail(decl, "declaration "+nodeName(decl))
	return nil
}

// member renders a type body member other than a field or a variant.
func (b *builder) member(member ast.Decl, selfType string) *Node {
	switch m := member.(type) {
	case *ast.FuncDecl:
		return b.funcNode(m, selfType)
	case *ast.ConstDecl:
		n := New("const", m.Name.Name, b.symID(m.Name))
		if m.Type != nil {
			n.Child(New("type", b.typeText(b.cpkg.Nodes[m.Type])))
		}
		if m.Value != nil {
			n.Child(b.expr(m.Value))
		}
		return b.at(n, m.Sp)
	case *ast.ImplBlock:
		n := New("impl", b.implSymbol(m.Interface))
		n.Child(b.typeParams(m.TypeParams)...)
		for _, f := range m.Methods {
			n.Child(b.funcNode(f, selfType))
		}
		return b.at(n, m.Sp)
	}
	b.fail(member, "member "+nodeName(member))
	return nil
}

// symID returns the symbol id bound to an identifier.
func (b *builder) symID(id *ast.Ident) string {
	if sym := b.rinfo.Defs[id]; sym != nil {
		return sym.ID
	}
	return ""
}

// typeParams renders the generic parameters of a declaration.
func (b *builder) typeParams(params []*ast.TypeParam) []*Node {
	var out []*Node
	for _, tp := range params {
		if tp.Name == nil {
			continue
		}
		n := New("type-param", tp.Name.Name, b.symID(tp.Name))
		for _, c := range tp.Constraints {
			n.Payload = append(n.Payload, b.typeText(b.cpkg.Nodes[c]))
		}
		out = append(out, b.at(n, tp.Sp))
	}
	return out
}

// hasDefer reports whether a statement holds a deferred call.
func (b *builder) hasDefer(s ast.Stmt) bool {
	switch v := s.(type) {
	case nil:
		return false
	case *ast.BlockStmt:
		for _, stmt := range v.Stmts {
			if b.hasDefer(stmt) {
				return true
			}
		}
	case *ast.DeferStmt:
		return true
	case *ast.IfStmt:
		return b.hasDefer(v.Then) || b.hasDefer(v.Else)
	case *ast.ForStmt:
		return b.hasDefer(v.Body)
	case *ast.RegionStmt:
		// A nested region runs its own defers.
		return false
	case *ast.ComptimeBlockStmt:
		return b.hasDefer(v.Body)
	case *ast.MatchStmt:
		for _, arm := range v.Arms {
			if b.hasDefer(arm.Body) {
				return true
			}
		}
	case *ast.SelectStmt:
		for _, arm := range v.Arms {
			if b.hasDefer(arm.Body) {
				return true
			}
		}
	}
	return false
}

// selfType returns the declared type of a type body, which is what a method
// of that body receives. Writing it on the method removes the only place
// where the backend would have had to read it back out of the symbol id.
func (b *builder) selfType(name *ast.Ident) string {
	if t := b.staticType(name); t != nil {
		return b.typeText(t)
	}
	return ""
}

// funcNode renders a function, a method, or an interface signature.
func (b *builder) funcNode(f *ast.FuncDecl, selfType string) *Node {
	qual := "plain"
	switch {
	case f.Quals.Comptime:
		qual = "comptime"
	case f.Quals.Extern:
		qual = "extern"
	case f.Quals.CAbi:
		qual = "c"
	}
	n := New("func", f.Name.Name, qual, b.symID(f.Name))
	if selfType != "" {
		n.Child(New("receiver", selfType))
	}
	if f.Name.Name == "init" {
		n.Mark("init", "")
	}
	if f.Name.Name == "main" {
		n.Mark("entry", "")
	}
	n.Child(b.typeParams(f.TypeParams)...)
	for _, p := range f.Params {
		pn := New("param")
		if p.Name != nil {
			pn.Payload = append(pn.Payload, p.Name.Name)
		} else {
			pn.Payload = append(pn.Payload, "...")
		}
		pn.Payload = append(pn.Payload, b.typeText(b.cpkg.Nodes[p.Type]))
		n.Child(b.at(pn, p.Sp))
	}
	if f.Result != nil {
		n.Child(b.at(New("result", b.typeText(b.cpkg.Nodes[f.Result])), f.Result.Span()))
	}
	if f.Body != nil {
		if b.hasDefer(f.Body) {
			n.Mark("defers", "")
		}
		n.Child(b.block(f.Body))
	} else if f.BodyExpr != nil {
		n.Child(b.expr(f.BodyExpr))
	}
	return b.at(n, f.Sp)
}

// sigNode renders an interface method signature.
func (b *builder) sigNode(f *ast.FuncDecl) *Node {
	n := New("sig", f.Name.Name, b.symID(f.Name))
	for _, p := range f.Params {
		pn := New("param")
		if p.Name != nil {
			pn.Payload = append(pn.Payload, p.Name.Name)
		}
		pn.Payload = append(pn.Payload, b.typeText(b.cpkg.Nodes[p.Type]))
		n.Child(b.at(pn, p.Sp))
	}
	if f.Result != nil {
		n.Child(b.at(New("result", b.typeText(b.cpkg.Nodes[f.Result])), f.Result.Span()))
	}
	return b.at(n, f.Sp)
}

// structNode renders a struct, extern struct, or the like.
func (b *builder) structNode(d *ast.StructDecl) *Node {
	kind := "struct"
	if d.Extern {
		kind = "extern-struct"
	}
	n := New(kind, d.Name.Name, b.symID(d.Name))
	n.Child(b.typeParams(d.TypeParams)...)
	selfType := b.selfType(d.Name)
	if t := b.staticType(d.Name); t != nil {
		for _, f := range b.fieldNodes(t, d) {
			n.Child(f)
		}
	}
	for _, member := range d.Members {
		if _, ok := member.(*ast.FieldDecl); ok {
			continue
		}
		if child := b.member(member, selfType); child != nil {
			n.Child(child)
		}
	}
	return b.at(n, d.Sp)
}

// fieldNodes renders the fields of a named aggregate in declaration order.
// Size, alignment, and offset are not written: they are a pure function of
// the type and the target, and the backend derives them from the same table
// and the same algorithm the front end uses for meta queries.
func (b *builder) fieldNodes(t *types.Type, decl *ast.StructDecl) []*Node {
	_ = t
	return b.declaredFields(decl)
}

// declaredFields renders the fields of a declaration in declaration order.
func (b *builder) declaredFields(decl *ast.StructDecl) []*Node {
	if decl == nil {
		return nil
	}
	var out []*Node
	for _, member := range decl.Members {
		f, ok := member.(*ast.FieldDecl)
		if !ok {
			continue
		}
		out = append(out, b.fieldDecl(f))
	}
	return out
}

// fieldDecl renders one field declaration.
func (b *builder) fieldDecl(f *ast.FieldDecl) *Node {
	n := New("field-decl", f.Name.Name, b.typeText(b.cpkg.Nodes[f.Type]))
	return b.at(n, f.Sp)
}

// variantNode renders an enum variant.
func (b *builder) variantNode(v *ast.VariantDecl) *Node {
	n := New("variant", v.Name.Name)
	if len(v.Payload) > 0 {
		parts := make([]string, 0, len(v.Payload))
		for _, t := range v.Payload {
			parts = append(parts, b.typeText(b.cpkg.Nodes[t]))
		}
		n.Child(New("type", "("+joinComma(parts)+")"))
	}
	return b.at(n, v.Sp)
}

// joinComma joins type texts with the format's separator.
func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// bindingType returns the `type` child of a local binding. A declared type
// wins; otherwise the type of the initializer settles a binding that
// introduces one name.
func (b *builder) bindingType(decl ast.Node, value ast.Expr, names int) *Node {
	if t := b.staticType(decl); t != nil {
		return New("type", b.typeText(t))
	}
	if names == 1 {
		if t := b.staticType(value); t != nil {
			return New("type", b.typeText(t))
		}
	}
	return nil
}

// block renders a statement block.
func (b *builder) block(s *ast.BlockStmt) *Node {
	n := New("block")
	if s != nil {
		for _, stmt := range s.Stmts {
			n.Child(b.stmt(stmt))
		}
	}
	return n
}

// stmt renders one statement.
// stmt renders a statement and attaches its marks. Every statement reaches
// the format through here, so a mark the analysis recorded is never lost on
// the way.
func (b *builder) stmt(s ast.Stmt) *Node {
	return b.mark(b.stmtNode(s), s)
}

// stmtNode renders a statement without its marks.
func (b *builder) stmtNode(s ast.Stmt) *Node {
	switch v := s.(type) {
	case nil:
		return nil
	case *ast.BlockStmt:
		return b.block(v)
	case *ast.ExprStmt:
		n := New("expr-stmt").Child(b.expr(v.X))
		return b.at(n, v.Sp)
	case *ast.AssignStmt:
		return b.assignNode(v)
	case *ast.VarDecl:
		n := New("bind")
		for _, name := range v.Names {
			n.Payload = append(n.Payload, name.Name, b.symID(name))
		}
		if child := b.bindingType(v.Type, v.Value, len(v.Names)); child != nil {
			n.Child(child)
		}
		if v.Value != nil {
			n.Child(b.wrap("init", b.expr(v.Value)))
		}
		return b.at(n, v.Sp)
	case *ast.ConstDecl:
		n := New("bind-const", v.Name.Name, b.symID(v.Name))
		if child := b.bindingType(v.Type, v.Value, 1); child != nil {
			n.Child(child)
		}
		if v.Value != nil {
			n.Child(b.wrap("value", b.expr(v.Value)))
		}
		return b.at(n, v.Sp)
	case *ast.IfStmt:
		n := New("if")
		n.Child(b.wrap("cond", b.expr(v.Cond)))
		n.Child(b.wrap("then", b.stmt(v.Then)))
		if v.Else != nil {
			n.Child(b.wrap("else", b.stmt(v.Else)))
		}
		return b.at(n, v.Sp)
	case *ast.ForStmt:
		return b.forNode(v)
	case *ast.BreakStmt:
		n := New("break")
		if v.Label != nil {
			n.Payload = append(n.Payload, v.Label.Name)
		}
		return b.at(n, v.Sp)
	case *ast.ContinueStmt:
		n := New("continue")
		if v.Label != nil {
			n.Payload = append(n.Payload, v.Label.Name)
		}
		return b.at(n, v.Sp)
	case *ast.ReturnStmt:
		n := New("return")
		for _, e := range v.Values {
			n.Child(b.expr(e))
		}
		return b.at(n, v.Sp)
	case *ast.DeferStmt:
		n := New("defer").Child(b.wrap("call", b.expr(v.Call)))
		return b.at(n, v.Sp)
	case *ast.RegionStmt:
		name := ""
		for _, m := range b.marks.MarksOf(v) {
			if m.Name == "region:enter" {
				name = m.Arg
			}
		}
		n := New("region", name)
		if b.hasDefer(v.Body) {
			n.Mark("defers", "")
		}
		n.Child(b.wrap("body", b.block(v.Body)))
		return b.at(n, v.Sp)
	case *ast.ComptimeBlockStmt:
		n := New("block").Child(b.block(v.Body))
		return b.at(n, v.Sp)
	case *ast.MatchStmt:
		n := New("match").Child(b.wrap("subject", b.expr(v.Subject)))
		for _, arm := range v.Arms {
			n.Child(b.armNode(arm))
		}
		return b.at(n, v.Sp)
	case *ast.SelectStmt:
		n := New("select")
		for _, arm := range v.Arms {
			n.Child(b.selectArm(arm))
		}
		return b.at(n, v.Sp)
	}
	b.fail(s, "statement "+nodeName(s))
	return nil
}

// wrap wraps a child in a labelled node.
func (b *builder) wrap(label string, child *Node) *Node {
	n := New(label)
	if child != nil {
		n.Child(child)
	}
	return n
}

// assignNode renders an assignment or a compound assignment.
func (b *builder) assignNode(v *ast.AssignStmt) *Node {
	if v.Op.String() == "=" {
		n := New("assign")
		n.Child(b.wrap("target", b.expr(v.Target)))
		n.Child(b.wrap("value", b.expr(v.Value)))
		return b.at(n, v.Sp)
	}
	n := New("assign-op", v.Op.String())
	n.Child(b.wrap("target", b.expr(v.Target)))
	n.Child(b.wrap("value", b.expr(v.Value)))
	return b.at(n, v.Sp)
}

// forNode renders one of the three loop forms.
func (b *builder) forNode(v *ast.ForStmt) *Node {
	kind := "for"
	if len(v.Bindings) > 0 {
		kind = "for-in"
	}
	n := New(kind)
	if v.Label != nil {
		n.Payload = append(n.Payload, v.Label.Name)
	}
	if kind == "for-in" {
		for i := 0; i < 2; i++ {
			if i < len(v.Bindings) && v.Bindings[i] != nil {
				n.Payload = append(n.Payload, v.Bindings[i].Name, b.symID(v.Bindings[i]))
				continue
			}
			n.Payload = append(n.Payload, "_", "")
		}
		n.Child(b.wrap("iter", b.expr(v.Iter)))
		for _, binding := range v.Bindings {
			if binding == nil {
				continue
			}
			if t := b.staticType(binding); t != nil {
				n.Child(New("type", b.typeText(t)))
			}
		}
	} else if v.Iter != nil {
		n.Child(b.wrap("cond", b.expr(v.Iter)))
	}
	n.Child(b.wrap("body", b.stmt(v.Body)))
	return b.at(n, v.Sp)
}

// armNode renders one match arm.
func (b *builder) armNode(arm *ast.MatchArm) *Node {
	n := New("arm")
	n.Child(b.wrap("pat", b.pattern(arm.Pattern)))
	if arm.Guard != nil {
		n.Child(b.wrap("guard", b.expr(arm.Guard)))
	}
	n.Child(b.wrap("body", b.stmt(arm.Body)))
	return b.at(n, arm.Sp)
}

// selectArm renders one select arm.
func (b *builder) selectArm(arm *ast.SelectArm) *Node {
	var n *Node
	switch arm.Kind {
	case ast.SelectRecv:
		n = New("recv-arm", nameOr(arm.RecvValue), b.symID(arm.RecvValue),
			nameOr(arm.RecvOk), b.symID(arm.RecvOk))
		n.Child(b.wrap("chan", b.expr(arm.Chan)))
	case ast.SelectSend:
		n = New("send-arm")
		n.Child(b.wrap("chan", b.expr(arm.Chan)))
		n.Child(b.wrap("value", b.expr(arm.Value)))
	case ast.SelectTimeout:
		n = New("timeout-arm")
		n.Child(b.wrap("dur", b.expr(arm.Value)))
	default:
		n = New("default-arm")
	}
	n.Child(b.wrap("body", b.stmt(arm.Body)))
	return b.at(n, arm.Sp)
}

// nameOr returns the name of an identifier, or an underscore.
func nameOr(id *ast.Ident) string {
	if id == nil || id.Name == "" {
		return "_"
	}
	return id.Name
}

// pattern renders a match pattern.
func (b *builder) pattern(p ast.Pattern) *Node {
	switch v := p.(type) {
	case nil:
		return nil
	case *ast.WildcardPattern:
		return b.at(New("pat-wild"), v.Sp)
	case *ast.BindPattern:
		if b.cpkg.PatternVariants[v] {
			return b.at(New("pat-variant", v.Name.Name), v.Sp)
		}
		n := New("pat-bind", v.Name.Name, b.symID(v.Name))
		if t := b.staticType(v.Name); t != nil {
			n.Child(New("type", b.typeText(t)))
		}
		return b.at(n, v.Sp)
	case *ast.LitPattern:
		return b.at(New("pat-lit", v.X.Value), v.Sp)
	case *ast.VariantPattern:
		n := New("pat-variant", v.Name.Name)
		for _, sub := range v.Payload {
			n.Child(b.pattern(sub))
		}
		return b.at(n, v.Sp)
	case *ast.TuplePattern:
		n := New("pat-tuple")
		for _, sub := range v.Elems {
			n.Child(b.pattern(sub))
		}
		return b.at(n, v.Sp)
	case *ast.OrPattern:
		n := New("pat-or")
		for _, sub := range v.Patterns {
			n.Child(b.pattern(sub))
		}
		return b.at(n, v.Sp)
	case *ast.RangePattern:
		n := New("pat-range")
		n.Child(b.wrap("lo", b.expr(v.Lo)))
		n.Child(b.wrap("hi", b.expr(v.Hi)))
		return b.at(n, v.Sp)
	}
	b.fail(p, "pattern "+nodeName(p))
	return nil
}

// staticType returns the static type of an expression, or nil when the
// expression produces no value. The format writes no annotation for a
// result-free expression, the same way a function with no result writes no
// `result` node.
func (b *builder) staticType(e ast.Node) *types.Type {
	if e == nil {
		return nil
	}
	t := b.cpkg.Nodes[e]
	if t == nil || t.Kind == types.Invalid {
		return nil
	}
	return t
}

// implSymbol returns the symbol id of the interface an impl block names. The
// interface is identified by its symbol, so a reader never has to match a
// spelling to find it.
func (b *builder) implSymbol(t ast.TypeExpr) string {
	if tn, ok := t.(*ast.TypeName); ok && tn.Name != nil {
		return b.symID(tn.Name)
	}
	return ""
}

// lambdaID returns the symbol id of a lambda, which is a function of its own
// and needs a name to be emitted under.
func (b *builder) lambdaID(v *ast.LambdaExpr) string {
	if sym := b.rinfo.Defs[v]; sym != nil {
		return sym.ID
	}
	return ""
}

// firstName returns the first name of a binding list, or nil when the list is
// empty.
func firstName(names []*ast.Ident) *ast.Ident {
	if len(names) == 0 {
		return nil
	}
	return names[0]
}

// declaredType returns the type a binding has: the type it writes, the type
// the checker recorded for its name, or the type its initializer has. A
// binding reaches the format with a settled type, so no later stage reads the
// initializer to find one.
func (b *builder) declaredType(declared ast.TypeExpr, name *ast.Ident, value ast.Expr) *types.Type {
	if declared != nil {
		return b.cpkg.Nodes[declared]
	}
	if name != nil {
		if t := b.staticType(name); t != nil {
			return t
		}
	}
	return b.staticType(value)
}

// annotate records the static type of an expression, which the backend
// consumes and never has to infer.
func (b *builder) annotate(n *Node, e ast.Expr) *Node {
	if n == nil {
		return n
	}
	if t := b.staticType(e); t != nil {
		n.Child(New("type", b.typeText(t)))
	}
	return n
}

// expr renders an expression and records its static type. The annotation
// settles every question the backend would otherwise have to answer by
// inference: the type of a literal, of a call whose callee carries no
// declaration, and of an unannotated binding.
func (b *builder) expr(e ast.Expr) *Node {
	return b.mark(b.annotate(b.exprNode(e), e), e)
}

// exprNode renders an expression without its type annotation.
func (b *builder) exprNode(e ast.Expr) *Node {
	switch v := e.(type) {
	case nil:
		return nil
	case *ast.Ident:
		if v.Name == "self" {
			return b.at(New("name", "self", b.symID(v)), v.Sp)
		}
		return b.at(New("name", v.Name, b.symID(v)), v.Sp)
	case *ast.Literal:
		return b.literalNode(v)
	case *ast.BinaryExpr:
		n := New("binary", v.Op.String())
		n.Child(b.wrap("lhs", b.expr(v.X)))
		n.Child(b.wrap("rhs", b.expr(v.Y)))
		return b.at(n, v.Sp)
	case *ast.UnaryExpr:
		n := New("unary", v.Op.String()).Child(b.wrap("operand", b.expr(v.X)))
		return b.at(n, v.Sp)
	case *ast.AddrOfExpr:
		n := New("addr").Child(b.wrap("operand", b.expr(v.X)))
		return b.at(n, v.Sp)
	case *ast.IsExpr:
		n := New("is", b.typeText(b.cpkg.Nodes[v.Type])).Child(b.wrap("operand", b.expr(v.X)))
		return b.at(n, v.Sp)
	case *ast.CallExpr:
		if n := b.callNode(v); n != nil {
			return n
		}
		if b.isPanic(v.Fn) {
			n := New("panic").Child(b.wrap("msg", b.expr(firstArg(v))))
			return b.at(n, v.Sp)
		}
		n := New("call")
		n.Child(b.wrap("callee", b.expr(v.Fn)))
		n.Child(b.args(v.Args)...)
		return b.at(n, v.Sp)
	case *ast.SelectorExpr:
		return b.selector(v)
	case *ast.TupleIndexExpr:
		n := New("tindex", strconv.Itoa(v.Index)).Child(b.wrap("recv", b.expr(v.X)))
		return b.at(n, v.Sp)
	case *ast.IndexExpr:
		if r, ok := v.Index.(*ast.RangeExpr); ok {
			n := New("slice")
			n.Child(b.wrap("recv", b.expr(v.X)))
			n.Child(b.wrap("lo", b.expr(r.Lo)))
			n.Child(b.wrap("hi", b.expr(r.Hi)))
			return b.at(n, v.Sp)
		}
		n := New("index")
		n.Child(b.wrap("recv", b.expr(v.X)))
		n.Child(b.wrap("idx", b.expr(v.Index)))
		return b.at(n, v.Sp)
	case *ast.RangeExpr:
		n := New("range")
		n.Child(b.wrap("lo", b.expr(v.Lo)))
		n.Child(b.wrap("hi", b.expr(v.Hi)))
		return b.at(n, v.Sp)
	case *ast.NewExpr:
		n := New("new", b.typeText(b.cpkg.Nodes[v.Type]))
		for _, a := range v.Args {
			n.Child(b.wrap("arg", b.expr(a)))
		}
		return b.at(n, v.Sp)
	case *ast.TryExpr:
		n := New("try").Child(b.wrap("call", b.expr(v.X)))
		return b.at(n, v.Sp)
	case *ast.SpawnExpr:
		n := New("spawn").Child(b.wrap("call", b.expr(v.X)))
		if v.Watcher != nil {
			n.Mark("watched", "")
			n.Child(b.wrap("watcher", b.expr(v.Watcher)))
		}
		return b.at(n, v.Sp)
	case *ast.FastExpr:
		n := New("c-call", "")
		if call, ok := v.X.(*ast.CallExpr); ok {
			n.Payload = []string{cName(call.Fn)}
			for _, a := range call.Args {
				n.Child(b.wrap("arg", b.expr(a)))
			}
			return b.at(n, call.Sp)
		}
		return b.expr(v.X)
	case *ast.FuncRefExpr:
		n := New("func-ref", b.symID(v.Name))
		return b.at(n, v.Sp)
	case *ast.LambdaExpr:
		n := New("lambda", b.lambdaID(v))
		for _, p := range v.Params {
			pn := New("param", nameOr(p.Name), b.typeText(b.cpkg.Nodes[p.Type]))
			n.Child(b.at(pn, p.Sp))
		}
		if v.Result != nil {
			n.Child(New("result", b.typeText(b.cpkg.Nodes[v.Result])))
		}
		if v.Body != nil {
			n.Child(b.wrap("body", b.block(v.Body)))
		} else if v.BodyExpr != nil {
			n.Child(b.expr(v.BodyExpr))
		}
		return b.at(n, v.Sp)
	case *ast.TupleExpr:
		n := New("tuple-lit")
		for _, x := range v.Elems {
			n.Child(b.wrap("elem", b.expr(x)))
		}
		return b.at(n, v.Sp)
	case *ast.ArrayLitExpr:
		n := New("array-lit")
		for _, x := range v.Elems {
			n.Child(b.wrap("elem", b.expr(x)))
		}
		return b.at(n, v.Sp)
	case *ast.ArrayRepeatExpr:
		n := New("array-repeat")
		n.Child(b.wrap("elem", b.expr(v.Elem)))
		n.Child(b.wrap("count", b.expr(v.Count)))
		return b.at(n, v.Sp)
	case *ast.AggregateLitExpr:
		return b.aggregate(v)
	}
	b.fail(e, "expression "+nodeName(e))
	return nil
}

// cName returns the C name a C call reaches.
func cName(fn ast.Expr) string {
	if sel, ok := fn.(*ast.SelectorExpr); ok && sel.Name != nil {
		return sel.Name.Name
	}
	return ""
}

// namespace returns the predeclared namespace a selector reaches, such as
// meta or dur, or the empty string.
func (b *builder) namespace(v *ast.SelectorExpr) string {
	id := qualifierName(v)
	if id == nil {
		return ""
	}
	sym := b.rinfo.Defs[id]
	if sym == nil {
		return ""
	}
	// The two comptime namespaces are predeclared names, so they carry the
	// builtin kind; an imported package carries the package kind.
	if sym.Kind != resolve.KindPackage && sym.Kind != resolve.KindBuiltin {
		return ""
	}
	switch sym.Name {
	case "meta", "dur":
		return sym.Name
	}
	return ""
}

// isJoin reports whether a selector names one of the operations the language
// builds into a coroutine handle.
func (b *builder) isJoin(v *ast.SelectorExpr) bool {
	recv := b.cpkg.Nodes[v.X]
	if recv == nil || recv.ID != types.JoinHandle.ID {
		return false
	}
	switch v.Name.Name {
	case "Join", "TryJoin":
		return true
	}
	return false
}

// isPanic reports whether a callee names the built-in panic.
func (b *builder) isPanic(fn ast.Expr) bool {
	id, ok := fn.(*ast.Ident)
	if !ok {
		return false
	}
	sym := b.rinfo.Defs[id]
	return sym != nil && sym.Kind == resolve.KindBuiltin && sym.Name == "panic"
}

// instanceMark returns the mark that records the type arguments a call
// supplies to a generic declaration. Matching the instantiated signature the
// checker recorded against the declaration's own settles them exactly, which
// is what the monomorphizer needs and what it would otherwise have to
// re-derive from the arguments.
func (b *builder) instanceMark(id *ast.Ident) string {
	if id == nil {
		return ""
	}
	got := b.staticType(id)
	if got == nil || got.Kind != types.KindFunc {
		return ""
	}
	declared := b.cpkg.Decls[id.Name]
	if declared == nil || declared.Kind != types.KindFunc || len(declared.Elems) == 0 {
		return ""
	}
	bindings, ok := types.Bindings(declared, got)
	if !ok {
		return ""
	}
	mark := "instance " + b.symID(id)
	for _, param := range declared.Elems {
		if param.Kind != types.KindTypeParam {
			continue
		}
		bound, ok := bindings[param.ID]
		if !ok {
			return ""
		}
		mark += " " + b.typeText(bound)
	}
	return mark
}

// calleeTypeNode returns the `callee-type` child of a call, which records
// the instantiated signature of the callee. A generic call is settled by
// matching that signature against the declaration's own.
func (b *builder) calleeTypeNode(id *ast.Ident) *Node {
	if id == nil {
		return nil
	}
	if t := b.staticType(id); t != nil {
		return New("callee-type", b.typeText(t))
	}
	return nil
}

// args renders a call's arguments.
func (b *builder) args(list []ast.Expr) []*Node {
	out := make([]*Node, 0, len(list))
	for _, a := range list {
		out = append(out, b.wrap("arg", b.expr(a)))
	}
	return out
}

// callNode renders a call whose callee has a node shape of its own: a method
// call, a qualified call, a C call, a variant construction, or a conversion.
func (b *builder) callNode(v *ast.CallExpr) *Node {
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
		switch {
		case b.isCCall(sel):
			n := New("c-call", sel.Name.Name)
			n.Child(b.args(v.Args)...)
			return b.at(n, v.Sp)
		case b.namespace(sel) != "":
			// meta.size_of(T) and dur.ms(100) are their own node kinds,
			// because the backend settles them without a call.
			n := New(b.namespace(sel), sel.Name.Name)
			n.Child(b.args(v.Args)...)
			return b.at(n, v.Sp)
		case b.isJoin(sel):
			n := New("join", sel.Name.Name)
			n.Child(b.wrap("recv", b.expr(sel.X)))
			return b.at(n, v.Sp)
		case b.isVariant(sel):
			n := New("variant-lit", b.typeText(b.staticType(v)), sel.Name.Name)
			n.Child(b.args(v.Args)...)
			return b.at(n, v.Sp)
		case b.isMethodCall(sel):
			n := New("mcall", sel.Name.Name, b.memberID(sel.X, sel.Name.Name))
			n.Child(b.wrap("recv", b.expr(sel.X)))
			n.Child(b.args(v.Args)...)
			if child := b.calleeTypeNode(sel.Name); child != nil {
				n.Child(child)
			}
			return b.at(n, v.Sp)
		case b.isQualified(sel):
			n := New("qual", nameOr(qualifierName(sel)), sel.Name.Name, b.symID(sel.Name))
			n.Child(b.args(v.Args)...)
			if child := b.calleeTypeNode(sel.Name); child != nil {
				n.Child(child)
			}
			return b.at(n, v.Sp)
		}
		return nil
	}
	if id, ok := v.Fn.(*ast.Ident); ok && b.isTypeName(id) {
		n := New("convert", b.typeText(b.cpkg.Nodes[id]))
		n.Child(b.wrap("operand", b.expr(firstArg(v))))
		return b.at(n, v.Sp)
	}
	return nil
}

// firstArg returns the first argument of a conversion.
func firstArg(v *ast.CallExpr) ast.Expr {
	if len(v.Args) == 0 {
		return nil
	}
	return v.Args[0]
}

// literalNode renders a literal. The kind is written out, so the backend
// never has to recover it from the type annotation. An interpolated string
// keeps every segment in order, so the text can be rebuilt without guessing
// where the holes were.
func (b *builder) literalNode(lit *ast.Literal) *Node {
	kind := literalKind(lit)
	if kind == "fstring" {
		n := New("lit", kind, lit.Value)
		for _, part := range lit.Parts {
			if part.X == nil {
				n.Child(New("part-text", part.Text))
				continue
			}
			n.Child(New("part-expr").Child(b.expr(part.X)))
		}
		return b.at(n, lit.Sp)
	}
	return b.at(New("lit", kind, lit.Value), lit.Sp)
}

// literalKind returns the spelling the format writes for a literal's kind.
func literalKind(lit *ast.Literal) string {
	switch lit.Kind.String() {
	case "int":
		return "int"
	case "float":
		return "float"
	case "str":
		return "str"
	case "char":
		return "char"
	case "bytestring":
		return "bytes"
	case "fstring":
		return "fstring"
	case "true", "false":
		return "bool"
	}
	return "invalid"
}

// selector renders a field access, a method value, a qualified name, a
// variant construction, or a channel operation.
func (b *builder) selector(v *ast.SelectorExpr) *Node {
	switch {
	case b.isQualified(v):
		n := New("qual", nameOr(qualifierName(v)), v.Name.Name, b.symID(v.Name))
		return b.at(n, v.Sp)
	case b.isCCall(v):
		return b.at(New("c-call", v.Name.Name), v.Sp)
	case b.isVariant(v):
		return b.at(New("variant-lit", b.typeText(b.staticType(v)), v.Name.Name), v.Sp)
	case b.isMethodCall(v):
		// In expression position a method names a bound method value. A call
		// is rendered by callNode, which carries the arguments.
		n := New("method-value", v.Name.Name, b.memberID(v.X, v.Name.Name))
		n.Child(b.wrap("recv", b.expr(v.X)))
		return b.at(n, v.Sp)
	case b.isFieldAccess(v):
		n := New("field", v.Name.Name, b.memberID(v.X, v.Name.Name))
		n.Child(b.wrap("recv", b.expr(v.X)))
		return b.at(n, v.Sp)
	}
	// A selector that names no package, namespace, variant, method, or field
	// has no symbol to carry, so emitting it would leave the backend a name it
	// cannot resolve.
	b.fail(v, "the selector "+v.Name.Name)
	return nil
}

// memberID returns the symbol id of a member of a receiver's type. A method
// the language builds in has no declaration, so its id stays empty and the
// backend pairs the name with the receiver's type.
func (b *builder) memberID(recv ast.Expr, name string) string {
	t := b.cpkg.Nodes[recv]
	if t == nil {
		return ""
	}
	d := b.cpkg.Decl(t)
	if d == nil || d.Sym == nil {
		return ""
	}
	if m := d.Sym.Member(name); m != nil {
		return m.ID
	}
	return ""
}

// qualifierName returns the package name of a qualified selector.
func qualifierName(v *ast.SelectorExpr) *ast.Ident {
	if id, ok := v.X.(*ast.Ident); ok {
		return id
	}
	return nil
}

// isQualified reports whether a selector names a declaration of an imported
// package.
func (b *builder) isQualified(v *ast.SelectorExpr) bool {
	id := qualifierName(v)
	if id == nil {
		return false
	}
	sym := b.rinfo.Defs[id]
	return sym != nil && sym.Kind == resolve.KindPackage && sym.Name != "c"
}

// isCCall reports whether a selector reaches the C namespace.
func (b *builder) isCCall(v *ast.SelectorExpr) bool {
	id := qualifierName(v)
	if id == nil {
		return false
	}
	sym := b.rinfo.Defs[id]
	return sym != nil && sym.Kind == resolve.KindPackage && sym.Name == "c"
}

// isVariant reports whether a selector constructs an enum variant. The
// qualifier must name a type, so a value whose name matches a variant is not
// mistaken for one, and the enum is read from the variant name, which is the
// node the checker records it on.
func (b *builder) isVariant(v *ast.SelectorExpr) bool {
	id := qualifierName(v)
	if id == nil || !b.isTypeName(id) {
		return false
	}
	t := b.cpkg.Nodes[v.Name]
	if t == nil || t.Kind != types.KindEnum {
		return false
	}
	d := b.cpkg.Decl(t)
	return d != nil && d.Variant(v.Name.Name) != nil
}

// isTypeName reports whether an identifier names a type. The predeclared
// names split in two: the type names name types, while the two comptime
// namespaces and the global function do not.
func (b *builder) isTypeName(id *ast.Ident) bool {
	sym := b.rinfo.Defs[id]
	if sym == nil {
		return false
	}
	switch sym.Kind {
	case resolve.KindType, resolve.KindTypeParam, resolve.KindCSymbol:
		return true
	case resolve.KindBuiltin:
		switch sym.Name {
		case "meta", "dur", "panic":
			return false
		}
		return true
	}
	return false
}

// isMethodCall reports whether the receiver's type declares the method,
// whether the declaration is in source or built into the language.
func (b *builder) isMethodCall(v *ast.SelectorExpr) bool {
	recv := b.cpkg.Nodes[v.X]
	if recv == nil {
		return false
	}
	return b.cpkg.MethodOf(recv, v.Name.Name) != nil
}

// isFieldAccess reports whether a selector names a field of the receiver.
func (b *builder) isFieldAccess(v *ast.SelectorExpr) bool {
	recv := b.cpkg.Nodes[v.X]
	if recv == nil {
		return false
	}
	d := b.cpkg.Decl(recv)
	if d == nil {
		return false
	}
	return d.Field(v.Name.Name) != nil
}

// aggregate renders a construction literal.
func (b *builder) aggregate(v *ast.AggregateLitExpr) *Node {
	typ := b.cpkg.Nodes[v]
	kind := "struct-lit"
	if typ != nil && typ.Kind == types.KindContainer {
		switch typ.Name {
		case "Map":
			kind = "map-lit"
		case "Set":
			kind = "set-lit"
		}
	}
	n := New(kind, b.typeText(typ))
	for _, entry := range v.Entries {
		switch kind {
		case "map-lit":
			e := New("entry")
			if entry.Key != nil {
				e.Child(b.wrap("key", b.expr(entry.Key)))
			}
			e.Child(b.wrap("value", b.expr(entry.Value)))
			n.Child(e)
		case "set-lit", "array-lit":
			n.Child(b.wrap("elem", b.expr(entry.Value)))
		default:
			name := ""
			if id, ok := entry.Key.(*ast.Ident); ok {
				name = id.Name
			}
			e := New("field-init", name)
			e.Child(b.wrap("value", b.expr(entry.Value)))
			n.Child(e)
		}
	}
	return b.at(n, v.Sp)
}
