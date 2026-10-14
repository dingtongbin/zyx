// Depth-first traversal of the syntax tree.

package ast

// Walk visits n and its children in depth-first pre-order. f is called for
// every node; when it returns false, the children of that node are
// skipped. Nil children are skipped.
func Walk(n Node, f func(Node) bool) {
	if n == nil {
		return
	}
	if !f(n) {
		return
	}
	walk(n, f)
}

// walk dispatches on the concrete node type. Every node type must appear
// here; a type without children simply returns.
func walk(n Node, f func(Node) bool) {
	switch v := n.(type) {
	// Leaves.
	case *Ident, *Literal, *ImportDecl, *WildcardPattern:
		return

	// Files.
	case *File:
		if v.Package != nil {
			Walk(v.Package, f)
		}
		for _, imp := range v.Imports {
			Walk(imp, f)
		}
		walkDecls(v.Decls, f)

	// Declarations.
	case *PackageDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
	case *ConstDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		Walk(v.Type, f)
		Walk(v.Value, f)
	case *VarDecl:
		for _, name := range v.Names {
			Walk(name, f)
		}
		Walk(v.Type, f)
		Walk(v.Value, f)
	case *TypeAlias:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		Walk(v.Type, f)
	case *FuncDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		walkTypeParams(v.TypeParams, f)
		for _, p := range v.Params {
			Walk(p, f)
		}
		Walk(v.Result, f)
		if v.Body != nil {
			Walk(v.Body, f)
		}
		Walk(v.BodyExpr, f)
	case *FieldDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		Walk(v.Type, f)
	case *StructDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		walkTypeParams(v.TypeParams, f)
		walkDecls(v.Members, f)
	case *UnionDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, fld := range v.Fields {
			Walk(fld, f)
		}
	case *VariantDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, t := range v.Payload {
			Walk(t, f)
		}
	case *EnumDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		walkTypeParams(v.TypeParams, f)
		walkDecls(v.Members, f)
	case *InterfaceDecl:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		walkTypeParams(v.TypeParams, f)
		for _, m := range v.Methods {
			Walk(m, f)
		}
	case *ImplBlock:
		walkTypeParams(v.TypeParams, f)
		Walk(v.Interface, f)
		for _, m := range v.Methods {
			Walk(m, f)
		}
	case *Param:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		Walk(v.Type, f)
		Walk(v.Default, f)
	case *TypeParam:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, c := range v.Constraints {
			Walk(c, f)
		}

	// Statements.
	case *BlockStmt:
		for _, s := range v.Stmts {
			Walk(s, f)
		}
	case *ExprStmt:
		Walk(v.X, f)
	case *AssignStmt:
		Walk(v.Target, f)
		Walk(v.Value, f)
	case *IfStmt:
		Walk(v.Cond, f)
		Walk(v.Then, f)
		Walk(v.Else, f)
	case *ForStmt:
		if v.Label != nil {
			Walk(v.Label, f)
		}
		for _, b := range v.Bindings {
			Walk(b, f)
		}
		Walk(v.Iter, f)
		Walk(v.Body, f)
	case *BreakStmt:
		if v.Label != nil {
			Walk(v.Label, f)
		}
	case *ContinueStmt:
		if v.Label != nil {
			Walk(v.Label, f)
		}
	case *ReturnStmt:
		for _, e := range v.Values {
			Walk(e, f)
		}
	case *DeferStmt:
		Walk(v.Call, f)
	case *RegionStmt:
		if v.Body != nil {
			Walk(v.Body, f)
		}
	case *ComptimeBlockStmt:
		if v.Body != nil {
			Walk(v.Body, f)
		}
	case *MatchStmt:
		Walk(v.Subject, f)
		for _, a := range v.Arms {
			Walk(a, f)
		}
	case *MatchArm:
		Walk(v.Pattern, f)
		Walk(v.Guard, f)
		Walk(v.Body, f)
	case *SelectStmt:
		for _, a := range v.Arms {
			Walk(a, f)
		}
	case *SelectArm:
		Walk(v.Chan, f)
		Walk(v.Value, f)
		if v.RecvValue != nil {
			Walk(v.RecvValue, f)
		}
		if v.RecvOk != nil {
			Walk(v.RecvOk, f)
		}
		Walk(v.Body, f)

	// Expressions.
	case *BinaryExpr:
		Walk(v.X, f)
		Walk(v.Y, f)
	case *UnaryExpr:
		Walk(v.X, f)
	case *AddrOfExpr:
		Walk(v.X, f)
	case *IsExpr:
		Walk(v.X, f)
		Walk(v.Type, f)
	case *CallExpr:
		Walk(v.Fn, f)
		for _, a := range v.Args {
			Walk(a, f)
		}
	case *SelectorExpr:
		Walk(v.X, f)
		if v.Name != nil {
			Walk(v.Name, f)
		}
	case *TupleIndexExpr:
		Walk(v.X, f)
	case *IndexExpr:
		Walk(v.X, f)
		Walk(v.Index, f)
	case *RangeExpr:
		Walk(v.Lo, f)
		Walk(v.Hi, f)
	case *NewExpr:
		Walk(v.Type, f)
		for _, a := range v.Args {
			Walk(a, f)
		}
	case *TryExpr:
		Walk(v.X, f)
	case *SpawnExpr:
		Walk(v.X, f)
		Walk(v.Watcher, f)
	case *FuncRefExpr:
		if v.Qualifier != nil {
			Walk(v.Qualifier, f)
		}
		if v.Name != nil {
			Walk(v.Name, f)
		}
	case *LambdaExpr:
		for _, p := range v.Params {
			Walk(p, f)
		}
		Walk(v.Result, f)
		if v.Body != nil {
			Walk(v.Body, f)
		}
		Walk(v.BodyExpr, f)
	case *TupleExpr:
		for _, e := range v.Elems {
			Walk(e, f)
		}
	case *ArrayLitExpr:
		for _, e := range v.Elems {
			Walk(e, f)
		}
	case *ArrayRepeatExpr:
		Walk(v.Elem, f)
		Walk(v.Count, f)
	case *AggregateLitExpr:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, e := range v.Entries {
			Walk(e, f)
		}
	case *AggregateEntry:
		Walk(v.Key, f)
		Walk(v.Value, f)
	case *FastExpr:
		Walk(v.X, f)

	// Type expressions.
	case *TypeName:
		if v.Qual != nil {
			Walk(v.Qual, f)
		}
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, a := range v.Args {
			Walk(a, f)
		}
	case *TupleType:
		for _, e := range v.Elems {
			Walk(e, f)
		}
	case *SliceType:
		Walk(v.Elem, f)
	case *ArrayType:
		Walk(v.Elem, f)
		Walk(v.Len, f)
	case *FuncType:
		for _, p := range v.Params {
			Walk(p, f)
		}
		Walk(v.Result, f)
	case *ChanType:
		Walk(v.Elem, f)

	// Patterns.
	case *LitPattern:
		Walk(v.X, f)
	case *BindPattern:
		if v.Name != nil {
			Walk(v.Name, f)
		}
	case *VariantPattern:
		if v.Name != nil {
			Walk(v.Name, f)
		}
		for _, p := range v.Payload {
			Walk(p, f)
		}
	case *TuplePattern:
		for _, e := range v.Elems {
			Walk(e, f)
		}
	case *OrPattern:
		for _, p := range v.Patterns {
			Walk(p, f)
		}
	case *RangePattern:
		Walk(v.Lo, f)
		Walk(v.Hi, f)
	}
}

// walkTypeParams visits a type parameter list.
func walkTypeParams(params []*TypeParam, f func(Node) bool) {
	for _, p := range params {
		Walk(p, f)
	}
}

// walkDecls visits a declaration list.
func walkDecls(decls []Decl, f func(Node) bool) {
	for _, d := range decls {
		Walk(d, f)
	}
}
