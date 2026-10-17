// Cross-function region checks: a value a function stores into an object it
// received must live as long as that object, and a reference cycle must lie
// inside one region.
//
// The walk in marks.go settles the region of every object a function can name
// for itself. A store into a parameter names no such region: the object came
// from the caller, so its region is the caller's, and the region a function
// runs in is nested inside it. This file summarizes, for every function of the
// package, which parameters receive region-bearing values and where those
// values come from, then checks each call site against the regions the caller
// does know. The summary reaches through the calls of the package, so a value
// that passes through two functions is checked where its region is still
// visible.

package marks

import (
	"zyx/internal/ast"
	"zyx/internal/check"
	"zyx/internal/diag"
	"zyx/internal/resolve"
	"zyx/internal/token"
	"zyx/internal/types"
)

// A funcSummary records what one function does with the region-bearing values
// that reach it through its parameters. Parameter positions number the
// receiver first for a method, so they line up with the arguments of a call.
type funcSummary struct {
	// fresh holds the parameters the function stores a value it makes itself
	// into. Such a value is created in the region the call runs in, which the
	// caller can compare against the region of the object it passed.
	fresh map[int]bool
	// from maps a parameter onto the parameters whose values the function
	// stores into it.
	from map[int]map[int]bool
	// cycle holds the parameters whose object a value the function makes both
	// references and is stored into, which is a reference cycle that spans the
	// two regions.
	cycle map[int]bool
}

// newSummary returns an empty summary.
func newSummary() *funcSummary {
	return &funcSummary{
		fresh: map[int]bool{},
		from:  map[int]map[int]bool{},
		cycle: map[int]bool{},
	}
}

// mergeSummary adds the facts of src to dst and reports whether dst grew.
func mergeSummary(dst, src *funcSummary) bool {
	grew := false
	for p := range src.fresh {
		if !dst.fresh[p] {
			dst.fresh[p] = true
			grew = true
		}
	}
	for p, from := range src.from {
		for q := range from {
			if dst.from[p] == nil {
				dst.from[p] = map[int]bool{}
			}
			if !dst.from[p][q] {
				dst.from[p][q] = true
				grew = true
			}
		}
	}
	for p := range src.cycle {
		if !dst.cycle[p] {
			dst.cycle[p] = true
			grew = true
		}
	}
	return grew
}

// funcSummaries returns the summary of every function of a package. Summaries
// feed each other through the calls of the package, so the pass repeats until
// no function learns anything new.
func funcSummaries(pkg *resolve.Package, rinfo *resolve.Info, cpkg *check.Package) map[*ast.FuncDecl]*funcSummary {
	if pkg == nil || rinfo == nil || cpkg == nil {
		return nil
	}
	s := &summaryBuilder{rinfo: rinfo, cpkg: cpkg, sums: map[*ast.FuncDecl]*funcSummary{}}
	funcs := packageFuncs(pkg)
	for _, f := range funcs {
		s.sums[f] = newSummary()
	}
	for changed := true; changed; {
		changed = false
		for _, f := range funcs {
			if mergeSummary(s.sums[f], s.summaryOf(f)) {
				changed = true
			}
		}
	}
	return s.sums
}

// packageFuncs lists every function of a package, including the methods of its
// types, in declaration order.
func packageFuncs(pkg *resolve.Package) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	var decls func([]ast.Decl)
	var decl func(ast.Decl)
	decls = func(list []ast.Decl) {
		for _, d := range list {
			decl(d)
		}
	}
	decl = func(d ast.Decl) {
		switch v := d.(type) {
		case *ast.FuncDecl:
			out = append(out, v)
		case *ast.StructDecl:
			decls(v.Members)
		case *ast.EnumDecl:
			decls(v.Members)
		case *ast.ImplBlock:
			out = append(out, v.Methods...)
		}
	}
	for _, file := range pkg.Files {
		decls(file.Decls)
	}
	return out
}

// summaryBuilder computes the summaries of one package.
type summaryBuilder struct {
	rinfo *resolve.Info
	cpkg  *check.Package
	sums  map[*ast.FuncDecl]*funcSummary
}

// A funcInput carries the state of summarizing one function.
type funcInput struct {
	fn *ast.FuncDecl
	// sum is the summary being built.
	sum *funcSummary
	// params maps a parameter symbol onto its position.
	params map[string]int
	// allocs maps a local symbol onto the expression that makes its object.
	allocs map[string]ast.Expr
	// into records, per object a local holds, the parameters it is stored
	// into.
	into map[ast.Expr]map[int]bool
	// refs records, per object a local holds, the parameters that object
	// references.
	refs map[ast.Expr]map[int]bool
}

// summaryOf walks one function and returns what it does with its parameters.
func (s *summaryBuilder) summaryOf(f *ast.FuncDecl) *funcSummary {
	in := &funcInput{
		fn:     f,
		sum:    newSummary(),
		params: s.paramsOf(f),
		allocs: map[string]ast.Expr{},
		into:   map[ast.Expr]map[int]bool{},
		refs:   map[ast.Expr]map[int]bool{},
	}
	s.walk(f.Body, in)
	if f.BodyExpr != nil {
		s.walk(f.BodyExpr, in)
	}
	// A cycle spans two regions when one object the function made is stored
	// into a parameter while it also references what that parameter names.
	for obj, params := range in.into {
		for p := range params {
			if in.refs[obj][p] {
				in.sum.cycle[p] = true
			}
		}
	}
	return in.sum
}

// walk visits every store and call of one body.
func (s *summaryBuilder) walk(n ast.Node, in *funcInput) {
	if n == nil {
		return
	}
	if block, ok := n.(*ast.BlockStmt); ok && block == nil {
		return
	}
	ast.Walk(n, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.VarDecl:
			s.bind(in, v)
		case *ast.AssignStmt:
			if v.Op == token.Assign {
				s.store(in, v.Target, v.Value)
			}
		case *ast.CallExpr:
			s.call(v, in)
		}
		return true
	})
}

// bind records the object a local name holds, when the name is bound to one
// the function makes.
func (s *summaryBuilder) bind(in *funcInput, v *ast.VarDecl) {
	if len(v.Names) != 1 || v.Names[0] == nil || v.Value == nil {
		return
	}
	if !allocates(v.Value) {
		return
	}
	sym := s.rinfo.Defs[v.Names[0]]
	if sym == nil {
		return
	}
	in.allocs[sym.ID] = v.Value
}

// allocates reports whether an expression makes an object of its own, which is
// what a local can then hold and reference other objects through.
func allocates(e ast.Expr) bool {
	switch e.(type) {
	case *ast.NewExpr, *ast.AggregateLitExpr, *ast.ArrayLitExpr, *ast.ArrayRepeatExpr, *ast.CallExpr:
		return true
	}
	return false
}

// call records what a call does to the values the function passes it.
func (s *summaryBuilder) call(v *ast.CallExpr, in *funcInput) {
	// A method of a container or a channel keeps its argument, and the
	// declaration of such a method is not one this pass reads.
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok && s.storingMethod(sel) {
		if len(v.Args) > 0 {
			s.store(in, sel.X, v.Args[0])
		}
		return
	}
	decl := calleeDecl(s.rinfo, s.cpkg, v)
	if decl == nil {
		return
	}
	callee := s.sums[decl]
	if callee == nil {
		return
	}
	args := argList(v)
	for p := range callee.fresh {
		a := argAt(args, p)
		if a == nil {
			continue
		}
		q, ok := s.paramIndex(in, a)
		if !ok {
			continue
		}
		in.sum.fresh[q] = true
		if callee.cycle[p] {
			in.sum.cycle[q] = true
		}
	}
	for p, sources := range callee.from {
		dest := argAt(args, p)
		if dest == nil {
			continue
		}
		for q := range sources {
			src := argAt(args, q)
			if src == nil {
				continue
			}
			s.store(in, dest, src)
		}
	}
}

// storingMethod reports whether a selector names a method of a container or a
// channel that keeps its argument.
func (s *summaryBuilder) storingMethod(sel *ast.SelectorExpr) bool {
	if !storeMethod(sel.Name.Name) {
		return false
	}
	t := s.cpkg.Nodes[sel.X]
	return t != nil && (t.Kind == types.KindContainer || t.Kind == types.KindChan)
}

// store records that the value of src is stored into dest.
func (s *summaryBuilder) store(in *funcInput, dest, src ast.Expr) {
	if dest == nil || src == nil {
		return
	}
	// A value type and a string carry no region: they are copied, and the
	// bytes of a string live in static storage.
	if !s.regionValue(src) {
		return
	}
	// A destination only holds a value for as long as itself when it is a
	// reference type of its own. An element of a value type lies in the frame,
	// so a store into one does not outlive the function.
	if !s.referenceRoot(dest) {
		return
	}
	param, isParam := s.paramIndex(in, dest)
	local := s.localObject(in, dest)
	if !isParam && local == nil {
		return
	}
	if q, ok := s.mentionsParam(in, src); ok {
		if isParam {
			markFrom(in.sum.from, param, q)
		} else {
			markNode(in.refs, local, q)
		}
		return
	}
	if !isParam {
		return
	}
	in.sum.fresh[param] = true
	for _, obj := range s.localObjects(in, src) {
		markNode(in.into, obj, param)
	}
}

// paramsOf returns the parameters of a function by position.
func (s *summaryBuilder) paramsOf(f *ast.FuncDecl) map[string]int {
	out := map[string]int{}
	next := 0
	if self := s.selfSymbol(f); self != nil {
		out[self.ID] = next
		next++
	}
	for _, p := range f.Params {
		if p.Name != nil {
			if sym := s.rinfo.Defs[p.Name]; sym != nil {
				out[sym.ID] = next
			}
		}
		next++
	}
	return out
}

// selfSymbol returns the implicit receiver of a method, or nil.
func (s *summaryBuilder) selfSymbol(f *ast.FuncDecl) *resolve.Symbol {
	fn := s.rinfo.Defs[f]
	if fn == nil {
		return nil
	}
	for _, sym := range s.rinfo.Symbols {
		if sym.Owner == fn && sym.Kind == resolve.KindParam && sym.Name == "self" {
			return sym
		}
	}
	return nil
}

// paramIndex returns the position of the parameter a destination names, or
// false when it names something else.
func (s *summaryBuilder) paramIndex(in *funcInput, e ast.Expr) (int, bool) {
	root := rootIdent(e)
	if root == nil {
		return 0, false
	}
	sym := s.rinfo.Defs[root]
	if sym == nil {
		return 0, false
	}
	i, ok := in.params[sym.ID]
	return i, ok
}

// localObject returns the object a destination names when the name holds one
// the function made, or nil.
func (s *summaryBuilder) localObject(in *funcInput, e ast.Expr) ast.Expr {
	root := rootIdent(e)
	if root == nil {
		return nil
	}
	sym := s.rinfo.Defs[root]
	if sym == nil {
		return nil
	}
	return in.allocs[sym.ID]
}

// mentionsParam returns the position of the parameter a value is taken from,
// or false when the value mentions none.
func (s *summaryBuilder) mentionsParam(in *funcInput, e ast.Expr) (int, bool) {
	best := -1
	ast.Walk(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		sym := s.rinfo.Defs[id]
		if sym == nil {
			return true
		}
		i, ok := in.params[sym.ID]
		if ok && (best < 0 || i < best) {
			best = i
		}
		return true
	})
	if best < 0 {
		return 0, false
	}
	return best, true
}

// localObjects returns the objects the function made that a value mentions.
func (s *summaryBuilder) localObjects(in *funcInput, e ast.Expr) []ast.Expr {
	var out []ast.Expr
	seen := map[ast.Expr]bool{}
	ast.Walk(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		sym := s.rinfo.Defs[id]
		if sym == nil {
			return true
		}
		obj := in.allocs[sym.ID]
		if obj != nil && !seen[obj] {
			seen[obj] = true
			out = append(out, obj)
		}
		return true
	})
	return out
}

// regionValue reports whether a value carries a region with it.
func (s *summaryBuilder) regionValue(e ast.Expr) bool {
	return regionType(s.cpkg.Nodes[e])
}

// referenceRoot reports whether the object a destination names is a reference
// type, which is the case a store outlives the function for.
func (s *summaryBuilder) referenceRoot(e ast.Expr) bool {
	root := rootIdent(e)
	if root == nil {
		return false
	}
	return regionType(s.cpkg.Nodes[root])
}

// regionType reports whether a type is one an object of which belongs to a
// region. A string is a reference whose bytes live in static storage, and a
// view borrows a buffer another object owns, so neither carries a region of
// its own.
func regionType(t *types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind {
	case types.KindStruct, types.KindEnum, types.KindInterface, types.KindChan, types.KindContainer:
		return true
	}
	return false
}

// storeMethod reports whether a method name is one the built-in containers and
// channels use to keep their argument.
func storeMethod(name string) bool {
	switch name {
	case "Push", "Insert", "Add", "Set", "Send", "TrySend", "TryPush":
		return true
	}
	return false
}

// calleeDecl returns the declaration a call names when it is a function or a
// method of the package being compiled, or nil. A reference that reaches
// another package, or a value that happens to be callable, has no body this
// pass can read.
func calleeDecl(rinfo *resolve.Info, cpkg *check.Package, v *ast.CallExpr) *ast.FuncDecl {
	if rinfo == nil || cpkg == nil {
		return nil
	}
	switch fn := v.Fn.(type) {
	case *ast.Ident:
		if sym := rinfo.Defs[fn]; sym != nil {
			if decl, ok := sym.Decl.(*ast.FuncDecl); ok {
				return decl
			}
		}
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			if sym := rinfo.Defs[id]; sym != nil && sym.Kind == resolve.KindPackage {
				return nil
			}
		}
		recv := cpkg.Nodes[fn.X]
		if recv == nil {
			return nil
		}
		d := cpkg.Decl(recv)
		if d == nil {
			return nil
		}
		if sym := d.MethodSyms[fn.Name.Name]; sym != nil {
			if decl, ok := sym.Decl.(*ast.FuncDecl); ok {
				return decl
			}
		}
	}
	return nil
}

// argList returns the arguments of a call in parameter order: the receiver of
// a method comes first.
func argList(v *ast.CallExpr) []ast.Expr {
	if sel, ok := v.Fn.(*ast.SelectorExpr); ok {
		args := make([]ast.Expr, 0, len(v.Args)+1)
		args = append(args, sel.X)
		return append(args, v.Args...)
	}
	return v.Args
}

// argAt returns the argument at a position, or nil when the call has none.
func argAt(args []ast.Expr, i int) ast.Expr {
	if i < 0 || i >= len(args) {
		return nil
	}
	return args[i]
}

// rootIdent returns the name a chain of field reads, indexes, and tuple reads
// starts from.
func rootIdent(e ast.Expr) *ast.Ident {
	for {
		switch v := e.(type) {
		case *ast.Ident:
			return v
		case *ast.SelectorExpr:
			e = v.X
		case *ast.IndexExpr:
			e = v.X
		case *ast.TupleIndexExpr:
			e = v.X
		default:
			return nil
		}
	}
}

// markFrom records that the value of parameter q is stored into parameter p.
func markFrom(m map[int]map[int]bool, p, q int) {
	if m[p] == nil {
		m[p] = map[int]bool{}
	}
	m[p][q] = true
}

// markNode records that an object is stored into a parameter.
func markNode(m map[ast.Expr]map[int]bool, obj ast.Expr, p int) {
	if obj == nil {
		return
	}
	if m[obj] == nil {
		m[obj] = map[int]bool{}
	}
	m[obj][p] = true
}

// checkCall reports the stores a call makes that the regions of the caller's
// own objects cannot hold. The callee is a function of this package, so its
// summary says which parameters receive values and where those values are made.
func (b *builder) checkCall(v *ast.CallExpr) {
	if b.summaries == nil || b.region == nil {
		return
	}
	decl := calleeDecl(b.rinfo, b.cpkg, v)
	if decl == nil {
		return
	}
	sum := b.summaries[decl]
	if sum == nil {
		return
	}
	args := argList(v)
	// A value the callee makes itself is created in the region this call runs
	// in, so the object it is stored into must not outlive that region.
	for p := range sum.fresh {
		arg := argAt(args, p)
		if arg == nil {
			continue
		}
		region := b.argRegion(arg)
		if region == nil || b.region.contains(region) {
			continue
		}
		if sum.cycle[p] {
			b.diags = append(b.diags, diag.New(diag.CodeRegionCycle, v.Span(),
				"a reference cycle spans two regions: the value this call stores is released before the object it is stored into").
				WithHelp("keep the cycle inside one region, or pass a copy so the cycle lies in the receiving region"))
			continue
		}
		b.diags = append(b.diags, diag.New(diag.CodeRegionDomination, v.Span(),
			"a value this call stores is made in the region of the call, which leaves before the object it is stored into").
			WithHelp("keep the value in the region that runs the call, widen that region, or pass a copy"))
	}
	// A value that comes from the caller belongs to the region of the caller's
	// argument, so it is created where the destination lives.
	for p, sources := range sum.from {
		dest := argAt(args, p)
		if dest == nil {
			continue
		}
		for q := range sources {
			src := argAt(args, q)
			if src == nil {
				continue
			}
			if a := b.allocFor(src); a != nil {
				b.noteStore(src, dest)
				continue
			}
			rq, rp := b.argRegion(src), b.argRegion(dest)
			if rq == nil || rp == nil || rq.contains(rp) {
				continue
			}
			b.diags = append(b.diags, diag.New(diag.CodeRegionDomination, v.Span(),
				"a value this call stores is released before the object it is stored into").
				WithHelp("keep the value in the region that holds the destination, widen that region, or pass a copy"))
		}
	}
}

// argRegion returns the region the object an argument names lives in, as far
// as this function can tell: the region of an object the function made, the
// region current where a call makes the value, or the function's own root for a
// value that came from outside, whose region contains that root.
func (b *builder) argRegion(e ast.Expr) *Region {
	if e == nil {
		return nil
	}
	if a := b.allocFor(e); a != nil && a.Region != nil {
		return a.Region
	}
	if root := rootIdent(e); root != nil {
		if a := b.allocFor(root); a != nil && a.Region != nil {
			return a.Region
		}
		return b.root
	}
	if _, ok := e.(*ast.CallExpr); ok {
		return b.region
	}
	return b.root
}
