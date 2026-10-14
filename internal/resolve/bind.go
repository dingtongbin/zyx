// Binding of every name occurrence to the declaration it names.

package resolve

import (
	"zyx/internal/ast"
	"zyx/internal/diag"
)

// resolveExpr binds every name in an expression.
func (r *resolver) resolveExpr(e ast.Expr) {
	switch v := e.(type) {
	case nil:
		return
	case *ast.Ident:
		r.bindName(v)
	case *ast.Literal:
		for _, p := range v.Parts {
			r.resolveExpr(p.X)
		}
	case *ast.BinaryExpr:
		r.resolveExpr(v.X)
		r.resolveExpr(v.Y)
	case *ast.UnaryExpr:
		r.resolveExpr(v.X)
	case *ast.AddrOfExpr:
		r.resolveExpr(v.X)
	case *ast.IsExpr:
		r.resolveExpr(v.X)
		r.resolveType(v.Type)
	case *ast.CallExpr:
		r.resolveExpr(v.Fn)
		for _, a := range v.Args {
			r.resolveExpr(a)
		}
	case *ast.SelectorExpr:
		r.resolveSelector(v)
	case *ast.TupleIndexExpr:
		r.resolveExpr(v.X)
	case *ast.IndexExpr:
		r.resolveExpr(v.X)
		r.resolveExpr(v.Index)
	case *ast.RangeExpr:
		r.resolveExpr(v.Lo)
		r.resolveExpr(v.Hi)
	case *ast.NewExpr:
		r.resolveType(v.Type)
		for _, a := range v.Args {
			r.resolveExpr(a)
		}
	case *ast.TryExpr:
		r.resolveExpr(v.X)
	case *ast.SpawnExpr:
		r.resolveExpr(v.X)
		r.resolveExpr(v.Watcher)
	case *ast.FuncRefExpr:
		r.resolveFuncRef(v)
	case *ast.LambdaExpr:
		r.resolveLambda(v)
	case *ast.TupleExpr:
		for _, x := range v.Elems {
			r.resolveExpr(x)
		}
	case *ast.ArrayLitExpr:
		for _, x := range v.Elems {
			r.resolveExpr(x)
		}
	case *ast.ArrayRepeatExpr:
		r.resolveExpr(v.Elem)
		r.resolveExpr(v.Count)
	case *ast.AggregateLitExpr:
		r.resolveAggregate(v)
	case *ast.FastExpr:
		r.resolveExpr(v.X)
	}
}

// resolveSelector binds a qualified name (`net.Listen`, `c.printf`) or
// leaves a member access for the type checker, which needs the receiver's
// type to pick the field or method.
func (r *resolver) resolveSelector(v *ast.SelectorExpr) {
	if id, ok := v.X.(*ast.Ident); ok {
		switch id.Name {
		case "c":
			r.resolveCMember(id, v.Name)
			return
		case "meta", "dur":
			r.bindName(id)
			return
		}
		if pkg := r.qualifier(id.Name); pkg != nil {
			r.resolveQualMember(pkg, id, v.Name)
			return
		}
	}
	r.resolveExpr(v.X)
}

// qualifier returns the imported package that name refers to here, or nil.
// A local binding of the same name hides the qualifier, which is allowed
// because only package-level names participate in the shadowing ban.
func (r *resolver) qualifier(name string) *Symbol {
	if sym := r.scope.lookup(name); sym != nil && sym.Kind == KindPackage {
		return sym
	}
	return nil
}

// resolveQualMember binds `pkg.Name` against the imported package's public
// surface.
func (r *resolver) resolveQualMember(pkg *Symbol, qual, name *ast.Ident) {
	r.info.Defs[qual] = pkg
	if pkg.Name == "c" {
		r.resolveCMember(qual, name)
		return
	}
	use := r.file.importOf(pkg)
	if use == nil {
		return
	}
	use.used = true
	if name == nil {
		return
	}
	member := use.itf.Decls[name.Name]
	if member == nil {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, name.Sp,
			"package %s has no declaration named %q", pkg.Name, name.Name).
			WithHelp("check the name, or the version of the package"))
		return
	}
	r.checkCrossPackage(member, name, pkg.Name)
	r.info.Defs[name] = member
}

// resolveCMember binds `c.Name`. The C symbol table comes from clang when
// the package's C headers are processed, so resolution accepts any member
// here and only checks that a C header was imported.
func (r *resolver) resolveCMember(qual, name *ast.Ident) {
	if r.file == nil || r.file.c == nil {
		r.diags = append(r.diags, diag.New(diag.CodeCNamespace, qual.Sp,
			"the c namespace is only available after importing a C header").
			WithHelp(`add an import such as import c "stdio.h"`))
		return
	}
	r.info.Defs[qual] = r.file.c
	r.file.cUsed = true
	if name != nil {
		r.info.Defs[name] = r.cSymbol(name.Name)
	}
}

// cSymbol returns the shared symbol for one name of an imported C header.
func (r *resolver) cSymbol(name string) *Symbol {
	if r.cSyms == nil {
		r.cSyms = map[string]*Symbol{}
	}
	if sym, ok := r.cSyms[name]; ok {
		return sym
	}
	sym := &Symbol{ID: "c." + name, Name: name, Kind: KindCSymbol, Public: true}
	r.cSyms[name] = sym
	return sym
}

// resolveFuncRef binds `func Name` and `func pkg.Name`.
func (r *resolver) resolveFuncRef(v *ast.FuncRefExpr) {
	if v.Name == nil {
		return
	}
	if v.Qualifier != nil {
		pkg := r.scope.lookup(v.Qualifier.Name)
		if pkg == nil || pkg.Kind != KindPackage {
			r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, v.Qualifier.Sp,
				"undefined package %q", v.Qualifier.Name).
				WithHelp("import the package before referencing its functions"))
			return
		}
		r.info.Defs[v.Qualifier] = pkg
		if pkg.Name == "c" {
			r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, v.Name.Sp,
				"a C symbol is not a function value").
				WithHelp("call it directly as "+"c."+v.Name.Name+"(...)"))
			return
		}
		use := r.file.importOf(pkg)
		if use == nil {
			return
		}
		use.used = true
		member := use.itf.Decls[v.Name.Name]
		if member == nil {
			r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, v.Name.Sp,
				"package %s has no declaration named %q", pkg.Name, v.Name.Name).
				WithHelp("check the name, or the version of the package"))
			return
		}
		r.checkCrossPackage(member, v.Name, pkg.Name)
		r.info.Defs[v.Name] = member
		return
	}
	sym := r.bindName(v.Name)
	if sym == nil {
		return
	}
	if sym.Kind != KindFunc && sym.Kind != KindMethod {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, v.Name.Sp,
			"%q is a %s, not a function", v.Name.Name, sym.Kind).
			WithHelp("name a function here"))
	}
}

// resolveAggregate binds a construction literal: the type name at its head
// and the entry expressions. A bare identifier key is a field name when the
// literal constructs a struct or union and an ordinary expression when it
// constructs a container, and resolution cannot tell the two apart without
// the type. It binds such a key only when the name is already in scope, and
// the checker validates the field names against the type.
func (r *resolver) resolveAggregate(v *ast.AggregateLitExpr) {
	r.resolveType(v.Type)
	for _, e := range v.Entries {
		if e.Key != nil {
			if id, ok := e.Key.(*ast.Ident); ok {
				if r.scope.lookup(id.Name) != nil {
					r.resolveExpr(id)
				}
			} else {
				r.resolveExpr(e.Key)
			}
		}
		r.resolveExpr(e.Value)
	}
}

// resolveLambda binds a lambda. The lambda is a function symbol of its own,
// numbered after the enclosing function's locals; its body may not read any
// binding of the enclosing function.
func (r *resolver) resolveLambda(v *ast.LambdaExpr) {
	r.localN++
	sym := &Symbol{
		ID:    localID(r.funcSym, r.localN),
		Name:  "func",
		Kind:  KindFunc,
		Decl:  v,
		Owner: r.funcSym,
	}
	r.add(sym)
	r.info.Defs[v] = sym

	fs := newScope(r.scope, scopeFunc)
	prevScope, prevFunc, prevSelf := r.scope, r.funcSym, r.self
	prevN, prevLabels, prevBase, prevExported := r.localN, r.labels, r.lambdaBase, r.exported
	r.scope, r.funcSym, r.self, r.localN = fs, sym, nil, 0
	r.labels, r.lambdaBase, r.exported = map[string]bool{}, fs, false

	for _, p := range v.Params {
		r.resolveType(p.Type)
		if p.Default != nil {
			r.resolveExpr(p.Default)
		}
	}
	for _, p := range v.Params {
		if p.Name != nil && p.Name.Name != "_" {
			r.bindLocal(p.Name, KindParam)
		}
	}
	r.resolveType(v.Result)
	if v.Body != nil {
		r.resolveStmt(v.Body)
	}
	if v.BodyExpr != nil {
		r.resolveExpr(v.BodyExpr)
	}

	r.scope, r.funcSym, r.self, r.localN = prevScope, prevFunc, prevSelf, prevN
	r.labels, r.lambdaBase, r.exported = prevLabels, prevBase, prevExported
}

// bindName resolves a bare name against the local scopes, the package, and
// the predeclared universe, in that order.
func (r *resolver) bindName(id *ast.Ident) *Symbol {
	if id == nil || id.Name == "" || id.Name == "_" {
		return nil
	}
	outside := false
	for s := r.scope; s != nil; s = s.parent {
		if sym, ok := s.names[id.Name]; ok {
			r.bindRef(id, sym, outside)
			r.checkMainUse(id, sym)
			return sym
		}
		if s == r.lambdaBase {
			outside = true
		}
	}
	if id.Name == "self" {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, id.Sp,
			"self is only available inside a method").
			WithHelp("declare a parameter for the value"))
		return nil
	}
	r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, id.Sp,
		"undefined name %q", id.Name).
		WithHelp("declare the name, or import the package that provides it"))
	return nil
}

// bindRef records the binding and reports a lambda that reads a binding of
// an enclosing function.
func (r *resolver) bindRef(id *ast.Ident, sym *Symbol, outsideLambda bool) {
	if outsideLambda && capturable(sym) {
		d := diag.New(diag.CodeCapture, id.Sp,
			"a lambda cannot read %q from the enclosing function", id.Name)
		if sym.Decl != nil {
			d = d.WithNote(sym.Decl.Span(), "%q is declared here", id.Name)
		}
		r.diags = append(r.diags, d.WithHelp("pass the value as a lambda parameter instead"))
	}
	r.info.Defs[id] = sym
}

// capturable reports whether a symbol is a function binding that a lambda
// may not read.
func capturable(sym *Symbol) bool {
	switch sym.Kind {
	case KindParam, KindLocal:
		return true
	}
	return sym.Name == "self"
}

// checkMainUse reports a reference to the program entry point, which only
// the runtime calls.
func (r *resolver) checkMainUse(id *ast.Ident, sym *Symbol) {
	if sym.Kind != KindFunc || sym.Name != "main" {
		return
	}
	r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, id.Sp,
		"main is the program entry point and cannot be referenced").
		WithHelp("call a helper function instead"))
}

// resolveType binds the names of a type expression.
func (r *resolver) resolveType(t ast.TypeExpr) {
	switch v := t.(type) {
	case nil:
		return
	case *ast.TypeName:
		r.resolveTypeName(v)
	case *ast.TupleType:
		for _, e := range v.Elems {
			r.resolveType(e)
		}
	case *ast.SliceType:
		r.resolveType(v.Elem)
	case *ast.ArrayType:
		r.resolveType(v.Elem)
		r.resolveExpr(v.Len)
	case *ast.FuncType:
		for _, p := range v.Params {
			r.resolveType(p)
		}
		r.resolveType(v.Result)
	case *ast.ChanType:
		r.resolveType(v.Elem)
	}
}

// resolveTypeName binds `Name`, `pkg.Name`, `c.Name`, `Self`, and `type`.
func (r *resolver) resolveTypeName(v *ast.TypeName) {
	if v.Qual != nil {
		r.resolveQualifiedType(v.Qual, v.Name)
	} else if v.Name != nil {
		switch v.Name.Name {
		case "Self":
			if r.typeSym == nil {
				r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, v.Name.Sp,
					"Self is only available inside a type body").
					WithHelp("name the type directly"))
			} else {
				r.info.Defs[v.Name] = r.typeSym
			}
		case "type":
			// The type value of a comptime context names no declaration.
		default:
			r.bindTypeName(v.Name)
		}
	}
	for _, a := range v.Args {
		r.resolveType(a)
	}
}

// bindTypeName binds a bare type name.
func (r *resolver) bindTypeName(id *ast.Ident) {
	sym := r.scope.lookup(id.Name)
	if sym == nil {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, id.Sp,
			"undefined type %q", id.Name).
			WithHelp("declare the type, or import the package that provides it"))
		return
	}
	if !isTypeKind(sym.Kind) {
		r.diags = append(r.diags, diag.New(diag.CodeNotAType, id.Sp,
			"%q is a %s, not a type", id.Name, sym.Kind).
			WithHelp("name a type here"))
		return
	}
	r.checkExportedType(sym, id)
	r.info.Defs[id] = sym
}

// resolveQualifiedType binds `pkg.Name` in type position.
func (r *resolver) resolveQualifiedType(qual, name *ast.Ident) {
	pkg := r.scope.lookup(qual.Name)
	if pkg == nil || pkg.Kind != KindPackage {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, qual.Sp,
			"undefined package %q", qual.Name).
			WithHelp("import the package before naming its types"))
		return
	}
	r.info.Defs[qual] = pkg
	if pkg.Name == "c" {
		r.resolveCMember(qual, name)
		return
	}
	use := r.file.importOf(pkg)
	if use == nil || name == nil {
		return
	}
	use.used = true
	member := use.itf.Decls[name.Name]
	if member == nil {
		r.diags = append(r.diags, diag.New(diag.CodeUndefinedName, name.Sp,
			"package %s has no declaration named %q", pkg.Name, name.Name).
			WithHelp("check the name, or the version of the package"))
		return
	}
	r.checkCrossPackage(member, name, pkg.Name)
	if member.Kind != KindType {
		r.diags = append(r.diags, diag.New(diag.CodeNotAType, name.Sp,
			"%q is a %s, not a type", name.Name, member.Kind).
			WithHelp("name a type here"))
		return
	}
	r.info.Defs[name] = member
}

// isTypeKind reports whether a symbol can stand in type position.
func isTypeKind(k SymbolKind) bool {
	switch k {
	case KindType, KindTypeParam, KindBuiltin, KindCSymbol:
		return true
	}
	return false
}

// resolveStmt binds every name in a statement.
func (r *resolver) resolveStmt(s ast.Stmt) {
	switch v := s.(type) {
	case nil:
		return
	case *ast.BlockStmt:
		if v == nil {
			return
		}
		r.push(scopeBlock)
		for _, x := range v.Stmts {
			r.resolveStmt(x)
		}
		r.pop()
	case *ast.ExprStmt:
		r.resolveExpr(v.X)
	case *ast.AssignStmt:
		r.resolveExpr(v.Target)
		r.resolveExpr(v.Value)
	case *ast.IfStmt:
		r.resolveExpr(v.Cond)
		r.resolveStmt(v.Then)
		r.resolveStmt(v.Else)
	case *ast.ForStmt:
		r.resolveFor(v)
	case *ast.BreakStmt:
		r.resolveLabel(v.Label)
	case *ast.ContinueStmt:
		r.resolveLabel(v.Label)
	case *ast.ReturnStmt:
		for _, e := range v.Values {
			r.resolveExpr(e)
		}
	case *ast.DeferStmt:
		r.resolveExpr(v.Call)
	case *ast.RegionStmt:
		r.resolveStmt(v.Body)
	case *ast.ComptimeBlockStmt:
		r.resolveStmt(v.Body)
	case *ast.MatchStmt:
		r.resolveMatch(v)
	case *ast.SelectStmt:
		r.resolveSelect(v)
	case *ast.VarDecl:
		r.resolveLocalVar(v)
	case *ast.ConstDecl:
		r.resolveLocalConst(v)
	}
}

// resolveFor binds a loop: the label first, then the iterable, then the
// iteration bindings, then the body.
func (r *resolver) resolveFor(v *ast.ForStmt) {
	r.push(scopeBlock)
	if v.Label != nil {
		r.declareLabel(v.Label)
	}
	r.resolveExpr(v.Iter)
	for _, b := range v.Bindings {
		r.bindLocal(b, KindLocal)
	}
	r.resolveStmt(v.Body)
	r.pop()
}

// declareLabel binds a loop label. Labels are unique within a function and
// reachable only from the loop they name. A label carries no symbol id: the
// .zyxc layer writes the source spelling, and label names are unique per
// function.
func (r *resolver) declareLabel(id *ast.Ident) {
	if id == nil {
		return
	}
	if r.labels[id.Name] {
		r.diags = append(r.diags, diag.New(diag.CodeLabelDuplicate, id.Sp,
			"label %q is already used in this function", id.Name).
			WithHelp("give the loops different labels"))
		return
	}
	r.labels[id.Name] = true
	r.scope.insertLabel(&Symbol{Name: id.Name, Kind: KindLabel, Decl: id, Owner: r.funcSym})
}

// resolveLabel checks a break or continue target.
func (r *resolver) resolveLabel(id *ast.Ident) {
	if id == nil {
		return
	}
	if r.scope.lookupLabel(id.Name) == nil {
		r.diags = append(r.diags, diag.New(diag.CodeLabelUndefined, id.Sp,
			"no enclosing loop is labeled %q", id.Name).
			WithHelp("label the loop, or drop the label"))
	}
}

// resolveMatch binds a match statement: the subject, then each arm with its
// own scope for the pattern bindings.
func (r *resolver) resolveMatch(v *ast.MatchStmt) {
	r.resolveExpr(v.Subject)
	for _, arm := range v.Arms {
		r.push(scopeBlock)
		r.resolvePattern(arm.Pattern, nil)
		r.resolveExpr(arm.Guard)
		r.resolveStmt(arm.Body)
		r.pop()
	}
}

// resolveSelect binds a select statement: each arm owns the bindings of its
// receive.
func (r *resolver) resolveSelect(v *ast.SelectStmt) {
	for _, arm := range v.Arms {
		r.push(scopeBlock)
		r.resolveExpr(arm.Chan)
		r.resolveExpr(arm.Value)
		r.bindLocal(arm.RecvValue, KindLocal)
		r.bindLocal(arm.RecvOk, KindLocal)
		r.resolveStmt(arm.Body)
		r.pop()
	}
}

// resolveLocalVar binds a local variable declaration. The initializer is
// resolved first, so `var x = x` reads the outer x.
func (r *resolver) resolveLocalVar(v *ast.VarDecl) {
	r.resolveType(v.Type)
	r.resolveExpr(v.Value)
	for _, n := range v.Names {
		r.bindLocal(n, KindLocal)
	}
}

// resolveLocalConst binds a local constant declaration.
func (r *resolver) resolveLocalConst(v *ast.ConstDecl) {
	r.resolveType(v.Type)
	r.resolveExpr(v.Value)
	r.bindLocal(v.Name, KindConst)
}

// resolvePattern binds the names a pattern introduces. shared, when non-nil,
// maps the names an or-pattern binds once to the symbols all alternatives
// share.
func (r *resolver) resolvePattern(p ast.Pattern, shared map[string]*Symbol) {
	switch v := p.(type) {
	case nil:
		return
	case *ast.LitPattern:
		// A literal binds nothing.
	case *ast.WildcardPattern:
	case *ast.BindPattern:
		if v.Name == nil || v.Name.Name == "_" {
			return
		}
		if sym := shared[v.Name.Name]; sym != nil {
			r.info.Defs[v.Name] = sym
			return
		}
		r.bindLocal(v.Name, KindLocal)
	case *ast.VariantPattern:
		// The variant is named without a prefix and belongs to the matched
		// type, so the checker binds it.
		for _, x := range v.Payload {
			r.resolvePattern(x, shared)
		}
	case *ast.TuplePattern:
		for _, x := range v.Elems {
			r.resolvePattern(x, shared)
		}
	case *ast.OrPattern:
		r.resolveOr(v)
	case *ast.RangePattern:
		r.resolveExpr(v.Lo)
		r.resolveExpr(v.Hi)
	}
}

// resolveOr binds an or-pattern. Every alternative must bind the same
// names, and each name is bound once for the whole arm.
func (r *resolver) resolveOr(v *ast.OrPattern) {
	if len(v.Patterns) == 0 {
		return
	}
	names, first := patternBindings(v.Patterns[0])
	for _, alt := range v.Patterns[1:] {
		got, _ := patternBindings(alt)
		if !sameNameSet(names, got) {
			r.diags = append(r.diags, diag.New(diag.CodeDuplicateName, alt.Span(),
				"every alternative of an or-pattern must bind the same names").
				WithHelp("bind the same names in every alternative"))
			break
		}
	}
	shared := make(map[string]*Symbol, len(names))
	for _, name := range names {
		shared[name] = r.bindLocal(first[name], KindLocal)
	}
	for _, alt := range v.Patterns {
		r.resolvePattern(alt, shared)
	}
}

// patternBindings returns the names a pattern binds, in source order, with
// the identifier that first declares each.
func patternBindings(p ast.Pattern) ([]string, map[string]*ast.Ident) {
	var names []string
	first := map[string]*ast.Ident{}
	var visit func(ast.Pattern)
	visit = func(p ast.Pattern) {
		switch v := p.(type) {
		case *ast.BindPattern:
			if v.Name == nil || v.Name.Name == "_" {
				return
			}
			if _, ok := first[v.Name.Name]; !ok {
				names = append(names, v.Name.Name)
				first[v.Name.Name] = v.Name
			}
		case *ast.VariantPattern:
			for _, x := range v.Payload {
				visit(x)
			}
		case *ast.TuplePattern:
			for _, x := range v.Elems {
				visit(x)
			}
		case *ast.OrPattern:
			for _, x := range v.Patterns {
				visit(x)
			}
		}
	}
	visit(p)
	return names, first
}

// push opens a nested scope.
func (r *resolver) push(kind scopeKind) { r.scope = newScope(r.scope, kind) }

// pop closes the innermost scope.
func (r *resolver) pop() { r.scope = r.scope.parent }
