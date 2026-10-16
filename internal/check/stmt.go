// Statement checking and the bodies of functions.

package check

import (
	"strings"

	"zyx/internal/ast"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/source"
	"zyx/internal/token"
	"zyx/internal/types"
)

// checkBodies checks the body of every function, method, and lambda, and
// the initializer of every constant and variable.
func (c *checker) checkBodies() {
	for _, f := range c.pkg.Files {
		for _, d := range f.Decls {
			switch v := d.(type) {
			case *ast.FuncDecl:
				c.withParams(v.TypeParams, func() { c.checkFuncBody(v, nil) })
			case *ast.StructDecl:
				c.checkTypeBodies(v.Name, v.TypeParams, v.Members)
			case *ast.EnumDecl:
				c.checkTypeBodies(v.Name, v.TypeParams, v.Members)
			case *ast.ConstDecl:
				c.checkConstInit(v)
			case *ast.VarDecl:
				c.checkVarInit(v)
			}
		}
	}
}

// checkTypeBodies checks the method bodies and member constants of one type
// body. A method body sees its own type instantiated with the type parameters
// it declared, so a call through self resolves the way a call from outside
// does.
func (c *checker) checkTypeBodies(name *ast.Ident, params []*ast.TypeParam, members []ast.Decl) {
	d := c.declOfDecl(name)
	if d == nil {
		return
	}
	self := selfType(d)
	for _, m := range members {
		switch v := m.(type) {
		case *ast.FuncDecl:
			c.withParams(params, func() { c.checkFuncBody(v, self) })
		case *ast.ImplBlock:
			c.withParams(params, func() {
				for _, f := range v.Methods {
					c.checkFuncBody(f, self)
				}
			})
		case *ast.ConstDecl:
			c.checkMemberInit(d, v)
		}
	}
}

// selfType returns the type a method body reads through self: the declared
// type instantiated with its own type parameters. A type that declares none
// is its own receiver type.
func selfType(d *Decl) *types.Type {
	if len(d.Params) == 0 {
		return d.Type
	}
	return d.Type.Instantiate(d.Params...)
}

// checkFuncBody checks the body of one function or method. self is the
// receiver type of a method, or nil.
func (c *checker) checkFuncBody(f *ast.FuncDecl, self *types.Type) {
	// An init constructs a value rather than reading one, so it has no
	// receiver value. Self still names the type it builds, which is the
	// result type its declaration writes.
	receiver := self
	if f.Name != nil && f.Name.Name == "init" {
		receiver = nil
	}
	prevSelf, prevReceiver := c.self, c.receiver
	prevResult, prevComptime := c.result, c.inComptime
	c.self, c.receiver = self, receiver
	c.result, c.inComptime = c.buildType(f.Result), f.Quals.Comptime
	defer func() {
		c.self, c.receiver = prevSelf, prevReceiver
		c.result, c.inComptime = prevResult, prevComptime
	}()

	for _, p := range f.Params {
		t := c.buildType(p.Type)
		c.bindName(p.Name, t)
		if p.Default != nil {
			c.inComptime = true
			c.checkExpr(p.Default, t)
			c.requireConst(p.Default, "a default parameter value")
			c.inComptime = f.Quals.Comptime
		}
	}
	switch {
	case f.Body != nil:
		c.checkBlock(f.Body)
		c.checkReturns(f)
	case f.BodyExpr != nil:
		c.checkResult(f.BodyExpr, c.checkExpr(f.BodyExpr, c.result))
	}
}

// checkBlock checks a statement list.
func (c *checker) checkBlock(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		c.checkStmt(s)
	}
}

// checkStmt checks one statement.
func (c *checker) checkStmt(s ast.Stmt) {
	switch v := s.(type) {
	case nil:
	case *ast.BlockStmt:
		c.checkBlock(v)
	case *ast.ExprStmt:
		c.checkExpr(v.X, nil)
	case *ast.AssignStmt:
		c.checkAssign(v)
	case *ast.IfStmt:
		c.checkCond(v.Cond)
		c.checkStmt(v.Then)
		c.checkStmt(v.Else)
	case *ast.ForStmt:
		c.checkFor(v)
	case *ast.BreakStmt, *ast.ContinueStmt:
		// The label was checked during resolution.
	case *ast.ReturnStmt:
		c.checkReturn(v)
	case *ast.DeferStmt:
		c.checkExpr(v.Call, nil)
	case *ast.RegionStmt:
		c.inRegion++
		c.checkBlock(v.Body)
		c.inRegion--
	case *ast.ComptimeBlockStmt:
		prev := c.inComptime
		c.inComptime = true
		c.checkBlock(v.Body)
		c.inComptime = prev
		c.execComptimeBlock(v.Body)
	case *ast.MatchStmt:
		c.checkMatch(v)
	case *ast.SelectStmt:
		c.checkSelect(v)
	case *ast.VarDecl:
		c.checkLocalVar(v)
	case *ast.ConstDecl:
		c.checkLocalConst(v)
	}
}

// checkCond checks a condition, which must be bool.
func (c *checker) checkCond(e ast.Expr) {
	t := c.checkExpr(e, types.Bool)
	if t != nil && t.Kind != types.Invalid && !t.IsBool() {
		c.errorHelp(diag.CodeTypeMismatch, e.Span(),
			"write a bool condition", "condition is %s, want bool", t)
	}
}

// checkAssignTarget reports a target that has no storage to write. An array
// element and a map entry are places, a slice is a read-only view, and a str
// is immutable.
func (c *checker) checkAssignTarget(target ast.Expr) {
	index, ok := target.(*ast.IndexExpr)
	if !ok {
		return
	}
	base := c.out.Nodes[index.X]
	if base == nil {
		return
	}
	switch base.Kind {
	case types.KindSlice:
		c.errorHelp(diag.CodeTypeMismatch, target.Span(),
			"assign to a name, a field, or an element of a fixed-size array",
			"a view is read-only, so its elements are not assigned")
	case types.KindStr:
		c.errorHelp(diag.CodeTypeMismatch, target.Span(),
			"assign to a name, a field, or an element of a fixed-size array",
			"a str is immutable")
	}
}

// checkAssign checks an assignment, simple or compound.
func (c *checker) checkAssign(v *ast.AssignStmt) {
	if v.Op != token.Assign {
		lt := c.checkExpr(v.Target, nil)
		c.checkAssignTarget(v.Target)
		rt := c.checkExpr(v.Value, lt)
		lt, rt = c.adaptLiterals(v.Target, v.Value, lt, rt)
		if lt != nil && rt != nil && lt.Kind != types.Invalid &&
			rt.Kind != types.Invalid && !types.Identical(lt, rt) {
			c.errorHelp(diag.CodeTypeMismatch, v.Sp,
				"convert one operand explicitly",
				"%s needs two operands of one type, got %s and %s", v.Op, lt, rt)
		}
		return
	}
	dst := c.checkExpr(v.Target, nil)
	c.checkAssignTarget(v.Target)
	if dst == nil || dst.Kind == types.Invalid {
		return
	}
	got := c.checkExpr(v.Value, dst)
	if got == nil {
		return
	}
	if !c.accepts(dst, v.Value, got) {
		c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
			"assign a value of the target's type",
			"assigning %s to %s", got, dst)
	}
}

// checkFor checks one of the three loop forms.
func (c *checker) checkFor(v *ast.ForStmt) {
	if len(v.Bindings) > 0 {
		c.checkForIn(v)
		return
	}
	if v.Iter != nil {
		c.checkCond(v.Iter)
	}
	c.checkStmt(v.Body)
}

// checkForIn checks `for x in xs` and `for k, v in m`, which bind the
// element type of the iterable.
func (c *checker) checkForIn(v *ast.ForStmt) {
	var elem *types.Type
	if r, ok := v.Iter.(*ast.RangeExpr); ok {
		// A range is only a value here, so its bounds decide the binding.
		lo := c.checkExpr(r.Lo, nil)
		c.checkExpr(r.Hi, lo)
		elem = lo
	} else {
		iter := c.checkExpr(v.Iter, nil)
		elem = c.iterableElem(iter, v.Iter)
	}
	c.bindLoop(v.Bindings, elem)
	c.checkStmt(v.Body)
}

// iterableElem returns the element type a loop binds, or nil after
// reporting a subject that cannot be iterated.
func (c *checker) iterableElem(iter *types.Type, subject ast.Expr) *types.Type {
	if iter == nil || iter.Kind == types.Invalid {
		return nil
	}
	switch iter.Kind {
	case types.KindStr:
		return types.Char
	case types.KindArray, types.KindSlice:
		return iter.Elem
	case types.KindContainer:
		switch iter.Name {
		case "Array", "Set":
			return arg(iter, 0)
		case "Map":
			return types.TupleOf(arg(iter, 0), arg(iter, 1))
		}
	}
	c.errorHelp(diag.CodeNotIterable, subject.Span(),
		"iterate a container, a fixed-size array, str, or a range",
		"%s is not iterable", iter)
	return nil
}

// bindLoop binds the names of an iteration to the element type.
func (c *checker) bindLoop(names []*ast.Ident, elem *types.Type) {
	for i, n := range names {
		t := elem
		if len(names) > 1 {
			switch {
			case elem == nil:
				t = nil
			case elem.Kind == types.KindTuple && i < len(elem.Elems):
				t = elem.Elems[i]
			default:
				c.errorHelp(diag.CodeTypeMismatch, n.Sp,
					"iterate a Map to bind a key and a value",
					"two names need a key and a value, got %s", elem)
				t = nil
			}
		}
		c.bindName(n, t)
	}
}

// checkReturn checks a return statement against the declared result.
func (c *checker) checkReturn(v *ast.ReturnStmt) {
	if len(v.Values) == 0 {
		if c.result != nil {
			c.errorHelp(diag.CodeReturnType, v.Sp,
				"return a value of the declared type",
				"the function returns %s", c.result)
		}
		return
	}
	if c.result == nil {
		c.errorHelp(diag.CodeReturnType, v.Sp,
			"return nothing; the function has no result",
			"a function with no result cannot return a value")
		return
	}
	if len(v.Values) == 1 {
		c.checkResult(v.Values[0], c.checkExpr(v.Values[0], c.result))
		return
	}
	got := c.tupleType(&ast.TupleExpr{Sp: v.Sp, Elems: v.Values}, c.result)
	if !c.assignable(c.result, got) {
		c.errorHelp(diag.CodeReturnType, v.Sp,
			"return a value of the declared type",
			"returning %s, want %s", got, c.result)
	}
}

// checkResult reports a value that does not match the declared result type.
func (c *checker) checkResult(e ast.Expr, t *types.Type) {
	if c.result == nil || t == nil {
		return
	}
	if !c.accepts(c.result, e, t) {
		c.errorHelp(diag.CodeReturnType, e.Span(),
			"return a value of the declared type",
			"returning %s, want %s", t, c.result)
	}
}

// checkReturns reports a function whose declared result is not produced on
// every path.
func (c *checker) checkReturns(f *ast.FuncDecl) {
	if c.result == nil || f.Body == nil {
		return
	}
	if !c.exits(f.Body) {
		c.errorHelp(diag.CodeMissingReturn, f.Body.Sp,
			"return on every path, or end the function with panic",
			"the function returns %s and must return on every path", c.result)
	}
}

// exits reports whether a statement always leaves its block: a return, a
// jump out, a panic, or a branch whose every arm exits.
func (c *checker) exits(s ast.Stmt) bool {
	switch v := s.(type) {
	case nil:
		return false
	case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.ExprStmt:
		return isPanicCall(v.X)
	case *ast.BlockStmt:
		for _, x := range v.Stmts {
			if c.exits(x) {
				return true
			}
		}
		return false
	case *ast.IfStmt:
		return v.Else != nil && c.exits(v.Then) && c.exits(v.Else)
	case *ast.RegionStmt:
		return c.exits(v.Body)
	case *ast.ComptimeBlockStmt:
		return c.exits(v.Body)
	case *ast.SelectStmt:
		// Exactly one arm runs, so the statement exits when every arm does.
		if len(v.Arms) == 0 {
			return false
		}
		for _, arm := range v.Arms {
			if !c.exits(arm.Body) {
				return false
			}
		}
		return true
	case *ast.MatchStmt:
		if !c.matchExhaustive(v) {
			return false
		}
		for _, arm := range v.Arms {
			if !c.exits(arm.Body) {
				return false
			}
		}
		return true
	}
	return false
}

// isPanicCall reports whether an expression calls panic, which never
// returns.
func isPanicCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fn.(*ast.Ident)
	return ok && id.Name == "panic"
}

// checkLocalVar checks a local variable declaration.
func (c *checker) checkLocalVar(v *ast.VarDecl) {
	want := c.buildType(v.Type)
	got := c.checkExpr(v.Value, want)
	if want != nil {
		if !c.accepts(want, v.Value, got) {
			c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
				"assign a value of the declared type",
				"assigning %s to %s", got, want)
		}
		for _, n := range v.Names {
			c.bindName(n, want)
		}
		return
	}
	if len(v.Names) == 1 {
		c.bindName(v.Names[0], got)
		return
	}
	c.bindTuple(v.Names, v.Value, got)
}

// checkLocalConst checks a local constant declaration, whose value is
// evaluated at compile time.
func (c *checker) checkLocalConst(v *ast.ConstDecl) {
	prev := c.inComptime
	c.inComptime = true
	want := c.buildType(v.Type)
	got := c.checkExpr(v.Value, want)
	val := c.requireConst(v.Value, "a constant initializer")
	c.inComptime = prev
	if want == nil {
		if val != nil && val.Type != nil {
			c.bindName(v.Name, val.Type)
			return
		}
		c.bindName(v.Name, got)
		return
	}
	if !c.accepts(want, v.Value, got) {
		c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
			"assign a value of the declared type",
			"assigning %s to %s", got, want)
	}
	c.bindName(v.Name, want)
}

// checkConstInit checks the initializer of a top-level constant, which must
// be a compile-time expression.
func (c *checker) checkConstInit(v *ast.ConstDecl) {
	prev := c.inComptime
	c.inComptime = true
	t := c.buildType(v.Type)
	got := c.checkExpr(v.Value, t)
	val := c.constValue(c.symOf(v.Name))
	c.inComptime = prev
	if val != nil && v.Name != nil {
		c.out.Consts[v.Name.Name] = val
	}
	if t == nil {
		// An unannotated constant takes the type of its value.
		if val != nil && val.Type != nil {
			c.bindName(v.Name, val.Type)
			return
		}
		c.bindName(v.Name, got)
		return
	}
	if !c.accepts(t, v.Value, got) {
		c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
			"assign a value of the declared type",
			"assigning %s to %s", got, t)
	}
	// An annotated constant names a value of that type, so a use of the name
	// as a value finds it. Without this, only an unannotated constant has a
	// type and every use of an annotated one reads as nothing.
	c.bindName(v.Name, t)
}

// checkVarInit checks the initializer of a top-level variable, which lives
// in static storage and must therefore be a compile-time expression.
func (c *checker) checkVarInit(v *ast.VarDecl) {
	prev := c.inComptime
	c.inComptime = true
	t := c.buildType(v.Type)
	got := c.checkExpr(v.Value, t)
	var val *Value
	if c.staticSyncCall(v.Value) {
		// A static sync object lives in static storage rather than in a
		// region, so its constructor is a call the backend runs before the
		// program does rather than a compile-time value.
	} else {
		val = c.requireConst(v.Value, "a top-level var initializer")
	}
	c.inComptime = prev
	if len(v.Names) == 1 && val != nil {
		c.out.Consts[v.Names[0].Name] = val
		if sym := c.symOf(v.Names[0]); sym != nil {
			c.consts[sym.ID] = val
		}
	}
	switch {
	case t != nil:
		if !c.accepts(t, v.Value, got) {
			c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
				"assign a value of the declared type",
				"assigning %s to %s", got, t)
		}
		// An annotated variable names a value of that type, so a use of the
		// name as a value finds it.
		c.bindName(v.Names[0], t)
	case len(v.Names) == 1:
		if val != nil && val.Type != nil {
			c.bindName(v.Names[0], val.Type)
			return
		}
		c.bindName(v.Names[0], got)
	default:
		c.bindTuple(v.Names, v.Value, got)
	}
}

// staticSyncCall reports whether an initializer builds a static sync object,
// which is what the standard library's Static* constructors do. Such a value
// lives in static storage rather than in a region, so it needs no region to
// exist yet, and the initializer runs before the program does.
func (c *checker) staticSyncCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fn.(*ast.SelectorExpr)
	if !ok || sel.Name == nil {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	sym := c.symOf(id)
	if sym == nil || sym.Kind != resolve.KindPackage {
		return false
	}
	if sym.ID != "sync" && !strings.HasSuffix(sym.ID, "/sync") {
		return false
	}
	return strings.HasPrefix(sel.Name.Name, "Static")
}

// checkMemberInit checks a constant declared inside a type body, which must
// be a compile-time expression so meta.attr can read its value.
func (c *checker) checkMemberInit(d *Decl, v *ast.ConstDecl) {
	prev := c.inComptime
	c.inComptime = true
	t := c.buildType(v.Type)
	got := c.checkExpr(v.Value, t)
	val := c.constValue(c.symOf(v.Name))
	c.inComptime = prev
	switch {
	case t != nil && !c.accepts(t, v.Value, got):
		c.errorHelp(diag.CodeTypeMismatch, v.Value.Span(),
			"assign a value of the declared type",
			"assigning %s to %s", got, t)
	case t == nil && val != nil && val.Type != nil:
		if _, ok := d.Consts[v.Name.Name]; !ok {
			d.Consts[v.Name.Name] = &Const{Sym: c.symOf(v.Name), Type: val.Type}
		}
	}
}

// bindName records the type of one binding.
func (c *checker) bindName(n *ast.Ident, t *types.Type) {
	if n == nil || n.Name == "_" || t == nil {
		return
	}
	if sym := c.symOf(n); sym != nil {
		c.bySym[sym.ID] = t
	}
}

// bindTuple distributes the elements of a tuple value over a binding list.
func (c *checker) bindTuple(names []*ast.Ident, value ast.Expr, got *types.Type) {
	if got != nil && got.Kind != types.Invalid && got.Kind != types.KindTuple {
		c.errorHelp(diag.CodeTypeMismatch, spanOf(value),
			"bind one name, or initialize from a tuple",
			"%d names need a tuple, got %s", len(names), got)
		return
	}
	if got != nil && got.Kind == types.KindTuple && len(got.Elems) != len(names) {
		c.errorHelp(diag.CodeTypeMismatch, spanOf(value),
			"bind as many names as the tuple has elements",
			"%s has %d elements for %d names", got, len(got.Elems), len(names))
	}
	for i, n := range names {
		if got != nil && i < len(got.Elems) {
			c.bindName(n, got.Elems[i])
		}
	}
}

// checkSelect checks a select statement.
func (c *checker) checkSelect(v *ast.SelectStmt) {
	timeout, def := false, false
	for _, arm := range v.Arms {
		switch arm.Kind {
		case ast.SelectRecv:
			c.bindRecv(arm, c.checkExpr(arm.Chan, nil))
		case ast.SelectSend:
			ch := c.checkExpr(arm.Chan, nil)
			elem := chanElem(ch)
			got := c.checkExpr(arm.Value, elem)
			if elem != nil && !c.accepts(elem, arm.Value, got) {
				c.errorHelp(diag.CodeTypeMismatch, arm.Value.Span(),
					"send the channel's element type",
					"sending %s to %s", got, ch)
			}
		case ast.SelectTimeout:
			timeout = true
			c.checkExpr(arm.Value, types.Duration)
		case ast.SelectDefault:
			def = true
		}
		c.checkStmt(arm.Body)
	}
	if timeout && def {
		c.errorHelp(diag.CodeSelectArms, v.Sp,
			"keep one of timeout and default",
			"a select cannot hold both timeout and default")
	}
}

// bindRecv binds the value and flag of a receive arm.
func (c *checker) bindRecv(arm *ast.SelectArm, ch *types.Type) {
	if ch == nil || ch.Kind == types.Invalid {
		return
	}
	if ch.Kind != types.KindChan {
		c.errorHelp(diag.CodeTypeMismatch, arm.Chan.Span(),
			"receive from a channel", "%s is not a channel", ch)
		return
	}
	c.bindName(arm.RecvValue, ch.Elem)
	c.bindName(arm.RecvOk, types.Bool)
}

// chanElem returns the element type of a channel, or nil.
func chanElem(t *types.Type) *types.Type {
	if t != nil && t.Kind == types.KindChan {
		return t.Elem
	}
	return nil
}

// spanOf returns the span of an expression, or a zero span when it is nil.
func spanOf(e ast.Expr) source.Span {
	if e == nil {
		return source.Span{}
	}
	return e.Span()
}
