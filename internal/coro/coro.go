// Package coro models coroutine structure and spawn constraints. It records
// where each coroutine starts, which explicit region hosts it, and the
// private region its objects belong to.

package coro

import (
	"zyx/internal/ast"
	"zyx/internal/resolve"
)

// A Coroutine is one spawn site.
type Coroutine struct {
	// Spawn is the spawn expression that starts the coroutine.
	Spawn *ast.SpawnExpr
	// Watched marks the spawn_watched form.
	Watched bool
	// Host is the innermost region block that encloses the spawn, or nil
	// when the spawn sits directly in a function body.
	Host *ast.RegionStmt
	// Region is the name of the coroutine's private region.
	Region string
}

// An Info is the coroutine structure of one package.
type Info struct {
	// Coroutines lists the spawn sites in source order.
	Coroutines []*Coroutine
}

// Build models the coroutines of one package. The caller has already
// checked that every spawn sits inside a region.
func Build(pkg *resolve.Package) *Info {
	b := &builder{info: &Info{}}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			b.walkDecl(decl)
		}
	}
	return b.info
}

// builder carries the walk state.
type builder struct {
	info *Info
	// host is the innermost enclosing region block, or nil.
	host *ast.RegionStmt
}

// walkDecl walks one declaration.
func (b *builder) walkDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		b.walkFunc(d)
	case *ast.StructDecl:
		b.walkMembers(d.Members)
	case *ast.EnumDecl:
		b.walkMembers(d.Members)
	case *ast.VarDecl:
		b.walkExpr(d.Value)
	case *ast.ConstDecl:
		b.walkExpr(d.Value)
	}
}

// walkMembers walks the members of a type body.
func (b *builder) walkMembers(members []ast.Decl) {
	for _, member := range members {
		switch m := member.(type) {
		case *ast.FuncDecl:
			b.walkFunc(m)
		case *ast.ImplBlock:
			for _, f := range m.Methods {
				b.walkFunc(f)
			}
		case *ast.ConstDecl:
			b.walkExpr(m.Value)
		}
	}
}

// walkFunc walks a function body.
func (b *builder) walkFunc(f *ast.FuncDecl) {
	b.walkStmt(f.Body)
	b.walkExpr(f.BodyExpr)
}

// walkStmt walks one statement.
func (b *builder) walkStmt(s ast.Stmt) {
	switch v := s.(type) {
	case *ast.BlockStmt:
		for _, x := range v.Stmts {
			b.walkStmt(x)
		}
	case *ast.ExprStmt:
		b.walkExpr(v.X)
	case *ast.AssignStmt:
		b.walkExpr(v.Target)
		b.walkExpr(v.Value)
	case *ast.VarDecl:
		b.walkExpr(v.Value)
	case *ast.ConstDecl:
		b.walkExpr(v.Value)
	case *ast.IfStmt:
		b.walkExpr(v.Cond)
		b.walkStmt(v.Then)
		b.walkStmt(v.Else)
	case *ast.ForStmt:
		b.walkExpr(v.Iter)
		b.walkStmt(v.Body)
	case *ast.ReturnStmt:
		for _, e := range v.Values {
			b.walkExpr(e)
		}
	case *ast.DeferStmt:
		b.walkExpr(v.Call)
	case *ast.RegionStmt:
		// A region block is the host of the coroutines it encloses.
		prev := b.host
		b.host = v
		b.walkStmt(v.Body)
		b.host = prev
	case *ast.ComptimeBlockStmt:
		b.walkStmt(v.Body)
	case *ast.MatchStmt:
		b.walkExpr(v.Subject)
		for _, arm := range v.Arms {
			b.walkExpr(arm.Guard)
			b.walkStmt(arm.Body)
		}
	case *ast.SelectStmt:
		for _, arm := range v.Arms {
			b.walkExpr(arm.Chan)
			b.walkExpr(arm.Value)
			b.walkStmt(arm.Body)
		}
	}
}

// walkExpr walks one expression.
func (b *builder) walkExpr(e ast.Expr) {
	switch v := e.(type) {
	case nil:
		return
	case *ast.SpawnExpr:
		b.info.Coroutines = append(b.info.Coroutines, &Coroutine{
			Spawn:   v,
			Watched: v.Watcher != nil,
			Host:    b.host,
			Region:  "c0",
		})
		b.walkExpr(v.X)
		b.walkExpr(v.Watcher)
	case *ast.CallExpr:
		b.walkExpr(v.Fn)
		for _, a := range v.Args {
			b.walkExpr(a)
		}
	case *ast.BinaryExpr:
		b.walkExpr(v.X)
		b.walkExpr(v.Y)
	case *ast.UnaryExpr:
		b.walkExpr(v.X)
	case *ast.AddrOfExpr:
		b.walkExpr(v.X)
	case *ast.IsExpr:
		b.walkExpr(v.X)
	case *ast.SelectorExpr:
		b.walkExpr(v.X)
	case *ast.TupleIndexExpr:
		b.walkExpr(v.X)
	case *ast.IndexExpr:
		b.walkExpr(v.X)
		b.walkExpr(v.Index)
	case *ast.RangeExpr:
		b.walkExpr(v.Lo)
		b.walkExpr(v.Hi)
	case *ast.NewExpr:
		for _, a := range v.Args {
			b.walkExpr(a)
		}
	case *ast.TryExpr:
		b.walkExpr(v.X)
	case *ast.FastExpr:
		b.walkExpr(v.X)
	case *ast.LambdaExpr:
		b.walkStmt(v.Body)
		b.walkExpr(v.BodyExpr)
	case *ast.TupleExpr:
		for _, x := range v.Elems {
			b.walkExpr(x)
		}
	case *ast.ArrayLitExpr:
		for _, x := range v.Elems {
			b.walkExpr(x)
		}
	case *ast.ArrayRepeatExpr:
		b.walkExpr(v.Elem)
		b.walkExpr(v.Count)
	case *ast.AggregateLitExpr:
		for _, entry := range v.Entries {
			b.walkExpr(entry.Key)
			b.walkExpr(entry.Value)
		}
	case *ast.Literal:
		for _, part := range v.Parts {
			b.walkExpr(part.X)
		}
	}
}
