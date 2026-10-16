// Package marks builds the memory mark model attached to .zyxc nodes: the
// home region of every object, ownership moves and returns, sharing, static
// release, coroutine boundary handling, region hosting, iteration guards,
// view borrows, shared stores, and C call blocking. Every decision is made
// at compile time; the machine code carries no marks.

package marks

import (
	"sort"

	"zyx/internal/ast"
	"zyx/internal/check"
	"zyx/internal/coro"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/token"
	"zyx/internal/types"
)

// A Mark is one mark attached to a node.
type Mark struct {
	// Name is the mark name, such as "home" or "coro:spawn".
	Name string
	// Arg is the argument of a parameterized mark, empty when it takes
	// none.
	Arg string
}

// String renders the mark the way the .zyxc layer writes it.
func (m Mark) String() string {
	if m.Arg == "" {
		return m.Name
	}
	return m.Name + ":" + m.Arg
}

// An Info is the mark model of one package.
type Info struct {
	// Marks maps each node to its marks, in insertion order.
	Marks map[ast.Node][]Mark
	// Regions lists every region of the package in function and lexical
	// order.
	Regions []*Region
}

// MarksOf returns the marks of a node.
func (i *Info) MarksOf(n ast.Node) []Mark { return i.Marks[n] }

// Add attaches one mark to a node.
func (i *Info) Add(n ast.Node, name, arg string) {
	if n == nil {
		return
	}
	i.Marks[n] = append(i.Marks[n], Mark{Name: name, Arg: arg})
}

// Has reports whether a node carries a mark of that name.
func (i *Info) Has(n ast.Node, name string) bool {
	for _, m := range i.Marks[n] {
		if m.Name == name {
			return true
		}
	}
	return false
}

// Sort orders every mark list by name and then argument, which the .zyxc
// layer requires.
func (i *Info) Sort() {
	for node, marks := range i.Marks {
		sort.SliceStable(marks, func(a, b int) bool {
			if marks[a].Name != marks[b].Name {
				return marks[a].Name < marks[b].Name
			}
			return marks[a].Arg < marks[b].Arg
		})
		i.Marks[node] = marks
	}
}

// Build attaches the memory marks of one package.
func Build(pkg *resolve.Package, rinfo *resolve.Info, cpkg *check.Package, cinfo *coro.Info) (*Info, []diag.Diagnostic) {
	b := &builder{
		pkg:    pkg,
		rinfo:  rinfo,
		cpkg:   cpkg,
		coro:   cinfo,
		info:   &Info{Marks: map[ast.Node][]Mark{}},
		hosted: map[ast.Node][]*coro.Coroutine{},
	}
	for _, c := range cinfo.Coroutines {
		if c.Host != nil {
			b.hosted[c.Host] = append(b.hosted[c.Host], c)
		}
	}
	b.run()
	b.info.Sort()
	return b.info, b.diags
}

// builder carries the walk state of one package.
type builder struct {
	pkg   *resolve.Package
	rinfo *resolve.Info
	cpkg  *check.Package
	coro  *coro.Info

	info  *Info
	diags []diag.Diagnostic

	// hosted maps a region block to the coroutines it hosts.
	hosted map[ast.Node][]*coro.Coroutine

	// Current function state.
	fn      *ast.FuncDecl
	regions *regionBuilder
	region  *Region
	// allocsByNode maps an allocating expression to its record.
	allocsByNode map[ast.Expr]*allocation
	// allocsBySym maps a local symbol to the allocation it holds.
	allocsBySym map[string]*allocation
	// uses maps an allocation to the regions its value is used in.
	uses map[*allocation][]*Region
	// lastUse maps an allocation to the node of its last use.
	lastUse map[*allocation]ast.Node
	// escapes records how each allocation leaves the function.
	escapes map[*allocation]escapeKind
}

// run walks every declaration of the package.
func (b *builder) run() {
	for _, file := range b.pkg.Files {
		for _, decl := range file.Decls {
			b.decl(decl)
		}
	}
}

// decl walks one top-level declaration.
func (b *builder) decl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		b.funcDecl(d)
	case *ast.StructDecl:
		b.members(d.Members)
	case *ast.EnumDecl:
		b.members(d.Members)
	case *ast.VarDecl:
		b.expr(d.Value)
	case *ast.ConstDecl:
		b.expr(d.Value)
	}
}

// members walks a type body.
func (b *builder) members(members []ast.Decl) {
	for _, member := range members {
		switch m := member.(type) {
		case *ast.FuncDecl:
			b.funcDecl(m)
		case *ast.ImplBlock:
			for _, f := range m.Methods {
				b.funcDecl(f)
			}
		case *ast.ConstDecl:
			b.expr(m.Value)
		}
	}
}

// funcDecl walks one function with a fresh region tree.
func (b *builder) funcDecl(f *ast.FuncDecl) {
	if f == nil {
		return
	}
	prevFn, prevRegions, prevRegion := b.fn, b.regions, b.region
	b.fn = f
	b.regions = newRegionBuilder(f)
	b.region = b.regions.root()
	b.info.Regions = append(b.info.Regions, b.region)
	b.uses = map[*allocation][]*Region{}
	b.lastUse = map[*allocation]ast.Node{}
	b.allocsByNode = map[ast.Expr]*allocation{}
	b.allocsBySym = map[string]*allocation{}
	b.escapes = map[*allocation]escapeKind{}

	b.stmt(f.Body)
	b.expr(f.BodyExpr)
	b.finishFunction()

	b.fn, b.regions, b.region = prevFn, prevRegions, prevRegion
}

// stmt walks one statement. A statement the parser left empty appears as
// a nil node, which a walk must not follow.
func (b *builder) stmt(s ast.Stmt) {
	if s == nil {
		return
	}
	switch v := s.(type) {
	case nil:
		return
	case *ast.BlockStmt:
		if v == nil {
			return
		}
		for _, x := range v.Stmts {
			b.stmt(x)
		}
	case *ast.ExprStmt:
		b.expr(v.X)
	case *ast.VarDecl:
		b.expr(v.Value)
		b.bind(v.Names, v.Value)
	case *ast.ConstDecl:
		b.expr(v.Value)
		b.bind([]*ast.Ident{v.Name}, v.Value)
	case *ast.AssignStmt:
		b.assign(v)
	case *ast.IfStmt:
		b.expr(v.Cond)
		b.stmt(v.Then)
		b.stmt(v.Else)
	case *ast.ForStmt:
		b.expr(v.Iter)
		if len(v.Bindings) > 0 {
			b.guardLoop(v)
		}
		b.stmt(v.Body)
	case *ast.ReturnStmt:
		for _, e := range v.Values {
			b.expr(e)
			b.escapesTo(e, escapeReturn)
		}
	case *ast.DeferStmt:
		b.info.Add(v, "defer-site", "")
		b.expr(v.Call)
	case *ast.RegionStmt:
		b.regionBlock(v)
	case *ast.ComptimeBlockStmt:
		b.stmt(v.Body)
	case *ast.MatchStmt:
		b.expr(v.Subject)
		for _, arm := range v.Arms {
			b.expr(arm.Guard)
			b.stmt(arm.Body)
		}
	case *ast.SelectStmt:
		b.info.Add(v, "chan:select", "")
		for _, arm := range v.Arms {
			b.expr(arm.Chan)
			b.expr(arm.Value)
			b.stmt(arm.Body)
		}
	}
}

// regionBlock walks a region block, which is an independent memory region
// and the host of the coroutines it encloses.
func (b *builder) regionBlock(v *ast.RegionStmt) {
	prev := b.region
	r := b.regions.explicit(v, prev)
	b.region = r
	b.info.Regions = append(b.info.Regions, r)
	b.info.Add(v, "region:enter", r.Name)
	if hosts := b.hosted[v]; len(hosts) > 0 {
		b.info.Add(v, "coro:host", "")
	}
	b.stmt(v.Body)
	b.info.Add(v, "region:exit", r.Name)
	b.region = prev
}

// assign walks an assignment and records the value's uses.
func (b *builder) assign(v *ast.AssignStmt) {
	// A write whose target is a value shared across coroutines is recorded,
	// because it is the one write the language allows to be observed by
	// another coroutine.
	b.noteSharedStore(v.Target, v)
	if id, ok := v.Target.(*ast.Ident); ok && v.Op == token.Assign {
		// A plain assignment to a name moves the value to that name.
		b.expr(v.Value)
		b.bind([]*ast.Ident{id}, v.Value)
		return
	}
	// A field or element write shares the value with the container.
	if sel, ok := v.Target.(*ast.SelectorExpr); ok {
		b.expr(v.Value)
		b.share(v.Value, v)
		b.expr(sel.X)
		return
	}
	if idx, ok := v.Target.(*ast.IndexExpr); ok {
		b.expr(v.Value)
		b.share(v.Value, v)
		b.expr(idx.X)
		b.expr(idx.Index)
		return
	}
	b.expr(v.Target)
	b.expr(v.Value)
}

// expr walks one expression, attaching the marks its form requires.
func (b *builder) expr(e ast.Expr) {
	switch v := e.(type) {
	case nil:
		return
	case *ast.Ident:
		b.use(v)
	case *ast.Literal:
		for _, part := range v.Parts {
			b.expr(part.X)
		}
	case *ast.BinaryExpr:
		b.expr(v.X)
		b.expr(v.Y)
	case *ast.UnaryExpr:
		b.expr(v.X)
	case *ast.AddrOfExpr:
		b.expr(v.X)
	case *ast.IsExpr:
		b.expr(v.X)
	case *ast.NewExpr:
		for _, a := range v.Args {
			b.expr(a)
		}
		b.alloc(v, v)
	case *ast.AggregateLitExpr:
		for _, entry := range v.Entries {
			b.expr(entry.Key)
			b.expr(entry.Value)
		}
		b.alloc(v, v)
	case *ast.ArrayLitExpr:
		for _, x := range v.Elems {
			b.expr(x)
		}
		b.alloc(v, v)
	case *ast.ArrayRepeatExpr:
		b.expr(v.Elem)
		b.expr(v.Count)
		b.alloc(v, v)
	case *ast.CallExpr:
		b.call(v)
	case *ast.SelectorExpr:
		b.expr(v.X)
		// A method taken as a value carries its receiver, so the receiver is
		// shared with the value rather than released at its last use. A
		// field read takes nothing, so it leaves the object alone.
		if b.cpkg.MethodValues[v] {
			b.escapesTo(v.X, escapeStore)
		}
	case *ast.TupleIndexExpr:
		b.expr(v.X)
	case *ast.IndexExpr:
		b.expr(v.X)
		if _, ok := v.Index.(*ast.RangeExpr); ok {
			b.info.Add(v, "view-borrow", "")
		}
		b.expr(v.Index)
	case *ast.RangeExpr:
		b.expr(v.Lo)
		b.expr(v.Hi)
	case *ast.TryExpr:
		b.expr(v.X)
	case *ast.SpawnExpr:
		b.spawn(v)
	case *ast.LambdaExpr:
		b.stmt(v.Body)
		b.expr(v.BodyExpr)
	case *ast.TupleExpr:
		for _, x := range v.Elems {
			b.expr(x)
		}
	}
}

// call walks a call and marks a C call with its blocking classification.
func (b *builder) call(v *ast.CallExpr) {
	if b.isCCall(v) {
		if b.cFast(v) {
			b.info.Add(v, "c-fast", "")
		} else {
			b.info.Add(v, "c-blocking", "")
		}
	}
	// A default the call leaves out is evaluated here, so it is walked here.
	for _, a := range ast.WithDefaults(v.Args, b.calleeParams(v)) {
		b.expr(a)
	}
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
		// A called selector reaches a method, so the receiver is walked and
		// the method is not taken as a value. A channel operation is the call
		// the program writes, so the mark names that call.
		b.expr(sel.X)
		if b.isChannelCall(sel) {
			b.info.Add(v, "chan:"+channelVerb(sel.Name.Name), "")
		}
		return
	}
	b.expr(v.Fn)
}

// isCCall reports whether a call crosses to C, which decides whether it runs
// on the current coroutine or through the blocking pool. A call reaches C
// either through the c namespace or by naming an extern function, whose
// symbol is the C function itself.
func (b *builder) isCCall(v *ast.CallExpr) bool {
	switch fn := v.Fn.(type) {
	case *ast.SelectorExpr:
		id, ok := fn.X.(*ast.Ident)
		return ok && b.cNamespace(id)
	case *ast.Ident:
		sym := b.rinfo.Defs[fn]
		if sym == nil {
			return false
		}
		decl, ok := sym.Decl.(*ast.FuncDecl)
		return ok && decl.Quals.Extern
	}
	return false
}

// cFast reports whether a call runs on the calling coroutine rather than
// through the blocking pool. The declaration that links the C function
// settles it, so the answer comes from the package's own linkage
// declarations rather than from the call site.
func (b *builder) cFast(v *ast.CallExpr) bool {
	if b.cpkg == nil {
		return false
	}
	switch fn := v.Fn.(type) {
	case *ast.SelectorExpr:
		return b.cpkg.FastC[fn.Name.Name]
	case *ast.Ident:
		return b.cpkg.FastC[fn.Name]
	}
	return false
}

// spawn walks a coroutine start: it opens the coroutine's private region
// and decides how each argument crosses the boundary.
func (b *builder) spawn(v *ast.SpawnExpr) {
	parent := b.region
	r := b.regions.coroutine(v, parent)
	b.info.Regions = append(b.info.Regions, r)
	b.info.Add(v, "coro:spawn", "")
	b.info.Add(v, "coro:region", r.Name)
	if v.Watcher != nil {
		b.info.Add(v, "coro:panic-path", "")
	}

	prev := b.region
	b.region = r
	if call, ok := v.X.(*ast.CallExpr); ok {
		for _, a := range call.Args {
			b.info.Add(a, boundaryMark(b.cpkg, a), "")
			b.info.Add(v, "coro:handoff", "")
			b.expr(a)
			b.escapesTo(a, escapeCoroutine)
		}
		b.expr(call.Fn)
	} else {
		b.expr(v.X)
	}
	b.region = prev

	// The watcher runs in the enclosing region, not in the coroutine.
	b.expr(v.Watcher)
}

// bind records the allocation a binding holds.
func (b *builder) bind(names []*ast.Ident, value ast.Expr) {
	if len(names) != 1 || names[0] == nil {
		return
	}
	a := b.allocsByNode[value]
	if a == nil {
		return
	}
	sym := b.rinfo.Defs[names[0]]
	if sym == nil {
		return
	}
	a.Sym = sym.ID
	b.allocsBySym[sym.ID] = a
}

// alloc records an allocation site and gives it a provisional home.
func (b *builder) alloc(node ast.Node, value ast.Expr) {
	a := &allocation{Node: value, Region: b.region}
	b.allocsByNode[value] = a
	b.info.Add(node, "home", b.region.Name)
}

// use records one use of a local name, which is also a use of the object the
// name holds.
func (b *builder) use(id *ast.Ident) {
	sym := b.rinfo.Defs[id]
	if sym == nil {
		return
	}
	a := b.allocsBySym[sym.ID]
	if a == nil {
		return
	}
	b.uses[a] = append(b.uses[a], b.region)
	b.lastUse[a] = id
}

// allocFor returns the allocation a value expression denotes: the allocating
// expression itself, or the object a binding holds.
func (b *builder) allocFor(value ast.Expr) *allocation {
	if a := b.allocsByNode[value]; a != nil {
		return a
	}
	id, ok := value.(*ast.Ident)
	if !ok {
		return nil
	}
	sym := b.rinfo.Defs[id]
	if sym == nil {
		return nil
	}
	return b.allocsBySym[sym.ID]
}

// noteSharedStore records a write to a value shared across coroutines. The
// language allows exactly one coroutine to own a mutable value, so a write
// through a shared one is the case the mark exists for.
func (b *builder) noteSharedStore(target ast.Expr, at ast.Node) {
	if b.cpkg == nil {
		return
	}
	if t := b.cpkg.Nodes[target]; t != nil && b.cpkg.SyncSafe(t) {
		b.info.Add(at, "shared-store", "")
	}
}

// share records that a value is stored where it outlives its binding.
func (b *builder) share(value ast.Expr, at ast.Node) {
	if a := b.allocFor(value); a != nil {
		b.escapes[a] = escapeStore
	}
	if isDerivedView(value) {
		b.diags = append(b.diags, diag.New(diag.CodeViewLifetime, value.Span(),
			"a view cannot be stored where it outlives the container it borrows").
			WithHelp("copy the elements, or keep the container itself"))
	}
	b.info.Add(at, "shared-ref", "")
}

// isDerivedView reports whether an expression produces a view of a
// container's buffer.
func isDerivedView(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.IndexExpr:
		_, ok := v.Index.(*ast.RangeExpr)
		return ok
	case *ast.CallExpr:
		sel, ok := v.Fn.(*ast.SelectorExpr)
		return ok && sel.Name.Name == "Bytes"
	}
	return false
}

// escapesTo records how a value leaves the function that created it.
func (b *builder) escapesTo(value ast.Expr, kind escapeKind) {
	a := b.allocFor(value)
	if a == nil {
		return
	}
	if prev := b.escapes[a]; prev != escapeNone && prev != kind {
		// Two different escapes: the object is truly shared.
		b.escapes[a] = escapeStore
		return
	}
	b.escapes[a] = kind
}

// calleeParams returns the parameters of the declaration a call names, or nil
// when the callee is a value rather than a function or a method.
func (b *builder) calleeParams(v *ast.CallExpr) []*ast.Param {
	switch fn := v.Fn.(type) {
	case *ast.Ident:
		if sym := b.rinfo.Defs[fn]; sym != nil {
			if decl, ok := sym.Decl.(*ast.FuncDecl); ok {
				return decl.Params
			}
		}
	case *ast.SelectorExpr:
		recv := b.cpkg.Nodes[fn.X]
		if recv == nil {
			return nil
		}
		d := b.cpkg.Decl(recv)
		if d == nil {
			return nil
		}
		sym := d.MethodSyms[fn.Name.Name]
		if sym == nil {
			return nil
		}
		if decl, ok := sym.Decl.(*ast.FuncDecl); ok {
			return decl.Params
		}
	}
	return nil
}

// isChannelCall reports whether a selector names a channel operation.
func (b *builder) isChannelCall(v *ast.SelectorExpr) bool {
	switch v.Name.Name {
	case "Send", "Recv", "TrySend", "TryRecv", "Close":
	default:
		return false
	}
	recv := b.cpkg.Nodes[v.X]
	return recv != nil && recv.Kind == types.KindChan
}

// channelVerb maps a channel method to its mark verb.
func channelVerb(name string) string {
	switch name {
	case "Send":
		return "send"
	case "Recv":
		return "recv"
	case "TrySend", "TryRecv":
		return "try"
	case "Close":
		return "close"
	}
	return "send"
}

// cNamespace reports whether an identifier is the C namespace.
func (b *builder) cNamespace(id *ast.Ident) bool {
	sym := b.rinfo.Defs[id]
	return sym != nil && sym.Kind == resolve.KindPackage && sym.Name == "c"
}
